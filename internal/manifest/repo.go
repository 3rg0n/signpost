package manifest

import (
	"path"
	"strconv"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
	"github.com/3rg0n/signpost/internal/sqlstmt"
)

// Repository-convention extraction: migrations, ownership, stated rules, and build
// scripts.
//
// Design §4.1 groups these as "data model evolution", "ownership", and "stated rules",
// and what they share is that a human wrote them for other humans. A CODEOWNERS file is
// the only machine-readable statement of who to ask; a migration sequence is the only
// record of how the schema got here; AGENTS.md is the repository telling an agent what
// to do, which is the one input that should outrank anything inferred from the code.
//
// That last point is why §4.3's injection rule matters here and nowhere else in this
// package. A rule file is untrusted text that will be read back by a model, so what is
// extracted is recorded as a *quotation of what the repository says*, never as an
// instruction this tool endorses. The emitter is where that framing is applied; this
// reader's job is to keep the text intact and attributed so the framing has something
// true to wrap.

// ExtractMigration reads a database migration file.
//
// The sequence is the fact: one migration says little, and the ordered set says how the
// data model actually evolved. Destructiveness is called out because a migration that
// drops a column is the single most consequential kind of change in a repository — it is
// irreversible in production and invisible in the application code.
func ExtractMigration(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindMigration}

	version, name := splitMigrationName(path.Base(f.Path))
	m := Migration{Version: version, Name: name, Line: 1}

	// A migration may be SQL or a framework's DSL. The statements read below are SQL
	// keywords, which covers hand-written SQL and the generated SQL that Alembic, Prisma,
	// and golang-migrate all emit; an ORM migration written in Python or TypeScript is
	// read for the same verbs, which appear there as method names.
	for _, ln := range sqlstmt.Split(f.Content) {
		verb, target := sqlstmt.Subject(ln.Text)
		if target != "" {
			m.Tables = append(m.Tables, target)
		}
		if sqlstmt.Destructive(ln.Text, verb) {
			// Recorded on the migration rather than per statement: "this migration is
			// destructive" is the fact a reader needs before running it.
			m.Destructive = true
		}
	}
	// A down migration undoes an up migration, and the pair is one change. The direction
	// is part of the name so both halves stay visible without doubling the sequence.
	facts.Migrations = append(facts.Migrations, m)
	return facts
}

// splitMigrationName separates a migration's version from its description.
//
// Every tool encodes both in the filename, and the version is what orders the sequence:
// `0003_add_index.sql`, `20240115120000_add_index.up.sql`, `V3__add_index.sql`. The
// version is kept as written rather than parsed to a number, since a timestamp and an
// ordinal are both versions and comparing them as strings sorts each correctly within its
// own convention — which is the only comparison that is ever meaningful.
func splitMigrationName(base string) (string, string) {
	name := base
	for _, ext := range []string{".sql", ".py", ".ts", ".js", ".go", ".rb"} {
		name = strings.TrimSuffix(name, ext)
	}
	// The direction suffix that golang-migrate and others use.
	direction := ""
	for _, d := range []string{".up", ".down", "_up", "_down"} {
		if strings.HasSuffix(strings.ToLower(name), d) {
			direction = strings.TrimLeft(d, "._")
			name = name[:len(name)-len(d)]
			break
		}
	}
	// Flyway's `V3__description`.
	if v, rest, ok := strings.Cut(name, "__"); ok {
		return strings.TrimLeft(v, "vV"), withDirection(rest, direction)
	}
	if v, rest, ok := strings.Cut(name, "_"); ok && isDigits(v) {
		return v, withDirection(rest, direction)
	}
	if v, rest, ok := strings.Cut(name, "-"); ok && isDigits(v) {
		return v, withDirection(rest, direction)
	}
	return "", withDirection(name, direction)
}

func withDirection(name, direction string) string {
	name = strings.ReplaceAll(name, "_", " ")
	if direction == "" {
		return name
	}
	return name + " (" + direction + ")"
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseUint(s, 10, 64)
	return err == nil
}

