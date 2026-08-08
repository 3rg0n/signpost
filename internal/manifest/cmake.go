package manifest

import (
	"path"
	"strconv"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// CMake extraction.
//
// CMake is the first build system signpost reads whose whole content is *commands*. A
// go.mod is a list of declarations, a compose file is a tree of values, and a Terraform
// file is a set of blocks — each states what is, and a reader takes it at its word. A
// CMakeLists.txt states what to do: it is an imperative script in a language with
// variables, conditionals, functions, and macros, and the build graph is what falls out of
// running it. Nothing short of CMake itself computes that graph.
//
// So this reads the subset that is a declaration in practice: the command name plus its
// literal arguments, for the dozen commands whose first argument names something. That is a
// real limit and it is the honest one — `add_executable(${APP_NAME} main.c)` names a target
// this reader cannot know the name of, and it says so rather than recording `${APP_NAME}`
// as though a target were called that.
//
// What CMake states that nothing else in a C or C++ repository does:
//
//   - Which targets exist, and which are libraries and which are executables. A C
//     repository has no manifest naming its own units — that is exactly the gap ADR 0017
//     and resolveC's comment describe — so `add_executable(app main.c)` is the only
//     statement that `app` is a program rather than a directory of sources.
//   - Which of those targets are tests. `add_test` is the only place a C project says a
//     binary is a test, since the language has no test convention the toolchain enforces
//     and isTestPath has to guess from directory names.
//   - Which dependencies come from outside. `find_package(OpenSSL REQUIRED)` and
//     `FetchContent_Declare` are a C project's nearest equivalent to a dependency
//     manifest, and until now a repository whose entire supply chain was declared there
//     read as declaring nothing.
//   - Which include directories the build adds. `target_include_directories` and
//     `include_directories` are the real `-I` list, which resolveC's comment names as the
//     thing it guesses at with `include/`, `src/`, and the project directory. Read and
//     deliberately not recorded, for the reason stated where those commands are matched.
//
// Not read, and each for a stated reason rather than as a shortcut: conditionals, because
// a target inside `if(WIN32)` exists on one platform and the bundle describes a repository
// rather than a build of it, so both arms are read and the target recorded once;
// `function` and `macro` bodies, which are definitions of commands rather than uses of
// them; and generator expressions (`$<TARGET_FILE:app>`), which are evaluated by the
// generator after this file has been fully processed.

// ExtractCMake reads a CMakeLists.txt or a .cmake module.
func ExtractCMake(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindCMake}
	cmds, diag := parseCMake(f.Content)
	facts.applyDiag(diag)

	// A target declared by `add_library` and later named by `target_link_libraries` is one
	// target, and the second command is where its dependencies are. So the declarations are
	// collected first and the links applied against them, which also decides the one
	// question `target_link_libraries` cannot answer on its own: whether the name it links
	// is a target in this project or a package from outside.
	//
	// Packages found by `find_package` go in the same set as targets built by
	// `add_library`, because what the set means is "already accounted for", not "built
	// here". A `find_package(OpenSSL)` followed by a link to `OpenSSL::SSL` is one
	// dependency declared once and used once; recording the use as well would list OpenSSL
	// twice — with two different scopes, which is what stops Normalize from folding them —
	// and report a supply chain larger than the one the file declares.
	declared := map[string]bool{}
	var links []cmakeLink

	for _, c := range cmds {
		switch c.name {
		case "project":
			// The project name is the closest thing a C build has to a module identity, and
			// `project()` is required to appear before anything else — so the first one
			// wins and a nested project's own call does not overwrite it. A subdirectory
			// that calls `project()` again is a real pattern in vendored trees, and taking
			// the last would name the whole repository after whatever it vendored.
			if facts.Module.Name == "" {
				if name := c.literal(0); name != "" {
					facts.Module.Name = name
					facts.Module.Ecosystem = ecoCMake
					facts.Module.Line = c.line
					// `project(foo VERSION 1.2.3)`, which is where a C project states a
					// version at all. Read from the keyword rather than by position: every
					// other argument to project() is a keyword pair too, and LANGUAGES may
					// come first.
					facts.Module.Version = c.keyword("VERSION")
				}
			}
		case "add_executable", "add_library":
			if t, ok := readCMakeTarget(&facts, c); ok {
				declared[t] = true
				// Recorded for the *other* CMakeLists.txt files in this tree, which cannot
				// see this declaration and will read a link to this name as a package from
				// outside. Only targets go in Facts.Targets, not the whole `declared` set: a
				// found package is accounted for here but is genuinely external, and a
				// sibling file linking it is stating a real dependency.
				facts.Targets = append(facts.Targets, t)
			}
		case "add_test":
			// A test's name is a keyword argument (`add_test(NAME x COMMAND y)`) in the
			// modern form and positional in the old one, and both are in wide use.
			name := c.keyword("NAME")
			if name == "" {
				name = c.literal(0)
			}
			if name != "" {
				facts.Scripts = append(facts.Scripts, Script{
					Name: name, Command: strings.Join(c.literalsFrom(1), " "), Line: c.line,
				})
			}
		case "find_package":
			if name := readCMakeFindPackage(&facts, c); name != "" {
				declared[name] = true
			}
		case "FetchContent_Declare", "ExternalProject_Add":
			readCMakeFetchContent(&facts, c)
		case "pkg_check_modules", "pkg_search_module":
			// pkg-config, which is how a Unix C project finds a system library. The first
			// argument is the variable prefix the results land in, not a package, so the
			// packages are everything after it.
			for _, a := range c.literalsFrom(1) {
				if isCMakeKeyword(a) {
					continue
				}
				// `libssl>=3.0` is one argument holding a name and a constraint, which is
				// pkg-config's own spelling.
				name, ver := splitPkgConfigModule(a)
				facts.Deps = append(facts.Deps, Dep{
					Name: name, Version: ver, Scope: ScopeRuntime,
					Ecosystem: ecoPkgConfig, Line: c.line,
				})
			}
		case "target_link_libraries", "link_libraries":
			// Deferred: whether a linked name is a sibling target or an external package
			// depends on every declaration in the file, including ones below this line.
			links = append(links, cmakeLink{args: c.literalsFrom(0), line: c.line})
		case "add_subdirectory":
			// The build's own composition, and the only statement of it. Recorded as a local
			// dependency for the same reason a Terraform `module` block with a `./` source
			// is: it names a directory in this repository whose code this build includes, so
			// addDeclaredDepEdges draws the edge onto the module holding it. Local is set
			// unconditionally because add_subdirectory takes nothing else — its argument is
			// a path, always, which is the difference from Terraform's ambiguous `source`.
			if dir := c.literal(0); dir != "" {
				resolved := path.Join(path.Dir(f.Path), dir)
				facts.Deps = append(facts.Deps, Dep{
					Name: path.Base(resolved), Scope: ScopeBuild, Ecosystem: ecoCMake,
					Source: resolved, Local: true, Line: c.line,
				})
			}
		case "include_directories", "target_include_directories":
			// Matched deliberately, and deliberately recording nothing.
			//
			// `target_include_directories(app PRIVATE include)` is the real `-I` list, and
			// resolveC's comment names the absence of it as the reason it guesses with
			// `include/`, `src/`, and the project directory. So this looks like the fix for
			// a stated limitation.
			//
			// It is not, because Facts has nowhere to put it. Resolution exists and is
			// documented as tsconfig's alias mapping — a pattern-to-target map — and an
			// include path is a bare directory list with no patterns in it. Writing one into
			// the other would give the field two meanings, and the resolver reading it would
			// have to know which reader wrote it. Landing it properly means a field of its
			// own plus a resolveC that consumes it, which is a change to C *resolution*
			// rather than to CMake reading and wants its own measurement of how many imports
			// it moves out of the gap report.
			//
			// The case is here rather than absent so that this is a decision on the record
			// instead of a command nobody considered.
		}
	}

	applyCMakeLinks(&facts, links, declared)
	return facts
}

