package manifest

import (
	"strings"
	"testing"
)

// doc parses a single-document YAML string, failing the test on any diagnostic the
// case did not expect.
func doc(t *testing.T, src string) *Node {
	t.Helper()
	n, diag := ParseYAMLDoc(src)
	if diag.Incomplete() {
		t.Fatalf("unexpected diagnostics: %s", diag.Summary())
	}
	if n == nil {
		t.Fatal("no document parsed")
	}
	return n
}

func TestYAMLNestedMappingsAndSequences(t *testing.T) {
	n := doc(t, `
services:
  api:
    image: cgr.dev/chainguard/static:latest
    ports:
      - "8080:8080"
      - 9090:9090
    depends_on:
      - db
  db:
    image: docker.io/postgres:17
`)
	api := n.Path("services", "api")
	if got := api.Get("image").String(); got != "cgr.dev/chainguard/static:latest" {
		t.Errorf("api image = %q", got)
	}
	ports := api.Get("ports").Strings()
	if strings.Join(ports, ",") != "8080:8080,9090:9090" {
		t.Errorf("ports = %v", ports)
	}
	if got := n.Path("services", "db", "image").String(); got != "docker.io/postgres:17" {
		t.Errorf("db image = %q", got)
	}
	// Key order is the file's, not Go's.
	if got := strings.Join(n.Get("services").MapKeys(), ","); got != "api,db" {
		t.Errorf("service order = %q, want file order", got)
	}
}

// A sequence of mappings whose first key sits on the dash line is the dominant real
// form — every Kubernetes container list and every workflow step list.
func TestYAMLSequenceOfMappingsWithInlineFirstKey(t *testing.T) {
	n := doc(t, `
steps:
  - name: checkout
    uses: actions/checkout@v4
  - name: build
    run: go build ./...
    env:
      CGO_ENABLED: "0"
`)
	steps := n.Get("steps")
	if steps.Len() != 2 {
		t.Fatalf("steps = %d, want 2", steps.Len())
	}
	if got := steps.At(0).Get("uses").String(); got != "actions/checkout@v4" {
		t.Errorf("step 0 uses = %q", got)
	}
	if got := steps.At(1).Get("run").String(); got != "go build ./..." {
		t.Errorf("step 1 run = %q", got)
	}
	if got := steps.At(1).Path("env", "CGO_ENABLED").String(); got != "0" {
		t.Errorf("step 1 env = %q", got)
	}
}

func TestYAMLFlowCollections(t *testing.T) {
	n := doc(t, `
on: [push, pull_request]
matrix: {go: "1.26", os: ubuntu-latest}
empty: []
`)
	if got := strings.Join(n.Get("on").Strings(), ","); got != "push,pull_request" {
		t.Errorf("on = %q", got)
	}
	if got := n.Path("matrix", "go").String(); got != "1.26" {
		t.Errorf("matrix.go = %q", got)
	}
	if got := n.Path("matrix", "os").String(); got != "ubuntu-latest" {
		t.Errorf("matrix.os = %q", got)
	}
	if n.Get("empty").Len() != 0 {
		t.Error("empty flow sequence should have no items")
	}
}

// A colon inside a flow scalar is not a key separator. YAML 1.2 makes `:` an indicator
// only when a space or a flow indicator follows it, and the difference is load-bearing
// in every file this reader exists to read: an OAuth scope, a port mapping, a registry
// image with a tag, a duration. Treating each one as a separator produced a scalar that
// consumed nothing, so the enclosing loop spun forever rather than misreading one file.
func TestYAMLFlowColonInsideScalar(t *testing.T) {
	n := doc(t, `
scopes: [things:read, things:write]
ports: ["8080:8080", 9090:9090]
image: [registry.example.com/api:1.4.0]
scheme: {url: https://api.example.com/v1}
json: {"a":1,"b":"c:d"}
`)
	if got := strings.Join(n.Get("scopes").Strings(), ","); got != "things:read,things:write" {
		t.Errorf("scopes = %q", got)
	}
	if got := strings.Join(n.Get("ports").Strings(), ","); got != "8080:8080,9090:9090" {
		t.Errorf("ports = %q", got)
	}
	if got := strings.Join(n.Get("image").Strings(), ","); got != "registry.example.com/api:1.4.0" {
		t.Errorf("image = %q", got)
	}
	// A colon followed by `/` is inside the scalar; the one after `url` is not.
	if got := n.Path("scheme", "url").String(); got != "https://api.example.com/v1" {
		t.Errorf("url = %q", got)
	}
	// JSON-style flow: the closing quote ends the key, so no space is needed after the
	// colon. This is what lets a JSON document parse through the YAML path at all.
	if got := n.Path("json", "a").String(); got != "1" {
		t.Errorf("json.a = %q", got)
	}
	if got := n.Path("json", "b").String(); got != "c:d" {
		t.Errorf("json.b = %q", got)
	}
}

