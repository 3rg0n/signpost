package sqlstmt

import (
	"fmt"
	"strings"
	"testing"
)

// The negative boundary is the whole point of this package's tests.
//
// A reader that finds `INSERT INTO orders` and reports `orders` is easy, and a test that
// only asserts that would pass equally against a reader that reports a table for every
// statement it sees — including `SELECT 1`, including `"DELETE FROM " + name`, including a
// sentence in a comment that happens to contain the word FROM. Those are the readings that
// put a page named `%s` in a committed artifact, and they are what these cases are for.
//
// So every table below has rows on both sides, and the counts are asserted rather than
// only the presence: an assertion that `orders` appears cannot fail when `%s` appears
// beside it.

func TestSplitDoesNotEndAStatementInsideAString(t *testing.T) {
	stmts := Split(`
-- Add an index; see #412.
CREATE TABLE things (
  note TEXT DEFAULT 'a; semicolon inside a string'
);
# A MySQL comment; also not a statement.
/* A block comment; also not one. */
CREATE INDEX things_idx ON things (note);
`)
	if len(stmts) != 2 {
		got := make([]string, 0, len(stmts))
		for _, s := range stmts {
			got = append(got, fmt.Sprintf("%d: %q", s.Line, s.Text))
		}
		t.Fatalf("statements = %d, want 2 — a `;` inside a string body or a comment does "+
			"not end one:\n  %s", len(stmts), strings.Join(got, "\n  "))
	}
	// The line is the statement's first, not the one the semicolon closed on: provenance
	// should point at where a reader will find it.
	if stmts[0].Line != 3 {
		t.Errorf("first statement began on line %d, want 3", stmts[0].Line)
	}
	if stmts[1].Line != 8 {
		t.Errorf("second statement began on line %d, want 8", stmts[1].Line)
	}
}

func TestSplitTreatsADollarQuotedBodyAsContent(t *testing.T) {
	stmts := Split(`CREATE FUNCTION bump() RETURNS trigger AS $$
BEGIN
  UPDATE counters SET n = n + 1;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TABLE counters (n INT);`)
	if len(stmts) != 2 {
		t.Fatalf("statements = %d, want 2 — everything between $$ and $$ is one "+
			"function body, semicolons included: %+v", len(stmts), stmts)
	}
}

