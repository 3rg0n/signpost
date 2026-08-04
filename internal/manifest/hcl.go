package manifest

import "strings"

// HCL block reading.
//
// Enough of HCL to read block headers and top-level attributes, and deliberately no
// more. The argument is the same one ADR 0001 makes about YAML: the facts worth
// extracting from a Terraform configuration live in the block header — `resource "type"
// "name"`, `module "vpc"` — and in a handful of literal attributes beside it, `source`,
// `version`, `sensitive`. Those are the two parts of the grammar with no ambiguity in
// them. Everything else in an HCL file is an expression language with functions, splats,
// conditionals, and `for` comprehensions, and evaluating it needs a real interpreter with
// variable bindings this tool does not have and should not want.
//
// So expressions are carried as the text that was written and never evaluated. A reader
// asking for `source` gets the string when a string is what was written, and nothing when
// the value is `var.module_source` — because "nothing" is true and a guess is not.
//
// What this parser does have to get right is *structure*: which brace closes which block,
// so that a nested `lifecycle { }` inside a resource does not read as a top-level block
// and an attribute inside it does not read as the resource's own. That means tracking
// strings, interpolations, heredocs, comments, and bracket depth — the four places where
// a naive brace count goes wrong. Each is handled below and each has a corpus fixture.

// hclAttr is one `key = value` assignment. Value is the expression text as written,
// unevaluated and untrimmed of its own syntax.
type hclAttr struct {
	key   string
	value string
	line  int
}

// hclBlock is one `kind "label" "label" { ... }` block.
type hclBlock struct {
	kind   string
	labels []string
	attrs  []hclAttr
	blocks []hclBlock
	line   int
}

// label returns the i-th block label, or "" when the block has fewer.
//
// Indexed access rather than a slice, because every caller wants a specific position —
// a resource's type is label 0 and its name is label 1 — and a caller that indexed
// directly would panic on the malformed `resource "aws_instance" {` that a real
// repository contains.
func (b hclBlock) label(i int) string {
	if i < 0 || i >= len(b.labels) {
		return ""
	}
	return b.labels[i]
}

func (b hclBlock) attr(key string) (hclAttr, bool) {
	for _, a := range b.attrs {
		if a.key == key {
			return a, true
		}
	}
	return hclAttr{}, false
}

// stringAttr returns an attribute's value when it was written as a literal string, and
// "" for anything else — an expression, a number, a missing key.
//
// Collapsing those three into one empty result is intentional. Every caller here is
// asking "did the author state this as a fact I can record", and an expression is not a
// stated fact; it is an instruction to compute one at apply time.
func (b hclBlock) stringAttr(key string) string {
	a, ok := b.attr(key)
	if !ok {
		return ""
	}
	return hclString(a.value)
}

// boolAttr reports whether an attribute is the literal `true`.
//
// A non-literal — `sensitive = var.is_prod` — is false, which is the safe direction for
// the one caller that matters: a variable whose sensitivity is conditional still gets
// name-only treatment, because no value is ever read from any variable regardless.
func (b hclBlock) boolAttr(key string) bool {
	a, ok := b.attr(key)
	return ok && strings.TrimSpace(a.value) == "true"
}

// maxHCLDepth bounds recursion.
//
// A guard on input, not on style. This parser reads files out of an arbitrary repository,
// and a file consisting of ten thousand open braces would recurse once per brace and take
// the process down with a stack overflow — a crash reachable by any contributor who can
// add a file, which is a denial of service in a tool that runs in CI. Past the limit the
// block is skipped by brace counting instead, and the diagnostic says so.
const maxHCLDepth = 64

// parseHCL reads a file into a root pseudo-block: its attributes are the file's top-level
// assignments, which is what a .tfvars file consists of, and its blocks are the file's
// top-level blocks.
//
// Never returns an error, matching every other reader in this package (see the Reader
// contract in registry.go): a file that goes wrong halfway yields what came before it plus
// a Diag saying where it stopped.
func parseHCL(src string) (hclBlock, Diag) {
	p := &hclParser{src: src, line: 1}
	root := hclBlock{}
	root.attrs, root.blocks = p.body(0)
	return root, p.diag
}

type hclParser struct {
	src  string
	i    int
	line int
	diag Diag
}

func (p *hclParser) eof() bool { return p.i >= len(p.src) }

