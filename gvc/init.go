package gvc

import (
	"github.com/forkcloser/go-graphviz/internal/wasm"
)

func init() {
	// The engine slot holds the pointer the plugin registered; anything else
	// is a binding error, and 0 hands Graphviz a null engine, which fails the
	// job instead of calling through a bad pointer.
	enginePtr := func(job *wasm.Job, api wasm.API) uint64 {
		ptr, ok := job.GetGvc().GetApi()[api].GetTypeptr().GetEngine().(uint64)
		if !ok {
			return 0
		}

		return ptr
	}
	getRenderEnginePtr := func(job *wasm.Job) uint64 { return enginePtr(job, wasm.API_RENDER) }
	getLoadImageEnginePtr := func(job *wasm.Job) uint64 { return enginePtr(job, wasm.API_LOADIMAGE) }

	wasm.Register_RenderEngine_BeginJob(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_EndJob(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_BeginGraph(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_EndGraph(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_BeginLayer(
		func(job *wasm.Job, _ string, _, _ int) (uint64, error) { return getRenderEnginePtr(job), nil },
	)
	wasm.Register_RenderEngine_EndLayer(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_BeginPage(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_EndPage(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_BeginCluster(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_EndCluster(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_BeginNodes(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_EndNodes(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_BeginEdges(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_EndEdges(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_BeginNode(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_EndNode(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_BeginEdge(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_EndEdge(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_BeginAnchor(func(job *wasm.Job, _, _, _, _ string) (uint64, error) {
		return getRenderEnginePtr(job), nil
	})
	wasm.Register_RenderEngine_EndAnchor(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_BeginLabel(
		func(job *wasm.Job, _ wasm.LabelType) (uint64, error) { return getRenderEnginePtr(job), nil },
	)
	wasm.Register_RenderEngine_EndLabel(func(job *wasm.Job) (uint64, error) { return getRenderEnginePtr(job), nil })
	wasm.Register_RenderEngine_Textspan(func(job *wasm.Job, _ *wasm.PointFloat, _ *wasm.Textspan) (uint64, error) {
		return getRenderEnginePtr(job), nil
	})
	wasm.Register_RenderEngine_ResolveColor(
		func(job *wasm.Job, _ *wasm.Color) (uint64, error) { return getRenderEnginePtr(job), nil },
	)
	wasm.Register_RenderEngine_Ellipse(
		func(job *wasm.Job, _ []*wasm.PointFloat, _ int) (uint64, error) { return getRenderEnginePtr(job), nil },
	)
	wasm.Register_RenderEngine_Polygon(func(job *wasm.Job, _ []*wasm.PointFloat, _ uint32, _ int) (uint64, error) {
		return getRenderEnginePtr(job), nil
	})
	wasm.Register_RenderEngine_Beziercurve(func(job *wasm.Job, _ []*wasm.PointFloat, _ uint32, _ int) (uint64, error) {
		return getRenderEnginePtr(job), nil
	})
	wasm.Register_RenderEngine_Polyline(func(job *wasm.Job, _ []*wasm.PointFloat, _ uint32) (uint64, error) {
		return getRenderEnginePtr(job), nil
	})
	wasm.Register_RenderEngine_Comment(
		func(job *wasm.Job, _ string) (uint64, error) { return getRenderEnginePtr(job), nil },
	)
	wasm.Register_RenderEngine_LibraryShape(
		func(job *wasm.Job, _ string, _ []*wasm.PointFloat, _ uint32, _ int) (uint64, error) {
			return getRenderEnginePtr(job), nil
		},
	)

	wasm.Register_LoadImageEngine_LoadImage(
		func(job *wasm.Job, _ *wasm.UserShape, _ *wasm.BoxFloat, _ bool) (uint64, error) {
			return getLoadImageEnginePtr(job), nil
		},
	)
}
