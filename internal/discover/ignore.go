package discover

import (
	"bufio"
	"io"
	"path"
	"strings"
)

// A hand-written .gitignore matcher.
//
// Why not a library: this is the one piece of signpost that decides which files
// exist at all, so a disagreement with git here silently changes every downstream
// artifact. It is ~150 lines of well-specified behaviour, it is exercised against
// the documented gitignore semantics in ignore_test.go, and owning it means the
// walk has no dependency that can change matching behaviour in a patch release.
//
// Matching is always case-sensitive, on every platform. Git's `core.ignorecase`
// makes this configurable, but the bundle is committed and CI must agree with a
// contributor's laptop byte for byte (design §8.1), so a platform-dependent walk
// is not an option. Case-sensitive is the Linux/CI behaviour and therefore the
// one that counts.

// pattern is one compiled .gitignore line.
type pattern struct {
	negate  bool
	dirOnly bool
	// segs is the pattern split on "/". A leading "**" is prepended for
	// unanchored patterns, which is what makes "*.log" match at any depth: the
	// unanchored and anchored cases then run through the same matcher.
	segs []string
	// base is the directory containing the .gitignore, relative to the walk
	// root, slash-separated, "" for the root file. Patterns only apply at or
	// below their own file's directory.
	base string
	// raw is the original line, kept for diagnostics.
	raw string
}

// ignoreSet is an ordered list of patterns. Later patterns win, which is how git
// resolves a negation against an earlier exclusion.
type ignoreSet struct {
	pats []pattern
}

// parseIgnore compiles the lines of one .gitignore. base is the file's directory
// relative to the walk root ("" at the root).
func parseIgnore(r io.Reader, base string) []pattern {
	var out []pattern
	sc := bufio.NewScanner(r)
	// .gitignore lines are short, but a pathological repo should not panic the
	// walk on a long line.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		if p, ok := compilePattern(sc.Text(), base); ok {
			out = append(out, p)
		}
	}
	return out
}

// compilePattern turns one line into a pattern. Returns false for blank lines
// and comments.
func compilePattern(line, base string) (pattern, bool) {
	raw := line

	// A line ending in an unescaped backslash-space keeps that space; otherwise
	// trailing whitespace is stripped. Leading whitespace is significant.
	line = trimTrailingUnescapedSpace(line)
	if line == "" {
		return pattern{}, false
	}
	// '#' starts a comment only unescaped at position 0.
	if line[0] == '#' {
		return pattern{}, false
	}
	if strings.HasPrefix(line, `\#`) {
		line = line[1:]
	}

	var p pattern
	p.raw = raw
	p.base = base

	if line[0] == '!' {
		p.negate = true
		line = line[1:]
		if line == "" {
			return pattern{}, false
		}
	} else if strings.HasPrefix(line, `\!`) {
		line = line[1:]
	}

	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimSuffix(line, "/")
		if line == "" {
			return pattern{}, false
		}
	}

	// A pattern is anchored to base when it contains a slash anywhere other than
	// the trailing one already stripped. "foo/bar" and "/foo" are anchored;
	// "foo" and "*.log" match at any depth.
	anchored := strings.Contains(line, "/")
	line = strings.TrimPrefix(line, "/")
	if line == "" {
		return pattern{}, false
	}

	p.segs = strings.Split(line, "/")
	if !anchored {
		p.segs = append([]string{"**"}, p.segs...)
	}
	return p, true
}

// trimTrailingUnescapedSpace strips trailing spaces and tabs unless the last one
// is escaped with a backslash.
func trimTrailingUnescapedSpace(s string) string {
	end := len(s)
	for end > 0 && (s[end-1] == ' ' || s[end-1] == '\t') {
		// Count preceding backslashes; an odd count escapes this space.
		bs := 0
		for i := end - 2; i >= 0 && s[i] == '\\'; i-- {
			bs++
		}
		if bs%2 == 1 {
			break
		}
		end--
	}
	return s[:end]
}

