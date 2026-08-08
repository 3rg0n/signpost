package manifest

import (
	"path"
	"strconv"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// Bazel extraction.
//
// A BUILD file is the opposite of a CMakeLists.txt in the one way that matters to a reader.
// Both are executable — Starlark is a real language with functions, loops, and `load()` — but
// a BUILD file's content is overwhelmingly a flat sequence of rule calls with literal
// arguments, because Bazel's own style forbids most of what would make it otherwise: no
// recursion, no `while`, and, by convention and in the vast majority of files, no logic
// above a `select()`. So the same tolerant-reader technique that gets a *subset* of CMake
// gets nearly all of a BUILD file, and this reader's Kind says so (see KindBazel).
//
// What it reads, and why each is a fact nothing else in a Bazel repository states:
//
//   - Targets. `go_library(name = "graph", srcs = [...])` is the unit of a Bazel build, and
//     a Bazel repository's directories are *not* its modules — one package may hold five
//     targets and a target may draw srcs from a glob. The `name` is the only place the unit
//     is named.
//   - Dependencies, which is where Bazel is unusually informative. `deps` distinguishes
//     in-repo from external syntactically, not by guesswork: `//internal/graph` is this
//     repository and `@com_github_pkg_errors//:errors` is not. That is exactly the
//     distinction Dep.Local exists for and that Terraform's `source` cannot make without
//     the reader having seen a `./`.
//   - Which targets are tests. A rule whose name ends `_test` is a test by Bazel's own
//     naming rule, enforced by the rule set rather than by convention.
//   - External repositories. A `WORKSPACE` file's `http_archive` and `git_repository`
//     calls, and a `MODULE.bazel` file's `bazel_dep`, are the repository's declared supply
//     chain — with a version or a sha256, which is the pinning fact worth having.
//
// Not read, deliberately: `select()`, because a dep behind a platform condition is a real
// dep on some platform and the bundle describes a repository rather than one configuration
// of it, so the strings inside a select are read as deps like any others; macro and function
// *bodies* in a `.bzl`, which define rules rather than declare targets; and any argument
// whose value is a name this reader cannot resolve, which is recorded as unread rather than
// guessed.

// ExtractBazel reads a BUILD, BUILD.bazel, WORKSPACE, MODULE.bazel, or .bzl file.
func ExtractBazel(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindBazel}
	calls, diag := parseStarlark(f.Content)
	facts.applyDiag(diag)

	pkg := path.Dir(f.Path)
	if pkg == "." {
		pkg = ""
	}
	for _, c := range calls {
		switch {
		case c.fn == "module":
			// MODULE.bazel's own declaration, which is bzlmod's answer to a go.mod: the one
			// place a Bazel repository states its own name and version.
			if facts.Module.Name == "" {
				facts.Module.Name = c.str("name")
				facts.Module.Version = c.str("version")
				if facts.Module.Name != "" {
					facts.Module.Ecosystem = ecoBazel
					facts.Module.Line = c.line
				}
			}
		case c.fn == "bazel_dep":
			// bzlmod's dependency declaration, and the only one of these forms that names a
			// module in a registry rather than an archive at a URL.
			if name := c.str("name"); name != "" {
				facts.Deps = append(facts.Deps, Dep{
					Name: name, Version: c.str("version"), Scope: ScopeBuild,
					Ecosystem: ecoBazel, Optional: c.boolArg("dev_dependency"), Line: c.line,
				})
			}
		case c.fn == "http_archive" || c.fn == "http_file" || c.fn == "git_repository" ||
			c.fn == "new_git_repository" || c.fn == "local_repository":
			readBazelRepository(&facts, c)
		case c.fn == "load":
			// A `load()` names the .bzl file the rules come from, and when it names one in
			// another repository — `@rules_go//go:def.bzl` — that repository is a build-time
			// dependency this file could not work without. In-repo loads are not deps: they
			// are this repository's own rule definitions, and recording them would report a
			// project as depending on itself.
			if lbl := c.positional(0); strings.HasPrefix(lbl, "@") {
				if repo, _ := splitBazelLabel(lbl); repo != "" {
					facts.Deps = append(facts.Deps, Dep{
						Name: repo, Scope: ScopeBuild, Ecosystem: ecoBazel, Line: c.line,
					})
				}
			}
		case strings.HasSuffix(c.fn, "_library") || strings.HasSuffix(c.fn, "_binary") ||
			strings.HasSuffix(c.fn, "_test") || strings.HasSuffix(c.fn, "_proto") ||
			c.fn == "proto_library" || c.fn == "filegroup" || c.fn == "genrule":
			readBazelTarget(&facts, c, pkg)
		}
	}
	return facts
}

