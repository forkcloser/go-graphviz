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
