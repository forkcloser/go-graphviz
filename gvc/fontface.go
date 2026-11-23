package gvc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode/utf16"
)

// faceNames is what the index needs of one face of a font file: the family
// names it goes by and its style. It is read from the face's name, OS/2
// and head tables, by this file and not by the font parser, for three
// reasons the parser gave. It returns one name for an identifier, the first
// record of the table whatever its language, so a font whose Catalan
// records come first was "Negreta" and not "Bold", and was found under its
// Catalan family alone. It reads no record of the Unicode platform, so a
// font with no other had no family. And it parses a face whole or not at
// all, so the members of a collection it cannot draw were not known to be
// there, and the family answered with the members that were left.
type faceNames struct {
	// families is every family name of the face, in every language, as
	// written.
	families []string
	style    fontStyle
}

var errFontFile = errors.New("not a font file this reader knows")

// The parts of a font file read here, by the OpenType specification.
const (
	collectionTag = "ttcf"
	// The header of a collection: its tag, its version, the number of
	// fonts, and an offset for each.
	collectionHeader = 12
	// A font's offset table, then a record of sixteen bytes for each table:
	// tag, checksum, offset, length.
	offsetTable = 12
	tableRecord = 16
	// The name table's header and records.
	nameHeader = 6
	nameRecord = 12
	// The identifiers of the names read: family and subfamily, and the
	// typographic ones a font with more than four styles has beside them.
	nameFamily              = 1
	nameSubfamily           = 2
	nameTypographicFamily   = 16
	nameTypographicSubstyle = 17
	// Platforms, and the Windows language that is English of the United
	// States.
	platformUnicode   = 0
	platformMacintosh = 1
	platformWindows   = 3
	windowsEnglish    = 0x409
	// OS/2: the weight class, the width class and the selection flags.
	os2Weight    = 4
	os2Width     = 6
	os2Selection = 62
	// A width class runs from one to nine, and below five is a condensed
	// one; selection bit 0 is italic and bit 9 oblique.
	widthNormal      = 5
	widthClasses     = 10
	selectionSlanted = 1<<0 | 1<<9
	// The Windows encodings read: UTF-16 of the basic plane, and of the
	// full range.
	windowsBasicPlane = 1
	windowsFullRange  = 10
	// asciiEnd is the first byte that is not ASCII.
	asciiEnd = 0x80
	// head: the Macintosh style flags: bold, italic, condensed.
	headMacStyle = 44
	macBold      = 1 << 0
	macItalic    = 1 << 1
	macCondensed = 1 << 5
	// maxTables bounds the table records read of one font, and maxMembers
	// the fonts of one collection: a file that says more is not a font.
	maxTables  = 1024
	maxMembers = 256
	// What is read of the OS/2 and head tables: up to the last field asked
	// of each.
	os2Read  = os2Selection + 2
	headRead = headMacStyle + 2
	// maxNameTable is the furthest a record of a name table can reach: the
	// names start within sixteen bits of the table's start, a name within
	// sixteen bits of theirs, and it is sixteen bits long.
	maxNameTable = 3 * math.MaxUint16
	// maxName is the bytes of the longest name read, 128 characters, and
	// maxNamesRead and maxFamilies the names read and the families kept of
	// one face. A table can hold 5,460 records that each name 64 KiB of
	// it: read whole, one file of that size was gigabytes.
	maxName      = 256
	maxNamesRead = 256
	maxFamilies  = 64
)

// readFaces is the faces of a font file, one for a font and one for each
// member of a collection, in the file's order.
func readFaces(file io.ReaderAt) ([]faceNames, error) {
	var header [collectionHeader]byte
	if _, err := file.ReadAt(header[:], 0); err != nil {
		return nil, fmt.Errorf("%w: %w", errFontFile, err)
	}

	if string(header[:4]) != collectionTag {
		face, err := readFace(file, 0)
		if err != nil {
			return nil, err
		}

		return []faceNames{face}, nil
	}

	count := binary.BigEndian.Uint32(header[8:])
	if count > maxMembers {
		return nil, fmt.Errorf("%w: a collection of %d fonts", errFontFile, count)
	}

	offsets := make([]byte, 4*count)
	if _, err := file.ReadAt(offsets, collectionHeader); err != nil {
		return nil, fmt.Errorf("%w: %w", errFontFile, err)
	}

	faces := make([]faceNames, 0, count)

	for i := range int(count) {
		// A member that cannot be read stays in its place, with no name.
		face, _ := readFace(file, int64(binary.BigEndian.Uint32(offsets[4*i:])))
		faces = append(faces, face)
	}

	return faces, nil
}

