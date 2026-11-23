package graphviz_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/cgraph"
)

// nodesOf is the names of graph's nodes, in order, joined by commas.
func nodesOf(t *testing.T, graph *graphviz.Graph) string {
	t.Helper()

	var names []string

	node, err := graph.FirstNode()
	for node != nil && err == nil {
		var name string

		if name, err = node.Name(); err != nil {
			break
		}

		names = append(names, name)
		node, err = graph.NextNode(node)
	}

	if err != nil {
		t.Fatal(err)
	}

	return strings.Join(names, ",")
}

// parseAll is what ParseAll yields for text: the nodes of each graph, and
// the error that ended the sequence, if one did.
func parseAll(t *testing.T, text string) (graphs []string, failure error) {
	t.Helper()

	for graph, err := range graphviz.ParseAll([]byte(text)) {
		if err != nil {
			if failure != nil || graph != nil {
				t.Fatalf("a second error, or a graph with an error: %v, %v after %v", graph, err, failure)
			}

			failure = err

			continue
		}

		if failure != nil {
			t.Fatalf("a graph after the error %v", failure)
		}

		graphs = append(graphs, nodesOf(t, graph))
		closeOrError(t, graph.Close)
	}

	return graphs, failure
}

// ParseAll yields every graph of a text, in order, wherever each starts:
// on a line of its own, on the line the one before ends on, or against its
// closing brace. A brace inside a string, an HTML string or a comment ends
// no graph. What follows a graph on its line and begins no graph is dropped
// to the end of the line. A part Graphviz cannot read ends the sequence
// with its error, after the graphs before it, and the line the error names
// is the text's.
//
// The count and the outcome are those of Graphviz's dot wherever no graph
// starts on the line another ends on, and those cases are held to it.
// Graphviz's dot loses such a graph, and counts an error's line from the
// first line of the graph it is in: those cases are held here alone.
func TestParseAllReadsEveryGraph(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		text     string
		graphs   []string
		failure  string // a part of the error, when the sequence ends on one
		asNative bool
	}{
		"one graph":                  {"digraph { a }\n", []string{"a"}, "", true},
		"one graph, no last newline": {"digraph { a }", []string{"a"}, "", true},
		"a graph a line":             {"digraph { a }\ndigraph { b }\ngraph { c }\n", []string{"a", "b", "c"}, "", true},
		"a blank line between":       {"digraph { a }\n\n\ndigraph { b }\n", []string{"a", "b"}, "", true},
		"a comment after the last":   {"digraph { a }\n// done\n/* really */\n", []string{"a"}, "", true},
		"no graph at all":            {"", nil, "", true},
		"comments alone":             {"/* nothing */\n// nothing\n", nil, "", true},
		"the first does not parse":   {"digraph { a -> }\ndigraph { b }\n", nil, "syntax error in line 1 near '}'", true},

		// What follows a graph on its line and begins no graph is dropped.
		"a semicolon after the brace":   {"digraph { a };", []string{"a"}, "", true},
		"a semicolon after each":        {"digraph { a };\ndigraph { b };\n", []string{"a", "b"}, "", true},
		"a word on the graph's line":    {"digraph { a } nonsense\ndigraph { b }\n", []string{"a", "b"}, "", true},
		"a brace too many, same line":   {"digraph { a } }\n", []string{"a"}, "", true},
		"a brace too many, then a line": {"digraph { a } }\ndigraph { b }\n", []string{"a", "b"}, "", true},
		"a line comment on its line":    {"digraph { a } // digraph { x }\ndigraph { b }\n", []string{"a", "b"}, "", true},
		"a hash comment on its line":    {"digraph { a } # digraph { x }\ndigraph { b }\n", []string{"a", "b"}, "", true},
		"a quote opened on its line":    {"digraph { a } \" digraph { x }\ndigraph { b }\n", []string{"a", "b"}, "", true},

		// A semicolon after a graph's brace is dropped, and a graph after
		// it on the line is read.
		"a semicolon, then a graph":        {"digraph { a }; digraph { b }\n", []string{"a", "b"}, "", false},
		"two semicolons, then a graph":     {"digraph { a };; digraph { b }\n", []string{"a", "b"}, "", false},
		"a semicolon after each on a line": {"digraph { a }; digraph { b };\n", []string{"a", "b"}, "", false},
		"semicolons, blanks and a comment": {"digraph { a } ; /* then */ ;\t; digraph { b }", []string{"a", "b"}, "", false},
		"semicolons, then a word":          {"digraph { a }; ; nonsense\ndigraph { b }\n", []string{"a", "b"}, "", true},

		// The rest of a graph's line is read as the language has it where
		// it opens a comment or begins as a graph does. Graphviz's dot
		// drops the line in each of these: the difference is meant.
		"a comment opened on its line, never closed": {"digraph { a } /* open\ndigraph { b }\n", []string{"a"}, "", false},
		"a comment that runs to the next line":       {"digraph { a } /* x\ny */ digraph { b }", []string{"a", "b"}, "", false},
		"strict alone on its line":                   {"digraph { a } strict\ndigraph { b }\n", []string{"a", "b"}, "", false},
		"graph and no body on its line":              {"digraph { a } graph [x=y]\ndigraph { b }\n", []string{"a"}, "syntax error in line 1 near '['", false},

		// A graph that starts on the line another ends on.
		"two on one line":               {"digraph { a } digraph { b }\n", []string{"a", "b"}, "", false},
		"three, two of them on a line":  {"digraph { a } digraph { b }\ndigraph { c }\n", []string{"a", "b", "c"}, "", false},
		"brace against brace":           {"digraph{a}digraph{b}graph{c}", []string{"a", "b", "c"}, "", false},
		"a comment between two":         {"digraph { a } /* then */ digraph { b }", []string{"a", "b"}, "", false},
		"the words in capitals":         {"digraph { a } DIGRAPH { b } Strict Graph { c }", []string{"a", "b", "c"}, "", false},
		"a word that only starts so":    {"digraph { a } graphic\ndigraph { b }\n", []string{"a", "b"}, "", false},
		"subgraphs end no graph":        {"digraph { subgraph { a } subgraph { b } } digraph { c }", []string{"a,b", "c"}, "", false},
		"a brace in a string":           {"digraph { a [label=\"}\"] } digraph { \"}\" -> b }", []string{"a", "},b"}, "", false},
		"an escaped quote, then braces": {"digraph { a [label=\"\\\"}}\\\\\"] } digraph { b }", []string{"a", "b"}, "", false},
		"a brace in an HTML string":     {"digraph { a [label=<<b>}</b>>] } digraph { b }", []string{"a", "b"}, "", false},
		"a brace in a comment":          {"digraph { a /* } */ } // }\n digraph { b }", []string{"a", "b"}, "", false},
		"a broken graph on the line":    {"digraph { a } digraph { b -> }\ndigraph { c }\n", []string{"a"}, "syntax error in line 1 near '}'", false},

		// An error after the first graph, and the line it names.
		"the second does not parse":       {"digraph { a }\ndigraph { b -> }\ndigraph { c }\n", []string{"a"}, "syntax error in line 2 near '}'", false},
		"a word after the graph":          {"digraph { a }\nnonsense\n", []string{"a"}, "syntax error in line 2 near 'nonsense'", false},
		"a brace too many, next line":     {"digraph { a }\n}\n", []string{"a"}, "syntax error in line 2 near '}'", false},
		"an error far down the text":      {"digraph {\n a\n b\n c\n}\n\ndigraph { x -> }\n", []string{"a,b,c"}, "syntax error in line 7 near '}'", false},
		"an error on a shared line":       {"digraph {\n a\n b\n c\n} digraph { x -> }\n", []string{"a,b,c"}, "syntax error in line 5 near '}'", false},
		"an error in the third":           {"digraph { a }\ndigraph { b }\n\ndigraph {\n x -> }\n", []string{"a", "b"}, "syntax error in line 5 near '}'", false},
		"an error after CRLF lines":       {"digraph { a }\r\n\r\ndigraph { x -> }\r\n", []string{"a"}, "syntax error in line 3 near '}'", false},
		"a line directive is one graph's": {"# 40\ndigraph { a }\ndigraph { x -> }\n", []string{"a"}, "syntax error in line 3 near '}'", false},

		"the text ends at a NUL":        {"digraph { a }\n\x00digraph { b }\n", []string{"a"}, "", false},
		"a comment never closed, after": {"digraph { a }\n/* open", []string{"a"}, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			graphs, failure := parseAll(t, tc.text)

			if strings.Join(graphs, " | ") != strings.Join(tc.graphs, " | ") {
				t.Errorf("the graphs' nodes are %q, want %q", graphs, tc.graphs)
			}

			switch {
			case tc.failure == "" && failure != nil:
				t.Errorf("the sequence ended on %v, want no error", failure)
			case tc.failure != "" && (!errors.Is(failure, cgraph.ErrGraphviz) || !strings.Contains(failure.Error(), tc.failure)):
				t.Errorf("the sequence ended on %v, want a Graphviz error with %q", failure, tc.failure)
			}

			if !tc.asNative {
				return
			}

			t.Run("as Graphviz's dot has it", func(t *testing.T) {
				cmd := child(t, nativeDot(t), "-Tcanon")
				cmd.Stdin = strings.NewReader(tc.text)

				out, err := cmd.Output()

				written := 0

				for line := range strings.SplitSeq(string(out), "\n") {
					if strings.HasPrefix(line, "digraph") || strings.HasPrefix(line, "graph") {
						written++
					}
				}

				if written != len(tc.graphs) || (err != nil) != (tc.failure != "") {
					t.Errorf("dot wrote %d graphs and returned %v, want %d graphs and a failure: %v",
						written, err, len(tc.graphs), tc.failure != "")
				}
			})
		})
	}
}

