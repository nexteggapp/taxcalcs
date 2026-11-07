//! Ported from taxyear/payroll/payroll_test.go.

#![allow(clippy::unwrap_used, clippy::expect_used, clippy::type_complexity)]

use filing::Status;
use money::{DOLLAR, Rate, USCents};
use taxcalcs_taxyear::payroll::{self, W4, W4Builder, new_w4};

fn w4(b: &mut W4Builder) -> W4 {
    b.build().unwrap()
}

fn d(n: i64) -> USCents {
    USCents(n * DOLLAR.0)
}

// Worksheet 1A, worked by hand from Rev. Proc. 2025-32's 2026 brackets and
// standard deductions.
#[test]
fn federal_withholding() {
    let cases: Vec<(&str, W4, i64, i64, i64)> = vec![
        (
            "single",
            w4(&mut new_w4(Status::Single)),
            583_333,
            24,
            93_058,
        ),
        (
            "married filing jointly",
            w4(&mut new_w4(Status::MarriedFilingJointly)),
            583_333,
            24,
            54_750,
        ),
        (
            "credits and extra",
            w4(new_w4(Status::Single).dependents(d(2000)).extra(d(50))),
            583_333,
            24,
            93_058 - 8_333 + 5_000,
        ),
        (
            "step 2",
            w4(new_w4(Status::Single).multiple_jobs()),
            583_333,
            24,
            127_363,
        ),
        (
            "exempt",
            w4(new_w4(Status::Single).exempt()),
            583_333,
            24,
            0,
        ),
        (
            "under the deduction",
            w4(&mut new_w4(Status::Single)),
            50_000,
            24,
            0,
        ),
    ];
    for (name, w, taxable, periods, want) in cases {
        let (got, rule) =
            payroll::federal_withholding(2026, &w, USCents(taxable), periods).unwrap();
        assert_eq!(got, USCents(want), "{name}: {}", rule.applied());
    }
    assert!(
        payroll::federal_withholding(2031, &w4(&mut new_w4(Status::Single)), USCents(100), 24)
            .is_err(),
        "a year with no tables gave no error"
    );
}

#[test]
fn flat_supplemental() {
    assert_eq!(payroll::flat_supplemental(d(10_000), USCents(0)), d(2_200));
    // $995,000 paid before: $5,000 at 22% and $5,000 at 37%.
    assert_eq!(
        payroll::flat_supplemental(d(10_000), d(995_000)),
        d(1_100) + d(1_850)
    );
}

#[test]
fn social_security_and_medicare() {
    let ss = payroll::social_security(2026, d(10_000), d(180_000)).unwrap();
    assert_eq!(ss, Rate(620).of(d(4_500)));
    assert_eq!(payroll::medicare(d(10_000), d(195_000)), d(145) + d(45));
}

#[test]
fn plan_limit() {
    for (year, age, base, catch_up) in [
        (2025, 40, 23_500, 0),
        (2025, 50, 23_500, 7_500),
        (2025, 61, 23_500, 11_250),
        (2026, 40, 24_500, 0),
        (2026, 50, 24_500, 8_000),
        (2026, 61, 24_500, 11_250),
        (2026, 64, 24_500, 8_000),
    ] {
        let (b, k) = payroll::plan_limit(year, age).unwrap();
        assert_eq!((b, k), (d(base), d(catch_up)), "{year} age {age}");
    }
    let err = payroll::plan_limit(2024, 40).unwrap_err();
    assert!(err.is(&ustaxcalc::Error::NoTable));
    assert_eq!(
        err.to_string(),
        "payroll: no § 402(g) limit for 2024: ustaxcalc: no published rate table for this tax year"
    );
}