// ecoBazel names the Bazel ecosystem.
//
// One constant for both `WORKSPACE` and `MODULE.bazel` dependencies, unlike CMake's split
// into cmake and pkg-config. The difference is that a `bazel_dep` and an `http_archive`
// resolve to the same thing — an external repository in the same namespace, which a
// `deps = ["@name//..."]` label refers to identically. A repository migrating from
// WORKSPACE to bzlmod declares the same dependency in both files during the transition, and
// two ecosystems would report it as two.
const ecoBazel = "bazel"

// readBazelTarget records one rule call as a target.
func readBazelTarget(facts *Facts, c starlarkCall, pkg string) {
	name := c.str("name")
	if name == "" {
		// A target whose name is computed, which happens inside a macro or a list
		// comprehension. The target is real and unnameable from here.
		facts.markIncomplete("rule " + c.fn + " with no literal name at line " + strconv.Itoa(c.line))
		return
	}

	switch {
	case strings.HasSuffix(c.fn, "_test"):
		// Bazel's naming rule is enforced by the rule sets: a rule ending `_test` produces a
		// test target and `bazel test` selects on exactly that. So this is a stated fact
		// rather than the directory-name guess isTestPath has to make elsewhere, and it goes
		// in Scripts because that is what practice.testFindings reads — a repository whose
		// tests are declared only in BUILD files otherwise reads as having none.
		facts.Scripts = append(facts.Scripts, Script{
			Name: name, Command: "bazel test //" + pkg + ":" + name, Line: c.line,
		})
	case strings.HasSuffix(c.fn, "_binary"):
		// A binary is a program, which is what an Entrypoint is. Path holds the label rather
		// than a source file because the label is what a person runs — `bazel run
		// //cmd/tool:tool` — and in Bazel the sources are a glob as often as a list.
		facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
			Name: name, Path: "//" + pkg + ":" + name, Line: c.line,
		})
	}

	for _, kw := range []string{"deps", "runtime_deps", "exports", "data"} {
		scope := ScopeBuild
		if kw == "runtime_deps" || kw == "data" {
			scope = ScopeRuntime
		}
		for _, lbl := range c.list(kw) {
			readBazelDepLabel(facts, lbl, scope, pkg, c.line)
		}
	}
}

// readBazelDepLabel records one label from a `deps` list.
//
// The three label forms Bazel permits are three different facts, and the syntax settles
// which without any resolution:
//
//   - `@repo//pkg:target` names another repository. An external dependency, and the
//     repository is the dependency — not the target inside it, for the same reason a CMake
//     `Qt6::Widgets` reduces to Qt6: one dependency with a part of it used, not two.
//   - `//pkg:target` names this repository, absolutely. Local, with Source set to the
//     package directory so addDeclaredDepEdges finds the module.
//   - `:target` or `target` names this package. Also local, and resolved against the
//     declaring file's own directory.
func readBazelDepLabel(facts *Facts, lbl string, scope DepScope, pkg string, line int) {
	if lbl == "" || strings.Contains(lbl, "$") {
		return
	}
	repo, pkgPath := splitBazelLabel(lbl)
	if repo != "" {
		facts.Deps = append(facts.Deps, Dep{
			Name: repo, Scope: scope, Ecosystem: ecoBazel, Line: line,
		})
		return
	}
	// A relative label — `:helper` or `helper` — is this package. Recording it as a local
	// dependency on the directory it already lives in would be a self-edge, which
	// addDeclaredDepEdges would draw from a module to itself. It is a real declaration and
	// there is nothing for the graph to say about it, so it is dropped here rather than
	// filtered downstream.
	if pkgPath == "" {
		return
	}
	facts.Deps = append(facts.Deps, Dep{
		Name: path.Base(pkgPath), Scope: scope, Ecosystem: ecoBazel,
		Source: pkgPath, Local: true, Line: line,
	})
}

