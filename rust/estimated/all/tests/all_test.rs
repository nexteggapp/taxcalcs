//! The Go tests of estimated/all: every jurisdiction is registered, and a
//! year or jurisdiction that is not built is refused.

#![allow(clippy::unwrap_used)]
use estimated::{Error, Jurisdiction};
use taxcalcs_estimated_all::for_jurisdiction;

/// Written out here rather than read from `estimated::jurisdictions`, so that
/// a jurisdiction missing from both the constants and the registry still
/// fails.
const POSTAL_CODES: [&str; 52] = [
    "US", "AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "DC", "FL", "GA", "HI", "ID", "IL", "IN",
    "IA", "KS", "KY", "LA", "ME", "MD", "MA", "MI", "MN", "MS", "MO", "MT", "NE", "NV", "NH", "NJ",
    "NM", "NY", "NC", "ND", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VT", "VA",
    "WA", "WV", "WI", "WY",
];

/// A single filer with nothing else said.
fn minimal_facts(j: &Jurisdiction) -> estimated::Facts {
    let this = estimated::new_figures().build().unwrap();
    estimated::new_facts(j.clone(), 2026)
        .filing_status(filing::Status::Single)
        .this_year(this)
        .build()
        .unwrap()
}

#[test]
fn for_finds_every_jurisdiction() {
    for code in POSTAL_CODES {
        let j = Jurisdiction::from_static(code);
        let rules = for_jurisdiction(&j, 2026).unwrap_or_else(|e| panic!("{j} 2026: {e}"));
        assert!(
            !rules.explanation().summary().is_empty(),
            "{j} 2026 has no explanation"
        );
        // Each jurisdiction's rules refuse facts about another, so this fails
        // when the registry maps a code to the wrong crate.
        if let Err(e) = rules.requirement(&minimal_facts(&j)) {
            panic!("{j} 2026 refuses its own facts: {e}");
        }
    }
}

#[test]
fn jurisdictions_lists_every_code() {
    let got: Vec<String> = estimated::jurisdictions()
        .iter()
        .map(|j| j.as_str().to_string())
        .collect();
    let mut sorted = got.clone();
    sorted.sort();
    sorted.dedup();
    assert_eq!(sorted.len(), got.len(), "a code is listed twice");
    let mut want: Vec<String> = POSTAL_CODES.iter().map(|c| c.to_string()).collect();
    want.sort();
    assert_eq!(sorted, want);
}

#[test]
fn for_refuses_what_is_not_built() {
    for code in POSTAL_CODES {
        for year in [2025, 2027] {
            match for_jurisdiction(&Jurisdiction::from_static(code), year) {
                Err(e) if e.is(&Error::NoRules) => {}
                Err(e) => panic!("{code} {year}: got {e}, want NoRules"),
                Ok(_) => panic!("{code} {year}: got rules, want NoRules"),
            }
        }
    }
    match for_jurisdiction(&Jurisdiction::from_static("ZZ"), 2026) {
        Err(e) => assert_eq!(
            e.to_string(),
            "estimated: no estimated tax rules for this jurisdiction and year: ZZ"
        ),
        Ok(_) => panic!("ZZ has rules"),
    }
}
