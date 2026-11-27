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

// bridge_reader_t reads the graphs of one DOT text in turn, each from where
// the one before it ended.
typedef struct bridge_reader bridge_reader_t;

// bridge_reader_open copies text and returns a reader at its start, or NULL
// when there is no memory for it.
bridge_reader_t *bridge_reader_open(const char *text);

// bridge_reader_next is the next graph of the text, or NULL when the rest
// holds none or Graphviz cannot read it.
Agraph_t *bridge_reader_next(bridge_reader_t *reader);

// bridge_reader_close frees the reader and its copy of the text.
void bridge_reader_close(bridge_reader_t *reader);

// bridge_version is the Graphviz release the module is built from, as
// gvcVersion answers it.
char *bridge_version(void);

// bridge_image_file is the path Graphviz opens for the image file a graph
// names, empty when it finds none.
char *bridge_image_file(const char *name);

// bridge_render_data is gvRenderData, and leaves no job on the context when
// the render failed.
int bridge_render_data(GVC_t *gvc, graph_t *g, const char *format,
                       char **result, unsigned int *length);

#endif
