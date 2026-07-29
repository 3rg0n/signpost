package manifest

import (
	"strings"
	"testing"
)

func tomlDoc(t *testing.T, src string) *Node {
	t.Helper()
	n, diag := ParseTOML(src)
	if diag.Incomplete() {
		t.Fatalf("unexpected diagnostics: %s", diag.Summary())
	}
	if n == nil {
		t.Fatal("no document parsed")
	}
	return n
}

func TestTOMLCargoManifest(t *testing.T) {
	n := tomlDoc(t, `
[package]
name = "signpost-core"
version = "0.1.0"
edition = "2021"

[dependencies]
serde = { version = "1.0", features = ["derive"] }
tokio = "1"

[dev-dependencies]
proptest = "1.5"

[[bin]]
name = "spctl"
path = "src/bin/spctl.rs"

[[bin]]
name = "spd"
path = "src/bin/spd.rs"
`)
	if got := n.Path("package", "name").String(); got != "signpost-core" {
		t.Errorf("package.name = %q", got)
	}
	if got := n.Path("package", "edition").String(); got != "2021" {
		t.Errorf("edition = %q", got)
	}
	// An inline table and a bare version string are both dependency declarations.
	if got := n.Path("dependencies", "serde", "version").String(); got != "1.0" {
		t.Errorf("serde version = %q", got)
	}
	if got := strings.Join(n.Path("dependencies", "serde", "features").Strings(), ","); got != "derive" {
		t.Errorf("serde features = %q", got)
	}
	if got := n.Path("dependencies", "tokio").String(); got != "1" {
		t.Errorf("tokio = %q", got)
	}
	if got := n.Path("dev-dependencies", "proptest").String(); got != "1.5" {
		t.Errorf("proptest = %q", got)
	}
	// An array of tables is how a crate declares several binaries, which is where
	// entrypoints come from.
	bins := n.Get("bin")
	if bins.Len() != 2 {
		t.Fatalf("bin entries = %d, want 2", bins.Len())
	}
	if got := bins.At(0).Get("name").String(); got != "spctl" {
		t.Errorf("bin 0 name = %q", got)
	}
	if got := bins.At(1).Get("path").String(); got != "src/bin/spd.rs" {
		t.Errorf("bin 1 path = %q", got)
	}
}

func TestTOMLDottedKeysAndNestedTables(t *testing.T) {
	n := tomlDoc(t, `
[tool.poetry]
name = "svc"

[tool.poetry.dependencies]
python = "^3.12"

[project]
requires-python = ">=3.12"
dynamic.version = true
`)
	if got := n.Path("tool", "poetry", "name").String(); got != "svc" {
		t.Errorf("tool.poetry.name = %q", got)
	}
	if got := n.Path("tool", "poetry", "dependencies", "python").String(); got != "^3.12" {
		t.Errorf("python = %q", got)
	}
	if got := n.Path("project", "requires-python").String(); got != ">=3.12" {
		t.Errorf("requires-python = %q", got)
	}
	if got := n.Path("project", "dynamic", "version").String(); got != "true" {
		t.Errorf("dotted key = %q", got)
	}
}

// Every real dependency array outgrows one line eventually.
func TestTOMLArraySpansLines(t *testing.T) {
	n := tomlDoc(t, `
[project]
dependencies = [
  "httpx>=0.27",
  "pydantic>=2.7",
  "uvicorn[standard]>=0.30",
]
name = "after"
`)
	deps := n.Path("project", "dependencies").Strings()
	if strings.Join(deps, "|") != "httpx>=0.27|pydantic>=2.7|uvicorn[standard]>=0.30" {
		t.Errorf("dependencies = %v", deps)
	}
	if got := n.Path("project", "name").String(); got != "after" {
		t.Errorf("the key after a wrapped array should still parse, got %q", got)
	}
}

