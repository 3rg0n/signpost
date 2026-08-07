package manifest

import (
	"path"
	"strings"

	"github.com/3rg0n/signpost/internal/discover"
)

// ExtractGem reads a Gemfile or a .gemspec.
//
// One reader for both files, and one Kind, because a gem's dependencies are split across
// the two by convention rather than by meaning: a gem declares its real requirements in
// the gemspec's `add_dependency` calls and its Gemfile contains the single line `gemspec`,
// while an application declares everything in the Gemfile and ships no gemspec at all. A
// reader that handled only one of them would read half of the repositories it was given and
// report the other half as declaring nothing.
//
// The two files' statements are disjoint — a Gemfile has no `add_dependency` and a gemspec
// has no bare `gem` call — so one line loop recognises both without having to know which
// file it is in.
//
// Both are Ruby programs rather than data, which bounds what this can honestly claim.
// `gem "rails", ENV["RAILS_VERSION"]` states a version this reader cannot know, and a
// Gemfile is free to compute its dependency list in a loop. What is read is the literal
// case, which is what almost every one of these files is; a computed declaration yields the
// name and an empty version rather than a guess.
func ExtractGem(f discover.File) Facts {
	facts := Facts{Path: f.Path, Class: f.Class, Kind: KindGemfile}
	facts.Module = Module{Ecosystem: "rubygems", Line: 1}
	if strings.HasSuffix(f.Path, ".gemspec") {
		// A gemspec names the gem it describes even when `spec.name` is computed, because the
		// filename is required to match the gem name — RubyGems will not build it otherwise.
		// So this is a fact rather than a guess, and it is the fallback for the `spec.name =
		// GEM_NAME` form that names a constant.
		facts.Module.Name = strings.TrimSuffix(path.Base(f.Path), ".gemspec")
	}

	// The block scopes, innermost last. `group :test do` puts every gem inside it in that
	// scope; every other block — `platforms`, `source ... do`, `git ... do` — pushes the
	// scope it inherited, so that its `end` pops the right thing. Tracking every block
	// rather than only the groups is what keeps the stack aligned: an `end` that popped a
	// group it did not open would leak the group's scope over the rest of the file.
	scopes := []DepScope{ScopeRuntime}
	cur := func() DepScope { return scopes[len(scopes)-1] }

	lines := strings.Split(strings.ReplaceAll(f.Content, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		num := i + 1
		line := lines[i]
		if c := indexComment(line); c >= 0 {
			line = line[:c]
		}
		line = strings.TrimSpace(line)
		// A declaration wrapped over several lines, which a Gemfile with several options per
		// gem routinely is. Joined here so the options are read with the name they belong to
		// rather than discarded as a line that declares nothing.
		for strings.HasSuffix(line, ",") && i+1 < len(lines) {
			i++
			next := lines[i]
			if c := indexComment(next); c >= 0 {
				next = next[:c]
			}
			line += " " + strings.TrimSpace(next)
		}
		if line == "" {
			continue
		}

		switch {
		case line == "end" || strings.HasPrefix(line, "end "):
			if len(scopes) > 1 {
				scopes = scopes[:len(scopes)-1]
			}
			continue
		case strings.HasSuffix(line, " do") || strings.HasSuffix(line, "|"):
			// A block opens. `do |spec|` ends in a pipe, which is why both endings count.
			next := cur()
			if rest, ok := rubyOptionAfter(line, "group", "groups"); ok {
				next = rubyGroupScope(rest)
			} else if strings.HasPrefix(line, "group ") {
				next = rubyGroupScope(line)
			}
			scopes = append(scopes, next)
			continue
		}

		// `gemspec` tells bundler to read the .gemspec beside this Gemfile. That file is
		// discovered and read on its own, so following the directive here would record every
		// dependency twice.
		if line == "gemspec" || strings.HasPrefix(line, "gemspec ") {
			continue
		}

		switch {
		case strings.HasPrefix(line, "gem "), strings.HasPrefix(line, "gem("):
			if d, ok := gemLine(line, cur(), num, dirOfPath(f.Path)); ok {
				facts.Deps = append(facts.Deps, d)
			}

		case strings.Contains(line, ".add_dependency"), strings.Contains(line, ".add_runtime_dependency"):
			if d, ok := gemspecDep(line, ScopeRuntime, num); ok {
				facts.Deps = append(facts.Deps, d)
			}

		case strings.Contains(line, ".add_development_dependency"):
			if d, ok := gemspecDep(line, ScopeDev, num); ok {
				facts.Deps = append(facts.Deps, d)
			}

		case strings.HasPrefix(line, "ruby "):
			// The Gemfile's own form: `ruby "3.3.0"`. `ruby file: ".ruby-version"` names a
			// file instead, and that file is discovered on its own, so there is nothing here
			// to record.
			if v := firstRubyString(line); v != "" {
				facts.Module.LangVersion = v
			}

		case strings.Contains(line, ".required_ruby_version"):
			if v := firstRubyString(line); v != "" {
				facts.Module.LangVersion = v
			}

		case strings.Contains(line, ".name") && strings.Contains(line, "="):
			if v := firstRubyString(line); v != "" {
				facts.Module.Name = v
			}

		case strings.Contains(line, ".version") && strings.Contains(line, "="):
			// `spec.version = Corpus::VERSION` is the usual form and names a constant this
			// reader cannot resolve, so it records nothing rather than the constant's name.
			if v := firstRubyString(line); v != "" {
				facts.Module.Version = v
			}

		case strings.Contains(line, ".executables"), strings.Contains(line, ".bindir"):
			facts.Entrypoints = append(facts.Entrypoints, gemExecutables(line, num)...)
		}
	}

	// bindir applies to every executable the gemspec names and may be stated after them, so
	// the paths are completed once the whole file has been read.
	if dir := gemBindir(f.Content); dir != "" {
		for i := range facts.Entrypoints {
			if facts.Entrypoints[i].Path == "" {
				facts.Entrypoints[i].Path = dir + "/" + facts.Entrypoints[i].Name
			}
		}
	}
	return facts
}

// gemLine reads a `gem "name", ...` declaration.
func gemLine(line string, scope DepScope, num int, dir string) (Dep, bool) {
	args := rubySplitArgs(strings.TrimPrefix(strings.TrimPrefix(line, "gem"), "("))
	if len(args) == 0 {
		return Dep{}, false
	}
	name := firstRubyString(args[0])
	if name == "" {
		return Dep{}, false
	}
	d := Dep{Name: name, Scope: scope, Ecosystem: "rubygems", Line: num}
	// Every positional argument after the name is a version constraint, and a gem may carry
	// two — `gem "rails", ">= 6.0", "< 7.1"` is one requirement expressed as a range. Joined
	// rather than reduced to the first, because either half alone is a different requirement
	// from the pair.
	//
	// Positional means "begins with a quote": an option is `key: value` or `:key => value`
	// and never starts with one. That test is what keeps a `git:` URL's own colon out of the
	// decision, which a scan for the first colon on the line would not.
	var versions []string
	for _, a := range args[1:] {
		a = strings.TrimSpace(a)
		if a == "" || (a[0] != '"' && a[0] != '\'') {
			break
		}
		if v := firstRubyString(a); v != "" {
			versions = append(versions, v)
		}
	}
	d.Version = strings.Join(versions, ", ")

	if rest, ok := rubyOptionAfter(line, "group", "groups"); ok {
		d.Scope = rubyGroupScope(rest)
	}
	if rest, ok := rubyOptionAfter(line, "optional"); ok && strings.Contains(rest, "true") {
		d.Optional = true
	}
	// A path dependency is a directory in this repository, resolved against the Gemfile the
	// way Terraform's local module source is: Local is what keeps it off the reference index,
	// where a page for the repository's own code would claim it came from outside.
	if rest, ok := rubyOptionAfter(line, "path"); ok {
		if p := firstRubyString(rest); p != "" {
			d.Source = path.Clean(path.Join(dir, p))
			d.Local = true
		}
	} else if rest, ok := rubyOptionAfter(line, "git", "github", "gitlab", "bitbucket"); ok {
		// A git dependency has no registry to publish an advisory against, which is the
		// reason Dep.Source exists.
		if p := firstRubyString(rest); p != "" {
			d.Source = p
		}
	}
	return d, true
}

// gemspecDep reads a `spec.add_dependency "name", "constraint"` call.
//
// Matched on the method rather than on the receiver, because the block parameter is named
// by whoever wrote the file — `spec`, `s`, and `gem` are all common — and the method name
// is the part the format fixes.
func gemspecDep(line string, scope DepScope, num int) (Dep, bool) {
	args := rubyStrings(line)
	if len(args) == 0 {
		return Dep{}, false
	}
	return Dep{
		Name: args[0], Version: strings.Join(args[1:], ", "),
		Scope: scope, Ecosystem: "rubygems", Line: num,
	}, true
}

// gemExecutables reads the executables a gemspec declares.
func gemExecutables(line string, num int) []Entrypoint {
	if !strings.Contains(line, ".executables") {
		return nil
	}
	var out []Entrypoint
	for _, name := range rubyStringList(line) {
		out = append(out, Entrypoint{Name: path.Base(name), Line: num})
	}
	return out
}

// gemBindir returns the directory a gemspec's executables live in.
//
// "exe" when the gemspec does not say, which is RubyGems' own default for a gem built with
// the current tooling — so the path is stated rather than left half-known. A gem that puts
// them in `bin` says so, and that is the case this reads.
func gemBindir(content string) string {
	for _, raw := range strings.Split(content, "\n") {
		if !strings.Contains(raw, ".bindir") {
			continue
		}
		if v := firstRubyString(raw); v != "" {
			return strings.Trim(v, "/")
		}
	}
	return "exe"
}

// rubyGroupScope maps a `group:` option or a `group` block onto a scope.
//
// Sniffed rather than parsed, because the same meaning arrives in four spellings —
// `:test`, `[:development, :test]`, `%i[test]`, `"test"` — and every one of them contains
// the group's name as a word. What matters downstream is only whether the dependency is
// needed to run the application or only to develop it, so the two names that mean "not at
// runtime" are the whole test.
func rubyGroupScope(s string) DepScope {
	for _, g := range []string{"development", "test", "lint", "doc", "ci"} {
		if strings.Contains(s, g) {
			return ScopeDev
		}
	}
	return ScopeRuntime
}

// rubyOptionAfter returns the text of the first option named by keys, in either the modern
// `key:` or the legacy `:key =>` spelling.
//
// The text ends at the next comma outside brackets, so an option that follows cannot be
// read as part of this one — `group: :assets, path: "vendor/test"` must not be scoped to
// test by the word in the path.
func rubyOptionAfter(line string, keys ...string) (string, bool) {
	best := -1
	for _, k := range keys {
		for _, marker := range []string{k + ":", ":" + k + " =>", ":" + k + "=>"} {
			i := strings.Index(line, marker)
			if i < 0 {
				continue
			}
			// The marker must begin a word, or `path:` would match inside `load_path:`.
			if i > 0 && (identByte(line[i-1]) || line[i-1] == ':') {
				continue
			}
			if best < 0 || i < best {
				best = i + len(marker)
			}
		}
	}
	if best < 0 {
		return "", false
	}
	rest := line[best:]
	depth := 0
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			depth--
		case ',':
			if depth <= 0 {
				return rest[:i], true
			}
		}
	}
	return rest, true
}

