package manifest

import (
	"path"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// ExtractTSConfig reads a tsconfig.json or jsconfig.json for its module-resolution
// mapping.
//
// This file is read for exactly one reason: `compilerOptions.paths` is where a
// TypeScript codebase states what its own import specifiers mean. An import of
// `@fider/services` is `public/services` on disk and nothing but this file says so, so a
// resolver that does not read it reports every such import as unresolved. Measured on a
// real repository before this reader existed: 542 of 3912 edges absent, 14% of the graph,
// from a single unread mapping.
//
// Nothing else here becomes a fact. `strict`, `target`, and `lib` describe how a
// compiler behaves, which is not architecture, and a node per tsconfig would add a page
// nobody reads. The Facts this returns carries a resolution mapping and, when the file
// extends another, the path it extends — enough for the resolver and no more.
func ExtractTSConfig(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindTSConfig}
	root, err := ParseJSON(StripJSONC(f.Content))
	if err != nil {
		facts.markIncomplete("tsconfig did not parse: " + err.Error())
		return facts
	}

	dir := path.Dir(f.Path)
	if dir == "." {
		dir = ""
	}

	if ex := root.Get("extends").String(); ex != "" {
		// Resolved here rather than in the resolver, because this is the only place that
		// knows which file the relative path is relative to. A bare specifier —
		// `extends: "@tsconfig/node20/tsconfig.json"` — names a published package that is
		// not in the repository, so it is recorded as written and simply finds no parent.
		if strings.HasPrefix(ex, ".") {
			facts.Resolution.Extends = path.Clean(path.Join(dir, ex))
		} else {
			facts.Resolution.Extends = ex
		}
	}

	co := root.Get("compilerOptions")
	// baseUrl is what `paths` targets are relative to, and it is itself relative to this
	// file's directory. Absent, targets are relative to the file's directory, which is
	// what TypeScript does when `paths` appears without it.
	base := dir
	if b := co.Get("baseUrl").String(); b != "" {
		base = path.Clean(path.Join(dir, b))
		if base == "." {
			base = ""
		}
	}
	facts.Resolution.BaseURL = base

	paths := co.Get("paths")
	paths.Each(func(pattern string, targets *Node) bool {
		alias := Alias{Pattern: pattern, Line: lineOf(targets, paths.Line)}
		for _, t := range targets.Strings() {
			// Every target is resolved against baseUrl into a repo-relative path here, so
			// the resolver never has to know where the tsconfig sat. `./public/*` under
			// baseUrl `.` becomes `public/*`.
			resolved := path.Clean(path.Join(base, t))
			if resolved == "." {
				resolved = ""
			}
			alias.Targets = append(alias.Targets, resolved)
		}
		if len(alias.Targets) > 0 {
			facts.Resolution.Aliases = append(facts.Resolution.Aliases, alias)
		}
		return true
	})
	return facts
}

// StripJSONC replaces JSON-with-comments constructs with whitespace so a strict JSON
// parser accepts the result.
//
// tsconfig.json is JSONC, not JSON, and this is not an edge case: of 14 tsconfig files in
// one real repository, the two that declared `paths` both carried comments, and one ended
// its `paths` object with a trailing comma. A strict parse of either fails outright, which
// would make this reader silently useless on the files it exists for.
//
// Comments are replaced with spaces rather than removed, and newlines inside a block
// comment are preserved, so every byte after the comment keeps its line and column. Line
// numbers are provenance in a bundle — they are what a reader follows back to the source —
// and a stripper that shifted them would make every `paths` line number point at the wrong
// place.
//
// The scanner is string-aware because it has to be. `"https://aka.ms/tsconfig.json"` is a
// real value in a real tsconfig, and a regex for `//` deletes the rest of that line and
// everything structural on it. Escapes are tracked for the same reason: a `\"` inside a
// string does not end it.
func StripJSONC(src string) string {
	out := []byte(src)
	inString, escaped := false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			for ; i < len(out) && out[i] != '\n'; i++ {
				out[i] = ' '
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			out[i], out[i+1] = ' ', ' '
			i += 2
			for ; i < len(out); i++ {
				if out[i] == '*' && i+1 < len(out) && out[i+1] == '/' {
					out[i], out[i+1] = ' ', ' '
					i++
					break
				}
				// Newlines survive, so nothing below this comment moves lines.
				if out[i] != '\n' {
					out[i] = ' '
				}
			}
		}
	}
	return stripTrailingCommas(string(out))
}

// stripTrailingCommas blanks a comma that is followed only by whitespace and a closing
// brace or bracket, which JSONC permits and JSON does not.
//
// Run after comment stripping rather than woven into it, because a trailing comma is
// routinely separated from its brace by a comment — `"src/*"], // note\n}` — and only
// once the comment is whitespace does the comma become visibly trailing.
func stripTrailingCommas(src string) string {
	out := []byte(src)
	inString, escaped := false, false
	for i := 0; i < len(out); i++ {
		c := out[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		if c == '"' {
			inString = true
			continue
		}
		if c != ',' {
			continue
		}
		for j := i + 1; j < len(out); j++ {
			switch out[j] {
			case ' ', '\t', '\r', '\n':
				continue
			case '}', ']':
				out[i] = ' '
			}
			break
		}
	}
	return string(out)
}
