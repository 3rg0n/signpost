// Package store reaches the corpus schema from Go. Corpus fixture: not compiled, not run.
//
// Every language's store in this repository holds the same three shapes, because the pass that
// reads them is one implementation and the recovery of a string literal is twelve — eleven
// scanner configurations plus Go's parser. A fixture in one language proves the reader; a
// fixture per recovery path proves the recovery, which is why there are twelve stores and not
// fifteen: C, C++ and Objective-C share one scanner (ADR 0022), as TypeScript and JavaScript do.
//
//   - A statement whose table is spelled out, in that language's idiomatic multi-line string.
//     That is the edge.
//   - A statement whose table the language interpolates in its own syntax. That is the gap,
//     counted and never guessed at (ADR 0034).
//   - A string that mentions a verb and is prose. That is neither, and it is the one that has
//     to produce silence: counting it inflates the number a reader uses to judge how much was
//     missed, and nothing in the output would look wrong.
package store

import (
	"database/sql"
	"fmt"
)

// A literal table name in a raw string, which is how Go writes a query longer than a clause.
const listOrders = `
SELECT o.id, o.total
FROM orders o
JOIN customers c ON c.id = o.customer_id
WHERE o.total > $1
`

// List reads two tables and writes none.
func List(db *sql.DB) error {
	_, err := db.Query(listOrders, 0)
	return err
}

// Record writes the table it names.
func Record(db *sql.DB) error {
	_, err := db.Exec("INSERT INTO orders (id, customer_id) VALUES ($1, $2)", 1, 2)
	return err
}

// Purge's table is the caller's. Resolving it needs the call graph ADR 0022 says this project
// does not have, so it is counted and no edge is drawn.
func Purge(db *sql.DB, table string) error {
	_, err := db.Exec(fmt.Sprintf("DELETE FROM %s WHERE total = 0", table))
	return err
}

// Fail's message is prose whose first three words are exactly an interpolated INSERT. Read as
// a statement it reports a gap that does not exist.
func Fail() error {
	return fmt.Errorf("insert into %s failed: %w", "orders", sql.ErrNoRows)
}
