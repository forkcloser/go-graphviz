# Upstream

Fork of https://github.com/goccy/go-graphviz (`master`).

- Forked from: [`76e0497`](https://github.com/goccy/go-graphviz/commit/76e04975df88d41930377420c9a3170ef0031379), committed 2025-11-29
- Reviewed through: [`76e0497`](https://github.com/goccy/go-graphviz/commit/76e04975df88d41930377420c9a3170ef0031379), reviewed 2026-10-05

## Incorporated

| Upstream commit | Here | What |
| --- | --- | --- |

## Not incorporated

| Upstream commit | Why |
| --- | --- |

## Graphviz source edits

`internal/wasm/graphviz.wasm` is Graphviz 16.1.0, built from its release tarball with these edits, which `internal/wasm/build/build.sh` applies and checks.

| File | Edit | Why | Reported to Graphviz |
| --- | --- | --- | --- |
| `lib/common/labels.c` | `storeline` passes the old span count to `gv_recalloc` | the span about to be filled was never zeroed, and a label whose first line is empty freed a garbage layout pointer ([#30](https://github.com/forkcloser/go-graphviz/pull/30)) | no |
| `lib/gvc/gvusershape.c` | `gvusershape_file_release` always closes the image file | Graphviz kept up to 50 image files open for the life of the process, which pins them on Windows ([#62](https://github.com/forkcloser/go-graphviz/pull/62)) | no, a choice for this embedding |
| `lib/gvc/gvusershape.c` | `webp_size` reads each WebP layout's canvas size where the format keeps it (`webp-size.patch`) | a lossless (VP8L) or extended (VP8X) WebP was sized tens of thousands to hundreds of millions of points a side; `dot` 16.1.0 does the same | no |
