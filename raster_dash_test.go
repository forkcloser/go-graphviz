package graphviz_test

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/forkcloser/go-graphviz"
)

// What a dashed edge costs to draw follows the page, not the edge. One ten
// thousand inches long, of which a viewport shows a hundred points, and one
// as long scaled into an inch, whose dashes are a thousandth of a pixel,
// each allocate under 4 MiB of the Go heap for their render (under 2, as
// measured), where the first allocated 31 and the second 41. Not parallel:
// the count is the process's.
func TestDashedEdgeFollowsThePage(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	g.SetLayout(graphviz.NEATO)

	render := func(src string) int {
		graph, err := graphviz.ParseBytes([]byte(src))
		if err != nil {
			t.Fatal(err)
		}

		defer closeOrError(t, graph.Close)

		var out bytes.Buffer
		if err := g.Render(ctx, graph, graphviz.PNG, &out); err != nil {
			t.Fatal(err)
		}

		return out.Len()
	}

	// The first render loads what every render shares.
	render(`digraph { a [pos="0,0!"]; b [pos="100,0!"]; a -> b [style=dashed] }`)

	for name, src := range map[string]string{
		"a long edge through a viewport": `digraph { viewport="100,100,1"
			a [pos="0,0!"]; b [pos="10000,0!"]; a -> b [style=dashed] }`,
		"a long edge scaled into an inch": `digraph { size="1,1"
			a [pos="0,0!"]; b [pos="10000,10000!"]; a -> b [style=dashed] }`,
	} {
		var before, after runtime.MemStats

		runtime.ReadMemStats(&before)

		size := render(src)

		runtime.ReadMemStats(&after)

		if size == 0 {
			t.Errorf("%s: nothing rendered", name)
		}

		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4<<20 {
			t.Errorf("%s: the render allocated %d KiB", name, allocated>>10)
		}
	}
}

// guardedProcessEnv marks the child process TestDashedEdgeFarFromThePage
// starts, which renders under a guard of its own.
const guardedProcessEnv = "GO_GRAPHVIZ_TEST_GUARDED_PROCESS"

// A dashed edge whose ends lie so far from the page that a dash is shorter
// than the gap between two numbers there is drawn like any other: in the
// memory and the time of the page. Its walk counted the distance done from
// the segment's start, where adding a dash changed nothing, and cut dashes
// without end: 600 MiB in half a second for 98 bytes of DOT.
//
// The renders run in a process of their own, which ends itself when its
// heap passes 256 MiB or ten seconds: a walk that never ends cannot be
// stopped from inside the process it runs in.
func TestDashedEdgeFarFromThePage(t *testing.T) {
	t.Parallel()

	if os.Getenv(guardedProcessEnv) == "1" {
		dashedEdgeFarFromThePage(t)

		return
	}

	cmd := child(t, os.Args[0], "-test.run=^TestDashedEdgeFarFromThePage$", "-test.v")

	cmd.Env = append(os.Environ(), guardedProcessEnv+"=1")

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the guarded process: %v\n%s", err, out)
	}
}

func dashedEdgeFarFromThePage(t *testing.T) {
	t.Helper()

	const (
		heapLimit = 256 << 20
		timeLimit = 10 * time.Second
	)

	started := time.Now()

	go func() {
		for {
			var stats runtime.MemStats

			runtime.ReadMemStats(&stats)

			if stats.HeapAlloc > heapLimit || time.Since(started) > timeLimit {
				fmt.Fprintf(os.Stderr, "guard: heap %d MiB after %v\n", stats.HeapAlloc>>20, time.Since(started))
				//revive:disable-next-line:deep-exit // the guard is there to end a process whose walk nothing else stops
				os.Exit(1)
			}

			time.Sleep(time.Millisecond)
		}
	}()

	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	g.SetLayout(graphviz.NEATO)

	for name, src := range map[string]string{
		"past 2^56 points": `digraph { viewport="100,100,1,9e16,0"
			a [pos="0,0!"]; b [pos="1e17,0!"]; a -> b [style=dashed] }`,
		"past 2^56 points, dotted and slanted": `digraph { viewport="100,100,1,9e16,9e16"
			a [pos="0,0!"]; b [pos="1e17,1e17!"]; a -> b [style=dotted] }`,
		"past any length": `digraph { viewport="100,100,1,9e300,0"
			a [pos="0,0!"]; b [pos="1e301,0!"]; a -> b [style=dashed] }`,
	} {
		graph, err := graphviz.ParseBytes([]byte(src))
		if err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if err := g.Render(ctx, graph, graphviz.PNG, &out); err != nil {
			t.Errorf("%s: %v", name, err)
		}

		if out.Len() == 0 {
			t.Errorf("%s: nothing rendered", name)
		}

		closeOrError(t, graph.Close)
	}
}
