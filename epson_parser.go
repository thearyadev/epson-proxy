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
	log.Printf("[PARSER] Starting XML parsing of %d bytes", len(xmlData))

	epos := &EposPrint{
		Instructions: []Instruction{},
	}

	decoder := xml.NewDecoder(strings.NewReader(string(xmlData)))

	var currentImage *ImageDecoded
	var inImage bool
	tokenCount := 0

	for {
		token, err := decoder.Token()
		if err != nil {
			if err != io.EOF {
				log.Printf("[PARSER] XML parsing error: %v", err)
				return nil, fmt.Errorf("XML syntax error: %w", err)
			}
			log.Printf("[PARSER] End of XML document reached after %d tokens", tokenCount)
			break
		}
		tokenCount++

		switch se := token.(type) {
		case xml.StartElement:
			name := se.Name.Local
			space := se.Name.Space
			log.Printf("[PARSER] Token %d: StartElement <%s> in namespace '%s'", tokenCount, name, space)

			if name == "epos-print" && strings.Contains(space, "epson-pos") {
				epos.XMLName = se.Name
				log.Printf("[PARSER] Found root element: epos-print (namespace: %s)", space)
			} else if name == "pulse" && strings.Contains(space, "epson-pos") {
				epos.Instructions = append(epos.Instructions, Instruction{Type: InstPulse})
				log.Printf("[PARSER] Added instruction: PULSE (kick drawer) [total: %d]", len(epos.Instructions))
			} else if name == "cut" && strings.Contains(space, "epson-pos") {
				epos.Instructions = append(epos.Instructions, Instruction{Type: InstCut})
				log.Printf("[PARSER] Added instruction: CUT [total: %d]", len(epos.Instructions))
			} else if name == "image" && strings.Contains(space, "epson-pos") {
				inImage = true
				currentImage = &ImageDecoded{}
				log.Printf("[PARSER] Processing image element with attributes:")
				for _, attr := range se.Attr {
					log.Printf("[PARSER]   Attribute: %s = %s", attr.Name.Local, attr.Value)
					if attr.Name.Local == "width" {
						fmt.Sscanf(attr.Value, "%d", &currentImage.Width)
					} else if attr.Name.Local == "height" {
						fmt.Sscanf(attr.Value, "%d", &currentImage.Height)
					}
				}
				log.Printf("[PARSER] Image dimensions set: width=%d, height=%d", currentImage.Width, currentImage.Height)
			}

		case xml.CharData:
			content := strings.TrimSpace(string(se))
			if inImage && currentImage != nil && content != "" {
				log.Printf("[PARSER] Processing base64 image data: %d characters", len(content))
				decodedData, err := base64.StdEncoding.DecodeString(content)
				if err != nil {
					log.Printf("[PARSER] ERROR: Failed to decode image base64: %v", err)
					return nil, fmt.Errorf("failed to decode image base64: %w", err)
				}
				currentImage.Data = append(currentImage.Data, decodedData...)
				log.Printf("[PARSER] Image data decoded: %d bytes", len(decodedData))
			}

		case xml.EndElement:
			name := se.Name.Local
			space := se.Name.Space

			if name == "image" && strings.Contains(space, "epson-pos") && inImage {
				inImage = false
				expectedBytes := (currentImage.Width / 8) * currentImage.Height
				log.Printf("[PARSER] Image element complete:")
				log.Printf("[PARSER]   Decoded data: %d bytes", len(currentImage.Data))
				log.Printf("[PARSER]   Expected size: %d bytes (width=%d/8 * height=%d)",
					expectedBytes, currentImage.Width, currentImage.Height)

				if len(currentImage.Data) != expectedBytes {
					log.Printf("[PARSER] ERROR: Image data size mismatch: got %d bytes, expected %d bytes",
						len(currentImage.Data), expectedBytes)
					return nil, fmt.Errorf("image data incomplete: got %d bytes, expected %d bytes (width=%d, height=%d)",
						len(currentImage.Data), expectedBytes, currentImage.Width, currentImage.Height)
				}
				epos.Instructions = append(epos.Instructions, Instruction{
					Type:  InstImage,
					Image: currentImage,
				})
				log.Printf("[PARSER] Added instruction: IMAGE (width=%d, height=%d, %d bytes) [total: %d]",
					currentImage.Width, currentImage.Height, len(currentImage.Data), len(epos.Instructions))
				currentImage = nil
			}
		}
	}

	log.Printf("[PARSER] XML parsing complete: %d instructions parsed", len(epos.Instructions))
	return epos, nil
}

func MustParse(xmlData []byte) *EposPrint {
	log.Printf("[PARSER] MustParse called with %d bytes", len(xmlData))
	ep, err := Parse(xmlData)
	if err != nil {
		log.Printf("[PARSER] MustParse panic: %v", err)
		panic(err)
	}
	log.Printf("[PARSER] MustParse successful: %d instructions", len(ep.Instructions))
	return ep
}