// match reports whether p matches the given root-relative slash path.
func (p pattern) match(relPath string, isDir bool) bool {
	if p.dirOnly && !isDir {
		return false
	}
	// The pattern only governs its own directory and below.
	rest := relPath
	if p.base != "" {
		if !strings.HasPrefix(relPath, p.base+"/") {
			return false
		}
		rest = relPath[len(p.base)+1:]
	}
	if rest == "" {
		return false
	}
	return matchSegments(p.segs, strings.Split(rest, "/"))
}

// matchSegments matches pattern segments against path segments, where "**"
// matches zero or more whole segments and every other segment is matched with
// path.Match (so "*" and "?" never cross a separator).
//
// Bottom-up DP rather than recursion: dp[i][j] is "pat[i:] matches segs[j:]".
// Depth and pattern length are both small, but a table makes the "**" case
// linear instead of exponential on patterns like "**/a/**/b/**".
func matchSegments(pat, segs []string) bool {
	m, n := len(pat), len(segs)
	dp := make([][]bool, m+1)
	for i := range dp {
		dp[i] = make([]bool, n+1)
	}
	dp[m][n] = true

	for i := m - 1; i >= 0; i-- {
		for j := n; j >= 0; j-- {
			if pat[i] == "**" {
				// Consume no segment, or consume one and stay on "**".
				dp[i][j] = dp[i+1][j] || (j < n && dp[i][j+1])
				continue
			}
			dp[i][j] = j < n && segMatch(pat[i], segs[j]) && dp[i+1][j+1]
		}
	}
	return dp[0][0]
}

// segMatch matches a single path segment against a single pattern segment.
// path.Match is the right primitive — '*' and '?' do not cross '/', character
// classes are supported, and backslash escapes work — and segments never contain
// '/' by construction. A malformed class falls back to a literal comparison,
// which is what git does with an unterminated bracket.
func segMatch(patSeg, seg string) bool {
	if patSeg == "*" {
		return true
	}
	if !strings.ContainsAny(patSeg, `*?[\`) {
		return patSeg == seg
	}
	ok, err := path.Match(translateClassNegation(patSeg), seg)
	if err != nil {
		return patSeg == seg
	}
	return ok
}

// translateClassNegation rewrites a negated character class from gitignore's
// syntax to Go's.
//
// gitignore follows fnmatch, where a class is negated with '!' — "[!a]". Go's
// path.Match negates with '^' — "[^a]" — and treats a leading '!' as a literal
// character to match. Without this translation "x[!a].txt" matches the literal
// filenames "x!.txt" and "xa.txt" instead of "any character except a", which is
// close enough to plausible that it would have shipped unnoticed.
func translateClassNegation(pat string) string {
	if !strings.Contains(pat, "[!") {
		return pat
	}
	var b strings.Builder
	b.Grow(len(pat))
	inClass := false
	for i := 0; i < len(pat); i++ {
		c := pat[i]
		if c == '\\' && i+1 < len(pat) {
			b.WriteByte(c)
			b.WriteByte(pat[i+1])
			i++
			continue
		}
		switch {
		case c == '[' && !inClass:
			inClass = true
			b.WriteByte('[')
			// '!' immediately after '[' is the negation marker. Anywhere else in
			// the class it is a literal and must be left alone.
			if i+1 < len(pat) && pat[i+1] == '!' {
				b.WriteByte('^')
				i++
			}
		case c == ']' && inClass:
			inClass = false
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// match returns whether the path is ignored. Later patterns override earlier
// ones, so a negation only wins if it appears after the exclusion that it
// reverses — git's rule, and the reason this cannot short-circuit on first hit.
func (s *ignoreSet) match(relPath string, isDir bool) bool {
	ignored := false
	for _, p := range s.pats {
		if p.match(relPath, isDir) {
			ignored = !p.negate
		}
	}
	return ignored
}

// add appends patterns, keeping .gitignore precedence: a nested file's patterns
// are consulted after the ones above it, so the deepest file wins a conflict.
func (s *ignoreSet) add(pats ...pattern) {
	s.pats = append(s.pats, pats...)
}

// clone returns a copy so a subtree can add nested patterns without leaking them
// back to its siblings.
func (s *ignoreSet) clone() *ignoreSet {
	out := &ignoreSet{pats: make([]pattern, len(s.pats))}
	copy(out.pats, s.pats)
	return out
}
