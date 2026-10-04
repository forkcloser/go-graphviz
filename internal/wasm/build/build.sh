#!/usr/bin/env bash
# Build internal/wasm/graphviz.wasm: Graphviz, expat and this module's C
# bridge compiled to wasm32-wasip1 with a pinned wasi-sdk, then shrunk with a
# pinned binaryen.
#
# Every input is fetched by version and verified against the sha256 recorded
# in pins.sh before it is unpacked. No container, no system compiler and no
# configure run: the two configuration headers next to this script stand in
# for configure's output, so the result does not depend on the host that
# built it. Two runs on one machine yield the same bytes, and the CI check
# rebuilds the committed blob and compares.
#
#   internal/wasm/build/build.sh            # writes internal/wasm/graphviz.wasm
#   WORK=/some/dir internal/wasm/build/build.sh   # keep downloads elsewhere
#
# The version of Graphviz comes from graphviz.version at the repository root;
# its sha256, and every other pin, from pins.sh.
#
# Graphviz's asserts embed __FILE__, so the paths of the unpacked sources and
# of this directory are mapped to fixed names, and the shell globs are
# expanded under the C locale; without either, two builders produce
# different bytes.
set -euo pipefail

# Glob expansion sorts by the current collation, so the order of the source
# files handed to clang, and with it the link order and the bytes of the
# output, would follow the builder's locale. The C locale makes it the same
# everywhere.
export LC_ALL=C

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"
work="${WORK:-${here}/work}"
out="${root}/internal/wasm/graphviz.wasm"

# shellcheck source-path=SCRIPTDIR
. "${here}/pins.sh"

graphviz_version="$(tr -d '[:space:]' < "${root}/graphviz.version")"
[ "${graphviz_version}" = "${GRAPHVIZ_VERSION}" ] || {
  echo "graphviz.version says ${graphviz_version}, pins.sh says ${GRAPHVIZ_VERSION}: update both" >&2
  exit 1
}

case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) sdk_os=macos; sdk_arch=arm64; binaryen_arch=arm64-macos ;;
  Darwin-x86_64) sdk_os=macos; sdk_arch=x86_64; binaryen_arch=x86_64-macos ;;
  Linux-aarch64) sdk_os=linux; sdk_arch=arm64; binaryen_arch=aarch64-linux ;;
  Linux-x86_64) sdk_os=linux; sdk_arch=x86_64; binaryen_arch=x86_64-linux ;;
  *) echo "unsupported host $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

