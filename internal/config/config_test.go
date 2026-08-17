package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/model"
)

// write puts a .signpost.yml in a fresh directory and returns the root.
func write(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, File), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

// load reads a file that is expected to be valid.
func load(t *testing.T, body string) *Config {
	t.Helper()
	cfg, err := Load(write(t, body))
	if err != nil {
		t.Fatalf("Load(%q) = %v, want it to be read", body, err)
	}
	return cfg
}

// loadErr reads a file that is expected to be rejected, and returns the message so the caller
// can assert what it says. The message is asserted rather than just the error, because an
// error that does not say which key or which line is one somebody fixes by deleting the file.
func loadErr(t *testing.T, body string) string {
	t.Helper()
	cfg, err := Load(write(t, body))
	if err == nil {
		t.Fatalf("Load(%q) = %+v, want an error", body, cfg)
	}
	return err.Error()
}

// An absent file is the supported normal case: no error, no warning, everything zero. Asserted
// first because most repositories will never have this file and this is the path they take.
func TestAbsentFileIsNotAnError(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load on a directory with no config = %v, want no error", err)
	}
	if !isZero(cfg) {
		t.Errorf("cfg = %+v, want the zero value", *cfg)
	}
}

// isZero reports that a config states nothing. reflect.DeepEqual against Config{} rather than
// == because of the []string field, and a field-by-field comparison here would be a second
// list of keys to forget to update.
func isZero(cfg *Config) bool { return reflect.DeepEqual(*cfg, Config{}) }

// The location rule: the root and nowhere else, no walk upward. This is the clause §8.1 rests
// on — a search path is how the same checkout produces two different bundles — so it is
// asserted rather than trusted to the absence of code implementing a search.
func TestConfigIsNotReadFromAParentDirectory(t *testing.T) {
	root := write(t, "include_vendored: true\n")
	sub := filepath.Join(root, "packages", "inner")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(sub)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IncludeVendored {
		t.Error("a .signpost.yml in a parent directory was read; ADR 0011 forbids the upward walk")
	}
}

func TestEveryConfigurableKeyIsRead(t *testing.T) {
	cfg := load(t, `# every key ADR 0011 puts in the configurable class
include_vendored: true
include_fixtures: true
no_history: true
max_commits: 500
ignore:
  - generated/**
  - "*.pb.go"
repo: example.com/org/repo
backend: openai
model: google.gemma-3-12b-it
hooks:
  check: verify
`)
	if !cfg.IncludeVendored {
		t.Error("include_vendored was not read")
	}
	if !cfg.IncludeFixtures {
		t.Error("include_fixtures was not read")
	}
	if !cfg.NoHistory {
		t.Error("no_history was not read")
	}
	if cfg.MaxCommits != 500 {
		t.Errorf("MaxCommits = %d, want 500", cfg.MaxCommits)
	}
	if got, want := strings.Join(cfg.Ignore, ","), "generated/**,*.pb.go"; got != want {
		t.Errorf("Ignore = %q, want %q", got, want)
	}
	if cfg.Repo != "example.com/org/repo" {
		t.Errorf("Repo = %q, want example.com/org/repo", cfg.Repo)
	}
	if cfg.Backend != string(model.KindOpenAI) {
		t.Errorf("Backend = %q, want openai", cfg.Backend)
	}
	if cfg.Model != "google.gemma-3-12b-it" {
		t.Errorf("Model = %q, want the model id verbatim", cfg.Model)
	}
	if cfg.HooksCheck != "verify" {
		t.Errorf("HooksCheck = %q, want verify", cfg.HooksCheck)
	}
}

// The negative boundary for the above: a file that says false must produce false. Without
// this, a reader that ignored the value and set every present key to true would pass
// TestEveryConfigurableKeyIsRead.
func TestFalseIsRead(t *testing.T) {
	cfg := load(t, "include_vendored: false\ninclude_fixtures: false\nno_history: false\n")
	if cfg.IncludeVendored || cfg.IncludeFixtures || cfg.NoHistory {
		t.Errorf("cfg = %+v, want every boolean false", *cfg)
	}
}

