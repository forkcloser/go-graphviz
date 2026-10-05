package graphviz_test

import (
	"testing"

	"github.com/forkcloser/go-graphviz"
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
