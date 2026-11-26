package graphviz_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
)

// The process renders the graph it laid out last and no other: Graphviz
// keeps a layout's state in variables of its own, and a render of another
// graph from them trapped the module, for every call after it in the
// process. Such a render is refused with gvc.ErrNotLaidOutLast, nothing is
// written, and the engine goes on: the graph laid out last renders, the
// other is freed, and laid out again it renders.
func TestRenderOfAGraphThatIsNotLaidOutLastIsRefused(t *testing.T) {
	ctx := t.Context()

	c, err := gvc.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, c.Close) })

	parsed := func(dot string) *graphviz.Graph {
		graph, parseErr := graphviz.ParseBytes([]byte(dot))
		if parseErr != nil {
			t.Fatal(parseErr)
		}

		t.Cleanup(func() { closeOrError(t, graph.Close) })

		return graph
	}

	// The second has attributes the first has not, which is what made the
	// render of the first read symbols that are not its own.
	first := parsed(`digraph { a -> b }`)
	second := parsed(
		`digraph { node [peripheries=2, style=filled, fontname="x", shape=box]; edge [color=red]; c -> d }`,
	)

	rendered := func(graph *graphviz.Graph, format string) (string, error) {
		var out bytes.Buffer

		renderErr := c.RenderData(ctx, graph, format, &out)

		return out.String(), renderErr
	}

	for _, graph := range []*graphviz.Graph{first, second} {
		if err = c.Layout(ctx, graph, "dot"); err != nil {
			t.Fatal(err)
		}
	}

	for _, format := range []string{"svg", "png", "dot"} {
		if out, renderErr := rendered(first, format); !errors.Is(renderErr, gvc.ErrNotLaidOutLast) || out != "" {
			t.Fatalf("%s of the first after the second's layout: %v and %d bytes, want gvc.ErrNotLaidOutLast and none",
				format, renderErr, len(out))
		}
	}

	if _, imageErr := c.RenderImage(ctx, first, "png"); !errors.Is(imageErr, gvc.ErrNotLaidOutLast) {
		t.Errorf("RenderImage of the first: %v, want gvc.ErrNotLaidOutLast", imageErr)
	}

	if out, renderErr := rendered(second, "svg"); renderErr != nil || !strings.Contains(out, "</svg>") {
		t.Fatalf("the second, laid out last: %v, %d bytes", renderErr, len(out))
	}

	// Laid out again, the first is the one to render, and the second is not.
	if err = c.FreeLayout(ctx, first); err != nil {
		t.Fatal(err)
	}

	if err = c.Layout(ctx, first, "neato"); err != nil {
		t.Fatal(err)
	}

	if out, renderErr := rendered(first, "svg"); renderErr != nil || !strings.Contains(out, "</svg>") {
		t.Fatalf("the first, laid out again: %v, %d bytes", renderErr, len(out))
	}

	if _, renderErr := rendered(second, "svg"); !errors.Is(renderErr, gvc.ErrNotLaidOutLast) {
		t.Errorf("the second after the first's new layout: %v, want gvc.ErrNotLaidOutLast", renderErr)
	}

	// A layout that fails leaves no graph to render.
	if err = c.Layout(ctx, parsed(`digraph { e }`), "no-such-layout"); err == nil {
		t.Fatal("a layout nobody has laid a graph out")
	}

	if _, renderErr := rendered(first, "svg"); !errors.Is(renderErr, gvc.ErrNotLaidOutLast) {
		t.Errorf("the first after a layout that failed: %v, want gvc.ErrNotLaidOutLast", renderErr)
	}

	for _, graph := range []*graphviz.Graph{first, second} {
		if err = c.FreeLayout(ctx, graph); err != nil {
			t.Errorf("freeing a layout that is not the last: %v", err)
		}
	}

	// No graph at all is Graphviz's to refuse, as it was.
	if _, renderErr := rendered(nil, "svg"); renderErr == nil || errors.Is(renderErr, gvc.ErrNotLaidOutLast) {
		t.Errorf("a render of no graph: %v, want Graphviz's own refusal", renderErr)
	}

	// The instance's own renders lay out and render in one turn: they are
	// not concerned, and the engine is whole.
	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	var out bytes.Buffer
	if err = g.Render(ctx, first, graphviz.SVG, &out); err != nil || !strings.Contains(out.String(), "</svg>") {
		t.Errorf("Render after it all: %v, %d bytes", err, out.Len())
	}
}

// A graph laid out, another laid out since, the first laid out again: it is
// drawn with its image as the file is at that layout.
func TestImageDrawnAfterAnotherGraphsLayout(t *testing.T) {
	const (
		side = 40
		with = `digraph { a [shape=box label="" color=white image="kept.png"] }`
	)

	ctx := t.Context()

	graphviz.SetFileSystem(fstest.MapFS{
		"kept.png": {Data: encodePNG(t, solidImage(side, side, color.RGBA{R: 255, A: 255}))},
	})
	t.Cleanup(func() { graphviz.SetFileSystem(nil) })

	c, err := gvc.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, c.Close) })

	for name, since := range map[string]string{
		"a graph with no image":  `digraph { b }`,
		"a graph with the image": with,
	} {
		graph, parseErr := graphviz.ParseBytes([]byte(with))
		if parseErr != nil {
			t.Fatal(parseErr)
		}

		other, parseErr := graphviz.ParseBytes([]byte(since))
		if parseErr != nil {
			t.Fatal(parseErr)
		}

		var out bytes.Buffer

		for _, step := range []func() error{
			func() error { return c.Layout(ctx, graph, "dot") },
			func() error { return c.Layout(ctx, other, "dot") },
			func() error { return c.FreeLayout(ctx, graph) },
			func() error { return c.Layout(ctx, graph, "dot") },
			func() error { return c.RenderData(ctx, graph, "png", &out) },
			func() error { return c.FreeLayout(ctx, graph) },
			func() error { return c.FreeLayout(ctx, other) },
			graph.Close,
			other.Close,
		} {
			if stepErr := step(); stepErr != nil {
				t.Fatalf("%s laid out since: %v", name, stepErr)
			}
		}

		img, _, decodeErr := image.Decode(&out)
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}

		if got := redBounds(img); math.Abs(float64(got.Dx()-side)) > 2 || math.Abs(float64(got.Dy()-side)) > 2 {
			t.Errorf("%s laid out since: the image is drawn %d by %d, want %d a side", name, got.Dx(), got.Dy(), side)
		}
	}
}
