// Package selfupdate replaces this binary with a published release.
//
// It is the same transaction `install.sh` and `install.ps1` perform, and that is
// deliberate rather than incidental: a second, subtly different way to install signpost
// would be a second place for the verification rules to drift, and the verification is
// the whole reason this is a package rather than three lines in the command. What the
// installers do, and what this does, in the same order:
//
//   - Resolve the latest tag from the redirect on /releases/latest rather than from the
//     API. The API is rate-limited per IP and unauthenticated CI shares an IP with
//     everyone else on the runner; the redirect is not, and it needs no token.
//   - Download the platform's archive from the release, and download checksums.txt.
//   - Refuse to proceed if the release publishes no checksums.txt, or if the archive is
//     not listed in it, or if the digest does not match. Three separate refusals, none
//     of them a warning: an unverified binary is the one outcome worse than not
//     updating.
//   - Write beside the target and rename over it, so a failure leaves the old binary
//     intact and a running process is never written into.
//
// Nothing here is a git operation and nothing reads the repository: an update is about
// the tool, not about the tree it analyses. The package therefore has no dependency on
// discover, graph, or okf, and can be exercised end to end against an httptest server.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Repo is the repository releases are published from.
//
// Hard-coded rather than configurable. An update fetches an executable and runs it as
// the user, so the source is a trust boundary: a flag or an environment variable
// pointing it elsewhere would turn one mistyped hostname, or one poisoned CI
// environment, into arbitrary code execution wearing this tool's name. Tests reach the
// seam through Client.BaseURL, which is unexported behaviour of a struct field rather
// than an option a user can set.
const Repo = "3rg0n/signpost"

// DefaultTimeout bounds the whole exchange: two redirects, two downloads.
//
// One bound for all of it rather than per request, because the failure this guards
// against is a stalled transfer and a per-request timeout resets on every byte. Ten
// megabytes over a slow link is the legitimate worst case and takes well under this.
const DefaultTimeout = 2 * time.Minute

// maxArchiveBytes caps a download, and maxChecksumBytes caps checksums.txt.
//
// Both are attacker-adjacent: a compromised or impersonated host could otherwise answer
// with an endless body and exhaust memory on the machine running the update. A release
// archive is a few megabytes and a checksums file is a few kilobytes, so these are far
// past any legitimate answer.
const (
	maxArchiveBytes  = 64 << 20
	maxChecksumBytes = 1 << 20
)

// Client fetches releases. The zero value works.
type Client struct {
	// HTTPClient is the transport. Nil means one with DefaultTimeout and redirects
	// followed, matching what a browser or curl would do for these URLs.
	HTTPClient *http.Client
	// BaseURL is where releases live. Empty means github.com, which is the only value
	// a released binary ever uses; a test points it at an httptest server.
	BaseURL string
}

// Release is a published release and the asset for the running platform.
type Release struct {
	// Version is the tag, as published: "v0.2.0".
	Version string
	// Asset is the archive's filename, which is also its key in checksums.txt.
	Asset string
	// URL is where Asset was, or would be, downloaded from.
	URL string
}

// Result is what an update did.
type Result struct {
	// From and To are the version replaced and the version installed. Equal when
	// nothing was written.
	From, To string
	// Path is the binary that was replaced, or would have been.
	Path string
	// Replaced is false when the binary was already at the requested version, or when
	// DryRun asked for no write.
	Replaced bool
	// SHA256 is the verified digest of the archive, hex-encoded. Reported so the
	// command can print what it checked rather than only that it checked.
	SHA256 string
}

// Being already current is not an error and has no sentinel: it is a successful outcome
// of `signpost update`, reported as Result.Replaced being false. Returning an error for
// it would make `signpost update && signpost build` fail for everybody who runs it twice.

// Latest resolves the most recent published release for this platform.
func (c *Client) Latest() (Release, error) {
	tag, err := c.latestTag()
	if err != nil {
		return Release{}, err
	}
	return c.release(tag)
}

// release names the asset for the running platform at a given tag.
func (c *Client) release(tag string) (Release, error) {
	if !validTag(tag) {
		return Release{}, fmt.Errorf("selfupdate: %q is not a release tag; expected a leading v, "+
			"as in v0.2.0", tag)
	}
	asset := assetName(tag, runtime.GOOS, runtime.GOARCH)
	return Release{
		Version: tag,
		Asset:   asset,
		URL:     c.base() + "/" + Repo + "/releases/download/" + tag + "/" + asset,
	}, nil
}

