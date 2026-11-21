package gvc

import (
	"image/color"
	"testing"
)

// opaqueBlack and halfBlack are the pens the tests draw with.
func opaqueBlack() color.NRGBA { return color.NRGBA{A: 0xff} }

func halfBlack() color.NRGBA { return color.NRGBA{A: 0x80} }

// alphaAt is the canvas's alpha at pixel (x, y).
func alphaAt(c *canvas, x, y int) uint8 {
	return c.img.RGBAAt(x, y).A
}

// A filled rectangle on pixel edges covers exactly its pixels.
func TestCanvasFill(t *testing.T) {
	c := newCanvas(20, 20)
	c.moveTo(5, 5)
	c.lineTo(15, 5)
	c.lineTo(15, 15)
	c.lineTo(5, 15)
	c.closePath()
	c.fill(opaqueBlack())

	for _, tc := range []struct {
		x, y int
		want uint8
	}{
		{5, 5, 0xff},
		{14, 14, 0xff},
		{10, 10, 0xff},
		{4, 10, 0},
		{15, 10, 0},
		{10, 4, 0},
		{10, 15, 0},
	} {
		if got := alphaAt(c, tc.x, tc.y); got != tc.want {
			t.Errorf("pixel (%d, %d): alpha %d, want %d", tc.x, tc.y, got, tc.want)
		}
	}
}

// A stroked line ends flat at its end points and is as wide as the pen.
func TestCanvasStrokeFlatEnds(t *testing.T) {
	c := newCanvas(40, 20)
	c.moveTo(10, 10)
	c.lineTo(30, 10)
	c.stroke(opaqueBlack(), 4, nil)

	for _, tc := range []struct {
		x, y int
		want uint8
	}{
		{10, 8, 0xff}, {29, 11, 0xff}, // inside the band
		{9, 10, 0}, {30, 10, 0}, // past the ends: flat, not round
		{20, 7, 0}, {20, 12, 0}, // outside the width
	} {
		if got := alphaAt(c, tc.x, tc.y); got != tc.want {
			t.Errorf("pixel (%d, %d): alpha %d, want %d", tc.x, tc.y, got, tc.want)
		}
	}
}

// A stroked square has square outer corners: the joins are mitred, as
// Cairo draws them, where gg drew them round.
func TestCanvasStrokeMiterJoin(t *testing.T) {
	c := newCanvas(40, 40)
	c.moveTo(10, 10)
	c.lineTo(30, 10)
	c.lineTo(30, 30)
	c.lineTo(10, 30)
	c.closePath()
	c.stroke(opaqueBlack(), 6, nil)

	for _, corner := range [][2]int{{7, 7}, {32, 7}, {32, 32}, {7, 32}} {
		if got := alphaAt(c, corner[0], corner[1]); got != 0xff {
			t.Errorf("outer corner pixel (%d, %d): alpha %d, want a full miter", corner[0], corner[1], got)
		}
	}

	if got := alphaAt(c, 20, 20); got != 0 {
		t.Errorf("inside the square: alpha %d, want 0", got)
	}
}

// A corner sharper than Cairo's miter limit is bevelled: the miter of a
// near reversal would reach far past the corner.
func TestCanvasStrokeMiterLimit(t *testing.T) {
	c := newCanvas(100, 40)
	c.moveTo(10, 18)
	c.lineTo(60, 20)
	c.lineTo(10, 22)
	c.stroke(opaqueBlack(), 4, nil)

	for x := 70; x < 100; x++ {
		if got := alphaAt(c, x, 20); got != 0 {
			t.Fatalf("pixel (%d, 20) past a bevelled corner: alpha %d, want 0", x, got)
		}
	}
}

// Dashes follow the pattern from the start of the line: on, off, on.
func TestCanvasStrokeDashes(t *testing.T) {
	c := newCanvas(60, 10)
	c.moveTo(0, 5)
	c.lineTo(60, 5)
	c.stroke(opaqueBlack(), 2, []float64{6})

	for _, tc := range []struct {
		x    int
		want uint8
	}{
		{1, 0xff}, {4, 0xff}, {7, 0}, {10, 0}, {13, 0xff}, {16, 0xff}, {19, 0},
	} {
		if got := alphaAt(c, tc.x, 5); got != tc.want {
			t.Errorf("pixel (%d, 5): alpha %d, want %d", tc.x, got, tc.want)
		}
	}
}

