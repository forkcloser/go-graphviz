/* expat build configuration for the wasm32-wasip1 target, hand-written in
 * place of configure's output. Entropy comes from wasi-libc's getentropy. */
#ifndef EXPAT_CONFIG_H
#define EXPAT_CONFIG_H 1
#define BYTEORDER 1234
#define HAVE_GETENTROPY 1
#define HAVE_INTTYPES_H 1
#define HAVE_STDINT_H 1
#define HAVE_STDIO_H 1
#define HAVE_STDLIB_H 1
#define HAVE_STRINGS_H 1
#define HAVE_STRING_H 1
#define HAVE_SYS_STAT_H 1
#define HAVE_SYS_TYPES_H 1
#define HAVE_UNISTD_H 1
#define STDC_HEADERS 1
#define PACKAGE "expat"
#define PACKAGE_NAME "expat"
#define PACKAGE_TARNAME "expat"
#define PACKAGE_VERSION "2.8.5"
#define PACKAGE_STRING "expat 2.8.5"
#define PACKAGE_BUGREPORT "https://github.com/libexpat/libexpat/issues"
#define PACKAGE_URL ""
#define VERSION "2.8.5"
#define XML_CONTEXT_BYTES 1024
#define XML_DTD 1
#define XML_GE 1
#define XML_NS 1
#endif
