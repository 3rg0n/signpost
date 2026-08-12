package main

import (
	"errors"
	"flag"
	"io"

	"github.com/3rg0n/signpost/internal/scaffold"
)

// runInitGitHub writes the workflow that keeps a bundle honest, and the config file that
// names the repository.
//
// **It previews by default and writes only when asked.** The workflow it produces requests
// `contents: write` and pushes to the default branch, which is not something to install
// into somebody's repository on the strength of a command being typed correctly. So the
// bare command prints what it would write and stops; `-y` carries it out.
//
// Preview-by-default rather than an interactive prompt, and that is a decision rather than
// an omission: a prompt needs a terminal, so it either behaves differently under CI and in
// a pipe or it needs TTY detection that tests cannot exercise. Printing and stopping is
// the same guarantee with no hidden state — and it makes the preview useful on its own,
// since `signpost init github >signpost.yml` is a reasonable thing to want.
func runInitGitHub(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("init github", flag.ContinueOnError)
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
		u.printf("usage: signpost init github [flags] [path]\n")
		u.printf("\nWrite the GitHub Actions workflow that rebuilds %s/ on the default\n"+
			"branch and gates pull requests against it, plus a %s naming this\n"+
			"repository.\n", ".signpost", scaffold.ConfigPath)
		u.printf("\nPrints what it would write and stops. Pass -y to write it. Neither file is\n" +
			"ever overwritten: one already there stops the command, because replacing a\n" +
			"workflow somebody tuned with this default would be worse than doing nothing.\n")
		u.printf("\nFlags:\n")
		fs.PrintDefaults()
	}
	write := fs.Bool("y", false, "write the files instead of printing them")
	repo := fs.String("repo", "", "the name pages record in `resource:` (default: read from the git remote)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	path, err := repoPath(fs)
	if err != nil {
		return err
	}

	plan, err := scaffold.PlanGitHub(path, *repo)
	if err != nil {
		return err
	}
	p := newPrinter(out)

	// Blocked is reported before the preview rather than after it. The answer to "what
	// would this do" is "nothing", and printing 190 lines of workflow first would bury
	// that under the file it is not going to write.
	if blocked := plan.Blocked(); len(blocked) > 0 {
		for _, path := range blocked {
			p.printf("%s is already here; not touching it\n", path)
		}
		p.printf("\nNothing written. Remove or rename %s to scaffold %s, or edit\n"+
			"what is there — this command has no opinion about a file it did not write.\n",
			pluralFiles(len(blocked)), pluralThem(len(blocked)))
		if err := p.Err(); err != nil {
			return err
		}
		// Exit 0. The files being present is a state somebody can be in legitimately, and
		// a scaffold that fails when the thing already exists is one a script has to
		// guard.
		return nil
	}

	if !*write {
		for i, f := range plan.Files {
			if i > 0 {
				p.printf("\n")
			}
			// The path on its own line, commented, so that redirecting the output into a
			// file leaves something valid: both files are YAML, and a bare path would be
			// a syntax error at the top of one.
			p.printf("# %s\n%s", f.Path, f.Contents)
		}
		p.printf("\n# Not written. Run `signpost init github -y` to write %s.\n",
			pluralThem(len(plan.Files)))
		return p.Err()
	}

	if err := scaffold.Apply(path, plan); err != nil {
		// ErrExists is possible here despite the check above — something can appear
		// between the plan and the write — and it is not this command's job to race it.
		if errors.Is(err, scaffold.ErrExists) {
			return err
		}
		return err
	}
	for _, f := range plan.Files {
		p.printf("wrote %s\n", f.Path)
	}
	reportRepo(p, plan)
	p.printf("\nCommit both, then push: the workflow writes the first bundle on the default\n" +
		"branch. Nothing is committed here — that is yours to review first.\n")
	return p.Err()
}

