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
	gvc *wasm.Context
}

func New(ctx context.Context) (*Context, error) {
	plugins, err := DefaultPlugins(ctx)
	if err != nil {
		return nil, err
	}

	return NewWithPlugins(ctx, plugins...)
}

func NewWithPlugins(ctx context.Context, plugins ...Plugin) (*Context, error) {
	plgs, err := newPlugins(ctx, plugins...)
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

	return &Context{gvc: gvc}, nil
}

// Close frees the context. gvFreeContext returns the number of errors
// Graphviz has reported since the process started, not a status for this
// call, so that number is not an error here.
func (c *Context) Close() error {
	_, err := c.gvc.FreeContext(context.Background())

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
	var (
		rendered    string
		renderedLen uint
	)

	res, err := c.gvc.RenderData(ctx, toGraphWasm(graph), format, &rendered, &renderedLen)
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

func (c *Context) RenderImage(ctx context.Context, g *cgraph.Graph, format string) (image.Image, error) {
	var buf bytes.Buffer
	if err := c.RenderData(ctx, g, format, &buf); err != nil {
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

	return &Context{gvc: gvc}, nil
}

func (c *Context) FreeClonedContext(ctx context.Context) error {
	return c.gvc.FreeClonedContext(ctx)
}

func newPlugins(ctx context.Context, plugins ...Plugin) ([]*wasm.SymList, error) {
	defaults, err := wasm.DefaultSymList(ctx)
	if err != nil {
		return nil, err
	}

	if len(plugins) == 0 {
		return defaults, nil
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

	symTerm, err := wasm.SymListZero(ctx)
	if err != nil {
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
