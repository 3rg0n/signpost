package manifest

import (
	"path"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// The dependency-manifest extractors: go.mod, package.json, pyproject.toml,
// requirements.txt, and Cargo.toml.
//
// These are the first row of design §4.1's table, and the reason they lead is that a
// manifest is the only place a repository states its supply chain exactly. Source
// imports tell you what a file uses; the manifest tells you what version, from which
// registry, in which scope, and whether a human asked for it or a resolver did.
//
// The four are separate functions rather than one table-driven reader because the
// interesting parts are exactly where they differ: go.mod marks indirect deps in a
// comment, npm splits scope across five sibling objects, pyproject has two
// incompatible conventions in the same file, and Cargo puts a dependency's version
// either in a string or inside an inline table. A generic reader would have to
// special-case all of that anyway, and would hide it while doing so.

// ExtractGoMod reads a go.mod or go.work file.
func ExtractGoMod(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindGoMod}
	m, diag := ParseGoMod(f.Content)
	facts.applyDiag(diag)

	facts.Module = Module{
		Name:        m.Module,
		Ecosystem:   "go",
		LangVersion: m.Go,
		Line:        m.ModuleLine,
	}
	// A toolchain pin is the version that actually builds, so it is the more useful
	// answer to "what Go does this need" when the two disagree.
	if m.Toolchain != "" {
		facts.Module.LangVersion = strings.TrimPrefix(m.Toolchain, "go")
	}
	// go.work's `use` lines are a workspace declaration in the same sense npm's
	// `workspaces` and Cargo's `members` are.
	facts.Module.Workspaces = append(facts.Module.Workspaces, m.Uses...)

	for _, r := range m.Requires {
		scope := ScopeRuntime
		if r.Indirect {
			scope = ScopeIndirect
		}
		facts.Deps = append(facts.Deps, Dep{
			Name: r.Path, Version: r.Version, Scope: scope, Ecosystem: "go", Line: r.Line,
		})
	}
	for _, r := range m.Replaces {
		// A replace onto a local directory is a module in this repository, not an
		// external dependency: recording it as a Dep would put a node in the graph
		// for code that is already there under its own path. It becomes a workspace
		// member instead, which is what it is.
		if r.Local() {
			facts.Module.Workspaces = append(facts.Module.Workspaces, path.Clean(r.NewPath))
			continue
		}
		// A replace onto a fork is a dependency whose supply chain the team has taken
		// over. Recording the fork as the Source keeps that visible.
		facts.Deps = append(facts.Deps, Dep{
			Name: r.OldPath, Version: r.NewVersion, Scope: ScopeRuntime,
			Ecosystem: "go", Source: r.NewPath, Line: r.Line,
		})
	}
	return facts
}

// npmScopes maps package.json's dependency objects to a scope.
//
// peerDependencies is ScopeBuild rather than ScopeRuntime deliberately: a peer dep is
// something the *consumer* must provide, so it is not this package's own runtime
// requirement, and calling it one would overstate what this repository ships.
var npmScopes = []struct {
	key   string
	scope DepScope
}{
	{"dependencies", ScopeRuntime},
	{"devDependencies", ScopeDev},
	{"peerDependencies", ScopeBuild},
	{"optionalDependencies", ScopeBuild},
}

// ExtractPackageJSON reads a package.json.
func ExtractPackageJSON(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindPackageJSON}
	root, err := ParseJSON(f.Content)
	if err != nil {
		facts.markIncomplete("package.json did not parse: " + err.Error())
		return facts
	}

	facts.Module = Module{
		Name:      root.Get("name").String(),
		Version:   root.Get("version").String(),
		Ecosystem: "npm",
		Line:      1,
	}
	if v := root.Path("engines", "node").String(); v != "" {
		facts.Module.LangVersion = v
	}
	if p, ok := root.Get("private").Bool(); ok {
		facts.Module.Private = p
	}
	facts.Module.Workspaces = append(facts.Module.Workspaces, root.Get("workspaces").Strings()...)
	// The object form: {"packages": [...], "nohoist": [...]}.
	facts.Module.Workspaces = append(facts.Module.Workspaces, root.Path("workspaces", "packages").Strings()...)

	for _, s := range npmScopes {
		obj := root.Get(s.key)
		optional := s.key == "optionalDependencies"
		obj.Each(func(name string, ver *Node) bool {
			d := Dep{
				Name: name, Version: ver.String(), Scope: s.scope,
				Ecosystem: "npm", Optional: optional, Line: lineOf(ver, obj.Line),
			}
			// npm overloads the version field with the origin: a value that is not a
			// semver range is a git URL, a tarball, a `file:` path, or a `workspace:`
			// reference. Which it is changes the dependency's whole posture — a git
			// dependency has no registry to publish an advisory against.
			if src := npmDepSource(d.Version); src != "" {
				d.Source = src
			}
			facts.Deps = append(facts.Deps, d)
			return true
		})
	}

	scripts := root.Get("scripts")
	scripts.Each(func(name string, cmd *Node) bool {
		facts.Scripts = append(facts.Scripts, Script{
			Name: name, Command: cmd.String(), Line: lineOf(cmd, scripts.Line),
		})
		return true
	})

	// bin is either a string (one executable named after the package) or an object.
	bin := root.Get("bin")
	switch {
	case bin == nil:
	case bin.Kind == KindScalar:
		facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
			Name: facts.Module.Name, Path: bin.String(), Line: bin.Line,
		})
	default:
		bin.Each(func(name string, p *Node) bool {
			facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
				Name: name, Path: p.String(), Line: lineOf(p, bin.Line),
			})
			return true
		})
	}
	return facts
}

