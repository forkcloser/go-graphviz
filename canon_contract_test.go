package graphviz_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// canonCorpus is a set of DOT graphs with the features a parser has to
// carry into its canonical form: subgraphs and clusters, attributes on
// graph, node and edge, records, HTML labels, ports, quoting and escapes,
// strict graphs, chained edges, groups, undirected graphs.
func canonCorpus() map[string]string {
	return map[string]string{
		"chain":        "digraph G { a -> b -> c; }",
		"attributes":   "digraph { graph [rankdir=LR]; node [shape=box]; edge [color=red]; a [label=\"A\"]; a -> b [weight=2]; }",
		"cluster":      "digraph { subgraph cluster_0 { label=\"box\"; color=blue; d; e } d -> e; }",
		"record":       "digraph { node [shape=record]; s [label=\"<f0> left|<f1> mid|<f2> right\"]; s:f0 -> s:f2; }",
		"html":         "digraph { a [label=<<b>bold</b>>]; a -> a; }",
		"strict":       "strict digraph { a -> b; a -> b; b -> a; }",
		"quoting":      "digraph { \"quoted name\" -> \"other \\\"name\\\"\"; \"a\\nb\" -> c; }",
		"group":        "digraph { a -> { b c d } -> e; }",
		"undirected":   "graph { a -- b -- c; a -- c; }",
		"rank same":    "digraph { { rank=same; a; b } a -> c; b -> c; }",
		"empty":        "digraph G { }",
		"unicode":      "digraph { \"\u65e5\u672c\" -> \"é\"; }",
		"numbers":      "digraph { 1 -> 2; 2 -> 3.5; -1 -> .5; }",
		"semicolons":   "digraph { a; b; c; a -> b; }",
		"edge default": "digraph { edge [arrowhead=none]; a -> b; a -> c [arrowhead=normal]; }",
	}
}

// oracleRequired is what `just test` sets GO_GRAPHVIZ_ORACLE to once it has
// fetched the native dot pinned for this platform: from then on a missing
// oracle is a failure, not a skip.
const oracleRequired = "required"

// nativeDot is Graphviz's own dot of the release go-graphviz embeds: the
// canonical writer changes between releases, so another version is not an
// oracle.
//
// It is the one `just oracle` fetches into build/oracle, pinned in pins.yaml
// beside the release it is built from: there, a dot that is not that release
// fails the test, and so does a missing one when GO_GRAPHVIZ_ORACLE says it
// was fetched, which is how the CI lanes run. Without it, a dot on PATH of
// the same major.minor will do (a patch release keeps the writer), and the
// test skips when there is none.
func nativeDot(t *testing.T) string {
	t.Helper()

	want := embeddedGraphvizVersion(t)

	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}

	pinned := filepath.Join("build", "oracle", "graphviz-"+runtime.GOOS+"-"+runtime.GOARCH, "dot"+exe)
	if _, err := os.Stat(pinned); err == nil {
		if got := dotVersion(t, pinned); got != want {
			t.Fatalf(
				"%s is graphviz %q, pins.yaml builds %s: the dot pins move with the graphviz pin",
				pinned,
				got,
				want,
			)
		}

		return pinned
	}

	if os.Getenv("GO_GRAPHVIZ_ORACLE") == oracleRequired {
		t.Fatalf("no native dot at %s: `just oracle` fetches it", pinned)
	}

	dot, err := exec.LookPath("dot")
	if err != nil {
		t.Skip("no native dot: `just oracle` fetches the pinned one")
	}

	if got := dotVersion(t, dot); majorMinor(got) != majorMinor(want) {
		t.Skipf("%s is graphviz %q, not %s: `just oracle` fetches the pinned one", dot, got, want)
	}

	return dot
}

// dotVersion is the version `dot -V` prints after its banner, empty when it
// prints none: the whole token, so 16.10 is not 16.1.
func dotVersion(t *testing.T, dot string) string {
	t.Helper()

	banner, err := child(t, dot, "-V").CombinedOutput()
	if err != nil {
		t.Fatalf("%s -V: %v", dot, err)
	}

	_, after, _ := strings.Cut(string(banner), "graphviz version ")
	version, _, _ := strings.Cut(strings.TrimSpace(after), " ")

	return version
}

