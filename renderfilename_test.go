package graphviz_test

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// RenderFilename writes the rendered graph, in every format, to the file
// named, and reports a file it cannot write.
func TestRenderFilename(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	dir := t.TempDir()

	for _, format := range []graphviz.Format{graphviz.SVG, graphviz.XDOT, graphviz.PNG, graphviz.JPG} {
		path := filepath.Join(dir, "out."+string(format))

		if err := g.RenderFilename(ctx, graph, format, path); err != nil {
			t.Fatalf("%s: %v", format, err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}

		switch format {
		case graphviz.SVG:
			if !strings.Contains(string(data), "<svg") {
				t.Fatalf("%s: no svg element in %d bytes", format, len(data))
			}
		case graphviz.XDOT:
			if !strings.Contains(string(data), "digraph") {
				t.Fatalf("%s: no graph in %d bytes", format, len(data))
			}
		case graphviz.PNG, graphviz.JPG:
			want := "png"
			if format == graphviz.JPG {
				want = "jpeg"
			}

			if _, kind, err := image.Decode(bytes.NewReader(data)); err != nil || kind != want {
				t.Fatalf("%s: decoded as %q with %v", format, kind, err)
			}
		}
	}

	if err := g.RenderFilename(ctx, graph, graphviz.SVG, filepath.Join(dir, "no-such-dir", "out.svg")); err == nil {
		t.Fatal("writing into a missing directory returned no error")
	}
}