// npmDepSource returns a non-registry origin for an npm version string, or "".
func npmDepSource(v string) string {
	for _, p := range []string{
		"git+", "git:", "github:", "gitlab:", "bitbucket:",
		"file:", "link:", "workspace:", "npm:", "http://", "https://",
	} {
		if strings.HasPrefix(v, p) {
			return v
		}
	}
	// The bare `owner/repo` GitHub shorthand. Distinguished from a semver range by
	// the slash, since no range contains one.
	if strings.Contains(v, "/") && !strings.ContainsAny(v, "^~<>= ") {
		return v
	}
	return ""
}

// ExtractPyProject reads a pyproject.toml.
//
// Two conventions coexist in this file and often in the same repository: PEP 621's
// `[project]` and Poetry's `[tool.poetry]`. Both are read, because which one a
// project uses is not something to be right about in advance — and a project
// migrating between them has both.
func ExtractPyProject(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindPyProject}
	root, diag := ParseTOML(f.Content)
	facts.applyDiag(diag)

	project := root.Get("project")
	poetry := root.Path("tool", "poetry")

	facts.Module = Module{
		Name:        firstNonEmpty(project.Get("name").String(), poetry.Get("name").String()),
		Version:     firstNonEmpty(project.Get("version").String(), poetry.Get("version").String()),
		Ecosystem:   "pypi",
		LangVersion: project.Get("requires-python").String(),
		Line:        lineOf(project, 1),
	}

	// PEP 621: a flat list of PEP 508 requirement strings.
	for _, item := range project.Get("dependencies").Seq() {
		if d, ok := parsePEP508(item.String(), ScopeRuntime, item.Line); ok {
			facts.Deps = append(facts.Deps, d)
		}
	}
	// PEP 621 extras. An extra is optional by definition, and its name is not
	// recorded as a separate scope: what matters downstream is that the dependency
	// is not unconditionally required.
	extras := project.Get("optional-dependencies")
	extras.Each(func(_ string, list *Node) bool {
		for _, item := range list.Seq() {
			if d, ok := parsePEP508(item.String(), ScopeRuntime, item.Line); ok {
				d.Optional = true
				facts.Deps = append(facts.Deps, d)
			}
		}
		return true
	})
	// PEP 735 dependency groups, which is where modern non-Poetry projects put dev
	// dependencies.
	groups := root.Get("dependency-groups")
	groups.Each(func(_ string, list *Node) bool {
		for _, item := range list.Seq() {
			if d, ok := parsePEP508(item.String(), ScopeDev, item.Line); ok {
				facts.Deps = append(facts.Deps, d)
			}
		}
		return true
	})
	// uv and PDM keep dev dependencies under their own tool tables.
	for _, p := range [][]string{{"tool", "uv", "dev-dependencies"}, {"tool", "pdm", "dev-dependencies"}} {
		for _, item := range root.Path(p...).Seq() {
			if d, ok := parsePEP508(item.String(), ScopeDev, item.Line); ok {
				facts.Deps = append(facts.Deps, d)
			}
		}
	}

	// Poetry: a table of name -> constraint or name -> {version, ...}.
	for _, sec := range []struct {
		path  []string
		scope DepScope
	}{
		{[]string{"dependencies"}, ScopeRuntime},
		{[]string{"dev-dependencies"}, ScopeDev},
		{[]string{"group", "dev", "dependencies"}, ScopeDev},
		{[]string{"group", "test", "dependencies"}, ScopeDev},
	} {
		tbl := poetry.Path(sec.path...)
		tbl.Each(func(name string, spec *Node) bool {
			// Poetry declares the interpreter as a dependency named "python". That is
			// a language version, not a package, and reporting it as a dependency
			// would put a node in the graph for the runtime itself.
			if name == "python" {
				if facts.Module.LangVersion == "" {
					facts.Module.LangVersion = specVersion(spec)
				}
				return true
			}
			d := Dep{
				Name: name, Version: specVersion(spec), Scope: sec.scope,
				Ecosystem: "pypi", Line: lineOf(spec, tbl.Line),
			}
			if spec.Kind == KindMap {
				if o, ok := spec.Get("optional").Bool(); ok {
					d.Optional = o
				}
				d.Source = firstNonEmpty(spec.Get("git").String(), spec.Get("path").String(), spec.Get("url").String())
			}
			facts.Deps = append(facts.Deps, d)
			return true
		})
	}

	// Console scripts are the entrypoints an installed package exposes.
	for _, p := range [][]string{{"project", "scripts"}, {"project", "gui-scripts"}, {"tool", "poetry", "scripts"}} {
		tbl := root.Path(p...)
		tbl.Each(func(name string, target *Node) bool {
			facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
				Name: name, Path: target.String(), Line: lineOf(target, tbl.Line),
			})
			return true
		})
	}

	// A workspace: uv's members, or Poetry's path dependencies.
	facts.Module.Workspaces = append(facts.Module.Workspaces,
		root.Path("tool", "uv", "workspace", "members").Strings()...)
	return facts
}

