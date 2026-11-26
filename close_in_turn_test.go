package graphviz_test

import (
	"bytes"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
)

// closingProcessEnv marks the child process TestCloseWhileRendering starts:
// what the test guards against is a trap, which ends Graphviz for the whole
// process it happens in.
const closingProcessEnv = "GO_GRAPHVIZ_TEST_CLOSING_PROCESS"

// An instance may be closed from one goroutine while another renders with
// it, and twice at once: the render is whole, or it is refused with
// gvc.ErrClosed, and nothing else. Close used to free the context in
// several turns in the module; a render waiting for the module got it
// between the free and the mark, laid out on the freed context, and the
// module trapped, within some twenty rounds of this test.
func TestCloseWhileRendering(t *testing.T) {
	t.Parallel()

	if os.Getenv(closingProcessEnv) == "1" {
		closeWhileRendering(t)

		return
	}

	cmd := child(t, os.Args[0], "-test.run=^TestCloseWhileRendering$", "-test.v")

	cmd.Env = append(os.Environ(), closingProcessEnv+"=1")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the closing process: %v\n%s", err, out)
	}
}

func closeWhileRendering(t *testing.T) {
	t.Helper()

	ctx := t.Context()

	graph, err := graphviz.ParseBytes([]byte("digraph { c -> d -> e }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	reference, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, reference.Close) })

	var want bytes.Buffer
	if err := reference.Render(ctx, graph, graphviz.SVG, &want); err != nil {
		t.Fatal(err)
	}

	// Instances made and closed beside the test's own, which take the
	// memory a Close has just freed.
	var (
		stop   atomic.Bool
		beside sync.WaitGroup
	)

	for range 3 {
		beside.Go(func() {
			for !stop.Load() {
				g, err := graphviz.New(ctx)
				if err != nil {
					return
				}

				_ = g.Close() //nolint:contextcheck // Close takes no context: a free runs whatever one says
			}
		})
	}

	defer func() {
		stop.Store(true)
		beside.Wait()
	}()

	const rounds = 1000

	whole, refused := 0, 0

	for round := range rounds {
		victim, err := graphviz.New(ctx)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}

		var (
			group    sync.WaitGroup
			out      bytes.Buffer
			rendered error
			closed   [2]error
		)

		group.Go(func() { rendered = victim.Render(ctx, graph, graphviz.SVG, &out) })
		group.Go(func() { closed[0] = victim.Close() })
		group.Go(func() { closed[1] = victim.Close() })
		group.Wait()

		switch {
		case rendered == nil && bytes.Equal(out.Bytes(), want.Bytes()):
			whole++
		case errors.Is(rendered, gvc.ErrClosed) && out.Len() == 0:
			refused++
		default:
			t.Fatalf(
				"round %d: the render returned %v with %d bytes, want the rendering or ErrClosed",
				round,
				rendered,
				out.Len(),
			)
		}

		if closed[0] != nil || closed[1] != nil {
			t.Fatalf("round %d: the two closes returned %v and %v", round, closed[0], closed[1])
		}

		if err := victim.Render(ctx, graph, graphviz.SVG, &out); !errors.Is(err, gvc.ErrClosed) {
			t.Fatalf("round %d: a render after Close returned %v, want ErrClosed", round, err)
		}
	}

	t.Logf("%d renders whole, %d refused", whole, refused)
}

// SetLayout may be called while another goroutine renders: each render is
// the one of an engine that was set, whole.
func TestSetLayoutWhileRendering(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b -> c -> a }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	engines := []graphviz.Layout{graphviz.DOT, graphviz.CIRCO}
	want := make([][]byte, len(engines))

	for i, engine := range engines {
		var out bytes.Buffer
		if err := g.SetLayout(engine).Render(ctx, graph, graphviz.SVG, &out); err != nil {
			t.Fatal(err)
		}

		want[i] = out.Bytes()
	}

	if bytes.Equal(want[0], want[1]) {
		t.Fatal("the two engines draw the graph alike: the test would see nothing")
	}

	var (
		stop    atomic.Bool
		setting sync.WaitGroup
	)

	setting.Go(func() {
		for i := 0; !stop.Load(); i++ {
			g.SetLayout(engines[i%len(engines)])
		}
	})

	defer func() {
		stop.Store(true)
		setting.Wait()
	}()

	for round := range 200 {
		var out bytes.Buffer
		if err := g.Render(ctx, graph, graphviz.SVG, &out); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}

		if !bytes.Equal(out.Bytes(), want[0]) && !bytes.Equal(out.Bytes(), want[1]) {
			t.Fatalf("round %d: the rendering is neither engine's", round)
		}
	}
}
