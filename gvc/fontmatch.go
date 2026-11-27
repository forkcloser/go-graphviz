package gvc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gobolditalic"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/gofont/gomonobolditalic"
	"golang.org/x/image/font/gofont/gomonoitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// Font resolution follows what Graphviz's Pango plugin asks fontconfig for.
// A PostScript name such as Times-Roman or Helvetica-Narrow-BoldOblique
// comes with Graphviz's alias for it: a family, a weight, a stretch, a style
// and a generic family. Any other name is read as a family followed by style
// words ("DejaVu Sans Bold", "Arial:italic"), or as a list of such names. The
// family is looked up among the installed fonts with its metric-compatible
// substitutes, then the generic family's, then the embedded Go fonts, which
// always answer. A character the chosen font has no glyph for is drawn from
// the first installed font that has one.

// Weights on the usual 100 to 900 scale.
const (
	weightThin       = 100
	weightExtraLight = 200
	weightLight      = 300
	weightRegular    = 400
	weightMedium     = 500
	weightDemi       = 600
	weightBold       = 700
	weightExtraBold  = 800
	weightBlack      = 900
)

// The font flags Graphviz sets for <B> and <I> in an HTML label.
const (
	htmlBold   = 1 << 0
	htmlItalic = 1 << 1
)

// Penalties when matching a face to a request: a slant or a width that
// differs counts more than any difference of weight.
const (
	italicMismatch    = 1000
	condensedMismatch = 500
)

// symbolBase is where a symbol-encoded font (cmap platform 3, encoding 0)
// keeps the glyph for the 8-bit code c: at U+F000+c, in the private use
// area.
const (
	symbolBase  = 0xF000
	symbolCodes = 0x100
)

// The generic families of CSS that Graphviz's aliases and font lists use.
const (
	genericSerif     = "serif"
	genericSansSerif = "sans-serif"
	genericMonospace = "monospace"
)

// fixedOne is 1 in the 26.6 fixed-point format x/image measures in.
const fixedOne = 64

// fontStyle is what a font name asks for besides its family.
type fontStyle struct {
	weight    int
	italic    bool
	condensed bool
}

// fontRequest is a resolved font name: the families to try in order, the
// generic family the embedded fonts stand in for, and the style.
type fontRequest struct {
	families []string
	generic  string
	style    fontStyle
}

// weightOf is the weight a style word names, or 0.
func weightOf(word string) int {
	switch word {
	case "thin", "hairline":
		return weightThin
	case "extralight", "ultralight":
		return weightExtraLight
	case "light":
		return weightLight
	case "book", "regular", "roman", "normal", "plain":
		return weightRegular
	case "medium":
		return weightMedium
	case "demi", "demibold", "semibold":
		return weightDemi
	case "bold":
		return weightBold
	case "extrabold", "ultrabold":
		return weightExtraBold
	case "black", "heavy", "extrablack", "ultrablack":
		return weightBlack
	}

	// Hiragino's W0 to W9: W3 is its regular weight, W6 its bold.
	if len(word) == 2 && word[0] == 'w' && word[1] >= '0' && word[1] <= '9' {
		return weightThin * (int(word[1]-'0') + 1)
	}

	return 0
}

// applyStyleWord applies one style word, which may run a weight and a slant
// together as a PostScript name does ("BoldItalic", "DemiOblique"), and
// reports whether it was one.
func applyStyleWord(word string, style *fontStyle) bool {
	word = strings.ToLower(word)

	switch word {
	case "":
		return false
	case "italic", "oblique", "slanted":
		style.italic = true

		return true
	case "condensed", "narrow", "compressed", "semicondensed", "extracondensed", "ultracondensed":
		style.condensed = true

		return true
	}

	if weight := weightOf(word); weight != 0 {
		style.weight = weight

		return true
	}

	for _, slant := range []string{"italic", "oblique"} {
		if rest, ok := strings.CutSuffix(word, slant); ok {
			if weight := weightOf(rest); weight != 0 {
				style.weight, style.italic = weight, true

				return true
			}
		}
	}

	return false
}

// styleWords is words with a style written in two of them made one: "Semi
// Bold", "Ultra Light" and "Extra Condensed" are one weight or one width
// each, as "SemiBold" is, and read apart they were the second word alone
// ("Demi Bold" was bold, "Ultra Light" light).
func styleWords(words []string) []string {
	joined := make([]string, 0, len(words))

	for i := 0; i < len(words); i++ {
		word := strings.ToLower(words[i])

		if i+1 < len(words) && slices.Contains([]string{"semi", "demi", "extra", "ultra"}, word) {
			var scratch fontStyle

			if pair := word + strings.ToLower(words[i+1]); applyStyleWord(pair, &scratch) {
				joined = append(joined, pair)
				i++

				continue
			}
		}

		joined = append(joined, word)
	}

	return joined
}

func isNameSeparator(char rune) bool { return char == ' ' || char == '-' || char == '_' }

// styled reads words as the words of a style, into style, and reports
// whether every one of them is one.
func styled(words []string, style *fontStyle) bool {
	read := *style

	for _, word := range styleWords(words) {
		if !applyStyleWord(word, &read) {
			return false
		}
	}

	*style = read

	return true
}

// splitFamilyStyle reads one font name of a list: a family followed by
// style words, separated by spaces or hyphens ("Arial Bold", "Go-Italic"),
// or by colons as in a fontconfig pattern ("DejaVu Sans:bold"). It reports
// whether the name had style words.
//
// Which words are the family's is asked of index, which knows the families
// that can be looked for: the name whole, then the longest start of it that
// is a family when what follows is all style, each as written and with its
// separators as spaces. So "Times New Roman" and "Arial Black" are
// families, where reading from the end took "Roman" and "Black" for a style
// and looked for "Times New". Only a name of which index knows no start is
// read from its end.
func splitFamilyStyle(
	name string, index *fontIndex, longest int,
) (family string, style fontStyle, hasStyle bool) {
	style = fontStyle{weight: weightRegular}

	family, styles, _ := strings.Cut(name, ":")
	family = strings.TrimSpace(family)

	colon := strings.FieldsFunc(styles, func(char rune) bool { return char == ':' || char == ' ' })
	for _, word := range styleWords(colon) {
		hasStyle = applyStyleWord(word, &style) || hasStyle
	}

	words, ends := nameWords(family)

	if start, tail, ok := knownStart(family, words[:min(len(words), longest)], ends, words, index, &style); ok {
		return start, style, hasStyle || tail
	}

	end := styleFromEnd(words, &style)

	return strings.Join(words[:end], " "), style, hasStyle || end < len(words)
}

