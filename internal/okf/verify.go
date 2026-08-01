package okf

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/3rg0n/signpost/internal/graph"
	"github.com/3rg0n/signpost/internal/manifest"
)

// Bundle verification: design §4.6's five checks, and the one rule that gives them their
// value — **a failure is non-zero**.
//
// The failure mode this file exists to prevent is not a missed defect. It is a check that
// passes because it never ran. A bundle that is silently stale is worse than no bundle: it
// is confidently wrong, an agent acts on it, and the trust never comes back. So two things
// are true of everything below:
//
//   - Every check that did not run is *named* in Skipped. "No findings" from a check that
//     never executed has the same shape as a pass, and only one of them is one.
//   - Every check that did run reports what it covered, in Checked. A verify whose output
//     is the word "ok" cannot be distinguished from a verify that opened nothing.
//
// # Why verify re-renders instead of comparing shas
//
// The byte-stability check re-runs the emitter and merges the result against what is on
// disk — the same renderAll and mergeOnDisk `build` uses, not a reimplementation. That is
// deliberate: a verify with its own idea of what a page should contain would eventually
// disagree with the emitter, and then one of them is wrong and neither is obviously so.
// Sharing the code makes "verify passes" mean exactly "a build would change nothing",
// which is the property CI actually needs.
//
// The corollary is that verify must be invoked with the same flags as the build it is
// checking. `-repo` feeds every page's `resource:`, and `-no-history` removes it, so a
// verify run with different flags reports differences that are real but describe the
// invocation rather than the bundle.
//
// # What is a failure and what is a warning
//
// A failure is a claim the bundle makes that is not true: an unreadable page, a link to a
// page that does not exist, a resource naming a commit that is not the one being
// described, content a rebuild would change. A warning is litter: a page whose concept no
// longer exists. The split follows from bundle.go's deletion rule — pages are never
// deleted, because a rename would otherwise silently destroy someone's notes — and a gate
// that failed on the litter it is designed to leave behind would be a gate people disable
// on the first rename.

// FindingKind names what kind of problem was found, so a caller can group findings without
// matching on message text.
type FindingKind string

const (
	// FindingMissingBundle means there is nothing at .signpost to verify.
	FindingMissingBundle FindingKind = "missing-bundle"
	// FindingConformance is an OKF §11 violation: unparseable frontmatter, an empty
	// `type`, or a reserved filename carrying the wrong one.
	FindingConformance FindingKind = "conformance"
	// FindingBrokenLink is an `edges[].to`, a `sources[].resource`, or a prose link that
	// names a path the bundle does not contain.
	FindingBrokenLink FindingKind = "broken-link"
	// FindingStaleResource means a page or the manifest describes a different commit than
	// the one being verified.
	FindingStaleResource FindingKind = "stale-resource"
	// FindingOutOfDate means a rebuild would change the file's bytes.
	FindingOutOfDate FindingKind = "out-of-date"
	// FindingMissingPage means the bundle lacks a page this repository has a concept for.
	FindingMissingPage FindingKind = "missing-page"
	// FindingOrphanPage means a page describes a concept the repository no longer has.
	// A warning, never a failure: see this file's header.
	FindingOrphanPage FindingKind = "orphan-page"
	// FindingStaleVerification means a page carries the `status:` mark §6.1 writes when a
	// human's review no longer matches the resource. A warning: the bundle is correct, and
	// what it needs is a reviewer rather than a rebuild.
	FindingStaleVerification FindingKind = "stale-verification"
)

// Finding is one problem, on one file.
type Finding struct {
	// Kind groups the finding. Page is the bundle-relative path, empty for a finding about
	// the bundle as a whole.
	Kind FindingKind
	Page string
	// Detail says what is wrong in the terms the reader needs to fix it.
	Detail string
}

func (f Finding) String() string {
	if f.Page == "" {
		return f.Detail
	}
	return f.Page + ": " + f.Detail
}

// VerifyCounts is what verify actually opened and resolved.
type VerifyCounts struct {
	Pages int
	// Links is bundle-absolute prose links resolved. Edges and Sources are frontmatter
	// `edges[].to` and `sources[].resource` values resolved.
	Links   int
	Edges   int
	Sources int
}

