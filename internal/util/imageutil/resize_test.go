package imageutil

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestResizePNGToJPEG(t *testing.T) {
	var input bytes.Buffer
	if err := png.Encode(&input, image.NewRGBA(image.Rect(0, 0, 800, 400))); err != nil {
		t.Fatal(err)
	}
	output, err := ResizeAndCompress(input.Bytes(), 512, 80)
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 512 || config.Height != 256 {
		t.Fatalf("dimensions %dx%d", config.Width, config.Height)
	}
}

func TestResizePreservesThinImage(t *testing.T) {
	var input bytes.Buffer
	if err := jpeg.Encode(&input, image.NewRGBA(image.Rect(0, 0, 1600, 1)), nil); err != nil {
		t.Fatal(err)
	}
	output, err := ResizeAndCompress(input.Bytes(), 512, 80)
	if err != nil {
		t.Fatal(err)
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(output))
	if err != nil {
		t.Fatal(err)
	}
	if config.Width != 512 || config.Height != 1 {
		t.Fatalf("dimensions %dx%d", config.Width, config.Height)
	}
}
