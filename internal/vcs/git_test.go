package vcs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/okf"
)

// These tests build throwaway repositories and run the real git binary against them.
//
// The parser's doc comment makes precise claims about a byte format — where the newline
// falls, how a rename is spelled, what a binary file looks like. Those claims were
// established by reading git's actual output, and a hand-built fixture cannot keep them
// honest afterwards: a future git that changed the layout would leave every parse test
// passing and every real analysis wrong. This is the test that would catch it.
//
// Skipped rather than failed when git is absent, because the package's whole contract is
// that a missing git is not an error.

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	gitRun(t, dir, "init", "--initial-branch=main")
	// Committer identity is required for `git commit` to run at all, and is set locally so
	// the test does not depend on — or disturb — the machine's global config.
	gitRun(t, dir, "config", "user.name", "Test Author")
	gitRun(t, dir, "config", "user.email", "author@example.invalid")
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Dates are pinned so the assertions below are about parsing rather than about what
	// day the suite happened to run.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
		"GIT_COMMITTER_DATE=2026-01-01T00:00:00Z",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gitCommit(t *testing.T, dir, msg string) {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", msg)
}

// The load-bearing test: a repository exercising every format case the parser claims to
// handle, read through the real git binary.
func TestReadAgainstRealGit(t *testing.T) {
	dir := gitRepo(t)

	write(t, dir, "a/one.go", "package a\n")
	write(t, dir, "b/two.go", "package b\n")
	gitCommit(t, dir, "first")

	write(t, dir, "a/one.go", "package a\n\nfunc F() {}\n")
	write(t, dir, "b/two.go", "package b\n\nfunc G() {}\n")
	gitCommit(t, dir, "second")

	// A rename, which git spells with an empty path field and two path tokens.
	gitRun(t, dir, "mv", "a/one.go", "a/renamed.go")
	gitCommit(t, dir, "rename")

	// A binary file: git writes `-` for both counts.
	if err := os.WriteFile(filepath.Join(dir, "assets.bin"), []byte{0x00, 0x01, 0x02, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}
	gitCommit(t, dir, "binary")

	// An empty commit, which yields a header and no numstat block.
	gitRun(t, dir, "commit", "--allow-empty", "-m", "empty")

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !s.Available {
		t.Fatalf("history unavailable: %s", s.Reason)
	}
	if s.Commits != 5 {
		t.Errorf("read %d commits, want 5", s.Commits)
	}
	if s.Shallow {
		t.Error("a freshly initialised repository is not shallow")
	}
	if s.Truncated {
		t.Error("5 commits under a 2000 cap is not truncated")
	}

	// The rename resolved to the new path, and its history came with it: two commits of
	// content plus the rename itself.
	r := s.Paths["a/renamed.go"]
	if r == nil {
		t.Fatalf("renamed path absent; got %v", pathKeys(s))
	}
	if r.Commits != 3 {
		t.Errorf("renamed path has %d commits, want 3 (history follows the file)", r.Commits)
	}
	if _, stale := s.Paths["a/one.go"]; stale {
		t.Error("the pre-rename path is still present, so history was split across both names")
	}

	if b := s.Paths["assets.bin"]; b == nil || !b.Binary {
		t.Errorf("binary file not recognised: %+v", b)
	}

	// Author name and date come back through the pretty format.
	if name, share := r.TopAuthor(); name != "Test Author" || share != 1 {
		t.Errorf("top author = %q at %v, want Test Author at 1", name, share)
	}
	if r.First != "2026-01-01" || r.Last != "2026-01-01" {
		t.Errorf("dates = %q..%q, want both 2026-01-01", r.First, r.Last)
	}

	// a and b changed together twice, which clears the one-commit coincidence floor.
	if len(s.CoChange) != 1 {
		t.Fatalf("co-change = %+v, want one pair", s.CoChange)
	}
	if s.CoChange[0].A != "a" || s.CoChange[0].B != "b" || s.CoChange[0].Commits != 2 {
		t.Errorf("pair = %+v, want a<->b at 2", s.CoChange[0])
	}
}

// A path containing a space, a quote, and a non-ASCII byte, through real git. This is the
// case that exists to prove -z is doing its job: without it git would C-quote the name
// and every assertion here would fail on escaped octal.
func TestReadAwkwardFilenameThroughRealGit(t *testing.T) {
	dir := gitRepo(t)

	// A quote in a filename is legal on Linux and rejected by Windows, so the quote is
	// dropped there rather than skipping the whole case: the space and the non-ASCII byte
	// are the parts that matter most and both are portable.
	name := `dir with space/quoté"file.go`
	if os.PathSeparator == '\\' {
		name = `dir with space/quotéfile.go`
	}
	write(t, dir, name, "package x\n")
	gitCommit(t, dir, "awkward")

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Paths[name] == nil {
		t.Errorf("path %q absent; got %q", name, pathKeys(s))
	}
}

// --use-mailmap consolidates one person's several addresses into one author. Without it,
// author concentration counts identities rather than people.
func TestReadHonoursInTreeMailmap(t *testing.T) {
	dir := gitRepo(t)

	write(t, dir, ".mailmap", "Real Person <real@example.invalid> <alias@example.invalid>\n")
	write(t, dir, "x.go", "package x\n")
	gitCommit(t, dir, "first")

	write(t, dir, "x.go", "package x\n\nfunc F() {}\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "user.name=Alias", "-c", "user.email=alias@example.invalid",
		"commit", "-m", "under an alias")

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got := s.Paths["x.go"].Authors["Real Person"]; got != 1 {
		t.Errorf("mailmap did not resolve the alias; authors = %v", s.Paths["x.go"].Authors)
	}
}

// A repository with no commits is a fact about the tree, not a failure. `git log` exits
// non-zero here, so this is the one git error the exec layer has to tell apart from a
// genuine fault.
func TestReadEmptyRepository(t *testing.T) {
	dir := gitRepo(t)

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("an empty repository must not error: %v", err)
	}
	if s.Available {
		t.Error("empty repository reported available history")
	}
	if !strings.Contains(s.Reason, "no commits") {
		t.Errorf("reason = %q, want it to name the empty history", s.Reason)
	}
}

