// Package scaffold writes the files a repository needs for CI to keep its bundle honest.
//
// A committed bundle is only worth committing if something rebuilds it, so the workflow
// is not decoration around the tool — it is the half of the design that makes the other
// half true. Until this package existed the only way to get it was to transcribe it out
// of the README, which puts the most load-bearing file in the setup in the place most
// likely to be copied wrong or skipped.
//
// Two decisions shape everything here.
//
// **The templates are embedded, not fetched.** Considered pulling them as a tagged
// artifact from a registry and declined: the templates do not version independently of
// the binary, so decoupling buys nothing, while it would make this the only networked
// command in an otherwise hermetic tool and put an unauthenticated fetch in the path of
// a file that requests `contents: write` in somebody's repository. Doing that safely
// needs signature verification, which is a dependency ADR 0002 says we must be able to
// patch ourselves.
//
// **Nothing here decides to write.** Plan reports what would happen and Apply carries
// out a plan. The caller shows the plan first, which is what makes `signpost init
// github` a preview by default rather than a command that edits a repository on the
// strength of being typed correctly.
package scaffold

import (
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// templates holds the files this package writes.
//
// Embedded rather than string constants because the workflow is 190 lines of YAML whose
// comments are most of its value: a Go string literal would lose the ability to lint it
// (actionlint reads .yml files, not string constants) and would invite somebody to trim
// the comments to keep the literal readable. The comments are the part that stops a
// reader from deleting a loop guard they did not recognise.
//
//go:embed templates
var templates embed.FS

// WorkflowPath and ConfigPath are where the two files go. Fixed, not configurable:
// GitHub reads workflows from exactly one directory, and ADR 0011 decided the config
// file is read from the repository root and nowhere else.
const (
	WorkflowPath = ".github/workflows/signpost.yml"
	ConfigPath   = ".signpost.yml"
)

// ErrExists reports that a file this command would write is already there.
//
// Its own error rather than a bool on the result, because it is the one outcome where
// doing nothing is success and doing the obvious thing is damage: overwriting a tuned
// workflow to install a default is worse than declining. The caller turns it into a
// message naming what it found.
var ErrExists = errors.New("already present")

// File is one file a plan would write.
type File struct {
	// Path is relative to the repository root, with forward slashes — it names a
	// position in a git tree, not a location on this filesystem.
	Path string
	// Contents is what would be written, rendered.
	Contents string
	// Exists reports that something is already at Path. A plan with any Exists file
	// writes nothing at all.
	Exists bool
}

// Plan is what `init github` would do, and what it prints when asked to preview.
type Plan struct {
	Files []File
	// Repo is the name that reached the config file, and Derived says it came from a
	// git remote rather than from the caller. Reported because #31 established that a
	// remote is a property of the checkout: a fork's remote names the upstream, so a
	// derived value is a proposal a reader has to agree with, never a fact.
	Repo    string
	Derived bool
}

// Blocked reports whether anything is in the way. A plan is all-or-nothing: two files
// where one exists must not write the other, because a repository with a config file and
// no workflow is a repository whose bundle silently stops being rebuilt.
func (p Plan) Blocked() []string {
	var blocked []string
	for _, f := range p.Files {
		if f.Exists {
			blocked = append(blocked, f.Path)
		}
	}
	sort.Strings(blocked)
	return blocked
}

// PlanGitHub works out what to write into root.
//
// repo is the name for `.signpost.yml`. Empty means derive it from the git remote, and
// Derived records that so the caller can say so — an empty result is not an error,
// because a bundle without a repository name still carries a commit-only resource, which
// is enough to tell whether a page describes the code in front of you.
func PlanGitHub(root, repo string) (Plan, error) {
	plan := Plan{Repo: repo}
	if plan.Repo == "" {
		if name, ok := remoteRepo(root); ok {
			plan.Repo, plan.Derived = name, true
		}
	}

	workflow, err := templates.ReadFile("templates/signpost.yml")
	if err != nil {
		return Plan{}, err
	}
	plan.Files = []File{
		{Path: WorkflowPath, Contents: string(workflow)},
		{Path: ConfigPath, Contents: renderConfig(plan.Repo)},
	}
	for i, f := range plan.Files {
		// Stat rather than a read: whether something is there is the question, and a
		// directory at that path blocks the write just as a file does. Any error other
		// than not-exist is also treated as present, because a path we cannot inspect is
		// not one to overwrite.
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(f.Path))); err == nil {
			plan.Files[i].Exists = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return Plan{}, fmt.Errorf("checking %s: %w", f.Path, err)
		}
	}
	return plan, nil
}

