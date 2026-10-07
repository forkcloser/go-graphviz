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
