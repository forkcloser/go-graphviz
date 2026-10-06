package graphviz_test

import (
	"image"
	"image/color"
	"testing"

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
// several: the raster renderer places each line's baseline where Graphviz's
// own renderers do.
func TestLabelSitsInsideItsBox(t *testing.T) {
	for _, label := range []string{"HHHH", `HHHH\nHHHH\nHHHH`} {
		t.Run(label, func(t *testing.T) {
			graph, err := graphviz.ParseBytes([]byte(
				`digraph { a [shape=box color=blue penwidth=2 fontcolor=red fontsize=20 label="` + label + `"] }`,
			))
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

			// Capitals have no descenders, so the ink is not centred on the
			// line; the box's half height bounds what the offset may be.
			boxMiddle, textMiddle := (boxTop+boxBottom)/2, (textTop+textBottom)/2
			if off := textMiddle - boxMiddle; off < -(boxBottom-boxTop)/4 || off > (boxBottom-boxTop)/4 {
				t.Fatalf("text centred %d rows from the box's centre (box %d to %d, text %d to %d)",
					off, boxTop, boxBottom, textTop, textBottom)
			}
		})
	}
}
