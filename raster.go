package main

import "fmt"

func rasterWidthBytes(width int) (int, error) {
	if width < 0 {
		return 0, fmt.Errorf("width must be >= 0: %d", width)
	}
	if width == 0 {
		return 0, nil
	}
	return (width + 7) / 8, nil
}

func rasterDataSize(width int, height int) (int, int, error) {
	if height < 0 {
		return 0, 0, fmt.Errorf("height must be >= 0: %d", height)
	}

	widthBytes, err := rasterWidthBytes(width)
	if err != nil {
		return 0, 0, err
	}
	if widthBytes == 0 || height == 0 {
		return widthBytes, 0, nil
	}

	maxInt := int(^uint(0) >> 1)
	if widthBytes > maxInt/height {
		return 0, 0, fmt.Errorf("image dimensions too large: width=%d height=%d", width, height)
	}

	return widthBytes, widthBytes * height, nil
}
