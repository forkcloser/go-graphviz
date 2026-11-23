package graphviz_test

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/forkcloser/go-graphviz"
)

// Version is the release the module was built from, the one pins.yaml
// names, and it is the module's own answer: the release is written in no Go
// file of the library, so a module rebuilt from another Graphviz answers
// with that one.
func TestVersionIsTheModulesOwn(t *testing.T) {
	pin := embeddedGraphvizVersion(t)

	got, err := graphviz.Version()
	if err != nil || got != pin {
		t.Fatalf("Version() = %q, %v; pins.yaml builds the module from %q", got, err, pin)
	}

	again, err := graphviz.Version()
	if err != nil || again != got {
		t.Fatalf("a second Version() = %q, %v; the first was %q", again, err, got)
	}

	err = filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if name := entry.Name(); path != "." && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "build" || name == "testdata") {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if bytes.Contains(src, []byte(`"`+pin)) {
			t.Errorf("%s holds the release %q as a string of its own", path, pin)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Asking for the version leaves nothing in the module: the context the
// bridge makes for the question is freed, and so is the string's wrapper.
func TestVersionLeavesNoMemory(t *testing.T) {
	growth := memoryGrowth(20000, func() {
		if _, err := graphviz.Version(); err != nil {
			t.Fatal(err)
		}
	})
	if growth != 0 {
		t.Fatalf("the module's memory grew by %d bytes over 20000 calls of Version", growth)
	}
}
