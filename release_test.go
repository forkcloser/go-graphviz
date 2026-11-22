package graphviz_test

import (
	"bytes"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// An instance made and closed registers no callback that stays: a process
// that creates and closes instances does not keep every render engine it
// ever had. The default plugins are the process's, built with the first
// instance and kept, so the count starts after one.
func TestCloseReleasesCallbacks(t *testing.T) {
	ctx := t.Context()

	first, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	closeOrError(t, first.Close)

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

// A plugin nothing reaches any more gives back the module memory the
// bindings allocated to describe it to Graphviz, each of the four kinds:
// built for each render, they left some 700 bytes a round for good.
func TestDroppedPluginsLeaveNoMemory(t *testing.T) {
	ctx := t.Context()

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	callbacks := wasm.RegisteredCallbacks()

	// batch builds a hundred plugins of each kind, renders through the
	// render and device ones, drops them all, and waits for the collector
	// to have given them back: the callbacks are counted, and the
	// structures go in the same cleanup, or for a device plugin, which has
	// no callback, in one of the same sweep, a few collections at most.
	batch := func() {
		for range 100 {
			engine := new(graphviz.DefaultRenderEngine)

			render, err := graphviz.NewRenderPlugin(ctx, "dropped", engine)
			if err != nil {
				t.Fatal(err)
			}

			device, err := graphviz.NewDevicePlugin(ctx, "dropped:dropped")
			if err != nil {
				t.Fatal(err)
			}

			if _, err = gvc.NewLoadImagePlugin(ctx, "png:dropped", engine); err != nil {
				t.Fatal(err)
			}

			if _, err = graphviz.NewTextLayoutPlugin(ctx); err != nil {
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

		deadline := time.Now().Add(10 * time.Second)

		for settled := 0; settled < 3; {
			if time.Now().After(deadline) {
				t.Fatalf(
					"%d callbacks registered after the plugins were dropped, %d before",
					wasm.RegisteredCallbacks(),
					callbacks,
				)
			}

			//revive:disable-next-line:call-to-gc // the plugins go at a collection, which the test asks for
			runtime.GC()
			time.Sleep(10 * time.Millisecond)

			if wasm.RegisteredCallbacks() <= callbacks {
				settled++
			}
		}
	}

	// The first batches take the memory a hundred live plugins need; the
	// batches after them have it to use again.
	for range 3 {
		batch()
	}

	before := wasm.MemorySize()

	for range 20 {
		batch()
	}

	if growth := wasm.MemorySize() - before; growth != 0 {
		t.Errorf("memory grew %d KiB over 2000 plugins of each kind built and dropped", growth/1024)
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
