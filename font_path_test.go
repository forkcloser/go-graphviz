package graphviz_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"testing/fstest"

	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"

	"github.com/forkcloser/go-graphviz"
)

// widthWith is how wide Graphviz makes a node labelled with twenty narrow
// letters in the font named: the ellipse's horizontal radius in the SVG, a
// monospace font's far from a proportional one's.
func widthWith(t *testing.T, fontname string) string {
	t.Helper()

	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	defer closeOrError(t, g.Close)

	graph, err := graphviz.ParseBytes(
		fmt.Appendf(nil, `digraph { a [fontname=%q label="iiiiiiiiiiiiiiiiiiii"] }`, fontname),
	)
	if err != nil {
		t.Fatal(err)
	}

	defer closeOrError(t, graph.Close)

	var out bytes.Buffer

	if err := g.Render(ctx, graph, graphviz.SVG, &out); err != nil {
		t.Fatal(err)
	}

	radius := regexp.MustCompile(`<ellipse[^>]* rx="([0-9.]+)"`).FindSubmatch(out.Bytes())
	if radius == nil {
		t.Fatalf("no ellipse in the SVG:\n%s", out.String())
	}

	return string(radius[1])
}

// A font the graph names by path is read from the file system SetFileSystem
// names, as a node image is: it was read from the host, whatever file
// system was named.
func TestFontNamedByPathIsReadFromTheFileSystem(t *testing.T) {
	t.Cleanup(func() { graphviz.SetFileSystem(nil) })

	// A font file the host has, at a path no file system below holds.
	onHost := filepath.ToSlash(filepath.Join(t.TempDir(), "host.ttf"))
	if err := os.WriteFile(onHost, gomono.TTF, 0o600); err != nil {
		t.Fatal(err)
	}

	graphviz.SetFileSystem(nil)

	missing := widthWith(t, "/fonts/nosuch.ttf")

	if got := widthWith(t, onHost); got == missing {
		t.Fatalf(
			"on the host, a font named by its path measures as one that is not there (%s): the test measures nothing",
			got,
		)
	}

	// On the host the path is the one the graph wrote: a file of that path
	// under the working directory is another file, and is not read. A path
	// not asked for yet, so that nothing kept answers for it.
	if runtime.GOOS != "windows" {
		written := filepath.Join(t.TempDir(), "written.ttf")
		if err := os.WriteFile(written, gomono.TTF, 0o600); err != nil {
			t.Fatal(err)
		}

		elsewhere := t.TempDir()
		under := filepath.Join(elsewhere, written)

		if err := os.MkdirAll(filepath.Dir(under), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(under, goregular.TTF, 0o600); err != nil {
			t.Fatal(err)
		}

		t.Chdir(elsewhere)

		if got := widthWith(t, written); got == missing {
			t.Errorf("an absolute path was read from under the working directory: %s", got)
		}
	}

	graphviz.SetFileSystem(fstest.MapFS{})

	if got := widthWith(t, onHost); got != missing {
		t.Errorf("under an empty file system the host's font file was measured with: %s, want %s", got, missing)
	}

	graphviz.SetFileSystem(fstest.MapFS{"fonts/face.ttf": {Data: gomono.TTF}})

	mono := widthWith(t, "/fonts/face.ttf")
	if mono == missing {
		t.Errorf("a font file the file system holds was not measured with: %s", mono)
	}

	// The same name in another file system is another file.
	graphviz.SetFileSystem(fstest.MapFS{"fonts/face.ttf": {Data: goregular.TTF}})

	if regular := widthWith(t, "/fonts/face.ttf"); regular == mono {
		t.Errorf("the font file of the file system named before was measured with: %s", regular)
	}
}
