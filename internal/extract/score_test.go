package extract

import (
	"math"
	"strings"
	"testing"

	"github.com/cisco-sbg-emu/signpost/internal/discover"
)

func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestScoreArithmetic(t *testing.T) {
	// 8 correct, 2 invented, 2 missed.
	s := Score{Kind: "imports", TruePos: 8, FalsePos: 2, FalseNeg: 2}
	if !closeTo(s.Precision(), 0.8) {
		t.Errorf("Precision = %v, want 0.8", s.Precision())
	}
	if !closeTo(s.Recall(), 0.8) {
		t.Errorf("Recall = %v, want 0.8", s.Recall())
	}
	if !closeTo(s.F1(), 0.8) {
		t.Errorf("F1 = %v, want 0.8", s.F1())
	}
}

// An extractor with nothing to find scores 1.0 rather than 0 or NaN: vacuous
// success is correct here, and a NaN would silently poison the manifest.
func TestScoreEmptyIsPerfectNotNaN(t *testing.T) {
	s := Score{Kind: "imports"}
	if !closeTo(s.Precision(), 1) || !closeTo(s.Recall(), 1) {
		t.Errorf("empty score should be 1.0/1.0, got P=%v R=%v", s.Precision(), s.Recall())
	}
	if math.IsNaN(s.F1()) {
		t.Error("F1 must never be NaN")
	}
}

// Found nothing when there was something to find: recall 0, and F1 must be 0
// rather than NaN from a 0/0 harmonic mean.
func TestScoreFoundNothing(t *testing.T) {
	s := Score{Kind: "symbols", FalseNeg: 5}
	if !closeTo(s.Recall(), 0) {
		t.Errorf("Recall = %v, want 0", s.Recall())
	}
	if !closeTo(s.F1(), 0) || math.IsNaN(s.F1()) {
		t.Errorf("F1 = %v, want 0", s.F1())
	}
}

func TestScoreExtractorPerfectRun(t *testing.T) {
	e := fakeExtractor{
		langs: []discover.Lang{discover.LangGo},
		byPath: map[string]Facts{
			"a.go": {
				Package: "a",
				Imports: []Import{{Raw: "fmt"}, {Raw: "os"}},
				Symbols: []Symbol{
					{Name: "Run", Kind: SymFunc, Exported: true},
					{Name: "helper", Kind: SymFunc},
				},
				Entrypoints: []string{"main"},
			},
		},
	}
	fx := []Fixture{{
		File: file("a.go", discover.LangGo),
		Expected: Expected{
			Path: "a.go", Package: "a",
			Imports:     []string{"fmt", "os"},
			Symbols:     []string{"Run", "helper"},
			Exported:    []string{"Run"},
			Entrypoints: []string{"main"},
		},
	}}

	ls := ScoreExtractor(e, discover.LangGo, fx)
	if !ls.MeetsTarget() {
		t.Errorf("a perfect extractor should meet target:\n%s", ls.Report())
	}
	if !closeTo(ls.Imports.F1(), 1) || !closeTo(ls.Symbols.F1(), 1) {
		t.Errorf("expected F1 of 1.0, got:\n%s", ls.Report())
	}
}

// The case a naive harness gets wrong: an extractor that reports everything has
// perfect recall. Precision must catch it, because a spurious edge points an
// agent at something that does not exist.
func TestScoreCatchesInventedFacts(t *testing.T) {
	e := fakeExtractor{
		langs: []discover.Lang{discover.LangGo},
		byPath: map[string]Facts{
			"a.go": {Imports: []Import{
				{Raw: "fmt"}, {Raw: "os"},
				{Raw: "invented/one"}, {Raw: "invented/two"},
			}},
		},
	}
	fx := []Fixture{{
		File:     file("a.go", discover.LangGo),
		Expected: Expected{Imports: []string{"fmt", "os"}},
	}}

	ls := ScoreExtractor(e, discover.LangGo, fx)
	if !closeTo(ls.Imports.Recall(), 1) {
		t.Errorf("recall should be perfect here, got %v", ls.Imports.Recall())
	}
	if !closeTo(ls.Imports.Precision(), 0.5) {
		t.Errorf("Precision = %v, want 0.5", ls.Imports.Precision())
	}
	if ls.MeetsTarget() {
		t.Error("an extractor inventing half its output must not meet target")
	}
	// The report must name what was invented, or a failure is unactionable.
	if len(ls.Imports.Spurious) != 2 {
		t.Errorf("expected 2 spurious imports recorded, got %v", ls.Imports.Spurious)
	}
	rep := ls.Report()
	if !strings.Contains(rep, "invented/one") || !strings.Contains(rep, "BELOW TARGET") {
		t.Errorf("report should name the invented imports and the status:\n%s", rep)
	}
}

