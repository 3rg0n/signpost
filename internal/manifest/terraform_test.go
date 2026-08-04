package manifest

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func tfFile(path, content string) discover.File {
	return discover.File{Path: path, Class: discover.ClassInfra, Content: content}
}

func TestExtractTerraformReadsModulesAndProviders(t *testing.T) {
	f := tfFile("infra/main.tf", `
terraform {
  required_version = ">= 1.6.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.31"
    }
    random = {
      source = "hashicorp/random"
    }
  }

  backend "s3" {
    bucket = "example-tfstate"
    key    = "prod/terraform.tfstate"
  }
}

provider "aws" {
  region = var.region
}

module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "5.5.1"
  cidr    = "10.0.0.0/16"
}

module "rds" {
  source = "../modules/rds"
  size   = var.db_size
}

module "runtime" {
  source = "./runtime"
}
`)
	facts := ExtractTerraform(f)

	if facts.Kind != KindTerraform {
		t.Errorf("kind = %q, want %q", facts.Kind, KindTerraform)
	}
	if facts.Incomplete {
		t.Fatalf("a well-formed configuration must not be reported as partially read: %q", facts.Note)
	}
	if facts.Module.LangVersion != ">= 1.6.0" {
		t.Errorf("required_version = %q", facts.Module.LangVersion)
	}

	// Providers come from required_providers with their constraints, and the bare
	// `provider "aws"` block must not double-count: two nodes for one plugin would report
	// a supply chain larger than the one declared.
	deps := map[string]Dep{}
	for _, d := range facts.Deps {
		if _, dup := deps[d.Name]; dup {
			t.Errorf("dependency %q recorded twice: %+v", d.Name, facts.Deps)
		}
		deps[d.Name] = d
		if d.Ecosystem != ecoTerraform {
			t.Errorf("dep %q ecosystem = %q, want %q", d.Name, d.Ecosystem, ecoTerraform)
		}
	}
	if got := deps["aws"]; got.Version != "~> 5.31" || got.Source != "hashicorp/aws" {
		t.Errorf("aws provider = %+v, want the required_providers constraint", got)
	}
	if got := deps["random"]; got.Source != "hashicorp/random" || got.Version != "" {
		t.Errorf("random provider = %+v; a provider with no version constraint has none, not a guessed one", got)
	}

	// A registry module keeps the source as written: it has a registry to publish an
	// advisory against, which is exactly what Dep.Source exists to distinguish.
	if got := deps["vpc"]; got.Source != "terraform-aws-modules/vpc/aws" || got.Version != "5.5.1" {
		t.Errorf("vpc module = %+v", got)
	}
	// A local module resolves to the directory it names, relative to the calling file.
	// This is the edge no source-level import can supply.
	if got := deps["rds"]; got.Source != "modules/rds" {
		t.Errorf("rds module source = %q, want the resolved repo-relative path", got.Source)
	}
	if got := deps["runtime"]; got.Source != "infra/runtime" {
		t.Errorf("runtime module source = %q, want it resolved against infra/", got.Source)
	}

	// The backend is where state lives, recorded by name. Its contents are not: a
	// backend block routinely carries an access key.
	var backend *Service
	for i, s := range facts.Services {
		if strings.HasPrefix(s.Kind, "backend.") {
			backend = &facts.Services[i]
		}
	}
	if backend == nil || backend.Kind != "backend.s3" {
		t.Fatalf("services = %+v, want a backend.s3 entry", facts.Services)
	}
	if strings.Contains(backend.Name, "example-tfstate") {
		t.Errorf("backend service = %+v; the bucket is configuration, not the unit's name", backend)
	}
}

