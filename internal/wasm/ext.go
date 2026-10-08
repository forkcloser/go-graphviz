package wasm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// reentrantLock serializes the module. Graphviz is single-threaded with
// global state and wazero's Call is not goroutine-safe, so one call runs at
// a time across every instance in the process. The goroutine inside a call
// calls again from every host function Graphviz invokes, so the lock is
// re-entrant for its holder, and it has to tell the holder from a waiting
// goroutine cheaply: the outermost call mints a token into the context it
// hands wazero, wazero hands that context to the host functions, the
// handles they build carry it, and a call that arrives with the current
// token is the holder's. A call without one takes the mutex, or, when that
// is held, asks which goroutine it is on (the slow path: a handle made
// outside the callback, or real contention).
type reentrantLock struct {
	mu     sync.Mutex
	holder atomic.Int64              // the goroutine holding mu, 0 when none
	token  atomic.Pointer[callToken] // the outermost call's token, nil when none
	depth  int                       // the holder's nesting, touched only by the holder
}

// callToken identifies one outermost call; a context carries a pointer to it.
type callToken struct{ _ byte }

// callKey is the context key the token travels under.
type callKey struct{}

// tokenOf is the token ctx carries, nil when none.
func tokenOf(ctx context.Context) *callToken {
	tok, ok := ctx.Value(callKey{}).(*callToken)
	if !ok {
		return nil
	}

	return tok
}

// withToken is ctx carrying token; ctx's deadline, cancellation and values
// stay its own.
func withToken(ctx context.Context, token *callToken) context.Context {
	if tokenOf(ctx) == token {
		return ctx
	}

	return context.WithValue(ctx, callKey{}, token)
}

// enter takes the lock for a call made under ctx and returns the context to
// run it with, which carries the token, the matching leave, and whether
// this is the outermost call: the one that acquired the lock, which is the
// one a parked callback error belongs to.
func (l *reentrantLock) enter(ctx context.Context) (context.Context, func(), bool) {
	if tok := tokenOf(ctx); tok != nil && tok == l.token.Load() {
		return ctx, func() {}, false
	}

	l.lock()

	outermost := l.depth == 1
	if outermost {
		l.token.Store(new(callToken))
	}

	return withToken(ctx, l.token.Load()), l.unlock, outermost
}

func (l *reentrantLock) lock() {
	if l.mu.TryLock() {
		l.holder.Store(goroutineID())
		l.depth = 1

		return
	}

	current := goroutineID()
	if l.holder.Load() == current {
		l.depth++

		return
	}

	l.mu.Lock()
	l.holder.Store(current)
	l.depth = 1
}

func (l *reentrantLock) unlock() {
	l.depth--
	if l.depth == 0 {
		l.token.Store(nil)
		l.holder.Store(0)
		l.mu.Unlock()
	}
}

// goroutineID is the running goroutine's number, read from the first line
// of its stack trace ("goroutine N [..."); the runtime offers no other way
// to tell goroutines apart, and the lock asks only when it is contended.
func goroutineID() int64 {
	var buf [64]byte

	n := runtime.Stack(buf[:], false)
	line := buf[:n]

	const (
		prefix = "goroutine "
		base   = 10
	)

	var number int64

	for _, c := range line[len(prefix):] {
		if c < '0' || c > '9' {
			break
		}

		number = number*base + int64(c-'0')
	}

	return number
}

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

// Exclusive runs calls with the module's lock held from start to end, so
// that no other goroutine's call runs between the calls it makes; it makes
// them through the context it is given, which carries the lock's token. An
// error a callback parked meanwhile is returned with its own.
func Exclusive(ctx context.Context, calls func(context.Context) error) error {
	if mod.mod == nil {
		return mod.unavailable()
	}

	ctx, leave, outermost := mod.lock.enter(ctx)
	defer leave()

	err := calls(ctx)
	if !outermost {
		return err
	}

	return errors.Join(err, mod.takeCallbackError())
}

