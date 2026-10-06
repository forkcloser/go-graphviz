# Changelog

All notable changes to this fork are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/). Changes are described against the
fork point, upstream [`goccy/go-graphviz`](https://github.com/goccy/go-graphviz)
v0.2.10 (commit `76e0497`), which is also upstream's current `master`; see
[UPSTREAM.md](UPSTREAM.md). For a user of `github.com/goccy/go-graphviz`,
switching is a change of import path, Go 1.26, and the label change under
Graphviz 16 below.

## [Unreleased]

## [0.3.1] - 2026-10-05

### Fixed

- PNG and JPEG output draws text whose font comes from a TrueType file or
  falls back to the embedded Go Regular. Since 0.3.0 such text was not
  drawn at all, which left labels blank on Linux and Windows, where Times
  is not installed.
- Text in PNG and JPEG output sits on the baseline Graphviz's own
  renderers use. Since 0.3.0 every line was drawn one font size too high,
  so the first line of a node's label crossed the top of its box.
- Parsing no longer overwrites a node's explicit empty label with `\N`:
  an image node drawn without text keeps no text, and `Node.Label()` on
  it returns `""`.
- The options passed to `Graph` apply to the graph it opens only; a name
  or graph type given for one graph no longer sticks to the instance for
  every later `Graph` call.
- A WebAssembly module that cannot be loaded no longer panics at import,
  taking the program down before `main` runs: the first call into the
  library returns the error.
- One graph rendered from several goroutines at once renders correctly:
  `Render`, `RenderImage` and `RenderFilename` hold the module from the
  layout to the end of the render. Concurrent renders of the same graph
  used to fail with "Layout was not done".
- Fields of Graphviz's structures that are 64 bits wide read back whole;
  a value above 2^32, such as an object tag's id, came back truncated to
  its low 32 bits.

### Changed

- `RenderImage` returns the page the raster renderer drew instead of
  encoding it to PNG and decoding it back, about 30% faster on a
  mid-sized graph. The pixels are the same; the concrete type is the
  renderer's `*image.RGBA`, where a decoded PNG with transparency was an
  `*image.NRGBA`.
- The compiled-code cache moves from a `go-graphviz` directory under the
  shared temporary directory, created readable by everyone, to
  `go-graphviz/wazero` under the user's cache directory, readable by that
  user alone. The old directory is left in place and can be removed.

### Removed

- `github.com/corona10/goimagehash` and `github.com/nfnt/resize` from the
  module's requirements. Only the tests used them, but they reached every
  dependent module's graph.

## [0.3.0] - 2026-10-05

### Changed

- The module is `github.com/forkcloser/go-graphviz`.
- Go 1.26 is the minimum (upstream: 1.23).
- Graphviz 16.1.0 (upstream: 12.1.2) and expat 2.8.5, compiled to
  `wasm32-wasip1` by a pinned wasi-sdk 34 and shrunk by binaryen 133, every
  input checked against a digest in `pins.yaml`; the build needs no container
  and is reproducible, and CI rebuilds the committed blob on Linux and macOS
  and compares the bytes.
- Since Graphviz 13, a value set through `SetLabel` or `SafeSet` is plain
  text: markup given there is printed escaped. An HTML-like label needs the
  new `SetLabelHTML` or `SafeSetHTML`.
- Calls into the module are serialized process-wide: instances and graphs
  may be used from any goroutine, and a call waits for the one in progress.
  A render callback may use the API on its own goroutine; the context and
  handles it receives must not be handed to another goroutine, and the
  points, spans, boxes and colours it receives are valid only until it
  returns.
- A failed Graphviz call always returns an error wrapping `ErrGraphviz`
  (`ErrDict` for libcdt) with Graphviz's own message, or its result code
  when it reported none; errors from the Go side are wrapped with what was
  being done.
- `Context.Close` returns only its own error; it used to return Graphviz's
  process-wide error count, so every `Close` after any failed parse failed.
- A plugin's callbacks are released when the last context using the plugin
  closes.
- Fonts are parsed by `golang.org/x/image/font/opentype` and found in the
  platform's font directories in pure Go; node images are resampled by
  `golang.org/x/image/draw` with a Lanczos kernel. `disintegration/imaging`,
  `flopp/go-findfont` (and its `replace` directive) and the direct
  dependency on the archived `golang/freetype` are gone.
- wazero 1.12.0 (upstream: 1.10.1), `golang.org/x/image` 0.46.0.
- `cdt.Link.SetHash` takes a `uint32`, `cgraph.Symbol.SetID` an `int32`, and
  `Symbol.SetKind`, `SetFixed` and `SetPrint` a `uint32`: the widths of the C
  fields they write.

### Added

- `SetLabelHTML` on `Graph`, `Node` and `Edge`; `SafeSetHTML` on `Graph`,
  `Node` and `Edge`; `Graph.StrBindHTML`, `StrBindText`, `StrFreeHTML` and
  `StrdupText`, Graphviz 13's text and HTML string functions.
- `graphviz.SetWarningWriter`: Graphviz's warnings go to the writer named,
  as the lines Graphviz prints. They were discarded, and still are by
  default.
- Sentinel errors `cgraph.ErrGraphviz`, `cdt.ErrDict`,
  `gvc.ErrFontNotFound`, `gvc.ErrNoContext` and `gvc.ErrPageTooLarge`.

### Fixed

- A label whose first line is empty no longer corrupts the heap. Graphviz
  16.1.0's `storeline` left the first span of a label unzeroed, and freeing
  the layout called through a garbage pointer; the build carries the
  one-line fix until upstream releases one.
- An error returned by a render or image callback reaches the caller. It was
  turned into a panic that surfaced as an unrelated memory read error and
  left the instance unable to render again.
- `RenderData` checks Graphviz's result: an unknown format returns Graphviz's
  message instead of a read of memory a failed render never wrote.
- `RenderFilename` writes the file. It wrote nothing for any format and
  returned nil.
- Two instances used from two goroutines no longer corrupt each other.
- Memory a call allocates is freed: the strings, slots and arrays passed in,
  the headers of strings and slices handed back, and the copies a callback
  receives. Each attribute read or write, parse and instance used to leak.
- PNG and JPEG output draw a filled shape's outline in its pen colour, width
  and style; colours carry their alpha; a shape whose pen is none is not
  outlined.
- The image renderer closes the node image file it opens.
- A font name with several suffixes (`Helvetica-Bold-Oblique`) falls back
  one suffix at a time; the embedded fallback face is parsed once.
- A page too large for the raster renderer returns `ErrPageTooLarge` instead
  of overflowing.

### Removed

- The `dot` command (`cmd/dot`). forkcloser ships its own,
  [github.com/forkcloser/dot](https://github.com/forkcloser/dot).
- `cdt.Hold` and `cdt.Data`, and their root aliases `DictHold` and
  `DictData`: nothing in the module produced or took them.
