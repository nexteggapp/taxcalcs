//! The helpers the Go package's tests share, from builders_test.go and
//! answers_test.go.
#![allow(dead_code, clippy::unwrap_used)]

pub use taxcalcs_estimated as estimated;

use estimated::{Date, Month};
use money::USCents;

pub fn d(year: i64, month: i64, day: i64) -> Date {
    estimated::must_date(year, Month(month), day)
}

pub fn figures(tax: USCents) -> estimated::Figures {
    estimated::new_figures().tax(tax).build().unwrap()
}

pub fn withheld(on: Date, a: USCents) -> estimated::Withheld {
    estimated::new_withheld().on(on).amount(a).build().unwrap()
}

pub fn payment(on: Date, a: i64) -> estimated::Payment {
    estimated::new_payment()
        .on(on)
        .amount(USCents(a))
        .build()
        .unwrap()
}

pub fn base() -> estimated::FactsBuilder {
    let mut b = estimated::new_facts(estimated::CALIFORNIA, 2026);
    b.filing_status(filing::Status::Single)
        .this_year(figures(money::DOLLAR * 10_000));
    b
}

pub fn exception() -> estimated::TargetOption {
    estimated::new_exception("No tax due last year")
        .build()
        .unwrap()
}

/// A requirement with a $900 target.
pub fn chosen() -> estimated::Requirement {
    let o = estimated::new_target_option(
        estimated::Basis::ThisYearTax,
        money::Rate(9000),
        USCents(900),
    )
    .build()
    .unwrap();
    estimated::new_requirement()
        .required(true, "x")
        .option(o)
        .choose(estimated::Basis::ThisYearTax)
        .build()
        .unwrap()
}

pub fn installment(
    n: i64,
    needed: i64,
    credited: i64,
    short: i64,
    excused: bool,
) -> estimated::Installment {
    let mut b = estimated::new_installment(n, d(2026, 4, 15).add_months(2 * (n - 1)));
    b.needed_by(USCents(needed))
        .credited_by(USCents(credited))
        .shortfall(USCents(short));
    if excused {
        b.excused();
    }
    b.build().unwrap()
}
