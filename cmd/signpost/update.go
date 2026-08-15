package main

import (
	"flag"
	"fmt"
	"io"
	"runtime/debug"

	"github.com/3rg0n/signpost/internal/selfupdate"
)

// `signpost update` replaces this binary with the latest published release.
//
// The reason to have it at all is the failure this tool actually produces when it is
// stale, which is not a version number anybody looks at: it is `signpost: unknown command
// "view"`, which reads as a missing feature rather than an old install. `version` was made
// to say otherwise (see version.go); this is the other half — the reader who now knows
// their binary is from a fortnight ago should not have to go and find the install command
// again, in a README, in a browser, while in the middle of something else.
//
// It does not add a distribution channel. The artifacts are GitHub Releases, which is what
// `install.sh` and `install.ps1` already read, and internal/selfupdate performs the same
// transaction with the same refusals: no checksums.txt, or an asset not listed in it, or a
// digest that does not match, and nothing is written. There is no auto-update, no
// background check, and nothing phones home — this runs when it is typed and at no other
// time. A tool that silently changed its own behaviour between two runs of a build would
// be a worse thing than a stale one.
//
// What it will not do is escalate. A binary in /usr/local/bin owned by root fails with the
// permission error and a pointer back to the installer, because a tool that acquired
// privilege to overwrite itself is a pattern nobody should have to trust.
func runUpdate(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
		u.printf("usage: signpost update [flags]\n")
		u.printf("\nReplace this binary with a release published at\n%s.\n", selfupdate.ReleasesURL())
		u.printf("\nThe archive's sha256 is checked against the release's checksums.txt before\n" +
			"anything is written. A release with no checksums, an asset missing from them,\n" +
			"or a digest that does not match all stop the update — none of them is a\n" +
			"warning, and the binary you have is left alone.\n")
		u.printf("\nThe new binary is written beside the old one and renamed over it, so an\n" +
			"interrupted update leaves you with a working signpost. Nothing is checked\n" +
			"unless you run this: there is no background check and no auto-update.\n")
		u.printf("\nInstalling into a directory that needs elevated permission is not\n" +
			"supported — rerun the installer from the README instead.\n")
		u.printf("\nFlags:\n")
		fs.PrintDefaults()
	}
	var (
		// Not `version`, which is the package-level link-time variable this file also
		// reads: a local of that name shadows it, and Current would then carry the flag's
		// empty default instead of the build's tag — so every run would look like a
		// version mismatch and reinstall.
		tag    = fs.String("version", "", "install this release tag instead of the latest (v0.2.0)")
		dryRun = fs.Bool("dry-run", false,
			"resolve and verify the release, report what would change, write nothing")
		force = fs.Bool("force", false,
			"reinstall even when the version already matches, for a binary with the wrong bytes")
		path = fs.String("path", "",
			"replace this file instead of the running binary")
	)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		// Named explicitly, because every other verb in this CLI takes a repository path
		// as its argument and somebody will type one here out of habit. An update has
		// nothing to do with a repository, and silently ignoring the argument would leave
		// them thinking they had updated something scoped to that tree.
		return fmt.Errorf("%w: update takes no positional arguments — it replaces this "+
			"binary, which has nothing to do with a repository. Use -path to replace a "+
			"different file", errUsage)
	}

	info, ok := debug.ReadBuildInfo()
	c := &selfupdate.Client{}
	res, err := c.Apply(selfupdate.Options{
		Version: *tag,
		Path:    *path,
		// The injected tag, not versionString's output: that string carries provenance in
		// parentheses — "dev (a1b2c3d, 2026-08-14)" — and comparing it to a tag would
		// never match, so a development build would download and reinstall on every run.
		Current: version,
		DryRun:  *dryRun,
		Force:   *force,
	})
	if err != nil {
		return err
	}
	return reportUpdate(out, res, *dryRun, versionString(version, info, ok))
}

// reportUpdate writes what an update did.
//
// Split from runUpdate so its four outcomes can be tested without a network: every one of
// them is a different sentence, and the case that reads wrong in production is not the
// happy path but the two quiet ones — already current, and a dry run. Reaching those
// through the command means an httptest server per branch and no coverage of the branch
// that only a released binary can produce.
//
// current is versionString's output, used only in prose. The comparison that decides
// anything happened at all is Result's, made against the link-time tag.
func reportUpdate(out io.Writer, res selfupdate.Result, dryRun bool, current string) error {
	p := newPrinter(out)
	switch {
	case dryRun && res.SHA256 == "":
		// A dry run that never got as far as a download, because the binary is already at
		// the version and -force was not given: Apply compares before fetching, so there
		// is no digest to report. Printing the usual two lines here claimed `sha256
		// verified:` with nothing after it — a verification that did not happen, stated in
		// the output of the command whose whole purpose is to say what was verified.
		p.printf("would do nothing: already at %s\n", res.To)
		p.printf("(add -force to reinstall it anyway)\n")
	case dryRun:
		p.printf("would install %s over %s\n", res.To, res.Path)
		p.printf("sha256 verified: %s\n", res.SHA256)
		// Reached by `-dry-run -force`, or by `-dry-run -version` naming the version
		// already installed. Without this line the output reads as an upgrade being
		// available when what is available is the same release again.
		if res.From == res.To {
			p.printf("(this binary already reports %s)\n", res.From)
		}
	case !res.Replaced:
		// The common outcome for anybody who runs this on a schedule, and it says which
		// version rather than only "up to date": a reader who suspected they were stale
		// needs the number to be sure the answer is about the binary they just ran.
		p.printf("already at %s; nothing to do\n", res.To)
	case res.From == devVersion:
		// A development build has no tag to compare, so "updated from dev" would imply a
		// downgrade might have happened. Said plainly instead.
		p.printf("installed %s over %s\n", res.To, res.Path)
		p.printf("this replaced a build that reported %q\n", current)
	case res.From == res.To:
		// -force on a binary already at the version: the repair case, for the right
		// version and the wrong bytes. Said as a reinstall, because "updated v0.1.0 to
		// v0.1.0" describes a change of version that did not happen and reads as a bug in
		// the tool rather than as the thing the reader asked for.
		p.printf("reinstalled %s at %s\n", res.To, res.Path)
	default:
		p.printf("updated %s to %s\n", res.From, res.To)
		p.printf("installed at %s\n", res.Path)
	}
	// Only where something did change. A forced reinstall of the version already installed
	// wrote new bytes and no new behaviour, so release notes are an answer to a question
	// nobody asked — and offering them implies the reinstall brought something with it.
	if res.Replaced && res.From != res.To {
		p.printf("What changed: %s/tag/%s\n", selfupdate.ReleasesURL(), res.To)
	}
	return p.Err()
}