// A single-pair mapping inside a flow sequence needs no braces, and a workflow's
// `branches` filter is where it shows up.
func TestYAMLFlowSinglePairMapping(t *testing.T) {
	n := doc(t, "security: [bearerAuth: [read, write], apiKey: []]\n")
	items := n.Get("security").Seq()
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if got := strings.Join(items[0].Get("bearerAuth").Strings(), ","); got != "read,write" {
		t.Errorf("bearerAuth = %q", got)
	}
	if items[1].Get("apiKey") == nil {
		t.Errorf("apiKey pair = %+v", items[1])
	}
}

// A formatter wrapping a long flow sequence across lines is ordinary; reading only
// the first line would report half the triggers.
func TestYAMLFlowSpansLines(t *testing.T) {
	n := doc(t, `
on: [
  push,
  pull_request,
  workflow_dispatch
]
name: ci
`)
	if got := strings.Join(n.Get("on").Strings(), ","); got != "push,pull_request,workflow_dispatch" {
		t.Errorf("on = %q", got)
	}
	if got := n.Get("name").String(); got != "ci" {
		t.Errorf("the key after a wrapped flow should still parse, got %q", got)
	}
}

// `run: |` is where a workflow says what it actually executes, so the literal block
// has to come through verbatim.
func TestYAMLLiteralBlockScalar(t *testing.T) {
	n := doc(t, `
steps:
  - run: |
      set -euo pipefail
      go test ./...
      # a shell comment, not a YAML one
    shell: bash
`)
	got := n.Get("steps").At(0).Get("run").String()
	want := "set -euo pipefail\ngo test ./...\n# a shell comment, not a YAML one\n"
	if got != want {
		t.Errorf("run block = %q, want %q", got, want)
	}
	if sh := n.Get("steps").At(0).Get("shell").String(); sh != "bash" {
		t.Errorf("the key after a block scalar should still parse, got %q", sh)
	}
}

