//! Every Washington figure in this crate, and the arithmetic at its edges.
//!
//! Go's TestFiguresMatchTheDepartmentsPage, TestSurchargeMatchesTheLegislation
//! and TestCitationsMatchTheStatute read the testdata/ extracts; only their
//! checks of the table's own figures are ported. Go's example and
//! positions tests are here too.

#![allow(clippy::unwrap_used)]

use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_washingtontaxcalc as washingtontaxcalc;
use washingtontaxcalc::{Deductions, Facts, excise_tax};
use working::{PositionKind, Required};

/// The deduction figures and the base rate the Department publishes.
#[test]
fn figures_match_the_departments_page() {
    let table = washingtontaxcalc::table_for(2025).unwrap();
    for (name, got, want) in [
        (
            "standard deduction",
            table.standard_deduction,
            DOLLAR * 278_000,
        ),
        (
            "charitable threshold",
            table.charitable_deduction_threshold,
            DOLLAR * 278_000,
        ),
        (
            "charitable cap",
            table.charitable_deduction_cap,
            DOLLAR * 111_000,
        ),
        (
            "surcharge threshold",
            table.surcharge_threshold,
            DOLLAR * 1_000_000,
        ),
    ] {
        assert_eq!(got, want, "{name}");
    }
    assert_eq!(table.base_rate, percent(7));
}

/// The 2.9% rate, on top of the base rate.
#[test]
fn surcharge_matches_the_legislation() {
    let table = washingtontaxcalc::table_for(2025).unwrap();
    assert_eq!(table.surcharge_rate, Rate(290));
    // "In addition to" is the load-bearing phrase. The top of a large gain
    // meets 9.9%, not 2.9%.
    assert_eq!(table.base_rate.0 + table.surcharge_rate.0, 990);
}

/// The ordinary case: nothing is owed and no return is due.
#[test]
fn gain_below_the_deduction_is_not_taxed() {
    for gain in [
        USCents(0),
        DOLLAR * -50_000, // a net loss
        DOLLAR * 277_999,
        DOLLAR * 278_000, // exactly the deduction
    ] {
        let got = excise_tax(2025, gain, Deductions::default(), Facts::default()).unwrap();
        assert_eq!(got.value().total.0, 0, "a gain of {gain}");
        assert_eq!(got.value().taxable.0, 0, "a gain of {gain}");
    }
}

/// A gain that clears the deduction but not the surcharge threshold.
#[test]
fn base_rate_only() {
    // $500,000 of gain, less the $278,000 deduction, leaves $222,000 at 7%.
    let got = excise_tax(
        2025,
        DOLLAR * 500_000,
        Deductions::default(),
        Facts::default(),
    )
    .unwrap();
    assert_eq!(got.value().taxable, DOLLAR * 222_000);
    assert_eq!(got.value().total, DOLLAR * 15_540);
    assert_eq!(got.value().surcharge_tax.0, 0);
}

/// The threshold is measured against gain after the standard deduction, and
/// the surcharge is on top of the base rate.
#[test]
fn surcharge_applies_above_one_million_of_taxable_gain() {
    let at = excise_tax(
        2025,
        DOLLAR * 1_278_000,
        Deductions::default(),
        Facts::default(),
    )
    .unwrap();
    assert_eq!(at.value().taxable, DOLLAR * 1_000_000);
    assert_eq!(at.value().surcharge_tax.0, 0);
    assert_eq!(at.value().total, DOLLAR * 70_000);

    let over = excise_tax(
        2025,
        DOLLAR * 1_778_000,
        Deductions::default(),
        Facts::default(),
    )
    .unwrap();
    let v = over.value();
    assert_eq!(v.taxable, DOLLAR * 1_500_000);
    assert_eq!(v.at_surcharge_rate, DOLLAR * 500_000);
    // 7% of $1,500,000 is $105,000; 2.9% of $500,000 is $14,500.
    assert_eq!(v.base_tax, DOLLAR * 105_000);
    assert_eq!(v.surcharge_tax, DOLLAR * 14_500);
    assert_eq!(v.total, DOLLAR * 119_500);
}

