package site

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The install commands on the landing page, checked against the README and against what the
// repository actually holds.
//
// # Why this exists
//
// The page's install section is the first thing a reader runs and the one claim here that
// fails in their terminal rather than on the page. It names two scripts by path and this
// module by name, and each of those is a fact with an answer in the tree: install.sh and
// install.ps1 exist or they do not, and the module path is in go.mod.
//
// [ADR 0037](../docs/adr/0037-the-landing-page-is-gated-on-its-verdicts-not-its-words.md)
// left these ungated, on the argument that gating prose means asserting on wording. A
// command is not prose. It is quoted text that has to be correct character for character,
// and a rename in this repository makes both documents wrong at once with nothing to say so.
//
// # What is gated
//
// Three things, in the two tests below:
//
//   - every command on the page appears verbatim in the README's `## Install` section, and
//     every way to install in that section appears on the page;
//   - the scripts the page names are exactly the installers this repository holds, so a
//     renamed, added, or deleted script fails rather than 404ing for a reader;
//   - the URLs and the `go install` line name this module, read from go.mod.
//
// A command in the README's Install section that begins with `signpost` is something you run
// after installing rather than a way to install, so it is not expected on the page. Nothing
// else in that section can be, because signpost is not on the reader's path yet.
//
// Wording stays free everywhere else: the page labels its blocks `macOS, Linux` where the
// README comments them `# macOS / Linux`, and neither is more right than the other.

var (
	fenceRe       = regexp.MustCompile("(?s)```[a-z]*\n(.*?)\n```")
	installSecRe  = regexp.MustCompile(`(?s)\n## Install\n(.*?)\n## `)
	siteInstallRe = regexp.MustCompile(`(?s)<section class="row" id="install">(.*?)</section>`)
	cmdCodeRe     = regexp.MustCompile(`(?s)<pre class="cmd__code">(.*?)</pre>`)
	installFileRe = regexp.MustCompile(`install\.[A-Za-z0-9]+`)
	rawURLRe      = regexp.MustCompile(`raw\.githubusercontent\.com/([^/]+/[^/]+)/`)
	goInstallRe   = regexp.MustCompile(`^go install (\S+)@latest$`)
	moduleRe      = regexp.MustCompile(`(?m)^module (\S+)$`)
)

// readmeInstallCommands returns the ways to install that the README's `## Install` section
// gives, one per line of its fenced blocks. Comments and blank lines are not commands, and a
// command that starts with `signpost` runs after the install rather than performing one.
func readmeInstallCommands(t *testing.T) []string {
	t.Helper()
	sec := installSecRe.FindStringSubmatch(siteDoc(t, readmePath))
	if sec == nil {
		t.Fatalf("%s: no `## Install` section, or it is the last section in the file", readmePath)
	}
	fences := fenceRe.FindAllStringSubmatch(sec[1], -1)
	if len(fences) == 0 {
		t.Fatalf("%s: the `## Install` section has no fenced command block", readmePath)
	}

	var out []string
	for _, fence := range fences {
		for _, line := range strings.Split(fence[1], "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "signpost ") {
				continue
			}
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s: the `## Install` section gives no way to install", readmePath)
	}
	return out
}

// siteInstallCommands returns the commands the landing page's install section shows, with
// HTML entities decoded — the PowerShell one contains an `&`, which the page must escape and
// the reader's shell must not see escaped.
func siteInstallCommands(t *testing.T) []string {
	t.Helper()
	sec := siteInstallRe.FindStringSubmatch(siteDoc(t, sitePath))
	if sec == nil {
		t.Fatalf(`%s: no <section class="row" id="install">`, sitePath)
	}
	blocks := cmdCodeRe.FindAllStringSubmatch(sec[1], -1)
	if len(blocks) == 0 {
		t.Fatalf("%s: the install section shows no command", sitePath)
	}

	var out []string
	for _, b := range blocks {
		out = append(out, strings.TrimSpace(html.UnescapeString(b[1])))
	}
	return out
}

// missingFrom returns the strings in a that b does not hold, sorted so a failure reads the
// same on every platform.
func missingFrom(a, b []string) []string {
	have := make(map[string]bool, len(b))
	for _, s := range b {
		have[s] = true
	}
	var out []string
	for _, s := range a {
		if !have[s] {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func TestSiteInstallCommandsAreTheREADMEs(t *testing.T) {
	readme := readmeInstallCommands(t)
	page := siteInstallCommands(t)

	for _, cmd := range missingFrom(page, readme) {
		t.Errorf("%s shows a command the README's `## Install` section does not give:\n  %s", sitePath, cmd)
	}
	for _, cmd := range missingFrom(readme, page) {
		t.Errorf("%s gives a way to install that %s does not show:\n  %s", readmePath, sitePath, cmd)
	}
}

func TestInstallCommandsNameWhatThisRepositoryHolds(t *testing.T) {
	// Both documents are checked. The test above makes the two sets equal, but it reports
	// rather than stops, and a command that names a script that is gone is wrong in whichever
	// document still carries it.
	cmds := append(readmeInstallCommands(t), siteInstallCommands(t)...)

	// The installers on disk are the ground truth. A script that is named and absent 404s in
	// the reader's terminal; one that exists and is named nowhere is a way to install that
	// neither document offers.
	onDisk, err := filepath.Glob("../install.*")
	if err != nil {
		t.Fatalf("glob installers: %v", err)
	}
	if len(onDisk) == 0 {
		t.Fatal("no ../install.* in the repository root; this test has nothing to compare against")
	}
	var want []string
	for _, p := range onDisk {
		want = append(want, filepath.Base(p))
	}

	var named []string
	for _, cmd := range cmds {
		named = append(named, installFileRe.FindAllString(cmd, -1)...)
	}
	for _, s := range missingFrom(want, named) {
		t.Errorf("this repository holds %s and no install command names it", s)
	}
	for _, s := range missingFrom(named, want) {
		t.Errorf("an install command names %s, which this repository does not hold", s)
	}

	module := moduleOf(t)
	repo := strings.TrimPrefix(module, "github.com/")
	if repo == module {
		t.Fatalf("go.mod names module %q, which this test cannot turn into an owner/repo pair", module)
	}

	var raw, gosrc int
	for _, cmd := range cmds {
		for _, m := range rawURLRe.FindAllStringSubmatch(cmd, -1) {
			raw++
			if m[1] != repo {
				t.Errorf("an install command fetches from %s, and go.mod says this is %s:\n  %s", m[1], repo, cmd)
			}
		}
		if m := goInstallRe.FindStringSubmatch(cmd); m != nil {
			gosrc++
			if want := module + "/cmd/signpost"; m[1] != want {
				t.Errorf("`go install` names %s, want %s", m[1], want)
			}
			if st, err := os.Stat("../cmd/signpost"); err != nil || !st.IsDir() {
				t.Errorf("`go install` names ../cmd/signpost, which is not a directory here: %v", err)
			}
		}
	}
	// Both forms are counted, because a check that runs zero times passes.
	if raw == 0 {
		t.Errorf("no install command fetches a script from raw.githubusercontent.com; the URL check ran against nothing")
	}
	if gosrc == 0 {
		t.Errorf("no install command builds from source; the module check ran against nothing")
	}
}

func moduleOf(t *testing.T) string {
	t.Helper()
	m := moduleRe.FindStringSubmatch(siteDoc(t, "../go.mod"))
	if m == nil {
		t.Fatal("../go.mod has no `module` line")
	}
	return m[1]
}
