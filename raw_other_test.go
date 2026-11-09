//go:build !windows

package main

import (
	"strings"
	"testing"
)

func TestRawPrintingUnsupportedOutsideWindows(t *testing.T) {
	_, err := NewPrinter("Receipt RAW", 576, WindowsRaw)
	if err == nil || !strings.Contains(err.Error(), "only supported on Windows") {
		t.Fatalf("expected clear unsupported-platform error, got %v", err)
	}
}
