package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/okf"
)

// The exit code is verify's whole interface, so these tests assert it directly rather than
// asserting on internal/okf's VerifyResult — which is tested there. What has to hold at this
// level is that a stale bundle reaches the process as 1 and a clean one as 0, because the
// only consumer that matters is a CI job reading `$?`.

// A verify that exits zero because it had nothing to check is the failure design §4.6 names
// as worse than no check at all. So a missing bundle is a failure, not a pass.
func TestVerifyFailsWhenThereIsNoBundle(t *testing.T) {
	root := fixture(t)
	stdout, _, code := invoke(t, "verify", "--quiet", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a repository with no bundle\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "signpost build") {
		t.Errorf("the failure does not say how to fix it:\n%s", stdout)
	}
}

func TestVerifyPassesOnAFreshBuildAndSaysWhatItChecked(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("build exit = %d\n%s", code, stderr)
	}

	stdout, stderr, code := invoke(t, "verify", "--quiet", root)
	if code != 0 {
		t.Fatalf("exit = %d for a freshly built bundle\n%s\n%s", code, stdout, stderr)
	}
	// The counts are not decoration. "ok" over zero pages and "ok" over every page read the
	// same to a human scanning a CI log, and only one of them is a result.
	for _, want := range []string{"checked", "page(s)", "edge(s)", "prose link(s)", "ok"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "checked 0 page(s)") {
		t.Errorf("verify passed having opened no pages:\n%s", stdout)
	}
}

// The case the PR check exists for: the code moved and the bundle did not.
func TestVerifyFailsAfterTheCodeChanges(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("build exit = %d\n%s", code, stderr)
	}
	// A new package is a new concept, so the bundle is missing a page for it.
	full := filepath.Join(root, "internal", "billing", "billing.go")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("package billing\n\nfunc Charge() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := invoke(t, "verify", "--quiet", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1: the repository gained a module the bundle has no page "+
			"for\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "problem(s)") {
		t.Errorf("the failure does not say what was wrong:\n%s", stdout)
	}
}

// A human's notes must not fail the gate. This is the same property build guarantees, checked
// from the other side: a verify that went red on someone's paragraph is a verify they turn
// off, and then the staleness check is gone too.
func TestVerifyPassesWithHumanNotes(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("build exit = %d\n%s", code, stderr)
	}
	page := filepath.Join(root, okf.BundleDir, "modules", "auth.md")
	src, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	note := "\n## Why this is here\n\nRate limiting looks like it belongs here but does not.\n"
	if err := os.WriteFile(page, append(src, []byte(note)...), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := invoke(t, "verify", "--quiet", root)
	if code != 0 {
		t.Fatalf("exit = %d: a human's notes failed verification\n%s", code, stdout)
	}
}

// Exit 2, not 1. A CI job has to tell a mistyped invocation from a stale bundle: the first
// will fail identically forever, and the second is fixed by a build.
func TestVerifyRejectsTwoPathsWithAUsageCode(t *testing.T) {
	root := fixture(t)
	if _, _, code := invoke(t, "verify", root, root); code != 2 {
		t.Errorf("exit = %d, want 2 for a usage error", code)
	}
}

// gitFixture is the fixture in a repository with the code committed, which is what
// -as-of-bundle needs: the flag reads the commit the bundle records, and there is nothing to
// record without one.
//
// The environment is isolated the way hookRepo isolates it, and for the same measured reasons —
// a global core.hooksPath runs someone's secret scanner on every commit here, and
// maintenance.auto leaves a detached process holding handles under .git that block t.TempDir's
// cleanup on Windows.
func gitFixture(t *testing.T) string {
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

	root := fixture(t)
	git(t, root, "init", "-q", "--initial-branch=main")
	git(t, root, "config", "commit.gpgsign", "false")
	git(t, root, "config", "maintenance.auto", "false")
	git(t, root, "config", "gc.auto", "0")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "the code")
	return root
}

// commitAComment appends a line that changes no node, no edge, and no counted fact, and commits
// it. What a pull request looks like when it is not restructuring anything.
func commitAComment(t *testing.T, root string) {
	t.Helper()
	full := filepath.Join(root, "internal", "auth", "auth.go")
	src, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	note := []byte("\n// A note that changes nothing.\n")
	if err := os.WriteFile(full, append(src, note...), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "a comment")
}

