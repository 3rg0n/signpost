package assemble

import (
	"path"
	"sort"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
	"github.com/3rg0n/signpost/internal/extract"
	"github.com/3rg0n/signpost/internal/manifest"
)

// resolver answers the one question graph assembly cannot avoid: given an import
// string written in a file, what does it point at?
//
// Three outcomes, and the third is as important as the other two:
//
//  1. a directory in this repository, which becomes an internal edge;
//  2. a dependency the repository declares in a manifest, which becomes an edge to
//     an external node;
//  3. neither, which is recorded and reported rather than guessed at.
//
// The third case is where a resolver is tempted to invent. `import os` in Python and
// `import "fmt"` in Go are neither internal nor declared, and turning them into
// external-dependency nodes would fill a repo's supply-chain view with things nobody
// can patch or has to. So an external node exists only where a *manifest* says the
// dependency exists — the manifest is the repository's own statement about what it
// depends on, and it is the only authoritative one. A bare import that matches no
// declared dependency is counted as unresolved.
type resolver struct {
	// moduleIDs maps a directory to the module node that covers it. Populated by the
	// builder, which owns ID assignment, before any import is resolved: resolution
	// needs every module to exist already, since an import routinely points backwards
	// in path order.
	moduleIDs map[string]string
	// srcFiles is every source file path, used to resolve a dotted or relative
	// import to the file it names before falling back to a directory.
	srcFiles map[string]bool
	// goMods maps a directory to the module path declared there, longest path first
	// so a nested module wins over the one that contains it.
	goMods []goModule
	// npmPkgs maps a directory to the package name declared there, longest name
	// first so `@scope/core-utils` wins over `@scope/core`.
	npmPkgs []npmPkg
	// tsConfigs are the resolution mappings read from tsconfig files, keyed by the
	// config's own path so `extends` can be followed.
	tsConfigs map[string]manifest.Resolution
	// tsAliases are the alias patterns in effect, most specific first. Flattened from
	// tsConfigs once every config is registered, since a config may extend one that has
	// not been read yet.
	tsAliases []tsAlias
	// crates are Cargo manifest directories, longest first, same reason.
	crates []string
	// pyRoots are directories holding a Python package manifest, longest first: each is
	// a path an absolute import resolves against for the files beneath it.
	pyRoots []string
	// jvmPkgs maps a declared JVM package name to the directory declaring it, longest
	// name first so a subpackage wins over the package containing it.
	jvmPkgs []jvmPackage
	// deps maps a normalized dependency key to the external node it belongs to.
	deps map[string]string
	// ecosystems are the ecosystems any manifest declared, sorted, for the
	// cross-ecosystem fallback in depOrEmpty.
	ecosystems []string
}

type goModule struct {
	dir  string
	path string
}

type npmPkg struct {
	dir  string
	name string
}

// jvmPackage is one `package` declaration and the directory the file declaring it sits
// in. Both halves are needed and neither can be derived from the other: the name is
// what an import writes, and the directory is what a module node is keyed on.
type jvmPackage struct {
	dir  string
	name string
	// test marks a directory where every file declaring this package is a test. The
	// standard JVM layout declares each package twice — `src/main/java/com/x` and
	// `src/test/java/com/x` — so two directories answer to the same name and only one
	// of them is what another module's import means.
	test bool
}

// tsAlias is one `paths` entry, flattened for matching.
type tsAlias struct {
	// scope is the directory the declaring config governs — its own directory. An alias
	// is not repo-wide: `@src/*` declared in packages/demo/tsconfig.json means
	// packages/demo/src, and applying it to a file in another package would resolve an
	// import to code that package cannot see.
	scope string
	// prefix and suffix are the pattern either side of its `*`. A pattern without one is
	// an exact specifier, matched whole, with wild false.
	prefix string
	suffix string
	wild   bool
	// targets are the mapped directories, repo-relative, in declaration order — the
	// order TypeScript tries them in.
	targets []string
}

