/* Reaching the corpus schema from C. Corpus fixture: not compiled, not run.
 *
 * The three shapes go/store/store.go's doc comment describes, in C's spellings — and C is the
 * language with no multi-line string at all. What it has instead is adjacent string literal
 * concatenation: two literals side by side are one literal, so a long query is written as a
 * column of them, each terminated on its own line. That is a different recovery problem from
 * every other language here. There is no delimiter spanning lines to find; there are four
 * ordinary single-line literals whose text only becomes one statement once the compiler joins
 * them, and this reader does not join them.
 *
 * So the fixture states the honest outcome rather than a hoped-for one: the first fragment
 * holds `SELECT ... FROM orders` and is read, and the fragments after it are clauses that
 * name nothing. A reader that joined them would find the same table twice.
 *
 * The first fragment carries no table alias, and that is a constraint the reader imposes
 * rather than a style preference. A statement ending on `FROM orders o` is two bare words with
 * nothing after them to say the second is an alias — the identical shape to "select the row
 * you want from the list", which must be declined — so a fragment ending there is read as
 * prose. Every other language's store aliases freely, because a JOIN or a WHERE follows and
 * closes the pair; C's fragments end wherever the author broke the line, which is why this is
 * the one file where the limit shows.
 *
 * C has no interpolation either. `sprintf` builds the string and the format lives in a
 * literal, so the gap arrives here as `%s` — the same spelling as Go's and by a different
 * route, since Go's comes from a function this pass reads the argument of and C's from one it
 * does not.
 */

#include <stdio.h>
#include <string.h>

/* Adjacent literals: C's multi-line query. The first fragment carries the table, and it ends
 * on the table rather than on an alias for the reason the file comment gives. */
static const char *list_orders =
    "SELECT o.id, o.total FROM orders "
    "JOIN customers c ON c.id = o.customer_id "
    "WHERE o.total > ?";

/* Writes the table it names. */
static const char *record_customer = "INSERT INTO customers (id) VALUES (?)";

/* The gap: the format string is the literal and the table is the argument. */
int corpus_store_purge(char *buf, size_t n, const char *table)
{
    return snprintf(buf, n, "DELETE FROM %s WHERE total = 0", table);
}

/* Prose. */
const char *corpus_store_warn(void)
{
    return "failed to insert into the queue";
}
