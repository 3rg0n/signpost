package extract

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

// The languages the documents claim signpost reads, checked against the extractors it
// registers.
//
// # Why this exists
//
// DefaultRegistry is the one place that decides which languages this tool reads. Four
// documents restate that set in prose — the README's `What it reads` paragraph and its
// status row, the landing page's `reads` lead and its status row, and design §4.1 — and a
// fifth restated it as a count. The count drifted: design's decision table said `nine
// languages at F1 1.000` against a registry holding eighteen, because nine was right when
// somebody wrote it and nothing read the sentence again.
//
// site/status_test.go compares the landing page against the README, which catches two
// documents disagreeing with each other. It cannot catch every document agreeing with the
// others and none of them with the code. This compares them to the code. A document that
// names a language no extractor reads, or omits one that ships, fails here.
//
// # What is gated
//
// The set of languages named. Not the wording, and not the order. Each document words the
// list its own way — `TypeScript/JavaScript` in the README, `TS/JS` in both status rows,
// `TypeScript, JavaScript` on the landing page — and all three name the same two languages,
// so langNames maps every name a document may use onto what it claims. Order is copy, and
// two of these lists are sentences rather than tables.
//
// TestLanguageNamesAreTheRegistry keeps langNames honest. Without it a new extractor with no
// entry in the map would be absent from the expected set as well as from the documents, and
// every assertion below would agree that nothing is missing.
//
// A list that deliberately leaves a language out declares it. Design §4.1 names Go in its
// own paragraph, above the list of the rest, so that claim omits Go explicitly rather than
// the test inferring an exception from a failure.
//
// go test runs in the package directory, so the paths below reach the repository root
// through internal/extract.
const (
	readmePath = "../../README.md"
	sitePath   = "../../site/index.html"
	designPath = "../../docs/design.md"
)

// langNames maps every name the documents use for a language onto the languages it claims.
// A name may claim two: `TS/JS` is one phrase for two extractable languages, and the C
// family's three are written out separately everywhere despite sharing an extractor.
var langNames = map[string][]discover.Lang{
	"Go":                    {discover.LangGo},
	"TypeScript":            {discover.LangTS},
	"JavaScript":            {discover.LangJS},
	"TS/JS":                 {discover.LangTS, discover.LangJS},
	"TypeScript/JavaScript": {discover.LangTS, discover.LangJS},
	"Python":                {discover.LangPython},
	"Rust":                  {discover.LangRust},
	"Java":                  {discover.LangJava},
	"Kotlin":                {discover.LangKotlin},
	"C":                     {discover.LangC},
	"C++":                   {discover.LangCpp},
	"Objective-C":           {discover.LangObjC},
	"Ruby":                  {discover.LangRuby},
	"PHP":                   {discover.LangPHP},
	"C#":                    {discover.LangCSharp},
	"shell":                 {discover.LangShell},
	"PowerShell":            {discover.LangPowerShell},
	"Vue":                   {discover.LangVue},
	"Svelte":                {discover.LangSvelte},
	"Astro":                 {discover.LangAstro},
}

// langClaim is one list of languages in one document. list captures the list itself and
// must match exactly once: a pattern that matches nothing, or matches a second list
// somewhere else in the file, is a failure rather than an empty set that agrees with
// everything.
type langClaim struct {
	doc   string
	what  string
	list  *regexp.Regexp
	omits []discover.Lang
}

var langClaims = []langClaim{
	{
		doc:  readmePath,
		what: "the `What it reads` paragraph",
		list: regexp.MustCompile(`(?s)First-class languages are (.*?) — imports`),
	},
	{
		doc:  readmePath,
		what: "the status row",
		list: regexp.MustCompile(`\| Language extractors \(([^)]*)\) \|`),
	},
	{
		doc:  sitePath,
		what: "the `reads` lead",
		list: regexp.MustCompile(`(?s)<section class="row" id="reads">.*?<p class="prose prose--lead">(.*?) — imports`),
	},
	{
		doc:  sitePath,
		what: "the status row",
		list: regexp.MustCompile(`<th scope="row">Extractors: ([^<]*)</th>`),
	},
	{
		doc:  designPath,
		what: "§4.1",
		list: regexp.MustCompile(`\n\*\*([^*]+)\*\*\s*\nget hand-written line-oriented extractors`),
		// §4.1 gives Go its own paragraph, above this list, because Go is parsed by the
		// stdlib rather than by a hand-written extractor. Naming it here would claim it
		// twice.
		omits: []discover.Lang{discover.LangGo},
	},
}