// pyExts, tsExts and rustExts are the file spellings a module path may resolve to,
// in the order a toolchain would try them.
var (
	pyExts   = []string{".py", ".pyi"}
	tsExts   = []string{".ts", ".tsx", ".d.ts", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"}
	tsIndex  = []string{"index.ts", "index.tsx", "index.mts", "index.cts", "index.js", "index.jsx", "index.mjs", "index.cjs"}
	rustExts = []string{".rs"}
)

func newResolver() *resolver {
	return &resolver{
		moduleIDs: make(map[string]string),
		srcFiles:  make(map[string]bool),
		deps:      make(map[string]string),
		tsConfigs: make(map[string]manifest.Resolution),
	}
}

// addGoModule records a go.mod's declared module path against its directory.
func (r *resolver) addGoModule(file, modPath string) {
	if modPath == "" {
		return
	}
	r.goMods = append(r.goMods, goModule{dir: dirOf(file), path: modPath})
	sort.Slice(r.goMods, func(i, j int) bool {
		if len(r.goMods[i].path) != len(r.goMods[j].path) {
			return len(r.goMods[i].path) > len(r.goMods[j].path)
		}
		return r.goMods[i].path < r.goMods[j].path
	})
}

// addNpmPackage records a package.json's declared name against its directory.
//
// This is what makes an npm workspace resolve internally, and its absence was a bug
// rather than a gap. A monorepo's packages import each other by published name —
// `import {x} from "@scope/core"`, not by relative path — and with no name-to-directory
// map the resolver had nothing to match, so every cross-package import fell through to
// the declared-dependency lookup and found the `workspace:*` entry sitting in
// package.json. The result was a package in this repository reported as a third-party
// dependency, with the module node for its own source showing no importers at all.
// Measured on a real monorepo before the fix: 60 of 81 scoped "external dependencies"
// were directories in the repository, and 2064 edges pointed at them.
//
// Go has worked correctly all along for exactly this reason — addGoModule gives
// resolveGo a path-to-directory map to consult first. This is the npm equivalent.
func (r *resolver) addNpmPackage(file, name string) {
	if name == "" {
		return
	}
	r.npmPkgs = append(r.npmPkgs, npmPkg{dir: dirOf(file), name: name})
	// Longest name first, so a deep import matches the most specific package that could
	// provide it: with both `@scope/core` and `@scope/core-utils` declared, an import of
	// `@scope/core-utils/x` must not be read as `@scope/core` plus a stray path.
	sort.Slice(r.npmPkgs, func(i, j int) bool {
		if len(r.npmPkgs[i].name) != len(r.npmPkgs[j].name) {
			return len(r.npmPkgs[i].name) > len(r.npmPkgs[j].name)
		}
		return r.npmPkgs[i].name < r.npmPkgs[j].name
	})
}

// npmSibling reports whether an npm dependency name is a package in this repository,
// and returns the module node covering its source if one exists.
//
// Separate from npmWorkspace because the input is a different thing: that resolves an
// import specifier, which may address a path inside a package, while this matches a
// dependency *name* declared in a manifest, which never does. Conflating them would let
// a dependency named `@scope/core` match a package named `@scope` with `/core` treated
// as a subpath.
func (r *resolver) npmSibling(name string) (id string, inRepo bool) {
	for _, p := range r.npmPkgs {
		if p.name != name {
			continue
		}
		if id := r.tsTarget(p.dir); id != "" {
			return id, true
		}
		for _, src := range []string{"src", "lib"} {
			if id := r.tsTarget(path.Join(p.dir, src)); id != "" {
				return id, true
			}
		}
		return "", true
	}
	return "", false
}

// addTSConfig records one tsconfig's resolution mapping.
//
// Recorded rather than flattened, because a config may extend one that has not been read
// yet: the manifest facts arrive in sorted path order, and `packages/a/tsconfig.json`
// extending the root config comes first. flattenTSAliases resolves the inheritance once
// every config is in.
func (r *resolver) addTSConfig(file string, res manifest.Resolution) {
	r.tsConfigs[file] = res
}

// flattenTSAliases turns the registered configs into a match list, resolving `extends`.
//
// Inheritance is not a refinement here. In one real monorepo 11 of 14 tsconfig files
// declare `extends` and most declare nothing else — a package config is often just
// `{"extends": "../../../tsconfig.json", "include": ["src"]}` — so a reader that ignored
// it would find aliases in the root config and never apply them to the packages that
// actually resolve by them.
//
// Ordering is most-specific-first on two axes at once: a deeper scope wins over a
// shallower one, because a package's own config governs its files more closely than the
// root's; and within a scope a longer prefix wins, so `@app/ui/*` is not matched by
// `@app/*` with `ui/` read as part of the wildcard.
func (r *resolver) flattenTSAliases() {
	r.tsAliases = nil
	for _, file := range sortedKeys(r.tsConfigs) {
		scope := dirOf(file)
		for _, a := range r.effectiveAliases(file) {
			prefix, suffix, wild := strings.Cut(a.Pattern, "*")
			r.tsAliases = append(r.tsAliases, tsAlias{
				scope: scope, prefix: prefix, suffix: suffix, wild: wild,
				targets: a.Targets,
			})
		}
	}
	sort.SliceStable(r.tsAliases, func(i, j int) bool {
		a, b := r.tsAliases[i], r.tsAliases[j]
		if len(a.scope) != len(b.scope) {
			return len(a.scope) > len(b.scope)
		}
		if a.scope != b.scope {
			return a.scope < b.scope
		}
		if len(a.prefix) != len(b.prefix) {
			return len(a.prefix) > len(b.prefix)
		}
		return a.prefix < b.prefix
	})
}

// effectiveAliases returns a config's own aliases, or the nearest inherited ones.
//
// A config that declares `paths` overrides its parent's entirely rather than merging, which
// is what TypeScript does: `compilerOptions` keys are replaced, not combined. So the walk
// stops at the first config in the chain that declares any.
//
// The visited set is not defensive. A tsconfig chain is written by hand and a cycle is a
// configuration error, but this loop would not terminate on one, and a hang while analysing
// someone else's repository is the worst possible way to report their mistake.
func (r *resolver) effectiveAliases(file string) []manifest.Alias {
	visited := make(map[string]bool, 4)
	for file != "" && !visited[file] {
		visited[file] = true
		res, ok := r.tsConfigs[file]
		if !ok {
			return nil
		}
		if len(res.Aliases) > 0 {
			return res.Aliases
		}
		file = r.tsConfigPath(res.Extends)
	}
	return nil
}

// tsConfigPath resolves an `extends` value to a config this repository holds, or "".
//
// TypeScript permits the extension to omit `.json` and to name a directory holding one, so
// both spellings are tried. A bare package specifier — `@tsconfig/node20/tsconfig.json` —
// is not in the repository and correctly finds nothing: its aliases are unknowable from
// here, and guessing at them would invent edges.
func (r *resolver) tsConfigPath(ext string) string {
	if ext == "" {
		return ""
	}
	for _, c := range []string{ext, ext + ".json", path.Join(ext, "tsconfig.json")} {
		if _, ok := r.tsConfigs[c]; ok {
			return c
		}
	}
	return ""
}

// addCrate records a Cargo manifest's directory.
func (r *resolver) addCrate(file string) {
	r.crates = append(r.crates, dirOf(file))
	sort.Slice(r.crates, func(i, j int) bool {
		if len(r.crates[i]) != len(r.crates[j]) {
			return len(r.crates[i]) > len(r.crates[j])
		}
		return r.crates[i] < r.crates[j]
	})
}

// addPyRoot records a directory that a Python absolute import resolves against.
//
// One per `pyproject.toml`, and it is that file's presence that makes the directory a root
// rather than any naming convention: it declares a package, so the interpreter running the
// code beneath it has that directory on its path — installed, reached by an editable
// install, or simply the working directory.
//
// `pyproject.toml` alone, of the two Python manifests signpost reads. A `requirements.txt`
// pins what to install and declares no package, and `requirements/base.txt` is a real and
// common spelling — registering its directory would make `requirements/` a resolution root
// and invent edges into a directory holding no code.
//
// This is the Python analogue of addGoModule and addNpmPackage, and its absence was the
// same class of bug. `resolvePython` tried exactly two roots, the repository root and
// `src`, on the reasoning that they account for essentially every Python project. That is
// true of a project. It is false of a monorepo, which is where the imports are, and the
// shape is not exotic: 28 package manifests in one measured repository, each package
// importing its own code by top-level name. `from api.client import make_api_request`
// appears in 340 imports there, resolves against the package that declares it, and signpost
// reported every one as a gap while nine sibling packages each had their own `api/client.py`.
//
// Registering the repository root unconditionally is what keeps the src-layout and
// single-package cases working when no manifest was read at all — a repository whose
// manifest sits outside the walk, or which has none.
func (r *resolver) addPyRoot(dir string) {
	for _, d := range r.pyRoots {
		if d == dir {
			return
		}
	}
	r.pyRoots = append(r.pyRoots, dir)
	// Longest first, so a file inside a package resolves against that package before the
	// repository root. Both are on the path in principle; the nearer one is the one whose
	// code the file can see, and preferring the root would route a package's own import
	// into a same-named directory in a sibling package.
	sort.Slice(r.pyRoots, func(i, j int) bool {
		if len(r.pyRoots[i]) != len(r.pyRoots[j]) {
			return len(r.pyRoots[i]) > len(r.pyRoots[j])
		}
		return r.pyRoots[i] < r.pyRoots[j]
	})
}

// addJVMPackage records a `package` declaration against the directory declaring it.
//
// This is the JVM's equivalent of addGoModule, and it comes from a different place: a Go
// module path is declared once in go.mod, while a JVM package is declared in every source
// file, and no build file signpost reads states the mapping at all. Task #19 does not read
// pom.xml or build.gradle, so the source files are the only authority available — and they
// are a sufficient one, because a `package` declaration is exactly the name another file
// writes in its `import`.
//
// Deriving the name from the path instead would be wrong rather than approximate. The same
// file compiles from `src/main/java`, from `src/`, or from any directory a Gradle source
// set names, so `src/main/java/com/example/api/A.java` yields `src.main.java.com.example.api`
// under a path rule and `com.example.api` under this one. Only the second resolves an
// import anybody wrote.
//
// `test` says whether the declaring file is one, and it is what makes the standard layout
// resolvable at all. Maven and Gradle put `com.example.api` in two directories —
// `src/main/java/com/example/api` and `src/test/java/com/example/api` — so an import of
// that package names two candidates and only the first is what another module means by it.
// A test directory is registered anyway rather than discarded, because a package declared
// *only* under src/test is still this repository's own and an import of it is internal, not
// a dependency somebody forgot to declare.
func (r *resolver) addJVMPackage(file, name string, test bool) {
	if name == "" {
		return
	}
	dir := dirOf(file)
	for i, p := range r.jvmPkgs {
		if p.name != name || p.dir != dir {
			continue
		}
		// A directory holding one production file and one test file is production. The
		// flag means "every file declaring this package here is a test", so a single
		// non-test file clears it — the same rule hasProdSource applies to a module.
		if !test {
			r.jvmPkgs[i].test = false
		}
		return
	}
	r.jvmPkgs = append(r.jvmPkgs, jvmPackage{dir: dir, name: name, test: test})
	// Longest name first, so an import of `com.example.api.internal` matches that package
	// rather than `com.example.api` with a stray `.internal` read as a class.
	//
	// Production before test at equal length, because directory order cannot be trusted to
	// do it. `src/main` happens to sort before `src/test`, but the source set holding tests
	// is not always called that: Android's is `src/androidTest` and Gradle's convention for
	// the extra one is `src/integrationTest`, and both sort ahead of `main`. So a repository
	// with either resolves every import of a package to the copy under test — an edge into
	// the tests instead of into the code, drawn with no indication that a choice was made.
	sort.Slice(r.jvmPkgs, func(i, j int) bool {
		if len(r.jvmPkgs[i].name) != len(r.jvmPkgs[j].name) {
			return len(r.jvmPkgs[i].name) > len(r.jvmPkgs[j].name)
		}
		if r.jvmPkgs[i].name != r.jvmPkgs[j].name {
			return r.jvmPkgs[i].name < r.jvmPkgs[j].name
		}
		if r.jvmPkgs[i].test != r.jvmPkgs[j].test {
			return !r.jvmPkgs[i].test
		}
		return r.jvmPkgs[i].dir < r.jvmPkgs[j].dir
	})
}

// addDep registers a declared dependency under every key an import of it might use.
//
// The gap between a distribution name and the name you import is real and routine:
// `PyYAML` is imported as `yaml`, `serde_json` is declared `serde-json` in some
// manifests and used with an underscore in every one of them. Registering several
// keys is a deliberate over-match: the cost of a wrong match here is one edge to the
// wrong external node, and the cost of a miss is losing the connection between a
// repository's code and its own supply chain, which is §2's entire subject.
func (r *resolver) addDep(ecosystem, name, id string) {
	for _, k := range depKeys(ecosystem, name) {
		if _, exists := r.deps[k]; !exists {
			r.deps[k] = id
		}
	}
}

// depKeys returns the lookup keys a declared dependency answers to.
func depKeys(ecosystem, name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	keys := []string{ecosystem + "\x00" + name}
	// A dependency is looked up by ecosystem so two same-named packages in different
	// registries stay distinct, and also unqualified, because an import string does
	// not name its registry: a `.ts` file importing "shared" cannot say whether that
	// is the npm package or a workspace path.
	add := func(s string) {
		if s == "" || s == name {
			return
		}
		keys = append(keys, ecosystem+"\x00"+s)
	}
	lower := strings.ToLower(name)
	add(lower)
	// PyPI normalization: `A.B_c` and `a-b-c` are the same project (PEP 503).
	norm := strings.NewReplacer("_", "-", ".", "-").Replace(lower)
	add(norm)
	add(strings.ReplaceAll(norm, "-", "_"))
	for _, imp := range pyImportNames[norm] {
		add(imp)
	}
	return keys
}

// pyImportNames maps PyPI distribution names to the names code actually imports.
//
// The divergence is not a normalization and cannot be derived: `PyYAML` provides
// `yaml`, `Pillow` provides `PIL`, and nothing in either name says so. The mapping
// lives only in each project's own packaging metadata, which is not in the repository
// signpost is reading.
//
// A table, therefore, and the same trade this file already takes with pyStdlib: it
// covers what appears in real code, and a distribution missing from it degrades to
// "unresolved" — a visible, correctable inaccuracy rather than a wrong edge. Entries
// are keyed by the PEP 503 normalized name.
var pyImportNames = map[string][]string{
	"attrs":                {"attr", "attrs"},
	"beautifulsoup4":       {"bs4"},
	"opencv-python":        {"cv2"},
	"python-dateutil":      {"dateutil"},
	"python-dotenv":        {"dotenv"},
	"protobuf":             {"google"},
	"google-cloud-storage": {"google"},
	"grpcio":               {"grpc"},
	"grpcio-tools":         {"grpc_tools"},
	"pillow":               {"PIL"},
	"pyyaml":               {"yaml"},
	"pyjwt":                {"jwt"},
	"pycryptodome":         {"Crypto"},
	"pytest-cov":           {"pytest_cov"},
	"scikit-learn":         {"sklearn"},
	"scikit-image":         {"skimage"},
	"msgpack-python":       {"msgpack"},
	"typing-extensions":    {"typing_extensions"},
	"setuptools":           {"setuptools", "pkg_resources"},
	"mysqlclient":          {"MySQLdb"},
	"psycopg2-binary":      {"psycopg2"},
	"python-multipart":     {"multipart"},
	"pymupdf":              {"fitz"},
	"faiss-cpu":            {"faiss"},
	"faiss-gpu":            {"faiss"},
}

// dep returns the external node ID for a declared dependency, or "".
func (r *resolver) dep(ecosystem, name string) string {
	for _, k := range depKeys(ecosystem, name) {
		if id, ok := r.deps[k]; ok {
			return id
		}
	}
	return ""
}

// resolveImport returns the node an import points at, and whether it is internal.
//
// from is the importing file's repo-relative path, which several languages need: a
// relative specifier means nothing without it.
func (r *resolver) resolveImport(lang discover.Lang, from, raw string) (id string, internal bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	switch lang {
	case discover.LangGo:
		return r.resolveGo(raw)
	case discover.LangPython:
		return r.resolvePython(from, raw)
	case discover.LangTS, discover.LangJS:
		return r.resolveTS(from, raw)
	case discover.LangRust:
		return r.resolveRust(from, raw)
	case discover.LangJava, discover.LangKotlin:
		return r.resolveJVM(raw)
	case discover.LangC, discover.LangCpp, discover.LangObjC:
		return r.resolveC(from, raw)
	}
	return "", false
}

// resolveC resolves an #include against the files in the repository.
//
// An include names a file, not a module, and the delimiter names the search rule — which
// is why the extractor keeps the delimiters in Import.Raw. A quoted include searches the
// including file's own directory first, so it is normally a header in this repository. An
// angled include searches only the compiler's path, so it is normally the toolchain or an
// installed library, and resolving one against repository paths would invent an edge from
// a coincidence of directory names.
//
// Both forms fall back to the include roots a C project conventionally declares —
// `include/`, `src/`, and the project directory itself — because that is what a build
// system's `-I` flags say and signpost reads no CMakeLists or Makefile to learn the real
// list (the same limitation ADR 0017 states for the JVM). An angled include is allowed
// that fallback too, and only that one: a project's own public headers are routinely
// included with angle brackets precisely because they are on the include path.
//
// The roots are tried relative to every ancestor of the including file rather than to the
// repository root alone, walking outward. Anchoring at the root only is what a
// single-project repository looks like, and it is wrong for every other shape: a C library
// vendored at `third_party/zlib/`, or one project among several in a monorepo, declares
// `-I` against its *own* directory, so `#include <corpus/buffer.h>` from
// `c/src/app.c` means `c/include/corpus/buffer.h`. Anchored at the root, every such
// include lands in the gap report and the project's own internal structure is invisible —
// which reads as C being unresolvable rather than as the search path being mismodelled.
//
// An include naming no file in the repository resolves to nothing rather than to an
// external node. There is no manifest declaring C dependencies to match against — a
// system header arrives from the toolchain or a distro package, neither of which the
// repository names — so inventing a node for `<stdio.h>` would publish a dependency the
// repository never declared. isStdlib keeps the well-known headers out of the unresolved
// count; the rest stay visible in the gap report, which is where the limitation belongs.
func (r *resolver) resolveC(from, raw string) (string, bool) {
	inc, quoted := extract.IncludePath(raw)
	if inc == "" {
		return "", false
	}
	if quoted {
		// Relative to the including file's own directory, which is the compiler's first
		// search location and the one a repository-local header is written against.
		if id := r.cTarget(path.Join(dirOf(from), inc)); id != "" {
			return id, true
		}
	}
	// Nearest ancestor first: a monorepo holding two C projects has an `include/` in each,
	// and the one nearer the including file is the one its build declares.
	for dir := dirOf(from); ; dir = dirOf(dir) {
		for _, root := range []string{"include", "src", "", "lib", "source"} {
			if id := r.cTarget(path.Join(dir, root, inc)); id != "" {
				return id, true
			}
		}
		if dir == "" || dir == "." {
			return "", false
		}
	}
}

// cTarget resolves a slash path to the module holding that file.
//
// The file must exist: an include names one, so unlike the Python and TypeScript
// resolvers there is no directory form to fall back to, and accepting a directory would
// match `#include <memory>` against any directory named `memory`.
func (r *resolver) cTarget(p string) string {
	p = strings.TrimPrefix(path.Clean(p), "./")
	if p == "" || p == "." || strings.HasPrefix(p, "..") {
		return ""
	}
	if !r.srcFiles[p] {
		return ""
	}
	return r.moduleAt(dirOf(p))
}

// resolveJVM resolves an import against the package declarations the repository's own
// source files make.
//
// No `from` is needed, which makes this the simplest resolver here: a JVM import is
// always the fully-qualified name and never relative to the importing file. What makes
// it the least complete is the other side — signpost reads no pom.xml or build.gradle
// (deferred with the rest of the JVM manifest work), so there is no declared-dependency
// list for a JVM import to match against. An import that names no package in this
// repository therefore resolves to nothing at all rather than to an external node.
//
// That is the honest outcome and not a placeholder to be filled with a guess. Inventing
// a Maven node from an import string would mean publishing a coordinate the repository
// never declared, in a bundle whose whole claim about dependencies is that a manifest
// said so. Until a build file is read, `org.springframework.*` is counted as unresolved
// — visible in the gap report, which is where a reader can see the limitation.
// The first match wins outright, and both halves of that are decided by the sort in
// addJVMPackage rather than here. Longest name first is what makes
// `com.example.store.internal` beat `com.example.store`, and production before test is what
// picks between the two directories a standard layout gives one package name. There is no
// second candidate to fall through to and no need for one: every jvmPkgs entry was recorded
// from a file that also created a module node for its directory, both keyed on dirOf, so the
// lookup below cannot come back empty.
func (r *resolver) resolveJVM(raw string) (string, bool) {
	for _, p := range r.jvmPkgs {
		if jvmUnderPackage(raw, p.name) {
			return r.moduleAt(p.dir), true
		}
	}
	return "", false
}

// jvmUnderPackage reports whether an import names a package or something inside it.
//
// Dot-delimited, so `com.example.apiv2` is not under `com.example.api` — the same care
// underPath takes with slashes, and for the same reason.
func jvmUnderPackage(raw, pkg string) bool {
	return raw == pkg || strings.HasPrefix(raw, pkg+".")
}

// resolveGo resolves by module path prefix, which is exactly how the Go toolchain
// resolves: an import inside the module is the module path plus the directory.
func (r *resolver) resolveGo(raw string) (string, bool) {
	for _, m := range r.goMods {
		rest, ok := underPath(raw, m.path)
		if !ok {
			continue
		}
		if id := r.moduleAt(path.Join(m.dir, rest)); id != "" {
			return id, true
		}
		// Inside the module but no node: a package with no extracted files, most
		// often generated code or a build-tagged directory. Internal, and honestly
		// unresolved — inventing an external node for it would misreport the supply
		// chain, which is the one thing this resolver must not do.
		return "", true
	}
	// A first segment with no dot is a standard-library path; the toolchain uses the
	// same test. It is not a dependency anyone patches, so it is not a node.
	first, _, _ := strings.Cut(raw, "/")
	if !strings.Contains(first, ".") {
		return "", false
	}
	return r.depOrEmpty("go", goDepCandidates(raw)), false
}

// goDepCandidates returns the prefixes of an import path that could be a module,
// longest first: `example.com/a/b/c` may be provided by a module at any depth.
func goDepCandidates(raw string) []string {
	segs := strings.Split(raw, "/")
	out := make([]string, 0, len(segs))
	for i := len(segs); i >= 1; i-- {
		out = append(out, strings.Join(segs[:i], "/"))
	}
	return out
}

// resolvePython resolves dotted and relative imports.
func (r *resolver) resolvePython(from, raw string) (string, bool) {
	if strings.HasPrefix(raw, ".") {
		// Leading dots count package levels up from the importing file's own package,
		// so `.` is the current package and `..` is its parent.
		dots := len(raw) - len(strings.TrimLeft(raw, "."))
		base := dirOf(from)
		for i := 1; i < dots; i++ {
			base = dirOf(base)
		}
		rest := strings.ReplaceAll(strings.Trim(raw[dots:], "."), ".", "/")
		if id := r.pyTarget(path.Join(base, rest)); id != "" {
			return id, true
		}
		// A relative import is internal by construction even when the target has no
		// node, so it must never fall through to the external lookup below.
		return "", true
	}
	rel := strings.ReplaceAll(raw, ".", "/")
	// Absolute imports resolve against the interpreter's path, and for a repository that is
	// the package root governing this file — nearest first — plus each root's `src`, which
	// is the other layout the packaging tools support. pyRootsFor is what keeps a package's
	// import inside its own package rather than in a same-named directory next door.
	for _, root := range r.pyRootsFor(from) {
		for _, layout := range []string{"", "src"} {
			if id := r.pyTarget(path.Join(root, layout, rel)); id != "" {
				return id, true
			}
		}
	}
	first, _, _ := strings.Cut(raw, ".")
	return r.depOrEmpty("pypi", []string{raw, first}), false
}

// pyRootsFor returns the package roots an import in `from` resolves against, nearest first.
//
// A root governs only the files beneath it, and that scoping is the whole of the care needed
// here. Nine packages in one measured repository each hold their own `api/client.py`, and a
// repo-wide root list would resolve `from api.client import ...` in one package to whichever
// of the nine sorted first — an edge between two packages that cannot see each other, which
// is worse than the gap it replaces because nothing reports it. Same reason tsPathAlias
// checks an alias's scope.
//
// The repository root is always last, so a layout with no manifest at all still resolves.
func (r *resolver) pyRootsFor(from string) []string {
	out := make([]string, 0, len(r.pyRoots)+1)
	for _, root := range r.pyRoots {
		if root == "" || from == root || strings.HasPrefix(from, root+"/") {
			out = append(out, root)
		}
	}
	if len(out) == 0 || out[len(out)-1] != "" {
		out = append(out, "")
	}
	return out
}

// pyTarget resolves a slash path to a package directory or a module file's directory.
func (r *resolver) pyTarget(p string) string {
	if id := r.moduleAt(p); id != "" {
		return id
	}
	for _, e := range pyExts {
		if r.srcFiles[p+e] {
			return r.moduleAt(dirOf(p + e))
		}
	}
	return ""
}

// resolveTS resolves relative specifiers by path and bare specifiers by package name.
func (r *resolver) resolveTS(from, raw string) (string, bool) {
	if strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") || raw == "." || raw == ".." {
		target := path.Join(dirOf(from), raw)
		if id := r.tsTarget(target); id != "" {
			return id, true
		}
		return "", true
	}
	// A declared alias comes first, because it is the codebase's own statement about what
	// this specifier means and the only authoritative one. `@fider/services` is
	// `public/services` and nothing but tsconfig says so.
	if id, ok := r.tsPathAlias(from, raw); ok {
		return id, true
	}
	// A leading slash or an undeclared `@/`-style prefix is repo-relative in intent, and
	// covers the case where the mapping is real but signpost did not read the config that
	// holds it — a tsconfig outside the walk, or one that did not parse. Try the literal
	// path so the common `@/` -> root and `~/` -> root conventions still land, and report
	// the rest as unresolved rather than as an npm package that does not exist.
	if alias, ok := tsAliasPath(raw); ok {
		if id := r.tsTarget(alias); id != "" {
			return id, true
		}
		return "", true
	}
	// A workspace package in this repository is checked before the dependency lookup,
	// because it is the more specific claim: a monorepo declares its own packages as
	// `workspace:*` dependencies, so both would match and the declared one is the
	// weaker fact. Same precedence resolveGo uses, for the same reason.
	if id, ok := r.npmWorkspace(raw); ok {
		return id, true
	}
	return r.depOrEmpty("npm", []string{npmPackage(raw)}), false
}

