package gvc

import (
	"image"
	"runtime"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// masked is a face that gives no outline: the canvas draws its glyphs as
// masks, whatever their size.
type masked struct{ font.Face }

// largeFace is the embedded regular font at ppem pixels per em.
func largeFace(t *testing.T, ppem float64) font.Face {
	t.Helper()

	regular, err := embeddedFont("", fontStyle{weight: weightRegular})
	if err != nil {
		t.Fatal(err)
	}

	face, err := regular.face(ppem, pointsPerInch)
	if err != nil {
		t.Fatal(err)
	}

	return face
}

// furthest is the largest difference between a channel of a pixel of one
// image and of the other, over the rectangle given in each.
func furthest(one *image.RGBA, in image.Rectangle, other *image.RGBA, at image.Point) int {
	worst := 0

	for y := range in.Dy() {
		for x := range in.Dx() {
			a := one.RGBAAt(in.Min.X+x, in.Min.Y+y)
			b := other.RGBAAt(at.X+x, at.Y+y)

			for _, pair := range [][2]uint8{{a.R, b.R}, {a.G, b.G}, {a.B, b.B}, {a.A, b.A}} {
				worst = max(worst, int(pair[0])-int(pair[1]), int(pair[1])-int(pair[0]))
			}
		}
	}

	return worst
}

// covered is how many pixels of the canvas are opaque.
func covered(c *canvas) int {
	count := 0

	for i := 3; i < len(c.img.Pix); i += 4 {
		if c.img.Pix[i] == 0xff {
			count++
		}
	}

	return count
}

// tolerance is how far two rasterizations of one edge may lie apart, in
// levels of 255. The rasterizer steps an edge's position row by row in
// float32, so over the thousand rows of these glyphs two walks that start
// on different rows, or flatten a curve differently, end up to a thirtieth
// of a pixel apart. A line misplaced by the clamp is whole pixels away.
const tolerance = 8

// A glyph too large for a mask is filled as its outline, and is the glyph
// its mask draws: letters of lines, of curves, and with counters. At 1200
// pixels per em the mask is past 512 pixels a side, where its rasterizer
// computes in floating point as the outline's does.
func TestLargeGlyphIsItsMask(t *testing.T) {
	face := largeFace(t, 1200)

	const side = 1400

	for _, char := range "WOg8&" {
		bounds, _, ok := face.GlyphBounds(char)
		if !ok {
			t.Fatalf("%q: no glyph", char)
		}

		if area := (bounds.Max.X - bounds.Min.X).Ceil() * (bounds.Max.Y - bounds.Min.Y).Ceil(); area <= maxGlyphMask {
			t.Fatalf("%q: %d pixels, not past the %d a mask is made for", char, area, maxGlyphMask)
		}

		outlined, mask := newCanvas(side, side), newCanvas(side, side)
		outlined.drawString(face, string(char), 100, 1000, opaqueBlack())
		mask.drawString(masked{face}, string(char), 100, 1000, opaqueBlack())

		if covered(mask) < side {
			t.Fatalf("%q: the mask covers %d pixels, not a letter", char, covered(mask))
		}

		// A curve is flattened here to within a tenth of a pixel, and by
		// the mask's rasterizer to within about as much: an edge of the
		// one lies up to a fifth of a pixel from the other's. A counter
		// filled, or a contour missing, is a whole pixel's coverage.
		if worst := furthest(outlined.img, outlined.img.Rect, mask.img, image.Point{}); worst > 255/5+tolerance {
			t.Errorf("%q: the outline is %d levels from the mask at its furthest pixel", char, worst)
		}
	}
}

// A glyph larger than the page is drawn as far as the page reaches, and
// what is drawn is that part of the whole glyph: across each edge and each
// corner, with the page inside the glyph, and with the glyph off the page.
// Every window is past 512 pixels one way, as the whole glyph is.
func TestLargeGlyphIsClippedToThePage(t *testing.T) {
	face := largeFace(t, 1200)

	const side = 1400

	for _, char := range "WOg" {
		whole := newCanvas(side, side)
		whole.drawString(face, string(char), 100.25, 1000.5, opaqueBlack())

		if covered(whole) < side {
			t.Fatalf("%q: the whole glyph covers %d pixels, not a letter", char, covered(whole))
		}

		for _, window := range []image.Rectangle{
			image.Rect(0, 0, 600, 600), image.Rect(800, 0, side, 600), // top corners
			image.Rect(0, 700, 600, side), image.Rect(800, 700, side, side), // bottom corners
			image.Rect(400, 0, 1000, 500), image.Rect(400, 800, 1000, side), // top and bottom edges
			image.Rect(0, 300, 520, 900), image.Rect(880, 300, side, 900), // left and right edges
			image.Rect(350, 400, 900, 700),    // inside the glyph's bounds
			image.Rect(450, 500, 1000, 520),   // a strip inside them
			image.Rect(0, side-40, 600, side), // under the glyph
		} {
			part := newCanvas(window.Dx(), window.Dy())
			part.translate(float64(-window.Min.X), float64(-window.Min.Y))
			part.drawString(face, string(char), 100.25, 1000.5, opaqueBlack())

			if worst := furthest(whole.img, window, part.img, image.Point{}); worst > tolerance {
				t.Errorf("%q through %v: %d levels from the whole glyph at its furthest pixel", char, window, worst)
			}
		}
	}
}

// A curve wholly outside the clamp is its chord, and one that crosses it
// is flattened: a circle of four cubics, larger than its window, covers the
// window as it covers that part of a canvas that holds it whole. Windows
// are past 512 pixels one way, as for the glyph.
func TestClampedPathCubics(t *testing.T) {
	const (
		side   = 1200
		centre = 600.0
		radius = 500.0
		ctrl   = kappa * radius
	)

	circle := func(path *clampedPath, shift point) {
		at := func(x, y float64) point { return point{x, y}.sub(shift) }

		path.moveTo(at(centre+radius, centre))
		path.cubicTo(at(centre+radius, centre+ctrl), at(centre+ctrl, centre+radius), at(centre, centre+radius), 0)
		path.cubicTo(at(centre-ctrl, centre+radius), at(centre-radius, centre+ctrl), at(centre-radius, centre), 0)
		path.cubicTo(at(centre-radius, centre-ctrl), at(centre-ctrl, centre-radius), at(centre, centre-radius), 0)
		path.cubicTo(at(centre+ctrl, centre-radius), at(centre+radius, centre-ctrl), at(centre+radius, centre), 0)
	}

	draw := func(window image.Rectangle) *canvas {
		c := newCanvas(window.Dx(), window.Dy())
		c.ras.Reset(window.Dx(), window.Dy())

		path := clampedPath{ras: &c.ras, max: point{float64(window.Dx()), float64(window.Dy())}}
		circle(&path, point{float64(window.Min.X), float64(window.Min.Y)})
		c.ras.Draw(c.img, c.img.Rect, image.NewUniform(opaqueBlack()), image.Point{})

		return c
	}

	whole := draw(image.Rect(0, 0, side, side))
	if alphaAt(whole, 600, 600) != 0xff || alphaAt(whole, 10, 10) != 0 {
		t.Fatal("the whole circle is not drawn")
	}

	for _, window := range []image.Rectangle{
		image.Rect(0, 0, 520, 400), image.Rect(680, 800, side, side), // across the circle, a quarter each
		image.Rect(330, 500, 870, 700),                             // inside it: every curve outside
		image.Rect(0, 0, 520, 30),                                  // outside it
		image.Rect(300, 0, 900, 300), image.Rect(0, 300, 300, 900), // across its top and its left
	} {
		if worst := furthest(whole.img, window, draw(window).img, image.Point{}); worst > tolerance {
			t.Errorf("through %v: %d levels from the whole circle at its furthest pixel", window, worst)
		}
	}
}

// What drawing a glyph allocates follows the page, not the font: at 3000
// pixels per em a W's mask is some nine million pixels, and the first W
// this face draws, across an 80 by 56 pixel page or off it, allocates under
// a megabyte, the rasterizer of the page's part included. No other test
// draws with this face: a face keeps the mask of the largest glyph it drew.
func TestGlyphAllocationFollowsThePage(t *testing.T) {
	bounds, _, _ := largeFace(t, 3000).GlyphBounds('W')
	if area := (bounds.Max.X - bounds.Min.X).Ceil() * (bounds.Max.Y - bounds.Min.Y).Ceil(); area < 5_000_000 {
		t.Fatalf("the W is %d pixels, not the millions this test is about", area)
	}

	src := image.NewUniform(opaqueBlack())

	for _, tc := range []struct {
		name  string
		ppem  float64
		at    fixed.Point26_6
		drawn bool
	}{
		{"across the page", 3000, fixed.Point26_6{X: fixed.I(-1000), Y: fixed.I(1000)}, true},
		{"off the page", 3001, fixed.Point26_6{X: fixed.I(5000), Y: fixed.I(5000)}, false},
	} {
		face, c := largeFace(t, tc.ppem), newCanvas(80, 56)

		var before, after runtime.MemStats

		runtime.ReadMemStats(&before)
		c.drawGlyph(face, 'W', tc.at, src)
		runtime.ReadMemStats(&after)

		if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 1<<20 {
			t.Errorf("%s: the glyph allocated %d bytes", tc.name, allocated)
		}

		if drawn := covered(c) > 0; drawn != tc.drawn {
			t.Errorf("%s: drawn: %v; want %v", tc.name, drawn, tc.drawn)
		}
	}
}
