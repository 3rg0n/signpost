// Package gitdiff produces a checkout of a commit that is not the working tree, and asks
// git which paths it considers renamed between two revisions.
//
// Both are things only git can answer, and both are needed by `signpost graph diff`
// ([ADR 0035]) and by nothing else. They live in their own package rather than in
// internal/vcs because vcs has a rule this does not follow: git there is an *optional*
// signal, and its absence is reported as a fact so the deterministic core stays complete
// without it (ADR 0020). A structural diff between two commits has no best-effort answer
// for a tree with no commits, so every function here fails loudly instead.
//
// [ADR 0035]: ../../docs/adr/0035-a-structural-diff-is-text-and-a-second-commit-is-a-worktree.md
package gitdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout bounds one git invocation.
//
// Generous, and for a different reason than vcs's identical default: the slow call here is
// a worktree checkout, which writes every file of the tree to disk. Measured at ~11s for
// 466 files, so the cap is for a repository an order of magnitude larger, not for this one.
const DefaultTimeout = 5 * time.Minute

// Worktree is a checkout of one revision in a temporary directory.
//
// Detached, so it takes no branch and cannot be confused with the user's own checkout; and
// under the system temp dir, so nothing this type creates is inside the repository being
// read. Close removes it.
type Worktree struct {
	// Dir is the checkout, which is what to hand the analysis pipeline.
	Dir string
	// Commit is the full sha the revision resolved to. Kept because the caller reports it:
	// `HEAD~5` is not a durable name for what was compared, and a diff whose output cannot
	// be tied back to two commits is not reproducible.
	Commit string

	repo string
}

// Add checks rev out into a new temporary worktree of the repository at repo.
//
// The revision is resolved to a full sha first, then the worktree is created from the sha
// rather than from the caller's spelling. Two reasons: the caller has to report what was
// actually compared, and `git worktree add` takes a commit-ish, so passing an unresolved
// `HEAD@{upstream}` or `:/subject` through would hand git revision syntax this package
// never validated. Resolving first means the only string that reaches the checkout is
// forty hex characters.
func Add(ctx context.Context, repo, rev string) (*Worktree, error) {
	if err := Available(ctx, repo); err != nil {
		return nil, err
	}
	sha, err := Resolve(ctx, repo, rev)
	if err != nil {
		return nil, err
	}
	// Named after signpost rather than left to MkdirTemp's default pattern, because a
	// worktree that outlives a killed process is visible in `git worktree list` for as
	// long as it takes somebody to notice, and a directory called `signpost-diff-*` says
	// which tool to blame.
	dir, err := os.MkdirTemp("", "signpost-diff-")
	if err != nil {
		return nil, fmt.Errorf("create a directory for the checkout: %w", err)
	}
	// The path git is given must not exist, and MkdirTemp just created it. Removed and
	// re-made by git, rather than passed --force, so a pre-existing directory somewhere
	// else can never be adopted as a worktree by accident.
	if err := os.Remove(dir); err != nil {
		return nil, fmt.Errorf("prepare the directory for the checkout: %w", err)
	}
	if _, err := run(ctx, repo, "worktree", "add", "--detach", "--quiet",
		"--no-checkout", "--end-of-options", dir, sha); err != nil {
		return nil, err
	}
	wt := &Worktree{Dir: dir, Commit: sha, repo: repo}
	// --no-checkout above, then checkout here, so a failure partway through still leaves a
	// registered worktree that Close knows how to remove. Creating and populating in one
	// call means a checkout that fails on one unwritable path leaves a half-populated
	// directory git has already registered, and no handle to clean it up with.
	if _, err := run(ctx, dir, "checkout", "--quiet", "--detach", "--end-of-options", sha); err != nil {
		_ = wt.Close()
		return nil, err
	}
	return wt, nil
}

// Close removes the worktree and the directory it was checked out into.
//
// Through git rather than os.RemoveAll, because the repository holds administrative
// files for every worktree it has registered under .git/worktrees: deleting only the
// checkout leaves that entry behind, and `git worktree list` then reports a path that is
// not there until somebody runs a prune. --force because the checkout is detached and git
// declines to remove a worktree it thinks might hold work.
func (w *Worktree) Close() error {
	if w == nil || w.Dir == "" {
		return nil
	}
	dir := w.Dir
	w.Dir = ""
	if _, err := run(context.Background(), w.repo, "worktree", "remove", "--force",
		"--end-of-options", dir); err == nil {
		return nil
	}
	// git declining is not the end of it. The directory is the caller's mess either way, and
	// leaving a full copy of a repository in the temp dir because a git subcommand failed is
	// worse than removing the files and letting a prune reclaim the administrative entry.
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove the temporary checkout at %s: %w", dir, err)
	}
	return nil
}

