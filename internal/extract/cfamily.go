package extract

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// CExtractor reads C, C++ and Objective-C includes and declaration surface.
//
// Line-oriented, not a parser (design §4.1). One extractor for three languages
// because the thing that makes them one family is the thing this extractor reads:
// they share the preprocessor, and `#include` is the only dependency syntax any of
// them has. C++ and Objective-C each add declaration forms on top, and the extractor
// reads all of them regardless of which Lang dispatched the file. That is deliberate,
// and it is what makes the `.h` problem tractable — a header is C, C++ or Objective-C
// and its filename cannot say which (see discover's sourceExts), so the label is C
// and the reading is the whole family's.
//
// Three properties of C decide most of what is here:
//
//   - There is no module system. `#include "util/buffer.h"` names a *file*, resolved
//     against the including file's directory and the compiler's search path; `#include
//     <stdio.h>` names a file on the search path alone. Facts.Package is therefore
//     empty for C and holds the namespace for C++ — there is nothing else a file
//     declares that another file writes down.
//   - Visibility is not a keyword. A C function is external unless it says `static`,
//     which is the inverse of Java's default and the same shape as Kotlin's: absence
//     means public. `static` is the one modifier that removes a symbol from the link
//     surface, so it is the one that decides Exported.
//   - A declaration and a definition are different things and both are surface. A
//     prototype in a header and its definition in a `.c` are the same symbol seen
//     twice, and the header is the file another translation unit reads. Both are
//     recorded, in their own files, because a header with no symbols would report an
//     empty public interface for the one file whose entire purpose is to declare one.
//
// The preprocessor is read for includes and otherwise ignored: a macro-guarded block
// contributes its declarations whether or not the guard would be taken, because which
// branch a build selects is a fact about the build and not about the file. A
// function-like macro is not a symbol — `#define MAX(a,b)` has no linkage and no
// address, and recording it would put a name on the page that no caller can link to.
type CExtractor struct{}

// Langs implements Extractor.
func (CExtractor) Langs() []discover.Lang {
	return []discover.Lang{discover.LangC, discover.LangCpp, discover.LangObjC}
}

