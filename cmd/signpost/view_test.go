package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/3rg0n/signpost/internal/vcs"
	"github.com/3rg0n/signpost/internal/view"
)

// withHistory is an analysis carrying only what the view helpers read. A real analysis is
// used by TestViewOptionsCarriesTheAnalysis below; these cases are about the functions that
// turn a repository name into a URL, where the interesting inputs are the ones a real run
// rarely produces.
func withHistory(sha string) *analysis {
	if sha == "" {
		return &analysis{History: &vcs.Signals{Available: false, Reason: "no git directory"}}
	}
	return &analysis{History: &vcs.Signals{
		Available: true,
		Head:      vcs.Commit{SHA: sha},
	}}
}

func TestViewTitle(t *testing.T) {
	cases := []struct {
		repo, root, want string
	}{
		{"github.com/org/repo", filepath.FromSlash("/work/src"), "github.com/org/repo"},
		// The declared name wins because several checkouts of different repositories can
		// all sit in a directory called src.
		{"", filepath.FromSlash("/work/repo"), "repo"},
		{"", "", "this repository"},
		{"", ".", "this repository"},
		{"", string(filepath.Separator), "this repository"},
	}
	for _, c := range cases {
		if got := viewTitle(c.repo, c.root); got != c.want {
			t.Errorf("viewTitle(%q, %q) = %q, want %q", c.repo, c.root, got, c.want)
		}
	}
}

func TestShortSHA(t *testing.T) {
	if got := shortSHA(withHistory("abcdef0123456789abcdef")); got != "abcdef012345" {
		t.Errorf("shortSHA = %q, want the first 12 characters", got)
	}
	// The negatives: every one of these renders as nothing rather than as "unknown",
	// because the notes already say history was not read.
	if got := shortSHA(withHistory("")); got != "" {
		t.Errorf("shortSHA with no history = %q, want empty", got)
	}
	if got := shortSHA(&analysis{}); got != "" {
		t.Errorf("shortSHA with a nil History = %q, want empty", got)
	}
	if got := shortSHA(&analysis{History: &vcs.Signals{Available: true}}); got != "" {
		t.Errorf("shortSHA with an empty SHA = %q, want empty", got)
	}
	// Shorter than the abbreviation, which a repository with one commit and a truncated
	// read can produce: returned whole rather than sliced out of range.
	if got := shortSHA(withHistory("abc")); got != "abc" {
		t.Errorf("shortSHA of a short sha = %q, want %q", got, "abc")
	}
}

// TestRepoBase asserts both halves, and the negative half is the larger one on purpose. A
// link built on the wrong path shape is a 404 on every file in the detail panel, which is
// worse than the plain filename an empty base produces — so anything not known to serve
// /blob/<ref>/<path> must return empty rather than a guess.
func TestRepoBase(t *testing.T) {
	const sha = "abcdef0123456789"
	ok := []struct {
		repo, want string
	}{
		{"github.com/org/repo", "https://github.com/org/repo/blob/" + sha + "/"},
		{"gitlab.com/org/repo", "https://gitlab.com/org/repo/blob/" + sha + "/"},
		{"codeberg.org/org/repo", "https://codeberg.org/org/repo/blob/" + sha + "/"},
		// A subgroup, which GitLab allows and which is still /blob/<ref>/.
		{"gitlab.com/org/group/repo", "https://gitlab.com/org/group/repo/blob/" + sha + "/"},
		// The host match is case-insensitive; the path is not touched.
		{"GitHub.com/Org/Repo", "https://github.com/Org/Repo/blob/" + sha + "/"},
	}
	for _, c := range ok {
		if got := repoBase(c.repo, withHistory(sha)); got != c.want {
			t.Errorf("repoBase(%q) = %q, want %q", c.repo, got, c.want)
		}
	}

	empty := []string{
		// No name declared: the common case, and a supported one.
		"",
		// Hosts that exist and do not use /blob/: Bitbucket serves /src/, cgit /tree/.
		"bitbucket.org/org/repo",
		"git.kernel.org/pub/scm/linux/kernel/git/torvalds/linux.git",
		"sr.ht/~user/repo",
		"dev.azure.com/org/project/_git/repo",
		// An enterprise install on its own hostname, which is indistinguishable from any
		// other host from here. Deliberately unmatched — see linkHosts.
		"git.example.com/org/repo",
		// Near misses on a matched host, which are the mutations that would let a
		// substring or suffix match through. Each must fail.
		"github.com.evil.example.com/org/repo",
		"evil-github.com/org/repo",
		"notgithub.com/org/repo",
		"github.company.com/org/repo",
		// A host with nothing after it, which cannot name a repository.
		"github.com",
		"github.com/",
		// A Go module path that is not a forge path at all.
		"golang.org/x/tools",
	}
	for _, repo := range empty {
		if got := repoBase(repo, withHistory(sha)); got != "" {
			t.Errorf("repoBase(%q) = %q, want empty", repo, got)
		}
	}

	// No commit to pin to, so no link even on a matched host: a link to a branch would
	// point at a file that has since moved, from a page claiming to describe one tree.
	if got := repoBase("github.com/org/repo", withHistory("")); got != "" {
		t.Errorf("repoBase with no history = %q, want empty", got)
	}
	if got := repoBase("github.com/org/repo", &analysis{}); got != "" {
		t.Errorf("repoBase with a nil History = %q, want empty", got)
	}
}