// readFace is the face whose offset table is at offset in file.
func readFace(file io.ReaderAt, offset int64) (faceNames, error) {
	var header [offsetTable]byte
	if _, err := file.ReadAt(header[:], offset); err != nil {
		return faceNames{}, fmt.Errorf("%w: %w", errFontFile, err)
	}

	count := int(binary.BigEndian.Uint16(header[4:]))
	if count > maxTables {
		return faceNames{}, fmt.Errorf("%w: %d tables", errFontFile, count)
	}

	records := make([]byte, tableRecord*count)
	if _, err := file.ReadAt(records, offset+offsetTable); err != nil {
		return faceNames{}, fmt.Errorf("%w: %w", errFontFile, err)
	}

	// table is the start of a table, as many bytes of it as most.
	table := func(tag string, most uint32) []byte {
		for i := range count {
			record := records[tableRecord*i : tableRecord*(i+1)]
			if string(record[:4]) != tag {
				continue
			}

			data := make([]byte, min(binary.BigEndian.Uint32(record[12:]), most))
			if _, err := file.ReadAt(data, int64(binary.BigEndian.Uint32(record[8:]))); err != nil {
				return nil
			}

			return data
		}

		return nil
	}

	families, subfamily := readNames(table("name", maxNameTable))
	if len(families) == 0 {
		return faceNames{}, fmt.Errorf("%w: no family name", errFontFile)
	}

	return faceNames{
		families: families,
		style:    readStyle(table("OS/2", os2Read), table("head", headRead), subfamily),
	}, nil
}

// readStyle is a face's style. The words of its subfamily say it where
// they say one: a face named Bold is its family's bold whatever weight its
// OS/2 table gives, and many a font gives its bold 400 there. The numbers
// say what the words do not, being in another language or no words of a
// style: those of the OS/2 table, or the Macintosh style flags of a font
// that has none. And the number is the weight of a face whose word it
// agrees with to less than a step of the scale, being the finer of the two:
// an Ultra Light at 250 is not the Extra Light at 200 beside it.
func readStyle(os2, head []byte, subfamily string) fontStyle {
	var style, numbers fontStyle

	for _, word := range styleWords(strings.FieldsFunc(subfamily, isNameSeparator)) {
		applyStyleWord(word, &style)
	}

	switch {
	case len(os2) >= os2Read:
		numbers = os2Style(os2)
	case len(head) >= headRead:
		numbers = macStyle(head)
	}

	switch apart := numbers.weight - style.weight; {
	case numbers.weight == 0 && style.weight == 0:
		style.weight = weightRegular
	case style.weight == 0, numbers.weight != 0 && apart > -weightThin && apart < weightThin:
		style.weight = numbers.weight
	}

	style.italic = style.italic || numbers.italic
	style.condensed = style.condensed || numbers.condensed

	return style
}

// os2Style is the weight, the width and the slant an OS/2 table states: a
// weight of 0 where it states none.
func os2Style(os2 []byte) fontStyle {
	var style fontStyle

	weight := int(binary.BigEndian.Uint16(os2[os2Weight:]))
	if weight >= 1 && weight < widthClasses {
		// The scale of the first fonts, one to nine.
		weight *= weightThin
	}

	if weight >= weightThin && weight <= weightBlack+weightThin {
		style.weight = weight
	}

	if width := binary.BigEndian.Uint16(os2[os2Width:]); width >= 1 && width < widthClasses {
		style.condensed = width < widthNormal
	}

	style.italic = binary.BigEndian.Uint16(os2[os2Selection:])&selectionSlanted != 0

	return style
}

// macStyle is the style the Macintosh flags of a head table state.
func macStyle(head []byte) fontStyle {
	var style fontStyle

	flags := binary.BigEndian.Uint16(head[headMacStyle:])
	if flags&macBold != 0 {
		style.weight = weightBold
	}

	style.italic = flags&macItalic != 0
	style.condensed = flags&macCondensed != 0

	return style
}

// How well a name record says a name in English: the Windows record in
// English, then the Macintosh one, then the Unicode platform's, which has
// no language, then any other.
const (
	anyLanguage = iota + 1
	unicodeRecord
	macintoshEnglish
	windowsInEnglish
)

