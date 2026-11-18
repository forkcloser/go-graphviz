package gvc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io/fs"
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

// largeImages is count image files of side by side pixels, named 0.png and
// on, and the file system that holds them, which counts how many times
// each is opened.
func largeImages(t *testing.T, count, side int) *countingFS {
	t.Helper()

	var file bytes.Buffer
	if err := png.Encode(&file, image.NewGray(image.Rect(0, 0, side, side))); err != nil {
		t.Fatal(err)
	}

	files := fstest.MapFS{}

	for i := range count {
		files[fmt.Sprintf("%d.png", i)] = &fstest.MapFile{Data: file.Bytes()}
	}

	return &countingFS{files: files, opened: map[string]int{}}
}

type countingFS struct {
	files  fstest.MapFS
	opened map[string]int
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.opened[name]++

	return c.files.Open(name)
}

func newImageRenderer() *ImageRenderer {
	return &ImageRenderer{decoded: map[string]image.Image{}, drawn: map[string]image.Image{}}
}

// The node images a page keeps stay within maxImagesKept however many the
// graph names: when one more would pass it, the decoded ones go, and an
// image that went is decoded again when a node shows it. They were all
// kept, so a page's memory followed the number of images its graph named.
func TestNodeImagesKeptWithinTheirBound(t *testing.T) {
	const side = 2048

	// One more image than the bound holds, each counted four bytes a pixel.
	count := maxImagesKept/(4*side*side) + 1

	wasm.SetWasmFileSystem(largeImages(t, count, side))
	defer wasm.SetWasmFileSystem(nil)

	renderer := newImageRenderer()

	for i := range count {
		img, err := renderer.nodeImage(fmt.Sprintf("%d.png", i), side, side)
		if err != nil {
			t.Fatal(err)
		}

		if img.Bounds().Dx() != side {
			t.Fatalf("image %d is %d pixels wide, want %d", i, img.Bounds().Dx(), side)
		}

		if kept := renderer.heldDecoded + renderer.heldDrawn; kept > maxImagesKept {
			t.Fatalf("%d bytes of images kept after %d of them, over the %d allowed", kept, i+1, maxImagesKept)
		}
	}

	if _, kept := renderer.decoded["0.png"]; kept {
		t.Error("the first image is still kept after one more than the bound holds")
	}

	last := fmt.Sprintf("%d.png", count-1)
	if _, kept := renderer.decoded[last]; !kept {
		t.Error("the last image shown is not kept")
	}

	again, err := renderer.nodeImage("0.png", side, side)
	if err != nil || again.Bounds().Dx() != side {
		t.Errorf("the first image shown again: %v, %v", again, err)
	}

	// A size drawn is kept and counted as the decoded image is.
	before := renderer.heldDrawn

	small, err := renderer.nodeImage("0.png", 16, 16)
	if err != nil || small.Bounds().Dx() != 16 {
		t.Fatalf("the first image at 16 pixels: %v, %v", small, err)
	}

	if grown := renderer.heldDrawn - before; grown != 4*16*16 {
		t.Errorf("an image scaled to 16 by 16 is counted %d bytes, want %d", grown, 4*16*16)
	}

	// Sixteen bits a channel are eight bytes a pixel.
	if got := imageBytes(image.NewNRGBA64(image.Rect(0, 0, 3, 2))); got != 8*3*2 {
		t.Errorf("a 3 by 2 image of sixteen bits a channel is counted %d bytes, want %d", got, 8*3*2)
	}
}

// More large images than the bound holds, each shown small by many nodes,
// are decoded once each: what a node shows is the copy at its size, and
// the copies stay when the decoded images go. With everything let go at
// once, ten images round-robin on sixty nodes were decoded sixty times.
func TestNodeImagesReusedAreDecodedOnce(t *testing.T) {
	const side = 2048

	count := maxImagesKept/(4*side*side) + 3
	files := largeImages(t, count, side)

	wasm.SetWasmFileSystem(files)
	defer wasm.SetWasmFileSystem(nil)

	renderer := newImageRenderer()

	for round := range 3 {
		for i := range count {
			img, err := renderer.nodeImage(fmt.Sprintf("%d.png", i), 16, 16)
			if err != nil {
				t.Fatal(err)
			}

			if img.Bounds().Dx() != 16 {
				t.Fatalf("round %d, image %d is %d pixels wide, want 16", round, i, img.Bounds().Dx())
			}

			if kept := renderer.heldDecoded + renderer.heldDrawn; kept > maxImagesKept {
				t.Fatalf("%d bytes of images kept, over the %d allowed", kept, maxImagesKept)
			}
		}
	}

	for name, opened := range files.opened {
		if opened != 1 {
			t.Errorf("%s was read %d times over three rounds, want once", name, opened)
		}
	}

	if len(files.opened) != count {
		t.Errorf("%d images were read, want %d", len(files.opened), count)
	}
}

// Room is made by letting the decoded images go, and the copies drawn only
// when they alone leave none.
func TestMakeRoomLetsTheDecodedGoFirst(t *testing.T) {
	pixel := image.NewGray(image.Rect(0, 0, 1, 1))

	renderer := newImageRenderer()
	renderer.decoded["a"], renderer.heldDecoded = pixel, maxImagesKept/2
	renderer.drawn["a|1x1"], renderer.heldDrawn = pixel, maxImagesKept/4

	renderer.makeRoom(maxImagesKept / 2)

	if len(renderer.decoded) != 0 || renderer.heldDecoded != 0 {
		t.Errorf(
			"the decoded images stay when there is no room: %d of them, %d bytes",
			len(renderer.decoded),
			renderer.heldDecoded,
		)
	}

	if len(renderer.drawn) != 1 || renderer.heldDrawn != maxImagesKept/4 {
		t.Errorf(
			"the copies drawn went though they left room: %d of them, %d bytes",
			len(renderer.drawn),
			renderer.heldDrawn,
		)
	}

	renderer.makeRoom(maxImagesKept)

	if len(renderer.drawn) != 0 || renderer.heldDrawn != 0 {
		t.Errorf(
			"the copies drawn stay when they alone leave no room: %d of them, %d bytes",
			len(renderer.drawn),
			renderer.heldDrawn,
		)
	}
}
