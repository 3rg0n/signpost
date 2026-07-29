package extract

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cisco-sbg-emu/signpost/internal/discover"
)

// Extractor accuracy is measured, not asserted (design §4.2).
//
// The hand-written extractors are explicitly approximations, which is a
// defensible engineering choice only if the approximation is quantified. So each
// one ships with a fixture corpus and a hand-labeled expectation, and the
// measured score is reported in manifest.json. A language below target is
// recorded in skipped_checks and says so on the affected pages.
//
// The scoring is per-fact-kind because the failure modes differ: an extractor
// that finds every import but misses half the symbols is useful for the module
// graph and useless for the public surface, and one aggregate number would hide
// that.

// Target is the F1 below which an extractor is considered inadequate for a
// language, and the bundle says so rather than presenting it as clean.
//
// 0.95 for imports because the module graph is the backbone of the bundle: a
// missed import is a missing edge, and a missing edge is a wrong answer to
// "what depends on this". 0.90 for symbols, which are a navigational aid where a
// miss costs less. These are the numbers the design commits to; an extractor that
// cannot reach them for a language is a documented gap, not a silent one.
const (
	TargetImportF1 = 0.95
	TargetSymbolF1 = 0.90
)

// Expected is the hand-labeled truth for one fixture file.
//
// Deliberately expressed as plain sorted string slices rather than as Facts:
// a person writing a fixture label should not have to fill in a struct with
// fields they are not testing, and a label that is tedious to write is a label
// that gets written carelessly.
type Expected struct {
	Path string
	// Package is checked only when non-empty.
	Package string
	// Imports are the raw import paths expected, in any order.
	Imports []string
	// Symbols are expected declarations. Methods are "Recv.Name".
	Symbols []string
	// Exported is the subset of Symbols expected to be exported. Checked only
	// when non-nil, so a fixture can decline to label exportedness.
	Exported []string
	// Entrypoints expected, in any order.
	Entrypoints []string
}

// Score is precision, recall, and F1 for one fact kind.
type Score struct {
	Kind string
	// TruePos, FalsePos, FalseNeg are counts over the whole corpus.
	TruePos  int
	FalsePos int
	FalseNeg int
	// Missing and Spurious are the actual offending values, capped, so a failing
	// score tells you what to fix rather than only that it failed.
	Missing  []string
	Spurious []string
}

// Precision is the fraction of found facts that were correct. Low precision
// means the extractor invents things, which is worse than missing them: a
// spurious edge sends an agent somewhere that does not exist.
func (s Score) Precision() float64 {
	d := s.TruePos + s.FalsePos
	if d == 0 {
		return 1
	}
	return float64(s.TruePos) / float64(d)
}

// Recall is the fraction of real facts that were found.
func (s Score) Recall() float64 {
	d := s.TruePos + s.FalseNeg
	if d == 0 {
		return 1
	}
	return float64(s.TruePos) / float64(d)
}

