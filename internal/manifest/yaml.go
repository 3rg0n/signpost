package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// A tolerant reader for the YAML subset that configuration files actually use.
//
// # Why this is hand-written rather than a library call
//
// Design §6.3 chooses gopkg.in/yaml.v3 for *reading frontmatter a human has
// edited*, and that reasoning is sound and unchanged: round-tripping someone's
// hand-written `verified:` block without mangling their comments and quoting is
// exactly where hand-rolling gets dangerous. This reader is for a different job
// with a different failure mode, and one hard requirement a conforming parser
// cannot meet:
//
//   - **Helm templates are not YAML.** `templates/deployment.yaml` is a Go template
//     that *produces* YAML. `{{- if .Values.ingress.enabled }}` is a syntax error to
//     any conforming parser, and Helm charts are explicitly in scope (design §4.1).
//     A reader that fails on them cannot read the thing it was added to read.
//   - **A repository is untrusted input** (design §4.5, §4.0). Every file here was
//     written by someone else, and a single malformed workflow must degrade to
//     "these facts are incomplete, here is why" rather than failing a build. A
//     conforming parser's contract is the opposite: it reports an error and yields
//     nothing.
//   - Nothing is round-tripped. Facts are read out; the file is never rewritten. The
//     risk that motivates §6.3 — silently mangling a human's text on the way back
//     out — does not exist on this path.
//
// So the split is three ways, not two: this tolerant reader for repository
// configuration, yaml.v3 for frontmatter round-tripping (§6.3), and the
// hand-written emitter for byte-stable output (§6.3).
//
// # What is supported, and what a caller is told
//
// Block mappings and sequences by indentation, flow mappings and sequences
// including across lines, single- and double-quoted scalars with escapes, literal
// and folded block scalars with chomping, comments, multiple documents, anchors,
// aliases, and merge keys — anchors and merges because compose files use
// `x-common: &common` heavily and a reader that ignored them would report a service
// as having no environment at all.
//
// Everything not supported is *recorded*, never silently dropped: complex keys,
// unresolved aliases, tab indentation, and template directives all append to Diag,
// which the extractors surface as Facts.Incomplete. That is the §4.2 rule applied to
// this package — absence of measurement is never presented as a clean bill.
type yamlParser struct {
	lines   []yamlLine
	i       int
	anchors map[string]*Node
	diag    *Diag
}

// yamlLine is one physical line, kept raw because a block scalar's content is
// verbatim: comments inside a `run: |` block are shell comments, not YAML ones.
type yamlLine struct {
	raw    string
	indent int
	num    int
}

// Diag records constructs a reader could not fully interpret.
//
// This exists so that "the reader handled everything it saw" and "the reader saw
// something it does not support" are distinguishable facts rather than the same
// silence. An extractor turns a non-empty Diag into Facts.Incomplete with the notes
// attached, which is what keeps a partially-read Helm template from being presented
// as a complete reading of the chart.
type Diag struct {
	Notes []string
	// Malformed marks the subset of notes that mean the *document* is broken rather than
	// that this reader stepped over something it does not support.
	//
	// The distinction is not cosmetic, and it exists because of what a caller does with it.
	// Most notes are tolerance working as designed (ADR 0001): a Helm template directive or
	// a tab-indented block is something this reader cannot interpret, but the file is still
	// valid input and every other reader will agree with what was read. An unterminated flow
	// collection is different in kind — the YAML is not parseable *by anything*, so a
	// conforming reader loses every entry after that point. A caller that treats both the
	// same either fails builds over a template it was designed to tolerate, or reports an
	// unreadable document as a nit. Both happened: see issue #9.
	Malformed bool
}

// maxDiagNotes caps the record. A file with a thousand template directives has one
// problem, not a thousand, and an unbounded list would put a wall of text in the
// manifest.
const maxDiagNotes = 20

