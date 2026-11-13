package main

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

var errSpooler = errors.New("spooler failure")

type fakeRawPrinter struct {
	events       []string
	title        string
	failAt       string
	writeLimit   int
	invalidCount *uint32
	chunkSizes   []int
	data         bytes.Buffer
	jobs         [][]byte
}

func (f *fakeRawPrinter) step(name string) error {
	f.events = append(f.events, name)
	if f.failAt == name {
		return errSpooler
	}
	return nil
}

func (f *fakeRawPrinter) StartDoc(title string) (uint32, error) {
	f.title = title
	f.data.Reset()
	return 42, f.step("startDoc")
}

func (f *fakeRawPrinter) StartPage() error { return f.step("startPage") }

func (f *fakeRawPrinter) Write(data []byte) (uint32, error) {
	f.chunkSizes = append(f.chunkSizes, len(data))
	if err := f.step("write"); err != nil {
		return 0, err
	}
	if f.invalidCount != nil {
		return *f.invalidCount, nil
	}
	n := len(data)
	if f.writeLimit > 0 {
		n = min(n, f.writeLimit)
	}
	f.data.Write(data[:n])
	return uint32(n), nil
}

func (f *fakeRawPrinter) EndPage() error { return f.step("endPage") }
func (f *fakeRawPrinter) EndDoc() error {
	if err := f.step("endDoc"); err != nil {
		return err
	}
	f.jobs = append(f.jobs, bytes.Clone(f.data.Bytes()))
	return nil
}
func (f *fakeRawPrinter) Abort() error { return f.step("abort") }
func (f *fakeRawPrinter) Close() error { return f.step("close") }

