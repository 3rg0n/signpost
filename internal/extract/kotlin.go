package extract

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// KotlinExtractor reads Kotlin package declarations, imports and declared surface.
//
// Kotlin shares Java's file layout — a package declaration, then imports, then
// declarations — and the two extractors share the tokeniser and the scope stack. It
// differs in three ways that are each a correctness rule rather than a matter of
// syntax:
//
//   - **Public is the default.** A declaration with no visibility modifier is public.
//     Java's is package-private. Applying Java's rule to Kotlin reports a file's
//     entire surface as private, and applying Kotlin's rule to Java reports every
//     field and helper as public API; there is no shared default that is right for
//     both, which is why javaTypeIsPublic and kotlinIsPublic are separate functions
//     rather than one with a flag.
//   - **A file need not declare a type.** Top-level `fun` and `val` are ordinary
//     Kotlin, so a symbol with no owner is normal here where in Java it is
//     impossible. The scope stack being empty is a valid state, not a parse failure.
//   - **An import may name a function.** `import kotlin.math.max` imports a top-level
//     function, and `import kotlinx.coroutines.flow.map` an extension. So the
//     capitalisation convention that splits a Java import into package and class is
//     weaker here, and a lowercase last segment is as likely to be a member as a
//     package. The split treats it the same way regardless — the package is what
//     another file declares, and that is the part resolution can match.
//
// `object` is recorded as a type because it declares a name that holds members. A
// `companion object` declares no name of its own, so its members are attributed to
// the enclosing type, which is where a caller reaches them.
type KotlinExtractor struct{}

// Langs implements Extractor.
func (KotlinExtractor) Langs() []discover.Lang { return []discover.Lang{discover.LangKotlin} }

// Extract implements Extractor.
func (KotlinExtractor) Extract(f discover.File) (Facts, error) {
	facts := Facts{Path: f.Path, Lang: discover.LangKotlin}
	lines := scanLines(f.Content, scanKotlin)

	var types []jvmScope
	depth := 0

	for i := 0; i < len(lines); i++ {
		cl := lines[i]
		code := strings.TrimSpace(cl.Text)
		if code == "" {
			depth, types = jvmAdvance(depth, types, cl.Text)
			continue
		}
		// An annotation, or a file-level `@file:JvmName`, is metadata.
		if strings.HasPrefix(code, "@") {
			depth, types = jvmAdvance(depth, types, cl.Text)
			continue
		}

		switch {
		case strings.HasPrefix(code, "package "):
			facts.Package = strings.TrimSpace(strings.TrimSuffix(
				strings.TrimSpace(strings.TrimPrefix(code, "package ")), ";"))
			depth, types = jvmAdvance(depth, types, cl.Text)

		case strings.HasPrefix(code, "import "):
			if im, ok := kotlinImport(code, cl.Num); ok {
				facts.Imports = append(facts.Imports, im)
			}
			depth, types = jvmAdvance(depth, types, cl.Text)

		default:
			joined, last := joinParens(lines, i)
			jcode := strings.TrimSpace(joined.Text)
			tail := joinedTail(lines, i, last)

			switch {
			case kotlinIsCompanion(jcode):
				// `companion object { }` holds what Java would call statics. Its members
				// belong to the enclosing type — that is how they are called — so the
				// scope pushed carries the outer name.
				owner := "companion"
				exported := true
				if len(types) > 0 {
					owner = types[len(types)-1].name
					exported = types[len(types)-1].exported
				}
				types = append(types, jvmScope{
					name: owner, depth: depth, exported: exported,
					opened: strings.Contains(tail, "{"), membersPublic: true,
				})

			default:
				kw, name, isType := jvmTypeDecl(jcode)
				if isType && jvmDeclSite(types, depth) {
					sym := Symbol{
						Name: name, Kind: jvmTypeKind(kw),
						Exported: kotlinIsPublic(jcode, types),
						Doc:      jvmDoc(lines, i), Line: cl.Num,
					}
					facts.Symbols = append(facts.Symbols, sym)
					// A primary constructor declares properties: `class P(val x: Int)`. They
					// are the type's surface, and for a data class they are the whole of it.
					facts.Symbols = append(facts.Symbols,
						kotlinCtorProps(jcode, name, sym.Exported, cl.Num)...)
					// A bodyless declaration — `data class Point(val x: Int)`, `object Empty
					// : Result()` — is far more common in Kotlin than in Java, and a scope
					// pushed for one never closes.
					if jvmOpensBody(lines, tail, last) {
						types = append(types, jvmScope{
							name: name, depth: depth, exported: sym.Exported,
							opened: strings.Contains(tail, "{"),
							// Every Kotlin type's members are public unless they say otherwise,
							// so the scope always carries the flag that Java sets only for an
							// interface.
							membersPublic: true,
						})
					}
					break
				}
				if kw, name, ok := kotlinMemberDecl(jcode); ok {
					sym := Symbol{
						Name: name, Kind: kotlinMemberKind(kw),
						Exported: kotlinIsPublic(jcode, types),
						Doc:      jvmDoc(lines, i), Line: cl.Num,
					}
					// A member declared directly in a type body is that type's; one at the
					// top level of the file has no owner, which is ordinary in Kotlin. One
					// deeper than either is local to a function body and is not surface.
					switch {
					case jvmDirectMember(types, depth):
						// A fun inside a type is a method; a val or var inside one is a
						// property and keeps its own kind.
						if kw == "fun" {
							sym.Kind = SymMethod
						}
						sym.Recv = types[len(types)-1].name
						facts.Symbols = append(facts.Symbols, sym)
					case len(types) == 0 && depth == 0:
						facts.Symbols = append(facts.Symbols, sym)
						// `fun main()` at the top level of a file is where a Kotlin program
						// starts. Unlike Java it takes no required parameter, so the name and
						// the position are the whole signature.
						if kw == "fun" && name == "main" {
							facts.Entrypoints = append(facts.Entrypoints, "main")
						}
					}
				}
			}

			depth, types = jvmAdvance(depth, types, tail)
			i = last
		}
	}
	facts.addQueries(sqlLiterals(lines))
	return facts, nil
}

