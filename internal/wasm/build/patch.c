#include <stdio.h>
#include <stdlib.h>
#include "gvc.h"
#include "gvplugin.h"
// After gvc.h and gvplugin.h, whose types it uses: GVC_t's definition, for
// the plugin list a context keeps.
#include "gvcint.h"
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

// wasm_bridge_SymList_default lists the built-in plugin libraries: the
// entries themselves, which are static, not copies. A copy was allocated per
// entry on every call and never freed; a context built with plugins of its
// own, which lists them before the built-in ones, made one such call.
WASM_EXPORT(wasm_bridge_SymList_default)
void wasm_bridge_SymList_default(GoSlice **ret) {
  GoSlice *v = (GoSlice *)malloc(sizeof(GoSlice));
  size_t len = sizeof(lt_preloaded_symbols) / sizeof(lt_preloaded_symbols[0]);
  v->len = len;
  void **data = malloc(8 * len);
  v->data = data;
  for (int i = 0; i < len; i++) {
    *data = &lt_preloaded_symbols[i];
    data += 2;
  }
  *ret = v;
}

// bridge_free_context frees gvc and then the plugin list it was made from.
// The bridge allocates that list for the gvContextPlugins call, Graphviz
// keeps it for the life of the context, and gvFreeContext leaves it, as it
// would a program's static list; the built-in list, static here too, is
// left. A clone shares the list with its original, as it shares the plugin
// tables gvFreeContext frees, so it is freed before the original is.
int bridge_free_context(GVC_t *gvc) {
  const lt_symlist_t *builtins = gvc->common.builtins;
  int ret = gvFreeContext(gvc);
  if (builtins != lt_preloaded_symbols) {
    free((void *)builtins);
  }
  return ret;
}

// bridge_free_plugin_list frees the symbol a context's own plugins were
// listed under, with the library it names: the name, the package name and
// the API array the bindings set, which only this side allocated. The
// plugins the API array copied from are not the list's.
void bridge_free_plugin_list(lt_symlist_t *sym) {
  gvplugin_library_t *lib = (gvplugin_library_t *)sym->address;
  if (lib != NULL) {
    free((void *)lib->packagename);
    free(lib->apis);
    free(lib);
  }
  free((void *)sym->name);
  free(sym);
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
