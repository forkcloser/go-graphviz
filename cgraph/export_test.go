package cgraph

import "github.com/forkcloser/go-graphviz/internal/wasm"

// InjectSetterError keeps err as a typed setter of obj (a *Graph, *Node or
// *Edge) would on failure: the setters fail only when the WebAssembly
// module does, which a test cannot cause on demand.
func InjectSetterError(obj any, err error) {
	switch v := obj.(type) {
	case *Graph:
		v.record(err)
	case *Node:
		v.record(err)
	case *Edge:
		v.record(err)
	}
}

// RootAddress is where g's root graph lives in the module, which keys its
// setter errors.
func RootAddress(g *Graph) uint64 {
	return wasm.WasmPtr(g.wasm.GetRoot())
}
