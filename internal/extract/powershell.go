package extract

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// PowerShellExtractor reads what a PowerShell script imports and what it defines.
//
// Line-oriented, not a parser (design §4.1). This is a separate extractor from the
// shell's rather than a mode of it, and the reason is that the two share exactly one
// thing — `#` as the line comment. Everything an extractor reads is different: the
// function syntax, the import syntax, the resolution rules, and the scoping. That is the
// inverse of the C-family decision (ADR 0022), where one extractor reads C, C++ and
// Objective-C because they share a preprocessor and a header convention, so the language
// boundary is not one an extractor can see. Here there is no shared mechanism to bend.
//
// Three PowerShell facts shape what follows:
//
//   - A nested function is *not* global. `function Outer { function Inner { } }` defines
//     Inner in Outer's scope, and it disappears when Outer returns — the exact opposite
//     of the shell, where a nested definition is global once the enclosing function has
//     run (see ShellExtractor). So a declaration site rule is required here and is
//     meaningless there, which is the clearest single reason these are two extractors.
//   - Export-ModuleMember is authoritative when present. A module that calls it exports
//     what it names and nothing else, so a function absent from the list is private no
//     matter how it is spelled. That makes it the one export statement in this package
//     that has to be read *before* exportedness can be decided for any symbol, which is
//     why Extract works in two passes.
//   - `using` and `Import-Module` are unrelated dependencies. `using namespace
//     System.Text` names a .NET namespace, `Import-Module Pester` names a PowerShell
//     module from a gallery or the module path, and `. "$PSScriptRoot/lib.ps1"` names a
//     file in this repository. All three are imports; only the third can ever resolve to
//     a node in the graph.
type PowerShellExtractor struct{}

// Langs implements Extractor.
func (PowerShellExtractor) Langs() []discover.Lang {
	return []discover.Lang{discover.LangPowerShell}
}

