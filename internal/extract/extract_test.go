package extract

import (
	"errors"
	"strings"
	"testing"

	"github.com/cisco-sbg-emu/signpost/internal/discover"
)

// fakeExtractor returns canned facts, so the harness itself can be tested
// independently of any real extractor.
type fakeExtractor struct {
	langs  []discover.Lang
	byPath map[string]Facts
	fail   map[string]error
}

func (f fakeExtractor) Langs() []discover.Lang { return f.langs }

func (f fakeExtractor) Extract(file discover.File) (Facts, error) {
	if err := f.fail[file.Path]; err != nil {
		return Facts{}, err
	}
	return f.byPath[file.Path], nil
}

func file(path string, lang discover.Lang) discover.File {
	return discover.File{Path: path, Lang: lang, Class: discover.ClassSource, Content: "x"}
}

func TestRegistryDispatchesByLanguage(t *testing.T) {
	goExt := fakeExtractor{langs: []discover.Lang{discover.LangGo}}
	tsExt := fakeExtractor{langs: []discover.Lang{discover.LangTS, discover.LangJS}}

	r := NewRegistry()
	r.Register(goExt)
	r.Register(tsExt)

	if r.For(discover.LangGo) == nil {
		t.Error("go extractor should be registered")
	}
	if r.For(discover.LangJS) == nil {
		t.Error("an extractor claiming two languages should be registered for both")
	}
	if r.For(discover.LangRust) != nil {
		t.Error("unregistered language should return nil")
	}
	got := r.Langs()
	if len(got) != 3 {
		t.Errorf("Langs() = %v, want 3 entries", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Errorf("Langs() not sorted: %v", got)
		}
	}
}

// Every first-class language must have an extractor in the default set, and each
// must actually read the language it claims. A language silently missing here
// would be reported as unhandled for a whole repo.
func TestDefaultRegistryCoversEveryFirstClassLanguage(t *testing.T) {
	r := DefaultRegistry()
	sources := map[discover.Lang]string{
		discover.LangGo:     "package p\n\nimport \"fmt\"\n\nfunc Exported() { fmt.Print() }\n",
		discover.LangPython: "import os\n\ndef exported():\n    return os.getcwd()\n",
		discover.LangTS:     "import fs from \"node:fs\";\nexport function exported() { return fs; }\n",
		discover.LangJS:     "const fs = require(\"node:fs\");\nfunction exported() { return fs; }\n",
		discover.LangRust:   "use std::fmt;\n\npub fn exported() -> fmt::Result { Ok(()) }\n",
	}
	for lang, src := range sources {
		e := r.For(lang)
		if e == nil {
			t.Errorf("%s has no extractor in the default registry", lang)
			continue
		}
		fa, err := e.Extract(discover.File{
			Path: "a." + string(lang), Lang: lang, Class: discover.ClassSource, Content: src,
		})
		if err != nil {
			t.Errorf("%s: Extract errored: %v", lang, err)
			continue
		}
		if len(fa.Imports) == 0 {
			t.Errorf("%s: registered extractor found no imports in %q", lang, src)
		}
		if len(fa.Symbols) == 0 {
			t.Errorf("%s: registered extractor found no symbols in %q", lang, src)
		}
	}
}

// A later registration replaces an earlier one, which is what makes the
// documented tree-sitter fallback a swap rather than a redesign.
func TestRegisterReplacesForSameLanguage(t *testing.T) {
	first := fakeExtractor{
		langs:  []discover.Lang{discover.LangPython},
		byPath: map[string]Facts{"a.py": {Package: "first"}},
	}
	second := fakeExtractor{
		langs:  []discover.Lang{discover.LangPython},
		byPath: map[string]Facts{"a.py": {Package: "second"}},
	}
	r := NewRegistry()
	r.Register(first)
	r.Register(second)

	facts, err := r.For(discover.LangPython).Extract(file("a.py", discover.LangPython))
	if err != nil {
		t.Fatal(err)
	}
	if facts.Package != "second" {
		t.Errorf("Package = %q, want the replacement extractor's output", facts.Package)
	}
}

