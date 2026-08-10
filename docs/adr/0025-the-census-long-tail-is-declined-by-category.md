# 25. The census long tail is declined by category, not one file type at a time

## Status

Accepted

## Context

A census of file types across roughly 14,000 repositories produced a list far longer than the set
signpost reads. Most of the entries are single-digit counts, and a few dozen are formats no
structural map has a use for. The list has been sitting in a task description, which is the wrong
place for it: what a tool declines to read is a claim about the tool, and a reader who finds
`.jinja2` files unread has no way to tell a decision from an oversight. §4.2 already says the
absence of a measurement is never a clean bill of health, and that rule applies to the roadmap as
much as to a run's output.

Answering the long tail file type by file type does not work, for two reasons. There are too many
to reason about individually, and reasoning about them individually produces the wrong answers:
`.jinja2`, `.erb`, `.hbs` and `.gotmpl` are one decision, and taking them one at a time invites
four different ones. The interesting property is not the extension, it is what kind of thing the
format *is*.

## Decision

**The long tail is declined by category, with the category stated, and revisited only when a fleet
scan shows a specific format blocking real coverage.** Five categories, each with a reason that
applies to every member:

1. **Editor and tooling artifacts, not repository content.** `.editorconfig`, vim and TextMate
   files, `.log`, `.eml`, `.diff`, `git-commit` buffers, `.http` request files, Copilot chat
   transcripts, and the various `text`/`plaintext`/`unknown` labels. These are not what the
   repository is; they are debris from working on it, and a node for one would be noise on a map
   whose whole value is that everything on it means something.
2. **Diagram and document formats with no dependency structure to extract.** Mermaid and its
   variants, PlantUML, LaTeX/TeX, Twee3. A diagram is a *picture* of structure and not a statement
   of it — reading one would produce a second, unverifiable graph beside the one derived from the
   code, and where the two disagreed the bundle would have no way to say which was true.
3. **Templating layers whose imports belong to their host language.** Jinja and its dialects,
   Go templates, Handlebars, Mustache, ERB, RHTML, Haml, XSL. The dependency a template expresses
   is the host program's, already read from the host program. Reading the template as well
   double-counts it and attributes it to the wrong file.
4. **Styling and markup.** CSS/SCSS/Less, HTML, XML, the CSV family. A stylesheet's `@import`
   reaches a file this graph holds no node for, which is the same conclusion the SFC extractor
   reached from the other direction (§4.1: a `<style>` block's import must appear in neither gap
   line, because as unresolved it would tell a reader to declare a dependency that is a file in the
   same tree, and as unlinked it would claim a missing edge onto a node no stylesheet should have).
5. **Low-count languages worth revisiting on demand.** Perl, Lua, Dart, Swift, Erlang, Clojure,
   Julia, R, CoffeeScript, VB, M4, CUE, Rego, Robot Framework, Cucumber/Gherkin, Puppet, Jupyter,
   and the config dialects (`sshconfig`, `gitconfig`, `.conf`, `debiancontrol`, `.spec`). These are
   declined on *count*, not on kind — each is a real language with a real module system, and each
   would be a straightforward extractor by the §4.2 harness. The reason to wait is that an
   extractor nobody's repository needs still has to be maintained, scored, and kept honest.

The revisit trigger is a fleet scan showing a named format blocking coverage on a repository
somebody has — not a guess about which might matter. That is the same standard ADR 0022 sets for
replacing a hand-written extractor: a threshold measured on fixtures rather than a feeling about
parsers.

Two entries in the tail are declined for a reason of their own and are recorded here so the
asymmetry is not read as an omission. **Pyspark** is Python, already covered; the Spark-specific
signal in a Pyspark file is a *dataflow* fact, and this graph holds module structure — a dataflow
edge on it would be a different kind of claim wearing the same arrow. And a **Rakefile** is
classified as a manifest because it is a build file, but what it holds is task definitions in Ruby;
the manifest registry deliberately does not claim it (`internal/manifest/registry.go`, `matchGem`),
because a dependency reader would find no dependency in it. A task runner's targets belong with the
build-graph work of ADR 0023, not with dependency manifests.

## Consequences

The tail is auditable. A contributor who wants Lua can see that Lua was considered, declined on
count rather than on principle, and what evidence would change the answer — which is a different
conversation from discovering silence and having to guess.

Declining by category also binds future decisions in a way a per-format list would not: a new
templating language arriving in a census is already answered by category 3, and answering it
differently now requires saying why the category is wrong. That is the intended cost.

The categories are not permanent, and category 5 is the one expected to shrink. Categories 1
through 4 are closer to structural: a format that is not repository content, or holds no dependency
structure, or restates its host's imports, does not become readable because more repositories
contain it.

One thing this ADR does *not* do is make the declined set visible in a run's output. The manifest
registry counts files no route claimed, but that count includes files other subsystems read —
`internal/practice` reads `dependabot.yml` and `renovate.json`, `internal/assemble` reads the
docs — so printing it would report unread files on a repository where none are unread. The source
side has no such overlap and does print its gap (`no extractor for: …`). Closing that asymmetry
needs a cross-subsystem notion of "read by someone" and is deliberately left undone rather than
approximated, because a coverage line that overstates a gap costs the same trust as one that hides
it.
