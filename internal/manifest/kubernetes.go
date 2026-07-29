package manifest

import (
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// Kubernetes and Helm extraction.
//
// Design §4.1 asks for "deployable units, config surface, secrets *references*", and
// the emphasis in the original is on the last word. This is the file kind where the
// secrets rule is load-bearing rather than theoretical: a Secret manifest's `data:`
// block holds base64-encoded credentials, it is trivially decodable, and it is
// sometimes committed. A reader that recorded those values would put live credentials
// into a bundle that gets pushed and published as a page.
//
// So the Secret reader below reads `metadata.name` and the *keys* under `data:`, and
// never the values. Knowing that `db-creds` exists and carries a key named `password`
// is the whole of the architectural signal; the bytes are the part that must not
// travel.

// workloadKinds are the resource kinds that run something. Only these become
// Services: a ConfigMap is config surface, not a deployable unit, and reporting one as
// a service would put a node in the graph for something that never runs.
var workloadKinds = map[string]bool{
	"Deployment": true, "StatefulSet": true, "DaemonSet": true,
	"Job": true, "CronJob": true, "ReplicaSet": true, "Pod": true,
	"Rollout": true, // Argo Rollouts, which replaces Deployment where it is used.
}

// ExtractKubernetes reads a Kubernetes manifest file, which may hold many documents.
//
// This is where the content sniffing that classify.go deliberately left out happens:
// a file under `deploy/` may be a Kubernetes resource or may be unrelated YAML, and
// only `apiVersion` plus `kind` settles it. Keeping the walk filename-only and the
// decision here is what makes classification cheap and this reader honest.
func ExtractKubernetes(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindKubernetes}
	docs, diag := ParseYAML(f.Content)
	facts.applyDiag(diag)

	for _, doc := range docs {
		kind := doc.Get("kind").String()
		if kind == "" || doc.Get("apiVersion").String() == "" {
			continue
		}
		name := doc.Path("metadata", "name").String()
		ns := doc.Path("metadata", "namespace").String()

		switch {
		case workloadKinds[kind]:
			readWorkload(&facts, doc, kind, name, ns)
		case kind == "Secret":
			readSecretManifest(&facts, doc, name, ns)
		case kind == "ConfigMap":
			// Config surface: the keys are what a service reads, and unlike a Secret
			// the values are not credentials — but they are also not architecture, and
			// a ConfigMap routinely holds an entire nginx.conf. Keys only, on the
			// grounds of relevance rather than safety.
			svc := Service{Name: name, Kind: kind, Namespace: ns, Line: doc.Line}
			svc.EnvKeys = doc.Get("data").MapKeys()
			svc.EnvKeys = append(svc.EnvKeys, doc.Get("stringData").MapKeys()...)
			facts.Services = append(facts.Services, svc)
		case kind == "Service":
			// A Service is how a workload is reached: its ports are the interface
			// other services actually connect to, which is a different and more useful
			// fact than the container's own port.
			svc := Service{Name: name, Kind: kind, Namespace: ns, Line: doc.Line}
			for _, p := range doc.Path("spec", "ports").Seq() {
				svc.Ports = append(svc.Ports, k8sPortString(p))
			}
			// The selector names the workload this fronts, which is the edge between
			// the two. Kept as label=value: a selector is matched on the pair, and the
			// key alone would not identify the target.
			for _, kv := range doc.Path("spec", "selector").KeyValues() {
				svc.DependsOn = append(svc.DependsOn, kv.Key+"="+kv.Value)
			}
			facts.Services = append(facts.Services, svc)
		case kind == "Ingress":
			// An Ingress is the repository's externally reachable surface, which is
			// the highest-value single fact in a deployment directory: it is where
			// traffic from outside enters.
			svc := Service{Name: name, Kind: kind, Namespace: ns, Line: doc.Line}
			for _, rule := range doc.Path("spec", "rules").Seq() {
				host := rule.Get("host").String()
				for _, p := range rule.Path("http", "paths").Seq() {
					backend := firstNonEmpty(
						p.Path("backend", "service", "name").String(),
						p.Path("backend", "serviceName").String(),
					)
					svc.Ports = append(svc.Ports, host+p.Get("path").String())
					if backend != "" {
						svc.DependsOn = append(svc.DependsOn, backend)
					}
				}
			}
			for _, t := range doc.Path("spec", "tls").Seq() {
				// The TLS certificate lives in a Secret, and the reference is the fact.
				if sn := t.Get("secretName").String(); sn != "" {
					facts.SecretRefs = append(facts.SecretRefs, SecretRef{
						Name: sn, Source: "kubernetes-secret", Line: t.Line,
					})
				}
			}
			facts.Services = append(facts.Services, svc)
		case kind == "CustomResourceDefinition":
			// A CRD defines an interface other manifests are written against, which
			// makes it a contract in the same sense a proto service is.
			facts.Contracts = append(facts.Contracts, Contract{
				Name: name, Kind: "crd",
				Package: doc.Path("spec", "group").String(),
				Detail:  doc.Path("spec", "names", "kind").String(),
				Line:    doc.Line,
			})
		default:
			// Every other kind — RBAC, HPA, NetworkPolicy, PVC, ServiceAccount.
			// Recorded as a named unit so the deployment's inventory is complete,
			// without inventing fields for kinds whose detail nothing consumes yet.
			facts.Services = append(facts.Services, Service{
				Name: name, Kind: kind, Namespace: ns, Line: doc.Line,
			})
		}

		// Secret references appear across every kind, not only in workloads: an
		// annotation naming a cert-manager issuer secret, a ServiceAccount's
		// imagePullSecrets. One subtree walk catches them all.
		collectK8sSecretRefs(&facts, doc)
	}

	// A file under deploy/ that holds no apiVersion+kind document is simply not a
	// Kubernetes manifest, and empty facts say that. Not a diagnostic: nothing was
	// missed, the file is not what its directory suggested — which is precisely why
	// classify.go leaves the decision to content and this reader makes it.
	return facts
}

