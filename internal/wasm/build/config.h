/* Graphviz build configuration for the wasm32-wasip1 target. Hand-written in
 * place of the configure-generated config.h so the build does not depend on
 * the host that ran configure: no zlib, no pango/cairo, no gd, no ltdl, no
 * dlopen; the layout engines and the core renderers only. build.sh fills in
 * the version from pins.yaml where configure would have. */
#ifndef GRAPHVIZ_WASM_CONFIG_H
#define GRAPHVIZ_WASM_CONFIG_H

#define PACKAGE_NAME "graphviz"
#define PACKAGE_TARNAME "graphviz"
#define PACKAGE_VERSION "@GRAPHVIZ_VERSION@"
#define PACKAGE_STRING "graphviz @GRAPHVIZ_VERSION@"
#define PACKAGE_BUGREPORT "https://gitlab.com/graphviz/graphviz/-/issues"
#define PACKAGE_URL ""
#define VERSION "@GRAPHVIZ_VERSION@"

#define DEFAULT_DPI 96
#define GVPLUGIN_VERSION 8
#define GVPLUGIN_CONFIG_FILE "config8"
#define WITH_CGRAPH 1
#define DIGCOLA 1
#define ORTHO 1
#define SFDP 1
#define HAVE_EXPAT 1

#define HAVE_DRAND48 1
#define HAVE_SRAND48 1
#define HAVE_SETENV 1
#define HAVE_STRCASESTR 1
#define HAVE_SYS_MMAN_H 1
#define HAVE_INTTYPES_H 1
#define HAVE_STDINT_H 1
#define HAVE_STDIO_H 1
#define HAVE_STDLIB_H 1
#define HAVE_STRINGS_H 1
#define HAVE_STRING_H 1
#define HAVE_SYS_STAT_H 1
#define HAVE_SYS_TIME_H 1
#define HAVE_SYS_TYPES_H 1
#define HAVE_UNISTD_H 1
#define STDC_HEADERS 1
#define YYTEXT_POINTER 1

#endif
