package main

import (
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
	Assembled  *assemble.Result
}

func (a *analysis) Graph() *graph.Graph { return a.Assembled.Graph }

// pipelineFlags are the flags every analysing command shares.
type pipelineFlags struct {
	includeVendored bool
	ignore          stringList
}

func (p *pipelineFlags) register(fs *flag.FlagSet) {
	fs.BoolVar(&p.includeVendored, "include-vendored", false,
		"analyse vendored third-party code instead of only recording it")
	fs.Var(&p.ignore, "ignore",
		"additional .gitignore-syntax pattern to skip; repeatable")
}

// stringList collects a repeatable flag. flag.Value rather than a comma-split
// string, because a gitignore pattern can legitimately contain a comma.
type stringList []string

func (s *stringList) String() string { return fmt.Sprint(*s) }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// analyse walks, extracts, reads manifests, and assembles the graph.
func analyse(path string, pf pipelineFlags) (*analysis, error) {
	disc, err := discover.Walk(path, discover.Options{
		IncludeVendored: pf.includeVendored,
		ExtraIgnores:    pf.ignore,
	})
	if err != nil {
		return nil, err
	}
	src := extract.DefaultRegistry().Run(disc)
	mans := manifest.DefaultRegistry().Run(disc)
	built, err := assemble.Build(assemble.Input{
		Discovered: disc,
		Source:     src,
		Manifests:  mans,
	})
	if err != nil {
		return nil, err
	}
	return &analysis{Discovered: disc, Source: src, Manifests: mans, Assembled: built}, nil
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
	if n := len(a.Source.Failures); n > 0 {
		p.printf("  %d file(s) failed extraction: %s\n", n, topFailures(a.Source.Failures))
	}
	if n := totalCount(a.Assembled.Unresolved); n > 0 {
		p.printf("  %d import(s) unresolved across %d specifier(s): %s\n",
			n, len(a.Assembled.Unresolved), topUnresolved(a.Assembled.Unresolved, 5))
	}
	// A dropped edge means assemble created an edge to a node it never created,
	// which is a bug in assemble rather than a fact about the repository.
	if a.Assembled.DroppedEdges > 0 {
		p.printf("  warning: %d edge(s) dropped as dangling; please report this\n",
			a.Assembled.DroppedEdges)
	}
}

func totalCount[K comparable](m map[K]int) int {
	total := 0
	for _, n := range m {
		total += n
	}
	return total
}

// topUnresolved returns the most frequent unresolved specifiers, highest count
// first. Frequency rather than all of them, because the useful signal is which
// dependency the map is missing, and a repository can have hundreds of distinct
// specifiers resolving to the same handful of causes.
func topUnresolved(m map[string]int, limit int) string {
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
