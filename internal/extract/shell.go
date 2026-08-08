package extract

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// ShellExtractor reads what a shell script sources and what functions it defines.
//
// Line-oriented, not a parser (design §4.1). The shell differs from every other
// language this package reads in three ways, and each one removes work rather than
// adding it:
//
//   - There is no scope. A function defined inside another function is still global
//     once the outer one has run — the shell has no lexical scoping of function names
//     at all — so there is no declaration-site rule to enforce and no scope stack to
//     keep. Every other extractor here spends most of its length on exactly that, and
//     its absence is a fact about the language rather than an omission. `local` is the
//     one scoping keyword there is, and it applies to variables only.
//   - There is no module system and no registry. `source` names a file, so a script
//     that sources nothing has no dependencies to speak of, and there is no manifest an
//     unresolved source could be matched against. An unresolved `source` is a gap in the
//     map, never an external package (see resolveShell).
//   - There is no visibility syntax. Everything a sourced file defines is reachable by
//     whoever sourced it, so the leading-underscore convention is the only statement of
//     intent available — the same position `_private` puts the Python extractor in.
//
// What this reads is deliberately narrower than what the shell can express. A
// dynamically constructed path (`source "$dir/$name.sh"`) names a file nothing here can
// know, and is recorded as no import at all rather than as a guess: the same rule
// rubyRequire applies to `require File.join(...)`, and for the same reason.
type ShellExtractor struct{}

// Langs implements Extractor.
func (ShellExtractor) Langs() []discover.Lang { return []discover.Lang{discover.LangShell} }

// Extract implements Extractor.
func (ShellExtractor) Extract(f discover.File) (Facts, error) {
	facts := Facts{Path: f.Path, Lang: discover.LangShell}
	lines := scanLines(f.Content, scanShell)

	// A shebang is what makes a script executable, and for a shell script it is the
	// only entrypoint signal there is: a script has no `main`, and whether it is run or
	// sourced is decided by its caller. A library meant to be sourced conventionally has
	// no shebang, so the presence of one is a real distinction rather than a formality.
	if strings.HasPrefix(f.Content, "#!") {
		facts.Entrypoints = append(facts.Entrypoints, "#!")
	}

	for _, cl := range lines {
		code := strings.TrimSpace(cl.Text)
		if code == "" {
			continue
		}
		switch {
		case shIsSource(code):
			if im, ok := shSource(cl); ok {
				facts.Imports = append(facts.Imports, im)
			}

		default:
			if name, ok := shFuncName(code); ok {
				facts.Symbols = append(facts.Symbols, Symbol{
					Name: name, Kind: SymFunc,
					// The underscore convention. A shell function is reachable by anything
					// that sourced the file whatever it is called, so this states intent and
					// not enforcement — which is exactly what the Python extractor's rule
					// does, and the shell's version of it is if anything more consistently
					// followed, since a sourced library has no other way to say "not yours".
					Exported: !strings.HasPrefix(name, "_"),
					Doc:      shDoc(lines, cl.Num-1), Line: cl.Num,
				})
				continue
			}
			if name, ok := shDeclaredVar(code); ok {
				facts.Symbols = append(facts.Symbols, Symbol{
					Name: name, Kind: SymConst, Exported: !strings.HasPrefix(name, "_"),
					Line: cl.Num,
				})
			}
		}
	}
	return facts, nil
}

// shIsSource reports whether a line sources another file.
//
// Both spellings: `source lib.sh` is bash's and `. lib.sh` is POSIX's, and the second is
// a single character, which is what makes it need care. A `.` is only the source builtin
// when it is the whole first token — `./script.sh` runs a script in a subshell rather
// than sourcing it, and `../lib` is a path fragment.
func shIsSource(code string) bool {
	if strings.HasPrefix(code, "source ") || strings.HasPrefix(code, "source\t") {
		return true
	}
	return len(code) > 1 && code[0] == '.' && (code[1] == ' ' || code[1] == '\t')
}

