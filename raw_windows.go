//go:build windows

package main

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	spooler      = syscall.NewLazyDLL("winspool.drv")
	openPrinter  = spooler.NewProc("OpenPrinterW")
	startDoc     = spooler.NewProc("StartDocPrinterW")
	startPage    = spooler.NewProc("StartPagePrinter")
	writePrinter = spooler.NewProc("WritePrinter")
	endPage      = spooler.NewProc("EndPagePrinter")
	endDoc       = spooler.NewProc("EndDocPrinter")
	abortPrinter = spooler.NewProc("AbortPrinter")
	closePrinter = spooler.NewProc("ClosePrinter")
)

// DOC_INFO_1W contains three pointer-sized fields on both 32- and 64-bit Windows.
type docInfo1 struct {
	docName    *uint16
	outputFile *uint16
	datatype   *uint16
}

type windowsRawPrinter struct {
	handle syscall.Handle
}

func newRawWriter(printer, title string) (*RawWriter, error) {
	if printer == "" {
		return nil, fmt.Errorf("RAW printer queue name is required")
	}
	if _, err := syscall.UTF16PtrFromString(printer); err != nil {
		return nil, fmt.Errorf("printer name: %w", err)
	}
	if _, err := syscall.UTF16PtrFromString(title); err != nil {
		return nil, fmt.Errorf("document title: %w", err)
	}
	return &RawWriter{printer: printer, title: title, openPrinter: openRawPrinter}, nil
}

func openRawPrinter(printer string) (rawPrinter, error) {
	name, err := syscall.UTF16PtrFromString(printer)
	if err != nil {
		return nil, err
	}
	var handle syscall.Handle
	ok, _, callErr := openPrinter.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&handle)), 0)
	runtime.KeepAlive(name)
	if ok == 0 {
		return nil, fmt.Errorf("OpenPrinterW: %w", callErr)
	}
	return &windowsRawPrinter{handle: handle}, nil
}

func (p *windowsRawPrinter) StartDoc(title string) (uint32, error) {
	name, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return 0, err
	}
	raw, _ := syscall.UTF16PtrFromString("RAW")
	info := docInfo1{docName: name, datatype: raw}
	id, _, callErr := startDoc.Call(uintptr(p.handle), 1, uintptr(unsafe.Pointer(&info)))
	runtime.KeepAlive(info)
	if id == 0 {
		return 0, callErr
	}
	return uint32(id), nil
}

func (p *windowsRawPrinter) StartPage() error {
	ok, _, err := startPage.Call(uintptr(p.handle))
	return spoolerResult(ok, err)
}

func (p *windowsRawPrinter) Write(data []byte) (uint32, error) {
	var written uint32
	ok, _, err := writePrinter.Call(uintptr(p.handle), uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)), uintptr(unsafe.Pointer(&written)))
	runtime.KeepAlive(data)
	return written, spoolerResult(ok, err)
}

func (p *windowsRawPrinter) EndPage() error {
	ok, _, err := endPage.Call(uintptr(p.handle))
	return spoolerResult(ok, err)
}

func (p *windowsRawPrinter) EndDoc() error {
	ok, _, err := endDoc.Call(uintptr(p.handle))
	return spoolerResult(ok, err)
}

func (p *windowsRawPrinter) Abort() error {
	ok, _, err := abortPrinter.Call(uintptr(p.handle))
	return spoolerResult(ok, err)
}

func (p *windowsRawPrinter) Close() error {
	ok, _, err := closePrinter.Call(uintptr(p.handle))
	if err := spoolerResult(ok, err); err != nil {
		return fmt.Errorf("ClosePrinter: %w", err)
	}
	return nil
}

func spoolerResult(ok uintptr, err error) error {
	if ok == 0 {
		return err
	}
	return nil
}
