package assemble

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
	"github.com/3rg0n/signpost/internal/extract"
	"github.com/3rg0n/signpost/internal/graph"
	"github.com/3rg0n/signpost/internal/manifest"
	"github.com/3rg0n/signpost/internal/vcs"
)

// build runs the whole pipeline over a repository written to a temp directory.
//
// Through discover.Walk rather than by constructing files directly, and through the
// real extractor and reader registries rather than hand-built Facts. Both choices are
// deliberate: classification is unexported and belongs to discover, and a hand-built
// Facts value would keep passing after an extractor changed what it emits — the shapes
// the extractors actually produce are the contract this package is written against.
// The cost is that a failure here can be an extractor's fault, which the per-package
// tests separate out.
func build(t *testing.T, files map[string]string) *Result {
	t.Helper()
	return buildWithHistory(t, files, nil)
}

// buildWithHistory is build with git signals supplied. Separate rather than a variadic
// on build, so the twenty-odd callers that have nothing to say about history do not
// have to say it.
func buildWithHistory(t *testing.T, files map[string]string, h *vcs.Signals) *Result {
	t.Helper()
	root := t.TempDir()
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(full, []byte(files[p]), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	res, err := discover.Walk(root, discover.Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	out, err := Build(Input{
		Discovered: res,
		Source:     extract.DefaultRegistry().Run(res),
		Manifests:  manifest.DefaultRegistry().Run(res),
		History:    h,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return out
}

// node returns the node with the given ID, failing if absent.
func node(t *testing.T, g *graph.Graph, id string) *graph.Node {
	t.Helper()
	n := g.Node(id)
	if n == nil {
		var have []string
		for _, x := range g.Nodes() {
			have = append(have, x.ID)
		}
		t.Fatalf("no node %q; have %v", id, have)
	}
	return n
}

// hasEdge reports whether an edge of the given kind connects two nodes.
func hasEdge(g *graph.Graph, from, to string, kind graph.EdgeKind) bool {
	for _, e := range g.EdgesFrom(from) {
		if e.To == to && e.Kind == kind {
			return true
		}
	}
	return false
}

func TestModuleNodePerDirectory(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                  "module example.com/app\n\ngo 1.26\n",
		"main.go":                 "package main\n\nimport \"example.com/app/internal/auth\"\n\nfunc main() { auth.Check() }\n",
		"internal/auth/auth.go":   "package auth\n\n// Check validates a token.\nfunc Check() bool { return true }\n\nfunc helper() {}\n",
		"internal/auth/token.go":  "package auth\n\ntype Token struct{}\n",
		"internal/store/store.go": "package store\n\nfunc Put() {}\n",
	})

	root := node(t, out.Graph, "/modules/root")
	if root.Title != "(repository root)" {
		t.Errorf("root title = %q", root.Title)
	}
	if root.Attrs["entrypoints"] == "" {
		t.Error("a directory with func main is where execution starts; that must be recorded")
	}
	if !containsStr(root.Tags, "entrypoint") {
		t.Errorf("root tags = %v", root.Tags)
	}

	auth := node(t, out.Graph, "/modules/auth")
	// Both files in the directory belong to the one node: directory granularity is
	// the grouping every one of the four languages agrees on.
	if len(auth.Files) != 2 {
		t.Errorf("auth files = %v, want both", auth.Files)
	}
	if auth.Lang != "go" || auth.Attrs["package"] != "auth" {
		t.Errorf("auth = %+v", auth)
	}
	// Check and Token are exported; helper is not.
	if auth.Attrs["exported"] != "2" {
		t.Errorf("exported = %q, want 2", auth.Attrs["exported"])
	}
}

// The resolver's whole job, on the case that matters: an import inside the module
// resolves by module path prefix, exactly as the toolchain resolves it.
func TestGoImportResolvesToModule(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                  "module example.com/app\n\ngo 1.26\n\nrequire example.com/ext v1.2.3\n",
		"main.go":                 "package main\n\nimport (\n\t\"fmt\"\n\t\"example.com/app/internal/auth\"\n\t\"example.com/ext/thing\"\n)\n\nfunc main() { fmt.Println(auth.Check(), thing.X) }\n",
		"internal/auth/auth.go":   "package auth\n\nimport \"example.com/app/internal/store\"\n\nfunc Check() bool { return store.Get() }\n",
		"internal/store/store.go": "package store\n\nfunc Get() bool { return true }\n",
	})
	g := out.Graph

	if !hasEdge(g, "/modules/root", "/modules/auth", graph.EdgeImports) {
		t.Error("root -> auth imports edge missing")
	}
	if !hasEdge(g, "/modules/auth", "/modules/store", graph.EdgeImports) {
		t.Error("auth -> store imports edge missing")
	}
	// A declared dependency, imported at a deeper path than the module path: the
	// longest-prefix candidate walk is what finds it.
	if !hasEdge(g, "/modules/root", "/references/go-example-com-ext", graph.EdgeImports) {
		var got []string
		for _, e := range g.EdgesFrom("/modules/root") {
			got = append(got, e.To)
		}
		t.Errorf("root edges = %v, want one to the declared dependency", got)
	}
	// `fmt` is resolved — to nothing. Counting it would make every honest repo look
	// like assembly had failed.
	if len(out.Unresolved) != 0 {
		t.Errorf("unresolved = %v, want none: fmt is stdlib", out.Unresolved)
	}
}

// An import matching no declared dependency is counted, never turned into a node.
// The manifest is the repository's only authoritative statement about its supply
// chain, and inventing an entry would misreport it.
func TestUndeclaredImportIsReportedNotInvented(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.26\n",
		"main.go": "package main\n\nimport \"github.com/undeclared/thing\"\n\nfunc main() { thing.X() }\n",
	})
	for _, n := range out.Graph.Nodes() {
		if n.Kind == graph.KindExternal {
			t.Errorf("invented external node %q for an undeclared import", n.ID)
		}
	}
	if out.Unresolved["go github.com/undeclared/thing"] != 1 {
		t.Errorf("unresolved = %v", out.Unresolved)
	}
}

// Counted per specifier rather than per occurrence: a repo importing one unresolvable
// package from forty files has one gap to fix, not forty.
func TestUnresolvedIsCountedPerSpecifier(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26\n",
		"a.go":   "package app\n\nimport \"github.com/x/y\"\n",
		"b.go":   "package app\n\nimport \"github.com/x/y\"\n",
		"c.go":   "package app\n\nimport \"github.com/x/y\"\n",
	})
	if len(out.Unresolved) != 1 || out.Unresolved["go github.com/x/y"] != 3 {
		t.Errorf("unresolved = %v, want one key counted three times", out.Unresolved)
	}
}

// A first-party import pointing at no node is counted, and counted apart from the
// specifiers that could not be placed at all.
//
// `example.com/app/gen` is inside the declared module, so the resolver knows exactly
// where it belongs and correctly declines to invent an external node for it — a
// reference page there would claim the repository depends on itself from outside. But
// the branch that reached that conclusion recorded nothing, so the edge went missing
// with no count anywhere admitting it. A module whose every import landed here read as
// importing nothing, and the coverage report agreed.
//
// Both halves in one test because the fix has two directions and each is a distinct
// wrong answer: routing the specifier to Unresolved tells the reader to go declare a
// dependency on their own code, and leaving it uncounted is the shipped bug.
func TestAFirstPartyImportWithNoTargetIsCountedSeparately(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26\n",
		// Two importers of the one specifier, so the per-specifier grouping is asserted
		// here too rather than assumed to carry over from Unresolved.
		"a.go": "package app\n\nimport (\n\t\"example.com/app/gen\"\n\t\"github.com/x/y\"\n)\n",
		"b.go": "package app\n\nimport \"example.com/app/gen\"\n",
		// gen/ exists and holds no source, which is what generated or build-tagged
		// directories look like to the walk. Without a file here the directory is not in
		// the tree at all and the import is a different case.
		"gen/README.md": "# generated\n",
	})
	if len(out.Unlinked) != 1 || out.Unlinked["go example.com/app/gen"] != 2 {
		t.Errorf("unlinked = %v, want one key counted twice", out.Unlinked)
	}
	// The negative: it must not also appear as unresolved. Reporting it there names a
	// gap whose only available fix is wrong — the specifier is this repository's own,
	// and nothing a manifest could declare would resolve it.
	if _, bad := out.Unresolved["go example.com/app/gen"]; bad {
		t.Errorf("a first-party specifier is reported unresolved: %v", out.Unresolved)
	}
	// And the two maps stay distinct in the other direction, which is what stops a fix
	// that merged them from passing: an external near-miss belongs only in Unresolved.
	if out.Unresolved["go github.com/x/y"] != 1 {
		t.Errorf("unresolved = %v, want the external specifier counted once", out.Unresolved)
	}
	if _, bad := out.Unlinked["go github.com/x/y"]; bad {
		t.Errorf("an external specifier is reported as first-party: %v", out.Unlinked)
	}
	// No node invented for either, which is the constraint the whole branch exists to
	// honour: an external page for `example.com/app/gen` is a false supply-chain claim.
	for _, n := range out.Graph.Nodes() {
		if n.Kind == graph.KindExternal {
			t.Errorf("invented external node %q for an unplaceable import", n.ID)
		}
	}
}

// Stdlib is in neither map. It is resolved — to nothing worth a node — and a repository
// that imports `fmt` and `os` has no gap to fix.
//
// The boundary matters more now that there are two maps: `internal` is what routes a
// specifier to Unlinked, and a stdlib import that came back internal by mistake would be
// counted as a missing first-party page, making every honest Go file a coverage gap.
func TestStdlibIsInNeitherGapMap(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod": "module example.com/app\n\ngo 1.26\n",
		"a.go":   "package app\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"path/filepath\"\n)\n",
	})
	if len(out.Unresolved) != 0 || len(out.Unlinked) != 0 {
		t.Errorf("unresolved = %v, unlinked = %v, want both empty: fmt, os and "+
			"path/filepath are the standard library", out.Unresolved, out.Unlinked)
	}
}

func TestPythonRelativeAndAbsoluteImports(t *testing.T) {
	out := build(t, map[string]string{
		"pyproject.toml":       "[project]\nname = \"app\"\nversion = \"1.0\"\ndependencies = [\"requests>=2.0\", \"PyYAML\"]\n",
		"app/__init__.py":      "",
		"app/main.py":          "import os\nimport requests\nimport yaml\nfrom .store import get\nfrom app.util import helper\n\ndef run():\n    return get(), helper()\n",
		"app/store.py":         "def get():\n    return 1\n",
		"app/util/util.py":     "def helper():\n    return 2\n",
		"app/util/__init__.py": "from .util import helper\n",
	})
	g := out.Graph

	// `from .store import get` targets the same directory, so the edge is a self-edge
	// and the graph drops it. Asserted so the absence is never read as a resolution
	// failure, and so `.store` is not counted as an unresolved import either.
	if hasEdge(g, "/modules/app", "/modules/app", graph.EdgeImports) {
		t.Error("a self-edge carries no signal and must be dropped")
	}
	if _, bad := out.Unresolved["python .store"]; bad {
		t.Error("a relative import is internal by construction, resolved or not")
	}
	if !hasEdge(g, "/modules/app", "/modules/util", graph.EdgeImports) {
		t.Error("app -> util edge missing: an absolute intra-repo import must resolve")
	}
	// `PyYAML` is imported as `yaml`. Distribution name and import name differ, and
	// the resolver registers both spellings for exactly this case.
	if !hasEdge(g, "/modules/app", "/references/pypi-pyyaml", graph.EdgeImports) {
		var got []string
		for _, e := range g.EdgesFrom("/modules/app") {
			got = append(got, e.To)
		}
		t.Errorf("app edges = %v, want one to PyYAML", got)
	}
	if _, bad := out.Unresolved["python os"]; bad {
		t.Error("os is stdlib and must not be reported unresolved")
	}
}

// A Python monorepo resolves an absolute import against the package that declares it, and
// that package is not the repository root. Before per-package roots this returned nothing:
// `resolvePython` tried the root and `src` only.
func TestPythonPackageRootResolvesAbsoluteImport(t *testing.T) {
	out := build(t, map[string]string{
		"services/alpha/pyproject.toml":  "[project]\nname = \"alpha\"\nversion = \"1.0\"\n",
		"services/alpha/handler.py":      "from api.client import fetch\n\ndef run():\n    return fetch()\n",
		"services/alpha/api/__init__.py": "",
		"services/alpha/api/client.py":   "def fetch():\n    return 1\n",
	})
	if !hasEdge(out.Graph, "/modules/alpha", "/modules/api", graph.EdgeImports) {
		t.Errorf("no alpha -> api edge. `api.client` is absolute and names a top-level package, "+
			"which resolves against the directory holding pyproject.toml; unresolved = %v",
			out.Unresolved)
	}
	if _, bad := out.Unresolved["python api.client"]; bad {
		t.Errorf("api.client reported as a gap: %v", out.Unresolved)
	}
}

