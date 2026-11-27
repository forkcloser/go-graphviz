package graphviz_test

import (
	"io"
	"math"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// fallbackAdvance is the advance, in the font that draws it, of char in a
// span set in the font name resolves to, which does not have it: the width
// a label of ten of them is laid out at, times ten.
func fallbackAdvance(t *testing.T, char rune, name string) float64 {
	t.Helper()

	dot := `digraph { node [shape=box margin=0 width=0 height=0]; a [label="` +
		strings.Repeat(string(char), len(corpusLabel)) + `" fontname="` + name + `" fontsize=` + corpusSize + `] }`

	return math.Round(nodeWidth(t, dot)) * 10
}

// A character the span's font does not have is drawn from an installed font
// that has it, and which one does not depend on what the process drew
// before, on an outline being there that encloses nothing, or on the order
// of the files in a directory. A font that has the character blank, with no
// outline at all, means it blank.
//
// The characters are ones no installed font is expected to have (Khitan
// small script: macOS has a font for many a script as rare, Nag Mundari
// among them, and not for this one), which the fonts of the corpus are
// given, each as its A and so at its own width.
func TestFallbackFont(t *testing.T) {
	const (
		twice    = rune(0x18B00) // in two fonts
		once     = rune(0x18B01) // in the second of them alone
		hollow   = rune(0x18B02) // a placeholder in a font, drawn in another
		inFamily = rune(0x18B03) // in the bold and the regular of a family
		lonely   = rune(0x18B04) // a placeholder in a font, and in no other
		empty    = rune(0x18B05) // blank in a font, drawn in a later one
		half     = rune(0x18B06) // in the regular of a family, not its bold

		// The advance of a space in Go Regular, which the corpus's fonts
		// are made of.
		space = 570
	)

	faces := []corpusFace{
		// The fonts the spans are set in: neither has any of the seven.
		{family: "Zq Span", subfamily: "Regular", advance: 200},
		{family: "Zq Span", subfamily: "Bold", advance: 240, weight: 700},
		{family: "Zq First", subfamily: "Regular", advance: 280, also: []rune{twice}},
		{family: "Zq Second", subfamily: "Regular", advance: 320, also: []rune{twice, once}},
		{family: "Zq Hollow", subfamily: "Regular", advance: 360, placeholder: []rune{hollow, lonely}},
		{family: "Zq Inked", subfamily: "Regular", advance: 400, also: []rune{hollow}},
		// The bold before the regular, as their files sort.
		{family: "Zq Fall", subfamily: "Bold", advance: 440, weight: 700, also: []rune{inFamily}},
		{family: "Zq Fall", subfamily: "Regular", advance: 480, also: []rune{inFamily}},
		{family: "Zq Blank", subfamily: "Regular", advance: 520, blank: []rune{empty}},
		{family: "Zq After", subfamily: "Regular", advance: 560, also: []rune{empty}},
		{family: "Zq Half", subfamily: "Regular", advance: 600, also: []rune{half}},
		{family: "Zq Half", subfamily: "Bold", advance: 640, weight: 700},
	}

	inFontCorpus(t, faces, func(t *testing.T) {
		t.Helper()

		// In this order: the first asks the second font, which then has
		// answered when the next character, that the first font has too, is
		// asked for.
		for _, tc := range []struct {
			what string
			char rune
			span string
			want float64
		}{
			{"a character one font has", once, "Zq Span", 320},
			{"a character two fonts have, the second having answered before", twice, "Zq Span", 280},
			{"a character one font has a placeholder for, and another draws", hollow, "Zq Span", 400},
			{"a character one font has a placeholder for, and no other has", lonely, "Zq Span", 360 + placeholderWider},
			{"a character one font has blank, though a later one draws it", empty, "Zq Span", space},
			{"a character of a family, in a regular span", inFamily, "Zq Span", 480},
			{"a character of a family, in a bold span", inFamily, "Zq Span Bold", 440},
			{"a character the family's bold has not, in a bold span", half, "Zq Span Bold", 600},
		} {
			if got := fallbackAdvance(t, tc.char, tc.span); got != tc.want {
				t.Errorf("%s (U+%X in %q): drawn %v wide, want %v", tc.what, tc.char, tc.span, got, tc.want)
			}
		}
	})
}

// BenchmarkFallbackUncovered lays out a node whose label is a character no
// installed font is expected to have (an ideograph of Extension H), another
// at each turn: what such a character costs the first time it is met, which
// was a read of every installed font.
func BenchmarkFallbackUncovered(b *testing.B) {
	const first, count = rune(0x31350), 4192

	g, err := graphviz.New(b.Context())
	if err != nil {
		b.Fatal(err)
	}

	defer func() { _ = g.Close() }()

	turn := 0

	for b.Loop() {
		dot := `digraph { a [label="` + string(first+rune(turn%count)) + `"] }`
		turn++

		graph, err := graphviz.ParseBytes([]byte(dot))
		if err != nil {
			b.Fatal(err)
		}

		if err = g.Render(b.Context(), graph, graphviz.SVG, io.Discard); err != nil {
			b.Fatal(err)
		}

		if err = graph.Close(); err != nil {
			b.Fatal(err)
		}
	}
}
