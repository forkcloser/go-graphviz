package graphviz_test

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/forkcloser/go-graphviz"
)

// svgText is one text element of an SVG rendering, in the SVG's points:
// where its baseline starts, how it is anchored there, and its size.
type svgText struct {
	x, y    float64
	anchor  string
	size    float64
	content string
}

// svgPage is what the cross-check needs from an SVG rendering: the page
// size in points, the translation the graph group applies, and the texts.
type svgPage struct {
	svgTransform

	width, height float64
	texts         []svgText
}

// svgTransform is the graph group's transform: a point p of the graph lands
// at scale × (p + (tx, ty)) on the page, turned by -90 degrees first when
// the page is rotated (a landscape page).
type svgTransform struct {
	scale   float64
	tx, ty  float64
	rotated bool
}

// parseSVG reads an SVG rendering written by Graphviz.
func parseSVG(data []byte) (svgPage, error) {
	var page svgPage

	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	var current *svgText

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return page, nil
		}

		if err != nil {
			return page, err
		}

		switch element := token.(type) {
		case xml.StartElement:
			attrs := map[string]string{}
			for _, attr := range element.Attr {
				attrs[attr.Name.Local] = attr.Value
			}

			switch element.Name.Local {
			case "svg":
				page.width = points(attrs["width"])
				page.height = points(attrs["height"])
			case "g":
				if attrs["id"] == "graph0" {
					page.svgTransform = parseTransform(attrs["transform"])
				}
			case "text":
				current = &svgText{
					x:      number(attrs["x"]),
					y:      number(attrs["y"]),
					anchor: attrs["text-anchor"],
					size:   number(attrs["font-size"]),
				}
			}
		case xml.CharData:
			if current != nil {
				current.content += string(element)
			}
		case xml.EndElement:
			if element.Name.Local == "text" && current != nil {
				if strings.TrimSpace(current.content) != "" {
					page.texts = append(page.texts, *current)
				}

				current = nil
			}
		}
	}
}

// parseTransform reads "scale(0.5 0.5) rotate(0) translate(4 405.01)".
func parseTransform(transform string) svgTransform {
	parsed := svgTransform{scale: 1}

	if _, rest, ok := strings.Cut(transform, "scale("); ok {
		if fields := strings.Fields(rest); len(fields) > 0 {
			parsed.scale = number(fields[0])
		}
	}

	if _, rest, ok := strings.Cut(transform, "rotate("); ok {
		parsed.rotated = !strings.HasPrefix(rest, "0)")
	}

	if _, rest, ok := strings.Cut(transform, "translate("); ok {
		if fields := strings.Fields(strings.TrimSuffix(rest, ")")); len(fields) == 2 {
			parsed.tx, parsed.ty = number(fields[0]), number(strings.TrimSuffix(fields[1], ")"))
		}
	}

	return parsed
}

// toPixel takes a point of the graph group to a pixel of img, a rendering
// of the whole page.
func (page svgPage) toPixel(img image.Rectangle, x, y float64) (px, py float64) {
	u, v := x+page.tx, y+page.ty
	if page.rotated {
		// rotate(-90) takes (u, v) to (v, -u).
		u, v = v, -u
	}

	return page.scale * u * float64(img.Dx()) / page.width,
		page.scale * v * float64(img.Dy()) / page.height
}

func points(value string) float64 { return number(strings.TrimSuffix(value, "pt")) }

func number(value string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}

	return n
}