// nameWords is the words of a family name, and where each ends in the name
// as it is written: a family may have a hyphen or an underscore of its own.
func nameWords(family string) (words []string, ends []int) {
	words, ends = []string{}, []int{}
	start := -1

	for position, char := range family + " " {
		switch {
		case !isNameSeparator(char):
			if start < 0 {
				start = position
			}
		case start >= 0:
			words, ends = append(words, family[start:position]), append(ends, position)
			start = -1
		}
	}

	return words, ends
}

// knownStart is the longest start of the name, the whole of it first, that
// is a family index knows when the words after it are all a style's, which
// it reads into style: the start as written, or its words with spaces
// between. It reports whether words followed the start.
//
// starts is the words a start may be made of, and words all of them. No
// start longer than the longest family there is can be one, so the caller
// gives no more words than that: a name is read in a time that follows its
// length, where asking about every start of a name of many words took the
// square of it.
func knownStart(
	family string, starts []string, ends []int, words []string, index *fontIndex, style *fontStyle,
) (start string, tail, ok bool) {
	for end := len(starts); end >= 1; end-- {
		written, spaced := family[:ends[end-1]], strings.Join(starts[:end], " ")

		found := ""

		switch {
		case index.knows(written):
			found = written
		case written != spaced && index.knows(spaced):
			found = spaced
		}

		if found != "" && styled(words[end:], style) {
			return found, end < len(words), true
		}
	}

	return "", false, false
}

// styleFromEnd reads the words at the end of a name that are a style's into
// style, down to one word of family, and returns how many words are left
// for the family: the reading of a name no start of which is a family
// anybody knows.
func styleFromEnd(words []string, style *fontStyle) int {
	end := len(words)

	for end > 1 {
		if end > 2 && len(styleWords(words[end-2:end])) == 1 && styled(words[end-2:end], style) {
			end -= 2

			continue
		}

		if !styled(words[end-1:end], style) {
			break
		}

		end--
	}

	return end
}

// genericOf is the generic family a font list entry names, or "".
func genericOf(entry string) string {
	switch strings.ToLower(entry) {
	case "serif":
		return genericSerif
	case "sans-serif", "sans":
		return genericSansSerif
	case "monospace", "mono":
		return genericMonospace
	case "fantasy", "cursive":
		return strings.ToLower(entry)
	}

	return ""
}

// aliasRequest is the request for a PostScript name, from Graphviz's alias.
func aliasRequest(alias *PostScriptAlias) fontRequest {
	request := fontRequest{
		families: []string{alias.Family()},
		generic:  strings.ToLower(alias.SVGFontFamily()),
		style:    fontStyle{weight: weightRegular},
	}

	applyStyleWord(alias.Weight(), &request.style)
	applyStyleWord(alias.Stretch(), &request.style)
	applyStyleWord(alias.Style(), &request.style)

	return request
}

// listRequest is the request for any other name: a comma-separated list of
// families, each possibly followed by style words, possibly with a generic
// family among them. The style is the list's: the words of the last entry
// that has any. Those of the last entry are how Pango reads a list
// ("Inter, Helvetica Bold" is bold); those of an earlier one, when the last
// has none, are this library's reading, which Pango takes for a part of
// that family's name ("Helvetica Bold, Inter" is bold here and regular in
// Graphviz's dot). A generic family takes style words as any other does:
// "Monospace Bold" is the monospace family, in bold.
func listRequest(name string, index *fontIndex) fontRequest {
	request := fontRequest{style: fontStyle{weight: weightRegular}}

	// Asked once for the list: it is a walk of the substitutes' table, and
	// a list may have as many entries as it has bytes.
	longest := index.longestFamily()

	for entry := range strings.SplitSeq(name, ",") {
		family, style, hasStyle := splitFamilyStyle(strings.TrimSpace(entry), index, longest)
		if family == "" {
			continue
		}

		if hasStyle {
			request.style = style
		}

		if generic := genericOf(family); generic != "" {
			if request.generic == "" {
				request.generic = generic
			}

			continue
		}

		request.families = append(request.families, family)
	}

	return request
}

// substituteGroups is the families that stand in for one another, group
// by group: the same metrics or the same design under another name.
func substituteGroups() [][]string {
	return [][]string{
		{
			"Times", "Times New Roman", "Nimbus Roman", "Nimbus Roman No9 L",
			"Liberation Serif", "Tinos", "TeX Gyre Termes",
		},
		{
			"Helvetica", "Arial", "Nimbus Sans", "Nimbus Sans L",
			"Liberation Sans", "Arimo", "TeX Gyre Heros",
		},
		{
			"Courier", "Courier New", "Nimbus Mono PS", "Nimbus Mono L",
			"Liberation Mono", "Cousine", "TeX Gyre Cursor",
		},
		{"Palatino", "Palatino Linotype", "P052", "URW Palladio L", "Book Antiqua", "TeX Gyre Pagella"},
		{"URW Bookman", "URW Bookman L", "Bookman Old Style", "Bookman", "ITC Bookman", "TeX Gyre Bonum"},
		{
			"URW Gothic", "URW Gothic L", "ITC Avant Garde Gothic", "Avant Garde",
			"Century Gothic", "TeX Gyre Adventor",
		},
		{"C059", "Century Schoolbook L", "New Century Schoolbook", "Century Schoolbook", "TeX Gyre Schola"},
		{"URW Chancery L", "Z003", "Apple Chancery", "TeX Gyre Chorus"},
		{"Symbol", "Standard Symbols PS", "Standard Symbols L"},
		{"Dingbats", "Zapf Dingbats", "ITC Zapf Dingbats", "D050000L"},
	}
}

// substitutesOf lists, for a family a graph names, the families with the
// same metrics or the same design that may be installed instead, the way
// fontconfig substitutes them for Graphviz's Pango plugin, or nil.
func substitutesOf(family string) []string {
	for _, group := range substituteGroups() {
		if slices.ContainsFunc(group, func(member string) bool { return strings.EqualFold(member, family) }) {
			return group
		}
	}

	return nil
}

