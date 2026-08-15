// Package sqlstmt reads SQL: where one statement ends, what verb it is, what tables it
// names, and whether it loses data.
//
// It exists as its own package because two readers need the same answers about the same
// text and neither belongs inside the other. internal/manifest reads a migration file,
// where the whole file is SQL. internal/extract reads a string literal in Go or Python or
// Ruby, where the SQL is a few lines inside somebody's function. Those are the same
// question — "what does this statement do, and to what" — and a second implementation of
// it would diverge on the cases that are hard: a `;` inside a quoted string, a `--`
// comment holding a rollback, Postgres' `$$` bodies. The migration reader had those right
// already, so this is where that reading lives and both callers share it.
//
// The precision rule is internal/extract's: a statement is read only when its first word
// is a verb this package knows, and a table only when it is spelled out. Text that merely
// mentions SQL — a comment, a sentence in a docstring, an error message — names nothing
// here. Neither does a name the source assembles at run time: `"DELETE FROM " + table`
// and `DELETE FROM %s` name no table in the tree, and resolving them needs a call graph
// signpost does not have (ADR 0022). Those are reported as gaps rather than guessed at,
// which is what ADR 0034 requires of a deterministic pass.
package sqlstmt

import "strings"

// Statement is one statement, with the line it began on.
type Statement struct {
	Text string
	Line int
}

// Split divides SQL into statements, skipping comments and string bodies.
//
// A `;` inside a string literal or a `--` comment does not end a statement, and both
// appear in real migrations — a seeded row of text, a commented-out rollback.
func Split(src string) []Statement {
	var out []Statement
	var cur strings.Builder
	start := 0
	inBlock := false
	q := byte(0)
	// A dollar-quoted body ($$ ... $$) holds a whole function in Postgres, and
	// everything inside it is content.
	inDollar := false

	for i, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		num := i + 1
		for j := 0; j < len(raw); j++ {
			c := raw[j]
			switch {
			case inBlock:
				if c == '*' && j+1 < len(raw) && raw[j+1] == '/' {
					inBlock = false
					j++
				}
			case inDollar:
				if c == '$' && j+1 < len(raw) && raw[j+1] == '$' {
					inDollar = false
					j++
				}
			case q != 0:
				// SQL escapes a quote by doubling it, not with a backslash.
				if c == q {
					if j+1 < len(raw) && raw[j+1] == q {
						j++
						cur.WriteByte(c)
						continue
					}
					q = 0
				}
				cur.WriteByte(c)
			case c == '\'' || c == '"' || c == '`':
				q = c
				cur.WriteByte(c)
			case c == '$' && j+1 < len(raw) && raw[j+1] == '$':
				inDollar = true
				j++
			case c == '-' && j+1 < len(raw) && raw[j+1] == '-':
				j = len(raw)
			case c == '#' && cur.Len() == 0:
				// A `#` comment is MySQL's, and a leading one is unambiguous.
				j = len(raw)
			case c == '/' && j+1 < len(raw) && raw[j+1] == '*':
				inBlock = true
				j++
			case c == ';':
				if text := strings.TrimSpace(cur.String()); text != "" {
					// The line it began on, not the one the `;` closed on. The migration
					// reader that this code came from never read this field — it stamps
					// every migration with line 1, since the file is the fact there — so
					// the two were indistinguishable until a caller wanted provenance for
					// a statement inside a function. A multi-line CREATE TABLE reported
					// its closing paren, which points a reader at the end of the thing
					// they were sent to look at.
					out = append(out, Statement{Text: text, Line: max(start, 1)})
				}
				cur.Reset()
				start = 0
			default:
				if cur.Len() == 0 {
					if c == ' ' || c == '\t' {
						continue
					}
					start = num
				}
				cur.WriteByte(c)
			}
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
	}
	if text := strings.TrimSpace(cur.String()); text != "" {
		out = append(out, Statement{Text: text, Line: max(start, 1)})
	}
	return out
}

// destructiveVerbs are the statement verbs that lose data or break a running deployment.
//
// DROP and TRUNCATE are obvious. A column rename is here because it breaks every reader
// of the old name at the moment it lands, which in a rolling deployment is the old
// version of the application still serving traffic.
var destructiveVerbs = map[string]bool{
	"DROP": true, "TRUNCATE": true, "DELETE": true, "RENAME": true,
}

