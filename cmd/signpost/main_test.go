package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture writes a small multi-language repository and returns its path.
//
// Small but not trivial: an import cycle to find, a service to name, a dependency
// declared and imported, a language with no extractor, and a file of no recognised
// kind at all, because those are the things the commands report on and a fixture
// without them would let a silent regression pass.
//
// The last two are deliberately different files. `.scala` is a source language signpost
// knows and cannot read; `.hbs` is a file it cannot classify, so no reader is ever
// offered it. One fixture file could not tell the two coverage lines apart.
//
// Both change as readers land, and both have already changed once. The unreadable
// language was `.kt` until Kotlin got an extractor; Scala is the deliberate replacement,
// because it is the JVM language the Java and Kotlin work explicitly deferred, so a
// fixture asserting it goes unread asserts something true rather than something
// forgotten. The unclassifiable file was `.astro` until the single-file-component
// extractor claimed it, and a Handlebars template is the honest successor: it is a real
// template language with no classification and no reader, so nothing here pretends a gap
// that has been closed is still open.
func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"go.mod":                  "module example.com/app\n\ngo 1.26\n\nrequire example.com/dep v1.0.0\n",
		"main.go":                 "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/app/internal/auth\"\n)\n\nfunc main() { fmt.Println(auth.Check()) }\n",
		"internal/auth/auth.go":   "package auth\n\nimport (\n\t\"example.com/app/internal/store\"\n\t\"example.com/dep/x\"\n)\n\nfunc Check() bool { return store.Get() && x.Y }\n",
		"internal/store/store.go": "package store\n\nimport \"example.com/app/internal/auth\"\n\nfunc Get() bool { return auth.Check() }\n",
		"compose.yaml":            "services:\n  api:\n    build: .\n    ports:\n      - \"8080:8080\"\n",
		"scratch.scala":           "object Scratch { def main(args: Array[String]): Unit = println(\"no extractor for this\") }\n",
		"web/card.hbs":            "<div class=\"card\">{{title}}</div>\n",
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
	stdout, stderr, code := invoke(t, "graph", "show", root)
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
	_, stderr, code := invoke(t, "graph", "show", root)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stderr, "analysed") {
		t.Errorf("no coverage line:\n%s", stderr)
	}
	// The Scala file has no extractor, and a user whose repository is half Scala
	// must not be left thinking signpost read it. Named by extension, because
	// signpost has no classifier for a language it does not support and "other (1)"
	// would not tell anyone what went unread.
	if !strings.Contains(stderr, "no extractor for") || !strings.Contains(stderr, ".scala") {
		t.Errorf("unhandled language not reported by extension:\n%s", stderr)
	}
	// The .hbs file is a different admission and gets a different line. It is not a
	// language with no extractor: it has no classification, so no reader was offered
	// it and nothing downstream recorded that it existed. Reported separately because
	// folding it into the line above would let a repository whose only frontend is
	// unclassifiable read as fully covered — the shape this was found in, where the
	// extension was `.astro` and the frontend was two pages.
	if !strings.Contains(stderr, "no recognised kind") || !strings.Contains(stderr, ".hbs") {
		t.Errorf("unclassified file not reported:\n%s", stderr)
	}
	// And the two lines stay distinct. A single line naming both extensions would
	// satisfy the two checks above while losing the distinction they exist to draw.
	if strings.Contains(coverageLine(stderr, "no extractor for"), ".hbs") {
		t.Errorf("unclassified file folded into the no-extractor line:\n%s", stderr)
	}
	if strings.Contains(coverageLine(stderr, "no recognised kind"), ".scala") {
		t.Errorf("unreadable language folded into the unclassified line:\n%s", stderr)
	}
}

// coverageLine returns the one line of a coverage report containing want, or "".
func coverageLine(report, want string) string {
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, want) {
			return line
		}
	}
	return ""
}

