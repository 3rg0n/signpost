package okf

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/3rg0n/signpost/internal/graph"
)

// Bundle writing: the whole of what `signpost build` produces at .signpost/.
//
// Three properties, each a test in this package:
//
//   - **Byte-stable.** Same graph and same commit in, identical bytes out. ADR 0005
//     commits the bundle, so nondeterminism is commit churn in someone else's repository.
//   - **Human regions survive.** Every existing page is read and merged, never replaced.
//     A run that clobbered a correction would teach people to delete the tool.
//   - **Nothing executable, nothing secret.** Markdown and JSON only. The bundle is
//     committed and often published, and a generator that wrote a script into a repository
//     would be a supply-chain hazard rather than a knowledge artifact.
//
// # Deletion
//
// A page whose concept is gone is deleted when, and only when, nobody has written on it —
// ADR 0010. The split is the whole of issue #10, and the two halves fail in opposite
// directions:
//
//   - **Never deleting** leaves a page describing a module that is not there, with plausible
//     `edges`, an `attributes` block, and a `resource:` naming a commit where the code really
//     did exist. That reads as authoritative, which makes it more expensive than a missing
//     page, and it survived every gate: build did not mention it and verify exited 0.
//   - **Always deleting** takes a human's `## Notes` with it the first time a directory is
//     renamed, which is the one failure this package is built to prevent — see page.go.
//
// So the test is whether the page still holds exactly the skeleton a first emit wrote
// (prunable, below). If it does, deleting it destroys nothing and the run says which files it
// removed. If anything else is there, the page is kept and *reported*, which leaves a human
// the decision that is theirs. Every uncertainty — an unreadable file, a title that does not
// round-trip, a skeleton from an older version — falls toward keeping it.
//
// verify's severity mirrors this exactly, and that is what makes the gate actionable: it fails
// on a surplus page a build would remove, because the remedy is to run build, and warns about
// one build kept, because there is no remedy a command can perform.

// BundleDir is the directory a bundle lives in, relative to the repository root.
const BundleDir = ".signpost"

// Reserved page names, per OKF §9 and §8. Named constants because verify checks them and
// two spellings of the same filename would be a conformance bug that only appears on a
// case-sensitive filesystem.
const (
	IndexPage    = "index.md"
	LogPage      = "log.md"
	ManifestFile = "manifest.json"
	// PracticesPage records what the repository declares about how it is built, tested,
	// gated, and owned — design §9.1. Reserved alongside the two above because its `type`
	// is checked in both directions: this filename must carry that type, and no other page
	// may.
	PracticesPage = "practices.md"
)

// Result reports what a write did, in enough detail for the CLI to say so.
type Result struct {
	// Written is the bundle-relative path of every file written, sorted.
	Written []string
	// Created counts pages that did not exist before this run.
	Created int
	// Updated counts pages that existed and whose bytes changed.
	Updated int
	// Unchanged counts pages whose bytes were already correct. A large number here is the
	// normal case on a re-run and is what byte-stability looks like from outside.
	Unchanged int
	// Preserved counts pages that had human text outside the managed regions, which this
	// run carried across. Reported because it is the number that says the compounding
	// mechanism is doing something.
	Preserved int
	// Downgraded lists pages whose human `verified:` no longer matches the resource being
	// described, per §6.1. Surfaced rather than silent: a reviewer needs to know to look
	// again.
	Downgraded []string
	// Stale lists pages with no corresponding node that were kept because somebody had
	// written on them. Reported, not deleted: the decision is theirs.
	Stale []string
	// Removed lists pages with no corresponding node that held nothing but the skeleton a
	// first emit wrote, so deleting them destroyed nothing. Named rather than counted, for
	// the same reason Downgraded is: a file this run deleted is the one thing in a build a
	// reader may want to recover from git.
	Removed []string
}

