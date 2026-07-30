package vcs

import (
	"strings"
	"testing"
)

// stream builds the byte layout git produces under `-z --numstat --pretty=format:...`,
// which parseLog's doc comment records and TestReadAgainstRealGit verifies against git
// itself. Written as a helper so a test reads as the history it describes rather than as
// an escape-sequence puzzle.
func stream(commits ...[]string) string {
	var b strings.Builder
	for _, c := range commits {
		// The header, then a newline — not a NUL. That asymmetry is the single most
		// error-prone part of this format and the reason this helper exists.
		b.WriteString(c[0])
		b.WriteString("\n")
		for _, line := range c[1:] {
			b.WriteString(line)
			b.WriteString("\x00")
		}
		b.WriteString("\x00")
	}
	return b.String()
}

func header(hash, author, date string) string {
	return hash + fieldSep + author + fieldSep + date + fieldSep
}

func TestParseLogBasic(t *testing.T) {
	out := stream(
		[]string{header("aaa", "Ann", "2026-02-01"), "3\t1\tinternal/a/x.go", "0\t7\tinternal/b/y.go"},
		[]string{header("bbb", "Bob", "2026-01-01"), "10\t0\tinternal/a/x.go"},
	)
	got := parseLog(out)
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2: %+v", len(got), got)
	}
	if got[0].hash != "aaa" || got[0].author != "Ann" || got[0].date != "2026-02-01" {
		t.Errorf("first header parsed as %+v", got[0])
	}
	if len(got[0].files) != 2 {
		t.Fatalf("first commit has %d files, want 2: %+v", len(got[0].files), got[0].files)
	}
	if f := got[0].files[0]; f.path != "internal/a/x.go" || f.insertions != 3 || f.deletions != 1 {
		t.Errorf("first file parsed as %+v", f)
	}
	if f := got[0].files[1]; f.path != "internal/b/y.go" || f.insertions != 0 || f.deletions != 7 {
		t.Errorf("second file parsed as %+v", f)
	}
	if len(got[1].files) != 1 || got[1].files[0].path != "internal/a/x.go" {
		t.Errorf("second commit files: %+v", got[1].files)
	}
}

// A rename arrives as an empty path field followed by the old and new paths as their own
// tokens. Attributing the change to the old path would report churn against a file that
// no longer exists, so the new path must win.
func TestParseLogRenameTakesNewPath(t *testing.T) {
	out := stream([]string{
		header("aaa", "Ann", "2026-02-01"),
		"4\t2\t", "internal/old/name.go", "internal/new/name.go",
	})
	got := parseLog(out)
	if len(got) != 1 || len(got[0].files) != 1 {
		t.Fatalf("got %+v, want one commit with one file", got)
	}
	f := got[0].files[0]
	if f.path != "internal/new/name.go" {
		t.Errorf("rename recorded as %q, want the new path", f.path)
	}
	if f.insertions != 4 || f.deletions != 2 {
		t.Errorf("rename lost its line counts: %+v", f)
	}
	// The old path is retained, because it is the only record linking this path to the
	// older commits that still name it.
	if f.oldPath != "internal/old/name.go" {
		t.Errorf("oldPath = %q, want the pre-rename path", f.oldPath)
	}
}

// A rename followed by an ordinary change in the same commit: the state machine must
// hand back control after consuming exactly two path tokens.
func TestParseLogRenameThenNormalFile(t *testing.T) {
	out := stream([]string{
		header("aaa", "Ann", "2026-02-01"),
		"1\t1\t", "a/old.go", "a/new.go",
		"5\t0\tb/other.go",
	})
	got := parseLog(out)
	if len(got) != 1 {
		t.Fatalf("got %d commits, want 1", len(got))
	}
	var paths []string
	for _, f := range got[0].files {
		paths = append(paths, f.path)
	}
	want := []string{"a/new.go", "b/other.go"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Errorf("paths = %v, want %v", paths, want)
	}
}

