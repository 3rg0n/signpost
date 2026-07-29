package manifest

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

// file builds a discover.File the way the walker would, so an extractor test
// exercises the same input shape a real run does.
func file(p, content string) discover.File {
	return discover.File{Path: p, Class: discover.ClassManifest, Content: content}
}

// depOf finds a dependency by name and scope, failing if it is absent. Lookup by
// identity rather than index keeps the test readable and independent of sort order.
func depOf(t *testing.T, f Facts, name string, scope DepScope) Dep {
	t.Helper()
	for _, d := range f.Deps {
		if d.Name == name && d.Scope == scope {
			return d
		}
	}
	t.Fatalf("no %s dependency named %q in %v", scope, name, f.DepNames())
	return Dep{}
}

func noDep(t *testing.T, f Facts, name string) {
	t.Helper()
	for _, d := range f.Deps {
		if d.Name == name {
			t.Fatalf("%q should not be recorded as a dependency", name)
		}
	}
}

func TestGoModExtraction(t *testing.T) {
	facts := ExtractGoMod(file("go.mod", `module github.com/3rg0n/signpost

go 1.24

toolchain go1.26.5

require (
	github.com/google/uuid v1.6.0
	golang.org/x/sync v0.8.0 // indirect
)

require github.com/spf13/cobra v1.8.1

replace github.com/broken/pkg => github.com/ourfork/pkg v1.2.3

replace github.com/3rg0n/signpost/tools => ./tools

exclude github.com/bad/pkg v0.0.1
`))
	facts.Normalize()

	if facts.Module.Name != "github.com/3rg0n/signpost" {
		t.Errorf("module = %q", facts.Module.Name)
	}
	// A toolchain pin is what actually builds, so it wins over the `go` directive.
	if facts.Module.LangVersion != "1.26.5" {
		t.Errorf("lang version = %q, want the toolchain pin", facts.Module.LangVersion)
	}
	if d := depOf(t, facts, "github.com/google/uuid", ScopeRuntime); d.Version != "v1.6.0" {
		t.Errorf("uuid version = %q", d.Version)
	}
	// The `// indirect` marker is the only meaningful comment in the grammar, and it
	// separates what a human chose from what the resolver added.
	depOf(t, facts, "golang.org/x/sync", ScopeIndirect)
	if got := strings.Join(facts.DirectDepNames(), ","); strings.Contains(got, "x/sync") {
		t.Errorf("indirect deps should not be direct: %s", got)
	}
	depOf(t, facts, "github.com/spf13/cobra", ScopeRuntime)
	// A replace onto a fork keeps the fork visible as the source.
	if d := depOf(t, facts, "github.com/broken/pkg", ScopeRuntime); d.Source != "github.com/ourfork/pkg" {
		t.Errorf("fork replace source = %q", d.Source)
	}
	// A local replace is a module in this repo, not an external dependency.
	noDep(t, facts, "github.com/3rg0n/signpost/tools")
	if got := strings.Join(facts.Module.Workspaces, ","); got != "tools" {
		t.Errorf("workspaces = %q, want the local replace target", got)
	}
	// An excluded version is a version-selection fact, not an architectural one, so
	// it is parsed but not surfaced as a dependency of this module.
	noDep(t, facts, "github.com/bad/pkg")
}

func TestGoWorkExtraction(t *testing.T) {
	facts := ExtractGoMod(file("go.work", `go 1.24

use (
	./api
	./worker
)

use ./cli
`))
	facts.Normalize()
	if got := strings.Join(facts.Module.Workspaces, ","); got != "./api,./cli,./worker" {
		t.Errorf("workspaces = %q", got)
	}
}

func TestGoModMalformedIsRecordedNotFatal(t *testing.T) {
	facts := ExtractGoMod(file("go.mod", `module m
go 1.24
frobnicate v1
require github.com/a/b v1.0.0
`))
	if !facts.Incomplete {
		t.Error("an unrecognised directive should mark the facts incomplete")
	}
	depOf(t, facts, "github.com/a/b", ScopeRuntime)
}

