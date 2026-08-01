package okf

import (
	"sort"
	"strconv"
	"strings"
)

// A byte-stable emitter for the YAML subset frontmatter needs.
//
// # Why this is hand-written
//
// Design §6.3 makes writing and reading asymmetric on purpose, and this is the writing
// half: scalars, flow mappings, and block sequences of flow mappings, which is the whole
// of what §3.1's page shape uses. Byte-stability is a hard requirement (§8.1) because the
// bundle is committed, and no general-purpose library guarantees identical bytes across
// its own minor versions — line-wrap width, quoting heuristics, and key ordering are all
// things a library is free to change without breaking its own contract. Owning a hundred
// lines of emitter is cheaper than a diff-churn bug that only appears after a dependency
// bump. It is also the reason ADR 0002's rule is satisfiable here rather than an
// inconvenience: there is nothing to bump.
//
// # What it deliberately cannot do
//
// No anchors, no aliases, no merge keys, no block scalars, no multi-document output. A
// caller that needs any of those is writing something this format was not designed to
// carry, and the emitter says so by not offering it. Multi-line strings are the one case
// where that bites: a description containing a newline is *folded to a space* rather than
// emitted as a block scalar, because a block scalar's indentation and chomping are where
// hand-written emitters get subtly wrong output. Frontmatter values here are titles,
// paths, and one-sentence descriptions; the prose that genuinely needs newlines lives in
// the page body, which is not YAML at all.
//
// The reader for this is internal/manifest's tolerant parser (ADR 0001), and yamlRound
// in the tests asserts the two agree — an emitter whose own reader cannot read its output
// would break `verify` rather than the emit path, which is the harder failure to find.

// yamlDoc builds frontmatter as an ordered list of top-level keys.
//
// Ordered rather than a map, and the order is the caller's rather than sorted, because
// frontmatter is read by humans and `type` before `title` before `description` is the
// order §3.1 shows. Sorting would put `description` first and `type` sixth, which is
// stable but unreadable. Determinism comes from the caller writing keys in a fixed
// sequence, which is a straight line of code rather than a traversal.
type yamlDoc struct {
	entries []yamlEntry
}

type yamlEntry struct {
	key string
	val yamlValue
}

// yamlValue is one of: a scalar, a flow mapping, or a sequence. A closed set rather than
// an interface with an exported method, so the emitter's supported surface is a thing you
// can read in one screen and nothing outside this package can extend it.
type yamlValue struct {
	kind yamlKind
	// scalar holds the text for kindScalar.
	scalar string
	// pairs holds a flow mapping's entries in caller order.
	pairs []yamlPair
	// items holds a sequence's entries.
	items []yamlValue
	// literal marks a scalar that must be emitted unquoted whatever it looks like,
	// for the one case where the value is structural: an integer count.
	literal bool
}

type yamlKind uint8

const (
	kindScalar yamlKind = iota
	// kindFlowMap is a one-line mapping: `{ by: signpost, at: 2026-07-30 }`.
	kindFlowMap
	// kindFlowSeq is a one-line sequence of scalars: `[go, security-boundary]`.
	kindFlowSeq
	// kindSeq is a block sequence, one entry per line.
	kindSeq
)

type yamlPair struct {
	key string
	val yamlValue
}

func scalar(s string) yamlValue { return yamlValue{kind: kindScalar, scalar: s} }

// number emits an integer unquoted. Separate from scalar because quoting a count would
// make a consumer read "14" as text and lose the arithmetic, and because passing an int
// through strconv at the call site would then need the literal flag set by hand.
func number(n int) yamlValue {
	return yamlValue{kind: kindScalar, scalar: strconv.Itoa(n), literal: true}
}

func flowMap(pairs ...yamlPair) yamlValue {
	return yamlValue{kind: kindFlowMap, pairs: pairs}
}

func seq(items ...yamlValue) yamlValue { return yamlValue{kind: kindSeq, items: items} }

