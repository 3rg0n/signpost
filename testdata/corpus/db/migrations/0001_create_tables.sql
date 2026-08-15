-- Corpus fixture: not applied, not run.
--
-- The schema every language's store in this repository talks to. One set of migrations for
-- thirteen languages, which is what a polyglot repository actually looks like: the database
-- is shared and the code that reaches it is not, so the table is the one page a reader with a
-- data symptom can start from whichever service reported it.

CREATE TABLE orders (
  id UUID PRIMARY KEY,
  customer_id UUID NOT NULL,
  total NUMERIC NOT NULL DEFAULT 0
);

CREATE TABLE customers (
  id UUID PRIMARY KEY,
  -- A semicolon inside a string body, which does not end this statement.
  note TEXT DEFAULT 'created by hand; see #412'
);

CREATE INDEX orders_customer_idx ON orders (customer_id);
