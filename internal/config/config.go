// Package config reads .signpost.yml, the one file a repository uses to state how it wants
// to be analysed.
//
// [ADR 0011] decides three things this package exists to enforce, and each of them is a rule
// about what a file may *not* do:
//
//   - It is read from the repository root and nowhere else. No user-level file, no
//     XDG_CONFIG_HOME, no --config pointing outside the tree, no walk upward. A config
//     search path is how the same checkout starts producing different bundles for two
//     people, and the byte-stability the committed bundle rests on (design §8.1) does not
//     survive that.
//   - A key may only change a default. Anything that decides whether a check *fails* —
//     as_of_bundle, fail_on_cycle, a threshold — stays a flag, because a repository that can
//     weaken its own gate by committing a file is not gated. Those keys are refused by name.
//   - There is nowhere to put a credential ([ADR 0009]). The file is committed, and a format
//     with a place for an API key is a format that eventually has one in it.
//
// # Nothing here is tolerant
//
// That is the difference between this reader and the ones in internal/manifest, and it is
// worth stating because the two share a parser. Those readers step over what they cannot
// interpret, deliberately (ADR 0001): they read files other people wrote for other tools, and
// one unusual Helm template must not fail a build. This file is signpost's own, written for
// signpost, by somebody who expected it to have an effect. So *any* diagnostic is a usage
// error — not only the malformed ones. `include_vendored true`, missing its colon, is a line
// the tolerant reader notes and steps over, and stepping over it would mean the run analysed
// the repository the way the file said not to while reporting success.
//
// [ADR 0011]: ../../docs/adr/0011-configuration-file-format-and-location.md
// [ADR 0009]: ../../docs/adr/0009-the-semantic-pass-is-opt-in-and-egress-is-explicit.md
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/3rg0n/signpost/internal/hook"
	"github.com/3rg0n/signpost/internal/manifest"
	"github.com/3rg0n/signpost/internal/model"
)

// File is the name, at the repository root and nowhere else.
const File = ".signpost.yml"

// altFile is the other spelling, which is not accepted and is not ignored either.
//
// A file named .signpost.yaml is somebody's configuration that signpost would never read,
// and silence about it is the failure this package is written against — so its presence is
// an error naming the spelling that works. One name rather than two, because two names is
// two files that can both exist and disagree.
const altFile = ".signpost.yaml"

// Config is what a repository may say about its own analysis.
//
// Every field sets the *default* for a flag, and each one is a fact about the repository that
// is the same for every caller — which is the test ADR 0011 applies to decide whether a key
// may exist at all. A zero field means the file did not say, and the flag's own default
// stands: for the booleans that coincides with false, and for MaxCommits with vcs's default,
// so there is no need to record which keys were present.
type Config struct {
	IncludeVendored bool
	IncludeFixtures bool
	Ignore          []string
	NoHistory       bool
	MaxCommits      int
	Repo            string
	Backend         string
	Model           string

	// HooksCheck is `hooks.check`, the local hook's check mode. The one nested key, and the
	// reason for the nesting: `hooks` names a command group, so a flat `check:` would be a
	// key whose meaning depends on knowing which command reads it.
	HooksCheck string
}

// Load reads the file from root.
//
// An absent file is the supported normal case and returns an empty Config with no error — not
// a warning, since most repositories will never have one. Everything else that goes wrong is
// an error the caller maps to exit 2.
func Load(root string) (*Config, error) {
	if _, err := os.Stat(filepath.Join(root, altFile)); err == nil {
		return nil, fmt.Errorf("%s is named %s, and this one is not read", altFile, File)
	}
	src, err := os.ReadFile(filepath.Join(root, File)) // #nosec G304 -- a fixed name under the root being analysed
	if errors.Is(err, fs.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %v", File, err)
	}

	doc, diag := manifest.ParseYAMLDoc(string(src))
	// Incomplete rather than Malformed. Malformed means the YAML is unreadable by anything;
	// Incomplete also covers a construct the reader stepped over, which for this file is a
	// line somebody wrote that would have had no effect. See the package comment.
	if diag.Incomplete() || diag.Malformed {
		return nil, fmt.Errorf("%s: %s", File, diag.Summary())
	}
	if doc == nil {
		// Empty, or nothing but comments. A file stating no keys states no keys.
		return &Config{}, nil
	}
	if doc.Kind != manifest.KindMap {
		return nil, fmt.Errorf("%s: line %d: the document is a %s, and this file is a mapping "+
			"of keys to values", File, doc.Line, kindName(doc.Kind))
	}

	if err := rejectInterpolation(doc); err != nil {
		return nil, err
	}

	cfg := &Config{}
	var bad error
	doc.Each(func(key string, val *manifest.Node) bool {
		bad = cfg.set(key, val)
		return bad == nil
	})
	if bad != nil {
		return nil, bad
	}
	return cfg, nil
}

