# Reaching the corpus schema from Ruby. Corpus fixture: not installed, not run.
# frozen_string_literal: true

# The three shapes go/store/store.go's doc comment describes, in Ruby's spellings. The squiggly
# heredoc is the multi-line form, `#{}` is the interpolation, and the last method is prose.
module Corpus
  # Reads and writes the corpus schema.
  module Store
    LIST_ORDERS = <<~SQL
      SELECT id, total
      FROM orders
      WHERE total > ?
    SQL

    def self.list_orders(db)
      db.execute(LIST_ORDERS, 0)
    end

    def self.record(db)
      db.execute("INSERT INTO customers (id) VALUES (?)", 1)
    end

    # The gap.
    def self.purge(db, table)
      db.execute("DELETE FROM #{table} WHERE total = 0")
    end

    # Prose.
    def self.warn(table)
      "could not update the #{table} row"
    end
  end
end
