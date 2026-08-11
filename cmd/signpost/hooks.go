package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/3rg0n/signpost/internal/config"
	"github.com/3rg0n/signpost/internal/hook"
)

// runHooksInstall adds the post-commit hook.
//
// The output is longer than an installer's usually is, and deliberately: on a machine with a
// global `core.hooksPath` this appends to a file every repository on that machine shares, and
// somebody who does not know they have such a setting must not find out from a hook firing in
// an unrelated repository. So the successful case names the path, says where the setting came
// from, and says how to undo it.
func runHooksInstall(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("hooks install", flag.ContinueOnError)
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
		u.printf("usage: signpost hooks install [flags] [path]\n")
		u.printf("\nAdd a post-commit hook that reports when %s/ has fallen behind the\n"+
			"code. The hook never fails a commit — CI is what gates (see CONTRIBUTING).\n",
			hook.BundleDir)
		u.printf("\nThe hook is appended to any post-commit hook already there, guarded so\n" +
			"that it does nothing in a repository without a bundle and nothing on a\n" +
			"machine without signpost installed. Remove it with `signpost hooks\n" +
			"uninstall`.\n")
		// No `Flags:` heading and no PrintDefaults: this command has no flags, and an empty
		// section under a heading reads as help that failed to print rather than as a command
		// that takes nothing.
	}
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	path, err := repoPath(fs)
	if err != nil {
		return err
	}

	res, err := hook.Install(context.Background(), path)
	if err != nil {
		return err
	}
	p := newPrinter(out)
	switch {
	case res.AlreadyPresent:
		p.printf("already installed: %s\n", res.Path)
	case res.Created:
		p.printf("installed: %s\n", res.Path)
	default:
		p.printf("installed: %s (appended; what was already there is unchanged)\n", res.Path)
	}
	reportHooksPath(p, res.Paths)
	p.printf("Remove it with `signpost hooks uninstall`.\n")
	return p.Err()
}

// runHooksUninstall removes the block and nothing else.
func runHooksUninstall(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("hooks uninstall", flag.ContinueOnError)
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
		u.printf("usage: signpost hooks uninstall [flags] [path]\n")
		u.printf("\nRemove signpost's lines from the post-commit hook. Anything else in that\n" +
			"file is left alone, and the file itself is removed only if signpost's lines\n" +
			"were all of it.\n")
		// No flags, so no heading. See runHooksInstall.
	}
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	path, err := repoPath(fs)
	if err != nil {
		return err
	}

	res, err := hook.Uninstall(context.Background(), path)
	if err != nil {
		return err
	}
	p := newPrinter(out)
	switch {
	case res.FileRemoved:
		p.printf("removed: %s\n", res.Path)
	case res.Removed:
		p.printf("removed signpost's lines from %s (the rest of the file is unchanged)\n", res.Path)
	default:
		// Exit 0. Nothing to remove is the state the user asked for, and an installer that
		// fails when the thing is already absent is one a script has to guard.
		p.printf("nothing to remove: %s has no signpost hook\n", res.Path)
	}
	return p.Err()
}

// reportHooksPath says where git will read the hook from, and warns when that is a file
// shared with other repositories.
//
// The warning is the whole reason this function exists. `core.hooksPath` set globally means
// git reads that one directory for every repository on the machine and ignores `.git/hooks`
// entirely (git-lfs/git-lfs#3240), so signpost has just edited a file that is not only this
// repository's. The guards in the block mean the effect elsewhere is nil, and the person is
// still owed the fact.
func reportHooksPath(p *printer, paths *hook.Paths) {
	if !paths.Redirected {
		return
	}
	p.printf("\ncore.hooksPath is set (%s), so git reads hooks from there and ignores\n"+
		".git/hooks. That is where this went.\n", paths.Scope)
	if !paths.Shared {
		return
	}
	p.printf("\nThat directory is outside this repository, so the file is shared with every\n" +
		"repository on this machine. The lines added do nothing in a repository that has\n" +
		"no bundle, and nothing where signpost is not installed.\n")
}

