//! Ported from estimated/builders_test.go: builder validation, and that
//! nothing a caller holds can change a value after it is built.

#![allow(clippy::unwrap_used)]

mod common;

use common::*;
use estimated::{Basis, Month, Status};
use money::{DOLLAR, Rate, USCents};

#[test]
fn facts_build_refuses_what_every_jurisdiction_would_refuse() {
    let period = |end| {
        estimated::new_period()
            .end(end)
            .annualised_tax(USCents(100))
            .build()
            .unwrap()
    };
    let annualised = estimated::new_elections().annualised().build().unwrap();
    let single = filing::Status::Single;

    let mut cases: Vec<(&str, estimated::FactsBuilder)> = Vec::new();
    let mut b = estimated::new_facts(estimated::Jurisdiction::new(""), 2026);
    b.filing_status(single).this_year(figures(USCents(1)));
    cases.push(("no jurisdiction", b));
    let mut b = estimated::new_facts(estimated::FEDERAL, 0);
    b.filing_status(single).this_year(figures(USCents(1)));
    cases.push(("no year", b));
    let mut b = estimated::new_facts(estimated::FEDERAL, 2026);
    b.this_year(figures(USCents(1)));
    cases.push(("no filing status", b));
    let mut b = estimated::new_facts(estimated::FEDERAL, 2026);
    b.filing_status(single);
    cases.push(("no figures", b));
    let mut b = base();
    b.withheld(&[withheld(d(2025, 12, 31), USCents(100))]);
    cases.push(("withheld in 2025", b));
    let mut b = base();
    b.withheld(&[withheld(d(2027, 1, 1), USCents(100))]);
    cases.push(("withheld in 2027", b));
    let mut b = base();
    b.paid(&[payment(d(2025, 12, 31), 100)]);
    cases.push(("paid in 2025", b));
    let mut b = base();
    b.return_filed(d(2026, 12, 31));
    cases.push(("filed in 2026", b));
    let mut b = base();
    b.requirement_first_met(d(2027, 1, 2));
    cases.push(("first met in 2027", b));
    let mut b = base();
    b.farm_share(Rate(6000)).fish_share(Rate(4001));
    cases.push(("shares over whole", b));
    let mut b = base();
    b.farm_share(Rate(-1));
    cases.push(("negative share", b));
    let mut b = base();
    b.elections(annualised).periods(&[
        period(d(2026, 3, 31)),
        period(d(2026, 5, 31)),
        period(d(2026, 8, 31)),
    ]);
    cases.push(("annualised, three periods", b));
    let mut b = base();
    b.periods(&[period(d(2026, 5, 31)), period(d(2026, 3, 31))]);
    cases.push(("periods out of order", b));
    for (name, b) in cases {
        assert!(b.build().is_err(), "{name}: built without error");
    }

    // An overpayment applied forward has no date of its own and is allowed.
    let forward = estimated::new_payment()
        .from_prior_year()
        .amount(USCents(100))
        .build()
        .unwrap();
    base().paid(&[forward]).build().unwrap();

    // The messages are Go's.
    assert_eq!(
        base()
            .paid(&[payment(d(2025, 12, 31), 100)])
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: a payment on 2025-12-31 is before tax year 2026 began"
    );
    let mut b = estimated::new_facts(estimated::Jurisdiction::new(""), 2026);
    b.filing_status(single).this_year(figures(USCents(1)));
    assert_eq!(
        b.build().unwrap_err().to_string(),
        "estimated: \"\" is not a jurisdiction code"
    );
    assert_eq!(
        base()
            .farm_share(Rate(6000))
            .fish_share(Rate(4001))
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: farming 60% and fishing 40.01% are not shares of one income"
    );
}

