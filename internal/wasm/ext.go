package wasm

import (
	"context"
	"fmt"
	"io"
	"io/fs"
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
// run it with, which carries the token, and the matching leave.
func (l *reentrantLock) enter(ctx context.Context) (context.Context, func()) {
	if tok := tokenOf(ctx); tok != nil && tok == l.token.Load() {
		return ctx, func() {}
	}

	l.lock()

	if l.depth == 1 {
		l.token.Store(new(callToken))
	}

	return withToken(ctx, l.token.Load()), l.unlock
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

func PluginAPIZero(ctx context.Context) (*PluginAPI, error) {
	slot, err := mod.NewPtr(ctx)
	if err != nil {
		return nil, err
	}

	if _, err = mod.invoke(ctx, "wasm_bridge_PluginAPI_zero", slot); err != nil {
		return nil, fmt.Errorf("wasm_bridge_PluginAPI_zero: %w", err)
	}

	ptr, err := mod.readU32(ctx, slot)
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

	if _, err = mod.invoke(ctx, "wasm_bridge_PluginInstalled_zero", slot); err != nil {
		return nil, fmt.Errorf("wasm_bridge_PluginInstalled_zero: %w", err)
	}

	ptr, err := mod.readU32(ctx, slot)
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
