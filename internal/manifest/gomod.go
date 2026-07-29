package manifest

import "strings"

// A reader for go.mod and go.work.
//
// go.mod is its own small grammar — not YAML, TOML, or JSON — so it needs its own
// reader. `golang.org/x/mod/modfile` would do this properly, but it would be a new
// direct dependency for a grammar with six directives and two forms, and design §2's
// dependency policy is explicit that the cost of a dependency is the remediation
// path, not the code it saves. A hand-written reader is the cheaper call here, and it
// keeps this package at zero dependencies alongside the rest.
//
// The grammar in full: a directive is either a single line (`require x v1`) or a
// parenthesised block (`require (` … `)`). Line comments start with `//`. That is
// everything, which is why this is worth writing rather than taking on.

// GoMod is a parsed go.mod or go.work file.
//
// A struct rather than a Node tree because go.mod has no nesting to speak of — the
// only structure is directive-to-lines, and a tree would add indirection over a
// document that is already flat.
type GoMod struct {
	// Module is the module path, empty in a go.work file.
	Module string
	// Go is the language version from the `go` directive.
	Go string
	// Toolchain is the pinned toolchain, when one is declared. Worth recording
	// separately: `go 1.24` with `toolchain go1.26.5` means the build uses 1.26.5
	// while the language semantics are 1.24's, and the two answer different
	// questions.
	Toolchain string
	// Requires are the require directives, direct and indirect.
	Requires []GoModRequire
	// Replaces are replace directives. These are load-bearing architectural facts:
	// a replace pointing at a local path is how a monorepo wires its own modules
	// together, and one pointing at a fork is a dependency the team has taken over
	// maintenance of.
	Replaces []GoModReplace
	// Excludes are excluded versions.
	Excludes []GoModRequire
	// Retracts are retracted versions of this module itself.
	Retracts []string
	// Uses are go.work `use` directives: the workspace's member modules.
	Uses []string
	// ModuleLine, GoLine record where the identity directives were, for provenance.
	ModuleLine int
	GoLine     int
}

// GoModRequire is one require or exclude entry.
type GoModRequire struct {
	Path    string
	Version string
	// Indirect is set by the `// indirect` marker, which is how the go tool records
	// a dependency no package in this module imports directly.
	Indirect bool
	Line     int
}

// GoModReplace is one replace directive.
type GoModReplace struct {
	// OldPath and OldVersion are the left side; OldVersion is empty for a
	// wildcard replace that covers every version.
	OldPath    string
	OldVersion string
	// NewPath is a module path or, when it starts with ./ or ../, a local
	// directory.
	NewPath    string
	NewVersion string
	Line       int
}

// Local reports whether a replace points at a directory in this repository rather
// than at another module. This is the fact that turns a replace into a graph edge:
// the target is a path in the tree, so it resolves to real files.
func (r GoModReplace) Local() bool {
	return strings.HasPrefix(r.NewPath, "./") || strings.HasPrefix(r.NewPath, "../") ||
		strings.HasPrefix(r.NewPath, "/") || r.NewPath == "."
}

// ParseGoMod reads a go.mod or go.work file.
//
// Never fails, on the same grounds as the other readers: a malformed line is noted
// and skipped.
func ParseGoMod(src string) (*GoMod, Diag) {
	var diag Diag
	m := &GoMod{}

	// block is the directive a parenthesised group belongs to, empty outside one.
	block := ""
	for i, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		num := i + 1
		line := strings.TrimSpace(stripGoModComment(raw))
		indirect := goModIndirect(raw)
		if line == "" {
			continue
		}

		if block != "" {
			if line == ")" {
				block = ""
				continue
			}
			m.addEntry(block, line, indirect, num, &diag)
			continue
		}

		verb, rest := splitFirstToken(line)
		rest = strings.TrimSpace(rest)
		if rest == "(" {
			switch verb {
			case "require", "replace", "exclude", "retract", "use":
				block = verb
			default:
				diag.note(num, "unsupported block directive "+verb)
				block = verb
			}
			continue
		}
		switch verb {
		case "module":
			m.Module = trimGoModQuotes(rest)
			m.ModuleLine = num
		case "go":
			m.Go = rest
			m.GoLine = num
		case "toolchain":
			m.Toolchain = rest
		case "require", "replace", "exclude", "retract", "use":
			m.addEntry(verb, rest, indirect, num, &diag)
		case "godebug", "ignore", "tool":
			// Later additions to the grammar with no bearing on the facts §4.1 asks
			// for. Skipped silently rather than noted: a diagnostic here would mark
			// every modern go.mod incomplete for something that was never missing.
		default:
			diag.note(num, "unrecognised directive "+verb)
		}
	}
	if block != "" {
		diag.note(0, "unclosed "+block+" block")
	}
	return m, diag
}

