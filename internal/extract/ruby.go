package extract

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// RubyExtractor reads Ruby requires, module and class surface, and methods.
//
// Line-oriented, not a parser (design §4.1). Ruby differs from every other language
// this package reads in three ways that shape the whole extractor:
//
//   - Scope is closed by the keyword `end`, not by a brace. So the scope stack is
//     driven by counting openers against `end` rather than by netDepth, and the
//     openers are many: `class`, `module`, `def`, `if`, `while`, `do`, `begin`, `case`.
//     Miscounting one leaves a scope open and attributes the rest of the file to it.
//   - There is no import statement. `require "json"` is a method call whose argument
//     happens to name a file, and `require_relative "../db"` is the same call
//     resolving against the caller's directory. Both are read as imports because
//     that is what they are, but neither is syntax the language enforces.
//   - Visibility is a *sticky section marker*. A bare `private` on its own line makes
//     every method after it in that class private, until a `public` reverses it. This
//     is unlike every other language here, where visibility is a modifier on the
//     declaration, and getting it wrong reports a class's whole internals as public
//     surface.
//
// A module or class name is recorded under its own name, not qualified — `Api::V2` is
// two nested declarations in the common form and one path in the compact form, and
// the resolution map (see addRubyModule) is what reconciles them.
type RubyExtractor struct{}

// Langs implements Extractor.
func (RubyExtractor) Langs() []discover.Lang { return []discover.Lang{discover.LangRuby} }

