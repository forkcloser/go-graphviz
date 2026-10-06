package gvc

import (
	"context"

	"github.com/forkcloser/go-graphviz/internal/wasm"
)

// Plugin is a render, device or image-loading plugin a Context is built
// with. One plugin value may serve several contexts; its callbacks stay
// registered until the last context using it closes.
type Plugin interface {
	raw() *wasm.PluginAPI
	// acquire counts a context the plugin is installed in; release counts
	// it out and drops the plugin's callbacks once no context uses it.
	acquire()
	release()
}

func DefaultPlugins(ctx context.Context) ([]Plugin, error) {
	pngRenderPlugin, err := PNGRenderPlugin(ctx)
	if err != nil {
		return nil, err
	}

	pngDevicePlugin, err := PNGDevicePlugin(ctx)
	if err != nil {
		return nil, err
	}

	jpgRenderPlugin, err := JPGRenderPlugin(ctx)
	if err != nil {
		return nil, err
	}

	jpgDevicePlugin, err := JPGDevicePlugin(ctx)
	if err != nil {
		return nil, err
	}

	pngLoadImagePlugin, err := PNGLoadImagePlugin(ctx, pngRenderPlugin.RenderEngine())
	if err != nil {
		return nil, err
	}

	textLayoutPlugin, err := NewTextLayoutPlugin(ctx)
	if err != nil {
		return nil, err
	}

	return []Plugin{
		pngRenderPlugin,
		pngDevicePlugin,
		jpgRenderPlugin,
		jpgDevicePlugin,
		pngLoadImagePlugin,
		textLayoutPlugin,
	}, nil
}