# sha256 [-c -]: the host's checksum tool under one name; coreutils calls it
# sha256sum, macOS ships it as shasum -a 256.
sha256() {
  if command -v sha256sum > /dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

# fetch <name> <url> <sha256>: download once into the work dir, verify always.
fetch() {
  local name="$1" url="$2" sum="$3"
  local file="${work}/dl/${name}"
  mkdir -p "${work}/dl"
  if [ ! -f "${file}" ]; then
    echo "fetching ${name}"
    curl --proto '=https' --tlsv1.2 -fsSL --retry 5 --retry-delay 3 -o "${file}.part" "${url}"
    mv "${file}.part" "${file}"
  fi
  echo "${sum}  ${file}" | sha256 -c - > /dev/null || {
    echo "${name}: sha256 mismatch against pins.sh" >&2
    rm -f "${file}"
    exit 1
  }
}

# unpack <archive-name> <dir>: extract once; a present dir is a finished one.
unpack() {
  local file="${work}/dl/$1" dir="${work}/src/$2"
  [ -d "${dir}" ] && return
  mkdir -p "${work}/src"
  tar -xzf "${file}" -C "${work}/src"
  [ -d "${dir}" ] || { echo "$1 did not unpack to $2" >&2; exit 1; }
}

sdk_sum_var="WASI_SDK_SHA256_${sdk_arch}_${sdk_os}"
binaryen_sum_var="BINARYEN_SHA256_${binaryen_arch//-/_}"
sdk_tar="wasi-sdk-${WASI_SDK_VERSION}.0-${sdk_arch}-${sdk_os}.tar.gz"
binaryen_tar="binaryen-version_${BINARYEN_VERSION}-${binaryen_arch}.tar.gz"

fetch "${sdk_tar}" \
  "https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-${WASI_SDK_VERSION}/${sdk_tar}" \
  "${!sdk_sum_var}"
fetch "${binaryen_tar}" \
  "https://github.com/WebAssembly/binaryen/releases/download/version_${BINARYEN_VERSION}/${binaryen_tar}" \
  "${!binaryen_sum_var}"
fetch "expat-${EXPAT_VERSION}.tar.gz" \
  "https://github.com/libexpat/libexpat/releases/download/R_${EXPAT_VERSION//./_}/expat-${EXPAT_VERSION}.tar.gz" \
  "${EXPAT_SHA256}"
fetch "graphviz-${GRAPHVIZ_VERSION}.tar.gz" \
  "https://gitlab.com/api/v4/projects/4207231/packages/generic/graphviz-releases/${GRAPHVIZ_VERSION}/graphviz-${GRAPHVIZ_VERSION}.tar.gz" \
  "${GRAPHVIZ_SHA256}"

unpack "${sdk_tar}" "wasi-sdk-${WASI_SDK_VERSION}.0-${sdk_arch}-${sdk_os}"
unpack "${binaryen_tar}" "binaryen-version_${BINARYEN_VERSION}"
unpack "expat-${EXPAT_VERSION}.tar.gz" "expat-${EXPAT_VERSION}"
unpack "graphviz-${GRAPHVIZ_VERSION}.tar.gz" "graphviz-${GRAPHVIZ_VERSION}"

sdk="${work}/src/wasi-sdk-${WASI_SDK_VERSION}.0-${sdk_arch}-${sdk_os}"
wasm_opt="${work}/src/binaryen-version_${BINARYEN_VERSION}/bin/wasm-opt"
gv="${work}/src/graphviz-${GRAPHVIZ_VERSION}"
expat="${work}/src/expat-${EXPAT_VERSION}"

# The sources are built as they ship, with three exceptions, each applied to
# the unpacked copy: configure's two outputs are replaced by the checked-in
# headers, and the one file under lib/ that defines main() is dropped.
cp "${here}/config.h" "${gv}/config.h"
cp "${here}/expat_config.h" "${expat}/expat_config.h"
rm -f "${gv}/lib/rbtree/test_red_black_tree.c"

# Graphviz's lib/neatogen has sources for optional engines (ipsep, the vpsc
# constraint solver) that the configure flags upstream used excluded; the
# list below is the engine set the Dockerfile compiled, moved to 16.x.
neatogen=(
  adjust bfs call_tri circuit closest compute_hierarchy conjgrad
  constrained_majorization constraint delaunay dijkstra edges embed_graph
  geometry heap hedges info kkutils legal lu matinv matrix_ops multispline
  neatoinit neatosplines opt_arrangement overlap pca poly quad_prog_solve
  randomkit sgd site smart_ini_x solve stress stuff voronoi
)
neatogen_sources=()
for f in "${neatogen[@]}"; do neatogen_sources+=("${gv}/lib/neatogen/${f}.c"); done

mkdir -p "${work}/out"
raw="${work}/out/graphviz.raw.wasm"

echo "compiling graphviz ${GRAPHVIZ_VERSION} for wasm32-wasip1 with wasi-sdk ${WASI_SDK_VERSION}"
"${sdk}/bin/clang" \
  -g0 -Os \
  -ffile-prefix-map="${work}=/work" \
  -ffile-prefix-map="${here}=/build" \
  --sysroot="${sdk}/share/wasi-sysroot" \
  --target=wasm32-wasip1 \
  -Wextra \
  -Wno-format-security \
  -Wno-bitwise-instead-of-logical \
  -Wno-implicit-function-declaration \
  -Wno-incompatible-pointer-types-discards-qualifiers \
  -Wno-incompatible-pointer-types \
  -Wno-sign-compare \
  -Wno-missing-field-initializers \
  -Wno-undef \
  -Wno-uninitialized \
  -Wno-unused \
  -Wno-unused-parameter \
  -Wno-write-strings \
  -Wno-char-subscripts \
  -Wno-writable-strings \
  -Wl,--export-all \
  -Wl,--no-entry \
  -Wl,--error-limit=0 \
  -Wl,--import-undefined \
  -funsigned-char \
  -fno-strict-aliasing \
  -D_WASI_EMULATED_SIGNAL \
  -D_WASI_EMULATED_MMAN \
  -D_WASI_EMULATED_PROCESS_CLOCKS \
  -lwasi-emulated-mman \
  -lwasi-emulated-getpid \
  -lwasi-emulated-signal \
  -lwasi-emulated-process-clocks \
  -I"${gv}" \
  -I"${gv}/lib" \
  -I"${gv}/lib/ast" \
  -I"${gv}/lib/cdt" \
  -I"${gv}/lib/cgraph" \
  -I"${gv}/lib/circogen" \
  -I"${gv}/lib/common" \
  -I"${gv}/lib/dotgen" \
  -I"${gv}/lib/edgepaint" \
  -I"${gv}/lib/expr" \
  -I"${gv}/lib/fdpgen" \
  -I"${gv}/lib/gvc" \
  -I"${gv}/lib/label" \
  -I"${gv}/lib/mingle" \
  -I"${gv}/lib/neatogen" \
  -I"${gv}/lib/ortho" \
  -I"${gv}/lib/osage" \
  -I"${gv}/lib/pack" \
  -I"${gv}/lib/patchwork" \
  -I"${gv}/lib/pathplan" \
  -I"${gv}/lib/rbtree" \
  -I"${gv}/lib/sfdpgen" \
  -I"${gv}/lib/sfio" \
  -I"${gv}/lib/sparse" \
  -I"${gv}/lib/topfish" \
  -I"${gv}/lib/twopigen" \
  -I"${gv}/lib/util" \
  -I"${gv}/lib/vpsc" \
  -I"${gv}/lib/xdot" \
  -I"${expat}" \
  -I"${expat}/lib" \
  "${gv}"/lib/ast/*.c \
  "${gv}"/lib/cdt/*.c \
  "${gv}"/lib/cgraph/*.c \
  "${gv}"/lib/circogen/*.c \
  "${gv}"/lib/common/*.c \
  "${gv}"/lib/dotgen/*.c \
  "${gv}"/lib/edgepaint/*.c \
  "${gv}"/lib/expr/*.c \
  "${gv}"/lib/fdpgen/*.c \
  "${gv}"/lib/gvc/*.c \
  "${gv}"/lib/label/*.c \
  "${gv}"/lib/ortho/*.c \
  "${gv}"/lib/osage/*.c \
  "${gv}"/lib/pack/*.c \
  "${gv}"/lib/patchwork/*.c \
  "${gv}"/lib/pathplan/*.c \
  "${gv}"/lib/rbtree/*.c \
  "${gv}"/lib/sfdpgen/*.c \
  "${gv}"/lib/sfio/*.c \
  "${gv}"/lib/sfio/Sfio_f/_sfslen.c \
  "${gv}"/lib/sparse/*.c \
  "${gv}"/lib/util/*.c \
  "${gv}"/lib/xdot/*.c \
  "${neatogen_sources[@]}" \
  "${gv}"/lib/twopigen/*.c \
  "${gv}"/plugin/dot_layout/*.c \
  "${gv}"/plugin/neato_layout/*.c \
  "${gv}"/plugin/core/*.c \
  "${expat}/lib/xmlparse.c" \
  "${expat}/lib/xmltok.c" \
  "${expat}/lib/xmlrole.c" \
  "${expat}/lib/random_getentropy.c" \
  "${here}/patch.c" \
  "${here}/bind.c" \
  -o "${raw}"

echo "optimizing with binaryen ${BINARYEN_VERSION}"
"${wasm_opt}" -g --strip --strip-producers -c -Os "${raw}" -o "${out}"
sha256 "${out}"
