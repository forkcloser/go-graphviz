#!/usr/bin/env bash
# Build internal/wasm/graphviz.wasm: Graphviz, expat and this module's C
# bridge compiled to wasm32-wasip1 with a pinned wasi-sdk, then shrunk with a
# pinned binaryen.
#
# Every input is a pins.yaml entry, read back with `limen pins get` and
# verified against its recorded sha256 before it is unpacked. No container,
# no system compiler and no configure run: the two configuration headers
# next to this script stand in for configure's output, so the result does not
# depend on the host that built it. Graphviz's asserts embed __FILE__, so the
# paths of the unpacked sources and of this directory are mapped to fixed
# names, and the shell globs that order the sources are expanded under the C
# locale; without either, two builders produce different bytes. With both,
# Linux x86_64 and macOS arm64 produce the same blob, which the ci workflow
# proves on every change.
#
#   just build wasm                 # the recipe; limen on the hermetic PATH
#   WORK=/some/dir just build wasm  # keep downloads elsewhere
set -euo pipefail
export LC_ALL=C

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${here}/../../.." && pwd)"
work="${WORK:-${root}/build/wasm}"
out="${root}/internal/wasm/graphviz.wasm"

# The pins name hosts the Go way; the archives name them their own way.
case "$(uname -s)-$(uname -m)" in
  Darwin-arm64) host=macos-arm64; sdk_host=arm64-macos; binaryen_host=arm64-macos ;;
  Darwin-x86_64) host=macos-amd64; sdk_host=x86_64-macos; binaryen_host=x86_64-macos ;;
  Linux-aarch64) host=linux-arm64; sdk_host=arm64-linux; binaryen_host=aarch64-linux ;;
  Linux-x86_64) host=linux-amd64; sdk_host=x86_64-linux; binaryen_host=x86_64-linux ;;
  *) echo "unsupported host $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

# pin <name> <field>: one value of a pins.yaml entry (version, url, sha256).
pin() {
  (cd "${root}" && limen pins get "$1" "$2")
}

graphviz_version="$(pin graphviz version)"
recorded="$(tr -d '[:space:]' < "${root}/graphviz.version")"
[ "${graphviz_version}" = "${recorded}" ] || {
  echo "graphviz.version says ${recorded}, pins.yaml says ${graphviz_version}: move both" >&2
  exit 1
}
expat_tag="$(pin expat version)"      # R_2_8_5, the tag libexpat releases under
expat_version="${expat_tag#R_}"
expat_version="${expat_version//_/.}" # 2.8.5
wasi_sdk_version="$(pin "wasi-sdk-${host}" version)"
binaryen_version="$(pin "binaryen-${host}" version)"

# sha256 [-c -]: the host's checksum tool under one name; coreutils calls it
# sha256sum, macOS ships it as shasum -a 256.
sha256() {
  if command -v sha256sum > /dev/null 2>&1; then
    sha256sum "$@"
  else
    shasum -a 256 "$@"
  fi
}

# fetch <pin> <file>: download the pin's url once into the work dir, verify
# against the pin's sha256 always.
fetch() {
  local name="$1" url sum
  local file="${work}/dl/$2"
  url="$(pin "${name}" url)"
  sum="$(pin "${name}" sha256)"
  mkdir -p "${work}/dl"
  if [ ! -f "${file}" ]; then
    echo "fetching ${name}"
    curl --proto '=https' --tlsv1.2 -fsSL --retry 5 --retry-delay 3 -o "${file}.part" "${url}"
    mv "${file}.part" "${file}"
  fi
  echo "${sum}  ${file}" | sha256 -c - > /dev/null || {
    echo "${name}: sha256 mismatch against pins.yaml" >&2
    rm -f "${file}"
    exit 1
  }
}

# unpack <file> <dir>: extract once; a present dir is a finished one.
unpack() {
  local file="${work}/dl/$1" dir="${work}/src/$2"
  [ -d "${dir}" ] && return
  mkdir -p "${work}/src"
  tar -xzf "${file}" -C "${work}/src"
  [ -d "${dir}" ] || { echo "$1 did not unpack to $2" >&2; exit 1; }
}

sdk_dir="wasi-sdk-${wasi_sdk_version}.0-${sdk_host}"
binaryen_dir="binaryen-version_${binaryen_version}"
expat_dir="libexpat-${expat_tag}"
gv_dir="graphviz-${graphviz_version}"

fetch "wasi-sdk-${host}" "${sdk_dir}.tar.gz"
fetch "binaryen-${host}" "${binaryen_dir}-${binaryen_host}.tar.gz"
fetch expat "${expat_dir}.tar.gz"
fetch graphviz "${gv_dir}.tar.gz"
unpack "${sdk_dir}.tar.gz" "${sdk_dir}"
unpack "${binaryen_dir}-${binaryen_host}.tar.gz" "${binaryen_dir}"
unpack "${expat_dir}.tar.gz" "${expat_dir}"
unpack "${gv_dir}.tar.gz" "${gv_dir}"