// MemorySize is the module's memory size in bytes, for tests of growth.
func MemorySize() uint32 {
	if mod.mod == nil {
		return 0
	}

	_, leave, _ := mod.lock.enter(context.Background())
	defer leave()

	return mod.mod.Memory().Size()
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

// DefaultSymList lists the built-in plugin libraries: the static entries,
// which nothing frees.
func DefaultSymList(ctx context.Context) ([]*SymList, error) {
	slot, err := mod.NewPtr(ctx)
	if err != nil {
		return nil, err
	}

	defer func() { _ = mod.free(ctx, slot) }()

	if _, err = mod.invoke(ctx, "wasm_bridge_SymList_default", slot); err != nil {
		return nil, fmt.Errorf("wasm_bridge_SymList_default: %w", err)
	}

	ptr, err := mod.readU32(ctx, slot)
	if err != nil {
		return nil, err
	}

	slice, err := mod.toSlice(ctx, ptr)
	if err != nil {
		return nil, err
	}

	return newSymListSlice(slice), nil
}

// PluginAPIZero is the static terminator of an API list.
func PluginAPIZero(ctx context.Context) (*PluginAPI, error) {
	slot, err := mod.NewPtr(ctx)
	if err != nil {
		return nil, err
	}

	defer func() { _ = mod.free(ctx, slot) }()

	if _, err = mod.invoke(ctx, "wasm_bridge_PluginAPI_zero", slot); err != nil {
		return nil, fmt.Errorf("wasm_bridge_PluginAPI_zero: %w", err)
	}

	ptr, err := mod.readU32(ctx, slot)
	if err != nil {
		return nil, err
	}

	return newPluginAPI(ptr), nil
}

// PluginInstalledZero is the static terminator of a plugin's type list.
func PluginInstalledZero(ctx context.Context) (*PluginInstalled, error) {
	slot, err := mod.NewPtr(ctx)
	if err != nil {
		return nil, err
	}

	defer func() { _ = mod.free(ctx, slot) }()

	if _, err = mod.invoke(ctx, "wasm_bridge_PluginInstalled_zero", slot); err != nil {
		return nil, fmt.Errorf("wasm_bridge_PluginInstalled_zero: %w", err)
	}

	ptr, err := mod.readU32(ctx, slot)
	if err != nil {
		return nil, err
	}

	return newPluginInstalled(ptr), nil
}

// SymListZero is the static terminator of a symbol list.
func SymListZero(ctx context.Context) (*SymList, error) {
	slot, err := mod.NewPtr(ctx)
	if err != nil {
		return nil, err
	}

	defer func() { _ = mod.free(ctx, slot) }()

	if _, err = mod.invoke(ctx, "wasm_bridge_SymList_zero", slot); err != nil {
		return nil, fmt.Errorf("wasm_bridge_SymList_zero: %w", err)
	}

	ptr, err := mod.readU32(ctx, slot)
	if err != nil {
		return nil, err
	}

	return newSymList(ptr), nil
}

var fsMu sync.Mutex

// WasmFileSystem is the file system mounted at the module's root: the one
// SetWasmFileSystem names, or the host's when it names none.
type WasmFileSystem struct {
	subFS fs.FS
}

// Open opens a file Graphviz or the renderer names. The runtime mounts this
// file system at /, so Graphviz's names arrive relative to it, /a/b.png as
// a/b.png, while the renderer's arrive as the graph wrote them; trimming the
// leading slash makes both open the same file. On the host, a name is tried
// from the working directory and then from the root, since the mount has lost
// which of the two the graph meant; a name that resolves today keeps
// resolving to the same file.
func (w *WasmFileSystem) Open(name string) (fs.File, error) {
	fsMu.Lock()
	sub := w.subFS
	fsMu.Unlock()

	rel := strings.TrimLeft(name, "/")
	if rel == "" {
		rel = "."
	}

	// An fs.FS returns its *PathError as it is.
	if sub != nil {
		return sub.Open(rel) //nolint:wrapcheck // see above
	}

	file, err := os.Open(rel) // #nosec G304 -- opening the file a graph names is the point
	if err == nil || !errors.Is(err, fs.ErrNotExist) || filepath.IsAbs(rel) {
		return file, err //nolint:wrapcheck // see above
	}

	// #nosec G304 -- as above
	if rooted, rootErr := os.Open(string(filepath.Separator) + rel); rootErr == nil {
		return rooted, nil
	}

	return nil, err //nolint:wrapcheck // see above
}

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

// SetTextspanSize sets a text span's size, which Graphviz copies in from a
// point. It runs inside the text-layout callback that measures the span, so
// the point is made, filled and freed with that call's context.
func SetTextspanSize(ctx context.Context, span *Textspan, width, height float64) (err error) {
	point, err := mod.newObject(ctx, "PointFloat")
	if err != nil {
		return err
	}

	defer func() {
		if freeErr := mod.free(ctx, point); freeErr != nil && err == nil {
			err = freeErr
		}
	}()

	for field, value := range map[string]float64{"PointFloat_x": width, "PointFloat_y": height} {
		encoded, err := mod.toDoubleWasmValue(ctx, value)
		if err != nil {
			return err
		}

		if err := mod.setField(ctx, field, point, encoded); err != nil {
			return err
		}
	}

	return mod.setField(ctx, "Textspan_size", span.getPtr(), point)
}

// ClearTextspanLayout nulls a span's layout and free_layout, as every
// text-layout plugin must: Graphviz frees a span's layout with its
// free_layout when both are set, and measures the text of an HTML label in
// a span it leaves uninitialized.
func ClearTextspanLayout(ctx context.Context, span *Textspan) error {
	if _, err := mod.invoke(ctx, "wasm_bridge_Textspan_clearLayout", span.getPtr()); err != nil {
		return fmt.Errorf("wasm_bridge_Textspan_clearLayout: %w", err)
	}

	return nil
}

// WriteJobOutput hands a device's encoded page to Graphviz through
// gvwrite, as Graphviz's own devices do: rendering into memory, it appends
// to the buffer gvRenderData allocated and returns.
func WriteJobOutput(ctx context.Context, job *Job, data []byte) (err error) {
	if len(data) == 0 {
		return nil
	}

	buf, err := mod.malloc(ctx, uint64(len(data)))
	if err != nil {
		return err
	}

	defer func() {
		if freeErr := mod.free(ctx, buf); freeErr != nil && err == nil {
			err = freeErr
		}
	}()

	if err := mod.write(ctx, buf, data); err != nil {
		return err
	}

	if _, err := mod.invoke(ctx, "wasm_bridge_Job_writeOutput", job.getPtr(), buf, uint64(len(data))); err != nil {
		return fmt.Errorf("wasm_bridge_Job_writeOutput: %w", err)
	}

	return nil
}

// RenderOutput renders graph in format into memory and returns the output.
// gvRenderData allocates the output for its caller, which frees it with
// gvFreeRenderData, a plain free; the generated RenderData reads the output
// without freeing it, so every render left its output in the module. This
// reads it as owned, freeing the buffer and the bridge's wrapper.
func (v *Context) RenderOutput(ctx context.Context, graph *Graph, format string) (data string, result int, err error) {
	ctx = v.callContext(ctx)

	graphArg, err := mod.toObjectWasmValue(ctx, graph)
	if err != nil {
		return "", 0, err
	}

	formatArg, err := mod.toStringWasmValue(ctx, format)
	if err != nil {
		return "", 0, err
	}

	defer func() { _ = mod.free(ctx, formatArg) }()

	dataSlot, err := mod.NewPtr(ctx)
	if err != nil {
		return "", 0, err
	}

	defer func() { _ = mod.free(ctx, dataSlot) }()

	lengthSlot, err := mod.NewPtr(ctx)
	if err != nil {
		return "", 0, err
	}

	defer func() { _ = mod.free(ctx, lengthSlot) }()

	ret, err := mod.callWithRet(ctx, "Context_renderData", v.getPtr(), graphArg, formatArg, dataSlot, lengthSlot)
	if err != nil {
		return "", 0, err
	}

	wrapper, err := mod.readU64(ctx, dataSlot)
	if err != nil {
		return "", 0, err
	}

	data, err = mod.readString(ctx, wrapper, true)
	if err != nil {
		return "", 0, err
	}

	return data, mod.toInt(ret), nil
}

// ErrCallbackPanic is the error a render, layout or other call returns when
// a Go callback it made panicked; the panic's value follows it. The panic is
// recovered where Graphviz called the callback and the call finishes like
// one whose callback returned an error, so the instance stays usable.
var ErrCallbackPanic = errors.New("callback panicked")

// ErrOutOfMemory is returned by a call that needed more WebAssembly memory
// than the module can hold.
var ErrOutOfMemory = errors.New("out of WebAssembly memory")

// maxAllocation is the largest size malloc can be asked for: the module's
// addresses are 32 bits.
const maxAllocation = 1<<32 - 1
