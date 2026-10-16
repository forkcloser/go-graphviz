# Upstream

Fork of https://github.com/goccy/go-graphviz (`master`).

- Forked from: [`76e0497`](https://github.com/goccy/go-graphviz/commit/76e04975df88d41930377420c9a3170ef0031379), committed 2025-11-29
- Reviewed through: [`76e0497`](https://github.com/goccy/go-graphviz/commit/76e04975df88d41930377420c9a3170ef0031379), reviewed 2026-10-07

## Incorporated

| Upstream commit | Here | What |
| --- | --- | --- |

## Not incorporated

| Upstream commit | Why |
| --- | --- |

## Changes upstream does not have

| Change | Here |
| --- | --- |
| The fork ships no `dot` command; the command is [forkcloser/dot](https://github.com/forkcloser/dot) | https://github.com/forkcloser/go-graphviz/pull/2 |
| Graphviz 16.1.0 embedded, built reproducibly from a pinned tarball; `disintegration/imaging` replaced by `x/image/draw` | https://github.com/forkcloser/go-graphviz/pull/3 |
| Fonts are parsed by `x/image`'s opentype, not the archived freetype | https://github.com/forkcloser/go-graphviz/pull/22 |
| A label's first span is zeroed before Graphviz fills it: an empty first line no longer frees a garbage pointer | https://github.com/forkcloser/go-graphviz/pull/30 |
| A callback's error reaches the caller instead of a panic; Graphviz's own result codes and messages are reported | https://github.com/forkcloser/go-graphviz/pull/31, https://github.com/forkcloser/go-graphviz/pull/32 |
| Every call into the module is serialized, and a render holds the module from layout to free, so goroutines can share an instance | https://github.com/forkcloser/go-graphviz/pull/34, https://github.com/forkcloser/go-graphviz/pull/47 |
| What a call allocates on either side is freed; module memory no longer leaks per render and instance; a context's plugin list is freed with the context | https://github.com/forkcloser/go-graphviz/pull/35, https://github.com/forkcloser/go-graphviz/pull/62, https://github.com/forkcloser/go-graphviz/pull/87 |
| Filled shapes are drawn with their pen, and alpha is carried | https://github.com/forkcloser/go-graphviz/pull/36 |
| `Graph` options apply to the graph they open, not the instance; empty labels are not overwritten after a parse | https://github.com/forkcloser/go-graphviz/pull/40, https://github.com/forkcloser/go-graphviz/pull/41 |
| A failed module load is an error, not a panic; a getter's result is read in full | https://github.com/forkcloser/go-graphviz/pull/44, https://github.com/forkcloser/go-graphviz/pull/45 |
| `RenderImage` returns the drawn page without a PNG round trip | https://github.com/forkcloser/go-graphviz/pull/47 |
| Text is drawn on Graphviz's baseline, and drawn at all when the font is a file or the fallback | https://github.com/forkcloser/go-graphviz/pull/50 |
| Callback arguments are read as 32 bits where Graphviz passes 32 | https://github.com/forkcloser/go-graphviz/pull/53 |
| Landscape pages are drawn, and pen widths scale with the page | https://github.com/forkcloser/go-graphviz/pull/54 |
| Fonts are resolved as Graphviz's text layout does, with glyph fallback, and labels are measured with the fonts they are drawn in | https://github.com/forkcloser/go-graphviz/pull/55, https://github.com/forkcloser/go-graphviz/pull/56 |
| Node images are drawn in the box Graphviz gives them, in every format, by absolute path too; a WebP is sized as it is; a node image is read no further than its pixel budget; a raster page over 256 megapixels is refused | https://github.com/forkcloser/go-graphviz/pull/57, https://github.com/forkcloser/go-graphviz/pull/68, https://github.com/forkcloser/go-graphviz/pull/74, https://github.com/forkcloser/go-graphviz/pull/60, https://github.com/forkcloser/go-graphviz/pull/61 |
| A callback panic is recovered instead of breaking the instance | https://github.com/forkcloser/go-graphviz/pull/65 |
| API trimmed (breaking): the cdt and cgraph bindings over Graphviz internals and gvc's setters of what a renderer receives are gone; `XDOT` is `"xdot"` and plain DOT output is `GV`; plugin options carry their `With` prefix | https://github.com/forkcloser/go-graphviz/pull/77, https://github.com/forkcloser/go-graphviz/pull/78 |
| PNG and JPEG are drawn with an in-tree canvas, Cairo's joins, caps and dash lengths; gg and freetype leave the module | https://github.com/forkcloser/go-graphviz/pull/81 |
| A typed setter keeps its first error, which `Layout` returns; an allocation the module cannot make fails instead of writing to address 0 | https://github.com/forkcloser/go-graphviz/pull/82 |
| A `FontLoader` supplies the font the layout measures with (breaking) | https://github.com/forkcloser/go-graphviz/pull/84 |
| The binding's and the generator's errors wrap sentinels | https://github.com/forkcloser/go-graphviz/pull/89 |

## Graphviz source edits

`internal/wasm/graphviz.wasm` is Graphviz 16.1.0, built from its release tarball with these edits, which `internal/wasm/build/build.sh` applies and checks.

| File | Edit | Why | Reported to Graphviz |
| --- | --- | --- | --- |
| `lib/common/labels.c` | `storeline` passes the old span count to `gv_recalloc` | the span about to be filled was never zeroed, and a label whose first line is empty freed a garbage layout pointer ([#30](https://github.com/forkcloser/go-graphviz/pull/30)) | no |
| `lib/gvc/gvusershape.c` | `gvusershape_file_release` always closes the image file | Graphviz kept up to 50 image files open for the life of the process, which pins them on Windows ([#62](https://github.com/forkcloser/go-graphviz/pull/62)) | no, a choice for this embedding |
| `lib/gvc/gvusershape.c` | `webp_size` reads each WebP layout's canvas size where the format keeps it (`webp-size.patch`) | a lossless (VP8L) or extended (VP8X) WebP was sized tens of thousands to hundreds of millions of points a side; `dot` 16.1.0 does the same | no |