// shSource reads the path out of a source command.
//
// Three shapes, and which one it is decides whether anything is recorded at all:
//
//	source lib/util.sh                     a literal path
//	source "$(dirname "$0")/util.sh"       a path anchored at the script's own directory
//	source "$config_dir/$name.sh"          a path this extractor cannot know
//
// The second shape is the one that matters, because it is what a correct script writes.
// A bare relative `source` resolves against the *invoking* directory rather than the
// script's, so a script that can be run from anywhere has to say `$(dirname "$0")` or
// `${BASH_SOURCE%/*}` — which means the common, careful form is the one carrying an
// expansion. Refusing to read it would leave the most correct scripts in a repository
// looking like they source nothing.
//
// So a leading expansion is stripped and the remainder marked relative with a `./`,
// which is the same marker rubyRequire and phpUse hand to their resolvers. That is a
// reading of intent rather than an evaluation: the expansion is *assumed* to name the
// script's own directory. It nearly always does — `$0`, `BASH_SOURCE`, and a
// `SCRIPT_DIR` assigned from one of them are what appears there — and when it does not,
// the cost is an edge to a sibling file rather than an invented dependency.
//
// An expansion anywhere *else* in the path is the third shape and is not read. There the
// unknown part is a path segment rather than an anchor, so no assumption recovers it, and
// a guess would put a file in the graph that nothing names.
func shSource(cl codeLine) (Import, bool) {
	code := strings.TrimSpace(cl.Text)
	kw := "."
	if strings.HasPrefix(code, "source") {
		kw = "source"
	}
	idx := strings.Index(cl.Text, kw)
	if idx < 0 {
		return Import{}, false
	}
	// Read from Raw, because the scanner blanked the string body — the rule every
	// quoted-path reader in this package follows.
	arg := shReadWord(strings.TrimSpace(cl.Raw[idx+len(kw):]))
	if arg == "" {
		return Import{}, false
	}
	p, ok := shStripAnchor(arg)
	if !ok {
		return Import{}, false
	}
	return Import{Raw: p, Line: cl.Num}, true
}

// shReadWord reads one shell word from the start of s and returns it unquoted.
//
// A word ends at whitespace or at a command separator, and the whole difficulty is that
// neither ends it *inside* a quote or an expansion. The canonical anchored source is
//
//	source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/lib.sh"
//
// which holds a `&&` five characters from its end, two levels of nested double quotes, and
// a `${...}` inside a `$(...)`. Cutting at the first `&` would leave
// `"$(cd "$(dirname "${BASH_SOURCE[0]}")"` — an unbalanced fragment that no anchor rule can
// recover — and that spelling is what every careful bash script in the wild writes, so
// getting it wrong drops the most correct scripts' only source line.
//
// So the scan tracks quote state and expansion depth, and a separator only ends the word at
// depth zero outside quotes. That is a parser for one word, which is more machinery than
// this package usually spends — justified because the alternative is not a slightly worse
// reading but no reading at all.
//
// The quotes come off as the word is built: they are the author's protection against word
// splitting and carry nothing about the path.
func shReadWord(s string) string {
	var b strings.Builder
	var quote byte
	depth := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
				continue
			}
			// A backslash escapes inside double quotes only; inside single quotes it is a
			// literal, which is the one thing the two quote forms disagree about here.
			if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				continue
			}
			b.WriteByte(c)
		case c == '"' || c == '\'':
			quote = c
		case c == '$' && i+1 < len(s) && (s[i+1] == '(' || s[i+1] == '{'):
			depth++
			b.WriteByte(c)
			i++
			b.WriteByte(s[i])
		case depth > 0 && (c == ')' || c == '}'):
			depth--
			b.WriteByte(c)
		case depth > 0:
			b.WriteByte(c)
		case c == ' ' || c == '\t' || c == '\r':
			return b.String()
		case c == ';' || c == '&' || c == '|' || c == '#' || c == '>' || c == '<':
			// A separator, a trailing comment, or a redirection: `source lib.sh || exit 1`
			// and `source lib.sh >/dev/null`. None is part of the path.
			return b.String()
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// shStripAnchor removes a leading directory expansion and reports whether what remains
// is a path this extractor can name.
//
// The expansion forms a script uses to anchor at its own directory all end at the first
// `/` after the expansion closes, so the rule is structural rather than a list of
// spellings: consume a balanced `$(...)` or `${...}` or a bare `$name` at the very start,
// then require a `/`. Anything left holding a `$` is the third shape shSource describes
// and is rejected.
func shStripAnchor(p string) (string, bool) {
	if strings.HasPrefix(p, "$") {
		end, ok := shExpansionEnd(p)
		if !ok {
			return "", false
		}
		rest := p[end:]
		if !strings.HasPrefix(rest, "/") {
			// `$lib_file` with no path after it names a whole file by variable. Nothing
			// here can say which, and the anchor assumption does not apply: what is
			// unknown is the file rather than the directory holding it.
			return "", false
		}
		p = "." + rest
	}
	if strings.Contains(p, "$") {
		return "", false
	}
	if p == "" || p == "." {
		return "", false
	}
	// A bare relative path keeps its shape: resolveShell tries the script's directory and
	// then walks outward, because a bare `source` resolves against the invoking
	// directory and no single root is right for every caller.
	return p, true
}