// YAML 1.1 spellings, which is what Node.Bool accepts and therefore what this file accepts.
// Asserted here so the two cannot drift: a reader that took only `true` would read
// `no_history: yes` as false and analyse history the file asked it to skip.
func TestBooleanSpellings(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"true", true}, {"yes", true}, {"on", true}, {"y", true},
		{"false", false}, {"no", false}, {"off", false}, {"n", false},
	} {
		cfg := load(t, "include_vendored: "+tc.text+"\n")
		if cfg.IncludeVendored != tc.want {
			t.Errorf("include_vendored: %s = %v, want %v", tc.text, cfg.IncludeVendored, tc.want)
		}
	}
}

// A single pattern written as a scalar. Node.Seq treats a lone scalar as a one-element
// sequence because both spellings appear in real files, and `ignore: vendor/**` is the one
// somebody writes first.
func TestIgnoreAcceptsASingleScalar(t *testing.T) {
	cfg := load(t, "ignore: vendor/**\n")
	if got, want := strings.Join(cfg.Ignore, ","), "vendor/**"; got != want {
		t.Errorf("Ignore = %q, want %q", got, want)
	}
}

// An empty document, and a document that is only comments. Both say nothing, which is not the
// same as being wrong.
func TestEmptyDocumentIsNotAnError(t *testing.T) {
	for _, body := range []string{"", "\n\n", "# nothing configured yet\n"} {
		cfg, err := Load(write(t, body))
		if err != nil {
			t.Fatalf("Load(%q) = %v, want no error", body, err)
		}
		if !isZero(cfg) {
			t.Errorf("Load(%q) = %+v, want the zero value", body, *cfg)
		}
	}
}

// ADR 0011: an unreadable or malformed file is a usage error, never a fallback to defaults.
// Each case names what it is testing, because "malformed YAML" covers two different failures
// — the document is unparseable by anything, or the tolerant reader stepped over a line — and
// this package refuses both where internal/manifest tolerates the second.
func TestMalformedFileIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		// The Diag.Malformed cases: an unterminated flow collection loses every entry after
		// it, so no reader agrees about what the document says.
		{"unterminated flow mapping", "hooks: {check: verify\n"},
		{"unterminated flow sequence", "ignore: [vendor/**\n"},
		// The merely-Incomplete cases. internal/manifest tolerates these by design and this
		// package must not: each is a line somebody wrote that would otherwise have no effect.
		{"key with no colon", "include_vendored true\n"},
		{"tab indentation", "hooks:\n\tcheck: verify\n"},
		{"alias with no anchor", "backend: *missing\n"},
		{"helm-style template", "model: {{ .Values.model }}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if msg := loadErr(t, tc.body); !strings.Contains(msg, File) {
				t.Errorf("error does not name the file: %s", msg)
			}
		})
	}
}

// A document that parses but is not a mapping. `include_vendored` alone on a line is a scalar
// document, and reading it as an empty config would silently ignore a file somebody wrote.
func TestNonMappingDocumentIsRejected(t *testing.T) {
	for _, body := range []string{"include_vendored\n", "- include_vendored\n"} {
		if msg := loadErr(t, body); !strings.Contains(msg, "mapping") {
			t.Errorf("Load(%q) error does not say a mapping was wanted: %s", body, msg)
		}
	}
}

// ADR 0009, restated in 0011: there is nowhere to put a credential, and the reader rejects the
// key *by name* with a message saying why. Rejected rather than ignored because a silently
// ignored api_key is a secret sitting in a committed file doing nothing, which is the worst of
// both outcomes.
func TestCredentialKeysAreRejectedByName(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"the openai block", "openai:\n  api_key: sk-not-a-real-key\n"},
		{"the openai block with only a base url", "openai:\n  base_url: http://localhost:8000/v1\n"},
		{"a top-level api_key", "api_key: sk-not-a-real-key\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := loadErr(t, tc.body)
			// The message has to name the environment variable, because "not allowed" without
			// somewhere to put it instead is what makes somebody look for a workaround.
			if !strings.Contains(msg, model.EnvAPIKey) {
				t.Errorf("error does not name %s, so it does not say where the key goes: %s",
					model.EnvAPIKey, msg)
			}
			if !strings.Contains(msg, "committed") {
				t.Errorf("error does not say why: %s", msg)
			}
		})
	}
}