#[test]
fn part_builders_refuse_bad_values() {
    let no_tax = estimated::new_figures()
        .federal_agi(USCents(50_000))
        .build()
        .unwrap();
    assert!(
        no_tax.tax().is_none(),
        "figures built without a tax report one"
    );
    let check = |name: &str, r: Result<(), estimated::Error>| {
        assert!(r.is_err(), "{name}: built without error");
    };
    fn ok<T>(_: T) {}
    check(
        "a prior return whose figures have no tax",
        estimated::new_prior_return()
            .filed(true)
            .figures(no_tax)
            .build()
            .map(ok),
    );
    check(
        "negative tax",
        estimated::new_figures().tax(USCents(-1)).build().map(ok),
    );
    check(
        "negative withholding",
        estimated::new_figures()
            .tax(USCents(1))
            .withheld(USCents(-1))
            .build()
            .map(ok),
    );
    check(
        "a return of 0 months",
        estimated::new_prior_return().months(0).build().map(ok),
    );
    check(
        "a return of 13 months",
        estimated::new_prior_return().months(13).build().map(ok),
    );
    check(
        "filed without figures",
        estimated::new_prior_return().filed(true).build().map(ok),
    );
    check(
        "filed and not required",
        estimated::new_prior_return()
            .filed(true)
            .not_required_to_file()
            .figures(figures(USCents(1)))
            .build()
            .map(ok),
    );
    check(
        "a payment with no date",
        estimated::new_payment().amount(USCents(1)).build().map(ok),
    );
    check(
        "a negative payment",
        estimated::new_payment()
            .on(d(2026, 4, 15))
            .amount(USCents(-1))
            .build()
            .map(ok),
    );
    check(
        "withholding with no date",
        estimated::new_withheld().amount(USCents(1)).build().map(ok),
    );
    check(
        "residence ending before it starts",
        estimated::new_residence_period("CA")
            .from(d(2026, 6, 1))
            .to(d(2026, 5, 31))
            .build()
            .map(ok),
    );
    check(
        "a state that is not a postal code",
        estimated::new_residence_period("California")
            .from(d(2026, 1, 1))
            .to(d(2026, 12, 31))
            .build()
            .map(ok),
    );
    check(
        "retired after 62 at 50",
        estimated::new_person(50).retired_after_62().build().map(ok),
    );
    // Go's "an unknown waiver", WaiverKind(9), cannot be written in Rust.
    check(
        "February 29, 2026",
        estimated::new_date(2026, Month::FEBRUARY, 29).map(ok),
    );
    assert_eq!(
        estimated::new_date(2026, Month::FEBRUARY, 29)
            .unwrap_err()
            .to_string(),
        "estimated: 2026-02-29 is not a date"
    );
    assert_eq!(
        estimated::new_figures()
            .tax(USCents(-1))
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: tax of -$0.01 is negative"
    );
    assert_eq!(
        estimated::new_residence_period("California")
            .from(d(2026, 1, 1))
            .to(d(2026, 12, 31))
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: \"California\" is not a postal code"
    );
}