// shExpansionEnd returns the index just past a `$`-expansion at the start of s.
//
// `$(...)` and `${...}` nest — `$(dirname "$0")` holds another expansion and
// `${BASH_SOURCE%/*}` holds none but is delimited the same way — so the closing
// delimiter is found by counting rather than by taking the first one. Taking the first
// `)` in `$(dirname "$0")` would leave `"$0")/util.sh` behind and reject a path this
// reads correctly.
func shExpansionEnd(s string) (int, bool) {
	if len(s) < 2 {
		return 0, false
	}
	switch s[1] {
	case '(', '{':
		open, close := byte('('), byte(')')
		if s[1] == '{' {
			open, close = '{', '}'
		}
		depth := 0
		for i := 1; i < len(s); i++ {
			switch s[i] {
			case open:
				depth++
			case close:
				depth--
				if depth == 0 {
					return i + 1, true
				}
			}
		}
		return 0, false
	}
	// A bare `$name`, which is what a script assigning SCRIPT_DIR once at the top then
	// writes at every source site.
	i := 1
	for i < len(s) && identChar(s[i]) {
		i++
	}
	if i == 1 {
		return 0, false
	}
	return i, true
}

// shFuncName reads a function definition's name.
//
// Two spellings, and a script may use either:
//
//	name() {          POSIX, and what most code writes
//	function name {   bash's, with the parens optional
//
// The name is where the care goes, because a shell name admits nearly every character —
// `my-func`, `pkg::install` and `2fa` are all legal function names in bash — so the rule
// has to be stated as what a name cannot hold rather than what it can. The metacharacters
// are excluded, and so is `=`: `x=(1 2)` is an array assignment whose shape is otherwise
// exactly a definition's, and reading it as one would put a variable on the page as a
// function.
//
// A `case` pattern is the near miss worth naming. `*.sh)` and `(foo)` both hold the
// punctuation of a definition, and neither has the `()` pair adjacent to a name, which is
// what this requires.
func shFuncName(code string) (string, bool) {
	if rest, ok := shCutWord(code, "function"); ok {
		// `function name` and `function name()` are the same declaration. The parens are
		// optional here and required in the POSIX form, which is the whole difference.
		name := rest
		if i := strings.IndexAny(name, " \t({"); i >= 0 {
			name = name[:i]
		}
		if !shIsName(name) {
			return "", false
		}
		return name, true
	}
	// The POSIX form. The parens must be empty — the shell takes no parameter list, and
	// something between them means this is a subshell or a case pattern rather than a
	// definition.
	open := strings.IndexByte(code, '(')
	if open <= 0 {
		return "", false
	}
	rest := strings.TrimLeft(code[open+1:], " \t")
	if !strings.HasPrefix(rest, ")") {
		return "", false
	}
	name := strings.TrimRight(code[:open], " \t")
	if !shIsName(name) {
		return "", false
	}
	return name, true
}