// Extract implements Extractor.
func (RubyExtractor) Extract(f discover.File) (Facts, error) {
	facts := Facts{Path: f.Path, Lang: discover.LangRuby}
	lines := scanLines(f.Content, scanRuby)

	// The scope stack: modules and classes, innermost last. A def inside a class is a
	// method of it; a def inside a def is a local definition nothing outside can call.
	var scopes []rubyScope
	// depth counts every open construct, not only types. A method's body is at least
	// one deeper than the class it belongs to, which is what tells a declaration from
	// a statement — the same rule jvmDirectMember states for the brace languages.
	depth := 0

	for i := 0; i < len(lines); i++ {
		cl := lines[i]
		code := strings.TrimSpace(cl.Text)
		if code == "" {
			continue
		}
		// A wrapped argument list: `require_relative(` or a long method signature.
		joined, last := joinParens(lines, i)
		jcode := strings.TrimSpace(joined.Text)

		switch {
		case rubyIsRequire(jcode):
			if im, ok := rubyRequire(joined); ok {
				facts.Imports = append(facts.Imports, im)
			}

		case rubyIsTypeDecl(jcode):
			name := rubyTypeDecl(jcode)
			if name != "" && rubyDeclSite(scopes, depth) {
				// The namespace this file declares, which is what a caller writes to reach
				// into it: `Api::V2::Users`. Recorded from the first declaration that
				// reaches the deepest point, so a file holding `module Api / module V2 /
				// class Users` reports the whole path rather than only its outermost
				// module. This is the Ruby analogue of Java's package clause, and unlike
				// Java's it is not a declaration of its own — it has to be assembled from
				// the nesting.
				if qn := rubyQualify(scopes, jcode); len(qn) > len(facts.Package) {
					facts.Package = qn
				}
				facts.Symbols = append(facts.Symbols, Symbol{
					// A module and a class are both a named thing holding members, which is
					// the distinction SymClass draws against SymType — the same call
					// jvmTypeKind makes for a Kotlin object.
					Name: name, Kind: SymClass,
					// Ruby has no way to make a constant private in the sense that matters
					// here: a class or module name is a constant, and every constant is
					// reachable through its namespace. So a declared type is surface.
					Exported: true,
					Doc:      rubyDoc(lines, i), Line: cl.Num,
				})
			}
			switch {
			case name != "":
				scopes = append(scopes, rubyScope{name: name, depth: depth})
			case len(scopes) > 0 && rubyDirectMember(scopes, depth):
				// `class << self` opens the singleton class, which takes an `end` like any
				// other body but declares no name: its methods belong to the type enclosing
				// it. So a scope is pushed anyway — the stack has to stay balanced against
				// that `end` — carrying the enclosing type's name, which is what attributes
				// `def build` inside it to that type rather than dropping it for sitting one
				// depth deeper than a declaration site.
				//
				// The name is copied and the private flag is not: a section marker inside the
				// singleton class applies to the singleton methods, and copying the flag would
				// let the enclosing class's `private` hide a public class method.
				scopes = append(scopes, rubyScope{name: scopes[len(scopes)-1].name, depth: depth})
			}
			depth++

		case rubyIsDef(jcode):
			name := rubyDefName(jcode)
			if name != "" && rubyDeclSite(scopes, depth) {
				sym := Symbol{
					Name: name, Kind: SymFunc,
					Exported: true,
					Doc:      rubyDoc(lines, i), Line: cl.Num,
				}
				if len(scopes) > 0 {
					// A `def self.build` class method is owned by the same type as an
					// instance method and is equally surface; the distinction between them
					// is not one Symbol can carry, so it is not recorded.
					owner := scopes[len(scopes)-1]
					sym.Kind = SymMethod
					sym.Recv = owner.name
					// The sticky section marker, and `private def name` as the inline form
					// that a modern codebase uses instead.
					sym.Exported = !owner.private && !rubyInlinePrivate(jcode)
				}
				facts.Symbols = append(facts.Symbols, sym)
			}
			// A one-line `def x = expr` (Ruby 3 endless method) opens no body.
			if !rubyEndlessDef(jcode) {
				depth++
			}

		case rubyIsAttr(jcode):
			// attr_reader/:writer/:accessor declare methods. They are the idiomatic way a
			// Ruby class states its public data surface, and a class that uses them
			// otherwise reports no readable attributes at all.
			if len(scopes) > 0 && rubyDirectMember(scopes, depth) {
				owner := scopes[len(scopes)-1]
				for _, n := range rubySymbolList(joined.Raw) {
					facts.Symbols = append(facts.Symbols, Symbol{
						Name: n, Kind: SymMethod, Recv: owner.name,
						Exported: !owner.private, Line: cl.Num,
					})
				}
			}

		case rubyIsVisibilityMarker(code):
			// The section marker itself. It applies to the innermost open type, which is
			// why it is recorded on the scope rather than in a single variable: an inner
			// class's `private` must not leak out to the enclosing one.
			if len(scopes) > 0 && rubyDirectMember(scopes, depth) {
				scopes[len(scopes)-1].private = code != "public"
			}

		case rubyIsConstant(jcode):
			if rubyDeclSite(scopes, depth) {
				if name := rubyConstantName(jcode); name != "" {
					sym := Symbol{Name: name, Kind: SymConst, Exported: true, Line: cl.Num}
					if len(scopes) > 0 {
						// A constant declared in a class body belongs to that class:
						// `Service::DEFAULT_TIMEOUT` is how a caller names it, and recording it
						// unqualified would present it as a top-level constant of the whole
						// repository. The visibility marker deliberately does not apply — a
						// `private` section makes methods private and leaves constants
						// reachable, which is Ruby's rule and not an oversight.
						sym.Recv = scopes[len(scopes)-1].name
					}
					facts.Symbols = append(facts.Symbols, sym)
				}
			}

		default:
			// Everything else only moves the depth. `if`, `while`, `do`, `case` and
			// `begin` all take an `end`, and a modifier form — `x if y` — does not, which
			// rubyOpeners is what separates.
			depth += rubyOpeners(code)
		}

		// `end` closes whatever is innermost. Counted after the declaration cases so a
		// one-line `class Foo; end` both opens and closes.
		depth -= rubyEnds(code)
		if depth < 0 {
			depth = 0
		}
		for len(scopes) > 0 && depth <= scopes[len(scopes)-1].depth {
			scopes = scopes[:len(scopes)-1]
		}
		i = last
	}
	return facts, nil
}

// rubyScope is one open module or class declaration.
type rubyScope struct {
	name string
	// depth is the open-construct count at the declaring line. Its members are at
	// depth+1.
	depth int
	// private records that a bare `private` has been seen in this body, which makes
	// every subsequent method private until a `public` reverses it. Per-scope rather
	// than global: an inner class's marker must not leak to the enclosing one.
	private bool
}

// rubyDirectMember reports whether depth is inside a type's own body rather than
// nested further inside a method or a control-flow block.
func rubyDirectMember(scopes []rubyScope, depth int) bool {
	return len(scopes) > 0 && depth == scopes[len(scopes)-1].depth+1
}

