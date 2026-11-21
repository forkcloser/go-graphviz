package graphviz_test

import (
	"bytes"
	"runtime"
	"testing"
	"time"

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

// A plugin is installed again after every instance it was in has closed,
// and renders: its callbacks were dropped at the last close, and the next
// instance rendered nothing through it, with no error.
func TestPluginInstalledAgain(t *testing.T) {
	ctx := t.Context()

	engine := &countingEngine{DefaultRenderEngine: new(graphviz.DefaultRenderEngine)}

	render, err := graphviz.NewRenderPlugin(ctx, "again", engine)
	if err != nil {
		t.Fatal(err)
	}

	device, err := graphviz.NewDevicePlugin(ctx, "again:again")
	if err != nil {
		t.Fatal(err)
	}

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	for round := 1; round <= 3; round++ {
		g, err := graphviz.NewWithPlugins(ctx, render, device)
		if err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if err := g.Render(ctx, graph, "again", &out); err != nil {
			t.Fatal(err)
		}

		closeOrError(t, g.Close)

		if engine.jobs != round {
			t.Fatalf("instance %d: the engine has begun %d jobs, want %d", round, engine.jobs, round)
		}
	}
}

// A plugin nothing reaches any more gives its callbacks back, so a process
// that builds a plugin for each render does not keep every engine it had.
func TestDroppedPluginReleasesCallbacks(t *testing.T) {
	ctx := t.Context()

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	before := wasm.RegisteredCallbacks()

	for range 20 {
		render, err := graphviz.NewRenderPlugin(ctx, "dropped", new(graphviz.DefaultRenderEngine))
		if err != nil {
			t.Fatal(err)
		}

		device, err := graphviz.NewDevicePlugin(ctx, "dropped:dropped")
		if err != nil {
			t.Fatal(err)
		}

		g, err := graphviz.NewWithPlugins(ctx, render, device)
		if err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if err := g.Render(ctx, graph, "dropped", &out); err != nil {
			t.Fatal(err)
		}

		closeOrError(t, g.Close)
	}

	if during := wasm.RegisteredCallbacks(); during <= before {
		t.Fatalf("%d callbacks registered with 20 plugins built, %d before: the count measures nothing", during, before)
	}

	// The callbacks go when the collector has seen the plugins go, which it
	// reports on a goroutine of its own. Fewer than before is as good: the
	// plugins of an earlier test may go in the same sweep.
	deadline := time.Now().Add(10 * time.Second)

	for wasm.RegisteredCallbacks() > before {
		if time.Now().After(deadline) {
			t.Fatalf(
				"%d callbacks registered after 20 plugins were dropped, %d before",
				wasm.RegisteredCallbacks(),
				before,
			)
		}

		//revive:disable-next-line:call-to-gc // the callbacks go at a collection, which the test asks for
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
}
