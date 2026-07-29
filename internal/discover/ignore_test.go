package discover

import (
	"strings"
	"testing"
)

// newSet compiles gitignore lines at the root for testing.
func newSet(t *testing.T, lines ...string) *ignoreSet {
	t.Helper()
	s := &ignoreSet{}
	s.add(parseIgnore(strings.NewReader(strings.Join(lines, "\n")), "")...)
	return s
}

func TestIgnoreBasicPatterns(t *testing.T) {
	s := newSet(t, "*.log", "build/", "/only-root.txt", "docs/*.tmp")

	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		// Unanchored patterns match at any depth.
		{"app.log", false, true},
		{"deep/nested/app.log", false, true},
		{"app.log.keep", false, false},
		// Directory-only pattern.
		{"build", true, true},
		{"build", false, false}, // a *file* named build is not matched
		{"src/build", true, true},
		// Anchored to the root.
		{"only-root.txt", false, true},
		{"sub/only-root.txt", false, false},
		// Anchored with a slash: matches exactly one level under docs.
		{"docs/a.tmp", false, true},
		{"docs/deep/a.tmp", false, false},
	}
	for _, c := range cases {
		if got := s.match(c.path, c.isDir); got != c.want {
			t.Errorf("match(%q, isDir=%v) = %v, want %v", c.path, c.isDir, got, c.want)
		}
	}
}

func TestIgnoreNegationOrderMatters(t *testing.T) {
	// Negation after the exclusion re-includes.
	s := newSet(t, "*.log", "!keep.log")
	if !s.match("app.log", false) {
		t.Error("app.log should be ignored")
	}
	if s.match("keep.log", false) {
		t.Error("keep.log should be re-included by the later negation")
	}

	// Reversed order: the exclusion comes last and wins. This is the case a
	// first-match-wins implementation gets wrong.
	rev := newSet(t, "!keep.log", "*.log")
	if !rev.match("keep.log", false) {
		t.Error("with the negation first, the later *.log must win and ignore keep.log")
	}
}

func TestIgnoreDoubleStar(t *testing.T) {
	s := newSet(t, "a/**/b.txt", "**/logs", "cache/**")

	cases := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"a/b.txt", false, true},       // ** matches zero segments
		{"a/x/b.txt", false, true},     // one segment
		{"a/x/y/z/b.txt", false, true}, // many segments
		{"q/a/b.txt", false, false},    // anchored at root, so no match under q
		{"logs", true, true},
		{"deep/logs", true, true},
		{"cache/x", false, true},
		{"cache/x/y/z", false, true},
	}
	for _, c := range cases {
		if got := s.match(c.path, c.isDir); got != c.want {
			t.Errorf("match(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

// A pattern with many "**" segments must not blow up combinatorially. The DP
// matcher makes this linear; a naive backtracking matcher hangs here.
func TestIgnoreManyDoubleStarsTerminates(t *testing.T) {
	pat := strings.Repeat("**/", 20) + "target.txt"
	s := newSet(t, pat)
	path := strings.Repeat("d/", 40) + "other.txt"
	if s.match(path, false) {
		t.Error("should not match a different filename")
	}
	if !s.match(strings.Repeat("d/", 40)+"target.txt", false) {
		t.Error("should match the target filename at depth")
	}
}

func TestIgnoreCommentsBlanksAndEscapes(t *testing.T) {
	s := newSet(t,
		"# a comment",
		"",
		"   ",
		`\#literal.txt`, // escaped hash is a literal filename
		`\!bang.txt`,    // escaped bang is a literal filename
	)
	if s.match("a", false) {
		t.Error("comments and blank lines must not match anything")
	}
	if !s.match("#literal.txt", false) {
		t.Error(`\#literal.txt should ignore the file named #literal.txt`)
	}
	if !s.match("!bang.txt", false) {
		t.Error(`\!bang.txt should ignore the file named !bang.txt`)
	}
}

func TestIgnoreTrailingWhitespace(t *testing.T) {
	// Unescaped trailing space is stripped.
	s := newSet(t, "a.txt   ")
	if !s.match("a.txt", false) {
		t.Error("trailing spaces should be stripped from the pattern")
	}
	// Escaped trailing space is significant.
	esc := newSet(t, `b.txt\ `)
	if !esc.match("b.txt ", false) {
		t.Error(`b.txt\ should match the file "b.txt " with a trailing space`)
	}
}

func TestIgnoreCharacterClass(t *testing.T) {
	s := newSet(t, "file[0-9].txt", "x[!a].txt")
	if !s.match("file3.txt", false) {
		t.Error("character class should match a digit")
	}
	if s.match("fileA.txt", false) {
		t.Error("character class should not match a letter")
	}
	if !s.match("xb.txt", false) {
		t.Error("negated class should match a non-'a' character")
	}
	if s.match("xa.txt", false) {
		t.Error("negated class should not match 'a'")
	}
}

// An unterminated bracket is malformed. It must be treated as a literal rather
// than panicking or matching everything.
func TestIgnoreMalformedClassIsLiteral(t *testing.T) {
	s := newSet(t, "bad[.txt")
	if !s.match("bad[.txt", false) {
		t.Error("a malformed class should compare literally")
	}
	if s.match("badx.txt", false) {
		t.Error("a malformed class must not match as a wildcard")
	}
}

// A nested .gitignore applies only at and below its own directory.
func TestIgnoreNestedScoping(t *testing.T) {
	s := &ignoreSet{}
	s.add(parseIgnore(strings.NewReader("*.tmp"), "sub/dir")...)

	if !s.match("sub/dir/a.tmp", false) {
		t.Error("nested pattern should match within its own directory")
	}
	if !s.match("sub/dir/deeper/a.tmp", false) {
		t.Error("nested unanchored pattern should match below its directory")
	}
	if s.match("a.tmp", false) {
		t.Error("nested pattern must not apply above its directory")
	}
	if s.match("sub/a.tmp", false) {
		t.Error("nested pattern must not apply to a sibling level")
	}
	if s.match("other/dir/a.tmp", false) {
		t.Error("nested pattern must not apply to an unrelated directory")
	}
}

// A deeper .gitignore can re-include what a parent excluded, because it is
// consulted afterwards.
func TestIgnoreNestedOverridesParent(t *testing.T) {
	s := &ignoreSet{}
	s.add(parseIgnore(strings.NewReader("*.log"), "")...)
	s.add(parseIgnore(strings.NewReader("!important.log"), "sub")...)

	if !s.match("sub/other.log", false) {
		t.Error("parent exclusion still applies to non-negated files")
	}
	if s.match("sub/important.log", false) {
		t.Error("nested negation should re-include the file")
	}
	if !s.match("important.log", false) {
		t.Error("the nested negation must not leak above its directory")
	}
}

func TestIgnoreCloneIsolatesSiblings(t *testing.T) {
	parent := newSet(t, "*.a")
	child := parent.clone()
	child.add(parseIgnore(strings.NewReader("*.b"), "")...)

	if parent.match("x.b", false) {
		t.Error("adding to the clone must not affect the parent set")
	}
	if !child.match("x.a", false) || !child.match("x.b", false) {
		t.Error("the clone should carry both parent and its own patterns")
	}
}

// Matching is case-sensitive on every platform, because CI and a Windows laptop
// must agree byte for byte (design §8.1).
func TestIgnoreIsCaseSensitiveEverywhere(t *testing.T) {
	s := newSet(t, "*.log")
	if s.match("app.LOG", false) {
		t.Error("matching must be case-sensitive regardless of host platform")
	}
}
