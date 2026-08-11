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

// header builds one commit header in the field order logArgs asks git for: hash, date,
// author, subject. The order is not arbitrary and this helper is where the tests state it —
// the two fields a repository controls come last, so that a separator inside one of them
// cannot shift the date. See logArgs.
func header(hash, date, author, subject string) string {
	return hash + fieldSep + date + fieldSep + author + fieldSep + subject + fieldSep
}

func TestParseLogBasic(t *testing.T) {
	out := stream(
		[]string{header("aaa", "2026-02-01", "Ann", "feat: add x"), "3\t1\tinternal/a/x.go", "0\t7\tinternal/b/y.go"},
		[]string{header("bbb", "2026-01-01", "Bob", "tidy up"), "10\t0\tinternal/a/x.go"},
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
		header("aaa", "2026-02-01", "Ann", "feat: add x"),
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
		header("aaa", "2026-02-01", "Ann", "feat: add x"),
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
	out := stream([]string{header("aaa", "2026-02-01", "Ann", "feat: add x"), "-\t-\tassets/logo.png"})
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
		[]string{header("aaa", "2026-02-02", "Ann", "empty")},
		[]string{header("bbb", "2026-02-01", "Bob", "one file"), "1\t0\tf.go"},
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
	out := stream([]string{header("aaa", "2026-02-01", "Ann", "feat: add x"), "1\t0\t" + awkward})
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
		header("bbb", "2026-01-01", "Bob", "tidy up") + "\n2\t0\tg.go\x00\x00"
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

func TestParseLogKeepsTheSubject(t *testing.T) {
	out := stream([]string{header("aaa", "2026-02-01", "Ann", "fix(parse): drop the guard"), "1\t0\tf.go"})
	got := parseLog(out)
	if len(got) != 1 {
		t.Fatalf("got %d commits, want 1", len(got))
	}
	if got[0].subject != "fix(parse): drop the guard" {
		t.Errorf("subject = %q, want the whole subject", got[0].subject)
	}
}

// A subject containing the field separator. git accepts every byte but NUL in a commit
// message, so this is legal input rather than a hypothetical, and it is the reason parseLog
// rejoins the tail instead of indexing field 3: read positionally, `feat: a<US>b` would be
// truncated to `feat: a` and the leftover would look like a spare field.
func TestParseLogSubjectContainingTheSeparator(t *testing.T) {
	subject := "feat: a" + fieldSep + "b"
	out := stream([]string{header("aaa", "2026-02-01", "Ann", subject), "1\t0\tf.go"})
	got := parseLog(out)
	if len(got) != 1 {
		t.Fatalf("got %d commits, want 1", len(got))
	}
	if got[0].subject != subject {
		t.Errorf("subject = %q, want %q", got[0].subject, subject)
	}
	// The point of the test: the fields ahead of the subject are untouched.
	if got[0].date != "2026-02-01" || got[0].author != "Ann" || got[0].hash != "aaa" {
		t.Errorf("a separator in the subject shifted an earlier field: %+v", got[0])
	}
}

// The regression this field order exists for. git accepts a unit separator inside an author
// name — verified against git 2.51, which takes any byte but NUL there — and under the old
// order (hash, author, date, ...) such a name shifted every following field right, so the
// *date* parsed as a fragment of the name and every module page recorded that fragment as
// its first_commit and last_commit.
//
// Ordering by trust does not stop the shift; it bounds what the shift can reach. What is
// downstream of the author now is the subject, which this package counts and discards.
func TestParseLogSeparatorInAuthorCannotReachTheDate(t *testing.T) {
	out := stream([]string{header("aaa", "2026-02-01", "ev"+fieldSep+"il", "feat: x"), "1\t0\tf.go"})
	got := parseLog(out)
	if len(got) != 1 {
		t.Fatalf("got %d commits, want 1", len(got))
	}
	if got[0].date != "2026-02-01" {
		t.Errorf("date = %q, want the real date: a separator in the author name reached it", got[0].date)
	}
	if got[0].hash != "aaa" {
		t.Errorf("hash = %q, want aaa", got[0].hash)
	}
	// The shift still happens, and this records exactly where it lands so that a future
	// change to the format cannot quietly move it somewhere that matters. The author reads
	// as the part before the separator and the rest bleeds into the subject.
	if got[0].author != "ev" {
		t.Errorf("author = %q, want the truncated name", got[0].author)
	}
	if !strings.Contains(got[0].subject, "il") {
		t.Errorf("subject = %q, expected the shifted tail of the author name", got[0].subject)
	}
}

// A header with four fields rather than five is the old format, or a truncated read. Either
// way it must be skipped: accepting it would read the author as a date.
func TestParseLogRejectsAFourFieldHeader(t *testing.T) {
	out := "aaa" + fieldSep + "2026-02-01" + fieldSep + "Ann" + fieldSep + "\n1\t0\tf.go\x00\x00"
	if got := parseLog(out); len(got) != 0 {
		t.Errorf("a four-field header was accepted: %+v", got)
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
