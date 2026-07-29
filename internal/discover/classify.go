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
	LangOther  Lang = "other"
)

// sourceExts maps an extension to its language. The four first-class languages
// get real extractors (design §4.1); everything else is LangOther and reaches
// only the generic extractor.
var sourceExts = map[string]Lang{
	".go":    LangGo,
	".ts":    LangTS,
	".tsx":   LangTS,
	".mts":   LangTS,
	".cts":   LangTS,
	".js":    LangJS,
	".jsx":   LangJS,
	".mjs":   LangJS,
	".cjs":   LangJS,
	".py":    LangPython,
	".pyi":   LangPython,
	".rs":    LangRust,
	".java":  LangOther,
	".kt":    LangOther,
	".rb":    LangOther,
	".c":     LangOther,
	".h":     LangOther,
	".cc":    LangOther,
	".cpp":   LangOther,
	".hpp":   LangOther,
	".cs":    LangOther,
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
	"pom.xml":             true,
	"build.gradle":        true,
	"build.gradle.kts":    true,
	"Makefile":            true,
	"Justfile":            true,
	"justfile":            true,
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

// isTestPath reports whether a path looks like a test, by the conventions of the
// four first-class languages. Tests are kept — they are the best evidence of how
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
	}
	return containsDir(rel, "tests") || containsDir(rel, "test")
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
