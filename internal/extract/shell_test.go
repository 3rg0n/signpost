package extract

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func shFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangShell, Class: discover.ClassSource, Content: src,
	}
}

func extractShell(t *testing.T, path, src string) Facts {
	t.Helper()
	fa, err := ShellExtractor{}.Extract(shFile(path, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Hand-labeled against real shell, including the forms no other
// language here has: a `$(dirname "$0")`-anchored source, both spellings of the source
// builtin, both function syntaxes, and a heredoc whose body is indistinguishable from
// code.
func shellCorpus() []Fixture {
	return []Fixture{
		{
			File: shFile("scripts/deploy.sh", `#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/log.sh"
. "$(dirname "$0")/lib/aws.sh"
source scripts/lib/retry.sh

readonly DEFAULT_REGION="us-east-1"
export DEPLOY_ENV="${DEPLOY_ENV:-staging}"
declare -r MAX_ATTEMPTS=3

# Pushes the built image to the registry.
# Takes the tag as its only argument.
push_image() {
  local tag="$1"
  local scratch="/tmp/push"
  log_info "pushing $tag"
  retry 3 docker push "$tag"
}

function rollback {
  local previous
  previous="$(aws_last_good)"
  log_warn "rolling back to $previous"
}

# Internal: not part of the interface a caller sees.
_cleanup() {
  rm -rf /tmp/push
}

main() {
  push_image "$1" || rollback
  _cleanup
}

main "$@"
`),
			Expected: Expected{
				Imports: []string{
					"./lib/aws.sh", "./lib/log.sh", "scripts/lib/retry.sh",
				},
				Symbols: []string{
					"DEFAULT_REGION", "DEPLOY_ENV", "MAX_ATTEMPTS", "_cleanup", "main",
					"push_image", "rollback",
				},
				// `local tag` and `local scratch` are absent, which is the whole point of
				// excluding the one scoping keyword the shell has: they are a function's
				// internals, not the script's surface. `_cleanup` is present as a symbol and
				// absent from the surface, which is the underscore convention.
				Exported: []string{
					"DEFAULT_REGION", "DEPLOY_ENV", "MAX_ATTEMPTS", "main", "push_image",
					"rollback",
				},
				Entrypoints: []string{"#!"},
			},
		},
		{
			// A sourced library: no shebang, because it is not executable, which is the
			// distinction the entrypoint records. Hyphenated and colon-qualified names are
			// legal shell function names and appear in real scripts.
			File: shFile("scripts/lib/log.sh", `# Logging helpers. Sourced, never executed.

LOG_LEVEL="${LOG_LEVEL:-info}"

log_info() {
  printf '[info] %s\n' "$*"
}

log-warn() {
  printf '[warn] %s\n' "$*" >&2
}

log::error() {
  printf '[error] %s\n' "$*" >&2
  return 1
}

_fmt() {
  date -u '+%Y-%m-%dT%H:%M:%SZ'
}
`),
			Expected: Expected{
				Imports: []string{},
				// LOG_LEVEL is a plain assignment with no declaring keyword, so it is
				// deliberately not a symbol: recording every assignment would bury the
				// declared ones under the incidental.
				Symbols:  []string{"_fmt", "log-warn", "log::error", "log_info"},
				Exported: []string{"log-warn", "log::error", "log_info"},
				// No shebang. A library meant to be sourced has none, and the absence is a
				// real fact about how the file is used.
				Entrypoints: []string{},
			},
		},
		{
			// The adversarial fixture. Every shape that looks like a declaration and is not:
			// a case pattern, a subshell, an array assignment, a heredoc body, a comment, a
			// quoted string, and an append redirection whose `<<` mirror would blank the
			// rest of the file.
			File: shFile("scripts/tricky.sh", `#!/bin/sh
# source ghost/commented.sh

real_function() {
  # A case statement's patterns carry a definition's punctuation and are not one.
  case "$1" in
    *.sh)
      echo "shell"
      ;;
    (deploy)
      echo "deploy"
      ;;
    *)
      echo "other"
      ;;
  esac

  # An array assignment has a definition's exact shape.
  items=(alpha beta gamma)

  # A subshell is parenthesised and declares nothing.
  ( cd /tmp && ls )

  # A quoted string holding what looks like source.
  echo "source ghost/quoted.sh"
  echo 'ghost_quoted() { :; }'

  # A dynamically built path names a file nothing here can know.
  source "$config_dir/$name.sh"
  . "${plugin}"

  # An append redirection, not a heredoc. Reading it as one blanks every line below.
  echo "line" >>/tmp/out.log

  cat <<'MANIFEST' >/tmp/manifest
source ghost/heredoc.sh
ghost_heredoc() {
  echo "not code"
}
MANIFEST

  cat <<-INDENTED
	ghost_indented() { :; }
	INDENTED
}

after_the_traps() {
  echo reached
}
`),
			Expected: Expected{
				// Only the literal path is a fact. The two computed sources name files this
				// extractor cannot know, and inventing them would put modules in the graph
				// that nothing declares.
				Imports:     []string{},
				Symbols:     []string{"after_the_traps", "real_function"},
				Exported:    []string{"after_the_traps", "real_function"},
				Entrypoints: []string{"#!"},
			},
		},
		{
			// An extensionless executable, which is what a script on the PATH looks like.
			// This fixture hands the extractor one directly, and in a real run nothing
			// would: classification is filename-only by design, so a file with no
			// extension and no known basename is ClassOther and no extractor is offered it
			// (classify.go). The corpus states that limitation as a counted unclassified
			// file, `shell/release`. What is asserted here is that the reading is correct
			// if a shebang rule ever supplies one — this is the extractor half of a gap
			// whose other half is in discover, and the two are worth landing separately.
			//
			// Its source has no extension either, which is the case the shell's
			// literal-path rule exists for: unlike Ruby's require, `source` does no
			// extension search.
			File: shFile("bin/release", `#!/bin/bash
set -e

. "$(dirname "$0")/../scripts/lib/log.sh"

export RELEASE_CHANNEL=stable

typeset -r BUILD_ID="${CI_RUN_ID:-local}"

tag_release() {
  git tag -a "v$1" -m "release $1"
}

tag_release "$1"
`),
			Expected: Expected{
				Imports:     []string{"./../scripts/lib/log.sh"},
				Symbols:     []string{"BUILD_ID", "RELEASE_CHANNEL", "tag_release"},
				Exported:    []string{"BUILD_ID", "RELEASE_CHANNEL", "tag_release"},
				Entrypoints: []string{"#!"},
			},
		},
	}
}

// The measurement design §4.2 promises for shell.
func TestShellExtractorMeetsTarget(t *testing.T) {
	ls := ScoreExtractor(ShellExtractor{}, discover.LangShell, shellCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("shell extractor below target:\n%s", ls.Report())
	}
	t.Logf("shell extractor score:\n%s", ls.Report())
}

// Both spellings of the source builtin, and the two near misses that share the `.`
// character. `./script.sh` runs a script in a subshell — a different relationship, and one
// the graph would misreport as a source edge — and `../lib` is a path fragment.
func TestShellBothSourceSpellingsAndNeitherNearMiss(t *testing.T) {
	fa := extractShell(t, "a.sh", `source lib/one.sh
. lib/two.sh
./lib/three.sh
../lib/four.sh
sourced_thing=5
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "lib/one.sh,lib/two.sh" {
		t.Errorf("imports = %q; only `source x` and `. x` source a file", got)
	}
}

// The anchored form is what a correct script writes, because a bare relative `source`
// resolves against the *invoking* directory rather than the script's. Refusing to read it
// would leave the most careful scripts in a repository looking like they source nothing —
// so the leading expansion is stripped and the remainder marked relative.
func TestShellAnchoredSourceIsReadAsRelative(t *testing.T) {
	fa := extractShell(t, "scripts/a.sh", `source "$(dirname "$0")/lib.sh"
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/other.sh"
source "${BASH_SOURCE%/*}/third.sh"
source "$SCRIPT_DIR/fourth.sh"
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "./fourth.sh,./lib.sh,./other.sh,./third.sh" {
		t.Errorf("imports = %q; every anchor form names the script's own directory", got)
	}
}

// The other half of that rule, and the negative boundary it needs. An expansion in a path
// *segment* names a file nothing can know, and a guess would put a module in the graph no
// file declares.
//
// Both directions are asserted here because the positive test above passes against an
// extractor that strips every `$` and hopes: `"$config_dir/$name.sh"` would become
// `./$name.sh` or `./`, and a resolver that then matched nothing would report a gap rather
// than an error. This test is what distinguishes reading the anchor from ignoring
// expansions.
func TestShellComputedSourcePathIsNotInvented(t *testing.T) {
	fa := extractShell(t, "a.sh", `source "$config_dir/$name.sh"
source "${plugin_dir}/${plugin}.sh"
source "$lib_file"
source "$(basename "$0")"
source lib/real.sh
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "lib/real.sh" {
		t.Errorf("imports = %q; only a path with no unknown segment is a fact", got)
	}
}

// `local` is the shell's one scoping keyword, so a `local` name is the one name that is
// definitely *not* the script's surface. Recording it would report a function's internals
// as its interface — and since a well-written shell function declares every variable
// `local`, that failure would put most of a codebase's temporaries on the page.
func TestShellLocalIsNotSurfaceAndDeclaringKeywordsAre(t *testing.T) {
	fa := extractShell(t, "a.sh", `export PUBLIC_VAR=1
readonly FROZEN=2
declare -r DECLARED=3
typeset -i COUNTER=4

f() {
  local scratch="x"
  local -r pinned="y"
  declare -r inner_declared="z"
  plain_assignment=1
}
`)
	// `declare -r` inside a function body is still a declaring keyword, and the extractor
	// has no scope to tell it otherwise — the shell's own scoping makes this ambiguous, and
	// erring toward recording a `declare` is additive where dropping an `export` is not.
	if got := strings.Join(fa.SymbolNames(), ","); got != "COUNTER,DECLARED,FROZEN,PUBLIC_VAR,f,inner_declared" {
		t.Errorf("symbols = %q; `local` must never appear and a plain assignment must not either", got)
	}
}

// Both function syntaxes, and the keyword's near miss. `functions_list()` begins with the
// letters of `function` and is an ordinary POSIX definition whose name must survive whole.
func TestShellBothFunctionSyntaxes(t *testing.T) {
	fa := extractShell(t, "a.sh", `posix_form() {
  :
}

function bash_form {
  :
}

function bash_form_with_parens() {
  :
}

functions_list() {
  :
}

no_space(){
  :
}
`)
	want := "bash_form,bash_form_with_parens,functions_list,no_space,posix_form"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("symbols = %q, want %q", got, want)
	}
}

// A shell function name admits nearly every character, so the rule is an exclusion rather
// than an allowlist. `pkg::install` and `my-func` are ordinary names in real scripts, and a
// rule built from identifier characters would drop every one of them.
func TestShellFunctionNamesAdmitPunctuation(t *testing.T) {
	fa := extractShell(t, "a.sh", `my-func() { :; }
pkg::install() { :; }
2fa() { :; }
name.with.dots() { :; }
_leading() { :; }
`)
	want := "2fa,_leading,my-func,name.with.dots,pkg::install"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("symbols = %q, want %q", got, want)
	}
	if got := strings.Join(exportedNames(fa), ","); got != "2fa,my-func,name.with.dots,pkg::install" {
		t.Errorf("exported = %q; only the leading underscore says private", got)
	}
}

// The shapes that carry a definition's punctuation and are not one. Each would put a
// function on the page that no caller can invoke, and the case pattern is the common one:
// every non-trivial script has a `case` in it.
func TestShellDefinitionLookalikesDeclareNothing(t *testing.T) {
	fa := extractShell(t, "a.sh", `real() {
  case "$1" in
    *.sh)
      :
      ;;
    (literal)
      :
      ;;
  esac
  items=(a b c)
  ( cd /tmp )
  x=$(compute)
  if [[ $x == *.txt ]]; then :; fi
}
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "real" {
		t.Errorf("symbols = %q; only `real` is a definition", got)
	}
}

// A heredoc's body is data, and shell is the language where heredocs are most common —
// generated manifests, embedded SQL, usage text. A body read as code invents whatever it
// happens to contain.
func TestShellHeredocBodyDeclaresNothing(t *testing.T) {
	fa := extractShell(t, "a.sh", `real() {
  cat <<EOF
source ghost/heredoc.sh
ghost_fn() { :; }
export GHOST_VAR=1
EOF

  cat <<'QUOTED'
ghost_quoted() { :; }
QUOTED

  cat <<-INDENTED
	ghost_indented() { :; }
	INDENTED
}

after() { :; }
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "" {
		t.Errorf("imports = %q; a heredoc body declares no dependency", got)
	}
	if got := strings.Join(fa.SymbolNames(), ","); got != "after,real" {
		t.Errorf("symbols = %q", got)
	}
}

// The negative boundary of the heredoc rule, and in shell it costs more than anywhere else:
// `>>` is the append redirection, which nearly every script uses, and `<<` also appears in
// arithmetic. A scanner treating either as a heredoc opener blanks the remainder of the
// file, and every declaration below vanishes — silently, with no error to read.
//
// The unspaced forms are the ones that hold the guard up. A spaced `$(( x << 2 ))` is
// stopped a step earlier by the requirement that an identifier follow the operator
// immediately, so a test written with spacing alone passes with the uppercase rule deleted.
func TestShellAppendRedirectionIsNotAHeredoc(t *testing.T) {
	fa := extractShell(t, "a.sh", `logger() {
  echo "x" >>/tmp/out.log
  echo "y" >> /tmp/out.log
}

shifter() {
  mask=$((1<<bit))
  other=$(( 1 << 2 ))
}

below() {
  echo reached
}

export MARKER=1
`)
	want := "MARKER,below,logger,shifter"
	if got := strings.Join(fa.SymbolNames(), ","); got != want {
		t.Errorf("symbols = %q, want %q; a `>>` or a shift was read as a heredoc opener, which "+
			"blanks every line below it and takes the declarations there out of the graph",
			got, want)
	}
}

// A comment and a quoted string both hold text that looks exactly like source, and in shell
// the quoted case is routine: a script echoes usage text naming its own functions.
func TestShellCommentsAndStringsDeclareNothing(t *testing.T) {
	fa := extractShell(t, "a.sh", `# source ghost/commented.sh
# ghost_commented() { :; }

real() {
  echo "source ghost/quoted.sh"
  echo 'ghost_quoted() { :; }'
  printf '%s\n' "export GHOST_VAR=1"
}
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "" {
		t.Errorf("imports = %q", got)
	}
	if got := strings.Join(fa.SymbolNames(), ","); got != "real" {
		t.Errorf("symbols = %q", got)
	}
}

// The shebang is the only entrypoint signal a shell script carries — there is no `main` the
// interpreter looks for, and whether a file is run or sourced is the caller's choice. A
// library conventionally has no shebang, so the absence is as much a fact as the presence.
func TestShellShebangIsTheEntrypointSignal(t *testing.T) {
	script := extractShell(t, "bin/run", "#!/usr/bin/env bash\nmain() { :; }\n")
	if got := strings.Join(script.Entrypoints, ","); got != "#!" {
		t.Errorf("entrypoints = %q; a shebang makes a script executable", got)
	}
	lib := extractShell(t, "lib/log.sh", "log_info() { :; }\n")
	if got := strings.Join(lib.Entrypoints, ","); got != "" {
		t.Errorf("entrypoints = %q; a sourced library has no shebang", got)
	}
}

// A trailing comment and a following command are both routine on a source line —
// `source lib.sh || exit 1` is how a script fails fast when its library is missing — and
// neither is part of the path.
func TestShellSourceStopsAtTheEndOfThePath(t *testing.T) {
	fa := extractShell(t, "a.sh", `source lib/one.sh || exit 1
source lib/two.sh # the second one
source lib/three.sh; echo done
source lib/four.sh && echo ok
`)
	want := "lib/four.sh,lib/one.sh,lib/three.sh,lib/two.sh"
	if got := strings.Join(fa.ImportPaths(), ","); got != want {
		t.Errorf("imports = %q, want %q", got, want)
	}
}

// The doc block. A shell script's only documentation convention is a run of `#` lines, and
// two things that look like one are not prose: a shellcheck directive is tooling
// configuration, and a rule of `#` characters is a section divider. Either one reaching a
// bundle page reads as a bug in the extracted prose.
func TestShellDocStopsAtDirectivesAndDividers(t *testing.T) {
	fa := extractShell(t, "a.sh", `#!/bin/bash

# Builds the artifact.
build() { :; }

# shellcheck disable=SC2086
sc_directive() { :; }

###############################
divided() { :; }

# Real prose.
# shellcheck disable=SC2086
mixed() { :; }
`)
	docs := map[string]string{}
	for _, s := range fa.Symbols {
		docs[s.Name] = s.Doc
	}
	if docs["build"] != "Builds the artifact." {
		t.Errorf("build doc = %q", docs["build"])
	}
	for _, name := range []string{"sc_directive", "divided"} {
		if docs[name] != "" {
			t.Errorf("%s doc = %q; a directive and a divider are not prose", name, docs[name])
		}
	}
	// The directive is nearest the declaration, so the walk upward stops there and the real
	// prose above it is not reached. That is the correct reading: a comment separated from
	// the declaration by a directive is not attached to it.
	if docs["mixed"] != "" {
		t.Errorf("mixed doc = %q; the walk stops at the directive", docs["mixed"])
	}
}