// Extract implements Extractor.
func (CExtractor) Extract(f discover.File) (Facts, error) {
	facts := Facts{Path: f.Path, Lang: f.Lang}
	lines := scanLines(f.Content, scanC)

	// The scope stack, holding namespaces, classes, structs and Objective-C
	// interfaces. A stack rather than a name because a nested class closing has to
	// restore the outer one, and because a namespace encloses types without being one.
	var scopes []cScope
	depth := 0
	// An Objective-C @interface or @implementation opens a scope that is closed by
	// `@end` and not by a brace, so it is tracked separately from the brace depth.
	objc := ""

	for i := 0; i < len(lines); i++ {
		cl := lines[i]
		code := strings.TrimSpace(cl.Text)
		if code == "" {
			depth, scopes = cAdvance(depth, scopes, cl.Text)
			continue
		}

		switch {
		case strings.HasPrefix(code, "#"):
			// A preprocessor directive may be spelled `#  include`, and a continuation
			// backslash may wrap it. Only the include carries a dependency.
			if im, ok := cInclude(code, cl.Raw, cl.Num); ok {
				facts.Imports = append(facts.Imports, im)
			}
			// A directive's own braces are not code braces: `#define BLOCK {` would open
			// a depth that never closes. Advancing is skipped entirely for that reason.

		case cAccessSpecifier(code) != "":
			// `public:` and its siblings change the default for every member after them,
			// which makes visibility in C++ a property of *position in the body* rather
			// than of the declaration's own line. Nothing else in this extractor works that
			// way, and without it a class reports its entire surface as unreachable: a
			// class's members default to private and the methods anybody calls are all
			// under a `public:` this switch is the only thing that sees.
			if len(scopes) > 0 && scopes[len(scopes)-1].kind == cScopeType &&
				depth == scopes[len(scopes)-1].depth+1 {
				scopes[len(scopes)-1].membersPublic = cAccessSpecifier(code) == "public"
			}

		case strings.HasPrefix(code, "@end"):
			objc = ""

		case strings.HasPrefix(code, "@interface") || strings.HasPrefix(code, "@implementation") ||
			strings.HasPrefix(code, "@protocol"):
			kw, name, ok := objcTypeDecl(code)
			if !ok {
				break
			}
			// A category — `@interface NSString (Extras)` — reopens a type declared
			// elsewhere and adds methods to it. The methods belong to the named type,
			// which is why the name is taken from before the parenthesis, but the
			// category itself is not a new type and is not recorded as one.
			if !objcIsCategory(code) && kw != "@implementation" {
				facts.Symbols = append(facts.Symbols, Symbol{
					Name: name, Kind: objcTypeKind(kw), Exported: true,
					Doc: cDoc(lines, i), Line: cl.Num,
				})
			}
			objc = name

		case objc != "" && (strings.HasPrefix(code, "-") || strings.HasPrefix(code, "+")):
			// An Objective-C method: `- (void)doThing:(id)arg`. The leading sign is the
			// whole signal for instance versus class, and both are surface.
			if name, ok := objcMethodDecl(code); ok {
				facts.Symbols = append(facts.Symbols, Symbol{
					Name: name, Kind: SymMethod, Recv: objc, Exported: true,
					Doc: cDoc(lines, i), Line: cl.Num,
				})
			}

		default:
			// A declaration wraps: a long parameter list, a template header on its own
			// line, a base-class list. Joining to where parentheses balance puts the
			// body's brace on the same logical line as the name.
			joined, last := joinParens(lines, i)
			// Attributes come off before anything reads the line, because every rule below
			// is written against the first parenthesis being the parameter list.
			jcode := cStripAttrs(strings.TrimSpace(joined.Text))
			tail := joinedTail(lines, i, last)

			switch {
			case cIsNamespace(jcode):
				name, anon := cNamespaceName(jcode)
				// An anonymous namespace gives its contents internal linkage, which is
				// `static` spelled as a scope. Its members are not link surface.
				scopes = append(scopes, cScope{
					name: name, kind: cScopeNamespace, depth: depth,
					exported: !anon, opened: strings.Contains(tail, "{"),
				})

			case cTypeDeclHasBody(lines, jcode, tail, last):
				kw, name, _ := cTypeDecl(jcode)
				sym := Symbol{
					Name: name, Kind: cTypeKind(kw), Exported: cScopesExported(scopes),
					Doc: cDoc(lines, i), Line: cl.Num,
				}
				facts.Symbols = append(facts.Symbols, sym)
				scopes = append(scopes, cScope{
					name: name, kind: cScopeType, depth: depth,
					exported: sym.Exported, opened: strings.Contains(tail, "{"),
					// A struct's and a C++ `class`'s members differ in exactly one way, and
					// it is the default: struct and union members are public, a class's are
					// private. Getting this backwards reports a class's whole implementation
					// as public surface, or a struct's whole interface as private.
					membersPublic: kw != "class",
				})

			default:
				if name, ok := cFuncDecl(jcode); ok && cDeclSite(scopes, depth) {
					owner, ownerExported := cOwner(scopes)
					sym := Symbol{
						Name: name, Line: cl.Num, Doc: cDoc(lines, i),
						Kind: SymFunc, Recv: owner,
						Exported: ownerExported && cFuncIsExported(jcode, scopes),
					}
					if owner != "" {
						sym.Kind = SymMethod
					}
					// A qualified definition — `void Buffer::append(...)` — is the out-of-line
					// body of a member declared in the class, so the receiver comes from the
					// name itself rather than from an open scope.
					//
					// Its visibility is not here. The access specifier that governs it is a line
					// in the class body, which is usually in another file, and this line carries
					// no keyword that could say. So the definition claims nothing: exportedness
					// is sticky in mergeSymbols, and a definition that claimed to be public
					// would override the `private:` the class body actually states — reporting
					// every out-of-line private method as public surface. The declaration in the
					// class body is the authority, and it is the file another translation unit
					// reads.
					if recv, base, ok := cQualifiedName(name); ok {
						sym.Name, sym.Recv, sym.Kind = base, recv, SymMethod
						sym.Exported = false
					}
					facts.Symbols = append(facts.Symbols, sym)
					// `main` is where a C program starts. Unlike the JVM's, it takes no
					// modifier and any signature the standard allows, so the name at file
					// scope is the whole test.
					if sym.Name == "main" && sym.Recv == "" && len(scopes) == 0 {
						facts.Entrypoints = append(facts.Entrypoints, "main")
					}
				}
			}

			depth, scopes = cAdvance(depth, scopes, tail)
			i = last
			continue
		}

		depth, scopes = cAdvance(depth, scopes, cl.Text)
	}
	facts.addQueries(sqlLiterals(lines))
	return facts, nil
}

// cScopeKind distinguishes a scope that names a type from one that only nests names.
type cScopeKind int

const (
	cScopeType cScopeKind = iota
	cScopeNamespace
)

// cScope is one open namespace or type declaration and the brace depth its body sits
// above.
type cScope struct {
	name string
	kind cScopeKind
	// depth is the brace depth of the line the scope was declared on. Its members are
	// at depth+1.
	depth int
	// exported is whether names inside are reachable from another translation unit. An
	// anonymous namespace and a static member both fail this.
	exported bool
	// opened records that the body's opening brace has been seen. C++ style routinely
	// puts it on the next line, and a scope popped before its brace arrives takes every
	// member of the type with it.
	opened bool
	// membersPublic marks a scope whose members need no keyword to be public: a struct,
	// a union, or a namespace. A C++ `class` defaults to private.
	membersPublic bool
}

