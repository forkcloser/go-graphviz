package gvc

import (
	"image"
	"image/draw"
	"math"
)

// maxWeightTable is the most weights an axis keeps for all the pixels it
// draws. Past it they are worked out again for each line, of which there
// are then few: a line that long is most of the image.
const maxWeightTable = 1 << 22

// lanczos is the Lanczos kernel of lanczosLobes lobes at offset pixels, the
// filter node images have always been resampled with here.
func lanczos(offset float64) float64 {
	offset = math.Abs(offset)

	switch {
	case offset >= lanczosLobes:
		return 0
	case offset == 0:
		return 1
	}

	x := math.Pi * offset

	return lanczosLobes * math.Sin(x) * math.Sin(x/lanczosLobes) / (x * x)
}

// axis resamples along one direction: read pixels to drawn pixels.
type axis struct {
	read, drawn int
	// table holds the weights of every pixel drawn, when they are no more
	// than maxWeightTable: those of pixel i are table[from[i]:from[i+1]],
	// for the pixels read from first[i] on.
	first, from []int
	table       []float32
	scratch     []float32
}

func newAxis(read, drawn int) *axis {
	scaled := &axis{read: read, drawn: drawn}

	each := 2*math.Ceil(lanczosLobes*math.Max(1, float64(read)/float64(drawn))) + 1
	if float64(drawn)*each > maxWeightTable {
		return scaled
	}

	scaled.first, scaled.from = make([]int, drawn), make([]int, drawn+1)
	scaled.table = make([]float32, 0, int(float64(drawn)*each))

	for i := range drawn {
		first, weights := scaled.weights(i)
		scaled.first[i], scaled.table, scaled.from[i+1] = first, append(
			scaled.table,
			weights...,
		), len(
			scaled.table,
		)+len(
			weights,
		)
	}

	return scaled
}

// weights is the pixels read for pixel i drawn, as the first of them and
// the weight of each, the weights summing to one: those within the
// kernel's lobes of its centre, the lobes as wide as the scale when the
// image shrinks, as golang.org/x/image/draw takes them. The weights are
// the axis's until its next call.
func (a *axis) weights(i int) (int, []float32) {
	scale := float64(a.read) / float64(a.drawn)
	spread := math.Max(1, scale)
	centre := (float64(i)+pixelCentre)*scale - pixelCentre
	first := max(0, int(math.Floor(centre-lanczosLobes*spread)))
	last := min(a.read, int(math.Ceil(centre+lanczosLobes*spread)))

	weights, total := a.scratch[:0], 0.0

	for k := first; k < last; k++ {
		weight := lanczos((centre - float64(k)) / spread)
		weights, total = append(weights, float32(weight)), total+weight
	}

	for n := range weights {
		weights[n] = float32(float64(weights[n]) / total)
	}

	a.scratch = weights

	return first, weights
}

// at is weights, from the table when there is one.
func (a *axis) at(i int) (int, []float32) {
	if a.table != nil {
		return a.first[i], a.table[a.from[i]:a.from[i+1]]
	}

	return a.weights(i)
}

// resample is a line of pixels read, as premultiplied RGBA bytes, resampled
// along the axis into resampled: four values for each pixel drawn.
func (a *axis) resample(pixels []uint8, resampled []float32) {
	for i := range a.drawn {
		first, weights := a.at(i)
		out := resampled[rgbaChannels*i : rgbaChannels*i+rgbaChannels]
		clear(out)

		for n, weight := range weights {
			in := pixels[rgbaChannels*(first+n) : rgbaChannels*(first+n)+rgbaChannels]
			for channel := range out {
				out[channel] += weight * float32(in[channel])
			}
		}
	}
}

// lines are the lines of an image along its first axis, each read when it
// is first asked for, resampled along the second axis, and kept until
// forgotten.
type lines struct {
	img image.Image
	// origin is where the first line starts in img, step leads from a line
	// to the next, and line holds the one being read.
	origin, step image.Point
	line         *image.RGBA
	inner        *axis
	// kept is the lines read, resampled, by their place; spare, the buffers
	// of those forgotten.
	kept  map[int][]float32
	spare [][]float32
}