// ecoCMake and ecoPkgConfig name the ecosystems CMake dependencies belong to.
//
// Two rather than one, because they are two different supply chains with two different
// remediation stories. A `find_package(OpenSSL)` is satisfied by whatever the machine has
// installed — a distro package, a vcpkg port, a Conan recipe — while a
// `pkg_check_modules(SSL libssl)` names a pkg-config module by its own registry's name.
// Folding them would report one dependency where a repository declares two lookups that can
// resolve to different versions of the same library.
const (
	ecoCMake     = "cmake"
	ecoPkgConfig = "pkg-config"
)

// cmakeLink is a deferred `target_link_libraries` call.
type cmakeLink struct {
	args []string
	line int
}

// readCMakeTarget records an `add_executable` or `add_library` declaration.
//
// The target is an Entrypoint when it is an executable and not when it is a library, which
// is the one distinction this pair of commands exists to make. A library is linked into
// something else; an executable is a thing a person or a container runs, and §4.1 asks for
// exactly that list.
func readCMakeTarget(facts *Facts, c cmakeCommand) (string, bool) {
	name := c.literal(0)
	if name == "" {
		// `add_executable(${APP_NAME} main.c)`. The target is real and its name is
		// computed, so recording the literal `${APP_NAME}` would put a target in the
		// bundle that no build ever produces. Noted as unread instead.
		facts.markIncomplete("target name computed at line " + strconv.Itoa(c.line))
		return "", false
	}
	// An IMPORTED or ALIAS library declares no sources: the first names a prebuilt artifact
	// found elsewhere and the second is a second name for a target already declared.
	// Recording either as a unit of this repository would claim it is built here.
	if c.hasKeyword("IMPORTED") || c.hasKeyword("ALIAS") {
		return name, true
	}
	if c.name == "add_executable" {
		// The sources, joined, are what the entrypoint runs. Path rather than Name carries
		// them because Name is the target and the Entrypoint contract puts the artifact in
		// Path — the same shape the Makefile reader gives a phony target.
		facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
			Name: name, Path: strings.Join(cmakeSourceArgs(c), " "), Line: c.line,
		})
	}
	return name, true
}

