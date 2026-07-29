package manifest

import "strings"

// A reader for the TOML subset that Cargo and pyproject manifests use.
//
// Hand-written for the same reason the graph algorithms are (design §2, §4.4): the
// subset actually needed is small, and it is a strictly better trade than a
// dependency whose remediation path we do not control. What is needed is tables,
// dotted keys, inline tables, arrays, and strings — which is the whole of
// `Cargo.toml` and the whole of `pyproject.toml`'s `[project]` and
// `[tool.*]` sections.
//
// What is deliberately not implemented is anything with no bearing on the facts
// being extracted: date-times, integer bases, float special values. Those parse as
// scalar text, which is exactly right here, since a manifest's `edition = "2021"`
// and `version = 2` are both just recorded.
//
// Arrays of tables (`[[bin]]`) are supported, because they are how a Cargo manifest
// declares multiple binaries — and entrypoints are one of the facts §4.1 asks this
// package for.

// ParseTOML parses a TOML document into a mapping node.
//
// Like ParseYAML, never fails: unreadable lines are noted and skipped so one
// malformed manifest does not fail a build.
func ParseTOML(src string) (*Node, Diag) {
	var diag Diag
	root := mapNode(1)
	// cur is the table new keys land in.
	cur := root

	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		num := i + 1
		// A triple-quoted string may span lines — pyproject files use them for
		// descriptions and inline scripts. Gather it before anything else looks at
		// the text, so a `#` or a bracket inside the body cannot be mistaken for a
		// comment or an unbalanced array.
		code, open := scanTOMLLine(lines[i], "")
		for open != "" && i+1 < len(lines) {
			i++
			var more string
			more, open = scanTOMLLine(lines[i], open)
			code += "\n" + more
		}
		if open != "" {
			diag.note(num, "unterminated multi-line string")
		}
		line := strings.TrimSpace(code)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[[") {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "[["), "]]"))
			path, ok := splitTOMLKeyPath(name)
			if !ok {
				diag.note(num, "unreadable array-of-tables header")
				continue
			}
			cur = appendTableArray(root, path, num)
			continue
		}
		if strings.HasPrefix(line, "[") {
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]"))
			path, ok := splitTOMLKeyPath(name)
			if !ok {
				diag.note(num, "unreadable table header")
				continue
			}
			cur = descend(root, path, num)
			continue
		}

		eq := findTOMLEquals(line)
		if eq < 0 {
			diag.note(num, "line is neither a table header nor a key/value pair")
			continue
		}
		path, ok := splitTOMLKeyPath(strings.TrimSpace(line[:eq]))
		if !ok {
			diag.note(num, "unreadable key")
			continue
		}
		rhs := strings.TrimSpace(line[eq+1:])
		// A value's brackets may be unbalanced because it wraps across lines — which
		// every real dependency array does once it exceeds a line's width.
		if bal := tomlBalance(rhs, 0); bal > 0 {
			joined := rhs
			for i+1 < len(lines) && bal > 0 {
				i++
				next, stillOpen := scanTOMLLine(lines[i], "")
				// A triple-quoted element inside the array carries its own
				// continuation, and its body must not be scanned for brackets.
				for stillOpen != "" && i+1 < len(lines) {
					i++
					var more string
					more, stillOpen = scanTOMLLine(lines[i], stillOpen)
					next += "\n" + more
				}
				joined += " " + strings.TrimSpace(next)
				bal = tomlBalance(next, bal)
			}
			rhs = joined
		}
		val, _ := parseTOMLValue(rhs, 0, num, &diag)
		if val == nil {
			diag.note(num, "unreadable value")
			continue
		}
		// A dotted key defines the intermediate tables it names.
		target := cur
		if len(path) > 1 {
			target = descend(cur, path[:len(path)-1], num)
		}
		target.set(path[len(path)-1], val)
	}
	return root, diag
}

// descend walks or creates a chain of tables.
func descend(root *Node, path []string, line int) *Node {
	cur := root
	for _, seg := range path {
		next := cur.Get(seg)
		// An array of tables addressed by name refers to its last element, which is
		// how `[[bin]]` followed by `name = ...` works.
		if next != nil && next.Kind == KindSeq && len(next.Items) > 0 {
			last := next.Items[len(next.Items)-1]
			if last.Kind == KindMap {
				cur = last
				continue
			}
		}
		if next == nil || next.Kind != KindMap {
			next = mapNode(line)
			cur.set(seg, next)
		}
		cur = next
	}
	return cur
}

// appendTableArray adds a new element to an array of tables and returns it.
func appendTableArray(root *Node, path []string, line int) *Node {
	parent := root
	if len(path) > 1 {
		parent = descend(root, path[:len(path)-1], line)
	}
	key := path[len(path)-1]
	arr := parent.Get(key)
	if arr == nil || arr.Kind != KindSeq {
		arr = seqNode(line)
		parent.set(key, arr)
	}
	entry := mapNode(line)
	arr.Items = append(arr.Items, entry)
	return entry
}

// splitTOMLKeyPath splits a dotted key into its segments, unquoting each.
func splitTOMLKeyPath(s string) ([]string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false
	}
	var out []string
	var cur strings.Builder
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote != 0:
			if c == '\\' && inQuote == '"' && i+1 < len(s) {
				cur.WriteByte(c)
				i++
				cur.WriteByte(s[i])
				continue
			}
			if c == inQuote {
				inQuote = 0
				continue
			}
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			inQuote = c
		case c == '.':
			seg := strings.TrimSpace(cur.String())
			if seg == "" {
				return nil, false
			}
			out = append(out, seg)
			cur.Reset()
		case c == ' ' || c == '\t':
			// Space around a dot is legal; space inside a bare key is not, but
			// tolerating it costs nothing and reading it strictly would drop the key.
		default:
			cur.WriteByte(c)
		}
	}
	if seg := strings.TrimSpace(cur.String()); seg != "" {
		out = append(out, seg)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// findTOMLEquals locates the `=` separating key from value, skipping quoted keys.
func findTOMLEquals(line string) int {
	inQuote := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case inQuote != 0:
			if c == '\\' && inQuote == '"' {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == '=':
			return i
		}
	}
	return -1
}

