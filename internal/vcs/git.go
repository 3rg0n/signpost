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

	// A commit named by the bundle is worth nothing if this clone does not have it, which
	// happens for an ordinary reason: a squash merge or a rebase leaves the recorded sha
	// with no object behind it while the content it describes is perfectly current. Asked
	// before the walk so the fallback is "read from HEAD, and say so" rather than a git
	// failure the caller has to interpret.
	asOf := opts.AsOf
	if asOf != "" && !hasCommit(ctx, dir, opts, asOf) {
		asOf = ""
	}

	out, err := run(ctx, dir, opts, logArgs(opts.MaxCommits, asOf)...)
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
	// Counted from the walk already in hand rather than by a second pass. The subjects are
	// classified and discarded here: nothing past this line has access to the message text.
	s.Conventions = countConventions(commits)
	// Asked for separately rather than taken from commits[0], which is neither HEAD nor
	// the commit being described: the walk passes --no-merges, so on a repository whose
	// tip is a merge the newest entry is one of its parents, and it counts bundle-only
	// commits that readHead deliberately looks past. See readHead for why that matters.
	s.Head = readHead(ctx, dir, opts, asOf)
	s.AsOf = asOf

	shallow := isShallow(ctx, dir, opts)
	// After the shallow check, because a shallow clone has no tags and readReleases must
	// report that as unknown rather than as "nothing is tagged". Read as of the same commit
	// the rest of the analysis describes, so the tag list on a branch verify is the one the
	// recorded commit had.
	s.Releases = readReleases(ctx, dir, opts, s.Head, shallow)

	if shallow {
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
// Every element is a compile-time constant except the numeric cap and, when a caller
// asked for one, a validated commit; the repository path is not among them: it is passed
// as the subprocess's working directory instead. That is deliberate. A path is
// attacker-influenced input in a tool pointed at arbitrary directories, and a directory
// named `--output=...` interpolated into an argument list would be read by git as a flag.
// As the working directory it cannot be.
//
// asOf reaches this function only through validCommit, so it is forty hex characters and
// cannot be a flag either. It is appended after a `--end-of-options` sentinel regardless,
// because the argument that is only safe by a caller's promise is the argument that stops
// being safe when someone adds a second caller.
func logArgs(maxCommits int, asOf string) []string {
	args := []string{
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
		// Field order is a correctness requirement, not a preference. The two fields whose
		// contents a repository controls — the author name and the subject — come last, and
		// the hash and date come first.
		//
		// git accepts a unit separator inside an author name (verified against git 2.51,
		// which takes any byte but NUL there), and a name containing one used to shift every
		// following field right: `git config user.name $'ev\x1fil'` made the *date* parse as
		// `il`, so a page's `first_commit` and `last_commit` silently became a fragment of
		// somebody's name. Ordering by trust does not stop the shift, it bounds what it can
		// reach: a separator in the author name now only corrupts the author name and the
		// subject, both of which are already free text, and the date field ahead of them is
		// out of reach. parseLog's field-count check does the rest.
		"--pretty=format:%H" + fieldSep + "%ad" + fieldSep + "%aN" + fieldSep + "%s" + fieldSep,
		"--max-count=" + strconv.Itoa(maxCommits),
	}
	if asOf != "" {
		args = append(args, "--end-of-options", asOf)
	}
	return args
}

// validCommit reports whether s is safe to hand git as a revision.
//
// A full hex sha and nothing else. The value arrives from a bundle's manifest.json, which
// is a committed file anyone with a pull request can edit, so it is untrusted input on its
// way to an argument list — and git's revision syntax is wide enough to be dangerous on its
// own terms: `HEAD@{upstream}`, a `:/text` search, or a path-shaped revision would all be
// accepted and none of them is what the caller meant. Requiring the one form signpost
// itself writes rejects every other reading without having to reason about which ones are
// harmful. Abbreviated shas are refused too: they are ambiguous by design, and the bundle
// always records a full one.
func validCommit(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// hasCommit reports whether dir has the named commit as a commit object.
//
// `^{commit}` rather than a bare --verify, which succeeds for a tree or a blob whose sha
// was pasted in by mistake and would then fail confusingly inside the log walk.
func hasCommit(ctx context.Context, dir string, opts Options, sha string) bool {
	if !validCommit(sha) {
		return false
	}
	_, err := run(ctx, dir, opts, "rev-parse", "--verify", "--end-of-options", sha+"^{commit}")
	return err == nil
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

// readHead resolves the commit the analysis describes.
//
// Not HEAD, when HEAD only rewrote generated output. The bundle is committed (ADR 0005),
// so committing it advances HEAD — and if the identity were HEAD, the bundle would name
// the commit before its own and `verify` would fail on every bundle ever committed,
// forever. That is not a hypothetical: it is what the workflow does on every push.
//
// So the identity is the newest commit that changed something other than the bundle. That
// is the right answer rather than a convenient one: the resource: field exists so a reader
// can tell whether a page still describes the code in front of them, and a commit that
// only regenerated the description did not change the code being described. Two commits
// with the same tree-outside-the-bundle get the same identity, which is exactly the
// property that makes the artifact converge.
//
// A failure here yields the zero Commit rather than an error. Every caller has already
// established that this is a repository with commits, so a failure is a git fault — but
// the identity is a provenance stamp on an otherwise complete analysis, and losing the
// whole bundle over an unstamped page would be the wrong trade. The emitter omits the
// field when it is empty rather than writing a hash it does not have.
//
// asOf, when set, is where the search starts instead of HEAD. It must already have been
// checked to exist — Read does that, so the caller here cannot turn a rewritten sha into an
// unstamped bundle.
func readHead(ctx context.Context, dir string, opts Options, asOf string) Commit {
	if c, ok := logOne(ctx, dir, opts, asOf, ".", ":(exclude)"+bundleDir); ok {
		return c
	}
	// Every commit touched only the bundle, which git reports as empty output and exit 0
	// rather than as an error. Falling back to HEAD keeps a repository that is nothing but
	// a bundle stamped, and the fallback cannot loop: there is no earlier commit to prefer.
	c, _ := logOne(ctx, dir, opts, asOf, ".")
	return c
}

// bundleDir is the path readHead excludes when deciding which commit is being described.
//
// Duplicated from internal/okf rather than imported: okf reads the graph this package
// feeds, so importing it here would be a cycle. A literal in one place with a test that
// fails if the two ever disagree is the cheaper of the two bad options.
const bundleDir = ".signpost"

// logOne reads one commit's hash and date under a pathspec, reporting whether git named
// one at all. Empty output is not a failure: it means no commit matched.
//
// rev, when set, bounds the search to that commit and its ancestors. It goes before the
// `--` where git expects a revision, and after `--end-of-options` for the reason logArgs
// gives.
//
// The sentinel is added only when there is a rev to protect, not unconditionally. It wants
// git 2.24, and every other argument here is a compile-time constant or a pathspec that
// already follows `--`, so spending a version requirement on them would buy nothing and
// would cost the stamp on an older git: this function reports failure by returning no
// commit, so a git that rejected the sentinel would produce an unstamped bundle rather than
// an error anyone could read.
func logOne(ctx context.Context, dir string, opts Options, rev string, pathspec ...string) (Commit, bool) {
	args := []string{"--no-pager", "log", "-1",
		"--date=short", "--pretty=format:%H" + fieldSep + "%ad"}
	if rev != "" {
		args = append(args, "--end-of-options", rev)
	}
	args = append(append(args, "--"), pathspec...)
	out, err := run(ctx, dir, opts, args...)
	if err != nil {
		return Commit{}, false
	}
	sha, date, ok := strings.Cut(strings.TrimSpace(out), fieldSep)
	if !ok {
		return Commit{}, false
	}
	return Commit{SHA: sha, Date: date}, true
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

	// #nosec G204 -- every argument is a compile-time constant from this file, plus a
	// strconv'd integer and, on the paths that take one, a commit that validCommit has
	// restricted to forty hex characters and that follows --end-of-options. The repository
	// path is passed as Dir, never as an argument, so a directory named like a flag cannot
	// become one. See logArgs.
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
