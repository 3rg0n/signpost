package manifest

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// GitHub Actions workflow extraction.
//
// Design §4.1 asks a workflow for "how it builds, tests, ships, and what gates
// exist". The last of those is the one worth the most: an agent about to open a pull
// request needs to know which checks will run against it, and that is stated nowhere
// else in the repository. A contributor who knows `lint` runs on every PR writes
// different code than one who finds out from a red build.
//
// Secrets are references only, as everywhere in this package. A workflow's
// `${{ secrets.NPM_TOKEN }}` names a repository secret, and the name is exactly the
// architectural fact — which credentials this pipeline needs — while the value is not
// in the file to begin with.

// ExtractWorkflow reads a GitHub Actions workflow or a composite action.
func ExtractWorkflow(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindWorkflow}
	root, diag := ParseYAMLDoc(f.Content)
	facts.applyDiag(diag)

	// A workflow's `name` is optional; GitHub falls back to the file path, so this
	// does too rather than leaving jobs attributed to nothing.
	wfName := root.Get("name").String()
	if wfName == "" {
		wfName = f.Path
	}

	triggers, defaultBranchPush := workflowTriggers(root)
	// A workflow-level permissions block applies to every job that does not override
	// it, which is the common case — most repositories set it once at the top.
	wfPerms := workflowPermissions(root.Get("permissions"))

	jobs := root.Get("jobs")
	jobs.Each(func(id string, spec *Node) bool {
		job := Job{
			Name:     firstNonEmpty(spec.Get("name").String(), id),
			Key:      id,
			Workflow: wfName,
			Uses:     spec.Get("uses").String(),
			Needs:    spec.Get("needs").Strings(),
			Line:     lineOf(spec, jobs.Line),
		}
		// runs-on takes a string, a list, or a `{group, labels}` object.
		if r := spec.Get("runs-on"); !r.IsZero() {
			if r.Kind == KindMap {
				job.Runner = strings.Join(append(r.Get("labels").Strings(), r.Get("group").Strings()...), ",")
			} else {
				job.Runner = strings.Join(r.Strings(), ",")
			}
		}
		job.Permissions = workflowPermissions(spec.Get("permissions"))
		if len(job.Permissions) == 0 {
			job.Permissions = wfPerms
		}
		// A gate is a job that runs on pull_request, or on a push to the default
		// branch. Every job in such a workflow is one, since GitHub's required-checks
		// configuration operates on job names and any of them can be selected. Which of
		// them is actually *required* is branch protection — repository configuration,
		// not stated in the tree — so this field does not claim a job blocks a merge,
		// and neither does anything reading it.
		job.Gate = defaultBranchPush

		steps := spec.Get("steps")
		for _, s := range steps.Seq() {
			step := Step{
				Name: s.Get("name").String(),
				Uses: s.Get("uses").String(),
				Run:  s.Get("run").String(),
				Line: s.Line,
			}
			if step.Name == "" {
				step.Name = firstNonEmpty(step.Uses, firstLine(step.Run))
			}
			job.Steps = append(job.Steps, step)
			// A third-party action is a supply-chain dependency of the build, in
			// exactly the sense a package dependency is, and it is one Dependabot
			// tracks. Local actions (`./.github/actions/x`) are this repository's own.
			if step.Uses != "" && !strings.HasPrefix(step.Uses, "./") {
				name, ver := splitActionRef(step.Uses)
				facts.Deps = append(facts.Deps, Dep{
					Name: name, Version: ver, Scope: ScopeBuild,
					Ecosystem: "github-actions", Line: step.Line,
				})
			}
			collectWorkflowSecrets(&facts, s, step.Line)
		}
		// A reusable workflow is a build dependency too, and its `secrets: inherit`
		// hands the caller's whole secret set to it — worth being visible.
		if job.Uses != "" && !strings.HasPrefix(job.Uses, "./") {
			name, ver := splitActionRef(job.Uses)
			facts.Deps = append(facts.Deps, Dep{
				Name: name, Version: ver, Scope: ScopeBuild,
				Ecosystem: "github-actions", Line: job.Line,
			})
		}
		collectWorkflowSecrets(&facts, spec, job.Line)

		// Container jobs and service containers run real images, which is the same
		// fact a compose file states.
		for _, n := range []*Node{spec.Get("container"), spec.Get("services")} {
			for _, ref := range containerImageRefs(n) {
				facts.Images = append(facts.Images, Image{Ref: ref, Line: job.Line})
			}
		}

		facts.Jobs = append(facts.Jobs, job)
		return true
	})

	switch {
	case jobs != nil:
	case !root.Get("runs").IsZero():
		// A composite or Docker action: `runs:` instead of `jobs:`. Its steps are the
		// action's implementation, recorded as a single job so the shape stays uniform.
		facts.Kind = KindWorkflow
		job := Job{Name: wfName, Workflow: wfName, Line: 1}
		for _, s := range root.Path("runs", "steps").Seq() {
			step := Step{
				Name: s.Get("name").String(), Uses: s.Get("uses").String(),
				Run: s.Get("run").String(), Line: s.Line,
			}
			if step.Name == "" {
				step.Name = firstNonEmpty(step.Uses, firstLine(step.Run))
			}
			job.Steps = append(job.Steps, step)
		}
		if img := root.Path("runs", "image").String(); img != "" {
			facts.Images = append(facts.Images, Image{Ref: img, Base: true, Line: 1})
		}
		facts.Jobs = append(facts.Jobs, job)
	default:
		facts.markIncomplete("no jobs or runs block found")
	}

	// The triggers go on the facts as scripts, which is a deliberate reuse rather
	// than a new field: "on: pull_request" is a named thing that causes work to
	// happen, and a consumer rendering "what runs when" wants it next to the jobs.
	for _, t := range triggers {
		facts.Scripts = append(facts.Scripts, Script{Name: "on:" + t.Key, Command: t.Value, Line: t.Line})
	}
	return facts
}

