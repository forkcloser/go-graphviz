package graphviz_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// contextGraph is an instance and a graph to render, closed with the test.
func contextGraph(t *testing.T) (*graphviz.Graphviz, *graphviz.Graph) {
	t.Helper()

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = g.Close() })

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = graph.Close() })

	return g, graph
}

// TestContractContextDoneBeforeTheStart: a context that is done when a
// render or a layout is asked for makes it return the context's error, with
// nothing laid out, rendered or written; the instance and the graph are as
// they were, and render under a live context.
func TestContractContextDoneBeforeTheStart(t *testing.T) {
	t.Parallel()

	g, graph := contextGraph(t)

	done, cancel := context.WithCancel(t.Context())
	cancel()

	var out bytes.Buffer
	if err := g.Render(done, graph, graphviz.SVG, &out); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Errorf("Render: %v, %d bytes written; want context.Canceled and nothing", err, out.Len())
	}

	if img, err := g.RenderImage(done, graph); !errors.Is(err, context.Canceled) || img != nil {
		t.Errorf("RenderImage: %v, %v; want context.Canceled and no image", img, err)
	}

	path := filepath.Join(t.TempDir(), "out.svg")
	if err := g.RenderFilename(done, graph, graphviz.SVG, path); !errors.Is(err, context.Canceled) {
		t.Errorf("RenderFilename: %v; want context.Canceled", err)
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("RenderFilename under a done context left a file: %v", err)
	}

	lower, err := gvc.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = lower.Close() })

	if err := lower.Layout(done, graph, "dot"); !errors.Is(err, context.Canceled) {
		t.Errorf("gvc Layout: %v; want context.Canceled", err)
	}

	if err := lower.RenderData(done, graph, "svg", &out); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Errorf("gvc RenderData: %v, %d bytes written; want context.Canceled and nothing", err, out.Len())
	}

	expired, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer stop()

	if err := g.Render(expired, graph, graphviz.SVG, &out); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Render past its deadline: %v; want context.DeadlineExceeded", err)
	}

	if err := g.Render(t.Context(), graph, graphviz.SVG, &out); err != nil || out.Len() == 0 {
		t.Errorf("Render under a live context, after the refusals: %v, %d bytes", err, out.Len())
	}
}

// TestContractContextDoneWhileWaiting: a render whose context ends while
// another call holds the engine stops waiting and returns the context's
// error, the engine still held; once the engine is free, a render runs: the
// one that gave up left nothing held.
func TestContractContextDoneWhileWaiting(t *testing.T) {
	t.Parallel()

	g, graph := contextGraph(t)

	holding, release, released := make(chan struct{}), make(chan struct{}), make(chan error, 1)

	go func() {
		released <- wasm.Exclusive(t.Context(), func(context.Context) error {
			close(holding)
			<-release

			return nil
		})
	}()

	<-holding

	waiting, stop := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer stop()

	var out bytes.Buffer

	start := time.Now()
	err := g.Render(waiting, graph, graphviz.SVG, &out)

	if !errors.Is(err, context.DeadlineExceeded) || out.Len() != 0 {
		t.Errorf(
			"Render while the engine is held: %v, %d bytes; want context.DeadlineExceeded and nothing",
			err,
			out.Len(),
		)
	}

	if waited := time.Since(start); waited > 10*time.Second {
		t.Errorf("Render gave up after %v, with a deadline of 100ms", waited)
	}

	select {
	case err := <-released:
		t.Fatalf("the engine was let go while the render waited: %v", err)
	default:
	}

	// A render with no deadline waits its turn and gets it.
	turn := make(chan error, 1)

	go func() {
		var later bytes.Buffer

		turn <- g.Render(t.Context(), graph, graphviz.SVG, &later)
	}()

	select {
	case err := <-turn:
		t.Fatalf("a render ran while the engine was held: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)

	if err := <-released; err != nil {
		t.Fatalf("the call that held the engine: %v", err)
	}

	select {
	case err := <-turn:
		if err != nil {
			t.Errorf("the render that waited its turn: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the render that waited never got its turn: the one that gave up left the engine held")
	}
}

// TestContractCallInsideACallIgnoresItsContext: what runs inside a call that
// holds the engine runs whatever its context says: it is the rest of that
// call, the frees among it.
func TestContractCallInsideACallIgnoresItsContext(t *testing.T) {
	t.Parallel()

	g, graph := contextGraph(t)

	var out bytes.Buffer

	err := wasm.Exclusive(t.Context(), func(inside context.Context) error {
		done, cancel := context.WithCancel(inside)
		cancel()

		return g.Render(done, graph, graphviz.SVG, &out)
	})
	if err != nil || out.Len() == 0 {
		t.Errorf("a render inside a call, under a done context: %v, %d bytes; want it rendered", err, out.Len())
	}

	// The same from a context that is not the call's own: the goroutine is
	// what holds the engine.
	out.Reset()

	err = wasm.Exclusive(t.Context(), func(context.Context) error {
		done, cancel := context.WithCancel(t.Context())
		cancel()

		//nolint:contextcheck // the point of the row: a context that is not the call's own
		return g.Render(done, graph, graphviz.SVG, &out)
	})
	if err != nil || out.Len() == 0 {
		t.Errorf("a render inside a call, under another done context: %v, %d bytes; want it rendered", err, out.Len())
	}
}