// npmWorkspace resolves a bare specifier against the packages declared in this
// repository, returning whether the specifier named one at all.
//
// The two return values are distinct and both matter. `ok` says the import is internal;
// `id` says which node it reached, and is empty when the package exists but the
// subdirectory it points at holds no extracted source — a `dist/` entry point, or a
// package whose files were all filtered. That case stays internal with no edge, which is
// resolveGo's answer too: inventing an external node for code that is in the tree
// misreports the supply chain, and that is the one thing this resolver must not do.
func (r *resolver) npmWorkspace(raw string) (string, bool) {
	for _, p := range r.npmPkgs {
		rest, under := underPath(raw, p.name)
		if !under {
			continue
		}
		// A deep import addresses a path inside the package, and the package root is
		// where that path is rooted: `@scope/core/lib/x` is `<dir>/lib/x`. Bare imports
		// pass rest == "" and land on the package directory itself.
		if id := r.tsTarget(path.Join(p.dir, rest)); id != "" {
			return id, true
		}
		// A published package points its specifier at built output — `main: dist/index.js`
		// — and `dist/` is not in the repository. So a bare import of a package whose root
		// holds no source is retried against the conventional source roots before giving
		// up, which is what makes `@scope/core` reach the code rather than nothing.
		for _, src := range []string{"src", "lib"} {
			if id := r.tsTarget(path.Join(p.dir, src, rest)); id != "" {
				return id, true
			}
		}
		return "", true
	}
	return "", false
}

