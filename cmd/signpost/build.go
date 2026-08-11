package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/3rg0n/signpost/internal/config"
	"github.com/3rg0n/signpost/internal/model"
	"github.com/3rg0n/signpost/internal/okf"
	"github.com/3rg0n/signpost/internal/practice"
	"github.com/3rg0n/signpost/internal/semantic"
	"github.com/3rg0n/signpost/internal/vcs"
)

// `signpost build` writes the OKF bundle to .signpost/.
//
// This is the command the other two exist to support: `graph` and `export` report what the
// pipeline found, and this one commits it to a durable artifact a reader will find without
// signpost installed.
//
// Two properties are the command's whole contract, and both are enforced in internal/okf
// rather than here: the same commit produces the same bytes, and text a human wrote outside
// the managed markers is never touched. This file's job is to say what happened, in enough
// detail that a run which preserved an edit or downgraded a verification is visible rather
// than buried in a file count.
func runBuild(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	// One writer for the whole of this command's usage, so the prose above and
	// PrintDefaults' flag list cannot land on different streams.
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
		u.printf("usage: signpost build [flags] [path]\n")
		u.printf("\nWrite the knowledge bundle to %s/ in the repository.\n\nFlags:\n", okf.BundleDir)
		fs.PrintDefaults()
	}
	var pf pipelineFlags
	pf.register(fs)
	quiet := fs.Bool("quiet", false, "suppress the coverage report on stderr")
	// The `resource:` field's host part. A repository has no way to know its own canonical
	// name — a remote URL is a checkout detail, and a fork's remote names the upstream — so
	// it is asked for rather than guessed. Absent, pages carry a commit-only resource, which
	// is enough for the staleness check verify performs and honest about what it does not
	// know.
	repo := fs.String("repo", "",
		"repository name for the resource URI, e.g. example.com/org/repo")
	// The semantic pass is opt-in twice over: a backend has to be configured (§5 makes
	// none the default) *and* this flag has to be set. Two gates rather than one because
	// they answer different questions — the environment says a model is available, and the
	// flag says this run should spend it. Without the flag, a developer who configured a
	// backend for `signpost model check` would find every subsequent build calling it.
	//
	// It is also what keeps §8's split honest. signpost.yml runs on every push and must
	// stay deterministic and byte-stable; signpost-semantic.yml runs on a schedule and
	// passes this. Relying on the environment alone would put the difference in a variable
	// somebody can set globally, where this puts it in the workflow file.
	sem := fs.Bool("semantic", false,
		"summarise modules with the configured model backend (§4.5); off by default")
	semTimeout := fs.Duration("semantic-timeout", 10*time.Minute,
		"how long the whole semantic pass may take before it stops and reports what it has")
	// Design §6.2. A flag on `build` rather than a verb of its own because it is about the
	// bundle build writes, and because ADR 0012 would otherwise make it a group: `suggest`
	// with one operation today and no sibling in sight.
	suggest := fs.Bool("suggest-agents-md", false,
		"print a proposed AGENTS.md pointer to stdout and exit, writing nothing")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	path, err := repoPath(fs)
	if err != nil {
		return err
	}
	// Before the walk, and it neither analyses nor writes. The stub is fixed text — see
	// agentsPointer — so spending a walk to print a constant would buy nothing, and printing
	// it alone is what makes `signpost build -suggest-agents-md >> AGENTS.md` the whole of
	// the adoption step. §6.2 is the boundary this respects: signpost writes .signpost/ and
	// nothing else, so the redirection is the human's to type.
	if *suggest {
		p := newPrinter(out)
		p.printf("%s", agentsPointer())
		return p.Err()
	}
	// Read from the root, before the walk, because the walk's options come out of it. See
	// config.go for why every analysing command does this in the same place.
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	applyConfig(fs, cfg, &pf)
	applyRepo(fs, cfg, repo)

	a, err := analyse(context.Background(), path, pf)
	if err != nil {
		return err
	}
	if !*quiet {
		reportCoverage(errOut, a)
	}
	// Clusters before writing: the manifest records how many the graph fell into, and the
	// assignment is computed lazily.
	a.Graph().Clusters()

	opts := buildOptions(a, *repo)
	pr := addPractices(&opts, a)
	if !*quiet {
		reportPractices(newPrinter(errOut), pr)
	}
	if *sem {
		sr, err := runSemantic(a, *semTimeout, cfg)
		if err != nil {
			return err
		}
		opts.Roles = sr.Regions()
		// On stderr and not suppressed by -quiet. -quiet silences the coverage report,
		// which is a routine summary; this is the §4.2 report of what a pass could not
		// account for, and a fail-open pass whose failures are quiet is the one failure
		// mode that looks like success.
		reportSemantic(newPrinter(errOut), sr)
	}

	res, err := okf.Write(a.Discovered.Root, a.Graph(), opts)
	if err != nil {
		return err
	}
	reportBuild(newPrinter(out), a.Discovered.Root, res)
	// On stderr, after the report, and not suppressed by -quiet. A bundle nothing points at
	// is the one failure mode a green build cannot show: every page is correct, verify passes,
	// and no agent opens it. -quiet silences the routine coverage summary; this names a
	// one-line fix, which is the same category as the §4.2 lines above it.
	if !pointsAtTheBundle(a) {
		reportPointer(newPrinter(errOut))
	}
	return nil
}

