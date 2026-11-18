package graphviz_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/cgraph"
)

// nestedClusters is a digraph of one edge inside clusters nested depth deep.
func nestedClusters(depth int) []byte {
	var source strings.Builder

	source.WriteString("digraph {")

	for i := range depth {
		fmt.Fprintf(&source, " subgraph cluster_%d {", i)
	}

	source.WriteString(" a -> b ")
	source.WriteString(strings.Repeat("}", depth))
	source.WriteString("}")

	return []byte(source.String())
}

// A parse Graphviz gives up on is an error, as it is for dot, which prints
// "memory exhausted" and exits non-zero. Its parser, out of its own stack,
// hands back the graph as far as it got; ParseBytes returned that with no
// error, one subgraph and no node, and a render drew an empty page.
func TestParseGraphvizGivesUpOnIsAnError(t *testing.T) {
	graph, err := graphviz.ParseBytes(nestedClusters(4000))
	if graph != nil {
		t.Error("a parse Graphviz gave up on returned a graph")

		closeOrError(t, graph.Close)
	}

	if !errors.Is(err, cgraph.ErrGraphviz) || !strings.Contains(fmt.Sprint(err), "memory exhausted") {
		t.Errorf("a parse Graphviz gave up on returned %v, want Graphviz's \"memory exhausted\"", err)
	}

	// The next parse is its own: nothing of the failed one is left for it.
	next, err := graphviz.ParseBytes([]byte(`digraph { a -> b }`))
	if err != nil {
		t.Fatalf("the parse after it: %v", err)
	}

	t.Cleanup(func() { closeOrError(t, next.Close) })

	if nodes, numErr := next.NodeNum(); numErr != nil || nodes != 2 {
		t.Errorf("the parse after it has %d nodes (%v), want 2", nodes, numErr)
	}

	// Clusters nested as deep as the parser goes are a graph.
	deep, err := graphviz.ParseBytes(nestedClusters(1000))
	if err != nil {
		t.Fatalf("clusters nested 1000 deep: %v", err)
	}

	closeOrError(t, deep.Close)
}

// What an earlier call left unread is not this parse's: a render Graphviz
// reported an error for and still returned from leaves its message behind,
// and the parse after it is a good one.
func TestParseAfterAnErrorLeftUnread(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	bad, err := graphviz.ParseBytes([]byte(`digraph { a [shape=record, label="{a|"] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, bad.Close) })

	var out bytes.Buffer

	// Graphviz reports "bad label format" and draws the node all the same.
	_ = g.Render(ctx, bad, graphviz.SVG, &out)

	next, err := graphviz.ParseBytes([]byte(`digraph { a -> b }`))
	if err != nil {
		t.Fatalf("a parse after a render that reported an error: %v", err)
	}

	closeOrError(t, next.Close)
}

// The graph of a parse Graphviz gave up on is closed, not left: forty of
// them, of clusters nested 4000 deep, grow the module's memory by what the
// parser itself leaves behind, a few MiB, where the graphs held 80 more.
func TestAbandonedParsesCloseTheirGraph(t *testing.T) {
	source := nestedClusters(4000)

	growth := memoryGrowth(40, func() {
		if graph, err := graphviz.ParseBytes(source); err == nil {
			t.Error("a parse Graphviz gave up on returned no error")

			closeOrError(t, graph.Close)
		}
	})

	const limit = 16 << 20

	if growth > limit {
		t.Errorf("memory grew %d MiB over 40 abandoned parses, want under %d", growth>>20, limit>>20)
	}
}

// A warning is no error: the graph it was reported for is returned.
func TestParseWithAWarningIsAGraph(t *testing.T) {
	var warnings strings.Builder

	graphviz.SetWarningWriter(&warnings)
	defer graphviz.SetWarningWriter(nil)

	graph, err := graphviz.ParseBytes([]byte(`digraph { 1a }`))
	if err != nil {
		t.Fatalf("a parse Graphviz warned about: %v", err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	if warnings.Len() == 0 {
		t.Error("Graphviz did not warn about `1a`: the test measures nothing")
	}
}