// Destructive reports whether a statement loses data or breaks a running deployment.
//
// The leading verb is not enough, and the case it misses is the one that matters most:
// `ALTER TABLE things DROP COLUMN legacy_id` leads with ALTER, which is additive far more
// often than not, and drops a column. So an ALTER is also searched for a destructive
// action word. The search is over whole fields rather than the raw text because a column
// named `dropped_at` or a table named `deleted_things` is not an action.
func Destructive(stmt, verb string) bool {
	if destructiveVerbs[verb] {
		return true
	}
	if verb != "ALTER" {
		return false
	}
	for _, f := range strings.Fields(stmt)[1:] {
		if destructiveVerbs[strings.ToUpper(trimName(f))] {
			return true
		}
	}
	return false
}

// noiseWords are the keywords between a statement's verb and the name it acts on.
var noiseWords = map[string]bool{
	"TABLE": true, "INDEX": true, "VIEW": true, "TYPE": true, "SCHEMA": true,
	"UNIQUE": true, "IF": true, "NOT": true, "EXISTS": true, "COLUMN": true,
	"CONSTRAINT": true, "MATERIALIZED": true, "CONCURRENTLY": true, "TRIGGER": true,
	"FUNCTION": true, "SEQUENCE": true, "INTO": true, "FROM": true, "ONLY": true,
	"OR": true, "REPLACE": true, "TEMPORARY": true, "TEMP": true, "PRIMARY": true,
	"KEY": true, "FOREIGN": true, "EXTENSION": true, "DATABASE": true,
}

// Subject returns a statement's verb and the object it names.
//
// The verb decides whether a migration is destructive; the object is the table, which is
// the node a data-model graph is built from. This is the migration reader's view — one
// name per statement, because a migration statement changes one thing — and Tables is the
// source reader's, which needs every table a query touches and in which direction.
func Subject(stmt string) (string, string) {
	fields := strings.Fields(stmt)
	if len(fields) == 0 {
		return "", ""
	}
	verb := strings.ToUpper(fields[0])
	if !subjectVerbs[verb] {
		// Not a DDL or DML statement: a SET, a COMMENT, a BEGIN. Nothing §4.1 asks for.
		return verb, ""
	}
	// Its own loop rather than nameAfter, because the two want different things from a
	// parenthesis. A migration statement's name may follow one — a column list can precede
	// the table in some dialects' syntax — so this keeps looking; Tables stops, because
	// there a parenthesis where a name belongs is a subquery it must not read as a table.
	for _, f := range fields[1:] {
		word := strings.ToUpper(f)
		if noiseWords[word] || strings.HasPrefix(f, "(") {
			continue
		}
		if n := trimName(f); n != "" {
			// A schema-qualified name keeps its qualifier: `public.things` and
			// `audit.things` are different tables.
			return verb, n
		}
	}
	return verb, ""
}

// subjectVerbs are the statements a migration is made of: the ones that change a schema
// or the rows in it. A SELECT is absent deliberately — a migration that reads is not what
// a migration is for, and Tables is where a query's reads are answered.
var subjectVerbs = map[string]bool{
	"CREATE": true, "DROP": true, "ALTER": true, "TRUNCATE": true,
	"INSERT": true, "DELETE": true, "UPDATE": true, "RENAME": true,
}

