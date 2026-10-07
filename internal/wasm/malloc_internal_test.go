package wasm

import (
	"errors"
	"testing"
)

// An allocation the module cannot make is ErrOutOfMemory, never the null
// pointer: the bridge would write a string there and overwrite low memory.
// Past 32 bits the size cannot be asked for; just under it, the module's
// malloc refuses and returns null.
func TestMallocOutOfMemory(t *testing.T) {
	for _, size := range []uint64{1 << 33, maxAllocation} {
		p, err := mod.malloc(t.Context(), size)
		if !errors.Is(err, ErrOutOfMemory) {
			t.Errorf("malloc(%d) = %d, %v; want ErrOutOfMemory", size, p, err)
		}
	}

	p, err := mod.malloc(t.Context(), 16)
	if err != nil || p == 0 {
		t.Fatalf("malloc(16) after a refusal = %d, %v", p, err)
	}

	if err := mod.free(t.Context(), p); err != nil {
		t.Fatal(err)
	}
}
