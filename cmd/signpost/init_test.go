package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/scaffold"
)

// initRun drives the real CLI and asserts the exit code, which is the contract a script
// depends on: the blocked case in particular must be exit 0, since a scaffold that fails
// when the thing already exists is one every caller has to guard.
func initRun(t *testing.T, wantCode int, args ...string) (string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run(args, &out, &errOut); code != wantCode {
		t.Fatalf("signpost %s exited %d, want %d\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), code, wantCode, out.String(), errOut.String())
	}
	return out.String(), errOut.String()
}

// TestInitGitHubPrintsAndWritesNothing is the guarantee the command is built around. The
// file it produces requests `contents: write` and pushes to the default branch, so typing
// the command correctly must not be enough to install it.
func TestInitGitHubPrintsAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	out, errOut := initRun(t, 0, "init", "github", root)

	if errOut != "" {
		t.Errorf("stderr on the preview path: %s", errOut)
	}
	if !strings.Contains(out, "name: signpost") {
		t.Errorf("the preview does not contain the workflow:\n%s", out)
	}
	if !strings.Contains(out, "# "+scaffold.WorkflowPath) {
		t.Errorf("the preview does not say where the workflow goes:\n%s", out)
	}
	if !strings.Contains(out, "Not written") {
		t.Errorf("the preview does not say it wrote nothing:\n%s", out)
	}
	for _, rel := range []string{scaffold.WorkflowPath, scaffold.ConfigPath} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s was written by a command that only previewed", rel)
		}
	}
}

func TestInitGitHubWritesWithY(t *testing.T) {
	root := t.TempDir()
	out, _ := initRun(t, 0, "init", "github", "-y", "-repo", "example.com/org/repo", root)

	if !strings.Contains(out, "wrote "+scaffold.WorkflowPath) {
		t.Errorf("did not report writing the workflow:\n%s", out)
	}
	workflow := readFile(t, root, scaffold.WorkflowPath)
	if !strings.Contains(workflow, "name: signpost") {
		t.Errorf("the workflow is not a workflow:\n%s", workflow)
	}
	if !strings.Contains(readFile(t, root, scaffold.ConfigPath), "repo: example.com/org/repo") {
		t.Error("the config file does not carry the name that was asked for")
	}
	// Said plainly, because the next thing the user does is commit: a command that wrote
	// two files and left them unstaged should not let anyone assume otherwise.
	if !strings.Contains(out, "Nothing is committed here") {
		t.Errorf("does not say the files are uncommitted:\n%s", out)
	}
}

// TestInitGitHubDoesNotOverwrite is the same rule as the package-level test, asserted at
// the boundary a user actually hits, and it checks the two things that test cannot: exit 0,
// and a message naming the file.
func TestInitGitHubDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	mine := "# mine\n"
	writeAt(t, root, scaffold.WorkflowPath, mine)

	out, _ := initRun(t, 0, "init", "github", "-y", root)

	if !strings.Contains(out, scaffold.WorkflowPath+" is already here") {
		t.Errorf("does not name the file in the way:\n%s", out)
	}
	if got := readFile(t, root, scaffold.WorkflowPath); got != mine {
		t.Errorf("the existing workflow was replaced:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, scaffold.ConfigPath)); err == nil {
		t.Error("wrote the config file while the workflow blocked the plan, leaving the " +
			"repository half set up")
	}
}

