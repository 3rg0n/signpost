package extract

import (
	"sort"
	"strings"

	"github.com/3rg0n/signpost/internal/sqlstmt"
)

// Reading SQL out of source, which is where the other half of the data map is.
//
// A migration says a table exists and how its schema got here. It does not say which code
// touches it, and that is the half a reader with a data symptom needs: duplicate rows,
// a missing write, an ordering problem between two services. Both facts are in the
// repository and only one of them was on the map.
//
// The SQL is inside string literals, which is exactly the text internal/extract's scanner
// spends its effort *removing*: scanLines blanks a string body in Text precisely so a
// declaration cannot be invented from prose. So this reader works from the other side of
// that scanner — codeLine.Raw, which is preserved for this kind of use — and it inherits
// the scanner's judgement about which lines are code at all. A `-- DROP TABLE` in a
// comment is already gone before this runs.
//
// The precision rule is ADR 0034's: read a table where the source spells one out, count a
// gap where it does not, and never guess. `"DELETE FROM " + table` is a real statement
// against a real table whose name is the caller's, and the honest output for it is a
// number in the coverage report rather than an edge to whatever `table` might hold.

// Query is one table reference read out of a string literal in this file.
type Query struct {
	// Table is the name as the source spelled it, qualifier included.
	Table string
	// Access is whether the statement writes the table or only reads it.
	Access sqlstmt.Access
	// Line is where the literal holding the statement began.
	Line int
}

// sqlLiteral is one string literal recovered from source, with the line it started on.
type sqlLiteral struct {
	text string
	line int
}

// readQueries reads the table references out of a file's string literals.
//
// Returns the references and the number of statements that needed a table name the source
// did not spell out. The second number is not a diagnostic afterthought: a module whose
// every query interpolates its table would otherwise report no data access at all, which
// reads as "this code touches no tables" rather than "signpost could not tell".
func readQueries(lits []sqlLiteral) ([]Query, int) {
	var out []Query
	gaps := 0
	for _, lit := range lits {
		// Split first, then gate each statement. Split even though a literal is usually one
		// statement, because a `db.Exec` holding a two-statement transaction is ordinary and
		// reading only the first would miss the write in `DELETE FROM a; INSERT INTO b`. The
		// gate goes after it rather than before because it judges one statement's grammar:
		// asking whether two statements spliced together look like prose means asking about
		// the seam between them, where the first statement's last word sits beside the
		// second's verb and nothing in SQL's syntax separates the two.
		//
		// The line is the literal's rather than the statement's within it, because that is
		// the position in the file — Split counts from the start of the text it was given,
		// which begins mid-line.
		for _, stmt := range sqlstmt.Split(lit.text) {
			if !sqlstmt.LooksLikeSQL(stmt.Text) {
				continue
			}
			refs, gap := sqlstmt.Tables(stmt.Text)
			if gap {
				gaps++
			}
			for _, r := range refs {
				out = append(out, Query{Table: r.Table, Access: r.Access, Line: lit.line})
			}
		}
	}
	return out, gaps
}

// sqlLiterals recovers the string literals from scanned lines.
//
// Three shapes, because the scanner reports them differently and a reader that handled only
// one would miss most of the SQL that exists. An ordinary single-line literal keeps its
// quotes in Text with the body blanked, so its value is recovered from Raw at the same
// offset. A literal spanning lines — a heredoc, a Python triple-quoted string, a JS template
// literal, a Java text block — is bounded by the BodyStart and BodyEnd the scanner recorded,
// and multi-line is how SQL is written whenever it is longer than a clause. And a literal in
// a form the scanner blanks delimiters and all — `r#"..."#`, `@"..."`, a one-line
// `"""..."""` — is found only in the Bodies spans, because there is nothing left in Text to
// find it by.
//
// The third shape is a fix rather than a completion: without it a `r#"SELECT id FROM
// customers"#` was read as no literal at all, so the query drew neither an edge nor a gap.
func sqlLiterals(lines []codeLine) []sqlLiteral {
	var out []sqlLiteral
	for i := 0; i < len(lines); i++ {
		cl := lines[i]
		// The delimited literals first, and unconditionally: a line can both close one of
		// these and open a multi-line literal, and this is the only record of them.
		for _, sp := range cl.Bodies {
			if sp[0] < 0 || sp[1] > len(cl.Raw) || sp[1] < sp[0] {
				continue
			}
			out = append(out, sqlLiteral{text: cl.Raw[sp[0]:sp[1]], line: cl.Num})
		}
		if cl.BodyStart >= 0 && cl.BodyStart <= len(cl.Raw) {
			// Anything that opened and closed before it. `db.Exec("SELECT 1", <<~SQL)` is
			// two literals on one line, and taking only the multi-line one would drop a
			// query the file states.
			out = append(out, inlineLiterals(head(cl))...)
			// A multi-line literal opens here. Its body is the tail of this line plus every
			// line up to the one whose BodyEnd closes it. Joined with newlines rather than
			// spaces so a `--` comment inside the body ends where it ends in the file —
			// joining with spaces would comment out the statement after it.
			body := []string{cl.Raw[cl.BodyStart:]}
			j := i + 1
			for ; j < len(lines) && lines[j].BodyEnd < 0; j++ {
				body = append(body, lines[j].Raw)
			}
			if j < len(lines) {
				end := min(lines[j].BodyEnd, len(lines[j].Raw))
				body = append(body, lines[j].Raw[:max(end, 0)])
			}
			// The line reported is the one the literal opened on, which is where a reader
			// sent to this file will look for it.
			out = append(out, sqlLiteral{text: strings.Join(body, "\n"), line: cl.Num})
			// Resume on the closing line rather than past it: a heredoc's terminator line
			// can hold code, and a `"""` can be followed by the next literal.
			i = j - 1
			continue
		}
		out = append(out, inlineLiterals(cl)...)
	}
	return out
}

