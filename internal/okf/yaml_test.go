package okf

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/manifest"
)

// yamlRound emits a document and reads it back with the tolerant parser the rest of
// signpost uses.
//
// This is the assertion the package comment promises, and it is here because the failure it
// catches is invisible on the emit path: a page whose frontmatter this emitter wrote and
// this project's own reader cannot read would look perfectly correct in a diff and would
// break `verify` — a different command, run later, by someone who did not make the change.
func yamlRound(t *testing.T, d *yamlDoc) *manifest.Node {
	t.Helper()
	src := d.String()
	n, diag := manifest.ParseYAMLDoc(src)
	if diag.Incomplete() {
		t.Fatalf("emitted YAML the reader could not fully read: %s\n---\n%s", diag.Summary(), src)
	}
	if n == nil {
		t.Fatalf("emitted YAML parsed to nothing:\n---\n%s", src)
	}
	return n
}

func TestYAMLScalarsRoundTrip(t *testing.T) {
	d := &yamlDoc{}
	d.setScalar("type", "Module")
	d.setScalar("title", "internal/auth")
	d.setScalar("description", "JWT validation; the only writer of the token table.")
	n := yamlRound(t, d)

	if got := n.Get("type").String(); got != "Module" {
		t.Errorf("type = %q", got)
	}
	if got := n.Get("title").String(); got != "internal/auth" {
		t.Errorf("title = %q", got)
	}
	if got := n.Get("description").String(); got != "JWT validation; the only writer of the token table." {
		t.Errorf("description = %q", got)
	}
}

// An empty scalar is skipped rather than emitted as a bare key. `title:` with nothing after
// it reads as a fact the generator failed to produce, where an absent key reads as one it
// had nothing to say about — and only the second is true.
func TestYAMLEmptyScalarIsSkipped(t *testing.T) {
	d := &yamlDoc{}
	d.setScalar("type", "Module")
	d.setScalar("description", "")
	if got := d.String(); got != "type: Module\n" {
		t.Errorf("String() = %q, want the description key absent", got)
	}
}

func TestYAMLFlowMapAndSequences(t *testing.T) {
	d := &yamlDoc{}
	d.set("generated", flowMap(
		yamlPair{"by", scalar("signpost/0.1.0")},
		yamlPair{"at", scalar("2026-07-30")},
	))
	d.setStrings("tags", []string{"go", "security-boundary"})
	d.set("edges", seq(
		flowMap(
			yamlPair{"kind", scalar("imports")},
			yamlPair{"to", scalar("/modules/storage.md")},
			yamlPair{"confidence", scalar("extracted")},
			yamlPair{"weight", number(14)},
		),
	))
	n := yamlRound(t, d)

	if got := n.Path("generated", "by").String(); got != "signpost/0.1.0" {
		t.Errorf("generated.by = %q", got)
	}
	if got := strings.Join(n.Get("tags").Strings(), ","); got != "go,security-boundary" {
		t.Errorf("tags = %q", got)
	}
	e := n.Get("edges").At(0)
	if got := e.Get("to").String(); got != "/modules/storage.md" {
		t.Errorf("edge to = %q", got)
	}
	if got := e.Get("confidence").String(); got != "extracted" {
		t.Errorf("edge confidence = %q", got)
	}
	// Unquoted, so a consumer gets the number rather than its text.
	if got, ok := e.Get("weight").Int(); !ok || got != 14 {
		t.Errorf("edge weight = %v, %v; want 14, true", got, ok)
	}
	if e.Get("weight").Quoted {
		t.Error("weight was quoted; a count must stay a number")
	}
}

func TestYAMLSetStringsSkipsEmptyList(t *testing.T) {
	d := &yamlDoc{}
	d.setStrings("tags", nil)
	d.setStrings("also", []string{})
	if got := d.String(); got != "" {
		t.Errorf("String() = %q, want empty", got)
	}
}