// ExtractCodeowners reads a CODEOWNERS file.
//
// This is the only machine-readable statement of who to ask about a part of the
// repository, and §4.1 asks for it by name. It is also the file most likely to be
// out of date, which is worth nothing to hide: the extraction records what the file
// says, and the git-signal pass (task #6) is what says who actually touches the code.
func ExtractCodeowners(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindCodeowners}

	for i, raw := range strings.Split(strings.ReplaceAll(f.Content, "\r\n", "\n"), "\n") {
		line := raw
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		pattern := fields[0]
		owners := fields[1:]
		if len(owners) == 0 {
			// A pattern with no owner un-assigns it — a real statement, and one that
			// silently drops a directory from review if nobody notices.
			facts.Owners = append(facts.Owners, Owner{Pattern: pattern, Line: i + 1})
			continue
		}
		facts.Owners = append(facts.Owners, Owner{
			Pattern: pattern, Owners: owners, Line: i + 1,
		})
	}
	if len(facts.Owners) == 0 {
		facts.markIncomplete("no ownership rules found")
	}
	return facts
}

// ExtractAgentRules reads an AGENTS.md, CLAUDE.md, or equivalent.
//
// These files are the repository's own instructions, and design §4.3 is explicit that
// they are untrusted input: the text will be read back by a model, so it is captured as
// a quotation attributed to the file rather than as guidance this tool adopts. The
// framing is the emitter's job; this reader keeps the heading structure so the emitter
// can attribute each rule to the section that stated it.
//
// The heading structure is what makes the extraction useful rather than a copy: a reader
// wanting "the testing rules" gets the section, not the file.
func ExtractAgentRules(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindAgentRules}
	facts.Rules = readMarkdownRules(f.Content, markdownRuleOptions{})
	if len(facts.Rules) == 0 {
		facts.markIncomplete("no headings or statements found")
	}
	return facts
}

// ExtractADR reads an architecture decision record.
//
// An ADR states a decision and its consequences, and its status is the part that decides
// whether it still applies: a superseded ADR describes how the system used to work, and
// presenting it as current would mislead in the most expensive direction.
func ExtractADR(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindADR}
	status := adrStatus(f.Content)
	facts.Rules = readMarkdownRules(f.Content, markdownRuleOptions{status: status})

	// The record's own identity: `0004-use-podman.md` is decision 4.
	base := strings.TrimSuffix(path.Base(f.Path), ".md")
	num, title, _ := strings.Cut(base, "-")
	if isDigits(num) {
		facts.Module = Module{
			Name: strings.ReplaceAll(title, "-", " "), Version: num,
			Ecosystem: "adr", LangVersion: status, Line: 1,
		}
	}
	if len(facts.Rules) == 0 {
		facts.markIncomplete("no headings or statements found")
	}
	return facts
}

// adrStatusValues are the Nygard-form statuses, longest first so "Superseded by" is not
// matched as "Superseded" when both would do — and so "Proposed" does not shadow
// "Accepted" in a document that mentions both.
var adrStatusValues = []string{
	"Superseded", "Deprecated", "Rejected", "Accepted", "Proposed", "Draft",
}

// adrStatus finds the record's status.
//
// Read from a `Status` section or a `status:` front-matter key, which are the two places
// the short form puts it. Absent means unknown, recorded as such: guessing "Accepted"
// would assert that an undated draft governs the codebase.
func adrStatus(src string) string {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		// Front matter or an inline label: `status: Accepted`, `**Status:** Accepted`.
		if v, ok := cutStatusLabel(line); ok {
			if s := matchADRStatus(v); s != "" {
				return s
			}
		}
		// A `## Status` heading with the value on a following line.
		if strings.HasPrefix(lower, "#") && strings.Contains(lower, "status") {
			for _, next := range lines[i+1:] {
				t := strings.TrimSpace(next)
				if t == "" {
					continue
				}
				if strings.HasPrefix(t, "#") {
					break
				}
				if s := matchADRStatus(t); s != "" {
					return s
				}
				break
			}
		}
	}
	return ""
}

