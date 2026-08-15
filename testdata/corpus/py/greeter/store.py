"""Reaching the corpus schema from Python. Corpus fixture: not installed, not run.

The three shapes go/store/store.go's doc comment describes, in Python's spellings: a
triple-quoted literal, an f-string whose table is a parameter, and a log message.
"""

LIST_ORDERS = """
SELECT id, total
FROM orders
WHERE total > %s
"""


def list_orders(cur):
    """Reads one table."""
    cur.execute(LIST_ORDERS, (0,))
    return cur.fetchall()


def record(cur):
    """Writes the table it names."""
    cur.execute("INSERT INTO customers (id) VALUES (%s)", (1,))


def purge(cur, table):
    """The gap. An f-string's `{table}` is not a name in the tree."""
    cur.execute(f"DELETE FROM {table} WHERE total = 0")


def warn(table):
    """Prose. `.format` on a sentence is not a statement with an interpolated name."""
    return "delete from {} returned no rows".format(table)
