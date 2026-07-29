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
