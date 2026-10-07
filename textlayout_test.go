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

// A font a FontLoader supplies measures the label as well as drawing it:
// the box fits the label in that font. The loader used to be asked only
// when drawing, so with one set Graphviz estimated every label.
func TestTextLayoutMeasuresWithFontLoader(t *testing.T) {
	_, measured := boldFont(t)

	const name = "Supplied Face"

	dot := `digraph { node [shape=box margin=0 width=0 height=0]; a [label="` + measuredLabel +
		`" fontname="` + name + `" fontsize=` + strconv.Itoa(measuredSize) + `] }`

	without := nodeWidth(t, dot)

	parsed, err := opentype.Parse(gobold.TTF)
	if err != nil {
		t.Fatal(err)
	}

	graphviz.SetFontLoader(func(_ context.Context, textFont *graphviz.TextFont) (*opentype.Font, error) {
		if textFont.Name() == name { //nolint:contextcheck // a TextFont's getters read memory and take no context
			return parsed, nil
		}

		return nil, nil //nolint:nilnil // no font asks for the usual resolution
	})
	t.Cleanup(func() { graphviz.SetFontLoader(nil) })

	if got := nodeWidth(t, dot); math.Abs(got-measured) > 1 {
		t.Errorf("box %.1f points wide, want the loaded font's %.1f", got, measured)
	}

	if math.Abs(without-measured) <= 1 {
		t.Errorf("without the loader the box is %.1f points wide too: the font name resolves to the same font", without)
	}
}
