package extract

import "strings"

// Shared line scanning for the hand-written extractors.
//
// The hand-written extractors are line-oriented approximations (design §4.1), and
// their dominant failure mode is not missing a declaration — it is *inventing*
// one by matching text inside a comment, a string, or a docstring. Precision
// matters more than recall here: a missed import is a gap, while a spurious one
// points an agent at a module that does not exist.
//
// So all three share one scanner that strips comments and blanks string bodies
// while preserving the byte offsets of everything else. Blanking rather than
// deleting keeps column positions meaningful and, more importantly, means a
// pattern cannot accidentally match across the hole a deletion would leave.

// codeLine is one line with its non-code content removed.
type codeLine struct {
	// Text is the line with comments stripped and string bodies blanked.
	Text string
	// Raw is the original line, for extracting a string literal's real value
	// once the scanner has confirmed the line is code.
	Raw string
	// Num is the 1-based line number.
	Num int
	// Indent is the count of leading spaces, with tabs expanded to the next
	// multiple of 8. Python needs this to tell a top-level declaration from a
	// nested one, which is the difference between a module's public surface and
	// a closure nobody can call.
	Indent int
	// InBlockString is true when this line is inside a multi-line string that
	// started on an earlier line. Its Text is fully blanked, but callers
	// sometimes need to know why.
	InBlockString bool
}

// scanConfig describes a language's comment and string syntax.
type scanConfig struct {
	lineComment []string // e.g. "//", "#"
	blockStart  string   // e.g. "/*"
	blockEnd    string   // e.g. "*/"
	blockNests  bool     // Rust's and Kotlin's /* */ nest; C-family ones do not
	// tripleQuotes are the triple-delimiters that open a string which may span
	// lines. A list rather than a flag because the languages that have them do not
	// agree on which: Python has """ and ''', while a Java text block and a Kotlin
	// raw string are """ only. Reading ''' in a JVM file would be a scanner that
	// blanks code on a line holding three adjacent char literals.
	tripleQuotes []string
	// backtick marks a template-literal delimiter that can span lines (JS/TS).
	backtick bool
	// rawStringHash enables Rust's r"..." and r#"..."# raw strings.
	rawStringHash bool
	// lifetimes makes a single quote ambiguous, as it is in Rust: 'a is a
	// lifetime, 'a' is a char literal.
	lifetimes bool
	// noSingleQuote disables ' as a string delimiter (Rust, where it is only
	// ever a char literal or a lifetime).
	noSingleQuote bool
	// multiLineQuote allows an ordinary double-quoted string to span lines, as
	// Rust's does. Python's and JavaScript's do not: there, an unterminated quote
	// is a syntax error, and the safe reading is that the line ends with it.
	multiLineQuote bool
}

// scanState is the cross-line scanner state: an open block comment, or an open
// multi-line string and whether it is a raw one.
type scanState struct {
	blockDepth int
	// strDelim is the terminator of the string currently open, empty if none.
	// For a raw string this is the closing quote plus its hashes.
	strDelim string
	// strRaw marks the open string as raw, where a backslash is an ordinary
	// character and so cannot escape the terminator.
	strRaw bool
}

// closes returns the index just past the terminator of the open string in line,
// or -1 when the string continues onto the next line.
func (st *scanState) closes(line string) int {
	if st.strRaw {
		if i := strings.Index(line, st.strDelim); i >= 0 {
			return i + len(st.strDelim)
		}
		return -1
	}
	if i := findUnescaped(line, st.strDelim); i >= 0 {
		return i + len(st.strDelim)
	}
	return -1
}

var (
	scanJSLike = scanConfig{
		lineComment: []string{"//"}, blockStart: "/*", blockEnd: "*/", backtick: true,
	}
	scanPython = scanConfig{
		lineComment: []string{"#"}, tripleQuotes: []string{`"""`, `'''`},
	}
	// Java: a text block is `"""` and spans lines. A single quote is only ever a
	// char literal, and it is left as an ordinary delimiter deliberately — the
	// scanner blanks a delimited body, so `if (c == '{')` contributes no brace to
	// the depth count the extractor walks with. Reading it as *not* a delimiter
	// would leave that brace in place and open a block that never closes.
	scanJava = scanConfig{
		lineComment: []string{"//"}, blockStart: "/*", blockEnd: "*/",
		tripleQuotes: []string{`"""`},
	}
	// Kotlin differs from Java in two ways the scanner has to know about: its block
	// comments nest, as Rust's do, and a string template `${...}` can hold code. The
	// template is not modelled — its body is blanked with the rest of the string,
	// which loses nothing this extractor reads, since a declaration cannot appear
	// inside one.
	scanKotlin = scanConfig{
		lineComment: []string{"//"}, blockStart: "/*", blockEnd: "*/",
		blockNests: true, tripleQuotes: []string{`"""`},
	}
	scanRust = scanConfig{
		lineComment: []string{"//"}, blockStart: "/*", blockEnd: "*/",
		blockNests: true, rawStringHash: true, lifetimes: true, noSingleQuote: true,
		multiLineQuote: true,
	}
)