// inkBand counts, in the band where the glyphs of text must lie (from a
// little above its baseline to most of a font size above it, around its
// anchor), the pixels where withText differs from withoutText: the same
// graph rendered with every font transparent. Only the text itself makes
// that difference; outlines, edges and fills are in both renders.
func inkBand(withText, withoutText image.Image, page svgPage, text svgText) (ink, area int) {
	bounds := withText.Bounds()

	// A rough width for the run of glyphs; only its middle is searched, so
	// a font wider or narrower than Graphviz's estimate still lands in it.
	width := 0.5 * text.size * float64(len([]rune(text.content)))
	centre := text.x

	switch text.anchor {
	case "start":
		centre += width / 2
	case "end":
		centre -= width / 2
	}

	half := max(0.15*text.size, 0.3*width)
	x0, y0 := page.toPixel(bounds, centre-half, text.y-0.75*text.size)
	x1, y1 := page.toPixel(bounds, centre+half, text.y-0.05*text.size)
	left, right := int(min(x0, x1)), int(max(x0, x1))
	top, bottom := int(min(y0, y1)), int(max(y0, y1))

	for y := max(top, bounds.Min.Y); y <= min(bottom, bounds.Max.Y-1); y++ {
		for x := max(left, bounds.Min.X); x <= min(right, bounds.Max.X-1); x++ {
			area++

			a, okA := color.NRGBAModel.Convert(withText.At(x, y)).(color.NRGBA)
			b, okB := color.NRGBAModel.Convert(withoutText.At(x, y)).(color.NRGBA)

			if okA && okB && distance(a, b) > inkContrast {
				ink++
			}
		}
	}

	return ink, area
}

// inkEdges is where, in font sizes from the text's baseline (positive
// below), the text's ink starts at the top and ends at the bottom: the
// highest and lowest pixel rows, from a font size above the baseline to
// inkWindowBelow of one below, whose ink is at least a fifth of the
// densest row's, over the band's middle columns. Glyphs sit on their
// baseline and reach up to their x-height or cap height; descenders, and a
// neighbouring line's edges, are too sparse to count. ok is false when the
// window holds no ink.
func inkEdges(withText, withoutText image.Image, page svgPage, text svgText) (topEdge, bottomEdge float64, ok bool) {
	bounds := withText.Bounds()

	width := 0.5 * text.size * float64(len([]rune(text.content)))
	centre := text.x

	switch text.anchor {
	case "start":
		centre += width / 2
	case "end":
		centre -= width / 2
	}

	half := max(0.15*text.size, 0.3*width)
	x0, y0 := page.toPixel(bounds, centre-half, text.y-text.size)
	x1, y1 := page.toPixel(bounds, centre+half, text.y+inkWindowBelow*text.size)
	left, right := max(int(min(x0, x1)), bounds.Min.X), min(int(max(x0, x1)), bounds.Max.X-1)
	top, bottom := max(int(min(y0, y1)), bounds.Min.Y), min(int(max(y0, y1)), bounds.Max.Y-1)

	rows := make([]int, 0, bottom-top+1)
	densest := 0

	for y := top; y <= bottom; y++ {
		count := 0

		for x := left; x <= right; x++ {
			a, okA := color.NRGBAModel.Convert(withText.At(x, y)).(color.NRGBA)
			b, okB := color.NRGBAModel.Convert(withoutText.At(x, y)).(color.NRGBA)

			if okA && okB && distance(a, b) > inkContrast {
				count++
			}
		}

		rows = append(rows, count)
		densest = max(densest, count)
	}

	if densest == 0 {
		return 0, 0, false
	}

	_, baseline := page.toPixel(bounds, text.x, text.y)
	fontPixels := page.scale * float64(bounds.Dy()) / page.height * text.size
	first := slices.IndexFunc(rows, func(count int) bool { return count*inkRowShare >= densest })

	last := first

	for i, count := range slices.Backward(rows) {
		if count*inkRowShare >= densest {
			last = i

			break
		}
	}

	return (float64(top+first) - baseline) / fontPixels, (float64(top+last) + 1 - baseline) / fontPixels, true
}

// inkWindowBelow is how far below its baseline, in font sizes, a text's ink
// is looked for: past a descender, short of the next line of a label.
const inkWindowBelow = 0.3

