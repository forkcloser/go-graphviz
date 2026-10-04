# shellcheck shell=bash
# shellcheck disable=SC2034  # every value is read by build.sh, which sources this file
# The inputs of internal/wasm/graphviz.wasm, pinned by version and sha256.
# build.sh sources this file and refuses any download whose digest differs.
#
# Graphviz's version is also graphviz.version at the repository root (the
# README links it); build.sh checks the two agree. The digests are the ones
# the projects publish: Graphviz's generic package registry serves a .sha256
# next to each tarball, the GitHub releases of wasi-sdk, binaryen and expat
# carry per-asset digests.

GRAPHVIZ_VERSION=16.1.0
GRAPHVIZ_SHA256=beea483ab130f456c1c3905f4f2e40778a9c493c3d73ae8012367d552d71ca84

EXPAT_VERSION=2.8.5
EXPAT_SHA256=920dde485e15eda0cce8d2310b41d492c534e5e3d89ad407a0b4176dd2ff88fe

WASI_SDK_VERSION=34
WASI_SDK_SHA256_arm64_macos=9c59398106b417f8f14913380fdf0097a8cc0ff4af9eb3ce0065a859e88d49e9
WASI_SDK_SHA256_x86_64_macos=87d27fa8adc68dee59bfbf2e22a6d34ef717c34d6bf1d8af2a56fc929d9ce0eb
WASI_SDK_SHA256_arm64_linux=f7e243dff54d60bcc576e94d6166b69f410f2500ae4a9ceef34315be10e77971
WASI_SDK_SHA256_x86_64_linux=b761e3a0721dbae9c09a0059e5fdb2bf917d1b4a8a7b430fb3b5aafb0984b2c4

BINARYEN_VERSION=133
BINARYEN_SHA256_arm64_macos=ad66da82ac13f163e424b1643f16c6dfcccc98b5966296b43e52d3cab04f84a8
BINARYEN_SHA256_x86_64_macos=13a9b90be775c6389ce3d1f879cb8627bea56708ba8c122983941d53a8199b95
BINARYEN_SHA256_aarch64_linux=89c07ea56faf38d0fbecf36ca8ec0721756716185f265b568e133d427f299bf8
BINARYEN_SHA256_x86_64_linux=2dc9c7813f5375db93d96ead4b78222fcc3e2677bbb832297af4797782a37489
