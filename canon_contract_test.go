package graphviz_test

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
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

// systemDot is the system's dot of the graphviz release go-graphviz embeds:
// the canonical writer changes between releases, so another version is not
// an oracle and the test skips. The CI lanes carry no graphviz yet, so the
// check runs locally only until they install this release.
func systemDot(t *testing.T) string {
	t.Helper()

	dot, err := exec.LookPath("dot")
	if err != nil {
		t.Skip("no system dot")
	}

	version, err := exec.CommandContext(t.Context(), dot, "-V").CombinedOutput()
	if err != nil {
		t.Skipf("%s -V: %v", dot, err)
	}

	// The token after the banner, so 16.10 is not 16.1; a patch release
	// keeps the writer, so major.minor is the match.
	_, after, found := strings.Cut(string(version), "graphviz version ")
	token, _, _ := strings.Cut(after, " ")

	if want := embeddedGraphvizVersion(t); !found || majorMinor(token) != majorMinor(want) {
		t.Skipf("system dot is not graphviz %s: %s", want, strings.TrimSpace(string(version)))
	}

	return dot
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

// TestContractCanonMatchesTheSystemDot: the canonical form go-graphviz
// renders for a graph (-Tcanon: the parsed graph written back, every
// attribute in place, nothing laid out) is the system dot's canonical
// form of the same text, byte for byte after trailing whitespace: the two
// parsers read the same graph.
func TestContractCanonMatchesTheSystemDot(t *testing.T) {
	t.Parallel()

	dot := systemDot(t)
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Cleanup, not defer: the parallel subtests outlive this function's body.
	t.Cleanup(func() { _ = g.Close() })

	// The nop layout lays nothing out: Render then writes the parsed
	// graph as the system's -Tcanon does, no bounding box added.
	g.SetLayout(graphviz.NOP)

	// Held apart, with the owner: for these two the wasm build and the
	// system binary of the same graphviz version write subgraphs apart.
	// "cluster" writes an edge declared at the root inside the cluster that
	// holds both its ends in the system's form, at the root in ours; "group"
	// keeps the anonymous subgraph of `a -> { b c d } -> e` in ours and
	// drops it in the system's.
	held := map[string]bool{"cluster": true, "group": true}

	for name, src := range canonCorpus() {
		if held[name] {
			continue
		}

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

			cmd := exec.CommandContext(ctx, dot, "-Tcanon")
			cmd.Stdin = strings.NewReader(src)

			theirs, err := cmd.Output()
			if err != nil {
				t.Fatalf("%s -Tcanon: %v", dot, err)
			}

			// Even the nop layout stamps a bounding box; the system's -Tcanon
			// writes none. The box is not the graph.
			if a, b := withoutBoundingBox(ours.String()), withoutBoundingBox(string(theirs)); a != b {
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
