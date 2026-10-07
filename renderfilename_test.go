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

	dir := t.TempDir()

	// Each format renders its own graph: a GV or XDOT render writes its
	// attributes onto the graph it renders (see TestRenderWritesOntoGraph).
	for _, format := range []graphviz.Format{graphviz.XDOT, graphviz.GV, graphviz.SVG, graphviz.PNG, graphviz.JPG} {
		graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { closeOrError(t, graph.Close) })

		path := filepath.Join(dir, "out."+string(format))

		if err = g.RenderFilename(ctx, graph, format, path); err != nil {
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
		case graphviz.GV, graphviz.XDOT:
			if !strings.Contains(string(data), "digraph") {
				t.Fatalf("%s: no graph in %d bytes", format, len(data))
			}

			// xdot is DOT with Graphviz's drawing operations; plain DOT has none.
			if drawn := strings.Contains(string(data), "_draw_"); drawn != (format == graphviz.XDOT) {
				t.Fatalf("%s: drawing operations present is %t", format, drawn)
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

	unwritten, parseErr := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if parseErr != nil {
		t.Fatal(parseErr)
	}

	t.Cleanup(func() { closeOrError(t, unwritten.Close) })

	if err := g.RenderFilename(ctx, unwritten, graphviz.SVG, filepath.Join(dir, "no-such-dir", "out.svg")); err == nil {
		t.Fatal("writing into a missing directory returned no error")
	}
}

// A GV or XDOT render writes the layout onto the graph as attributes, as
// Graphviz does (pos, and for XDOT the _draw_ operations), and they stay: a
// GV render after an XDOT render of the same graph carries the drawing
// operations. A clean GV needs a graph no XDOT render has touched.
func TestRenderWritesOntoGraph(t *testing.T) {
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

	render := func(format graphviz.Format) string {
		t.Helper()

		var buf bytes.Buffer
		if err := g.Render(ctx, graph, format, &buf); err != nil {
			t.Fatal(err)
		}

		return buf.String()
	}

	if out := render(graphviz.GV); strings.Contains(out, "_draw_") || !strings.Contains(out, "pos=") {
		t.Fatalf("a first GV render is not plain DOT with the layout:\n%s", out)
	}

	render(graphviz.XDOT)

	if out := render(graphviz.GV); !strings.Contains(out, "_draw_") {
		t.Fatalf("a GV render after an XDOT render lost the drawing operations:\n%s", out)
	}
}