// parseTOMLValue parses one value at s[at], returning the node and the index just
// past it.
func parseTOMLValue(s string, at int, line int, diag *Diag) (*Node, int) {
	at = skipFlowSpace(s, at)
	if at >= len(s) {
		return nil, at
	}
	switch s[at] {
	case '{':
		n := mapNode(line)
		at++
		for {
			at = skipFlowSpace(s, at)
			if at >= len(s) {
				diag.note(line, "unterminated inline table")
				return n, at
			}
			if s[at] == '}' {
				return n, at + 1
			}
			if s[at] == ',' {
				at++
				continue
			}
			keyEnd := at
			for keyEnd < len(s) && s[keyEnd] != '=' && s[keyEnd] != '}' {
				keyEnd++
			}
			if keyEnd >= len(s) || s[keyEnd] == '}' {
				diag.note(line, "inline table entry has no value")
				return n, keyEnd
			}
			path, ok := splitTOMLKeyPath(s[at:keyEnd])
			if !ok {
				diag.note(line, "unreadable inline table key")
				return n, keyEnd
			}
			var val *Node
			val, at = parseTOMLValue(s, keyEnd+1, line, diag)
			if val == nil {
				return n, at
			}
			target := n
			if len(path) > 1 {
				target = descend(n, path[:len(path)-1], line)
			}
			target.set(path[len(path)-1], val)
		}
	case '[':
		n := seqNode(line)
		at++
		for {
			at = skipFlowSpace(s, at)
			if at >= len(s) {
				diag.note(line, "unterminated array")
				return n, at
			}
			if s[at] == ']' {
				return n, at + 1
			}
			if s[at] == ',' {
				at++
				continue
			}
			var item *Node
			item, at = parseTOMLValue(s, at, line, diag)
			if item == nil {
				return n, at
			}
			n.Items = append(n.Items, item)
		}
	case '"', '\'':
		text, next := readTOMLString(s, at)
		return scalarNode(text, true, line), next
	}
	// A bare value: number, bool, or date. Kept as text.
	start := at
	for at < len(s) && s[at] != ',' && s[at] != ']' && s[at] != '}' {
		at++
	}
	return scalarNode(strings.TrimSpace(s[start:at]), false, line), at
}

// readTOMLString reads a quoted string, handling both basic and literal forms and
// their triple-quoted variants.
func readTOMLString(s string, at int) (string, int) {
	q := s[at]
	// Triple-quoted, which pyproject files use for long descriptions and scripts.
	if at+2 < len(s) && s[at+1] == q && s[at+2] == q {
		delim := s[at : at+3]
		if end := strings.Index(s[at+3:], delim); end >= 0 {
			body := s[at+3 : at+3+end]
			if q == '"' {
				body = unescapeYAMLDouble(body)
			}
			return strings.TrimPrefix(body, "\n"), at + 3 + end + 3
		}
		// Unterminated: the rest of the line is the value. TOML allows the body to
		// span lines, but this reader is line-oriented, so it says so.
		return s[at+3:], len(s)
	}
	for i := at + 1; i < len(s); i++ {
		if q == '"' && s[i] == '\\' {
			i++
			continue
		}
		if s[i] == q {
			body := s[at+1 : i]
			if q == '"' {
				body = unescapeYAMLDouble(body)
			}
			return body, i + 1
		}
	}
	return s[at+1:], len(s)
}

// scanTOMLLine removes a trailing comment from one line and reports whether a
// multi-line string is still open at its end.
//
// open is the delimiter carried in from the previous line (either triple-quote form), empty
// when none, and the same on the way out. This is the cross-line state a
// line-oriented reader needs to handle a triple-quoted value at all: without it a
// `#` or a `[` inside a description body reads as a comment or an unbalanced array,
// and the keys after the body are lost.
//
// Unlike YAML, TOML's `#` begins a comment anywhere outside a string, so no
// preceding-whitespace rule applies.
func scanTOMLLine(line, open string) (string, string) {
	if open != "" {
		// Inside a multi-line string: everything up to the terminator is content,
		// and nothing in it can be a comment.
		if end := strings.Index(line, open); end >= 0 {
			rest, stillOpen := scanTOMLLine(line[end+len(open):], "")
			return line[:end+len(open)] + rest, stillOpen
		}
		return line, open
	}
	inQuote := byte(0)
	triple := ""
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case triple != "":
			if strings.HasPrefix(line[i:], triple) {
				i += len(triple) - 1
				triple, inQuote = "", 0
			}
		case inQuote != 0:
			if c == '\\' && inQuote == '"' {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			if i+2 < len(line) && line[i+1] == c && line[i+2] == c {
				triple = line[i : i+3]
				inQuote = c
				i += 2
				continue
			}
			inQuote = c
		case c == '#':
			return line[:i], ""
		}
	}
	return line, triple
}

// tomlBalance returns the bracket depth after scanning s, for joining a value that
// wraps across lines.
func tomlBalance(s string, depth int) int {
	inQuote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote != 0:
			if c == '\\' && inQuote == '"' {
				i++
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
		case c == '"' || c == '\'':
			inQuote = c
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
		}
	}
	if depth < 0 {
		return 0
	}
	return depth
}
