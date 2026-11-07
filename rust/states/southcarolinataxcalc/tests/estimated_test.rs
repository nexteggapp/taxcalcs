//! South Carolina's estimated tax rules for 2026, ported from the Go
//! package's estimated_test.go. (Go also compares every figure, and the
//! calendar against Revenue Procedure #23-1's own rules, with their extracts
//! in testdata/; those checks stay in Go.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status, WaiverCause,
    WaiverKind,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_southcarolinataxcalc::{
    EstimatedFacts, estimated_calendar, estimated_tax, new_estimated_facts,
};
use working::{PositionKind, Reading, Required, Rule};

const SC: estimated::Jurisdiction = estimated::SOUTH_CAROLINA;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

/// Whether a required position of kind `k` mentions `s`.
fn asks(rs: &[Required], k: PositionKind, s: &str) -> bool {
    rs.iter()
        .any(|r| r.position() == k && (r.question().contains(s) || r.effect().contains(s)))
}

/// Whether a rule's working contains `s`.
fn applied(rs: &[Rule], s: &str) -> bool {
    rs.iter().any(|r| r.applied().contains(s))
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

fn dues() -> [Date; 4] {
    [
        day(2026, 4, 15),
        day(2026, 6, 15),
        day(2026, 9, 15),
        day(2027, 1, 15),
    ]
}

#[derive(Default, Clone)]
struct Household {
    status: Option<filing::Status>,
    /// SC2210 Part I line 3.
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    withheld_on: Date,
    as_withheld: bool,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    waiver: Option<WaiverKind>,
    positions: Option<Positions>,
    /// Schedule AI tax for each period.
    annualised: Vec<USCents>,
}

impl Household {
    fn facts(&self) -> Facts {
        let status = self.status.unwrap_or(filing::Status::Single);
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        let mut b = estimated::new_facts(SC, 2026);
        b.filing_status(status)
            .this_year(fb.build().unwrap())
            .paid(&self.payments)
            .requirement_first_met(self.first_met)
            .return_filed(self.return_filed);
        if self.withheld.0 > 0 {
            let mut on = self.withheld_on;
            if on.is_zero() {
                on = day(2026, 12, 15);
            }
            b.withheld(&[estimated::new_withheld()
                .on(on)
                .amount(self.withheld)
                .build()
                .unwrap()]);
        }
        if self.as_withheld {
            b.elections(
                estimated::new_elections()
                    .withholding_as_withheld()
                    .build()
                    .unwrap(),
            );
        }
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        b.state(self.extras.unwrap_or_else(answered));
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        if !self.annualised.is_empty() {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            let periods: Vec<_> = self
                .annualised
                .iter()
                .enumerate()
                .map(|(i, t)| {
                    estimated::new_period()
                        .end(ends[i])
                        .annualised_tax(*t)
                        .build()
                        .unwrap()
                })
                .collect();
            b.periods(&periods)
                .elections(estimated::new_elections().annualised().build().unwrap());
        }
        b.build().unwrap()
    }
}

/// Every South Carolina fact answered no.
fn answered() -> EstimatedFacts {
    new_estimated_facts()
        .prior_year_farmer_or_fisher(false)
        .underpayment_from_other_state_income(false)
        .nonresident_contractor(false)
        .build()
        .unwrap()
}

/// A filed twelve-month 2025 SC1040 with its tax and AGI, federal and South
/// Carolina the same.
fn last(tax: USCents, agi: USCents) -> Option<PriorReturn> {
    let f = estimated::new_figures()
        .tax(tax)
        .state_agi(agi)
        .federal_agi(agi)
        .build()
        .unwrap();
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(f)
            .build()
            .unwrap(),
    )
}

/// $10,000 of tax and nothing paid: $9,000 required, $2,250 a quarter.
fn base() -> Household {
    Household {
        tax: DOLLAR * 10_000,
        ..Household::default()
    }
}

/// The 2026 dates are the ones the SC1040ES and § 12-6-3910(A)(3)(a) give.
/// Each is a weekday and none is a South Carolina legal holiday, so none
/// moves.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let cal = estimated_calendar(2026).unwrap();
    let res = rules().schedule(&base().facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        assert!(cal.is_business_day(inst.due()));
        assert_eq!(inst.needed_by(), DOLLAR * 2_250 * (i as i64 + 1));
        assert_eq!(inst.status(), Status::Short);
        assert_eq!(inst.shortfall(), DOLLAR * 2_250);
    }
    // Last year's overpayment applied forward counts on April 15.
    let h = Household {
        payments: vec![
            estimated::new_payment()
                .amount(DOLLAR * 2_250)
                .from_prior_year()
                .build()
                .unwrap(),
        ],
        ..base()
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].status(), Status::Met);
    assert_eq!(s[1].status(), Status::Short);
}

