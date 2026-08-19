package site

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The landing page's claims about this tool, checked against the tree.
//
// # Why this exists
//
// site/index.html states twice over what signpost can do: a status table of 25 components, and
// a pasted run of the tool on this repository. Nothing in Go reads either file, no CI job
// parses them, and both went wrong within two days of each other. The table listed the
// semantic pass at v0.2 for eight rows after the README and the 0.1.0 changelog entry recorded
// it as shipped (#63). The pasted run reported a repository of 166 files, 49 nodes, and 87
// edges, against a tree of 258, 90, and 224, and the sentence beside it counted two coverage
// gaps where the run printed four (#64).
//
// Both are the failure this tool exists to prevent, in the one committed artifact here that
// signpost does not check: a document making a claim about a tree, with no gate that compares
// the two. `verify` does this for the bundle. This does it for the landing page.
//
// # What is gated, and what is deliberately left free
//
// The verdict, not the wording. The two tables must have the same rows in the same order and
// must agree on each row's *state* — done, v0.3, declined. Row labels are not compared,
// because the site deliberately words them differently: `Export: Mermaid, DOT, GraphML, JSON`
// against the README's `Mermaid / DOT / GraphML / JSON export`, and five more like it. A gate
// on label text would make the README's prose dictate the landing page's copy, and label
// wording was never what went wrong.
//
// The state cell is compared as the verdict alone, discarding whatever follows the em dash:
// the README cites ADR 0035 on the declined row and the site does not, which is a difference
// between a reference document and a landing page rather than a contradiction.
//
// For the pasted run, only its internal consistency. Nothing here re-runs `graph show` to
// compare the numbers, for the reason corpus_test.go gives about count assertions: a check
// that fails whenever an extractor improves trains people to update the number instead of
// reading the diff. The figure's caption carries the date it was pasted, so a number in it is
// a fact about a day. What is checked is that the paste does not contradict itself — the same
// node and edge counts in its header and its summary, a hub heading that counts the hub rows
// under it, and a note whose "four dim lines" counts the dim lines. Those hold across every
// improvement to the tool and break only when somebody edits the paste by hand, which is the
// one dishonesty a dated paste is still open to.
//
// # Why this test lives here
//
// This package embeds the viewer's files so that `view` serves the published page rather than a
// fork of it. index.html is the one file in this directory it does not embed — the landing page
// has no reader in Go — so the package owns the file without depending on it, which is the right
// place for a test that reads it. go test runs in the package directory, so the paths below are
// relative to site/.

const (
	readmePath = "../README.md"
	sitePath   = "index.html"
)

// statusRow is one row of either table. class is the site's <tr> class and is empty for a
// README row.
type statusRow struct {
	label string
	state string
	class string
}

var (
	htmlTagRe   = regexp.MustCompile(`<[^>]*>`)
	mdLinkRe    = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	siteRowRe   = regexp.MustCompile(`(?s)<tr class="([^"]*)">(.*?)</tr>`)
	siteHeadRe  = regexp.MustCompile(`(?s)<th scope="row">(.*?)</th>`)
	siteCellRe  = regexp.MustCompile(`(?s)<td>(.*?)</td>`)
	sepRowRe    = regexp.MustCompile(`^\|[\s\-:|]+\|$`)
	analysedRe  = regexp.MustCompile(`analysed (\d+) files: (\d+) nodes, (\d+) edges`)
	summaryRe   = regexp.MustCompile(`(\d+) nodes, (\d+) edges, (\d+) clusters`)
	hubHeadRe   = regexp.MustCompile(`hubs \(top (\d+) by degree\)`)
	hubRowRe    = regexp.MustCompile(`^\s+\S+\s+in \d+\s+out \d+$`)
	dimLinesRe  = regexp.MustCompile(`The (\w+) dim lines are the gaps`)
	capDateRe   = regexp.MustCompile(`signpost, run on its own repository · (\d{4}-\d{2}-\d{2})`)
	numberWords = map[string]int{"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8}
)

// siteDoc reads a repository document with CRLF folded to LF. .gitattributes pins eol=lf, so the
// fold matters only for a checkout that ignores it — which is what a contributor gets from a
// clone with a global core.autocrlf, and the line-ending bug the corpus harness records was
// found that way.
func siteDoc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// flatten strips markup and link syntax and collapses whitespace, so a cell written across
// three wrapped lines of HTML compares equal to the same text on one line of markdown.
func flatten(s string) string {
	s = mdLinkRe.ReplaceAllString(s, "$1")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "`", "")
	return strings.Join(strings.Fields(s), " ")
}

// verdict is the state cell reduced to the word it decides — everything before the em dash
// that introduces an explanation.
func verdict(cell string) string {
	s := flatten(cell)
	if i := strings.Index(s, " — "); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// readmeStatusRows parses the pipe table under `## Status`.
func readmeStatusRows(t *testing.T) []statusRow {
	t.Helper()
	body := siteDoc(t, readmePath)
	at := strings.Index(body, "\n## Status\n")
	if at < 0 {
		t.Fatalf("%s has no `## Status` heading; this test cannot find the table", readmePath)
	}

	var lines []string
	for _, line := range strings.Split(body[at+1:], "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "|") {
			lines = append(lines, trimmed)
			continue
		}
		if len(lines) > 0 {
			break
		}
	}
	// Three is the header, the separator, and one row: fewer means the scan above found
	// something that is not the table, and reporting no rows would pass every assertion below.
	if len(lines) < 3 {
		t.Fatalf("%s: found %d table line(s) under `## Status`, want at least 3", readmePath, len(lines))
	}
	if !sepRowRe.MatchString(lines[1]) {
		t.Fatalf("%s: second table line is %q, want a |---|---| separator", readmePath, lines[1])
	}

	var rows []statusRow
	for _, line := range lines[2:] {
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 2 {
			t.Fatalf("%s: row %q split into %d cell(s), want 2; a `|` inside a cell needs escaping", readmePath, line, len(cells))
		}
		rows = append(rows, statusRow{label: flatten(cells[0]), state: cells[1]})
	}
	return rows
}

