package extract

import (
	"fmt"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
	"github.com/3rg0n/signpost/internal/sqlstmt"
)

// Both boundaries, per language, and the negative one is the reason this file is long.
//
// A reader that finds `INSERT INTO orders` in a Go string is easy to write and easy to test
// into a false pass: assert that `orders` appears and the test is equally happy with a
// reader that reports a table for every string in the file. What that reader actually does
// is put a page named `%s` in a committed artifact and link a module to it, or read the
// word "from" in an error message and report a table called "the".
//
// So every language below has a fixture with a spelled-out table name *and* one whose name
// the source assembles at run time, in that language's own interpolation syntax, plus prose
// that mentions a verb. The count is asserted rather than the presence, because an
// assertion that `orders` was found cannot fail when `%s` was found beside it.

// queriesOf extracts a file and returns its queries and gap count.
func queriesOf(t *testing.T, lang discover.Lang, path, src string) ([]Query, int) {
	t.Helper()
	e := DefaultRegistry().For(lang)
	if e == nil {
		t.Fatalf("no extractor registered for %s", lang)
	}
	facts, err := e.Extract(discover.File{Path: path, Lang: lang, Content: src})
	if err != nil {
		t.Fatalf("extract %s: %v", path, err)
	}
	facts.Normalize()
	return facts.Queries, facts.UnnamedQueries
}

// refs renders queries for a failure message and for comparison, dropping the line so a
// fixture can be edited without renumbering every expectation.
func refs(qs []Query) []string {
	out := make([]string, 0, len(qs))
	for _, q := range qs {
		out = append(out, fmt.Sprintf("%s %s", q.Access, q.Table))
	}
	return out
}