// head returns the part of a line before a multi-line literal's body begins, so the
// single-line reader cannot walk into the body's quotes.
func head(cl codeLine) codeLine {
	n := min(cl.BodyStart, len(cl.Text))
	cl.Text = cl.Text[:n]
	cl.Raw = cl.Raw[:min(n, len(cl.Raw))]
	return cl
}

// inlineLiterals recovers every string literal that opened and closed on one line.
//
// Driven by Text and read from Raw: Text is where the scanner left the delimiters of a
// literal it blanked, so a quote there is a literal that is really one, while a quote in
// Raw might be inside a comment or inside another string. That is the whole reason the
// scanner preserves the delimiters.
func inlineLiterals(cl codeLine) []sqlLiteral {
	var out []sqlLiteral
	for i := 0; i < len(cl.Text); i++ {
		c := cl.Text[i]
		if c != '"' && c != '\'' && c != '`' {
			continue
		}
		if i >= len(cl.Raw) || cl.Raw[i] != c {
			// Text and Raw are the same length by construction and the delimiters line
			// up, but a caller could hand over lines from elsewhere; a mismatch means the
			// offsets cannot be trusted, and reading Raw at them would slice arbitrary
			// text.
			break
		}
		val, ok := stringAt(cl.Raw, i)
		if !ok {
			break
		}
		out = append(out, sqlLiteral{text: val, line: cl.Num})
		// Past the closing delimiter. The body is blanked in Text, so the next quote
		// there is the closer — which is what makes this loop terminate on a literal
		// containing a quote of the other kind.
		j := i + 1
		for j < len(cl.Text) && cl.Text[j] != c {
			j++
		}
		i = j
	}
	return out
}

// addQueries attaches the SQL a file's literals name to its facts.
//
// Takes literals rather than lines because the Go extractor has a real parser and gets them
// from the AST, while the other twelve languages recover them from the scanner. Both arrive
// here, so the reading of SQL is one implementation and only the recovery of a literal
// differs — the languages disagree about how a string is quoted, not about what SQL is.
func (fa *Facts) addQueries(lits []sqlLiteral) {
	q, gaps := readQueries(lits)
	fa.Queries = append(fa.Queries, q...)
	fa.UnnamedQueries += gaps
}

// normalizeQueries sorts and dedupes the table references.
//
// Sorted by table then access, not by line, for the reason Normalize's comment gives: a
// fact's identity is what it says, not where it was written, and the bundle's diff should
// not move when a function is moved. Deduped because one module writing one table from
// eleven call sites is one edge — the count that matters for a data edge is which module,
// not how many statements, and a weight of eleven would read as coupling strength when it
// is only verbosity.
func (fa *Facts) normalizeQueries() {
	sort.Slice(fa.Queries, func(i, j int) bool {
		a, b := fa.Queries[i], fa.Queries[j]
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		if a.Access != b.Access {
			return a.Access < b.Access
		}
		return a.Line < b.Line
	})
	if len(fa.Queries) < 2 {
		return
	}
	out := fa.Queries[:1]
	for _, q := range fa.Queries[1:] {
		last := &out[len(out)-1]
		if last.Table == q.Table && last.Access == q.Access {
			// The earliest line, so provenance points at the first place a reader would
			// find it rather than an arbitrary one.
			if q.Line > 0 && (last.Line == 0 || q.Line < last.Line) {
				last.Line = q.Line
			}
			continue
		}
		out = append(out, q)
	}
	fa.Queries = out
}
