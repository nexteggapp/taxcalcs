//! The federal return rules engine and the ReturnPreparer service, ported
//! from the Go packages `returns/rules/federal` and `returns/preparer`.

pub mod preparer;

/// The taxlisp rule packs, one module per jurisdiction.
pub mod rules {
    pub mod federal;
}