func TestRunRecordsUnhandledLanguagesAndFailures(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeExtractor{
		langs:  []discover.Lang{discover.LangGo},
		byPath: map[string]Facts{"ok.go": {Package: "main"}},
		fail:   map[string]error{"bad.go": errors.New("syntax error at line 3")},
	})

	res := &discover.Result{Files: []discover.File{
		file("ok.go", discover.LangGo),
		file("bad.go", discover.LangGo),
		file("a.rs", discover.LangRust),
		file("b.rs", discover.LangRust),
		file("c.java", discover.LangOther),
	}}

	run := r.Run(res)

	// One file failing must not abort the run: a large repo always has one file
	// something cannot parse.
	if len(run.Facts) != 1 || run.Facts[0].Path != "ok.go" {
		t.Errorf("expected only ok.go to yield facts, got %+v", run.Facts)
	}
	if len(run.Failures) != 1 || run.Failures[0].Path != "bad.go" {
		t.Errorf("expected bad.go recorded as a failure, got %+v", run.Failures)
	}
	if !strings.Contains(run.Failures[0].Reason, "syntax error") {
		t.Errorf("failure reason should be preserved, got %q", run.Failures[0].Reason)
	}
	if run.Unhandled[discover.LangRust] != 2 {
		t.Errorf("Unhandled[rust] = %d, want 2", run.Unhandled[discover.LangRust])
	}
	if run.Unhandled[discover.LangOther] != 1 {
		t.Errorf("Unhandled[other] = %d, want 1", run.Unhandled[discover.LangOther])
	}
}

// Truncated input must be reported as incomplete: extraction over a head/tail
// slice is not extraction over the file.
func TestRunMarksTruncatedFilesIncomplete(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeExtractor{
		langs:  []discover.Lang{discover.LangGo},
		byPath: map[string]Facts{"big.go": {Package: "main"}},
	})
	f := file("big.go", discover.LangGo)
	f.Truncated = true

	run := r.Run(&discover.Result{Files: []discover.File{f}})
	if len(run.Facts) != 1 {
		t.Fatalf("expected 1 facts entry, got %d", len(run.Facts))
	}
	if !run.Facts[0].Incomplete {
		t.Error("facts from a truncated file must be marked Incomplete")
	}
	if run.Facts[0].Note == "" {
		t.Error("Incomplete facts should carry an explanatory Note")
	}
}

func TestRunSortsFactsByPath(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeExtractor{
		langs: []discover.Lang{discover.LangGo},
		byPath: map[string]Facts{
			"z.go": {Package: "z"}, "a.go": {Package: "a"}, "m.go": {Package: "m"},
		},
	})
	res := &discover.Result{Files: []discover.File{
		file("z.go", discover.LangGo), file("a.go", discover.LangGo), file("m.go", discover.LangGo),
	}}
	run := r.Run(res)
	var got []string
	for _, f := range run.Facts {
		got = append(got, f.Path)
	}
	if strings.Join(got, ",") != "a.go,m.go,z.go" {
		t.Errorf("facts should be sorted by path, got %v", got)
	}
}

func TestNormalizeMergesDuplicateImports(t *testing.T) {
	fa := Facts{Imports: []Import{
		{Raw: "os", Names: []string{"b"}, Line: 5},
		{Raw: "os", Names: []string{"a"}, Line: 2},
		{Raw: "sys", Line: 1},
		{Raw: "os", Names: []string{"a"}, Line: 9}, // duplicate name
	}}
	fa.Normalize()

	if len(fa.Imports) != 2 {
		t.Fatalf("expected os and sys to merge to 2 imports, got %d: %+v", len(fa.Imports), fa.Imports)
	}
	os := fa.Imports[0]
	if os.Raw != "os" {
		t.Fatalf("imports should be sorted by raw path, got %q first", os.Raw)
	}
	if strings.Join(os.Names, ",") != "a,b" {
		t.Errorf("merged names = %v, want [a b] deduped and sorted", os.Names)
	}
	if os.Line != 2 {
		t.Errorf("merged Line = %d, want the earliest (2)", os.Line)
	}
}

