package graphviz_test

import (
	"bytes"
	"context"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/forkcloser/go-graphviz"
)

// holdingEngine is a render engine that stops in its first callback until
// it is released, holding the module as a long layout does.
type holdingEngine struct {
	*graphviz.DefaultRenderEngine

	inside, release chan struct{}
	held            bool
}

func (h *holdingEngine) BeginJob(context.Context, *graphviz.Job) error {
	if !h.held {
		h.held = true

		close(h.inside)
		<-h.release
	}

	return nil
}

// A plugin that is dropped while a render holds the module gives back what
// it has of the module once the render is over, and holds up nothing
// meanwhile: the program's other cleanups run. The drop used to wait for
// the module on the runtime's cleanup goroutines, which are few, and with
// 64 plugins dropped none of a program's own cleanups ran until the render
// returned.
func TestADroppedPluginDoesNotHoldUpOtherCleanups(t *testing.T) {
	ctx := t.Context()

	holder := &holdingEngine{
		DefaultRenderEngine: new(graphviz.DefaultRenderEngine),
		inside:              make(chan struct{}),
		release:             make(chan struct{}),
	}

	render, err := graphviz.NewRenderPlugin(ctx, "hold", holder)
	if err != nil {
		t.Fatal(err)
	}

	device, err := graphviz.NewDevicePlugin(ctx, "hold:hold")
	if err != nil {
		t.Fatal(err)
	}

	g, err := graphviz.NewWithPlugins(ctx, render, device)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	// Plugins built and dropped: their cleanups are due at a collection.
	for range 64 {
		if _, err := graphviz.NewRenderPlugin(ctx, "dropped", new(graphviz.DefaultRenderEngine)); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan error, 1)

	go func() {
		var out bytes.Buffer

		done <- g.Render(ctx, graph, "hold", &out)
	}()

	<-holder.inside

	released := false

	release := func() {
		if !released {
			released = true

			close(holder.release)

			if err := <-done; err != nil {
				t.Errorf("the held render: %v", err)
			}
		}
	}

	defer release()

	// The dropped plugins' cleanups are queued, and their drops wait for
	// the module.
	//revive:disable-next-line:call-to-gc // the plugins go at a collection, which the test asks for
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	// The program's own cleanups, which have nothing to do with Graphviz.
	const own = 200

	var ran atomic.Int64

	for range own {
		runtime.AddCleanup(new([64]byte), func(int) { ran.Add(1) }, 0)
	}

	for deadline := time.Now().Add(20 * time.Second); ran.Load() < own; {
		if time.Now().After(deadline) {
			t.Fatalf("%d of the program's own %d cleanups ran while a render held the module", ran.Load(), own)
		}

		//revive:disable-next-line:call-to-gc // the cleanups run after a collection, which the test asks for
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
}