// cAdvance applies one line's brace movement and closes the scopes it left.
//
// Braces only, for the reason jvmAdvance gives: a wrapped parameter list or a
// template argument list would otherwise move a depth meant to track bodies.
func cAdvance(depth int, scopes []cScope, text string) (int, []cScope) {
	depth += netDepth(text, "{", "}")
	if depth < 0 {
		depth = 0
	}
	for len(scopes) > 0 {
		s := &scopes[len(scopes)-1]
		if depth > s.depth {
			s.opened = true
			break
		}
		if !s.opened {
			break
		}
		scopes = scopes[:len(scopes)-1]
	}
	return depth, scopes
}

// cDeclSite reports whether a position can hold a declaration that is surface at all:
// file scope, a namespace's body, or a type's own body.
//
// This is the strongest precision rule here, and it is the same one the JVM extractor
// leans on: a call statement and a declaration have nearly the same shape —
// `append(buf, x);` against `void append(Buffer *buf, int x)` — and a function body is
// always at least one brace deeper than the declarations around it.
func cDeclSite(scopes []cScope, depth int) bool {
	if len(scopes) == 0 {
		return depth == 0
	}
	return depth == scopes[len(scopes)-1].depth+1
}

// cOwner returns the innermost enclosing *type* and whether it is reachable.
//
// A namespace is skipped: it qualifies a name without owning it, so a function in
// `namespace util` is a function and not a method of `util`.
func cOwner(scopes []cScope) (name string, exported bool) {
	if len(scopes) == 0 {
		return "", true
	}
	inner := scopes[len(scopes)-1]
	if inner.kind == cScopeNamespace {
		return "", cScopesExported(scopes)
	}
	return inner.name, cScopesExported(scopes)
}

// cScopesExported reports whether every enclosing scope is reachable from outside the
// translation unit. A public member of a type in an anonymous namespace is not
// surface, and reporting it as such puts a symbol on the page that nothing can link.
func cScopesExported(scopes []cScope) bool {
	for _, s := range scopes {
		if !s.exported {
			return false
		}
	}
	return true
}

// cInclude reads one #include directive.
//
// Both forms are recorded and the delimiters are kept, because the delimiter *is* the
// resolution rule and nothing else in the line carries it: `"..."` searches the
// including file's own directory first and so is normally a file in this repository,
// while `<...>` searches only the compiler's path and so is normally the toolchain or
// an installed library. Dropping the distinction would make every include look
// repo-relative and point half of them at files that do not exist.
//
// An `#import` is Objective-C's include-once form and carries the identical fact.
//
// The quoted form's path is read from raw rather than from the scanned text, because
// the scanner blanks a string's body: `#include "buffer.h"` arrives here as
// `#include "         "`, with the quotes kept and the name gone. The angled form needs
// no such recovery — `<...>` is not a string delimiter, so it survives scanning intact.
// Reading the quoted form from the scanned text is how every quoted include in a file
// goes missing while every angled one is found, which is a gap that looks like a
// language quirk rather than a bug.
func cInclude(code, raw string, line int) (Import, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(code, "#"))
	for _, kw := range []string{"include_next", "include", "import"} {
		r, ok := strings.CutPrefix(rest, kw)
		if !ok {
			continue
		}
		// The keyword must end here: `#included` and `#importing` are not directives,
		// and a prefix match would read one as an include of whatever followed.
		if r != "" && identChar(r[0]) {
			continue
		}
		rest = strings.TrimSpace(r)
		// A macro-valued include — `#include HEADER_NAME` — names no file this
		// extractor can know, and recording the macro's name as a path would point at
		// something that does not exist.
		if len(rest) < 2 {
			return Import{}, false
		}
		switch rest[0] {
		case '"':
			path, ok := stringAt(raw, 0)
			if !ok || strings.TrimSpace(path) == "" {
				return Import{}, false
			}
			return Import{Raw: `"` + path + `"`, Line: line}, true
		case '<':
			end := strings.IndexByte(rest[1:], '>')
			if end <= 0 {
				return Import{}, false
			}
			path := rest[1 : 1+end]
			if strings.TrimSpace(path) == "" {
				return Import{}, false
			}
			return Import{Raw: "<" + path + ">", Line: line}, true
		}
		return Import{}, false
	}
	return Import{}, false
}

// IncludePath returns the path inside an include's delimiters, and whether the form
// was the quoted one.
//
// Exported because resolution needs both halves and the delimiters are carried in
// Import.Raw rather than in a field of their own — a field only C would set. The
// alternative, an extractor that strips them, loses the resolution rule; the
// alternative that keeps them in a separate field adds a column to every language's
// import for one language's benefit.
func IncludePath(raw string) (path string, quoted bool) {
	if len(raw) < 2 {
		return raw, false
	}
	switch raw[0] {
	case '"':
		return strings.TrimSuffix(raw[1:], `"`), true
	case '<':
		return strings.TrimSuffix(raw[1:], ">"), false
	}
	return raw, false
}

