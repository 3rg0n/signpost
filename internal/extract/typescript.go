package extract

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// TSExtractor reads TypeScript and JavaScript imports and top-level declarations.
//
// One extractor for both languages because their module and declaration syntax is
// the same; TypeScript adds `interface`, `type`, `enum`, and `declare`, which
// simply do not appear in a .js file. Splitting them would duplicate every
// import form to gain nothing.
//
// Module scope is decided by brace depth rather than indentation, unlike Python.
// JavaScript's indentation carries no meaning, and a file run through a formatter
// with a different style would otherwise extract a different set of symbols —
// which would break the determinism the committed bundle depends on.
//
// Known limitation: a regular-expression literal containing an unbalanced brace
// (`/\{/`) skews the depth counter. Distinguishing a regex literal from division
// requires knowing whether the previous token was a value, which is parser
// territory. The failure is contained — it affects which symbols in one file are
// judged top-level, not the run — and the alternative is the tree-sitter fallback
// documented in design §4.1.
type TSExtractor struct{}

// Langs implements Extractor.
func (TSExtractor) Langs() []discover.Lang {
	return []discover.Lang{discover.LangTS, discover.LangJS}
}

// Extract implements Extractor.
func (TSExtractor) Extract(f discover.File) (Facts, error) {
	facts := Facts{Path: f.Path, Lang: f.Lang}
	lines := scanLines(f.Content, scanJSLike)

	// A shebang makes a file directly executable, which is the one entrypoint
	// signal that exists inside a JS/TS source file. The rest — `bin`, `main`,
	// framework conventions — live in the manifest, and the manifest extractor
	// reports them.
	if strings.HasPrefix(f.Content, "#!") && strings.Contains(firstLine(f.Content), "node") {
		facts.Entrypoints = append(facts.Entrypoints, "#!")
	}

	depth := 0
	for i := 0; i < len(lines); i++ {
		cl := lines[i]
		code := strings.TrimSpace(cl.Text)
		if code == "" {
			continue
		}
		// Depth is evaluated before this line's own braces are counted, so a
		// declaration whose body opens on the same line is still module-level.
		topLevel := depth == 0

		switch {
		case strings.HasPrefix(code, "import"):
			joined, last := joinBraces(lines, i)
			if im, ok := tsImport(joined); ok {
				facts.Imports = append(facts.Imports, im)
			}
			depth += netDepth(joinedTail(lines, i, last), "([{", ")]}")
			i = last
			continue

		case strings.HasPrefix(code, "export"):
			joined, last := joinBraces(lines, i)
			// A re-export carries a module path and is a dependency; an export of
			// local names is not.
			if im, ok := tsExportFrom(joined); ok {
				facts.Imports = append(facts.Imports, im)
			} else if names := tsExportList(joined); len(names) > 0 {
				facts.Symbols = append(facts.Symbols, tsExportedNames(names, cl.Num)...)
			} else if s, ok := tsDeclaration(joined.Text, cl, lines, i, true); ok && topLevel {
				facts.Symbols = append(facts.Symbols, s...)
			}
			// An `export { ... }` list balances on its own line, so counting the
			// joined region keeps the depth honest either way.
			depth += netDepth(joinedTail(lines, i, last), "([{", ")]}")
			i = last
			continue
		}

		if topLevel {
			if im, ok := tsRequire(cl); ok {
				facts.Imports = append(facts.Imports, im)
			}
			if s, ok := tsDeclaration(code, cl, lines, i, false); ok {
				facts.Symbols = append(facts.Symbols, s...)
			}
		} else if im, ok := tsRequire(cl); ok {
			// A require or dynamic import inside a function is a lazy dependency,
			// which is real coupling however deeply it is nested.
			facts.Imports = append(facts.Imports, im)
		}
		depth += netDepth(cl.Text, "([{", ")]}")
		if depth < 0 {
			depth = 0
		}
	}
	return facts, nil
}

