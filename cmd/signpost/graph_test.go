package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/graph"
)

// Issue #41: every finding `graph show` reports was bounded by a literal at its use site, and
// no flag lifted any of them. A reader that got `and 35 more` had nowhere to go — the complete
// data exists only as raw nodes and edges in the JSON export, from which recovering the
// bridges means recomputing the clustering — so the findings design §7.1 calls load-bearing
// were available in full to nobody.
//
// These tests go through the writers with a hand-built graph rather than through the binary
// with a fixture repository, and the reason is the bounds themselves: each one needs a
// different shape to exceed (nine bridges, six components, seven names in one cycle), and
// producing those from real source means twelve packages of Go per case whose partition
// depends on what Louvain does with it. The graph is the input the writers actually take.
//
// The default path is asserted alongside every -all assertion. `-all` is worth nothing if it
// works by removing the bound for everyone: the elision is right for a terminal, `joinTop`'s
// comment says it exists so a truncated list never reads as complete, and issue #41 puts
// changing the default out of scope.

// showGraph builds a graph with more of every finding than any default bound admits.
//
// A ring of cliques, which is the one shape that produces all five at once: each clique
// becomes a cluster, so each ring edge is a bridge; the cliques are joined by `imports`, so
// the ring is one cycle spanning every node; and the loose nodes are attached to nothing, so
// they are orphans and single-node components at the same time.
//
// The numbers are chosen above the bounds rather than at them — nine cliques against a bound
// of eight bridges, thirteen components against five — because a test that lands exactly on a
// bound cannot tell `>` from `>=`.
func showGraph(t *testing.T) *graph.Graph {
	t.Helper()
	g := graph.New()
	const cliques, size = 9, 4
	id := func(c, i int) string { return fmt.Sprintf("/modules/c%d-n%d", c, i) }
	for c := 0; c < cliques; c++ {
		for i := 0; i < size; i++ {
			if err := g.AddNode(&graph.Node{ID: id(c, i), Kind: graph.KindModule}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for c := 0; c < cliques; c++ {
		// Dense inside, one edge out. Louvain needs the density difference to draw the
		// boundary: a ring of single nodes collapses into one community and produces no
		// bridges at all.
		for i := 0; i < size; i++ {
			for j := i + 1; j < size; j++ {
				g.AddEdge(graph.Edge{From: id(c, i), To: id(c, j), Kind: graph.EdgeImports})
				g.AddEdge(graph.Edge{From: id(c, j), To: id(c, i), Kind: graph.EdgeImports})
			}
		}
		g.AddEdge(graph.Edge{From: id(c, 0), To: id((c+1)%cliques, 0), Kind: graph.EdgeImports})
	}
	// Twelve orphans, which is above the bound of ten, and each is its own component, which
	// takes the component count above five.
	for i := 0; i < 12; i++ {
		if err := g.AddNode(&graph.Node{
			ID:   fmt.Sprintf("/references/loose-%02d", i),
			Kind: graph.KindDocument,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Clusters before Bridges, which reads the assignment it writes — the same order
	// runGraphShow depends on.
	g.Clusters()
	return g
}

// show renders the findings the way runGraphShow does, at the given limits.
func show(g *graph.Graph, lim limits, top int) string {
	var b bytes.Buffer
	p := newPrinter(&b)
	writeHubs(p, g, top)
	writeCycles(p, g.Cycles(), lim)
	writeBridges(p, g, lim)
	writeComponents(p, g, lim)
	writeOrphans(p, g, lim)
	return b.String()
}

// The bound is what makes the -all assertions mean anything, so it is asserted first. If the
// default stopped truncating, every test below would pass while the report a person reads at
// a terminal had silently become 60 lines of bundle paths.
func TestTheDefaultReportIsStillBounded(t *testing.T) {
	g := showGraph(t)
	out := show(g, defaultLimits, 10)
	// One case per field of limits, because the useful assertion is not that the report
	// truncates somewhere. `strings.Contains(out, "more")` on the whole report passes with any
	// four of the five bounds removed, which is the partial lift TestAllListsEveryFindingInFull
	// is written against in the other direction: a default that stopped bounding orphans would
	// print sixty bundle paths at every terminal and no test here would say so.
	//
	// Located by section as well as by marker, since two of the five print into the same one:
	// `components` elides whole findings and `compNames` elides the names inside one, so the
	// pair is told apart by which line the marker lands on rather than by the marker itself.
	for _, c := range []struct {
		field   string
		section string
		line    []string
	}{
		{"bridges", "cross-cluster edges", []string{"and ", "more (-all lists them)"}},
		{"components", "disconnected components", []string{"and ", "more (-all lists them)"}},
		{"cycleNames", "import cycles", []string{" modules: ", " more"}},
		{"compNames", "disconnected components", []string{" nodes: ", " more"}},
		{"orphans", "unconnected concepts", []string{"/references/", " more"}},
	} {
		if !sectionLineHas(out, c.section, c.line...) {
			t.Errorf("limits.%s does not bound the default report: no line under %q contains "+
				"all of %q, so the elision design §11 wants for a terminal is gone for this "+
				"finding:\n%s", c.field, c.section, c.line, out)
		}
	}
	// The count line and the list under it are separate facts, and the elision must not touch
	// the first. A reader shown five components of thirteen still has to be told there are
	// thirteen: that number is the whole reason to reach for -all, and it is what `and 8 more`
	// is counted against. Taken from the graph rather than written out, because a literal here
	// would be a second claim about the fixture that could drift from the fixture.
	want := fmt.Sprintf("disconnected components (%d)", len(g.Components()))
	if !strings.Contains(out, want) {
		t.Errorf("the truncated report does not state %q, so the count was bounded along "+
			"with the list:\n%s", want, out)
	}
}

// The flag issue #41 asked for, asserted as the absence of every truncation marker rather
// than as the presence of a few more entries. A partial lift — four of the five bounds — reads
// as a fix and leaves exactly the failure the issue describes for whichever finding was
// missed.
func TestAllListsEveryFindingInFull(t *testing.T) {
	g := showGraph(t)
	out := show(g, noLimits, 0)
	for _, marker := range []string{"more (-all lists them)", "and 1 more", " more"} {
		if strings.Contains(out, marker) {
			t.Errorf("-all still truncates (%q):\n%s", marker, out)
		}
	}

	// Each finding is checked against what the graph holds, not against a literal, so an
	// extractor change cannot make this test assert a stale number. The comparison is the
	// point: a report that says 21 and lists 5 is the defect, and only a count taken from
	// the graph catches it.
	for _, f := range []struct {
		name  string
		want  int
		lines func(string) int
	}{
		{"cross-cluster edges", len(g.Bridges()), func(s string) int {
			return countLines(s, "cross-cluster edges", "(cluster ")
		}},
		{"disconnected components", len(g.Components()), func(s string) int {
			return countLines(s, "disconnected components", " nodes: ")
		}},
		{"unconnected concepts", len(g.Orphans()), func(s string) int {
			return countLines(s, "unconnected concepts", "/")
		}},
	} {
		if got := f.lines(out); got != f.want {
			t.Errorf("-all listed %d of %d %s:\n%s", got, f.want, f.name, out)
		}
		if !strings.Contains(out, fmt.Sprintf("%s (%d)", f.name, f.want)) {
			t.Errorf("the %s count is not %d, so the heading and the list disagree:\n%s",
				f.name, f.want, out)
		}
	}

	// The names *inside* a line are a separate bound from the number of lines, and they are
	// the two the issue's table distinguishes. A cycle spanning 36 modules is where the
	// second one shows: eight lines of bridges could all be present while every one of them
	// still said `and 3 more`.
	cycles := g.Cycles()
	if len(cycles) != 1 {
		t.Fatalf("the fixture is meant to hold one ring-spanning cycle, got %d", len(cycles))
	}
	for _, id := range cycles[0] {
		if !strings.Contains(out, id) {
			t.Errorf("-all elided %s from the cycle, so the names inside a line are still "+
				"bounded:\n%s", id, out)
		}
	}
}

// -all lists one orphan per line where the default packs them into a comma list. Asserted
// because it is the difference between a finding a model can grep and one it cannot: 35
// bundle paths wrapped onto a single line is not something `grep` returns usefully, and
// design §11's "greps and reads selectively" is the whole argument for the flag.
func TestAllPrintsOneOrphanPerLine(t *testing.T) {
	g := showGraph(t)
	out := show(g, noLimits, 0)
	section := findSection(out, "unconnected concepts")
	for _, line := range strings.Split(section, "\n") {
		if strings.Count(line, "/references/") > 1 {
			t.Errorf("-all put %d orphans on one line, which is the format the flag exists "+
				"to replace:\n%s", strings.Count(line, "/references/"), line)
		}
	}
	// And the default keeps the comma list, which is what makes the line above a change of
	// format rather than a change of default.
	if !strings.Contains(findSection(show(g, defaultLimits, 10), "unconnected concepts"),
		", /references/") {
		t.Error("the default no longer packs orphans onto one line")
	}
}

// The hub heading counted nodes it did not print. `graph show -top 500` on this repository
// printed `hubs (top 87 by degree)` above 52 lines, because Hubs returns every node and the
// zero-degree ones were skipped inside the print loop — and on a tree with no edges at all it
// printed a heading with nothing under it. Invisible while the bound was 10 and every
// repository had ten connected nodes; -all is what makes it a number somebody reads.
func TestTheHubHeadingCountsWhatItPrints(t *testing.T) {
	g := showGraph(t)
	// Every loose node is a hub candidate with degree 0, so the fixture has 12 of them
	// against 36 real hubs — a heading that counts them overstates by a third.
	for _, top := range []int{0, 5, 1000} {
		out := show(g, noLimits, top)
		heading := findingLineIn(out, "hubs (top ")
		var claimed int
		if _, err := fmt.Sscanf(heading, "hubs (top %d by degree)", &claimed); err != nil {
			t.Fatalf("-top %d: no hub heading in:\n%s", top, out)
		}
		if got := countLines(out, "hubs (top", " in "); got != claimed {
			t.Errorf("-top %d: the heading claims %d hubs and %d lines follow it", top, claimed, got)
		}
	}
}

// A graph with no edges at all: every node is degree 0, so there are no hubs and the section
// must be absent rather than empty. The negative boundary for the test above, and the case a
// repository of documents actually is.
func TestAnEdgelessGraphReportsNoHubSection(t *testing.T) {
	g := graph.New()
	for i := 0; i < 3; i++ {
		if err := g.AddNode(&graph.Node{
			ID:   fmt.Sprintf("/references/doc-%d", i),
			Kind: graph.KindDocument,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if out := show(g, noLimits, 0); strings.Contains(out, "hubs") {
		t.Errorf("a graph with no edges reported a hub section:\n%s", out)
	}
}

// -all and -top are refused together rather than one silently winning, on `view -static`'s
// precedent: dropping the flag somebody typed leaves them believing they configured
// something. Exit 2, because the command line was wrong and re-running it unchanged fails
// the same way — 1 would report it as a broken repository.
func TestAllAndTopTogetherAreRefused(t *testing.T) {
	root := fixture(t)
	_, stderr, code := invoke(t, "graph", "show", "-quiet", "-all", "-top", "5", root)
	if code != 2 {
		t.Errorf("exit = %d, want 2 for a bad command line", code)
	}
	for _, want := range []string{"-all", "-top"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the error does not name %s, so it does not say which two flags "+
				"conflict:\n%s", want, stderr)
		}
	}
	// Either one alone is fine. Without this the assertion above is satisfied by a command
	// that rejects -all outright.
	if _, stderr, code := invoke(t, "graph", "show", "-quiet", "-all", root); code != 0 {
		t.Errorf("-all alone: exit = %d\n%s", code, stderr)
	}
	if _, stderr, code := invoke(t, "graph", "show", "-quiet", "-top", "5", root); code != 0 {
		t.Errorf("-top alone: exit = %d\n%s", code, stderr)
	}
}

// -all is a property of one invocation, so `.signpost.yml` may not set it — the same class as
// -top and -quiet (ADR 0011). Asserted here as well as in internal/config because this is
// where the flag is declared: the refusal is a decision about this flag, and a reader looking
// for why it is not configurable looks at it rather than at the config package.
func TestAllIsRefusedInTheConfigFile(t *testing.T) {
	root := fixture(t)
	writeConfig(t, root, "all: true\n")
	_, stderr, code := invoke(t, "graph", "show", "-quiet", root)
	if code != 2 {
		t.Errorf("exit = %d, want 2: a file setting -all must be refused, not ignored", code)
	}
	// The refusal phrase, and not the unknown-key one. `unknown key "all"` also names the key
	// and also contains "may set" — it lists what the file may set — so the looser check passes
	// with the key simply dropped from `refused`, which is the ignored-with-a-shrug outcome
	// ADR 0011 is written against.
	if !strings.Contains(stderr, "all is not a key this file may set") {
		t.Errorf("the refusal does not say the key is one this file may not set:\n%s", stderr)
	}
	if strings.Contains(stderr, "unknown key") {
		t.Errorf("all fell through to the unknown-key branch, so it is refused without the "+
			"reason ADR 0011 asks for:\n%s", stderr)
	}
}

// The flag has to be findable, and for this flag that is most of the point: the issue's
// sharpest complaint was not that the list was cut but that `and 66 more` was not a prompt to
// go anywhere, because there was nowhere to go. So the truncation names the flag that lifts
// it, and the flag's own help entry says what it does.
func TestTheTruncationNamesTheFlagThatLiftsIt(t *testing.T) {
	out := show(showGraph(t), defaultLimits, 10)
	// Every such line, not one of them. The marker is printed from two writers, so asserting
	// that the report contains `-all` somewhere is satisfied by either of them: dropping the
	// flag name from the bridges line leaves the components line to pass the check, and a
	// reader whose repository has no cross-cluster edges is then told what was withheld and not
	// how to see it.
	//
	// Matched on a line that *starts* with `and `, which is what the two writers print when they
	// stop listing findings. joinTop's elision is appended inside a line that begins with the
	// finding itself, and it deliberately does not carry the flag name: it is shared with the
	// bundle writers, whose output is byte-stable by design §8.1, so its wording is not this
	// command's to change.
	found := 0
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "and ") {
			continue
		}
		found++
		if !strings.Contains(line, "-all") {
			t.Errorf("a truncation does not name the flag that lifts it: %q", strings.TrimSpace(line))
		}
	}
	if found == 0 {
		t.Fatalf("no finding was truncated, so there was nothing to name -all:\n%s", out)
	}

	// Stdout and exit 0: an explicit -h is a request that succeeded, not a usage error.
	usage, stderr, code := invoke(t, "graph", "show", "-h")
	if code != 0 {
		t.Fatalf("-h exit = %d\n%s", code, stderr)
	}
	i := strings.Index(usage, "-all")
	if i < 0 {
		t.Fatalf("-all is not in the flag list:\n%s", usage)
	}
	// The flag's own entry only. Cut at the next entry rather than at a named neighbour, so a
	// reordered flag set cannot widen the slice and satisfy the check from somebody else's
	// description.
	entry := usage[i:]
	if j := strings.Index(entry, "\n  -"); j > 0 {
		entry = entry[:j]
	}
	// "every" is the word that distinguishes this flag from -top. An entry that only said
	// "list findings" would leave a reader guessing whether it raises a bound or removes it.
	if !strings.Contains(entry, "every") {
		t.Errorf("-all's help does not say it lists every finding:\n%s", entry)
	}
}

// countLines counts the lines in the section beginning with the line containing heading that
// contain want. The section ends at the first blank line, which is how every writer here
// separates one finding from the next.
func countLines(out, heading, want string) int {
	n := 0
	for _, line := range strings.Split(findSection(out, heading), "\n") {
		if strings.Contains(line, want) {
			n++
		}
	}
	return n
}

// findSection returns the lines from the one containing heading up to the next blank line,
// exclusive of the heading itself.
func findSection(out, heading string) string {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if !strings.Contains(line, heading) {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimSpace(lines[j]) == "" {
				return strings.Join(lines[i+1:j], "\n")
			}
		}
		return strings.Join(lines[i+1:], "\n")
	}
	return ""
}

// sectionLineHas reports whether any single line of the named section contains all of want.
//
// One line rather than the section, because the two bounds that print into the same section are
// distinguished only by which line the marker lands on.
func sectionLineHas(out, heading string, want ...string) bool {
	for _, line := range strings.Split(findSection(out, heading), "\n") {
		all := true
		for _, w := range want {
			if !strings.Contains(line, w) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

// findingLineIn returns the one line containing want, trimmed.
func findingLineIn(out, want string) string {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, want) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}
