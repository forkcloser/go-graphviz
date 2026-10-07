package cgraph_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/cgraph"
)

var (
	errFirstSetter  = errors.New("first")
	errSecondSetter = errors.New("second")
)

// The first error a typed setter meets anywhere in a graph is kept against
// its root: Err returns it from the graph and its subgraphs, a render
// returns it before laying out, other graphs do not see it, and closing the
// root forgets it.
func TestSetterErrors(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = g.Close() }()

	graph, err := graphviz.ParseBytes([]byte(`digraph { subgraph s { a -> b } c }`))
	if err != nil {
		t.Fatal(err)
	}

	other, err := graphviz.ParseBytes([]byte(`digraph { x }`))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = other.Close() }()

	if setErr := graph.Err(); setErr != nil {
		t.Fatalf("a new graph has setter error %v", setErr)
	}

	sub, err := graph.SubGraphByName("s")
	if err != nil || sub == nil {
		t.Fatalf("subgraph s: %v", err)
	}

	node, err := sub.NodeByName("a")
	if err != nil || node == nil {
		t.Fatalf("node a: %v", err)
	}

	edge, err := graph.FirstOut(node)
	if err != nil || edge == nil {
		t.Fatalf("edge a -> b: %v", err)
	}

	first, second := errFirstSetter, errSecondSetter
	cgraph.InjectSetterError(edge, first)
	cgraph.InjectSetterError(node, second)

	for name, holder := range map[string]*graphviz.Graph{"root": graph, "subgraph": sub} {
		if setErr := holder.Err(); !errors.Is(setErr, first) {
			t.Errorf("%s: Err() = %v, want the first error", name, setErr)
		}
	}

	if setErr := other.Err(); setErr != nil {
		t.Errorf("another graph sees setter error %v", setErr)
	}

	var buf bytes.Buffer
	if err = g.Render(ctx, graph, graphviz.SVG, &buf); !errors.Is(err, first) {
		t.Errorf("Render = %v, want the setter error", err)
	}

	if err = graph.Close(); err != nil {
		t.Fatal(err)
	}

	again, err := graphviz.ParseBytes([]byte(`digraph { subgraph s { a -> b } c }`))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = again.Close() }()

	if setErr := again.Err(); setErr != nil {
		t.Errorf("a graph made after the root closed has setter error %v", setErr)
	}

	if err = g.Render(ctx, again, graphviz.SVG, &buf); err != nil {
		t.Errorf("Render of a clean graph: %v", err)
	}
}

// Closing a root graph forgets its setter error. The module reuses a closed
// graph's address for the next one, which would otherwise inherit it.
func TestSetterErrorForgottenOnClose(t *testing.T) {
	const dot = `digraph { a -> b }`

	closed, err := graphviz.ParseBytes([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}

	address := cgraph.RootAddress(closed)
	cgraph.InjectSetterError(closed, errFirstSetter)

	if err = closed.Close(); err != nil {
		t.Fatal(err)
	}

	reused, err := graphviz.ParseBytes([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = reused.Close() }()

	if cgraph.RootAddress(reused) != address {
		t.Fatalf(
			"the new graph is at %#x, the closed one was at %#x: the test needs the address reused",
			cgraph.RootAddress(reused),
			address,
		)
	}

	if setErr := reused.Err(); setErr != nil {
		t.Errorf("a graph at a closed graph's address has its setter error %v", setErr)
	}
}
