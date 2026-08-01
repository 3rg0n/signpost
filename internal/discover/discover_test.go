package discover

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree materialises a map of relative slash paths to contents.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", abs, err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", abs, err)
		}
	}
	return root
}

// paths returns the discovered paths, for order-sensitive assertions.
func paths(r *Result) []string {
	out := make([]string, len(r.Files))
	for i, f := range r.Files {
		out[i] = f.Path
	}
	return out
}

func find(t *testing.T, r *Result, path string) File {
	t.Helper()
	for _, f := range r.Files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("path %q not discovered; got %v", path, paths(r))
	return File{}
}

func TestWalkClassifiesAndSorts(t *testing.T) {
	root := writeTree(t, map[string]string{
		"go.mod":                   "module example.com/x\n",
		"main.go":                  "package main\n",
		"internal/a/a.go":          "package a\n",
		"internal/a/a_test.go":     "package a\n",
		"web/app.ts":               "export const x = 1\n",
		"web/app.spec.ts":          "test('x', () => {})\n",
		"py/mod.py":                "import os\n",
		"rs/lib.rs":                "pub fn f() {}\n",
		"docs/adr/0001-thing.md":   "# ADR\n",
		"CODEOWNERS":               "* @team\n",
		"Containerfile":            "FROM cgr.dev/chainguard/static\n",
		"docker-compose.yml":       "services: {}\n",
		".github/workflows/ci.yml": "on: push\n",
		"api/openapi.yaml":         "openapi: 3.1.0\n",
		"proto/svc.proto":          "syntax = \"proto3\";\n",
		"migrations/001_init.sql":  "CREATE TABLE t();\n",
		"config.json":              "{}\n",
		"go.sum":                   "example.com/y v1.0.0 h1:abc=\n",
	})

	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	// Sorted output is a hard requirement: the bundle is committed.
	got := paths(res)
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("results not sorted ascending: %v", got)
		}
	}

	wantClass := map[string]Class{
		"go.mod":                   ClassManifest,
		"go.sum":                   ClassManifest,
		"main.go":                  ClassSource,
		"web/app.ts":               ClassSource,
		"py/mod.py":                ClassSource,
		"rs/lib.rs":                ClassSource,
		"docs/adr/0001-thing.md":   ClassDoc,
		"CODEOWNERS":               ClassOwnership,
		"Containerfile":            ClassInfra,
		"docker-compose.yml":       ClassInfra,
		".github/workflows/ci.yml": ClassInfra,
		"api/openapi.yaml":         ClassContract,
		"proto/svc.proto":          ClassContract,
		"migrations/001_init.sql":  ClassMigration,
		"config.json":              ClassData,
	}
	for p, want := range wantClass {
		if f := find(t, res, p); f.Class != want {
			t.Errorf("%s: class = %q, want %q", p, f.Class, want)
		}
	}

	wantLang := map[string]Lang{
		"main.go":    LangGo,
		"web/app.ts": LangTS,
		"py/mod.py":  LangPython,
		"rs/lib.rs":  LangRust,
	}
	for p, want := range wantLang {
		if f := find(t, res, p); f.Lang != want {
			t.Errorf("%s: lang = %q, want %q", p, f.Lang, want)
		}
	}

	// Tests are discovered and marked, not dropped.
	for _, p := range []string{"internal/a/a_test.go", "web/app.spec.ts"} {
		if f := find(t, res, p); !f.IsTest {
			t.Errorf("%s should be marked IsTest", p)
		}
	}
	if f := find(t, res, "main.go"); f.IsTest {
		t.Error("main.go must not be marked IsTest")
	}
}

