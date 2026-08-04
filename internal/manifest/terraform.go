package manifest

import (
	"path"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// Terraform extraction.
//
// A Terraform reader belongs here rather than in internal/extract, and the reason is
// discover's own classification: `.tf` and `.tfvars` are ClassInfra (classify.go), so
// they never reach extract.Registry.Run, which walks Sources() only. That is the right
// classification — Terraform describes deployment rather than program structure — and it
// decides which package reads it.
//
// What Terraform states that nothing else in a repository does is *what is deployed and
// where the definition of it comes from*. A compose file names the services on one host;
// a Kubernetes manifest names the workloads in one cluster; Terraform names the cloud
// resources underneath both, and its `module` blocks are the only place a repository says
// which parts of its own infrastructure are reused from which directory or registry.
//
// Three fact kinds come out of it, and each maps onto something the graph already models:
//
//   - A `module` block is a Dep, in the `terraform` ecosystem. Local sources ("./vpc",
//     "../modules/rds") are recorded with the resolved repo-relative path in Source so
//     addDeclaredDepEdges can draw the edge to the module holding that code; registry and
//     git sources stay external, exactly like an npm dependency. A `provider` and a
//     `required_providers` entry are Deps too: a provider is a versioned third-party
//     plugin the run downloads, which is a supply-chain fact of the same kind.
//   - A `resource` that runs or stores something is a Service, with Kind carrying the
//     Terraform type. Only those: see readTerraformResource for why a security group rule
//     is read and not recorded.
//   - A `variable` marked `sensitive = true`, a sensitive `output`, and a resource whose
//     type is a secret store are SecretRefs. Names only, and a `default` is never read —
//     a sensitive variable's default is the one place in this whole format where a
//     credential sits in plain text.
//
// Not a full HCL parser, and not trying to be, for the reason design §4.1 gives about the
// line-oriented extractors: the block header is where the architectural facts live, and
// the header is exactly the part of HCL that is trivially unambiguous. Expressions,
// `for_each`, and dynamic blocks are stepped over rather than evaluated.

// ExtractTerraform reads a .tf or .tfvars file.
func ExtractTerraform(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindTerraform}
	root, diag := parseHCL(f.Content)
	facts.applyDiag(diag)

	// A provider appears twice in a well-formed configuration: once in
	// `required_providers` with its version constraint, and once as a `provider` block
	// configuring it. Both are collected and the constrained one wins, because
	// Facts.Normalize's dedupe folds only deps that agree on version — so emitting both
	// would report two dependencies on one plugin, and a supply chain larger than the one
	// declared. Deferred to the end rather than resolved inline because block order is the
	// author's choice: `provider` before `terraform` is unusual and legal.
	constrained := map[string]bool{}
	configured := map[string]int{}

	for _, b := range root.blocks {
		switch b.kind {
		case "module":
			readTerraformModule(&facts, f.Path, b)
		case "resource", "data":
			readTerraformResource(&facts, b)
		case "variable":
			readTerraformVariable(&facts, b)
		case "provider":
			// A provider is which cloud this deploys to, recorded as a dependency
			// because that is what it is: a versioned third-party plugin the run
			// downloads.
			if name := b.label(0); name != "" {
				facts.Deps = append(facts.Deps, Dep{
					Name: name, Scope: ScopeRuntime,
					Ecosystem: ecoTerraform, Line: b.line,
				})
				if _, seen := configured[name]; !seen {
					configured[name] = len(facts.Deps) - 1
				}
			}
		case "terraform":
			readTerraformSettings(&facts, b, constrained)
		case "output":
			// An output is this configuration's public surface — what a parent module
			// reads back — and only the sensitive ones are recorded. The rest are read
			// and dropped deliberately: an output name is a fact with nowhere to go,
			// since Facts has no notion of an exported value and inventing a field one
			// reader writes and no consumer reads would be worse than saying nothing.
			// A sensitive output does have somewhere to go, and it matters: it is a
			// credential leaving this module for its caller.
			if name := b.label(0); name != "" && b.boolAttr("sensitive") {
				facts.SecretRefs = append(facts.SecretRefs, SecretRef{
					Name: name, Source: "terraform-output", Line: b.line,
					// The module's own surface, not any one resource's. See the
					// Unattributed field for why the difference has to be recorded
					// rather than left to an empty Service.
					Unattributed: true,
				})
			}
		case "locals", "moved", "import", "check", "removed":
			// Read and deliberately not recorded. A local is an intermediate
			// expression, and the other four are state-management instructions to the
			// CLI rather than statements about what exists. Not a diagnostic: nothing
			// was missed.
		case "":
		default:
			facts.markIncomplete("unrecognised block type " + b.kind)
		}
	}

	// Drop the bare `provider` entry for anything `required_providers` already
	// constrained. Filtered rather than skipped at emit time so that a `provider` block
	// preceding the `terraform` block is handled the same as one following it.
	if len(constrained) > 0 && len(configured) > 0 {
		drop := make(map[int]bool, len(configured))
		for name, idx := range configured {
			if constrained[name] {
				drop[idx] = true
			}
		}
		kept := facts.Deps[:0]
		for i, d := range facts.Deps {
			if !drop[i] {
				kept = append(kept, d)
			}
		}
		facts.Deps = kept
	}

	// A .tfvars file is values, not structure: it has no blocks at all, only
	// assignments. The only fact worth taking from it is which of those assignments
	// name something sensitive — and, again, never the value beside the name.
	if isTFVars(f.Path) {
		readTFVars(&facts, f.Content)
	}
	return facts
}