// minInkBottom and maxInkBottom bound where a text's ink may end, and
// maxInkTop how low it may start, in font sizes from its baseline. Measured
// on the corpus when the check was written, drawn text ends from 0.28 above
// its baseline to 0.38 below (dense descenders: "pipe", "type") and starts
// no lower than 0.35 above it (lower case alone). With every line drawn
// half a font size too high, half too low or 0.3 too low, this check fails
// 52, 53 and 33 of the 54 graphs with text, where the band check alone
// fails 1, 36 and 1. The weak side is upward: where drawn text ends spreads
// over 0.66 of a font size, so a line drawn 0.3 too high passes in all but
// 4 graphs and 0.2 too high in all but 1, while 0.2 too low fails 28. The
// window's top is at a font size above the baseline, which taller
// glyphs and a previous line's descenders reach, so how high ink may start is
// not bounded: a line drawn high is caught by where it ends.
const (
	minInkBottom = -0.40
	maxInkBottom = 0.45
	maxInkTop    = -0.30
)

// reachesXHeight reports whether text has a letter or a digit, whose ink
// reaches the x-height that maxInkTop asks for; punctuation alone ("-", ".")
// does not.
func reachesXHeight(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0
}

// inkRowShare is the divisor of the densest row's ink a row needs to count
// as the text's own: a fifth.
const inkRowShare = 5

func distance(a, b color.NRGBA) int {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}

		return int(y - x)
	}

	return d(a.R, b.R) + d(a.G, b.G) + d(a.B, b.B)
}

// inkContrast is how far, summed over the three channels, a pixel of the
// render with text must be from the same pixel without it to count as ink:
// above rounding, low enough for dark text on a dark fill.
const inkContrast = 48

// renderTextless renders data with every font transparent, the twin the
// cross-check subtracts: the layout, fills and lines are the same, the text
// is gone.
func renderTextless(t *testing.T, g *graphviz.Graphviz, data []byte) image.Image {
	t.Helper()

	graph, err := graphviz.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	if hideErr := hideText(graph); hideErr != nil {
		t.Fatal(hideErr)
	}

	img, err := g.RenderImage(t.Context(), graph)
	if err != nil {
		t.Fatal(err)
	}

	return img
}

// transparent is a fully transparent colour, spelled in hex: the name
// "transparent" resolves within a node's colorscheme, where it is unknown
// and falls back to black.
const transparent = "#00000000"

