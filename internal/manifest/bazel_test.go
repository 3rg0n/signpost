package manifest

import (
	"strings"
	"testing"
	"time"

	"github.com/3rg0n/signpost/internal/discover"
)

func bazelFile(path, content string) discover.File {
	return discover.File{Path: path, Class: discover.ClassManifest, Content: content}
}

func TestExtractBazelReadsTargetsAndLabels(t *testing.T) {
	f := bazelFile("internal/graph/BUILD.bazel", `
load("@rules_go//go:def.bzl", "go_library", "go_test")
load("//tools:macros.bzl", "wrapper")

go_library(
    name = "graph",
    srcs = [
        "graph.go",
        "louvain.go",
    ],
    importpath = "github.com/3rg0n/signpost/internal/graph",
    visibility = ["//visibility:public"],
    deps = [
        "//internal/discover",
        "@com_github_pkg_errors//:errors",
        ":helper",
    ],
)

go_binary(
    name = "graphctl",
    srcs = ["main.go"],
    deps = [":graph"],
)

go_test(
    name = "graph_test",
    srcs = ["graph_test.go"],
    embed = [":graph"],
)
`)
	facts := ExtractBazel(f)

	if facts.Kind != KindBazel {
		t.Errorf("kind = %q, want %q", facts.Kind, KindBazel)
	}
	if facts.Incomplete {
		t.Fatalf("a well-formed BUILD file must not be reported as partially read: %q", facts.Note)
	}

	deps := map[string]Dep{}
	for _, d := range facts.Deps {
		deps[d.Name] = d
	}
	// An absolute in-repo label is this repository's own code, which is exactly the
	// distinction Dep.Local exists for — and Bazel states it syntactically, with no guessing.
	if got := deps["discover"]; got.Source != "internal/discover" || !got.Local {
		t.Errorf("//internal/discover = %+v, want a local dep on the package directory", got)
	}
	// An `@repo//pkg:target` names another repository. The repository is the dependency,
	// not the target inside it.
	if got := deps["com_github_pkg_errors"]; got.Local || got.Ecosystem != ecoBazel {
		t.Errorf("external label = %+v, want a non-local bazel dep", got)
	}
	// A `load()` from another repository is a build-time dependency this file cannot work
	// without; a load from this repository is not a dependency at all.
	if _, ok := deps["rules_go"]; !ok {
		t.Errorf("deps = %+v, want the loaded rule set recorded", facts.Deps)
	}
	if _, ok := deps["tools"]; ok {
		t.Errorf("deps = %+v; an in-repo load is this repository's own rules", facts.Deps)
	}
	// A relative label names this package, so recording it would draw a module's edge to
	// itself. Asserted as the whole set rather than by absence of `helper`, because the
	// failure this guards against does not produce a dep called `helper` — it produces one
	// named by whatever an empty package path resolves to, which no by-name check would see.
	want := []string{"com_github_pkg_errors", "discover", "rules_go"}
	if got := facts.DepNames(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("dep names = %v, want exactly %v", got, want)
	}

	// A `_binary` is a program and a `_library` is not, which is what an Entrypoint means.
	if len(facts.Entrypoints) != 1 || facts.Entrypoints[0].Name != "graphctl" {
		t.Fatalf("entrypoints = %+v, want only the binary", facts.Entrypoints)
	}
	if facts.Entrypoints[0].Path != "//internal/graph:graphctl" {
		t.Errorf("entrypoint path = %q, want the label a person runs", facts.Entrypoints[0].Path)
	}

	// A rule ending `_test` produces a test target by Bazel's own naming rule, which is a
	// stated fact rather than the directory guess isTestPath has to make.
	if len(facts.Scripts) != 1 || facts.Scripts[0].Name != "graph_test" {
		t.Fatalf("scripts = %+v, want the test target", facts.Scripts)
	}
	if !strings.Contains(facts.Scripts[0].Command, "//internal/graph:graph_test") {
		t.Errorf("test command = %q, want the runnable label", facts.Scripts[0].Command)
	}
}