/// The calendar moves a due date that falls on a holiday or a weekend.
/// Confederate Memorial Day, May 10, 2026, is a Sunday and is observed on
/// Monday the 11th.
#[test]
fn estimated_calendar_is_revenue_procedure_twenty_three_one() {
    let cal = estimated_calendar(2026).unwrap();
    assert_eq!(cal.holidays().len(), 17);
    assert!(cal.source().contains("Revenue Procedure #23-1"));
    for (from, to) in [
        (day(2026, 5, 10), day(2026, 5, 12)), // Sunday, then Confederate Memorial Day observed
        (day(2026, 7, 3), day(2026, 7, 6)),   // Independence Day observed, then the weekend
        (day(2026, 12, 24), day(2026, 12, 28)), // Christmas Eve, Christmas Day, the weekend
        (day(2027, 1, 31), day(2027, 2, 1)),  // a Sunday
        (day(2026, 6, 15), day(2026, 6, 15)), // a due date that is a business day
    ] {
        assert_eq!(cal.next_business_day(from), to, "a date due {from}");
    }
    let err = estimated_calendar(2025).err().unwrap();
    assert!(err.is(&estimated::Error::NoRules));
}

/// The $100 threshold, one cent either side.
#[test]
fn estimated_threshold_boundaries() {
    for (tax, withheld, exempt) in [
        (9_999, 0, true),
        (10_000, 0, false),
        (50_000, 40_001, true),
        (50_000, 40_000, false),
    ] {
        let h = Household {
            tax: USCents(tax),
            withheld: USCents(withheld),
            ..Household::default()
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "tax {tax}, withheld {withheld}"
        );
        if exempt {
            let c = res.value().chosen().unwrap();
            assert_eq!(c.basis(), Basis::Exception);
            assert_eq!(c.amount(), USCents(0));
            assert_eq!(c.label(), "Tax less withholding under $100");
        }
        let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        assert_eq!(
            p.value().total().0 == 0,
            exempt,
            "tax {tax}, withheld {withheld}"
        );
    }
}

/// The 110% step, one cent either side: "more than $150,000", or more than
/// $75,000 on a married separate return.
#[test]
fn estimated_prior_year_step_boundaries() {
    use filing::Status::*;
    for (status, agi, want) in [
        (Single, 15_000_000, percent(100)),
        (Single, 15_000_001, percent(110)),
        (MarriedFilingJointly, 15_000_001, percent(110)),
        (MarriedFilingSeparately, 7_500_000, percent(100)),
        (MarriedFilingSeparately, 7_500_001, percent(110)),
    ] {
        let h = Household {
            status: Some(status),
            tax: DOLLAR * 20_000,
            prior: last(DOLLAR * 10_000, USCents(agi)),
            ..Household::default()
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let o = res.value().options()[1].clone();
        assert_eq!(o.percent(), want, "{status} with AGI {agi}");
        assert_eq!(o.amount(), want.of(DOLLAR * 10_000));
        assert_eq!(res.value().chosen().unwrap().basis(), Basis::PriorYearTax);
        assert_eq!(res.value().target(), o.amount());
        assert!(!has(&res.required(), PositionKind::AdjustedGrossIncome));
    }
}

/// The target is the smaller of 90% of this year's tax and 100% of last
/// year's, and last year's needs a filed twelve-month SC1040.
#[test]
fn estimated_target_options() {
    let res = rules().requirement(&base().facts()).unwrap();
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::ThisYearTax);
    assert_eq!(c.percent(), percent(90));
    assert_eq!(res.value().target(), DOLLAR * 9_000);
    assert!(res.value().required());
    assert!(has(&res.required(), PositionKind::PriorYearReturn));
    let mut h = base();
    h.prior = last(DOLLAR * 8_000, DOLLAR * 90_000);
    let res = rules().requirement(&h.facts()).unwrap();
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::PriorYearTax);
    assert_eq!(c.percent(), percent(100));
    assert_eq!(res.value().target(), DOLLAR * 8_000);
    h.prior = last(DOLLAR * 9_000, DOLLAR * 90_000);
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().chosen().unwrap().basis(), Basis::ThisYearTax);

    let not_filed = estimated::new_prior_return().build().unwrap();
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .months(11)
        .build()
        .unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(estimated::new_figures().tax(DOLLAR * 500).build().unwrap())
        .build()
        .unwrap();
    for (name, p) in [
        ("not filed", not_filed),
        ("not required for a short year", not_required),
        ("a short year", short),
    ] {
        let h = Household {
            prior: Some(p),
            ..base()
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let o = res.value().options()[1].clone();
        assert!(!o.available() && !o.why_not().is_empty(), "{name}");
        assert!(!res.value().exempt(), "{name}");
        assert_eq!(res.value().target(), DOLLAR * 9_000, "{name}");
    }

    // Without last year's AGI, 110% is assumed and the question asked.
    let no_agi = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 8_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    h.prior = Some(no_agi);
    let res = rules().requirement(&h.facts()).unwrap();
    let o = res.value().options()[1].clone();
    assert_eq!(o.percent(), percent(110));
    assert_eq!(o.amount(), DOLLAR * 8_800);
    assert!(has(&res.required(), PositionKind::AdjustedGrossIncome));

    // South Carolina AGI under the step and federal AGI over it: the
    // SC1040ES figure is used, and the other reading is reported.
    let split = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 8_000)
                .state_agi(DOLLAR * 140_000)
                .federal_agi(DOLLAR * 160_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    h.prior = Some(split);
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().options()[1].percent(), percent(100));
    assert!(asks(
        &res.required(),
        PositionKind::AdjustedGrossIncome,
        "would make the test 110%"
    ));
}

