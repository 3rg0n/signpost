package okf

import (
	"strings"

	"github.com/3rg0n/signpost/internal/manifest"
)

// Frontmatter merging, and the `verified:` downgrade design §6.1 requires.
//
// The two halves of frontmatter behave differently and the distinction is the whole
// design:
//
//   - **Generated keys** — type, title, description, resource, tags, status, generated,
//     edges, sources — are signpost's. They are replaced wholesale, because they are
//     derived from the tree and a stale one is a wrong claim about the code.
//   - **Human keys** — `verified:` above all, and anything signpost does not recognise —
//     are the reader's. They are carried across, because a review someone performed is a
//     fact signpost has no standing to discard.
//
// `verified:` is the interesting one, and it is not simply preserved. A human verified a
// page describing a *specific commit*. When the commit changes, the claim "a human checked
// this" is no longer supported by anything: the text they approved may have been replaced.
// So the block is **downgraded**: kept verbatim, with the resource it was made against, and
// the page gains a generated `status: stale-verification` key saying the claim no longer
// holds. Neither of the alternatives works — dropping the block loses the audit trail and the
// reviewer's name, and silently retaining it is the failure mode this whole project exists to
// avoid, a guess wearing a fact's clothing. Because `status` is generated rather than carried,
// a re-review recording the current resource clears the mark on the next run, which is what
// makes the downgrade a recoverable default rather than a scar.
//
// Reading uses internal/manifest's tolerant parser per ADR 0001. Writing uses the emitter
// in yaml.go. The asymmetry is deliberate and is why the unrecognised-key path works at
// all: an unknown key is copied as *raw source lines*, never round-tripped through a parse
// and re-emit, so a human's quoting and comments survive exactly.

// generatedKeys are the frontmatter keys signpost owns. Anything not in this set is
// carried across from the existing page untouched.
//
// A set rather than a switch, because the same list is needed in two directions: to know
// what to overwrite, and to know what to preserve. Two switches would drift.
var generatedKeys = map[string]bool{
	"type":        true,
	"title":       true,
	"description": true,
	"resource":    true,
	"tags":        true,
	"status":      true,
	"generated":   true,
	"edges":       true,
	"sources":     true,
	"okf_version": true,
	"attributes":  true,
}

// mergeFrontmatter returns next's generated keys plus prev's human keys.
//
// Order is next's for the keys it owns, then prev's for the rest, so a page's generated
// header stays in the §3.1 order and a human's additions accumulate below it in the order
// they were written. That is stable across runs: both inputs are ordered, and neither is
// a map.
func mergeFrontmatter(prev, next string) string {
	carried := carryHumanKeys(prev)
	if carried == "" {
		return next
	}
	if !strings.HasSuffix(next, "\n") && next != "" {
		next += "\n"
	}
	return next + carried
}

