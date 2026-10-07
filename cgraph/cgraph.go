// Package cgraph mirrors libcgraph, Graphviz's graph library: graphs, nodes,
// edges, subgraphs, their attributes and the DOT reader and writer, as typed
// handles over the WebAssembly module's memory.
package cgraph

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

type Graph struct {
	wasm *wasm.Graph
}

func toGraph(v *wasm.Graph) *Graph {
	if v == nil {
		return nil
	}

	return &Graph{wasm: v}
}

func (g *Graph) Parent() *Graph {
	return toGraph(g.wasm.GetParent())
}

func (g *Graph) GraphRoot() *Graph {
	return toGraph(g.wasm.GetRoot())
}

func (g *Graph) CopyAttr(t *Graph) error {
	res, err := wasm.CopyAttr(context.Background(), g.wasm, t.getWasm())
	if err != nil {
		return err
	}

	return toError(res)
}

func (g *Graph) GetStr(name string) string {
	v, _ := wasm.GetStr(context.Background(), g.wasm, name)
	return v
}

func (g *Graph) SymbolName(sym *Symbol) (string, error) {
	return wasm.GetSymName(context.Background(), g.wasm, sym.getWasm())
}

func (g *Graph) Set(name, value string) error {
	res, err := wasm.SetStr(context.Background(), g.wasm, name, value)
	if err != nil {
		return err
	}

	return toError(res)
}

func (g *Graph) SetSymbolName(sym *Symbol, value string) error {
	res, err := wasm.SetSymName(context.Background(), g.wasm, sym.getWasm(), value)
	if err != nil {
		return err
	}

	return toError(res)
}

func (g *Graph) SafeSet(name, value, def string) error {
	res, err := wasm.SafeSetStr(context.Background(), g.wasm, name, value, def)
	if err != nil {
		return err
	}

	return toError(res)
}

// SafeSetHTML is SafeSet for an HTML-like value. Since Graphviz 13 a value
// set through SafeSet is plain text and an HTML label set that way renders
// as escaped markup; this is the call for a label such as <table>...</table>.
func (g *Graph) SafeSetHTML(name, value, def string) error {
	res, err := wasm.SafeSetStrHTML(context.Background(), g.wasm, name, value, def)
	if err != nil {
		return err
	}

	return toError(res)
}

func (g *Graph) Close() error {
	if root := g.wasm.GetRoot(); wasm.WasmPtr(root) == wasm.WasmPtr(g.wasm) {
		forgetSetterErrors(root)
	}

	res, err := g.wasm.Close(context.Background())
	if err != nil {
		return err
	}

	return toError(res)
}

func (g *Graph) IsSimple() (bool, error) {
	res, err := g.wasm.IsSimple(context.Background())
	if err != nil {
		return false, err
	}

	return res == 1, nil
}

func (g *Graph) CreateNodeByName(name string) (*Node, error) {
	res, err := g.wasm.Node(context.Background(), name, 1)
	if err != nil {
		return nil, err
	}

	return toNode(res), nil
}

func (g *Graph) NodeByName(name string) (*Node, error) {
	res, err := g.wasm.Node(context.Background(), name, 0)
	if err != nil {
		return nil, err
	}

	return toNode(res), nil
}

func (g *Graph) CreateNodeByID(id ID) (*Node, error) {
	res, err := g.wasm.IdNode(context.Background(), uint64(id), 1)
	if err != nil {
		return nil, err
	}

	return toNode(res), nil
}

func (g *Graph) NodeByID(id ID) (*Node, error) {
	res, err := g.wasm.IdNode(context.Background(), uint64(id), 0)
	if err != nil {
		return nil, err
	}

	return toNode(res), nil
}

func (g *Graph) CreateSubNode(n *Node) (*Node, error) {
	res, err := g.wasm.SubNode(context.Background(), n.getWasm(), 1)
	if err != nil {
		return nil, err
	}

	return toNode(res), nil
}

func (g *Graph) SubNode(n *Node) (*Node, error) {
	res, err := g.wasm.SubNode(context.Background(), n.getWasm(), 0)
	if err != nil {
		return nil, err
	}

	return toNode(res), nil
}

func (g *Graph) FirstNode() (*Node, error) {
	res, err := g.wasm.FirstNode(context.Background())
	if err != nil {
		return nil, err
	}

	return toNode(res), nil
}