func (p *hclParser) peek() byte {
	if p.eof() {
		return 0
	}
	return p.src[p.i]
}

// next consumes one byte and is the only place the line counter moves, so a newline
// inside a string or a heredoc counts exactly like any other.
func (p *hclParser) next() byte {
	c := p.src[p.i]
	p.i++
	if c == '\n' {
		p.line++
	}
	return c
}

// body reads statements until the `}` that closes this block, or end of file at depth 0.
func (p *hclParser) body(depth int) (attrs []hclAttr, blocks []hclBlock) {
	for {
		p.skipBlank()
		if p.eof() {
			if depth > 0 {
				p.diag.malformed(p.line, "unterminated block at end of file")
			}
			return attrs, blocks
		}
		if p.peek() == '}' {
			p.next()
			if depth == 0 {
				p.diag.malformed(p.line, "unmatched closing brace")
				continue
			}
			return attrs, blocks
		}
		line := p.line
		head, stop := p.header()
		switch stop {
		case '=':
			key := strings.TrimSpace(head)
			value := p.value()
			if key == "" || !isHCLIdent(key) {
				// A left-hand side that is not a plain name is an indexed or quoted
				// assignment — `tags["env"] = "x"`. Recorded as unread rather than
				// guessed at, since the key is computed.
				p.diag.note(line, "assignment to a computed name")
				continue
			}
			attrs = append(attrs, hclAttr{key: key, value: value, line: line})
		case '{':
			b := hclBlock{line: line}
			b.kind, b.labels = splitHCLHeader(head)
			if b.kind == "" {
				p.diag.note(line, "block with no type")
			}
			if depth >= maxHCLDepth {
				p.diag.note(line, "block nested deeper than the reader follows")
				p.skipBalanced()
				continue
			}
			b.attrs, b.blocks = p.body(depth + 1)
			blocks = append(blocks, b)
		default:
			// A statement that ended without `=` or `{`. Bare text at block level is
			// not valid HCL, so this is either a malformed file or a construct outside
			// what the header scanner models; either way it is worth saying so.
			if strings.TrimSpace(head) != "" {
				p.diag.note(line, "unread statement "+firstWord(head))
			}
		}
	}
}

// header reads a statement's left side, stopping at the `=` that makes it an attribute,
// the `{` that makes it a block, or the newline that makes it neither. The stop byte is
// returned and has been consumed; 0 means end of file.
func (p *hclParser) header() (string, byte) {
	var sb strings.Builder
	for !p.eof() {
		c := p.peek()
		switch {
		case c == '=':
			p.next()
			return sb.String(), '='
		case c == '{':
			p.next()
			return sb.String(), '{'
		case c == '\n':
			p.next()
			return sb.String(), '\n'
		case c == '}':
			// Not consumed: the caller's loop needs to see it to close the block.
			return sb.String(), '\n'
		case c == '"':
			sb.WriteString(p.stringLiteral())
		case p.atComment():
			p.skipComment()
			return sb.String(), '\n'
		default:
			sb.WriteByte(p.next())
		}
	}
	return sb.String(), 0
}

// value reads an expression: everything up to a newline or comma outside brackets, or the
// `}` that closes the enclosing block on the same line.
//
// Bracket depth is what makes a multi-line list or object one value rather than several
// broken ones, and it is why this cannot be a line-oriented reader the way the
// Containerfile one is.
func (p *hclParser) value() string {
	var sb strings.Builder
	depth := 0
	for !p.eof() {
		c := p.peek()
		switch {
		case c == '"':
			sb.WriteString(p.stringLiteral())
			continue
		case c == '<' && strings.HasPrefix(p.src[p.i:], "<<"):
			sb.WriteString(p.heredoc())
			continue
		case p.atComment():
			p.skipComment()
			if depth == 0 {
				return strings.TrimSpace(sb.String())
			}
			sb.WriteByte(' ')
			continue
		case c == '{' || c == '[' || c == '(':
			depth++
		case c == '}' || c == ']' || c == ')':
			if depth == 0 {
				// The enclosing block's closing brace, left for the caller.
				return strings.TrimSpace(sb.String())
			}
			depth--
		case c == '\n' || c == ',':
			if depth == 0 {
				p.next()
				return strings.TrimSpace(sb.String())
			}
			// Inside brackets the newline is kept rather than folded to a space, because
			// in an object literal it is a *separator*: HCL ends an object's element at a
			// comma or a newline, exactly as it ends a statement. Collapsing it read
			//
			//	aws = { source = "hashicorp/aws"
			//	        version = "~> 5.31" }
			//
			// as one element whose value ran from the first quote to the last, and the
			// aws provider's source came back as `hashicorp/aws" version = "~> 5.31`.
			// hclObjectField reparses this text, so the separators have to survive.
		}
		sb.WriteByte(p.next())
	}
	return strings.TrimSpace(sb.String())
}