// genericSubstitutesOf lists the families tried for a generic family once
// the named ones are exhausted.
func genericSubstitutesOf(generic string) []string {
	switch generic {
	case genericSerif:
		return []string{"Times", "Times New Roman", "Nimbus Roman", "Liberation Serif", "DejaVu Serif", "Noto Serif"}
	case genericSansSerif:
		return []string{"Helvetica", "Arial", "Nimbus Sans", "Liberation Sans", "DejaVu Sans", "Noto Sans"}
	case genericMonospace:
		return []string{
			"Courier", "Courier New", "Nimbus Mono PS", "Liberation Mono",
			"Menlo", "DejaVu Sans Mono", "Noto Sans Mono",
		}
	}

	return nil
}

// candidate is a family to look for, and whether only a condensed face of
// it will do.
type candidate struct {
	family    string
	condensed bool
}

// candidates lists the families to look for, in order: each named family
// with its substitutes, then the generic family's.
//
// A condensed request first goes through the same families asking for a
// condensed face: of each family in turn, its narrow cut under a name of
// its own ("Arial Narrow"), then the family itself where it keeps a
// condensed face under its plain name. A named family's own condensed face
// so comes before the narrow cut of a generic family's stand-in. Then every
// family again, for whatever face it has.
func (request fontRequest) candidates() []candidate {
	var found []candidate

	seen := map[candidate]bool{}
	add := func(family string, condensed bool) {
		key := candidate{strings.ToLower(family), condensed}
		if !seen[key] {
			seen[key] = true

			found = append(found, candidate{family, condensed})
		}
	}

	named := append(slices.Clone(request.families), genericSubstitutesOf(request.generic)...)
	groups := make([][]string, 0, len(named))

	for _, family := range named {
		groups = append(groups, append([]string{family}, substitutesOf(family)...))
	}

	if request.style.condensed {
		for _, group := range groups {
			for _, member := range group {
				add(member+" Narrow", false)
				add(member+" Condensed", false)
				add(member, true)
			}
		}
	}

	for _, group := range groups {
		for _, member := range group {
			add(member, false)
		}
	}

	return found
}

// installedFace is one face of an installed font file.
type installedFace struct {
	path string
	// index is the face's position in a collection, -1 for a font file
	// that is not one.
	index int
	style fontStyle
	// named says the graph names the file by its path: it is read through
	// the file system the graph's files are read from, as a node image is,
	// where an installed font is read from the host.
	named bool
	// unloadable says the font parser does not take the face: it is known
	// to be there, with its style, and cannot be drawn with.
	unloadable bool
}

// fileKey is what the face's file is kept under: its path, and for a file
// the graph names, the file system it was read through.
func (f installedFace) fileKey() string {
	if f.named {
		return fmt.Sprintf("fs %d: %s", wasm.FileSystemGeneration(), f.path)
	}

	return f.path
}

// read reads the face's file whole. With no file system named, a file the
// graph names is read from the host by its path as the graph wrote it: the
// font's name arrives whole, so nothing is tried but that path.
func (f installedFace) read() ([]byte, error) {
	if !f.named || !wasm.FileSystemNamed() {
		// #nosec G304 -- a file found in the platform's font directories, or named by the graph
		data, err := os.ReadFile(f.path)
		if err != nil {
			return nil, fmt.Errorf("reading font %s: %w", f.path, err)
		}

		return data, nil
	}

	file, err := wasm.FileSystem().Open(f.path)
	if err != nil {
		return nil, fmt.Errorf("opening font %s: %w", f.path, err)
	}

	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("reading font %s: %w", f.path, err)
	}

	return data, nil
}

// fontIndex is what the platform's font directories hold, by family.
type fontIndex struct {
	byFamily map[string][]installedFace
	// byFile is the faces of each file, by its name without extension,
	// for a graph that names a font file.
	byFile map[string][]installedFace
	faces  []installedFace
	// covers is the characters each face maps to a glyph, and family the
	// name its siblings are indexed under, in lower case: for the fallback,
	// which asks every face for a character and then the face's family for
	// the style.
	covers map[installedFace]charCoverage
	family map[installedFace]string

	// listed keeps the last names read against the index, as many as
	// maxResolved: a graph sets most of its text in a few names, and each
	// span asks.
	mu     sync.Mutex
	listed map[string]fontRequest

	// words is the most words an installed family's name has.
	words int
}

// standIns is the families a substitute may stand in for, by their names
// in lower case, and the most words one of them has; "sans-serif", of the
// generic ones, is two.
type standIns struct {
	names map[string]bool
	words int
}

// knownStandIns reads the substitutes' table once: a name asks of it for
// each of its starts, and a list for each of its entries.
//
//nolint:gochecknoglobals // the table of substitutes, read once
var knownStandIns = sync.OnceValue(func() standIns {
	const genericWords = 2

	known := standIns{names: map[string]bool{}, words: genericWords}

	for _, group := range substituteGroups() {
		for _, member := range group {
			words, _ := nameWords(member)

			known.names[strings.ToLower(member)] = true
			known.words = max(known.words, len(words))
		}
	}

	return known
})

// longestFamily is the most words a family index knows has: an installed
// one, one a substitute may stand in for, or a generic one.
func (index *fontIndex) longestFamily() int {
	if index == nil {
		return knownStandIns().words
	}

	return max(index.words, knownStandIns().words)
}

// knows reports whether a family of that name can be looked for: one that
// is installed, one a substitute may stand in for, or a generic one. A nil
// index has no installed family.
func (index *fontIndex) knows(family string) bool {
	if family == "" {
		return false
	}

	key := strings.ToLower(family)

	if index != nil && len(index.byFamily[key]) > 0 {
		return true
	}

	return knownStandIns().names[key] || genericOf(key) != ""
}

