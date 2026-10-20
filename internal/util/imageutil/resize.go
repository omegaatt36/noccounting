package imageutil

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
)

// ResizeAndCompress shrinks an image until its shorter side is at most maxShort
// and its longer side at most maxLong, then encodes it as JPEG with the given
// quality (1-100). The shorter side leads because a receipt is long and narrow:
// capping the longer side alone would leave its text a few pixels tall.
// An image already within both limits is still re-encoded as JPEG to ensure
// consistent format and to strip unnecessary metadata.
func ResizeAndCompress(data []byte, maxShort, maxLong, quality int) ([]byte, error) {
	// Decode supported JPEG and PNG receipt images.
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	scale := min(1,
		float64(maxShort)/float64(min(w, h)),
		float64(maxLong)/float64(max(w, h)),
	)

	var dst image.Image
	if scale == 1 {
		dst = img
	} else {
		newW := max(1, int(float64(w)*scale+0.5))
		newH := max(1, int(float64(h)*scale+0.5))

		dst = image.NewRGBA(image.Rect(0, 0, newW, newH))
		draw.CatmullRom.Scale(dst.(*image.RGBA), dst.Bounds(), img, bounds, draw.Over, nil)
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode JPEG: %w", err)
	}
	return buf.Bytes(), nil
}
