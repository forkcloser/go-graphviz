package wasm

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// trapChild marks the process TestTrapEndsTheModule runs its trap in: the
// module is one for the process, and the test is that it stays ended.
const trapChild = "GO_GRAPHVIZ_TRAP_CHILD"

// A trap ends the module for the process: the call it happened in returns
// ErrTrapped with the trap, and every call after it returns that error
// without entering the module, where it ran on whatever the trap left.
func TestTrapEndsTheModule(t *testing.T) {
	if os.Getenv(trapChild) == "" {
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		cmd := exec.CommandContext(
			ctx,
			os.Args[0],
			"-test.run=^TestTrapEndsTheModule$",
			"-test.v",
		)

		cmd.Env = append(os.Environ(), trapChild+"=1")
		cmd.WaitDelay = time.Second

		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the child: %v\n%s", err, out)
		}

		return
	}

	ctx := t.Context()

	// A call that works, before: the module is whole.
	block, err := mod.malloc(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}

	// A field read through a pointer outside the module's memory traps.
	outside := uint64(mod.mod.Memory().Size()) + 1<<20

	_, trap := mod.getField(ctx, "Color_rgba_double", outside)
	if !errors.Is(trap, ErrTrapped) {
		t.Fatalf("a read outside the module's memory returned %v, want ErrTrapped", trap)
	}

	if !strings.Contains(trap.Error(), "out of bounds") {
		t.Errorf("the error does not carry the trap: %v", trap)
	}

	// Every call after it is refused with the same error, the ones that
	// would have worked included.
	if _, err := mod.malloc(ctx, 8); !errors.Is(err, ErrTrapped) || err.Error() != trap.Error() {
		t.Errorf("malloc after the trap returned %v, want %v", err, trap)
	}

	if err := mod.free(ctx, block); !errors.Is(err, ErrTrapped) {
		t.Errorf("free after the trap returned %v, want ErrTrapped", err)
	}

	if err := Exclusive(ctx, func(ctx context.Context) error {
		_, err := mod.malloc(ctx, 8)

		return err
	}); !errors.Is(err, ErrTrapped) {
		t.Errorf("a call inside Exclusive after the trap returned %v, want ErrTrapped", err)
	}
}
