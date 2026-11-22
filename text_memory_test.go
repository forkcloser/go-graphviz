package graphviz_test

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// heapKept is the Go heap in use once what can be collected has been:
// twice, since a first collection only queues what a finalizer holds.
func heapKept() uint64 {
	for range 2 {
		//revive:disable-next-line:call-to-gc // what is kept is what a collection leaves
		runtime.GC()
	}

	var stats runtime.MemStats

	runtime.ReadMemStats(&stats)

	return stats.HeapAlloc
}

// What drawing text keeps does not follow the fonts a graph names: a
// render is over with a megabyte or two of the Go heap in use more than
// before it, where each case kept tens of megabytes. Not parallel: the
// heap is the process's.
func TestTextKeepsNoMemory(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

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

	// The first text drawn reads the installed fonts, which are kept.
	render(`digraph { a [label="W"] }`)

	// A face was kept for every size a graph named, with the mask of the
	// largest glyph it drew. The nodes are small and close, for a small page:
	// the page is not what this measures.
	var sizes strings.Builder

	sizes.WriteString(`nodesep=0.02; node [fixedsize=true width=0.02 height=0.02 label="W"];`)

	for i := range 400 {
		fmt.Fprintf(&sizes, "n%d [fontsize=%d.%02d];", i, 75+i/100, i%100)
	}

	for name, src := range map[string]string{
		// One glyph of some nine million pixels on a page of a few thousand:
		// its mask, and the rasterizer that made it, were 58 MiB kept.
		"a glyph larger than its page": `digraph { a [fixedsize=true label="W" fontsize=3000] }`,
		"a font at four hundred sizes": "digraph { " + sizes.String() + " }",
	} {
		before := heapKept()

		if size := render(src); size == 0 {
			t.Errorf("%s: nothing rendered", name)
		}

		if after := heapKept(); after > before+4<<20 {
			t.Errorf("%s: the render kept %d KiB", name, (after-before)>>10)
		}
	}
}
