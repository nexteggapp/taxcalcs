//! Ported from estimated/helpers_test.go: the shared arithmetic, on
//! hand-worked cases.
//!
//! The published worked examples that exercise these helpers are tested in
//! ustaxcalc and californiataxcalc, beside the extracts they come from. What
//! is here are the cases nobody publishes: boundaries, compounding, months
//! counted whole, and the annualised recapture.

#![allow(clippy::unwrap_used)]

mod common;

use common::*;
use money::{DOLLAR, Rate, USCents, percent};

#[test]
fn next_business_day() {
    let c = estimated::new_calendar("test")
        .holiday(d(2026, 9, 7), "Labor Day")
        .build()
        .unwrap();
    for (input, want) in [
        (d(2026, 4, 15), d(2026, 4, 15)), // a Wednesday
        (d(2027, 1, 31), d(2027, 2, 1)),  // Sunday to Monday
        (d(2026, 9, 5), d(2026, 9, 8)),   // Saturday, past Labor Day Monday
        (d(2026, 9, 7), d(2026, 9, 8)),   // the holiday itself
    ] {
        assert_eq!(
            c.next_business_day(input),
            want,
            "{input} ({})",
            input.weekday()
        );
    }
}

#[test]
fn cumulative_shares() {
    // California's 30, 40, 0 and 30 on an amount that does not divide evenly.
    let shares = [Rate(3000), Rate(4000), Rate(0), Rate(3000)];
    let got = estimated::cumulative_shares(USCents(100_001), &shares).unwrap();
    assert_eq!(
        got,
        [
            USCents(30_000),
            USCents(70_001),
            USCents(70_001),
            USCents(100_001)
        ]
    );
    let inc = estimated::increments(&got);
    assert_eq!(inc[2], USCents(0));
    assert_eq!(inc.iter().sum::<USCents>(), USCents(100_001));
    assert_eq!(
        estimated::cumulative_shares(USCents(100), &[Rate(2500), Rate(2500), Rate(2500)])
            .unwrap_err()
            .to_string(),
        "estimated: shares add to 75%, not 100%"
    );
}

/// The Schedule AI recapture, worked by hand with the federal percentages:
/// a filer with nothing in the first quarter, a little by May and most of
/// the year's income by August.
#[test]
fn annualised_recaptures_what_it_lowered() {
    let regular = [USCents(2_500); 4];
    let taxes = [USCents(0), USCents(4_000), USCents(12_000), USCents(12_000)];
    let got = estimated::annualised(
        &taxes,
        &[Rate(2250), Rate(4500), Rate(6750), Rate(9000)],
        &regular,
    )
    .unwrap();
    assert_eq!(
        got,
        [USCents(0), USCents(1_800), USCents(5_700), USCents(2_500)]
    );
}

/// One payment pays the oldest shortfall first, and what is left counts
/// toward the installment it was made for.
#[test]
fn apply_pays_the_oldest_shortfall_first() {
    let dues = [d(2026, 4, 15), d(2026, 6, 15)];
    let got = estimated::apply(
        &dues,
        &[USCents(1_000), USCents(1_000)],
        &[payment(d(2026, 4, 15), 400), payment(d(2026, 5, 1), 900)],
    )
    .unwrap();
    assert_eq!(got[0].shortfall(), USCents(600));
    let pieces = got[0].pieces();
    assert_eq!(pieces.len(), 1);
    assert_eq!(pieces[0].amount(), USCents(600));
    assert_eq!(pieces[0].paid_on(), Some(d(2026, 5, 1)));
    // 300 of the May payment is left for June, which is then 700 short.
    assert_eq!(got[1].shortfall(), USCents(700));
    assert_eq!(got[1].credited_by(), USCents(1_300));
    assert_eq!(got[1].needed_by(), USCents(2_000));
    assert!(
        got[1].pieces()[0].paid_on().is_none(),
        "June's shortfall shows as paid"
    );
}

fn series() -> estimated::RateSeries {
    estimated::new_rate_series()
        .period(d(2026, 4, 1), d(2026, 6, 30), percent(6), "Q2")
        .period(d(2026, 7, 1), d(2026, 12, 31), percent(7), "H2")
        .build()
        .unwrap()
}

/// Simple interest on $10,000 from April 15 to August 14: 76 days at 6% and
/// 45 at 7%, each over 365 and rounded to the cent.
#[test]
fn simple_daily_splits_at_the_rate_change() {
    let (parts, unrated) =
        estimated::simple_daily(DOLLAR * 10_000, d(2026, 4, 15), d(2026, 8, 14), &series())
            .unwrap();
    assert!(unrated.is_zero());
    assert_eq!(parts.len(), 2);
    // 1,000,000¢ × 76 × 0.06 / 365 = 12,493.15¢; × 45 × 0.07 / 365 = 8,630.14¢.
    for (p, (days, charge, source)) in parts.iter().zip([(76, 12_493, "Q2"), (45, 8_630, "H2")]) {
        assert_eq!(
            (p.days(), p.charge(), p.source().as_str()),
            (days, USCents(charge), source)
        );
    }
}

/// Days after the series ends are not charged, and the first of them is
/// reported.
#[test]
fn simple_daily_reports_unrated_days() {
    let (parts, unrated) =
        estimated::simple_daily(DOLLAR * 10_000, d(2026, 9, 15), d(2027, 4, 15), &series())
            .unwrap();
    assert_eq!(unrated, d(2027, 1, 1));
    assert_eq!(parts.len(), 1);
    assert_eq!(parts[0].to(), d(2026, 12, 31));
    assert_eq!(parts[0].days(), 107);
}

