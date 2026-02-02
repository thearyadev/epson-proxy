package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func generateCert() error {
	certPath := "server.crt"
	keyPath := "server.key"

	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			return nil
		}
	}

	fmt.Println("Generating SSL certificate...")

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Epson Proxy"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %v", err)
	}

	certFile, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("failed to create cert file: %v", err)
	}
	defer certFile.Close()
	pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyFile, err := os.Create(keyPath)
	if err != nil {
		return fmt.Errorf("failed to create key file: %v", err)
	}
	defer keyFile.Close()

	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return fmt.Errorf("failed to marshal private key: %v", err)
	}
	pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})

	fmt.Println("SSL certificate generated successfully")
	return nil
}

func main() {
	var (
		printerConn  = flag.String("printer", "", "Printer connection string (required)")
		receiptWidth = flag.Int("receipt-width", 576, "Receipt width in pixels")
		proto        = flag.String("proto", "", "Protocol: USB or TCP (required)")
		host         = flag.String("host", "127.0.0.1", "Server host")
		port         = flag.String("port", "8000", "Server port")
		secure       = flag.Bool("secure", false, "Use HTTPS")
	)
	flag.Parse()

	if *printerConn == "" {
		fmt.Fprintf(os.Stderr, "Error: -printer flag is required\n")
		flag.Usage()
		os.Exit(1)
	}

	if *proto == "" {
		fmt.Fprintf(os.Stderr, "Error: -proto flag is required\n")
		flag.Usage()
		os.Exit(1)
	}

	var connType ConnectionType
	switch *proto {
	case "TCP":
		connType = TcpSocket
	case "USB":
		connType = UsbPath
	default:
		fmt.Fprintf(os.Stderr, "Unknown protocol: %s (must be USB or TCP)\n", *proto)
		os.Exit(1)
	}

	printer, err := NewPrinter(*printerConn, *receiptWidth, connType)
	if err != nil {
		log.Fatalf("Failed to connect to printer: %v", err)
	}
	defer printer.Close()
	fmt.Println("Connected to printer:", printer.connection_string)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
			return
		}

		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to read body: %v", err), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if len(data) == 0 {
			http.Error(w, "Empty request body", http.StatusBadRequest)
			return
		}

		epos, err := Parse(data)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse XML: %v", err), http.StatusBadRequest)
			return
		}

		for _, inst := range epos.Instructions {
			switch inst.Type {
			case InstImage:
				if inst.Image != nil {
					err = printer.PrintGraphics(inst.Image.Data, inst.Image.Width, inst.Image.Height)
					if err != nil {
						http.Error(w, fmt.Sprintf("Failed to print image: %v", err), http.StatusInternalServerError)
						return
					}
				}
			case InstPulse:
				err = printer.KickDrawer()
				if err != nil {
					http.Error(w, fmt.Sprintf("Failed to kick drawer: %v", err), http.StatusInternalServerError)
					return
				}
			case InstCut:
				err = printer.Cut()
				if err != nil {
					http.Error(w, fmt.Sprintf("Failed to cut: %v", err), http.StatusInternalServerError)
					return
				}
			}
		}

		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		response := `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
<s:Body>
<response success="true" code="" status="123456" battery="0"/>
</s:Body>
</s:Envelope>`
		w.Write([]byte(response))
	})

	addr := *host + ":" + *port
	fmt.Printf("Starting server on %s (secure=%v)\n", addr, *secure)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down gracefully...")
		if err := printer.Close(); err != nil {
			log.Printf("Error closing printer connection: %v", err)
		}
		os.Exit(0)
	}()

	if *secure {
		if err := generateCert(); err != nil {
			panic(err)
		}
		if err := http.ListenAndServeTLS(addr, "server.crt", "server.key", nil); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	} else {
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}
}
