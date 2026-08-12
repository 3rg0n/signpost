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
