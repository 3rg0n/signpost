// Reaching the corpus schema from C++. Corpus fixture: not compiled, not run.
//
// The three shapes go/store/store.go's doc comment describes, in C++'s spellings — plus a
// fourth that is a stated limitation rather than a shape, and it is the reason this file exists
// beside store.c instead of being assumed from it.
//
// C++ inherits C's adjacent-literal concatenation, and that is what the read query below uses.
// It also adds the raw string literal, `R"delim(...)delim"`, which is what modern C++ actually
// reaches for when embedding SQL — and the scanner does not model it (see scanC's comment in
// internal/extract/lines.go): the delimiter is arbitrary text rather than a run of hashes, so
// there is no rule shared with Rust's `r#"..."#` to lean on.
//
// The fixture pins what that costs, which is a query read as nothing. That is the *acceptable*
// direction — the map is missing an edge — and the assertion is that it stays acceptable: the
// unmodelled literal must produce silence, not a table named after a delimiter and not a gap
// counted against a statement nobody wrote. A raw string's middle lines survive as code, and
// `FROM audit_log` sitting there unquoted is exactly the text a looser reader would mint a
// table page from.

#include <corpus/session.hpp>

#include <string>

namespace corpus {

// Adjacent literals, which C++ shares with C. This is the query that is read, and its first
// fragment ends on the table rather than on an alias for the reason c/src/store.c states: a
// fragment ending `FROM orders o` is two bare words with nothing to mark the second as an
// alias, which is the shape prose has.
const char *kListOrders =
    "SELECT o.id, o.total FROM orders "
    "JOIN customers c ON c.id = o.customer_id "
    "WHERE o.total > ?";

// Writes the table it names.
const char *kRecordCustomer = "INSERT INTO customers (id) VALUES (?)";

// The unmodelled form, and the negative boundary. Every table it names is spelled out, so a
// reader that guessed at raw-string bodies would find them — and would find them in a file
// where the delimiter and the closing parenthesis are also candidates for a name.
const char *kAuditTrail = R"SQL(
SELECT actor
FROM audit_log
WHERE actor IS NOT NULL
)SQL";

// The gap.
std::string purge(const std::string &table)
{
    return "DELETE FROM " + table + " WHERE total = 0";
}

// Prose.
const char *warn() { return "could not update the order"; }

} // namespace corpus