func (g *Graph) NextNode(n *Node) (*Node, error) {
	res, err := g.wasm.NextNode(context.Background(), n.getWasm())
	if err != nil {
		return nil, err
	}

	return toNode(res), nil
}

func (g *Graph) LastNode() (*Node, error) {
	res, err := g.wasm.LastNode(context.Background())
	if err != nil {
		return nil, err
	}

	return toNode(res), nil
}

func (g *Graph) PreviousNode(n *Node) (*Node, error) {
	res, err := g.wasm.PrevNode(context.Background(), n.getWasm())
	if err != nil {
		return nil, err
	}

	return toNode(res), nil
}

func (g *Graph) CreateEdgeByName(name string, start, end *Node) (*Edge, error) {
	res, err := g.wasm.Edge(context.Background(), start.getWasm(), end.getWasm(), name, 1)
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) EdgeByName(name string, start, end *Node) (*Edge, error) {
	res, err := g.wasm.Edge(context.Background(), start.getWasm(), end.getWasm(), name, 0)
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) CreateEdgeByID(id ID, start, end *Node) (*Edge, error) {
	res, err := g.wasm.IdEdge(context.Background(), start.getWasm(), end.getWasm(), uint64(id), 1)
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) EdgeByID(id ID, start, end *Node) (*Edge, error) {
	res, err := g.wasm.IdEdge(context.Background(), start.getWasm(), end.getWasm(), uint64(id), 0)
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) CreateSubEdge(e *Edge) (*Edge, error) {
	res, err := g.wasm.SubEdge(context.Background(), e.getWasm(), 1)
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) SubEdge(e *Edge) (*Edge, error) {
	res, err := g.wasm.SubEdge(context.Background(), e.getWasm(), 0)
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) FirstIn(n *Node) (*Edge, error) {
	res, err := g.wasm.FirstIn(context.Background(), n.getWasm())
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) FirstOut(n *Node) (*Edge, error) {
	res, err := g.wasm.FirstOut(context.Background(), n.getWasm())
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) NextIn(e *Edge) (*Edge, error) {
	res, err := g.wasm.NextIn(context.Background(), e.getWasm())
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) NextOut(e *Edge) (*Edge, error) {
	res, err := g.wasm.NextOut(context.Background(), e.getWasm())
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) FirstEdge(n *Node) (*Edge, error) {
	res, err := g.wasm.FirstEdge(context.Background(), n.getWasm())
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) NextEdge(e *Edge, n *Node) (*Edge, error) {
	res, err := g.wasm.NextEdge(context.Background(), e.getWasm(), n.getWasm())
	if err != nil {
		return nil, err
	}

	return toEdge(res), nil
}

func (g *Graph) Contains(o any) (bool, error) {
	res, err := g.wasm.Contains(context.Background(), o)
	if err != nil {
		return false, err
	}

	return res == 1, nil
}

func (g *Graph) Name() (string, error) {
	return wasm.GraphNameOf(context.Background(), g.wasm)
}

func (g *Graph) Delete(obj any) error {
	res, err := g.wasm.Delete(context.Background(), obj)
	if err != nil {
		return err
	}

	return toError(res)
}

func (g *Graph) DeleteSubGraph(sub *Graph) error {
	res, err := g.wasm.DeleteSubGraph(context.Background(), sub.getWasm())
	if err != nil {
		return err
	}

	return toError(res)
}

func (g *Graph) DeleteNode(n *Node) (bool, error) {
	res, err := g.wasm.DeleteNode(context.Background(), n.getWasm())
	if err != nil {
		return false, err
	}

	return res == 1, nil
}

func (g *Graph) DeleteEdge(e *Edge) (bool, error) {
	res, err := g.wasm.DeleteEdge(context.Background(), e.getWasm())
	if err != nil {
		return false, err
	}

	return res == 1, nil
}

func (g *Graph) Attr(kind int, name, value string) (*Symbol, error) {
	res, err := g.wasm.Attr(context.Background(), kind, name, value)
	if err != nil {
		return nil, err
	}

	return toSymbol(res), nil
}

func (g *Graph) NextAttr(kind int, attr *Symbol) (*Symbol, error) {
	res, err := g.wasm.NextAttr(context.Background(), kind, attr.getWasm())
	if err != nil {
		return nil, err
	}

	return toSymbol(res), nil
}

func (g *Graph) CreateSubGraphByName(name string) (*Graph, error) {
	res, err := g.wasm.SubGraph(context.Background(), name, 1)
	if err != nil {
		return nil, err
	}

	return toGraph(res), nil
}