sdk="${work}/src/${sdk_dir}"
wasm_opt="${work}/src/${binaryen_dir}/bin/wasm-opt"
gv="${work}/src/${gv_dir}"
expat="${work}/src/${expat_dir}/expat"

# The sources are built as they ship, with three exceptions, each applied to
# the unpacked copy: configure's two outputs are replaced by the checked-in
# headers, with the versions filled in from the pins where configure would
# have put them, and the one file under lib/ that defines main() is dropped.
sed "s/@GRAPHVIZ_VERSION@/${graphviz_version}/g" "${here}/config.h" > "${gv}/config.h"
sed "s/@EXPAT_VERSION@/${expat_version}/g" "${here}/expat_config.h" > "${expat}/expat_config.h"
rm -f "${gv}/lib/rbtree/test_red_black_tree.c"

# One source fix on top of the release: storeline (lib/common/labels.c) tells
# gv_recalloc that one more span existed than did, so the span it is about to
# fill is never zeroed, and a line that is empty skips textspan_size, the
# only other thing that would null the span's layout fields. A label whose
# first line is empty then frees a garbage layout pointer in free_textspan,
# which under wasm is an indirect call into nowhere. The old count is the
# number of spans stored so far.
labels="${gv}/lib/common/labels.c"
if grep -q 'size_t oldsz = lp->u.txt.nspans + 1;' "${labels}"; then
  sed -i.orig 's/size_t oldsz = lp->u.txt.nspans + 1;/size_t oldsz = lp->u.txt.nspans;/' "${labels}"
  rm -f "${labels}.orig"
fi
grep -q 'size_t oldsz = lp->u.txt.nspans;' "${labels}" || {
  echo "labels.c: storeline does not look like the one this fix is for" >&2
  exit 1
}

# A second fix: Graphviz keeps every image file it sizes open for the life of
# the process, closing it after use only once 50 are open, and a host file
# system pins an open file (Windows will not delete it). Nothing reads the
# handle after sizing here, the renderer reads images itself, and
# gvusershape_file_access reopens a closed file when anything does, so
# gvusershape_file_release always closes it.
usershape="${gv}/lib/gvc/gvusershape.c"
if grep -q '^  if (us->nocache) {$' "${usershape}"; then
  sed -i.orig 's/^  if (us->nocache) {$/  if (us->f) {/' "${usershape}"
  rm -f "${usershape}.orig"
fi
grep -q '^  if (us->f) {$' "${usershape}" || {
  echo "gvusershape.c: gvusershape_file_release does not look like the one this fix is for" >&2
  exit 1
}

# A third: webp_size reads a WebP's size from where only a plain lossy one
# keeps it, so a lossless (VP8L) or extended (VP8X) WebP, any with alpha,
# came out tens of thousands to hundreds of millions of points a side, as it
# does in dot 16.1.0. webp-size.patch reads each layout's canvas size from
# where the format keeps it.
# The patch is applied only to the file it was written against, this
# Graphviz's gvusershape.c after the edit above, by digest: patch applies a
# hunk with fuzz without failing, and macOS's says nothing when it does, so
# after a Graphviz bump a fuzzed hunk could land on a changed webp_size and
# bring its marker with it. A new Graphviz means rechecking the patch and
# this digest together.
webp_size_base=75ea4df566d9627b88748d1f684c6edddeb50aafee074e969a12b7dad796b048
if ! grep -q 'go-graphviz: the canvas size of each of the three WebP layouts' "${usershape}"; then
  echo "${webp_size_base}  ${usershape}" | sha256 -c - > /dev/null || {
    echo "gvusershape.c: not the file webp-size.patch was written against; recheck the patch for this Graphviz" >&2
    exit 1
  }
  # POSIX options only: -p0 for the paths the patch names, -N to refuse a
  # patch already applied.
  (cd "${gv}" && patch -p0 -N) < "${here}/webp-size.patch"
fi
grep -q 'go-graphviz: the canvas size of each of the three WebP layouts' "${usershape}" || {
  echo "gvusershape.c: webp-size.patch did not apply" >&2
  exit 1
}

# Graphviz's lib/neatogen has sources for optional engines (ipsep, the vpsc
# constraint solver) that the configure flags upstream used excluded; the
# list below is the engine set the original container build compiled.
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

echo "compiling graphviz ${graphviz_version} and expat ${expat_version} for wasm32-wasip1 with wasi-sdk ${wasi_sdk_version}"
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

echo "optimizing with binaryen ${binaryen_version}"
"${wasm_opt}" -g --strip --strip-producers -c -Os "${raw}" -o "${out}"
sha256 "${out}"