/// Without this year's tax the target is last year's figure alone, and the
/// penalty against it is an upper bound.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: last(DOLLAR * 8_000, DOLLAR * 90_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 8_000 && r.target_known());
    let o = r.options()[0].clone();
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    assert_eq!(sched.value()[0].needed_by(), DOLLAR * 2_000);
    let p = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
    assert!(p.value().upper_bound() && p.value().total().0 != 0);

    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let r = rules().requirement(&none.facts()).unwrap().value();
    assert!(!r.target_known() && !r.required());
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
    let err = rules()
        .penalty(&none.facts(), day(2027, 4, 15))
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");

    let zero = Household {
        no_tax: true,
        prior: last(USCents(0), DOLLAR * 20_000),
        ..Household::default()
    };
    let p = rules().penalty(&zero.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));
    assert!(!p.value().upper_bound());
}

/// A shortfall that spans the July 1 rate change is charged 6% for the days
/// to June 30 and 7% after.
#[test]
fn estimated_penalty_across_the_july_rate_change() {
    let h = Household {
        payments: vec![pay(day(2026, 8, 14), DOLLAR * 2_250)],
        ..base()
    };
    let p = rules().penalty(&h.facts(), day(2026, 8, 14)).unwrap();
    let lines = p.value().lines();
    assert!(!lines.is_empty() && lines[0].installment() == 1);
    let parts = lines[0].parts();
    assert_eq!(parts.len(), 2);
    for (i, (to, days, rate, charge)) in [
        (day(2026, 6, 30), 76, percent(6), 2_811),
        (day(2026, 8, 14), 45, percent(7), 1_942),
    ]
    .into_iter()
    .enumerate()
    {
        assert_eq!(parts[i].to(), to, "part {}", i + 1);
        assert_eq!(parts[i].days(), days, "part {}", i + 1);
        assert_eq!(parts[i].rate(), rate, "part {}", i + 1);
        assert_eq!(parts[i].charge(), USCents(charge), "part {}", i + 1);
        assert!(parts[i].source().contains("#26-19"), "part {}", i + 1);
    }
    assert_eq!(lines[0].total(), USCents(4_753));
}

