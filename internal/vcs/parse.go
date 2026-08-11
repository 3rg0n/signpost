package vcs

import "strings"

// The record separator inside the pretty format. A unit separator (0x1f) rather than a
// tab or a pipe, because an author name can contain either and cannot contain this.
const fieldSep = "\x1f"

// parseLog parses the output of the `git log` invocation logArgs builds.
//
// The format is `-z` throughout, which is what makes this parseable at all: git quotes
// paths containing spaces, quotes, or non-ASCII bytes when it writes them line-delimited,
// and the quoting is C-style with octal escapes. Under `-z` it writes them raw and
// NUL-separated, so a path is whatever bytes lie between two NULs and no unquoting is
// needed. A repository is untrusted input and a filename is the part of it most likely to
// be adversarial, so not having to unquote is a correctness property rather than a
// convenience.
//
// The stream `-z --numstat --pretty=format:H<US>date<US>author<US>subject<US>` produces,
// verified against git rather than inferred from its documentation:
//
//	<hash><US><date><US><author><US><subject><US>\n
//	<ins>\t<del>\t<path>\0
//	<ins>\t<del>\t<path>\0
//	\0                                  <- separates this commit from the next header
//	<hash><US><date><US><author><US><subject><US>\n
//	...
//
// Three details in that shape drive the implementation, and each one is a test:
//
//   - The header ends with `\n`, not `\0`, because `--pretty=format:` places a newline
//     between the format output and the numstat block. So a token that contains a `\x1f`
//     is a header, possibly with a numstat line trailing after the newline.
//   - A rename is `<ins>\t<del>\t\0<old>\0<new>\0` — the path field is empty and the two
//     paths follow as their own tokens. Both are kept. The new path is what the file is
//     called now and is what the signal is keyed by, but the old one cannot be dropped:
//     git marks the rename only in the commit that performed it, and every older commit
//     still names the old path. aggregate uses the pair to fold the two halves together.
//     (`--follow` would do this in git, but only for a single pathspec.)
//   - An empty commit contributes a header and no numstat, so a header may be directly
//     followed by another header.
func parseLog(out string) []commit {
	var commits []commit
	var cur *commit

	// pendingRename holds a rename's line counts between the empty path field and the
	// two path tokens that follow it.
	var pendingRename *fileChange
	// renameOld is set once the old path has been consumed, so the next token is known
	// to be the new path.
	renameOld := false

	for _, tok := range strings.Split(out, "\x00") {
		if tok == "" {
			continue
		}

		// A header carries the field separator; a numstat line never can, because git
		// writes it as two counts and a path separated by tabs.
		if strings.Contains(tok, fieldSep) {
			// git emits the header and, after a newline, the first numstat line of the
			// same commit within one NUL-delimited token. Split at the first newline:
			// everything before it is the header, anything after is a numstat line.
			head, rest, hasRest := strings.Cut(tok, "\n")
			fields := strings.Split(head, fieldSep)
			if len(fields) < 5 {
				// Not the header this code wrote the format for. Skipping rather than
				// guessing: a partial header would attribute changes to the wrong
				// commit, which is worse than a commit going uncounted.
				continue
			}
			if cur != nil {
				commits = append(commits, *cur)
			}
			// Positional up to the author, then the whole remainder is the subject. A
			// separator inside a subject is legal — git accepts every byte but NUL in a
			// message — so the field count can exceed five, and rejoining the tail is what
			// keeps `feat: a<US>b` from being read as a five-field header with a spare piece
			// that shifts nothing. The format's trailing separator makes the last element
			// empty, which is why the join stops one short of the end.
			//
			// A separator in the *author name* is also legal and still shifts: its tail lands
			// in the subject. That is the whole reason logArgs orders the fields the way it
			// does. What used to be downstream of a shifted author was the date, which is
			// written onto every module page as first_commit and last_commit; what is
			// downstream now is a subject this package only ever counts and never stores. A
			// name with a separator in it therefore costs at most one miscounted commit
			// instead of a page asserting that a directory was first touched on "il".
			cur = &commit{hash: fields[0], date: fields[1], author: fields[2]}
			cur.subject = strings.Join(fields[3:len(fields)-1], fieldSep)
			pendingRename, renameOld = nil, false
			if hasRest && strings.TrimSpace(rest) != "" {
				applyNumstat(cur, rest, &pendingRename, &renameOld)
			}
			continue
		}

		if cur == nil {
			// A numstat line before any header. Cannot be attributed, so it is dropped
			// rather than assigned to a commit that was not stated.
			continue
		}
		applyNumstat(cur, tok, &pendingRename, &renameOld)
	}
	if cur != nil {
		commits = append(commits, *cur)
	}
	return commits
}

// applyNumstat consumes one token of the numstat stream, which is either a
// counts-and-path line or one half of a rename's path pair.
func applyNumstat(c *commit, tok string, pendingRename **fileChange, renameOld *bool) {
	// Mid-rename: the two tokens after an empty path field are the old and new paths.
	if *pendingRename != nil {
		if !*renameOld {
			// The old path, kept rather than discarded: older commits still name it, and
			// this token is the only place git states the link. aggregate folds it forward.
			(*pendingRename).oldPath = normalizePath(tok)
			*renameOld = true
			return
		}
		fc := **pendingRename
		fc.path = normalizePath(tok)
		if fc.path != "" {
			c.files = append(c.files, fc)
		}
		*pendingRename, *renameOld = nil, false
		return
	}

	// `<insertions>\t<deletions>\t<path>`, where a binary file has `-` for both counts
	// and a rename has an empty path.
	parts := strings.SplitN(strings.TrimLeft(tok, "\n"), "\t", 3)
	if len(parts) < 3 {
		return
	}
	fc := fileChange{binary: parts[0] == "-" || parts[1] == "-"}
	if !fc.binary {
		fc.insertions = atoiOr(parts[0], 0)
		fc.deletions = atoiOr(parts[1], 0)
	}
	if parts[2] == "" {
		// A rename: the paths arrive as the next two tokens.
		cp := fc
		*pendingRename, *renameOld = &cp, false
		return
	}
	fc.path = normalizePath(parts[2])
	if fc.path != "" {
		c.files = append(c.files, fc)
	}
}

// normalizePath trims the newline git leaves on a numstat line and rejects anything that
// is not a plain repo-relative path.
//
// git paths are already slash-separated and repository-relative on every platform, so
// this is a guard rather than a conversion: an absolute path or one climbing out of the
// tree cannot come from `git log` in a well-formed repository, and accepting one would
// let history attach signals to a path outside the analysed tree.
func normalizePath(p string) string {
	p = strings.Trim(p, "\n\r")
	if p == "" || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "../") || p == ".." {
		return ""
	}
	if strings.Contains(p, "\\") {
		// git writes forward slashes everywhere. A backslash here means a filename
		// genuinely containing one, which is legal on Linux; it is left alone rather
		// than translated, because rewriting it would produce a path that does not
		// exist.
		return p
	}
	return p
}
