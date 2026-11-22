package graphviz_test

import (
	"bytes"
	"image"
	"image/color"
	_ "image/jpeg" // decodes the renders under test
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// JPEG has no transparency: what the drawing leaves transparent is white in
// it, and a colour that is partly transparent is that colour over white, as
// in Graphviz's own JPEG. The page went to the encoder as it was, and the
// margin of a graph, or the whole page under bgcolor=transparent, came out
// black.
func TestJPEGIsLaidOverWhite(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	// A pixel is read in the middle of a flat stretch, two inches of one
	// colour or half an inch of margin: there the encoder keeps a block's
	// mean to its quantizer's step, a level at this quality, and the
	// conversion to and from its colour space rounds each channel once
	// more each way.
	const tolerance = 3

	box := `a [shape=box style=filled color=transparent label="" width=2 height=2`

	for name, tc := range map[string]struct {
		src    string
		centre bool
		want   color.RGBA
	}{
		"the margin": {`digraph { margin=0.5; a }`, false, color.RGBA{255, 255, 255, 255}},
		"a transparent background": {
			`digraph { bgcolor=transparent; margin=0.5; a }`, false, color.RGBA{255, 255, 255, 255},
		},
		"a half transparent fill on a transparent background": {
			`digraph { bgcolor=transparent; ` + box + ` fillcolor="#ff000080"] }`, true, color.RGBA{255, 127, 127, 255},
		},
		"an opaque fill": {`digraph { ` + box + ` fillcolor="#00ff00"] }`, true, color.RGBA{0, 255, 0, 255}},
	} {
		graph, err := graphviz.ParseBytes([]byte(tc.src))
		if err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if err = g.Render(ctx, graph, graphviz.JPG, &out); err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		closeOrError(t, graph.Close)

		page, _, err := image.Decode(&out)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		// Ten pixels in from the corner is inside the margin; the middle
		// of the page is inside the box.
		x, y := 10, 10
		if tc.centre {
			x, y = page.Bounds().Dx()/2, page.Bounds().Dy()/2
		}

		// A JPEG has no alpha: its colours are the sixteen bits RGBA gives,
		// of which the high eight are the level.
		red, green, blue, _ := page.At(x, y).RGBA()

		for channel, pair := range map[string][2]int{
			"red":   {int(red >> 8), int(tc.want.R)},
			"green": {int(green >> 8), int(tc.want.G)},
			"blue":  {int(blue >> 8), int(tc.want.B)},
		} {
			if diff := pair[0] - pair[1]; diff < -tolerance || diff > tolerance {
				t.Errorf("%s: pixel (%d, %d): %s is %d, want %d", name, x, y, channel, pair[0], pair[1])
			}
		}
	}
}
