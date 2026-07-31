package extract

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// RustExtractor reads Rust imports and top-level items.
//
// Line-oriented, not a parser (design §4.1). Rust is the language where that
// choice costs the most, because two of its constructs carry real meaning that a
// line matcher has to reconstruct:
//
//   - `use` is not an import in the sense the other languages mean. It is a name
//     binding into scope, and the module it names may be another crate, this
//     crate (`crate::`), the parent module (`super::`), or this module (`self::`).
//     The distinction is what tells an internal edge from an external dependency,
//     so it is recorded rather than flattened.
//   - `impl` blocks hold the methods, and the type they belong to is on the impl
//     line, not the method line. Losing the association would file every method
//     under no owner at all.
//
// Visibility is `pub`, with `pub(crate)` and `pub(super)` deliberately treated as
// not exported: they are visible within the crate but are not the crate's public
// API, and the bundle's "public surface" claim would be wrong if it included them.
type RustExtractor struct{}

// Langs implements Extractor.
func (RustExtractor) Langs() []discover.Lang { return []discover.Lang{discover.LangRust} }

// Extract implements Extractor.
func (RustExtractor) Extract(f discover.File) (Facts, error) {
	facts := Facts{Path: f.Path, Lang: discover.LangRust}
	facts.Package = rustModulePath(f.Path)

	lines := scanLines(f.Content, scanRust)

	depth := 0
	// The impl block currently open, and the depth at which it closes. Methods
	// inside it belong to implType.
	implType := ""
	implDepth := -1
	// A trait block behaves the same way for attribution purposes: its methods
	// are the trait's declared surface.
	inTrait := false

	for i := 0; i < len(lines); i++ {
		cl := lines[i]
		code := strings.TrimSpace(cl.Text)
		if code == "" {
			continue
		}
		// An attribute or derive is metadata, not an item.
		if strings.HasPrefix(code, "#") {
			depth += netDepth(cl.Text, "([{", ")]}")
			continue
		}

		topLevel := depth == 0
		inImpl := implDepth >= 0 && depth > implDepth

		switch {
		// rustIsItem rather than a prefix match on "use " / "pub use ", because a
		// re-export can carry a restricted visibility: `pub(super) use wire::{A, B}`
		// and `pub(crate) use x::Y` are imports, and a prefix match misses both.
		case rustIsItem(code, "use"):
			joined, last := joinBraces(lines, i)
			facts.Imports = append(facts.Imports, rustUses(joined)...)
			depth += netDepth(joinedTail(lines, i, last), "([{", ")]}")
			i = last
			continue

		case strings.HasPrefix(code, "extern crate "):
			// The 2015-edition dependency declaration. Rare now, but a repo old
			// enough to use it is exactly the repo an agent most needs a map of.
			if name := identAfter(code, "extern crate "); name != "" {
				facts.Imports = append(facts.Imports, Import{Raw: name, External: true, Line: cl.Num})
			}

		case topLevel && rustIsItem(code, "mod"):
			name := rustItemName(code, "mod")
			if name == "" {
				break
			}
			// A module declaration is both facts at once, and dropping either one
			// loses something real. `mod x;` pulls in another file, so it is a
			// dependency edge — without it the crate root looks like it depends on
			// nothing local. It also declares the name `x`, and `pub mod x` puts that
			// name in the crate's public API, which is how a caller reaches
			// `crate::x::Thing`. `mod x { }` is the same declaration with the body
			// inline, so it is a symbol without the edge.
			if strings.HasSuffix(code, ";") {
				facts.Imports = append(facts.Imports, Import{
					Raw: "self::" + name, Line: cl.Num,
				})
			}
			facts.Symbols = append(facts.Symbols, Symbol{
				Name: name, Kind: SymType, Exported: rustIsPub(code),
				Doc: rustDoc(lines, i), Line: cl.Num,
			})

		case strings.HasPrefix(code, "impl") && rustImplWord(code):
			implType = rustImplTarget(code)
			implDepth = depth
			inTrait = false

		case rustIsItem(code, "trait"):
			if name := rustItemName(code, "trait"); name != "" {
				if topLevel {
					facts.Symbols = append(facts.Symbols, Symbol{
						Name: name, Kind: SymInterface, Exported: rustIsPub(code),
						Doc: rustDoc(lines, i), Line: cl.Num,
					})
				}
				implType = name
				implDepth = depth
				inTrait = true
			}

		case rustIsItem(code, "fn"):
			name := rustItemName(code, "fn")
			if name == "" {
				break
			}
			s := Symbol{
				Name: name, Kind: SymFunc, Exported: rustIsPub(code),
				Doc: rustDoc(lines, i), Line: cl.Num,
			}
			switch {
			case topLevel:
				facts.Symbols = append(facts.Symbols, s)
				// `fn main` is the binary entrypoint. Only at the top level of a
				// crate root — a `fn main` inside a module is an ordinary function.
				if name == "main" && rustIsCrateRoot(f.Path) {
					facts.Entrypoints = append(facts.Entrypoints, "main")
				}
			case inImpl && implType != "":
				s.Kind = SymMethod
				s.Recv = implType
				// A trait's methods are part of the trait's surface, so they follow
				// the trait's own visibility rather than needing `pub` themselves —
				// which they never carry, since it is not allowed there.
				if inTrait {
					s.Exported = true
				}
				facts.Symbols = append(facts.Symbols, s)
			}
			// Anything else is a nested fn: unreachable from outside, not surface.

		case topLevel && (rustIsItem(code, "struct") || rustIsItem(code, "enum") ||
			rustIsItem(code, "union") || rustIsItem(code, "type")):
			kw := rustKeyword(code)
			if name := rustItemName(code, kw); name != "" {
				facts.Symbols = append(facts.Symbols, Symbol{
					Name: name, Kind: SymType, Exported: rustIsPub(code),
					Doc: rustDoc(lines, i), Line: cl.Num,
				})
			}

		case topLevel && (rustIsItem(code, "const") || rustIsItem(code, "static")):
			kw := rustKeyword(code)
			// `static mut` and `const fn` both put a keyword where the name goes.
			rest := strings.TrimSpace(strings.TrimPrefix(rustStripVis(code), kw))
			rest = strings.TrimPrefix(rest, "mut ")
			if strings.HasPrefix(rest, "fn ") {
				// A const fn is a function, handled by the fn case on its own terms.
				break
			}
			if name := identAfter("x "+rest, "x "); name != "" {
				facts.Symbols = append(facts.Symbols, Symbol{
					Name: name, Kind: SymConst, Exported: rustIsPub(code),
					Doc: rustDoc(lines, i), Line: cl.Num,
				})
			}
		}

		depth += netDepth(cl.Text, "([{", ")]}")
		if depth < 0 {
			depth = 0
		}
		// Closing out of the impl or trait block.
		if implDepth >= 0 && depth <= implDepth {
			implType = ""
			implDepth = -1
			inTrait = false
		}
	}
	return facts, nil
}

