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

		out = binary.BigEndian.AppendUint16(out, windows)
		out = binary.BigEndian.AppendUint16(out, unicode)
		out = binary.BigEndian.AppendUint16(out, entry.language)
		out = binary.BigEndian.AppendUint16(out, uint16(entry.id))
		out = binary.BigEndian.AppendUint16(out, uint16(2*len(text)))
		out = binary.BigEndian.AppendUint16(out, uint16(len(strings)))

		for _, unit := range text {
			strings = binary.BigEndian.AppendUint16(strings, unit)
		}
	}

	return append(out, strings...)
}

// font is the face as a TrueType font: Go Regular with the face's names and
// its advance for A.
func (face corpusFace) font(t *testing.T) []byte {
	t.Helper()

	tables := tablesOf(t, goregular.TTF)

	records := []nameRecord{
		{english, sfnt.NameIDFamily, face.family},
		{english, sfnt.NameIDSubfamily, face.subfamily},
		{english, sfnt.NameIDFull, face.family + " " + face.subfamily},
	}

	if face.typoFamily != "" {
		records = append(records, nameRecord{english, sfnt.NameIDTypographicFamily, face.typoFamily})
	}

	if face.typoSubfamily != "" {
		records = append(records, nameRecord{english, sfnt.NameIDTypographicSubfamily, face.typoSubfamily})
	}

	tables["name"] = nameTable(records)

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

	return fontOf(tables)
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

	if os.Getenv(fontCorpusEnv) != "" {
		test(t)

		return
	}

	home := t.TempDir()

	var dir string

	switch runtime.GOOS {
	case "darwin":
		dir = filepath.Join(home, "Library", "Fonts")
	case "windows":
		dir = filepath.Join(home, "Microsoft", "Windows", "Fonts")
	default:
		dir = filepath.Join(home, ".fonts")
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	for i, face := range faces {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("corpus%02d.ttf", i)), face.font(t), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cmd := child(t, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")

	cmd.Env = append(os.Environ(), fontCorpusEnv+"=1", "HOME="+home, "USERPROFILE="+home, "LOCALAPPDATA="+home)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the process with the font corpus: %v\n%s", err, out)
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