var (
	langTagRe  = regexp.MustCompile(`<[^>]*>`)
	langLinkRe = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// langDoc reads a repository document with CRLF folded to LF, so a clone made with a global
// core.autocrlf compares the same text as CI does.
func langDoc(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// langNamesIn splits one list into the names it holds. The lists are sentences as often as
// they are table cells, so a serial `and` separates like a comma does, and markup around a
// name is not part of it. Whitespace collapses first, because the site wraps `Svelte and
// Astro` across two lines of HTML and the `and` between them still separates two names.
func langNamesIn(list string) []string {
	list = langLinkRe.ReplaceAllString(list, "$1")
	list = langTagRe.ReplaceAllString(list, "")
	list = strings.ReplaceAll(list, "`", "")
	list = strings.Join(strings.Fields(list), " ")
	list = strings.ReplaceAll(list, " and ", ", ")
	var out []string
	for _, part := range strings.Split(list, ",") {
		if name := strings.TrimSpace(part); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// langsMissing returns the languages in a that b does not hold, named and sorted so a
// failure reads the same on every platform.
func langsMissing(a, b map[discover.Lang]bool) []string {
	var out []string
	for l := range a {
		if !b[l] {
			out = append(out, string(l))
		}
	}
	sort.Strings(out)
	return out
}

func registeredLangs() map[discover.Lang]bool {
	out := make(map[discover.Lang]bool)
	for _, l := range DefaultRegistry().Langs() {
		out[l] = true
	}
	return out
}

// A language with no name here is invisible to every assertion in this file, so the map is
// checked against the registry before it is used to check anything else.
func TestLanguageNamesAreTheRegistry(t *testing.T) {
	named := make(map[discover.Lang]bool)
	for name, langs := range langNames {
		if len(langs) == 0 {
			t.Errorf("langNames maps %q onto no language", name)
		}
		for _, l := range langs {
			named[l] = true
		}
	}
	registered := registeredLangs()

	if miss := langsMissing(registered, named); len(miss) > 0 {
		t.Errorf("DefaultRegistry reads %s and langNames holds no name for it; add the name the documents use, then say it in all %d of them",
			strings.Join(miss, ", "), len(langClaims))
	}
	if extra := langsMissing(named, registered); len(extra) > 0 {
		t.Errorf("langNames names %s, which DefaultRegistry does not read", strings.Join(extra, ", "))
	}
}

func TestDocumentedLanguagesAreTheRegistry(t *testing.T) {
	for _, claim := range langClaims {
		t.Run(claim.doc+" "+claim.what, func(t *testing.T) {
			found := claim.list.FindAllStringSubmatch(langDoc(t, claim.doc), -1)
			if len(found) != 1 {
				t.Fatalf("%s: %d match(es) for %s, want exactly 1; the pattern is %s",
					claim.doc, len(found), claim.what, claim.list)
			}

			got := make(map[discover.Lang]bool)
			for _, name := range langNamesIn(found[0][1]) {
				langs, ok := langNames[name]
				if !ok {
					t.Errorf("%s: %s names %q, and no language answers to that name; either the list is wrong or langNames needs the spelling",
						claim.doc, claim.what, name)
					continue
				}
				for _, l := range langs {
					if got[l] {
						t.Errorf("%s: %s names %s twice", claim.doc, claim.what, l)
					}
					got[l] = true
				}
			}

			want := registeredLangs()
			for _, l := range claim.omits {
				delete(want, l)
				if got[l] {
					t.Errorf("%s: %s names %s, which this list states separately", claim.doc, claim.what, l)
					delete(got, l)
				}
			}

			if miss := langsMissing(want, got); len(miss) > 0 {
				t.Errorf("%s: %s does not name %s, which signpost reads", claim.doc, claim.what, strings.Join(miss, ", "))
			}
			if extra := langsMissing(got, want); len(extra) > 0 {
				t.Errorf("%s: %s names %s, which signpost does not read", claim.doc, claim.what, strings.Join(extra, ", "))
			}
		})
	}
}
