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

func (r *ImageRenderer) toX(job *Job, x float64) float64 {
	return job.Scale().X() * x
}

func (r *ImageRenderer) toY(job *Job, y float64) float64 {
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

func (r *ImageRenderer) BeginPage(ctx context.Context, job *Job) error {
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

func (r *ImageRenderer) isPNG(job *Job) bool {
	return job.OutputLangName() == "png"
}

func (r *ImageRenderer) isJPG(job *Job) bool {
	return job.OutputLangName() == "jpg"
}

func (r *ImageRenderer) encodeJPG(w io.Writer) error {
	return jpeg.Encode(w, r.ctx.Image(), &jpeg.Options{
		Quality: jpeg.DefaultQuality,
	})
}

func (r *ImageRenderer) saveJPG(path string) error {
	// #nosec G304 -- the output file is the one the render job names
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return r.encodeJPG(file)
}

func (r *ImageRenderer) setPenStyle(job *Job) {
	o := job.Object()
	switch o.Pen() {
	case PenDashed:
		r.ctx.SetDash(dashLength)
	case PenDotted:
		r.ctx.SetDash(dotLength, dashLength)
	case PenSolid, PenNone:
	}

	r.ctx.SetLineWidth(o.PenWidth())
}

func (r *ImageRenderer) EndPage(ctx context.Context, job *Job) error {
	var buf bytes.Buffer

	switch {
	case r.isPNG(job):
		if err := r.ctx.EncodePNG(&buf); err != nil {
			return err
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
				return err
			}
		case r.isJPG(job):
			if err := r.saveJPG(filename); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *ImageRenderer) TextSpan(ctx context.Context, job *Job, p *PointFloat, span *TextSpan) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	rgba := job.Object().PenColor().RGBAUint()
	r.setRGB(rgba)

	font := span.Font()

	face, err := r.getFontFace(ctx, job, font)
	if face == nil || err != nil {
		defaultFont, err := r.defaultFontFace(job, font)
		if err != nil {
			return err
		}

		face = defaultFont
	}

	p.SetX(r.toX(job, p.X()))

	switch span.Just() {
	case 'r':
		p.SetX(p.X() - r.toX(job, span.Size().X()))
	case 'l':
		// skip
	case 'n':
		p.SetX(p.X() - r.toX(job, span.Size().X()/half))
	}

	r.ctx.SetFontFace(face)
	y := r.toY(job, p.Y()+span.YOffsetCenterLine()+span.YOffsetLayout())
	r.ctx.DrawStringAnchored(span.Text(), p.X(), -y, 0, 0)

	return nil
}

func (r *ImageRenderer) getFontFace(ctx context.Context, job *Job, font *TextFont) (font.Face, error) {
	return r.lookupFontWithCache(ctx, job, font)
}

func (r *ImageRenderer) lookupFontWithCache(ctx context.Context, job *Job, font *TextFont) (font.Face, error) {
	fontSize := font.Size() * job.Zoom()
	fontName := font.Name()
	cacheKey := fmt.Sprintf("%s:%f", fontName, fontSize)

	fontMu.RLock()

	if font, exists := fontCache[cacheKey]; exists {
		fontMu.RUnlock()
		return font, nil
	}

	fontMu.RUnlock()

	fontLoaderMu.RLock()
	defer fontLoaderMu.RUnlock()

	if fontLoader != nil {
		face, err := fontLoader(ctx, job, font)
		if err != nil {
			return nil, err
		}

		if face != nil {
			return face, nil
		}
	}

	ft, err := r.lookupFont(fontName, fontSize, job.DPI())
	if err != nil {
		return nil, err
	}

	fontMu.Lock()
	fontCache[cacheKey] = ft
	fontMu.Unlock()

	return ft, nil
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
		if err != nil {
			return nil, err
		}

		if ttfFace != nil {
			return ttfFace, nil
		}

		ttcFace, err := r.lookupFontFromTTCFile(fontName, fontSize, dpi, baseName+".ttc")
		if err != nil {
			return nil, err
		}

		if ttcFace != nil {
			return ttcFace, nil
		}
	}

	return nil, fmt.Errorf("%w: %s", ErrFontNotFound, fontName)
}

func (r *ImageRenderer) lookupFontFromTTFFile(fontSize float64, fontPath string) (font.Face, error) {
	// #nosec G304 -- a font file found in the platform font directories or named by the font loader
	fontData, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, nil
	}

	ft, err := truetype.Parse(fontData)
	if err != nil {
		return nil, err
	}

	return truetype.NewFace(ft, &truetype.Options{
		Size: fontSize,
	}), nil
}

