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

// A render that failed on its format leaves the instance whole: the next
// render is the one a fresh instance makes, after one unknown format and
// after two in a row.
func TestRenderAfterAnUnknownFormat(t *testing.T) {
	ctx := t.Context()

	const src = "digraph { a -> b }"

	render := func(g *graphviz.Graphviz, format graphviz.Format) ([]byte, error) {
		graph, err := graphviz.ParseBytes([]byte(src))
		if err != nil {
			t.Fatal(err)
		}

		defer closeOrError(t, graph.Close)

		var buf bytes.Buffer

		err = g.Render(ctx, graph, format, &buf)

		return buf.Bytes(), err
	}

	fresh, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, fresh.Close) })

	want, err := render(fresh, graphviz.SVG)
	if err != nil {
		t.Fatal(err)
	}

	for _, unknown := range [][]graphviz.Format{{"nosuchformat"}, {"jpeg", "jpe"}} {
		g, err := graphviz.New(ctx)
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() { closeOrError(t, g.Close) })

		for _, format := range unknown {
			if _, err = render(g, format); !errors.Is(err, cgraph.ErrGraphviz) ||
				!strings.Contains(err.Error(), `"`+string(format)+`" not recognized`) {
				t.Fatalf("%s: Render returned %v, want Graphviz's refusal of the format", format, err)
			}
		}

		got, err := render(g, graphviz.SVG)
		if err != nil {
			t.Fatalf("after %v: the next render failed: %v", unknown, err)
		}

		if !bytes.Equal(got, want) {
			t.Errorf("after %v: the next render is not a fresh instance's:\n%s", unknown, got)
		}
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

// A call that fails without a message is reported with the code Graphviz
// returned: agset's failure is -1, and that is what the error says.
func TestAFailedCallReportsGraphvizsCode(t *testing.T) {
	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	err = graph.Set("nosuchattribute", "x")
	if !errors.Is(err, cgraph.ErrGraphviz) || !strings.Contains(err.Error(), "code -1 ") {
		t.Fatalf("Set on an undeclared attribute returned %v, want a Graphviz error with code -1", err)
	}
}
