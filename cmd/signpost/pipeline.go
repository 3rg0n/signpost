package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/3rg0n/signpost/internal/assemble"
	"github.com/3rg0n/signpost/internal/discover"
	"github.com/3rg0n/signpost/internal/extract"
	"github.com/3rg0n/signpost/internal/graph"
	"github.com/3rg0n/signpost/internal/manifest"
	"github.com/3rg0n/signpost/internal/telemetry"
	"github.com/3rg0n/signpost/internal/vcs"
)

// analysis is the deterministic pipeline's output: design §4.0 through §4.4, with
// no model and no network anywhere in it.
//
// Every command in this binary runs the same pipeline, which is deliberate — a
// command that analysed the repository differently from `build` would report
// something `build` does not produce.
type analysis struct {
	Discovered *discover.Result
	Source     *extract.RunResult
	Manifests  *manifest.RunResult
	History    *vcs.Signals
	Assembled  *assemble.Result
}

func (a *analysis) Graph() *graph.Graph { return a.Assembled.Graph }

// pipelineFlags are the flags every analysing command shares.
type pipelineFlags struct {
	includeVendored bool
	includeFixtures bool
	ignore          stringList
	noHistory       bool
	maxCommits      int
}

func (p *pipelineFlags) register(fs *flag.FlagSet) {
	fs.BoolVar(&p.includeVendored, "include-vendored", false,
		"analyse vendored third-party code instead of only recording it")
	// The escape hatch for a directory named `fixtures` that holds real code. Off by
	// default because the failure it prevents lands on a committed page: a sample project
	// under testdata/ produces modules and dependencies the repository does not have.
	fs.BoolVar(&p.includeFixtures, "include-fixtures", false,
		"analyse sample projects under testdata/ instead of only recording them")
	fs.Var(&p.ignore, "ignore",
		"additional .gitignore-syntax pattern to skip; repeatable")
	// Opt-out rather than opt-in: history is the signal §4.1 calls the cheapest way to
	// find coupling imports do not show, so defaulting it off would mean most bundles
	// never get it. The flag exists for the cases where it is the wrong thing to read —
	// a freshly filtered repository whose history describes code that is no longer there,
	// or a shallow CI checkout that cannot be deepened.
	fs.BoolVar(&p.noHistory, "no-history", false,
		"skip git history, analysing only the files on disk")
	fs.IntVar(&p.maxCommits, "max-commits", vcs.DefaultMaxCommits,
		"how many commits of history to read")
}

// stringList collects a repeatable flag. flag.Value rather than a comma-split
// string, because a gitignore pattern can legitimately contain a comma.
type stringList []string

func (s *stringList) String() string { return fmt.Sprint(*s) }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// analyse walks, extracts, reads manifests, reads history, and assembles the graph.
//
// Each of those five is a span (ADR 0014), which is the reason the stages are written
// out here rather than chained: the span boundaries and the stage boundaries are the same
// thing, and "which stage ate the forty seconds" is the question the instrumentation
// exists to answer. On a run without SIGNPOST_ENABLE_TELEMETRY every telemetry.Stage call
// returns the context it was handed and a no-op span, so the cost is a nil check per
// stage.
//
// The counts attached are the ones that explain a duration — files walked, facts
// extracted, commits read — and nothing that names anything in the repository. See the
// package doc for why Span cannot carry a path even if a later change wanted it to.
func analyse(ctx context.Context, path string, pf pipelineFlags) (*analysis, error) {
	ctx, root := telemetry.Stage(ctx, "analyse")
	defer root.End()

	disc, err := func() (*discover.Result, error) {
		_, span := telemetry.Stage(ctx, "discover")
		defer span.End()
		r, err := discover.Walk(path, discover.Options{
			IncludeVendored: pf.includeVendored,
			IncludeFixtures: pf.includeFixtures,
			ExtraIgnores:    pf.ignore,
		})
		if err != nil {
			span.Failed()
			return nil, err
		}
		span.Count("signpost.files", len(r.Files))
		span.Count("signpost.files_skipped", len(r.Skipped))
		return r, nil
	}()
	if err != nil {
		root.Failed()
		return nil, err
	}

	src := func() *extract.RunResult {
		_, span := telemetry.Stage(ctx, "extract")
		defer span.End()
		r := extract.DefaultRegistry().Run(disc)
		span.Count("signpost.facts", len(r.Facts))
		span.Count("signpost.extract_failures", len(r.Failures))
		return r
	}()

	mans := func() *manifest.RunResult {
		_, span := telemetry.Stage(ctx, "manifests")
		defer span.End()
		r := manifest.DefaultRegistry().Run(disc)
		span.Count("signpost.manifests", len(r.Facts))
		return r
	}()

	var hist *vcs.Signals
	if !pf.noHistory {
		hist, err = func() (*vcs.Signals, error) {
			hctx, span := telemetry.Stage(ctx, "history")
			defer span.End()
			// An error here is a real git fault — vcs reports a missing git, a
			// non-repository, an empty history, and a shallow clone as facts rather than
			// errors, so anything that reaches this branch is worth failing on rather
			// than swallowing.
			s, err := vcs.Read(hctx, path, vcs.Options{MaxCommits: pf.maxCommits})
			if err != nil {
				span.Failed()
				return nil, err
			}
			span.Count("signpost.commits", s.Commits)
			return s, nil
		}()
		if err != nil {
			root.Failed()
			return nil, err
		}
	}

	built, err := func() (*assemble.Result, error) {
		_, span := telemetry.Stage(ctx, "assemble")
		defer span.End()
		r, err := assemble.Build(assemble.Input{
			Discovered: disc,
			Source:     src,
			Manifests:  mans,
			History:    hist,
		})
		if err != nil {
			span.Failed()
			return nil, err
		}
		nodes, edges := r.Graph.Counts()
		span.Count("signpost.nodes", nodes)
		span.Count("signpost.edges", edges)
		span.Count("signpost.unresolved", totalCount(r.Unresolved))
		span.Count("signpost.unlinked", totalCount(r.Unlinked))
		return r, nil
	}()
	if err != nil {
		root.Failed()
		return nil, err
	}
	return &analysis{
		Discovered: disc, Source: src, Manifests: mans,
		History: hist, Assembled: built,
	}, nil
}

