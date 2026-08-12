# 29. The viewer is written by the run that publishes it

## Status

Accepted

## Context

`signpost init pages` was specified to scaffold a Pages deploy workflow the way
[ADR 0028](0028-scaffolded-files-are-embedded-and-tested-against-our-own.md) scaffolds
`signpost.yml`: embedded, previewed, never overwriting, and tested against the workflow this
repository runs. Writing it surfaced a problem in the premise rather than in the
implementation.

**This repository's `pages.yml` uploads `site/`, and `site/` is a directory that exists only
here.** It holds the landing page, the stylesheet, `graph.html`, and `graph.js` — files
written by hand, committed, and specific to this project. The deploy generates one thing into
it, `graph.json`, and uploads the directory. A scaffolded repository has no `site/`, so a
faithful copy of that workflow uploads nothing, and the scaffold would be a file that
succeeds while publishing an empty site.

No command produced a directory to upload. The viewer existed in exactly two forms: committed
in `site/` for our own deploy, and embedded in the binary for `signpost view`, which binds a
port and serves it. §7.3 states that `view` "writes nothing anywhere" and
[ADR 0018](0018-view-serves-a-repository-over-loopback.md) makes it a decision rather than an
implementation detail — a `view` that left an artifact would create the stale second copy
[ADR 0008](0008-the-viewer-lives-in-this-repository.md) declined to commit, from the one
command whose output is transient.

So the scaffold needed something that did not exist, and the obvious ways to get it were both
wrong:

1. **Have the template commit the assets.** `init pages` writes the viewer's HTML, CSS, and
   JavaScript into the adopter's repository alongside the workflow. This is what ADR 0008
   declined, in the one place where the consequence is worst: roughly 57KB of hand-written
   JavaScript copied into repositories we cannot see, going stale the moment either the
   exporter's JSON shape or the viewer changes, with no channel to patch it. A published
   viewer built against last year's `graph.json` renders a blank frame rather than failing.
2. **Have the template fetch the assets at deploy time.** A `curl` of `graph.js` from a
   release, or from Pages, or from a registry. This is ADR 0028's rejected design with a
   worse blast radius: an unauthenticated fetch of executable content into a job that
   publishes to a URL, needing signature verification, needing a dependency §2 will not take.

The third option is that the binary already holds the bytes and could simply write them.

## Decision

**`signpost view -static <dir>` writes the viewer to a directory and exits.** The page, the
stylesheet, the script, the icon, and `graph.json` — the same five responses `view` serves
over a port, as files. Nothing is committed, and the scaffolded deploy calls it into
`RUNNER_TEMP`.

**This is not a reversal of ADR 0008 or 0018, and the distinction it turns on is stated here
because it is the whole justification.** What those records decline is a *committed* copy of
derived data: an artifact that outlives the run that made it, in a repository whose central
claim is that stale fails loudly. `-static` writes derived data that is uploaded and
discarded by the same run. It cannot be stale, because there is no interval during which it
exists and the tree has moved on. The same argument ADR 0008 makes for not committing
`graph.json` — "it has no value without the page that reads it" — extends to the page: it has
no value without the graph beside it, and both are produced together or not at all.

**The exported files come from the map `view` serves, not from a list.** `WriteStatic` reads
`Options.assets("")` — the same function the HTTP handler routes — and writes one file per
entry, named for its route with `/` becoming `index.html`. A fifth asset added to the server
and forgotten in the export is therefore not possible, which is the reason the function is
written this way rather than as five `os.WriteFile` calls. The test asserts against
`assets()` rather than against filenames, for the same reason.

**One document serves both, with the address as the switch.** `view.html` renders the local
address and the "stop it with ctrl-c" line only when `Options.Address` is set, which it is
not for an export. A third template was rejected: it would double the drift surface the
parity discipline in ADR 0028 exists to catch, in a file where the divergence would be
invisible until somebody published a page saying it was served from their laptop.

**The meta-tag CSP is load-bearing for the exported page in a way it is not for the served
one.** `Serve` sets `Content-Security-Policy` as a response header and the header is the copy
that binds. A static host sends whatever headers it likes, and GitHub Pages sends no CSP — so
on a published page the meta tag is not a courtesy for somebody who saved the file, it is the
only CSP there is. ADR 0008's hardening rule now depends on a tag inside the document, and a
test asserts it survives the export.

