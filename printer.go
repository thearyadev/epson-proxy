package main

import (
	"fmt"
	"log"
	"time"
)

type Printer struct {
	connection_string string
	receipt_width     int
	connection        Writable
}

type ConnectionType int

const (
	TcpSocket ConnectionType = iota
	UsbPath
)

var CUT_CMD = []byte{0x1d, 'V', 0x00}
var PRINT_RASTER_CMD = []byte{0x1d, 0x76, 0x30, 0x00}
var FEED_N_CMD = func(n int) []byte {
	return []byte{0x1b, 0x64, byte(n)}
}

func withRetry[T any](p *Printer, maxRetries int, fn func() (T, error)) (T, error) {
	var result T
	var err error
	for i := range maxRetries {
		result, err = fn()
		if err == nil {
			return result, nil
		}

		log.Printf("Operation failed (attempt %d/%d): %v. Reconnecting...", i+1, maxRetries, err)

		if p.connection != nil {
			if closeErr := p.connection.Close(); closeErr != nil {
				log.Printf("Failed to close connection: %v", closeErr)
			}
		}

		time.Sleep(2 * time.Second)

		if openErr := p.connection.Open(); openErr != nil {
			log.Printf("Failed to reopen connection: %v", openErr)
			err = openErr
			continue
		}

		log.Printf("Connection reopened successfully")
	}

	return result, err
}

func NewPrinter(connection_string string, receipt_width int, con_type ConnectionType) (*Printer, error) {
	p := &Printer{connection_string: connection_string, receipt_width: receipt_width}

	switch con_type {
	case TcpSocket:
		p.connection = &TcpWriter{address: connection_string}
	case UsbPath:
		p.connection = &UsbWriter{path: connection_string}
	}

	for {
		err := p.connection.Open()
		if err == nil {
			log.Println("Connected to printer:", connection_string)
			return p, nil
		}

		log.Printf("Initial connection failed: %v. Retrying in 2 seconds...", err)
		time.Sleep(2 * time.Second)
	}
}

func (p *Printer) PrintGraphics(data []byte, width int, height int) error {
	width_bytes := width / 8
	required_bytes := width_bytes * height

	if len(data) < required_bytes {
		return fmt.Errorf("data too short: got %d bytes, need %d bytes", len(data), required_bytes)
	}

	raster_data := data[:required_bytes]
	paper_width_bytes := p.receipt_width / 8
	if width < p.receipt_width {
		centered, err := center(raster_data, width_bytes, paper_width_bytes, height)
		if err != nil {
			return err
		}
		raster_data = centered
		width = p.receipt_width
		width_bytes = paper_width_bytes
	}

	xL := byte(width_bytes & 0xFF)
	xH := byte((width_bytes >> 8) & 0xFF)
	yL := byte(height & 0xFF)
	yH := byte((height >> 8) & 0xff)

	totalLen := 4 + 4 + len(raster_data) + 3
	buf := make([]byte, 0, totalLen)

	buf = append(buf, PRINT_RASTER_CMD...)
	buf = append(buf, xL, xH, yL, yH)
	buf = append(buf, raster_data...)
	buf = append(buf, FEED_N_CMD(12)...)

	_, err := withRetry(p, 3, func() (any, error) {
		return nil, p.connection.WriteRaw(buf)
	})
	return err
}

func (p *Printer) KickDrawer() error {
	_, err := withRetry(p, 8, func() (any, error) {
		var KICK_CMD = []byte{0x1B, 0x70, 0, 25, 25}
		err := p.connection.WriteRaw(KICK_CMD)
		if err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func (p *Printer) Cut() error {
	_, err := withRetry(p, 8, func() (any, error) {
		err := p.connection.WriteRaw(CUT_CMD)
		if err != nil {
			return nil, err
		}
		err = p.connection.WriteRaw(FEED_N_CMD(2))
		if err != nil {
			return nil, err
		}
		return nil, nil
	})
	return err
}

func center(d []byte, w_b int, p_w_b int, h int) ([]byte, error) {
	expected_len := w_b * h
	if len(d) < expected_len {
		err := fmt.Errorf("center: data too short: got %d bytes, need %d bytes", len(d), expected_len)
		log.Printf("center error: %v", err)
		return nil, err
	}

	padding_bytes := (p_w_b - w_b) / 2
	centered_data := []byte{}
	for y := range h {
		row_start := y * w_b
		row_end := row_start + w_b

		if row_end > len(d) {
			err := fmt.Errorf("center: row out of bounds at y=%d", y)
			log.Printf("center error: %v", err)
			return nil, err
		}

		row_data := d[row_start:row_end]
		centered_data = append(centered_data, make([]byte, padding_bytes)...)
		centered_data = append(centered_data, row_data...)
		centered_data = append(centered_data, make([]byte, (p_w_b-w_b-padding_bytes))...)
	}
	return centered_data, nil
}

func (p *Printer) Close() error {
	if p.connection != nil {
		return p.connection.Close()
	}
	return nil
}