// The quoting table. Each row is a value that, left unquoted, would come back as
// something other than what was written — which is a wrong page rather than an ugly one.
func TestYAMLQuotesValuesThatWouldChangeMeaning(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"yaml 1.1 bool no", "no"},
		{"yaml 1.1 bool single letter", "y"},
		{"yaml 1.1 bool on", "on"},
		{"yaml 1.1 bool uppercase", "OFF"},
		{"null spelling", "null"},
		{"tilde", "~"},
		{"leading dash reads as a sequence", "- not a list"},
		{"leading brace reads as a flow map", "{redacted}"},
		{"leading bracket reads as a flow sequence", "[see below]"},
		{"leading hash reads as a comment", "#1 priority"},
		{"leading ampersand reads as an anchor", "&common settings"},
		{"leading asterisk reads as an alias", "*wildcard"},
		{"leading bang reads as a tag", "!important"},
		{"leading pipe reads as a block scalar", "|pipe"},
		{"leading angle reads as a folded scalar", ">folded"},
		{"leading quote reads as a quoted scalar", `"quoted`},
		{"leading apostrophe reads as a quoted scalar", "'tis"},
		{"leading percent reads as a directive", "%50 done"},
		{"colon space splits the key", "ADR 0007: tokens are opaque"},
		{"trailing colon reads as a key", "see below:"},
		{"space hash starts a comment", "auth # the boundary"},
		{"integer would stop being text", "42"},
		{"version loses a digit", "1.10"},
		{"leading space would be stripped", " indented"},
		{"trailing space would be stripped", "trailing "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := &yamlDoc{}
			d.setScalar("title", c.in)
			n := yamlRound(t, d)
			if got := n.Get("title").String(); got != c.in {
				t.Errorf("title round-tripped as %q, want %q\nemitted: %s",
					got, c.in, d.String())
			}
		})
	}
}

// Values that need no quoting stay unquoted, so the common case produces a readable page.
// This is the direction quoteYAML is allowed to get wrong — asserted anyway, because a
// change that quoted everything would pass every other test in this file while making
// every page noisier and every diff larger.
func TestYAMLLeavesOrdinaryValuesUnquoted(t *testing.T) {
	plain := []string{
		"Module",
		"internal/auth",
		"git://example.com/repo@8f2a1c9/internal/auth",
		"https://example.com/x",
		"JWT validation and PAT issuance.",
		"cgr.dev/chainguard/static:latest",
	}
	for _, s := range plain {
		d := &yamlDoc{}
		d.setScalar("title", s)
		if got := d.String(); got != "title: "+s+"\n" {
			t.Errorf("setScalar(%q) emitted %q, want it unquoted", s, got)
		}
	}
}

// A colon with no following space does not split a plain scalar, which is why an image
// reference and a URL survive the row above. Asserted through the reader rather than by
// eye, since it is the reader's behaviour that makes it safe.
func TestYAMLBareColonSurvivesUnquoted(t *testing.T) {
	d := &yamlDoc{}
	d.setScalar("image", "docker.io/postgres:17")
	n := yamlRound(t, d)
	if got := n.Get("image").String(); got != "docker.io/postgres:17" {
		t.Errorf("image = %q", got)
	}
}

func TestYAMLEscapesQuotesAndBackslashes(t *testing.T) {
	d := &yamlDoc{}
	// Leading quote forces quoting; the embedded quote and backslash must then be escaped
	// or the value terminates early and the rest becomes garbage.
	d.setScalar("title", `"weird" C:\path`)
	n := yamlRound(t, d)
	if got := n.Get("title").String(); got != `"weird" C:\path` {
		t.Errorf("title = %q\nemitted: %s", got, d.String())
	}
}

