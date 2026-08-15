package extract

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// CSharpExtractor reads C# namespaces, using directives, and type surface.
//
// Line-oriented, not a parser (design §4.1). C# is close enough to Java that it reuses
// the same scope machinery — jvmAdvance, jvmDirectMember, jvmDeclSite, jvmWords — and
// the differences worth stating are these:
//
//   - The default visibility is *two* defaults, not one. A type with no modifier is
//     `internal`: assembly-visible, which is not public surface, on the same grounds as
//     Rust's `pub(crate)` and Kotlin's `internal`. A member with no modifier is
//     `private`. Java's single package-private default covers both cases; C# needs the
//     two rules kept apart, which is why csharpTypeIsPublic and csharpMemberIsPublic
//     are separate functions.
//   - A file-scoped namespace — `namespace App.Domain;` with no braces, the default in
//     every template since C# 10 — declares the namespace for the whole file without
//     opening a scope. The braced form opens one. Reading the first as the second would
//     put every declaration in the file one depth too shallow.
//   - A property is real surface and has no analogue in Java. `public string Name
//     { get; set; }` is what a C# class exposes where Java would expose a getter, and an
//     extractor that skips them reports a DTO as having no members at all.
//   - `using` has two unrelated meanings. At file level it imports a namespace; inside
//     a method body, `using var f = ...` is a disposal scope and imports nothing.
type CSharpExtractor struct{}

// Langs implements Extractor.
func (CSharpExtractor) Langs() []discover.Lang { return []discover.Lang{discover.LangCSharp} }