// tsPathAlias resolves a specifier through the `paths` mapping declared for the importing
// file, returning whether any pattern claimed it.
//
// The two return values carry the same distinction npmWorkspace does: `ok` says a declared
// alias matched, so the specifier is internal and must not fall through to the npm lookup;
// `id` says which node it reached, empty when the mapping is real but points at a directory
// holding no extracted source. Falling through on a matched pattern is the failure mode that
// matters — it would turn `@fider/services` into a third-party dependency named `@fider`,
// which is #12's defect reached by a different road.
func (r *resolver) tsPathAlias(from, raw string) (string, bool) {
	for _, a := range r.tsAliases {
		// An alias governs only the files under its config's directory. Two packages that
		// each declare `@src/*` for their own source is a real and common shape, and a
		// repo-wide match would route one package's import into the other's code.
		if a.scope != "" && from != a.scope && !strings.HasPrefix(from, a.scope+"/") {
			continue
		}
		var rest string
		if a.wild {
			if !strings.HasPrefix(raw, a.prefix) || !strings.HasSuffix(raw, a.suffix) ||
				len(raw) < len(a.prefix)+len(a.suffix) {
				continue
			}
			rest = raw[len(a.prefix) : len(raw)-len(a.suffix)]
		} else if raw != a.prefix {
			continue
		}
		for _, t := range a.targets {
			// The wildcard's capture substitutes into the target's own `*`, which is how
			// `@fider/*` -> `public/*` maps `@fider/services` to `public/services`. A
			// target without a wildcard is a fixed location and the capture is dropped,
			// which is TypeScript's behaviour for a single-file mapping.
			p := t
			if a.wild {
				if i := strings.IndexByte(t, '*'); i >= 0 {
					p = t[:i] + rest + t[i+1:]
				}
			}
			if id := r.tsTarget(path.Clean(p)); id != "" {
				return id, true
			}
		}
		return "", true
	}
	return "", false
}

