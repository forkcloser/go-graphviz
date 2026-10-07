package graphviz_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io/fs"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"golang.org/x/image/bmp"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
)

// solidImage is a w by h image of one colour.
func solidImage(w, h int, c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, c)
		}
	}

	return img
}

func encodePNG(t *testing.T, img image.Image) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// isRedish tolerates JPEG's loss.
func isRedish(c color.NRGBA) bool { return c.A > 200 && c.R > 180 && c.G < 90 && c.B < 90 }

// redBounds is the bounding box of img's red pixels.
func redBounds(img image.Image) image.Rectangle {
	var found image.Rectangle

	bounds := img.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if c, ok := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); ok && isRedish(c) {
				found = found.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}

	return found
}

// renderWithImages renders dot to an image of format with the files of
// images visible to Graphviz.
func renderWithImages(t *testing.T, images fs.FS, dot string, format graphviz.Format) image.Image {
	t.Helper()

	graphviz.SetFileSystem(images)
	t.Cleanup(func() { graphviz.SetFileSystem(nil) })

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var out bytes.Buffer
	if err = g.Render(t.Context(), graph, format, &out); err != nil {
		t.Fatal(err)
	}

	img, _, err := image.Decode(&out)
	if err != nil {
		t.Fatal(err)
	}

	return img
}

// A node's image fills the box Graphviz computes from imagescale, centred:
// both stretches it to the node, true keeps its aspect ratio, width
// stretches only its width. The renderer used to apply its own rules, with
// a fixed padding that pushed a scaled image out of its node.
func TestRasterNodeImageFillsItsBox(t *testing.T) {
	// A square, 72 pixels at 96 dpi: 54 points, in a node of 2 by 1 inch,
	// 192 by 96 pixels.
	images := fstest.MapFS{"red.png": {Data: encodePNG(t, solidImage(72, 72, color.RGBA{R: 255, A: 255}))}}

	const node = `shape=box width=2 height=1 fixedsize=true label="" color=white`

	for _, tc := range []struct {
		scale         string
		width, height int
	}{
		{scale: "both", width: 192, height: 96},
		{scale: "true", width: 96, height: 96},
		{scale: "width", width: 192, height: 72},
	} {
		img := renderWithImages(t, images,
			`digraph { a [`+node+` image="red.png" imagescale=`+tc.scale+`] }`, graphviz.PNG)

		got := redBounds(img)
		page := img.Bounds()
		centre := image.Pt(page.Dx()/2, page.Dy()/2)
		gotCentre := image.Pt((got.Min.X+got.Max.X)/2, (got.Min.Y+got.Max.Y)/2)

		if math.Abs(float64(got.Dx()-tc.width)) > 2 || math.Abs(float64(got.Dy()-tc.height)) > 2 ||
			math.Abs(float64(gotCentre.X-centre.X)) > 2 || math.Abs(float64(gotCentre.Y-centre.Y)) > 2 {
			t.Errorf("imagescale=%s: image drawn at %v on a %v page, want %dx%d centred",
				tc.scale, got, page, tc.width, tc.height)
		}
	}
}

