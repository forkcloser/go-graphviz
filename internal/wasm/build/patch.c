#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include "gvc.h"
#include "gvplugin.h"
// After gvc.h and gvplugin.h, whose types it uses: GVC_t's definition, for
// the plugin list a context keeps.
#include "gvcint.h"
// For gvjobs_delete: without its declaration the call is linked with
// another signature, and traps.
#include "gvcproc.h"
#include "gvplugin_render.h"
#include "gvio.h"
#include "textspan.h"
#include "bridge.h"

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

// bridge_free_plugin frees what the bindings allocated for one plugin: the
// API record, its array of types, and each type's name, engine and
// features. Graphviz keeps pointers to them for as long as a context lists
// the plugin, and frees none of them with the context.
void bridge_free_plugin(gvplugin_api_t *api) {
  if (api->types != NULL) {
    for (gvplugin_installed_t *type = api->types; type->type != NULL; type++) {
      free((void *)type->type);
      free(type->engine);
      free(type->features);
    }
    free(api->types);
  }
  free(api);
}

// bridge_reader_t reads the graphs of one DOT text in turn. Graphviz's own
// readers hand the scanner a line at a time, and each agread makes a scanner
// of its own that is destroyed with whatever it had read and not used: a
// graph that starts on the line another one ends on is lost. This one hands
// the scanner the text up to each closing brace that is a token of its own,
// so the scanner, which reads no further than the token it returns, holds
// nothing past the brace that ends a graph, and the next agread starts
// right after it.
//
// Each agread counts lines from 1. So that an error names the line of the
// text, a read that does not start at the text's start is told its line
// first, with the "# <line>" directive the scanner takes at the start of a
// line.
struct bridge_reader {
  char *text;
  size_t len;
  size_t cur;
  // line is the line of the text cur is on, from 1.
  size_t line;
  // in is what the text handed out so far leaves the scanner inside of, and
  // html_depth how many angle brackets deep an HTML string is.
  int in;
  int html_depth;
  // directive is what is left of the line directive to hand out before the
  // text, and directive_text holds it.
  const char *directive;
  char directive_text[32];
  Agiodisc_t io;
  Agdisc_t disc;
};

// What a brace can be inside of, by the scanner's rules (lib/cgraph/scan.l):
// a comment, a quoted string, an HTML string. A brace in one of them is not
// a token, and a chunk that ended there would end inside a token, which the
// scanner then goes over again from its start: once for each such brace.
enum {
  IN_GRAPH,
  // After a '/' that opens a comment if a '/' or a '*' follows.
  IN_SLASH,
  // "//" or "#", to the end of the line.
  IN_LINE_COMMENT,
  // "/*" to "*/", and after a '*' inside it.
  IN_COMMENT,
  IN_COMMENT_STAR,
  // A quoted string, and after a backslash inside it.
  IN_STRING,
  IN_STRING_ESCAPE,
  IN_HTML,
};

// bridge_reader_ends_chunk follows the scanner over c and reports whether c
// is a closing brace that is a token of its own.
static int bridge_reader_ends_chunk(bridge_reader_t *reader, char c) {
  switch (reader->in) {
  case IN_SLASH:
    if (c == '/') {
      reader->in = IN_LINE_COMMENT;
      return 0;
    }
    if (c == '*') {
      reader->in = IN_COMMENT;
      return 0;
    }
    reader->in = IN_GRAPH;
    // fall through
  case IN_GRAPH:
    switch (c) {
    case '/':
      reader->in = IN_SLASH;
      break;
    case '#':
      reader->in = IN_LINE_COMMENT;
      break;
    case '"':
      reader->in = IN_STRING;
      break;
    case '<':
      reader->in = IN_HTML;
      reader->html_depth = 1;
      break;
    case '}':
      return 1;
    }
    return 0;
  case IN_LINE_COMMENT:
    if (c == '\n') {
      reader->in = IN_GRAPH;
    }
    return 0;
  case IN_COMMENT:
    if (c == '*') {
      reader->in = IN_COMMENT_STAR;
    }
    return 0;
  case IN_COMMENT_STAR:
    if (c == '/') {
      reader->in = IN_GRAPH;
    } else if (c != '*') {
      reader->in = IN_COMMENT;
    }
    return 0;
  case IN_STRING:
    if (c == '\\') {
      reader->in = IN_STRING_ESCAPE;
    } else if (c == '"') {
      reader->in = IN_GRAPH;
    }
    return 0;
  case IN_STRING_ESCAPE:
    reader->in = IN_STRING;
    return 0;
  default:
    if (c == '<') {
      reader->html_depth++;
    } else if (c == '>' && --reader->html_depth == 0) {
      reader->in = IN_GRAPH;
    }
    return 0;
  }
}

static int bridge_reader_read(void *chan, char *buf, int bufsize) {
  bridge_reader_t *reader = chan;
  int n = 0;

  if (reader->directive != NULL) {
    while (n < bufsize && *reader->directive != '\0') {
      buf[n++] = *reader->directive++;
    }

    if (*reader->directive == '\0') {
      reader->directive = NULL;
    }

    return n;
  }

  while (n < bufsize && reader->cur < reader->len) {
    char c = reader->text[reader->cur++];

    buf[n++] = c;
    if (c == '\n') {
      reader->line++;
    }

    if (bridge_reader_ends_chunk(reader, c)) {
      break;
    }
  }

  return n;
}

