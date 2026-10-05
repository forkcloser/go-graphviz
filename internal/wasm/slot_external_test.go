package wasm_test

import (
	"testing"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// A 64-bit field reads back whole: the bridge writes eight bytes into the
// slot a getter reads, so the slot is eight bytes and is read as eight. An
// object tag's id is cgraph's IDTYPE, a uint64_t.
func TestSixtyFourBitFieldRoundTrips(t *testing.T) {
	tag, err := wasm.NewTag(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	const id = uint64(1)<<40 + 5

	if err := tag.SetId(id); err != nil {
		t.Fatal(err)
	}

	if got := tag.GetId(); got != id {
		t.Fatalf("GetId() = %d, want %d", got, id)
	}
}