// Every image type Graphviz recognizes and Go decodes, WebP included, is
// drawn, into PNG and JPEG output alike: there was a loader for a PNG image into a PNG
// page only, and the others drew nothing.
func TestRasterNodeImageFormats(t *testing.T) {
	red := solidImage(40, 40, color.RGBA{R: 255, A: 255})

	encoded := map[string][]byte{"png": encodePNG(t, red)}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, red, nil); err != nil {
		t.Fatal(err)
	}

	encoded["jpg"] = bytes.Clone(buf.Bytes())

	buf.Reset()

	if err := gif.Encode(&buf, red, nil); err != nil {
		t.Fatal(err)
	}

	encoded["gif"] = bytes.Clone(buf.Bytes())

	buf.Reset()

	if err := bmp.Encode(&buf, red); err != nil {
		t.Fatal(err)
	}

	encoded["bmp"] = bytes.Clone(buf.Bytes())

	// x/image decodes WebP but cannot write it: testdata/red.webp is the
	// same 40 by 40 red square, written by libwebp's cwebp 1.6 at quality
	// 100. It is lossy (VP8): Graphviz 16 sizes a lossless (VP8L) WebP
	// from the wrong bytes, tens of thousands of points a side, as dot does.
	webp, err := os.ReadFile("testdata/red.webp")
	if err != nil {
		t.Fatal(err)
	}

	encoded["webp"] = webp

	for extension, data := range encoded {
		for _, format := range []graphviz.Format{graphviz.PNG, graphviz.JPG} {
			name := "red." + extension
			img := renderWithImages(t, fstest.MapFS{name: {Data: data}},
				`digraph { a [shape=box label="" color=white image="`+name+`"] }`, format)

			if got := redBounds(img); got.Dx() < 35 || got.Dy() < 35 {
				t.Errorf("a %s image in %s output drawn at %v, want about 40 by 40 pixels", extension, format, got)
			}
		}
	}
}

// countingFS counts the opens of each file.
type countingFS struct {
	fstest.MapFS

	opens atomic.Int64
}

func (c *countingFS) Open(name string) (fs.File, error) {
	if !strings.HasSuffix(name, ".") {
		c.opens.Add(1)
	}

	return c.MapFS.Open(name)
}

// An image shown by many nodes is read and decoded once per page, not
// once per node.
func TestRasterNodeImageDecodedOnce(t *testing.T) {
	data := encodePNG(t, solidImage(20, 20, color.RGBA{R: 255, A: 255}))

	opens := func(nodes int) int64 {
		images := &countingFS{MapFS: fstest.MapFS{"red.png": {Data: data}}}

		var dot strings.Builder

		dot.WriteString(`digraph { node [shape=box label="" image="red.png"]; `)

		for i := range nodes {
			dot.WriteString("n" + string(rune('a'+i)) + "; ")
		}

		dot.WriteString("}")
		renderWithImages(t, images, dot.String(), graphviz.PNG)

		return images.opens.Load()
	}

	if one, five := opens(1), opens(5); five != one {
		t.Errorf("the image file opened %d times for five nodes, %d for one", five, one)
	}
}

// declaringPNG is a 1 by 1 PNG whose header declares width by height.
func declaringPNG(t *testing.T, width, height uint32) []byte {
	t.Helper()

	data := encodePNG(t, solidImage(1, 1, color.RGBA{R: 255, A: 255}))

	// The IHDR chunk follows the 8-byte signature: length, type, then the
	// width and height, and its CRC over type and data.
	const ihdr, ihdrLength = 8, 13

	binary.BigEndian.PutUint32(data[ihdr+8:], width)
	binary.BigEndian.PutUint32(data[ihdr+12:], height)
	binary.BigEndian.PutUint32(data[ihdr+8+ihdrLength:], crc32.ChecksumIEEE(data[ihdr+4:ihdr+8+ihdrLength]))

	return data
}

// An image whose header declares more pixels than MaxImagePixels is
// refused before it is decoded: the file and its name come from the
// graph, and decoding a small file declaring 60000 by 60000 pixels would
// allocate about 14 GB.
func TestRasterNodeImageTooLarge(t *testing.T) {
	graphviz.SetFileSystem(fstest.MapFS{"huge.png": {Data: declaringPNG(t, 60000, 60000)}})
	t.Cleanup(func() { graphviz.SetFileSystem(nil) })

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	// The node is fixed at an inch, so the page stays small and only the
	// image declares a large size.
	const dot = `digraph { a [shape=box label="" image="huge.png" width=1 height=1 fixedsize=true imagescale=true] }`

	graph, err := graphviz.ParseBytes([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var out bytes.Buffer
	if err = g.Render(t.Context(), graph, graphviz.PNG, &out); !errors.Is(err, gvc.ErrImageTooLarge) {
		t.Fatalf("Render returned %v, want %v", err, gvc.ErrImageTooLarge)
	}
}