// TestViewNotesReportsUnreadHistory: the notes are what the page says was already out of
// step, and history not having been read is the case a reader cannot otherwise infer —
// the commit line is simply absent.
func TestViewNotesReportsUnreadHistory(t *testing.T) {
	root := t.TempDir()

	notes := viewNotes(context.Background(), &analysis{}, root)
	if len(notes) == 0 || !strings.Contains(notes[0], "-no-history") {
		t.Errorf("notes with a nil History = %q; expected it to name the flag that caused it", notes)
	}

	notes = viewNotes(context.Background(), &analysis{
		History: &vcs.Signals{Available: false, Reason: "no git directory"},
	}, root)
	if len(notes) == 0 || !strings.Contains(notes[0], "no git directory") {
		t.Errorf("notes for an unavailable history = %q; expected the reason", notes)
	}

	notes = viewNotes(context.Background(), &analysis{
		History: &vcs.Signals{Available: true, Shallow: true, Reason: "shallow clone; run git fetch --unshallow"},
	}, root)
	if len(notes) == 0 || !strings.Contains(notes[0], "unshallow") {
		t.Errorf("notes for a shallow read = %q; expected the reason", notes)
	}

	// The negative: a complete read of a repository with no bundle says nothing. A note
	// that is always present is a note a reader stops reading.
	notes = viewNotes(context.Background(), withHistory("abcdef0123456789"), root)
	if len(notes) != 0 {
		t.Errorf("notes for a clean read = %q, want none", notes)
	}
}

// listenLoopback occupies a loopback port and reports its number, so a test can make
// binding fail without picking a number and hoping it is free.
func listenLoopback(t *testing.T) (net.Listener, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	return ln, port
}

