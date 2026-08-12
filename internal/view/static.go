package view

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// WriteStatic writes the viewer to dir as ordinary files, and returns what it wrote.
//
// This is the half of the viewer a deploy needs. `Serve` holds the same bytes in memory
// behind a listener, which is right for one person looking at one repository and useless
// to GitHub Pages — Pages uploads a directory. Until this existed, a scaffolded Pages
// workflow had nothing to upload: the assets live in this binary (see site.Files), and
// the only command that could reach them bound a port and blocked.
//
// **The files come from assets(), the same map handler() routes.** That is the point of
// the function rather than an implementation note. A separate writer listing the four
// files itself would be a second definition of what the viewer consists of, and the way
// that fails is a fifth asset added to the server and not to the export — a published
// page that 404s one request and renders a frame with a control missing. There is one
// list, and both callers read it.
//
// The address recorded in the page is the empty string here, because there isn't one: a
// static export is served from wherever it is uploaded to, and the document says so
// instead of naming a host it cannot know. See view.html, where a missing Address is what
// distinguishes the two.
//
// Nothing is removed. A stale file from a previous export is left where it is, which is
// deliberate: this writes into a directory the caller named, and a function that deletes
// what it does not recognise is one that eats a CNAME the first time somebody points it
// at a directory holding one. The Pages artifact is assembled by the workflow, and it is
// the workflow's business what else belongs in it.
func WriteStatic(dir string, o Options) ([]string, error) {
	files, err := o.assets("")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { // #nosec G301 -- a published site's directory; the umask default is the mode it should have.
		return nil, err
	}

	// Keyed by the name on disk rather than by the route, and sorted on that, because the
	// name is what gets reported: `/` sorts before every other route while `index.html`
	// sorts into the middle of the filenames, so sorting routes and reporting names
	// produces an order that is neither. Map iteration is randomised, so unsorted here is
	// output that reorders between runs of the same command.
	names := make([]string, 0, len(files))
	byName := make(map[string]asset, len(files))
	for route, a := range files {
		name := staticName(route)
		names = append(names, name)
		byName[name] = a
	}
	sort.Strings(names)

	written := make([]string, 0, len(names))
	for _, name := range names {
		// #nosec G304 -- the path is a route name from assets(), joined onto the caller's
		// directory. Every element is a literal in this package.
		dest := filepath.Join(dir, name)
		// #nosec G306 -- these are files a web server reads and publishes; 0o600 would
		// make the artifact unreadable to the process that uploads it.
		if err := os.WriteFile(dest, byName[name].body, 0o644); err != nil {
			return nil, fmt.Errorf("writing %s: %w", name, err)
		}
		written = append(written, name)
	}
	return written, nil
}

// staticName turns a server route into a filename.
//
// One rule, and it only has one case to handle: `/` is the document, which on a static
// host has to be `index.html` for a directory URL to resolve. Everything else is already
// a filename with the leading slash taken off.
//
// A route that grew a directory component would land in a subdirectory here, which
// filepath.Join handles and TestStaticWritesEveryServedAsset would show, so this does not
// need to guard against one.
func staticName(route string) string {
	if route == "/" {
		return "index.html"
	}
	return strings.TrimPrefix(route, "/")
}