// rubyDeclSite reports whether a position can hold a declaration that is surface:
// the top level of a file, or a type's own body.
func rubyDeclSite(scopes []rubyScope, depth int) bool {
	if len(scopes) == 0 {
		return depth == 0
	}
	return rubyDirectMember(scopes, depth)
}

// rubyIsRequire reports whether a line is a require call.
func rubyIsRequire(code string) bool {
	for _, kw := range []string{"require ", "require(", "require_relative ", "require_relative(",
		"load ", "load("} {
		if strings.HasPrefix(code, kw) {
			return true
		}
	}
	return false
}

// rubyRequire reads the path out of a require call.
//
// The path is read from Raw, because the scanner blanked the string body — the same
// rule every quoted-path reader in this package follows, and the one whose violation
// has produced a defect in more than one language.
//
// `require_relative` is kept distinguishable from `require` by keeping the leading
// "./" that resolution needs: the two resolve against different roots, and a resolver
// that cannot tell them apart either invents a gem for a local file or misses a real
// gem. The marker is a "./" prefix on a path that had none, which is the same
// convention the TS resolver already reads as "relative".
func rubyRequire(cl codeLine) (Import, bool) {
	code := strings.TrimSpace(cl.Text)
	kw := "require"
	relative := false
	if strings.HasPrefix(code, "require_relative") {
		kw, relative = "require_relative", true
	} else if strings.HasPrefix(code, "load") {
		kw = "load"
		// `load` takes a filename rather than a feature name, so it is relative in the
		// same sense require_relative is — except that it resolves against $LOAD_PATH.
		// Treated as a plain require, which is the reading that resolves.
	}
	idx := strings.Index(cl.Text, kw)
	if idx < 0 {
		return Import{}, false
	}
	// The argument must *be* a literal, not merely contain one. `require File.join(__dir__,
	// "thing")` computes the path, and a scan that took the first quote anywhere on the line
	// would read "thing" as the whole require — inventing a top-level module from what is
	// actually one segment of a path this extractor cannot resolve. So the search starts at
	// the first non-blank byte after the keyword and stops there if it is not a quote.
	arg := idx + len(kw)
	for arg < len(cl.Raw) && (cl.Raw[arg] == ' ' || cl.Raw[arg] == '\t' || cl.Raw[arg] == '(') {
		arg++
	}
	if arg >= len(cl.Raw) || (cl.Raw[arg] != '"' && cl.Raw[arg] != '\'') {
		// `require File.join(...)` and `require some_var` compute the path. Nothing here
		// can say what it is, and inventing a name would put a module in the graph that
		// no file declares.
		return Import{}, false
	}
	path, ok := stringAt(cl.Raw, arg)
	if !ok || path == "" {
		return Import{}, false
	}
	if relative && !strings.HasPrefix(path, ".") {
		path = "./" + path
	}
	return Import{Raw: path, Line: cl.Num}, true
}

// rubyIsTypeDecl reports whether a line declares a module or class.
func rubyIsTypeDecl(code string) bool {
	for _, kw := range []string{"class ", "module ", "class<<", "class <<"} {
		if strings.HasPrefix(code, kw) {
			return true
		}
	}
	return false
}

// rubyTypeDecl reads the name of a module or class declaration.
//
// For the compact `class Api::V2::Users` form — one line declaring what the nested
// form spells as three — the name is the last segment, because that is the type. The
// segments before it are the namespace, and no symbol is recorded for them: the file
// does not declare `Api`, it declares something inside it, and recording the namespace
// as a class would put a type on the page that this file does not define.
//
// `class << self` declares no name: it opens the singleton class, and its methods
// belong to the enclosing type. Returning no name means no scope is pushed, which
// leaves those methods attributed to the type they are actually on.
func rubyTypeDecl(code string) string {
	var rest string
	switch {
	case strings.HasPrefix(code, "module "):
		rest = code[len("module "):]
	case strings.HasPrefix(code, "class "):
		rest = code[len("class "):]
	default:
		// `class<<self`, with or without the space.
		return ""
	}
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "<<") {
		return ""
	}
	// A superclass or a subject: `class Foo < Bar` and `class Foo = Struct.new(...)`.
	if i := strings.IndexAny(rest, " <;(="); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.LastIndex(rest, "::"); i >= 0 {
		rest = rest[i+2:]
	}
	if !rubyIsConstName(rest) {
		return ""
	}
	return rest
}

