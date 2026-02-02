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
	retryDelay        time.Duration
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

	log.Printf("[RETRY] Starting operation with max %d retries", maxRetries)

	for i := range maxRetries {
		log.Printf("[RETRY] Attempt %d/%d: Executing operation...", i+1, maxRetries)
		result, err = fn()
		if err == nil {
			if i == 0 {
				log.Printf("[RETRY] Operation succeeded on first attempt")
			} else {
				log.Printf("[RETRY] Operation succeeded after %d retry attempt(s)", i)
			}
			return result, nil
		}

		log.Printf("[RETRY] Attempt %d/%d FAILED: %v", i+1, maxRetries, err)
		log.Printf("[RETRY] Closing connection before reconnect...")

		if p.connection != nil {
			if closeErr := p.connection.Close(); closeErr != nil {
				log.Printf("[RETRY] WARNING: Failed to close connection during retry: %v", closeErr)
			} else {
				log.Printf("[RETRY] Connection closed successfully")
			}
		} else {
			log.Printf("[RETRY] WARNING: Connection is nil, cannot close")
		}

		if i < maxRetries-1 {
			log.Printf("[RETRY] Waiting %v before retry attempt %d/%d...", p.retryDelay, i+2, maxRetries)
			time.Sleep(p.retryDelay)

			log.Printf("[RETRY] Attempting to reopen connection...")
			if openErr := p.connection.Open(); openErr != nil {
				log.Printf("[RETRY] ERROR: Failed to reopen connection: %v", openErr)
				err = openErr
				continue
			}
			log.Printf("[RETRY] Connection reopened successfully, will retry operation")
		}
	}

	log.Printf("[RETRY] Operation failed after %d attempts: %v", maxRetries, err)
	return result, err
}

func NewPrinter(connection_string string, receipt_width int, con_type ConnectionType) (*Printer, error) {
	log.Printf("[PRINTER] Creating new printer instance:")
	log.Printf("[PRINTER]   Connection string: %s", connection_string)
	log.Printf("[PRINTER]   Receipt width: %d pixels", receipt_width)
	log.Printf("[PRINTER]   Connection type: %v", con_type)

	p := &Printer{
		connection_string: connection_string,
		receipt_width:     receipt_width,
		retryDelay:        2 * time.Second,
	}

	switch con_type {
	case TcpSocket:
		p.connection = &TcpWriter{address: connection_string}
		log.Printf("[PRINTER] Created TCP writer for address: %s", connection_string)
	case UsbPath:
		p.connection = &UsbWriter{path: connection_string}
		log.Printf("[PRINTER] Created USB writer for path: %s", connection_string)
	}

	log.Printf("[PRINTER] Establishing initial connection...")
	for {
		err := p.connection.Open()
		if err == nil {
			log.Printf("[PRINTER] Successfully connected to printer: %s", connection_string)
			return p, nil
		}

		log.Printf("[PRINTER] Initial connection attempt failed: %v", err)
		log.Printf("[PRINTER] Retrying in 2 seconds...")
		time.Sleep(2 * time.Second)
	}
}

func (p *Printer) PrintGraphics(data []byte, width int, height int) error {
	log.Printf("[PRINTER] PrintGraphics called: width=%d, height=%d, data_size=%d bytes", width, height, len(data))

	width_bytes := width / 8
	required_bytes := width_bytes * height
	log.Printf("[PRINTER] Calculated: width_bytes=%d, required_bytes=%d", width_bytes, required_bytes)

	if len(data) < required_bytes {
		log.Printf("[PRINTER] ERROR: Image data too short: got %d bytes, need %d bytes", len(data), required_bytes)
		return fmt.Errorf("data too short: got %d bytes, need %d bytes", len(data), required_bytes)
	}

	raster_data := data[:required_bytes]
	paper_width_bytes := p.receipt_width / 8
	log.Printf("[PRINTER] Paper width: %d pixels (%d bytes)", p.receipt_width, paper_width_bytes)

	if width < p.receipt_width {
		log.Printf("[PRINTER] Image width (%d) < paper width (%d), centering image...", width, p.receipt_width)
		centered, err := center(raster_data, width_bytes, paper_width_bytes, height)
		if err != nil {
			log.Printf("[PRINTER] ERROR: Centering failed: %v", err)
			return err
		}
		raster_data = centered
		width = p.receipt_width
		width_bytes = paper_width_bytes
		log.Printf("[PRINTER] Image centered successfully, new width: %d bytes", width_bytes)
	} else {
		log.Printf("[PRINTER] Image width (%d) >= paper width (%d), no centering needed", width, p.receipt_width)
	}

	xL := byte(width_bytes & 0xFF)
	xH := byte((width_bytes >> 8) & 0xFF)
	yL := byte(height & 0xFF)
	yH := byte((height >> 8) & 0xff)
	log.Printf("[PRINTER] Raster command parameters: xL=%d, xH=%d, yL=%d, yH=%d", xL, xH, yL, yH)

	totalLen := 4 + 4 + len(raster_data) + 3
	buf := make([]byte, 0, totalLen)

	buf = append(buf, PRINT_RASTER_CMD...)
	buf = append(buf, xL, xH, yL, yH)
	buf = append(buf, raster_data...)
	buf = append(buf, FEED_N_CMD(12)...)
	log.Printf("[PRINTER] Command buffer prepared: %d bytes total", len(buf))

	log.Printf("[PRINTER] Sending raster print command with retry...")
	_, err := withRetry(p, 3, func() (any, error) {
		return nil, p.connection.WriteRaw(buf)
	})

	if err != nil {
		log.Printf("[PRINTER] ERROR: PrintGraphics failed after retries: %v", err)
	} else {
		log.Printf("[PRINTER] PrintGraphics completed successfully")
	}

	return err
}

