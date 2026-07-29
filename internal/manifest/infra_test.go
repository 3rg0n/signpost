package manifest

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func infraFile(p, content string) discover.File {
	return discover.File{Path: p, Class: discover.ClassInfra, Content: content}
}

// serviceOf finds a service by name, failing if absent.
func serviceOf(t *testing.T, f Facts, name string) Service {
	t.Helper()
	for _, s := range f.Services {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no service named %q in %v", name, f.ServiceNames())
	return Service{}
}

func jobOf(t *testing.T, f Facts, name string) Job {
	t.Helper()
	for _, j := range f.Jobs {
		if j.Name == name {
			return j
		}
	}
	t.Fatalf("no job named %q in %v", name, f.JobNames())
	return Job{}
}

// secretRefOf finds a secret reference by name and key.
func secretRefOf(t *testing.T, f Facts, name, key string) SecretRef {
	t.Helper()
	for _, s := range f.SecretRefs {
		if s.Name == name && s.Key == key {
			return s
		}
	}
	t.Fatalf("no secret reference %q/%q in %+v", name, key, f.SecretRefs)
	return SecretRef{}
}

func joined(ss []string) string { return strings.Join(ss, ",") }

func TestContainerfileExtraction(t *testing.T) {
	facts := ExtractContainerfile(infraFile("services/api/Containerfile", `
# syntax=docker/dockerfile:1
ARG GO_VERSION=1.24
FROM cgr.dev/chainguard/go:${GO_VERSION} AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && \
    go build -o /out/api ./cmd/api

FROM builder AS test
RUN go test ./...

FROM cgr.dev/chainguard/static:latest
COPY --from=builder /out/api /api
ENV PORT=8080 LOG_LEVEL=info
ENV DATABASE_PASSWORD=""
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/api"]
CMD ["--serve"]
`))
	facts.Normalize()

	if facts.Incomplete {
		t.Errorf("unexpected incompleteness: %s", facts.Note)
	}
	// An ARG default resolves the templated FROM, so the base image is exact rather
	// than being recorded as "${GO_VERSION}".
	refs := facts.ImageRefs()
	if !contains(refs, "cgr.dev/chainguard/go:1.24") {
		t.Errorf("images = %v, want the ARG resolved", refs)
	}
	if !contains(refs, "cgr.dev/chainguard/static:latest") {
		t.Errorf("images = %v, want the final base", refs)
	}
	// `FROM builder` names an earlier stage. It is internal to this build, not a
	// registry pull, and recording it as an image would invent one.
	if contains(refs, "builder") {
		t.Errorf("a stage reference is not an image: %v", refs)
	}
	svc := serviceOf(t, facts, "api")
	if joined(svc.Ports) != "8080" {
		t.Errorf("ports = %v", svc.Ports)
	}
	if !contains(svc.EnvKeys, "PORT") || !contains(svc.EnvKeys, "LOG_LEVEL") {
		t.Errorf("env keys = %v", svc.EnvKeys)
	}
	// The name is recorded; the value never is.
	ref := secretRefOf(t, facts, "DATABASE_PASSWORD", "")
	if ref.Source != "containerfile-env" {
		t.Errorf("secret source = %q", ref.Source)
	}
	if len(facts.Entrypoints) != 1 || facts.Entrypoints[0].Path != "/api --serve" {
		t.Errorf("entrypoints = %+v, want ENTRYPOINT and CMD joined", facts.Entrypoints)
	}
}

// A multi-line RUN is how every real Containerfile keeps its layer count down; without
// continuation joining each wrapped line parses as a bogus instruction.
func TestContainerfileContinuationsDoNotProduceDiagnostics(t *testing.T) {
	facts := ExtractContainerfile(infraFile("Containerfile", `FROM alpine
RUN apk add --no-cache \
    curl \
    ca-certificates && \
    rm -rf /var/cache/apk/*
EXPOSE 80
`))
	if facts.Incomplete {
		t.Errorf("continuation lines should not read as instructions: %s", facts.Note)
	}
	if joined(facts.ImageRefs()) != "alpine" {
		t.Errorf("images = %v", facts.ImageRefs())
	}
}

func TestContainerfileServiceNaming(t *testing.T) {
	cases := map[string]string{
		"services/api/Containerfile": "api",
		"Dockerfile":                 "image",
		"deploy/Dockerfile.worker":   "deploy-worker",
		"build/worker.containerfile": "build-worker",
		"apps/web/Dockerfile":        "web",
	}
	for p, want := range cases {
		facts := ExtractContainerfile(infraFile(p, "FROM scratch\n"))
		if got := facts.Services[0].Name; got != want {
			t.Errorf("%s -> %q, want %q", p, got, want)
		}
	}
}

func TestContainerfileUnknownInstructionIsRecorded(t *testing.T) {
	facts := ExtractContainerfile(infraFile("Containerfile", "FROM scratch\nFROBNICATE x\n"))
	if !facts.Incomplete {
		t.Error("an unrecognised instruction should mark the facts incomplete")
	}
}

func TestComposeExtraction(t *testing.T) {
	facts := ExtractCompose(infraFile("compose.yaml", `
services:
  api:
    build:
      context: ./services/api
      dockerfile: Containerfile
      args:
        REGISTRY_TOKEN: ${CI_TOKEN}
    ports:
      - "8080:8080"
      - "127.0.0.1:9090:9090"
    environment:
      LOG_LEVEL: debug
      DATABASE_PASSWORD: ${DB_PASSWORD}
    env_file:
      - .env.local
    depends_on:
      postgres:
        condition: service_healthy
    volumes:
      - ./data:/var/lib/data
    secrets:
      - api_signing_key
  postgres:
    image: docker.io/library/postgres:17
    expose:
      - 5432
    deploy:
      replicas: 2
  worker:
    image: docker.io/library/redis:7
    command: ["redis-server", "--appendonly", "yes"]
    depends_on: [postgres]

secrets:
  api_signing_key:
    file: ./secrets/signing.pem
`))
	facts.Normalize()

	if facts.Incomplete {
		t.Errorf("unexpected incompleteness: %s", facts.Note)
	}
	api := serviceOf(t, facts, "api")
	// The Containerfile path is resolved against the build context so it matches the
	// path the walker discovered, which is what links the two facts together.
	if api.Build != "services/api/Containerfile" {
		t.Errorf("build = %q", api.Build)
	}
	// A loopback binding says something a bare port does not, so the mapping is kept
	// as written.
	if joined(api.Ports) != "127.0.0.1:9090:9090,8080:8080" {
		t.Errorf("ports = %v", api.Ports)
	}
	if !contains(api.EnvKeys, "LOG_LEVEL") || !contains(api.EnvKeys, "DATABASE_PASSWORD") {
		t.Errorf("env keys = %v", api.EnvKeys)
	}
	if !contains(api.EnvKeys, "REGISTRY_TOKEN") {
		t.Errorf("build args are env keys too: %v", api.EnvKeys)
	}
	if joined(api.DependsOn) != "postgres" {
		t.Errorf("depends_on = %v, want the condition-map form read", api.DependsOn)
	}
	if joined(api.Volumes) != "./data:/var/lib/data" {
		t.Errorf("volumes = %v", api.Volumes)
	}
	// A ${VAR} on a credential-shaped key is a reference to the host environment.
	// The name travels; the value is not in the file and never would be.
	if r := secretRefOf(t, facts, "DB_PASSWORD", ""); r.EnvVar != "DATABASE_PASSWORD" {
		t.Errorf("interpolated secret = %+v", r)
	}
	// A non-credential interpolation is not a secret, so it does not become one.
	for _, s := range facts.SecretRefs {
		if s.Name == "LOG_LEVEL" {
			t.Error("an ordinary variable should not be recorded as a secret")
		}
	}
	secretRefOf(t, facts, ".env.local", "")
	secretRefOf(t, facts, "api_signing_key", "")
	// The secret's declared file is a path, not a value, so it is safe to record.
	if r := secretRefOf(t, facts, "api_signing_key", "./secrets/signing.pem"); r.Source != "compose-secret" {
		t.Errorf("compose secret = %+v", r)
	}

	pg := serviceOf(t, facts, "postgres")
	if pg.Image != "docker.io/library/postgres:17" || pg.Replicas != "2" {
		t.Errorf("postgres = %+v", pg)
	}
	if joined(pg.Ports) != "5432" {
		t.Errorf("expose = %v", pg.Ports)
	}
	w := serviceOf(t, facts, "worker")
	if joined(w.DependsOn) != "postgres" {
		t.Errorf("list-form depends_on = %v", w.DependsOn)
	}
	if len(facts.Entrypoints) != 1 || facts.Entrypoints[0].Path != "redis-server --appendonly yes" {
		t.Errorf("entrypoints = %+v", facts.Entrypoints)
	}
}

func TestComposeWithoutServicesIsIncomplete(t *testing.T) {
	facts := ExtractCompose(infraFile("compose.yaml", "version: \"3.9\"\n"))
	if !facts.Incomplete {
		t.Error("a compose file with no services should be reported as unread")
	}
}

func TestWorkflowExtraction(t *testing.T) {
	facts := ExtractWorkflow(infraFile(".github/workflows/ci.yml", `
name: CI

on:
  pull_request:
  push:
    branches: [main]

permissions:
  contents: read

jobs:
  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
      - name: Run golangci-lint
        run: golangci-lint run ./...
  test:
    runs-on: ubuntu-latest
    needs: [lint]
    services:
      postgres:
        image: docker.io/library/postgres:17
    steps:
      - uses: actions/setup-go@v5
      - run: go test ./...
        env:
          NPM_TOKEN: ${{ secrets.NPM_TOKEN }}
  publish:
    runs-on: ubuntu-latest
    needs: [test]
    permissions:
      contents: write
      id-token: write
    steps:
      - run: echo publish
        env:
          REGISTRY_PASSWORD: ${{ secrets.REGISTRY_PASSWORD }}
`))
	facts.Normalize()

	if facts.Incomplete {
		t.Errorf("unexpected incompleteness: %s", facts.Note)
	}
	lint := jobOf(t, facts, "Lint")
	if lint.Workflow != "CI" || lint.Runner != "ubuntu-latest" {
		t.Errorf("lint = %+v", lint)
	}
	// A workflow triggered by pull_request can block a merge, which is the fact §4.1
	// asks for by name.
	if !lint.Gate {
		t.Error("a pull_request-triggered job is a merge gate")
	}
	// Steps keep file order: a job's steps run in sequence, and sorting them would
	// report something false.
	if len(lint.Steps) != 2 || lint.Steps[0].Uses == "" || lint.Steps[1].Name != "Run golangci-lint" {
		t.Errorf("steps = %+v", lint.Steps)
	}
	// A workflow-level permissions block applies to jobs that do not override it.
	if joined(lint.Permissions) != "contents:read" {
		t.Errorf("inherited permissions = %v", lint.Permissions)
	}
	pub := jobOf(t, facts, "publish")
	if joined(pub.Permissions) != "contents:write,id-token:write" {
		t.Errorf("job permissions = %v, want the override not the inherited set", pub.Permissions)
	}
	if joined(jobOf(t, facts, "test").Needs) != "lint" {
		t.Errorf("needs = %v", jobOf(t, facts, "test").Needs)
	}
	// A third-party action is a supply-chain dependency of the build, and whether it
	// is pinned to a SHA or a tag is exactly what the version field should show.
	if d := depOf(t, facts, "actions/checkout", ScopeBuild); len(d.Version) != 40 {
		t.Errorf("checkout pin = %q, want the SHA kept as written", d.Version)
	}
	if d := depOf(t, facts, "actions/setup-go", ScopeBuild); d.Version != "v5" {
		t.Errorf("setup-go = %+v", d)
	}
	// A service container runs a real image.
	if !contains(facts.ImageRefs(), "docker.io/library/postgres:17") {
		t.Errorf("images = %v", facts.ImageRefs())
	}
	// A secret is named, once, with the variable it is bound to.
	if r := secretRefOf(t, facts, "NPM_TOKEN", ""); r.EnvVar != "NPM_TOKEN" {
		t.Errorf("secret ref = %+v", r)
	}
	if got := len(facts.SecretRefs); got != 2 {
		t.Errorf("secret refs = %d (%+v), want one per secret", got, facts.SecretRefs)
	}
	// The triggers are recorded so a consumer can render what runs when.
	if !contains(scriptNames(facts), "on:pull_request") {
		t.Errorf("triggers = %+v", facts.Scripts)
	}
}

// `on` is YAML 1.1's boolean true. A conforming parser turns the key into `true` and
// the trigger block is lost — one of the concrete reasons the tolerant reader exists.
func TestWorkflowOnKeyIsNotABoolean(t *testing.T) {
	facts := ExtractWorkflow(infraFile(".github/workflows/w.yml", `
on: [push, pull_request]
jobs:
  b: { runs-on: ubuntu-latest }
`))
	facts.Normalize()
	if !jobOf(t, facts, "b").Gate {
		t.Error("the on: block was not read")
	}
	if !contains(scriptNames(facts), "on:push") {
		t.Errorf("triggers = %+v", facts.Scripts)
	}
}

// A push to a tag is a release, not a merge gate. Over-reporting a gate is the safer
// error, but a tag-only trigger is unambiguous.
func TestWorkflowTagPushIsNotAGate(t *testing.T) {
	facts := ExtractWorkflow(infraFile(".github/workflows/release.yml", `
on:
  push:
    tags: ['v*']
jobs:
  release: { runs-on: ubuntu-latest }
`))
	facts.Normalize()
	if jobOf(t, facts, "release").Gate {
		t.Error("a tag push does not gate a merge")
	}
}

func TestWorkflowReusableAndInheritedSecrets(t *testing.T) {
	facts := ExtractWorkflow(infraFile(".github/workflows/call.yml", `
on: pull_request
jobs:
  shared:
    uses: cisco-sbg-emu/shared/.github/workflows/build.yml@v2
    secrets: inherit
`))
	facts.Normalize()
	depOf(t, facts, "cisco-sbg-emu/shared/.github/workflows/build.yml", ScopeBuild)
	// `secrets: inherit` hands over the caller's entire secret set — a broad grant
	// with no individual secret to name, so it is recorded under its own.
	secretRefOf(t, facts, "inherit", "")
}

func TestWorkflowCompositeAction(t *testing.T) {
	facts := ExtractWorkflow(infraFile(".github/actions/setup/action.yml", `
name: Setup
runs:
  using: composite
  steps:
    - run: echo hi
      shell: bash
`))
	facts.Normalize()
	if facts.Incomplete {
		t.Errorf("a composite action has runs: not jobs:, which is not a defect: %s", facts.Note)
	}
	if len(facts.Jobs) != 1 || len(facts.Jobs[0].Steps) != 1 {
		t.Errorf("jobs = %+v", facts.Jobs)
	}
}

func TestWorkflowWithoutJobsIsIncomplete(t *testing.T) {
	facts := ExtractWorkflow(infraFile(".github/workflows/x.yml", "name: X\non: push\n"))
	if !facts.Incomplete {
		t.Error("a workflow with no jobs should be reported as unread")
	}
}

func TestKubernetesExtraction(t *testing.T) {
	facts := ExtractKubernetes(infraFile("deploy/api.yaml", `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: prod
spec:
  replicas: 3
  template:
    spec:
      imagePullSecrets:
        - name: registry-creds
      containers:
        - name: api
          image: registry.example.com/api:1.4.0
          command: ["/api", "--serve"]
          ports:
            - containerPort: 8080
            - containerPort: 9090
              protocol: UDP
          env:
            - name: LOG_LEVEL
              value: info
            - name: DATABASE_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: db-creds
                  key: password
          envFrom:
            - configMapRef:
                name: api-config
            - secretRef:
                name: api-secrets
      volumes:
        - name: certs
          secret:
            secretName: api-tls
        - name: config
          configMap:
            name: api-config
---
apiVersion: v1
kind: Service
metadata:
  name: api
spec:
  selector:
    app: api
  ports:
    - port: 80
      targetPort: 8080
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: api
spec:
  tls:
    - secretName: api-tls
  rules:
    - host: api.example.com
      http:
        paths:
          - path: /v1
            backend:
              service:
                name: api
`))
	facts.Normalize()

	if facts.Incomplete {
		t.Errorf("unexpected incompleteness: %s", facts.Note)
	}
	var dep Service
	for _, s := range facts.Services {
		if s.Kind == "Deployment" {
			dep = s
		}
	}
	if dep.Name != "api" || dep.Namespace != "prod" || dep.Replicas != "3" {
		t.Errorf("deployment = %+v", dep)
	}
	if dep.Image != "registry.example.com/api:1.4.0" {
		t.Errorf("image = %q", dep.Image)
	}
	// TCP is the default so it adds nothing; UDP is worth stating.
	if joined(dep.Ports) != "8080,9090/UDP" {
		t.Errorf("ports = %v", dep.Ports)
	}
	if !contains(dep.EnvKeys, "DATABASE_PASSWORD") {
		t.Errorf("env keys = %v", dep.EnvKeys)
	}
	// The canonical shape of the fact this package exists to capture: variable name,
	// Secret name, key inside it — three references and no value.
	r := secretRefOf(t, facts, "db-creds", "password")
	if r.EnvVar != "DATABASE_PASSWORD" || r.Source != "kubernetes-secret" {
		t.Errorf("secretKeyRef = %+v", r)
	}
	secretRefOf(t, facts, "registry-creds", "")
	secretRefOf(t, facts, "api-secrets", "")
	secretRefOf(t, facts, "api-tls", "")
	if !contains(dep.Volumes, "certs:secret/api-tls") {
		t.Errorf("volumes = %v", dep.Volumes)
	}
	if !contains(dep.DependsOn, "api-config") {
		t.Errorf("a mounted ConfigMap is a dependency: %v", dep.DependsOn)
	}
	// A Service's ports are the interface other services connect to, which is a
	// different fact from the container's own port.
	var k8sSvc Service
	for _, s := range facts.Services {
		if s.Kind == "Service" {
			k8sSvc = s
		}
	}
	if joined(k8sSvc.Ports) != "80:8080" {
		t.Errorf("service ports = %v", k8sSvc.Ports)
	}
	if joined(k8sSvc.DependsOn) != "app=api" {
		t.Errorf("selector = %v", k8sSvc.DependsOn)
	}
	var ing Service
	for _, s := range facts.Services {
		if s.Kind == "Ingress" {
			ing = s
		}
	}
	if joined(ing.Ports) != "api.example.com/v1" {
		t.Errorf("ingress = %+v", ing)
	}
	if joined(ing.DependsOn) != "api" {
		t.Errorf("ingress backend = %v", ing.DependsOn)
	}
}

// The rule this package is built around: a Secret's keys are recorded, its values are
// not. base64 is not encryption, and this bundle gets committed and published.
func TestSecretManifestRecordsKeysNeverValues(t *testing.T) {
	// Both are fixtures, and a secret scanner is right to flag them on sight — the
	// exemption is inline so each one stays visible in review rather than hidden behind
	// a config rule that would quietly excuse every future test file too.
	const password = "c3VwZXItc2VjcmV0LXBhc3N3b3Jk" //gitleaks:allow
	const plaintext = "hunter2-in-the-clear"        //gitleaks:allow
	facts := ExtractKubernetes(infraFile("deploy/secret.yaml", `
apiVersion: v1
kind: Secret
metadata:
  name: db-creds
type: Opaque
data:
  password: `+password+`
  username: cG9zdGdyZXM=
stringData:
  token: `+plaintext+`
`))
	facts.Normalize()

	secretRefOf(t, facts, "db-creds", "password")
	secretRefOf(t, facts, "db-creds", "username")
	// stringData is plaintext, and its key is signal for the same reason data's is.
	secretRefOf(t, facts, "db-creds", "token")

	// No value from either block may appear anywhere in the facts. Checked against
	// the whole rendered struct rather than the SecretRef fields alone, because the
	// failure this guards against is a value leaking through some other field.
	rendered := renderFacts(facts)
	for _, secret := range []string{password, plaintext, "cG9zdGdyZXM="} {
		if strings.Contains(rendered, secret) {
			t.Errorf("a secret value reached the facts: %q", secret)
		}
	}
}

func TestConfigMapRecordsKeysOnly(t *testing.T) {
	facts := ExtractKubernetes(infraFile("deploy/cm.yaml", `
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-config
data:
  LOG_LEVEL: debug
  nginx.conf: |
    server { listen 80; }
`))
	facts.Normalize()
	cm := serviceOf(t, facts, "api-config")
	if joined(cm.EnvKeys) != "LOG_LEVEL,nginx.conf" {
		t.Errorf("config keys = %v", cm.EnvKeys)
	}
	// A ConfigMap routinely holds an entire config file. The keys are the surface;
	// the bodies are neither architecture nor worth the bundle's bytes.
	if strings.Contains(renderFacts(facts), "listen 80") {
		t.Error("a ConfigMap value should not be recorded")
	}
}

func TestCronJobTemplateDepth(t *testing.T) {
	facts := ExtractKubernetes(infraFile("deploy/cron.yaml", `
apiVersion: batch/v1
kind: CronJob
metadata:
  name: nightly
spec:
  schedule: "0 3 * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
            - name: job
              image: registry.example.com/nightly:2
`))
	facts.Normalize()
	// A CronJob nests its pod template two levels deeper than a Deployment does.
	if got := serviceOf(t, facts, "nightly").Image; got != "registry.example.com/nightly:2" {
		t.Errorf("cronjob image = %q", got)
	}
}

// A file under deploy/ that is not a Kubernetes resource yields nothing, and that is
// not a defect — it is why classification stays filename-only and content decides here.
func TestNonKubernetesYAMLUnderDeployYieldsNothing(t *testing.T) {
	facts := ExtractKubernetes(infraFile("deploy/notes.yaml", "colors:\n  - red\n  - blue\n"))
	if len(facts.Services) != 0 || facts.Incomplete {
		t.Errorf("facts = %+v, want empty and not flagged", facts)
	}
}

func TestHelmChartExtraction(t *testing.T) {
	facts := ExtractHelmChart(infraFile("charts/api/Chart.yaml", `
apiVersion: v2
name: api
version: 0.3.1
appVersion: "1.4.0"
dependencies:
  - name: postgresql
    version: 15.5.x
    repository: https://charts.bitnami.com/bitnami
  - name: redis
    version: 19.x
    repository: https://charts.bitnami.com/bitnami
    condition: redis.enabled
`))
	facts.Normalize()
	if facts.Module.Name != "api" || facts.Module.Version != "0.3.1" {
		t.Errorf("module = %+v", facts.Module)
	}
	// appVersion is the version of the software the chart deploys, which is what a
	// reader actually wants.
	if facts.Module.LangVersion != "1.4.0" {
		t.Errorf("appVersion = %q", facts.Module.LangVersion)
	}
	if d := depOf(t, facts, "postgresql", ScopeRuntime); d.Source == "" || d.Optional {
		t.Errorf("postgresql = %+v", d)
	}
	// A conditional subchart is deployed only when a value enables it.
	if d := depOf(t, facts, "redis", ScopeRuntime); !d.Optional {
		t.Error("a conditional dependency is optional")
	}
}

func TestHelmValuesExtraction(t *testing.T) {
	facts := ExtractHelmValues(infraFile("charts/api/values.yaml", `
replicaCount: 2
image:
  repository: registry.example.com/api
  tag: "1.4.0"
ingress:
  enabled: true
  hosts:
    - host: api.example.com
  tls:
    - secretName: api-tls
postgresql:
  auth:
    existingSecret: db-creds
  image:
    repository: docker.io/library/postgres
    tag: "17"
sidecar:
  image: docker.io/library/busybox@sha256:abc123
`))
	facts.Normalize()

	refs := facts.ImageRefs()
	for _, want := range []string{
		"registry.example.com/api:1.4.0",
		"docker.io/library/postgres:17",
		"docker.io/library/busybox@sha256:abc123",
	} {
		if !contains(refs, want) {
			t.Errorf("images = %v, want %q", refs, want)
		}
	}
	secretRefOf(t, facts, "api-tls", "")
	// existingSecret is how a well-run chart avoids putting a credential in
	// values.yaml at all, so its presence is worth recording.
	if r := secretRefOf(t, facts, "db-creds", ""); r.Source != "helm-values" {
		t.Errorf("existingSecret = %+v", r)
	}
	surface := serviceOf(t, facts, "values")
	if !contains(surface.EnvKeys, "replicaCount") || !contains(surface.EnvKeys, "ingress") {
		t.Errorf("config surface = %v", surface.EnvKeys)
	}
	if joined(surface.Ports) != "ingress" {
		t.Errorf("an enabled ingress means this chart publishes externally: %v", surface.Ports)
	}
}

// A Helm template is not YAML — `{{- if }}` is a syntax error to any conforming
// parser — and a chart's templates are explicitly in scope, so one must degrade to a
// skeleton with a diagnostic rather than failing the file.
func TestHelmTemplateDegradesWithADiagnostic(t *testing.T) {
	facts := ExtractKubernetes(infraFile("charts/api/templates/deployment.yaml", `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "api.fullname" . }}
spec:
  replicas: {{ .Values.replicaCount }}
  template:
    spec:
      containers:
        - name: api
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          {{- if .Values.env }}
          env:
            - name: DATABASE_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: db-creds
                  key: password
          {{- end }}
`))
	facts.Normalize()
	if !facts.Incomplete {
		t.Error("a templated manifest is not a complete reading and must say so")
	}
	// The exact facts that survive templating are still extracted: a chart's secret
	// references are usually literal even when everything around them is not.
	secretRefOf(t, facts, "db-creds", "password")
}

func TestInfraExtractionIsDeterministic(t *testing.T) {
	f := infraFile("compose.yaml", `
services:
  b:
    image: img-b
    depends_on: [a, c]
    environment: { Z: "1", A: "2" }
  a:
    image: img-a
    ports: ["1:1", "2:2"]
  c:
    build: ./c
`)
	first := ""
	for i := 0; i < 10; i++ {
		facts := ExtractCompose(f)
		facts.Normalize()
		got := renderFacts(facts)
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("run %d differed", i)
		}
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func scriptNames(f Facts) []string {
	out := make([]string, 0, len(f.Scripts))
	for _, s := range f.Scripts {
		out = append(out, s.Name)
	}
	return out
}

// renderFacts flattens every field of a Facts into one comparable string. Used both
// for determinism checks and, in the secrets tests, to assert that no value reached
// any field — a check against the SecretRef fields alone would miss a leak through
// some other one.
func renderFacts(f Facts) string {
	var b strings.Builder
	b.WriteString(f.Path + "|" + string(f.Kind) + "|" + f.Note + "\n")
	b.WriteString(f.Module.Name + "@" + f.Module.Version + "/" + f.Module.LangVersion + "\n")
	b.WriteString(joined(f.Module.Workspaces) + "\n")
	for _, d := range f.Deps {
		b.WriteString("dep " + d.Name + "@" + d.Version + "/" + string(d.Scope) + "/" + d.Source + "\n")
	}
	for _, s := range f.Scripts {
		b.WriteString("script " + s.Name + "=" + s.Command + "\n")
	}
	for _, e := range f.Entrypoints {
		b.WriteString("entry " + e.Name + ">" + e.Path + "\n")
	}
	for _, s := range f.Services {
		b.WriteString("svc " + s.Name + "/" + s.Kind + "/" + s.Namespace + "/" + s.Image +
			"/" + s.Build + "/" + s.Replicas + " ports=" + joined(s.Ports) +
			" env=" + joined(s.EnvKeys) + " deps=" + joined(s.DependsOn) +
			" vols=" + joined(s.Volumes) + "\n")
	}
	for _, im := range f.Images {
		b.WriteString("img " + im.Ref + "/" + im.Stage + "\n")
	}
	for _, j := range f.Jobs {
		b.WriteString("job " + j.Name + "/" + j.Workflow + "/" + j.Runner + "/" + j.Uses +
			" needs=" + joined(j.Needs) + " perms=" + joined(j.Permissions) + "\n")
		for _, s := range j.Steps {
			b.WriteString("  step " + s.Name + "/" + s.Uses + "/" + s.Run + "\n")
		}
	}
	for _, c := range f.Contracts {
		b.WriteString("contract " + c.Kind + " " + c.Name + "/" + c.Package + "/" + c.Detail + "\n")
	}
	for _, m := range f.Migrations {
		b.WriteString("migration " + m.Version + "/" + m.Name + " tables=" + joined(m.Tables) + "\n")
	}
	for _, o := range f.Owners {
		b.WriteString("owner " + o.Pattern + "=" + joined(o.Owners) + "\n")
	}
	for _, r := range f.Rules {
		b.WriteString("rule " + r.Heading + "/" + r.Status + ": " + r.Text + "\n")
	}
	for _, s := range f.SecretRefs {
		b.WriteString("secret " + s.Name + "/" + s.Key + "/" + s.EnvVar + "/" + s.Source + "\n")
	}
	return b.String()
}