// A translucent pen covers a corner once: the pieces of a stroke are one
// shape, so where they overlap the alpha is the pen's, not twice it.
func TestCanvasStrokeTranslucentOverlap(t *testing.T) {
	c := newCanvas(40, 40)
	c.moveTo(10, 10)
	c.lineTo(30, 10)
	c.lineTo(30, 30)
	c.stroke(halfBlack(), 6, nil)

	along, corner := alphaAt(c, 20, 10), alphaAt(c, 30, 10)
	if along == 0 || corner != along {
		t.Errorf("alpha along the line %d, at the corner %d: want equal and non-zero", along, corner)
	}
}

// An ellipse is closed and round: its points sit on the radii.
func TestCanvasEllipse(t *testing.T) {
	c := newCanvas(40, 40)
	c.ellipse(20, 20, 10, 5)
	c.fill(opaqueBlack())

	for _, tc := range []struct {
		x, y int
		want uint8
	}{
		{20, 20, 0xff},
		{11, 20, 0xff},
		{28, 20, 0xff},
		{20, 16, 0xff},
		{20, 23, 0xff},
		{8, 20, 0},
		{31, 20, 0},
		{20, 13, 0},
		{20, 26, 0},
		{11, 16, 0},
	} {
		if got := alphaAt(c, tc.x, tc.y); got != tc.want {
			t.Errorf("pixel (%d, %d): alpha %d, want %d", tc.x, tc.y, got, tc.want)
		}
	}
}

// The origin offsets every position, and a shape partly off the canvas is
// drawn where it overlaps it.
func TestCanvasOriginAndClipping(t *testing.T) {
	c := newCanvas(20, 20)
	c.translate(-10, 5)
	c.moveTo(5, 0)
	c.lineTo(25, 0)
	c.lineTo(25, 10)
	c.lineTo(5, 10)
	c.closePath()
	c.fill(opaqueBlack())

	if got := alphaAt(c, 0, 10); got != 0xff {
		t.Errorf("pixel (0, 10) inside the clipped rectangle: alpha %d", got)
	}

	if got := alphaAt(c, 15, 10); got != 0 {
		t.Errorf("pixel (15, 10) past the rectangle's right: alpha %d", got)
	}

	if got := alphaAt(c, 5, 4); got != 0 {
		t.Errorf("pixel (5, 4) above the translated rectangle: alpha %d", got)
	}
}

// A closed path dashed so that a dash runs through its start is joined
// there, as Cairo joins the last dash to the first: the start corner is
// mitred like the others, where it had two flat ends. The gaps stay gaps.
func TestCanvasStrokeClosedDashes(t *testing.T) {
	c := newCanvas(40, 40)
	c.moveTo(10, 10)
	c.lineTo(30, 10)
	c.lineTo(30, 30)
	c.lineTo(10, 30)
	c.closePath()
	// 80 around: on 0 to 9, 18 to 27, …, 72 to 81, so drawing at the close.
	c.stroke(opaqueBlack(), 6, []float64{9})

	if got := alphaAt(c, 7, 7); got != 0xff {
		t.Errorf("start corner (7, 7): alpha %d, want a full miter", got)
	}

	if got := alphaAt(c, 23, 10); got != 0 {
		t.Errorf("pixel (23, 10) in the first gap: alpha %d, want 0", got)
	}
}

// A pattern that turns over in less than a tenth of a pixel is drawn as one
// dash and one gap that length together: a hundred pixels of it are a
// thousand dashes, not the twenty-five thousand of the pattern, and cover
// the quarter of the line the pattern covers.
func TestCanvasDashesFinerThanAPixel(t *testing.T) {
	c := newCanvas(100, 10)
	c.moveTo(0, 5)
	c.lineTo(100, 5)

	if runs := len(dash(c.path[0], []float64{0.001, 0.003}, point{100, 10})); runs < 990 || runs > 1010 {
		t.Errorf("%d dashes in a hundred pixels, want a thousand", runs)
	}

	c.stroke(opaqueBlack(), 2, []float64{0.001, 0.003})

	for _, x := range []int{3, 50, 97} {
		if got := alphaAt(c, x, 5); got < 60 || got > 68 {
			t.Errorf("pixel (%d, 5): alpha %d, want the quarter of 255 the pattern covers", x, got)
		}
	}
}

