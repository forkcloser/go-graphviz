package cgraph

import (
	_ "unsafe" // for go:linkname

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// toGraphWasm is reached from gvc through a linkname declaration
// (gvc/link.go), which the unused linter cannot see.
func toGraphWasm(v *Graph) *wasm.Graph { //nolint:unused // called through a linkname declaration from gvc
	if v == nil {
		return nil
	}

	return v.wasm
}