// VerifyResult is what one verification found.
type VerifyResult struct {
	// Findings are failures. A non-empty Findings means the command exits non-zero, and
	// that is the only rule a caller needs to know.
	Findings []Finding
	// Warnings are problems that do not make the bundle wrong. Reported because an
	// unreported warning is indistinguishable from a clean result.
	Warnings []Finding
	Checked  VerifyCounts
	// Skipped names each check that did not run, and why.
	Skipped []string
}

// OK reports whether the bundle passed.
func (r *VerifyResult) OK() bool { return len(r.Findings) == 0 }

func (r *VerifyResult) fail(kind FindingKind, page, format string, args ...any) {
	r.Findings = append(r.Findings, Finding{kind, page, fmt.Sprintf(format, args...)})
}

func (r *VerifyResult) warn(kind FindingKind, page, format string, args ...any) {
	r.Warnings = append(r.Warnings, Finding{kind, page, fmt.Sprintf(format, args...)})
}

// Verify checks the bundle at root/.signpost against the repository g was built from.
//
// g and opts must be the ones `build` would be invoked with, for the reason this file's
// header gives: the check is "would a build change anything", and a build with different
// options is a different question.
func Verify(root string, g *graph.Graph, opts Options) (*VerifyResult, error) {
	dir := filepath.Join(root, BundleDir)
	res := &VerifyResult{}

	disk, err := readBundle(dir)
	if err != nil {
		return nil, err
	}
	if disk == nil {
		// Not a finding among findings — the only one. Every check below would report
		// every page as missing, which is eighty lines saying what this one line says.
		res.fail(FindingMissingBundle, "",
			"no bundle at %s — run `signpost build` and commit the result", BundleDir)
		return res, nil
	}

	if opts.AsOfBundle {
		// Adopted before rendering, so everything below is a comparison of content. See
		// Options.AsOfBundle for why a branch has no other honest answer.
		opts = adoptProvenance(res, dir, opts)
	}

	// What a build would write now. Built before anything is compared so that a graph the
	// emitter cannot render fails as a render error rather than as a bundle defect.
	fresh, err := renderAll(g, opts)
	if err != nil {
		return nil, err
	}

	checkConformance(res, disk)
	checkStaleResource(res, dir, disk, fresh, opts)
	checkUpToDate(res, dir, disk, fresh, opts)
	checkOrphans(res, disk, fresh)
	return res, nil
}

// onDisk is the bundle as it exists: page text for the markdown, and the path set every
// link resolves against.
type onDisk struct {
	// pages maps bundle-relative slash path to content, for .md files only.
	pages map[string]string
	// files is every file in the bundle, pages included. Links resolve against this rather
	// than against the filesystem: a set lookup cannot be fooled by a path that escapes
	// the bundle, and it makes resolution independent of the platform's path rules.
	files map[string]bool
	// order is pages' keys, sorted, so findings come out in a stable order. A verify whose
	// output reordered between runs would be unreviewable in a CI diff.
	order []string
}

// readBundle reads the bundle, or returns nil when there is none.
//
// The walk goes through os.Root rather than filepath.WalkDir: reading a path a walk handed
// back re-resolves it, and between the walk and the read a component can become a symlink
// pointing anywhere. The bundle is a committed artifact, so its contents are as untrusted as
// the rest of a checked-out tree. A root confines every read to the bundle by construction,
// which is also why the relative paths below need no validating — they are the walk's own
// keys, not strings derived from a filesystem path.
func readBundle(dir string) (*onDisk, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("okf: reading %s: %w", dir, err)
	}
	defer root.Close() //nolint:errcheck // read-only; a close error cannot invalidate what was read

	d := &onDisk{pages: map[string]string{}, files: map[string]bool{}}
	err = fs.WalkDir(root.FS(), ".", func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			// Unlike findStale, an unreadable directory here is returned rather than
			// skipped. findStale is a diagnostic pass over a bundle that was written
			// successfully; this is the gate, and a gate that treats a directory it could
			// not open as containing no problems is the false pass this file is against.
			return fmt.Errorf("okf: walking %s: %w",
				filepath.Join(dir, filepath.FromSlash(p)), err)
		}
		if e.IsDir() {
			// cache/ is gitignored and content-hash keyed. Not part of the artifact, and
			// nothing links to it.
			if e.Name() == "cache" {
				return fs.SkipDir
			}
			return nil
		}
		d.files[p] = true
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		b, err := root.ReadFile(p)
		if err != nil {
			return fmt.Errorf("okf: reading %s: %w", p, err)
		}
		d.pages[p] = normalizeRead(string(b))
		d.order = append(d.order, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(d.order)
	return d, nil
}