// And the boundary in the other direction, which is the one that invents structure. Two
// packages holding the same module path is the shape that makes a repo-wide root list wrong:
// the specifier cannot distinguish them, so only the root's scope can.
func TestPythonPackageRootDoesNotReachASibling(t *testing.T) {
	out := build(t, map[string]string{
		"services/alpha/pyproject.toml":  "[project]\nname = \"alpha\"\nversion = \"1.0\"\n",
		"services/alpha/handler.py":      "from api.client import fetch\n\ndef run():\n    return fetch()\n",
		"services/alpha/api/__init__.py": "",
		"services/alpha/api/client.py":   "def fetch():\n    return 1\n",
		"services/beta/pyproject.toml":   "[project]\nname = \"beta\"\nversion = \"1.0\"\n",
		"services/beta/handler.py":       "from api.client import fetch\n\ndef run():\n    return fetch()\n",
		"services/beta/api/__init__.py":  "",
		"services/beta/api/client.py":    "def fetch():\n    return 2\n",
	})
	g := out.Graph
	// Each package's own api directory. Both slug to `api`, so one holds `/modules/api`
	// and the other carries a suffix derived from its path — asked for by path here
	// rather than spelled literally, because the suffix is deliberately not something a
	// reader should be able to predict from ordering.
	ids := map[string]string{}
	for _, n := range g.NodesOfKind(graph.KindModule) {
		ids[n.Path] = n.ID
	}
	alphaAPI, betaAPI := ids["services/alpha/api"], ids["services/beta/api"]
	if alphaAPI == "" || betaAPI == "" || alphaAPI == betaAPI {
		t.Fatalf("want two distinct api module nodes; got alpha=%q beta=%q", alphaAPI, betaAPI)
	}
	if !hasEdge(g, "/modules/alpha", alphaAPI, graph.EdgeImports) ||
		!hasEdge(g, "/modules/beta", betaAPI, graph.EdgeImports) {
		var got []string
		for _, e := range g.Edges() {
			if e.Kind == graph.EdgeImports {
				got = append(got, e.From+" -> "+e.To)
			}
		}
		t.Errorf("each package must import its own api; imports = %v", got)
	}
	if hasEdge(g, "/modules/alpha", betaAPI, graph.EdgeImports) ||
		hasEdge(g, "/modules/beta", alphaAPI, graph.EdgeImports) {
		t.Error("one package's absolute import resolved into the other's code. Neither declares " +
			"the other and neither can see it, so this is an edge between two packages that " +
			"cannot import each other — invented structure, reported as extracted")
	}
}

// A requirements.txt is a pin list, not a package boundary, and `requirements/base.txt` is a
// real spelling. Registering its directory as a resolution root would make `requirements/` a
// package and resolve imports into a directory holding no code at all.
func TestRequirementsDirectoryIsNotAPythonRoot(t *testing.T) {
	out := build(t, map[string]string{
		"requirements/base.txt": "httpx>=0.27\n",
		"requirements/api.py":   "def helper():\n    return 1\n",
		"pkg/__init__.py":       "",
		"pkg/main.py":           "from api import helper\n\ndef run():\n    return helper()\n",
	})
	if hasEdge(out.Graph, "/modules/pkg", "/modules/requirements", graph.EdgeImports) {
		t.Error("a requirements directory became a resolution root, so `from api import helper` " +
			"resolved into the directory holding the pin lists")
	}
}

func TestTypeScriptRelativeAndPackageImports(t *testing.T) {
	out := build(t, map[string]string{
		"package.json":      "{\"name\":\"app\",\"dependencies\":{\"react\":\"^18.0.0\",\"@scope/util\":\"^1.0.0\"}}",
		"src/index.ts":      "import { helper } from './lib/helper';\nimport React from 'react';\nimport { x } from '@scope/util/deep/path';\nimport fs from 'node:fs';\nexport const run = () => helper(React, x, fs);\n",
		"src/lib/helper.ts": "export function helper(a: unknown, b: unknown, c: unknown) { return [a, b, c]; }\n",
	})
	g := out.Graph

	if !hasEdge(g, "/modules/src", "/modules/lib", graph.EdgeImports) {
		t.Error("relative specifier must resolve to the directory holding the file")
	}
	if !hasEdge(g, "/modules/src", "/references/npm-react", graph.EdgeImports) {
		t.Error("react edge missing")
	}
	// A deep import of a scoped package is one dependency on the package, not on the
	// path inside it.
	if !hasEdge(g, "/modules/src", "/references/npm-scope-util", graph.EdgeImports) {
		var got []string
		for _, e := range g.EdgesFrom("/modules/src") {
			got = append(got, e.To)
		}
		t.Errorf("src edges = %v, want one to @scope/util", got)
	}
	if _, bad := out.Unresolved["typescript node:fs"]; bad {
		t.Error("node:fs is a runtime builtin, not a dependency")
	}
}

// A Node builtin addressed by subpath is the same builtin. Issue #14.
//
// `nodeBuiltin` was consulted with the whole specifier, so `fs/promises` missed a map that
// holds `fs` and was counted as a dependency the repository failed to resolve. There is no
// other way to reach the promise-based API, so this is not an exotic spelling: on
// webex/webex-js-sdk it was three of the twenty-six reported gaps. Purely cosmetic — no
// node fabricated, no edge lost — but the unresolved count is what tells a reader how much
// of their repository signpost did not understand, and inflating it with things it
// understood perfectly spends the only signal there.
//
// Ten specifiers are affected: the `/promises` variants of fs, dns, stream, timers and
// readline, plus `util/types`, `stream/web`, `stream/consumers`, `assert/strict` and
// `path/posix`.
func TestNodeBuiltinSubpathsAreTheRuntime(t *testing.T) {
	out := build(t, map[string]string{
		"package.json": "{\"name\":\"app\"}",
		"src/index.ts": "import fs from 'fs/promises';\n" +
			"import { pipeline } from 'node:stream/promises';\n" +
			"import { isDate } from 'util/types';\n" +
			"import { tap } from 'node:test/reporters';\n" +
			"import { resolve } from 'pathe/utils';\n" +
			"export const run = () => [fs, pipeline, isDate, tap, resolve];\n",
	})

	for _, raw := range []string{"fs/promises", "node:stream/promises", "util/types", "node:test/reporters"} {
		if _, bad := out.Unresolved["typescript "+raw]; bad {
			t.Errorf("%q is a Node builtin addressed by subpath, and it is reported as an "+
				"unresolved dependency. unresolved = %v", raw, out.Unresolved)
		}
	}
	// The counterpart, and the reason the rule cuts on the separator instead of matching a
	// prefix: `pathe` is a real npm path utility whose name starts with the builtin `path`,
	// and this manifest does not declare it. Under a prefix comparison it reads as the
	// runtime and disappears from the report — an undeclared dependency the reader is never
	// told about, which is the failure this count exists to surface and is worse than the
	// inflated count the fix removes. Asserted here rather than only in the corpus because
	// the report truncates to five specifiers and this is one of seven.
	if _, ok := out.Unresolved["typescript pathe/utils"]; !ok {
		t.Errorf("`pathe/utils` is an npm package no manifest here declares, and it is not "+
			"reported. The builtin test is matching a prefix rather than cutting the first "+
			"path segment, so an honest gap was silenced as the runtime. unresolved = %v",
			out.Unresolved)
	}
}

// A tsconfig `paths` alias is the codebase's own statement about what a specifier means,
// and the only one. `@fider/*` -> `./public/*` is why `@fider/services` is `public/services`;
// nothing else in the repository says so, so without reading it the import resolves nowhere.
func TestTypeScriptPathAliasResolves(t *testing.T) {
	out := build(t, map[string]string{
		"package.json":             "{\"name\":\"app\"}",
		"tsconfig.json":            "{\"compilerOptions\":{\"baseUrl\":\".\",\"paths\":{\"@app/*\":[\"./public/*\"]}}}",
		"public/index.ts":          "import { get } from '@app/services/store';\nexport const run = () => get();\n",
		"public/services/store.ts": "export function get() { return 1; }\n",
	})
	if !hasEdge(out.Graph, "/modules/public", "/modules/services", graph.EdgeImports) {
		t.Errorf("alias edge missing; edges = %v", edgeTargets(out.Graph, "/modules/public"))
	}
	if len(out.Unresolved) != 0 {
		t.Errorf("unresolved = %v, want none: the mapping is declared", out.Unresolved)
	}
}

// The failure mode that matters is not the missing edge but what fills it. An alias whose
// prefix is not `@`-scoped reduces to its first segment under the npm reading, so a pattern
// that matched and then fell through to the dependency lookup reports `utils/format` as a
// third-party package named `utils` — which exists on npm, so the collision is real and the
// wrong answer is indistinguishable from a right one. That is #12's defect, a fabricated
// supply-chain entry, reached by a different road.
//
// The second import is what reaches that branch: an asset addressed through the alias. The
// pattern matches and the target is not extracted source, so there is no node to point at,
// and claiming the specifier anyway is the only correct answer.
func TestTypeScriptPathAliasIsNotADependency(t *testing.T) {
	out := build(t, map[string]string{
		"package.json":        "{\"name\":\"app\",\"dependencies\":{\"utils\":\"^0.3.0\",\"react\":\"^18.0.0\"}}",
		"tsconfig.json":       "{\"compilerOptions\":{\"paths\":{\"utils/*\":[\"./src/utils/*\"]}}}",
		"src/index.ts":        "import { fmt } from 'utils/format';\nimport logo from 'utils/icons/logo.svg';\nimport React from 'react';\nexport const run = () => fmt(React) + logo;\n",
		"src/utils/format.ts": "export function fmt(x: unknown) { return String(x); }\n",
	})
	g := out.Graph
	if hasEdge(g, "/modules/src", "/references/npm-utils", graph.EdgeImports) {
		t.Error("a declared alias resolved as a third-party package; the alias is the " +
			"authoritative reading and the dependency name only collides with it")
	}
	if _, bad := out.Unresolved["typescript utils/icons/logo.svg"]; bad {
		t.Error("an asset addressed through a declared alias was reported as an unresolved " +
			"import; the mapping was read, so the specifier is not a gap")
	}
	if !hasEdge(g, "/modules/src", "/modules/utils", graph.EdgeImports) {
		t.Errorf("alias edge missing; edges = %v", edgeTargets(g, "/modules/src"))
	}
	// The counterpart, without which the assertion above is satisfied by a resolver that
	// stopped reporting dependencies at all: a specifier that is genuinely a package must
	// still reach one.
	if !hasEdge(g, "/modules/src", "/references/npm-react", graph.EdgeImports) {
		t.Errorf("react edge missing; edges = %v", edgeTargets(g, "/modules/src"))
	}
}

// `extends` is the dominant real shape: 11 of 14 tsconfig files in one monorepo declare it,
// most declaring nothing else. A resolver that read each config in isolation would find the
// aliases in the base config and never apply them to the packages that resolve by them.
//
// The base config lives in a shared directory rather than at the repository root, and that
// placement is the whole test. A root config's scope is the root, so its aliases already
// reach every file and inheritance changes nothing — the test would pass against a resolver
// that ignored `extends` entirely. `configs/` is not an ancestor of `packages/api`, so the
// inherited entry is the only thing that can resolve this import.
//
// The target is written `../shared/*` because `paths` are relative to the config declaring
// them, which is where TypeScript reads them from too.
func TestTypeScriptPathAliasInheritedThroughExtends(t *testing.T) {
	out := build(t, map[string]string{
		"package.json":               "{\"name\":\"root\",\"workspaces\":[\"packages/*\"]}",
		"configs/tsconfig.base.json": "{\"compilerOptions\":{\"paths\":{\"@app/*\":[\"../shared/*\"]}}}",
		"packages/api/tsconfig.json": "{\"extends\":\"../../configs/tsconfig.base.json\",\"include\":[\"src\"]}",
		"packages/api/package.json":  "{\"name\":\"@ws/api\"}",
		"packages/api/src/h.ts":      "import { fmt } from '@app/text/fmt';\nexport const h = () => fmt();\n",
		"shared/text/fmt.ts":         "export function fmt() { return ''; }\n",
	})
	if !hasEdge(out.Graph, "/modules/src", "/modules/text", graph.EdgeImports) {
		t.Errorf("inherited alias edge missing; edges = %v", edgeTargets(out.Graph, "/modules/src"))
	}
	if len(out.Unresolved) != 0 {
		t.Errorf("unresolved = %v, want none", out.Unresolved)
	}
}