**`-static` refuses `-port` and `-no-open` rather than ignoring them**, with exit 2. It does
not listen, so neither can be honoured, and a command that silently drops a flag somebody
passed leaves them believing something happened that did not. Exit 2 rather than 1 because
that is the contract with CI: 2 means the command line was wrong and re-running it unchanged
fails the same way.

**`init pages` scaffolds one file, and it makes no network call.** The workflow it writes
carries `contents: read`, writes the site into `RUNNER_TEMP`, and fails rather than publishing
a graph with no nodes. Two things it deliberately does not do:

- **No `site/CNAME` check.** Ours has one because a `site/` published without the file
  *clears* the custom domain in repository settings, and the failure is a deploy that
  succeeds while moving the site back to the `github.io` address. That is a property of this
  repository's apex domain. A scaffolded deploy has no custom domain to protect, and a check
  for a file that will never exist is a step that fails on the first run.
- **No `repos/{owner}/{repo}` lookup to confirm the site would be private.** This was
  specified and is declined, on two grounds. It would make `init` the only command that
  touches the network, against ADR 0028 and against §2. And it would be a gate in appearance
  only: `actions/configure-pages` documents that enabling Pages "requires a token other than
  `GITHUB_TOKEN`", so the scaffolded workflow *cannot* turn publishing on. Somebody has to
  set Settings → Pages → Source to "GitHub Actions", and that act is the consent. What is
  left worth doing is making sure they know what they are consenting to.

**So the visibility consequence is stated, in four places, and asserted in all four.** The
preview, the confirmation after `-y`, `init pages -h`, and the written file's own comments
each say what gets published — every module name, every file path, and who has been changing
them — and each states GitHub's actual rule rather than the intuition. The rule, from GitHub's
own documentation: publishing a Pages site privately requires GitHub Enterprise Cloud, and
access control applies only to project sites from private or internal repositories owned by
an **organization**. A personal account's private repository publishes a site anyone can
read, and an organization site cannot use access control at any tier. The intuition that a
private repository gets a private site is true in one configuration and this is the sentence
that says which.

## Consequences

`init pages` produces a workflow that publishes something in any repository, which is what
made #34 shippable at all. The scaffolded deploy and ours now run the same viewer from the
same bytes, and the only remaining difference between the two files is the one ADR 0028
already established: we build from source, an adopter installs a pinned release.

**This repository's own `pages.yml` is not changed by this decision, and that is a divergence
worth naming.** Ours still uploads `site/`, because it publishes a landing page that
`-static` knows nothing about and carries a `CNAME` that `-static` must not invent. So the
parity test between the two files asserts three intended differences rather than one, and the
scaffolded deploy is the one that exercises `-static` in CI. Converging them would mean
teaching the exporter about this project's landing page, which is the wrong direction: the
exporter's job is the viewer, and the landing page is content.

**`view` now has a mode that writes files, so "writes nothing anywhere" needs a qualifier
everywhere it appears.** §7.3, the command's help, and its doc comment all say it, and all
three now say where the exception is and why it is not the artifact ADR 0018 refused. The
risk this leaves is a reader who learns the short version: the mitigation is that the flag
names the directory, so there is no default path anything lands in by accident.

**A published site is a decision the tool can inform and cannot make.** Nothing signpost
writes can enable Pages, so nothing signpost writes can publish a private repository's
structure by itself — but once somebody flips the setting, every push to the default branch
republishes. The scaffold cannot know whether that was understood, only whether it was
stated. That is the accepted limit of a scaffold, and it is why the statement is asserted in
four places instead of one.

**The empty-graph guard is in the workflow, not in the exporter.** `view -static` on a
repository with nothing in it writes a valid page with an empty graph, which is correct
behaviour for a viewer and the wrong thing to publish: an empty frame looks like a working
page. The deploy counts nodes in `graph.json` and fails below one. Putting that check in
`WriteStatic` would make an empty repository an error in a command whose job is to describe
whatever it was pointed at.

Design reference: [docs/design.md](../design.md) §7.2, §7.3, §8.3.