#[test]
fn output_builders_check_consistency() {
    let small = estimated::new_target_option(Basis::ThisYearTax, Rate(9000), USCents(900))
        .build()
        .unwrap();
    let large = estimated::new_target_option(Basis::PriorYearTax, Rate(10_000), USCents(1_000))
        .build()
        .unwrap();
    let gone = estimated::new_target_option(Basis::PriorYearTax, Rate(10_000), USCents(0))
        .unavailable("no return")
        .build()
        .unwrap();

    let e = estimated::new_requirement()
        .required(true, "x")
        .option(small.clone())
        .option(large)
        .choose(Basis::PriorYearTax)
        .build()
        .unwrap_err();
    assert_eq!(
        e.to_string(),
        "estimated: this year's tax gives $9.00, less than the chosen $10.00"
    );
    assert!(
        estimated::new_requirement()
            .required(true, "x")
            .option(small.clone())
            .option(gone.clone())
            .choose(Basis::PriorYearTax)
            .build()
            .is_err(),
        "a requirement chose an unavailable option"
    );
    assert!(
        estimated::new_requirement()
            .required(true, "x")
            .option(small.clone())
            .exempt(exception())
            .build()
            .is_err(),
        "an exempt requirement said payments were required"
    );
    assert!(
        estimated::new_requirement()
            .required(true, "")
            .option(small.clone())
            .choose(Basis::ThisYearTax)
            .build()
            .is_err(),
        "a requirement without a reason was built"
    );
    let r = estimated::new_requirement()
        .required(true, "x")
        .option(small)
        .option(gone)
        .choose(Basis::ThisYearTax)
        .build()
        .unwrap();
    assert_eq!(r.target(), USCents(900));
    assert!(
        estimated::new_target_option(Basis::ThisYearTax, Rate(9000), USCents(900))
            .unavailable("")
            .build()
            .is_err(),
        "an unavailable option without a reason was built"
    );

    assert!(
        estimated::new_installment(1, d(2026, 4, 15))
            .needed_by(USCents(10))
            .credited_by(USCents(10))
            .shortfall(USCents(5))
            .build()
            .is_err(),
        "an installment with a shortfall was built as met"
    );
    assert_eq!(
        estimated::new_installment(1, d(2026, 4, 15))
            .shortfall(USCents(5))
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: installment 1 is nothing due with a shortfall of $0.05"
    );
    // An exception can excuse an installment that was short: Pennsylvania's
    // REV-1630 Exceptions 1 and 2 take the interest off a shortfall it still
    // reports.
    let i = estimated::new_installment(1, d(2026, 4, 15))
        .needed_by(USCents(10))
        .credited_by(USCents(5))
        .shortfall(USCents(5))
        .excused()
        .build()
        .unwrap();
    assert_eq!(i.status(), Status::Excused);

    let part = estimated::new_penalty_part(estimated::PartKind::Interest, USCents(200), Rate(700))
        .span(d(2026, 4, 15), d(2026, 5, 15))
        .days(30)
        .charge(USCents(1))
        .source("s")
        .build()
        .unwrap();
    assert!(
        estimated::new_penalty_line(1, d(2026, 4, 15), USCents(100))
            .part(part.clone())
            .build()
            .is_err(),
        "a line charged interest on more than its shortfall"
    );
    assert!(
        estimated::new_penalty_part(estimated::PartKind::Interest, USCents(1), Rate(700))
            .span(d(2026, 4, 15), d(2026, 5, 15))
            .build()
            .is_err(),
        "a part without a source was built"
    );
    let line = estimated::new_penalty_line(1, d(2026, 4, 15), USCents(300))
        .part(part)
        .build()
        .unwrap();
    assert_eq!(
        estimated::new_penalty(d(2027, 4, 15))
            .line(line.clone())
            .line(line)
            .because(chosen(), &[])
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: installment 1 has two penalty lines"
    );

    assert!(
        estimated::new_rate_series()
            .period(d(2026, 4, 1), d(2026, 6, 30), Rate(600), "a")
            .period(d(2026, 7, 2), d(2026, 9, 30), Rate(700), "b")
            .build()
            .is_err(),
        "a rate series with a gap was built"
    );
    assert_eq!(
        estimated::new_rate_series()
            .period(d(2026, 4, 1), d(2026, 6, 30), Rate(600), "a")
            .period(d(2026, 6, 30), d(2026, 9, 30), Rate(700), "b")
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: the rate from 2026-06-30 does not follow the period ending 2026-06-30"
    );
    assert!(
        estimated::new_rate_series()
            .period(d(2026, 4, 1), d(2026, 6, 30), Rate(600), "")
            .build()
            .is_err(),
        "a rate without a source was built"
    );
    assert_eq!(
        estimated::new_calendar("s")
            .holiday(d(2026, 7, 3), "a")
            .holiday(d(2026, 7, 3), "b")
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: 2026-07-03 is listed twice"
    );
    assert!(
        estimated::new_explanation("s").source("c").build().is_err(),
        "an explanation without a body was built"
    );
    assert!(
        working::new_divergence("r")
            .form("f", "fs")
            .uses(working::Reading::Form)
            .affects(true)
            .build()
            .is_err(),
        "a divergence without the statute's reading was built"
    );
}