// note records an unsupported construct, deduplicated by message so a repeated
// directive does not fill the cap.
func (d *Diag) note(line int, msg string) {
	if d == nil {
		return
	}
	entry := fmt.Sprintf("line %d: %s", line, msg)
	for _, n := range d.Notes {
		// Compare on the message, not the line, so the same construct repeated 40
		// times is recorded once with its first location.
		if strings.HasSuffix(n, ": "+msg) || n == entry {
			return
		}
	}
	if len(d.Notes) >= maxDiagNotes {
		return
	}
	d.Notes = append(d.Notes, entry)
}

// malformed records a note that additionally means the document is not parseable.
//
// The flag is set even when the note itself is dropped by maxDiagNotes above. A capped note
// costs a location; a dropped flag would silently turn an unreadable document back into a
// clean one, which is the failure this field exists to prevent.
func (d *Diag) malformed(line int, msg string) {
	if d == nil {
		return
	}
	d.note(line, msg)
	d.Malformed = true
}

// Incomplete reports whether anything went unread.
func (d Diag) Incomplete() bool { return len(d.Notes) > 0 }

// Summary renders the notes as one string for Facts.Note.
func (d Diag) Summary() string {
	if len(d.Notes) == 0 {
		return ""
	}
	return strings.Join(d.Notes, "; ")
}

// ParseYAML parses a YAML stream into one Node per document.
//
// Never returns an error. A file it cannot read yields the documents it could plus
// a Diag explaining the rest, because the alternative — one bad file failing the
// build — makes the tool unusable on the repositories it exists for.
func ParseYAML(src string) ([]*Node, Diag) {
	var diag Diag
	var docs []*Node
	for _, doc := range splitYAMLDocs(src) {
		p := &yamlParser{lines: doc, anchors: map[string]*Node{}, diag: &diag}
		n := p.parseBlock(0)
		if n == nil {
			// An empty document is legal YAML and carries no facts. Skipping it is
			// not a loss, so it is not a diagnostic either.
			continue
		}
		docs = append(docs, n)
	}
	return docs, diag
}

// ParseYAMLDoc parses the first document, for the single-document files that most
// manifests are.
func ParseYAMLDoc(src string) (*Node, Diag) {
	docs, diag := ParseYAML(src)
	if len(docs) == 0 {
		return nil, diag
	}
	return docs[0], diag
}

// splitYAMLDocs splits a stream on document markers.
//
// A marker is recognised only at column 0. That restriction is what makes this safe
// in the presence of block scalars: a `---` inside a `run: |` script is always
// indented under its key, so it cannot be mistaken for a document boundary.
func splitYAMLDocs(src string) [][]yamlLine {
	raw := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var docs [][]yamlLine
	cur := []yamlLine{}
	flush := func() {
		if len(cur) > 0 {
			docs = append(docs, cur)
			cur = []yamlLine{}
		}
	}
	for i, line := range raw {
		trimmed := strings.TrimRight(line, " \t")
		switch {
		case trimmed == "---" || strings.HasPrefix(trimmed, "--- "):
			flush()
			// Content on the marker line itself (`--- key: value`) belongs to the new
			// document.
			if rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "---")); rest != "" {
				cur = append(cur, yamlLine{raw: rest, indent: 0, num: i + 1})
			}
			continue
		case trimmed == "...":
			flush()
			continue
		}
		cur = append(cur, yamlLine{raw: line, indent: yamlIndent(line), num: i + 1})
	}
	flush()
	return docs
}

// yamlIndent measures leading whitespace.
//
// A tab is counted as one column rather than expanded. YAML forbids tabs in
// indentation entirely, so any file using them is already outside the spec; the goal
// here is only to keep relative depth consistent so a tab-indented file still yields
// its structure. The parser notes it.
func yamlIndent(line string) int {
	for i := 0; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			return i
		}
	}
	return len(line)
}

// content returns line i with its comment removed and surrounding space trimmed.
func (p *yamlParser) content(i int) string {
	return strings.TrimSpace(stripYAMLComment(p.lines[i].raw))
}