// stringLiteral consumes a quoted string and returns it with its quotes, so callers that
// are accumulating expression text keep what was written and hclString can unquote later.
//
// Interpolation is tracked rather than skipped: `"${length({a = 1})}"` contains braces
// that must not be counted as block structure, and a scanner that stopped at the first
// `"` inside `"${var.x["k"]}"` would end the string in the wrong place.
func (p *hclParser) stringLiteral() string {
	start := p.line
	var sb strings.Builder
	sb.WriteByte(p.next()) // opening quote
	interp := 0
	for !p.eof() {
		c := p.next()
		sb.WriteByte(c)
		switch c {
		case '\\':
			if !p.eof() {
				sb.WriteByte(p.next())
			}
		case '$', '%':
			// Both `${...}` and `%{...}` open a template expression.
			if p.peek() == '{' {
				sb.WriteByte(p.next())
				interp++
			}
		case '}':
			if interp > 0 {
				interp--
			}
		case '"':
			if interp == 0 {
				return sb.String()
			}
		case '\n':
			// A bare newline inside a quoted string is not legal HCL, and continuing
			// would swallow the rest of the file into one token.
			p.diag.malformed(start, "unterminated string")
			return sb.String()
		}
	}
	p.diag.malformed(start, "unterminated string at end of file")
	return sb.String()
}

// heredoc consumes a `<<EOT` or `<<-EOT` block and returns a placeholder.
//
// The body is dropped rather than returned, which is the point: a heredoc in a Terraform
// file is a user-data script, a policy document, or a rendered config, and it is the one
// place in the format where multi-line secret material shows up verbatim. Nothing here
// needs its contents, so nothing here reads them.
func (p *hclParser) heredoc() string {
	start := p.line
	p.next() // <
	p.next() // <
	if p.peek() == '-' {
		p.next()
	}
	var tag strings.Builder
	for !p.eof() && isIdentByte(p.peek()) {
		tag.WriteByte(p.next())
	}
	marker := tag.String()
	if marker == "" {
		p.diag.note(start, "heredoc with no delimiter")
		return `""`
	}
	// Skip the remainder of the opening line, then each line until the delimiter.
	for !p.eof() && p.peek() != '\n' {
		p.next()
	}
	for !p.eof() {
		p.next() // the newline
		lineStart := p.i
		for !p.eof() && p.peek() != '\n' {
			p.next()
		}
		if strings.TrimSpace(p.src[lineStart:p.i]) == marker {
			return `""`
		}
		if p.eof() {
			break
		}
	}
	p.diag.malformed(start, "unterminated heredoc "+marker)
	return `""`
}

// skipBalanced consumes an already-opened block by counting braces, used when nesting
// passes maxHCLDepth. Strings, comments, and heredocs are still honoured, because the
// whole reason to count braces here is that the ones inside them do not count.
func (p *hclParser) skipBalanced() {
	depth := 1
	for !p.eof() && depth > 0 {
		switch c := p.peek(); {
		case c == '"':
			p.stringLiteral()
		case c == '<' && strings.HasPrefix(p.src[p.i:], "<<"):
			p.heredoc()
		case p.atComment():
			p.skipComment()
		case c == '{':
			depth++
			p.next()
		case c == '}':
			depth--
			p.next()
		default:
			p.next()
		}
	}
}

// skipBlank consumes whitespace and newlines.
//
// Not comments, deliberately: a comment in statement position is consumed by header, which
// has to handle one anyway for the trailing `# note` after an attribute. Skipping comments
// here as well would be a second path doing the same job, and a mutation deleting either
// one would leave the other covering for it — a branch no test can distinguish from its
// absence is a branch that will be wrong without anybody finding out.
func (p *hclParser) skipBlank() {
	for !p.eof() {
		switch p.peek() {
		case ' ', '\t', '\r', '\n':
			p.next()
		default:
			return
		}
	}
}

