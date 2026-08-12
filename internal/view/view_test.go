package view

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/3rg0n/signpost/site"
)

// testOptions is a filled Options, so a test that cares about one field does not have to
// name the rest. Every string carries something a broken escape or a dropped field would
// make visible in the rendered page.
func testOptions() Options {
	return Options{
		Root:     "/work/repo",
		Title:    "github.com/org/repo",
		Commit:   "abcdef012345",
		RepoBase: "https://github.com/org/repo/blob/abcdef012345/",
		// A node with content, not `{"nodes":[],"edges":[]}`. An empty graph is exactly what
		// a placeholder looks like, so a fixture carrying one cannot distinguish "served the
		// graph" from "served a stand-in" — which is how a mutant replacing graph.json with
		// an empty document survived. The path is here because it is the kind of string that
		// must reach the page verbatim.
		Graph: []byte(`{"nodes":[{"id":"/modules/auth","files":["internal/auth/auth.go"]}],"edges":[]}`),
		Nodes: 53,
		Edges: 94,
		Notes: []string{"The bundle is behind by 2 commit(s)."},
	}
}

// hook matches the data-* attribute names graph.js queries by.
//
// Deliberately not an HTML parser: the contract is textual — graph.js calls
// querySelector("[data-plot]") — so what has to be compared is the set of attribute
// names present in each document, and a regexp over the source finds exactly that.
var hook = regexp.MustCompile(`data-[a-z0-9-]+`)

func hooks(t *testing.T, src string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	for _, m := range hook.FindAllString(src, -1) {
		found[m] = true
	}
	return found
}

