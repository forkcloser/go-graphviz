package graphviz_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/forkcloser/go-graphviz/cgraph"
	"github.com/forkcloser/go-graphviz/gvc"
)

// layoutAndFree parses dot, lays it out and frees the layout in a fresh
// context, rounds times. Each round allocates the label's span array anew,
// which is what exposes a span that was never zeroed.
func layoutAndFree(t *testing.T, dot []byte, rounds int) {
	t.Helper()

	ctx := t.Context()

	for round := range rounds {
		graph, err := cgraph.ParseBytes(dot)
		if err != nil {
			t.Fatalf("round %d: parsing: %v", round, err)
		}

		gctx, err := gvc.New(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if err := gctx.Layout(ctx, graph, "dot"); err != nil {
			t.Fatalf("round %d: layout: %v", round, err)
		}

		if err := gctx.FreeLayout(ctx, graph); err != nil {
			t.Fatalf("round %d: freeing the layout: %v", round, err)
		}

		if err := gctx.Close(); err != nil {
			t.Fatal(err)
		}

		if err := graph.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// A label whose first line is empty is laid out and freed cleanly. Graphviz
// 16.1.0's storeline never zeroes the first span of a label, and an empty
// line skips the sizing that would null its layout fields, so freeing the
// label called through whatever the allocator left there; the wasm build
// carries the one-line fix (internal/wasm/build/build.sh). The crazy.gv
// corpus file is the graph that showed it, two rounds being enough for the
// allocator to hand the span array a dirty block. FreeLayout reports the
// trap since callback errors and traps stopped being swallowed.
func TestLabelEmptyFirstLine(t *testing.T) {
	layoutAndFree(t, []byte(`digraph { label="\n\nfirst line empty"; a -> b }`), 20)

	crazy, err := os.ReadFile(filepath.Join("testdata", "directed", "crazy.gv"))
	if err != nil {
		t.Fatal(err)
	}

	layoutAndFree(t, crazy, 3)
}
