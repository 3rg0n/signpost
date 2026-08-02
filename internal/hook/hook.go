// Package hook installs, removes, and runs signpost's optional post-commit hook.
//
// The hook is a convenience and nothing more. It prints one line when the committed bundle
// has fallen behind the code, and CI is what actually gates (design §4.6, §8). That
// division is the whole design, and it is stated here because the two halves look similar
// enough to be "fixed" into each other by a later reader:
//
//   - The hook never fails a commit. It reports and exits 0 on its own failures, because a
//     hook that breaks `git commit` over an optional knowledge artifact gets deleted within
//     a day and takes the tool with it. This is not design line 1304's prohibition on "a
//     hook that exits zero on failure" — that forbids masking a *verification* failure as
//     success in the gate, and the gate is CI. A hook that printed nothing and returned 1
//     would fail nothing and annoy everybody.
//   - It is post-commit, never pre-commit. §8.0 is explicit that the bundle is not built on
//     branches, because two branches both regenerating .signpost/ is what makes merges
//     painful. A pre-commit rebuild recreates exactly that, on every commit, on every
//     branch. post-commit runs after the commit object exists, so it cannot block work and
//     cannot amend what was committed.
//
// git ignores a post-commit hook's exit code entirely — verified, not assumed — so the
// first point is about the message rather than the status: nothing this package does can
// stop a commit, and the reason to exit 0 anyway is that a nonzero status from a hook shows
// up in some porcelain and in people's shells for no reason.
package hook

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// BundleDir is the directory whose staleness the hook reports.
//
// Duplicated from internal/okf rather than imported, the same trade internal/vcs makes for
// the same reason: this package is reached from the CLI alongside okf, and the hook script
// needs the name as a shell literal rather than a Go constant. A test asserts the two agree.
const BundleDir = ".signpost"

// Check is what the hook does when it runs.
type Check string

const (
	// CheckFast compares commits and never analyses. Two `git log -1` invocations, so it
	// costs milliseconds and cannot become the reason somebody uninstalls the hook.
	CheckFast Check = "fast"
	// CheckVerify runs `signpost verify -as-of-bundle`, which reports the pages that would
	// actually change rather than inferring from commit topology. Accurate and roughly a
	// second on a repository this size, which is why it is not the default.
	CheckVerify Check = "verify"
)

// ParseCheck reads a check mode, naming the alternatives on failure.
func ParseCheck(s string) (Check, error) {
	switch c := Check(strings.ToLower(strings.TrimSpace(s))); c {
	case CheckFast, CheckVerify:
		return c, nil
	}
	return "", fmt.Errorf("unknown check mode %q; want %s or %s", s, CheckFast, CheckVerify)
}

// EnvCheck overrides the check mode for one invocation of the hook.
//
// An environment variable rather than a config file, for now: ADR 0011 puts a flag and the
// environment above `.signpost.yml` in precedence, so `hooks.check` can be read from the
// file later without changing anything here or in the installed script.
const EnvCheck = "SIGNPOST_HOOK_CHECK"

// Paths says where a repository's hooks live and how git decided that.
type Paths struct {
	// Dir is the directory git will actually read hooks from, absolute.
	Dir string
	// PostCommit is the post-commit hook's path inside Dir.
	PostCommit string
	// Redirected reports that core.hooksPath is set, which means .git/hooks is ignored
	// entirely — git looks in exactly one place and this is it.
	Redirected bool
	// Shared reports that Dir is outside this repository, so the file governs every
	// repository on the machine. The reason install writes a guarded block rather than a
	// file: on a machine with a global core.hooksPath, that path usually already holds
	// somebody else's hook. git-lfs's is the common one.
	Shared bool
	// Scope is where core.hooksPath was set — "global", "system", "local", or "" when it is
	// not set at all. Reported so a person who did not know they had one can find it.
	Scope string
}

