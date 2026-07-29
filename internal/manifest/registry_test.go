package manifest

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

// routed asserts which kind a path routes to. Content is supplied because several routes
// are name-based but their readers sniff content, and an empty file would route correctly
// and read nothing.
func routed(t *testing.T, path, content string, class discover.Class, want Kind) Facts {
	t.Helper()
	r := DefaultRegistry()
	f := discover.File{Path: path, Class: class, Content: content}
	rt := r.Route(f)
	if rt == nil {
		t.Fatalf("%s: no route", path)
	}
	if rt.Kind != want {
		t.Fatalf("%s routed to %q, want %q", path, rt.Kind, want)
	}
	facts := rt.Read(f)
	facts.Normalize()
	return facts
}

func TestRegistryRoutesByName(t *testing.T) {
	cases := []struct {
		path  string
		class discover.Class
		want  Kind
	}{
		{"go.mod", discover.ClassManifest, KindGoMod},
		{"web/package.json", discover.ClassManifest, KindPackageJSON},
		{"pyproject.toml", discover.ClassManifest, KindPyProject},
		{"requirements.txt", discover.ClassManifest, KindRequirement},
		{"requirements-dev.txt", discover.ClassManifest, KindRequirement},
		{"requirements/base.txt", discover.ClassManifest, KindRequirement},
		{"Cargo.toml", discover.ClassManifest, KindCargo},
		{"Makefile", discover.ClassManifest, KindMakefile},
		{"go.sum", discover.ClassManifest, KindLock},
		{"web/pnpm-lock.yaml", discover.ClassManifest, KindLock},

		{"Containerfile", discover.ClassInfra, KindContainer},
		{"services/api/Dockerfile.dev", discover.ClassInfra, KindContainer},
		{"build/dev.dockerfile", discover.ClassInfra, KindContainer},
		{"compose.yaml", discover.ClassInfra, KindCompose},
		{"docker-compose.prod.yml", discover.ClassInfra, KindCompose},
		{".github/workflows/ci.yml", discover.ClassInfra, KindWorkflow},
		{".github/actions/setup/action.yml", discover.ClassInfra, KindWorkflow},
		{"charts/api/Chart.yaml", discover.ClassInfra, KindHelmChart},
		{"charts/api/values.yaml", discover.ClassInfra, KindHelmValues},
		{"charts/api/values-prod.yaml", discover.ClassInfra, KindHelmValues},
		{"charts/api/templates/deployment.yaml", discover.ClassInfra, KindKubernetes},
		{"deploy/api.yaml", discover.ClassInfra, KindKubernetes},
		{"k8s/service.yml", discover.ClassInfra, KindKubernetes},

		{"api/v1/things.proto", discover.ClassContract, KindProto},
		{"schema.graphql", discover.ClassContract, KindGraphQL},
		{"api/openapi.yaml", discover.ClassContract, KindOpenAPI},
		{"api/swagger.json", discover.ClassContract, KindOpenAPI},

		{"migrations/0001_init.sql", discover.ClassMigration, KindMigration},
		{"CODEOWNERS", discover.ClassOwnership, KindCodeowners},
		{"AGENTS.md", discover.ClassOwnership, KindAgentRules},
		{"CLAUDE.md", discover.ClassOwnership, KindAgentRules},
		{"docs/adr/0004-use-podman.md", discover.ClassDoc, KindADR},
		{"docs/decisions/0001-x.md", discover.ClassDoc, KindADR},
	}
	r := DefaultRegistry()
	for _, c := range cases {
		f := discover.File{Path: c.path, Class: c.class, Content: "x"}
		rt := r.Route(f)
		if rt == nil {
			t.Errorf("%s: no route, want %q", c.path, c.want)
			continue
		}
		if rt.Kind != c.want {
			t.Errorf("%s routed to %q, want %q", c.path, rt.Kind, c.want)
		}
	}
}