// pointerFiles are the files a model is trained to open, and so the only places a pointer at
// the bundle does any work.
//
// An explicit list rather than a scan of every doc. The question is not whether the string
// `.signpost` appears somewhere in the repository — it appears in the bundle's own pages, in a
// workflow that runs signpost, and in a .gitignore — but whether it appears somewhere read
// before the work starts. A broader search would find the first and report a repository as
// pointed-at while no agent ever gets there.
var pointerFiles = []string{
	"AGENTS.md",
	"CLAUDE.md",
	".cursorrules",
	".github/copilot-instructions.md",
	"README.md",
}

// pointsAtTheBundle reports whether any of those files names the bundle's index page.
//
// The index page rather than the bundle directory, and that is the whole precision of this
// check. `.signpost/` appears in prose that is not a pointer — a README paragraph explaining
// that the harness writes one, a note about what is gitignored — and matching it would report
// those repositories as pointed-at while no agent has been given anywhere to start. The corpus
// README says exactly that about the directory and nothing about the page, which is what
// caught the looser rule.
//
// It costs a false negative in the other direction: a file that points at `practices.md` or
// `manifest.json` and never at the index gets the note anyway. That is the right way round.
// The note costs a line and suggests something harmless; the silence the loose rule bought is
// the failure this exists to catch.
//
// Content from the walk rather than a fresh read: the walk has already read these files,
// applies the size caps, and normalises the paths, so re-reading them here would be a second
// path-handling implementation to keep in agreement with the first. A file over the size cap
// arrives truncated, so a mention in the elided middle of a very long README reads as absent —
// the same trade, in the same direction.
func pointsAtTheBundle(a *analysis) bool {
	want := make(map[string]bool, len(pointerFiles))
	for _, p := range pointerFiles {
		want[p] = true
	}
	target := okf.BundleDir + "/" + okf.IndexPage
	for _, f := range a.Discovered.Files {
		if want[f.Path] && strings.Contains(f.Content, target) {
			return true
		}
	}
	return false
}

// reportPointer says the bundle has nothing pointing at it, and how to fix that.
//
// Named as what it is rather than as a warning: nothing is wrong with the bundle, and a build
// that exited non-zero or said "warning" over a file signpost is forbidden to write (§6.2)
// would be reporting a fault it created. The flag is named because the alternative is a line
// telling somebody to go and compose a sentence.
func reportPointer(p *printer) {
	p.printf("nothing points at the bundle: no %s and no %s names %s/%s, so an agent has no "+
		"reason to open it\n", pointerFiles[0], pointerFiles[len(pointerFiles)-1],
		okf.BundleDir, okf.IndexPage)
	p.printf("  run `signpost build -suggest-agents-md >> %s` to add one, or write your own\n",
		pointerFiles[0])
}

