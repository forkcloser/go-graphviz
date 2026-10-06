package gvc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // node images: Graphviz recognizes GIF
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"sync"

	"github.com/fogleman/gg"
	_ "golang.org/x/image/bmp" // node images: Graphviz recognizes BMP
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	_ "golang.org/x/image/webp" // node images: Graphviz recognizes WebP

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

const (
	// Pen styles, in points: Graphviz draws a dashed line as four on, four
	// off, and a dotted one as two on, four off.
	dashLength = 4.0
	dotLength  = 2.0
	// half centres a span or an image on its anchor.
	half = 2.0
	// colorChannelMax is the top of Graphviz's 8-bit colour channels; gg
	// takes channels in [0, 1].
	colorChannelMax = 255.0
	// lanczosLobes is the support of the Lanczos kernel in pixels (three
	// lobes), the resampling filter node images have always used here.
	lanczosLobes = 3.0
	// landscape is the rotation, in degrees, of a page laid out with
	// rotate=90 or landscape=true.
	landscape = 90
)

// ErrImageTooLarge is returned by the raster renderer for a node image
// whose pixels, as its file declares them or at the size it is drawn,
// exceed MaxImagePixels.
var ErrImageTooLarge = errors.New("node image too large for the raster renderer")

// MaxImagePixels is how many pixels a node image may have, decoded or
// scaled: 64 megapixels, 256 MB as RGBA. The image file and the size it
// is drawn at both come from the graph.
const MaxImagePixels = 1 << 26

// ErrRotation is returned by the raster renderer for a page turned by an
// angle other than the two Graphviz uses, 0 and 90 degrees.
var ErrRotation = errors.New("page rotation not supported by the raster renderer")

// ErrPageTooLarge is returned by the raster renderer for a page whose width
// or height does not fit the drawing context's int coordinates.
var ErrPageTooLarge = errors.New("page too large for the raster renderer")

type ImageRenderer struct {
	*DefaultRenderEngine
	ctx *gg.Context
	// scaleX and scaleY take a length in points along the canvas's axes to
	// pixels, and lineScale takes a pen width or a dash; BeginPage sets
	// them from the job. rotated says the canvas holds a landscape page
	// drawn upright, which EndPage turns a quarter turn.
	scaleX, scaleY float64
	lineScale      float64
	rotated        bool
	// images holds the node images of the page being drawn, decoded and
	// at each size drawn, by file name and by name and size.
	images map[string]image.Image
	// imageOnly skips encoding a page that only its image is wanted for,
	// and last keeps that image; Context.RenderImage sets the first and
	// reads the second while it holds the module, so no other render runs
	// on the renderer in between.
	imageOnly bool
	last      image.Image
}

// BeginPage sets the canvas up the way Graphviz's Cairo renderer sets its
// page: a point p of the graph lands at scale × rotate(-rotation) ×
// ((p.x, -p.y) + (tx, -ty)). Pen widths and dashes are lengths in that
// space too, so they shrink and grow with the page.
//
// gg cannot turn glyphs, so a landscape page (rotation 90) is drawn upright
// on a canvas with the sides swapped and turned when it is finished: there
// a point lands at (height + sy × (p.x + tx), sx × (-p.y - ty)), and the
// finished page's pixel (X, Y) is the canvas's (height-1-Y, X).
func (r *ImageRenderer) BeginPage(_ context.Context, job *Job) error {
	width, height := job.Width(), job.Height()
	if width > math.MaxInt32 || height > math.MaxInt32 {
		return fmt.Errorf("%w: %d by %d points", ErrPageTooLarge, width, height)
	}

	r.images = map[string]image.Image{}

	scale, translation := job.Scale(), job.Translation()
	r.lineScale = math.Sqrt(math.Abs(scale.X() * scale.Y()))

	switch rotation := job.Rotation(); rotation {
	case 0:
		r.rotated = false
		r.scaleX, r.scaleY = scale.X(), scale.Y()
		r.ctx = gg.NewContext(int(width), int(height))
		r.ctx.Translate(r.scaleX*translation.X(), -r.scaleY*translation.Y())
	case landscape:
		r.rotated = true
		r.scaleX, r.scaleY = scale.Y(), scale.X()
		r.ctx = gg.NewContext(int(height), int(width))
		r.ctx.Translate(float64(height)+r.scaleX*translation.X(), -r.scaleY*translation.Y())
	default:
		return fmt.Errorf("%w: %d degrees", ErrRotation, rotation)
	}

	return nil
}