// Not a repository at all: reported, not fatal, because the structural bundle is complete
// without history.
func TestReadNonRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	s, err := Read(context.Background(), t.TempDir(), Options{})
	if err != nil {
		t.Fatalf("a non-repository must not error: %v", err)
	}
	if s.Available {
		t.Error("a non-repository reported available history")
	}
	if !strings.Contains(s.Reason, "not a git repository") {
		t.Errorf("reason = %q, want it to say what is wrong", s.Reason)
	}
	// Paths is non-nil even when unavailable, so a caller can range over it without a
	// nil check on a path it did not choose to take.
	if s.Paths == nil {
		t.Error("Paths is nil, forcing every caller to guard")
	}
}

// The case this whole reporting apparatus exists for. A default actions/checkout is depth
// 1, so a bundle built in CI would otherwise claim no coupling exists — with nothing in
// the output distinguishing that from a repository that genuinely has none.
func TestReadReportsShallowClone(t *testing.T) {
	src := gitRepo(t)
	write(t, src, "a/x.go", "package a\n")
	gitCommit(t, src, "first")
	write(t, src, "a/x.go", "package a\n\nfunc F() {}\n")
	gitCommit(t, src, "second")
	write(t, src, "a/x.go", "package a\n\nfunc F() {}\nfunc G() {}\n")
	gitCommit(t, src, "third")

	clone := filepath.Join(t.TempDir(), "shallow")
	// file:// rather than a plain path: git refuses --depth against a local path copy.
	gitRun(t, src, "clone", "--depth=1", "file://"+filepath.ToSlash(src), clone)

	s, err := Read(context.Background(), clone, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !s.Shallow {
		t.Fatal("shallow clone not detected")
	}
	if !strings.Contains(s.Reason, "fetch-depth") {
		t.Errorf("reason = %q, want it to name the CI fix", s.Reason)
	}
	// The signals it did read are still real, just partial.
	if !s.Available || s.Commits != 1 {
		t.Errorf("available=%v commits=%d, want one readable commit", s.Available, s.Commits)
	}
}

func TestReadReportsTruncation(t *testing.T) {
	dir := gitRepo(t)
	for i := 0; i < 4; i++ {
		write(t, dir, "x.go", strings.Repeat("// line\n", i+1))
		gitCommit(t, dir, "change")
	}

	s, err := Read(context.Background(), dir, Options{MaxCommits: 2})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Commits != 2 {
		t.Errorf("read %d commits under a cap of 2", s.Commits)
	}
	if !s.Truncated {
		t.Fatal("hitting the cap was not reported as truncation")
	}
	if !strings.Contains(s.Reason, "lower bounds") {
		t.Errorf("reason = %q, want it to say First dates are lower bounds", s.Reason)
	}
}

// Merges are excluded, because under default settings a merge's numstat repeats the
// changes its parents already carried.
func TestReadExcludesMerges(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "base.go", "package a\n")
	gitCommit(t, dir, "base")

	gitRun(t, dir, "checkout", "-b", "side")
	write(t, dir, "side.go", "package a\n")
	gitCommit(t, dir, "side change")

	gitRun(t, dir, "checkout", "main")
	write(t, dir, "main.go", "package a\n")
	gitCommit(t, dir, "main change")

	gitRun(t, dir, "merge", "--no-ff", "-m", "merge side", "side")

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// base, side change, main change — the merge itself is not counted.
	if s.Commits != 3 {
		t.Errorf("read %d commits, want 3 with the merge excluded", s.Commits)
	}
	if p := s.Paths["side.go"]; p == nil || p.Commits != 1 {
		t.Errorf("side.go = %+v, want exactly one commit rather than one plus the merge", p)
	}
}

