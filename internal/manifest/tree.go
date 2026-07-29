// Package manifest reads the deterministic, non-source signal in a repository:
// dependency manifests, container and compose definitions, CI workflows, Helm
// charts and Kubernetes manifests, interface contracts, migrations, and the files
// that state ownership and rules.
//
// Design §4.1 calls this "the highest-value deterministic signal and the part
// comparable tools mostly skip", and the reason is that it answers questions
// source code cannot. Imports tell you what a module uses; a compose file tells
// you what actually runs, on which port, from which image. An agent that knows the
// second is oriented in a way one that only knows the first is not.
//
// Everything here is exact. There is no approximation budget as there is for the
// hand-written source extractors (design §4.2): a manifest is a machine-readable
// file with a defined grammar, so a fact read out of one is either present or
// absent, never guessed. Where a file uses a construct these readers do not
// support, the facts are marked Incomplete and say why, rather than being reported
// as a complete reading.
//
// # Secrets
//
// One rule runs through every extractor in this package: a secret is recorded as a
// *reference*, never as a value. Knowing that a service reads `DATABASE_PASSWORD`
// from a Kubernetes Secret named `db-creds` is architectural signal an agent needs.
// The bytes in that Secret's `data:` block are a credential, and this bundle gets
// committed to the repository and published as a page. So the readers below
// deliberately walk past value fields they are perfectly capable of parsing.
package manifest

import (
	"sort"
	"strconv"
	"strings"
)

// NodeKind is the shape of a parsed value.
type NodeKind uint8

const (
	// KindScalar is a leaf: a string, number, bool, or null.
	KindScalar NodeKind = iota
	// KindMap is a key-value mapping, with key order preserved.
	KindMap
	// KindSeq is an ordered sequence.
	KindSeq
)

// Node is one value in a parsed configuration file.
//
// YAML, TOML, and JSON all parse into this single tree so that every extractor
// below has one lookup API regardless of which format a project chose. That
// matters more than it sounds: `openapi.yaml` and `openapi.json` are the same
// document, a Helm chart is YAML while a Cargo manifest is TOML, and an extractor
// written against a per-format type would have to be written twice.
//
// A map keeps parallel Keys and Vals slices rather than a Go map. Lookup is linear,
// which is irrelevant at the size of a real manifest, and in exchange iteration
// order is the file's own order rather than Go's randomised one — the determinism
// the committed bundle depends on (design §8.1).
type Node struct {
	Kind NodeKind
	// Str is the scalar text, exactly as written after quote and escape
	// processing. Numbers and bools are kept as text: a manifest's `"3.10"` and
	// `3.10` mean different things to the tool reading it, and converting both to
	// a float would erase the difference.
	Str string
	// Quoted records that a scalar was written in quotes. Distinguishes YAML's
	// `on: push` (a mapping key that is also a reserved word) from `on: "push"`,
	// and a version pin `"1.0"` from the number 1.0.
	Quoted bool
	// Keys and Vals hold a mapping's entries in file order. Always the same length.
	Keys []string
	Vals []*Node
	// Items holds a sequence's entries in file order.
	Items []*Node
	// Line is the 1-based line the value started on, for provenance.
	Line int
}

// scalarNode builds a leaf.
func scalarNode(s string, quoted bool, line int) *Node {
	return &Node{Kind: KindScalar, Str: s, Quoted: quoted, Line: line}
}

// mapNode builds an empty mapping.
func mapNode(line int) *Node { return &Node{Kind: KindMap, Line: line} }

// seqNode builds an empty sequence.
func seqNode(line int) *Node { return &Node{Kind: KindSeq, Line: line} }

// set adds or replaces a mapping entry.
//
// A duplicate key replaces rather than appends. Duplicate keys are invalid in all
// three formats, but they do occur in hand-edited files, and every real parser
// resolves them last-wins — so an extractor that saw both would disagree with the
// tool the file is actually for.
func (n *Node) set(key string, val *Node) {
	if n == nil || n.Kind != KindMap {
		return
	}
	for i, k := range n.Keys {
		if k == key {
			n.Vals[i] = val
			return
		}
	}
	n.Keys = append(n.Keys, key)
	n.Vals = append(n.Vals, val)
}

// Get returns the value for a mapping key, or nil.
//
// Nil-safe in both directions: Get on a nil node returns nil, and a missing key
// returns nil, so `root.Get("a").Get("b").String()` is a legal lookup that yields
// "" when any link is absent. Every extractor here reads optional keys out of
// documents written by other people, and a lookup chain that panics on a missing
// key would turn one unusual manifest into a failed build.
func (n *Node) Get(key string) *Node {
	if n == nil || n.Kind != KindMap {
		return nil
	}
	for i, k := range n.Keys {
		if k == key {
			return n.Vals[i]
		}
	}
	return nil
}

// GetAny returns the value of the first key present, for the fields that have
// more than one spelling — compose's `container_name`, a chart's `fullnameOverride`.
func (n *Node) GetAny(keys ...string) *Node {
	for _, k := range keys {
		if v := n.Get(k); v != nil {
			return v
		}
	}
	return nil
}