// cIsNamespace reports whether a line opens a C++ namespace.
func cIsNamespace(code string) bool {
	rest, ok := strings.CutPrefix(code, "namespace")
	if !ok {
		return false
	}
	// `namespace alias = other::ns;` is an alias, not a scope, and pushing one for it
	// would leave a scope open for the rest of the file.
	return (rest == "" || !identChar(rest[0])) && !strings.Contains(rest, "=")
}

// cNamespaceName returns a namespace's name and whether it is anonymous.
//
// A nested definition — `namespace a::b {` — is named for its innermost part, which is
// the one a member is qualified by. An anonymous namespace has no name and gives
// everything inside it internal linkage.
func cNamespaceName(code string) (name string, anon bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(code, "namespace"))
	rest = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rest), "{"))
	if rest == "" {
		return "", true
	}
	if i := strings.LastIndex(rest, "::"); i >= 0 {
		rest = rest[i+2:]
	}
	return strings.TrimSpace(rest), false
}

// cTypeDecl reports the keyword and name of a struct, union, enum or class
// declaration.
//
// The name may be absent, which is not an error: `typedef struct { int x; } Point;`
// declares its type through the typedef, and the anonymous struct that carries it has
// no name of its own. Such a declaration yields no symbol here — cTypeDeclHasBody
// rejects it — and the typedef's own line is what names the type.
//
// The name is not always the token straight after the keyword. C and C++ both allow
// things in between, and all of them are in wide use: an export macro
// (`class CORPUS_API Session`), an alignment specifier (`struct alignas(16) Aligned`),
// and either compiler's attribute syntax (`class __declspec(dllexport) Exported`,
// `struct __attribute__((packed)) Packed`). Read without skipping them, the attribute
// forms named the type `__declspec` or `__attribute__`, and the macro form named
// nothing at all.
func cTypeDecl(code string) (kw, name string, ok bool) {
	words := cWords(code[:cTypeHeadEnd(code)])
	for i, w := range words {
		if !cIsTypeKeyword(w) {
			if cIsDeclModifier(w) {
				continue
			}
			return "", "", false
		}
		// `enum class Mode` is C++'s scoped enumeration: the keyword that names the type
		// is the second one.
		if i+1 < len(words) && cIsTypeKeyword(words[i+1]) {
			continue
		}
		// An export macro cannot be told from a type name by anything but convention, and
		// the convention is that it shouts. So a shouting token is skipped in favour of a
		// following name — and kept when there is none, which is what `struct POINT {`
		// needs.
		macro := ""
		for j := i + 1; j < len(words); j++ {
			n := cIdentAt(words, j)
			if n == "" {
				continue
			}
			if cIsShoutingName(n) && macro == "" {
				macro = n
				continue
			}
			return w, n, true
		}
		if macro != "" {
			return w, macro, true
		}
		return "", "", false
	}
	return "", "", false
}

// cIsShoutingName reports whether an identifier is spelled the way an export or
// attribute macro is spelled: upper case, digits and underscores only.
func cIsShoutingName(s string) bool {
	upper := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= 'A' && c <= 'Z':
			upper = true
		case c == '_' || (c >= '0' && c <= '9'):
		default:
			return false
		}
	}
	return upper
}

// cTypeHeadEnd returns where a type declaration's head stops: at the brace that opens
// the body, or at the `:` that introduces a base-class list or a scoped enum's
// underlying type, whichever comes first.
//
// Cutting there is what lets the name be read as the last identifier. Without it,
// `enum class Mode : unsigned int {` ends in `int` and `class Holder : public Base {`
// ends in `Base`, and the type would be named after its own base. A `::` is skipped
// because it qualifies a name rather than starting a clause.
func cTypeHeadEnd(code string) int {
	depth := 0
	for i := 0; i < len(code); i++ {
		switch code[i] {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			if depth > 0 {
				depth--
			}
		case '{':
			if depth == 0 {
				return i
			}
		case ':':
			if depth > 0 {
				continue
			}
			if i+1 < len(code) && code[i+1] == ':' {
				i++
				continue
			}
			if i > 0 && code[i-1] == ':' {
				continue
			}
			return i
		}
	}
	return len(code)
}

