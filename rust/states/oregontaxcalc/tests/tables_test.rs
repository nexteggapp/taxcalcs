//! Every Oregon figure in this library, checked against what Oregon
//! publishes. Ported from tables_test.go and positions_test.go, with the
//! package example from example_test.go.
//!
//! The oracle is text extracted from the 2025 Form OR-40 instructions and
//! from Publication OR-17, read from the Go package's testdata. The strongest
//! of them is the tax table. Oregon prints its own answer for every band of
//! taxable income below $50,000 across two columns — 1,004 cases nobody here
//! chose. It is also what pinned Oregon's rounding rule, which no prose in
//! either document states.

#![allow(clippy::unwrap_used)]

use filing::Status;
use money::{DOLLAR, USCents};
use regex::Regex;
use taxcalcs_oregontaxcalc as oregontaxcalc;
use working::{PositionKind, Required};

use oregontaxcalc::{Adjustments, Conditions, Election, Residency};

fn read_file(name: &str) -> String {
    let p = golden::repo_path("oregontaxcalc/testdata").join(name);
    std::fs::read_to_string(&p).unwrap_or_else(|e| panic!("read {}: {e}", p.display()))
}

/// Turns "49,900" into cents.
fn parse_whole_dollars(s: &str) -> USCents {
    let n: i64 = s.replace(',', "").parse().unwrap();
    DOLLAR * n
}

#[derive(Clone, Copy, Debug)]
struct TableCase {
    at_least: USCents,
    less_than: USCents,
    /// The Column S figure, for a single or separate filer.
    tax_s: USCents,
    /// Column J, for everybody else.
    tax_j: USCents,
}

fn read_tax_table() -> Vec<TableCase> {
    // One band and its two printed taxes. Applied to every match on a line,
    // because the printed table sets up to four bands side by side.
    let row = Regex::new(r"([\d,]+)\s*—\s*([\d,]+)\s+([\d,]+)\s+([\d,]+)").unwrap();
    let raw = read_file("or40-2025-tax-table.txt");
    let mut cases = Vec::new();
    for line in raw.split('\n') {
        for m in row.captures_iter(line) {
            cases.push(TableCase {
                at_least: parse_whole_dollars(&m[1]),
                less_than: parse_whole_dollars(&m[2]),
                tax_s: parse_whole_dollars(&m[3]),
                tax_j: parse_whole_dollars(&m[4]),
            });
        }
    }
    // Printed left to right across four columns, so document order is not
    // income order.
    cases.sort_by_key(|c| c.at_least.0);
    cases
}

/// The extract covers the whole printed table with no holes and no repeats:
/// every band's floor is the previous band's ceiling, from $0 to $50,000.
fn assert_table_is_complete(cases: &[TableCase]) {
    assert!(!cases.is_empty(), "the tax table extract is empty");
    assert_eq!(cases[0].at_least, USCents(0));
    let mut prev = USCents(0);
    for c in cases {
        assert_eq!(
            c.at_least, prev,
            "the table jumps from {prev} to {}",
            c.at_least
        );
        assert!(c.less_than > c.at_least);
        prev = c.less_than;
    }
    assert_eq!(prev, DOLLAR * 50_000);
}

/// This library against Oregon's own answer for every band the printed
/// table covers, at the band's midpoint.
#[test]
fn against_oregon_tax_table() {
    let cases = read_tax_table();
    assert_table_is_complete(&cases);
    let mut mismatched = Vec::new();
    for c in &cases {
        let midpoint = (c.at_least + c.less_than) / 2;
        for (status, want) in [
            (Status::Single, c.tax_s),
            (Status::MarriedFilingJointly, c.tax_j),
        ] {
            let got = oregontaxcalc::ordinary_income_tax(2025, status, midpoint)
                .unwrap()
                .value()
                .total;
            if got != want {
                mismatched.push(format!(
                    "{midpoint} {status}: got {got}, the table says {want}"
                ));
            }
        }
    }
    assert!(mismatched.is_empty(), "{mismatched:?}");
    assert_eq!(cases.len(), 502);
}

/// The four figures Oregon prints for income above what the tax table
/// covers.
#[test]
fn rate_chart_base_amounts() {
    let text = read_file("or40-2025-rate-charts.txt");
    for (status, at, want, printed) in [
        (Status::Single, 50_000, 4_065, "$4,065"),
        (Status::Single, 125_000, 10_627, "$10,627"),
        (Status::MarriedFilingJointly, 50_000, 3_756, "$3,756"),
        (Status::MarriedFilingJointly, 250_000, 21_256, "$21,256"),
    ] {
        assert!(
            text.contains(printed),
            "the rate charts no longer print {printed}"
        );
        let got = oregontaxcalc::ordinary_income_tax(2025, status, DOLLAR * at)
            .unwrap()
            .value();
        assert_eq!(got.total, DOLLAR * want, "{status} at {at}");
    }
    for want in ["8.75%", "9.9%"] {
        assert!(text.contains(want));
    }
}

