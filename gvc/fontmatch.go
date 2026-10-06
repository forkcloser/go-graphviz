package gvc

import (
	"fmt"
	"io/fs"
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
	case "extrabold", "ultrabold", "heavy":
		return weightExtraBold
	case "black":
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
	case "condensed", "narrow", "compressed":
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

// parseStyle reads a subfamily such as "Bold Italic", "Condensed Light" or
// "W6".
func parseStyle(subfamily string) fontStyle {
	style := fontStyle{weight: weightRegular}

	for _, word := range strings.FieldsFunc(subfamily, isNameSeparator) {
		applyStyleWord(word, &style)
	}

	return style
}

func isNameSeparator(char rune) bool { return char == ' ' || char == '-' || char == '_' }

// splitFamilyStyle reads one font name of a list: a family followed by
// style words, separated by spaces or hyphens ("Arial Bold", "Go-Italic"),
// or by colons as in a fontconfig pattern ("DejaVu Sans:bold").
func splitFamilyStyle(name string) (string, fontStyle) {
	style := fontStyle{weight: weightRegular}

	family, styles, _ := strings.Cut(name, ":")
	for _, word := range strings.FieldsFunc(styles, func(char rune) bool { return char == ':' || char == ' ' }) {
		applyStyleWord(word, &style)
	}

	words := strings.FieldsFunc(family, isNameSeparator)

	end := len(words)
	for end > 1 && applyStyleWord(words[end-1], &style) {
		end--
	}

	return strings.Join(words[:end], " "), style
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

// requestFor resolves what Graphviz asks for: the font's name, the alias
// Graphviz found for it if it is a PostScript name, and the font flags of
// an HTML label.
func requestFor(name string, alias *PostScriptAlias, flags uint) fontRequest {
	var request fontRequest
	if alias != nil && alias.Family() != "" {
		request = aliasRequest(alias)
	} else {
		request = listRequest(name)
	}

	if flags&htmlBold != 0 && request.style.weight < weightBold {
		request.style.weight = weightBold
	}

	if flags&htmlItalic != 0 {
		request.style.italic = true
	}

	return request
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
// families, the first with the style, possibly ending with a generic family.
func listRequest(name string) fontRequest {
	request := fontRequest{style: fontStyle{weight: weightRegular}}

	for i, entry := range strings.Split(name, ",") {
		entry = strings.TrimSpace(entry)
		if generic := genericOf(entry); generic != "" {
			if request.generic == "" {
				request.generic = generic
			}

			continue
		}

		family, style := splitFamilyStyle(entry)
		if family == "" {
			continue
		}

		if i == 0 {
			request.style = style
		}

		request.families = append(request.families, family)
	}

	return request
}

// substitutesOf lists, for a family a graph names, the families with the
// same metrics or the same design that may be installed instead, the way
// fontconfig substitutes them for Graphviz's Pango plugin, or nil.
func substitutesOf(family string) []string {
	groups := [][]string{
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

	for _, group := range groups {
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

// candidates lists the families to look for, in order: each named family
// with its substitutes, then the generic family's. A condensed request
// tries the narrow cut of every one of them before any regular cut.
func (request fontRequest) candidates() []string {
	var families []string

	seen := map[string]bool{}
	add := func(family string) {
		if key := strings.ToLower(family); !seen[key] {
			seen[key] = true

			families = append(families, family)
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
				add(member + " Narrow")
				add(member + " Condensed")
			}
		}
	}

	for _, group := range groups {
		for _, member := range group {
			add(member)
		}
	}

	return families
}

// installedFace is one face of an installed font file.
type installedFace struct {
	path string
	// index is the face's position in a collection, -1 for a font file
	// that is not one.
	index int
	style fontStyle
}

// fontIndex is what the platform's font directories hold, by family.
type fontIndex struct {
	byFamily map[string][]installedFace
	// byFile is the faces of each file, by its name without extension,
	// for a graph that names a font file.
	byFile map[string][]installedFace
	faces  []installedFace
}

// buildFontIndex indexes the font files under dirs, reading only each
// font's name table.
func buildFontIndex(dirs []string) *fontIndex {
	index := &fontIndex{byFamily: map[string][]installedFace{}, byFile: map[string][]installedFace{}}

	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !isFontFile(path) {
				return nil //nolint:nilerr // an unreadable entry is skipped, not fatal
			}

			index.addFile(path)

			return nil
		})
	}

	return index
}

func isFontFile(path string) bool {
	return slices.Contains(fontSuffixes, strings.ToLower(filepath.Ext(path)))
}

// addFile indexes the faces of one font file under their family names,
// typographic and legacy, and under the file's name.
func (index *fontIndex) addFile(path string) {
	file := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))

	_ = withFonts(path, func(face int, member *sfnt.Font) bool {
		var buf sfnt.Buffer

		subfamily := firstName(member, &buf, sfnt.NameIDTypographicSubfamily, sfnt.NameIDSubfamily)
		entry := installedFace{path: path, index: face, style: parseStyle(subfamily)}

		families := map[string]bool{}

		for _, id := range []sfnt.NameID{sfnt.NameIDTypographicFamily, sfnt.NameIDFamily} {
			if family, err := member.Name(&buf, id); err == nil && family != "" {
				families[strings.ToLower(family)] = true
			}
		}

		for family := range families {
			index.byFamily[family] = append(index.byFamily[family], entry)
		}

		index.byFile[file] = append(index.byFile[file], entry)
		index.faces = append(index.faces, entry)

		return true
	})
}

