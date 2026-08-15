package extract

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// JavaExtractor reads Java package declarations, imports and type surface.
//
// Line-oriented, not a parser (design §4.1). Java's import syntax is the least
// ambiguous of any language here — `import a.b.C;` is one statement on one line —
// and two other things about the language are what this extractor exists to get
// right:
//
//   - A file's own name is a fact it declares. `package com.example.api;` is the
//     name other files import, and nothing about the directory guarantees it: a
//     repository may root its sources at `src/main/java`, at `src/`, or wherever a
//     build file says. So Facts.Package carries the declared name verbatim and
//     resolution matches on that rather than on a path, which is what lets a JVM
//     import resolve in a tree whose build file signpost has not read.
//   - Visibility is a keyword whose *absence* means something. A member with no
//     modifier is package-private: reachable from its own package and nowhere else.
//     That is not public surface, so it is not Exported. Kotlin inverts this
//     default, and it is the one rule the two extractors must not share by accident
//     (see KotlinExtractor).
//
// A nested type is recorded under its own name rather than as `Outer.Inner`, and a
// method is attributed to the type it is declared in through Symbol.Recv — the same
// shape the Rust extractor's impl blocks produce.
type JavaExtractor struct{}

// Langs implements Extractor.
func (JavaExtractor) Langs() []discover.Lang { return []discover.Lang{discover.LangJava} }

// Extract implements Extractor.
func (JavaExtractor) Extract(f discover.File) (Facts, error) {
	facts := Facts{Path: f.Path, Lang: discover.LangJava}
	lines := scanLines(f.Content, scanJava)

	// The type stack: JVM sources nest types, and a member belongs to the innermost
	// one open at its line. A stack rather than a single name because an inner class
	// closing has to restore the outer one; losing that attributes the remainder of
	// the outer class's methods to a type that has ended.
	var types []jvmScope
	depth := 0

	for i := 0; i < len(lines); i++ {
		cl := lines[i]
		code := strings.TrimSpace(cl.Text)
		if code == "" {
			depth, types = jvmAdvance(depth, types, cl.Text)
			continue
		}
		// An annotation is metadata, not a declaration, and the common formatting
		// puts it on its own line above the thing it annotates. `@interface` is the
		// exception: it declares an annotation type.
		if strings.HasPrefix(code, "@") && !strings.Contains(code, "interface") {
			depth, types = jvmAdvance(depth, types, cl.Text)
			continue
		}

		switch {
		case strings.HasPrefix(code, "package "):
			// The declared name, which is this file's identity to every other file.
			facts.Package = strings.TrimSpace(strings.TrimSuffix(
				strings.TrimSpace(strings.TrimPrefix(code, "package ")), ";"))
			depth, types = jvmAdvance(depth, types, cl.Text)

		case strings.HasPrefix(code, "import "):
			if im, ok := javaImport(code, cl.Num); ok {
				facts.Imports = append(facts.Imports, im)
			}
			depth, types = jvmAdvance(depth, types, cl.Text)

		default:
			// A declaration wraps: a class with a long `implements` list, a method
			// signature broken across lines, a record's component list. Joining to the
			// point where parentheses balance is what puts the body's brace on the same
			// logical line as the name.
			joined, last := joinParens(lines, i)
			jcode := strings.TrimSpace(joined.Text)
			tail := joinedTail(lines, i, last)

			if kw, name, ok := jvmTypeDecl(jcode); ok && jvmDeclSite(types, depth) {
				sym := Symbol{
					Name: name, Kind: jvmTypeKind(kw),
					Exported: javaTypeIsPublic(jcode, types),
					Doc:      jvmDoc(lines, i), Line: cl.Num,
				}
				facts.Symbols = append(facts.Symbols, sym)
				if !jvmOpensBody(lines, tail, last) {
					// A type with no body declares no members, so no scope is pushed. One
					// pushed here would never reach a closing brace and would claim every
					// declaration in the rest of the file as its member.
					depth, types = jvmAdvance(depth, types, tail)
					i = last
					continue
				}
				types = append(types, jvmScope{
					name: name, depth: depth, exported: sym.Exported,
					// A brace on this line opens the body here; otherwise it opens on the
					// next line carrying code, and a scope popped before its brace arrives
					// takes every method in the type with it.
					opened: strings.Contains(tail, "{"),
					// An interface's members are public by definition and carry no modifier
					// saying so, and neither do a Java 8 default method or an interface's
					// nested type. Without this the one type whose whole purpose is to be a
					// public contract reports its entire surface as package-private.
					membersPublic: kw == "interface",
				})
			} else if name, ok := javaMethodDecl(jcode); ok && jvmDirectMember(types, depth) &&
				name != types[len(types)-1].name {
				// A constructor is skipped, which is a judgment about what a Symbol can
				// carry rather than about Java. Symbol has a name and a kind and no
				// signature, so `Service.Service` on a page repeats the type's own name
				// and says nothing further — the parameters, which are the interesting
				// part of a constructor, are not a fact this record can hold.
				owner := types[len(types)-1]
				facts.Symbols = append(facts.Symbols, Symbol{
					Name: name, Kind: SymMethod, Recv: owner.name,
					Exported: owner.exported && javaMemberIsPublic(jcode, owner),
					Doc:      jvmDoc(lines, i), Line: cl.Num,
				})
				// `public static void main(String[] args)` is where a JVM program starts,
				// and an instance method named main is an ordinary method.
				if name == "main" && javaIsMainSignature(jcode) {
					facts.Entrypoints = append(facts.Entrypoints, "main")
				}
			}

			depth, types = jvmAdvance(depth, types, tail)
			i = last
		}
	}
	facts.addQueries(sqlLiterals(lines))
	return facts, nil
}