// scanLines splits source into code lines with comments and string bodies removed.
func scanLines(src string, cfg scanConfig) []codeLine {
	raw := strings.Split(src, "\n")
	out := make([]codeLine, 0, len(raw))

	var st scanState

	for i, line := range raw {
		cl := codeLine{Raw: line, Num: i + 1, Indent: indentWidth(line)}
		if st.strDelim != "" {
			// Inside a multi-line string: look for its terminator.
			if end := st.closes(line); end >= 0 {
				st.strDelim, st.strRaw = "", false
				// Everything up to and including the terminator is string body;
				// the remainder of the line is code again.
				cl.Text = strings.Repeat(" ", end) + scanOne(line[end:], cfg, &st)
			} else {
				cl.Text = ""
				cl.InBlockString = true
			}
			out = append(out, cl)
			continue
		}
		if st.blockDepth > 0 {
			// Inside a block comment.
			cl.Text = consumeBlockComment(line, cfg, &st)
			out = append(out, cl)
			continue
		}
		cl.Text = scanOne(line, cfg, &st)
		out = append(out, cl)
	}
	return out
}

// consumeBlockComment advances through a line already inside a block comment,
// returning any code that follows the comment's end.
func consumeBlockComment(line string, cfg scanConfig, st *scanState) string {
	i := 0
	for i < len(line) && st.blockDepth > 0 {
		if cfg.blockNests && strings.HasPrefix(line[i:], cfg.blockStart) {
			st.blockDepth++
			i += len(cfg.blockStart)
			continue
		}
		if strings.HasPrefix(line[i:], cfg.blockEnd) {
			st.blockDepth--
			i += len(cfg.blockEnd)
			continue
		}
		i++
	}
	if st.blockDepth > 0 {
		return ""
	}
	return strings.Repeat(" ", i) + scanOne(line[i:], cfg, st)
}