func TestScoreRecordsMissingFacts(t *testing.T) {
	e := fakeExtractor{
		langs:  []discover.Lang{discover.LangPython},
		byPath: map[string]Facts{"m.py": {Imports: []Import{{Raw: "os"}}}},
	}
	fx := []Fixture{{
		File:     file("m.py", discover.LangPython),
		Expected: Expected{Imports: []string{"os", "sys", "json"}},
	}}

	ls := ScoreExtractor(e, discover.LangPython, fx)
	if ls.Imports.FalseNeg != 2 {
		t.Errorf("FalseNeg = %d, want 2", ls.Imports.FalseNeg)
	}
	if !closeTo(ls.Imports.Precision(), 1) {
		t.Errorf("Precision should be 1.0 when nothing was invented, got %v", ls.Imports.Precision())
	}
	joined := strings.Join(ls.Imports.Missing, " ")
	if !strings.Contains(joined, "sys") || !strings.Contains(joined, "json") {
		t.Errorf("missing imports should be named, got %v", ls.Imports.Missing)
	}
	if ls.MeetsTarget() {
		t.Error("recall of 1/3 must not meet target")
	}
}

func TestScoreCatchesWrongPackage(t *testing.T) {
	e := fakeExtractor{
		langs:  []discover.Lang{discover.LangGo},
		byPath: map[string]Facts{"a.go": {Package: "wrong"}},
	}
	fx := []Fixture{{
		File:     file("a.go", discover.LangGo),
		Expected: Expected{Package: "right"},
	}}
	ls := ScoreExtractor(e, discover.LangGo, fx)
	if len(ls.Mismatches) != 1 {
		t.Fatalf("expected a package mismatch, got %v", ls.Mismatches)
	}
	if ls.MeetsTarget() {
		t.Error("a package mismatch must fail the target even with perfect F1")
	}
}

func TestScoreRecordsExtractorFailures(t *testing.T) {
	e := fakeExtractor{
		langs: []discover.Lang{discover.LangRust},
		fail:  map[string]error{"broken.rs": errTest},
	}
	fx := []Fixture{{File: file("broken.rs", discover.LangRust)}}
	ls := ScoreExtractor(e, discover.LangRust, fx)
	if len(ls.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %v", ls.Failures)
	}
	if ls.MeetsTarget() {
		t.Error("an outright extractor failure must fail the target")
	}
	if !strings.Contains(ls.Report(), "FAILED") {
		t.Errorf("report should surface the failure:\n%s", ls.Report())
	}
}

// Exportedness is only checked when the fixture labels it, so a fixture can
// decline to assert a field it is not testing.
func TestScoreSkipsUnlabeledFields(t *testing.T) {
	e := fakeExtractor{
		langs: []discover.Lang{discover.LangGo},
		byPath: map[string]Facts{"a.go": {
			Symbols:     []Symbol{{Name: "X", Exported: true}},
			Entrypoints: []string{"main"},
		}},
	}
	// Expected labels neither Exported nor Entrypoints (both nil).
	fx := []Fixture{{
		File:     file("a.go", discover.LangGo),
		Expected: Expected{Symbols: []string{"X"}},
	}}
	ls := ScoreExtractor(e, discover.LangGo, fx)
	if len(ls.Mismatches) != 0 {
		t.Errorf("unlabeled fields should not be scored, got %v", ls.Mismatches)
	}
	if !ls.MeetsTarget() {
		t.Errorf("should meet target:\n%s", ls.Report())
	}
}