// TestViewMarkupMatchesPublishedViewer is the reason site/graph.html is embedded.
//
// graph.js is one file serving two documents — the published page and the one this
// package renders — and it finds everything it drives by attribute name. A hook renamed
// or dropped in one document is not a compile error and not a runtime error: the control
// silently does nothing, in the half of the pair nobody was looking at. This is the only
// thing that notices.
func TestViewMarkupMatchesPublishedViewer(t *testing.T) {
	published, err := site.Files.ReadFile("graph.html")
	if err != nil {
		t.Fatal(err)
	}
	served, err := docFS.ReadFile("view.html")
	if err != nil {
		t.Fatal(err)
	}
	want, got := hooks(t, string(published)), hooks(t, string(served))
	if len(want) == 0 {
		t.Fatal("no data-* hooks found in site/graph.html; the regexp or the file changed shape")
	}

	var missing []string
	for h := range want {
		// data-repo-base is on the root element of both documents but is not a control:
		// graph.html hardcodes it and view.html interpolates it, and view.html omits the
		// attribute entirely when there is no base. Compared by behaviour in
		// TestRenderOmitsRepoBaseWhenEmpty instead.
		if h == "data-repo-base" {
			continue
		}
		if !got[h] {
			missing = append(missing, h)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("internal/view/view.html is missing hooks graph.js drives: %s",
			strings.Join(missing, " "))
	}
}

func TestRenderStatesWhatWasRead(t *testing.T) {
	page, err := testOptions().render("127.0.0.1:7777")
	if err != nil {
		t.Fatal(err)
	}
	body := string(page)
	for _, want := range []string{
		"github.com/org/repo",
		"/work/repo",
		"abcdef012345",
		"53",
		"94",
		"The bundle is behind by 2 commit(s).",
		`data-repo-base="https://github.com/org/repo/blob/abcdef012345/"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered page is missing %q", want)
		}
	}
}

// TestRenderMakesNoOutboundRequest is the offline requirement, checked rather than
// asserted. A local tool that fetches a webfont tells a third party which repositories
// somebody opens, and on a machine with no route it renders in a fallback face after a
// timeout.
func TestRenderMakesNoOutboundRequest(t *testing.T) {
	page, err := testOptions().render("127.0.0.1:7777")
	if err != nil {
		t.Fatal(err)
	}
	body := string(page)
	// data-repo-base is an href a reader clicks, not a subresource, and it is the one
	// external URL the document is allowed to name.
	body = strings.ReplaceAll(body, testOptions().RepoBase, "")
	for _, bad := range []string{"http://", "https://", "//fonts."} {
		if strings.Contains(body, bad) {
			t.Errorf("the served page names an external URL (%q); it must load nothing off this machine:\n%s",
				bad, body)
		}
	}
}

// TestRenderOmitsRepoBaseWhenEmpty pins the negative half of the link behaviour: no
// attribute at all rather than an empty one. graph.js treats a missing attribute and an
// empty string alike, so this is about the document — an empty data-repo-base="" reads
// like a bug in the caller rather than the supported case it is.
func TestRenderOmitsRepoBaseWhenEmpty(t *testing.T) {
	o := testOptions()
	o.RepoBase = ""
	page, err := o.render("127.0.0.1:7777")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(page), "data-repo-base") {
		t.Errorf("empty RepoBase still emitted the attribute:\n%s", page)
	}
}

// TestRenderEscapesRepositoryStrings is the vulnerability class this document has. Every
// interpolated value came out of a repository — a module path, a directory name — and a
// repository is allowed to contain a directory called `<script>`.
func TestRenderEscapesRepositoryStrings(t *testing.T) {
	o := testOptions()
	o.Title = `<script>alert(1)</script>`
	o.Root = `/work/"><img onerror=alert(1) src=x>`
	o.Notes = []string{`<script>alert(2)</script>`}
	page, err := o.render("127.0.0.1:7777")
	if err != nil {
		t.Fatal(err)
	}
	body := string(page)
	for _, bad := range []string{"<script>alert(1)", "<script>alert(2)", "<img onerror"} {
		if strings.Contains(body, bad) {
			t.Errorf("%q reached the page unescaped:\n%s", bad, body)
		}
	}
	// Escaped and still present: the point is that the reader sees the real directory
	// name, not that the value is dropped.
	if !strings.Contains(body, "&lt;script&gt;alert(1)") {
		t.Errorf("the title was not rendered at all; expected it escaped:\n%s", body)
	}
}

func TestAssetsAreTypedLiterally(t *testing.T) {
	files, err := testOptions().assets("127.0.0.1:7777")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"/":            "text/html; charset=utf-8",
		"/graph.json":  "application/json; charset=utf-8",
		"/graph.js":    "text/javascript; charset=utf-8",
		"/style.css":   "text/css; charset=utf-8",
		"/favicon.svg": "image/svg+xml",
	}
	if len(files) != len(want) {
		t.Errorf("assets = %d entries, want %d; a new route needs a test", len(files), len(want))
	}
	for path, ctype := range want {
		a, ok := files[path]
		if !ok {
			t.Errorf("no asset at %s", path)
			continue
		}
		if a.ctype != ctype {
			t.Errorf("%s: content type = %q, want %q", path, a.ctype, ctype)
		}
		if len(a.body) == 0 {
			t.Errorf("%s: empty body", path)
		}
	}
}

// TestGraphIsServedVerbatim: the JSON is not parsed, re-encoded, or validated anywhere in
// this package, and the page fetches exactly what `graph export -format json` wrote.
func TestGraphIsServedVerbatim(t *testing.T) {
	o := testOptions()
	files, err := o.assets("127.0.0.1:7777")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(files["/graph.json"].body); got != string(o.Graph) {
		t.Errorf("graph.json = %q, want %q", got, o.Graph)
	}
	var doc struct {
		Nodes []json.RawMessage `json:"nodes"`
		Edges []json.RawMessage `json:"edges"`
	}
	if err := json.Unmarshal(files["/graph.json"].body, &doc); err != nil {
		t.Fatalf("the served graph is not the shape graph.js reads: %v", err)
	}
}

// serve starts the handler on a test server and returns a request helper. httptest gives
// a 127.0.0.1 listener, which is what makes the Host cases below realistic.
func serve(t *testing.T) *httptest.Server {
	t.Helper()
	files, err := testOptions().assets("127.0.0.1:7777")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(handler(files))
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, srv *httptest.Server, method, path, host string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestHandlerServesTheAssetSet(t *testing.T) {
	srv := serve(t)
	for _, path := range []string{"/", "/graph.json", "/graph.js", "/style.css", "/favicon.svg"} {
		resp := do(t, srv, http.MethodGet, path, "")
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, resp.StatusCode)
		}
	}
}

