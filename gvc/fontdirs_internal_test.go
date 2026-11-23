package gvc

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// A font directory is read once, whatever leads to it: named twice, once by
// a link, and holding a link back to itself, it gives each of its fonts
// once. Read each time it is reached, it gave them once for each level a
// link can be followed down, which is as far as the system follows links;
// every font was found, and nothing but their number said so.
func TestAFontDirectoryIsReadOnce(t *testing.T) {
	root := t.TempDir()
	fonts, alias := filepath.Join(root, "fonts"), filepath.Join(root, "alias")

	if err := os.MkdirAll(filepath.Join(fonts, "more"), 0o700); err != nil {
		t.Fatal(err)
	}

	for name, data := range map[string][]byte{"regular.ttf": goregular.TTF, filepath.Join("more", "bold.ttf"): gobold.TTF} {
		if err := os.WriteFile(filepath.Join(fonts, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for at, target := range map[string]string{alias: fonts, filepath.Join(fonts, "again"): fonts} {
		if err := os.Symlink(target, at); err != nil {
			t.Skipf("no symbolic link here: %v", err)
		}
	}

	index := buildFontIndex([]string{fonts, alias})
	if len(index.faces) != 2 {
		t.Errorf("%d faces indexed of a directory of two fonts", len(index.faces))
	}
}