// workflowTriggers reads the `on:` block, returning each trigger and whether any of
// them can gate a merge.
//
// `on` is YAML 1.1's boolean `true`, so a conforming parser turns the key into `true`
// and the block is lost. The tolerant reader keeps keys as written, which is one of
// the concrete reasons it exists — but the alias is checked anyway, since a values
// file that went through another tool may carry it.
func workflowTriggers(root *Node) ([]KeyValue, bool) {
	on := root.GetAny("on", "true", "True")
	if on == nil {
		return nil, false
	}
	var out []KeyValue
	gate := false
	switch on.Kind {
	case KindScalar, KindSeq:
		for _, t := range on.Strings() {
			out = append(out, KeyValue{Key: t, Line: on.Line})
			if isGateTrigger(t, nil) {
				gate = true
			}
		}
	case KindMap:
		on.Each(func(name string, spec *Node) bool {
			// The branch filter is the detail that decides whether a push gates: a
			// push to a release tag is not a merge gate, a push to main is.
			detail := strings.Join(spec.GetAny("branches", "branches-ignore", "types", "tags").Strings(), ",")
			out = append(out, KeyValue{Key: name, Value: detail, Line: lineOf(spec, on.Line)})
			if isGateTrigger(name, spec) {
				gate = true
			}
			return true
		})
	}
	return out, gate
}

// defaultBranchNames are the branches a push to which is a merge gate.
//
// A repository's real default branch is a remote fact this tool does not have —
// design §5 keeps extraction offline — so the convention is used. Being wrong here
// over-reports a gate rather than under-reporting one, which is the right direction:
// a contributor told a check might block them loses nothing by double-checking.
var defaultBranchNames = map[string]bool{
	"main": true, "master": true, "trunk": true, "develop": true, "development": true,
}

// isGateTrigger reports whether a trigger means this workflow can block a merge.
func isGateTrigger(name string, spec *Node) bool {
	switch name {
	case "pull_request", "pull_request_target", "merge_group":
		return true
	case "push":
		branches := spec.Get("branches").Strings()
		if len(branches) == 0 {
			// An unfiltered push includes the default branch.
			return spec.Get("tags") == nil
		}
		for _, b := range branches {
			if defaultBranchNames[b] || b == "**" || b == "*" {
				return true
			}
		}
	}
	return false
}

