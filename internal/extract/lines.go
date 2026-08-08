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
	// verbatimAt enables C#'s @"..." verbatim string, where a backslash is an
	// ordinary character, "" is an escaped quote, and the literal may span lines.
	verbatimAt bool
	// heredocPrefix opens a heredoc: "<<" in Ruby, "<<<" in PHP. A heredoc's body
	// is arbitrary text terminated by an identifier on a line of its own, which no
	// delimiter-matching rule can find — so it is a separate mechanism rather than
	// a kind of string.
	heredocPrefix string
	// lineBlockStart and lineBlockEnd are whole-line block comment delimiters:
	// Ruby's =begin and =end, which are only comments when they begin a line.
	lineBlockStart string
	lineBlockEnd   string
	// scriptTags marks a language embedded in text, where code exists only between
	// its open and close tags: PHP's <?php ... ?>.
	scriptTags bool
	// hashBracketAttr exempts `#[` from the `#` line comment, because PHP 8 spells an
	// attribute that way: `#[Route('/x')] public function show()` is a declaration,
	// and reading the hash as a comment would delete it.
	hashBracketAttr bool
	// hereStringAt enables PowerShell's here-string: `@"` or `@'` as the last thing on a
	// line, with `"@` or `'@` on a line of its own closing it. Neither a heredoc nor any
	// delimiter form — there is no identifier to name the terminator and the terminator is
	// punctuation — so it gets its own rule and reuses the heredoc stack to hold it.
	hereStringAt bool
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
	// strVerbatim marks an open C# verbatim string, where the terminator is escaped
	// by doubling it rather than by a backslash: `""` is a quote, not the end.
	strVerbatim bool
	// heredocs are the terminators of the heredocs opened and not yet closed, in
	// the order they must close. A stack rather than one string because a single
	// line may open several — `query(<<~SQL, <<~PARAMS)` is legal Ruby — and closing
	// them out of order would leave a body readable as code.
	heredocs []heredoc
	// outsideScript is true when the scanner is in the text a script language is
	// embedded in rather than in its code: a PHP file before its first <?php.
	outsideScript bool
}

// heredoc is one open heredoc body.
type heredoc struct {
	// term is the identifier that closes it.
	term string
	// indented allows the terminator to be preceded by whitespace, which `<<~` and
	// `<<-` permit and a bare `<<ID` does not.
	indented bool
	// atTerm marks a PowerShell here-string, whose terminator closes on a different
	// rule from every heredoc's: it must begin the line, and what follows it on that
	// line is code rather than a syntax error. `@"..."@ -ContentType 'application/json'`
	// is how a here-string is passed as a parameter value, which is most of why one is
	// written at all, so requiring the terminator to be the whole line would leave the
	// string open and blank the rest of the file.
	atTerm bool
}