// Write emits the bundle for g into root/.signpost, merging with what is there.
func Write(root string, g *graph.Graph, opts Options) (*Result, error) {
	dir := filepath.Join(root, BundleDir)
	res := &Result{}

	files, err := renderAll(g, opts)
	if err != nil {
		return nil, err
	}

	// Directories are created for the pages that exist rather than unconditionally: an
	// empty `services/` in a repository with no services is a directory a reader opens and
	// learns nothing from.
	for _, rel := range sortedFileKeys(files) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		// 0o755 for the same reason the files below are 0o644: the bundle is committed and
		// often published, so a directory only its creator can enter is a surprise in
		// somebody else's checkout. Nothing here is private — every path in it is a path
		// already visible in the repository.
		// #nosec G301 -- world-traversable is the intent for a committed artifact.
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, fmt.Errorf("okf: creating %s: %w", filepath.Dir(rel), err)
		}
		merged, stat, err := mergeOnDisk(full, files[rel], opts)
		if err != nil {
			return nil, err
		}
		if err := writeIfChanged(full, merged, stat, res); err != nil {
			return nil, err
		}
		res.Written = append(res.Written, rel)
		if stat.preserved {
			res.Preserved++
		}
		if stat.downgraded {
			res.Downgraded = append(res.Downgraded, rel)
		}
	}
	sort.Strings(res.Written)

	if err := sweepStale(dir, files, res); err != nil {
		return nil, err
	}
	return res, nil
}

// renderAll builds every file's content, keyed by bundle-relative slash path.
//
// Built entirely in memory before anything is written, which is what makes a failure
// atomic in the way that matters: a graph that produces a bad page fails before the first
// file lands, rather than leaving a half-written bundle that verify then reports as
// corrupt.
func renderAll(g *graph.Graph, opts Options) (map[string]string, error) {
	out := make(map[string]string, len(g.Nodes())+3)
	for _, n := range g.Nodes() {
		rel := strings.TrimPrefix(pagePath(n.ID), "/")
		if err := checkPageRel(rel); err != nil {
			return nil, err
		}
		if _, dup := out[rel]; dup {
			// Two nodes rendering to one path would mean one silently overwrote the other,
			// producing a bundle that describes fewer things than the graph contains.
			// assemble/id.go's collision resolution is what prevents it; this is the
			// assertion that it worked.
			return nil, fmt.Errorf("okf: two nodes both render to %s", rel)
		}
		out[rel] = pageFor(g, n, opts).Render()
	}
	out[IndexPage] = indexPage(g, opts).Render()
	out[LogPage] = logPage(g, opts).Render()
	// Rendered only when there is something to say. Unlike `role`, this does not depend on
	// a model, so a build normally does write it — but a caller that did not run the
	// analysis (a graph assembled in a test, or a future caller with no manifest pass) would
	// otherwise get a page whose every section reads as an absence, which is §4.2's rule
	// exactly: unmeasured must not render as measured. Merge keeps a page found on disk, so
	// an existing practices.md survives a run that renders none.
	if opts.Practices != "" {
		out[PracticesPage] = practicesPage(opts).Render()
	}
	man, err := manifestJSON(g, opts)
	if err != nil {
		return nil, err
	}
	out[ManifestFile] = man
	return out, nil
}

// checkPageRel rejects a page path that would escape the bundle directory or name
// something other than a markdown page.
//
// Node IDs are derived from repository directory names, which is untrusted input: a
// directory named `../../etc` would otherwise write outside the bundle. assemble's slug
// already strips the characters that would do it, so this is a second gate on the same
// property rather than the only one — worth having because the two are in different
// packages and a change to one should not be able to break the other silently.
func checkPageRel(rel string) error {
	if rel == "" || !strings.HasSuffix(rel, ".md") {
		return fmt.Errorf("okf: page path %q is not a markdown page", rel)
	}
	if strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") || filepath.IsAbs(rel) {
		return fmt.Errorf("okf: page path %q escapes the bundle", rel)
	}
	segs := strings.Split(rel, "/")
	for _, seg := range segs {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("okf: page path %q has an empty or relative segment", rel)
		}
	}
	// A node ID ending in `/` produces the filename `.md`, which is a dotfile with no
	// basename rather than a page: on a POSIX checkout it is hidden, and it names nothing a
	// link could describe. Caught here because it passes every other check above.
	if segs[len(segs)-1] == ".md" {
		return fmt.Errorf("okf: page path %q has no filename before the extension", rel)
	}
	return nil
}

// writeStat records what merging one page found.
type writeStat struct {
	existed    bool
	preserved  bool
	downgraded bool
}