// An alias is directory-scoped, not repo-wide. Two packages each declaring `@src/*` for
// their own source is a real and common shape — both configs in the monorepo this was drawn
// from spell it exactly that way — and resolving one package's import into the other's code
// is a wrong edge rather than a missing one, the more expensive kind.
//
// Both packages hold the same subpath on purpose. With distinct subpaths a repo-wide match
// merely fails to resolve, so the test would pass against the defect it exists to catch:
// the first-matching pattern would point at a path the other package does not have.
func TestTypeScriptPathAliasIsScopedToItsConfig(t *testing.T) {
	out := build(t, map[string]string{
		"package.json":                 "{\"name\":\"root\",\"workspaces\":[\"packages/*\"]}",
		"packages/a/tsconfig.json":     "{\"compilerOptions\":{\"paths\":{\"@src/*\":[\"src/*\"]}}}",
		"packages/a/package.json":      "{\"name\":\"@ws/a\"}",
		"packages/a/src/entry.ts":      "import { x } from '@src/thing/impl';\nexport const a = () => x();\n",
		"packages/a/src/thing/impl.ts": "export function x() { return 'a'; }\n",
		"packages/b/tsconfig.json":     "{\"compilerOptions\":{\"paths\":{\"@src/*\":[\"src/*\"]}}}",
		"packages/b/package.json":      "{\"name\":\"@ws/b\"}",
		"packages/b/src/entry.ts":      "import { x } from '@src/thing/impl';\nexport const b = () => x();\n",
		"packages/b/src/thing/impl.ts": "export function x() { return 'b'; }\n",
	})
	g := out.Graph
	if len(out.Unresolved) != 0 {
		t.Errorf("unresolved = %v, want none: each package declares its own mapping",
			out.Unresolved)
	}
	// Neither package's entry may reach the other's source. IDs are assigned by discovery
	// order, so the assertion is on the directories the nodes hold rather than on fixed IDs.
	edges := allEdges(g, graph.EdgeImports)
	crossings := 0
	for _, e := range edges {
		from, to := node(t, g, e.From), node(t, g, e.To)
		if pkgOf(from.Path) == "" || pkgOf(to.Path) == "" {
			continue
		}
		if pkgOf(from.Path) != pkgOf(to.Path) {
			crossings++
			t.Errorf("%s (%s) imports %s (%s): an alias declared in one package resolved "+
				"into another's code", e.From, from.Path, e.To, to.Path)
		}
	}
	// And each must reach its own, or the check above is satisfied by a resolver that drew
	// no edges at all.
	within := 0
	for _, e := range edges {
		from, to := node(t, g, e.From), node(t, g, e.To)
		if p := pkgOf(from.Path); p != "" && p == pkgOf(to.Path) {
			within++
		}
	}
	if within != 2 {
		t.Errorf("%d within-package alias edges, want 2 (%d crossings); edges = %v",
			within, crossings, edges)
	}
}

// `paths` targets are an ordered fallback, not a set: TypeScript tries each in turn and the
// first that exists wins. Stopping at the first target when it names no directory would
// leave the import unresolved even though the config says where it lives.
func TestTypeScriptPathAliasFallsThroughToASecondTarget(t *testing.T) {
	out := build(t, map[string]string{
		"package.json":       "{\"name\":\"app\"}",
		"tsconfig.json":      "{\"compilerOptions\":{\"paths\":{\"@ui/*\":[\"./generated/*\",\"./src/*\"]}}}",
		"src/index.ts":       "import { btn } from '@ui/widgets/btn';\nexport const run = () => btn();\n",
		"src/widgets/btn.ts": "export function btn() { return 1; }\n",
	})
	if !hasEdge(out.Graph, "/modules/src", "/modules/widgets", graph.EdgeImports) {
		t.Errorf("resolution stopped at a target that does not exist; edges = %v",
			edgeTargets(out.Graph, "/modules/src"))
	}
}

// Within one scope the longer prefix wins, so `@app/ui/*` is not matched by `@app/*` with
// `ui/` read as part of the wildcard. Both patterns match the specifier; only one is right.
func TestTypeScriptPathAliasPrefersTheLongerPattern(t *testing.T) {
	out := build(t, map[string]string{
		"package.json":   "{\"name\":\"app\"}",
		"tsconfig.json":  "{\"compilerOptions\":{\"paths\":{\"@app/*\":[\"./core/*\"],\"@app/ui/*\":[\"./widgets/*\"]}}}",
		"src/index.ts":   "import { btn } from '@app/ui/btn';\nexport const run = () => btn();\n",
		"widgets/btn.ts": "export function btn() { return 1; }\n",
		"core/ui/btn.ts": "export function btn() { return 2; }\n",
	})
	g := out.Graph
	if !hasEdge(g, "/modules/src", "/modules/widgets", graph.EdgeImports) {
		t.Errorf("the more specific pattern lost; edges = %v", edgeTargets(g, "/modules/src"))
	}
	if hasEdge(g, "/modules/src", "/modules/ui", graph.EdgeImports) {
		t.Error("`@app/*` swallowed `ui/` into its wildcard, resolving to core/ui")
	}
}

// A pattern with no wildcard is an exact specifier, matched whole rather than by prefix.
func TestTypeScriptPathAliasExactPattern(t *testing.T) {
	out := build(t, map[string]string{
		"package.json":  "{\"name\":\"app\"}",
		"tsconfig.json": "{\"compilerOptions\":{\"paths\":{\"@entry\":[\"./src/boot\"]}}}",
		"app/main.ts":   "import { boot } from '@entry';\nexport const run = () => boot();\n",
		"src/boot.ts":   "export function boot() { return 1; }\n",
	})
	if !hasEdge(out.Graph, "/modules/app", "/modules/src", graph.EdgeImports) {
		t.Errorf("exact alias edge missing; edges = %v", edgeTargets(out.Graph, "/modules/app"))
	}
}

// A hand-written `extends` cycle is a configuration error, not something to handle. But the
// inheritance walk would not terminate on one, and hanging while analysing someone else's
// repository is the worst available way to report their mistake. The assertion is that this
// test finishes at all.
func TestTypeScriptExtendsCycleTerminates(t *testing.T) {
	out := build(t, map[string]string{
		"package.json":       "{\"name\":\"app\"}",
		"tsconfig.json":      "{\"extends\":\"./tsconfig.base.json\"}",
		"tsconfig.base.json": "{\"extends\":\"./tsconfig.json\"}",
		"src/index.ts":       "import { x } from '@app/x';\nexport const run = () => x();\n",
	})
	// No aliases are reachable through the cycle, so the specifier stays unresolved. It
	// must be reported, not invented into a dependency named `@app`.
	for _, n := range out.Graph.NodesOfKind(graph.KindExternal) {
		if n.Title == "@app" {
			t.Error("invented an external node from an unresolvable alias")
		}
	}
}

// pkgOf returns the `packages/<name>` prefix of a path, or "" if it has none. Used to check
// that no edge crosses between two packages that declared the same alias.
func pkgOf(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) >= 2 && parts[0] == "packages" {
		return parts[1]
	}
	return ""
}

// edgeTargets lists a node's outgoing edge destinations, for failure messages.
func edgeTargets(g *graph.Graph, from string) []string {
	var out []string
	for _, e := range g.EdgesFrom(from) {
		out = append(out, e.To)
	}
	return out
}

// allEdges lists every edge of a kind in the graph.
func allEdges(g *graph.Graph, kind graph.EdgeKind) []graph.Edge {
	var out []graph.Edge
	for _, n := range g.Nodes() {
		for _, e := range g.EdgesFrom(n.ID) {
			if e.Kind == kind {
				out = append(out, e)
			}
		}
	}
	return out
}

func TestRustCrateAndExternImports(t *testing.T) {
	out := build(t, map[string]string{
		"Cargo.toml":       "[package]\nname = \"app\"\nversion = \"0.1.0\"\n\n[dependencies]\nserde = \"1\"\nserde-json = \"1\"\n",
		"src/main.rs":      "use crate::store::get;\nuse serde::Serialize;\nuse serde_json::Value;\nuse std::collections::HashMap;\n\nfn main() { let _: HashMap<String, Value> = get(); }\n",
		"src/store/mod.rs": "pub fn get() -> std::collections::HashMap<String, serde_json::Value> { Default::default() }\n",
	})
	g := out.Graph

	if !hasEdge(g, "/modules/src", "/modules/store", graph.EdgeImports) {
		t.Error("crate:: path must resolve within the crate")
	}
	if !hasEdge(g, "/modules/src", "/references/crates-io-serde", graph.EdgeImports) {
		t.Error("serde edge missing")
	}
	// `serde-json` in the manifest, `serde_json` in every use path. The underscore
	// and dash spellings are the same crate.
	if !hasEdge(g, "/modules/src", "/references/crates-io-serde-json", graph.EdgeImports) {
		var got []string
		for _, e := range g.EdgesFrom("/modules/src") {
			got = append(got, e.To)
		}
		t.Errorf("src edges = %v, want one to serde-json", got)
	}
	if _, bad := out.Unresolved["rust std::collections"]; bad {
		t.Error("std is not a dependency")
	}
}

// TestRustSuperIsTheEnclosingModuleNotTheParentDirectory pins the one Rust path where the
// module tree and the directory tree come apart.
//
// `use super::*` inside an inline `#[cfg(test)] mod tests` is by a wide margin the commonest
// `super` in the language, and the parent module there is the enclosing *file*. Resolving it
// to the parent directory walks out of the crate: for `src/lib.rs` it lands on the crate root,
// which holds `Cargo.toml` and no source, so the import reached nothing. It was silent because
// the resolver was right that the import is first-party and right to invent no external crate
// for it — the two facts that between them meant nothing recorded the gap.
//
// The negative half is what stops the fix from being "always use the file's own directory":
// `mod.rs` is the one spelling whose module is its directory, so a `super` there does mean the
// directory above, and a submodule in its own subdirectory is the layout where the two answers
// differ.
func TestRustSuperIsTheEnclosingModuleNotTheParentDirectory(t *testing.T) {
	out := build(t, map[string]string{
		"Cargo.toml": "[package]\nname = \"app\"\nversion = \"0.1.0\"\n",
		// The inline test module: `super` is this file.
		"src/lib.rs": "pub mod store;\n\npub fn top() {}\n\n#[cfg(test)]\nmod tests {\n    use super::*;\n\n    #[test]\n    fn works() { top(); }\n}\n",
		// A submodule one directory down: `super` is the crate root module, `src`.
		"src/store/mod.rs": "use super::top;\n\npub fn get() { top(); }\n",
	})

	// Nothing may be reported as unlinked: both spellings resolve, one to a self-edge the
	// graph drops and one to a real edge. This is the assertion the defect failed.
	if len(out.Unlinked) != 0 {
		t.Errorf("unlinked = %v, want empty. A `super` that walked out of the crate reached no "+
			"module, and the import drew no edge with nothing saying so", out.Unlinked)
	}
	if len(out.Unresolved) != 0 {
		t.Errorf("unresolved = %v, want empty; `super` is never an external crate", out.Unresolved)
	}
	// The positive: the submodule's `super` is the parent module and that edge is real.
	if !hasEdge(out.Graph, "/modules/store", "/modules/src", graph.EdgeImports) {
		t.Errorf("no imports edge store -> src. `use super::top` in src/store/mod.rs names the "+
			"crate root module: a mod.rs *is* its directory's module, so its parent is the "+
			"directory above. Edges from store: %v", edgeTargets(out.Graph, "/modules/store"))
	}
	// And the negative that a directory-blind fix would break: `src` must not import `store`
	// on the strength of the inline `use super::*`, which points the other way entirely.
	if hasEdge(out.Graph, "/modules/src", "/modules/store", graph.EdgeImports) {
		t.Error("imports edge src -> store. The only specifier in src/lib.rs is `super::*` in " +
			"its own test module, which means src itself — an edge to the submodule is an " +
			"invented dependency in the wrong direction")
	}
}

