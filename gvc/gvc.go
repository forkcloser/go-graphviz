// Package gvc mirrors libgvc, Graphviz's layout and rendering context: the
// plugin registry, the render, device and image-loading plugins, and the
// raster renderer this module provides for PNG and JPEG output.
package gvc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the decoder image.Decode needs
	_ "image/png"  // registers the decoder image.Decode needs
	"io"
	"strings"

	"github.com/forkcloser/go-xos"

	"github.com/forkcloser/go-graphviz/cgraph"
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

type Context struct {
	gvc     *wasm.Context
	plugins []Plugin
	// list is the entry the context's own plugins are listed under, freed
	// with the context; nil for a context on the shared default list.
	list *wasm.SymList
}

func New(ctx context.Context) (*Context, error) {
	plugins, err := DefaultPlugins(ctx)
	if err != nil {
		return nil, err
	}

	return NewWithPlugins(ctx, plugins...)
}

func NewWithPlugins(ctx context.Context, plugins ...Plugin) (*Context, error) {
	plgs, list, err := pluginLists(ctx, plugins)
	if err != nil {
		return nil, err
	}

	gvc, err := wasm.GetContextWithPlugins(ctx, plgs, 1)
	if err != nil {
		return nil, err
	}

	if gvc == nil {
		return nil, ErrNoContext
	}

	for _, plugin := range plugins {
		plugin.acquire()
	}

	return &Context{gvc: gvc, plugins: plugins, list: list}, nil
}

// Close frees the context, with the plugin list it was made from, which
// Graphviz keeps for the context's life and leaves. gvFreeContext returns
// the number of errors Graphviz has reported since the process started, not
// a status for this call, so that number is not an error here.
//
// Close frees once: a second Close does nothing, where it freed the
// context again in the module every instance shares and released the
// plugins a second time, the default ones the process's, and every PNG and
// JPEG rendered after it was empty. Layout and RenderData on a closed
// context return ErrClosed.
//
// The whole of it is one turn in the module, as the closed check of Layout
// and RenderData is part of theirs: a context may be closed from one
// goroutine while another renders with it, and the render is then whole or
// refused. Freed in several turns, a render waiting for the module got it
// between the free and the mark that says so, and laid out on the freed
// context, which trapped the module.
func (c *Context) Close() error {
	return wasm.Exclusive(context.Background(), func(ctx context.Context) error {
		if c.gvc == nil {
			return nil
		}

		_, err := c.gvc.FreeContext(ctx)
		c.gvc = nil

		if c.list != nil {
			if freeErr := wasm.FreePluginList(ctx, c.list); err == nil {
				err = freeErr
			}

			c.list = nil
		}

		for _, plugin := range c.plugins {
			plugin.release()
		}

		return err
	})
}

// Layout lays the graph out with the named engine. It first returns the
// error a typed setter met on the graph (cgraph.Graph.Err), if one did: the
// graph does not hold what the program set.
//
// An error that is ErrReported is a layout that is done: Graphviz reported
// an error and laid the graph out all the same. The layout is on the graph,
// to render and to free as after nil: a caller that returns on it without
// FreeLayout leaves the layout in the module. After any other error there
// is nothing to free: what the layout left on the graph, Layout has freed.
//
// ctx is honoured until the layout starts: when it is done before the
// module is free for this call, Layout returns its error and lays nothing
// out. A layout that is running is not interrupted, whatever ctx does.
func (c *Context) Layout(ctx context.Context, graph *cgraph.Graph, engine string) error {
	if err := graph.Err(); err != nil {
		return fmt.Errorf("setting an attribute of the graph: %w", err)
	}

	ran := false

	err := wasm.Exclusive(ctx, func(ctx context.Context) error {
		// Read in the turn that uses it: see Close.
		if c.gvc == nil {
			return ErrClosed
		}

		mark := wasm.ErrorMark()
		ran = true

		res, err := c.gvc.Layout(ctx, toGraphWasm(graph), engine)
		if err != nil {
			return err
		}

		return outcome(res, mark)
	})

	// A callback's error reaches here and not the calls above: Graphviz went
	// on to the layout's end past it, and the layout is on the graph. A trap
	// leaves nothing that can be freed.
	if ran && err != nil && !errors.Is(err, ErrReported) && !errors.Is(err, ErrTrapped) {
		if freeErr := c.FreeLayout(ctx, graph); freeErr != nil {
			err = errors.Join(err, freeErr)
		}
	}

	return err
}

