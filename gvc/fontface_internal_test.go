package gvc

import (
	"bytes"
	"encoding/binary"
	"runtime"
	"slices"
	"testing"
	"unicode/utf16"

	"golang.org/x/image/font/gofont/goregular"
)

// namedRecord is one record of a name table made by a test: a Windows
// record in English, of a name that is length bytes at offset of the
// table's names.
type namedRecord struct {
	identifier     uint16
	offset, length int
}

// namesOnly is a font file that is a name table and nothing else: records,
// then names. storage is where the table says its names start, which is
// after the records in a table of the format.
func namesOnly(records []namedRecord, storage int, names []byte) []byte {
	const windowsUnicode = 1

	table := binary.BigEndian.AppendUint16(nil, 0)
	table = binary.BigEndian.AppendUint16(table, uint16(len(records)))
	table = binary.BigEndian.AppendUint16(table, uint16(storage))

	for _, record := range records {
		for _, field := range []int{
			platformWindows, windowsUnicode, windowsEnglish, int(record.identifier), record.length, record.offset,
		} {
			table = binary.BigEndian.AppendUint16(table, uint16(field))
		}
	}

	table = append(table, names...)

	file := []byte{0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	file = append(file, "name"...)
	file = binary.BigEndian.AppendUint32(file, 0)
	file = binary.BigEndian.AppendUint32(file, uint32(offsetTable+tableRecord))
	file = binary.BigEndian.AppendUint32(file, uint32(len(table)))

	return append(file, table...)
}

// utf16Of is text as the bytes of a name record.
func utf16Of(text string) []byte {
	var out []byte
	for _, unit := range utf16.Encode([]rune(text)) {
		out = binary.BigEndian.AppendUint16(out, unit)
	}

	return out
}

// allocated is the bytes one call of read allocates.
func allocated(read func()) uint64 {
	const runs = 16

	var before, after runtime.MemStats

	runtime.ReadMemStats(&before)

	for range runs {
		read()
	}

	runtime.ReadMemStats(&after)

	return (after.TotalAlloc - before.TotalAlloc) / runs
}

// most is what reading one face may allocate, in bytes: the name table as
// far as a record can reach, which is 192 KiB, and 256 names of 256 bytes,
// each some times over as it is decoded, lowered and kept. The tables of
// the test below take 90 to 450 KiB.
const most = 1 << 20

// A name table is read within bounds whatever it says: the reader kept every
// family record of a table, and a table can be made of thousands of records
// that each name 64 KiB of the same bytes, each text one more family. A file
// of 70 KiB with 4,096 such records took two seconds and 2.8 GiB to read,
// and stayed in the index as 320 MiB.
//
// It is not run beside other tests: it reads what the process allocates.
func TestANameTableIsReadWithinBounds(t *testing.T) {
	const (
		// As many records as fit before the names, whose offset is sixteen
		// bits.
		fitting = (0xFFFF - nameHeader) / nameRecord
		long    = 0xFFFE
	)

	// Names of distinct characters, so that every stretch of them differs.
	distinct := make([]byte, 0, 2*fitting+long)
	for i := range cap(distinct) / 2 {
		distinct = binary.BigEndian.AppendUint16(distinct, uint16(0x4E00+i%0x5000))
	}

	// Records that lie among the names they name: the table has no room for
	// them before its names, which start at its seventh byte.
	amongNames := make([]namedRecord, 4096)
	for i := range amongNames {
		amongNames[i] = namedRecord{identifier: nameFamily, offset: 2 * i, length: maxName}
	}

	// A table of the format: every record a family of the longest length
	// read, each its own.
	families := make([]namedRecord, fitting)
	for i := range families {
		families[i] = namedRecord{identifier: nameFamily, offset: 2 * i, length: maxName}
	}

	// The same family in every record: none is one more to keep, and each
	// is read to know it.
	same := make([]namedRecord, fitting)
	for i := range same {
		same[i] = namedRecord{identifier: nameFamily, length: maxName}
	}

	// A family behind names too long to be one.
	genuine := utf16Of("Zq Real")
	behind := make([]namedRecord, 1024, 1025)

	for i := range behind {
		behind[i] = namedRecord{identifier: nameFamily, offset: len(genuine) + 2*i, length: long}
	}

	behind = append(behind, namedRecord{identifier: nameFamily, length: len(genuine)})

	// A table that says it is a gigabyte long, in a file of some bytes.
	gigabyte := namesOnly([]namedRecord{{identifier: nameFamily, length: len(genuine)}}, nameHeader+nameRecord, genuine)
	if len(gigabyte) < offsetTable+tableRecord {
		t.Fatal("no table record to say a length in")
	}

	binary.BigEndian.PutUint32(gigabyte[offsetTable+12:], 1<<30)

	for _, tc := range []struct {
		name     string
		file     []byte
		families int
		first    string
	}{
		{name: "a table a gigabyte long", file: gigabyte},
		{name: "records among their names", file: namesOnly(amongNames, nameHeader, distinct)},
		{
			name:     "every record a family",
			file:     namesOnly(families, nameHeader+nameRecord*len(families), distinct),
			families: maxFamilies,
		},
		{
			name:     "one family in every record",
			file:     namesOnly(same, nameHeader+nameRecord*len(same), distinct),
			families: 1,
		},
		{
			name:     "a family behind names too long",
			file:     namesOnly(behind, nameHeader+nameRecord*len(behind), append(slices.Clone(genuine), distinct...)),
			families: 1,
			first:    "Zq Real",
		},
	} {
		var faces []faceNames

		took := allocated(func() { faces, _ = readFaces(bytes.NewReader(tc.file)) })
		if took > most {
			t.Errorf("%s: reading the file allocates %d bytes, want %d at most", tc.name, took, most)
		}

		var got []string
		if len(faces) == 1 {
			got = faces[0].families
		}

		if len(got) != tc.families || tc.first != "" && !slices.Contains(got, tc.first) {
			t.Errorf("%s: %d families, want %d (%q among them)", tc.name, len(got), tc.families, tc.first)
		}

		for _, family := range got {
			if len(family) > 2*maxName {
				t.Errorf("%s: a family of %d bytes", tc.name, len(family))
			}
		}
	}
}

// A face's style is what the words of its subfamily say and, where they say
// nothing, what its numbers do; a weight the words name is the number's
// when the two are less than a step apart, and the word's when the number
// says another weight.
func TestAFacesStyleIsItsWordsThenItsNumbers(t *testing.T) {
	const (
		italicFlag = 1 << 0
		normal     = 5
		narrow     = 3
	)

	os2 := func(weight, width, selection uint16) []byte {
		table := make([]byte, os2Read)
		binary.BigEndian.PutUint16(table[os2Weight:], weight)
		binary.BigEndian.PutUint16(table[os2Width:], width)
		binary.BigEndian.PutUint16(table[os2Selection:], selection)

		return table
	}

	head := func(flags uint16) []byte {
		table := make([]byte, headRead)
		binary.BigEndian.PutUint16(table[headMacStyle:], flags)

		return table
	}

	for _, tc := range []struct {
		subfamily string
		os2, head []byte
		want      fontStyle
	}{
		// A weight the words name, and a number that says another.
		{"Bold", os2(400, normal, 0), nil, fontStyle{weight: weightBold}},
		{"Light", os2(400, normal, 0), nil, fontStyle{weight: weightLight}},
		{"Regular", os2(700, normal, 0), nil, fontStyle{weight: weightRegular}},
		{"Black", os2(800, normal, 0), nil, fontStyle{weight: weightBlack}},
		{"W3", os2(300, normal, 0), nil, fontStyle{weight: weightRegular}},
		// A number less than a step from the word's weight is the finer.
		{"Ultra Light", os2(275, normal, 0), nil, fontStyle{weight: 275}},
		{"Extra Light", os2(200, normal, 0), nil, fontStyle{weight: weightExtraLight}},
		{"Book", os2(350, normal, 0), nil, fontStyle{weight: 350}},
		// Words that name no weight: the number.
		{"Negreta", os2(700, normal, 0), nil, fontStyle{weight: weightBold}},
		{"", os2(250, normal, 0), nil, fontStyle{weight: 250}},
		{"Italic", os2(600, normal, 0), nil, fontStyle{weight: weightDemi, italic: true}},
		// The scale of one to nine, and a number that is no weight.
		{"", os2(7, normal, 0), nil, fontStyle{weight: weightBold}},
		{"", os2(0, normal, 0), nil, fontStyle{weight: weightRegular}},
		{"Bold", os2(5000, normal, 0), nil, fontStyle{weight: weightBold}},
		// Slant and width: of the words or of the numbers.
		{"Regular", os2(400, normal, italicFlag), nil, fontStyle{weight: weightRegular, italic: true}},
		{"Bold Italic", os2(700, normal, 0), nil, fontStyle{weight: weightBold, italic: true}},
		{"Regular", os2(400, narrow, 0), nil, fontStyle{weight: weightRegular, condensed: true}},
		{"Condensed", os2(400, normal, 0), nil, fontStyle{weight: weightRegular, condensed: true}},
		// No OS/2 table: the Macintosh flags.
		{"Negreta", nil, head(macBold), fontStyle{weight: weightBold}},
		{"Light", nil, head(macBold | macItalic), fontStyle{weight: weightLight, italic: true}},
		{"", nil, head(macCondensed), fontStyle{weight: weightRegular, condensed: true}},
		// An OS/2 table too short to hold its flags is not read.
		{"", os2(700, normal, 0)[:os2Read-1], head(macItalic), fontStyle{weight: weightRegular, italic: true}},
	} {
		if got := readStyle(tc.os2, tc.head, tc.subfamily); got != tc.want {
			t.Errorf("the style of %q with OS/2 %v is %+v, want %+v", tc.subfamily, tc.os2 != nil, got, tc.want)
		}
	}
}

// A collection is read within bounds too: each member is a face, and a file
// may list one face as many times as it has bytes for.
func TestACollectionIsReadWithinItsCount(t *testing.T) {
	collection := func(members int) []byte {
		out := []byte("ttcf")
		out = binary.BigEndian.AppendUint32(out, 0x00010000)
		out = binary.BigEndian.AppendUint32(out, uint32(members))

		face := namesOnly([]namedRecord{{identifier: nameFamily, length: 4}}, nameHeader+nameRecord, utf16Of("Zq"))
		start := len(out) + 4*members

		// The face's one table, where it is in the collection.
		binary.BigEndian.PutUint32(face[offsetTable+8:], uint32(start+offsetTable+tableRecord))

		for range members {
			out = binary.BigEndian.AppendUint32(out, uint32(start))
		}

		return append(out, face...)
	}

	faces, err := readFaces(bytes.NewReader(collection(maxMembers)))
	if err != nil || len(faces) != maxMembers || len(faces[0].families) != 1 || faces[0].families[0] != "Zq" {
		t.Errorf("a collection of %d members: %d faces, %v", maxMembers, len(faces), err)
	}

	// One more, and as many as a file of 256 KiB has room to list.
	for _, members := range []int{maxMembers + 1, 1 << 16} {
		var err error

		file := collection(members)

		took := allocated(func() { faces, err = readFaces(bytes.NewReader(file)) })

		if err == nil {
			t.Errorf("a collection of %d members is read as %d faces, want an error", members, len(faces))
		}

		if took > most {
			t.Errorf("a collection of %d members: reading it allocates %d bytes, want %d at most", members, took, most)
		}
	}
}

// Whatever a font file holds, reading its faces ends, with no more families
// and no longer ones than a face is read for.
func FuzzReadFaces(f *testing.F) {
	f.Add(goregular.TTF)
	f.Add(namesOnly([]namedRecord{{identifier: nameFamily, length: 4}}, nameHeader+nameRecord, utf16Of("Zq")))
	f.Add(namesOnly([]namedRecord{{identifier: nameFamily, length: 0xFFFE}}, nameHeader, utf16Of("Zq")))
	f.Add([]byte("ttcf\x00\x01\x00\x00\x00\x00\x00\x02\x00\x00\x00\x14\x00\x00\x00\x14"))

	f.Fuzz(func(t *testing.T, file []byte) {
		faces, err := readFaces(bytes.NewReader(file))
		if err != nil {
			return
		}

		if len(faces) > maxMembers {
			t.Fatalf("%d faces", len(faces))
		}

		for _, face := range faces {
			// A member that could not be read is there with no name.
			if len(face.families) == 0 {
				continue
			}

			if len(face.families) > maxFamilies {
				t.Fatalf("%d families", len(face.families))
			}

			for _, family := range face.families {
				if len(family) > 2*maxName {
					t.Fatalf("a family of %d bytes", len(family))
				}
			}

			if face.style.weight < weightThin || face.style.weight > weightBlack+weightThin {
				t.Fatalf("a weight of %d", face.style.weight)
			}
		}
	})
}