// cTypeDeclHasBody reports whether a line declares a type *with* a body, which is the
// only form that is a definition and so the only one that is a symbol.
//
// The distinction is load-bearing and specific to C. A forward declaration — `struct
// Buffer;` or `class Widget;` — is a promise that a type exists, made so a pointer to
// it can be declared, and it appears in headers constantly. Recording one as a symbol
// would claim a type is defined in a file that deliberately does not define it, and
// pushing a scope for one would leave that scope open for the rest of the file and
// claim every later declaration as its member.
//
// A variable of struct type — `struct Buffer buf;` — has the same first two tokens and
// is not a declaration of the type at all.
// A brace near a type's name is also not enough. `struct Buffer *buffer_make(size_t)`
// is a function returning a pointer to a struct, and its body's brace is on the same
// line — so the brace alone reports a phantom `Buffer` defined in a file that only
// mentions it, opens a scope that swallows the rest of the file, and loses the one
// symbol the line actually declares. cTypeDefinesName is the rule that separates them.
func cTypeDeclHasBody(lines []codeLine, code, tail string, last int) bool {
	_, name, ok := cTypeDecl(code)
	if !ok {
		return false
	}
	if strings.Contains(tail, "{") {
		return cTypeDefinesName(code, name)
	}
	// A semicolon on the declaration's own line ends it, and nothing further down can
	// give it a body. Checking this before looking ahead is what keeps `union Slot;`
	// from borrowing the brace of the next type in the file — the exact way a forward
	// declaration becomes a phantom type, and worse, an open scope that claims every
	// declaration after it as a member.
	if strings.Contains(tail, ";") {
		return false
	}
	// A brace may open further down, because C++ style routinely puts it below a wrapped
	// head: a base-class list with one base per line, `final` on its own line, an
	// attribute run. The guard against a forward declaration reaching a later type's
	// brace is the `;` below and not the size of this window — a forward declaration
	// always ends in a semicolon, and the scan stops at the first one. So the count is a
	// bound on a runaway scan and nothing more, and it is wide enough for a real
	// base-class list: at five, a class with six bases appeared nowhere in the bundle.
	const maxLook = 40
	head := code
	for i, seen := last+1, 0; i < len(lines) && seen < maxLook; i++ {
		t := strings.TrimSpace(lines[i].Text)
		if t == "" {
			continue
		}
		seen++
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "}") {
			return false
		}
		brace := strings.IndexByte(t, '{')
		semi := strings.IndexByte(t, ';')
		if brace >= 0 && (semi < 0 || semi > brace) {
			// The wrapped form has the same ambiguity as the single-line one: a struct
			// returning function may be spelled with its name on the next line.
			return cTypeDefinesName(head+" "+t, name)
		}
		if semi >= 0 {
			return false
		}
		head += " " + t
	}
	return false
}

// cTypeDefinesName reports whether the brace that follows opens the named type's own
// body rather than something declared *using* that type.
//
// A definition puts nothing between the name and the body except what C++ allows there:
// `final`, and a `:` clause, which is a base-class list on a class or the underlying
// type on a scoped enum. Any further token means the type is being used, not defined —
// a declarator. `struct Buffer *buffer_make(size_t cap) {` has `buffer_make` after the
// name and is a function; `class Session final : public Stream {` has only the two
// allowed forms and is a type.
func cTypeDefinesName(code, name string) bool {
	words := cWords(code)
	for i, w := range words {
		if w != name {
			continue
		}
		for _, w := range words[i+1:] {
			// Everything from the colon on is the base-class list or the underlying type,
			// and the body opens after it.
			if strings.HasPrefix(w, ":") {
				return true
			}
			if w == "final" {
				continue
			}
			return false
		}
		return true
	}
	return false
}

// cTypeKind maps a type keyword onto a SymbolKind.
func cTypeKind(kw string) SymbolKind {
	if kw == "class" {
		// A C++ class holds members and controls their visibility, which is the
		// distinction SymClass draws against SymType.
		return SymClass
	}
	// A struct, a union and an enum are named aggregates without a visibility
	// mechanism, which is what SymType describes.
	return SymType
}

// objcTypeKind maps an Objective-C declaration keyword onto a SymbolKind.
func objcTypeKind(kw string) SymbolKind {
	if kw == "@protocol" {
		// A protocol is a set of methods a class promises to implement, which is an
		// interface by every property that matters here.
		return SymInterface
	}
	return SymClass
}

// objcTypeDecl reports the keyword and name of an Objective-C type declaration.
//
// The name is the first identifier after the keyword in every form: `@interface Foo :
// NSObject`, `@interface Foo (Category)`, `@protocol Bar <NSObject>`, `@implementation
// Foo`.
func objcTypeDecl(code string) (kw, name string, ok bool) {
	fields := strings.FieldsFunc(code, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ':' || r == '(' || r == ')' || r == '<' || r == '>'
	})
	if len(fields) < 2 || !strings.HasPrefix(fields[0], "@") {
		return "", "", false
	}
	return fields[0], fields[1], true
}

// objcIsCategory reports whether an @interface reopens an existing class to add
// methods, rather than declaring a new one.
func objcIsCategory(code string) bool {
	open := strings.IndexByte(code, '(')
	colon := strings.IndexByte(code, ':')
	return open >= 0 && (colon < 0 || open < colon)
}