// cutStatusLabel returns the text after a `status:` label in any of its markup forms.
func cutStatusLabel(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, "*_- ")
	if len(trimmed) < len("status:") {
		return "", false
	}
	if !strings.EqualFold(trimmed[:len("status")], "status") {
		return "", false
	}
	rest := strings.TrimLeft(trimmed[len("status"):], "*_")
	after, ok := strings.CutPrefix(rest, ":")
	if !ok {
		return "", false
	}
	return strings.TrimSpace(strings.Trim(after, "*_ ")), true
}

func matchADRStatus(text string) string {
	for _, s := range adrStatusValues {
		if strings.HasPrefix(strings.ToLower(text), strings.ToLower(s)) {
			return s
		}
	}
	return ""
}

// markdownRuleOptions configures rule extraction.
type markdownRuleOptions struct {
	// status is carried onto every rule, which is how an ADR's superseded state travels
	// with the statements it made.
	status string
}

// readMarkdownRules turns a markdown document into headed statements.
//
// One rule per paragraph or list item, attributed to its enclosing heading path. That
// granularity is chosen so a consumer can cite a single rule: "the repository says, under
// Testing, that ..." is checkable, and "here is CLAUDE.md" is not.
//
// Fenced code blocks are skipped. A code block in a rules file is an example rather than
// a rule, and its contents would read as instructions once the fence markers are gone —
// which is exactly the shape §4.3 is guarding against.
func readMarkdownRules(src string, opts markdownRuleOptions) []Rule {
	var out []Rule
	var headings []string
	var para strings.Builder
	paraLine := 0
	fence := ""

	flush := func() {
		text := strings.TrimSpace(para.String())
		para.Reset()
		if text == "" {
			return
		}
		out = append(out, Rule{
			Heading: strings.Join(headings, " / "),
			Text:    collapseSpaces(text),
			Status:  opts.status,
			Line:    paraLine,
		})
	}

	for i, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		num := i + 1
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)

		if fence != "" {
			if strings.HasPrefix(trimmed, fence) {
				fence = ""
			}
			continue
		}
		if f := fenceMarker(trimmed); f != "" {
			flush()
			fence = f
			continue
		}

		if level, title, ok := markdownHeading(trimmed); ok {
			flush()
			// A heading at level N replaces everything at or below N, which reproduces
			// the document's own nesting without tracking it explicitly.
			if level-1 < len(headings) {
				headings = headings[:level-1]
			}
			for len(headings) < level-1 {
				headings = append(headings, "")
			}
			headings = append(headings, title)
			continue
		}

		if trimmed == "" {
			flush()
			continue
		}
		// A list item is its own rule: a bulleted list in a rules file is a list of
		// separate statements, and joining them into one paragraph would make each
		// uncitable.
		if isListItem(trimmed) {
			flush()
			para.WriteString(stripListMarker(trimmed))
			paraLine = num
			continue
		}
		if para.Len() == 0 {
			paraLine = num
		} else {
			para.WriteString(" ")
		}
		para.WriteString(trimmed)
	}
	flush()
	return out
}

// fenceMarker returns the fence a line opens, or "".
func fenceMarker(line string) string {
	for _, f := range []string{"```", "~~~"} {
		if strings.HasPrefix(line, f) {
			return f
		}
	}
	return ""
}

// markdownHeading reads an ATX heading's level and title.
func markdownHeading(line string) (int, string, bool) {
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level > 6 || level >= len(line) {
		return 0, "", false
	}
	title := strings.TrimSpace(strings.Trim(line[level:], "# "))
	if title == "" {
		return 0, "", false
	}
	return level, title, true
}

func isListItem(line string) bool {
	if len(line) >= 2 && (line[0] == '-' || line[0] == '*' || line[0] == '+') && line[1] == ' ' {
		return true
	}
	// An ordered item: `1. text`.
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(line) && (line[i] == '.' || line[i] == ')') && line[i+1] == ' '
}

func stripListMarker(line string) string {
	if len(line) >= 2 && (line[0] == '-' || line[0] == '*' || line[0] == '+') {
		return strings.TrimSpace(line[1:])
	}
	if i := strings.IndexAny(line, ".)"); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return line
}

