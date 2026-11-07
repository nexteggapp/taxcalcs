//! Package floridataxcalc answers the shared estimated tax questions for
//! Florida, which has no individual income tax. Ported from the Go package
//! `floridataxcalc`.
//!
//! Florida does not impose a personal income tax.
//!
//! For 2026 the answer is that there is nothing to estimate. Requirement is
//! exempt with no target, Schedule has no due dates, and Penalty is zero. Each
//! result cites the document the finding rests on, which is checked into the
//! Go package's testdata/.
//!
//! Start at [`estimated_tax`].

// estimated.go: the rules. (A module named `estimated` would shadow the crate.)
mod estimated_y2026;
mod rules;

pub use rules::estimated_tax;