// Extract implements Extractor.
func (PowerShellExtractor) Extract(f discover.File) (Facts, error) {
	facts := Facts{Path: f.Path, Lang: discover.LangPowerShell}
	lines := scanLines(f.Content, scanPowerShell)

	// Pass one: the module's own statement about what it exports. It can appear anywhere
	// in the file and conventionally appears at the bottom, so exportedness cannot be
	// decided while walking declarations — every symbol would be judged before the
	// authority for the judgement had been read.
	exports, hasExports := psExportedNames(lines)

	if strings.HasPrefix(f.Content, "#!") {
		facts.Entrypoints = append(facts.Entrypoints, "#!")
	}

	var types []jvmScope
	depth := 0

	for i := 0; i < len(lines); i++ {
		cl := lines[i]
		code := strings.TrimSpace(cl.Text)

		// A `#Requires` statement is a comment to the scanner and a dependency declaration
		// to PowerShell — the one place in this package where a comment carries a fact, so
		// it is read from Raw before the blanked line is looked at.
		if ims, ok := psRequires(cl); ok {
			facts.Imports = append(facts.Imports, ims...)
			continue
		}
		if code == "" {
			depth, types = jvmAdvance(depth, types, cl.Text)
			continue
		}
		// An attribute is metadata above a declaration, as it is in C#. `[CmdletBinding()]`
		// sits between a function's doc comment and its param block, and `[Parameter()]`
		// above each parameter. Its brackets are not braces, so only the guard is needed.
		if strings.HasPrefix(code, "[") && !psIsClassMemberSite(types, depth) {
			depth, types = jvmAdvance(depth, types, cl.Text)
			continue
		}

		switch {
		case psIsUsing(code):
			if im, ok := psUsing(code, cl.Num); ok {
				facts.Imports = append(facts.Imports, im)
			}
			depth, types = jvmAdvance(depth, types, cl.Text)

		case psIsImportModule(code):
			if im, ok := psImportModule(cl); ok {
				facts.Imports = append(facts.Imports, im)
			}
			depth, types = jvmAdvance(depth, types, cl.Text)

		case psIsDotSource(code):
			if im, ok := psDotSource(cl); ok {
				facts.Imports = append(facts.Imports, im)
			}
			depth, types = jvmAdvance(depth, types, cl.Text)

		default:
			// A top-level param block is what makes a script a command rather than a
			// library: it is how a `.ps1` declares the arguments it is invoked with, and
			// PowerShell requires it to be the first statement in the body it belongs to.
			// At depth 0 that body is the script itself.
			if depth == 0 && len(types) == 0 && psIsParamBlock(code) {
				facts.Entrypoints = append(facts.Entrypoints, "param")
				depth, types = jvmAdvance(depth, types, cl.Text)
				continue
			}

			if kw, name, ok := psTypeDecl(code); ok && depth == 0 && len(types) == 0 {
				sym := Symbol{
					Name: name, Kind: psTypeKind(kw),
					// A class or enum is not a module member and Export-ModuleMember cannot
					// name one — the parameter takes functions, cmdlets, variables and
					// aliases. A `using module` in the consumer is what makes a class
					// visible, so a declared type is surface whenever the file is imported
					// at all.
					Exported: !strings.HasPrefix(name, "_"),
					Doc:      psDoc(lines, i), Line: cl.Num,
				}
				facts.Symbols = append(facts.Symbols, sym)
				if kw == "enum" {
					// An enum's members are not recorded, for the reason the C# extractor
					// gives: the type is the fact and its cases belong to the page
					// describing it. But its braces still move the depth, and a scope is
					// pushed so a member-shaped line inside it is not read as one.
					types = append(types, jvmScope{
						name: name, depth: depth, exported: sym.Exported,
						opened: strings.Contains(cl.Text, "{"),
					})
					depth, types = jvmAdvance(depth, types, cl.Text)
					continue
				}
				types = append(types, jvmScope{
					name: name, depth: depth, exported: sym.Exported,
					opened: strings.Contains(cl.Text, "{"),
					// A class member is public unless it says `hidden`, which is the
					// language's only visibility keyword and the inverse of C#'s default.
					membersPublic: true,
				})
				depth, types = jvmAdvance(depth, types, cl.Text)
				continue
			}

			if name, ok := psFuncDecl(code); ok && depth == 0 && len(types) == 0 {
				facts.Symbols = append(facts.Symbols, Symbol{
					Name: name, Kind: SymFunc,
					Exported: psFuncExported(name, exports, hasExports),
					Doc:      psFuncDoc(lines, i), Line: cl.Num,
				})
				depth, types = jvmAdvance(depth, types, cl.Text)
				continue
			}

			if psIsClassMemberSite(types, depth) {
				owner := types[len(types)-1]
				if name, ok := psMethodDecl(code); ok && name != owner.name {
					// A constructor is skipped for the reason javaMethodDecl gives: Symbol
					// carries no signature, so `Widget.Widget` repeats the type's name.
					facts.Symbols = append(facts.Symbols, Symbol{
						Name: name, Kind: SymMethod, Recv: owner.name,
						Exported: owner.exported && !psIsHidden(code),
						Doc:      psDoc(lines, i), Line: cl.Num,
					})
				} else if name, ok := psPropertyDecl(code); ok {
					facts.Symbols = append(facts.Symbols, Symbol{
						// A PowerShell class property is a field — `[string]$Name` with no
						// accessors — so SymVar states what it is. The C# extractor records
						// its properties as SymMethod because a C# property *is* accessor
						// code; the two languages spell different things with the same word.
						Name: name, Kind: SymVar, Recv: owner.name,
						Exported: owner.exported && !psIsHidden(code),
						Doc:      psDoc(lines, i), Line: cl.Num,
					})
				}
			}

			depth, types = jvmAdvance(depth, types, cl.Text)
		}
	}
	facts.addQueries(sqlLiterals(lines))
	return facts, nil
}

