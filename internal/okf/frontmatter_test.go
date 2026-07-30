package okf

import (
	"strings"
	"testing"
)

func TestMergeFrontmatterReplacesGeneratedAndCarriesHuman(t *testing.T) {
	prev := "type: Module\n" +
		"title: old title\n" +
		"tags: [go]\n" +
		"verified:\n" +
		"  - { by: human:ecopelan, at: 2026-07-29, resource: git://x@aaa }\n" +
		"stale_after: 2026-10-27\n"
	next := "type: Module\ntitle: new title\ntags: [go, security-boundary]\n"

	got := mergeFrontmatter(prev, next)

	if !strings.Contains(got, "title: new title") {
		t.Error("generated title not refreshed")
	}
	if strings.Contains(got, "old title") {
		t.Error("stale generated title survived")
	}
	if !strings.Contains(got, "human:ecopelan") {
		t.Fatal("the verified block was dropped")
	}
	if !strings.Contains(got, "stale_after: 2026-10-27") {
		t.Error("an unrecognised human key was dropped")
	}
	// Generated keys first, in next's order; human keys below, in prev's.
	if strings.Index(got, "type: Module") > strings.Index(got, "verified:") {
		t.Error("human keys were emitted above generated ones")
	}
}

// An unknown key is carried across as raw source lines, never re-emitted from a parse tree.
// Comments, quoting style, and key order inside the block are the human's, and normalising
// them is a small act of overwriting them.
func TestCarryHumanKeysPreservesRawText(t *testing.T) {
	prev := "type: Module\n" +
		"verified:\n" +
		"  # checked against the 2026-07 audit\n" +
		"  - by:   'human:ecopelan'\n" +
		"    at:   2026-07-29\n" +
		"custom_key: \"quoted   oddly\"\n"
	got := carryHumanKeys(prev)
	want := "verified:\n" +
		"  # checked against the 2026-07 audit\n" +
		"  - by:   'human:ecopelan'\n" +
		"    at:   2026-07-29\n" +
		"custom_key: \"quoted   oddly\"\n"
	if got != want {
		t.Errorf("carryHumanKeys:\n got %q\nwant %q", got, want)
	}
}

// Every generated key must be dropped from the carried text, including the block-valued
// ones. A carried `edges:` block would leave the page asserting relationships the current
// graph does not have, which is the one thing worse than a missing edge.
func TestCarryHumanKeysDropsEveryGeneratedKeyIncludingBlocks(t *testing.T) {
	prev := "type: Module\n" +
		"title: auth\n" +
		"description: d\n" +
		"resource: git://x@aaa\n" +
		"tags: [go]\n" +
		"status: stable\n" +
		"okf_version: \"0.2\"\n" +
		"generated: { by: signpost, at: 2026-07-30 }\n" +
		"attributes:\n" +
		"  - { name: port, value: \"8080\" }\n" +
		"edges:\n" +
		"  - { kind: imports, to: /modules/storage.md, confidence: extracted }\n" +
		"sources:\n" +
		"  - id: adr-0007\n" +
		"    title: tokens are opaque\n" +
		"mine: kept\n"
	got := carryHumanKeys(prev)
	if got != "mine: kept\n" {
		t.Errorf("carryHumanKeys = %q, want only the human key", got)
	}
}

// A generated key's continuation lines go with it. A block sequence under `edges:` is
// indented, so the drop must span the whole block rather than one line.
func TestCarryHumanKeysDropsIndentedContinuations(t *testing.T) {
	prev := "edges:\n" +
		"  - { kind: imports, to: /modules/a.md }\n" +
		"  - { kind: calls, to: /modules/b.md }\n" +
		"verified:\n" +
		"  - { by: human:x, at: 2026-01-01 }\n"
	got := carryHumanKeys(prev)
	if strings.Contains(got, "/modules/") {
		t.Errorf("an edge line survived: %q", got)
	}
	if !strings.Contains(got, "human:x") {
		t.Errorf("the verified block was lost: %q", got)
	}
}

func TestCarryHumanKeysOnEmptyFrontmatter(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\n"} {
		if got := carryHumanKeys(in); got != "" {
			t.Errorf("carryHumanKeys(%q) = %q, want empty", in, got)
		}
	}
}

