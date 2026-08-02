package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/config"
	"github.com/3rg0n/signpost/internal/model"
)

// These tests drive the real CLI, which is the only place ADR 0011's precedence exists: the
// config package reads a file, and applyConfig decides whether the file's value or the flag's
// wins. internal/config cannot test that, because the question is about flag.Visit.

// writeConfig puts a .signpost.yml at a repository root.
func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, config.File), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// vendoredFixture is fixture(t) plus a committed node_modules, so that whether vendored code
// was analysed is answerable from the output rather than inferred.
//
// The vendored package declares a dependency nothing else declares, which is what separates
// "the walk read the file" from "the analysis used it": the census counts files either way, and
// only a real reading puts the dependency in the graph. That is issue #11's failure mode, and
// it is the one this key can reintroduce.
func vendoredFixture(t *testing.T) string {
	t.Helper()
	root := fixture(t)
	files := map[string]string{
		"node_modules/@vendor/logger/package.json": `{"name":"@vendor/logger","version":"1.0.0",` +
			`"dependencies":{"vendored-only-dep":"^1.0.0"}}`,
		"node_modules/@vendor/logger/index.js": "export function log(m) { console.log(m); }\n",
	}
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// analysedVendored reports whether a `graph show` run put the vendored package on the map.
//
// The node id, not the path: the vendored files appear in the coverage census on stderr either
// way, and issue #11 was precisely a flag that moved the census and nothing else.
func analysedVendored(t *testing.T, args ...string) bool {
	t.Helper()
	stdout, stderr, code := invoke(t, args...)
	if code != 0 {
		t.Fatalf("exit = %d\nstderr:\n%s", code, stderr)
	}
	return strings.Contains(stdout, "/modules/logger")
}

// The key has an effect at all. Asserted through the graph rather than the file census,
// because the census moved even when issue #11 had every consumer filtering the files back out.
func TestConfigIncludeVendoredIsHonoured(t *testing.T) {
	root := vendoredFixture(t)

	// The negative boundary first: without the file, vendored code stays off the map. Without
	// this half, a reader that ignored the value and turned the key on for any file at all
	// would pass the positive.
	if analysedVendored(t, "graph", "show", root) {
		t.Error("vendored code is on the map with no config file asking for it")
	}

	writeConfig(t, root, "include_vendored: true\n")
	if !analysedVendored(t, "graph", "show", root) {
		t.Error("include_vendored: true did not reach the walk")
	}

	// And false in the file is false, not merely absent-shaped. A reader that treated any
	// present key as true would pass both halves above.
	writeConfig(t, root, "include_vendored: false\n")
	if analysedVendored(t, "graph", "show", root) {
		t.Error("include_vendored: false turned the option on")
	}
}

// ADR 0011's precedence, in the direction that is easy to get wrong and impossible to notice.
//
// A flag written as `-include-vendored=false` has the same value as an absent flag, so a
// reader comparing against the zero value cannot tell them apart — and would leave the file's
// `true` in place, silently ignoring the flag. flag.Visit is what distinguishes them, and this
// is the test that fails if somebody replaces it with a zero-value check.
func TestFlagBeatsConfigFile(t *testing.T) {
	root := vendoredFixture(t)
	writeConfig(t, root, "include_vendored: true\n")

	if analysedVendored(t, "graph", "show", "-include-vendored=false", root) {
		t.Error("-include-vendored=false did not override include_vendored: true in the file. " +
			"A flag explicitly set to the zero value must still win; comparing against the " +
			"zero value instead of flag.Visit cannot tell it from an absent flag")
	}
	// The other direction, which any implementation gets right, asserted so the pair reads as
	// a precedence rule rather than a special case.
	writeConfig(t, root, "include_vendored: false\n")
	if !analysedVendored(t, "graph", "show", "-include-vendored", root) {
		t.Error("-include-vendored did not override include_vendored: false in the file")
	}
}

// `ignore` replaces rather than appends, which is the one configurable key where that is a
// decision. A union would read naturally from the flag's "additional" help text and would make
// this the single key a caller cannot override.
func TestIgnoreFlagReplacesTheConfiguredPatterns(t *testing.T) {
	root := fixture(t)
	writeConfig(t, root, "ignore:\n  - internal/store/**\n")

	// The file's pattern applies: the store module is gone from the graph.
	if hasModule(t, root, "store", nil) {
		t.Error("the file's ignore pattern was not applied")
	}

	// A passed -ignore is the whole list, so the file's pattern stops applying and store
	// returns. This is the assertion that fails if applyConfig ever unions the two.
	flags := []string{"-ignore", "internal/auth/**"}
	if !hasModule(t, root, "store", flags) {
		t.Error("-ignore did not replace the file's patterns; the two were unioned, which " +
			"leaves no way to run without what the file configured")
	}
	if hasModule(t, root, "auth", flags) {
		t.Error("-ignore was not applied")
	}
}

// hasModule reports whether the graph contains a module node for name.
//
// The node id rather than a substring of the whole report: the module's own path appears in the
// coverage census on stderr whether or not it was analysed, so matching loosely would answer
// "was the file seen" when the question is "is it on the map".
func hasModule(t *testing.T, root, name string, flags []string) bool {
	t.Helper()
	args := append([]string{"graph", "show"}, flags...)
	stdout, stderr, code := invoke(t, append(args, root)...)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	return strings.Contains(stdout, "/modules/"+name)
}

func TestConfigNoHistoryAndMaxCommitsAreHonoured(t *testing.T) {
	root := fixture(t)

	// no_history changes what the coverage report says about history, which is the §4.2 line
	// that must distinguish "not read" from "read and empty".
	writeConfig(t, root, "no_history: true\n")
	_, stderr, code := invoke(t, "graph", "show", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "history not read") {
		t.Errorf("no_history: true did not reach the pipeline:\n%s", stderr)
	}

	// The negative boundary: false leaves history alone. The fixture is not a git repository,
	// so vcs reports its own reason — what matters is that it is not the -no-history one,
	// since that string is what a wrongly-defaulted key produces.
	writeConfig(t, root, "no_history: false\nmax_commits: 10\n")
	_, stderr, code = invoke(t, "graph", "show", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if strings.Contains(stderr, "(-no-history)") {
		t.Errorf("no_history: false skipped history anyway:\n%s", stderr)
	}
}

// -repo has to be settable, because it is the key ADR 0011's fourth force names: signpost's own
// CI passes it in five places across four workflows. And it has to be settable identically for
// build and verify, since a verify run with a different value reports a difference that
// describes the invocation rather than the bundle.
func TestConfigRepoReachesBuildAndVerify(t *testing.T) {
	root := fixture(t)
	writeConfig(t, root, "repo: example.com/org/from-file\n")

	if _, stderr, code := invoke(t, "build", "-quiet", root); code != 0 {
		t.Fatalf("build: exit = %d\n%s", code, stderr)
	}
	// The fixture has no git history, so no commit means no resource URI to check. What is
	// checkable is the pair: a verify that read the key the same way build did agrees with the
	// bundle build wrote. A verify that ignored it would render a different resource and
	// report the whole bundle as changed.
	if _, stderr, code := invoke(t, "verify", "-quiet", root); code != 0 {
		t.Fatalf("verify does not agree with the bundle build wrote from the same config: "+
			"exit = %d\n%s", code, stderr)
	}
}

// hooks.check, the key that was promised in `hooks run -h` before it existed.
func TestConfigHooksCheckIsHonoured(t *testing.T) {
	root := fixture(t)
	writeConfig(t, root, "hooks:\n  check: verify\n")

	// The fixture has no bundle, so the verify path reports that it could not check rather
	// than a staleness result — which is enough to tell the two modes apart, since the fast
	// path reports commit topology instead and never mentions a bundle it could not read.
	stdout, _, code := invoke(t, "hooks", "run", root)
	if code != 0 {
		t.Fatalf("hooks run exited %d, and it must always exit 0", code)
	}
	if !strings.Contains(stdout, "could not be checked") && !strings.Contains(stdout, "does not match") {
		t.Errorf("hooks.check: verify did not select the verify path:\n%s", stdout)
	}

	// The negative boundary: with fast configured, the verify path's message must be absent.
	// Without this, a hooks run that always took the verify path would pass the positive.
	writeConfig(t, root, "hooks:\n  check: fast\n")
	stdout, _, code = invoke(t, "hooks", "run", root)
	if code != 0 {
		t.Fatalf("hooks run exited %d", code)
	}
	if strings.Contains(stdout, "could not be checked") {
		t.Errorf("hooks.check: fast took the verify path anyway:\n%s", stdout)
	}
}

// The environment sits above the file, so a variable overrides the key. Asserted through the
// CLI because resolveCheck's own test cannot see whether the command passes the layers in the
// right order.
func TestEnvironmentBeatsConfigFileForHooksCheck(t *testing.T) {
	root := fixture(t)
	writeConfig(t, root, "hooks:\n  check: verify\n")
	t.Setenv("SIGNPOST_HOOK_CHECK", "fast")

	stdout, _, code := invoke(t, "hooks", "run", root)
	if code != 0 {
		t.Fatalf("hooks run exited %d", code)
	}
	if strings.Contains(stdout, "could not be checked") {
		t.Errorf("the file beat the environment, inverting ADR 0011's order:\n%s", stdout)
	}
}

// `backend` reaches the model layer, and the environment still outranks it.
//
// Asserted on `model check`, which is the command that exists to answer "why is my semantic
// pass doing nothing" — a backend key it ignored would make it answer that question wrongly.
func TestConfigBackendIsHonouredBelowTheEnvironment(t *testing.T) {
	// The environment has to be cleared: a developer machine with a Bedrock token set would
	// otherwise make the no-backend case reach a real endpoint.
	for _, v := range []string{
		model.EnvBackend, model.EnvModel, model.EnvBaseURL, model.EnvAPIKey, model.EnvBedrockToken,
	} {
		t.Setenv(v, "")
	}
	// model check takes no path, so the file has to be in the working directory. Chdir rather
	// than a flag, deliberately: ADR 0011 puts the file at the root and nowhere else, and a
	// -config flag is what that rule forbids.
	dir := t.TempDir()
	chdir(t, dir)

	// Nothing configured: the deterministic-only report, which is the negative boundary. A
	// reader that invented a backend would fail here.
	stdout, _, code := invoke(t, "model", "check")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "no backend is configured") {
		t.Errorf("a backend appeared from nowhere:\n%s", stdout)
	}

	// backend: openai with no base URL is a configuration error naming the variable to set,
	// which proves the key reached model.New without needing a reachable endpoint.
	writeConfig(t, dir, "backend: openai\n")
	_, stderr, code := invoke(t, "model", "check")
	if code == 0 {
		t.Error("backend: openai with no base URL was accepted")
	}
	if !strings.Contains(stderr, model.EnvBaseURL) {
		t.Errorf("the file's backend did not reach model.New:\n%s", stderr)
	}

	// And the environment overrides it back to none, which is the precedence boundary.
	t.Setenv(model.EnvBackend, "none")
	stdout, stderr, code = invoke(t, "model", "check")
	if code != 0 {
		// The likely cause, named: model.Config.withEnv treats a set field as explicit and lets
		// it beat the environment, so a file value placed in that field outranks the variable
		// and the openai path above is still selected. modelConfig resolves the layers before withEnv
		// sees them for exactly this reason.
		t.Fatalf("exit = %d with %s=none, so the file beat the environment (see modelConfig):\n"+
			"stdout:\n%s\nstderr:\n%s", code, model.EnvBackend, stdout, stderr)
	}
	if !strings.Contains(stdout, "no backend is configured") {
		t.Errorf("the file beat %s, inverting ADR 0011's order:\n%s", model.EnvBackend, stdout)
	}
}

// chdir moves to dir for the test and back afterwards.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prev); err != nil {
			t.Fatal(err)
		}
	})
}