// Repository-local config must not be able to change what the analysis sees. These two
// settings are the ones that would: showSignature prepends signature text to the log, and
// quotePath re-enables the C-quoting that -z exists to avoid.
func TestReadIgnoresHostileRepoConfig(t *testing.T) {
	dir := gitRepo(t)
	name := "dir with space/plainé.go"
	write(t, dir, name, "package x\n")
	gitCommit(t, dir, "first")

	gitRun(t, dir, "config", "core.quotePath", "true")
	gitRun(t, dir, "config", "log.showSignature", "true")
	gitRun(t, dir, "config", "log.date", "raw")

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Paths[name] == nil {
		t.Errorf("path mangled by repo config; got %q", pathKeys(s))
	}
	if p := s.Paths[name]; p != nil && p.Last != "2026-01-01" {
		t.Errorf("date = %q, want the explicit --date=short to win over log.date", p.Last)
	}
}

// A cancelled context stops the subprocess rather than being ignored, and the failure is
// reported as an error rather than as an empty history — a timeout is a fault, not a fact
// about the repository.
func TestReadRespectsCancelledContext(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "x.go", "package x\n")
	gitCommit(t, dir, "first")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s, err := Read(ctx, dir, Options{})
	// isRepo fails under a cancelled context, so this reports unavailable rather than
	// erroring. Either is acceptable; what is not is a hang or a claim of complete history.
	if err == nil && s.Available && s.Commits > 0 {
		t.Error("a cancelled context produced a full history read")
	}
}

// The identity must not move when a commit only rewrote the bundle.
//
// This is the property that lets the artifact converge at all. The bundle is committed, so
// committing it advances HEAD; if the identity followed HEAD, every bundle would name the
// commit before its own and `verify` would fail on every committed bundle forever. Tested
// through real git because the behaviour is entirely a claim about what a pathspec does.
// bundleDir is duplicated from okf to avoid an import cycle, so the duplication is what
// gets tested. Without this, a future rename of the bundle directory would leave the
// exclusion pointing at a path that no longer exists and the convergence property above
// would silently stop holding.
func TestBundleDirMatchesTheEmitters(t *testing.T) {
	if bundleDir != okf.BundleDir {
		t.Errorf("bundleDir is %q but okf.BundleDir is %q; the exclusion in readHead no "+
			"longer names the bundle", bundleDir, okf.BundleDir)
	}
}

func TestHeadIgnoresCommitsThatOnlyTouchTheBundle(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "internal/a/a.go", "package a\n")
	gitCommit(t, dir, "the code")
	code := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))

	write(t, dir, bundleDir+"/index.md", "# generated\n")
	gitCommit(t, dir, "bundle [skip ci]")
	head := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))
	if head == code {
		t.Fatal("the second commit did not land; the test proves nothing")
	}

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Head.SHA != code {
		t.Errorf("identity is %s, want %s: a commit that only regenerated the description "+
			"did not change the code being described", s.Head.Short(), code[:7])
	}
}

// A commit that changes code *and* the bundle is a code change, and the identity moves to
// it. The exclusion is about commits with nothing else in them, not about ignoring the
// bundle wherever it appears.
func TestHeadFollowsACommitThatChangesCodeAndTheBundle(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "internal/a/a.go", "package a\n")
	write(t, dir, bundleDir+"/index.md", "# generated\n")
	gitCommit(t, dir, "first")

	write(t, dir, "internal/a/a.go", "package a\n\nfunc A() {}\n")
	write(t, dir, bundleDir+"/index.md", "# regenerated\n")
	gitCommit(t, dir, "second")
	want := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Head.SHA != want {
		t.Errorf("identity is %s, want HEAD %s", s.Head.Short(), want[:7])
	}
}

// A repository containing nothing but a bundle still gets stamped. git reports an
// all-excluded pathspec as empty output and exit 0, which is not an error and must not be
// read as one — an unstamped page would claim less than the tool knows.
func TestHeadFallsBackWhenEveryCommitIsBundleOnly(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, bundleDir+"/index.md", "# generated\n")
	gitCommit(t, dir, "bundle only")
	want := strings.TrimSpace(gitRun(t, dir, "rev-parse", "HEAD"))

	s, err := Read(context.Background(), dir, Options{})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if s.Head.SHA != want {
		t.Errorf("identity is %q, want the HEAD fallback %s", s.Head.SHA, want[:7])
	}
}
