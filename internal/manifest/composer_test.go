package manifest

import (
	"strings"
	"testing"
)

func TestComposerExtraction(t *testing.T) {
	facts := ExtractComposer(file("composer.json", `{
  "name": "acme/ordering",
  "version": "2.1.0",
  "require": {
    "php": ">=8.2",
    "ext-json": "*",
    "lib-openssl": "*",
    "symfony/console": "^7.0",
    "psr/log": "^3.0"
  },
  "require-dev": {
    "phpunit/phpunit": "^11.0"
  },
  "autoload": {
    "psr-4": {
      "Acme\\Ordering\\": "src/",
      "Acme\\Shared\\": ["src/Shared/", "lib/Shared/"]
    },
    "psr-0": {
      "Legacy_": "legacy/"
    },
    "classmap": ["database/seeds"],
    "files": ["src/helpers.php"]
  },
  "autoload-dev": {
    "psr-4": {
      "Acme\\Tests\\": "tests/"
    }
  },
  "scripts": {
    "test": "phpunit",
    "lint": ["php-cs-fixer fix --dry-run", "phpstan analyse"]
  },
  "bin": ["bin/ordering"]
}
`))
	facts.Normalize()

	if facts.Module.Name != "acme/ordering" || facts.Module.Version != "2.1.0" {
		t.Errorf("module = %+v", facts.Module)
	}
	// PHP's version arrives as a dependency named `php`, and it is the runtime — lifted to
	// LangVersion where every other reader puts the toolchain floor.
	if facts.Module.LangVersion != ">=8.2" {
		t.Errorf("lang version = %q", facts.Module.LangVersion)
	}
	// Platform requirements are checked by composer and installed by nobody. A reference
	// page for `ext-json` would be a page for a compiled extension.
	noDep(t, facts, "php")
	noDep(t, facts, "ext-json")
	noDep(t, facts, "lib-openssl")

	if d := depOf(t, facts, "symfony/console", ScopeRuntime); d.Version != "^7.0" {
		t.Errorf("console version = %q", d.Version)
	}
	depOf(t, facts, "psr/log", ScopeRuntime)
	depOf(t, facts, "phpunit/phpunit", ScopeDev)

	// The autoload map is the load-bearing fact: without it every first-party PHP import
	// in the repository is unresolved, because nothing else states that `Acme\Ordering\`
	// begins at src/.
	if got := aliasTargets(facts, `Acme\Ordering\`); got != "src" {
		t.Errorf("psr-4 Acme\\Ordering\\ -> %q", got)
	}
	// A namespace split across two directories is one prefix with two targets; composer
	// tries each, so dropping either would leave half its classes unresolvable.
	if got := aliasTargets(facts, `Acme\Shared\`); got != "src/Shared,lib/Shared" {
		t.Errorf("psr-4 Acme\\Shared\\ -> %q", got)
	}
	// PSR-0 is superseded but still present in long-lived projects, and it states the same
	// prefix-to-directory fact.
	if got := aliasTargets(facts, "Legacy_"); got != "legacy" {
		t.Errorf("psr-0 Legacy_ -> %q", got)
	}
	// autoload-dev carries the test namespace, which is where a repository's test imports
	// resolve — unread, every one of them is a gap.
	if got := aliasTargets(facts, `Acme\Tests\`); got != "tests" {
		t.Errorf("autoload-dev Acme\\Tests\\ -> %q", got)
	}

	if got := scriptCommand(facts, "test"); got != "phpunit" {
		t.Errorf("test script = %q", got)
	}
	// A list of commands runs in order, which is what joining with && says.
	if got := scriptCommand(facts, "lint"); got != "php-cs-fixer fix --dry-run && phpstan analyse" {
		t.Errorf("lint script = %q", got)
	}
	if len(facts.Entrypoints) != 1 || facts.Entrypoints[0].Path != "bin/ordering" {
		t.Errorf("entrypoints = %+v", facts.Entrypoints)
	}
}

// `classmap` and `files` name directories with no namespace prefix at all — composer scans
// them and builds the map itself. Recording one as an alias would give it an empty pattern,
// and an empty pattern is a prefix of every import in the repository.
func TestComposerSkipsPrefixlessAutoload(t *testing.T) {
	facts := ExtractComposer(file("composer.json", `{
  "autoload": {
    "classmap": ["database/"],
    "files": ["src/helpers.php"]
  }
}
`))
	facts.Normalize()
	for _, a := range facts.Resolution.Aliases {
		if a.Pattern == "" {
			t.Fatalf("an empty alias pattern claims every import: %+v", a)
		}
	}
	if len(facts.Resolution.Aliases) != 0 {
		t.Errorf("aliases = %+v, want none from classmap or files", facts.Resolution.Aliases)
	}
}

// A composer.json nested in a package directory maps its namespace onto a directory beside
// itself, not beside the repository root. Resolving it here is the only place that knows
// where the file sat.
func TestComposerAutoloadIsRelativeToTheManifest(t *testing.T) {
	facts := ExtractComposer(file("packages/ordering/composer.json", `{
  "name": "acme/ordering",
  "autoload": {"psr-4": {"Acme\\Ordering\\": "src/"}}
}
`))
	facts.Normalize()
	if got := aliasTargets(facts, `Acme\Ordering\`); got != "packages/ordering/src" {
		t.Errorf("target = %q, want it resolved against the manifest's directory", got)
	}
}

// Composer accepts `repositories` as an array or as an object keyed by a name a human
// chose, and both spellings are in wide use. A reader that handled one would report a
// monorepo's members as absent.
func TestComposerPathRepositoriesInBothSpellings(t *testing.T) {
	array := ExtractComposer(file("composer.json", `{
  "repositories": [
    {"type": "path", "url": "packages/*"},
    {"type": "vcs", "url": "https://github.com/acme/other.git"}
  ]
}
`))
	array.Normalize()
	if got := strings.Join(array.Module.Workspaces, ","); got != "packages/*" {
		t.Errorf("array form workspaces = %q, want only the path repo", got)
	}

	object := ExtractComposer(file("composer.json", `{
  "repositories": {
    "local": {"type": "path", "url": "engines/billing"},
    "packagist": {"type": "composer", "url": "https://packagist.org"}
  }
}
`))
	object.Normalize()
	if got := strings.Join(object.Module.Workspaces, ","); got != "engines/billing" {
		t.Errorf("object form workspaces = %q", got)
	}
}

// A composer.json that does not parse must keep whatever it was: half a dependency list
// read as a whole one is the most misleading outcome a manifest reader has.
func TestComposerMalformedIsRecordedNotFatal(t *testing.T) {
	facts := ExtractComposer(file("composer.json", `{"name": "a", "require": {`))
	if !facts.Incomplete {
		t.Error("an unparseable composer.json must be marked incomplete")
	}
}

// aliasTargets returns one alias's targets joined, or "" if the pattern is absent.
func aliasTargets(f Facts, pattern string) string {
	for _, a := range f.Resolution.Aliases {
		if a.Pattern == pattern {
			return strings.Join(a.Targets, ",")
		}
	}
	return ""
}

func scriptCommand(f Facts, name string) string {
	for _, s := range f.Scripts {
		if s.Name == name {
			return s.Command
		}
	}
	return ""
}
