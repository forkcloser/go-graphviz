package gvc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// An image file is read no further than the most an image within the pixel
// budget takes: a file one byte over is refused, after reading that byte.
func TestReadAtMost(t *testing.T) {
	const content = "12345"

	data, err := readAtMost(strings.NewReader(content), int64(len(content)))
	if err != nil || string(data) != content {
		t.Errorf("readAtMost at the limit = %q, %v; want %q", data, err, content)
	}

	reader := strings.NewReader(content + "6789")
	if _, err := readAtMost(reader, int64(len(content))); !errors.Is(err, ErrImageTooLarge) {
		t.Errorf("readAtMost over the limit returned %v, want %v", err, ErrImageTooLarge)
	}

	if read := int64(len(content)+4) - int64(reader.Len()); read != int64(len(content))+1 {
		t.Errorf("read %d bytes of a file over the limit, want %d", read, len(content)+1)
	}
}

// The file limit admits the largest image the pixel budget does: an
// uncompressed BMP at four bytes a pixel.
func TestImageFileLimitCoversThePixelBudget(t *testing.T) {
	if maxImageFileBytes < 4*MaxImagePixels {
		t.Errorf("file limit %d below a %d-pixel image at four bytes a pixel", maxImageFileBytes, MaxImagePixels)
	}
}

// The node images a page keeps stay within maxImagesKept however many the
// graph names: when one more would pass it, those kept go, and an image
// that went is decoded again when a node shows it. They were all kept, so
// a page's memory followed the number of images its graph named.
func TestNodeImagesKeptWithinTheirBound(t *testing.T) {
	const side = 2048

	var file bytes.Buffer
	if err := png.Encode(&file, image.NewGray(image.Rect(0, 0, side, side))); err != nil {
		t.Fatal(err)
	}

	// One more image than the bound holds, each counted four bytes a pixel.
	count := maxImagesKept/(4*side*side) + 1
	files := fstest.MapFS{}

	for i := range count {
		files[fmt.Sprintf("%d.png", i)] = &fstest.MapFile{Data: file.Bytes()}
	}

	wasm.SetWasmFileSystem(files)
	defer wasm.SetWasmFileSystem(nil)

	renderer := &ImageRenderer{images: map[string]image.Image{}}

	for i := range count {
		img, err := renderer.nodeImage(fmt.Sprintf("%d.png", i), side, side)
		if err != nil {
			t.Fatal(err)
		}

		if img.Bounds().Dx() != side {
			t.Fatalf("image %d is %d pixels wide, want %d", i, img.Bounds().Dx(), side)
		}

		if renderer.held > maxImagesKept {
			t.Fatalf("%d bytes of images kept after %d of them, over the %d allowed", renderer.held, i+1, maxImagesKept)
		}
	}

	if _, kept := renderer.images["0.png"]; kept {
		t.Error("the first image is still kept after one more than the bound holds")
	}

	last := fmt.Sprintf("%d.png", count-1)
	if _, kept := renderer.images[last]; !kept {
		t.Error("the last image shown is not kept")
	}

	again, err := renderer.nodeImage("0.png", side, side)
	if err != nil || again.Bounds().Dx() != side {
		t.Errorf("the first image shown again: %v, %v", again, err)
	}

	// A size drawn is kept and counted as the decoded image is.
	before := renderer.held

	small, err := renderer.nodeImage("0.png", 16, 16)
	if err != nil || small.Bounds().Dx() != 16 {
		t.Fatalf("the first image at 16 pixels: %v, %v", small, err)
	}

	if grown := renderer.held - before; grown != 4*16*16 {
		t.Errorf("an image scaled to 16 by 16 is counted %d bytes, want %d", grown, 4*16*16)
	}

	// Sixteen bits a channel are eight bytes a pixel.
	if got := imageBytes(image.NewNRGBA64(image.Rect(0, 0, 3, 2))); got != 8*3*2 {
		t.Errorf("a 3 by 2 image of sixteen bits a channel is counted %d bytes, want %d", got, 8*3*2)
	}
}