func (p *hclParser) atComment() bool {
	if p.peek() == '#' {
		return true
	}
	return strings.HasPrefix(p.src[p.i:], "//") || strings.HasPrefix(p.src[p.i:], "/*")
}

// skipComment consumes one comment. A line comment leaves its newline in place, so a
// caller that treats a newline as a statement terminator still sees one.
func (p *hclParser) skipComment() {
	if strings.HasPrefix(p.src[p.i:], "/*") {
		start := p.line
		p.next()
		p.next()
		for !p.eof() {
			if p.peek() == '*' && strings.HasPrefix(p.src[p.i:], "*/") {
				p.next()
				p.next()
				return
			}
			p.next()
		}
		p.diag.malformed(start, "unterminated block comment")
		return
	}
	for !p.eof() && p.peek() != '\n' {
		p.next()
	}
}

// splitHCLHeader splits `resource "aws_s3_bucket" "logs"` into its type and labels.
//
// Unquoted labels are accepted even though Terraform requires quotes, because a file that
// writes `resource aws_s3_bucket logs` is a file whose resources are still worth knowing
// about; rejecting it would trade a real fact for a point of pedantry.
func splitHCLHeader(head string) (kind string, labels []string) {
	for _, tok := range hclHeaderTokens(head) {
		switch {
		case kind == "" && !strings.HasPrefix(tok, `"`):
			kind = tok
		default:
			if lbl := hclString(tok); lbl != "" {
				labels = append(labels, lbl)
			} else if !strings.HasPrefix(tok, `"`) {
				labels = append(labels, tok)
			}
		}
	}
	return kind, labels
}

// hclHeaderTokens splits a block header on whitespace, keeping quoted strings whole.
func hclHeaderTokens(head string) []string {
	var out []string
	var cur strings.Builder
	inString := false
	for i := 0; i < len(head); i++ {
		c := head[i]
		switch {
		case c == '"':
			inString = !inString
			cur.WriteByte(c)
		case !inString && (c == ' ' || c == '\t' || c == '\r' || c == '\n'):
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// hclString unquotes a literal string, and returns "" for anything that is not one.
//
// An interpolated string is returned with its `${...}` intact rather than dropped. The
// interpolation is a fact about the value — `"${var.env}-api"` says the name is
// environment-suffixed — and a reader seeing it can tell it is a template, where a reader
// handed `-api` could not.
func hclString(value string) string {
	v := strings.TrimSpace(value)
	if len(v) < 2 || v[0] != '"' || v[len(v)-1] != '"' {
		return ""
	}
	inner := v[1 : len(v)-1]
	// A closing quote that is escaped means this is not one string but the start of an
	// expression — `"a\" + b + \"c"` — and there is no single literal to return.
	if strings.HasSuffix(inner, `\`) && !strings.HasSuffix(inner, `\\`) {
		return ""
	}
	var sb strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] != '\\' || i+1 >= len(inner) {
			sb.WriteByte(inner[i])
			continue
		}
		i++
		switch inner[i] {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		default:
			sb.WriteByte(inner[i])
		}
	}
	return sb.String()
}

// hclObjectField reads one field out of an object-literal expression, or "" when the
// expression is not an object or does not state that field literally.
//
// Parsing the object by running the body parser over its contents rather than by
// splitting on commas: an object value can itself contain objects, lists, and strings
// holding commas, and every one of those breaks a split.
func hclObjectField(value, field string) string {
	v := strings.TrimSpace(value)
	if len(v) < 2 || v[0] != '{' || v[len(v)-1] != '}' {
		return ""
	}
	inner := &hclParser{src: v[1 : len(v)-1], line: 1}
	attrs, _ := inner.body(0)
	for _, a := range attrs {
		if a.key == field {
			return hclString(a.value)
		}
	}
	return ""
}

// isHCLIdent reports whether a string is a bare HCL name.
func isHCLIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isIdentByte(s[i]) {
			return false
		}
	}
	return true
}

func isIdentByte(c byte) bool {
	return c == '_' || c == '-' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// firstWord names the construct a diagnostic is about without quoting a whole line, since
// a line of HCL can be long and can contain a value.
func firstWord(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
