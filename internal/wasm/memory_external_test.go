package wasm_test

import (
	"testing"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// The module's memory does not grow under churn: every string or slot the
// Go side allocates for a call, and every header the bridge allocates for
// a result, is freed once read. Memory grows in pages, so a leak of a few
// bytes a call shows after the first thousands.
func TestChurnDoesNotGrowMemory(t *testing.T) {
	ctx := t.Context()

	size := wasm.MemorySize

	graph, err := wasm.MemRead(ctx, "digraph { a -> b }")
	if err != nil {
		t.Fatal(err)
	}

	churn := func(name string, rounds int, step func() error) {
		t.Helper()

		for range rounds / 10 { // warm the allocator's free lists
			if err := step(); err != nil {
				t.Fatal(err)
			}
		}

		before := size()

		for range rounds {
			if err := step(); err != nil {
				t.Fatal(err)
			}
		}

		if after := size(); after != before {
			t.Errorf("%s: memory grew from %d to %d bytes over %d rounds", name, before, after, rounds)
		}
	}

	churn("GetStr", 20000, func() error {
		_, err := wasm.GetStr(ctx, graph, "label")

		return err
	})
	churn("SetStr", 20000, func() error {
		_, err := wasm.SetStr(ctx, graph, "label", "x")

		return err
	})

	if _, err := graph.Close(ctx); err != nil {
		t.Fatal(err)
	}

	churn("parse and close", 2000, func() error {
		g, err := wasm.MemRead(ctx, "digraph { a -> b -> c; d -> a }")
		if err != nil {
			return err
		}

		_, err = g.Close(ctx)

		return err
	})
}