// The end-to-end regression for the pull-request gate, and the reason -as-of-bundle needed more
// than a provenance exception.
//
// Seven churn attributes and the co-change edges are history-derived and land in page *content*,
// so before this every commit on a branch moved them: one commit adding a comment changed
// `commits` and `lines_added` on that directory's page, and the edge a second touched directory
// created moved the edge totals on index.md, log.md, and manifest.json. The gate went red on
// every pull request, including ones that changed no code — which is the failure design §4.6
// warns about arriving from the other direction: a check nobody can keep green is a check
// somebody turns off, and the staleness guarantee goes with it.
func TestVerifyAsOfBundlePassesAfterACommitThatChangesNoStructure(t *testing.T) {
	root := gitFixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("build exit = %d\n%s", code, stderr)
	}
	// Committed, because the bundle is a committed artifact (ADR 0005) and the commit it
	// records is the one before its own.
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "the bundle")
	commitAComment(t, root)

	stdout, stderr, code := invoke(t, "verify", "-as-of-bundle", root)
	if code != 0 {
		t.Fatalf("exit = %d: a commit that changed no structure failed the gate\n%s\n%s",
			code, stdout, stderr)
	}
	// The relaxation stayed the announced one. A pass that also stopped opening pages reads the
	// same from the exit code.
	if strings.Contains(stdout, "checked 0 page(s)") {
		t.Errorf("passed having opened no pages:\n%s", stdout)
	}
	// Which commit the churn numbers describe. A reader comparing a page's `commits` attribute
	// against `git log` on their branch would otherwise find it short by the branch's commits.
	if !strings.Contains(stderr, "history read as of") {
		t.Errorf("the run did not say it read history as of the bundle's commit:\n%s", stderr)
	}
}

// The same repository, verified strictly, must fail — otherwise the test above passes for a
// reason that has nothing to do with the flag.
func TestVerifyStrictStillFailsOnABranchCommit(t *testing.T) {
	root := gitFixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("build exit = %d\n%s", code, stderr)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "the bundle")
	commitAComment(t, root)

	stdout, _, code := invoke(t, "verify", "--quiet", root)
	if code != 1 {
		t.Fatalf("strict exit = %d, want 1: the default has to keep checking the stamp it "+
			"writes on the default branch\n%s", code, stdout)
	}
}

// addBilling writes and commits a package the bundle has no page for. The shape of most pull
// requests that touch the gate at all.
func addBilling(t *testing.T, root string) {
	t.Helper()
	full := filepath.Join(root, "internal", "billing", "billing.go")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("package billing\n\nfunc Charge() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "a new module")
}

// A branch that adds a module exits zero, prints every difference, and says nobody has to act.
//
// This asserted exit 1 until the gate had failed thirteen consecutive pull requests, every one of
// them for this reason and every one of them correctly. The remedy the failure named is
// `signpost build`, which §8.0 forbids on a branch: the bundle is written on the default branch
// only, so two branches cannot collide in it. A red gate whose instructions the author is not
// allowed to follow gets merged past as a habit, and the habit does not pause for the run where
// the bundle is really broken — so the severity had to follow the remedy. The counterpart that
// still fails is below, and the strict run on the same repository is
// TestVerifyFailsAfterTheCodeChanges.
func TestVerifyAsOfBundleReportsANewModuleAsPendingAndPasses(t *testing.T) {
	root := gitFixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("build exit = %d\n%s", code, stderr)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "the bundle")
	addBilling(t, root)

	stdout, _, code := invoke(t, "verify", "-as-of-bundle", "--quiet", root)
	if code != 0 {
		t.Fatalf("exit = %d: the branch cannot rebuild the bundle, so it cannot be asked to\n%s",
			code, stdout)
	}
	// Printed in full, never folded into a count and never dropped. "Nothing to do" is only
	// trustworthy if the reader can see what was set aside and disagree with it; a gate that
	// silently swallowed a page would be the false pass §4.6 forbids, arriving through the exit
	// code instead of the output.
	if !strings.Contains(stdout, "billing") {
		t.Errorf("the pass does not name the module that appeared:\n%s", stdout)
	}
	if !strings.Contains(stdout, "the merge will rebuild") {
		t.Errorf("the differences were not reported as the merge's work:\n%s", stdout)
	}
	// Deliberately not "the bundle matches this tree", which would be false. It does not match;
	// nothing on this branch is supposed to make it match.
	if strings.Contains(stdout, "matches this tree") {
		t.Errorf("claimed a match on a bundle that is a page short:\n%s", stdout)
	}
	if !strings.Contains(stdout, "rebuilt after this merges") {
		t.Errorf("the verdict does not say what resolves the differences:\n%s", stdout)
	}
}