// Extract implements Extractor.
func (CSharpExtractor) Extract(f discover.File) (Facts, error) {
	facts := Facts{Path: f.Path, Lang: discover.LangCSharp}
	lines := scanLines(f.Content, scanCSharp)

	var types []jvmScope
	depth := 0
	// nsDepth is the brace depth a top-level declaration of this file sits at: 0 for a
	// file-scoped namespace, and 1 inside a braced one. Without it every declaration in
	// the braced form is one brace deeper than jvmDeclSite's top-level test allows, and
	// the whole file is silently dropped — which is most C# written before C# 10.
	nsDepth := 0
	nsPending := false

	for i := 0; i < len(lines); i++ {
		cl := lines[i]
		code := strings.TrimSpace(cl.Text)
		if nsPending && strings.HasPrefix(code, "{") {
			// The namespace's opening brace on its own line, which is the style the older
			// templates emit. Its depth is what the file's declarations sit at.
			depth, types = jvmAdvance(depth, types, cl.Text)
			nsDepth, nsPending = depth, false
			continue
		}
		if code == "" {
			depth, types = jvmAdvance(depth, types, cl.Text)
			continue
		}
		// An attribute is metadata on its own line above the declaration, the same
		// position a Java annotation occupies. `[Fact]`, `[HttpGet("/x")]`.
		if strings.HasPrefix(code, "[") {
			depth, types = jvmAdvance(depth, types, cl.Text)
			continue
		}
		// A preprocessor directive is not a declaration. `#region` and `#if DEBUG` are
		// both common, and neither opens a brace the scope stack should count.
		if strings.HasPrefix(code, "#") {
			continue
		}

		switch {
		case strings.HasPrefix(code, "namespace "):
			facts.Package = csharpNamespace(code)
			// A braced namespace opens a scope; a file-scoped one does not. Either way the
			// brace on this line is what says which, so jvmAdvance gets the line as-is.
			depth, types = jvmAdvance(depth, types, cl.Text)
			if strings.Contains(cl.Text, "{") {
				nsDepth = depth
			} else if !strings.Contains(cl.Text, ";") {
				// A brace on the next line, which the older templates put there.
				nsPending = true
			}

		case csharpIsUsingDirective(code, types, depth):
			if im, ok := csharpUsing(code, cl.Num); ok {
				facts.Imports = append(facts.Imports, im)
			}
			depth, types = jvmAdvance(depth, types, cl.Text)

		default:
			joined, last := joinParens(lines, i)
			jcode := strings.TrimSpace(joined.Text)
			tail := joinedTail(lines, i, last)

			if kw, name, ok := csharpTypeDecl(jcode); ok && csharpDeclSite(types, depth, nsDepth) {
				sym := Symbol{
					Name: name, Kind: csharpTypeKind(kw),
					Exported: csharpTypeIsPublic(jcode, types),
					Doc:      csharpDoc(lines, i), Line: cl.Num,
				}
				facts.Symbols = append(facts.Symbols, sym)
				if !csharpOpensBody(lines, tail, last) {
					// A record declared with a positional parameter list and no body —
					// `public record Money(decimal Amount);` — declares no members. A scope
					// pushed for it would never close and would claim the rest of the file.
					depth, types = jvmAdvance(depth, types, tail)
					i = last
					continue
				}
				types = append(types, jvmScope{
					name: name, depth: depth, exported: sym.Exported,
					opened: strings.Contains(tail, "{"),
					// An interface's members are public by definition and carry no modifier
					// saying so. A class's are private by default, which is the case
					// membersPublic=false covers.
					membersPublic: kw == "interface",
				})
			} else if name, ok := csharpConstDecl(jcode); ok && jvmDirectMember(types, depth) {
				owner := types[len(types)-1]
				facts.Symbols = append(facts.Symbols, Symbol{
					Name: name, Kind: SymConst, Recv: owner.name,
					Exported: owner.exported && csharpMemberIsPublic(jcode, owner),
					Doc:      csharpDoc(lines, i), Line: cl.Num,
				})
			} else if name, ok := csharpPropertyDecl(jcode); ok && jvmDirectMember(types, depth) {
				owner := types[len(types)-1]
				facts.Symbols = append(facts.Symbols, Symbol{
					// A property is not a method — it is accessed as a field — but it is a
					// member of its type, and SymMethod is the kind that carries a Recv.
					// Recording it as SymVar would lose the owner, which is the more useful
					// fact.
					Name: name, Kind: SymMethod, Recv: owner.name,
					Exported: owner.exported && csharpMemberIsPublic(jcode, owner),
					Doc:      csharpDoc(lines, i), Line: cl.Num,
				})
			} else if name, ok := csharpMethodDecl(jcode); ok && jvmDirectMember(types, depth) &&
				name != types[len(types)-1].name {
				// A constructor is skipped for the reason javaMethodDecl gives: Symbol has
				// no signature field, so `Service.Service` repeats the type's name and adds
				// nothing.
				owner := types[len(types)-1]
				facts.Symbols = append(facts.Symbols, Symbol{
					Name: name, Kind: SymMethod, Recv: owner.name,
					Exported: owner.exported && csharpMemberIsPublic(jcode, owner),
					Doc:      csharpDoc(lines, i), Line: cl.Num,
				})
				if name == "Main" && jvmHasModifier(jcode, "static") {
					facts.Entrypoints = append(facts.Entrypoints, "Main")
				}
			}

			depth, types = jvmAdvance(depth, types, tail)
			i = last
		}
	}
	facts.addQueries(sqlLiterals(lines))
	return facts, nil
}