func TestPackageJSONExtraction(t *testing.T) {
	facts := ExtractPackageJSON(file("package.json", `{
  "name": "@signpost/viewer",
  "version": "0.1.0",
  "private": true,
  "engines": { "node": ">=22" },
  "bin": { "sp-view": "./dist/cli.js" },
  "scripts": { "build": "vite build", "test": "vitest run" },
  "dependencies": {
    "cytoscape": "^3.30.0",
    "patched": "git+https://github.com/us/patched.git#v1",
    "shorthand": "owner/repo",
    "local": "workspace:*"
  },
  "devDependencies": { "vite": "^5.4.0" },
  "peerDependencies": { "react": ">=18" },
  "optionalDependencies": { "fsevents": "^2.3.3" },
  "workspaces": ["apps/*", "packages/*"]
}`))
	facts.Normalize()

	if facts.Module.Name != "@signpost/viewer" || facts.Module.Version != "0.1.0" {
		t.Errorf("module = %+v", facts.Module)
	}
	if !facts.Module.Private {
		t.Error("private should be set")
	}
	if facts.Module.LangVersion != ">=22" {
		t.Errorf("engines.node = %q", facts.Module.LangVersion)
	}
	if got := strings.Join(facts.Module.Workspaces, ","); got != "apps/*,packages/*" {
		t.Errorf("workspaces = %q", got)
	}
	if d := depOf(t, facts, "cytoscape", ScopeRuntime); d.Version != "^3.30.0" || d.Source != "" {
		t.Errorf("cytoscape = %+v", d)
	}
	// A git dependency has no registry to publish an advisory against, so the origin
	// is a fact worth keeping.
	if d := depOf(t, facts, "patched", ScopeRuntime); !strings.HasPrefix(d.Source, "git+") {
		t.Errorf("git dep source = %q", d.Source)
	}
	if d := depOf(t, facts, "shorthand", ScopeRuntime); d.Source != "owner/repo" {
		t.Errorf("shorthand source = %q", d.Source)
	}
	if d := depOf(t, facts, "local", ScopeRuntime); d.Source != "workspace:*" {
		t.Errorf("workspace dep source = %q", d.Source)
	}
	depOf(t, facts, "vite", ScopeDev)
	// A peer dependency is the consumer's obligation, not this package's runtime one.
	depOf(t, facts, "react", ScopeBuild)
	if d := depOf(t, facts, "fsevents", ScopeBuild); !d.Optional {
		t.Error("an optionalDependency should be marked optional")
	}
	if got := len(facts.Scripts); got != 2 {
		t.Errorf("scripts = %d", got)
	}
	if facts.Scripts[0].Name != "build" || facts.Scripts[0].Command != "vite build" {
		t.Errorf("first script = %+v", facts.Scripts[0])
	}
	if len(facts.Entrypoints) != 1 || facts.Entrypoints[0].Name != "sp-view" {
		t.Errorf("entrypoints = %+v", facts.Entrypoints)
	}
}

// A string bin is one executable named after the package.
func TestPackageJSONStringBin(t *testing.T) {
	facts := ExtractPackageJSON(file("package.json", `{"name":"tool","bin":"./cli.js"}`))
	if len(facts.Entrypoints) != 1 || facts.Entrypoints[0].Name != "tool" ||
		facts.Entrypoints[0].Path != "./cli.js" {
		t.Errorf("entrypoints = %+v", facts.Entrypoints)
	}
}

func TestPackageJSONMalformedIsIncomplete(t *testing.T) {
	facts := ExtractPackageJSON(file("package.json", `{"name": "x",`))
	if !facts.Incomplete || facts.Note == "" {
		t.Error("a malformed package.json should be reported as unread, with a reason")
	}
}

func TestPyProjectPEP621(t *testing.T) {
	facts := ExtractPyProject(file("pyproject.toml", `
[project]
name = "signpost-svc"
version = "0.2.0"
requires-python = ">=3.12"
dependencies = [
  "httpx>=0.27",
  "uvicorn[standard]>=0.30",
  "Zope.Interface==5.0",
  "vendored @ git+https://example.com/v.git",
]

[project.optional-dependencies]
tracing = ["opentelemetry-sdk>=1.25"]

[project.scripts]
svc = "signpost_svc.cli:main"

[dependency-groups]
dev = ["pytest>=8.3", "ruff>=0.6"]

[tool.uv]
dev-dependencies = ["mypy>=1.11"]
`))
	facts.Normalize()

	if facts.Module.Name != "signpost-svc" || facts.Module.LangVersion != ">=3.12" {
		t.Errorf("module = %+v", facts.Module)
	}
	if d := depOf(t, facts, "httpx", ScopeRuntime); d.Version != ">=0.27" {
		t.Errorf("httpx = %+v", d)
	}
	// An extra changes what gets installed, not which package.
	if d := depOf(t, facts, "uvicorn", ScopeRuntime); d.Version != ">=0.30" {
		t.Errorf("uvicorn = %+v", d)
	}
	// PEP 503 normalisation, so the same package declared two ways is one node.
	if d := depOf(t, facts, "zope-interface", ScopeRuntime); d.Version != "==5.0" {
		t.Errorf("zope.interface = %+v", d)
	}
	if d := depOf(t, facts, "vendored", ScopeRuntime); d.Source == "" {
		t.Errorf("direct reference source = %+v", d)
	}
	if d := depOf(t, facts, "opentelemetry-sdk", ScopeRuntime); !d.Optional {
		t.Error("an extra's dependency should be optional")
	}
	depOf(t, facts, "pytest", ScopeDev)
	depOf(t, facts, "ruff", ScopeDev)
	depOf(t, facts, "mypy", ScopeDev)
	if len(facts.Entrypoints) != 1 || facts.Entrypoints[0].Path != "signpost_svc.cli:main" {
		t.Errorf("entrypoints = %+v", facts.Entrypoints)
	}
}

