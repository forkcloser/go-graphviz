package wasm

import (
	"context"
	"testing"
)

// An exported function is kept and handed out again once its call ends,
// and a call nested in one in progress gets a fresh one: a wazero function
// is not re-entrant.
func TestFunctionReuse(t *testing.T) {
	outer, doneOuter := mod.function("free")
	nested, doneNested := mod.function("free")

	if outer == nested {
		t.Error("a nested call was handed the function the outer call is in")
	}

	doneNested()
	doneOuter()

	again, done := mod.function("free")
	defer done()

	if again != outer {
		t.Error("a function was not reused once its call ended")
	}
}

// A call into the module allocates no more when no trap happens than it did
// before traps were recorded: three objects for a call made inside another,
// as measured, where taking the address of the call's own error for the
// record made it four, one for every call of every render.
func TestACallAllocatesNothingForTheTrapRecord(t *testing.T) {
	const want = 3

	_ = Exclusive(t.Context(), func(ctx context.Context) error {
		if got := testing.AllocsPerRun(1000, func() { _ = mod.free(ctx, 0) }); got > want {
			t.Errorf("a call into the module makes %.0f allocations, want at most %d", got, want)
		}

		return nil
	})
}