/// Nothing paid, figured to December 31: $238.63 in all. The 2027 days have
/// no published rate, so figuring to April 15, 2027 gives the same total and
/// reports the gap; a later date stops at April 15.
#[test]
fn estimated_penalty_to_the_year_end_and_the_unpublished_rate() {
    let f = base().facts();
    let p = rules().penalty(&f, day(2026, 12, 31)).unwrap();
    let lines = p.value().lines();
    assert_eq!(lines.len(), 4);
    for (i, w) in [10_751, 8_495, 4_617, 0].into_iter().enumerate() {
        assert_eq!(lines[i].total(), USCents(w), "line {}", i + 1);
    }
    assert_eq!(p.value().total(), USCents(23_863));
    assert!(!has(&p.required(), PositionKind::UnpublishedRate));
    assert!(has(&p.required(), PositionKind::PenaltyWaiver));

    let p = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(23_863));
    assert!(asks(
        &p.required(),
        PositionKind::UnpublishedRate,
        "Dec 31, 2026"
    ));
    let p = rules().penalty(&f, day(2027, 6, 30)).unwrap();
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
}

/// A return filed and paid by February 1, 2027, which is January 31 moved
/// past a Sunday, excuses the January installment, and the SC2210 ends the
/// earlier installments' periods on January 15.
#[test]
fn estimated_return_filed_by_february_first() {
    for (filed, excused, as_of, days) in [
        (day(2027, 1, 25), true, day(2027, 1, 15), 275),
        (day(2027, 2, 1), true, day(2027, 1, 15), 275),
        (day(2027, 2, 2), false, day(2027, 2, 2), 293),
        (day(2027, 1, 10), true, day(2027, 1, 10), 270),
    ] {
        let h = Household {
            return_filed: filed,
            ..base()
        };
        let s = rules().schedule(&h.facts()).unwrap().value();
        assert_eq!(s[3].status() == Status::Excused, excused, "filed {filed}");
        if excused {
            assert!(
                applied(
                    &rules().requirement(&h.facts()).unwrap().applied(),
                    "by February 1, 2027 (January 31, 2027, moved to the next business day)"
                ),
                "filed {filed}"
            );
        }
        let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        assert_eq!(p.value().as_of(), as_of, "filed {filed}");
        assert_eq!(p.value().lines()[0].days(), days, "filed {filed}");
        if excused {
            assert_eq!(p.value().lines().len(), 3, "filed {filed}");
        }
    }
}

/// No penalty after a $0 year of twelve months, on a filed return or with
/// none required. An 11-month year is not.
#[test]
fn estimated_no_liability_last_year() {
    let zero = || estimated::new_figures().tax(USCents(0)).build().unwrap();
    let zero_nonresident = estimated::new_prior_return()
        .filed(true)
        .figures(zero())
        .full_year_resident(false)
        .build()
        .unwrap();
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let short_zero = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(zero())
        .build()
        .unwrap();
    for (name, p, exempt) in [
        (
            "$0 on a filed return",
            last(USCents(0), DOLLAR * 30_000),
            true,
        ),
        ("$0, not resident all year", Some(zero_nonresident), true),
        ("no return required", Some(not_required), true),
        ("$0 for 11 months", Some(short_zero), false),
    ] {
        let h = Household { prior: p, ..base() };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "{name}: {}",
            res.value().why()
        );
        let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        assert_eq!(p.value().total().0 == 0, exempt, "{name}");
    }
}

/// An underpayment from personal service income earned in another state,
/// which withheld its own tax, bears no penalty. Not answered, it is asked
/// when there is a penalty, and taken as not.
#[test]
fn estimated_income_earned_in_another_state() {
    let yes = new_estimated_facts()
        .prior_year_farmer_or_fisher(false)
        .underpayment_from_other_state_income(true)
        .build()
        .unwrap();
    let mut h = Household {
        extras: Some(yes),
        ..base()
    };
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));
    assert!(p.value().lines().is_empty());
    assert_eq!(
        p.value().reason(),
        "The due dates that fall short are excused."
    );
    for (i, inst) in rules()
        .schedule(&h.facts())
        .unwrap()
        .value()
        .iter()
        .enumerate()
    {
        assert_eq!(inst.status(), Status::Excused, "installment {}", i + 1);
        assert!(inst.shortfall().0 != 0, "installment {}", i + 1);
    }
    h.extras = Some(
        new_estimated_facts()
            .prior_year_farmer_or_fisher(false)
            .build()
            .unwrap(),
    );
    let p = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
    assert_eq!(p.value().total(), USCents(23_863));
    assert!(asks(
        &p.required(),
        PositionKind::JurisdictionFact,
        "another state"
    ));
}