// requestFor resolves what Graphviz asks for: the font's name, the alias
// Graphviz found for it if it is a PostScript name, and the font flags of
// an HTML label.
//
// The name is read against the families index holds: see splitFamilyStyle.
func (index *fontIndex) requestFor(name string, alias *PostScriptAlias, flags uint) fontRequest {
	var request fontRequest
	if alias != nil && alias.Family() != "" {
		request = aliasRequest(alias)
	} else {
		request = index.listedRequest(name)
	}

	if flags&htmlBold != 0 && request.style.weight < weightBold {
		request.style.weight = weightBold
	}

	if flags&htmlItalic != 0 {
		request.style.italic = true
	}

	return request
}

// listedRequest is listRequest for name against the index, read once for each
// name among the last ones asked.
func (index *fontIndex) listedRequest(name string) fontRequest {
	if index == nil {
		return listRequest(name, index)
	}

	index.mu.Lock()
	defer index.mu.Unlock()

	if request, ok := index.listed[name]; ok {
		return request
	}

	if index.listed == nil || len(index.listed) >= maxResolved {
		index.listed = map[string]fontRequest{}
	}

	request := listRequest(name, index)
	index.listed[name] = request

	return request
}

// buildFontIndex indexes the font files under dirs, reading of each font
// its names and its style.
func buildFontIndex(dirs []string) *fontIndex {
	index := &fontIndex{byFamily: map[string][]installedFace{}, byFile: map[string][]installedFace{}}
	walked := map[string]bool{}

	for _, dir := range dirs {
		index.addDirectory(dir, walked)
	}

	return index
}

// addDirectory indexes the font files under dir, in name order, through
// symbolic links: dir may be one, and so may a directory or a file under
// it, as a user's font directory linked from elsewhere is, and the
// directories some systems link under share/fonts. walked holds the
// directories read, by the path their links come to, so that one reached
// twice, or a link that leads back to where it is, is read once.
//
// Only a file is opened: a pipe or a device named like a font is not one,
// and opening it would wait for whoever writes to it.
func (index *fontIndex) addDirectory(dir string, walked map[string]bool) {
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || walked[resolved] {
		return
	}

	walked[resolved] = true

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return
	}

	for _, entry := range entries {
		path := filepath.Join(resolved, entry.Name())

		// What the entry is, or leads to.
		info, err := os.Stat(path)
		if err != nil {
			continue
		}

		switch {
		case info.IsDir():
			index.addDirectory(path, walked)
		case info.Mode().IsRegular() && isFontFile(path):
			index.addFile(path)
		}
	}
}

func isFontFile(path string) bool {
	return slices.Contains(fontSuffixes, strings.ToLower(filepath.Ext(path)))
}

// addFile indexes the faces of one font file under every family name each
// goes by, in whatever language, and under the file's name. A face the font
// parser does not take is indexed too, as one that cannot be loaded: a
// family whose regular face is such a one is then known to have it, and is
// not answered for by its medium.
func (index *fontIndex) addFile(path string) {
	// #nosec G304 -- a file found in the platform's font directories
	file, err := os.Open(path)
	if err != nil {
		return
	}

	faces, err := readFaces(file)
	_ = file.Close()

	if err != nil {
		return
	}

	loadable := map[int]bool{}
	_ = withFonts(path, func(member int, _ *sfnt.Font) bool {
		loadable[member] = true

		return true
	})

	collection := strings.EqualFold(filepath.Ext(path), ".ttc")
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))

	for member, face := range faces {
		if len(face.families) == 0 || !collection && member > 0 {
			continue
		}

		if !collection {
			member = -1
		}

		entry := installedFace{path: path, index: member, style: face.style, unloadable: !loadable[member]}

		for _, family := range face.families {
			key := strings.ToLower(family)
			index.byFamily[key] = append(index.byFamily[key], entry)

			words, _ := nameWords(family)
			index.words = max(index.words, len(words))
		}

		index.byFile[name] = append(index.byFile[name], entry)
		index.faces = append(index.faces, entry)

		if index.covers == nil {
			index.covers, index.family = map[installedFace]charCoverage{}, map[installedFace]string{}
		}

		index.covers[entry], index.family[entry] = face.covers, strings.ToLower(face.families[0])
	}
}

// withFonts opens a font file and calls visit with each face it holds, read
// lazily, until visit returns false. The index is -1 for a file that is not
// a collection.
func withFonts(path string, visit func(index int, f *sfnt.Font) bool) error {
	// #nosec G304 -- a file found in the platform's font directories or named by the graph
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening font %s: %w", path, err)
	}

	defer func() { _ = file.Close() }()

	if !strings.EqualFold(filepath.Ext(path), ".ttc") {
		member, parseErr := sfnt.ParseReaderAt(file)
		if parseErr != nil {
			return fmt.Errorf("parsing font %s: %w", path, parseErr)
		}

		visit(-1, member)

		return nil
	}

	collection, err := sfnt.ParseCollectionReaderAt(file)
	if err != nil {
		return fmt.Errorf("parsing font collection %s: %w", path, err)
	}

	for i := range collection.NumFonts() {
		member, memberErr := collection.Font(i)
		if memberErr != nil {
			continue
		}

		if !visit(i, member) {
			break
		}
	}

	return nil
}

// loadable is the faces of faces the font parser takes.
func loadable(faces []installedFace) []installedFace {
	kept := make([]installedFace, 0, len(faces))

	for _, face := range faces {
		if !face.unloadable {
			kept = append(kept, face)
		}
	}

	return kept
}

// best returns the face of faces closest to style.
func best(faces []installedFace, style fontStyle) (installedFace, bool) {
	var chosen installedFace

	bestScore := -1

	for _, face := range faces {
		score := face.style.weight - style.weight
		if score < 0 {
			score = -score
		}

		if face.style.italic != style.italic {
			score += italicMismatch
		}

		if face.style.condensed != style.condensed {
			score += condensedMismatch
		}

		if bestScore < 0 || score < bestScore {
			chosen, bestScore = face, score
		}
	}

	return chosen, bestScore >= 0
}

// loadedFont is a parsed font, the source of faces at every size.
// fromLoader marks a font a FontLoader supplied, whose faces are kept
// apart, in loaderFaces.
type loadedFont struct {
	key        string
	font       *sfnt.Font
	fromLoader bool
}