// readWorkload reads a Deployment, StatefulSet, CronJob, or Pod.
func readWorkload(facts *Facts, doc *Node, kind, name, ns string) {
	svc := Service{Name: name, Kind: kind, Namespace: ns, Line: doc.Line}
	if r := doc.Path("spec", "replicas"); !r.IsZero() {
		svc.Replicas = r.String()
	}

	// The pod template sits at a different depth per kind: a CronJob nests a JobSpec
	// inside a JobTemplateSpec, and a Pod is its own template. Trying each known path
	// is shorter than a kind switch and does not fail on the next kind that appears.
	spec := firstNonNil(
		doc.Path("spec", "template", "spec"),
		doc.Path("spec", "jobTemplate", "spec", "template", "spec"),
		doc.Get("spec"),
	)

	for _, c := range append(spec.Get("containers").Seq(), spec.Get("initContainers").Seq()...) {
		if img := c.Get("image").String(); img != "" {
			svc.Image = firstNonEmpty(svc.Image, img)
			facts.Images = append(facts.Images, Image{Ref: img, Line: c.Line})
		}
		for _, p := range c.Get("ports").Seq() {
			svc.Ports = append(svc.Ports, k8sPortString(p))
		}
		// env is a list of {name, value} or {name, valueFrom} objects, which
		// Node.KeyValues already normalises.
		for _, kv := range c.Get("env").KeyValues() {
			svc.EnvKeys = append(svc.EnvKeys, kv.Key)
			readValueFrom(facts, kv, kv.Node.Get("valueFrom"))
		}
		// envFrom pulls a whole ConfigMap or Secret into the environment. The
		// individual variable names are not stated here — they are in the referenced
		// object — so the reference itself is the fact.
		for _, ef := range c.Get("envFrom").Seq() {
			if sn := ef.Path("secretRef", "name").String(); sn != "" {
				facts.SecretRefs = append(facts.SecretRefs, SecretRef{
					Name: sn, Source: "kubernetes-secret", Line: ef.Line,
				})
			}
			if cm := ef.Path("configMapRef", "name").String(); cm != "" {
				svc.DependsOn = append(svc.DependsOn, cm)
			}
		}
		if cmd := c.GetAny("command", "args"); !cmd.IsZero() {
			facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
				Name: firstNonEmpty(c.Get("name").String(), name),
				Path: strings.Join(cmd.Strings(), " "), Line: cmd.Line,
			})
		}
	}

	// Volumes name the ConfigMaps, Secrets, and claims a workload mounts.
	for _, v := range spec.Get("volumes").Seq() {
		vn := v.Get("name").String()
		switch {
		case v.Get("secret") != nil:
			sn := firstNonEmpty(v.Path("secret", "secretName").String(), v.Path("secret", "name").String())
			facts.SecretRefs = append(facts.SecretRefs, SecretRef{
				Name: sn, Source: "kubernetes-secret", Line: v.Line,
			})
			svc.Volumes = append(svc.Volumes, vn+":secret/"+sn)
		case v.Get("configMap") != nil:
			cm := v.Path("configMap", "name").String()
			svc.Volumes = append(svc.Volumes, vn+":configMap/"+cm)
			svc.DependsOn = append(svc.DependsOn, cm)
		case v.Get("persistentVolumeClaim") != nil:
			svc.Volumes = append(svc.Volumes, vn+":pvc/"+v.Path("persistentVolumeClaim", "claimName").String())
		default:
			svc.Volumes = append(svc.Volumes, vn)
		}
	}
	for _, ps := range spec.Get("imagePullSecrets").Seq() {
		if n := ps.Get("name").String(); n != "" {
			facts.SecretRefs = append(facts.SecretRefs, SecretRef{
				Name: n, Source: "kubernetes-secret", Line: ps.Line,
			})
		}
	}
	facts.Services = append(facts.Services, svc)
}