// names is what is kept of a name table as its records are read: every
// family once, and for the subfamily and the typographic one the record
// that says it best in English.
type names struct {
	families    []string
	seen        map[string]bool
	rank        [nameTypographicSubstyle + 1]int
	subfamilies [nameTypographicSubstyle + 1]string
}

func isFamily(identifier int) bool {
	return identifier == nameFamily || identifier == nameTypographicFamily
}

func isSubfamily(identifier int) bool {
	return identifier == nameSubfamily || identifier == nameTypographicSubstyle
}

// wants reports whether a record is a name to keep: a family while there
// is room for one, or a subfamily said better in English than the one kept.
func (n *names) wants(identifier, english int) bool {
	return isFamily(identifier) && len(n.families) < maxFamilies ||
		isSubfamily(identifier) && english > n.rank[identifier]
}

// keep keeps the text of a record n wants.
func (n *names) keep(identifier, english int, text string) {
	if isSubfamily(identifier) {
		n.rank[identifier], n.subfamilies[identifier] = english, text

		return
	}

	if key := strings.ToLower(text); !n.seen[key] {
		n.seen[key] = true

		n.families = append(n.families, text)
	}
}

// readNames is the family names of a name table, every one once, and the
// subfamily in English, or in the language the table has when it has none
// in English.
//
// It reads a record's text only when it is a name it keeps, and no more of
// them than maxNamesRead.
func readNames(table []byte) (families []string, subfamily string) {
	if len(table) < nameHeader {
		return nil, ""
	}

	count, storage := int(binary.BigEndian.Uint16(table[2:])), int(binary.BigEndian.Uint16(table[4:]))
	// The names start after the records: a table whose records lie among
	// its names has as many records as it likes.
	if storage < nameHeader+nameRecord*count || storage > len(table) {
		return nil, ""
	}

	kept, read := names{seen: map[string]bool{}}, 0

	for i := range count {
		record := table[nameHeader+nameRecord*i:]
		platform, language := binary.BigEndian.Uint16(record), binary.BigEndian.Uint16(record[4:])
		identifier, english := int(binary.BigEndian.Uint16(record[6:])), englishOf(platform, language)

		if !kept.wants(identifier, english) {
			continue
		}

		data, ok := nameBytes(record, table[storage:])
		if !ok {
			continue
		}

		if read == maxNamesRead {
			break
		}

		read++

		if text := nameText(platform, binary.BigEndian.Uint16(record[2:]), data); text != "" {
			kept.keep(identifier, english, text)
		}
	}

	if typographic := kept.subfamilies[nameTypographicSubstyle]; typographic != "" {
		return kept.families, typographic
	}

	return kept.families, kept.subfamilies[nameSubfamily]
}

// englishOf is how well a record of that platform and language says a name
// in English.
func englishOf(platform, language uint16) int {
	switch {
	case platform == platformWindows && language == windowsEnglish:
		return windowsInEnglish
	case platform == platformMacintosh && language == 0:
		return macintoshEnglish
	case platform == platformUnicode:
		return unicodeRecord
	}

	return anyLanguage
}

// nameBytes is the bytes of a record's name in storage, and whether it is
// one that is read: all there, and no longer than a name is.
func nameBytes(record, storage []byte) ([]byte, bool) {
	length, offset := int(binary.BigEndian.Uint16(record[8:])), int(binary.BigEndian.Uint16(record[10:]))
	if length > maxName || offset+length > len(storage) {
		return nil, false
	}

	return storage[offset : offset+length], true
}

// nameText is the text of a name record, empty when it is in an encoding
// not read here. Those read are UTF-16 for Windows and the Unicode
// platform, and ASCII for the Macintosh's Roman, of which a name in English
// uses nothing else.
func nameText(platform, encoding uint16, data []byte) string {
	switch {
	case platform == platformUnicode,
		platform == platformWindows && (encoding == windowsBasicPlane || encoding == windowsFullRange):
		units := make([]uint16, len(data)/2)

		for i := range units {
			units[i] = binary.BigEndian.Uint16(data[2*i:])
		}

		return string(utf16.Decode(units))
	case platform == platformMacintosh && encoding == 0:
		for _, char := range data {
			if char >= asciiEnd {
				return ""
			}
		}

		return string(data)
	}

	return ""
}
