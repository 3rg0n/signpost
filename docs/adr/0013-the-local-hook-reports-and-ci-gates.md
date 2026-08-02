# 13. The local hook reports, CI gates, and the hook is a guest in somebody else's file

## Status

Accepted

## Decision

`signpost hooks install` writes a `post-commit` hook. Four clauses, each of which a later
change will want to violate for a reasonable-sounding reason:

1. **The hook reports and never gates.** It prints at most one line and exits 0 whatever it
   finds, including on its own failures. `signpost verify` in CI is the only thing that fails
   a build over a stale bundle.
2. **The hook never writes.** It does not rebuild the bundle, and it is `post-commit` rather
   than `pre-commit`, so it cannot amend or block what was committed.
3. **Install appends a marked block; uninstall removes exactly that block.** Never a whole
   file over an existing one, and never a whole file removed when signpost's lines were not
   all of it.
4. **The hook goes where git actually looks.** `git rev-parse --git-path hooks`, which honours
   `core.hooksPath` at whatever scope it was set, rather than `.git/hooks` unconditionally.

Clause 1 is why the thing is safe to install at all; clause 4 is the one that is easy to get
wrong and looks like it worked.

## Context

The bundle is committed (ADR 0005) and names the commit it describes (ADR 0007), so a
developer building locally can commit code and forget the bundle. That is a real annoyance
worth a reminder — and a reminder is the largest thing it is worth, because every stronger
option has a failure mode that costs more than the annoyance.

**A hook that gates gets uninstalled.** The bundle is optional, the tool is optional, and a
`post-commit` hook that reddens a terminal after every commit — or a `pre-commit` one that
refuses a commit over a knowledge artifact — is removed within a day, and takes the reminder
with it. Design line 1304 prohibits "a hook that exits zero on failure", which reads like it
forbids clause 1 and does not: it forbids masking a *verification* failure as success in the
**gate**, and the gate is CI. The hook is not a gate, which is the distinction this ADR
exists to fix in place.

**A hook that rebuilds recreates the merge pain §8.0 avoids.** The bundle is built only on the
default branch, deliberately, because two branches both regenerating `.signpost/` is what
makes merges painful. A `pre-commit` rebuild reintroduces exactly that on every commit on
every branch, and does it silently.

**`core.hooksPath` means git looks in exactly one place.** When it is set — at *any* scope,
including global — that directory is the only one git reads and `.git/hooks` is ignored
entirely. A tool that writes to `.git/hooks` anyway installs a file that will never run,
which is worse than not installing, because the output says it succeeded. git-lfs settled
this in [git-lfs/git-lfs#3240](https://github.com/git-lfs/git-lfs/issues/3240): it installs
to the resolved path whatever the scope, on the grounds that otherwise git-lfs would not work
at all, and the escape hatch for a user who dislikes that is to set `core.hooksPath` locally.

**Which means the file is usually not ours.** The machine this was developed on has a global
`core.hooksPath` already holding a git-lfs `post-commit` shared by every repository on it.
Writing a file there is not an option; appending to one is what git-lfs itself does.

**Accuracy and cost pull opposite ways.** Two candidate checks, both measured on this
repository: comparing the newest code commit against the newest `.signpost/` commit costs two
`git log -1` calls and milliseconds, and reports a code commit the bundle does not cover even
where no page would change. `verify -as-of-bundle` reports the pages that would actually
change and costs about a second. On this repository, after a code commit, `-as-of-bundle`
found the 1 real problem where a strict `verify` reported 38 — so the accurate check has to
be the `-as-of-bundle` comparison, not the strict one. `-no-history` cannot be used to make
it cheaper: git signals are page content, so dropping them produced 16 false positives.

## Consequences

**The hook is not in the gate, and CONTRIBUTING says so.** A contributor who never runs
`hooks install` is not disadvantaged, and a contributor who does is not more trusted. The
alternative — a hook that is expected — is a gate enforced on machines nobody can inspect,
which is not a gate.

**A stale-bundle commit still lands, locally.** Accepted. The pull request is where it is
caught, by `verify -as-of-bundle`, and the local reminder is what makes the developer fix it
before opening one rather than after.

**The check mode is configurable, defaulting to the cheap one.** `fast` by default because
this runs on every commit; `-check verify` and `SIGNPOST_HOOK_CHECK=verify` for anyone who
wants the accurate answer. A `hooks.check` key in `.signpost.yml` will set it per repository
— which ADR 0011 permits precisely because it changes a default and not whether a check
fails. The default is a documented inaccuracy: `fast` reports a commit that touched only
`LICENSE` as behind. Saying so is the price of it being fast enough not to be removed.

**The accurate mode calls `verify`, not a second implementation of it.** `hooks run -check
verify` invokes the same code path CI gates on. A separate comparison would eventually
disagree with the gate, and a hook that disagrees with the gate is worse than no hook.

**Install edits a file other tools own, so both markers are a contract.** `# >>> signpost >>>`
and `# <<< signpost <<<` are what make removal exact. They are full-line comments so a person
reading a shared hook can see what owns those lines. Three guards keep the block inert
elsewhere: `[ -d .signpost ]`, `command -v signpost`, and a trailing `|| true`. The first is
what makes appending to a machine-wide hook defensible at all — every repository without a
bundle behaves exactly as before.

**Install output is longer than an installer's usually is.** When `core.hooksPath` is set it
names the path, the scope it came from, and that the file may be shared. Somebody who did not
know they had that setting must not discover it from a signpost line appearing in an unrelated
repository.

**This needs a test that runs a real `git commit`.** Clause 1 and clause 4 are both invisible
from a terminal: a non-zero `hooks run` puts a red line in a shell that nobody reads the exit
code of, and a hook installed in the wrong directory simply never fires. So the test drives an
actual commit with a stub on `PATH`, asserting the stub ran, its output reached the terminal,
and the commit was created anyway — mode, shebang, location, Git-for-Windows `sh` dispatch,
and the never-gates property in one assertion. CI repeats the exit-0 half against the built
binary for both check modes.

**Clause 1 will be argued against.** It will arrive as "a `pre-commit` hook would stop the bad
commit from ever existing." It would, and it would also rebuild the bundle on a branch, block
a commit somebody needed to make with a dirty tree, and be switched off by the third person it
inconvenienced. The optional local convenience does not get to be the enforcement point.

## Notes

git ignores a `post-commit` hook's exit code entirely — verified, not assumed, with a stub
that exits 7. So clause 1 is about the *message* rather than the status: nothing this hook does
can stop a commit. Exiting 0 anyway is because a non-zero status from a hook surfaces in some
porcelain and in people's shells for no reason, and because the block runs under whatever
wrapping script the shared hook is, which may be under `set -e`.

Command reference: [docs/design.md](../design.md) §6.0.1.