// RenderData renders graph, which a Layout has laid out, in format and
// writes the result to w. An error that is ErrReported comes after the
// write: the render is whole, and Graphviz reported an error during it.
//
// ctx is honoured until the render starts, as Layout's is; a render that is
// running is not interrupted.
func (c *Context) RenderData(ctx context.Context, graph *cgraph.Graph, format string, w io.Writer) error {
	var (
		rendered string
		reported error
	)

	err := wasm.Exclusive(ctx, func(ctx context.Context) error {
		// Read in the turn that uses it: see Close.
		if c.gvc == nil {
			return ErrClosed
		}

		defer c.releasePages()

		mark := wasm.ErrorMark()

		out, res, err := c.gvc.RenderOutput(ctx, toGraphWasm(graph), format)
		if err != nil {
			return err
		}

		rendered = out

		if err = outcome(res, mark); errors.Is(err, ErrReported) {
			reported = err

			return nil
		}

		return err
	})
	if err != nil {
		return err
	}

	if _, err := w.Write([]byte(rendered)); err != nil {
		return fmt.Errorf("writing the rendered %s: %w", format, err)
	}

	return reported
}

// RenderImage renders g in format and returns the image. For PNG with the
// module's own raster renderer, the page the renderer drew is returned as
// is; any other format or renderer is rendered to bytes and decoded.
func (c *Context) RenderImage(ctx context.Context, graph *cgraph.Graph, format string) (image.Image, error) {
	if renderer := c.imageRenderer(format); renderer != nil && format == pngFormat {
		return c.renderImageDirect(ctx, graph, format, renderer)
	}

	var buf bytes.Buffer

	reported := c.RenderData(ctx, graph, format, &buf)
	if reported != nil && !errors.Is(reported, ErrReported) {
		return nil, reported
	}

	img, _, err := image.Decode(&buf)
	if err != nil {
		return nil, fmt.Errorf("decoding the rendered %s: %w", format, err)
	}

	return img, reported
}

// RenderFilename renders graph in format into the file at filename: a
// RenderData into memory, then one write. Graphviz's own gvRenderFilename
// opens the file inside the module, where the file system is read-only, so
// it wrote nothing for any format and returned success.
func (c *Context) RenderFilename(ctx context.Context, graph *cgraph.Graph, format, filename string) error {
	var buf bytes.Buffer

	reported := c.RenderData(ctx, graph, format, &buf)
	if reported != nil && !errors.Is(reported, ErrReported) {
		return reported
	}

	// #nosec G306 -- the caller names its own output file, readable like the dot command's
	if err := xos.WriteFile(filename, buf.Bytes(), outputFileMode); err != nil {
		return fmt.Errorf("writing %s: %w", filename, err)
	}

	return reported
}

// FreeLayout frees the layout a Layout left on the graph, whatever ctx
// says, and on a closed context too: the layout is the graph's.
func (c *Context) FreeLayout(ctx context.Context, graph *cgraph.Graph) error {
	// The call and the reading of what Graphviz reported are one turn in
	// the module: read after it, the report of a layout another goroutine
	// is in the middle of would be taken for this call's. A free runs
	// whatever ctx says, as every free does: a layout kept because its
	// context was done would stay in the module for good.
	return wasm.Exclusive(context.WithoutCancel(ctx), func(ctx context.Context) error {
		mark := wasm.ErrorMark()

		res, err := c.gvc.FreeLayout(ctx, toGraphWasm(graph))
		if err != nil {
			return err
		}

		return outcome(res, mark)
	})
}

// pngFormat is the format RenderImage takes the drawn page for directly;
// JPEG keeps its lossy round trip, which is what a JPEG caller asked for.
const pngFormat = "png"

// renderImageDirect renders g with renderer told to keep its page instead
// of encoding it, holding the module so no other render runs on the
// renderer before the page is read back.
func (c *Context) renderImageDirect(
	ctx context.Context,
	graph *cgraph.Graph,
	format string,
	renderer *ImageRenderer,
) (image.Image, error) {
	var (
		img      image.Image
		reported error
	)

	err := wasm.Exclusive(ctx, func(ctx context.Context) error {
		renderer.imageOnly = true
		renderer.last = nil

		defer func() {
			renderer.imageOnly = false
			renderer.last = nil
		}()

		reported = c.RenderData(ctx, graph, format, io.Discard)
		if reported != nil && !errors.Is(reported, ErrReported) {
			return reported
		}

		img = renderer.last

		return nil
	})
	if err != nil {
		return nil, err
	}

	if img == nil {
		return nil, fmt.Errorf("%w: the %s renderer drew no page", cgraph.ErrGraphviz, format)
	}

	return img, reported
}

// releasePages drops what c's raster renderers hold of the page they last
// drew, the canvas and the node images. A renderer is its plugin's for as
// long as the plugin is installed, the default ones for the life of the
// process, and a page that fails is left before its end.
func (c *Context) releasePages() {
	for _, plugin := range c.plugins {
		render, ok := plugin.(*RenderPlugin)
		if !ok {
			continue
		}

		if renderer, ok := render.engine.(*ImageRenderer); ok {
			renderer.ctx, renderer.decoded, renderer.drawn = nil, nil, nil
			renderer.heldDecoded, renderer.heldDrawn = 0, 0
		}
	}
}