// LooksLikeSQL reports whether a string literal is a query rather than prose.
//
// This is the gate on the source reader, and it is deliberately strict, because the
// literals it says no to are almost all of them: a log message, a URL, a format string, an
// error, a comment's text. Two conditions, and both are needed.
//
// First, the text must *begin* with a statement verb. A sentence mentioning a verb is not
// a statement — "could not update the order" and "select a row from the list" both read as
// SQL to a rule that searches anywhere in the string, and the second one names a table
// called "the".
//
// Second, the verb must be followed by the keyword its syntax requires — SELECT by FROM,
// INSERT by INTO, UPDATE by SET, DELETE by FROM — and neither what stands between the two
// nor what follows the keyword may be a phrase of prose. All of that is needed, and two
// sentences say why. "select the row you want from the list" begins with a verb and does
// have a "from", so a rule checking only for the keyword accepts it and then reports a
// table called "the". "insert into %s failed: %w" is a Go error message whose first three
// words are exactly an interpolated INSERT, so a rule checking only the front half accepts
// it and reports a query signpost could not resolve — which is worse than a wrong table,
// because it inflates the one number a reader uses to judge how much was missed.
//
// What separates them is SQL's grammar rather than a list of English words: a clause is
// punctuated, and prose is a run of bare words. `SELECT id FROM t` has one bare word before
// the keyword and one after; `SELECT a, b FROM t WHERE id = $1` has commas and an equals;
// "the row you want" has four bare words in a row and "failed: %w" has two. A blacklist of
// function words would answer these two sentences; the grammar answers the ones nobody
// thought of.
//
// `SELECT 1` is declined as a consequence, and correctly: it is a real query that names no
// table, so there is nothing here for this reader to report either way.
//
// A DDL statement in a string literal is not read at all. `CREATE TABLE` in source is a
// test fixture or an embedded schema, and a migration is where a schema change is a fact
// about the repository — reading one here would put a module that holds a test's setup SQL
// on the map as the thing that created the table.
func LooksLikeSQL(s string) bool {
	fields := strings.Fields(s)
	if len(fields) < 2 {
		// A query naming a table cannot be shorter than verb, keyword, name — but the
		// keyword may be the last field when the name is interpolated, which is the case
		// Tables reports as a gap and so must reach it.
		return false
	}
	verb := strings.ToUpper(trimName(fields[0]))
	want, ok := requiredKeyword[verb]
	if !ok {
		return false
	}
	for i, f := range fields[1:] {
		if strings.ToUpper(trimName(f)) != want {
			continue
		}
		// The keyword goes into the left clause rather than being cut out between the two,
		// because it is the thing that closes whatever name precedes it. `UPDATE orders o SET
		// total = 0` is an aliased update, and the SET is the only field saying that `o` is an
		// alias and not a second bare word; excluded, the clause ended on a two-run with
		// nothing closing it and the statement was declined as prose. It only opens that one
		// shape: FROM and INTO are noise words rather than clause introducers, so a prose
		// two-run before either of those is still declined.
		return isClause(fields[1:i+2]) && isClause(fields[i+2:])
	}
	return false
}

// isClause reports whether a run of fields is SQL syntax rather than a phrase of prose.
//
// The rule is the grammar's, and it is the same on both sides of a statement's keyword. SQL
// puts syntax between its names: a comma, a parenthesis, a dot, an equals, or a keyword —
// `JOIN`, `WHERE`, `AS`, `SET`. Prose does not, so what tells the two apart is not how many
// bare words a clause holds but how many stand *in a row* with nothing between them.
// `FROM orders JOIN customers ON c.id = o.id` names two tables and never has two bare words
// adjacent; "the wrong replica again" is three.
//
// Two adjacent words are permitted where a *separator* closes them — punctuation, or a
// clause introducer — because that is a table and its alias: `FROM orders o JOIN`. They are
// not permitted at the end of the clause, where nothing identifies the second word as an
// alias and "delete from the queue" has exactly that shape, and not where what closes them is
// a DDL word: `TABLE`, `COLUMN`, and `INDEX` structure a migration and have no place after a
// DML keyword, so "insert into the outbox table" is a sentence rather than an aliased name.
// Three in a row is prose anywhere.
//
// Counting the total instead was wrong in the direction that costs most, and silently: it
// rejected every query naming two tables, so a repository's joins simply were not on the map
// and no gap counted them either.
func isClause(fields []string) bool {
	run := 0
	for _, f := range fields {
		if !isSyntax(f) {
			run++
			continue
		}
		if run > 2 || (run == 2 && !isSeparator(f)) {
			return false
		}
		run = 0
	}
	return run < 2
}