// skipIgnorable advances past blank lines, comment-only lines, and Go template
// directives.
//
// The template case is the Helm one. A line whose content begins with `{{` is a
// control directive — `{{- if }}`, `{{- end }}`, `{{- range }}` — that produces no
// YAML of its own. Skipping it lets the surrounding keys be read; it also means the
// structure read is the template's *unconditional* skeleton, which is why it is
// recorded in Diag rather than passed over quietly.
func (p *yamlParser) skipIgnorable() {
	for p.i < len(p.lines) {
		c := p.content(p.i)
		switch {
		case c == "":
			p.i++
		case strings.HasPrefix(c, "{{"):
			p.diag.note(p.lines[p.i].num, "template directive skipped; structure read is the unconditional skeleton")
			p.i++
		default:
			if strings.ContainsRune(p.lines[p.i].raw[:p.lines[p.i].indent], '\t') {
				p.diag.note(p.lines[p.i].num, "tab indentation is invalid YAML; depth read positionally")
			}
			return
		}
	}
}

// parseBlock parses whatever block-level value begins at or after the cursor,
// provided it is indented at least minIndent.
func (p *yamlParser) parseBlock(minIndent int) *Node {
	p.skipIgnorable()
	if p.i >= len(p.lines) {
		return nil
	}
	ln := p.lines[p.i]
	if ln.indent < minIndent {
		return nil
	}
	c := p.content(p.i)
	if isSeqEntry(c) {
		return p.parseSeq(ln.indent)
	}
	if _, _, ok := splitYAMLKey(c); ok {
		return p.parseMap(ln.indent)
	}
	// Neither a key nor a dash: a bare scalar document, such as a values file that
	// is a single string. Flow collections are handled here too.
	p.i++
	return p.parseValue(c, ln.indent, ln.num)
}

// parseSeq parses a block sequence whose dashes sit at the given indent.
func (p *yamlParser) parseSeq(indent int) *Node {
	n := seqNode(p.lines[p.i].num)
	for {
		p.skipIgnorable()
		if p.i >= len(p.lines) {
			break
		}
		ln := p.lines[p.i]
		if ln.indent != indent {
			break
		}
		c := p.content(p.i)
		if !isSeqEntry(c) {
			break
		}
		dash := strings.IndexByte(ln.raw, '-')
		rest := strings.TrimSpace(c[1:])
		if rest == "" {
			// The item's value is on the following, more-indented lines.
			p.i++
			item := p.parseBlock(indent + 1)
			if item == nil {
				item = scalarNode("", false, ln.num)
			}
			n.Items = append(n.Items, item)
			continue
		}
		// Content sits on the dash line. Blanking the dash in place turns
		// `- name: x` into `  name: x` at the exact column the item's keys align on,
		// so the ordinary block parser handles the item — including the common
		// `- name: x` / `  value: y` pair, and nested `- - a` sequences — with no
		// separate code path for "first entry was inline".
		p.lines[p.i].raw = ln.raw[:dash] + " " + ln.raw[dash+1:]
		p.lines[p.i].indent = yamlIndent(p.lines[p.i].raw)
		item := p.parseBlock(dash + 1)
		if item == nil {
			item = scalarNode("", false, ln.num)
		}
		n.Items = append(n.Items, item)
	}
	return n
}

// parseMap parses a block mapping whose keys sit at the given indent.
func (p *yamlParser) parseMap(indent int) *Node {
	n := mapNode(p.lines[p.i].num)
	for {
		p.skipIgnorable()
		if p.i >= len(p.lines) {
			break
		}
		ln := p.lines[p.i]
		if ln.indent != indent {
			break
		}
		c := p.content(p.i)
		if isSeqEntry(c) {
			// A sequence at the same indent as the mapping's keys. Legal YAML only as
			// a value of the preceding key, which parseValue already consumed, so
			// reaching here means the document is malformed. Stop rather than guess
			// which key it belongs to.
			break
		}
		if strings.HasPrefix(c, "? ") {
			p.diag.note(ln.num, "explicit complex key not supported")
			p.i++
			continue
		}
		key, rest, ok := splitYAMLKey(c)
		if !ok {
			p.diag.note(ln.num, "line is neither a mapping entry nor a sequence entry")
			p.i++
			continue
		}
		p.i++
		val := p.parseValue(rest, indent, ln.num)
		if key == "<<" {
			p.mergeInto(n, val, ln.num)
			continue
		}
		n.set(key, val)
	}
	return n
}