// shIsName reports whether s can be a shell function name.
//
// Stated as an exclusion because bash's rule is one: a function name is any word, and a
// word is anything not holding a metacharacter. `my-func` and `pkg::install` are ordinary
// names in real scripts, so a rule built from identifier characters would drop them.
func shIsName(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, " \t|&;<>()$`\\\"'=!*?[]{}#~") {
		return false
	}
	return true
}

// shDeclaredVar reads the name of a variable a script declares as part of its surface.
//
// Only the three declaring forms, and never a plain assignment. `x=1` is how a shell
// script holds a loop counter, and recording every one of them would bury the handful a
// caller is meant to see under the dozens it is not. `export`, `readonly` and
// `declare -r` are the forms that say something to a reader: exported crosses into child
// processes, readonly is the shell's only constant.
//
// `local` is deliberately absent, and it is the negative case that matters. It is the one
// scoping keyword the shell has, so a `local` name is the one name that is *not* surface —
// recording it would report a function's internals as the script's interface.
func shDeclaredVar(code string) (string, bool) {
	rest, ok := shCutAnyWord(code, "export", "readonly", "declare", "typeset")
	if !ok {
		return "", false
	}
	// `declare -r X=1` and `readonly -a X=(...)`: the flags come before the name.
	for strings.HasPrefix(rest, "-") {
		i := strings.IndexAny(rest, " \t")
		if i < 0 {
			return "", false
		}
		rest = strings.TrimLeft(rest[i:], " \t")
	}
	name := rest
	if i := strings.IndexAny(name, "= \t"); i >= 0 {
		name = name[:i]
	}
	// A variable name is stricter than a function name: it is what goes after `$`, so it
	// is an identifier and nothing else.
	if name == "" || !shIsVarName(name) {
		return "", false
	}
	return name, true
}

// shIsVarName reports whether s is a shell variable name.
func shIsVarName(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		ok := c == '_' ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9')
		if !ok {
			return false
		}
	}
	return true
}

// shCutWord returns what follows kw when kw is the line's first whole word.
func shCutWord(code, kw string) (string, bool) {
	if !strings.HasPrefix(code, kw) {
		return "", false
	}
	rest := code[len(kw):]
	if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
		// `functions` and `exported_at` begin with the keyword's letters and are not it.
		return "", false
	}
	return strings.TrimLeft(rest, " \t"), true
}

// shCutAnyWord is shCutWord over several keywords.
func shCutAnyWord(code string, kws ...string) (string, bool) {
	for _, kw := range kws {
		if rest, ok := shCutWord(code, kw); ok {
			return rest, true
		}
	}
	return "", false
}

// shDoc reads the `#` comment block above a declaration.
//
// Read from Raw, because the scanner strips comments. The shell has no doc-comment
// syntax, so a run of `#` lines is the whole convention — the same position the Ruby
// extractor is in, and shells being what they are, the block above a function is where
// scripts have always put their prose.
func shDoc(lines []codeLine, idx int) string {
	const maxDoc = 200

	var block []string
	for i := idx - 1; i >= 0 && idx-i < maxDoc; i-- {
		t := strings.TrimSpace(lines[i].Raw)
		if !strings.HasPrefix(t, "#") {
			break
		}
		body := strings.TrimSpace(strings.TrimPrefix(t, "#"))
		// A shebang is an interpreter directive rather than prose, and it is the one
		// comment that can sit directly above the first declaration in a short script.
		if strings.HasPrefix(t, "#!") {
			break
		}
		// A shellcheck directive is tooling configuration, the same case as a rubocop one.
		if strings.HasPrefix(body, "shellcheck ") || strings.HasPrefix(body, "shellcheck.") {
			break
		}
		// A rule of `#` characters is a section divider a great many scripts use to
		// separate functions. It is punctuation, not a sentence about what follows.
		if body == "" || strings.Trim(body, "#-=*_ ") == "" {
			break
		}
		block = append([]string{body}, block...)
	}
	if len(block) == 0 {
		return ""
	}
	return FirstSentence(strings.TrimSpace(strings.Join(block, " ")))
}