// splitBazelLabel splits a label into its repository and its package path.
//
// Returns ("", "") for a label naming this package, since neither part is present. The
// package path comes back repo-relative and slash-separated, with no leading `//` and the
// `:target` removed — which is the shape Dep.Source is documented to hold.
func splitBazelLabel(lbl string) (repo, pkgPath string) {
	rest := lbl
	if strings.HasPrefix(rest, "@") {
		// `@repo//pkg:target`, or bzlmod's `@@canonical_name//pkg:target` where the doubled
		// sigil marks a name Bazel resolved rather than one a human wrote. The canonical
		// form carries a version suffix — `@@rules_go~0.46.0//go` — which is dropped: the
		// dependency is the module, and its version belongs in Dep.Version from the
		// declaration that pinned it.
		rest = strings.TrimPrefix(strings.TrimPrefix(rest, "@"), "@")
		i := strings.Index(rest, "//")
		if i < 0 {
			// `@repo`, which is shorthand for `@repo//:repo`.
			return trimBazelCanonical(rest), ""
		}
		repo = trimBazelCanonical(rest[:i])
		rest = rest[i:]
	}
	if !strings.HasPrefix(rest, "//") {
		// A relative label. `:helper` and `helper` both name this package.
		return repo, ""
	}
	rest = strings.TrimPrefix(rest, "//")
	if i := strings.Index(rest, ":"); i >= 0 {
		rest = rest[:i]
	}
	return repo, strings.Trim(rest, "/")
}

// trimBazelCanonical drops bzlmod's version suffix from a canonical repository name.
func trimBazelCanonical(name string) string {
	if i := strings.IndexAny(name, "~+"); i > 0 {
		return name[:i]
	}
	return name
}

// readBazelRepository records a WORKSPACE-era external repository declaration.
//
// This is the highest-value supply-chain statement in a Bazel repository, and the reason is
// the same one that makes CMake's FetchContent worth reading: the dependency is an archive
// at a URL or a commit in a git repository, so there is no registry to publish an advisory
// against and the pin is the only thing standing between the build and whatever that URL
// serves tomorrow. `sha256` is what makes it a pin rather than a hope, so its absence is
// worth carrying — recorded as the version being empty, which is what a reader auditing
// dependencies will see as unpinned.
func readBazelRepository(facts *Facts, c starlarkCall) {
	name := c.str("name")
	if name == "" {
		return
	}
	d := Dep{Name: name, Scope: ScopeBuild, Ecosystem: ecoBazel, Line: c.line}
	d.Version = firstNonEmpty(c.str("sha256"), c.str("commit"), c.str("tag"), c.str("branch"))
	d.Source = firstNonEmpty(c.str("remote"), c.str("url"), c.str("path"), c.first("urls"))
	// A `local_repository` names a directory, which is the one form of this that is not
	// external at all — the same distinction Dep.Local carries for a Terraform `./` source.
	if c.fn == "local_repository" && d.Source != "" {
		d.Local = true
	}
	facts.Deps = append(facts.Deps, d)
}

// starlarkCall is one top-level function call: `go_library(name = "x", srcs = [...])`.
//
// Arguments are kept as text rather than as a value tree, because every consumer here wants
// either a string, a list of strings, or a bool, and an expression that is none of those is
// something this reader steps over. That is the same choice hclBlock.stringAttr makes and
// for the same reason: "the author stated this outright" and "the author wrote something to
// compute it" are the two answers a caller needs, and a partial value tree would offer a
// third that nothing knows what to do with.
type starlarkCall struct {
	fn   string
	args []starlarkArg
	line int
}

// starlarkArg is one argument: a keyword and its value, or a positional value with no
// keyword.
type starlarkArg struct {
	kw string
	// str is the value when it was a literal string, unquoted.
	str string
	// list is the values when it was a list or tuple of literal strings.
	list []string
	// isStr and isList distinguish an absent value from an empty one: `srcs = []` is a
	// deliberate empty list and `srcs = glob(["*.go"])` is a value this reader did not read.
	isStr, isList bool
	// isTrue and isBool carry a `True`/`False` literal.
	isTrue, isBool bool
}