func TestServiceMergesAcrossFiles(t *testing.T) {
	out := build(t, map[string]string{
		"compose.yaml":         "services:\n  api:\n    build: ./services/api\n    ports:\n      - \"8080:8080\"\n    depends_on:\n      - db\n  db:\n    image: docker.io/library/postgres:17\n",
		"deploy/api.yaml":      "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n  namespace: prod\nspec:\n  replicas: 3\n",
		"services/api/main.go": "package main\n\nfunc main() {}\n",
	})
	g := out.Graph

	api := node(t, g, "/services/api")
	// One node from two files: a compose file and a Kubernetes manifest describing
	// `api` describe the same thing, and the union is the whole picture.
	if len(api.Files) != 2 {
		t.Errorf("api files = %v, want compose and the manifest", api.Files)
	}
	if api.Attrs["ports"] != "8080:8080" {
		t.Errorf("ports = %q", api.Attrs["ports"])
	}
	if api.Attrs["workload"] != "Deployment" {
		t.Errorf("workload = %q", api.Attrs["workload"])
	}
	if api.Attrs["namespace"] != "prod" {
		t.Errorf("namespace = %q", api.Attrs["namespace"])
	}

	// depends_on is startup ordering stated by a human; no import can supply it.
	if !hasEdge(g, "/services/api", "/services/db", graph.EdgeDeploys) {
		t.Error("depends_on edge missing")
	}
	// A build context is the one link from a deployment back to the code it runs.
	if !hasEdge(g, "/services/api", "/modules/api", graph.EdgeDeploys) {
		var got []string
		for _, e := range g.EdgesFrom("/services/api") {
			got = append(got, string(e.Kind)+"->"+e.To)
		}
		t.Errorf("api edges = %v, want one to the module it builds from", got)
	}
}

func TestDataStoreNodePerTable(t *testing.T) {
	out := build(t, map[string]string{
		"migrations/0001_init.sql":  "CREATE TABLE things (id int);\nCREATE TABLE users (id int);\n",
		"migrations/0002_alter.sql": "ALTER TABLE things ADD COLUMN name text;\n",
		"migrations/0003_drop.sql":  "ALTER TABLE things DROP COLUMN name;\n",
	})
	g := out.Graph

	things := node(t, g, "/data/things")
	// The table is the durable concept and the migration is an event: grouping by
	// table is what makes the history readable.
	if things.Attrs["migrations"] != "3" {
		t.Errorf("migrations = %q, want 3", things.Attrs["migrations"])
	}
	if !containsStr(things.Tags, "destructive-history") {
		t.Errorf("things tags = %v; a dropped column must be flagged", things.Tags)
	}
	users := node(t, g, "/data/users")
	if containsStr(users.Tags, "destructive-history") {
		t.Error("users was only created; flagging it would misreport the risk")
	}
}

func TestInterfaceNodePerContractFile(t *testing.T) {
	out := build(t, map[string]string{
		"api/things.proto": "syntax = \"proto3\";\npackage things.v1;\n\nservice Things {\n  rpc Get(GetRequest) returns (Thing);\n}\n\nmessage GetRequest { string id = 1; }\nmessage Thing { string id = 1; }\n",
	})
	g := out.Graph
	ifaces := g.NodesOfKind(graph.KindInterface)
	// One node per file, not per declaration: twelve pages for twelve messages would
	// bury the service definition among its parameter types.
	if len(ifaces) != 1 {
		t.Fatalf("interfaces = %d, want 1", len(ifaces))
	}
	n := ifaces[0]
	if n.Attrs["service"] != "Things" {
		t.Errorf("service = %q", n.Attrs["service"])
	}
	if !strings.Contains(n.Attrs["message"], "GetRequest") || !strings.Contains(n.Attrs["message"], "Thing") {
		t.Errorf("messages = %q", n.Attrs["message"])
	}
	if n.Attrs["package"] != "things.v1" {
		t.Errorf("package = %q", n.Attrs["package"])
	}
}

// A Kubernetes CRD is recorded as a contract by its reader, but it belongs to the
// service page. An interface node for it would put a deployment detail in the API index.
func TestKubernetesCRDIsNotAnInterface(t *testing.T) {
	out := build(t, map[string]string{
		"deploy/crd.yaml": "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: things.example.com\nspec:\n  group: example.com\n  names:\n    kind: Thing\n",
	})
	if n := len(out.Graph.NodesOfKind(graph.KindInterface)); n != 0 {
		t.Errorf("interfaces = %d, want 0", n)
	}
}

// An AGENTS.md's placement is its scope, which every tool that reads one honors. That
// makes the edge extracted: the file's location is the statement.
func TestAgentRulesScopeByPlacement(t *testing.T) {
	out := build(t, map[string]string{
		"services/api/AGENTS.md": "# API rules\n\nNever log request bodies.\n",
		"services/api/main.go":   "package main\n\nfunc main() {}\n",
		"services/web/main.go":   "package main\n\nfunc main() {}\n",
	})
	g := out.Graph
	doc := ""
	for _, n := range g.NodesOfKind(graph.KindDocument) {
		doc = n.ID
	}
	if doc == "" {
		t.Fatal("no document node for AGENTS.md")
	}
	if !hasEdge(g, doc, "/modules/api", graph.EdgeDocuments) {
		t.Error("rules must govern the directory they sit in")
	}
	if hasEdge(g, doc, "/modules/web", graph.EdgeDocuments) {
		t.Error("rules must not reach a sibling directory they say nothing about")
	}
}

// An ADR's subject is in its prose, not its path. Linking it to a module would require
// reading what it says, which is the semantic pass's job.
func TestADRGetsNoModuleEdge(t *testing.T) {
	out := build(t, map[string]string{
		"docs/adr/0007-tokens-are-opaque.md": "# 0007: Tokens are opaque\n\n## Status\n\nAccepted\n\n## Decision\n\nTokens carry no claims.\n",
		"internal/auth/auth.go":              "package auth\n\nfunc Check() {}\n",
	})
	g := out.Graph
	var adr *graph.Node
	for _, n := range g.NodesOfKind(graph.KindDocument) {
		if containsStr(n.Tags, string(manifest.KindADR)) {
			adr = n
		}
	}
	if adr == nil {
		t.Fatal("no ADR node")
	}
	if adr.Attrs["status"] != "Accepted" {
		t.Errorf("status = %q; a superseded ADR read as current is worse than an unread one", adr.Attrs["status"])
	}
	if len(g.EdgesFrom(adr.ID)) != 0 {
		t.Errorf("ADR edges = %v, want none", g.EdgesFrom(adr.ID))
	}
}

func TestCodeownersAttachesToModules(t *testing.T) {
	out := build(t, map[string]string{
		"CODEOWNERS":            "*.go @everyone\n/internal/auth/ @security-team\n",
		"internal/auth/auth.go": "package auth\n\nfunc Check() {}\n",
		"internal/api/api.go":   "package api\n\nfunc Serve() {}\n",
	})
	g := out.Graph
	if got := node(t, g, "/modules/auth").Attrs["owners"]; got != "@security-team" {
		t.Errorf("auth owners = %q", got)
	}
	// `*.go` covers the whole repository and says nothing about who owns what.
	if got := node(t, g, "/modules/api").Attrs["owners"]; got != "" {
		t.Errorf("api owners = %q, want empty: a repo-wide glob is not ownership", got)
	}
}

// A manifest's declarations must reach the graph even when no source file resolves to
// them, or the supply chain is present in the bundle but disconnected from the code.
func TestDeclaredDepsAreConnectedWithoutImports(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":  "module example.com/app\n\ngo 1.26\n\nrequire (\n\texample.com/used v1.0.0\n\texample.com/unused v2.0.0\n)\n",
		"main.go": "package main\n\nimport \"example.com/used\"\n\nfunc main() { used.X() }\n",
	})
	g := out.Graph
	for _, name := range []string{"/references/go-example-com-used", "/references/go-example-com-unused"} {
		if !hasEdge(g, "/modules/root", name, graph.EdgeConfigures) {
			t.Errorf("no configures edge to %s", name)
		}
	}
	// The imports edge exists only for the one actually imported. That pair —
	// declared versus used — is what finds both unused and undeclared dependencies.
	if !hasEdge(g, "/modules/root", "/references/go-example-com-used", graph.EdgeImports) {
		t.Error("the used dependency must also carry an imports edge")
	}
	if hasEdge(g, "/modules/root", "/references/go-example-com-unused", graph.EdgeImports) {
		t.Error("an unimported dependency must not carry an imports edge")
	}
	if n := len(g.Orphans()); n != 0 {
		t.Errorf("orphans = %d, want none", n)
	}
}

// A declaration whose target is a directory in this repository is not an external
// dependency, and the composition it states is worth an edge instead.
//
// Both halves are asserted together because either alone is satisfiable by a broken
// reading. Dropping the reference page without drawing the edge loses the only statement
// anywhere in the repository of which of its own directories the infrastructure is
// composed from; drawing an edge while still emitting the page claims the repository
// pulls its own code in from outside. The registry module beside it is the negative
// boundary: `terraform-aws-modules/vpc/aws` and `./modules/queue` are the same
// slash-separated shape, so a reader guessing from the shape rather than from the `./`
// gets exactly one of these two rows wrong whichever way it guesses.
func TestLocalDeclarationIsAnEdgeAndNotAReferencePage(t *testing.T) {
	out := build(t, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"infra/main.tf": "" +
			"module \"queue\" {\n  source = \"./modules/queue\"\n}\n\n" +
			"module \"vpc\" {\n  source  = \"terraform-aws-modules/vpc/aws\"\n  version = \"5.5.1\"\n}\n",
		// Source in the target directory, because a module node is what the edge needs
		// to land on and a directory of `.tf` files alone produces none.
		"infra/modules/queue/queue.go": "package queue\n\nfunc Send() {}\n",
	})
	g := out.Graph
	if g.Node("/references/terraform-queue") != nil {
		t.Error("a directory in this repository must not get an external dependency page")
	}
	if g.Node("/references/terraform-vpc") == nil {
		t.Error("a registry module is genuinely external and must keep its page")
	}
	// From the nearest module above the declaring file, which for a manifest in a
	// source-free directory is the same rule every other manifest gets.
	if !hasEdge(g, "/modules/root", "/modules/queue", graph.EdgeConfigures) {
		var got []string
		for _, e := range g.Edges() {
			got = append(got, e.From+" -"+string(e.Kind)+"-> "+e.To)
		}
		t.Errorf("edges = %v, want infra configures queue", got)
	}
}

// A manifest at the repository root sits in a directory that often holds no source of
// its own; the declaration still belongs to the nearest module above it.
func TestManifestInSourcelessDirectory(t *testing.T) {
	out := build(t, map[string]string{
		"app.go":        "package app\n\nfunc X() {}\n",
		"config/go.mod": "module example.com/cfg\n\ngo 1.26\n\nrequire example.com/dep v1.0.0\n",
	})
	if !hasEdge(out.Graph, "/modules/root", "/references/go-example-com-dep", graph.EdgeConfigures) {
		t.Error("a manifest in a source-free directory must attach to the nearest module above")
	}
}

// A JVM import resolves against the `package` declarations the repository's own source
// makes, which is the one resolution map here built from extracted facts rather than from
// a manifest. Nothing in a JVM tree states the mapping otherwise: signpost reads no
// pom.xml or build.gradle yet, and the directory does not say it — the same file compiles
// from src/main/java, from src/, or from any directory a Gradle source set names.
func TestJVMImportsResolveByDeclaredPackage(t *testing.T) {
	out := build(t, map[string]string{
		// Deliberately not a Maven layout, so a path-derived package name would be
		// `code.api` and would resolve nothing an import writes.
		"code/api/Service.java": "package com.example.api;\n\n" +
			"import com.example.store.Repository;\n\n" +
			"public class Service {\n    public void go() {}\n}\n",
		"code/store/Repository.java": "package com.example.store;\n\n" +
			"public class Repository {\n    public void find() {}\n}\n",
		"code/app/Main.kt": "package com.example.app\n\n" +
			"import com.example.api.Service\n" +
			"import com.example.store.internal.Cache\n\n" +
			"fun main() {\n    Service().go()\n}\n",
		// A subpackage of one already declared. Its own directory, its own node, and the
		// longest declared name has to win: matched shortest-first, an import of
		// `com.example.store.internal` lands on com.example.store with `.internal` read as
		// a class name, and the edge points at the parent of the package that was asked for.
		"code/store/internal/Cache.java": "package com.example.store.internal;\n\n" +
			"public class Cache {\n    public void clear() {}\n}\n",
	})
	g := out.Graph
	if !hasEdge(g, "/modules/api", "/modules/store", graph.EdgeImports) {
		var got []string
		for _, e := range g.Edges() {
			got = append(got, e.From+" -"+string(e.Kind)+"-> "+e.To)
		}
		t.Errorf("edges = %v, want api imports store", got)
	}
	// Across languages, because the two share the namespace: Kotlin importing a Java
	// package is the ordinary case in a mixed repository and the resolver must not care
	// which extractor produced either side.
	if !hasEdge(g, "/modules/app", "/modules/api", graph.EdgeImports) {
		t.Error("a Kotlin file importing a Java package must resolve; the JVM namespace is shared")
	}
	if !hasEdge(g, "/modules/app", "/modules/internal", graph.EdgeImports) {
		var got []string
		for _, e := range g.EdgesFrom("/modules/app") {
			got = append(got, e.To)
		}
		t.Errorf("app imports %v; a subpackage must win over the package containing it", got)
	}
	if hasEdge(g, "/modules/app", "/modules/store", graph.EdgeImports) {
		t.Error("the import named com.example.store.internal and the edge landed on its parent")
	}
}

