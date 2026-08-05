# 18. `view` serves a repository's structure over loopback, and holds no state

## Status

Accepted

## Context

[ADR 0008](0008-the-viewer-lives-in-this-repository.md) moved the viewer into this repository as
static files a Pages job publishes. That decision assumed one reader: somebody looking at
*signpost's own* graph, because the deploy job runs the export against this tree and commits the
result to the published site. Every other repository — the interesting ones — had no viewer at
all. A reader with a bundle could read the Markdown in GitHub or open the GraphML in yEd, which
means installing a graph tool and knowing what to do with a file format.

`signpost view` closes that: analyse the repository in front of you, serve the graph, open a
browser. It is the first command in the tool that opens a socket, and that is what makes it an
ADR rather than a feature. Everything else signpost does reads files and writes files. This
listens, and what it serves is a complete inventory of a repository's modules, files, and
dependencies — for a private repository, one of the more sensitive artifacts on the machine
short of the source itself.

Four forces, and each had a plausible answer that was rejected:

**Where the data comes from.** A cached `graph.json` written next to the bundle would make the
server trivial and startup instant. It also creates a second artifact describing the same tree,
which is the thing ADR 0008 declined to commit and [ADR 0007](0007-the-bundle-names-the-commit-it-describes.md)
built a staleness check around — reintroduced by the one command whose output nobody keeps.
Gating on a fresh bundle was the other candidate: `view` refuses unless `build` has run and the
bundle matches. That inverts the value, since the reader who most needs to *see* the structure is
the one deciding whether to commit a map of it, and they have no bundle yet by definition.

**Whether the server re-reads the tree.** A file watcher, or an analysis per request, keeps the
page live while somebody edits. A full pass over this repository's 185 files takes about five seconds; over a
large one, longer. Per-request analysis spends that on every reload, and a watcher makes the page
change under a reader who is comparing two parts of it.

**What it binds.** `0.0.0.0` with a flag to restrict it is the shape most dev servers ship, and
it is the shape that puts a private repository's structure on the hotel wifi when somebody omits
the flag. Loopback with a flag to widen it is better and still wrong: the flag exists, so
somebody's shell alias sets it. And loopback alone is not sufficient — a page the reader is
browsing can issue requests to `127.0.0.1`, and the same-origin policy withholds the *responses*
only because no CORS header is set. DNS rebinding defeats that: an attacker's hostname
re-resolves to `127.0.0.1`, the browser considers the response same-origin with their page, and
the fetch succeeds. The mitigation is not at the socket.

**What the page may reach.** `site/graph.html` loads Archivo and Spline Sans Mono from Google
Fonts, and its CSP allows those origins. A locally-served page doing the same makes an outbound
request per view, which tells a third party the times at which somebody looked at a repository,
and renders in a fallback face on a machine with no route out.

## Decision

**`view` serves from the analysis it just ran, holds no state, and writes nothing.** No cached
graph, no `graph.json`, nothing in the repository or in a temp directory. It does not require
`build` to have run and does not check the bundle before serving. Where a bundle exists and is
behind the tree, the page says so as a note — `view` is the command somebody runs *instead of*
opening the bundle, so a bundle page's own staleness banner never reaches them.

**The graph is a snapshot taken before the listener opens.** Nothing re-analyses on a request and
nothing watches the tree. The page states the commit it describes; restarting is the refresh.

**The bind address is the literal `127.0.0.1`, and it is not configurable.** Not a flag, not a
config key, not an environment variable. The absence of the knob is the security property: a
default is something a wrapper script overrides, and there is no legitimate reason for this
command to be reachable from another host.

**A request whose `Host` header is not a loopback name is refused, before anything else.** Before
the method check and before the asset lookup, so a refused request cannot distinguish a path that
exists from one that does not, and the refusal body names nothing about the repository. Accepted
values are `localhost` and `127.0.0.1`, with or without a port, matched whole — a suffix or
substring match would accept `localhost.attacker.example`, which is the exact attack.

**The document is fully offline.** Its CSP names no webfont origin and the page loads no font;
`site/style.css` is served unchanged, because its stacks already fall through to `ui-sans-serif`
and `ui-monospace`.

**Assets are an explicit map from path to bytes with content types written as literals**, not an
`http.FileServer` over the embedded FS. `FileServer` serves directory listings, redirects
`/index.html` to `/`, and resolves content types through `mime.TypeByExtension`, which on Windows
reads the registry — so a machine with an unusual `HKCR\.js` serves the viewer as a type the
browser declines to execute.

**A `-port` the user named is honoured or the command fails; the default falls back.** An
explicit port is named because something else is configured to reach it, and quietly serving a
different one satisfies the command and not the intent. Set-ness comes from `flag.Visit`, since
every port is a legitimate value and `-port 7777` is indistinguishable from an unpassed flag by
value alone.

## Consequences

**A socket is now part of signpost's threat model, and it was not before.** The `Host` check is
load-bearing security in a tool whose other commands have no network surface at all, which means
it is the kind of code a future refactor can weaken without any test failing on the happy path.
It is asserted in both directions, including that the 403 body leaks neither the repository path,
nor the declared repository name, nor the commit.

**The viewer is now one file serving two documents,** and `graph.js` finds every control it drives
by attribute name. A hook renamed in `site/graph.html` and not in `internal/view/view.html` is
neither a compile error nor a runtime error — the control silently stops working, in whichever of
the pair nobody is looking at. A test extracts the `data-` attributes from both documents and
compares the sets; it is the only thing that notices. The alternative, one document with the
differences templated, was rejected because the published page is a static file a Pages job
serves and templating it would put a build step in front of it.

**Re-running the analysis is the price of no state.** Startup costs a full pass, seconds on a
large repository, every time. That is paid where it is visible — the coverage report prints to
stderr as it always does, and the URL is printed before the browser is opened — rather than
amortised into a cache whose invalidation would be the next bug.

**No flag can widen the bind address, which is a real limitation stated as a decision.** Somebody
who wants to show a colleague the graph cannot; the answer is `ssh -L`, which puts the
authentication and the audit trail somewhere that already has both. If a case for a wider bind
arrives, it supersedes this ADR rather than adding a flag to it.

**The fully-offline document diverges from the published page's typography.** They render in
different faces, and a reader who sees both will notice. This is the intended trade: an outbound
request from a localhost tool is worse than a font mismatch, and the divergence is one line of
CSP plus an absent `<link>`, not a second stylesheet.
