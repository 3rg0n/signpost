package extract

import (
	"strings"
	"testing"
)

// codeOf returns the scanned code text, one entry per line.
func codeOf(src string, cfg scanConfig) []string {
	var out []string
	for _, cl := range scanLines(src, cfg) {
		out = append(out, strings.TrimRight(cl.Text, " "))
	}
	return out
}

func TestScanStripsLineComments(t *testing.T) {
	got := codeOf("import x // import y\n# not a comment in js\n", scanJSLike)
	if strings.Contains(got[0], "import y") {
		t.Errorf("line comment not stripped: %q", got[0])
	}
	if !strings.Contains(got[0], "import x") {
		t.Errorf("code before a comment must survive: %q", got[0])
	}
	// '#' is not a comment in JS, so the line stays.
	if !strings.Contains(got[1], "not a comment") {
		t.Errorf("# should not start a comment in JS: %q", got[1])
	}
}

// The case that motivates the whole scanner: text inside a comment must not be
// mistaken for a declaration.
func TestScanBlockCommentSpanningLines(t *testing.T) {
	got := codeOf(`before
/* import "fake"
   export function ghost() {}
*/ after
real`, scanJSLike)

	if strings.Contains(got[1], "fake") {
		t.Errorf("import inside a block comment leaked: %q", got[1])
	}
	if strings.Contains(got[2], "ghost") {
		t.Errorf("declaration inside a block comment leaked: %q", got[2])
	}
	if !strings.Contains(got[3], "after") {
		t.Errorf("code after a block comment close must survive: %q", got[3])
	}
	if !strings.Contains(got[4], "real") {
		t.Errorf("code after the comment must survive: %q", got[4])
	}
}

func TestScanBlockCommentOpensAndClosesInline(t *testing.T) {
	got := codeOf(`const a = 1 /* c */ + 2`, scanJSLike)
	if strings.Contains(got[0], "c */") {
		t.Errorf("inline block comment not stripped: %q", got[0])
	}
	if !strings.Contains(got[0], "const a = 1") || !strings.Contains(got[0], "+ 2") {
		t.Errorf("code on both sides must survive: %q", got[0])
	}
}

// Rust block comments nest; C-family ones do not. Getting nesting wrong means the
// scanner resumes reading comment text as code.
func TestScanNestedBlockComments(t *testing.T) {
	rust := codeOf(`/* outer /* inner */ still comment */
fn real() {}`, scanRust)
	if strings.TrimSpace(rust[0]) != "" {
		t.Errorf("nested comment should consume the whole line: %q", rust[0])
	}
	if !strings.Contains(rust[1], "fn real") {
		t.Errorf("code after a nested comment must survive: %q", rust[1])
	}

	// The same text in JS: the first */ closes, so the tail is code.
	js := codeOf(`/* outer /* inner */ tail`, scanJSLike)
	if !strings.Contains(js[0], "tail") {
		t.Errorf("JS comments do not nest, so tail is code: %q", js[0])
	}
}

// A template literal can span lines and can contain anything, including text
// that looks exactly like an import.
func TestScanTemplateLiteralSpansLines(t *testing.T) {
	got := codeOf("const q = `\nimport \"fake\"\nexport const ghost = 1\n`\nreal", scanJSLike)
	if strings.Contains(got[1], "fake") {
		t.Errorf("import inside a template literal leaked: %q", got[1])
	}
	if strings.Contains(got[2], "ghost") {
		t.Errorf("declaration inside a template literal leaked: %q", got[2])
	}
	if !strings.Contains(got[4], "real") {
		t.Errorf("code after the literal must survive: %q", got[4])
	}
}

func TestScanBlanksStringBodies(t *testing.T) {
	got := codeOf(`const s = "import fake from 'x'"`, scanJSLike)
	if strings.Contains(got[0], "import") {
		t.Errorf("string body must be blanked: %q", got[0])
	}
	// The quotes stay, so a caller can still see a literal was present.
	if strings.Count(got[0], `"`) != 2 {
		t.Errorf("quotes should be preserved: %q", got[0])
	}
}

func TestScanEscapedQuoteDoesNotEndString(t *testing.T) {
	got := codeOf(`a = "he said \"import x\"" ; b = 1`, scanJSLike)
	if strings.Contains(got[0], "import") {
		t.Errorf("an escaped quote must not end the string early: %q", got[0])
	}
	if !strings.Contains(got[0], "b = 1") {
		t.Errorf("code after the string must survive: %q", got[0])
	}
}

