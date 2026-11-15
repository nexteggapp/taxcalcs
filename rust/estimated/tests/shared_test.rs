//! Ported from estimated/shared_test.go and factor_test.go: the helpers
//! several states share, on hand-worked cases.

#![allow(clippy::unwrap_used)]

mod common;

use common::*;
use estimated::Rounding;
use money::{DOLLAR, Rate, USCents, percent};
use num_bigint::BigInt;

#[test]
fn late_start_counts_the_groups_on_or_before_the_day() {
    let groups = [d(2026, 4, 1), d(2026, 6, 1), d(2026, 9, 1)];
    for (met, want) in [
        (estimated::Date::default(), 0),
        (d(2026, 3, 31), 0),
        (d(2026, 4, 1), 1),
        (d(2026, 5, 31), 1),
        (d(2026, 6, 1), 2),
        (d(2026, 9, 1), 3),
        (d(2026, 12, 31), 3),
    ] {
        assert_eq!(estimated::late_start(met, &groups), want, "met {met}");
    }
}

#[test]
fn equal_shares_put_the_odd_cent_where_rounding_does() {
    // $1,000.00 over the last three of four: 333.33, 666.67, 1,000.00.
    let got = estimated::equal_shares(USCents(100_000), 4, 1).unwrap();
    assert_eq!(
        got,
        [
            USCents(0),
            USCents(33_333),
            USCents(66_667),
            USCents(100_000)
        ]
    );
    // From the start it is four quarters, as cumulative_shares gives.
    let all = estimated::equal_shares(USCents(100_001), 4, 0).unwrap();
    let quarters = estimated::cumulative_shares(USCents(100_001), &[Rate(2500); 4]).unwrap();
    assert_eq!(all, quarters);
    for (annual, count, first) in [(100, 4, 4), (100, 4, -1), (100, 0, 0), (-100, 4, 0)] {
        assert!(
            estimated::equal_shares(USCents(annual), count, first).is_err(),
            "{annual} over {count} from {first} was accepted"
        );
    }
    assert_eq!(
        estimated::equal_shares(USCents(100), 4, 4)
            .unwrap_err()
            .to_string(),
        "estimated: equal shares from installment 5 of 4"
    );
}

#[test]
fn resident_all_year_needs_one_period_for_the_whole_year() {
    let period = |state: &str, from, to| {
        estimated::new_residence_period(state)
            .from(from)
            .to(to)
            .build()
            .unwrap()
    };
    let whole = period("ND", d(2026, 1, 1), d(2026, 12, 31));
    let first_half = period("ND", d(2026, 1, 1), d(2026, 6, 30));
    let second_half = period("ND", d(2026, 7, 1), d(2026, 12, 31));
    let elsewhere = period("MN", d(2026, 1, 1), d(2026, 12, 31));
    for (name, periods, want) in [
        ("all year", vec![whole.clone()], true),
        ("two halves", vec![first_half, second_half], false),
        ("another state", vec![elsewhere.clone()], false),
        ("none", vec![], false),
        ("another state, too", vec![elsewhere, whole.clone()], true),
    ] {
        assert_eq!(
            estimated::resident_all_year(&periods, &estimated::NORTH_DAKOTA, 2026),
            want,
            "{name}"
        );
    }
    assert!(!estimated::resident_all_year(
        &[whole],
        &estimated::NORTH_DAKOTA,
        2027
    ));
}

/// Two columns of $1,000 each. $400 is credited by April 15 and $900 on May
/// 1, so April's $600 is paid on May 1. June's column measures every credit
/// by June 15 against $2,000, so $700 is under and runs to the stop.
#[test]
fn columns_measure_each_column_on_its_own() {
    let dues = [d(2026, 4, 15), d(2026, 6, 15), d(2026, 9, 15)];
    let credits = [
        payment(d(2026, 5, 1), 900),
        payment(d(2026, 4, 15), 400),
        payment(d(2026, 7, 1), 100),
    ];
    let cols = estimated::columns(
        &dues,
        &[USCents(1_000), USCents(2_000), USCents(3_000)],
        &credits,
        d(2027, 4, 15),
        d(2026, 8, 1),
    )
    .unwrap();
    assert_eq!(cols.len(), 2);
    let (april, june) = (&cols[0], &cols[1]);
    assert_eq!(april.under(), USCents(600));
    assert_eq!(april.credited_by(), USCents(400));
    assert_eq!(april.end(), d(2026, 6, 15));
    let p = april.pieces();
    assert_eq!(p.len(), 1);
    assert_eq!(p[0].amount(), USCents(600));
    assert_eq!(p[0].paid_on(), Some(d(2026, 5, 1)));
    assert_eq!(april.through(), d(2026, 5, 1));
    assert_eq!(june.under(), USCents(700));
    assert_eq!(june.end(), d(2026, 8, 1));
    let p = june.pieces();
    assert_eq!(p.len(), 2);
    assert_eq!((p[0].amount(), p[1].amount()), (USCents(100), USCents(600)));
    assert!(p[1].paid_on().is_none());
    assert_eq!(june.through(), d(2026, 8, 1));
    assert!(
        estimated::columns(
            &dues,
            &[USCents(1), USCents(2)],
            &[],
            d(2027, 4, 15),
            d(2027, 4, 15)
        )
        .is_err(),
        "two running totals for three due dates were accepted"
    );
}

