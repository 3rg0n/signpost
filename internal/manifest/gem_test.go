package manifest

import (
	"strings"
	"testing"
)

func TestGemfileExtraction(t *testing.T) {
	facts := ExtractGem(file("Gemfile", `source "https://rubygems.org"

ruby "3.3.0"

gem "rails", ">= 7.0", "< 8.0"
gem "puma", "~> 6.4"
gem "redis"
gem "corpus-core", path: "vendor/corpus-core"
gem "acme-client", git: "https://github.com/acme/acme-client.git", branch: "main"
gem "sidekiq-pro", "1.2", optional: true

group :development, :test do
  gem "rspec-rails"
  gem "factory_bot_rails"
end

group :test do
  gem "webmock"
end

gem "listen", group: :development

platforms :jruby do
  gem "activerecord-jdbc-adapter"
end
`))
	facts.Normalize()

	if facts.Module.LangVersion != "3.3.0" {
		t.Errorf("ruby version = %q", facts.Module.LangVersion)
	}
	// Two positional constraints are one requirement expressed as a range. Either half
	// alone is a different requirement, which is why they are joined rather than reduced.
	if d := depOf(t, facts, "rails", ScopeRuntime); d.Version != ">= 7.0, < 8.0" {
		t.Errorf("rails version = %q, want both constraints", d.Version)
	}
	if d := depOf(t, facts, "puma", ScopeRuntime); d.Version != "~> 6.4" {
		t.Errorf("puma version = %q", d.Version)
	}
	// A gem with no constraint is still a declared dependency; the empty version says
	// "any", which is a fact about the repository rather than a gap in the reading.
	if d := depOf(t, facts, "redis", ScopeRuntime); d.Version != "" {
		t.Errorf("redis version = %q, want empty", d.Version)
	}
	// A path dependency is this repository's own code, resolved against the Gemfile.
	// Local is what keeps it off the reference index.
	d := depOf(t, facts, "corpus-core", ScopeRuntime)
	if !d.Local || d.Source != "vendor/corpus-core" {
		t.Errorf("path gem = %+v, want a local dep at vendor/corpus-core", d)
	}
	// A git dependency has no registry to publish an advisory against, and is not local.
	d = depOf(t, facts, "acme-client", ScopeRuntime)
	if d.Local || d.Source != "https://github.com/acme/acme-client.git" {
		t.Errorf("git gem = %+v", d)
	}
	if d := depOf(t, facts, "sidekiq-pro", ScopeRuntime); !d.Optional || d.Version != "1.2" {
		t.Errorf("optional gem = %+v", d)
	}
	// A group block scopes every gem inside it, and the `end` must pop it — a gem after
	// the block is at runtime again.
	depOf(t, facts, "rspec-rails", ScopeDev)
	depOf(t, facts, "factory_bot_rails", ScopeDev)
	depOf(t, facts, "webmock", ScopeDev)
	// The inline `group:` option means what the block means.
	depOf(t, facts, "listen", ScopeDev)
	// A non-group block pushes the scope it inherited, so a gem inside `platforms` stays
	// at runtime rather than adopting whatever the last group block said.
	depOf(t, facts, "activerecord-jdbc-adapter", ScopeRuntime)
	// `source` is a registry, not a dependency.
	noDep(t, facts, "https://rubygems.org")
}

// The block stack is the one piece of state in this reader, and a mismatched `end` is how
// it goes wrong: a group's scope leaking past its own block silently marks production
// dependencies as development-only, which reads as a smaller runtime surface than the
// repository has.
func TestGemfileBlockScopesDoNotLeak(t *testing.T) {
	facts := ExtractGem(file("Gemfile", `group :test do
  gem "rspec"
  platforms :ruby do
    gem "pg"
  end
end

gem "rails"
`))
	facts.Normalize()
	depOf(t, facts, "rspec", ScopeDev)
	// Nested inside a dev group, so it inherits dev — the inner block pushes what it was
	// given rather than resetting.
	depOf(t, facts, "pg", ScopeDev)
	// Both `end`s popped, so this is back at runtime. If the inner `end` had popped the
	// group, rails would be dev.
	depOf(t, facts, "rails", ScopeRuntime)
}

// A `git:` URL contains a colon, and an option scan that looked for the first colon on the
// line would truncate the version arguments there. The URL is also the only place a `path`
// substring can appear without being the path option.
func TestGemfileOptionBoundaries(t *testing.T) {
	facts := ExtractGem(file("Gemfile", `gem "a", "~> 1.0", git: "https://example.com:8443/a.git"
gem "b", group: :assets, path: "vendor/test-helpers"
gem "c", require: false, group: :development
`))
	facts.Normalize()

	if d := depOf(t, facts, "a", ScopeRuntime); d.Version != "~> 1.0" {
		t.Errorf("version = %q, want the URL's colon not to end the arguments", d.Version)
	}
	// `group: :assets` is not a development group, and the word "test" in the *path* must
	// not scope it — which is what stopping the option's text at the next comma buys.
	d := depOf(t, facts, "b", ScopeRuntime)
	if d.Source != "vendor/test-helpers" || !d.Local {
		t.Errorf("b = %+v", d)
	}
	// `require: false` contains no group name; the later `group:` is the one that decides.
	depOf(t, facts, "c", ScopeDev)
}