/// A requirement with no target says so, and Build refuses one that claims
/// no target while an option is available.
#[test]
fn requirement_with_no_target() {
    let needs = estimated::new_target_option(Basis::ThisYearTax, Rate(9000), USCents(0))
        .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
        .build()
        .unwrap();
    let none = estimated::new_target_option(Basis::PriorYearTax, Rate(10_000), USCents(0))
        .unavailable("no return")
        .build()
        .unwrap();
    let some = estimated::new_target_option(Basis::PriorYearTax, Rate(10_000), USCents(500))
        .build()
        .unwrap();
    let r = estimated::new_requirement()
        .required(false, "no target yet")
        .option(needs.clone())
        .option(none.clone())
        .no_target()
        .build()
        .unwrap();
    assert!(!r.target_known() && !r.exempt() && r.target() == USCents(0));
    assert!(r.chosen().is_none(), "an option was chosen");

    let cases = [
        (
            "an option available",
            estimated::new_requirement()
                .required(false, "x")
                .option(needs.clone())
                .option(some.clone())
                .no_target()
                .build(),
        ),
        (
            "required",
            estimated::new_requirement()
                .required(true, "x")
                .option(needs.clone())
                .option(none)
                .no_target()
                .build(),
        ),
        (
            "also exempt",
            estimated::new_requirement()
                .required(false, "x")
                .option(needs)
                .no_target()
                .exempt(exception())
                .build(),
        ),
        (
            "also chosen",
            estimated::new_requirement()
                .required(false, "x")
                .option(some.clone())
                .no_target()
                .choose(Basis::PriorYearTax)
                .build(),
        ),
    ];
    for (name, r) in cases {
        assert!(r.is_err(), "{name}: accepted");
    }
    let exempt = estimated::new_requirement()
        .required(false, "x")
        .option(some)
        .exempt(exception())
        .build()
        .unwrap();
    assert!(exempt.exempt() && exempt.target_known());
    let c = exempt.chosen().unwrap();
    assert_eq!(c.basis(), Basis::Exception);
    assert_eq!(c.amount(), USCents(0));
    assert_eq!(exempt.target(), USCents(0));
}

#[test]
fn penalty_upper_bound_survives_to_builder() {
    let p = estimated::new_penalty(d(2027, 4, 15))
        .waiver(estimated::WaiverKind::Casualty)
        .because(chosen(), &[])
        .build()
        .unwrap();
    assert!(!p.upper_bound());
    let b = p.to_builder().upper_bound().build().unwrap();
    assert!(b.upper_bound());
    assert_eq!(b.waiver(), estimated::WaiverKind::Casualty);
    assert_eq!(b.as_of(), p.as_of());
    assert_eq!(b.reason(), p.reason());
}