// TestHandlerRefusesNonLoopbackHost is the DNS-rebinding mitigation. An attacker's page
// cannot change the Host header a browser sends, so a request arriving with somebody
// else's hostname is a request whose response would be same-origin with their page.
func TestHandlerRefusesNonLoopbackHost(t *testing.T) {
	srv := serve(t)
	for _, host := range []string{
		"evil.example.com",
		"evil.example.com:7777",
		// A rebinding name that merely looks local, and a raw non-loopback address.
		"localhost.evil.example.com",
		"192.168.1.10:7777",
		"10.0.0.1",
	} {
		resp := do(t, srv, http.MethodGet, "/", host)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Host %q = %d, want 403", host, resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		// The refusal must not carry what the page would have said. This is the reason
		// checkHost runs before the method check and before the map lookup.
		for _, leak := range []string{"/work/repo", "github.com/org/repo", "abcdef012345"} {
			if strings.Contains(string(body), leak) {
				t.Errorf("the 403 for Host %q disclosed %q:\n%s", host, leak, body)
			}
		}
	}
}

func TestHandlerAcceptsLoopbackHosts(t *testing.T) {
	srv := serve(t)
	for _, host := range []string{
		"127.0.0.1:7777",
		"localhost:7777",
		"LOCALHOST:7777",
		// No port: valid, and the scheme's default applies.
		"localhost",
		"127.0.0.1",
		// Anywhere in 127/8 is loopback, not just .0.1.
		"127.9.9.9:7777",
		// Bracketed IPv6, which is how a browser sends it.
		"[::1]:7777",
	} {
		resp := do(t, srv, http.MethodGet, "/", host)
		if resp.StatusCode != http.StatusOK {
			t.Errorf("Host %q = %d, want 200", host, resp.StatusCode)
		}
	}
}

func TestHandlerRefusesWrites(t *testing.T) {
	srv := serve(t)
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		resp := do(t, srv, method, "/", "")
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s / = %d, want 405", method, resp.StatusCode)
		}
		if got := resp.Header.Get("Allow"); got != "GET, HEAD" {
			t.Errorf("%s /: Allow = %q, want %q", method, got, "GET, HEAD")
		}
	}
}

func TestHandlerHeadHasNoBody(t *testing.T) {
	srv := serve(t)
	resp := do(t, srv, http.MethodHead, "/", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD / = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Errorf("HEAD / returned %d bytes of body", len(body))
	}
	if resp.ContentLength <= 0 {
		t.Errorf("HEAD /: Content-Length = %d, want the length of the page", resp.ContentLength)
	}
}

// TestHandlerServesNothingOffDisk is the negative boundary on the routing map: this
// server has no document root, so a path that is not one of the five names is a 404 and
// never a file. Traversal is the case that would matter if it did.
func TestHandlerServesNothingOffDisk(t *testing.T) {
	srv := serve(t)
	for _, path := range []string{
		"/go.mod",
		"/../go.mod",
		"/%2e%2e/go.mod",
		"/index.html",
		"/graph.html",
		"/view.html",
		"/graph.js/",
		"/GRAPH.JS",
		"/.signpost/index.md",
	} {
		resp := do(t, srv, http.MethodGet, path, "")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
	}
}

