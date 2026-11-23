package graphviz_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"slices"
	"testing"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"

	"github.com/forkcloser/go-graphviz"
)

// kerned is the TrueType font ttf with a kern table of its own: left
// followed by right, and right by left, is moved by units of the font's em.
// The fonts every platform has to test with carry no kerning.
func kerned(t *testing.T, ttf []byte, left, right rune, units int16) []byte {
	t.Helper()

	parsed, err := sfnt.Parse(ttf)
	if err != nil {
		t.Fatal(err)
	}

	var buf sfnt.Buffer

	first, err := parsed.GlyphIndex(&buf, left)
	if err != nil || first == 0 {
		t.Fatalf("no glyph for %q: %v", left, err)
	}

	second, err := parsed.GlyphIndex(&buf, right)
	if err != nil || second == 0 {
		t.Fatalf("no glyph for %q: %v", right, err)
	}

	pairs := [][2]uint16{{uint16(first), uint16(second)}, {uint16(second), uint16(first)}}
	slices.SortFunc(pairs, func(a, b [2]uint16) int {
		return int(uint32(a[0])<<16|uint32(a[1])) - int(uint32(b[0])<<16|uint32(b[1]))
	})

	// The table: its header, one horizontal subtable in format 0, the pairs.
	const (
		tableHeader    = 4
		subtableHeader = 6
		format0Header  = 8
		pairSize       = 6
	)

	subtable := subtableHeader + format0Header + pairSize*len(pairs)
	kern := make([]byte, 0, tableHeader+subtable)
	kern = binary.BigEndian.AppendUint16(kern, 0) // version
	kern = binary.BigEndian.AppendUint16(kern, 1) // subtables
	kern = binary.BigEndian.AppendUint16(kern, 0) // the subtable's version
	kern = binary.BigEndian.AppendUint16(kern, uint16(subtable))
	kern = binary.BigEndian.AppendUint16(kern, 1) // horizontal, format 0
	kern = binary.BigEndian.AppendUint16(kern, uint16(len(pairs)))
	kern = binary.BigEndian.AppendUint16(kern, 12) // searchRange for two pairs
	kern = binary.BigEndian.AppendUint16(kern, 1)  // entrySelector
	kern = binary.BigEndian.AppendUint16(kern, 0)  // rangeShift

	for _, pair := range pairs {
		kern = binary.BigEndian.AppendUint16(kern, pair[0])
		kern = binary.BigEndian.AppendUint16(kern, pair[1])
		kern = binary.BigEndian.AppendUint16(kern, uint16(units))
	}

	// The font again, with one more record in its table directory, in tag
	// order as the reader wants it, and the table at the end.
	const (
		offsetTable = 12
		record      = 16
	)

	tables := int(binary.BigEndian.Uint16(ttf[4:]))

	out := make([]byte, 0, len(ttf)+record+len(kern)+3)
	out = append(out, ttf[:offsetTable]...)
	binary.BigEndian.PutUint16(out[4:], uint16(tables+1))

	added := false

	add := func() {
		out = append(out, "kern"...)
		out = binary.BigEndian.AppendUint32(out, 0) // the checksum, which the reader does not ask for
		out = binary.BigEndian.AppendUint32(out, uint32((len(ttf)+record+3)&^3))
		out = binary.BigEndian.AppendUint32(out, uint32(len(kern)))
		added = true
	}

	for i := range tables {
		entry := ttf[offsetTable+record*i : offsetTable+record*(i+1)]

		switch tag := string(entry[:4]); {
		case tag == "kern" || tag == "GPOS":
			t.Fatalf("the font has a %s table of its own", tag)
		case !added && tag > "kern":
			add()
		}

		out = append(out, entry...)
		binary.BigEndian.PutUint32(out[len(out)-8:], binary.BigEndian.Uint32(entry[8:])+record)
	}

	if !added {
		add()
	}

	out = append(out, ttf[offsetTable+record*tables:]...)
	for len(out)%4 != 0 {
		out = append(out, 0)
	}

	return append(out, kern...)
}

// inkWidth is how many pixel columns of img hold something darker than the
// page, from the first to the last.
func inkWidth(img image.Image) int {
	bounds := img.Bounds()
	low, high := bounds.Max.X, bounds.Min.X

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if r, g, b, _ := img.At(x, y).RGBA(); r+g+b < 3*0x8000 {
				low, high = min(low, x), max(high, x)
			}
		}
	}

	return max(high-low+1, 0)
}

// Kerning is a part of the em, so it grows with the font's size: a label
// set in a font that draws A and V closer is laid out, at every size, that
// much narrower than in the same font without the pair, and drawn that
// much narrower too. The kern used to be the font's units read as
// pixels, the same at every size: Arial's A and V were 2.4 px closer at
// 7 points as at 140, the label was laid out narrower than it was drawn,
// and the text ran past its box.
func TestKerningScalesWithTheFontSize(t *testing.T) {
	const (
		units    = -400 // of an em of 2048: a fifth of the size
		em       = 2048
		label    = "AVAVAVAVAV"
		pairs    = len(label) - 1
		plain    = "Plain Face"
		closeSet = "Kerned Face"
	)

	plainFont, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}

	kernedFont, err := opentype.Parse(kerned(t, goregular.TTF, 'A', 'V', units))
	if err != nil {
		t.Fatal(err)
	}

	graphviz.SetFontLoader(func(_ context.Context, textFont *graphviz.TextFont) (*opentype.Font, error) {
		switch textFont.Name() { //nolint:contextcheck // a TextFont's getters read memory and take no context
		case plain:
			return plainFont, nil
		case closeSet:
			return kernedFont, nil
		}

		return nil, nil //nolint:nilnil // no font asks for the usual resolution
	})
	t.Cleanup(func() { graphviz.SetFontLoader(nil) })

	dot := func(shape, face string, size int) string {
		return fmt.Sprintf(
			`digraph { node [shape=%s margin=0 width=0 height=0]; a [label=%q fontname=%q fontsize=%d] }`,
			shape,
			label,
			face,
			size,
		)
	}

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	for _, size := range []int{7, 14, 40, 140} {
		without := nodeWidth(t, dot("box", plain, size))
		with := nodeWidth(t, dot("box", closeSet, size))

		// The pairs' share of the size, in points.
		want := float64(pairs) * float64(-units) / em * float64(size)

		if got := without - with; math.Abs(got-want) > 0.5 {
			t.Errorf("at %d points the kerned label is laid out %.2f points narrower, want %.2f", size, got, want)
		}

		// Drawn, at 96 dots an inch: the ink is narrower by the same share.
		const dpi, pointsPerInch = 96.0, 72.0

		drawn := func(face string) float64 {
			graph, err := graphviz.ParseBytes([]byte(dot("plaintext", face, size)))
			if err != nil {
				t.Fatal(err)
			}

			defer closeOrError(t, graph.Close)

			img, err := g.RenderImage(t.Context(), graph)
			if err != nil {
				t.Fatal(err)
			}

			return float64(inkWidth(img))
		}

		if got := drawn(plain) - drawn(closeSet); math.Abs(got-want*dpi/pointsPerInch) > 2 {
			t.Errorf(
				"at %d points the kerned label is drawn %.0f px narrower, want %.1f",
				size,
				got,
				want*dpi/pointsPerInch,
			)
		}
	}
}
