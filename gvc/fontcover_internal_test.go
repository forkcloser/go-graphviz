package gvc

import (
	"bytes"
	"slices"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

// The coverage read from a font's cmap table is the font parser's own
// answer, character for character: a character the parser has a glyph for
// and the coverage has not would hide the font from the fallback.
func TestCoverageIsTheFontParsers(t *testing.T) {
	for name, ttf := range map[string][]byte{"regular": goregular.TTF, "bold": gobold.TTF, "mono": gomono.TTF} {
		faces, err := readFaces(bytes.NewReader(ttf))
		if err != nil || len(faces) != 1 {
			t.Fatalf("%s: %d faces, %v", name, len(faces), err)
		}

		covers := faces[0].covers
		if covers.unknown || len(covers.ranges) == 0 {
			t.Fatalf("%s: the coverage was not read: %+v", name, covers)
		}

		parsed, err := sfnt.Parse(ttf)
		if err != nil {
			t.Fatal(err)
		}

		var buf sfnt.Buffer

		for char := rune(0); char <= 0x1FFFF; char++ {
			glyph, glyphErr := parsed.GlyphIndex(&buf, char)
			if has := glyphErr == nil && glyph != 0; has != covers.has(char) {
				t.Fatalf("%s U+%04X: the parser has a glyph: %t, the coverage: %t", name, char, has, covers.has(char))
			}
		}
	}
}

// cmapOf is a cmap table of one subtable, for platform in encoding.
func cmapOf(platform, encoding byte, subtable ...byte) []byte {
	return append([]byte{0, 0, 0, 1, 0, platform, 0, encoding, 0, 0, 0, 12}, subtable...)
}

// The formats beside the segments of the Go fonts, and what is not read:
// a table the parser reads another way, or one that does not hold
// together, is unknown, and a face of unknown coverage is asked itself.
func TestCoverageByFormat(t *testing.T) {
	for name, tc := range map[string]struct {
		table   []byte
		ranges  []runeRange
		unknown bool
	}{
		"groups, of any plane, merged where they touch": {
			table: cmapOf(3, 10,
				0, 12, 0, 0, 0, 0, 0, 52, 0, 0, 0, 0, 0, 0, 0, 3,
				0, 0, 0, 0x41, 0, 0, 0, 0x43, 0, 0, 0, 5, // A to C
				0, 0, 0, 0x44, 0, 0, 0, 0x44, 0, 0, 0, 9, // D, touching
				0, 1, 0xE4, 0xD0, 0, 1, 0xE4, 0xD2, 0, 0, 0, 0, // three, the first to glyph 0
			),
			ranges: []runeRange{{0x41, 0x44}, {0x1E4D1, 0x1E4D2}},
		},
		"a trimmed table, a glyph 0 left out": {
			table:  cmapOf(3, 1, 0, 6, 0, 16, 0, 0, 0, 0x61, 0, 3, 0, 7, 0, 0, 0, 8),
			ranges: []runeRange{{0x61, 0x61}, {0x63, 0x63}},
		},
		"groups cut short":          {table: cmapOf(3, 10, 0, 12, 0, 0, 0, 0, 0, 28, 0, 0, 0, 0, 0, 0, 0, 1), unknown: true},
		"a Macintosh table":         {table: append(cmapOf(1, 0, 0, 0, 1, 6, 0, 0), make([]byte, 256)...), unknown: true},
		"an encoding nobody reads":  {table: cmapOf(3, 5, 0, 12, 0, 0, 0, 0, 0, 16, 0, 0, 0, 0, 0, 0, 0, 0), unknown: true},
		"a format nobody reads":     {table: cmapOf(3, 10, 0, 13, 0, 0, 0, 0, 0, 16, 0, 0, 0, 0, 0, 0, 0, 0), unknown: true},
		"no table":                  {unknown: true},
		"a subtable past the table": {table: []byte{0, 0, 0, 1, 0, 3, 0, 10, 0, 0, 1, 0}, unknown: true},
		"records past the table":    {table: []byte{0, 0, 0, 9, 0, 3, 0, 10, 0, 0, 0, 12}, unknown: true},
		"groups, a last before a first": {
			table: cmapOf(3, 10,
				0, 12, 0, 0, 0, 0, 0, 28, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 9, 0, 0, 0, 8, 0, 0, 0, 1),
			unknown: true,
		},
	} {
		got := readCoverage(tc.table)
		if got.unknown != tc.unknown || !slices.Equal(got.ranges, tc.ranges) {
			t.Errorf("%s: %+v, want ranges %v, unknown %t", name, got, tc.ranges, tc.unknown)
		}

		if tc.unknown && !got.has('A') {
			t.Errorf("%s: a face of unknown coverage is not asked", name)
		}
	}
}