// objcMethodDecl reads the selector of an Objective-C method declaration.
//
// The selector is the method's name and it is interleaved with the parameters:
// `- (void)setName:(NSString *)n age:(int)a` is the selector `setName:age:`. Recording
// only the first part would collapse `setName:` and `setName:age:` into one name, and
// they are different methods.
func objcMethodDecl(code string) (string, bool) {
	// Skip the sign and the return type in parentheses.
	rest := strings.TrimSpace(code[1:])
	if strings.HasPrefix(rest, "(") {
		d, i := 0, 0
		for ; i < len(rest); i++ {
			if rest[i] == '(' {
				d++
			} else if rest[i] == ')' {
				d--
				if d == 0 {
					i++
					break
				}
			}
		}
		rest = strings.TrimSpace(rest[i:])
	}
	var sel strings.Builder
	i := 0
	for i < len(rest) {
		// The space between one argument's name and the next selector part is the only
		// separator the syntax has, so skipping it is what lets the loop reach the second
		// part at all. Without this the selector stops at `setName:` and every
		// multi-argument method in the file collapses onto its first part.
		for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t') {
			i++
		}
		// Each selector part is an identifier, optionally followed by a colon and a
		// parenthesised type that is skipped along with the argument name after it.
		start := i
		for i < len(rest) && identChar(rest[i]) {
			i++
		}
		if i == start {
			break
		}
		part := rest[start:i]
		if i < len(rest) && rest[i] == ':' {
			sel.WriteString(part)
			sel.WriteByte(':')
			i++
			i = cSkipParens(rest, i)
			// The argument's own name carries no part of the selector.
			for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t') {
				i++
			}
			for i < len(rest) && identChar(rest[i]) {
				i++
			}
			continue
		}
		// No colon: a selector with no arguments, which is the whole name.
		if sel.Len() == 0 {
			sel.WriteString(part)
		}
		break
	}
	name := sel.String()
	if name == "" {
		return "", false
	}
	return name, true
}

// cStripAttrs removes attribute constructs from a declaration's head.
//
// An attribute carries a parenthesised argument list, and every rule in cFuncDecl is
// written against the first parenthesis on the line being the *parameter* list. So an
// attribute in front of the return type moves that parenthesis and takes the whole
// declaration with it: `__attribute__((unused)) static int helper(void)` yielded no
// symbol at all, and `struct __attribute__((packed)) Buffer *make(void)` yielded one
// called `__attribute__`. Both spellings are ordinary in real C — the GNU form guards
// half of a portable header, and `__declspec(dllexport)` is how a symbol leaves a DLL.
//
// Removed rather than skipped, because there is no upper bound on how many of them
// stack, and nothing downstream needs to know an attribute was there.
func cStripAttrs(code string) string {
	var b strings.Builder
	for i := 0; i < len(code); {
		// C++11's own syntax, which takes no keyword: `[[nodiscard]] int size() const`.
		if strings.HasPrefix(code[i:], "[[") {
			if j := strings.Index(code[i:], "]]"); j >= 0 {
				i += j + 2
				continue
			}
		}
		if !identChar(code[i]) {
			b.WriteByte(code[i])
			i++
			continue
		}
		start := i
		for i < len(code) && identChar(code[i]) {
			i++
		}
		w := code[start:i]
		if cIsAttrKeyword(w) {
			if j := cSkipParens(code, i); j > i {
				i = j
				continue
			}
		}
		b.WriteString(w)
	}
	return strings.TrimSpace(b.String())
}

// cIsAttrKeyword reports whether a token introduces a parenthesised attribute.
func cIsAttrKeyword(w string) bool {
	switch w {
	case "__attribute__", "__attribute", "__declspec", "alignas", "_Alignas":
		return true
	}
	return false
}

// cSkipParens advances past a balanced parenthesised group starting at i, if one is
// there.
func cSkipParens(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	if i >= len(s) || s[i] != '(' {
		return i
	}
	d := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case '(':
			d++
		case ')':
			d--
			if d == 0 {
				return i + 1
			}
		}
	}
	return i
}

