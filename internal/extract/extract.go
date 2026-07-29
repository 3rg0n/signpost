// Package extract turns discovered files into language-neutral facts: what a
// file declares, what it imports, and what it exposes.
//
// Extractors deliberately do not write into the graph. They return Facts, and a
// later stage assembles the graph from them. Two reasons:
//
//   - Facts are what a fixture can hand-label. Scoring an extractor against a
//     hand-labeled expectation (design §4.2) needs a comparison at the level of
//     "did you find this import", not "did the graph end up with this node".
//   - Graph assembly is shared. Resolving an import to a module node, deciding
//     whether a symbol deserves its own page, and merging duplicates are the same
//     problem in every language, and they should be solved once.
package extract

import (
	"sort"
	"strings"

	"github.com/cisco-sbg-emu/signpost/internal/discover"
)

// SymbolKind classifies a declaration. These are the shapes that exist across
// the four first-class languages; a language maps its own vocabulary onto them.
type SymbolKind string

const (
	SymFunc      SymbolKind = "func"
	SymMethod    SymbolKind = "method"
	SymType      SymbolKind = "type"
	SymInterface SymbolKind = "interface"
	SymClass     SymbolKind = "class"
	SymConst     SymbolKind = "const"
	SymVar       SymbolKind = "var"
)

// Import is one dependency edge read out of a file.
type Import struct {
	// Raw is the import path exactly as written, minus quotes. This is what a
	// fixture labels, because it is the only part that is unambiguously in the
	// source text.
	Raw string
	// Resolved is the repo-relative path or module ID this import refers to,
	// filled in by resolution rather than by the extractor. Empty when the
	// import is external or could not be resolved.
	Resolved string
	// External marks an import that leaves the repository.
	External bool
	// Names are the specific symbols imported, when the syntax names them
	// (`from x import a, b`, `import { a, b } from 'x'`). Sorted. Empty for a
	// whole-module import.
	Names []string
	// Alias is the local name bound to the import, when renamed.
	Alias string
	Line  int
}

// Symbol is one declaration a file exposes.
type Symbol struct {
	Name string
	Kind SymbolKind
	// Exported is language-specific: capitalisation in Go, `export` in TS/JS,
	// absence of a leading underscore in Python, `pub` in Rust.
	Exported bool
	// Recv is the receiver or owning type for a method, empty otherwise.
	Recv string
	// Doc is the first sentence of the declaration's doc comment, when present.
	// This is the only prose the deterministic pass can honestly attribute to a
	// symbol, and it is often better than anything a model would write.
	Doc  string
	Line int
}

// Facts is everything one extractor read out of one file.
type Facts struct {
	Path string
	Lang discover.Lang
	// Package is the declared package, module, or namespace. Go's package
	// clause, Python's inferred dotted path, Rust's crate module. Empty for
	// TS/JS, which has no such declaration.
	Package string
	Imports []Import
	Symbols []Symbol
	// Entrypoints are declarations that start execution: `func main`,
	// `if __name__ == "__main__"`, a `bin` script target, `fn main`.
	Entrypoints []string
	// Incomplete marks a file the extractor could not fully read — a syntax
	// error, or content truncated by the size caps. The bundle reports these
	// rather than presenting partial extraction as complete (design §4.2).
	Incomplete bool
	// Note explains Incomplete, for the manifest.
	Note string
}

// Extractor reads facts out of one file's content.
//
// Implementations must be pure: same input, same output, no filesystem access, no
// network, no clock. Everything downstream depends on that, and it is what makes
// the fixture scoring meaningful.
type Extractor interface {
	// Langs reports which languages this extractor handles.
	Langs() []discover.Lang
	// Extract reads facts from one file. An error means the file could not be
	// processed at all; a partial read should return Facts with Incomplete set
	// instead, because partial facts are still useful and an error discards them.
	Extract(f discover.File) (Facts, error)
}

// Registry dispatches files to extractors by language.
type Registry struct {
	byLang map[discover.Lang]Extractor
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byLang: make(map[discover.Lang]Extractor)}
}

// Register adds an extractor for each language it claims. A second registration
// for the same language replaces the first, which is what makes the documented
// tree-sitter fallback (design §4.1) a swap rather than a redesign.
func (r *Registry) Register(e Extractor) {
	for _, l := range e.Langs() {
		r.byLang[l] = e
	}
}

// DefaultRegistry returns a registry holding every extractor signpost ships.
//
// One place assembles the set so a run cannot silently disagree with a test about
// which languages are covered — a repo reported as fully extracted because an
// extractor was never registered is worse than one reported as unhandled.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(GoExtractor{})
	r.Register(PythonExtractor{})
	r.Register(TSExtractor{})
	r.Register(RustExtractor{})
	return r
}

// For returns the extractor for a language, or nil.
func (r *Registry) For(l discover.Lang) Extractor {
	return r.byLang[l]
}