// collapseSpaces folds runs of whitespace, so a rule reads as one sentence regardless of
// how the source wrapped it.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ExtractMakefile reads a Makefile or a Justfile.
//
// The targets are the repository's own answer to "how do I build, test, and lint this",
// and §4.1 asks for exactly that. It is the most reliable answer available: a README's
// instructions drift, but a target that stopped working gets fixed because CI calls it.
func ExtractMakefile(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindMakefile}

	// A target's recipe is the fact, so the first command line is kept with it: knowing
	// `test` exists is less useful than knowing it runs `go test -race ./...`.
	var cur *Script
	phony := map[string]bool{}

	lines := strings.Split(strings.ReplaceAll(f.Content, "\r\n", "\n"), "\n")
	for i, raw := range lines {
		num := i + 1
		// A recipe line begins with a tab — the one piece of Makefile syntax that is
		// whitespace-significant, and the reason a spaces-indented recipe fails.
		if strings.HasPrefix(raw, "\t") || strings.HasPrefix(raw, "    ") {
			if cur != nil && cur.Command == "" {
				cmd := strings.TrimSpace(raw)
				// The leading `@`, `-`, and `+` modify how make runs the line, not what
				// it runs.
				cur.Command = strings.TrimLeft(cmd, "@-+ ")
			}
			continue
		}
		line := stripMakeComment(raw)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, ".PHONY:"); ok {
			// A phony target's name is a command rather than a file it produces, which is
			// the distinction between "run this" and "this gets built". Collected here and
			// applied after the walk, since .PHONY may be declared before or after the
			// targets it names — both conventions are in wide use.
			for _, t := range strings.Fields(rest) {
				phony[t] = true
			}
			continue
		}
		// A variable assignment, not a target. Checked before the colon rule because
		// `VAR := value` contains one.
		if isMakeAssignment(trimmed) {
			continue
		}
		name, deps, ok := makeTarget(trimmed)
		if !ok {
			continue
		}
		facts.Scripts = append(facts.Scripts, Script{Name: name, Line: num})
		cur = &facts.Scripts[len(facts.Scripts)-1]
		if deps != "" {
			// Prerequisites are the order the build imposes — `release: test lint` says
			// the gate runs first — which is a different fact from the recipe and gets
			// its own record rather than competing for Command.
			facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
				Name: name, Path: deps, Line: num,
			})
		}
	}
	// A phony target is one a person runs; a file target is one the build produces. The
	// entrypoints are the runnable set, which is the answer to "how do I test this".
	for _, s := range facts.Scripts {
		if phony[s.Name] {
			facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
				Name: s.Name, Path: f.Path, Line: s.Line,
			})
		}
	}
	if len(facts.Scripts) == 0 {
		facts.markIncomplete("no targets found")
	}
	return facts
}

// makeTarget reads a target line, returning its name and prerequisites.
//
// Only the first name is taken when a rule declares several. A multi-target rule shares
// one recipe, and recording the same recipe under each name would report several build
// steps where there is one.
func makeTarget(line string) (string, string, bool) {
	colon := strings.IndexByte(line, ':')
	if colon <= 0 {
		return "", "", false
	}
	name := strings.TrimSpace(line[:colon])
	// A pattern rule (`%.o: %.c`) is a build mechanism rather than an entry point, and
	// naming it as a script would offer a reader a command they cannot run.
	if name == "" || strings.ContainsAny(name, "%$ \t") {
		return "", "", false
	}
	deps := strings.TrimSpace(strings.TrimPrefix(line[colon+1:], ":"))
	return name, deps, true
}

// isMakeAssignment reports whether a line assigns a variable rather than declaring a
// target.
func isMakeAssignment(line string) bool {
	for _, op := range []string{":=", "::=", "?=", "+=", "!="} {
		if i := strings.Index(line, op); i > 0 {
			// `a: b = c` is a target; `a := b` is an assignment. The distinguishing
			// feature is whether a bare colon comes first.
			if c := strings.IndexByte(line, ':'); c < i || strings.HasPrefix(line[c:], op) {
				return true
			}
		}
	}
	if i := strings.IndexByte(line, '='); i > 0 {
		return !strings.Contains(line[:i], ":")
	}
	return false
}

// stripMakeComment removes a `#` comment.
func stripMakeComment(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] == '\\' {
			i++
			continue
		}
		if line[i] == '#' {
			return line[:i]
		}
	}
	return line
}
