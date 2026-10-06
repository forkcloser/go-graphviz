package wasm_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// ClearTextspanLayout writes the two words textspan_t keeps between its
// font pointer and its first double, and nothing else: the double the
// bridge writes as yoffset_layout is found right after them, unchanged,
// and the font is still the span's.
func TestClearTextspanLayout(t *testing.T) {
	ctx := t.Context()

	span, err := wasm.NewTextspan(ctx)
	if err != nil {
		t.Fatal(err)
	}

	textFont, err := wasm.NewTextFont(ctx)
	if err != nil {
		t.Fatal(err)
	}

	const yOffset = 1.5

	if err = span.SetFont(textFont); err != nil {
		t.Fatal(err)
	}

	if err = span.SetYOffsetLayout(yOffset); err != nil {
		t.Fatal(err)
	}

	wasm.FillTextspanLayout(span)

	if err = wasm.ClearTextspanLayout(ctx, span); err != nil {
		t.Fatal(err)
	}

	layout, freeLayout, err := wasm.TextspanLayoutWords(ctx, span)
	if err != nil {
		t.Fatal(err)
	}

	next, err := wasm.TextspanAfterLayout(ctx, span)
	if err != nil {
		t.Fatal(err)
	}

	if layout != 0 || freeLayout != 0 {
		t.Errorf("layout %#x, free_layout %#x after clearing, want both 0", layout, freeLayout)
	}

	if got := math.Float64frombits(binary.LittleEndian.Uint64(next)); got != yOffset {
		t.Errorf("the double after the cleared words is %v, want yoffset_layout %v", got, yOffset)
	}

	if got := span.GetYOffsetLayout(); got != yOffset {
		t.Errorf("yoffset_layout %v after clearing, want %v", got, yOffset)
	}

	if wasm.WasmPtr(span.GetFont()) != wasm.WasmPtr(textFont) {
		t.Error("the span's font changed when its layout was cleared")
	}
}
