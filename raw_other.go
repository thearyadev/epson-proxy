//go:build !windows

package main

import "fmt"

func newRawWriter(printer, title string) (*RawWriter, error) {
	return nil, fmt.Errorf("RAW printing is only supported on Windows; use -proto USB or -proto TCP on this platform")
}
