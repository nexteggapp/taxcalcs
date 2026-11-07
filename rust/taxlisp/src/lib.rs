//! The taxlisp engine, ported from the Go packages `taxlisp/parser`,
//! `taxlisp/types` and `taxlisp/eval`: a strongly, statically typed, purely
//! functional Lisp for tax rules over protobuf schemas. The language is
//! specified in docs/taxlisp.md at the repository root.

pub mod eval;
pub mod parser;
pub mod types;
