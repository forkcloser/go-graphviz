package graphviz_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/cgraph"
)

// quotes is a string whose canonical form is as long as one gets: runs of
// quotes, two bytes each when written, and a letter after every run, where
// the DOT writer may break the line.
func quotes(size int) string {
	const run = 128

	return strings.Repeat(strings.Repeat(`"`, run-1)+"x", size/run)
}

// The DOT writer's canonical form of a string can be longer than twice the
// string, the room Graphviz gives it: a label of quotes was written with the
// heap's bytes in it at 8 KiB, and trapped the module at 2 MiB. The graph
// written is the graph read, by this parser and by Graphviz's own dot, and
// the module goes on working.
func TestAStringOfQuotesIsWrittenWhole(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = g.Close() }()

	for _, size := range []int{1 << 10, 8 << 10, 2 << 20} {
		t.Run(fmt.Sprintf("%d bytes", size), func(t *testing.T) {
			label := quotes(size)
			written := canonOf(t, g, `digraph { a [label="`+strings.ReplaceAll(label, `"`, `\"`)+`"]; a -> b }`)

			if got := labelOfA(t, written); got != label {
				t.Fatalf(
					"the label written and read back differs from the one given: %d bytes for %d, the same up to byte %d",
					len(got),
					len(label),
					commonPrefix(got, label),
				)
			}

			// Asked for JSON, which has its own writer: asked for canonical
			// DOT, Graphviz's dot overruns as the module did, and dies of it
			// at 2 MiB.
			t.Run("read by Graphviz's dot", func(t *testing.T) {
				cmd := child(t, nativeDot(t), "-Tjson0")
				cmd.Stdin = bytes.NewReader(written)

				theirs, err := cmd.Output()
				if err != nil {
					t.Fatalf("dot does not read what was written: %v", err)
				}

				var read dotJSON

				if err := json.Unmarshal(theirs, &read); err != nil {
					t.Fatalf("dot -Tjson0: %v", err)
				}

				for _, object := range read.Objects {
					if object.Name != "a" {
						continue
					}

					if object.Label != label {
						t.Errorf("dot read another label than the one given: %d bytes for %d, the same up to byte %d",
							len(object.Label), len(label), commonPrefix(object.Label, label))
					}

					return
				}

				t.Error("dot read no node a in what was written")
			})
		})
	}

	// A trap leaves every later call failing: this one is made after the
	// largest label was written.
	if got := labelOfA(t, canonOf(t, g, `digraph { a [label="after"] }`)); got != "after" {
		t.Errorf("after the labels of quotes a graph is written with label %q, want %q", got, "after")
	}
}

// What CanonStr returns is the string as DOT writes it: read as DOT, it is
// the string, the longest canonical forms included.
func TestCanonStrReadsBackAsTheString(t *testing.T) {
	for _, value := range []string{"plain", "two words", `say "hi"`, "node", "-1.5", quotes(1 << 10), quotes(8 << 10), quotes(2 << 20)} {
		canon, err := cgraph.CanonStr(value)
		if err != nil {
			t.Fatalf("CanonStr of %d bytes: %v", len(value), err)
		}

		if got := labelOfA(t, []byte(`digraph { a [label=`+canon+`] }`)); got != value {
			t.Errorf("CanonStr of %d bytes, read as DOT, is %d bytes, the same up to byte %d",
				len(value), len(got), commonPrefix(got, value))
		}
	}
}

// dotJSON is what the test reads of dot's JSON output: the objects, nodes
// among them, with their labels.
type dotJSON struct {
	Objects []dotJSONObject `json:"objects"`
}

type dotJSONObject struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// canonOf is the graph of src written back in canonical DOT.
func canonOf(t *testing.T, g *graphviz.Graphviz, src string) []byte {
	t.Helper()

	graph, err := graphviz.ParseBytes([]byte(src))
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}

	defer func() { _ = graph.Close() }()

	var out bytes.Buffer
	if err := g.Render(t.Context(), graph, graphviz.Format("canon"), &out); err != nil {
		t.Fatalf("Render canon: %v", err)
	}

	return out.Bytes()
}

// labelOfA is the label of node a in the DOT text given.
func labelOfA(t *testing.T, dot []byte) string {
	t.Helper()

	graph, err := graphviz.ParseBytes(dot)
	if err != nil {
		t.Fatalf("the DOT written does not parse: %v", err)
	}

	defer func() { _ = graph.Close() }()

	a, err := graph.NodeByName("a")
	if err != nil || a == nil {
		t.Fatalf("the DOT written has no node a: %v", err)
	}

	return a.GetStr("label")
}

func commonPrefix(a, b string) int {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}

	return n
}