// assetName is what the release workflow called the archive for one platform.
//
// The naming is that workflow's, duplicated here because it is a contract between two
// files that cannot import each other: release.yml builds the names in shell and this
// has to predict them well enough to fetch one. Nothing at run time can detect a
// disagreement — a renamed asset is a 404, which reads as a network problem or a missing
// release rather than as the rename it is. So the contract is held by a test that reads
// the workflow and derives these names from the line in it, for every platform it
// publishes rather than only the one the test runs on.
//
// Parameterised by goos and goarch rather than reading runtime directly for that reason:
// a rule that only its own platform can check is a rule that breaks on the five others.
func assetName(tag, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("signpost_%s_%s_%s%s", tag, goos, goarch, ext)
}

// latestTag reads the tag out of the redirect on /releases/latest.
//
// GitHub answers that path with a 302 to /releases/tag/<tag>, so the tag is in the
// Location header and no API call, token, or JSON parsing is involved. The redirect is
// deliberately *not* followed: the answer is the header, and following it would download
// a release notes page to learn nothing further.
//
// A repository with no releases redirects to /releases instead, which parses out as the
// literal "releases" — checked for, because the alternative is a download URL built from
// a tag that does not exist and a 404 blamed on the platform.
func (c *Client) latestTag() (string, error) {
	u := c.base() + "/" + Repo + "/releases/latest"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("selfupdate: building the request for %s: %w", u, err)
	}
	client := *c.client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("selfupdate: %s: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drained rather than ignored, so the connection can be reused for the download
	// that follows.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxChecksumBytes))

	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", fmt.Errorf("selfupdate: %s answered %d with no redirect; expected one to the "+
			"latest release", u, resp.StatusCode)
	}
	tag := path.Base(loc)
	if !validTag(tag) {
		return "", fmt.Errorf("selfupdate: could not read a release tag from %s (got %q). If this "+
			"repository has no releases yet, there is nothing to update to", loc, tag)
	}
	return tag, nil
}

// validTag reports whether s looks like a release tag this project publishes.
//
// Deliberately strict, and checked before the string is ever put in a URL: the tag comes
// from a redirect header, which is remote input, and it is concatenated into the download
// path. A tag of "../../../etc" would otherwise build a URL pointing outside the release.
func validTag(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, r := range s[1:] {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r == '.', r == '-', r == '+':
		default:
			return false
		}
	}
	return true
}

// Download fetches a release's archive and returns it, verified.
//
// Verified before it is returned rather than after, so no caller can be written that
// unpacks first and checks second. The three refusals are separate errors because they
// mean different things to whoever reads them: a release without checksums is a broken
// release, an asset missing from checksums is a partial publish, and a mismatch is either
// a corrupted transfer or an archive that is not the one that was published.
func (c *Client) Download(r Release) (archive []byte, digest string, err error) {
	sums, err := c.get(c.base()+"/"+Repo+"/releases/download/"+r.Version+"/checksums.txt",
		maxChecksumBytes)
	if err != nil {
		// Both readings of a 404 here are named, because this is the error a mistyped
		// -version produces and the two causes have different remedies. Claiming only the
		// first would tell somebody who typed v99.0.0 that the release is broken.
		return nil, "", fmt.Errorf("selfupdate: no checksums.txt for release %s, so nothing can "+
			"be verified; refusing to install. Either that release does not exist — check %s — "+
			"or it was published without one: %w", r.Version, ReleasesURL(), err)
	}
	want := checksumFor(string(sums), r.Asset)
	if want == "" {
		return nil, "", fmt.Errorf("selfupdate: %s is not listed in release %s's checksums.txt; "+
			"refusing to install an unverified binary", r.Asset, r.Version)
	}

	archive, err = c.get(r.URL, maxArchiveBytes)
	if err != nil {
		return nil, "", fmt.Errorf("selfupdate: downloading %s: %w", r.Asset, err)
	}
	sum := sha256.Sum256(archive)
	got := hex.EncodeToString(sum[:])
	if got != want {
		return nil, "", fmt.Errorf("selfupdate: checksum mismatch for %s\n  expected %s\n  got      "+
			"%s\nNot installing. Either the download was corrupted or the asset is not the one that "+
			"was published", r.Asset, want, got)
	}
	return archive, got, nil
}

