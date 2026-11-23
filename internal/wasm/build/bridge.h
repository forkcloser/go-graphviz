// The hand-written bridge functions bind.proto binds by alias, so that the
// generated bind.c can call them; patch.c defines them.
#ifndef BRIDGE_H
#define BRIDGE_H

#include "gvc.h"
#include "gvplugin.h"

// bridge_free_context is gvFreeContext and then frees the plugin list the
// context was made from, unless it is the built-in one.
int bridge_free_context(GVC_t *gvc);

// bridge_free_plugin_list frees the symbol a context's own plugins were
// listed under, with its library, name and API array.
void bridge_free_plugin_list(lt_symlist_t *sym);

// bridge_free_plugin frees one plugin's API record, its types, and each
// type's name, engine and features.
void bridge_free_plugin(gvplugin_api_t *api);

// bridge_version is the Graphviz release the module is built from, as
// gvcVersion answers it.
char *bridge_version(void);

// bridge_render_data is gvRenderData, and leaves no job on the context when
// the render failed.
int bridge_render_data(GVC_t *gvc, graph_t *g, const char *format,
                       char **result, unsigned int *length);

#endif
