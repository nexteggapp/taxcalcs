//! Ported from illinoistaxcalc/estimated_test.go and
//! estimated_refund_test.go: Illinois's estimated tax rules for 2026,
//! checked against the Department of Revenue's own documents.
//!
//! Illinois publishes no exhaustive oracle for estimated tax. The strongest
//! checks are two worked examples from the department: the IL-2210
//! instructions' Penalty Peterson, whose payments and penalty of $121.20 are
//! reproduced line by line, and the annualised installments of 86 Ill. Adm.
//! Code 100.8010(d)(2) Examples 1 to 3. The exceptions, the boundaries and
//! the withholding rules are hand-written cases. The Go checks that compare
//! each figure and quote against the extracts in testdata/ are not ported:
//! they check the Go package's citations, not the rules.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, PartKind, Payment, Penalty, Positions,
    PriorReturn, Status as InstallmentStatus, WaiverCause, WaiverKind, Weekday, Withheld,
};
use filing::Status;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_illinoistaxcalc::{
    EstimatedFacts, EstimatedFactsBuilder, estimated_tax, new_estimated_facts,
};
use working::{PositionKind, Reading, Required};

const ILLINOIS: Jurisdiction = Jurisdiction::from_static("IL");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

fn count(rs: &[Required], k: PositionKind) -> usize {
    rs.iter().filter(|r| r.position() == k).count()
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

fn withheld_on(on: Date, a: USCents) -> Withheld {
    estimated::new_withheld().on(on).amount(a).build().unwrap()
}

fn dues() -> [Date; 4] {
    [
        day(2026, 4, 15),
        day(2026, 6, 15),
        day(2026, 9, 15),
        day(2027, 1, 15),
    ]
}

/// One payment of `a` on each due date.
fn quarterly(a: &[USCents]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .map(|(i, &x)| pay(dues()[i], x))
        .collect()
}

/// Every Illinois fact answered: no nursing home, no pass-through
/// withholding, no interest.
fn answered() -> EstimatedFactsBuilder {
    let mut b = new_estimated_facts();
    b.nursing_home(false)
        .no_pass_through_withholding()
        .interest_on_late_installments(false);
    b
}

#[derive(Clone, Default)]
struct Household {
    /// After credits, IL-1040 lines 14 and 22 less 15 to 17 and 28 to 30.
    tax: USCents,
    no_tax: bool,
    /// One entry on December 15.
    withheld: USCents,
    withholding: Vec<Withheld>,
    as_withheld: bool,
    prior: Option<PriorReturn>,
    no_prior: bool,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    no_extras: bool,
    return_filed: Date,
    /// Annualised tax after credits at each period end.
    periods: Option<Vec<USCents>>,
    farm: Rate,
    fish: Rate,
    waiver: Option<WaiverKind>,
    positions: Option<Positions>,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let figures = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(ILLINOIS, 2026);
        b.filing_status(Status::Single)
            .this_year(figures)
            .paid(&self.payments)
            .return_filed(self.return_filed)
            .farm_share(self.farm)
            .fish_share(self.fish);
        let mut withheld = Vec::new();
        if self.withheld.0 > 0 {
            withheld.push(withheld_on(day(2026, 12, 15), self.withheld));
        }
        withheld.extend(self.withholding.iter().copied());
        b.withheld(&withheld);
        if self.as_withheld {
            b.elections(
                estimated::new_elections()
                    .withholding_as_withheld()
                    .build()
                    .unwrap(),
            );
        }
        match self.prior {
            Some(p) => {
                b.prior_year(p);
            }
            // Large enough never to be the target.
            None if !self.no_prior => {
                b.prior_year(prior(DOLLAR * 1_000_000, USCents(0)).unwrap());
            }
            None => {}
        }
        match &self.extras {
            Some(e) => {
                b.state(e.clone());
            }
            None if !self.no_extras => {
                b.state(answered().build().unwrap());
            }
            None => {}
        }
        if let Some(ps) = &self.periods {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            let periods: Vec<_> = ps
                .iter()
                .enumerate()
                .map(|(i, &t)| {
                    estimated::new_period()
                        .end(ends[i])
                        .annualised_tax(t)
                        .build()
                        .unwrap()
                })
                .collect();
            b.periods(&periods);
            b.elections(estimated::new_elections().annualised().build().unwrap());
        }
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed 12-month return with its tax after credits and its withholding.
fn prior(tax: USCents, withheld: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(
                estimated::new_figures()
                    .tax(tax)
                    .withheld(withheld)
                    .build()
                    .unwrap(),
            )
            .build()
            .unwrap(),
    )
}

fn penalty(h: &Household, as_of: Date) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), as_of).unwrap()
}