// scanOne strips comments and blanks string bodies in a single line of code,
// updating cross-line state for an unterminated block comment or string.
func scanOne(line string, cfg scanConfig, st *scanState) string {
	var b strings.Builder
	b.Grow(len(line))

	i := 0
	for i < len(line) {
		// Line comment: the rest of the line is gone.
		if c := matchAny(line[i:], cfg.lineComment); c != "" {
			break
		}
		// Block comment.
		if cfg.blockStart != "" && strings.HasPrefix(line[i:], cfg.blockStart) {
			st.blockDepth = 1
			i += len(cfg.blockStart)
			// Scan forward for the end on this same line.
			for i < len(line) && st.blockDepth > 0 {
				if cfg.blockNests && strings.HasPrefix(line[i:], cfg.blockStart) {
					st.blockDepth++
					i += len(cfg.blockStart)
					continue
				}
				if strings.HasPrefix(line[i:], cfg.blockEnd) {
					st.blockDepth--
					i += len(cfg.blockEnd)
					continue
				}
				i++
			}
			if st.blockDepth > 0 {
				break // comment continues onto the next line
			}
			b.WriteByte(' ')
			continue
		}
		// Triple-quoted strings, checked before single quotes so that """ is not
		// read as an empty string followed by a quote.
		if len(cfg.tripleQuotes) > 0 {
			if d := matchAny(line[i:], cfg.tripleQuotes); d != "" {
				if end := findUnescaped(line[i+len(d):], d); end >= 0 {
					// Opens and closes on this line.
					blank := len(d)*2 + end
					b.WriteString(strings.Repeat(" ", blank))
					i += blank
					continue
				}
				st.strDelim = d
				break
			}
		}
		// Rust raw strings: r"..." and r#"..."#, where the hash count sets the
		// terminator. Handled explicitly because escapes do not apply inside
		// them, so the ordinary string scanner would misjudge the end. The
		// preceding-character check keeps an identifier that merely ends in r
		// (`for`, `iter`) from being read as a raw-string prefix.
		if cfg.rawStringHash && (line[i] == 'r' || line[i] == 'b') && !identChar(prevByte(line, i)) {
			if consumed, term, ok := scanRustRawString(line[i:]); ok {
				b.WriteString(strings.Repeat(" ", consumed))
				i += consumed
				if term != "" {
					// Unterminated on this line: the string continues, and inside a
					// raw string a backslash escapes nothing, so the terminator is
					// matched literally.
					st.strDelim, st.strRaw = term, true
					break
				}
				continue
			}
		}
		// A Rust lifetime looks like a string opener but is not: 'a in
		// `&'a str` or `struct S<'a>` has no closing quote, so treating it as one
		// would blank the rest of the line and hide the declaration entirely.
		if cfg.lifetimes && line[i] == '\'' {
			consumed, isChar := scanRustQuote(line[i:])
			if isChar {
				b.WriteString(strings.Repeat(" ", consumed))
			} else {
				// A lifetime is part of the code; keep it verbatim.
				b.WriteString(line[i : i+consumed])
			}
			i += consumed
			continue
		}
		// Backtick template literal, which can span lines.
		if cfg.backtick && line[i] == '`' {
			if end := findUnescaped(line[i+1:], "`"); end >= 0 {
				// The delimiters are preserved, as they are for ordinary strings,
				// so a caller can see that a literal was here and recover its
				// value from Raw at the same offset.
				b.WriteByte('`')
				b.WriteString(strings.Repeat(" ", end))
				b.WriteByte('`')
				i += end + 2
				continue
			}
			st.strDelim = "`"
			break
		}
		// Ordinary quoted strings.
		if line[i] == '"' || (line[i] == '\'' && !cfg.noSingleQuote) {
			q := string(line[i])
			if end := findUnescaped(line[i+1:], q); end >= 0 {
				// Preserve the quotes so callers can still see that a string was
				// here, but blank the body: a quoted module path is extracted by
				// the caller from Raw, not from Text.
				b.WriteByte(line[i])
				b.WriteString(strings.Repeat(" ", end))
				b.WriteByte(line[i])
				i += end + 2
				continue
			}
			// Unterminated on this line. In Rust an ordinary "..." may legally span
			// lines, and a `const S: &str = "` holding sample code is exactly where
			// a phantom declaration comes from, so the state carries over. Elsewhere
			// this is a syntax error, and the safe reading is "the rest is not code".
			if cfg.multiLineQuote && q == `"` {
				st.strDelim = q
			}
			break
		}
		b.WriteByte(line[i])
		i++
	}

	// Pad to the original length so byte offsets in Text still line up with Raw.
	out := b.String()
	if len(out) < len(line) {
		out += strings.Repeat(" ", len(line)-len(out))
	}
	return out
}

// scanRustRawString measures a Rust raw string starting at s[0]=='r' or 'b'.
//
// Returns the byte length consumed, the terminator still outstanding when the
// string runs past the end of the line (empty when it closed here), and whether a
// raw string was found at all.
func scanRustRawString(s string) (consumed int, open string, ok bool) {
	i := 1
	hashes := 0
	for i < len(s) && s[i] == '#' {
		hashes++
		i++
	}
	if i >= len(s) || s[i] != '"' {
		return 0, "", false
	}
	i++
	term := `"` + strings.Repeat("#", hashes)
	if idx := strings.Index(s[i:], term); idx >= 0 {
		return i + idx + len(term), "", true
	}
	// Unterminated on this line: consume the remainder and report what closes it.
	return len(s), term, true
}

// findUnescaped returns the index of the first occurrence of delim not preceded
// by an odd number of backslashes, or -1.
func findUnescaped(s, delim string) int {
	for i := 0; i+len(delim) <= len(s); i++ {
		if !strings.HasPrefix(s[i:], delim) {
			continue
		}
		bs := 0
		for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
			bs++
		}
		if bs%2 == 0 {
			return i
		}
	}
	return -1
}

// matchAny returns the first of the candidates that prefixes s, or "".
func matchAny(s string, candidates []string) string {
	for _, c := range candidates {
		if c != "" && strings.HasPrefix(s, c) {
			return c
		}
	}
	return ""
}

// indentWidth measures leading whitespace, expanding tabs to multiples of 8 —
// the same rule Python's tokenizer uses.
func indentWidth(line string) int {
	w := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			w++
		case '\t':
			w += 8 - (w % 8)
		default:
			return w
		}
	}
	return w
}

// stringAt extracts the quoted string literal beginning at or after byte offset
// idx in raw, returning its unquoted value. Used by the extractors to recover a
// module path from the original text once the scanner has confirmed the
// surrounding line is code.
func stringAt(raw string, idx int) (string, bool) {
	for i := idx; i < len(raw); i++ {
		c := raw[i]
		if c != '"' && c != '\'' && c != '`' {
			continue
		}
		q := string(c)
		if end := findUnescaped(raw[i+1:], q); end >= 0 {
			return unescapeBasic(raw[i+1 : i+1+end]), true
		}
		return "", false
	}
	return "", false
}

