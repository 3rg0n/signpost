package manifest

import (
	"strings"
	"testing"
)

// aliasOf finds a declared alias by pattern, failing if it is absent.
func aliasOf(t *testing.T, f Facts, pattern string) Alias {
	t.Helper()
	for _, a := range f.Resolution.Aliases {
		if a.Pattern == pattern {
			return a
		}
	}
	t.Fatalf("no alias for pattern %q in %+v", pattern, f.Resolution.Aliases)
	return Alias{}
}

func TestTSConfigPaths(t *testing.T) {
	facts := ExtractTSConfig(file("tsconfig.json", `{
  "compilerOptions": {
    "baseUrl": ".",
    "paths": {
      "@fider/*": ["./public/*"],
      "@locale/*": ["./locale/*"]
    }
  }
}`))
	facts.Normalize()

	if facts.Incomplete {
		t.Fatalf("a well-formed tsconfig was recorded as incomplete: %s", facts.Note)
	}
	if got := aliasOf(t, facts, "@fider/*").Targets; len(got) != 1 || got[0] != "public/*" {
		t.Errorf("@fider/* targets = %v, want [public/*]: baseUrl `.` and a `./`-prefixed "+
			"target must both normalize away, or the resolver compares against a path no "+
			"file has", got)
	}
	if got := aliasOf(t, facts, "@locale/*").Targets; len(got) != 1 || got[0] != "locale/*" {
		t.Errorf("@locale/* targets = %v, want [locale/*]", got)
	}
}

// TestTSConfigBaseURLIsRelativeToTheConfig is the case a repo-root assumption gets wrong.
//
// baseUrl is relative to the file that declares it, so the same `src/*` target means two
// different directories in two packages. Resolving it against the repository root instead
// would route one package's imports into another's source.
func TestTSConfigBaseURLIsRelativeToTheConfig(t *testing.T) {
	facts := ExtractTSConfig(file("packages/demo/tsconfig.json", `{
  "compilerOptions": {
    "baseUrl": "./src",
    "paths": {"@app/*": ["*"]}
  }
}`))
	if got := facts.Resolution.BaseURL; got != "packages/demo/src" {
		t.Errorf("baseUrl = %q, want packages/demo/src", got)
	}
	if got := aliasOf(t, facts, "@app/*").Targets; len(got) != 1 || got[0] != "packages/demo/src/*" {
		t.Errorf("@app/* targets = %v, want [packages/demo/src/*]", got)
	}
}

// TestTSConfigNoBaseURL covers `paths` declared without one, which TypeScript reads as
// relative to the config's own directory.
func TestTSConfigNoBaseURL(t *testing.T) {
	facts := ExtractTSConfig(file("packages/api/tsconfig.json", `{
  "compilerOptions": {"paths": {"~/*": ["src/*"]}}
}`))
	if got := facts.Resolution.BaseURL; got != "packages/api" {
		t.Errorf("baseUrl = %q, want packages/api: absent, targets are relative to the "+
			"config's directory", got)
	}
	if got := aliasOf(t, facts, "~/*").Targets; len(got) != 1 || got[0] != "packages/api/src/*" {
		t.Errorf("~/* targets = %v, want [packages/api/src/*]", got)
	}
}

// TestTSConfigExtendsIsResolvedHere checks the relative `extends` path is made
// repo-relative by the reader, which is the only place that knows what it is relative to.
func TestTSConfigExtendsIsResolvedHere(t *testing.T) {
	facts := ExtractTSConfig(file("packages/@scope/thing/tsconfig.json",
		`{"extends": "../../../tsconfig.json", "include": ["src"]}`))
	if got := facts.Resolution.Extends; got != "tsconfig.json" {
		t.Errorf("extends = %q, want tsconfig.json", got)
	}
	if len(facts.Resolution.Aliases) != 0 {
		t.Errorf("a config declaring no paths must declare no aliases, got %v",
			facts.Resolution.Aliases)
	}
}

// TestTSConfigExtendsPackageIsKeptVerbatim covers `@tsconfig/node20/tsconfig.json`: a
// published base config, not in the repository, whose aliases are unknowable from here.
// Recorded as written so it simply finds no parent rather than being mangled into a path.
func TestTSConfigExtendsPackageIsKeptVerbatim(t *testing.T) {
	facts := ExtractTSConfig(file("tsconfig.json", `{"extends": "@tsconfig/node20/tsconfig.json"}`))
	if got := facts.Resolution.Extends; got != "@tsconfig/node20/tsconfig.json" {
		t.Errorf("extends = %q, want the specifier verbatim", got)
	}
}

