package view

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestStaticWritesEveryServedAsset is the assertion that makes WriteStatic worth having
// rather than a second definition of the viewer.
//
// It compares against assets() rather than against a list of four filenames, and that is
// the whole point: a fifth asset added to the server and not to the export is a published
// page that 404s one request, which renders as a control that quietly does nothing. A test
// naming the files itself would have to be remembered at the same moment the writer would.
func TestStaticWritesEveryServedAsset(t *testing.T) {
	dir := t.TempDir()
	written, err := WriteStatic(dir, testOptions())
	if err != nil {
		t.Fatal(err)
	}

	served, err := testOptions().assets("")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for route := range served {
		want = append(want, staticName(route))
	}
	sort.Strings(want)
	if len(want) == 0 {
		t.Fatal("assets() served nothing; this test is measuring the wrong thing")
	}

	if strings.Join(written, " ") != strings.Join(want, " ") {
		t.Errorf("WriteStatic reported %v, assets() has %v", written, want)
	}
	for _, name := range want {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(got) == 0 {
			t.Errorf("%s was written empty", name)
		}
	}
}

// TestStaticFilesAreTheServedBytes checks the files are the same bytes, not merely the same
// names. A writer that re-rendered the document, or wrote a placeholder graph, would pass
// the test above.
func TestStaticFilesAreTheServedBytes(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteStatic(dir, testOptions()); err != nil {
		t.Fatal(err)
	}
	served, err := testOptions().assets("")
	if err != nil {
		t.Fatal(err)
	}
	for route, a := range served {
		name := staticName(route)
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(a.body) {
			t.Errorf("%s on disk differs from what %s serves", name, route)
		}
	}
}

// TestStaticDocumentIsIndexHTML pins the one name that is not simply the route.
//
// A static host resolves a directory URL to index.html and nothing else, so a document
// written as anything else publishes a directory listing or a 404 where the page should
// be — and the deploy that did it succeeds.
func TestStaticDocumentIsIndexHTML(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteStatic(dir, testOptions()); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("no index.html: %v", err)
	}
	if !strings.Contains(string(page), "<!doctype html>") {
		t.Error("index.html is not the document")
	}
}

// TestStaticPageNamesNoAddress is why Address is guarded in view.html rather than always
// printed.
//
// A written page has no host to name. The served document prints the address it is
// answering on, which is a fact about a listener that does not exist here — publishing
// `127.0.0.1:7777` on a public URL states something false to every reader.
func TestStaticPageNamesNoAddress(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteStatic(dir, testOptions()); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(page)
	for _, unwanted := range []string{"127.0.0.1", "ctrl-c", `class="local"`} {
		if strings.Contains(body, unwanted) {
			t.Errorf("the written page contains %q, which describes a server it is not", unwanted)
		}
	}
	// And the opposite direction, so this does not pass by rendering nothing at all: the
	// page still has to say what it is.
	if !strings.Contains(body, "view -static") {
		t.Error("the written page does not say what wrote it")
	}
}

// TestStaticPageCarriesTheCSPInTheDocument is the one security property that changes when
// the viewer stops being served by us.
//
// `Serve` sets Content-Security-Policy as a response header, and the header is the copy
// that binds. A static host sends whatever headers it likes, and GitHub Pages sends no CSP
// — so for a published page the meta tag is not a courtesy for anyone saving the file, it
// is the only CSP there is. The page interpolates module names and file paths that came
// out of a repository, which is the injection surface design §7.2 names.
func TestStaticPageCarriesTheCSPInTheDocument(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteStatic(dir, testOptions()); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	meta := regexp.MustCompile(`(?s)http-equiv="Content-Security-Policy".*?content="([^"]+)"`).
		FindStringSubmatch(string(page))
	if meta == nil {
		t.Fatal("the written page has no Content-Security-Policy meta tag; on a static host " +
			"there is no header to fall back on")
	}
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "base-uri 'none'"} {
		if !strings.Contains(meta[1], want) {
			t.Errorf("the page's CSP lacks %q: %s", want, meta[1])
		}
	}
	// unsafe-inline in script-src would let an injected <script> run, which is the whole
	// thing the policy is for.
	if strings.Contains(meta[1], "unsafe-inline") && strings.Contains(meta[1], "script-src") {
		if i := strings.Index(meta[1], "script-src"); i >= 0 {
			rest := meta[1][i:]
			if end := strings.Index(rest, ";"); end >= 0 {
				rest = rest[:end]
			}
			if strings.Contains(rest, "unsafe-inline") {
				t.Errorf("script-src allows unsafe-inline: %s", rest)
			}
		}
	}
}

// TestStaticPageMakesNoOutboundRequest holds for the written page the property
// TestRenderMakesNoOutboundRequest holds for the served one.
//
// The published viewer is the one where a font request would be visible to a third party
// — every reader of the URL, not just the person who ran the command — and site/graph.html
// does fetch webfonts. This document deliberately does not, and a copy-paste from that
// file is how it would acquire them.
func TestStaticPageMakesNoOutboundRequest(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteStatic(dir, testOptions()); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"fonts.googleapis.com", "fonts.gstatic.com", "//"} {
		if host == "//" {
			// A protocol-relative URL is the form that slips past a check for https://.
			// Counted rather than rejected outright: `<!doctype` and comments contain no
			// `//`, but a `src="//host"` does.
			for _, attr := range regexp.MustCompile(`(?:src|href)="([^"]*)"`).
				FindAllStringSubmatch(string(page), -1) {
				if strings.HasPrefix(attr[1], "//") || strings.Contains(attr[1], "://") {
					t.Errorf("the written page fetches %q from somewhere else", attr[1])
				}
			}
			continue
		}
		if strings.Contains(string(page), host) {
			t.Errorf("the written page requests %s", host)
		}
	}
}

// TestStaticWritesIntoAMissingDirectory covers the case the workflow actually hits: a
// deploy names a directory that does not exist yet, because nothing committed one.
func TestStaticWritesIntoAMissingDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "site", "nested")
	if _, err := WriteStatic(dir, testOptions()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		t.Error(err)
	}
}

// TestStaticLeavesUnknownFilesAlone pins the decision not to clean the directory.
//
// A CNAME is the case that matters and the reason this is a test rather than a comment:
// Pages reads the custom domain out of the artifact on every deploy, so an export that
// tidied away a file it did not recognise would clear a configured domain, and the deploy
// that did it would report success.
func TestStaticLeavesUnknownFilesAlone(t *testing.T) {
	dir := t.TempDir()
	keep := filepath.Join(dir, "CNAME")
	if err := os.WriteFile(keep, []byte("example.com\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteStatic(dir, testOptions()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(keep)
	if err != nil {
		t.Fatalf("WriteStatic removed a file it did not write: %v", err)
	}
	if string(got) != "example.com\n" {
		t.Errorf("CNAME says %q now", got)
	}
}

// TestStaticNamesAreSorted checks the reported order is stable. Map iteration is
// randomised, so an unsorted report reorders between runs of the same command — which
// makes the output undiffable and a test of it flaky rather than wrong.
func TestStaticNamesAreSorted(t *testing.T) {
	dir := t.TempDir()
	written, err := WriteStatic(dir, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !sort.StringsAreSorted(written) {
		t.Errorf("WriteStatic reported %v, which is not sorted", written)
	}
}
