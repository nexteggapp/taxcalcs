//! Tax year 2025.
//!
//! # Two sources, not one
//!
//! 2026 comes entirely from one revenue procedure. 2025 does not, and
//! assuming it did would produce a standard deduction that is wrong by $750.
//!
//! The rate tables and the capital gains thresholds are Rev. Proc. 2024-40,
//! which set the § 1(j)(2) and § 1(j)(5)(B) figures for taxable years
//! beginning in 2025. Those still stand.
//!
//! The standard deduction and the § 63(f) addition do not. Legislation
//! enacted during 2025 raised the § 63(c) amounts above what that revenue
//! procedure published, so the figures below are the ones the IRS actually
//! printed on the 2025 Form 1040 and in its instructions: $15,750, $31,500
//! and $23,625, against the $15,000 and $30,000 the revenue procedure had.
//! The § 63(f) additions come from the same source, read as the difference
//! between the "under 65" and "65 or older" rows of Chart A.
//!
//! Both extracts are in the Go package's testdata, whose tests check the
//! figures against them.
//!
//! The Social Security wage base is $176,100, read from line 7 of the
//! printed 2025 Schedule SE rather than from either document above.
//!
//! The additional Medicare threshold and the prior-year AGI step are
//! statutory and are not adjusted for inflation, so they appear in no year's
//! revenue procedure.

use std::collections::BTreeMap;

use crate::tables::{Tables, register as register_year};
use crate::{
    Bracket, DOLLAR, FilingStatus, HEAD_OF_HOUSEHOLD, MARRIED_FILING_JOINTLY,
    MARRIED_FILING_SEPARATELY, SINGLE, SURVIVING_SPOUSE, Table, USCents, dollars, percent,
};

/// § 1401(b)(2)(A). Not inflation adjusted.
fn additional_medicare(s: FilingStatus) -> USCents {
    match s {
        MARRIED_FILING_JOINTLY | SURVIVING_SPOUSE => dollars(250_000),
        MARRIED_FILING_SEPARATELY => dollars(125_000),
        _ => dollars(200_000),
    }
}

/// § 6654(d)(1)(C)(i). Not inflation adjusted, and halved for a separate
/// return.
fn agi_step(s: FilingStatus) -> USCents {
    if s == MARRIED_FILING_SEPARATELY {
        return dollars(75_000);
    }
    dollars(150_000)
}

/// Registers 2025's tables.
pub(crate) fn register(tables: &mut Tables) {
    let wage_base = DOLLAR * 176_100; // 2025 Schedule SE line 7

    // Married filing jointly and surviving spouse share TABLE 1 and the
    // § 63(c)(2) amount. They are registered separately because § 63(f)(3)
    // treats only one of them as unmarried.
    let joint = vec![
        Bracket {
            over: USCents(0),
            rate: percent(10),
        },
        Bracket {
            over: dollars(23_850),
            rate: percent(12),
        },
        Bracket {
            over: dollars(96_950),
            rate: percent(22),
        },
        Bracket {
            over: dollars(206_700),
            rate: percent(24),
        },
        Bracket {
            over: dollars(394_600),
            rate: percent(32),
        },
        Bracket {
            over: dollars(501_050),
            rate: percent(35),
        },
        Bracket {
            over: dollars(751_600),
            rate: percent(37),
        },
    ];

    // TABLE 3 and TABLE 4 agree up to the 35% band and part at the top: a
    // separate return reaches 37% at $375,800 rather than $626,350.
    let unmarried_bands = vec![
        Bracket {
            over: USCents(0),
            rate: percent(10),
        },
        Bracket {
            over: dollars(11_925),
            rate: percent(12),
        },
        Bracket {
            over: dollars(48_475),
            rate: percent(22),
        },
        Bracket {
            over: dollars(103_350),
            rate: percent(24),
        },
        Bracket {
            over: dollars(197_300),
            rate: percent(32),
        },
        Bracket {
            over: dollars(250_525),
            rate: percent(35),
        },
        Bracket {
            over: dollars(626_350),
            rate: percent(37),
        },
    ];
    let separate_bands = vec![
        Bracket {
            over: USCents(0),
            rate: percent(10),
        },
        Bracket {
            over: dollars(11_925),
            rate: percent(12),
        },
        Bracket {
            over: dollars(48_475),
            rate: percent(22),
        },
        Bracket {
            over: dollars(103_350),
            rate: percent(24),
        },
        Bracket {
            over: dollars(197_300),
            rate: percent(32),
        },
        Bracket {
            over: dollars(250_525),
            rate: percent(35),
        },
        Bracket {
            over: dollars(375_800),
            rate: percent(37),
        },
    ];

    let table = |ordinary: &[Bracket], sd: i64, aab: i64, zero: i64, fifteen: i64| Table {
        ordinary: ordinary.to_vec(),
        standard_deduction: dollars(sd),
        additional_aged_blind: dollars(aab),
        max_zero_rate_amount: dollars(zero),
        max_15_rate_amount: dollars(fifteen),
        ..Table::default()
    };
    let hoh = vec![
        Bracket {
            over: USCents(0),
            rate: percent(10),
        },
        Bracket {
            over: dollars(17_000),
            rate: percent(12),
        },
        Bracket {
            over: dollars(64_850),
            rate: percent(22),
        },
        Bracket {
            over: dollars(103_350),
            rate: percent(24),
        },
        Bracket {
            over: dollars(197_300),
            rate: percent(32),
        },
        Bracket {
            over: dollars(250_500),
            rate: percent(35),
        },
        Bracket {
            over: dollars(626_350),
            rate: percent(37),
        },
    ];

    let mut by_status = BTreeMap::from([
        (
            MARRIED_FILING_JOINTLY,
            table(&joint, 31_500, 1_600, 96_700, 600_050),
        ),
        (
            SURVIVING_SPOUSE,
            table(&joint, 31_500, 1_600, 96_700, 600_050),
        ),
        // § 63(f)(3) gives an unmarried individual who is not a surviving
        // spouse the larger amount.
        (
            HEAD_OF_HOUSEHOLD,
            table(&hoh, 23_625, 2_000, 64_750, 566_700),
        ),
        (
            SINGLE,
            table(&unmarried_bands, 15_750, 2_000, 48_350, 533_400),
        ),
        (
            MARRIED_FILING_SEPARATELY,
            table(&separate_bands, 15_750, 1_600, 48_350, 300_000),
        ),
    ]);

    for (status, t) in by_status.iter_mut() {
        t.year = 2025;
        t.status = *status;
        t.social_security_wage_base = wage_base;
        t.additional_medicare_threshold = additional_medicare(*status);
        t.prior_year_agi_step = agi_step(*status);
    }

    register_year(tables, 2025, by_status);
}