// cmakeSourceArgs returns the literal source files of a target declaration, dropping the
// keywords that are not sources. STATIC, SHARED, and the rest are library kinds; EXCLUDE_FROM_ALL
// is a build flag.
func cmakeSourceArgs(c cmakeCommand) []string {
	var out []string
	for _, a := range c.literalsFrom(1) {
		if isCMakeKeyword(a) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// readCMakeFindPackage records a `find_package` call as an external dependency.
//
// This is a C project's dependency declaration, and the version is the second positional
// argument when one is given — `find_package(Boost 1.83 REQUIRED)`. REQUIRED and the
// component list say how the lookup behaves rather than what it looks for, so neither
// changes the dependency's identity.
//
// COMPONENTS are deliberately not recorded as separate dependencies. `find_package(Qt6
// COMPONENTS Widgets Network)` is one package with two of its parts requested, and
// reporting three dependencies would treble a repository's declared supply chain — the
// same over-count the Terraform reader avoids by folding a `provider` into its
// `required_providers` entry.
//
// Returns the package name, so the caller can record that this file has accounted for it and
// a later link to `Name::Component` adds nothing.
func readCMakeFindPackage(facts *Facts, c cmakeCommand) string {
	name := c.literal(0)
	if name == "" {
		return ""
	}
	d := Dep{Name: name, Scope: ScopeRuntime, Ecosystem: ecoCMake, Line: c.line}
	if v := c.literal(1); v != "" && !isCMakeKeyword(v) && isCMakeVersion(v) {
		d.Version = v
	}
	// A package the build tolerates the absence of is optional in the sense Dep.Optional
	// means: the code compiles without it, behind a feature test. QUIET is not that — it
	// only silences the search — so REQUIRED's absence alone is what marks it, and only
	// when the call did not say REQUIRED at all.
	d.Optional = !c.hasKeyword("REQUIRED")
	facts.Deps = append(facts.Deps, d)
	return name
}

// readCMakeFetchContent records a `FetchContent_Declare` or `ExternalProject_Add` call.
//
// This is the one place a CMake project pins a dependency to an exact revision, so it is
// the highest-value supply-chain fact in the file: a `GIT_TAG` is a version constraint in
// the sense Dep.Version means, and a `GIT_REPOSITORY` is a non-registry origin in the sense
// Dep.Source means — a dependency with no registry to publish an advisory against, which is
// what the Dep.Source comment is about.
func readCMakeFetchContent(facts *Facts, c cmakeCommand) {
	name := c.literal(0)
	if name == "" {
		return
	}
	d := Dep{Name: name, Scope: ScopeBuild, Ecosystem: ecoCMake, Line: c.line}
	// GIT_TAG holds a tag, a branch, or a commit sha, and all three are recorded as
	// written for the reason the Dep.Version comment gives: the constraint the repository
	// states is the fact, and resolving it is the lock file's business.
	d.Version = firstNonEmpty(c.keyword("GIT_TAG"), c.keyword("URL_HASH"), c.keyword("SVN_REVISION"))
	d.Source = firstNonEmpty(c.keyword("GIT_REPOSITORY"), c.keyword("URL"), c.keyword("SVN_REPOSITORY"))
	facts.Deps = append(facts.Deps, d)
}

// applyCMakeLinks turns deferred `target_link_libraries` calls into dependencies.
//
// The first argument is the target doing the linking and the rest are what it links, so a
// name is only a dependency when it is not a target this file declared. That test is the
// whole reason the calls are deferred: `target_link_libraries(app PRIVATE core)` says
// nothing about whether `core` is a sibling library or an installed package, and the answer
// is in the `add_library` call — which CMake permits below this line, and which real
// projects routinely put in a subdirectory processed later.
//
// A name this file did not declare is recorded as an external dependency, and the risk in
// that direction is understood: a target declared in a *different* CMakeLists.txt of the
// same project reads as external. That is the honest reading of one file, which is the unit
// a Reader sees (registry.go), and the assembler is where a cross-file view would belong.
// The alternative — dropping every unrecognised name — loses the actual dependency
// declarations, which is the fact this reader exists to surface.
func applyCMakeLinks(facts *Facts, links []cmakeLink, declared map[string]bool) {
	for _, l := range links {
		for i, a := range l.args {
			// The linking target itself, not a dependency. `link_libraries` has no such
			// argument, but it is deprecated and rare enough that treating its first
			// argument as the target too would misread it in only the same direction the
			// comment above accepts.
			if i == 0 {
				continue
			}
			if isCMakeKeyword(a) {
				continue
			}
			// A namespaced target — `OpenSSL::SSL`, `Qt6::Widgets` — names a package this
			// file has already found, so the package is the dependency and the component
			// after `::` is which part of it. Reduced to the package for the same reason
			// COMPONENTS are: one dependency, not two.
			//
			// The reduction comes before the declared check, not after, because that check is
			// what stops a found package being recorded twice. `find_package(OpenSSL)`
			// declares OpenSSL and the link names OpenSSL::SSL, so testing the label as
			// written would find nothing and add a second OpenSSL with a different scope —
			// which Normalize keeps apart, since scope is part of a dependency's identity.
			name := a
			if i := strings.Index(name, "::"); i > 0 {
				name = name[:i]
			}
			if declared[name] {
				continue
			}
			facts.Deps = append(facts.Deps, Dep{
				Name: name, Scope: ScopeBuild, Ecosystem: ecoCMake, Line: l.line,
			})
		}
	}
}

// cmakeCommand is one `name(arg arg arg)` invocation.
type cmakeCommand struct {
	name string
	args []cmakeArg
	line int
}

// cmakeArg is one argument, with whether it was quoted.
//
// Quoting matters for exactly one thing: a quoted argument is a single argument even when
// it holds spaces, so `"a b"` is one source file and `a b` is two. It does not change
// whether a `${}` is expanded — CMake expands variables inside quotes too, which is why
// every caller here tests for `$` rather than for quoting.
type cmakeArg struct {
	text   string
	quoted bool
}

// literal returns the i-th argument when it was written as a literal this reader can trust,
// and "" otherwise — a missing argument, or one holding a variable reference.
//
// Collapsing those into one empty result is the same decision hclBlock.stringAttr makes:
// every caller is asking "did the author state this outright", and `${TARGET}` is an
// instruction to compute a name rather than a name.
func (c cmakeCommand) literal(i int) string {
	if i < 0 || i >= len(c.args) {
		return ""
	}
	a := c.args[i]
	if strings.Contains(a.text, "$") {
		return ""
	}
	return a.text
}

// literalsFrom returns every literal argument from index i onward, skipping the computed
// ones. Used where the command takes a list and a single unreadable entry should not
// discard the rest.
func (c cmakeCommand) literalsFrom(i int) []string {
	if i < 0 {
		i = 0
	}
	out := make([]string, 0, len(c.args))
	for ; i < len(c.args); i++ {
		if t := c.args[i].text; t != "" && !strings.Contains(t, "$") {
			out = append(out, t)
		}
	}
	return out
}

// keyword returns the argument following the named keyword, which is how CMake spells a
// named parameter: `FetchContent_Declare(googletest GIT_TAG v1.14.0)`.
//
// The value is returned even when it holds a `$`, unlike literal: a `GIT_TAG ${VERSION}` is
// a version this reader cannot resolve, and the callers that use keyword put their result in
// a Version or Source field where the text as written is what the Dep contract asks for.
// Callers that need a name still go through literal.
func (c cmakeCommand) keyword(kw string) string {
	for i := 0; i+1 < len(c.args); i++ {
		if c.args[i].text == kw && !c.args[i].quoted {
			return c.args[i+1].text
		}
	}
	return ""
}

// hasKeyword reports whether the command carries a bare keyword such as REQUIRED.
func (c cmakeCommand) hasKeyword(kw string) bool {
	for _, a := range c.args {
		if a.text == kw && !a.quoted {
			return true
		}
	}
	return false
}

// cmakeKeywords are argument words that modify a command rather than name something.
//
// The list is deliberately short and all-caps, which is CMake's own convention for a
// keyword and the thing that makes this safe: a source file is never called PRIVATE, and a
// package is never called REQUIRED. A lowercase word is always a name.
var cmakeKeywords = map[string]bool{
	"PRIVATE": true, "PUBLIC": true, "INTERFACE": true,
	"STATIC": true, "SHARED": true, "MODULE": true, "OBJECT": true,
	"IMPORTED": true, "ALIAS": true, "GLOBAL": true,
	"REQUIRED": true, "QUIET": true, "COMPONENTS": true, "OPTIONAL_COMPONENTS": true,
	"EXCLUDE_FROM_ALL": true, "WIN32": true, "MACOSX_BUNDLE": true,
	"CONFIG": true, "NO_MODULE": true, "NAMES": true, "PATHS": true,
	"NAME": true, "COMMAND": true, "WORKING_DIRECTORY": true,
	"CONFIGURATIONS": true, "REUSE_FROM": true, "NO_DEFAULT_PATH": true,
	"VERSION": true, "LANGUAGES": true, "DESCRIPTION": true, "HOMEPAGE_URL": true,
	"IMPORTED_TARGET": true,
}

func isCMakeKeyword(s string) bool { return cmakeKeywords[s] }

// isCMakeVersion reports whether an argument looks like a version rather than a keyword
// this reader does not know.
//
// A digit-leading, digit-and-dot string. Needed because find_package's second positional
// argument is a version *or* absent, and an unknown all-caps keyword sitting there would
// otherwise be recorded as one — a dependency pinned to "EXACT" reads as a real constraint
// and would be wrong in the direction that matters, since a version is what a reader
// auditing dependencies acts on.
func isCMakeVersion(s string) bool {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && c != '.' {
			return false
		}
	}
	return true
}

// splitPkgConfigModule splits a pkg-config module spec into its name and constraint.
//
// pkg-config writes the two together — `libssl>=3.0` — which is its own spelling and not
// CMake's. Recorded apart because Dep keeps them apart, and because a dependency whose Name
// held the operator would never match the same library declared elsewhere without one.
func splitPkgConfigModule(s string) (string, string) {
	if i := strings.IndexAny(s, "<>="); i > 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i:])
	}
	return s, ""
}