// Only resources that run or store something become services, because a service becomes a
// page. The negative half of this test is the point: a configuration is mostly wiring, and
// a reader that admitted all of it would report forty pages where six things run.
func TestExtractTerraformRecordsOnlyWorkloadResources(t *testing.T) {
	f := tfFile("infra/app.tf", `
resource "aws_ecs_service" "api" {
  name = "api"
}

resource "aws_lambda_function" "worker" {
  function_name = "worker"
  image_uri     = "ghcr.io/example/worker:1.4.0"
}

resource "aws_db_instance" "primary" {
  identifier = "primary"
}

resource "aws_security_group_rule" "allow_https" {
  type = "ingress"
}

resource "aws_iam_role_policy_attachment" "api" {
  role = aws_iam_role.api.name
}

resource "aws_route_table_association" "public" {
  subnet_id = aws_subnet.public.id
}

resource "aws_ecs_cluster" "main" {
  name = "main"
}

resource "aws_s3_bucket_versioning" "logs" {
  bucket = aws_s3_bucket.logs.id
}

resource "aws_secretsmanager_secret" "session" {
  name = "prod/session-signing-key"
}

data "aws_secretsmanager_secret" "shared" {
  name = "shared/session-signing-key"
}

data "aws_ecs_service" "other_team_api" {
  service_name = "their-api"
}
`)
	facts := ExtractTerraform(f)

	got := map[string]string{}
	for _, s := range facts.Services {
		got[s.Name] = s.Kind
	}
	want := map[string]string{
		"aws_ecs_service.api":        "aws_ecs_service",
		"aws_lambda_function.worker": "aws_lambda_function",
		"aws_db_instance.primary":    "aws_db_instance",
		// Not a workload — nothing runs in a secret store — and a service all the same,
		// because the reference recorded against it reaches a reader only through the
		// node it names. See readTerraformResource.
		"aws_secretsmanager_secret.session": "aws_secretsmanager_secret",
	}
	if len(got) != len(want) {
		t.Fatalf("services = %v, want exactly %v", got, want)
	}
	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("service %q kind = %q, want %q", name, got[name], kind)
		}
	}
	// Named individually rather than left to the count above, so that a suffix rule
	// loosened later fails here saying which thing it wrongly admitted.
	for _, notAService := range []string{
		"aws_security_group_rule.allow_https",
		"aws_iam_role_policy_attachment.api",
		"aws_route_table_association.public",
		"aws_s3_bucket_versioning.logs",
		"aws_ecs_cluster.main",
		"aws_ecs_service.other_team_api",
		// A `data` block reading a secret declared in another configuration. The
		// reference is real and this one is not ours to describe, which is the boundary
		// that stops the secret-store exception from admitting every secret any
		// configuration anywhere declares.
		"aws_secretsmanager_secret.shared",
	} {
		if _, ok := got[notAService]; ok {
			t.Errorf("%q became a service; it is wiring, capacity, or another team's, and each would be a page nobody can act on", notAService)
		}
	}
	// And the reference it does carry is attributed to nothing, so it cannot be handed to
	// the workloads beside it in this file.
	for _, s := range facts.SecretRefs {
		if s.Name != "shared/session-signing-key" {
			continue
		}
		if !s.Unattributed || s.Service != "" {
			t.Errorf("a data-block secret is attributed to %q (unattributed=%v); the resource it "+
				"names has no node here, and handing it to this file's services would tell a "+
				"reader an ECS task reads a credential nothing says it reads", s.Service, s.Unattributed)
		}
	}

	if len(facts.Images) != 1 || facts.Images[0].Ref != "ghcr.io/example/worker:1.4.0" {
		t.Errorf("images = %+v, want the lambda's image", facts.Images)
	}
}