// Available reports whether repo can be diffed at all.
//
// Three separate failures with three separate messages, because the remedies are
// different: install git, run this somewhere else, or make a commit. A single "cannot
// read history" would be true and useless — which is the failure mode ADR 0027 names for
// a gate, and it applies to a command's own error just as much.
func Available(ctx context.Context, repo string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("git is not installed, and a structural diff between two commits " +
			"needs it: unlike the rest of signpost, this command has no answer for a tree " +
			"with no history")
	}
	out, err := run(ctx, repo, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(out) != "true" {
		return fmt.Errorf("%s is not a git repository, so there are no two commits to compare", repo)
	}
	if _, err := run(ctx, repo, "rev-parse", "--verify", "HEAD"); err != nil {
		return errors.New("this repository has no commits yet, so there is nothing to compare")
	}
	return nil
}

// Resolve turns a revision into the full sha it names.
//
// Any spelling git accepts is allowed — a branch, a tag, `HEAD~5`, a short sha — because
// this is a value the person typed on their own command line, about their own repository,
// and refusing `HEAD~1` in favour of forty hex characters would be a command nobody could
// use. That is a different position from vcs.validCommit, deliberately: the value it
// guards arrives from a committed manifest.json that anyone with a pull request can edit.
//
// --end-of-options keeps a revision that begins with a dash from being read as a flag, and
// `^{commit}` rejects a tree or a blob whose sha was pasted in by mistake rather than
// letting it fail later inside the checkout.
func Resolve(ctx context.Context, repo, rev string) (string, error) {
	if strings.TrimSpace(rev) == "" {
		return "", errors.New("no revision given")
	}
	out, err := run(ctx, repo, "rev-parse", "--verify", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%q does not name a commit in this repository", rev)
	}
	sha := strings.TrimSpace(out)
	if len(sha) != 40 {
		return "", fmt.Errorf("%q resolved to %q, which is not a commit sha", rev, sha)
	}
	return sha, nil
}

// Renames asks git which files it considers renamed between two revisions, and returns
// old path to new path.
//
// git rather than the graph, because the graph does not carry it and does not need to: a
// module node is its directory (ADR 0003), so a directory that moved is the set of file
// renames under it, and that set is a question about two revisions rather than a field on
// a node. -M is git's own similarity detection at its default threshold; asking for
// -M50% or --find-copies-harder would substitute a number nobody here can justify for
// git's, which every other tool a reader compares this against also uses.
func Renames(ctx context.Context, repo, from, to string) (map[string]string, error) {
	// -z, so a path containing a newline cannot end a record early. The format is then
	// NUL-separated with a quirk worth stating: a rename record is three fields — the
	// status, the old path, the new path — where every other status is two.
	out, err := run(ctx, repo, "diff", "--name-status", "-M", "-z", "--no-color",
		"--end-of-options", from, to)
	if err != nil {
		return nil, err
	}
	fields := strings.Split(out, "\x00")
	renames := map[string]string{}
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if f == "" {
			continue
		}
		// R with a similarity score, e.g. R082. C is a copy, which is not a rename: the
		// old path is still there, so the module it belongs to did not move.
		if f[0] != 'R' {
			// Every non-rename status carries one path, which is the next field.
			i++
			continue
		}
		if i+2 >= len(fields) {
			break
		}
		renames[fields[i+1]] = fields[i+2]
		i += 2
	}
	return renames, nil
}

// run invokes git in dir and returns stdout.
//
// The same hardening as vcs.run, and for the same reasons: config that could perturb the
// output is overridden per-invocation with -c, which beats .git/config where an
// environment variable would not; the two mailmap keys block an out-of-tree mailmap this
// process would otherwise read from a path of the repository's choosing; and a terminal
// prompt or a pager would hang a subprocess whose stdin is not a terminal.
//
// Not shared with vcs.run, which is a private function of a package whose Options type
// carries a commit and a commit cap this package has no use for. Exporting it to save
// twenty lines would make internal/vcs's git-is-optional contract reachable from a
// package built on git being required.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	full := append([]string{
		"-c", "log.showSignature=false",
		"-c", "core.quotePath=false",
		"-c", "mailmap.file=",
		"-c", "mailmap.blob=",
	}, args...)

	// #nosec G204 -- args are compile-time constants from this file plus a revision or a
	// path, each following --end-of-options so neither can be read as a flag. The
	// repository path is passed as Dir, never as an argument.
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"LC_ALL=C",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New("git " + args[0] + ": " + msg)
	}
	return stdout.String(), nil
}

// Root returns the top of the work tree containing dir.
//
// The path a caller typed may be a subdirectory, and every revision and rename in this
// package is expressed relative to the repository root. Resolving it here means the diff
// compares the same tree whichever directory the command was run from.
func Root(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(out)
	if root == "" {
		return "", errors.New("git did not report a work tree root")
	}
	return filepath.FromSlash(root), nil
}
