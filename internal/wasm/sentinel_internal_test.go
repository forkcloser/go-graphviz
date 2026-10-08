package wasm

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// A module that was never loaded refuses every call with ErrNotLoaded.
func TestNotLoaded(t *testing.T) {
	m := newWasmModule()

	if _, err := m.read(t.Context(), 0, 4); !errors.Is(err, ErrNotLoaded) {
		t.Errorf("read on an unloaded module = %v; want ErrNotLoaded", err)
	}

	if err := m.write(t.Context(), 0, []byte{0}); !errors.Is(err, ErrNotLoaded) {
		t.Errorf("write on an unloaded module = %v; want ErrNotLoaded", err)
	}
}

// A read or write past the module's memory is ErrMemoryAccess, with the
// address and the memory size after it.
func TestMemoryAccessOutOfBounds(t *testing.T) {
	past := uint64(mod.mod.Memory().Size()) + 16

	_, err := mod.read(t.Context(), past, 4)
	if !errors.Is(err, ErrMemoryAccess) {
		t.Errorf("read(%d, 4) = %v; want ErrMemoryAccess", past, err)
	}

	if _, err = mod.readU32(t.Context(), past); !errors.Is(err, ErrMemoryAccess) {
		t.Errorf("readU32(%d) = %v; want ErrMemoryAccess", past, err)
	}

	if err = mod.write(t.Context(), past, []byte{0}); !errors.Is(err, ErrMemoryAccess) {
		t.Errorf("write(%d) = %v; want ErrMemoryAccess", past, err)
	}
}

// A callback setter whose lookup function nothing registered is
// ErrNotRegistered, naming the Register_ call to make first.
func TestNotRegistered(t *testing.T) {
	if mod.lookupFuncMap.DictWalk != nil {
		t.Skip("Register_DictWalk was called in this process")
	}

	cb := CreateCallbackFunc(func(context.Context, any, any) (int, error) { return 0, nil }, 1)

	_, err := (&Dict{}).Walk(t.Context(), cb, nil)
	if !errors.Is(err, ErrNotRegistered) {
		t.Fatalf("Walk without Register_DictWalk = %v; want ErrNotRegistered", err)
	}

	if want := "call Register_DictWalk first"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q; want it to name %q", err, want)
	}
}