// psExportedNames reads the names an Export-ModuleMember call exports.
//
// The second return value distinguishes "exports nothing" from "does not say", and the
// difference is the whole point of reading this. A module with no Export-ModuleMember
// exports every function it defines, so absence means *everything*; a module that calls it
// exports only what is listed, so a name's absence from the list means private. Collapsing
// the two would report a module's internals as its public surface, which is the failure
// this is here to prevent.
//
//	Export-ModuleMember -Function Get-Thing, Set-Thing
//	Export-ModuleMember -Function @('Get-Thing','Set-Thing') -Alias gt
//	Export-ModuleMember -Function *-Thing
//
// A wildcard is treated as no statement at all. It exports by pattern, and a pattern this
// cannot evaluate against names it has not finished reading is worse than the default —
// the default is at least right for the common case.
func psExportedNames(lines []codeLine) (map[string]bool, bool) {
	out := map[string]bool{}
	found := false
	for _, cl := range lines {
		code := strings.TrimSpace(cl.Text)
		if !strings.HasPrefix(strings.ToLower(code), "export-modulemember") {
			continue
		}
		// Read from Raw: the names are inside quotes in the array form, and the scanner
		// blanked those. The unquoted comma-list form survives in Text, but one reader has
		// to serve both.
		raw := strings.TrimSpace(cl.Raw)
		names, wildcard := psExportArgs(raw)
		if wildcard {
			return nil, false
		}
		found = true
		for _, n := range names {
			out[n] = true
		}
	}
	return out, found
}

// psExportArgs reads the -Function and -Variable name lists out of one
// Export-ModuleMember call.
//
// -Alias and -Cmdlet are deliberately not read. An alias is a second name for a function
// this already recorded under its real one, and a binary cmdlet is not declared in this
// file at all — neither is a symbol a page could describe.
func psExportArgs(raw string) (names []string, wildcard bool) {
	fields := psSplitArgs(raw)
	for i := 0; i < len(fields); i++ {
		p := strings.ToLower(fields[i])
		if p != "-function" && p != "-variable" {
			continue
		}
		for j := i + 1; j < len(fields) && !strings.HasPrefix(fields[j], "-"); j++ {
			n := strings.Trim(fields[j], "@()'\", \t")
			if n == "" {
				continue
			}
			if strings.ContainsAny(n, "*?$") {
				// A wildcard or a variable holding the list. Either way the set is not
				// knowable from this line.
				return nil, true
			}
			names = append(names, n)
		}
	}
	return names, false
}

// psSplitArgs splits a command line into arguments, treating a comma as whitespace.
//
// PowerShell's argument list accepts both `-Function A, B` and `-Function A,B` for the
// same thing, so a comma is a separator rather than part of a name. A quote is stripped by
// the caller, not here, because an argument may be quoted in whole or not at all.
func psSplitArgs(raw string) []string {
	return strings.FieldsFunc(raw, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ',' || r == '\r'
	})
}

// psFuncExported reports whether a function is part of the file's surface.
//
// Two rules, in order. An Export-ModuleMember list is authoritative — that is what the
// statement means. Without one, the leading-underscore convention is all there is, the
// same position the Python and shell extractors are in: PowerShell has no visibility
// keyword for a function, so intent is the only signal a single file carries.
func psFuncExported(name string, exports map[string]bool, hasExports bool) bool {
	if hasExports {
		return exports[name]
	}
	return !strings.HasPrefix(name, "_")
}

// psIsUsing reports whether a line is a `using` statement.
//
// PowerShell's `using` must be the first statement in a file and has none of C#'s
// ambiguity — there is no disposal form — so the keyword alone decides. The one thing to
// exclude is `using:` scope modifier syntax, `$using:var`, which appears inside a
// script block and begins with a `$`.
func psIsUsing(code string) bool {
	lower := strings.ToLower(code)
	return strings.HasPrefix(lower, "using module ") ||
		strings.HasPrefix(lower, "using namespace ") ||
		strings.HasPrefix(lower, "using assembly ")
}