// mergeInto applies a YAML merge key.
//
// Existing keys are never overwritten, which is the spec's own precedence rule:
// entries written explicitly in a mapping beat entries it inherits. Since `<<`
// conventionally appears first, and a later explicit key overwrites via set, both
// orderings come out correct.
func (p *yamlParser) mergeInto(dst *Node, src *Node, line int) {
	if src == nil {
		return
	}
	// `<<: [*a, *b]` merges several, earlier winning.
	if src.Kind == KindSeq {
		for _, item := range src.Items {
			p.mergeInto(dst, item, line)
		}
		return
	}
	if src.Kind != KindMap {
		p.diag.note(line, "merge key value is not a mapping")
		return
	}
	src.Each(func(k string, v *Node) bool {
		if dst.Get(k) == nil {
			dst.set(k, v)
		}
		return true
	})
}

// parseValue interprets the text after a `key:` — or after a sequence dash — and
// consumes any continuation lines the value needs.
func (p *yamlParser) parseValue(rest string, parentIndent, line int) *Node {
	rest = strings.TrimSpace(rest)

	// An anchor definition may precede the value: `key: &name value`, or
	// `key: &name` with the value on following lines.
	anchor := ""
	if strings.HasPrefix(rest, "&") {
		name, after := splitFirstToken(rest[1:])
		anchor = name
		rest = strings.TrimSpace(after)
	}

	node := p.parseValueBody(rest, parentIndent, line)
	if anchor != "" && node != nil {
		p.anchors[anchor] = node
	}
	return node
}

// parseValueBody is parseValue with the anchor already stripped.
func (p *yamlParser) parseValueBody(rest string, parentIndent, line int) *Node {
	switch {
	case rest == "":
		// The value is a nested block, or nothing at all.
		if v := p.parseBlock(parentIndent + 1); v != nil {
			return v
		}
		return scalarNode("", false, line)

	case strings.HasPrefix(rest, "*"):
		name, _ := splitFirstToken(rest[1:])
		if v, ok := p.anchors[name]; ok {
			return v
		}
		p.diag.note(line, "alias *"+name+" refers to an anchor not seen")
		return scalarNode("", false, line)

	case isBlockScalarHeader(rest):
		return p.parseBlockScalar(rest, parentIndent, line)

	// The template case must be tested before the flow-mapping one, since `{{` also
	// starts with `{`. Reading `image: {{ .Values.image }}` as a flow mapping would
	// yield a nested map with a key nobody wrote, in place of the value's text.
	case strings.HasPrefix(rest, "{{"):
		// A templated value. Its text is kept verbatim: `image: {{ .Values.image }}`
		// still says the container's image comes from a value, which is the fact.
		p.diag.note(line, "templated value kept as literal text")
		return scalarNode(rest, false, line)

	case strings.HasPrefix(rest, "{") || strings.HasPrefix(rest, "["):
		joined := p.joinFlow(rest)
		n, _ := parseFlow(joined, 0, line, p.diag)
		if n == nil {
			return scalarNode("", false, line)
		}
		return n
	}
	s, quoted := unquoteYAMLScalar(rest)
	return scalarNode(s, quoted, line)
}

// joinFlow extends a flow collection across lines until its brackets balance.
//
// A formatter wrapping a long `on: [push, pull_request]` across lines is ordinary,
// and reading only the first line would report half the triggers.
func (p *yamlParser) joinFlow(first string) string {
	depth := flowBalance(first, 0)
	joined := first
	const maxJoin = 200
	for n := 0; depth > 0 && p.i < len(p.lines) && n < maxJoin; n++ {
		c := p.content(p.i)
		p.i++
		if c == "" {
			continue
		}
		joined += " " + c
		depth = flowBalance(c, depth)
	}
	return joined
}