/// The 2026 dates are the ones IL-1040-ES prints, and none moves: they are a
/// Wednesday, Monday, Tuesday and Friday.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&tax(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i], "installment {}", i + 1);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
}

/// The IL-2210 instructions' example, moved a year: $1,800 of tax after
/// credits, $2,350 last year, no withholding, and payments of $100, $200,
/// $750, $250 and $400 with the return. The form charges $121.20. In 2026
/// the second due date is June 15 rather than June 16, so the 84 days
/// become 85, which is still 10%; every other count is the same.
#[test]
fn estimated_penalty_matches_il2210_example() {
    let h = Household {
        prior: prior(DOLLAR * 2_350, USCents(0)),
        payments: vec![
            pay(day(2026, 4, 7), DOLLAR * 100),
            pay(day(2026, 6, 9), DOLLAR * 200),
            pay(day(2026, 9, 8), DOLLAR * 750),
            pay(day(2027, 1, 19), DOLLAR * 250),
            pay(day(2027, 5, 10), DOLLAR * 400),
        ],
        ..tax(DOLLAR * 1_800)
    };
    let req = rules().requirement(&h.facts()).unwrap().value();
    assert_eq!(req.target(), DOLLAR * 1_620);
    let res = penalty(&h, day(2027, 6, 30));
    let want: [&[(USCents, i64, i32, i64)]; 4] = [
        &[
            (DOLLAR * 200, 55, 1000, 2_000),
            (DOLLAR * 105, 146, 1000, 1_050),
        ],
        &[(DOLLAR * 405, 85, 1000, 4_050)],
        &[(DOLLAR * 165, 126, 1000, 1_650)],
        &[(DOLLAR * 85, 4, 200, 170), (DOLLAR * 320, 115, 1000, 3_200)],
    ];
    let want_short = [DOLLAR * 305, DOLLAR * 405, DOLLAR * 165, DOLLAR * 405];
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    for (i, l) in lines.iter().enumerate() {
        assert_eq!(l.shortfall(), want_short[i], "quarter {}", i + 1);
        let parts = l.parts();
        assert_eq!(parts.len(), want[i].len(), "quarter {}", i + 1);
        for (j, p) in parts.iter().enumerate() {
            let (principal, days, rate, charge) = want[i][j];
            assert!(
                p.kind() == PartKind::Flat
                    && p.principal() == principal
                    && p.days() == days
                    && p.rate() == Rate(rate)
                    && p.charge() == USCents(charge),
                "quarter {} part {}",
                i + 1,
                j + 1
            );
        }
    }
    assert_eq!(res.value().total(), USCents(12_120));
    assert!(!res.value().upper_bound());
}