// cFuncDecl reports the name of a function declared or defined on a line.
//
// The shape is `<type> name(`, and every hard case is a statement or an expression
// that shares it. C is worse than Java here because it has no `new` and no modifier
// requirement, so the rules are drawn tighter:
//
//   - The name may not stand alone. `append(buf, x);` is a call; a declaration always
//     has a return type in front of the name. C++ constructors and destructors are the
//     exception and are handled by the caller, which knows the enclosing type's name.
//   - A control-flow keyword taking a parenthesis is rejected on the name itself:
//     `if`, `while`, `switch`, `for`, `catch`, `return`, `sizeof`.
//   - No `=` before the name, which rejects an initialiser whose right-hand side is a
//     call at exactly the depth a declaration sits at: `int *p = malloc(n);`.
//   - A macro invocation at file scope — `MODULE_LICENSE("GPL");`, `TEST(Suite, Name)`
//     — is a call, not a declaration, and is rejected by the same standing-alone rule.
func cFuncDecl(code string) (string, bool) {
	open := strings.IndexByte(code, '(')
	if open < 0 {
		return "", false
	}
	head := strings.TrimSpace(code[:open])
	if head == "" {
		return "", false
	}
	// A pointer-returning declarator wraps its name in parentheses: `int (*handler)(void)`
	// declares a variable, not a function, and its name sits inside the first group.
	// Rejected rather than read, because a function pointer is data.
	if strings.HasSuffix(head, "*") || strings.HasSuffix(head, "&") {
		return "", false
	}
	// The same declarator with a space before the parenthesis — `static void (*hook)(void)`
	// — leaves nothing on the head to reject, so the star inside the group is the signal.
	// Without this the last word of the *type* is read as the name, and the file reports a
	// function called `void`.
	if rest := strings.TrimLeft(code[open+1:], " \t"); rest != "" &&
		(rest[0] == '*' || rest[0] == '&') {
		return "", false
	}
	end := len(head)
	start := end
	for start > 0 && cNameChar(head[start-1]) {
		start--
	}
	name := head[start:end]
	if name == "" || (name[0] >= '0' && name[0] <= '9') || cControlKeyword(name) {
		return "", false
	}
	// A trailing `::` means the name is a qualifier and the real name follows, which
	// happens when the declarator wraps oddly. Nothing usable here.
	if strings.HasSuffix(name, "::") {
		return "", false
	}
	before := strings.TrimSpace(head[:start])
	if before == "" || strings.ContainsRune(before, '=') {
		return "", false
	}
	for _, w := range cWords(before) {
		switch w {
		case "return", "sizeof", "new", "delete", "throw", "case", "goto", "typedef":
			return "", false
		}
	}
	// A return type, a modifier or a pointer star is the last thing before a function's
	// name. Any other operator means the name is being used rather than declared.
	if last := before[len(before)-1]; !identChar(last) && last != '*' && last != '&' &&
		last != '>' && last != ':' {
		return "", false
	}
	return name, true
}

// cQualifiedName splits an out-of-line member definition's name into its type and the
// member: `Buffer::append` is `append` on `Buffer`.
//
// The last qualifier wins, because a namespace may precede the type —
// `util::Buffer::append` is a method of `Buffer`, not of `util`.
func cQualifiedName(name string) (recv, base string, ok bool) {
	i := strings.LastIndex(name, "::")
	if i < 0 {
		return "", name, false
	}
	base = name[i+2:]
	recv = name[:i]
	if j := strings.LastIndex(recv, "::"); j >= 0 {
		recv = recv[j+2:]
	}
	if recv == "" || base == "" {
		return "", name, false
	}
	return recv, base, true
}

// cFuncIsExported reports whether a function is reachable from another translation
// unit.
//
// `static` is the whole rule at file scope, and it is the inverse of Java's default:
// absence of a keyword means external linkage. Inside a C++ class, the access
// specifier decides instead, and that is a *line* elsewhere in the body rather than a
// modifier on this declaration — which is why the scope carries membersPublic and this
// function consults it.
func cFuncIsExported(code string, scopes []cScope) bool {
	if cHasWord(code, "static") {
		return false
	}
	if len(scopes) > 0 && scopes[len(scopes)-1].kind == cScopeType {
		return scopes[len(scopes)-1].membersPublic
	}
	return true
}

// cAccessSpecifier returns the access-specifier label a line consists of, or "".
//
// The whole line must be the specifier, because the same keyword introduces two other
// things that are not a visibility switch: a base-class list writes `public Base` after
// a colon, and `private: int x;` on one line is legal but vanishingly rare in real code
// next to the risk of matching either of those.
func cAccessSpecifier(code string) string {
	label, ok := strings.CutSuffix(strings.TrimSpace(code), ":")
	if !ok {
		return ""
	}
	switch strings.TrimSpace(label) {
	case "public", "private", "protected":
		return strings.TrimSpace(label)
	// A slot is Qt's macro-based extension of the same idea and behaves identically.
	case "public slots", "private slots", "protected slots", "signals", "public Q_SLOTS":
		if strings.HasPrefix(strings.TrimSpace(label), "public") ||
			strings.TrimSpace(label) == "signals" {
			return "public"
		}
		return "private"
	}
	return ""
}

// cIsTypeKeyword reports whether a token introduces a named aggregate type.
func cIsTypeKeyword(w string) bool {
	switch w {
	case "struct", "union", "enum", "class":
		return true
	}
	return false
}

// cIsDeclModifier reports whether a token may precede a type keyword.
func cIsDeclModifier(w string) bool {
	switch w {
	case "typedef", "static", "extern", "inline", "const", "constexpr", "template",
		"public", "private", "protected", "friend", "final", "explicit", "virtual",
		"thread_local", "alignas", "_Alignas", "__declspec", "__attribute__":
		return true
	}
	return false
}

// cControlKeyword reports whether a token is a statement keyword that takes a
// parenthesis, which is the shape a declaration has to be told apart from.
func cControlKeyword(w string) bool {
	switch w {
	case "if", "for", "while", "switch", "catch", "return", "do", "else", "try",
		"sizeof", "alignof", "typeof", "defined", "static_assert", "_Static_assert",
		"throw", "new", "delete", "decltype", "noexcept":
		return true
	}
	return false
}