// runHooksRun is what the installed hook calls.
//
// It exits 0 whatever it finds. That is not a verification result being swallowed — the gate
// is CI, which runs `signpost verify` and fails on it. This is a reminder printed after a
// commit that already happened, and a reminder that can break `git commit` is one that gets
// uninstalled by lunchtime.
func runHooksRun(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("hooks run", flag.ContinueOnError)
	help := helpStream(args, out, errOut)
	fs.SetOutput(help)
	fs.Usage = func() {
		u := newPrinter(help)
		u.printf("usage: signpost hooks run [flags] [path]\n")
		u.printf("\nReport whether %s/ has fallen behind the code. Called by the post-commit\n"+
			"hook; useful by hand too. Always exits 0 — `signpost verify` is the check\n"+
			"that fails.\n", hook.BundleDir)
		u.printf("\nChecks:\n")
		u.printf("  fast    compare commits only. Milliseconds, and reports a code commit\n" +
			"          the bundle does not cover even when no page would change.\n")
		u.printf("  verify  run the same comparison `signpost verify -as-of-bundle` runs.\n" +
			"          Reports the pages that would actually change, and costs about a\n" +
			"          second.\n")
		u.printf("\nSet %s to change the default for one invocation, or a `hooks.check`\n"+
			"key in %s to set it for the repository. This flag beats both.\n",
			hook.EnvCheck, config.File)
		u.printf("\nFlags:\n")
		fs.PrintDefaults()
	}
	// The flag's default is empty rather than "fast", so that an unset flag and `-check fast`
	// are distinguishable: precedence is flag > environment > default (ADR 0011), and a flag
	// defaulted to "fast" would silently outrank the environment variable.
	check := fs.String("check", "",
		"what to check: fast (commits only) or verify (accurate, ~1s)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	path, err := repoPath(fs)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}
	mode, err := resolveCheck(*check, os.Getenv(hook.EnvCheck), cfg.HooksCheck)
	if err != nil {
		// A misuse, and the one thing here that is worth an error: a mode nobody recognises
		// means the check the user configured is not running, and falling back silently
		// would report "current" from a check that never ran.
		return fmt.Errorf("%w: %v", errUsage, err)
	}

	if mode == hook.CheckVerify {
		return hooksRunVerify(path, out, errOut)
	}
	st, err := hook.Fast(context.Background(), path)
	if err != nil {
		return err
	}
	reportHookStatus(newPrinter(out), st)
	return nil
}

// resolveCheck applies ADR 0011's precedence: flag, then environment, then the file, then the
// default.
//
// The first non-empty value wins and is parsed; a later layer is never consulted to recover
// from an earlier one being wrong. That matters for the file layer specifically — config.Load
// has already rejected an unparseable mode, so a value reaching here is valid, and a fallback
// would only exist to paper over a bug.
func resolveCheck(flagValue, envValue, fileValue string) (hook.Check, error) {
	for _, v := range []string{flagValue, envValue, fileValue} {
		if strings.TrimSpace(v) == "" {
			continue
		}
		return hook.ParseCheck(v)
	}
	// Fast by default because this runs on every commit. The accurate check is a second on
	// this repository, and a second on every commit is what makes a hook an irritation.
	return hook.CheckFast, nil
}

// reportHookStatus prints the one line the hook exists to print, or nothing.
//
// Nothing is the common case and has to stay silent. A hook that printed "bundle is current"
// after every commit would be noise on every commit, and noise is what gets a hook removed.
func reportHookStatus(p *printer, st hook.Status) {
	if st.Reason != "" {
		p.printf("signpost: %s\n", st.Reason)
		return
	}
	if !st.Stale() {
		return
	}
	p.printf("signpost: %s/ is behind by %d commit(s) — run `signpost build` and commit %s/\n",
		hook.BundleDir, st.Behind, hook.BundleDir)
}

// hooksRunVerify runs the real verify and turns its result into a reminder.
//
// It calls runVerify rather than reimplementing the comparison, which is the point: a second
// implementation of "is the bundle current" would eventually disagree with the one CI gates
// on, and a hook that disagrees with the gate is worse than no hook. What differs is only
// what happens to the answer — verify's exit code becomes a printed line, and its report goes
// to stderr where the hook's own output is.
//
// -as-of-bundle, because the alternative is unusable here. A strict verify after any code
// commit compares against a sha that does not exist yet and calls almost every page stale;
// measured on this repository it reports 38 problems where -as-of-bundle reports the 1 that
// is real.
//
// Pending is a reminder here and silence in CI, from the same run, and the asymmetry is the
// point rather than an exception to it. ADR 0027 keeps a difference out of CI's exit code when
// the only thing that resolves it is the rebuild after the merge — nothing the author of a pull
// request is permitted to do. On this machine, right after a commit, there is no merge and no
// push job: `signpost build` is the remedy and the person reading this line is the one who runs
// it. Same comparison, same severity, opposite audience.
func hooksRunVerify(path string, out, errOut io.Writer) error {
	err := runVerify([]string{"-as-of-bundle", "-quiet", path}, errOut, errOut)
	p := newPrinter(out)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, errStale), errors.Is(err, errPending):
		p.printf("signpost: %s/ does not match this tree — run `signpost build` and commit %s/\n",
			hook.BundleDir, hook.BundleDir)
		return p.Err()
	}
	// Something else went wrong: no git, an unreadable tree, a malformed bundle. Reported and
	// not returned, because this is a post-commit reminder and the commit already happened.
	p.printf("signpost: the bundle could not be checked: %v\n", err)
	return p.Err()
}
