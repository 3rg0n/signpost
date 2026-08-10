package okf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// RecordedCommit is what `verify -as-of-bundle` reads before it analyses anything, so the sha
// it returns has to be the one the bundle was built at — the churn attributes and co-change
// edges on every page are read as of it.
func TestRecordedCommitReturnsTheShaTheBundleWasBuiltAt(t *testing.T) {
	root, _, _ := write(t)
	// demoOptions records git://example.com/repo@8f2a1c9, and the sha is the part after the
	// last @ so a host carrying one of its own cannot be mistaken for it.
	if got := RecordedCommit(root); got != "8f2a1c9" {
		t.Errorf("RecordedCommit = %q, want 8f2a1c9", got)
	}
}

// Absence is empty rather than an error, because the caller does the same thing either way:
// read history from HEAD and let the strict comparison report whatever is wrong. A bundle
// broken enough to have no readable manifest already has a finding waiting for it in Verify,
// and a second one from here would name the same defect twice.
func TestRecordedCommitIsEmptyWhenTheBundleCannotSayWhatItDescribes(t *testing.T) {
	t.Run("no bundle at all", func(t *testing.T) {
		if got := RecordedCommit(t.TempDir()); got != "" {
			t.Errorf("RecordedCommit = %q for a directory with no bundle", got)
		}
	})
	t.Run("unparseable manifest", func(t *testing.T) {
		root, _, _ := write(t)
		path := filepath.Join(root, BundleDir, ManifestFile)
		if err := os.WriteFile(path, []byte("{ not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := RecordedCommit(root); got != "" {
			t.Errorf("RecordedCommit = %q for an unparseable manifest", got)
		}
	})
	t.Run("no resource recorded", func(t *testing.T) {
		root, _, _ := write(t)
		path := filepath.Join(root, BundleDir, ManifestFile)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// The field removed rather than the file replaced, so everything else about the
		// manifest still parses and only the resource is missing. This is what a bundle built
		// in a repository with no readable history looks like.
		out := strings.Replace(string(src),
			`"resource": "`+demoOptions().Resource+`",`, "", 1)
		if out == string(src) {
			t.Fatalf("the resource line was not found, so this test edited nothing:\n%s", src)
		}
		if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := RecordedCommit(root); got != "" {
			t.Errorf("RecordedCommit = %q for a manifest recording no resource", got)
		}
	})
}

// The parse, on the forms a resource can take and the ones it cannot.
//
// A unit test as well as the end-to-end one above, because this function is the only place the
// sha is recovered from the URI and the URI's shape is a convention rather than a checked
// format. A resource that stopped round-tripping would otherwise surface as a gate that reads
// history from HEAD and says nothing.
func TestCommitFromResourceReadsTheShaAndRefusesEverythingElse(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"git://example.com/repo@8f2a1c9", "8f2a1c9"},
		// No repo name: resourceBase writes this when -repo was not passed.
		{"git://8f2a1c9", "8f2a1c9"},
		// A page's own resource, which appends a path. Not what the manifest holds, but
		// nothing here should depend on which of the two it was handed.
		{"git://example.com/repo@8f2a1c9/internal/auth", "8f2a1c9"},
		// A host carrying an @ of its own: the last one wins, because a sha cannot contain one.
		{"git://user@host/repo@8f2a1c9", "8f2a1c9"},
		{"", ""},
		// Not a resource this tool writes. Refused rather than guessed at — the value goes on
		// to git, and vcs.validCommit is the second gate rather than the only one.
		{"https://example.com/repo@8f2a1c9", ""},
		{"8f2a1c9", ""},
		{"git://", ""},
		{"git://example.com/repo@", ""},
	} {
		if got := commitFromResource(c.in); got != c.want {
			t.Errorf("commitFromResource(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
