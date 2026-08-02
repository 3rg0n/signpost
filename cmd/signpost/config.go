package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/3rg0n/signpost/internal/config"
	"github.com/3rg0n/signpost/internal/model"
)

// Where .signpost.yml is applied, and it is one place on purpose.
//
// ADR 0011 decided a single precedence order — flag > environment > file > default — and the
// way that decision gets broken is not by somebody arguing against it. It is by a second
// command reading the file at a slightly different point in its own flag handling, so that
// `build` and `graph show` disagree about whether -ignore replaces the file's patterns or
// adds to them. Every analysing command therefore calls loadConfig and applyConfig here,
// between repoPath and analyse, and nothing else reads config.Config directly.
//
// Between repoPath and analyse because the order is forced both ways: the file lives at the
// root, so the path has to be known first, and the walk's options come out of the file, so
// the file has to be read before the walk. ADR 0011's Consequences records that constraint
// because it is easy for a refactor to invert.

// loadConfig reads .signpost.yml from root.
//
// A bad file is exit 2, which is ADR 0011's decision and the opposite of the usual reflex.
// The tolerant readers in internal/manifest step over what they cannot interpret because
// they read other people's files; this one is signpost's own, written by somebody who
// expected it to take effect, so ignoring it would mean analysing the repository the way the
// file said not to and reporting success.
func loadConfig(root string) (*config.Config, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errUsage, err)
	}
	return cfg, nil
}

// applyConfig fills the shared pipeline flags from the file, for the flags this invocation
// did not pass.
//
// flag.Visit rather than comparing against the zero value, which is the only way to get this
// right: -include-vendored=false and an absent -include-vendored are the same value and must
// not be the same decision, or a file with include_vendored: true could not be turned off
// from the command line.
func applyConfig(fs *flag.FlagSet, cfg *config.Config, pf *pipelineFlags) {
	set := setFlags(fs)
	if !set["include-vendored"] && cfg.IncludeVendored {
		pf.includeVendored = true
	}
	if !set["include-fixtures"] && cfg.IncludeFixtures {
		pf.includeFixtures = true
	}
	if !set["no-history"] && cfg.NoHistory {
		pf.noHistory = true
	}
	if !set["max-commits"] && cfg.MaxCommits > 0 {
		pf.maxCommits = cfg.MaxCommits
	}
	// Replaced, not appended, and this is the one key where that is a choice rather than a
	// consequence. A union would read naturally — the flag's help says "additional" — but it
	// would also make `ignore` the single configurable key a caller cannot override, since
	// there would be no way to run without the file's patterns. One order, no exceptions per
	// key (ADR 0011), so a passed -ignore is the whole list.
	if !set["ignore"] && len(cfg.Ignore) > 0 {
		pf.ignore = cfg.Ignore
	}
}

// applyRepo fills -repo, which `build` and `verify` share and must pass identically: it feeds
// every page's `resource:`, so a verify run with a different value reports a difference that
// describes the invocation rather than the bundle. That is the argument for the key existing
// — signpost's own CI passes -repo in five places — and the argument for one function
// setting it in both commands.
func applyRepo(fs *flag.FlagSet, cfg *config.Config, repo *string) {
	if !setFlags(fs)["repo"] && cfg.Repo != "" {
		*repo = cfg.Repo
	}
}

// setFlags reports which flags this invocation actually passed.
func setFlags(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

// modelConfig resolves the backend selection across all four layers.
//
// The environment is read here rather than left to model.Config.withEnv, which is the only
// way to slot the file *under* it: withEnv treats a set field as explicit and lets it beat
// the environment, so putting a file value in that field would make .signpost.yml outrank
// SIGNPOST_BACKEND and invert ADR 0011's order. Resolving first and handing withEnv either
// the winner or an empty string keeps the order intact and leaves the default where it is.
//
// Credentials are not here and there is no layer for them: they are read from the
// environment inside model, and the file has nowhere to put one (ADR 0009).
func modelConfig(flagBackend, flagModel, baseURL, addr string, cfg *config.Config) model.Config {
	return model.Config{
		Backend: model.Kind(firstSet(flagBackend, os.Getenv(model.EnvBackend), cfg.Backend)),
		Model:   firstSet(flagModel, os.Getenv(model.EnvModel), cfg.Model),
		BaseURL: baseURL,
		Addr:    addr,
		Version: version,
	}
}

// firstSet returns the first value somebody actually set.
func firstSet(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