// fontCaches is what font resolution keeps: for the life of the process,
// the index of the installed fonts, built on first use; and within bounds,
// the font files read, the fonts parsed from them, what each font name
// resolved to, the last faces made and the fallback for each character.
type fontCaches struct {
	installed func() *fontIndex
	fallback  func() *glyphFallback

	mu sync.Mutex
	// files holds the font files read, whole and parsed, a collection once
	// for all its members, and held is their size together: at most
	// maxFiles of them and maxBytes are kept. Nothing else here keeps a
	// font past its file: loaded, resolved and faces go when the files do.
	files    map[string]fontFile
	held     int64
	maxFiles int
	maxBytes int64
	loaded   map[string]*loadedFont
	resolved map[string]*loadedFont
	// faces holds the last faces of the fonts resolved here.
	faces map[string]font.Face
	// loaderFaces holds the last faces of fonts a FontLoader supplied.
	loaderFaces map[string]font.Face
}

func newFontCaches() *fontCaches {
	caches := &fontCaches{
		installed: sync.OnceValue(func() *fontIndex { return buildFontIndex(fontDirectories()) }),
		files:     map[string]fontFile{},
		maxFiles:  maxFontFiles,
		maxBytes:  maxFontBytes,
		loaded:    map[string]*loadedFont{},
		resolved:  map[string]*loadedFont{},
		faces:     map[string]font.Face{},

		loaderFaces: map[string]font.Face{},
	}
	caches.fallback = sync.OnceValue(func() *glyphFallback { return newGlyphFallback(caches.installed()) })

	return caches
}

// fonts holds the process's font caches; renders are serialized by the
// module, faces are not safe for concurrent use, and parsing a font once
// per process is the point.
var fonts = newFontCaches() //nolint:gochecknoglobals // process-wide caches, as the face cache before them

// parsed returns the font under key, parsing it with parse the first time.
func (c *fontCaches) parsed(key string, parse func() (*sfnt.Font, error)) (*loadedFont, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if loaded, ok := c.loaded[key]; ok {
		return loaded, nil
	}

	parsedFont, err := parse()
	if err != nil {
		return nil, err
	}

	loaded := &loadedFont{key: key, font: parsedFont}
	c.loaded[key] = loaded

	return loaded, nil
}

const (
	// maxFontFiles and maxFontBytes bound the font files kept, read whole
	// and parsed: 64 files, and 256 MiB together. A graph names its fonts,
	// by family or by path, and a character no named font has is looked for
	// in every installed one, so without a bound a process came to hold
	// every font of the machine it had ever been asked for. A file over
	// maxFontBytes by itself is kept alone.
	maxFontFiles = 64
	maxFontBytes = 256 << 20
	// maxResolved bounds the font names whose resolution is remembered.
	maxResolved = 1024
	// maxFallbackRunes bounds the characters whose fallback is remembered.
	maxFallbackRunes = 1 << 16
)

// fontFile is a font file read whole and parsed: one font, or a collection
// of them.
type fontFile struct {
	font       *sfnt.Font
	collection *sfnt.Collection
	size       int64
}

// load is face's font, from the file kept for it or read now.
func (c *fontCaches) load(face installedFace) (*loadedFont, error) {
	key := fmt.Sprintf("%s#%d", face.fileKey(), face.index)

	c.mu.Lock()
	defer c.mu.Unlock()

	if loaded, ok := c.loaded[key]; ok {
		return loaded, nil
	}

	file, err := c.file(face)
	if err != nil {
		return nil, err
	}

	member := file.font

	if file.collection != nil {
		if member, err = file.collection.Font(face.index); err != nil {
			return nil, fmt.Errorf("font %d of collection %s: %w", face.index, face.path, err)
		}
	}

	loaded := &loadedFont{key: key, font: member}
	c.loaded[key] = loaded

	return loaded, nil
}

// file is face's font file, kept or read now, as a collection or as one
// font. Keeping one more than maxFiles, or more than maxBytes together,
// first drops everything kept, so a file over maxBytes by itself is kept
// alone. The caller holds c.mu.
func (c *fontCaches) file(face installedFace) (fontFile, error) {
	path, key := face.path, face.fileKey()

	if kept, ok := c.files[key]; ok {
		return kept, nil
	}

	data, err := face.read()
	if err != nil {
		return fontFile{}, err
	}

	file := fontFile{size: int64(len(data))}

	if face.index >= 0 {
		if file.collection, err = opentype.ParseCollection(data); err != nil {
			return fontFile{}, fmt.Errorf("parsing font collection %s: %w", path, err)
		}
	} else if file.font, err = opentype.Parse(data); err != nil {
		return fontFile{}, fmt.Errorf("parsing font %s: %w", path, err)
	}

	if len(c.files) >= c.maxFiles || c.held+file.size > c.maxBytes {
		c.drop()
	}

	c.files[key] = file
	c.held += file.size

	return file, nil
}

// drop forgets every font file kept and what was made from them: the
// fonts, what names resolved to, and the faces. What a render is drawing
// with stays its own until it is done. The caller holds c.mu.
func (c *fontCaches) drop() {
	clear(c.files)
	clear(c.loaded)
	clear(c.resolved)
	clear(c.faces)

	c.held = 0
}

// embeddedFont is the Go font standing in for a generic family and style,
// the face that always answers: Go Mono for monospace, Go otherwise, in the
// four styles each has.
func embeddedFont(generic string, style fontStyle) (*loadedFont, error) {
	mono, bold, italic := generic == genericMonospace, style.weight >= weightDemi, style.italic

	var data []byte

	switch {
	case mono && bold && italic:
		data = gomonobolditalic.TTF
	case mono && bold:
		data = gomonobold.TTF
	case mono && italic:
		data = gomonoitalic.TTF
	case mono:
		data = gomono.TTF
	case bold && italic:
		data = gobolditalic.TTF
	case bold:
		data = gobold.TTF
	case italic:
		data = goitalic.TTF
	default:
		data = goregular.TTF
	}

	key := fmt.Sprintf("go mono=%t bold=%t italic=%t", mono, bold, italic)

	return fonts.parsed(key, func() (*sfnt.Font, error) {
		parsedFont, err := opentype.Parse(data)
		if err != nil {
			return nil, fmt.Errorf("parsing the embedded font %s: %w", key, err)
		}

		return parsedFont, nil
	})
}

