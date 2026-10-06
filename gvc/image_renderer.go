package gvc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/fogleman/gg"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"

	"github.com/forkcloser/go-graphviz/cgraph"
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

var (
	fontMu    sync.RWMutex
	fontCache = make(map[string]font.Face)
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

	textFont := span.Font()

	face, err := r.getFontFace(ctx, job, textFont)
	if face == nil || err != nil {
		defaultFont, err := r.defaultFontFace(job, textFont)
		if err != nil {
			return err
		}

		face = defaultFont
	}

	pos.SetX(r.toX(job, pos.X()))

	switch span.Just() {
	case 'r':
		pos.SetX(pos.X() - r.toX(job, span.Size().X()))
	case 'l':
		// skip
	case 'n':
		pos.SetX(pos.X() - r.toX(job, span.Size().X()/half))
	}

	r.ctx.SetFontFace(face)
	// The baseline goes where Graphviz's own renderers put it: the span's
	// position raised by its centreline offset. yoffset_layout is the
	// ascent of a text-layout plugin's logical rectangle; Graphviz 16's
	// size estimate sets it to the font size (12 left it 0), and adding it
	// lifted every line by one font size, the first out of its box.
	y := r.toY(job, pos.Y()+span.YOffsetCenterLine())
	r.ctx.DrawStringAnchored(span.Text(), pos.X(), -y, 0, 0)

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

func (r *ImageRenderer) LoadImage(_ context.Context, job *Job, shape *UserShape, box *BoxFloat, _ bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	fs := wasm.FileSystem()

	file, err := fs.Open(shape.Name())
	if err != nil {
		return fmt.Errorf("opening image %s: %w", shape.Name(), err)
	}
	defer file.Close()

	var buf bytes.Buffer
	if _, copyErr := io.Copy(&buf, file); copyErr != nil {
		return fmt.Errorf("reading image %s: %w", shape.Name(), copyErr)
	}

	img, _, err := image.Decode(&buf)
	if err != nil {
		return fmt.Errorf("decoding image %s: %w", shape.Name(), err)
	}

	topLeftX := box.LL().X()
	topLeftY := box.LL().Y()

	node := job.Object().Node()
	if node != nil {
		if node.FixedSize() || node.ImageScale() != cgraph.ImageScaleDefault {
			bottomRightX := box.UR().X()
			bottomRightY := box.UR().Y()
			width := bottomRightX - topLeftX
			height := bottomRightY - topLeftY
			img = resizeLanczos(img, int(width), int(height))
			xPAD := defaultXPAD / half
			yPAD := defaultYPAD / half
			posX := r.toX(job, topLeftX+xPAD)
			posY := r.toY(job, topLeftY+yPAD)
			r.ctx.DrawImageAnchored(img, int(posX), -int(posY), 0, 1)

			return nil
		}
	}

	posX := r.toX(job, topLeftX)
	posY := r.toY(job, topLeftY)
	r.ctx.DrawImageAnchored(img, int(posX), -int(posY), 0, 1)

	return nil
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
	return job.OutputLangName() == "png"
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

func (r *ImageRenderer) getFontFace(ctx context.Context, job *Job, textFont *TextFont) (font.Face, error) {
	return r.lookupFontWithCache(ctx, job, textFont)
}

func (r *ImageRenderer) lookupFontWithCache(ctx context.Context, job *Job, textFont *TextFont) (font.Face, error) {
	fontSize := textFont.Size() * job.Zoom()
	fontName := textFont.Name()
	cacheKey := fmt.Sprintf("%s:%f:%f", fontName, fontSize, job.DPI().X())

	fontMu.RLock()

	if cached, exists := fontCache[cacheKey]; exists {
		fontMu.RUnlock()
		return cached, nil
	}

	fontMu.RUnlock()

	fontLoaderMu.RLock()
	defer fontLoaderMu.RUnlock()

	if fontLoader != nil {
		face, err := fontLoader(ctx, job, textFont)
		if err != nil {
			return nil, err
		}

		if face != nil {
			return face, nil
		}
	}

	face, err := r.lookupFont(fontName, fontSize, job.DPI())
	if err != nil {
		return nil, err
	}

	fontMu.Lock()
	fontCache[cacheKey] = face
	fontMu.Unlock()

	return face, nil
}

func (r *ImageRenderer) lookupFont(fontName string, fontSize float64, dpi *PointFloat) (font.Face, error) {
	fontPath, err := findFont(fontName)
	if err == nil {
		return r.lookupFontFromTTFFile(fontSize, dpi, fontPath)
	}

	// "Helvetica-Bold-Oblique" is tried as Helvetica-Bold, then Helvetica.
	parts := strings.Split(fontName, "-")
	for i := len(parts) - 1; i > 0; i-- {
		baseName := strings.Join(parts[:i], "-")

		ttfFace, err := r.lookupFontFromTTFFile(fontSize, dpi, baseName+".ttf")
		if err == nil {
			return ttfFace, nil
		}

		if !errors.Is(err, ErrFontNotFound) {
			return nil, err
		}

		ttcFace, err := r.lookupFontFromTTCFile(fontName, fontSize, dpi, baseName+".ttc")
		if err == nil {
			return ttcFace, nil
		}

		if !errors.Is(err, ErrFontNotFound) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrFontNotFound, fontName)
}

// faceOptions sizes a face for a job: the font size in points at the job's
// resolution, the scale Graphviz lays the page out at. A face built
// without a DPI has x/image's scale size × DPI ÷ 72 of zero, and draws
// nothing; freetype, which the renderer used before, defaulted to 72.
func faceOptions(size float64, dpi *PointFloat) *opentype.FaceOptions {
	resolution := dpi.X()
	if resolution <= 0 {
		resolution = defaultDeviceDPI
	}

	return &opentype.FaceOptions{Size: size, DPI: resolution}
}

func (*ImageRenderer) lookupFontFromTTFFile(fontSize float64, dpi *PointFloat, fontPath string) (font.Face, error) {
	// #nosec G304 -- a font file found in the platform font directories or named by the font loader
	fontData, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFontNotFound, fontPath)
	}

	ft, err := opentype.Parse(fontData)
	if err != nil {
		return nil, fmt.Errorf("parsing font %s: %w", fontPath, err)
	}

	face, err := opentype.NewFace(ft, faceOptions(fontSize, dpi))
	if err != nil {
		return nil, fmt.Errorf("face for %s: %w", fontPath, err)
	}

	return face, nil
}