// TestHandlerSetsHeadersOnErrorsToo: a 404 body is still a document the browser renders,
// and a cached 200 would outlive the snapshot it describes.
func TestHandlerSetsHeadersOnErrorsToo(t *testing.T) {
	srv := serve(t)
	cases := []struct {
		what           string
		method, path   string
		host           string
		wantStatusCode int
	}{
		{"ok", http.MethodGet, "/", "", http.StatusOK},
		{"not found", http.MethodGet, "/nope", "", http.StatusNotFound},
		{"forbidden", http.MethodGet, "/", "evil.example.com", http.StatusForbidden},
		{"wrong method", http.MethodPost, "/", "", http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		resp := do(t, srv, c.method, c.path, c.host)
		if resp.StatusCode != c.wantStatusCode {
			t.Errorf("%s: status = %d, want %d", c.what, resp.StatusCode, c.wantStatusCode)
		}
		csp := resp.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "unsafe-inline") {
			t.Errorf("%s: CSP = %q", c.what, csp)
		}
		if strings.Contains(csp, "fonts.googleapis.com") || strings.Contains(csp, "fonts.gstatic.com") {
			t.Errorf("%s: the served CSP allows a webfont origin; this document is offline: %q", c.what, csp)
		}
		if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s: X-Content-Type-Options = %q", c.what, got)
		}
		if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s: Referrer-Policy = %q", c.what, got)
		}
		if got := resp.Header.Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q", c.what, got)
		}
	}
}

func TestCheckHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1:7777", true},
		{"127.0.0.1", true},
		{"127.255.255.254", true},
		{"localhost:7777", true},
		{"LocalHost", true},
		{"[::1]:7777", true},
		{"::1", true},
		// The negatives, which are the half that matters. Each is something that reaches a
		// loopback listener in the real world: a rebinding name, a name that contains
		// "localhost" as a label, the LAN address of this machine, and the empty Host an
		// HTTP/1.0 client sends.
		{"", false},
		{"evil.example.com", false},
		{"evil.example.com:7777", false},
		{"localhost.evil.example.com", false},
		{"notlocalhost", false},
		{"localhostx:7777", false},
		{"192.168.1.10:7777", false},
		{"10.0.0.1", false},
		{"0.0.0.0", false},
		// Not loopback: 127.0.0.1 as a label of somebody else's domain.
		{"127.0.0.1.evil.example.com", false},
	}
	for _, c := range cases {
		if got := checkHost(c.host); got != c.want {
			t.Errorf("checkHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestCheckURL(t *testing.T) {
	ok := []string{
		"http://127.0.0.1:7777/",
		"http://localhost:7777/",
		"http://[::1]:7777/",
	}
	for _, url := range ok {
		if err := checkURL(url); err != nil {
			t.Errorf("checkURL(%q) = %v, want nil", url, err)
		}
	}
	// The negatives are the whole point: this string becomes an argument to rundll32 or
	// to the xdg-open shell script, both of which act on what it says.
	bad := []string{
		"",
		"https://127.0.0.1:7777/",
		"file:///c:/windows/system32/calc.exe",
		"http://evil.example.com/",
		"http://127.0.0.1:7777",
		"http://127.0.0.1:7777/graph.json",
		"http://127.0.0.1:7777/?x=1",
		"http://127.0.0.1:7777/#x",
		// Shell metacharacters, which is what the character check is for.
		"http://127.0.0.1:7777;calc/",
		"http://127.0.0.1:7777&calc/",
		"http://127.0.0.1:7777|calc/",
		"http://127.0.0.1:7777 calc/",
		"http://127.0.0.1:7777$(calc)/",
		"http://127.0.0.1:7777`calc`/",
		"http://127.0.0.1:7777\ncalc/",
		// A credential-bearing authority, which is not something Serve ever builds.
		"http://user:pass@127.0.0.1:7777/",
	}
	for _, url := range bad {
		if err := checkURL(url); err == nil {
			t.Errorf("checkURL(%q) = nil, want an error", url)
		}
	}
}

func TestOpenerNamesALauncher(t *testing.T) {
	name, args := opener("http://127.0.0.1:7777/")
	if name == "" {
		t.Fatalf("no launcher for this platform")
	}
	if len(args) == 0 || args[len(args)-1] != "http://127.0.0.1:7777/" {
		t.Errorf("opener args = %q; the URL must be the last argument", args)
	}
	for _, a := range args {
		// No shell, on any platform: `cmd /c start` parses its argument and splits on &.
		if strings.Contains(a, "cmd") && strings.Contains(strings.Join(args, " "), "start") {
			t.Errorf("the launcher goes through cmd start: %q", args)
		}
	}
}

// TestListenBindsLoopbackOnly: the address is a literal in this package for a reason, and
// this is what says so. A listener on 0.0.0.0 would serve a private repository's
// structure to the network the machine is on.
func TestListenBindsLoopbackOnly(t *testing.T) {
	ln, addr, err := listen(0, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" {
		t.Errorf("listen bound %q, want 127.0.0.1", host)
	}
	if port == "0" || port == "" {
		t.Errorf("listen reported port %q; the resolved port is what the URL needs", port)
	}
}

// TestListenAskedPortCollisionIsAnError is the regression test for the bug this feature
// shipped with: `-port 7777` on a taken port fell back to a free one and printed a
// different URL, because listen compared the value against DefaultPort instead of asking
// whether the flag was passed. Both directions are asserted — an error when asked, a
// fallback when not — since a fix that made every collision fatal would break the
// default and pass a one-sided test.
func TestListenAskedPortCollisionIsAnError(t *testing.T) {
	held, addr, err := listen(0, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatal(err)
	}

	if _, _, err := listen(port, true); err == nil {
		t.Errorf("listen(%d, asked) succeeded on a taken port; an explicitly named port must be an error", port)
	}

	ln, got, err := listen(port, false)
	if err != nil {
		t.Fatalf("listen(%d, unasked) on a taken port = %v, want a fallback", port, err)
	}
	defer func() { _ = ln.Close() }()
	if got == addr {
		t.Errorf("the fallback returned the held address %q", got)
	}
	if h, _, err := net.SplitHostPort(got); err != nil || h != "127.0.0.1" {
		t.Errorf("the fallback bound %q, want 127.0.0.1", got)
	}
}

// syncBuffer is a buffer a test can read while Serve is still writing to it.
//
// The only test here that needs one is the cancel test below, and it needs one because
// Serve runs in another goroutine there: it writes its banner while the test body polls
// for it, which is a data race on a plain strings.Builder even though the poll only ever
// reads. It went in without this and passed on two of the three platforms — the race
// detector, not the behaviour, is what tells the difference, so the failure was a red
// -race job on Linux and nothing at all elsewhere.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestServeStopsOnContextCancel: the ordering in Serve is what makes the command usable
// headless — the URL is printed before the browser opens and before anything is served —
// and ctrl-c has to end it. A regression here is a command that appears to hang.
func TestServeStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, testOptions(), &out, false) }()

	// The listener is open by the time the URL is printed, so the first line appearing is
	// the signal that the server is up.
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), "stop it with ctrl-c") {
		if time.Now().After(deadline) {
			t.Fatalf("Serve did not print its banner:\n%s", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve = %v, want nil on cancel", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Serve did not return after the context was cancelled")
	}
	banner := out.String()
	if !strings.Contains(banner, "serving github.com/org/repo on http://127.0.0.1:") {
		t.Errorf("the banner does not name the repository and URL:\n%s", banner)
	}
	if !strings.Contains(banner, "stopped") {
		t.Errorf("the shutdown was not reported:\n%s", banner)
	}
}

// TestServeReportsAnAskedPortCollision covers the message rather than the binding: the
// "(port N was in use)" line is only correct when the port was not asked for, and it used
// to be printed on a path that can no longer happen.
func TestServeReportsAnAskedPortCollision(t *testing.T) {
	held, addr, err := listen(0, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatal(err)
	}

	o := testOptions()
	o.Port, o.PortWasAsked = port, true
	var out strings.Builder
	err = Serve(context.Background(), o, &out, false)
	if err == nil {
		t.Fatalf("Serve on a taken asked-for port = nil, want an error; output:\n%s", out.String())
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("127.0.0.1:%d", port)) {
		t.Errorf("the error does not name the address: %v", err)
	}
	// Nothing served, so nothing announced. A banner printed before the failure would
	// tell the reader a URL that does not answer.
	if out.String() != "" {
		t.Errorf("Serve printed on the failure path:\n%s", out.String())
	}
}
