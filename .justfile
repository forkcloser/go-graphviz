# This file is the project's own.
# Add recipes leveraging provided `do` ready-made recipes, or create your own.
# The import must be kept: it mounts every shared limen task under `just do ...`.
import '.limen/just/main.just'

# The FIRST recipe defined here becomes `just`'s default.
lint: do::lint::default do::lint::go::default do::lint::go::deadcode examples
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

# The programs under _examples sit outside ./..., which skips directories
# starting with an underscore, so nothing else compiles them; each is its
# own main package, vetted here so the examples cannot stop compiling
# unnoticed again.
[doc('Vet the example programs under _examples')]
examples:
    #!/usr/bin/env bash
    set -euo pipefail
    go vet ./_examples/simple ./_examples/rw

# Compares this library's PNG output with a Graphviz installed on this
# machine, over the test corpus: unhermetic, so never part of lint or test.
# Pass the dot binary when it is not the dot on PATH. Side-by-side images
# (ours, dot's, their difference) land in build/smoke.
[doc('Compare PNG output with a local Graphviz dot (needs Graphviz installed)')]
smoke dot="dot":
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p build/smoke
    GO_GRAPHVIZ_SMOKE=1 GO_GRAPHVIZ_DOT="{{ dot }}" GO_GRAPHVIZ_SMOKE_DIR="${PWD}/build/smoke" \
        go test -count=1 -run '^TestDotSmoke$' -v .