// maxCMakeArgs bounds one command's argument list.
//
// A guard on input rather than on style, the same one maxHCLDepth is: a generated
// CMakeLists.txt listing every file in a large tree is a real artifact, and an unbounded
// list would hold the whole of it in memory for a command whose facts are in its first few
// arguments. Past the limit the arguments are dropped and the command still reads, which is
// the reading that loses least — a target's name is argument zero.
const maxCMakeArgs = 4096

// parseCMake reads a file into its top-level command invocations.
//
// The grammar this needs is small and, unusually for a build format, fully documented as
// regular: a CMake file is a sequence of `identifier(arguments)`, where the arguments are
// whitespace-separated tokens, bracket-quoted (`[[...]]`), quoted with backslash escapes,
// or bare. Comments run from an unquoted `#` to end of line. Nothing here is ambiguous, so
// the parser is exact for the structure and makes no attempt at the semantics — which is
// the same split parseHCL makes and for the same reason.
//
// Never returns an error, matching the Reader contract: a file that goes wrong halfway
// yields the commands before it plus a Diag saying where it stopped.
func parseCMake(src string) ([]cmakeCommand, Diag) {
	p := &cmakeParser{src: src, line: 1}
	return p.parse()
}

type cmakeParser struct {
	src  string
	i    int
	line int
	diag Diag
}

