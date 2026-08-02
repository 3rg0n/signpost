package hook

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/okf"
)

// The literal this package keeps rather than importing, and the one it must agree with. A
// second spelling of the bundle directory would make the hook report on a directory nothing
// writes to, which is a silent no-op.
func TestBundleDirAgreesWithTheEmitter(t *testing.T) {
	if BundleDir != okf.BundleDir {
		t.Fatalf("BundleDir = %q, okf.BundleDir = %q", BundleDir, okf.BundleDir)
	}
}

// The git configuration these tests run under, set once for the process rather than per test
// with t.Setenv — which cannot be combined with t.Parallel, and parallelism is what keeps this
// package's tests bearable: every git invocation costs the better part of a second on Windows
// and there are a hundred of them.
//
// The isolation itself is not tidiness. This machine has a global core.hooksPath, and a test
// that inherited it would install a hook into the developer's real shared hooks directory.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("git"); err != nil {
		// Skipped rather than failed: git is a runtime dependency of the hook, not of the
		// build, and a machine without it should not fail the suite.
		fmt.Fprintln(os.Stderr, "git is not installed; skipping internal/hook")
		os.Exit(0)
	}
	home, err := os.MkdirTemp("", "signpost-hook-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for k, v := range map[string]string{
		// A home of its own, so ~/.gitconfig cannot reach in. GIT_CONFIG_GLOBAL alone would
		// do it for git's config, but git reads HOME for other things too and the two
		// disagreeing is a worse failure than either.
		"HOME":                home,
		"USERPROFILE":         home,
		"GIT_CONFIG_GLOBAL":   filepath.Join(home, "nonexistent-gitconfig"),
		"GIT_CONFIG_NOSYSTEM": "1",
		// An identity from the environment rather than two `git config` writes per repository.
		"GIT_AUTHOR_NAME":     "T",
		"GIT_AUTHOR_EMAIL":    "t@example.invalid",
		"GIT_COMMITTER_NAME":  "T",
		"GIT_COMMITTER_EMAIL": "t@example.invalid",
	} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// repo is an empty repository, ready for commits.
func repo(t *testing.T) string {
	t.Helper()
	t.Parallel()
	work := filepath.Join(t.TempDir(), "work")
	mustMkdir(t, work)
	run(t, work, "init", "-q")
	return work
}

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// commit makes a commit touching path, so the tests can move the two commits the fast check
// compares independently of each other.
func commit(t *testing.T, dir, path, content, msg string) {
	t.Helper()
	write(t, filepath.Join(dir, path), content)
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", msg)
}

