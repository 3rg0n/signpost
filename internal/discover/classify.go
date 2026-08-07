package discover

import (
	"path"
	"strings"
)

// Class is what kind of thing a file is, which decides which extractor sees it.
type Class string

const (
	ClassSource    Class = "source"   // code, dispatched by Lang
	ClassManifest  Class = "manifest" // dependency/build manifest
	ClassInfra     Class = "infra"    // containers, compose, workflows, k8s, helm
	ClassContract  Class = "contract" // proto, OpenAPI, GraphQL SDL
	ClassMigration Class = "migration"
	ClassDoc       Class = "doc"       // markdown, rst, adoc
	ClassOwnership Class = "ownership" // CODEOWNERS, AGENTS.md, CLAUDE.md
	ClassData      Class = "data"      // json/yaml/toml that is not a known manifest
	ClassOther     Class = "other"
)

// Lang is the source language, empty for non-source files.
type Lang string

const (
	LangGo     Lang = "go"
	LangTS     Lang = "typescript"
	LangJS     Lang = "javascript"
	LangPython Lang = "python"
	LangRust   Lang = "rust"
	LangJava   Lang = "java"
	LangKotlin Lang = "kotlin"
	LangC      Lang = "c"
	LangCpp    Lang = "cpp"
	LangObjC   Lang = "objc"
	LangRuby   Lang = "ruby"
	LangPHP    Lang = "php"
	LangCSharp Lang = "csharp"
	LangOther  Lang = "other"
)

// sourceExts maps an extension to its language. A language with a real extractor
// (design §4.1) gets its own Lang; everything else is LangOther and reaches only
// the generic extractor.
var sourceExts = map[string]Lang{
	".go":   LangGo,
	".ts":   LangTS,
	".tsx":  LangTS,
	".mts":  LangTS,
	".cts":  LangTS,
	".js":   LangJS,
	".jsx":  LangJS,
	".mjs":  LangJS,
	".cjs":  LangJS,
	".py":   LangPython,
	".pyi":  LangPython,
	".rs":   LangRust,
	".java": LangJava,
	".kt":   LangKotlin,
	// A .kts script is Kotlin the extractor can read, and the two whose names make
	// them build files — build.gradle.kts, settings.gradle.kts — never reach here:
	// manifestNames matches them by basename first.
	".kts": LangKotlin,
	".c":   LangC,
	// A .h is C, C++, or Objective-C, and no filename can say which: the same
	// extension serves all three and only the content distinguishes them.
	// Classification here is name-based by design — the walk stays filename-only so
	// it is cheap and deterministic — so a header is labelled C, the family's lowest
	// common denominator, and the extractor reads the whole family's syntax
	// regardless of which Lang dispatched it. A C++ class in a .h is still recorded;
	// what the label loses is only the dialect's name, which is the honest limit of
	// what a filename carries.
	".h":   LangC,
	".cc":  LangCpp,
	".cpp": LangCpp,
	".cxx": LangCpp,
	".hpp": LangCpp,
	".hh":  LangCpp,
	".hxx": LangCpp,
	// Objective-C's .m is unambiguous; .mm is Objective-C++ and reads as the same
	// language here, since every construct .mm adds is one the extractor already
	// handles for C++.
	".m":  LangObjC,
	".mm": LangObjC,
	".rb": LangRuby,
	// Rakefile and Gemfile are Ruby too, but they are build files: manifestNames
	// matches them by basename before this map is consulted, which is the same
	// precedence build.gradle.kts relies on. A `.rake` file stays source: it holds task
	// definitions written in Ruby, which is code, where a `.gemspec` holds only
	// assignments and so is matched as a manifest instead.
	".rake":  LangRuby,
	".php":   LangPHP,
	".cs":    LangCSharp,
	".swift": LangOther,
	".scala": LangOther,
	".sh":    LangOther,
	".bash":  LangOther,
	".ps1":   LangOther,
	".sql":   LangOther,
	".tf":    LangOther,
}

// manifestNames are exact basenames that identify a dependency or build manifest.
var manifestNames = map[string]bool{
	"go.mod":              true,
	"go.sum":              true,
	"go.work":             true,
	"package.json":        true,
	"pnpm-workspace.yaml": true,
	"pyproject.toml":      true,
	"requirements.txt":    true,
	"setup.py":            true,
	"setup.cfg":           true,
	"Cargo.toml":          true,
	"Gemfile":             true,
	"Rakefile":            true,
	"rakefile":            true,
	"composer.json":       true,
	// MSBuild's two shared-configuration files. Matched by name and not by the
	// .props extension, because an arbitrary .props file is build logic while these
	// two are fixed by the toolchain and Directory.Packages.props is where Central
	// Package Management declares every version the solution uses.
	"Directory.Build.props":    true,
	"Directory.Packages.props": true,
	"pom.xml":                  true,
	"build.gradle":             true,
	"build.gradle.kts":         true,
	"Makefile":                 true,
	"Justfile":                 true,
	"justfile":                 true,
}

