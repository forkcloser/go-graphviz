// Package graphviz is the facade of the library: a Graphviz instance built
// from the embedded WebAssembly module, the graph it lays out and renders, and
// aliases for every type of the cgraph and gvc packages, so a program
// imports this one package for the whole API.
//
// One WebAssembly module serves the whole process, and Graphviz is
// single-threaded, so calls are serialized: instances and graphs may be used
// from any goroutine, and a call waits for the one in progress. Render,
// RenderImage and RenderFilename hold the module from layout to the end of
// the render, so the same graph may be rendered from several goroutines; a
// caller sequencing gvc's Layout, RenderData and FreeLayout on a graph
// itself must not let another goroutine render that graph in between. A callback a
// render makes (a RenderEngine method, a FontLoader) runs inside that call
// and may use the API on the same goroutine; another goroutine's call waits
// until the render ends. The context and the handles a callback receives
// (the Job, its points, spans and colours) identify that call to the lock,
// so a callback must not hand them to another goroutine while it runs; the
// points, spans, boxes and colours are copies that are freed when the
// callback returns, so they are not to be kept past it either.
package graphviz

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

// Render lays graph out with the instance's layout engine, renders it in
// format and writes the result to w.
func (g *Graphviz) Render(ctx context.Context, graph *Graph, format Format, w io.Writer) error {
	var buf bytes.Buffer

	err := g.laidOut(ctx, graph, func(ctx context.Context) error {
		return g.ctx.RenderData(ctx, graph, string(format), &buf)
	})
	if err != nil {
		return err
	}

	// Written after the module is released: a slow writer must not hold up
	// every other render in the process.
	if _, err := w.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("writing the rendered %s: %w", format, err)
	}

	return nil
}

// RenderImage lays graph out with the instance's layout engine and returns
// it drawn as an image.
func (g *Graphviz) RenderImage(ctx context.Context, graph *Graph) (image.Image, error) {
	var img image.Image

	err := g.laidOut(ctx, graph, func(ctx context.Context) error {
		var err error

		img, err = g.ctx.RenderImage(ctx, graph, string(PNG))

		return err
	})
	if err != nil {
		return nil, err
	}

	return img, nil
}

// RenderFilename lays graph out with the instance's layout engine and writes
// it, rendered in format, to the file at path.
func (g *Graphviz) RenderFilename(ctx context.Context, graph *Graph, format Format, path string) error {
	return g.laidOut(ctx, graph, func(ctx context.Context) error {
		return g.ctx.RenderFilename(ctx, graph, string(format), path)
	})
}

// Graph opens a new root graph, named and typed by the options given; the
// options apply to this graph only, and the next call starts again from the
// defaults the instance was created with (unnamed, directed).
func (g *Graphviz) Graph(option ...GraphOption) (*Graph, error) {
	call := *g
	for _, opt := range option {
		opt(&call)
	}

	graph, err := cgraph.Open(call.name, call.dir)
	if err != nil {
		return nil, err
	}

	return graph, nil
}

// laidOut lays graph out, runs render, and frees the layout, holding the
// module from the layout to the free: the layout lives on the graph, so a
// render of the same graph from another goroutine must not lay it out again
// or free it in between.
func (g *Graphviz) laidOut(ctx context.Context, graph *Graph, render func(context.Context) error) error {
	return wasm.Exclusive(ctx, func(ctx context.Context) (err error) {
		defer func() {
			err = errors.Join(err, g.ctx.FreeLayout(ctx, graph))
		}()

		if err := g.ctx.Layout(ctx, graph, string(g.layout)); err != nil {
			return err
		}

		return render(ctx)
	})
}

// SetFileSystem names the file system every file a graph names is read from:
// the files Graphviz opens, node images among them, and the images the
// renderer draws. nil restores the default, the host's. It is process-wide,
// for every instance, and takes effect on the next file opened, renders in
// progress included.
//
// A name is read as an fs.FS name with any leading slash removed, so
// image="/a/b.png" opens a/b.png. On the host, a name is tried from the
// working directory and then from the root.
func SetFileSystem(fsys fs.FS) {
	wasm.SetWasmFileSystem(fsys)
}

// SetWarningWriter names where Graphviz's warnings go, as the lines Graphviz
// prints them; nil drops them, which is the default. Errors are not written
// there: the call that caused one returns it.
func SetWarningWriter(w io.Writer) {
	wasm.SetWarningWriter(w)
}