// at is the line at place, resampled.
func (l *lines) at(place int) []float32 {
	if resampled, ok := l.kept[place]; ok {
		return resampled
	}

	resampled := l.buffer()

	draw.Draw(l.line, l.line.Rect, l.img, l.origin.Add(l.step.Mul(place)), draw.Src)
	l.inner.resample(l.line.Pix, resampled)
	l.kept[place] = resampled

	return resampled
}

// buffer is room for a line resampled: that of a line forgotten, or new.
func (l *lines) buffer() []float32 {
	if last := len(l.spare) - 1; last >= 0 {
		spare := l.spare[last]
		l.spare = l.spare[:last]

		return spare
	}

	return make([]float32, rgbaChannels*l.inner.drawn)
}

// forget drops the lines before first: no line drawn will weigh them again.
func (l *lines) forget(first int) {
	for k, resampled := range l.kept {
		if k < first {
			l.spare = append(l.spare, resampled)

			delete(l.kept, k)
		}
	}
}

const (
	// rgbaChannels is the values of a pixel.
	rgbaChannels = 4
	// pixelCentre is where a pixel's centre lies from its edge, and what
	// rounds a value to the nearest byte.
	pixelCentre = 0.5
)

// resizeLanczos scales img to width by height pixels, both positive, with
// Lanczos resampling and no aspect-ratio preservation: along one axis and
// then the other, to the picture golang.org/x/image/draw's Scale gives with
// the same kernel, but a line at a time. Scale keeps every column drawn of
// every row read, 32 bytes each, which neither the image's size nor the
// size it is drawn at bounds: an 18 KB image of 1024 by 8192 pixels drawn
// 1152 by 48 took 332 MiB. Here a line of the first axis is read, resampled
// along the second, and kept only while a line drawn still weighs it; and
// the first axis is the one that leaves less to weigh, rows when the width
// drawn times the rows read is the smaller product. So a resize keeps a few
// lines, takes time in proportion to the pixels read and drawn, and no
// image within the pixel budgets is refused.
func resizeLanczos(img image.Image, width, height int) image.Image {
	bounds := img.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	read := &lines{img: img, origin: bounds.Min, kept: map[int][]float32{}, spare: [][]float32{}}

	// outer is the first axis, and outerStride and innerStride lead from a
	// pixel of dst to the next along each.
	var (
		outer                    *axis
		outerStride, innerStride int
	)

	if float64(width)*float64(bounds.Dy()) <= float64(height)*float64(bounds.Dx()) {
		outer, read.inner = newAxis(bounds.Dy(), height), newAxis(bounds.Dx(), width)
		read.line, read.step = image.NewRGBA(image.Rect(0, 0, bounds.Dx(), 1)), image.Pt(0, 1)
		outerStride, innerStride = dst.Stride, rgbaChannels
	} else {
		outer, read.inner = newAxis(bounds.Dx(), width), newAxis(bounds.Dy(), height)
		read.line, read.step = image.NewRGBA(image.Rect(0, 0, 1, bounds.Dy())), image.Pt(1, 0)
		outerStride, innerStride = rgbaChannels, dst.Stride
	}

	sum := make([]float32, rgbaChannels*read.inner.drawn)

	for place := range outer.drawn {
		first, weights := outer.at(place)

		read.forget(first)
		clear(sum)

		for n, weight := range weights {
			for x, value := range read.at(first + n) {
				sum[x] += weight * value
			}
		}

		for i := range read.inner.drawn {
			straight(dst.Pix[place*outerStride+i*innerStride:], sum[rgbaChannels*i:])
		}
	}

	return dst
}

// straight writes a premultiplied pixel, its four values from 0 to 255 and
// past either where the kernel rings, as the bytes of a pixel that is not.
func straight(pixel []uint8, premultiplied []float32) {
	const opaque = 255

	alpha := min(max(premultiplied[3], 0), opaque)
	if alpha == 0 {
		clear(pixel[:rgbaChannels])

		return
	}

	for channel := range rgbaChannels - 1 {
		value := min(max(premultiplied[channel], 0), alpha)
		pixel[channel] = uint8(value*opaque/alpha + pixelCentre)
	}

	pixel[3] = uint8(alpha + pixelCentre)
}
