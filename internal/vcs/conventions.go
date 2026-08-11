package vcs

import "strings"

// Classifying commit subjects.
//
// Everything in this file is pure and returns integers. No subject reaches an exported type
// and none is written anywhere — Conventions says why that is the security boundary rather
// than an economy, and it is what lets this package read a field that is arbitrary bytes
// from an untrusted repository without owning an escaping problem.
//
// The classification is deliberately conservative in one direction: a subject this file does
// not recognise counts toward Subjects and toward nothing else. A false negative understates
// adoption, which a reader can see in the rate; a false positive would claim a repository
// follows a convention it does not.
//
// Not done in git with `--grep`, which was the cheaper-looking option and is wrong here for
// two reasons. `--grep` matches any line of the whole message, not the subject, so
// `^revert:` matches a body quoting an earlier subject and `^fix:` matches a bullet in a
// commit body — measured on a real repository, `^Co-Authored-By` matched 528 of 2000 commits
// where the subject can never contain it. And each pattern is its own `git rev-list`, at a
// process spawn apiece, to re-derive what one already-running walk has in hand.

// countConventions classifies the subjects of a walk.
//
// A commit with no subject is not counted at all — that is a header that did not carry one,
// so including it in the denominator would report a repository as less disciplined than it is
// for a parsing reason.
func countConventions(commits []commit) Conventions {
	var c Conventions
	for _, cm := range commits {
		s := strings.TrimSpace(cm.subject)
		if s == "" {
			continue
		}
		c.Subjects++
		if strings.Contains(s, "#") && hasIssueRef(s) {
			c.IssueRefs++
		}
		typ, ok := conventionalType(s)
		if !ok {
			// git's own revert wording, which is what `git revert` writes and is therefore
			// how a repository that does not use conventional commits still spells one.
			if strings.HasPrefix(s, `Revert "`) {
				c.Reverts++
			}
			continue
		}
		c.Conventional++
		switch typ {
		case "fix":
			c.Fixes++
		case "feat":
			c.Features++
		case "revert":
			c.Reverts++
		}
	}
	return c
}

// maxTypeLen bounds how far conventionalType scans for the colon.
//
// A subject is untrusted input of unbounded length, and without this a 200KB single-line
// message would be scanned in full to conclude it is not a type. Twenty is past every type in
// the Conventional Commits vocabulary and every one in the wild worth matching.
const maxTypeLen = 20

// conventionalType returns the lowercased type of a Conventional Commits subject.
//
// The shape is `type(optional scope)!: description`. Implemented by hand rather than with a
// regexp because it is three character-class checks and the standard library's regexp would
// be compiled once to answer a question `strings` answers directly — the same call ADR 0022
// makes about parsers.
//
// The space after the colon is required, which is the spec's own rule and also the thing that
// keeps `http://x` and `TODO:something` from being read as types.
func conventionalType(s string) (string, bool) {
	limit := len(s)
	if limit > maxTypeLen {
		limit = maxTypeLen
	}
	i := 0
	for i < limit && isAlpha(s[i]) {
		i++
	}
	// A type must have at least one letter, and the letters must be followed by something:
	// a bare word is not a conventional subject.
	if i == 0 || i == len(s) {
		return "", false
	}
	typ := strings.ToLower(s[:i])

	// An optional scope in parentheses. Its content is not read — a scope is a project's own
	// vocabulary and means nothing outside the project — only skipped, and an unclosed one
	// makes the subject unconventional rather than being tolerated.
	if s[i] == '(' {
		close := strings.IndexByte(s[i:], ')')
		if close < 0 {
			return "", false
		}
		i += close + 1
		if i == len(s) {
			return "", false
		}
	}
	// The breaking-change marker, which may appear with or without a scope.
	if s[i] == '!' {
		i++
	}
	if !strings.HasPrefix(s[i:], ": ") {
		return "", false
	}
	return typ, true
}

// hasIssueRef reports whether the subject names an issue or pull request as `#N`.
//
// A digit is required after the `#`, so a subject mentioning a C preprocessor directive or a
// markdown heading does not count as a tracker reference. The number itself is not parsed:
// which issue it is would be a link into a forge this package does not know, and the useful
// fact is that the commit is traceable at all.
func hasIssueRef(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '#' && isDigit(s[i+1]) {
			return true
		}
	}
	return false
}

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