// str returns a keyword argument's literal string value, or "".
func (c starlarkCall) str(kw string) string {
	for _, a := range c.args {
		if a.kw == kw && a.isStr {
			return a.str
		}
	}
	return ""
}

// list returns a keyword argument's literal string list.
//
// A `deps = ["//a"] + PLATFORM_DEPS` yields the literal half and nothing for the rest, which
// is the reading that loses least: the declared deps are real deps, and the concatenated
// name is recorded as unread by the parser's own diagnostic rather than dropping the whole
// list.
func (c starlarkCall) list(kw string) []string {
	for _, a := range c.args {
		if a.kw == kw && a.isList {
			return a.list
		}
	}
	return nil
}

// first returns the first element of a list argument — `urls = [...]`, where the convention
// is a list of mirrors of one artifact rather than a list of dependencies.
func (c starlarkCall) first(kw string) string {
	if l := c.list(kw); len(l) > 0 {
		return l[0]
	}
	return ""
}

// boolArg returns a keyword argument's `True`/`False` value, false when absent or computed.
func (c starlarkCall) boolArg(kw string) bool {
	for _, a := range c.args {
		if a.kw == kw && a.isBool {
			return a.isTrue
		}
	}
	return false
}

// positional returns the i-th argument that carried no keyword, as a literal string.
//
// Only `load()` uses positional string arguments among the calls this reads; every rule
// takes `name = ` by keyword, which Bazel's own style guide requires.
func (c starlarkCall) positional(i int) string {
	n := 0
	for _, a := range c.args {
		if a.kw != "" {
			continue
		}
		if n == i {
			return a.str
		}
		n++
	}
	return ""
}

// maxStarlarkDepth bounds nesting inside an argument list.
//
// The same guard, for the same reason, as maxHCLDepth: a file consisting of ten thousand
// open brackets is a plausible input to a tool that runs in CI on whatever a pull request
// contains, and unbounded recursion on it is a denial of service. Sixty-four is far past any
// hand-written or generated BUILD file — Bazel's own style caps rule arguments at one level
// of list nesting — so nothing real reaches it.
const maxStarlarkDepth = 64

// maxStarlarkArgs bounds one call's argument count, and maxStarlarkList one list's length.
//
// Generated BUILD files are the case: a `filegroup` listing every file in a large data
// directory is a real artifact and its facts are in its `name`. Past the cap the remaining
// entries are dropped with a note, so the target still reads.
const (
	maxStarlarkArgs = 512
	maxStarlarkList = 4096
)

// parseStarlark reads a file's top-level calls.
//
// Deliberately not a Starlark interpreter and not a full parser either. What it does is scan
// for `identifier(` at the start of a logical line and read the argument list that follows,
// recognising literal strings, lists of literal strings, and booleans, and stepping over
// everything else with the nesting tracked so that a `glob([...])` inside an argument does
// not end the call.
//
// The consequence, stated plainly: a target declared inside a `for` loop or produced by a
// macro is not read, because neither is a top-level call. That is the same class of limit
// the CMake reader has, and it is visible in the same way — a macro-generated target simply
// does not appear, rather than appearing wrong.
//
// Never returns an error, matching the Reader contract.
func parseStarlark(src string) ([]starlarkCall, Diag) {
	p := &starlarkParser{src: src, line: 1}
	return p.parse()
}

type starlarkParser struct {
	src  string
	i    int
	line int
	diag Diag
}

func (p *starlarkParser) eof() bool { return p.i >= len(p.src) }

func (p *starlarkParser) next() byte {
	c := p.src[p.i]
	p.i++
	if c == '\n' {
		p.line++
	}
	return c
}

