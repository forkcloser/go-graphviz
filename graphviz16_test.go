package graphviz_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/cgraph"
)

// Graphviz 16 renders an sfdp layout of a non-trivial graph many times in one
// runtime without corrupting its heap. Graphviz 15.1.1 shipped with a wrong
// index in SparseMatrix_decompose_to_supervariables that broke exactly this;
// 16.1.0 carries the fix, and this keeps the fork from regressing to a
// release that does not.
func TestSFDPRepeatedRender(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	g.SetLayout(graphviz.SFDP)

	data, err := os.ReadFile(filepath.Join("testdata", "directed", "KW91.gv"))
	if err != nil {
		t.Fatal(err)
	}

	for i := range 30 {
		graph, err := graphviz.ParseBytes(data)
		if err != nil {
			t.Fatal(err)
		}

		var buf bytes.Buffer
		if err := g.Render(ctx, graph, graphviz.SVG, &buf); err != nil {
			graph.Close()
			t.Fatalf("iteration %d: %v", i, err)
		}

		if buf.Len() == 0 {
			graph.Close()
			t.Fatalf("iteration %d: empty render", i)
		}

		graph.Close()
	}
}

// Since Graphviz 13 a label set as plain text stays plain text, so markup
// given to SetLabel is printed quoted and escaped, and only SetLabelHTML
// produces an HTML-like label (the <...> form in DOT output).
func TestHTMLLabel(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	graph, err := g.Graph()
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	const markup = "<b>bold</b>"

	plain, err := graph.CreateNodeByName("plain")
	if err != nil {
		t.Fatal(err)
	}

	plain.SetLabel(markup)

	html, err := graph.CreateNodeByName("html")
	if err != nil {
		t.Fatal(err)
	}

	html.SetLabelHTML(markup)

	var buf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.XDOT, &buf); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, `label="<b>bold</b>"`) {
		t.Errorf("SetLabel did not keep the markup as a quoted string:\n%s", out)
	}

	if !strings.Contains(out, `label=<<b>bold</b>>`) {
		t.Errorf("SetLabelHTML did not produce an HTML-like label:\n%s", out)
	}
}

// Canon and CanonStr lost their C counterparts in Graphviz 13 and 14 and are
// now built on agstrcanon; they still quote what DOT needs quoted and leave
// an identifier alone.
func TestCanon(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"plain", "plain"},
		{"two words", `"two words"`},
		{`say "hi"`, `"say \"hi\""`},
	} {
		got, err := cgraph.CanonStr(tc.in)
		if err != nil {
			t.Fatal(err)
		}

		if got != tc.want {
			t.Errorf("CanonStr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	got, err := cgraph.Canon("<b>x</b>", 1)
	if err != nil {
		t.Fatal(err)
	}

	if got != "<<b>x</b>>" {
		t.Errorf("Canon(html) = %q", got)
	}
}