/// The casualty and retirement waivers are the department's to grant: the
/// penalty is reported before them, with the waiver asked for named.
#[test]
fn estimated_waiver_is_reported_not_applied() {
    for k in [WaiverKind::Casualty, WaiverKind::RetiredOrDisabled] {
        let h = Household {
            waiver: Some(k),
            ..base()
        };
        let p = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
        assert_eq!(p.value().total(), USCents(23_863), "{k}");
        assert_eq!(p.value().waiver(), k);
        assert!(!has(&p.required(), PositionKind::PenaltyWaiver), "{k}");
    }
}

/// The SC2210's first two waivers are the federal pair, at age 62, in 2025
/// or 2026: the penalty stays in full, with what the department can waive.
#[test]
fn estimated_waiver_outcome() {
    let pos = |c: WaiverCause, on: Date, born: Option<Date>| {
        let mut b = estimated::new_positions();
        b.waiver_for(c, on);
        if let Some(born) = born {
            b.born(born);
        }
        b.build().unwrap()
    };
    for (name, p, eligible, reason) in [
        (
            "a disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
            true,
            "a disaster",
        ),
        (
            "retired at 64",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1962, 3, 1)),
            ),
            true,
            "after reaching 62",
        ),
        (
            "disabled in 2025",
            pos(WaiverCause::Disability, day(2025, 6, 1), None),
            true,
            "reasonable cause",
        ),
        (
            "retired in 2020",
            pos(
                WaiverCause::Retirement,
                day(2020, 6, 1),
                Some(day(1955, 3, 1)),
            ),
            false,
            "2025 or 2026",
        ),
        (
            "retired at 60",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1966, 3, 1)),
            ),
            false,
            "reached 62",
        ),
    ] {
        let h = Household {
            positions: Some(p),
            ..base()
        };
        let p = rules()
            .penalty(&h.facts(), day(2026, 12, 31))
            .unwrap()
            .value();
        let o = p.waiver_outcome().unwrap();
        assert_eq!(p.total(), USCents(23_863), "{name}");
        assert_eq!(o.eligible(), eligible, "{name}");
        assert_eq!(o.refused(), !eligible, "{name}");
        assert!(o.reason().contains(reason), "{name}: {:?}", o.reason());
        let want = if eligible { USCents(0) } else { p.total() };
        assert_eq!(p.if_waived(), want, "{name}");
    }
}

/// An exception sets the target, so last year's return is not asked for.
#[test]
fn estimated_exempt_does_not_ask_for_last_year() {
    let h = Household {
        tax: DOLLAR * 50,
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(res.value().exempt());
    assert!(!has(&res.required(), PositionKind::PriorYearReturn));
}

/// A nonresident contractor with 2% withheld need not file a declaration,
/// but the SC2210 has no matching exception, so the penalty is still figured
/// and the question is left open.
#[test]
fn estimated_nonresident_contractor() {
    let yes = new_estimated_facts()
        .prior_year_farmer_or_fisher(false)
        .underpayment_from_other_state_income(false)
        .nonresident_contractor(true)
        .build()
        .unwrap();
    let mut h = Household {
        extras: Some(yes),
        ..base()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().required() && !res.value().exempt());
    assert_eq!(res.value().target(), DOLLAR * 9_000);
    assert!(asks(
        &res.required(),
        PositionKind::JurisdictionFact,
        "excuses the penalty"
    ));
    let p = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
    assert_eq!(p.value().total(), USCents(23_863));

    // Not answered: asked, and taken as not.
    h.extras = Some(
        new_estimated_facts()
            .prior_year_farmer_or_fisher(false)
            .build()
            .unwrap(),
    );
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(res.value().required());
    assert!(asks(
        &res.required(),
        PositionKind::JurisdictionFact,
        "nonresident contractor"
    ));
}

/// Withholding counts in four equal parts unless the filer elects the dates
/// it was withheld.
#[test]
fn estimated_withholding_credited_as_withheld() {
    let mut h = Household {
        withheld: DOLLAR * 9_000,
        ..base()
    };
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));
    h.as_withheld = true;
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [Status::Short, Status::Short, Status::Short, Status::Met]
        .into_iter()
        .enumerate()
    {
        assert_eq!(s[i].status(), want, "installment {}", i + 1);
    }
    // The December 15 withholding pays April, June and September that day.
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let lines = p.value().lines();
    assert_eq!(lines.len(), 3);
    assert_eq!(lines[0].days(), 244);
}

