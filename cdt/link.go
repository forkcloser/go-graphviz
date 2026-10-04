package cdt

import (
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// toLinkWasm and toDictWasm are reached from cgraph and gvc through linkname
// declarations (cgraph/link.go, gvc/link.go), which the unused linter cannot
// see.
func toLinkWasm(v *Link) *wasm.DictLink { //nolint:unused // called through a linkname declaration from cgraph and gvc
	if v == nil {
		return nil
	}

	return v.wasm
}

func toDictWasm(v *Dict) *wasm.Dict { //nolint:unused // called through a linkname declaration from cgraph
	if v == nil {
		return nil
	}

	return v.wasm
}
