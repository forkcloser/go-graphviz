package graphviz_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// A parse Graphviz gives up on leaves nothing in the module, and returns no
// graph. Out of its own stack, the parser returned without the clean-up a
// syntax error runs: the graph came back half built, and what the grammar
// had open stayed in the module for good, about 175 KiB for clusters nested
// 4000 deep.
func TestAbandonedParseLeavesNoMemory(t *testing.T) {
	const depth = 4000

	var source strings.Builder

	source.WriteString("digraph {")

	for i := range depth {
		fmt.Fprintf(&source, " subgraph cluster_%d {", i)
	}

	source.WriteString(" a -> b ")
	source.WriteString(strings.Repeat("}", depth))
	source.WriteString("}")

	growth := memoryGrowth(40, func() {
		graph, err := graphviz.ParseBytes([]byte(source.String()))
		if graph != nil {
			t.Error("a parse Graphviz gave up on returned a graph")

			closeOrError(t, graph.Close)
		}

		if err == nil || !strings.Contains(err.Error(), "memory exhausted") {
			t.Errorf("a parse Graphviz gave up on returned %v, want its \"memory exhausted\"", err)
		}
	})

	if growth != 0 {
		t.Errorf("memory grew %d KiB over 40 abandoned parses", growth/1024)
	}
}
