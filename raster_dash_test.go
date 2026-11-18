package graphviz_test

import (
	"bytes"
	"runtime"
	"testing"

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
