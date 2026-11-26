// Package cgraph mirrors libcgraph, Graphviz's graph library: graphs, nodes,
// edges, subgraphs, their attributes and the DOT reader and writer, as typed
// handles over the WebAssembly module's memory.
package cgraph

import (
	"context"
	"errors"
	"fmt"
	"iter"
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
	self, other := g.wasm, t.getWasm()

	return checked(func(ctx context.Context) (int, error) {
		return wasm.CopyAttr(ctx, self, other)
	})
}

func (g *Graph) GetStr(name string) string {
	v, _ := wasm.GetStr(context.Background(), g.wasm, name)
	return v
}

func (g *Graph) SymbolName(sym *Symbol) (string, error) {
	return wasm.GetSymName(context.Background(), g.wasm, sym.getWasm())
}

func (g *Graph) Set(name, value string) error {
	self := g.wasm

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SetStr(ctx, self, name, value)
	})
}

func (g *Graph) SetSymbolName(sym *Symbol, value string) error {
	self, symbol := g.wasm, sym.getWasm()

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SetSymName(ctx, self, symbol, value)
	})
}

func (g *Graph) SafeSet(name, value, def string) error {
	self := g.wasm

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SafeSetStr(ctx, self, name, value, def)
	})
}

// SafeSetHTML is SafeSet for an HTML-like value. Since Graphviz 13 a value
// set through SafeSet is plain text and an HTML label set that way renders
// as escaped markup; this is the call for a label such as <table>...</table>.
func (g *Graph) SafeSetHTML(name, value, def string) error {
	self := g.wasm

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SafeSetStrHTML(ctx, self, name, value, def)
	})
}

func (g *Graph) Close() error {
	if root := g.wasm.GetRoot(); wasm.WasmPtr(root) == wasm.WasmPtr(g.wasm) {
		forgetSetterErrors(root)
	}

	return checked(g.wasm.Close)
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
	self := g.wasm

	return checked(func(ctx context.Context) (int, error) {
		return self.Delete(ctx, obj)
	})
}

func (g *Graph) DeleteSubGraph(sub *Graph) error {
	self, other := g.wasm, sub.getWasm()

	return checked(func(ctx context.Context) (int, error) {
		return self.DeleteSubGraph(ctx, other)
	})
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
	self, other := n.wasm, t.getWasm()

	return checked(func(ctx context.Context) (int, error) {
		return wasm.CopyAttr(ctx, self, other)
	})
}

func (n *Node) GetStr(name string) string {
	v, _ := wasm.GetStr(context.Background(), n.wasm, name)
	return v
}

func (n *Node) SymbolName(sym *Symbol) (string, error) {
	return wasm.GetSymName(context.Background(), n.wasm, sym.getWasm())
}

func (n *Node) Set(name, value string) error {
	self := n.wasm

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SetStr(ctx, self, name, value)
	})
}

func (n *Node) SetSymbolName(sym *Symbol, value string) error {
	self, symbol := n.wasm, sym.getWasm()

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SetSymName(ctx, self, symbol, value)
	})
}

func (n *Node) SafeSet(name, value, def string) error {
	self := n.wasm

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SafeSetStr(ctx, self, name, value, def)
	})
}

// SafeSetHTML is SafeSet for an HTML-like value. Since Graphviz 13 a value
// set through SafeSet is plain text and an HTML label set that way renders
// as escaped markup; this is the call for a label such as <table>...</table>.
func (n *Node) SafeSetHTML(name, value, def string) error {
	self := n.wasm

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SafeSetStrHTML(ctx, self, name, value, def)
	})
}

func (n *Node) ReLabel(newname string) error {
	self := n.wasm

	return checked(func(ctx context.Context) (int, error) {
		return self.ReLabel(ctx, newname)
	})
}

func (n *Node) Before(v *Node) error {
	self, other := n.wasm, v.getWasm()

	return checked(func(ctx context.Context) (int, error) {
		return self.Before(ctx, other)
	})
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
	self, other := e.wasm, t.getWasm()

	return checked(func(ctx context.Context) (int, error) {
		return wasm.CopyAttr(ctx, self, other)
	})
}