func TestExtractBazelReadsModuleAndWorkspaceDependencies(t *testing.T) {
	mod := ExtractBazel(bazelFile("MODULE.bazel", `
module(
    name = "signpost",
    version = "0.1.0",
    compatibility_level = 1,
)

bazel_dep(name = "rules_go", version = "0.46.0")
bazel_dep(name = "gazelle", version = "0.35.0", dev_dependency = True)
`))
	if mod.Module.Name != "signpost" || mod.Module.Version != "0.1.0" {
		t.Errorf("module = %+v, want bzlmod's own declaration", mod.Module)
	}
	deps := map[string]Dep{}
	for _, d := range mod.Deps {
		deps[d.Name] = d
	}
	if got := deps["rules_go"]; got.Version != "0.46.0" || got.Optional {
		t.Errorf("rules_go = %+v, want the pinned version and not optional", got)
	}
	// A dev_dependency is not needed to build what this module ships, which is what
	// Dep.Optional means.
	if got := deps["gazelle"]; !got.Optional {
		t.Errorf("gazelle = %+v, want optional for a dev_dependency", got)
	}

	ws := ExtractBazel(bazelFile("WORKSPACE", `
http_archive(
    name = "rules_python",
    sha256 = "9c6e26911a79fbf510a8f06d8eedb40f412023cf7fa6d1461def27116bff022c",
    urls = ["https://github.com/bazelbuild/rules_python/releases/download/0.31.0/rules_python-0.31.0.tar.gz"],
)

git_repository(
    name = "abseil",
    remote = "https://github.com/abseil/abseil-cpp.git",
    commit = "2f9e432cce407ce0ae50676696666f33a77d42ac",
)

local_repository(
    name = "vendored",
    path = "third_party/vendored",
)

http_archive(
    name = "unpinned",
    urls = ["https://example.invalid/archive.tar.gz"],
)
`))
	wsDeps := map[string]Dep{}
	for _, d := range ws.Deps {
		wsDeps[d.Name] = d
	}
	// The sha256 is the pin, and the URL is the origin — the fact that matters most, since a
	// dependency fetched from a URL has no registry to publish an advisory against.
	if got := wsDeps["rules_python"]; !strings.HasPrefix(got.Version, "9c6e2691") ||
		!strings.Contains(got.Source, "rules_python-0.31.0.tar.gz") {
		t.Errorf("rules_python = %+v, want the sha256 and the archive URL", got)
	}
	if got := wsDeps["abseil"]; got.Version != "2f9e432cce407ce0ae50676696666f33a77d42ac" ||
		got.Source != "https://github.com/abseil/abseil-cpp.git" {
		t.Errorf("abseil = %+v, want the commit and the remote", got)
	}
	// A local_repository is the one form of this that is not external at all.
	if got := wsDeps["vendored"]; !got.Local || got.Source != "third_party/vendored" {
		t.Errorf("vendored = %+v, want a local dependency", got)
	}
	// The negative boundary: an unpinned archive must not acquire a version. An invented one
	// would read as a pin, which is what a reader auditing the supply chain acts on.
	if got := wsDeps["unpinned"]; got.Version != "" {
		t.Errorf("unpinned = %+v, want no version for an archive with no sha256", got)
	}
}

// A label's three forms are three different facts, and this is where each is pinned down.
func TestSplitBazelLabel(t *testing.T) {
	for _, tc := range []struct{ label, repo, pkg string }{
		{"@rules_go//go:def.bzl", "rules_go", "go"},
		{"@com_github_pkg_errors//:errors", "com_github_pkg_errors", ""},
		{"@rules_go", "rules_go", ""},
		// bzlmod's canonical form carries the module version in the repository name; the
		// dependency is the module, and the version belongs to the declaration that pinned it.
		{"@@rules_go~0.46.0//go:def.bzl", "rules_go", "go"},
		{"@@rules_go+0.46.0//go", "rules_go", "go"},
		{"//internal/graph:graph", "", "internal/graph"},
		{"//internal/graph", "", "internal/graph"},
		{":helper", "", ""},
		{"helper", "", ""},
	} {
		repo, pkg := splitBazelLabel(tc.label)
		if repo != tc.repo || pkg != tc.pkg {
			t.Errorf("splitBazelLabel(%q) = (%q, %q), want (%q, %q)", tc.label, repo, pkg, tc.repo, tc.pkg)
		}
	}
}

