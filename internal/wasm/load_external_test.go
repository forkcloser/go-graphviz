package wasm_test

import (
	"errors"
	"testing"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// A blob that does not load comes back as an error, not a panic, and leaves
// a module whose every call returns that error.
func TestLoadFailureIsAnError(t *testing.T) {
	m, err := wasm.LoadModule(t.Context(), []byte("not a WebAssembly module"))
	if err == nil {
		t.Fatal("loading garbage succeeded")
	}

	if callErr := m.CallExport(t.Context(), "malloc"); callErr == nil {
		t.Fatal("a call into a module that did not load succeeded")
	}
}

var errNoModule = errors.New("no module")

// Calls and memory reads on a module whose load failed return the load's
// error.
func TestFailedModuleReturnsItsError(t *testing.T) {
	m := wasm.FailedModule(errNoModule)

	if err := m.CallExport(t.Context(), "malloc"); !errors.Is(err, errNoModule) {
		t.Fatalf("CallExport returned %v, want %v", err, errNoModule)
	}

	if err := m.ReadWord(t.Context(), 0); !errors.Is(err, errNoModule) {
		t.Fatalf("ReadWord returned %v, want %v", err, errNoModule)
	}
}