// workflowPermissions flattens a permissions block.
//
// Two forms: `permissions: read-all` and a map of scope to level. Both come back as
// `scope:level` strings, since the pair is what carries the meaning — `contents:write`
// is a very different posture from `contents:read`.
func workflowPermissions(n *Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind != KindMap {
		if s := n.String(); s != "" {
			return []string{s}
		}
		return nil
	}
	var out []string
	n.Each(func(scope string, level *Node) bool {
		out = append(out, scope+":"+level.String())
		return true
	})
	return out
}

// collectWorkflowSecrets records every `secrets.X` reference in a node's subtree.
//
// One traversal, carrying the key each scalar sits under. Two passes — a subtree walk
// plus a separate read of the env block — would record the same reference twice with
// different attribution, and a bundle that lists NPM_TOKEN once bare and once bound to
// a variable reads as two secrets where there is one.
func collectWorkflowSecrets(facts *Facts, n *Node, line int) {
	// `secrets: inherit` on a reusable-workflow call passes the caller's entire
	// secret set. That is a real and broad grant, recorded under its own name because
	// there is no individual secret to name.
	if s := n.Get("secrets"); s != nil && s.String() == "inherit" {
		facts.SecretRefs = append(facts.SecretRefs, SecretRef{
			Name: "inherit", Source: "github-secret", Line: lineOf(s, line),
		})
	}
	walkScalars(n, "", func(key, text string, at int) {
		// The key is the variable the secret is bound to only where a binding is what
		// the key means: an env entry, or a `with:`/`secrets:` input. Under `run:` the
		// key is the instruction name and would be misleading as an EnvVar.
		envVar := ""
		if key != "run" && key != "if" && key != "uses" && key != "name" {
			envVar = key
		}
		for _, ref := range expressionSecretNames(text) {
			facts.SecretRefs = append(facts.SecretRefs, SecretRef{
				Name: ref, EnvVar: envVar, Source: "github-secret", Line: at,
			})
		}
	})
}

// expressionSecretNames extracts the names in `${{ secrets.NAME }}` expressions.
//
// Names only. There is no value here to leak — GitHub substitutes it at run time —
// which is exactly why a workflow is the cleanest illustration of the reference rule:
// the file names the credential it needs and never contains it.
func expressionSecretNames(s string) []string {
	if !strings.Contains(s, "secrets.") {
		return nil
	}
	var out []string
	rest := s
	for {
		i := strings.Index(rest, "secrets.")
		if i < 0 {
			return out
		}
		rest = rest[i+len("secrets."):]
		end := 0
		for end < len(rest) && isVarChar(rest[end]) {
			end++
		}
		if end > 0 {
			out = append(out, rest[:end])
		}
		rest = rest[end:]
	}
}

// walkScalars calls fn for every scalar in a subtree, with the mapping key it sits
// under and its line. A sequence element inherits the key of the mapping the sequence
// belongs to, which is what a caller wants: an item under `env:` is still an env entry.
func walkScalars(n *Node, key string, fn func(key, text string, line int)) {
	if n == nil {
		return
	}
	switch n.Kind {
	case KindScalar:
		fn(key, n.Str, n.Line)
	case KindMap:
		for i, v := range n.Vals {
			walkScalars(v, n.Keys[i], fn)
		}
	case KindSeq:
		for _, v := range n.Items {
			walkScalars(v, key, fn)
		}
	}
}

// containerImageRefs reads the image out of a container spec or a services map.
func containerImageRefs(n *Node) []string {
	if n == nil {
		return nil
	}
	// The shorthand: `container: node:22`.
	if n.Kind == KindScalar {
		if s := n.String(); s != "" {
			return []string{s}
		}
		return nil
	}
	if img := n.Get("image").String(); img != "" {
		return []string{img}
	}
	// A services map: each value is a container spec.
	var out []string
	n.Each(func(_ string, spec *Node) bool {
		out = append(out, containerImageRefs(spec)...)
		return true
	})
	return out
}

// splitActionRef splits `owner/repo/path@ref` into name and version.
//
// The ref is kept as written, whether a tag or a commit SHA, because which one it is
// answers a question the bundle should surface: a SHA-pinned action cannot be
// silently retagged under you, and a tag-pinned one can.
func splitActionRef(s string) (string, string) {
	if i := strings.LastIndex(s, "@"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// firstLine returns the first non-blank line of a block, trimmed — enough to name a
// step by what it does when the author gave it no name.
func firstLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			return t
		}
	}
	return ""
}
