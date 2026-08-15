// Corpus fixture: not compiled, not run.
package com.example.store;

import java.sql.Connection;
import java.sql.SQLException;

/**
 * The three shapes go/store/store.go's doc comment describes, in Java's spellings.
 *
 * <p>Java is the one language here whose interpolation is not interpolation. It has no
 * f-string and no template literal, so a table name assembled at run time is written with
 * `+` — and by the time the scanner sees the literal, the operator and the variable are
 * outside it. What reaches the reader is `"DELETE FROM "`, a statement whose text simply
 * stops where the name belongs. It is the same gap as Python's `{table}` and it arrives by a
 * different route, which is why the fixture is here rather than assumed from the Python one.
 */
public class Orders {
    /** A text block, which is how Java writes a query longer than a clause since 15. */
    static final String LIST = """
            SELECT o.id, o.total
            FROM orders o
            JOIN customers c ON c.id = o.customer_id
            WHERE o.total > ?
            """;

    /** Reads two tables. */
    public void list(Connection db) throws SQLException {
        db.prepareStatement(LIST).executeQuery();
    }

    /** Writes the table it names. */
    public void record(Connection db) throws SQLException {
        db.prepareStatement("INSERT INTO customers (id) VALUES (?)").executeUpdate();
    }

    /** The gap, and Java's own spelling of it: the literal ends where the name begins. */
    public void purge(Connection db, String table) throws SQLException {
        db.prepareStatement("DELETE FROM " + table + " WHERE total = 0").executeUpdate();
    }

    /** Prose. */
    public String warn() {
        return "failed to insert into the queue";
    }
}