// cNameChar reports whether a byte can appear in a possibly-qualified C++ name, which
// includes the `::` an out-of-line definition carries.
func cNameChar(b byte) bool { return identChar(b) || b == ':' }

// cIdentAt returns words[i] when it is a plain identifier that could be a declared
// name, else "".
func cIdentAt(words []string, i int) string {
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
	if cIsTypeKeyword(w) || cIsDeclModifier(w) || cControlKeyword(w) {
		return ""
	}
	return w
}

// cWords splits a declaration head into tokens, skipping anything inside brackets so
// that `std::map<std::string, int> byName` yields two tokens rather than five, and
// stopping at the brace that opens a body.
func cWords(code string) []string {
	var out []string
	depth := 0
	start := -1
	flush := func(end int) {
		if start >= 0 {
			out = append(out, code[start:end])
			start = -1
		}
	}
	for i := 0; i < len(code); i++ {
		c := code[i]
		switch c {
		case '<', '(', '[':
			flush(i)
			depth++
			continue
		case '>', ')', ']':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth > 0 {
			continue
		}
		if identChar(c) || c == ':' {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
		if c == '{' {
			return out
		}
	}
	flush(len(code))
	return out
}

// cHasWord reports whether a declaration carries a keyword as a whole token.
//
// Matched against tokens rather than by substring, for the reason jvmHasModifier
// gives: a variable named `static_buf` and a type named `ExternalRef` both contain a
// keyword's letters.
func cHasWord(code, kw string) bool {
	for _, w := range cWords(code) {
		if w == kw {
			return true
		}
	}
	return false
}

// cDoc reads the documentation comment above a declaration.
//
// Read from Raw, because the scanner strips comments. C-family projects use three
// forms and all three are in wide use: Doxygen's `/** */` and `/*! */`, and a run of
// `///` line comments. A plain `/* */` block is not documentation — it is as often a
// licence header or a commented-out block — and is deliberately not read, which is the
// same line jvmDoc draws.
func cDoc(lines []codeLine, idx int) string {
	const maxDoc = 200

	i := idx - 1
	for i >= 0 && idx-i < maxDoc {
		t := strings.TrimSpace(lines[i].Raw)
		// A preprocessor line between the comment and the declaration is normal — an
		// `#ifdef` guarding a documented function — and is skipped for the same reason
		// jvmDoc skips annotations.
		if t == "" || strings.HasPrefix(t, "#") {
			i--
			continue
		}
		break
	}
	if i < 0 {
		return ""
	}
	t := strings.TrimSpace(lines[i].Raw)
	if strings.HasPrefix(t, "///") {
		return cLineDoc(lines, i)
	}
	if !strings.HasSuffix(t, "*/") {
		return ""
	}
	var block []string
	for stop := i - maxDoc; i >= 0 && i > stop; i-- {
		t := strings.TrimSpace(lines[i].Raw)
		if strings.HasPrefix(t, "/**") || strings.HasPrefix(t, "/*!") {
			block = append([]string{cDocLine(t)}, block...)
			return cDocText(block)
		}
		if strings.HasPrefix(t, "/*") {
			return ""
		}
		block = append([]string{cDocLine(t)}, block...)
		if i == 0 {
			return ""
		}
	}
	return ""
}

// cLineDoc reads a run of `///` comments ending at idx.
func cLineDoc(lines []codeLine, idx int) string {
	var block []string
	for i := idx; i >= 0; i-- {
		t := strings.TrimSpace(lines[i].Raw)
		if !strings.HasPrefix(t, "///") {
			break
		}
		block = append([]string{strings.TrimSpace(strings.TrimPrefix(t, "///"))}, block...)
	}
	return cDocText(block)
}

// cDocText joins a documentation block and cuts it at the first Doxygen tag.
//
// A tag block is reference material rather than a summary sentence, which is the same
// call jvmDoc makes about Javadoc's. Doxygen accepts both `@param` and `\param` and
// both appear in real code.
func cDocText(block []string) string {
	text := strings.TrimSpace(strings.Join(block, " "))
	for _, tag := range []string{
		"@param", "@return", "@retval", "@throws", "@see", "@note", "@brief",
		`\param`, `\return`, `\retval`, `\throws`, `\see`, `\note`, `\brief`,
	} {
		if j := strings.Index(text, tag); j > 0 {
			text = text[:j]
		}
	}
	return FirstSentence(text)
}

// cDocLine strips a doc comment's delimiters and leading asterisk from one line.
func cDocLine(t string) string {
	for _, p := range []string{"/**", "/*!", "/*"} {
		if s, ok := strings.CutPrefix(t, p); ok {
			t = s
			break
		}
	}
	t = strings.TrimSuffix(t, "*/")
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "*")
	return strings.TrimSpace(t)
}
