package manifest

import (
	"path"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// ExtractComposer reads a composer.json.
//
// Two facts come out of this file and the second is the one that makes it load-bearing:
//
//   - The dependencies, in the shape every other manifest states them.
//   - The **autoload map**, which is what makes a PHP `use` mean a file. `"App\\": "src/"`
//     is the only statement anywhere that `use App\Domain\Order` names `src/Domain/Order.php`
//     — no path convention supplies it, and the same project could map `App\` onto `lib/`
//     with nothing in the source changing. This is ADR 0017's case, the same one tsconfig's
//     `paths` is, and without it every first-party PHP import in a repository is unresolved.
//
// The map lands in Resolution.Aliases, reusing tsconfig's shape rather than adding a field:
// both are "a prefix, and the directories it maps onto", and the resolver reads the two
// through the same struct. The Pattern is the namespace prefix as written, backslashes
// included, and each Target is repo-relative — resolved against this file's directory here,
// because this is the only place that knows where it sat.
func ExtractComposer(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindComposer}
	root, err := ParseJSON(f.Content)
	if err != nil {
		facts.markIncomplete("composer.json did not parse: " + err.Error())
		return facts
	}

	facts.Module = Module{
		Name:      root.Get("name").String(),
		Version:   root.Get("version").String(),
		Ecosystem: "composer",
		Line:      1,
	}
	// PHP's own version requirement is declared as a dependency named `php`, which is the
	// one entry in `require` that is not a package. Lifted to LangVersion, where every other
	// reader puts the toolchain floor, and left out of the dependency list below: a reference
	// page for "php" would be a page for the runtime.
	if v := root.Path("require", "php").String(); v != "" {
		facts.Module.LangVersion = v
	}

	for _, s := range composerScopes {
		obj := root.Get(s.key)
		obj.Each(func(name string, ver *Node) bool {
			if name == "php" || strings.HasPrefix(name, "ext-") || strings.HasPrefix(name, "lib-") {
				// `php`, `ext-json`, and `lib-openssl` are platform requirements: an
				// interpreter version, a compiled extension, a linked library. Composer
				// checks them and installs none of them, and each would otherwise become a
				// reference page for something that is not a package.
				return true
			}
			facts.Deps = append(facts.Deps, Dep{
				Name: name, Version: ver.String(), Scope: s.scope,
				Ecosystem: "composer", Line: lineOf(ver, obj.Line),
			})
			return true
		})
	}

	// A composer repository entry of type `path` names a directory in this repository, which
	// is composer's monorepo form and the same thing an npm `workspace:` reference is. It is
	// recorded as a workspace rather than resolved into the deps, because the dependency is
	// already declared in `require` — what this adds is where its code lives.
	//
	// Composer accepts `repositories` as either an array or an object keyed by a name a
	// human chose, and both spellings are in wide use — so both are walked. Each visits
	// nothing on an array and Seq returns nothing for an object, which is what makes the
	// pair safe rather than double-counting.
	repos := root.Get("repositories")
	repos.Each(func(_ string, r *Node) bool {
		facts.Module.Workspaces = append(facts.Module.Workspaces, composerPathRepo(r, f.Path)...)
		return true
	})
	for _, r := range repos.Seq() {
		facts.Module.Workspaces = append(facts.Module.Workspaces, composerPathRepo(r, f.Path)...)
	}

	dir := dirOfPath(f.Path)
	for _, key := range []string{"autoload", "autoload-dev"} {
		block := root.Get(key)
		// PSR-4 is the current standard and PSR-0 the superseded one. Both are read: a
		// long-lived project has both blocks, and the difference between them — PSR-0 maps
		// underscores in a class name onto directories too — affects where a *class* file
		// sits, not which directory a namespace prefix begins at, which is all that is
		// recorded here.
		for _, std := range []string{"psr-4", "psr-0"} {
			ns := block.Get(std)
			ns.Each(func(prefix string, target *Node) bool {
				alias := Alias{Pattern: prefix, Line: lineOf(target, ns.Line)}
				for _, t := range composerTargets(target) {
					alias.Targets = append(alias.Targets, joinRepoPath(dir, t))
				}
				if len(alias.Targets) > 0 {
					facts.Resolution.Aliases = append(facts.Resolution.Aliases, alias)
				}
				return true
			})
		}
		// `classmap` and `files` name directories and files with no namespace mapping at
		// all — composer scans them and builds the map itself. There is no prefix to record,
		// so they are deliberately not read: an alias with an empty pattern would claim every
		// import in the repository, which is the case addPSR4 declines for composer's own
		// empty-prefix fallback and for the same reason.
	}

	scripts := root.Get("scripts")
	scripts.Each(func(name string, cmd *Node) bool {
		// A composer script is either one command or a list of them. A list is joined with
		// `&& `, which is what running them in order means and is readable as the one thing
		// Script.Command is for.
		text := cmd.String()
		if cmd != nil && cmd.Kind == KindSeq {
			text = strings.Join(cmd.Strings(), " && ")
		}
		facts.Scripts = append(facts.Scripts, Script{
			Name: name, Command: text, Line: lineOf(cmd, scripts.Line),
		})
		return true
	})

	bin := root.Get("bin")
	for _, b := range bin.Strings() {
		facts.Entrypoints = append(facts.Entrypoints, Entrypoint{
			Name: path.Base(b), Path: joinRepoPath(dir, b), Line: bin.Line,
		})
	}
	return facts
}

// composerScopes maps composer's two dependency objects to a scope.
var composerScopes = []struct {
	key   string
	scope DepScope
}{
	{"require", ScopeRuntime},
	{"require-dev", ScopeDev},
}

// composerTargets returns the directories one autoload entry maps onto.
//
// The value is a string or an array of them: a namespace may be split across two
// directories, and composer tries each. Same fallback list tsconfig's `paths` targets are.
func composerTargets(n *Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == KindSeq {
		return n.Strings()
	}
	if s := n.String(); s != "" {
		return []string{s}
	}
	return nil
}

// composerPathRepo returns the directories a `{"type": "path", "url": "..."}` repository
// entry names, resolved against the composer.json's own location.
//
// A `url` may be a glob — `packages/*` is the standard monorepo spelling — and it is kept as
// written: the workspace list is a statement about where the members are, and the paths that
// match are already in the tree under their own names.
func composerPathRepo(r *Node, file string) []string {
	if r == nil || r.Get("type").String() != "path" {
		return nil
	}
	url := r.Get("url").String()
	if url == "" {
		return nil
	}
	return []string{joinRepoPath(dirOfPath(file), url)}
}

// joinRepoPath resolves a manifest-relative path into a repo-relative one, with the
// repository root spelled "".
func joinRepoPath(dir, p string) string {
	out := path.Clean(path.Join(dir, p))
	if out == "." || out == "/" {
		return ""
	}
	return strings.TrimPrefix(out, "./")
}