func (r *ImageRenderer) EndPage(_ context.Context, job *Job) error {
	page := r.page()
	r.images = nil

	if r.imageOnly {
		r.last = page

		return nil
	}

	var buf bytes.Buffer

	switch {
	case r.isPNG(job):
		if err := png.Encode(&buf, page); err != nil {
			return fmt.Errorf("encoding the page as PNG: %w", err)
		}
	case r.isJPG(job):
		if err := jpeg.Encode(&buf, page, &jpeg.Options{Quality: jpeg.DefaultQuality}); err != nil {
			return fmt.Errorf("encoding the page as JPEG: %w", err)
		}
	}

	job.SetOutputData(buf.Bytes())
	job.SetOutputDataPosition(uint(len(buf.Bytes())))

	return nil
}

func (r *ImageRenderer) TextSpan(ctx context.Context, job *Job, pos *PointFloat, span *TextSpan) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.setColor(job.Object().PenColor())

	size, dpi := span.Font().Size()*job.Zoom(), resolution(job.DPI())

	primary, err := r.spanFace(ctx, job, span.Font(), size, dpi)
	if err != nil {
		return err
	}

	// A character the span's font lacks is drawn from a font that has it,
	// so the span is drawn run by run, and justified by the width drawn:
	// Graphviz's own renderers justify by the width their text layout
	// measured, which is the width they draw.
	runs := fonts.fallback().runs(span.Text(), primary, size, dpi)
	width := advance(runs)

	penX := r.toX(job, pos.X())

	switch span.Just() {
	case 'r':
		penX -= width
	case 'l':
	default:
		penX -= width / half
	}

	// The baseline goes where Graphviz's own renderers put it: the span's
	// position raised by its centreline offset. yoffset_layout is the
	// ascent of a text-layout plugin's logical rectangle; Graphviz 16's
	// size estimate sets it to the font size (12 left it 0), and adding it
	// lifted every line by one font size, the first out of its box.
	y := r.toY(job, pos.Y()+span.YOffsetCenterLine())

	for _, run := range runs {
		r.ctx.SetFontFace(run.face)
		r.ctx.DrawStringAnchored(run.text, penX, -y, 0, 0)
		penX += advance([]textRun{run})
	}

	return nil
}

func (r *ImageRenderer) Ellipse(_ context.Context, job *Job, points []*PointFloat, filled bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	radiusX := r.toX(job, points[1].X()-points[0].X())
	radiusY := r.toY(job, points[1].Y()-points[0].Y())
	r.ctx.DrawEllipse(r.toX(job, points[0].X()), r.toY(job, -points[0].Y()), radiusX, radiusY)
	r.paint(job, filled)

	return nil
}

func (r *ImageRenderer) Polygon(_ context.Context, job *Job, points []*PointFloat, filled bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.ctx.MoveTo(r.toX(job, points[0].X()), r.toY(job, -points[0].Y()))

	for i := 1; i < len(points); i++ {
		r.ctx.LineTo(r.toX(job, points[i].X()), r.toY(job, -points[i].Y()))
	}

	r.ctx.ClosePath()
	r.paint(job, filled)

	return nil
}

func (r *ImageRenderer) Polyline(_ context.Context, job *Job, points []*PointFloat) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.ctx.MoveTo(r.toX(job, points[0].X()), r.toY(job, -points[0].Y()))

	for i := 1; i < len(points); i++ {
		r.ctx.LineTo(r.toX(job, points[i].X()), r.toY(job, -points[i].Y()))
	}

	r.paint(job, false)

	return nil
}

func (r *ImageRenderer) BezierCurve(_ context.Context, job *Job, points []*PointFloat, filled bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.ctx.MoveTo(r.toX(job, points[0].X()), r.toY(job, -points[0].Y()))

	for i := 1; i < len(points); i += 3 {
		r.ctx.CubicTo(
			r.toX(job, points[i].X()),
			r.toY(job, -points[i].Y()),
			r.toX(job, points[i+1].X()),
			r.toY(job, -points[i+1].Y()),
			r.toX(job, points[i+2].X()),
			r.toY(job, -points[i+2].Y()),
		)
	}

	r.paint(job, filled)

	return nil
}