// reportCoverage prints what the analysis could not account for.
//
// This goes to stderr on every analysing command and is not optional, for the
// reason design §4.2 gives: the absence of a measurement is never a clean bill of
// health. A user whose repository is 40% Kotlin needs to be told signpost read
// none of it, and a user with 200 unresolved imports needs to know the map has
// holes in it — silence there is the failure mode that makes a structural map
// worse than no map.
// A write failure here is deliberately not returned: the coverage report is a
// diagnostic on stderr, and failing a run whose actual output on stdout was written
// successfully would turn a redirected stderr into a build failure.
func reportCoverage(w io.Writer, a *analysis) {
	p := newPrinter(w)
	nodes, edges := a.Graph().Counts()
	p.printf("analysed %d files: %d nodes, %d edges\n",
		len(a.Discovered.Files), nodes, edges)

	if n := len(a.Discovered.Skipped); n > 0 {
		p.printf("  %d file(s) not read: %s\n", n, topSkips(a.Discovered.Skipped))
	}
	if len(a.Source.Unhandled) > 0 {
		p.printf("  no extractor for: %s\n", unhandledDetail(a))
	}
	// Separate from the line above, because the two are different admissions. "no
	// extractor for .kt" says signpost knows what Kotlin is and cannot read it; this
	// says it could not tell what the file was at all, so no reader was even offered
	// it. Conflating them would hide the second behind the first.
	if u := a.Discovered.Unclassified(); len(u) > 0 {
		p.printf("  %d file(s) of no recognised kind: %s\n", totalCount(u), topCounts(u, 6))
	}
	if n := len(a.Source.Failures); n > 0 {
		p.printf("  %d file(s) failed extraction: %s\n", n, topFailures(a.Source.Failures))
	}
	if n := totalCount(a.Assembled.Unresolved); n > 0 {
		p.printf("  %d import(s) unresolved across %d specifier(s): %s\n",
			n, len(a.Assembled.Unresolved), topCounts(a.Assembled.Unresolved, 5))
	}
	// Its own line, because it is its own fix. The line above names specifiers signpost
	// could not place at all; this names ones it placed inside this repository and found
	// nothing at — so the edge is missing and the map is thinner than the code. A handful
	// is ordinary (generated code, a build-tagged directory, files over the size cap); a
	// lot means a resolution root is missing, which is the shape the tsconfig `paths` gap
	// had: 542 absent edges and no line anywhere admitting it.
	if n := totalCount(a.Assembled.Unlinked); n > 0 {
		p.printf("  %d first-party import(s) reached no page across %d specifier(s): %s\n",
			n, len(a.Assembled.Unlinked), topCounts(a.Assembled.Unlinked, 5))
	}
	reportHistory(p, a)
	// A dropped edge means assemble created an edge to a node it never created,
	// which is a bug in assemble rather than a fact about the repository.
	if a.Assembled.DroppedEdges > 0 {
		p.printf("  warning: %d edge(s) dropped as dangling; please report this\n",
			a.Assembled.DroppedEdges)
	}
}

// reportHistory says what git contributed, and says so even when the answer is nothing.
//
// This is the §4.2 rule applied to the one signal whose absence is easiest to mistake for
// a finding: a bundle built from a shallow clone shows no co-change, and so does a
// repository that genuinely has none. Without a line here the two are indistinguishable,
// and the reader would take the first for the second.
func reportHistory(p *printer, a *analysis) {
	if a.History == nil {
		p.printf("  history not read (-no-history)\n")
		return
	}
	if !a.History.Available {
		p.printf("  history not read: %s\n", a.History.Reason)
		return
	}
	pairs := 0
	for _, e := range a.Graph().Edges() {
		if e.Kind == graph.EdgeCoChanges {
			pairs++
		}
	}
	// Halved because addCoChangeEdges draws each symmetric coupling in both directions.
	p.printf("  history: %d commits, %d co-change pair(s)\n", a.History.Commits, pairs/2)
	if a.History.Reason != "" {
		// Shallow or truncated. Reported as a warning rather than a note: it is the case
		// where the numbers above are real but describe less history than the reader will
		// assume, and the Reason names the fix.
		p.printf("  warning: %s\n", a.History.Reason)
	}
	if n := a.History.SkippedBulkCommits; n > 0 {
		p.printf("  %d commit(s) too broad for co-change (churn still counted)\n", n)
	}
}

