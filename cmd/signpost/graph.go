package main

import (
	"flag"
	"fmt"
	"io"
	"sort"

	"github.com/3rg0n/signpost/internal/graph"
)

// `signpost graph` reports the structure of a repository to a terminal.
//
// The findings it leads with are the ones design §12 argues are the differentiator:
// import cycles, cross-cluster bridges, hubs, and disconnected components. Those
// are the things a person is wrong about in their own repository, and they are what
// a structural map is for — a listing of every module is something `ls` already
// gives you.
func runGraph(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("graph", flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.Usage = func() {
		u := newPrinter(errOut)
		u.printf("usage: signpost graph [flags] [path]\n")
		u.printf("\nAnalyse a repository and report its structure.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	var pf pipelineFlags
	pf.register(fs)
	top := fs.Int("top", 10, "how many hubs to list")
	quiet := fs.Bool("quiet", false, "suppress the coverage report on stderr")
	failOnCycle := fs.Bool("fail-on-cycle", false,
		"exit non-zero if the graph contains an import cycle, for use as a CI gate")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	path, err := repoPath(fs)
	if err != nil {
		return err
	}

	a, err := analyse(path, pf)
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

	writeKindCounts(p, g)
	writeHubs(p, g, *top)
	cycles := g.Cycles()
	writeCycles(p, cycles)
	writeBridges(p, g)
	writeComponents(p, g)
	writeOrphans(p, g)
	if err := p.Err(); err != nil {
		return err
	}

	if *failOnCycle && len(cycles) > 0 {
		return fmt.Errorf("%d import cycle(s)", len(cycles))
	}
	return nil
}

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
func writeHubs(p *printer, g *graph.Graph, top int) {
	hubs := g.Hubs(top)
	if len(hubs) == 0 {
		return
	}
	p.printf("\nhubs (top %d by degree)\n", len(hubs))
	for _, d := range hubs {
		if d.Total == 0 {
			continue
		}
		p.printf("  %-42s in %-4d out %-4d\n", d.ID, d.In, d.Out)
	}
}

// writeCycles reports import cycles, which are a real finding rather than a
// stylistic one: two modules in a cycle cannot be understood or changed
// independently, whatever the directory structure suggests.
func writeCycles(p *printer, cycles [][]string) {
	if len(cycles) == 0 {
		return
	}
	p.printf("\nimport cycles (%d)\n", len(cycles))
	for _, c := range cycles {
		p.printf("  %d modules: %s\n", len(c), joinTop(len(c), 6, func(i int) string { return c[i] }))
	}
}

// writeBridges reports edges crossing a cluster boundary — where a change is most
// likely to surprise, because the two sides are maintained as separate concerns
// and coupled anyway.
func writeBridges(p *printer, g *graph.Graph) {
	bridges := g.Bridges()
	if len(bridges) == 0 {
		return
	}
	p.printf("\ncross-cluster edges (%d)\n", len(bridges))
	for i, b := range bridges {
		if i == 8 {
			p.printf("  and %d more\n", len(bridges)-i)
			break
		}
		p.printf("  %s -%s-> %s  (cluster %d -> %d)\n",
			b.From, b.Kind, b.To, b.FromCluster, b.ToCluster)
	}
}

// writeComponents reports islands. More than one component means part of the
// repository has no structural link to the rest — most often docs describing code
// nothing connects them to, which is the drift the semantic pass exists to close.
func writeComponents(p *printer, g *graph.Graph) {
	comps := g.Components()
	if len(comps) <= 1 {
		return
	}
	p.printf("\ndisconnected components (%d)\n", len(comps))
	for i, c := range comps {
		if i == 5 {
			p.printf("  and %d more\n", len(comps)-i)
			break
		}
		p.printf("  %d nodes: %s\n", len(c), joinTop(len(c), 4, func(j int) string { return c[j] }))
	}
}

// writeOrphans reports nodes with no edges at all: dead code, an unlinked doc, or
// an extractor gap. Which of the three it is needs a human, so the list is
// reported rather than judged.
func writeOrphans(p *printer, g *graph.Graph) {
	orphans := g.Orphans()
	if len(orphans) == 0 {
		return
	}
	p.printf("\nunconnected concepts (%d)\n  %s\n",
		len(orphans), joinTop(len(orphans), 10, func(i int) string { return orphans[i] }))
}
