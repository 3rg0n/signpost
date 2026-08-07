package extract

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// PHPExtractor reads PHP namespaces, use statements, and class surface.
//
// Line-oriented, not a parser (design §4.1). PHP is brace-scoped and class-based, so
// it reuses the scope machinery the JVM extractors established — jvmAdvance,
// jvmDirectMember, jvmDeclSite — and the four things it does differently are what this
// file is about:
//
//   - A file's namespace is a declaration, as on the JVM: `namespace App\Domain;`. The
//     separator is a backslash rather than a dot, which matters because a backslash is
//     also PHP's string escape — so a namespace read out of a double-quoted string
//     would be mangled, and every name here is read from the code rather than a literal.
//   - `use` is not an import in the JVM sense. It aliases a name into the current file's
//     scope, and the name it aliases is resolved by the autoloader against composer.json's
//     PSR-4 map. So the dependency it records is the namespace, and resolution matches on
//     the PSR-4 prefix — the same shape addDeclaredPackage established for the JVM, and the
//     case ADR 0017 describes as a resolution root drawn from the source itself.
//   - Visibility is public by *default*, which inverts Java's rule. A method with no
//     modifier is public, so the test is for the modifiers that take public away. This is
//     the same relationship Kotlin has to Java, and the reason those are separate
//     functions rather than one with a flag.
//   - Code exists only between `<?php` and `?>`. The scanner starts outside, so a
//     template's markup is not read as source (see scanPHP).
type PHPExtractor struct{}

// Langs implements Extractor.
func (PHPExtractor) Langs() []discover.Lang { return []discover.Lang{discover.LangPHP} }

// Extract implements Extractor.
func (PHPExtractor) Extract(f discover.File) (Facts, error) {
	facts := Facts{Path: f.Path, Lang: discover.LangPHP}
	lines := scanLines(f.Content, scanPHP)

	var types []jvmScope
	depth := 0

	for i := 0; i < len(lines); i++ {
		cl := lines[i]
		code := strings.TrimSpace(cl.Text)
		if code == "" {
			depth, types = jvmAdvance(depth, types, cl.Text)
			continue
		}
		// A PHP 8 attribute is metadata on its own line above the declaration, the same
		// position a Java annotation occupies. The scanner keeps `#[` out of the comment
		// rule so the line survives; here it is skipped as a non-declaration.
		if strings.HasPrefix(code, "#[") {
			depth, types = jvmAdvance(depth, types, cl.Text)
			continue
		}

		switch {
		case strings.HasPrefix(code, "namespace "), strings.HasPrefix(code, "namespace\\"):
			facts.Package = phpNamespace(code)
			depth, types = jvmAdvance(depth, types, cl.Text)

		case strings.HasPrefix(code, "use "):
			// A `use` inside a class body is a trait import, not a namespace import, and
			// the two mean opposite things: one is a dependency on another file, the other
			// composes a mixin into this class. Only the file-level form is an import.
			if len(types) == 0 {
				facts.Imports = append(facts.Imports, phpUse(code, cl.Num)...)
			}
			depth, types = jvmAdvance(depth, types, cl.Text)

		case strings.HasPrefix(code, "require"), strings.HasPrefix(code, "include"):
			// The pre-autoloader form, still ubiquitous in scripts and in a project's
			// entrypoint: `require __DIR__ . '/vendor/autoload.php'`. A real dependency on
			// a real file, so it is read as one.
			if im, ok := phpRequire(cl); ok {
				facts.Imports = append(facts.Imports, im)
			}
			depth, types = jvmAdvance(depth, types, cl.Text)

		default:
			joined, last := joinParens(lines, i)
			jcode := strings.TrimSpace(joined.Text)
			tail := joinedTail(lines, i, last)

			if kw, name, ok := phpTypeDecl(jcode); ok && jvmDeclSite(types, depth) {
				sym := Symbol{
					Name: name, Kind: phpTypeKind(kw),
					// A PHP type is always reachable from outside its file: there is no
					// file-private class. What decides whether a caller can name it is the
					// autoloader, not a modifier.
					Exported: true,
					Doc:      jvmDoc(lines, i), Line: cl.Num,
				}
				facts.Symbols = append(facts.Symbols, sym)
				if !jvmOpensBody(lines, tail, last) {
					depth, types = jvmAdvance(depth, types, tail)
					i = last
					continue
				}
				types = append(types, jvmScope{
					name: name, depth: depth, exported: true,
					opened: strings.Contains(tail, "{"),
					// An interface's members are public by definition and carry no modifier
					// saying so. So are a class's, in PHP — membersPublic is set for every
					// type, which is what inverts Java's default.
					membersPublic: true,
				})
			} else if name, ok := phpFuncDecl(jcode); ok {
				switch {
				case jvmDirectMember(types, depth):
					owner := types[len(types)-1]
					facts.Symbols = append(facts.Symbols, Symbol{
						Name: name, Kind: SymMethod, Recv: owner.name,
						Exported: phpMemberIsPublic(jcode),
						Doc:      jvmDoc(lines, i), Line: cl.Num,
					})
				case len(types) == 0 && depth == 0:
					// A free function. Unlike the JVM, PHP has them, and a procedural
					// codebase is nothing but.
					facts.Symbols = append(facts.Symbols, Symbol{
						Name: name, Kind: SymFunc, Exported: true,
						Doc: jvmDoc(lines, i), Line: cl.Num,
					})
				}
			} else if name, ok := phpConstDecl(jcode); ok && jvmDeclSite(types, depth) {
				sym := Symbol{Name: name, Kind: SymConst, Exported: true, Line: cl.Num}
				if jvmDirectMember(types, depth) {
					sym.Recv = types[len(types)-1].name
					sym.Exported = phpMemberIsPublic(jcode)
				}
				facts.Symbols = append(facts.Symbols, sym)
			}

			depth, types = jvmAdvance(depth, types, tail)
			i = last
		}
	}
	return facts, nil
}

