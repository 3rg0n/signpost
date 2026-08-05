// Package view serves the graph viewer from the binary, on the loopback interface,
// for as long as the command runs.
//
// The whole of the design is that this adds no artifact and no state. There is no
// cache directory, nothing written to the repository, and no second copy of the
// viewer: the assets are the ones site/ publishes (see site.Files), and the graph is
// the same JSON `signpost graph export -format json` writes, held in memory and
// discarded when the process exits. A `view` that wrote a graph.json would create
// exactly the stale second artifact ADR 0008 declined to commit.
//
// **The graph is a snapshot, taken before the listener opens.** Nothing re-analyses
// on a request, and nothing watches the tree. That is a decision rather than a
// simplification: a viewer that quietly re-read the repository would change what it
// shows while somebody was reading it, and a viewer that re-analysed per request
// would spend seconds of CPU on a page reload. The page says which commit it
// describes and what was already out of step when it started, so a reader can tell
// how old the picture is. Restarting is the refresh.
//
// # Why loopback, and why the Host header is checked
//
// The document interpolates strings that came out of a repository — module names,
// file paths — and lists every file in every module. That is a private repository's
// structure, so it is served to this machine and only this machine: the listener
// binds 127.0.0.1, never 0.0.0.0, and there is no flag to change that.
//
// Binding loopback is not sufficient on its own. A page the user is browsing can
// issue requests to 127.0.0.1, and the same-origin policy stops it *reading* the
// responses only because no CORS header is ever set here. DNS rebinding is the case
// that defeats that: an attacker's hostname re-resolves to 127.0.0.1, so the browser
// treats the response as same-origin with the attacker's page. checkHost is the
// mitigation — a request whose Host is not a loopback name is refused before any
// repository content reaches the response.
package view

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/3rg0n/signpost/site"
)

// The document served at /. Embedded rather than a Go string literal so it stays
// editable as HTML — it is markup graph.js queries by attribute, and the parity test
// reads both files.
//
//go:embed view.html
var docFS embed.FS

var doc = template.Must(template.ParseFS(docFS, "view.html"))

// DefaultPort is what `signpost view` listens on when nothing says otherwise.
//
// Fixed rather than ephemeral, so the URL is the same on every run and can be left
// open in a tab across restarts. High and unfashionable on purpose: 3000, 5000, 5173,
// 8000, and 8080 are all in use on a working developer's machine, and a default that
// usually collides is a default that always prints a different URL.
const DefaultPort = 7777

// Options is everything the page shows. The caller has already analysed the
// repository; this package neither walks a tree nor runs git.
type Options struct {
	// Root is the repository path, as the analysis resolved it. Shown so a reader can
	// tell which checkout a tab is pointing at.
	Root string
	// Title heads the page: the repository's name if one is known, or the directory's.
	Title string
	// Commit is the short sha the analysis describes, or empty where there is no
	// readable history. Empty is printed as nothing rather than as "unknown", because
	// the Notes already say history was not read.
	Commit string
	// RepoBase is the URL prefix a file path is appended to, with a trailing slash, or
	// empty when nothing known can be built. Empty means graph.js renders a filename as
	// text instead of as a link — see repoBase.
	RepoBase string
	// Graph is the JSON document graph.js fetches, exactly as `graph export -format
	// json` writes it.
	Graph []byte
	// Nodes and Edges are what the page states in prose. Passed rather than counted
	// from Graph so this package does not parse what it only serves.
	Nodes, Edges int
	// Notes are what was already out of step before the server started — a bundle
	// behind the code, uncommitted edits the commit stamp does not cover. Printed
	// verbatim, so each entry is a whole sentence.
	Notes []string
	// Port is the TCP port on 127.0.0.1. Zero asks the kernel for a free one.
	Port int
	// PortWasAsked separates "-port 7777" from an unpassed flag that defaults to the
	// same number, and the two must not behave alike: a port somebody named is one they
	// want, most likely because something else is configured to reach it, so a
	// collision is an error. An unnamed one is a convenience, and a collision falls back
	// to whatever is free.
	//
	// A bool beside the port rather than a sentinel value, because every port is a
	// legitimate value and there is none left to mean "unset". Comparing Port against
	// DefaultPort was the first way this was written and it is wrong for exactly the
	// case that motivated the flag: `-port 7777` with 7777 taken fell back silently,
	// having been mistaken for the default. See applyConfig for the same distinction
	// drawn the same way.
	PortWasAsked bool
}