// isSyntax reports whether a field is SQL rather than a name.
//
// Three ways to be SQL: punctuation, which prose does not put between a word and the word
// "from"; a field with no letter in it at all, which is a number or a placeholder and so can
// never be a name; or a keyword that structures the statement.
//
// AND and OR are the two keywords excluded, and the reason is what they connect. Every other
// keyword here introduces a clause or qualifies a name, so a name beside one is a name. These
// two join *expressions*, and an expression made only of a bare word is not SQL: `WHERE a = 1
// AND b = 2` has an equals on either side and reaches this function's first case, while
// "insert into cache and retry" has nothing on either side and is an error message.
func isSyntax(f string) bool {
	if strings.ContainsAny(f, ",()*.=") || !hasLetter(f) {
		return true
	}
	word := strings.ToUpper(trimName(f))
	if word == "AND" || word == "OR" {
		return false
	}
	return clauseWords[word] || noiseWords[word] || listKeywords[word]
}

// isSeparator reports whether a field can close a table's alias — the narrower half of
// isSyntax. Punctuation and a clause introducer both mark where a name ended; a DDL noise word
// does not, because nothing in a DML statement puts one after the table.
func isSeparator(f string) bool {
	if strings.ContainsAny(f, ",()*.=") || !hasLetter(f) {
		return true
	}
	return clauseWords[strings.ToUpper(trimName(f))]
}

func hasLetter(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i] | 0x20
		if c >= 'a' && c <= 'z' {
			return true
		}
	}
	return false
}

// listKeywords are the words that may stand in a select list without being a column.
var listKeywords = map[string]bool{
	"DISTINCT": true, "ALL": true, "TOP": true, "SQL_CALC_FOUND_ROWS": true,
}

// requiredKeyword is the word each readable verb's syntax requires, and the verbs are the
// four DML statements. DDL is absent for the reason LooksLikeSQL's doc gives.
var requiredKeyword = map[string]string{
	"SELECT": "FROM",
	"INSERT": "INTO",
	"UPDATE": "SET",
	"DELETE": "FROM",
}

// Access is the direction of one table reference.
type Access string

const (
	// Writes is a statement that changes rows or the schema holding them.
	Writes Access = "writes"
	// Reads is a statement that only observes.
	Reads Access = "reads"
)

// Reference is one table a statement names, and what it does to it.
type Reference struct {
	Table  string
	Access Access
}

