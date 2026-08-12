package main

import (
	"runtime/debug"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/vcs"
)

// buildInfo is one build's worth of what the toolchain records, as a *debug.BuildInfo.
//
// Constructed rather than read, because the thing under test is a function of how a
// binary was *built* and a test process is exactly one build: it is a `go test`
// binary, which carries no vcs.* settings at all. Every interesting case here —
// released, dirty checkout, module install — belongs to a binary this test is not, so
// the only way to cover them is to hand versionString the settings each of those
// builds would have. That is the reason ReadBuildInfo is called in runVersion and not
// inside versionString.
func buildInfo(mainVersion string, kv ...string) *debug.BuildInfo {
	info := &debug.BuildInfo{Main: debug.Module{Version: mainVersion}}
	for i := 0; i+1 < len(kv); i += 2 {
		info.Settings = append(info.Settings, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
	}
	return info
}

// TestVersionStringNamesTheBuild covers the four differently-built binaries by name.
//
// Each case is a build that exists: the release path, a clone built with `go build`,
// the same clone with edits in the tree, `go install ...@version` through the proxy,
// and a binary with no build info to read. The values are the ones the toolchain
// actually writes — checked against `go version -m` on real binaries, including a
// proxy install of v0.1.0, which is where the "(go install)" case came from: it has
// no vcs.* settings and a genuine tag in Main.Version, and it printed "dev" before
// this.
func TestVersionStringNamesTheBuild(t *testing.T) {
	// The set the toolchain writes into a binary built from a clean clone. Named once
	// because three cases below vary one field of it and would otherwise disagree by
	// accident.
	const rev = "af8c6ab4243c727a4b22bba8806523f7f247e9eb"
	checkout := []string{
		"vcs", "git",
		"vcs.revision", rev,
		"vcs.time", "2026-08-12T10:13:56Z",
		"vcs.modified", "false",
	}
	dirty := append(append([]string{}, checkout[:len(checkout)-1]...), "true")

	for _, tc := range []struct {
		name     string
		injected string
		info     *debug.BuildInfo
		ok       bool
		want     string
	}{{
		// A release. The stamp wins over everything, including the vcs.* settings a
		// release build also carries: the tag is what was published, and nothing derived
		// is more specific than that.
		name:     "release",
		injected: "v0.1.0",
		info:     buildInfo("v0.1.0", checkout...),
		ok:       true,
		want:     "v0.1.0",
	}, {
		// `go build` or `go install ./cmd/signpost` from a clone. This is the case #35
		// was filed for.
		name:     "clean checkout",
		injected: devVersion,
		info:     buildInfo("(devel)", checkout...),
		ok:       true,
		want:     "dev (af8c6ab, 2026-08-12)",
	}, {
		// The same clone with uncommitted edits. "dirty" is the load-bearing word: the
		// revision is real and the binary contains something that is not in it, so a
		// bug reproduced against this sha may not reproduce against the commit.
		name:     "dirty checkout",
		injected: devVersion,
		info:     buildInfo("(devel)", dirty...),
		ok:       true,
		want:     "dev (af8c6ab, 2026-08-12, dirty)",
	}, {
		// `go install github.com/3rg0n/signpost/cmd/signpost@v0.1.0`. A proxy zip has no
		// .git, so there is nothing to stamp — but the module version is the tag, and
		// reporting "dev" for a binary that is exactly v0.1.0 is the same failure #35
		// describes with a different cause. Qualified, because this is not the artifact
		// the release published.
		name:     "module install",
		injected: devVersion,
		info:     buildInfo("v0.1.0"),
		ok:       true,
		want:     "v0.1.0 (go install)",
	}, {
		// A test binary, or `-buildvcs=false`. Nothing to add, so it says what it always
		// said rather than inventing a qualifier.
		name:     "nothing recorded",
		injected: devVersion,
		info:     buildInfo("(devel)"),
		ok:       true,
		want:     "dev",
	}, {
		// No build info at all. Not reachable in a released binary; covered because the
		// bool is part of the signature and a nil dereference here would be a panic in
		// the one command somebody runs to diagnose a broken install.
		name:     "no build info",
		injected: devVersion,
		info:     nil,
		ok:       false,
		want:     "dev",
	}, {
		// A checkout with vcs.time missing. Not a shape the toolchain produces, and
		// asserted anyway because the three settings are read independently: a revision
		// with no date is still worth printing, and the alternative is a trailing ", ".
		name:     "revision with no time",
		injected: devVersion,
		info:     buildInfo("(devel)", "vcs.revision", rev),
		ok:       true,
		want:     "dev (af8c6ab)",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			if got := versionString(tc.injected, tc.info, tc.ok); got != tc.want {
				t.Errorf("versionString = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestVersionAbbreviatesTheShaLikeEverythingElse pins the width to vcs.Commit.Short
// rather than to the number 7.
//
// The sha in `version` and the sha on a bundle page are the same fact, and a reader
// comparing them by eye is the whole point of printing it. Two abbreviations of
// different lengths would make that comparison a decision about whether a prefix
// matches. Written as "the same function's answer" so that changing the convention
// changes both, and this test does not have to be found and edited.
func TestVersionAbbreviatesTheShaLikeEverythingElse(t *testing.T) {
	const rev = "0123456789abcdef0123456789abcdef01234567"
	got := versionString(devVersion, buildInfo("(devel)", "vcs.revision", rev), true)

	full := "(" + rev
	if strings.Contains(got, full) {
		t.Errorf("version prints the whole sha: %q", got)
	}
	// The comparison that matters: whatever Short() returns is what appears here. Called
	// rather than spelled out, so changing the convention changes both sides of this and
	// this test does not have to be found and edited.
	if want := "dev (" + (vcs.Commit{SHA: rev}).Short() + ")"; got != want {
		t.Errorf("versionString = %q, want %q — the abbreviation must be the one every "+
			"other display of a commit in this tool uses", got, want)
	}
}

// TestVersionCommandPrintsWhatVersionStringSays is the wiring, which the table above
// cannot see: it tests a pure function, and a command that computed the right string
// and printed the bare `version` variable would pass every case of it.
//
// Asserted against versionString's own answer rather than against a literal, because
// this test runs in a test binary and cannot know which branch that is — which is the
// point. It compares the command with the function, and the function with the four
// builds, and neither assertion has to guess how this binary was built.
func TestVersionCommandPrintsWhatVersionStringSays(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	want := versionString(version, info, ok)

	for _, args := range [][]string{{"version"}, {"--version"}, {"-v"}} {
		stdout, stderr, code := invoke(t, args...)
		if code != 0 {
			t.Errorf("%v: exit = %d\n%s", args, code, stderr)
		}
		if got := strings.TrimSpace(stdout); got != want {
			t.Errorf("%v: printed %q, want %q", args, got, want)
		}
	}
}

// TestHelpBannerNamesTheBuildToo covers the other place the version is displayed.
//
// `signpost -h` is where a stale binary is most likely to be met — the reader ran a
// command that does not exist yet in what is installed, and the banner is the reply. A
// banner saying `dev` while `version` names the revision puts the answer one command
// further away, which is the failure #35 reported rather than a cosmetic difference.
//
// What this can and cannot catch, because the difference is not obvious: it pins the
// format, and reverting the banner to the bare `version` variable fails to compile —
// `info` and `ok` go unused, which is the regression a reader would actually write. What
// it cannot see is a banner that calls versionString on the wrong build info, because
// this runs in a test binary where every branch of versionString returns "dev". That is
// the same limit TestVersionCommandPrintsWhatVersionStringSays documents, and closing it
// would mean building a second binary from the checkout inside a test.
func TestHelpBannerNamesTheBuildToo(t *testing.T) {
	info, ok := debug.ReadBuildInfo()
	want := "signpost " + versionString(version, info, ok) + " —"

	stdout, _, _ := invoke(t, "-h")
	if !strings.Contains(stdout, want) {
		t.Errorf("`signpost -h` banner does not carry %q:\n%s", want,
			strings.SplitN(stdout, "\n", 2)[0])
	}
}
