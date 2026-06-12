package imageutil

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"

	"golang.org/x/image/draw"
)

// ResizeAndCompress resizes an image so that its longest dimension does not exceed maxDim,
// then encodes it as JPEG with the given quality (1-100).
// If the image is already smaller than maxDim, it is still re-encoded as JPEG
// to ensure consistent format and to strip unnecessary metadata.
func ResizeAndCompress(data []byte, maxDim, quality int) ([]byte, error) {
	// Ensure decoders are registered (image/jpeg and image/png are imported above).
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	var dst image.Image
	if w <= maxDim && h <= maxDim {
		// Still re-encode to JPEG for consistent format and size reduction.
		dst = img
	} else {
		var newW, newH int
		if w > h {
			newW = maxDim
			newH = h * maxDim / w
		} else {
			newH = maxDim
			newW = w * maxDim / h
		}

		dst = image.NewRGBA(image.Rect(0, 0, newW, newH))
		draw.CatmullRom.Scale(dst.(*image.RGBA), dst.Bounds(), img, bounds, draw.Over, nil)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode JPEG: %w", err)
	}
	return buf.Bytes(), nil
}