// rejectInterpolation refuses ${VAR} anywhere in the document.
//
// ADR 0011 withdrew it: design §5's sketch showed `api_key: ${SIGNPOST_OPENAI_API_KEY}`, and an
// interpolation syntax exists mainly to put secrets in files. There is nothing to expand it
// against here and nothing that wants to be.
//
// Rejected rather than left alone, even though no key needs expansion, because the failure of
// leaving it alone is silent: `model: ${SIGNPOST_MODEL}` would be stored verbatim and sent to
// the backend as a model id, and the resulting 400 says nothing about the config file. A whole
// document scan rather than a per-key check so a key added later inherits it.
func rejectInterpolation(n *manifest.Node) error {
	if n == nil {
		return nil
	}
	switch n.Kind {
	case manifest.KindScalar:
		// `$VAR` unbraced is not matched: it is a legal character in a gitignore pattern and in
		// a repository name, and refusing it would reject files that mean exactly what they say.
		if strings.Contains(n.Str, "${") {
			return fmt.Errorf("%s: line %d: %q looks like variable interpolation, which this "+
				"file does not do: values are used as written. Credentials belong in the "+
				"environment (%s), not in a committed file", File, n.Line, n.Str, model.EnvAPIKey)
		}
	case manifest.KindMap:
		for _, v := range n.Vals {
			if err := rejectInterpolation(v); err != nil {
				return err
			}
		}
	case manifest.KindSeq:
		for _, v := range n.Items {
			if err := rejectInterpolation(v); err != nil {
				return err
			}
		}
	}
	return nil
}

// setter reads one key's value onto a Config.
type setter func(c *Config, key string, val *manifest.Node) error

// keys is every key a file may set, and it is the only list of them: the reader dispatches
// through this map and the unknown-key message is built from it, so a key cannot be
// half-added — accepted by the reader and missing from what the error says exists.
var keys = map[string]setter{
	"include_vendored": func(c *Config, k string, v *manifest.Node) error {
		return boolKey(k, v, &c.IncludeVendored)
	},
	"include_fixtures": func(c *Config, k string, v *manifest.Node) error {
		return boolKey(k, v, &c.IncludeFixtures)
	},
	"no_history": func(c *Config, k string, v *manifest.Node) error {
		return boolKey(k, v, &c.NoHistory)
	},
	"max_commits": func(c *Config, k string, v *manifest.Node) error {
		n, ok := v.Int()
		if !ok || n <= 0 {
			return keyErr(k, v, "a positive number of commits")
		}
		c.MaxCommits = n
		return nil
	},
	"ignore": func(c *Config, k string, v *manifest.Node) error {
		// Strings drops non-scalars, so a nested mapping under a list entry would silently
		// shorten the list rather than be reported. Compared against the length instead.
		pats := v.Strings()
		if len(pats) == 0 || len(pats) != v.Len() && v.Kind != manifest.KindScalar {
			return keyErr(k, v, "a list of .gitignore-syntax patterns")
		}
		c.Ignore = pats
		return nil
	},
	"repo": func(c *Config, k string, v *manifest.Node) error {
		return stringKey(k, v, &c.Repo)
	},
	"backend": func(c *Config, k string, v *manifest.Node) error {
		// Validated here rather than left to model.New, because build only builds a backend
		// when -semantic is passed: a typo in the file would otherwise sit unreported until
		// the scheduled workflow that needs it ran. model owns the names, so it owns the
		// check — one list, not two that agree by coincidence.
		kind, err := model.ParseKind(v.String())
		if err != nil {
			return fmt.Errorf("%s: line %d: %s: %v", File, lineOf(v), k, err)
		}
		c.Backend = string(kind)
		return nil
	},
	"model": func(c *Config, k string, v *manifest.Node) error {
		// Passed through verbatim, deliberately: §5 measured that Bedrock rejects the
		// `:0`-suffixed and `global.`-prefixed spellings of a working id, so any
		// normalisation here would rewrite a working id into a 400.
		return stringKey(k, v, &c.Model)
	},
	"hooks": (*Config).setHooks,
}

// set reads one top-level key.
func (c *Config) set(key string, val *manifest.Node) error {
	if fn, ok := keys[key]; ok {
		return fn(c, key, val)
	}
	if why, ok := refused[key]; ok {
		return fmt.Errorf("%s: line %d: %s is not a key this file may set: %s",
			File, lineOf(val), key, why)
	}
	return unknownKey(key, lineOf(val))
}

// setHooks reads the `hooks` mapping, which holds exactly one key.
func (c *Config) setHooks(_ string, val *manifest.Node) error {
	if val.Kind != manifest.KindMap {
		return keyErr("hooks", val, "a mapping, with a check: key under it")
	}
	var bad error
	val.Each(func(k string, v *manifest.Node) bool {
		if k != "check" {
			// Its own message rather than unknownKey's, which lists the top-level keys and would
			// suggest `repo` belongs under `hooks:`. One key here, so naming it is the whole hint.
			bad = fmt.Errorf("%s: line %d: unknown key hooks.%s; the only key under hooks is check",
				File, lineOf(v), k)
			return false
		}
		// hook owns the mode names and the message that names the alternatives, for the same
		// reason model owns the backend names above.
		mode, err := hook.ParseCheck(v.String())
		if err != nil {
			bad = fmt.Errorf("%s: line %d: hooks.check: %v", File, lineOf(v), err)
			return false
		}
		c.HooksCheck = string(mode)
		return true
	})
	return bad
}