// resolveFont finds the font for a request among index's faces: a file
// the name points to, then the candidate families, then a font file of
// that name, then the embedded fonts.
func resolveFont(index *fontIndex, name string, request fontRequest) (*loadedFont, error) {
	return fonts.resolve(index, name, request)
}

// resolve is resolveFont with the fonts it loads kept in c.
func (c *fontCaches) resolve(index *fontIndex, name string, request fontRequest) (*loadedFont, error) {
	if isFontFile(name) {
		if face, ok := c.namedFace(name, request.style); ok {
			return c.load(face)
		}
	}

	if loaded, ok := c.amongFamilies(index, request); ok {
		return loaded, nil
	}

	file := strings.ToLower(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	if face, ok := best(loadable(index.byFile[file]), request.style); ok {
		if loaded, err := c.load(face); err == nil {
			return loaded, nil
		}
	}

	return embeddedFont(request.generic, request.style)
}

// amongFamilies is the font for request among the families it names and
// their stand-ins: each family's face for the style, when it can be loaded;
// then, of each family, the nearest face that can. A family whose own face
// for the style cannot be loaded so gives way to the next one's that can,
// and answers with another of its faces only when no family has better.
func (c *fontCaches) amongFamilies(index *fontIndex, request fontRequest) (*loadedFont, bool) {
	for _, nearest := range []bool{false, true} {
		for _, wanted := range request.candidates() {
			faces := index.byFamily[strings.ToLower(wanted.family)]
			if nearest {
				faces = loadable(faces)
			}

			face, ok := best(faces, request.style)
			if !ok || face.unloadable || wanted.condensed && !face.style.condensed {
				continue
			}

			if loaded, err := c.load(face); err == nil {
				return loaded, true
			}
		}
	}

	return nil, false
}

// remember keeps loaded as what the name under key resolved to, forgetting
// every name first when it would be one more than maxResolved.
func (c *fontCaches) remember(key string, loaded *loadedFont) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.resolved) >= maxResolved {
		clear(c.resolved)
	}

	c.resolved[key] = loaded
}

// namedFace is the face, closest to style, of a font file the graph names
// by path, read through the file system the graph's files are read from.
func (c *fontCaches) namedFace(path string, style fontStyle) (installedFace, bool) {
	whole := installedFace{path: path, index: -1, named: true}
	if strings.EqualFold(filepath.Ext(path), ".ttc") {
		whole.index = 0
	}

	c.mu.Lock()
	file, err := c.file(whole)
	c.mu.Unlock()

	if err != nil {
		return installedFace{}, false
	}

	// The styles are read as the index reads them: see readFaces.
	data, err := whole.read()
	if err != nil {
		return installedFace{}, false
	}

	read, err := readFaces(bytes.NewReader(data))
	if err != nil {
		return installedFace{}, false
	}

	if file.collection == nil {
		if len(read) == 0 {
			return installedFace{}, false
		}

		return installedFace{path: path, index: -1, style: read[0].style, named: true}, true
	}

	var faces []installedFace

	for index, member := range read {
		if index >= file.collection.NumFonts() {
			break
		}

		if _, memberErr := file.collection.Font(index); memberErr == nil {
			faces = append(faces, installedFace{path: path, index: index, style: member.style, named: true})
		}
	}

	return best(faces, style)
}

// fontForSpan is the font a span is set in, for measuring and drawing
// alike: the font loader's, when one is set and answers, otherwise what the
// span's font resolves to among the installed and embedded fonts.
func fontForSpan(ctx context.Context, textFont *TextFont) (*loadedFont, error) {
	fontLoaderMu.RLock()

	loader := fontLoader

	fontLoaderMu.RUnlock()

	if loader != nil {
		supplied, err := loader(ctx, textFont)
		if err != nil {
			return nil, err
		}

		if supplied != nil {
			// The faces made from it are cached by this key, one per font.
			return &loadedFont{key: fmt.Sprintf("loader|%p", supplied), font: supplied, fromLoader: true}, nil
		}
	}

	return fontFor(textFont)
}

// styleOf is the style a span's font name and flags ask for.
func styleOf(textFont *TextFont) fontStyle {
	return fonts.installed().requestFor(textFont.Name(), textFont.PostScriptAlias(), textFont.Flags()).style
}

// fontFor resolves a span's font, once per name, alias and flags.
func fontFor(textFont *TextFont) (*loadedFont, error) {
	// A name is read against the families there are: see splitFamilyStyle.
	request := fonts.installed().requestFor(textFont.Name(), textFont.PostScriptAlias(), textFont.Flags())

	key := fmt.Sprintf("%s|%v|%s|%+v", textFont.Name(), request.families, request.generic, request.style)
	if isFontFile(textFont.Name()) {
		// A file the graph names is the one in the file system of the day.
		key = fmt.Sprintf("fs %d: %s", wasm.FileSystemGeneration(), key)
	}

	fonts.mu.Lock()
	loaded, ok := fonts.resolved[key]
	fonts.mu.Unlock()

	if ok {
		return loaded, nil
	}

	loaded, err := resolveFont(fonts.installed(), textFont.Name(), request)
	if err != nil {
		return nil, err
	}

	fonts.remember(key, loaded)

	return loaded, nil
}

// face is f at size points and dpi dots per inch, kept among the last
// made: maxFaces of the fonts resolved here, and maxLoaderFaces of the
// fonts a FontLoader supplied, which may be a new one on every call.
func (f *loadedFont) face(size, dpi float64) (font.Face, error) {
	key := fmt.Sprintf("%s|%g|%g", f.key, size, dpi)

	fonts.mu.Lock()
	defer fonts.mu.Unlock()

	cache := fonts.faces
	if f.fromLoader {
		cache = fonts.loaderFaces
	}

	if cached, ok := cache[key]; ok {
		return cached, nil
	}

	sized, err := opentype.NewFace(f.font, &opentype.FaceOptions{Size: size, DPI: dpi})
	if err != nil {
		return nil, fmt.Errorf("face for %s: %w", f.key, err)
	}

	// The pixels per em opentype makes the face at.
	face := &outlinedFace{
		Face: sized,
		font: f.font,
		ppem: fixed.Int26_6(math.Round(size * dpi * subpixel / pointsPerInch)),
	}

	limit := maxFaces
	if f.fromLoader {
		limit = maxLoaderFaces
	}

	if len(cache) >= limit {
		clear(cache)
	}

	cache[key] = face

	return face, nil
}