/// Only donations above the threshold count, and only up to the cap.
#[test]
fn charitable_deduction_is_both_floored_and_capped() {
    for (name, donated, want) in [
        ("below the threshold", DOLLAR * 200_000, USCents(0)),
        ("at the threshold", DOLLAR * 278_000, USCents(0)),
        ("just above", DOLLAR * 288_000, DOLLAR * 10_000),
        ("at the cap", DOLLAR * 389_000, DOLLAR * 111_000),
        ("far above the cap", DOLLAR * 5_000_000, DOLLAR * 111_000),
    ] {
        let got = excise_tax(
            2025,
            DOLLAR * 2_000_000,
            Deductions {
                charitable_donations: donated,
                ..Deductions::default()
            },
            Facts::default(),
        )
        .unwrap();
        assert_eq!(got.value().charitable_deduction, want, "{name}");
    }
}

/// The question this crate cannot answer always comes back.
#[test]
fn every_result_asks_about_allocation() {
    for gain in [USCents(0), DOLLAR * 500_000, DOLLAR * 5_000_000] {
        let got = excise_tax(2025, gain, Deductions::default(), Facts::default()).unwrap();
        assert!(got.needs(), "a gain of {gain}");
        assert!(
            got.required()
                .iter()
                .any(|r| r.position() == PositionKind::CapitalAssetSitus),
            "a gain of {gain}"
        );
    }
}

/// A year this build does not carry is refused.
#[test]
fn unknown_year_is_an_error() {
    for year in [2024, 2026] {
        let err = washingtontaxcalc::table_for(year).err().unwrap();
        assert!(err.is(&washingtontaxcalc::Error::NoTable));
        assert_eq!(
            err.to_string(),
            format!("washingtontaxcalc: no published figures for this tax year: {year}")
        );
    }
    assert!(
        excise_tax(
            2024,
            DOLLAR * 5_000_000,
            Deductions::default(),
            Facts::default()
        )
        .is_err()
    );
    assert_eq!(washingtontaxcalc::years(), [2025]);
}

fn has_position(rs: &[Required], want: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == want)
}

/// Both positions this crate reports can also be settled.
#[test]
fn positions_can_be_answered() {
    let gain = DOLLAR * 2_000_000;
    let unstated = excise_tax(2025, gain, Deductions::default(), Facts::default()).unwrap();
    for want in [PositionKind::CapitalAssetSitus, PositionKind::LotSelection] {
        assert!(has_position(&unstated.required(), want), "{want}");
    }
    let answered = excise_tax(
        2025,
        gain,
        Deductions::default(),
        Facts {
            gain_allocated: true,
            lots_identified: true,
        },
    )
    .unwrap();
    assert!(!answered.needs());
    // Answering a position says something about the filer, not about the
    // arithmetic, so the tax must not move.
    assert_eq!(answered.value().total, unstated.value().total);
    // One at a time.
    let lots = excise_tax(
        2025,
        gain,
        Deductions::default(),
        Facts {
            lots_identified: true,
            ..Facts::default()
        },
    )
    .unwrap();
    assert!(!has_position(&lots.required(), PositionKind::LotSelection));
    assert!(has_position(
        &lots.required(),
        PositionKind::CapitalAssetSitus
    ));
}

/// Go's Example: one large share sale, which meets both rates.
#[test]
fn example() {
    let gain = DOLLAR * 2_278_000;
    let tax = excise_tax(2025, gain, Deductions::default(), Facts::default()).unwrap();
    let mut out = String::new();
    out += &format!("Washington gain  {gain}\n");
    out += &format!("deduction       -{}\n", tax.value().standard_deduction);
    out += &format!("taxable          {}\n", tax.value().taxable);
    out += &format!("tax              {}\n", tax.value().total);
    out += "\nwhy:\n";
    for r in tax.applied() {
        out += &format!("  {}\n", r.cite());
    }
    out += "\nunanswered:\n";
    for r in tax.required() {
        out += &format!("  {}\n", r.position());
    }
    assert_eq!(
        out,
        "Washington gain  $2278000.00
deduction       -$278000.00
taxable          $2000000.00
tax              $169000.00

why:
  RCW 82.87.060(1), as adjusted for 2025
  RCW 82.87.040(1)(a)
  RCW 82.87.040(1)(b), as enacted by ESSB 5813, ch. 421, Laws of 2025

unanswered:
  capital asset situs
  lot selection
"
    );
}