func TestParseCheck(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Check
		ok   bool
	}{
		{"fast", CheckFast, true},
		{"verify", CheckVerify, true},
		// Case and surrounding space are a person typing, not a different mode.
		{"  VERIFY ", CheckVerify, true},
		{"Fast", CheckFast, true},
		// The negatives. A mode this does not know must be an error rather than a silent
		// fall back to fast: somebody who set `verifyy` asked for accuracy and would get
		// speed without being told.
		{"", "", false},
		{"quick", "", false},
		{"none", "", false},
		{"fast verify", "", false},
		{"fastest", "", false},
	} {
		got, err := ParseCheck(tc.in)
		if tc.ok {
			if err != nil {
				t.Errorf("ParseCheck(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("ParseCheck(%q) = %q, want %q", tc.in, got, tc.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("ParseCheck(%q) = %q, want an error", tc.in, got)
		}
	}
}

// The failure this guards is the one the git-lfs thread is about (git-lfs/git-lfs#3240): when
// core.hooksPath is set, .git/hooks is ignored completely, so installing there produces a file
// git will never run — which looks exactly like a successful install.
func TestResolveFollowsCoreHooksPath(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()

	p, err := Resolve(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if p.Redirected {
		t.Errorf("Redirected with no core.hooksPath set, scope %q", p.Scope)
	}
	if p.Shared {
		t.Error(".git/hooks reported as shared, but it is inside the repository")
	}
	if !strings.HasSuffix(filepath.ToSlash(p.PostCommit), "hooks/post-commit") {
		t.Errorf("PostCommit = %q", p.PostCommit)
	}

	// Redirected inside the repository: a committed hooks directory, which is a real and
	// reasonable setup and is not shared with anything.
	inside := filepath.Join(dir, ".githooks")
	mustMkdir(t, inside)
	run(t, dir, "config", "--local", "core.hooksPath", ".githooks")
	p, err = Resolve(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Redirected || p.Scope != "local" {
		t.Errorf("Redirected = %v, Scope = %q, want true/local", p.Redirected, p.Scope)
	}
	if p.Shared {
		t.Error("a hooks directory inside the repository was reported as shared")
	}
	if p.Dir != filepath.Clean(inside) {
		t.Errorf("Dir = %q, want %q", p.Dir, inside)
	}
}

// The case this host is actually in, and the one that decides whether install may write a
// whole file: a hooks directory outside the repository is shared by every repository on the
// machine.
func TestResolveReportsASharedHooksDirectory(t *testing.T) {
	dir := repo(t)
	outside := filepath.Join(t.TempDir(), "shared-hooks")
	mustMkdir(t, outside)
	run(t, dir, "config", "--local", "core.hooksPath", outside)

	p, err := Resolve(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Shared {
		t.Errorf("a hooks directory outside the repository was not reported as shared: %q", p.Dir)
	}
	if !p.Redirected {
		t.Error("Redirected = false with core.hooksPath set")
	}
}

func TestInstallCreatesTheHookWhenThereIsNone(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()

	res, err := Install(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.Appended || res.AlreadyPresent {
		t.Fatalf("created = %v, appended = %v, present = %v",
			res.Created, res.Appended, res.AlreadyPresent)
	}
	got := read(t, res.Path)
	if !strings.HasPrefix(got, "#!/bin/sh") {
		t.Errorf("no shebang:\n%s", got)
	}
	for _, want := range []string{beginMarker, endMarker, "signpost hooks run", BundleDir} {
		if !strings.Contains(got, want) {
			t.Errorf("hook does not contain %q:\n%s", want, got)
		}
	}
}

// Somebody else's hook must survive. git-lfs installs a post-commit on a great many machines,
// and clobbering it would break something the user depends on to gain something they merely
// opted into.
func TestInstallAppendsAndKeepsTheExistingHook(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	p, err := Resolve(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	// The real git-lfs hook, near enough, including the exit 2 that makes clobbering it a
	// visible breakage rather than a silent one.
	lfs := "#!/bin/sh\n" +
		"command -v git-lfs >/dev/null 2>&1 || { printf >&2 \"missing git-lfs\\n\"; exit 2; }\n" +
		"git lfs post-commit \"$@\"\n"
	write(t, p.PostCommit, lfs)

	res, err := Install(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Appended || res.Created {
		t.Fatalf("appended = %v, created = %v", res.Appended, res.Created)
	}
	got := read(t, p.PostCommit)
	if !strings.Contains(got, "git lfs post-commit") {
		t.Errorf("the existing hook was lost:\n%s", got)
	}
	if !strings.Contains(got, beginMarker) {
		t.Errorf("the block was not appended:\n%s", got)
	}
	if strings.Index(got, "git lfs post-commit") > strings.Index(got, beginMarker) {
		t.Errorf("the block was inserted before the existing hook:\n%s", got)
	}
}

// A hook whose last line has no newline. Appending straight onto it would join the block's
// first line to that command, silently changing what the existing hook runs.
func TestInstallDoesNotJoinOntoALastLineWithoutANewline(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	p, err := Resolve(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, p.PostCommit, "#!/bin/sh\necho hello")

	if _, err := Install(ctx, dir); err != nil {
		t.Fatal(err)
	}
	got := read(t, p.PostCommit)
	if !strings.Contains(got, "echo hello\n") {
		t.Errorf("the last line was altered:\n%s", got)
	}
	if strings.Contains(got, "echo hello"+beginMarker) {
		t.Errorf("the block was joined onto the last line:\n%s", got)
	}
}

func TestInstallIsIdempotent(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()

	if _, err := Install(ctx, dir); err != nil {
		t.Fatal(err)
	}
	res, err := Install(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadyPresent {
		t.Errorf("second install: present = %v, created = %v, appended = %v",
			res.AlreadyPresent, res.Created, res.Appended)
	}
	if n := strings.Count(read(t, res.Path), beginMarker); n != 1 {
		t.Errorf("%d blocks after installing twice, want 1", n)
	}
}

// Install must follow core.hooksPath, or it writes a file git never reads. Asserted by
// checking that .git/hooks is left alone, because a file in the right place *and* a file in
// the wrong place would pass a test that only looked at the right one.
func TestInstallWritesWhereGitLooks(t *testing.T) {
	dir := repo(t)
	elsewhere := filepath.Join(t.TempDir(), "shared-hooks")
	mustMkdir(t, elsewhere)
	run(t, dir, "config", "--local", "core.hooksPath", elsewhere)

	res, err := Install(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Dir(res.Path); got != filepath.Clean(elsewhere) {
		t.Errorf("installed to %q, want %q", got, elsewhere)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "hooks", "post-commit")); err == nil {
		t.Error("a hook was written to .git/hooks, which git ignores when core.hooksPath is set")
	}
}

func TestUninstallRemovesOnlyTheBlock(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	p, err := Resolve(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	other := "#!/bin/sh\ngit lfs post-commit \"$@\"\n"
	write(t, p.PostCommit, other)
	if _, err := Install(ctx, dir); err != nil {
		t.Fatal(err)
	}

	res, err := Uninstall(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Removed {
		t.Fatal("Removed = false")
	}
	if res.FileRemoved {
		t.Error("a file with somebody else's hook in it was deleted")
	}
	got := read(t, p.PostCommit)
	if strings.Contains(got, beginMarker) || strings.Contains(got, "signpost") {
		t.Errorf("signpost lines survived uninstall:\n%s", got)
	}
	if !strings.Contains(got, "git lfs post-commit") {
		t.Errorf("the other tool's hook was removed too:\n%s", got)
	}
}

// Our own file, with nothing else in it, should go rather than being left as an empty
// shebang that looks like a hook.
func TestUninstallRemovesAFileThatWasOnlyOurs(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	res, err := Install(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	path := res.Path

	un, err := Uninstall(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !un.Removed || !un.FileRemoved {
		t.Fatalf("removed = %v, fileRemoved = %v", un.Removed, un.FileRemoved)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("the hook file signpost created was left behind")
	}
}

// Uninstall on a repository that never had the hook, and on one whose hook is somebody
// else's. Neither is an error and neither may touch anything.
func TestUninstallIsQuietWhenThereIsNothingOfOurs(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()

	res, err := Uninstall(ctx, dir)
	if err != nil {
		t.Fatalf("uninstall with no hook at all: %v", err)
	}
	if res.Removed || res.FileRemoved {
		t.Errorf("removed = %v, fileRemoved = %v with no hook present", res.Removed, res.FileRemoved)
	}

	p, err := Resolve(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	other := "#!/bin/sh\necho not ours\n"
	write(t, p.PostCommit, other)
	res, err = Uninstall(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed {
		t.Error("Removed = true for a hook signpost never installed")
	}
	if got := read(t, p.PostCommit); got != other {
		t.Errorf("another tool's hook was modified:\ngot  %q\nwant %q", got, other)
	}
}

// Two blocks, which a version before the idempotence check could leave behind, or a hand
// pasted copy. Both must go: a half-cleaned file still runs the hook.
func TestUninstallRemovesEveryBlock(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	p, err := Resolve(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, p.PostCommit, "#!/bin/sh\necho keep\n\n"+Block()+"\necho also keep\n\n"+Block())

	if _, err := Uninstall(ctx, dir); err != nil {
		t.Fatal(err)
	}
	got := read(t, p.PostCommit)
	if strings.Contains(got, beginMarker) || strings.Contains(got, "hooks run") {
		t.Errorf("a block survived:\n%s", got)
	}
	for _, want := range []string{"echo keep", "echo also keep"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q was removed with the blocks:\n%s", want, got)
		}
	}
}

// An unterminated block — a hand-edited file that lost its end marker. Taking the rest of the
// file is the deliberate choice: leaving the begin marker would make the next install skip as
// though already installed, which is the one state that is neither installed nor removable.
func TestUninstallHandlesAnUnterminatedBlock(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	p, err := Resolve(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, p.PostCommit, "#!/bin/sh\necho keep\n"+beginMarker+"\nsignpost hooks run\n")

	if _, err := Uninstall(ctx, dir); err != nil {
		t.Fatal(err)
	}
	got := read(t, p.PostCommit)
	if strings.Contains(got, beginMarker) {
		t.Errorf("the begin marker survived, so a reinstall would skip:\n%s", got)
	}
	if !strings.Contains(got, "echo keep") {
		t.Errorf("content before the marker was lost:\n%s", got)
	}

	// And the state is recoverable: install works again afterwards.
	res, err := Install(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if res.AlreadyPresent {
		t.Error("install skipped after the block was stripped")
	}
}

// The block must be inert everywhere it is not wanted, since it may be appended to a hook
// shared by every repository on the machine.
func TestTheBlockIsGuarded(t *testing.T) {
	b := Block()
	for _, want := range []string{
		// No bundle, no output: this is what makes a machine-wide install defensible.
		"[ -d " + BundleDir + " ]",
		// No signpost on PATH, no error on every commit.
		"command -v signpost",
		// And it cannot fail, whatever happens inside.
		"|| true",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("the block does not contain %q:\n%s", want, b)
		}
	}
	// ASCII only. Git for Windows runs hooks through sh, and a non-ASCII byte in a file a
	// Windows user may open in an editor is the encoding trap the installers already hit.
	for i, r := range b {
		if r > 127 {
			t.Errorf("non-ASCII rune %q at byte %d", r, i)
		}
	}
	// POSIX sh, not bash. `[[` and `==` are the two that would work on a developer's machine
	// and fail on a runner with dash as /bin/sh.
	for _, bad := range []string{"[[", "==", "$(<", "local "} {
		if strings.Contains(b, bad) {
			t.Errorf("the block uses %q, which is not POSIX sh:\n%s", bad, b)
		}
	}
}

func TestFastReportsNothingWhenTheBundleIsCurrent(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()

	// Code and bundle in one commit: the single-developer flow ADR 0007 describes, and the
	// case a naive manifest-stamp comparison gets wrong.
	write(t, filepath.Join(dir, "a.go"), "package a\n")
	write(t, filepath.Join(dir, BundleDir, "index.md"), "# index\n")
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "code and bundle together")

	got, err := Fast(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stale() {
		t.Errorf("behind = %d for a bundle committed with the code (%s)", got.Behind, got.Reason)
	}
}

func TestFastReportsTheBundleFallingBehind(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	commit(t, dir, "a.go", "package a\n", "code")
	commit(t, dir, filepath.Join(BundleDir, "index.md"), "# index\n", "bundle")

	if got, err := Fast(ctx, dir); err != nil {
		t.Fatal(err)
	} else if got.Stale() {
		t.Fatalf("behind = %d right after the bundle was committed (%s)", got.Behind, got.Reason)
	}

	commit(t, dir, "b.go", "package b\n", "more code")
	got, err := Fast(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stale() {
		t.Fatalf("behind = 0 after a code commit the bundle does not cover (%s)", got.Reason)
	}
	if got.Behind != 1 {
		t.Errorf("behind = %d, want 1", got.Behind)
	}

	commit(t, dir, "c.go", "package c\n", "yet more code")
	got, err = Fast(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Behind != 2 {
		t.Errorf("behind = %d after two code commits, want 2", got.Behind)
	}

	// A commit that changed no file must not raise the count. Behind is a number of code
	// commits, not a number of commits, and the exclusion pathspec in the count is what makes
	// that true — it looks redundant, since nothing after the bundle commit can have touched
	// the bundle, and this is the case that shows it is not.
	run(t, dir, "commit", "-q", "--allow-empty", "-m", "no files")
	got, err = Fast(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Behind != 2 {
		t.Errorf("behind = %d after an empty commit, want 2 — it counted a commit that changed nothing", got.Behind)
	}
}

// The CI flow, and the direction that must not be reported as stale: the bot regenerates the
// bundle after the code lands, so the bundle commit is newer than the newest code commit.
func TestFastDoesNotReportABundleAheadOfTheCode(t *testing.T) {
	dir := repo(t)
	ctx := context.Background()
	commit(t, dir, "a.go", "package a\n", "code")
	commit(t, dir, "b.go", "package b\n", "more code")
	commit(t, dir, filepath.Join(BundleDir, "index.md"), "# index\n", "rebuild the bundle")

	got, err := Fast(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stale() {
		t.Errorf("behind = %d for a bundle newer than the code (%s)", got.Behind, got.Reason)
	}
}

// Every case where there is no answer must say so rather than printing nothing, because a
// hook that could not run and a hook with nothing to report look identical from a terminal.
func TestFastExplainsItselfWhenItCannotAnswer(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("not a repository", func(t *testing.T) {
		t.Parallel()
		got, err := Fast(ctx, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if got.Stale() || got.Reason == "" {
			t.Errorf("behind = %d, reason = %q", got.Behind, got.Reason)
		}
	})

	t.Run("no bundle", func(t *testing.T) {
		dir := repo(t)
		commit(t, dir, "a.go", "package a\n", "code")
		got, err := Fast(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		if got.Stale() {
			t.Errorf("behind = %d in a repository with no bundle", got.Behind)
		}
		if !strings.Contains(got.Reason, BundleDir) {
			t.Errorf("reason = %q, does not name the bundle directory", got.Reason)
		}
	})

	t.Run("bundle on disk but never committed", func(t *testing.T) {
		dir := repo(t)
		commit(t, dir, "a.go", "package a\n", "code")
		write(t, filepath.Join(dir, BundleDir, "index.md"), "# index\n")
		got, err := Fast(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		// Not stale: nothing has fallen behind, the bundle simply is not committed yet. A
		// "behind" here would be a nag with no correct action.
		if got.Stale() {
			t.Errorf("behind = %d for an uncommitted bundle", got.Behind)
		}
		if got.Reason == "" {
			t.Error("no reason given for an uncommitted bundle")
		}
	})

	t.Run("repository with no commits", func(t *testing.T) {
		dir := repo(t)
		write(t, filepath.Join(dir, BundleDir, "index.md"), "# index\n")
		got, err := Fast(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		if got.Stale() {
			t.Errorf("behind = %d in a repository with no commits", got.Behind)
		}
	})
}

// A repository that is nothing but a bundle. Nothing outside it has ever been committed, so
// there is nothing to be behind — and the count would otherwise be taken against an empty sha.
func TestFastHandlesARepositoryOfOnlyBundle(t *testing.T) {
	dir := repo(t)
	commit(t, dir, filepath.Join(BundleDir, "index.md"), "# index\n", "bundle only")

	got, err := Fast(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Stale() {
		t.Errorf("behind = %d in a bundle-only repository (%s)", got.Behind, got.Reason)
	}
}