// outlinedFace is a face that also gives its glyphs' outlines, for the
// canvas to fill a glyph too large for a mask.
type outlinedFace struct {
	font.Face

	font *sfnt.Font
	ppem fixed.Int26_6
	buf  sfnt.Buffer
}

// Kern is how far the pen moves between left and right beside their
// advances, at the face's size. The face's own Kern gives the font's units
// for pixels, the same number at every size (it hands sfnt the units per em
// where sfnt wants the pixels per em), so a kerned label was laid out and
// drawn at two widths, neither of them the font's.
func (f *outlinedFace) Kern(left, right rune) fixed.Int26_6 {
	first, err := f.font.GlyphIndex(&f.buf, left)
	if err != nil {
		return 0
	}

	second, err := f.font.GlyphIndex(&f.buf, right)
	if err != nil {
		return 0
	}

	kern, err := f.font.Kern(&f.buf, first, second, f.ppem, font.HintingNone)
	if err != nil {
		return 0
	}

	return kern
}

func (f *outlinedFace) outline(char rune) (sfnt.Segments, bool) {
	index, err := f.font.GlyphIndex(&f.buf, char)
	if err != nil || index == 0 {
		return nil, false
	}

	segments, err := f.font.LoadGlyph(&f.buf, index, f.ppem, nil)

	return segments, err == nil
}

// maxFaces bounds the faces kept of the fonts resolved here. A face keeps
// the mask of the largest glyph it drew, and a graph asks for one at every
// size it sets a font in, scaled by its zoom: without a bound, each size a
// graph ever named stayed for the life of the process.
const maxFaces = 64

// maxLoaderFaces bounds the faces kept of fonts a FontLoader supplied, each
// of which holds its font: enough for the fonts and sizes of a graph, and a
// loader that parses afresh on every call cannot grow it past that.
const maxLoaderFaces = 16

// fallbackFamilies are tried first for a character the chosen font lacks:
// fonts with wide coverage, of Chinese, Japanese and Korean in particular.
func fallbackFamilies() []string {
	return []string{
		"Hiragino Sans", "Hiragino Kaku Gothic ProN", "PingFang SC", "Apple SD Gothic Neo",
		"Noto Sans CJK JP", "Noto Sans CJK SC", "Source Han Sans", "Yu Gothic", "Meiryo",
		"MS Gothic", "Microsoft YaHei", "Malgun Gothic", "WenQuanYi Zen Hei", "Droid Sans Fallback",
		"Arial Unicode MS", "DejaVu Sans", "Noto Sans", "Apple Symbols", "Segoe UI Symbol",
		"Noto Sans Symbols", "Noto Sans Symbols2", "Symbola",
	}
}

// glyphFallback finds, for characters a font lacks, an installed font that
// has them, and remembers the answer, including that there is none.
//
// The answer for a character is the same whatever was asked before: the
// faces are asked in one order, the wide-coverage families' and then every
// installed face as indexed, and the first that has the character with ink
// gives its family, whose face of the span's style draws it when that one
// has the character too.
type glyphFallback struct {
	mu    sync.Mutex
	index *fontIndex
	// order is the faces a character is looked for in.
	order []installedFace
	// byRune is the face that has each character asked for, in each style
	// it was asked in, nil for none, for the last maxFallbackRunes of them:
	// the face and not its font, which is loaded when it is asked for and
	// kept as its file is.
	byRune map[styledRune]*installedFace
}

// styledRune is a character in the style of the span that has it.
type styledRune struct {
	char  rune
	style fontStyle
}

func newGlyphFallback(index *fontIndex) *glyphFallback {
	fallback := &glyphFallback{index: index, byRune: map[styledRune]*installedFace{}}

	for _, family := range fallbackFamilies() {
		if face, ok := best(index.byFamily[strings.ToLower(family)], fontStyle{weight: weightRegular}); ok {
			fallback.order = append(fallback.order, face)
		}
	}

	fallback.order = append(fallback.order, index.faces...)

	return fallback
}

// fontsFor returns a font for each of chars that some installed font has a
// drawable glyph for, in style where the font's family has it so.
func (g *glyphFallback) fontsFor(chars []rune, style fontStyle) map[rune]*loadedFont {
	g.mu.Lock()
	defer g.mu.Unlock()

	result := map[rune]*loadedFont{}

	var missing []rune

	for _, char := range chars {
		face, known := g.byRune[styledRune{char, style}]

		switch {
		case !known:
			missing = append(missing, char)
		case face != nil:
			if loaded, err := fonts.load(*face); err == nil {
				result[char] = loaded
			}
		}
	}

	// placeholders is, for a character, the first face that has an outline
	// for it that encloses nothing: what draws it when no face does better.
	placeholders := map[rune]installedFace{}

	for _, candidate := range g.order {
		if len(missing) == 0 {
			break
		}

		missing = g.take(candidate, style, missing, result, placeholders)
	}

	for _, char := range missing {
		var answer *installedFace

		if face, ok := placeholders[char]; ok {
			if loaded, err := fonts.load(face); err == nil {
				answer, result[char] = &face, loaded
			}
		}

		g.remember(styledRune{char, style}, answer)
	}

	return result
}

// remember keeps face as the answer for char, forgetting every answer first
// when it would be one more than maxFallbackRunes.
func (g *glyphFallback) remember(char styledRune, face *installedFace) {
	if len(g.byRune) >= maxFallbackRunes {
		clear(g.byRune)
	}

	g.byRune[char] = face
}

// take records the characters of missing that candidate draws, in result
// and for later, and returns those it does not: each from the face of
// candidate's family in style when that face draws it too, from candidate
// otherwise. A character candidate has a placeholder for is not drawn by it,
// and candidate is noted in placeholders as the first to have that much.
func (g *glyphFallback) take(
	candidate installedFace,
	style fontStyle,
	missing []rune,
	result map[rune]*loadedFont,
	placeholders map[rune]installedFace,
) []rune {
	has := g.glyphs(candidate, missing)
	if len(has) == 0 {
		return missing
	}

	styled := has

	sibling, ok := best(g.index.byFamily[g.index.family[candidate]], style)
	if ok && sibling != candidate {
		styled = g.glyphs(sibling, slices.Collect(maps.Keys(has)))
	}

	var still []rune

	for _, char := range missing {
		face := candidate
		if styled[char].drawn() {
			face = sibling
		}

		if _, noted := placeholders[char]; has[char] == placeholderGlyph && !noted {
			placeholders[char] = candidate
		}

		if !has[char].drawn() {
			still = append(still, char)

			continue
		}

		if loaded, err := fonts.load(face); err == nil {
			g.remember(styledRune{char, style}, &face)
			result[char] = loaded
		}
	}

	return still
}

