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
  Linux-aarch64) host=linux-arm64; sdk_host=arm64-linux; binaryen_host=aarch64-linux ;;
  Linux-x86_64) host=linux-amd64; sdk_host=x86_64-linux; binaryen_host=x86_64-linux ;;
  *) echo "unsupported host $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

# pin <name> <field>: one value of a pins.yaml entry (version, url, sha256).
pin() {
  (cd "${root}" && limen pins get "$1" "$2")
}

graphviz_version="$(pin graphviz version)"
expat_tag="$(pin expat version)"      # R_2_8_5, the tag libexpat releases under
expat_version="${expat_tag#R_}"
expat_version="${expat_version//_/.}" # 2.8.5
wasi_sdk_version="$(pin "wasi-sdk-${host}" version)"
binaryen_version="$(pin "binaryen-${host}" version)"

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
  echo "${sum}  ${file}" | sha256sum -c - > /dev/null || {
    echo "${name}: sha256 mismatch against pins.yaml" >&2
    rm -f "${file}"
    exit 1
  }
}

# unpack <file> <dir> [anew]: extract the archive to its directory under the
# work dir. It is extracted beside its place and renamed there once tar is
# done, so a directory that is there is a whole one, and is kept; an
# extraction cut short leaves nothing a later build would compile.
#
# With "anew" the directory is removed and extracted again on every build.
# That is for the trees this script edits, Graphviz and expat: kept, a tree
# would carry the edits of the build that unpacked it, and a patch changed
# since, or an edit removed from this script, would not show in the module.
unpack() {
  local file="${work}/dl/$1" dir="${work}/src/$2"
  if [ "${3:-}" = anew ]; then
    rm -rf "${dir:?}"
  fi
  [ -d "${dir}" ] && return
  rm -rf "${dir:?}.part"
  mkdir -p "${dir}.part"
  tar -xzf "${file}" -C "${dir}.part"
  [ -d "${dir}.part/$2" ] || { echo "$1 did not unpack to $2" >&2; exit 1; }
  mv "${dir}.part/$2" "${dir}"
  rmdir "${dir}.part"
}

sdk_dir="wasi-sdk-${wasi_sdk_version}.0-${sdk_host}"
binaryen_dir="binaryen-version_${binaryen_version}"
expat_dir="expat-${expat_version}"
gv_dir="graphviz-${graphviz_version}"

fetch "wasi-sdk-${host}" "${sdk_dir}.tar.gz"
fetch "binaryen-${host}" "${binaryen_dir}-${binaryen_host}.tar.gz"
fetch expat "${expat_dir}.tar.gz"
fetch graphviz "${gv_dir}.tar.gz"
unpack "${sdk_dir}.tar.gz" "${sdk_dir}"
unpack "${binaryen_dir}-${binaryen_host}.tar.gz" "${binaryen_dir}"
unpack "${expat_dir}.tar.gz" "${expat_dir}" anew
unpack "${gv_dir}.tar.gz" "${gv_dir}" anew

sdk="${work}/src/${sdk_dir}"
wasm_opt="${work}/src/${binaryen_dir}/bin/wasm-opt"
wasm_dis="${work}/src/${binaryen_dir}/bin/wasm-dis"
gv="${work}/src/${gv_dir}"
expat="${work}/src/${expat_dir}"

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
sed -i.orig 's/size_t oldsz = lp->u.txt.nspans + 1;/size_t oldsz = lp->u.txt.nspans;/' "${labels}"
rm -f "${labels}.orig"
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
sed -i.orig 's/^  if (us->nocache) {$/  if (us->f) {/' "${usershape}"
rm -f "${usershape}.orig"
grep -q '^  if (us->f) {$' "${usershape}" || {
  echo "gvusershape.c: gvusershape_file_release does not look like the one this fix is for" >&2
  exit 1
}

# A third: webp_size reads a WebP's size from where only a plain lossy one
# keeps it, so a lossless (VP8L) or extended (VP8X) WebP, any with alpha,
# came out tens of thousands to hundreds of millions of points a side, as it
# does in dot 16.1.1. webp-size.patch reads each layout's canvas size from
# where the format keeps it.
# The patch is applied only to the file it was written against, this
# Graphviz's gvusershape.c after the edit above, by digest: patch applies a
# hunk with fuzz without failing, and macOS's says nothing when it does, so
# after a Graphviz bump a fuzzed hunk could land on a changed webp_size and
# bring its marker with it. A new Graphviz means rechecking the patch and
# this digest together.
webp_size_base=1af7a25ce49beda3222bd12dac1adbf67ffa11301a6112588a755138b5400e67
echo "${webp_size_base}  ${usershape}" | sha256sum -c - > /dev/null || {
  echo "gvusershape.c: not the file webp-size.patch was written against; recheck the patch for this Graphviz" >&2
  exit 1
}
# POSIX options only: -p0 for the paths the patch names, -N to refuse a
# patch already applied.
(cd "${gv}" && patch -p0 -N) < "${here}/webp-size.patch"
grep -q 'go-graphviz: the canvas size of each of the three WebP layouts' "${usershape}" || {
  echo "gvusershape.c: webp-size.patch did not apply" >&2
  exit 1
}