func (p *Printer) KickDrawer() error {
	log.Printf("[PRINTER] KickDrawer called - sending drawer kick command")

	_, err := withRetry(p, 8, func() (any, error) {
		var KICK_CMD = []byte{0x1B, 0x70, 0, 25, 25}
		log.Printf("[PRINTER] Sending drawer kick command (ESC p 0 25 25)")
		err := p.connection.WriteRaw(KICK_CMD)
		if err != nil {
			return nil, err
		}
		return nil, nil
	})

	if err != nil {
		log.Printf("[PRINTER] ERROR: KickDrawer failed: %v", err)
	} else {
		log.Printf("[PRINTER] KickDrawer completed successfully")
	}

	return err
}

func (p *Printer) Cut() error {
	log.Printf("[PRINTER] Cut called - executing paper cut sequence")

	_, err := withRetry(p, 8, func() (any, error) {
		log.Printf("[PRINTER] Sending cut command (GS V 0)")
		err := p.connection.WriteRaw(CUT_CMD)
		if err != nil {
			return nil, fmt.Errorf("cut command failed: %w", err)
		}
		log.Printf("[PRINTER] Cut command sent successfully")

		log.Printf("[PRINTER] Sending feed command (ESC d 2)")
		err = p.connection.WriteRaw(FEED_N_CMD(2))
		if err != nil {
			return nil, fmt.Errorf("feed command failed: %w", err)
		}
		log.Printf("[PRINTER] Feed command sent successfully")

		return nil, nil
	})

	if err != nil {
		log.Printf("[PRINTER] ERROR: Cut operation failed: %v", err)
	} else {
		log.Printf("[PRINTER] Cut operation completed successfully")
	}

	return err
}

func center(d []byte, w_b int, p_w_b int, h int) ([]byte, error) {
	log.Printf("[CENTER] Centering image data: image_width_bytes=%d, paper_width_bytes=%d, height=%d", w_b, p_w_b, h)

	expected_len := w_b * h
	if len(d) < expected_len {
		err := fmt.Errorf("center: data too short: got %d bytes, need %d bytes", len(d), expected_len)
		log.Printf("[CENTER] ERROR: %v", err)
		return nil, err
	}

	padding_bytes := (p_w_b - w_b) / 2
	log.Printf("[CENTER] Calculated padding: %d bytes per side", padding_bytes)

	centered_data := []byte{}
	for y := range h {
		row_start := y * w_b
		row_end := row_start + w_b

		if row_end > len(d) {
			err := fmt.Errorf("center: row out of bounds at y=%d", y)
			log.Printf("[CENTER] ERROR: %v", err)
			return nil, err
		}

		row_data := d[row_start:row_end]

		// Add left padding
		if padding_bytes > 0 {
			centered_data = append(centered_data, make([]byte, padding_bytes)...)
		}

		// Add image data
		centered_data = append(centered_data, row_data...)

		// Add right padding
		right_padding := p_w_b - w_b - padding_bytes
		if right_padding > 0 {
			centered_data = append(centered_data, make([]byte, right_padding)...)
		}

	}

	log.Printf("[CENTER] Centering complete: input=%d bytes, output=%d bytes", len(d), len(centered_data))
	return centered_data, nil
}

func (p *Printer) Close() error {
	log.Printf("[PRINTER] Close called for printer: %s", p.connection_string)

	if p.connection == nil {
		log.Printf("[PRINTER] WARNING: Connection is nil, nothing to close")
		return nil
	}

	err := p.connection.Close()
	if err != nil {
		log.Printf("[PRINTER] ERROR: Failed to close connection: %v", err)
		return err
	}

	log.Printf("[PRINTER] Printer connection closed successfully")
	return nil
}
