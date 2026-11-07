//! Texas's estimated tax rules for 2026, which are that there is no
//! individual income tax to estimate.
//!
//! There is no arithmetic to check. The oracle is the document the finding
//! rests on: each result must cite it with the quote. (Go also compares each
//! quote against its extract in testdata/; that check stays in Go.)

#![allow(clippy::unwrap_used)]

use estimated::{Basis, Date, EstimatedTax, Facts, Month};
use money::{DOLLAR, USCents};
use taxcalcs_texastaxcalc::estimated_tax;

const CODE: estimated::Jurisdiction = estimated::TEXAS;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

/// Each document the finding rests on: a sentence it contains, and the
/// citation the rules give it.
const BASIS: &[(&str, &str)] = &[
    (
        "The legislature may not impose a tax on the net incomes of individuals",
        "Tex. H.J.R. 38, 86th Leg. (2019), enrolled, adding Tex. Const. art. VIII, § 24-a",
    ),
    (
        "PROPOSITION No. 4, as submitted by House Joint Resolution No. 38, prohibiting the imposition of an individual income tax",
        "Office of the Texas Governor, proclamation of the November 5, 2019 constitutional amendment election, December 4, 2019",
    ),
];

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
        .filing_status(filing::Status::MarriedFilingJointly)
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

/// Every result cites each document with the quote.
#[test]
fn estimated_finding_is_in_its_sources() {
    let f = facts(true);
    let r = rules();
    let req = r.requirement(&f).unwrap().applied();
    let sched = r.schedule(&f).unwrap().applied();
    let pen = r.penalty(&f, day(2027, 4, 15)).unwrap().applied();
    for (name, applied) in [("Requirement", req), ("Schedule", sched), ("Penalty", pen)] {
        assert_eq!(applied.len(), BASIS.len(), "{name}");
        for (i, (quote, cite)) in BASIS.iter().enumerate() {
            assert_eq!(applied[i].cite(), *cite, "{name} rule {}", i + 1);
            assert!(
                applied[i].applied().contains(quote),
                "{name} rule {}",
                i + 1
            );
        }
    }
}

/// No payments are required and no penalty can arise, whatever the
/// household's figures, and whether or not this year's tax is given.
#[test]
fn estimated_nothing_to_estimate() {
    for with_tax in [true, false] {
        let res = rules().requirement(&facts(with_tax)).unwrap();
        let r = res.value();
        assert!(!r.required() && r.exempt() && r.target() == USCents(0));
        assert!(
            r.options().len() == 1 && r.target_known(),
            "with tax {with_tax}"
        );
        // The one option is the exception, and it is the target.
        let c = r.chosen().unwrap();
        assert_eq!(c.basis(), Basis::Exception);
        assert_eq!(c.label(), "No income tax");
        assert_eq!(c.amount(), USCents(0));
        for s in ["no Texas income tax to estimate", "2026"] {
            assert!(
                r.why().contains(s),
                "the reason {:?} does not say {s:?}",
                r.why()
            );
        }
        assert!(res.required().is_empty() && res.divergences().is_empty());
    }
}

#[test]
fn estimated_schedule_has_no_due_dates() {
    let res = rules().schedule(&facts(true)).unwrap();
    assert!(res.value().is_empty());
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
        let err = estimated_tax(year).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{year}: {err}");
    }
    let this = estimated::new_figures().build().unwrap();
    let cases: Vec<(&str, Facts)> = vec![
        (
            "another state's facts",
            estimated::new_facts(estimated::CALIFORNIA, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(CODE, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "extras",
            estimated::new_facts(CODE, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("extras")
                .build()
                .unwrap(),
        ),
    ];
    for (name, f) in cases {
        assert!(rules().requirement(&f).is_err(), "{name}: Requirement");
        assert!(rules().schedule(&f).is_err(), "{name}: Schedule");
        assert!(
            rules().penalty(&f, day(2027, 4, 15)).is_err(),
            "{name}: Penalty"
        );
    }
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
        "net incomes of individuals",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
    assert_eq!(e.sources().len(), BASIS.len());
}