func TestPyProjectPoetry(t *testing.T) {
	facts := ExtractPyProject(file("pyproject.toml", `
[tool.poetry]
name = "legacy-svc"
version = "1.4.0"

[tool.poetry.dependencies]
python = "^3.11"
requests = "^2.32"
boto3 = { version = "^1.35", optional = true }
internal = { path = "../internal" }

[tool.poetry.group.dev.dependencies]
pytest = "^8.0"

[tool.poetry.scripts]
legacy = "legacy_svc:main"
`))
	facts.Normalize()

	if facts.Module.Name != "legacy-svc" {
		t.Errorf("module = %+v", facts.Module)
	}
	// Poetry declares the interpreter as a dependency named "python". That is a
	// language version, not a package.
	noDep(t, facts, "python")
	if facts.Module.LangVersion != "^3.11" {
		t.Errorf("lang version = %q, want the python constraint", facts.Module.LangVersion)
	}
	if d := depOf(t, facts, "requests", ScopeRuntime); d.Version != "^2.32" {
		t.Errorf("requests = %+v", d)
	}
	if d := depOf(t, facts, "boto3", ScopeRuntime); !d.Optional || d.Version != "^1.35" {
		t.Errorf("boto3 = %+v", d)
	}
	if d := depOf(t, facts, "internal", ScopeRuntime); d.Source != "../internal" {
		t.Errorf("path dep source = %q", d.Source)
	}
	depOf(t, facts, "pytest", ScopeDev)
	if len(facts.Entrypoints) != 1 {
		t.Errorf("entrypoints = %+v", facts.Entrypoints)
	}
}

func TestRequirementsExtraction(t *testing.T) {
	facts := ExtractRequirements(file("requirements.txt", `
# runtime deps
httpx>=0.27
Django==5.1.1
pydantic  # no constraint
package @ https://example.com/pkg.whl
git+https://github.com/o/r.git#egg=forked
-r base.txt
--index-url https://internal/simple
`))
	facts.Normalize()

	if d := depOf(t, facts, "httpx", ScopeRuntime); d.Version != ">=0.27" {
		t.Errorf("httpx = %+v", d)
	}
	if d := depOf(t, facts, "django", ScopeRuntime); d.Version != "==5.1.1" {
		t.Errorf("django = %+v", d)
	}
	if d := depOf(t, facts, "pydantic", ScopeRuntime); d.Version != "" {
		t.Errorf("pydantic = %+v", d)
	}
	if d := depOf(t, facts, "package", ScopeRuntime); d.Source == "" {
		t.Errorf("direct reference = %+v", d)
	}
	// A bare VCS URL's package name lives in the #egg fragment, which the comment
	// stripper must not truncate.
	if d := depOf(t, facts, "forked", ScopeRuntime); d.Source != "git+https://github.com/o/r.git" {
		t.Errorf("egg fragment = %+v", d)
	}
	// Options are not dependencies. The referenced file is discovered on its own.
	noDep(t, facts, "base.txt")
	if len(facts.Deps) != 5 {
		t.Errorf("dep count = %d: %v", len(facts.Deps), facts.DepNames())
	}
}

// The filename says what the requirements are for, and reporting a dev tool as a
// runtime requirement would overstate what the project ships.
func TestRequirementsFilenameDecidesScope(t *testing.T) {
	for _, name := range []string{"requirements-dev.txt", "requirements/test.txt", "dev-requirements.txt"} {
		facts := ExtractRequirements(file(name, "pytest>=8\n"))
		depOf(t, facts, "pytest", ScopeDev)
	}
	facts := ExtractRequirements(file("requirements.txt", "httpx>=0.27\n"))
	depOf(t, facts, "httpx", ScopeRuntime)
}