func (p *starlarkParser) parse() ([]starlarkCall, Diag) {
	var out []starlarkCall
	atLineStart := true
	for !p.eof() {
		c := p.src[p.i]
		switch {
		case c == '\n':
			p.next()
			atLineStart = true
			continue
		case c == ' ' || c == '\t' || c == '\r':
			p.next()
			continue
		case c == '#':
			p.skipLineComment()
			continue
		case c == '"' || c == '\'':
			// A module-level docstring, or a string in an expression this reader is
			// stepping over. Consumed as a unit so a `(` inside it is not a call.
			p.stringLit()
			atLineStart = false
			continue
		}
		if !atLineStart || !isStarlarkIdentStart(c) {
			// Anything indented is inside a function, a loop, or a comprehension, and
			// anything mid-line is part of an expression. Neither is a top-level call.
			p.next()
			atLineStart = false
			continue
		}
		line := p.line
		name := p.identifier()
		p.skipSpaceInLine()
		if p.eof() || p.src[p.i] != '(' {
			// `NAME = [...]` or a bare identifier: a variable, not a call.
			atLineStart = false
			continue
		}
		p.next()
		args, ok := p.arguments(1)
		if !ok {
			p.diag.malformed(line, "unterminated argument list for "+name)
			return append(out, starlarkCall{fn: name, args: args, line: line}), p.diag
		}
		out = append(out, starlarkCall{fn: name, args: args, line: line})
		atLineStart = false
	}
	return out, p.diag
}

// arguments reads a call's argument list up to its closing paren, the opener consumed.
func (p *starlarkParser) arguments(depth int) ([]starlarkArg, bool) {
	if depth > maxStarlarkDepth {
		p.diag.note(p.line, "nesting deeper than the reader follows")
		return nil, p.skipToClose(')')
	}
	var out []starlarkArg
	for {
		p.skipSpaceAndComments()
		if p.eof() {
			return out, false
		}
		if p.src[p.i] == ')' {
			p.next()
			return out, true
		}
		if p.src[p.i] == ',' {
			p.next()
			continue
		}
		// A bracket this loop does not own — a `}` where a `)` belongs — is a character every
		// scanner below declines: skipExpression stops before any closer at depth zero, so
		// argument returns having consumed nothing and the loop offers it the same byte again.
		// Left unguarded that is not a wrong reading but a spin, and a reader that hangs on one
		// malformed file stops the whole bundle rather than reporting a partial page.
		before := p.i
		arg, ok := p.argument(depth)
		if !ok {
			return out, false
		}
		if p.i == before {
			p.diag.malformed(p.line, "argument list this reader cannot follow")
			return out, false
		}
		if len(out) < maxStarlarkArgs {
			out = append(out, arg)
		} else {
			p.diag.note(p.line, "argument list longer than the reader follows")
		}
	}
}

// argument reads one argument, with its keyword when it has one.
func (p *starlarkParser) argument(depth int) (starlarkArg, bool) {
	var a starlarkArg
	// A keyword is an identifier followed by `=` — and not by `==`, which is a comparison
	// inside a positional expression.
	if isStarlarkIdentStart(p.src[p.i]) {
		save, saveLine := p.i, p.line
		name := p.identifier()
		p.skipSpaceInLine()
		if !p.eof() && p.src[p.i] == '=' && (p.i+1 >= len(p.src) || p.src[p.i+1] != '=') {
			p.next()
			a.kw = name
		} else {
			p.i, p.line = save, saveLine
		}
	}
	p.skipSpaceAndComments()
	if p.eof() {
		return a, false
	}
	switch p.src[p.i] {
	case '"', '\'':
		a.str, a.isStr = p.stringLit(), true
	case '[', '(':
		closer := byte(']')
		if p.src[p.i] == '(' {
			closer = ')'
		}
		p.next()
		l, ok := p.stringList(closer, depth+1)
		if !ok {
			return a, false
		}
		a.list, a.isList = l, true
	default:
		// An expression: a `glob(...)`, a `select({...})`, a variable, a number, a boolean.
		// Read as far as the next comma at this depth, so the argument after it still reads.
		text, ok := p.skipExpression()
		if !ok {
			return a, false
		}
		switch strings.TrimSpace(text) {
		case "True":
			a.isTrue, a.isBool = true, true
		case "False":
			a.isBool = true
		}
	}
	// A trailing `+ OTHER_LIST` or `.format(...)` makes the value larger than what was read.
	// Whatever was read stays — it is a real subset of a real value — and the rest is
	// stepped over rather than misread as the next argument.
	p.skipSpaceAndComments()
	if !p.eof() && p.src[p.i] != ',' && p.src[p.i] != ')' {
		if _, ok := p.skipExpression(); !ok {
			return a, false
		}
	}
	return a, true
}