// TestInitGitHubSaysWhenTheNameWasGuessed exists because the guess is the one thing in this
// command that can be wrong in a way the user cannot see. #31: a git remote is a property of
// the checkout, and a fork's remote names the upstream — so a derived name has to be
// flagged for checking, and an explicit one must not be.
func TestInitGitHubSaysWhenTheNameWasGuessed(t *testing.T) {
	root := t.TempDir()
	writeAt(t, root, ".git/config", "[remote \"origin\"]\n\turl = https://github.com/o/r.git\n")

	derived, _ := initRun(t, 0, "init", "github", "-y", root)
	if !strings.Contains(derived, "read from your `origin` remote") {
		t.Errorf("a derived name is not reported as derived:\n%s", derived)
	}
	if !strings.Contains(derived, "github.com/o/r") {
		t.Errorf("does not say what name it used:\n%s", derived)
	}

	root2 := t.TempDir()
	writeAt(t, root2, ".git/config", "[remote \"origin\"]\n\turl = https://github.com/o/r.git\n")
	explicit, _ := initRun(t, 0, "init", "github", "-y", "-repo", "example.com/asked", root2)
	if strings.Contains(explicit, "read from your `origin` remote") {
		t.Errorf("a name the user supplied is reported as a guess:\n%s", explicit)
	}
	if !strings.Contains(readFile(t, root2, scaffold.ConfigPath), "repo: example.com/asked") {
		t.Error("the explicit name did not win over the remote")
	}
}

// TestScaffoldedWorkflowMatchesTheOneThisRepositoryRuns is the test #33 called for, and it
// is the reason the template can be trusted at all.
//
// The scaffold ships advice about how to run signpost in CI. If it drifts from
// .github/workflows/signpost.yml, we are recommending a workflow we do not use — and the
// drift would show up as somebody else's gate behaving differently from ours, which is the
// hardest kind of bug to be told about.
//
// Compared on structure, not bytes: the two files genuinely differ in one respect, and must.
// This repository builds signpost from its own source, because a repository that analyses
// itself has to use the binary it currently contains; a scaffolded repository installs a
// pinned release. Everything else — the triggers, the loop guards, the two jobs' names and
// conditions, the permissions, the concurrency, and which verify is strict — is the design
// and must agree.
func TestScaffoldedWorkflowMatchesTheOneThisRepositoryRuns(t *testing.T) {
	ours := readRepoFile(t, ".github/workflows/signpost.yml")
	theirs := scaffoldedWorkflow(t)

	// Keys and values that encode a decision rather than a detail. Each is a line that,
	// if it differed, would mean the scaffolded gate behaves differently from ours.
	for _, want := range []string{
		"name: signpost",
		"    branches: [main]",
		`      - ".signpost/**"`, // loop guard 1
		"  pull_request:",        //
		"  contents: read",       // default-deny at the top
		"  group: signpost-${{ github.ref }}",
		"  cancel-in-progress: ${{ github.event_name == 'pull_request' }}",
		"    name: rebuild the bundle",
		"github.actor != 'github-actions[bot]'", // loop guard 2
		"      contents: write",                 // scoped to the build job only
		"          fetch-depth: 0",
		"    name: the bundle still describes this tree",
		"    if: github.event_name == 'pull_request'",
		"[skip ci]", // loop guard 3
		"git push --force-with-lease",
		"git diff --quiet --cached -- .signpost",
	} {
		if !strings.Contains(ours, want) {
			t.Fatalf("this test's expectations are stale: %q is no longer in "+
				".github/workflows/signpost.yml", want)
		}
		if !strings.Contains(theirs, want) {
			t.Errorf("the scaffolded workflow is missing %q, which this repository's own "+
				"workflow has", want)
		}
	}

	// The strictness split, which is the part most likely to be broken by a
	// well-intentioned edit: the push job verifies strictly and the pull-request job does
	// not. A scaffold with -as-of-bundle on both would ship a gate that cannot catch a
	// wrong stamp; without it on either, a gate that is red on every pull request.
	//
	// Counted over invocations rather than over the whole file, because both files explain
	// the split in prose and a substring count measures the comments — which is what this
	// assertion did on its first run, reporting four.
	verifies := regexp.MustCompile(`(?m)^\s*(?:\./)?signpost verify(.*)$`).
		FindAllStringSubmatch(commandLines(theirs), -1)
	var strict, asOf int
	for _, v := range verifies {
		if strings.Contains(v[1], "-as-of-bundle") {
			asOf++
		} else {
			strict++
		}
	}
	if strict != 1 || asOf != 1 {
		t.Errorf("the scaffolded workflow runs %d strict verify and %d -as-of-bundle "+
			"verify, want 1 of each: the push job writes the stamp so it must check it, "+
			"and the pull-request job must not or it is red on every pull request",
			strict, asOf)
	}

	// Every action pinned by sha, same rule this repository holds itself to. A tag has
	// been moved in a real supply-chain compromise, and this file is one we hand to
	// other people.
	for _, use := range regexp.MustCompile(`(?m)^\s*- uses: (\S+)`).FindAllStringSubmatch(theirs, -1) {
		if !regexp.MustCompile(`@[0-9a-f]{40}$`).MatchString(use[1]) {
			t.Errorf("scaffolded action %q is not pinned to a commit sha", use[1])
		}
	}

	// And the one difference, asserted rather than tolerated, so that a future edit
	// bringing `go build` into the template fails here with the reason.
	if strings.Contains(theirs, "go build -o signpost") {
		t.Error("the scaffolded workflow builds signpost from source; a repository that is " +
			"not this one has no signpost source to build and must install a release")
	}
	if !strings.Contains(theirs, "releases/download/") {
		t.Error("the scaffolded workflow does not install a signpost release")
	}
}