// hideText makes the text of graph, its subgraphs, nodes and edges
// transparent, head and tail labels included.
func hideText(graph *graphviz.Graph) error {
	for _, name := range []string{"fontcolor", "labelfontcolor"} {
		if err := graph.SafeSet(name, transparent, transparent); err != nil {
			return err
		}
	}

	for sub, err := graph.FirstSubGraph(); sub != nil || err != nil; sub, err = sub.NextSubGraph() {
		if err != nil {
			return err
		}

		if err := hideText(sub); err != nil {
			return err
		}
	}

	for node, err := graph.FirstNode(); node != nil || err != nil; node, err = graph.NextNode(node) {
		if err != nil {
			return err
		}

		if err := node.SafeSet("fontcolor", transparent, transparent); err != nil {
			return err
		}

		for edge, err := graph.FirstOut(node); edge != nil || err != nil; edge, err = graph.NextOut(edge) {
			if err != nil {
				return err
			}

			for _, name := range []string{"fontcolor", "labelfontcolor"} {
				if err := edge.SafeSet(name, transparent, transparent); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// minInkShare is the least share of a text's band its glyphs must cover.
// Measured on the corpus when the check was written, correctly drawn text
// covers 8% of its band at the least (a label padded with spaces) and about
// half typically, while a line drawn out of place leaves only a stray pixel
// or two of a neighbour's descender, under 0.2%.
const minInkShare = 0.04

// minInkHeight is the smallest text, in pixels, the cross-check judges:
// below it a scaled-down label blends into its fill and the band holds no
// clear ink even where the glyphs are drawn.
const minInkHeight = 6

// knownGaps are texts the raster renderer does not draw today, by file and
// text, with why. They are logged, not failed, so the gaps stay in view; a
// gap that starts drawing passes like any other text.
func knownGaps() map[string]map[string]string {
	return map[string]map[string]string{
		"testdata/directed/japanese.gv": {
			"*": "no glyph fallback: neither the requested font nor Go Regular has CJK glyphs",
		},
		"testdata/directed/psfonttest.gv": {
			"Symbol":       "symbol-encoded font (macOS): x/image maps no ASCII to it",
			"ZapfDingbats": "symbol-encoded font (macOS): x/image maps no ASCII to it",
		},
	}
}

func knownGap(path, text string) (string, bool) {
	gaps := knownGaps()[filepath.ToSlash(path)]
	if reason, ok := gaps["*"]; ok {
		return reason, true
	}

	reason, ok := gaps[text]

	return reason, ok
}

// Every text Graphviz places in a rendering is drawn by the raster renderer
// where Graphviz put it. Graphviz's own SVG renderer gives each text's
// baseline from the same layout; the PNG must have ink in the band above
// that baseline where the glyphs lie, and that ink must end on the baseline.
// A text drawn nowhere leaves its band empty; a line drawn too high or too
// low ends away from its baseline, even where a neighbouring line's ink
// fills its band.
func TestRasterTextMatchesSVG(t *testing.T) {
	for _, dir := range testPaths {
		if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}

			t.Run(path, func(t *testing.T) { crossCheckText(t, path) })

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func crossCheckText(t *testing.T, path string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	graph, err := graphviz.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	var svg bytes.Buffer
	if renderErr := g.Render(t.Context(), graph, graphviz.SVG, &svg); renderErr != nil {
		t.Fatal(renderErr)
	}

	page, err := parseSVG(svg.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	img, err := g.RenderImage(t.Context(), graph)
	if err != nil {
		t.Fatal(err)
	}

	textless := renderTextless(t, g, data)

	pixelsPerPoint := page.scale * float64(img.Bounds().Dy()) / page.height

	var missing, misplaced []string

	small := 0

	for _, text := range page.texts {
		if text.size*pixelsPerPoint < minInkHeight {
			small++

			continue
		}

		if topEdge, bottomEdge, ok := inkEdges(img, textless, page, text); ok && !page.rotated &&
			((topEdge > maxInkTop && reachesXHeight(text.content)) ||
				bottomEdge < minInkBottom || bottomEdge > maxInkBottom) {
			if _, gap := knownGap(path, text.content); !gap {
				misplaced = append(misplaced, fmt.Sprintf(
					"%q at (%.0f, %.0f) size %.0f: ink from %.2f to %.2f font sizes off its baseline",
					text.content, text.x, text.y, text.size, topEdge, bottomEdge,
				))
			}
		}

		if ink, area := inkBand(img, textless, page, text); area == 0 || float64(ink) < minInkShare*float64(area) {
			if reason, ok := knownGap(path, text.content); ok {
				t.Logf("known gap, %q not drawn: %s", text.content, reason)

				continue
			}

			missing = append(missing, fmt.Sprintf(
				"%q at (%.0f, %.0f) size %.0f: %d of %d",
				text.content, text.x, text.y, text.size, ink, area,
			))
		}
	}

	if small > 0 {
		t.Logf("%d of %d texts under %d pixels high, too small to judge", small, len(page.texts), minInkHeight)
	}

	if len(missing) > 0 {
		t.Errorf("%d of %d texts have too little ink where Graphviz placed them:\n  %s",
			len(missing), len(page.texts), strings.Join(missing, "\n  "))
	}

	if len(misplaced) > 0 {
		t.Errorf("%d of %d texts are not drawn on their baseline:\n  %s",
			len(misplaced), len(page.texts), strings.Join(misplaced, "\n  "))
	}
}
