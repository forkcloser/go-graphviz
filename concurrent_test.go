package graphviz_test

import (
	"bytes"
	"sync"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// Independent instances render the same graph from several goroutines at
// once and every result is the one a single goroutine gets: the module
// serializes the calls, and nothing reads another instance's memory.
func TestConcurrentInstances(t *testing.T) {
	ctx := t.Context()
	dot := []byte("digraph { a -> b -> c -> a; d -> a; b -> d [label=x] }")

	// Each instance and graph is closed at the test's end, in order, so the
	// closes themselves are not part of what runs concurrently.
	render := func() ([]byte, error) {
		g, err := graphviz.New(ctx)
		if err != nil {
			return nil, err
		}

		t.Cleanup(func() { closeOrError(t, g.Close) })

		graph, err := graphviz.ParseBytes(dot)
		if err != nil {
			return nil, err
		}

		t.Cleanup(func() { closeOrError(t, graph.Close) })

		var buf bytes.Buffer
		if err := g.Render(ctx, graph, graphviz.SVG, &buf); err != nil {
			return nil, err
		}

		return buf.Bytes(), nil
	}

	want, err := render()
	if err != nil {
		t.Fatal(err)
	}

	const goroutines, rounds = 4, 10

	var wg sync.WaitGroup

	for worker := range goroutines {
		wg.Go(func() {
			for round := range rounds {
				got, err := render()
				if err != nil {
					t.Errorf("worker %d round %d: %v", worker, round, err)

					return
				}

				if !bytes.Equal(got, want) {
					t.Errorf("worker %d round %d: the render differs from the single-goroutine one", worker, round)

					return
				}
			}
		})
	}

	wg.Wait()
}
