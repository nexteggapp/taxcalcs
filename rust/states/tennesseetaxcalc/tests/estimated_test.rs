//! Ported from tennesseetaxcalc/estimated_test.go: Tennessee's estimated tax
//! rules for 2026, which are that there is no individual income tax to
//! estimate.
//!
//! There is no arithmetic to check. The oracle is the document the finding
//! rests on: each quoted sentence is compared against its extract in the Go
//! package's testdata/, and each result must cite it. The constitution's
//! sentence is compared against the research file's quote, research-tn.md,
//! because the Blue Book could not be downloaded.

#![allow(clippy::unwrap_used)]

use std::collections::BTreeMap;

use estimated::{Basis, Date, EstimatedTax, Facts, Jurisdiction, Month};
use filing::Status;
use money::{DOLLAR, USCents};
use taxcalcs_tennesseetaxcalc::estimated_tax;

const CODE: Jurisdiction = Jurisdiction::from_static("TN");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn extract(name: &str) -> String {
    let path = format!(
        "{}/../../../tennesseetaxcalc/testdata/{name}",
        env!("CARGO_MANIFEST_DIR")
    );
    let raw = std::fs::read_to_string(&path).unwrap_or_else(|e| panic!("read {name}: {e}"));
    raw.split_whitespace().collect::<Vec<_>>().join(" ")
}

/// Each document the finding rests on, a sentence it contains, and the
/// citation the rules give it.
const BASIS: [(&str, &str, &str); 2] = [
    (
        "dor-tn-hall-income-tax.txt",
        "The Hall Income tax was repealed for tax periods that begin on January 1, 2021, or later.",
        "Tennessee Department of Revenue, Hall Income Tax",
    ),
    (
        "research-tn.md",
        "the Legislature shall not levy, authorize or otherwise permit any state or local tax upon payroll or earned personal income",
        "Tenn. Const. art. II, § 28 (Tennessee Blue Book 2025-2026)",
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

/// Each quoted sentence is in its document, and every result cites it with
/// the quote.
#[test]
fn estimated_finding_is_in_its_sources() {
    for (file, quote, _) in BASIS {
        assert!(extract(file).contains(quote), "{quote:?} is not in {file}");
    }
    let f = facts(true);
    let r = rules();
    let applied = BTreeMap::from([
        ("Requirement", r.requirement(&f).unwrap().applied()),
        ("Schedule", r.schedule(&f).unwrap().applied()),
        (
            "Penalty",
            r.penalty(&f, day(2027, 4, 15)).unwrap().applied(),
        ),
    ]);
    for (name, applied) in applied {
        assert_eq!(applied.len(), BASIS.len(), "{name}");
        for (i, (_, quote, cite)) in BASIS.iter().enumerate() {
            assert!(
                applied[i].cite() == *cite && applied[i].applied().contains(quote),
                "{name} rule {} is {:?}: {:?}; want {cite:?} quoting {quote:?}",
                i + 1,
                applied[i].cite(),
                applied[i].applied()
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
        assert!(
            !r.required()
                && r.exempt()
                && r.target() == USCents(0)
                && r.options().len() == 1
                && r.target_known(),
            "with tax {with_tax}"
        );
        // The one option is the exception, and it is the target.
        let c = r.chosen().unwrap();
        assert_eq!(c.basis(), Basis::Exception);
        assert_eq!(c.label(), "No income tax");
        assert_eq!(c.amount(), USCents(0));
        for s in [
            "no Tennessee income tax to estimate",
            "2026",
            "January 1, 2021",
        ] {
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
    assert!(
        p.total() == USCents(0)
            && p.lines().is_empty()
            && p.as_of() == as_of
            && !p.upper_bound()
            && p.reason() == "No income tax.",
        "penalty {} to {}, reason {:?}",
        p.total(),
        p.as_of(),
        p.reason()
    );
    assert!(rules().penalty(&facts(false), Date::default()).is_err());
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for year in [2025, 2027] {
        let err = estimated_tax(year).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{year}: {err}");
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
        assert!(
            rules().requirement(&f).is_err(),
            "{name}: Requirement accepted them"
        );
        assert!(
            rules().schedule(&f).is_err(),
            "{name}: Schedule accepted them"
        );
        assert!(
            rules().penalty(&f, day(2027, 4, 15)).is_err(),
            "{name}: Penalty accepted them"
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
        "Hall tax",
        "January 1, 2021",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
    assert_eq!(e.sources().len(), BASIS.len());
}
