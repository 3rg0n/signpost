package extract

import (
	"strings"
	"testing"

	"github.com/3rg0n/signpost/internal/discover"
)

func rbFile(path, src string) discover.File {
	return discover.File{
		Path: path, Lang: discover.LangRuby, Class: discover.ClassSource, Content: src,
	}
}

func extractRuby(t *testing.T, path, src string) Facts {
	t.Helper()
	fa, err := RubyExtractor{}.Extract(rbFile(path, src))
	if err != nil {
		t.Fatalf("Extract(%s): %v", path, err)
	}
	fa.Normalize()
	return fa
}

// The scored corpus. Hand-labeled against real Ruby, including the forms no other
// language here has: a sticky visibility section marker, attr_accessor as the way a class
// states its data surface, an endless method, `class << self`, and the compact
// `class A::B::C` spelling of what the nested form writes as three declarations.
func rubyCorpus() []Fixture {
	return []Fixture{
		{
			File: rbFile("lib/api/service.rb", `# frozen_string_literal: true

require "json"
require "net/http"
require_relative "../store/repository"

module Api
  # Serves requests.
  class Service
    DEFAULT_TIMEOUT = 30

    attr_reader :name, :repo
    attr_accessor :timeout

    def initialize(name, repo)
      @name = name
      @repo = repo
    end

    # Looks something up.
    def lookup(key)
      repo.find(key) if key
    end

    def valid?
      !@name.nil?
    end

    def name=(value)
      @name = value
    end

    def self.build(name)
      new(name, Store::Repository.new)
    end

    private

    def helper(a, b)
      a > b ? a : b
    end

    def also_hidden
      helper(1, 2)
    end
  end
end
`),
			Expected: Expected{
				Package: "Api::Service",
				Imports: []string{"../store/repository", "json", "net/http"},
				Symbols: []string{
					"Api", "Service", "Service.DEFAULT_TIMEOUT", "Service.also_hidden",
					"Service.build", "Service.helper", "Service.initialize", "Service.lookup",
					"Service.name", "Service.name=", "Service.repo", "Service.timeout",
					"Service.valid?",
				},
				// Everything before the bare `private`. The two methods after it are the
				// whole point of the marker, and a reader that missed it would report a
				// class's internals as its contract.
				Exported: []string{
					"Api", "Service", "Service.DEFAULT_TIMEOUT", "Service.build",
					"Service.initialize", "Service.lookup", "Service.name", "Service.name=",
					"Service.repo", "Service.timeout", "Service.valid?",
				},
				Entrypoints: []string{},
			},
		},
		{
			// The declaration forms: the compact namespace spelling, a superclass, a
			// singleton class, an endless method, an operator method, a `protected`
			// section, and the inline `private def`.
			File: rbFile("lib/api/model.rb", `module Api
  class V2::Point < Struct
    def initialize(x, y)
      @x = x
      @y = y
    end

    def to_s = "(#{@x}, #{@y})"

    def <=>(other)
      x <=> other.x
    end

    def [](i)
      i.zero? ? @x : @y
    end

    class << self
      def origin
        new(0, 0)
      end
    end

    private def internal_key
      "#{@x}:#{@y}"
    end

    protected

    def shared_with_kin
      @x
    end
  end
end
`),
			Expected: Expected{
				// The compact form nested inside a module qualifies to the whole path, which
				// is what a caller writes to reach it.
				Package: "Api::V2::Point",
				Imports: []string{},
				Symbols: []string{
					"Api", "Point", "Point.<=>", "Point.[]", "Point.initialize",
					"Point.internal_key", "Point.origin", "Point.shared_with_kin",
					"Point.to_s",
				},
				// `class << self` pushes a scope carrying Point's own name, so `origin` stays
				// attributed to Point and is public. `private def` and the `protected` marker
				// are the two that are not surface.
				Exported: []string{
					"Api", "Point", "Point.<=>", "Point.[]", "Point.initialize",
					"Point.origin", "Point.to_s",
				},
				Entrypoints: []string{},
			},
		},
		{
			// The adversarial fixture. Ruby's hardest rule is telling a block keyword from
			// its modifier form, because one phantom opener swallows every declaration
			// after it — so this file puts `if`, `unless`, `while` and `do` in both
			// positions, and hides declarations in a heredoc, a `=begin` block, and
			// interpolated and single-quoted strings.
			File: rbFile("lib/tricky.rb", `require "real/thing"

=begin
require "ghost/gem"
class BlockCommentGhost
  def ghostly; end
end
=end

# require "commented/out"

class Tricky
  SNIPPET = 'class QuotedGhost; def phantom; end; end'

  TEMPLATE = <<~SQL
    class HeredocGhost
      def spectral
      end
    end
  SQL

  def real(items)
    return nil if items.empty?
    puts "x" unless items.any?
    total = 0
    items.each do |item|
      total += item while item > 0
    end
    if total > 10
      total = 10
    elsif total < 0
      total = 0
    else
      total
    end
    case total
    when 0 then "none"
    else "some"
    end
    total
  end

  def after_the_blocks
    :reached
  end
end
`),
			Expected: Expected{
				Package: "Tricky",
				Imports: []string{"real/thing"},
				Symbols: []string{
					"Tricky", "Tricky.SNIPPET", "Tricky.TEMPLATE", "Tricky.after_the_blocks",
					"Tricky.real",
				},
				Exported: []string{
					"Tricky", "Tricky.SNIPPET", "Tricky.TEMPLATE", "Tricky.after_the_blocks",
					"Tricky.real",
				},
				Entrypoints: []string{},
			},
		},
		{
			// A script: no module, no class, top-level methods and constants. This is the
			// shape a Rakefile helper or a bin/ script takes, and an extractor that
			// required an enclosing type would report the whole file as empty.
			File: rbFile("bin/report", `#!/usr/bin/env ruby

require "optparse"
require_relative "../lib/api/service"

OUTPUT_DIR = "out"

def render(name)
  "report: #{name}"
end

def main(argv)
  puts render(argv.first || "default")
end

main(ARGV)
`),
			Expected: Expected{
				Imports:     []string{"../lib/api/service", "optparse"},
				Symbols:     []string{"OUTPUT_DIR", "main", "render"},
				Exported:    []string{"OUTPUT_DIR", "main", "render"},
				Entrypoints: []string{},
			},
		},
	}
}

