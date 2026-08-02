package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/hook"
)

// hookRepo is a git repository with a bundle directory and a commit, which is the state the
// hooks commands are used in. Isolated from the developer's git configuration for the same
// reason internal/hook's tests are: this machine has a global core.hooksPath, and inheriting
// it would install a hook into the developer's real shared hooks directory.
func hookRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "nonexistent-gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "T")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "T")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.invalid")
	// The hook's own mode must not be inherited from whatever ran the tests.
	t.Setenv(hook.EnvCheck, "")

	root := filepath.Join(home, "work")
	if err := os.MkdirAll(filepath.Join(root, hook.BundleDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, hook.BundleDir, "index.md"),
		[]byte("# index\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "init", "-q")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "code and bundle")
	return root
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestHooksInstallThenUninstall(t *testing.T) {
	root := hookRepo(t)
	path := filepath.Join(root, ".git", "hooks", "post-commit")

	stdout, stderr, code := invoke(t, "hooks", "install", root)
	if code != 0 {
		t.Fatalf("install: exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "installed") || !strings.Contains(stdout, "post-commit") {
		t.Errorf("install did not name what it did or where:\n%s", stdout)
	}
	// The path is the one fact a person needs in order to check or undo this by hand.
	if !strings.Contains(stdout, filepath.Base(path)) {
		t.Errorf("install output does not name the hook file:\n%s", stdout)
	}
	if !strings.Contains(stdout, "hooks uninstall") {
		t.Errorf("install does not say how to undo it:\n%s", stdout)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no hook was written: %v", err)
	}

	// Twice is not an error and does not duplicate.
	stdout, stderr, code = invoke(t, "hooks", "install", root)
	if code != 0 {
		t.Fatalf("second install: exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "already installed") {
		t.Errorf("second install did not say it was already there:\n%s", stdout)
	}

	stdout, stderr, code = invoke(t, "hooks", "uninstall", root)
	if code != 0 {
		t.Fatalf("uninstall: exit = %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "removed") {
		t.Errorf("uninstall did not say what it did:\n%s", stdout)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the hook file survived an uninstall that created it: %v", err)
	}

	// And uninstalling what is not there is a success, so a teardown script needs no guard.
	stdout, _, code = invoke(t, "hooks", "uninstall", root)
	if code != 0 {
		t.Errorf("uninstall with nothing installed: exit = %d, want 0\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "nothing to remove") {
		t.Errorf("second uninstall did not say there was nothing to do:\n%s", stdout)
	}
}

// The output a person on this machine's configuration will see, and the one the chosen design
// depends on: the hook went into a file shared by every repository on the machine, and they
// have to be told that before they discover it.
func TestHooksInstallWarnsAboutASharedHooksDirectory(t *testing.T) {
	root := hookRepo(t)
	shared := filepath.Join(t.TempDir(), "shared-hooks")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, root, "config", "--local", "core.hooksPath", shared)

	stdout, stderr, code := invoke(t, "hooks", "install", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	for _, want := range []string{
		// Where it went, which is not where a reader would look.
		"core.hooksPath",
		// That .git/hooks is not consulted at all — the git-lfs#3240 fact.
		".git/hooks",
		// Where the setting came from, so they can find and change it.
		"local",
		// That the file is not this repository's alone.
		"shared with every",
		// And why that is nonetheless safe.
		"no bundle",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("install output does not mention %q:\n%s", want, stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(shared, "post-commit")); err != nil {
		t.Errorf("the hook did not go where git reads: %v", err)
	}
}

// A repository with no core.hooksPath must not be told about one. The warning is the useful
// part of the output precisely because it is not always there.
func TestHooksInstallIsQuietWhenThereIsNoRedirection(t *testing.T) {
	root := hookRepo(t)
	stdout, _, code := invoke(t, "hooks", "install", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stdout)
	}
	if strings.Contains(stdout, "core.hooksPath") {
		t.Errorf("mentioned core.hooksPath in a repository that does not set it:\n%s", stdout)
	}
	if strings.Contains(stdout, "shared with every") {
		t.Errorf("called .git/hooks a shared directory:\n%s", stdout)
	}
}

// Silence when the bundle is current, which is every commit on a repository somebody keeps up
// to date. A hook that says "still fine" after each commit is one that gets uninstalled.
func TestHooksRunIsSilentWhenTheBundleIsCurrent(t *testing.T) {
	root := hookRepo(t)
	stdout, stderr, code := invoke(t, "hooks", "run", root)
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	if stdout != "" {
		t.Errorf("printed something for a current bundle:\n%s", stdout)
	}
}

func TestHooksRunReportsAStaleBundle(t *testing.T) {
	root := hookRepo(t)
	if err := os.WriteFile(filepath.Join(root, "b.go"), []byte("package b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "more code")

	stdout, stderr, code := invoke(t, "hooks", "run", root)
	// Exit 0 even when stale. This is the division the whole design rests on: the hook
	// reports, `signpost verify` in CI is what fails.
	if code != 0 {
		t.Fatalf("exit = %d, want 0 even when stale\n%s", code, stderr)
	}
	if !strings.Contains(stdout, hook.BundleDir) {
		t.Errorf("the message does not name the bundle directory:\n%s", stdout)
	}
	// The message has to say what to do. "Your bundle is stale" with no next step is a
	// notification rather than a reminder.
	if !strings.Contains(stdout, "signpost build") {
		t.Errorf("the message does not say what to run:\n%s", stdout)
	}
}

// The user's requirement: configurable, defaulting to fast. Each layer of the precedence is
// asserted, including that the flag beats the environment — which is the half a default of
// "fast" on the flag would silently break.
func TestResolveCheckPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flag  string
		env   string
		file  string
		want  hook.Check
		wantE bool
	}{
		{name: "nothing set is fast", want: hook.CheckFast},
		{name: "environment alone", env: "verify", want: hook.CheckVerify},
		{name: "flag alone", flag: "verify", want: hook.CheckVerify},
		{name: "file alone", file: "verify", want: hook.CheckVerify},
		// The flag wins, and it wins in the direction that is easy to get wrong: a flag
		// asking for the cheap check must not be overridden by an environment variable
		// asking for the expensive one.
		{name: "flag beats environment", flag: "fast", env: "verify", want: hook.CheckFast},
		{name: "flag beats environment the other way", flag: "verify", env: "fast", want: hook.CheckVerify},
		// The file is the bottom layer above the default, so both directions of both
		// boundaries are asserted. `env beats file` in the fast direction is the one that
		// would pass by coincidence if the layers were reversed and the default were fast.
		{name: "environment beats file", env: "fast", file: "verify", want: hook.CheckFast},
		{name: "environment beats file the other way", env: "verify", file: "fast", want: hook.CheckVerify},
		{name: "flag beats file", flag: "fast", file: "verify", want: hook.CheckFast},
		{name: "flag beats file and environment", flag: "fast", env: "verify", file: "verify", want: hook.CheckFast},
		// Blank is not a value. An exported-but-empty variable is the normal state of a
		// variable somebody unset, and it must not be an error.
		{name: "empty environment is not a value", env: "", want: hook.CheckFast},
		{name: "whitespace environment is not a value", env: "   ", want: hook.CheckFast},
		{name: "empty file value falls through to the default", file: "", want: hook.CheckFast},
		{name: "empty environment does not mask the file", env: "", file: "verify", want: hook.CheckVerify},
		// The negatives: a mode nobody recognises is an error, not a silent fall back to
		// fast, in every place it can arrive from.
		{name: "unknown flag", flag: "quick", wantE: true},
		{name: "unknown environment", env: "quick", wantE: true},
		// The file layer cannot normally hold a bad value — config.Load rejects one — so this
		// asserts the behaviour if it ever did: an error, not a fall back to fast.
		{name: "unknown file value", file: "quick", wantE: true},
		// And an unknown flag is still an error when a lower layer is valid, because the
		// flag is what the user typed most recently.
		{name: "unknown flag with a valid environment", flag: "quick", env: "fast", wantE: true},
		{name: "unknown flag with a valid file", flag: "quick", file: "fast", wantE: true},
		{name: "unknown environment with a valid file", env: "quick", file: "fast", wantE: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveCheck(tc.flag, tc.env, tc.file)
			if tc.wantE {
				if err == nil {
					t.Fatalf("resolveCheck(%q, %q, %q) = %q, want an error", tc.flag, tc.env, tc.file, got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("resolveCheck(%q, %q, %q) = %q, want %q",
					tc.flag, tc.env, tc.file, got, tc.want)
			}
		})
	}
}

// The environment variable has to reach the command, not just resolveCheck. Asserted through
// the CLI because that is the path the installed hook actually takes.
func TestHooksRunReadsTheEnvironmentVariable(t *testing.T) {
	root := hookRepo(t)
	t.Setenv(hook.EnvCheck, "nonsense")

	_, stderr, code := invoke(t, "hooks", "run", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for an unusable check mode\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "nonsense") {
		t.Errorf("stderr does not name the bad value:\n%s", stderr)
	}
}

func TestHooksRunRejectsAnUnknownCheck(t *testing.T) {
	root := hookRepo(t)
	_, stderr, code := invoke(t, "hooks", "run", "-check", "quick", root)
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, stderr)
	}
	// Naming the alternatives, because "unknown check mode" alone leaves the reader to guess
	// twice.
	for _, want := range []string{"quick", "fast", "verify"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not mention %q:\n%s", want, stderr)
		}
	}
}

// -check verify must reach the real verify rather than a second implementation of the same
// idea. Asserted on a repository whose bundle is a stub, so a real verify has plenty to
// disagree with and the fast check has nothing: the fast check compares commits and both
// were committed together, so only the accurate check can report anything here.
func TestHooksRunVerifyUsesTheRealVerify(t *testing.T) {
	root := hookRepo(t)

	if stdout, _, _ := invoke(t, "hooks", "run", "-check", "fast", root); stdout != "" {
		t.Fatalf("the fast check found something, so this test proves nothing:\n%s", stdout)
	}

	stdout, stderr, code := invoke(t, "hooks", "run", "-check", "verify", root)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 even when the bundle does not match\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "signpost build") {
		t.Errorf("the verify check did not report the mismatch a stub bundle must produce:\n"+
			"stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

// hooks is a group, so its bare name lists its subcommands rather than doing something
// (ADR 0012). TestAGroupNameIsNotItselfAnAction covers the rule; this covers the message.
func TestHooksWithNoSubcommandListsThem(t *testing.T) {
	_, stderr, code := invoke(t, "hooks")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\n%s", code, stderr)
	}
	for _, want := range []string{"install", "uninstall", "run"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("usage does not list %q:\n%s", want, stderr)
		}
	}
}

// The end-to-end assertion, and the one worth the most: a real `git commit` runs the installed
// hook, the hook's message reaches the terminal, and the commit succeeds anyway.
//
// Two things are verified here that no unit test can reach. That git actually executes the
// file — mode, shebang, and location all have to be right, and on Windows that runs through
// Git for Windows' sh. And that a hook printing a complaint does not fail the commit.
func TestTheInstalledHookRunsOnCommitAndCannotBlockIt(t *testing.T) {
	root := hookRepo(t)

	// The hook shells out to `signpost`, which is not on PATH in a test. A stub named
	// `signpost` on a PATH of our own is what makes the guard testable: it proves git ran the
	// file and the file called what it says it calls, without needing the real binary
	// installed. The block's `command -v signpost` guard means an absent binary is silence,
	// so a test that skipped this would pass on a hook that never ran.
	bin := t.TempDir()
	stub := filepath.Join(bin, "signpost")
	script := "#!/bin/sh\necho \"STUB CALLED: $*\" >&2\nexit 7\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	// Git for Windows resolves a hook's commands through sh, which uses PATH; a .exe is not
	// needed for `command -v` to find an extensionless script there.
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if _, stderr, code := invoke(t, "hooks", "install", root); code != 0 {
		t.Fatalf("install: exit = %d\n%s", code, stderr)
	}

	if err := os.WriteFile(filepath.Join(root, "c.go"), []byte("package c\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	// Not the git helper: this has to tolerate a nonzero exit in order to assert there is
	// none, and the helper fails the test instead.
	cmd := exec.Command("git", "commit", "-m", "with the hook installed")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the installed hook broke `git commit`: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "STUB CALLED") {
		t.Fatalf("git did not run the installed hook:\n%s", out)
	}
	// `hooks run` is the subcommand the block must call. A block that called `verify` would
	// gate the commit, which is the inversion this whole design is avoiding.
	if !strings.Contains(string(out), "hooks run") {
		t.Errorf("the hook did not call `signpost hooks run`:\n%s", out)
	}
	// The stub exits 7. The commit still has to exist, which is the `|| true` guard and
	// git's own indifference to a post-commit status, together.
	if n := git(t, root, "rev-list", "--count", "HEAD"); n != "2" {
		t.Errorf("commit count = %s, want 2 — the commit did not happen", n)
	}

	// And in a repository with no bundle the same shared hook must stay silent, which is the
	// guard that makes appending to a machine-wide file defensible.
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, other, "init", "-q")
	// Point it at the same hooks directory, the way a global core.hooksPath does.
	git(t, other, "config", "--local", "core.hooksPath", filepath.Join(root, ".git", "hooks"))
	if err := os.WriteFile(filepath.Join(other, "x.txt"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, other, "add", "-A")
	cmd = exec.Command("git", "commit", "-m", "no bundle here")
	cmd.Dir = other
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the shared hook broke a commit in an unrelated repository: %v\n%s", err, out)
	}
	if strings.Contains(string(out), "STUB CALLED") {
		t.Errorf("the hook ran signpost in a repository with no bundle:\n%s", out)
	}
}