func (g *Graph) SubGraphByName(name string) (*Graph, error) {
	res, err := g.wasm.SubGraph(context.Background(), name, 0)
	if err != nil {
		return nil, err
	}

	return toGraph(res), nil
}

// CreateSubGraphByID returns the subgraph with the given ID, creating it when
// it does not exist. Graphviz 13 made agidsubg a pure lookup, so creation
// goes through agsubg under the ID's decimal name; the subgraph's ID is then
// allocated by the graph's ID discipline, not forced to id.
func (g *Graph) CreateSubGraphByID(graphID ID) (*Graph, error) {
	res, err := g.wasm.IdSubGraph(context.Background(), uint64(graphID))
	if err != nil {
		return nil, err
	}

	if res != nil {
		return toGraph(res), nil
	}

	created, err := g.wasm.SubGraph(context.Background(), strconv.FormatUint(uint64(graphID), 10), 1)
	if err != nil {
		return nil, err
	}

	return toGraph(created), nil
}

func (g *Graph) SubGraphByID(id ID) (*Graph, error) {
	res, err := g.wasm.IdSubGraph(context.Background(), uint64(id))
	if err != nil {
		return nil, err
	}

	return toGraph(res), nil
}

func (g *Graph) FirstSubGraph() (*Graph, error) {
	res, err := g.wasm.FirstSubGraph(context.Background())
	if err != nil {
		return nil, err
	}

	return toGraph(res), nil
}

func (g *Graph) NextSubGraph() (*Graph, error) {
	res, err := g.wasm.NextSubGraph(context.Background())
	if err != nil {
		return nil, err
	}

	return toGraph(res), nil
}

func (g *Graph) NodeNum() (int, error) {
	return g.wasm.NodeNum(context.Background())
}

func (g *Graph) EdgeNum() (int, error) {
	return g.wasm.EdgeNum(context.Background())
}

func (g *Graph) SubGraphNum() (int, error) {
	return g.wasm.SubGraphNum(context.Background())
}

// Degree returns the degree of the given node in the graph, where arguments "in" and
// "out" are C-like booleans that select which edge sets to query.
//
// g.Degree(node, 0, 0) // always returns 0
// g.Degree(node, 0, 1) // returns the node's outdegree
// g.Degree(node, 1, 0) // returns the node's indegree
// g.Degree(node, 1, 1) // returns the node's total degree (indegree + outdegree).
func (g *Graph) Degree(n *Node, in, out int) (int, error) {
	return g.wasm.Degree(context.Background(), n.getWasm(), in, out)
}

// Indegree returns the indegree of the given node in the graph.
//
// Note: While undirected graphs don't normally have a
// notion of indegrees, calling this method on an
// undirected graph will treat it as if it's directed.
// As a result, it's best to avoid calling this method
// on an undirected graph.
func (g *Graph) Indegree(n *Node) (int, error) {
	return g.wasm.Degree(context.Background(), n.getWasm(), 1, 0)
}

// Outdegree returns the outdegree of the given node in the graph.
//
// Note: While undirected graphs don't normally have a
// notion of outdegrees, calling this method on an
// undirected graph will treat it as if it's directed.
// As a result, it's best to avoid calling this method
// on an undirected graph.
func (g *Graph) Outdegree(n *Node) (int, error) {
	return g.wasm.Degree(context.Background(), n.getWasm(), 0, 1)
}

// TotalDegree returns the total degree of the given node in the graph.
// This can be thought of as the total number of edges coming
// in and out of a node.
func (g *Graph) TotalDegree(n *Node) (int, error) {
	return g.wasm.Degree(context.Background(), n.getWasm(), 1, 1)
}

func (g *Graph) CountUniqueEdges(n *Node, in, out int) (int, error) {
	return g.wasm.CountUniqueEdges(context.Background(), n.getWasm(), in, out)
}

func (g *Graph) getWasm() *wasm.Graph {
	if g == nil {
		return nil
	}

	return g.wasm
}

type Node struct {
	wasm *wasm.Node
}

func toNode(v *wasm.Node) *Node {
	if v == nil {
		return nil
	}

	return &Node{wasm: v}
}

func (n *Node) Root() *Graph {
	return toGraph(n.wasm.GetRoot())
}

