package wasm_test

import (
	"bytes"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// A NULL-terminated array argument that is itself NULL is read as empty,
// whatever lies at address 0: the bridge scanned it from address 0, which
// held zero by luck. The text-layout callback is called with such an
// array, Graphviz's fontpath, for every label laid out.
func TestNullArrayArgument(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = g.Close() })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a [label="laid out"] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = graph.Close() })

	restore := wasm.PoisonNullPage()
	defer restore()

	var out bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.SVG, &out); err != nil {
		t.Fatalf("render with a NULL fontpath and ones at address 0: %v", err)
	}

	if !bytes.Contains(out.Bytes(), []byte("laid out")) {
		t.Error("the label is missing from the SVG")
	}
}
