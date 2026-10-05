package graphviz_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// renderString lays out and renders graph in format with a fresh instance.
func renderString(t *testing.T, graph *graphviz.Graph, format graphviz.Format) string {
	t.Helper()

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	var buf bytes.Buffer
	if err := g.Render(t.Context(), graph, format, &buf); err != nil {
		t.Fatal(err)
	}

	return buf.String()
}

// Parsing leaves the graph as written: an explicit empty label, the usual
// way to draw an image node without text, stays empty.
func TestParseKeepsLabelsAsWritten(t *testing.T) {
	graph, err := graphviz.ParseBytes([]byte(`digraph { a -> b; c [label=""] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	node, err := graph.NodeByName("c")
	if err != nil || node == nil {
		t.Fatalf("node c: %v", err)
	}

	if label := node.Label(); label != "" {
		t.Errorf("the explicit empty label of c reads %q after parsing", label)
	}
}

// A graph parsed before any instance exists still draws each node's name:
// Graphviz falls back to the node name when no label is declared.
func TestParsedNodesShowTheirNames(t *testing.T) {
	graph, err := graphviz.ParseBytes([]byte(`digraph { alpha -> beta }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	out := renderString(t, graph, graphviz.SVG)

	for _, name := range []string{">alpha</text>", ">beta</text>"} {
		if !strings.Contains(out, name) {
			t.Errorf("the SVG has no %s:\n%s", name, out)
		}
	}
}
