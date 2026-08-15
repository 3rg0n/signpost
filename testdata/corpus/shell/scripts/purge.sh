#!/usr/bin/env bash
# Reaching the corpus schema from a shell script. Corpus fixture: not run.
#
# The three shapes go/store/store.go's doc comment describes, in the shell's spellings.
#
# The shell is where this pass matters most and is trusted least. A `psql -c` in a deploy
# script is real production write access to a table, written by whoever was on call, reviewed
# by nobody, and named in no application's source — and it is also the language whose every
# string is interpolating, so the gap and the edge are one quote form apart. `"$TABLE"` and
# `"orders"` are the same construct with different contents, which is the boundary the two
# functions below sit on.
set -euo pipefail

# A heredoc, which is how a script writes SQL longer than a flag's worth.
list_orders() {
  psql --quiet <<'SQL'
SELECT o.id, o.total
FROM orders o
JOIN customers c ON c.id = o.customer_id
WHERE o.total > 0
SQL
}

# The single-flag form, which is most of the SQL in most scripts.
record() {
  psql --quiet -c "INSERT INTO customers (id) VALUES (1)"
}

# The gap. The table is this script's first argument, so nothing here names it.
purge() {
  psql --quiet -c "DELETE FROM $1 WHERE total = 0"
}

# Prose: a log line whose first three words are exactly an interpolated statement.
warn() {
  echo "insert into $1 failed, retrying" >&2
}

main() {
  list_orders
  record
  purge "${1:?table required}" || warn "$1"
}

main "$@"