// Secret references are recorded by name, and never a value. This is the test that has to
// hold: the bundle is committed and published, so a reader that carried a value across
// would be a credential-exfiltration path wearing a documentation tool's clothes.
func TestExtractTerraformRecordsSecretNamesAndNeverValues(t *testing.T) {
	f := tfFile("infra/secrets.tf", `
variable "db_password" {
  type      = string
  sensitive = true
  default   = "hunter2-do-not-publish"
}

variable "api_token" {
  type = string
}

variable "region" {
  type    = string
  default = "us-east-1"
}

variable "instance_count" {
  type      = number
  sensitive = true
}

output "connection_string" {
  value     = "postgres://admin:${var.db_password}@db.example.com/app"
  sensitive = true
}

output "vpc_id" {
  value = module.vpc.id
}

resource "aws_secretsmanager_secret" "session" {
  name = "prod/session-signing-key"
}

resource "aws_secretsmanager_secret_version" "session" {
  secret_id     = aws_secretsmanager_secret.session.id
  secret_string = "s3cr3t-material-that-must-not-be-read"
}

resource "random_password" "db" {
  length = 32
}

resource "aws_ecs_service" "api" {
  name = "api"
}

resource "aws_s3_bucket" "assets" {
  bucket = "example-assets"
}
`)
	facts := ExtractTerraform(f)

	names := map[string]SecretRef{}
	for _, s := range facts.SecretRefs {
		names[s.Name] = s
	}
	for _, want := range []string{
		"db_password",              // sensitive = true
		"api_token",                // name heuristic, no sensitive flag
		"connection_string",        // sensitive output
		"prod/session-signing-key", // the secret's own name, from the resource's `name`
		"session",                  // the version resource, named for its block
		"db",                       // random_password generates one into state
		"instance_count",           // author-flagged, and taken at their word
	} {
		if _, ok := names[want]; !ok {
			t.Errorf("secret reference %q not recorded; refs = %v", want, keysOfRefs(names))
		}
	}
	for _, notASecret := range []string{"region", "vpc_id", "example-assets", "assets"} {
		if _, ok := names[notASecret]; ok {
			t.Errorf("%q recorded as a secret reference; a false claim that a credential is reachable is the direction that misleads most", notASecret)
		}
	}

	// A variable, a tfvars value, and an output belong to the configuration as a whole, not
	// to any one resource in it. One .tf file holds a dozen unrelated resources, so a
	// reference the reader cannot attribute has to say so rather than leave Service empty —
	// which SecretNamesFor reads as "shared with this file's services", and which reported an
	// ECS task and an S3 state backend as each reading three credentials neither of them names.
	for _, name := range []string{"db_password", "api_token", "connection_string", "instance_count"} {
		ref, ok := names[name]
		if !ok {
			t.Fatalf("%q not recorded, so this asserts nothing about attribution", name)
		}
		if !ref.Unattributed {
			t.Errorf("%q is an input to the configuration and is attributed to %q; every workload in this file would claim to read it",
				name, ref.Service)
		}
	}
	// The counterpart, and the reason this is not simply "mark everything unattributed": a
	// secret store's reference names the resource that *is* the credential, and dropping that
	// attribution would leave it reaching no page at all.
	if ref := names["prod/session-signing-key"]; ref.Service != "aws_secretsmanager_secret.session" || ref.Unattributed {
		t.Errorf("the secret store's own reference is attributed to %q (unattributed=%v), want the resource that holds it",
			ref.Service, ref.Unattributed)
	}
	// What the two rules add up to, asked the way assemble asks it.
	for _, unwanted := range []string{"db_password", "api_token", "connection_string"} {
		if got := facts.SecretNamesFor("aws_ecs_service.api"); contains(got, unwanted) {
			t.Errorf("the api service reads %q according to this file, and nothing in it says so: %v", unwanted, got)
		}
	}
	// And an unattributable reference is still a reference: this is the function that asks
	// whether the file touches credentials at all, and the answer must not have changed.
	if !contains(facts.SecretNames(), "db_password") {
		t.Errorf("a sensitive variable left the facts entirely: %v", facts.SecretNames())
	}

	// The whole file, rendered as the reader saw it, must not contain any value that was
	// beside a sensitive name. Asserted over the facts as a whole rather than field by
	// field, because a value can only leak through a field somebody adds later, and a
	// per-field assertion would not see it.
	rendered := renderFacts(facts)
	for _, secret := range []string{
		"hunter2-do-not-publish",
		"s3cr3t-material-that-must-not-be-read",
		"postgres://admin:",
	} {
		if strings.Contains(rendered, secret) {
			t.Errorf("a secret value reached the facts: %q found in %s", secret, rendered)
		}
	}
}

// A .tfvars file is where a real deployment's values are set, which makes it the file in a
// Terraform repository most likely to hold a live credential. Names out, values never.
func TestExtractTerraformReadsTFVarsNamesOnly(t *testing.T) {
	f := tfFile("infra/prod.tfvars", `
# Production values.
region         = "us-east-1"
instance_count = 4
db_password    = "correct-horse-battery-staple"
api_token      = "ghp_realtokenshapedstring"
tags = {
  env  = "prod"
  team = "platform"
}
`)
	facts := ExtractTerraform(f)

	names := map[string]SecretRef{}
	for _, s := range facts.SecretRefs {
		names[s.Name] = s
	}
	if len(names) != 2 || names["db_password"].Source != "terraform-tfvars" {
		t.Fatalf("tfvars secret refs = %v, want db_password and api_token", keysOfRefs(names))
	}
	if got := names["db_password"].EnvVar; got != "TF_VAR_db_password" {
		t.Errorf("env var = %q, want the TF_VAR_ binding a reader can search for", got)
	}
	// A value for a variable inherits the variable's scope: an input to the configuration,
	// attributable to no single resource in it. Asserted here even though this file declares
	// no resources for it to be misattributed to, because that is a property of the fixture
	// and not of the rule — the .tf file beside it is where the same reference does damage.
	for _, s := range facts.SecretRefs {
		if !s.Unattributed {
			t.Errorf("tfvars reference %q is attributed to %q; a values file names no unit that could read it",
				s.Name, s.Service)
		}
	}
	rendered := renderFacts(facts)
	for _, value := range []string{"correct-horse-battery-staple", "ghp_realtokenshapedstring", "us-east-1"} {
		if strings.Contains(rendered, value) {
			t.Errorf("a .tfvars value reached the facts: %q", value)
		}
	}
	// A values file declares no structure, and reporting one would be a false claim about
	// what this repository deploys.
	if len(facts.Services) != 0 || len(facts.Deps) != 0 {
		t.Errorf("services = %+v, deps = %+v; a .tfvars file states values, not structure", facts.Services, facts.Deps)
	}
}