func TestYAMLBlockScalarChomping(t *testing.T) {
	n := doc(t, `
clip: |
  a
strip: |-
  a
keep: |+
  a

folded: >
  one
  two

  three
`)
	cases := map[string]string{
		"clip":   "a\n",
		"strip":  "a",
		"keep":   "a\n",
		"folded": "one two\nthree\n",
	}
	for key, want := range cases {
		if got := n.Get(key).String(); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestYAMLQuotingAndEscapes(t *testing.T) {
	n := doc(t, `
plain: 3.10
quoted: "3.10"
single: 'it''s here'
escaped: "a\tb\"c"
nulled: null
tilde: ~
`)
	// Quotedness is retained because "3.10" and 3.10 mean different things to the
	// tool the file is for.
	if p := n.Get("plain"); p.String() != "3.10" || p.Quoted {
		t.Errorf("plain = %q quoted=%v, want unquoted", p.String(), p.Quoted)
	}
	if q := n.Get("quoted"); q.String() != "3.10" || !q.Quoted {
		t.Errorf("quoted = %q quoted=%v, want quoted", q.String(), q.Quoted)
	}
	if got := n.Get("single").String(); got != "it's here" {
		t.Errorf("single = %q", got)
	}
	if got := n.Get("escaped").String(); got != "a\tb\"c" {
		t.Errorf("escaped = %q", got)
	}
	for _, k := range []string{"nulled", "tilde"} {
		if got := n.Get(k).String(); got != "" {
			t.Errorf("%s = %q, want empty", k, got)
		}
	}
}

// A `#` only starts a comment at the start or after whitespace. Otherwise an image
// digest would be truncated — a wrong fact, not a missing one.
func TestYAMLCommentStripping(t *testing.T) {
	n := doc(t, `
# leading comment
image: nginx@sha256:abc123   # trailing comment
url: "https://host/#frag"
note: 'a # b'
`)
	cases := map[string]string{
		"image": "nginx@sha256:abc123",
		"url":   "https://host/#frag",
		"note":  "a # b",
	}
	for k, want := range cases {
		if got := n.Get(k).String(); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// A colon must be followed by whitespace to separate a key. Without that rule every
// URL in a values file becomes a mapping the author never wrote.
func TestYAMLURLIsNotAMapping(t *testing.T) {
	n := doc(t, `
endpoint: https://api.example.com:8443/v1
image: docker.io/library/redis:7
`)
	if got := n.Get("endpoint").String(); got != "https://api.example.com:8443/v1" {
		t.Errorf("endpoint = %q", got)
	}
	if got := n.Get("image").String(); got != "docker.io/library/redis:7" {
		t.Errorf("image = %q", got)
	}
	if n.Get("https") != nil {
		t.Error("a URL's scheme must not become a mapping key")
	}
}

// Compose files use anchors heavily; a reader that ignored them would report a
// service as having no environment at all.
func TestYAMLAnchorsAliasesAndMergeKeys(t *testing.T) {
	n := doc(t, `
x-common: &common
  restart: always
  environment:
    LOG_LEVEL: info
services:
  api:
    <<: *common
    image: api:latest
  worker:
    <<: *common
    restart: on-failure
`)
	api := n.Path("services", "api")
	if got := api.Get("restart").String(); got != "always" {
		t.Errorf("api restart = %q, want the merged value", got)
	}
	if got := api.Path("environment", "LOG_LEVEL").String(); got != "info" {
		t.Errorf("api env = %q, want the merged value", got)
	}
	if got := api.Get("image").String(); got != "api:latest" {
		t.Errorf("api image = %q", got)
	}
	// An explicit key beats an inherited one, which is the spec's precedence rule.
	if got := n.Path("services", "worker", "restart").String(); got != "on-failure" {
		t.Errorf("worker restart = %q, want the explicit value to win", got)
	}
}

// An alias to an anchor that was never defined is recorded, not silently empty:
// otherwise a truncated file looks like a complete reading.
func TestYAMLUnresolvedAliasIsRecorded(t *testing.T) {
	_, diag := ParseYAMLDoc("key: *missing\n")
	if !diag.Incomplete() {
		t.Fatal("an unresolved alias should be recorded")
	}
	if !strings.Contains(diag.Summary(), "missing") {
		t.Errorf("diagnostic should name the anchor: %s", diag.Summary())
	}
}

func TestYAMLMultipleDocuments(t *testing.T) {
	docs, diag := ParseYAML(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
---
apiVersion: v1
kind: Service
metadata:
  name: api
---
`)
	if diag.Incomplete() {
		t.Fatalf("unexpected diagnostics: %s", diag.Summary())
	}
	if len(docs) != 2 {
		t.Fatalf("got %d documents, want 2", len(docs))
	}
	if got := docs[0].Get("kind").String(); got != "Deployment" {
		t.Errorf("doc 0 kind = %q", got)
	}
	if got := docs[1].Get("kind").String(); got != "Service" {
		t.Errorf("doc 1 kind = %q", got)
	}
}

// The reason this reader is hand-written: a Helm template is not YAML, and a
// conforming parser fails on it entirely. The unconditional skeleton must still
// come through, and the fact that it is a skeleton must be recorded.
func TestYAMLHelmTemplateYieldsSkeletonAndDiagnostic(t *testing.T) {
	n, diag := ParseYAMLDoc(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "chart.fullname" . }}
spec:
  replicas: {{ .Values.replicaCount }}
  template:
    spec:
      containers:
        - name: app
          image: "nginx:1.27"
{{- if .Values.ingress.enabled }}
          ports:
            - containerPort: 80
{{- end }}
`)
	if n == nil {
		t.Fatal("a Helm template must still yield its skeleton")
	}
	if got := n.Get("kind").String(); got != "Deployment" {
		t.Errorf("kind = %q", got)
	}
	c := n.Path("spec", "template", "spec", "containers").At(0)
	if got := c.Get("image").String(); got != "nginx:1.27" {
		t.Errorf("container image = %q", got)
	}
	// A templated value keeps its text: "the image comes from a value" is the fact.
	if got := n.Path("metadata", "name").String(); !strings.Contains(got, "chart.fullname") {
		t.Errorf("templated name = %q, want the directive text kept", got)
	}
	if !diag.Incomplete() {
		t.Error("template directives must be recorded, not silently skipped")
	}
	if !strings.Contains(diag.Summary(), "skeleton") {
		t.Errorf("diagnostic should explain what was read: %s", diag.Summary())
	}
}

func TestYAMLEmptyAndCommentOnlyInput(t *testing.T) {
	for _, src := range []string{"", "\n\n", "# just a comment\n", "---\n---\n"} {
		n, diag := ParseYAMLDoc(src)
		if n != nil {
			t.Errorf("%q should yield no document, got %+v", src, n)
		}
		if diag.Incomplete() {
			t.Errorf("%q: an empty document is not a diagnostic: %s", src, diag.Summary())
		}
	}
}

// A key with no value is a real thing in a workflow (`on: workflow_dispatch:`) and
// must not swallow the following key.
func TestYAMLEmptyValueDoesNotSwallowSiblings(t *testing.T) {
	n := doc(t, `
on:
  workflow_dispatch:
  push:
    branches: [main]
name: ci
`)
	on := n.Get("on")
	if got := strings.Join(on.MapKeys(), ","); got != "workflow_dispatch,push" {
		t.Errorf("on keys = %q", got)
	}
	if got := strings.Join(on.Path("push", "branches").Strings(), ","); got != "main" {
		t.Errorf("push branches = %q", got)
	}
	if got := n.Get("name").String(); got != "ci" {
		t.Errorf("name = %q, want the sibling key preserved", got)
	}
}

// YAML 1.1 booleans, because that is what CI and Kubernetes tooling accepts: a
// reader that only understood "true" would report a disabled job as enabled.
func TestNodeBoolAcceptsYAML11Spellings(t *testing.T) {
	n := doc(t, "a: yes\nb: on\nc: true\nd: no\ne: off\nf: false\ng: maybe\n")
	for _, k := range []string{"a", "b", "c"} {
		if v, ok := n.Get(k).Bool(); !ok || !v {
			t.Errorf("%s should be true", k)
		}
	}
	for _, k := range []string{"d", "e", "f"} {
		if v, ok := n.Get(k).Bool(); !ok || v {
			t.Errorf("%s should be false", k)
		}
	}
	if _, ok := n.Get("g").Bool(); ok {
		t.Error("a non-boolean scalar should report not-a-bool")
	}
}

// A lone scalar counts as a one-element sequence, because compose and workflows both
// accept either spelling and both appear in real repositories.
func TestNodeSeqTreatsScalarAsOneElement(t *testing.T) {
	n := doc(t, "on: push\ncommand: [\"./run\", \"--flag\"]\nnothing:\n")
	if got := strings.Join(n.Get("on").Strings(), ","); got != "push" {
		t.Errorf("scalar as sequence = %q", got)
	}
	if got := strings.Join(n.Get("command").Strings(), ","); got != "./run,--flag" {
		t.Errorf("command = %q", got)
	}
	if got := n.Get("nothing").Strings(); len(got) != 0 {
		t.Errorf("an empty value should yield no elements, got %v", got)
	}
}

// Both spellings of an environment block mean the same thing and both are idiomatic,
// so normalising here keeps "which spelling did this author use" out of every caller.
func TestNodeKeyValuesAcceptsAllThreeSpellings(t *testing.T) {
	n := doc(t, `
mapping:
  A: "1"
  B: "2"
list:
  - A=1
  - B=2
  - INHERITED
objects:
  - name: A
    value: "1"
  - name: B
    value: "2"
`)
	for _, key := range []string{"mapping", "list", "objects"} {
		kvs := n.Get(key).KeyValues()
		if len(kvs) < 2 {
			t.Fatalf("%s: got %d entries", key, len(kvs))
		}
		if kvs[0].Key != "A" || kvs[0].Value != "1" {
			t.Errorf("%s[0] = %+v, want A=1", key, kvs[0])
		}
		if kvs[1].Key != "B" || kvs[1].Value != "2" {
			t.Errorf("%s[1] = %+v, want B=2", key, kvs[1])
		}
	}
	// A bare name in a list means "inherit from the host", a distinction worth
	// keeping rather than dropping as an empty value.
	list := n.Get("list").KeyValues()
	if len(list) != 3 || list[2].Key != "INHERITED" || list[2].Value != "" {
		t.Errorf("bare entry = %+v, want INHERITED with no value", list[2:])
	}
}

// Nil-safety across the whole lookup API: every extractor reads optional keys out of
// documents written by other people, and a chain that panicked on a missing key would
// turn one unusual manifest into a failed build.
func TestNodeLookupsAreNilSafe(t *testing.T) {
	var n *Node
	if got := n.Path("a", "b").Get("c").At(0).String(); got != "" {
		t.Errorf("nil chain = %q, want empty", got)
	}
	if n.Len() != 0 || !n.IsZero() || n.MapKeys() != nil || n.Strings() != nil {
		t.Error("nil node accessors should return zero values")
	}
	if _, ok := n.Int(); ok {
		t.Error("nil Int should report not-an-int")
	}
	real := doc(t, "a: 1\n")
	if got := real.Path("a", "b", "c").String(); got != "" {
		t.Errorf("descending into a scalar = %q, want empty", got)
	}
	if real.Get("missing").At(3) != nil {
		t.Error("indexing a missing node should be nil")
	}
}

// A duplicate key resolves last-wins, matching every real parser: an extractor that
// saw both would disagree with the tool the file is for.
func TestYAMLDuplicateKeyIsLastWins(t *testing.T) {
	n := doc(t, "image: first\nimage: second\n")
	if got := n.Get("image").String(); got != "second" {
		t.Errorf("image = %q, want the last value", got)
	}
	if n.Len() != 1 {
		t.Errorf("duplicate keys should collapse to one entry, got %d", n.Len())
	}
}

// Determinism is a correctness property here: CI commits the bundle, so an unstable
// read becomes commit churn (design §8.1).
func TestYAMLIsDeterministic(t *testing.T) {
	src := `
services:
  api: {image: a, ports: ["1:1"]}
  db: {image: b}
x: &a
  k: v
y:
  <<: *a
`
	first := renderNode(doc(t, src))
	for i := 0; i < 10; i++ {
		if got := renderNode(doc(t, src)); got != first {
			t.Fatalf("run %d differed:\n%s\nvs\n%s", i, got, first)
		}
	}
}

// renderNode flattens a tree to a comparable string.
func renderNode(n *Node) string {
	var b strings.Builder
	var walk func(*Node, string)
	walk = func(n *Node, prefix string) {
		if n == nil {
			return
		}
		switch n.Kind {
		case KindScalar:
			b.WriteString(prefix + "=" + n.Str + "\n")
		case KindMap:
			n.Each(func(k string, v *Node) bool {
				walk(v, prefix+"."+k)
				return true
			})
		case KindSeq:
			for i, it := range n.Items {
				walk(it, prefix+"["+string(rune('0'+i))+"]")
			}
		}
	}
	walk(n, "")
	return b.String()
}