/// IL-2210 line 7: no installment when this year's tax less credits and all
/// withholding is $1,000 or less. Pass-through withholding counts.
#[test]
fn estimated_threshold_boundaries() {
    let pt = answered()
        .pass_through_withholding(day(2026, 12, 31), DOLLAR * 500)
        .build()
        .unwrap();
    for (name, t, withheld, extras, exempt) in [
        ("$1,000.00 left", 400_000, 300_000, None, true),
        ("$1,000.01 left", 400_001, 300_000, None, false),
        (
            "$1,000.00 after pass-through",
            400_000,
            250_000,
            Some(pt.clone()),
            true,
        ),
        (
            "$1,000.01 after pass-through",
            400_001,
            250_000,
            Some(pt.clone()),
            false,
        ),
    ] {
        let h = Household {
            withheld: USCents(withheld),
            extras,
            ..tax(USCents(t))
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "{name}: {}",
            res.value().why()
        );
        if exempt {
            let c = res.value().chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount() == USCents(0)
                    && c.label() == "Tax less withholding of $1,000 or less",
                "{name}"
            );
        }
        // $1,000.01 rounds to $1,000, so the reason does not show it as the
        // threshold.
        if !exempt {
            assert!(
                res.value()
                    .why()
                    .contains("just over $1,000. That is more than $1,000"),
                "{name}: why is {:?}",
                res.value().why()
            );
        }
    }
}

/// The target is the lesser of 90% of this year's tax and 100% of last
/// year's, with no 110% tier at any income.
#[test]
fn estimated_target_options() {
    // 90% of $10,000 is $9,000, less than last year's $9,500.
    let h = Household {
        prior: prior(DOLLAR * 9_500, USCents(0)),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(r.chosen().unwrap().basis() == Basis::ThisYearTax && r.target() == DOLLAR * 9_000);
    // Last year's $8,999.99 is less, and stays 100% at $1,000,000 of AGI.
    let f = estimated::new_facts(ILLINOIS, 2026)
        .filing_status(Status::MarriedFilingJointly)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .federal_agi(DOLLAR * 1_000_000)
                .state_agi(DOLLAR * 1_000_000)
                .build()
                .unwrap(),
        )
        .prior_year(prior(USCents(899_999), USCents(0)).unwrap())
        .state(answered().build().unwrap())
        .build()
        .unwrap();
    let r = rules().requirement(&f).unwrap().value();
    let c = r.chosen().unwrap();
    assert!(
        c.basis() == Basis::PriorYearTax
            && c.percent() == percent(100)
            && r.target() == USCents(899_999)
    );
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(
            estimated::new_figures()
                .tax(USCents(100))
                .withheld(USCents(0))
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    for (name, h, ask) in [
        (
            "no return on record",
            Household {
                no_prior: true,
                ..tax(DOLLAR * 10_000)
            },
            true,
        ),
        (
            "not filed",
            Household {
                prior: Some(not_filed),
                ..tax(DOLLAR * 10_000)
            },
            false,
        ),
        (
            "an 11-month year",
            Household {
                prior: Some(short),
                ..tax(DOLLAR * 10_000)
            },
            false,
        ),
    ] {
        let res = rules().requirement(&h.facts()).unwrap();
        let o = &res.value().options()[1];
        assert!(!o.available() && !o.why_not().is_empty(), "{name}");
        assert!(
            res.value().target() == DOLLAR * 9_000 && !res.value().exempt(),
            "{name}"
        );
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn),
            ask,
            "{name}"
        );
    }
}

/// IL-2210's penalty rates: 2% for 1 to 30 days late, 10% for 31 or more,
/// or never paid. An amount still unpaid is charged as of the date figured
/// to.
#[test]
fn estimated_penalty_rate_boundary() {
    // 90% of $4,444.44 is $4,000, so $1,000 a quarter.
    for (paid, as_of, charge) in [
        // On time; 1 day; 30 days; 31 days; unpaid, 30 days so far; unpaid,
        // 31 days so far.
        (day(2026, 4, 15), day(2026, 6, 1), USCents(0)),
        (day(2026, 4, 16), day(2026, 6, 1), DOLLAR * 20),
        (day(2026, 5, 15), day(2026, 6, 1), DOLLAR * 20),
        (day(2026, 5, 16), day(2026, 6, 1), DOLLAR * 100),
        (Date::default(), day(2026, 5, 15), DOLLAR * 20),
        (Date::default(), day(2026, 5, 16), DOLLAR * 100),
    ] {
        let mut h = tax(USCents(444_444));
        if !paid.is_zero() {
            h.payments = vec![pay(paid, DOLLAR * 1_000)];
        }
        assert_eq!(
            penalty(&h, as_of).value().total(),
            charge,
            "paid {paid}, figured to {as_of}"
        );
    }
}

