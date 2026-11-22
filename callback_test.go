package graphviz_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
)

var errPage = errors.New("page refused")

// closeOrError runs a Close at cleanup and reports its error, so a test's
// teardown is checked without a deferred call the linters cannot see.
func closeOrError(t *testing.T, closeFn func() error) {
	t.Helper()

	if err := closeFn(); err != nil {
		t.Error(err)
	}
}

// refusingEngine is a render engine whose BeginPage fails, the first
// callback of a job that has something to fail.
type refusingEngine struct {
	*graphviz.DefaultRenderEngine
}

func (*refusingEngine) BeginPage(_ context.Context, _ *graphviz.Job) error {
	return errPage
}

// An error returned by a render callback reaches the caller as that error,
// and the instance it happened on keeps working: the host side parks the
// error instead of panicking through Graphviz's frames.
func TestRenderEngineError(t *testing.T) {
	ctx := t.Context()

	render, err := graphviz.NewRenderPlugin(ctx, "refuse", &refusingEngine{new(graphviz.DefaultRenderEngine)})
	if err != nil {
		t.Fatal(err)
	}

	device, err := graphviz.NewDevicePlugin(ctx, "refuse:refuse")
	if err != nil {
		t.Fatal(err)
	}

	g, err := graphviz.NewWithPlugins(ctx, render, device)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var buf bytes.Buffer
	if err := g.Render(ctx, graph, "refuse", &buf); !errors.Is(err, errPage) {
		t.Fatalf("Render returned %v, want %v", err, errPage)
	}

	buf.Reset()

	if err := g.Render(ctx, graph, graphviz.SVG, &buf); err != nil {
		t.Fatalf("the instance no longer renders after a callback error: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("empty SVG after a callback error")
	}
}

// A node image Graphviz accepts from its header but Go cannot decode is the
// realistic way a callback fails: the decode error comes back, and the
// instance still renders afterwards. The image lives in a file system in
// memory: Graphviz keeps the files of the images it has sized open for the
// life of the process, and Windows will not remove an open file.
func TestLoadImageError(t *testing.T) {
	ctx := t.Context()

	logo, err := os.ReadFile(filepath.Join("testdata", "logo.png"))
	if err != nil {
		t.Fatal(err)
	}

	graphviz.SetFileSystem(fstest.MapFS{"truncated.png": &fstest.MapFile{Data: logo[:200]}})
	defer graphviz.SetFileSystem(nil)

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := g.Graph()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	n, err := graph.CreateNodeByName("n")
	if err != nil {
		t.Fatal(err)
	}

	n.SetLabel("").SetImage("truncated.png")

	var buf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.PNG, &buf); err == nil {
		t.Fatal("rendering a truncated image returned no error")
	} else if !strings.Contains(err.Error(), "truncated.png") {
		t.Fatalf("the error does not name the image: %v", err)
	}

	buf.Reset()

	if err := g.Render(ctx, graph, graphviz.SVG, &buf); err != nil {
		t.Fatalf("the instance no longer renders after a callback error: %v", err)
	}
}

// panickingEngine is a render engine whose BeginPage panics.
type panickingEngine struct {
	*graphviz.DefaultRenderEngine
}

func (*panickingEngine) BeginPage(_ context.Context, _ *graphviz.Job) error {
	panic("page panicked")
}

// A panic in a render callback reaches the caller as an error, and the
// instance it happened on keeps working, as with a returned error: the
// panic used to unwind through Graphviz's frames, skipping its cleanup, and
// the next render on the instance failed.
func TestRenderEnginePanic(t *testing.T) {
	ctx := t.Context()

	render, err := graphviz.NewRenderPlugin(ctx, "panic", &panickingEngine{new(graphviz.DefaultRenderEngine)})
	if err != nil {
		t.Fatal(err)
	}

	device, err := graphviz.NewDevicePlugin(ctx, "panic:panic")
	if err != nil {
		t.Fatal(err)
	}

	g, err := graphviz.NewWithPlugins(ctx, render, device)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte("digraph { a -> b }"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var buf bytes.Buffer

	err = g.Render(ctx, graph, "panic", &buf)
	if !errors.Is(err, gvc.ErrCallbackPanic) || !strings.Contains(err.Error(), "page panicked") {
		t.Fatalf("Render returned %v, want %v carrying the panic's value", err, gvc.ErrCallbackPanic)
	}

	buf.Reset()

	if err := g.Render(ctx, graph, graphviz.SVG, &buf); err != nil {
		t.Fatalf("the instance no longer renders after a callback panic: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("empty SVG after a callback panic")
	}
}

// cornerPanickingEngine panics in a callback Graphviz hands a shape's
// corners, which the bridge copies for it.
type cornerPanickingEngine struct {
	*graphviz.DefaultRenderEngine
}

func (*cornerPanickingEngine) Polygon(context.Context, *graphviz.Job, []*graphviz.PointFloat, bool) error {
	panic("polygon panicked")
}

// A render whose callback fails or panics leaves nothing in the module. The
// callbacks Graphviz goes on to make are skipped, and the bridge has made
// copies of their arguments by then, a point for each corner of a shape, a
// string for each name: skipped before they were read, or passed over by a
// panic, the copies stayed, some 300 bytes a node.
func TestAFailedCallbackLeavesNoMemory(t *testing.T) {
	ctx := t.Context()

	var src strings.Builder

	src.WriteString("digraph {")

	for i := range 50 {
		fmt.Fprintf(&src, " n%d [shape=box]; n%d -> n%d;", i, i, (i+1)%50)
	}

	src.WriteString(" }")

	for name, engine := range map[string]graphviz.RenderEngine{
		"a callback that returns an error":             &refusingEngine{new(graphviz.DefaultRenderEngine)},
		"a callback that panics":                       &panickingEngine{new(graphviz.DefaultRenderEngine)},
		"a callback that panics, its arguments copied": &cornerPanickingEngine{new(graphviz.DefaultRenderEngine)},
	} {
		render, err := graphviz.NewRenderPlugin(ctx, "fails", engine)
		if err != nil {
			t.Fatal(err)
		}

		device, err := graphviz.NewDevicePlugin(ctx, "fails:fails")
		if err != nil {
			t.Fatal(err)
		}

		g, err := graphviz.NewWithPlugins(ctx, render, device)
		if err != nil {
			t.Fatal(err)
		}

		growth := memoryGrowth(200, func() {
			graph, err := graphviz.ParseBytes([]byte(src.String()))
			if err != nil {
				t.Fatal(err)
			}

			var out bytes.Buffer
			if err := g.Render(ctx, graph, "fails", &out); err == nil {
				t.Fatal("the render returned no error: the engine's callback did not fail")
			}

			closeOrError(t, graph.Close)
		})

		if growth != 0 {
			t.Errorf("%s: memory grew %d KiB over 200 renders", name, growth/1024)
		}

		closeOrError(t, g.Close)
	}
}
