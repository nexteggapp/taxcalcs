//! The taxlisp engine, ported from the Go packages `taxlisp/parser`,
//! `taxlisp/types` and `taxlisp/eval`: a strongly, statically typed, purely
//! functional Lisp for tax rules over protobuf schemas. The language is
//! specified in docs/taxlisp.md at the repository root.

use std::fmt;

/// Where in a source file an error is: the start of the expression or token
/// it is about.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SourcePos {
    /// The file's path, as given to the parser.
    pub file: String,
    /// The 1-based line.
    pub line: i64,
    /// The 1-based column.
    pub column: i64,
}

/// Renders the position as Go's errors do: `file:line:col`.
impl fmt::Display for SourcePos {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}:{}:{}", self.file, self.line, self.column)
    }
}

/// Declares an error type with an optional [`SourcePos`] and a message.
/// Its text is the Go error's: `file:line:col: message` with a position,
/// else the message alone.
macro_rules! located_error {
    ($(#[$doc:meta])* $name:ident) => {
        $(#[$doc])*
        #[derive(Clone, Debug, PartialEq, Eq)]
        pub struct $name {
            /// Where the error is, when the source position is known.
            pub pos: Option<$crate::SourcePos>,
            /// What is wrong, without the position.
            pub message: String,
        }

        impl $name {
            /// An error with no source position.
            pub fn new(message: impl Into<String>) -> Self {
                $name { pos: None, message: message.into() }
            }

            /// An error at `pos`.
            pub fn at(pos: $crate::SourcePos, message: impl Into<String>) -> Self {
                $name { pos: Some(pos), message: message.into() }
            }
        }

        impl std::fmt::Display for $name {
            fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
                match &self.pos {
                    Some(pos) => write!(f, "{pos}: {}", self.message),
                    None => f.write_str(&self.message),
                }
            }
        }

        impl std::error::Error for $name {}
    };
}
pub(crate) use located_error;

pub mod eval;
pub mod parser;
pub mod types;