func tsAliasPath(raw string) (string, bool) {
	for _, p := range []string{"/", "@/", "~/", "#/"} {
		if strings.HasPrefix(raw, p) {
			return raw[len(p):], true
		}
	}
	return "", false
}

// tsTarget resolves a specifier path to a module directory, trying the file
// spellings and then the directory-with-index form, as a bundler would.
func (r *resolver) tsTarget(p string) string {
	// An extensionless specifier is the norm in TS; an explicit `.js` in an ESM
	// import very often names a `.ts` file that has not been built yet.
	cands := []string{p}
	if ext := path.Ext(p); ext == ".js" || ext == ".mjs" || ext == ".cjs" {
		cands = append(cands, strings.TrimSuffix(p, ext))
	}
	for _, c := range cands {
		for _, e := range tsExts {
			if r.srcFiles[c+e] {
				return r.moduleAt(dirOf(c + e))
			}
		}
		for _, idx := range tsIndex {
			if r.srcFiles[path.Join(c, idx)] {
				return r.moduleAt(c)
			}
		}
		if id := r.moduleAt(c); id != "" {
			return id
		}
	}
	return ""
}

// npmPackage reduces a bare specifier to the package it names, dropping any deep
// import path: `@scope/pkg/sub/thing` is one dependency, `@scope/pkg`.
func npmPackage(raw string) string {
	segs := strings.Split(raw, "/")
	if strings.HasPrefix(raw, "@") && len(segs) >= 2 {
		return segs[0] + "/" + segs[1]
	}
	return segs[0]
}