// stringList reads a bracketed list of literal strings, the opener consumed.
//
// Non-string entries are stepped over rather than ending the list: a
// `deps = ["//a"] + select({...})` is handled by the caller, but a
// `deps = ["//a", CONSTANT, "//b"]` is handled here, and dropping `//b` because of what sat
// between it and `//a` would lose a declared dependency for a reason the file's author would
// not recognise.
func (p *starlarkParser) stringList(closer byte, depth int) ([]string, bool) {
	if depth > maxStarlarkDepth {
		p.diag.note(p.line, "nesting deeper than the reader follows")
		return nil, p.skipToClose(closer)
	}
	var out []string
	for {
		p.skipSpaceAndComments()
		if p.eof() {
			return out, false
		}
		switch c := p.src[p.i]; c {
		case closer:
			p.next()
			return out, true
		case ',':
			p.next()
			continue
		case '"', '\'':
			s := p.stringLit()
			if len(out) < maxStarlarkList {
				out = append(out, s)
			} else {
				p.diag.note(p.line, "list longer than the reader follows")
			}
			continue
		case '[':
			// A nested list — `srcs = [["a"], ["b"]]` is not idiomatic but a list
			// comprehension's body can produce one. Flattened, since a dep is a dep at
			// whatever nesting it was written.
			p.next()
			inner, ok := p.stringList(']', depth+1)
			if !ok {
				return out, false
			}
			out = append(out, inner...)
			continue
		}
		before := p.i
		if _, ok := p.skipExpression(); !ok {
			return out, false
		}
		if p.i == before {
			// The same spin the argument loop guards against: a closer that is not this list's
			// closer is declined by every scanner here. See the comment there.
			p.diag.malformed(p.line, "list this reader cannot follow")
			return out, false
		}
	}
}

// skipExpression consumes one expression, stopping before the comma or closing bracket that
// ends it at this nesting level, and returns its text.
//
// The text is returned only so the caller can test it against `True` and `False`; nothing
// else interprets it.
func (p *starlarkParser) skipExpression() (string, bool) {
	start := p.i
	depth := 0
	for !p.eof() {
		switch c := p.src[p.i]; c {
		case '(', '[', '{':
			if depth >= maxStarlarkDepth {
				p.diag.note(p.line, "nesting deeper than the reader follows")
				return p.src[start:p.i], false
			}
			depth++
			p.next()
		case ')', ']', '}':
			if depth == 0 {
				// The bracket closing the argument list this expression sits in. Left for
				// the caller, which is what makes the recursion terminate.
				return p.src[start:p.i], true
			}
			depth--
			p.next()
		case ',':
			if depth == 0 {
				return p.src[start:p.i], true
			}
			p.next()
		case '"', '\'':
			p.stringLit()
		case '#':
			p.skipLineComment()
		default:
			p.next()
		}
	}
	return p.src[start:p.i], false
}

// skipToClose consumes up to and including the matching closer, used when a depth guard
// fires. Reports whether it found one.
func (p *starlarkParser) skipToClose(closer byte) bool {
	depth := 1
	for !p.eof() {
		switch c := p.src[p.i]; c {
		case '"', '\'':
			p.stringLit()
		case '#':
			p.skipLineComment()
		case '(', '[', '{':
			depth++
			p.next()
		case ')', ']', '}':
			p.next()
			if c == closer {
				depth--
				if depth == 0 {
					return true
				}
			}
		default:
			p.next()
		}
	}
	return false
}