// The negative boundary, and the one that decides whether the test above is measuring
// anything: a resolver that matched on a prefix would draw all of these too.
//
// `com.example.apiv2` shares every character of `com.example.api` and is a different
// package. An import naming a class inside a package resolves to that package, since the
// package is what a node exists for. And `org.springframework.*` is a real dependency
// signpost has no manifest for — it must land in the unresolved count rather than become
// an invented Maven node, because a bundle's claim about dependencies is that a manifest
// said so.
func TestJVMResolutionDoesNotMatchOnPrefixOrInventDependencies(t *testing.T) {
	out := build(t, map[string]string{
		"code/api/Service.java": "package com.example.api;\n\n" +
			"import com.example.apiv2.Legacy;\n" +
			"import com.example.store.Repository;\n" +
			"import org.springframework.stereotype.Component;\n" +
			"import java.util.List;\n" +
			"import kotlin.collections.CollectionsKt;\n" +
			// The two prefixes that are half runtime and half not, and neither is
			// decidable from its first segment. `javax` was split between the platform
			// and Java EE in 1999 and the split is historical: javax.crypto ships with
			// the JDK and javax.servlet is a Maven artifact with its own advisories.
			// `kotlinx` opens with the six characters of `kotlin` and holds coroutines,
			// serialization and datetime — separately versioned, separately patched.
			// Matched on a segment prefix, both halves read as the toolchain, which is
			// the one misclassification that makes a dependency vanish from the coverage
			// report rather than appear in it.
			"import javax.crypto.Cipher;\n" +
			"import javax.servlet.http.HttpServletRequest;\n" +
			"import kotlinx.coroutines.Job;\n\n" +
			"public class Service {\n    public void go() {}\n}\n",
		"code/store/Repository.java": "package com.example.store;\n\n" +
			"public class Repository {\n    public void find() {}\n}\n",
		// A manifest in another ecosystem declaring a name a JVM import happens to share.
		// depOrEmpty falls back across ecosystems when a specifier matches nothing in its
		// own — deliberately, for the mixed-language case — so a JVM resolver reaching that
		// lookup at all would draw an edge from Java code to an npm package. There is no
		// JVM manifest reader yet, which makes not reaching it the whole of the rule.
		"web/package.json": `{"name":"web","dependencies":{"org.springframework.stereotype":"1.0.0"}}`,
		"web/index.ts":     "export const x = 1;\n",
	})
	g := out.Graph
	// The one real internal import lands; the near-miss must not.
	if !hasEdge(g, "/modules/api", "/modules/store", graph.EdgeImports) {
		t.Error("com.example.store is declared in this repository and must resolve")
	}
	for _, e := range g.EdgesFrom("/modules/api") {
		if strings.HasPrefix(e.To, "/references/") {
			t.Errorf("an external node %q was invented from an import; no JVM manifest was read", e.To)
		}
	}
	// apiv2 is declared by nobody here, so it is a gap and is counted as one rather than
	// silently absorbed into com.example.api.
	unresolved := map[string]bool{}
	for k := range out.Unresolved {
		unresolved[k] = true
	}
	for _, want := range []string{
		"java com.example.apiv2",
		"java org.springframework.stereotype",
		// Both halves of the two split prefixes, and each is only meaningful beside its
		// runtime twin below. A gap missing here is a dependency nobody is told to patch.
		"java javax.servlet.http",
		"java kotlinx.coroutines",
	} {
		if !unresolved[want] {
			t.Errorf("%q is not in the unresolved count %v; a gap the reader is not told about "+
				"is the one outcome the count exists to prevent", want, out.Unresolved)
		}
	}
	// And the platform's own packages are neither edges nor gaps: nobody patches the JDK
	// separately, and counting them would make every honest JVM repository look unread.
	// `javax.crypto` and `kotlin.collections` are one segment from the two counted above,
	// so this loop and that one fail in opposite directions on a single prefix match.
	for _, no := range []string{
		"java java.util",
		"java kotlin.collections",
		"java javax.crypto",
	} {
		if unresolved[no] {
			t.Errorf("%q was counted as a gap; it is the platform", no)
		}
	}
	// The exact count, per the corpus's negative-boundary rule: a resolver that reported
	// everything as unresolved would satisfy every positive assertion above.
	if len(out.Unresolved) != 4 {
		t.Errorf("Unresolved = %v, want exactly the four undeclared packages", out.Unresolved)
	}
}

// The standard JVM layout declares each package twice, and the JVM is the only language
// here where that matters, because it is the only one whose resolution map comes from
// extracted facts. Maven and Gradle put `com.example.api` in src/main/java/com/example/api
// *and* src/test/java/com/example/api, so an import of that package names two directories
// and only the production one is what another module means by it.
//
// The source set here is `integrationTest`, which is the whole reason this test can fail.
// Directory order was the tiebreaker and it looks sound — `src/main` sorts before `src/test`
// — but the source set holding tests is not always called `test`: Gradle's convention for
// the extra one is `integrationTest` and Android's is `androidTest`, and both sort *ahead*
// of `main`. So a repository with either resolved every import of a package to the copy
// under test: an edge into the tests instead of into the code, drawn with nothing recording
// that a choice between two candidates was made. Written with a source set named `test`,
// every assertion below passes on the broken ordering too.
func TestJVMImportPrefersTheProductionSourceSet(t *testing.T) {
	out := build(t, map[string]string{
		// A test beside the code it tests, which is the second half of the rule and is why
		// the flag means "*every* file declaring this package here is a test" rather than
		// "some file here is". It sorts ahead of Service.java, so this directory is first
		// seen as test-only and something has to clear the flag — and if nothing does, the
		// production directory ties with the integrationTest one and loses on `i` < `m`.
		// Not hypothetical: a src/main package holding one *Test.java is ordinary, and it
		// sends every import of that package into another source set entirely.
		"src/main/java/com/example/api/ApiTest.java": "package com.example.api;\n\n" +
			"public class ApiTest {\n    public void checks() {}\n}\n",
		"src/main/java/com/example/api/Service.java": "package com.example.api;\n\n" +
			"public class Service {\n    public void go() {}\n}\n",
		// The same package, a second source set. Registered rather than discarded: a
		// package declared *only* under a test source set is still this repository's own,
		// so an import of it is internal and unlinked rather than a missing dependency.
		"src/integrationTest/java/com/example/api/ServiceIT.java": "package com.example.api;\n\n" +
			"public class ServiceIT {\n    public void returnsRows() {}\n}\n",
		"src/main/kotlin/com/example/app/Main.kt": "package com.example.app\n\n" +
			"import com.example.api.Service\n\n" +
			"fun main() {\n    Service().go()\n}\n",
	})
	g := out.Graph
	// Found by path rather than by ID, because both api directories slug to `api` and the
	// suffix each gets is derived from its own key (ADR 0015) — asserting on it would be
	// asserting on the naming scheme rather than on resolution.
	byPath := map[string]string{}
	for _, n := range g.Nodes() {
		if n.Path != "" {
			byPath[n.Path] = n.ID
		}
	}
	mainAPI := byPath["src/main/java/com/example/api"]
	testAPI := byPath["src/integrationTest/java/com/example/api"]
	appMod := byPath["src/main/kotlin/com/example/app"]
	if mainAPI == "" || testAPI == "" || appMod == "" {
		t.Fatalf("missing a module node: main=%q test=%q app=%q — both source sets must "+
			"become their own module, or this test asserts nothing", mainAPI, testAPI, appMod)
	}
	if !hasEdge(g, appMod, mainAPI, graph.EdgeImports) {
		var got []string
		for _, e := range g.EdgesFrom(appMod) {
			got = append(got, e.To)
		}
		t.Errorf("app imports %v, want the production copy of com.example.api", got)
	}
	if hasEdge(g, appMod, testAPI, graph.EdgeImports) {
		t.Error("the import resolved to the package under the integrationTest source set. " +
			"Both directories declare com.example.api and `integrationTest` sorts ahead of " +
			"`main`, so the tiebreaker cannot be the directory")
	}
}

// A JVM test's subject is the one thing its import list does not name, which is why
// addTestEdges reads the declaration for these two languages instead of the imports.
//
// Same-package access needs no import: a test of a class beside it imports every
// collaborator and never the class itself. Maven and Gradle then put the test in a separate
// source set, so the directory differs while the `package` declaration says plainly what is
// under test. Reading imports finds `com.example.store` and reports the store as tested by
// a test that never touches it — a confidently-wrong edge, which is worse than no edge.
func TestJVMTestedByComesFromTheDeclaredPackage(t *testing.T) {
	out := build(t, map[string]string{
		"src/main/java/com/example/api/Service.java": "package com.example.api;\n\n" +
			"import com.example.store.Repository;\n\n" +
			"public class Service {\n    public void go() {}\n}\n",
		"src/main/java/com/example/store/Repository.java": "package com.example.store;\n\n" +
			"public class Repository {\n    public void find() {}\n}\n",
		// Declares the package it tests, imports only a collaborator. Neither existing rule
		// in addTestEdges reaches it: placement is per-directory and this is a different
		// directory, and the imports name the store.
		"src/integrationTest/java/com/example/api/ServiceIT.java": "package com.example.api;\n\n" +
			"import com.example.store.Repository;\n\n" +
			"public class ServiceIT {\n    public void returnsRows() {}\n}\n",
	})
	g := out.Graph
	byPath := map[string]string{}
	for _, n := range g.Nodes() {
		if n.Path != "" {
			byPath[n.Path] = n.ID
		}
	}
	mainAPI := byPath["src/main/java/com/example/api"]
	testAPI := byPath["src/integrationTest/java/com/example/api"]
	store := byPath["src/main/java/com/example/store"]
	if mainAPI == "" || testAPI == "" || store == "" {
		t.Fatalf("missing a module node: api=%q test=%q store=%q", mainAPI, testAPI, store)
	}
	if !hasEdge(g, mainAPI, testAPI, graph.EdgeTestedBy) {
		var got []string
		for _, e := range g.Edges() {
			if e.Kind == graph.EdgeTestedBy {
				got = append(got, e.From+" -> "+e.To)
			}
		}
		t.Errorf("tested_by = %v, want the package ServiceIT declares", got)
	}
	if hasEdge(g, store, testAPI, graph.EdgeTestedBy) {
		t.Error("tested_by points from com.example.store, which is what ServiceIT imports " +
			"rather than what it declares — every collaborator reported as under test")
	}
}

// An #include names a *file*, which makes C the only language here whose resolution is
// path-based rather than name-based, and the delimiter is the search rule. A quoted
// include searches the including file's own directory first; an angled one searches the
// include path, which for a project's own public headers is `include/`.
func TestCIncludesResolveByPathAndDelimiter(t *testing.T) {
	out := build(t, map[string]string{
		"src/app.c": "#include \"buffer.h\"\n" +
			"#include \"../include/log.h\"\n" +
			"#include <util/hash.h>\n" +
			"#include <stdio.h>\n\n" +
			"int main(void) { return 0; }\n",
		"src/buffer.h":        "int buffer_grow(int n);\n",
		"include/log.h":       "void log_error(const char *m);\n",
		"include/util/hash.h": "unsigned hash(const char *s);\n",
	})
	g := out.Graph
	// Quoted, resolved against the including file's own directory.
	if !hasEdge(g, "/modules/src", "/modules/src", graph.EdgeImports) &&
		g.Node("/modules/src") == nil {
		t.Fatal("no module for src/")
	}
	// Quoted with a relative prefix, reaching another directory.
	if !hasEdge(g, "/modules/src", "/modules/include", graph.EdgeImports) {
		var got []string
		for _, e := range g.EdgesFrom("/modules/src") {
			got = append(got, e.To)
		}
		t.Errorf("src imports %v; a quoted relative include must resolve", got)
	}
	// Angled, resolved through the `include/` root a C project conventionally declares.
	if !hasEdge(g, "/modules/src", "/modules/util", graph.EdgeImports) {
		t.Error("an angled include of a header under include/ is the project's own public " +
			"header and must resolve")
	}
	// And no external node was invented: there is no manifest declaring C dependencies,
	// so a node for one would publish a dependency the repository never named.
	for _, e := range g.EdgesFrom("/modules/src") {
		if strings.HasPrefix(e.To, "/references/") {
			t.Errorf("an external node %q was invented from an include", e.To)
		}
	}
}