// mergeFrontmatter with nothing to carry returns next unchanged — no trailing blank line,
// because a page whose frontmatter grew a blank line on every run would churn the diff.
func TestMergeFrontmatterWithNothingToCarryIsExact(t *testing.T) {
	next := "type: Module\ntitle: auth\n"
	if got := mergeFrontmatter("type: Module\ntitle: old\n", next); got != next {
		t.Errorf("mergeFrontmatter = %q, want %q", got, next)
	}
}

func TestMergeFrontmatterAddsMissingNewlineBeforeCarriedKeys(t *testing.T) {
	got := mergeFrontmatter("mine: kept\n", "type: Module")
	if got != "type: Module\nmine: kept\n" {
		t.Errorf("mergeFrontmatter = %q", got)
	}
}

// A comment sitting between two generated keys is dropped along with them. It is a comment
// on generated content, and carrying it would attach a human's note about `edges:` to a
// page that no longer has that block — worse than losing it, because it would then be read
// as a note about whatever followed.
func TestCarryHumanKeysDropsCommentsAttachedToGeneratedKeys(t *testing.T) {
	prev := "type: Module\n" +
		"# this comment is about the tags below\n" +
		"tags: [go]\n" +
		"mine: kept\n"
	got := carryHumanKeys(prev)
	if strings.Contains(got, "this comment") {
		t.Errorf("a comment on a generated key was carried: %q", got)
	}
	if got != "mine: kept\n" {
		t.Errorf("carryHumanKeys = %q", got)
	}
}

func TestTopLevelKey(t *testing.T) {
	yes := map[string]string{
		"type: Module":       "type",
		"verified:":          "verified",
		"okf_version: \"1\"": "okf_version",
		"a:b":                "a",
	}
	for line, want := range yes {
		got, ok := topLevelKey(line)
		if !ok || got != want {
			t.Errorf("topLevelKey(%q) = %q, %v; want %q, true", line, got, ok, want)
		}
	}
	no := []string{
		"",
		"  indented: x",
		"\tindented: x",
		"# comment",
		"- sequence entry",
		"no colon at all",
		": leading colon",
		"two words: x",
	}
	for _, line := range no {
		if _, ok := topLevelKey(line); ok {
			t.Errorf("topLevelKey(%q) = true, want false", line)
		}
	}
}

func TestReadVerified(t *testing.T) {
	fm := "type: Module\n" +
		"verified:\n" +
		"  - { by: human:ecopelan, at: 2026-07-29, resource: git://x@aaa }\n" +
		"  - { by: signpost/0.1.0, at: 2026-07-30 }\n"
	vs := readVerified(fm)
	if len(vs) != 2 {
		t.Fatalf("readVerified returned %d entries: %#v", len(vs), vs)
	}
	if vs[0].By != "human:ecopelan" || vs[0].At != "2026-07-29" || vs[0].Resource != "git://x@aaa" {
		t.Errorf("entry 0 = %#v", vs[0])
	}
	if vs[1].By != "signpost/0.1.0" || vs[1].Resource != "" {
		t.Errorf("entry 1 = %#v", vs[1])
	}
}

// A lone mapping rather than a sequence is accepted, because the tolerant reader treats a
// scalar or mapping as a one-element sequence and a human writing one entry by hand is
// likely to write it that way.
func TestReadVerifiedAcceptsASingleMapping(t *testing.T) {
	vs := readVerified("verified:\n  by: human:x\n  at: 2026-01-01\n")
	if len(vs) != 1 || vs[0].By != "human:x" {
		t.Errorf("readVerified = %#v", vs)
	}
}

// Malformed input returns nothing rather than erroring. A human hand-edited this block, and
// failing the build over their typo is worse than reporting the page as unverified — which
// is visible and fixable.
func TestReadVerifiedOnMalformedInputReturnsNothing(t *testing.T) {
	cases := []string{
		"",
		"verified:\n",
		"verified: not-a-list\n",
		"verified:\n  - { at: 2026-01-01 }\n", // no `by`, so nothing identifies the reviewer
		"type: Module\n",
		"{{ .Values.broken }}\n",
	}
	for _, fm := range cases {
		if vs := readVerified(fm); len(vs) != 0 {
			t.Errorf("readVerified(%q) = %#v, want nothing", fm, vs)
		}
	}
}