// specVersion reads a version out of either a bare constraint or a table.
func specVersion(spec *Node) string {
	if spec == nil {
		return ""
	}
	if spec.Kind == KindMap {
		return spec.Get("version").String()
	}
	// A list is Poetry's multiple-constraints form: several specs for different
	// markers. The first is representative enough for a fact about what is required;
	// the alternative is inventing a joined pseudo-constraint nobody wrote.
	if spec.Kind == KindSeq {
		return specVersion(spec.At(0))
	}
	return spec.String()
}

// ExtractRequirements reads a requirements.txt.
func ExtractRequirements(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindRequirement}
	// A requirements file's name says what it is for: requirements-dev.txt is a dev
	// dependency set. This is a filename convention rather than a specification, but
	// it is universal, and the alternative is reporting every dev tool as a runtime
	// requirement.
	scope := ScopeRuntime
	base := strings.ToLower(path.Base(f.Path))
	for _, hint := range []string{"dev", "test", "lint", "doc", "ci"} {
		if strings.Contains(base, hint) {
			scope = ScopeDev
			break
		}
	}

	for i, raw := range strings.Split(strings.ReplaceAll(f.Content, "\r\n", "\n"), "\n") {
		num := i + 1
		line := strings.TrimSpace(raw)
		// A `#` begins a comment only at the start or after whitespace, so a URL
		// fragment such as `...#egg=name` survives — and that fragment is the only
		// place a VCS requirement states its package name.
		if i := indexComment(line); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		// Options rather than requirements. `-r other.txt` is a real edge between two
		// requirements files, but it is not a dependency, and the referenced file is
		// discovered and read on its own.
		if strings.HasPrefix(line, "-") {
			continue
		}
		if strings.HasSuffix(line, "\\") {
			// A wrapped line. Rare in requirements files and not worth a
			// continuation buffer, but silently mis-reading it would be wrong.
			facts.markIncomplete("line continuation not read")
			continue
		}
		if d, ok := parsePEP508(line, scope, num); ok {
			facts.Deps = append(facts.Deps, d)
		}
	}
	return facts
}

// indexComment returns the index of a `#` that begins a comment, or -1.
//
// The preceding-whitespace rule is the same one the YAML reader uses, and for the
// same reason: `pkg @ git+https://h/r#egg=pkg` must not be truncated at the fragment.
func indexComment(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != '#' {
			continue
		}
		if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
			return i
		}
	}
	return -1
}

// parsePEP508 reads a Python requirement specifier.
//
// The grammar is `name[extras] constraint ; marker` or `name @ url ; marker`. Only
// the name, the constraint, and whether an origin was given are extracted: the
// environment marker decides whether a dependency applies on *this* platform, which
// is a resolution question, and this reader records what the repository declares.
func parsePEP508(s string, scope DepScope, line int) (Dep, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Dep{}, false
	}
	// Drop the environment marker.
	if i := strings.Index(s, ";"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	d := Dep{Scope: scope, Ecosystem: "pypi", Line: line}

	// Direct reference: `name @ url`.
	if i := strings.Index(s, "@"); i >= 0 && !strings.HasPrefix(s, "@") {
		d.Name = normalizePyName(s[:i])
		d.Source = strings.TrimSpace(s[i+1:])
		if d.Name == "" {
			return Dep{}, false
		}
		return d, true
	}
	// A bare VCS URL, whose package name lives in the `#egg=` fragment. Without the
	// fragment there is no name to record, so there is no fact to report either.
	if strings.Contains(s, "://") {
		if i := strings.Index(s, "#egg="); i >= 0 {
			d.Name = normalizePyName(s[i+5:])
			d.Source = s[:i]
			return d, d.Name != ""
		}
		return Dep{}, false
	}

	// Split name[extras] from the constraint at the first operator character.
	end := len(s)
	for i := 0; i < len(s); i++ {
		if strings.ContainsRune("<>=!~ \t", rune(s[i])) {
			end = i
			break
		}
	}
	name := s[:end]
	if i := strings.IndexByte(name, '['); i >= 0 {
		// An extra changes what gets installed, not which package: `uvicorn[standard]`
		// is still uvicorn, and a graph with both as separate nodes would be wrong.
		d.Optional = false
		name = name[:i]
	}
	d.Name = normalizePyName(name)
	d.Version = strings.TrimSpace(s[end:])
	if d.Name == "" {
		return Dep{}, false
	}
	return d, true
}