// rustModulePath turns a file path into a module path.
//
// src/lib.rs and src/main.rs are the crate root, so they have no module path of
// their own. src/foo/mod.rs and src/foo.rs are both module foo — the two layouts
// are equivalent to a caller, and treating them differently would split one module
// across two nodes.
func rustModulePath(path string) string {
	p := strings.TrimSuffix(strings.ReplaceAll(path, "\\", "/"), ".rs")
	p = strings.TrimPrefix(p, "src/")
	switch p {
	case "lib", "main":
		return ""
	}
	p = strings.TrimSuffix(p, "/mod")
	// A binary under src/bin is its own crate root.
	if strings.HasPrefix(p, "bin/") {
		return ""
	}
	if p == "" {
		return ""
	}
	return strings.ReplaceAll(p, "/", "::")
}

// rustIsCrateRoot reports whether a path holds a crate's entrypoint, which is
// where `fn main` means something.
func rustIsCrateRoot(path string) bool {
	p := strings.ReplaceAll(path, "\\", "/")
	base := p
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		base = p[i+1:]
	}
	if base == "main.rs" {
		return true
	}
	// Any file directly under a bin/ or examples/ directory is its own binary.
	return strings.Contains(p, "/bin/") || strings.HasPrefix(p, "bin/") ||
		strings.Contains(p, "/examples/") || strings.HasPrefix(p, "examples/")
}

// rustUses parses a use declaration into one import per bound path.
//
// The nested-brace form is the reason this is not a one-liner: a single statement
// can bind an arbitrary tree of paths, and each leaf is a distinct dependency.
//
//	use std::collections::{HashMap, BTreeMap};
//	use crate::graph::{node::Node, edge::{Edge, Kind}};
//	use serde::{Serialize as Ser, Deserialize};
//	use super::helper;
//	use std::io::*;
func rustUses(cl codeLine) []Import {
	code := strings.TrimSpace(cl.Text)
	code = strings.TrimSuffix(code, ";")
	code = strings.TrimSpace(strings.TrimPrefix(rustStripVis(code), "use"))
	if code == "" {
		return nil
	}
	var out []Import
	for _, leaf := range rustExpandUse(code) {
		path, alias := splitRustAs(leaf)
		path = strings.TrimSpace(path)
		// `self` as a leaf means the parent path itself: `use a::b::{self, c}`
		// binds a::b and a::b::c.
		path = strings.TrimSuffix(path, "::self")
		if path == "" || path == "*" {
			continue
		}
		im := Import{Raw: path, Alias: alias, Line: cl.Num}
		// A glob binds no specific name, so there is nothing to record beyond the
		// module it came from.
		im.Raw = strings.TrimSuffix(im.Raw, "::*")
		im.External = rustIsExternal(im.Raw)
		out = append(out, im)
	}
	return out
}