// A local module source is only a local source when Terraform says it is, which is when it
// begins with ./ or ../. Everything else is remote, and resolving a registry module to a
// same-named directory would draw an edge to code that has nothing to do with it.
func TestLocalModuleSourceFollowsTerraformsOwnRule(t *testing.T) {
	tests := []struct {
		name string
		file string
		src  string
		want string // "" means not local
	}{
		{"same directory", "infra/main.tf", "./vpc", "infra/vpc"},
		{"parent directory", "infra/prod/main.tf", "../modules/rds", "infra/modules/rds"},
		{"root of the repository", "infra/main.tf", "../modules", "modules"},
		{"submodule of a local module", "main.tf", "./modules/vpc//networking", "modules/vpc"},
		{"registry short form", "infra/main.tf", "terraform-aws-modules/vpc/aws", ""},
		{"explicit registry host", "infra/main.tf", "registry.terraform.io/hashicorp/consul/aws", ""},
		{"git over https", "infra/main.tf", "git::https://example.com/vpc.git", ""},
		{"github shorthand", "infra/main.tf", "github.com/example/vpc", ""},
		{"s3 archive", "infra/main.tf", "s3::https://s3.amazonaws.com/b/vpc.zip", ""},
		{"a bare relative path is not local to Terraform", "infra/main.tf", "modules/vpc", ""},
		{"escaping the repository root", "main.tf", "../../elsewhere", ""},
		{"resolving to the root itself", "infra/main.tf", "./", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := localModuleSource(tt.file, tt.src)
			if tt.want == "" {
				if ok {
					t.Fatalf("localModuleSource(%q, %q) = %q, want not local", tt.file, tt.src, got)
				}
				return
			}
			if !ok || got != tt.want {
				t.Fatalf("localModuleSource(%q, %q) = %q, %v; want %q", tt.file, tt.src, got, ok, tt.want)
			}
		})
	}
}

// The reader claims Terraform configuration and nothing that merely looks like it. The
// negative rows carry the weight: .tfstate holds every attribute of every resource,
// credentials included, and a reader that opened it would publish them.
func TestMatchTerraformClaimsConfigurationOnly(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.tf", true},
		{"infra/prod/network.tf", true},
		{"infra/prod.tfvars", true},
		{"infra/prod.auto.tfvars", true},
		{"infra/vars.tfvars.json", true},
		{"MAIN.TF", true},
		{"terraform.tfstate", false},
		{"terraform.tfstate.backup", false},
		{"infra/main.tf.json", false},
		{"docs/terraform.md", false},
		{"infra/.terraform.lock.hcl", false},
		{"main.tfx", false},
		{"notes.txt", false},
	}
	for _, tt := range tests {
		if got := matchTerraform(discover.File{Path: tt.path}); got != tt.want {
			t.Errorf("matchTerraform(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// Attribution is part of a reference's identity, asserted against Normalize directly
// because no reader emits both forms of the same name today and so no fixture reaches it.
// It is a contract on Facts rather than on any one reader: the compose reader means "shared
// with this file's services" by an empty Service and the Terraform reader means "belongs to
// no unit here", and a Facts that folded those would resolve one file's claim using the
// other file's convention. Which of the two survived would depend on sort order, so the
// dedupe's identity check and the comparator above it are one rule tested once.
func TestNormalizeKeepsAttributedAndUnattributedReferencesApart(t *testing.T) {
	facts := &Facts{SecretRefs: []SecretRef{
		{Name: "db_password", Service: "", Unattributed: true, Source: "terraform-variable"},
		{Name: "db_password", Service: "", Unattributed: false, Source: "env-interpolation"},
		{Name: "db_password", Service: "", Unattributed: true, Source: "terraform-tfvars"},
	}}
	facts.Normalize()
	if len(facts.SecretRefs) != 2 {
		t.Fatalf("refs = %+v, want the two claims kept apart and the repeated one folded", facts.SecretRefs)
	}
	// And in a fixed order, because the bundle is committed and a reordering is a diff.
	if facts.SecretRefs[0].Unattributed || !facts.SecretRefs[1].Unattributed {
		t.Errorf("refs = %+v, want the attributed claim first", facts.SecretRefs)
	}
}

func keysOfRefs(m map[string]SecretRef) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return sortedUnique(out)
}