// runInitPages writes the workflow that publishes the graph to GitHub Pages.
//
// Separate from `init github` deliberately, and the separation is the design rather than
// tidiness. `init github` commits a map into a repository, where the audience is whoever
// can already read the repository. This publishes the module names, file paths, and
// ownership signals in that map at a URL. Those are different consequences, and folding
// the second into "set up CI" would do it to somebody who did not realise.
//
// **There is no API call, and the visibility consequence is stated instead.** Refusing
// unless `repos/{owner}/{repo}` confirms a site would be private was the obvious design
// and is not this one, for two reasons. It would make `init` the only command that
// touches the network, against the whole posture of the tool (ADR 0028). And it would be
// a gate in appearance only: `actions/configure-pages` cannot enable Pages with
// GITHUB_TOKEN, so the scaffolded workflow cannot turn publishing on — somebody has to
// change a repository setting, which is where the decision actually gets made. What is
// left to do is make sure they know what they are deciding, so it is said here and in the
// file's own comments.
func runInitPages(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("init pages", flag.ContinueOnError)
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
		u.printf("usage: signpost init pages [flags] [path]\n")
		u.printf("\nWrite the GitHub Actions workflow that publishes this repository's graph to\n"+
			"GitHub Pages. One file, %s; nothing else, and\n"+
			"no %s — the deploy describes whatever tree it checks out.\n",
			scaffold.PagesPath, scaffold.ConfigPath)
		u.printf("\nWhat gets published is every module name, every file path, and who has been\n" +
			"touching them. Whether that URL is private depends on your plan and on who owns\n" +
			"the repository, not on this file: a private site needs GitHub Enterprise Cloud,\n" +
			"and access control covers only project sites from organization-owned private\n" +
			"repositories. A personal account's private repository publishes a site anyone\n" +
			"can read.\n")
		u.printf("\nThis workflow cannot switch Pages on — that needs a token GitHub Actions is\n" +
			"not given — so nothing is published until you choose \"GitHub Actions\" as the\n" +
			"source in Settings → Pages. Check what visibility you actually get there.\n")
		u.printf("\nPrints what it would write and stops. Pass -y to write it. An existing\n" +
			"pages.yml is never overwritten.\n")
		u.printf("\nFlags:\n")
		fs.PrintDefaults()
	}
	write := fs.Bool("y", false, "write the file instead of printing it")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	path, err := repoPath(fs)
	if err != nil {
		return err
	}

	plan, err := scaffold.PlanPages(path)
	if err != nil {
		return err
	}
	p := newPrinter(out)

	if blocked := plan.Blocked(); len(blocked) > 0 {
		for _, path := range blocked {
			p.printf("%s is already here; not touching it\n", path)
		}
		p.printf("\nNothing written. A deploy workflow somebody tuned is not one to replace\n" +
			"with this default — remove or rename it to scaffold, or edit what is there.\n")
		if err := p.Err(); err != nil {
			return err
		}
		// Exit 0, the same reason `init github` does: the file being present is a state
		// somebody can legitimately be in.
		return nil
	}

	if !*write {
		p.printf("# %s\n%s", plan.Files[0].Path, plan.Files[0].Contents)
		p.printf("\n# Not written. Run `signpost init pages -y` to write it.\n")
		return p.Err()
	}

	if err := scaffold.Apply(path, plan); err != nil {
		return err
	}
	p.printf("wrote %s\n", plan.Files[0].Path)
	// Said again, after the file exists, because this is the point at which somebody is
	// about to commit it. The help text is read by people deciding whether to run the
	// command; this is read by the person who just did.
	p.printf("\nNothing is published yet. The workflow cannot enable Pages itself, so it does\n" +
		"nothing until you set Settings → Pages → Source to \"GitHub Actions\".\n")
	p.printf("\nBefore you do: the site lists every module and file path in this repository,\n" +
		"and who has been changing them. A private site requires GitHub Enterprise Cloud\n" +
		"and an organization-owned repository — a personal account's private repository\n" +
		"publishes a site anyone can read. Check which case you are in on that settings\n" +
		"page, where GitHub states it.\n")
	return p.Err()
}

// reportRepo says what name the config file ended up with, and where it came from.
//
// A derived name is stated rather than assumed correct. #31 established the reason: a git
// remote is a property of the checkout, and a fork's remote names the upstream — so a
// derived value is a proposal the reader has to agree with. Saying nothing here would make
// the one guess this command makes the one thing it does not mention.
func reportRepo(p *printer, plan scaffold.Plan) {
	switch {
	case plan.Repo == "":
		p.printf("\nNo repository name: nothing here could work one out, and it is not\n" +
			"read from your git remote because a remote names your checkout rather than\n" +
			"the repository being described. Pages will carry a commit-only resource,\n" +
			"which still says whether a page describes the code in front of you. Set\n" +
			"`repo:` when you want the full URI.\n")
	case plan.Derived:
		p.printf("\n%s says `repo: %s`, read from your `origin` remote. Check it: a\n"+
			"fork's remote names the upstream, and that name is what every page's\n"+
			"`resource:` will claim to describe.\n", scaffold.ConfigPath, plan.Repo)
	default:
		p.printf("\n%s says `repo: %s`.\n", scaffold.ConfigPath, plan.Repo)
	}
}

// pluralFiles and pluralThem keep the two messages above readable in both cases. Written
// out rather than an `if n == 1` at each site, because there are four of them and the
// version with the conditional inline is what makes a message read like a template.
func pluralFiles(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func pluralThem(n int) string {
	if n == 1 {
		return "it"
	}
	return "both"
}