// ADR 0011 withdrew ${VAR} from design §5's sketch, so it is refused wherever it appears
// rather than stored verbatim — a model id of "${SIGNPOST_MODEL}" reaches the backend as a 400
// that says nothing about the config file.
func TestInterpolationIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"in a scalar", "model: ${SIGNPOST_MODEL}\n"},
		{"in a nested value", "hooks:\n  check: ${SIGNPOST_HOOK_CHECK}\n"},
		{"in a list element", "ignore:\n  - ${HOME}/generated\n"},
		{"embedded in a longer value", "repo: example.com/${ORG}/repo\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if msg := loadErr(t, tc.body); !strings.Contains(msg, "interpolation") {
				t.Errorf("error does not say interpolation is not done: %s", msg)
			}
		})
	}
}

// The negative boundary for the above: an unbraced dollar is a legal character in a gitignore
// pattern and in a repository name, so refusing it would reject files that mean what they say.
func TestUnbracedDollarIsNotInterpolation(t *testing.T) {
	cfg := load(t, "ignore:\n  - \"*$temp\"\n")
	if got, want := strings.Join(cfg.Ignore, ","), "*$temp"; got != want {
		t.Errorf("Ignore = %q, want %q", got, want)
	}
}

// The clause that erodes. ADR 0011's second and third classes are the load-bearing half of the
// decision — a repository must not be able to weaken its own gate by committing a file — and
// nothing about the code stops somebody adding one of these keys later. This test is what
// stops it: every one of them must be refused, and the refusal must say why rather than
// reading as an oversight.
func TestNonConfigurableFlagsCannotBeSet(t *testing.T) {
	for _, key := range []string{
		// Second class: decides whether a check fails.
		"as_of_bundle", "fail_on_cycle",
		// ADR 0009: whether to spend a model, as opposed to which model.
		"semantic", "semantic_timeout",
		// Third class: a property of one invocation, not repository state.
		"quiet", "verbose", "top", "all", "format", "o", "output",
		// Specified in design §5 and not built. Accepting it would be a file that looks
		// configured and is not.
		"budget",
	} {
		t.Run(key, func(t *testing.T) {
			msg := loadErr(t, key+": true\n")
			if !strings.Contains(msg, key) {
				t.Errorf("error does not name the key: %s", msg)
			}
			// "not a key this file may set" plus a reason. The reason is the part that matters:
			// somebody who wanted fail_on_cycle off needs to be told the workflow is where that
			// decision lives, or they will look for another way to do it here.
			//
			// The whole phrase, and the absence of "unknown key" beside it. A substring of
			// "may set" is in unknownKey's message too — it lists what the file may set — so
			// dropping a key from `refused` moves it to the unknown branch and satisfies a
			// looser check. That distinction is the one ADR 0011 exists for: an unknown key
			// says nothing about *why*, and the reason is what stops somebody looking for
			// another way to weaken their own gate here.
			if !strings.Contains(msg, "is not a key this file may set") {
				t.Errorf("error does not say the key is refused rather than unknown: %s", msg)
			}
			if strings.Contains(msg, "unknown key") {
				t.Errorf("the key fell through to the unknown-key branch, so it is not refused "+
					"with a reason: %s", msg)
			}
			if len(msg) < len(key)+60 {
				t.Errorf("error gives no reason: %s", msg)
			}
		})
	}
}

// Refusing `as_of_bundle` is worth nothing if `as-of-bundle` is accepted, and dashes are how
// the flags themselves are spelled. Both spellings of both classes.
func TestDashedSpellingsAreAlsoRefused(t *testing.T) {
	for _, key := range []string{"as-of-bundle", "fail-on-cycle", "include-vendored", "max-commits"} {
		msg := loadErr(t, key+": true\n")
		if !strings.Contains(msg, key) {
			t.Errorf("%s: error does not name the key: %s", key, msg)
		}
	}
}

// The one hint worth printing: a configurable key written with dashes is a near-certain typo,
// so it says the underscore spelling rather than listing every key.
func TestDashedConfigurableKeySuggestsTheUnderscoreSpelling(t *testing.T) {
	msg := loadErr(t, "include-vendored: true\n")
	if !strings.Contains(msg, `"include_vendored"`) {
		t.Errorf("error does not suggest the underscore spelling: %s", msg)
	}
}