// rustExpandUse flattens a use tree into its leaf paths.
func rustExpandUse(path string) []string {
	open := strings.IndexByte(path, '{')
	if open < 0 {
		return []string{path}
	}
	close := matchBrace(path, open)
	if close < 0 {
		// Malformed; treat the prefix as the whole path rather than inventing
		// leaves that were never written.
		return []string{strings.TrimSuffix(strings.TrimSpace(path[:open]), "::")}
	}
	prefix := path[:open]
	var out []string
	for _, part := range splitTopLevel(path[open+1:close], ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// Recurse: a brace group can contain another.
		out = append(out, rustExpandUse(prefix+part)...)
	}
	// Anything after the closing brace is not valid in a use tree, so it is
	// ignored rather than guessed at.
	return out
}

// matchBrace returns the index of the brace closing the one at open.
func matchBrace(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// rustIsExternal reports whether a use path leaves the crate.
//
// crate::, self::, and super:: are explicitly internal. Everything else is
// another crate — including std, which is external in the sense that matters here:
// it is not code in this repository.
func rustIsExternal(path string) bool {
	for _, p := range []string{"crate::", "self::", "super::"} {
		if strings.HasPrefix(path, p) {
			return false
		}
	}
	switch path {
	case "crate", "self", "super":
		return false
	}
	return true
}

// splitRustAs splits "path as alias".
func splitRustAs(s string) (path, alias string) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " as "); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(" as "):])
	}
	return s, ""
}

// rustIsItem reports whether a line declares an item of the given keyword,
// allowing for the visibility and modifier words that may precede it.
func rustIsItem(code, kw string) bool {
	return rustKeyword(code) == kw
}

// rustKeyword returns the item keyword on a line, after stripping visibility and
// the modifiers that can precede it. Returns "" when the line declares no item.
//
// The modifier list matters for correctness, not completeness: `pub async unsafe
// extern "C" fn` is all one declaration, and stopping at the first unrecognised
// word would classify it as no item at all.
func rustKeyword(code string) string {
	code = rustStripVis(code)
	for {
		w := firstWord(code)
		switch w {
		case "async", "unsafe", "const", "extern", "default", "move":
			rest := rustTrimABI(strings.TrimSpace(code[len(w):]))
			// `const` is both a modifier (`const fn`) and an item keyword
			// (`const X: u8`). It is a modifier when what follows resolves to a
			// function, which is not the same as `fn` following immediately:
			// `const unsafe fn` and `const extern "C" fn` are both const functions,
			// and testing only for a literal `fn` prefix classifies them as a const
			// item named `unsafe` or `extern` — a symbol the file never declared.
			if w == "const" && rustKeyword(rest) != "fn" {
				return "const"
			}
			code = rest
			continue
		}
		switch w {
		case "fn", "struct", "enum", "union", "trait", "type", "mod", "static", "impl", "use", "macro_rules":
			return w
		}
		return ""
	}
}

// rustTrimABI removes the ABI string of an `extern "C" fn`.
//
// The scanner preserves a string's delimiters and blanks only its body, so `"C"`
// arrives as a quote, a space, and a quote. Trimming a literal `""` would miss it
// and leave the walk stranded on a quote character, which reads as "no item here"
// and drops the whole declaration.
func rustTrimABI(code string) string {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, `"`) {
		return code
	}
	if end := strings.IndexByte(code[1:], '"'); end >= 0 {
		return strings.TrimSpace(code[end+2:])
	}
	return code
}

// rustStripVis removes a visibility modifier, including its restriction clause.
func rustStripVis(code string) string {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, "pub") {
		return code
	}
	rest := code[len("pub"):]
	// pub(crate), pub(super), pub(in path)
	if strings.HasPrefix(rest, "(") {
		if c := strings.IndexByte(rest, ')'); c >= 0 {
			rest = rest[c+1:]
		}
	}
	return strings.TrimSpace(rest)
}

// rustIsPub reports whether an item is part of the crate's public API.
//
// pub(crate) and pub(super) are excluded deliberately. They are visible inside
// the crate but are not its public surface, and a bundle that listed them as
// public API would be telling a consumer they can depend on something they cannot.
func rustIsPub(code string) bool {
	code = strings.TrimSpace(code)
	if !strings.HasPrefix(code, "pub") {
		return false
	}
	rest := code[len("pub"):]
	if rest == "" {
		return true
	}
	// Restricted visibility.
	if rest[0] == '(' {
		return false
	}
	// `pub` must be a whole word: `public_thing` is not a visibility modifier.
	return rest[0] == ' ' || rest[0] == '\t'
}

