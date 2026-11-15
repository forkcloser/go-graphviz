# This file is the project's own.
# Add recipes leveraging provided `do` ready-made recipes, or create your own.
# The import must be kept: it mounts every shared limen task under `just do ...`.
import '.limen/just/main.just'

# The FIRST recipe defined here becomes `just`'s default.
lint: do::lint::default do::lint::go::default do::lint::go::deadcode examples
fix: do::fix::go::default do::fix::default
test: oracle
    #!/usr/bin/env bash
    set -euo pipefail
    # With a native dot pinned for this platform, `oracle` has fetched it,
    # and a test that cannot find it fails where it would skip. limen fails
    # the same way for a pin that is not there and for pins it cannot read,
    # so the graphviz pin, which is always there, tells the two apart.
    limen pins get graphviz version > /dev/null
    if limen pins get "dot-$(go env GOHOSTOS)-$(go env GOHOSTARCH)" url > /dev/null 2>&1; then
      export GO_GRAPHVIZ_ORACLE=required
    fi
    just do test go unit
    just do test go race
security: do::security::default

# Graphviz, expat and the C bridge compiled to wasm32-wasip1 and shrunk, from
# the inputs pinned in pins.yaml; the result is internal/wasm/graphviz.wasm,
# byte-identical on every host (the ci workflow rebuilds and compares, with
# `just do lint converge`, which `lint` leaves out: it fetches a toolchain,
# pinned for three hosts). Set WORK to keep the downloads and the unpacked
# sources outside the tree.
[doc('Rebuild internal/wasm/graphviz.wasm from the pinned Graphviz, expat, wasi-sdk and binaryen')]
wasm:
    #!/usr/bin/env bash
    set -euo pipefail
    ./internal/wasm/build/build.sh

# Rewrite every file .converge declares, the wasm module, from the tree as it
# stands: what the checksum workflow runs on a Renovate branch that moved one
# of the module's pins, and commits.
[doc('Rebuild the files derived from pins.yaml (.converge)')]
converge: wasm

# The oracle of the tests that compare the module with Graphviz itself: the
# native dot forkcloser/testbin builds, unpatched, from the Graphviz release
# the module is built from, pinned per platform in pins.yaml (dot-<os>-<arch>)
# and verified against its digest. It lands in build/oracle/graphviz-<os>-<arch>,
# where the tests look for it. A platform with no pin gets nothing, and the
# tests fall back to a dot on PATH or skip.
[doc('Fetch the pinned native dot the tests compare the module with (build/oracle)')]
oracle:
    #!/usr/bin/env bash
    set -euo pipefail
    host="$(go env GOHOSTOS)-$(go env GOHOSTARCH)"
    # limen fails the same way for a pin that is not there and for pins it
    # cannot read: the graphviz pin, which is always there, tells the two
    # apart, and pins that cannot be read stop the recipe.
    limen pins get graphviz version > /dev/null
    if ! url="$(limen pins get "dot-${host}" url 2> /dev/null)"; then
      echo "no native dot is pinned for ${host}" >&2
      exit 0
    fi
    sum="$(limen pins get "dot-${host}" sha256)"
    version="$(limen pins get "dot-${host}" version)"
    dl="build/oracle/dl/graphviz-${host}-${version}.tar.gz"
    mkdir -p build/oracle/dl
    if [ ! -f "${dl}" ]; then
      curl --proto '=https' --tlsv1.2 -fsSL --retry 5 --retry-delay 3 -o "${dl}.part" "${url}"
      mv "${dl}.part" "${dl}"
    fi
    echo "${sum}  ${dl}" | sha256sum -c - > /dev/null || {
      echo "dot-${host}: sha256 mismatch against pins.yaml" >&2
      rm -f "${dl}"
      exit 1
    }
    # Unpacked anew each time: the directory's name carries no version.
    rm -rf "build/oracle/graphviz-${host}"
    tar -xzf "${dl}" -C build/oracle

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
# Takes the path of Graphviz's own dot binary — never a bare name: the
# hermetic PATH holds no Graphviz, and forkcloser/dot, a Go-built tool of
# this repository, is a different program with the same name. Side-by-side
# images (ours, Graphviz's, their difference) land in build/smoke.
[doc('Compare PNG output with a Graphviz dot binary at the given path (needs Graphviz installed)')]
smoke graphviz:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p build/smoke
    GO_GRAPHVIZ_SMOKE=1 GO_GRAPHVIZ_DOT="{{ graphviz }}" GO_GRAPHVIZ_SMOKE_DIR="${PWD}/build/smoke" \
        go test -count=1 -run '^TestDotSmoke$' -v .