// Multi-line text folds to one line rather than becoming a block scalar. The subset has no
// multi-line form; folding is honest, and a mangled block scalar would not be.
func TestYAMLFoldsMultilineToOneLine(t *testing.T) {
	d := &yamlDoc{}
	d.setScalar("description", "first line\n\nsecond\tline\r\nthird")
	out := d.String()
	if strings.Count(out, "\n") != 1 {
		t.Errorf("emitted %d newlines, want 1:\n%s", strings.Count(out, "\n"), out)
	}
	n := yamlRound(t, d)
	if got := n.Get("description").String(); got != "first line second line third" {
		t.Errorf("description = %q", got)
	}
}

func TestYAMLEmptyStringEmitsQuotedEmpty(t *testing.T) {
	// Reached through set rather than setScalar: setScalar skips an empty value, and the
	// case that matters is a flow-map pair, which has no skip.
	d := &yamlDoc{}
	d.set("generated", flowMap(yamlPair{"by", scalar("")}))
	if got := d.String(); got != "generated: { by: \"\" }\n" {
		t.Errorf("String() = %q", got)
	}
}

// Key order is the caller's. This is the property that makes a page readable and the diff
// small, and it is the one a switch to a map would silently break.
func TestYAMLKeyOrderIsTheCallersNotSorted(t *testing.T) {
	d := &yamlDoc{}
	d.setScalar("type", "Module")
	d.setScalar("title", "auth")
	d.setScalar("description", "d")
	want := "type: Module\ntitle: auth\ndescription: d\n"
	if got := d.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestYAMLNumberIsUnquotedEvenAtZero(t *testing.T) {
	d := &yamlDoc{}
	d.set("counts", seq(flowMap(yamlPair{"weight", number(0)})))
	if got := d.String(); !strings.Contains(got, "weight: 0 }") {
		t.Errorf("String() = %q, want an unquoted zero", got)
	}
}

func TestSortedStringsDeduplicatesAndSorts(t *testing.T) {
	got := sortedStrings([]string{"go", "  security  ", "go", "", "  ", "api"})
	want := []string{"api", "go", "security"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("sortedStrings = %v, want %v", got, want)
	}
	if sortedStrings(nil) != nil {
		t.Error("sortedStrings(nil) should be nil, so setStrings skips the key")
	}
	if sortedStrings([]string{"", "   "}) != nil {
		t.Error("a list of blanks should be nil, not a list of empty strings")
	}
}

// A nested sequence has no representation in the subset and must not be rendered as
// something a reader would misparse. Unreachable from every caller in this package, so the
// test builds one by hand — the assertion is that the emitter drops it rather than
// producing `  - [a]`, which a reader would take for a scalar named "[a]".
func TestYAMLNestedSequenceIsDroppedNotMisrendered(t *testing.T) {
	d := &yamlDoc{}
	d.set("edges", seq(yamlValue{kind: kindFlowSeq, items: []yamlValue{scalar("a")}}))
	if got := d.String(); got != "edges:\n" {
		t.Errorf("String() = %q, want the nested sequence dropped", got)
	}
}

// TestYAMLFlowIndicatorsInAPathSurviveInsideAFlowMapping is issue #9 at the unit level.
//
// The table above emits each value as a top-level scalar, where a bracket in the *middle* of
// the string is genuinely harmless — `title: a[b]c` round-trips. That is why the old
// first-character rule passed every test in this file and still shipped the bug: the character
// only bites inside a flow collection, which is where every `edges[]` entry lives. So this test
// puts the path where the emitter actually puts it.
//
// Two entries, deliberately. A fault in the first must not be masked by the second being
// checked instead, and the second is what proves the *mapping* still terminated — an
// unterminated one swallows every entry after it, which is how four pages lost seven edges.
func TestYAMLFlowIndicatorsInAPathSurviveInsideAFlowMapping(t *testing.T) {
	// Real conventions, not contrived strings. Next.js puts brackets and parentheses in
	// directory names; POSIX permits all five characters in any filename.
	paths := []struct{ name, path string }{
		{"next.js dynamic route", "ts/app/tools/[slug]/page.tsx"},
		{"next.js catch-all route", "ts/app/blog/[...rest]/page.tsx"},
		{"comma in a basename", "py/greeter/data,notes.py"},
		{"brace in a basename", "sh/{template}.sh"},
		{"every indicator at once", "x/[a]{b},c.ts"},
	}
	for _, c := range paths {
		t.Run(c.name, func(t *testing.T) {
			d := &yamlDoc{}
			d.set("edges", seq(
				flowMap(
					yamlPair{"kind", scalar("imports")},
					yamlPair{"to", scalar("/modules/storage.md")},
					yamlPair{"source", scalar(c.path)},
				),
				flowMap(
					yamlPair{"kind", scalar("tested_by")},
					yamlPair{"to", scalar("/modules/tests.md")},
				),
			))
			src := d.String()

			n, diag := manifest.ParseYAMLDoc(src)
			if diag.Malformed {
				t.Fatalf("a path containing a YAML flow indicator made the document unparseable "+
					"(issue #9): %s\n---\n%s", diag.Summary(), src)
			}
			if diag.Incomplete() {
				t.Fatalf("the reader could not fully read the emitted YAML: %s\n---\n%s",
					diag.Summary(), src)
			}

			first := n.Get("edges").At(0)
			if got := first.Get("source").String(); got != c.path {
				t.Errorf("source round-tripped as %q, want %q — the scalar terminated early\n"+
					"---\n%s", got, c.path, src)
			}
			// The key set, which is what catches a comma. An unquoted `[` makes the document
			// unreadable and the check above fires; an unquoted `,` parses clean and silently
			// splits the scalar, leaving `source: py/greeter/data` beside an invented
			// `notes.py:` key. Nothing about parseability catches that.
			for _, k := range first.Keys {
				switch k {
				case "kind", "to", "source":
				default:
					t.Errorf("the edge read back with the key %q, which was not emitted — the "+
						"source scalar split and its remainder became a key\n---\n%s", k, src)
				}
			}
			// The second entry, which an unterminated mapping would have consumed.
			if got := n.Get("edges").At(1).Get("kind").String(); got != "tested_by" {
				t.Errorf("the following edge read back with kind %q, want tested_by — the first "+
					"mapping never terminated and swallowed it\n---\n%s", got, src)
			}
		})
	}
}

// TestNeedsYAMLQuoteOnFlowIndicatorsAnywhere pins the rule directly, because the round-trip
// above passes for the wrong reason if the emitter starts quoting everything.
//
// Position-independence is the whole correction: the old rule tested `s[0]`, which is right for
// the indicators that only matter at the start of a scalar (`&`, `*`, `!`, `|`, `>`, `%`) and
// wrong for the five that terminate a scalar wherever they appear.
func TestNeedsYAMLQuoteOnFlowIndicatorsAnywhere(t *testing.T) {
	for _, s := range []string{
		"a[b", "a]b", "a{b", "a}b", "a,b",
		"app/tools/[slug]/page.tsx",
		"data,notes.py",
		// Trailing, which is neither first nor interior.
		"trailing[", "trailing,",
	} {
		if !needsYAMLQuote(s) {
			t.Errorf("needsYAMLQuote(%q) = false; a flow indicator terminates the scalar "+
				"wherever it appears, and every value this emitter writes may land inside a "+
				"flow mapping (issue #9)", s)
		}
	}
}

func TestLooksNumeric(t *testing.T) {
	numeric := []string{"1", "42", "1.10", "1.2.3", "-1", "+1", "1e9", "1_000", "0.0"}
	for _, s := range numeric {
		if !looksNumeric(s) {
			t.Errorf("looksNumeric(%q) = false, want true", s)
		}
	}
	// No digits at all, so nothing to lose by leaving it unquoted.
	notNumeric := []string{"", "-", ".", "e9", "abc", "1a", "v1.0"}
	for _, s := range notNumeric {
		if looksNumeric(s) {
			t.Errorf("looksNumeric(%q) = true, want false", s)
		}
	}
}
