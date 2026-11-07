//! Idaho's estimated tax rules for 2026, checked against the Tax
//! Commission's and the Legislature's own documents.
//!
//! There is no oracle to run, because there is no computation: Idaho
//! requires no estimated payments of individuals. The tests check that every
//! answer says so for any household and that a changed figure changes the
//! Explanation. Go's TestEstimatedFiguresAreInTheirSources, which reads the
//! testdata/ extracts, is not ported.

#![allow(clippy::unwrap_used)]

use estimated::{Basis, Date, EstimatedTax, Facts, Month, Payment, WaiverKind};
use money::{DOLLAR, Rate, USCents};
use taxcalcs_idahotaxcalc as idahotaxcalc;
use working::Reading;

const IDAHO: estimated::Jurisdiction = estimated::IDAHO;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    idahotaxcalc::estimated_tax(2026).unwrap()
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

/// Facts that would need estimated payments federally, and some that would
/// not. Idaho's answer is the same for every one.
fn households() -> Vec<(&'static str, Facts)> {
    let big = estimated::new_figures()
        .tax(DOLLAR * 40_000)
        .state_agi(DOLLAR * 500_000)
        .build()
        .unwrap();
    let last = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 30_000)
                .build()
                .unwrap(),
        )
        .full_year_resident(true)
        .build()
        .unwrap();
    vec![
        (
            "large tax, nothing withheld",
            estimated::new_facts(IDAHO, 2026)
                .filing_status(filing::Status::Single)
                .this_year(big)
                .prior_year(last)
                .build()
                .unwrap(),
        ),
        (
            "this year's tax not given",
            estimated::new_facts(IDAHO, 2026)
                .filing_status(filing::Status::MarriedFilingJointly)
                .this_year(estimated::new_figures().build().unwrap())
                .prior_year(last)
                .build()
                .unwrap(),
        ),
        (
            "no prior return, some paid",
            estimated::new_facts(IDAHO, 2026)
                .filing_status(filing::Status::HeadOfHousehold)
                .this_year(big)
                .paid(&[pay(day(2026, 4, 15), DOLLAR * 1_000)])
                .withheld(&[estimated::new_withheld()
                    .on(day(2026, 6, 30))
                    .amount(DOLLAR * 5_000)
                    .build()
                    .unwrap()])
                .build()
                .unwrap(),
        ),
        (
            "a farmer",
            estimated::new_facts(IDAHO, 2026)
                .filing_status(filing::Status::Single)
                .this_year(big)
                .farm_share(Rate(9_000))
                .build()
                .unwrap(),
        ),
    ]
}

/// No household is required to pay, and none can owe a penalty: Idaho's
/// estimated tax law reaches corporations only.
#[test]
fn estimated_no_individual_is_required_to_pay() {
    for (name, f) in households() {
        let res = rules().requirement(&f).unwrap();
        let r = res.value();
        assert!(
            !r.required()
                && r.exempt()
                && r.target().0 == 0
                && r.target_known()
                && r.options().len() == 1,
            "{name}: want an exemption as the one option"
        );
        // The one option is the exception, and it is the target.
        let c = r.chosen().unwrap();
        assert!(
            c.basis() == Basis::Exception
                && c.amount().0 == 0
                && c.label() == "Idaho doesn't require estimated payments",
            "{name}: chosen {:?}",
            c
        );
        assert!(
            r.why()
                .contains("Idaho does not require individuals to make estimated tax payments"),
            "{name}: why is {:?}",
            r.why()
        );
        // Nothing is asked: no answer the caller could give changes this.
        assert!(res.required().is_empty(), "{name}: asks");
        assert!(
            !res.applied().is_empty() && res.applied()[0].cite().contains("63-3036A"),
            "{name}: the rule does not cite § 63-3036A"
        );
    }
}

/// There are no installments, so no due dates to move.
#[test]
fn estimated_schedule_is_empty() {
    for (name, f) in households() {
        let res = rules().schedule(&f).unwrap();
        assert!(res.value().is_empty(), "{name}");
        assert!(res.required().is_empty(), "{name}");
    }
}

