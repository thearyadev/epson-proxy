package main

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"strings"
)

type InstructionType int

const (
	InstImage InstructionType = iota
	InstPulse
	InstCut
)

type Instruction struct {
	Type  InstructionType
	Image *ImageDecoded
}

type EposPrint struct {
	XMLName      xml.Name
	Instructions []Instruction
}

type ImageDecoded struct {
	Width  int
	Height int
	Data   []byte
}

func Parse(xmlData []byte) (*EposPrint, error) {
	epos := &EposPrint{
		Instructions: []Instruction{},
	}

	decoder := xml.NewDecoder(strings.NewReader(string(xmlData)))

	var currentImage *ImageDecoded
	var inImage bool

	for {
		token, err := decoder.Token()
		if err != nil {
			if err != io.EOF {
				log.Printf("XML parsing error: %v", err)
			}
			break
		}

		switch se := token.(type) {
		case xml.StartElement:
			name := se.Name.Local
			space := se.Name.Space

			if name == "epos-print" && strings.Contains(space, "epson-pos") {
				epos.XMLName = se.Name
			} else if name == "pulse" && strings.Contains(space, "epson-pos") {
				epos.Instructions = append(epos.Instructions, Instruction{Type: InstPulse})
			} else if name == "cut" && strings.Contains(space, "epson-pos") {
				epos.Instructions = append(epos.Instructions, Instruction{Type: InstCut})
			} else if name == "image" && strings.Contains(space, "epson-pos") {
				inImage = true
				currentImage = &ImageDecoded{}
				for _, attr := range se.Attr {
					if attr.Name.Local == "width" {
						fmt.Sscanf(attr.Value, "%d", &currentImage.Width)
					} else if attr.Name.Local == "height" {
						fmt.Sscanf(attr.Value, "%d", &currentImage.Height)
					}
				}
			}

		case xml.CharData:
			if inImage && currentImage != nil {
				content := strings.TrimSpace(string(se))
				if content != "" {
					decodedData, err := base64.StdEncoding.DecodeString(content)
					if err != nil {
						return nil, fmt.Errorf("failed to decode image base64: %w", err)
					}
					currentImage.Data = append(currentImage.Data, decodedData...)
				}
			}

		case xml.EndElement:
			name := se.Name.Local
			space := se.Name.Space

			if name == "image" && strings.Contains(space, "epson-pos") && inImage {
				inImage = false
				expectedBytes := (currentImage.Width / 8) * currentImage.Height
				if len(currentImage.Data) != expectedBytes {
					return nil, fmt.Errorf("image data incomplete: got %d bytes, expected %d bytes (width=%d, height=%d)",
						len(currentImage.Data), expectedBytes, currentImage.Width, currentImage.Height)
				}
				epos.Instructions = append(epos.Instructions, Instruction{
					Type:  InstImage,
					Image: currentImage,
				})
				currentImage = nil
			}
		}
	}

	return epos, nil
}

func MustParse(xmlData []byte) *EposPrint {
	ep, err := Parse(xmlData)
	if err != nil {
		panic(err)
	}
	return ep
}