// rustItemName returns the identifier declared by an item.
func rustItemName(code, kw string) string {
	if kw == "" {
		return ""
	}
	code = rustStripVis(code)
	// Walk past the modifiers rustKeyword already accounted for.
	for {
		w := firstWord(code)
		if w == kw {
			break
		}
		switch w {
		case "async", "unsafe", "const", "extern", "default", "move":
			code = rustTrimABI(strings.TrimSpace(code[len(w):]))
			continue
		}
		return ""
	}
	rest := strings.TrimSpace(code[len(kw):])
	end := 0
	for end < len(rest) && identChar(rest[end]) {
		end++
	}
	name := rest[:end]
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		return ""
	}
	return name
}

// rustImplWord guards against a line that merely starts with the letters "impl",
// such as an `implementation` identifier or a `impl Trait` return type mid-line.
func rustImplWord(code string) bool {
	rest := code[len("impl"):]
	return rest == "" || rest[0] == ' ' || rest[0] == '<' || rest[0] == '\t'
}

// rustImplTarget extracts the type an impl block is for.
//
//	impl Client                       -> Client
//	impl<T> List<T>                   -> List
//	impl Display for Client           -> Client
//	impl<'a> Iterator for Iter<'a>     -> Iter
//
// The `for` form is the one that matters: the methods belong to the type after
// `for`, not to the trait before it. Attributing them to the trait would file
// every trait implementation in the repo under the trait's own node.
func rustImplTarget(code string) string {
	code = strings.TrimSpace(strings.TrimPrefix(code, "impl"))
	// Drop the generic parameter list on the impl itself.
	if strings.HasPrefix(code, "<") {
		if c := matchAngle(code); c >= 0 {
			code = strings.TrimSpace(code[c+1:])
		}
	}
	// `for` separates the trait from the implementing type.
	if i := indexWord(code, "for"); i >= 0 {
		code = strings.TrimSpace(code[i+len("for"):])
	}
	// Stop at the type's own generics, a where clause, or the body.
	for _, cut := range []string{"<", " where", "{", "("} {
		if i := strings.Index(code, cut); i >= 0 {
			code = code[:i]
		}
	}
	code = strings.TrimSpace(code)
	// A qualified path names the type at its end: `crate::graph::Node` -> Node.
	if i := strings.LastIndex(code, "::"); i >= 0 {
		code = code[i+2:]
	}
	// A reference impl (`impl Trait for &T`) still targets T.
	code = strings.TrimLeft(code, "&*")
	code = strings.TrimSpace(strings.TrimPrefix(code, "mut "))
	if !isRustIdent(code) {
		return ""
	}
	return code
}

// matchAngle returns the index of the '>' closing the '<' at s[0], accounting for
// nesting so `<T, U<V>>` is measured correctly.
func matchAngle(s string) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			depth++
		case '>':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// indexWord finds word as a whole word, so the `for` in `formatter` does not
// match.
func indexWord(s, word string) int {
	for i := 0; i+len(word) <= len(s); i++ {
		if !strings.HasPrefix(s[i:], word) {
			continue
		}
		if i > 0 && identChar(s[i-1]) {
			continue
		}
		if i+len(word) < len(s) && identChar(s[i+len(word)]) {
			continue
		}
		return i
	}
	return -1
}

// rustDoc reads the /// or //! doc comment block above an item at line index idx.
//
// Read from Raw because the scanner strips comments. A #[derive] or other
// attribute between the doc and the item is normal and must be skipped, or every
// documented struct in a serde-using codebase would come back with no doc.
func rustDoc(lines []codeLine, idx int) string {
	i := idx - 1
	// Skip attributes and blank lines between the comment and the item.
	for i >= 0 {
		t := strings.TrimSpace(lines[i].Raw)
		if t == "" || strings.HasPrefix(t, "#[") || strings.HasPrefix(t, "#!") {
			i--
			continue
		}
		break
	}
	// Collect the contiguous doc-comment block, which reads downward.
	var block []string
	for i >= 0 {
		t := strings.TrimSpace(lines[i].Raw)
		if !strings.HasPrefix(t, "///") && !strings.HasPrefix(t, "//!") {
			break
		}
		line := strings.TrimSpace(t[3:])
		block = append([]string{line}, block...)
		i--
	}
	if len(block) == 0 {
		return ""
	}
	return FirstSentence(strings.Join(block, " "))
}

// firstWord returns the leading identifier-ish token of s.
func firstWord(s string) string {
	s = strings.TrimLeft(s, " \t")
	end := 0
	for end < len(s) && (identChar(s[end]) || s[end] == '!') {
		end++
	}
	return s[:end]
}

// isRustIdent reports whether s is a plain identifier.
func isRustIdent(s string) bool {
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