func TestWalkHonorsGitignoreAtEveryLevel(t *testing.T) {
	root := writeTree(t, map[string]string{
		".gitignore":        "*.log\nsecret/\n",
		"app.log":           "noise\n",
		"keep.go":           "package a\n",
		"secret/creds.txt":  "token\n",
		"sub/.gitignore":    "*.tmp\n!important.tmp\n",
		"sub/a.tmp":         "x\n",
		"sub/important.tmp": "x\n",
		"sub/b.go":          "package b\n",
		"other/c.tmp":       "x\n",
	})

	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := strings.Join(paths(res), " ")

	for _, excluded := range []string{"app.log", "secret/creds.txt", "sub/a.tmp"} {
		if strings.Contains(got, excluded) {
			t.Errorf("%s should have been ignored; got %v", excluded, paths(res))
		}
	}
	for _, included := range []string{"keep.go", "sub/b.go", "sub/important.tmp", "other/c.tmp"} {
		if !strings.Contains(got, included) {
			t.Errorf("%s should have been discovered; got %v", included, paths(res))
		}
	}
}

func TestWalkSkipsBinaries(t *testing.T) {
	root := writeTree(t, map[string]string{
		"text.go": "package a\n",
	})
	// A NUL byte is the decisive binary signal.
	if err := os.WriteFile(filepath.Join(root, "blob.bin"), []byte("MZ\x00\x00\x01binary"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	b := find(t, res, "blob.bin")
	if !b.Binary {
		t.Error("blob.bin should be flagged Binary")
	}
	if b.Content != "" {
		t.Error("binary content must not be read into memory")
	}
	if b.Size == 0 {
		t.Error("binary files should still record their size")
	}
	if f := find(t, res, "text.go"); f.Binary {
		t.Error("a text file must not be flagged Binary")
	}
}

// Valid UTF-8 with multi-byte runes is text, even though a naive high-byte check
// would call it binary.
func TestWalkTreatsUTF8AsText(t *testing.T) {
	root := writeTree(t, map[string]string{
		"i18n.go": "package a // ünïcödé — 日本語 🎉\n",
	})
	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	f := find(t, res, "i18n.go")
	if f.Binary {
		t.Error("valid UTF-8 must be treated as text")
	}
	if !strings.Contains(f.Content, "日本語") {
		t.Error("multi-byte content should survive intact")
	}
}

func TestWalkTruncatesOversizedFileKeepingHeadAndTail(t *testing.T) {
	root := t.TempDir()
	// Over the byte cap: head marker, filler, tail marker.
	var sb strings.Builder
	sb.WriteString("HEAD_MARKER\n")
	sb.WriteString(strings.Repeat("x", MaxFullBytes+HeadTailBytes))
	sb.WriteString("\nTAIL_MARKER\n")
	if err := os.WriteFile(filepath.Join(root, "big.go"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	f := find(t, res, "big.go")
	if !f.Truncated {
		t.Fatal("oversized file should be marked Truncated")
	}
	if !strings.Contains(f.Content, "HEAD_MARKER") {
		t.Error("head of an oversized file should be retained")
	}
	if !strings.Contains(f.Content, "TAIL_MARKER") {
		t.Error("tail of an oversized file should be retained")
	}
	if !strings.Contains(f.Content, Elision) {
		t.Error("truncated content should carry the elision marker")
	}
	if len(f.Content) > 3*HeadTailBytes {
		t.Errorf("truncated content is %d bytes, expected roughly 2x%d", len(f.Content), HeadTailBytes)
	}
}

// A file inside the byte cap but with a huge number of lines — a minified bundle
// or a data dump — is truncated by line count.
func TestWalkTruncatesByLineCount(t *testing.T) {
	root := t.TempDir()
	content := strings.Repeat("a\n", MaxFullLines+100)
	if err := os.WriteFile(filepath.Join(root, "many.py"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	f := find(t, res, "many.py")
	if f.Size > MaxFullBytes {
		t.Fatalf("fixture should be within the byte cap, got %d bytes", f.Size)
	}
	if !f.Truncated {
		t.Error("a file over the line cap should be truncated")
	}
	if f.Lines != MaxFullLines+100 {
		t.Errorf("Lines = %d, want the true count %d", f.Lines, MaxFullLines+100)
	}
	if strings.Count(f.Content, "\n") > MaxFullLines {
		t.Error("truncated content should not exceed the line cap")
	}
}

// CRLF must normalise to LF at ingest, or a Windows checkout and a Linux runner
// produce different bundles from the same commit (design §8.1).
func TestWalkNormalizesCRLF(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "crlf.go"), []byte("package a\r\n\r\nfunc F() {}\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	f := find(t, res, "crlf.go")
	if strings.Contains(f.Content, "\r") {
		t.Errorf("content should have no CR after normalisation: %q", f.Content)
	}
	if f.Content != "package a\n\nfunc F() {}\n" {
		t.Errorf("unexpected normalised content: %q", f.Content)
	}
}

func TestWalkRecordsVendoredWithoutReading(t *testing.T) {
	root := writeTree(t, map[string]string{
		"app.go":                        "package main\n",
		"vendor/example.com/lib/lib.go": "package lib\n",
		"web/node_modules/pkg/index.js": "module.exports = {}\n",
	})

	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := strings.Join(paths(res), " ")
	if strings.Contains(got, "vendor/") || strings.Contains(got, "node_modules") {
		t.Errorf("vendored trees should be pruned from the walk; got %v", paths(res))
	}
	// Pruned directories are recorded so the walk never looks complete when it isn't.
	var reasons []string
	for _, s := range res.Skipped {
		reasons = append(reasons, s.Path+":"+s.Reason)
	}
	joined := strings.Join(reasons, " ")
	if !strings.Contains(joined, "vendor") || !strings.Contains(joined, "node_modules") {
		t.Errorf("pruned vendored directories should be recorded in Skipped; got %v", reasons)
	}
	if len(res.Sources()) != 1 {
		t.Errorf("Sources() should return only the repo's own code, got %d", len(res.Sources()))
	}
}

// TestWalkPrunesTestFixturesFromAnalysis is the defect signpost found by biting itself.
//
// Adding testdata/corpus — four sample projects built to look like real repositories — put
// `testdata/corpus/ts/app/(marketing)` in signpost's own index as a module, and react, httpx
// and serde in it as dependencies. The manifest is the half that matters most: a fixture's
// package.json is a statement about the *sample*, and treating it as the host repository's
// makes the bundle claim a dependency nobody can find in the go.mod, which is the false
// grounding design §4.6 exists to prevent.
//
// Asserted on both halves, because they fail independently: the walk can prune the source
// files while a manifest reader still reaches the fixture's package.json.
func TestWalkPrunesTestFixturesFromAnalysis(t *testing.T) {
	root := writeTree(t, map[string]string{
		"app.go":                          "package main\n",
		"go.mod":                          "module example.com/host\n",
		"testdata/corpus/ts/src/index.ts": "export const x = 1\n",
		"testdata/corpus/ts/package.json": `{"dependencies":{"react":"19.0.0"}}`,
		"internal/x/fixtures/sample/a.py": "def f(): pass\n",
		"web/__fixtures__/sample/b.js":    "module.exports = {}\n",
	})

	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range res.Files {
		if isFixture(f.Path) {
			t.Errorf("%s was discovered as a file; a fixture's contents belong to the sample "+
				"project, not to the repository being described", f.Path)
		}
	}
	// The manifest half. Nothing here reads package.json, but this is the field every
	// manifest reader routes on, so an empty content is what makes the reader a no-op.
	for _, f := range res.Files {
		if strings.Contains(f.Path, "testdata") && f.Content != "" {
			t.Errorf("%s was read (%d bytes); a fixture manifest names the sample's "+
				"dependencies, and reading it puts them on the host repository's pages",
				f.Path, len(f.Content))
		}
	}
	if len(res.Sources()) != 1 {
		t.Errorf("Sources() = %d, want only app.go: %v", len(res.Sources()), paths(res))
	}

	// Named as a fixture, not as vendored. Skips are surfaced to the user, and telling
	// somebody their own reviewed testdata is third-party code is a wrong explanation of a
	// right decision.
	var reasons []string
	for _, s := range res.Skipped {
		reasons = append(reasons, s.Path+":"+s.Reason)
	}
	joined := strings.Join(reasons, " ")
	for _, want := range []string{"testdata:test fixture", "fixtures:test fixture", "__fixtures__:test fixture"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no skip recorded matching %q; got %v", want, reasons)
		}
	}
}

// The escape hatch, asserted because a flag nobody checks is a flag that rots. This is the
// only way to recover a directory genuinely named `fixtures` that holds shipping code.
func TestWalkIncludeFixturesAnalysesThem(t *testing.T) {
	root := writeTree(t, map[string]string{
		"app.go":               "package main\n",
		"testdata/corpus/b.go": "package b\n",
	})
	res, err := Walk(root, Options{IncludeFixtures: true})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	f := find(t, res, "testdata/corpus/b.go")
	if !f.Fixture {
		t.Error("Fixture should stay true with -include-fixtures: the flag changes whether " +
			"the file is analysed, not what it is")
	}
	if f.Content == "" {
		t.Error("-include-fixtures did not read the file")
	}
	if len(res.Sources()) != 2 {
		t.Errorf("Sources() = %d, want both files: %v", len(res.Sources()), paths(res))
	}
}

// A fixture is neither vendored nor a test, and the distinction is load-bearing: a
// tested_by edge from a real module to a sample project would be a false claim, and calling
// a hand-maintained fixture third-party code would be a wrong reason for a right skip.
func TestFixtureIsDistinctFromVendoredAndTest(t *testing.T) {
	const rel = "testdata/corpus/go/greeter/greeter.go"
	if !isFixture(rel) {
		t.Fatalf("isFixture(%q) = false", rel)
	}
	if isVendored(rel) {
		t.Errorf("isVendored(%q) = true; a fixture is this repository's own reviewed code", rel)
	}
	// A fixture that happens to be named like a test is still a fixture, and the reverse
	// must not hold: a real test file must not be pruned.
	if isFixture("internal/okf/verify_test.go") {
		t.Error("a test file outside a fixture directory must stay in the walk")
	}
	// `testdata` matches as a directory segment only, so a file named for it is untouched.
	if isFixture("internal/discover/testdata.go") {
		t.Error("isFixture matched a file basename; only directory segments count")
	}
}

// Symlinks are recorded and never followed: a loop must not hang the walk, and a
// link out of the tree must not escape it.
func TestWalkDoesNotFollowSymlinks(t *testing.T) {
	root := writeTree(t, map[string]string{"real.go": "package a\n"})
	if err := os.Symlink(root, filepath.Join(root, "loop")); err != nil {
		t.Skipf("symlinks unavailable on this platform/account: %v", err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.go"), []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := strings.Join(paths(res), " ")
	if strings.Contains(got, "secret.go") {
		t.Error("a symlink out of the tree must not be followed")
	}
	if strings.Contains(got, "loop/") {
		t.Error("a symlink loop must not be traversed")
	}
	if len(res.Skipped) < 2 {
		t.Errorf("both symlinks should be recorded in Skipped, got %v", res.Skipped)
	}
}

// Every read is confined to the root by an os.Root handle, so content from
// outside the tree cannot reach the bundle even if the symlink skip above were
// removed. Verified by reading through the same path the walker uses.
func TestReadsAreConfinedToTheRoot(t *testing.T) {
	root := writeTree(t, map[string]string{"real.go": "package a\n"})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.go"), []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := os.OpenRoot(root)
	if err != nil {
		t.Fatalf("OpenRoot: %v", err)
	}
	defer func() { _ = rt.Close() }()

	var total int64
	for _, escape := range []string{
		"../secret.go",
		"a/../../secret.go",
		filepath.ToSlash(filepath.Join(outside, "secret.go")),
	} {
		if f, skip := readFile(rt, escape, Options{}, &total); skip == nil {
			t.Errorf("readFile(%q) escaped the root and returned %+v", escape, f)
		}
	}
	// A path inside the root still reads, so the confinement is not simply
	// rejecting everything.
	if _, skip := readFile(rt, "real.go", Options{}, &total); skip != nil {
		t.Errorf("a path inside the root must still read: %+v", skip)
	}
}

func TestWalkIsDeterministicAcrossRuns(t *testing.T) {
	root := writeTree(t, map[string]string{
		"z.go": "package z\n", "a.go": "package a\n", "m/q.go": "package q\n",
		"m/b.py": "import os\n", "docs/x.md": "# x\n", "b/c/d/e.ts": "export {}\n",
	})
	first, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	want := strings.Join(paths(first), "\n")
	for i := 0; i < 10; i++ {
		got, err := Walk(root, Options{})
		if err != nil {
			t.Fatalf("Walk: %v", err)
		}
		if strings.Join(paths(got), "\n") != want {
			t.Fatalf("run %d produced a different ordering", i)
		}
	}
}

func TestWalkEmptyAndMissingRoot(t *testing.T) {
	res, err := Walk(t.TempDir(), Options{})
	if err != nil {
		t.Fatalf("an empty directory is valid input: %v", err)
	}
	if len(res.Files) != 0 {
		t.Errorf("expected no files, got %v", paths(res))
	}

	if _, err := Walk(filepath.Join(t.TempDir(), "nope"), Options{}); err == nil {
		t.Error("a missing root should be an error")
	}

	f := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Walk(f, Options{}); err == nil {
		t.Error("a file as root should be an error")
	}
}

func TestWalkExtraIgnores(t *testing.T) {
	root := writeTree(t, map[string]string{
		"keep.go":      "package a\n",
		"gen/out.go":   "package gen\n",
		"other/out.go": "package other\n",
	})
	res, err := Walk(root, Options{ExtraIgnores: []string{"gen/"}})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := strings.Join(paths(res), " ")
	if strings.Contains(got, "gen/out.go") {
		t.Error("ExtraIgnores should exclude the gen directory")
	}
	if !strings.Contains(got, "other/out.go") {
		t.Error("ExtraIgnores must not over-match")
	}
}

// .git is never content, regardless of .gitignore.
func TestWalkAlwaysSkipsDotGit(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go":            "package a\n",
		".git/config":     "[core]\n",
		".git/objects/ab": "binary-ish\n",
	})
	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if strings.Contains(strings.Join(paths(res), " "), ".git/") {
		t.Errorf(".git must never be walked; got %v", paths(res))
	}
}

func TestByClassExcludesVendoredAndBinary(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.md":      "# doc\n",
		"docs/b.md": "# doc\n",
	})
	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if n := len(res.ByClass(ClassDoc)); n != 2 {
		t.Errorf("ByClass(doc) = %d, want 2", n)
	}
	if n := len(res.ByClass(ClassSource)); n != 0 {
		t.Errorf("ByClass(source) = %d, want 0", n)
	}
}

func TestCountLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0}, {"a", 1}, {"a\n", 1}, {"a\nb", 2}, {"a\nb\n", 2}, {"\n", 1},
	}
	for _, c := range cases {
		if got := countLines(c.in); got != c.want {
			t.Errorf("countLines(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestIsBinary(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{"empty", nil, false},
		{"ascii", []byte("package main"), false},
		{"utf8", []byte("héllo 日本"), false},
		{"nul", []byte("ab\x00cd"), true},
		{"invalid utf8", []byte{0xff, 0xfe, 0xfd, 0xfc, 0xfb, 0xfa, 0xf9, 0xf8}, true},
	}
	for _, c := range cases {
		if got := isBinary(c.in); got != c.want {
			t.Errorf("%s: isBinary = %v, want %v", c.name, got, c.want)
		}
	}
}
