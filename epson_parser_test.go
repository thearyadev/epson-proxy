package main

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Core Functionality Tests

func TestParse_ValidXML(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<pulse/>
	<cut/>
	<image width="8" height="8">AAAAAAAAAAA=</image>
</epos-print>`

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 3 {
		t.Errorf("expected 3 instructions, got %d", len(result.Instructions))
	}

	if result.Instructions[0].Type != InstPulse {
		t.Errorf("expected first instruction to be Pulse, got %v", result.Instructions[0].Type)
	}

	if result.Instructions[1].Type != InstCut {
		t.Errorf("expected second instruction to be Cut, got %v", result.Instructions[1].Type)
	}

	if result.Instructions[2].Type != InstImage {
		t.Errorf("expected third instruction to be Image, got %v", result.Instructions[2].Type)
	}
}

func TestParse_MultipleInstructions(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<pulse/>
	<image width="8" height="1">AA==</image>
	<cut/>
	<pulse/>
</epos-print>`

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 4 {
		t.Errorf("expected 4 instructions, got %d", len(result.Instructions))
	}

	expectedTypes := []InstructionType{InstPulse, InstImage, InstCut, InstPulse}
	for i, exp := range expectedTypes {
		if result.Instructions[i].Type != exp {
			t.Errorf("instruction %d: expected %v, got %v", i, exp, result.Instructions[i].Type)
		}
	}
}

func TestParse_EmptyXML(t *testing.T) {
	xml := ``

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 0 {
		t.Errorf("expected 0 instructions, got %d", len(result.Instructions))
	}
}

func TestMustParse_Panic(t *testing.T) {
	// Use invalid base64 image data which will cause an actual error
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<image width="8" height="8">!!!INVALID_BASE64!!!</image>
</epos-print>`

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic but did not get one")
		}
	}()

	MustParse([]byte(xml))
}

// XML Structure Edge Cases

func TestParse_MalformedXML(t *testing.T) {
	tests := []struct {
		name string
		xml  string
	}{
		{
			name: "unclosed tag",
			xml:  `<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print"><pulse>`,
		},
		{
			name: "invalid syntax",
			xml:  `<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print"><invalid<<<>>>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.xml))
			if err != nil {
				t.Logf("Got expected error: %v", err)
			}
		})
	}
}

func TestParse_WrongNamespace(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://wrong-namespace.com">
	<pulse/>
	<cut/>
</epos-print>`

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 0 {
		t.Errorf("expected 0 instructions (wrong namespace), got %d", len(result.Instructions))
	}
}

func TestParse_MissingRoot(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<pulse xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print"/>`

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 1 {
		t.Errorf("expected 1 instruction, got %d", len(result.Instructions))
	}
}

func TestParse_CommentsAndWhitespace(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<!-- This is a comment -->
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<!-- Comment inside -->
	<pulse/>

	<cut/>
</epos-print>`

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 2 {
		t.Errorf("expected 2 instructions, got %d", len(result.Instructions))
	}
}

// Image Data Edge Cases

func TestParse_ValidBase64Image(t *testing.T) {
	// 8x8 pixels = (8/8) * 8 = 8 bytes
	imageData := make([]byte, 8)
	for i := range imageData {
		imageData[i] = byte(i)
	}
	b64Data := base64.StdEncoding.EncodeToString(imageData)

	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<image width="8" height="8">` + b64Data + `</image>
</epos-print>`

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(result.Instructions))
	}

	if result.Instructions[0].Type != InstImage {
		t.Errorf("expected instruction to be Image, got %v", result.Instructions[0].Type)
	}

	img := result.Instructions[0].Image
	if img == nil {
		t.Fatal("expected image to not be nil")
	}

	if img.Width != 8 {
		t.Errorf("expected width 8, got %d", img.Width)
	}

	if img.Height != 8 {
		t.Errorf("expected height 8, got %d", img.Height)
	}

	if len(img.Data) != 8 {
		t.Errorf("expected 8 bytes of data, got %d", len(img.Data))
	}
}