// TestViewRejectsAnAskedPortCollision is the CLI half of the regression: view.go must pass
// the flag's set-ness through, and a test only in internal/view would pass with
// PortWasAsked wired to nothing. The bug was exactly that — the field existed and was fed
// by nothing.
//
// Run against a deadline rather than called directly, because the bug's symptom is not a
// wrong exit code: `view` binds a free port instead and serves until interrupted. Called
// inline, this test would hang for the whole run rather than report anything, which is a
// regression test that fails to be one.
func TestViewRejectsAnAskedPortCollision(t *testing.T) {
	root := fixture(t)
	ln, port := listenLoopback(t)
	defer func() { _ = ln.Close() }()

	type result struct {
		stderr string
		code   int
	}
	done := make(chan result, 1)
	go func() {
		var out, errOut lockedBuffer
		code := run([]string{"view", "-port", port, "-no-open", "-quiet", root}, &out, &errOut)
		done <- result{errOut.String(), code}
	}()

	// Generous: it covers a full analysis of the fixture on a loaded machine, and the
	// passing path returns as soon as the bind fails.
	select {
	case r := <-done:
		if r.code == 0 {
			t.Fatalf("view on a taken -port exited 0; a named port that cannot be bound is an error")
		}
		if !strings.Contains(r.stderr, "127.0.0.1:"+port) {
			t.Errorf("stderr does not name the address it could not bind: %q", r.stderr)
		}
	case <-time.After(60 * time.Second):
		// The goroutine is left running and holds a port for the rest of the binary. That is
		// the cost of reporting this at all: the command it is running does not return until
		// interrupted, and this process cannot portably interrupt itself.
		t.Fatalf("view is still running after 60s on a taken -port; it fell back to a free port "+
			"instead of reporting the collision (port %s)", port)
	}
}

// lockedBuffer is a buffer a test can read while the command is still writing to it. Serve
// writes its banner from the goroutine running the command, which is a data race on a
// plain bytes.Buffer and is reported as one under -race.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestViewOptionsCarriesTheAnalysis is the wiring check. internal/view's tests build their
// own Options and cannot see a field this command never fills, and the -port bug was
// exactly that: the field existed, was tested in isolation, and was fed by nothing.
//
// Not a test of the whole command, which blocks in Serve until a signal the test process
// cannot portably send itself. This is the half with a contract.
func TestViewOptionsCarriesTheAnalysis(t *testing.T) {
	root := fixture(t)
	a, err := analyse(context.Background(), root, pipelineFlags{})
	if err != nil {
		t.Fatal(err)
	}

	opts, err := viewOptions(context.Background(), a, root, "github.com/org/repo", 7777, true)
	if err != nil {
		t.Fatal(err)
	}
	if !opts.PortWasAsked {
		t.Error("PortWasAsked did not reach Options; an explicitly named -port would fall back silently")
	}
	if opts.Port != 7777 {
		t.Errorf("Port = %d, want 7777", opts.Port)
	}
	if opts.Title != "github.com/org/repo" {
		t.Errorf("Title = %q", opts.Title)
	}
	if opts.Root != a.Discovered.Root {
		t.Errorf("Root = %q, want %q", opts.Root, a.Discovered.Root)
	}

	// The counts the page states in prose come from the graph, not from the JSON, so a
	// page saying "0 nodes" over a populated graph is a real failure mode.
	nodes, edges := a.Graph().Counts()
	if opts.Nodes != nodes || opts.Edges != edges {
		t.Errorf("Options counts = %d/%d, graph = %d/%d", opts.Nodes, opts.Edges, nodes, edges)
	}
	if nodes == 0 || edges == 0 {
		t.Fatalf("the fixture produced %d nodes and %d edges; it has modules that import each other", nodes, edges)
	}

	var doc struct {
		Nodes []json.RawMessage `json:"nodes"`
		Edges []json.RawMessage `json:"edges"`
	}
	if err := json.Unmarshal(opts.Graph, &doc); err != nil {
		t.Fatalf("Graph is not the shape graph.js reads: %v\n%s", err, opts.Graph)
	}
	if len(doc.Nodes) != nodes || len(doc.Edges) != edges {
		t.Errorf("the served JSON has %d nodes and %d edges, the graph has %d and %d",
			len(doc.Nodes), len(doc.Edges), nodes, edges)
	}

	// The fixture has no bundle, so nothing to say about staleness; -port aside, this is
	// the negative that would catch a note printed unconditionally.
	for _, n := range opts.Notes {
		if strings.Contains(n, "behind this tree") {
			t.Errorf("a repository with no bundle got a staleness note: %q", n)
		}
	}

	// Unasked, so a collision may fall back — the other half of the same field.
	opts, err = viewOptions(context.Background(), a, root, "", view.DefaultPort, false)
	if err != nil {
		t.Fatal(err)
	}
	if opts.PortWasAsked {
		t.Error("PortWasAsked is true for a flag that was not passed")
	}
	// No -repo, so no links: the panel lists filenames as text. Empty is the supported
	// answer and the common one.
	if opts.RepoBase != "" {
		t.Errorf("RepoBase = %q with no -repo, want empty", opts.RepoBase)
	}
	if opts.Title != filepath.Base(a.Discovered.Root) {
		t.Errorf("Title = %q, want the directory name %q", opts.Title, filepath.Base(a.Discovered.Root))
	}
}