// git writes `-` for both counts of a binary file. Recording that as 0 would make a
// binary file's history read as a series of empty diffs.
func TestParseLogBinary(t *testing.T) {
	out := stream([]string{header("aaa", "Ann", "2026-02-01"), "-\t-\tassets/logo.png"})
	got := parseLog(out)
	if len(got) != 1 || len(got[0].files) != 1 {
		t.Fatalf("got %+v", got)
	}
	f := got[0].files[0]
	if !f.binary {
		t.Error("binary file not marked binary")
	}
	if f.insertions != 0 || f.deletions != 0 {
		t.Errorf("binary counts should stay zero, got %+v", f)
	}
}

// An empty commit produces a header and no numstat, so two headers arrive back to back.
// A parser that expected a numstat block after every header would swallow the second.
func TestParseLogEmptyCommit(t *testing.T) {
	out := stream(
		[]string{header("aaa", "Ann", "2026-02-02")},
		[]string{header("bbb", "Bob", "2026-02-01"), "1\t0\tf.go"},
	)
	got := parseLog(out)
	if len(got) != 2 {
		t.Fatalf("got %d commits, want 2: %+v", len(got), got)
	}
	if len(got[0].files) != 0 {
		t.Errorf("empty commit has files: %+v", got[0].files)
	}
	if got[1].hash != "bbb" || len(got[1].files) != 1 {
		t.Errorf("commit after an empty one parsed as %+v", got[1])
	}
}

// A path with a space, a quote, and a non-ASCII byte. Under -z git writes these raw, so
// they must survive verbatim — this is the case that C-quoting would mangle and the
// reason -z is used at all.
func TestParseLogAwkwardPaths(t *testing.T) {
	awkward := `dir with space/a"quoteé.go`
	out := stream([]string{header("aaa", "Ann", "2026-02-01"), "1\t0\t" + awkward})
	got := parseLog(out)
	if len(got) != 1 || len(got[0].files) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].files[0].path != awkward {
		t.Errorf("path = %q, want %q", got[0].files[0].path, awkward)
	}
}

func TestParseLogEmptyInput(t *testing.T) {
	if got := parseLog(""); len(got) != 0 {
		t.Errorf("empty input produced %+v", got)
	}
}

// A malformed header must not attach its files to the previous commit: that would
// attribute changes to an author who did not make them.
func TestParseLogMalformedHeaderSkipped(t *testing.T) {
	out := "short" + fieldSep + "header\n" + "1\t0\tf.go\x00\x00" +
		header("bbb", "Bob", "2026-01-01") + "\n2\t0\tg.go\x00\x00"
	got := parseLog(out)
	for _, c := range got {
		if c.hash == "short" {
			t.Fatalf("malformed header was accepted: %+v", c)
		}
	}
	// The well-formed commit still parses; one bad record does not poison the walk.
	if len(got) != 1 || got[0].hash != "bbb" {
		t.Fatalf("got %+v, want only the well-formed commit", got)
	}
}

// Paths that cannot come from git log in a well-formed repository are refused rather than
// allowed to attach signals outside the analysed tree.
func TestNormalizePathRejectsEscapes(t *testing.T) {
	for _, p := range []string{"/etc/passwd", "../outside.go", "..", ""} {
		if got := normalizePath(p); got != "" {
			t.Errorf("normalizePath(%q) = %q, want rejected", p, got)
		}
	}
	if got := normalizePath("internal/a/x.go\n"); got != "internal/a/x.go" {
		t.Errorf("trailing newline not trimmed: %q", got)
	}
}

func TestDirOf(t *testing.T) {
	cases := map[string]string{
		"internal/a/x.go": "internal/a",
		"README.md":       "",
		"a/b/c/d.go":      "a/b/c",
	}
	for in, want := range cases {
		if got := dirOf(in); got != want {
			t.Errorf("dirOf(%q) = %q, want %q", in, got, want)
		}
	}
}