// manifestExts identify a build manifest by extension rather than by basename.
//
// This exists for exactly one ecosystem. Every other build manifest has a fixed
// name the toolchain requires — go.mod, Cargo.toml, pom.xml — so a basename match
// finds it. MSBuild instead names the project after the project: the file is
// Ordering.Api.csproj, and no list of names can hold it. The extension is the only
// stable part.
// The .gemspec is here for a related reason with a different cause. Its name is chosen by
// the author — and required to match the gem — so no basename holds it either; what makes
// it a manifest rather than the Ruby source its extension suggests is that a gemspec's
// entire content is metadata assignments. It declares the gem's dependencies and nothing
// callable, so reading it as source would report a file of assignments as a module with no
// symbols while its dependency list went unread.
var manifestExts = map[string]bool{
	".csproj":  true,
	".fsproj":  true,
	".vbproj":  true,
	".sln":     true,
	".gemspec": true,
}

// lockNames are manifests we record but never parse for structure: they are
// enormous, wholly derived from a manifest we already read, and contribute no
// architectural signal. Recognising them explicitly keeps them out of ClassData,
// where they would otherwise be ingested as generic data files.
var lockNames = map[string]bool{
	"go.sum":            true,
	"package-lock.json": true,
	"pnpm-lock.yaml":    true,
	"yarn.lock":         true,
	"Cargo.lock":        true,
	"uv.lock":           true,
	"poetry.lock":       true,
	"Gemfile.lock":      true,
	"composer.lock":     true,
	// NuGet's, which is opt-in: it exists only when a project sets
	// RestorePackagesWithLockFile, which is why a .NET repository usually has none.
	"packages.lock.json": true,
}

// ownershipNames carry stated human intent: who owns this, and what the rules
// are. These are inputs to signpost and are never written by it (design §6.2).
var ownershipNames = map[string]bool{
	"CODEOWNERS": true,
	"AGENTS.md":  true,
	"CLAUDE.md":  true,
	"OWNERS":     true,
}

// classify determines the Class and Lang of a root-relative slash path.
//
// Order matters: the most specific signal wins. A file named docker-compose.yml
// is infra, not data, even though it is YAML; a file named CODEOWNERS is
// ownership even though it has no extension.
func classify(rel string) (Class, Lang) {
	base := path.Base(rel)
	ext := strings.ToLower(path.Ext(base))
	lower := strings.ToLower(base)

	// Lock files first: several are also in manifestNames (go.sum) and they must
	// classify as manifests we skip parsing, not as data to ingest.
	if lockNames[base] {
		return ClassManifest, ""
	}
	if ownershipNames[base] {
		return ClassOwnership, ""
	}
	// ADRs and docs directories are documents regardless of depth.
	if ext == ".md" || ext == ".markdown" || ext == ".rst" || ext == ".adoc" || ext == ".txt" {
		return ClassDoc, ""
	}
	if manifestNames[base] {
		return ClassManifest, ""
	}
	// The .NET project and solution files are the only build manifests whose name
	// is chosen by the author rather than fixed by the toolchain, so they are the
	// one family that cannot be matched by basename. The extension is the whole
	// signal: Foo.csproj, Foo.fsproj, Foo.vbproj, Foo.sln.
	if manifestExts[ext] {
		return ClassManifest, ""
	}
	if c, ok := classifyInfra(rel, base, lower, ext); ok {
		return c, ""
	}
	if c, ok := classifyContract(rel, lower, ext); ok {
		return c, ""
	}
	if isMigration(rel) {
		return ClassMigration, ""
	}
	if lang, ok := sourceExts[ext]; ok {
		return ClassSource, lang
	}
	switch ext {
	case ".json", ".yaml", ".yml", ".toml", ".ini", ".env", ".properties":
		return ClassData, ""
	}
	return ClassOther, ""
}