func TestReadResource(t *testing.T) {
	if got := readResource("resource: git://x@aaa/internal/auth\n"); got != "git://x@aaa/internal/auth" {
		t.Errorf("readResource = %q", got)
	}
	if got := readResource("type: Module\n"); got != "" {
		t.Errorf("readResource with no key = %q, want empty", got)
	}
}

// The trust tiers: `human:` prefixed actors are human review, anything else is a machine
// confirmation. A machine confirmation is never downgraded, because it is regenerated
// alongside the page it describes.
func TestHumanVerified(t *testing.T) {
	if humanVerified([]Verification{{By: "signpost/0.1.0"}}) {
		t.Error("a machine actor counted as human review")
	}
	if !humanVerified([]Verification{{By: "signpost/0.1.0"}, {By: "human:x"}}) {
		t.Error("a human entry alongside a machine one was missed")
	}
	if humanVerified(nil) {
		t.Error("no verification counted as human review")
	}
}

// The downgrade rule, which is where the project's central claim gets enforced: a
// verification stands only when the reviewer recorded which resource they reviewed *and* it
// is the one being described now.
func TestDowngrade(t *testing.T) {
	const res = "git://example.com/repo@8f2a1c9/internal/auth"
	cases := []struct {
		name     string
		vs       []Verification
		resource string
		want     bool
	}{
		{
			name: "no verification, nothing to downgrade",
			vs:   nil, resource: res, want: false,
		},
		{
			name:     "machine confirmation is not downgraded",
			vs:       []Verification{{By: "signpost/0.1.0", Resource: "git://old"}},
			resource: res, want: false,
		},
		{
			name:     "human review of this exact resource stands",
			vs:       []Verification{{By: "human:ecopelan", At: "2026-07-29", Resource: res}},
			resource: res, want: false,
		},
		{
			name:     "human review of a different commit is downgraded",
			vs:       []Verification{{By: "human:ecopelan", Resource: "git://example.com/repo@0000000/internal/auth"}},
			resource: res, want: true,
		},
		{
			// The judgement call, stated in the code: nothing in the file says which commit
			// was reviewed, so the claim is unsupported. Downgrading is recoverable in one
			// step; wrongly retaining it is not recoverable at all, because nobody looks.
			name:     "human review with no recorded resource is downgraded",
			vs:       []Verification{{By: "human:ecopelan", At: "2026-07-29"}},
			resource: res, want: true,
		},
		{
			name: "one matching human entry is enough even alongside a stale one",
			vs: []Verification{
				{By: "human:a", Resource: "git://old"},
				{By: "human:b", Resource: res},
			},
			resource: res, want: false,
		},
		{
			// No resource on the page means there is nothing to match against, so a
			// verification cannot be shown to still hold.
			name:     "an unknown page resource downgrades",
			vs:       []Verification{{By: "human:ecopelan", Resource: res}},
			resource: "", want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := downgrade(c.vs, c.resource); got != c.want {
				t.Errorf("downgrade = %v, want %v", got, c.want)
			}
		})
	}
}

// generatedKeys must cover every key the emitter writes. Without this, adding a frontmatter
// key to emit.go would silently make it a "human" key: the new key would be written by the
// generator and then carried across from the old page on the next run, so the page would
// keep the first value it ever had forever.
func TestGeneratedKeysCoverEverythingTheEmitterWrites(t *testing.T) {
	g, n := demoGraph(t)
	pages := []*Page{
		pageFor(g, n, demoOptions()),
		indexPage(g, demoOptions()),
		logPage(g, demoOptions()),
	}
	for _, p := range pages {
		for off := 0; off < len(p.Frontmatter); {
			line, next := nextLine(p.Frontmatter, off)
			off = next
			key, ok := topLevelKey(line)
			if !ok {
				continue
			}
			if !generatedKeys[key] {
				t.Errorf("the emitter writes %q but generatedKeys does not list it; "+
					"it would be carried across from the previous page and never refresh", key)
			}
		}
	}
}
