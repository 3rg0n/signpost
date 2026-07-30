package vcs

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
)

// Read runs git over the repository at dir and aggregates what its history says.
//
// It never returns an error for a repository whose history cannot be read. git may be
// absent, dir may not be a repository, the history may be empty, the clone may be
// shallow — all of those produce Available false or Shallow true with a Reason, because
// the deterministic core is complete without history (§4.4) and failing the whole
// analysis over a missing optional signal would be the wrong trade. An error is returned
// only when git is present, the directory is a repository, and the command still failed:
// that is a real fault worth surfacing rather than absorbing.
func Read(ctx context.Context, dir string, opts Options) (*Signals, error) {
	opts = opts.withDefaults()

	if _, err := exec.LookPath("git"); err != nil {
		return unavailable("git is not installed, so no history signals were read"), nil
	}
	if !isRepo(ctx, dir, opts) {
		return unavailable("not a git repository, so no history signals were read"), nil
	}

	out, err := run(ctx, dir, opts, logArgs(opts.MaxCommits)...)
	if err != nil {
		// An empty repository is the one git failure that is a fact about the tree
		// rather than a fault: `git log` on a repository with no commits exits
		// non-zero. Distinguished by asking whether HEAD resolves, so a genuine
		// failure is still reported.
		if !hasCommits(ctx, dir, opts) {
			return unavailable("the repository has no commits yet, so no history signals were read"), nil
		}
		return nil, err
	}

	commits := parseLog(out)
	s := aggregate(commits, opts)
	s.Truncated = len(commits) >= opts.MaxCommits

	if isShallow(ctx, dir, opts) {
		s.Shallow = true
		// Named as a CI problem specifically, because that is where it happens and
		// where nobody is watching: a default actions/checkout is depth 1, so a bundle
		// built in CI would silently claim no coupling exists. Saying "history is
		// truncated" without saying how to fix it would leave the reader with a warning
		// they cannot act on.
		s.Reason = "the clone is shallow, so co-change and churn cover only the commits present" +
			" (in CI, set fetch-depth: 0)"
	} else if s.Truncated {
		s.Reason = "history was capped at " + strconv.Itoa(opts.MaxCommits) +
			" commits, so first-seen dates are lower bounds"
	}
	return s, nil
}

// logArgs builds the git invocation.
//
// Every element is a compile-time constant except the numeric cap, and the repository
// path is not among them: it is passed as the subprocess's working directory instead.
// That is deliberate. A path is attacker-influenced input in a tool pointed at arbitrary
// directories, and a directory named `--output=...` interpolated into an argument list
// would be read by git as a flag. As the working directory it cannot be.
func logArgs(maxCommits int) []string {
	return []string{
		// No pager, no config that could redirect output, no signature verification.
		"--no-pager", "log",
		// Merges contribute no numstat of their own under default settings and would
		// double-count the changes they bring in.
		"--no-merges",
		"--numstat",
		// Follow renames, so a file's history survives being moved. Without this the
		// churn of a recently moved file reads as though the code were new.
		"--find-renames",
		// .mailmap consolidation, so one person committing under three addresses is one
		// author. Author concentration is meaningless without it.
		"--use-mailmap",
		// NUL-delimited: paths arrive raw rather than C-quoted. See parseLog.
		"-z",
		"--date=short",
		"--pretty=format:%H" + fieldSep + "%aN" + fieldSep + "%ad" + fieldSep,
		"--max-count=" + strconv.Itoa(maxCommits),
	}
}

// isRepo reports whether dir is inside a git work tree.
func isRepo(ctx context.Context, dir string, opts Options) bool {
	out, err := run(ctx, dir, opts, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// isShallow reports a shallow clone.
func isShallow(ctx context.Context, dir string, opts Options) bool {
	out, err := run(ctx, dir, opts, "rev-parse", "--is-shallow-repository")
	return err == nil && strings.TrimSpace(out) == "true"
}

// hasCommits reports whether HEAD resolves, which separates an empty repository from a
// genuine git failure.
func hasCommits(ctx context.Context, dir string, opts Options) bool {
	_, err := run(ctx, dir, opts, "rev-parse", "--verify", "HEAD")
	return err == nil
}

// run invokes git in dir with a timeout, returning stdout.
func run(ctx context.Context, dir string, opts Options, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	// Config that could perturb the output is overridden per-invocation, with -c
	// because it beats .git/config where an environment variable would not. Checked
	// against git rather than assumed: an explicit flag already wins over local config,
	// so --date and -z hold on their own, but log.showSignature prepends signature text
	// to the log and quotePath re-enables the C-quoting that -z exists to avoid.
	//
	// The two mailmap keys block an out-of-tree mailmap — a file or blob the repository
	// points at, which is untrusted input this process would otherwise read from a path
	// of the repository's choosing. The in-tree .mailmap is still honoured, verified by
	// test: that one is part of the tree being analysed and is the whole point of
	// --use-mailmap.
	full := append([]string{
		"-c", "log.showSignature=false",
		"-c", "core.quotePath=false",
		"-c", "mailmap.file=",
		"-c", "mailmap.blob=",
	}, args...)

	// #nosec G204 -- every argument is a compile-time constant from this file plus a
	// strconv'd integer. The repository path is passed as Dir, never as an argument, so
	// a directory named like a flag cannot become one. See logArgs.
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	// GIT_CONFIG_* suppress system and global config only; the repository's own config
	// is handled by the -c flags above. /dev/null is accepted as a path by git on
	// Windows as well, checked rather than assumed.
	cmd.Env = append(cmd.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		// No credential prompt and no pager: either would hang a subprocess whose
		// stdin is not a terminal, and a hung analysis is worse than a failed one.
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		// Message text is never parsed here, but a localised git would make any
		// diagnostic unreadable to the person reporting it.
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
		// args[0], not full[0]: the subcommand is what identifies the failure, and full
		// begins with the -c flags prepended above.
		return "", errors.New("git " + args[0] + ": " + msg)
	}
	return stdout.String(), nil
}