/// The penalty is an exact zero, figured to `as_of` or to the day the return
/// was filed, whichever is earlier.
#[test]
fn estimated_penalty_is_zero() {
    for (name, f) in households() {
        let p = rules().penalty(&f, day(2027, 4, 15)).unwrap().value();
        assert!(
            p.total().0 == 0
                && p.lines().is_empty()
                && !p.upper_bound()
                && p.as_of() == day(2027, 4, 15)
                && p.reason() == "Idaho doesn't require estimated payments.",
            "{name}: {:?}",
            p
        );
    }

    let this = estimated::new_figures()
        .tax(DOLLAR * 40_000)
        .build()
        .unwrap();
    let filed = estimated::new_facts(IDAHO, 2026)
        .filing_status(filing::Status::Single)
        .this_year(this)
        .return_filed(day(2027, 2, 10))
        .positions(
            estimated::new_positions()
                .waiver(WaiverKind::Casualty)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules().penalty(&filed, day(2027, 4, 15)).unwrap();
    assert_eq!(res.value().as_of(), day(2027, 2, 10));
    assert_eq!(res.value().waiver(), WaiverKind::Casualty);
    let applied = res.applied();
    let last = applied.last().unwrap();
    assert!(
        last.applied().contains("nothing to waive"),
        "a waiver request is not answered: {:?}",
        last.applied()
    );
    assert!(rules().penalty(&filed, Date::default()).is_err());
}

/// The Form 40 instructions word the extension's prior-year test as what was
/// paid; the statute and Form 51 say the tax on last year's return.
#[test]
fn estimated_divergences_pin_both_readings() {
    let f = households().remove(0).1;
    for ds in [
        rules().requirement(&f).unwrap().divergences(),
        rules().schedule(&f).unwrap().divergences(),
        rules().penalty(&f, day(2027, 4, 15)).unwrap().divergences(),
    ] {
        assert_eq!(ds.len(), 1);
        let d = &ds[0];
        assert!(
            d.rule() == "What the extension's prior-year test measures"
                && d.form()
                    .contains("100% of state income taxes paid the year before")
                && d.form_source().contains("Form 40 instructions (2025")
                && d.statute().contains("total tax due on last year's return")
                && d.statute_source() == "Idaho Code § 63-3033(a)"
                && d.uses() == Reading::Statute
                && !d.affects_household(),
            "the divergence does not pin both readings: {:?}",
            d
        );
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for year in [2025, 2027] {
        let err = idahotaxcalc::estimated_tax(year).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{year}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let cases = [
        (
            "California's facts",
            estimated::new_facts(estimated::CALIFORNIA, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(IDAHO, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "extras",
            estimated::new_facts(IDAHO, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("not Idaho's")
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

/// The Explanation states the extension test and the interest with the 2026
/// figures, so changing any of them fails here.
#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    let all = format!(
        "{} {} {}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "does not require individuals to make estimated tax payments",
        "applies only to corporations",
        "must be at least one of these",
        "One is 80% of this year's tax",
        "The other is 100% of the tax on last year's return",
        "open only if you filed an Idaho return last year",
        "$50 or less",
        "A penalty of 2% a month",
        "6% a year for 2027",
        "no 110% tier",
        "$1,000",
    ] {
        assert!(all.contains(s), "the explanation does not say {s:?}");
    }
    assert!(!e.sources().is_empty());
}

/// Withholding over the tax prints no minus amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let rules = rules();
    let mut asked = false;
    for a in estimatedtest::ask(&rules, &estimatedtest::households(&IDAHO)) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked = true;
        let why = a.requirement().unwrap().value().why();
        assert!(
            why.contains("Idaho does not require individuals to make estimated tax payments"),
            "why {why:?}"
        );
        for x in estimatedtest::texts(Some(&rules), &[a]) {
            assert!(
                !x.text().contains("−$") && !x.text().contains("-$"),
                "{}: {:?} has a minus amount",
                x.where_(),
                x.text()
            );
        }
    }
    assert!(asked, "no household withholding over the tax");
}
