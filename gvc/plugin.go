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

	loadImagePlugins, err := imageLoaders(ctx, map[string]RenderEngine{
		pngFormat: pngRenderPlugin.RenderEngine(),
		"jpg":     jpgRenderPlugin.RenderEngine(),
	})
	if err != nil {
		return nil, err
	}

	textLayoutPlugin, err := NewTextLayoutPlugin(ctx)
	if err != nil {
		return nil, err
	}

	return append([]Plugin{
		pngRenderPlugin,
		pngDevicePlugin,
		jpgRenderPlugin,
		jpgDevicePlugin,
		textLayoutPlugin,
	}, loadImagePlugins...), nil
}

// imageLoaders makes an image-loading plugin for each image type Graphviz
// recognizes and Go decodes, into each output: Graphviz looks a loader up
// as the image's type and the renderer's, "jpeg:png" for a JPEG drawn into
// a PNG, and the loader draws on that output's renderer.
func imageLoaders(ctx context.Context, outputs map[string]RenderEngine) ([]Plugin, error) {
	var plugins []Plugin

	for _, imageType := range []string{pngFormat, "jpeg", "gif", "bmp", "webp"} {
		for output, renderer := range outputs {
			engine, ok := renderer.(LoadImageEngine)
			if !ok {
				continue
			}

			plugin, err := NewLoadImagePlugin(ctx, imageType+":"+output, engine)
			if err != nil {
				return nil, err
			}

			plugins = append(plugins, plugin)
		}
	}

	return plugins, nil
}