// A backslash that is itself escaped does end the string: "a\\" is complete.
func TestScanEscapedBackslashEndsString(t *testing.T) {
	got := codeOf(`a = "path\\" ; import x`, scanJSLike)
	if !strings.Contains(got[0], "import x") {
		t.Errorf(`"a\\" is a terminated string, so the tail is code: %q`, got[0])
	}
}

// A comment marker inside a string is not a comment.
func TestScanCommentMarkerInsideStringIsNotAComment(t *testing.T) {
	got := codeOf(`const url = "http://example.com" ; real = 1`, scanJSLike)
	if !strings.Contains(got[0], "real = 1") {
		t.Errorf("// inside a string must not start a comment: %q", got[0])
	}
}

// And the reverse: a quote inside a comment must not open a string that then
// swallows subsequent lines.
func TestScanQuoteInsideCommentDoesNotOpenString(t *testing.T) {
	got := codeOf("// it's fine\nimport x", scanJSLike)
	if !strings.Contains(got[1], "import x") {
		t.Errorf("an apostrophe in a comment must not open a string: %q", got[1])
	}
}

func TestScanPythonDocstring(t *testing.T) {
	got := codeOf(`"""Module docstring.

import fake
def ghost(): pass
"""
import real`, scanPython)

	if strings.Contains(got[2], "fake") {
		t.Errorf("import inside a docstring leaked: %q", got[2])
	}
	if strings.Contains(got[3], "ghost") {
		t.Errorf("def inside a docstring leaked: %q", got[3])
	}
	if !strings.Contains(got[5], "import real") {
		t.Errorf("code after the docstring must survive: %q", got[5])
	}
}

func TestScanPythonSingleLineTripleQuote(t *testing.T) {
	got := codeOf(`x = """inline""" ; import real`, scanPython)
	if strings.Contains(got[0], "inline") {
		t.Errorf("triple-quoted body must be blanked: %q", got[0])
	}
	if !strings.Contains(got[0], "import real") {
		t.Errorf("a triple quote opened and closed on one line must not span: %q", got[0])
	}
}

func TestScanPythonBothTripleQuoteStyles(t *testing.T) {
	got := codeOf("'''\nimport fake\n'''\nimport real", scanPython)
	if strings.Contains(got[1], "fake") {
		t.Errorf("''' docstrings must be handled too: %q", got[1])
	}
	if !strings.Contains(got[3], "import real") {
		t.Errorf("code after ''' must survive: %q", got[3])
	}
}

// A docstring delimiter nested inside the other style is just text.
func TestScanPythonMixedQuotesInsideDocstring(t *testing.T) {
	got := codeOf(`"""It has ''' inside."""
import real`, scanPython)
	if !strings.Contains(got[1], "import real") {
		t.Errorf("mismatched inner quotes must not confuse the terminator: %q", got[1])
	}
}

// The most important Rust case: a lifetime is not a string. Reading 'a as an
// unterminated string would blank the rest of the line and hide the declaration.
func TestScanRustLifetimeIsNotAString(t *testing.T) {
	cases := []string{
		`pub fn parse<'a>(s: &'a str) -> &'a str { s }`,
		`pub struct Wrapper<'a> { inner: &'a [u8] }`,
		`impl<'a> Trait for Wrapper<'a> {}`,
		`pub fn boxed() -> Box<dyn Error + 'static> { todo!() }`,
	}
	for _, src := range cases {
		got := codeOf(src, scanRust)[0]
		if !strings.Contains(got, "fn ") && !strings.Contains(got, "struct ") && !strings.Contains(got, "impl") {
			t.Errorf("lifetime swallowed the declaration in %q: got %q", src, got)
		}
	}
}

func TestScanRustCharLiteral(t *testing.T) {
	got := codeOf(`let c = 'x'; let n = '\n'; let u = 'é'; pub fn after() {}`, scanRust)[0]
	if !strings.Contains(got, "pub fn after") {
		t.Errorf("char literals must not swallow the line: %q", got)
	}
	// A char literal's body is blanked like any other string.
	if strings.Contains(got, "'x'") {
		t.Errorf("char literal body should be blanked: %q", got)
	}
}