// mergeOnDisk reads an existing page and merges the generated content into it.
func mergeOnDisk(full, generated string, opts Options) (string, writeStat, error) {
	var st writeStat
	existing, err := os.ReadFile(full) // #nosec G304 -- path is built from the bundle dir and a validated relative page path
	switch {
	case errors.Is(err, os.ErrNotExist):
		return generated, st, nil
	case err != nil:
		return "", st, fmt.Errorf("okf: reading %s: %w", full, err)
	}
	st.existed = true

	// A non-markdown file needs no merge: manifest.json is regenerated wholesale, since
	// every field in it is a machine record of a run and none of it is a human's to edit.
	if !strings.HasSuffix(full, ".md") {
		return generated, st, nil
	}

	// Normalised before parsing, so a CRLF checkout does not read as a page whose every
	// line differs from the generated one. See normalizeRead: without this, st.preserved
	// below is true for every page and the run claims human notes nobody wrote.
	prev := ParsePage(normalizeRead(string(existing)))
	next := ParsePage(generated)
	st.preserved = strings.TrimSpace(prev.HumanText()) != strings.TrimSpace(next.HumanText())

	// Compared against the resource *this page* now describes, read out of the generated
	// frontmatter rather than rebuilt from opts. A page's resource includes its own path, so
	// checking against the bundle's base URI would compare a module's verification to the
	// repository root and report every reviewed page as downgraded on every run.
	if downgrade(readVerified(prev.Frontmatter), readResource(next.Frontmatter)) {
		// The page describes a different commit than the one a human reviewed. The block
		// itself is kept — the reviewer's name and date are the audit trail — and the fact
		// that it no longer holds is written into the page as a generated `status:` key, so
		// a reader who never runs signpost still sees it. Marked before the merge so the
		// status lands in the generated half, above the human keys.
		next.Frontmatter = withStatus(next.Frontmatter, statusStaleVerification)
		st.downgraded = true
	}
	return prev.Merge(next).Render(), st, nil
}

