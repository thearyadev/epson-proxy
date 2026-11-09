package main

import (
	"fmt"
	"log"
	"sync"
)

const defaultJobName = "Epson Proxy"

// rawPrinter is one open Windows printer handle. Keeping the job lifecycle here
// lets it be tested without a Windows spooler or a physical printer.
type rawPrinter interface {
	StartDoc(title string) (uint32, error)
	StartPage() error
	Write(data []byte) (uint32, error)
	EndPage() error
	EndDoc() error
	Abort() error
	Close() error
}

// RawWriter submits printer-ready ESC/POS bytes as Windows RAW spooler jobs.
// Each WriteRaw is one complete job and must never be automatically retried:
// even a failed call may already have produced output.
type RawWriter struct {
	mu          sync.Mutex
	printer     string
	title       string
	openPrinter func(string) (rawPrinter, error)
	device      rawPrinter
}

func (r *RawWriter) Open() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.device != nil {
		return nil
	}
	device, err := r.openPrinter(r.printer)
	if err != nil {
		return fmt.Errorf("open RAW printer %q: %w", r.printer, err)
	}
	r.device = device
	return nil
}

func (r *RawWriter) WriteRaw(data []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(data) == 0 {
		return fmt.Errorf("empty RAW print job")
	}
	if r.device == nil {
		return fmt.Errorf("RAW printer %q is not open", r.printer)
	}

	jobID, err := r.device.StartDoc(r.title)
	if err != nil {
		return fmt.Errorf("StartDocPrinterW: %w", err)
	}
	finished := false
	defer func() {
		if !finished {
			if err := r.device.Abort(); err != nil {
				log.Printf("[RAW] Failed to abort job %d: %v", jobID, err)
			}
		}
	}()
	if err := r.device.StartPage(); err != nil {
		return fmt.Errorf("StartPagePrinter (job %d): %w", jobID, err)
	}

	// WritePrinter takes a DWORD length and may accept only part of a chunk.
	for offset := 0; offset < len(data); {
		n := min(len(data)-offset, 64*1024)
		written, err := r.device.Write(data[offset : offset+n])
		if err != nil {
			return fmt.Errorf("WritePrinter (job %d, offset %d): %w", jobID, offset, err)
		}
		if written == 0 || written > uint32(n) {
			return fmt.Errorf("WritePrinter (job %d, offset %d): invalid byte count %d", jobID, offset, written)
		}
		offset += int(written)
	}
	if err := r.device.EndPage(); err != nil {
		return fmt.Errorf("EndPagePrinter (job %d): %w", jobID, err)
	}
	if err := r.device.EndDoc(); err != nil {
		return fmt.Errorf("EndDocPrinter (job %d): %w", jobID, err)
	}
	finished = true
	log.Printf("[RAW] Spooler accepted job %d: %d bytes to %q", jobID, len(data), r.printer)
	return nil
}

func (r *RawWriter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.device == nil {
		return nil
	}
	err := r.device.Close()
	r.device = nil
	return err
}
