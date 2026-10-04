package gvc

import (
	_ "unsafe" // for go:linkname

	"github.com/forkcloser/go-graphviz/cdt"
	"github.com/forkcloser/go-graphviz/cgraph"
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

//go:linkname toGraph github.com/forkcloser/go-graphviz/cgraph.toGraph
func toGraph(*wasm.Graph) *cgraph.Graph

//go:linkname toGraphWasm github.com/forkcloser/go-graphviz/cgraph.toGraphWasm
func toGraphWasm(*cgraph.Graph) *wasm.Graph

//go:linkname toNode github.com/forkcloser/go-graphviz/cgraph.toNode
func toNode(*wasm.Node) *cgraph.Node

//go:linkname toEdge github.com/forkcloser/go-graphviz/cgraph.toEdge
func toEdge(*wasm.Edge) *cgraph.Edge

//go:linkname toDictLink github.com/forkcloser/go-graphviz/cdt.toLink
func toDictLink(*wasm.DictLink) *cdt.Link

//go:linkname toDictLinkWasm github.com/forkcloser/go-graphviz/cdt.toLinkWasm
func toDictLinkWasm(*cdt.Link) *wasm.DictLink
