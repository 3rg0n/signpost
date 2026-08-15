-- Corpus fixture: not applied, not run.
--
-- A table no source in this repository reads or writes. It is the negative boundary on the
-- other side of the pass from an unknown table: a table page with no writer and no reader is
-- the correct outcome for a table nothing touches, and a reader that invented one from a
-- coincidence — a column called `audit_log`, the word appearing in a comment — would be
-- claiming code that does not exist.
CREATE TABLE audit_log (
  id BIGSERIAL PRIMARY KEY,
  actor TEXT NOT NULL
);
