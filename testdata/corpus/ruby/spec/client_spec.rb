# Corpus fixture: not installed, not run.
# frozen_string_literal: true

# The declared dev gem. It is in the Gemfile's `group :development, :test` block, so a reader
# that ignored the group would still resolve it — what the group decides is the scope on its
# reference page, not whether it resolves.
require "rspec"

# The subject, reached the way a Ruby test reaches it: a relative require. This is the whole
# reason Ruby has no arm in addTestEdges. A JVM test declares the package it tests and its
# imports name only collaborators, so the declaration is the only honest source of the
# `tested_by` edge; a Ruby test declares nothing and names its subject in a require, which is
# an import like any other. The edge here comes from this line.
require_relative "../lib/api/client"

RSpec.describe Api::Client do
  it "greets" do
    expect(described_class.new("http://localhost").greet("world")).to eq("Hello, world")
  end
end