// normalizePyName applies PEP 503 name normalisation.
//
// `Flask`, `flask`, and `FLASK` are one package to pip, as are `zope.interface` and
// `zope-interface`. Normalising means a dependency declared one way in
// pyproject.toml and another in requirements.txt dedupes to one node rather than two.
func normalizePyName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	prevSep := false
	for _, r := range strings.ToLower(s) {
		if r == '-' || r == '_' || r == '.' {
			if !prevSep && b.Len() > 0 {
				b.WriteByte('-')
				prevSep = true
			}
			continue
		}
		prevSep = false
		b.WriteRune(r)
	}
	return strings.TrimSuffix(b.String(), "-")
}

// cargoScopes maps Cargo's dependency tables to a scope.
var cargoScopes = []struct {
	key   string
	scope DepScope
}{
	{"dependencies", ScopeRuntime},
	{"dev-dependencies", ScopeDev},
	{"build-dependencies", ScopeBuild},
}

// ExtractCargo reads a Cargo.toml.
func ExtractCargo(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindCargo}
	root, diag := ParseTOML(f.Content)
	facts.applyDiag(diag)

	pkg := root.Get("package")
	facts.Module = Module{
		Name:      pkg.Get("name").String(),
		Version:   specVersion(pkg.Get("version")),
		Ecosystem: "crates.io",
		// Rust states compatibility as an edition, with rust-version as the finer
		// floor when a crate declares one.
		LangVersion: firstNonEmpty(pkg.Get("rust-version").String(), pkg.Get("edition").String()),
		Line:        lineOf(pkg, 1),
	}
	if p, ok := pkg.Get("publish").Bool(); ok && !p {
		facts.Module.Private = true
	}
	// A virtual manifest — `[workspace]` with no `[package]` — is the root of a
	// Cargo workspace and declares its members.
	ws := root.Get("workspace")
	facts.Module.Workspaces = append(facts.Module.Workspaces, ws.Get("members").Strings()...)

	for _, s := range cargoScopes {
		addCargoDeps(&facts, root.Get(s.key), s.scope)
		// Target-specific tables: [target.'cfg(unix)'.dependencies].
		root.Get("target").Each(func(_ string, tgt *Node) bool {
			addCargoDeps(&facts, tgt.Get(s.key), s.scope)
			return true
		})
		// A workspace root declares versions its members inherit.
		addCargoDeps(&facts, ws.Get(s.key), s.scope)
	}

	// [[bin]] entries, and the implicit src/main.rs binary.
	for _, b := range root.Get("bin").Seq() {
		name := b.Get("name").String()
		if name == "" {
			continue
		}
		facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
			Name: name, Path: b.Get("path").String(), Line: b.Line,
		})
	}
	// Cargo aliases are the closest thing a crate has to npm scripts.
	// They live in .cargo/config.toml, not here, so nothing to read.
	return facts
}

// addCargoDeps reads one Cargo dependency table.
func addCargoDeps(facts *Facts, tbl *Node, scope DepScope) {
	tbl.Each(func(name string, spec *Node) bool {
		d := Dep{
			Name: name, Version: specVersion(spec), Scope: scope,
			Ecosystem: "crates.io", Line: lineOf(spec, tbl.Line),
		}
		if spec.Kind == KindMap {
			if o, ok := spec.Get("optional").Bool(); ok {
				d.Optional = o
			}
			d.Source = firstNonEmpty(spec.Get("git").String(), spec.Get("path").String(), spec.Get("registry").String())
			// `package = "x"` renames a dependency locally. The real crate name is
			// what belongs in the graph — a node named for the local alias would not
			// match the same crate declared normally elsewhere.
			if real := spec.Get("package").String(); real != "" {
				d.Name = real
			}
			// `workspace = true` inherits the constraint from the workspace root.
			// Recording the marker rather than an empty version says why it is empty.
			if w, ok := spec.Get("workspace").Bool(); ok && w && d.Version == "" {
				d.Version = "workspace"
			}
		}
		facts.Deps = append(facts.Deps, d)
		return true
	})
}

// firstNonEmpty returns the first non-empty argument.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
