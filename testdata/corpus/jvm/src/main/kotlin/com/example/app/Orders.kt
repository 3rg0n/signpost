// Corpus fixture: not compiled, not run.
package com.example.app

// The three shapes go/store/store.go's doc comment describes, in Kotlin's spellings.
//
// Kotlin's raw string is `"""`, the same delimiter as Java's text block one directory over,
// and the two are not the same construct: a text block strips incidental indentation by the
// language's own rule and a raw string keeps every byte until `trimIndent()` is called. That
// difference does not reach this reader — it reads the body either way — and the pair is here
// so the claim is measured rather than assumed.
//
// Interpolation is `$table`, which is a bare sigil rather than a delimited one. It is the
// shortest interpolation form in any language in this corpus, which makes it the hardest to
// tell from a name: `$table` and `table` differ by one character.

val LIST_ORDERS = """
    SELECT id, total
    FROM orders
    WHERE total > ?
"""

/** Reads one table. */
fun listOrders(db: java.sql.Connection) = db.prepareStatement(LIST_ORDERS).executeQuery()

/** Writes the table it names. */
fun record(db: java.sql.Connection) =
    db.prepareStatement("INSERT INTO customers (id) VALUES (?)").executeUpdate()

/** The gap. */
fun purge(db: java.sql.Connection, table: String) =
    db.prepareStatement("DELETE FROM $table WHERE total = 0").executeUpdate()

/** Prose. */
fun warn(): String = "deleted from cache"