// classifyInfra recognises deployment and CI surface.
func classifyInfra(rel, base, lower, ext string) (Class, bool) {
	// Containerfiles: "Dockerfile", "Containerfile", and their ".dev"/"-prod"
	// variants in either order ("Dockerfile.dev", "dev.Dockerfile").
	if lower == "dockerfile" || lower == "containerfile" ||
		strings.HasPrefix(lower, "dockerfile.") || strings.HasPrefix(lower, "containerfile.") ||
		strings.HasSuffix(lower, ".dockerfile") || strings.HasSuffix(lower, ".containerfile") {
		return ClassInfra, true
	}
	// Compose files: docker-compose.yml, compose.yaml, compose.prod.yml.
	if strings.HasPrefix(lower, "docker-compose") || strings.HasPrefix(lower, "compose.") || lower == "compose.yml" || lower == "compose.yaml" {
		if ext == ".yml" || ext == ".yaml" {
			return ClassInfra, true
		}
	}
	// GitHub Actions workflows and reusable actions.
	if strings.HasPrefix(rel, ".github/workflows/") && (ext == ".yml" || ext == ".yaml") {
		return ClassInfra, true
	}
	if lower == "action.yml" || lower == "action.yaml" {
		return ClassInfra, true
	}
	// Helm: Chart.yaml identifies a chart; values and templates ride along.
	if lower == "chart.yaml" || lower == "chart.yml" {
		return ClassInfra, true
	}
	if strings.HasPrefix(lower, "values") && (ext == ".yaml" || ext == ".yml") {
		return ClassInfra, true
	}
	if containsDir(rel, "templates") && (ext == ".yaml" || ext == ".yml" || ext == ".tpl") {
		return ClassInfra, true
	}
	// Plain k8s manifests live under conventional directories. Content sniffing
	// for apiVersion/kind happens in the infra extractor; the walk stays
	// filename-only so classification is cheap and deterministic.
	if (containsDir(rel, "k8s") || containsDir(rel, "kubernetes") || containsDir(rel, "manifests") || containsDir(rel, "deploy")) &&
		(ext == ".yaml" || ext == ".yml") {
		return ClassInfra, true
	}
	if ext == ".tf" || ext == ".tfvars" {
		return ClassInfra, true
	}
	return "", false
}

// classifyContract recognises interface definitions.
func classifyContract(rel, lower, ext string) (Class, bool) {
	if ext == ".proto" {
		return ClassContract, true
	}
	if ext == ".graphql" || ext == ".graphqls" || ext == ".gql" {
		return ClassContract, true
	}
	// OpenAPI/Swagger by conventional filename. A YAML file that merely happens
	// to contain an "openapi:" key is caught later by the contract extractor,
	// which may read content; classification here stays name-based.
	if strings.HasPrefix(lower, "openapi") || strings.HasPrefix(lower, "swagger") {
		if ext == ".yaml" || ext == ".yml" || ext == ".json" {
			return ClassContract, true
		}
	}
	if containsDir(rel, "openapi") && (ext == ".yaml" || ext == ".yml" || ext == ".json") {
		return ClassContract, true
	}
	return "", false
}

// isMigration recognises schema migrations, which are the data model's history.
func isMigration(rel string) bool {
	return containsDir(rel, "migrations") || containsDir(rel, "migrate") || containsDir(rel, "db/migration")
}

