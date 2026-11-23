package graphviz_test

import (
	"os"
	"testing"

	"golang.org/x/image/font/sfnt"
)

// catalan is the Windows language of a name record in Catalan, which a name
// table lists before English.
const catalan = 0x403

// A font says its family in every language it has names in, and its style
// in the words of its subfamily and in numbers, and it is found and matched
// by them: by its English family and by its other ones, at the weight and
// slant its OS/2 table gives where its subfamily names none. The font parser
// gave one name for each, the first record of the table, so a font whose
// Catalan records come first was "Negreta" and not "Bold": both its faces
// weighed the same, and the bold file answered for the regular. A font with
// names on the Unicode platform alone had no family.
func TestAFaceIsKnownByItsNamesAndItsNumbers(t *testing.T) {
	inOtherLanguages := func(family, subfamily string) []nameRecord {
		return []nameRecord{
			{language: catalan, id: sfnt.NameIDFamily, text: family},
			{language: catalan, id: sfnt.NameIDSubfamily, text: subfamily},
		}
	}

	faces := []corpusFace{
		// The bold file first, as a directory may list it.
		{
			family: "Zt", subfamily: "Bold", weight: 700, advance: 240,
			names: inOtherLanguages("Zt", "Negreta"),
		},
		{
			family: "Zt", subfamily: "Regular", advance: 200,
			names: inOtherLanguages("Zt", "Normal"),
		},
		// A family with another name in another language.
		{
			family: "Zu English", subfamily: "Regular", advance: 280,
			names: inOtherLanguages("Tipus Zu", "Normal"),
		},
		// Styles whose subfamily is no word of a style.
		{family: "Zv", subfamily: "Strong", weight: 700, advance: 320},
		{family: "Zv", subfamily: "Leaning", italic: true, advance: 360},
		{family: "Zv", subfamily: "Tight", width: 3, advance: 400},
		{family: "Zv", subfamily: "Plain", advance: 440},
		// Names on the Unicode platform and nowhere else.
		{
			noEnglish: true, advance: 480,
			names: []nameRecord{
				{id: sfnt.NameIDFamily, text: "Zw Unicode", unicode: true},
				{id: sfnt.NameIDSubfamily, text: "Regular", unicode: true},
			},
		},
	}

	inFontCorpus(t, faces, func(t *testing.T) {
		t.Helper()

		for name, want := range map[string]float64{
			"Zt":           200,
			"Zt Bold":      240,
			"Zt:bold":      240,
			"Zu English":   280,
			"Tipus Zu":     280,
			"Zv":           440,
			"Zv Bold":      320,
			"Zv Italic":    360,
			"Zv Condensed": 400,
			"Zw Unicode":   480,
		} {
			if got := chosenAdvance(t, name); got != want {
				t.Errorf("%q is set in the font whose A is %.0f units wide, want %.0f", name, got, want)
			}
		}
	})
}

// A face whose subfamily names a weight in English has that weight, whatever
// its OS/2 table gives: many a font says 400 there for its Bold and for its
// Light, and read by the number alone the three faces of such a family were
// one, and the first of them answered for all.
func TestAFaceNamedForAWeightHasIt(t *testing.T) {
	faces := []corpusFace{
		// The regular last, as a directory may list it.
		{family: "Zs", subfamily: "Bold", weight: 400, advance: 520},
		{family: "Zs", subfamily: "Light", weight: 400, advance: 560},
		{family: "Zs", subfamily: "Regular", weight: 400, advance: 600},
		// A weight in the words and a slant in the numbers alone.
		{family: "Zs", subfamily: "Bold", weight: 400, italic: true, advance: 640},
		// The typographic subfamily is the one read, where there are both.
		{family: "Zr Heavy", subfamily: "Regular", typoFamily: "Zr", typoSubfamily: "Heavy", weight: 400, advance: 680},
		{family: "Zr", subfamily: "Regular", advance: 720},
		// Heavy is the weight above extra bold, as OpenType numbers them.
		{family: "Zp", subfamily: "ExtraBold", weight: 800, advance: 760},
		{family: "Zp", subfamily: "Heavy", weight: 900, advance: 800},
	}

	inFontCorpus(t, faces, func(t *testing.T) {
		t.Helper()

		for name, want := range map[string]float64{
			"Zs":             600,
			"Zs Bold":        520,
			"Zs Light":       560,
			"Zs Bold Italic": 640,
			"Zr":             720,
			"Zr Heavy":       680,
			"Zr:heavy":       680,
			"Zp ExtraBold":   760,
			"Zp Heavy":       800,
		} {
			if got := chosenAdvance(t, name); got != want {
				t.Errorf("%q is set in the font whose A is %.0f units wide, want %.0f", name, got, want)
			}
		}
	})
}

// A family whose face for a style is one the font parser does not take is
// known to have that face: it gives way to the next family that has one it
// can draw with, and answers with another of its own faces only where no
// family has better. The members of a collection the parser refused were
// left out as if they were not there, so a family whose regular was one of
// them answered a request for regular with its medium, with the next
// family's regular at hand.
func TestAFaceThatCannotBeLoadedGivesWay(t *testing.T) {
	files := map[string][]byte{}

	if os.Getenv(fontCorpusEnv) == "" {
		files["zx.ttc"] = collectionOf(t,
			corpusFace{family: "Zx", subfamily: "Regular", advance: 200, unparsable: true},
			corpusFace{family: "Zx", subfamily: "Medium", weight: 500, advance: 240},
		)
		files["zy.ttf"] = corpusFace{family: "Zy", subfamily: "Regular", advance: 280}.font(t)
		files["zy-bold.ttf"] = corpusFace{family: "Zy", subfamily: "Bold", weight: 700, advance: 320}.font(t)
	}

	inFontFiles(t, files, func(t *testing.T) {
		t.Helper()

		for name, want := range map[string]float64{
			// The next family's regular, not this one's medium.
			"Zx, Zy": 280,
			// No family has better: the face of its own that is nearest.
			"Zx": 240,
			// A style it can draw is its own.
			"Zx Medium, Zy": 240,
			"Zy, Zx":        280,
			"Zy Bold":       320,
		} {
			if got := chosenAdvance(t, name); got != want {
				t.Errorf("%q is set in the font whose A is %.0f units wide, want %.0f", name, got, want)
			}
		}
	})
}