// writeIfChanged writes only when the bytes differ, and counts what happened.
//
// The read-compare-write is not an optimisation. A build that rewrote every file with
// identical content would still update mtimes, which makes `git status` clean but a
// filesystem watcher fire on every page — and on a large bundle that is the difference
// between a tool people run on save and one they do not.
func writeIfChanged(full, content string, st writeStat, res *Result) error {
	if st.existed {
		cur, err := os.ReadFile(full) // #nosec G304 -- see mergeOnDisk
		// Compared after normalising, so a CRLF checkout is recognised as unchanged rather
		// than rewritten on every run. The write below still emits LF: the bundle's line
		// endings are signpost's to choose, and choosing one is what makes it byte-stable
		// across the platforms design §4.6 requires agree.
		if err == nil && normalizeRead(string(cur)) == content {
			res.Unchanged++
			return nil
		}
		res.Updated++
	} else {
		res.Created++
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil { // #nosec G306 -- a committed, world-readable knowledge artifact
		return fmt.Errorf("okf: writing %s: %w", full, err)
	}
	return nil
}

// sweepStale deletes the pages this run did not produce and nobody has written on, and
// reports the rest.
//
// Ordered after every write rather than before, which is what keeps a failed build from
// deleting anything: renderAll has already succeeded, so the concept set is real. A run that
// pruned first and then failed to render would remove pages on the strength of a graph it
// turned out it could not emit.
func sweepStale(dir string, files map[string]string, res *Result) error {
	stale, err := findStale(dir, files)
	if err != nil {
		return err
	}
	for _, rel := range stale {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		src, err := os.ReadFile(full) // #nosec G304 -- a path from a walk of the bundle directory
		if err != nil {
			// Unreadable. Reported as kept, because "delete a file I could not read" is the one
			// action here with no way back, and the reason it could not be read may be that it
			// is open in the editor of the person whose notes are in it.
			res.Stale = append(res.Stale, rel)
			continue
		}
		if !prunable(normalizeRead(string(src))) {
			res.Stale = append(res.Stale, rel)
			continue
		}
		if err := os.Remove(full); err != nil {
			// A page that could not be removed is a page that is still there, so it is reported
			// as kept rather than as removed. Not a build failure: the bundle it wrote is
			// correct, and this is the sweep over what was already on disk.
			res.Stale = append(res.Stale, rel)
			continue
		}
		res.Removed = append(res.Removed, rel)
	}
	// Directories are not removed, even when the sweep emptied one. An empty `services/`
	// carries no wrong claim, `git` does not track it, and a rmdir walking upward from a
	// deleted page is a loop with the bundle root at the end of it.
	sort.Strings(res.Stale)
	sort.Strings(res.Removed)
	return nil
}

// prunable reports whether a page holds nothing but the skeleton a first emit wrote, so that
// deleting it destroys nothing a person put there.
//
// Three things have to be true, and each one is a direction the answer falls when in doubt:
//
//   - It is a page signpost wrote: frontmatter, and at least one managed region. A markdown
//     file somebody dropped in the bundle directory has neither, and is not signpost's to
//     delete no matter what the graph says.
//   - No human frontmatter key. `verified:` above all — a review someone performed is an audit
//     trail, and a renamed directory is not a reason to destroy it. Any unrecognised key
//     counts, because signpost did not write it.
//   - Nothing outside the managed regions but headings and the notes invitation, which is
//     exactly what emit.go seeds a new page with.
//
// The heading exemption is the one concession, and it is worth naming: a heading a human
// *rewrote* reads here as skeleton, so renaming a heading and nothing else does not save the
// page. The alternative is comparing against the heading a first emit would have written,
// which cannot be done — the node is gone, so its title is gone with it. Losing a heading
// somebody retyped is a smaller loss than keeping every orphan forever, which is what the
// strict reading would amount to.
func prunable(src string) bool {
	p := ParsePage(src)
	if !p.HasFrontmatter {
		return false
	}
	if carryHumanKeys(p.Frontmatter) != "" {
		return false
	}
	managed := false
	for _, r := range p.Body {
		if r.Managed() {
			managed = true
			break
		}
	}
	if !managed {
		return false
	}
	for _, line := range strings.Split(p.HumanText(), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if s == strings.TrimSpace(notesInvitation()) {
			continue
		}
		return false
	}
	return true
}

// findStale lists existing bundle pages that this run did not produce.
func findStale(dir string, files map[string]string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			// A directory that cannot be read is reported as no stale pages within it
			// rather than as a failed build: the bundle was written successfully, and this
			// is a diagnostic pass over it.
			return nil //nolint:nilerr // see comment
		}
		if d.IsDir() {
			// cache/ is gitignored, content-hash keyed, and not a page. Skipped entirely
			// rather than reported as thousands of stale files.
			if d.Name() == "cache" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil //nolint:nilerr // as above
		}
		slash := filepath.ToSlash(rel)
		if _, ok := files[slash]; !ok && !reservedPage(slash) {
			out = append(out, slash)
		}
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("okf: scanning %s: %w", dir, err)
	}
	sort.Strings(out)
	return out, nil
}

// reservedPage reports whether a bundle-relative path is one signpost owns by name.
//
// Used to keep findStale from reporting a reserved page a given run did not render.
// "Describes a concept that no longer exists" is the wrong claim about practices.md: it
// describes the repository, which still exists, and a run renders none only when it had no
// analysis to base one on. Reporting it would send someone looking for a deleted directory.
func reservedPage(rel string) bool {
	switch rel {
	case IndexPage, LogPage, PracticesPage:
		return true
	}
	return false
}

func sortedFileKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// indexPage builds the bundle root, per OKF §8's grouped-and-described listing.
//
// Grouped by concern with one described line per concept, deliberately: the index exists so
// an agent can pick the three pages it needs instead of reading the bundle, and a flat list
// of eighty filenames does not let it. The description on each line is what makes the
// choice possible without opening anything.
func indexPage(g *graph.Graph, opts Options) *Page {
	fm := &yamlDoc{}
	fm.setScalar("okf_version", "0.2")
	fm.setScalar("type", "Index")
	fm.setScalar("title", "Repository map")
	fm.setScalar("description", indexDescription(g))
	if res := resourceFor(opts.Resource, ""); res != "" {
		fm.setScalar("resource", res)
	}
	if opts.Actor != "" && opts.Date != "" {
		fm.set("generated", flowMap(
			yamlPair{"by", scalar(string(opts.Actor))},
			yamlPair{"at", scalar(opts.Date)},
		))
	}
	body := []Region{
		humanRegion(heading(1, "Repository map")),
		managedRegion(regionIndex, indexBody(g, opts)),
		humanRegion("\n" + heading(2, "Notes") + notesInvitation()),
	}
	return NewPage(fm.String(), body...)
}