// phpNamespace reads a namespace declaration.
//
// `namespace App\Domain;` and the braced form `namespace App\Domain {` both declare
// the same name. The braced form's opening brace is left for jvmAdvance to count,
// which puts every declaration in the file one depth deeper — correct, since that is
// exactly what the braces do.
func phpNamespace(code string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(code, "namespace"))
	rest = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), ";"))
	if i := strings.IndexAny(rest, " \t{;"); i >= 0 {
		rest = rest[:i]
	}
	return strings.Trim(rest, `\`)
}

// phpUse reads a use statement.
//
// Four forms, and the group form is the only one that yields several imports:
//
//	use App\Domain\Order;                    one class
//	use App\Domain\Order as PurchaseOrder;   aliased
//	use function App\Support\slugify;        a function, not a class
//	use App\Domain\{Order, Invoice};         a group
//
// What is recorded is the *namespace*, not the class, for the same reason javaImport
// records the package: the namespace is what a PSR-4 map declares and therefore the
// only part resolution can match. Recording `App\Domain\Order` as the dependency would
// point at a node no autoloader entry will ever claim, and would split two imports
// from one namespace into two dependencies.
//
// A `use` of a trait inside a class body never reaches here — the caller checks that,
// because the same keyword composing a mixin is not a file dependency.
func phpUse(code string, line int) []Import {
	rest := strings.TrimSpace(strings.TrimPrefix(code, "use "))
	rest = strings.TrimSpace(strings.TrimSuffix(rest, ";"))
	// `use function` and `use const` import a single symbol rather than a type. The
	// namespace is still what resolution matches, so the keyword only needs stripping.
	for _, kw := range []string{"function ", "const "} {
		if strings.HasPrefix(rest, kw) {
			rest = strings.TrimSpace(rest[len(kw):])
			break
		}
	}
	if rest == "" {
		return nil
	}

	// The group form: a shared prefix and a braced list.
	if open := strings.IndexByte(rest, '{'); open >= 0 {
		prefix := strings.Trim(strings.TrimSpace(rest[:open]), `\`)
		body := rest[open+1:]
		if end := strings.IndexByte(body, '}'); end >= 0 {
			body = body[:end]
		}
		var out []Import
		for _, part := range strings.Split(body, ",") {
			name, alias := phpSplitAs(strings.TrimSpace(part))
			if name == "" {
				continue
			}
			ns, sym := phpSplitNamespace(prefix + `\` + name)
			if ns == "" {
				continue
			}
			im := Import{Raw: ns, Line: line, Alias: alias}
			if sym != "" {
				im.Names = []string{sym}
			}
			out = append(out, im)
		}
		return out
	}

	name, alias := phpSplitAs(rest)
	ns, sym := phpSplitNamespace(name)
	if ns == "" {
		// A single-segment `use Throwable;` names a global-namespace class and declares
		// no dependency on any namespace. Recording the class as a namespace would
		// invent a node.
		return nil
	}
	im := Import{Raw: ns, Line: line, Alias: alias}
	if sym != "" {
		im.Names = []string{sym}
	}
	return []Import{im}
}

// phpSplitAs splits "Name as Alias", which PHP spells case-insensitively.
func phpSplitAs(s string) (name, alias string) {
	lower := strings.ToLower(s)
	if i := strings.Index(lower, " as "); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(" as "):])
	}
	return strings.TrimSpace(s), ""
}

// phpSplitNamespace splits a qualified name into its namespace and final segment.
func phpSplitNamespace(name string) (ns, sym string) {
	name = strings.Trim(strings.TrimSpace(name), `\`)
	i := strings.LastIndexByte(name, '\\')
	if i < 0 {
		return "", name
	}
	return name[:i], name[i+1:]
}

// phpRequire reads the path out of a require or include.
//
// Read from Raw, because the scanner blanked the string body. A computed path —
// `require __DIR__ . '/../config.php'` — yields the literal part, which is the piece
// that identifies the file; the `__DIR__` prefix says it is relative, which is what the
// leading "./" the resolver reads records.
func phpRequire(cl codeLine) (Import, bool) {
	code := strings.TrimSpace(cl.Text)
	path, ok := stringAt(cl.Raw, 0)
	if !ok || path == "" {
		return Import{}, false
	}
	// `__DIR__ . '/x.php'` and `'./x.php'` are both relative to this file. The literal
	// begins with a slash in the first form, which alone would read as absolute.
	if strings.Contains(code, "__DIR__") && strings.HasPrefix(path, "/") {
		path = "." + path
	}
	return Import{Raw: path, Line: cl.Num}, true
}

// phpTypeDecl reports the keyword and name of a type declaration.
//
// PHP's four are class, interface, trait and enum. `abstract` and `final` may precede
// the keyword, and `readonly` may on a class since 8.2.
func phpTypeDecl(code string) (kw, name string, ok bool) {
	words := jvmWords(code)
	for i, w := range words {
		if !phpIsTypeKeyword(w) {
			if phpIsModifier(w) {
				continue
			}
			return "", "", false
		}
		if n := phpIdentAt(words, i+1); n != "" {
			return w, n, true
		}
		// An anonymous class — `new class implements X {` — declares no name, and
		// recording one would invent a symbol. The `new` before it is also what
		// phpFuncDecl's rejection rules catch.
		return "", "", false
	}
	return "", "", false
}

// phpIsTypeKeyword reports whether a token introduces a named type.
func phpIsTypeKeyword(w string) bool {
	switch w {
	case "class", "interface", "trait", "enum":
		return true
	}
	return false
}

// phpTypeKind maps a PHP type keyword onto a SymbolKind.
func phpTypeKind(kw string) SymbolKind {
	if kw == "interface" {
		return SymInterface
	}
	// A trait is a set of methods composed into a class, and an enum in PHP 8 may hold
	// methods too — both are a named type holding members, which is what SymClass means
	// here.
	return SymClass
}

// phpIsModifier reports whether a token is a declaration modifier.
func phpIsModifier(w string) bool {
	switch w {
	case "public", "protected", "private", "static", "final", "abstract", "readonly",
		"var":
		return true
	}
	return false
}

// phpIdentAt returns words[i] when it is a plain identifier that could be a declared
// name.
func phpIdentAt(words []string, i int) string {
	if i >= len(words) {
		return ""
	}
	w := words[i]
	if w == "" || (w[0] >= '0' && w[0] <= '9') {
		return ""
	}
	for j := 0; j < len(w); j++ {
		if !identChar(w[j]) {
			return ""
		}
	}
	if phpIsTypeKeyword(w) || phpIsModifier(w) || w == "function" || w == "const" {
		return ""
	}
	return w
}

// phpFuncDecl reports the name of a function or method declared on a line.
//
// Far simpler than javaMethodDecl, and for a good reason: PHP requires the `function`
// keyword. Java's method declaration has no keyword at all, which is why that function
// needs three rejection rules to tell `void compute(int a)` from `compute(a);`. Here
// the keyword is the signal, and the only things to reject are the forms that use it
// without declaring a name.
func phpFuncDecl(code string) (string, bool) {
	words := jvmWords(code)
	for i, w := range words {
		if w != "function" {
			if phpIsModifier(w) {
				continue
			}
			return "", false
		}
		name := phpIdentAt(words, i+1)
		if name == "" {
			// A closure — `$fn = function ($x) {` — or an arrow function. Neither declares
			// a named function, and the `$fn =` before it is not a declaration either.
			return "", false
		}
		return name, true
	}
	return "", false
}

// phpConstDecl reports the name of a const declaration.
//
// Both the class constant `const MAX = 10;` and the file-level form. `define('MAX',
// 10)` is deliberately not read: its name is a string argument, so a caller cannot
// see it as a declaration, and reading one would mean trusting a literal that may be
// computed.
func phpConstDecl(code string) (string, bool) {
	words := jvmWords(code)
	for i, w := range words {
		if w != "const" {
			if phpIsModifier(w) {
				continue
			}
			return "", false
		}
		if name := phpIdentAt(words, i+1); name != "" {
			return name, true
		}
		return "", false
	}
	return "", false
}

// phpMemberIsPublic reports whether a member declaration is public surface.
//
// Public is the default: a method with no modifier is callable from anywhere. So the
// test is for what takes public away, which is the inverse of javaMemberIsPublic and
// the same shape as kotlinIsPublic.
func phpMemberIsPublic(code string) bool {
	for _, w := range jvmWords(code) {
		switch w {
		case "private", "protected":
			return false
		}
	}
	return true
}
