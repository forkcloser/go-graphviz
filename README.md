# go-graphviz [![ci](https://github.com/forkcloser/go-graphviz/actions/workflows/ci.yaml/badge.svg)](https://github.com/forkcloser/go-graphviz/actions/workflows/ci.yaml)

Go bindings for Graphviz

## About this fork

A fork of [github.com/goccy/go-graphviz](https://github.com/goccy/go-graphviz),
maintained by forkcloser. The library is the same: Graphviz compiled to
WebAssembly, run on wazero, no C toolchain and no system Graphviz. The
upstream `dot` command is not part of this module; forkcloser ships its own,
[github.com/forkcloser/dot](https://github.com/forkcloser/dot), as a static
binary with every layout and format of the library.

<img src="https://user-images.githubusercontent.com/209884/90976476-64e84000-e578-11ea-9596-fb4a7d3b11a6.png" width="400px"></img>

# Features

The embedded Graphviz is the version of the `graphviz` entry in [pins.yaml](./pins.yaml).

- Pure Go Library
- No need to install Graphviz library ( ~`brew install graphviz`~ or ~`apt-get install graphviz`~ )
  - The Graphviz library has been converted to WebAssembly (WASM) and embedded it, so it works consistently across all environments
- Supports encoding/decoding for DOT language
- Supports custom renderer for custom format
- Supports setting graph properties in a type-safe manner

## Supported Layout

`circo` `dot` `fdp` `neato` `nop` `nop1` `nop2` `osage` `patchwork` `sfdp` `twopi`

## Supported Format

`dot` `svg` `png` `jpg`

The above are the formats supported by default. You can also add custom formats.

# Installation

```bash
$ go get github.com/forkcloser/go-graphviz
```

# Synopsis

## 1. Write DOT Graph in Go

```go
package main

import (
  "bytes"
  "context"
  "fmt"
  "log"

  "github.com/forkcloser/go-graphviz"
)

func main() {
  ctx := context.Background()
  g, err := graphviz.New(ctx)
  if err != nil { panic(err )}

  graph, err := g.Graph()
  if err != nil { panic(err) }
  defer func() {
    if err := graph.Close(); err != nil { panic(err) }
    g.Close()
  }()
  n, err := graph.CreateNodeByName("n")
  if err != nil { panic(err) }

  m, err := graph.CreateNodeByName("m")
  if err != nil { panic(err) }

  e, err := graph.CreateEdgeByName("e", n, m)
  if err != nil { panic(err) }
  e.SetLabel("e")

  var buf bytes.Buffer
  if err := g.Render(ctx, graph, "dot", &buf); err != nil {
    log.Fatal(err)
  }
  fmt.Println(buf.String())
}
```

## 2. Parse DOT Graph

```go
path := "/path/to/dot.gv"
b, err := os.ReadFile(path)
if err != nil { panic(err) }
graph, err := graphviz.ParseBytes(b)
```

## 3. Render Graph

```go
ctx := context.Background()
g, err := graphviz.New(ctx)
if err != nil { panic(err) }

graph, err := g.Graph()
if err != nil { panic(err) }

// create your graph

// 1. write encoded PNG data to buffer
var buf bytes.Buffer
if err := g.Render(ctx, graph, graphviz.PNG, &buf); err != nil { panic(err) }

// 2. get as image.Image instance
image, err := g.RenderImage(ctx, graph)
if err != nil { panic(err) }

// 3. write to file directly
if err := g.RenderFilename(ctx, graph, graphviz.PNG, "/path/to/graph.png"); err != nil { panic(err) }
```

# Development

Every task runs through [`just`](https://just.systems): `just lint`, `just test`,
`just fix`, `just wasm`, `just bindings`; `just --list` shows the rest. Tools are
pinned by [aqua](https://aquaproj.github.io/) and the shared conventions come
from [limen](https://github.com/farcloser/limen) (see `AGENTS.md`).

# How it works

1. Generates bindings between Go and C from [Protocol Buffers file](./internal/wasm/bind.proto) with `just bindings`.
2. Builds `graphviz.wasm` with `just wasm` ([internal/wasm/build/build.sh](./internal/wasm/build/build.sh)):
   Graphviz, expat and the C bridge compiled to `wasm32-wasip1` by a pinned
   wasi-sdk and shrunk by a pinned binaryen, every input fetched by version and
   checked against the digests in [pins.yaml](./pins.yaml). No container and no
   `configure` run: the build is reproducible, and CI rebuilds the committed
   blob on Linux and macOS and compares the bytes.
3. Uses Graphviz functionality from a sub-packages ( `cdt` `cgraph` `gvc` ) via the `internal/wasm` package.
4. `graphviz` package provides facade interface for all sub packages.

# License

MIT

This library embeds and uses `graphviz.wasm`, which is generated based on the original source code of Graphviz. Therefore, the `graphviz.wasm` follows [the license adopted by Graphviz](https://graphviz.org/license) ( Eclipse Public License ).