// What ParseAll yields first is what ParseBytes returns: the same graph,
// the same error, or nothing where ParseBytes says ErrNoGraph.
func TestParseAllBeginsAsParseBytes(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"digraph { a -> b }",
		"digraph { a } digraph { b }",
		"",
		" \n",
		"// nothing\n",
		"digraph { a -> ",
		"nothing",
		"digraph { a [label=\"never closed] }",
		"/* never closed",
		"graph { a -- b } nonsense",
	} {
		one, oneErr := graphviz.ParseBytes([]byte(text))

		graphs, failure := parseAll(t, text)

		switch {
		case oneErr == nil:
			if len(graphs) == 0 || graphs[0] != nodesOf(t, one) {
				t.Errorf("%q: ParseBytes read the nodes %q, ParseAll %q first", text, nodesOf(t, one), graphs)
			}

			closeOrError(t, one.Close)
		case errors.Is(oneErr, cgraph.ErrNoGraph):
			if len(graphs) != 0 || failure != nil {
				t.Errorf("%q: ParseBytes found no graph, ParseAll yielded %q and %v", text, graphs, failure)
			}
		default:
			if len(graphs) != 0 || failure == nil || failure.Error() != oneErr.Error() {
				t.Errorf("%q: ParseBytes failed with %v, ParseAll yielded %q and %v", text, oneErr, graphs, failure)
			}
		}
	}
}