// A Gemfile routinely wraps one declaration over several lines, one option per line. Read
// line-by-line, the options would be discarded and the gem would look unscoped and
// unversioned — which is a wrong reading, not a missing one.
func TestGemfileContinuationLines(t *testing.T) {
	facts := ExtractGem(file("Gemfile", `gem "corpus",
    "~> 2.1",
    path: "engines/corpus",
    require: false

gem "rails"
`))
	facts.Normalize()
	d := depOf(t, facts, "corpus", ScopeRuntime)
	if d.Version != "~> 2.1" || d.Source != "engines/corpus" || !d.Local {
		t.Errorf("wrapped gem = %+v", d)
	}
	depOf(t, facts, "rails", ScopeRuntime)
}

func TestGemspecExtraction(t *testing.T) {
	facts := ExtractGem(file("corpus.gemspec", `# frozen_string_literal: true

require_relative "lib/corpus/version"

Gem::Specification.new do |spec|
  spec.name = "corpus"
  spec.version = Corpus::VERSION
  spec.required_ruby_version = ">= 3.1.0"
  spec.executables = %w[corpus corpus-verify]
  spec.bindir = "exe"

  spec.add_dependency "thor", "~> 1.3"
  spec.add_runtime_dependency "zeitwerk", ">= 2.6", "< 3.0"
  spec.add_development_dependency "rspec", "~> 3.13"
end
`))
	facts.Normalize()

	if facts.Module.Name != "corpus" {
		t.Errorf("gem name = %q", facts.Module.Name)
	}
	// `spec.version = Corpus::VERSION` names a constant this reader cannot resolve, and
	// recording the constant's text would present "Corpus::VERSION" as a version.
	if facts.Module.Version != "" {
		t.Errorf("version = %q, want a computed version left unread", facts.Module.Version)
	}
	if facts.Module.LangVersion != ">= 3.1.0" {
		t.Errorf("required ruby = %q", facts.Module.LangVersion)
	}
	depOf(t, facts, "thor", ScopeRuntime)
	if d := depOf(t, facts, "zeitwerk", ScopeRuntime); d.Version != ">= 2.6, < 3.0" {
		t.Errorf("zeitwerk version = %q", d.Version)
	}
	depOf(t, facts, "rspec", ScopeDev)

	var names []string
	for _, e := range facts.Entrypoints {
		names = append(names, e.Name+"@"+e.Path)
	}
	if got := strings.Join(names, ","); got != "corpus@exe/corpus,corpus-verify@exe/corpus-verify" {
		t.Errorf("entrypoints = %q", got)
	}
}

// The gemspec's name is required to match its filename, so the filename is a fact rather
// than a guess — which is what makes `spec.name = GEM_NAME` readable at all.
func TestGemspecNameFallsBackToFilename(t *testing.T) {
	facts := ExtractGem(file("gems/corpus-core.gemspec", `GEM_NAME = "corpus-core"
Gem::Specification.new do |s|
  s.name = GEM_NAME
  s.add_dependency "thor"
end
`))
	facts.Normalize()
	if facts.Module.Name != "corpus-core" {
		t.Errorf("name = %q, want the filename", facts.Module.Name)
	}
	depOf(t, facts, "thor", ScopeRuntime)
}

// A gem's Gemfile is usually the single line `gemspec`, and the gemspec beside it is
// discovered and read on its own. Following the directive here would record every
// dependency twice, which would double the repository's reported dependency count.
func TestGemfileGemspecDirectiveIsNotFollowed(t *testing.T) {
	facts := ExtractGem(file("Gemfile", `source "https://rubygems.org"

gemspec

gem "rake", "~> 13.0"
`))
	facts.Normalize()
	if len(facts.Deps) != 1 {
		t.Fatalf("deps = %v, want only the Gemfile's own gem", facts.DepNames())
	}
	depOf(t, facts, "rake", ScopeRuntime)
}

// Comments and a computed version are the two shapes this reader knowingly cannot resolve,
// and neither may become an invented dependency.
func TestGemfileIgnoresCommentsAndComputedVersions(t *testing.T) {
	facts := ExtractGem(file("Gemfile", `# gem "commented-out"
gem "rails" # the framework
gem "puma", ENV.fetch("PUMA_VERSION", "6.4")
`))
	facts.Normalize()
	noDep(t, facts, "commented-out")
	depOf(t, facts, "rails", ScopeRuntime)
	// The name is known and the version is not. Recording the fallback string inside the
	// ENV call as the version would be a guess dressed as a constraint — but it *is* a
	// quoted literal on the line, so this pins what the reader actually does with it.
	if d := depOf(t, facts, "puma", ScopeRuntime); d.Version != "" {
		t.Errorf("computed version = %q, want nothing claimed", d.Version)
	}
}

// A gemspec that does not say where its executables live gets RubyGems' own default, so
// the path is stated rather than left half-known.
func TestGemBindirDefaults(t *testing.T) {
	facts := ExtractGem(file("corpus.gemspec", `Gem::Specification.new do |s|
  s.executables = ["corpus"]
end
`))
	facts.Normalize()
	if len(facts.Entrypoints) != 1 || facts.Entrypoints[0].Path != "exe/corpus" {
		t.Errorf("entrypoints = %+v, want RubyGems' exe/ default", facts.Entrypoints)
	}
}