// ecoTerraform names the ecosystem Terraform dependencies belong to, so a module
// called `vpc` and an npm package called `vpc` stay distinct nodes.
const ecoTerraform = "terraform"

// readTerraformModule records a `module "name" { source = ... }` block.
//
// The source is the fact that matters. A local path is a real edge inside this
// repository — the one statement of which of its own directories the infrastructure is
// composed from — and it is resolved against the calling file's directory here, because
// this is the only place that knows where the call was written.
func readTerraformModule(facts *Facts, file string, b hclBlock) {
	name := b.label(0)
	if name == "" {
		return
	}
	src := b.stringAttr("source")
	d := Dep{
		Name: name, Scope: ScopeRuntime, Ecosystem: ecoTerraform,
		Version: b.stringAttr("version"), Line: b.line,
	}
	if src != "" {
		d.Source = src
		if local, ok := localModuleSource(file, src); ok {
			// The resolved repo-relative directory, which is what the resolver matches
			// against, and the flag saying that is what it is. Source alone cannot carry
			// it: `modules/rds` and `hashicorp/vpc/aws` are the same shape, and only this
			// function saw whether the author wrote the `./` that makes one local.
			d.Source = local
			d.Local = true
		}
	}
	facts.Deps = append(facts.Deps, d)
}

// readTerraformResource records a `resource "type" "name"` or `data "type" "name"` block.
//
// Only the resources that *run something* become Services, on exactly the argument
// kubernetes.go makes for workloadKinds: a Service becomes a node and a node becomes a
// page, so a resource type that never executes would put a page in the bundle for
// something nobody can look at the behaviour of. A real Terraform repository declares
// hundreds of IAM policy attachments, security group rules, and route table associations;
// admitting all of them would bury the six things that actually serve traffic under four
// hundred that describe the wiring between them.
//
// A secret store is the one exception to the workload rule, and it is the same exception
// kubernetes.go makes for a `Secret` document: the resource *is* the named credential, so
// it becomes a Service carrying its own reference. Recording the reference without the
// node would be a dead write — a SecretRef names a service, and assemble reaches secrets
// only through the service that reads them, so a reference attributed to a resource with
// no node reaches no page at all. "Where the credentials in this configuration live" is
// worth a page in a way an IAM policy attachment is not.
func readTerraformResource(facts *Facts, b hclBlock) {
	typ, name := b.label(0), b.label(1)
	if typ == "" || name == "" {
		return
	}
	// Terraform's own addressing, `type.name`, rather than the name alone: an author
	// names a bucket and its log group both `logs`, and folding the two would report one
	// resource where the configuration declares two of different kinds.
	addr := typ + "." + name
	// A reference to a secret manager is a credential reference, and the resource type is
	// what identifies it. The secret's *name* is recorded; nothing reads a
	// `secret_string`, which is where the value would be.
	isSecret := secretResourceTypes[typ]
	if isSecret {
		ref := SecretRef{
			Name:    firstNonEmpty(b.stringAttr("name"), name),
			Service: addr,
			Source:  "terraform-" + typ,
			Line:    b.line,
		}
		// The `data` form reads a secret declared somewhere else, so this configuration
		// does not own it and there is nothing here to be a page about. The reference
		// stands on its own, unattributed for the same reason a variable's is.
		if b.kind == "data" {
			ref.Service, ref.Unattributed = "", true
		}
		facts.SecretRefs = append(facts.SecretRefs, ref)
	}
	// A `data` block reads something that exists elsewhere rather than declaring
	// something this configuration owns, so it is never a workload here however
	// compute-shaped its type is. What it is worth recording for is its secret
	// reference, which the block above already did.
	if b.kind == "data" {
		return
	}
	if isSecret {
		// The node the reference above is attributed to. Nothing else about a secret
		// store is worth carrying — it has no image and serves no traffic.
		facts.Services = append(facts.Services, Service{
			Name: addr, Kind: typ, Line: b.line,
		})
		return
	}
	if !isTerraformWorkload(typ) {
		return
	}
	svc := Service{Name: addr, Kind: typ, Line: b.line}
	if img := firstNonEmpty(b.stringAttr("image"), b.stringAttr("image_uri")); img != "" {
		svc.Image = img
		facts.Images = append(facts.Images, Image{Ref: img, Line: b.line})
	}
	facts.Services = append(facts.Services, svc)
}

