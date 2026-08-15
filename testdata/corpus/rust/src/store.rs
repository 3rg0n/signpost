//! Reaching the corpus schema from Rust. Corpus fixture: not compiled, not run.
//!
//! The three shapes `go/store/store.go`'s doc comment describes, in Rust's spellings.
//!
//! Rust's multi-line form is the one that is not a distinct construct at all: an ordinary
//! `"..."` spans lines, so nothing in the syntax marks a long query as long. The raw form
//! `r#"..."#` exists for a different reason — escaping — and a query with a `\` in a LIKE
//! pattern is where it earns its place. Both are here, because the scanner treats them
//! differently and only one of them ends at the line's end when it is left unterminated.

/// A query in an ordinary string spanning lines, which is Rust's plainest form.
pub const LIST_ORDERS: &str = "
SELECT id, total
FROM orders
WHERE total > $1
";

/// The raw form, holding the backslash that is the reason to reach for it.
pub const FIND_CUSTOMER: &str = r#"SELECT id FROM customers WHERE note LIKE '%\_%'"#;

/// Writes the table it names.
pub fn record() -> &'static str {
    "INSERT INTO customers (id) VALUES ($1)"
}

/// The gap. `format!` builds the name, so the literal holds a placeholder and not a table.
pub fn purge(table: &str) -> String {
    format!("DELETE FROM {} WHERE total = 0", table)
}

/// Prose.
pub fn warn() -> &'static str {
    "select the row you want from the list"
}
