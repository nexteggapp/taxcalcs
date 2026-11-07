//! The income tax calculators, ported from tables_test.go, positions_test.go
//! and example_test.go.
//!
//! Go's TestExtractIsComplete, TestTablesMatchThePrintedSchedules and the
//! extract checks in the other tests parse the booklet text in testdata/ and
//! are not ported. TestLadderReproducesEveryPublishedBaseAmount, which checks
//! the ladder against every base amount the booklet prints, is.

#![allow(clippy::unwrap_used)]

use filing::Status;
use money::{DOLLAR, USCents};
use taxcalcs_californiataxcalc::{
    Adjustments, Election, Error, Exemptions, Residency, behavioral_health_services_tax,
    effective_rate, exemption_credit, ordinary_income_tax, table_for, taxable_income, years,
};
use working::{PositionKind, Required};

/// The check that the brackets are right rather than merely transcribed:
/// the tax at each bracket's floor is the base the booklet prints beside it.
#[test]
fn ladder_reproduces_every_published_base_amount() {
    let mut checked = 0;
    for status in Status::ALL {
        let table = table_for(2025, status).unwrap();
        for (i, b) in table.ordinary.iter().enumerate() {
            let got = ordinary_income_tax(2025, status, b.over).unwrap();
            assert_eq!(
                got.value().total,
                b.base_tax,
                "{status} bracket {i}: tax on {}",
                b.over
            );
            checked += 1;
        }
    }
    assert_eq!(checked, 45);
}

/// The booklet works Chris and Pat Smith's joint return at $125,000 of
/// taxable income through Schedule Y and gets $4,768.10.
#[test]
fn the_booklets_worked_example() {
    let got = ordinary_income_tax(2025, Status::MarriedFilingJointly, DOLLAR * 125_000).unwrap();
    assert_eq!(got.value().total, DOLLAR * 4_768 + USCents(10));
}

/// The five standard deductions, as the chart prints them.
#[test]
fn standard_deduction_matches_the_chart() {
    for (status, want) in [
        (Status::Single, 5_706),
        (Status::MarriedFilingJointly, 11_412),
        (Status::MarriedFilingSeparately, 5_706),
        (Status::HeadOfHousehold, 11_412),
        (Status::SurvivingSpouse, 11_412),
    ] {
        let table = table_for(2025, status).unwrap();
        assert_eq!(table.standard_deduction, DOLLAR * want, "{status}");
    }
}

#[test]
fn exemption_credits_match_the_form() {
    let table = table_for(2025, Status::Single).unwrap();
    assert_eq!(table.personal_exemption_credit, DOLLAR * 153);
    assert_eq!(table.dependent_exemption_credit, DOLLAR * 475);
}

/// The worksheet is the whole rule: subtract a million, take one per cent.
#[test]
fn behavioral_health_tax_matches_the_worksheet() {
    let got = behavioral_health_services_tax(2025, Status::Single, DOLLAR * 1_500_000).unwrap();
    assert_eq!(got.value(), DOLLAR * 5_000);
    // And nothing at all at the threshold itself, because § 17043 says "in
    // excess of".
    let at = behavioral_health_services_tax(2025, Status::Single, DOLLAR * 1_000_000).unwrap();
    assert_eq!(at.value(), USCents(0));
}

/// A single filer $6,000 over the $252,203 threshold loses three steps,
/// $18, from each exemption.
#[test]
fn exemption_credit_phase_out() {
    let e = Exemptions {
        personal: 1,
        dependents: 1,
        ..Exemptions::default()
    };
    let got = exemption_credit(2025, Status::Single, DOLLAR * 258_203, e).unwrap();
    assert_eq!(got.value(), DOLLAR * (153 - 18) + DOLLAR * (475 - 18));
    let under = exemption_credit(2025, Status::Single, DOLLAR * 252_203, e).unwrap();
    assert_eq!(under.value(), DOLLAR * 628);
    assert!(
        got.applied()[1]
            .applied()
            .contains("which is 3 step(s) of $2500.00, so each exemption loses $18.00")
    );
}

/// A year this build does not carry is refused rather than served from an
/// adjacent year.
#[test]
fn unknown_year_is_an_error() {
    for year in [2024, 2026] {
        let err = table_for(year, Status::Single).unwrap_err();
        assert!(err.is(&Error::NoTable), "{err}");
        assert_eq!(
            err.to_string(),
            format!("californiataxcalc: no published rate table for this tax year: {year}")
        );
    }
    assert_eq!(years(), vec![2025]);
    assert!(ordinary_income_tax(2024, Status::Single, DOLLAR * 100).is_err());
    assert!(table_for(2025, Status::Unspecified).is_err());
}

