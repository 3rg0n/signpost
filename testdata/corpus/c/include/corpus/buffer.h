/* Corpus fixture: not compiled, not run. */
#ifndef CORPUS_BUFFER_H
#define CORPUS_BUFFER_H

/* The runtime. A C standard header ends in .h and is shape-indistinguishable from a
 * project's own, so it is recognised from a list rather than from its form — and it is
 * neither an edge nor a reported gap, because nobody patches libc separately. */
#include <stddef.h>

/* A forward declaration: a promise that the type exists so a pointer to it can be
 * declared. It defines nothing, and recording it as a symbol would claim this header
 * defines a type it deliberately does not. */
struct CorpusArena;

/** A growable byte buffer. */
struct CorpusBuffer {
    char *data;
    size_t len;
    size_t cap;
};

typedef struct CorpusBuffer CorpusBuffer;

/** Grows the buffer to hold at least want bytes.
 *
 * @param b the buffer
 * @param want the required capacity
 * @return 0 on success, -1 on allocation failure
 */
int corpus_buffer_grow(CorpusBuffer *b, size_t want);

/** Releases the buffer's storage. */
void corpus_buffer_free(CorpusBuffer *b);

/* A function returning a pointer to a struct. Its first two tokens are a type keyword and
 * a type name, exactly like a definition, and in the .c file below the body's brace sits
 * on the same line — so a brace is not enough to say a type follows. Read as a definition,
 * this reports a phantom CorpusBuffer defined in the wrong file, opens a scope that claims
 * every later declaration as a member, and loses the function itself. */
struct CorpusBuffer *corpus_buffer_make(size_t cap);

#endif /* CORPUS_BUFFER_H */
