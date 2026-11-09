//go:build windows

package main

import "testing"

func TestRawWriterValidatesWindowsNames(t *testing.T) {
	for _, names := range [][2]string{
		{"", "Receipt"},
		{"Receipt\x00RAW", "Receipt"},
		{"Receipt RAW", "Receipt\x00title"},
	} {
		if _, err := newRawWriter(names[0], names[1]); err == nil {
			t.Fatalf("accepted invalid queue/title: %q", names)
		}
	}
	if _, err := newRawWriter("Reçu RAW", "Reçu client"); err != nil {
		t.Fatalf("Unicode queue/title rejected: %v", err)
	}
}