/// Interest is charged only when the caller says it runs, at the uniform
/// rate. The department's page prints one 7% rate for 01/01/2025 to
/// 12/31/2026; a shortfall that runs into 2027 stops at the last published
/// day and reports the gap.
#[test]
fn estimated_interest_only_when_asked() {
    // $1,000 a quarter, never paid.
    let mut h = tax(USCents(444_444));
    let res = penalty(&h, day(2026, 12, 31));
    assert!(
        res.value().total() == DOLLAR * 300
            && !has(&res.required(), PositionKind::JurisdictionFact)
    );
    for l in res.value().lines() {
        for p in l.parts() {
            assert_ne!(
                p.kind(),
                PartKind::Interest,
                "interest charged though not asked for"
            );
        }
    }
    let unanswered = new_estimated_facts()
        .nursing_home(false)
        .no_pass_through_withholding()
        .build()
        .unwrap();
    h.extras = Some(unanswered);
    let res = penalty(&h, day(2026, 12, 31));
    assert!(
        has(&res.required(), PositionKind::JurisdictionFact) && res.value().total() == DOLLAR * 300
    );
    let yes = new_estimated_facts()
        .nursing_home(false)
        .no_pass_through_withholding()
        .interest_on_late_installments(true)
        .build()
        .unwrap();
    h.extras = Some(yes);
    // April's $1,000 for 260 days at 7% ÷ 365 is $49.86, June's for 199 days
    // $38.16, September's for 107 days $20.52; January's is not due yet.
    for as_of in [day(2026, 12, 31), day(2027, 1, 14)] {
        let res = penalty(&h, as_of);
        let mut interest = USCents(0);
        for l in res.value().lines() {
            for p in l.parts() {
                if p.kind() == PartKind::Interest {
                    interest += p.charge();
                    assert_eq!(p.rate(), percent(7));
                }
            }
        }
        assert_eq!(interest, USCents(4_986 + 3_816 + 2_052), "to {as_of}");
        assert_eq!(res.value().total(), DOLLAR * 300 + interest, "to {as_of}");
        assert_eq!(
            has(&res.required(), PositionKind::UnpublishedRate),
            as_of.year() == 2027,
            "to {as_of}"
        );
    }
}

/// Farmers and filers 65 or older in a nursing home owe no estimates; nor
/// does anyone who had no return to file, or no tax due after withholding,
/// last year. Fishermen have no rule of their own.
#[test]
fn estimated_exceptions() {
    let nursing = answered().nursing_home(true).build().unwrap();
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let short_zero = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(
            estimated::new_figures()
                .tax(USCents(300_000))
                .withheld(USCents(300_000))
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let base = tax(DOLLAR * 10_000);
    for (name, h, exempt, why) in [
        (
            "two-thirds from farming",
            Household {
                farm: Rate(6_667),
                ..base.clone()
            },
            true,
            "two-thirds of your gross income is from farming",
        ),
        (
            "just under two-thirds",
            Household {
                farm: Rate(6_666),
                ..base.clone()
            },
            false,
            "",
        ),
        (
            "a fisherman",
            Household {
                fish: Rate(10_000),
                ..base.clone()
            },
            false,
            "",
        ),
        (
            "nursing home",
            Household {
                extras: Some(nursing),
                ..base.clone()
            },
            true,
            "live permanently in a nursing home",
        ),
        (
            "no return required last year",
            Household {
                prior: Some(not_required),
                ..base.clone()
            },
            true,
            "",
        ),
        (
            "last year covered by withholding",
            Household {
                prior: prior(USCents(300_000), USCents(300_000)),
                ..base.clone()
            },
            true,
            "",
        ),
        (
            "last year one cent short",
            Household {
                prior: prior(USCents(300_000), USCents(299_999)),
                ..base.clone()
            },
            false,
            "",
        ),
        (
            "no tax last year",
            Household {
                prior: prior(USCents(0), USCents(0)),
                ..base.clone()
            },
            true,
            "",
        ),
        (
            "covered last year, but six months",
            Household {
                prior: Some(short_zero),
                ..base.clone()
            },
            false,
            "",
        ),
    ] {
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "{name}: {}",
            res.value().why()
        );
        assert!(res.value().why().contains(why), "{name}");
        if exempt {
            let p = penalty(&h, day(2027, 4, 15)).value();
            assert!(p.total() == USCents(0) && p.lines().is_empty(), "{name}");
        }
    }
    // Last year's withholding not given: the test is taken on the tax alone,
    // and the withholding is asked for.
    let no_withholding = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(USCents(300_000))
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let h = Household {
        prior: Some(no_withholding),
        ..base
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().exempt() && has(&res.required(), PositionKind::PriorYearReturn));
}