// workloadResourceSuffixes are the resource-type endings that mean "this runs code or
// serves traffic".
//
// Suffix matching rather than a closed list of types, and the reason is arithmetic: the
// three major providers ship somewhere north of three thousand resource types between
// them, a closed list would be stale the week it was written, and the naming convention
// is stable in a way the inventory is not — an `aws_ecs_service`, a
// `google_cloud_run_service`, and an `azurerm_app_service` all end in `_service` and all
// run something. The cost of the convention failing is a missing node rather than a wrong
// one, which is the right direction for a tool whose claims a reader is meant to trust.
var workloadResourceSuffixes = []string{
	"_service", "_deployment", "_function", "_task_definition", "_instance",
	"_container_group", "_app", "_app_service", "_web_app", "_lambda_function",
	"_cluster", "_node_group", "_autoscaling_group", "_job", "_cronjob",
	"_container_app", "_workflow", "_state_machine", "_job_definition",
	"_db_instance", "_database", "_cache_cluster", "_bucket", "_queue", "_topic",
}

// nonWorkloadResourceTypes are the types that match a workload suffix and are not one.
//
// A short exceptions list is what makes the suffix rule usable rather than an argument
// against it. `aws_iam_service_linked_role` ends in a workload word and is a permission;
// `aws_ecs_cluster` is capacity that runs nothing by itself, while the `aws_ecs_service`
// inside it is the thing that runs. Each entry earns its place by being a type a real
// configuration declares often.
var nonWorkloadResourceTypes = map[string]bool{
	"aws_iam_service_linked_role":             true,
	"aws_service_discovery_service":           true,
	"aws_ecs_cluster":                         true,
	"aws_eks_cluster":                         true,
	"google_container_cluster":                true,
	"azurerm_kubernetes_cluster":              true,
	"aws_db_instance_role_association":        true,
	"aws_lambda_function_event_invoke_config": true,
	"aws_lambda_function_url":                 true,
	"aws_s3_bucket_policy":                    true,
	"aws_s3_bucket_acl":                       true,
	"aws_s3_bucket_versioning":                true,
	"aws_s3_bucket_public_access_block":       true,
	"aws_sqs_queue_policy":                    true,
	"aws_sns_topic_policy":                    true,
	"aws_sns_topic_subscription":              true,
}

// isTerraformWorkload reports whether a resource type names something that runs or stores.
func isTerraformWorkload(typ string) bool {
	if nonWorkloadResourceTypes[typ] {
		return false
	}
	for _, suffix := range workloadResourceSuffixes {
		if strings.HasSuffix(typ, suffix) {
			return true
		}
	}
	return false
}

// readTerraformVariable records a variable, and a sensitive one as a secret reference.
//
// `sensitive = true` is the author telling us this holds a credential, which is a
// stronger signal than any name heuristic — and the reason the `default` is never read.
// A sensitive variable with a default is a credential written in the configuration, and
// this bundle is committed and published (see the Facts.SecretRefs comment).
func readTerraformVariable(facts *Facts, b hclBlock) {
	name := b.label(0)
	if name == "" {
		return
	}
	if b.boolAttr("sensitive") || looksSensitive(name) {
		facts.SecretRefs = append(facts.SecretRefs, SecretRef{
			Name: name, EnvVar: "TF_VAR_" + name,
			Source: "terraform-variable", Line: b.line,
			// An input to the configuration as a whole. Which resource reads it is
			// stated in an expression this reader does not evaluate, so it is
			// attributed to nothing rather than to everything — see Unattributed.
			Unattributed: true,
		})
	}
}

// readTerraformSettings reads the `terraform` block: required version, providers, and
// where state lives.
// constrained records which provider names carried a `required_providers` entry, so the
// caller can drop the duplicate `provider` block for each.
func readTerraformSettings(facts *Facts, b hclBlock, constrained map[string]bool) {
	if v := b.stringAttr("required_version"); v != "" {
		facts.Module.LangVersion = v
	}
	for _, inner := range b.blocks {
		switch inner.kind {
		case "required_providers":
			// Each attribute is a provider, and its value is an object holding source
			// and version.
			for _, a := range inner.attrs {
				facts.Deps = append(facts.Deps, Dep{
					Name: a.key, Version: hclObjectField(a.value, "version"),
					Source: hclObjectField(a.value, "source"),
					Scope:  ScopeRuntime, Ecosystem: ecoTerraform, Line: a.line,
				})
				constrained[a.key] = true
			}
		case "backend", "cloud":
			// Where state lives. State is the most sensitive artifact a Terraform
			// repository has — it holds every attribute of every resource, credentials
			// included — so which backend holds it is worth recording. The name only:
			// a backend block's contents routinely include an access key.
			name := inner.label(0)
			if name == "" {
				name = inner.kind
			}
			facts.Services = append(facts.Services, Service{
				Name: "terraform-state", Kind: "backend." + name, Line: inner.line,
			})
		}
	}
}