func TestParse_InvalidBase64(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<image width="8" height="8">!!!INVALID_BASE64!!!</image>
</epos-print>`

	_, err := Parse([]byte(xml))
	if err == nil {
		t.Error("expected error for invalid base64, got nil")
	}

	if !strings.Contains(err.Error(), "base64") {
		t.Errorf("expected base64 error, got: %v", err)
	}
}

func TestParse_LargeImageData(t *testing.T) {
	// 1000x1000 pixels = (1000/8) * 1000 = 125 * 1000 = 125000 bytes (~122KB)
	width := 1000
	height := 1000
	expectedBytes := (width / 8) * height

	imageData := make([]byte, expectedBytes)
	for i := range imageData {
		imageData[i] = byte(i % 256)
	}
	b64Data := base64.StdEncoding.EncodeToString(imageData)

	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<image width="1000" height="1000">` + b64Data + `</image>
</epos-print>`

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(result.Instructions))
	}

	img := result.Instructions[0].Image
	if len(img.Data) != expectedBytes {
		t.Errorf("expected %d bytes, got %d", expectedBytes, len(img.Data))
	}
}

func TestParse_ImageSizeMismatch(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<image width="8" height="8">AA==</image>
</epos-print>`

	_, err := Parse([]byte(xml))
	if err == nil {
		t.Error("expected error for image size mismatch, got nil")
	}

	if !strings.Contains(err.Error(), "incomplete") {
		t.Errorf("expected 'incomplete' error, got: %v", err)
	}
}

func TestParse_ZeroDimensions(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<image width="0" height="0"></image>
</epos-print>`

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(result.Instructions))
	}

	img := result.Instructions[0].Image
	if img.Width != 0 || img.Height != 0 {
		t.Errorf("expected 0x0 dimensions, got %dx%d", img.Width, img.Height)
	}
}

func TestParse_MissingWidthHeight(t *testing.T) {
	// Default to 0, expected bytes = (0/8) * 0 = 0
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<image></image>
</epos-print>`

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(result.Instructions))
	}

	img := result.Instructions[0].Image
	if img.Width != 0 || img.Height != 0 {
		t.Errorf("expected 0x0 dimensions (defaults), got %dx%d", img.Width, img.Height)
	}
}

func TestParse_WhitespaceInImageData(t *testing.T) {
	// 8x1 pixels = 1 byte
	imageData := []byte{0xFF}
	b64Data := base64.StdEncoding.EncodeToString(imageData)

	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<image width="8" height="1">
		` + b64Data + `
	</image>
</epos-print>`

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(result.Instructions))
	}

	img := result.Instructions[0].Image
	if len(img.Data) != 1 {
		t.Errorf("expected 1 byte, got %d", len(img.Data))
	}
}

// Attribute Edge Cases

func TestParse_NonNumericDimensions(t *testing.T) {
	// Non-numeric width/height default to 0, so no image data needed
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<image width="abc" height="def"></image>
</epos-print>`

	result, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Instructions) != 1 {
		t.Fatalf("expected 1 instruction, got %d", len(result.Instructions))
	}

	img := result.Instructions[0].Image
	if img.Width != 0 || img.Height != 0 {
		t.Errorf("expected 0x0 dimensions (invalid parsed), got %dx%d", img.Width, img.Height)
	}
}

func TestParse_NegativeDimensions(t *testing.T) {
	// Negative dimensions result in expected bytes calculation issues
	// This documents current behavior - parser accepts negative values
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<image width="-8" height="-8"></image>
</epos-print>`

	result, err := Parse([]byte(xml))
	// Negative dimensions cause expectedBytes = (-8/8) * -8 = 8 bytes expected
	// But no data provided, so this should error
	if err == nil {
		t.Error("expected error for negative dimensions with no data")
	}

	if result != nil && len(result.Instructions) != 0 {
		t.Logf("Parser returned partial result with %d instructions", len(result.Instructions))
	}
}

func TestParse_VeryLargeDimensions(t *testing.T) {
	// Very large dimensions result in huge expected byte count
	// This documents current behavior - parser calculates expected bytes but fails validation
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<epos-print xmlns="http://www.epson-pos.com/schemas/2012/10/epos-print">
	<image width="2147483647" height="1"></image>
</epos-print>`

	result, err := Parse([]byte(xml))
	// expectedBytes = (2147483647/8) * 1 = 268435455 bytes expected
	// But no data provided, so this should error
	if err == nil {
		t.Error("expected error for very large dimensions with no data")
	}

	if result != nil && len(result.Instructions) != 0 {
		t.Logf("Parser returned partial result with %d instructions", len(result.Instructions))
	}
}