fn has_position(rs: &[Required], want: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == want)
}

#[test]
fn residency_can_be_answered() {
    let agi = DOLLAR * 150_000;
    let unstated = taxable_income(
        2025,
        Status::Single,
        agi,
        Adjustments {
            election: Election::Standard,
            ..Adjustments::default()
        },
    )
    .unwrap();
    assert!(has_position(&unstated.required(), PositionKind::Residency));
    let answered = taxable_income(
        2025,
        Status::Single,
        agi,
        Adjustments {
            election: Election::Standard,
            residency: Residency::FullYearResident,
            ..Adjustments::default()
        },
    )
    .unwrap();
    assert!(!has_position(&answered.required(), PositionKind::Residency));
    assert_eq!(answered.value().amount, unstated.value().amount);
    // A filer who says they were not a full-year resident gets an error
    // rather than a Form 540 figure computed for somebody else.
    assert!(
        taxable_income(
            2025,
            Status::Single,
            agi,
            Adjustments {
                residency: Residency::PartYearOrNonResident,
                ..Adjustments::default()
            },
        )
        .is_err()
    );
}

#[test]
fn deduction_method_can_be_answered() {
    let agi = DOLLAR * 150_000;
    let with = |election| Adjustments {
        itemised_deductions: DOLLAR * 9_400,
        election,
        ..Adjustments::default()
    };
    let larger = taxable_income(2025, Status::Single, agi, with(Election::Larger)).unwrap();
    assert!(has_position(
        &larger.required(),
        PositionKind::DeductionMethod
    ));
    let elected = taxable_income(2025, Status::Single, agi, with(Election::Itemise)).unwrap();
    assert!(!has_position(
        &elected.required(),
        PositionKind::DeductionMethod
    ));
    // Electing the standard deduction takes it even though itemising is
    // worth more here, because that is the filer's to decide.
    let standard = taxable_income(2025, Status::Single, agi, with(Election::Standard)).unwrap();
    assert_eq!(standard.value().deduction, DOLLAR * 5_706);
}

/// Go's Example: one salaried California filer from federal adjusted gross
/// income to tax owed, with its printed output.
#[test]
fn example() {
    let (year, agi, itemised) = (2025, DOLLAR * 150_000, DOLLAR * 9_400);
    let status = Status::Single;
    let income = taxable_income(
        year,
        status,
        agi,
        Adjustments {
            itemised_deductions: itemised,
            ..Adjustments::default()
        },
    )
    .unwrap();
    let tax = ordinary_income_tax(year, status, income.value().amount).unwrap();
    let credit = exemption_credit(
        year,
        status,
        agi,
        Exemptions {
            personal: 1,
            ..Exemptions::default()
        },
    )
    .unwrap();

    let mut out = String::new();
    out += &format!("federal AGI    {agi}\n");
    out += &format!(
        "deduction      {} (standard {}, itemised {itemised})\n",
        income.value().deduction,
        income.value().standard
    );
    out += &format!("taxable        {}\n", income.value().amount);
    out += &format!("tax            {}\n", tax.value().total);
    out += &format!("exemptions    -{}\n", credit.value());
    out += &format!("owed           {}\n", tax.value().total - credit.value());
    out += &format!("marginal rate  {}\n", tax.value().marginal);
    out += "\nwhy:\n";
    for b in tax.value().bands {
        if b.income.0 == 0 {
            continue;
        }
        out += &format!("  {} on {} = {}\n", b.rate, b.income, b.tax);
    }
    out += "\nunanswered:\n";
    for r in income.required() {
        out += &format!("  {}\n", r.position());
    }
    assert_eq!(
        out,
        "federal AGI    $150000.00
deduction      $9400.00 (standard $5706.00, itemised $9400.00)
taxable        $140600.00
tax            $9514.44
exemptions    -$153.00
owed           $9361.44
marginal rate  9.3%

why:
  1% on $11079.00 = $110.79
  2% on $15185.00 = $303.70
  4% on $15188.00 = $607.52
  6% on $16090.00 = $965.40
  8% on $15182.00 = $1214.56
  9.3% on $67876.00 = $6312.47

unanswered:
  deduction method
  residency
"
    );
    assert_eq!(
        effective_rate(tax.value().total, income.value().amount),
        money::Rate(677)
    );
    assert_eq!(effective_rate(DOLLAR, USCents(0)), money::Rate(0));
}
