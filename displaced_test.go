package graphviz_test

import (
	"fmt"
	"image"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// An edge is drawn where it lies however far outside the page it starts:
// one straight edge seen through a viewport is the same page whether its
// ends are 2 inches from the page, 100, or 1000. The rasterizer steps along
// an edge a row at a time from its start, rounding each step, so an edge
// begun 100 inches above the page arrived 11 pixels off, and one begun 1000
// inches above did not arrive.
func TestEdgeFromFarOutsideThePage(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	g.SetLayout(graphviz.NEATO)

	// The line x = 1.5in - 0.015 y, its ends away inches above and below a
	// viewport centred on a node of its own.
	page := func(away float64) image.Image {
		graph, err := graphviz.ParseBytes(fmt.Appendf(nil, `digraph {
			viewport="200,200,1,'c'"; node [shape=point]; edge [arrowhead=none, penwidth=2]
			c [pos="2.2,0!" style=invis]; a [pos="%g,%g!"]; b [pos="%g,%g!"]; a -> b }`,
			1.5-away*0.015, away, 1.5+away*0.015, -away))
		if err != nil {
			t.Fatal(err)
		}

		defer closeOrError(t, graph.Close)

		drawn, err := g.RenderImage(ctx, graph)
		if err != nil {
			t.Fatal(err)
		}

		return drawn
	}

	// inked is how many pixels of img are not the page's white, and
	// apart how many differ from other's by more than a level of 255 in
	// red.
	compare := func(img, other image.Image) (inked, apart int) {
		bounds := img.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				red, _, _, _ := img.At(x, y).RGBA()
				otherRed, _, _, _ := other.At(x, y).RGBA()

				if red < 0x8000 {
					inked++
				}

				if red > otherRed+0x101 || otherRed > red+0x101 {
					apart++
				}
			}
		}

		return inked, apart
	}

	near := page(2)

	if inked, _ := compare(near, near); inked < 267 {
		t.Fatalf("the edge two inches away inks %d pixels of its page, not a line across it", inked)
	}

	for _, away := range []float64{20, 100, 1000} {
		if _, apart := compare(page(away), near); apart != 0 {
			t.Errorf("ends %g inches away: %d pixels are not those of the edge drawn from 2 inches", away, apart)
		}
	}
}
