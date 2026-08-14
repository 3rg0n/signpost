package manifest

import (
	"sort"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// Facts is everything one extractor read out of one non-source file.
//
// One struct rather than a type per file kind, with each extractor filling the
// fields relevant to it. The reason is the same one that makes the source
// extractors return a shared Facts (see internal/extract): graph assembly is the
// consumer, and it wants "every service in this repo" and "every external
// dependency" across all file kinds at once. A per-kind type would push that union
// into the consumer, which is where it becomes N special cases instead of one.
type Facts struct {
	// Path is the repo-relative slash path of the file these facts came from.
	Path string
	// Class is what discover decided the file is, carried through so a consumer can
	// group without re-classifying.
	Class discover.Class
	// Kind names the specific reader that produced these facts, which is finer than
	// Class: a Containerfile and a compose file are both ClassInfra.
	Kind Kind

	// Module identifies the unit this manifest declares, when it declares one:
	// a Go module path, an npm package name, a crate name, a Python project name.
	Module Module
	// Deps are external dependencies. This is the manifest table's first row and the
	// single highest-value fact in the package: it is the only place a repository
	// states its supply chain exactly.
	Deps []Dep
	// Scripts are named commands the project defines — npm scripts, Makefile
	// targets, Cargo aliases. How a human is expected to build and test this.
	Scripts []Script
	// Entrypoints are the executables this manifest declares.
	Entrypoints []Entrypoint
	// Resolution is how this file says import specifiers map onto directories. Read
	// from tsconfig.json, and the only place a codebase's own aliases are stated.
	Resolution Resolution

	// Targets are the units a build file declares it builds, under the names other build
	// files in the same tree use to link against them.
	//
	// Written by the CMake reader and read by assemble, for a reason particular to CMake:
	// `target_link_libraries(app PRIVATE core)` names `core` with nothing in the syntax
	// saying whether it is a library this repository builds or a package from outside. A
	// Bazel label states which it is (`//pkg` against `@repo`) and a Terraform `source`
	// carries a `./`, so both settle it in the reader; CMake settles it only across files,
	// because the declaration is usually in the CMakeLists.txt of a subdirectory the
	// linking file merely calls add_subdirectory on. That is the ordinary layout of a C
	// project rather than an edge, and read one file at a time it turns every sibling
	// library into an external dependency with a reference page of its own.
	Targets []string

	// Services are runnable units: a compose service, a Kubernetes workload, a Helm
	// chart's deployment.
	Services []Service
	// Images are container images referenced or built, including base images.
	Images []Image
	// Jobs are CI jobs: what builds, tests, and ships, and what gates exist.
	Jobs []Job
	// Contracts are interface definitions: proto services, OpenAPI paths, GraphQL types.
	Contracts []Contract
	// Migrations are schema changes, in the order the tooling applies them.
	Migrations []Migration
	// Owners are ownership assignments read from CODEOWNERS.
	Owners []Owner
	// Rules are stated constraints read from AGENTS.md, CLAUDE.md, and ADRs.
	Rules []Rule

	// SecretRefs are the secrets this file *references*, never their values.
	//
	// The distinction is the whole point (design §4.1: "secrets *references*").
	// Knowing that a service reads DATABASE_PASSWORD from a Secret named db-creds is
	// architectural signal. The bytes in that Secret are a credential, and this
	// bundle is committed and published, so a reader that recorded the value would
	// be a credential-exfiltration path wearing a documentation tool's clothes.
	SecretRefs []SecretRef

	// Incomplete marks a file that could not be fully read, with Note explaining
	// why. Same contract as the source extractors: partial extraction is never
	// presented as complete (design §4.2).
	Incomplete bool
	Note       string
}

// Kind names the reader that produced a Facts value.
type Kind string

const (
	KindGoMod       Kind = "go.mod"
	KindPackageJSON Kind = "package.json"
	KindPyProject   Kind = "pyproject.toml"
	KindRequirement Kind = "requirements.txt"
	KindCargo       Kind = "Cargo.toml"
	KindContainer   Kind = "containerfile"
	KindCompose     Kind = "compose"
	KindWorkflow    Kind = "workflow"
	KindHelmChart   Kind = "helm-chart"
	KindHelmValues  Kind = "helm-values"
	KindKubernetes  Kind = "kubernetes"
	KindProto       Kind = "proto"
	KindOpenAPI     Kind = "openapi"
	KindGraphQL     Kind = "graphql"
	KindMigration   Kind = "migration"
	KindCodeowners  Kind = "codeowners"
	KindAgentRules  Kind = "agent-rules"
	KindADR         Kind = "adr"
	KindMakefile    Kind = "makefile"
	KindTSConfig    Kind = "tsconfig"
	KindLock        Kind = "lock"
	KindTerraform   Kind = "terraform"
	KindGemfile     Kind = "gemfile"
	KindComposer    Kind = "composer.json"
	KindMSBuild     Kind = "msbuild"
	// KindSolution is a Visual Studio solution, and it is a separate kind from
	// KindMSBuild rather than a variant of it because a .sln declares no dependencies at
	// all — only which projects exist. Folding the two would report a repository whose
	// solution file the walk reached before any .csproj as a NuGet manifest declaring
	// nothing, which reads as "this project has no dependencies" rather than as "this
	// file never listed any".
	KindSolution Kind = "solution"
	// KindCMake and KindBazel are the two build-graph readers, and they are two kinds
	// rather than one `build-graph` because they answer the same question with different
	// authority. A CMakeLists.txt is a script whose real target list only CMake computes,
	// so its facts are a best reading of a subset; a BUILD file is declarative and its
	// targets are exactly what it says. A reader deciding how far to trust a target list
	// needs to know which of the two it is looking at, and Kind is where that is stated.
	KindCMake Kind = "cmake"
	KindBazel Kind = "bazel"
)

// Resolution is a file's statement about what its own import specifiers mean.
//
// Only tsconfig.json produces one. The other ecosystems state resolution in a form the
// resolver can derive without help — a Go import path is the module path plus the
// directory, a Python package is a directory — while TypeScript lets a project invent
// arbitrary aliases, and the mapping exists nowhere except this file.
type Resolution struct {
	// BaseURL is the directory alias targets are relative to, repo-relative and already
	// joined with the config's own location. "" is the repository root.
	BaseURL string
	// Aliases are the declared patterns, in declaration order.
	Aliases []Alias
	// Extends is the config this one inherits from: a repo-relative path when it named
	// one, or the specifier as written when it named a package. Present in 11 of 14
	// tsconfig files in one real monorepo, which is why it is modelled rather than
	// ignored — a package config that declares only `extends` and `include` inherits
	// every alias it resolves by.
	Extends string
}

// Alias is one `paths` entry: a pattern and the directories it maps onto.
type Alias struct {
	// Pattern is the specifier pattern as written, wildcard included: `@fider/*`.
	Pattern string
	// Targets are the mapped locations, repo-relative and already resolved against
	// BaseURL, in declaration order. An array because TypeScript tries each in turn and
	// the first that exists wins — a real fallback, not a formality.
	Targets []string
	Line    int
}

// Module is the identity a manifest declares for its unit of code.
type Module struct {
	// Name is the module path, package name, or project name.
	Name string
	// Version is the declared version, when the manifest carries one. Absent from
	// go.mod by design, present in the other three.
	Version string
	// Ecosystem names the package manager this identity belongs to, so two
	// same-named modules in different ecosystems stay distinct.
	Ecosystem string
	// LangVersion is the required toolchain version: Go's `go 1.26.5`, Rust's
	// edition, Python's requires-python, npm's engines.node.
	LangVersion string
	// Workspaces are member paths of a monorepo declaration. Their presence is the
	// signal that a repository is a workspace rather than a single project, which
	// changes how everything downstream should be read.
	Workspaces []string
	// Private marks a package explicitly flagged as not for publication.
	Private bool
	Line    int
}

// DepScope is where a dependency is needed.
type DepScope string

const (
	// ScopeRuntime is needed to run: go.mod require, npm dependencies.
	ScopeRuntime DepScope = "runtime"
	// ScopeDev is needed only to develop or test.
	ScopeDev DepScope = "dev"
	// ScopeBuild is needed only to build: npm peer/optional, Cargo build-dependencies.
	ScopeBuild DepScope = "build"
	// ScopeIndirect is a transitive dependency recorded by the tool rather than
	// requested by a human. Distinguished because §2's whole argument turns on
	// direct-versus-transitive: a direct dependency is one we can bump ourselves.
	ScopeIndirect DepScope = "indirect"
)

// Dep is one external dependency.
type Dep struct {
	Name string
	// Version is the constraint exactly as written — "^3.30.0", ">=0.27",
	// "v1.2.3" — not a resolved version. The constraint is what the repository
	// actually states; resolution is the lock file's business, and lock files are
	// deliberately not parsed for structure.
	Version   string
	Scope     DepScope
	Ecosystem string
	// Optional marks a dependency behind a feature flag or extra.
	Optional bool
	// Source is a non-registry origin: a git URL, a local path. Worth recording
	// because a git dependency has a different supply-chain posture entirely — it
	// has no registry to publish an advisory against.
	Source string
	// Local marks Source as a repo-relative directory in *this* repository, already
	// resolved against the declaring file.
	//
	// A flag rather than a shape assemble can infer, because it cannot: a Terraform
	// module source of `modules/rds` and one of `hashicorp/vpc/aws` are both bare
	// slash-separated strings, and only the reader that saw whether the author wrote
	// `./` knows which is which. Guessing by looking for a matching directory would
	// resolve a registry module to a directory that happens to share its name.
	//
	// What it decides is whether a declaration becomes an external dependency page.
	// It must not: a page for a directory in this repository claims the repository
	// pulls its own infrastructure in from outside, which is the false claim
	// externals() excludes npm workspace siblings for. The declaration is not
	// discarded — addDeclaredDepEdges draws it onto the module holding that code.
	Local bool
	Line  int
}

// Script is a named command a project defines.
type Script struct {
	Name string
	// Command is the command text. Kept whole rather than tokenised: the point is
	// that a human can read what `make test` does.
	Command string
	Line    int
}

// Entrypoint is an executable a manifest declares.
type Entrypoint struct {
	Name string
	// Path is the source or built artifact the entrypoint runs, when stated.
	Path string
	Line int
}

// Service is a runnable unit.
type Service struct {
	Name string
	// Image is the container image it runs, when stated.
	Image string
	// Build is the build context or Containerfile it is built from, when it is built
	// rather than pulled.
	Build string
	// Ports are published port mappings, as written.
	Ports []string
	// DependsOn are other services it declares a dependency on. This is a real edge
	// no source-level import can supply: it is startup ordering and runtime
	// coupling, stated by a human.
	DependsOn []string
	// EnvKeys are the environment variable names it reads. Names only — a value in
	// a compose file is very often a credential.
	EnvKeys []string
	// Volumes are mount declarations, as written.
	Volumes []string
	// Replicas is the declared replica count, when stated.
	Replicas string
	// Kind is the workload kind for a Kubernetes service (Deployment, StatefulSet,
	// CronJob), empty for compose.
	Kind string
	// Namespace is the declared namespace, when stated.
	Namespace string
	Line      int
}

// Image is a container image reference.
type Image struct {
	// Ref is the image reference as written, including tag or digest.
	Ref string
	// Stage is the build stage name for a multi-stage Containerfile's `AS name`.
	Stage string
	// Base marks an image a build starts FROM, as opposed to one a service runs.
	Base bool
	Line int
}

// Job is one CI job.
type Job struct {
	Name string
	// Key is the job's key in the workflow's `jobs` map, which is what a `needs`
	// names. It is not the same string as Name whenever the job sets `name:`, and
	// resolving one against the other silently drops the dependency.
	Key string
	// Workflow is the name of the workflow that contains it.
	Workflow string
	// Runner is the runs-on value.
	Runner string
	// Uses is the reusable workflow this job calls instead of running steps.
	Uses string
	// Steps are the actions and commands it runs, in order.
	Steps []Step
	// Needs are jobs that must finish first.
	Needs []string
	// Gate marks a job that runs on pull_request, or on push to the default branch.
	// This is the fact §4.1 asks for by name — "what gates exist" — and it is what
	// tells a contributor which checks a change meets. Not that the job blocks: which
	// checks are *required* is branch protection, repository configuration rather than
	// anything in the tree.
	Gate bool
	// Permissions are the declared GITHUB_TOKEN permissions, which are the job's
	// blast radius.
	Permissions []string
	Line        int
}

// Step is one step in a CI job.
type Step struct {
	Name string
	// Uses is the action reference, when the step uses one.
	Uses string
	// Run is the command text, when the step runs one.
	Run  string
	Line int
}

// Contract is one interface definition.
type Contract struct {
	// Name is the service, path, or type name.
	Name string
	// Kind distinguishes what was found: "service", "message", "rpc", "path",
	// "type", "query", "mutation".
	Kind string
	// Package is the proto package or GraphQL schema namespace.
	Package string
	// Detail is the method signature, HTTP method, or field summary.
	Detail string
	Line   int
}

// Migration is one schema change.
type Migration struct {
	// Version is the ordering key the tool uses: a numeric prefix, a timestamp.
	Version string
	Name    string
	// Tables are the tables the migration creates, alters, or drops.
	Tables []string
	// Destructive marks a migration that drops a table or column. Worth flagging on
	// its own: it is the class of change an agent should never make casually, and
	// the pattern of past ones tells it how this team handles them.
	Destructive bool
	Line        int
}

// Owner is one ownership assignment from CODEOWNERS.
type Owner struct {
	// Pattern is the path pattern, as written.
	Pattern string
	// Owners are the teams or users, including the leading @.
	Owners []string
	Line   int
}

// Rule is a stated constraint from AGENTS.md, CLAUDE.md, or an ADR.
type Rule struct {
	// Heading is the section the rule was stated under, for provenance.
	Heading string
	// Text is the statement itself, verbatim. Never paraphrased: this is a human's
	// stated intent, and summarising it is the semantic pass's job (§4.5), not this
	// reader's.
	Text string
	// Status is an ADR's lifecycle state (Proposed, Accepted, Superseded).
	Status string
	Line   int
}

// SecretRef is a secret a file references.
//
// Every field here names something. None of them holds a value, and that is a
// design invariant rather than an oversight: see the Facts.SecretRefs comment.
type SecretRef struct {
	// Name is the secret's own name — a Kubernetes Secret's metadata.name, a
	// GitHub Actions secret's identifier.
	Name string
	// Key is the specific key read out of that secret, when one is named.
	Key string
	// EnvVar is the environment variable the value is bound to, when stated.
	EnvVar string
	// Source describes where the reference lives: "kubernetes-secret",
	// "github-secret", "env-file", "compose-secret", "helm-values".
	Source string
	// Service names the service that makes this reference, when the file states one.
	//
	// Empty means file-scoped: the reference is real, but nothing in the file says
	// which unit reads it — a compose top-level `secrets:` declaration, a workflow
	// secret, an OpenAPI security scheme. The distinction is load-bearing rather
	// than descriptive, because a Facts is per file and a compose file routinely
	// declares a dozen services. Without this field the only link back was the
	// filename, so every service in the file inherited every secret named anywhere
	// in it, and a reverse proxy that declares no environment at all was reported
	// as reading the database password. That is a false architectural claim in the
	// direction that matters most: it tells a reader a credential is reachable from
	// somewhere it is not.
	Service string
	// Unattributed marks a reference that belongs to the file as a whole and must not
	// be handed to any of the units in it.
	//
	// The third state Service alone cannot express. An empty Service means "shared with
	// this file's services", which is what a compose top-level `secrets:` block is: the
	// file declares credentials for the services beside it without saying which reads
	// which, so handing them to all of them trades a false claim for no claim at all.
	//
	// A Terraform `variable "db_password"` is not that. One `.tf` file holds a dozen
	// unrelated resources, and a sensitive variable is an input to the configuration —
	// which resource references it is stated in an expression this reader does not
	// evaluate. Shared with all of them, it reported an ECS service and an S3 state
	// backend as each reading three credentials neither of them names.
	//
	// So the reference is kept and deliberately attributed to nothing. It stays in
	// SecretNames, which asks whether a file touches credentials at all, and it reaches
	// no page — a fact with nowhere to go rather than a fact in the wrong place.
	Unattributed bool
	Line         int
}

// Normalize sorts and dedupes every fact list so two readings of the same file
// produce identical output regardless of traversal order.
//
// Sorting is by identity, never by line, for the same reason it is in the source
// extractors: reordering a manifest's keys without changing its meaning must not
// change the bundle, or CI's committed output churns on a cosmetic edit (design §8.1).
//
// One exception, deliberate: Job.Steps and Migrations keep their file order, because
// order *is* their meaning. A CI job's steps run in sequence and a migration series
// applies in sequence; sorting either would report something false.
func (f *Facts) Normalize() {
	sort.Slice(f.Deps, func(i, j int) bool {
		a, b := f.Deps[i], f.Deps[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Scope != b.Scope {
			return a.Scope < b.Scope
		}
		return a.Version < b.Version
	})
	f.Deps = dedupeDeps(f.Deps)

	sort.Slice(f.Scripts, func(i, j int) bool { return f.Scripts[i].Name < f.Scripts[j].Name })
	sort.Slice(f.Entrypoints, func(i, j int) bool {
		if f.Entrypoints[i].Name != f.Entrypoints[j].Name {
			return f.Entrypoints[i].Name < f.Entrypoints[j].Name
		}
		return f.Entrypoints[i].Path < f.Entrypoints[j].Path
	})
	sort.Slice(f.Services, func(i, j int) bool {
		if f.Services[i].Name != f.Services[j].Name {
			return f.Services[i].Name < f.Services[j].Name
		}
		return f.Services[i].Kind < f.Services[j].Kind
	})
	for i := range f.Services {
		s := &f.Services[i]
		s.Ports = sortedUnique(s.Ports)
		s.DependsOn = sortedUnique(s.DependsOn)
		s.EnvKeys = sortedUnique(s.EnvKeys)
		s.Volumes = sortedUnique(s.Volumes)
	}
	sort.Slice(f.Images, func(i, j int) bool {
		if f.Images[i].Ref != f.Images[j].Ref {
			return f.Images[i].Ref < f.Images[j].Ref
		}
		return f.Images[i].Stage < f.Images[j].Stage
	})
	f.Images = dedupeImages(f.Images)

	sort.Slice(f.Jobs, func(i, j int) bool { return f.Jobs[i].Name < f.Jobs[j].Name })
	for i := range f.Jobs {
		f.Jobs[i].Needs = sortedUnique(f.Jobs[i].Needs)
		f.Jobs[i].Permissions = sortedUnique(f.Jobs[i].Permissions)
		// Steps keep file order: a job's steps run in sequence.
	}
	sort.Slice(f.Contracts, func(i, j int) bool {
		a, b := f.Contracts[i], f.Contracts[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Detail < b.Detail
	})
	// Migrations keep file order, which the caller established by sorted path: the
	// sequence is the data model's history and re-sorting it would misreport it.
	for i := range f.Migrations {
		f.Migrations[i].Tables = sortedUnique(f.Migrations[i].Tables)
	}
	sort.Slice(f.Owners, func(i, j int) bool { return f.Owners[i].Pattern < f.Owners[j].Pattern })
	for i := range f.Owners {
		f.Owners[i].Owners = sortedUnique(f.Owners[i].Owners)
	}
	sort.Slice(f.Rules, func(i, j int) bool {
		if f.Rules[i].Heading != f.Rules[j].Heading {
			return f.Rules[i].Heading < f.Rules[j].Heading
		}
		return f.Rules[i].Text < f.Rules[j].Text
	})
	sort.Slice(f.SecretRefs, func(i, j int) bool {
		a, b := f.SecretRefs[i], f.SecretRefs[j]
		// Service leads, because it is the outer scope: it keeps one service's
		// references adjacent, which is what makes the dedupe below — a scan of
		// neighbours — fold within a service and never across two.
		if a.Service != b.Service {
			return a.Service < b.Service
		}
		// Attribution leads the rest for the same reason Service does: it keeps the two
		// kinds apart so the neighbour scan below cannot fold across them.
		if a.Unattributed != b.Unattributed {
			return !a.Unattributed
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return a.EnvVar < b.EnvVar
	})
	f.SecretRefs = dedupeSecretRefs(f.SecretRefs)
	f.Module.Workspaces = sortedUnique(f.Module.Workspaces)
	// A target list is a set of names, so it sorts and dedupes: unlike Resolution.Targets
	// below, nothing walks it in order — assemble only ever asks whether a name is in it.
	f.Targets = sortedUnique(f.Targets)

	// Aliases sort by pattern, and their Targets deliberately do not. A pattern is an
	// identity and reordering the `paths` object must not change the bundle; a target list
	// is a fallback sequence TypeScript walks in order, so sorting it would change which
	// directory a specifier resolves to. Same distinction as Job.Steps.
	sort.Slice(f.Resolution.Aliases, func(i, j int) bool {
		return f.Resolution.Aliases[i].Pattern < f.Resolution.Aliases[j].Pattern
	})
}

// dedupeDeps folds repeated declarations of the same dependency.
//
// A dependency named twice in the same scope is one dependency. The scope is part of
// the identity, though: a package in both dependencies and devDependencies is a real
// and meaningful duplication, and collapsing it would lose the fact that it ships.
func dedupeDeps(in []Dep) []Dep {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, d := range in[1:] {
		last := &out[len(out)-1]
		if last.Name == d.Name && last.Scope == d.Scope && last.Version == d.Version {
			if last.Source == "" {
				last.Source = d.Source
				// Local travels with the Source it describes. Taking one without the
				// other would leave a repo-relative directory unflagged, which is an
				// external dependency page for a directory in this repository.
				last.Local = d.Local
			}
			continue
		}
		out = append(out, d)
	}
	return out
}

func dedupeImages(in []Image) []Image {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, im := range in[1:] {
		last := &out[len(out)-1]
		if last.Ref == im.Ref && last.Stage == im.Stage {
			// A reference that is both a base and a runtime image is a base image:
			// the stronger claim is the one worth keeping.
			last.Base = last.Base || im.Base
			continue
		}
		out = append(out, im)
	}
	return out
}

// dedupeSecretRefs folds repeated references to the same secret.
//
// The identity is the pair that locates the credential — the secret's name and the key
// inside it. Two references to that pair are one fact even when different readers found
// them by different routes, which happens routinely: a workload's `secretKeyRef` is
// found once by the container walk that knows which variable it feeds and once by the
// key-name sweep that catches the same shape in resource kinds this package does not
// model. A bare reference is subsumed by one that names the variable, because the bound
// form says everything the bare one does and more. Keeping both would report two
// secrets where there is one, and the more informative record would be the one a reader
// scanning for a name never reaches.
//
// A secret bound to two *different* variables is genuinely two facts and stays two.
//
// So is the same secret read by two different services. Two services reading one
// credential is precisely the coupling this reader exists to surface, and folding
// them would erase one of the two readers — so Service is part of the identity.
//
// And so is attribution. A file can name one credential both as an input it does not
// attribute and as something a named unit reads; folding those would silently pick one
// of the two claims, and which one it picked would depend on sort order.
func dedupeSecretRefs(in []SecretRef) []SecretRef {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		last := &out[len(out)-1]
		if last.Unattributed != s.Unattributed ||
			last.Service != s.Service || last.Name != s.Name || last.Key != s.Key {
			out = append(out, s)
			continue
		}
		switch last.EnvVar {
		case s.EnvVar:
			if last.Source == "" {
				last.Source = s.Source
			}
		case "":
			// EnvVar sorts ascending, so the bare reference arrives first and the bound
			// one replaces it — including its line, which points at the binding rather
			// than at whichever sweep happened to see the name.
			last.EnvVar, last.Source, last.Line = s.EnvVar, s.Source, s.Line
		default:
			out = append(out, s)
		}
	}
	return out
}

// markIncomplete records that a reading was partial, appending to any existing note.
func (f *Facts) markIncomplete(note string) {
	if note == "" {
		return
	}
	f.Incomplete = true
	if f.Note == "" {
		f.Note = note
		return
	}
	if strings.Contains(f.Note, note) {
		return
	}
	f.Note += "; " + note
}

// applyDiag folds a reader's diagnostics into the facts.
func (f *Facts) applyDiag(d Diag) {
	f.markIncomplete(d.Summary())
}

// DepNames returns the dependency names, sorted and deduped, for tests and for the
// graph's external-dependency nodes.
func (f *Facts) DepNames() []string {
	out := make([]string, 0, len(f.Deps))
	for _, d := range f.Deps {
		out = append(out, d.Name)
	}
	return sortedUnique(out)
}

// DirectDepNames returns only the dependencies a human asked for, excluding the
// transitive ones a tool recorded. This is the list §2's dependency policy is about.
func (f *Facts) DirectDepNames() []string {
	out := make([]string, 0, len(f.Deps))
	for _, d := range f.Deps {
		if d.Scope == ScopeIndirect {
			continue
		}
		out = append(out, d.Name)
	}
	return sortedUnique(out)
}

// ServiceNames returns service names, sorted.
func (f *Facts) ServiceNames() []string {
	out := make([]string, 0, len(f.Services))
	for _, s := range f.Services {
		out = append(out, s.Name)
	}
	return sortedUnique(out)
}

// ImageRefs returns image references, sorted.
func (f *Facts) ImageRefs() []string {
	out := make([]string, 0, len(f.Images))
	for _, im := range f.Images {
		out = append(out, im.Ref)
	}
	return sortedUnique(out)
}

// JobNames returns job names, sorted.
func (f *Facts) JobNames() []string {
	out := make([]string, 0, len(f.Jobs))
	for _, j := range f.Jobs {
		out = append(out, j.Name)
	}
	return sortedUnique(out)
}

// SecretNames returns every referenced secret name, sorted. Names, never values.
//
// File-scoped: this is the whole file's set, regardless of which service reads what.
// Correct for asking "does this file touch credentials at all", wrong for attributing
// them to a service — use SecretNamesFor for that.
func (f *Facts) SecretNames() []string {
	out := make([]string, 0, len(f.SecretRefs))
	for _, s := range f.SecretRefs {
		out = append(out, s.Name)
	}
	return sortedUnique(out)
}

// SecretNamesFor returns the secret names attributable to one service, sorted.
//
// That means the references the file attributed to this service by name, plus the
// ones it attributed to no service at all. The second half is deliberate: a compose
// top-level `secrets:` block declares credentials for the file's services without
// saying which, and dropping those would trade a false claim for a missing one.
// A reference naming a *different* service is never included, which is the whole
// point — that is the misattribution this function exists to prevent. Nor is one the
// reader marked Unattributed, which is the same prevention for a file whose units are
// unrelated to each other rather than sharing a declaration.
func (f *Facts) SecretNamesFor(service string) []string {
	out := make([]string, 0, len(f.SecretRefs))
	for _, s := range f.SecretRefs {
		if s.Unattributed {
			continue
		}
		if s.Service == "" || s.Service == service {
			out = append(out, s.Name)
		}
	}
	return sortedUnique(out)
}
