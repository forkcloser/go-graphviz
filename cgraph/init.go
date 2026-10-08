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
// descriptor setters it calls are generated field writes and take none;
// each can still fail to reach the module's memory, and the first failure
// is the package's init error.
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

	if err = directed.SetDirected(1); err != nil {
		return err
	}

	if err = directed.SetMaingraph(1); err != nil {
		return err
	}

	strictDirected, err := wasm.NewGraphDescriptor(ctx)
	if err != nil {
		return err
	}

	if err = strictDirected.SetDirected(1); err != nil {
		return err
	}

	if err = strictDirected.SetStrict(1); err != nil {
		return err
	}

	if err = strictDirected.SetMaingraph(1); err != nil {
		return err
	}

	undirected, err := wasm.NewGraphDescriptor(ctx)
	if err != nil {
		return err
	}

	if err = undirected.SetMaingraph(1); err != nil {
		return err
	}

	strictUndirected, err := wasm.NewGraphDescriptor(ctx)
	if err != nil {
		return err
	}

	if err = strictUndirected.SetStrict(1); err != nil {
		return err
	}

	if err = strictUndirected.SetMaingraph(1); err != nil {
		return err
	}

	Directed = toDesc(directed)
	StrictDirected = toDesc(strictDirected)
	UnDirected = toDesc(undirected)
	StrictUnDirected = toDesc(strictUndirected)

	return nil
}