// TestScaffoldedWorkflowVerifiesWhatItDownloads is the assertion behind the one place this
// file reaches the network. The job that runs it holds `contents: write` and pushes to the
// default branch, so what it executes has to be the release it says it is.
//
// It deliberately does *not* pipe install.sh into a shell. That was the first version, and
// semgrep's gha-curl-pipe-shell was right about it: install.sh checks the archive's
// SHA-256, but the script doing the checking would itself have arrived unverified over the
// network. A checksum is worth nothing if the code comparing it is fetched the same way.
func TestScaffoldedWorkflowVerifiesWhatItDownloads(t *testing.T) {
	theirs := scaffoldedWorkflow(t)
	// Measured over commands, not over the file. This template explains both decisions in
	// prose, so it contains the strings "curl ... | sh" and "sha256sum -c" as comments
	// describing what it does and does not do — and asserting against the raw text failed
	// on the comments while the commands were right. Twice now in this file.
	commands := commandLines(theirs)

	if regexp.MustCompile(`curl[^\n|]*\|\s*(sh|bash)`).MatchString(commands) {
		t.Error("the scaffolded workflow pipes a downloaded script into a shell, in a job " +
			"that holds contents: write")
	}
	// Both installs verify, not just the first. The pull-request job is the gate, so a
	// binary of unknown provenance there decides whether other people's changes merge.
	if got := strings.Count(commands, "sha256sum -c"); got != 2 {
		t.Errorf("%d sha256 checks in the scaffolded workflow, want 2 (both jobs install "+
			"signpost, and both must verify what they install)", got)
	}
	if !strings.Contains(commands, "checksums.txt") {
		t.Error("nothing compares the download against the release's published checksums")
	}

	// The asset name is written out rather than detected, which is only correct while the
	// runner is the platform it names. These two lines are far apart in the file and there
	// is nothing about editing one that suggests the other, so a change to `runs-on` that
	// left the asset alone would download an amd64 binary onto an arm runner and fail with
	// "cannot execute binary file" — a message that says nothing about this workflow.
	if strings.Contains(commands, "_linux_amd64") {
		for _, on := range regexp.MustCompile(`(?m)^\s*runs-on: (\S+)`).FindAllStringSubmatch(commands, -1) {
			// The architecture is checked as well as the operating system, and separately,
			// because GitHub spells its arm runners `ubuntu-24.04-arm` — a prefix test for
			// `ubuntu-` passes that happily, which is how this assertion first let the
			// mutation through. Found by trying it.
			if !strings.HasPrefix(on[1], "ubuntu-") || strings.Contains(on[1], "arm") {
				t.Errorf("the scaffolded workflow runs on %q but downloads a linux_amd64 "+
					"asset; the hardcoded platform is only right while runs-on names a "+
					"linux/amd64 runner, and a mismatch fails as \"cannot execute binary "+
					"file\", which says nothing about this workflow", on[1])
			}
		}
	}
}