// psUsing reads a `using` statement.
//
//	using module Pester                  a PowerShell module
//	using module ./lib/Widget.psm1       a module by path, which is how a class is shared
//	using namespace System.Text          a .NET namespace
//	using assembly ./bin/Thing.dll       a compiled assembly
//
// The path form is the one that can resolve to a node here, and it is also the only way to
// get a PowerShell class from one file into another — `Import-Module` does not bring
// classes across. So a repository that defines classes has its real internal edges written
// this way and nowhere else.
func psUsing(code string, line int) (Import, bool) {
	fields := strings.Fields(code)
	if len(fields) < 3 {
		return Import{}, false
	}
	arg := psUnquote(strings.TrimSpace(strings.Join(fields[2:], " ")))
	if arg == "" || strings.Contains(arg, "$") {
		return Import{}, false
	}
	// A hashtable module specification — `using module @{ModuleName='X';RequiredVersion=
	// '1.0'}` — names a module and a version. The name is inside it, and reading it would
	// be a second parser for a form that is rare in scripts and belongs to a manifest.
	if strings.HasPrefix(arg, "@{") {
		return Import{}, false
	}
	return Import{Raw: psNormalizeSep(arg), Line: line}, true
}

// psIsImportModule reports whether a line imports a module at runtime.
func psIsImportModule(code string) bool {
	lower := strings.ToLower(code)
	return strings.HasPrefix(lower, "import-module ") || strings.HasPrefix(lower, "ipmo ")
}

// psImportModule reads an Import-Module call.
//
//	Import-Module Pester
//	Import-Module -Name Pester -MinimumVersion 5.0
//	Import-Module "$PSScriptRoot/lib/Widget.psm1"
//
// The first positional argument is the module, and `-Name` names it explicitly. Every
// other parameter is configuration — a version constraint, `-Force`, `-Scope` — and none
// of them changes what is depended on.
func psImportModule(cl codeLine) (Import, bool) {
	fields := psSplitArgs(strings.TrimSpace(cl.Raw))
	if len(fields) < 2 {
		return Import{}, false
	}
	arg := ""
	for i := 1; i < len(fields); i++ {
		if strings.EqualFold(fields[i], "-Name") {
			if i+1 < len(fields) {
				arg = fields[i+1]
			}
			break
		}
		if strings.HasPrefix(fields[i], "-") {
			// A switch parameter takes no value and a valued one consumes the next field.
			// Neither is the module, and skipping only the switch would read `5.0` from
			// `-MinimumVersion 5.0` as a module name.
			if i+1 < len(fields) && !strings.HasPrefix(fields[i+1], "-") {
				i++
			}
			continue
		}
		arg = fields[i]
		break
	}
	arg = psUnquote(arg)
	if arg == "" || strings.HasPrefix(arg, "@{") {
		return Import{}, false
	}
	p, ok := psStripAnchor(arg)
	if !ok {
		return Import{}, false
	}
	return Import{Raw: p, Line: cl.Num}, true
}

// psIsDotSource reports whether a line dot-sources another script.
//
// The same `.` builtin the shell has and the same care it needs: a `.` is the operator
// only when it is the whole first token. `.\build.ps1` runs a script in a child scope
// rather than dot-sourcing it, and `..\lib` is a path fragment.
func psIsDotSource(code string) bool {
	return len(code) > 1 && code[0] == '.' && (code[1] == ' ' || code[1] == '\t')
}

// psDotSource reads the path out of a dot-source statement.
//
// Dot-sourcing is how a `.ps1` library is loaded, and it is nearly always anchored:
// `. "$PSScriptRoot\lib\util.ps1"`. $PSScriptRoot is the directory of the file being run,
// so unlike the shell's `$(dirname "$0")` it is not an approximation of the script's
// directory — it is exactly that, which makes the anchor assumption a fact here rather
// than a reading of intent.
func psDotSource(cl codeLine) (Import, bool) {
	raw := strings.TrimSpace(cl.Raw)
	arg := strings.TrimSpace(raw[1:])
	if i := strings.IndexAny(arg, ";#"); i >= 0 {
		arg = strings.TrimSpace(arg[:i])
	}
	arg = psUnquote(arg)
	if arg == "" {
		return Import{}, false
	}
	p, ok := psStripAnchor(arg)
	if !ok {
		return Import{}, false
	}
	return Import{Raw: p, Line: cl.Num}, true
}

