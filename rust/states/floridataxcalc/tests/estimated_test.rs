//! Florida's estimated tax rules for 2026, which are that there is no
//! individual income tax to estimate. Ported from estimated_test.go.
//!
//! The Go test that each quoted sentence is in its testdata/ extract reads
//! Go source data and is not ported; that every result cites the quote is.

#![allow(clippy::unwrap_used)]

use estimated::{Basis, Date, EstimatedTax, Facts, Jurisdiction, Month};
use filing::Status;
use money::{DOLLAR, USCents};
use taxcalcs_floridataxcalc::estimated_tax;

const CODE: Jurisdiction = Jurisdiction::from_static("FL");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

/// Each document the finding rests on: a sentence it contains, and the
/// citation the rules give it.
const BASIS: [(&str, &str); 1] = [(
    "Florida does not impose personal income tax, inheritance tax, gift taxes, or tax on intangible personal property.",
    "Florida Department of Revenue, Tax Information for New Residents, GT-800025 (R. 08/25)",
)];

/// A household with every figure a state with an income tax would read, so
/// that the answer is seen not to depend on any of them.
fn facts(with_tax: bool) -> Facts {
    let mut fb = estimated::new_figures();
    fb.federal_agi(DOLLAR * 250_000);
    if with_tax {
        fb.tax(DOLLAR * 12_000);
    }
    let last = estimated::new_figures()
        .tax(DOLLAR * 9_000)
        .withheld(DOLLAR * 1_000)
        .build()
        .unwrap();
    estimated::new_facts(CODE, 2026)
        .filing_status(Status::MarriedFilingJointly)
        .this_year(fb.build().unwrap())
        .prior_year(
            estimated::new_prior_return()
                .filed(true)
                .figures(last)
                .build()
                .unwrap(),
        )
        .withheld(&[estimated::new_withheld()
            .on(day(2026, 6, 30))
            .amount(DOLLAR * 500)
            .build()
            .unwrap()])
        .paid(&[estimated::new_payment()
            .on(day(2026, 4, 15))
            .amount(DOLLAR * 2_000)
            .build()
            .unwrap()])
        .build()
        .unwrap()
}

#[test]
fn estimated_finding_is_cited() {
    let f = facts(true);
    let r = rules();
    for (name, applied) in [
        ("Requirement", r.requirement(&f).unwrap().applied()),
        ("Schedule", r.schedule(&f).unwrap().applied()),
        (
            "Penalty",
            r.penalty(&f, day(2027, 4, 15)).unwrap().applied(),
        ),
    ] {
        assert_eq!(applied.len(), BASIS.len(), "{name}");
        for (a, (quote, cite)) in applied.iter().zip(BASIS) {
            assert_eq!(a.cite(), cite, "{name}");
            assert!(a.applied().contains(quote), "{name}: {}", a.applied());
        }
    }
}

#[test]
fn estimated_nothing_to_estimate() {
    for with_tax in [true, false] {
        let res = rules().requirement(&facts(with_tax)).unwrap();
        let r = res.value();
        assert!(
            !r.required()
                && r.exempt()
                && r.target() == USCents(0)
                && r.options().len() == 1
                && r.target_known(),
            "with tax {with_tax}"
        );
        let c = r.chosen().unwrap();
        assert_eq!(c.basis(), Basis::Exception);
        assert_eq!(c.label(), "No income tax");
        assert_eq!(c.amount(), USCents(0));
        for s in ["no Florida income tax to estimate", "2026"] {
            assert!(r.why().contains(s), "{}", r.why());
        }
        assert!(res.required().is_empty() && res.divergences().is_empty());
    }
}

#[test]
fn estimated_schedule_has_no_due_dates() {
    assert!(rules().schedule(&facts(true)).unwrap().value().is_empty());
}

#[test]
fn estimated_penalty_is_zero() {
    let as_of = day(2027, 4, 15);
    let p = rules().penalty(&facts(false), as_of).unwrap().value();
    assert_eq!(p.total(), USCents(0));
    assert!(p.lines().is_empty());
    assert_eq!(p.as_of(), as_of);
    assert!(!p.upper_bound());
    assert_eq!(p.reason(), "No income tax.");
    assert!(rules().penalty(&facts(false), Date::default()).is_err());
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for year in [2025, 2027] {
        assert!(
            estimated_tax(year)
                .err()
                .is_some_and(|e| e.is(&estimated::Error::NoRules))
        );
    }
    let this = estimated::new_figures().build().unwrap();
    let cases = [
        (
            "another state's facts",
            estimated::new_facts(estimated::CALIFORNIA, 2026)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(CODE, 2027)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "extras",
            estimated::new_facts(CODE, 2026)
                .filing_status(Status::Single)
                .this_year(this)
                .state("extras")
                .build()
                .unwrap(),
        ),
    ];
    for (name, f) in cases {
        assert!(rules().requirement(&f).is_err(), "{name}");
        assert!(rules().schedule(&f).is_err(), "{name}");
        assert!(rules().penalty(&f, day(2027, 4, 15)).is_err(), "{name}");
    }
    let f = estimated::new_facts(CODE, 2026)
        .filing_status(Status::Single)
        .this_year(this)
        .state("extras")
        .build()
        .unwrap();
    assert_eq!(
        rules().requirement(&f).err().unwrap().to_string(),
        "floridataxcalc: Florida's rules read no extras, and the facts carry string"
    );
}

#[test]
fn estimated_explanation_says_there_is_nothing_to_estimate() {
    let e = rules().explanation();
    let text = format!(
        "{} {} {}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "nothing can be penalised as late",
        "Federal estimated tax",
        "no personal income tax",
    ] {
        assert!(text.contains(s), "{s}");
    }
    assert_eq!(e.sources().len(), BASIS.len());
}