func TestScanRustRawStrings(t *testing.T) {
	got := codeOf(`let re = r"use fake::thing"; pub fn real() {}`, scanRust)[0]
	if strings.Contains(got, "fake") {
		t.Errorf("raw string body must be blanked: %q", got)
	}
	if !strings.Contains(got, "pub fn real") {
		t.Errorf("code after a raw string must survive: %q", got)
	}

	// r#"..."# lets a quote appear inside, so the terminator is "# not ".
	hashed := codeOf(`let j = r#"{"use": "fake::x"}"#; pub fn real() {}`, scanRust)[0]
	if strings.Contains(hashed, "fake") {
		t.Errorf("hashed raw string body must be blanked: %q", hashed)
	}
	if !strings.Contains(hashed, "pub fn real") {
		t.Errorf("code after a hashed raw string must survive: %q", hashed)
	}
}

// A Rust raw string may run for many lines, and the crate-embedded-sample-code
// idiom is exactly where a phantom declaration comes from. The terminator is
// matched literally, because a backslash inside a raw string escapes nothing.
func TestScanRustRawStringSpansLines(t *testing.T) {
	got := codeOf("const RAW: &str = r#\"\nuse fake::thing;\npub fn ghost() {}\n\"#;\npub fn real() {}", scanRust)
	for i := 1; i <= 2; i++ {
		if strings.TrimSpace(got[i]) != "" {
			t.Errorf("line %d is inside a raw string and must be blank: %q", i+1, got[i])
		}
	}
	if !strings.Contains(got[4], "pub fn real") {
		t.Errorf("code after the raw string must survive: %q", got[4])
	}
}

// Rust's ordinary "..." also spans lines, unlike Python's and JavaScript's. A
// `const S: &str = "` holding sample code is the same trap as the raw form.
func TestScanRustPlainStringSpansLines(t *testing.T) {
	got := codeOf("const S: &str = \"\nuse fake::thing;\npub fn ghost() {}\n\";\npub fn real() {}", scanRust)
	for i := 1; i <= 2; i++ {
		if strings.TrimSpace(got[i]) != "" {
			t.Errorf("line %d is inside a string and must be blank: %q", i+1, got[i])
		}
	}
	if !strings.Contains(got[4], "pub fn real") {
		t.Errorf("code after the string must survive: %q", got[4])
	}
}

// An escaped quote does not close a multi-line Rust string, where a raw one has
// no escapes at all — the two cases must not share a matcher.
func TestScanRustMultiLineStringEscapes(t *testing.T) {
	got := codeOf("let s = \"\nnot \\\" the end\npub fn ghost() {}\n\";\npub fn real() {}", scanRust)
	if strings.Contains(got[2], "ghost") {
		t.Errorf("an escaped quote must not close the string: %q", got[2])
	}
	if !strings.Contains(got[4], "pub fn real") {
		t.Errorf("code after the string must survive: %q", got[4])
	}

	// In a raw string the backslash is literal, so the very next quote closes it.
	raw := codeOf("let s = r\"\nnot \\\"; pub fn real() {}", scanRust)
	if !strings.Contains(raw[1], "pub fn real") {
		t.Errorf("a backslash does not escape a raw string's terminator: %q", raw[1])
	}
}

// An identifier ending in r must not be read as a raw-string prefix.
func TestScanRustIdentifierEndingInR(t *testing.T) {
	got := codeOf(`for x in iter { let s = "y"; }`, scanRust)[0]
	if !strings.Contains(got, "for x in iter") {
		t.Errorf("identifier ending in r misread as a raw string: %q", got)
	}
}