// kotlinImport reads one Kotlin import.
//
// `import a.b.C` and `import a.b.C as D` and `import a.b.*` are the three forms. The
// alias is recorded because Kotlin's is the only JVM rename, and a file that imports
// two same-named classes uses it.
func kotlinImport(code string, line int) (Import, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(code, "import "))
	rest = strings.TrimSpace(strings.TrimSuffix(rest, ";"))
	alias := ""
	if i := indexWord(rest, "as"); i > 0 {
		alias = strings.TrimSpace(rest[i+len("as"):])
		rest = strings.TrimSpace(rest[:i])
	}
	if rest == "" {
		return Import{}, false
	}
	if strings.HasSuffix(rest, ".*") {
		pkg := strings.TrimSuffix(rest, ".*")
		if pkg == "" {
			return Import{}, false
		}
		return Import{Raw: pkg, Alias: alias, Line: line}, true
	}
	pkg, sym := jvmSplitPackage(rest)
	if pkg == "" {
		return Import{}, false
	}
	im := Import{Raw: pkg, Alias: alias, Line: line}
	if sym != "" {
		im.Names = []string{sym}
	}
	return im, true
}

// kotlinIsCompanion reports whether a line opens a companion object.
func kotlinIsCompanion(code string) bool {
	words := jvmWords(code)
	for i, w := range words {
		if w == "companion" && i+1 < len(words) && words[i+1] == "object" {
			// A named companion — `companion object Factory` — declares a name, and
			// jvmTypeDecl reads it. Only the anonymous form is folded into its owner.
			return jvmIdentAt(words, i+2) == ""
		}
	}
	return false
}