// jvmScope is one open type declaration and the brace depth its body sits above.
type jvmScope struct {
	name string
	// depth is the brace depth of the line the type was declared on. Its members
	// are at depth+1.
	depth int
	// exported is whether the type is reachable from outside its package. A public
	// method of a package-private class is not public surface, and reporting it as
	// such puts a symbol on the page that nothing outside the package can call.
	exported bool
	// opened records that the body's opening brace has been seen. Until it has, the
	// scope must not be popped: `class Foo` with its brace on the next line sits at
	// the same depth as the code before it.
	opened bool
	// membersPublic marks a scope whose members need no modifier to be public: a
	// Java interface, or any Kotlin type.
	membersPublic bool
}

// jvmAdvance applies one line's brace movement and closes the scopes it left.
//
// Braces only. Parentheses and brackets are deliberately not counted, because a
// wrapped signature or an annotation's argument list would otherwise move a depth
// that is meant to track type and method bodies.
func jvmAdvance(depth int, types []jvmScope, text string) (int, []jvmScope) {
	depth += netDepth(text, "{", "}")
	if depth < 0 {
		depth = 0
	}
	for len(types) > 0 {
		t := &types[len(types)-1]
		if depth > t.depth {
			t.opened = true
			break
		}
		if !t.opened {
			break
		}
		types = types[:len(types)-1]
	}
	return depth, types
}

// jvmDirectMember reports whether depth is inside a type's own body rather than
// nested further inside a method, an initialiser, or a control-flow block.
//
// This is the strongest precision rule in either extractor. A statement in a method
// body has the same shape as a declaration — `compute(a, b);` and `void compute(int
// a, int b)` differ only in what precedes the name — and a body is always at least
// one brace deeper than the declarations it contains.
func jvmDirectMember(types []jvmScope, depth int) bool {
	return len(types) > 0 && depth == types[len(types)-1].depth+1
}

// jvmDeclSite reports whether a position can hold a declaration that is surface at
// all: the top level of a file, or a type's own body.
//
// A class declared inside a method body is legal in both languages and is reachable
// from nothing — recording it would put a type on a page that no caller can name.
func jvmDeclSite(types []jvmScope, depth int) bool {
	if len(types) == 0 {
		return depth == 0
	}
	return jvmDirectMember(types, depth)
}

// javaImport reads one import statement.
//
// The two forms carry the same fact and are not distinguished: `import
// java.util.List;` and `import static java.util.Arrays.asList;` both say this file
// depends on that package. What is worth separating is the package from the class,
// because the package is the name another file *declares* and so the only part
// resolution can match. Recording `com.example.util.Strings` as the dependency would
// point every import at a node no file will ever claim, and would split two imports
// of different classes in one package into two dependencies.
//
// The split uses the convention that package segments are lowercase and type names
// are capitalised, which is near-universal on the JVM and is the only signal a single
// file carries. Where a file does not follow it — a lowercase class name — the
// fallback drops the last segment, which is right for the common `a.b.c` case.
func javaImport(code string, line int) (Import, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(code, "import "))
	rest = strings.TrimSpace(strings.TrimSuffix(rest, ";"))
	if strings.HasPrefix(rest, "static ") {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "static "))
	}
	if rest == "" {
		return Import{}, false
	}
	// A wildcard names the package and no symbol.
	if strings.HasSuffix(rest, ".*") {
		pkg := strings.TrimSuffix(rest, ".*")
		if pkg == "" {
			return Import{}, false
		}
		return Import{Raw: pkg, Line: line}, true
	}
	pkg, sym := jvmSplitPackage(rest)
	if pkg == "" {
		return Import{}, false
	}
	im := Import{Raw: pkg, Line: line}
	if sym != "" {
		im.Names = []string{sym}
	}
	return im, true
}