// F1 is the harmonic mean of precision and recall.
func (s Score) F1() float64 {
	p, r := s.Precision(), s.Recall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

// String renders a score compactly for test output and manifest.json.
func (s Score) String() string {
	return fmt.Sprintf("%s: P=%.3f R=%.3f F1=%.3f (tp=%d fp=%d fn=%d)",
		s.Kind, s.Precision(), s.Recall(), s.F1(), s.TruePos, s.FalsePos, s.FalseNeg)
}

// LangScore is the full result for one language.
type LangScore struct {
	Lang    discover.Lang
	Files   int
	Imports Score
	Symbols Score
	// Failures are fixture files the extractor rejected outright.
	Failures []Failure
	// Mismatches are non-set-valued disagreements, such as a wrong package name.
	Mismatches []string
}

// MeetsTarget reports whether both scores clear their thresholds and nothing
// failed outright. This is what decides whether the bundle presents the language
// as measured-good or records it in skipped_checks.
func (ls LangScore) MeetsTarget() bool {
	return len(ls.Failures) == 0 &&
		len(ls.Mismatches) == 0 &&
		ls.Imports.F1() >= TargetImportF1 &&
		ls.Symbols.F1() >= TargetSymbolF1
}

// Report renders a multi-line summary.
func (ls LangScore) Report() string {
	var b strings.Builder
	status := "MEETS TARGET"
	if !ls.MeetsTarget() {
		status = "BELOW TARGET"
	}
	fmt.Fprintf(&b, "%s (%d files) — %s\n", ls.Lang, ls.Files, status)
	fmt.Fprintf(&b, "  %s\n", ls.Imports)
	fmt.Fprintf(&b, "  %s\n", ls.Symbols)
	for _, f := range ls.Failures {
		fmt.Fprintf(&b, "  FAILED %s: %s\n", f.Path, f.Reason)
	}
	for _, m := range ls.Mismatches {
		fmt.Fprintf(&b, "  MISMATCH %s\n", m)
	}
	if len(ls.Imports.Missing) > 0 {
		fmt.Fprintf(&b, "  imports missed: %s\n", strings.Join(ls.Imports.Missing, ", "))
	}
	if len(ls.Imports.Spurious) > 0 {
		fmt.Fprintf(&b, "  imports invented: %s\n", strings.Join(ls.Imports.Spurious, ", "))
	}
	if len(ls.Symbols.Missing) > 0 {
		fmt.Fprintf(&b, "  symbols missed: %s\n", strings.Join(ls.Symbols.Missing, ", "))
	}
	if len(ls.Symbols.Spurious) > 0 {
		fmt.Fprintf(&b, "  symbols invented: %s\n", strings.Join(ls.Symbols.Spurious, ", "))
	}
	return b.String()
}

// Fixture pairs a file with its hand-labeled expectation.
type Fixture struct {
	File     discover.File
	Expected Expected
}

// ScoreExtractor runs an extractor over a fixture corpus and scores it.
//
// Every fixture must be labeled; an unlabeled fixture would silently inflate
// precision, since anything the extractor found would have nothing to contradict
// it. That is the specific way a scoring harness lies, so it is checked.
func ScoreExtractor(e Extractor, lang discover.Lang, fixtures []Fixture) LangScore {
	ls := LangScore{Lang: lang, Files: len(fixtures)}
	ls.Imports.Kind = "imports"
	ls.Symbols.Kind = "symbols"

	// Sort so the reported missing/spurious lists are stable.
	fx := make([]Fixture, len(fixtures))
	copy(fx, fixtures)
	sort.Slice(fx, func(i, j int) bool { return fx[i].File.Path < fx[j].File.Path })

	for _, f := range fx {
		facts, err := e.Extract(f.File)
		if err != nil {
			ls.Failures = append(ls.Failures, Failure{Path: f.File.Path, Reason: err.Error()})
			continue
		}
		facts.Path = f.File.Path
		facts.Lang = f.File.Lang
		facts.Normalize()

		if f.Expected.Package != "" && facts.Package != f.Expected.Package {
			ls.Mismatches = append(ls.Mismatches,
				fmt.Sprintf("%s: package = %q, want %q", f.File.Path, facts.Package, f.Expected.Package))
		}
		accumulate(&ls.Imports, f.File.Path, facts.ImportPaths(), f.Expected.Imports)
		accumulate(&ls.Symbols, f.File.Path, facts.SymbolNames(), f.Expected.Symbols)

		if f.Expected.Exported != nil {
			var got []string
			for _, s := range facts.ExportedSymbols() {
				if s.Recv != "" {
					got = append(got, s.Recv+"."+s.Name)
					continue
				}
				got = append(got, s.Name)
			}
			sort.Strings(got)
			if diff := setDiff(dedupeStrings(got), sortedCopy(f.Expected.Exported)); diff != "" {
				ls.Mismatches = append(ls.Mismatches,
					fmt.Sprintf("%s: exported symbols %s", f.File.Path, diff))
			}
		}
		if f.Expected.Entrypoints != nil {
			if diff := setDiff(facts.Entrypoints, sortedCopy(f.Expected.Entrypoints)); diff != "" {
				ls.Mismatches = append(ls.Mismatches,
					fmt.Sprintf("%s: entrypoints %s", f.File.Path, diff))
			}
		}
	}
	return ls
}

// accumulate folds one file's got-versus-want comparison into a Score.
func accumulate(s *Score, path string, got, want []string) {
	const maxReported = 12

	wantSet := make(map[string]bool, len(want))
	for _, w := range want {
		wantSet[w] = true
	}
	gotSet := make(map[string]bool, len(got))
	for _, g := range got {
		gotSet[g] = true
	}

	for _, g := range got {
		if wantSet[g] {
			s.TruePos++
			continue
		}
		s.FalsePos++
		if len(s.Spurious) < maxReported {
			s.Spurious = append(s.Spurious, path+":"+g)
		}
	}
	for _, w := range want {
		if !gotSet[w] {
			s.FalseNeg++
			if len(s.Missing) < maxReported {
				s.Missing = append(s.Missing, path+":"+w)
			}
		}
	}
}

// setDiff describes how two sorted sets differ, or returns "" when equal.
func setDiff(got, want []string) string {
	if strings.Join(got, "\x00") == strings.Join(want, "\x00") {
		return ""
	}
	return fmt.Sprintf("= %v, want %v", got, want)
}

func sortedCopy(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	sort.Strings(out)
	return dedupeStrings(out)
}