// glyphKind is what a face has for a character; none, at zero.
type glyphKind int

const (
	// placeholderGlyph: an outline that encloses nothing. A colour emoji
	// font keeps one, two points an em apart, for each of its pictures,
	// which are bitmaps the renderer does not draw; taken for the
	// character, it left it blank where another font had it drawn.
	placeholderGlyph glyphKind = iota + 1
	// blankGlyph: no outline at all. The character is blank in the font, as
	// a space is, a variation selector, a filler, the braille cell with no
	// dot: that is how it is drawn, and no other font is asked.
	blankGlyph
	// inkGlyph: an outline that encloses something.
	inkGlyph
)

// drawn reports whether the face draws the character as it means to.
func (kind glyphKind) drawn() bool {
	return kind == blankGlyph || kind == inkGlyph
}

// glyphs is what face has for each of chars it has: those its table maps,
// which costs nothing to ask, looked at in the font, read then.
func (g *glyphFallback) glyphs(face installedFace, chars []rune) map[rune]glyphKind {
	if face.unloadable {
		return nil
	}

	covers, mapped := g.index.covers[face], chars[:0:0]

	for _, char := range chars {
		if covers.has(char) {
			mapped = append(mapped, char)
		}
	}

	if len(mapped) == 0 {
		return nil
	}

	return glyphKinds(face, mapped)
}

// glyphKinds is the kind of glyph face has for each of chars it has one
// for, reading the font lazily.
func glyphKinds(face installedFace, chars []rune) map[rune]glyphKind {
	kinds := map[rune]glyphKind{}

	_ = withFonts(face.path, func(index int, member *sfnt.Font) bool {
		if index != face.index {
			return true
		}

		var buf sfnt.Buffer

		for _, char := range chars {
			glyph, err := member.GlyphIndex(&buf, char)
			if err != nil || glyph == 0 {
				continue
			}

			segments, err := member.LoadGlyph(&buf, glyph, fixed.I(int(member.UnitsPerEm())), nil)

			switch {
			case err != nil:
			case len(segments) == 0:
				kinds[char] = blankGlyph
			case encloses(segments):
				kinds[char] = inkGlyph
			default:
				kinds[char] = placeholderGlyph
			}
		}

		return false
	})

	return kinds
}

// encloses reports whether an outline encloses anything: whether a contour
// of it has three points, those its curves are drawn towards included, that
// are not on one line. A point, or a line gone over and back, fills nothing.
func encloses(segments sfnt.Segments) bool {
	var start, last fixed.Point26_6

	for _, segment := range segments {
		points := segment.Args[:1]

		switch segment.Op {
		case sfnt.SegmentOpMoveTo:
			start, last = segment.Args[0], segment.Args[0]

			continue
		case sfnt.SegmentOpQuadTo:
			points = segment.Args[:2]
		case sfnt.SegmentOpCubeTo:
			points = segment.Args[:3]
		case sfnt.SegmentOpLineTo:
		}

		for _, point := range points {
			// Twice the area of the triangle from the contour's start.
			a, b := last.Sub(start), point.Sub(start)
			if int64(a.X)*int64(b.Y) != int64(a.Y)*int64(b.X) {
				return true
			}

			last = point
		}
	}

	return false
}

// textRun is a stretch of text drawn with one face.
type textRun struct {
	face font.Face
	text string
}

// runs splits text into runs: the characters primary has a glyph for stay
// with it, a symbol-encoded font's characters move to its private use area,
// and the others go to the installed font the fallback gives for them in
// style, the span's, or stay with primary when there is none.
func (g *glyphFallback) runs(text string, primary font.Face, style fontStyle, size, dpi float64) []textRun {
	chars := []rune(text)
	mapped, faces, missing := classify(chars, primary)

	if len(missing) > 0 {
		found := g.fontsFor(missing, style)

		for i, char := range chars {
			if faces[i] == nil {
				faces[i] = fallbackFace(found[char], primary, size, dpi)
			}
		}
	}

	var result []textRun

	start := 0

	for i := 1; i <= len(chars); i++ {
		if i == len(chars) || faces[i] != faces[start] {
			result = append(result, textRun{face: faces[start], text: string(mapped[start:i])})
			start = i
		}
	}

	return result
}

// fallbackFace is loaded at size and dpi, or primary when there is no
// fallback font or no face of it.
func fallbackFace(loaded *loadedFont, primary font.Face, size, dpi float64) font.Face {
	if loaded == nil {
		return primary
	}

	face, err := loaded.face(size, dpi)
	if err != nil {
		return primary
	}

	return face
}

// classify gives each character the character to draw and primary when
// primary can draw it, directly or at a symbol font's private use code,
// and nil with the character listed in missing otherwise.
func classify(chars []rune, primary font.Face) (mapped []rune, faces []font.Face, missing []rune) {
	mapped = slices.Clone(chars)
	faces = make([]font.Face, len(chars))

	for i, char := range chars {
		faces[i] = primary

		if _, ok := primary.GlyphAdvance(char); ok || unicode.IsSpace(char) || !unicode.IsPrint(char) {
			continue
		}

		if char < symbolCodes {
			if _, ok := primary.GlyphAdvance(symbolBase + char); ok {
				mapped[i] = symbolBase + char

				continue
			}
		}

		faces[i] = nil

		missing = append(missing, char)
	}

	return mapped, faces, missing
}

// advance is how far text set in runs moves the pen, in the faces' units:
// pixels at the faces' resolution, points at 72 dpi.
func advance(runs []textRun) float64 {
	total := 0.0
	for _, run := range runs {
		total += float64(font.MeasureString(run.face, run.text)) / fixedOne
	}

	return total
}
