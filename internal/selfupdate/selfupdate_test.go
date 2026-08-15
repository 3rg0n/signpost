package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestAssetNamesMatchTheReleaseWorkflow is the test the package's naming comment relies on.
//
// release.yml builds asset names in shell and this package predicts them in Go, and the two
// cannot import each other. A disagreement is undetectable at run time: a renamed asset is
// a 404, which a user reads as a network fault or a missing release rather than as the
// rename it is, and `signpost update` would be broken for every platform at once with no
// test failing anywhere.
//
// So the expectation is derived from the workflow rather than restated: the `name=` line,
// the archive extension rule, and the six published targets are read out of the file, and
// every one of them is checked — not only the platform this test happens to run on, which
// is the one platform a naive version of this test would cover.
func TestAssetNamesMatchTheReleaseWorkflow(t *testing.T) {
	yaml := readRepoFile(t, ".github/workflows/release.yml")

	// The template, as the workflow writes it: name="signpost_${version}_${os}_${arch}".
	// Read rather than assumed, so reordering the fields fails here.
	tmpl := regexp.MustCompile(`name="([^"]*\$\{version\}[^"]*)"`).FindStringSubmatch(yaml)
	if tmpl == nil {
		t.Fatalf("no `name=\"...${version}...\"` line in release.yml; this test can no longer " +
			"read the asset naming out of the workflow and has to be rewritten against " +
			"whatever replaced it")
	}

	targets := releaseTargets(t, yaml)
	if len(targets) != 6 {
		t.Errorf("release.yml publishes %d targets, this test expected the 6 it had: %v. Not a "+
			"failure of the naming rule, but the list below is now checking the wrong set",
			len(targets), targets)
	}

	for _, target := range targets {
		os_, arch, _ := strings.Cut(target, "/")
		// The workflow's own expansion, done here with the same substitutions it makes.
		want := strings.NewReplacer("${version}", "v1.2.3", "${os}", os_, "${arch}", arch).
			Replace(tmpl[1])
		if os_ == "windows" {
			want += ".zip"
		} else {
			want += ".tar.gz"
		}
		if got := assetName("v1.2.3", os_, arch); got != want {
			t.Errorf("assetName(v1.2.3, %s, %s) = %q, release.yml publishes %q. An update on "+
				"that platform would 404", os_, arch, got, want)
		}
	}

	// The archive-extension split, asserted against the workflow's condition rather than
	// against this package's belief about it: zip for windows and tar.gz for everything
	// else, which is what decides whether extract reaches extractZip or extractTarGz.
	if !strings.Contains(commandLines(yaml), `if [ "$os" = "windows" ]; then bin="signpost.exe"; fi`) {
		t.Error("release.yml no longer names the windows binary signpost.exe on the line this " +
			"test reads; binaryNameFor may now be wrong")
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		want := "signpost"
		if goos == "windows" {
			want = "signpost.exe"
		}
		if got := binaryNameFor(goos); got != want {
			t.Errorf("binaryNameFor(%s) = %q, the archive holds %q", goos, got, want)
		}
	}

	// The digest algorithm, which is the one part of the contract that cannot fail
	// visibly: a release switching to sha512 would produce checksums this package hashes
	// as sha256, so every asset would look tampered with and every update would refuse.
	if !strings.Contains(commandLines(yaml), "sha256sum") {
		t.Error("release.yml no longer writes checksums with sha256sum, but Download hashes " +
			"with sha256 and would report every asset as a mismatch")
	}
	if !strings.Contains(commandLines(yaml), "checksums.txt") {
		t.Error("release.yml no longer publishes checksums.txt, which is the file Download " +
			"refuses to install without")
	}
}

