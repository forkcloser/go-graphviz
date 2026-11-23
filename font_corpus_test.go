package graphviz_test

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"testing"
	"unicode/utf16"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

// The fonts of a machine are not the test's to choose, so the tests of how
// a font name is resolved bring their own: Go Regular under other names,
// each with an advance for the letter A that no real font has and no other
// font of the corpus shares. A label of ten A is then as wide as the font
// that was chosen makes it, and its width says which one it was.

// corpusFace is one font of a test's corpus.
type corpusFace struct {
	// family and subfamily are name records 1 and 2; typoFamily and
	// typoSubfamily, when set, 16 and 17.
	family, subfamily         string
	typoFamily, typoSubfamily string
	// advance is how far an A moves the pen, in the font's units of 2048 to
	// the em.
	advance uint16
	// weight and width are the classes of the OS/2 table, 400 and 5 when
	// left at zero, and italic its italic flag: a font says its style in
	// numbers there, beside the words of its subfamily.
	weight, width uint16
	italic        bool
	// names are records beside the English ones: the same names in other
	// languages, or on another platform.
	names []nameRecord
	// noEnglish leaves the English records out: the font has its names in
	// the other records alone.
	noEnglish bool
	// unparsable gives the font a character map the font parser does not
	// take: the face is there and cannot be drawn with.
	unparsable bool
}

// corpusSize is the font size at which a label of ten A is a tenth of the
// A's advance wide, in points.
const corpusSize = "20.48"

// corpusLabel is the label the corpus tests set.
const corpusLabel = "AAAAAAAAAA"

// tablesOf is the tables of a TrueType font, by tag.
func tablesOf(t *testing.T, ttf []byte) map[string][]byte {
	t.Helper()

	const (
		offsetTable = 12
		record      = 16
	)

	tables := map[string][]byte{}

	for i := range int(binary.BigEndian.Uint16(ttf[4:])) {
		entry := ttf[offsetTable+record*i:]
		offset, length := binary.BigEndian.Uint32(entry[8:]), binary.BigEndian.Uint32(entry[12:])

		tables[string(entry[:4])] = ttf[offset : offset+length]
	}

	return tables
}

// fontOf is the TrueType font made of tables: its directory in tag order,
// each table at a multiple of four bytes. The checksums are left at zero,
// which the reader does not ask for.
func fontOf(tables map[string][]byte) []byte {
	const (
		offsetTable = 12
		record      = 16
	)

	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}

	sort.Strings(tags)

	selector := 0
	for 1<<(selector+1) <= len(tags) {
		selector++
	}

	out := []byte{0, 1, 0, 0}
	out = binary.BigEndian.AppendUint16(out, uint16(len(tags)))
	out = binary.BigEndian.AppendUint16(out, uint16(record<<selector))
	out = binary.BigEndian.AppendUint16(out, uint16(selector))
	out = binary.BigEndian.AppendUint16(out, uint16(record*len(tags)-record<<selector))

	offset := offsetTable + record*len(tags)

	for _, tag := range tags {
		out = append(out, tag...)
		out = binary.BigEndian.AppendUint32(out, 0)
		out = binary.BigEndian.AppendUint32(out, uint32(offset))
		out = binary.BigEndian.AppendUint32(out, uint32(len(tables[tag])))

		offset += (len(tables[tag]) + 3) &^ 3
	}

	for _, tag := range tags {
		out = append(out, tables[tag]...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}

	return out
}

// nameRecord is one record of a name table: a name in a language.
type nameRecord struct {
	// language is the Windows language of the record: 0x409 is English of
	// the United States.
	language uint16
	id       sfnt.NameID
	text     string
	// unicode puts the record on the Unicode platform, which has no
	// language, and not on the Windows one.
	unicode bool
}

const english = 0x409