// Subject is the migration reader's view: one name per statement, and nothing for a
// statement that is not a schema or row change.
func TestSubjectNamesOnlyWhatAMigrationChanges(t *testing.T) {
	cases := []struct {
		stmt, verb, target string
	}{
		{"CREATE TABLE IF NOT EXISTS public.things (id UUID)", "CREATE", "public.things"},
		{"CREATE UNIQUE INDEX CONCURRENTLY things_idx ON things (name)", "CREATE", "things_idx"},
		{"ALTER TABLE audit.things ADD COLUMN actor TEXT", "ALTER", "audit.things"},
		{"TRUNCATE TABLE sessions", "TRUNCATE", "sessions"},
		{"DROP TABLE old_things", "DROP", "old_things"},
		// Not a schema change and not a row change: no target, and the verb still
		// reported so a caller can see what was declined.
		{"SET statement_timeout = 0", "SET", ""},
		{"BEGIN", "BEGIN", ""},
		{"COMMENT ON TABLE things IS 'a note'", "COMMENT", ""},
		{"SELECT 1", "SELECT", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		verb, target := Subject(c.stmt)
		if verb != c.verb || target != c.target {
			t.Errorf("Subject(%q) = (%q, %q), want (%q, %q)",
				c.stmt, verb, target, c.verb, c.target)
		}
	}
}

func TestDestructiveReadsPastTheLeadingVerb(t *testing.T) {
	cases := map[string]bool{
		"ALTER TABLE things DROP COLUMN legacy_id":    true,
		"DROP TABLE IF EXISTS old_things":             true,
		"TRUNCATE TABLE sessions":                     true,
		"ALTER TABLE things RENAME COLUMN a TO b":     true,
		"DELETE FROM things WHERE created_at < now()": true,
		"CREATE TABLE things (id INT)":                false,
		"ALTER TABLE things ADD COLUMN note TEXT":     false,
		"CREATE INDEX things_idx ON things (name)":    false,
		// A column named for a deletion is not a deletion. This is the case the
		// field-wise search exists for: a substring search over the text says true.
		"ALTER TABLE things ADD COLUMN dropped_at TIMESTAMP": false,
		"ALTER TABLE deleted_things ADD COLUMN note TEXT":    false,
	}
	for stmt, want := range cases {
		verb, _ := Subject(stmt)
		if got := Destructive(stmt, verb); got != want {
			t.Errorf("Destructive(%q) = %v, want %v", stmt, got, want)
		}
	}
}

// Tables is the source reader's view, and the direction is half of the answer: a module
// that reads `orders` and one that writes it are different things to a reader chasing a
// duplicate row.
func TestTablesReadsBothDirections(t *testing.T) {
	cases := []struct {
		stmt string
		want []Reference
	}{
		{"INSERT INTO orders (id) VALUES ($1)", []Reference{{"orders", Writes}}},
		{"UPDATE orders SET total = $1 WHERE id = $2", []Reference{{"orders", Writes}}},
		{"DELETE FROM orders WHERE id = $1", []Reference{{"orders", Writes}}},
		{"SELECT id, total FROM orders WHERE id = $1", []Reference{{"orders", Reads}}},
		{"TRUNCATE TABLE sessions", []Reference{{"sessions", Writes}}},
		// A qualified name keeps its qualifier, as the migration reader's does: two
		// schemas holding a table of one name are two tables.
		{"SELECT 1 FROM public.orders", []Reference{{"public.orders", Reads}}},
		// A join reads every table it names.
		{
			"SELECT o.id FROM orders o JOIN customers c ON c.id = o.customer_id",
			[]Reference{{"orders", Reads}, {"customers", Reads}},
		},
		// The one statement where both directions are in one place. FROM introduces the
		// write target for a DELETE and a read for everything else, so a rule that read
		// FROM one way would be wrong about one of these two.
		{
			"INSERT INTO archive SELECT * FROM orders WHERE created_at < now()",
			[]Reference{{"archive", Writes}, {"orders", Reads}},
		},
		// A statement may read the table it writes, and both facts survive.
		{
			"UPDATE counters SET n = (SELECT count(*) FROM orders)",
			[]Reference{{"counters", Writes}, {"orders", Reads}},
		},
		// Names nothing and needed nothing. Distinct from the gap cases below, and the
		// distinction is what keeps the gap count meaningful.
		{"SELECT 1", nil},
		{"SET statement_timeout = 0", nil},
		{"BEGIN", nil},
		{"", nil},
	}
	for _, c := range cases {
		got, gap := Tables(c.stmt)
		if gap {
			t.Errorf("Tables(%q) reported a gap; every name here is spelled out", c.stmt)
		}
		if !sameRefs(got, c.want) {
			t.Errorf("Tables(%q) = %v, want %v", c.stmt, got, c.want)
		}
	}
}

// The negative boundary ADR 0034 turns on: a name the source assembles at run time is a
// gap, reported as one, and never a table.
//
// Each row is how one language spells interpolation, because that is how this fails in
// practice — the Go fixture passes and the Python one silently mints a page called
// `{table}`. Asserting the gap flag as well as the empty result is what makes a reader
// that returns nothing at all fail here: silently dropping these would report a module
// with six tables as having four, which is §4.2's forbidden failure.
func TestAnInterpolatedTableNameIsAGapAndNotATable(t *testing.T) {
	cases := []string{
		// Go: fmt.Sprintf, and a concatenation whose literal half simply stops.
		"SELECT * FROM %s WHERE id = $1",
		"INSERT INTO %s (id) VALUES ($1)",
		"DELETE FROM ",
		"INSERT INTO ",
		"UPDATE ",
		// Python: an f-string and .format.
		"SELECT * FROM {table}",
		"UPDATE {} SET n = 1",
		// Ruby and shell.
		"SELECT * FROM #{table}",
		"DELETE FROM $TABLE",
		// A driver placeholder where a name belongs.
		"SELECT * FROM ? WHERE id = ?",
		"INSERT INTO $1 (id) VALUES ($2)",
		// A clause word where the name belongs: the statement is malformed or its
		// literal ended mid-clause, and neither WHERE nor SELECT is a table.
		"SELECT a FROM WHERE id = 1",
		"INSERT INTO SELECT * FROM orders",
	}
	for _, stmt := range cases {
		got, gap := Tables(stmt)
		for _, r := range got {
			if !validName(r.Table) {
				t.Errorf("Tables(%q) returned %q as a table. A name the source builds at "+
					"run time is not in the tree, and reporting one puts formatting syntax "+
					"on the map as a page", stmt, r.Table)
			}
		}
		if !gap {
			t.Errorf("Tables(%q) reported no gap. The statement needs a table name and the "+
				"text does not have one, so it must be counted — a reader dropping it "+
				"silently reports fewer tables than the module touches, with nothing "+
				"saying so", stmt)
		}
	}
}

// The other half of the same boundary, and the reason the gap flag is not simply
// "returned nothing": a statement that names no table because it needs none is not a gap,
// and conflating the two would make the count a number nobody could act on.
func TestAStatementThatNeedsNoTableIsNotAGap(t *testing.T) {
	for _, stmt := range []string{
		"SELECT 1",
		"SELECT now()",
		"BEGIN",
		"COMMIT",
		"SET statement_timeout = 0",
		"VACUUM",
		"",
	} {
		got, gap := Tables(stmt)
		if gap {
			t.Errorf("Tables(%q) reported a gap. Nothing here needs a table name, and "+
				"counting it would inflate the gap report with statements that are "+
				"complete as written", stmt)
		}
		if len(got) != 0 {
			t.Errorf("Tables(%q) = %v, want none", stmt, got)
		}
	}
}

// A subquery in a FROM is not a gap either. The inner statement's tables are unreachable
// from this call — the caller splits on `;` and a subquery has none — so what matters is
// that the parenthesis does not become a table and does not become a gap.
// LooksLikeSQL is the gate that keeps prose out, and it is where a sentence mentioning a
// verb is declined. Tables deliberately does not do this — it is given SQL and answers
// about SQL — so this is the only place the distinction is made, and it is the one a
// string-literal reader depends on.
func TestLooksLikeSQLDeclinesProseThatMentionsAVerb(t *testing.T) {
	yes := []string{
		"SELECT id, total FROM orders WHERE id = $1",
		"select id from orders",
		"INSERT INTO orders (id) VALUES ($1)",
		"UPDATE orders SET total = $1",
		"DELETE FROM orders WHERE id = $1",
		"SELECT count(*) AS n FROM orders",
		// The literal half of a concatenation. Still a query, still readable as one, and
		// its missing name is what Tables reports as a gap.
		"SELECT * FROM ",
		// Two tables. A rule counting bare words across the whole clause rather than
		// adjacent ones rejected every one of these, so a repository's joins were absent
		// from the map and no gap counted them either — the quietest of the two failure
		// directions, because the output looks complete.
		"SELECT id FROM orders JOIN customers ON true",
		"SELECT o.id FROM orders o JOIN customers c ON c.id = o.customer_id",
		"SELECT id FROM orders LEFT JOIN customers USING id",
		// A clause after the table, which is most of what a real query is made of, and
		// two of these end on a bare word.
		"DELETE FROM orders WHERE done",
		"SELECT id FROM orders ORDER BY created_at",
		"UPDATE orders SET total = 0 RETURNING id",
		// An interpolated name with a clause after it. Its gap is what the coverage
		// report counts, so declining it as prose loses the count as well as the edge.
		"DELETE FROM $TABLE WHERE done",
		"SELECT id FROM %s WHERE done",
		// An aliased write target, which is the shape the required keyword itself has to
		// close. The SET is the only field in this statement saying `o` is an alias rather
		// than a second bare word, so cutting the keyword out between the two clauses left
		// the left one ending on an unclosed two-run — and every aliased UPDATE and DELETE
		// was declined as prose, drawing neither an edge nor a gap.
		"UPDATE orders o SET total = 0",
		"DELETE FROM orders o USING customers c WHERE c.id = o.id",
	}
	no := []string{
		// The sentences that a search-anywhere rule accepts. The second one names a table
		// called "the".
		"could not update the order",
		"select the row you want from the list",
		"failed to insert into the queue: %w",
		"deleted from cache",
		// A real query naming no table. Declined here because there is nothing to report,
		// which keeps the gap count free of statements that are complete as written.
		"SELECT 1",
		"SELECT now()",
		// The literals a source file is mostly made of.
		"", "orders", "application/json", "https://example.invalid/orders",
		"%s/%s", "no rows in result set",
		// DDL in source is a fixture or an embedded schema, and a migration is where a
		// schema change is a fact about the repository.
		"CREATE TABLE orders (id INT)",
		"DROP TABLE orders",
		"ALTER TABLE orders ADD COLUMN note TEXT",
		// A verb with its required keyword missing: the syntax is not there, so this is
		// prose that happens to start with the word.
		"update your credentials before continuing",
		"insert the card and wait",
		// The error messages whose first words are exactly an interpolated statement. What
		// follows the keyword decides these, which is why the rule is applied to both sides:
		// `%s failed: %w` is prose, and this is the shape that inflates the gap count rather
		// than the table list, so nothing in the output would look wrong.
		"insert into %s failed: %w",
		"delete from %s returned no rows",
		"select from the wrong replica again",
		// Prose with more than two bare words before the keyword, which is what the
		// column-list rule is measuring.
		"delete every stale entry from the queue",
		"select whichever candidate scores best from among these",
		// The negative side of the adjacency rule, and the boundary it sits on. Two bare
		// words at the end of a clause is prose: nothing marks the second as an alias, and
		// a queue named "the" is what a looser rule reports.
		"delete from the queue",
		"insert into the outbox table",
		// AND joins expressions, so a bare word on either side of it is a sentence rather
		// than a predicate — the one keyword that would otherwise break up a run of prose.
		"insert into cache and retry",
		"select from replica and hope",
		// The boundary on the fix above. The required keyword closes the left clause, and
		// these two are why it only closes the *aliased* shape: INTO and FROM are noise
		// words rather than clause introducers, so a two-run of prose standing before
		// either of them is still prose. Widen isSeparator to cover them and both of these
		// become queries against a table called "the".
		"delete from the orders",
		"insert into the customers",
	}
	for _, s := range yes {
		if !LooksLikeSQL(s) {
			t.Errorf("LooksLikeSQL(%q) = false, want true", s)
		}
	}
	for _, s := range no {
		if LooksLikeSQL(s) {
			t.Errorf("LooksLikeSQL(%q) = true. A string literal in source is prose far more "+
				"often than it is a query, and accepting one puts a table on the map that no "+
				"file names", s)
		}
	}
}

// TestProseClosedByAPunctuationMarkIsCountedAsAGap pins a limitation rather than a
// behaviour, and it is here because the alternative is finding it again from a number.
//
// The adjacency rule asks what closes a run of two bare words, and a comma closes one. That
// is not a bug in the rule — `FROM orders o, customers c` is exactly that shape and is the
// oldest way SQL writes a join — but it means a sentence whose second and third words are
// followed by a comma is indistinguishable from an aliased name followed by one. So
// "insert into ${table} failed, retrying" is accepted, and since its name is interpolated it
// is counted as a gap: a query signpost could not resolve, which no query is.
//
// The cost is bounded and it is the cheaper direction. No table is drawn, because there is no
// literal name to draw — the failure is confined to the number in the coverage report, and it
// moves that number *up*, so a reader is told more of the map is missing than really is. The
// expensive direction, a table named `failed`, is closed by validName and by the interpolation
// check, and both of those are asserted above.
//
// Fixing it needs something this package does not have. `into ${table} failed,` and
// `FROM orders o,` are the same grammar; separating them means knowing that `retrying` is not
// a column and `failed` is not an alias, which is a lexicon rather than a rule, and a lexicon
// of English function words is what the whole gate was written to avoid. The five prose
// fixtures in the corpus that end their clause differently — a colon, a period, a bare word —
// are all correctly declined, which is what makes the comma the boundary rather than the rule.
//
// Asserted rather than commented so the day it is fixed this test fails and says so. A
// limitation nobody wrote down is rediscovered from a gap count that is two too high, which is
// the most expensive way to learn it.
func TestProseClosedByAPunctuationMarkIsCountedAsAGap(t *testing.T) {
	// The two spellings in the corpus: TypeScript's template literal and the shell's `$1`,
	// both a log line, both with a comma after the third word.
	for _, s := range []string{
		"insert into ${table} failed, retrying",
		"insert into $1 failed, retrying",
	} {
		if !LooksLikeSQL(s) {
			t.Errorf("LooksLikeSQL(%q) = false. This is the known limitation above, and the "+
				"assertion is inverted on purpose: if the gate now declines it, the comma case "+
				"has been fixed and the corpus's gap count is two lower than the stage in "+
				"cmd/signpost asserts. Update that count and delete this test", s)
			continue
		}
		got, gap := Tables(s)
		// The bound on the cost. It may be counted; it may not name a table.
		if len(got) != 0 {
			t.Errorf("Tables(%q) = %v. The limitation is a gap counted against prose, and it "+
				"has to stay that: a table named after a word in a sentence is a page in a "+
				"committed artifact naming something the database does not have", s, got)
		}
		if !gap {
			t.Errorf("Tables(%q) reported no gap, so this sentence is accepted by the gate and "+
				"then reported as a statement that names no table at all — which is the one "+
				"outcome with no number behind it", s)
		}
	}
}

// A FROM inside a function call is a preposition, not a table source. SQL has three of
// these forms and every one of them names a column where this reader looks for a table.
func TestAFromInsideAFunctionCallIsNotATableSource(t *testing.T) {
	cases := []struct {
		stmt string
		want []Reference
	}{
		{
			"SELECT EXTRACT(DAY FROM created_at) FROM orders",
			[]Reference{{"orders", Reads}},
		},
		{
			"SELECT SUBSTRING(note FROM 1 FOR 4) FROM orders",
			[]Reference{{"orders", Reads}},
		},
		{
			"SELECT TRIM(BOTH ' ' FROM note) FROM orders",
			[]Reference{{"orders", Reads}},
		},
		// And the case the rule must not break: a FROM inside a subquery's parentheses
		// *is* a table source, so the two are told apart by what opened the paren.
		{
			"SELECT n FROM orders WHERE id IN (SELECT id FROM archive)",
			[]Reference{{"orders", Reads}, {"archive", Reads}},
		},
	}
	for _, c := range cases {
		got, gap := Tables(c.stmt)
		if gap {
			t.Errorf("Tables(%q) reported a gap", c.stmt)
		}
		if !sameRefs(got, c.want) {
			t.Errorf("Tables(%q) = %v, want %v — a column named after a preposition is "+
				"not a table", c.stmt, got, c.want)
		}
	}
}

func TestASubqueryInAFromIsNeitherATableNorAGap(t *testing.T) {
	const stmt = "SELECT n FROM (SELECT count(*) AS n FROM orders) t"
	got, gap := Tables(stmt)
	if gap {
		t.Errorf("Tables(%q) reported a gap", stmt)
	}
	for _, r := range got {
		if r.Table == "t" || strings.HasPrefix(r.Table, "(") {
			t.Errorf("Tables(%q) returned %q. An alias and a parenthesis are not tables",
				stmt, r.Table)
		}
	}
}

func TestValidNameRejectsEverythingThatIsNotAnIdentifier(t *testing.T) {
	valid := []string{"orders", "public.orders", "order_items", "a$b", "T1", "_private"}
	invalid := []string{
		"", "%s", "{table}", "#{t}", "$TABLE", "$1", "?", "1", "10",
		// A keyword reached through a malformed statement.
		"WHERE", "SELECT", "FROM", "TABLE",
		// More than one dot is a qualification this reader cannot place, and a
		// leading or trailing dot is not a name at all.
		"a.b.c", ".orders", "orders.",
		// Characters no identifier has, each of which appears in some language's
		// interpolation syntax.
		"or-ders", "or ders", "%s.orders", "orders%s", "{a}.{b}", "@table", ":table",
	}
	for _, n := range valid {
		if !validName(n) {
			t.Errorf("validName(%q) = false, want true", n)
		}
	}
	for _, n := range invalid {
		if validName(n) {
			t.Errorf("validName(%q) = true. The whitelist is closed on purpose — each "+
				"language spells interpolation differently, and a rule listing bad "+
				"characters would pass the next one silently", n)
		}
	}
}

// Tables is deterministic in its order, which the bundle depends on: CI commits the
// output, so a reader whose references came back in a different order on two runs would
// produce a diff on every build.
func TestTablesReturnsTheStatementsOwnOrder(t *testing.T) {
	const stmt = "INSERT INTO archive SELECT * FROM orders JOIN customers ON true"
	first, _ := Tables(stmt)
	for i := 0; i < 20; i++ {
		got, _ := Tables(stmt)
		if !sameRefs(got, first) {
			t.Fatalf("Tables(%q) = %v on run %d, %v on the first", stmt, got, i, first)
		}
	}
	want := []Reference{{"archive", Writes}, {"orders", Reads}, {"customers", Reads}}
	if !sameRefs(first, want) {
		t.Errorf("Tables(%q) = %v, want %v — the order is the statement's", stmt, first, want)
	}
}

// A table named twice in one statement is one reference per direction, so a self-join does
// not weight an edge twice for what the file said once.
func TestARepeatedNameIsOneReference(t *testing.T) {
	const stmt = "SELECT a.id FROM orders a JOIN orders b ON a.parent = b.id"
	got, _ := Tables(stmt)
	if !sameRefs(got, []Reference{{"orders", Reads}}) {
		t.Errorf("Tables(%q) = %v, want one read of orders", stmt, got)
	}
}

func sameRefs(a, b []Reference) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
