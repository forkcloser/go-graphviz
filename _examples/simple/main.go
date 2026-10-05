// Command simple builds a two-node graph in Go and prints it as DOT.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/forkcloser/go-graphviz"
)

func run(ctx context.Context) (err error) {
	g, err := graphviz.New(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, g.Close()) }()

	graph, err := g.Graph()
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, graph.Close()) }()

	n, err := graph.CreateNodeByName("n")
	if err != nil {
		return err
	}

	m, err := graph.CreateNodeByName("m")
	if err != nil {
		return err
	}

	e, err := graph.CreateEdgeByName("e", n, m)
	if err != nil {
		return err
	}

	e.SetLabel("e")

	var buf bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.XDOT, &buf); err != nil {
		return err
	}

	fmt.Println(buf.String())

	return nil
}

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