// checksumFor finds one asset's digest in a sha256sum-format file.
//
// The `*` form is accepted because sha256sum writes it for a file read in binary mode,
// which is what the release workflow produces on some platforms and not others. Matching
// only the bare form would reject a legitimate release for a difference in a marker
// character.
func checksumFor(sums, asset string) string {
	for _, ln := range strings.Split(sums, "\n") {
		digest, name, ok := strings.Cut(strings.TrimSpace(ln), " ")
		if !ok {
			continue
		}
		name = strings.TrimPrefix(strings.TrimSpace(name), "*")
		if name == asset && len(digest) == hex.EncodedLen(sha256.Size) {
			return strings.ToLower(digest)
		}
	}
	return ""
}

// get reads one URL, bounded.
func (c *Client) get(u string, limit int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("building the request for %s: %w", u, err)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", u, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", u, err)
	}
	if int64(len(body)) == limit {
		return nil, fmt.Errorf("%s is larger than the %d-byte limit; refusing it", u, limit)
	}
	return body, nil
}

func (c *Client) client() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: DefaultTimeout}
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimSuffix(c.BaseURL, "/")
	}
	return "https://github.com"
}

// binaryName is what the archive calls the executable. Split the same way assetName is,
// and for the same reason: the workflow-contract test has to check the name for a platform
// other than the one it is running on.
func binaryName() string { return binaryNameFor(runtime.GOOS) }

func binaryNameFor(goos string) string {
	if goos == "windows" {
		return "signpost.exe"
	}
	return "signpost"
}

// extract pulls the executable out of a release archive.
//
// The archive holds LICENSE and README.md beside the binary, so this looks for one entry
// by name rather than taking the first file. The name is anchored to the archive's own
// directory — `signpost_v0.2.0_linux_amd64/signpost` — and the base name is compared
// rather than the path, because matching the full path would hard-code the version into
// the comparison.
//
// Entry names are checked, not trusted. A tar or zip entry may name any path it likes,
// including `../`, and nothing here writes an entry to a path derived from its name — but
// the check stays, because the next person to touch this may not know that.
func extract(archive []byte, asset string) ([]byte, error) {
	if strings.HasSuffix(asset, ".zip") {
		return extractZip(archive)
	}
	return extractTarGz(archive)
}