// TestScaffoldedWorkflowPinsTheVersionItInstalls guards the property that makes a
// scaffolded bundle reproducible: `@latest` would let the bundle's bytes change because
// signpost changed, on a day nothing in that repository did.
func TestScaffoldedWorkflowPinsTheVersionItInstalls(t *testing.T) {
	theirs := scaffoldedWorkflow(t)
	// Every assignment is matched, then each is required to be a tag — rather than
	// matching only the tag shape. A regexp that matched `v1.2.3` and nothing else made
	// `SIGNPOST_VERSION: latest` invisible to this test instead of failing it, which is
	// the one mutation it exists to catch. Found by trying it.
	found := regexp.MustCompile(`SIGNPOST_VERSION: (\S+)`).FindAllStringSubmatch(theirs, -1)
	if len(found) == 0 {
		t.Fatalf("no pinned SIGNPOST_VERSION in the scaffolded workflow:\n%s", theirs)
	}
	tag := regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	for _, m := range found {
		if !tag.MatchString(m[1]) {
			t.Errorf("SIGNPOST_VERSION is %q, not a release tag: a floating version lets "+
				"the bundle's bytes change because signpost changed, on a day nothing in "+
				"the repository did", m[1])
		}
	}
	// Both jobs install it, and they must agree: a pull request gated by one version
	// against a bundle written by another reports a difference that is neither branch's
	// fault.
	seen := map[string]bool{}
	for _, m := range found {
		seen[m[1]] = true
	}
	if len(seen) != 1 {
		var versions []string
		for v := range seen {
			versions = append(versions, v)
		}
		sort.Strings(versions)
		t.Errorf("the scaffolded workflow installs %v; the two jobs must run the same "+
			"version or a pull request is gated by a different build than wrote the bundle",
			versions)
	}
}

// TestInitPagesPrintsAndWritesNothing holds the preview-by-default guarantee for the
// command where it matters most: this one's output publishes the repository's structure at
// a URL, which is not undone by deleting the workflow afterwards.
func TestInitPagesPrintsAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	out, errOut := initRun(t, 0, "init", "pages", root)

	if errOut != "" {
		t.Errorf("stderr on the preview path: %s", errOut)
	}
	if !strings.Contains(out, "name: pages") {
		t.Errorf("the preview does not contain the workflow:\n%s", out)
	}
	if !strings.Contains(out, "# "+scaffold.PagesPath) {
		t.Errorf("the preview does not say where the workflow goes:\n%s", out)
	}
	if !strings.Contains(out, "Not written") {
		t.Errorf("the preview does not say it wrote nothing:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(scaffold.PagesPath))); err == nil {
		t.Errorf("%s was written by a command that only previewed", scaffold.PagesPath)
	}
}

// TestInitPagesWritesOnlyTheWorkflow pins what `init pages` is and is not. It writes no
// `.signpost.yml`: the deploy describes whatever tree it checks out, so there is no
// repository name for it to carry — and writing one would put this command in the business
// of a file `init github` owns, in a repository that may already have it.
func TestInitPagesWritesOnlyTheWorkflow(t *testing.T) {
	root := t.TempDir()
	out, _ := initRun(t, 0, "init", "pages", "-y", root)

	if !strings.Contains(out, "wrote "+scaffold.PagesPath) {
		t.Errorf("did not report writing the workflow:\n%s", out)
	}
	if !strings.Contains(readFile(t, root, scaffold.PagesPath), "name: pages") {
		t.Error("the written file is not the deploy workflow")
	}
	if _, err := os.Stat(filepath.Join(root, scaffold.ConfigPath)); err == nil {
		t.Errorf("%s was written by `init pages`, which has no name to put in it",
			scaffold.ConfigPath)
	}
}