// addEntry records one line belonging to a directive.
func (m *GoMod) addEntry(verb, line string, indirect bool, num int, diag *Diag) {
	switch verb {
	case "require", "exclude":
		path, ver := splitFirstToken(line)
		path = trimGoModQuotes(path)
		ver = trimGoModQuotes(strings.TrimSpace(ver))
		if path == "" {
			diag.note(num, "empty "+verb+" path")
			return
		}
		e := GoModRequire{Path: path, Version: ver, Indirect: indirect, Line: num}
		if verb == "require" {
			m.Requires = append(m.Requires, e)
		} else {
			m.Excludes = append(m.Excludes, e)
		}
	case "replace":
		r, ok := parseGoModReplace(line, num)
		if !ok {
			diag.note(num, "unreadable replace directive")
			return
		}
		m.Replaces = append(m.Replaces, r)
	case "retract":
		m.Retracts = append(m.Retracts, strings.TrimSpace(line))
	case "use":
		m.Uses = append(m.Uses, trimGoModQuotes(strings.TrimSpace(line)))
	}
}

// parseGoModReplace splits a replace directive around its `=>`.
//
// Both sides are `path [version]`, and the version is optional on each side
// independently: `replace a => ../a` is a wildcard replace onto a local directory,
// which is the monorepo case and the most common one in practice.
func parseGoModReplace(line string, num int) (GoModReplace, bool) {
	arrow := strings.Index(line, "=>")
	if arrow < 0 {
		return GoModReplace{}, false
	}
	oldPath, oldVer := splitPathVersion(line[:arrow])
	newPath, newVer := splitPathVersion(line[arrow+2:])
	if oldPath == "" || newPath == "" {
		return GoModReplace{}, false
	}
	return GoModReplace{
		OldPath: oldPath, OldVersion: oldVer,
		NewPath: newPath, NewVersion: newVer,
		Line: num,
	}, true
}

// splitPathVersion splits "path" or "path version".
func splitPathVersion(s string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(s))
	switch len(fields) {
	case 0:
		return "", ""
	case 1:
		return trimGoModQuotes(fields[0]), ""
	default:
		return trimGoModQuotes(fields[0]), trimGoModQuotes(fields[1])
	}
}

// stripGoModComment removes a `//` comment.
//
// go.mod has no string literals a `//` could hide inside — a quoted module path
// cannot contain one — so unlike the YAML and TOML strippers this needs no quote
// tracking.
func stripGoModComment(s string) string {
	if i := strings.Index(s, "//"); i >= 0 {
		return s[:i]
	}
	return s
}

// goModIndirect reports whether a line carries the `// indirect` marker.
//
// Read from the raw line, before the comment is stripped, because the marker *is* a
// comment. It is the only comment in the grammar that carries meaning, and it is the
// one that separates the dependencies a human chose from the ones the tool resolved.
func goModIndirect(raw string) bool {
	i := strings.Index(raw, "//")
	if i < 0 {
		return false
	}
	for _, f := range strings.Fields(raw[i+2:]) {
		if f == "indirect" {
			return true
		}
	}
	return false
}

// trimGoModQuotes removes the optional quotes around a module path.
func trimGoModQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
