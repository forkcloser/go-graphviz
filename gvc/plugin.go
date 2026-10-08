package gvc

import (
	"context"
	"slices"
	"sync"

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

// sharedDefaults is the default plugin set, built once per process and
// shared by every context New makes: each set holds a few kilobytes of the
// module's memory that nothing frees, and New used to build one per
// context. The set is pinned, so closing a context never drops its
// callbacks.
type sharedDefaults struct {
	mu      sync.Mutex
	plugins []Plugin
	// lists is the symbol list of plugins, built for the first context.
	lists []*wasm.SymList
}

var defaults sharedDefaults //nolint:gochecknoglobals // one plugin set for the process's one module

// DefaultPlugins returns the plugins New installs: PNG and JPEG rendering
// and output, image loading into both, and text layout. They are built on
// the first call and the same plugins are returned to every caller; they
// stay registered for the life of the process.
func DefaultPlugins(ctx context.Context) ([]Plugin, error) {
	defaults.mu.Lock()
	defer defaults.mu.Unlock()

	if defaults.plugins == nil {
		plugins, err := newDefaultPlugins(ctx)
		if err != nil {
			return nil, err
		}

		for _, plugin := range plugins {
			plugin.acquire()
		}

		defaults.plugins = plugins
	}

	return slices.Clone(defaults.plugins), nil
}

// pluginLists returns the symbol list Graphviz loads plugins from. The
// list of the default set is built once and shared, as the set is: New
// built one per context, a few hundred bytes of the module's memory that
// nothing freed. Any other set gets its own list: its plugins can be
// collected once their contexts close, and a list kept for them could be
// handed to new plugins at the same addresses.
//
// The second result is the entry for the context's own plugins when the
// context is to free it with itself, and nil for the shared list.
func pluginLists(ctx context.Context, plugins []Plugin) ([]*wasm.SymList, *wasm.SymList, error) {
	defaults.mu.Lock()
	defer defaults.mu.Unlock()

	if defaults.plugins == nil || !slices.Equal(plugins, defaults.plugins) {
		return newPlugins(ctx, plugins...)
	}

	if defaults.lists == nil {
		lists, _, err := newPlugins(ctx, plugins...)
		if err != nil {
			return nil, nil, err
		}

		defaults.lists = lists
	}

	return defaults.lists, nil, nil
}

func newDefaultPlugins(ctx context.Context) ([]Plugin, error) {
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