// psRequires reads a `#Requires -Modules` statement.
//
// This is the one comment in this package that is read as code, because in PowerShell it
// is code: `#Requires` is a statement the engine enforces before the script runs, and a
// script that declares its modules this way declares them nowhere else.
//
// Only -Modules is read. -Version, -RunAsAdministrator and -PSEdition are execution
// preconditions rather than dependencies — nothing in the graph could hold them.
//
// One statement can require several modules, and each is a dependency in its own right, so
// this returns one Import per module rather than one per statement. Putting the second name
// in the first Import's Names would say something different and false: Names is the symbols
// a specifier brings across, so `#Requires -Modules Pester, powershell-yaml` would become a
// dependency on Pester with a member named powershell-yaml, and the second module's edge
// would never reach the graph at all.
func psRequires(cl codeLine) ([]Import, bool) {
	raw := strings.TrimSpace(cl.Raw)
	if !strings.HasPrefix(strings.ToLower(raw), "#requires") {
		return nil, false
	}
	fields := psSplitArgs(raw)
	var names []string
	for i := 0; i < len(fields); i++ {
		if !strings.EqualFold(fields[i], "-Modules") {
			continue
		}
		for j := i + 1; j < len(fields) && !strings.HasPrefix(fields[j], "-"); j++ {
			n := strings.Trim(fields[j], "@()'\" \t")
			// The hashtable form, `@{ModuleName='X';ModuleVersion='1.0'}`, holds the name
			// inside it. Left unread for the reason psUsing gives.
			if n == "" || strings.ContainsAny(n, "{};=$*") {
				continue
			}
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil, false
	}
	out := make([]Import, 0, len(names))
	for _, n := range names {
		out = append(out, Import{Raw: n, Line: cl.Num})
	}
	return out, true
}

// psStripAnchor removes a leading `$PSScriptRoot`-style anchor and reports whether what
// remains is a path this extractor can name.
//
// Three anchors carry the same meaning and all three appear in real scripts:
// $PSScriptRoot is the running file's directory, $PSCommandPath is the file itself, and
// `$MyInvocation.MyCommand.Path` is what was written before $PSScriptRoot existed. A
// module name that is not a path — `Pester` — passes through unchanged, because
// resolvePowerShell has to tell those apart anyway and the presence of a separator is what
// says which it is.
func psStripAnchor(p string) (string, bool) {
	for _, anchor := range []string{"$PSScriptRoot", "$psscriptroot", "${PSScriptRoot}"} {
		if !strings.HasPrefix(p, anchor) {
			continue
		}
		rest := p[len(anchor):]
		if !strings.HasPrefix(rest, "/") && !strings.HasPrefix(rest, "\\") {
			return "", false
		}
		return psNormalizeSep("." + rest), true
	}
	if strings.Contains(p, "$") {
		// A path assembled from variables. Nothing here can name the file, and a guess
		// would put a node in the graph that no file claims.
		return "", false
	}
	return psNormalizeSep(p), true
}

// psNormalizeSep turns Windows separators into the repo-relative form resolution expects.
//
// A PowerShell path is written with either separator and often with `\` even on Linux,
// where PowerShell accepts it. Every path in the graph is `/`-separated, so the conversion
// belongs here rather than in the resolver, which would otherwise need the same rule for
// one language.
func psNormalizeSep(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

// psUnquote removes one layer of surrounding quotes.
func psUnquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// psIsParamBlock reports whether a line opens a param block.
func psIsParamBlock(code string) bool {
	lower := strings.ToLower(code)
	return strings.HasPrefix(lower, "param(") || strings.HasPrefix(lower, "param (")
}

// psFuncDecl reports the name of a function or filter declared on a line.
//
//	function Get-Thing {
//	function Global:Get-Thing {
//	filter Where-Big {
//
// A filter is a function with an implicit process block — the same declaration with a
// different execution model — so it is recorded as SymFunc rather than given a kind of its
// own. `workflow` is deliberately absent: it was removed in PowerShell 6 and a file using
// it is Windows PowerShell 5 only.
//
// A scope prefix is stripped. `function Script:Helper` declares Helper with a scope
// qualifier, and recording the qualifier as part of the name would put `Script:Helper` on
// the page under a name no caller writes.
func psFuncDecl(code string) (string, bool) {
	var rest string
	switch {
	case psCutKeyword(code, "function", &rest):
	case psCutKeyword(code, "filter", &rest):
	default:
		return "", false
	}
	name := rest
	if i := strings.IndexAny(name, " \t({"); i >= 0 {
		name = name[:i]
	}
	name = psStripScope(name)
	if !psIsName(name) {
		return "", false
	}
	return name, true
}

// psStripScope removes a `Global:`/`Script:`/`Local:`/`Private:` qualifier.
func psStripScope(name string) string {
	i := strings.IndexByte(name, ':')
	if i <= 0 {
		return name
	}
	switch strings.ToLower(name[:i]) {
	case "global", "script", "local", "private":
		return name[i+1:]
	}
	return name
}

// psIsName reports whether s can be a PowerShell function name.
//
// A hyphen is the interesting character: `Get-Thing` is the idiomatic Verb-Noun form and a
// rule built from identifier characters would reject every well-named function in a
// codebase. Digits, underscores and dots are legal too — `Get-Thing2`, `_helper`,
// `Invoke.Thing` — so the rule is again an exclusion of what a name cannot hold.
func psIsName(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if identChar(c) || c == '-' || c == '.' {
			continue
		}
		return false
	}
	return true
}

// psCutKeyword reports whether code opens with kw as a whole word, case-insensitively, and
// writes what follows into rest.
//
// Case-insensitive because PowerShell is: `Function Get-Thing` and `function Get-Thing`
// are the same declaration, and both are written. A case-sensitive rule would silently
// drop every function in a file that capitalises the keyword.
func psCutKeyword(code, kw string, rest *string) bool {
	if len(code) <= len(kw) || !strings.EqualFold(code[:len(kw)], kw) {
		return false
	}
	if c := code[len(kw)]; c != ' ' && c != '\t' {
		// `functions` and `filtered` begin with the keyword's letters and are not it.
		return false
	}
	*rest = strings.TrimLeft(code[len(kw):], " \t")
	return true
}

// psTypeDecl reports the keyword and name of a class or enum declaration.
//
// PowerShell 5 added both. They are the only declarations in the language that own
// members, which is why they get a scope on the stack and a function does not.
func psTypeDecl(code string) (kw, name string, ok bool) {
	var rest string
	switch {
	case psCutKeyword(code, "class", &rest):
		kw = "class"
	case psCutKeyword(code, "enum", &rest):
		kw = "enum"
	default:
		return "", "", false
	}
	name = rest
	// A base class or interface list follows a colon: `class Widget : Base, IThing {`.
	if i := strings.IndexAny(name, " \t:{("); i >= 0 {
		name = name[:i]
	}
	if !psIsName(name) {
		return "", "", false
	}
	return kw, name, true
}

// psTypeKind maps a PowerShell type keyword onto a SymbolKind.
func psTypeKind(kw string) SymbolKind {
	if kw == "enum" {
		return SymType
	}
	return SymClass
}

// psIsClassMemberSite reports whether a position is directly inside a class body.
func psIsClassMemberSite(types []jvmScope, depth int) bool {
	return jvmDirectMember(types, depth)
}

// psIsHidden reports whether a class member is declared hidden.
//
// `hidden` is PowerShell's only visibility keyword, and it is the inverse of C#'s default:
// a member with no modifier is public, and hiding is opt-in. It also does not truly
// prevent access — a hidden member is reachable with an explicit cast — but it is the
// author's statement that the member is not surface, which is what exportedness records.
func psIsHidden(code string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(code)), "hidden ")
}