// csharpNamespace reads a namespace declaration, in either the braced or the
// file-scoped form.
func csharpNamespace(code string) string {
	rest := strings.TrimSpace(strings.TrimPrefix(code, "namespace "))
	if i := strings.IndexAny(rest, " \t{;"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// csharpIsUsingDirective reports whether a line is a namespace import rather than a
// disposal scope.
//
// The keyword is the same for both, and only position and shape tell them apart: a
// `using` directive is at the top of a file, while `using var conn = Open()` is a
// statement inside a method body. Reading the second as an import would record a
// dependency on a namespace named after a local variable.
//
// An assignment is not enough to separate them, because the alias form —
// `using Json = System.Text.Json;` — has one and is a directive. What does separate them
// is what stands to the left of the `=`: an alias is a single identifier, while a
// statement declares a variable and so has a type in front of its name. `using var x` and
// `using StreamReader r` are both two words; `using Json` is one.
func csharpIsUsingDirective(code string, types []jvmScope, depth int) bool {
	code = strings.TrimSpace(strings.TrimPrefix(code, "global "))
	if !strings.HasPrefix(code, "using ") && !strings.HasPrefix(code, "using(") {
		return false
	}
	// Inside a type body, a `using` can only be a statement — there are no directives
	// there.
	if len(types) > 0 {
		return false
	}
	_ = depth
	rest := strings.TrimSpace(code[len("using"):])
	// `using (var x = ...)` is a statement whatever else it holds.
	if strings.HasPrefix(rest, "(") {
		return false
	}
	eq := strings.IndexByte(rest, '=')
	if eq < 0 {
		// A plain directive. A call with no assignment — `using Open();` is not legal C#,
		// but a wrapped statement could reach here — is not a namespace.
		return !strings.ContainsAny(rest, "(")
	}
	// The alias form: one identifier before the `=`, and a dotted name after it.
	return len(strings.Fields(rest[:eq])) == 1 && !strings.ContainsAny(rest[eq:], "(")
}

// csharpUsing reads a using directive.
//
// Three forms:
//
//	using System.Text.Json;                       a namespace
//	using Json = System.Text.Json;                aliased
//	using static System.Math;                     a type's static members
//
// The namespace is what is recorded, for the reason javaImport gives: it is the name
// another file declares, and so the only part resolution can match. `using static
// System.Math` names a type, so its final segment is split off the same way — the
// dependency is on System, not on a namespace called System.Math that nothing declares.
//
// A global using — `global using System;`, which applies to every file in the project —
// is read as an ordinary one. The distinction is about scope within the project, not
// about what is depended on.
func csharpUsing(code string, line int) (Import, bool) {
	// `global` precedes the keyword, not follows it: `global using System;`. Trimmed first
	// for that reason — trimming it after the keyword leaves the whole directive unread and
	// silently drops every namespace a project imports project-wide.
	code = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(code), "global "))
	rest := strings.TrimSpace(strings.TrimPrefix(code, "using "))
	rest = strings.TrimSpace(strings.TrimSuffix(rest, ";"))
	static := false
	if strings.HasPrefix(rest, "static ") {
		rest, static = strings.TrimSpace(rest[len("static "):]), true
	}
	alias := ""
	if i := strings.IndexByte(rest, '='); i > 0 {
		alias = strings.TrimSpace(rest[:i])
		rest = strings.TrimSpace(rest[i+1:])
	}
	if rest == "" {
		return Import{}, false
	}
	im := Import{Line: line, Alias: alias}
	if static {
		// The final segment is a type name, not part of the namespace.
		if i := strings.LastIndexByte(rest, '.'); i > 0 {
			im.Raw, im.Names = rest[:i], []string{rest[i+1:]}
			return im, true
		}
		return Import{}, false
	}
	im.Raw = rest
	return im, true
}

// csharpDeclSite reports whether a position can hold a type declaration.
//
// jvmDeclSite's top-level test is `depth == 0`, which is right for Java and for a
// file-scoped C# namespace. A braced namespace puts every declaration in the file at depth
// 1, and applying the Java rule there drops the whole file — no types, no members, no
// imports attributed to anything. nsDepth is what the namespace form decided.
func csharpDeclSite(types []jvmScope, depth, nsDepth int) bool {
	if len(types) == 0 {
		return depth == nsDepth
	}
	return jvmDirectMember(types, depth)
}

