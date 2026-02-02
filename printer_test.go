package main

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

// MockWritable for testing error conditions
type MockWritable struct {
	WriteRawError  error
	OpenError      error
	CloseError     error
	WriteRawCalls  [][]byte
	OpenCallCount  int
	CloseCallCount int
}

func (m *MockWritable) WriteRaw(data []byte) error {
	m.WriteRawCalls = append(m.WriteRawCalls, data)
	return m.WriteRawError
}

func (m *MockWritable) Open() error {
	m.OpenCallCount++
	return m.OpenError
}

func (m *MockWritable) Close() error {
	m.CloseCallCount++
	return m.CloseError
}

func createMockPrinter() (*Printer, string) {
	f, err := os.CreateTemp("/tmp/", "epsonproxytest")
	if err != nil {
		panic(err)
	}

	printer, err := NewPrinter(f.Name(), 576, UsbPath)
	if err != nil {
		panic(err)
	}
	printer.connection = &UsbWriter{
		path:   f.Name(),
		writer: f,
	}
	printer.retryDelay = 0 // No delay for tests

	return printer, f.Name()
}

func TestWithRetry_RetriesSuccessful(t *testing.T) {
	printer, _ := createMockPrinter()
	count := 0
	numRetries := 3
	fn := func() (*string, error) {
		count++
		return nil, errors.New("testing err")
	}

	withRetry(printer, numRetries, fn)
	if count != numRetries {
		t.Errorf("expected %d retries, got %d", numRetries, count)
	}
}