// Tables returns the tables one statement names, and whether a name it needed was not
// spelled out in the text.
//
// The second return is the point of the signature, and it is why this does not simply
// return a possibly-empty slice. `"SELECT * FROM " + table` reaches this function as
// `SELECT * FROM` and `fmt.Sprintf("DELETE FROM %s", t)` as `DELETE FROM %s`: both are
// real statements against a real table, and neither says which. Silently returning
// nothing for them would report a module that touches six tables as touching four, with
// nothing anywhere saying so — the failure mode design §4.2 exists to prevent. So the
// caller is told, and counts it.
//
// A statement that names no table and needed none — `SELECT 1`, `BEGIN`, `SET
// statement_timeout = 0` — returns nothing and reports no gap. The difference between
// that and the paragraph above is the whole of what this function is careful about.
//
// The input is assumed to be SQL. Deciding whether a string literal in somebody's
// function is a query at all is the caller's, because the caller has the evidence this
// function does not: what the literal is passed to. `LooksLikeSQL` is that gate, and a
// sentence of English that happens to contain the word "from" is its problem rather than
// this one's.
func Tables(stmt string) ([]Reference, bool) {
	fields := strings.Fields(stmt)
	if len(fields) == 0 {
		return nil, false
	}
	verb := strings.ToUpper(trimName(fields[0]))
	if !subjectVerbs[verb] && verb != "SELECT" && verb != "WITH" {
		return nil, false
	}

	var refs []Reference
	gap := false
	// written is the field index of the name the verb acts on, so the FROM scan below can
	// tell `DELETE FROM orders` — where FROM introduces the write target — from
	// `INSERT INTO archive SELECT * FROM orders`, where it introduces a read. By index and
	// not by name, because a statement may legitimately read the table it writes.
	written := -1

	add := func(n name, acc Access) {
		switch {
		case n.subquery:
			// A parenthesised subquery where a name could be. Nothing is missing from the
			// text, so this is not a gap; the inner statement's own tables are read by the
			// scan below, which walks into the parentheses.
		case !validName(n.text):
			// Either the verb needed a name and the literal stops here, or what stands in
			// the slot is a placeholder the source substitutes at run time.
			gap = true
		default:
			refs = append(refs, Reference{Table: n.text, Access: acc})
			if acc == Writes && written < 0 {
				written = n.idx
			}
		}
	}

	switch verb {
	case "SELECT", "WITH":
		// Reads only; every table it names arrives through the FROM and JOIN scan below.
	case "INSERT":
		// The target follows INTO, and `INSERT INTO t SELECT ... FROM u` also reads.
		if i := indexOfWord(fields, "INTO"); i >= 0 {
			add(nameAfter(fields, i), Writes)
		} else {
			gap = true
		}
	case "DELETE":
		// FROM introduces the target here rather than a source, which is the one place
		// the two readings collide.
		if i := indexOfWord(fields, "FROM"); i >= 0 {
			add(nameAfter(fields, i), Writes)
		} else {
			gap = true
		}
	default:
		// UPDATE, TRUNCATE, and the DDL verbs all name their target immediately, past
		// whatever noise words the dialect puts in between.
		add(nameAfter(fields, 0), Writes)
	}

	// Reads: every FROM and JOIN past the write target. A joined lookup table and a
	// subquery in a WHERE clause are both real reads, and both are what a reader chasing a
	// data symptom wants to find on the table's page.
	for i := 1; i < len(fields); i++ {
		word := strings.ToUpper(trimName(fields[i]))
		if word != "FROM" && word != "JOIN" {
			continue
		}
		// A FROM inside a function call is not a table source: `EXTRACT(DAY FROM ts)`,
		// `SUBSTRING(s FROM 1 FOR 2)`, and `TRIM(BOTH ' ' FROM s)` are SQL's three
		// prepositional forms, and reading them as one puts a column on the map as a
		// table. A FROM inside a *subquery's* parentheses is a table source, so the two
		// cases are told apart by what opened the paren rather than by depth alone.
		if !inSubquery(fields[:i]) {
			continue
		}
		n := nameAfter(fields, i)
		if n.subquery {
			continue
		}
		// The write target, reached a second time through the FROM that introduced it.
		// Guarded on written being set, because -1 is also nameAfter's "nothing there" —
		// and treating that as the write target would skip the gap below.
		if written >= 0 && n.idx == written {
			continue
		}
		if !validName(n.text) {
			// A FROM with nothing readable after it. Either a placeholder or a literal
			// that ends mid-clause, and both are a table this reader could not name.
			gap = true
			continue
		}
		refs = append(refs, Reference{Table: n.text, Access: Reads})
		i = n.idx
	}
	return dedupe(refs), gap
}

// clauseWords end a name's position. Reaching one means the slot being read is empty:
// `SELECT a FROM WHERE` has no table, and returning "WHERE" as one would put a SQL
// keyword on the map as a page.
var clauseWords = map[string]bool{
	"SELECT": true, "VALUES": true, "SET": true, "WHERE": true, "GROUP": true,
	"ORDER": true, "LIMIT": true, "OFFSET": true, "HAVING": true, "UNION": true,
	"RETURNING": true, "ON": true, "USING": true, "AS": true, "WITH": true,
	"JOIN": true, "LEFT": true, "RIGHT": true, "INNER": true, "OUTER": true,
	"FULL": true, "CROSS": true, "LATERAL": true, "BY": true, "AND": true,
	"NULL": true, "DEFAULT": true, "WINDOW": true, "FETCH": true,
}

// name is what stands where a table name belongs.
type name struct {
	// idx is the field it was read from, -1 when there was nothing to read.
	idx int
	// text is the name with its quoting and punctuation stripped, empty when the slot
	// held no name at all.
	text string
	// subquery marks a parenthesised statement standing where a name could: `FROM
	// (SELECT ...)`. Not a name and not a missing one, which is why it is its own field
	// rather than an empty text — the two have opposite answers for the gap count.
	subquery bool
}