// joinedTail returns the concatenated text of lines i through last, so a joined
// statement's braces are counted exactly once.
func joinedTail(lines []codeLine, i, last int) string {
	var b strings.Builder
	for ; i <= last && i < len(lines); i++ {
		b.WriteString(lines[i].Text)
	}
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// tsImport parses an ES module import.
//
// Every form carries the module path in a trailing string literal except the
// side-effect form, which is nothing but the literal:
//
//	import "./polyfill"                  side effect only
//	import x from "m"                    default binding
//	import * as ns from "m"              namespace binding
//	import { a, b as c } from "m"        named bindings
//	import x, { a } from "m"             both
//	import type { T } from "m"           type-only, still a real dependency
func tsImport(cl codeLine) (Import, bool) {
	code := strings.TrimSpace(cl.Text)
	// `import(` is a dynamic import expression, not a statement; tsRequire handles
	// it, and matching it here would treat an await expression as a declaration.
	if strings.HasPrefix(code, "import(") || strings.HasPrefix(code, "import (") {
		return Import{}, false
	}
	body := strings.TrimSpace(strings.TrimPrefix(code, "import"))
	if body == "" {
		return Import{}, false
	}

	im := Import{Line: cl.Num}
	// Locate the module path: the last string literal on the statement. Using the
	// last one rather than the first matters for `import x from "m"` where the
	// scanner has left both quotes in place.
	q := strings.LastIndexAny(body, `"'`+"`")
	if q < 0 {
		return Import{}, false
	}
	open := lastQuoteOpen(body, q)
	if open < 0 {
		return Import{}, false
	}
	path, ok := stringAt(cl.Raw, rawOffset(cl, open))
	if !ok || path == "" {
		return Import{}, false
	}
	im.Raw = path

	clause := strings.TrimSpace(body[:open])
	// A type-only import is still a build-time dependency and still tells an agent
	// where a type comes from, so it is kept — with the marker removed so the
	// clause parses like any other.
	clause = strings.TrimPrefix(clause, "type ")
	clause = strings.TrimSuffix(strings.TrimSpace(clause), "from")
	clause = strings.TrimSpace(clause)
	if clause == "" {
		// Side-effect import: no bindings, but a genuine dependency — often the
		// only thing that pulls in a polyfill or a CSS module.
		return im, true
	}
	im.Names, im.Alias = tsBindings(clause)
	return im, true
}

// tsBindings parses an import clause into named bindings and a single alias.
func tsBindings(clause string) (names []string, alias string) {
	// The named-binding braces are handled separately from the default and
	// namespace bindings outside them.
	outside := clause
	if open := strings.IndexByte(clause, '{'); open >= 0 {
		close := strings.LastIndexByte(clause, '}')
		if close > open {
			for _, part := range splitTopLevel(clause[open+1:close], ',') {
				n, a := splitTSAs(part)
				if n == "" {
					continue
				}
				names = append(names, n)
				if a != "" {
					alias = a
				}
			}
			outside = strings.TrimSuffix(strings.TrimSpace(clause[:open]), ",")
		}
	}
	for _, part := range splitTopLevel(outside, ',') {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
		case strings.HasPrefix(part, "*"):
			// `* as ns` binds the whole module under one name.
			if _, a := splitTSAs(part); a != "" {
				alias = a
			}
		default:
			// The default binding. Recorded as the alias because that is the local
			// name the code uses, and there is no source-side name to record: a
			// default export has no identifier at the exporting end.
			if isTSIdent(part) {
				alias = part
			}
		}
	}
	return names, alias
}

