package graphviz_test

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// freshProcessEnv marks the child process TestFirstRenderIsEveryRender
// starts: in it, the test body runs; in the parent, the test runs the body
// in a child, since only a process that has not yet made a context can show
// what a graph read before any context looks like.
const freshProcessEnv = "GO_GRAPHVIZ_TEST_FRESH_PROCESS"

// TestFirstRenderIsEveryRender: a graph read before any context exists
// renders the same bytes as one read after. Graphviz's context constructor
// installs the node label default on the prototype graph, so a graph read
// before it lacked `node [label="\N"];` in its DOT, and the first render of
// a process differed from every later one; the engine installs the default
// at init instead.
func TestFirstRenderIsEveryRender(t *testing.T) {
	t.Parallel()

	if os.Getenv(freshProcessEnv) == "1" {
		firstRenderIsEveryRender(t)

		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestFirstRenderIsEveryRender$", "-test.v")

	cmd.Env = append(os.Environ(), freshProcessEnv+"=1")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the fresh process: %v\n%s", err, out)
	}
}

func firstRenderIsEveryRender(t *testing.T) {
	t.Helper()

	src := []byte("digraph { a [shape=record, label=\"{a|b}\"] }")

	// Read before any context exists: the case the defect was about.
	first, err := graphviz.ParseBytes(src)
	if err != nil {
		t.Fatalf("ParseBytes before any context: %v", err)
	}

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	var before bytes.Buffer
	if err = g.Render(t.Context(), first, graphviz.Format("dot"), &before); err != nil {
		t.Fatalf("Render the graph read before the context: %v", err)
	}

	closeOrError(t, first.Close)

	if !strings.Contains(before.String(), `node [label="\N"];`) {
		t.Errorf(
			"the graph read before any context renders without the node label default:\n%s",
			before.String(),
		)
	}

	for _, format := range []graphviz.Format{"dot", "xdot"} {
		var again, once bytes.Buffer

		render(t, g, src, format, &again)
		render(t, g, src, format, &once)

		if !bytes.Equal(again.Bytes(), once.Bytes()) {
			t.Errorf(
				"-T%s: two renders of the same input differ:\n%s\n---\n%s",
				format,
				again.String(),
				once.String(),
			)
		}

		if format == "dot" && !bytes.Equal(before.Bytes(), again.Bytes()) {
			t.Errorf(
				"-Tdot: the graph read before any context renders differently from one read after:\n%s\n---\n%s",
				before.String(),
				again.String(),
			)
		}
	}
}

func render(t *testing.T, g *graphviz.Graphviz, src []byte, format graphviz.Format, out *bytes.Buffer) {
	t.Helper()

	graph, err := graphviz.ParseBytes(src)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}

	defer closeOrError(t, graph.Close)

	if err = g.Render(t.Context(), graph, format, out); err != nil {
		t.Fatalf("Render %s: %v", format, err)
	}
}