func (n *Node) Name() (string, error) {
	return wasm.GraphNameOf(context.Background(), n.wasm)
}

func (n *Node) CopyAttr(t *Node) error {
	res, err := wasm.CopyAttr(context.Background(), n.wasm, t.getWasm())
	if err != nil {
		return err
	}

	return toError(res)
}

func (n *Node) GetStr(name string) string {
	v, _ := wasm.GetStr(context.Background(), n.wasm, name)
	return v
}

func (n *Node) SymbolName(sym *Symbol) (string, error) {
	return wasm.GetSymName(context.Background(), n.wasm, sym.getWasm())
}

func (n *Node) Set(name, value string) error {
	res, err := wasm.SetStr(context.Background(), n.wasm, name, value)
	if err != nil {
		return err
	}

	return toError(res)
}

func (n *Node) SetSymbolName(sym *Symbol, value string) error {
	res, err := wasm.SetSymName(context.Background(), n.wasm, sym.getWasm(), value)
	if err != nil {
		return err
	}

	return toError(res)
}

func (n *Node) SafeSet(name, value, def string) error {
	res, err := wasm.SafeSetStr(context.Background(), n.wasm, name, value, def)
	if err != nil {
		return err
	}

	return toError(res)
}

// SafeSetHTML is SafeSet for an HTML-like value. Since Graphviz 13 a value
// set through SafeSet is plain text and an HTML label set that way renders
// as escaped markup; this is the call for a label such as <table>...</table>.
func (n *Node) SafeSetHTML(name, value, def string) error {
	res, err := wasm.SafeSetStrHTML(context.Background(), n.wasm, name, value, def)
	if err != nil {
		return err
	}

	return toError(res)
}

func (n *Node) ReLabel(newname string) error {
	res, err := n.wasm.ReLabel(context.Background(), newname)
	if err != nil {
		return err
	}

	return toError(res)
}

func (n *Node) Before(v *Node) error {
	res, err := n.wasm.Before(context.Background(), v.getWasm())
	if err != nil {
		return err
	}

	return toError(res)
}

func (n *Node) getWasm() *wasm.Node {
	if n == nil {
		return nil
	}

	return n.wasm
}

type Edge struct {
	wasm *wasm.Edge
}

func toEdge(v *wasm.Edge) *Edge {
	if v == nil {
		return nil
	}

	return &Edge{wasm: v}
}

func (e *Edge) Head() (*Node, error) {
	n, err := e.wasm.Head(context.Background())
	if err != nil {
		return nil, err
	}

	return toNode(n), nil
}

func (e *Edge) Tail() (*Node, error) {
	n, err := e.wasm.Tail(context.Background())
	if err != nil {
		return nil, err
	}

	return toNode(n), nil
}

func (e *Edge) Name() (string, error) {
	return wasm.GraphNameOf(context.Background(), e.wasm)
}

func (e *Edge) CopyAttr(t *Edge) error {
	res, err := wasm.CopyAttr(context.Background(), e.wasm, t.getWasm())
	if err != nil {
		return err
	}

	return toError(res)
}

func (e *Edge) GetStr(name string) string {
	v, _ := wasm.GetStr(context.Background(), e.wasm, name)
	return v
}

func (e *Edge) SymbolName(sym *Symbol) (string, error) {
	return wasm.GetSymName(context.Background(), e.wasm, sym.getWasm())
}

func (e *Edge) Set(name, value string) error {
	res, err := wasm.SetStr(context.Background(), e.wasm, name, value)
	if err != nil {
		return err
	}

	return toError(res)
}

func (e *Edge) SetSymbolName(sym *Symbol, value string) error {
	res, err := wasm.SetSymName(context.Background(), e.wasm, sym.getWasm(), value)
	if err != nil {
		return err
	}

	return toError(res)
}

func (e *Edge) SafeSet(name, value, def string) error {
	res, err := wasm.SafeSetStr(context.Background(), e.wasm, name, value, def)
	if err != nil {
		return err
	}

	return toError(res)
}

// SafeSetHTML is SafeSet for an HTML-like value. Since Graphviz 13 a value
// set through SafeSet is plain text and an HTML label set that way renders
// as escaped markup; this is the call for a label such as <table>...</table>.
func (e *Edge) SafeSetHTML(name, value, def string) error {
	res, err := wasm.SafeSetStrHTML(context.Background(), e.wasm, name, value, def)
	if err != nil {
		return err
	}

	return toError(res)
}