#[test]
fn accessors_return_copies() {
    let mut b = base();
    b.withheld(&[withheld(d(2026, 3, 1), USCents(100))])
        .paid(&[payment(d(2026, 4, 15), 200)]);
    let f = b.build().unwrap();

    let mut w = f.withholding();
    w[0] = withheld(d(2026, 3, 1), USCents(999));
    let mut p = f.payments();
    p[0] = payment(d(2026, 4, 15), 999);
    b.withheld(&[withheld(d(2026, 5, 1), DOLLAR * 50)]);
    assert_eq!(f.withholding()[0].amount(), USCents(100));
    assert_eq!(f.withholding().len(), 1);
    assert_eq!(f.withheld_total(), USCents(100));
    assert_eq!(f.payments()[0].amount(), USCents(200));

    let opt = estimated::new_target_option(Basis::ThisYearTax, Rate(9000), USCents(900))
        .build()
        .unwrap();
    let r = estimated::new_requirement()
        .required(true, "x")
        .option(opt)
        .choose(Basis::ThisYearTax)
        .build()
        .unwrap();
    r.options()[0] = estimated::TargetOption::default();
    assert_eq!(r.options()[0].amount(), USCents(900));

    let part = estimated::new_penalty_part(estimated::PartKind::Interest, USCents(100), Rate(700))
        .span(d(2026, 4, 15), d(2026, 5, 15))
        .days(30)
        .charge(USCents(1))
        .source("s")
        .build()
        .unwrap();
    let mut lb = estimated::new_penalty_line(1, d(2026, 4, 15), USCents(100));
    lb.part(part.clone());
    let line = lb.build().unwrap();
    lb.part(part);
    line.parts()[0] = estimated::PenaltyPart::default();
    assert_eq!(line.parts().len(), 1);
    assert_eq!(line.parts()[0].charge(), USCents(1));
    let pen = estimated::new_penalty(d(2027, 4, 15))
        .line(line)
        .because(chosen(), &[])
        .build()
        .unwrap();
    pen.lines()[0] = estimated::PenaltyLine::default();
    assert_eq!(pen.lines()[0].total(), USCents(1));
    assert_eq!(pen.total(), USCents(1));

    let e = estimated::new_explanation("s")
        .paragraph("p")
        .source("c")
        .build()
        .unwrap();
    e.body()[0] = "x".to_string();
    assert_eq!(e.body()[0], "p");
    assert_eq!(e.sources()[0], "c");

    let c = estimated::new_calendar("s")
        .holiday(d(2026, 7, 3), "Independence Day (observed)")
        .build()
        .unwrap();
    c.holidays()[0] = estimated::Holiday::default();
    assert!(c.is_holiday(d(2026, 7, 3)).is_some());

    let s = estimated::new_rate_series()
        .period(d(2026, 4, 1), d(2026, 6, 30), Rate(600), "a")
        .build()
        .unwrap();
    s.periods()[0] = estimated::RatePeriod::default();
    assert_eq!(s.ends(), d(2026, 6, 30));

    let applied = estimated::apply(&[d(2026, 4, 15)], &[USCents(100)], &[]).unwrap();
    applied[0].pieces()[0] = estimated::Piece::default();
    assert_eq!(applied[0].pieces()[0].amount(), USCents(100));
}

#[test]
fn dates() {
    assert_eq!(d(2026, 1, 31).add_months(1), d(2026, 2, 28));
    assert_eq!(d(2026, 6, 10).days_since(d(2026, 4, 15)), 56);
    assert_eq!(d(2026, 4, 15).long(), "April 15, 2026");
    assert!(estimated::Date::default().to_string().contains("no date"));
    // Beyond what Go's tests check: weekday, carrying and layouts.
    assert_eq!(d(2026, 4, 15).weekday(), estimated::Weekday::WEDNESDAY);
    assert_eq!(d(2026, 4, 15).weekday().to_string(), "Wednesday");
    assert_eq!(estimated::date_of(2026, Month::FEBRUARY, 30), d(2026, 3, 2));
    assert_eq!(estimated::date_of(2026, Month(13), 1), d(2027, 1, 1));
    assert_eq!(d(2026, 11, 30).add_months(3), d(2027, 2, 28));
    assert_eq!(d(2024, 3, 31).add_months(-1), d(2024, 2, 29));
    assert_eq!(d(2027, 1, 5).format("Jan 2, 2006"), "Jan 5, 2027");
    assert_eq!(d(2027, 1, 5).format("Mon Jan 02"), "Tue Jan 05");
    assert_eq!(Month(13).to_string(), "%!Month(13)");
    assert_eq!(
        d(2026, 1, 1).days_since(estimated::Date::default()),
        106_751
    );
}
