package gvc

import (
	"encoding/binary"
	"slices"
	"sort"
	"unicode"
)

// runeRange is a run of characters, both ends in it.
type runeRange struct {
	first, last rune
}

// charCoverage is the characters a face's cmap table maps to a glyph: the
// subtable the font parser would choose, in the formats it takes, read as
// the specification has them. It is what lets the fallback ask every
// installed face for a character without opening one.
//
// What this reader does not take (a Macintosh table of one byte a
// character, a table cut short by maxCmapTable, a subtable that does not
// hold together) is unknown, and a face of unknown coverage is asked
// itself: the reader costs a look there and hides nothing.
//
// The parser parts from the specification in two shapes, which no font
// installed where this was written has: for a segment with both an offset
// into the glyph array and a delta it leaves the delta out, and it asks a
// trimmed table for a character cut to 16 bits. There the reader says what
// the font means: it has not the character the parser would draw with
// another's glyph.
type charCoverage struct {
	ranges  []runeRange
	unknown bool
}

// has reports whether the face may have char: its table maps it, or the
// table could not be read here.
func (c charCoverage) has(char rune) bool {
	if c.unknown {
		return true
	}

	i := sort.Search(len(c.ranges), func(i int) bool { return c.ranges[i].last >= char })

	return i < len(c.ranges) && c.ranges[i].first <= char
}

// The parts of a cmap table read here, by the OpenType specification, and
// the encodings the font parser takes, with the bytes a character each.
const (
	cmapHeader  = 4
	cmapRecord  = 8
	cmapUnicode = 0
	cmapMac     = 1
	cmapWindows = 3

	// The formats of a subtable the font parser reads: a byte a character,
	// segments of 16 bits, a trimmed table, groups of 32 bits.
	cmapBytes    = 0
	cmapSegments = 4
	cmapTrimmed  = 6
	cmapGroups   = 12

	// The bytes a character of the encodings it takes.
	cmapNarrow = 2
	cmapWide   = 4

	// maxCmapTable is the most of a cmap table that is read: the largest
	// installed ones, of the fonts with every ideograph, are under a
	// megabyte.
	maxCmapTable = 4 << 20
)

// cmapWidth is the bytes a character of the encoding, 0 for an encoding
// the font parser does not take.
func cmapWidth(platform, encoding uint16) int {
	// Unicode's encodings 3 and 4 are the Basic Multilingual Plane and the
	// full repertoire; Windows's 0, 1 and 10 a symbol font's, the plane and
	// the full repertoire; the Macintosh's 0 is Roman.
	switch {
	case platform == cmapUnicode && encoding == 3, platform == cmapWindows && (encoding == 0 || encoding == 1):
		return cmapNarrow
	case platform == cmapUnicode && encoding == 4, platform == cmapWindows && encoding == 10:
		return cmapWide
	case platform == cmapMac && encoding == 0:
		return 1
	}

	return 0
}

// readCoverage is the coverage of a face whose cmap table is table, whole
// or cut at maxCmapTable. The subtable is the font parser's choice: the
// widest encoding it takes, the first of them, in a format it reads.
func readCoverage(table []byte) charCoverage {
	unknown := charCoverage{unknown: true}

	if len(table) < cmapHeader {
		return unknown
	}

	count := int(binary.BigEndian.Uint16(table[2:]))
	if len(table) < cmapHeader+cmapRecord*count {
		return unknown
	}

	var (
		width  int
		chosen []byte
	)

	for i := range count {
		record := table[cmapHeader+cmapRecord*i:]

		platform, encoding := binary.BigEndian.Uint16(record), binary.BigEndian.Uint16(record[2:])
		if cmapWidth(platform, encoding) <= width {
			continue
		}

		offset := binary.BigEndian.Uint32(record[4:])
		if uint64(offset)+4 > uint64(len(table)) {
			return unknown
		}

		format := binary.BigEndian.Uint16(table[offset:])
		if format != cmapSegments && format != cmapTrimmed && format != cmapGroups &&
			(format != cmapBytes || platform != cmapMac) {
			continue
		}

		width, chosen = cmapWidth(platform, encoding), table[offset:]
	}

	if width != cmapNarrow && width != cmapWide {
		return unknown
	}

	var (
		ranges []runeRange
		whole  bool
	)

	switch binary.BigEndian.Uint16(chosen) {
	case cmapSegments:
		ranges, whole = segmentRanges(chosen)
	case cmapTrimmed:
		ranges, whole = trimmedRanges(chosen)
	case cmapGroups:
		ranges, whole = groupRanges(chosen)
	}

	if !whole {
		return unknown
	}

	return charCoverage{ranges: mergeRanges(ranges)}
}

