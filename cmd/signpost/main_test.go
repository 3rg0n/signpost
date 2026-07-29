package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture writes a small multi-language repository and returns its path.
//
// Small but not trivial: an import cycle to find, a service to name, a dependency
// declared and imported, and a language with no extractor, because those are the
// four things the commands report on and a fixture without them would let a silent
// regression pass.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                  "module example.com/app\n\ngo 1.26\n\nrequire example.com/dep v1.0.0\n",
		"main.go":                 "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/app/internal/auth\"\n)\n\nfunc main() { fmt.Println(auth.Check()) }\n",
		"internal/auth/auth.go":   "package auth\n\nimport (\n\t\"example.com/app/internal/store\"\n\t\"example.com/dep/x\"\n)\n\nfunc Check() bool { return store.Get() && x.Y }\n",
		"internal/store/store.go": "package store\n\nimport \"example.com/app/internal/auth\"\n\nfunc Get() bool { return auth.Check() }\n",
		"compose.yaml":            "services:\n  api:\n    build: .\n    ports:\n      - \"8080:8080\"\n",
		"scratch.kt":              "fun main() { println(\"no extractor for this\") }\n",
	}
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// invoke runs the CLI exactly as main does and returns its streams and exit code.
func invoke(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestGraphReportsStructure(t *testing.T) {
	root := fixture(t)
	stdout, stderr, code := invoke(t, "graph", root)
	if code != 0 {
		t.Fatalf("exit = %d\nstderr:\n%s", code, stderr)
	}
	for _, want := range []string{"nodes", "edges", "clusters", "hubs", "concepts"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	// The cycle between auth and store is the finding a structural map exists to
	// surface, and it is invisible from the directory layout.
	if !strings.Contains(stdout, "import cycles") {
		t.Errorf("the auth/store cycle was not reported:\n%s", stdout)
	}
}

// Coverage goes to stderr on every analysing command and is not opt-in. Design §4.2:
// the absence of a measurement is never a clean bill of health, so a repository
// signpost read only part of must say so without being asked.
func TestCoverageGapsAreReportedByDefault(t *testing.T) {
	root := fixture(t)
	_, stderr, code := invoke(t, "graph", root)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr, "analysed") {
		t.Errorf("no coverage line:\n%s", stderr)
	}
	// The Kotlin file has no extractor, and a user whose repository is half Kotlin
	// must not be left thinking signpost read it. Named by extension, because
	// signpost has no classifier for a language it does not support and "other (1)"
	// would not tell anyone what went unread.
	if !strings.Contains(stderr, "no extractor for") || !strings.Contains(stderr, ".kt") {
		t.Errorf("unhandled language not reported by extension:\n%s", stderr)
	}
}

func TestQuietSuppressesCoverageOnly(t *testing.T) {
	root := fixture(t)
	stdout, stderr, code := invoke(t, "graph", "--quiet", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if stderr != "" {
		t.Errorf("--quiet left output on stderr:\n%s", stderr)
	}
	if !strings.Contains(stdout, "nodes") {
		t.Error("--quiet suppressed the report itself")
	}
}

// --fail-on-cycle is the CI gate, so its exit code is the whole contract.
func TestFailOnCycleExitsNonZero(t *testing.T) {
	root := fixture(t)
	if _, _, code := invoke(t, "graph", "--quiet", root); code != 0 {
		t.Errorf("without the flag a cycle is a finding, not a failure: exit = %d", code)
	}
	_, stderr, code := invoke(t, "graph", "--quiet", "--fail-on-cycle", root)
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "cycle") {
		t.Errorf("stderr does not say why it failed:\n%s", stderr)
	}
}

func TestFailOnCyclePassesOnAcyclicRepo(t *testing.T) {
	root := t.TempDir()
	write := func(p, content string) {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/clean\n\ngo 1.26\n")
	write("main.go", "package main\n\nimport \"example.com/clean/internal/a\"\n\nfunc main() { a.F() }\n")
	write("internal/a/a.go", "package a\n\nfunc F() {}\n")

	if _, stderr, code := invoke(t, "graph", "--quiet", "--fail-on-cycle", root); code != 0 {
		t.Errorf("exit = %d on an acyclic repo\n%s", code, stderr)
	}
}

func TestExportEveryFormat(t *testing.T) {
	root := fixture(t)
	for _, tc := range []struct{ format, want string }{
		{"mermaid", "flowchart LR"},
		{"dot", "digraph signpost {"},
		{"graphml", "<graphml"},
		{"json", `"nodes"`},
	} {
		stdout, stderr, code := invoke(t, "export", "--format", tc.format, "--quiet", root)
		if code != 0 {
			t.Errorf("%s: exit = %d\n%s", tc.format, code, stderr)
			continue
		}
		if !strings.Contains(stdout, tc.want) {
			t.Errorf("%s: output does not look like %s:\n%s", tc.format, tc.format, stdout)
		}
	}
}

func TestExportDefaultsToMermaid(t *testing.T) {
	stdout, _, code := invoke(t, "export", "--quiet", fixture(t))
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "flowchart LR") {
		t.Error("default format is not mermaid")
	}
}

func TestExportToFile(t *testing.T) {
	root := fixture(t)
	dest := filepath.Join(t.TempDir(), "graph.json")
	stdout, stderr, code := invoke(t, "export", "--format", "json", "--quiet", "-o", dest, root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("wrote to both the file and stdout:\n%s", stdout)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("output file: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("file is not valid json: %v", err)
	}
	if _, ok := parsed["nodes"]; !ok {
		t.Error("no nodes in the exported file")
	}
	// The temp file the atomic write went through must not be left behind: a
	// stray dotfile in a repository is noise a user has to clean up.
	entries, err := os.ReadDir(filepath.Dir(dest))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".signpost-export-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// An export written twice is byte-identical, which is what makes it safe to commit:
// design §8.1 treats instability here as CI churn rather than a cosmetic problem.
func TestExportIsReproducible(t *testing.T) {
	root := fixture(t)
	for _, format := range []string{"mermaid", "dot", "graphml", "json"} {
		first, _, code := invoke(t, "export", "--format", format, "--quiet", root)
		if code != 0 {
			t.Fatalf("%s: exit = %d", format, code)
		}
		for i := 0; i < 3; i++ {
			again, _, _ := invoke(t, "export", "--format", format, "--quiet", root)
			if again != first {
				t.Fatalf("%s: output differs between runs", format)
			}
		}
	}
}

// A wrong command line exits 2, a repository finding exits 1. CI needs to tell them
// apart: the first means the invocation is broken, the second means the code is.
func TestUsageErrorsExitTwo(t *testing.T) {
	root := fixture(t)
	cases := []struct {
		name string
		args []string
	}{
		{"no arguments", nil},
		{"unknown command", []string{"frobnicate"}},
		{"unknown format", []string{"export", "--format", "svg", root}},
		{"unknown flag", []string{"graph", "--nope", root}},
		{"two paths", []string{"graph", root, root}},
		{"help flag on a command", []string{"graph", "-h"}},
	}
	for _, tc := range cases {
		_, stderr, code := invoke(t, tc.args...)
		if code != 2 {
			t.Errorf("%s: exit = %d, want 2", tc.name, code)
		}
		if strings.TrimSpace(stderr) == "" {
			t.Errorf("%s: exited 2 with nothing on stderr", tc.name)
		}
	}
}

func TestHelpExitsZero(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		stdout, _, code := invoke(t, arg)
		if code != 0 {
			t.Errorf("%s: exit = %d, want 0", arg, code)
		}
		// Every command has to appear, or the help is a trap.
		for _, c := range commands() {
			if !strings.Contains(stdout, c.name) {
				t.Errorf("%s: help omits %q", arg, c.name)
			}
		}
	}
}

func TestVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		stdout, _, code := invoke(t, args...)
		if code != 0 {
			t.Errorf("%v: exit = %d", args, code)
		}
		if strings.TrimSpace(stdout) != version {
			t.Errorf("%v: printed %q, want %q", args, strings.TrimSpace(stdout), version)
		}
	}
}

