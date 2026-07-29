package manifest

import (
	"strings"
	"testing"
)

func jsonDoc(t *testing.T, src string) *Node {
	t.Helper()
	n, err := ParseJSON(src)
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	return n
}

func TestJSONPackageManifest(t *testing.T) {
	n := jsonDoc(t, `{
  "name": "@scope/viewer",
  "version": "0.1.0",
  "type": "module",
  "bin": { "viewer": "./dist/cli.js" },
  "scripts": {
    "build": "vite build",
    "test": "vitest run",
    "lint": "eslint ."
  },
  "dependencies": { "cytoscape": "^3.30.0" },
  "devDependencies": { "vite": "^5.4.0", "typescript": "~5.6.2" },
  "workspaces": ["apps/*", "packages/*"]
}`)
	if got := n.Get("name").String(); got != "@scope/viewer" {
		t.Errorf("name = %q", got)
	}
	if got := n.Path("bin", "viewer").String(); got != "./dist/cli.js" {
		t.Errorf("bin.viewer = %q", got)
	}
	// Key order is the file's, which is a fact worth keeping: a scripts block reads
	// as a list a human wrote in a deliberate order.
	if got := strings.Join(n.Get("scripts").MapKeys(), ","); got != "build,test,lint" {
		t.Errorf("script order = %q, want file order", got)
	}
	if got := n.Path("devDependencies", "typescript").String(); got != "~5.6.2" {
		t.Errorf("typescript = %q", got)
	}
	if got := strings.Join(n.Get("workspaces").Strings(), ","); got != "apps/*,packages/*" {
		t.Errorf("workspaces = %q", got)
	}
}

// Numbers keep their original text: a version pin of 1.10 must not come back as 1.1,
// and a large integer must not acquire an exponent.
func TestJSONNumbersKeepTheirText(t *testing.T) {
	n := jsonDoc(t, `{"a": 1.10, "b": 10000000000000000000, "c": 1e3, "d": 0}`)
	cases := map[string]string{"a": "1.10", "b": "10000000000000000000", "c": "1e3", "d": "0"}
	for k, want := range cases {
		if got := n.Get(k).String(); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestJSONScalarsAndNesting(t *testing.T) {
	n := jsonDoc(t, `{"t": true, "f": false, "z": null, "s": "x",
	  "deep": {"a": [{"b": ["c"]}]}}`)
	if v, ok := n.Get("t").Bool(); !ok || !v {
		t.Error("t should be true")
	}
	if v, ok := n.Get("f").Bool(); !ok || v {
		t.Error("f should be false")
	}
	if got := n.Get("z").String(); got != "" {
		t.Errorf("null = %q, want empty", got)
	}
	if got := n.Path("deep", "a").At(0).Get("b").At(0).String(); got != "c" {
		t.Errorf("nested lookup = %q", got)
	}
}

// A malformed package.json is genuinely malformed — there is no JSON equivalent of a
// Helm template — so the honest report is that it could not be read.
func TestJSONMalformedIsAnError(t *testing.T) {
	cases := map[string]string{
		"unclosed object": `{"a": 1`,
		"trailing comma":  `{"a": 1,}`,
		"merge conflict":  "<<<<<<< HEAD\n{\"a\": 1}\n",
		"two documents":   `{"a": 1} {"b": 2}`,
		"empty":           ``,
	}
	for name, src := range cases {
		if _, err := ParseJSON(src); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestJSONTopLevelArray(t *testing.T) {
	n := jsonDoc(t, `["a", "b"]`)
	if got := strings.Join(n.Strings(), ","); got != "a,b" {
		t.Errorf("top-level array = %q", got)
	}
}

func TestJSONIsDeterministic(t *testing.T) {
	src := `{"z": 1, "a": {"y": 2, "b": [3, {"c": 4}]}}`
	first := renderNode(jsonDoc(t, src))
	for i := 0; i < 10; i++ {
		if got := renderNode(jsonDoc(t, src)); got != first {
			t.Fatalf("run %d differed", i)
		}
	}
}
