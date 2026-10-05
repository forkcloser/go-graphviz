package gvc

import (
	"context"
	"sync/atomic"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

type LoadImagePlugin struct {
	plugin *wasm.PluginAPI
	funcID uint64       // the engine's address, the key its callback is registered under
	uses   atomic.Int32 // the contexts the plugin is installed in
}

func NewLoadImagePlugin(ctx context.Context, typ string, engine LoadImageEngine) (*LoadImagePlugin, error) {
	cfg := defaultLoadImagePluginConfig(typ, engine)
	return buildLoadImagePlugin(ctx, cfg)
}

func (p *LoadImagePlugin) raw() *wasm.PluginAPI {
	return p.plugin
}

func (p *LoadImagePlugin) acquire() {
	p.uses.Add(1)
}

func (p *LoadImagePlugin) release() {
	if p.uses.Add(-1) == 0 {
		wasm.UnregisterCallbacks(p.funcID)
	}
}

type loadImageConfig struct {
	Type   string
	Engine LoadImageEngine
}

type LoadImageEngine interface {
	LoadImage(ctx context.Context, job *Job, shape *UserShape, bf *BoxFloat, filled bool) error
}

type DefaultLoadImageEngine struct{}

func PNGLoadImagePlugin(ctx context.Context, engine LoadImageEngine) (*LoadImagePlugin, error) {
	return NewLoadImagePlugin(ctx, "png:png", engine)
}

func defaultLoadImagePluginConfig(typ string, engine LoadImageEngine) *loadImageConfig {
	return &loadImageConfig{
		Type:   typ,
		Engine: engine,
	}
}

func buildLoadImagePlugin(ctx context.Context, cfg *loadImageConfig) (*LoadImagePlugin, error) {
	plg, err := wasm.NewPluginAPI(ctx)
	if err != nil {
		return nil, err
	}

	if err = plg.SetApi(wasm.API_LOADIMAGE); err != nil {
		return nil, err
	}

	types, err := wasm.NewPluginInstalled(ctx)
	if err != nil {
		return nil, err
	}

	if err = types.SetType(cfg.Type); err != nil {
		return nil, err
	}

	if err = types.SetQuality(1); err != nil {
		return nil, err
	}

	engine, err := newLoadImageEngine(ctx, cfg.Engine)
	if err != nil {
		return nil, err
	}

	if err = types.SetEngine(engine); err != nil {
		return nil, err
	}

	term, err := wasm.PluginInstalledZero(ctx)
	if err != nil {
		return nil, err
	}

	if err := plg.SetTypes([]*wasm.PluginInstalled{types, term}); err != nil {
		return nil, err
	}

	return &LoadImagePlugin{
		plugin: plg,
		funcID: wasm.WasmPtr(engine),
	}, nil
}

func newLoadImageEngine(ctx context.Context, engine LoadImageEngine) (*wasm.LoadImageEngine, error) {
	loadEngine, err := wasm.NewLoadImageEngine(ctx)
	if err != nil {
		return nil, err
	}

	ptr := wasm.WasmPtr(loadEngine)
	if err := loadEngine.SetLoadImage(
		ctx,
		wasm.CreateCallbackFunc(
			func(ctx context.Context, job *wasm.Job, shape *wasm.UserShape, bf *wasm.BoxFloat, filled bool) error {
				return engine.LoadImage(ctx, toJob(job), toUserShape(shape), toBoxFloat(bf), filled)
			},
			ptr,
		),
	); err != nil {
		return nil, err
	}

	return loadEngine, nil
}