/// The annualised method takes 22.5%, 45%, 67.5% and 90% of each period's
/// annualised tax, less earlier installments, capped by the regular amount
/// plus what it carried.
#[test]
fn estimated_annualised_method() {
    let h = Household {
        annualised: vec![
            DOLLAR * 2_000,
            DOLLAR * 4_000,
            DOLLAR * 8_000,
            DOLLAR * 10_000,
        ],
        ..base()
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [450, 1_800, 5_400, 9_000].into_iter().enumerate() {
        assert_eq!(s[i].needed_by(), DOLLAR * want, "installment {}", i + 1);
    }
    let period = |m, d| {
        estimated::new_period()
            .end(day(2026, m, d))
            .annualised_tax(USCents(0))
            .build()
            .unwrap()
    };
    let wrong = estimated::new_facts(SC, 2026)
        .filing_status(filing::Status::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .elections(estimated::new_elections().annualised().build().unwrap())
        .state(answered())
        .periods(&[period(3, 31), period(5, 31), period(8, 31), period(11, 30)])
        .build()
        .unwrap();
    assert!(rules().schedule(&wrong).is_err());
}

/// The SC1040ES would put a late starter's first payment on a later date;
/// the SC2210 still charges 25% by every date.
#[test]
fn estimated_late_start_is_noted_not_applied() {
    for (met, note) in [
        (day(2026, 4, 1), ""),
        (day(2026, 4, 2), "1/2 of the estimate on June 15, 2026"),
        (day(2026, 6, 2), "3/4 of the estimate on September 15, 2026"),
        (day(2026, 9, 2), "all of the estimate on January 15, 2027"),
    ] {
        let h = Household {
            first_met: met,
            ..base()
        };
        let res = rules().schedule(&h.facts()).unwrap();
        assert_eq!(
            applied(&res.applied(), "the requirement first arose"),
            !note.is_empty(),
            "first met {met}"
        );
        if !note.is_empty() {
            assert!(applied(&res.applied(), note), "first met {met}");
        }
        assert_eq!(res.value()[0].needed_by(), DOLLAR * 2_250);
        assert_eq!(res.value()[0].status(), Status::Short);
    }
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&base().facts()).unwrap();
    let want: [(&str, [&str; 4]); 9] = [
        (
            "The 2026 worksheet predates Act 110",
            [
                "0%, 3% and 6%",
                "Rev. 10/13/25",
                "1.99%",
                "Act No. 110 of 2026",
            ],
        ),
        (
            "The interest rate",
            [
                "7% (.07) in all four rate periods",
                "#26-19",
                "at the same time as the federal underpayment rate",
                "§§ 12-54-25(D)",
            ],
        ),
        (
            "The 110% test at exactly $150,000",
            [
                "\"$150,000 or more\"",
                "line 33",
                "exceeds $150,000",
                "§ 6654(d)(1)(C)(i)",
            ],
        ),
        (
            "The married filing separately step",
            ["$75,000", "Part I line 7", "$75,000", "§ 6654(d)(1)(C)(ii)"],
        ),
        (
            "No tax liability last year",
            [
                "no condition of U.S. citizenship or residence",
                "Exceptions to the penalty",
                "citizen or resident",
                "§ 6654(e)(2)(C)",
            ],
        ),
        (
            "The January 31 date",
            [
                "February 1",
                "SC1040ES (2026)",
                "next business day",
                "§ 12-60-50(A)",
            ],
        ),
        (
            "Earlier installments after a return filed by January 31",
            [
                "end on January 15",
                "Period of underpayment",
                "until the underpayment is paid",
                "§ 6654(b)(2)",
            ],
        ),
        (
            "Income earned in another state",
            [
                "request a waiver",
                "Waiver of penalty item 3",
                "no interest or penalty is due",
                "§ 12-54-55(1)",
            ],
        ),
        (
            "A requirement first met after April 1",
            [
                "25% of the required annual payment by every date",
                "worksheet line 11",
                "no rule for a requirement that arises late",
                "§ 6654(c), (d)(1)(A)",
            ],
        ),
    ];
    let ds = res.divergences();
    assert_eq!(ds.len(), want.len());
    for d in &ds {
        let Some((_, w)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(d.form().contains(w[0]), "{:?}", d.rule());
        assert!(d.form_source().contains(w[1]), "{:?}", d.rule());
        assert!(d.statute().contains(w[2]), "{:?}", d.rule());
        assert!(d.statute_source().contains(w[3]), "{:?}", d.rule());
        // The tax and the rate follow the statute; the rest follow the
        // forms, and only those two affect these facts.
        let statute =
            d.rule() == "The 2026 worksheet predates Act 110" || d.rule() == "The interest rate";
        let uses = if statute {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert_eq!(d.uses(), uses, "{:?}", d.rule());
        assert_eq!(d.affects_household(), statute, "{:?}", d.rule());
    }
    assert!(applied(&res.applied(), "taken as figured under Act 110"));
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for y in [2025, 2027] {
        let err = estimated_tax(y).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{y}: {err}");
    }
    let this = estimated::new_figures()
        .tax(DOLLAR * 10_000)
        .build()
        .unwrap();
    let facts = |j: estimated::Jurisdiction, year: i64| {
        let mut b = estimated::new_facts(j, year);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    let farmer_last = new_estimated_facts()
        .prior_year_farmer_or_fisher(true)
        .build()
        .unwrap();
    for (name, f) in [
        (
            "a farmer at 66.67%",
            facts(SC, 2026)
                .farm_share(Rate(6_667))
                .state(answered())
                .build()
                .unwrap(),
        ),
        (
            "farming and fishing together",
            facts(SC, 2026)
                .farm_share(Rate(3_334))
                .fish_share(Rate(3_333))
                .state(answered())
                .build()
                .unwrap(),
        ),
        (
            "a farmer last year",
            facts(SC, 2026).state(farmer_last).build().unwrap(),
        ),
    ] {
        let err = rules().requirement(&f).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    rules()
        .requirement(
            &facts(SC, 2026)
                .farm_share(Rate(6_666))
                .state(answered())
                .build()
                .unwrap(),
        )
        .unwrap();
    for (name, f) in [
        (
            "Pennsylvania's facts",
            facts(estimated::PENNSYLVANIA, 2026).build().unwrap(),
        ),
        ("2027", facts(SC, 2027).build().unwrap()),
        (
            "other extras",
            facts(SC, 2026)
                .state("not South Carolina's")
                .build()
                .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(rules().penalty(&base().facts(), Date::default()).is_err());
    // Last year's farming share not given: asked, and taken as under
    // two-thirds.
    let res = rules()
        .requirement(&facts(SC, 2026).build().unwrap())
        .unwrap();
    assert!(asks(
        &res.required(),
        PositionKind::JurisdictionFact,
        "farmer rules"
    ));
}

#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    let text = format!(
        "{} {} {}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$100",
        "another state",
        "January 15",
        "Confederate Memorial Day",
        "110%",
        "$75,000",
        "6% from April to June",
        "February 1, 2027",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.versus_federal().is_empty() && !e.sources().is_empty());
}

/// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
/// never as a negative amount.
#[test]
fn estimated_withholding_over_the_tax_reads_as_a_refund() {
    let figures = |tax: USCents| {
        estimated::new_figures()
            .tax(tax)
            .federal_agi(DOLLAR * 60_000)
            .state_agi(DOLLAR * 60_000)
            .build()
            .unwrap()
    };
    let last = estimated::new_prior_return()
        .filed(true)
        .months(12)
        .full_year_resident(true)
        .figures(figures(DOLLAR * 2_800))
        .build()
        .unwrap();
    let withheld: Vec<_> = (1..=12)
        .map(|m| {
            estimated::new_withheld()
                .on(day(2026, m, 28))
                .amount(DOLLAR * 300)
                .build()
                .unwrap()
        })
        .collect();
    let f = estimated::new_facts(SC, 2026)
        .filing_status(filing::Status::Single)
        .this_year(figures(DOLLAR * 3_000))
        .prior_year(last)
        .withheld(&withheld)
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    let r = res.value();
    assert!(r.why().contains("a $600 refund"), "why = {:?}", r.why());
    let mut texts = vec![r.why()];
    for o in r.options() {
        texts.push(o.label());
        texts.push(o.why_not());
    }
    for q in res.required() {
        texts.push(q.because());
        texts.push(q.question());
        texts.push(q.effect());
    }
    for d in res.divergences() {
        texts.push(d.rule());
        texts.push(d.form());
        texts.push(d.statute());
    }
    for s in texts {
        assert!(
            !s.contains("−$") && !s.contains("-$"),
            "a negative amount where a refund belongs: {s:?}"
        );
    }
}
