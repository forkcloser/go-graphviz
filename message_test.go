package graphviz_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/cgraph"
)

// A format Graphviz does not know is reported with Graphviz's own message,
// not with a read of the out-parameters a failed gvRenderData never wrote.
func TestUnknownFormat(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var buf bytes.Buffer

	err = g.Render(ctx, graph, "nosuchformat", &buf)
	if !errors.Is(err, cgraph.ErrGraphviz) {
		t.Fatalf("Render returned %v, want a Graphviz error", err)
	}

	if !strings.Contains(err.Error(), `"nosuchformat" not recognized`) {
		t.Fatalf("the error does not carry Graphviz's message: %v", err)
	}
}

// Graphviz's warnings reach the writer named for them, as the lines Graphviz
// prints, and an error is returned by its call rather than written there.
func TestWarningWriter(t *testing.T) {
	ctx := t.Context()

	var warnings bytes.Buffer

	graphviz.SetWarningWriter(&warnings)
	t.Cleanup(func() { graphviz.SetWarningWriter(nil) })

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a [image="no-such-image.png"] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var buf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.SVG, &buf); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(warnings.String(), "Warning: ") || !strings.Contains(warnings.String(), "no-such-image.png") {
		t.Fatalf("the warning did not reach the writer as Graphviz prints it: %q", warnings.String())
	}

	warnings.Reset()

	if _, err := graphviz.ParseBytes([]byte("digraph { a -- b }")); !errors.Is(err, cgraph.ErrGraphviz) {
		t.Fatalf("a parse error came back as %v", err)
	}

	if warnings.Len() != 0 {
		t.Fatalf("an error was written to the warning writer: %q", warnings.String())
	}
}

// A parse error is reported once, by the parse: the message does not linger
// to be attached to a later, unrelated failure.
func TestErrorMessageIsConsumed(t *testing.T) {
	if _, err := graphviz.ParseBytes([]byte("graph { a - b }")); err == nil {
		t.Fatal("a syntax error parsed")
	}

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	// agset on an attribute nobody declared fails with no message of its own.
	err = graph.Set("nosuchattribute", "x")
	if !errors.Is(err, cgraph.ErrGraphviz) {
		t.Fatalf("Set on an undeclared attribute returned %v, want a Graphviz error", err)
	}

	if strings.Contains(err.Error(), "syntax error") {
		t.Fatalf("the earlier parse error was attached to an unrelated failure: %v", err)
	}
}