func testRawWriter(t *testing.T, device *fakeRawPrinter) *RawWriter {
	t.Helper()
	r := &RawWriter{
		printer: "Receipt RAW", title: "Receipt test",
		openPrinter: func(name string) (rawPrinter, error) {
			if name != "Receipt RAW" {
				t.Errorf("unexpected printer name: %q", name)
			}
			return device, nil
		},
	}
	if err := r.Open(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestRawWriterJobLifecycle(t *testing.T) {
	device := &fakeRawPrinter{}
	r := testRawWriter(t, device)
	data := []byte{0x1b, 0x40, 0x00, 0xff, 0x1d, 0x56, 0x00}
	if err := r.WriteRaw(data); err != nil {
		t.Fatal(err)
	}
	want := []string{"startDoc", "startPage", "write", "endPage", "endDoc"}
	if !reflect.DeepEqual(device.events, want) {
		t.Fatalf("job lifecycle = %v, want %v", device.events, want)
	}
	if device.title != r.title || len(device.jobs) != 1 || !bytes.Equal(device.jobs[0], data) {
		t.Fatalf("job title or printer-ready bytes changed: title=%q, jobs=%x", device.title, device.jobs)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if len(device.events) != len(want)+1 || device.events[len(want)] != "close" {
		t.Fatalf("handle should be closed once: %v", device.events)
	}
	if err := r.WriteRaw(data); err == nil {
		t.Fatal("write after close should fail")
	}
}

func TestRawWriterHandlesPartialWritesAndChunkLimit(t *testing.T) {
	device := &fakeRawPrinter{writeLimit: 11000}
	r := testRawWriter(t, device)
	data := bytes.Repeat([]byte{0x00, 0x80, 0xff}, 50000)
	if err := r.WriteRaw(data); err != nil {
		t.Fatal(err)
	}
	if len(device.jobs) != 1 || !bytes.Equal(device.jobs[0], data) {
		t.Fatal("partial writes lost or duplicated bytes")
	}
	if device.chunkSizes[0] != 64*1024 {
		t.Fatalf("first chunk = %d, want 65536", device.chunkSizes[0])
	}
	for _, n := range device.chunkSizes {
		if n <= 0 || n > 64*1024 {
			t.Fatalf("invalid WritePrinter chunk length: %d", n)
		}
	}
}

func TestRawWriterAbortsFailedJobs(t *testing.T) {
	for _, step := range []string{"startDoc", "startPage", "write", "endPage", "endDoc"} {
		t.Run(step, func(t *testing.T) {
			device := &fakeRawPrinter{failAt: step}
			r := testRawWriter(t, device)
			err := r.WriteRaw([]byte{0xff})
			if !errors.Is(err, errSpooler) {
				t.Fatalf("error = %v, want spooler failure", err)
			}
			if step == "startDoc" {
				if !reflect.DeepEqual(device.events, []string{"startDoc"}) {
					t.Fatalf("should not abort a job that never started: %v", device.events)
				}
			} else if device.events[len(device.events)-1] != "abort" || !strings.Contains(err.Error(), "job 42") {
				t.Fatalf("failed job must be identified and aborted: events=%v, error=%v", device.events, err)
			}
			if len(device.jobs) != 0 {
				t.Fatal("failed job reported as completed")
			}
			// A later explicit request may use the handle after the aborted job.
			device.failAt = ""
			if err := r.WriteRaw([]byte{0x55}); err != nil {
				t.Fatalf("next job failed: %v", err)
			}
		})
	}
}

func TestRawWriterRejectsInvalidWriteCounts(t *testing.T) {
	for _, count := range []uint32{0, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			device := &fakeRawPrinter{invalidCount: &count}
			r := testRawWriter(t, device)
			if err := r.WriteRaw([]byte{0xff}); err == nil || !strings.Contains(err.Error(), "invalid byte count") {
				t.Fatalf("error = %v, want invalid byte count", err)
			}
			if device.events[len(device.events)-1] != "abort" {
				t.Fatalf("invalid write count did not abort job: %v", device.events)
			}
		})
	}
}

func TestRawWriterRejectsEmptyJob(t *testing.T) {
	device := &fakeRawPrinter{}
	r := testRawWriter(t, device)
	if err := r.WriteRaw(nil); err == nil || len(device.events) != 0 {
		t.Fatalf("empty job should fail without using spooler: events=%v, error=%v", device.events, err)
	}
}

func TestRawWriterOpenFailure(t *testing.T) {
	r := &RawWriter{printer: "Missing queue", openPrinter: func(string) (rawPrinter, error) {
		return nil, errSpooler
	}}
	if err := r.Open(); !errors.Is(err, errSpooler) {
		t.Fatalf("Open error = %v, want spooler failure", err)
	}
	if err := r.WriteRaw([]byte{0xff}); err == nil {
		t.Fatal("write without an open handle should fail")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRawPrinterOperationsNeverRetry(t *testing.T) {
	for _, operation := range []string{"graphics", "drawer", "cut", "reset", "receipt"} {
		t.Run(operation, func(t *testing.T) {
			device := &fakeRawPrinter{failAt: "write"}
			r := testRawWriter(t, device)
			p := &Printer{connection: r, receipt_width: 8}
			var err error
			switch operation {
			case "graphics":
				err = p.PrintGraphics([]byte{0xff}, 8, 1)
			case "drawer":
				err = p.KickDrawer()
			case "cut":
				err = p.Cut()
			case "reset":
				err = p.Reset()
			case "receipt":
				err = p.PrintReceipt(&EposPrint{Instructions: []Instruction{{Type: InstCut}}})
			}
			if !errors.Is(err, errSpooler) {
				t.Fatalf("error = %v, want spooler failure", err)
			}
			want := []string{"startDoc", "startPage", "write", "abort"}
			if !reflect.DeepEqual(device.events, want) {
				t.Fatalf("failed RAW operation was retried: %v", device.events)
			}
		})
	}
}

func TestRawReceiptIsOneOrderedJob(t *testing.T) {
	device := &fakeRawPrinter{}
	p := &Printer{connection: testRawWriter(t, device), receipt_width: 8}
	receipt := &EposPrint{Instructions: []Instruction{
		{Type: InstImage, Image: &ImageDecoded{Width: 8, Height: 1, Data: []byte{0xa5}}},
		{Type: InstImage, Image: &ImageDecoded{Width: 8, Height: 1, Data: []byte{0x5a}}},
		{Type: InstPulse}, {Type: InstCut},
	}}
	if err := p.PrintReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x1d, 0x76, 0x30, 0x00, 1, 0, 1, 0, 0xa5, // Raster header and pixels.
		0x1d, 0x76, 0x30, 0x00, 1, 0, 1, 0, 0x5a, // Next image without blank feed lines.
		0x1b, 0x70, 0, 25, 25, // Drawer pulse.
		0x1d, 0x56, 65, 0, // Feed to cutter and cut, without extra margin.
		0x1b, 0x40, // Final reset.
	}
	if len(device.jobs) != 1 || !bytes.Equal(device.jobs[0], want) {
		t.Fatalf("receipt should be one ordered job: got %x, want %x", device.jobs, want)
	}
}

func TestInvalidRawReceiptDoesNotSpoolEarlierInstructions(t *testing.T) {
	device := &fakeRawPrinter{}
	p := &Printer{connection: testRawWriter(t, device), receipt_width: 8}
	receipt := &EposPrint{Instructions: []Instruction{
		{Type: InstPulse},
		{Type: InstImage, Image: &ImageDecoded{Width: 8, Height: 2, Data: []byte{0xff}}},
	}}
	if err := p.PrintReceipt(receipt); err == nil {
		t.Fatal("expected invalid raster error")
	}
	if len(device.events) != 0 {
		t.Fatalf("invalid receipt reached spooler: %v", device.events)
	}
}

func TestConcurrentRawReceiptsStaySeparate(t *testing.T) {
	device := &fakeRawPrinter{}
	p := &Printer{connection: testRawWriter(t, device), receipt_width: 8}
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			receipt := &EposPrint{Instructions: []Instruction{
				{Type: InstImage, Image: &ImageDecoded{Width: 8, Height: 1, Data: []byte{byte(i)}}},
				{Type: InstCut},
			}}
			if err := p.PrintReceipt(receipt); err != nil {
				t.Errorf("receipt %d: %v", i, err)
			}
		})
	}
	wg.Wait()
	if len(device.jobs) != 10 {
		t.Fatalf("got %d jobs, want 10", len(device.jobs))
	}
	seen := make(map[byte]bool)
	for _, data := range device.jobs {
		if len(data) != 15 {
			t.Fatalf("receipt commands interleaved: %x", data)
		}
		seen[data[8]] = true
	}
	if len(seen) != 10 {
		t.Fatal("concurrent receipts were duplicated or lost")
	}
}