// TestViewStaticWritesAndReturns runs the whole command, which the tests above deliberately
// do not: `view` without -static blocks in Serve until a signal this process cannot portably
// send itself, so everything else here tests viewOptions and leaves the command alone.
// -static is the one path that returns, and running it is the only way to see that the flag
// reaches WriteStatic at all — internal/view's tests build their own Options and would pass
// against a command that parsed -static and then served.
func TestViewStaticWritesAndReturns(t *testing.T) {
	root := fixture(t)
	dir := filepath.Join(t.TempDir(), "site")

	var out, errOut bytes.Buffer
	if code := run([]string{"view", "-static", dir, "-quiet", root}, &out, &errOut); code != 0 {
		t.Fatalf("view -static: exit %d\n%s", code, errOut.String())
	}

	// The set, not a spot check: a fifth asset added to the server and missed by the export
	// is the failure this whole surface exists to make impossible, and the command is where
	// it would be visible to somebody publishing.
	for _, name := range []string{"index.html", "graph.json", "graph.js", "style.css", "favicon.svg"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("view -static did not write %s: %v", name, err)
		}
		if !strings.Contains(out.String(), name) {
			t.Errorf("stdout does not report writing %s:\n%s", name, out.String())
		}
	}

	// A caller about to upload the directory is told what is in it. Stated on every run
	// rather than behind a flag, because it is the one thing about this command that cannot
	// be taken back.
	for _, want := range []string{"module", "publish"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("stdout does not say what publishing this costs (%q):\n%s", want, out.String())
		}
	}

	// The graph the page loads is this repository's, not a placeholder. An empty document
	// renders as a page that looks like it works.
	data, err := os.ReadFile(filepath.Join(dir, "graph.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Nodes []json.RawMessage `json:"nodes"`
		Edges []json.RawMessage `json:"edges"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("graph.json is not the shape graph.js reads: %v", err)
	}
	if len(doc.Nodes) == 0 || len(doc.Edges) == 0 {
		t.Errorf("graph.json has %d nodes and %d edges; the fixture has modules that import each other",
			len(doc.Nodes), len(doc.Edges))
	}
}

// TestViewStaticRejectsServingFlags asserts the exit code as well as the message, because
// exit 2 is the half CI depends on: 2 means the command line was wrong, 1 means signpost ran
// and the repository was the problem. A mutual exclusion reported as 1 tells a deploy its
// repository is broken.
func TestViewStaticRejectsServingFlags(t *testing.T) {
	root := fixture(t)
	dir := filepath.Join(t.TempDir(), "site")

	for _, flag := range []string{"-port", "-no-open"} {
		args := []string{"view", "-static", dir, flag}
		if flag == "-port" {
			args = append(args, "7777")
		}
		args = append(args, "-quiet", root)

		var out, errOut bytes.Buffer
		code := run(args, &out, &errOut)
		if code != 2 {
			t.Errorf("view -static with %s: exit %d, want 2 — a flag that cannot be honoured is "+
				"a misuse of the command line\n%s", flag, code, errOut.String())
		}
		if !strings.Contains(errOut.String(), flag) {
			t.Errorf("stderr does not name %s: %q", flag, errOut.String())
		}
		// Nothing written. Rejecting after the export would leave a directory somebody has
		// to notice is there.
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("view -static wrote %s despite rejecting %s", dir, flag)
		}
	}
}