// jvmSplitPackage splits a dotted name into its package and the outermost type
// named after it.
func jvmSplitPackage(name string) (pkg, sym string) {
	segs := strings.Split(name, ".")
	for i, s := range segs {
		if s == "" {
			continue
		}
		if s[0] >= 'A' && s[0] <= 'Z' {
			if i == 0 {
				// A capitalised first segment is a type in the default package.
				return "", s
			}
			return strings.Join(segs[:i], "."), s
		}
	}
	if len(segs) < 2 {
		// One segment, uncapitalised: a package imported whole, or the default
		// package. Either way there is nothing to split.
		return name, ""
	}
	return strings.Join(segs[:len(segs)-1], "."), segs[len(segs)-1]
}

// jvmTypeDecl reports the keyword and name of a type declaration on a line.
//
// Shared with Kotlin, which spells its keywords differently but puts the name in the
// same place: modifiers, then the keyword, then the identifier.
func jvmTypeDecl(code string) (kw, name string, ok bool) {
	words := jvmWords(code)
	for i, w := range words {
		if !jvmIsTypeKeyword(w) {
			// A modifier or an annotation may precede the keyword. Anything else means
			// this line is not a type declaration, and stopping at the first such token
			// rather than scanning on is what keeps `Map<String, Class> byName` from
			// reading as a class.
			if jvmIsModifier(w) || strings.HasPrefix(w, "@") {
				continue
			}
			return "", "", false
		}
		if n := jvmIdentAt(words, i+1); n != "" {
			return strings.TrimPrefix(w, "@"), n, true
		}
		// One keyword qualifying another: Kotlin's `enum class Foo` and
		// `annotation class Foo`. The one that names the type is the second.
		if i+1 < len(words) && jvmIsTypeKeyword(words[i+1]) {
			continue
		}
		// `companion object {` declares no name of its own; the Kotlin extractor
		// handles it, because the name it needs is the enclosing type's.
		return "", "", false
	}
	return "", "", false
}

// jvmIsTypeKeyword reports whether a token introduces a named type in either
// language.
func jvmIsTypeKeyword(w string) bool {
	switch w {
	case "class", "interface", "enum", "record", "object", "@interface":
		return true
	}
	return false
}

// jvmTypeKind maps a type keyword onto a SymbolKind.
func jvmTypeKind(kw string) SymbolKind {
	if kw == "interface" {
		return SymInterface
	}
	// A class, an enum, a record and a Kotlin object are all a named type that holds
	// members, which is the distinction SymClass draws against SymType.
	return SymClass
}

// javaMethodDecl reports the name of a method or constructor declared on a line.
//
// The shape is `<something> name(`, and the work is in rejecting the statements that
// share it. Three rules, each for a real form that appears in every Java file:
//
//   - The name may not stand alone. `doThing(x);` is a call, and a declaration
//     always has a return type or a modifier in front of the name. A constructor is
//     the exception the rule tolerates: `public Foo(int x)` keeps its modifier.
//   - No `new`, `return` or `throw` anywhere before the name. `private Map<K,V> m =
//     new HashMap<>();` is a field whose initialiser ends in a parenthesised call at
//     exactly the depth a method is declared at, so the depth rule cannot see it.
//   - No `=` before the name, for the same reason: an assignment's right-hand side
//     is not a declaration.
//
// A control-flow keyword taking a parenthesis — `if`, `while`, `switch`, `catch` —
// is rejected on the name itself.
func javaMethodDecl(code string) (string, bool) {
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
	if name == "" || (name[0] >= '0' && name[0] <= '9') || jvmControlKeyword(name) {
		return "", false
	}
	before := strings.TrimSpace(head[:start])
	if before == "" || strings.ContainsRune(before, '=') {
		return "", false
	}
	for _, w := range jvmWords(before) {
		switch w {
		case "new", "return", "throw", "yield", "assert", "else", "case", "instanceof":
			return "", false
		}
	}
	// A return type, a type parameter list or a modifier is the last thing before a
	// method's name. An operator there means the name is being used, not declared.
	if strings.ContainsAny(before[len(before)-1:], "+-*/%<>!&|,.?:;") {
		// `<T> T get(` ends in `>` once the type parameters are stripped, so the
		// generic method is admitted explicitly rather than by the character test.
		if !strings.HasSuffix(before, ">") {
			return "", false
		}
	}
	return name, true
}

