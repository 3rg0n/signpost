/* Corpus fixture: not compiled, not run.
 *
 * A C test, recognised two ways at once: the `tests/` directory and the `_test.c` suffix.
 * C has no test convention its toolchain enforces, so several coexist and this file carries
 * two of them. It is kept — a test is the best evidence of how an interface is meant to be
 * used — and marked, so it never counts as production surface. */

#include <corpus/buffer.h>

#include "unity.h"

void test_buffer_grow_allocates(void)
{
    CorpusBuffer b = {0};
    corpus_buffer_grow(&b, 16);
    corpus_buffer_free(&b);
}

int main(void)
{
    test_buffer_grow_allocates();
    return 0;
}