// kotlinMemberDecl reports the keyword and name of a fun, val, var or typealias.
//
// An extension function's receiver is part of its name in the sense that matters
// here: `fun String.shout()` is not a method of anything in this file, and recording
// it as `shout` with no owner is the honest reading — the type it extends is
// somewhere else, and attributing a symbol to a type the repository may not contain
// would put a method on a page for a class that is not there.
func kotlinMemberDecl(code string) (kw, name string, ok bool) {
	words := jvmWords(code)
	for i, w := range words {
		switch w {
		case "fun", "val", "var", "typealias":
			// A generic receiver is checked first: `fun List<Int>.total()` leaves the
			// receiver and the name as two tokens, because jvmWords drops the bracketed
			// run between them. Reading the first of the two as the declared name would
			// record the receiver — a type this file does not own — instead.
			n := kotlinReceiverName(words, i+1)
			if n == "" {
				n = jvmDottedIdentAt(words, i+1)
			}
			if n == "" {
				return "", "", false
			}
			// The declared name is what follows the last dot: `String.shout` declares
			// shout, and String is a type this file does not own.
			if j := strings.LastIndexByte(n, '.'); j >= 0 {
				n = n[j+1:]
			}
			if n == "" {
				return "", "", false
			}
			return w, n, true
		case "constructor", "init":
			// A secondary constructor and an initialiser block declare no name.
			return "", "", false
		}
		if !jvmIsModifier(w) && !strings.HasPrefix(w, "@") {
			return "", "", false
		}
	}
	return "", "", false
}

// kotlinReceiverName reads the declared name out of the token run a generic
// extension receiver leaves behind: `List`, `.total`.
func kotlinReceiverName(words []string, i int) string {
	if i+1 >= len(words) || !strings.HasPrefix(words[i+1], ".") {
		return ""
	}
	rest := []string{strings.TrimPrefix(words[i+1], ".")}
	return jvmDottedIdentAt(rest, 0)
}

// kotlinMemberKind maps a member keyword onto a SymbolKind.
func kotlinMemberKind(kw string) SymbolKind {
	switch kw {
	case "fun":
		return SymFunc
	case "val":
		// A `val` is read-only, which is what a const means in the other extractors'
		// vocabulary. `const val` is the compile-time form and is the same fact.
		return SymConst
	case "var":
		return SymVar
	case "typealias":
		return SymType
	}
	return ""
}

// kotlinIsPublic reports whether a Kotlin declaration is public surface.
//
// Public is the default, so this is a search for the modifiers that take it away.
// `internal` is module-visible and is treated as not public on the same grounds as
// Rust's `pub(crate)`: it is reachable inside the module and is not the module's API.
// A nested declaration is only reachable if everything enclosing it is.
func kotlinIsPublic(code string, enclosing []jvmScope) bool {
	for _, s := range enclosing {
		if !s.exported {
			return false
		}
	}
	for _, w := range jvmWords(code) {
		switch w {
		case "private", "protected", "internal":
			return false
		}
	}
	return true
}

// kotlinCtorProps reads the properties a primary constructor declares.
//
// `class Point(val x: Int, val y: Int)` declares two public properties and no body.
// Reading only the class name from that line would report a type with no surface at
// all, and for a data class — where the constructor is the entire declaration — the
// page would say nothing about what the type holds. A plain parameter (`class
// Logger(name: String)`) declares no property and is skipped.
func kotlinCtorProps(code, owner string, ownerExported bool, line int) []Symbol {
	open := strings.IndexByte(code, '(')
	if open < 0 {
		return nil
	}
	// The matching close, so a default value holding a call does not end the list
	// early: `class C(val a: Int = f(1), val b: Int)`.
	depth := 0
	end := -1
	for i := open; i < len(code) && end < 0; i++ {
		switch code[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				end = i
			}
		}
	}
	if end < 0 {
		return nil
	}
	var out []Symbol
	for _, param := range splitTopLevel(code[open+1:end], ',') {
		p := strings.TrimSpace(param)
		if p == "" {
			continue
		}
		kw, name, ok := kotlinMemberDecl(p)
		if !ok || (kw != "val" && kw != "var") {
			continue
		}
		out = append(out, Symbol{
			Name: name, Kind: kotlinMemberKind(kw), Recv: owner,
			Exported: ownerExported && kotlinIsPublic(p, nil), Line: line,
		})
	}
	return out
}
