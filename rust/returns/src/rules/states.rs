//! The taxlisp rule modules for state individual income tax returns:
//! California Form 540 and Oregon Form OR-40, ported from the Go package
//! `returns/rules/states`.
//!
//! The modules are not a program of their own. They read the prepared
//! federal return, so [`super::federal::load_engine`] loads them after the
//! federal rules into one module, and the federal prepare-return calls
//! prepare-state-returns.

/// The rule files, which Go embeds with `go:embed *.tlisp ca/*.tlisp
/// or/*.tlisp`, by their path under `returns/rules/states`.
const RULE_FILES: &[(&str, &str)] = &[
    (
        "00_states.tlisp",
        include_str!("../../../../returns/rules/states/00_states.tlisp"),
    ),
    (
        "ca/01_params.tlisp",
        include_str!("../../../../returns/rules/states/ca/01_params.tlisp"),
    ),
    (
        "ca/02_form_540.tlisp",
        include_str!("../../../../returns/rules/states/ca/02_form_540.tlisp"),
    ),
    (
        "or/01_params.tlisp",
        include_str!("../../../../returns/rules/states/or/01_params.tlisp"),
    ),
    (
        "or/02_form_or40.tlisp",
        include_str!("../../../../returns/rules/states/or/02_form_or40.tlisp"),
    ),
];

/// Every rule module in load order, as (path, source): the shared
/// 00_states.tlisp, then each state's directory, each in lexical order.
pub fn files() -> Vec<(&'static str, &'static str)> {
    let mut files = RULE_FILES.to_vec();
    files.sort_by(|a, b| a.0.cmp(b.0));
    files
}
