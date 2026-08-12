package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanGitHubWritesBothFiles(t *testing.T) {
	root := t.TempDir()
	plan, err := PlanGitHub(root, "example.com/org/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Files) != 2 {
		t.Fatalf("planned %d file(s), want 2", len(plan.Files))
	}
	if blocked := plan.Blocked(); len(blocked) > 0 {
		t.Fatalf("an empty directory blocked the plan: %v", blocked)
	}
	if err := Apply(root, plan); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{WorkflowPath, ConfigPath} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s was planned but not written: %v", rel, err)
		}
	}
	cfg := read(t, root, ConfigPath)
	if !strings.Contains(cfg, "repo: example.com/org/repo\n") {
		t.Errorf("the config file does not name the repository it was given:\n%s", cfg)
	}
}

// TestPlanGitHubRefusesToOverwrite is the assertion that matters most here, because the
// failure it guards against is silent and unrecoverable: this command's output is a
// workflow, and a repository that has tuned theirs must not have it replaced by a default
// because somebody re-ran a setup command.
//
// Each file is tested on its own, and then the pair, because the interesting case is the
// partial one. A plan that skipped the blocked file and wrote the other would leave a
// repository with a config file and no workflow — which is a repository whose bundle
// silently stops being rebuilt, the exact failure this whole command exists to prevent.
func TestPlanGitHubRefusesToOverwrite(t *testing.T) {
	for _, present := range []string{WorkflowPath, ConfigPath} {
		t.Run(present, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, present, "# mine, tuned by hand\n")

			plan, err := PlanGitHub(root, "example.com/org/repo")
			if err != nil {
				t.Fatal(err)
			}
			blocked := plan.Blocked()
			if len(blocked) != 1 || blocked[0] != present {
				t.Fatalf("blocked = %v, want [%s]", blocked, present)
			}

			err = Apply(root, plan)
			if !errors.Is(err, ErrExists) {
				t.Fatalf("Apply on a blocked plan returned %v, want ErrExists", err)
			}
			if got := read(t, root, present); got != "# mine, tuned by hand\n" {
				t.Errorf("%s was overwritten by a plan that reported itself blocked:\n%s",
					present, got)
			}
			// The other file must not appear. Half a scaffold is the outcome the
			// all-or-nothing rule exists for.
			other := WorkflowPath
			if present == WorkflowPath {
				other = ConfigPath
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(other))); err == nil {
				t.Errorf("%s was written while %s blocked the plan, leaving a repository "+
					"half set up", other, present)
			}
		})
	}
}

// TestPlanGitHubWithoutARepoNameCommentsTheKeyOut covers the negative boundary: an absent
// name must not produce `repo:` with nothing after it. The tool's own reader would reject
// that, so a scaffold that emitted it would write a file signpost cannot parse — worse than
// writing no key at all.
func TestPlanGitHubWithoutARepoNameCommentsTheKeyOut(t *testing.T) {
	root := t.TempDir() // no .git, so nothing to derive from
	plan, err := PlanGitHub(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Repo != "" {
		t.Fatalf("derived %q from a directory with no git config", plan.Repo)
	}
	if plan.Derived {
		t.Error("Derived is true with no remote to derive from")
	}
	cfg := plan.Files[1].Contents
	for _, line := range strings.Split(cfg, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "repo:") {
			t.Errorf("an uncommented repo: key with no value: %q", line)
		}
	}
	if !strings.Contains(cfg, "# repo: example.com/org/repo") {
		t.Errorf("no commented example to fill in:\n%s", cfg)
	}
}

// TestRemoteRepoReadsOriginAndNothingElse is the regression test for the defect #31 fixed,
// one layer down. `repo` must name the repository being described, and the two ways to get
// that wrong here are reading the *upstream* remote in a fork and mangling a URL into a
// name that is not one.
func TestRemoteRepoReadsOriginAndNothingElse(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   string
	}{
		{
			name:   "https",
			config: "[remote \"origin\"]\n\turl = https://github.com/o/r.git\n",
			want:   "github.com/o/r",
		},
		{
			name:   "scp-like",
			config: "[remote \"origin\"]\n\turl = git@github.com:o/r.git\n",
			want:   "github.com/o/r",
		},
		{
			name:   "no .git suffix",
			config: "[remote \"origin\"]\n\turl = https://github.com/o/r\n",
			want:   "github.com/o/r",
		},
		{
			name:   "nested path",
			config: "[remote \"origin\"]\n\turl = https://gitlab.com/g/sub/r.git\n",
			want:   "gitlab.com/g/sub/r",
		},
		{
			// The fork case, which is the whole point. `upstream` is a different
			// repository, and reading it would reintroduce the bug from the other side.
			name: "upstream is not origin",
			config: "[remote \"upstream\"]\n\turl = https://github.com/upstream/r.git\n" +
				"[remote \"origin\"]\n\turl = https://github.com/fork/r.git\n",
			want: "github.com/fork/r",
		},
		{
			name: "a port is not part of the name",
			config: "[remote \"origin\"]\n" +
				"\turl = https://git.example.com:8443/o/r.git\n",
			want: "git.example.com/o/r",
		},
		{
			// Negative boundary that matters more than the rest: a token in a remote URL
			// must not reach a committed file.
			name:   "credentials are dropped",
			config: "[remote \"origin\"]\n\turl = https://user:ghp_secret@github.com/o/r.git\n",
			want:   "github.com/o/r",
		},
		{
			name:   "no origin at all",
			config: "[remote \"upstream\"]\n\turl = https://github.com/u/r.git\n",
			want:   "",
		},
		{
			name:   "a local path is not a repository name",
			config: "[remote \"origin\"]\n\turl = /srv/git/r.git\n",
			want:   "",
		},
		{
			name:   "no remotes",
			config: "[core]\n\tbare = false\n",
			want:   "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, ".git/config", tc.config)

			got, ok := remoteRepo(root)
			if got != tc.want {
				t.Errorf("remoteRepo = %q, want %q", got, tc.want)
			}
			if ok != (tc.want != "") {
				t.Errorf("remoteRepo ok = %v with name %q", ok, got)
			}

			// And through the plan, so a name that is read correctly but reported as
			// authored is still a failure: the caller prints a different message for a
			// derived name, and #31 is the reason that distinction exists.
			plan, err := PlanGitHub(root, "")
			if err != nil {
				t.Fatal(err)
			}
			if plan.Repo != tc.want {
				t.Errorf("plan.Repo = %q, want %q", plan.Repo, tc.want)
			}
			if plan.Derived != (tc.want != "") {
				t.Errorf("plan.Derived = %v for name %q", plan.Derived, plan.Repo)
			}
		})
	}
}

// TestPlanGitHubPrefersTheGivenNameOverTheRemote keeps the precedence ADR 0011 set: an
// explicit value wins over anything inferred, and it must not be silently labelled derived.
func TestPlanGitHubPrefersTheGivenNameOverTheRemote(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".git/config", "[remote \"origin\"]\n\turl = https://github.com/o/r.git\n")

	plan, err := PlanGitHub(root, "example.com/asked/for")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Repo != "example.com/asked/for" {
		t.Errorf("plan.Repo = %q; the remote won over an explicit name", plan.Repo)
	}
	if plan.Derived {
		t.Error("an explicit name is reported as derived, so the caller will tell the " +
			"user to check a value they supplied")
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	dest := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}
