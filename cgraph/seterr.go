package cgraph

import (
	"context"
	"sync"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// setterErrors holds, per root graph, the first error a typed setter met on
// the graph or anything in it. The setters chain, so they cannot return it.
//
//nolint:gochecknoglobals // per-graph state the chaining setters have nowhere else to keep
var setterErrors = struct {
	mu     sync.Mutex
	byRoot map[uint64]error
}{byRoot: map[uint64]error{}}

// Err is the first error a typed setter (SetLabel, SetShape and the rest,
// which return their receiver to chain) met on the graph, its subgraphs,
// nodes or edges since the root graph was made, or nil. gvc's Layout, which
// every render goes through, returns it too. The setters write through the
// WebAssembly module, so what fails them is the module: memory it cannot
// allocate, or a module that did not load.
func (g *Graph) Err() error {
	setterErrors.mu.Lock()
	defer setterErrors.mu.Unlock()

	return setterErrors.byRoot[wasm.WasmPtr(g.wasm.GetRoot())]
}

// recordSetterError keeps err as the first setter error of the graph root,
// unless one is kept already.
func recordSetterError(root *wasm.Graph, err error) {
	if err == nil || root == nil {
		return
	}

	key := wasm.WasmPtr(root)

	setterErrors.mu.Lock()
	defer setterErrors.mu.Unlock()

	if _, kept := setterErrors.byRoot[key]; !kept {
		setterErrors.byRoot[key] = err
	}
}

// forgetSetterErrors drops what the root graph's setters met, when it closes.
func forgetSetterErrors(root *wasm.Graph) {
	setterErrors.mu.Lock()
	defer setterErrors.mu.Unlock()

	delete(setterErrors.byRoot, wasm.WasmPtr(root))
}

func (g *Graph) record(err error) {
	if err != nil {
		recordSetterError(g.wasm.GetRoot(), err)
	}
}

func (n *Node) record(err error) {
	if err != nil {
		recordSetterError(n.wasm.GetRoot(), err)
	}
}

// record keeps err against the edge's graph, which it reaches through its
// tail node; when even that fails, the module is past reporting to.
func (e *Edge) record(err error) {
	if err == nil {
		return
	}

	if tail, tailErr := e.wasm.Tail(context.Background()); tailErr == nil && tail != nil {
		recordSetterError(tail.GetRoot(), err)
	}
}