/// A leap year divides by 366, so a span across into one is split at the
/// year end; a span between two 365-day years is not.
#[test]
fn simple_daily_divides_by_the_years_length() {
    let s = estimated::new_rate_series()
        .period(d(2027, 1, 1), d(2028, 12, 31), percent(10), "s")
        .build()
        .unwrap();
    let (parts, _) =
        estimated::simple_daily(USCents(36_600), d(2027, 12, 29), d(2028, 1, 2), &s).unwrap();
    assert_eq!(parts.len(), 2);
    assert_eq!((parts[0].days(), parts[1].days()), (2, 2));
    // 36,600¢ × 2 × 0.10 / 365 = 20.05¢; / 366 = 20¢.
    assert_eq!(
        (parts[0].charge(), parts[1].charge()),
        (USCents(20), USCents(20))
    );
}

#[test]
fn compound_daily() {
    let s = estimated::new_rate_series()
        .period(d(2026, 1, 1), d(2026, 12, 31), percent(7), "s")
        .build()
        .unwrap();
    let (parts, _) =
        estimated::compound_daily(DOLLAR * 1_000, d(2026, 1, 1), d(2026, 12, 31), &s).unwrap();
    // 364 days, as 100,000¢ × ((1 + 0.07/365)^364 − 1).
    let want = (100_000.0 * ((1.0f64 + 0.07 / 365.0).powi(364) - 1.0)).round() as i64;
    assert_eq!(parts.len(), 1);
    assert_eq!(parts[0].charge(), USCents(want));
    let (simple, _) =
        estimated::simple_daily(DOLLAR * 1_000, d(2026, 1, 1), d(2026, 12, 31), &s).unwrap();
    assert!(
        parts[0].charge() > simple[0].charge(),
        "compounding charged no more than simple interest"
    );

    // Two spans, the second compounding on the first's charge, against the
    // exact rational figure: 256 bits leaves no room for a rounding
    // difference at these sizes.
    let s = estimated::new_rate_series()
        .period(d(2026, 1, 1), d(2026, 6, 30), percent(7), "a")
        .fine_period(
            d(2026, 7, 1),
            d(2026, 12, 31),
            money::FineRate(108_133),
            "b",
        )
        .build()
        .unwrap();
    let (parts, _) =
        estimated::compound_daily(USCents(123_456_789), d(2026, 2, 3), d(2026, 11, 30), &s)
            .unwrap();
    assert_eq!(parts.len(), 2);
    let exact = |balance: f64, rate: f64, days: i32| {
        (balance * ((1.0 + rate / 365.0).powi(days) - 1.0)).round() as i64
    };
    let first = exact(123_456_789.0, 0.07, parts[0].days() as i32);
    assert_eq!(parts[0].charge(), USCents(first));
    let second = exact(
        123_456_789.0 + first as f64,
        0.108133,
        parts[1].days() as i32,
    );
    assert_eq!(parts[1].charge(), USCents(second));
}

#[test]
fn months_or_part_counts_part_months_whole() {
    for (from, to, want) in [
        (d(2026, 4, 15), d(2026, 4, 15), 0),
        (d(2026, 4, 15), d(2026, 4, 16), 1),
        (d(2026, 4, 15), d(2026, 5, 15), 1),
        (d(2026, 4, 15), d(2026, 5, 16), 2),
        (d(2026, 1, 31), d(2026, 2, 28), 1),
        (d(2026, 1, 31), d(2026, 3, 1), 2),
    ] {
        assert_eq!(estimated::months_or_part(from, to), want, "{from} to {to}");
    }
    let p = estimated::monthly(
        DOLLAR * 1_000,
        d(2026, 4, 15),
        d(2026, 6, 16),
        Rate(50),
        "s",
    )
    .unwrap();
    // Three months or part at 0.5% of $1,000 is $15.
    assert_eq!(p.months(), 3);
    assert_eq!(p.charge(), DOLLAR * 15);
}

#[test]
fn days_across_a_year_end() {
    assert_eq!(d(2027, 4, 15).days_since(d(2026, 6, 30)), 289);
}

/// Credits spreads withholding by the shares whatever the facts elect, and
/// credits_as_withheld puts it on its dates. Which one applies is the
/// jurisdiction's rule, so neither reads the election.
#[test]
fn credits_do_not_read_the_election() {
    let this = estimated::new_figures()
        .tax(USCents(10_000))
        .build()
        .unwrap();
    let w = estimated::new_withheld()
        .on(d(2026, 11, 30))
        .amount(USCents(4_000))
        .build()
        .unwrap();
    let forward = estimated::new_payment()
        .from_prior_year()
        .amount(USCents(500))
        .build()
        .unwrap();
    let elected = estimated::new_elections()
        .withholding_as_withheld()
        .build()
        .unwrap();
    let f = estimated::new_facts(estimated::FEDERAL, 2026)
        .filing_status(filing::Status::Single)
        .this_year(this)
        .withheld(&[w])
        .paid(&[forward])
        .elections(elected)
        .build()
        .unwrap();
    let dues = [
        d(2026, 4, 15),
        d(2026, 6, 15),
        d(2026, 9, 15),
        d(2027, 1, 15),
    ];

    let spread = estimated::credits(&f, &dues, &[Rate(2500); 4]).unwrap();
    // 1,000 on each date, and the 500 brought forward on the first.
    assert_eq!(spread.len(), 5);
    assert_eq!(spread[0].on(), dues[0]);
    assert_eq!(spread[4].on(), dues[3]);
    assert_eq!(spread[4].amount(), USCents(1_000));
    let dated = estimated::credits_as_withheld(&f, &dues).unwrap();
    assert_eq!(dated.len(), 2);
    assert_eq!((dated[0].on(), dated[0].amount()), (dues[0], USCents(500)));
    assert_eq!(
        (dated[1].on(), dated[1].amount()),
        (d(2026, 11, 30), USCents(4_000))
    );
}
