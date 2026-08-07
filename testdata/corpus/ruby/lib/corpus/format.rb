# Corpus fixture: not installed, not run.
# frozen_string_literal: true

# Two stdlib requires, and they are here rather than in the file below because the pair is
# what makes Ruby's runtime rule different from every other language's. `json` is flat and
# `net/http` is hierarchical, and both are the standard library — so the table is matched on
# the whole require path with no first-segment rule, which is the only reading that can also
# report `net/ldap` as the gem it is.
require "json"
require "net/http"

module Corpus
  # Renders a greeting.
  module Format
    SEPARATOR = ", "

    # Returns the greeting for a name.
    def self.greet(name)
      "Hello#{SEPARATOR}#{name}"
    end

    # A one-line class body, which both opens and closes on the same line: read as an
    # opener alone it leaves a scope that swallows every declaration below it.
    class Blank; end

    # The sticky section marker, which is Ruby's alone. Every method below this line is
    # private until a `public` reverses it, so a class's internals are not surface — and
    # `SEPARATOR` above stays reachable, because the marker applies to methods only.
    private

    def self.normalise(name)
      name.to_s.strip
    end
  end
end
