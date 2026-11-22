package graphviz_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/cgraph"
	"github.com/forkcloser/go-graphviz/gvc"
)

// reportedGraphs are graphs Graphviz reports an error for and draws all the
// same, each with what it reports: dot 16.1.1 prints the error, writes its
// output and exits 1 for every one of them.
func reportedGraphs() map[string][2]string {
	return map[string][2]string{
		"a record label": {
			`digraph { a [shape=record, label="{a|"] }`,
			"bad label format {a|",
		},
		"a node's HTML label": {
			`digraph { a [label=<<b>x>] }`,
			"in label of node a",
		},
		"an edge's HTML label": {
			`digraph { a -> b [label=<<table><tr><td>x</td></tr>>] }`,
			"in label of edge a -> b",
		},
	}
}

// A render Graphviz reports an error during is written whole and returns
// that error, as dot writes its output and exits 1: it returned nil, and the
// message waited in the module for the next failure to carry it.
func TestRenderReturnsWhatGraphvizReported(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	for name, row := range reportedGraphs() {
		graph, err := graphviz.ParseBytes([]byte(row[0]))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		var out bytes.Buffer

		err = g.Render(ctx, graph, graphviz.SVG, &out)

		closeOrError(t, graph.Close)

		if !errors.Is(err, gvc.ErrReported) || !errors.Is(err, cgraph.ErrGraphviz) {
			t.Errorf("%s: Render returned %v, want an error that is gvc.ErrReported and cgraph.ErrGraphviz", name, err)

			continue
		}

		if !strings.Contains(err.Error(), row[1]) {
			t.Errorf("%s: the error is %q, want what Graphviz reported, %q", name, err, row[1])
		}

		if !strings.HasSuffix(strings.TrimSpace(out.String()), "</svg>") {
			t.Errorf("%s: the render is not a whole SVG: %d bytes", name, out.Len())
		}

		// The message was this render's: the next one has none.
		clean, err := graphviz.ParseBytes([]byte(`digraph { a -> b }`))
		if err != nil {
			t.Fatalf("%s: a parse after it: %v", name, err)
		}

		out.Reset()

		if err := g.Render(ctx, clean, graphviz.SVG, &out); err != nil {
			t.Errorf("%s: the render after it returned %v", name, err)
		}

		closeOrError(t, clean.Close)
	}
}

// RenderImage returns its image and RenderFilename writes its file when
// Graphviz reported an error on the way, each with that error.
func TestRenderImageAndFilenameOfAReportedError(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a [shape=record, label="{a|"] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	img, err := g.RenderImage(ctx, graph)
	if !errors.Is(err, gvc.ErrReported) {
		t.Errorf("RenderImage returned %v, want gvc.ErrReported", err)
	}

	if img == nil || img.Bounds().Empty() {
		t.Errorf("RenderImage returned no image with %v", err)
	}

	for _, format := range []graphviz.Format{graphviz.SVG, graphviz.PNG} {
		path := filepath.Join(t.TempDir(), "out")

		if err := g.RenderFilename(ctx, graph, format, path); !errors.Is(err, gvc.ErrReported) {
			t.Errorf("RenderFilename as %s returned %v, want gvc.ErrReported", format, err)
		}

		if written, err := os.ReadFile(path); err != nil || len(written) == 0 {
			t.Errorf("RenderFilename as %s wrote %d bytes, %v", format, len(written), err)
		}
	}
}

// A layout that reports an error leaves its layout on the graph: gvc's
// Layout returns ErrReported, and the graph renders and is freed as after
// a layout that returned nil.
func TestLayoutThatReportsIsLaidOut(t *testing.T) {
	ctx := t.Context()

	c, err := gvc.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, c.Close) })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a [shape=record, label="{a|"] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	if err := c.Layout(ctx, graph, "dot"); !errors.Is(err, gvc.ErrReported) {
		t.Fatalf("Layout returned %v, want gvc.ErrReported", err)
	}

	var out bytes.Buffer

	if err := c.RenderData(ctx, graph, "svg", &out); err != nil {
		t.Errorf("RenderData after it returned %v: the error was the layout's", err)
	}

	if !strings.HasSuffix(strings.TrimSpace(out.String()), "</svg>") {
		t.Errorf("the render is not a whole SVG: %d bytes", out.Len())
	}

	if err := c.FreeLayout(ctx, graph); err != nil {
		t.Errorf("FreeLayout: %v", err)
	}
}

