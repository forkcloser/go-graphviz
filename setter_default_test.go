package graphviz_test

import (
	"bytes"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/cgraph"
)

// pieces is the graph every row of TestSetterTouchesOneObject starts from:
// a -> b -> c, with b in cluster_x and c in cluster_y.
type pieces struct {
	graph    *graphviz.Graph
	x, y     *cgraph.Graph
	a, b, c  *cgraph.Node
	ab, bc   *cgraph.Edge
	directed bool
}

func newPieces(t *testing.T, g *graphviz.Graphviz, directed bool) pieces {
	t.Helper()

	kind := graphviz.Directed
	if !directed {
		kind = graphviz.UnDirected
	}

	graph, err := g.Graph(graphviz.WithDirectedType(kind))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	must := func(err error) {
		t.Helper()

		if err != nil {
			t.Fatal(err)
		}
	}

	p := pieces{graph: graph, directed: directed}

	p.a, err = graph.CreateNodeByName("a")
	must(err)

	p.x, err = graph.CreateSubGraphByName("cluster_x")
	must(err)

	p.b, err = p.x.CreateNodeByName("b")
	must(err)

	p.y, err = graph.CreateSubGraphByName("cluster_y")
	must(err)

	p.c, err = p.y.CreateNodeByName("c")
	must(err)

	p.ab, err = graph.CreateEdgeByName("", p.a, p.b)
	must(err)

	p.bc, err = graph.CreateEdgeByName("", p.b, p.c)
	must(err)

	return p
}