// Resolve asks git where hooks live rather than assuming .git/hooks.
//
// This is the one thing an installer of this kind must not get wrong. When core.hooksPath is
// set — at any scope, including global — that path is the *only* place git looks and
// .git/hooks is ignored completely, so a tool that writes to .git/hooks anyway installs a
// file that will never run. That is worse than not installing, because it looks like it
// worked. git-lfs settled this in git-lfs/git-lfs#3240: it installs to the resolved path
// whatever the scope, on the grounds that otherwise it would not work at all, and the
// documented escape hatch for a user who dislikes that is to set core.hooksPath locally.
//
// `git rev-parse --git-path hooks` is the answer to both questions at once: it applies
// core.hooksPath when set and falls back to the git directory when not, so this never has to
// reimplement git's precedence.
//
// Two subprocesses, not six. On Windows a git invocation costs the better part of a second, so
// the three rev-parse questions go in one command — they are answered in argument order — and
// the scope comes from `--show-scope` rather than from asking each scope in turn.
func Resolve(ctx context.Context, dir string) (*Paths, error) {
	out, err := git(ctx, dir, "rev-parse",
		"--absolute-git-dir", "--show-toplevel", "--git-path", "hooks")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		return nil, fmt.Errorf("git rev-parse returned %d lines, want 3: %q", len(lines), out)
	}
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	gitDir, top, hooks := lines[0], lines[1], lines[2]
	// --git-path returns a relative path when it is inside the repository, and the process
	// running this is not necessarily at the repository root.
	if !filepath.IsAbs(hooks) {
		hooks = filepath.Join(top, hooks)
	}

	p := &Paths{
		Dir:        filepath.Clean(hooks),
		PostCommit: filepath.Join(filepath.Clean(hooks), "post-commit"),
		Scope:      hooksPathScope(ctx, dir),
	}
	p.Redirected = p.Scope != ""
	// Resolved on both sides, because git resolves one and not the other. `--show-toplevel`
	// comes back with symlinks expanded; an absolute `core.hooksPath` comes back exactly as
	// it was written. So a hooks directory inside a repository reached through a symlink —
	// /tmp on macOS is one, and a junction to a checkout is another — compares unequal to a
	// toplevel naming the same place, and would be reported as shared with every repository
	// on the machine. Dir itself is left as git reported it: that is the path git will use
	// and the path the person set, and rewriting it in the install output would answer a
	// question nobody asked.
	shared := resolve(p.Dir)
	p.Shared = !within(resolve(top), shared) && !within(resolve(gitDir), shared)
	return p, nil
}

// resolve expands symlinks, falling back to a cleaned path when it cannot.
//
// The fallback is the case that matters: the hooks directory frequently does not exist yet,
// and EvalSymlinks fails outright on a path that is not there. An unresolvable path compares
// as itself, which is the behaviour this had before and is right for a directory install is
// about to create.
func resolve(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(r)
	}
	return filepath.Clean(path)
}

// hooksPathScope reports where core.hooksPath was set, or "" if it was not.
//
// The scope, not just the value: the value alone does not say whether it came from this
// repository or from the user's ~/.gitconfig, and that distinction is exactly what the person
// reading the install output needs in order to find the setting and decide whether they meant
// it. `--show-scope` prints `<scope>\t<value>` for the setting that actually won, so it also
// answers the multiple-scopes case that asking each scope in turn gets wrong.
func hooksPathScope(ctx context.Context, dir string) string {
	out, err := git(ctx, dir, "config", "--show-scope", "--get", "core.hooksPath")
	if err != nil || out == "" {
		// Exit 1 with no output is git saying the key is unset, which is not an error here.
		return ""
	}
	scope, value, ok := strings.Cut(out, "\t")
	if !ok || strings.TrimSpace(value) == "" {
		return ""
	}
	return scope
}