// parseBlockScalar consumes a literal (`|`) or folded (`>`) block.
//
// Worth doing properly rather than skipping: `run: |` is where a GitHub Actions
// workflow says what it actually executes, and the workflow extractor's whole
// contribution is reporting how a repository builds, tests, and ships.
func (p *yamlParser) parseBlockScalar(header string, parentIndent, line int) *Node {
	folded := strings.HasPrefix(header, ">")
	chomp := ' '
	for _, c := range header[1:] {
		if c == '-' || c == '+' {
			chomp = c
		}
	}

	// Content is every following line indented past the key, plus any blank line
	// among them. Blanks inside a block are content, not terminators, so the
	// scan looks ahead past them rather than stopping.
	var body []string
	blockIndent := -1
	for j := p.i; j < len(p.lines); j++ {
		ln := p.lines[j]
		if strings.TrimSpace(ln.raw) == "" {
			body = append(body, "")
			continue
		}
		if ln.indent <= parentIndent {
			break
		}
		if blockIndent < 0 {
			blockIndent = ln.indent
		}
		strip := blockIndent
		if ln.indent < strip {
			strip = ln.indent
		}
		body = append(body, ln.raw[strip:])
		p.i = j + 1
	}
	// Trailing blanks belong to the chomping decision, not to the content.
	for len(body) > 0 && body[len(body)-1] == "" {
		body = body[:len(body)-1]
	}

	var text string
	if folded {
		text = foldLines(body)
	} else {
		text = strings.Join(body, "\n")
	}
	switch chomp {
	case '-':
		// Strip: no trailing newline.
	case '+':
		text += "\n"
	default:
		// Clip: exactly one trailing newline, when there was any content.
		if text != "" {
			text += "\n"
		}
	}
	return scalarNode(text, true, line)
}

// foldLines implements folded-scalar semantics: a single newline between two
// non-empty lines becomes a space, a blank line becomes a newline, and a more-
// indented line keeps its breaks.
func foldLines(body []string) string {
	var b strings.Builder
	for i, l := range body {
		if i == 0 {
			b.WriteString(l)
			continue
		}
		prev := body[i-1]
		switch {
		case l == "":
			b.WriteString("\n")
		case prev == "":
			b.WriteString(l)
		case startsIndented(l) || startsIndented(prev):
			b.WriteString("\n")
			b.WriteString(l)
		default:
			b.WriteString(" ")
			b.WriteString(l)
		}
	}
	return b.String()
}

func startsIndented(s string) bool {
	return s != "" && (s[0] == ' ' || s[0] == '\t')
}

// isBlockScalarHeader reports whether a value is a block scalar indicator, being
// careful not to match a plain scalar that merely starts with the character —
// `command: > /dev/null` is not a folded block, and `key: |grep` is not a literal
// one.
func isBlockScalarHeader(s string) bool {
	if s == "" || (s[0] != '|' && s[0] != '>') {
		return false
	}
	for i := 1; i < len(s); i++ {
		switch {
		case s[i] == '-' || s[i] == '+':
		case s[i] >= '0' && s[i] <= '9':
		default:
			return false
		}
	}
	return true
}

// isSeqEntry reports whether a line's content is a block sequence entry.
func isSeqEntry(c string) bool {
	return c == "-" || strings.HasPrefix(c, "- ") || strings.HasPrefix(c, "-\t")
}

// splitFirstToken splits off a leading non-space token.
func splitFirstToken(s string) (string, string) {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		return s[:i], s[i:]
	}
	return s, ""
}

// stripYAMLComment removes a trailing comment, respecting quotes.
//
// A `#` only begins a comment when it is at the start of the content or preceded by
// whitespace. Without that rule an image tag such as `nginx#sha` or a URL fragment
// would be truncated — and a truncated image reference is a wrong fact, not a
// missing one.
func stripYAMLComment(line string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inDouble:
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inDouble = false
			}
		case inSingle:
			if c == '\'' {
				// '' is an escaped quote inside a single-quoted scalar.
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
			}
		case c == '"':
			inDouble = true
		case c == '\'':
			inSingle = true
		case c == '#':
			if i == 0 || line[i-1] == ' ' || line[i-1] == '\t' {
				return line[:i]
			}
		}
	}
	return line
}