// checkConformance runs the OKF §11 checks and the link resolution, per page.
//
// Combined into one pass because both need the page parsed, and parsing every page twice
// to keep two functions tidy is the kind of tidiness a reader pays for.
func checkConformance(res *VerifyResult, disk *onDisk) {
	skippedLinks := 0
	for _, rel := range disk.order {
		src := disk.pages[rel]
		page := ParsePage(src)
		res.Checked.Pages++

		if !page.HasFrontmatter {
			// Everything below reads the frontmatter, and a page without any has no `type`,
			// no `resource`, and no `edges`. One finding rather than four.
			res.fail(FindingConformance, rel,
				"no YAML frontmatter — an OKF page opens with a `---` fenced block")
			continue
		}
		fm, diag := manifest.ParseYAMLDoc(page.Frontmatter)
		if diag.Malformed {
			// A failure, and checked before the mapping test below, because a broken document
			// still reads back as a mapping: an unterminated flow collection loses everything
			// after it and what remains is a well-formed map of whatever came first. That is
			// precisely how issue #9 passed — four pages lost seven edges between them, the
			// frontmatter parsed as a mapping, and verify exited 0.
			//
			// The severity is the whole point of splitting this from Incomplete. This is not a
			// construct signpost stepped over; it is YAML no conforming reader can read, so
			// every consumer of this page silently sees less than it says. A bundle is committed
			// and read by people who did not build it (§4.6), and a checker that calls that a
			// nit is the false pass verify exists to prevent.
			res.fail(FindingConformance, rel,
				"frontmatter is not parseable YAML, so a conforming reader loses everything "+
					"after the first fault: %s", diag.Summary())
			continue
		}
		if fm == nil || fm.Kind != manifest.KindMap {
			res.fail(FindingConformance, rel, "frontmatter is not a YAML mapping")
			continue
		}
		if diag.Incomplete() {
			// A warning, not a failure. The parser is tolerant by design (ADR 0001), and a
			// note means it read the frontmatter while stepping over something — which is a
			// thing to look at, not a reason to fail a build over a human's comment.
			res.warn(FindingConformance, rel, "frontmatter partly unread: %s", diag.Summary())
		}
		checkPageType(res, rel, fm)
		if fm.Get("status").String() == statusStaleVerification {
			res.warn(FindingStaleVerification, rel,
				"a human's `verified:` block no longer matches this page's resource; "+
					"re-review and record the current `resource:` to clear it")
		}
		checkPageLinks(res, disk, rel, page, fm, &skippedLinks)
	}
	if skippedLinks > 0 {
		res.Skipped = append(res.Skipped, fmt.Sprintf(
			"%d link(s) not checked: only bundle-absolute targets (`/page.md`) are resolved, "+
				"because a relative one may name a file in the repository rather than in the bundle",
			skippedLinks))
	}
}

// reservedTypes fixes the `type` the reserved filenames must carry, per OKF §8 and §9.
//
// Checked in both directions below: the reserved name must have its type, and no other
// page may claim it. A second page typed `Index` would give a consumer two roots and no
// way to choose, which is worse than having none.
var reservedTypes = map[string]string{
	IndexPage:     "Index",
	LogPage:       "Log",
	PracticesPage: "Practices",
}

func checkPageType(res *VerifyResult, rel string, fm *manifest.Node) {
	got := fm.Get("type").String()
	if got == "" {
		res.fail(FindingConformance, rel, "`type` is missing or empty")
		return
	}
	if want, reserved := reservedTypes[rel]; reserved {
		if got != want {
			res.fail(FindingConformance, rel,
				"reserved page must be type %q, not %q", want, got)
		}
		return
	}
	for name, t := range reservedTypes {
		if got == t {
			res.fail(FindingConformance, rel,
				"type %q is reserved for %s", t, name)
		}
	}
}

