package cgraph

import (
	"context"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

var (
	Directed         *Desc
	StrictDirected   *Desc
	UnDirected       *Desc
	StrictUnDirected *Desc
)

func init() {
	if err := setGlobalVars(); err != nil {
		panic(err)
	}
}

// setGlobalVars builds the four graph descriptors in the module's memory.
// It runs once, at package init, so the context is the background one; the
// descriptor setters it calls are generated field writes and take none.
func setGlobalVars() error {
	ctx := context.Background()

	// Set MAX to prevent outputting internally generated errors or warnings with agerr to the stderr.
	wasm.SetError(ctx, wasm.MAX)

	directed, err := wasm.NewGraphDescriptor(ctx)
	if err != nil {
		return err
	}

	directed.SetDirected(1)
	directed.SetMaingraph(1)

	strictDirected, err := wasm.NewGraphDescriptor(ctx)
	if err != nil {
		return err
	}

	strictDirected.SetDirected(1)
	strictDirected.SetStrict(1)
	strictDirected.SetMaingraph(1)

	undirected, err := wasm.NewGraphDescriptor(ctx)
	if err != nil {
		return err
	}

	undirected.SetMaingraph(1)

	strictUndirected, err := wasm.NewGraphDescriptor(ctx)
	if err != nil {
		return err
	}

	strictUndirected.SetStrict(1)
	strictUndirected.SetMaingraph(1)

	Directed = toDesc(directed)
	StrictDirected = toDesc(strictDirected)
	UnDirected = toDesc(undirected)
	StrictUnDirected = toDesc(strictUndirected)

	return nil
}