// A text that is all closing braces where none is a token, in a string, an
// HTML string or a comment, is read in the time ParseBytes reads it: a
// reader that stopped at each of them had the scanner go over the token
// again each time, the square of its length, with the module held (20 s
// for 80,000 braces in a label).
func TestParseAllIsNotSlowedByBraces(t *testing.T) {
	const braces = 200_000

	for name, text := range map[string]string{
		"in a label":         "digraph { a [label=\"" + strings.Repeat("}", braces) + "\"] }",
		"in an HTML label":   "digraph { a [label=<" + strings.Repeat("}", braces) + ">] }",
		"in a comment":       "digraph { a /*" + strings.Repeat("}", braces) + "*/ }",
		"in a line comment":  "digraph { a //" + strings.Repeat("}", braces) + "\n }",
		"after a hash":       "digraph { a #" + strings.Repeat("}", braces) + "\n }",
		"one every 8 bytes":  "digraph { a [label=\"" + strings.Repeat("abcdefg}", braces/8) + "\"] }",
		"escapes and braces": "digraph { a [label=\"" + strings.Repeat("\\\"}", braces/3) + "\"] }",
	} {
		start := time.Now()

		one, err := graphviz.ParseBytes([]byte(text))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		closeOrError(t, one.Close)

		alone := time.Since(start)
		start = time.Now()

		if graphs, failure := parseAll(t, text); len(graphs) != 1 || failure != nil {
			t.Fatalf("%s: ParseAll read %d graphs and %v, want one", name, len(graphs), failure)
		}

		// Twenty times, and a tenth of a second for a machine that is busy:
		// the square of the length was more than a thousand times.
		if all := time.Since(start); all > 20*alone+100*time.Millisecond {
			t.Errorf("%s: ParseAll took %v where ParseBytes took %v", name, all, alone)
		}
	}
}

