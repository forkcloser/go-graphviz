package graphviz_test

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/forkcloser/go-graphviz"
)

// RenderImage returns the page the renderer drew, pixel for pixel what a PNG
// render of the same graph decodes to.
func TestRenderImageMatchesPNG(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a -> b -> c; a -> c [color=red label=x] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	direct, err := g.RenderImage(ctx, graph)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if renderErr := g.Render(ctx, graph, graphviz.PNG, &buf); renderErr != nil {
		t.Fatal(renderErr)
	}

	decoded, _, err := image.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}

	if direct.Bounds() != decoded.Bounds() {
		t.Fatalf("RenderImage bounds %v, PNG bounds %v", direct.Bounds(), decoded.Bounds())
	}

	bounds := direct.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			want := color.NRGBAModel.Convert(decoded.At(x, y))
			if got := color.NRGBAModel.Convert(direct.At(x, y)); got != want {
				t.Fatalf("pixel (%d, %d): RenderImage %v, PNG %v", x, y, got, want)
			}
		}
	}
}

// A PNG render after RenderImage still produces PNG bytes: the renderer is
// told to skip encoding only for the image render.
func TestRenderAfterRenderImageEncodes(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a -> b }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	if _, err := g.RenderImage(ctx, graph); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.PNG, &buf); err != nil {
		t.Fatal(err)
	}

	if _, kind, err := image.Decode(&buf); err != nil || kind != "png" {
		t.Fatalf("the PNG render after RenderImage decoded as %q with %v", kind, err)
	}
}

// An image a callback cannot decode fails RenderImage with that error, and
// the instance renders an image afterwards.
func TestRenderImageCallbackError(t *testing.T) {
	ctx := t.Context()

	logo, err := os.ReadFile(filepath.Join("testdata", "logo.png"))
	if err != nil {
		t.Fatal(err)
	}

	graphviz.SetFileSystem(fstest.MapFS{"truncated.png": &fstest.MapFile{Data: logo[:200]}})
	t.Cleanup(func() { graphviz.SetFileSystem(nil) })

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	broken, err := graphviz.ParseBytes([]byte(`digraph { a [image="truncated.png" label=""] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, broken.Close) })

	if _, renderErr := g.RenderImage(ctx, broken); renderErr == nil ||
		!strings.Contains(renderErr.Error(), "truncated.png") {
		t.Fatalf("RenderImage of a truncated node image returned %v", renderErr)
	}

	fine, err := graphviz.ParseBytes([]byte(`digraph { a -> b }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, fine.Close) })

	if img, err := g.RenderImage(ctx, fine); err != nil || img.Bounds().Empty() {
		t.Fatalf("RenderImage after a callback error: %v", err)
	}
}

// RenderImage and Render run from several goroutines on one instance: each
// image is the one a lone RenderImage draws, and each PNG decodes.
func TestRenderImageConcurrent(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a -> b -> c -> a }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	want, err := g.RenderImage(ctx, graph)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup

	for worker := range 4 {
		wg.Go(func() {
			for range 10 {
				if worker%2 == 0 {
					img, err := g.RenderImage(ctx, graph)
					if err != nil || img.Bounds() != want.Bounds() {
						t.Errorf("worker %d: RenderImage %v, %v", worker, img.Bounds(), err)

						return
					}

					continue
				}

				var buf bytes.Buffer
				if err := g.Render(ctx, graph, graphviz.PNG, &buf); err != nil {
					t.Errorf("worker %d: Render: %v", worker, err)

					return
				}

				if _, _, err := image.Decode(&buf); err != nil {
					t.Errorf("worker %d: the PNG does not decode: %v", worker, err)

					return
				}
			}
		})
	}

	wg.Wait()
}
