package graphviz_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/image/font/opentype"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/cgraph"
	"github.com/forkcloser/go-graphviz/gvc"
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

// A call's error is its own when other goroutines' calls fail beside it.
// One goroutine deletes a node of another graph, which Graphviz refuses
// with a message; another sets an attribute nobody declared, which it
// refuses without one. Read after the call had left the module, the
// message of the one was taken by the other, and the first was left with
// none.
func TestACallsErrorIsItsOwnAmongGoroutines(t *testing.T) {
	gv, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, gv.Close) })

	mine, err := gv.Graph()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, mine.Close) })

	other, err := gv.Graph()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, other.Close) })

	stranger, err := other.CreateNodeByName("stranger")
	if err != nil || stranger == nil {
		t.Fatalf("no node to delete: %v", err)
	}

	const rounds = 20000

	var (
		group   sync.WaitGroup
		deleted atomic.Pointer[error]
		set     atomic.Pointer[error]
	)

	group.Go(func() {
		for range rounds {
			err := mine.Delete(stranger)
			if err == nil || !strings.Contains(err.Error(), "agdelete on wrong graph") {
				deleted.CompareAndSwap(nil, &err)

				return
			}
		}
	})

	group.Go(func() {
		for range rounds {
			err := other.Set("nosuchattribute", "x")
			if err == nil || !strings.Contains(err.Error(), "and no message") {
				set.CompareAndSwap(nil, &err)

				return
			}
		}
	})

	group.Wait()

	if wrong := deleted.Load(); wrong != nil {
		t.Errorf("deleting another graph's node returned %v, want Graphviz's own message", *wrong)
	}

	if wrong := set.Load(); wrong != nil {
		t.Errorf("setting an undeclared attribute returned %v, want a failure with no message", *wrong)
	}
}

// A graph call made from inside a layout, by a callback, has its own error
// and leaves the layout's: what Graphviz reported for the layout before the
// callback ran is still the layout's when it ends. A font loader is such a
// callback. Here the layout has a record label it cannot read, which it
// reports and goes on from, and the loader then sets an attribute nobody
// declared on another graph.
func TestACallInsideALayoutLeavesTheLayoutsError(t *testing.T) {
	other, err := graphviz.ParseBytes([]byte("digraph { x }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, other.Close) })

	var inside []error

	graphviz.SetFontLoader(func(context.Context, *graphviz.TextFont) (*opentype.Font, error) {
		//nolint:contextcheck // Set takes no context: the call is made in the layout's own stay
		inside = append(inside, other.Set("nosuchattribute", "x"))

		return nil, nil //nolint:nilnil // no font asks for the usual resolution
	})
	t.Cleanup(func() { graphviz.SetFontLoader(nil) })

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a [shape=record label="{a|"]; b [label="after"] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var out bytes.Buffer

	err = g.Render(t.Context(), graph, graphviz.SVG, &out)
	if !errors.Is(err, gvc.ErrReported) || !strings.Contains(err.Error(), "bad label format") {
		t.Errorf("the render returned %v, want the layout's own report of the label", err)
	}

	if len(inside) == 0 {
		t.Fatal("the font loader was not asked: the test saw nothing")
	}

	for _, err := range inside {
		if err == nil || !strings.Contains(err.Error(), "and no message") {
			t.Fatalf("a call from inside the layout returned %v, want its own failure with no message", err)
		}
	}
}

// A parse, a render and a free made from inside a layout, by a callback,
// each read what Graphviz reported during them and leave the layout's: what
// was reported for the layout before the callback ran is still the layout's
// when it ends. Here the layout has a record label it cannot read, and the
// font loader then parses a text that is no graph, renders a graph laid out
// before, in a format Graphviz does not know and in one it does, and frees
// the layout of another.
func TestAParseOrARenderInsideALayoutLeavesTheLayoutsError(t *testing.T) {
	lower, err := gvc.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, lower.Close) })

	// Laid out before the loader is set: one to render, the others to free,
	// one at each call of the loader.
	var laid []*cgraph.Graph

	for range 9 {
		before, parseErr := graphviz.ParseBytes([]byte("digraph { x -> y }"))
		if parseErr != nil {
			t.Fatal(parseErr)
		}

		t.Cleanup(func() { closeOrError(t, before.Close) })

		if layoutErr := lower.Layout(t.Context(), before, "dot"); layoutErr != nil {
			t.Fatal(layoutErr)
		}

		laid = append(laid, before)
	}

	kept, toFree := laid[0], laid[1:]

	t.Cleanup(func() {
		for _, before := range append(toFree, kept) {
			if freeErr := lower.FreeLayout(context.WithoutCancel(t.Context()), before); freeErr != nil {
				t.Error(freeErr)
			}
		}
	})

	// A render Graphviz refuses with a message, made alone: what the same
	// render says from inside the layout.
	alone := lower.RenderData(t.Context(), kept, "nosuchformat", io.Discard)
	if alone == nil {
		t.Fatal("a render in a format Graphviz does not know returned nil")
	}

	var (
		parsed, refused, rendered, freed []error
		inside                           bool
	)

	graphviz.SetFontLoader(func(ctx context.Context, _ *graphviz.TextFont) (*opentype.Font, error) {
		if inside {
			return nil, nil //nolint:nilnil // no font asks for the usual resolution
		}

		inside = true
		defer func() { inside = false }()

		_, parseErr := graphviz.ParseBytes([]byte("digraph { a -> }"))
		parsed = append(parsed, parseErr)

		refused = append(refused, lower.RenderData(ctx, kept, "nosuchformat", io.Discard))
		rendered = append(rendered, lower.RenderData(ctx, kept, "svg", io.Discard))

		if len(toFree) > 0 {
			freed = append(freed, lower.FreeLayout(ctx, toFree[0]))
			toFree = toFree[1:]
		}

		return nil, nil //nolint:nilnil // no font asks for the usual resolution
	})
	t.Cleanup(func() { graphviz.SetFontLoader(nil) })

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a [shape=record label="{a|"]; b [label="after"] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var out bytes.Buffer

	err = g.Render(t.Context(), graph, graphviz.SVG, &out)
	if !errors.Is(err, gvc.ErrReported) || !strings.Contains(err.Error(), "bad label format") ||
		strings.Contains(err.Error(), "syntax error") || strings.Contains(err.Error(), "nosuchformat") {
		t.Errorf("the render returned %v, want the layout's own report of the label and that alone", err)
	}

	if len(parsed) == 0 || len(freed) == 0 {
		t.Fatal("the font loader was not asked: the test saw nothing")
	}

	for _, err := range parsed {
		if err == nil || !strings.Contains(err.Error(), "syntax error") || strings.Contains(err.Error(), "bad label") {
			t.Fatalf("a parse from inside the layout returned %v, want its own syntax error and that alone", err)
		}
	}

	for _, err := range refused {
		if err == nil || err.Error() != alone.Error() {
			t.Fatalf("a refused render from inside the layout returned %q, want what it returns alone, %q", err, alone)
		}
	}

	for _, err := range append(rendered, freed...) {
		if err != nil {
			t.Fatalf("a render or a free from inside the layout returned %v, want nil", err)
		}
	}
}