func (e *Edge) getWasm() *wasm.Edge {
	if e == nil {
		return nil
	}

	return e.wasm
}

type Desc struct {
	wasm *wasm.GraphDescriptor
}

func toDesc(v *wasm.GraphDescriptor) *Desc {
	if v == nil {
		return nil
	}

	return &Desc{wasm: v}
}

func (d *Desc) getWasm() *wasm.GraphDescriptor {
	if d == nil {
		return nil
	}

	return d.wasm
}

// Symbol symbol in one of the above dictionaries.
type Symbol struct {
	wasm *wasm.Sym
}

func toSymbol(v *wasm.Sym) *Symbol {
	if v == nil {
		return nil
	}

	return &Symbol{wasm: v}
}

func (s *Symbol) Name() string {
	return s.wasm.GetName()
}

func (s *Symbol) DefaultValue() string {
	return s.wasm.GetDefval()
}

func (s *Symbol) ID() int {
	return int(s.wasm.GetId())
}

func (s *Symbol) Kind() uint {
	return uint(s.wasm.GetKind())
}

func (s *Symbol) getWasm() *wasm.Sym {
	if s == nil {
		return nil
	}

	return s.wasm
}

type ID uint64

func ParseBytes(bytes []byte) (*Graph, error) {
	if errInit != nil {
		return nil, errInit
	}

	graph, err := wasm.MemRead(context.Background(), string(bytes))
	if err != nil {
		return nil, err
	}

	if graph == nil {
		return nil, parseError()
	}

	return toGraph(graph), nil
}

// parseError is the error for a read or open that returned no graph.
func parseError() error {
	if err := lastError(); err != nil {
		return err
	}

	return fmt.Errorf("%w: no graph was produced", ErrGraphviz)
}

func ParseFile(path string) (*Graph, error) {
	// #nosec G304 -- reading the file the caller names is this function's purpose
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	return ParseBytes(file)
}

// Open makes a new, empty graph of the given name and kind (Directed,
// StrictDirected, UnDirected or StrictUnDirected).
func Open(name string, desc *Desc) (*Graph, error) {
	if errInit != nil {
		return nil, errInit
	}

	graph, err := wasm.Open(context.Background(), name, desc.getWasm(), nil)
	if err != nil {
		return nil, err
	}

	if graph == nil {
		return nil, parseError()
	}

	return toGraph(graph), nil
}

type ObjectTag int

var (
	GRAPH   ObjectTag = ObjectTag(wasm.GRAPH)
	NODE    ObjectTag = ObjectTag(wasm.NODE)
	OUTEDGE ObjectTag = ObjectTag(wasm.OUT_EDGE)
	INEDGE  ObjectTag = ObjectTag(wasm.IN_EDGE)
	EDGE    ObjectTag = ObjectTag(wasm.EDGE)
)

// Canon returns s in the form the DOT writer would print it: quoted and
// escaped as needed, or wrapped in angle brackets when html is non-zero.
// Graphviz 13 removed agcanon; this is what it did, over agstrcanon.
func Canon(s string, html int) (string, error) {
	if html != 0 {
		return "<" + s + ">", nil
	}

	return CanonStr(s)
}

// CanonStr returns s quoted and escaped as the DOT writer would print it.
// Graphviz 14 removed agcanonStr in favour of agstrcanon with a caller-owned
// buffer; the buffer is sized here.
func CanonStr(s string) (string, error) {
	return wasm.StrCanon(context.Background(), s, string(make([]byte, 2*len(s)+3)))
}

// toError maps a Graphviz result code: zero is success, anything else is a
// failure, with the message Graphviz reported when it reported one.
func toError(result int) error {
	if result == 0 {
		return nil
	}

	if err := lastError(); err != nil {
		return err
	}

	return fmt.Errorf("%w: call failed with code %d and no message", ErrGraphviz, result)
}

// ErrGraphviz is the error every failed Graphviz call wraps; the message
// Graphviz reported follows it, so errors.Is tells a Graphviz failure from
// any other and the text is still there to read.
var ErrGraphviz = errors.New("graphviz")

// lastError is the latest message Graphviz reported as an error, as an
// error, or nil when it reported none since the last read.
func lastError() error {
	if text := wasm.TakeLastError(); text != "" {
		return fmt.Errorf("%w: %s", ErrGraphviz, strings.TrimRight(text, "\n"))
	}

	return nil
}
