package wasm

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
)

// Graphviz reports through one function, agerrf, in fragments: the level
// ("Error" or "Warning"), then ": ", then the text; a continuation line
// (AGPREV) comes as text alone. The module routes that stream here rather
// than to the guest's stderr, which wazero discards: the errors since the
// last read wait for TakeLastError, and warnings go to the writer
// SetWarningWriter names.
var (
	messageMu     sync.Mutex
	messageLevel  string // the level of the message in progress
	lastErrorText strings.Builder
	warningWriter = io.Discard
)

// The fragments Graphviz's out() (lib/cgraph/agerror.c) emits before a
// message's text: the level, then the separator. onMessage matches them
// exactly, so a Graphviz release that changes out() breaks the tests that
// read a message rather than silently misfiling the text.
const (
	levelError     = "Error"
	levelWarning   = "Warning"
	levelSeparator = ": "
)

// RouteMessages installs onMessage as Graphviz's message function. cgraph
// calls it once at its own init, so every package that reaches Graphviz has
// the route in place.
func RouteMessages(ctx context.Context) error {
	Register_UserRef(func(string) (uint64, error) { return 0, nil })

	return SetErrorf(ctx, CreateCallbackFunc(onMessage, 0))
}

// onMessage receives one fragment of Graphviz's message stream.
func onMessage(_ context.Context, fragment string) (int, error) {
	messageMu.Lock()
	defer messageMu.Unlock()

	switch fragment {
	case levelError, levelWarning:
		// Errors accumulate until read, one per line, so two reported in
		// one call both reach the caller.
		if fragment == levelError && lastErrorText.Len() > 0 {
			lastErrorText.WriteString("\n")
		}

		messageLevel = fragment
	case levelSeparator:
		// the separator after the level
	default:
		if messageLevel == levelError {
			lastErrorText.WriteString(fragment)

			return 0, nil
		}
	}

	if messageLevel == levelWarning {
		_, _ = io.WriteString(warningWriter, fragment)
	}

	return 0, nil
}

// TakeLastError returns the text of the errors Graphviz reported since the
// last read, with their continuation lines, and clears it; "" when there
// is none.
func TakeLastError() string {
	messageMu.Lock()
	defer messageMu.Unlock()

	text := lastErrorText.String()
	lastErrorText.Reset()

	return text
}

// SetWarningWriter names where Graphviz's warnings go, as the lines
// Graphviz prints them ("Warning: ..."); nil drops them, the default.
func SetWarningWriter(w io.Writer) {
	messageMu.Lock()
	defer messageMu.Unlock()

	if w == nil {
		w = io.Discard
	}

	warningWriter = w
}

func DefaultSymList(ctx context.Context) ([]*SymList, error) {
	slot, err := mod.NewPtr(ctx)
	if err != nil {
		return nil, err
	}

	if _, err = mod.ExportedFunction("wasm_bridge_SymList_default").Call(ctx, slot); err != nil {
		return nil, fmt.Errorf("wasm_bridge_SymList_default: %w", err)
	}

	ptr, err := mod.readU32(slot)
	if err != nil {
		return nil, err
	}

	slice, err := mod.toSlice(ctx, ptr)
	if err != nil {
		return nil, err
	}

	return newSymListSlice(slice), nil
}

func PluginAPIZero(ctx context.Context) (*PluginAPI, error) {
	slot, err := mod.NewPtr(ctx)
	if err != nil {
		return nil, err
	}

	if _, err = mod.ExportedFunction("wasm_bridge_PluginAPI_zero").Call(ctx, slot); err != nil {
		return nil, fmt.Errorf("wasm_bridge_PluginAPI_zero: %w", err)
	}

	ptr, err := mod.readU32(slot)
	if err != nil {
		return nil, err
	}

	return newPluginAPI(ptr), nil
}

func PluginInstalledZero(ctx context.Context) (*PluginInstalled, error) {
	slot, err := mod.NewPtr(ctx)
	if err != nil {
		return nil, err
	}

	if _, err = mod.ExportedFunction("wasm_bridge_PluginInstalled_zero").Call(ctx, slot); err != nil {
		return nil, fmt.Errorf("wasm_bridge_PluginInstalled_zero: %w", err)
	}

	ptr, err := mod.readU32(slot)
	if err != nil {
		return nil, err
	}

	return newPluginInstalled(ptr), nil
}

func SymListZero(ctx context.Context) (*SymList, error) {
	slot, err := mod.NewPtr(ctx)
	if err != nil {
		return nil, err
	}

	if _, err = mod.ExportedFunction("wasm_bridge_SymList_zero").Call(ctx, slot); err != nil {
		return nil, fmt.Errorf("wasm_bridge_SymList_zero: %w", err)
	}

	ptr, err := mod.readU32(slot)
	if err != nil {
		return nil, err
	}

	return newSymList(ptr), nil
}

var fsMu sync.Mutex

func SetWasmFileSystem(fsys fs.FS) {
	fsMu.Lock()
	mod.fs.subFS = fsys
	fsMu.Unlock()
}

func FileSystem() fs.FS {
	fsMu.Lock()
	defer fsMu.Unlock()

	return mod.fs
}