// embeddedGraphvizVersion is the graphviz release pins.yaml names for the
// wasm build, read from the file so the gate moves with the build instead
// of drifting into a silent skip.
func embeddedGraphvizVersion(t *testing.T) string {
	t.Helper()

	pins, err := os.ReadFile("pins.yaml")
	if err != nil {
		t.Fatal(err)
	}

	match := pinnedGraphviz.FindSubmatch(pins)
	if match == nil {
		t.Fatal("pins.yaml names no graphviz version")
	}

	return string(match[1])
}

var pinnedGraphviz = regexp.MustCompile(`(?s)- name: graphviz\n.*?\n\s+version: (\S+)`)

func majorMinor(version string) string {
	parts := strings.SplitN(version, ".", 3)

	return strings.Join(parts[:min(2, len(parts))], ".")
}

// TestContractCanonMatchesTheNativeDot: the canonical form go-graphviz
// renders for a graph laid out with dot (-Tcanon: the graph written back,
// every attribute in place, no position) is the canonical form `dot -Tcanon`
// writes for the same text, byte for byte once the bounding box and the
// whitespace are set aside: the two parsers read the same graph. One graph
// is held apart, for a difference between Graphviz's library and its
// command, and the test fails when that difference is gone.
func TestContractCanonMatchesTheNativeDot(t *testing.T) {
	t.Parallel()

	dot := nativeDot(t)
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Cleanup, not defer: the parallel subtests outlive this function's body.
	t.Cleanup(func() { _ = g.Close() })

	// The layout `dot -Tcanon` runs: it is not a no-op on the graph (it moves
	// an edge between two nodes of a cluster into the cluster), so another
	// layout here writes another canonical form.
	g.SetLayout(graphviz.DOT)

	// Held apart: the library's entry point, gvLayout, stamps the root with
	// its bounding box (lib/gvc/gvc.c:78, `agsafeset(g, "bb", buf, "")`),
	// which the command's gvLayoutJobs does not. The canonical writer drops
	// an anonymous subgraph only when none of its attributes differs from
	// its parent's (lib/cgraph/write.c:409, irrelevant_subgraph), and bb now
	// does: the `{ b c d }` of "group" is written by the library and dropped
	// by the command. The graph read is the same.
	held := map[string]bool{"group": true}

	for name, src := range canonCorpus() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			graph, err := graphviz.ParseBytes([]byte(src))
			if err != nil {
				t.Fatalf("ParseBytes: %v", err)
			}

			defer func() { _ = graph.Close() }() //nolint:contextcheck // Close takes no context; the graph is freed, not rendered

			var ours bytes.Buffer
			if err = g.Render(ctx, graph, graphviz.Format("canon"), &ours); err != nil {
				t.Fatalf("Render canon: %v", err)
			}

			//nolint:contextcheck // the child runs on its subtest's context, under a deadline
			cmd := child(t, dot, "-Tcanon")
			cmd.Stdin = strings.NewReader(src)

			theirs, err := cmd.Output()
			if err != nil {
				t.Fatalf("%s -Tcanon: %v", dot, err)
			}

			// The library stamps a bounding box; the command's -Tcanon writes
			// none. The box is not the graph.
			a, b := withoutBoundingBox(ours.String()), withoutBoundingBox(string(theirs))

			switch {
			case held[name] && a == b:
				t.Errorf("held apart, and the canonical forms are the same: it is held for nothing\n%s", a)
			case !held[name] && a != b:
				t.Errorf("canonical forms differ\nours:\n%s\ntheirs:\n%s", a, b)
			}
		})
	}
}

var (
	bbLine = regexp.MustCompile(`(?m)^\s*graph \[bb="[^"]*"\];\n`)
	bbAttr = regexp.MustCompile(`bb="[^"]*",\s*`)
	spaces = regexp.MustCompile(`\s+`)
)

// withoutBoundingBox drops the bb attribute and folds the whitespace of
// attribute lists, which the two writers break differently once an
// attribute is gone.
func withoutBoundingBox(canon string) string {
	canon = bbLine.ReplaceAllString(canon, "")
	canon = bbAttr.ReplaceAllString(canon, "")
	canon = spaces.ReplaceAllString(canon, " ")
	canon = strings.ReplaceAll(canon, "[ ", "[")
	canon = strings.ReplaceAll(canon, " ]", "]")

	return strings.TrimSpace(canon)
}