/// IL-2210 takes each quarter's withholding off that quarter's installment
/// and carries an excess forward, never back. Withholding in June does not
/// pay April's shortfall; it covers June and September. The status reads
/// the running totals, so September is short of the $3,000 needed by then
/// though its own installment is paid.
#[test]
fn estimated_withholding_meets_its_own_quarter() {
    let mut h = Household {
        withholding: vec![withheld_on(day(2026, 6, 1), DOLLAR * 2_000)],
        as_withheld: true,
        ..tax(USCents(444_444))
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    let want_status = [
        InstallmentStatus::Short,
        InstallmentStatus::Met,
        InstallmentStatus::Short,
        InstallmentStatus::Short,
    ];
    let want_short = [DOLLAR * 1_000, USCents(0), USCents(0), DOLLAR * 1_000];
    for (i, inst) in sched.iter().enumerate() {
        assert_eq!(
            (inst.status(), inst.shortfall()),
            (want_status[i], want_short[i]),
            "installment {}",
            i + 1
        );
    }
    // April's $1,000 unpaid, 10%, and January's $1,000, 44 days late by
    // February 28, 10%.
    assert_eq!(penalty(&h, day(2027, 2, 28)).value().total(), DOLLAR * 200);
    // Without the election the same $2,000 is $500 a quarter: each quarter
    // $500 short, all unpaid.
    h.as_withheld = false;
    let p = penalty(&h, day(2027, 2, 28)).value();
    assert!(p.total() == DOLLAR * 200 && p.lines().len() == 4);
}

/// Pass-through withholding counts in full in the quarter whose due date is
/// the first on or after the entity's year end: June 30 is the September
/// quarter.
#[test]
fn estimated_pass_through_withholding_counts_at_the_entitys_year_end() {
    let e = answered()
        .pass_through_withholding(day(2026, 6, 30), DOLLAR * 1_000)
        .build()
        .unwrap();
    let h = Household {
        extras: Some(e),
        payments: quarterly(&[DOLLAR * 1_000, DOLLAR * 1_000, USCents(0), DOLLAR * 1_000]),
        ..tax(USCents(444_444))
    };
    for (i, inst) in rules()
        .schedule(&h.facts())
        .unwrap()
        .value()
        .iter()
        .enumerate()
    {
        assert_eq!(
            inst.status(),
            InstallmentStatus::Met,
            "installment {}",
            i + 1
        );
    }
    // A December 31 year end counts toward January only. September's unpaid
    // $1,000 is not paid by it, as it would be if it were a payment applied
    // to the oldest quarter.
    let e = answered()
        .pass_through_withholding(day(2026, 12, 31), DOLLAR * 1_000)
        .build()
        .unwrap();
    let h = Household {
        extras: Some(e),
        payments: quarterly(&[DOLLAR * 1_000, DOLLAR * 1_000]),
        ..tax(USCents(444_444))
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    assert!(sched[2].shortfall() == DOLLAR * 1_000 && sched[3].shortfall() == USCents(0));
    assert_eq!(penalty(&h, day(2027, 4, 15)).value().total(), DOLLAR * 100);
}

/// Last year's overpayment counts on April 15, or on the day it was elected
/// if that came after the extended due date of last year's return.
#[test]
fn estimated_overpayment_from_last_year() {
    let credit = estimated::new_payment()
        .amount(DOLLAR * 1_000)
        .from_prior_year()
        .build()
        .unwrap();
    let d = dues();
    let mut h = Household {
        payments: vec![
            credit,
            pay(d[1], DOLLAR * 1_000),
            pay(d[2], DOLLAR * 1_000),
            pay(d[3], DOLLAR * 1_000),
        ],
        ..tax(USCents(444_444))
    };
    let res = penalty(&h, day(2027, 4, 15));
    assert!(
        res.value().total() == USCents(0) && has(&res.required(), PositionKind::JurisdictionFact)
    );
    h.extras = Some(
        answered()
            .overpayment_elected_late(day(2026, 6, 1))
            .build()
            .unwrap(),
    );
    // April's $1,000 is paid on June 1, 47 days late: 10%.
    assert_eq!(penalty(&h, day(2027, 4, 15)).value().total(), DOLLAR * 100);
}

/// 86 Ill. Adm. Code 100.8010(d)(2) Examples 1 to 3: a required annual
/// payment of $13,648, so $3,412 a quarter, and annualised installments of
/// $1,278, $1,660 and $3,414, which leave $7,296 for January.
#[test]
fn estimated_annualised_matches_the_rules_examples() {
    // 90% of $15,164.44 is $13,648. The annualised taxes give 22.5% of
    // $5,680 = $1,278; 45% of $6,528.89 = $2,938 less $1,278; and 67.5% of
    // $9,410.37 = $6,352 less $2,938.
    let h = Household {
        periods: Some(vec![
            USCents(568_000),
            USCents(652_889),
            USCents(941_037),
            USCents(1_516_444),
        ]),
        ..tax(USCents(1_516_444))
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [127_800, 293_800, 635_200, 1_364_800]
        .into_iter()
        .enumerate()
    {
        assert_eq!(sched[i].needed_by(), USCents(want), "installment {}", i + 1);
    }
}

/// A return filed with the balance paid stops the penalty that day.
/// Illinois has no January 31 waiver, so January is still charged, at 2%
/// for 14 days.
#[test]
fn estimated_return_filed_and_paid() {
    let h = Household {
        payments: quarterly(&[DOLLAR * 1_000; 3]),
        return_filed: day(2027, 1, 29),
        ..tax(USCents(444_444))
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(sched[3].status(), InstallmentStatus::Short);
    let p = penalty(&h, day(2027, 4, 15)).value();
    assert!(p.as_of() == day(2027, 1, 29) && p.total() == DOLLAR * 20);
}

/// Every Illinois fact a caller can answer is asked for when it is not, and
/// the reasonable-cause question when there is a penalty.
#[test]
fn estimated_unanswered_facts_are_asked() {
    let mut h = Household {
        no_extras: true,
        ..tax(USCents(444_444))
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(count(&res.required(), PositionKind::JurisdictionFact), 2);
    let p = penalty(&h, day(2027, 4, 15));
    assert_eq!(count(&p.required(), PositionKind::JurisdictionFact), 3);
    assert!(has(&p.required(), PositionKind::PenaltyWaiver));
    h.waiver = Some(WaiverKind::Casualty);
    let p = penalty(&h, day(2027, 4, 15));
    assert!(
        !has(&p.required(), PositionKind::PenaltyWaiver)
            && p.value().waiver() == WaiverKind::Casualty
    );
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want = [
        (
            "No liability last year",
            "all Illinois withholding from last year's tax",
            "5/804(d)",
        ),
        (
            "Last year's return as a measure",
            "Column B",
            "5/804(c)(1)(B)(ii)",
        ),
        (
            "Withholding not from wages",
            "includes 1099 withholding",
            "(g)(3)",
        ),
        (
            "How withholding meets the installments",
            "to later quarters only",
            "100.8010(e)(1)",
        ),
        (
            "Choosing actual withholding dates",
            "one-fourth",
            "5/804(g)(1)",
        ),
        ("The $1,000 threshold", "$1,000 or less", "100.8000(a)(1)"),
        ("Exceptions the form does not list", "five cases", "(g)(6)"),
        ("The farmer test", "federal gross income", "5/803(e)"),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, form, source)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(
            d.form().contains(form)
                && d.statute_source().contains(source)
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{:?} does not pin both readings",
            d.rule()
        );
        // Nothing withheld, no farming, no exception: none of the rules
        // changes anything.
        assert!(!d.affects_household(), "{:?}", d.rule());
        let statute = d.rule() == "Last year's return as a measure"
            || d.rule() == "Choosing actual withholding dates";
        assert_eq!(d.uses() == Reading::Statute, statute, "{:?}", d.rule());
    }
    // Withholding is what the withholding rules turn on.
    let h = Household {
        withheld: DOLLAR * 2_000,
        ..tax(DOLLAR * 10_000)
    };
    for d in rules().requirement(&h.facts()).unwrap().divergences() {
        if d.rule() == "Withholding not from wages" {
            assert!(d.affects_household());
        }
    }
    for (name, s) in [
        ("the statute's no-liability test", "had no tax liability"),
        ("the statute's non-wage rule", "on the date it is made"),
        ("the rule's stale threshold", "still says $500"),
        ("the rule's death exception", "death"),
    ] {
        assert!(
            res.divergences().iter().any(|d| d.statute().contains(s)),
            "{name} ({s:?}) is not in any divergence"
        );
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for y in [2025, 2027] {
        let err = estimated_tax(y).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{y}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let fiscal = new_estimated_facts()
        .pass_through_withholding(day(2025, 12, 31), USCents(100))
        .build()
        .unwrap();
    for (name, f) in [
        (
            "Pennsylvania's facts",
            estimated::new_facts(estimated::PENNSYLVANIA, 2026)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(ILLINOIS, 2027)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(ILLINOIS, 2026)
                .filing_status(Status::Single)
                .this_year(this)
                .state("not Illinois's")
                .build()
                .unwrap(),
        ),
        (
            "a 2025 entity year",
            estimated::new_facts(ILLINOIS, 2026)
                .filing_status(Status::Single)
                .this_year(this)
                .state(fiscal)
                .build()
                .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        new_estimated_facts()
            .pass_through_withholding(day(2026, 12, 31), USCents(-1))
            .build()
            .is_err()
    );
    assert!(
        rules()
            .penalty(&tax(USCents(100)).facts(), Date::default())
            .is_err()
    );
}

/// The farming and nursing home exceptions are in the reasons, pinned by
/// the exceptions test.
#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    let all = format!(
        "{}{}{}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$1,000",
        "2%",
        "10%",
        "30 days",
        "90%",
        "100%",
        "not interest",
        "January 31",
        "22.5%",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.body().join(" ").contains('§'));
}

/// Without this year's tax the target is 100% of last year's, the penalty
/// is an upper bound, and with no prior return there is no target. An
/// exception that needs nothing about this year still gives an exact zero.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 4_000, USCents(0)),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 4_000, "{}", r.why());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [100_000, 200_000, 300_000, 400_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), USCents(want), "installment {}", i + 1);
    }
    let pen = penalty(&h, day(2027, 4, 15));
    assert!(pen.value().upper_bound() && pen.value().total() == DOLLAR * 400);
    let none = Household {
        no_tax: true,
        no_prior: true,
        ..Household::default()
    };
    assert!(
        !rules()
            .requirement(&none.facts())
            .unwrap()
            .value()
            .target_known()
    );
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(
        err.is(&estimated::Error::NeedsThisYearTax),
        "schedule: {err}"
    );
    let err = rules()
        .penalty(&none.facts(), day(2027, 4, 15))
        .err()
        .unwrap();
    assert!(
        err.is(&estimated::Error::NeedsThisYearTax),
        "penalty: {err}"
    );
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let exempt = Household {
        no_tax: true,
        prior: Some(not_required),
        ..Household::default()
    };
    let p = penalty(&exempt, day(2027, 4, 15));
    assert!(p.value().total() == USCents(0) && !p.value().upper_bound());
}

/// Illinois has no waiver for retirement or disability, where federal law
/// has one: a retiree the IRS can excuse is refused. A casualty or disaster
/// can be reasonable cause under 86 Ill. Adm. Code 700.400. The penalty is
/// the same before any waiver in each case.
#[test]
fn estimated_waiver_outcomes() {
    let base = penalty(&tax(DOLLAR * 10_000), day(2027, 4, 15))
        .value()
        .total();
    assert_ne!(base, USCents(0), "no penalty to waive");
    let pos = |cause: WaiverCause, on: Date, born: Option<Date>| -> Positions {
        let mut b = estimated::new_positions();
        b.waiver_for(cause, on);
        if let Some(born) = born {
            b.born(born);
        }
        b.build().unwrap()
    };
    for (name, p, want, says) in [
        (
            "a casualty",
            pos(WaiverCause::Casualty, day(2026, 8, 1), None),
            "eligible",
            "fire or other casualty",
        ),
        (
            "a disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
            "eligible",
            "within an IRS disaster extension",
        ),
        (
            "retired in 2026 at 64",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1962, 3, 1)),
            ),
            "refused",
            "no waiver for retirement or disability",
        ),
        (
            "disabled in 2026",
            pos(WaiverCause::Disability, day(2026, 6, 1), None),
            "refused",
            "no waiver for retirement or disability",
        ),
    ] {
        let h = Household {
            positions: Some(p),
            ..tax(DOLLAR * 10_000)
        };
        let p = penalty(&h, day(2027, 4, 15)).value();
        let o = p.waiver_outcome().unwrap();
        let got = format!(
            "{}{}{}",
            if o.eligible() { "eligible" } else { "" },
            if o.refused() { "refused" } else { "" },
            if o.undecided() { "undecided" } else { "" }
        );
        assert_eq!(p.total(), base, "{name}: before any waiver");
        assert!(
            got == want && o.reason().contains(says),
            "{name}: {got} {:?}",
            o.reason()
        );
        assert_eq!(o.eligible(), p.if_waived() == USCents(0), "{name}");
    }
}