// siteStatusRows parses the <tbody> of <table class="status">.
func siteStatusRows(t *testing.T) []statusRow {
	t.Helper()
	body := siteDoc(t, sitePath)
	tbody := siteBetween(t, body, `<table class="status">`, `</table>`)
	tbody = siteBetween(t, tbody, "<tbody>", "</tbody>")

	var rows []statusRow
	for _, m := range siteRowRe.FindAllStringSubmatch(tbody, -1) {
		class, inner := m[1], m[2]
		head := siteHeadRe.FindStringSubmatch(inner)
		cell := siteCellRe.FindStringSubmatch(inner)
		if head == nil || cell == nil {
			t.Fatalf("%s: a status row has no <th scope=\"row\"> or no <td>: %q", sitePath, strings.Join(strings.Fields(inner), " "))
		}
		rows = append(rows, statusRow{label: flatten(head[1]), state: cell[1], class: class})
	}
	if len(rows) == 0 {
		t.Fatalf("%s: parsed no rows from the status table", sitePath)
	}
	return rows
}

// siteBetween returns the text bounded by open and close, failing if either is absent. A missing
// marker means the page was restructured, and every count taken from the result would be zero.
func siteBetween(t *testing.T, body, open, close string) string {
	t.Helper()
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatalf("%s: no %q", sitePath, open)
	}
	rest := body[start+len(open):]
	end := strings.Index(rest, close)
	if end < 0 {
		t.Fatalf("%s: %q is never closed by %q", sitePath, open, close)
	}
	return rest[:end]
}

func TestSiteStatusTableAgreesWithREADME(t *testing.T) {
	want := readmeStatusRows(t)
	got := siteStatusRows(t)

	if len(got) != len(want) {
		t.Fatalf("status table rows: %s has %d, %s has %d", sitePath, len(got), readmePath, len(want))
	}
	for i := range want {
		wantVerdict, gotVerdict := verdict(want[i].state), verdict(got[i].state)
		if wantVerdict == gotVerdict {
			continue
		}
		t.Errorf("row %d states a different verdict:\n  %s: %q — %s\n  %s: %q — %s",
			i+1, readmePath, want[i].label, wantVerdict, sitePath, got[i].label, gotVerdict)
	}
}

// The row class draws the row: a solid rule for a shipped component, dashed for one that has
// not shipped, as the table's own caption tells the reader. A row whose class disagrees with
// its state renders a lie that no text on the page contradicts.
func TestSiteStatusRowClassMatchesItsState(t *testing.T) {
	for i, row := range siteStatusRows(t) {
		want := "is-open"
		if verdict(row.state) == "done" {
			want = "is-done"
		}
		if row.class != want {
			t.Errorf("row %d (%q) states %q and is drawn %q, want %q", i+1, row.label, verdict(row.state), row.class, want)
		}
	}
}

func TestSiteHeroRunDoesNotContradictItself(t *testing.T) {
	body := siteDoc(t, sitePath)
	run := siteBetween(t, body, `<pre class="term__body">`, `</pre>`)

	caption := siteBetween(t, body, `<figcaption class="term__cap">`, `</figcaption>`)
	if !capDateRe.MatchString(flatten(caption)) {
		t.Errorf("the figure's caption is %q; it must date the run, because that is what makes a number in it a fact about a day", flatten(caption))
	}

	// The tool prints the node and edge counts twice, once beside the file count and once
	// beside the cluster count. A hand-edited paste updates one of them.
	head := analysedRe.FindStringSubmatch(run)
	sum := summaryRe.FindStringSubmatch(run)
	if head == nil || sum == nil {
		t.Fatalf("the pasted run has no `analysed N files:` line (%v) or no `N nodes, M edges, K clusters` line (%v)", head != nil, sum != nil)
	}
	if head[2] != sum[1] || head[3] != sum[2] {
		t.Errorf("the paste reports %s nodes, %s edges at the top and %s nodes, %s edges in its summary", head[2], head[3], sum[1], sum[2])
	}

	// The note names how many coverage lines the reader is looking at. It said two while the
	// paste showed four.
	note := flatten(siteBetween(t, body, `<p class="term__note">`, `</p>`))
	word := dimLinesRe.FindStringSubmatch(note)
	if word == nil {
		t.Fatalf("the note beside the figure does not say how many dim lines are the gaps: %q", note)
	}
	claimed, ok := numberWords[word[1]]
	if !ok {
		t.Fatalf("the note counts the gaps as %q, which is not a number this test can read; spell it as a word", word[1])
	}
	if dim := strings.Count(run, `<span class="u">`); dim != claimed {
		t.Errorf("the note says %s dim line(s) are the gaps; the paste marks %d", word[1], dim)
	}

	// The hub heading states its own bound, and the tool prints exactly that many rows.
	hub := hubHeadRe.FindStringSubmatch(run)
	if hub == nil {
		t.Fatalf("the pasted run has no `hubs (top N by degree)` heading")
	}
	var hubs int
	for _, line := range strings.Split(run[strings.Index(run, hub[0])+len(hub[0]):], "\n") {
		if hubRowRe.MatchString(line) {
			hubs++
		}
	}
	bound, err := strconv.Atoi(hub[1])
	if err != nil {
		t.Fatalf("the hub heading bound %q is not a number: %v", hub[1], err)
	}
	if bound != hubs {
		t.Errorf("the hub heading says top %d; %d hub row(s) follow it", bound, hubs)
	}
}