// tsExportFrom parses a re-export, which is an import with a different keyword:
//
//	export { a, b } from "m"
//	export * from "m"
//	export * as ns from "m"
//
// These matter more than they look: a barrel file made of nothing but re-exports
// is how most TypeScript packages define their public surface, and missing them
// leaves the module graph disconnected exactly at the package boundary.
func tsExportFrom(cl codeLine) (Import, bool) {
	code := strings.TrimSpace(cl.Text)
	if !strings.Contains(code, " from ") {
		return Import{}, false
	}
	body := strings.TrimSpace(strings.TrimPrefix(code, "export"))
	body = strings.TrimPrefix(body, " type ")
	q := strings.LastIndexAny(body, `"'`+"`")
	if q < 0 {
		return Import{}, false
	}
	open := lastQuoteOpen(body, q)
	if open < 0 {
		return Import{}, false
	}
	// Everything before the path must be an export clause, not a declaration with
	// a string default value: `export const x = "y"` contains no " from ".
	clause := strings.TrimSpace(body[:open])
	if !strings.HasSuffix(clause, "from") {
		return Import{}, false
	}
	path, ok := stringAt(cl.Raw, rawOffset(cl, open))
	if !ok || path == "" {
		return Import{}, false
	}
	im := Import{Raw: path, Line: cl.Num}
	im.Names, _ = tsBindings(strings.TrimSpace(strings.TrimSuffix(clause, "from")))
	return im, true
}

// tsExportList parses `export { a, b as c }` — an export of names declared
// elsewhere in this same file.
func tsExportList(cl codeLine) []string {
	code := strings.TrimSpace(cl.Text)
	if strings.Contains(code, " from ") {
		return nil
	}
	open := strings.IndexByte(code, '{')
	close := strings.LastIndexByte(code, '}')
	if open < 0 || close < open {
		return nil
	}
	// Only a bare export list, not a declaration whose body happens to contain
	// braces: `export function f() { ... }` must not be read as a name list.
	if head := strings.TrimSpace(code[len("export"):open]); head != "" && head != "type" {
		return nil
	}
	var out []string
	for _, part := range splitTopLevel(code[open+1:close], ',') {
		// The exported name is the alias when one is given: `export { internal as
		// public }` exposes `public`, and that is what a caller imports.
		n, a := splitTSAs(part)
		if a != "" {
			n = a
		}
		if isTSIdent(n) {
			out = append(out, n)
		}
	}
	return out
}

// tsExportedNames turns an export list into symbols.
//
// These have no Kind: the declaration they refer to is elsewhere in the file and
// may not have been seen yet. Normalize merges them with the declaration when it
// is found, and marking the declaration exported is what tsMarkExported does.
func tsExportedNames(names []string, line int) []Symbol {
	out := make([]Symbol, 0, len(names))
	for _, n := range names {
		out = append(out, Symbol{Name: n, Exported: true, Line: line})
	}
	return out
}

// tsDeclaration parses a top-level declaration.
func tsDeclaration(code string, cl codeLine, lines []codeLine, idx int, exported bool) ([]Symbol, bool) {
	code = strings.TrimSpace(code)
	isDefault := false
	if exported {
		code = strings.TrimSpace(strings.TrimPrefix(code, "export"))
		// `export default` need not name anything. When a named declaration
		// follows, that name is used; otherwise the export is recorded as
		// "default", which is the name a caller imports it under.
		if rest := strings.TrimPrefix(code, "default"); rest != code {
			code = strings.TrimSpace(rest)
			isDefault = true
			if code == "" || !startsWithDeclKeyword(code) {
				return []Symbol{{Name: "default", Exported: true, Line: cl.Num}}, true
			}
		}
	}
	// Modifiers that precede the keyword without changing what is declared.
	for _, mod := range []string{"declare ", "abstract ", "async ", "readonly "} {
		code = strings.TrimSpace(strings.TrimPrefix(code, mod))
	}

	kw, name := declKeywordAndName(code)
	if name == "" {
		// `export default function () {}` has a keyword but no name. It is still
		// the module's default export and callers still import it, so it must not
		// be dropped for lack of an identifier.
		if isDefault {
			k := SymFunc
			if kw == "class" {
				k = SymClass
			}
			return []Symbol{{Name: "default", Kind: k, Exported: true, Line: cl.Num}}, true
		}
		return nil, false
	}
	kind := SymVar
	switch kw {
	case "function", "function*":
		kind = SymFunc
	case "class":
		kind = SymClass
	case "interface":
		kind = SymInterface
	case "type", "enum", "namespace", "module":
		kind = SymType
	case "const":
		kind = SymConst
		// `const f = () => {}` and `const f = function () {}` declare a function.
		// Reporting them as variables would make most modern TypeScript look like
		// it has no functions at all.
		if tsIsFunctionValue(code) {
			kind = SymFunc
		}
	case "let", "var":
		if tsIsFunctionValue(code) {
			kind = SymFunc
		}
	default:
		return nil, false
	}

	return []Symbol{{
		Name:     name,
		Kind:     kind,
		Exported: exported,
		Doc:      tsJSDoc(lines, idx),
		Line:     cl.Num,
	}}, true
}