// set appends a key. A caller that would set an empty value should not call this: an
// empty frontmatter key renders as a label with nothing after it, which reads as a
// missing fact rather than an absent one.
func (d *yamlDoc) set(key string, v yamlValue) {
	d.entries = append(d.entries, yamlEntry{key: key, val: v})
}

// setScalar sets a scalar key, skipping it entirely when the text is empty.
func (d *yamlDoc) setScalar(key, val string) {
	if val == "" {
		return
	}
	d.set(key, scalar(val))
}

// setStrings sets a key to a flow sequence of scalars, skipping an empty list.
//
// Flow rather than block for a list of short strings: `tags: [go, security]` is one line
// a human reads at a glance, where a block sequence spends four. The threshold is not
// length-based — a fixed choice per key keeps the output diffable, since a list that
// switched representation at some width would produce a whole-block diff when one tag
// was added.
func (d *yamlDoc) setStrings(key string, vals []string) {
	if len(vals) == 0 {
		return
	}
	items := make([]yamlValue, 0, len(vals))
	for _, v := range vals {
		items = append(items, scalar(v))
	}
	d.set(key, yamlValue{kind: kindFlowSeq, items: items})
}

// String renders the document. No leading or trailing `---`: the caller owns the
// frontmatter fence, because it also owns what follows it.
func (d *yamlDoc) String() string {
	var b strings.Builder
	for _, e := range d.entries {
		writeYAMLEntry(&b, e.key, e.val)
	}
	return b.String()
}

// writeYAMLEntry renders one top-level key. Only top-level: the subset has exactly one
// nesting level, and it is the block sequence below, so there is no indent parameter to
// get wrong.
func writeYAMLEntry(b *strings.Builder, key string, v yamlValue) {
	switch v.kind {
	case kindScalar:
		b.WriteString(key + ": " + quoteYAML(v.scalar, v.literal) + "\n")
	case kindFlowSeq:
		b.WriteString(key + ": [")
		for i, it := range v.items {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(quoteYAML(it.scalar, it.literal))
		}
		b.WriteString("]\n")
	case kindFlowMap:
		b.WriteString(key + ": " + renderFlowMap(v.pairs) + "\n")
	case kindSeq:
		b.WriteString(key + ":\n")
		for _, it := range v.items {
			switch it.kind {
			case kindFlowMap:
				b.WriteString("  - " + renderFlowMap(it.pairs) + "\n")
			case kindFlowSeq, kindSeq:
				// A nested sequence has no representation in the subset. Skipped rather
				// than rendered as something a reader would misparse — and unreachable
				// from every caller in this package, which the emitter enforces by not
				// providing a constructor that builds one.
			case kindScalar:
				b.WriteString("  - " + quoteYAML(it.scalar, it.literal) + "\n")
			}
		}
	}
}

func renderFlowMap(pairs []yamlPair) string {
	var b strings.Builder
	b.WriteString("{ ")
	for i, p := range pairs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(p.key + ": " + quoteYAML(p.val.scalar, p.val.literal))
	}
	b.WriteString(" }")
	return b.String()
}

// quoteYAML renders a scalar, quoting when the text would otherwise parse as something
// else.
//
// Conservative in one direction only: it may quote a string that would have been fine
// unquoted, and it must never leave one unquoted that changes meaning. The cost of the
// first is a slightly noisier diff; the cost of the second is a bundle whose frontmatter
// says something the generator did not — `status: no` read as a boolean, a description
// beginning with `- ` read as a sequence, a title containing `: ` splitting into two
// keys. Every one of those is a wrong page rather than an ugly one.
func quoteYAML(s string, literal bool) string {
	if literal {
		return s
	}
	if s == "" {
		return `""`
	}
	// Newlines and tabs are folded to spaces rather than escaped or block-scalared. See
	// the package comment: the subset has no multi-line form, and a folded description is
	// honest where a mangled block scalar is not.
	if strings.ContainsAny(s, "\n\r\t") {
		s = foldWhitespace(s)
	}
	if needsYAMLQuote(s) {
		return `"` + escapeYAMLDouble(s) + `"`
	}
	return s
}

