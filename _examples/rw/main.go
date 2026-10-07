// Command rw writes a graph as DOT, reads the DOT back, adds to it, and
// renders the result to rw.png.
package main

import (
	"bytes"
	"context"
	"errors"
	"log"

	"github.com/forkcloser/go-graphviz"
)

// writeDOT builds a two-node graph and returns it as DOT.
func writeDOT(ctx context.Context) (out []byte, err error) {
	g, err := graphviz.New(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, g.Close()) }()

	graph, err := g.Graph()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, graph.Close()) }()

	n, err := graph.CreateNodeByName("n")
	if err != nil {
		return nil, err
	}

	m, err := graph.CreateNodeByName("m")
	if err != nil {
		return nil, err
	}

	e, err := graph.CreateEdgeByName("e", n, m)
	if err != nil {
		return nil, err
	}

	e.SetLabel("e")

	var buf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.GV, &buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// extendAndRender parses dot, adds a node and an edge, and writes a PNG.
func extendAndRender(ctx context.Context, dot []byte) (err error) {
	graph, err := graphviz.ParseBytes(dot)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, graph.Close()) }()

	n, err := graph.NodeByName("n")
	if err != nil {
		return err
	}

	l, err := graph.CreateNodeByName("l")
	if err != nil {
		return err
	}

	e2, err := graph.CreateEdgeByName("e2", n, l)
	if err != nil {
		return err
	}

	e2.SetLabel("e2")

	g, err := graphviz.New(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, g.Close()) }()

	return g.RenderFilename(ctx, graph, graphviz.PNG, "rw.png")
}

func main() {
	ctx := context.Background()

	dot, err := writeDOT(ctx)
	if err != nil {
		log.Fatal(err)
	}

	if err := extendAndRender(ctx, dot); err != nil {
		log.Fatal(err)
	}
}
