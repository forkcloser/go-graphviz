package wasm

import "testing"

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