// containsDir reports whether any path segment equals name (case-insensitively),
// excluding the final segment, which is the filename.
func containsDir(rel, name string) bool {
	segs := strings.Split(rel, "/")
	if len(segs) < 2 {
		return false
	}
	// Multi-segment names like "db/migration" are matched as a run of segments.
	want := strings.Split(strings.ToLower(name), "/")
	dirs := segs[:len(segs)-1]
	for i := 0; i+len(want) <= len(dirs); i++ {
		ok := true
		for j, w := range want {
			if strings.ToLower(dirs[i+j]) != w {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// isTestPath reports whether a path looks like a test, by the conventions of each
// language signpost reads. Tests are kept — they are the best evidence of how
// an interface is meant to be used, and they source the tested_by edge — but they
// are marked so they never masquerade as production surface.
func isTestPath(rel string, lang Lang) bool {
	base := strings.ToLower(path.Base(rel))
	switch lang {
	case LangGo:
		return strings.HasSuffix(base, "_test.go")
	case LangPython:
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") ||
			containsDir(rel, "tests") || containsDir(rel, "test")
	case LangTS, LangJS:
		for _, s := range []string{".test.", ".spec."} {
			if strings.Contains(base, s) {
				return true
			}
		}
		return containsDir(rel, "__tests__") || containsDir(rel, "tests") || containsDir(rel, "test")
	case LangRust:
		return containsDir(rel, "tests") || base == "tests.rs"
	case LangC, LangCpp, LangObjC:
		// C has no test convention the toolchain enforces, so several coexist and all
		// of them are in wide use: GoogleTest's `_test.cc` and `_unittest.cc`, CTest's
		// `test_` prefix, and Xcode's `FooTests.m`. The directory fallback catches
		// `test/` and `tests/` trees, which is where autotools and CMake projects
		// conventionally put them.
		return containsDir(rel, "test") || containsDir(rel, "tests") ||
			cTestBasename(path.Base(rel))
	case LangRuby:
		// RSpec and minitest are the two conventions in use, and they disagree about
		// everything except the delimiter: `_spec.rb` under `spec/`, `_test.rb` under
		// `test/`. Both directory names are caught by the fallback below.
		return containsDir(rel, "spec") || containsDir(rel, "test") || containsDir(rel, "tests") ||
			strings.HasSuffix(base, "_spec.rb") || strings.HasSuffix(base, "_test.rb")
	case LangPHP:
		// PHPUnit names a test class FooTest and requires the file to match the class,
		// so the capital is the signal — the same case-sensitivity jvmTestBasename
		// documents, and for the same reason: `Manifest.php` is not a test.
		return containsDir(rel, "test") || containsDir(rel, "tests") ||
			phpTestBasename(path.Base(rel))
	case LangCSharp:
		// A .NET test project is conventionally a whole directory — Foo.Tests — and
		// the segment check finds it. The basename forms are xUnit's and NUnit's.
		return containsDir(rel, "test") || containsDir(rel, "tests") ||
			csharpTestDir(rel) || jvmTestBasename(strings.TrimSuffix(path.Base(rel), ".cs"))
	case LangJava, LangKotlin:
		// src/test/java and src/test/kotlin are where Maven and Gradle put tests, and
		// both are caught by the "test" segment the fallback already checks. The
		// basename conventions are what needs stating: a JVM test is routinely a class
		// in the same source set as the code it exercises, named for it.
		return containsDir(rel, "test") || containsDir(rel, "tests") ||
			jvmTestBasename(path.Base(rel))
	}
	return containsDir(rel, "tests") || containsDir(rel, "test")
}

// cTestBasename reports whether a C-family filename names a test file.
//
// C has no test convention its toolchain enforces, so the ones in use are conventions
// of separate test frameworks and all of them appear in real trees: GoogleTest's
// `_test.cc` and `_unittest.cc`, CTest's `test_` prefix, and Xcode's `ReaderTests.m`.
//
// Case matters, and in two different ways, which is why the delimited forms are matched
// against the lowercased name and the undelimited one is not:
//
//   - `_test`, `-test` and `test_` carry a delimiter, and the delimiter is what makes
//     the boundary. Case adds nothing, and `Buffer_Test.c` is a test.
//   - `Tests` with no delimiter has only its capital. Lowercased, `protests.c` and
//     `contests.cpp` end in "tests" and would be marked as tests — and marking
//     production code as a test drops it out of the public surface it declares, which is
//     a silent loss of real API rather than a noisy false positive. So that form is
//     matched case-sensitively, which is the same call jvmTestBasename makes and for the
//     same reason.
//
// `latest.c` ends in the letters of `test` and is not one, under either rule.
func cTestBasename(base string) bool {
	name := base
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	if name == "" {
		return false
	}
	lower := strings.ToLower(name)
	if lower == "test" || lower == "tests" {
		return true
	}
	for _, suffix := range []string{"_test", "_tests", "_unittest", "-test", "-tests"} {
		if strings.HasSuffix(lower, suffix) && len(lower) > len(suffix) {
			return true
		}
	}
	for _, prefix := range []string{"test_", "test-"} {
		if strings.HasPrefix(lower, prefix) && len(lower) > len(prefix) {
			return true
		}
	}
	// Xcode's convention, capital and all: `ReaderTests.m`, `ReaderTest.mm`.
	for _, suffix := range []string{"Tests", "Test"} {
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix) {
			return true
		}
	}
	return false
}

// jvmTestBasename reports whether a JVM filename names a test class.
//
// Case-sensitive on purpose, against the file's own name rather than the lowercased
// one the caller holds. A JVM test class is named `FooTest`, and the capital is the
// whole signal: matched case-insensitively, `Latest.java` and `Manifest.kt` are tests,
// and marking a production class as one drops it out of the public surface it
// declares. `Test` as a prefix is JUnit 3's convention and still common.
func jvmTestBasename(base string) bool {
	name := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(base,
		".java"), ".kt"), ".kts")
	if name == "" {
		return false
	}
	for _, suffix := range []string{"Test", "Tests", "TestCase", "Spec", "IT"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	// `TestFoo` is a test; `Tester` and `Testament` are not, so the next character has
	// to begin a new word.
	if rest, ok := strings.CutPrefix(name, "Test"); ok && rest != "" {
		return rest[0] >= 'A' && rest[0] <= 'Z'
	}
	return false
}

// phpTestBasename reports whether a PHP filename names a test class.
//
// PHPUnit discovers tests by class name — a class extending TestCase, conventionally
// named FooTest — and PSR-4 requires the file to be named for the class it holds. So
// the filename carries the convention, capital and all.
//
// Case-sensitive, for the reason jvmTestBasename gives: lowercased, `Manifest.php`
// and `Latest.php` end in the letters of "test", and marking production code as a
// test silently drops the API it declares out of the public surface.
func phpTestBasename(base string) bool {
	name := strings.TrimSuffix(base, ".php")
	if name == "" {
		return false
	}
	for _, suffix := range []string{"Test", "TestCase", "Spec"} {
		if strings.HasSuffix(name, suffix) && len(name) > len(suffix) {
			return true
		}
	}
	return false
}

// csharpTestDir reports whether any directory segment names a .NET test project.
//
// The .NET convention is a sibling project rather than a directory inside the one
// under test: Ordering.Api and Ordering.Api.Tests. So the signal is a segment
// *ending* in ".Tests", which containsDir cannot express — it matches a segment
// equal to a name, and `Ordering.Api.Tests` equals nothing.
//
// Matched case-sensitively on the ".Tests" and ".Test" suffixes only. The dot is
// what makes the boundary safe: a project legitimately named `Contests` is not
// caught, and neither is one named `LatestApi`.
func csharpTestDir(rel string) bool {
	segs := strings.Split(rel, "/")
	if len(segs) < 2 {
		return false
	}
	for _, d := range segs[:len(segs)-1] {
		for _, suffix := range []string{".Tests", ".Test", ".UnitTests", ".IntegrationTests"} {
			if strings.HasSuffix(d, suffix) {
				return true
			}
		}
	}
	return false
}

// isVendored reports whether a path is third-party code checked into the tree.
// Vendored code is discovered but not analysed as the repo's own surface: it
// would swamp the graph with nodes nobody on the team can change.
//
// This is deliberately separate from .gitignore — vendor/ and node_modules are
// frequently committed, so the ignore file will not exclude them.
func isVendored(rel string) bool {
	for _, d := range []string{
		"vendor", "node_modules", "third_party", "thirdparty", "external",
		".venv", "venv", "site-packages", "dist", "build", "target",
		".git", ".idea", ".vscode", "__pycache__", ".mypy_cache",
		".pytest_cache", ".ruff_cache", ".tox", "coverage", ".next",
		".nuxt", ".svelte-kit", ".terraform", "bin", "obj",
	} {
		if containsDir(rel, d) {
			return true
		}
	}
	return false
}

// isFixture reports whether a path is a sample project kept for tests to run
// against, rather than code that ships.
//
// This is not a tidiness rule, it is a correctness one, and signpost found it by
// biting itself. Adding testdata/corpus — four sample projects, deliberately built
// to look like real repositories — put `testdata/corpus/ts/app/(marketing)` in
// signpost's own index as a module and react, httpx and serde in it as
// dependencies. Worse, it reached practices.md, which cited
// `testdata/corpus/py/pyproject.toml` as evidence about how *signpost* pins its
// dependencies. A bundle is committed and read by people who did not build it
// (design §4.6), so that is not noise: it is a page confidently describing a
// repository that does not exist.
//
// The fixture is a distinct thing from the two neighbours it sits between, which
// is why it is neither of them:
//
//   - Not vendored. Vendored code is somebody else's, unchangeable by this team.
//     A fixture is this repository's own, hand-maintained, and reviewed — its
//     content is load-bearing for the test suite.
//   - Not a test. A test file exercises the repository's own surface and earns a
//     tested_by edge pointing at it. A fixture is the *subject* of a test, and an
//     edge from a real module to a sample project would be a false claim.
//
// `testdata` is the strongest possible signal because it is toolchain-defined
// rather than a convention: the go command ignores it outright, so a Go
// repository cannot use the name for shipping code. `fixtures` and `__fixtures__`
// are conventions, included because the cost of being wrong is asymmetric — a
// missed fixture puts a phantom module on a committed page, while a
// misclassified real directory named `fixtures` loses nodes that a reader can see
// are missing and recover with -include-fixtures.
func isFixture(rel string) bool {
	for _, d := range []string{"testdata", "fixtures", "__fixtures__"} {
		if containsDir(rel, d) {
			return true
		}
	}
	return false
}
