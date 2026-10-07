package gvc

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func TestParseStyle(t *testing.T) {
	for _, tc := range []struct {
		subfamily string
		want      fontStyle
	}{
		{"Regular", fontStyle{weight: weightRegular}},
		{"Bold Italic", fontStyle{weight: weightBold, italic: true}},
		{"BoldOblique", fontStyle{weight: weightBold, italic: true}},
		{"Demi Bold", fontStyle{weight: weightBold}},
		{"SemiBold", fontStyle{weight: weightDemi}},
		{"Condensed Light", fontStyle{weight: weightLight, condensed: true}},
		{"W3", fontStyle{weight: weightRegular}},
		{"W6", fontStyle{weight: weightBold}},
		{"Chancery", fontStyle{weight: weightRegular}},
	} {
		if got := parseStyle(tc.subfamily); got != tc.want {
			t.Errorf("parseStyle(%q) = %+v, want %+v", tc.subfamily, got, tc.want)
		}
	}
}

// A name that is not one of Graphviz's PostScript names is a family
// followed by style words, or a list of those, possibly ending with a
// generic family.
func TestRequestForPlainNames(t *testing.T) {
	for _, tc := range []struct {
		name     string
		flags    uint
		families []string
		generic  string
		style    fontStyle
	}{
		{"Arial", 0, []string{"Arial"}, "", fontStyle{weight: weightRegular}},
		{"DejaVu Sans Bold", 0, []string{"DejaVu Sans"}, "", fontStyle{weight: weightBold}},
		{"Go-BoldItalic", 0, []string{"Go"}, "", fontStyle{weight: weightBold, italic: true}},
		{"DejaVu Sans:bold:italic", 0, []string{"DejaVu Sans"}, "", fontStyle{weight: weightBold, italic: true}},
		{"Inter, Helvetica, sans-serif", 0, []string{"Inter", "Helvetica"}, "sans-serif", fontStyle{weight: weightRegular}},
		{"Bold", 0, []string{"Bold"}, "", fontStyle{weight: weightRegular}},
		{"Arial", htmlBold | htmlItalic, []string{"Arial"}, "", fontStyle{weight: weightBold, italic: true}},
	} {
		got := requestFor(tc.name, nil, tc.flags)
		if !slices.Equal(got.families, tc.families) || got.generic != tc.generic || got.style != tc.style {
			t.Errorf("requestFor(%q, %d) = %+v, want families %v, generic %q, style %+v",
				tc.name, tc.flags, got, tc.families, tc.generic, tc.style)
		}
	}
}

// A condensed request tries the narrow cut of every candidate family before
// any regular cut; the substitutes of a family follow it.
func TestCandidates(t *testing.T) {
	request := fontRequest{
		families: []string{"Helvetica"},
		generic:  "sans-serif",
		style:    fontStyle{weight: weightRegular, condensed: true},
	}

	got := request.candidates()
	narrow, plain := slices.Index(got, "Arial Narrow"), slices.Index(got, "Helvetica")

	if narrow < 0 || plain < 0 || narrow > plain {
		t.Errorf("Arial Narrow at %d, Helvetica at %d in %v; want the narrow cut first", narrow, plain, got)
	}

	request.style.condensed = false
	if got := request.candidates(); got[0] != "Helvetica" || got[1] != "Arial" {
		t.Errorf("candidates start %v, want Helvetica then its substitute Arial", got[:2])
	}
}

// writeFonts puts font files in a directory and indexes it.
func writeFonts(t *testing.T, files map[string][]byte) *fontIndex {
	t.Helper()

	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return buildFontIndex([]string{dir})
}

// A family is matched to the installed face closest in weight and slant,
// by its name table, whatever the files are called.
func TestResolveFontFromIndex(t *testing.T) {
	index := writeFonts(t, map[string][]byte{
		"a.ttf": goregular.TTF,
		"b.ttf": gobold.TTF,
		"c.ttf": goitalic.TTF,
		"d.ttf": gobolditalic.TTF,
	})

	for _, tc := range []struct {
		name string
		file string
	}{
		{"Go", "a.ttf"},
		{"Go Bold", "b.ttf"},
		{"Go-Italic", "c.ttf"},
		{"Go:bold:italic", "d.ttf"},
		{"Go Black", "b.ttf"},
		{"Go Light Oblique", "c.ttf"},
	} {
		loaded, err := resolveFont(index, tc.name, requestFor(tc.name, nil, 0))
		if err != nil {
			t.Fatal(err)
		}

		if want := filepath.Join(filepath.Dir(index.faces[0].path), tc.file) + "#-1"; loaded.key != want {
			t.Errorf("%q resolved to %s, want %s", tc.name, loaded.key, want)
		}
	}
}