// within reports whether path is base or sits underneath it.
func within(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// The markers that make the block removable. Written as full-line comments so that a person
// reading a shared post-commit hook can see what owns those lines and how to get rid of
// them, and so that uninstall can take out exactly what install put in.
const (
	beginMarker = "# >>> signpost >>>"
	endMarker   = "# <<< signpost <<<"
)

// Block is the text install appends to a post-commit hook.
//
// Three guards, each earning its place on a file that may be shared by every repository on
// the machine:
//
//   - `[ -d .signpost ]` — the block does nothing in a repository that has no bundle. This is
//     what makes appending to a machine-wide hook defensible: somebody else's repositories
//     carry on exactly as before.
//   - `command -v signpost` — a contributor who has not installed the tool gets silence, not
//     an error on every commit. The hook may also be reached through a committed shared
//     directory on a machine where signpost was later removed.
//   - `|| true` — the hook cannot fail. git ignores a post-commit exit code, but the shell
//     under `set -e` in a wrapping script does not, and a nonzero status leaks into places
//     nobody wants it.
//
// POSIX sh only, and ASCII only. Git for Windows runs hooks through sh, so one script covers
// all three platforms — but it must not assume GNU coreutils, and anything a Windows user may
// open in an editor stays ASCII for the same encoding reason the installers already fixed.
func Block() string {
	return beginMarker + "\n" +
		"# Reports when " + BundleDir + "/ has fallen behind the code. Never fails a commit;\n" +
		"# CI is the gate. Remove with `signpost hooks uninstall`.\n" +
		"[ -d " + BundleDir + " ] && command -v signpost >/dev/null 2>&1 && signpost hooks run || true\n" +
		endMarker + "\n"
}

// Script is a whole post-commit hook, for the case where there is no file to append to.
func Script() string {
	return "#!/bin/sh\n" +
		"# Installed by `signpost hooks install`.\n" +
		Block()
}

// InstallResult says what Install did, so the CLI can explain it rather than print "ok".
type InstallResult struct {
	Path string
	// Created reports that the hook file did not exist and was written whole.
	Created bool
	// Appended reports that a hook was already there and the block went on the end.
	Appended bool
	// AlreadyPresent reports that the block was there, so nothing was written. Install is
	// idempotent: running it twice must not leave two blocks behind.
	AlreadyPresent bool
	Paths          *Paths
}

// Install adds the block to the repository's post-commit hook.
//
// It appends rather than replacing, always. A post-commit hook already exists on a great
// many machines — git-lfs installs one — and clobbering it would break something the user
// depends on to gain something they merely opted into.
func Install(ctx context.Context, dir string) (*InstallResult, error) {
	p, err := Resolve(ctx, dir)
	if err != nil {
		return nil, err
	}
	res := &InstallResult{Path: p.PostCommit, Paths: p}

	existing, err := os.ReadFile(p.PostCommit)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// 0o750, not 0o755: git runs hooks as the user who ran git, so nothing outside the
		// owner and their group ever needs to traverse this directory. The hook *file* is a
		// different matter — see below.
		if err := os.MkdirAll(p.Dir, 0o750); err != nil {
			return nil, err
		}
		// 0o755: a hook git cannot execute is a hook that does not run. On Windows the mode
		// is mostly advisory, and Git for Windows runs the file through sh regardless.
		// #nosec G306 -- an executable hook must be executable.
		if err := os.WriteFile(p.PostCommit, []byte(Script()), 0o755); err != nil {
			return nil, err
		}
		res.Created = true
		return res, nil
	case err != nil:
		return nil, err
	}

	if strings.Contains(string(existing), beginMarker) {
		res.AlreadyPresent = true
		return res, nil
	}

	// A trailing newline first when the file lacks one, or the block's first line would be
	// appended to whatever the last line was — which on a hook that ends in a command means
	// silently changing that command.
	var b strings.Builder
	b.Write(existing)
	if !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(Block())
	if err := replace(p.PostCommit, b.String()); err != nil {
		return nil, err
	}
	res.Appended = true
	return res, nil
}

// UninstallResult says what Uninstall did.
type UninstallResult struct {
	Path string
	// Removed reports that the block was found and taken out.
	Removed bool
	// FileRemoved reports that nothing but signpost's own block was left, so the hook file
	// went too. Leaving an empty `#!/bin/sh` behind would be litter that looks like a hook.
	FileRemoved bool
	Paths       *Paths
}

// Uninstall removes exactly the block install added, and nothing else.
//
// The whole point of the markers. On a shared post-commit hook the other lines belong to
// somebody else — git-lfs, most often — and an uninstall that removed the file would break
// their tool to clean up ours.
func Uninstall(ctx context.Context, dir string) (*UninstallResult, error) {
	p, err := Resolve(ctx, dir)
	if err != nil {
		return nil, err
	}
	res := &UninstallResult{Path: p.PostCommit, Paths: p}

	existing, err := os.ReadFile(p.PostCommit)
	if errors.Is(err, os.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return nil, err
	}

	stripped, found := strip(string(existing))
	if !found {
		return res, nil
	}
	res.Removed = true

	// Only a shebang and blank lines left: this file is ours and nothing else's.
	if onlyShebang(stripped) {
		if err := os.Remove(p.PostCommit); err != nil {
			return nil, err
		}
		res.FileRemoved = true
		return res, nil
	}
	if err := replace(p.PostCommit, stripped); err != nil {
		return nil, err
	}
	return res, nil
}

// strip removes every marked block, reporting whether it found one.
//
// Every block rather than the first, so that a file which somehow collected two — an install
// from a version before the idempotence check, a hand-pasted copy — comes out clean instead of
// half-cleaned. An unterminated block takes the rest of the file with it: the alternative is
// leaving a begin marker that the next install would treat as an existing block and skip,
// which is the state where the hook is neither installed nor removable.
func strip(s string) (string, bool) {
	var b strings.Builder
	found := false
	skipping := false
	for _, line := range strings.SplitAfter(s, "\n") {
		t := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		switch {
		case t == beginMarker:
			skipping, found = true, true
		case t == endMarker && skipping:
			skipping = false
		case !skipping:
			b.WriteString(line)
		}
	}
	out := strings.TrimRight(b.String(), "\n")
	if out != "" {
		out += "\n"
	}
	return out, found
}

// onlyShebang reports a file with no content beyond a shebang, a comment, or blank lines.
func onlyShebang(s string) bool {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		return false
	}
	return true
}