// nameTable is a name table of Windows Unicode records, in the order the
// format wants them.
func nameTable(records []nameRecord) []byte {
	const (
		header  = 6
		record  = 12
		windows = 3
		unicode = 1
	)

	records = slices.Clone(records)
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].unicode != records[j].unicode {
			return records[i].unicode
		}

		if records[i].language != records[j].language {
			return records[i].language < records[j].language
		}

		return records[i].id < records[j].id
	})

	out := binary.BigEndian.AppendUint16(nil, 0)
	out = binary.BigEndian.AppendUint16(out, uint16(len(records)))
	out = binary.BigEndian.AppendUint16(out, uint16(header+record*len(records)))

	var strings []byte

	for _, entry := range records {
		text := utf16.Encode([]rune(entry.text))

		if entry.unicode {
			// The Unicode platform, in its encoding for the basic plane.
			out = binary.BigEndian.AppendUint16(out, 0)
			out = binary.BigEndian.AppendUint16(out, 3)
			out = binary.BigEndian.AppendUint16(out, 0)
		} else {
			out = binary.BigEndian.AppendUint16(out, windows)
			out = binary.BigEndian.AppendUint16(out, unicode)
			out = binary.BigEndian.AppendUint16(out, entry.language)
		}

		out = binary.BigEndian.AppendUint16(out, uint16(entry.id))
		out = binary.BigEndian.AppendUint16(out, uint16(2*len(text)))
		out = binary.BigEndian.AppendUint16(out, uint16(len(strings)))

		for _, unit := range text {
			strings = binary.BigEndian.AppendUint16(strings, unit)
		}
	}

	return append(out, strings...)
}

// tables is the face as the tables of a TrueType font: Go Regular with the
// face's names, its style in the OS/2 table and its advance for A.
func (face corpusFace) tables(t *testing.T) map[string][]byte {
	t.Helper()

	tables := tablesOf(t, goregular.TTF)

	var records []nameRecord

	if !face.noEnglish {
		records = []nameRecord{
			{language: english, id: sfnt.NameIDFamily, text: face.family},
			{language: english, id: sfnt.NameIDSubfamily, text: face.subfamily},
			{language: english, id: sfnt.NameIDFull, text: face.family + " " + face.subfamily},
		}

		if face.typoFamily != "" {
			records = append(
				records,
				nameRecord{language: english, id: sfnt.NameIDTypographicFamily, text: face.typoFamily},
			)
		}

		if face.typoSubfamily != "" {
			records = append(records,
				nameRecord{language: english, id: sfnt.NameIDTypographicSubfamily, text: face.typoSubfamily})
		}
	}

	tables["name"] = nameTable(append(records, face.names...))

	parsed, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}

	var buf sfnt.Buffer

	glyph, err := parsed.GlyphIndex(&buf, 'A')
	if err != nil || glyph == 0 {
		t.Fatalf("no glyph for A: %v", err)
	}

	// hmtx holds an advance and a side bearing for each of the first
	// glyphs, as many as hhea says at its end.
	const (
		metricsCount = 34
		metric       = 4
	)

	hhea, hmtx := tables["hhea"], slices.Clone(tables["hmtx"])
	if len(hhea) < metricsCount+2 || len(hmtx) < metric*(int(glyph)+1) ||
		int(glyph) >= int(binary.BigEndian.Uint16(hhea[metricsCount:])) {
		t.Fatal("the glyph for A has no advance of its own")
	}

	binary.BigEndian.PutUint16(hmtx[metric*int(glyph):], face.advance)
	tables["hmtx"] = hmtx

	// OS/2: the weight class, the width class and the selection flags,
	// whose first bit is italic.
	const (
		weightClass = 4
		widthClass  = 6
		selection   = 62
	)

	os2 := slices.Clone(tables["OS/2"])
	if len(os2) < selection+2 {
		t.Fatal("the font has no OS/2 table to say its style in")
	}

	weight, width := face.weight, face.width
	if weight == 0 {
		weight = 400
	}

	if width == 0 {
		width = 5
	}

	binary.BigEndian.PutUint16(os2[weightClass:], weight)
	binary.BigEndian.PutUint16(os2[widthClass:], width)

	flags := binary.BigEndian.Uint16(os2[selection:]) &^ 1
	if face.italic {
		flags |= 1
	}

	binary.BigEndian.PutUint16(os2[selection:], flags)
	tables["OS/2"] = os2

	if face.unparsable {
		// One subtable, for the Macintosh's Japanese: a version, a count,
		// then platform 1, encoding 1 and where the subtable is; the
		// subtable in format 0, 262 bytes of it.
		cmap := []byte{0, 0, 0, 1, 0, 1, 0, 1, 0, 0, 0, 12, 0, 0, 1, 6, 0, 0}
		tables["cmap"] = append(cmap, make([]byte, 256)...)
	}

	return tables
}

