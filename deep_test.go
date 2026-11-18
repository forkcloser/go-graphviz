package graphviz_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// Clusters nested a thousand deep are drawn, as Graphviz's own dot draws
// them: the layout recurses once a cluster, and on the 64 KiB of stack the
// module was linked with it trapped ("out of bounds memory access" in
// rank2), where dot has the 8 MiB of a main thread.
func TestClustersNestedDeep(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	const depth = 1000

	var source strings.Builder

	source.WriteString("digraph {")

	for i := range depth {
		fmt.Fprintf(&source, " subgraph cluster_%d {", i)
	}

	source.WriteString(" a -> b ")
	source.WriteString(strings.Repeat("}", depth))
	source.WriteString("}")

	graph, err := graphviz.ParseBytes([]byte(source.String()))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var out bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.SVG, &out); err != nil {
		t.Fatalf("%d nested clusters: %v", depth, err)
	}

	if clusters := bytes.Count(out.Bytes(), []byte(`class="cluster"`)); clusters != depth {
		t.Errorf("the SVG draws %d clusters, want %d", clusters, depth)
	}
}
