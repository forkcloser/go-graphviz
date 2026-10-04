package gvc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math"
	"os"
	"strings"
	"sync"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
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
)

// ErrPageTooLarge is returned by the raster renderer for a page whose width
// or height does not fit the drawing context's int coordinates.
var ErrPageTooLarge = errors.New("page too large for the raster renderer")

type ImageRenderer struct {
	*DefaultRenderEngine
	ctx *gg.Context
}

func (r *ImageRenderer) BeginPage(_ context.Context, job *Job) error {
	width, height := job.Width(), job.Height()
	if width > math.MaxInt32 || height > math.MaxInt32 {
		return fmt.Errorf("%w: %d by %d points", ErrPageTooLarge, width, height)
	}

	gctx := gg.NewContext(int(width), int(height))
	translation := job.Translation()
	gctx.Translate(r.toX(job, translation.X()), r.toY(job, -translation.Y()))
	r.ctx = gctx

	return nil
}

func (r *ImageRenderer) EndPage(_ context.Context, job *Job) error {
	var buf bytes.Buffer

	switch {
	case r.isPNG(job):
		if err := r.ctx.EncodePNG(&buf); err != nil {
			return fmt.Errorf("encoding the page as PNG: %w", err)
		}
	case r.isJPG(job):
		if err := r.encodeJPG(&buf); err != nil {
			return err
		}
	}

	job.SetOutputData(buf.Bytes())
	job.SetOutputDataPosition(uint(len(buf.Bytes())))

	filename := job.OutputFileName()
	if filename != "" {
		switch {
		case r.isPNG(job):
			if err := r.ctx.SavePNG(filename); err != nil {
				return fmt.Errorf("writing %s: %w", filename, err)
			}
		case r.isJPG(job):
			if err := r.saveJPG(filename); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ImageRenderer) TextSpan(ctx context.Context, job *Job, pos *PointFloat, span *TextSpan) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	rgba := job.Object().PenColor().RGBAUint()
	r.setRGB(rgba)

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
	y := r.toY(job, pos.Y()+span.YOffsetCenterLine()+span.YOffsetLayout())
	r.ctx.DrawStringAnchored(span.Text(), pos.X(), -y, 0, 0)

	return nil
}

func (r *ImageRenderer) Ellipse(_ context.Context, job *Job, points []*PointFloat, filled bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.setPenStyle(job)
	radiusX := r.toX(job, points[1].X()-points[0].X())
	radiusY := r.toY(job, points[1].Y()-points[0].Y())

	var paint *Color
	if filled {
		paint = job.Object().FillColor()

		r.ctx.FillPreserve()
	} else {
		paint = job.Object().PenColor()
	}

	rgba := paint.RGBAUint()
	r.setRGB(rgba)
	r.ctx.DrawEllipse(r.toX(job, points[0].X()), r.toY(job, -points[0].Y()), radiusX, radiusY)

	if filled {
		r.ctx.Fill()
	} else {
		r.ctx.Stroke()
	}

	return nil
}

func (r *ImageRenderer) Polygon(_ context.Context, job *Job, points []*PointFloat, filled bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.setPenStyle(job)

	var paint *Color
	if filled {
		paint = job.Object().FillColor()
	} else {
		paint = job.Object().PenColor()
	}

	rgba := paint.RGBAUint()
	r.setRGB(rgba)
	r.ctx.MoveTo(r.toX(job, points[0].X()), r.toY(job, -points[0].Y()))

	for i := 1; i < len(points); i++ {
		r.ctx.LineTo(r.toX(job, points[i].X()), r.toY(job, -points[i].Y()))
	}

	r.ctx.ClosePath()

	if filled {
		r.ctx.Fill()
	} else {
		r.ctx.Stroke()
	}

	return nil
}

func (r *ImageRenderer) Polyline(_ context.Context, job *Job, points []*PointFloat) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.setPenStyle(job)
	rgba := job.Object().PenColor().RGBAUint()
	r.setRGB(rgba)
	r.ctx.MoveTo(r.toX(job, points[0].X()), r.toY(job, -points[0].Y()))

	for i := 1; i < len(points); i++ {
		r.ctx.LineTo(r.toX(job, points[i].X()), r.toY(job, -points[i].Y()))
	}

	r.ctx.Stroke()

	return nil
}