func extractTarGz(archive []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: the archive is not gzip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("selfupdate: reading the archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg || !isBinaryEntry(h.Name) {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, maxArchiveBytes))
		if err != nil {
			return nil, fmt.Errorf("selfupdate: reading %s from the archive: %w", h.Name, err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("selfupdate: the archive contains no %s", binaryName())
}

func extractZip(archive []byte) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("selfupdate: the archive is not a zip: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !isBinaryEntry(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("selfupdate: opening %s in the archive: %w", f.Name, err)
		}
		b, err := io.ReadAll(io.LimitReader(rc, maxArchiveBytes))
		_ = rc.Close()
		if err != nil {
			return nil, fmt.Errorf("selfupdate: reading %s from the archive: %w", f.Name, err)
		}
		return b, nil
	}
	return nil, fmt.Errorf("selfupdate: the archive contains no %s", binaryName())
}

// isBinaryEntry reports whether an archive entry is the executable.
//
// Both separators are accepted because an archive is not built by the platform that
// unpacks it: a zip written on Linux for a Windows target uses forward slashes, and
// splitting on the local separator alone would find nothing.
func isBinaryEntry(name string) bool {
	if strings.Contains(name, "..") {
		return false
	}
	name = strings.ReplaceAll(name, "\\", "/")
	return path.Base(name) == binaryName()
}

// Options configure Apply.
type Options struct {
	// Version is the tag to install. Empty means the latest release.
	Version string
	// Path is the binary to replace. Empty means the running executable.
	Path string
	// Current is the version this binary reports, used to skip a no-op write.
	Current string
	// DryRun resolves and verifies but writes nothing, so somebody can see what an
	// update would do before letting it happen to a tool their CI depends on.
	DryRun bool
	// Force writes even when the versions already match — the repair case, for a
	// binary that is the right version and the wrong bytes.
	Force bool
}

// Apply installs a release over the target binary.
func (c *Client) Apply(o Options) (Result, error) {
	target, err := targetPath(o.Path)
	if err != nil {
		return Result{}, err
	}
	res := Result{From: o.Current, Path: target}

	var rel Release
	if o.Version != "" {
		rel, err = c.release(o.Version)
	} else {
		rel, err = c.Latest()
	}
	if err != nil {
		return res, err
	}
	res.To = rel.Version

	// Compared before anything is downloaded. Somebody running this on a schedule is
	// current almost every time, and the common case should cost one redirect rather
	// than a download and a digest.
	if !o.Force && o.Current == rel.Version {
		return res, nil
	}

	archive, digest, err := c.Download(rel)
	if err != nil {
		return res, err
	}
	res.SHA256 = digest
	bin, err := extract(archive, rel.Asset)
	if err != nil {
		return res, err
	}
	if o.DryRun {
		return res, nil
	}
	if err := replace(target, bin); err != nil {
		return res, err
	}
	res.Replaced = true
	return res, nil
}

// targetPath resolves which file to replace.
//
// Symlinks are resolved, because the interesting case is a binary reached through one: a
// package manager or a version manager may put signpost on the PATH as a link, and
// writing over the link would replace the link with a file and detach it from whatever
// manages it. Resolving means the update lands on the real binary and the link keeps
// pointing at it.
func targetPath(explicit string) (string, error) {
	p := explicit
	if p == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("selfupdate: cannot find this binary's own path: %w", err)
		}
		p = exe
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("selfupdate: %s: %w", p, err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	// A path that does not resolve is reported by the write, which can say whether it
	// was missing or unwritable. Guessing here would produce a worse message.
	return abs, nil
}

// replace writes the new binary over the old one.
//
// Write-then-rename, in the target's own directory so the rename is within one
// filesystem and therefore atomic: a partial write can never be left where the shell
// will find it, and a process already running the old binary is not written into.
//
// Windows needs the extra step. It refuses to rename over a file that is open for
// execution — which this file is, since it is the process doing the renaming — so the
// old binary is moved aside first. The leftover is best-effort deleted and named
// `.signpost.old` so that a locked one is recognisable rather than mysterious.
func replace(target string, bin []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".new*")
	if err != nil {
		return fmt.Errorf("selfupdate: cannot write to %s: %w\nIf signpost is installed somewhere "+
			"that needs elevated permission, run the installer from README instead", dir, err)
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(bin); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("selfupdate: writing %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("selfupdate: writing %s: %w", name, err)
	}
	// 0755 explicitly: CreateTemp makes 0600, which would install a binary the user's
	// own shell can run and nobody else can, silently changing who may use a shared
	// install. Not writable by group or other — only executable, which is what an
	// installed binary is and what install.sh's `chmod 0755` already writes.
	// #nosec G302 -- an executable must be executable, and a shared install readable.
	if err := os.Chmod(name, 0o755); err != nil {
		return fmt.Errorf("selfupdate: setting permissions on %s: %w", name, err)
	}

	old := filepath.Join(dir, "."+filepath.Base(target)+".old")
	_ = os.Remove(old)
	if runtime.GOOS == "windows" {
		if err := os.Rename(target, old); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("selfupdate: cannot move the running binary aside: %w\nClose any "+
				"other signpost process and try again", err)
		}
	}
	if err := os.Rename(name, target); err != nil {
		if runtime.GOOS == "windows" {
			// Put it back rather than leaving the user with no signpost at all.
			_ = os.Rename(old, target)
		}
		return fmt.Errorf("selfupdate: installing over %s: %w", target, err)
	}
	// Best-effort: Windows holds the moved-aside file until the process exits, and a
	// leftover `.signpost.old` beside the binary is harmless.
	_ = os.Remove(old)
	return nil
}

// ReleasesURL is where a human goes to read what changed.
func ReleasesURL() string {
	return "https://github.com/" + url.PathEscape(strings.SplitN(Repo, "/", 2)[0]) + "/" +
		url.PathEscape(strings.SplitN(Repo, "/", 2)[1]) + "/releases"
}