#[test]
fn rounding_div_rounds_once_to_the_unit() {
    for (r, n, dv, want) in [
        // $1,234.50 ÷ 3 is $411.50, which is $412 to the dollar.
        (Rounding::RoundToDollar, 123_450, 3, 41_200),
        // $1,234.49 ÷ 3 is $411.4966…, which is $411; rounding to the cent
        // first would give $411.50 and then $412.
        (Rounding::RoundToDollar, 123_449, 3, 41_100),
        (Rounding::RoundToCent, 123_449, 3, 41_150),
        (Rounding::RoundToCent, 1, 2, 1),
    ] {
        assert_eq!(
            r.div(&BigInt::from(n), &BigInt::from(dv)),
            USCents(want),
            "{n} ÷ {dv} {r}"
        );
    }
    assert_eq!(
        Rounding::RoundToDollar.round(USCents(12_350)),
        USCents(12_400)
    );
    assert_eq!(
        Rounding::RoundToDollar.round(USCents(-12_350)),
        USCents(-12_400)
    );
}

/// Maryland's 10.8133% on $10,000 for the 365 days of 2026 is $1,081.33,
/// where 10.81% would give $1,081.00.
#[test]
fn simple_daily_charges_a_fine_rate_as_published() {
    let s = estimated::new_rate_series()
        .fine_period(
            d(2026, 1, 1),
            d(2026, 12, 31),
            money::FineRate(108_133),
            "test",
        )
        .build()
        .unwrap();
    let (parts, _) =
        estimated::simple_daily(DOLLAR * 10_000, d(2025, 12, 31), d(2026, 12, 31), &s).unwrap();
    assert_eq!(parts.len(), 1);
    assert_eq!(parts[0].charge(), USCents(108_133));
    assert_eq!(parts[0].fine_rate(), money::FineRate(108_133));
    assert_eq!(parts[0].rate(), Rate(1081));
    // A rate added in basis points charges what it did before.
    let bp = estimated::new_rate_series()
        .period(d(2026, 1, 1), d(2026, 12, 31), Rate(1081), "test")
        .build()
        .unwrap();
    let (same, _) =
        estimated::simple_daily(DOLLAR * 10_000, d(2025, 12, 31), d(2026, 12, 31), &bp).unwrap();
    assert_eq!(same[0].charge(), USCents(108_100));
    assert_eq!(same[0].fine_rate(), money::FineRate(108_100));
}

fn factor(printed: &str) -> estimated::Factor {
    estimated::parse_factor(printed).unwrap()
}

#[test]
fn parse_factor() {
    for ok in ["0.000192", "0.0192", "1", ".5"] {
        assert_eq!(estimated::parse_factor(ok).unwrap().to_string(), ok);
    }
    for bad in ["", ".", "0", "0.000", "-0.1", "0.00a", "0.1.2", "1e-4"] {
        assert!(
            estimated::parse_factor(bad).is_err(),
            "{bad:?} was accepted"
        );
    }
    assert_eq!(
        estimated::parse_factor("0.000").unwrap_err().to_string(),
        "estimated: a factor of \"0.000\" charges nothing"
    );
}

