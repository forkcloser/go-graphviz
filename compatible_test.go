package graphviz_test

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/png" // decodes the system dot's output when the hashes are regenerated
	"math/bits"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/image/draw"

	"github.com/forkcloser/go-graphviz"
)

var (
	testPaths = []string{
		filepath.Join("testdata", "directed"),
		filepath.Join("testdata", "undirected"),
	}
	imageHashJSON = filepath.Join("testdata", "imagehash.json")
)

const (
	imageThreshold = 40
)

// TestGenerateHashes rewrites testdata/imagehash.json from the system `dot`,
// the reference the compatibility test compares against. It runs only when
// GO_GRAPHVIZ_UPDATE_HASHES is set, since it needs Graphviz installed and
// changes the expected data; the ordinary run skips it.
func TestGenerateHashes(t *testing.T) {
	if os.Getenv("GO_GRAPHVIZ_UPDATE_HASHES") == "" {
		t.Skip("set GO_GRAPHVIZ_UPDATE_HASHES=1 to regenerate testdata/imagehash.json with the system dot")
	}

	if err := generateTestData(); err != nil {
		t.Fatal(err)
	}
}

func generateTestData() error {
	pathToHash := map[string]string{}

	for _, path := range testPaths {
		if err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			tmpfile, err := os.CreateTemp("", "graphviz")
			if err != nil {
				return err
			}
			defer os.Remove(tmpfile.Name())

			if err = exec.Command("dot", "-Tpng", "-o"+tmpfile.Name(), p).Run(); err != nil {
				return err
			}

			img, _, err := image.Decode(tmpfile)
			if err != nil {
				return err
			}

			pathToHash[filepath.ToSlash(p)] = fmt.Sprintf("%016x", differenceHash(img))

			return nil
		}); err != nil {
			return err
		}
	}

	content, err := json.MarshalIndent(pathToHash, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(imageHashJSON, append(content, '\n'), 0o644)
}

func TestGraphviz_Compatible(t *testing.T) {
	var pathToHash map[string]string

	file, err := os.ReadFile(imageHashJSON)
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(file, &pathToHash); err != nil {
		t.Fatal(err)
	}

	for _, path := range testPaths {
		if err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() {
				return nil
			}

			t.Run(path, func(t *testing.T) {
				compareWithDot(t, path, pathToHash[filepath.ToSlash(path)])
			})

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// compareWithDot renders the graph at path and fails when its difference
// hash is further than imageThreshold bits from want, the hash of the system
// dot's rendering.
func compareWithDot(t *testing.T, path, want string) {
	t.Helper()

	reference, err := strconv.ParseUint(want, 16, 64)
	if err != nil {
		t.Fatalf("no reference hash for %s: %v", path, err)
	}

	file, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	graph, err := graphviz.ParseBytes(file)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	img, err := g.RenderImage(ctx, graph)
	if err != nil {
		t.Fatal(err)
	}

	if distance := bits.OnesCount64(differenceHash(img) ^ reference); distance > imageThreshold {
		t.Fatalf("%s differs from the system dot's rendering by %d bits of 64", path, distance)
	}
}

// differenceHash is the 64-bit difference hash of img: the image scaled to
// 9 by 8 pixels, each row's 8 neighbouring pairs compared by luminosity, a
// bit set where the left one is darker, first row in the high bits. It is
// goimagehash's DifferenceHash, which made the reference hashes, with
// x/image/draw's bilinear scaling in place of nfnt/resize's. Measured over
// the corpus when the switch was made, the two disagree by at most 10 bits,
// and this one sits at most 25 bits from the references (goimagehash: 26),
// under the threshold of 40.
func differenceHash(img image.Image) uint64 {
	const (
		width  = 9
		height = 8
	)

	small := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.BiLinear.Scale(small, small.Bounds(), img, img.Bounds(), draw.Src, nil)

	var hash uint64

	bit := 63

	for y := range height {
		for x := range width - 1 {
			if luminosity(small.At(x, y)) < luminosity(small.At(x+1, y)) {
				hash |= 1 << bit
			}

			bit--
		}
	}

	return hash
}

// luminosity weighs a colour's channels as goimagehash does, including its
// division of the blue channel by 256 where the others divide by 257.
func luminosity(c interface{ RGBA() (r, g, b, a uint32) }) float64 {
	r, g, b, _ := c.RGBA()

	return 0.299*float64(r/257) + 0.587*float64(g/257) + 0.114*float64(b/256)
}
