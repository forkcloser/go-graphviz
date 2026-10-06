package graphviz_test

import (
	"bytes"
	"context"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"

	"github.com/forkcloser/go-graphviz"
)

// pointsAttr finds the first polygon of the first node of an SVG rendering.
var pointsAttr = regexp.MustCompile(`(?s)class="node".*?<polygon[^>]* points="([^"]+)"`)

// nodeWidth lays dot out and returns the width, in points, of its first
// node, a box.
func nodeWidth(t *testing.T, dot string) float64 {
	t.Helper()

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	var svg bytes.Buffer
	if err := g.Render(t.Context(), graph, graphviz.SVG, &svg); err != nil {
		t.Fatal(err)
	}

	match := pointsAttr.FindSubmatch(svg.Bytes())
	if match == nil {
		t.Fatalf("no node polygon in:\n%s", svg.String())
	}

	low, high := math.Inf(1), math.Inf(-1)

	for pair := range strings.FieldsSeq(string(match[1])) {
		x, _, _ := strings.Cut(pair, ",")

		value, err := strconv.ParseFloat(x, 64)
		if err != nil {
			t.Fatal(err)
		}

		low, high = min(low, value), max(high, value)
	}

	return high - low
}

const (
	measuredLabel = "Graphviz measures this"
	measuredSize  = 20
)

// boldFont writes Go Bold to a file, a font every platform can name by
// path, and returns the path and the label's width in it, in points.
func boldFont(t *testing.T) (string, float64) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "gobold.ttf")
	if err := os.WriteFile(path, gobold.TTF, 0o600); err != nil {
		t.Fatal(err)
	}

	const fixedOne = 64

	return path, float64(font.MeasureString(boldFace(t), measuredLabel)) / fixedOne
}

// boldFace is Go Bold at the label's size, measuring in points.
func boldFace(t *testing.T) font.Face {
	t.Helper()

	parsed, err := opentype.Parse(gobold.TTF)
	if err != nil {
		t.Fatal(err)
	}

	face, err := opentype.NewFace(parsed, &opentype.FaceOptions{Size: measuredSize, DPI: 72})
	if err != nil {
		t.Fatal(err)
	}

	return face
}

func boxDot(fontPath string) string {
	return `digraph { node [shape=box margin=0 width=0 height=0]; a [label="` + measuredLabel +
		`" fontname="` + fontPath + `" fontsize=` + strconv.Itoa(measuredSize) + `] }`
}

// A label is laid out at the width it is drawn with: Graphviz asks the
// text-layout plugin, which measures with the font the renderer draws in,
// instead of estimating from its tables for Times, Courier and Arial.
func TestTextLayoutMeasures(t *testing.T) {
	path, want := boldFont(t)

	if got := nodeWidth(t, boxDot(path)); math.Abs(got-want) > 1 {
		t.Errorf("box %.1f points wide, want the label's measured %.1f", got, want)
	}
}

// With a font loader set, the plugin declines, its false reaches Graphviz,
// and Graphviz estimates: a callback's result used to be left as the slot
// held, which read as true and left the label measured as nothing.
func TestTextLayoutDeclinesForFontLoader(t *testing.T) {
	path, measured := boldFont(t)

	face := boldFace(t)

	graphviz.SetFontLoader(func(context.Context, *graphviz.Job, *graphviz.TextFont) (font.Face, error) {
		return face, nil
	})
	t.Cleanup(func() { graphviz.SetFontLoader(nil) })

	got := nodeWidth(t, boxDot(path))
	if got < 1 {
		t.Fatalf("box %.1f points wide: the declined layout was taken as done", got)
	}

	if math.Abs(got-measured) <= 1 {
		t.Errorf("box %.1f points wide, the measured width: the plugin measured despite the loader", got)
	}
}