func firstName(f *sfnt.Font, buf *sfnt.Buffer, ids ...sfnt.NameID) string {
	for _, id := range ids {
		if name, err := f.Name(buf, id); err == nil && name != "" {
			return name
		}
	}

	return ""
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
type loadedFont struct {
	key  string
	font *sfnt.Font
}

// fontCaches is what font resolution keeps for the life of the process:
// the index of the installed fonts, built on first use, the fonts parsed
// from it, what each font name resolved to, the faces made at each size
// and the fallback for each character.
type fontCaches struct {
	installed func() *fontIndex
	fallback  func() *glyphFallback

	mu       sync.Mutex
	loaded   map[string]*loadedFont
	resolved map[string]*loadedFont
	faces    map[string]font.Face
}

func newFontCaches() *fontCaches {
	caches := &fontCaches{
		installed: sync.OnceValue(func() *fontIndex { return buildFontIndex(fontDirectories()) }),
		loaded:    map[string]*loadedFont{},
		resolved:  map[string]*loadedFont{},
		faces:     map[string]font.Face{},
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

// load reads an installed face once.
func (face installedFace) load() (*loadedFont, error) {
	return fonts.parsed(fmt.Sprintf("%s#%d", face.path, face.index), func() (*sfnt.Font, error) {
		// #nosec G304 -- a file found in the platform's font directories or named by the graph
		data, err := os.ReadFile(face.path)
		if err != nil {
			return nil, fmt.Errorf("reading font %s: %w", face.path, err)
		}

		if face.index < 0 {
			parsedFont, parseErr := opentype.Parse(data)
			if parseErr != nil {
				return nil, fmt.Errorf("parsing font %s: %w", face.path, parseErr)
			}

			return parsedFont, nil
		}

		collection, err := opentype.ParseCollection(data)
		if err != nil {
			return nil, fmt.Errorf("parsing font collection %s: %w", face.path, err)
		}

		member, err := collection.Font(face.index)
		if err != nil {
			return nil, fmt.Errorf("font %d of collection %s: %w", face.index, face.path, err)
		}

		return member, nil
	})
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
	if info, err := os.Stat(name); err == nil && !info.IsDir() && isFontFile(name) {
		if face, ok := fileFace(name, request.style); ok {
			return face.load()
		}
	}

	for _, family := range request.candidates() {
		if face, ok := best(index.byFamily[strings.ToLower(family)], request.style); ok {
			if loaded, err := face.load(); err == nil {
				return loaded, nil
			}
		}
	}

	file := strings.ToLower(strings.TrimSuffix(filepath.Base(name), filepath.Ext(name)))
	if face, ok := best(index.byFile[file], request.style); ok {
		if loaded, err := face.load(); err == nil {
			return loaded, nil
		}
	}

	return embeddedFont(request.generic, request.style)
}

// fileFace is the face, closest to style, of a font file the graph names by
// path.
func fileFace(path string, style fontStyle) (installedFace, bool) {
	var faces []installedFace

	_ = withFonts(path, func(index int, member *sfnt.Font) bool {
		var buf sfnt.Buffer

		subfamily := firstName(member, &buf, sfnt.NameIDTypographicSubfamily, sfnt.NameIDSubfamily)
		faces = append(faces, installedFace{path: path, index: index, style: parseStyle(subfamily)})

		return true
	})

	return best(faces, style)
}

// fontFor resolves a span's font, once per name, alias and flags.
func fontFor(textFont *TextFont) (*loadedFont, error) {
	request := requestFor(textFont.Name(), textFont.PostScriptAlias(), textFont.Flags())
	key := fmt.Sprintf("%s|%v|%s|%+v", textFont.Name(), request.families, request.generic, request.style)

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

	fonts.mu.Lock()
	fonts.resolved[key] = loaded
	fonts.mu.Unlock()

	return loaded, nil
}

// face is f at size points and dpi dots per inch, made once.
func (f *loadedFont) face(size, dpi float64) (font.Face, error) {
	key := fmt.Sprintf("%s|%g|%g", f.key, size, dpi)

	fonts.mu.Lock()
	defer fonts.mu.Unlock()

	if cached, ok := fonts.faces[key]; ok {
		return cached, nil
	}

	face, err := opentype.NewFace(f.font, &opentype.FaceOptions{Size: size, DPI: dpi})
	if err != nil {
		return nil, fmt.Errorf("face for %s: %w", f.key, err)
	}

	fonts.faces[key] = face

	return face, nil
}

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
type glyphFallback struct {
	mu     sync.Mutex
	index  *fontIndex
	byRune map[rune]*loadedFont
	// found is the fonts that answered before, tried first.
	found []installedFace
}

func newGlyphFallback(index *fontIndex) *glyphFallback {
	return &glyphFallback{index: index, byRune: map[rune]*loadedFont{}}
}

// fontsFor returns a font for each of chars that some installed font has a
// drawable glyph for.
func (g *glyphFallback) fontsFor(chars []rune) map[rune]*loadedFont {
	g.mu.Lock()
	defer g.mu.Unlock()

	result := map[rune]*loadedFont{}

	var missing []rune

	for _, char := range chars {
		loaded, known := g.byRune[char]

		switch {
		case !known:
			missing = append(missing, char)
		case loaded != nil:
			result[char] = loaded
		}
	}

	for _, candidate := range g.candidates(len(missing) > 0) {
		if len(missing) == 0 {
			break
		}

		missing = g.take(candidate, missing, result)
	}

	for _, char := range missing {
		g.byRune[char] = nil
	}

	return result
}

// candidates lists the faces to search, if any: the fonts that answered
// before, the wide-coverage families, then every installed face.
func (g *glyphFallback) candidates(needed bool) []installedFace {
	if !needed {
		return nil
	}

	candidates := slices.Clone(g.found)

	for _, family := range fallbackFamilies() {
		if face, ok := best(g.index.byFamily[strings.ToLower(family)], fontStyle{weight: weightRegular}); ok {
			candidates = append(candidates, face)
		}
	}

	return append(candidates, g.index.faces...)
}

// take records the characters of missing that candidate has, in result
// and for later, and returns those it does not have.
func (g *glyphFallback) take(candidate installedFace, missing []rune, result map[rune]*loadedFont) []rune {
	covered := coverage(candidate, missing)
	if len(covered) == 0 {
		return missing
	}

	loaded, err := candidate.load()
	if err != nil {
		return missing
	}

	g.found = append(g.found, candidate)

	var still []rune

	for _, char := range missing {
		if covered[char] {
			g.byRune[char] = loaded
			result[char] = loaded
		} else {
			still = append(still, char)
		}
	}

	return still
}

// coverage reports which of chars face has an outline glyph for, reading
// the font lazily; a font with bitmap glyphs only, such as a colour emoji
// font, has none the renderer can draw.
func coverage(face installedFace, chars []rune) map[rune]bool {
	covered := map[rune]bool{}

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

			if _, err := member.LoadGlyph(&buf, glyph, fixed.I(int(member.UnitsPerEm())), nil); err == nil {
				covered[char] = true
			}
		}

		return false
	})

	return covered
}

// textRun is a stretch of text drawn with one face.
type textRun struct {
	face font.Face
	text string
}

// runs splits text into runs: the characters primary has a glyph for stay
// with it, a symbol-encoded font's characters move to its private use area,
// and the others go to the first installed font that has them, or stay
// with primary when none does.
func (g *glyphFallback) runs(text string, primary font.Face, size, dpi float64) []textRun {
	chars := []rune(text)
	mapped, faces, missing := classify(chars, primary)

	if len(missing) > 0 {
		found := g.fontsFor(missing)

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
