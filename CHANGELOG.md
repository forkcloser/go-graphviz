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

### Fixed

- The bridge no longer reads WebAssembly address 0 for a NULL-terminated
  array that is itself NULL, as Graphviz passes a text layout's font path:
  every label laid out read there, harmless only while that word was zero.
- A Go callback that panics during a render, a `RenderEngine` method for
  one, no longer leaves the instance broken: the panic comes back as an
  error wrapping `ErrCallbackPanic`, with the panic's value, and the
  instance keeps rendering. The next render on it used to fail.
- Rendering and making instances no longer grow the WebAssembly module's
  memory. A render left about 4.5 KiB behind, in PNG or JPEG as in SVG,
  and `New` with `Close` about 4 KiB, which a long-running process
  accumulated without bound: the output Graphviz allocates for the caller
  was never freed, the PNG and JPEG device replaced Graphviz's output
  buffer instead of writing into it, every read of a struct-valued field
  allocated a copy, and every instance built its own plugins.
- Graphviz closes a node image's file once it has sized it. It kept up to
  50 open for the life of the process, which on Windows pins the files.
- `NewWithPlugins` with no plugins handed Graphviz an unterminated plugin
  list, and could fail with an out-of-bounds memory access.
- A lossless or extended WebP node image, any WebP with alpha, is sized as
  it is. Graphviz 16.1.0 read its size from the wrong bytes, tens of
  thousands to hundreds of millions of points a side, and the render then
  failed with `ErrPageTooLarge`.

### Changed

- A render allocates on the order of its page: the bindings keep each
  function they call in the WebAssembly module instead of having a call
  engine built for every call. A PNG render of a graph of about 40 nodes
  went from 320 MB allocated and 30 ms to 7 MB and 13 ms.
- `DefaultPlugins` builds its plugins once and returns the same ones to
  every caller; they stay registered for the life of the process.
- A struct-valued field read through the bindings, such as `Job.Scale`,
  `ObjectState.PenColor` or `BoxFloat.LL`, is the field itself rather than
  a copy: setting through it changes the struct, and it is valid as long
  as the struct is, which for what a callback receives is the callback.
- `graphviz.version` is gone: the Graphviz version is the `graphviz` entry
  in `pins.yaml`, which the build already read and checked the file against.

## [0.4.0] - 2026-10-06

### Changed

- Text is measured for the layout with the fonts it is drawn in, as
  Graphviz's own Pango-based tools measure it, instead of Graphviz's
  built-in width estimates for Times, Courier and Arial. Labels fit their
  nodes in every output. The layout now follows the fonts installed on the
  machine: the same graph can come out with slightly different
  coordinates, in SVG, DOT and every other format, on two machines with
  different fonts, as it does with `dot`. Text whose font a `FontLoader`
  supplies is still estimated, since a loader answers for a render job and
  layout has none.
- Font names resolve the way Graphviz's text layout resolves them. A
  PostScript name such as `Times-Roman` or `Helvetica-Narrow-BoldOblique`
  uses Graphviz's family, weight, width and slant for it. Any other name
  is read as a family with style words (`DejaVu Sans Bold`,
  `Arial:italic`), or a list of them ending, optionally, in a generic
  family. The family and its metric-compatible substitutes (Arial for
  Helvetica, Liberation and TeX Gyre faces, and others) are looked up
  among the installed fonts by their names, not their file names, and the
  closest face is taken. Where nothing installed answers, the embedded Go
  fonts stand in, now in bold, italic and monospace cuts, which adds about
  1.2 MB to binaries.
- Pen widths and dashes scale with the page, as they do in Graphviz's own
  renderers: lines are thinner on a graph reduced by `size` and wider at a
  higher `dpi`. At the default 96 dpi a line of pen width 1 is 1.33 pixels
  wide, a third wider than before.
- A node image is stretched to the box Graphviz computes from
  `imagescale` and `imagepos`, at the page's scale. It used to be drawn
  at its own pixel size, or, scaled, at its size in points and offset by
  a padding of the renderer's own.

### Added

- `Job.Rotation`, for render engines that transform coordinates
  themselves, and `ErrRotation` for a page turned by another angle than 0
  or 90 degrees.
- `TextLayoutPlugin` and `NewTextLayoutPlugin`, which `DefaultPlugins`
  installs; a context built with `NewWithPlugins` measures text only when
  it is among the plugins.
- Node images in JPEG, GIF, BMP and WebP, and node images in JPEG output.
- `ErrImageTooLarge` and `MaxImagePixels`: a node image whose header
  declares, or whose drawn size reaches, more than 64 megapixels fails the
  render before it is decoded, and an image file is read no further than
  such an image can take.
- `MaxPagePixels`: a PNG or JPEG page over 256 megapixels, 16384 by 16384,
  fails with `ErrPageTooLarge` before its canvas is allocated; a page's
  size and resolution come from the graph.

### Fixed

- A graph laid out with `rotate=90` or `landscape=true` renders in PNG and
  JPEG. The page's rotation was ignored and the page came out blank.
- A character the label's font has no glyph for is drawn from an installed
  font that has one: Japanese, Chinese and Korean in a Latin font, for
  one, where a box was drawn.
- Text in a symbol-encoded font, which keeps its glyphs in the private
  use area as Symbol and Zapf Dingbats do on Windows, is drawn from there
  instead of as boxes. This is untested on a real symbol-encoded font.
- Labels no longer overflow their nodes in PNG and JPEG output: bold and
  wide faces were drawn wider than the box Graphviz had estimated for them.
- A node image shown by many nodes is read and decoded once per page, not
  once per node.

## [0.3.1] - 2026-10-05

### Fixed

- PNG and JPEG output on amd64 draws edges as lines. The flag Graphviz
  passes to say whether a shape is filled was read with bits it does not
  set, which on amd64 marked every curve and outlined ellipse as filled:
  edges came out as solid shapes over the nodes they join. A custom
  render engine received the same wrong flags.
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