// An import and its aliased form are different bindings and must not merge.
func TestNormalizeKeepsDistinctAliasesSeparate(t *testing.T) {
	fa := Facts{Imports: []Import{
		{Raw: "numpy", Alias: "np"},
		{Raw: "numpy"},
	}}
	fa.Normalize()
	if len(fa.Imports) != 2 {
		t.Errorf("an aliased and unaliased import of the same path are distinct, got %+v", fa.Imports)
	}
}

// Ordering must not depend on line numbers: reordering declarations in a file
// without changing them should not change the bundle.
func TestNormalizeOrderIsIndependentOfSourceOrder(t *testing.T) {
	a := Facts{
		Imports: []Import{{Raw: "b", Line: 1}, {Raw: "a", Line: 2}},
		Symbols: []Symbol{{Name: "Zed", Line: 1}, {Name: "Alpha", Line: 2}},
	}
	b := Facts{
		Imports: []Import{{Raw: "a", Line: 90}, {Raw: "b", Line: 91}},
		Symbols: []Symbol{{Name: "Alpha", Line: 90}, {Name: "Zed", Line: 91}},
	}
	a.Normalize()
	b.Normalize()

	if strings.Join(a.ImportPaths(), ",") != strings.Join(b.ImportPaths(), ",") {
		t.Error("import ordering should not depend on source position")
	}
	if strings.Join(a.SymbolNames(), ",") != strings.Join(b.SymbolNames(), ",") {
		t.Error("symbol ordering should not depend on source position")
	}
}

func TestSymbolNamesQualifiesMethods(t *testing.T) {
	fa := Facts{Symbols: []Symbol{
		{Name: "Read", Kind: SymMethod, Recv: "Client"},
		{Name: "Read", Kind: SymFunc},
	}}
	fa.Normalize()
	got := strings.Join(fa.SymbolNames(), ",")
	if got != "Client.Read,Read" {
		t.Errorf("SymbolNames() = %q, want methods qualified by receiver", got)
	}
}

func TestExportedSymbols(t *testing.T) {
	fa := Facts{Symbols: []Symbol{
		{Name: "Public", Exported: true},
		{Name: "private"},
	}}
	got := fa.ExportedSymbols()
	if len(got) != 1 || got[0].Name != "Public" {
		t.Errorf("ExportedSymbols() = %+v, want only Public", got)
	}
}

func TestFirstSentence(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"Does the thing.", "Does the thing."},
		{"Does the thing. And then more.", "Does the thing."},
		{"Multi\nline\ncomment.", "Multi line comment."},
		{"   spaced   out   text.  ", "spaced out text."},
		// A period inside a version or abbreviation is not a sentence end.
		{"Requires v1.2.3 of the API.", "Requires v1.2.3 of the API."},
		{"Uses e.g. this form.", "Uses e.g. this form."},
		{"See i.e. the note. Then more.", "See i.e. the note."},
		{"Handles retries, timeouts, etc. Nothing else.", "Handles retries, timeouts, etc. Nothing else."},
		{"Named after A. Turing. More text.", "Named after A. Turing."},
		// No terminator at all: returned whole.
		{"No terminator here", "No terminator here"},
	}
	for _, c := range cases {
		if got := FirstSentence(c.in); got != c.want {
			t.Errorf("FirstSentence(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A doc comment with no sentence break must be capped, and the cap must not
// split a multi-byte rune.
func TestFirstSentenceCapsLongTextOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("héllo wörld ", 60) // no period anywhere
	got := FirstSentence(long)
	if len(got) > 260 {
		t.Errorf("result is %d bytes, expected it to be capped", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated sentence should be marked with an ellipsis, got %q", got)
	}
	for i, r := range got {
		if r == 0xFFFD {
			t.Errorf("truncation split a rune at byte %d: %q", i, got)
		}
	}
}