func TestCargoExtraction(t *testing.T) {
	facts := ExtractCargo(file("Cargo.toml", `
[package]
name = "signpost-core"
version = "0.1.0"
edition = "2021"
rust-version = "1.80"
publish = false

[dependencies]
serde = { version = "1.0", features = ["derive"] }
tokio = "1.40"
renamed = { package = "real-crate", version = "0.3" }
forked = { git = "https://github.com/us/forked" }
inherited = { workspace = true }
extra = { version = "0.1", optional = true }

[dev-dependencies]
proptest = "1.5"

[build-dependencies]
cc = "1.1"

[target.'cfg(unix)'.dependencies]
nix = "0.29"

[[bin]]
name = "spctl"
path = "src/bin/spctl.rs"

[[bin]]
name = "spd"
path = "src/bin/spd.rs"
`))
	facts.Normalize()

	if facts.Module.Name != "signpost-core" || facts.Module.LangVersion != "1.80" {
		t.Errorf("module = %+v", facts.Module)
	}
	if !facts.Module.Private {
		t.Error("publish = false means private")
	}
	if d := depOf(t, facts, "serde", ScopeRuntime); d.Version != "1.0" {
		t.Errorf("serde = %+v", d)
	}
	if d := depOf(t, facts, "tokio", ScopeRuntime); d.Version != "1.40" {
		t.Errorf("tokio = %+v", d)
	}
	// A renamed dependency belongs in the graph under the real crate name, or it
	// will not match the same crate declared normally elsewhere.
	depOf(t, facts, "real-crate", ScopeRuntime)
	noDep(t, facts, "renamed")
	if d := depOf(t, facts, "forked", ScopeRuntime); d.Source == "" {
		t.Errorf("git dep = %+v", d)
	}
	if d := depOf(t, facts, "inherited", ScopeRuntime); d.Version != "workspace" {
		t.Errorf("workspace inheritance = %+v, want the marker not an empty version", d)
	}
	if d := depOf(t, facts, "extra", ScopeRuntime); !d.Optional {
		t.Error("extra should be optional")
	}
	depOf(t, facts, "proptest", ScopeDev)
	depOf(t, facts, "cc", ScopeBuild)
	// A target-specific table is still a dependency of this crate.
	depOf(t, facts, "nix", ScopeRuntime)
	if len(facts.Entrypoints) != 2 || facts.Entrypoints[0].Name != "spctl" {
		t.Errorf("entrypoints = %+v", facts.Entrypoints)
	}
}

// A virtual manifest has no [package] and declares the workspace's members.
func TestCargoVirtualManifest(t *testing.T) {
	facts := ExtractCargo(file("Cargo.toml", `
[workspace]
members = ["crates/core", "crates/cli"]

[workspace.dependencies]
serde = "1.0"
`))
	facts.Normalize()
	if facts.Module.Name != "" {
		t.Errorf("a virtual manifest declares no package, got %q", facts.Module.Name)
	}
	if got := strings.Join(facts.Module.Workspaces, ","); got != "crates/cli,crates/core" {
		t.Errorf("members = %q", got)
	}
	depOf(t, facts, "serde", ScopeRuntime)
}

// The same dependency in two scopes is two facts: a package that is both a runtime
// and a dev dependency really does ship, and collapsing them would lose that.
func TestDepScopeIsPartOfIdentity(t *testing.T) {
	facts := ExtractPackageJSON(file("package.json",
		`{"dependencies":{"x":"1"},"devDependencies":{"x":"1"}}`))
	facts.Normalize()
	if len(facts.Deps) != 2 {
		t.Fatalf("dep count = %d, want the runtime and dev declarations kept apart", len(facts.Deps))
	}
	depOf(t, facts, "x", ScopeRuntime)
	depOf(t, facts, "x", ScopeDev)
}

func TestNormalizeIsIdempotentAndSorted(t *testing.T) {
	src := `{"dependencies":{"z":"1","a":"2","m":"3"}}`
	facts := ExtractPackageJSON(file("package.json", src))
	facts.Normalize()
	first := strings.Join(facts.DepNames(), ",")
	if first != "a,m,z" {
		t.Errorf("deps = %q, want sorted", first)
	}
	facts.Normalize()
	if got := strings.Join(facts.DepNames(), ","); got != first {
		t.Errorf("second Normalize changed the output: %q", got)
	}
}

func TestDepExtractionIsDeterministic(t *testing.T) {
	f := file("Cargo.toml", `
[package]
name = "x"
[dependencies]
b = "1"
a = { version = "2", features = ["p", "q"] }
[[bin]]
name = "one"
`)
	render := func() string {
		facts := ExtractCargo(f)
		facts.Normalize()
		var b strings.Builder
		for _, d := range facts.Deps {
			b.WriteString(d.Name + "@" + d.Version + "/" + string(d.Scope) + ";")
		}
		for _, e := range facts.Entrypoints {
			b.WriteString(e.Name + ">" + e.Path + ";")
		}
		return b.String()
	}
	first := render()
	for i := 0; i < 10; i++ {
		if got := render(); got != first {
			t.Fatalf("run %d differed: %q vs %q", i, got, first)
		}
	}
}
