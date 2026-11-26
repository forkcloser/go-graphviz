package gvc

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// fontDirectory is the index of a directory holding the font files given,
// by name.
func fontDirectory(t *testing.T, files map[string][]byte) *fontIndex {
	t.Helper()

	dir := t.TempDir()

	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return buildFontIndex([]string{dir})
}

// copies is count small font files, each the embedded Go font under a name
// of its own: copy000.ttf, copy001.ttf, and so on.
func copies(count int) map[string][]byte {
	files := map[string][]byte{}
	for i := range count {
		files[fmt.Sprintf("copy%03d.ttf", i)] = goregular.TTF
	}

	return files
}

// kept is how many font files c keeps, and their size together.
func kept(c *fontCaches) (files int, held int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.files), c.held
}

// The font files kept are bounded in number: a graph may name more fonts
// than maxFontFiles, each is found and loaded, and one asked for again
// while it is kept is the same font; the one past the bound drops those
// kept, with what names resolved to.
func TestFontFilesKeptWithinTheirCount(t *testing.T) {
	const more = 8

	index, c := fontDirectory(t, copies(maxFontFiles+more)), newFontCaches()

	for i := range maxFontFiles + more {
		name := fmt.Sprintf("copy%03d", i)

		loaded, err := c.resolve(index, name, requestFor(name, 0))
		if err != nil || !strings.Contains(loaded.key, name) {
			t.Fatalf("%s resolved to %+v, %v; want the file of that name", name, loaded, err)
		}

		if again, _ := c.resolve(index, name, requestFor(name, 0)); again != loaded {
			t.Fatalf("%s: asked for twice, two fonts", name)
		}

		// The first fonts fill the bound; the one past it starts again.
		want := i%maxFontFiles + 1

		if files, held := kept(c); files != want || held != int64(want*len(goregular.TTF)) {
			t.Fatalf("after %d fonts: %d files and %d bytes kept, want %d files of %d bytes",
				i+1, files, held, want, len(goregular.TTF))
		}

		c.remember(name, loaded)

		c.mu.Lock()
		fonts, names := len(c.loaded), len(c.resolved)
		c.mu.Unlock()

		if fonts != want || names != want {
			t.Fatalf("after %d fonts: %d fonts and %d names kept, want %d", i+1, fonts, names, want)
		}
	}
}

// The font files kept are bounded in bytes together; a file over the bound
// by itself is read all the same, and kept alone.
func TestFontFilesKeptWithinTheirBytes(t *testing.T) {
	size := int64(len(goregular.TTF))
	index := fontDirectory(t, copies(10))

	c := newFontCaches()
	c.maxBytes = 3*size + size/2

	for i := range 10 {
		name := fmt.Sprintf("copy%03d", i)

		if loaded, err := c.resolve(
			index,
			name,
			requestFor(name, 0),
		); err != nil ||
			!strings.Contains(loaded.key, name) {
			t.Fatalf("%s resolved to %+v, %v; want the file of that name", name, loaded, err)
		}

		want := i%3 + 1

		if files, held := kept(c); files != want || held != int64(want)*size {
			t.Fatalf("after %d fonts: %d files and %d bytes kept, want %d files", i+1, files, held, want)
		}
	}

	small := newFontCaches()
	small.maxBytes = size - 1

	for i := range 3 {
		name := fmt.Sprintf("copy%03d", i)

		if loaded, err := small.resolve(
			index,
			name,
			requestFor(name, 0),
		); err != nil ||
			!strings.Contains(loaded.key, name) {
			t.Fatalf("%s, a byte over the bound, resolved to %+v, %v; want the file of that name", name, loaded, err)
		}

		if files, held := kept(small); files != 1 || held != size {
			t.Errorf("%d files and %d bytes kept after %d files over the bound, want the last alone", files, held, i+1)
		}
	}
}

// collection is a font collection of the fonts given: each whole after the
// header, the offsets of its tables moved by where it lands.
func collection(members ...[]byte) []byte {
	const header, record = 12, 16

	out := binary.BigEndian.AppendUint16([]byte("ttcf"), 1)
	out = binary.BigEndian.AppendUint16(out, 0)
	out = binary.BigEndian.AppendUint32(out, uint32(len(members)))

	base := uint32(header + 4*len(members))
	for _, member := range members {
		out = binary.BigEndian.AppendUint32(out, base)
		base += uint32(len(member))
	}

	for _, member := range members {
		base = uint32(len(out))

		moved := slices.Clone(member)
		for table := range int(binary.BigEndian.Uint16(moved[4:])) {
			at := header + record*table + 8
			binary.BigEndian.PutUint32(moved[at:], binary.BigEndian.Uint32(moved[at:])+base)
		}

		out = append(out, moved...)
	}

	return out
}

