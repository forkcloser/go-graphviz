package wasm_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/forkcloser/go-graphviz"
	"github.com/forkcloser/go-graphviz/gvc"
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// apiTrapChild marks the process TestAfterATrapTheAPIReturnsErrTrapped
// traps the module in.
const apiTrapChild = "GO_GRAPHVIZ_API_TRAP_CHILD"

// After a trap the calls a program makes return gvc.ErrTrapped, on graphs
// and instances made before it as on new ones, and none panics: a render,
// a setter and the Err that reports it, a getter, a close, a parse. The
// getters every one of them reads through panic on an error, and after a
// trap every read is one.
func TestAfterATrapTheAPIReturnsErrTrapped(t *testing.T) {
	if os.Getenv(apiTrapChild) == "" {
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAfterATrapTheAPIReturnsErrTrapped$", "-test.v")

		cmd.Env = append(os.Environ(), apiTrapChild+"=1")
		cmd.WaitDelay = time.Second

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the child: %v\n%s", err, out)
		}

		return
	}

	ctx := t.Context()

	g, err := graphviz.New(ctx)
	if err != nil {
		t.Fatal(err)
	}

	graph, err := graphviz.ParseBytes([]byte(`digraph { a -> b }`))
	if err != nil {
		t.Fatal(err)
	}

	node, err := graph.NodeByName("a")
	if err != nil || node == nil {
		t.Fatalf("node a: %v", err)
	}

	edge, err := graph.FirstOut(node)
	if err != nil || edge == nil {
		t.Fatalf("edge a -> b: %v", err)
	}

	if err := wasm.Trap(ctx); !errors.Is(err, gvc.ErrTrapped) {
		t.Fatalf("the trap returned %v, want ErrTrapped", err)
	}

	var out bytes.Buffer
	if err := g.Render(ctx, graph, graphviz.SVG, &out); !errors.Is(err, gvc.ErrTrapped) {
		t.Errorf("Render returned %v, want ErrTrapped", err)
	}

	node.SetLabel("one")
	edge.SetLabel("two")
	graph.SetLabel("three")

	if err := graph.Err(); !errors.Is(err, gvc.ErrTrapped) {
		t.Errorf("Err after three setters returned %v, want ErrTrapped", err)
	}

	if root := node.Root(); root != nil {
		t.Errorf("a node's Root returned %v, want nothing", root)
	}

	if _, err := graphviz.ParseBytes([]byte(`digraph { c }`)); !errors.Is(err, gvc.ErrTrapped) {
		t.Errorf("ParseBytes returned %v, want ErrTrapped", err)
	}

	if _, err := graphviz.New(ctx); !errors.Is(err, gvc.ErrTrapped) {
		t.Errorf("New returned %v, want ErrTrapped", err)
	}

	if err := graph.Close(); !errors.Is(err, gvc.ErrTrapped) {
		t.Errorf("a graph's Close returned %v, want ErrTrapped", err)
	}

	if err := g.Close(); !errors.Is(err, gvc.ErrTrapped) {
		t.Errorf("an instance's Close returned %v, want ErrTrapped", err)
	}
}
