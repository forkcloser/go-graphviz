package graphviz_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// The options given to Graph apply to that graph only: the next call opens a
// graph with the instance's defaults again, unnamed and directed.
func TestGraphOptionsApplyToOneGraph(t *testing.T) {
	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	first, err := g.Graph(graphviz.WithName("first"), graphviz.WithDirectedType(graphviz.UnDirected))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, first.Close) })

	second, err := g.Graph()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, second.Close) })

	dotOf := func(graph *graphviz.Graph) string {
		t.Helper()

		var buf bytes.Buffer
		if err := g.Render(t.Context(), graph, graphviz.GV, &buf); err != nil {
			t.Fatal(err)
		}

		return buf.String()
	}

	if out := dotOf(first); !strings.HasPrefix(out, "graph first {") {
		t.Errorf("the first graph is not the undirected graph named first:\n%s", out)
	}

	if out := dotOf(second); !strings.HasPrefix(out, `digraph "" {`) {
		t.Errorf("the second graph is not the instance's default, an unnamed digraph:\n%s", out)
	}
}