func (e *Edge) GetStr(name string) string {
	v, _ := wasm.GetStr(context.Background(), e.wasm, name)
	return v
}

func (e *Edge) SymbolName(sym *Symbol) (string, error) {
	return wasm.GetSymName(context.Background(), e.wasm, sym.getWasm())
}

func (e *Edge) Set(name, value string) error {
	self := e.wasm

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SetStr(ctx, self, name, value)
	})
}

func (e *Edge) SetSymbolName(sym *Symbol, value string) error {
	self, symbol := e.wasm, sym.getWasm()

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SetSymName(ctx, self, symbol, value)
	})
}

func (e *Edge) SafeSet(name, value, def string) error {
	self := e.wasm

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SafeSetStr(ctx, self, name, value, def)
	})
}

// SafeSetHTML is SafeSet for an HTML-like value. Since Graphviz 13 a value
// set through SafeSet is plain text and an HTML label set that way renders
// as escaped markup; this is the call for a label such as <table>...</table>.
func (e *Edge) SafeSetHTML(name, value, def string) error {
	self := e.wasm

	return checked(func(ctx context.Context) (int, error) {
		return wasm.SafeSetStrHTML(ctx, self, name, value, def)
	})
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

// ParseBytes reads the first graph of the DOT text given, and nothing of
// what follows it: a text may hold several graphs, which ParseAll reads in
// turn.
//
// An error that is ErrNoGraph is a text that ends before a graph begins,
// with nothing Graphviz reports as wrong: empty, blank, comments alone, and
// also a comment or a quote that is never closed. The text ends at its
// first NUL byte, as a C string does. Any other error is a text Graphviz
// could not read, with what it reported.
func ParseBytes(bytes []byte) (*Graph, error) {
	if errInit != nil {
		return nil, errInit
	}

	return readGraph(func(ctx context.Context) (*wasm.Graph, error) {
		return wasm.MemRead(ctx, string(bytes))
	})
}

// ParseAll reads every graph of the DOT text given, in the order the text
// has them: the sequence yields each graph once it is read, the caller's to
// close, and reads the next when asked for it.
//
// A graph that starts on the line another ends on is read, where Graphviz's
// dot loses it with the rest of that line, and so is one that follows the
// semicolon some write after a graph's brace. Anything else that follows a
// graph on its line (a brace too many, a word, a semicolon with no graph
// after it) is dropped to the end of the line, as Graphviz's dot drops it.
//
// A text with no graph in it (empty, blank, comments alone) yields nothing.
// A part Graphviz cannot read ends the sequence: it yields the error, after
// the graphs that came before, and nothing of what follows is read, as with
// Graphviz's dot. The text ends at its first NUL byte, as a C string does.
//
// The line an error names is the line of the text, counted from its start:
// a "# N" line directive in the text renumbers the lines of the graph it
// precedes and of no graph after that one.
func ParseAll(text []byte) iter.Seq2[*Graph, error] {
	return func(yield func(*Graph, error) bool) {
		reader, err := openReader(text)
		if err != nil {
			yield(nil, err)

			return
		}

		// Whatever ends the sequence frees the reader: its end, an error, a
		// caller that stops, a loop body that panics. A close fails only
		// once the module has, which the read before it has then said.
		defer func() { _ = reader.Close(context.Background()) }()

		for {
			graph, err := readGraph(reader.Next)
			if err != nil {
				// ErrNoGraph is the end of the text.
				if !errors.Is(err, ErrNoGraph) {
					yield(nil, err)
				}

				return
			}

			if !yield(graph, nil) {
				return
			}
		}
	}
}

// openReader is a reader at the start of text, which it has copied.
func openReader(text []byte) (*wasm.Reader, error) {
	if errInit != nil {
		return nil, errInit
	}

	reader, err := wasm.ReaderOpen(context.Background(), string(text))
	if err != nil {
		return nil, err
	}

	if reader == nil {
		return nil, fmt.Errorf("%w: a reader for %d bytes of DOT", wasm.ErrOutOfMemory, len(text))
	}

	return reader, nil
}

// readGraph is the graph read returns, or the error of a read that returned
// none: ErrNoGraph when Graphviz reported nothing, what it reported
// otherwise.
func readGraph(read func(context.Context) (*wasm.Graph, error)) (*Graph, error) {
	var (
		graph    *wasm.Graph
		reported error
	)

	// The parse and the read of what Graphviz reported during it are one
	// stay in the module, so that the errors are this parse's, as in
	// checked.
	err := wasm.Exclusive(context.Background(), func(ctx context.Context) error {
		mark := wasm.ErrorMark()

		var err error

		graph, err = read(ctx)
		reported = errorSince(mark)

		return err
	})
	if err != nil {
		return nil, err
	}

	if graph == nil {
		if reported != nil {
			return nil, reported
		}

		return nil, fmt.Errorf("%w: %w", ErrGraphviz, ErrNoGraph)
	}

	// A parse that reported an error is a failed parse, graph or no graph.
	// Graphviz's parser closes the graph on a syntax error and returns
	// none; when it gives up for want of stack ("memory exhausted") it
	// returns what it had built, and the caller drew that.
	if reported != nil {
		if closeErr := toGraph(graph).Close(); closeErr != nil {
			return nil, errors.Join(reported, closeErr)
		}

		return nil, reported
	}

	return toGraph(graph), nil
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

	var (
		graph   *wasm.Graph
		failure error
	)

	// One stay in the module for the call and for what Graphviz reported
	// during it, and that alone, as in checked.
	err := wasm.Exclusive(context.Background(), func(ctx context.Context) error {
		mark := wasm.ErrorMark()

		var err error

		graph, err = wasm.Open(ctx, name, desc.getWasm(), nil)
		reported := errorSince(mark)

		if err == nil && graph == nil {
			if failure = reported; failure == nil {
				failure = fmt.Errorf("%w: no graph was produced", ErrGraphviz)
			}
		}

		return err
	})
	if err != nil {
		return nil, err
	}

	if failure != nil {
		return nil, failure
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
	// Not 2n+3, the size Graphviz gives its own: a character is two bytes
	// at most and a line break, two more, can follow each.
	return wasm.StrCanon(context.Background(), s, string(make([]byte, 4*len(s)+3)))
}

// checked makes one Graphviz call that returns a result code, and its
// error: zero is success, anything else a failure, with the message
// Graphviz reported when it reported one.
//
// The call and the reading of that message are one stay in the module.
// Read after it, the message could be one another goroutine's call
// reported in between, and this call's own could be taken by that one.
//
// What Graphviz had reported before the call is not the call's, and is left
// where it is: the call may be made from a callback of a layout or a
// render, inside that one's stay, and what was reported so far is that
// one's to read at its end. What is reported during the call is taken
// whatever the result, so that none of it stays for another call to find.
//
// A caller reads its handles before it calls: a nil one then panics in the
// caller, not in the module's turn.
func checked(call func(context.Context) (int, error)) error {
	var failure error

	err := wasm.Exclusive(context.Background(), func(ctx context.Context) error {
		mark := wasm.ErrorMark()

		result, err := call(ctx)
		reported := errorSince(mark)

		if err != nil || result == 0 {
			return err
		}

		if failure = reported; failure == nil {
			failure = fmt.Errorf("%w: call failed with code %d and no message", ErrGraphviz, result)
		}

		return nil
	})
	if err != nil {
		return err
	}

	return failure
}

// ErrGraphviz is the error every failed Graphviz call wraps; the message
// Graphviz reported follows it, so errors.Is tells a Graphviz failure from
// any other and the text is still there to read.
var ErrGraphviz = errors.New("graphviz")

// ErrNoGraph is the error of a DOT text that holds no graph and nothing
// Graphviz reports as wrong: the end of the text came before a graph did.
// Graphviz's dot renders nothing for such an input, and succeeds.
var ErrNoGraph = errors.New("no graph was produced")

// errorSince is what Graphviz reported as an error since mark, as an error,
// or nil when it reported nothing; what it had reported before stays.
func errorSince(mark int) error {
	if text := strings.Trim(wasm.TakeErrorsSince(mark), "\n"); text != "" {
		return fmt.Errorf("%w: %s", ErrGraphviz, text)
	}

	return nil
}
