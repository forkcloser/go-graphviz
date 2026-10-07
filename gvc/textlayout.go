package gvc

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// lineSpacing is the height of a line of text as a multiple of its font
// size, Graphviz's LINESPACING.
const lineSpacing = 1.2

// centerLine is how far below the middle of its line Graphviz centres a
// span, as a share of the font size: the value of its own size estimate,
// which the renderer's baseline is placed from.
const centerLine = 0.1

// pointsPerInch makes a face measure in points: at 72 dots per inch, a
// point is a dot.
const pointsPerInch = 72

// TextLayoutPlugin measures text for Graphviz's layout with the fonts the
// raster renderer draws it with, so a node's box fits its label. Without
// one, Graphviz sizes text from built-in estimates for Times, Courier and
// Arial. New installs it; a context built with NewWithPlugins measures
// with it only when it is among the plugins.
// Text whose font a FontLoader supplies is measured with that font.
type TextLayoutPlugin struct {
	plugin *wasm.PluginAPI
	funcID uint64
	uses   atomic.Int64
}

// NewTextLayoutPlugin builds the text-layout plugin.
func NewTextLayoutPlugin(ctx context.Context) (*TextLayoutPlugin, error) {
	plg, err := wasm.NewPluginAPI(ctx)
	if err != nil {
		return nil, err
	}

	if err = plg.SetApi(wasm.API_TEXTLAYOUT); err != nil {
		return nil, err
	}

	types, err := wasm.NewPluginInstalled(ctx)
	if err != nil {
		return nil, err
	}

	// Graphviz loads the text-layout plugin by this type name.
	if err = types.SetType("textlayout"); err != nil {
		return nil, err
	}

	engine, err := wasm.NewTextLayoutEngine(ctx)
	if err != nil {
		return nil, err
	}

	funcID := wasm.WasmPtr(engine)

	if err = engine.SetTextlayout(ctx, wasm.CreateCallbackFunc(layoutText, funcID)); err != nil {
		return nil, err
	}

	if err = types.SetEngine(engine); err != nil {
		return nil, err
	}

	term, err := wasm.PluginInstalledZero(ctx)
	if err != nil {
		return nil, err
	}

	if err = plg.SetTypes([]*wasm.PluginInstalled{types, term}); err != nil {
		return nil, err
	}

	return &TextLayoutPlugin{plugin: plg, funcID: funcID}, nil
}

func (p *TextLayoutPlugin) raw() *wasm.PluginAPI {
	return p.plugin
}

func (p *TextLayoutPlugin) acquire() {
	if p.uses.Add(1) == 1 {
		textLayouts.add(p.funcID)
	}
}

func (p *TextLayoutPlugin) release() {
	if p.uses.Add(-1) == 0 {
		textLayouts.remove(p.funcID)
		wasm.UnregisterCallbacks(p.funcID)
	}
}

// liveEngines is the text-layout engines installed in a live context.
// Graphviz calls the text-layout callback with the span alone, not the
// context or its engine, and every engine measures the same way, so the
// call goes to any of them.
type liveEngines struct {
	mu  sync.Mutex
	ids []uint64
}

var textLayouts liveEngines //nolint:gochecknoglobals // Graphviz's text-layout call names no engine

func (l *liveEngines) add(id uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.ids = append(l.ids, id)
}

func (l *liveEngines) remove(id uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if i := slices.Index(l.ids, id); i >= 0 {
		l.ids = slices.Delete(l.ids, i, i+1)
	}
}

// any returns a live engine, or 0 when there is none: no callback answers
// to 0, and Graphviz falls back to its estimate.
func (l *liveEngines) any() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.ids) == 0 {
		return 0
	}

	return l.ids[len(l.ids)-1]
}

// layoutText measures a span as Graphviz's text-layout plugins do: its
// width as drawn, in points, and Graphviz's own line height and offsets,
// which the renderer's vertical placement follows. The font is the one the
// renderer draws the span in, a font loader's included.
func layoutText(ctx context.Context, span *wasm.Textspan, _ []string) (bool, error) {
	textFont := toTextFont(span.GetFont())
	size := textFont.Size()

	loaded, err := fontForSpan(ctx, textFont)
	if err != nil {
		return false, err
	}

	face, err := loaded.face(size, pointsPerInch)
	if err != nil {
		return false, err
	}

	width := advance(fonts.fallback().runs(span.GetStr(), face, size, pointsPerInch))

	if err := wasm.SetTextspanSize(ctx, span, width, size*lineSpacing); err != nil {
		return false, err
	}

	if err := wasm.ClearTextspanLayout(ctx, span); err != nil {
		return false, err
	}

	if err := span.SetYOffsetLayout(size); err != nil {
		return false, err
	}

	if err := span.SetYOffsetCenterLine(centerLine * size); err != nil {
		return false, err
	}

	return true, nil
}