// ADR 0011: a malformed file is exit 2, the usage category, and not a fallback to defaults.
// Exit 2 rather than 1 because CI has to tell a broken invocation from a broken repository,
// and every analysing command has to agree — a command that fell back would produce a
// differently-shaped bundle from a file somebody thought they had configured.
func TestMalformedConfigIsExitTwoOnEveryCommand(t *testing.T) {
	root := fixture(t)
	writeConfig(t, root, "include_vendored true\n")

	for _, args := range [][]string{
		{"build", "-quiet", root},
		{"verify", "-quiet", root},
		{"graph", "show", "-quiet", root},
		{"graph", "export", "-quiet", root},
		{"hooks", "run", root},
	} {
		name := strings.Join(args[:len(args)-1], " ")
		t.Run(name, func(t *testing.T) {
			stdout, stderr, code := invoke(t, args...)
			if code != 2 {
				t.Fatalf("exit = %d, want 2 for a malformed config\nstdout:\n%s\nstderr:\n%s",
					code, stdout, stderr)
			}
			if !strings.Contains(stderr, config.File) {
				t.Errorf("stderr does not name the file:\n%s", stderr)
			}
		})
	}
}

// A refused key fails the same way, and the message says why rather than reading as a typo.
// This is the CLI half of ADR 0011's second class: the reviewer looking at a diff that adds
// `as_of_bundle: true` sees CI fail, rather than seeing CI pass a quieter gate.
func TestGateWeakeningConfigFailsTheBuild(t *testing.T) {
	root := fixture(t)
	writeConfig(t, root, "as_of_bundle: true\n")

	_, stderr, code := invoke(t, "verify", "-quiet", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "as_of_bundle") {
		t.Errorf("stderr does not name the key:\n%s", stderr)
	}
	if !strings.Contains(stderr, "-as-of-bundle") {
		t.Errorf("stderr does not say where the decision lives instead:\n%s", stderr)
	}
}

