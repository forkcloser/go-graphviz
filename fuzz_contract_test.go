package graphviz_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/forkcloser/go-graphviz"
)

// FuzzParseAndRender: whatever the bytes, ParseBytes returns a graph or an
// error and never panics; a graph it returns lays out and renders to gv
// and svg, or errs, within a bound, and never panics; the engine survives
// to render the next graph.
func FuzzParseAndRender(f *testing.F) {
	for _, seed := range []string{
		"digraph G { a -> b; b -> c; a -> c [label=\"x\"]; }",
		"graph { a -- b -- c; subgraph cluster_0 { label=\"box\"; d; e } }",
		"digraph { node [shape=record]; s [label=\"<f0> left|<f1> mid|<f2> right\"]; s:f0 -> s:f2 }",
		"digraph { rankdir=LR; a [label=<<b>bold</b> html>]; a -> a }",
		"strict digraph { a -> b; a -> b; b -> a }",
		"digraph { a [shape=box, style=filled, fillcolor=\"#ff0000\", width=0.1, height=0.1]; }",
		"digraph { \"quoted name\" -> \"other \\\"name\\\"\"; }",
		"digraph { a -> b [weight=0, minlen=0, constraint=false]; { rank=same; a; b } }",
		"digraph G { }",
		"digraph { a -> { b c d } -> e }",
		"digraph { splines=ortho; overlap=false; a -> b; }",
		"graph { \xff\xfe }",
		"",
		"digraph { a -> ",
		"digraph { a [label=\"" + string(bytes.Repeat([]byte("x"), 5000)) + "\"] }",
	} {
		f.Add([]byte(seed))
	}

	ctx := context.Background()

	g, err := graphviz.New(ctx)
	if err != nil {
		f.Fatal(err)
	}

	f.Cleanup(func() { _ = g.Close() })

	f.Fuzz(func(t *testing.T, dot []byte) {
		graph, err := graphviz.ParseBytes(dot)
		if err != nil {
			return
		}

		defer func() { _ = graph.Close() }() //nolint:contextcheck // Close takes no context; the graph is freed, not rendered

		rctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()

		for _, format := range []graphviz.Format{graphviz.GV, graphviz.SVG} {
			var out bytes.Buffer

			start := time.Now()
			err := g.Render(rctx, graph, format, &out)

			if d := time.Since(start); d > 15*time.Second {
				t.Fatalf("rendering %s took %v for %d bytes of DOT: %q", format, d, len(dot), truncate(dot))
			}

			if err == nil && out.Len() == 0 {
				t.Fatalf("rendering %s produced nothing and no error for %q", format, truncate(dot))
			}
		}
	})
}

func truncate(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}

	return string(b)
}
