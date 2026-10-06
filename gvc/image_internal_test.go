package gvc

import (
	"errors"
	"strings"
	"testing"
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