// psMethodDecl reports the name of a class method declared on a line.
//
//	[string] Greet([string]$name) {
//	static [Widget] Empty() {
//	Widget([string]$name) {              a constructor, skipped by the caller
//	hidden [void] Reset() {
//
// The name is the identifier immediately before the parameter list, which is the same
// shape javaMethodDecl reads. What differs is that PowerShell's return type is a bracketed
// attribute rather than a bare name, so the text before the method name is `[string]` or
// `static [string]` or nothing at all — a constructor has no return type. That means an
// empty prefix cannot be used to reject a call the way the Java rule does, and the
// rejection has to come from the name and from the depth guard instead.
func psMethodDecl(code string) (string, bool) {
	open := strings.IndexByte(code, '(')
	if open < 0 {
		return "", false
	}
	head := strings.TrimRight(code[:open], " \t")
	end := len(head)
	start := end
	for start > 0 && (identChar(head[start-1]) || head[start-1] == '-') {
		start--
	}
	name := head[start:end]
	if !psIsName(name) || psControlKeyword(name) {
		return "", false
	}
	before := strings.TrimSpace(head[:start])
	// A pipeline or an assignment before the name means the name is being used rather than
	// declared: `$x = Get-Thing(` and `| Where-Object (`. A method's prefix is a bracketed
	// return type, the word `static`, `hidden`, or nothing.
	if before != "" && !strings.HasSuffix(before, "]") &&
		!strings.EqualFold(before, "static") && !strings.EqualFold(before, "hidden") &&
		!strings.EqualFold(before, "static hidden") && !strings.EqualFold(before, "hidden static") {
		return "", false
	}
	return name, true
}