// bridge_reader_open copies text, which ends at its first NUL as a C string
// does, and returns a reader at its start, or NULL when there is no memory.
bridge_reader_t *bridge_reader_open(const char *text) {
  bridge_reader_t *reader = calloc(1, sizeof(*reader));
  if (reader == NULL) {
    return NULL;
  }

  reader->text = strdup(text);
  if (reader->text == NULL) {
    free(reader);
    return NULL;
  }

  reader->len = strlen(reader->text);
  reader->line = 1;
  reader->io = AgIoDisc;
  reader->io.afread = bridge_reader_read;
  reader->disc.id = &AgIdDisc;
  reader->disc.io = &reader->io;

  return reader;
}

// bridge_reader_names is whether c can be part of a name, by the scanner's
// rules: a letter, a digit, an underscore, or any byte above ASCII.
static int bridge_reader_names(char c) {
  unsigned char u = (unsigned char)c;

  return (u >= 'a' && u <= 'z') || (u >= 'A' && u <= 'Z') ||
         (u >= '0' && u <= '9') || u == '_' || u >= 0x80;
}

// bridge_reader_begins_graph is whether the text at s starts with one of the
// words a graph starts with, in any case as the scanner takes them.
static int bridge_reader_begins_graph(const char *s) {
  static const char *const words[] = {"strict", "digraph", "graph"};

  for (size_t i = 0; i < sizeof(words) / sizeof(words[0]); i++) {
    size_t n = strlen(words[i]);

    if (strncasecmp(s, words[i], n) == 0 && !bridge_reader_names(s[n])) {
      return 1;
    }
  }

  return 0;
}

// bridge_reader_after_graph places the reader for the graph after the one
// just read. What follows a graph on its line is read when it begins a
// graph, past blanks, comments and semicolons, which some write after a
// graph's brace: the semicolons are dropped, since the grammar has none
// there. Anything else on the line is dropped to its end, as Graphviz's dot
// drops it with the rest of the line: a word, a brace too many.
static void bridge_reader_after_graph(bridge_reader_t *reader) {
  const char *text = reader->text;
  size_t at = reader->cur;
  // from is where the reading goes on if a graph begins on this line: past
  // the last semicolon.
  size_t from = reader->cur;

  for (;;) {
    if (text[at] == ' ' || text[at] == '\t' || text[at] == '\r') {
      at++;
      continue;
    }

    if (text[at] == ';') {
      from = ++at;
      continue;
    }

    if (text[at] != '/' || text[at + 1] != '*') {
      break;
    }

    // A comment: past it, unless it runs on to another line or to the end
    // of the text, which the scanner reads as it is.
    const char *end = strstr(text + at + 2, "*/");
    if (end == NULL || memchr(text + at, '\n', (size_t)(end - (text + at))) != NULL) {
      break;
    }

    at = (size_t)(end - text) + 2;
  }

  // Nothing more on the line, a comment the scanner reads, or a graph.
  if (text[at] == '\0' || text[at] == '\n' || text[at] == '#' ||
      (text[at] == '/' && (text[at + 1] == '/' || text[at + 1] == '*')) ||
      bridge_reader_begins_graph(text + at)) {
    reader->cur = from;
    return;
  }

  while (text[at] != '\0' && text[at] != '\n') {
    at++;
  }

  reader->cur = at;
}

// bridge_reader_next is agread at the reader's place: the next graph, or
// NULL when the rest of the text holds none or Graphviz cannot read it,
// which it has then reported.
Agraph_t *bridge_reader_next(bridge_reader_t *reader) {
  reader->directive = NULL;
  reader->in = IN_GRAPH;

  if (reader->cur > 0) {
    bridge_reader_after_graph(reader);

    snprintf(reader->directive_text, sizeof(reader->directive_text),
             "# %zu\n", reader->line);
    reader->directive = reader->directive_text;
  }

  return agread(reader, &reader->disc);
}

void bridge_reader_close(bridge_reader_t *reader) {
  free(reader->text);
  free(reader);
}

// bridge_version is the Graphviz release the module is built from, as
// Graphviz answers it: gvcVersion, of a context made for the question that
// holds nothing else. Not PACKAGE_VERSION read here: the config.h beside
// this file is the template, with the release not filled in.
char *bridge_version(void) {
  GVC_t *gvc = gvNEWcontext(NULL, 0);
  char *version = gvcVersion(gvc);

  free(gvc);

  return version;
}

// bridge_render_data is gvRenderData, and deletes the jobs of a render that
// failed. For a format it does not know, gvRenderData returns with its job
// still on the context, named by the caller's format string, which the
// caller frees: the next render on the context would run that job first,
// and read the freed name. gvRenderData deletes the jobs on its other paths,
// and deleting none is a no-op.
int bridge_render_data(GVC_t *gvc, graph_t *g, const char *format,
                       char **result, unsigned int *length) {
  int ret = gvRenderData(gvc, g, format, result, length);
  if (ret != 0) {
    gvjobs_delete(gvc);
  }
  return ret;
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