# Another in that file: Graphviz sizes an image once, by name, and keeps the size for as
# long as the process lives, which for its dot is one run and here is every
# layout the module ever does: a file that changed between two layouts kept
# the size it had at the first. image-size.patch counts layouts and sizes an
# image again when its size is of another layout, once a layout and not once
# a node. gvLayoutJobs, which every layout goes through, does the counting.
# The patch is applied to gvusershape.c as webp-size.patch left it, by
# digest, for the reason above.
image_size_base=046997239f311a80c3c8e49e1d5f149d7536ca87c241b431f15165e00bc33a9e
echo "${image_size_base}  ${usershape}" | sha256sum -c - > /dev/null || {
  echo "gvusershape.c: not the file image-size.patch was written against; recheck the patch for this Graphviz" >&2
  exit 1
}
(cd "${gv}" && patch -p0 -N) < "${here}/image-size.patch"
grep -q 'go-graphviz: an image is sized once a layout' "${usershape}" || {
  echo "gvusershape.c: image-size.patch did not apply" >&2
  exit 1
}
gvcproc="${gv}/lib/gvc/gvcproc.h"
sed -i.orig 's/^    usershape_t \*gvusershape_find(const char \*name);$/& void gvusershape_layout_begins(void);/' "${gvcproc}"
rm -f "${gvcproc}.orig"
grep -q 'void gvusershape_layout_begins(void);$' "${gvcproc}" || {
  echo "gvcproc.h: gvusershape_find is not declared as this fix expects" >&2
  exit 1
}
gvlayout="${gv}/lib/gvc/gvlayout.c"
sed -i.orig 's/^    int rc;$/& gvusershape_layout_begins();/' "${gvlayout}"
rm -f "${gvlayout}.orig"
[ "$(grep -c '^    int rc; gvusershape_layout_begins();$' "${gvlayout}")" = 1 ] || {
  echo "gvlayout.c: gvLayoutJobs does not look like the one this fix is for" >&2
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

# A fourth: Graphviz's parser frees what it holds in the grammar's error
# rule, which a syntax error goes through. When bison runs out of its own
# stack ("memory exhausted") it returns without that rule: the graph comes
# back half built, and the grammar's stack of open subgraphs is never freed,
# about 175 KiB for clusters nested 4000 deep. agconcat runs the rule's
# clean-up, graph_error, for any parse bison reports failed; after a syntax
# error it finds the graph already gone and does nothing.
grammar="${gv}/lib/cgraph/grammar.c"
sed -i.orig 's/^	aagparse(scanner);$/	if (aagparse(scanner) != 0) graph_error(scanner);/' "${grammar}"
rm -f "${grammar}.orig"
grep -q '^	if (aagparse(scanner) != 0) graph_error(scanner);$' "${grammar}" || {
  echo "grammar.c: agconcat does not look like the one this fix is for" >&2
  exit 1
}

# A fifth: the parser of a record label (parse_reclbl, lib/common/shapes.c)
# frees what it built, when the label cannot be read, by a count of fields
# it sets only once it is done: the fields read before the fault were never
# freed, about 330 bytes for each layout of label="{a|", more for a longer
# label. reclbl-free.patch counts the fields as they are built. It is
# applied to this Graphviz's shapes.c alone, by digest, as the patch above
# is and for the same reason.
shapes="${gv}/lib/common/shapes.c"
reclbl_free_base=78370a37daef0003c4a00bd303b744f811dd240668fee57e3e7d6b06321aa82c
echo "${reclbl_free_base}  ${shapes}" | sha256sum -c - > /dev/null || {
  echo "shapes.c: not the file reclbl-free.patch was written against; recheck the patch for this Graphviz" >&2
  exit 1
}
(cd "${gv}" && patch -p0 -N) < "${here}/reclbl-free.patch"
grep -q 'go-graphviz: the fields built so far are counted' "${shapes}" || {
  echo "shapes.c: reclbl-free.patch did not apply" >&2
  exit 1
}

# A sixth: sfdp calls remove_overlap at the end of every layout, with the
# node sizes when prism is to remove overlaps and with none otherwise. The
# function begins by returning when it has no sizes. Built without a
# triangulation library, as here, it is a stand-in that reports "Graphviz
# not built with triangulation library" whatever it is given, and sfdp
# never gives it sizes in such a build: an error reported for a layout that
# asked nothing of the library. The stand-in reports when it has sizes.
overlap="${gv}/lib/neatogen/overlap.c"
sed -i.orig 's/^    if (!atomic_flag_test_and_set(&once)) {$/    if (label_sizes \&\& !atomic_flag_test_and_set(\&once)) {/' "${overlap}"
rm -f "${overlap}.orig"
grep -q '^    if (label_sizes && !atomic_flag_test_and_set(&once)) {$' "${overlap}" || {
  echo "overlap.c: the stand-in remove_overlap does not look like the one this fix is for" >&2
  exit 1
}

# A seventh: the DOT writer puts a string in its canonical form into a buffer
# of 2n+3 bytes for n characters (agstrcanon_bytes), and the form can be
# longer: a quote is written as two bytes, and a line break, two more, goes
# in after every linelength characters (128, or the graph's attribute) at
# the next character an unquoted name could not hold. A value that is nearly
# all quotes overran the buffer: an 8 KiB label came out with bytes of the
# heap in it, a 2 MiB one trapped the module. A character is at most two
# bytes and is followed by at most one break, so 4n, the two quotes around
# them and the terminator hold any string.
canon="${gv}/lib/cgraph/agstrcanon.h"
sed -i.orig 's/^  return 2 \* strlen(str) + 3;$/  return 4 * strlen(str) + 3;/' "${canon}"
rm -f "${canon}.orig"
grep -q '^  return 4 \* strlen(str) + 3;$' "${canon}" || {
  echo "agstrcanon.h: agstrcanon_bytes does not look like the one this fix is for" >&2
  exit 1
}

mkdir -p "${work}/out"
raw="${work}/out/graphviz.raw.wasm"

# The module exports what Go calls and nothing else: malloc and free, and the
# wasm_bridge_ functions, which bind.c and patch.c mark with WASM_EXPORT.
# Whatever Graphviz code none of them reaches is left out, which
# --export-all kept.
#
# It imports what bind.c marks with WASM_IMPORT, the callbacks the Go host
# provides, and the WASI calls listed after the link. The link takes no
# --import-undefined, so a symbol no source defines (a file missing from the
# list below) stops the build here, where with the flag it became an import
# the host does not have and the module failed to load.

# The module's C stack is what native dot runs on, the main thread's 8 MiB
# (`ulimit -s`; Linux's _STK_LIM in include/uapi/linux/resource.h). wasm-ld
# gives 64 KiB when it is not told, and Graphviz recurses with the graph:
# clusters nested a thousand deep, which dot draws, overflowed it.
stack_size=$((8 * 1024 * 1024))

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
  -Wl,--export=malloc \
  -Wl,--export=free \
  -Wl,--no-entry \
  -Wl,-z,stack-size="${stack_size}" \
  -Wl,--error-limit=0 \
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
  "${expat}/lib/xcs.c" \
  "${expat}/lib/random_getentropy.c" \
  "${here}/patch.c" \
  "${here}/bind.c" \
  -o "${raw}"

# What the link does not see: an import some source declares for itself, and a
# WASI call a new wasi-libc starts to make. Every import of the module is a
# callback bind.c declares or one of the WASI calls below, which are the ones
# the module makes today and the host provides; anything else stops the build
# with its name, where it would have failed the load. A new WASI call is added
# here once the host is known to provide it.
wasi=(
  clock_time_get environ_get environ_sizes_get fd_close fd_fdstat_get
  fd_fdstat_set_flags fd_filestat_get fd_pread fd_prestat_dir_name
  fd_prestat_get fd_read fd_seek fd_write path_filestat_get path_open
  proc_exit random_get
)
callbacks="$(sed -n 's/^WASM_IMPORT(\(.*\))$/env \1/p' "${here}/bind.c")"
[ -n "${callbacks}" ] || { echo "bind.c declares no import: WASM_IMPORT is not where this check reads it" >&2; exit 1; }
allowed="$({ echo "${callbacks}"; printf 'wasi_snapshot_preview1 %s\n' "${wasi[@]}"; } | sort)"
imported="$("${wasm_dis}" "${raw}" | sed -n 's/^ *(import "\([^"]*\)" "\([^"]*\)".*/\1 \2/p' | sort)"
[ -n "${imported}" ] || { echo "the module has no import: wasm-dis does not print them as this check reads them" >&2; exit 1; }
unknown="$(comm -13 <(echo "${allowed}") <(echo "${imported}"))"
if [ -n "${unknown}" ]; then
  echo "the module imports what is neither a callback bind.c declares nor a WASI call build.sh lists:" >&2
  echo "${unknown}" >&2
  exit 1
fi

echo "optimizing with binaryen ${binaryen_version}"
"${wasm_opt}" -g --strip --strip-producers -c -Os "${raw}" -o "${out}"
sha256sum "${out}"