// psControlKeyword reports whether a token is a statement keyword that takes a
// parenthesis.
func psControlKeyword(w string) bool {
	switch strings.ToLower(w) {
	case "if", "elseif", "else", "for", "foreach", "while", "do", "switch", "catch",
		"trap", "param", "return", "throw", "try", "finally", "until", "in", "new":
		return true
	}
	return false
}

// psPropertyDecl reports the name of a class property declared on a line.
//
//	[string]$Name
//	[int]$Count = 0
//	hidden [hashtable]$cache = @{}
//	static [Widget]$Empty
//	$Untyped
//
// The type annotation is optional, because PowerShell's is: a property declared without one
// is [Object], and `class Widget { $Untyped }` is ordinary code. What makes the rule safe is
// the depth guard rather than the bracket — only a property can appear directly inside a
// class body, so a `$name` line there is one.
//
// A default value is cut off before anything is judged, and that is the whole reason this
// does not simply reject a line holding a paren. `[int]$Count = Get-Count(5)` declares
// Count; the paren belongs to the default, not to the declaration. A method's paren is in
// the declaration itself, which is what still separates the two.
func psPropertyDecl(code string) (string, bool) {
	decl := code
	if i := strings.IndexByte(decl, '='); i >= 0 {
		decl = decl[:i]
	}
	if strings.ContainsAny(decl, "()") {
		return "", false
	}
	dollar := strings.IndexByte(decl, '$')
	if dollar < 0 {
		return "", false
	}
	// When a type annotation is present it must be a closed bracket group before the name.
	// A stray bracket on either side is a shape this does not read.
	if close := strings.LastIndexByte(decl[:dollar], ']'); close >= 0 {
		if !strings.Contains(decl[:close], "[") {
			return "", false
		}
	} else if strings.ContainsAny(decl[:dollar], "[]") {
		return "", false
	}
	name := strings.TrimSpace(decl[dollar+1:])
	if i := strings.IndexAny(name, " \t;"); i >= 0 {
		name = name[:i]
	}
	if name == "" || !psIsVarName(name) {
		return "", false
	}
	return name, true
}

// psIsVarName reports whether s is a PowerShell variable name.
//
// Stricter than a function name: no hyphen and no dot, because both are how a variable is
// used rather than named — `$a-$b` is subtraction and `$obj.Prop` is member access.
func psIsVarName(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !identChar(s[i]) {
			return false
		}
	}
	return true
}

// psFuncDoc reads a function's documentation, looking inside the body before above it.
//
// PowerShell's comment-based help is a `<# ... #>` block holding dotted section keywords,
// and its canonical position is the *first thing inside the function body* — that is where
// Get-Help looks first, and where every template and every style guide puts it. Above the
// declaration is also valid and also used. So the inside is tried first and the outside is
// the fallback, which is the opposite order from every other extractor here and is a fact
// about the language rather than a preference.
func psFuncDoc(lines []codeLine, idx int) string {
	if s := psHelpInBody(lines, idx); s != "" {
		return s
	}
	return psDoc(lines, idx)
}