// closes returns the index just past the terminator of the open string in line,
// or -1 when the string continues onto the next line.
func (st *scanState) closes(line string) int {
	if st.strVerbatim {
		// A doubled quote inside a verbatim string is an escaped quote, so the first
		// quote *not* followed by another is the terminator.
		for i := 0; i < len(line); i++ {
			if line[i] != '"' {
				continue
			}
			if i+1 < len(line) && line[i+1] == '"' {
				i++
				continue
			}
			return i + 1
		}
		return -1
	}
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
	// C, C++ and Objective-C share one config. Block comments do not nest — the C
	// standard says the first `*/` closes, so `/* /* */` is a closed comment followed
	// by code, and treating it as Rust's nesting form would swallow the rest of the
	// file. The single quote stays an ordinary delimiter for the reason scanJava gives:
	// the scanner blanks a delimited body, so `if (c == '{')` contributes no brace to
	// the depth count the extractor walks with.
	//
	// C++11 raw strings — R"delim(...)delim" — are not modelled. rawStringHash is
	// Rust's `r#"..."#` form and does not fit: C++ takes an arbitrary delimiter, not a
	// run of hashes. An unterminated ordinary string is what the scanner sees instead,
	// which ends at the line's end (multiLineQuote is off), so a raw string spanning
	// lines leaves its middle lines readable as code. Declarations do not appear inside
	// string literals, so what that can produce is a spurious symbol rather than a lost
	// one — and cDeclSite's depth rule rejects text at any depth a declaration cannot
	// occupy.
	scanC = scanConfig{
		lineComment: []string{"//"}, blockStart: "/*", blockEnd: "*/",
	}
	scanRust = scanConfig{
		lineComment: []string{"//"}, blockStart: "/*", blockEnd: "*/",
		blockNests: true, rawStringHash: true, lifetimes: true, noSingleQuote: true,
		multiLineQuote: true,
	}
	// Ruby. Its block comment is the whole-line =begin/=end pair, which is why it uses
	// lineBlockStart rather than blockStart: `=begin` is only a comment at column 0,
	// and `x =begin_at(3)` is code.
	//
	// Two things are deliberately not modelled. `%w[a b]` and `%i[a b]` are word-array
	// literals whose contents are bare words: they survive as code, which can only
	// contribute a spurious token to a line the extractor's declaration rules then
	// reject, never blank a real one. And `/regex/` is left alone for the same reason
	// as Rust's lifetimes — a slash is division far more often than it opens a regex,
	// and guessing wrong blanks the rest of a line.
	scanRuby = scanConfig{
		lineComment: []string{"#"}, lineBlockStart: "=begin", lineBlockEnd: "=end",
		heredocPrefix: "<<",
	}
	// PHP. Code exists only inside <?php ... ?>, so scriptTags starts the scanner in
	// text and everything outside the tags is not read — a template's HTML is not
	// source, and a `class="btn"` attribute in it would otherwise read as a class.
	//
	// Three comment forms, because PHP inherited both C's and the shell's. The
	// heredoc prefix is <<< for both heredoc and nowdoc.
	scanPHP = scanConfig{
		lineComment: []string{"//", "#"}, blockStart: "/*", blockEnd: "*/",
		heredocPrefix: "<<<", scriptTags: true, hashBracketAttr: true,
	}
	// C#. Block comments do not nest, as in C. A raw string literal is `"""` and spans
	// lines, and the verbatim `@"..."` form needs its own rule because it escapes a
	// quote by doubling it rather than with a backslash.
	//
	// The single quote stays an ordinary delimiter for the reason scanJava gives: the
	// scanner blanks a delimited body, so `if (c == '{')` contributes no brace to the
	// depth count the extractor walks.
	scanCSharp = scanConfig{
		lineComment: []string{"//"}, blockStart: "/*", blockEnd: "*/",
		tripleQuotes: []string{`"""`}, verbatimAt: true,
	}
	// POSIX shell and bash. `#` is the only comment form there is — the shell has no
	// block comment, and the `: <<'EOF'` idiom sometimes used as one is a heredoc, which
	// the heredoc mechanism already blanks.
	//
	// The heredoc prefix is `<<`, as Ruby's is, and it is the one shared mechanism between
	// the two languages. Its uppercase rule earns more here than anywhere else: `<<` is
	// also the shell's append redirection, `echo x >>file`, and a shell script is where
	// heredocs are most common.
	//
	// Two things are deliberately not modelled, both for the reason the Ruby config gives.
	// `$(...)` and backticks hold a nested command, and their contents survive as code —
	// which can contribute a spurious token to a line the declaration rules then reject,
	// never blank a real one. And a `[[ $x == *.sh ]]` glob is left alone: a `*` is not a
	// delimiter, so nothing needs to know.
	scanShell = scanConfig{
		lineComment: []string{"#"}, heredocPrefix: "<<",
	}
	// PowerShell. Its block comment is `<# ... #>` rather than C's `/* ... */`, and its
	// here-string is `@"..."@` on lines of its own, which is neither a heredoc nor any
	// delimiter form: hereStringAt handles it, because there is no identifier naming the
	// terminator and the terminator is punctuation rather than a word.
	//
	// A single quote is a literal string in PowerShell and a double quote is an
	// interpolating one, and both are ordinary delimiters here — the scanner blanks a
	// delimited body either way, and what an interpolation `$($x)` holds is not a
	// declaration.
	scanPowerShell = scanConfig{
		lineComment: []string{"#"}, blockStart: "<#", blockEnd: "#>",
		hereStringAt: true,
	}
)

