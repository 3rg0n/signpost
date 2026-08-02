---
type: Practices
title: How work is done here
description: "What this repository declares about building, testing, gating, and ownership — and what it does not."
resource: git://d0ea89253a2bdcb6948c4746ef4cd98bce8db2b0
generated: { by: signpost/dev, at: "2026-08-02" }
---
# How work is done here

Each line is something this repository states, or something it does not. A missing declaration is not a criticism and there is no score here: it is a fact about what an agent can rely on, and the absences are the ones worth reading, because they are what it would otherwise have to guess.
<!-- signpost:managed:practices -->
### Building

- **Not declared.** No build command is declared. An agent asked to build this repository has to infer how, and its first guess is not reviewable.
  - Looked in Makefile targets, package.json scripts, and Cargo aliases.

### Testing

- **Not declared.** No test command is declared. This is the fact an agent most needs before it offers to add a test, because it decides where the test goes and how it is run.
  - Looked in Makefile targets, package.json scripts, and Cargo aliases.
- 48 test files in the tree.

### What blocks a merge

- 10 jobs can block a merge: `corpus (a repository signpost did not write)`, `dependency gate`, `deploy`, `installer parses (5.1 and 7)`, `lint`, `rebuild the bundle`, and 4 others.
  - Stated in `.github/workflows/ci.yml` line 23, `.github/workflows/pages.yml` line 36, and `.github/workflows/signpost.yml` line 43.
- 2 further CI jobs run outside that gate — on a schedule, a tag, or manually.
  - Stated in `.github/workflows/release.yml` line 17 and `.github/workflows/signpost-semantic.yml` line 41.

### Dependencies

- **Not declared.** The Go dependencies are declared but not pinned by any lockfile in the tree, so two builds can resolve different versions.
  - Looked in `go.mod`.
- Automated dependency updates are configured.
  - Stated in `.github/dependabot.yml` and `renovate.json`.

### Ownership and policy

- 6 ownership rules assign paths to reviewers.
  - Stated in `.github/CODEOWNERS`.
- The repository states its licence.
  - Stated in `LICENSE`.
- A security policy states how to report a vulnerability.
  - Stated in `SECURITY.md`.

### Documentation

- The repository has a README.
  - Stated in `README.md`.
- 19 documentation files in the tree, outside the bundle.

### Observability

- **Not declared.** No observability library is a declared dependency, so a failure in production is diagnosed from whatever the code happens to log.
  - Looked in declared dependencies of every manifest read.

### Instructions for agents

- 14 stated rules for agents working in this repository.
  - Stated in `AGENTS.md`.
- 14 architecture decision records state why things are the way they are.
  - Stated in `docs/adr/0001-hand-written-tolerant-yaml-reader.md`, `docs/adr/0002-patchable-dependencies-not-zero-dependencies.md`, `docs/adr/0003-directory-granularity-for-module-nodes.md`, `docs/adr/0004-confidence-is-a-first-class-field.md`, `docs/adr/0005-commit-the-bundle-to-the-repository.md`, `docs/adr/0006-generator-and-viewer-are-separate-repositories.md`, and 8 other files.
<!-- /signpost:managed:practices -->

## Notes

_Anything written here is yours. signpost rewrites only the regions between its managed markers, and never this section._