func totalCount[K comparable](m map[K]int) int {
	total := 0
	for _, n := range m {
		total += n
	}
	return total
}

// topCounts renders the most frequent keys of a count map, highest count first and
// ties broken by key so the line is stable. Frequency rather than all of them,
// because the useful signal is which few causes dominate: a repository can have
// hundreds of distinct unresolved specifiers coming from the same handful of gaps.
func topCounts(m map[string]int, limit int) string {
	type kv struct {
		k string
		n int
	}
	all := make([]kv, 0, len(m))
	for k, n := range m {
		all = append(all, kv{k, n})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].n != all[j].n {
			return all[i].n > all[j].n
		}
		return all[i].k < all[j].k
	})
	return joinTop(len(all), limit, func(i int) string {
		return fmt.Sprintf("%s (%d)", all[i].k, all[i].n)
	})
}

// unhandledDetail names the languages that had no extractor, expanding
// discover.LangOther into the file extensions behind it.
//
// "other (12)" is not actionable — it does not say whether those twelve files are
// Kotlin, Terraform, or shell, and which of those matters depends entirely on the
// repository. The extension is the closest thing to a language name available
// without a classifier for a language signpost does not support, and naming it is
// what turns the line from an admission into something a user can act on.
func unhandledDetail(a *analysis) string {
	langs := make([]string, 0, len(a.Source.Unhandled))
	for l := range a.Source.Unhandled {
		langs = append(langs, string(l))
	}
	sort.Strings(langs)

	parts := make([]string, 0, len(langs))
	for _, l := range langs {
		n := a.Source.Unhandled[discover.Lang(l)]
		if discover.Lang(l) != discover.LangOther {
			parts = append(parts, fmt.Sprintf("%s (%d)", l, n))
			continue
		}
		if exts := unhandledExtensions(a); exts != "" {
			parts = append(parts, exts)
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%d)", l, n))
	}
	return strings.Join(parts, ", ")
}

// unhandledExtensions counts the extensions of source files classified as
// LangOther, highest count first.
func unhandledExtensions(a *analysis) string {
	if a.Discovered == nil {
		return ""
	}
	byExt := map[string]int{}
	for _, f := range a.Discovered.Files {
		if f.Lang != discover.LangOther {
			continue
		}
		ext := path.Ext(f.Path)
		if ext == "" {
			// An extensionless source file is named by its basename, which is what
			// identifies it: `Dockerfile`, `Justfile`, a shell script with a shebang.
			ext = path.Base(f.Path)
		}
		byExt[ext]++
	}
	if len(byExt) == 0 {
		return ""
	}
	exts := make([]string, 0, len(byExt))
	for e := range byExt {
		exts = append(exts, e)
	}
	sort.Slice(exts, func(i, j int) bool {
		if byExt[exts[i]] != byExt[exts[j]] {
			return byExt[exts[i]] > byExt[exts[j]]
		}
		return exts[i] < exts[j]
	})
	return joinTop(len(exts), 6, func(i int) string {
		return fmt.Sprintf("%s (%d)", exts[i], byExt[exts[i]])
	})
}

func topSkips(skips []discover.Skip) string {
	// Grouped by reason rather than listed by path: "3 files too large" is what a
	// user acts on, and forty paths is what makes them stop reading.
	byReason := map[string]int{}
	for _, s := range skips {
		byReason[s.Reason]++
	}
	keys := make([]string, 0, len(byReason))
	for r := range byReason {
		keys = append(keys, r)
	}
	sort.Strings(keys)
	return joinTop(len(keys), 4, func(i int) string {
		return fmt.Sprintf("%s (%d)", keys[i], byReason[keys[i]])
	})
}

func topFailures(fails []extract.Failure) string {
	return joinTop(len(fails), 3, func(i int) string {
		return fmt.Sprintf("%s (%s)", fails[i].Path, fails[i].Reason)
	})
}

// joinTop renders up to limit items and says how many it left out, so a truncated
// list never reads as a complete one.
func joinTop(total, limit int, item func(int) string) string {
	if total == 0 {
		return ""
	}
	n := total
	if limit > 0 && limit < n {
		n = limit
	}
	parts := make([]string, 0, n+1)
	for i := 0; i < n; i++ {
		parts = append(parts, item(i))
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	if n < total {
		out += fmt.Sprintf(", and %d more", total-n)
	}
	return out
}
