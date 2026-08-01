//! Corpus fixture binary.

use corpus_greeter::Greeting;

fn main() {
    println!("{}", Greeting::new("world").text);
}
