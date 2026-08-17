package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/3rg0n/signpost/internal/graph"
)

// `signpost graph show` reports the structure of a repository to a terminal.
//
// The findings it leads with are the ones design §12 argues are the differentiator:
// import cycles, cross-cluster bridges, hubs, and disconnected components. Those
// are the things a person is wrong about in their own repository, and they are what
// a structural map is for — a listing of every module is something `ls` already
// gives you.
//
// `show` rather than a bare `graph`, because `graph export` belongs beside it and a
// name cannot be both an action and a namespace without being learned twice.
func runGraphShow(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("graph show", flag.ContinueOnError)
	// One writer for the whole of this command's usage, so the prose above and
	// PrintDefaults' flag list cannot land on different streams.
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
		u.printf("usage: signpost graph show [flags] [path]\n")
		u.printf("\nAnalyse a repository and report its structure.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	var pf pipelineFlags
	pf.register(fs)
	top := fs.Int("top", 10, "how many hubs to list")
	// The flag issue #41 asked for. Every finding this command reports is bounded, and
	// before this there was no way to lift any of them: a reader who got `and 35 more`
	// had nowhere to go, because the complete data exists only as raw nodes and edges in
	// `graph export -format json`, from which recovering the bridges means recomputing
	// the clustering. So the findings this tool exists to produce were available in full
	// to nobody.
	//
	// A boolean rather than `-limit N`, because there is no single number to raise: the
	// five bounds are different on purpose, each chosen against what its own line looks
	// like at a terminal, and one number replacing all five would be a worse report at
	// every value. What the other reader wants is not a bigger bound, it is none.
	all := fs.Bool("all", false,
		"list every finding in full, instead of the first few of each; for a reader that greps")
	quiet := fs.Bool("quiet", false, "suppress the coverage report on stderr")
	failOnCycle := fs.Bool("fail-on-cycle", false,
		"exit non-zero if the graph contains an import cycle, for use as a CI gate")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	// Rejected rather than silently ignored, on `view -static`'s precedent: -all means no
	// bound and -top is a bound, so one of the two cannot be honoured, and dropping the
	// one somebody typed leaves them believing they configured something. Checked against
	// set-ness because -top's default carries a real number.
	if *all && setFlags(fs)["top"] {
		return fmt.Errorf("%w: -all lists every hub, so -top has nothing to bound; "+
			"drop one of the two", errUsage)
	}
	path, err := repoPath(fs)
	if err != nil {
		return err
	}
	// -top, -all, and -fail-on-cycle are not configurable and take no part in this: the
	// first two are properties of one reader's invocation, the third decides whether the
	// command fails.
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	applyConfig(fs, cfg, &pf)

	// context.Background() rather than a cancellable one: this process has nothing to
	// cancel from, and the only subprocess in the pipeline carries its own timeout.
	a, err := analyse(context.Background(), path, pf)
	if err != nil {
		return err
	}
	if !*quiet {
		reportCoverage(errOut, a)
	}

	g := a.Graph()
	// Clusters must run before Bridges, which reads the assignment it writes.
	clusters := g.Clusters()
	nodes, edges := g.Counts()

	p := newPrinter(out)
	p.printf("%s\n", a.Discovered.Root)
	p.printf("  %d nodes, %d edges, %d clusters\n", nodes, edges, len(clusters))

	lim := defaultLimits
	if *all {
		lim = noLimits
		// -top governs the hub list and nothing else, so lifting the other four without it
		// would leave one finding bounded under a flag that says every.
		*top = 0
	}
	writeKindCounts(p, g)
	writeHubs(p, g, *top)
	cycles := g.Cycles()
	writeCycles(p, cycles, lim)
	writeBridges(p, g, lim)
	writeComponents(p, g, lim)
	writeOrphans(p, g, lim)
	if err := p.Err(); err != nil {
		return err
	}

	if *failOnCycle && len(cycles) > 0 {
		return fmt.Errorf("%d import cycle(s)", len(cycles))
	}
	return nil
}

// limits holds the bound on each finding `graph show` reports.
//
// A struct rather than five literals at their use sites, which is what they were: the
// literals could not be lifted together, so `-all` had nowhere to write. Named fields
// rather than a slice, because the bounds are not interchangeable — each is chosen against
// what its own line looks like at a terminal, and a positional list would let a reordering
// swap two of them silently.
//
// Zero means no bound, which is joinTop's existing meaning for `limit <= 0` and Hubs'
// meaning for n, so `-all` sets zeroes rather than a sentinel this file would have to
// translate.
type limits struct {
	bridges    int // whole findings listed
	components int
	cycleNames int // concepts named inside one finding
	compNames  int
	orphans    int
}

var (
	// defaultLimits is the report a person reads at a terminal, and the numbers are the
	// ones that were inline before `-all` existed. They differ per finding on purpose: a
	// bridge is one line so eight fit, a component prints a nested list so five do, and
	// the names inside a line are bounded far lower because an eleven-module cycle wraps
	// into a paragraph.
	defaultLimits = limits{bridges: 8, components: 5, cycleNames: 6, compNames: 4, orphans: 10}
	// noLimits is what -all selects. Every field zero, because joinTop and Hubs already
	// read a non-positive bound as none.
	noLimits = limits{}
)

func writeKindCounts(p *printer, g *graph.Graph) {
	byKind := map[graph.Kind]int{}
	for _, n := range g.Nodes() {
		byKind[n.Kind]++
	}
	if len(byKind) == 0 {
		return
	}
	kinds := make([]string, 0, len(byKind))
	for k := range byKind {
		kinds = append(kinds, string(k))
	}
	sort.Strings(kinds)
	p.printf("\nconcepts\n")
	for _, k := range kinds {
		p.printf("  %-22s %d\n", k, byKind[graph.Kind(k)])
	}
}

// writeHubs lists the highest-degree nodes: the places where a wrong assumption
// propagates furthest, which is what an agent should read first.
//
// top <= 0 lists every node that has an edge, which is what -all passes.
func writeHubs(p *printer, g *graph.Graph, top int) {
	// Filtered before the heading is written, not while printing. A node with no edges is
	// not a hub, and counting one produced a heading that disagreed with the list under it:
	// `-top 500` on this repository printed `hubs (top 87 by degree)` above 52 lines, and on
	// a tree with no edges at all it printed a heading with nothing beneath it. Harmless
	// while the default bound was 10 and every repository had ten connected nodes; a header
	// that overstates by 35 is the kind of number a reader quotes.
	hubs := make([]graph.Degree, 0, len(g.Nodes()))
	for _, d := range g.Hubs(0) {
		if d.Total == 0 {
			continue
		}
		hubs = append(hubs, d)
	}
	if len(hubs) == 0 {
		return
	}
	if top > 0 && len(hubs) > top {
		hubs = hubs[:top]
	}
	p.printf("\nhubs (top %d by degree)\n", len(hubs))
	for _, d := range hubs {
		p.printf("  %-42s in %-4d out %-4d\n", d.ID, d.In, d.Out)
	}
}

// writeCycles reports import cycles, which are a real finding rather than a
// stylistic one: two modules in a cycle cannot be understood or changed
// independently, whatever the directory structure suggests.
// Every cycle is listed whatever the bound: a cycle is the finding this command leads with,
// and there are rarely more than a handful. What lim bounds is the modules named inside one.
func writeCycles(p *printer, cycles [][]string, lim limits) {
	if len(cycles) == 0 {
		return
	}
	p.printf("\nimport cycles (%d)\n", len(cycles))
	for _, c := range cycles {
		p.printf("  %d modules: %s\n", len(c),
			joinTop(len(c), lim.cycleNames, func(i int) string { return c[i] }))
	}
}

// writeBridges reports edges crossing a cluster boundary — where a change is most
// likely to surprise, because the two sides are maintained as separate concerns
// and coupled anyway.
func writeBridges(p *printer, g *graph.Graph, lim limits) {
	bridges := g.Bridges()
	if len(bridges) == 0 {
		return
	}
	p.printf("\ncross-cluster edges (%d)\n", len(bridges))
	for i, b := range bridges {
		if lim.bridges > 0 && i == lim.bridges {
			p.printf("  and %d more (-all lists them)\n", len(bridges)-i)
			break
		}
		p.printf("  %s -%s-> %s  (cluster %d -> %d)\n",
			b.From, b.Kind, b.To, b.FromCluster, b.ToCluster)
	}
}

// writeComponents reports islands. More than one component means part of the
// repository has no structural link to the rest — most often docs describing code
// nothing connects them to, which is the drift the semantic pass exists to close.
func writeComponents(p *printer, g *graph.Graph, lim limits) {
	comps := g.Components()
	if len(comps) <= 1 {
		return
	}
	p.printf("\ndisconnected components (%d)\n", len(comps))
	for i, c := range comps {
		if lim.components > 0 && i == lim.components {
			p.printf("  and %d more (-all lists them)\n", len(comps)-i)
			break
		}
		p.printf("  %d nodes: %s\n", len(c),
			joinTop(len(c), lim.compNames, func(j int) string { return c[j] }))
	}
}

// writeOrphans reports nodes with no edges at all: dead code, an unlinked doc, or
// an extractor gap. Which of the three it is needs a human, so the list is
// reported rather than judged.
func writeOrphans(p *printer, g *graph.Graph, lim limits) {
	orphans := g.Orphans()
	if len(orphans) == 0 {
		return
	}
	// One per line under -all, where the default packs them into a comma list. Thirty-five
	// bundle paths on one wrapped line is not something a reader greps, and this is the
	// finding where the difference is largest — the other three already print one fact per
	// line, so only this one had a format that stopped working when the bound came off.
	if lim.orphans <= 0 {
		p.printf("\nunconnected concepts (%d)\n", len(orphans))
		for _, o := range orphans {
			p.printf("  %s\n", o)
		}
		return
	}
	p.printf("\nunconnected concepts (%d)\n  %s\n",
		len(orphans), joinTop(len(orphans), lim.orphans, func(i int) string { return orphans[i] }))
}