// The file is repository content and is not exempted from the walk it configures. ADR 0011:
// signpost does not get to be invisible in its own map.
//
// Asserted on the census count rather than on a page, because a config file holding only
// `repo:` describes no module and no service and so earns no page of its own — the census is
// where a file that carries no concept still shows up. The temptation this guards against is
// concrete: `.signpost/` is excluded from the walk unconditionally, and adding `.signpost.yml`
// to that ignore set is a one-word change that looks consistent.
func TestConfigFileIsNotExcludedFromTheWalk(t *testing.T) {
	root := fixture(t)

	before := analysedFileCount(t, root)
	writeConfig(t, root, "repo: example.com/org/repo\n")
	after := analysedFileCount(t, root)

	if after != before+1 {
		t.Errorf("the census counted %d files before %s was added and %d after, want %d: the "+
			"file that shaped this analysis is invisible in it",
			before, config.File, after, before+1)
	}
}

// analysedFileCount reads the file count out of the coverage report on stderr.
func analysedFileCount(t *testing.T, root string) int {
	t.Helper()
	_, stderr, code := invoke(t, "graph", "show", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(stderr), "analysed %d files", &n); err != nil {
		t.Fatalf("the coverage report does not start with a file count: %v\n%s", err, stderr)
	}
	return n
}
