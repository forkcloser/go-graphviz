package graphviz_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
)

// renderPNG renders dot to PNG and returns the error.
func renderPNG(t *testing.T, dot string) error {
	t.Helper()

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

	return g.Render(t.Context(), graph, graphviz.PNG, &out)
}

// A page over MaxPagePixels is refused before its canvas is allocated: a
// graph that asks for a 200-inch page at 300 dpi, 60000 by 60000 pixels,
// would take about 14 GB. A large page within the budget still renders.
func TestRasterPageBudget(t *testing.T) {
	if err := renderPNG(t, `digraph { size="200,200!"; dpi=300; a -> b }`); !errors.Is(err, gvc.ErrPageTooLarge) {
		t.Errorf("a 60000 by 60000 page rendered with %v, want %v", err, gvc.ErrPageTooLarge)
	}

	if err := renderPNG(t, `digraph { size="20,20!"; dpi=300; a -> b }`); err != nil {
		t.Errorf("a 6000 by 6000 page failed: %v", err)
	}
}
