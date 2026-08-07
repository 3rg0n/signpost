/* Corpus fixture: not compiled, not run. */

/* An angled include of the project's own public header, which is ordinary: the header is
 * on the include path because the build puts it there. It resolves against `c/include/`,
 * the include root nearest this file — not against the repository root, which is where a
 * resolver anchored at the top would look and find nothing. */
#include <corpus/buffer.h>

/* A quoted include of a header beside this file: the compiler's first search location. */
#include "internal.h"

/* The runtime, from the standard-header list. */
#include <stdlib.h>
#include <string.h>

/* The near-miss for C include resolution, and the reason the positive assertions above
 * measure anything. `corpus/buffers.h` is one character from the header this file really
 * includes, and nothing in this tree is called that — a resolver matching a prefix, or
 * accepting a directory in place of a file, swallows it into the real header and draws an
 * edge that the source never asked for. It must land in the gap report instead. */
#include <corpus/buffers.h>

/* The other near-miss, on the other side of the stdlib boundary. `<stdlib_extras.h>` opens
 * with the six characters of the standard `stdlib.h` and is a package somebody would have
 * to patch; classified as the runtime by a prefix match, it vanishes from the coverage
 * report entirely, which is the one outcome the report exists to prevent. */
#include <stdlib_extras.h>

/** Grows the buffer to hold at least want bytes. */
int corpus_buffer_grow(CorpusBuffer *b, size_t want)
{
    if (want <= b->cap) {
        return 0;
    }
    /* Every line in this body has a call's shape, which is a declaration's shape minus
     * the return type. None of them is a declaration. */
    void *grown = realloc(b->data, want);
    if (grown == NULL) {
        corpus_internal_note("allocation failed");
        return -1;
    }
    b->data = grown;
    b->cap = want;
    return 0;
}

/** Releases the buffer's storage. */
void corpus_buffer_free(CorpusBuffer *b)
{
    free(b->data);
    memset(b, 0, sizeof *b);
}

/* The struct-returning function whose prototype is in the header, here with a body. The
 * type keyword, the type name and the opening brace are all on one line, so this is the
 * shape a brace-only rule reads as a type definition — reporting a CorpusBuffer defined
 * here, opening a scope that claims everything below as its member, and dropping the
 * function. What separates them is what sits between the name and the brace: a definition
 * allows only `final` and a `:` clause there, a declarator puts its own name there. */
struct CorpusBuffer *corpus_buffer_make(size_t cap) { return NULL; }

/* `static` is the one keyword that removes a symbol from the link surface, which is the
 * inverse of Java's default: absence of a keyword here means external linkage. */
static int corpus_buffer_shrink(CorpusBuffer *b)
{
    b->cap = b->len;
    return 0;
}

/* An attribute carries a parenthesised argument list, and the rules that tell a
 * declaration from a call are all written against the first parenthesis on the line being
 * the *parameter* list. In front of the return type an attribute moves that parenthesis and
 * takes the whole declaration with it — this function vanished entirely. The GNU spelling
 * guards half of any portable header and the MSVC one is how a symbol leaves a DLL, so
 * neither is exotic. */
__attribute__((unused)) static int corpus_buffer_unused(CorpusBuffer *b)
{
    return b == NULL;
}

__declspec(dllexport) int corpus_buffer_exported(CorpusBuffer *b)
{
    return b != NULL;
}

/* The same construct between the keyword and the name breaks a type instead of a function:
 * read without skipping it, the type is named `__attribute__`. An export macro has no
 * parenthesis and fails a third way — it sits where the name is expected, so the type is
 * named after the macro. Neither may consume a name that shouts on its own account, which
 * is how Win32 spells half its structs. */
struct __attribute__((packed)) CorpusPacked {
    int tag;
};

/* A macro invocation at file scope has a declaration's exact shape at a declaration's
 * exact depth, and only the missing return type tells them apart. It is a call, and it is
 * not a symbol. */
CORPUS_MODULE_NOTE("buffer");

/* A function pointer is data. Its declarator puts a name where a function's would be. */
int (*corpus_buffer_hook)(CorpusBuffer *) = NULL;

typedef int (*corpus_buffer_visitor)(CorpusBuffer *, void *);

/* A commented-out declaration contributes nothing:
 *
 * #include "corpus/never.h"
 * int corpus_commented_out(void);
 */

/* A declaration inside a string literal contributes nothing either. */
static const char *CORPUS_SNIPPET =
    "#include \"corpus/in_a_string.h\"\n"
    "int corpus_string_function(void) { return 0; }\n";

/* A braced character literal must not move the brace depth: read as code, it opens a block
 * that never closes and every declaration after it sits too deep to be seen. */
int corpus_buffer_is_open_brace(char c)
{
    char open = '{';
    char close = '}';
    return c == open || c == close;
}