// LoadImage draws a node's image into the box Graphviz computed for it,
// which already applies imagescale and imagepos: stretched to the box under
// the page's transform, as Graphviz's Cairo image loader draws it.
func (r *ImageRenderer) LoadImage(_ context.Context, job *Job, shape *UserShape, box *BoxFloat, _ bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	left, top := r.toX(job, box.LL().X()), r.toY(job, -box.UR().Y())
	width := int(math.Round(r.toX(job, box.UR().X()-box.LL().X())))
	height := int(math.Round(r.toY(job, box.UR().Y()-box.LL().Y())))

	if width <= 0 || height <= 0 {
		return nil
	}

	if !withinImageBudget(width, height) {
		return fmt.Errorf("%w: %s drawn at %d by %d pixels", ErrImageTooLarge, shape.Name(), width, height)
	}

	img, err := r.nodeImage(shape.Name(), width, height)
	if err != nil {
		return err
	}

	r.ctx.DrawImage(img, int(math.Round(left)), int(math.Round(top)))

	return nil
}

// nodeImage is the image file name at width by height pixels, decoded and
// scaled once per page however many nodes show it.
func (r *ImageRenderer) nodeImage(name string, width, height int) (image.Image, error) {
	key := fmt.Sprintf("%s|%dx%d", name, width, height)
	if scaled, ok := r.images[key]; ok {
		return scaled, nil
	}

	decoded, ok := r.images[name]
	if !ok {
		var err error

		decoded, err = decodeImage(name)
		if err != nil {
			return nil, err
		}

		r.images[name] = decoded
	}

	scaled := decoded
	if bounds := decoded.Bounds(); bounds.Dx() != width || bounds.Dy() != height {
		scaled = resizeLanczos(decoded, width, height)
	}

	r.images[key] = scaled

	return scaled, nil
}

// decodeImage reads and decodes an image file through the file system
// Graphviz sees, once its header has shown it within MaxImagePixels: a
// small file can declare dimensions whose decoding would take gigabytes.
func decodeImage(name string) (image.Image, error) {
	data, err := readImageFile(name)
	if err != nil {
		return nil, err
	}

	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding image %s: %w", name, err)
	}

	if !withinImageBudget(config.Width, config.Height) {
		return nil, fmt.Errorf("%w: %s is %d by %d pixels", ErrImageTooLarge, name, config.Width, config.Height)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding image %s: %w", name, err)
	}

	return img, nil
}

// maxImageFileBytes is the most an image file within MaxImagePixels takes:
// four bytes a pixel, an uncompressed 32-bit BMP, and room for headers.
const maxImageFileBytes = 4*MaxImagePixels + 1<<20

// readImageFile reads an image file through the file system Graphviz sees,
// no further than maxImageFileBytes: a larger file holds no image the
// renderer would draw.
func readImageFile(name string) ([]byte, error) {
	file, err := wasm.FileSystem().Open(name)
	if err != nil {
		return nil, fmt.Errorf("opening image %s: %w", name, err)
	}

	defer func() { _ = file.Close() }()

	data, err := readAtMost(file, maxImageFileBytes)
	if err != nil {
		return nil, fmt.Errorf("reading image %s: %w", name, err)
	}

	return data, nil
}

// readAtMost reads r to its end, failing with ErrImageTooLarge, after
// reading one byte more, when it holds more than limit bytes.
func readAtMost(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("reading: %w", err)
	}

	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: over %d bytes", ErrImageTooLarge, limit)
	}

	return data, nil
}

// withinImageBudget reports whether width by height pixels is at most
// MaxImagePixels, without overflowing.
func withinImageBudget(width, height int) bool {
	return width > 0 && height > 0 && width <= MaxImagePixels/height
}

// spanFace is the face a span is drawn with: the font loader's, when one
// is set and answers, otherwise the installed or embedded font its name
// resolves to, at size points and dpi dots per inch.
func (*ImageRenderer) spanFace(
	ctx context.Context,
	job *Job,
	textFont *TextFont,
	size, dpi float64,
) (font.Face, error) {
	fontLoaderMu.RLock()

	loader := fontLoader

	fontLoaderMu.RUnlock()

	if loader != nil {
		face, err := loader(ctx, job, textFont)
		if err != nil {
			return nil, err
		}

		if face != nil {
			return face, nil
		}
	}

	loaded, err := fontFor(textFont)
	if err != nil {
		return nil, err
	}

	return loaded.face(size, dpi)
}

// resolution is the job's horizontal resolution, or the device default
// when Graphviz left it unset: a face built without one has x/image's
// scale size × DPI ÷ 72 of zero, and draws nothing.
func resolution(dpi *PointFloat) float64 {
	if dpi.X() <= 0 {
		return defaultDeviceDPI
	}

	return dpi.X()
}