// The negative boundary for C resolution, and the one that decides whether the test above
// measures anything: a resolver that accepted a directory, or that ignored the delimiter,
// would draw an edge for every line here.
func TestCResolutionRespectsTheDelimiterAndRequiresAFile(t *testing.T) {
	out := build(t, map[string]string{
		"src/app.c": "#include \"missing.h\"\n" +
			// A directory named exactly what an angled include names. The C++ standard
			// library has no extensions, so `<memory>` is stdlib — and a resolver that
			// accepted a directory target would point it at this repository's own
			// `memory/` instead, which is an edge to code that has nothing to do with it.
			"#include <memory>\n" +
			// A real dependency signpost has no manifest for. It must land in the
			// unresolved count rather than become an invented node.
			"#include <gtest/gtest.h>\n" +
			// The platform, which is neither an edge nor a gap: nobody patches libc
			// separately, and counting it would make every honest C repository look unread.
			"#include <stdio.h>\n" +
			"#include <sys/socket.h>\n\n" +
			"int main(void) { return 0; }\n",
		"src/memory/pool.c": "int pool_take(void) { return 0; }\n",
		"src/memory/pool.h": "int pool_take(void);\n",
	})
	g := out.Graph
	if hasEdge(g, "/modules/src", "/modules/memory", graph.EdgeImports) {
		t.Error("`#include <memory>` is the C++ standard library and resolved to a " +
			"directory of the same name; an include names a file")
	}
	unresolved := map[string]bool{}
	for k := range out.Unresolved {
		unresolved[k] = true
	}
	for _, want := range []string{
		`c "missing.h"`,
		"c <gtest/gtest.h>",
	} {
		if !unresolved[want] {
			t.Errorf("%q is not in the unresolved count %v; a gap the reader is not told "+
				"about is the one outcome the count exists to prevent", want, out.Unresolved)
		}
	}
	for _, no := range []string{
		"c <stdio.h>", "c <sys/socket.h>",
		// `<memory>` is C++'s standard library by shape — extensionless, angled — so it
		// is not a gap either, and it must not become one just because this repository
		// happens to have a directory of the same name.
		"c <memory>",
	} {
		if unresolved[no] {
			t.Errorf("%q was counted as a gap; it is the platform", no)
		}
	}
	// The exact count, per the corpus's negative-boundary rule: a resolver that reported
	// everything as unresolved would satisfy every positive assertion above.
	if len(out.Unresolved) != 2 {
		t.Errorf("Unresolved = %v, want exactly the two includes naming nothing here",
			out.Unresolved)
	}
}

// The stdlib rule for C has a shape no other language's does, and both halves need
// pinning. C++'s standard library headers have no extension, so an extensionless angled
// include *is* the standard library by construction and needs no list that can go stale.
// C's own headers do end in `.h` and are indistinguishable by shape from a project's, so
// those need the list — and a quoted include is never the system library, because the
// quotes say "look here first".
func TestCSystemHeaderRecognition(t *testing.T) {
	stdlib := []string{
		// C++, by shape.
		"<memory>", "<vector>", "<unordered_map>", "<coroutine>",
		// C, from the list.
		"<stdio.h>", "<stdint.h>", "<stdbool.h>", "<threads.h>",
		// POSIX and Windows, which arrive with the platform.
		"<sys/socket.h>", "<pthread.h>", "<unistd.h>", "<windows.h>",
		// Apple's frameworks, which arrive with the SDK.
		"<Foundation/Foundation.h>", "<UIKit/UIKit.h>",
	}
	for _, raw := range stdlib {
		if !isStdlib(discover.LangC, raw) {
			t.Errorf("isStdlib(%q) = false; this arrives with the toolchain and nobody "+
				"patches it separately", raw)
		}
	}
	notStdlib := []string{
		// A chosen dependency with its own advisories, one segment from a framework.
		"<gtest/gtest.h>", "<openssl/ssl.h>", "<curl/curl.h>", "<zlib.h>",
		// The quoted form of a header that is on the list. The quotes are the whole
		// difference: this names a file in the repository that shadows it.
		`"stdio.h"`, `"vector"`,
		// A project's own public header, included with angle brackets because it is on
		// the include path — which is ordinary, and is why the extension matters.
		"<mylib/mylib.h>", "<buffer.h>",
	}
	for _, raw := range notStdlib {
		if isStdlib(discover.LangC, raw) {
			t.Errorf("isStdlib(%q) = true; this is a dependency somebody chose and has "+
				"to patch, and marking it as the platform is how it vanishes from the "+
				"coverage report", raw)
		}
	}
}

// A `.h` is labelled C because its name cannot say more, and that placeholder must not
// decide the directory's language. An Objective-C directory holds a `.h` for every `.m`,
// so counting the header gives a 1–1 tie that alphabetical order resolves to "c" — and
// Objective-C then appears nowhere in the bundle, in a repository that is written in it.
// The same asymmetry hits C++: `session.hpp` is unambiguous, `buffer.h` is not.
func TestHeaderLabelDoesNotDecideTheModuleLanguage(t *testing.T) {
	cases := []struct {
		name  string
		facts []extract.Facts
		want  string
	}{{
		name: "objc header does not outvote its implementation",
		facts: []extract.Facts{
			{Path: "objc/Sources/Reader.h", Lang: discover.LangC},
			{Path: "objc/Sources/Reader.m", Lang: discover.LangObjC},
		},
		want: "objc",
	}, {
		// Two headers to one implementation, so the header count wins outright rather
		// than by a tie. The implementation is still the only file that names a dialect.
		name: "outnumbered implementation still names the language",
		facts: []extract.Facts{
			{Path: "cpp/inc/a.h", Lang: discover.LangC},
			{Path: "cpp/inc/b.h", Lang: discover.LangC},
			{Path: "cpp/inc/a.cc", Lang: discover.LangCpp},
		},
		want: "cpp",
	}, {
		// A header-only directory has nothing else to go on, so the placeholder is the
		// best available answer and is used. Dropping headers unconditionally would
		// leave every public-header directory in a C project with no language at all.
		name: "header-only directory keeps the placeholder",
		facts: []extract.Facts{
			{Path: "c/include/corpus/buffer.h", Lang: discover.LangC},
		},
		want: "c",
	}, {
		// `.hpp` carries its dialect in its name, so it is not a placeholder and votes
		// like any other file.
		name: "unambiguous header extension votes normally",
		facts: []extract.Facts{
			{Path: "cpp/include/session.hpp", Lang: discover.LangCpp},
		},
		want: "cpp",
	}, {
		// A real C directory: the .c files answer, and they answer "c".
		name: "c implementation answers c",
		facts: []extract.Facts{
			{Path: "c/src/buffer.h", Lang: discover.LangC},
			{Path: "c/src/buffer.c", Lang: discover.LangC},
		},
		want: "c",
	}}
	for _, tc := range cases {
		if got := moduleLang(tc.facts); got != tc.want {
			t.Errorf("%s: moduleLang = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTestedByEdge(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                "module example.com/app\n\ngo 1.26\n",
		"internal/auth/auth.go": "package auth\n\nfunc Check() bool { return true }\n",
		"tests/auth_test.go":    "package tests\n\nimport (\n\t\"testing\"\n\n\t\"example.com/app/internal/auth\"\n)\n\nfunc TestCheck(t *testing.T) { _ = auth.Check() }\n",
	})
	g := out.Graph
	// The edge points from the code to its tests, matching the kind's name.
	if !hasEdge(g, "/modules/auth", "/modules/tests", graph.EdgeTestedBy) {
		var got []string
		for _, e := range g.Edges() {
			got = append(got, e.From+" -"+string(e.Kind)+"-> "+e.To)
		}
		t.Errorf("edges = %v, want auth tested_by tests", got)
	}
}

// Tests beside the code make the edge a self-edge, which the graph drops. Correct, and
// asserted so the absence is never read as a resolution failure.
func TestTestsBesideCodeDrawNoEdge(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                     "module example.com/app\n\ngo 1.26\n",
		"internal/auth/auth.go":      "package auth\n\nfunc Check() bool { return true }\n",
		"internal/auth/auth_test.go": "package auth\n\nimport \"testing\"\n\nfunc TestCheck(t *testing.T) { _ = Check() }\n",
	})
	for _, e := range out.Graph.Edges() {
		if e.Kind == graph.EdgeTestedBy {
			t.Errorf("unexpected tested_by edge %+v", e)
		}
	}
	// The test file still belongs to its module's file list — it is part of the
	// directory, and omitting it would understate what is there.
	if got := len(node(t, out.Graph, "/modules/auth").Files); got != 2 {
		t.Errorf("auth files = %d, want both", got)
	}
}

// A test beside its subject that imports another module for fixtures must not claim
// that module is tested by this one.
//
// This is the case that makes reading a co-located test's imports wrong rather than
// merely redundant: `auth_test.go` importing a `testutil` helper says nothing about
// what testutil is tested by, and the edge it would draw is confidently backwards.
// Placement already answers the question for a co-located test, so the imports are
// not consulted.
func TestBesideTestImportsDoNotClaimForeignSubjects(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                       "module example.com/app\n\ngo 1.26\n",
		"internal/auth/auth.go":        "package auth\n\nfunc Check() bool { return true }\n",
		"internal/auth/auth_test.go":   "package auth\n\nimport (\n\t\"testing\"\n\n\t\"example.com/app/internal/testutil\"\n)\n\nfunc TestCheck(t *testing.T) { testutil.Setup(); _ = Check() }\n",
		"internal/testutil/fixture.go": "package testutil\n\nfunc Setup() {}\n",
	})
	for _, e := range out.Graph.Edges() {
		if e.Kind == graph.EdgeTestedBy {
			t.Errorf("unexpected tested_by edge %+v", e)
		}
	}
	// The import itself is still recorded — the file does import testutil, and that
	// is a fact about coupling regardless of what it says about test subjects.
	if !hasEdge(out.Graph, "/modules/auth", "/modules/testutil", graph.EdgeImports) {
		t.Error("the import edge should still be drawn")
	}
}

// Every edge this package draws is extracted. Inference belongs to the semantic pass,
// and an inferred edge wearing extracted confidence would defeat the trust grading.
func TestEveryEdgeIsExtracted(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                "module example.com/app\n\ngo 1.26\n\nrequire example.com/d v1.0.0\n",
		"main.go":               "package main\n\nimport \"example.com/app/internal/auth\"\n\nfunc main() { auth.Check() }\n",
		"internal/auth/auth.go": "package auth\n\nfunc Check() {}\n",
		"compose.yaml":          "services:\n  api:\n    build: .\n",
		"CODEOWNERS":            "/internal/ @team\n",
		"AGENTS.md":             "# Rules\n\nDo not skip the gate.\n",
	})
	for _, e := range out.Graph.Edges() {
		if e.Conf != graph.Extracted {
			t.Errorf("edge %+v has confidence %q", e, e.Conf)
		}
		if e.Source == "" {
			t.Errorf("edge %+v has no provenance", e)
		}
	}
}

// A dangling edge is a bug in this package, not a normal outcome: every edge is drawn
// from a node ID this package assigned.
func TestNoDanglingEdges(t *testing.T) {
	out := build(t, map[string]string{
		"go.mod":                "module example.com/app\n\ngo 1.26\n",
		"main.go":               "package main\n\nimport \"example.com/app/internal/auth\"\n\nfunc main() { auth.Check() }\n",
		"internal/auth/auth.go": "package auth\n\nfunc Check() {}\n",
		"compose.yaml":          "services:\n  api:\n    depends_on:\n      - missing\n",
	})
	if out.DroppedEdges != 0 {
		t.Errorf("dropped %d edges; every edge should point at a node this package created", out.DroppedEdges)
	}
}

// Two directories that slug identically must not merge into one node: AddNode treats a
// repeated ID as the same node, so a collision would be a wrong graph, not an ugly one.
func TestIDCollisionsAreDisambiguated(t *testing.T) {
	out := build(t, map[string]string{
		"a/auth/x.go":    "package auth\n\nfunc A() {}\n",
		"b/auth/y.go":    "package auth\n\nfunc B() {}\n",
		"c/a-u-t-h/z.go": "package auth\n\nfunc C() {}\n",
	})
	mods := out.Graph.NodesOfKind(graph.KindModule)
	if len(mods) != 3 {
		var got []string
		for _, m := range mods {
			got = append(got, m.ID+" "+m.Path)
		}
		t.Fatalf("modules = %v, want three distinct nodes", got)
	}
	seen := map[string]bool{}
	for _, m := range mods {
		if seen[m.ID] {
			t.Errorf("duplicate ID %q", m.ID)
		}
		seen[m.ID] = true
	}
}

// moduleIDsByPath maps each module node's directory to the ID it was assigned.
func moduleIDsByPath(t *testing.T, files map[string]string) map[string]string {
	t.Helper()
	out := build(t, files)
	m := make(map[string]string)
	for _, n := range out.Graph.NodesOfKind(graph.KindModule) {
		m[n.Path] = n.ID
	}
	return m
}