// runSemantic builds the backend and runs the pass.
//
// The error return covers configuration only — an unknown backend name, a missing base
// URL — because that is a mistake in the invocation and failing open on it would mean a
// typo silently producing a deterministic bundle. Everything that happens *after* the
// backend is built fails open inside semantic.Run, per §5.
func runSemantic(a *analysis, timeout time.Duration, cfg *config.Config) (*semantic.Result, error) {
	// No -backend or -model flag on `build`, so the file sits directly under the environment
	// here. Which model to call is a property of the repository and configurable; *whether* to
	// call one is not, which is why -semantic itself has no key (ADR 0009, ADR 0011).
	b, err := model.New(modelConfig("", "", "", "", cfg))
	if err != nil {
		return nil, err
	}
	if b == nil {
		// -semantic with no backend configured. An error, not a skip: the flag is a request
		// to spend a model, and answering it with a silent deterministic build is how a
		// scheduled workflow runs for a month producing nothing while reporting success.
		return nil, fmt.Errorf("%w: -semantic needs a backend: set %s=inferd or %s=openai, or "+
			"a `backend:` key in %s (run `signpost model check` to test one)",
			errUsage, model.EnvBackend, model.EnvBackend, config.File)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return semantic.Run(ctx, semantic.Input{
		Graph:      a.Graph(),
		Discovered: a.Discovered,
		Backend:    b,
		CacheDir:   filepath.Join(a.Discovered.Root, okf.BundleDir, "cache", "summary"),
	}), nil
}

// addPractices runs the practices pass and puts its rendering on opts.
//
// Shared by `build` and `verify`, and that sharing is the point rather than a convenience.
// verify works by rendering the bundle the current tree would produce and comparing it to the
// one on disk, so any page build emits and verify does not is reported twice over: once as an
// orphan page describing a concept the repository no longer has, and once as an index that a
// build would change. Both messages describe the checker rather than the repository, and
// neither names the cause. That is what happened when this pass was wired into build alone.
//
// Deterministic, so it runs on every invocation rather than behind a flag. The findings are
// facts the pipeline has already extracted, and gating them would mean the page an agent most
// wants first — how do I build and test this — is the one it usually does not get.
func addPractices(opts *okf.Options, a *analysis) *practice.Result {
	// History may be nil, which is what -no-history and a tarball with no .git both produce.
	// practice.Analyse takes it as-is: the history topic reports nothing at all rather than
	// reporting absences, because a flag the operator passed is not a fact about the repository.
	pr := practice.Analyse(practice.Input{
		Discovered: a.Discovered,
		Manifests:  a.Manifests,
		History:    a.History,
	})
	opts.Practices = pr.Render()
	return pr
}

// reportPractices says how many declarations the page records and how many absences.
//
// Two counts and nothing derived from them. A percentage or a ratio here would be the score
// design §9.1 rules out, arriving through the CLI instead of through the page — and the
// absences are what the reader should open the page for, not a number to watch go up.
func reportPractices(p *printer, r *practice.Result) {
	p.printf("practices: %d declared, %d not declared\n", r.Declared(), r.Absent())
}

// reportSemantic says what the pass did and what it could not do.
func reportSemantic(p *printer, r *semantic.Result) {
	p.printf("semantic pass: %d module(s) summarised (%d from cache, %d call(s), "+
		"%d tokens in, %d out)\n",
		len(r.Summaries), r.Cached, r.Calls, r.InputTokens, r.OutputTokens)
	if r.Truncated > 0 {
		p.printf("  %d summarised from part of the module: the input caps applied\n", r.Truncated)
	}
	for _, s := range r.Skipped {
		// Printed as-is. Every entry semantic.Run writes is already a full sentence naming
		// what it did not do — "X not summarised: …", "the semantic pass stopped at X: …" —
		// so prefixing them here produced "not summarised: X not summarised: …" and, for the
		// stopped-early and cache-write lines, a prefix that was simply untrue.
		p.printf("  %s\n", s)
	}
}

// agentsPointer is the stub -suggest-agents-md prints.
//
// Three sentences, and the shortness is the design. What the bundle needs from AGENTS.md is
// one thing — the path, stated somewhere a model already reads — and every sentence past that
// is content signpost is asserting about somebody else's repository. A generated paragraph in
// a hand-written file is also the thing a reader deletes, and it takes the pointer with it.
//
// The gap it closes is measured, not assumed. Given the same task in two repositories that
// both had a bundle committed, an agent used it in the one whose instructions named
// .signpost/ and ignored it entirely in the one that did not — re-deriving structure by hand
// from eleven files that was sitting in twenty-eight pages it never opened. Models are
// trained to read README.md and AGENTS.md; nothing trains them to look inside a dot-directory
// they have never heard of.
//
// Markdown, ending in a newline, so appending it to an existing file cannot join the previous
// line. It names index.md rather than the directory because a directory listing is not a
// starting point, and index.md is the page written to be one.
func agentsPointer() string {
	return "## Repository map\n\n" +
		"Read [`" + okf.BundleDir + "/" + okf.IndexPage + "`](" + okf.BundleDir + "/" +
		okf.IndexPage + ") before starting work. It is a compiled map of this repository's " +
		"structure — modules, dependencies, entrypoints, and what couples to what — " +
		"regenerated by [signpost](https://github.com/3rg0n/signpost) and committed, so it " +
		"describes the commit you have checked out. Start there rather than re-deriving the " +
		"layout by reading files.\n"
}

// buildOptions maps the analysis's provenance onto the emitter's.
//
// Both fields come from the commit rather than the clock or the flags, which is what makes a
// re-run at the same commit produce the same bytes. A repository with no readable history
// gets neither, and the emitter omits the frontmatter keys entirely — an absent provenance
// stamp being better than one naming a commit nobody can check.
func buildOptions(a *analysis, repo string) okf.Options {
	var head vcs.Commit
	if a.History != nil && a.History.Available {
		head = a.History.Head
	}
	return okf.Options{
		Actor:    okf.Actor("signpost/" + version),
		Resource: resourceBase(repo, head.SHA),
		Date:     head.Date,
	}
}

// resourceBase builds the `git://` base URI every page's resource extends.
//
// No commit means no resource at all. A URI naming a repository but not a commit would look
// like provenance while carrying none of its value: verify's staleness check is a comparison
// against a sha, and there would be nothing to compare.
func resourceBase(repo, sha string) string {
	if sha == "" {
		return ""
	}
	if repo == "" {
		return "git://" + sha
	}
	return "git://" + repo + "@" + sha
}

// reportBuild says what the run did.
//
// The counts are ordered by what a reader needs to know, not by size. `unchanged` leading on
// a re-run is the signal that byte-stability holds; `preserved` is the number that says the
// compounding mechanism is doing something; and the two lists below are the only things in a
// build that ask for a human's attention.
func reportBuild(p *printer, root string, res *okf.Result) {
	p.printf("%s/%s\n", root, okf.BundleDir)
	p.printf("  %d page(s): %d created, %d updated, %d unchanged\n",
		len(res.Written), res.Created, res.Updated, res.Unchanged)
	if n := len(res.Removed); n > 0 {
		// Named individually rather than folded into the counts above, because this is the one
		// line in a build that reports a file being *deleted*. The counts describe what was
		// written and a reader scanning them would not learn that three files went away; the
		// names are what makes recovering one from git possible.
		p.printf("  %d page(s) removed, their concepts gone and nothing written on them:\n    %s\n",
			n, joinTop(n, 10, func(i int) string { return res.Removed[i] }))
	}
	if res.Preserved > 0 {
		p.printf("  %d page(s) had human notes, carried across\n", res.Preserved)
	}
	if n := len(res.Downgraded); n > 0 {
		// Named individually rather than counted: each one is a page a person reviewed at a
		// commit that is no longer the one being described, so each one is a page for that
		// person to look at again. A count would tell them there is work without saying where.
		p.printf("  %d page(s) verified against an older commit, now marked stale:\n    %s\n",
			n, joinTop(n, 10, func(i int) string { return res.Downgraded[i] }))
	}
	if n := len(res.Stale); n > 0 {
		// Kept because somebody has written on them. See internal/okf's package comment: a
		// renamed directory must not silently remove a page someone put notes on, so the
		// decision is handed back rather than made here. `verify` warns about the same pages.
		p.printf("  %d page(s) describe concepts that no longer exist and have been "+
			"written on, so they were kept:\n    %s\n",
			n, joinTop(n, 10, func(i int) string { return res.Stale[i] }))
	}
}
