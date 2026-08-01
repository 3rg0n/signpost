//! Corpus fixture binary.

use corpus_greeter::Greeting;

// The negative boundary for crate-name matching. A use path spells a crate with
// underscores even when Cargo declares it with dashes, so `serde` is reached from
// `serde::...` and the declared `pretty_assertions` from `pretty_assertions::...`. That
// dash/underscore equivalence is what a matcher over-applies: `serde_yaml` is a different
// crate that nobody declared here, and folding it into the declared `serde` node would
// credit this crate with a dependency its manifest does not list — a fabricated
// supply-chain entry, which is the one thing the resolver must never produce.
use serde_yaml::Value;

// `std` is the standard library: in no manifest, patched by nobody, so no node and no
// reported gap.
use std::fmt::Write as _;

fn main() {
    let mut out = String::new();
    let _ = write!(out, "{}", Greeting::new("world").text);
    let _: Option<Value> = None;
    println!("{out}");
}
