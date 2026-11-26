package graphviz_test

import (
	"os"
	"path/filepath"
	"testing"
)

// The fonts of a directory are read through symbolic links: a font
// directory that is a link to another, a directory inside it that is one,
// and a file that is one. Dotfiles trees link the user's font directory,
// and Nix, Guix and some packages link directories under share/fonts; none
// of them was read, the walk not following links. A link that leads back to
// where it is does not keep the walk going for ever.
func TestFontDirectoriesAreReadThroughLinks(t *testing.T) {
	if os.Getenv(fontCorpusEnv) != "" {
		for name, want := range map[string]float64{
			"Zl Root":      200,
			"Zl Directory": 240,
			"Zl File":      280,
		} {
			if got := chosenAdvance(t, name); got != want {
				t.Errorf("%q is set in the font whose A is %.0f units wide, want %.0f", name, got, want)
			}
		}

		return
	}

	home, elsewhere := t.TempDir(), t.TempDir()

	write := func(path string, face corpusFace) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, face.font(t), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	link := func(target, at string) {
		if err := os.MkdirAll(filepath.Dir(at), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.Symlink(target, at); err != nil {
			t.Skipf("no symbolic link here: %v", err)
		}
	}

	// The user's font directory is a link to a directory elsewhere, which
	// holds a font, a link to another directory, a link to a font file, and
	// a link back to itself.
	fonts := filepath.Join(elsewhere, "fonts")

	write(filepath.Join(fonts, "root.ttf"), corpusFace{family: "Zl Root", subfamily: "Regular", advance: 200})
	write(filepath.Join(elsewhere, "more", "directory.ttf"),
		corpusFace{family: "Zl Directory", subfamily: "Regular", advance: 240})
	write(
		filepath.Join(elsewhere, "files", "file.bin"),
		corpusFace{family: "Zl File", subfamily: "Regular", advance: 280},
	)

	link(filepath.Join(elsewhere, "more"), filepath.Join(fonts, "linked"))
	link(filepath.Join(elsewhere, "files", "file.bin"), filepath.Join(fonts, "file.ttf"))
	link(fonts, filepath.Join(fonts, "again"))

	link(fonts, userFontDirectory(home))

	cmd := child(t, os.Args[0], "-test.run=^"+t.Name()+"$", "-test.v")

	cmd.Env = append(os.Environ(), fontCorpusEnv+"=1", "HOME="+home, "USERPROFILE="+home, "LOCALAPPDATA="+home)

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the process with the linked font directory: %v\n%s", err, out)
	}
}