// The measurement design §4.2 promises for Ruby.
func TestRubyExtractorMeetsTarget(t *testing.T) {
	ls := ScoreExtractor(RubyExtractor{}, discover.LangRuby, rubyCorpus())
	if !ls.MeetsTarget() {
		t.Errorf("Ruby extractor below target:\n%s", ls.Report())
	}
	t.Logf("Ruby extractor score:\n%s", ls.Report())
}

// The sticky section marker is Ruby's own rule and has no counterpart in any other
// language this package reads: every other one puts visibility on the declaration. Getting
// it wrong reports a class's whole internals as its public contract, so both directions
// are asserted — the marker taking effect, and `public` reversing it.
func TestRubyVisibilityMarkerIsStickyAndReversible(t *testing.T) {
	fa := extractRuby(t, "a.rb", `class A
  def first; end

  private

  def hidden; end
  def also_hidden; end

  public

  def visible_again; end
end
`)
	if got := strings.Join(exportedNames(fa), ","); got != "A,A.first,A.visible_again" {
		t.Errorf("exported = %q; private is sticky until public reverses it", got)
	}
}

// A marker inside a nested class must not leak out to the enclosing one, which is why the
// flag lives on the scope rather than in a single variable. A leak would report the outer
// class's remaining methods as private — a smaller public surface than the file has.
func TestRubyVisibilityMarkerDoesNotLeakOutOfANestedClass(t *testing.T) {
	fa := extractRuby(t, "a.rb", `class Outer
  class Inner
    private

    def inner_hidden; end
  end

  def outer_visible; end
end
`)
	if got := strings.Join(exportedNames(fa), ","); got != "Inner,Outer,Outer.outer_visible" {
		t.Errorf("exported = %q; the inner marker must not reach the outer class", got)
	}
}

