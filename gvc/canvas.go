package gvc

import (
	"image"
	"image/color"
	"math"
	"slices"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/f64"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

const (
	// flatness is how far, in pixels, a flattened curve may stray from the
	// curve: a tenth of a pixel, under what anti-aliasing shows.
	flatness = 0.1
	// maxSubdivision bounds the halving of a curve while it is flattened.
	maxSubdivision = 16
	// midpoint is the parameter at which a curve is halved.
	midpoint = 0.5
	// miterLimit is Cairo's default, which Graphviz's Cairo renderer keeps:
	// a corner whose miter would reach past ten half pen widths from the
	// corner is bevelled instead.
	miterLimit = 10.0
	// kappa places the control points of a cubic Bézier quarter ellipse.
	kappa = 0.5522847498307936
	// collinear is the turn, as a cross product of unit directions, under
	// which a corner needs no join.
	collinear = 1e-9
	// coincident is the distance, in pixels, under which two points of a
	// path are one.
	coincident = 1e-9
	// subpixel is the fixed-point scale of font positions.
	subpixel = 64
	// maxGlyphMask is the most pixels a glyph is rasterized into as a mask
	// of its own. A larger glyph is filled as an outline, no further than
	// the page reaches: what a glyph costs follows the page, not its size.
	maxGlyphMask = 1 << 16
	// exactWidth is the width from which the rasterizer computes in
	// floating point. In a narrower and no taller area it rounds an edge's
	// position row by row, and a long edge ends most of a pixel away.
	exactWidth = 513
	// quadToCubic places the control points of the cubic Bézier that is a
	// quadratic one: two thirds of the way from each end to its control.
	quadToCubic = 2.0 / 3
)

// outliner is a face that gives a glyph's outline at the face's size: its
// segments in 26.6 pixels from the glyph's origin, every contour closed.
type outliner interface {
	outline(char rune) (sfnt.Segments, bool)
}

// point is a position on the canvas, in pixels.
type point struct{ x, y float64 }

func (p point) add(q point) point     { return point{p.x + q.x, p.y + q.y} }
func (p point) sub(q point) point     { return point{p.x - q.x, p.y - q.y} }
func (p point) scale(f float64) point { return point{p.x * f, p.y * f} }
func (p point) length() float64       { return math.Hypot(p.x, p.y) }
func (p point) cross(q point) float64 { return p.x*q.y - p.y*q.x }
func (p point) dot(q point) float64   { return p.x*q.x + p.y*q.y }
func (p point) normal() point         { return point{-p.y, p.x} }
func (p point) lerp(q point, t float64) point {
	return point{p.x + (q.x-p.x)*t, p.y + (q.y-p.y)*t}
}

// subpath is a flattened run of a path: its points, and whether it is
// closed back to its first.
type subpath struct {
	points []point
	closed bool
}

// canvas draws a page the way Graphviz's Cairo renderer does, over
// golang.org/x/image: anti-aliased fills under the nonzero rule, strokes
// with mitred corners (bevelled past Cairo's miter limit) and flat ends,
// dashes, text and images. Every position it takes is offset by its origin.
type canvas struct {
	img    *image.RGBA
	origin point
	path   []subpath
	ras    vector.Rasterizer
}

func newCanvas(width, height int) *canvas {
	return &canvas{img: image.NewRGBA(image.Rect(0, 0, width, height))}
}

// translate moves the origin every later position is taken from.
func (c *canvas) translate(byX, byY float64) {
	c.origin = c.origin.add(point{byX, byY})
}

func (c *canvas) moveTo(atX, atY float64) {
	c.path = append(c.path, subpath{points: []point{c.origin.add(point{atX, atY})}})
}

func (c *canvas) lineTo(atX, atY float64) {
	if len(c.path) == 0 {
		c.moveTo(atX, atY)

		return
	}

	last := &c.path[len(c.path)-1]
	last.points = append(last.points, c.origin.add(point{atX, atY}))
}

// cubicTo flattens the cubic Bézier from the current point through two
// control points to an end point, each given as its x and y.
func (c *canvas) cubicTo(ctrl1X, ctrl1Y, ctrl2X, ctrl2Y, endX, endY float64) {
	if len(c.path) == 0 {
		c.moveTo(ctrl1X, ctrl1Y)
	}

	last := &c.path[len(c.path)-1]
	start := last.points[len(last.points)-1]
	ctrl1 := c.origin.add(point{ctrl1X, ctrl1Y})
	ctrl2 := c.origin.add(point{ctrl2X, ctrl2Y})
	end := c.origin.add(point{endX, endY})
	last.points = flattenCubic(last.points, start, ctrl1, ctrl2, end, 0)
}

func (c *canvas) closePath() {
	if len(c.path) > 0 {
		c.path[len(c.path)-1].closed = true
	}
}

// ellipse adds a closed ellipse as four cubic Bézier quarters, the
// approximation Cairo draws an arc with.
func (c *canvas) ellipse(centreX, centreY, radiusX, radiusY float64) {
	ctrlX, ctrlY := kappa*radiusX, kappa*radiusY
	right, left := centreX+radiusX, centreX-radiusX
	bottom, top := centreY+radiusY, centreY-radiusY

	c.moveTo(right, centreY)
	c.cubicTo(right, centreY+ctrlY, centreX+ctrlX, bottom, centreX, bottom)
	c.cubicTo(centreX-ctrlX, bottom, left, centreY+ctrlY, left, centreY)
	c.cubicTo(left, centreY-ctrlY, centreX-ctrlX, top, centreX, top)
	c.cubicTo(centreX+ctrlX, top, right, centreY-ctrlY, right, centreY)
	c.closePath()
}

func (c *canvas) clearPath() {
	c.path = c.path[:0]
}

// fill paints the inside of the path, every subpath closed, in col.
func (c *canvas) fill(col color.Color) {
	var polygons [][]point

	for _, sub := range c.path {
		if len(sub.points) > 2 {
			polygons = append(polygons, sub.points)
		}
	}

	c.paint(polygons, col)
}

// stroke paints the path's outline width pixels wide in col, dashed by
// dashes (lengths on and off, in pixels; none for a solid line).
func (c *canvas) stroke(col color.Color, width float64, dashes []float64) {
	if width <= 0 {
		return
	}

	var pieces [][]point

	page := point{float64(c.img.Rect.Dx()), float64(c.img.Rect.Dy())}

	for _, sub := range c.path {
		for _, run := range dash(sub, dashes, page) {
			pieces = strokePieces(pieces, run, width/2)
		}
	}

	c.paint(pieces, col)
}

// paint fills polygons, each wound either way, as one nonzero shape: the
// rasterizer sums signed coverage and takes its magnitude, clamped, so the
// polygons are wound alike first and overlaps are covered once. Only the
// polygons' bounding box is rasterized.
func (c *canvas) paint(polygons [][]point, col color.Color) {
	bounds := polygonBounds(polygons).Intersect(c.img.Bounds())
	if bounds.Empty() {
		return
	}

	c.ras.Reset(bounds.Dx(), bounds.Dy())

	offset := point{float64(bounds.Min.X), float64(bounds.Min.Y)}

	// Each polygon is clamped to what is rasterized: the rasterizer steps
	// along an edge a row at a time from where it starts, rounding each
	// step, and an edge that starts far off the page arrives pixels away
	// from its place, or not at all.
	path := clampedPath{ras: &c.ras, max: point{float64(bounds.Dx()), float64(bounds.Dy())}}

	for _, polygon := range polygons {
		if signedArea(polygon) < 0 {
			polygon = reversed(polygon)
		}

		first := polygon[0].sub(offset)
		path.moveTo(first)

		for _, p := range polygon[1:] {
			path.lineTo(p.sub(offset))
		}

		path.lineTo(first)
	}

	c.ras.Draw(c.img, bounds, image.NewUniform(col), image.Point{})
}

// polygonBounds is the smallest pixel rectangle that holds every point.
func polygonBounds(polygons [][]point) image.Rectangle {
	var bounds image.Rectangle

	for _, polygon := range polygons {
		for _, p := range polygon {
			pixel := image.Rect(
				int(math.Floor(p.x)), int(math.Floor(p.y)),
				int(math.Ceil(p.x))+1, int(math.Ceil(p.y))+1,
			)
			if bounds.Empty() {
				bounds = pixel
			} else {
				bounds = bounds.Union(pixel)
			}
		}
	}

	return bounds
}

// drawString draws text with face in col, its baseline starting at (atX,
// atY). Each glyph is rasterized at its position before the origin's offset
// and moved by that offset with bilinear resampling, which lands nearer
// Graphviz's own text than drawing it at the offset (measured against dot
// 16.1.0 with the smoke test). A glyph too large for a mask is filled as its
// outline instead.
func (c *canvas) drawString(face font.Face, text string, atX, atY float64, col color.Color) {
	src := image.NewUniform(col)
	dot := fixed.Point26_6{
		X: fixed.Int26_6(math.Round(atX * subpixel)),
		Y: fixed.Int26_6(math.Round(atY * subpixel)),
	}
	previous := rune(-1)

	for _, char := range text {
		if previous >= 0 {
			dot.X += face.Kern(previous, char)
		}

		dot.X += c.drawGlyph(face, char, dot, src)
		previous = char
	}
}

// drawGlyph draws char with its origin at dot, and returns its advance.
func (c *canvas) drawGlyph(face font.Face, char rune, dot fixed.Point26_6, src image.Image) fixed.Int26_6 {
	// The bounds come first: they cost no mask, and the mask is the size
	// of the glyph, whatever the page.
	bounds, advance, ok := face.GlyphBounds(char)
	if !ok {
		return advance
	}

	pixels := (bounds.Max.X - bounds.Min.X).Ceil() * (bounds.Max.Y - bounds.Min.Y).Ceil()

	if outlined, gives := face.(outliner); gives && pixels > maxGlyphMask {
		if segments, found := outlined.outline(char); found {
			c.fillOutline(segments, c.origin.add(point{float64(dot.X) / subpixel, float64(dot.Y) / subpixel}), src)
		}

		return advance
	}

	if rect, mask, maskp, _, drawn := face.Glyph(dot, char); drawn {
		corner := c.origin.add(point{float64(rect.Min.X), float64(rect.Min.Y)})
		transform := f64.Aff3{1, 0, corner.x, 0, 1, corner.y}
		draw.BiLinear.Transform(c.img, transform, src, rect.Sub(rect.Min), draw.Over,
			&draw.Options{SrcMask: mask, SrcMaskP: maskp})
	}

	return advance
}

// fillOutline fills a glyph's outline, its origin at origin, with src,
// rasterizing only the part of the page the outline's bounds cover.
func (c *canvas) fillOutline(segments sfnt.Segments, origin point, src image.Image) {
	// placed is a point of the outline, in pixels from from.
	placed := func(p fixed.Point26_6, from point) point {
		return from.add(point{float64(p.X) / subpixel, float64(p.Y) / subpixel})
	}

	bounds := segments.Bounds()
	topLeft, bottomRight := placed(bounds.Min, origin), placed(bounds.Max, origin)

	clip := image.Rect(
		int(math.Floor(topLeft.x)), int(math.Floor(topLeft.y)),
		int(math.Ceil(bottomRight.x)), int(math.Ceil(bottomRight.y)),
	).Intersect(c.img.Bounds())
	if clip.Empty() {
		return
	}

	// A large glyph's edges are long: the area is made wide enough to be
	// rasterized exactly, and no more of it is drawn for that.
	c.ras.Reset(max(clip.Dx(), exactWidth), clip.Dy())

	// The outline's origin from the corner of what is rasterized.
	from := origin.sub(point{float64(clip.Min.X), float64(clip.Min.Y)})
	path := clampedPath{ras: &c.ras, max: point{float64(clip.Dx()), float64(clip.Dy())}}

	for _, segment := range segments {
		switch segment.Op {
		case sfnt.SegmentOpMoveTo:
			path.moveTo(placed(segment.Args[0], from))
		case sfnt.SegmentOpLineTo:
			path.lineTo(placed(segment.Args[0], from))
		case sfnt.SegmentOpQuadTo:
			ctrl, end := placed(segment.Args[0], from), placed(segment.Args[1], from)
			path.cubicTo(path.pen.lerp(ctrl, quadToCubic), end.lerp(ctrl, quadToCubic), end, 0)
		case sfnt.SegmentOpCubeTo:
			path.cubicTo(placed(segment.Args[0], from), placed(segment.Args[1], from), placed(segment.Args[2], from), 0)
		}
	}

	c.ras.Draw(c.img, clip, src, image.Point{})
}

// clampedPath feeds a rasterizer a path of closed contours clamped to the
// rectangle from the origin to max, the part of the rasterizer's area that
// is drawn: what lies outside is drawn along the rectangle's edge. The
// rasterizer sums, row by row, the height each line crosses, at the line's
// column, or at the first column or past the last for a line outside, so a
// line moved onto the edge covers the pixels inside as it did where it was,
// and a path is walked no further than the rectangle, however far it runs.
type clampedPath struct {
	ras *vector.Rasterizer
	max point
	pen point
}

func (p *clampedPath) clamp(q point) (x, y float32) {
	return float32(math.Min(math.Max(q.x, 0), p.max.x)), float32(math.Min(math.Max(q.y, 0), p.max.y))
}

func (p *clampedPath) moveTo(to point) {
	p.ras.MoveTo(p.clamp(to))
	p.pen = to
}

// lineTo draws the line from the pen to next, clamped: it is cut where it
// crosses an edge of the rectangle, since a clamped line turns there.
func (p *clampedPath) lineTo(next point) {
	delta := next.sub(p.pen)
	cuts := make([]float64, 0, 4)

	for _, crossing := range [...]struct{ from, by, edge float64 }{
		{p.pen.x, delta.x, 0},
		{p.pen.x, delta.x, p.max.x},
		{p.pen.y, delta.y, 0},
		{p.pen.y, delta.y, p.max.y},
	} {
		if crossing.by == 0 {
			continue
		}

		if t := (crossing.edge - crossing.from) / crossing.by; t > 0 && t < 1 {
			cuts = append(cuts, t)
		}
	}

	slices.Sort(cuts)

	for _, t := range cuts {
		p.ras.LineTo(p.clamp(p.pen.lerp(next, t)))
	}

	p.ras.LineTo(p.clamp(next))
	p.pen = next
}

// cubicTo draws the cubic Bézier from the pen through two control points
// to end as lines within flatness of it. A curve wholly to one side of the
// rectangle is its chord: outside, only the rows a path crosses count, and
// the chord crosses the rows the curve does, the same way.
func (p *clampedPath) cubicTo(ctrl1, ctrl2, end point, depth int) {
	left := math.Max(math.Max(p.pen.x, ctrl1.x), math.Max(ctrl2.x, end.x)) < 0
	right := math.Min(math.Min(p.pen.x, ctrl1.x), math.Min(ctrl2.x, end.x)) > p.max.x
	above := math.Max(math.Max(p.pen.y, ctrl1.y), math.Max(ctrl2.y, end.y)) < 0
	below := math.Min(math.Min(p.pen.y, ctrl1.y), math.Min(ctrl2.y, end.y)) > p.max.y

	if left || right || above || below || depth >= maxSubdivision ||
		cubicDeviation(p.pen, ctrl1, ctrl2, end) <= flatness {
		p.lineTo(end)

		return
	}

	// de Casteljau at the midpoint.
	first, second, third := p.pen.lerp(ctrl1, midpoint), ctrl1.lerp(ctrl2, midpoint), ctrl2.lerp(end, midpoint)
	near, far := first.lerp(second, midpoint), second.lerp(third, midpoint)
	mid := near.lerp(far, midpoint)

	p.cubicTo(first, near, mid, depth+1)
	p.cubicTo(far, third, end, depth+1)
}

// drawImage draws img with its top left corner at (atX, atY), rounded to
// the nearest pixel, over what is there.
func (c *canvas) drawImage(img image.Image, atX, atY float64) {
	corner := c.origin.add(point{atX, atY})
	topLeft := image.Pt(int(math.Round(corner.x)), int(math.Round(corner.y)))
	draw.Draw(
		c.img,
		image.Rectangle{Min: topLeft, Max: topLeft.Add(img.Bounds().Size())},
		img,
		img.Bounds().Min,
		draw.Over,
	)
}

// flattenCubic appends to points the cubic Bézier from start through two
// control points to end, less its start, as line segments within flatness
// of the curve.
func flattenCubic(points []point, start, ctrl1, ctrl2, end point, depth int) []point {
	if cubicDeviation(start, ctrl1, ctrl2, end) <= flatness || depth >= maxSubdivision {
		return append(points, end)
	}

	// de Casteljau at the midpoint.
	first, second, third := start.lerp(ctrl1, midpoint), ctrl1.lerp(ctrl2, midpoint), ctrl2.lerp(end, midpoint)
	left, right := first.lerp(second, midpoint), second.lerp(third, midpoint)
	mid := left.lerp(right, midpoint)

	points = flattenCubic(points, start, first, left, mid, depth+1)

	return flattenCubic(points, mid, right, third, end, depth+1)
}

// cubicDeviation is how far a cubic Bézier's control points lie from its
// chord, which bounds how far the curve does.
func cubicDeviation(start, ctrl1, ctrl2, end point) float64 {
	chord := end.sub(start)

	length := chord.length()
	if length == 0 {
		return math.Max(ctrl1.sub(start).length(), ctrl2.sub(start).length())
	}

	return math.Max(math.Abs(chord.cross(ctrl1.sub(start))), math.Abs(chord.cross(ctrl2.sub(start)))) / length
}

// dasher walks a polyline through a dash pattern, total long, on a page
// that reaches from the origin to page.
type dasher struct {
	pattern   []float64
	total     float64
	page      point
	index     int
	remaining float64
	drawing   bool
	current   []point
	runs      []subpath
	broken    bool
}

// cut moves the walk to where the pattern turns over, at position cut.
func (d *dasher) cut(cut point) {
	if d.drawing {
		d.runs = append(d.runs, subpath{points: append(d.current, cut)})
		d.current = nil
		d.broken = true
	} else {
		d.current = []point{cut}
	}

	d.drawing = !d.drawing
	d.index = (d.index + 1) % len(d.pattern)
	d.remaining = d.pattern[d.index]
}

// segment walks the segment from one point to the next. Where the segment
// runs off the page, the whole turns of the pattern that lie off it are
// stepped over: they leave nothing to see, and a segment is cut into no
// more dashes than the page shows of it, and a turn before, however long it
// is. A dash begun before a step is drawn from there, longer than it is by
// what is off the page.
func (d *dasher) segment(from, next point) {
	length := next.sub(from).length()
	shownFrom, shownTo := d.shown(from, next, length)
	done := 0.0

	for length-done > d.remaining {
		done += d.remaining
		d.cut(from.lerp(next, done/length))

		var off float64

		// A step stays inside its segment: past its end the path turns,
		// and a dash that is off the page along this segment may reach it
		// from the next one, by as much as half the pen.
		switch {
		case done < shownFrom:
			off = math.Min(shownFrom, length) - done
		case done > shownTo:
			off = length - done
		}

		done += math.Floor(off/d.total) * d.total
	}

	d.remaining -= length - done

	if d.drawing {
		d.current = append(d.current, next)
	}
}

// shown is the stretch of a segment, as distances from its start, whose
// dashes can reach the page: from the first of the page's corners along
// the segment to the last. A dash ends flat, so one before or past every
// corner covers nothing of the page, whatever the pen.
func (d *dasher) shown(from, next point, length float64) (first, last float64) {
	if length == 0 {
		return 0, 0
	}

	along := next.sub(from).scale(1 / length)
	first, last = math.Inf(1), math.Inf(-1)

	for _, corner := range [...]point{{0, 0}, {d.page.x, 0}, {0, d.page.y}, d.page} {
		at := corner.sub(from).dot(along)
		first, last = math.Min(first, at), math.Max(last, at)
	}

	return first, last
}

// dash cuts a subpath into the runs a dash pattern leaves drawn on a page
// that reaches from the origin to page, the way Cairo does: lengths
// alternate on and off, an odd count repeats, and the pattern starts afresh
// with each subpath. A closed subpath no dash breaks stays closed. No
// pattern leaves the subpath whole. A pattern that turns over in less than
// flatness is drawn as Cairo approximates it: as one dash and one gap that
// length together, the same share of it drawn.
func dash(sub subpath, pattern []float64, page point) []subpath {
	if len(pattern)%2 == 1 {
		pattern = slices.Concat(pattern, pattern)
	}

	var total, drawn float64
	for i, length := range pattern {
		total += length

		if i%2 == 0 {
			drawn += length
		}
	}

	if len(pattern) == 0 || total <= 0 || len(sub.points) < 2 {
		return []subpath{sub}
	}

	if total < flatness {
		pattern, total = []float64{flatness * drawn / total, flatness * (1 - drawn/total)}, flatness
	}

	points := sub.points
	if sub.closed {
		points = slices.Concat(points, points[:1])
	}

	walk := &dasher{
		pattern: pattern, total: total, page: page,
		remaining: pattern[0], drawing: true, current: []point{points[0]},
	}

	for i := 1; i < len(points); i++ {
		walk.segment(points[i-1], points[i])
	}

	if walk.drawing && len(walk.current) > 1 {
		switch {
		case sub.closed && !walk.broken:
			return []subpath{sub}
		case sub.closed:
			// Drawing where it closes and where it starts: the last dash and
			// the first are one, joined at the start, as Cairo draws them.
			walk.runs[0] = subpath{points: slices.Concat(walk.current, walk.runs[0].points[1:])}
		default:
			walk.runs = append(walk.runs, subpath{points: walk.current})
		}
	}

	return walk.runs
}

// strokePieces appends to pieces the convex polygons that make up a run's
// outline halfWidth either side of it: a quad along each segment and a
// join at each corner. Ends are flat: a run is drawn exactly as long as it
// is.
func strokePieces(pieces [][]point, run subpath, halfWidth float64) [][]point {
	points := dedupe(run.points)
	if len(points) < 2 {
		return pieces
	}

	closed := run.closed && len(points) > 2
	if closed && points[len(points)-1].sub(points[0]).length() < coincident {
		points = points[:len(points)-1]
	}

	count := len(points)

	segments := count - 1
	if closed {
		segments = count
	}

	direction := func(i int) point {
		d := points[(i+1)%count].sub(points[i%count])

		return d.scale(1 / d.length())
	}

	for i := range segments {
		from, next := points[i%count], points[(i+1)%count]
		n := direction(i).normal().scale(halfWidth)
		pieces = append(pieces, []point{from.add(n), next.add(n), next.sub(n), from.sub(n)})
	}

	joins := segments - 1
	if closed {
		joins = segments
	}

	for j := range joins {
		if join := joinPiece(points[(j+1)%count], direction(j), direction(j+1), halfWidth); join != nil {
			pieces = append(pieces, join)
		}
	}

	return pieces
}

// joinPiece is the wedge on the outer side of a corner, between the ends of
// the segments arriving along inward and leaving along outward (unit
// directions): up to the miter, or the bevel past Cairo's miter limit. A
// straight corner has none.
func joinPiece(corner, inward, outward point, halfWidth float64) []point {
	turn := inward.cross(outward)
	cosine := inward.dot(outward)

	if math.Abs(turn) < collinear && cosine > 0 {
		return nil
	}

	// The outer side is the one the path turns away from.
	side := 1.0
	if turn > 0 {
		side = -1
	}

	inEdge, outEdge := inward.normal().scale(side*halfWidth), outward.normal().scale(side*halfWidth)
	inCorner, outCorner := corner.add(inEdge), corner.add(outEdge)

	// The miter reaches 1/cos(half the turn) half widths from the corner.
	if 1/math.Sqrt((1+cosine)/2) <= miterLimit {
		tip := corner.add(inEdge.add(outEdge).scale(1 / (1 + cosine)))

		return []point{corner, inCorner, tip, outCorner}
	}

	return []point{corner, inCorner, outCorner}
}

// dedupe drops consecutive repeats, which have no direction to stroke.
func dedupe(points []point) []point {
	out := make([]point, 0, len(points))

	for _, p := range points {
		if len(out) > 0 && p.sub(out[len(out)-1]).length() < coincident {
			continue
		}

		out = append(out, p)
	}

	return out
}

// signedArea is twice the polygon's signed area, positive when it winds
// clockwise on the y-down canvas.
func signedArea(polygon []point) float64 {
	var area float64

	for i, p := range polygon {
		area += p.cross(polygon[(i+1)%len(polygon)])
	}

	return area
}

func reversed(polygon []point) []point {
	out := slices.Clone(polygon)
	slices.Reverse(out)

	return out
}