#[test]
fn brackets_match_publication_or17() {
    let text = read_file("or17-2025-annual-figures.txt");
    assert!(text.contains("for tax years 2022–2025"));
    for want in [
        "First $4,400",
        "$4,401–$11,100",
        "First $8,800",
        "$8,801–$22,200",
    ] {
        assert!(text.contains(want), "{want:?}");
    }
    let single = oregontaxcalc::table_for(2025, Status::Single).unwrap();
    for (i, want) in [0, 4_400, 11_100, 125_000].into_iter().enumerate() {
        assert_eq!(single.ordinary[i].over, DOLLAR * want);
    }
    let joint = oregontaxcalc::table_for(2025, Status::MarriedFilingJointly).unwrap();
    for (i, want) in [0, 8_800, 22_200, 250_000].into_iter().enumerate() {
        assert_eq!(joint.ordinary[i].over, DOLLAR * want);
    }
}

#[test]
fn standard_deduction_matches_table5() {
    let text = read_file("or40-2025-standard-deduction.txt");
    for (status, printed, want) in [
        (Status::Single, "$2,835", 2_835),
        (Status::MarriedFilingJointly, "$5,670", 5_670),
        (Status::MarriedFilingSeparately, "$2,835", 2_835),
        (Status::HeadOfHousehold, "$4,560", 4_560),
        (Status::SurvivingSpouse, "$5,670", 5_670),
    ] {
        assert!(text.contains(printed));
        let table = oregontaxcalc::table_for(2025, status).unwrap();
        assert_eq!(table.standard_deduction, DOLLAR * want, "{status}");
    }
    // The additions differ by status, which is easy to get backwards.
    assert!(text.contains("$1,200 if single or head of household"));
    for (status, want) in [
        (Status::Single, 1_200),
        (Status::HeadOfHousehold, 1_200),
        (Status::MarriedFilingJointly, 1_000),
        (Status::MarriedFilingSeparately, 1_000),
        (Status::SurvivingSpouse, 1_000),
    ] {
        let table = oregontaxcalc::table_for(2025, status).unwrap();
        assert_eq!(table.additional_aged_blind, DOLLAR * want, "{status}");
    }
}

#[test]
fn spouse_itemising_removes_the_standard_deduction() {
    let text = read_file("or40-2025-standard-deduction.txt");
    assert!(
        text.contains("If your spouse itemizes deductions, your standard deduction is $0")
            || text.contains("If spouse claims itemized deductions")
    );
    let got = oregontaxcalc::standard_deduction(
        2025,
        Status::MarriedFilingSeparately,
        Conditions {
            spouse_itemises: true,
            ..Conditions::default()
        },
    )
    .unwrap();
    assert_eq!(got.value(), USCents(0));
    // And the addition for age does not resurrect it.
    let aged = oregontaxcalc::standard_deduction(
        2025,
        Status::MarriedFilingSeparately,
        Conditions {
            spouse_itemises: true,
            aged_65_or_older: true,
            ..Conditions::default()
        },
    )
    .unwrap();
    assert_eq!(aged.value(), USCents(0));
}

#[test]
fn federal_tax_subtraction_matches_table4() {
    let text = read_file("or40-2025-federal-tax-subtraction.txt");
    for want in [
        "$8,500", "$6,800", "$5,100", "$3,400", "$1,700", "$4,250", "$850",
    ] {
        assert!(text.contains(want));
    }
    // A federal tax liability far larger than any cap, so the answer is the
    // cap itself at every step.
    let huge = DOLLAR * 99_999;
    for (status, agi, want) in [
        (Status::Single, 124_999, 8_500),
        (Status::Single, 125_000, 6_800),
        (Status::Single, 130_000, 5_100),
        (Status::Single, 135_000, 3_400),
        (Status::Single, 140_000, 1_700),
        (Status::Single, 145_000, 0),
        (Status::MarriedFilingSeparately, 0, 4_250),
        (Status::MarriedFilingSeparately, 140_000, 850),
        (Status::MarriedFilingJointly, 249_999, 8_500),
        (Status::MarriedFilingJointly, 250_000, 6_800),
        (Status::MarriedFilingJointly, 290_000, 0),
        (Status::HeadOfHousehold, 250_000, 6_800),
    ] {
        let got = oregontaxcalc::federal_tax_subtraction(2025, status, huge, DOLLAR * agi)
            .unwrap()
            .value();
        assert_eq!(got, DOLLAR * want, "{status} at {agi}");
    }
    // Below the cap the filer's own liability is the limit.
    let small =
        oregontaxcalc::federal_tax_subtraction(2025, Status::Single, DOLLAR * 1_200, USCents(0))
            .unwrap();
    assert_eq!(small.value(), DOLLAR * 1_200);
}

