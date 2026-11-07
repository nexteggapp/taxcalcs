//! taxyear's errors, each with the text of the Go error it ports.

/// An error from taxyear. Its text is the Go error's.
#[derive(Clone, Debug, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    /// The file can't be read as a TaxYearScenarios: [`crate::parse`]'s
    /// errors.
    #[error("{0}")]
    Parse(String),
    /// A field path can't be parsed: [`crate::parse_path`]'s errors.
    #[error("{0}")]
    Path(String),
    /// A scenario's changes don't apply: [`crate::apply_changes`]'s errors,
    /// protodiffgen's text.
    #[error("{0}")]
    Apply(String),
    /// The federal return layer failed, as loading the rules engine.
    #[error("{0}")]
    Federal(#[from] crate::federal::Error),
}
