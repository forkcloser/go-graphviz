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

// errInit is why the package could not set itself up at import, nil when it
// could; Open and ParseBytes return it. It used to be a panic, which took the
// importing program down before main ran.
var errInit error

func init() {
	errInit = setGlobalVars()
}

// setGlobalVars builds the four graph descriptors in the module's memory.
// It runs once, at package init, so the context is the background one; the
// descriptor setters it calls are generated field writes and take none.
func setGlobalVars() error {
	ctx := context.Background()

	// Graphviz's messages come to Go from here on: errors wait for the call
	// that caused them, warnings go to the writer the user names.
	if err := wasm.RouteMessages(ctx); err != nil {
		return err
	}

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
