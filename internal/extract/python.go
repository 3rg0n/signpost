package extract

import (
	"strings"

	"github.com/cisco-sbg-emu/signpost/internal/discover"
)

// PythonExtractor reads Python imports and top-level declarations.
//
// Line-oriented, not a parser (design §4.1). The scoring harness holds it to
// F1 ≥ 0.95 on imports and ≥ 0.90 on symbols, and the design's answer to falling
// short is to record the language in skipped_checks — not to widen the patterns
// until the numbers look better.
//
// Two Python-specific decisions:
//
//   - Indentation, not braces, decides scope. Only declarations at column 0 are
//     module surface; a def inside a class is a method, and a def inside a
//     function is a closure nothing outside can call. The scanner's Indent field
//     is what makes this cheap.
//   - Exportedness is the leading-underscore convention, overridden by __all__
//     when a module declares one. __all__ is the module author saying explicitly
//     what the public surface is, so it wins over the convention.
type PythonExtractor struct{}

// Langs implements Extractor.
func (PythonExtractor) Langs() []discover.Lang { return []discover.Lang{discover.LangPython} }

// Extract implements Extractor.
func (PythonExtractor) Extract(f discover.File) (Facts, error) {
	facts := Facts{Path: f.Path, Lang: discover.LangPython}
	facts.Package = pyModulePath(f.Path)

	lines := scanLines(f.Content, scanPython)
	// Class bodies contribute methods, so the extractor tracks the innermost
	// enclosing class by indentation. A dict rather than a stack: nesting deeper
	// than one class is rare enough that the extra fidelity is not worth the code.
	var className string
	var classIndent int
	inClass := false

	var allNames []string
	haveAll := false

	for i := 0; i < len(lines); i++ {
		cl := lines[i]
		code := strings.TrimSpace(cl.Text)
		if code == "" {
			continue
		}
		// A wrapped statement is joined on demand; joinParens deliberately does not
		// count braces, so a def's body is never folded into its signature.
		joined, last := joinParens(lines, i)
		jcode := strings.TrimSpace(joined.Text)

		// Leaving a class body: any code at or left of the class's own indent.
		if inClass && cl.Indent <= classIndent {
			inClass = false
			className = ""
		}

		switch {
		case strings.HasPrefix(jcode, "import ") || strings.HasPrefix(jcode, "from "):
			// An import inside a function is still a real dependency — it is a lazy
			// import, a common way to break a cycle — so indentation is not checked
			// here, unlike for declarations.
			facts.Imports = append(facts.Imports, pyImports(joined)...)
			i = last

		case strings.HasPrefix(jcode, "def ") || strings.HasPrefix(jcode, "async def "):
			if s, ok := pyFuncSymbol(jcode, cl); ok {
				s.Doc = pyDocstring(lines, last+1)
				if cl.Indent == 0 {
					facts.Symbols = append(facts.Symbols, s)
				} else if inClass && cl.Indent > classIndent {
					s.Kind = SymMethod
					s.Recv = className
					// A method on a private class is not public surface, the same
					// reasoning as an exported Go method on an unexported type.
					if strings.HasPrefix(className, "_") {
						s.Exported = false
					}
					facts.Symbols = append(facts.Symbols, s)
				}
				// Anything else is a closure: unreachable from outside, so not
				// surface.
			}
			i = last

		case strings.HasPrefix(jcode, "class "):
			if s, ok := pyClassSymbol(jcode, cl); ok {
				s.Doc = pyDocstring(lines, last+1)
				if cl.Indent == 0 {
					facts.Symbols = append(facts.Symbols, s)
				}
				// Track it either way, so a nested class's methods do not get
				// attributed to the outer one.
				className = s.Name
				classIndent = cl.Indent
				inClass = true
			}
			i = last

		case cl.Indent == 0 && strings.HasPrefix(jcode, "__all__"):
			// __all__ can be written as a list or a tuple across several lines,
			// which is why this reads the joined form.
			allNames = pyAllNames(joined.Raw)
			haveAll = true
			i = last

		case cl.Indent == 0 && pyIsMainGuard(jcode):
			facts.Entrypoints = append(facts.Entrypoints, "__main__")
			i = last

		case cl.Indent == 0:
			if s, ok := pyAssignSymbol(jcode, cl); ok {
				facts.Symbols = append(facts.Symbols, s)
				i = last
			}
		}
	}

	if haveAll {
		facts.Symbols = pyApplyAll(facts.Symbols, allNames)
		facts.Symbols = append(facts.Symbols, pyReExports(facts, allNames)...)
	}
	return facts, nil
}