/// The printed factor is used as printed, not the rate it came from:
/// $10,000 for 56 days at 0.000192 is $107.52, where 7% ÷ 365 would give
/// $107.40.
#[test]
fn daily_factor_uses_the_printed_factor() {
    let f = factor("0.000192");
    for (r, want) in [
        (Rounding::RoundToCent, USCents(10_752)),
        (Rounding::RoundToDollar, DOLLAR * 108),
    ] {
        let s = estimated::new_factor_series(r)
            .period(
                d(2026, 1, 1),
                d(2026, 12, 31),
                f.clone(),
                percent(7),
                "REV-1611",
            )
            .build()
            .unwrap();
        let (parts, unrated) =
            estimated::daily_factor(DOLLAR * 10_000, d(2026, 4, 15), d(2026, 6, 10), &s).unwrap();
        assert!(unrated.is_zero());
        assert_eq!(parts.len(), 1);
        assert_eq!(parts[0].days(), 56);
        assert_eq!(parts[0].charge(), want, "rounded {r}");
        assert_eq!(parts[0].rate(), percent(7));
        assert_eq!(parts[0].source(), "REV-1611");
    }
}

/// A span crossing into a period with another factor is charged in two
/// parts, each rounded on its own, and days past the last factor are
/// reported.
#[test]
fn daily_factor_splits_at_each_period_and_stops_where_they_end() {
    let s = estimated::new_factor_series(Rounding::RoundToDollar)
        .period(
            d(2024, 1, 1),
            d(2024, 12, 31),
            factor("0.000219"),
            percent(8),
            "REV-1611 2024",
        )
        .period(
            d(2025, 1, 1),
            d(2025, 12, 31),
            factor("0.000192"),
            percent(7),
            "REV-1611 2025",
        )
        .build()
        .unwrap();
    let (parts, unrated) =
        estimated::daily_factor(DOLLAR * 1_000, d(2024, 12, 1), d(2026, 1, 10), &s).unwrap();
    assert_eq!(unrated, d(2026, 1, 1));
    // 30 days at 0.000219 is $6.57, so $7; 365 at 0.000192 is $70.08, so $70.
    assert_eq!(parts.len(), 2);
    assert_eq!((parts[0].days(), parts[0].charge()), (30, DOLLAR * 7));
    assert_eq!((parts[1].days(), parts[1].charge()), (365, DOLLAR * 70));
}

/// Maryland's Form 502UP prints one factor per span between due dates; for
/// 2025 the first is 0.0192, from April 15 to June 15.
#[test]
fn span_factor() {
    let p = estimated::span_factor(
        DOLLAR * 1_000,
        d(2025, 4, 15),
        d(2025, 6, 15),
        &factor("0.0192"),
        Rate(1148),
        Rounding::RoundToCent,
        "Form 502UP (2025)",
    )
    .unwrap();
    assert_eq!(p.charge(), USCents(1_920));
    assert_eq!(p.days(), 61);
    assert!(
        estimated::span_factor(
            USCents(100),
            d(2025, 4, 15),
            d(2025, 6, 15),
            &estimated::Factor::default(),
            Rate(0),
            Rounding::RoundToCent,
            "x"
        )
        .is_err(),
        "a zero factor was accepted"
    );
    assert!(
        estimated::span_factor(
            USCents(100),
            d(2025, 4, 15),
            d(2025, 6, 15),
            &factor("0.0192"),
            Rate(0),
            Rounding::Unspecified,
            "x"
        )
        .is_err(),
        "no rounding was accepted"
    );
}

#[test]
fn factor_series_build_refuses_gaps_and_unsourced_factors() {
    let f = factor("0.000192");
    let mk =
        |r: Rounding, periods: &[(estimated::Date, estimated::Date, estimated::Factor, &str)]| {
            let mut b = estimated::new_factor_series(r);
            for (from, to, f, src) in periods {
                b.period(*from, *to, f.clone(), Rate(700), *src);
            }
            b.build()
        };
    let year = (d(2026, 1, 1), d(2026, 12, 31));
    let cases = [
        (
            "no rounding",
            mk(Rounding::Unspecified, &[(year.0, year.1, f.clone(), "x")]),
        ),
        ("empty", mk(Rounding::RoundToCent, &[])),
        (
            "no source",
            mk(Rounding::RoundToCent, &[(year.0, year.1, f.clone(), "")]),
        ),
        (
            "no factor",
            mk(
                Rounding::RoundToCent,
                &[(year.0, year.1, estimated::Factor::default(), "x")],
            ),
        ),
        (
            "backwards",
            mk(Rounding::RoundToCent, &[(year.1, year.0, f.clone(), "x")]),
        ),
        (
            "gap",
            mk(
                Rounding::RoundToCent,
                &[
                    (d(2025, 1, 1), d(2025, 12, 30), f.clone(), "x"),
                    (year.0, year.1, f.clone(), "x"),
                ],
            ),
        ),
    ];
    for (name, r) in cases {
        assert!(r.is_err(), "{name}: accepted");
    }
}