// A `select()` or a `glob()` is an expression this reader steps over, and the deps declared
// literally beside it are still real deps. Dropping the whole list because one entry was
// computed would lose declarations for a reason the file's author would not recognise.
func TestExtractBazelKeepsLiteralDepsBesideExpressions(t *testing.T) {
	facts := ExtractBazel(bazelFile("cmd/tool/BUILD", `
cc_binary(
    name = "tool",
    srcs = glob(["*.cc"]),
    deps = [
        "//internal/core",
        PLATFORM_DEP,
        "@fmt//:fmt",
    ] + select({
        "//conditions:default": ["//internal/posix"],
    }),
)
`))
	got := map[string]bool{}
	for _, d := range facts.Deps {
		got[d.Name] = true
	}
	for _, want := range []string{"core", "fmt"} {
		if !got[want] {
			t.Errorf("deps = %+v, want %q read from the literal half of the list", facts.Deps, want)
		}
	}
	// The negative half: nothing the reader could not read becomes a dependency, and the
	// target itself is still found.
	for _, unwanted := range []string{"PLATFORM_DEP", "conditions", "tool"} {
		if got[unwanted] {
			t.Errorf("deps = %+v; %q is not a dependency", facts.Deps, unwanted)
		}
	}
	if len(facts.Entrypoints) != 1 || facts.Entrypoints[0].Name != "tool" {
		t.Errorf("entrypoints = %+v, want the binary despite the globbed srcs", facts.Entrypoints)
	}
}

// A target with no literal name is a target this reader cannot name, which is the same honest
// limit CMake's computed names have — and it is recorded rather than guessed at.
func TestExtractBazelReportsComputedNamesAsUnread(t *testing.T) {
	facts := ExtractBazel(bazelFile("BUILD", `
go_binary(
    name = NAME,
    srcs = ["main.go"],
)
`))
	if len(facts.Entrypoints) != 0 {
		t.Errorf("entrypoints = %+v; a computed name is not a target name", facts.Entrypoints)
	}
	if !facts.Incomplete || !strings.Contains(facts.Note, "go_binary") {
		t.Errorf("incomplete = %v note = %q, want the unread rule recorded", facts.Incomplete, facts.Note)
	}
}

// The stated limit, asserted rather than left implicit: a target produced inside a loop or a
// macro body is not a top-level call and is not read. Asserting it is what makes the limit a
// decision instead of a surprise, and what will catch it changing.
func TestExtractBazelDoesNotReadIndentedRuleCalls(t *testing.T) {
	facts := ExtractBazel(bazelFile("BUILD", `
go_binary(
    name = "real",
    srcs = ["main.go"],
)

for name in ["a", "b"]:
    go_binary(
        name = name,
        srcs = [name + ".go"],
    )
`))
	if len(facts.Entrypoints) != 1 || facts.Entrypoints[0].Name != "real" {
		t.Errorf("entrypoints = %+v, want only the top-level target", facts.Entrypoints)
	}
}

