package graphviz_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// fillRecorder is a render engine that keeps the fill flag Graphviz passes
// with each shape, by kind of shape.
type fillRecorder struct {
	*graphviz.DefaultRenderEngine

	fills map[string][]bool
}

func (r *fillRecorder) Ellipse(_ context.Context, _ *graphviz.Job, _ []*graphviz.PointFloat, filled bool) error {
	r.fills["ellipse"] = append(r.fills["ellipse"], filled)

	return nil
}

func (r *fillRecorder) Polygon(_ context.Context, _ *graphviz.Job, _ []*graphviz.PointFloat, filled bool) error {
	r.fills["polygon"] = append(r.fills["polygon"], filled)

	return nil
}

func (r *fillRecorder) BezierCurve(_ context.Context, _ *graphviz.Job, _ []*graphviz.PointFloat, filled bool) error {
	r.fills["bezier"] = append(r.fills["bezier"], filled)

	return nil
}

// A callback's 32-bit arguments reach the engine as Graphviz passed them.
// wazero hands every argument over in a 64-bit word and leaves the high half
// of a 32-bit one undefined; on amd64 it is not zero, and read whole it made
// every edge a filled shape.
func TestCallbackNarrowArguments(t *testing.T) {
	ctx := t.Context()

	engine := &fillRecorder{
		DefaultRenderEngine: new(graphviz.DefaultRenderEngine),
		fills:               map[string][]bool{},
	}

	render, err := graphviz.NewRenderPlugin(ctx, "fills", engine)
	if err != nil {
		t.Fatal(err)
	}

	device, err := graphviz.NewDevicePlugin(ctx, "fills:fills")
	if err != nil {
		t.Fatal(err)
	}

	g, err := graphviz.NewWithPlugins(ctx, render, device)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	// One outlined node, one filled node, two edges with the default
	// arrowhead (a filled polygon) and no box around the page.
	graph, err := graphviz.ParseBytes([]byte(`digraph {
		bgcolor=transparent
		a
		b [style=filled]
		a -> b -> a
	}`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var buf bytes.Buffer
	if err := g.Render(ctx, graph, "fills", &buf); err != nil {
		t.Fatal(err)
	}

	want := map[string][]bool{
		"ellipse": {false, true},
		"bezier":  {false, false},
		"polygon": {true, true},
	}

	for kind, flags := range want {
		got := engine.fills[kind]
		if len(got) != len(flags) {
			t.Errorf("%s: %d shapes drawn, want %d", kind, len(got), len(flags))

			continue
		}

		for i := range flags {
			if got[i] != flags[i] {
				t.Errorf("%s %d: filled = %v, want %v", kind, i, got[i], flags[i])
			}
		}
	}
}