// Issue #11, through the binary and stated the way the issue states it: the flag moved the
// file count and left the node count alone, and the asymmetry with -include-fixtures is what
// made it visible. Both flags are exercised against one tree here for that reason — a fix
// that regressed fixtures while fixing vendored would pass a test that only knew about one.
//
// The node count is the assertion because it is the number that failed. A count assertion is
// normally avoided in this suite (see corpus_test.go: it fails on every extractor
// improvement), but the whole defect was a count that did not move, and the tree is four
// files written by this test rather than a repository anything else can change.
func TestIncludeFlagsReachTheGraphNotJustTheWalk(t *testing.T) {
	root := t.TempDir()
	for p, content := range map[string]string{
		"go.mod":                    "module example.com/app\n\ngo 1.26\n",
		"main.go":                   "package main\n\nfunc main() {}\n",
		"vendor/example.com/v/v.go": "package v\n\nfunc V() {}\n",
		"testdata/sample/s/s.go":    "package s\n\nfunc S() {}\n",
	} {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	nodes := func(t *testing.T, flags ...string) int {
		t.Helper()
		args := append([]string{"graph", "show"}, flags...)
		stdout, stderr, code := invoke(t, append(args, "--quiet", root)...)
		if code != 0 {
			t.Fatalf("graph %v: exit = %d\n%s", flags, code, stderr)
		}
		var n int
		for _, line := range strings.Split(stdout, "\n") {
			if _, err := fmt.Sscanf(strings.TrimSpace(line), "%d nodes,", &n); err == nil {
				return n
			}
		}
		t.Fatalf("no node count in the report:\n%s", stdout)
		return 0
	}

	base := nodes(t)
	// The negative boundary: neither directory is analysed unless asked for. Without this
	// the two assertions below are satisfied by a tool that ignores both flags and analyses
	// everything, which is the failure that costs a committed page a phantom module.
	if base != 1 {
		t.Fatalf("%d nodes by default, want only the root module: a vendored or fixture "+
			"directory was analysed without being asked for", base)
	}
	if got := nodes(t, "-include-vendored"); got != base+1 {
		t.Errorf("-include-vendored: %d nodes, want %d. The flag read the vendored file and "+
			"every consumer filtered it out again, so it changed the file count and nothing "+
			"else — issue #11.", got, base+1)
	}
	if got := nodes(t, "-include-fixtures"); got != base+1 {
		t.Errorf("-include-fixtures: %d nodes, want %d", got, base+1)
	}
}

func TestQuietSuppressesCoverageOnly(t *testing.T) {
	root := fixture(t)
	stdout, stderr, code := invoke(t, "graph", "show", "--quiet", root)
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
	if _, _, code := invoke(t, "graph", "show", "--quiet", root); code != 0 {
		t.Errorf("without the flag a cycle is a finding, not a failure: exit = %d", code)
	}
	_, stderr, code := invoke(t, "graph", "show", "--quiet", "--fail-on-cycle", root)
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

	if _, stderr, code := invoke(t, "graph", "show", "--quiet", "--fail-on-cycle", root); code != 0 {
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
		stdout, stderr, code := invoke(t, "graph", "export", "--format", tc.format, "--quiet", root)
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
	stdout, _, code := invoke(t, "graph", "export", "--quiet", fixture(t))
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
	stdout, stderr, code := invoke(t, "graph", "export", "--format", "json", "--quiet", "-o", dest, root)
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
		first, _, code := invoke(t, "graph", "export", "--format", format, "--quiet", root)
		if code != 0 {
			t.Fatalf("%s: exit = %d", format, code)
		}
		for i := 0; i < 3; i++ {
			again, _, _ := invoke(t, "graph", "export", "--format", format, "--quiet", root)
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
		{"unknown format", []string{"graph", "export", "--format", "svg", root}},
		{"unknown flag", []string{"graph", "show", "--nope", root}},
		{"two paths", []string{"graph", "show", root, root}},
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

// Help works at every level, not just the root, and each level lists its own children.
// The reason to assert it here rather than trust it is that this used to be two
// implementations: `model` printed its own help and the root printed a different
// format, so one could gain a group the other did not know how to describe.
func TestHelpWorksAtEveryLevel(t *testing.T) {
	for _, c := range commands() {
		if c.run != nil {
			continue
		}
		for _, arg := range []string{"-h", "--help", "help"} {
			stdout, stderr, code := invoke(t, c.name, arg)
			if code != 0 {
				t.Errorf("%s %s: exit = %d, want 0\n%s", c.name, arg, code, stderr)
			}
			if !strings.Contains(stdout, "signpost "+c.name+" <command>") {
				t.Errorf("%s %s: usage line does not name the group:\n%s", c.name, arg, stdout)
			}
			for _, s := range c.subs {
				if !strings.Contains(stdout, s.name) {
					t.Errorf("%s %s: omits subcommand %q\n%s", c.name, arg, s.name, stdout)
				}
			}
		}
	}
}

// A leaf command's `-h` is an answer, so it exits 0 and it goes to stdout. Both halves
// were wrong at once: `-h` reached runOr as flag.ErrHelp and fell through to 2 while the
// root and the groups exited 0, and the whole of the text went to stderr, so `signpost
// graph show -h | less` showed nothing and the shell saw a failure.
//
// stderr is asserted empty rather than merely smaller because the first fix passed a
// looser check: the prose moved to stdout and fs.PrintDefaults' flag list stayed behind
// on stderr, which is help split down the middle and reads as working from either side
// alone.
func TestLeafHelpExitsZeroOnStdout(t *testing.T) {
	for _, path := range leafPaths(commands(), nil) {
		for _, arg := range []string{"-h", "--help"} {
			stdout, stderr, code := invoke(t, append(append([]string{}, path...), arg)...)
			name := strings.Join(path, " ")
			if code != 0 {
				t.Errorf("%s %s: exit = %d, want 0\n%s", name, arg, code, stderr)
			}
			if !strings.Contains(stdout, "usage: signpost "+name) {
				t.Errorf("%s %s: stdout does not carry the usage line:\n%s", name, arg, stdout)
			}
			// The flag list is the half that lived on the wrong stream, so it has to be
			// asserted separately from the prose — but only where there is one. `hooks
			// install` and `hooks uninstall` take no flags, and requiring a list there
			// would push them into printing an empty `Flags:` heading to satisfy a test.
			if _, ok := flaglessLeaves[name]; !ok && !strings.Contains(stdout, "  -") {
				t.Errorf("%s %s: stdout has no flag list, so PrintDefaults went elsewhere:\n%s",
					name, arg, stdout)
			}
			if stderr != "" {
				t.Errorf("%s %s: wrote to stderr:\n%s", name, arg, stderr)
			}
		}
	}
}

// flaglessLeaves are the commands that take no flags, so their help has no flag list to
// check. An explicit list rather than "assert a flag list only if one appears", because the
// bug this test exists for is a flag list going to the wrong stream — and a check that
// tolerates its absence would pass on exactly that.
//
// TestEveryFlaglessLeafReallyHasNoFlags keeps the list honest in the other direction.
var flaglessLeaves = map[string]struct{}{
	"hooks install":   {},
	"hooks uninstall": {},
}

// The exemption above is load-bearing, so it gets checked from the other side: a command
// listed as flagless that later grows a flag would otherwise stop being covered by the help
// contract without anything saying so.
func TestEveryFlaglessLeafReallyHasNoFlags(t *testing.T) {
	for name := range flaglessLeaves {
		stdout, _, _ := invoke(t, append(strings.Split(name, " "), "-h")...)
		if strings.Contains(stdout, "\n  -") {
			t.Errorf("%s is listed as flagless but its help prints a flag list:\n%s", name, stdout)
		}
	}
	// And nothing in the list may be a name that does not exist, which is how an exemption
	// outlives the command it was written for and starts covering nothing.
	leaves := map[string]struct{}{}
	for _, p := range leafPaths(commands(), nil) {
		leaves[strings.Join(p, " ")] = struct{}{}
	}
	for name := range flaglessLeaves {
		if _, ok := leaves[name]; !ok {
			t.Errorf("flaglessLeaves names %q, which is not a leaf command", name)
		}
	}
}

// leafPaths returns the full argument path of every runnable command, so a test covers
// commands added later without being edited. `version` is skipped: it takes no flags and
// has no usage text, which is the one leaf the help contract does not describe.
func leafPaths(cmds []command, prefix []string) [][]string {
	var out [][]string
	for _, c := range cmds {
		path := append(append([]string{}, prefix...), c.name)
		switch {
		case c.name == "version":
		case c.run != nil:
			out = append(out, path)
		default:
			out = append(out, leafPaths(c.subs, path)...)
		}
	}
	return out
}

// A group's own name is never an action. The whole reason `graph` grew a `show` is that
// a name which is sometimes a command and sometimes a namespace has to be learned
// twice, and the failure this guards is a later change quietly giving a group a default
// verb — at which point `signpost graph` starts analysing a repository again and the
// rule in commands() is a comment describing something untrue.
func TestAGroupNameIsNotItselfAnAction(t *testing.T) {
	for _, c := range commands() {
		if c.run != nil {
			continue
		}
		if len(c.subs) == 0 {
			t.Errorf("%q has neither a run function nor subcommands, so it cannot be invoked", c.name)
		}
		_, stderr, code := invoke(t, c.name)
		if code != 2 {
			t.Errorf("bare %q: exit = %d, want 2 — a group must not run something\n%s", c.name, code, stderr)
		}
		// And it has to say what to type instead. Exiting 2 in silence would be the
		// same dead end with a different exit code.
		for _, s := range c.subs {
			if !strings.Contains(stderr, s.name) {
				t.Errorf("bare %q: stderr does not offer %q\n%s", c.name, s.name, stderr)
			}
		}
	}
}

// The verbs v0.1.0 shipped were renamed without an alias, so the one thing owed to
// somebody typing the old spelling is being told where it went. `gh`, whose command
// shape this borrows, does not do this — a renamed command there is simply unknown.
//
// Both directions are asserted, because a hint that fires too eagerly is its own bug:
// `frobnicate` was never a signpost command and must not be reported as one that moved.
func TestARenamedVerbSaysWhereItWent(t *testing.T) {
	for old, now := range moved {
		_, stderr, code := invoke(t, old, ".")
		if code != 2 {
			t.Errorf("%q: exit = %d, want 2", old, code)
		}
		if !strings.Contains(stderr, "signpost "+now) {
			t.Errorf("%q: stderr does not name `signpost %s`:\n%s", old, now, stderr)
		}
	}
	_, stderr, _ := invoke(t, "frobnicate")
	if strings.Contains(stderr, "is now") {
		t.Errorf("a command that never existed was reported as renamed:\n%s", stderr)
	}
}

// `signpost graph .` was the v0.1.0 spelling, and it is now a group handed a path. The
// literal truth — unknown command "." — sends the reader hunting for a typo they did
// not make, so a group that used to be a command says so and names the verb that
// inherited its behaviour.
//
// The negative half is what keeps this from swallowing everything: inside a group that
// carries the note, a genuine typo must still be treated as a typo.
func TestAGroupThatUsedToBeACommandSaysSo(t *testing.T) {
	var was []command
	for _, c := range commands() {
		if c.was != "" {
			was = append(was, c)
		}
	}
	if len(was) == 0 {
		t.Skip("no group carries a `was` note")
	}
	for _, c := range was {
		_, stderr, code := invoke(t, c.name, ".")
		if code != 2 {
			t.Errorf("%s .: exit = %d, want 2", c.name, code)
		}
		if !strings.Contains(stderr, "signpost "+c.name+" "+c.was) {
			t.Errorf("%s .: does not name `signpost %s %s`:\n%s", c.name, c.name, c.was, stderr)
		}
		if strings.Contains(stderr, `unknown command "."`) {
			t.Errorf("%s .: reported the path as an unknown command:\n%s", c.name, stderr)
		}
	}

	_, stderr, _ := invoke(t, "graph", "expot", ".")
	if !strings.Contains(stderr, "Did you mean") {
		t.Errorf("a typo inside a `was` group lost its suggestion:\n%s", stderr)
	}
}

// A typo gets one suggestion, and only when there is exactly one candidate close
// enough to be almost certainly right. The negative half is the half worth having: a
// suggester that always guesses is noise, and `xyzzy` resembles nothing here.
func TestATypoGetsOneSuggestion(t *testing.T) {
	_, stderr, code := invoke(t, "verfiy", ".")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "signpost verify") {
		t.Errorf("no suggestion for a one-transposition typo:\n%s", stderr)
	}

	_, stderr, _ = invoke(t, "xyzzy")
	if strings.Contains(stderr, "Did you mean") {
		t.Errorf("guessed at a name resembling nothing:\n%s", stderr)
	}
	// It still has to be usable, so with no guess to offer it falls back to listing
	// what does exist.
	if !strings.Contains(stderr, "build") {
		t.Errorf("no suggestion and no command list either:\n%s", stderr)
	}
}

// Suggestions are per level: a group's subcommand typo is compared against that
// group's children, not the top-level verbs.
func TestASuggestionIsScopedToItsLevel(t *testing.T) {
	_, stderr, code := invoke(t, "graph", "expot", ".")
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "signpost graph export") {
		t.Errorf("suggestion is not scoped to the group:\n%s", stderr)
	}
}

// `version`, `--version`, and `-v` are covered in version_test.go, where the build
// info they now report can be varied. They were asserted here against the bare
// `version` variable, which is no longer what the command prints.

// A path that does not exist is an error, not an empty graph. Reporting "0 nodes" for
// a typo'd path is the kind of quiet wrong answer that wastes an afternoon.
func TestMissingPathIsAnError(t *testing.T) {
	_, stderr, code := invoke(t, "graph", "show", filepath.Join(t.TempDir(), "nope"))
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
	stdout, stderr, code := invoke(t, "graph", "show", root)
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
	base, _, _ := invoke(t, "graph", "export", "--format", "json", "--quiet", root)
	ignored, _, code := invoke(t, "graph", "export", "--format", "json", "--quiet",
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
	with, _, _ := invoke(t, "graph", "export", "--quiet", root)
	without, _, code := invoke(t, "graph", "export", "--quiet", "--no-cluster", root)
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