// page is the finished page: the canvas, turned back for a landscape page.
func (r *ImageRenderer) page() *image.RGBA {
	canvas := imageRGBA(r.ctx.Image())
	if !r.rotated {
		return canvas
	}

	bounds := canvas.Bounds()
	width, height := bounds.Dy(), bounds.Dx()
	turned := image.NewRGBA(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			from := canvas.PixOffset(bounds.Min.X+height-1-y, bounds.Min.Y+x)
			to := turned.PixOffset(x, y)
			copy(turned.Pix[to:to+4], canvas.Pix[from:from+4])
		}
	}

	return turned
}

// imageRGBA returns img as an *image.RGBA, copying it only when it is some
// other type.
func imageRGBA(img image.Image) *image.RGBA {
	if rgba, ok := img.(*image.RGBA); ok {
		return rgba
	}

	rgba := image.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Bounds(), img, img.Bounds().Min, draw.Src)

	return rgba
}

// paint finishes the current path the way Graphviz's renderers do: the fill
// colour inside when the shape is filled, then the pen along the edge in
// the pen colour, width and style, unless the pen is none. Both colours
// carry their alpha.
func (r *ImageRenderer) paint(job *Job, filled bool) {
	object := job.Object()

	if filled {
		r.setColor(object.FillColor())
		r.ctx.FillPreserve()
	}

	if object.Pen() == PenNone {
		r.ctx.ClearPath()

		return
	}

	r.setPenStyle(job)
	r.setColor(object.PenColor())
	r.ctx.Stroke()
}

// toX and toY take a length in points along the canvas's axes to pixels.
func (r *ImageRenderer) toX(_ *Job, x float64) float64 {
	return r.scaleX * x
}

func (r *ImageRenderer) toY(_ *Job, y float64) float64 {
	return r.scaleY * y
}

// setColor sets the drawing colour from Graphviz's 8-bit channels, alpha
// included.
func (r *ImageRenderer) setColor(color *Color) {
	rgba := color.RGBAUint()
	r.ctx.SetRGBA(
		float64(rgba[0])/colorChannelMax,
		float64(rgba[1])/colorChannelMax,
		float64(rgba[2])/colorChannelMax,
		float64(rgba[3])/colorChannelMax,
	)
}

func (*ImageRenderer) isPNG(job *Job) bool {
	return job.OutputLangName() == pngFormat
}

func (*ImageRenderer) isJPG(job *Job) bool {
	return job.OutputLangName() == "jpg"
}

func (r *ImageRenderer) setPenStyle(job *Job) {
	object := job.Object()
	switch object.Pen() {
	case PenDashed:
		r.ctx.SetDash(dashLength * r.lineScale)
	case PenDotted:
		r.ctx.SetDash(dotLength*r.lineScale, dashLength*r.lineScale)
	case PenSolid, PenNone:
	}

	r.ctx.SetLineWidth(object.PenWidth() * r.lineScale)
}

type FontLoader func(ctx context.Context, job *Job, textFont *TextFont) (font.Face, error)

var (
	fontLoaderMu sync.RWMutex
	fontLoader   FontLoader
)

func SetFontLoader(loader FontLoader) {
	fontLoaderMu.Lock()
	defer fontLoaderMu.Unlock()

	fontLoader = loader
}

// lanczos3 is the Lanczos kernel with a support of three pixels, the filter
// the renderer has always used for node images; x/image/draw ships only the
// box, bilinear and Catmull-Rom interpolators, so the kernel is spelled out.
var lanczos3 = &draw.Kernel{
	Support: lanczosLobes,
	At: func(offset float64) float64 {
		if offset < 0 {
			offset = -offset
		}

		if offset >= lanczosLobes {
			return 0
		}

		if offset == 0 {
			return 1
		}

		x := math.Pi * offset

		return lanczosLobes * math.Sin(x) * math.Sin(x/lanczosLobes) / (x * x)
	},
}

// resizeLanczos scales img to width×height with Lanczos resampling and no
// aspect-ratio preservation, as the previous imaging.Resize call did. A
// non-positive dimension yields an empty image, as it did before.
func resizeLanczos(img image.Image, width, height int) image.Image {
	if width <= 0 || height <= 0 {
		return image.NewNRGBA(image.Rect(0, 0, 0, 0))
	}

	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	lanczos3.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)

	return dst
}
