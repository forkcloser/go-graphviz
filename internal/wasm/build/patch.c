#include <stdio.h>
#include <stdlib.h>
#include "gvplugin.h"
#include "gvplugin_render.h"
#include "gvio.h"
#include "textspan.h"

// WASM_EXPORT exports a bridge function from the module under its own name;
// build.sh links with no other exports but malloc and free.
#define WASM_EXPORT(name) __attribute__((export_name(#name)))

static const char *tmpfilename = "tmpfile";

FILE *tmpfile(void)
{
  return fopen(tmpfilename, "w+");
}

extern gvplugin_library_t gvplugin_dot_layout_LTX_library;
extern gvplugin_library_t gvplugin_neato_layout_LTX_library;
extern gvplugin_library_t gvplugin_core_LTX_library;

lt_symlist_t lt_preloaded_symbols[] = {
    { "gvplugin_dot_layout_LTX_library", (void *)(&gvplugin_dot_layout_LTX_library) },
    { "gvplugin_neato_layout_LTX_library", (void*)(&gvplugin_neato_layout_LTX_library) },
    { "gvplugin_core_LTX_library", (void*)(&gvplugin_core_LTX_library) },
};

static gvplugin_api_t api_zero = {(api_t)0, 0};
static gvplugin_installed_t installed_zero = {0, NULL, 0, NULL, NULL};
static lt_symlist_t symlist_zero = {NULL, NULL};

typedef struct { int len; void *data; } GoSlice;

WASM_EXPORT(wasm_bridge_PluginAPI_zero)
void wasm_bridge_PluginAPI_zero(void **ret) {
  *ret = &api_zero;
}

WASM_EXPORT(wasm_bridge_PluginInstalled_zero)
void wasm_bridge_PluginInstalled_zero(void **ret) {
  *ret = &installed_zero;
}

WASM_EXPORT(wasm_bridge_SymList_zero)
void wasm_bridge_SymList_zero(void **ret) {
  *ret = &symlist_zero;
}

WASM_EXPORT(wasm_bridge_SymList_default)
void wasm_bridge_SymList_default(GoSlice **ret) {
  GoSlice *v = (GoSlice *)malloc(sizeof(GoSlice));
  size_t len = sizeof(lt_preloaded_symbols) / sizeof(lt_preloaded_symbols[0]);
  v->len = len;
  void **data = malloc(8 * len);
  v->data = data;
  for (int i = 0; i < len; i++) {
    lt_symlist_t *elem = (lt_symlist_t *)malloc(sizeof(lt_symlist_t));
    memcpy(elem, &lt_preloaded_symbols[i], sizeof(lt_symlist_t));
    *data = elem;
    data += 2;
  }
  *ret = v;
}

// A device that encodes the page itself hands the bytes to Graphviz through
// gvwrite, as its own devices do: in memory, gvwrite appends to the buffer
// gvRenderData allocated, which setting output_data would replace and leak.
WASM_EXPORT(wasm_bridge_Job_writeOutput)
void wasm_bridge_Job_writeOutput(GVJ_t *job, const char *data, size_t len) {
  gvwrite(job, data, len);
}

// A text-layout plugin owns a span's layout and the function that frees it,
// and must null both when it keeps no layout: Graphviz frees a layout with
// free_layout when both are set, and measures the text of an HTML label in a
// span it leaves uninitialized.
WASM_EXPORT(wasm_bridge_Textspan_clearLayout)
void wasm_bridge_Textspan_clearLayout(textspan_t *span) {
  span->layout = NULL;
  span->free_layout = NULL;
}

int main() { return 0; }