#[test]
fn exemption_credit_is_a_cliff() {
    let text = read_file("or17-2025-annual-figures.txt");
    assert!(text.contains("$256"));
    assert!(text.contains("the exemption credit amount is $0"));
    let credit = |status, agi, n| {
        oregontaxcalc::exemption_credit(2025, status, DOLLAR * agi, n)
            .unwrap()
            .value()
    };
    assert_eq!(credit(Status::Single, 100_000, 2), DOLLAR * 512);
    assert_eq!(credit(Status::Single, 100_001, 2), USCents(0));
    // A joint return gets twice the room.
    assert_eq!(
        credit(Status::MarriedFilingJointly, 200_000, 4),
        DOLLAR * 1_024
    );
}

#[test]
fn unknown_year_is_an_error() {
    for year in [2024, 2026] {
        let err = oregontaxcalc::table_for(year, Status::Single).unwrap_err();
        assert!(err.is(&oregontaxcalc::Error::NoTable), "{err}");
    }
    assert_eq!(oregontaxcalc::years(), [2025]);
    assert!(oregontaxcalc::ordinary_income_tax(2024, Status::Single, DOLLAR * 100).is_err());
    assert_eq!(
        oregontaxcalc::table_for(2024, Status::Single)
            .unwrap_err()
            .to_string(),
        "oregontaxcalc: no published rate table for this tax year: 2024"
    );
}

fn has_position(rs: &[Required], want: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == want)
}

/// Both positions this crate reports can also be settled.
#[test]
fn positions_can_be_answered() {
    let agi = DOLLAR * 95_000;
    let unstated = oregontaxcalc::taxable_income(
        2025,
        Status::Single,
        agi,
        Conditions::default(),
        Adjustments {
            itemised_deductions: DOLLAR * 4_000,
            ..Adjustments::default()
        },
    )
    .unwrap();
    for want in [PositionKind::DeductionMethod, PositionKind::Residency] {
        assert!(has_position(&unstated.required(), want), "{want}");
    }
    let answered = oregontaxcalc::taxable_income(
        2025,
        Status::Single,
        agi,
        Conditions::default(),
        Adjustments {
            itemised_deductions: DOLLAR * 4_000,
            election: Election::Itemise,
            residency: Residency::FullYearResident,
            ..Adjustments::default()
        },
    )
    .unwrap();
    assert!(!answered.needs());
    // A filer who says they were not a full-year resident gets an error
    // rather than a Form OR-40 figure computed for somebody else.
    assert!(
        oregontaxcalc::taxable_income(
            2025,
            Status::Single,
            agi,
            Conditions::default(),
            Adjustments {
                residency: Residency::PartYearOrNonResident,
                ..Adjustments::default()
            },
        )
        .is_err()
    );
}

/// The package example: one salaried Oregon filer from federal adjusted
/// gross income to tax owed.
#[test]
fn example() {
    let (year, agi, federal_tax, exemptions) = (2025, DOLLAR * 95_000, DOLLAR * 12_000, 1);
    let status = Status::Single;
    let subtraction =
        oregontaxcalc::federal_tax_subtraction(year, status, federal_tax, agi).unwrap();
    let income = oregontaxcalc::taxable_income(
        year,
        status,
        agi,
        Conditions::default(),
        Adjustments {
            federal_tax_subtraction: subtraction.value(),
            ..Adjustments::default()
        },
    )
    .unwrap()
    .value();
    let tax = oregontaxcalc::ordinary_income_tax(year, status, income.amount)
        .unwrap()
        .value();
    let credit = oregontaxcalc::exemption_credit(year, status, agi, exemptions)
        .unwrap()
        .value();

    let mut out = String::new();
    out += &format!("federal AGI     {agi}\n");
    out += &format!("deduction       {}\n", income.deduction.amount);
    out += &format!(
        "federal tax     {} subtracted, capped at $8500.00\n",
        income.federal_tax_subtraction
    );
    out += &format!("taxable         {}\n", income.amount);
    out += &format!("tax             {}\n", tax.total);
    out += &format!("exemptions     -{credit}\n");
    out += &format!("owed            {}\n", tax.total - credit);
    out += "\nwhy, each bracket rounded to a whole dollar as Oregon does:\n";
    for b in &tax.bands {
        if b.income.0 == 0 {
            continue;
        }
        out += &format!("  {} on {} = {}\n", b.rate, b.income, b.tax);
    }
    assert_eq!(
        out,
        "federal AGI     $95000.00
deduction       $2835.00
federal tax     $8500.00 subtracted, capped at $8500.00
taxable         $83665.00
tax             $7010.00
exemptions     -$256.00
owed            $6754.00

why, each bracket rounded to a whole dollar as Oregon does:
  4.75% on $4400.00 = $209.00
  6.75% on $6700.00 = $452.00
  8.75% on $72565.00 = $6349.00
"
    );
    assert_eq!(
        oregontaxcalc::effective_rate(tax.total, agi),
        money::Rate(738)
    );
}