func (p *cmakeParser) eof() bool { return p.i >= len(p.src) }

// next consumes one byte and is the only place the line counter moves, so a newline inside
// a quoted argument counts exactly like any other.
func (p *cmakeParser) next() byte {
	c := p.src[p.i]
	p.i++
	if c == '\n' {
		p.line++
	}
	return c
}

func (p *cmakeParser) parse() ([]cmakeCommand, Diag) {
	var out []cmakeCommand
	for {
		p.skipBlankAndComments()
		if p.eof() {
			return out, p.diag
		}
		line := p.line
		name := p.identifier()
		if name == "" {
			// Not an identifier where a command must start. Consumed one byte and moved on
			// rather than stopping: a stray character is a broken line, not a broken file,
			// and every command after it still reads.
			p.next()
			continue
		}
		p.skipBlankAndComments()
		if p.eof() || p.src[p.i] != '(' {
			p.diag.malformed(line, "command "+name+" with no argument list")
			continue
		}
		p.next()
		args, ok := p.arguments()
		if !ok {
			p.diag.malformed(line, "unterminated argument list for "+name)
			return append(out, cmakeCommand{name: name, args: args, line: line}), p.diag
		}
		out = append(out, cmakeCommand{name: name, args: args, line: line})
	}
}

// identifier reads a command name. CMake command names are case-insensitive, and this
// preserves the case as written — the caller switches on the canonical spelling, and
// canonicalising here would hide which spelling a file used from a diagnostic.
func (p *cmakeParser) identifier() string {
	start := p.i
	for !p.eof() {
		c := p.src[p.i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
			p.i++
			continue
		}
		break
	}
	return p.src[start:p.i]
}