// csharpConstDecl reports the name of a const declared on a line.
//
// A `public const int MaxItems = 50;` is part of a type's contract in the way an ordinary
// field is not — it is a value callers write into their own code, and it cannot change
// without breaking them. That is the same reason Kotlin's `const val` and PHP's class
// constant are recorded, and it is why a plain field still is not: C# convention keeps
// fields private, and a private mutable field is an implementation detail.
//
// An enum's members are deliberately not recorded, matching what the Kotlin extractor does
// with `enum class Mode { FAST, SLOW }`: the type is the fact, and its cases belong to the
// page describing it rather than to the symbol index.
func csharpConstDecl(code string) (string, bool) {
	if !jvmHasModifier(code, "const") {
		return "", false
	}
	eq := strings.IndexByte(code, '=')
	if eq < 0 {
		return "", false
	}
	words := jvmWords(strings.TrimSpace(code[:eq]))
	if len(words) < 2 {
		// A const needs a type and a name in front of the value.
		return "", false
	}
	name := words[len(words)-1]
	if csharpIsModifier(name) || csharpIsTypeKeyword(name) {
		return "", false
	}
	for j := 0; j < len(name); j++ {
		if !identChar(name[j]) {
			return "", false
		}
	}
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		return "", false
	}
	return name, true
}

// csharpTypeDecl reports the keyword and name of a type declaration.
//
// C#'s six named types. `record struct` and `record class` spell one declaration with
// two keywords, which is why a second keyword is skipped rather than read as the name —
// the same shape Kotlin's `enum class` takes.
func csharpTypeDecl(code string) (kw, name string, ok bool) {
	words := jvmWords(code)
	for i, w := range words {
		if !csharpIsTypeKeyword(w) {
			if csharpIsModifier(w) {
				continue
			}
			return "", "", false
		}
		if w == "delegate" {
			// A delegate is the one type declaration with something between the keyword and
			// the name: its return type. `public delegate void OrderPlaced(Order order);`
			// names OrderPlaced, and taking the word after the keyword names the return type
			// — putting a type called `void` or `Task` on the page for every delegate in the
			// codebase. The name is the last word before the parameter list, which jvmWords
			// has already dropped.
			if n := csharpIdentAt(words, len(words)-1); n != "" && len(words) > i+1 {
				return w, n, true
			}
			return "", "", false
		}
		if n := csharpIdentAt(words, i+1); n != "" {
			return w, n, true
		}
		if i+1 < len(words) && csharpIsTypeKeyword(words[i+1]) {
			continue
		}
		return "", "", false
	}
	return "", "", false
}

// csharpIsTypeKeyword reports whether a token introduces a named type.
func csharpIsTypeKeyword(w string) bool {
	switch w {
	case "class", "interface", "struct", "enum", "record", "delegate":
		return true
	}
	return false
}

// csharpTypeKind maps a C# type keyword onto a SymbolKind.
func csharpTypeKind(kw string) SymbolKind {
	switch kw {
	case "interface":
		return SymInterface
	case "delegate":
		// A delegate names a function signature and holds no members, which is what
		// SymType means as against SymClass.
		return SymType
	}
	return SymClass
}

// csharpIsModifier reports whether a token is a declaration modifier.
//
// A separate list from jvmIsModifier rather than a shared one, because the overlap is
// partial and the differences are load-bearing: `internal` means assembly-visible in
// both C# and Kotlin, but `sealed` means the opposite of Java's, and `partial`,
// `unsafe`, `async` and `required` have no JVM counterpart. A modifier a language does
// not have cannot appear in its source, so a shared list would be harmless — but the
// C# list has to be complete or a declaration with an unrecognised modifier in front of
// it is silently dropped, and completeness is easier to see in a list that stands alone.
func csharpIsModifier(w string) bool {
	switch w {
	case "public", "private", "protected", "internal", "static", "readonly", "sealed",
		"abstract", "virtual", "override", "new", "partial", "async", "extern",
		"unsafe", "volatile", "const", "required", "file", "ref", "in", "out",
		"implicit", "explicit", "operator", "event", "fixed":
		return true
	}
	return false
}