// Route order is the whole design of this table, and these are the pairs where a later
// route would also match. Each one is a wrong reading rather than a missing one.
func TestRegistryOrderResolvesOverlaps(t *testing.T) {
	// go.sum is in discover's manifest names as well as its lock names; parsing it as a
	// manifest would report thousands of transitive modules as direct dependencies.
	if got := DefaultRegistry().Route(discover.File{Path: "go.sum"}).Kind; got != KindLock {
		t.Errorf("go.sum -> %q, want the lock route to win", got)
	}
	// An OpenAPI document inside a deploy directory is still an OpenAPI document. The
	// Kubernetes route would claim it and find no apiVersion, reporting nothing.
	if got := DefaultRegistry().Route(discover.File{Path: "deploy/openapi.yaml"}).Kind; got != KindOpenAPI {
		t.Errorf("deploy/openapi.yaml -> %q, want the contract route to win", got)
	}
	// Chart.yaml sits beside the templates directory but is a chart, not a manifest.
	if got := DefaultRegistry().Route(discover.File{Path: "charts/api/Chart.yaml"}).Kind; got != KindHelmChart {
		t.Errorf("Chart.yaml -> %q", got)
	}
	// values.yaml under templates/ is still values: the Kubernetes reader would find no
	// kind in it and the chart's actual configuration would go unread.
	if got := DefaultRegistry().Route(discover.File{Path: "charts/api/templates/values.yaml"}).Kind; got != KindHelmValues {
		t.Errorf("templates/values.yaml -> %q, want the values route to win", got)
	}
	// A workflow lives under .github/workflows/, which is not a deployment directory —
	// but a repo may name a workflow deploy.yml and put it there, so the workflow route
	// must precede the Kubernetes one.
	if got := DefaultRegistry().Route(discover.File{Path: ".github/workflows/deploy.yml"}).Kind; got != KindWorkflow {
		t.Errorf("deploy workflow -> %q", got)
	}
}

func TestRegistryDeclinesUnknownFiles(t *testing.T) {
	r := DefaultRegistry()
	// Terraform is real infrastructure this package does not read. Claiming it would be
	// worse than reporting the gap.
	for _, p := range []string{"infra/main.tf", "config/settings.ini", "README.md", "docs/guide.md", "web/tsconfig.json"} {
		if rt := r.Route(discover.File{Path: p}); rt != nil {
			t.Errorf("%s claimed by %q, want no route", p, rt.Kind)
		}
	}
}

// A `.sql` file outside a migrations directory is a seed, a dump, or a hand-run query.
// Reading one as a migration would place a change in a sequence the tooling never applies.
func TestRegistryDoesNotClaimLooseSQL(t *testing.T) {
	r := DefaultRegistry()
	if rt := r.Route(discover.File{Path: "scripts/seed.sql", Class: discover.ClassOther}); rt != nil {
		t.Errorf("scripts/seed.sql claimed by %q", rt.Kind)
	}
	if rt := r.Route(discover.File{Path: "db/migrations/2_x.sql", Class: discover.ClassMigration}); rt == nil {
		t.Error("a classified migration should be claimed")
	}
}

// A lock file is recorded rather than parsed, and the two are different facts: an
// unhandled file is a gap in signpost, and a recorded-not-parsed file is a decision.
func TestLockFileIsRecordedNotFlaggedIncomplete(t *testing.T) {
	facts := routed(t, "go.sum", strings.Repeat("h1:abc\n", 100), discover.ClassManifest, KindLock)
	if facts.Incomplete {
		t.Error("a lock file was not read short of intent; it was not read by design")
	}
	if len(facts.Deps) != 0 {
		t.Errorf("a lock file must not contribute dependencies: %+v", facts.Deps)
	}
	if !strings.Contains(facts.Note, "not parsed") {
		t.Errorf("note = %q, want the decision stated", facts.Note)
	}
}

// The route sniffs nothing, so a name-based match on a file that turns out to be something
// else must come back empty rather than wrong — the reader's admission rule, exercised
// through the registry.
func TestRegistryRoutedReaderSniffsContent(t *testing.T) {
	facts := routed(t, "deploy/notes.yaml", "a: 1\nb: 2\n", discover.ClassInfra, KindKubernetes)
	if len(facts.Services) != 0 || facts.Incomplete {
		t.Errorf("facts = %+v, want empty and unflagged", facts)
	}
	real := routed(t, "deploy/api.yaml", "apiVersion: v1\nkind: Service\nmetadata:\n  name: api\n", discover.ClassInfra, KindKubernetes)
	if len(real.Services) != 1 {
		t.Errorf("services = %+v", real.Services)
	}
}

