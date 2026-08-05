// Package site embeds the viewer's assets so `signpost view` can serve them from
// the binary, with no network and no second checkout.
//
// **This package exists because `go:embed` cannot reach upwards.** An embed pattern
// resolves against the directory holding the source file and may not contain `..`,
// so the only two places that can embed `site/` are the repository root and this
// directory. Root would make the whole tree a package; this is the smaller of the
// two. It is a component of the viewer rather than an API, and nothing outside
// internal/view imports it.
//
// The alternative was a copy of these files under internal/, kept in step by a
// check. That trades a real risk for a bookkeeping one: two copies of a 1400-line
// viewer drift, and the drift shows up as a control that silently does nothing.
// One copy, embedded, cannot.
//
// graph.html is embedded and never served. `signpost view` serves its own document
// (internal/view/view.html) because the two genuinely differ — no fonts fetched
// from Google, no landing-page navigation, and a repository that is not this one.
// What must not differ is the markup graph.js queries, and the parity test in
// internal/view reads this copy to assert that. Embedding it is what makes that
// test read the file the deploy publishes rather than a path it guessed.
package site

import "embed"

// Files holds the viewer as published. Read-only, and the same bytes pages.yml
// uploads.
//
//go:embed graph.html graph.js style.css favicon.svg
var Files embed.FS