// declKeywordAndName splits a declaration into its keyword and declared name.
func declKeywordAndName(code string) (kw, name string) {
	fields := strings.FieldsFunc(code, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '(' || r == '<' || r == '=' || r == ':' || r == ';'
	})
	if len(fields) == 0 {
		return "", ""
	}
	kw = fields[0]
	// `function*` is a generator; the star is part of the keyword, not the name.
	if kw == "function" && len(fields) > 1 && strings.HasPrefix(fields[1], "*") {
		fields[1] = strings.TrimPrefix(fields[1], "*")
		if fields[1] == "" && len(fields) > 2 {
			fields = append(fields[:1], fields[2:]...)
		}
	}
	if len(fields) < 2 {
		return kw, ""
	}
	name = strings.TrimPrefix(fields[1], "*")
	if !isTSIdent(name) {
		return kw, ""
	}
	return kw, name
}

func startsWithDeclKeyword(code string) bool {
	for _, kw := range []string{
		"function", "class", "interface", "type", "enum", "const", "let", "var",
		"abstract", "async", "namespace", "declare",
	} {
		if code == kw || strings.HasPrefix(code, kw+" ") || strings.HasPrefix(code, kw+"*") {
			return true
		}
	}
	return false
}

// tsIsFunctionValue reports whether a const/let/var is bound to a function.
func tsIsFunctionValue(code string) bool {
	eq := strings.IndexByte(code, '=')
	if eq < 0 {
		return false
	}
	rhs := strings.TrimSpace(code[eq+1:])
	rhs = strings.TrimPrefix(rhs, "async ")
	if strings.HasPrefix(rhs, "function") {
		return true
	}
	// An arrow function: the => must be at the top level of the initialiser, not
	// inside a nested object or call, or `const config = { on: () => {} }` would
	// count as a function declaration.
	depth := 0
	for i := 0; i < len(rhs); i++ {
		switch rhs[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '=':
			if depth <= 0 && i+1 < len(rhs) && rhs[i+1] == '>' {
				return true
			}
		}
	}
	return false
}

// tsRequire parses CommonJS require and dynamic import.
//
//	const x = require("m")
//	require("./side-effect")
//	const { a } = require("m")
//	await import("m")
//
// CommonJS is not legacy in practice: build tooling, config files, and a great
// deal of Node code still use it, and a tool that only reads ES modules reports an
// empty dependency graph for those files.
func tsRequire(cl codeLine) (Import, bool) {
	code := cl.Text
	for _, kw := range []string{"require(", "import("} {
		idx := strings.Index(code, kw)
		if idx < 0 {
			continue
		}
		// The call must not be part of a longer identifier: `myRequire(` is not
		// require.
		if idx > 0 && identChar(code[idx-1]) {
			continue
		}
		path, ok := stringAt(cl.Raw, rawOffset(cl, idx+len(kw)))
		if !ok || path == "" {
			continue
		}
		// Only a literal argument. `require(name)` computes its path at runtime,
		// and recording the variable name as a module would be a fabricated edge.
		between := code[idx+len(kw):]
		if q := strings.IndexAny(between, `"'`+"`"); q < 0 || strings.TrimSpace(between[:q]) != "" {
			continue
		}
		// An interpolated template is computed at runtime just as a variable is.
		if strings.Contains(path, "${") {
			continue
		}
		im := Import{Raw: path, Line: cl.Num}
		if names := tsDestructured(code[:idx]); len(names) > 0 {
			im.Names = names
		} else if alias := tsAssignedName(code[:idx]); alias != "" {
			im.Alias = alias
		}
		return im, true
	}
	return Import{}, false
}