func TestRunReadsEveryNonSourceFile(t *testing.T) {
	res := &discover.Result{Files: []discover.File{
		{Path: "go.mod", Class: discover.ClassManifest, Content: "module example.com/m\n\ngo 1.26\n"},
		{Path: "main.go", Class: discover.ClassSource, Lang: discover.LangGo, Content: "package main\n"},
		{Path: "Containerfile", Class: discover.ClassInfra, Content: "FROM cgr.dev/chainguard/static:latest\nCMD [\"/api\"]\n"},
		{Path: "infra/main.tf", Class: discover.ClassInfra, Content: "resource \"aws_s3_bucket\" \"b\" {}\n"},
		{Path: "infra/dns.tf", Class: discover.ClassInfra, Content: "resource \"aws_route53_zone\" \"z\" {}\n"},
		{Path: "vendor/other/go.mod", Class: discover.ClassManifest, Content: "module other\n", Vendored: true},
		{Path: "logo.png", Class: discover.ClassOther, Binary: true},
	}}

	out := DefaultRegistry().Run(res)

	// Source files belong to internal/extract; a file is one or the other.
	// Vendored code is another repository's statement about itself.
	if len(out.Facts) != 2 {
		var got []string
		for _, f := range out.Facts {
			got = append(got, f.Path)
		}
		t.Fatalf("facts = %v, want go.mod and Containerfile", got)
	}
	// Sorted by path, so a bundle is byte-identical across runs and platforms.
	if out.Facts[0].Path != "Containerfile" || out.Facts[1].Path != "go.mod" {
		t.Errorf("order = %q, %q", out.Facts[0].Path, out.Facts[1].Path)
	}
	if out.Facts[1].Kind != KindGoMod || out.Facts[1].Module.Name != "example.com/m" {
		t.Errorf("go.mod facts = %+v", out.Facts[1])
	}
	// Coverage is grouped by extension so a repo whose deployment is entirely Terraform
	// reports one gap of two files rather than looking covered because its go.mod parsed.
	if out.Unhandled[".tf"] != 2 {
		t.Errorf("unhandled = %v, want two .tf files", out.Unhandled)
	}
	if _, ok := out.Unhandled[".png"]; ok {
		t.Error("a binary file is not an extraction gap")
	}
}

// A truncated manifest is where partial reading misleads most: the dependency list is the
// fact a reader trusts completely, and half of one looks exactly like all of one.
func TestRunFlagsTruncatedFiles(t *testing.T) {
	res := &discover.Result{Files: []discover.File{{
		Path:      "go.mod",
		Class:     discover.ClassManifest,
		Content:   "module example.com/m\n\nrequire example.com/a v1.0.0\n",
		Truncated: true,
	}}}
	out := DefaultRegistry().Run(res)
	if !out.Facts[0].Incomplete {
		t.Fatal("a truncated file must not be presented as fully read")
	}
	if !strings.Contains(out.Facts[0].Note, "truncated") {
		t.Errorf("note = %q", out.Facts[0].Note)
	}
	// The partial facts are kept: they are still useful, which is why a reader returns
	// Facts rather than an error.
	if len(out.Facts[0].Deps) != 1 {
		t.Errorf("deps = %+v, want the partial reading kept", out.Facts[0].Deps)
	}
}

// Every kind a reader can produce must be reachable through the table. A kind with no
// route is a reader that never runs, which would report a whole file class as a gap.
func TestEveryKindIsRoutable(t *testing.T) {
	got := make(map[Kind]bool)
	for _, k := range DefaultRegistry().Kinds() {
		got[k] = true
	}
	for _, k := range []Kind{
		KindGoMod, KindPackageJSON, KindPyProject, KindRequirement, KindCargo,
		KindContainer, KindCompose, KindWorkflow, KindHelmChart, KindHelmValues,
		KindKubernetes, KindProto, KindOpenAPI, KindGraphQL, KindMigration,
		KindCodeowners, KindAgentRules, KindADR, KindMakefile, KindLock,
	} {
		if !got[k] {
			t.Errorf("kind %q has no route", k)
		}
	}
}

func TestRunIsDeterministic(t *testing.T) {
	res := &discover.Result{Files: []discover.File{
		{Path: "z/Cargo.toml", Class: discover.ClassManifest, Content: "[package]\nname = \"z\"\nversion = \"1\"\n\n[dependencies]\nb = \"1\"\na = \"2\"\n"},
		{Path: "a/go.mod", Class: discover.ClassManifest, Content: "module a\n\nrequire (\n\tz v1.0.0\n\tb v2.0.0\n)\n"},
		{Path: "CODEOWNERS", Class: discover.ClassOwnership, Content: "*.go @b\n*.md @a\n"},
	}}
	first := ""
	for i := 0; i < 10; i++ {
		out := DefaultRegistry().Run(res)
		var b strings.Builder
		for _, f := range out.Facts {
			b.WriteString(f.Path + "|" + string(f.Kind) + "|" + renderFacts(f) + "\n")
		}
		if i == 0 {
			first = b.String()
			continue
		}
		if b.String() != first {
			t.Fatalf("run %d differed:\n%s\nvs\n%s", i, b.String(), first)
		}
	}
}
