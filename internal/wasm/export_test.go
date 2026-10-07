package wasm

import "context"

// LoadModule loads blob into a fresh module the way the package does at
// import, for tests of a load that fails.
func LoadModule(ctx context.Context, blob []byte) (*WasmModule, error) {
	m := newWasmModule()
	err := m.load(ctx, blob)

	return m, err
}

// FailedModule is a module whose load failed with err.
func FailedModule(err error) *WasmModule {
	m := newWasmModule()
	m.initErr = err

	return m
}

// CallExport calls an exported function of m.
func (m *WasmModule) CallExport(ctx context.Context, name string) error {
	_, err := m.invoke(ctx, name)

	return err
}

// ReadWord reads a 32-bit word of m's memory.
func (m *WasmModule) ReadWord(ctx context.Context, addr uint64) error {
	_, err := m.readU32(ctx, addr)

	return err
}

// Where textspan_t (Graphviz's lib/common/textspan.h) keeps its layout
// pointer and the function that frees it, in wasm32: after the str and font
// pointers. The test reads them to check the bridge's clearLayout.
const (
	textspanLayout     = 8
	textspanFreeLayout = 12
)

// TextspanLayoutWords reads the layout and free_layout words of a span.
func TextspanLayoutWords(ctx context.Context, span *Textspan) (layout, freeLayout uint64, err error) {
	if layout, err = mod.readU32(ctx, span.getPtr()+textspanLayout); err != nil {
		return 0, 0, err
	}

	freeLayout, err = mod.readU32(ctx, span.getPtr()+textspanFreeLayout)

	return layout, freeLayout, err
}

// TextspanAfterLayout reads the eight bytes after a span's free_layout
// word, where textspan_t keeps yoffset_layout.
func TextspanAfterLayout(ctx context.Context, span *Textspan) ([]byte, error) {
	return mod.read(ctx, span.getPtr()+textspanFreeLayout+4, 8)
}

// FillTextspanLayout writes ones over a span's layout and free_layout words,
// as an uninitialized span holds garbage there.
func FillTextspanLayout(span *Textspan) {
	base := uint32(span.getPtr())
	mod.mod.Memory().WriteUint32Le(base+textspanLayout, ^uint32(0))
	mod.mod.Memory().WriteUint32Le(base+textspanFreeLayout, ^uint32(0))
}

// PoisonNullPage writes ones over the module's first eight bytes, where a
// read through a NULL pointer lands, and returns what restores them.
func PoisonNullPage() func() {
	memory := mod.mod.Memory()
	saved, _ := memory.Read(0, 8)
	saved = append([]byte(nil), saved...)

	memory.Write(0, []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})

	return func() { memory.Write(0, saved) }
}