// An unknown key is an error too, not a key silently doing nothing.
func TestUnknownKeyIsRejected(t *testing.T) {
	msg := loadErr(t, "colour: mauve\n")
	if !strings.Contains(msg, "colour") {
		t.Errorf("error does not name the key: %s", msg)
	}
	// The list of what is allowed, because "unknown key" alone does not tell somebody what to
	// write instead and this file has no documentation next to it.
	if !strings.Contains(msg, "include_vendored") || !strings.Contains(msg, "max_commits") {
		t.Errorf("error does not list the settable keys: %s", msg)
	}
}

func TestBadValuesAreRejected(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		wantMsg string
	}{
		{"non-boolean boolean", "include_vendored: maybe\n", "true or false"},
		{"non-numeric max_commits", "max_commits: lots\n", "positive"},
		// Zero and negative are refused rather than taken, because vcs treats MaxCommits <= 0
		// as "apply the default" — so a file asking for zero commits would silently get 2000,
		// which is the opposite of what it asked for.
		{"zero max_commits", "max_commits: 0\n", "positive"},
		{"negative max_commits", "max_commits: -1\n", "positive"},
		{"empty repo", "repo: \n", "want a value"},
		{"unknown backend", "backend: ollama\n", "want inferd"},
		{"unknown hooks.check", "hooks:\n  check: quick\n", "want fast"},
		{"unknown key under hooks", "hooks:\n  install: true\n", "only key under hooks is check"},
		{"hooks as a scalar", "hooks: verify\n", "a mapping"},
		{"ignore as a mapping", "ignore:\n  vendor: true\n", "list of .gitignore"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if msg := loadErr(t, tc.body); !strings.Contains(msg, tc.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", msg, tc.wantMsg)
			}
		})
	}
}

// Every error names a line, because a config error somebody cannot locate is one they fix by
// deleting the file. The key is on line 3 in each case, so the number is a real lookup rather
// than a constant.
func TestErrorsNameTheLine(t *testing.T) {
	for _, key := range []string{"colour: mauve", "as_of_bundle: true", "max_commits: lots"} {
		msg := loadErr(t, "# a comment\nrepo: example.com/org/repo\n"+key+"\n")
		if !strings.Contains(msg, "line 3") {
			t.Errorf("error does not locate line 3: %s", msg)
		}
	}
}

// The wrong extension is a real mistake with a silent failure mode: .signpost.yaml is a file
// signpost would never open, so its author would conclude the tool ignores configuration.
func TestTheOtherExtensionIsReported(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, altFile), []byte("repo: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root)
	if err == nil {
		t.Fatal("a .signpost.yaml was ignored silently")
	}
	if !strings.Contains(err.Error(), File) {
		t.Errorf("error does not name the spelling that works: %v", err)
	}
}

// A directory named .signpost.yml. Not a plausible mistake, but ReadFile fails with an
// unhelpful errno and Load must still name the file rather than passing the raw error up.
func TestUnreadableFileIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, File), 0o750); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root)
	if err == nil {
		t.Fatal("an unreadable config was accepted")
	}
	if !strings.Contains(err.Error(), File) {
		t.Errorf("error does not name the file: %v", err)
	}
}

// The first error wins and reading stops. Not a preference: reporting six problems from a file
// where the first one may have caused the rest is a wall of text, and a person fixes them one
// at a time anyway.
func TestTheFirstBadKeyIsReported(t *testing.T) {
	msg := loadErr(t, "as_of_bundle: true\ncolour: mauve\n")
	if !strings.Contains(msg, "as_of_bundle") {
		t.Errorf("error does not report the first bad key: %s", msg)
	}
	if strings.Contains(msg, "colour") {
		t.Errorf("error reports more than the first problem: %s", msg)
	}
}

// The refused list and the settable list must not overlap. A key in both would be accepted or
// refused depending on which map `set` consults first, which is a decision nobody made.
func TestNoKeyIsBothSettableAndRefused(t *testing.T) {
	for k := range keys {
		if _, ok := refused[k]; ok {
			t.Errorf("%q is in both keys and refused", k)
		}
	}
}

// Every refusal has to carry a reason. An entry added later with an empty or one-word value
// would produce "x is not a key this file may set:" and read as a bug.
func TestEveryRefusalGivesAReason(t *testing.T) {
	for k, why := range refused {
		if len(why) < 40 {
			t.Errorf("refused[%q] = %q, too short to be a reason", k, why)
		}
	}
}
