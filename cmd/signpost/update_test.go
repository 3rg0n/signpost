package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/selfupdate"
)

// TestUpdateRejectsAPositionalArgument covers the mistake every other verb in this CLI
// invites. `signpost build .`, `signpost verify .`, `signpost graph show .` all take a
// repository path, so somebody will type `signpost update .` out of habit — and an update
// has nothing to do with a repository. Silently ignoring the argument would leave them
// believing they had updated something scoped to that tree.
func TestUpdateRejectsAPositionalArgument(t *testing.T) {
	stdout, stderr, code := invoke(t, "update", ".")
	if code != 2 {
		t.Errorf("exit = %d, want 2: a misuse of the command line, not a failure of the "+
			"update\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "no positional arguments") {
		t.Errorf("stderr does not say the argument is the problem:\n%s", stderr)
	}
	// And it says what to use instead, because somebody replacing a binary other than the
	// running one has a real need and -path is not guessable from the refusal alone.
	if !strings.Contains(stderr, "-path") {
		t.Errorf("stderr refuses without pointing at -path:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("a refusal wrote to stdout:\n%s", stdout)
	}
}

// TestUpdateHelpStatesWhatIsVerified is about trust rather than usage. This command
// downloads an executable and runs it as the user, so the help is where somebody decides
// whether to type it — and the facts they need are what is checked, what happens when the
// check fails, and what the command does when nobody is running it.
func TestUpdateHelpStatesWhatIsVerified(t *testing.T) {
	stdout, stderr, code := invoke(t, "update", "-h")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, stderr)
	}
	for _, want := range []string{
		"sha256",                        // what is checked
		"checksums.txt",                 // against what
		"binary you have is left alone", // what happens when it does not match
		"no background check",           // that it runs only when typed
		"no auto-update",                //
		selfupdate.ReleasesURL(),        // where the artifacts come from
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not mention %q, which is a fact somebody needs before letting "+
				"a binary replace itself:\n%s", want, stdout)
		}
	}
	// The four flags, each of which changes what happens on disk.
	for _, flag := range []string{"-dry-run", "-force", "-path", "-version"} {
		if !strings.Contains(stdout, flag) {
			t.Errorf("help does not list %s:\n%s", flag, stdout)
		}
	}
}

// TestUpdateReportsEachOutcome asserts the sentences, because they are the whole of what a
// user sees and three of the four cannot be reached from a test that goes through the
// network: already-current, a dry run, and a development build being replaced.
func TestUpdateReportsEachOutcome(t *testing.T) {
	tests := []struct {
		name    string
		res     selfupdate.Result
		dryRun  bool
		current string
		want    []string
		absent  []string
	}{
		{
			name: "an update that happened",
			res: selfupdate.Result{From: "v0.3.0", To: "v0.4.1", Path: "/usr/local/bin/signpost",
				Replaced: true, SHA256: "abc"},
			// Both versions, so the reader can see the direction — and the path, because a
			// machine with two signposts on it has a reasonable next question.
			want: []string{"updated v0.3.0 to v0.4.1", "/usr/local/bin/signpost",
				"What changed: " + selfupdate.ReleasesURL() + "/tag/v0.4.1"},
		},
		{
			name: "a binary already at the latest version",
			res:  selfupdate.Result{From: "v0.4.1", To: "v0.4.1", Path: "/bin/signpost"},
			// The version, not a bare "up to date": somebody who suspected they were stale
			// needs the number to be sure the answer is about the binary they just ran.
			want: []string{"already at v0.4.1", "nothing to do"},
			// No release link, because nothing changed and there is nothing to read about.
			absent: []string{"What changed"},
		},
		{
			name:   "a dry run",
			res:    selfupdate.Result{From: "v0.3.0", To: "v0.4.1", Path: "/bin/signpost", SHA256: "d1g35t"},
			dryRun: true,
			// The conditional mood is the point: it must not read as though it installed.
			want:   []string{"would install v0.4.1", "sha256 verified: d1g35t"},
			absent: []string{"What changed", "updated v0.3.0"},
		},
		{
			// A dry run that never downloaded, because Apply compares the version before
			// fetching and this binary is already current. A real run printed `sha256
			// verified:` with nothing after it — a verification that did not happen,
			// asserted by the command whose job is to report what was verified.
			name:   "a dry run with nothing to install",
			res:    selfupdate.Result{From: "v0.4.1", To: "v0.4.1", Path: "/bin/signpost"},
			dryRun: true,
			want:   []string{"would do nothing: already at v0.4.1", "-force"},
			absent: []string{"sha256 verified", "would install"},
		},
		{
			// -force with nothing to upgrade to. A real run reported "updated v0.1.0 to
			// v0.1.0", which describes a change of version that did not happen — and reads
			// as a defect in the tool rather than as the repair the reader asked for.
			name: "a forced reinstall of the same version",
			res: selfupdate.Result{From: "v0.1.0", To: "v0.1.0", Path: "/bin/signpost",
				Replaced: true, SHA256: "abc"},
			want: []string{"reinstalled v0.1.0", "/bin/signpost"},
			// And no release link: the same version's notes describe nothing that arrived
			// with the reinstall, so offering them implies something did.
			absent: []string{"updated v0.1.0 to v0.1.0", "What changed"},
		},
		{
			name: "a development build replaced by a release",
			res: selfupdate.Result{From: devVersion, To: "v0.4.1", Path: "/bin/signpost",
				Replaced: true},
			current: "dev (a1b2c3d, 2026-08-14)",
			// "updated dev to v0.4.1" would imply a direction between two things that
			// cannot be ordered, so this case says what was there instead.
			want:   []string{"installed v0.4.1", `reported "dev (a1b2c3d, 2026-08-14)"`},
			absent: []string{"updated dev"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := reportUpdate(&out, tt.res, tt.dryRun, tt.current); err != nil {
				t.Fatal(err)
			}
			got := out.String()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("output missing %q:\n%s", want, got)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("output contains %q, which describes a different outcome:\n%s",
						absent, got)
				}
			}
		})
	}
}

// TestUpdateIsListedBesideVersion is the reachability assertion, and it is not as
// tautological as it looks: runUpdate was written and compiled and gofmt-clean for an hour
// before anything called it, because a command in this CLI only exists once it is in the
// table. A verb absent from commands() is dead code that tests of its own would still pass.
func TestUpdateIsListedBesideVersion(t *testing.T) {
	var names []string
	for _, c := range commands() {
		names = append(names, c.name)
	}
	joined := strings.Join(names, " ")
	if !strings.Contains(joined, "update") {
		t.Fatalf("update is not a command; runUpdate is unreachable. Commands: %s", joined)
	}

	// And it appears in the top-level help, which is where a reader with a stale binary
	// looks — the same place `unknown command` sends them.
	stdout, _, code := invoke(t, "-h")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "update") {
		t.Errorf("`signpost -h` does not list update:\n%s", stdout)
	}
}