// arguments reads a command's argument list up to its closing paren.
//
// Nested parens are counted rather than treated as terminators: `if(NOT (A AND B))` is one
// command whose arguments contain parens, and stopping at the first `)` would leave the
// rest of the line to be read as a command name.
func (p *cmakeParser) arguments() ([]cmakeArg, bool) {
	var out []cmakeArg
	depth := 1
	for {
		p.skipArgSpace()
		if p.eof() {
			return out, false
		}
		switch c := p.src[p.i]; c {
		case ')':
			p.next()
			depth--
			if depth == 0 {
				return out, true
			}
			continue
		case '(':
			p.next()
			depth++
			continue
		case '"':
			p.next()
			out = p.appendArg(out, p.quoted(), true)
			continue
		case '[':
			// A bracket argument, `[[...]]` or `[=[...]=]`, which is CMake's raw string:
			// nothing inside it is escaped or expanded. Only a real bracket opener is one —
			// a lone `[` is a bare argument, and treating it as an opener would swallow the
			// rest of the file.
			if body, ok, isBracket := p.bracket(); isBracket {
				if !ok {
					return out, false
				}
				out = p.appendArg(out, body, true)
				continue
			}
		}
		// This loop terminates because bare() stops only on characters the cases above consume,
		// so every byte reaching here is consumed and the position always advances. The
		// Starlark reader needs an explicit guard for this — its expression scanner declines
		// any closer — and TestParseCMakeTerminatesOnEveryInput holds the property here.
		out = p.appendArg(out, p.bare(), false)
	}
}

