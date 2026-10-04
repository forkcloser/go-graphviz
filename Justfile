# This file is the project's own.
# Add recipes leveraging provided `do` ready-made recipes, or create your own.
# The import must be kept: it mounts every shared limen task under `just do ...`.
import '.limen/just/main.just'

# The FIRST recipe defined here becomes `just`'s default.
lint: do::lint::default do::lint::go::default do::lint::go::deadcode
fix: do::fix::go::default do::fix::default
test: do::test::go::unit do::test::go::race
security: do::security::default

# Graphviz, expat and the C bridge compiled to wasm32-wasip1 and shrunk, from
# the inputs pinned in pins.yaml; the result is internal/wasm/graphviz.wasm,
# byte-identical on every host (the ci workflow rebuilds and compares). Set
# WORK to keep the downloads and the unpacked sources outside the tree.
[doc('Rebuild internal/wasm/graphviz.wasm from the pinned Graphviz, expat, wasi-sdk and binaryen')]
wasm:
    #!/usr/bin/env bash
    set -euo pipefail
    ./internal/wasm/build/build.sh

# The Go and C halves of the bridge, from internal/wasm/bind.proto: buf runs
# nori, this repository's own protoc plugin, built here from source so the
# generator is the one the tree carries. Rebuild the wasm after a change to
# the proto, since bind.c is part of it.
[doc('Regenerate internal/wasm/bind.go and internal/wasm/build/bind.c from bind.proto')]
bindings:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p build/bin
    (cd internal/tools/nori && go build -o ../../../build/bin/protoc-gen-nori ./cmd/protoc-gen-nori)
    PATH="${PWD}/build/bin:${PATH}" BUF_CACHE_DIR="${PWD}/build/cache/buf" buf generate
    mv bind.c internal/wasm/build/bind.c
    mv bind.go internal/wasm/bind.go
    gofmt -w internal/wasm/bind.go
