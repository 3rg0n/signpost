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

// TestWalkIncludeVendoredAnalysesThem is issue #11, and it is the assertion the flag
// shipped without.
//
// `-include-vendored` read the files and nothing downstream looked at them, so the census
// on stderr went from two files to three and the node count did not move. Coverage existed
// — TestWalkRecordsVendoredWithoutReading checks the default path — but it stopped at the
// walk, and the walk was the half that already worked.
//
// So the assertion is on Sources(), not on File.Content. Content is what the flag used to
// change; Sources() is what extraction is driven from, which makes it the difference between
// reading a vendored file and analysing one. The three-way shape below is deliberate: with
// the flag off the file must be recorded and not analysed, and with it on both must hold.
func TestWalkIncludeVendoredAnalysesThem(t *testing.T) {
	tree := map[string]string{
		"app.go":                        "package main\n",
		"vendor/example.com/lib/lib.go": "package lib\n",
		"vendor/example.com/lib/go.mod": "module example.com/lib\n",
	}

	off, err := Walk(writeTree(t, tree), Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	on, err := Walk(writeTree(t, tree), Options{IncludeVendored: true})
	if err != nil {
		t.Fatalf("Walk(IncludeVendored): %v", err)
	}

	// The negative boundary, and the one that stops the fix from being "analyse everything".
	// A flag that turns on by accident is as wrong as one that never turns on, and this half
	// is what a fix threading the option too widely would break.
	if n := len(off.Sources()); n != 1 {
		t.Errorf("Sources() with the flag off = %d, want only app.go: %v", n, paths(off))
	}
	if len(off.Files) != 1 {
		t.Errorf("a vendored tree should still be pruned by default; got %v", paths(off))
	}

	f := find(t, on, "vendor/example.com/lib/lib.go")
	if !f.Vendored {
		t.Error("Vendored should stay true with -include-vendored: the flag changes whether " +
			"the file is analysed, not what it is")
	}
	if f.Content == "" {
		t.Error("-include-vendored did not read the file")
	}
	if n := len(on.Sources()); n != 2 {
		t.Errorf("Sources() = %d, want both files: %v.\n\nThis is issue #11 exactly: the walk "+
			"honoured the flag and Sources() filtered the result out again, so the file was "+
			"read and no extractor ever saw it.", n, paths(on))
	}
	// ByClass is the manifest half, and it fails independently. A fix to Sources() alone
	// analyses the vendored code and still ignores the go.mod beside it, which is a module
	// whose own declaration signpost has read and discarded.
	if n := len(on.ByClass(ClassManifest)); n != 1 {
		t.Errorf("ByClass(manifest) = %d, want the vendored go.mod: %v", n, paths(on))
	}
	if n := len(off.ByClass(ClassManifest)); n != 0 {
		t.Errorf("ByClass(manifest) with the flag off = %d, want none", n)
	}
}

// Analyses is the predicate the six consumers defer to, so its truth table is asserted
// directly rather than only through them. The zero value matters most: a Result built by a
// test or by a future caller that does not set IncludeVendored must exclude vendored files,
// because that is the default the flag overrules and not a decision to include them.
func TestAnalysesHonoursTheOptionAndDefaultsToExcluding(t *testing.T) {
	own := File{Path: "app.go"}
	vend := File{Path: "vendor/x/y.go", Vendored: true}

	zero := &Result{}
	if !zero.Analyses(own) {
		t.Error("a file that is not vendored is always analysed")
	}
	if zero.Analyses(vend) {
		t.Error("a zero-value Result analysed a vendored file; the default is to exclude it")
	}
	on := &Result{IncludeVendored: true}
	if !on.Analyses(vend) || !on.Analyses(own) {
		t.Error("IncludeVendored should analyse both")
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

// A JVM test is recognised two ways, and the second is what needs asserting: Maven and
// Gradle put tests under src/test, which the directory rule already catches, but a JVM
// test is also routinely a class named for the code it exercises sitting anywhere at all.
//
// The negative half is the half that matters. Matched case-insensitively — which is what
// the surrounding rules do — `Latest.java` ends in "test" and `Manifest.kt` ends in
// "test" too, and a production class marked as a test drops out of the public surface it
// declares. That is a silent loss of real API, not a noisy false positive, so both
// directions are pinned here.
func TestJVMTestPathsAreRecognisedAndOrdinaryClassesAreNot(t *testing.T) {
	tests := []string{
		"src/test/java/com/example/ServiceTest.java",
		"src/test/kotlin/com/example/ServiceTest.kt",
		"src/main/java/com/example/ServiceTest.java",
		"src/main/java/com/example/ServiceTests.java",
		"src/main/java/com/example/ServiceTestCase.java",
		"src/main/java/com/example/ServiceSpec.kt",
		"src/main/java/com/example/ServiceIT.java",
		"src/main/java/com/example/TestService.java",
		"scripts/BuildSpec.kts",
		// A class named exactly `Test` is a real test class, and JUnit's own tree has
		// several. The suffix rule takes no minimum length for that reason.
		"src/main/java/com/example/Test.java",
	}
	for _, rel := range tests {
		lang := LangJava
		if !strings.HasSuffix(rel, ".java") {
			lang = LangKotlin
		}
		if !isTestPath(rel, lang) {
			t.Errorf("isTestPath(%q) = false; this names a test class", rel)
		}
	}
	// Production classes whose names end in the same letters, and the JUnit-3 prefix
	// applied to a word rather than to a class name.
	notTests := []string{
		"src/main/java/com/example/Latest.java",
		"src/main/java/com/example/Manifest.kt",
		"src/main/java/com/example/Protest.java",
		"src/main/java/com/example/Tester.java",
		"src/main/java/com/example/Testament.kt",
		"src/main/java/com/example/Service.java",
		"src/main/kotlin/com/example/Api.kt",
	}
	for _, rel := range notTests {
		lang := LangJava
		if !strings.HasSuffix(rel, ".java") {
			lang = LangKotlin
		}
		if isTestPath(rel, lang) {
			t.Errorf("isTestPath(%q) = true; this is production code and its surface is real", rel)
		}
	}
}

// A .h is C, C++ or Objective-C and no filename can say which. The extension maps to
// LangC deliberately — classification is name-based, so the label is the family's lowest
// common denominator and the extractor reads the whole family's syntax — and the point of
// pinning it here is that the mapping is a decision rather than an oversight. The rest of
// the family's extensions are unambiguous and each must reach the right language, because
// a file whose Lang has no registered extractor is counted as unhandled and its whole
// surface goes missing.
func TestCFamilyExtensionsMapToTheirLanguage(t *testing.T) {
	want := map[string]Lang{
		"src/a.c": LangC,
		// The ambiguous one. Labelled C by design; see sourceExts.
		"src/a.h":   LangC,
		"src/a.cc":  LangCpp,
		"src/a.cpp": LangCpp,
		"src/a.cxx": LangCpp,
		"src/a.hpp": LangCpp,
		"src/a.hh":  LangCpp,
		"src/a.hxx": LangCpp,
		"src/a.m":   LangObjC,
		"src/a.mm":  LangObjC,
	}
	for rel, w := range want {
		class, got := classify(rel)
		if class != ClassSource {
			t.Errorf("classify(%q) class = %q, want %q", rel, class, ClassSource)
		}
		if got != w {
			t.Errorf("classify(%q) lang = %q, want %q", rel, got, w)
		}
	}
}

// langOfTest is what classify reports for a path, for the isTestPath cases below —
// isTestPath takes the language as an argument and the point is that classification and
// the test rule agree on the same file.
func langOfTest(rel string) Lang {
	_, lang := classify(rel)
	return lang
}

// A C-family test is recognised by directory or by basename, and the frameworks disagree
// about which basename. The negative half is again the half that matters: a production
// file marked as a test drops out of the public surface it declares, which is a silent
// loss of real API. `latest.c`, `protests.c` and `contest.cpp` all end in the letters of
// "test".
func TestCFamilyTestPathsAreRecognisedAndOrdinaryFilesAreNot(t *testing.T) {
	tests := []string{
		"test/buffer.c",
		"tests/buffer.c",
		"src/buffer_test.c",
		"src/buffer_test.cc",
		"src/buffer_tests.cpp",
		"src/buffer_unittest.cc",
		"src/test_buffer.c",
		"src/test-buffer.c",
		"src/buffer-test.cc",
		"src/Buffer_Test.c",
		// Xcode's convention, where the capital is the whole boundary.
		"Sources/ReaderTests.m",
		"Sources/ReaderTest.mm",
		// A file named exactly `test`, which autotools trees have.
		"src/test.c",
		"src/tests.cc",
	}
	for _, rel := range tests {
		if !isTestPath(rel, langOfTest(rel)) {
			t.Errorf("isTestPath(%q) = false; this names a test", rel)
		}
	}
	notTests := []string{
		"src/latest.c",
		"src/protests.c",
		"src/contest.cpp",
		"src/contests.cc",
		"src/tester.c",
		"src/testament.cpp",
		"src/buffer.c",
		"src/Reader.m",
		"include/buffer.h",
		// `testing.h` is a header a test framework ships, not a test.
		"include/testing.h",
	}
	for _, rel := range notTests {
		if isTestPath(rel, langOfTest(rel)) {
			t.Errorf("isTestPath(%q) = true; this is production code and its surface is real",
				rel)
		}
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

// The bundle is signpost's own output. Reading it back makes the tool describe itself.
//
// Not caught by the .gitignore rules that cover an ordinary build directory, because the
// bundle is deliberately committed (ADR 0005) — which is exactly how this shipped. The
// visible cost was the census on stderr: `analysed 223 files` on a repository with 143,
// the difference being the pages of the previous run. Skipped rather than recorded as
// Skipped, because a Skipped entry is a claim that the repository contains something
// signpost declined to read, and these files are not the repository's content at all.
func TestWalkAlwaysSkipsTheBundle(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go":                        "package a\n",
		"docs/note.md":                "# a real document\n",
		".signpost/index.md":          "---\ntype: index\n---\n",
		".signpost/modules/a.md":      "---\ntype: module\n---\n",
		".signpost/manifest.json":     "{}\n",
		".signpost/cache/abc123.json": "{}\n",
	})
	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, f := range res.Files {
		if strings.HasPrefix(f.Path, ".signpost/") {
			t.Errorf("the bundle was analysed as repository content: %s", f.Path)
		}
	}
	for _, s := range res.Skipped {
		if strings.HasPrefix(s.Path, ".signpost/") {
			t.Errorf("the bundle was recorded as skipped content: %s (%s). It is not the "+
				"repository's content, so counting it as content signpost declined to read "+
				"is a different wrong answer.", s.Path, s.Reason)
		}
	}
	// The point of the walk still happens. A skip rule broad enough to drop a real
	// document beside it would pass every assertion above.
	if len(res.Files) != 2 {
		t.Errorf("want the 2 real files, got %v", paths(res))
	}
}

// The literal in discover.go must stay the directory okf actually writes.
//
// Spelled literally there because this package imports nothing else in the module, and
// keeping the foundation dependency-free is worth a test instead. This is that test: it
// fails if the bundle directory is ever renamed, which is the only way the literal can rot.
func TestBundleDirLiteralMatchesTheEmitter(t *testing.T) {
	// The name ADR 0005 fixes, and the value of okf.BundleDir. Asserted by value rather
	// than by import, which is the whole point — importing okf here would create the
	// dependency this test exists to avoid.
	const bundleDir = ".signpost"
	root := writeTree(t, map[string]string{
		"a.go":                  "package a\n",
		bundleDir + "/index.md": "---\ntype: index\n---\n",
	})
	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(res.Files) != 1 {
		t.Errorf("the walk did not skip %s/; got %v", bundleDir, paths(res))
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

// Unclassified counts what nothing else does, and counts only that.
//
// The negative half is the point. A method that reported every file no reader
// claimed would fire on a README, a .gitignore signpost deliberately parses, and a
// package.json — all of which are classified, routed, and read. That line would be
// noise on every repository, and a coverage line nobody reads is worse than no line:
// it teaches the reader to skip the place the real gaps are reported.
func TestUnclassifiedCountsOnlyFilesOfNoRecognisedKind(t *testing.T) {
	root := writeTree(t, map[string]string{
		// Genuinely unrecognised: no class, so no reader is ever offered them.
		"web/views/page.hbs": "<h1>{{title}}</h1>\n",
		"web/views/mail.ejs": "<p><%= body %></p>\n",
		"web/src/site.css":   "body { color: red }\n",
		"deploy/app.conf":    "listen = 8080\n",
		".helmignore":        "*.md\n",
		// Classified, and must not be counted. Each is a different class, because
		// the failure mode is a method that keys on "did a reader produce facts"
		// rather than on the classification.
		"main.go":            "package main\n\nfunc main() {}\n",             // source
		"web/src/App.astro":  "---\nimport B from './B.vue'\n---\n<B />\n",   // source
		"web/src/Card.vue":   "<script setup>import y from './y'</script>\n", // source
		"web/src/Nav.svelte": "<script>export let items = []</script>\n",     // source
		"go.mod":             "module example.com/m\n\ngo 1.26\n",            // manifest
		"README.md":          "# hi\n",                                       // doc
		"compose.yaml":       "services:\n  api:\n    build: .\n",            // infra
		"config.json":        "{}\n",                                         // data
		"CODEOWNERS":         "* @team\n",                                    // ownership
		"api.proto":          "syntax = \"proto3\";\n",                       // contract
		"migrations/001.sql": "CREATE TABLE t (id int);\n",                   // migration
	})
	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	got := res.Unclassified()
	want := map[string]int{
		".hbs": 1, ".ejs": 1, ".css": 1, ".conf": 1, ".helmignore": 1,
	}
	if len(got) != len(want) {
		t.Errorf("Unclassified() = %v, want %v", got, want)
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("Unclassified()[%q] = %d, want %d (full: %v)", k, got[k], n, got)
		}
	}
	// Named individually so a failure says which classified file leaked in, rather
	// than only that the count moved.
	for _, k := range []string{
		".go", ".mod", ".md", ".yaml", ".json", "codeowners", ".proto", ".sql",
		// The three single-file-component extensions, named here because they are
		// the ones that moved: each was unclassified until an extractor read it,
		// and an .astro in this list is what a repository whose only frontend is
		// Astro depends on to not report itself as entirely unread.
		".astro", ".vue", ".svelte",
	} {
		if n, ok := got[k]; ok {
			t.Errorf("classified file counted as unclassified: %q (%d)", k, n)
		}
	}
}

// A binary is classified correctly and holds nothing to read, so it is not a gap in
// coverage. Counting it would bury the extensions that are gaps under the ones that
// never could be: a repository with forty PNGs would report forty unread files and
// hide the one template that is genuinely unread.
func TestUnclassifiedExcludesBinaries(t *testing.T) {
	root := writeTree(t, map[string]string{
		"web/views/page.hbs": "<h1>{{title}}</h1>\n",
	})
	if err := os.WriteFile(filepath.Join(root, "logo.png"), []byte("\x89PNG\x00\x00\x00\rIHDR"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if f := find(t, res, "logo.png"); f.Class != ClassOther || !f.Binary {
		t.Fatalf("logo.png: Class = %q, Binary = %v; want other/true — the test asserts nothing otherwise",
			f.Class, f.Binary)
	}
	got := res.Unclassified()
	if _, ok := got[".png"]; ok {
		t.Errorf("binary counted as unclassified: %v", got)
	}
	if got[".hbs"] != 1 {
		t.Errorf("Unclassified()[.hbs] = %d, want 1 (full: %v)", got[".hbs"], got)
	}
}

// An extensionless file is named by its basename, because the name is the only thing
// identifying it — and lowercased, so a bundle built on a case-insensitive
// filesystem reports the same key as one built on Linux.
func TestUnclassifiedNamesExtensionlessFilesByBasename(t *testing.T) {
	root := writeTree(t, map[string]string{
		"NOTICE":        "copyright\n",
		"tools/RUNBOOK": "step 1\n",
	})
	res, err := Walk(root, Options{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	got := res.Unclassified()
	if got["notice"] != 1 || got["runbook"] != 1 {
		t.Errorf("Unclassified() = %v, want notice and runbook counted once each", got)
	}
}

// A component's test convention is TypeScript's, because the runners are TypeScript's, and
// a story is marked a test too. The negative half is where this earns its place: a story is
// only a story for the component formats. `Widget.stories.tsx` is the same idea in a `.tsx`
// file and is deliberately *not* matched, because changing how existing TypeScript
// repositories read is a separate decision from adding these three languages — and the
// implementation shares one switch case across all five, so a rule meant for the components
// reaching the scripts is the mistake to catch.
//
// Both directions cost something real. A component wrongly marked a test drops out of the
// public surface it declares, which is a silent loss; a story counted as production reports
// a demo's imports as the component library's own.
func TestComponentTestAndStoryPaths(t *testing.T) {
	tests := []string{
		"web/src/lib/Widget.test.vue",
		"web/src/lib/Widget.spec.svelte",
		"web/src/lib/Widget.stories.svelte",
		"web/src/lib/Widget.story.vue",
		"web/tests/Page.astro",
		"web/__tests__/Page.svelte",
	}
	for _, rel := range tests {
		if !isTestPath(rel, langOfTest(rel)) {
			t.Errorf("isTestPath(%q) = false; this is a component test or a story", rel)
		}
	}
	notTests := []string{
		"web/src/lib/Widget.vue",
		"web/src/lib/Widget.svelte",
		"web/src/pages/index.astro",
		// The negative that matters: the story rule must not reach TypeScript, where
		// `.stories.tsx` is just as conventional and has never been treated as a test.
		"web/src/lib/Widget.stories.tsx",
		"web/src/lib/Widget.story.ts",
	}
	for _, rel := range notTests {
		if isTestPath(rel, langOfTest(rel)) {
			t.Errorf("isTestPath(%q) = true; this is production surface", rel)
		}
	}
}