// A typed setter sets its attribute on the object it is called on, and
// leaves every other object as it was. Each row builds the graph through
// the setters and draws it; the drawing is that of the same graph written
// in DOT with the attribute on that one object.
//
// A setter declares its attribute for the whole graph when it is the first
// to use it, with a default, and every other object of the kind takes that
// default as if it were written on it. Any default but the empty one, which
// Graphviz reads as unset, redraws them: every other edge labelled with its
// name, a doublecircle with one ring, arrowheads in an undirected graph.
// Each row is a setter that did, beside what shows it.
func TestSetterTouchesOneObject(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	for _, tc := range []struct {
		name       string
		undirected bool
		set        func(p pieces)
		// The same in DOT: what is written in the graph, in cluster_x, on
		// b, in cluster_y, on a, on the first edge and on the second.
		root, x, b, y, a, ab, bc string
	}{
		{
			name: "no setter: the two ways of building draw alike",
			set:  func(pieces) {},
		},
		{
			name: "an edge's label",
			set:  func(p pieces) { p.ab.SetLabel("one") },
			ab:   `[label=one]`,
		},
		{
			name: "a cluster's label",
			set:  func(p pieces) { p.x.SetLabel("one") },
			x:    `label=one;`,
		},
		{
			name: "an edge's label in HTML",
			set:  func(p pieces) { p.ab.SetLabelHTML("<b>one</b>") },
			ab:   `[label=<<b>one</b>>]`,
		},
		{
			name: "a cluster's label in HTML",
			set:  func(p pieces) { p.x.SetLabelHTML("<b>one</b>") },
			x:    `label=<<b>one</b>>;`,
		},
		{
			name: "a node's color, beside a filled node",
			set: func(p pieces) {
				p.b.SetStyle(cgraph.FilledNodeStyle)
				p.a.SetColor("red")
			},
			a: `[color=red]`, b: `[style=filled]`,
		},
		{
			name: "a node's fill color, beside a filled node with a color",
			set: func(p pieces) {
				p.b.SetStyle(cgraph.FilledNodeStyle)
				p.b.SetColor("red")
				p.a.SetFillColor("yellow")
			},
			a: `[fillcolor=yellow]`, b: `[style=filled color=red]`,
		},
		{
			name: "a node's peripheries, beside a doublecircle",
			set: func(p pieces) {
				p.b.SetShape(cgraph.DoubleCircleShape)
				p.a.SetPeripheries(3)
			},
			a: `[peripheries=3]`, b: `[shape=doublecircle]`,
		},
		{
			name:       "an edge's direction, in an undirected graph",
			undirected: true,
			set:        func(p pieces) { p.ab.SetDir(cgraph.ForwardDir) },
			ab:         `[dir=forward]`,
		},
		{
			name: "a cluster's label at the bottom, beside a labelled cluster",
			set: func(p pieces) {
				p.x.SetLabel("one")
				p.y.SetLabel("two")
				p.x.SetLabelLocation(cgraph.BottomLocation)
			},
			x: `label=one; labelloc=b;`, y: `label=two;`,
		},
		{
			name: "an edge's length, under a layout whose edges are shorter",
			set: func(p pieces) {
				p.graph.SetLayout("fdp")
				p.ab.SetLen(2)
			},
			root: `layout=fdp;`, ab: `[len=2]`,
		},
		{
			name: "an edge's tail port",
			set:  func(p pieces) { p.ab.SetTailPort("n") },
			ab:   `[tailport=n]`,
		},
		{
			name: "the angle of an edge's end labels, beside an edge with one",
			set: func(p pieces) {
				p.bc.SetHeadLabel("head")
				p.ab.SetLabelAngle(40)
			},
			ab: `[labelangle=40]`, bc: `[headlabel=head]`,
		},
		{
			name: "the distance of an edge's end labels, beside an edge with one",
			set: func(p pieces) {
				p.bc.SetHeadLabel("head")
				p.ab.SetLabelDistance(3)
			},
			ab: `[labeldistance=3]`, bc: `[headlabel=head]`,
		},
		{
			name: "the color of an edge's end labels, beside an edge with a font color",
			set: func(p pieces) {
				p.bc.SetHeadLabel("head")
				p.bc.SetFontColor("blue")
				p.ab.SetLabelFontColor("red")
			},
			ab: `[labelfontcolor=red]`, bc: `[headlabel=head fontcolor=blue]`,
		},
		{
			name: "the size of an edge's end labels, beside an edge with a font size",
			set: func(p pieces) {
				p.bc.SetHeadLabel("head")
				p.bc.SetFontSize(30)
				p.ab.SetLabelFontSize(20)
			},
			ab: `[labelfontsize=20]`, bc: `[headlabel=head fontsize=30]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			//nolint:contextcheck // opening a graph and its objects takes no context
			p := newPieces(t, g, !tc.undirected)
			tc.set(p)

			var got bytes.Buffer
			if err := g.Render(ctx, p.graph, graphviz.SVG, &got); err != nil {
				t.Fatal(err)
			}

			kind, arrow := "digraph", "->"
			if tc.undirected {
				kind, arrow = "graph", "--"
			}

			// In the order newPieces makes them: the drawing numbers its
			// objects as they come.
			source := kind + " { " + tc.root + " a " + tc.a + "; " +
				"subgraph cluster_x { " + tc.x + " b " + tc.b + " } " +
				"subgraph cluster_y { " + tc.y + " c } " +
				"a " + arrow + " b " + tc.ab + "; b " + arrow + " c " + tc.bc + " }"

			written, err := graphviz.ParseBytes([]byte(source))
			if err != nil {
				t.Fatalf("%s: %v", source, err)
			}

			t.Cleanup(func() { closeOrError(t, written.Close) })

			var want bytes.Buffer
			if err := g.Render(ctx, written, graphviz.SVG, &want); err != nil {
				t.Fatal(err)
			}

			if !bytes.Equal(got.Bytes(), want.Bytes()) {
				t.Errorf(
					"the graph built with setters is not drawn as\n%s\n\ngot:\n%s\nwant:\n%s",
					source,
					got.String(),
					want.String(),
				)
			}
		})
	}
}

// Graphviz's kinds of object, as Graph.NextAttr takes them.
const (
	kindGraph = 0
	kindNode  = 1
	kindEdge  = 2
)

// declared is the attributes a graph declares for a kind of object, each
// with its default.
func declared(t *testing.T, graph *graphviz.Graph, kind int) map[string]string {
	t.Helper()

	defaults := map[string]string{}

	for sym, err := graph.NextAttr(kind, nil); sym != nil; sym, err = graph.NextAttr(kind, sym) {
		if err != nil {
			t.Fatal(err)
		}

		defaults[sym.Name()] = sym.DefaultValue()
	}

	return defaults
}

// anyValue is a value for a setter's parameter: one of its kind, whatever
// the attribute. A value Graphviz makes nothing of is written the same on
// the object in both graphs compared.
func anyValue(param reflect.Type) (reflect.Value, bool) {
	var value any

	switch param.Kind() { //nolint:exhaustive // the kinds the setters take
	case reflect.String:
		value = "1"
	case reflect.Bool:
		value = true
	case reflect.Int:
		value = 2
	case reflect.Float64:
		value = 2.5
	default:
		return reflect.Value{}, false
	}

	return reflect.ValueOf(value).Convert(param), true
}

// Every typed setter of a node, of an edge, of a cluster and of the graph
// does what writing its attribute on that one object in DOT does: it
// declares the attribute with the empty default, as Graphviz's parser does
// (bindattrs in lib/cgraph/grammar.y), and the graph is drawn as that DOT.
// The attribute it wrote is read back from the object, so the row needs no
// table: the next setter added is held too. A graph's attribute is written
// last in the DOT, where it reaches no subgraph, as a setter reaches none.
func TestEverySetterDrawsItsDOT(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	// A value Graphviz refuses is refused in both graphs; its message is
	// not compared, since it carries the warnings of the renders before.
	draw := func(graph *graphviz.Graph) string {
		var out bytes.Buffer
		if err := g.Render(ctx, graph, graphviz.SVG, &out); err != nil {
			return "an error"
		}

		return out.String()
	}

	for _, target := range []struct {
		name string
		kind int
		of   func(p pieces) any
		read func(p pieces, name string) string
	}{
		{"Node", kindNode, func(p pieces) any { return p.a }, func(p pieces, name string) string { return p.a.GetStr(name) }},
		{"Edge", kindEdge, func(p pieces) any { return p.ab }, func(p pieces, name string) string { return p.ab.GetStr(name) }},
		{"Cluster", kindGraph, func(p pieces) any { return p.x }, func(p pieces, name string) string { return p.x.GetStr(name) }},
		{"Graph", kindGraph, func(p pieces) any { return p.graph }, func(p pieces, name string) string { return p.graph.GetStr(name) }},
	} {
		setters := reflect.TypeOf(target.of(pieces{}))

		for setter := range setters.Methods() {
			// A typed setter returns its object; an HTML label is no
			// string to write back, and has its rows above.
			if !strings.HasPrefix(setter.Name, "Set") || strings.HasSuffix(setter.Name, "HTML") ||
				setter.Type.NumOut() != 1 || setter.Type.Out(0) != setters {
				continue
			}

			t.Run(target.name+"."+setter.Name, func(t *testing.T) {
				p := newPieces(t, g, true)
				before := declared(t, p.graph, target.kind)

				args := []reflect.Value{reflect.ValueOf(target.of(p))}

				for at := 1; at < setter.Type.NumIn(); at++ {
					value, ok := anyValue(setter.Type.In(at))
					if !ok {
						t.Skipf("no value for a %s", setter.Type.In(at))
					}

					args = append(args, value)
				}

				setter.Func.Call(args)

				if err := p.graph.Err(); err != nil {
					t.Skipf("the setter refuses the value: %v", err)
				}

				var written []string

				for name, def := range declared(t, p.graph, target.kind) {
					if _, was := before[name]; was {
						continue
					}

					written = append(written, name+"="+strconv.Quote(target.read(p, name)))

					// The graph's own value is the default of its kind.
					if def != "" && target.name != "Graph" {
						t.Errorf("%s is declared with the default %q, want the empty one", name, def)
					}
				}

				if len(written) == 0 {
					t.Skip("the attribute was declared before the setter: it declares nothing")
				}

				var a, ab, x, root string

				switch target.name {
				case "Node":
					a = "[" + strings.Join(written, " ") + "]"
				case "Edge":
					ab = "[" + strings.Join(written, " ") + "]"
				case "Cluster":
					x = strings.Join(written, "; ") + ";"
				default:
					root = strings.Join(written, "; ") + ";"
				}

				source := "digraph { a " + a + "; subgraph cluster_x { " + x + " b } subgraph cluster_y { c } " +
					"a -> b " + ab + "; b -> c; " + root + " }"

				twin, err := graphviz.ParseBytes([]byte(source))
				if err != nil {
					t.Fatalf("%s: %v", source, err)
				}

				t.Cleanup(func() { closeOrError(t, twin.Close) })

				if got, want := draw(p.graph), draw(twin); got != want {
					t.Errorf(
						"the graph built with the setter is not drawn as\n%s\n\ngot:\n%s\nwant:\n%s",
						source,
						got,
						want,
					)
				}
			})
		}
	}
}