// Path walks a chain of mapping keys.
func (n *Node) Path(keys ...string) *Node {
	cur := n
	for _, k := range keys {
		cur = cur.Get(k)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// At returns a sequence element, or nil when out of range.
func (n *Node) At(i int) *Node {
	if n == nil || n.Kind != KindSeq || i < 0 || i >= len(n.Items) {
		return nil
	}
	return n.Items[i]
}

// Len reports the number of entries: mapping keys, sequence items, or 0 for a
// scalar.
func (n *Node) Len() int {
	if n == nil {
		return 0
	}
	switch n.Kind {
	case KindMap:
		return len(n.Keys)
	case KindSeq:
		return len(n.Items)
	}
	return 0
}

// IsZero reports whether a node is absent or holds nothing.
func (n *Node) IsZero() bool {
	if n == nil {
		return true
	}
	if n.Kind == KindScalar {
		return n.Str == ""
	}
	return n.Len() == 0
}

// String returns a scalar's text, or "" for a map or sequence.
func (n *Node) String() string {
	if n == nil || n.Kind != KindScalar {
		return ""
	}
	return n.Str
}

// Bool interprets a scalar as a boolean, reporting whether it was one.
//
// The accepted spellings are YAML 1.1's, because that is what CI and Kubernetes
// tooling still accepts in practice: `yes`, `on`, and `true` all mean true in a
// GitHub Actions workflow, and a reader that only understood `true` would report a
// disabled job as enabled.
func (n *Node) Bool() (bool, bool) {
	switch strings.ToLower(n.String()) {
	case "true", "yes", "on", "y":
		return true, true
	case "false", "no", "off", "n":
		return false, true
	}
	return false, false
}

// Int interprets a scalar as an integer, reporting whether it was one.
func (n *Node) Int() (int, bool) {
	s := strings.TrimSpace(n.String())
	if s == "" {
		return 0, false
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return v, true
}

// Seq returns a node's elements as a sequence.
//
// A lone scalar counts as a one-element sequence. This is not laxity, it is the
// formats' own convention: compose accepts `command: ./run` and
// `command: ["./run"]`, a workflow accepts `on: push` and `on: [push,
// pull_request]`, and both spellings appear in real repositories. An extractor that
// only handled the list form would silently drop every single-valued case.
func (n *Node) Seq() []*Node {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case KindSeq:
		return n.Items
	case KindScalar:
		if n.Str == "" {
			return nil
		}
		return []*Node{n}
	}
	return nil
}

// Strings returns a node's elements as scalar text, skipping non-scalars.
func (n *Node) Strings() []string {
	items := n.Seq()
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s := it.String(); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// MapKeys returns a mapping's keys in file order.
func (n *Node) MapKeys() []string {
	if n == nil || n.Kind != KindMap {
		return nil
	}
	out := make([]string, len(n.Keys))
	copy(out, n.Keys)
	return out
}

// SortedMapKeys returns a mapping's keys in sorted order, for output that must be
// stable regardless of how the file was written.
func (n *Node) SortedMapKeys() []string {
	out := n.MapKeys()
	sort.Strings(out)
	return out
}

// Each calls fn for every mapping entry in file order, stopping if fn returns
// false.
func (n *Node) Each(fn func(key string, val *Node) bool) {
	if n == nil || n.Kind != KindMap {
		return
	}
	for i, k := range n.Keys {
		if !fn(k, n.Vals[i]) {
			return
		}
	}
}

// KeyValues flattens a node that may be written either as a mapping or as a
// sequence of `KEY=VALUE` strings.
//
// Both spellings mean the same thing and both are idiomatic, in compose
// (`environment:`), in Kubernetes (`env:` is a list of name/value objects), and in
// Dockerfiles. Normalising here rather than in each caller keeps "which spelling
// did this author use" out of the extractors entirely.
//
// The returned slice preserves file order and may contain duplicate keys, because
// which one wins is the consuming tool's business, not this reader's.
func (n *Node) KeyValues() []KeyValue {
	if n == nil {
		return nil
	}
	var out []KeyValue
	switch n.Kind {
	case KindMap:
		n.Each(func(k string, v *Node) bool {
			out = append(out, KeyValue{Key: k, Value: v.String(), Node: v, Line: lineOf(v, n.Line)})
			return true
		})
	case KindSeq:
		for _, it := range n.Items {
			// The Kubernetes form: a list of {name: X, value: Y} objects.
			if it.Kind == KindMap {
				if name := it.Get("name").String(); name != "" {
					out = append(out, KeyValue{
						Key: name, Value: it.Get("value").String(), Node: it, Line: it.Line,
					})
					continue
				}
				continue
			}
			k, v := splitKeyValue(it.String())
			if k == "" {
				continue
			}
			out = append(out, KeyValue{Key: k, Value: v, Node: it, Line: it.Line})
		}
	}
	return out
}

// KeyValue is one entry from KeyValues.
type KeyValue struct {
	Key   string
	Value string
	// Node is the value's node, so a caller can look inside a structured value
	// such as Kubernetes' valueFrom.
	Node *Node
	Line int
}

// splitKeyValue splits "KEY=VALUE", with a bare "KEY" yielding an empty value —
// which in an environment list means "inherit from the host", a distinction worth
// keeping.
func splitKeyValue(s string) (string, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", ""
	}
	if i := strings.IndexByte(s, '='); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

// lineOf returns a node's line, falling back to a parent's when the node is nil.
func lineOf(n *Node, fallback int) int {
	if n == nil || n.Line == 0 {
		return fallback
	}
	return n.Line
}