// carryHumanKeys extracts the source lines of every top-level key signpost does not
// generate.
//
// Line-based rather than parse-and-re-emit, and that is the point: a human's `verified:`
// block may carry comments, a particular quoting style, or a key ordering they chose, and
// re-emitting it from a parse tree would normalise all three. Normalising someone's text
// is a small act of overwriting it. So the original bytes are moved across.
func carryHumanKeys(prev string) string {
	if strings.TrimSpace(prev) == "" {
		return ""
	}
	var b strings.Builder
	keep := false
	for off := 0; off < len(prev); {
		line, next := nextLine(prev, off)
		off = next

		if key, isTop := topLevelKey(line); isTop {
			keep = !generatedKeys[key]
		} else if strings.TrimSpace(line) == "" {
			// A blank line belongs to whatever block it sits inside. Between blocks it is
			// dropped, so removing a generated key does not leave a gap behind.
			if !keep {
				continue
			}
		}
		if keep {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// topLevelKey reports whether a line starts a top-level mapping key, and its name.
//
// Indentation is the test: a continuation line of a block, or a sequence entry, is
// indented, and a top-level key is not. A line inside a multi-line quoted scalar could in
// principle be unindented and contain a colon, which would be misread here — that is
// accepted, because the emitter never writes one and a human who hand-writes an unindented
// multi-line scalar in frontmatter has produced something no reader agrees on anyway.
func topLevelKey(line string) (string, bool) {
	s := strings.TrimRight(line, "\r")
	if s == "" || s[0] == ' ' || s[0] == '\t' || s[0] == '#' || s[0] == '-' {
		return "", false
	}
	key, _, ok := strings.Cut(s, ":")
	if !ok || key == "" || strings.ContainsAny(key, " \t") {
		return "", false
	}
	return key, true
}

// Verification is a `verified:` entry a human added.
type Verification struct {
	// By is the actor string, e.g. "human:ecopelan".
	By string
	// At is the review date, YYYY-MM-DD.
	At string
	// Resource is the `resource:` value the page carried when the review was made, if the
	// page recorded one. This is what makes a downgrade possible: without it, "verified"
	// and "verified against something else" are indistinguishable.
	Resource string
}

// readVerified parses the `verified:` block out of frontmatter.
//
// Returns nothing on a malformed block rather than an error. A human hand-edited this,
// and the consequence of failing to read it is that the page reports as unverified —
// visible, and correctable by fixing the block. The consequence of erroring is a failed
// build over someone's typo in a comment field.
func readVerified(frontmatter string) []Verification {
	root, _ := manifest.ParseYAMLDoc(frontmatter)
	block := root.Get("verified")
	if block == nil {
		return nil
	}
	// A lone mapping rather than a sequence of them. Accepted because a human hand-writing
	// their first verification is likely to write it that way, and the cost of not accepting
	// it is not a missing entry — it is that downgrade never fires, so a review of an older
	// commit is retained with nothing saying it no longer holds. That is the exact failure
	// §6.1 exists to prevent, arriving through the reader.
	items := block.Seq()
	if items == nil && block.Kind == manifest.KindMap {
		items = []*manifest.Node{block}
	}

	var out []Verification
	for _, item := range items {
		v := Verification{
			By:       item.Get("by").String(),
			At:       item.Get("at").String(),
			Resource: item.Get("resource").String(),
		}
		if v.By == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

// statusStaleVerification marks a page whose human `verified:` block no longer matches the
// resource the page describes.
//
// A value rather than a bare `true`, because a later status has to be able to mean
// something else without changing what this one meant.
const statusStaleVerification = "stale-verification"

// keysBeforeStatus are the generated keys that precede `status:` in §3.1's order. Used to
// find where a status line belongs in an already-emitted header.
var keysBeforeStatus = map[string]bool{
	"okf_version": true,
	"type":        true,
	"title":       true,
	"description": true,
	"resource":    true,
	"tags":        true,
}

// withStatus inserts a `status:` line into generated frontmatter, in §3.1's order.
//
// Written into the page rather than only reported on stdout, and that is the whole point:
// the bundle is read by people and agents who never ran signpost, and a downgrade that
// exists only in a terminal someone has closed is a downgrade nobody acts on. `status` is
// a generated key, so the next run replaces it wholesale — a page whose verification comes
// to match again loses the mark without anyone editing it.
//
// Inserted into the *generated* half, before the human keys are carried across, because a
// human's block must stay at the bottom in the order they wrote it.
//
// Idempotent: an existing `status:` and the lines belonging to it are dropped, so the result
// carries exactly one. No caller can currently pass frontmatter that has one — the emitter
// never writes the key, and the only call site passes a freshly generated page — but the
// function should not depend on that. A second status value is a change this design invites,
// and the cost of the precondition going unmet is two `status:` lines in a committed file,
// where the second is the one a YAML reader keeps.
func withStatus(frontmatter, status string) string {
	var before, after strings.Builder
	past, dropping := false, false
	for off := 0; off < len(frontmatter); {
		line, next := nextLine(frontmatter, off)
		off = next

		if key, isTop := topLevelKey(line); isTop {
			dropping = key == "status"
			if !keysBeforeStatus[key] {
				past = true
			}
		}
		if dropping {
			// The key's own line, plus any indented continuation of it.
			continue
		}
		b := &before
		if past {
			b = &after
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return before.String() + "status: " + quoteYAML(status, false) + "\n" + after.String()
}

// readResource returns the page's `resource:` value.
func readResource(frontmatter string) string {
	root, _ := manifest.ParseYAMLDoc(frontmatter)
	return root.Get("resource").String()
}

// humanVerified reports whether any verification was made by a human, per OKF's trust
// tiers: an actor prefixed `human:` is a human review, anything else is a machine
// confirmation.
func humanVerified(vs []Verification) bool {
	for _, v := range vs {
		if strings.HasPrefix(v.By, "human:") {
			return true
		}
	}
	return false
}

// downgrade reports whether a page's human verification still stands, given the resource
// the page now describes.
//
// Stands only when the reviewer recorded which resource they reviewed *and* it is the one
// being described now. A verification with no recorded resource is downgraded: it may
// well have been made against this exact commit, but nothing in the file says so, and
// treating an unsupported claim as supported is the specific error §6.1 is guarding
// against. The downgrade is recoverable in one step — a reviewer re-checks and the new
// entry carries the resource — where a wrongly-retained verification is not recoverable
// at all, because nobody knows to look.
func downgrade(vs []Verification, resource string) bool {
	if !humanVerified(vs) {
		return false
	}
	for _, v := range vs {
		if !strings.HasPrefix(v.By, "human:") {
			continue
		}
		if v.Resource != "" && v.Resource == resource {
			return false
		}
	}
	return true
}