// appendArg adds one argument, dropping it past the cap. Empty arguments are kept when they
// were quoted — `""` is a deliberate empty string in a source list — and dropped otherwise,
// since a bare empty token is a scanner artifact rather than something written.
func (p *cmakeParser) appendArg(out []cmakeArg, text string, quoted bool) []cmakeArg {
	if text == "" && !quoted {
		return out
	}
	if len(out) >= maxCMakeArgs {
		p.diag.note(p.line, "argument list longer than the reader follows")
		return out
	}
	return append(out, cmakeArg{text: text, quoted: quoted})
}

// quoted reads a double-quoted argument, the opening quote already consumed.
//
// A backslash escapes the next byte, including a quote and a newline — the second is how a
// long argument is continued, and a reader that stopped at it would take the rest of the
// file as part of the string.
func (p *cmakeParser) quoted() string {
	var sb strings.Builder
	for !p.eof() {
		c := p.next()
		switch c {
		case '"':
			return sb.String()
		case '\\':
			if !p.eof() {
				sb.WriteByte(p.next())
			}
		default:
			sb.WriteByte(c)
		}
	}
	// Unterminated. What was read is returned rather than discarded, and the caller's
	// end-of-file check is what records it.
	return sb.String()
}

// bracket reads a bracket argument. The third result reports whether this was one at all,
// which is what lets a lone `[` fall through to the bare-argument scanner.
func (p *cmakeParser) bracket() (string, bool, bool) {
	// `[`, then any number of `=`, then `[`. Anything else is not a bracket opener.
	j := p.i + 1
	for j < len(p.src) && p.src[j] == '=' {
		j++
	}
	if j >= len(p.src) || p.src[j] != '[' {
		return "", false, false
	}
	closer := "]" + strings.Repeat("=", j-p.i-1) + "]"
	for p.i <= j {
		p.next()
	}
	start := p.i
	idx := strings.Index(p.src[start:], closer)
	if idx < 0 {
		for !p.eof() {
			p.next()
		}
		return p.src[start:], false, true
	}
	body := p.src[start : start+idx]
	for p.i < start+idx+len(closer) {
		p.next()
	}
	return body, true, true
}

// bare reads an unquoted argument, stopping at whitespace, a paren, a quote, or a comment.
func (p *cmakeParser) bare() string {
	var sb strings.Builder
	for !p.eof() {
		c := p.src[p.i]
		switch c {
		case ' ', '\t', '\r', '\n', '(', ')', '"', '#':
			return sb.String()
		case '\\':
			p.next()
			if !p.eof() {
				sb.WriteByte(p.next())
			}
			continue
		}
		sb.WriteByte(p.next())
	}
	return sb.String()
}

// skipArgSpace skips whitespace and comments between arguments. A `#` inside an argument
// list is still a comment — CMake allows one — which is why this is not just a whitespace
// skip.
func (p *cmakeParser) skipArgSpace() {
	for !p.eof() {
		switch p.src[p.i] {
		case ' ', '\t', '\r', '\n':
			p.next()
		case '#':
			p.skipComment()
		default:
			return
		}
	}
}

func (p *cmakeParser) skipBlankAndComments() {
	for !p.eof() {
		switch p.src[p.i] {
		case ' ', '\t', '\r', '\n':
			p.next()
		case '#':
			p.skipComment()
		default:
			return
		}
	}
}

// skipComment consumes a comment. A `#[[...]]` bracket comment spans lines, which is the
// case a to-end-of-line skip gets wrong: the lines below it would be read as commands.
func (p *cmakeParser) skipComment() {
	p.next()
	if !p.eof() && p.src[p.i] == '[' {
		if _, _, isBracket := p.bracket(); isBracket {
			return
		}
	}
	for !p.eof() && p.src[p.i] != '\n' {
		p.next()
	}
}

// matchCMake claims CMake's build files.
//
// Two shapes, and the second is why this is not a basename match. `CMakeLists.txt` is fixed
// by the toolchain and appears once per directory; a `.cmake` file is a module whose name
// the author chooses — `cmake/FindFoo.cmake`, `cmake/CompilerWarnings.cmake` — and it holds
// the same commands, including the `find_package` calls a project factors out of its
// top-level file.
func matchCMake(f discover.File) bool {
	base := path.Base(f.Path)
	return base == "CMakeLists.txt" || strings.EqualFold(path.Ext(base), ".cmake")
}