/// Under an exception the target is $0, so last year's return isn't asked
/// for as a way to change it. Without one, it is.
#[test]
fn estimated_prior_return_asked_only_when_it_can_set_the_target() {
    let under = Household {
        no_prior: true,
        ..tax(DOLLAR * 800)
    };
    let res = rules().requirement(&under.facts()).unwrap();
    assert!(res.value().exempt() && !has(&res.required(), PositionKind::PriorYearReturn));
    let over = Household {
        no_prior: true,
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&over.facts()).unwrap();
    assert!(has(&res.required(), PositionKind::PriorYearReturn));
}

/// Withholding over the tax reads as a refund, never as a minus amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let rules = rules();
    let mut asked = false;
    for a in estimatedtest::ask(&rules, &estimatedtest::households(&estimated::ILLINOIS)) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked = true;
        let why = a.requirement().unwrap().value().why();
        assert!(
            why.contains("leaves a $600 refund. No estimated payments are needed"),
            "why {why:?}"
        );
        for x in estimatedtest::texts(Some(&rules as &dyn EstimatedTax), std::slice::from_ref(&a)) {
            assert!(
                !x.text().contains("−$") && !x.text().contains("-$"),
                "{}: {:?} has a minus amount",
                x.where_(),
                x.text()
            );
        }
    }
    assert!(asked, "no household withholding over the tax");
}
