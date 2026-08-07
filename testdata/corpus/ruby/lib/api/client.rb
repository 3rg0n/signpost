# Corpus fixture: not installed, not run.
# frozen_string_literal: true

# The declared gem, required by name. This is the positive that makes the two near-misses
# below mean something: `rack` is in the Gemfile, so it must reach the reference page, and a
# resolver that reported it as a gap would be reporting the one dependency this repository
# does declare.
require "rack"

# A plain require of the repository's own code, which is the case Ruby's load path exists
# for: nothing in this file says `lib`, and only the gem convention — RubyGems puts a gem's
# `lib` on the path — makes `corpus/version` mean `lib/corpus/version.rb`. Resolved against
# the directory holding the Gemfile, not against the repository root.
require "corpus/version"

# The relative form, which needs no load path at all: it is relative to this file. The two
# forms resolve against different roots, so a resolver that could not tell them apart would
# either invent a gem for this or miss the one above.
require_relative "../corpus/format"

# The first near-miss, and it is the one Ruby's runtime rule turns on. `net/http` in
# format.rb is the standard library; `net/ldap` is the `net-ldap` gem, which nobody has
# declared here. They share a first segment, so a rule that cut the require path on the slash
# and asked about `net` would call this the runtime and drop it from the coverage report —
# hiding a dependency somebody has to install and patch behind the word "standard".
require "net/ldap"

# The second, on the other boundary. `rack_extras` opens with the four characters of the gem
# this file really requires, and no Gemfile in the tree declares it. A lookup matching a
# declared name by prefix, or folding the dash and underscore spellings together the way PyPI
# normalization legitimately does, swallows it into `rack` and reports an import of a gem
# this code never wrote.
require "rack_extras"

module Api
  # Fetches greetings over HTTP.
  class Client
    # A public attribute, which is how a Ruby class states its readable surface. Nothing
    # else in the file declares these methods.
    attr_reader :base_url

    def initialize(base_url)
      @base_url = base_url
    end

    # Returns the greeting the service reports for a name.
    def greet(name)
      Corpus::Format.greet(name)
    end

    private

    # Below the section marker, so not surface. There is no keyword on this line saying
    # so — the marker four lines up is the whole signal, which is what makes Ruby's
    # visibility rule unlike every other language here.
    def headers
      { "Accept" => "application/json" }
    end
  end
end