// TestTSConfigMultipleTargetsKeepOrder guards the one list in Resolution that must not be
// sorted. TypeScript tries targets in order and the first that exists wins, so reordering
// them changes which directory a specifier resolves to.
func TestTSConfigMultipleTargetsKeepOrder(t *testing.T) {
	facts := ExtractTSConfig(file("tsconfig.json", `{
  "compilerOptions": {"paths": {"@x/*": ["zz/*", "aa/*"]}}
}`))
	facts.Normalize()
	got := aliasOf(t, facts, "@x/*").Targets
	if len(got) != 2 || got[0] != "zz/*" || got[1] != "aa/*" {
		t.Errorf("targets = %v, want [zz/* aa/*] in declaration order: this is a fallback "+
			"sequence, and sorting it would change what the alias resolves to", got)
	}
}

// TestTSConfigJSONC is the case that decides whether this reader works at all on real
// files. tsconfig.json is JSONC: of 14 configs in one real monorepo, both that declared
// `paths` carried comments and one closed its `paths` object with a trailing comma. A
// strict parse of either fails outright.
func TestTSConfigJSONC(t *testing.T) {
	facts := ExtractTSConfig(file("tsconfig.json", `{
  /* Visit https://aka.ms/tsconfig.json to read more about this file */
  "compilerOptions": {
    "target": "es6" /* trailing block comment */,
    // A line comment.
    "paths": {
      "@src/*": [
        "src/*"
      ],
    },
  },
}`))
	if facts.Incomplete {
		t.Fatalf("a JSONC tsconfig did not parse, so every alias in it is lost: %s", facts.Note)
	}
	if got := aliasOf(t, facts, "@src/*").Targets; len(got) != 1 || got[0] != "src/*" {
		t.Errorf("@src/* targets = %v, want [src/*]", got)
	}
}

// TestStripJSONCDoesNotTouchStrings is the trap a regex-based stripper falls into.
//
// `https://aka.ms/tsconfig.json` is a real value in a real config, and treating its `//` as
// a comment deletes the rest of that line — including whatever structure followed it. The
// same applies to an escaped quote, which does not end the string it sits in.
func TestStripJSONCDoesNotTouchStrings(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"url", `{"a": "https://x/y.json"}`, `{"a": "https://x/y.json"}`},
		{"block-in-string", `{"a": "/* not a comment */"}`, `{"a": "/* not a comment */"}`},
		{"escaped-quote", `{"a": "he said \"//\" here", "b": 1}`, `{"a": "he said \"//\" here", "b": 1}`},
	} {
		if got := StripJSONC(tc.in); got != tc.want {
			t.Errorf("%s: StripJSONC(%q) = %q, want it unchanged", tc.name, tc.in, got)
		}
	}
}

// TestStripJSONCPreservesLineNumbers checks that stripping does not move anything.
//
// Line numbers are provenance: they are what a reader follows from a bundle page back to the
// source. A stripper that removed a block comment's newlines would shift every line after it,
// so each recorded line would point somewhere the fact is not.
func TestStripJSONCPreservesLineNumbers(t *testing.T) {
	in := "{\n/* one\ntwo\nthree */\n\"paths\": {}\n}"
	got := StripJSONC(in)
	if a, b := strings.Count(in, "\n"), strings.Count(got, "\n"); a != b {
		t.Errorf("newline count changed from %d to %d, so every line after a block comment "+
			"now reports the wrong number", a, b)
	}
	if len(got) != len(in) {
		t.Errorf("length changed from %d to %d; comments must be blanked, not removed",
			len(in), len(got))
	}
}

// TestTSConfigMalformedIsIncomplete: a config that is broken beyond JSONC tolerance is
// reported as unread rather than as one with no aliases. The two are different claims, and
// the second would quietly present a resolution failure as a repository with no aliases.
func TestTSConfigMalformedIsIncomplete(t *testing.T) {
	facts := ExtractTSConfig(file("tsconfig.json", `{"compilerOptions": {`))
	if !facts.Incomplete {
		t.Error("a truncated tsconfig was reported as fully read")
	}
}
