package graphviz_test

import (
	"image"
	"image/color"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// rasterCounts renders dot to an image and counts the pixels pick accepts.
func rasterCounts(t *testing.T, dot string, pick func(color.NRGBA) bool) int {
	t.Helper()

	return countPixels(rasterImage(t, dot), pick)
}

// rasterImage renders dot with the module's raster renderer.
func rasterImage(t *testing.T, dot string) image.Image {
	t.Helper()

	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	graph, err := graphviz.ParseBytes([]byte(dot))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	img, err := g.RenderImage(ctx, graph)
	if err != nil {
		t.Fatal(err)
	}

	return img
}

func countPixels(img image.Image, pick func(color.NRGBA) bool) int {
	bounds := img.Bounds()
	count := 0

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if c, ok := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA); ok && pick(c) {
				count++
			}
		}
	}

	return count
}

func isRed(c color.NRGBA) bool  { return c.A == 255 && c.R > 200 && c.G < 60 && c.B < 60 }
func isBlue(c color.NRGBA) bool { return c.A == 255 && c.B > 200 && c.R < 60 && c.G < 60 }

// A filled shape is filled with its fill colour and outlined with its pen,
// as every Graphviz renderer draws it.
func TestRasterFilledShapeHasOutline(t *testing.T) {
	const dot = `digraph { a [shape=box style=filled fillcolor=red color=blue penwidth=6 label=""] }`

	if red := rasterCounts(t, dot, isRed); red == 0 {
		t.Error("no fill")
	}

	if blue := rasterCounts(t, dot, isBlue); blue == 0 {
		t.Error("no outline")
	}
}

// A colour's alpha is carried: a half-transparent edge leaves no opaque
// pixel of its colour.
func TestRasterAlpha(t *testing.T) {
	const dot = `digraph { bgcolor=transparent; a [style=invis]; b [style=invis]; a -> b [color="#00ff0080" penwidth=8 arrowhead=none] }`

	opaque := rasterCounts(t, dot, func(c color.NRGBA) bool { return c.G > 200 && c.A == 255 })
	if opaque != 0 {
		t.Errorf("%d opaque pixels on a half-transparent edge", opaque)
	}

	half := rasterCounts(t, dot, func(c color.NRGBA) bool { return c.G > 200 && c.A > 100 && c.A < 160 })
	if half == 0 {
		t.Error("the edge did not render at half alpha")
	}
}

// A shape whose pen is none is filled and not outlined. The label is set
// to the fill colour so the only non-red, non-white pixels an outline could
// leave are its own; the fill's edge blends red into the white page, which
// keeps R at 255.
func TestRasterPenNone(t *testing.T) {
	const dot = `digraph { a [shape=box style=filled fillcolor=red fontcolor=red color=transparent penwidth=6] }`

	if red := rasterCounts(t, dot, isRed); red == 0 {
		t.Error("no fill")
	}

	if outline := rasterCounts(t, dot, func(c color.NRGBA) bool { return c.A == 255 && c.R < 200 }); outline != 0 {
		t.Errorf("%d opaque pixels neither fill nor page around a shape with a transparent pen", outline)
	}
}
