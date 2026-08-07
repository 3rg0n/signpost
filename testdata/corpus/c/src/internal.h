/* Corpus fixture: not compiled, not run. */
#ifndef CORPUS_INTERNAL_H
#define CORPUS_INTERNAL_H

/* A private header beside the code that includes it, which is what the quoted form is for.
 * It is not under include/, so it is not part of the library's public interface. */
void corpus_internal_note(const char *message);

#endif /* CORPUS_INTERNAL_H */