func foldWhitespace(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		}
		return r
	}, s)
	// Collapse the runs the substitution just created, so folding a paragraph break does
	// not leave a double space that a reader would take for deliberate spacing.
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	return strings.TrimSpace(s)
}

// yamlReserved are the plain scalars YAML 1.1 resolves to a bool or a null. Quoted
// because a `title: NO` that came back as `false` would be a silently wrong page, and
// because YAML 1.1's list is wider than most people expect — `y`, `on`, and `off` are all
// in it, and `on` in particular is a real key in every GitHub workflow.
var yamlReserved = map[string]bool{
	"y": true, "Y": true, "yes": true, "Yes": true, "YES": true,
	"n": true, "N": true, "no": true, "No": true, "NO": true,
	"true": true, "True": true, "TRUE": true,
	"false": true, "False": true, "FALSE": true,
	"on": true, "On": true, "ON": true, "off": true, "Off": true, "OFF": true,
	"null": true, "Null": true, "NULL": true, "~": true,
}

func needsYAMLQuote(s string) bool {
	if yamlReserved[s] {
		return true
	}
	// Leading or trailing space would be stripped by a reader, so the value would not
	// round-trip.
	if s != strings.TrimSpace(s) {
		return true
	}
	// An indicator in the first position starts a different construct: a sequence entry,
	// a mapping, an anchor, an alias, a tag, a block scalar, a flow collection, a quote,
	// a comment, a directive, or a document marker.
	switch s[0] {
	case '-', '?', ':', ',', '[', ']', '{', '}', '#', '&', '*', '!', '|', '>', '\'', '"',
		'%', '@', '`':
		return true
	}
	// `key: value` inside a plain scalar splits it. Only `: ` and a trailing colon do —
	// a bare colon in `http://x` does not, which is why this is not a check for ':'.
	if strings.Contains(s, ": ") || strings.HasSuffix(s, ":") {
		return true
	}
	// ` #` starts a trailing comment, dropping everything after it.
	if strings.Contains(s, " #") {
		return true
	}
	// The flow indicators, checked anywhere in the string rather than only in the first
	// position above. This is the one rule here that is not about how a scalar *begins*, and
	// it exists because every scalar this function renders may end up inside a flow
	// collection — `edges:` is a block sequence of flow mappings — where `[`, `]`, `{`, `}`
	// and `,` terminate the current scalar wherever they appear. A path is the case that
	// bites: `app/tools/[slug]/page.tsx` opens a flow sequence that never closes, so the
	// mapping never terminates and every *subsequent* entry in the sequence is unreadable.
	// Reported as issue #9, where four pages of a Next.js repository silently lost seven
	// edges — Next.js dynamic routes put brackets in a directory name by convention, and
	// POSIX permits all five characters in any filename.
	//
	// Quoted unconditionally rather than only when the scalar is bound for a flow context.
	// This function does not know its caller's context, and threading one through would put
	// the decision in three call sites instead of one — where the cost of over-quoting is a
	// pair of quotes in a diff, and the cost of getting the context wrong once is this bug
	// again.
	if strings.ContainsAny(s, "[]{},") {
		return true
	}
	// Text that would resolve as a number must be quoted to stay text. A version string
	// like "1.10" is the case that matters: unquoted it becomes 1.1, losing a digit.
	if looksNumeric(s) {
		return true
	}
	return false
}

// looksNumeric reports whether a plain scalar would be read as a number.
//
// Deliberately loose — it accepts things that are not valid numbers, such as "1.2.3",
// and quoting those is harmless. The direction that must not fail is the other one.
func looksNumeric(s string) bool {
	digits := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			digits = true
		case c == '.' || c == '-' || c == '+' || c == '_':
			// Separator or sign; carries no verdict on its own.
		case c == 'e' || c == 'E':
			// Exponent, only meaningful once digits have been seen.
			if !digits {
				return false
			}
		default:
			return false
		}
	}
	return digits
}

func escapeYAMLDouble(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// sortedStrings returns a deduplicated, sorted copy. Used for every list that reaches
// frontmatter, since a list whose order came from a map would churn the diff.
func sortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}