// A path that does not exist is an error, not an empty graph. Reporting "0 nodes" for
// a typo'd path is the kind of quiet wrong answer that wastes an afternoon.
func TestMissingPathIsAnError(t *testing.T) {
	_, stderr, code := invoke(t, "graph", filepath.Join(t.TempDir(), "nope"))
	if code == 0 {
		t.Error("a missing path exited 0")
	}
	if strings.TrimSpace(stderr) == "" {
		t.Error("no error message")
	}
}

// An empty directory is a valid repository with nothing in it, which must not be an
// error: signpost run in a fresh repo should say so, not fail.
func TestEmptyRepositoryIsNotAnError(t *testing.T) {
	root := t.TempDir()
	stdout, stderr, code := invoke(t, "graph", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "0 nodes") {
		t.Errorf("stdout = %q", stdout)
	}
}

// --ignore is repeatable, and a pattern containing a comma must survive: that is why
// it is a repeatable flag rather than a comma-separated list.
func TestIgnorePatternsAreApplied(t *testing.T) {
	root := fixture(t)
	base, _, _ := invoke(t, "export", "--format", "json", "--quiet", root)
	ignored, _, code := invoke(t, "export", "--format", "json", "--quiet",
		"--ignore", "internal/store/**", root)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(base, "/modules/store") {
		t.Fatalf("fixture has no store module to ignore:\n%s", base)
	}
	if strings.Contains(ignored, "/modules/store") {
		t.Error("--ignore did not exclude the pattern")
	}
}

func TestNoClusterSkipsSubgraphs(t *testing.T) {
	root := fixture(t)
	with, _, _ := invoke(t, "export", "--quiet", root)
	without, _, code := invoke(t, "export", "--quiet", "--no-cluster", root)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(with, "subgraph") {
		t.Fatalf("clustered export has no subgraph:\n%s", with)
	}
	if strings.Contains(without, "subgraph") {
		t.Error("--no-cluster still drew subgraphs")
	}
}