// csharpIdentAt returns words[i] when it is a plain identifier that could be a
// declared name.
func csharpIdentAt(words []string, i int) string {
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
	if csharpIsTypeKeyword(w) || csharpIsModifier(w) {
		return ""
	}
	return w
}

// csharpMethodDecl reports the name of a method declared on a line.
//
// The same shape as javaMethodDecl and the same rejection rules, because the ambiguity
// is the same: C# has no keyword introducing a method, so `Compute(a, b);` and `void
// Compute(int a, int b)` differ only in what precedes the name.
//
// One C#-specific addition. An expression-bodied member — `public int Area => w * h;` —
// has no parenthesis at all when it is a property, which csharpPropertyDecl handles,
// and a method form `public int Add(int a) => a + 1;` which this admits normally.
func csharpMethodDecl(code string) (string, bool) {
	open := strings.IndexByte(code, '(')
	if open < 0 {
		return "", false
	}
	head := strings.TrimSpace(code[:open])
	if head == "" {
		return "", false
	}
	end := len(head)
	start := end
	for start > 0 && identChar(head[start-1]) {
		start--
	}
	name := head[start:end]
	if name == "" || (name[0] >= '0' && name[0] <= '9') || csharpControlKeyword(name) {
		return "", false
	}
	before := strings.TrimSpace(head[:start])
	if before == "" || strings.ContainsRune(before, '=') {
		return "", false
	}
	for _, w := range jvmWords(before) {
		switch w {
		case "new", "return", "throw", "yield", "await", "else", "case", "is", "as":
			return "", false
		}
	}
	// A return type, a type parameter list or a modifier is the last thing before a
	// method's name. An operator there means the name is being used, not declared.
	if strings.ContainsAny(before[len(before)-1:], "+-*/%<>!&|,.?:;") {
		// A generic method's type parameters leave a `>`, and a nullable return type
		// leaves a `?`: `Task<Order>? FindAsync(` is a declaration.
		if !strings.HasSuffix(before, ">") && !strings.HasSuffix(before, "?") {
			return "", false
		}
	}
	return name, true
}

// csharpControlKeyword reports whether a token is a statement keyword that takes a
// parenthesis.
func csharpControlKeyword(w string) bool {
	switch w {
	case "if", "for", "foreach", "while", "switch", "catch", "lock", "using", "return",
		"do", "else", "try", "throw", "new", "checked", "unchecked", "fixed", "sizeof",
		"typeof", "nameof", "await", "when", "base", "this":
		return true
	}
	return false
}

// csharpPropertyDecl reports the name of a property declared on a line.
//
// A property has no analogue in the languages this package already reads, and it is
// what a C# type mostly exposes: `public string Name { get; set; }` is the idiomatic
// spelling of what Java writes as a private field and a getter. An extractor that skips
// them reports every DTO and record in a codebase as having no members.
//
// The two forms:
//
//	public string Name { get; set; }      an accessor block
//	public int Area => Width * Height;    expression-bodied
//
// Told apart from a field — `private readonly ILogger _log;` — by requiring one of
// those two, because a field is not part of a type's contract in the way a property is
// and C# convention keeps fields private. And told apart from a method by having no
// parameter list before the brace, which is what the parenthesis check tests.
func csharpPropertyDecl(code string) (string, bool) {
	brace := strings.IndexByte(code, '{')
	arrow := strings.Index(code, "=>")
	var head string
	switch {
	case arrow >= 0 && (brace < 0 || arrow < brace):
		head = code[:arrow]
	case brace >= 0:
		// The accessor block must actually declare accessors. `public class Foo {` also
		// has a brace, and so does a method body.
		body := code[brace:]
		if !csharpHasAccessor(body) {
			return "", false
		}
		head = code[:brace]
	default:
		return "", false
	}
	head = strings.TrimSpace(head)
	// A parameter list means this is a method, not a property.
	if strings.ContainsAny(head, "()") {
		return "", false
	}
	words := jvmWords(head)
	if len(words) < 2 {
		// A property needs a type and a name. One word is a bare block or a label.
		return "", false
	}
	// An indexer — `public string this[int i]` — has no name of its own.
	name := words[len(words)-1]
	if name == "this" {
		return "", false
	}
	if csharpIsModifier(name) || csharpIsTypeKeyword(name) || csharpControlKeyword(name) {
		return "", false
	}
	for j := 0; j < len(name); j++ {
		if !identChar(name[j]) {
			return "", false
		}
	}
	if name[0] >= '0' && name[0] <= '9' {
		return "", false
	}
	return name, true
}

