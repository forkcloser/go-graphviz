package graphviz_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/forkcloser/go-graphviz"
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