// rubySplitArgs splits an argument list on the commas that separate arguments, ignoring
// commas inside a string or inside brackets.
//
// `gem "rails", ">= 6.0", group: [:development, :test]` is three arguments, and the comma
// inside the array is not one of them.
func rubySplitArgs(s string) []string {
	var out []string
	depth, start := 0, 0
	inString, quote, escaped := false, byte(0), false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == quote:
				inString = false
			}
			continue
		}
		switch c {
		case '"', '\'':
			inString, quote = true, c
		case '[', '(', '{':
			depth++
		case ']', ')', '}':
			depth--
		case ',':
			if depth <= 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if last := strings.TrimSpace(s[start:]); last != "" {
		out = append(out, last)
	}
	return out
}

// identByte reports whether c can appear inside a Ruby identifier.
func identByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// rubyStrings returns the quoted literals on a line, in order.
//
// Both quote styles, because Ruby treats them as interchangeable for a plain string and a
// Gemfile uses whichever the author preferred. An escaped quote does not end a literal; an
// interpolated one — `"#{version}"` — yields its text as written, which is not a version
// but is at least visibly not one.
func rubyStrings(line string) []string {
	var out []string
	for i := 0; i < len(line); i++ {
		q := line[i]
		if q != '"' && q != '\'' {
			continue
		}
		j := i + 1
		var b strings.Builder
		for ; j < len(line); j++ {
			if line[j] == '\\' && j+1 < len(line) {
				b.WriteByte(line[j+1])
				j++
				continue
			}
			if line[j] == q {
				break
			}
			b.WriteByte(line[j])
		}
		if j >= len(line) {
			// An unterminated literal: the line is wrapped inside a string, which nothing
			// here can read. Stopping keeps the literals already found.
			break
		}
		if s := strings.TrimSpace(b.String()); s != "" {
			out = append(out, s)
		}
		i = j
	}
	return out
}

// firstRubyString returns the first quoted literal on a line, or "".
func firstRubyString(line string) string {
	if s := rubyStrings(line); len(s) > 0 {
		return s[0]
	}
	return ""
}

// rubyStringList reads a list of names written either as quoted literals or as a `%w[]`
// word array, which is the form a gemspec usually uses.
func rubyStringList(line string) []string {
	for _, open := range []string{"%w[", "%w(", "%w{", "%i[", "%i("} {
		i := strings.Index(line, open)
		if i < 0 {
			continue
		}
		rest := line[i+len(open):]
		if end := strings.IndexAny(rest, "])}"); end >= 0 {
			rest = rest[:end]
		}
		return strings.Fields(rest)
	}
	// The assignment's own target is not a value: `spec.executables = ["corpus"]` has no
	// literal before the bracket, but `spec.executables = ["bin/corpus"]` written with the
	// name in quotes does, and both reach here through the same path.
	return rubyStrings(line)
}

// dirOfPath returns a file's directory as a repo-relative slash path, with the root
// spelled "".
func dirOfPath(p string) string {
	d := path.Dir(p)
	if d == "." || d == "/" {
		return ""
	}
	return d
}