// resolveRust resolves crate-relative paths against the crate that owns the file.
//
// `self::` and `super::` land in the file's own module or its parent, which is the
// same directory or the one above; both resolve to a node the file usually already
// belongs to, and a self-edge is dropped by the graph.
//
// Rust module paths and directories only line up loosely, which is what makes `super`
// the awkward one — see the case below.
func (r *resolver) resolveRust(from, raw string) (string, bool) {
	segs := strings.Split(raw, "::")
	switch segs[0] {
	case "crate":
		crate := r.crateOf(from)
		// A use path routinely ends in a type or a function — `use crate::store::get`
		// binds a function, not a module — so the trailing segments are dropped one at
		// a time until a module is found. Longest first, because `crate::a::b` where
		// both `a/b.rs` and a `b` item in `a` exist must resolve to the module.
		for i := len(segs); i > 1; i-- {
			rest := path.Join(segs[1:i]...)
			for _, root := range []string{"src", ""} {
				if id := r.rustTarget(path.Join(crate, root, rest)); id != "" {
					return id, true
				}
			}
		}
		return "", true
	case "self":
		if id := r.moduleAt(dirOf(from)); id != "" {
			return id, true
		}
		return "", true
	case "super":
		if id := r.moduleAt(rustSuperDir(from)); id != "" {
			return id, true
		}
		return "", true
	}
	// A crate name in a use path is spelled with underscores even when the manifest
	// declares it with dashes, which depKeys already accounts for.
	return r.depOrEmpty("crates.io", []string{segs[0]}), false
}