func (r *ImageRenderer) BezierCurve(_ context.Context, job *Job, points []*PointFloat, filled bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.setPenStyle(job)

	var paint *Color
	if filled {
		paint = job.Object().FillColor()

		r.ctx.FillPreserve()
	} else {
		paint = job.Object().PenColor()
	}

	rgba := paint.RGBAUint()
	r.setRGB(rgba)
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

	if filled {
		r.ctx.Fill()
	} else {
		r.ctx.Stroke()
	}

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

	var buf bytes.Buffer
	io.Copy(&buf, file)

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
			posX := (topLeftX + xPAD) * job.Scale().X()
			posY := (topLeftY + yPAD) * job.Scale().Y()
			r.ctx.DrawImageAnchored(img, int(posX), -int(posY), 0, 1)

			return nil
		}
	}

	posX := topLeftX * job.Scale().X()
	posY := topLeftY * job.Scale().Y()
	r.ctx.DrawImageAnchored(img, int(posX), -int(posY), 0, 1)

	return nil
}

func (*ImageRenderer) toX(job *Job, x float64) float64 {
	return job.Scale().X() * x
}

func (*ImageRenderer) toY(job *Job, y float64) float64 {
	return job.Scale().Y() * y
}

// setRGB sets the drawing colour from Graphviz's 8-bit channels.
func (r *ImageRenderer) setRGB(rgba [4]uint) {
	r.ctx.SetRGB(
		float64(rgba[0])/colorChannelMax,
		float64(rgba[1])/colorChannelMax,
		float64(rgba[2])/colorChannelMax,
	)
}

func (*ImageRenderer) isPNG(job *Job) bool {
	return job.OutputLangName() == "png"
}

func (*ImageRenderer) isJPG(job *Job) bool {
	return job.OutputLangName() == "jpg"
}

func (r *ImageRenderer) encodeJPG(w io.Writer) error {
	if err := jpeg.Encode(w, r.ctx.Image(), &jpeg.Options{Quality: jpeg.DefaultQuality}); err != nil {
		return fmt.Errorf("encoding the page as JPEG: %w", err)
	}

	return nil
}

func (r *ImageRenderer) saveJPG(path string) error {
	// #nosec G304 -- the output file is the one the render job names
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer file.Close()

	return r.encodeJPG(file)
}

func (r *ImageRenderer) setPenStyle(job *Job) {
	object := job.Object()
	switch object.Pen() {
	case PenDashed:
		r.ctx.SetDash(dashLength)
	case PenDotted:
		r.ctx.SetDash(dotLength, dashLength)
	case PenSolid, PenNone:
	}

	r.ctx.SetLineWidth(object.PenWidth())
}

func (r *ImageRenderer) getFontFace(ctx context.Context, job *Job, textFont *TextFont) (font.Face, error) {
	return r.lookupFontWithCache(ctx, job, textFont)
}

func (r *ImageRenderer) lookupFontWithCache(ctx context.Context, job *Job, textFont *TextFont) (font.Face, error) {
	fontSize := textFont.Size() * job.Zoom()
	fontName := textFont.Name()
	cacheKey := fmt.Sprintf("%s:%f", fontName, fontSize)

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
		return r.lookupFontFromTTFFile(fontSize, fontPath)
	}

	parts := strings.Split(fontName, "-")
	for i := len(parts) - 1; i > 0; i-- {
		baseName := strings.Join(parts[:len(parts)-1], "-")

		ttfFace, err := r.lookupFontFromTTFFile(fontSize, baseName+".ttf")
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

func (*ImageRenderer) lookupFontFromTTFFile(fontSize float64, fontPath string) (font.Face, error) {
	// #nosec G304 -- a font file found in the platform font directories or named by the font loader
	fontData, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFontNotFound, fontPath)
	}

	ft, err := truetype.Parse(fontData)
	if err != nil {
		return nil, fmt.Errorf("parsing TrueType font %s: %w", fontPath, err)
	}

	return truetype.NewFace(ft, &truetype.Options{
		Size: fontSize,
	}), nil
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
			face, err := opentype.NewFace(member, &opentype.FaceOptions{
				Size: fontSize,
				DPI:  dpi.X(),
			})
			if err != nil {
				return nil, fmt.Errorf("face for %s from collection %s: %w", fontName, fontPath, err)
			}

			return face, nil
		}
	}

	return nil, fmt.Errorf("%w: %s in %s", ErrFontNotFound, fontName, fontPath)
}

func (*ImageRenderer) defaultFontFace(job *Job, textFont *TextFont) (font.Face, error) {
	ft, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return nil, fmt.Errorf("parsing the embedded Go Regular font: %w", err)
	}

	return truetype.NewFace(ft, &truetype.Options{
		Size: textFont.Size() * job.Zoom(),
	}), nil
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