// readValueFrom records where an environment variable's value comes from.
//
// A `secretKeyRef` is the canonical shape of the fact this package exists to capture:
// the variable name, the Secret's name, and the key inside it — three references and
// no value.
func readValueFrom(facts *Facts, kv KeyValue, from *Node) {
	if from == nil {
		return
	}
	if s := from.Get("secretKeyRef"); s != nil {
		facts.SecretRefs = append(facts.SecretRefs, SecretRef{
			Name:   s.Get("name").String(),
			Key:    s.Get("key").String(),
			EnvVar: kv.Key,
			Source: "kubernetes-secret",
			Line:   lineOf(s, kv.Line),
		})
	}
}

// readSecretManifest reads a Secret's identity and key names, never its values.
//
// This function is the one place in signpost where walking past data it can trivially
// read is the entire point. `data:` is base64, `stringData:` is plaintext, and both
// hold credentials. The names of the keys are recorded because a service's
// `secretKeyRef` points at one of them, and that cross-reference is what makes the
// deployment graph connect.
func readSecretManifest(facts *Facts, doc *Node, name, ns string) {
	facts.Services = append(facts.Services, Service{
		Name: name, Kind: "Secret", Namespace: ns, Line: doc.Line,
	})
	keys := append(doc.Get("data").MapKeys(), doc.Get("stringData").MapKeys()...)
	if len(keys) == 0 {
		facts.SecretRefs = append(facts.SecretRefs, SecretRef{
			Name: name, Source: "kubernetes-secret", Line: doc.Line,
		})
		return
	}
	for _, k := range keys {
		facts.SecretRefs = append(facts.SecretRefs, SecretRef{
			Name: name, Key: k, Source: "kubernetes-secret", Line: doc.Line,
		})
	}
}

// collectK8sSecretRefs finds secret references anywhere in a document.
//
// The keys checked are the conventional ones across the ecosystem's resource kinds and
// the controllers that extend it — cert-manager writes `secretName`, external-secrets
// writes `secretStoreRef`. A key-name walk catches these without this package needing
// to know every CRD that exists.
func collectK8sSecretRefs(facts *Facts, doc *Node) {
	walkNodes(doc, func(key string, n *Node) {
		switch key {
		case "secretName", "sslSecretName", "tlsSecretName", "caSecretName":
			if s := n.String(); s != "" {
				facts.SecretRefs = append(facts.SecretRefs, SecretRef{
					Name: s, Source: "kubernetes-secret", Line: n.Line,
				})
			}
		case "secretRef", "secretKeyRef", "secretStoreRef":
			if s := n.Get("name").String(); s != "" {
				facts.SecretRefs = append(facts.SecretRefs, SecretRef{
					Name: s, Key: n.Get("key").String(),
					Source: "kubernetes-secret", Line: n.Line,
				})
			}
		}
	})
}

// walkNodes calls fn for every node in a subtree with the mapping key it sits under.
func walkNodes(n *Node, fn func(key string, n *Node)) {
	if n == nil {
		return
	}
	switch n.Kind {
	case KindMap:
		for i, v := range n.Vals {
			fn(n.Keys[i], v)
			walkNodes(v, fn)
		}
	case KindSeq:
		for _, v := range n.Items {
			walkNodes(v, fn)
		}
	}
}

