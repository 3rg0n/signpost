package manifest

import (
	"path"
	"sort"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// Reader reads one file into Facts.
//
// Unlike extract.Extractor there is no error return, and the reason is the shape of the
// problem rather than a preference. A source extractor either parses a file or does not;
// a manifest reader reads a file whose format it can only partly rely on — a Helm
// template is not YAML, a `.yaml` under deploy/ may hold anything — and a partial reading
// is the normal outcome, not a failure. That case is carried by Facts.Incomplete, which
// keeps the partial facts. An error would discard them.
type Reader func(discover.File) Facts

// Route is one entry in the dispatch table: a predicate over a file and the reader that
// claims it.
type Route struct {
	// Kind is what the reader produces, recorded here so the registry can report its
	// own coverage without running anything.
	Kind Kind
	// Match decides whether this route claims the file. Earlier routes win, so a Match
	// may assume every route above it already declined.
	Match func(discover.File) bool
	// Read produces the facts.
	Read Reader
}

// Registry dispatches non-source files to readers.
//
// The dispatch is by name and path rather than by a single key, which is the difference
// from extract.Registry: discover.Class is too coarse to route on (a Containerfile, a
// compose file, a workflow, and a Kubernetes manifest are all ClassInfra) and there is no
// finer key on the file to map from. So the table is ordered and the first match wins,
// exactly as classify.go orders its own checks: the most specific signal first.
//
// Content is deliberately not part of the routing decision. Where the name is ambiguous —
// a `.yaml` under deploy/ that may or may not be a Kubernetes manifest — the reader
// sniffs its own content and returns empty facts if the file is not what the directory
// suggested. Keeping that judgement inside the reader means each reader states its own
// admission rule next to the code that depends on it, rather than the registry encoding a
// second, drifting copy.
type Registry struct {
	routes []Route
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{} }

// Register appends a route. Order is significant: a route added earlier is tried first.
func (r *Registry) Register(rt Route) { r.routes = append(r.routes, rt) }

// DefaultRegistry returns the routing table signpost ships.
//
// One place assembles the set, for the same reason extract.DefaultRegistry does: a repo
// reported as fully read because a reader was never registered is worse than one reported
// as unhandled.
func DefaultRegistry() *Registry {
	r := NewRegistry()

	// Lock files first. Several are also manifests by name (go.sum), and they must be
	// recorded as deliberately unparsed rather than fall through to a reader that would
	// try. Same ordering reason as classify.go's own lock-first check.
	r.Register(Route{Kind: KindLock, Match: matchLock, Read: readLock})

	// Dependency manifests: exact basenames, so no ambiguity to resolve.
	r.Register(Route{Kind: KindGoMod, Match: basename("go.mod"), Read: ExtractGoMod})
	r.Register(Route{Kind: KindPackageJSON, Match: basename("package.json"), Read: ExtractPackageJSON})
	r.Register(Route{Kind: KindPyProject, Match: basename("pyproject.toml"), Read: ExtractPyProject})
	r.Register(Route{Kind: KindRequirement, Match: matchRequirements, Read: ExtractRequirements})
	r.Register(Route{Kind: KindCargo, Match: basename("Cargo.toml"), Read: ExtractCargo})
	r.Register(Route{Kind: KindMakefile, Match: basename("Makefile", "makefile", "GNUmakefile"), Read: ExtractMakefile})

	// tsconfig, for its resolution mapping alone. Matched by prefix rather than exact
	// name because the variants are conventional and carry the same mapping:
	// `tsconfig.build.json`, `tsconfig.node.json`, `jsconfig.json`.
	r.Register(Route{Kind: KindTSConfig, Match: matchTSConfig, Read: ExtractTSConfig})

	// Infrastructure. Containerfiles and compose files are named unambiguously;
	// workflows are identified by their directory, which is the only thing that
	// distinguishes a workflow from any other YAML.
	r.Register(Route{Kind: KindContainer, Match: matchContainerfile, Read: ExtractContainerfile})
	r.Register(Route{Kind: KindCompose, Match: matchCompose, Read: ExtractCompose})
	r.Register(Route{Kind: KindWorkflow, Match: matchWorkflow, Read: ExtractWorkflow})
	r.Register(Route{Kind: KindHelmChart, Match: basename("Chart.yaml", "Chart.yml"), Read: ExtractHelmChart})
	r.Register(Route{Kind: KindHelmValues, Match: matchHelmValues, Read: ExtractHelmValues})

	// Contracts, before the Kubernetes catch-all: an OpenAPI document under a deploy
	// directory is still an OpenAPI document, and the reverse route would read it as a
	// manifest with no kind and report nothing.
	r.Register(Route{Kind: KindProto, Match: ext(".proto"), Read: ExtractProto})
	r.Register(Route{Kind: KindGraphQL, Match: ext(".graphql", ".graphqls", ".gql"), Read: ExtractGraphQL})
	r.Register(Route{Kind: KindOpenAPI, Match: matchOpenAPI, Read: ExtractOpenAPI})

	// Kubernetes last among the YAML routes, because it is the widest: any YAML under a
	// deployment directory, including a Helm template. The reader requires apiVersion
	// and kind, so a file that is not a manifest yields nothing rather than noise.
	r.Register(Route{Kind: KindKubernetes, Match: matchKubernetes, Read: ExtractKubernetes})

	// Human-authored inputs.
	r.Register(Route{Kind: KindMigration, Match: matchMigration, Read: ExtractMigration})
	r.Register(Route{Kind: KindCodeowners, Match: basename("CODEOWNERS", "OWNERS"), Read: ExtractCodeowners})
	r.Register(Route{Kind: KindADR, Match: matchADR, Read: ExtractADR})
	r.Register(Route{Kind: KindAgentRules, Match: basename("AGENTS.md", "CLAUDE.md"), Read: ExtractAgentRules})

	return r
}

// Route returns the first route claiming the file, or nil.
func (r *Registry) Route(f discover.File) *Route {
	for i := range r.routes {
		if r.routes[i].Match(f) {
			return &r.routes[i]
		}
	}
	return nil
}

// Kinds returns the kinds the registry can produce, sorted and deduped.
func (r *Registry) Kinds() []Kind {
	seen := make(map[Kind]bool, len(r.routes))
	out := make([]Kind, 0, len(r.routes))
	for _, rt := range r.routes {
		if seen[rt.Kind] {
			continue
		}
		seen[rt.Kind] = true
		out = append(out, rt.Kind)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// RunResult is the outcome of reading every non-source file in a discovery result.
type RunResult struct {
	// Facts are the readings, sorted by path.
	Facts []Facts
	// Unhandled counts files no route claimed, keyed by basename for extensionless
	// files and by extension otherwise. Reported for the same reason
	// extract.RunResult reports it: a repo whose deployment lives entirely in
	// Terraform must not look covered because signpost read its go.mod.
	Unhandled map[string]int
}

// Run reads every non-source file in a discovery result.
//
// Source files are skipped: they belong to internal/extract, and a file is one or the
// other. Binary files have nothing to read. Vendored files are skipped unless the walk was
// asked for them, because a vendored manifest is another repository's statement about
// itself — the decision is deferred to Result.Analyses so that -include-vendored reaches
// here and not only the walk (issue #11).
func (r *Registry) Run(res *discover.Result) *RunResult {
	out := &RunResult{Unhandled: make(map[string]int)}
	for _, f := range res.Files {
		if f.Class == discover.ClassSource || !res.Analyses(f) || f.Binary {
			continue
		}
		rt := r.Route(f)
		if rt == nil {
			out.Unhandled[unhandledKey(f.Path)]++
			continue
		}
		facts := rt.Read(f)
		facts.Path = f.Path
		facts.Class = f.Class
		if facts.Kind == "" {
			facts.Kind = rt.Kind
		}
		if f.Truncated {
			// A truncated manifest is the case where partial reading is most
			// misleading: the dependency list is the one fact a reader will trust
			// completely, and half of one looks exactly like all of one.
			facts.markIncomplete("content truncated by size cap; reading covers head and tail only")
		}
		facts.Normalize()
		out.Facts = append(out.Facts, facts)
	}
	sort.Slice(out.Facts, func(i, j int) bool { return out.Facts[i].Path < out.Facts[j].Path })
	return out
}

// unhandledKey names a file's shape for the coverage report.
//
// An extension groups the interesting cases — every `.tf` file in a repo is one gap, not
// forty — and a basename covers the extensionless files, where the name is the only thing
// that identifies the format.
func unhandledKey(rel string) string {
	base := path.Base(rel)
	if e := path.Ext(base); e != "" {
		return strings.ToLower(e)
	}
	return strings.ToLower(base)
}

// readLock records that a lock file exists without parsing it.
//
// A lock file is wholly derived from a manifest already read, is often megabytes, and
// contributes no architectural signal — discover says as much where it classifies them.
// The route exists anyway so the file is reported as deliberately unparsed rather than as
// an unhandled gap; the two mean different things to anyone reading the coverage numbers.
//
// Incomplete is not set. It marks a reading that fell short of what the reader intended,
// and nothing was intended here.
func readLock(f discover.File) Facts {
	return Facts{
		Path:  f.Path,
		Class: f.Class,
		Kind:  KindLock,
		Note:  "lock file recorded, not parsed: fully derived from the manifest beside it",
	}
}

// basename matches any of the exact basenames given.
func basename(names ...string) func(discover.File) bool {
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return func(f discover.File) bool { return set[path.Base(f.Path)] }
}

// ext matches any of the extensions given, case-insensitively.
func ext(exts ...string) func(discover.File) bool {
	set := make(map[string]bool, len(exts))
	for _, e := range exts {
		set[e] = true
	}
	return func(f discover.File) bool { return set[strings.ToLower(path.Ext(f.Path))] }
}

var lockBasenames = map[string]bool{
	"go.sum": true, "package-lock.json": true, "pnpm-lock.yaml": true,
	"yarn.lock": true, "Cargo.lock": true, "uv.lock": true,
	"poetry.lock": true, "Gemfile.lock": true, "composer.lock": true,
}

func matchLock(f discover.File) bool { return lockBasenames[path.Base(f.Path)] }

// matchTSConfig claims TypeScript and JavaScript project configs, including the
// `tsconfig.<name>.json` variants a project splits its build across. Those variants matter
// rather than being tidiness: a repo whose aliases live in `tsconfig.base.json` and are
// inherited everywhere else states its whole resolution mapping in a file an exact-name
// match would skip.
func matchTSConfig(f discover.File) bool {
	base := strings.ToLower(path.Base(f.Path))
	if !strings.HasSuffix(base, ".json") {
		return false
	}
	return strings.HasPrefix(base, "tsconfig.") || strings.HasPrefix(base, "jsconfig.")
}

// matchRequirements claims pip requirement files under any of their conventional names:
// `requirements.txt`, `requirements-dev.txt`, `requirements/base.txt`.
func matchRequirements(f discover.File) bool {
	base := strings.ToLower(path.Base(f.Path))
	if !strings.HasSuffix(base, ".txt") {
		return false
	}
	return strings.HasPrefix(base, "requirements") || inDir(f.Path, "requirements")
}

// matchContainerfile mirrors discover's own recognition, which covers the variants that
// actually appear: `Dockerfile.dev` and `dev.Dockerfile` are both real conventions.
func matchContainerfile(f discover.File) bool {
	lower := strings.ToLower(path.Base(f.Path))
	return lower == "dockerfile" || lower == "containerfile" ||
		strings.HasPrefix(lower, "dockerfile.") || strings.HasPrefix(lower, "containerfile.") ||
		strings.HasSuffix(lower, ".dockerfile") || strings.HasSuffix(lower, ".containerfile")
}

func matchCompose(f discover.File) bool {
	lower := strings.ToLower(path.Base(f.Path))
	if !isYAMLName(lower) {
		return false
	}
	return strings.HasPrefix(lower, "docker-compose") || strings.HasPrefix(lower, "compose.")
}

// matchWorkflow claims GitHub Actions workflows and composite actions.
//
// The directory is what identifies a workflow: `ci.yml` says nothing on its own, and
// `.github/workflows/ci.yml` says everything. A composite action is identified by its
// basename instead, since it may live anywhere.
func matchWorkflow(f discover.File) bool {
	lower := strings.ToLower(path.Base(f.Path))
	if !isYAMLName(lower) {
		return false
	}
	if lower == "action.yml" || lower == "action.yaml" {
		return true
	}
	return strings.HasPrefix(f.Path, ".github/workflows/")
}

func matchHelmValues(f discover.File) bool {
	lower := strings.ToLower(path.Base(f.Path))
	return isYAMLName(lower) && strings.HasPrefix(lower, "values")
}

// matchOpenAPI claims documents named or located as API descriptions. The reader requires
// an `openapi` or `swagger` key, so a file that merely sits under api/ costs nothing.
func matchOpenAPI(f discover.File) bool {
	lower := strings.ToLower(path.Base(f.Path))
	if !isYAMLName(lower) && !strings.HasSuffix(lower, ".json") {
		return false
	}
	if strings.HasPrefix(lower, "openapi") || strings.HasPrefix(lower, "swagger") {
		return true
	}
	return inDir(f.Path, "openapi") || inDir(f.Path, "swagger")
}

// matchKubernetes claims YAML under a deployment directory, and Helm templates.
//
// This is the widest YAML route, which is why it is registered last. The directory
// conventions are the same ones discover classifies on; the reader then requires
// apiVersion and kind, so a values fragment or a CI config that happens to live under
// deploy/ yields nothing rather than a service that does not exist.
func matchKubernetes(f discover.File) bool {
	lower := strings.ToLower(path.Base(f.Path))
	if !isYAMLName(lower) && !strings.HasSuffix(lower, ".tpl") {
		return false
	}
	for _, d := range []string{"k8s", "kubernetes", "manifests", "deploy", "templates"} {
		if inDir(f.Path, d) {
			return true
		}
	}
	return false
}

// matchMigration defers to discover's classification rather than re-deriving it.
//
// A `.sql` file outside a migrations directory is deliberately not claimed: a seed file, a
// hand-run query, and a schema dump are all `.sql`, and reading one as a migration would
// place a change in the sequence that the tooling will never apply.
func matchMigration(f discover.File) bool { return f.Class == discover.ClassMigration }

// matchADR claims architecture decision records by directory.
//
// The filename convention (`0004-use-podman.md`) is not enough on its own — a numbered
// chapter in a documentation set looks identical — and mistaking a document for a decision
// record would present ordinary prose as a binding constraint with a status.
func matchADR(f discover.File) bool {
	if !strings.HasSuffix(strings.ToLower(path.Base(f.Path)), ".md") {
		return false
	}
	for _, d := range []string{"adr", "adrs", "decisions", "decision-records"} {
		if inDir(f.Path, d) {
			return true
		}
	}
	return false
}

func isYAMLName(lower string) bool {
	return strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml")
}

// inDir reports whether any directory segment of a slash path equals name, compared
// case-insensitively. The final segment is the filename and is never considered.
func inDir(rel, name string) bool {
	segs := strings.Split(rel, "/")
	if len(segs) < 2 {
		return false
	}
	for _, s := range segs[:len(segs)-1] {
		if strings.EqualFold(s, name) {
			return true
		}
	}
	return false
}