func TestTOMLStringForms(t *testing.T) {
	n := tomlDoc(t, `
basic = "a\tb"
literal = 'c:\path\no-escape'
triple = """
line one
line two
"""
tripleLiteral = '''raw \n text'''
num = 42
float = 1.10
bool = true
`)
	cases := map[string]string{
		"basic":         "a\tb",
		"literal":       `c:\path\no-escape`,
		"triple":        "line one\nline two\n",
		"tripleLiteral": `raw \n text`,
		"num":           "42",
		// Kept as text: a manifest's 1.10 must not come back as 1.1.
		"float": "1.10",
		"bool":  "true",
	}
	for k, want := range cases {
		if got := n.Get(k).String(); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

// TOML's `#` starts a comment anywhere outside a string, but a `#` inside one is
// content — truncating a URL or a Windows path would be a wrong fact.
func TestTOMLCommentStripping(t *testing.T) {
	n := tomlDoc(t, `
# a leading comment
url = "https://host/#frag"   # trailing
hash = 'a # b'
plain = "x"    # y
`)
	cases := map[string]string{
		"url":   "https://host/#frag",
		"hash":  "a # b",
		"plain": "x",
	}
	for k, want := range cases {
		if got := n.Get(k).String(); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestTOMLQuotedKeys(t *testing.T) {
	n := tomlDoc(t, `
[tool.setuptools.package-data]
"my.pkg" = ["*.json"]

["odd.table"]
key = "v"
`)
	if got := strings.Join(n.Path("tool", "setuptools", "package-data", "my.pkg").Strings(), ","); got != "*.json" {
		t.Errorf("quoted key lookup = %q", got)
	}
	if got := n.Path("odd.table", "key").String(); got != "v" {
		t.Errorf("quoted table = %q", got)
	}
}

func TestTOMLNestedInlineTables(t *testing.T) {
	n := tomlDoc(t, `
[dependencies]
a = { version = "1", features = ["x", "y"], optional = true }
b = { git = "https://example.com/b", branch = "main" }
`)
	a := n.Path("dependencies", "a")
	if got := a.Get("version").String(); got != "1" {
		t.Errorf("a.version = %q", got)
	}
	if got := strings.Join(a.Get("features").Strings(), ","); got != "x,y" {
		t.Errorf("a.features = %q", got)
	}
	if v, ok := a.Get("optional").Bool(); !ok || !v {
		t.Error("a.optional should be true")
	}
	if got := n.Path("dependencies", "b", "git").String(); got != "https://example.com/b" {
		t.Errorf("b.git = %q", got)
	}
}

// A malformed line is recorded and skipped, so one bad manifest never fails a build.
func TestTOMLMalformedLineIsRecordedNotFatal(t *testing.T) {
	n, diag := ParseTOML(`
[package]
name = "ok"
this line has no equals sign
version = "1.0"
`)
	if got := n.Path("package", "name").String(); got != "ok" {
		t.Errorf("name = %q, want the readable keys preserved", got)
	}
	if got := n.Path("package", "version").String(); got != "1.0" {
		t.Errorf("version = %q, want parsing to continue past the bad line", got)
	}
	if !diag.Incomplete() {
		t.Error("a malformed line should be recorded")
	}
}

func TestTOMLEmptyInput(t *testing.T) {
	for _, src := range []string{"", "\n\n", "# only a comment\n"} {
		n, diag := ParseTOML(src)
		if n == nil || n.Len() != 0 {
			t.Errorf("%q should yield an empty mapping, got %+v", src, n)
		}
		if diag.Incomplete() {
			t.Errorf("%q: empty input is not a diagnostic: %s", src, diag.Summary())
		}
	}
}

func TestTOMLIsDeterministic(t *testing.T) {
	src := `
[package]
name = "x"
[dependencies]
a = { version = "1", features = ["p", "q"] }
b = "2"
[[bin]]
name = "one"
[[bin]]
name = "two"
`
	first := renderNode(tomlDoc(t, src))
	for i := 0; i < 10; i++ {
		if got := renderNode(tomlDoc(t, src)); got != first {
			t.Fatalf("run %d differed", i)
		}
	}
}
