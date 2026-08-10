package okf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/3rg0n/signpost/internal/graph"
)

// manifest.json is the machine-readable record of a run: what was described, by what, and
// what the run could not account for.
//
// Separate from index.md because the two have different readers and different failure
// modes. The index is prose for a model choosing what to read; this is structured data for
// a CI check, a staleness gate, or a diff between two commits' bundles. Putting both in one
// file would mean either a machine parsing markdown or a human reading JSON, and both are
// worse than two files.
//
// Every field here answers a question `verify` asks. Nothing is recorded because it is
// interesting — a manifest that grew fields nobody checked would be a second place for
// staleness to hide.

type bundleManifest struct {
	// OKFVersion is the spec version the pages conform to.
	OKFVersion string `json:"okf_version"`
	// Generator identifies what wrote this, including the model when one was used. This is
	// the field that makes a bundle built partly on a local model and partly on a remote
	// one auditable.
	Generator string `json:"generator"`
	// Resource is the commit the bundle describes. verify compares it against HEAD, which
	// is the whole staleness check.
	Resource string `json:"resource,omitempty"`
	// Date is the commit's date, which is also every page's generated.at. Not a wall clock:
	// see vcs.Commit.Date for why a timestamp here would produce commit churn.
	Date string `json:"date,omitempty"`

	Counts manifestCounts `json:"counts"`
	// Confidence counts edges by how they were established. Present so a consumer can see
	// at a glance how much of the map is parsed and how much is inferred, per ADR 0004 —
	// and so a no-model run can be *asserted* to contain nothing but extracted edges, which
	// is what CI checks.
	Confidence map[string]int `json:"confidence"`
	// Pages lists every page written, sorted, so a consumer can check the set without
	// walking the directory.
	Pages []string `json:"pages"`
}

type manifestCounts struct {
	Nodes int `json:"nodes"`
	Edges int `json:"edges"`
	// Clusters is how many communities the graph fell into. A useful one-number summary of
	// whether the repository has structure: one cluster containing everything means it does
	// not.
	Clusters int `json:"clusters"`
}

// manifestJSON renders the run record.
func manifestJSON(g *graph.Graph, opts Options) (string, error) {
	nodes, edges := g.Counts()
	m := bundleManifest{
		OKFVersion: "0.2",
		Generator:  string(opts.Actor),
		Resource:   resourceFor(opts.Resource, ""),
		Date:       opts.Date,
		Counts: manifestCounts{
			Nodes:    nodes,
			Edges:    edges,
			Clusters: len(g.Clusters()),
		},
		Confidence: confidenceCounts(g),
		Pages:      pageList(g, opts),
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Same reasoning as internal/export's JSON writer: this file is read by tools and
	// diffed by humans, and HTML-escaping a `>` in a path makes both worse.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return "", fmt.Errorf("okf: encoding manifest: %w", err)
	}
	return buf.String(), nil
}

// RecordedCommit returns the commit sha the bundle at root records, or "" when there is no
// readable bundle, no manifest, or no resource in it.
//
// Exported for one caller: `verify -as-of-bundle` has to know which commit to read history as
// of *before* it analyses anything, because seven churn attributes and the co-change edges are
// history-derived and land in page content. Everything else about the bundle is read after the
// analysis, which is why this is the only accessor of its kind.
//
// No error is returned, and the absence is not distinguished from a failure, because the
// caller does the same thing either way: read history from HEAD and let the strict comparison
// report whatever is wrong. A bundle so broken that its manifest will not parse has a finding
// waiting for it in Verify, and a second one here would name the same defect twice.
func RecordedCommit(root string) string {
	man, err := readManifest(filepath.Join(root, BundleDir))
	if err != nil {
		return ""
	}
	return commitFromResource(man.Resource)
}

// commitFromResource pulls the sha back out of a `git://repo@sha` or `git://sha` URI.
//
// Parsed rather than recorded as its own manifest field, so there is one place a bundle says
// which commit it describes. A second field would be a second thing to keep in agreement, and
// the failure would be silent: the two could disagree and every check would still pass.
//
// The last `@` wins, because a repository host may carry one and a sha may not.
func commitFromResource(res string) string {
	const scheme = "git://"
	if !strings.HasPrefix(res, scheme) {
		return ""
	}
	rest := res[len(scheme):]
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		rest = rest[i+1:]
	}
	// A path may follow the sha in a page's resource. The manifest's own is the bare base, but
	// nothing about this function should depend on which of the two it was handed.
	if i := strings.Index(rest, "/"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// confidenceCounts tallies edges by confidence.
//
// Every level is present even at zero, which is the §4.2 rule applied to one number: an
// absent `"inferred"` key and `"inferred": 0` look the same to a careless consumer but mean
// "the field was not computed" and "no edge was inferred" respectively, and only the second
// is a fact about the bundle.
func confidenceCounts(g *graph.Graph) map[string]int {
	out := map[string]int{
		string(graph.Extracted): 0,
		string(graph.Inferred):  0,
		string(graph.Ambiguous): 0,
	}
	for _, e := range g.Edges() {
		if !g.Has(e.From) || !g.Has(e.To) {
			continue
		}
		out[string(e.Conf)]++
	}
	return out
}

// pageList is every page this run writes, which is what makes the list checkable.
//
// practices.md is conditional on the same thing renderAll's is. Omitting it was a real
// discrepancy this repository's own bundle carried — 32 listed against 33 on disk — invisible
// until verify started comparing the two, which is the third thing issue #10 asked for. A list
// a consumer is invited to read *instead of* walking the directory has to name what is there.
func pageList(g *graph.Graph, opts Options) []string {
	out := make([]string, 0, len(g.Nodes())+3)
	out = append(out, IndexPage, LogPage)
	if opts.Practices != "" {
		out = append(out, PracticesPage)
	}
	for _, n := range g.Nodes() {
		out = append(out, trimLeadingSlash(pagePath(n.ID)))
	}
	// Sorted explicitly. g.Nodes() is already sorted by ID, but the two reserved pages are
	// not in that order relative to it — "index.md" sorts before "interfaces/" and "log.md"
	// after it — so relying on the node ordering alone would emit a list that is nearly but
	// not quite sorted, which is stable yet wrong for a consumer doing a binary search.
	sort.Strings(out)
	return out
}

func trimLeadingSlash(s string) string {
	if len(s) > 0 && s[0] == '/' {
		return s[1:]
	}
	return s
}
