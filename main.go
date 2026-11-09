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
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Version is set at build time via ldflags
var Version = "dev"

func generateCert() error {
	certPath := "server.crt"
	keyPath := "server.key"

	log.Printf("[SSL] Checking for existing SSL certificates: cert=%s, key=%s", certPath, keyPath)

	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			log.Printf("[SSL] Existing certificates found, skipping generation")
			return nil
		}
		log.Printf("[SSL] Certificate file exists but key file missing: %s", keyPath)
	} else {
		log.Printf("[SSL] Certificate file not found: %s", certPath)
	}

	log.Printf("[SSL] Generating new self-signed SSL certificate...")
	log.Printf("[SSL] Using ECDSA P-256 curve for key generation")

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %v", err)
	}
	log.Printf("[SSL] Private key generated successfully")

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
	log.Printf("[SSL] Certificate template created: valid from %v to %v", template.NotBefore, template.NotAfter)

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("failed to create certificate: %v", err)
	}
	log.Printf("[SSL] Certificate DER encoded successfully")

	certFile, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("failed to create cert file: %v", err)
	}
	defer certFile.Close()
	pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	log.Printf("[SSL] Certificate written to: %s", certPath)

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
	log.Printf("[SSL] Private key written to: %s", keyPath)

	log.Printf("[SSL] SSL certificate generation completed successfully")
	return nil
}

// isOriginAllowed checks if the given origin is in the whitelist
func isOriginAllowed(origin string, allowedOrigins []string) bool {
	if len(allowedOrigins) == 0 {
		// No whitelist configured, allow all
		return true
	}

	for _, allowed := range allowedOrigins {
		if strings.EqualFold(origin, allowed) {
			return true
		}
	}
	return false
}