// rubyQualify builds the fully qualified name a declaration sits at.
//
// The enclosing scopes joined with `::`, plus whatever the declaring line itself
// spells — which for the compact form is a path rather than a name, so
// `module Api` containing `class V2::Users` qualifies to `Api::V2::Users`.
func rubyQualify(scopes []rubyScope, code string) string {
	parts := make([]string, 0, len(scopes)+1)
	for _, s := range scopes {
		parts = append(parts, s.name)
	}
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(code, "class"), "module"))
	if i := strings.IndexAny(rest, " <;(="); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimPrefix(rest, "::")
	if rest == "" {
		return ""
	}
	parts = append(parts, rest)
	return strings.Join(parts, "::")
}

// rubyIsConstName reports whether s is a Ruby constant name, which is what a module
// or class must be: an identifier beginning with a capital.
func rubyIsConstName(s string) bool {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !identChar(s[i]) {
			return false
		}
	}
	return true
}

// rubyIsDef reports whether a line declares a method.
func rubyIsDef(code string) bool {
	return strings.HasPrefix(code, "def ") || strings.HasPrefix(code, "private def ") ||
		strings.HasPrefix(code, "protected def ") || strings.HasPrefix(code, "public def ")
}

// rubyDefName reads a method name.
//
// Ruby method names admit characters no other language here allows: a trailing `?`
// marks a predicate, `!` a mutating variant, and `=` a setter, and all three are part
// of the name. Dropping them would merge `valid?` with a `valid` that may also exist,
// and would rename every setter in the codebase.
//
// An operator method — `def ==(other)`, `def <=>`, `def []` — is also a real name.
// These are read as-is rather than rejected, because a class defining `<=>` is
// declaring that it is comparable, which is real surface.
func rubyDefName(code string) string {
	for _, m := range []string{"private def ", "protected def ", "public def "} {
		if strings.HasPrefix(code, m) {
			code = code[len(m)-len("def "):]
			break
		}
	}
	rest := strings.TrimSpace(strings.TrimPrefix(code, "def "))
	if rest == "" {
		return ""
	}
	// `def self.build` and the rarer `def Klass.build` name a class method, whose own
	// name is what follows the dot.
	if i := strings.IndexByte(rest, '.'); i > 0 && !strings.ContainsAny(rest[:i], "( ") {
		if recv := rest[:i]; recv == "self" || rubyIsConstName(recv) {
			rest = strings.TrimSpace(rest[i+1:])
		}
	}
	// The name ends at the parameter list or at whitespace.
	end := 0
	for end < len(rest) && (identChar(rest[end]) || rubyOperatorChar(rest[end])) {
		end++
	}
	name := rest[:end]
	// A setter's `=` is part of its name and is written attached: `def name=(v)`. The
	// endless form puts a space before it — `def name = expr` — and its name is `name`,
	// so a trailing `=` followed by whitespace is the operator, not the name.
	if strings.HasSuffix(name, "=") && end < len(rest) && (rest[end] == ' ' || rest[end] == '\t') {
		name = strings.TrimSuffix(name, "=")
	}
	if name == "" || name == "=" {
		return ""
	}
	return name
}

// rubyOperatorChar reports whether b can appear in an operator method name.
func rubyOperatorChar(b byte) bool {
	return strings.IndexByte("+-*/%<>=!~^&|[]?", b) >= 0
}

// rubyEndlessDef reports whether a def is Ruby 3's one-line form, which opens no
// body and so takes no `end`.
//
//	def name = expression
//
// Distinguished from a setter by the space before the `=`, and from a default
// parameter value by requiring the `=` to sit outside the parameter list.
func rubyEndlessDef(code string) bool {
	rest := strings.TrimSpace(strings.TrimPrefix(code, "def "))
	depth := 0
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case '=':
			if depth > 0 {
				continue
			}
			// `==`, `<=`, `>=`, `!=` and `=~` are operator method names, not the
			// endless-def marker, and a setter's `=` is attached to the name.
			if i > 0 && (rubyOperatorChar(rest[i-1]) || identChar(rest[i-1])) {
				continue
			}
			if i+1 < len(rest) && rest[i+1] == '=' {
				continue
			}
			return true
		}
	}
	return false
}

// rubyInlinePrivate reports whether a def line carries an inline visibility modifier.
func rubyInlinePrivate(code string) bool {
	return strings.HasPrefix(code, "private def ") || strings.HasPrefix(code, "protected def ")
}