// rustSuperDir returns the directory holding the parent module of the file at from.
//
// `super` is the parent *module*, and Rust's module tree is tied to the directory tree
// only loosely, so which file the path is written in decides the answer. `mod.rs` is the
// one spelling whose module *is* its directory, which puts its parent in the directory
// above. Every other file is a module inside the module its directory stands for, so its
// parent is that directory — `src/a.rs` is module `a` in the crate root, and the crate
// root is `src`.
//
// A crate root, `lib.rs` or `main.rs`, has no parent module at all, so `super` in one can
// only have been written inside an inline `mod` — and the parent of an inline module is
// the crate root itself, which is the file's own directory. Same answer as the general
// case, which is why they are not spelled out separately.
//
// That general case was resolving a directory too high, and it is the common one: `use
// super::*` inside an inline `#[cfg(test)] mod tests` is by a wide margin the most
// frequent `super` in the language. For `src/lib.rs` a directory up is where Cargo.toml
// lives, which holds no source, so the import reached nothing — silently, because the
// resolver was right that it is first-party and right to invent no external crate for it,
// and those two correct decisions were between them the reason nothing recorded the gap.
//
// Resolving to the file's own module yields a self-edge, which the graph drops. That is
// the right outcome rather than a workaround: a test module importing the file it is
// written in tells a reader nothing the file did not already say.
//
// The ambiguity that remains is a `super` inside an inline module in a `mod.rs`, which
// means that file's own directory where a top-level `super` in the same file means the one
// above. Distinguishing them needs the nesting depth of the `use`, which the extractor
// does not record; the top-level reading is taken because that is what `mod.rs` exists
// for, and the cost of being wrong is one self-edge the graph would have dropped anyway.
func rustSuperDir(from string) string {
	if path.Base(from) == "mod.rs" {
		return dirOf(dirOf(from))
	}
	return dirOf(from)
}