// pyReExports adds symbols for names a module exports but does not declare.
//
// A package's __init__.py commonly consists of nothing but imports and an
// __all__ that re-exports them. Without this, asking "what does this package
// expose" returns nothing for precisely the file whose only purpose is to expose
// things — the caller writes `from pkg import Engine`, so Engine is part of pkg's
// surface regardless of where it was defined.
//
// The symbol carries no Kind, because the extractor genuinely does not know from
// this file alone whether the re-exported name is a class, a function, or a
// constant. Resolution fills that in when the defining module is available;
// guessing here would put a wrong Kind in the bundle.
func pyReExports(facts Facts, allNames []string) []Symbol {
	declared := make(map[string]bool, len(facts.Symbols))
	for _, s := range facts.Symbols {
		if s.Kind != SymMethod {
			declared[s.Name] = true
		}
	}
	imported := make(map[string]bool)
	for _, im := range facts.Imports {
		for _, n := range im.Names {
			imported[n] = true
		}
		if im.Alias != "" {
			imported[im.Alias] = true
		}
	}

	var out []Symbol
	for _, n := range allNames {
		// Only a name the file actually imported. A name in __all__ that is
		// neither declared nor imported is a bug in the module, and inventing a
		// symbol for it would propagate that bug into the bundle.
		if declared[n] || !imported[n] {
			continue
		}
		out = append(out, Symbol{Name: n, Exported: true})
	}
	return out
}

// pyModulePath turns a file path into a dotted module path, which is how Python
// code refers to it and therefore how an import resolves against it.
//
// src/pkg/mod.py -> pkg.mod, and pkg/__init__.py -> pkg, because importing a
// package runs its __init__ and the two are the same module to a caller.
func pyModulePath(path string) string {
	p := strings.TrimSuffix(strings.ReplaceAll(path, "\\", "/"), ".py")
	p = strings.TrimSuffix(p, "/__init__")
	// A source root is a directory convention, not part of the module path.
	for _, root := range []string{"src/", "lib/", "python/"} {
		if strings.HasPrefix(p, root) {
			p = p[len(root):]
			break
		}
	}
	if p == "" || p == "__init__" {
		return ""
	}
	return strings.ReplaceAll(p, "/", ".")
}

// pyImports parses a joined import statement.
//
// Both forms carry different information and both matter:
//
//	import a.b.c as abc      -> module dependency, aliased
//	from .rel import x, y    -> module dependency plus the names used from it
//
// The named symbols are what let the graph draw an edge to a specific
// declaration rather than only to the module.
func pyImports(cl codeLine) []Import {
	code := strings.TrimSpace(cl.Text)

	if strings.HasPrefix(code, "from ") {
		rest := code[len("from "):]
		idx := strings.Index(rest, " import ")
		if idx < 0 {
			return nil
		}
		mod := strings.TrimSpace(rest[:idx])
		if mod == "" {
			return nil
		}
		im := Import{Raw: mod, Line: cl.Num}
		names, alias := pyImportNames(rest[idx+len(" import "):])
		// `from x import *` names nothing specific; the star is not a symbol, and
		// recording it as one would put a bogus name in the graph.
		if len(names) == 1 && names[0] == "*" {
			names = nil
		}
		im.Names = names
		// A single aliased name is the only case where the alias belongs to the
		// import as a whole; `from x import a as b, c as d` has per-name aliases
		// that Facts has no field for, and inventing one for a rare form is not
		// worth it.
		if len(names) == 1 {
			im.Alias = alias
		}
		return []Import{im}
	}

	// `import a, b.c as bc` — one statement, several dependencies.
	var out []Import
	for _, part := range splitTopLevel(code[len("import "):], ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, alias := splitAs(part)
		if name == "" {
			continue
		}
		out = append(out, Import{Raw: name, Alias: alias, Line: cl.Num})
	}
	return out
}