// font is the face as a TrueType font file.
func (face corpusFace) font(t *testing.T) []byte {
	t.Helper()

	return fontOf(face.tables(t))
}

// collectionOf is the faces as one TrueType collection, in their order.
func collectionOf(t *testing.T, faces ...corpusFace) []byte {
	t.Helper()

	const (
		offsetTable = 12
		record      = 16
	)

	out := []byte("ttcf")
	out = binary.BigEndian.AppendUint32(out, 0x00010000)
	out = binary.BigEndian.AppendUint32(out, uint32(len(faces)))

	offsets := len(out)
	out = append(out, make([]byte, 4*len(faces))...)

	for i, face := range faces {
		binary.BigEndian.PutUint32(out[offsets+4*i:], uint32(len(out)))

		// The font as a file of its own, each table moved by where the
		// font starts in the collection.
		font, start := fontOf(face.tables(t)), len(out)

		if len(font) < offsetTable {
			t.Fatal("no font to put in the collection")
		}

		for table := range int(binary.BigEndian.Uint16(font[4:])) {
			at := offsetTable + record*table + 8
			if len(font) < at+4 {
				t.Fatal("the font's table directory is cut short")
			}

			binary.BigEndian.PutUint32(font[at:], binary.BigEndian.Uint32(font[at:])+uint32(start))
		}

		out = append(out, font...)
	}

	return out
}

// fontCorpusEnv marks the process a corpus test runs in.
const fontCorpusEnv = "GO_GRAPHVIZ_TEST_FONT_CORPUS"

// inFontCorpus runs test in a process of its own whose user's font
// directory holds faces and nothing else. The installed fonts are indexed
// once in a process, so the process is started for them; the machine's own
// fonts are there too, which is why the corpus has names and widths no real
// font has.
func inFontCorpus(t *testing.T, faces []corpusFace, test func(t *testing.T)) {
	t.Helper()

	files := map[string][]byte{}

	if os.Getenv(fontCorpusEnv) == "" {
		for i, face := range faces {
			files[fmt.Sprintf("corpus%02d.ttf", i)] = face.font(t)
		}
	}

	inFontFiles(t, files, test)
}

// inFontFiles is inFontCorpus for font files made by the test, by name.
func inFontFiles(t *testing.T, files map[string][]byte, test func(t *testing.T)) {
	t.Helper()

	if os.Getenv(fontCorpusEnv) != "" {
		test(t)

		return
	}

	home := t.TempDir()
	dir := userFontDirectory(home)

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cmd := child(t, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")

	cmd.Env = append(os.Environ(), fontCorpusEnv+"=1", "HOME="+home, "USERPROFILE="+home, "LOCALAPPDATA="+home)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the process with the font corpus: %v\n%s", err, out)
	}
}

// userFontDirectory is where the platform reads the fonts of the user whose
// home, and local application data on Windows, is home.
func userFontDirectory(home string) string {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Fonts")
	case "windows":
		return filepath.Join(home, "Microsoft", "Windows", "Fonts")
	default:
		return filepath.Join(home, ".fonts")
	}
}

// chosenAdvance is the advance of an A in the font that name resolves to:
// the width a label of ten A is laid out at, times ten.
func chosenAdvance(t *testing.T, name string) float64 {
	t.Helper()

	dot := fmt.Sprintf(`digraph { node [shape=box margin=0 width=0 height=0]; a [label=%q fontname=%q fontsize=%s] }`,
		corpusLabel, name, corpusSize)

	// To the nearest ten units: the corpus's advances are forty apart, and
	// the layout rounds a width to a hundredth of a point.
	return math.Round(nodeWidth(t, dot)) * 10
}