// splitYAMLKey splits `key: rest`, reporting whether the line is a mapping entry.
//
// The colon must be followed by whitespace or end the line. That is the spec's rule
// and it is load-bearing rather than pedantic: without it every `https://host` in a
// values file becomes a mapping with the key `https`, and the extractor above it
// starts reporting keys nobody wrote.
func splitYAMLKey(c string) (key, rest string, ok bool) {
	if c == "" {
		return "", "", false
	}
	// A quoted key may contain a colon of its own.
	if c[0] == '"' || c[0] == '\'' {
		q := c[0]
		end := -1
		for i := 1; i < len(c); i++ {
			if q == '"' && c[i] == '\\' {
				i++
				continue
			}
			if c[i] == q {
				if q == '\'' && i+1 < len(c) && c[i+1] == '\'' {
					i++
					continue
				}
				end = i
				break
			}
		}
		if end < 0 {
			return "", "", false
		}
		after := strings.TrimSpace(c[end+1:])
		if !strings.HasPrefix(after, ":") {
			return "", "", false
		}
		k, _ := unquoteYAMLScalar(c[:end+1])
		return k, strings.TrimSpace(after[1:]), true
	}
	if c[0] == '{' || c[0] == '[' {
		return "", "", false
	}
	depth := 0
	for i := 0; i < len(c); i++ {
		switch c[i] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case ':':
			if depth > 0 {
				continue
			}
			if i+1 == len(c) || c[i+1] == ' ' || c[i+1] == '\t' {
				k := strings.TrimSpace(c[:i])
				if k == "" {
					return "", "", false
				}
				return k, strings.TrimSpace(c[i+1:]), true
			}
		}
	}
	return "", "", false
}

// unquoteYAMLScalar resolves a scalar's quoting, reporting whether it was quoted.
//
// Quotedness is retained because it carries meaning a caller needs: in a workflow,
// `python-version: 3.10` is the number 3.1 to a YAML parser while
// `python-version: "3.10"` is the version string, and a matrix reported as testing
// Python 3.1 is wrong in a way that matters.
func unquoteYAMLScalar(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return unescapeYAMLDouble(s[1 : len(s)-1]), true
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'"), true
	}
	switch s {
	case "null", "Null", "NULL", "~":
		return "", false
	}
	return s, false
}

// unescapeYAMLDouble resolves the escapes a double-quoted scalar can carry.
//
// Only the ones that appear in configuration are handled; a `\u` sequence is left
// as written rather than decoded, because a wrongly decoded escape is a wrong value
// while an undecoded one is visibly literal.
func unescapeYAMLDouble(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '0':
			b.WriteByte(0)
		case '"', '\\', '/', '\'':
			b.WriteByte(s[i])
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// flowBalance returns the bracket depth after scanning s, starting from depth.
// Quoted text is skipped so a bracket in a string cannot skew it.
func flowBalance(s string, depth int) int {
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inDouble:
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inDouble = false
			}
		case inSingle:
			if c == '\'' {
				inSingle = false
			}
		case c == '"':
			inDouble = true
		case c == '\'':
			inSingle = true
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			depth--
		}
	}
	if depth < 0 {
		return 0
	}
	return depth
}