// TestInitPagesSaysWhatPublishingCosts is the whole gate, and it is a message rather than a
// refusal for a reason worth stating in the test.
//
// #34 proposed reading repos/{owner}/{repo} and refusing when a private site cannot be
// confirmed. That is not what this does: `configure-pages` cannot enable Pages with
// GITHUB_TOKEN, so the scaffolded workflow publishes nothing until somebody changes a
// repository setting — the decision is made there, not here, and an API call would make
// `init` the only networked command (ADR 0028) while gating a step that was never the one
// that publishes. What is left is to make sure the person who runs this knows three things,
// so all three are asserted.
func TestInitPagesSaysWhatPublishingCosts(t *testing.T) {
	root := t.TempDir()
	written, _ := initRun(t, 0, "init", "pages", "-y", root)
	preview, _ := initRun(t, 0, "init", "pages", t.TempDir())
	help, _ := initRun(t, 0, "init", "pages", "-h")

	// Checked on all three paths. Somebody deciding whether to run this reads the help;
	// somebody who ran it reads the confirmation; and the preview is what gets piped
	// into a file and read later.
	//
	// Measured over prose with its line breaks collapsed, because the assertion is about
	// what the text says and every one of these strings is long enough to be wrapped. A
	// raw substring search makes rewrapping a paragraph a test failure, and the first
	// version of this test failed exactly that way — on a comment that did say "personal
	// account", with a newline and a `#` between the two words.
	for name, out := range map[string]string{
		"the -y output": prose(written),
		"the preview":   prose(preview),
		"the help":      prose(help),
	} {
		// 1. What is published. Not "the graph" — the specific thing, which is that every
		// file path and the names of who touched them leave the repository.
		if !strings.Contains(out, "file path") {
			t.Errorf("%s does not say file paths are published:\n%s", name, out)
		}
		// 2. That "private repository" does not imply "private site". This is the belief
		// somebody acts on, and it is wrong for exactly the common case: a personal
		// account.
		if !strings.Contains(out, "Enterprise Cloud") {
			t.Errorf("%s does not say a private site needs Enterprise Cloud:\n%s", name, out)
		}
		if !strings.Contains(strings.ToLower(out), "personal account") {
			t.Errorf("%s does not name the case where a private repository publishes a "+
				"public site:\n%s", name, out)
		}
		// 3. That nothing has happened yet, and where the decision is. A message that
		// warned without saying the workflow is inert would read as "too late".
		if !strings.Contains(out, "Settings") {
			t.Errorf("%s does not say Pages must be enabled in settings:\n%s", name, out)
		}
	}
}

func TestInitPagesDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	mine := "# my deploy\n"
	writeAt(t, root, scaffold.PagesPath, mine)

	out, _ := initRun(t, 0, "init", "pages", "-y", root)

	if !strings.Contains(out, scaffold.PagesPath+" is already here") {
		t.Errorf("does not name the file in the way:\n%s", out)
	}
	if got := readFile(t, root, scaffold.PagesPath); got != mine {
		t.Errorf("the existing workflow was replaced:\n%s", got)
	}
}