// FreeLayout frees when its context is done: the layouts of two thousand
// graphs, each freed under a context already cancelled, leave nothing in
// the module. Unfreed, they are some twenty megabytes.
func TestFreeLayoutRunsWhenItsContextIsDone(t *testing.T) {
	ctx := t.Context()

	c, err := gvc.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, c.Close) })

	done, cancel := context.WithCancel(ctx)
	cancel()

	growth := memoryGrowth(2000, func() {
		graph, err := graphviz.ParseBytes([]byte(`digraph { a -> b -> c }`))
		if err != nil {
			t.Fatal(err)
		}

		if err := c.Layout(ctx, graph, "dot"); err != nil {
			t.Fatal(err)
		}

		if err := c.FreeLayout(done, graph); err != nil {
			t.Fatalf("FreeLayout under a context that is done: %v", err)
		}

		closeOrError(t, graph.Close)
	})

	if growth != 0 {
		t.Errorf("memory grew %d KiB over 2000 layouts freed under a context that is done", growth/1024)
	}
}

// A render that fails after its layout reported an error is a failure: its
// error tells what was reported and is no gvc.ErrReported, which says the
// output is there.
func TestFailureAfterAReportedLayout(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a [shape=record, label="{a|"] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var out bytes.Buffer

	err = g.Render(ctx, graph, "nosuchformat", &out)
	if err == nil || errors.Is(err, gvc.ErrReported) {
		t.Fatalf("a render in a format there is not returned %v", err)
	}

	if !strings.Contains(err.Error(), "bad label format") {
		t.Errorf("the failure does not tell what the layout reported: %v", err)
	}

	if out.Len() != 0 {
		t.Errorf("a failed render wrote %d bytes", out.Len())
	}
}

// What Graphviz warns of is no error: the render returns nil.
func TestRenderWithAWarningReturnsNil(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(`digraph { a [shape=nosuchshape] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var out bytes.Buffer

	if err := g.Render(ctx, graph, graphviz.SVG, &out); err != nil {
		t.Errorf("a render Graphviz warned during returned %v", err)
	}
}

// sfdpFirstEnv marks the child process TestFirstSFDPRenderReportsNothing
// starts: Graphviz reported its missing triangulation library once in a
// process, at the first sfdp layout, so only a fresh process shows it.
const sfdpFirstEnv = "GO_GRAPHVIZ_TEST_SFDP_FIRST"

// The first sfdp render of a process returns nil: built without a
// triangulation library, Graphviz reported an error for it, though sfdp
// asks nothing of the library in such a build.
func TestFirstSFDPRenderReportsNothing(t *testing.T) {
	t.Parallel()

	if os.Getenv(sfdpFirstEnv) != "1" {
		cmd := child(t, os.Args[0], "-test.run=^TestFirstSFDPRenderReportsNothing$", "-test.v")

		cmd.Env = append(os.Environ(), sfdpFirstEnv+"=1")

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the fresh process: %v\n%s", err, out)
		}

		return
	}

	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	g.SetLayout(graphviz.SFDP)

	for _, src := range []string{`graph { a }`, `graph { a -- b -- c -- a; c -- d }`} {
		graph, err := graphviz.ParseBytes([]byte(src))
		if err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer

		if err := g.Render(ctx, graph, graphviz.SVG, &out); err != nil {
			t.Errorf("sfdp of %s returned %v", src, err)
		}

		if out.Len() == 0 {
			t.Errorf("sfdp of %s rendered nothing", src)
		}

		closeOrError(t, graph.Close)
	}
}