// checkPageLinks resolves everything on a page that names another page: the typed `edges`
// list, `sources[].resource`, and the prose links §3.1 emits alongside the list.
//
// All three are checked rather than just the typed list, because the prose links are the
// only ones a generic OKF consumer follows — OKF links are untyped by design, so a broken
// prose link is broken for exactly the readers the bundle exists to serve, and it would be
// invisible to a check that only read frontmatter.
func checkPageLinks(res *VerifyResult, disk *onDisk, rel string, page *Page,
	fm *manifest.Node, skipped *int) {
	for _, e := range fm.Get("edges").Seq() {
		to := e.Get("to").String()
		if to == "" {
			res.fail(FindingConformance, rel, "an `edges` entry has no `to`")
			continue
		}
		res.Checked.Edges++
		if !resolves(disk, to) {
			res.fail(FindingBrokenLink, rel, "edge target %s is not in the bundle", to)
		}
	}
	for _, s := range fm.Get("sources").Seq() {
		src := s.Get("resource").String()
		if !strings.HasPrefix(src, "/") {
			// A source may cite something outside the bundle — a commit URI, a spec URL —
			// and design §4.6 asks only that the ones naming a bundle page resolve.
			continue
		}
		res.Checked.Sources++
		if !resolves(disk, src) {
			res.fail(FindingBrokenLink, rel, "source %s is not in the bundle", src)
		}
	}
	for _, r := range page.Body {
		targets, n := bundleLinks(r.Text)
		*skipped += n
		for _, t := range targets {
			res.Checked.Links++
			if resolves(disk, t) {
				continue
			}
			// The region is named because it says whose problem it is. A broken link in a
			// managed region is signpost's bug or a stale bundle; one in human text is a
			// typo the person who wrote it can fix.
			where := "prose"
			if r.Managed() {
				where = "the generated `" + r.Name + "` region"
			}
			res.fail(FindingBrokenLink, rel, "link %s in %s is not in the bundle", t, where)
		}
	}
}

// resolves reports whether a bundle-absolute target names a file in the bundle.
func resolves(disk *onDisk, target string) bool {
	rel, ok := bundleRel(target)
	if !ok {
		return false
	}
	return disk.files[rel]
}

// bundleRel turns a link target into the bundle-relative path it names.
//
// Cleaned before lookup, so `/modules/../index.md` resolves to the page it names rather
// than failing as a string mismatch — and rejected if cleaning walks out of the bundle,
// which is a link that cannot resolve no matter what is on disk.
func bundleRel(target string) (string, bool) {
	if !strings.HasPrefix(target, "/") {
		return "", false
	}
	// A fragment names a heading within the page; the page is what has to exist.
	if i := strings.IndexByte(target, '#'); i >= 0 {
		target = target[:i]
	}
	rel := path.Clean(strings.TrimPrefix(target, "/"))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// bundleLinks returns the bundle-absolute markdown link targets in a span of text, and how
// many links it deliberately left unchecked.
//
// Only `/`-prefixed targets are returned, and that is a decision about false positives
// rather than an oversight. A relative link in a page is genuinely ambiguous — a human
// writing `../src/main.go` in their notes means a file in the repository, not in the
// bundle — and a gate that failed on those would be a gate people turn off. Every link
// signpost itself writes is bundle-absolute (see pagePath), so the checked set covers all
// of the generated content and the unchecked count is reported rather than swallowed.
//
// Code — fenced blocks and inline spans — is skipped: a note showing an example link is not
// a link, and failing a build over one would make the page's own documentation unwritable.
func bundleLinks(text string) (targets []string, skipped int) {
	fence := ""
	for off := 0; off < len(text); {
		line, next := nextLine(text, off)
		off = next

		if f := codeFence(line); f != "" {
			switch {
			case fence == "":
				fence = f
			case strings.HasPrefix(f, fence):
				// Closed only by a fence of the same character and at least the same
				// length, per CommonMark: a ``` inside a ~~~~ block is content.
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		for _, t := range lineLinks(stripCodeSpans(line)) {
			if strings.HasPrefix(t, "/") {
				targets = append(targets, t)
				continue
			}
			skipped++
		}
	}
	return targets, skipped
}

// stripCodeSpans blanks the contents of inline code spans, keeping the line's length so a
// finding's position still lines up with what the reader sees.
//
// Unterminated runs are left alone: a lone backtick in prose is prose, and treating the
// rest of the line as code would hide a real link behind a typo.
func stripCodeSpans(line string) string {
	b := []byte(line)
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		// A span opened by N backticks is closed by exactly N, per CommonMark, which is how
		// a span can contain a backtick at all: ``a ` b``.
		open := i
		for i < len(b) && b[i] == '`' {
			i++
		}
		run := b[open:i]
		end := indexRun(b, i, len(run))
		if end < 0 {
			return string(b)
		}
		for j := i; j < end; j++ {
			b[j] = ' '
		}
		i = end + len(run)
	}
	return string(b)
}

// indexRun finds the next run of exactly n backticks at or after from, or -1.
func indexRun(b []byte, from, n int) int {
	for i := from; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		start := i
		for i < len(b) && b[i] == '`' {
			i++
		}
		if i-start == n {
			return start
		}
	}
	return -1
}

// codeFence returns the fence a line opens or closes, or "".
func codeFence(line string) string {
	s := strings.TrimSpace(strings.TrimRight(line, "\r"))
	for _, c := range []string{"```", "~~~"} {
		if strings.HasPrefix(s, c) {
			n := 0
			for n < len(s) && s[n] == c[0] {
				n++
			}
			return s[:n]
		}
	}
	return ""
}

// lineLinks returns the targets of every inline markdown link on a line.
//
// Deliberately small: it finds `](`, reads to the first `)`, and drops a title. It does not
// implement CommonMark. What it has to be right about is the shape signpost emits and the
// shape a person writes by hand, and being wrong in the other direction is bounded — an
// unrecognised link is counted as unchecked rather than reported as broken.
func lineLinks(line string) []string {
	var out []string
	for i := 0; i+1 < len(line); i++ {
		if line[i] != ']' || line[i+1] != '(' {
			continue
		}
		rest := line[i+2:]
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			break
		}
		target := strings.TrimSpace(rest[:end])
		i += 2 + end
		// A title after the target: `](/a.md "Why")`. Cut at the first space, which also
		// discards the pathological case of an unquoted target containing one.
		if sp := strings.IndexAny(target, " \t"); sp >= 0 {
			target = target[:sp]
		}
		// The angle-bracket form, `](</a b.md>)`.
		target = strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">")
		if target == "" || strings.HasPrefix(target, "#") {
			continue
		}
		out = append(out, target)
	}
	return out
}