func sameStrings(a, b []string) bool {
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

// The per-language table. Each source holds, deliberately:
//
//   - a literal query whose table is spelled out, in that language's most idiomatic way of
//     holding a multi-line string, since that is how SQL longer than a clause is written;
//   - a query whose table name the language interpolates, which must be counted and must
//     not become a table;
//   - at least one string that mentions a SQL verb and is not SQL.
//
// wantGaps is asserted as a number rather than "at least one" because the difference
// between one gap and three is the difference between a reader that noticed the
// interpolation and one that also silently dropped two literal queries.
func TestSQLIsReadOutOfEveryLanguageThatCanHoldIt(t *testing.T) {
	cases := []struct {
		name     string
		lang     discover.Lang
		path     string
		src      string
		want     []string
		wantGaps int
	}{
		{
			name: "go raw string and Sprintf",
			lang: discover.LangGo,
			path: "store/orders.go",
			src: "package store\n\nimport \"fmt\"\n\n" +
				"const listOrders = `\nSELECT id, total\nFROM orders\nWHERE customer_id = $1\n`\n\n" +
				"func purge(t string) string {\n" +
				"\treturn fmt.Sprintf(\"DELETE FROM %s WHERE created_at < now()\", t)\n" +
				"}\n\n" +
				"const errMsg = \"could not update the order\"\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
		{
			name: "python triple-quoted and f-string",
			lang: discover.LangPython,
			path: "store/orders.py",
			src: "LIST = \"\"\"\nSELECT id, total\nFROM orders\nWHERE id = %s\n\"\"\"\n\n" +
				"def purge(table):\n" +
				"    return f\"DELETE FROM {table} WHERE created_at < now()\"\n\n" +
				"LOG = \"failed to insert into the queue\"\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
		{
			name: "typescript template literal",
			lang: discover.LangTS,
			path: "store/orders.ts",
			src: "const list = `\n  SELECT id, total\n  FROM orders\n  WHERE id = $1\n`;\n\n" +
				"function purge(table: string) {\n" +
				"  return `DELETE FROM ${table}`;\n" +
				"}\n\n" +
				"const msg = \"select the row you want from the list\";\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
		{
			name: "ruby heredoc",
			lang: discover.LangRuby,
			path: "store/orders.rb",
			src: "LIST = <<~SQL\n  SELECT id, total\n  FROM orders\n  WHERE id = $1\nSQL\n\n" +
				"def purge(table)\n" +
				"  \"DELETE FROM #{table}\"\n" +
				"end\n\n" +
				"MSG = 'deleted from cache'\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
		{
			name: "php heredoc",
			lang: discover.LangPHP,
			path: "store/orders.php",
			src: "<?php\n$list = <<<SQL\nSELECT id, total\nFROM orders\nWHERE id = ?\nSQL;\n\n" +
				"function purge($table) {\n" +
				"    return \"DELETE FROM {$table}\";\n" +
				"}\n\n" +
				"$msg = 'could not update the order';\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
		{
			name: "java text block",
			lang: discover.LangJava,
			path: "store/Orders.java",
			src: "package store;\n\nclass Orders {\n" +
				"  static final String LIST = \"\"\"\n" +
				"      SELECT id, total\n      FROM orders\n      WHERE id = ?\n" +
				"      \"\"\";\n" +
				"  String purge(String table) {\n" +
				"    return \"DELETE FROM \" + table;\n" +
				"  }\n" +
				"  static final String MSG = \"failed to insert into the queue\";\n" +
				"}\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
		{
			name: "kotlin raw string",
			lang: discover.LangKotlin,
			path: "store/Orders.kt",
			src: "package store\n\n" +
				"val LIST = \"\"\"\n  SELECT id, total\n  FROM orders\n  WHERE id = ?\n\"\"\"\n\n" +
				"fun purge(table: String) = \"DELETE FROM $table\"\n\n" +
				"val MSG = \"deleted from cache\"\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
		{
			name: "csharp verbatim string",
			lang: discover.LangCSharp,
			path: "Store/Orders.cs",
			src: "namespace Store;\n\nclass Orders {\n" +
				"  const string List = @\"\nSELECT id, total\nFROM orders\nWHERE id = @id\n\";\n" +
				"  string Purge(string table) => $\"DELETE FROM {table}\";\n" +
				"  const string Msg = \"could not update the order\";\n" +
				"}\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
		{
			// C#'s two one-line forms, both of which the scanner blanks whole: the verbatim
			// `@"` and the raw `"""`. The same defect as Rust's raw string reached by two
			// more spellings, and C# is where it is most likely to bite — a verbatim string
			// on one line is the ordinary way to write a query with a Windows path or a
			// regex in it.
			name: "csharp one-line verbatim and raw strings",
			lang: discover.LangCSharp,
			path: "Store/Raw.cs",
			src: "namespace Store;\n\nclass Raw {\n" +
				"  const string Find = @\"SELECT id FROM customers WHERE note LIKE '%\\_%'\";\n" +
				"  const string List = \"\"\"SELECT id FROM orders WHERE id = @id\"\"\";\n" +
				"  string Purge(string table) => $\"DELETE FROM {table}\";\n" +
				"}\n",
			want:     []string{"reads customers", "reads orders"},
			wantGaps: 1,
		},
		{
			name: "rust multi-line string",
			lang: discover.LangRust,
			path: "src/store.rs",
			src: "pub const LIST: &str = \"\nSELECT id, total\nFROM orders\nWHERE id = $1\n\";\n\n" +
				"pub fn purge(table: &str) -> String {\n" +
				"    format!(\"DELETE FROM {} WHERE created_at < now()\", table)\n" +
				"}\n\n" +
				"const MSG: &str = \"select the row you want from the list\";\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
		{
			// Rust's raw string, which is the form a query with a backslash in a LIKE
			// pattern has to use — and the form that was read as no literal at all until
			// the scanner began recording the spans of the literals it blanks whole. Both
			// tables here are spelled out, so a reader that misses the raw one reports the
			// module as touching `orders` alone and nothing says a query went unread.
			name: "rust raw string on one line",
			lang: discover.LangRust,
			path: "src/raw.rs",
			src: "pub const FIND: &str = r#\"SELECT id FROM customers WHERE note LIKE '%\\_%'\"#;\n" +
				"pub const LIST: &str = \"SELECT id FROM orders WHERE id = $1\";\n" +
				"pub fn purge(t: &str) -> String { format!(\"DELETE FROM {} WHERE id = $1\", t) }\n",
			want:     []string{"reads customers", "reads orders"},
			wantGaps: 1,
		},
		{
			name: "powershell here-string",
			lang: discover.LangPowerShell,
			path: "store/Orders.ps1",
			src: "$list = @\"\nSELECT id, total\nFROM orders\nWHERE id = @id\n\"@\n\n" +
				"function Purge($table) {\n" +
				"  \"DELETE FROM $table\"\n" +
				"}\n\n" +
				"$msg = 'deleted from cache'\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
		{
			name: "shell",
			lang: discover.LangShell,
			path: "scripts/purge.sh",
			src: "#!/usr/bin/env bash\n" +
				"psql -c \"SELECT id, total FROM orders WHERE id = 1\"\n" +
				"psql -c \"DELETE FROM $TABLE\"\n" +
				"echo 'could not update the order'\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
		{
			name: "c",
			lang: discover.LangC,
			path: "src/store.c",
			src: "#include <stdio.h>\n\n" +
				"static const char *list = \"SELECT id, total FROM orders WHERE id = ?\";\n" +
				"static const char *purge = \"DELETE FROM \";\n" +
				"static const char *msg = \"failed to insert into the queue\";\n",
			want:     []string{"reads orders"},
			wantGaps: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, gaps := queriesOf(t, c.lang, c.path, c.src)
			if !sameStrings(refs(got), c.want) {
				t.Errorf("queries = %v, want %v", refs(got), c.want)
			}
			if gaps != c.wantGaps {
				t.Errorf("unnamed queries = %d, want %d. A table name the source builds at run "+
					"time is counted rather than resolved (ADR 0034), and a count that is too "+
					"low means a query was dropped with nothing saying so", gaps, c.wantGaps)
			}
		})
	}
}

// A comment is not a query, in any language. This is the case the scanner already handles
// and the reader inherits — so the assertion is that the inheritance is real, since a
// reader working from Raw without consulting Text would find every one of these.
func TestSQLInACommentIsNotRead(t *testing.T) {
	cases := []struct {
		lang discover.Lang
		path string
		src  string
	}{
		{
			discover.LangGo, "a.go",
			"package a\n\n// SELECT id FROM orders\n/* DELETE FROM sessions */\n",
		},
		{
			discover.LangPython, "a.py",
			"# SELECT id FROM orders\nx = 1  # DELETE FROM sessions\n",
		},
		{
			discover.LangTS, "a.ts",
			"// SELECT id FROM orders\n/* DELETE FROM sessions */\nconst x = 1;\n",
		},
		{
			discover.LangRuby, "a.rb",
			"# SELECT id FROM orders\n=begin\nDELETE FROM sessions\n=end\nx = 1\n",
		},
		{
			discover.LangShell, "a.sh",
			"#!/bin/sh\n# SELECT id FROM orders\n# DELETE FROM sessions\n",
		},
	}
	for _, c := range cases {
		got, gaps := queriesOf(t, c.lang, c.path, c.src)
		if len(got) != 0 || gaps != 0 {
			t.Errorf("%s: queries = %v, gaps = %d, want none of either. A commented-out "+
				"query is a query the repository does not run, and reporting one draws an "+
				"edge to a table this module never touches", c.lang, refs(got), gaps)
		}
	}
}

// A docstring is prose, and prose is where the word "from" lives. Python's is the case
// worth its own test: a triple-quoted string is both how a docstring is written and how a
// multi-line query is written, so the two are told apart by the text and by nothing else.
func TestADocstringMentioningAVerbIsNotAQuery(t *testing.T) {
	const src = `"""Order storage.

Reads orders from the database and can delete every stale entry from the queue.
Callers should update the summary afterwards.
"""

QUERY = """
SELECT id FROM orders
"""
`
	got, gaps := queriesOf(t, discover.LangPython, "store.py", src)
	if !sameStrings(refs(got), []string{"reads orders"}) {
		t.Errorf("queries = %v, want just the real one. The docstring's sentences begin with "+
			"SQL verbs and contain the keyword, which is what the column-list rule is for",
			refs(got))
	}
	if gaps != 0 {
		t.Errorf("unnamed queries = %d, want 0", gaps)
	}
}

// Two statements in one literal are two facts. A transaction written as one string is
// ordinary, and reading only the first statement would miss the write in every one of them.
func TestTwoStatementsInOneLiteralAreBothRead(t *testing.T) {
	const src = "package a\n\nconst tx = `\nDELETE FROM stale_orders;\nINSERT INTO orders (id) " +
		"SELECT id FROM staging;\n`\n"
	got, gaps := queriesOf(t, discover.LangGo, "a.go", src)
	// Sorted by table, so the two writes are not adjacent. That is Normalize's rule and not
	// an accident: sorting by access would make the order depend on which statement came
	// first, which is what the bundle's diff must not do.
	want := []string{"writes orders", "reads staging", "writes stale_orders"}
	if !sameStrings(refs(got), want) {
		t.Errorf("queries = %v, want %v", refs(got), want)
	}
	if gaps != 0 {
		t.Errorf("unnamed queries = %d, want 0", gaps)
	}
}

// The literal a heredoc's opener shares its line with is still read. `db.exec("SELECT 1",
// <<~SQL)` is how a heredoc is passed as an argument, and a reader that took the multi-line
// literal and stopped would drop whatever came before it on that line.
func TestALiteralBesideAHeredocOpenerIsStillRead(t *testing.T) {
	const src = "db.exec(\"SELECT id FROM customers\", <<~SQL)\n" +
		"  UPDATE orders SET total = 0\nSQL\n"
	got, gaps := queriesOf(t, discover.LangRuby, "a.rb", src)
	want := []string{"reads customers", "writes orders"}
	if !sameStrings(refs(got), want) {
		t.Errorf("queries = %v, want %v", refs(got), want)
	}
	if gaps != 0 {
		t.Errorf("unnamed queries = %d, want 0", gaps)
	}
}

// The code after a here-string's terminator is code, and a query in it is a query. This is
// the boundary the scanner's own terminator rule exists for, read from the other side.
func TestCodeAfterAHereStringTerminatorIsStillScanned(t *testing.T) {
	const src = "$body = @\"\n{\"a\":1}\n\"@ -join ''\n" +
		"$q = 'SELECT id FROM orders'\n"
	got, gaps := queriesOf(t, discover.LangPowerShell, "a.ps1", src)
	if !sameStrings(refs(got), []string{"reads orders"}) {
		t.Errorf("queries = %v, want the query below the here-string. A reader that left the "+
			"string open would find nothing at all in the rest of the file", refs(got))
	}
	if gaps != 0 {
		t.Errorf("unnamed queries = %d, want 0", gaps)
	}
}

// Normalize dedupes, because one module writing one table from eleven call sites is one
// edge. The count is what the graph reads, so a duplicate here is a weight nobody declared.
func TestRepeatedQueriesAgainstOneTableCollapse(t *testing.T) {
	const src = "package a\n\n" +
		"const a = \"INSERT INTO orders (id) VALUES ($1)\"\n" +
		"const b = \"INSERT INTO orders (id, total) VALUES ($1, $2)\"\n" +
		"const c = \"UPDATE orders SET total = $1\"\n" +
		"const d = \"SELECT id FROM orders\"\n"
	got, _ := queriesOf(t, discover.LangGo, "a.go", src)
	want := []string{"reads orders", "writes orders"}
	if !sameStrings(refs(got), want) {
		t.Errorf("queries = %v, want %v — four statements, two facts", refs(got), want)
	}
	// Provenance points at the first place a reader would find it.
	for _, q := range got {
		if q.Access == sqlstmt.Writes && q.Line != 3 {
			t.Errorf("write of orders reported at line %d, want 3 — the earliest", q.Line)
		}
	}
}

// Normalize sorts by what the fact says, not by where it was written, so moving a function
// does not move the bundle's diff.
func TestQueryOrderDoesNotFollowTheFilesOrder(t *testing.T) {
	const a = "package a\n\nconst x = \"SELECT id FROM zebras\"\nconst y = \"SELECT id FROM apples\"\n"
	const b = "package a\n\nconst y = \"SELECT id FROM apples\"\nconst x = \"SELECT id FROM zebras\"\n"
	first, _ := queriesOf(t, discover.LangGo, "a.go", a)
	second, _ := queriesOf(t, discover.LangGo, "a.go", b)
	if !sameStrings(refs(first), refs(second)) {
		t.Fatalf("reordering the file changed the output: %v then %v", refs(first), refs(second))
	}
	if !sameStrings(refs(first), []string{"reads apples", "reads zebras"}) {
		t.Errorf("queries = %v, want them sorted by table", refs(first))
	}
}

// The file that holds no SQL at all is the common case, and it must cost nothing and report
// nothing. A reader whose gate is loose reports gaps here, which is worse than reporting
// none: a coverage number that counts ordinary strings is a number nobody can act on.
func TestAFileWithNoSQLReportsNothing(t *testing.T) {
	const src = "package a\n\nimport \"fmt\"\n\n" +
		"const base = \"https://example.invalid/v1/orders\"\n" +
		"const ct = \"application/json\"\n" +
		"const errf = \"insert into %s failed: %w\"\n" +
		"func f() { fmt.Println(\"deleted from cache\") }\n"
	got, gaps := queriesOf(t, discover.LangGo, "a.go", src)
	if len(got) != 0 {
		t.Errorf("queries = %v, want none. Every string here mentions a verb or a table-like "+
			"word, and none of them is SQL", refs(got))
	}
	if gaps != 0 {
		t.Errorf("unnamed queries = %d, want 0. `insert into %%s failed` is an error message, "+
			"and counting it as a query signpost could not resolve would make the coverage "+
			"report unreadable", gaps)
	}
}

// sqlLiterals is the recovery half, tested directly because a literal recovered with the
// wrong bounds is the failure that produces a plausible-looking wrong answer: a body sliced
// one byte short still parses, and reports a table named `order`.
func TestLiteralBodiesAreRecoveredExactly(t *testing.T) {
	cases := []struct {
		name string
		cfg  scanConfig
		src  string
		want []string
	}{
		{
			name: "backtick spanning lines",
			cfg:  scanJSLike,
			src:  "const q = `\nSELECT 1\n`;\nconst r = \"x\";\n",
			want: []string{"\nSELECT 1\n", "x"},
		},
		{
			name: "triple quote",
			cfg:  scanPython,
			src:  "q = \"\"\"\nSELECT 1\n\"\"\"\nr = 'x'\n",
			want: []string{"\nSELECT 1\n", "x"},
		},
		{
			name: "heredoc",
			cfg:  scanRuby,
			src:  "q = <<~SQL\n  SELECT 1\nSQL\nr = 'x'\n",
			want: []string{"\n  SELECT 1\n", "x"},
		},
		{
			name: "verbatim string",
			cfg:  scanCSharp,
			src:  "var q = @\"\nSELECT 1\n\";\nvar r = \"x\";\n",
			want: []string{"\nSELECT 1\n", "x"},
		},
		{
			name: "here-string with code after the terminator",
			cfg:  scanPowerShell,
			src:  "$q = @\"\nSELECT 1\n\"@ -join ''\n$r = 'x'\n",
			// The empty one is the `''` on the terminator line, and it belongs here: it is
			// evidence that the code after `"@` was scanned rather than swallowed as body.
			want: []string{"\nSELECT 1\n", "", "x"},
		},
		{
			name: "single line only",
			cfg:  scanJSLike,
			src:  "const a = \"one\", b = 'two';\n",
			want: []string{"one", "two"},
		},
		// The six below are one defect, and it was a class rather than a case. Every form
		// that *can* span lines is also written on one line, and the scanner blanks those
		// forms delimiters and all — it has to, since a preserved `"` from a `"""` would be
		// read as an ordinary quote and the body recovered from it would end at the opener's
		// second quote. So there was nothing left in Text to find the literal by, and
		// `sqlLiterals` returned nothing at all: `r#"SELECT id FROM customers"#` drew
		// neither an edge nor a gap, in silence.
		//
		// One row per form rather than one representative row, because the forms are
		// separate branches of the scanner with separate offset arithmetic — the prefix is
		// two bytes for `r"`, five for `r###"`, two for `@"`, three for `"""` — and a fix
		// that got one of them right proves nothing about the others.
		{
			name: "rust raw string on one line",
			cfg:  scanRust,
			src:  "let q = r\"SELECT 1\";\nlet r = \"x\";\n",
			want: []string{"SELECT 1", "x"},
		},
		{
			name: "rust hashed raw string on one line",
			cfg:  scanRust,
			src:  "let q = r#\"SELECT 1\"#;\nlet r = \"x\";\n",
			want: []string{"SELECT 1", "x"},
		},
		{
			// Two hashes, so the body itself may hold `"#` — which is the reason the form
			// exists and the reason the span cannot be derived from the terminator alone.
			name: "rust raw string with two hashes",
			cfg:  scanRust,
			src:  "let q = r##\"SELECT \"#\" AS quoted\"##;\n",
			want: []string{"SELECT \"#\" AS quoted"},
		},
		{
			name: "csharp verbatim string on one line",
			cfg:  scanCSharp,
			src:  "var q = @\"SELECT 1\";\nvar r = \"x\";\n",
			want: []string{"SELECT 1", "x"},
		},
		{
			// The body keeps its doubled quotes, which is what C# writes a quote as. Kept
			// rather than unescaped because SQL treats `""` as an empty quoted identifier
			// and nothing downstream reads one, so collapsing it would be a second escape
			// rule to keep in step with the scanner's.
			name: "csharp verbatim string with a doubled quote",
			cfg:  scanCSharp,
			src:  "var q = @\"SELECT \"\"x\"\" FROM t\";\n",
			want: []string{"SELECT \"\"x\"\" FROM t"},
		},
		{
			name: "triple quote opening and closing on one line",
			cfg:  scanPython,
			src:  "q = \"\"\"SELECT 1\"\"\"\nr = 'x'\n",
			want: []string{"SELECT 1", "x"},
		},
		{
			// Two of them in one call, since the spans are collected per line and a fix
			// that recorded only the first would lose the second silently.
			name: "two raw strings on one line",
			cfg:  scanRust,
			src:  "f(r#\"SELECT 1\"#, r#\"SELECT 2\"#);\n",
			want: []string{"SELECT 1", "SELECT 2"},
		},
		{
			// The negative boundary for all of the above. A raw string inside a comment is
			// not a literal, and the spans must come from the scanner's own reading of the
			// line rather than from a search of Raw — which is exactly the shortcut that
			// would satisfy every positive row here.
			name: "raw string inside a comment is not a literal",
			cfg:  scanRust,
			src:  "// let q = r#\"SELECT 1\"#;\nlet r = \"x\";\n",
			want: []string{"x"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []string
			for _, lit := range sqlLiterals(scanLines(c.src, c.cfg)) {
				got = append(got, lit.text)
			}
			if !sameStrings(got, c.want) {
				t.Errorf("literals = %q, want %q", got, c.want)
			}
		})
	}
}

// An unterminated literal must not swallow the file into one gap, and must not report the
// rest of the source as a table. A file mid-edit is a real input — the pre-commit hook runs
// on one — and the safe reading is that whatever follows is not a query.
func TestAnUnterminatedLiteralNamesNothing(t *testing.T) {
	const src = "const q = `\nSELECT id FROM orders\n"
	got, gaps := queriesOf(t, discover.LangTS, "a.ts", src)
	// The query is real and readable even though the literal never closes, so reading it is
	// correct; what must not happen is a second, invented one.
	if len(got) > 1 || (len(got) == 1 && got[0].Table != "orders") {
		t.Errorf("queries = %v, want at most the one real read of orders", refs(got))
	}
	if gaps > 1 {
		t.Errorf("unnamed queries = %d; an unterminated literal is one piece of text, not a "+
			"gap per line", gaps)
	}
}

// A file whose every query interpolates its table reports no tables and says so. This is
// the shape ADR 0034's consequence names: the module's page shows a count rather than
// nothing, because nothing reads as "this code touches no tables".
func TestAModuleWhoseTablesAreAllInterpolatedReportsTheGap(t *testing.T) {
	const src = "package a\n\nimport \"fmt\"\n\n" +
		"func read(t string) string { return fmt.Sprintf(\"SELECT * FROM %s\", t) }\n" +
		"func write(t string) string { return fmt.Sprintf(\"INSERT INTO %s (id) VALUES ($1)\", t) }\n" +
		"func purge(t string) string { return fmt.Sprintf(\"DELETE FROM %s\", t) }\n"
	got, gaps := queriesOf(t, discover.LangGo, "a.go", src)
	if len(got) != 0 {
		t.Errorf("queries = %v, want none — no table is named", refs(got))
	}
	if gaps != 3 {
		t.Errorf("unnamed queries = %d, want 3. Reporting zero would present a module that "+
			"touches three tables as touching none, which is the failure design §4.2 exists "+
			"to prevent", gaps)
	}
}

// Extraction is a pure function of the file, and the bundle is committed, so two runs over
// the same source must agree byte for byte.
func TestQueryExtractionIsDeterministic(t *testing.T) {
	src := "package a\n\nconst q = `\nINSERT INTO archive SELECT * FROM orders JOIN customers " +
		"ON true;\nUPDATE counters SET n = n + 1;\n`\n"
	first, firstGaps := queriesOf(t, discover.LangGo, "a.go", src)
	for i := 0; i < 20; i++ {
		got, gaps := queriesOf(t, discover.LangGo, "a.go", src)
		if !sameStrings(refs(got), refs(first)) || gaps != firstGaps {
			t.Fatalf("run %d = %v/%d, first = %v/%d", i, refs(got), gaps, refs(first), firstGaps)
		}
	}
	want := []string{"writes archive", "writes counters", "reads customers", "reads orders"}
	if !sameStrings(refs(first), want) {
		t.Errorf("queries = %v, want %v", refs(first), want)
	}
}

// The scanner's offsets are the contract sqlLiterals depends on, so a line outside any
// multi-line string must say -1 rather than 0. Zero is a real offset, and a reader treating
// it as "no literal here" or the reverse would slice the line's own code as a string body.
func TestALineOutsideAMultiLineStringHasNoBodyOffsets(t *testing.T) {
	lines := scanLines("const x = 1;\nconst q = `\nSELECT 1\n`;\n", scanJSLike)
	if lines[0].BodyStart != -1 || lines[0].BodyEnd != -1 {
		t.Errorf("line 1 offsets = (%d, %d), want (-1, -1)", lines[0].BodyStart, lines[0].BodyEnd)
	}
	if lines[1].BodyStart != len("const q = `") {
		t.Errorf("line 2 BodyStart = %d, want %d — just past the backtick",
			lines[1].BodyStart, len("const q = `"))
	}
	if lines[2].BodyStart != -1 || lines[2].BodyEnd != -1 {
		t.Errorf("line 3 is body, not a boundary: offsets = (%d, %d), want (-1, -1)",
			lines[2].BodyStart, lines[2].BodyEnd)
	}
	if lines[3].BodyEnd != 0 {
		t.Errorf("line 4 BodyEnd = %d, want 0 — the terminator is the line's first byte",
			lines[3].BodyEnd)
	}
}

// A joined line carries no offsets, because its Raw is several lines spliced together and
// an offset from any one of them points somewhere else in it.
func TestAJoinedLineCarriesNoBodyOffsets(t *testing.T) {
	lines := scanLines("from typing import (\n    Any,\n)\n", scanPython)
	joined, _ := joinParens(lines, 0)
	if joined.BodyStart != -1 || joined.BodyEnd != -1 {
		t.Errorf("joined offsets = (%d, %d), want (-1, -1); the zero value would read as a "+
			"literal body beginning at the line's first byte", joined.BodyStart, joined.BodyEnd)
	}
}

// The whole file, end to end, with the interpolated case beside the literal one — which is
// what a corpus fixture looks like and why the reader can be trusted on one.
func TestASourceFileReportsBothItsTablesAndItsGaps(t *testing.T) {
	src := strings.Join([]string{
		"package store",
		"",
		"import \"fmt\"",
		"",
		"// list reads the open orders. SELECT id FROM commented_out",
		"const list = `",
		"SELECT o.id, o.total",
		"FROM orders o",
		"JOIN customers c ON c.id = o.customer_id",
		"WHERE o.state = $1",
		"`",
		"",
		"const archive = `INSERT INTO order_archive SELECT * FROM orders WHERE created_at < $1`",
		"",
		"func purge(table string) string {",
		"\treturn fmt.Sprintf(\"DELETE FROM %s\", table)",
		"}",
		"",
		"const errNoRows = \"could not update the order: no rows\"",
		"",
	}, "\n")
	got, gaps := queriesOf(t, discover.LangGo, "store/orders.go", src)
	want := []string{"reads customers", "writes order_archive", "reads orders"}
	if !sameStrings(refs(got), want) {
		t.Errorf("queries = %v, want %v", refs(got), want)
	}
	if gaps != 1 {
		t.Errorf("unnamed queries = %d, want 1 — the Sprintf, and only it", gaps)
	}
}