// pyImportNames splits the name list of a `from ... import ...` clause,
// returning the names and, for a single aliased name, its alias.
func pyImportNames(s string) (names []string, alias string) {
	s = strings.TrimSpace(s)
	// Parenthesised lists are the wrapped form; the parens are punctuation.
	s = strings.TrimSuffix(strings.TrimPrefix(s, "("), ")")
	for _, part := range splitTopLevel(s, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		n, a := splitAs(part)
		if n == "" {
			continue
		}
		names = append(names, n)
		alias = a
	}
	return names, alias
}

// splitAs splits "name as alias".
func splitAs(s string) (name, alias string) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " as "); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(" as "):])
	}
	return s, ""
}

// pyFuncSymbol reads a def line.
func pyFuncSymbol(code string, cl codeLine) (Symbol, bool) {
	code = strings.TrimPrefix(code, "async ")
	name := identAfter(code, "def ")
	if name == "" {
		return Symbol{}, false
	}
	return Symbol{
		Name:     name,
		Kind:     SymFunc,
		Exported: pyIsPublic(name),
		Line:     cl.Num,
	}, true
}

// pyDocstring reads the docstring that follows a def or class at line index start.
//
// Python's docstring is the first statement of the body, not a preceding comment,
// so this looks forward. It must read Raw: the scanner blanked the string body
// precisely so that a def inside a docstring is not extracted, which means the
// prose only exists in the original bytes.
func pyDocstring(lines []codeLine, start int) string {
	// Skip blank lines between the signature and the body.
	for start < len(lines) && strings.TrimSpace(lines[start].Raw) == "" {
		start++
	}
	if start >= len(lines) {
		return ""
	}
	raw := strings.TrimSpace(lines[start].Raw)
	delim := ""
	for _, d := range []string{`"""`, `'''`, `"`, `'`} {
		if strings.HasPrefix(raw, d) {
			delim = d
			break
		}
	}
	if delim == "" {
		return ""
	}
	body := raw[len(delim):]
	if end := strings.Index(body, delim); end >= 0 {
		return FirstSentence(body[:end])
	}
	// A multi-line docstring: gather until the terminator. Only the first sentence
	// survives anyway, so this stops as soon as one is found.
	var b strings.Builder
	b.WriteString(body)
	for i := start + 1; i < len(lines); i++ {
		l := lines[i].Raw
		if end := strings.Index(l, delim); end >= 0 {
			b.WriteString(" " + l[:end])
			break
		}
		b.WriteString(" " + l)
		if strings.Contains(b.String(), ". ") {
			break
		}
	}
	return FirstSentence(b.String())
}

// pyClassSymbol reads a class line.
func pyClassSymbol(code string, cl codeLine) (Symbol, bool) {
	name := identAfter(code, "class ")
	if name == "" {
		return Symbol{}, false
	}
	return Symbol{
		Name:     name,
		Kind:     SymClass,
		Exported: pyIsPublic(name),
		Line:     cl.Num,
	}, true
}

