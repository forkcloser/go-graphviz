package graphviz_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/image/font/opentype"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
)

var errFontRefused = errors.New("font refused")

// A layout that fails in a callback, a font loader's here, returns the
// callback's error with the graph laid out all the same: Graphviz goes on
// to the layout's end. Layout's doc has a caller free after nil and after
// gvc.ErrReported; one that returns on any other error left the layout in
// the module, some 700 bytes for one node.
func TestALayoutThatFailsLeavesNothing(t *testing.T) {
	ctx := t.Context()

	c, err := gvc.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, c.Close) })

	// The one font the graph below names, so that no other test's text
	// meets the refusal.
	const refused = "a font the loader refuses"

	graphviz.SetFontLoader(func(_ context.Context, textFont *graphviz.TextFont) (*opentype.Font, error) {
		if textFont.Name() == refused { //nolint:contextcheck // a TextFont's getters read memory and take no context
			return nil, errFontRefused
		}

		return nil, nil //nolint:nilnil // no font asks for the usual resolution
	})
	t.Cleanup(func() { graphviz.SetFontLoader(nil) })

	// One node, so one text to measure: a callback skipped after the one
	// that failed would leave its arguments behind, which is not this.
	growth := memoryGrowth(2000, func() {
		graph, err := graphviz.ParseBytes([]byte(`digraph { a [fontname="` + refused + `"] }`))
		if err != nil {
			t.Fatal(err)
		}

		if err := c.Layout(ctx, graph, "dot"); !errors.Is(err, errFontRefused) || errors.Is(err, gvc.ErrReported) {
			t.Fatalf("Layout returned %v, want the font loader's error and no gvc.ErrReported", err)
		}

		closeOrError(t, graph.Close)
	})

	if growth != 0 {
		t.Errorf("memory grew %d KiB over 2000 layouts that failed and were not freed by the caller", growth/1024)
	}
}

// A Layout whose context is done before the layout starts lays nothing out
// and frees nothing: the layout an earlier call left on the graph is the
// caller's still, to render and to free.
func TestALayoutThatDidNotStartFreesNothing(t *testing.T) {
	ctx := t.Context()

	c, err := gvc.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, c.Close) })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a -> b }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	if err := c.Layout(ctx, graph, "dot"); err != nil {
		t.Fatal(err)
	}

	done, cancel := context.WithCancel(ctx)
	cancel()

	if err := c.Layout(done, graph, "dot"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Layout under a context that is done returned %v, want context.Canceled", err)
	}

	var out bytes.Buffer
	if err := c.RenderData(ctx, graph, "svg", &out); err != nil {
		t.Errorf("the earlier layout does not render after it: %v", err)
	}

	if !strings.HasSuffix(strings.TrimSpace(out.String()), "</svg>") {
		t.Errorf("the render is not a whole SVG: %d bytes", out.Len())
	}

	if err := c.FreeLayout(ctx, graph); err != nil {
		t.Errorf("FreeLayout: %v", err)
	}
}

// FreeLayout frees the layout of a graph whose context was closed first, two
// deferred calls in the other order: Graphviz frees a layout without its
// context. It did nothing then, and the layout stayed in the module, some
// 2.7 KiB for three nodes.
func TestFreeLayoutAfterItsContextClosed(t *testing.T) {
	ctx := t.Context()

	growth := memoryGrowth(2000, func() {
		c, err := gvc.New(ctx)
		if err != nil {
			t.Fatal(err)
		}

		graph, err := graphviz.ParseBytes([]byte(`digraph { a -> b -> c }`))
		if err != nil {
			t.Fatal(err)
		}

		if err := c.Layout(ctx, graph, "dot"); err != nil {
			t.Fatal(err)
		}

		closeOrError(t, c.Close)

		if err := c.FreeLayout(ctx, graph); err != nil {
			t.Fatalf("FreeLayout after Close: %v", err)
		}

		closeOrError(t, graph.Close)
	})

	if growth != 0 {
		t.Errorf("memory grew %d KiB over 2000 layouts freed after their context closed", growth/1024)
	}
}
