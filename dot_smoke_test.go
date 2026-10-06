package graphviz_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/image/draw"

	"github.com/forkcloser/go-graphviz"
)

// The dot smoke test compares this library's PNG output with a Graphviz
// installed on the machine, graph by graph over the corpus. It depends on
// that installation, its version and its fonts, so it never runs in CI: set
// GO_GRAPHVIZ_SMOKE=1, and GO_GRAPHVIZ_DOT to the dot binary when it is not
// the dot on PATH. `just smoke` does both. It fails only on gross
// differences; its use is the side-by-side images it writes, ours on the
// left, dot's in the middle, their difference on the right, to look at.
const (
	// smokeSizeTolerance is how far, as a fraction, the two pages' sides
	// may differ: two Graphviz versions lay text out at slightly different
	// widths.
	smokeSizeTolerance = 0.10

	// smokeMaxDifference is the mean per-pixel difference, out of 255, of
	// the two pages scaled to a common thumbnail, above which a graph
	// fails: a blank or misplaced label moves it a little, a missing node
	// or a wrong fill a lot.
	smokeMaxDifference = 24.0

	smokeThumbWidth = 256
)

func TestDotSmoke(t *testing.T) {
	if os.Getenv("GO_GRAPHVIZ_SMOKE") == "" {
		t.Skip("set GO_GRAPHVIZ_SMOKE=1 (and GO_GRAPHVIZ_DOT if dot is not on PATH) to compare with a local Graphviz")
	}

	dot := os.Getenv("GO_GRAPHVIZ_DOT")
	if dot == "" {
		dot = "dot"
	}

	version, err := exec.CommandContext(t.Context(), dot, "-V").CombinedOutput()
	if err != nil {
		t.Fatalf("running %s -V: %v\n%s", dot, err, version)
	}

	out := os.Getenv("GO_GRAPHVIZ_SMOKE_DIR")
	if out == "" {
		out = t.TempDir()
	}

	if err := os.MkdirAll(out, 0o750); err != nil {
		t.Fatal(err)
	}

	t.Logf("comparing with %s; side-by-side images in %s", strings.TrimSpace(string(version)), out)

	var report []string

	for _, dir := range testPaths {
		if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}

			t.Run(path, func(t *testing.T) {
				report = append(report, smokeOne(t, dot, path, out))
			})

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}

	sort.Strings(report)
	t.Logf("mean difference per graph, out of 255 (fails above %.0f):\n%s",
		smokeMaxDifference, strings.Join(report, "\n"))
}

// smokeOne renders path with dot and with the library, writes their
// side-by-side image, and returns a report line led by the difference.
func smokeOne(t *testing.T, dot, path, out string) string {
	t.Helper()

	var theirs bytes.Buffer

	// Graphviz renders PNG at 96 DPI unless told otherwise, as this library
	// does; no -G flag is passed, so a dot without one works too.
	cmd := exec.CommandContext(t.Context(), dot, "-Tpng", path)
	cmd.Stdout = &theirs

	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v", dot, err)
	}

	reference, err := png.Decode(&theirs)
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	graph, err := graphviz.ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	g, err := graphviz.New(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	ours, err := g.RenderImage(t.Context(), graph)
	if err != nil {
		t.Fatal(err)
	}

	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	difference := thumbDifference(ours, reference)

	if err := writeSideBySide(filepath.Join(out, name+".png"), ours, reference); err != nil {
		t.Fatal(err)
	}

	ob, rb := ours.Bounds(), reference.Bounds()
	if !within(ob.Dx(), rb.Dx()) || !within(ob.Dy(), rb.Dy()) {
		t.Errorf("page %dx%d, dot's %dx%d", ob.Dx(), ob.Dy(), rb.Dx(), rb.Dy())
	}

	if difference > smokeMaxDifference {
		t.Errorf("mean difference %.1f of 255 against dot's rendering", difference)
	}

	return fmt.Sprintf("%6.1f  %s  (%dx%d, dot %dx%d)", difference, path, ob.Dx(), ob.Dy(), rb.Dx(), rb.Dy())
}

func within(a, b int) bool {
	return float64(max(a, b)-min(a, b)) <= smokeSizeTolerance*float64(max(a, b))
}

// thumbnail scales img to a grey thumbnail of a fixed width.
func thumbnail(img image.Image, height int) *image.Gray {
	thumb := image.NewGray(image.Rect(0, 0, smokeThumbWidth, height))
	draw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), img, img.Bounds(), draw.Src, nil)

	return thumb
}

// thumbDifference is the mean absolute difference of the two images scaled
// to the same thumbnail, out of 255.
func thumbDifference(a, b image.Image) float64 {
	height := max(1, smokeThumbWidth*b.Bounds().Dy()/max(1, b.Bounds().Dx()))
	ta, tb := thumbnail(a, height), thumbnail(b, height)

	total := 0

	for i := range ta.Pix {
		d := int(ta.Pix[i]) - int(tb.Pix[i])
		if d < 0 {
			d = -d
		}

		total += d
	}

	return float64(total) / float64(len(ta.Pix))
}

// writeSideBySide writes ours, theirs and their difference next to each
// other, at theirs' scale.
func writeSideBySide(path string, ours, theirs image.Image) error {
	size := theirs.Bounds().Size()
	canvas := image.NewNRGBA(image.Rect(0, 0, 3*size.X, size.Y))
	draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)

	left := image.Rect(0, 0, size.X, size.Y)
	draw.ApproxBiLinear.Scale(canvas, left, ours, ours.Bounds(), draw.Over, nil)
	draw.Draw(canvas, left.Add(image.Pt(size.X, 0)), theirs, theirs.Bounds().Min, draw.Over)

	for y := range size.Y {
		for x := range size.X {
			d := 255 - absDiff(grey(canvas.At(x, y)), grey(canvas.At(size.X+x, y)))
			canvas.Set(2*size.X+x, y, color.NRGBA{R: 255, G: d, B: d, A: 255})
		}
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}

	if err := png.Encode(file, canvas); err != nil {
		_ = file.Close()

		return err
	}

	return file.Close()
}

// grey is c's luminance.
func grey(c color.Color) uint8 {
	g, ok := color.GrayModel.Convert(c).(color.Gray)
	if !ok {
		return 0
	}

	return g.Y
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}

	return b - a
}
