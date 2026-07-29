package assemble

import (
	"path"
	"sort"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
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
	// crates are Cargo manifest directories, longest first, same reason.
	crates []string
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
	}
	return "", false
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
	// Absolute imports resolve against the interpreter's path, which for a repository
	// is its root or a src/ directory — the two layouts that account for essentially
	// every Python project.
	for _, root := range []string{"", "src"} {
		if id := r.pyTarget(path.Join(root, rel)); id != "" {
			return id, true
		}
	}
	first, _, _ := strings.Cut(raw, ".")
	return r.depOrEmpty("pypi", []string{raw, first}), false
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
	// A leading slash or a tsconfig path alias (`@/components/x`) is repo-relative in
	// intent but the mapping lives in tsconfig, which this package does not read. Try
	// the literal path so the common `@/` -> root and `~/` -> root conventions land,
	// and report the rest as unresolved rather than as an npm package that does not
	// exist.
	if alias, ok := tsAliasPath(raw); ok {
		if id := r.tsTarget(alias); id != "" {
			return id, true
		}
		return "", true
	}
	return r.depOrEmpty("npm", []string{npmPackage(raw)}), false
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
		if id := r.moduleAt(dirOf(dirOf(from))); id != "" {
			return id, true
		}
		return "", true
	}
	// A crate name in a use path is spelled with underscores even when the manifest
	// declares it with dashes, which depKeys already accounts for.
	return r.depOrEmpty("crates.io", []string{segs[0]}), false
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
