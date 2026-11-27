package wasm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
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

// enterWaiting is enter for a call that honours its context: a caller that
// does not hold the lock gets ctx's error when ctx is done before the lock
// is its, and takes nothing then. The holder is let in whatever ctx says:
// what it runs inside its own call is the rest of that call, the frees
// among it.
func (l *reentrantLock) enterWaiting(ctx context.Context) (context.Context, entered, error) {
	if tok := tokenOf(ctx); tok != nil && tok == l.token.Load() {
		return ctx, entered{leave: func() {}}, nil
	}

	current := goroutineID()
	if l.holder.Load() == current {
		l.depth++

		return withToken(ctx, l.token.Load()), entered{leave: l.unlock}, nil
	}

	if err := ctx.Err(); err != nil {
		//nolint:wrapcheck // the bare ctx.Err() is the cancellation contract: callers match it with errors.Is
		return ctx, entered{}, err
	}

	if err := l.wait(ctx); err != nil {
		return ctx, entered{}, err
	}

	l.holder.Store(current)
	l.depth = 1
	l.token.Store(new(callToken))

	return withToken(ctx, l.token.Load()), entered{leave: l.unlock, outermost: true}, nil
}

// entered is a call let into the module: what gives the lock back, and
// whether the call is the outermost one, which a parked callback error
// belongs to.
type entered struct {
	leave     func()
	outermost bool
}