// A family nothing installed answers to falls back to the embedded Go font
// of the generic family and style, and a font file named by its path is
// read from there.
func TestResolveFontFallbacks(t *testing.T) {
	empty := &fontIndex{byFamily: map[string][]installedFace{}, byFile: map[string][]installedFace{}}

	for _, tc := range []struct {
		name    string
		request fontRequest
		want    string
	}{
		{"NoSuchFont", requestFor("NoSuchFont", nil, 0), "go mono=false bold=false italic=false"},
		{"NoSuchFont Bold Italic", requestFor("NoSuchFont Bold Italic", nil, 0), "go mono=false bold=true italic=true"},
		{
			"NoSuchMono",
			fontRequest{generic: genericMonospace, style: fontStyle{weight: weightBold}},
			"go mono=true bold=true italic=false",
		},
	} {
		loaded, err := resolveFont(empty, tc.name, tc.request)
		if err != nil {
			t.Fatal(err)
		}

		if loaded.key != tc.want {
			t.Errorf("%q resolved to %s, want %s", tc.name, loaded.key, tc.want)
		}
	}

	path := filepath.Join(t.TempDir(), "custom.ttf")
	if err := os.WriteFile(path, gobold.TTF, 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := resolveFont(empty, path, requestFor(path, nil, 0))
	if err != nil {
		t.Fatal(err)
	}

	if loaded.key != path+"#-1" {
		t.Errorf("a font named by its path resolved to %s", loaded.key)
	}
}

// lacking is a face that has no glyph for one character, standing in for a
// font without, say, Japanese.
type lacking struct {
	font.Face

	missing rune
}

func (f lacking) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	if r == f.missing {
		return 0, false
	}

	return f.Face.GlyphAdvance(r)
}

// A character the span's face lacks is drawn from an installed font that
// has it, the rest of the span staying with its face; with no such font it
// stays, and the answer is remembered.
func TestGlyphFallbackRuns(t *testing.T) {
	regular, err := embeddedFont("", fontStyle{weight: weightRegular})
	if err != nil {
		t.Fatal(err)
	}

	face, err := regular.face(12, 72)
	if err != nil {
		t.Fatal(err)
	}

	primary := lacking{Face: face, missing: 'é'}

	fallback := newGlyphFallback(writeFonts(t, map[string][]byte{"bold.ttf": gobold.TTF}))

	runs := fallback.runs("café au lait", primary, 12, 72)
	if len(runs) != 3 || runs[0].text != "caf" || runs[1].text != "é" || runs[2].text != " au lait" {
		t.Fatalf("runs %+v, want caf, é, au lait", runs)
	}

	if runs[0].face != primary || runs[2].face != primary || runs[1].face == primary {
		t.Error("é not drawn from the fallback font, or the rest not from the span's")
	}

	none := newGlyphFallback(writeFonts(t, map[string][]byte{}))
	if runs := none.runs("café", primary, 12, 72); len(runs) != 1 || runs[0].face != primary {
		t.Errorf("with no fallback font, runs %+v, want the whole span with its face", runs)
	}

	if loaded, known := none.byRune['é']; !known || loaded != nil {
		t.Error("the missing glyph's answer not remembered")
	}
}

// symbolEncodedFace finds an installed font that keeps its glyphs in the
// private use area, as Symbol and Wingdings do on Windows: no glyph for 'A',
// one at U+F041.
func symbolEncodedFace(t *testing.T) font.Face {
	t.Helper()

	for _, installed := range fonts.installed().faces {
		loaded, err := installed.load()
		if err != nil {
			continue
		}

		face, err := loaded.face(12, 72)
		if err != nil {
			continue
		}

		_, latin := face.GlyphAdvance('A')
		_, private := face.GlyphAdvance(symbolBase + 'A')

		if !latin && private {
			t.Logf("symbol-encoded font: %s", loaded.key)

			return face
		}
	}

	return nil
}

// Text in a symbol-encoded font is drawn from its private use area, with
// that font, not handed to a fallback or drawn as boxes.
func TestSymbolEncodedFont(t *testing.T) {
	face := symbolEncodedFace(t)
	if face == nil {
		t.Skip("no symbol-encoded font installed (Windows has Symbol and Wingdings)")
	}

	runs := newGlyphFallback(&fontIndex{byFamily: map[string][]installedFace{}}).runs("ABC", face, 12, 72)

	want := string([]rune{symbolBase + 'A', symbolBase + 'B', symbolBase + 'C'})
	if len(runs) != 1 || runs[0].face != face || runs[0].text != want {
		t.Errorf("runs %+v, want one run of %q in the symbol font", runs, want)
	}
}

// Faces of fonts a FontLoader supplied are kept a few at a time: a loader
// that parses its font afresh on every call, a new pointer each time, does
// not grow the cache with every text drawn.
func TestLoaderFacesBounded(t *testing.T) {
	for range 3 * maxLoaderFaces {
		parsed, err := opentype.Parse(goregular.TTF)
		if err != nil {
			t.Fatal(err)
		}

		loaded := &loadedFont{key: fmt.Sprintf("loader|%p", parsed), font: parsed, fromLoader: true}
		if _, err := loaded.face(12, pointsPerInch); err != nil {
			t.Fatal(err)
		}
	}

	fonts.mu.Lock()
	kept := len(fonts.loaderFaces)
	fonts.mu.Unlock()

	if kept > maxLoaderFaces {
		t.Errorf("%d loader faces kept, want at most %d", kept, maxLoaderFaces)
	}
}
