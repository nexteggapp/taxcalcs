//! Tax year 2025. Ported from y2025.go.
//!
//! The brackets, the standard deduction, the exemption credit and the federal
//! tax subtraction maximum are the four-year table of "Miscellaneous Oregon
//! income tax information" in Publication OR-17, whose last column is 2025.
//! The phase-out steps and the additions for age and blindness are the 2025
//! Form OR-40 instructions. The extracts are in the Go package's testdata and
//! its tables_test.go checks every figure below against them.

use std::collections::BTreeMap;

use filing::Status;
use money::{DOLLAR, Rate, USCents};

use crate::tables::{Bracket, SubtractionStep, Table};

fn bracket(over: USCents, rate: Rate) -> Bracket {
    Bracket { over, rate }
}

fn step(at_least: USCents, maximum: USCents) -> SubtractionStep {
    SubtractionStep { at_least, maximum }
}

/// The 2025 tables by filing status.
pub(crate) fn tables() -> BTreeMap<Status, Table> {
    // ORS 316.037. Four rates, shared by both charts, which differ only in
    // where the bands start.
    let r475 = Rate(475); // 4.75%
    let r675 = Rate(675); // 6.75%
    let r875 = Rate(875); // 8.75%
    let r99 = Rate(990); // 9.9%
    let dollar = DOLLAR;
    let zero = USCents(0);

    // Chart S. Single, or married filing separately.
    let chart_s = vec![
        bracket(zero, r475),
        bracket(dollar * 4_400, r675),
        bracket(dollar * 11_100, r875),
        bracket(dollar * 125_000, r99),
    ];

    // Chart J. Married filing jointly, head of household, or qualifying
    // surviving spouse. Every threshold is twice Chart S's except the top,
    // which is twice $125,000 exactly.
    let chart_j = vec![
        bracket(zero, r475),
        bracket(dollar * 8_800, r675),
        bracket(dollar * 22_200, r875),
        bracket(dollar * 250_000, r99),
    ];

    // Table 4. The federal tax subtraction shrinks in five steps and then
    // stops. A separate filer's figures are half a single filer's
    // throughout.
    let subtraction_single = vec![
        step(zero, dollar * 8_500),
        step(dollar * 125_000, dollar * 6_800),
        step(dollar * 130_000, dollar * 5_100),
        step(dollar * 135_000, dollar * 3_400),
        step(dollar * 140_000, dollar * 1_700),
        step(dollar * 145_000, zero),
    ];
    let subtraction_separate = vec![
        step(zero, dollar * 4_250),
        step(dollar * 125_000, dollar * 3_400),
        step(dollar * 130_000, dollar * 2_550),
        step(dollar * 135_000, dollar * 1_700),
        step(dollar * 140_000, dollar * 850),
        step(dollar * 145_000, zero),
    ];
    let subtraction_joint = vec![
        step(zero, dollar * 8_500),
        step(dollar * 250_000, dollar * 6_800),
        step(dollar * 260_000, dollar * 5_100),
        step(dollar * 270_000, dollar * 3_400),
        step(dollar * 280_000, dollar * 1_700),
        step(dollar * 290_000, zero),
    ];

    let table = |status: Status,
                 ordinary: &Vec<Bracket>,
                 standard: i64,
                 aged_blind: i64,
                 limit: i64,
                 subtraction: &Vec<SubtractionStep>| Table {
        year: 2025,
        status,
        ordinary: ordinary.clone(),
        standard_deduction: dollar * standard,
        additional_aged_blind: dollar * aged_blind,
        exemption_credit: dollar * 256,
        exemption_credit_agi_limit: dollar * limit,
        federal_tax_subtraction: subtraction.clone(),
    };

    let mut by_status = BTreeMap::new();
    by_status.insert(
        Status::Single,
        table(
            Status::Single,
            &chart_s,
            2_835,
            1_200,
            100_000,
            &subtraction_single,
        ),
    );
    // The exemption credit limit is halved with everything else on a
    // separate return.
    by_status.insert(
        Status::MarriedFilingSeparately,
        table(
            Status::MarriedFilingSeparately,
            &chart_s,
            2_835,
            1_000,
            100_000,
            &subtraction_separate,
        ),
    );
    by_status.insert(
        Status::MarriedFilingJointly,
        table(
            Status::MarriedFilingJointly,
            &chart_j,
            5_670,
            1_000,
            200_000,
            &subtraction_joint,
        ),
    );
    by_status.insert(
        Status::SurvivingSpouse,
        table(
            Status::SurvivingSpouse,
            &chart_j,
            5_670,
            1_000,
            200_000,
            &subtraction_joint,
        ),
    );
    // A head of household is unmarried, so the larger addition.
    by_status.insert(
        Status::HeadOfHousehold,
        table(
            Status::HeadOfHousehold,
            &chart_j,
            4_560,
            1_200,
            100_000,
            &subtraction_joint,
        ),
    );
    by_status
}
