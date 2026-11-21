package gvc

import (
	"image"
	"image/color"
	"runtime"
	"testing"

	"golang.org/x/image/draw"
)

// scaled is img resized by golang.org/x/image/draw's Scale with the same
// kernel: the picture resizeLanczos is held to.
func scaled(img image.Image, width, height int) *image.NRGBA {
	kernel := &draw.Kernel{Support: lanczosLobes, At: lanczos}
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	kernel.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)

	return dst
}

// patterned is an image of colours and alphas that change from pixel to
// pixel, its bounds off the origin.
func patterned(width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(7, 3, 7+width, 3+height))

	for y := range height {
		for x := range width {
			img.SetNRGBA(7+x, 3+y, color.NRGBA{
				R: uint8(x * 255 / width), G: uint8(y * 255 / height),
				B: uint8((x*7 + y*13) % 256), A: uint8(128 + (x*3+y*5)%128),
			})
		}
	}

	return img
}

// shaded is a grey image, light to dark from left to right and top to
// bottom.
func shaded(width, height int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, width, height))

	for y := range height {
		for x := range width {
			img.SetGray(x, y, color.Gray{Y: uint8(255 - 255*(x+y)/(width+height))})
		}
	}

	return img
}

// A resize is the picture x/image's Scale gives: shrunk, grown, and each
// along one axis, rows first and columns first, from an image with alpha
// and from a grey one, every channel of every pixel is within two levels
// of 255, the rounding of bytes where Scale keeps sixteen bits.
func TestResizeIsScale(t *testing.T) {
	for name, img := range map[string]image.Image{"colours and alphas": patterned(300, 200), "grey": shaded(300, 200)} {
		for _, size := range []image.Point{
			{97, 41}, {500, 333}, {300, 20}, {40, 200}, {450, 60}, {60, 450}, {300, 200}, {1, 1}, {900, 7}, {7, 900},
		} {
			got, want := resizeLanczos(img, size.X, size.Y), scaled(img, size.X, size.Y)

			nrgba, ok := got.(*image.NRGBA)
			if !ok || nrgba.Rect != want.Rect {
				t.Fatalf("%s to %v: a %T of %v", name, size, got, got.Bounds())
			}

			worst := 0
			for i := range want.Pix {
				worst = max(worst, int(nrgba.Pix[i])-int(want.Pix[i]), int(want.Pix[i])-int(nrgba.Pix[i]))
			}

			if worst > 2 {
				t.Errorf("%s to %v: a channel is %d levels from Scale's", name, size, worst)
			}
		}
	}
}

// A resize keeps a few lines, not every column drawn of every row read. An
// image 64 by 140000 drawn 64 by 100, for which Scale keeps 287 MB, is
// resized in under 8 MiB, to the picture its rows average to; and one 1500
// square drawn 700 square, whose rows read are forgotten as the rows drawn
// pass them, in under 8 MiB where keeping them all is 17 MB.
func TestResizeKeepsAFewLines(t *testing.T) {
	const width, height, rows = 64, 140000, 100

	tall := image.NewGray(image.Rect(0, 0, width, height))
	for y := range height {
		// Light above the middle, dark below.
		shade := uint8(200)
		if y >= height/2 {
			shade = 40
		}

		for x := range width {
			tall.SetGray(x, y, color.Gray{Y: shade})
		}
	}

	// resize is img resized, and what the resize allocated.
	resize := func(img image.Image, width, height int) (image.Image, uint64) {
		var before, after runtime.MemStats

		runtime.ReadMemStats(&before)

		resized := resizeLanczos(img, width, height)

		runtime.ReadMemStats(&after)

		return resized, after.TotalAlloc - before.TotalAlloc
	}

	resized, took := resize(tall, width, rows)
	if took > 8<<20 {
		t.Errorf("the tall image's resize allocated %d KiB", took>>10)
	}

	top, bottom := color.NRGBAModel.Convert(resized.At(32, 10)), color.NRGBAModel.Convert(resized.At(32, 90))
	if top != (color.NRGBA{200, 200, 200, 255}) || bottom != (color.NRGBA{40, 40, 40, 255}) {
		t.Errorf("the resized image is %v above and %v below, want the 200 and the 40 of the image", top, bottom)
	}

	resized, took = resize(shaded(1500, 1500), 700, 700)
	if took > 8<<20 {
		t.Errorf("the square image's resize allocated %d KiB", took>>10)
	}

	if resized.Bounds() != image.Rect(0, 0, 700, 700) {
		t.Errorf("the square resized is %v", resized.Bounds())
	}
}

// An image long and thin is resized whichever way it is drawn, and soon:
// 64 by 65536 drawn as a row of 65536, which resampling the rows first
// would take minutes for; and a row of 700000 drawn as 300, too long for
// its weights to be kept, which are then worked out for the one line.
func TestResizeLongAndThin(t *testing.T) {
	flat := image.NewGray(image.Rect(0, 0, 64, 65536))
	for i := range flat.Pix {
		flat.Pix[i] = 200
	}

	row := resizeLanczos(flat, 65536, 1)
	for x := range 65536 {
		if got := color.NRGBAModel.Convert(row.At(x, 0)); got != (color.NRGBA{200, 200, 200, 255}) {
			t.Fatalf("pixel %d of the row: %v, want the image's 200", x, got)
		}
	}

	const long, drawn = 700000, 300

	if newAxis(long, drawn).table != nil {
		t.Fatalf(
			"the weights of %d pixels drawn from %d are kept: the test wants an axis too long for that",
			drawn,
			long,
		)
	}

	ramp := image.NewGray(image.Rect(0, 0, long, 1))
	for x := range long {
		ramp.Pix[x] = uint8(x * 256 / long)
	}

	short := resizeLanczos(ramp, drawn, 1)
	for x := range drawn {
		// The mean of the ramp over the pixels drawn pixel x stands for.
		want := (float64(x) + 0.5) * 256 / drawn

		if got := float64(color.NRGBAModel.Convert(short.At(x, 0)).(color.NRGBA).R); got < want-2 || got > want+2 {
			t.Fatalf("pixel %d of the ramp: %v, want about %.0f", x, got, want)
		}
	}
}
