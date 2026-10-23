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

	// Graphviz's context constructor installs the node label default on the
	// prototype graph (gvContext: agattr_text(NULL, AGNODE, "label", "\N")),
	// so a graph read after a context exists carries `node [label="\N"];` in
	// its DOT and one read before any context does not: the first render of a
	// process differed from every later one. Native dot creates its context
	// before it reads; a program parsing first had no way to. The default
	// is installed here, at the engine's init, so a graph is the same graph
	// whenever it is read; the constructor's later call sets the same value.
	if err := wasm.SetDefaultAttr(ctx, int(wasm.NODE), "label", "\\N"); err != nil {
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