// readTFVars records the sensitive-looking assignments in a .tfvars file.
//
// Values are never read. A .tfvars file is where a real deployment's variables are set,
// which makes it the single most likely file in a Terraform repository to hold a live
// credential — so this reader takes names and stops.
func readTFVars(facts *Facts, src string) {
	for i, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || !isHCLIdent(key) || !looksSensitive(key) {
			continue
		}
		facts.SecretRefs = append(facts.SecretRefs, SecretRef{
			Name: key, EnvVar: "TF_VAR_" + key,
			Source: "terraform-tfvars", Line: i + 1,
			// A value for a variable, so it inherits the variable's scope: an input to
			// the configuration, attributable to no single resource in it.
			Unattributed: true,
		})
	}
}

// secretResourceTypes are the resource types whose whole purpose is to hold a credential.
//
// A closed list rather than a name heuristic, and short on purpose: a false positive here
// claims a resource is a credential store when it is not, which is the direction that
// misleads a reader building a threat model. Every entry names the resource, never its
// value.
var secretResourceTypes = map[string]bool{
	"aws_secretsmanager_secret":         true,
	"aws_secretsmanager_secret_version": true,
	"aws_ssm_parameter":                 true,
	"aws_kms_key":                       true,
	"aws_iam_access_key":                true,
	"google_secret_manager_secret":      true,
	"google_service_account_key":        true,
	"azurerm_key_vault":                 true,
	"azurerm_key_vault_secret":          true,
	"azurerm_key_vault_certificate":     true,
	"kubernetes_secret":                 true,
	"kubernetes_secret_v1":              true,
	"vault_generic_secret":              true,
	"vault_kv_secret_v2":                true,
	// The two that generate a credential rather than referencing one. Worth flagging
	// for a different reason than the rest: whatever they produce lands in the state
	// file in plain text, which is the fact a reader auditing this configuration needs.
	"random_password": true,
	"tls_private_key": true,
}

// localModuleSource resolves a module source that names a directory in this repository.
//
// Terraform's rule is unambiguous and worth relying on: a source is a local path *only*
// if it begins with `./` or `../`. Everything else — `hashicorp/vpc/aws`,
// `git::https://…`, `registry.terraform.io/…` — is remote, and a reader that guessed
// otherwise would resolve a registry module to a directory that happens to share its
// name.
//
// The path is resolved against the calling file's directory, since that is what
// Terraform does and what makes `../modules/rds` mean anything. A source that escapes the
// repository root resolves to nothing rather than to a path outside the tree.
func localModuleSource(file, src string) (string, bool) {
	if !strings.HasPrefix(src, "./") && !strings.HasPrefix(src, "../") {
		return "", false
	}
	// A submodule inside a local module — "./modules/vpc//subdir" — addresses the same
	// directory tree; the `//` separator is a remote-source convention that Terraform
	// also tolerates locally.
	if i := strings.Index(src, "//"); i > 0 {
		src = src[:i]
	}
	dir := path.Dir(file)
	joined := path.Join(dir, src)
	if joined == "." || joined == "/" {
		return "", false
	}
	// path.Join has already collapsed `..`, so a remaining one escaped the root.
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", false
	}
	// A source resolving to the calling file's own directory — `source = "./"` — is a
	// module calling itself, which Terraform rejects as a cycle. Recording it would draw
	// a self-edge asserting a directory depends on itself.
	if joined == dir {
		return "", false
	}
	return joined, true
}

// isTFVars reports whether a path is a variable-values file rather than configuration.
func isTFVars(rel string) bool {
	lower := strings.ToLower(path.Base(rel))
	return strings.HasSuffix(lower, ".tfvars") || strings.HasSuffix(lower, ".tfvars.json")
}

// matchTerraform claims Terraform configuration and variable files.
//
// `.tf.json` is excluded deliberately: it is the JSON syntax variant, a different grammar
// that this block-header parser would read as one long unrecognised block. It is rare
// enough that reporting it as an unhandled gap is more honest than half-reading it.
func matchTerraform(f discover.File) bool {
	lower := strings.ToLower(path.Base(f.Path))
	if strings.HasSuffix(lower, ".tf.json") {
		return false
	}
	return strings.HasSuffix(lower, ".tf") || isTFVars(f.Path)
}