// releaseTargets reads the platforms release.yml builds for out of its `for target in`
// list. Reading it means adding a platform to the workflow without teaching this package
// about it is a visible failure rather than a silently unreachable target.
func releaseTargets(t *testing.T, yaml string) []string {
	t.Helper()
	body := regexp.MustCompile(`(?s)for target in\s*\\\n(.*?)\n\s*do\n`).FindStringSubmatch(yaml)
	if body == nil {
		t.Fatalf("no `for target in ... do` loop in release.yml; this test cannot see which " +
			"platforms are published")
	}
	var targets []string
	for _, f := range strings.Fields(strings.ReplaceAll(body[1], `\`, " ")) {
		if strings.Contains(f, "/") {
			targets = append(targets, f)
		}
	}
	return targets
}

func TestLatestReadsTheTagFromTheRedirect(t *testing.T) {
	c, _ := serve(t, release{tag: "v0.4.1"})
	rel, err := c.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "v0.4.1" {
		t.Errorf("version = %q, want v0.4.1 — the tag is the last element of the Location "+
			"header, and nothing else in the response says it", rel.Version)
	}
	if want := assetName("v0.4.1", runtime.GOOS, runtime.GOARCH); rel.Asset != want {
		t.Errorf("asset = %q, want %q", rel.Asset, want)
	}
	if !strings.HasSuffix(rel.URL, "/releases/download/v0.4.1/"+rel.Asset) {
		t.Errorf("url = %q, want it under the release's download path", rel.URL)
	}
}

// TestARepositoryWithNoReleasesIsSaidToHaveNone is the negative half of the redirect
// boundary. GitHub answers /releases/latest with a redirect to /releases when nothing is
// published, so path.Base yields the literal "releases" — which, unchecked, becomes a tag
// in a download URL and a 404 blamed on the platform.
func TestARepositoryWithNoReleasesIsSaidToHaveNone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/"+Repo+"/releases", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL}

	_, err := c.Latest()
	if err == nil {
		t.Fatal("a repository with no releases resolved a tag; the next step would be a " +
			"download URL containing the word `releases` where a version belongs")
	}
	if !strings.Contains(err.Error(), "no releases yet") {
		t.Errorf("error does not name the cause:\n%v", err)
	}
}

// TestATagFromTheNetworkIsNotTrustedInAURL asserts the traversal boundary. The tag comes
// out of a redirect header, which is remote input, and it is concatenated into a path.
func TestATagFromTheNetworkIsNotTrustedInAURL(t *testing.T) {
	for _, tag := range []string{
		"../../../etc/passwd",
		"v1.0.0/../../../other/releases/download/v9",
		"v1.0.0 ",
		"v1.0.0\nX-Injected: 1",
		"1.0.0",
		"v",
		"",
		"latest",
		"releases",
	} {
		if validTag(tag) {
			t.Errorf("validTag(%q) = true; that string would be concatenated into a download "+
				"URL", tag)
		}
	}
	// The positive half, so a rule that rejects everything fails too: these are the shapes
	// this project actually publishes, including the prerelease and build-metadata forms.
	for _, tag := range []string{"v0.1.0", "v1.2.3", "v10.0.0-rc.1", "v1.0.0+build.5"} {
		if !validTag(tag) {
			t.Errorf("validTag(%q) = false, but that is a tag this repository could publish", tag)
		}
	}

	c, _ := serve(t, release{tag: "v0.4.1"})
	if _, err := c.Apply(Options{Version: "../../../etc", Path: filepath.Join(t.TempDir(), "signpost")}); err == nil {
		t.Error("Apply accepted a traversal-shaped -version")
	}
}

func TestDownloadRefusesARelease(t *testing.T) {
	tests := []struct {
		name string
		rel  release
		want string
	}{
		{
			// A release that published no checksums at all cannot be verified by anything,
			// and installing it anyway would make the other two refusals decorative.
			name: "publishing no checksums",
			rel:  release{tag: "v0.4.1", noChecksums: true},
			want: "no checksums.txt for release v0.4.1",
		},
		{
			// A partial publish: checksums.txt exists and this platform's archive is not in
			// it. The archive may still download fine, which is exactly why this has to
			// refuse rather than warn.
			name: "whose checksums omit this asset",
			rel:  release{tag: "v0.4.1", omitAsset: true},
			want: "is not listed in release",
		},
		{
			// Either a corrupted transfer or an archive that is not the one published.
			name: "whose asset does not match its digest",
			rel:  release{tag: "v0.4.1", corrupt: true},
			want: "checksum mismatch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := serve(t, tt.rel)
			rel, err := c.Latest()
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := c.Download(rel); err == nil {
				t.Fatalf("Download succeeded for a release %s; an unverified binary is the one "+
					"outcome worse than not updating", tt.name)
			} else if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not say %q:\n%v", tt.want, err)
			}
		})
	}
}

// TestNothingIsWrittenWhenVerificationFails is the same three refusals asserted where they
// matter — on the filesystem. Download returning an error is only useful if Apply cannot be
// reached past it, and the failure this guards against is a future refactor that unpacks
// first and verifies second.
func TestNothingIsWrittenWhenVerificationFails(t *testing.T) {
	for _, rel := range []release{
		{tag: "v0.4.1", noChecksums: true},
		{tag: "v0.4.1", omitAsset: true},
		{tag: "v0.4.1", corrupt: true},
	} {
		c, _ := serve(t, rel)
		target := filepath.Join(t.TempDir(), binaryName())
		if err := os.WriteFile(target, []byte("the binary the user already has"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Apply(Options{Path: target, Current: "v0.1.0"}); err == nil {
			t.Fatal("Apply succeeded on an unverifiable release")
		}
		got := readFile(t, target)
		if got != "the binary the user already has" {
			t.Errorf("the target was modified by a failed update; it now holds %q", got)
		}
		// And no debris beside it. A leftover temp file in a directory on the PATH is a
		// file somebody may later find and run.
		if names := dirEntries(t, filepath.Dir(target)); len(names) != 1 {
			t.Errorf("a failed update left %v in the install directory", names)
		}
	}
}

func TestApplyInstallsAVerifiedRelease(t *testing.T) {
	c, _ := serve(t, release{tag: "v0.4.1", binary: "the new binary"})
	dir := t.TempDir()
	target := filepath.Join(dir, binaryName())
	if err := os.WriteFile(target, []byte("the old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := c.Apply(Options{Path: target, Current: "v0.3.0"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replaced {
		t.Error("Replaced = false after a write")
	}
	if res.From != "v0.3.0" || res.To != "v0.4.1" {
		t.Errorf("From/To = %q/%q, want v0.3.0/v0.4.1", res.From, res.To)
	}
	if got := readFile(t, target); got != "the new binary" {
		t.Errorf("target holds %q, want the extracted binary — and not LICENSE or README, "+
			"which sit beside it in the archive", got)
	}
	if res.SHA256 == "" {
		t.Error("SHA256 is empty; the command prints what it verified, not only that it did")
	}
	// Executable, which CreateTemp's 0600 is not. A binary installed unreadable by
	// anybody but the updating user silently changes who may run a shared install.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("mode = %v, want 0755", info.Mode().Perm())
		}
	}
	if names := dirEntries(t, dir); len(names) != 1 {
		t.Errorf("update left %v behind; want only the binary", names)
	}
}

// TestTheArchivesOtherFilesAreNotInstalled is the negative boundary on extraction. The
// release archive holds LICENSE and README.md beside the binary, and both sort before
// `signpost`, so an extractor taking the first regular file installs a text file as an
// executable — a failure that reports success and leaves the user with a signpost that
// cannot run.
func TestTheArchivesOtherFilesAreNotInstalled(t *testing.T) {
	c, _ := serve(t, release{tag: "v0.4.1", binary: "the new binary"})
	target := filepath.Join(t.TempDir(), binaryName())

	if _, err := c.Apply(Options{Path: target, Current: "v0.3.0"}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, target)
	for _, other := range []string{"LICENSE text", "README text"} {
		if strings.Contains(got, other) {
			t.Fatalf("the installed file holds %q; the archive's other members were taken for "+
				"the binary", other)
		}
	}
	if got != "the new binary" {
		t.Errorf("installed %q", got)
	}
}

// TestAnArchiveWithNoBinaryIsAnError is the case where the release is malformed rather than
// tampered with: verification passes, because the archive really is the published one, and
// there is still nothing to install.
func TestAnArchiveWithNoBinaryIsAnError(t *testing.T) {
	c, _ := serve(t, release{tag: "v0.4.1", omitBinary: true})
	target := filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := c.Apply(Options{Path: target, Current: "v0.3.0"}); err == nil {
		t.Fatal("an archive containing no binary installed something")
	} else if !strings.Contains(err.Error(), "contains no "+binaryName()) {
		t.Errorf("error does not name what was missing:\n%v", err)
	}
	if got := readFile(t, target); got != "old" {
		t.Errorf("target was modified: %q", got)
	}
}

// TestAnEntryNamedWithATraversalIsNotTheBinary covers the archive-entry check. Nothing here
// writes to a path taken from an entry name, so this is defence for the next change rather
// than for a live bug — and the assertion says so by checking the entry is skipped, which
// is the behaviour a future writer would inherit.
func TestAnEntryNamedWithATraversalIsNotTheBinary(t *testing.T) {
	for _, name := range []string{
		"../signpost",
		"signpost_v1_linux_amd64/../../signpost",
		"..\\signpost.exe",
	} {
		if isBinaryEntry(name) {
			t.Errorf("isBinaryEntry(%q) = true; an entry naming a path outside the archive is "+
				"not the binary", name)
		}
	}
	// A zip built on Linux for a Windows target writes forward slashes, so splitting on
	// the local separator alone finds nothing and the update fails on Windows only.
	if !isBinaryEntry("signpost_v1_windows_amd64/" + binaryName()) {
		t.Errorf("isBinaryEntry rejected a forward-slash entry for %q", binaryName())
	}
	if !isBinaryEntry("signpost_v1_windows_amd64\\" + binaryName()) {
		t.Errorf("isBinaryEntry rejected a backslash entry for %q", binaryName())
	}
}

// TestBothArchiveFormatsExtract runs the tar and zip readers regardless of the platform the
// test is on. Half the release matrix ships zip and half ships tar.gz, so a per-platform
// test leaves one of the two readers unexercised on every machine that runs it.
func TestBothArchiveFormatsExtract(t *testing.T) {
	for _, tt := range []struct {
		asset string
		build func(string) []byte
	}{
		{"signpost_v1_linux_amd64.tar.gz", func(bin string) []byte { return tarGz(t, "signpost", bin) }},
		{"signpost_v1_windows_amd64.zip", func(bin string) []byte { return zipOf(t, "signpost.exe", bin) }},
	} {
		t.Run(tt.asset, func(t *testing.T) {
			// The entry is named for the platform the archive is for, not for the one this
			// runs on, so one of the two cases always names the other platform's binary.
			// extract finds it by the local binaryName, so the case that does not match is
			// expected to report exactly that.
			got, err := extract(tt.build("payload"), tt.asset)
			wantFound := strings.Contains(tt.asset, "windows") == (runtime.GOOS == "windows")
			switch {
			case wantFound && err != nil:
				t.Fatalf("extract: %v", err)
			case wantFound && string(got) != "payload":
				t.Errorf("extract = %q, want the entry's contents", got)
			case !wantFound && err == nil:
				t.Error("extract found a binary named for another platform")
			}
		})
	}
}

// TestABinaryAlreadyAtTheVersionIsNotDownloaded is both a correctness and a cost assertion:
// the common case for anybody running this on a schedule is that they are current, and the
// comparison happens before the download so that case costs one redirect.
func TestABinaryAlreadyAtTheVersionIsNotDownloaded(t *testing.T) {
	c, hits := serve(t, release{tag: "v0.4.1", binary: "new"})
	target := filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(target, []byte("current"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := c.Apply(Options{Path: target, Current: "v0.4.1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Replaced {
		t.Error("Replaced = true for a binary already at the latest version")
	}
	if res.From != res.To {
		t.Errorf("From/To = %q/%q; want them equal when nothing was written", res.From, res.To)
	}
	if got := readFile(t, target); got != "current" {
		t.Errorf("target was rewritten with %q", got)
	}
	if n := hits.archives; n != 0 {
		t.Errorf("downloaded the archive %d times while already current; the version is "+
			"compared before the download for exactly this case", n)
	}

	// -force is the repair case: right version, wrong bytes. It must download.
	res, err = c.Apply(Options{Path: target, Current: "v0.4.1", Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replaced || readFile(t, target) != "new" {
		t.Errorf("-force did not reinstall: Replaced=%v, target=%q", res.Replaced,
			readFile(t, target))
	}
}

// TestDryRunVerifiesAndWritesNothing asserts the half of -dry-run that is easy to get
// wrong. Returning early before the download would make it cheap and useless: the point is
// to report what an update *would* do, which includes whether it verifies.
func TestDryRunVerifiesAndWritesNothing(t *testing.T) {
	c, hits := serve(t, release{tag: "v0.4.1", binary: "new"})
	target := filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := c.Apply(Options{Path: target, Current: "v0.3.0", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Replaced {
		t.Error("Replaced = true for a dry run")
	}
	if res.SHA256 == "" {
		t.Error("a dry run reported no digest; it downloaded and verified, and saying so is " +
			"the reason to run it")
	}
	if hits.archives != 1 {
		t.Errorf("a dry run made %d archive requests, want 1: it reports what would happen, "+
			"which means finding out", hits.archives)
	}
	if got := readFile(t, target); got != "old" {
		t.Errorf("a dry run wrote to the target: %q", got)
	}
	if names := dirEntries(t, filepath.Dir(target)); len(names) != 1 {
		t.Errorf("a dry run left %v in the install directory", names)
	}

	// And a dry run against an unverifiable release still refuses, rather than reporting
	// what it would have installed.
	bad, _ := serve(t, release{tag: "v0.4.1", corrupt: true})
	if _, err := bad.Apply(Options{Path: target, Current: "v0.3.0", DryRun: true}); err == nil {
		t.Error("a dry run reported success for a release whose checksum does not match")
	}
}

// TestASymlinkedInstallIsFollowed covers the version-manager case: signpost on the PATH as
// a link into a versioned directory. Writing over the link replaces it with a file and
// detaches it from whatever manages it, so the update has to land on the real binary.
func TestASymlinkedInstallIsFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating a symlink on Windows needs a privilege this test cannot assume")
	}
	c, _ := serve(t, release{tag: "v0.4.1", binary: "new"})
	real_ := filepath.Join(t.TempDir(), "versions", "v0.3.0")
	if err := os.MkdirAll(real_, 0o755); err != nil {
		t.Fatal(err)
	}
	realBin := filepath.Join(real_, binaryName())
	if err := os.WriteFile(realBin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), binaryName())
	if err := os.Symlink(realBin, link); err != nil {
		t.Fatal(err)
	}

	res, err := c.Apply(Options{Path: link, Current: "v0.3.0"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a regular file, detaching it from whatever " +
			"manages the versioned directory it pointed into")
	}
	if got := readFile(t, realBin); got != "new" {
		t.Errorf("the real binary holds %q; the update did not follow the link", got)
	}
	// Compared as files rather than as strings. On macOS the temporary directory is itself
	// reached through a symlink — /var is /private/var — so targetPath resolves that prefix
	// too, and the path it returns is right while not being the string this test built. A
	// string comparison here passed on Linux and Windows and failed on macOS only, which is
	// the worst place for the difference to surface: in CI, on a platform this host cannot
	// run. What the assertion is about is which file was named, so ask that.
	wantFile, err := os.Stat(realBin)
	if err != nil {
		t.Fatal(err)
	}
	gotFile, err := os.Stat(res.Path)
	if err != nil {
		t.Fatalf("Path = %q, which does not exist: %v", res.Path, err)
	}
	if !os.SameFile(gotFile, wantFile) {
		t.Errorf("Path = %q, want the file at %q, which is the binary the link points into",
			res.Path, realBin)
	}
	// And separately: not the link. os.SameFile cannot tell those apart, because Stat follows
	// the link and reports the target either way — so reporting the link would satisfy the
	// check above while telling the reader a path that is still a symlink after the update.
	if res.Path == link {
		t.Errorf("Path = %q is the symlink, not the binary it resolves to; the command would "+
			"report the path it did not write to", res.Path)
	}
}

// TestAnUnwritableDirectoryPointsAtTheInstaller is the no-escalation boundary. A binary in
// a root-owned directory is the ordinary case for `/usr/local/bin`, and the answer is a
// message, not a privilege prompt.
func TestAnUnwritableDirectoryPointsAtTheInstaller(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only directory does not block writes on Windows the way a mode bit does")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write to a 0500 directory, so there is nothing to refuse")
	}
	c, _ := serve(t, release{tag: "v0.4.1", binary: "new"})
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, binaryName())
	if err := os.WriteFile(target, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	_, err := c.Apply(Options{Path: target, Current: "v0.3.0"})
	if err == nil {
		t.Fatal("an update into an unwritable directory reported success")
	}
	if !strings.Contains(err.Error(), "installer") {
		t.Errorf("the error does not tell the reader what to do instead:\n%v", err)
	}
}

// TestABodyPastTheLimitIsRefused covers the memory bound. Both downloads are
// attacker-adjacent — an impersonated or compromised host can answer with an endless body,
// and reading it exhausts memory on the machine running the update rather than failing.
func TestABodyPastTheLimitIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// One byte past the checksum limit is enough to prove the bound is applied; an
		// actually endless body would hang this test rather than assert anything.
		_, _ = w.Write(bytes.Repeat([]byte("a"), maxChecksumBytes+1))
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL}

	if _, err := c.get(srv.URL, maxChecksumBytes); err == nil {
		t.Error("a body past the limit was accepted")
	} else if !strings.Contains(err.Error(), "limit") {
		t.Errorf("error does not say what was exceeded:\n%v", err)
	}
}

func TestChecksumForReadsBothSha256sumForms(t *testing.T) {
	sums := "aa" + strings.Repeat("0", 62) + "  signpost_v1_linux_amd64.tar.gz\n" +
		"bb" + strings.Repeat("0", 62) + " *signpost_v1_windows_amd64.zip\n" +
		"\n" +
		"not a checksum line\n"

	// The `*` marks a file sha256sum read in binary mode, which the workflow produces on
	// some platforms and not others. Matching only the bare form rejects a legitimate
	// release over a marker character.
	if got := checksumFor(sums, "signpost_v1_windows_amd64.zip"); got != "bb"+strings.Repeat("0", 62) {
		t.Errorf("binary-mode entry not found: %q", got)
	}
	if got := checksumFor(sums, "signpost_v1_linux_amd64.tar.gz"); got != "aa"+strings.Repeat("0", 62) {
		t.Errorf("text-mode entry not found: %q", got)
	}
	// The negative boundary: not listed means not listed, and a prefix is not a match.
	// Returning "" is what makes Download's second refusal fire.
	for _, asset := range []string{
		"signpost_v1_darwin_arm64.tar.gz",
		"signpost_v1_linux_amd64",
		"signpost_v1_linux_amd64.tar.gz.sig",
		"",
	} {
		if got := checksumFor(sums, asset); got != "" {
			t.Errorf("checksumFor(%q) = %q, want no match", asset, got)
		}
	}
	// A truncated digest is not a digest. Accepting one would compare a short string to a
	// full sha256 and refuse the install with a mismatch, blaming the archive for a
	// malformed checksums file.
	short := "abc123  signpost_v1_linux_amd64.tar.gz\n"
	if got := checksumFor(short, "signpost_v1_linux_amd64.tar.gz"); got != "" {
		t.Errorf("a %d-character digest was accepted as sha256: %q", len(got), got)
	}
}

func TestReleasesURLIsTheRepositorysReleasePage(t *testing.T) {
	if got, want := ReleasesURL(), "https://github.com/"+Repo+"/releases"; got != want {
		t.Errorf("ReleasesURL() = %q, want %q — it is printed to a user, who will click it",
			got, want)
	}
}

// release describes the fixture server's answer, including the ways a release can be
// wrong. Each field is one of the failures Download exists to refuse.
type release struct {
	tag         string
	binary      string
	noChecksums bool // the release published no checksums.txt
	omitAsset   bool // checksums.txt exists and does not list this platform's archive
	corrupt     bool // the archive served is not the one the checksums attest to
	omitBinary  bool // a well-formed archive holding everything except the binary
}

// hits counts what was fetched, so a test can assert that a download did not happen.
type hits struct{ archives int }

// serve stands up a GitHub-shaped release host: a redirect at /releases/latest, an archive,
// and a checksums.txt. It answers with real archives so extraction is exercised rather than
// stubbed — the alternative is a test that passes against a tar reader that never ran.
func serve(t *testing.T, r release) (*Client, *hits) {
	t.Helper()
	asset := assetName(r.tag, runtime.GOOS, runtime.GOARCH)
	bin := r.binary
	if bin == "" {
		bin = "binary bytes"
	}

	var archive []byte
	entries := [][2]string{{"LICENSE", "LICENSE text"}, {"README.md", "README text"}}
	if !r.omitBinary {
		// Appended after LICENSE and README, deliberately: they sort first, so an
		// extractor taking the first regular file picks one of them.
		entries = append(entries, [2]string{binaryName(), bin})
	}
	if strings.HasSuffix(asset, ".zip") {
		archive = zipEntries(t, r.tag, entries)
	} else {
		archive = tarGzEntries(t, r.tag, entries)
	}

	sum := sha256.Sum256(archive)
	sums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	if r.omitAsset {
		sums = strings.Repeat("c", 64) + "  signpost_" + r.tag + "_plan9_386.tar.gz\n"
	}
	if r.corrupt {
		archive = append(archive, "tampered"...)
	}

	h := &hits{}
	mux := http.NewServeMux()
	mux.HandleFunc("/"+Repo+"/releases/latest", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/"+Repo+"/releases/tag/"+r.tag, http.StatusFound)
	})
	mux.HandleFunc("/"+Repo+"/releases/download/"+r.tag+"/checksums.txt",
		func(w http.ResponseWriter, _ *http.Request) {
			if r.noChecksums {
				http.NotFound(w, &http.Request{})
				return
			}
			_, _ = w.Write([]byte(sums))
		})
	mux.HandleFunc("/"+Repo+"/releases/download/"+r.tag+"/"+asset,
		func(w http.ResponseWriter, _ *http.Request) {
			h.archives++
			_, _ = w.Write(archive)
		})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL}, h
}

func tarGz(t *testing.T, name, contents string) []byte {
	t.Helper()
	return tarGzEntries(t, "v1", [][2]string{{name, contents}})
}

func tarGzEntries(t *testing.T, tag string, entries [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	dir := fmt.Sprintf("signpost_%s_%s_%s/", tag, runtime.GOOS, runtime.GOARCH)
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: dir + e[0], Mode: 0o755, Size: int64(len(e[1])), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipOf(t *testing.T, name, contents string) []byte {
	t.Helper()
	return zipEntries(t, "v1", [][2]string{{name, contents}})
}

func zipEntries(t *testing.T, tag string, entries [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	dir := fmt.Sprintf("signpost_%s_%s_%s/", tag, runtime.GOOS, runtime.GOARCH)
	for _, e := range entries {
		w, err := zw.Create(dir + e[0])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e[1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// readRepoFile reads a file from this repository, found by walking up to go.mod rather than
// assuming a depth — the number of levels is a detail that changes when a package moves.
func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return readFile(t, filepath.Join(dir, filepath.FromSlash(rel)))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above %s; cannot find %s", dir, rel)
		}
		dir = parent
	}
}

// commandLines drops comments, so an assertion about what a workflow runs does not match
// prose explaining what it does not run. This repository's workflows carry a lot of the
// latter.
func commandLines(yaml string) string {
	var kept []string
	for _, line := range strings.Split(yaml, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}