// replace writes content over an existing file through a temp file and a rename, so an
// interrupted uninstall cannot leave a half-written hook — which on a shared post-commit
// would be somebody else's tool broken by our cleanup.
func replace(path, content string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".signpost-hook-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() {
		// Already renamed away on the success path, where removing it fails and says
		// nothing worth reporting.
		_ = os.Remove(name)
	}()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// #nosec G302 -- a hook must be executable to run.
	if err := os.Chmod(name, 0o755); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// Status is what the fast check found.
type Status struct {
	// Behind is how many commits have touched code since the bundle was last written. Zero
	// means the bundle is current, or that the check could not tell.
	Behind int
	// Reason is why no answer was produced, when there is none: no bundle, no history, git
	// absent. Reported rather than swallowed, because "the hook printed nothing" and "the
	// hook could not run" look identical from a terminal.
	Reason string
}

// Stale reports whether the hook should say something.
func (s Status) Stale() bool { return s.Behind > 0 }

// Fast reports whether the bundle is behind the code, using commit topology alone.
//
// Two `git log -1` calls and no analysis, which is the point: anything perceptible on every
// commit gets uninstalled, and a full verify on this repository is around a second. The cost
// of that choice is stated plainly rather than hidden — this answers "the bundle was written
// before the newest code commit", not "the bundle's content has drifted". A commit that
// touched only, say, LICENSE moves the code commit without changing any page, and this
// reports it as behind. CheckVerify is the accurate answer for anyone who wants to pay for it.
//
// The comparison deliberately mirrors internal/vcs.readHead: the newest commit that changed
// something outside the bundle is the commit the bundle claims to describe (ADR 0007), so
// asking whether the bundle's own last commit is older than that is asking the same question
// the provenance stamp answers, without reading a file.
func Fast(ctx context.Context, dir string) (Status, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return Status{Reason: "git is not installed"}, nil
	}
	root, err := git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Status{Reason: "not a git repository"}, nil
	}
	if _, err := os.Stat(filepath.Join(root, BundleDir)); err != nil {
		return Status{Reason: "no " + BundleDir + "/ in this repository"}, nil
	}

	code, err := git(ctx, dir, "--no-pager", "log", "-1", "--format=%H",
		"--", ".", ":(exclude)"+BundleDir)
	if err != nil {
		return Status{Reason: "the commit history could not be read"}, nil
	}
	bundle, err := git(ctx, dir, "--no-pager", "log", "-1", "--format=%H", "--", BundleDir)
	if err != nil {
		return Status{Reason: "the commit history could not be read"}, nil
	}
	switch {
	case code == "":
		// Nothing but the bundle has ever been committed. Nothing to be behind.
		return Status{}, nil
	case bundle == "":
		return Status{Reason: BundleDir + "/ is not committed yet"}, nil
	case code == bundle:
		// The same commit carried both: the single-developer flow ADR 0007 describes, and
		// the commonest case there is. A shortcut rather than a guard — the ancestry check
		// below already answers it, since a commit is its own ancestor — kept because it
		// saves a subprocess in the case that happens most, and a git invocation is the
		// only thing in this check that costs anything. Removing it changes no behaviour,
		// which is why no test fails when it goes.
		return Status{}, nil
	}

	// Which way round they are matters. A bundle commit *newer* than the newest code commit
	// is the CI flow — the bot regenerates the bundle after the code lands — and is not
	// stale. Only the other order is.
	if err := gitOK(ctx, dir, "merge-base", "--is-ancestor", code, bundle); err == nil {
		return Status{}, nil
	}
	n, err := git(ctx, dir, "--no-pager", "rev-list", "--count", bundle+"..HEAD",
		"--", ".", ":(exclude)"+BundleDir)
	if err != nil {
		// The order is known even when the count is not, so report the fact without it.
		return Status{Behind: 1}, nil
	}
	count, err := strconv.Atoi(n)
	if err != nil || count < 1 {
		return Status{Behind: 1}, nil
	}
	return Status{Behind: count}, nil
}

// git runs one git command in dir and returns its trimmed stdout.
//
// The repository path is the subprocess's working directory and never an argument, the same
// rule internal/vcs.run follows and for the same reason: a directory named like a flag would
// otherwise be read as one. Every argument here is a compile-time constant or a sha this
// package just read from git.
func git(ctx context.Context, dir string, args ...string) (string, error) {
	// #nosec G204 -- see above: constants plus git's own output, never the path.
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(cmd.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
		"LC_ALL=C",
	)
	var out, errBuf strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// gitOK runs a git command for its exit status alone, which is `merge-base --is-ancestor`'s
// entire interface.
func gitOK(ctx context.Context, dir string, args ...string) error {
	_, err := git(ctx, dir, args...)
	return err
}
