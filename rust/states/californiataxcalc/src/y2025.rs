//! Tax year 2025.
//!
//! The rate schedules, the standard deduction, the exemption credits and the
//! adjusted gross income limitation are the 2025 Personal Income Tax
//! Booklet, which is the Form 540 instructions the Franchise Tax Board
//! publishes. The extracts those were read from are in the Go package's
//! testdata, and its tables_test.go checks every figure below against them.
//!
//! The base tax on each bracket is written as the booklet prints it, in
//! dollars and cents, because it is a published figure rather than one this
//! library derives. The ladder test checks that the ladder reproduces it.

use std::collections::BTreeMap;

use filing::Status;
use money::{DOLLAR, Rate, USCents, percent};

use crate::tables::{Bracket, Table};

// Rev. & Tax. Code § 17041(a). The three schedules share these rates and
// differ only in where the bands start.
const R1: Rate = Rate(100); // 1%
const R2: Rate = Rate(200); // 2%
const R4: Rate = Rate(400); // 4%
const R6: Rate = Rate(600); // 6%
const R8: Rate = Rate(800); // 8%
const R93: Rate = Rate(930); // 9.3%
const R103: Rate = Rate(1030); // 10.3%
const R113: Rate = Rate(1130); // 11.3%
const R123: Rate = Rate(1230); // 12.3%

fn b(over: i64, rate: Rate, base_dollars: i64, base_cents: i64) -> Bracket {
    Bracket {
        over: DOLLAR * over,
        rate,
        base_tax: DOLLAR * base_dollars + USCents(base_cents),
    }
}

/// Every 2025 table, by filing status.
pub(crate) fn tables() -> BTreeMap<Status, Table> {
    // Schedule X. Single, or married/RDP filing separately.
    let schedule_x = vec![
        b(0, R1, 0, 0),
        b(11_079, R2, 110, 79),
        b(26_264, R4, 414, 49),
        b(41_452, R6, 1_022, 1),
        b(57_542, R8, 1_987, 41),
        b(72_724, R93, 3_201, 97),
        b(371_479, R103, 30_986, 19),
        b(445_771, R113, 38_638, 27),
        b(742_953, R123, 72_219, 84),
    ];

    // Schedule Y. Married/RDP filing jointly, or qualifying surviving
    // spouse/RDP.
    let schedule_y = vec![
        b(0, R1, 0, 0),
        b(22_158, R2, 221, 58),
        b(52_528, R4, 828, 98),
        b(82_904, R6, 2_044, 2),
        b(115_084, R8, 3_974, 82),
        b(145_448, R93, 6_403, 94),
        b(742_958, R103, 61_972, 37),
        b(891_542, R113, 77_276, 52),
        b(1_485_906, R123, 144_439, 65),
    ];

    // Schedule Z. Head of household.
    let schedule_z = vec![
        b(0, R1, 0, 0),
        b(22_173, R2, 221, 73),
        b(52_530, R4, 828, 87),
        b(67_716, R6, 1_436, 31),
        b(83_805, R8, 2_401, 65),
        b(98_990, R93, 3_616, 45),
        b(505_208, R103, 41_394, 72),
        b(606_251, R113, 51_802, 15),
        b(1_010_417, R123, 97_472, 91),
    ];

    // The AGI Limitation Worksheet in the booklet. A separate return counts
    // half-sized steps, so the reduction bites twice as fast.
    let phase_out_agi = |s: Status| match s {
        Status::MarriedFilingJointly | Status::SurvivingSpouse => DOLLAR * 504_411,
        Status::HeadOfHousehold => DOLLAR * 378_310,
        _ => DOLLAR * 252_203,
    };
    let phase_out_step = |s: Status| {
        if s == Status::MarriedFilingSeparately {
            DOLLAR * 1_250
        } else {
            DOLLAR * 2_500
        }
    };

    let by_status = [
        (Status::Single, schedule_x.clone(), DOLLAR * 5_706),
        (Status::MarriedFilingSeparately, schedule_x, DOLLAR * 5_706),
        (
            Status::MarriedFilingJointly,
            schedule_y.clone(),
            DOLLAR * 11_412,
        ),
        (Status::SurvivingSpouse, schedule_y, DOLLAR * 11_412),
        // Head of household takes Schedule Z but the joint standard
        // deduction, which is the one place California's chart and its rate
        // schedules disagree about which filers go together.
        (Status::HeadOfHousehold, schedule_z, DOLLAR * 11_412),
    ];

    by_status
        .into_iter()
        .map(|(status, ordinary, standard_deduction)| {
            (
                status,
                Table {
                    year: 2025,
                    status,
                    ordinary,
                    standard_deduction,
                    personal_exemption_credit: DOLLAR * 153,
                    dependent_exemption_credit: DOLLAR * 475,
                    exemption_phase_out_agi: phase_out_agi(status),
                    exemption_phase_out_step: phase_out_step(status),
                    exemption_phase_out_per_step: DOLLAR * 6,
                    behavioral_health_threshold: DOLLAR * 1_000_000,
                    behavioral_health_rate: percent(1),
                },
            )
        })
        .collect()
}