func TestIndentWidth(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"x", 0},
		{"    x", 4},
		{"\tx", 8},
		{" \tx", 8},   // tab advances to the next multiple of 8
		{"\t\tx", 16}, //
		{"        ", 8},
	}
	for _, c := range cases {
		if got := indentWidth(c.in); got != c.want {
			t.Errorf("indentWidth(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestJoinParensMergesWrappedList(t *testing.T) {
	lines := scanLines("from typing import (\n    Any,\n    Dict,\n)\nx = 1", scanPython)
	joined, last := joinParens(lines, 0)
	if last != 3 {
		t.Errorf("last consumed line = %d, want 3", last)
	}
	for _, want := range []string{"Any", "Dict"} {
		if !strings.Contains(joined.Text, want) {
			t.Errorf("joined line missing %q: %q", want, joined.Text)
		}
	}
	if joined.Num != 1 {
		t.Errorf("joined line should keep the first line's number, got %d", joined.Num)
	}
}

func TestJoinParensBackslashContinuation(t *testing.T) {
	lines := scanLines("import os, \\\n    sys\nx = 1", scanPython)
	joined, last := joinParens(lines, 0)
	if last != 1 {
		t.Errorf("last = %d, want 1", last)
	}
	if !strings.Contains(joined.Text, "sys") {
		t.Errorf("backslash continuation not joined: %q", joined.Text)
	}
	if strings.Contains(joined.Text, "\\") {
		t.Errorf("the continuation marker should be removed: %q", joined.Text)
	}
}

// The bug this guards against: counting braces would fold a whole function body
// into one line, and every nested declaration would then look top-level.
func TestJoinParensIgnoresBraces(t *testing.T) {
	lines := scanLines("def outer():\n    def inner(): pass\nx = 1", scanPython)
	joined, last := joinParens(lines, 0)
	if last != 0 {
		t.Errorf("a balanced def line must not join forward, consumed through %d", last)
	}
	if strings.Contains(joined.Text, "inner") {
		t.Errorf("the body must not be folded into the def line: %q", joined.Text)
	}
}

func TestJoinBracesMergesWrappedNamedImports(t *testing.T) {
	lines := scanLines("import {\n  a,\n  b,\n} from \"mod\";\nconst x = 1;", scanJSLike)
	joined, last := joinBraces(lines, 0)
	if last != 3 {
		t.Errorf("last = %d, want 3", last)
	}
	if !strings.Contains(joined.Text, "a,") || !strings.Contains(joined.Text, "from") {
		t.Errorf("wrapped named imports not joined: %q", joined.Text)
	}
}

// An unclosed bracket must not consume the rest of the file.
func TestJoinParensCapsRunaway(t *testing.T) {
	src := "f(\n" + strings.Repeat("x,\n", 500)
	lines := scanLines(src, scanPython)
	_, last := joinParens(lines, 0)
	if last >= len(lines)-1 {
		t.Errorf("an unclosed bracket consumed the whole file (last=%d of %d)", last, len(lines))
	}
}

// Raw must stay aligned with Text so a module path can be recovered from the
// original bytes once the scanner confirms the line is code.
func TestScanPreservesRawAndOffsets(t *testing.T) {
	src := `import fake from "./real/path"`
	lines := scanLines(src, scanJSLike)
	cl := lines[0]
	if cl.Raw != src {
		t.Errorf("Raw altered: %q", cl.Raw)
	}
	if len(cl.Text) != len(cl.Raw) {
		t.Errorf("Text (%d bytes) and Raw (%d bytes) must be the same length for offsets to line up",
			len(cl.Text), len(cl.Raw))
	}
	idx := strings.Index(cl.Text, `"`)
	got, ok := stringAt(cl.Raw, idx)
	if !ok || got != "./real/path" {
		t.Errorf("stringAt at the scanned offset = %q, %v; want ./real/path", got, ok)
	}
}

func TestStringAt(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{`from "a/b"`, "a/b", true},
		{`from 'a/b'`, "a/b", true},
		{"from `a/b`", "a/b", true},
		{`from "with\"quote"`, `with"quote`, true},
		{`no string here`, "", false},
		{`unterminated "abc`, "", false},
	}
	for _, c := range cases {
		got, ok := stringAt(c.raw, 0)
		if got != c.want || ok != c.ok {
			t.Errorf("stringAt(%q) = %q, %v; want %q, %v", c.raw, got, ok, c.want, c.ok)
		}
	}
}

func TestScanEmptyAndBlankInput(t *testing.T) {
	if got := scanLines("", scanJSLike); len(got) != 1 {
		t.Errorf("empty input should yield one empty line, got %d", len(got))
	}
	if got := scanLines("\n\n\n", scanPython); len(got) != 4 {
		t.Errorf("blank lines should be preserved for numbering, got %d", len(got))
	}
}

// Line numbers must survive every path through the scanner, since they end up in
// the bundle as the pointer an agent follows back to the source.
func TestScanLineNumbersAreContiguous(t *testing.T) {
	src := "a\n/* c\nc */\n`t\nt`\nz"
	for i, cl := range scanLines(src, scanJSLike) {
		if cl.Num != i+1 {
			t.Errorf("line %d reports Num=%d", i+1, cl.Num)
		}
	}
}

func TestScanIsDeterministic(t *testing.T) {
	src := "import a from \"x\"\n/* c */\n`tpl ${1}`\nexport const y = 'z'"
	want := strings.Join(codeOf(src, scanJSLike), "\n")
	for i := 0; i < 20; i++ {
		if got := strings.Join(codeOf(src, scanJSLike), "\n"); got != want {
			t.Fatalf("run %d differs", i)
		}
	}
}
