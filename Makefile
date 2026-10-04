export

GOBIN := $(PWD)/bin
PATH := $(GOBIN):internal/tools/nori/bin:$(PATH)

.PHONY: tools
tools: nori
	go install github.com/bufbuild/buf/cmd/buf@v1.32.2

fmt/buf:
	buf format --write

# Graphviz, expat and the C bridge, compiled to wasm32-wasip1 with the pinned
# toolchain in internal/wasm/build/pins.sh; no container involved.
.PHONY: generate/wasm
generate/wasm:
	./internal/wasm/build/build.sh

.PHONY: generate/buf
generate/buf:
	$(GOBIN)/buf generate
	mv bind.c internal/wasm/build
	mv bind.go internal/wasm/

.PHONY: nori
nori:
	make build -C ./internal/tools/nori