// parseFlow parses a flow collection or scalar beginning at s[at], returning the
// node and the index just past it.
func parseFlow(s string, at int, line int, diag *Diag) (*Node, int) {
	at = skipFlowSpace(s, at)
	if at >= len(s) {
		return nil, at
	}
	switch s[at] {
	case '{':
		n := mapNode(line)
		at++
		for {
			at = skipFlowSpace(s, at)
			if at >= len(s) {
				diag.malformed(line, "unterminated flow mapping")
				return n, at
			}
			if s[at] == '}' {
				return n, at + 1
			}
			if s[at] == ',' {
				at++
				continue
			}
			before := at
			key, next := readFlowScalar(s, at)
			at = skipFlowSpace(s, next)
			if at < len(s) && s[at] == ':' {
				at++
				var val *Node
				val, at = parseFlow(s, at, line, diag)
				if val == nil {
					val = scalarNode("", false, line)
				}
				k, _ := unquoteYAMLScalar(key)
				n.set(k, val)
				continue
			}
			// A flow mapping entry with no value: `{a, b}` is a set in YAML.
			k, _ := unquoteYAMLScalar(key)
			n.set(k, scalarNode("", false, line))
			// Same guard as the sequence branch: an entry that consumed nothing would
			// loop forever rather than misread one file.
			if at <= before {
				diag.malformed(line, "flow mapping entry made no progress")
				return n, len(s)
			}
		}
	case '[':
		n := seqNode(line)
		at++
		for {
			at = skipFlowSpace(s, at)
			if at >= len(s) {
				diag.malformed(line, "unterminated flow sequence")
				return n, at
			}
			if s[at] == ']' {
				return n, at + 1
			}
			if s[at] == ',' {
				at++
				continue
			}
			var item *Node
			before := at
			item, at = parseFlow(s, at, line, diag)
			if item == nil {
				return n, at
			}
			// A single-pair mapping written without braces: `[main: {}]` and
			// `- [a: 1, b: 2]` are both legal, and the element is a mapping rather than
			// the scalar just read.
			if next := skipFlowSpace(s, at); next < len(s) && s[next] == ':' && flowColonIsIndicator(s, next) {
				pair := mapNode(line)
				var val *Node
				val, at = parseFlow(s, next+1, line, diag)
				if val == nil {
					val = scalarNode("", false, line)
				}
				pair.set(item.Str, val)
				item = pair
			}
			// Every branch above consumes at least the byte it examined, so this cannot
			// trigger today. It is here because it is the difference between a
			// misreading and a hang: a scanner that returns a node without advancing
			// turns one unanticipated shape into an infinite loop, and this reader runs
			// over whatever YAML a repository happens to contain.
			if at <= before {
				diag.malformed(line, "flow sequence element made no progress")
				return n, len(s)
			}
			n.Items = append(n.Items, item)
		}
	}
	text, next := readFlowScalar(s, at)
	v, quoted := unquoteYAMLScalar(text)
	return scalarNode(v, quoted, line), next
}

func skipFlowSpace(s string, at int) int {
	for at < len(s) && (s[at] == ' ' || s[at] == '\t') {
		at++
	}
	return at
}

// readFlowScalar reads one scalar inside a flow collection, stopping at the
// separators that end it.
func readFlowScalar(s string, at int) (string, int) {
	if at < len(s) && (s[at] == '"' || s[at] == '\'') {
		q := s[at]
		for i := at + 1; i < len(s); i++ {
			if q == '"' && s[i] == '\\' {
				i++
				continue
			}
			if s[i] == q {
				if q == '\'' && i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				return s[at : i+1], i + 1
			}
		}
		return s[at:], len(s)
	}
	start := at
	for at < len(s) && s[at] != ',' && s[at] != '}' && s[at] != ']' {
		if s[at] == ':' && flowColonIsIndicator(s, at) {
			break
		}
		at++
	}
	return strings.TrimSpace(s[start:at]), at
}

// flowColonIsIndicator reports whether the colon at s[at] separates a key from a value
// rather than sitting inside a plain scalar.
//
// YAML 1.2 makes `:` an indicator in flow context only when a space or a flow indicator
// follows it. That rule is not pedantry here — it is what makes `[things:read]` one
// scope name rather than a key with no value, and `8080:8080` one port mapping rather
// than two numbers. A reader that stopped at every colon returned an empty scalar and
// consumed nothing, which is a hang rather than a misreading: the caller's loop saw no
// progress and no terminator.
//
// A colon after a quoted key does not reach here, because the closing quote ends the
// scalar first — which is what lets JSON-style `{"a":1}` parse with no special case.
func flowColonIsIndicator(s string, at int) bool {
	if at+1 >= len(s) {
		return true
	}
	switch s[at+1] {
	case ' ', '\t', ',', '}', ']':
		return true
	}
	return false
}

// sortedUnique sorts and dedupes, for the fact lists every extractor here emits.
func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