// asset is one file this server will serve, with its type decided here.
type asset struct {
	body  []byte
	ctype string
}

// assets maps a request path to its content, and the map is the whole of the routing.
//
// An explicit map rather than http.FileServer over the embedded FS, for three
// reasons that all bite: FileServer serves directory listings, it redirects
// /index.html to / in a way that has to be reasoned about, and it resolves the
// content type through mime.TypeByExtension — which on Windows reads the registry, so
// a machine with an unusual HKCR\.js can serve the viewer as something the browser
// declines to execute. Every type here is a literal, and a path not in the map is a
// 404 rather than an attempt to find a file.
func (o Options) assets(addr string) (map[string]asset, error) {
	page, err := o.render(addr)
	if err != nil {
		return nil, err
	}
	js, err := site.Files.ReadFile("graph.js")
	if err != nil {
		return nil, err
	}
	css, err := site.Files.ReadFile("style.css")
	if err != nil {
		return nil, err
	}
	icon, err := site.Files.ReadFile("favicon.svg")
	if err != nil {
		return nil, err
	}
	// charset on every text type. Without it a browser may guess, and a repository
	// containing a non-ASCII path would render it as mojibake in the one view whose
	// job is to show what is there.
	return map[string]asset{
		"/":            {page, "text/html; charset=utf-8"},
		"/graph.json":  {o.Graph, "application/json; charset=utf-8"},
		"/graph.js":    {js, "text/javascript; charset=utf-8"},
		"/style.css":   {css, "text/css; charset=utf-8"},
		"/favicon.svg": {icon, "image/svg+xml"},
	}, nil
}