// stringLit reads a string literal and returns its content, the quote already at p.i.
//
// Handles Starlark's four forms: single and double quoted, each optionally tripled. A raw
// string's `r` prefix is consumed by the caller's identifier scan or by skipExpression, so
// what arrives here always starts at the quote — and a raw string's backslashes are literal,
// which matters not at all for the labels this reads.
func (p *starlarkParser) stringLit() string {
	q := p.next()
	// A triple quote, which spans lines. Checked before the single-quote loop because `"""`
	// would otherwise read as an empty string followed by a quote.
	if p.i+1 < len(p.src) && p.src[p.i] == q && p.src[p.i+1] == q {
		p.next()
		p.next()
		closer := strings.Repeat(string(q), 3)
		start := p.i
		idx := strings.Index(p.src[start:], closer)
		if idx < 0 {
			for !p.eof() {
				p.next()
			}
			p.diag.malformed(p.line, "unterminated triple-quoted string")
			return p.src[start:]
		}
		body := p.src[start : start+idx]
		for p.i < start+idx+3 {
			p.next()
		}
		return body
	}
	var sb strings.Builder
	for !p.eof() {
		c := p.next()
		switch c {
		case q:
			return sb.String()
		case '\\':
			if !p.eof() {
				sb.WriteByte(p.next())
			}
		case '\n':
			// An unescaped newline ends a single-quoted string in Starlark, and treating it
			// as content would swallow the rest of the file into one label.
			p.diag.malformed(p.line-1, "unterminated string")
			return sb.String()
		default:
			sb.WriteByte(c)
		}
	}
	p.diag.malformed(p.line, "unterminated string")
	return sb.String()
}

func (p *starlarkParser) identifier() string {
	start := p.i
	for !p.eof() {
		c := p.src[p.i]
		if isStarlarkIdentStart(c) || (c >= '0' && c <= '9') {
			p.i++
			continue
		}
		break
	}
	return p.src[start:p.i]
}

func isStarlarkIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// skipSpaceInLine skips spaces and tabs but not newlines, used where a construct must
// continue on the same line — `name` then `(`, `name` then `=`.
func (p *starlarkParser) skipSpaceInLine() {
	for !p.eof() {
		switch p.src[p.i] {
		case ' ', '\t', '\r':
			p.next()
		default:
			return
		}
	}
}

// skipSpaceAndComments skips whitespace including newlines, plus comments. Correct inside an
// argument list, where a newline is not a statement boundary — Python's implicit line
// continuation inside brackets, which Starlark inherits and which every multi-line rule call
// depends on.
func (p *starlarkParser) skipSpaceAndComments() {
	for !p.eof() {
		switch p.src[p.i] {
		case ' ', '\t', '\r', '\n':
			p.next()
		case '#':
			p.skipLineComment()
		default:
			return
		}
	}
}

func (p *starlarkParser) skipLineComment() {
	for !p.eof() && p.src[p.i] != '\n' {
		p.next()
	}
}

// matchBazel claims Bazel's build files.
//
// The four fixed names plus `.bzl`. `BUILD` with no extension is the older spelling and
// still the more common one in large repositories, and it is matched exactly rather than
// case-insensitively: Bazel itself accepts only `BUILD` and `BUILD.bazel`, and a
// case-insensitive match would claim a `build` directory's file on a case-insensitive
// filesystem — where discover already reports paths as the filesystem spells them.
func matchBazel(f discover.File) bool {
	switch base := path.Base(f.Path); base {
	case "BUILD", "BUILD.bazel", "WORKSPACE", "WORKSPACE.bazel", "WORKSPACE.bzlmod",
		"MODULE.bazel", "REPO.bazel":
		return true
	default:
		return strings.EqualFold(path.Ext(base), ".bzl")
	}
}

// IsBazelWorkspaceRoot reports whether a path is the file that marks a directory as the root
// of a Bazel workspace — which is what a `//pkg` label is relative to.
//
// Exported because assemble needs the rule and this reader cannot apply it: reading
// `go/greeter/BUILD.bazel` alone says nothing about where the root above it is, so the label
// is recorded workspace-relative and joined against the root there. Keeping the rule here
// rather than restating the filenames in assemble is the point — it is Bazel's rule, and two
// copies of it would be two things to keep in step with matchBazel above.
//
// `REPO.bazel` is deliberately not a root. It carries repository-wide settings and Bazel
// requires it to sit *beside* a WORKSPACE or MODULE.bazel rather than marking a root itself,
// so treating it as one would put a root where Bazel sees none.
func IsBazelWorkspaceRoot(p string) bool {
	switch path.Base(p) {
	case "WORKSPACE", "WORKSPACE.bazel", "WORKSPACE.bzlmod", "MODULE.bazel":
		return true
	}
	return false
}