// javaTypeIsPublic reports whether a Java type declaration is public surface.
//
// A type with no modifier is package-private, and a nested type is reachable only if
// every type enclosing it is — which is why the scope stack is consulted and not the
// line alone.
func javaTypeIsPublic(code string, enclosing []jvmScope) bool {
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

// javaMemberIsPublic reports whether a member declaration is public.
func javaMemberIsPublic(code string, owner jvmScope) bool {
	if owner.membersPublic {
		return !jvmHasModifier(code, "private")
	}
	return jvmHasModifier(code, "public")
}

// javaIsMainSignature reports whether a `main` declaration is the JVM entrypoint.
//
// `static` and a string-array parameter are what a launcher requires. The parameter
// is matched loosely, across `String[] args`, `String args[]` and `String...`,
// because the cost of being strict is losing the one fact that says where a program
// starts.
func javaIsMainSignature(code string) bool {
	if !jvmHasModifier(code, "static") {
		return false
	}
	open := strings.IndexByte(code, '(')
	if open < 0 {
		return false
	}
	params := code[open:]
	return strings.Contains(params, "String[]") || strings.Contains(params, "String ...") ||
		strings.Contains(params, "String...") || strings.Contains(params, "String[")
}

// jvmWords splits a declaration head into tokens, skipping anything inside
// brackets so that `Map<String, List<Integer>> byName` yields two tokens and not
// five, and stopping at the brace that opens a body.
func jvmWords(code string) []string {
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
		if identChar(c) || c == '@' || c == '.' {
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

// jvmIdentAt returns words[i] when it is a plain identifier that could be a
// declared name, else "".
//
// A modifier is deliberately *not* rejected here. Kotlin's are soft keywords — legal
// identifiers everywhere except the one position where they modify a declaration — so
// `val value: String` declares a property called value, `val data = f()` one called
// data, and rejecting them loses a real name. Only a keyword that introduces a
// declaration of its own is refused, because `enum class Mode` and `companion object`
// put one where a name would otherwise be.
func jvmIdentAt(words []string, i int) string {
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
	if jvmIsTypeKeyword(w) || jvmIsMemberKeyword(w) {
		return ""
	}
	return w
}

// jvmDottedIdentAt is jvmIdentAt admitting dots, for Kotlin's extension receiver:
// `fun String.shout()` puts `String.shout` where a plain name would be.
func jvmDottedIdentAt(words []string, i int) string {
	if i >= len(words) {
		return ""
	}
	w := words[i]
	if !strings.Contains(w, ".") {
		return jvmIdentAt(words, i)
	}
	for _, part := range strings.Split(w, ".") {
		if part == "" {
			return ""
		}
		if part[0] >= '0' && part[0] <= '9' {
			return ""
		}
		for j := 0; j < len(part); j++ {
			if !identChar(part[j]) {
				return ""
			}
		}
	}
	return w
}

// jvmOpensBody reports whether a type declaration has a body, so a scope should be
// pushed for it.
//
// Two real forms make this a lookahead rather than a test of one line:
//
//   - A body may open further down. A Java class with a long `implements` clause puts
//     its brace two or three lines below its name, and those lines balance their own
//     brackets so joinParens does not reach them. A scope not pushed there loses
//     every method in the type.
//   - A type may have no body at all. `data class Point(val x: Int)` and `object
//     Empty : Result()` declare no members and never reach a closing brace of their
//     own, so a scope pushed for one stays open for the rest of the file and claims
//     everything after it as a member. Kotlin is full of these.
//
// The scan therefore looks forward for a brace, and stops at the first thing that
// means the declaration has already ended: a semicolon, a closing brace, or another
// declaration.
func jvmOpensBody(lines []codeLine, tail string, last int) bool {
	if strings.Contains(tail, "{") {
		return true
	}
	const maxLook = 10
	for i, seen := last+1, 0; i < len(lines) && seen < maxLook; i++ {
		t := strings.TrimSpace(lines[i].Text)
		if t == "" {
			continue
		}
		seen++
		// The terminators are tested before the brace, because the next declaration
		// usually carries one. `data class Point(val x: Int)` followed by `class Logger {`
		// would otherwise borrow Logger's brace, push a scope for Point, and file
		// everything after it — Logger included — under Point.
		if strings.HasPrefix(t, "}") || strings.HasPrefix(t, "@") {
			return false
		}
		if _, _, isType := jvmTypeDecl(t); isType {
			return false
		}
		if _, _, isMember := kotlinMemberDecl(t); isMember {
			return false
		}
		if _, isMethod := javaMethodDecl(t); isMethod {
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

// jvmIsMemberKeyword reports whether a token introduces a member rather than a type.
// Listed so it can never be mistaken for a declared name.
func jvmIsMemberKeyword(w string) bool {
	switch w {
	case "fun", "val", "var", "typealias", "init", "constructor":
		return true
	}
	return false
}

// jvmIsModifier reports whether a token is a declaration modifier in either
// language. One list for both: a modifier a language does not have cannot appear in
// its source, so sharing costs nothing.
func jvmIsModifier(w string) bool {
	switch w {
	// Java.
	case "public", "protected", "private", "static", "final", "abstract",
		"synchronized", "native", "transient", "volatile", "strictfp", "default",
		"sealed", "non-sealed":
		return true
	// Kotlin. `internal` is module-visible and deliberately not public surface, on
	// the same grounds as Rust's `pub(crate)`.
	case "internal", "open", "override", "lateinit", "inline", "noinline", "crossinline",
		"reified", "suspend", "operator", "infix", "external", "tailrec", "const",
		"annotation", "data", "inner", "value", "expect", "actual", "vararg",
		"companion", "out", "in":
		return true
	}
	return false
}

// jvmHasModifier reports whether a declaration carries a modifier keyword.
//
// Matched against tokens rather than by substring: a parameter named `publicId` and
// a type named `StaticFactory` both contain a modifier's letters.
func jvmHasModifier(code, mod string) bool {
	for _, w := range jvmWords(code) {
		if w == mod {
			return true
		}
	}
	return false
}

// jvmControlKeyword reports whether a token is a statement keyword that takes a
// parenthesis, which is the shape a declaration has to be told apart from.
func jvmControlKeyword(w string) bool {
	switch w {
	case "if", "for", "while", "switch", "catch", "synchronized", "return", "do",
		"else", "try", "throw", "new", "assert", "when", "super", "this", "yield":
		return true
	}
	return false
}

// jvmDoc reads the /** */ Javadoc or KDoc block above a declaration.
//
// Read from Raw, because the scanner strips comments. Blank lines and annotations
// between the comment and the declaration are normal and skipped, for the same
// reason rustDoc skips attributes: a Spring or JUnit codebase puts an annotation on
// nearly every documented method.
func jvmDoc(lines []codeLine, idx int) string {
	// A cap, so a file with a stray `*/` and no opening delimiter does not walk to
	// its start once per declaration.
	const maxDoc = 200

	i := idx - 1
	for i >= 0 && idx-i < maxDoc {
		t := strings.TrimSpace(lines[i].Raw)
		if t == "" || strings.HasPrefix(t, "@") {
			i--
			continue
		}
		break
	}
	if i < 0 || !strings.HasSuffix(strings.TrimSpace(lines[i].Raw), "*/") {
		return ""
	}
	var block []string
	for stop := i - maxDoc; i >= 0 && i > stop; i-- {
		t := strings.TrimSpace(lines[i].Raw)
		if strings.HasPrefix(t, "/*") && !strings.HasPrefix(t, "/**") {
			// A plain block comment carries no prose meant as documentation.
			return ""
		}
		block = append([]string{jvmDocLine(t)}, block...)
		if strings.HasPrefix(t, "/**") {
			break
		}
		if i == 0 {
			return ""
		}
	}
	text := strings.TrimSpace(strings.Join(block, " "))
	// A Javadoc tag block is reference material rather than a summary sentence.
	for _, tag := range []string{"@param", "@return", "@throws", "@see"} {
		if j := strings.Index(text, tag); j >= 0 {
			text = text[:j]
		}
	}
	return FirstSentence(text)
}

// jvmDocLine strips a doc comment's delimiters and leading asterisk from one line.
func jvmDocLine(t string) string {
	t = strings.TrimPrefix(t, "/**")
	t = strings.TrimSuffix(t, "*/")
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "*")
	return strings.TrimSpace(t)
}