// unescapeBasic resolves the escapes that can appear in a module path. Full
// escape handling is unnecessary — a path with a newline in it is not a path.
func unescapeBasic(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// scanRustQuote measures what follows a single quote in Rust and reports whether
// it is a char literal rather than a lifetime.
//
// The two are only distinguishable by looking ahead: 'a' and '\n' are chars,
// while 'a and 'static are lifetimes. Getting this wrong on a lifetime is the
// expensive direction — the scanner would look for a closing quote, not find one,
// and blank the remainder of a line that may hold the only declaration in it.
func scanRustQuote(s string) (consumed int, isChar bool) {
	if len(s) < 2 {
		return 1, false
	}
	if s[1] == '\\' {
		// An escape is always a char literal; find its close.
		if end := findUnescaped(s[1:], "'"); end >= 0 {
			return end + 2, true
		}
		return len(s), true
	}
	// A single character followed by a quote is a char literal. Measured in runes
	// so a multi-byte char literal such as 'é' is not split.
	r := []rune(s[1:])
	if len(r) >= 2 && r[1] == '\'' {
		return 1 + len(string(r[0])) + 1, true
	}
	// Otherwise a lifetime: consume the quote and the identifier after it.
	i := 1
	for i < len(s) && identChar(s[i]) {
		i++
	}
	return i, false
}

// identChar reports whether b can appear in an identifier.
func identChar(b byte) bool {
	return b == '_' || b >= 0x80 ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// prevByte returns the byte before i, or a space at the start of the line.
func prevByte(s string, i int) byte {
	if i == 0 {
		return ' '
	}
	return s[i-1]
}

// joinParens returns lines[i] extended with following lines until its
// parentheses and square brackets balance, plus the index of the last line
// consumed.
//
// This exists for the wrapped import list, which is the common real form:
//
//	from typing import (
//	    Any,
//	    Dict,
//	)
//
// Without it the extractor sees "from typing import (" and finds no names.
//
// Braces are deliberately not counted. An unbalanced `{` opens a function or
// class body, so counting it would fold a whole definition into one line, and
// every nested declaration inside it would then appear to start at the outer
// line's indentation — which is precisely the signal Python uses to tell
// top-level from nested. Brace-delimited import forms (`import { a } from "m"`)
// balance on their own line, so they need no joining. `joinBraces` handles the
// rare wrapped case for JS explicitly.
func joinParens(lines []codeLine, i int) (codeLine, int) {
	return joinWhile(lines, i, "([", ")]")
}

// joinBraces is joinParens including braces, for the JS/TS named-import list
// that a formatter has wrapped across lines. Only call it on a line already
// identified as an import statement, where a `{` cannot be a function body.
func joinBraces(lines []codeLine, i int) (codeLine, int) {
	return joinWhile(lines, i, "([{", ")]}")
}

// joinWhile merges forward while the given bracket pairs are unbalanced or the
// line ends in a backslash continuation.
func joinWhile(lines []codeLine, i int, open, close string) (codeLine, int) {
	cur := lines[i]
	text, raw := cur.Text, cur.Raw
	depth := netDepth(text, open, close)
	cont := endsWithBackslash(text)

	// A cap, because a file with an unclosed bracket would otherwise swallow the
	// rest of itself into one line.
	const maxJoin = 200
	for joined := 0; (depth > 0 || cont) && i+1 < len(lines) && joined < maxJoin; joined++ {
		i++
		next := lines[i]
		text = trimBackslash(text) + " " + strings.TrimSpace(next.Text)
		raw = trimBackslash(raw) + " " + strings.TrimSpace(next.Raw)
		depth += netDepth(next.Text, open, close)
		cont = endsWithBackslash(next.Text)
		if depth < 0 {
			depth = 0
		}
	}
	return codeLine{Text: text, Raw: raw, Num: cur.Num, Indent: cur.Indent}, i
}

func endsWithBackslash(s string) bool {
	return strings.HasSuffix(strings.TrimRight(s, " \t"), "\\")
}

func trimBackslash(s string) string {
	return strings.TrimSuffix(strings.TrimRight(s, " \t"), "\\")
}

// netDepth returns the net balance of the given bracket pairs. Strings are
// already blanked by the scanner, so a bracket inside a string cannot skew this.
func netDepth(s, open, close string) int {
	d := 0
	for i := 0; i < len(s); i++ {
		switch {
		case strings.IndexByte(open, s[i]) >= 0:
			d++
		case strings.IndexByte(close, s[i]) >= 0:
			d--
		}
	}
	return d
}
