package graphviz_test

import (
	"bytes"
	"os"
	"runtime"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// maxRenderBytes bounds what a PNG render of unix.gv allocates: about
// 7 MB, where a call engine built for every call into the module made it
// 320 MB.
const maxRenderBytes = 32 << 20

// A render allocates on the order of its page, not of the calls it makes
// into the module: each exported function is kept and reused.
func TestRenderAllocations(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	data, err := os.ReadFile("testdata/directed/unix.gv")
	if err != nil {
		t.Fatal(err)
	}

	render := func() {
		graph, err := graphviz.ParseBytes(data)
		if err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if err := g.Render(ctx, graph, graphviz.PNG, &out); err != nil {
			t.Fatal(err)
		}

		closeOrError(t, graph.Close)
	}

	render()

	const renders = 3

	var before, after runtime.MemStats

	runtime.ReadMemStats(&before)

	for range renders {
		render()
	}

	runtime.ReadMemStats(&after)

	if perRender := (after.TotalAlloc - before.TotalAlloc) / renders; perRender > maxRenderBytes {
		t.Errorf("a render allocated %d MB, want at most %d", perRender>>20, maxRenderBytes>>20)
	}
}