// A collection is read once for all its members: its two fonts are two
// fonts of one file kept.
func TestFontCollectionReadOnce(t *testing.T) {
	data := collection(goregular.TTF, gobold.TTF)
	index, c := fontDirectory(t, map[string][]byte{"pair.ttc": data}), newFontCaches()

	if len(index.faces) != 2 {
		t.Fatalf("%d faces indexed of a collection of two", len(index.faces))
	}

	regular, err := c.load(index.faces[0])
	if err != nil {
		t.Fatal(err)
	}

	bold, err := c.load(index.faces[1])
	if err != nil {
		t.Fatal(err)
	}

	if regular.font == bold.font || regular.font.NumGlyphs() == 0 || bold.font.NumGlyphs() == 0 {
		t.Errorf("the collection's members: %p and %p, want two fonts with glyphs", regular.font, bold.font)
	}

	if files, held := kept(c); files != 1 || held != int64(len(data)) {
		t.Errorf("%d files and %d bytes kept for two members of one %d byte collection", files, held, len(data))
	}
}

// A face that answered for a character is tried first for the next, and is
// remembered once: ten characters found in one font leave one face
// remembered, where each left another.
func TestFallbackRemembersAFaceOnce(t *testing.T) {
	fallback := newGlyphFallback(fontDirectory(t, copies(1)))

	for _, char := range "ABCDEFGHIJ" {
		if found := fallback.fontsFor([]rune{char}); found[char] == nil {
			t.Fatalf("%q: no font found for it", char)
		}
	}

	if len(fallback.found) != 1 {
		t.Errorf("%d faces remembered for ten characters of one font, want 1", len(fallback.found))
	}
}

// Dropping the font files drops everything made from them, and what is
// remembered beside them is bounded by itself: the names resolved, and the
// characters a fallback was looked for.
func TestFontCachesForget(t *testing.T) {
	index, c := fontDirectory(t, copies(1)), newFontCaches()

	loaded, err := c.load(index.faces[0])
	if err != nil {
		t.Fatal(err)
	}

	face, err := loaded.face(12, pointsPerInch)
	if err != nil {
		t.Fatal(err)
	}

	c.mu.Lock()
	c.faces["a face"] = face
	c.resolved["a name"] = loaded
	c.drop()

	if len(c.files)+len(c.loaded)+len(c.resolved)+len(c.faces) != 0 || c.held != 0 {
		t.Errorf("after the drop: %d files, %d fonts, %d names, %d faces, %d bytes",
			len(c.files), len(c.loaded), len(c.resolved), len(c.faces), c.held)
	}

	c.mu.Unlock()

	for i := range maxResolved + 10 {
		c.remember(strconv.Itoa(i), loaded)
	}

	c.mu.Lock()
	names := len(c.resolved)
	c.mu.Unlock()

	if names != 10 {
		t.Errorf("%d names remembered after %d, want the 10 past the bound", names, maxResolved+10)
	}

	fallback := newGlyphFallback(index)
	for i := range maxFallbackRunes + 10 {
		fallback.remember(rune(i), nil)
	}

	if len(fallback.byRune) != 10 {
		t.Errorf(
			"%d characters remembered after %d, want the 10 past the bound",
			len(fallback.byRune),
			maxFallbackRunes+10,
		)
	}
}

// The names read against an index are kept as many as maxResolved at most:
// a process that sets text in ever new names does not keep them all.
func TestNamesReadAreKeptWithinTheirBound(t *testing.T) {
	index := fontDirectory(t, copies(1))

	for i := range 2*maxResolved + 10 {
		name := fmt.Sprintf("Family %d Bold", i)

		if request := index.requestFor(name, nil, 0); len(request.families) != 1 || request.style.weight != weightBold {
			t.Fatalf("%q read as %+v", name, request)
		}

		if kept := len(index.listed); kept > maxResolved {
			t.Fatalf("%d names kept after %d were read, want %d at most", kept, i+1, maxResolved)
		}
	}
}
