package main

import (
	"bytes"
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

const (
	eposNamespacePrefix = "http://www.epson-pos.com/schemas/"
	eposNamespaceSuffix = "/epos-print"
)

func isSupportedEposNamespace(namespace string) bool {
	if !strings.HasPrefix(namespace, eposNamespacePrefix) || !strings.HasSuffix(namespace, eposNamespaceSuffix) {
		return false
	}

	version := strings.TrimSuffix(strings.TrimPrefix(namespace, eposNamespacePrefix), eposNamespaceSuffix)
	if version == "" {
		return false
	}

	for _, part := range strings.Split(version, "/") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}

	return true
}

func Parse(xmlData []byte) (*EposPrint, error) {
	log.Printf("[PARSER] Starting XML parsing of %d bytes", len(xmlData))

	epos := &EposPrint{
		Instructions: []Instruction{},
	}

	decoder := xml.NewDecoder(bytes.NewReader(xmlData))

	var currentImage *ImageDecoded
	var inImage bool
	var rootSeen bool
	var rootOpen bool
	rootDepth := 0
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

			if !rootSeen {
				if name != "epos-print" || !isSupportedEposNamespace(space) {
					log.Printf("[PARSER] ERROR: Invalid root element <%s> in namespace '%s'", name, space)
					return nil, fmt.Errorf("invalid EPOS root element <%s> in namespace %q", name, space)
				}

				rootSeen = true
				rootOpen = true
				rootDepth = 1
				epos.XMLName = se.Name
				log.Printf("[PARSER] Found root element: epos-print (namespace: %s)", space)
				continue
			}

			if !rootOpen {
				log.Printf("[PARSER] ERROR: Unexpected element <%s> after root was closed", name)
				return nil, fmt.Errorf("unexpected element <%s> after epos-print root", name)
			}

			rootDepth++

			if name == "pulse" && space == epos.XMLName.Space {
				epos.Instructions = append(epos.Instructions, Instruction{Type: InstPulse})
				log.Printf("[PARSER] Added instruction: PULSE (kick drawer) [total: %d]", len(epos.Instructions))
			} else if name == "cut" && space == epos.XMLName.Space {
				epos.Instructions = append(epos.Instructions, Instruction{Type: InstCut})
				log.Printf("[PARSER] Added instruction: CUT [total: %d]", len(epos.Instructions))
			} else if name == "image" && space == epos.XMLName.Space {
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
			if rootOpen && inImage && currentImage != nil && content != "" {
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

			if rootOpen && name == "image" && space == epos.XMLName.Space && inImage {
				inImage = false
				widthBytes, expectedBytes, err := rasterDataSize(currentImage.Width, currentImage.Height)
				if err != nil {
					log.Printf("[PARSER] ERROR: Invalid image dimensions: %v", err)
					return nil, fmt.Errorf("invalid image dimensions: %w", err)
				}
				log.Printf("[PARSER] Image element complete:")
				log.Printf("[PARSER]   Decoded data: %d bytes", len(currentImage.Data))
				log.Printf("[PARSER]   Expected size: %d bytes (width_bytes=%d, width=%d, height=%d)",
					expectedBytes, widthBytes, currentImage.Width, currentImage.Height)

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

			if rootOpen {
				rootDepth--
				if rootDepth == 0 {
					rootOpen = false
				}
			}
		}
	}

	if !rootSeen {
		log.Printf("[PARSER] ERROR: Missing epos-print root element")
		return nil, fmt.Errorf("missing epos-print root element")
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