func (r *ImageRenderer) lookupFontFromTTCFile(
	fontName string,
	fontSize float64,
	dpi *PointFloat,
	fontPath string,
) (font.Face, error) {
	parts := strings.Split(fontName, "-")

	fontPath, err := findFont(fontPath)
	if err != nil {
		return nil, nil
	}

	// #nosec G304 -- a font file found in the platform font directories or named by the font loader
	fontData, err := os.ReadFile(fontPath)
	if err != nil {
		return nil, err
	}

	c, err := opentype.ParseCollection(fontData)
	if err != nil {
		return nil, err
	}

	for j := range c.NumFonts() {
		ft, err := c.Font(j)
		if err != nil {
			return nil, err
		}

		var buf sfnt.Buffer

		name, err := ft.Name(&buf, sfnt.NameIDFull)
		if err != nil {
			return nil, err
		}

		if strings.Join(parts, " ") == name {
			return opentype.NewFace(ft, &opentype.FaceOptions{
				Size: fontSize,
				DPI:  dpi.X(),
			})
		}
	}

	return nil, fmt.Errorf("%w: %s in %s", ErrFontNotFound, fontName, fontPath)
}

func (r *ImageRenderer) defaultFontFace(job *Job, font *TextFont) (font.Face, error) {
	ft, err := truetype.Parse(goregular.TTF)
	if err != nil {
		return nil, err
	}

	return truetype.NewFace(ft, &truetype.Options{
		Size: font.Size() * job.Zoom(),
	}), nil
}

func (r *ImageRenderer) Ellipse(ctx context.Context, job *Job, p []*PointFloat, filled bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.setPenStyle(job)
	rx := r.toX(job, p[1].X()-p[0].X())
	ry := r.toY(job, p[1].Y()-p[0].Y())

	var c *Color
	if filled {
		c = job.Object().FillColor()

		r.ctx.FillPreserve()
	} else {
		c = job.Object().PenColor()
	}

	rgba := c.RGBAUint()
	r.setRGB(rgba)
	r.ctx.DrawEllipse(r.toX(job, p[0].X()), r.toY(job, -p[0].Y()), rx, ry)

	if filled {
		r.ctx.Fill()
	} else {
		r.ctx.Stroke()
	}

	return nil
}

func (r *ImageRenderer) Polygon(ctx context.Context, job *Job, a []*PointFloat, filled bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.setPenStyle(job)

	var c *Color
	if filled {
		c = job.Object().FillColor()
	} else {
		c = job.Object().PenColor()
	}

	rgba := c.RGBAUint()
	r.setRGB(rgba)
	r.ctx.MoveTo(r.toX(job, a[0].X()), r.toY(job, -a[0].Y()))

	for i := 1; i < len(a); i++ {
		r.ctx.LineTo(r.toX(job, a[i].X()), r.toY(job, -a[i].Y()))
	}

	r.ctx.ClosePath()

	if filled {
		r.ctx.Fill()
	} else {
		r.ctx.Stroke()
	}

	return nil
}

func (r *ImageRenderer) Polyline(ctx context.Context, job *Job, a []*PointFloat) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.setPenStyle(job)
	rgba := job.Object().PenColor().RGBAUint()
	r.setRGB(rgba)
	r.ctx.MoveTo(r.toX(job, a[0].X()), r.toY(job, -a[0].Y()))

	for i := 1; i < len(a); i++ {
		r.ctx.LineTo(r.toX(job, a[i].X()), r.toY(job, -a[i].Y()))
	}

	r.ctx.Stroke()

	return nil
}

func (r *ImageRenderer) BezierCurve(ctx context.Context, job *Job, a []*PointFloat, filled bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	r.setPenStyle(job)

	var c *Color
	if filled {
		c = job.Object().FillColor()

		r.ctx.FillPreserve()
	} else {
		c = job.Object().PenColor()
	}

	rgba := c.RGBAUint()
	r.setRGB(rgba)
	r.ctx.MoveTo(r.toX(job, a[0].X()), r.toY(job, -a[0].Y()))

	for i := 1; i < len(a); i += 3 {
		r.ctx.CubicTo(
			r.toX(job, a[i].X()),
			r.toY(job, -a[i].Y()),
			r.toX(job, a[i+1].X()),
			r.toY(job, -a[i+1].Y()),
			r.toX(job, a[i+2].X()),
			r.toY(job, -a[i+2].Y()),
		)
	}

	if filled {
		r.ctx.Fill()
	} else {
		r.ctx.Stroke()
	}

	return nil
}

const (
	defaultGAP  = 4
	defaultXPAD = 4 * defaultGAP
	defaultYPAD = 2 * defaultGAP
)

func (r *ImageRenderer) LoadImage(ctx context.Context, job *Job, shape *UserShape, bf *BoxFloat, filled bool) error {
	r.ctx.Push()
	defer r.ctx.Pop()

	fs := wasm.FileSystem()

	f, err := fs.Open(shape.Name())
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	io.Copy(&buf, f)

	img, _, err := image.Decode(&buf)
	if err != nil {
		return err
	}

	topLeftX := bf.LL().X()
	topLeftY := bf.LL().Y()

	node := job.Object().Node()
	if node != nil {
		if node.FixedSize() || node.ImageScale() != cgraph.ImageScaleDefault {
			bottomRightX := bf.UR().X()
			bottomRightY := bf.UR().Y()
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

type FontLoader func(ctx context.Context, job *Job, font *TextFont) (font.Face, error)

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
	At: func(t float64) float64 {
		if t < 0 {
			t = -t
		}

		if t >= lanczosLobes {
			return 0
		}

		if t == 0 {
			return 1
		}

		x := math.Pi * t

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