// psHelpInBody reads a comment-based help block from the top of a function body.
//
// The block has to be the body's *first* substantive line — past blanks and a brace on its
// own line, and nothing else. A `<#` below any code is a comment about the code it precedes
// rather than help for the function, and reading it as help would attribute a remark about
// one statement to the whole function. The known cost is that help written *after* the
// `param()` block, which Get-Help also accepts, is not found here; such a function falls
// back to a comment above its declaration and otherwise reports no doc. Read from Raw, since
// the scanner blanks a block comment.
func psHelpInBody(lines []codeLine, idx int) string {
	for i := idx + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i].Raw)
		if t == "" || t == "{" {
			continue
		}
		if !strings.HasPrefix(t, "<#") {
			return ""
		}
		var block []string
		for j := i; j < len(lines); j++ {
			l := strings.TrimSpace(lines[j].Raw)
			block = append(block, l)
			if strings.Contains(l, "#>") {
				break
			}
		}
		return psHelpText(block)
	}
	return ""
}

// psDoc reads the comment block above a declaration.
//
// Both forms, because both are written: a `<# ... #>` block and a run of `#` lines. Read
// from Raw, because the scanner strips comments.
func psDoc(lines []codeLine, idx int) string {
	const maxDoc = 200

	i := idx - 1
	// An attribute between the comment and the declaration is normal —
	// `[CmdletBinding()]` above a function — and is skipped as jvmDoc skips annotations.
	for i >= 0 && idx-i < maxDoc {
		t := strings.TrimSpace(lines[i].Raw)
		if t == "" || strings.HasPrefix(t, "[") {
			i--
			continue
		}
		break
	}
	if i < 0 {
		return ""
	}
	if t := strings.TrimSpace(lines[i].Raw); strings.HasSuffix(t, "#>") {
		var block []string
		for j := i; j >= 0 && i-j < maxDoc; j-- {
			l := strings.TrimSpace(lines[j].Raw)
			block = append([]string{l}, block...)
			if strings.HasPrefix(l, "<#") {
				return psHelpText(block)
			}
		}
		return ""
	}
	var block []string
	for ; i >= 0 && idx-i < maxDoc; i-- {
		t := strings.TrimSpace(lines[i].Raw)
		if !strings.HasPrefix(t, "#") || strings.HasPrefix(t, "#!") {
			break
		}
		// A `#Requires` statement is code, and a `#region` marker is editor furniture.
		// Neither is prose about the declaration below it.
		low := strings.ToLower(t)
		if strings.HasPrefix(low, "#requires") || strings.HasPrefix(low, "#region") ||
			strings.HasPrefix(low, "#endregion") {
			break
		}
		body := strings.TrimSpace(strings.TrimPrefix(t, "#"))
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

// psHelpText reduces a `<# ... #>` block to one sentence.
//
// A help block is sectioned by dotted keywords — .SYNOPSIS, .DESCRIPTION, .PARAMETER,
// .EXAMPLE — and the synopsis is the sentence. The keywords themselves must not reach the
// page: a bundle page reading ".SYNOPSIS Creates a widget." looks like a bug in the
// extracted prose rather than in the splitter.
//
// A block with no keywords at all is ordinary prose, which is how a terse comment above a
// function is written, and is taken whole.
func psHelpText(block []string) string {
	var body []string
	for _, l := range block {
		l = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(l, "<#"), "#>"))
		body = append(body, l)
	}
	var out []string
	inSection := false
	seenKeyword := false
	for _, l := range body {
		if strings.HasPrefix(l, ".") {
			kw := strings.ToUpper(strings.Fields(l + " ")[0])
			seenKeyword = true
			inSection = kw == ".SYNOPSIS" || kw == ".DESCRIPTION"
			if inSection {
				// A keyword may carry its text on the same line, which some authors write.
				if tail := strings.TrimSpace(strings.TrimPrefix(l, strings.Fields(l)[0])); tail != "" {
					out = append(out, tail)
				}
			}
			// A .SYNOPSIS already collected is enough; a later .DESCRIPTION would append a
			// second paragraph to a one-sentence field.
			if kw == ".SYNOPSIS" {
				continue
			}
			if len(out) > 0 && !inSection {
				break
			}
			continue
		}
		if !seenKeyword || inSection {
			out = append(out, l)
		}
	}
	return FirstSentence(strings.TrimSpace(strings.Join(out, " ")))
}