// rubyIsAttr reports whether a line is an attr_* declaration.
func rubyIsAttr(code string) bool {
	for _, kw := range []string{"attr_reader", "attr_writer", "attr_accessor"} {
		if strings.HasPrefix(code, kw) {
			// The next character must not continue the identifier, so `attr_readers` is
			// not matched.
			rest := code[len(kw):]
			if rest == "" || !identChar(rest[0]) {
				return true
			}
		}
	}
	return false
}

// rubySymbolList reads the :symbol arguments of a line.
//
// Read from Raw. A symbol is not a string, so the scanner leaves it intact — but a
// quoted-string form (`attr_reader "name"`) is legal too, and reading Raw handles
// both without a second rule.
func rubySymbolList(raw string) []string {
	var out []string
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == '#' {
			break
		}
		if c != ':' {
			continue
		}
		// `::` is a namespace separator and `a ? b : c` a ternary; neither opens a
		// symbol. A symbol's colon is followed immediately by an identifier character.
		if i+1 >= len(raw) || !identChar(raw[i+1]) {
			continue
		}
		if i > 0 && (raw[i-1] == ':' || identChar(raw[i-1])) {
			continue
		}
		j := i + 1
		for j < len(raw) && (identChar(raw[j]) || raw[j] == '?' || raw[j] == '!') {
			j++
		}
		out = append(out, raw[i+1:j])
		i = j - 1
	}
	return out
}

// rubyIsVisibilityMarker reports whether a line is a bare visibility keyword, which
// makes every method after it in the enclosing body take that visibility.
//
// Only the bare form. `private :helper` names one method and `private def x` is
// inline, and treating either as a section marker would make the rest of the class
// private when the author scoped it to one method.
func rubyIsVisibilityMarker(code string) bool {
	switch code {
	case "private", "protected", "public":
		return true
	}
	return false
}

// rubyIsConstant reports whether a line assigns a top-level constant.
func rubyIsConstant(code string) bool {
	eq := strings.IndexByte(code, '=')
	if eq <= 0 {
		return false
	}
	return rubyIsConstName(strings.TrimSpace(code[:eq]))
}

// rubyConstantName reads the name of a constant assignment.
//
// Ruby has no const keyword: a constant is any assignment whose name begins with a
// capital, which is the only signal available — the same position Python's
// SCREAMING_CASE convention puts pyAssignSymbol in.
func rubyConstantName(code string) string {
	eq := strings.IndexByte(code, '=')
	if eq <= 0 {
		return ""
	}
	// Reject comparison and augmented assignment, which are statements: ==, ||=, <=.
	if eq+1 < len(code) && code[eq+1] == '=' {
		return ""
	}
	if strings.IndexByte("!<>+-*/%&|^~", code[eq-1]) >= 0 {
		return ""
	}
	name := strings.TrimSpace(code[:eq])
	if !rubyIsConstName(name) {
		return ""
	}
	return name
}

// rubyOpeners counts the constructs on a line that will require an `end`.
//
// This is the extractor's hardest rule, and the reason is that Ruby spells the same
// keyword two ways. `if x then y end` opens a block; `y if x` is a modifier and opens
// nothing. The difference is only position: a modifier form has the keyword after an
// expression rather than at the start of a statement. So a keyword counts only when it
// begins the line or follows something that can precede a statement — `;`, `=`, `(`,
// or another opener.
//
// `do` is counted when it ends a line or is followed by a block parameter list, which
// is the block form: `items.each do |x|`. A `while ... do` on one line would
// double-count, so `do` is not counted when the line already opened a `while`.
func rubyOpeners(code string) int {
	n := 0
	words := rubyStatementWords(code)
	for i, w := range words {
		switch w {
		case "class", "module", "def", "begin", "case", "unless", "until", "if", "while", "for":
			// Only in statement position. rubyStatementWords already dropped the
			// modifier occurrences, so anything surviving here opens a block.
			n++
		case "do":
			// `while x do` and `for x in y do` reuse the block keyword for a loop whose
			// `while` already counted.
			if i > 0 && rubyLoopOpened(words[:i]) {
				continue
			}
			n++
		}
	}
	return n
}