// render fills the document. html/template escapes every interpolated value by
// context, which is what makes it safe to put a repository's own strings in a page.
func (o Options) render(addr string) ([]byte, error) {
	var buf strings.Builder
	err := doc.ExecuteTemplate(&buf, "view.html", struct {
		Options
		Address string
	}{o, addr})
	if err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

// Serve analyses nothing, listens on loopback, and blocks until ctx is done.
//
// The URL is printed before the browser is opened and before anything is served, so
// a machine with no browser — a container, a remote shell — is still told where to
// point one. That ordering is the difference between a command that works headless
// and one that appears to hang.
func Serve(ctx context.Context, o Options, out io.Writer, open bool) error {
	ln, addr, err := listen(o.Port, o.PortWasAsked)
	if err != nil {
		return err
	}
	defer func() {
		// Closed by the shutdown below on the normal path; this covers the early
		// returns, and closing twice is not an error worth reporting.
		_ = ln.Close()
	}()

	files, err := o.assets(addr)
	if err != nil {
		return err
	}
	url := "http://" + addr + "/"

	if _, err := fmt.Fprintf(out, "serving %s on %s\n", o.Title, url); err != nil {
		return err
	}
	if o.Port != 0 && !strings.HasSuffix(addr, fmt.Sprintf(":%d", o.Port)) {
		// Only reachable when the port was not asked for and was taken: a named -port that
		// cannot be bound is an error above, and a zero Port asked for nothing. Said out
		// loud because a URL differing from the one in the reader's bookmark is otherwise
		// a mystery.
		if _, err := fmt.Fprintf(out, "  (port %d was in use)\n", o.Port); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(out, "stop it with ctrl-c\n"); err != nil {
		return err
	}

	srv := &http.Server{
		Handler: handler(files),
		// A local viewer has no reason to hold a connection open for long, and these
		// bound what a stuck client can occupy. ReadHeaderTimeout specifically is the one
		// a linter asks for: without it a connection that opens and sends nothing keeps a
		// goroutine indefinitely.
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	if open {
		// After the listener exists, so the browser cannot arrive before the port is
		// accepting. A failure here is reported and not returned — the URL is already
		// printed, and a machine with no way to open a browser is a normal machine, not a
		// failed run.
		if err := openBrowser(url); err != nil {
			if _, werr := fmt.Fprintf(out, "  could not open a browser (%v); open the URL above\n", err); werr != nil {
				return werr
			}
		}
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		// ErrServerClosed cannot arrive here: nothing has called Shutdown yet on this
		// path, so anything from Serve is a real failure.
		return err
	case <-ctx.Done():
	}
	// A bounded shutdown rather than Close, so a response in flight finishes. The
	// deadline is what stops ctrl-c from appearing to hang on a client that is holding
	// a connection open.
	sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if _, err := fmt.Fprintf(out, "stopped\n"); err != nil {
		return err
	}
	return nil
}

// listen binds loopback, falling back to a kernel-chosen port only for the default.
//
// The literal 127.0.0.1 is the security property, and it is here rather than in a
// configurable field so that no flag, config key, or environment variable can widen
// it. "localhost" is deliberately not used: it resolves through the host's resolver,
// which can answer with something that is not loopback.
func listen(port int, asked bool) (net.Listener, string, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err == nil {
		return ln, ln.Addr().String(), nil
	}
	if asked {
		// A port somebody named. Reported rather than worked around: they passed it most
		// likely because something else is configured to reach it, and quietly serving a
		// different one would satisfy the command and not the intent.
		return nil, "", fmt.Errorf("listening on 127.0.0.1:%d: %w", port, err)
	}
	ln, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("listening on 127.0.0.1: %w", err)
	}
	return ln, ln.Addr().String(), nil
}

// handler serves the fixed asset set and refuses everything else.
func handler(files map[string]asset) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Applied to every response including the errors below, because a 404 body is
		// still a document a browser renders.
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'none'; script-src 'self'; style-src 'self'; "+
				"connect-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		// The graph is a snapshot taken at startup, so a cached copy would outlive the
		// thing it describes: a reader who restarts `view` after changing code and gets a
		// 200 from cache is looking at the previous run's repository.
		h.Set("Cache-Control", "no-store")

		if !checkHost(r.Host) {
			// See the package comment. Before the method check and before any lookup, so
			// nothing about this repository is disclosed to a request that reached here
			// through a name that is not loopback.
			http.Error(w, "signpost view serves 127.0.0.1 only", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// Allow is required on a 405 and is what makes the refusal legible.
			h.Set("Allow", "GET, HEAD")
			http.Error(w, "GET or HEAD only", http.StatusMethodNotAllowed)
			return
		}
		a, ok := files[r.URL.Path]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		h.Set("Content-Type", a.ctype)
		// ServeContent rather than Write: it sets Content-Length, answers a HEAD without
		// a body, and handles a Range request, none of which is worth reimplementing. The
		// zero time disables Last-Modified and the conditional-request handling that goes
		// with it, which is right for content that only exists while this process does.
		http.ServeContent(w, r, r.URL.Path, time.Time{}, bytes.NewReader(a.body))
	})
}

// checkHost accepts only a loopback Host header.
//
// A Host with no port is valid (the default port for the scheme), so a missing-port
// error is not a rejection. An IPv6 literal arrives bracketed, which SplitHostPort
// unwraps; without it, "[::1]:7777" would be compared with brackets attached and
// refused.
func checkHost(host string) bool {
	if host == "" {
		// HTTP/1.1 requires it and Go's server rejects a request without one before a
		// handler runs, so this is HTTP/1.0 or a hand-rolled client. Nothing legitimate
		// reaches the viewer that way.
		return false
	}
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	// An address rather than a name, so a resolver cannot be what decides this.
	ip := net.ParseIP(strings.Trim(h, "[]"))
	return ip != nil && ip.IsLoopback()
}
