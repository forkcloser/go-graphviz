package graphviz_test

import (
	"bytes"
	"errors"
	"image/color"
	"testing"
	"testing/fstest"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
)

// A render leaves no page behind: the canvas a PNG or a JPEG is drawn on,
// 16 MB for the four megapixels here, and the node images decoded for it
// are the renderer's until the render is over and no longer, whether the
// page was finished or a node image stopped it half way. Not parallel: the
// heap is the process's.
func TestRenderKeepsNoPage(t *testing.T) {
	ctx := t.Context()

	graphviz.SetFileSystem(fstest.MapFS{
		// Two megapixels, 9 MB decoded.
		"large.png": {Data: encodePNG(t, solidImage(1500, 1500, color.RGBA{B: 255, A: 255}))},
		"huge.png":  {Data: declaringPNG(t, 60000, 60000)},
	})
	t.Cleanup(func() { graphviz.SetFileSystem(nil) })

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	render := func(src string, format graphviz.Format) (int, error) {
		graph, err := graphviz.ParseBytes([]byte(src))
		if err != nil {
			t.Fatal(err)
		}

		defer closeOrError(t, graph.Close)

		var out bytes.Buffer

		err = g.Render(ctx, graph, format, &out)

		return out.Len(), err
	}

	// The first render loads what every render shares.
	if _, err := render(`digraph { a }`, graphviz.PNG); err != nil {
		t.Fatal(err)
	}

	const (
		// Twenty inches a side at 100 dots an inch.
		page = `digraph { size="20,20!"; dpi=100; a -> b }`
		// The same page, with a node image the renderer draws and then one
		// it refuses: the render fails once the page is begun and the first
		// image decoded.
		stopped = `digraph { size="20,20!"; dpi=100; a -> b
			node [shape=box label="" width=1 height=1 fixedsize=true imagescale=true]
			a [image="large.png"]; b [image="huge.png"] }`
	)

	for _, format := range []graphviz.Format{graphviz.PNG, graphviz.JPG} {
		before := heapKept()

		if size, err := render(page, format); err != nil || size == 0 {
			t.Fatalf("%s: %d bytes, %v", format, size, err)
		}

		if after := heapKept(); after > before+4<<20 {
			t.Errorf("%s: the render kept %d KiB", format, (after-before)>>10)
		}

		before = heapKept()

		if _, err := render(stopped, format); !errors.Is(err, gvc.ErrImageTooLarge) {
			t.Fatalf("%s: the page with the image: %v, want %v", format, err, gvc.ErrImageTooLarge)
		}

		if after := heapKept(); after > before+4<<20 {
			t.Errorf("%s: the render a node image stopped kept %d KiB", format, (after-before)>>10)
		}
	}
}
