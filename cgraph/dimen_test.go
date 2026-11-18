package cgraph_test

import (
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// SetDimen sets dimen, the dimensions a layout is rendered in, and SetDim
// sets dim, those it is laid out in: SetDimen set dim.
func TestSetDimenSetsDimen(t *testing.T) {
	graph, err := graphviz.ParseBytes([]byte(`graph { a -- b }`))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = graph.Close() }()

	graph.SetDimen(3)

	if dimen, dim := graph.GetStr("dimen"), graph.GetStr("dim"); dimen != "3" || dim != "" {
		t.Errorf(`after SetDimen(3) dimen is %q and dim %q, want "3" and ""`, dimen, dim)
	}

	graph.SetDim(4)

	if dimen, dim := graph.GetStr("dimen"), graph.GetStr("dim"); dimen != "3" || dim != "4" {
		t.Errorf(`after SetDim(4) dimen is %q and dim %q, want "3" and "4"`, dimen, dim)
	}

	if err := graph.Err(); err != nil {
		t.Error(err)
	}
}
