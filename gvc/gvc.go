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
	"os"
	"strings"

	"github.com/forkcloser/go-graphviz/cgraph"
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

type Context struct {
	gvc     *wasm.Context
	plugins []Plugin
}

func New(ctx context.Context) (*Context, error) {
	plugins, err := DefaultPlugins(ctx)
	if err != nil {
		return nil, err
	}

	return NewWithPlugins(ctx, plugins...)
}

func NewWithPlugins(ctx context.Context, plugins ...Plugin) (*Context, error) {
	plgs, err := pluginLists(ctx, plugins)
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

	return &Context{gvc: gvc, plugins: plugins}, nil
}

// Close frees the context. gvFreeContext returns the number of errors
// Graphviz has reported since the process started, not a status for this
// call, so that number is not an error here.
func (c *Context) Close() error {
	_, err := c.gvc.FreeContext(context.Background())

	for _, plugin := range c.plugins {
		plugin.release()
	}

	return err
}

func (c *Context) Layout(ctx context.Context, g *cgraph.Graph, engine string) error {
	res, err := c.gvc.Layout(ctx, toGraphWasm(g), engine)
	if err != nil {
		return err
	}

	return toError(res)
}

func (c *Context) RenderData(ctx context.Context, graph *cgraph.Graph, format string, w io.Writer) error {
	rendered, res, err := c.gvc.RenderOutput(ctx, toGraphWasm(graph), format)
	if err != nil {
		return err
	}

	if err := toError(res); err != nil {
		return err
	}

	if _, err := w.Write([]byte(rendered)); err != nil {
		return fmt.Errorf("writing the rendered %s: %w", format, err)
	}

	return nil
}

// RenderImage renders g in format and returns the image. For PNG with the
// module's own raster renderer, the page the renderer drew is returned as
// is; any other format or renderer is rendered to bytes and decoded.
func (c *Context) RenderImage(ctx context.Context, graph *cgraph.Graph, format string) (image.Image, error) {
	if renderer := c.imageRenderer(format); renderer != nil && format == pngFormat {
		return c.renderImageDirect(ctx, graph, format, renderer)
	}

	var buf bytes.Buffer
	if err := c.RenderData(ctx, graph, format, &buf); err != nil {
		return nil, err
	}

	img, _, err := image.Decode(&buf)
	if err != nil {
		return nil, fmt.Errorf("decoding the rendered %s: %w", format, err)
	}

	return img, nil
}

// RenderFilename renders graph in format into the file at filename: a
// RenderData into memory, then one write. Graphviz's own gvRenderFilename
// opens the file inside the module, where the file system is read-only, so
// it wrote nothing for any format and returned success.
func (c *Context) RenderFilename(ctx context.Context, graph *cgraph.Graph, format, filename string) error {
	var buf bytes.Buffer
	if err := c.RenderData(ctx, graph, format, &buf); err != nil {
		return err
	}

	// #nosec G306 -- the caller names its own output file, readable like the dot command's
	if err := os.WriteFile(filename, buf.Bytes(), outputFileMode); err != nil {
		return fmt.Errorf("writing %s: %w", filename, err)
	}

	return nil
}

func (c *Context) FreeLayout(ctx context.Context, g *cgraph.Graph) error {
	res, err := c.gvc.FreeLayout(ctx, toGraphWasm(g))
	if err != nil {
		return err
	}

	return toError(res)
}

func (c *Context) Clone(ctx context.Context) (*Context, error) {
	gvc, err := c.gvc.Clone(ctx)
	if err != nil {
		return nil, err
	}

	// The plugins stay the original's; a clone does not release them.
	return &Context{gvc: gvc, plugins: nil}, nil
}

func (c *Context) FreeClonedContext(ctx context.Context) error {
	return c.gvc.FreeClonedContext(ctx)
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
	var img image.Image

	err := wasm.Exclusive(ctx, func(ctx context.Context) error {
		renderer.imageOnly = true
		renderer.last = nil

		defer func() {
			renderer.imageOnly = false
			renderer.last = nil
		}()

		if err := c.RenderData(ctx, graph, format, io.Discard); err != nil {
			return err
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

	return img, nil
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

func newPlugins(ctx context.Context, plugins ...Plugin) ([]*wasm.SymList, error) {
	defaults, err := wasm.DefaultSymList(ctx)
	if err != nil {
		return nil, err
	}

	symTerm, err := wasm.SymListZero(ctx)
	if err != nil {
		return nil, err
	}

	// Graphviz reads the list up to an entry with no name; without the
	// terminator, a context with no plugins of its own read past the end
	// of the built-in list.
	if len(plugins) == 0 {
		return append(defaults, symTerm), nil
	}

	sym, err := wasm.NewSymList(ctx)
	if err != nil {
		return nil, err
	}

	if err = sym.SetName("gvplugin_go_LTX_library"); err != nil {
		return nil, err
	}

	lib, err := wasm.NewPluginLibrary(ctx)
	if err != nil {
		return nil, err
	}

	if err = lib.SetPackageName("go"); err != nil {
		return nil, err
	}

	var apis []*wasm.PluginAPI
	for _, plg := range plugins {
		apis = append(apis, plg.raw())
	}

	term, err := wasm.PluginAPIZero(ctx)
	if err != nil {
		return nil, err
	}

	apis = append(apis, term)

	if err = lib.SetApis(apis); err != nil {
		return nil, err
	}

	if err = sym.SetAddress(lib); err != nil {
		return nil, err
	}

	return append(append([]*wasm.SymList{sym}, defaults...), symTerm), nil
}

// toError maps a Graphviz result code: zero is success, anything else is a
// failure, with the message Graphviz reported when it reported one.
func toError(result int) error {
	if result == 0 {
		return nil
	}

	if text := wasm.TakeLastError(); text != "" {
		return fmt.Errorf("%w: %s", cgraph.ErrGraphviz, strings.TrimRight(text, "\n"))
	}

	return fmt.Errorf("%w: call failed with code %d and no message", cgraph.ErrGraphviz, result)
}

// outputFileMode is the mode RenderFilename creates a file with: readable
// by everyone, as the dot command's output is.
const outputFileMode = 0o644

// ErrNoContext is returned when Graphviz cannot allocate a rendering context.
var ErrNoContext = errors.New("graphviz context could not be created")
