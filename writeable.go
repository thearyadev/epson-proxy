package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
)

type Writable interface {
	WriteRaw([]byte) error
	Open() error
	Close() error
}

type UsbWriter struct {
	mu     sync.Mutex
	path   string
	writer io.WriteCloser
}

type TcpWriter struct {
	mu      sync.Mutex
	address string
	conn    net.Conn
}

func (u *UsbWriter) WriteRaw(data []byte) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.writer == nil {
		log.Printf("[USB] ERROR: Write attempted but no active connection to: %s", u.path)
		return errors.New("No active connection. Reconnect")
	}

	log.Printf("[USB] Writing %d bytes to USB device: %s", len(data), u.path)
	n, err := u.writer.Write(data)
	if err != nil {
		log.Printf("[USB] ERROR: Write failed to %s: %v (wrote %d/%d bytes)", u.path, err, n, len(data))
		return err
	}

	log.Printf("[USB] Write successful: %d/%d bytes written to %s", n, len(data), u.path)
	return nil
}

func (u *UsbWriter) Open() error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.writer != nil {
		log.Printf("[USB] WARNING: Connection already open to: %s", u.path)
		return nil
	}

	log.Printf("[USB] Opening USB device: %s", u.path)
	f, err := os.OpenFile(u.path, os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		log.Printf("[USB] ERROR: Failed to open USB device %s: %v", u.path, err)
		return fmt.Errorf("failed to open USB device %s: %w", u.path, err)
	}

	u.writer = f
	log.Printf("[USB] USB device opened successfully: %s", u.path)
	return nil
}

func (u *UsbWriter) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()

	log.Printf("[USB] Closing USB connection to: %s", u.path)

	if u.writer == nil {
		log.Printf("[USB] WARNING: No active connection to close for: %s", u.path)
		return nil
	}

	err := u.writer.Close()
	u.writer = nil

	if err != nil {
		log.Printf("[USB] ERROR: Error closing USB connection %s: %v", u.path, err)
		return fmt.Errorf("error closing USB connection %s: %w", u.path, err)
	}

	log.Printf("[USB] USB connection closed successfully: %s", u.path)
	return nil
}

func (t *TcpWriter) WriteRaw(data []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn == nil {
		log.Printf("[TCP] ERROR: Write attempted but no active connection to: %s", t.address)
		return errors.New("No active connection. Reconnect")
	}

	log.Printf("[TCP] Writing %d bytes to TCP connection: %s", len(data), t.address)
	n, err := t.conn.Write(data)
	if err != nil {
		log.Printf("[TCP] ERROR: Write failed to %s: %v (wrote %d/%d bytes)", t.address, err, n, len(data))
		return err
	}

	log.Printf("[TCP] Write successful: %d/%d bytes written to %s", n, len(data), t.address)
	return nil
}

func (t *TcpWriter) Open() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn != nil {
		log.Printf("[TCP] WARNING: Connection already open to: %s", t.address)
		return nil
	}

	log.Printf("[TCP] Opening TCP connection to: %s", t.address)
	conn, err := net.Dial("tcp", t.address)
	if err != nil {
		log.Printf("[TCP] ERROR: Failed to dial %s: %v", t.address, err)
		return fmt.Errorf("failed to dial %s: %w", t.address, err)
	}

	t.conn = conn
	log.Printf("[TCP] TCP connection established successfully: %s", t.address)
	return nil
}

func (t *TcpWriter) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	log.Printf("[TCP] Closing TCP connection to: %s", t.address)

	if t.conn == nil {
		log.Printf("[TCP] WARNING: No active connection to close for: %s", t.address)
		return nil
	}

	err := t.conn.Close()
	t.conn = nil

	if err != nil {
		log.Printf("[TCP] ERROR: Error closing TCP connection %s: %v", t.address, err)
		return fmt.Errorf("error closing TCP connection %s: %w", t.address, err)
	}

	log.Printf("[TCP] TCP connection closed successfully: %s", t.address)
	return nil
}