// TestScaffoldedPagesWorkflowMatchesTheOneThisRepositoryRuns is ADR 0028's parity rule
// applied to the second scaffold, and it is the same three properties: anchors asserted in
// both files, the intended differences asserted rather than tolerated, and assertions
// measured over comment-stripped command lines.
//
// The differences are real and there are three of them. This repository builds signpost
// from its own source; a scaffolded repository installs a pinned release. This repository
// publishes `site/` — hand-written pages that live here — while a scaffolded repository has
// no `site/` and gets the viewer written out of the binary. And the CNAME check is ours: a
// custom domain is this repository's choice, and #34 is explicit that a scaffolded copy
// should drop it rather than generalise it.
func TestScaffoldedPagesWorkflowMatchesTheOneThisRepositoryRuns(t *testing.T) {
	ours := readRepoFile(t, ".github/workflows/pages.yml")
	theirs := scaffoldedPages(t)

	for _, want := range []string{
		"name: pages",
		"    branches: [main]",
		"  workflow_dispatch:",
		"  contents: read", // and no contents: write anywhere; asserted below
		"  group: pages",
		"  cancel-in-progress: false",
		"      name: github-pages",
		"      pages: write",
		"      id-token: write",
		"          fetch-depth: 0",
		"actions/configure-pages@",
		"actions/upload-pages-artifact@",
		"actions/deploy-pages@",
		"not publishing an empty graph",
	} {
		if !strings.Contains(ours, want) {
			t.Fatalf("this test's expectations are stale: %q is no longer in "+
				".github/workflows/pages.yml", want)
		}
		if !strings.Contains(theirs, want) {
			t.Errorf("the scaffolded pages workflow is missing %q, which this repository's "+
				"own workflow has", want)
		}
	}

	commands := commandLines(theirs)

	// Nothing here writes to the repository. This is the property that makes the workflow
	// safe to hand somebody: `init github`'s file requests contents: write because it
	// commits a bundle, and a deploy that acquired the same permission would be a token
	// with push access in a job whose whole purpose is to publish.
	if strings.Contains(commands, "contents: write") {
		t.Error("the scaffolded pages workflow requests contents: write; a deploy publishes " +
			"and must not be able to push")
	}
	for _, forbidden := range []string{"git push", "git commit"} {
		if strings.Contains(commands, forbidden) {
			t.Errorf("the scaffolded pages workflow runs %q; it publishes, it does not write "+
				"to the repository", forbidden)
		}
	}

	// Every action pinned by sha, for the reason this repository pins its own: a tag is
	// mutable, and these run in a job that publishes to a URL.
	for _, use := range regexp.MustCompile(`(?m)^\s*- uses: (\S+)`).FindAllStringSubmatch(theirs, -1) {
		if !regexp.MustCompile(`@[0-9a-f]{40}$`).MatchString(use[1]) {
			t.Errorf("scaffolded action %q is not pinned to a commit sha", use[1])
		}
	}

	// The three intended differences, asserted so a future edit that copies one across
	// fails here with the reason rather than shipping.
	if strings.Contains(theirs, "go build -o signpost") {
		t.Error("the scaffolded pages workflow builds signpost from source; a repository " +
			"that is not this one has no signpost source to build")
	}
	if !strings.Contains(theirs, "releases/download/") {
		t.Error("the scaffolded pages workflow does not install a signpost release")
	}
	if strings.Contains(commands, "site/CNAME") || strings.Contains(theirs, "EXPECTED_DOMAIN") {
		t.Error("the scaffolded pages workflow checks a CNAME; the custom domain is this " +
			"repository's own, and a check for one nobody configured fails every deploy")
	}
	// And it must publish the written site rather than a directory in the checkout, which
	// is what our own workflow uploads. A scaffolded `path: site` would upload nothing —
	// or, worse, whatever a repository happens to keep in a directory called site.
	if strings.Contains(regexp.MustCompile(`(?s)upload-pages-artifact.*`).FindString(commands), "path: site\n") {
		t.Error("the scaffolded workflow uploads `site`, a directory a scaffolded repository " +
			"does not have; it must upload what `view -static` wrote")
	}
	if !strings.Contains(commands, "view -static") {
		t.Error("the scaffolded pages workflow does not write the viewer; nothing else in a " +
			"repository that is not this one can produce one")
	}
}

// TestScaffoldedPagesWorkflowVerifiesWhatItDownloads holds the same property for this
// template that `init github`'s test holds for that one. The reasoning is unchanged and the
// assertion is repeated rather than shared, because the two files are edited separately and
// a helper covering both would let a change to one be justified by the other.
func TestScaffoldedPagesWorkflowVerifiesWhatItDownloads(t *testing.T) {
	commands := commandLines(scaffoldedPages(t))

	if regexp.MustCompile(`curl[^\n|]*\|\s*(sh|bash)`).MatchString(commands) {
		t.Error("the scaffolded pages workflow pipes a downloaded script into a shell")
	}
	// One install here, not two: this workflow has a single job.
	if got := strings.Count(commands, "sha256sum -c"); got != 1 {
		t.Errorf("%d sha256 checks in the scaffolded pages workflow, want 1", got)
	}
	if !strings.Contains(commands, "checksums.txt") {
		t.Error("nothing compares the download against the release's published checksums")
	}
	if strings.Contains(commands, "_linux_amd64") {
		for _, on := range regexp.MustCompile(`(?m)^\s*runs-on: (\S+)`).FindAllStringSubmatch(commands, -1) {
			if !strings.HasPrefix(on[1], "ubuntu-") || strings.Contains(on[1], "arm") {
				t.Errorf("the scaffolded pages workflow runs on %q but downloads a linux_amd64 "+
					"asset", on[1])
			}
		}
	}
	// Pinned, same as the other template and for the same reason: a floating version means
	// the published graph can change on a day nothing in the repository did.
	found := regexp.MustCompile(`SIGNPOST_VERSION: (\S+)`).FindAllStringSubmatch(scaffoldedPages(t), -1)
	if len(found) != 1 {
		t.Fatalf("want exactly one pinned SIGNPOST_VERSION, found %d", len(found))
	}
	if !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(found[0][1]) {
		t.Errorf("SIGNPOST_VERSION is %q, not a release tag", found[0][1])
	}
}