// refused names the keys a file may not set, and says why for each one.
//
// Refused rather than ignored, which is the whole point. Somebody who writes
// `as_of_bundle: true` believes they have configured something; a tool that reads the file,
// silently does the opposite, and exits 0 has told them their gate is what they asked for
// when it is not. ADR 0011's second and third classes, key by key — the second because it
// decides whether a check fails, the third because it is a property of one invocation rather
// than of the repository.
//
// The spellings are the flags' own with dashes turned to underscores, because that is what
// somebody reaching for one will type.
var refused = map[string]string{
	"as_of_bundle": "it decides whether verify fails. verify's severity model is a contract " +
		"with CI, not a repository preference — a repository that can quiet its own gate by " +
		"committing a file is not gated. Pass -as-of-bundle in the workflow that wants it, " +
		"where the change is reviewed as CI",
	"fail_on_cycle": "it decides whether a check fails, like as_of_bundle: the workflow that " +
		"wants it passes -fail-on-cycle, and stopping is a change to CI rather than to " +
		"repository data",
	"semantic": "the semantic pass spends tokens and sends source to a backend, so it is a " +
		"decision one run makes and never a default a file sets (ADR 0009). Pass -semantic. " +
		"`backend` and `model` are configurable — which model, not whether to call it",
	"semantic_timeout": "it bounds one run of the semantic pass, which -semantic turns on per " +
		"invocation. Pass -semantic-timeout beside it",
	"quiet": "it is a property of one invocation, not of the repository. A file setting it " +
		"would make every developer's terminal omit the coverage report for reasons in a file " +
		"they did not read, and design §4.2 is that unmeasured must not read as measured",
	"verbose": "it is a property of one invocation. Pass -verbose",
	"top":     "it is a property of one invocation: how many hubs this reader wanted listed",
	"all": "it is a property of one invocation: whether this reader wanted every finding " +
		"listed in full or the terminal-sized report. A file setting it would make a person's " +
		"terminal print sixty lines of bundle paths for reasons in a file they did not read. " +
		"Pass -all",
	"format": "it is a property of one invocation: what this caller wanted rendered",
	"o":      "it is a property of one invocation: where this caller wanted the output",
	"output": "it is a property of one invocation: where this caller wanted the output",
	"openai": "credentials are read from the environment and never from a file (ADR 0009). " +
		"openai.api_key does not exist: the file is committed, and a format with a place for " +
		"an API key is a format that eventually has one in it. Set " + model.EnvAPIKey +
		", and " + model.EnvBaseURL + " for the endpoint",
	"api_key": "credentials are read from the environment and never from a file (ADR 0009): " +
		"set " + model.EnvAPIKey + ". The file is committed",
	"budget": "the semantic pass's call and token caps are sketched in design §5 and not " +
		"built. Nothing reads this key yet, so accepting it would be a file that looks " +
		"configured and is not",
}

// unknownKey reports a key nothing reads, and guesses at the one mistake worth guessing at.
//
// The underscore hint is not decoration: every one of these keys is a flag spelled with
// dashes, so `include-vendored` is what a person types from memory, and the tolerant reader
// would otherwise hand back a config that looks accepted and does nothing.
func unknownKey(key string, line int) error {
	if alt := strings.ReplaceAll(key, "-", "_"); alt != key {
		if _, ok := keys[alt]; ok {
			return fmt.Errorf("%s: line %d: unknown key %q; the keys use underscores, so this "+
				"one is %q", File, line, key, alt)
		}
	}
	return fmt.Errorf("%s: line %d: unknown key %q; this file may set %s",
		File, line, key, strings.Join(known(), ", "))
}

// known lists the settable keys, sorted so the message does not depend on map order.
func known() []string {
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func boolKey(key string, val *manifest.Node, dst *bool) error {
	b, ok := val.Bool()
	if !ok {
		return keyErr(key, val, "true or false")
	}
	*dst = b
	return nil
}

func stringKey(key string, val *manifest.Node, dst *string) error {
	if val.Kind != manifest.KindScalar || strings.TrimSpace(val.String()) == "" {
		return keyErr(key, val, "a value")
	}
	*dst = strings.TrimSpace(val.String())
	return nil
}

// keyErr says what a key wanted and what it got, with the line, because a config error a
// person cannot locate is one they fix by deleting the file.
func keyErr(key string, val *manifest.Node, want string) error {
	if got := val.String(); got != "" {
		return fmt.Errorf("%s: line %d: %s: want %s, got %q", File, lineOf(val), key, want, got)
	}
	return fmt.Errorf("%s: line %d: %s: want %s, got a %s",
		File, lineOf(val), key, want, kindName(val.Kind))
}

func lineOf(val *manifest.Node) int {
	if val == nil {
		return 0
	}
	return val.Line
}

func kindName(k manifest.NodeKind) string {
	switch k {
	case manifest.KindMap:
		return "mapping"
	case manifest.KindSeq:
		return "list"
	}
	return "value"
}