// checkStaleResource compares what the bundle says it describes against what is being
// verified — design §4.6's "`resource` shas match the commit being described".
//
// The manifest is checked first and separately, because it is the one place the bundle
// states its commit once. When it disagrees, every page disagrees too, and reporting
// eighty pages would bury the one fact that explains all of them.
// adoptProvenance replaces the commit being verified with the one the bundle records, and
// says so in Skipped.
//
// Read from manifest.json rather than from a page because the manifest is the one file in the
// bundle no human has a claim on: every field in it is a machine record of the run that wrote
// it. Taking provenance from a page would mean taking it from a file people are invited to
// edit.
//
// Nothing here validates the recorded commit against git, and that is deliberate. The
// manifest reaches the tree the same way every other file does — through a commit — so it is
// exactly as authoritative as the source being analysed. Proving it names a commit in this
// branch's history would also fail on a squash merge or a rebase, where the recorded sha no
// longer exists but the content is perfectly current, and it would buy nothing: the content
// comparison runs either way, so a wrong stamp can mislabel which commit produced a page but
// cannot hide a stale one.
//
// A missing, unparseable, or resource-less manifest adopts nothing and leaves the strict
// comparison in place, which then reports it. A bundle that cannot say what it describes has
// no provenance at all, and inventing some to make the gate pass is the false pass this file
// exists to prevent.
func adoptProvenance(res *VerifyResult, dir string, opts Options) Options {
	man, err := readManifest(dir)
	if err != nil || man.Resource == "" {
		return opts
	}
	// The base only. Every page appends its own path to it, so each page's stamp stays derived
	// rather than copied: a page whose resource does not follow from the bundle's own base is
	// still reported by the content comparison below.
	opts.Resource = man.Resource
	opts.Date = man.Date
	res.Skipped = append(res.Skipped, fmt.Sprintf(
		"provenance not compared against this tree: content was checked as of the commit the "+
			"bundle records (%s). The bundle is built on the default branch only, so elsewhere "+
			"its stamp is older by construction; it is compared exactly there",
		man.Resource))
	return opts
}