// The modifier forms are the extractor's hardest rule. `return x if y` puts a block
// keyword where it opens nothing, and counting it leaves a phantom scope open that
// swallows every declaration in the rest of the file — a silent, total loss rather than a
// visible error, which is why it gets its own test.
func TestRubyModifierKeywordsOpenNoScope(t *testing.T) {
	fa := extractRuby(t, "a.rb", `class A
  def guarded(x)
    return nil if x.nil?
    puts x unless x.zero?
    x -= 1 while x > 0
    x += 1 until x > 5
    x
  end

  def still_a_method; end
end
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "A,A.guarded,A.still_a_method" {
		t.Errorf("symbols = %q; a modifier keyword must not open a scope", got)
	}
}

// `end` matched as a substring is the mirror of the same failure: `append`, `send` and a
// variable named `end_time` all contain the letters, and each false `end` pops a scope
// that is still open — which reattributes the rest of the class to whatever encloses it.
func TestRubyEndIsMatchedAsAWholeToken(t *testing.T) {
	fa := extractRuby(t, "a.rb", `class A
  def send_all(items)
    items.append(1)
    end_time = Time.now
    backend = :x
    send(:noop)
    end_time
  end

  def after; end
end
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "A,A.after,A.send_all" {
		t.Errorf("symbols = %q; only a bare `end` closes a scope", got)
	}
}

// `require_relative` and `require` resolve against different roots, and the two are
// indistinguishable once the keyword is gone. The "./" marker is what the resolver reads,
// and without it a local file becomes an invented gem or a real gem goes missing.
func TestRubyRequireRelativeKeepsItsMarker(t *testing.T) {
	fa := extractRuby(t, "lib/a.rb", `require "json"
require_relative "helper"
require_relative "../other/thing"
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "../other/thing,./helper,json" {
		t.Errorf("imports = %q; require_relative must stay distinguishable", got)
	}
}

// A computed require names a path this extractor cannot know. Inventing one would put a
// module in the graph that no file declares, which is worse than the gap it fills.
func TestRubyComputedRequireIsNotInvented(t *testing.T) {
	fa := extractRuby(t, "a.rb", `require File.join(__dir__, "thing")
require SOME_CONSTANT
require "real/gem"
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "real/gem" {
		t.Errorf("imports = %q; only the literal path is a fact", got)
	}
}

// attr_accessor is how a Ruby class states its data surface, and a class that uses nothing
// else would otherwise report no readable attributes at all. A writer is as much a member
// as a reader.
func TestRubyAttrDeclarationsAreMembers(t *testing.T) {
	fa := extractRuby(t, "a.rb", `class A
  attr_reader :id, :name
  attr_writer :secret
  attr_accessor :count

  private

  attr_reader :internal
end
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "A,A.count,A.id,A.internal,A.name,A.secret" {
		t.Errorf("symbols = %q", got)
	}
	// The section marker applies to attr_* the same way it applies to a def.
	if got := strings.Join(exportedNames(fa), ","); got != "A,A.count,A.id,A.name,A.secret" {
		t.Errorf("exported = %q; an attr after `private` is not surface", got)
	}
	// `attr_readers` is not `attr_reader`, and matching it as one would invent members
	// from an ordinary method call.
	plain := extractRuby(t, "b.rb", `class B
  attr_readers :nope
end
`)
	if got := strings.Join(plain.SymbolNames(), ","); got != "B" {
		t.Errorf("symbols = %q; only the three attr_ forms declare members", got)
	}
}

// A method name's trailing `?`, `!` and `=` are part of the name. Dropping them would
// merge a predicate with a same-named plain method and rename every setter in the
// codebase; the endless-def form is the one place a trailing `=` is *not* the name.
func TestRubyMethodNameSuffixesArePartOfTheName(t *testing.T) {
	fa := extractRuby(t, "a.rb", `class A
  def valid?; end
  def save!; end
  def name=(v); end
  def name; end
  def label = "x"
end
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "A,A.label,A.name,A.name=,A.save!,A.valid?" {
		t.Errorf("symbols = %q", got)
	}
}

// An endless method opens no body, so it takes no `end`. Counting one would leave the
// scope stack a level too deep for the rest of the file.
func TestRubyEndlessDefOpensNoScope(t *testing.T) {
	fa := extractRuby(t, "a.rb", `class A
  def one = 1
  def two = compute(2)
  def three
    3
  end
end

class B
  def four = 4
end
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "A,A.one,A.three,A.two,B,B.four" {
		t.Errorf("symbols = %q; an endless def must not leave a scope open", got)
	}
}

// `class << self` opens the singleton class and declares no name. Pushing a scope for it
// would attribute its methods to a type that does not exist, when they belong to the type
// enclosing it.
func TestRubySingletonClassKeepsItsMethodsOnTheEnclosingType(t *testing.T) {
	fa := extractRuby(t, "a.rb", `class A
  class << self
    def build; end
    def configure; end
  end

  def instance_method; end
end
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "A,A.build,A.configure,A.instance_method" {
		t.Errorf("symbols = %q; singleton methods belong to A", got)
	}
}

// The compact `class A::B::C` form declares one type inside a namespace it does not
// define. Recording the namespace segments as classes would put types on the page this
// file never declares, and the qualified Package is what reconciles the compact form with
// the nested one.
func TestRubyCompactNamespaceDeclaresOnlyTheLastSegment(t *testing.T) {
	fa := extractRuby(t, "lib/api/v2/users.rb", `class Api::V2::Users
  def index; end
end
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Users,Users.index" {
		t.Errorf("symbols = %q; Api and V2 are not declared here", got)
	}
	if fa.Package != "Api::V2::Users" {
		t.Errorf("package = %q, want the whole path", fa.Package)
	}
}

// A constant is any capitalised assignment — Ruby has no const keyword — so the rule has
// to reject the shapes that look like one. A comparison and an augmented assignment are
// statements, and reading either as a declaration invents a constant.
func TestRubyConstantAssignmentRejectsStatements(t *testing.T) {
	fa := extractRuby(t, "a.rb", `LIMIT = 10
Config ||= {}
DEBUG == true
local_thing = 5
Other <= Base
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "LIMIT" {
		t.Errorf("symbols = %q; only a plain capitalised assignment is a constant", got)
	}
}

// A `=begin`/`=end` block is Ruby's block comment and must be invisible, and a heredoc's
// body is data. Both hold text that looks exactly like source, which is how a phantom
// class or a phantom gem dependency gets into a graph.
func TestRubyBlockCommentsAndHeredocsDeclareNothing(t *testing.T) {
	fa := extractRuby(t, "a.rb", `=begin
require "ghost/gem"
class Ghost
end
=end

class Real
  DOC = <<~TEXT
    require "heredoc/gem"
    class HeredocGhost
    end
  TEXT
end
`)
	if got := strings.Join(fa.ImportPaths(), ","); got != "" {
		t.Errorf("imports = %q; neither a comment nor a heredoc declares a dependency", got)
	}
	if got := strings.Join(fa.SymbolNames(), ","); got != "Real,Real.DOC" {
		t.Errorf("symbols = %q", got)
	}
}

// The other side of the heredoc rule, and the reason it is a rule about case rather than a
// match on `<<`. `a << b` is a legal shift and `<<SQL` is a heredoc opener, and the two are
// the same two characters followed by an identifier — so `scanHeredocOpen` requires the
// identifier to be uppercase, which is the convention every heredoc in the wild follows.
//
// The test above asserts only the direction where a heredoc is honoured. That passes for a
// scanner that treats *every* `<<` as an opener, and such a scanner blanks the remainder of
// any file holding a shift, taking every declaration below it out of the graph. Ruby's `<<`
// is also how a line is appended to an array and how a singleton class is opened, so the
// construct is ordinary rather than exotic, and the failure is silent: the symbols simply
// are not there.
//
// The trade this documents runs the other way too. Ruby permits a lowercase heredoc
// identifier, so `<<sql` is a real opener this deliberately does not honour. That direction
// costs at most a phantom declaration read out of the heredoc's body, where honouring every
// `<<` costs real symbols — so the rule is set where a miss is additive rather than
// subtractive, which is the same asymmetry the fixture-skipping rules are chosen on.
//
// Both spacings are here because only one of them reaches the case rule. `count << shift`
// is stopped a step earlier, by the requirement that an identifier follow the operator
// immediately — a space means it is not an opener whatever case the identifier is. The
// unspaced `lines<<line` gets past that and the case is the only thing left, which makes it
// the form that actually holds the guard up. A test written with the spaced form alone passes
// with the guard deleted.
func TestRubyAShiftIsNotAHeredoc(t *testing.T) {
	fa := extractRuby(t, "a.rb", `class Shifted
  def pack(count, shift)
    count << shift
  end
end

class Appended
  def collect(lines, line)
    lines<<line
  end
end

class Below
  MARKER = 1
end
`)
	if got := strings.Join(fa.SymbolNames(), ","); got != "Appended,Appended.collect,Below,Below.MARKER,Shifted,Shifted.pack" {
		t.Errorf("symbols = %q; a `<<` shift was read as a heredoc opener, which blanks every "+
			"line below it and takes the declarations there out of the graph", got)
	}
}
