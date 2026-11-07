//! Computes a tax year from a TaxYearScenarios file, ported from the Go
//! package `taxyear` and the packages under it, which are modules here.

pub mod federal;
pub mod internal;
pub mod model;
pub mod paid;
pub mod payroll;

mod cache;
mod canadd;
mod compute;
mod dependents;
mod error;
mod goquote;
mod models;
#[cfg(test)]
mod override_tests;
mod overrides;
mod parse;
mod path;
mod protojson;
mod prune;
mod reflect;
mod resolve;
mod result;
mod round;
mod summary;
#[cfg(test)]
mod taxyear_tests;
mod validate;
mod working;

pub use cache::{Cache, CacheOption};
pub use compute::{COMPUTED_BY, compute};
pub use error::Error;
pub use parse::{parse, strip_comments};
pub use path::{Path, parse_path};
pub use resolve::{Resolved, apply_changes, resolve};
pub use result::{Figure, Scenario, TaxYearResult};
pub use validate::{
    check_file, check_ids, check_scenarios, known_kinds, overridable_path, validate,
};