// csharpHasAccessor reports whether a brace-delimited body declares property
// accessors.
//
// Checked against a short prefix of the body rather than the whole of it, because a
// method whose body happens to call something named `get` would otherwise read as a
// property. An accessor is the first thing inside the brace.
func csharpHasAccessor(body string) bool {
	inner := strings.TrimSpace(strings.TrimPrefix(body, "{"))
	for _, a := range []string{"get;", "set;", "init;", "get =>", "set =>", "get{", "get {"} {
		if strings.HasPrefix(inner, a) {
			return true
		}
	}
	// An accessor may carry its own modifier: `{ get; private set; }`.
	for _, m := range []string{"private ", "protected ", "internal "} {
		if strings.HasPrefix(inner, m) {
			return csharpHasAccessor("{" + strings.TrimPrefix(inner, m))
		}
	}
	return false
}

// csharpTypeIsPublic reports whether a type declaration is public surface.
//
// A type with no modifier is `internal`: visible within its own assembly and nowhere
// else. That is not public surface, on the same grounds as Rust's `pub(crate)` — a
// caller outside the assembly cannot name it, so putting it on the page as public would
// be a false claim.
//
// A nested type is reachable only if every type enclosing it is, which is why the scope
// stack is consulted and not the line alone.
func csharpTypeIsPublic(code string, enclosing []jvmScope) bool {
	for _, s := range enclosing {
		if !s.exported {
			return false
		}
	}
	if len(enclosing) > 0 && enclosing[len(enclosing)-1].membersPublic {
		return !jvmHasModifier(code, "private")
	}
	return jvmHasModifier(code, "public")
}

// csharpMemberIsPublic reports whether a member declaration is public.
//
// A member with no modifier is private — a stricter default than C#'s type default and
// than Java's package-private. `protected` is deliberately not public: it is surface to
// a subclass, but an arbitrary caller cannot reach it.
func csharpMemberIsPublic(code string, owner jvmScope) bool {
	if owner.membersPublic {
		return !jvmHasModifier(code, "private")
	}
	return jvmHasModifier(code, "public")
}

// csharpOpensBody reports whether a type declaration has a body.
//
// The C# form jvmOpensBody does not know about is the positional record: `public record
// Money(decimal Amount);` declares a whole type on one line with no body at all, and a
// scope pushed for it never closes and claims every declaration after it. The semicolon
// is what says so, and it is checked before delegating.
func csharpOpensBody(lines []codeLine, tail string, last int) bool {
	if strings.Contains(tail, "{") {
		return true
	}
	if strings.Contains(tail, ";") {
		return false
	}
	const maxLook = 10
	for i, seen := last+1, 0; i < len(lines) && seen < maxLook; i++ {
		t := strings.TrimSpace(lines[i].Text)
		if t == "" {
			continue
		}
		seen++
		// The terminators are tested before the brace, for the reason jvmOpensBody gives:
		// a bodyless declaration must not borrow the next declaration's brace.
		if strings.HasPrefix(t, "}") || strings.HasPrefix(t, "[") {
			return false
		}
		if _, _, isType := csharpTypeDecl(t); isType {
			return false
		}
		if _, isMethod := csharpMethodDecl(t); isMethod {
			return false
		}
		brace := strings.IndexByte(t, '{')
		semi := strings.IndexByte(t, ';')
		if brace >= 0 && (semi < 0 || semi > brace) {
			return true
		}
		if semi >= 0 {
			return false
		}
	}
	return false
}