// k8sPortString renders a port entry as text.
func k8sPortString(p *Node) string {
	if p.Kind == KindScalar {
		return p.String()
	}
	target := firstNonEmpty(p.Get("containerPort").String(), p.Get("targetPort").String())
	published := firstNonEmpty(p.Get("port").String(), p.Get("nodePort").String())
	proto := p.Get("protocol").String()
	out := target
	if published != "" && published != target {
		out = published + ":" + target
	}
	if out == "" {
		out = published
	}
	// TCP is the default, so recording it adds nothing; UDP is worth saying.
	if proto != "" && !strings.EqualFold(proto, "TCP") {
		out += "/" + proto
	}
	return out
}

// firstNonNil returns the first non-nil node.
func firstNonNil(nodes ...*Node) *Node {
	for _, n := range nodes {
		if n != nil {
			return n
		}
	}
	return nil
}

// ExtractHelmChart reads a Chart.yaml.
//
// A chart's identity and its dependencies are exact — Chart.yaml is ordinary YAML with
// no templating — which makes this the one Helm file that reads like a normal manifest.
func ExtractHelmChart(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindHelmChart}
	root, diag := ParseYAMLDoc(f.Content)
	facts.applyDiag(diag)

	facts.Module = Module{
		Name:      root.Get("name").String(),
		Version:   root.Get("version").String(),
		Ecosystem: "helm",
		// appVersion is the version of the software the chart deploys, which is what
		// someone reading the bundle actually wants to know.
		LangVersion: root.Get("appVersion").String(),
		Line:        1,
	}
	for _, d := range root.GetAny("dependencies", "requirements").Seq() {
		name := d.Get("name").String()
		if name == "" {
			continue
		}
		facts.Deps = append(facts.Deps, Dep{
			Name: name, Version: d.Get("version").String(), Scope: ScopeRuntime,
			Ecosystem: "helm", Source: d.Get("repository").String(),
			// A conditional subchart is only deployed when a value enables it, which
			// is optionality in exactly the sense the field means.
			Optional: !d.Get("condition").IsZero(),
			Line:     d.Line,
		})
	}
	return facts
}

// ExtractHelmValues reads a values.yaml.
//
// A values file is the chart's config surface: the knobs an operator turns. The
// interesting facts are which images it pins, whether ingress is on, and which secret
// names it references — not the whole tree, which is often hundreds of lines of
// defaults nobody changes.
func ExtractHelmValues(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindHelmValues}
	root, diag := ParseYAMLDoc(f.Content)
	facts.applyDiag(diag)

	// An `image:` block anywhere in the tree pins what runs. The nesting is by
	// convention rather than schema — a chart may put it at the root or under each
	// subchart's key — so this is a walk rather than a fixed path.
	walkNodes(root, func(key string, n *Node) {
		if key != "image" {
			return
		}
		if n.Kind == KindScalar {
			if s := n.String(); s != "" {
				facts.Images = append(facts.Images, Image{Ref: s, Line: n.Line})
			}
			return
		}
		repo := firstNonEmpty(n.Get("repository").String(), n.Get("name").String())
		if repo == "" {
			return
		}
		ref := repo
		if tag := firstNonEmpty(n.Get("tag").String(), n.Get("digest").String()); tag != "" {
			sep := ":"
			if strings.HasPrefix(tag, "sha256:") {
				sep = "@"
			}
			ref += sep + tag
		}
		facts.Images = append(facts.Images, Image{Ref: ref, Line: n.Line})
	})

	collectK8sSecretRefs(&facts, root)
	// existingSecret is the chart convention for "use a secret I already created",
	// and it is how a well-run deployment avoids putting credentials in values.yaml
	// at all — so its presence is a good sign worth recording.
	walkNodes(root, func(key string, n *Node) {
		if !strings.EqualFold(key, "existingSecret") && !strings.EqualFold(key, "existingClaim") {
			return
		}
		if s := n.String(); s != "" {
			facts.SecretRefs = append(facts.SecretRefs, SecretRef{
				Name: s, Source: "helm-values", Line: n.Line,
			})
		}
	})

	// The top-level keys are the config surface itself: what this chart lets you
	// change. Recorded as one service so a consumer can render the surface without
	// the bundle carrying every default value.
	svc := Service{Name: "values", Kind: "helm-values", EnvKeys: root.MapKeys(), Line: 1}
	if e, ok := root.Path("ingress", "enabled").Bool(); ok && e {
		// An enabled ingress means this chart publishes something externally.
		svc.Ports = append(svc.Ports, "ingress")
	}
	facts.Services = append(facts.Services, svc)
	return facts
}
