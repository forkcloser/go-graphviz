package graphviz_test

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/forkcloser/go-graphviz"
)

// rows is the first and last row holding a pixel pick accepts, -1 when none.
func rows(img image.Image, pick func(color.NRGBA) bool) (first, last int) {
	first, last = -1, -1
	bounds := img.Bounds()

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if c, ok := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); ok && pick(c) {
				if first < 0 {
					first = y
				}

				last = y

				break
			}
		}
	}

	return first, last
}

func isOpaqueBlue(c color.NRGBA) bool { return c.A == 255 && c.B > 200 && c.R < 80 && c.G < 80 }
func isOpaqueRed(c color.NRGBA) bool  { return c.A == 255 && c.R > 200 && c.G < 80 && c.B < 80 }

// A node's label is drawn inside its box, centred on it, one line or
// several, whichever way its font is found: by the platform's font name, as
// a TrueType file named by path, or not at all, which falls back to the
// embedded Go Regular. The raster renderer places each line's baseline where
// Graphviz's own renderers do, and sizes every face for the page's DPI.
func TestLabelSitsInsideItsBox(t *testing.T) {
	ttf := filepath.Join(t.TempDir(), "goregular.ttf")
	if err := os.WriteFile(ttf, goregular.TTF, 0o600); err != nil {
		t.Fatal(err)
	}

	fonts := map[string]string{
		"default":   "",
		"ttf file":  ttf,
		"not found": "NoSuchFontAnywhere",
	}

	for name, font := range fonts {
		for _, label := range []string{"HHHH", `HHHH\nHHHH\nHHHH`} {
			t.Run(name+"/"+label, func(t *testing.T) {
				checkLabelInBox(t, font, label)
			})
		}
	}
}

// checkLabelInBox renders a box node with label in font (the default font
// when empty) and checks the label's ink against the box's outline.
func checkLabelInBox(t *testing.T, font, label string) {
	t.Helper()

	attrs := `shape=box color=blue penwidth=2 fontcolor=red fontsize=20 label="` + label + `"`
	if font != "" {
		attrs += ` fontname="` + font + `"`
	}

	graph, err := graphviz.ParseBytes([]byte(`digraph { a [` + attrs + `] }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	img, err := g.RenderImage(t.Context(), graph)
	if err != nil {
		t.Fatal(err)
	}

	boxTop, boxBottom := rows(img, isOpaqueBlue)
	textTop, textBottom := rows(img, isOpaqueRed)

	if boxTop < 0 || textTop < 0 {
		t.Fatalf("no box (%d) or no text (%d) drawn", boxTop, textTop)
	}

	if textTop <= boxTop || textBottom >= boxBottom {
		t.Fatalf("text rows %d to %d are not inside box rows %d to %d", textTop, textBottom, boxTop, boxBottom)
	}

	// Capitals have no descenders, so the ink is not centred on the line;
	// a quarter of the box's height bounds what the offset may be.
	boxMiddle, textMiddle := (boxTop+boxBottom)/2, (textTop+textBottom)/2
	if off := textMiddle - boxMiddle; off < -(boxBottom-boxTop)/4 || off > (boxBottom-boxTop)/4 {
		t.Fatalf("text centred %d rows from the box's centre (box %d to %d, text %d to %d)",
			off, boxTop, boxBottom, textTop, textBottom)
	}
}
