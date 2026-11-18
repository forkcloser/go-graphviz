package graphviz_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// Closing an instance releases the callbacks its plugins registered, so a
// process that creates and closes instances does not keep every render
// engine it ever had.
func TestCloseReleasesCallbacks(t *testing.T) {
	ctx := t.Context()

	before := wasm.RegisteredCallbacks()

	for range 20 {
		g, err := graphviz.New(ctx)
		if err != nil {
			t.Fatal(err)
		}

		if err := g.Close(); err != nil {
			t.Fatal(err)
		}
	}

	if after := wasm.RegisteredCallbacks(); after != before {
		t.Fatalf("%d callbacks registered after 20 instances were closed, %d before", after, before)
	}
}

// A plugin shared by two instances keeps its callbacks until the last one
// closes: closing the first does not leave the second rendering nothing.
func TestSharedPluginOutlivesFirstClose(t *testing.T) {
	ctx := t.Context()

	plugins, err := graphviz.DefaultPlugins(ctx)
	if err != nil {
		t.Fatal(err)
	}

	first, err := graphviz.NewWithPlugins(ctx, plugins...)
	if err != nil {
		t.Fatal(err)
	}

	second, err := graphviz.NewWithPlugins(ctx, plugins...)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, second.Close) })

	if closeErr := first.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	img, err := second.RenderImage(ctx, graph)
	if err != nil {
		t.Fatal(err)
	}

	if img.Bounds().Empty() {
		t.Fatal("the second instance rendered nothing after the first closed")
	}
}

// Closing an instance twice closes it once: the second Close does nothing, a
// render with the closed instance is ErrClosed, and an instance made after
// renders the PNG one made before did. A second Close freed the context
// again and released the default plugins, the process's, a second time, and
// every PNG rendered after it, by any instance, was empty and no error.
func TestCloseTwice(t *testing.T) {
	ctx := t.Context()

	render := func(g *graphviz.Graphviz) ([]byte, error) {
		graph, err := graphviz.ParseBytes([]byte(`digraph { a -> b [label="x"] }`))
		if err != nil {
			t.Fatal(err)
		}

		defer closeOrError(t, graph.Close)

		var out bytes.Buffer

		err = g.Render(ctx, graph, graphviz.PNG, &out)

		return out.Bytes(), err
	}

	before, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	want, err := render(before)
	if err != nil || len(want) == 0 {
		t.Fatalf("the first render: %d bytes, %v", len(want), err)
	}

	for range 2 {
		if err = before.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	if _, err = render(before); !errors.Is(err, gvc.ErrClosed) {
		t.Errorf("a render with a closed instance: %v, want %v", err, gvc.ErrClosed)
	}

	after, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, after.Close) })

	if got, err := render(after); err != nil || !bytes.Equal(got, want) {
		t.Errorf(
			"a render after the two closes: %d bytes, %v; want the %d bytes of the first",
			len(got),
			err,
			len(want),
		)
	}
}
