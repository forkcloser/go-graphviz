package graphviz_test

import (
	"bytes"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// A record label Graphviz cannot read leaves nothing in the module. Its
// parser freed what it had built by a count it sets only when it is done,
// so the fields read before the fault stayed for good: about 330 bytes a
// layout.
func TestRecordLabelThatCannotBeReadLeavesNoMemory(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	for _, label := range []string{`{a|`, `{a|b|{c|d}|<e`, `a|{b|c`} {
		growth := memoryGrowth(2000, func() {
			graph, err := graphviz.ParseBytes([]byte(`digraph { n [shape=record, label="` + label + `"] }`))
			if err != nil {
				t.Fatal(err)
			}

			var out bytes.Buffer

			// Graphviz reports the label and draws the node all the same.
			_ = g.Render(ctx, graph, graphviz.SVG, &out)

			if out.Len() == 0 {
				t.Fatalf("label %s: nothing rendered", label)
			}

			closeOrError(t, graph.Close)
		})

		if growth != 0 {
			t.Errorf("label %s: memory grew %d KiB over 2000 renders", label, growth/1024)
		}
	}
}