// crateOf returns the crate directory owning a file, "" when no Cargo manifest was
// found — a single-crate repo whose manifest was outside the walk still resolves,
// because the empty crate root makes `src/...` repo-relative.
func (r *resolver) crateOf(from string) string {
	for _, c := range r.crates {
		if c == "" || from == c || strings.HasPrefix(from, c+"/") {
			return c
		}
	}
	return ""
}

// rustTarget resolves a module path to `x.rs`, `x/mod.rs`, or a directory.
func (r *resolver) rustTarget(p string) string {
	for _, e := range rustExts {
		if r.srcFiles[p+e] {
			return r.moduleAt(dirOf(p + e))
		}
	}
	if r.srcFiles[path.Join(p, "mod.rs")] {
		return r.moduleAt(p)
	}
	return r.moduleAt(p)
}

// moduleAt returns the module node for a directory, or "".
func (r *resolver) moduleAt(dir string) string { return r.moduleIDs[cleanDir(dir)] }

// depOrEmpty returns the first candidate that matches a declared dependency.
func (r *resolver) depOrEmpty(ecosystem string, candidates []string) string {
	for _, c := range candidates {
		if id := r.dep(ecosystem, c); id != "" {
			return id
		}
	}
	// A specifier that names no declared dependency is also tried unqualified: a
	// TypeScript file in a Go repository imports npm packages whose manifest may sit
	// in a directory the walk covered under a different ecosystem label, and a match
	// on the name alone is better than reporting the repo's own dependency as absent.
	for _, c := range candidates {
		for _, eco := range r.ecosystems {
			if eco == ecosystem {
				continue
			}
			if id := r.dep(eco, c); id != "" {
				return id
			}
		}
	}
	return ""
}

// underPath reports whether raw is prefix itself or lies beneath it, returning the
// remainder. String prefix matching alone is wrong here: `example.com/foobar` is not
// under `example.com/foo`.
func underPath(raw, prefix string) (rest string, ok bool) {
	if raw == prefix {
		return "", true
	}
	if strings.HasPrefix(raw, prefix+"/") {
		return raw[len(prefix)+1:], true
	}
	return "", false
}

// dirOf returns a repo-relative path's directory, with the root spelled "" rather
// than path.Dir's ".". Every path in a bundle is compared as a string, so one
// spelling for the root is not cosmetic.
func dirOf(p string) string {
	d := path.Dir(p)
	if d == "." || d == "/" {
		return ""
	}
	return d
}

func cleanDir(p string) string {
	if p == "" || p == "." {
		return ""
	}
	return path.Clean(p)
}

// manifestEcosystems returns the ecosystems a reading declares, for the
// cross-ecosystem fallback in depOrEmpty.
func manifestEcosystems(facts []manifest.Facts) []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range facts {
		for _, d := range f.Deps {
			if d.Ecosystem != "" && !seen[d.Ecosystem] {
				seen[d.Ecosystem] = true
				out = append(out, d.Ecosystem)
			}
		}
	}
	sort.Strings(out)
	return out
}
