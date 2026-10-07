package graphviz_test

import (
	"os"
	"testing"
	"testing/fstest"

	"github.com/forkcloser/go-graphviz"
)

// A WebP node image is sized as it is, whichever of the format's three
// layouts it uses: Graphviz read every size from where only a plain lossy
// WebP keeps it, and laid a lossless or extended one out tens of thousands
// to hundreds of millions of points a side, as dot 16.1.0 does. The
// fixtures, written by libwebp's cwebp 1.6, are 40 by 40 pixels, all red
// but the extended one, red on its left half and transparent on its right.
func TestRasterWebPSizes(t *testing.T) {
	for _, tc := range []struct {
		file          string
		width, height int
	}{
		{"lossy.webp", 40, 40},
		{"lossless.webp", 40, 40},
		{"extended.webp", 20, 40},
	} {
		data, err := os.ReadFile("testdata/webp/" + tc.file)
		if err != nil {
			t.Fatal(err)
		}

		img := renderWithImages(t, fstest.MapFS{tc.file: {Data: data}},
			`digraph { a [shape=box label="" color=white image="`+tc.file+`"] }`, graphviz.PNG)

		if got := redBounds(img); abs(got.Dx()-tc.width) > 2 || abs(got.Dy()-tc.height) > 2 {
			t.Errorf("%s drawn at %v on a %v page, want %d by %d red pixels",
				tc.file, got, img.Bounds(), tc.width, tc.height)
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}

	return n
}
