package graphviz_test

import (
	"bytes"
	"context"
	"errors"
	"image/color"
	"io"
	"io/fs"
	"runtime/debug"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// memoryGrowth runs op n times after a warm-up and returns how far the
// module's memory grew. Memory grows in 64 KiB pages and never shrinks, so
// a leak of a few dozen bytes a call shows after a few thousand calls.
func memoryGrowth(n int, op func()) uint32 {
	op()

	before := wasm.MemorySize()

	for range n {
		op()
	}

	return wasm.MemorySize() - before
}

// Rendering, in PNG as in SVG, leaves nothing in the module: the output
// Graphviz allocates for the caller is freed, the device writes into it
// instead of replacing it, and a struct field read in a callback is the
// field, not a copy. A render used to leave about 4.5 KiB.
func TestRenderLeavesNoMemory(t *testing.T) {
	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, g.Close) })

	for _, format := range []graphviz.Format{graphviz.PNG, graphviz.SVG} {
		growth := memoryGrowth(2000, func() {
			graph, err := graphviz.ParseBytes([]byte(`digraph { a -> b; b [shape=box label="text"] }`))
			if err != nil {
				t.Fatal(err)
			}

			var out bytes.Buffer
			if err := g.Render(ctx, graph, format, &out); err != nil {
				t.Fatal(err)
			}

			closeOrError(t, graph.Close)
		})

		if growth != 0 {
			t.Errorf("%s: memory grew %d KiB over 2000 renders", format, growth/1024)
		}
	}
}

// Making and closing an instance leaves nothing in the module, whatever
// plugins it is made with: the default plugins and the list Graphviz loads
// them from are built once and shared, and a context frees the list it was
// made from with itself. Each instance used to leave about 4 KiB, then the
// 48 bytes of its list; one made with no plugins about 130, and one with
// plugins of its own a few hundred, the list's symbol, library and names.
func TestNewLeavesNoMemory(t *testing.T) {
	ctx := t.Context()

	render, err := graphviz.NewRenderPlugin(ctx, "count", new(graphviz.DefaultRenderEngine))
	if err != nil {
		t.Fatal(err)
	}

	device, err := graphviz.NewDevicePlugin(ctx, "count:count")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name string
		make func() (*graphviz.Graphviz, error)
	}{
		{"default plugins", func() (*graphviz.Graphviz, error) { return graphviz.New(ctx) }},
		{"no plugins", func() (*graphviz.Graphviz, error) { return graphviz.NewWithPlugins(ctx) }},
		{"own plugins", func() (*graphviz.Graphviz, error) { return graphviz.NewWithPlugins(ctx, render, device) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			growth := memoryGrowth(4000, func() {
				g, err := tc.make()
				if err != nil {
					t.Fatal(err)
				}

				closeOrError(t, g.Close)
			})

			if growth != 0 {
				t.Errorf("memory grew %d KiB over 4000 instances", growth/1024)
			}
		})
	}
}

// A context with no plugins of its own is made from the built-in list,
// terminated: without the terminator Graphviz read past the end of the
// list, and making the context failed or not depending on what followed.
func TestNewWithNoPlugins(t *testing.T) {
	for range 100 {
		c, err := gvc.NewWithPlugins(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		closeOrError(t, c.Close)
	}
}

// trackingFS counts the files open on a file system, its root aside.
type trackingFS struct {
	fstest.MapFS

	mu   sync.Mutex
	open int
}

type trackedFile struct {
	fs.File

	owner *trackingFS
}

// Seek and ReadAt pass through: Graphviz rewinds and reads an image's
// header through them.
func (f *trackedFile) Seek(offset int64, whence int) (int64, error) {
	seeker, ok := f.File.(io.Seeker)
	if !ok {
		return 0, errors.ErrUnsupported
	}

	return seeker.Seek(offset, whence)
}

func (f *trackedFile) ReadAt(p []byte, offset int64) (int, error) {
	reader, ok := f.File.(io.ReaderAt)
	if !ok {
		return 0, errors.ErrUnsupported
	}

	return reader.ReadAt(p, offset)
}

func (f *trackedFile) Close() error {
	f.owner.mu.Lock()
	f.owner.open--
	f.owner.mu.Unlock()

	return f.File.Close()
}

func (t *trackingFS) Open(name string) (fs.File, error) {
	file, err := t.MapFS.Open(name)
	if err != nil || name == "." {
		// The root is the directory the module mounts, open for good.
		return file, err
	}

	t.mu.Lock()
	t.open++
	t.mu.Unlock()

	return &trackedFile{File: file, owner: t}, nil
}

// Graphviz closes a node image's file once it has sized it: it kept up to
// 50 open for the life of the process, which on Windows pins them.
func TestNodeImageFilesClosed(t *testing.T) {
	images := &trackingFS{MapFS: fstest.MapFS{}}
	for _, name := range []string{"a.png", "b.png", "c.png"} {
		images.MapFS[name] = &fstest.MapFile{Data: encodePNG(t, solidImage(4, 4, color.RGBA{R: 255, A: 255}))}
	}

	renderWithImages(t, images,
		`digraph { a [image="a.png" label=""]; b [image="b.png" label=""]; c [image="c.png" label=""] }`,
		graphviz.PNG)

	images.mu.Lock()
	defer images.mu.Unlock()

	if images.open != 0 {
		t.Errorf("%d image files left open after the render", images.open)
	}
}

// countingEngine counts the jobs it begins.
type countingEngine struct {
	*graphviz.DefaultRenderEngine

	jobs int
}

func (e *countingEngine) BeginJob(_ context.Context, _ *graphviz.Job) error {
	e.jobs++

	return nil
}

// A context built with plugins of its own renders with them, however many
// such contexts came and went before: a plugin list kept for collected
// plugins would be handed to new ones at the same addresses, and the
// render would run the old, unregistered engine without error.
func TestCustomPluginsAfterCollection(t *testing.T) {
	ctx := t.Context()

	graph, err := graphviz.ParseBytes([]byte(`digraph { a -> b }`))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { closeOrError(t, graph.Close) })

	for round := range 100 {
		engine := &countingEngine{DefaultRenderEngine: new(graphviz.DefaultRenderEngine)}

		render, err := graphviz.NewRenderPlugin(ctx, "count", engine)
		if err != nil {
			t.Fatal(err)
		}

		device, err := graphviz.NewDevicePlugin(ctx, "count:count")
		if err != nil {
			t.Fatal(err)
		}

		g, err := graphviz.NewWithPlugins(ctx, render, device)
		if err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if err := g.Render(ctx, graph, "count", &out); err != nil {
			t.Fatal(err)
		}

		closeOrError(t, g.Close)
		// Collect the closed context's plugins, so the next round's can
		// take their addresses.
		debug.FreeOSMemory()

		if engine.jobs != 1 {
			t.Fatalf("round %d: the context's own engine began %d jobs, want 1", round, engine.jobs)
		}
	}
}