// Apply writes a plan, or writes nothing.
//
// The blocked check is here as well as in the caller on purpose: this is the function
// that touches the filesystem, and a guard that lives only in the command is one a second
// caller does not get.
func Apply(root string, plan Plan) error {
	if blocked := plan.Blocked(); len(blocked) > 0 {
		return fmt.Errorf("%w: %s", ErrExists, strings.Join(blocked, ", "))
	}
	for _, f := range plan.Files {
		dest := filepath.Join(root, filepath.FromSlash(f.Path))
		// #nosec G301 -- `.github/workflows` is a directory git tracks and every
		// checkout recreates; the umask-default mode is the one it should have.
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		// 0o644, and the workflow is not executable: GitHub reads it, nothing runs it.
		//
		// #nosec G306 -- both files are committed, and git records only the executable
		// bit, so 0o600 here would be restored as 0o644 on the next clone anyway. A mode
		// that does not survive the artifact's own distribution is not a control.
		if err := os.WriteFile(dest, []byte(f.Contents), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// renderConfig writes `.signpost.yml`.
//
// Built here rather than embedded with a placeholder because there are two genuinely
// different files: with a name, and without one. A template with `repo: {{.Repo}}` left
// blank produces a key with no value, which the reader is right to reject — and a
// scaffold whose first act is to write a file the tool refuses to parse is worse than
// one that writes nothing.
func renderConfig(repo string) string {
	var b strings.Builder
	b.WriteString("# How this repository wants to be read.\n" +
		"#\n" +
		"# One precedence order: flag > environment > file > default. A key may only change a\n" +
		"# default — anything deciding whether a check *fails* stays a flag, so a repository\n" +
		"# cannot quiet its own gate by committing a file. There is nowhere to put a\n" +
		"# credential; the file is committed, and `api_key` is refused by name.\n")
	if repo == "" {
		b.WriteString("#\n" +
			"# `repo` names the repository every page's `resource:` points at, and it is left\n" +
			"# out here because nothing could work it out for you. It is asked for rather than\n" +
			"# read from a git remote: a remote URL is a property of your checkout, and a\n" +
			"# fork's remote names the upstream. Without it pages carry a commit-only\n" +
			"# resource, which still tells a reader whether a page describes the code in\n" +
			"# front of them.\n" +
			"#\n" +
			"# repo: example.com/org/repo\n")
		return b.String()
	}
	b.WriteString("#\n" +
		"# `repo` belongs here rather than in the workflow, and a fork shows why: it names the\n" +
		"# repository being described, while a workflow knows only the repository it is\n" +
		"# running in. Passing it from CI meant a fork rebuilt identical source under its own\n" +
		"# name, so its bundle never byte-matched upstream and its first sync conflicted\n" +
		"# inside `.signpost/`. Committed, the name travels with the clone, and a fork that\n" +
		"# means to publish under its own name changes this line in a diff that says so.\n" +
		"repo: " + repo + "\n")
	return b.String()
}

// remoteRepo reads `origin` out of .git/config and turns it into a bundle name.
//
// The file is parsed rather than shelling out to `git remote get-url`, for the reason the
// rest of this package is hermetic: `init` should work in a checkout on a machine without
// git installed, and this is a well-specified INI-ish file whose one line we need.
//
// It returns a host/path name, stripping the scheme, any credentials, the port, and the
// `.git` suffix, so both `https://github.com/o/r.git` and `git@github.com:o/r.git` become
// `github.com/o/r`. A value it cannot make sense of is dropped rather than guessed at,
// because the caller's fallback — say nothing and leave the key commented out — is
// correct, and a mangled name in a committed file is not.
func remoteRepo(root string) (string, bool) {
	// #nosec G304 -- a fixed name under the root being scaffolded, same as config.Load.
	data, err := os.ReadFile(filepath.Join(root, ".git", "config"))
	if err != nil {
		return "", false
	}
	var inOrigin bool
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			// Section names are matched exactly. `[remote "origin"]` is the one wanted;
			// `[remote "upstream"]` is a different repository, which is the whole subtlety
			// #31 was about.
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if !inOrigin || !strings.HasPrefix(line, "url") {
			continue
		}
		_, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if name := normaliseRemote(strings.TrimSpace(value)); name != "" {
			return name, true
		}
	}
	return "", false
}

// normaliseRemote turns a git URL into host/path, or returns empty for anything it cannot
// read confidently.
func normaliseRemote(url string) string {
	url = strings.TrimSuffix(url, ".git")
	switch {
	case strings.Contains(url, "://"):
		_, rest, _ := strings.Cut(url, "://")
		url = rest
	case strings.Contains(url, ":"):
		// scp-like: git@host:org/repo. The colon is a separator, not a port, so it
		// becomes a slash.
		host, path, _ := strings.Cut(url, ":")
		url = host + "/" + strings.TrimPrefix(path, "/")
	}
	// Credentials, if any, precede an @ in the authority. Dropped: a name that carries a
	// token would put it in a committed file.
	if at := strings.LastIndex(url, "@"); at >= 0 {
		url = url[at+1:]
	}
	host, path, ok := strings.Cut(url, "/")
	if !ok || path == "" {
		return ""
	}
	// A port is a property of how this checkout reaches the server, not of the
	// repository's name.
	if h, _, found := strings.Cut(host, ":"); found {
		host = h
	}
	if host == "" {
		return ""
	}
	return host + "/" + strings.Trim(path, "/")
}