// imageRenderer is the module's raster renderer installed for format in c,
// or nil when c renders format with something else.
func (c *Context) imageRenderer(format string) *ImageRenderer {
	for _, plugin := range c.plugins {
		render, ok := plugin.(*RenderPlugin)
		if !ok || render.typ != format {
			continue
		}

		if renderer, ok := render.engine.(*ImageRenderer); ok {
			return renderer
		}
	}

	return nil
}

// newPlugins builds the symbol list Graphviz loads plugins from: the
// context's own plugins, when it has any, under one entry, then the
// built-in libraries. The second result is that entry, for the context to
// free with itself; nil when there is none.
func newPlugins(ctx context.Context, plugins ...Plugin) ([]*wasm.SymList, *wasm.SymList, error) {
	defaults, err := wasm.DefaultSymList(ctx)
	if err != nil {
		return nil, nil, err
	}

	symTerm, err := wasm.SymListZero(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Graphviz reads the list up to an entry with no name; without the
	// terminator, a context with no plugins of its own read past the end
	// of the built-in list.
	if len(plugins) == 0 {
		return append(defaults, symTerm), nil, nil
	}

	sym, err := wasm.NewSymList(ctx)
	if err != nil {
		return nil, nil, err
	}

	if err = sym.SetName("gvplugin_go_LTX_library"); err != nil {
		return nil, nil, err
	}

	lib, err := wasm.NewPluginLibrary(ctx)
	if err != nil {
		return nil, nil, err
	}

	if err = lib.SetPackageName("go"); err != nil {
		return nil, nil, err
	}

	var apis []*wasm.PluginAPI
	for _, plg := range plugins {
		apis = append(apis, plg.raw())
	}

	term, err := wasm.PluginAPIZero(ctx)
	if err != nil {
		return nil, nil, err
	}

	apis = append(apis, term)

	if err = lib.SetApis(apis); err != nil {
		return nil, nil, err
	}

	if err = sym.SetAddress(lib); err != nil {
		return nil, nil, err
	}

	return append(append([]*wasm.SymList{sym}, defaults...), symTerm), sym, nil
}

// outcome is the error of a call Graphviz returned result from, read with
// what Graphviz reported at its error level since mark, taken at the call's
// start in the same stay in the module: the call may be made inside
// another's, whose report so far stays that one's. A result other than zero
// is a failure, with that message when there is one. Zero with a message is
// a call that went on to its end past an error: the message is returned
// with ErrReported.
func outcome(result, mark int) error {
	text := strings.Trim(wasm.TakeErrorsSince(mark), "\n")

	switch {
	case result != 0 && text != "":
		return fmt.Errorf("%w: %s", cgraph.ErrGraphviz, text)
	case result != 0:
		return fmt.Errorf("%w: call failed with code %d and no message", cgraph.ErrGraphviz, result)
	case text != "":
		return fmt.Errorf("%w: %s (%w)", cgraph.ErrGraphviz, text, ErrReported)
	default:
		return nil
	}
}

// outputFileMode is the mode RenderFilename creates a file with: readable
// by everyone, as the dot command's output is.
const outputFileMode = 0o644

// ErrClosed is returned by a call on a Context that has been closed.
var ErrClosed = errors.New("graphviz context is closed")

// ErrReported marks the error of a call that did all it was asked while
// Graphviz reported an error on the way, a label it could not read for one:
// the layout is on the graph, the render is written, the image is returned,
// the file is there, as dot writes its output and exits 1. The error is a
// cgraph.ErrGraphviz too, with the message Graphviz reported.
var ErrReported = errors.New("reported, and the call went on to its end")

// ErrNoContext is returned when Graphviz cannot allocate a rendering context.
var ErrNoContext = errors.New("graphviz context could not be created")

// ErrCallbackPanic is returned by a render, or any call Graphviz makes Go
// callbacks during, when one of them panicked; the panic's value follows
// it. The call finishes as when a callback returns an error, and the
// Context stays usable.
var ErrCallbackPanic = wasm.ErrCallbackPanic

// ErrTrapped is returned by the call Graphviz trapped in, a stack it
// overflowed or memory it reached outside its own, and by every call after
// it, on any context or graph: the process has to be restarted to render
// again. A getter, which has no error to return, returns its zero value. A trap leaves Graphviz's state half-updated,
// so no call is let in
// to draw from it; and the module is one for the process, holding every
// graph and context of every caller, so none is made anew in its place.
var ErrTrapped = wasm.ErrTrapped