// A directory that did not change keeps its page, whatever else the repository gains or
// loses. The ID is the page's filename and the concept path every other page links
// against, so a rename is not cosmetic — it rewrites unrelated pages across a committed
// bundle, and it does it in the diff of a commit that never touched that directory.
//
// The counter this replaced failed on exactly the edits below. Both are things that
// happen to a repository every week.
func TestAnUnchangedDirectoryKeepsItsIDWhenTheRepositoryChanges(t *testing.T) {
	base := map[string]string{
		"a/auth/x.go":    "package auth\n\nfunc A() {}\n",
		"b/auth/y.go":    "package auth\n\nfunc B() {}\n",
		"c/a-u-t-h/z.go": "package auth\n\nfunc C() {}\n",
	}
	with := func(edit func(map[string]string)) map[string]string {
		next := make(map[string]string, len(base)+1)
		for k, v := range base {
			next[k] = v
		}
		edit(next)
		return next
	}

	before := moduleIDsByPath(t, base)
	if before["b/auth"] == "" || before["b/auth"] == before["a/auth"] {
		t.Fatalf("fixture no longer collides, so this test proves nothing: %v", before)
	}

	// A fourth directory whose name collides with the same group, sorting ahead of every
	// existing member. This is the position that breaks a first-come rule as well as a
	// counter: the newcomer is seen first, so it takes the bare readable name off whoever
	// held it, and both that page and the newcomer's are new files in the diff.
	ahead := moduleIDsByPath(t, with(func(m map[string]string) {
		m["0/auth/n.go"] = "package auth\n\nfunc N() {}\n"
	}))
	for _, dir := range []string{"a/auth", "b/auth", "c/a-u-t-h"} {
		if ahead[dir] != before[dir] {
			t.Errorf("%s moved from %q to %q because a directory sorting ahead of it was "+
				"added; every page linking to %s is now rewritten too",
				dir, before[dir], ahead[dir], dir)
		}
	}

	// A suffixed member of the group deleted. Under the counter every member after the
	// deleted one shifted down — b/auth was auth-3 here and became auth-2, again
	// without changing. a/auth is left in place so the bare name does not change hands;
	// that case is its own test below.
	group := with(func(m map[string]string) {
		m["aa/auth/n.go"] = "package auth\n\nfunc N() {}\n"
	})
	withMiddle := moduleIDsByPath(t, group)
	delete(group, "aa/auth/n.go")
	if got := moduleIDsByPath(t, group)["b/auth"]; got != withMiddle["b/auth"] {
		t.Errorf("b/auth moved from %q to %q because a different directory in its "+
			"collision group was deleted", withMiddle["b/auth"], got)
	}

	// The negative boundary: not moving is worthless if nothing ever moves, which is
	// what a scheme that gave every module the same ID would also satisfy. A directory
	// that genuinely is new must still get a page of its own.
	after := moduleIDsByPath(t, with(func(m map[string]string) {
		m["d/auth/w.go"] = "package auth\n\nfunc D() {}\n"
	}))
	if after["d/auth"] == "" {
		t.Errorf("the new directory got no module node at all: %v", after)
	}
	for path, id := range after {
		if path != "d/auth" && id == after["d/auth"] {
			t.Errorf("new directory d/auth shares ID %q with %s, which merges two "+
				"directories into one page", id, path)
		}
	}
}

// The stability guarantee holds for every prefix, not only for modules — a service page and
// a data-store page are linked to by name exactly as a module page is.
//
// Worth its own test because each prefix is a separate reservation with its own list, and
// the lists are the part that rots: a pass that starts naming something new, or stops
// filtering something, silently drops back to order-dependent IDs for that prefix while
// every other test stays green. Services and data stores are also the two that fold by name
// before assigning, which makes them the ones where over-counting a reservation is possible.
func TestIDsAreStableForServicesAndDataStoresToo(t *testing.T) {
	// Each prefix gets a real slug collision — `web-ui` and `web_ui` are two distinct
	// services that slug alike, as `app_log` and `app-log` are two distinct tables — because
	// a reservation only does anything where names collide. Plus a second file declaring one
	// service, which is the fold.
	base := map[string]string{
		"compose.yaml": "services:\n  web-ui:\n    image: docker.io/library/nginx:1\n  web_ui:\n" +
			"    image: docker.io/library/nginx:1\n  api:\n    image: docker.io/library/nginx:1\n",
		"deploy/api.yaml": "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\n" +
			"  namespace: prod\nspec:\n  replicas: 2\n",
		"db/migrations/0001_a.sql": "CREATE TABLE app_log (id int);\n",
		"db/migrations/0002_b.sql": "CREATE TABLE \"app-log\" (id int);\n",
	}
	ids := func(files map[string]string) map[string]string {
		out := build(t, files)
		m := map[string]string{}
		for _, n := range out.Graph.Nodes() {
			switch n.Kind {
			case graph.KindService, graph.KindDataStore:
				m[string(n.Kind)+" "+n.Title] = n.ID
			}
		}
		return m
	}

	before := ids(base)
	// The fold: `api` is declared twice and must be one node with the bare name, not a
	// suffixed pair. A reservation that counted declarations rather than entries would
	// suffix it.
	if before["Service api"] != "/services/api" {
		t.Errorf("Service api = %q, want /services/api: two files declaring one service is "+
			"one node, so nothing collides with it", before["Service api"])
	}
	// The collisions have to be real for the edit below to test anything: if these ever
	// stop sharing a short name, the test passes while asserting nothing.
	if before["Service web-ui"] == "/services/web-ui" || before["Service web_ui"] == "/services/web-ui" {
		t.Fatalf("neither colliding service may hold the bare name, or the reservation is not "+
			"being applied to this prefix at all: %v", before)
	}
	if before["Data Store app_log"] == "/data/app-log" || before["Data Store app-log"] == "/data/app-log" {
		t.Fatalf("neither colliding table may hold the bare name: %v", before)
	}

	next := make(map[string]string, len(base)+2)
	for k, v := range base {
		next[k] = v
	}
	// A third member of each colliding group, spelled so it sorts ahead of the two already
	// there — the position that takes the bare name under a first-come rule and so the one
	// that moves an existing page.
	next["compose.yaml"] = "services:\n  WEB-UI:\n    image: docker.io/library/nginx:1\n" +
		"  web-ui:\n    image: docker.io/library/nginx:1\n  web_ui:\n" +
		"    image: docker.io/library/nginx:1\n  api:\n    image: docker.io/library/nginx:1\n"
	next["db/migrations/0000_c.sql"] = "CREATE TABLE APP_LOG (id int);\n"

	after := ids(next)
	if after["Service WEB-UI"] == "" || after["Data Store APP_LOG"] == "" {
		t.Fatalf("the added service and table got no nodes, so the edit under test did not "+
			"happen: %v", after)
	}
	for what, id := range before {
		if got := after[what]; got != id {
			t.Errorf("%s moved from %q to %q; nothing about it changed", what, id, got)
		}
	}
}

// The one residual, recorded rather than claimed away: a name is suffixed because more
// than one thing wants it, so when a collision group shrinks to a single member that
// member stops needing the suffix and its page moves to the bare name.
//
// Deliberate. The alternative is suffixing every name in the bundle whether it collides
// or not — `signpost` becoming `signpost-1f4ka9` in a repository with nothing else called
// that — which trades away the readability of every page in every repository to hold still
// in a case that needs a directory to be deleted. The churn is also bounded: it is the
// last member of that one group, not the bundle.
//
// Written as a test because it is the one shape where the guarantee above does not hold,
// and a reader finding a moved page deserves to find the reason in the suite rather than
// deduce it. Should this ever be revisited, this test is what changes.
func TestDeletingTheBareNameHolderMovesOnlyItsOwnGroup(t *testing.T) {
	before := moduleIDsByPath(t, map[string]string{
		"a/auth/x.go":  "package auth\n\nfunc A() {}\n",
		"b/auth/y.go":  "package auth\n\nfunc B() {}\n",
		"z/store/s.go": "package store\n\nfunc S() {}\n",
	})
	after := moduleIDsByPath(t, map[string]string{
		"b/auth/y.go":  "package auth\n\nfunc B() {}\n",
		"z/store/s.go": "package store\n\nfunc S() {}\n",
	})
	// Both were suffixed while they shared the name; with the other gone, b/auth is the
	// only thing called auth and takes the bare name. Documented, not desired.
	if before["b/auth"] == "/modules/auth" {
		t.Fatalf("setup: b/auth already held the bare name, so the shrink this test is "+
			"about cannot happen: %v", before)
	}
	if after["b/auth"] != "/modules/auth" {
		t.Errorf("b/auth = %q, want /modules/auth: with the only other auth gone it is "+
			"the sole holder of the short name", after["b/auth"])
	}
	// A directory outside the group is untouched, which is the part that must hold: the
	// churn is bounded by the collision, not spread across the bundle.
	if after["z/store"] != before["z/store"] {
		t.Errorf("z/store moved from %q to %q; it shares no name with the deleted "+
			"directory and must not move at all", before["z/store"], after["z/store"])
	}
}

// A name that slugs directly onto another entry's suffixed ID must still not merge with
// it. Nothing stops a directory from being called `api-1f4ka9`, and the suffix is only a
// discriminator, not a reserved namespace — so the collision check has to be on the
// finished ID rather than on the short name that produced it.
//
// Built through ids rather than through a repository, because the fixture needs to know
// the hash to name a directory after it.
func TestANameThatSlugsOntoASuffixedIDIsStillDistinct(t *testing.T) {
	x := newIDs()
	first := x.assign(prefixModule, "a/api", "api")
	second := x.assign(prefixModule, "b/api", "api")
	if first != prefixModule+"api" || second == first {
		t.Fatalf("setup: first=%q second=%q, want the bare name and a distinct suffixed one",
			first, second)
	}
	// A third directory whose own name is what the second was given.
	third := x.assign(prefixModule, "c/"+strings.TrimPrefix(second, prefixModule),
		strings.TrimPrefix(second, prefixModule))
	if third == second || third == first {
		t.Errorf("a directory named %q took the ID %q, which already belongs to another "+
			"directory: two things on one page is a merged node, not a naming wart",
			strings.TrimPrefix(second, prefixModule), third)
	}
}

// Two same-named directories whose keys hash to the same 32 bits. Then the suffix does
// not discriminate either, and something still has to.
//
// This is not hypothetical at the scale signpost is aimed at: 32 bits collides by the
// birthday bound somewhere around a hundred thousand keys, and the pair below was found
// by search rather than constructed, so a large monorepo can reach it. It is also why
// the ID is checked for use rather than assumed unique once suffixed.
func TestTwoDirectoriesWhoseKeysHashAlikeStillGetDistinctIDs(t *testing.T) {
	// Found by enumerating `/modules/pN/auth`; both keys give fnv32a base36 1qhazc2.
	const a, b = "p46047/auth", "p540990/auth"
	if keyHash(prefixModule+a) != keyHash(prefixModule+b) {
		t.Fatalf("fixture stale: %s and %s no longer hash alike, so this test covers "+
			"nothing — find a new pair or delete it", a, b)
	}
	// Three, not two: the first entry takes the bare name, so it is the second and third
	// that are both pushed onto the suffix and discover the hash does not separate them.
	x := newIDs()
	bare := x.assign(prefixModule, "aaa/auth", "auth")
	first := x.assign(prefixModule, a, "auth")
	second := x.assign(prefixModule, b, "auth")
	if first == bare || second == bare {
		t.Fatalf("setup: %q / %q collided with the bare name %q", first, second, bare)
	}
	if second == first {
		t.Errorf("both directories got %q: their names collide and so do their key "+
			"hashes, and the ID must still be unique because a shared ID is one page "+
			"claiming to describe two directories", first)
	}
}

// A directory whose name survives no slugging still needs a stable, unique ID. A
// counter would depend on how many such names preceded it; a hash does not.
func TestNonLatinDirectoryGetsStableID(t *testing.T) {
	files := map[string]string{
		"データ/x.go": "package data\n\nfunc X() {}\n",
		"資料/y.go":  "package docs\n\nfunc Y() {}\n",
	}
	first := build(t, files)
	ids := func(r *Result) []string {
		var out []string
		for _, n := range r.Graph.NodesOfKind(graph.KindModule) {
			out = append(out, n.ID)
		}
		return out
	}
	a := ids(first)
	if len(a) != 2 || a[0] == a[1] {
		t.Fatalf("ids = %v, want two distinct", a)
	}
	if b := ids(build(t, files)); fmt.Sprint(a) != fmt.Sprint(b) {
		t.Errorf("ids differ between runs: %v vs %v", a, b)
	}
}