// segmentRanges is the characters of a format 4 subtable that have a glyph,
// and whether the subtable held together.
func segmentRanges(sub []byte) ([]runeRange, bool) {
	const header = 14

	if len(sub) < header {
		return nil, false
	}

	segments := int(binary.BigEndian.Uint16(sub[6:])) / 2

	ends, starts := header, header+2*segments+2
	deltas, offsets := starts+2*segments, starts+4*segments

	if len(sub) < offsets+2*segments {
		return nil, false
	}

	var ranges []runeRange

	for i := range segments {
		first, last := binary.BigEndian.Uint16(sub[starts+2*i:]), binary.BigEndian.Uint16(sub[ends+2*i:])
		if last < first {
			return nil, false
		}

		delta, offset := binary.BigEndian.Uint16(sub[deltas+2*i:]), int(binary.BigEndian.Uint16(sub[offsets+2*i:]))

		// Counted from first, and stopped at last by equality: last may be
		// the largest a character of 16 bits can be.
		for code := first; ; code++ {
			glyph, held := segmentGlyph(sub, offsets+2*i, offset, delta, code, int(code-first))

			switch {
			case !held:
				return nil, false
			case glyph != 0:
				ranges = append(ranges, runeRange{rune(code), rune(code)})
			}

			if code == last {
				break
			}
		}
	}

	return ranges, true
}

// segmentGlyph is the glyph of code, the nth character of a segment: code
// moved by delta, or, for a segment with an offset, what the glyph array
// has n places past the offset, counted from where the offset itself is,
// at, and moved by delta unless it is 0.
func segmentGlyph(sub []byte, at, offset int, delta, code uint16, n int) (uint16, bool) {
	if offset == 0 {
		return code + delta, true
	}

	place := at + offset + 2*n
	if place+2 > len(sub) {
		return 0, false
	}

	glyph := binary.BigEndian.Uint16(sub[place:])
	if glyph != 0 {
		glyph += delta
	}

	return glyph, true
}

// trimmedRanges is the characters of a format 6 subtable that have a glyph.
func trimmedRanges(sub []byte) ([]runeRange, bool) {
	const header = 10

	if len(sub) < header {
		return nil, false
	}

	code, count := rune(binary.BigEndian.Uint16(sub[6:])), int(binary.BigEndian.Uint16(sub[8:]))
	if len(sub) < header+2*count {
		return nil, false
	}

	var ranges []runeRange

	for i := range count {
		if binary.BigEndian.Uint16(sub[header+2*i:]) != 0 {
			ranges = append(ranges, runeRange{code, code})
		}

		code++
	}

	return ranges, true
}

// groupRanges is the characters of a format 12 subtable that have a glyph:
// each group's, but for the one character a group may map to glyph 0.
func groupRanges(sub []byte) ([]runeRange, bool) {
	const header, group = 16, 12

	if len(sub) < header {
		return nil, false
	}

	count := int64(binary.BigEndian.Uint32(sub[12:]))
	if int64(len(sub)) < header+group*count {
		return nil, false
	}

	ranges := make([]runeRange, 0, count)

	for at := header; at < header+group*int(count); at += group {
		first, last := int64(binary.BigEndian.Uint32(sub[at:])), int64(binary.BigEndian.Uint32(sub[at+4:]))
		glyph := binary.BigEndian.Uint32(sub[at+8:])

		if last < first || last > unicode.MaxRune {
			return nil, false
		}

		if glyph == 0 {
			first++
		}

		if first <= last {
			// #nosec G115 -- both at most unicode.MaxRune, by the check above
			ranges = append(ranges, runeRange{rune(first), rune(last)})
		}
	}

	return ranges, true
}

// mergeRanges is ranges in order, those that touch or overlap made one.
func mergeRanges(ranges []runeRange) []runeRange {
	slices.SortFunc(ranges, func(a, b runeRange) int { return int(a.first - b.first) })

	merged := make([]runeRange, 0, len(ranges))

	for _, next := range ranges {
		if last := len(merged) - 1; last >= 0 && next.first <= merged[last].last+1 {
			merged[last].last = max(merged[last].last, next.last)

			continue
		}

		merged = append(merged, next)
	}

	return slices.Clip(merged)
}