func TestKickDrawer_Bytes(t *testing.T) {
	printer, path := createMockPrinter()
	defer os.Remove(path)

	err := printer.KickDrawer()
	if err != nil {
		t.Fatalf("KickDrawer failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	expected := []byte{0x1B, 0x70, 0, 25, 25}
	if !bytes.Equal(data, expected) {
		t.Errorf("KickDrawer sent wrong bytes. Got %v, expected %v", data, expected)
	}
}

func TestKickDrawer_WriteRawFailure(t *testing.T) {
	mock := &MockWritable{
		WriteRawError: errors.New("write failed"),
	}
	printer := &Printer{
		connection_string: "/test",
		receipt_width:     576,
		connection:        mock,
		retryDelay:        0,
	}

	err := printer.KickDrawer()
	if err == nil {
		t.Error("expected error when WriteRaw fails, got nil")
	}
}

func TestCut_Bytes(t *testing.T) {
	printer, path := createMockPrinter()
	defer os.Remove(path)

	err := printer.Cut()
	if err != nil {
		t.Fatalf("Cut failed: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	expected := append(CUT_CMD, FEED_N_CMD(2)...)
	if !bytes.Equal(data, expected) {
		t.Errorf("Cut sent wrong bytes. Got %v, expected %v", data, expected)
	}
}

// Custom mock for testing partial failures in Cut
type PartialFailMock struct {
	callCount int
	failAfter int
}

func (p *PartialFailMock) WriteRaw(data []byte) error {
	p.callCount++
	if p.callCount > p.failAfter {
		return errors.New("write failed")
	}
	return nil
}

func (p *PartialFailMock) Open() error  { return nil }
func (p *PartialFailMock) Close() error { return nil }

func TestCut_WriteRawFailure(t *testing.T) {
	mock := &PartialFailMock{failAfter: 1} // Fail on second call (FEED)

	printer := &Printer{
		connection_string: "/test",
		retryDelay:        0,
		receipt_width:     576,
		connection:        mock,
	}

	err := printer.Cut()
	if err == nil {
		t.Error("expected error when WriteRaw fails on feed, got nil")
	}
}

func TestPrintGraphics_NoCentering(t *testing.T) {
	printer, path := createMockPrinter()
	defer os.Remove(path)

	// Use width >= receipt_width to avoid centering
	width := 576 // Same as receipt width
	height := 1
	widthBytes := width / 8 // 72 bytes
	data := make([]byte, widthBytes*height)
	for i := range data {
		data[i] = byte(i)
	}

	err := printer.PrintGraphics(data, width, height)
	if err != nil {
		t.Fatalf("PrintGraphics failed: %v", err)
	}

	fileData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Verify header
	if !bytes.HasPrefix(fileData, PRINT_RASTER_CMD) {
		t.Errorf("PrintGraphics missing raster command prefix")
	}

	// Verify dimensions in bytes
	offset := len(PRINT_RASTER_CMD)
	xL := byte(widthBytes & 0xFF)
	xH := byte((widthBytes >> 8) & 0xFF)
	yL := byte(height & 0xFF)
	yH := byte((height >> 8) & 0xff)

	if fileData[offset] != xL || fileData[offset+1] != xH {
		t.Errorf("width bytes incorrect. Got %d,%d expected %d,%d",
			fileData[offset], fileData[offset+1], xL, xH)
	}
	if fileData[offset+2] != yL || fileData[offset+3] != yH {
		t.Errorf("height bytes incorrect. Got %d,%d expected %d,%d",
			fileData[offset+2], fileData[offset+3], yL, yH)
	}

	// Verify footer
	expectedFooter := FEED_N_CMD(12)
	if !bytes.HasSuffix(fileData, expectedFooter) {
		t.Errorf("PrintGraphics missing feed command footer")
	}
}

func TestPrintGraphics_WithCentering(t *testing.T) {
	printer, path := createMockPrinter()
	defer os.Remove(path)

	// Use width < receipt_width to trigger centering
	width := 64 // 8 bytes
	height := 1
	paperWidthBytes := printer.receipt_width / 8 // 72 bytes
	widthBytes := width / 8                      // 8 bytes
	data := make([]byte, widthBytes*height)
	for i := range data {
		data[i] = byte(i + 1)
	}

	err := printer.PrintGraphics(data, width, height)
	if err != nil {
		t.Fatalf("PrintGraphics failed: %v", err)
	}

	fileData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	// Verify dimensions reflect paper width, not image width
	offset := len(PRINT_RASTER_CMD)
	xL := byte(paperWidthBytes & 0xFF)
	xH := byte((paperWidthBytes >> 8) & 0xFF)

	if fileData[offset] != xL || fileData[offset+1] != xH {
		t.Errorf("width bytes should be paper width. Got %d,%d expected %d,%d",
			fileData[offset], fileData[offset+1], xL, xH)
	}

	// Calculate where raster data starts
	rasterStart := offset + 4
	paddingBytes := (paperWidthBytes - widthBytes) / 2 // (72-8)/2 = 32

	// Verify padding before data (should be zeros)
	for i := 0; i < paddingBytes; i++ {
		if fileData[rasterStart+i] != 0 {
			t.Errorf("padding byte %d should be 0, got %d", i, fileData[rasterStart+i])
		}
	}

	// Verify actual data is centered
	dataStart := rasterStart + paddingBytes
	for i := 0; i < widthBytes; i++ {
		if fileData[dataStart+i] != byte(i+1) {
			t.Errorf("data byte %d incorrect. Got %d expected %d",
				i, fileData[dataStart+i], byte(i+1))
		}
	}
}

func TestPrintGraphics_DataTooShort(t *testing.T) {
	printer, path := createMockPrinter()
	defer os.Remove(path)

	width := 64
	height := 10
	// Provide less data than required
	data := make([]byte, 10)

	err := printer.PrintGraphics(data, width, height)
	if err == nil {
		t.Error("expected error when data is too short, got nil")
	}
}

func TestPrintGraphics_WriteRawFailure(t *testing.T) {
	mock := &MockWritable{
		WriteRawError: errors.New("write failed"),
	}
	printer := &Printer{
		connection_string: "/test",
		retryDelay:        0,
		receipt_width:     576,
		connection:        mock,
	}

	width := 576
	height := 1
	data := make([]byte, width/8*height)

	err := printer.PrintGraphics(data, width, height)
	if err == nil {
		t.Error("expected error when WriteRaw fails, got nil")
	}
}

func TestCenter_SingleRow(t *testing.T) {
	// 4-byte image, 8-byte paper, height 1
	// Expected: 2 bytes padding left, 4 bytes data, 2 bytes padding right
	data := []byte{1, 2, 3, 4}
	w_b := 4
	p_w_b := 8
	h := 1

	result, err := center(data, w_b, p_w_b, h)
	if err != nil {
		t.Fatalf("center failed: %v", err)
	}

	expected := []byte{0, 0, 1, 2, 3, 4, 0, 0}
	if !bytes.Equal(result, expected) {
		t.Errorf("center result incorrect. Got %v, expected %v", result, expected)
	}
}

func TestCenter_MultiRow(t *testing.T) {
	// 2-byte image, 6-byte paper, height 2
	// Row 1: data[0:2], Row 2: data[2:4]
	data := []byte{1, 2, 3, 4}
	w_b := 2
	p_w_b := 6
	h := 2

	result, err := center(data, w_b, p_w_b, h)
	if err != nil {
		t.Fatalf("center failed: %v", err)
	}

	// Expected: [0,0,1,2,0,0, 0,0,3,4,0,0]
	expected := []byte{0, 0, 1, 2, 0, 0, 0, 0, 3, 4, 0, 0}
	if !bytes.Equal(result, expected) {
		t.Errorf("center result incorrect. Got %v, expected %v", result, expected)
	}
}

func TestCenter_AsymmetricPadding(t *testing.T) {
	// 3-byte image, 8-byte paper (diff is 5, so 2 left, 3 right)
	data := []byte{1, 2, 3}
	w_b := 3
	p_w_b := 8
	h := 1

	result, err := center(data, w_b, p_w_b, h)
	if err != nil {
		t.Fatalf("center failed: %v", err)
	}

	// Expected: 2 bytes left padding, 3 bytes data, 3 bytes right padding
	expected := []byte{0, 0, 1, 2, 3, 0, 0, 0}
	if !bytes.Equal(result, expected) {
		t.Errorf("center result incorrect. Got %v, expected %v", result, expected)
	}
}

func TestCenter_DataTooShort(t *testing.T) {
	data := []byte{1, 2} // Only 2 bytes
	w_b := 4
	p_w_b := 8
	h := 1

	_, err := center(data, w_b, p_w_b, h)
	if err == nil {
		t.Error("expected error when data is too short, got nil")
	}
}

func TestCenter_OutOfBounds(t *testing.T) {
	// Height 2 but only enough data for height 1
	data := []byte{1, 2, 3, 4} // 4 bytes = height 1 with w_b=2
	w_b := 2
	p_w_b := 4
	h := 3 // Request 3 rows, but only have data for 2

	_, err := center(data, w_b, p_w_b, h)
	if err == nil {
		t.Error("expected error when row is out of bounds, got nil")
	}
}

func TestCenter_ExactSize(t *testing.T) {
	// Image width equals paper width, no padding needed
	data := []byte{1, 2, 3, 4}
	w_b := 4
	p_w_b := 4
	h := 1

	result, err := center(data, w_b, p_w_b, h)
	if err != nil {
		t.Fatalf("center failed: %v", err)
	}

	// Expected: same as input, no padding
	if !bytes.Equal(result, data) {
		t.Errorf("center result incorrect. Got %v, expected %v", result, data)
	}
}

func TestClose(t *testing.T) {
	printer, path := createMockPrinter()
	defer os.Remove(path)

	err := printer.Close()
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestClose_WithNilConnection(t *testing.T) {
	printer := &Printer{
		connection_string: "/test",
		retryDelay:        0,
		receipt_width:     576,
		connection:        nil,
	}

	// Should not panic
	err := printer.Close()
	if err != nil {
		t.Errorf("Close with nil connection should return nil, got: %v", err)
	}
}

func TestClose_Failure(t *testing.T) {
	mock := &MockWritable{
		CloseError: errors.New("close failed"),
	}
	printer := &Printer{
		connection_string: "/test",
		retryDelay:        0,
		receipt_width:     576,
		connection:        mock,
	}

	err := printer.Close()
	if err == nil {
		t.Error("expected error when Close fails, got nil")
	}
}

func TestWithRetry_ClosesConnectionOnFailure(t *testing.T) {
	mock := &MockWritable{}
	printer := &Printer{
		connection_string: "/test",
		retryDelay:        0,
		receipt_width:     576,
		connection:        mock,
	}

	fn := func() (any, error) {
		return nil, errors.New("always fails")
	}

	withRetry(printer, 2, fn)

	// Should have closed connection at least once
	if mock.CloseCallCount == 0 {
		t.Error("expected Close to be called on failure")
	}

	// Should have reopened connection
	if mock.OpenCallCount == 0 {
		t.Error("expected Open to be called on failure")
	}
}

func TestWithRetry_CloseFailure(t *testing.T) {
	mock := &MockWritable{
		CloseError: errors.New("close failed"),
	}
	printer := &Printer{
		connection_string: "/test",
		retryDelay:        0,
		receipt_width:     576,
		connection:        mock,
	}

	fn := func() (any, error) {
		return nil, errors.New("always fails")
	}

	// Should not panic even when Close fails
	_, err := withRetry(printer, 2, fn)
	if err == nil {
		t.Error("expected error after retries")
	}

	// Should still attempt to close
	if mock.CloseCallCount == 0 {
		t.Error("expected Close to be attempted even on failure")
	}
}

func TestWithRetry_OpenCalledOnEachRetry(t *testing.T) {
	mock := &MockWritable{}
	printer := &Printer{
		connection_string: "/test",
		retryDelay:        0,
		receipt_width:     576,
		connection:        mock,
	}

	fn := func() (any, error) {
		return nil, errors.New("always fails")
	}

	withRetry(printer, 3, fn)

	// Should have called Open 2 times (after failures 1 and 2, not after final failure)
	if mock.OpenCallCount != 2 {
		t.Errorf("expected Open to be called 2 times (after failures 1 and 2), got %d", mock.OpenCallCount)
	}
}

func TestWithRetry_OpenCalledOnlyOnFailureRetries(t *testing.T) {
	mock := &MockWritable{}
	printer := &Printer{
		connection_string: "/test",
		retryDelay:        0,
		receipt_width:     576,
		connection:        mock,
	}

	callCount := 0
	fn := func() (any, error) {
		callCount++
		if callCount < 3 {
			return nil, errors.New("fails first 2 times")
		}
		return "success", nil
	}

	result, err := withRetry(printer, 5, fn)
	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}
	if result != "success" {
		t.Errorf("expected 'success', got %v", result)
	}

	// Should have called Open 2 times (after failures 1 and 2, not after success)
	if mock.OpenCallCount != 2 {
		t.Errorf("expected Open to be called 2 times (after failures), got %d", mock.OpenCallCount)
	}
}

func TestWithRetry_OpenFailureContinuesToNextRetry(t *testing.T) {
	mock := &MockWritable{}
	printer := &Printer{
		connection_string: "/test",
		retryDelay:        0,
		receipt_width:     576,
		connection:        mock,
	}

	fnCallCount := 0
	fn := func() (any, error) {
		fnCallCount++
		return nil, errors.New("always fails")
	}

	withRetry(printer, 3, fn)

	// Should have called Open 2 times (after failures 1 and 2, not after final failure)
	if mock.OpenCallCount != 2 {
		t.Errorf("expected Open to be called 2 times (after failures 1 and 2), got %d", mock.OpenCallCount)
	}

	// Should have called the function 3 times
	if fnCallCount != 3 {
		t.Errorf("expected fn to be called 3 times, got %d", fnCallCount)
	}
}

func TestWithRetry_NoOpenOnFirstSuccess(t *testing.T) {
	mock := &MockWritable{}
	printer := &Printer{
		connection_string: "/test",
		retryDelay:        0,
		receipt_width:     576,
		connection:        mock,
	}

	fn := func() (any, error) {
		return "immediate success", nil
	}

	result, err := withRetry(printer, 3, fn)
	if err != nil {
		t.Errorf("expected success, got error: %v", err)
	}
	if result != "immediate success" {
		t.Errorf("expected 'immediate success', got %v", result)
	}

	// Should not call Open on immediate success
	if mock.OpenCallCount != 0 {
		t.Errorf("expected Open to be called 0 times on first success, got %d", mock.OpenCallCount)
	}

	// Should not call Close on immediate success
	if mock.CloseCallCount != 0 {
		t.Errorf("expected Close to be called 0 times on first success, got %d", mock.CloseCallCount)
	}
}

func TestWithRetry_CloseAndOpenSequence(t *testing.T) {
	mock := &MockWritable{}
	printer := &Printer{
		connection_string: "/test",
		retryDelay:        0,
		receipt_width:     576,
		connection:        mock,
	}

	fn := func() (any, error) {
		return nil, errors.New("always fails")
	}

	withRetry(printer, 2, fn)

	// Verify sequence:
	// - Attempt 1 fails: Close then Open
	// - Attempt 2 fails: Close (no Open because it's the final attempt)
	if mock.CloseCallCount != 2 {
		t.Errorf("expected Close to be called 2 times (after each failure), got %d", mock.CloseCallCount)
	}
	if mock.OpenCallCount != 1 {
		t.Errorf("expected Open to be called 1 time (after first failure only), got %d", mock.OpenCallCount)
	}
}