func (*ImageRenderer) lookupFontFromTTCFile(
	fontName string,
	fontSize float64,
	dpi *PointFloat,
	fontPath string,
) (font.Face, error) {
	parts := strings.Split(fontName, "-")

	fontPath, err := findFont(fontPath)
	if err != nil {
		return nil, err // already ErrFontNotFound with the name
	}

	// #nosec G304 -- a font file found in the platform font directories or named by the font loader
	fontData, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, fmt.Errorf("reading font collection %s: %w", fontPath, err)
	}

	collection, err := opentype.ParseCollection(fontData)
	if err != nil {
		return nil, fmt.Errorf("parsing font collection %s: %w", fontPath, err)
	}

	for index := range collection.NumFonts() {
		member, err := collection.Font(index)
		if err != nil {
			return nil, fmt.Errorf("font %d of collection %s: %w", index, fontPath, err)
		}

		var buf sfnt.Buffer

		name, err := member.Name(&buf, sfnt.NameIDFull)
		if err != nil {
			return nil, fmt.Errorf("name of font %d of collection %s: %w", index, fontPath, err)
		}

		if strings.Join(parts, " ") == name {
			face, err := opentype.NewFace(member, faceOptions(fontSize, dpi))
			if err != nil {
				return nil, fmt.Errorf("face for %s from collection %s: %w", fontName, fontPath, err)
			}

			return face, nil
		}
	}

	return nil, fmt.Errorf("%w: %s in %s", ErrFontNotFound, fontName, fontPath)
}

// The embedded Go Regular font, the face every unresolved font name falls
// back to, parsed once; its faces are cached by size like any other.
var (
	goRegularOnce sync.Once
	goRegular     *sfnt.Font
	errGoRegular  error
)

func (*ImageRenderer) defaultFontFace(job *Job, textFont *TextFont) (font.Face, error) {
	goRegularOnce.Do(func() {
		goRegular, errGoRegular = opentype.Parse(goregular.TTF)
	})

	if errGoRegular != nil {
		return nil, fmt.Errorf("parsing the embedded Go Regular font: %w", errGoRegular)
	}

	fontSize := textFont.Size() * job.Zoom()
	cacheKey := fmt.Sprintf("goregular:%f:%f", fontSize, job.DPI().X())

	fontMu.RLock()

	if cached, exists := fontCache[cacheKey]; exists {
		fontMu.RUnlock()

		return cached, nil
	}

	fontMu.RUnlock()

	face, err := opentype.NewFace(goRegular, faceOptions(fontSize, job.DPI()))
	if err != nil {
		return nil, fmt.Errorf("face for the embedded Go Regular font: %w", err)
	}

	fontMu.Lock()
	fontCache[cacheKey] = face
	fontMu.Unlock()

	return face, nil
}

const (
	defaultGAP  = 4
	defaultXPAD = 4 * defaultGAP
	defaultYPAD = 2 * defaultGAP
)

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
