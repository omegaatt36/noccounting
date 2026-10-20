package imageutil

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
)

func resized(t *testing.T, input []byte, maxShort, maxLong int) (int, int) {
	t.Helper()
	output, err := ResizeAndCompress(input, maxShort, maxLong, 80)
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	return config.Width, config.Height
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return input.Bytes()
}

func TestResizePNGToJPEG(t *testing.T) {
	if w, h := resized(t, pngOf(t, 1600, 800), 400, 4096); w != 800 || h != 400 {
		t.Fatalf("dimensions %dx%d, want 800x400", w, h)
	}
}

func TestResizeLeavesAnImageWithinTheLimitsAlone(t *testing.T) {
	if w, h := resized(t, pngOf(t, 800, 400), 512, 4096); w != 800 || h != 400 {
		t.Fatalf("dimensions %dx%d, want 800x400", w, h)
	}
}

// A long receipt keeps its width: capping the longer side alone would shrink
// this one to 256 pixels across.
func TestResizeKeepsTheWidthOfALongReceipt(t *testing.T) {
	if w, h := resized(t, pngOf(t, 1000, 4000), 500, 4096); w != 500 || h != 2000 {
		t.Fatalf("dimensions %dx%d, want 500x2000", w, h)
	}
}

func TestResizeCapsTheLongerSide(t *testing.T) {
	if w, h := resized(t, pngOf(t, 400, 6000), 1280, 3000); w != 200 || h != 3000 {
		t.Fatalf("dimensions %dx%d, want 200x3000", w, h)
	}
}

func TestResizePreservesThinImage(t *testing.T) {
	var input bytes.Buffer
	if err := jpeg.Encode(&input, image.NewRGBA(image.Rect(0, 0, 1600, 1)), nil); err != nil {
		t.Fatal(err)
	}
	if w, h := resized(t, input.Bytes(), 512, 512); w != 512 || h != 1 {
		t.Fatalf("dimensions %dx%d, want 512x1", w, h)
	}
}
