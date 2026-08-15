//! Greeting types. Corpus fixture: not compiled, not run.

pub mod formatter;
pub mod store;

use serde::Serialize;

/// A greeting with an identity.
#[derive(Serialize)]
pub struct Greeting {
    pub text: String,
}

impl Greeting {
    /// Builds a greeting for `name`.
    pub fn new(name: &str) -> Self {
        Self { text: formatter::render(name) }
    }
}

pub(crate) fn internal_only() {}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn builds() {
        assert!(!Greeting::new("world").text.is_empty());
    }
}
