// Package graphviz is the facade of the library: a Graphviz instance built
// from the embedded WebAssembly module, the graph it lays out and renders, and
// aliases for every type of the cgraph, cdt and gvc packages, so a program
// imports this one package for the whole API.
//
// One WebAssembly module serves the whole process, and Graphviz is
// single-threaded, so calls are serialized: instances and graphs may be used
// from any goroutine, and a call waits for the one in progress. A callback a
// render makes (a RenderEngine method, a FontLoader) runs inside that call
// and may use the API on the same goroutine; another goroutine's call waits
// until the render ends. The context and the handles a callback receives
// (the Job, its points, spans and colours) identify that call to the lock,
// so a callback must not hand them to another goroutine while it runs; the
// points, spans, boxes and colours are copies that are freed when the
// callback returns, so they are not to be kept past it either.
package graphviz

import (
	"context"
	"image"
	"io"
	"io/fs"

	"github.com/forkcloser/go-graphviz/cgraph"
	"github.com/forkcloser/go-graphviz/gvc"
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

type Graphviz struct {
	ctx    *gvc.Context
	name   string
	dir    *GraphDescriptor
	layout Layout
}

type Layout string

const (
	CIRCO     Layout = "circo"
	DOT       Layout = "dot"
	FDP       Layout = "fdp"
	NEATO     Layout = "neato"
	NOP       Layout = "nop"
	NOP1      Layout = "nop1"
	NOP2      Layout = "nop2"
	OSAGE     Layout = "osage"
	PATCHWORK Layout = "patchwork"
	SFDP      Layout = "sfdp"
	TWOPI     Layout = "twopi"
)

type Format string

const (
	XDOT Format = "dot"
	SVG  Format = "svg"
	PNG  Format = "png"
	JPG  Format = "jpg"
)

func New(ctx context.Context) (*Graphviz, error) {
	gctx, err := gvc.New(ctx)
	if err != nil {
		return nil, err
	}

	return &Graphviz{
		ctx:    gctx,
		dir:    Directed,
		layout: DOT,
	}, nil
}

func NewWithPlugins(ctx context.Context, plugins ...Plugin) (*Graphviz, error) {
	gctx, err := gvc.NewWithPlugins(ctx, plugins...)
	if err != nil {
		return nil, err
	}

	return &Graphviz{
		ctx:    gctx,
		dir:    Directed,
		layout: DOT,
	}, nil
}

func (g *Graphviz) Close() error {
	return g.ctx.Close()
}

func (g *Graphviz) SetLayout(layout Layout) *Graphviz {
	g.layout = layout
	return g
}

func (g *Graphviz) Render(ctx context.Context, graph *Graph, format Format, w io.Writer) (e error) {
	defer func() {
		if err := g.ctx.FreeLayout(ctx, graph); err != nil {
			e = err
		}
	}()

	if err := g.ctx.Layout(ctx, graph, string(g.layout)); err != nil {
		return err
	}

	return g.ctx.RenderData(ctx, graph, string(format), w)
}

func (g *Graphviz) RenderImage(ctx context.Context, graph *Graph) (img image.Image, e error) {
	defer func() {
		if err := g.ctx.FreeLayout(ctx, graph); err != nil {
			e = err
		}
	}()

	if err := g.ctx.Layout(ctx, graph, string(g.layout)); err != nil {
		return nil, err
	}

	rendered, err := g.ctx.RenderImage(ctx, graph, string(PNG))
	if err != nil {
		return nil, err
	}

	return rendered, nil
}

func (g *Graphviz) RenderFilename(ctx context.Context, graph *Graph, format Format, path string) (e error) {
	defer func() {
		if err := g.ctx.FreeLayout(ctx, graph); err != nil {
			e = err
		}
	}()

	if err := g.ctx.Layout(ctx, graph, string(g.layout)); err != nil {
		return err
	}

	return g.ctx.RenderFilename(ctx, graph, string(format), path)
}

func (g *Graphviz) Graph(option ...GraphOption) (*Graph, error) {
	for _, opt := range option {
		opt(g)
	}

	graph, err := cgraph.Open(g.name, g.dir, nil)
	if err != nil {
		return nil, err
	}

	return graph, nil
}

func SetFileSystem(fsys fs.FS) {
	wasm.SetWasmFileSystem(fsys)
}

// SetWarningWriter names where Graphviz's warnings go, as the lines Graphviz
// prints them; nil drops them, which is the default. Errors are not written
// there: the call that caused one returns it.
func SetWarningWriter(w io.Writer) {
	wasm.SetWarningWriter(w)
}