// Reading every graph of a text leaves nothing in the module: the reader
// and its copy of the text are freed when the sequence ends, by its last
// graph, by an error, by a caller that stops before the end, or by a loop
// body that panics.
func TestParseAllLeavesNoMemory(t *testing.T) {
	text := []byte(strings.Repeat("digraph { a -> b } ", 3) + strings.Repeat("// padding\n", 200))
	failing := append([]byte("digraph { a } digraph { b -> }\n"), text...)

	for name, read := range map[string]func(){
		"to the end": func() {
			for graph, err := range graphviz.ParseAll(text) {
				if err != nil {
					t.Fatal(err)
				}

				closeOrError(t, graph.Close)
			}
		},
		"stopped at the first": func() {
			for graph, err := range graphviz.ParseAll(text) {
				if err != nil {
					t.Fatal(err)
				}

				closeOrError(t, graph.Close)

				break
			}
		},
		"ended by an error": func() {
			for graph, err := range graphviz.ParseAll(failing) {
				if err == nil {
					closeOrError(t, graph.Close)
				}
			}
		},
		"a loop body that panics": func() {
			defer func() { _ = recover() }()

			for graph, err := range graphviz.ParseAll(text) {
				if err != nil {
					t.Fatal(err)
				}

				closeOrError(t, graph.Close)

				panic("the caller's own")
			}
		},
	} {
		if growth := memoryGrowth(2000, read); growth != 0 {
			t.Errorf("%s: the module's memory grew by %d bytes over 2000 reads", name, growth)
		}
	}
}

// FuzzParseAll: whatever the bytes, ParseAll ends, having yielded no more
// graphs than the text has closing braces and at most one error, the last
// thing it yields; and it begins as ParseBytes does: the same first graph,
// the same error, or nothing where ParseBytes finds no graph.
func FuzzParseAll(f *testing.F) {
	for _, seed := range []string{
		"digraph { a }\ndigraph { b }\n",
		"digraph { a } digraph { b }\ndigraph { c }\n",
		"digraph{a}digraph{b}graph{c}",
		"digraph { a [label=\"}\"] } digraph { \"}\" -> b }",
		"digraph { a [label=<<b>}</b>>] } digraph { b }",
		"digraph { a /* } */ } // }\n digraph { b }",
		"digraph { a }\ndigraph { b -> }\ndigraph { c }\n",
		"digraph { a }\n# 7 \"elsewhere\"\ndigraph { b -> }\n",
		"digraph { a }\n\x00digraph { b }\n",
		"digraph { a } }\n",
		"digraph { a };\ndigraph { b };",
		"digraph { a }; digraph { b };; /* x */ ; graph { c }",
		"digraph { a } nonsense\ndigraph { b }",
		"digraph { a } /* x */ DIGRAPH { b } strict graph { c }",
		"digraph { a [label=\"}}}}\\\"}}\"] } digraph { b }",
		"",
		"/* open",
		"strict graph { a -- b } }}} {{{",
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, text []byte) {
		var (
			first   string
			graphs  int
			failure error
		)

		for graph, err := range graphviz.ParseAll(text) {
			if failure != nil {
				t.Fatalf("something after the error %v: %v, %v", failure, graph, err)
			}

			if err != nil {
				failure = err

				continue
			}

			if graphs == 0 {
				first = nodesOf(t, graph)
			}

			graphs++

			if err := graph.Close(); err != nil {
				t.Fatal(err)
			}
		}

		if most := strings.Count(string(text), "}"); graphs > most {
			t.Fatalf("%d graphs out of a text with %d closing braces: %q", graphs, most, truncate(text))
		}

		one, oneErr := graphviz.ParseBytes(text)

		switch {
		case oneErr == nil:
			if graphs == 0 || first != nodesOf(t, one) {
				t.Fatalf("ParseBytes read the nodes %q, ParseAll %d graphs, the first with %q: %q",
					nodesOf(t, one), graphs, first, truncate(text))
			}

			if err := one.Close(); err != nil {
				t.Fatal(err)
			}
		case errors.Is(oneErr, cgraph.ErrNoGraph):
			if graphs != 0 || failure != nil {
				t.Fatalf("ParseBytes found no graph, ParseAll %d and %v: %q", graphs, failure, truncate(text))
			}
		default:
			if graphs != 0 || failure == nil || failure.Error() != oneErr.Error() {
				t.Fatalf("ParseBytes failed with %v, ParseAll yielded %d graphs and %v: %q",
					oneErr, graphs, failure, truncate(text))
			}
		}
	})
}