// A dashed line much longer than the page is cut into the dashes the page
// shows, and those are the dashes of the whole line: the pattern's turns
// off the page are stepped over, not walked and not lost. The line starts
// a million pixels to the left, which is 4 short of a whole number of 12
// pixel turns: on the page a dash ends at 2, and the next runs from 8.
func TestCanvasDashesOffThePage(t *testing.T) {
	const far = 1e6

	page := point{60, 10}

	for name, ends := range map[string][2]point{
		"left to right": {{-far, 5}, {far, 5}},
		"ending on it":  {{-far, 5}, {30, 5}},
	} {
		c := newCanvas(60, 10)
		c.moveTo(ends[0].x, ends[0].y)
		c.lineTo(ends[1].x, ends[1].y)

		if runs := len(dash(c.path[0], []float64{6}, page)); runs > 12 {
			t.Errorf("%s: %d dashes for a page that shows six", name, runs)
		}

		c.stroke(opaqueBlack(), 2, []float64{6})

		for _, tc := range []struct {
			x    int
			want uint8
		}{
			{0, 0xff}, {1, 0xff}, {3, 0}, {6, 0}, {9, 0xff}, {12, 0xff}, {15, 0}, {18, 0}, {21, 0xff}, {25, 0xff}, {27, 0},
		} {
			if got := alphaAt(c, tc.x, 5); got != tc.want {
				t.Errorf("%s: pixel (%d, 5): alpha %d, want %d", name, tc.x, got, tc.want)
			}
		}
	}
}

// A dashed path that leaves the page and comes back keeps its pattern and
// its corners: around a square far larger than the page, whose first corner
// alone is on it, the dashes at that corner are those of a square that is
// walked in full.
func TestCanvasDashesAroundAnOffPageCorner(t *testing.T) {
	const side = 12e4 // a whole number of 12 pixel turns: the close meets the start in step

	draw := func(page point) *canvas {
		c := newCanvas(40, 40)
		c.moveTo(20, 20)
		c.lineTo(20+side, 20)
		c.lineTo(20+side, 20+side)
		c.lineTo(20, 20+side)
		c.closePath()

		var pieces [][]point
		for _, run := range dash(c.path[0], []float64{6}, page) {
			pieces = strokePieces(pieces, run, 3)
		}

		c.paint(pieces, opaqueBlack())

		return c
	}

	// A page as large as the square steps over nothing.
	stepped, walked := draw(point{40, 40}), draw(point{2 * side, 2 * side})

	if alphaAt(walked, 22, 20) != 0xff || alphaAt(walked, 20, 22) != 0xff || alphaAt(walked, 29, 20) != 0 {
		t.Fatal("the square walked in full has no dash at its corner")
	}

	if worst := furthestAlpha(stepped, walked); worst != 0 {
		t.Errorf("stepping over the turns off the page moved a pixel by %d", worst)
	}
}

// A step over the pattern's turns stops at the end of its segment: where
// the path turns off the page and comes onto it at an angle, a wide pen
// draws the dashes of the path walked in full. A step carried past the
// vertex left one over-long dash, off the page along the first segment and
// on it across the second, half the pen deep.
func TestCanvasDashesWidePenPastAnOffPageVertex(t *testing.T) {
	const (
		side      = 200
		halfWidth = 45
		away      = 2000 // moves the path into a page that holds all of it
	)

	path := []point{{-1367.2, 180.5}, {-10.2, 120.1}, {59.8, 89.9}, {230, 40}}
	pattern := []float64{5, 3, 1, 3}

	draw := func(page point, shift float64) *canvas {
		moved := make([]point, len(path))
		for i, p := range path {
			moved[i] = p.add(point{shift, shift})
		}

		var pieces [][]point

		for _, run := range dash(subpath{points: moved}, pattern, page) {
			for i, p := range run.points {
				run.points[i] = p.sub(point{shift, shift})
			}

			pieces = strokePieces(pieces, run, halfWidth)
		}

		c := newCanvas(side, side)
		c.paint(pieces, opaqueBlack())

		return c
	}

	stepped, walked := draw(point{side, side}, 0), draw(point{2 * away, 2 * away}, away)

	if inked(walked) == 0 || inked(walked) == side*side {
		t.Fatalf("the path walked in full covers %d pixels of the page, not dashes", inked(walked))
	}

	// The same cuts computed two thousand pixels apart: a rounding apart.
	if worst := furthestAlpha(stepped, walked); worst > 2 {
		t.Errorf("stepping over the turns off the page moved a pixel by %d levels", worst)
	}
}

// inked is how many pixels of the canvas are opaque.
func inked(c *canvas) int {
	count := 0

	for i := 3; i < len(c.img.Pix); i += 4 {
		if c.img.Pix[i] == 0xff {
			count++
		}
	}

	return count
}

// furthestAlpha is the largest difference in alpha between two canvases of
// one size.
func furthestAlpha(one, other *canvas) int {
	worst := 0

	for i := 3; i < len(one.img.Pix); i += 4 {
		worst = max(worst, int(one.img.Pix[i])-int(other.img.Pix[i]), int(other.img.Pix[i])-int(one.img.Pix[i]))
	}

	return worst
}