// tsDestructured reads the names out of `const { a, b } = ` preceding a require.
func tsDestructured(lhs string) []string {
	open := strings.IndexByte(lhs, '{')
	close := strings.LastIndexByte(lhs, '}')
	if open < 0 || close < open {
		return nil
	}
	var out []string
	for _, part := range splitTopLevel(lhs[open+1:close], ',') {
		if n, _ := splitTSAs(part); isTSIdent(n) {
			out = append(out, n)
		}
	}
	return out
}

// tsAssignedName reads the binding from `const x = ` preceding a require.
func tsAssignedName(lhs string) string {
	eq := strings.LastIndexByte(lhs, '=')
	if eq < 0 {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(lhs[:eq]))
	if len(fields) == 0 {
		return ""
	}
	name := fields[len(fields)-1]
	if !isTSIdent(name) {
		return ""
	}
	return name
}

// tsJSDoc reads the JSDoc block immediately above a declaration.
//
// Read from Raw because the scanner strips comments — which is exactly what makes
// a declaration inside a comment invisible, and therefore what makes the comment's
// prose only recoverable from the original bytes.
func tsJSDoc(lines []codeLine, idx int) string {
	i := idx - 1
	for i >= 0 && strings.TrimSpace(lines[i].Raw) == "" {
		i--
	}
	if i < 0 || !strings.HasSuffix(strings.TrimSpace(lines[i].Raw), "*/") {
		return ""
	}
	end := i
	for i >= 0 && !strings.HasPrefix(strings.TrimSpace(lines[i].Raw), "/*") {
		i--
	}
	if i < 0 {
		return ""
	}
	// Only a doc comment. A plain /* */ block above a declaration is usually a
	// disabled block of code or a banner, not documentation.
	if !strings.HasPrefix(strings.TrimSpace(lines[i].Raw), "/**") {
		return ""
	}
	var b strings.Builder
	for j := i; j <= end; j++ {
		l := strings.TrimSpace(lines[j].Raw)
		l = strings.TrimPrefix(l, "/**")
		l = strings.TrimSuffix(l, "*/")
		l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "*"))
		// A JSDoc tag is structured metadata, not the summary sentence.
		if strings.HasPrefix(l, "@") {
			break
		}
		b.WriteString(l + " ")
	}
	return FirstSentence(b.String())
}

// splitTSAs splits "name as alias", the ES module rename form.
func splitTSAs(s string) (name, alias string) {
	s = strings.TrimSpace(s)
	// A default-export rename is written `default as x`; the exported name is x.
	if i := strings.Index(s, " as "); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(" as "):])
	}
	// A type-only member inside a named list: `import { type T }`.
	s = strings.TrimPrefix(s, "type ")
	return strings.TrimSpace(s), ""
}

// isTSIdent reports whether s is a plain identifier. `$` is legal in JavaScript
// identifiers and common in real code.
func isTSIdent(s string) bool {
	if s == "" {
		return false
	}
	if s[0] >= '0' && s[0] <= '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !identChar(s[i]) && s[i] != '$' {
			return false
		}
	}
	return true
}

// rawOffset maps an offset in a joined line's Text back into its Raw. They are
// the same length for an unjoined line, and joining applies the identical
// transformation to both, so the offsets stay aligned.
func rawOffset(cl codeLine, off int) int {
	if off > len(cl.Raw) {
		return len(cl.Raw)
	}
	return off
}

// lastQuoteOpen returns the index of the quote that opens the literal ending at
// or containing index q.
func lastQuoteOpen(s string, q int) int {
	// The scanner blanked the body, so the literal is a matched pair of identical
	// quote characters with only spaces between them.
	c := s[q]
	for i := q - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
		if s[i] != ' ' {
			break
		}
	}
	// q is itself the opening quote.
	return q
}
