package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/okf"
)

// The exit code is verify's whole interface, so these tests assert it directly rather than
// asserting on internal/okf's VerifyResult — which is tested there. What has to hold at this
// level is that a stale bundle reaches the process as 1 and a clean one as 0, because the
// only consumer that matters is a CI job reading `$?`.

// A verify that exits zero because it had nothing to check is the failure design §4.6 names
// as worse than no check at all. So a missing bundle is a failure, not a pass.
func TestVerifyFailsWhenThereIsNoBundle(t *testing.T) {
	root := fixture(t)
	stdout, _, code := invoke(t, "verify", "--quiet", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a repository with no bundle\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "signpost build") {
		t.Errorf("the failure does not say how to fix it:\n%s", stdout)
	}
}

func TestVerifyPassesOnAFreshBuildAndSaysWhatItChecked(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("build exit = %d\n%s", code, stderr)
	}

	stdout, stderr, code := invoke(t, "verify", "--quiet", root)
	if code != 0 {
		t.Fatalf("exit = %d for a freshly built bundle\n%s\n%s", code, stdout, stderr)
	}
	// The counts are not decoration. "ok" over zero pages and "ok" over every page read the
	// same to a human scanning a CI log, and only one of them is a result.
	for _, want := range []string{"checked", "page(s)", "edge(s)", "prose link(s)", "ok"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "checked 0 page(s)") {
		t.Errorf("verify passed having opened no pages:\n%s", stdout)
	}
}

// The case the PR check exists for: the code moved and the bundle did not.
func TestVerifyFailsAfterTheCodeChanges(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("build exit = %d\n%s", code, stderr)
	}
	// A new package is a new concept, so the bundle is missing a page for it.
	full := filepath.Join(root, "internal", "billing", "billing.go")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("package billing\n\nfunc Charge() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := invoke(t, "verify", "--quiet", root)
	if code != 1 {
		t.Fatalf("exit = %d, want 1: the repository gained a module the bundle has no page "+
			"for\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "problem(s)") {
		t.Errorf("the failure does not say what was wrong:\n%s", stdout)
	}
}

// A human's notes must not fail the gate. This is the same property build guarantees, checked
// from the other side: a verify that went red on someone's paragraph is a verify they turn
// off, and then the staleness check is gone too.
func TestVerifyPassesWithHumanNotes(t *testing.T) {
	root := fixture(t)
	if _, stderr, code := invoke(t, "build", "--quiet", root); code != 0 {
		t.Fatalf("build exit = %d\n%s", code, stderr)
	}
	page := filepath.Join(root, okf.BundleDir, "modules", "auth.md")
	src, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	note := "\n## Why this is here\n\nRate limiting looks like it belongs here but does not.\n"
	if err := os.WriteFile(page, append(src, []byte(note)...), 0o600); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := invoke(t, "verify", "--quiet", root)
	if code != 0 {
		t.Fatalf("exit = %d: a human's notes failed verification\n%s", code, stdout)
	}
}

// Exit 2, not 1. A CI job has to tell a mistyped invocation from a stale bundle: the first
// will fail identically forever, and the second is fixed by a build.
func TestVerifyRejectsTwoPathsWithAUsageCode(t *testing.T) {
	root := fixture(t)
	if _, _, code := invoke(t, "verify", root, root); code != 2 {
		t.Errorf("exit = %d, want 2 for a usage error", code)
	}
}

func TestVerifyIsListedInTheUsage(t *testing.T) {
	stdout, _, code := invoke(t, "help")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "verify") {
		t.Errorf("usage does not list verify:\n%s", stdout)
	}
}