func indexDescription(g *graph.Graph) string {
	nodes, edges := g.Counts()
	return "Structural map of this repository: " + plural(nodes, "concept") +
		", " + plural(edges, "relationship") + "."
}

// indexBody groups pages by kind, with the hubs called out first.
func indexBody(g *graph.Graph, opts Options) string {
	var b strings.Builder
	b.WriteString("Start here. Each line names a page and what is on it.\n")

	// Linked from the index rather than left to be found, and near the top: the practices
	// page answers "how do I build and test this" before any module page becomes useful, and
	// an agent that reads the index and stops has the pointer. Conditional on the same
	// thing renderAll is, so the index never links a page that was not written — a dangling
	// bundle-absolute link is a verify failure.
	if opts.Practices != "" {
		b.WriteString("\n" + heading(3, "How work is done here"))
		b.WriteString("- " + proseLink("How work is done here", "/"+PracticesPage) +
			" — what this repository declares about building, testing, gating, and " +
			"ownership, and what it does not.\n")
	}

	if hubs := hubLines(g); hubs != "" {
		b.WriteString("\n" + heading(3, "Most connected"))
		b.WriteString("The places a wrong assumption propagates furthest, so the places to " +
			"read first.\n\n")
		b.WriteString(hubs)
	}

	for _, k := range indexKindOrder {
		ns := g.NodesOfKind(k)
		if len(ns) == 0 {
			continue
		}
		b.WriteString("\n" + heading(3, kindHeading(k)))
		for _, n := range ns {
			b.WriteString("- " + proseLink(n.Title, pagePath(n.ID)))
			if n.Description != "" {
				b.WriteString(" — " + n.Description)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// indexKindOrder fixes the section order.
//
// Not sorted alphabetically: the order is what a reader wants first, which is modules and
// services before the reference material they depend on. Fixed in one place so the index is
// byte-stable and so the reasoning is reviewable rather than implied by a sort.
var indexKindOrder = []graph.Kind{
	graph.KindModule,
	graph.KindService,
	graph.KindInterface,
	graph.KindDataStore,
	graph.KindDocument,
	graph.KindSymbol,
	graph.KindExternal,
}

func kindHeading(k graph.Kind) string {
	switch k {
	case graph.KindModule:
		return "Modules"
	case graph.KindService:
		return "Services"
	case graph.KindInterface:
		return "Interfaces"
	case graph.KindDataStore:
		return "Data stores"
	case graph.KindDocument:
		return "Documents"
	case graph.KindExternal:
		return "External dependencies"
	case graph.KindSymbol:
		return "Symbols"
	}
	return string(k)
}

// hubLines renders the top-degree nodes.
func hubLines(g *graph.Graph) string {
	const maxHubs = 5
	var b strings.Builder
	for _, d := range g.Hubs(maxHubs) {
		// Degree 0 nodes are not hubs. On a graph with fewer than maxHubs connected nodes
		// the tail of Hubs() is everything else, and listing an unconnected page under
		// "most connected" would be actively misleading.
		if d.Total == 0 {
			break
		}
		n := g.Node(d.ID)
		if n == nil {
			continue
		}
		b.WriteString("- " + proseLink(n.Title, pagePath(n.ID)) + " — " +
			plural(d.Total, "relationship") + " (" + strconv.Itoa(d.In) + " in, " +
			strconv.Itoa(d.Out) + " out)\n")
	}
	return b.String()
}

// logPage builds the date-grouped change history OKF §9 reserves.
//
// The region name carries the date, which is what makes this a history rather than a status
// line. A run writes `log-2026-07-30`; the next run at a later date does not generate that
// name, so Merge keeps it verbatim as a region this version no longer produces and appends
// the new one. Past entries therefore accumulate without any code here appending to a file,
// and without Merge growing a special case for one page — a history whose past entries a
// generator rewrites is not a history, and the way to guarantee that is for the generator to
// have no name for them.
//
// Two consequences worth stating rather than discovering:
//
//   - **Oldest first.** Merge appends, because inserting would mean choosing a position
//     inside text a human may have written around. Newest-first would read better and is not
//     available at the price of that guarantee.
//   - **One entry per date.** Two commits on the same day collapse to one region, the later
//     run winning. That is what "date-grouped" means, and it is also what keeps a repository
//     that rebuilds on every push from growing an entry per push.
//
// practicesPage records what the repository declares about how work is done here.
//
// One page rather than a section on each module page, because the facts are repository-wide:
// a test command, a CI gate and a licence are properties of the repository, and repeating
// them on every module page would be the same claim eighty times. Where a finding *is*
// per-module, internal/practice says so in the finding's own text.
//
// The body arrives finished, in opts.Practices. This function contributes the frontmatter,
// the heading, and the Notes invitation — the same division of labour as every other page
// here, and the reason the emitter cannot overstate what was measured: it does not know what
// the findings say.
func practicesPage(opts Options) *Page {
	fm := &yamlDoc{}
	fm.setScalar("type", "Practices")
	fm.setScalar("title", "How work is done here")
	fm.setScalar("description",
		"What this repository declares about building, testing, gating, and ownership — "+
			"and what it does not.")
	if res := resourceFor(opts.Resource, ""); res != "" {
		fm.setScalar("resource", res)
	}
	if opts.Actor != "" && opts.Date != "" {
		fm.set("generated", flowMap(
			yamlPair{"by", scalar(string(opts.Actor))},
			yamlPair{"at", scalar(opts.Date)},
		))
	}
	body := []Region{
		humanRegion(heading(1, "How work is done here") +
			"Each line is something this repository states, or something it does not. " +
			"A missing declaration is not a criticism and there is no score here: it is a " +
			"fact about what an agent can rely on, and the absences are the ones worth " +
			"reading, because they are what it would otherwise have to guess.\n"),
		managedRegion(regionPractices, opts.Practices),
		humanRegion("\n" + heading(2, "Notes") + notesInvitation()),
	}
	return NewPage(fm.String(), body...)
}

func logPage(g *graph.Graph, opts Options) *Page {
	fm := &yamlDoc{}
	fm.setScalar("type", "Log")
	fm.setScalar("title", "Change log")
	fm.setScalar("description", "What each signpost run changed about this bundle.")
	body := []Region{
		humanRegion(heading(1, "Change log") +
			"One entry per date signpost ran, oldest first. Everything outside the managed " +
			"markers is yours, and a past entry is never rewritten.\n\n"),
		managedRegion(logRegion(opts.Date), logEntry(g, opts)),
	}
	return NewPage(fm.String(), body...)
}

// logRegion names the region holding one date's entry.
//
// An unknown or unusable date gets a fixed name rather than an empty or invalid suffix. The
// date reaches here from `git log`, so it is repository content: a name that failed
// validRegionName would be written by the emitter and then not recognised as a marker on the
// next read, which is the one failure mode that looks like working code — the region would
// silently become human text and stop regenerating.
func logRegion(date string) string {
	if name := "log-" + date; date != "" && validRegionName(name) {
		return name
	}
	return "log-unknown"
}

func logEntry(g *graph.Graph, opts Options) string {
	nodes, edges := g.Counts()
	var b strings.Builder
	date := opts.Date
	if date == "" {
		date = "unknown date"
	}
	b.WriteString("## " + date + "\n\n")
	b.WriteString("- Bundle rebuilt: " + plural(nodes, "concept") + ", " +
		plural(edges, "relationship") + ".\n")
	if opts.Actor != "" {
		b.WriteString("- Generated by `" + string(opts.Actor) + "`.\n")
	}
	if opts.Resource != "" {
		b.WriteString("- Describes `" + opts.Resource + "`.\n")
	}
	return b.String()
}

func plural(n int, unit string) string {
	s := strconv.Itoa(n) + " " + unit
	if n != 1 {
		s += "s"
	}
	return s
}