// wait takes the mutex, or gives up with ctx's error when ctx is done first.
func (l *reentrantLock) wait(ctx context.Context) error {
	if l.mu.TryLock() {
		return nil
	}

	// A mutex cannot be waited on with a context: a goroutine waits on it
	// and hands it over, or gives it back when nobody is waiting any more.
	handed, abandoned := make(chan struct{}), make(chan struct{})

	go func() {
		l.mu.Lock()

		select {
		case handed <- struct{}{}:
		case <-abandoned:
			l.mu.Unlock()
		}
	}()

	select {
	case <-handed:
		return nil
	case <-ctx.Done():
		close(abandoned)

		//nolint:wrapcheck // the bare ctx.Err(), as above
		return ctx.Err()
	}
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
// last read wait for TakeErrorsSince, and warnings go to the writer
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

// dropErrors forgets the errors Graphviz reported and no call read.
func dropErrors() {
	messageMu.Lock()
	defer messageMu.Unlock()

	lastErrorText.Reset()
}

// ErrorMark is where the text of the errors Graphviz has reported stands
// now: what TakeErrorsSince takes from.
func ErrorMark() int {
	messageMu.Lock()
	defer messageMu.Unlock()

	return lastErrorText.Len()
}

// TakeErrorsSince returns the text of the errors Graphviz reported since
// mark, with their continuation lines, and clears that much, leaving what
// it had reported before where it is; "" when there is none. It is how a
// call reads its own errors, between a mark taken at its start and its
// end, both in one Exclusive: the call may be made inside another's stay
// in the module, from a callback of a layout or a render, and what that
// one has had reported so far is its own to read at its end.
func TakeErrorsSince(mark int) string {
	messageMu.Lock()
	defer messageMu.Unlock()

	text := lastErrorText.String()
	if mark >= len(text) {
		return ""
	}

	lastErrorText.Reset()
	lastErrorText.WriteString(text[:mark])

	return text[mark:]
}

// Exclusive runs calls with the module's lock held from start to end, so
// that no other goroutine's call runs between the calls it makes; it makes
// them through the context it is given, which carries the lock's token. An
// error a callback parked meanwhile is returned with its own.
//
// It honours ctx until calls starts: when ctx is done before the module is
// free, it returns ctx's error and calls does not run. Once calls runs,
// nothing stops it: the module cannot be interrupted without being closed.
// Called from inside another Exclusive, it runs calls whatever ctx says.
//
// A stay that is not inside another begins with no error to read: what
// Graphviz reported to the calls before it, and none of them read, is
// dropped.
func Exclusive(ctx context.Context, calls func(context.Context) error) error {
	if mod.mod == nil {
		return mod.unavailable()
	}

	ctx, call, err := mod.lock.enterWaiting(ctx)
	if err != nil {
		return err
	}

	defer call.leave()

	if call.outermost {
		dropErrors()
	}

	err = calls(ctx)
	if !call.outermost {
		return err
	}

	return errors.Join(err, mod.takeCallbackError())
}

// Free frees the module memory at address, a handle's (WasmPtr) that a New
// constructor made, once Graphviz holds a copy of what it points to and not
// the handle's own: a struct set by value into another, an element copied
// into an array.
func Free(ctx context.Context, address uint64) error {
	return mod.free(ctx, address)
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

var (
	fsMu sync.Mutex
	// fsGeneration counts the file systems named, under fsMu.
	fsGeneration uint64
)

// WasmFileSystem is the file system mounted at the module's root: the one
// SetWasmFileSystem names, or the host's when it names none.
type WasmFileSystem struct {
	subFS fs.FS
}

// Open opens a file Graphviz or the renderer names. The runtime mounts this
// file system at /, so Graphviz's names arrive relative to it and clean,
// /a//b.png as a/b.png, while the renderer's arrive as the graph wrote them,
// or as Graphviz joined them to a directory of the graph's imagepath;
// trimming the leading slash and, for a named file system, which takes a
// clean name alone, cleaning the rest makes both open the same file. On the
// host, a name is tried from the working directory and then from the root,
// since the mount has lost which of the two the graph meant; a name that
// resolves today keeps resolving to the same file.
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
		return sub.Open(path.Clean(rel)) //nolint:wrapcheck // see above
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
	fsGeneration++
	fsMu.Unlock()
}

// FileSystemNamed says whether SetWasmFileSystem has named a file system:
// with none, a name is the host's.
func FileSystemNamed() bool {
	fsMu.Lock()
	defer fsMu.Unlock()

	return mod.fs.subFS != nil
}

// FileSystemGeneration changes each time SetWasmFileSystem names a file
// system: what was read under a name through one is not what the next
// holds under it, so what is kept of a file is kept with this.
func FileSystemGeneration() uint64 {
	fsMu.Lock()
	defer fsMu.Unlock()

	return fsGeneration
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

// SetDefaultAttr installs a default on Graphviz's prototype graph: agattr
// with a null graph, which every graph made afterwards inherits as its own
// default of that kind (AGRAPH, AGNODE or AGEDGE). A kind Graphviz does not
// know traps in the module, and the binding's own error carries that.
func SetDefaultAttr(ctx context.Context, kind int, name, value string) error {
	_, err := (&Graph{}).Attr(ctx, kind, name, value)

	return err
}

// ErrCallbackPanic is the error a render, layout or other call returns when
// a Go callback it made panicked; the panic's value follows it. The panic is
// recovered where Graphviz called the callback and the call finishes like
// one whose callback returned an error, so the instance stays usable.
var ErrCallbackPanic = errors.New("callback panicked")

// ErrOutOfMemory is returned by a call that needed more WebAssembly memory
// than the module can hold.
var ErrOutOfMemory = errors.New("out of WebAssembly memory")

// ErrTrapped is returned by the call the Graphviz WebAssembly module
// trapped in, and by every call after it: a stack it overflowed, an access
// outside its memory, an abort. The trap follows in the error.
//
// A getter, which has no error to return, returns its zero value from then
// on.
//
// The process has to be restarted to render again. A trap stops Graphviz
// where it stands, with its heap and its globals half-updated, and a call
// let in after that could draw from that state: so none is. No fresh module
// is made in its place either: the module is one for the process, and every
// graph, node, edge and context a caller holds lives in its memory.
var ErrTrapped = errors.New("the Graphviz WebAssembly module trapped")

// Trapped is the error every call returns once the module has trapped, and
// nil until then. It does not wait for the module.
func Trapped() error {
	if trap := mod.trapped.Load(); trap != nil {
		return *trap
	}

	return nil
}

// getterFailed is what a getter does with an error it has no way to
// return: it panics, since reading a field does not fail. Once the module
// has trapped every read fails; the getter then returns its zero value, and
// the calls that return an error return ErrTrapped.
func getterFailed(err error) {
	if !errors.Is(err, ErrTrapped) {
		panic(err)
	}
}

// ErrNotLoaded is returned by a call made before the Graphviz WebAssembly
// module was loaded, or after loading failed without an error of its own.
var ErrNotLoaded = errors.New("the Graphviz WebAssembly module is not loaded")

// ErrMemoryAccess is returned by a read or write of the module's memory at
// an address outside it: a pointer no call handed out, or one the module
// has since released. The address and the memory size follow it.
var ErrMemoryAccess = errors.New("WebAssembly memory access out of bounds")

// ErrNotRegistered is returned by a setter of a callback field whose
// lookup function was never registered: the Register_ call the message
// names has to come first.
var ErrNotRegistered = errors.New("callback lookup function not registered")

// maxAllocation is the largest size malloc can be asked for: the module's
// addresses are 32 bits.
const maxAllocation = 1<<32 - 1