// scanLines splits source into code lines with comments and string bodies removed.
func scanLines(src string, cfg scanConfig) []codeLine {
	raw := strings.Split(src, "\n")
	out := make([]codeLine, 0, len(raw))

	var st scanState
	// A script language's file begins in the text it is embedded in, not in code.
	// Starting inside would read a template's HTML as source.
	st.outsideScript = cfg.scriptTags

	for i, line := range raw {
		cl := codeLine{Raw: line, Num: i + 1, Indent: indentWidth(line)}
		// A heredoc's body is checked before everything else, including an open
		// string: the heredoc opened later, so it closes first.
		if len(st.heredocs) > 0 {
			if end, ok := heredocCloses(line, st.heredocs[0]); ok {
				st.heredocs = st.heredocs[1:]
				// The remainder of a PowerShell here-string's terminator line is code. For
				// every other heredoc end is the line's length, so this rescans nothing.
				if end < len(line) && len(st.heredocs) == 0 {
					cl.Text = strings.Repeat(" ", end) + scanOne(line[end:], cfg, &st)
					out = append(out, cl)
					continue
				}
			}
			cl.InBlockString = true
			out = append(out, cl)
			continue
		}
		if st.outsideScript {
			// Text outside the script tags. Only the opening tag matters; everything
			// before it is markup, and a declaration cannot appear in it.
			if j := strings.Index(line, "<?"); j >= 0 {
				st.outsideScript = false
				open := j + 2
				if strings.HasPrefix(line[open:], "php") {
					open += 3
				}
				cl.Text = strings.Repeat(" ", open) + scanOne(line[open:], cfg, &st)
				out = append(out, cl)
				continue
			}
			out = append(out, cl)
			continue
		}
		if cfg.lineBlockStart != "" && st.blockDepth == 0 &&
			strings.HasPrefix(line, cfg.lineBlockStart) {
			// Ruby's =begin. Only a comment at column 0, which is why it is matched
			// against the unindented line and not tested inside scanOne.
			st.blockDepth = 1
			out = append(out, cl)
			continue
		}
		if cfg.lineBlockEnd != "" && st.blockDepth > 0 {
			if strings.HasPrefix(line, cfg.lineBlockEnd) {
				st.blockDepth = 0
			}
			out = append(out, cl)
			continue
		}
		if st.strDelim != "" {
			// Inside a multi-line string: look for its terminator.
			if end := st.closes(line); end >= 0 {
				st.strDelim, st.strRaw, st.strVerbatim = "", false, false
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
			attr := cfg.hashBracketAttr && c == "#" && strings.HasPrefix(line[i:], "#[")
			if !attr {
				break
			}
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
		// The close tag of an embedded script language ends the code region.
		if cfg.scriptTags && strings.HasPrefix(line[i:], "?>") {
			st.outsideScript = true
			break
		}
		// A heredoc opener. Checked before the string and operator cases because
		// `<<~SQL` starts with two characters that are otherwise a shift operator, and
		// the identifier after them is not a string by any delimiter rule.
		if cfg.heredocPrefix != "" && strings.HasPrefix(line[i:], cfg.heredocPrefix) {
			if hd, consumed, ok := scanHeredocOpen(line[i:], cfg.heredocPrefix); ok {
				st.heredocs = append(st.heredocs, hd)
				b.WriteString(strings.Repeat(" ", consumed))
				i += consumed
				continue
			}
		}
		// PowerShell's here-string. `@"` or `@'` opens one only when it is the last thing
		// on the line — `@"x"` mid-line is an ordinary string with a splat before it — and
		// it is closed by `"@` at the start of a line. Both quote forms exist and differ
		// only in interpolation, which nothing here reads, so the terminator is recorded
		// rather than assumed.
		if cfg.hereStringAt && line[i] == '@' && i+1 < len(line) &&
			(line[i+1] == '"' || line[i+1] == '\'') {
			if strings.TrimSpace(line[i+2:]) == "" {
				st.heredocs = append(st.heredocs,
					heredoc{term: string(line[i+1]) + "@", atTerm: true})
				b.WriteString(strings.Repeat(" ", len(line)-i))
				i = len(line)
				continue
			}
		}
		// C#'s verbatim string: @"..." spans lines and treats a backslash literally,
		// so the ordinary scanner would misjudge both its end and its escapes.
		if cfg.verbatimAt && line[i] == '@' && i+1 < len(line) && line[i+1] == '"' {
			consumed, open := scanVerbatim(line[i:])
			b.WriteString(strings.Repeat(" ", consumed))
			i += consumed
			if open {
				st.strDelim, st.strVerbatim = `"`, true
				break
			}
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

// scanHeredocOpen measures a heredoc opener at the start of s and reports what
// closes it.
//
// The forms, across both languages that have one:
//
//	<<SQL      <<-SQL     <<~SQL          Ruby
//	<<<SQL     <<<"SQL"   <<<'SQL'        PHP (the quoted form is nowdoc)
//
// Two things make this a measurement rather than a match. The identifier is
// arbitrary, so nothing but the opener says what terminates the body; and `a << b`
// is a legal shift, so an opener is only an opener when an identifier follows
// immediately. Reading a shift as a heredoc would blank the rest of the file.
func scanHeredocOpen(s, prefix string) (heredoc, int, bool) {
	i := len(prefix)
	hd := heredoc{}
	// Ruby's squiggly and dash forms allow the terminator to be indented. PHP's
	// always does, which the caller expresses by using the <<< prefix.
	if prefix == "<<<" {
		hd.indented = true
	}
	if i < len(s) && (s[i] == '~' || s[i] == '-') {
		hd.indented = true
		i++
	}
	// A quoted identifier: Ruby's <<~'SQL' and PHP's nowdoc <<<'SQL'.
	quote := byte(0)
	if i < len(s) && (s[i] == '\'' || s[i] == '"') {
		quote = s[i]
		i++
	}
	start := i
	for i < len(s) && identChar(s[i]) {
		i++
	}
	if i == start {
		return heredoc{}, 0, false
	}
	// A heredoc identifier is conventionally uppercase, and requiring it is what
	// separates the opener from a shift by a variable: `count << shift` has a
	// lowercase identifier after the operator and is not a heredoc. Ruby permits a
	// lowercase one, so this trades a rare real heredoc for never destroying a line
	// that holds a shift — the safe direction, since a missed heredoc leaves its body
	// readable as code where a missed shift blanks real declarations.
	name := s[start:i]
	if name != strings.ToUpper(name) {
		return heredoc{}, 0, false
	}
	if quote != 0 {
		if i >= len(s) || s[i] != quote {
			return heredoc{}, 0, false
		}
		i++
	}
	hd.term = name
	return hd, i, true
}

// heredocCloses reports whether line closes an open heredoc, and the index just past the
// terminator when it does.
//
// The index is only meaningful for a PowerShell here-string, and it is why this returns one
// at all. A heredoc terminator is the whole line by definition, so there is never anything
// after it to read; a `"@` closes as soon as it starts the line, and what follows is code:
//
//	Invoke-RestMethod -Uri $u -Body @"
//	{"a":1}
//	"@ -ContentType 'application/json'
//
// That trailing `-ContentType` is not decoration. Passing a here-string as a parameter value
// is most of the reason one is written, so treating the line as body would leave the string
// open and blank every declaration below it — the same silent whole-file loss the `>>`
// boundary in shell guards against.
func heredocCloses(line string, hd heredoc) (int, bool) {
	if hd.atTerm {
		t := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(t, hd.term) {
			return 0, false
		}
		return len(line) - len(t) + len(hd.term), true
	}
	t := line
	if hd.indented {
		t = strings.TrimLeft(t, " \t")
	}
	// Ruby allows a trailing comma or paren after the terminator when the heredoc was
	// an argument; PHP allows a semicolon.
	t = strings.TrimRight(t, " \t\r,);")
	if t != hd.term {
		return 0, false
	}
	return len(line), true
}

// scanVerbatim measures a C# verbatim string starting at s[0]=='@'.
//
// Returns the bytes consumed and whether the string is still open at the line's
// end. A doubled quote is an escaped quote rather than a terminator, which is the
// one rule that makes this different from an ordinary string: `@"C:\a"" b"` is one
// literal, and reading the second quote as its end would leave ` b"` as code.
func scanVerbatim(s string) (consumed int, open bool) {
	i := 2 // past @"
	for i < len(s) {
		if s[i] != '"' {
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '"' {
			i += 2
			continue
		}
		return i + 1, false
	}
	return len(s), true
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