// nameAfter returns what stands past field i, skipping the keywords a dialect puts
// between a verb and its object.
func nameAfter(fields []string, i int) name {
	for j := i + 1; j < len(fields); j++ {
		word := strings.ToUpper(trimName(fields[j]))
		if noiseWords[word] {
			continue
		}
		// Checked on the field as written and before the clause words, because trimming
		// strips the parenthesis and would turn `(SELECT` into the keyword SELECT and
		// then into a missing name.
		if strings.HasPrefix(fields[j], "(") {
			return name{idx: j, subquery: true}
		}
		if clauseWords[word] {
			return name{idx: -1}
		}
		return name{idx: j, text: trimName(fields[j])}
	}
	return name{idx: -1}
}

// inSubquery reports whether the field following those given is inside a parenthesis a
// subquery opened, or inside none at all. False means it is inside a function call's
// argument list, where a FROM is a preposition rather than a table source.
//
// Read forwards over the fields each time rather than tracked as the caller walks them,
// because the walk skips fields — a name consumed by nameAfter is stepped past — and a
// depth counter that missed a parenthesis in the gap would be wrong for the rest of the
// statement. A statement is a few dozen fields, so rescanning is cheaper than the bug.
func inSubquery(before []string) bool {
	// stack holds, per open parenthesis, whether a statement opened it.
	var stack []bool
	for i, f := range before {
		for j := 0; j < len(f); j++ {
			switch f[j] {
			case '(':
				// A subquery paren is one immediately followed by a statement keyword,
				// either in this same field — `(SELECT` — or as the next one.
				rest := strings.ToUpper(strings.TrimLeft(f[j+1:], "("))
				stmt := strings.HasPrefix(rest, "SELECT") || strings.HasPrefix(rest, "WITH")
				if strings.TrimSpace(f[j+1:]) == "" && i+1 < len(before) {
					next := strings.ToUpper(trimName(before[i+1]))
					stmt = next == "SELECT" || next == "WITH"
				}
				stack = append(stack, stmt)
			case ')':
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
		}
	}
	if len(stack) == 0 {
		return true
	}
	return stack[len(stack)-1]
}

// indexOfWord returns the index of a keyword in fields, or -1.
func indexOfWord(fields []string, want string) int {
	for i, f := range fields {
		if strings.ToUpper(trimName(f)) == want {
			return i
		}
	}
	return -1
}

// trimName strips the punctuation that surrounds a name in a statement: the dialect's
// quoting, and the parentheses and commas of the syntax around it.
func trimName(f string) string {
	return strings.Trim(f, "`\"'();,[]")
}

// validName reports whether a name is a table this repository could have.
//
// This is the negative half of the reader and the reason ADR 0034 has fixtures on both
// sides of it. A placeholder is what an interpolated table name looks like once the
// literal reaches here — `%s` from Sprintf, `?` and `$1` from a driver, `{table}` from an
// f-string, `#{t}` from Ruby — and every one of them would otherwise become a page named
// after formatting syntax, linked to from a module, in a committed artifact.
//
// The rule is a whitelist rather than a list of bad characters, because the bad set is
// open: each language spells interpolation its own way and a new one would pass a
// blacklist silently. What a table name may contain is closed — the identifier characters
// and the dot that qualifies a schema.
func validName(name string) bool {
	if name == "" {
		return false
	}
	if clauseWords[strings.ToUpper(name)] || noiseWords[strings.ToUpper(name)] {
		return false
	}
	// A leading digit is a number rather than an identifier: `LIMIT 10` reached through
	// a malformed statement should not become a table called 10.
	if name[0] >= '0' && name[0] <= '9' {
		return false
	}
	dots := 0
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '.':
			// A qualifier, and only one: `public.things` is a table and `a.b.c.d` is not
			// something this reader can place.
			dots++
			if dots > 1 || i == 0 || i == len(name)-1 {
				return false
			}
		case c == '_' || c == '$':
			// `$` is legal in a MySQL identifier, and `$1` is a placeholder — the
			// leading-digit rule above and this one together allow `a$b` and reject `$1`.
			if c == '$' && i == 0 {
				return false
			}
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}

// dedupe folds repeated references, keeping the first of each (table, access) pair so the
// order stays the statement's.
func dedupe(refs []Reference) []Reference {
	if len(refs) < 2 {
		return refs
	}
	seen := make(map[Reference]bool, len(refs))
	out := refs[:0:0]
	for _, r := range refs {
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}