func main() {
	var (
		printerConn    = flag.String("printer", "", "Printer device path, TCP address, or Windows printer queue name (required)")
		receiptWidth   = flag.Int("receipt-width", 576, "Receipt width in pixels")
		proto          = flag.String("proto", "", "Protocol: USB, TCP, or RAW (Windows only) (required)")
		jobName        = flag.String("job-name", defaultJobName, "Document title for Windows RAW print jobs")
		host           = flag.String("host", "127.0.0.1", "Server host")
		port           = flag.String("port", "8000", "Server port")
		secure         = flag.Bool("secure", false, "Use HTTPS")
		allowedOrigins = flag.String("allow-origins", "", "Comma-separated list of allowed CORS origins (empty = allow all)")
		version        = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *version {
		fmt.Println(Version)
		os.Exit(0)
	}

	log.Printf("[MAIN] Epson Proxy Server starting up...")

	// Parse allowed origins
	var originsList []string
	if *allowedOrigins != "" {
		originsList = strings.Split(*allowedOrigins, ",")
		// Trim whitespace from each origin
		for i, origin := range originsList {
			originsList[i] = strings.TrimSpace(origin)
		}
		log.Printf("[MAIN] CORS whitelist configured: %v", originsList)
	} else {
		log.Printf("[MAIN] CORS whitelist not configured - allowing all origins")
	}

	log.Printf("[MAIN] Command-line arguments parsed:")
	log.Printf("[MAIN]   -printer: %s", *printerConn)
	log.Printf("[MAIN]   -receipt-width: %d pixels", *receiptWidth)
	log.Printf("[MAIN]   -proto: %s", *proto)
	log.Printf("[MAIN]   -job-name: %s", *jobName)
	log.Printf("[MAIN]   -host: %s", *host)
	log.Printf("[MAIN]   -port: %s", *port)
	log.Printf("[MAIN]   -secure: %v", *secure)
	log.Printf("[MAIN]   -allow-origins: %s", *allowedOrigins)

	if *printerConn == "" {
		log.Printf("[MAIN] ERROR: Required flag -printer not provided")
		fmt.Fprintf(os.Stderr, "Error: -printer flag is required\n")
		flag.Usage()
		os.Exit(1)
	}

	if *proto == "" {
		log.Printf("[MAIN] ERROR: Required flag -proto not provided")
		fmt.Fprintf(os.Stderr, "Error: -proto flag is required\n")
		flag.Usage()
		os.Exit(1)
	}

	var connType ConnectionType
	switch strings.ToUpper(*proto) {
	case "TCP":
		connType = TcpSocket
		log.Printf("[MAIN] Selected protocol: TCP (TcpSocket)")
	case "USB":
		connType = UsbPath
		log.Printf("[MAIN] Selected protocol: USB (UsbPath)")
	case "RAW":
		connType = WindowsRaw
		log.Printf("[MAIN] Selected protocol: RAW (Windows spooler)")
	default:
		log.Printf("[MAIN] ERROR: Unknown protocol specified: %s", *proto)
		fmt.Fprintf(os.Stderr, "Unknown protocol: %s (must be USB, TCP, or RAW)\n", *proto)
		os.Exit(1)
	}

	log.Printf("[MAIN] Initializing printer connection to: %s", *printerConn)
	printer, err := NewPrinter(*printerConn, *receiptWidth, connType, WithJobName(*jobName))
	if err != nil {
		log.Fatalf("[MAIN] FATAL: Failed to connect to printer: %v", err)
	}
	defer func() {
		log.Printf("[MAIN] Shutting down: closing printer connection")
		if err := printer.Close(); err != nil {
			log.Printf("[MAIN] ERROR: Failed to close printer connection: %v", err)
		} else {
			log.Printf("[MAIN] Printer connection closed successfully")
		}
	}()
	log.Printf("[MAIN] Printer connected successfully: %s", printer.connection_string)

	var requestCounter atomic.Uint64
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		requestCount := requestCounter.Add(1)
		origin := r.Header.Get("Origin")
		log.Printf("[HTTP] Request #%d received: %s %s from %s", requestCount, r.Method, r.URL.Path, r.RemoteAddr)
		log.Printf("[HTTP]   Content-Type: %s", r.Header.Get("Content-Type"))
		log.Printf("[HTTP]   Content-Length: %d", r.ContentLength)
		log.Printf("[HTTP]   Origin: %s", origin)

		// Check CORS origin
		if !isOriginAllowed(origin, originsList) {
			log.Printf("[HTTP] Request #%d: CORS blocked - origin '%s' not in whitelist", requestCount, origin)
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, "CORS Error: Origin '%s' is not allowed\n", origin)
			return
		}

		// Set CORS headers for allowed origin
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			log.Printf("[HTTP] Request #%d: CORS preflight request, returning 200", requestCount)
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method != http.MethodPost {
			log.Printf("[HTTP] Request #%d: Method not allowed: %s", requestCount, r.Method)
			http.Error(w, "Only POST requests are allowed", http.StatusMethodNotAllowed)
			return
		}

		data, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("[HTTP] Request #%d: ERROR reading request body: %v", requestCount, err)
			http.Error(w, fmt.Sprintf("Failed to read body: %v", err), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()
		log.Printf("[HTTP] Request #%d: Read %d bytes from request body", requestCount, len(data))

		if len(data) == 0 {
			log.Printf("[HTTP] Request #%d: ERROR empty request body", requestCount)
			http.Error(w, "Empty request body", http.StatusBadRequest)
			return
		}

		log.Printf("[XML] Request #%d: Parsing EPOS XML data...", requestCount)
		epos, err := Parse(data)
		if err != nil {
			log.Printf("[XML] Request #%d: ERROR parsing XML: %v", requestCount, err)
			http.Error(w, fmt.Sprintf("Failed to parse XML: %v", err), http.StatusBadRequest)
			return
		}
		log.Printf("[XML] Request #%d: XML parsed successfully, found %d instruction(s)", requestCount, len(epos.Instructions))

		if err := printer.PrintReceipt(epos); err != nil {
			log.Printf("[PRINT] Request #%d: ERROR printing receipt: %v", requestCount, err)
			http.Error(w, fmt.Sprintf("Failed to print receipt: %v", err), http.StatusInternalServerError)
			return
		}

		log.Printf("[HTTP] Request #%d: All instructions processed successfully, sending success response", requestCount)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		response := `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/">
<s:Body>
<response success="true" code="" status="123456" battery="0"/>
</s:Body>
</s:Envelope>`
		w.Write([]byte(response))
		log.Printf("[HTTP] Request #%d: Response sent (200 OK)", requestCount)
	})

	addr := *host + ":" + *port
	log.Printf("[MAIN] HTTP server configured:")
	log.Printf("[MAIN]   Listen address: %s", addr)
	log.Printf("[MAIN]   Secure (HTTPS): %v", *secure)
	log.Printf("[MAIN]   Allowed Origins: %v", originsList)

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("[MAIN] Received signal: %v", sig)
		log.Printf("[MAIN] Initiating graceful shutdown...")
		if err := printer.Close(); err != nil {
			log.Printf("[MAIN] ERROR closing printer during shutdown: %v", err)
		}
		log.Printf("[MAIN] Graceful shutdown complete")
		os.Exit(0)
	}()

	if *secure {
		log.Printf("[MAIN] Starting HTTPS server...")
		if err := generateCert(); err != nil {
			log.Fatalf("[MAIN] FATAL: Certificate generation failed: %v", err)
		}
		log.Printf("[MAIN] HTTPS server starting on %s", addr)
		if err := http.ListenAndServeTLS(addr, "server.crt", "server.key", nil); err != nil {
			log.Fatalf("[MAIN] FATAL: HTTPS server error: %v", err)
		}
	} else {
		log.Printf("[MAIN] HTTP server starting on %s", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Fatalf("[MAIN] FATAL: HTTP server error: %v", err)
		}
	}
}