// readManifest reads and parses the bundle's run record.
func readManifest(dir string) (*bundleManifest, error) {
	src, err := os.ReadFile(filepath.Join(dir, ManifestFile)) // #nosec G304 -- a fixed name under the bundle
	if err != nil {
		return nil, err
	}
	var man bundleManifest
	if err := json.Unmarshal(src, &man); err != nil {
		return nil, err
	}
	return &man, nil
}

func checkStaleResource(res *VerifyResult, dir string, disk *onDisk,
	fresh map[string]string, opts Options) {
	if opts.Resource == "" {
		// No commit to compare against: either the repository has no readable history or
		// verify was invoked with -no-history. Named rather than silently passing, because
		// this is precisely the check whose silent success destroys the tool's value.
		res.Skipped = append(res.Skipped,
			"staleness not checked: the commit being verified is unknown "+
				"(no git history, or -no-history)")
		return
	}
	want := resourceFor(opts.Resource, "")
	// Read from disk rather than from disk.pages, which holds markdown only. The manifest is
	// the one file in the bundle a human has no claim on — every field in it is a machine
	// record of a run — so it is read whole rather than parsed as a page.
	man, err := readManifest(dir)
	if err != nil {
		res.fail(FindingConformance, ManifestFile,
			"missing, unreadable, or not JSON; a bundle records the commit it describes here: %v",
			err)
		return
	}
	if man.Resource != want {
		res.fail(FindingStaleResource, ManifestFile,
			"describes %s, but this tree is %s — run `signpost build` and commit the result",
			orNone(man.Resource), want)
		// The per-page comparison is suppressed, not skipped: it would report every page,
		// and each report would be a restatement of the line above.
		res.Skipped = append(res.Skipped,
			"per-page resources not compared: the whole bundle describes another commit")
		return
	}
	for _, rel := range disk.order {
		gen, ok := fresh[rel]
		if !ok {
			continue // An orphan page. Reported by checkOrphans, which knows why.
		}
		got := readResource(ParsePage(disk.pages[rel]).Frontmatter)
		if exp := readResource(ParsePage(gen).Frontmatter); got != exp {
			res.fail(FindingStaleResource, rel, "resource is %s, expected %s",
				orNone(got), orNone(exp))
		}
	}
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// checkUpToDate asserts that a build would change nothing.
//
// This is design §4.6's byte-stability check, and it is stronger than re-running the
// emitter twice: it merges against what is on disk exactly as `build` does, so a page that
// stopped regenerating — a managed marker a hand edit broke, which ParsePage deliberately
// resolves toward "human text" — shows up here as content a build would change. That case
// is the cost page.go accepts to avoid deleting anyone's writing, and this is the check
// that makes it visible rather than permanent.
func checkUpToDate(res *VerifyResult, dir string, disk *onDisk,
	fresh map[string]string, opts Options) {
	for _, rel := range sortedFileKeys(fresh) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if !disk.files[rel] {
			res.fail(FindingMissingPage, rel,
				"the repository has this concept and the bundle has no page for it")
			continue
		}
		merged, _, err := mergeOnDisk(full, fresh[rel], opts)
		if err != nil {
			res.fail(FindingConformance, rel, "%v", err)
			continue
		}
		cur, err := os.ReadFile(full) // #nosec G304 -- built from the bundle dir and a rendered page path
		if err != nil {
			res.fail(FindingConformance, rel, "unreadable: %v", err)
			continue
		}
		// normalizeRead, or a CRLF checkout fails this check on every page in the bundle
		// while being byte-identical to what a build produces — and the remedy the message
		// names would not fix it.
		if normalizeRead(string(cur)) != merged {
			res.fail(FindingOutOfDate, rel,
				"a build would change this page — run `signpost build` and commit the result")
		}
	}
}

// checkOrphans reports pages describing concepts that are gone.
//
// A warning. bundle.go never deletes a page, so this is the litter that rule leaves, and
// failing on it would mean every rename turns CI red with no supported way to fix it —
// `-prune` is a v0.2 answer. Reported so the litter is visible and someone can decide.
func checkOrphans(res *VerifyResult, disk *onDisk, fresh map[string]string) {
	for _, rel := range disk.order {
		if _, ok := fresh[rel]; !ok {
			res.warn(FindingOrphanPage, rel,
				"describes a concept this repository no longer has (not deleted: "+
					"a rename would otherwise take your notes with it)")
		}
	}
}