// Determinism is a correctness property here, not a nicety: the bundle is committed,
// so a graph that varied by map iteration order would mean CI churn on every run.
func TestAssemblyIsDeterministic(t *testing.T) {
	files := map[string]string{
		"go.mod":                "module example.com/app\n\ngo 1.26\n\nrequire (\n\texample.com/z v1.0.0\n\texample.com/a v2.0.0\n)\n",
		"main.go":               "package main\n\nimport (\n\t\"example.com/app/internal/store\"\n\t\"example.com/app/internal/auth\"\n)\n\nfunc main() { auth.Check(); store.Put() }\n",
		"internal/auth/auth.go": "package auth\n\nfunc Check() {}\n",
		"internal/store/s.go":   "package store\n\nfunc Put() {}\n",
		"compose.yaml":          "services:\n  web:\n    depends_on: [api]\n  api:\n    build: ./internal/auth\n",
		"migrations/1_a.sql":    "CREATE TABLE zeta (id int);\nCREATE TABLE alpha (id int);\n",
		"CODEOWNERS":            "/internal/auth/ @b\n/internal/store/ @a\n",
	}
	first := ""
	for i := 0; i < 10; i++ {
		out := build(t, files)
		got := render(out)
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("run %d differed:\n%s\n---\n%s", i, got, first)
		}
	}
}

// render flattens the whole graph to a string, so a difference in any field of any
// node or edge fails the determinism test rather than only the fields it thought to
// compare.
func render(r *Result) string {
	var b strings.Builder
	for _, n := range r.Graph.Nodes() {
		fmt.Fprintf(&b, "N %s|%s|%s|%s|%s|%v|%v\n", n.ID, n.Kind, n.Title, n.Description, n.Path, n.Tags, n.Files)
		for _, k := range sortedKeys(n.Attrs) {
			fmt.Fprintf(&b, "  A %s=%s\n", k, n.Attrs[k])
		}
	}
	for _, e := range r.Graph.Edges() {
		fmt.Fprintf(&b, "E %s %s %s %s %d %s\n", e.From, e.To, e.Kind, e.Conf, e.Weight, e.Source)
	}
	for _, k := range sortedKeys(r.Unresolved) {
		fmt.Fprintf(&b, "U %s=%d\n", k, r.Unresolved[k])
	}
	return b.String()
}

// An empty repository must assemble to an empty graph rather than an error. A repo
// with nothing signpost recognizes is a real case — a documentation repo, a Terraform
// repo — and it should produce an honest empty bundle.
func TestEmptyInputIsNotAnError(t *testing.T) {
	out, err := Build(Input{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if n, e := out.Graph.Counts(); n != 0 || e != 0 {
		t.Errorf("counts = %d nodes, %d edges", n, e)
	}
}

// Secrets are referenced, never valued — proved by flattening every field of every
// node rather than by checking the fields a test thought to look at.
func TestSecretValuesNeverReachTheGraph(t *testing.T) {
	const password = "c3VwZXItc2VjcmV0LXBhc3N3b3Jk" //gitleaks:allow
	out := build(t, map[string]string{
		"deploy/secret.yaml": "apiVersion: v1\nkind: Secret\nmetadata:\n  name: db-creds\ndata:\n  password: " + password + "\n",
		"deploy/api.yaml":    "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: api\nspec:\n  template:\n    spec:\n      containers:\n        - name: api\n          env:\n            - name: DB_PASSWORD\n              valueFrom:\n                secretKeyRef:\n                  name: db-creds\n                  key: password\n",
	})
	flat := render(out)
	if strings.Contains(flat, password) {
		t.Fatal("a secret value reached the graph")
	}
	// The reference itself is architectural signal and must be present.
	api := node(t, out.Graph, "/services/api")
	if !strings.Contains(api.Attrs["secret_refs"], "db-creds") {
		t.Errorf("secret_refs = %q, want the secret named", api.Attrs["secret_refs"])
	}
	if !containsStr(api.Tags, "reads-secrets") {
		t.Errorf("api tags = %v", api.Tags)
	}
}

// A CMake target one build file declares and another links is this repository's own
// library, and reading one file at a time cannot tell. `target_link_libraries(app PRIVATE
// twofile_core)` names the target with nothing in the syntax saying where it comes from, and
// the declaration sits in the subdirectory's own CMakeLists.txt — which is what
// add_subdirectory is for, and the ordinary layout of a C project rather than an edge case.
// Before addBuildTargets every sibling library in every multi-directory C project got a
// reference page of its own, reported as a third-party dependency.
//
// Both halves are asserted, because dropping the external is only half the fix: which library
// an executable links is structure a C project states in no other file, so it has to survive
// as an edge onto the module holding that library rather than disappearing with the page.
func TestALinkedSiblingTargetIsNotAnExternalDependency(t *testing.T) {
	out := build(t, map[string]string{
		"CMakeLists.txt": "cmake_minimum_required(VERSION 3.20)\n" +
			"project(twofile VERSION 1.0.0 LANGUAGES C)\n" +
			"find_package(OpenSSL REQUIRED)\n" +
			"add_subdirectory(src)\n" +
			"add_executable(app app.c)\n" +
			"target_link_libraries(app PRIVATE twofile_core OpenSSL::SSL)\n",
		"src/CMakeLists.txt": "add_library(twofile_core STATIC core.c)\n",
		"app.c":              "int main(void) { return 0; }\n",
		"src/core.c":         "int core(void) { return 1; }\n",
	})

	for _, n := range out.Graph.Nodes() {
		if strings.Contains(n.ID, "twofile-core") || strings.Contains(n.ID, "twofile_core") {
			t.Errorf("node %q: a library this repository builds is not an external dependency", n.ID)
		}
	}
	// The negative half of the drop. A found package genuinely is from outside, and a rule
	// that dropped every cmake dependency would pass the check above while losing the one
	// supply-chain fact a C build states.
	node(t, out.Graph, "/references/cmake-openssl")

}

// The composition the link states, in the one shape where the link is the only thing stating
// it. Above, the root file both links `twofile_core` and calls add_subdirectory on the
// directory declaring it, so the edge onto that module is already there from the
// add_subdirectory alone — which is why that test asserts the dropped page and this one
// asserts the edge. Here `render` links `geometry` and neither directory includes the other:
// the root file adds both, and nothing but the link says which of the two is built against
// the other.
func TestALinkAcrossSiblingDirectoriesBecomesAnEdge(t *testing.T) {
	out := build(t, map[string]string{
		"CMakeLists.txt": "project(fan LANGUAGES CXX)\n" +
			"add_subdirectory(geometry)\n" +
			"add_subdirectory(render)\n",
		"geometry/CMakeLists.txt": "add_library(geometry STATIC point.cc)\n",
		"geometry/point.cc":       "int point() { return 1; }\n",
		"render/CMakeLists.txt": "add_library(render STATIC draw.cc)\n" +
			"target_link_libraries(render PUBLIC geometry)\n",
		"render/draw.cc": "int draw() { return 2; }\n",
	})
	if !hasEdge(out.Graph, "/modules/render", "/modules/geometry", graph.EdgeConfigures) {
		var got []string
		for _, e := range out.Graph.EdgesFrom("/modules/render") {
			got = append(got, string(e.Kind)+" -> "+e.To)
		}
		t.Errorf("edges from /modules/render = %v, want a configures edge onto the linked "+
			"library's module. Nothing else in this tree states that render is built against "+
			"geometry: neither directory includes the other.", got)
	}
}

// TestABazelLabelResolvesAgainstItsWorkspaceRoot covers the one thing a `//pkg` label cannot
// state: where the `//` is.
//
// A Bazel label is relative to the workspace root — the directory holding WORKSPACE or
// MODULE.bazel — and that is the repository root only in a repository that is one workspace.
// The corpus is the ordinary other shape: its workspace is under `go/`, so `//cmd/hello`
// means `go/cmd/hello`, and read as repository-relative it named a directory that does not
// exist and the declared edge vanished with no gap recorded anywhere.
//
// Both trees below declare the identical label from the identical relative position, and the
// only difference is where the root sits. That is what makes this a test of the root and not
// of the label: a fix that resolved against the repository root passes neither, and one that
// resolved against the declaring file's own directory passes neither.
func TestABazelLabelResolvesAgainstItsWorkspaceRoot(t *testing.T) {
	const (
		lib = "load(\"@rules_go//go:def.bzl\", \"go_library\")\n" +
			"go_library(name = \"greeter\", srcs = [\"greeter.go\"], deps = [\"//cmd/hello\"])\n"
		bin  = "go_binary(name = \"hello\", srcs = [\"main.go\"])\n"
		main = "package main\n\nfunc main() {}\n"
		src  = "package greeter\n\nfunc Greet() string { return \"hi\" }\n"
	)
	for _, tc := range []struct{ name, prefix string }{
		{"workspace at the repository root", ""},
		{"workspace in a subdirectory", "go/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := build(t, map[string]string{
				tc.prefix + "MODULE.bazel":          "module(name = \"w\", version = \"1\")\n",
				tc.prefix + "greeter/BUILD.bazel":   lib,
				tc.prefix + "greeter/greeter.go":    src,
				tc.prefix + "cmd/hello/BUILD.bazel": bin,
				tc.prefix + "cmd/hello/main.go":     main,
			})
			if !hasEdge(out.Graph, "/modules/greeter", "/modules/hello", graph.EdgeConfigures) {
				var got []string
				for _, e := range out.Graph.EdgesFrom("/modules/greeter") {
					got = append(got, string(e.Kind)+" -> "+e.To)
				}
				t.Errorf("edges from /modules/greeter = %v, want a configures edge onto the "+
					"module the `//cmd/hello` label names. The label is relative to the "+
					"workspace root, which is %q here.", got, "/"+tc.prefix)
			}
		})
	}
}

// The negative boundary on the label rule: a nested workspace's labels are its own.
//
// Workspaces nest in practice — a subproject brought in with the WORKSPACE file it was
// published with, or a monorepo whose Bazel tree contains a self-contained one. So a label has two candidate
// roots and only the nearer one is right, which is the same rule resolveC applies to include
// roots and for the same reason.
//
// Both directories below are named `tool` deliberately. A rule that took the outermost root
// resolves the label onto the wrong one and still produces an edge, so an assertion that only
// checked for *an* edge would pass — that is the mutant this exists to fail, and it is why the
// assertion is on the resolved path and not on the edge's presence.
func TestABazelLabelResolvesInsideTheNearestWorkspace(t *testing.T) {
	out := build(t, map[string]string{
		"MODULE.bazel":         "module(name = \"outer\", version = \"1\")\n",
		"tool/BUILD.bazel":     "go_library(name = \"tool\", srcs = [\"tool.go\"])\n",
		"tool/tool.go":         "package tool\n\nfunc Outer() {}\n",
		"sub/lib/MODULE.bazel": "module(name = \"lib\", version = \"1\")\n",
		"sub/lib/app/BUILD.bazel": "go_library(name = \"app\", srcs = [\"app.go\"], " +
			"deps = [\"//tool\"])\n",
		"sub/lib/app/app.go":       "package app\n\nfunc A() {}\n",
		"sub/lib/tool/BUILD.bazel": "go_library(name = \"tool\", srcs = [\"tool.go\"])\n",
		"sub/lib/tool/tool.go":     "package tool\n\nfunc Inner() {}\n",
	})

	// Which node id the two same-named directories were given depends on how ids were
	// suffixed, so the resolved path is what this asserts on.
	var got []string
	for _, e := range out.Graph.EdgesFrom("/modules/app") {
		if e.Kind != graph.EdgeConfigures {
			continue
		}
		got = append(got, node(t, out.Graph, e.To).Path)
	}
	if !containsStr(got, "sub/lib/tool") {
		t.Errorf("configures edges from /modules/app reach %v, want sub/lib/tool. "+
			"`//tool` in the nested workspace names that workspace's own package.", got)
	}
	if containsStr(got, "tool") {
		t.Errorf("configures edges from /modules/app reach %v, which includes the outer "+
			"workspace's tool/. A label is relative to the nearest workspace root above the "+
			"file that wrote it, so this is an edge between two projects that name each "+
			"other nowhere", got)
	}
}

// The negative boundary on the rule above: a name no build file in this tree declares is a
// real external dependency and must keep its page. Without this, dropping every linked name
// would look correct — the test above cannot tell the fix from a reader that records nothing.
func TestAnUndeclaredLinkedLibraryStaysExternal(t *testing.T) {
	out := build(t, map[string]string{
		"CMakeLists.txt": "project(single LANGUAGES CXX)\n" +
			"add_executable(app app.cc)\n" +
			"target_link_libraries(app PRIVATE fmt::fmt absl_strings)\n",
		"app.cc": "int main() { return 0; }\n",
	})
	for _, want := range []string{"/references/cmake-fmt", "/references/cmake-absl-strings"} {
		node(t, out.Graph, want)
	}
}