// Langs returns the registered languages, sorted.
func (r *Registry) Langs() []discover.Lang {
	out := make([]discover.Lang, 0, len(r.byLang))
	for l := range r.byLang {
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// RunResult is the outcome of extracting a whole discovery result.
type RunResult struct {
	Facts []Facts
	// Unhandled counts files whose language has no registered extractor, by
	// language. Reported so a repo that is 40% Java does not look fully covered.
	Unhandled map[discover.Lang]int
	// Failures records files an extractor rejected outright.
	Failures []Failure
}

// Failure is one file an extractor could not process.
type Failure struct {
	Path   string
	Reason string
}

// Run extracts every source file in a discovery result.
//
// Facts come back sorted by path. A single file's failure never aborts the run —
// a build that fails because one file in a large repo has a syntax error is a
// build nobody can use — but every failure is recorded.
func (r *Registry) Run(res *discover.Result) *RunResult {
	out := &RunResult{Unhandled: make(map[discover.Lang]int)}
	for _, f := range res.Sources() {
		e := r.byLang[f.Lang]
		if e == nil {
			out.Unhandled[f.Lang]++
			continue
		}
		facts, err := e.Extract(f)
		if err != nil {
			out.Failures = append(out.Failures, Failure{Path: f.Path, Reason: err.Error()})
			continue
		}
		facts.Path = f.Path
		facts.Lang = f.Lang
		if f.Truncated {
			facts.Incomplete = true
			if facts.Note == "" {
				facts.Note = "content truncated by size cap; extraction covers head and tail only"
			}
		}
		facts.Normalize()
		out.Facts = append(out.Facts, facts)
	}
	sort.Slice(out.Facts, func(i, j int) bool { return out.Facts[i].Path < out.Facts[j].Path })
	sort.Slice(out.Failures, func(i, j int) bool { return out.Failures[i].Path < out.Failures[j].Path })
	return out
}

// Normalize sorts and dedupes a Facts value so two extractors that found the
// same things produce identical output regardless of traversal order.
//
// Sorting is by the fields that identify a fact, never by line number: a fact's
// identity is what it says, not where it was written. That distinction matters
// because it makes the output stable when a file is reordered without being
// semantically changed, which keeps the bundle's diff small.
func (fa *Facts) Normalize() {
	sort.Slice(fa.Imports, func(i, j int) bool {
		if fa.Imports[i].Raw != fa.Imports[j].Raw {
			return fa.Imports[i].Raw < fa.Imports[j].Raw
		}
		return fa.Imports[i].Alias < fa.Imports[j].Alias
	})
	// Merge duplicate imports of the same path, unioning their named symbols.
	// Two `from x import a` / `from x import b` lines are one dependency.
	if len(fa.Imports) > 1 {
		merged := make([]Import, 0, len(fa.Imports))
		for _, im := range fa.Imports {
			sort.Strings(im.Names)
			im.Names = dedupeStrings(im.Names)
			last := len(merged) - 1
			if last >= 0 && merged[last].Raw == im.Raw && merged[last].Alias == im.Alias {
				merged[last].Names = dedupeStrings(sortedUnion(merged[last].Names, im.Names))
				// Keep the earliest line, so provenance points at first use.
				if im.Line > 0 && (merged[last].Line == 0 || im.Line < merged[last].Line) {
					merged[last].Line = im.Line
				}
				continue
			}
			merged = append(merged, im)
		}
		fa.Imports = merged
	} else {
		for i := range fa.Imports {
			sort.Strings(fa.Imports[i].Names)
			fa.Imports[i].Names = dedupeStrings(fa.Imports[i].Names)
		}
	}

	sort.Slice(fa.Symbols, func(i, j int) bool {
		a, b := fa.Symbols[i], fa.Symbols[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Recv != b.Recv {
			return a.Recv < b.Recv
		}
		return a.Kind < b.Kind
	})
	fa.Symbols = mergeSymbols(fa.Symbols)
	sort.Strings(fa.Entrypoints)
	fa.Entrypoints = dedupeStrings(fa.Entrypoints)
}

// mergeSymbols folds two records of the same declaration into one.
//
// This exists because a language can name a declaration in two places. In
// TypeScript, `function helper() {}` and a later `export { helper }` are one
// symbol described twice: the first knows its kind, the second knows it is
// exported. Emitting both would put a duplicate on the page, one of them with no
// kind at all.
//
// Merging is a union of what each record knows, and exportedness is sticky: if
// any mention says exported, the declaration is reachable from outside, because
// that is what an export statement means.
//
// Requires the slice to be sorted by (Name, Recv, Kind), which Normalize does
// immediately before calling this — so a kindless record sorts adjacent to the
// typed one it describes.
func mergeSymbols(syms []Symbol) []Symbol {
	if len(syms) < 2 {
		return syms
	}
	out := syms[:1]
	for _, s := range syms[1:] {
		last := &out[len(out)-1]
		// Same declaration when the identity matches and the kinds are compatible:
		// either equal, or one side simply does not know.
		same := last.Name == s.Name && last.Recv == s.Recv &&
			(last.Kind == s.Kind || last.Kind == "" || s.Kind == "")
		if !same {
			out = append(out, s)
			continue
		}
		if last.Kind == "" {
			last.Kind = s.Kind
		}
		if s.Exported {
			last.Exported = true
		}
		if last.Doc == "" {
			last.Doc = s.Doc
		}
		// Keep the earliest line: provenance should point at the declaration, not
		// at the export statement that mentions it later.
		if s.Line > 0 && (last.Line == 0 || s.Line < last.Line) {
			last.Line = s.Line
		}
	}
	return out
}

// ExportedSymbols returns only the exported declarations, which are the public
// surface an agent needs and the private ones it usually does not.
func (fa *Facts) ExportedSymbols() []Symbol {
	var out []Symbol
	for _, s := range fa.Symbols {
		if s.Exported {
			out = append(out, s)
		}
	}
	return out
}

// ImportPaths returns the raw import paths, sorted and deduped.
func (fa *Facts) ImportPaths() []string {
	out := make([]string, 0, len(fa.Imports))
	for _, im := range fa.Imports {
		out = append(out, im.Raw)
	}
	sort.Strings(out)
	return dedupeStrings(out)
}

// SymbolNames returns symbol names, sorted and deduped. Methods are qualified as
// "Recv.Name" so an interface method and a same-named free function stay distinct.
func (fa *Facts) SymbolNames() []string {
	out := make([]string, 0, len(fa.Symbols))
	for _, s := range fa.Symbols {
		if s.Recv != "" {
			out = append(out, s.Recv+"."+s.Name)
			continue
		}
		out = append(out, s.Name)
	}
	sort.Strings(out)
	return dedupeStrings(out)
}

func dedupeStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}

// sortedUnion merges two sorted slices.
func sortedUnion(a, b []string) []string {
	out := make([]string, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	sort.Strings(out)
	return out
}

// FirstSentence extracts the first sentence of a doc comment, for Symbol.Doc.
//
// Shared across extractors so "what counts as a sentence" is one decision. The
// rule is deliberately blunt: stop at the first period followed by a space or
// end-of-string, cap the length, and collapse whitespace. Prose in a doc comment
// is a human's, so the safest transformation is the smallest one.
func FirstSentence(doc string) string {
	doc = strings.TrimSpace(strings.Join(strings.Fields(doc), " "))
	if doc == "" {
		return ""
	}
	for i := 0; i < len(doc); i++ {
		if doc[i] != '.' {
			continue
		}
		// A period with a non-space after it is inside a version number or a
		// dotted identifier ("v1.2.3", "pkg.Func"), not a sentence end.
		if i+1 < len(doc) && doc[i+1] != ' ' {
			continue
		}
		// A period followed by a space still is not a sentence end when it
		// terminates an abbreviation — "e.g. this form" is one sentence. Getting
		// this wrong truncates a doc comment mid-clause, which reads as a bug in
		// the extracted prose rather than in the splitter.
		if isAbbreviation(lastWord(doc[:i])) {
			continue
		}
		return doc[:i+1]
	}
	const limit = 240
	if len(doc) > limit {
		// Cut on a rune boundary, then back up to the last space so a word is
		// not split.
		cut := limit
		for cut > 0 && doc[cut]&0xC0 == 0x80 {
			cut--
		}
		if sp := strings.LastIndexByte(doc[:cut], ' '); sp > limit/2 {
			cut = sp
		}
		return strings.TrimSpace(doc[:cut]) + "…"
	}
	return doc
}

// lastWord returns the final whitespace-delimited token of s.
func lastWord(s string) string {
	if i := strings.LastIndexByte(s, ' '); i >= 0 {
		return s[i+1:]
	}
	return s
}

// abbreviations are the tokens that end in a period without ending a sentence.
// Deliberately a short closed list rather than a heuristic: a heuristic here
// guesses wrong on real prose, and the cost of missing one is a slightly short
// summary rather than a wrong one.
var abbreviations = map[string]bool{
	"e.g": true, "i.e": true, "etc": true, "cf": true, "vs": true,
	"approx": true, "resp": true, "viz": true, "al": true, // "et al."
	"Mr": true, "Ms": true, "Mrs": true, "Dr": true, "Inc": true, "Ltd": true,
	"No": true, "vol": true, "fig": true, "ref": true,
}

// isAbbreviation reports whether word (the text before a period) is a known
// abbreviation rather than the end of a sentence.
func isAbbreviation(word string) bool {
	if word == "" {
		return false
	}
	if abbreviations[word] {
		return true
	}
	// A single letter before a period is an initial ("A. Turing") or part of a
	// dotted abbreviation, not a sentence end.
	if len(word) == 1 {
		c := word[0]
		return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
	}
	return false
}