// An empty labeled slice is an assertion that there is nothing, and is distinct
// from a nil slice meaning "not labeled".
func TestScoreEmptyLabelAssertsAbsence(t *testing.T) {
	e := fakeExtractor{
		langs:  []discover.Lang{discover.LangGo},
		byPath: map[string]Facts{"a.go": {Entrypoints: []string{"main"}}},
	}
	fx := []Fixture{{
		File:     file("a.go", discover.LangGo),
		Expected: Expected{Entrypoints: []string{}},
	}}
	ls := ScoreExtractor(e, discover.LangGo, fx)
	if len(ls.Mismatches) == 0 {
		t.Error("an empty label should assert absence and catch a spurious entrypoint")
	}
}

// Scoring must be deterministic: the reported missing/spurious lists are read by
// humans and written into manifest.json, so they cannot vary between runs.
func TestScoreIsDeterministic(t *testing.T) {
	e := fakeExtractor{
		langs: []discover.Lang{discover.LangTS},
		byPath: map[string]Facts{
			"z.ts": {Imports: []Import{{Raw: "bad-z"}}},
			"a.ts": {Imports: []Import{{Raw: "bad-a"}}},
			"m.ts": {Imports: []Import{{Raw: "bad-m"}}},
		},
	}
	fx := []Fixture{
		{File: file("z.ts", discover.LangTS), Expected: Expected{Imports: []string{"real-z"}}},
		{File: file("a.ts", discover.LangTS), Expected: Expected{Imports: []string{"real-a"}}},
		{File: file("m.ts", discover.LangTS), Expected: Expected{Imports: []string{"real-m"}}},
	}

	want := ScoreExtractor(e, discover.LangTS, fx).Report()
	for i := 0; i < 20; i++ {
		if got := ScoreExtractor(e, discover.LangTS, fx).Report(); got != want {
			t.Fatalf("run %d differed:\n%s\nvs\n%s", i, got, want)
		}
	}
	// Fixture input order must not matter either.
	rev := []Fixture{fx[2], fx[0], fx[1]}
	if got := ScoreExtractor(e, discover.LangTS, rev).Report(); got != want {
		t.Errorf("report depends on fixture input order:\n%s\nvs\n%s", got, want)
	}
}

// The thresholds are a commitment in the design; a change to them should be a
// deliberate edit that fails this test first.
func TestTargetsMatchTheDesign(t *testing.T) {
	if TargetImportF1 != 0.95 {
		t.Errorf("TargetImportF1 = %v, design §4.2 commits to 0.95", TargetImportF1)
	}
	if TargetSymbolF1 != 0.90 {
		t.Errorf("TargetSymbolF1 = %v, design §4.2 commits to 0.90", TargetSymbolF1)
	}
}

// A score just under the threshold must fail: the boundary is where a
// misimplemented comparison hides.
func TestMeetsTargetBoundary(t *testing.T) {
	// 19 of 20 correct = F1 0.95 exactly, which meets the import target.
	at := LangScore{
		Imports: Score{TruePos: 19, FalsePos: 1, FalseNeg: 1},
		Symbols: Score{TruePos: 10},
	}
	if !closeTo(at.Imports.F1(), 0.95) {
		t.Fatalf("fixture arithmetic wrong: F1 = %v", at.Imports.F1())
	}
	if !at.MeetsTarget() {
		t.Error("exactly at the threshold should meet target")
	}

	below := LangScore{
		Imports: Score{TruePos: 18, FalsePos: 2, FalseNeg: 2},
		Symbols: Score{TruePos: 10},
	}
	if below.MeetsTarget() {
		t.Errorf("F1 of %v is below 0.95 and must fail", below.Imports.F1())
	}
}

var errTest = &testErr{}

type testErr struct{}

func (*testErr) Error() string { return "parse failed" }