// TestScaffoldedPagesWorkflowNamesTheVisibilityRule keeps the warning in the file, not only
// in the command's output. The output is read once, by whoever ran it; the file is read by
// everyone who reviews the pull request that adds it, and by whoever inherits the repository
// later.
func TestScaffoldedPagesWorkflowNamesTheVisibilityRule(t *testing.T) {
	theirs := prose(scaffoldedPages(t))
	for _, want := range []string{"Enterprise Cloud", "organization", "GITHUB_TOKEN"} {
		if !strings.Contains(theirs, want) {
			t.Errorf("the scaffolded pages workflow does not mention %q; the reviewer of the "+
				"pull request that adds this file is the last person who can catch it", want)
		}
	}
}

// prose collapses a message into one line so an assertion about what it says is not
// answered by where it was wrapped.
//
// Newlines become spaces, a `#` that starts a line goes with them, and runs of whitespace
// collapse. That makes "a personal\n# account" match "personal account", which is the same
// text a reader sees and the thing being asserted. It is the opposite job from
// commandLines: that one removes comments to find the commands, this one keeps the comments
// and removes the formatting.
func prose(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		b.WriteString(" ")
		b.WriteString(strings.TrimPrefix(strings.TrimSpace(line), "# "))
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// scaffoldedPages returns what `init pages` would write, read the way a user gets it.
func scaffoldedPages(t *testing.T) string {
	t.Helper()
	plan, err := scaffold.PlanPages(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range plan.Files {
		if f.Path == scaffold.PagesPath {
			return f.Contents
		}
	}
	t.Fatalf("no %s in the plan", scaffold.PagesPath)
	return ""
}

// commandLines strips YAML comments, so an assertion about what the workflow *does* is not
// answered by what it *says about itself*. Both files here are heavily commented by design,
// which makes a substring search over the raw text the wrong instrument.
//
// Only whole-line comments are removed. A trailing `# v7.0.1` after a pinned action is left
// alone, because taking it off would need to know when a `#` inside a quoted string is not a
// comment — and the assertions that care about trailing comments want the sha, not the note.
func commandLines(yaml string) string {
	var kept []string
	for _, line := range strings.Split(yaml, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// scaffoldedWorkflow returns what `init github` would write, read the way a user gets it.
func scaffoldedWorkflow(t *testing.T) string {
	t.Helper()
	plan, err := scaffold.PlanGitHub(t.TempDir(), "example.com/org/repo")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range plan.Files {
		if f.Path == scaffold.WorkflowPath {
			return f.Contents
		}
	}
	t.Fatalf("no %s in the plan", scaffold.WorkflowPath)
	return ""
}

// readRepoFile reads a file from the repository this test is part of, found by walking up
// from the working directory rather than assuming a depth — go test runs in the package
// directory, and the number of levels up is a detail that changes when a package moves.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return readAbs(t, filepath.Join(dir, filepath.FromSlash(rel)))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s; cannot find %s", dir, rel)
		}
		dir = parent
	}
}

func readAbs(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	return readAbs(t, filepath.Join(root, filepath.FromSlash(rel)))
}

func writeAt(t *testing.T, root, rel, contents string) {
	t.Helper()
	dest := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