// csharpDoc reads the /// XML doc comment above a declaration.
//
// C# documents with a run of `///` lines holding XML rather than with a `/** */` block,
// so jvmDoc cannot read it. The prose lives inside a `<summary>` element, and the tags
// themselves are markup that must not reach the page: an unstripped `<summary>` on a
// bundle page reads as a bug in the extracted prose.
//
// Read from Raw, because the scanner strips comments.
func csharpDoc(lines []codeLine, idx int) string {
	const maxDoc = 200

	i := idx - 1
	// An attribute between the comment and the declaration is normal — `[Obsolete]` on a
	// documented method — and is skipped for the reason jvmDoc skips annotations.
	for i >= 0 && idx-i < maxDoc {
		t := strings.TrimSpace(lines[i].Raw)
		if t == "" || strings.HasPrefix(t, "[") {
			i--
			continue
		}
		break
	}
	var block []string
	for ; i >= 0 && idx-i < maxDoc; i-- {
		t := strings.TrimSpace(lines[i].Raw)
		if !strings.HasPrefix(t, "///") {
			break
		}
		block = append([]string{strings.TrimSpace(strings.TrimPrefix(t, "///"))}, block...)
	}
	if len(block) == 0 {
		// A `/** */` block is legal C# and some codebases use it.
		return jvmDoc(lines, idx)
	}
	text := strings.Join(block, " ")
	// The summary is the sentence; everything else is reference material. When there is
	// no summary element the whole run is prose, which is how a terse `/// Does X.`
	// comment is written.
	if s := csharpXMLElement(text, "summary"); s != "" {
		text = s
	} else {
		for _, tag := range []string{"<param", "<returns", "<exception", "<remarks", "<seealso"} {
			if j := strings.Index(text, tag); j >= 0 {
				text = text[:j]
			}
		}
	}
	return FirstSentence(csharpStripTags(text))
}

// csharpXMLElement returns the body of the named element, or "".
func csharpXMLElement(text, name string) string {
	open := "<" + name + ">"
	i := strings.Index(text, open)
	if i < 0 {
		return ""
	}
	rest := text[i+len(open):]
	if j := strings.Index(rest, "</"+name+">"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// csharpStripTags removes XML markup, leaving the prose inside it.
//
// A `<see cref="Foo"/>` reference carries a name worth keeping, so the cref's value is
// substituted for the tag rather than dropped with it — otherwise a sentence reading
// "see <see cref="Order"/> for details" loses the only noun in it.
func csharpStripTags(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			b.WriteByte(s[i])
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			// An unclosed angle bracket is prose, not markup.
			b.WriteString(s[i:])
			break
		}
		tag := s[i : i+end+1]
		if ref := csharpCref(tag); ref != "" {
			b.WriteString(ref)
		}
		i += end
	}
	return strings.TrimSpace(strings.Join(strings.Fields(b.String()), " "))
}

// csharpCref extracts the referenced name from a see or paramref tag.
func csharpCref(tag string) string {
	for _, attr := range []string{`cref="`, `name="`} {
		i := strings.Index(tag, attr)
		if i < 0 {
			continue
		}
		rest := tag[i+len(attr):]
		j := strings.IndexByte(rest, '"')
		if j < 0 {
			continue
		}
		ref := rest[:j]
		// A cref is qualified — `T:App.Order` or `M:App.Order.Total` — and the prose wants
		// the name, not the metadata prefix.
		if len(ref) > 2 && ref[1] == ':' {
			ref = ref[2:]
		}
		if k := strings.LastIndexByte(ref, '.'); k >= 0 {
			ref = ref[k+1:]
		}
		return ref
	}
	return ""
}
