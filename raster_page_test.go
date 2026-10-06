package graphviz_test

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// isDark is a pixel of a black line or glyph on the white page, its
// anti-aliased fringe excluded.
func isDark(c color.NRGBA) bool {
	return c.A == 255 && int(c.R)+int(c.G)+int(c.B) < 3*128
}

// darkMask marks the dark pixels of img.
func darkMask(img image.Image) [][]bool {
	bounds := img.Bounds()
	mask := make([][]bool, bounds.Dy())

	for y := range mask {
		mask[y] = make([]bool, bounds.Dx())
		for x := range mask[y] {
			c, ok := color.NRGBAModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.NRGBA)
			mask[y][x] = ok && isDark(c)
		}
	}

	return mask
}

// covered is the share of the dark pixels of a that have a dark pixel of b
// within reach pixels.
func covered(a, b [][]bool, reach int) float64 {
	total, near := 0, 0

	for y := range a {
		for x := range a[y] {
			if !a[y][x] {
				continue
			}

			total++

			if anyDarkNear(b, x, y, reach) {
				near++
			}
		}
	}

	if total == 0 {
		return 0
	}

	return float64(near) / float64(total)
}

func anyDarkNear(mask [][]bool, x, y, reach int) bool {
	for dy := -reach; dy <= reach; dy++ {
		for dx := -reach; dx <= reach; dx++ {
			if yy, xx := y+dy, x+dx; yy >= 0 && yy < len(mask) && xx >= 0 && xx < len(mask[yy]) && mask[yy][xx] {
				return true
			}
		}
	}

	return false
}

// turnCounterclockwise turns a mask a quarter turn counterclockwise: the
// top row becomes the left column.
func turnCounterclockwise(mask [][]bool) [][]bool {
	height, width := len(mask), len(mask[0])
	turned := make([][]bool, width)

	for y := range turned {
		turned[y] = make([]bool, height)
		for x := range turned[y] {
			turned[y][x] = mask[x][width-1-y]
		}
	}

	return turned
}

// A landscape page (rotate=90) is the portrait page turned a quarter turn
// counterclockwise, text included, as Graphviz's own renderers draw it.
// The raster renderer used to ignore the rotation and draw the whole graph
// off the canvas, leaving the page blank.
func TestRasterLandscape(t *testing.T) {
	const body = `a -> b -> c; a -> c; b [shape=box label="hello"]; c [label="world"]`

	portrait := rasterImage(t, "digraph {"+body+"}")
	landscape := rasterImage(t, "digraph { rotate=90; "+body+" }")

	if p, l := portrait.Bounds(), landscape.Bounds(); p.Dx() != l.Dy() || p.Dy() != l.Dx() {
		t.Fatalf("landscape page is %v, want the portrait %v with its sides swapped", l, p)
	}

	// The two pages place the drawing at different sub-pixel offsets, so
	// their anti-aliasing differs; every dark pixel of one is within two
	// pixels of a dark pixel of the other.
	const reach, wantShare = 2, 0.99

	want := turnCounterclockwise(darkMask(portrait))
	got := darkMask(landscape)

	if share := covered(want, got, reach); share < wantShare {
		t.Errorf("%.1f%% of the turned portrait drawn on the landscape page, want %.0f%%", 100*share, 100*wantShare)
	}

	if share := covered(got, want, reach); share < wantShare {
		t.Errorf("%.1f%% of the landscape page matches the turned portrait, want %.0f%%", 100*share, 100*wantShare)
	}
}

// strokeWidth is the width of the dark run where the middle row of img
// first meets a line: the left side of a box filling the page.
func strokeWidth(img image.Image) int {
	mask := darkMask(img)
	row := mask[len(mask)/2]
	width := 0

	for _, dark := range row {
		switch {
		case dark:
			width++
		case width > 0:
			return width
		}
	}

	return width
}

// A pen width is a length in points, like everything else on the page: it
// grows with the resolution and shrinks with a graph scaled down by size,
// as in Graphviz's own renderers. The raster renderer used to draw every
// line its pen width in pixels, bold on a reduced graph and thin at a high
// resolution.
func TestRasterStrokeScalesWithPage(t *testing.T) {
	const (
		box      = `a [shape=box label="" width=4 height=4 penwidth=8]`
		penWidth = 8
	)

	// At 72 dpi a point is a pixel; the other pages are measured against
	// this one.
	full := rasterImage(t, "digraph { dpi=72; "+box+" }")
	reduced := rasterImage(t, `digraph { dpi=72; size="1,1"; `+box+" }")

	for _, tc := range []struct {
		name string
		img  image.Image
		// scale is pixels per point: dpi/72, times the zoom size imposes,
		// which reduces the whole page by the same factor.
		scale float64
	}{
		{name: "72 dpi", img: full, scale: 1},
		{name: "144 dpi", img: rasterImage(t, "digraph { dpi=144; "+box+" }"), scale: 2},
		{name: "reduced", img: reduced, scale: float64(reduced.Bounds().Dx()) / float64(full.Bounds().Dx())},
	} {
		want := penWidth * tc.scale

		if got := strokeWidth(tc.img); math.Abs(float64(got)-want) > 1.5 {
			t.Errorf("%s: box side %d pixels wide, want %.1f (pen width %d at %.2f pixels per point)",
				tc.name, got, want, penWidth, tc.scale)
		}
	}
}