// pyAssignSymbol reads a module-level constant or variable.
//
// Restricted to a plain `NAME = ...` or `NAME: type = ...`: a tuple unpack or an
// attribute assignment is not a declaration of a new module-level name that a
// caller can import, and matching them produces junk symbols.
func pyAssignSymbol(code string, cl codeLine) (Symbol, bool) {
	eq := strings.IndexByte(code, '=')
	if eq <= 0 {
		return Symbol{}, false
	}
	// Reject comparison and augmented assignment, which are statements not
	// declarations: ==, !=, <=, >=, +=, and so on.
	if eq+1 < len(code) && code[eq+1] == '=' {
		return Symbol{}, false
	}
	if strings.IndexByte("!<>+-*/%&|^~", code[eq-1]) >= 0 {
		return Symbol{}, false
	}
	lhs := strings.TrimSpace(code[:eq])
	// An annotation is fine; the name is what precedes the colon.
	if c := strings.IndexByte(lhs, ':'); c >= 0 {
		lhs = strings.TrimSpace(lhs[:c])
	}
	if !isPyIdent(lhs) {
		return Symbol{}, false
	}
	// A module dunder is interpreter machinery, not a declaration a caller
	// imports — the same reasoning that skips Go's blank identifier.
	if strings.HasPrefix(lhs, "__") && strings.HasSuffix(lhs, "__") {
		return Symbol{}, false
	}
	kind := SymVar
	// SCREAMING_CASE is the Python convention for a constant. There is no const
	// keyword, so the convention is the only signal available.
	if lhs == strings.ToUpper(lhs) && strings.ContainsAny(lhs, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		kind = SymConst
	}
	return Symbol{Name: lhs, Kind: kind, Exported: pyIsPublic(lhs), Line: cl.Num}, true
}

// pyAllNames extracts the string literals from an __all__ assignment. Read from
// Raw because the scanner has blanked the string bodies in Text.
func pyAllNames(raw string) []string {
	var out []string
	i := strings.IndexByte(raw, '=')
	if i < 0 {
		return nil
	}
	for i < len(raw) {
		s, ok := stringAt(raw, i)
		if !ok {
			break
		}
		// Advance past this literal: find its opening quote, then its close.
		q := i
		for q < len(raw) && raw[q] != '"' && raw[q] != '\'' && raw[q] != '`' {
			q++
		}
		if q >= len(raw) {
			break
		}
		end := findUnescaped(raw[q+1:], string(raw[q]))
		if end < 0 {
			break
		}
		if s != "" {
			out = append(out, s)
		}
		i = q + 1 + end + 1
	}
	return out
}

// pyApplyAll makes __all__ authoritative for exportedness.
//
// A module that declares __all__ is stating its public surface explicitly, which
// is better evidence than the underscore convention — it is common for a package
// __init__ to re-export names that look private, and equally common for a module
// to define public-looking helpers it does not intend to export.
func pyApplyAll(syms []Symbol, all []string) []Symbol {
	inAll := make(map[string]bool, len(all))
	for _, n := range all {
		inAll[n] = true
	}
	for i := range syms {
		if syms[i].Kind == SymMethod {
			// __all__ lists module-level names; a method's visibility still follows
			// from its own name and its class.
			continue
		}
		syms[i].Exported = inAll[syms[i].Name]
	}
	return syms
}

// pyIsMainGuard recognises the script entrypoint.
func pyIsMainGuard(code string) bool {
	if !strings.HasPrefix(code, "if ") {
		return false
	}
	// The scanner has blanked the string body, so the comparison is against the
	// surviving structure: `if __name__ == "":`. Matching on the blanked form is
	// deliberate — it means a line mentioning __name__ inside a string cannot
	// match.
	return strings.Contains(code, "__name__") && strings.Contains(code, "==")
}

// pyIsPublic applies the leading-underscore convention. A dunder is special
// method machinery, not public surface.
func pyIsPublic(name string) bool {
	return name != "" && !strings.HasPrefix(name, "_")
}

// identAfter returns the identifier immediately following prefix in code.
func identAfter(code, prefix string) string {
	if !strings.HasPrefix(code, prefix) {
		return ""
	}
	rest := strings.TrimLeft(code[len(prefix):], " \t")
	end := 0
	for end < len(rest) && identChar(rest[end]) {
		end++
	}
	name := rest[:end]
	if !isPyIdent(name) {
		return ""
	}
	return name
}

// isPyIdent reports whether s is a plain identifier.
func isPyIdent(s string) bool {
	if s == "" {
		return false
	}
	if s[0] >= '0' && s[0] <= '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !identChar(s[i]) {
			return false
		}
	}
	return true
}

// splitTopLevel splits on sep, ignoring separators inside brackets. `Dict[str,
// int]` in an import list is one item, not two.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}