func TestParseStarlarkHandlesStringsAndComments(t *testing.T) {
	calls, diag := parseStarlark(`
"""A module docstring, which spans lines and holds an ( unbalanced paren.

go_binary(name = "ghost")
"""

# go_binary(name = "commented_out")

py_library(
    name = "lib",  # a trailing comment
    srcs = ["a.py"],
    # a comment between arguments
    deps = ["//x"],
)

CONSTANT = ["not", "a", "call"]

genrule(
    name = "gen",
    cmd = "echo ')' > $@",
)
`)
	if diag.Malformed {
		t.Fatalf("diag = %+v, want a clean read", diag)
	}
	fns := map[string]starlarkCall{}
	for _, c := range calls {
		fns[c.str("name")] = c
	}
	// A docstring is a string, not a call site, and a `(` inside it is content.
	if _, ok := fns["ghost"]; ok {
		t.Errorf("calls = %+v; a call inside a docstring is not a call", calls)
	}
	if _, ok := fns["commented_out"]; ok {
		t.Errorf("calls = %+v; a commented-out call is not a call", calls)
	}
	// A newline inside brackets is not a statement boundary — Python's implicit line
	// continuation, which every multi-line rule call depends on.
	lib, ok := fns["lib"]
	if !ok {
		t.Fatalf("calls = %+v, want the multi-line rule", calls)
	}
	if got := lib.list("deps"); len(got) != 1 || got[0] != "//x" {
		t.Errorf("deps = %v, want the list after the interleaved comment", got)
	}
	// A `)` inside a string argument does not end the call.
	if _, ok := fns["gen"]; !ok {
		t.Errorf("calls = %+v, want the genrule whose cmd holds a paren", calls)
	}
	// An assignment is not a call, even though it looks like one up to the `=`.
	for _, c := range calls {
		if c.fn == "CONSTANT" {
			t.Errorf("calls = %+v; an assignment is not a call", calls)
		}
	}
}

// An unterminated call is a broken document rather than a construct stepped over, which is
// what Diag.Malformed carries — and what was read before the fault is kept.
func TestParseStarlarkReportsUnterminatedCalls(t *testing.T) {
	calls, diag := parseStarlark("go_library(name = \"ok\")\ngo_binary(name = \"broken\",\n")
	if !diag.Malformed {
		t.Errorf("diag = %+v, want the document reported unparseable", diag)
	}
	if len(calls) < 1 || calls[0].str("name") != "ok" {
		t.Errorf("calls = %+v, want the call before the fault", calls)
	}
}

// A malformed file must be read wrongly at worst, never read forever. Both of these once spun:
// a closer that is not the one the loop is waiting for is declined by every scanner in it, so
// the loop was offered the same byte until the process was killed. Written as a table with a
// deadline because a spin does not fail a test, it hangs the suite — and one hung reader in CI
// stops the whole bundle.
func TestParseStarlarkTerminatesOnUnbalancedBrackets(t *testing.T) {
	for _, src := range []string{
		"go_binary(a})\n",
		"go_binary(name = \"x\", deps = [\"//a\"})\n",
		"go_binary(}\n",
		"go_binary([a})\n",
	} {
		done := make(chan Diag, 1)
		go func(src string) {
			_, diag := parseStarlark(src)
			done <- diag
		}(src)
		select {
		case diag := <-done:
			if !diag.Malformed {
				t.Errorf("parseStarlark(%q) diag = %+v, want the document reported unparseable", src, diag)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("parseStarlark(%q) did not terminate", src)
		}
	}
}

func TestMatchBazelClaimsBuildFilesOnly(t *testing.T) {
	for _, p := range []string{
		"BUILD", "internal/graph/BUILD", "BUILD.bazel", "WORKSPACE", "WORKSPACE.bzlmod",
		"MODULE.bazel", "tools/macros.bzl",
	} {
		if !matchBazel(discover.File{Path: p}) {
			t.Errorf("matchBazel(%q) = false, want true", p)
		}
	}
	// The negative half. `build/` is an ordinary directory name and `build.gradle` is
	// another ecosystem's manifest entirely; claiming either would read a Gradle file with
	// Starlark's rules.
	for _, p := range []string{
		"build/output.txt", "build.gradle", "build.gradle.kts", "buildspec.yml",
		"src/build.go", "Makefile",
	} {
		if matchBazel(discover.File{Path: p}) {
			t.Errorf("matchBazel(%q) = true, want false", p)
		}
	}
}