// rubyLoopOpened reports whether the preceding words already opened a loop whose
// `do` is punctuation rather than a block.
func rubyLoopOpened(words []string) bool {
	for _, w := range words {
		switch w {
		case "while", "until", "for":
			return true
		}
	}
	return false
}

// rubyStatementWords tokenises a line, keeping only the keywords that are in
// statement position.
//
// The modifier forms are what this drops. `return x if y` and `puts a unless b` put a
// block keyword where it opens nothing, and counting them leaves a phantom scope open
// for the rest of the file — which then swallows every declaration after it. So a
// keyword survives only at the start of the line or after a token that can precede a
// statement.
func rubyStatementWords(code string) []string {
	var out []string
	i := 0
	// stmtStart says whether the next token is in statement position, which is what
	// separates an opener from a modifier.
	stmtStart := true
	for i < len(code) {
		c := code[i]
		if c == ' ' || c == '\t' {
			i++
			continue
		}
		if !identChar(c) {
			// `;` and `then` end a statement; `=`, `(`, `&&`, `||` and `,` all put what
			// follows in expression position, where a keyword is still an opener —
			// `x = if y ... end` is legal and does open a block.
			switch c {
			case ';', '=', '(', '{', '|', '&', ',', '?', ':':
				stmtStart = true
			default:
				stmtStart = false
			}
			i++
			continue
		}
		j := i
		for j < len(code) && identChar(code[j]) {
			j++
		}
		w := code[i:j]
		if rubyIsBlockKeyword(w) {
			// `do` is never a modifier, and `end` is never in doubt.
			if w == "do" || stmtStart {
				out = append(out, w)
			}
		} else if w == "then" {
			stmtStart = true
			i = j
			continue
		}
		// After an identifier, a keyword is in modifier position: `value if cond`.
		stmtStart = false
		i = j
	}
	return out
}

// rubyIsBlockKeyword reports whether a token opens a block that takes an `end`.
func rubyIsBlockKeyword(w string) bool {
	switch w {
	case "class", "module", "def", "begin", "case", "unless", "until", "if", "while",
		"for", "do":
		return true
	}
	return false
}

// rubyEnds counts the `end` keywords on a line.
//
// Matched as a whole token: `send`, `append` and `backend` all contain the letters,
// and a variable named `end_time` is legal. Miscounting here is the mirror of
// miscounting an opener, and just as destructive.
func rubyEnds(code string) int {
	n := 0
	for i := 0; i+3 <= len(code); i++ {
		if code[i:i+3] != "end" {
			continue
		}
		if i > 0 && identChar(code[i-1]) {
			continue
		}
		if i+3 < len(code) && identChar(code[i+3]) {
			continue
		}
		n++
		i += 2
	}
	return n
}

// rubyDoc reads the `#` comment block above a declaration.
//
// Read from Raw, because the scanner strips comments. Ruby has no doc-comment
// delimiter — a run of `#` lines is the convention, and RDoc reads exactly that — so
// the block is gathered upward until a line that is not a comment.
func rubyDoc(lines []codeLine, idx int) string {
	const maxDoc = 200

	i := idx - 1
	// A blank line between the comment and the declaration breaks the association, the
	// same rule Go's doc comments follow. Nothing is skipped.
	var block []string
	for ; i >= 0 && idx-i < maxDoc; i-- {
		t := strings.TrimSpace(lines[i].Raw)
		if !strings.HasPrefix(t, "#") {
			break
		}
		// A magic comment is an interpreter directive, not prose: `# frozen_string_literal:
		// true` and `# encoding: utf-8` say nothing about the declaration below them.
		body := strings.TrimSpace(strings.TrimPrefix(t, "#"))
		if rubyMagicComment(body) {
			break
		}
		// A rubocop directive is tooling configuration for the same reason.
		if strings.HasPrefix(body, "rubocop:") || strings.HasPrefix(body, ":nodoc:") {
			break
		}
		block = append([]string{body}, block...)
	}
	if len(block) == 0 {
		return ""
	}
	return FirstSentence(strings.TrimSpace(strings.Join(block, " ")))
}

// rubyMagicComment reports whether a comment body is an interpreter directive.
func rubyMagicComment(body string) bool {
	for _, d := range []string{"frozen_string_literal:", "encoding:", "coding:",
		"warn_indent:", "shareable_constant_value:", "!"} {
		if strings.HasPrefix(body, d) {
			return true
		}
	}
	return false
}