// The hook prints a reminder for exactly what CI stays quiet about, and both read the same run.
//
// Not an exception to the severity — an application of it. Pending means "the rebuild after the
// merge resolves this", which is true on a branch and false on a laptop: there is no merge here and
// no push job, so `signpost build` is the remedy and the person reading the line is the one who
// runs it. The hook calls runVerify rather than reimplementing the comparison precisely so it
// cannot drift from the gate, which is what makes this worth pinning: the shared function now has
// to carry a distinction its two callers resolve in opposite directions.
func TestHookRemindsAboutWhatTheBranchGateDefers(t *testing.T) {
	root := gitFixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("build exit = %d\n%s", code, stderr)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-q", "-m", "the bundle")
	addBilling(t, root)

	// The gate's answer on this tree, asserted here as well so the two are compared rather than
	// assumed: green, because nothing the author can do resolves it.
	if _, _, code := invoke(t, "verify", "-as-of-bundle", "--quiet", root); code != 0 {
		t.Fatalf("the branch gate is not green on this tree, so the comparison below is not the "+
			"asymmetry it claims to be: exit = %d", code)
	}

	stdout, stderr, code := invoke(t, "hooks", "run", "-check", "verify", root)
	// Zero regardless: a post-commit hook that failed would reject a commit that already
	// happened. The reminder is the output, never the exit code.
	if code != 0 {
		t.Fatalf("exit = %d, want 0 from a post-commit reminder\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "signpost build") {
		t.Errorf("the hook stayed silent about a bundle this machine can rebuild:\nstdout:\n%s\n"+
			"stderr:\n%s", stdout, stderr)
	}
}

// The counterpart, and the whole reason the split is a distinction rather than a switch-off: a
// bundle that is broken *now* still exits 1 on a branch, because the merge inherits it rather
// than repairing it. Both cases here sit on a repository that *also* has a pending difference, so
// a classifier that keyed off the mode rather than the finding would pass this.
func TestVerifyAsOfBundleStillFailsOnABundleTheMergeCannotFix(t *testing.T) {
	for _, tc := range []struct {
		name    string
		want    string
		breakIt func(t *testing.T, root string)
	}{
		{
			// index.md keeps pointing at a path that is not there, in every checkout of this
			// branch and in every checkout after the merge.
			name: "a page the bundle links to deleted",
			want: "is not in the bundle",
			breakIt: func(t *testing.T, root string) {
				page := filepath.Join(root, okf.BundleDir, "modules", "auth.md")
				if err := os.Remove(page); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "frontmatter no conforming reader can parse",
			want: "not parseable YAML",
			breakIt: func(t *testing.T, root string) {
				page := filepath.Join(root, okf.BundleDir, "modules", "auth.md")
				src, err := os.ReadFile(page)
				if err != nil {
					t.Fatal(err)
				}
				out := strings.Replace(string(src), "type:", "type: [unterminated\nkey:", 1)
				if err := os.WriteFile(page, []byte(out), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := gitFixture(t)
			if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
				t.Fatalf("build exit = %d\n%s", code, stderr)
			}
			git(t, root, "add", "-A")
			git(t, root, "commit", "-q", "-m", "the bundle")
			addBilling(t, root)
			tc.breakIt(t, root)
			git(t, root, "add", "-A")
			git(t, root, "commit", "-q", "-m", "break the bundle")

			stdout, _, code := invoke(t, "verify", "-as-of-bundle", "--quiet", root)
			if code != 1 {
				t.Fatalf("exit = %d, want 1: no rebuild resolves this\n%s", code, stdout)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Errorf("the failure does not say %q:\n%s", tc.want, stdout)
			}
			// The pending difference is still reported alongside it. A failure that stopped
			// printing them would leave the author guessing which half to act on.
			if !strings.Contains(stdout, "the merge will rebuild") {
				t.Errorf("the deferred differences vanished once something else failed:\n%s", stdout)
			}
		})
	}
}

// A rewritten sha — what a squash merge or a rebase leaves behind — falls back to HEAD rather
// than failing. The recorded commit has no object in this clone while the content it describes
// is perfectly current, so refusing to read anything would break the gate on exactly the
// repositories that squash-merge, which is most of them.
func TestVerifyAsOfBundleFallsBackWhenTheRecordedCommitIsGone(t *testing.T) {
	root := gitFixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("build exit = %d\n%s", code, stderr)
	}
	// The manifest names a commit this repository has never had, which is what a rewritten
	// history leaves in a bundle built before the rewrite.
	man := filepath.Join(root, okf.BundleDir, okf.ManifestFile)
	src, err := os.ReadFile(man)
	if err != nil {
		t.Fatal(err)
	}
	out := regexp.MustCompile(`git://[0-9a-f]{40}`).
		ReplaceAllString(string(src), "git://"+strings.Repeat("a", 40))
	if out == string(src) {
		t.Fatalf("the resource was not rewritten, so this test proves nothing:\n%s", src)
	}
	if err := os.WriteFile(man, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := invoke(t, "verify", "-as-of-bundle", root)
	// It fails, because the pages still carry the real sha and the manifest now does not — that
	// is a genuine finding. What must not happen is a git error, or a claim to have read history
	// as of a commit that does not exist.
	if code == 2 {
		t.Fatalf("exit = 2: a rewritten sha became a fault rather than a fallback\n%s\n%s",
			stdout, stderr)
	}
	if strings.Contains(stderr, "history read as of") {
		t.Errorf("claimed to read history as of a commit this clone does not have:\n%s", stderr)
	}
	if !strings.Contains(stderr, "history:") {
		t.Errorf("history was not read at all:\n%s", stderr)
	}
}

func TestVerifyIsListedInTheUsage(t *testing.T) {
	stdout, _, code := invoke(t, "help")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "verify") {
		t.Errorf("usage does not list verify:\n%s", stdout)
	}
}
