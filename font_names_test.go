package graphviz_test

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// A font name is a family and, after it, the words of a style. Which words
// are the family's is asked of the fonts there are: a family whose own name
// ends in a word a style could be ("New Roman", "Black") is found by its
// name, and a style written after it is still read. A weight in two words
// is one weight, in a name and in a font's own subfamily. In a list, the
// style words apply wherever they stand. A condensed face a family keeps
// under its plain name comes before another family's narrow cut.
func TestFontNamesAreReadAgainstTheFontsThereAre(t *testing.T) {
	faces := []corpusFace{
		{family: "Zq New Roman", subfamily: "Regular", advance: 200},
		{family: "Zq New Roman", subfamily: "Bold", advance: 240, weight: 700},
		{family: "Zq", subfamily: "Regular", advance: 280},
		{family: "Zq", subfamily: "Bold", advance: 320, weight: 700},
		{family: "Zq Black", subfamily: "Regular", advance: 360},
		{family: "Zq Next", subfamily: "Regular", advance: 400},
		{family: "Zq Next", subfamily: "Ultra Light", advance: 440, weight: 200},
		{family: "Zq Next", subfamily: "Demi Bold", advance: 480, weight: 600},
		{family: "Zq Next", subfamily: "Bold", advance: 520, weight: 700},
		{family: "Zq-Script_One", subfamily: "Regular", advance: 560},
		{family: "Zq Wide", subfamily: "Regular", advance: 600},
		// A family of more words than any that a substitute stands in for.
		{family: "Zq Very Long New Roman", subfamily: "Regular", advance: 680},
		{family: "Zq Wide", subfamily: "Condensed ExtraBold", advance: 640, weight: 800, width: 3},
	}

	inFontCorpus(t, faces, func(t *testing.T) {
		t.Helper()

		for name, want := range map[string]float64{
			// A family by its name, whatever its last word.
			"Zq":            280,
			"Zq New Roman":  200,
			"Zq Black":      360,
			"Zq-Script_One": 560,
			"zq new roman":  200,
			"Zq-New-Roman":  200,

			// The longest family there is, by its name and with a style.
			"Zq Very Long New Roman":      680,
			"Zq Very Long New Roman Bold": 680,
			"Zq_New_Roman":                200,

			// A style after the family, in each way of writing it.
			"Zq Bold":            320,
			"Zq-Bold":            320,
			"Zq:bold":            320,
			"Zq New Roman Bold":  240,
			"Zq New Roman:bold":  240,
			"Zq New Roman-Bold":  240,
			"Zq-New-Roman-Bold":  240,
			"Zq Black Bold":      360,
			"Zq-Script_One Bold": 560,

			// A weight in two words, or with a hyphen.
			"Zq Next Ultra Light": 440,
			"Zq Next Ultra-Light": 440,
			"Zq Next UltraLight":  440,
			"Zq Next Demi Bold":   480,
			"Zq Next Semi-Bold":   480,
			"Zq Next Bold":        520,
			"Zq Next":             400,

			// A list: the first family there is, with the style written
			// anywhere in it.
			"Zq Nowhere, Zq Bold":      320,
			"Zq Bold, Zq Nowhere":      320,
			"Zq Nowhere, Zq New Roman": 200,

			// A condensed face under the family's plain name, with and
			// without a generic family after it.
			"Zq Wide Condensed ExtraBold":             640,
			"Zq Wide Condensed ExtraBold, sans-serif": 640,
			"Zq Wide": 600,
			// A family with no condensed face gives way to the next that
			// has one.
			"Zq Condensed, Zq Wide": 640,
		} {
			if got := chosenAdvance(t, name); got != want {
				t.Errorf("%q is set in the font whose A is %.0f units wide, want %.0f", name, got, want)
			}
		}
	})
}

// A generic family takes style words as any other family does: "Monospace
// Bold" is set in a monospace font, where the two words were taken for a
// family named Monospace that nobody has, and the label drew in the
// proportional font that stands in for everything.
func TestAGenericFamilyTakesStyleWords(t *testing.T) {
	width := func(name, label string) float64 {
		return nodeWidth(t, fmt.Sprintf(
			`digraph { node [shape=box margin=0 width=0 height=0]; a [label=%q fontname=%q fontsize=20] }`, label, name,
		))
	}

	const narrow, wide = "iiiiiiiiii", "WWWWWWWWWW"

	for _, name := range []string{"Monospace", "Monospace Bold", "monospace:bold", "Mono Bold Italic", "Monospace Semi Bold"} {
		if thin, broad := width(name, narrow), width(name, wide); math.Abs(thin-broad) > 0.5 {
			t.Errorf("%q sets ten i %.1f points wide and ten W %.1f: not a monospace font", name, thin, broad)
		}
	}

	if thin, broad := width("Sans Bold", narrow), width("Sans Bold", wide); math.Abs(thin-broad) <= 0.5 {
		t.Errorf("Sans Bold sets ten i and ten W both %.1f points wide: the test would see nothing", thin)
	}
}

// A name is read in a time that follows its length. Asking of every start
// of a name whether it is a family took the square of its words, in the
// text layout's callback with the module held: a name of 65,536 words took
// half a minute to a minute, and every other render of the process waited.
func TestALongFontNameIsReadInItsLength(t *testing.T) {
	const words = 1 << 16

	for _, word := range []string{"Zq ", "bold ", "a ", "Times New Roman "} {
		name := strings.Repeat(word, words)

		start := time.Now()

		_ = nodeWidth(t, fmt.Sprintf(
			`digraph { node [shape=box margin=0 width=0 height=0]; a [label=x fontname=%q fontsize=20] }`, name,
		))

		// Ten seconds is a machine that is busy: the square took twenty-three.
		if took := time.Since(start); took > 10*time.Second {
			t.Errorf("a name of %d times %q took %v to read", words, word, took)
		}
	}
}
