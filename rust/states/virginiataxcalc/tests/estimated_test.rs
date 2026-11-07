//! Virginia's estimated tax rules for 2026, ported from the Go package's
//! estimated_test.go. The worked examples from 23VAC10-112-42 and -43 and the
//! Form 760C instructions are reproduced; Go also compares their text, and
//! every figure, against the extracts in testdata/, which stays in Go.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status, WaiverCause,
    WaiverKind, Weekday,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_virginiataxcalc::{
    EstimatedFacts, EstimatedFactsBuilder, RatePeriod, calendar, estimated_tax,
    new_estimated_facts, rates,
};
use working::{PositionKind, Reading, Required, Rule};

const VA: estimated::Jurisdiction = estimated::VIRGINIA;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

fn because(rs: &[Required], k: PositionKind) -> String {
    rs.iter()
        .filter(|r| r.position() == k)
        .map(|r| {
            format!("{} {}", r.question(), r.effect())
                .trim()
                .to_string()
        })
        .collect::<Vec<_>>()
        .join(" | ")
}

fn applied(rs: &[Rule]) -> String {
    rs.iter()
        .map(|r| r.applied())
        .collect::<Vec<_>>()
        .join(" | ")
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

fn dues() -> [Date; 4] {
    [
        day(2026, 5, 1),
        day(2026, 6, 15),
        day(2026, 9, 15),
        day(2027, 1, 15),
    ]
}

/// One payment of each amount on each due date.
fn quarterly(a: [USCents; 4]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .filter(|(_, x)| x.0 > 0)
        .map(|(i, x)| pay(dues()[i], *x))
        .collect()
}

fn q(a: i64, b: i64, c: i64, d: i64) -> Vec<Payment> {
    quarterly([USCents(a), USCents(b), USCents(c), USCents(d)])
}

#[derive(Default, Clone)]
struct Household {
    status: Option<filing::Status>,
    /// After the Spouse Tax Adjustment and credits, Form 760C line 1.
    tax: USCents,
    no_tax: bool,
    agi: Option<USCents>,
    withheld: USCents,
    withheld_on: Date,
    as_withheld: bool,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    /// Form 760C line 18f for each period.
    annualised: Vec<USCents>,
    waiver: Option<WaiverKind>,
    /// Replaces `waiver` when set.
    positions: Option<Positions>,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

fn ten() -> Household {
    tax(DOLLAR * 10_000)
}

impl Household {
    fn facts(&self) -> Facts {
        let status = self.status.unwrap_or(filing::Status::Single);
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        if let Some(agi) = self.agi {
            fb.state_agi(agi);
        }
        let mut b = estimated::new_facts(VA, 2026);
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
        match &self.extras {
            Some(e) => b.state(e.clone()),
            None => b.state(
                new_estimated_facts()
                    .merchant_seaman_share(Rate(0))
                    .build()
                    .unwrap(),
            ),
        };
        if !self.annualised.is_empty() {
            let ends = [
                day(2026, 4, 30),
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
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed twelve-month return showing tax.
fn prior(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .build()
            .unwrap(),
    )
}

fn extras(b: &mut EstimatedFactsBuilder) -> Option<EstimatedFacts> {
    Some(b.merchant_seaman_share(Rate(0)).build().unwrap())
}

/// The rates are the bulletins' quarters: 8% from April 1 to June 30, 2026
/// and 9% from July 1 to December 31, 2026, with 9% at the daily .00025 Form
/// 760C prints and 8% at no printed factor.
#[test]
fn estimated_rates_are_the_bulletins() {
    let want = vec![
        RatePeriod {
            from: day(2026, 4, 1),
            to: day(2026, 6, 30),
            rate: percent(8),
            factor: String::new(),
        },
        RatePeriod {
            from: day(2026, 7, 1),
            to: day(2026, 12, 31),
            rate: percent(9),
            factor: "0.00025".to_string(),
        },
    ];
    assert_eq!(rates(2026), want);
}

/// The 2026 dates are the ones Form 760ES prints, and none moves.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let cal = calendar(2026);
    let res = rules().schedule(&ten().facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        assert!(cal.is_business_day(inst.due()));
    }
}

/// The nth `wd` of a month, or the last for n = -1.
fn nth_weekday(year: i64, m: i64, wd: Weekday, n: i64) -> Date {
    if n < 0 {
        let mut d = day(year, m + 1, 1).add_days(-1);
        while d.weekday() != wd {
            d = d.add_days(-1);
        }
        return d;
    }
    let mut d = day(year, m, 1);
    while d.weekday() != wd {
        d = d.add_days(1);
    }
    d.add_days(7 * (n - 1))
}

/// Moves a fixed holiday on a Saturday to the Friday and on a Sunday to the
/// Monday, as § 2.2-3300 does.
fn observed(d: Date) -> Date {
    match d.weekday() {
        Weekday::SATURDAY => d.add_days(-1),
        Weekday::SUNDAY => d.add_days(1),
        _ => d,
    }
}

/// The calendar is § 2.2-3300's holidays from January 2026 to February 2027,
/// worked out from the statute's rules, and it moves a date that falls on
/// one or on a weekend to the next business day.
#[test]
fn estimated_calendar_moves_weekends_and_holidays() {
    let want = [
        observed(day(2026, 1, 1)),
        nth_weekday(2026, 1, Weekday::MONDAY, 3),
        nth_weekday(2026, 2, Weekday::MONDAY, 3),
        nth_weekday(2026, 5, Weekday::MONDAY, -1),
        observed(day(2026, 6, 19)),
        observed(day(2026, 7, 4)),
        nth_weekday(2026, 9, Weekday::MONDAY, 1),
        nth_weekday(2026, 10, Weekday::MONDAY, 2),
        nth_weekday(2026, 11, Weekday::MONDAY, 1).add_days(1),
        observed(day(2026, 11, 11)),
        nth_weekday(2026, 11, Weekday::THURSDAY, 4),
        nth_weekday(2026, 11, Weekday::THURSDAY, 4).add_days(1),
        observed(day(2026, 12, 25)),
        observed(day(2027, 1, 1)),
        nth_weekday(2027, 1, Weekday::MONDAY, 3),
        nth_weekday(2027, 2, Weekday::MONDAY, 3),
    ];
    let cal = calendar(2026);
    let hs = cal.holidays();
    assert_eq!(hs.len(), want.len());
    for (i, h) in hs.iter().enumerate() {
        assert_eq!(h.on(), want[i], "holiday {} ({})", i + 1, h.name());
    }
    for (from, to) in [
        (day(2026, 7, 3), day(2026, 7, 6)),
        (day(2026, 9, 7), day(2026, 9, 8)),
        (day(2026, 11, 26), day(2026, 11, 30)),
        (day(2027, 5, 1), day(2027, 5, 3)),
        (day(2027, 3, 1), day(2027, 3, 1)),
    ] {
        assert_eq!(cal.next_business_day(from), to, "{from}");
    }
}

/// Form 760ES Section I: estimated payments are required when tax after
/// withholding is more than $1,000.
#[test]
fn estimated_filing_threshold_boundary() {
    for (withheld, required) in [(50_000, false), (49_999, true)] {
        let h = Household {
            withheld: USCents(withheld),
            ..tax(DOLLAR * 1_500)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().required(), required, "{}", res.value().why());
    }
}

/// Form 760ES Section I and § 58.1-321: below $11,950 of VAGI single or
/// married filing separately, or $23,900 jointly, no return and no payments
/// are required. No floor is given for other statuses.
#[test]
fn estimated_income_floor_boundaries() {
    use filing::Status::*;
    for (status, agi, exempt) in [
        (Single, 1_194_999, true),
        (Single, 1_195_000, false),
        (MarriedFilingJointly, 2_389_999, true),
        (MarriedFilingJointly, 2_390_000, false),
        (MarriedFilingSeparately, 1_194_999, true),
        (MarriedFilingSeparately, 1_195_000, false),
        (HeadOfHousehold, 500_000, false),
    ] {
        let h = Household {
            status: Some(status),
            agi: Some(USCents(agi)),
            ..ten()
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "{status} with VAGI {agi}");
        if exempt {
            let c = res.value().chosen().unwrap();
            assert_eq!(c.basis(), Basis::Exception);
            assert_eq!(c.amount(), USCents(0));
            assert!(
                c.label()
                    .starts_with("Virginia adjusted gross income under $")
            );
            let p = rules().penalty(&h.facts(), day(2027, 5, 1)).unwrap();
            assert_eq!(p.value().total(), USCents(0));
        }
    }
}

/// § 58.1-492(D)(3): no addition when the year's underpayments total $1,000
/// or less.
#[test]
fn estimated_small_underpayment_boundary() {
    for (t, excuse) in [(111_111, true), (111_112, false)] {
        let f = tax(USCents(t)).facts();
        let sched = rules().schedule(&f).unwrap().value();
        for inst in &sched {
            assert_eq!(
                inst.status() == Status::Excused,
                excuse,
                "tax {t}: installment {}",
                inst.number()
            );
        }
        let p = rules().penalty(&f, day(2026, 12, 31)).unwrap().value();
        assert_eq!(p.total().0 == 0, excuse, "tax {t}");
    }
}

/// The target is the least of 90% of this year's tax, 100% of last year's
/// and the tax on last year's income at this year's rates.
#[test]
fn estimated_target_options() {
    for (p, basis, target) in [
        (899_999, Basis::PriorYearTax, 899_999),
        (900_000, Basis::ThisYearTax, 900_000),
        (900_001, Basis::ThisYearTax, 900_000),
    ] {
        let h = Household {
            prior: prior(USCents(p)),
            ..ten()
        };
        let r = rules().requirement(&h.facts()).unwrap().value();
        assert_eq!(r.chosen().unwrap().basis(), basis, "last year {p}");
        assert_eq!(r.target(), USCents(target), "last year {p}");
    }

    let h = Household {
        prior: prior(DOLLAR * 8_000),
        extras: extras(new_estimated_facts().prior_year_income_tax(USCents(799_999))),
        ..ten()
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearIncome);
    assert_eq!(r.target(), USCents(799_999));

    // A return last year that showed no tax sets the target at zero.
    let f = Household {
        prior: prior(USCents(0)),
        ..ten()
    }
    .facts();
    let r = rules().requirement(&f).unwrap().value();
    assert_eq!(r.target(), USCents(0));
    assert!(!r.required());
    let p = rules().penalty(&f, day(2027, 5, 1)).unwrap().value();
    assert_eq!(p.total(), USCents(0));

    // Unavailable options say why, and the questions are asked.
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    for (name, p, ask) in [
        (
            "no return on record",
            None,
            "Did you file a 2025 Virginia return",
        ),
        (
            "no return filed",
            Some(not_filed),
            "Form 760C says whether last year then counts as zero",
        ),
    ] {
        let h = Household { prior: p, ..ten() };
        let res = rules().requirement(&h.facts()).unwrap();
        let o = res.value().options();
        assert!(!o[1].available() && !o[1].why_not().is_empty(), "{name}");
        assert!(
            because(&res.required(), PositionKind::PriorYearReturn).contains(ask),
            "{name}"
        );
    }
    let res = rules().requirement(&ten().facts()).unwrap();
    assert!(!res.value().options()[2].available());
    assert!(
        because(&res.required(), PositionKind::JurisdictionFact)
            .contains("cancel the charge under Exception 2")
    );
}

/// Without this year's tax the installments are measured against last
/// year's tax, which can only be too high, so the penalty is an upper bound.
#[test]
fn estimated_without_this_years_tax() {
    let mut h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required());
    assert_eq!(r.target(), DOLLAR * 8_000);
    let o = r.options()[0].clone();
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    for (i, inst) in rules()
        .schedule(&h.facts())
        .unwrap()
        .value()
        .iter()
        .enumerate()
    {
        assert_eq!(inst.needed_by(), DOLLAR * 2_000 * (i as i64 + 1));
    }
    let p = rules()
        .penalty(&h.facts(), day(2026, 12, 31))
        .unwrap()
        .value();
    assert!(p.total().0 != 0 && p.upper_bound());
    let k = 200_000;
    h.payments = q(k, k, k, k);
    let p = rules()
        .penalty(&h.facts(), day(2027, 5, 1))
        .unwrap()
        .value();
    assert_eq!(p.total(), USCents(0));
    assert!(!p.upper_bound());

    // With no return last year and only Exception 2, a missed installment is
    // measured against 90% of this year's tax, which is not known.
    let mut ex2 = Household {
        no_tax: true,
        extras: extras(new_estimated_facts().prior_year_income_tax(DOLLAR * 8_000)),
        ..Household::default()
    };
    let r = rules().requirement(&ex2.facts()).unwrap().value();
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearIncome);
    let err = rules()
        .penalty(&ex2.facts(), day(2027, 5, 1))
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
    ex2.payments = h.payments.clone();
    let p = rules()
        .penalty(&ex2.facts(), day(2027, 5, 1))
        .unwrap()
        .value();
    assert_eq!(p.total(), USCents(0));
    assert!(!p.upper_bound());

    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let r = rules().requirement(&none.facts()).unwrap().value();
    assert!(!r.target_known());
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
    let err = rules()
        .penalty(&none.facts(), day(2027, 5, 1))
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
}

/// 23VAC10-112-42, Example 2: $5,000 of tax, $1,125 a quarter, paid $200,
/// $800, $3,000 and $0. The September overpayment of $1,875 goes $925 to May
/// 1, $325 to June 15 and $625 to January 15.
#[test]
fn estimated_payment_order_matches_regulation_example() {
    let f = Household {
        payments: q(20_000, 80_000, 300_000, 0),
        ..tax(DOLLAR * 5_000)
    }
    .facts();
    let sched = rules().schedule(&f).unwrap().value();
    for (i, want) in [925, 325, 0, 500].into_iter().enumerate() {
        assert_eq!(sched[i].shortfall(), DOLLAR * want, "installment {}", i + 1);
    }
    let lines = rules()
        .penalty(&f, day(2027, 5, 1))
        .unwrap()
        .value()
        .lines();
    assert_eq!(lines.len(), 3);
    for (i, want) in [137, 92, 106].into_iter().enumerate() {
        assert_eq!(lines[i].days(), want, "line {}", lines[i].installment());
    }
}

/// 23VAC10-112-42, Example 1: $10,000 of tax and $1,500 paid on each date is
/// $750 short on each, for 365, 320, 228 and 106 days to May 1. May's $750
/// is 60 days at 8%, $9.86, and 184 days at .00025, $34.50, with the 2027
/// days unrated.
#[test]
fn estimated_underpayment_matches_regulation_example() {
    let k = 150_000;
    let f = Household {
        payments: q(k, k, k, k),
        ..ten()
    }
    .facts();
    let res = rules().penalty(&f, day(2027, 5, 1)).unwrap();
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    for (i, want) in [365, 320, 228, 106].into_iter().enumerate() {
        assert_eq!(lines[i].shortfall(), DOLLAR * 750, "line {}", i + 1);
        assert_eq!(lines[i].days(), want, "line {}", i + 1);
    }
    assert_eq!(lines[0].total(), USCents(4_436));
    assert!(
        because(&res.required(), PositionKind::UnpublishedRate)
            .contains("rate for the days after Dec 31, 2026 isn't published yet")
    );
}

/// The Form 760C instructions for line 13: a $1,000 payment on May 15
/// against a $500 underpayment pays $500 late in A and $500 on time in B.
#[test]
fn estimated_late_payment_matches_instructions_example() {
    let d = dues();
    let f = Household {
        payments: vec![
            pay(d[0], DOLLAR * 1_750),
            pay(day(2026, 5, 15), DOLLAR * 1_000),
            pay(d[1], DOLLAR * 1_750),
            pay(d[2], DOLLAR * 2_250),
        ],
        ..ten()
    }
    .facts();
    let sched = rules().schedule(&f).unwrap().value();
    assert_eq!(sched[0].shortfall(), DOLLAR * 500);
    assert_eq!(sched[1].shortfall(), USCents(0));
    let lines = rules()
        .penalty(&f, day(2026, 12, 31))
        .unwrap()
        .value()
        .lines();
    assert_eq!(lines.len(), 2);
    assert_eq!(lines[0].days(), 14);
    assert_eq!(lines[0].total(), USCents(153));
}

/// The Form 760C instructions for line 21: $1,000 short on May 1, paid $500
/// on May 15 and $500 on June 30, is two computations.
#[test]
fn estimated_two_payments_match_instructions_example() {
    let d = dues();
    let f = Household {
        payments: vec![
            pay(d[0], DOLLAR * 1_250),
            pay(day(2026, 5, 15), DOLLAR * 500),
            pay(d[1], DOLLAR * 2_250),
            pay(day(2026, 6, 30), DOLLAR * 500),
            pay(d[2], DOLLAR * 2_250),
        ],
        ..ten()
    }
    .facts();
    let line = rules()
        .penalty(&f, day(2026, 12, 31))
        .unwrap()
        .value()
        .lines()[0]
        .clone();
    let parts = line.parts();
    assert_eq!(parts.len(), 2);
    assert_eq!(parts[0].days(), 14);
    assert_eq!(parts[0].charge(), USCents(153));
    assert_eq!(parts[1].days(), 60);
    assert_eq!(parts[1].charge(), USCents(658));
    assert_eq!(line.days(), 60);
    assert_eq!(line.total(), USCents(811));
}

/// A shortfall from May 1 paid on September 15 spans the change from 8% to
/// 9% on July 1.
#[test]
fn estimated_penalty_spans_the_rate_change() {
    // 90% of $44,444.44 is $40,000, $10,000 a quarter.
    let f = Household {
        payments: q(0, 1_000_000, 2_000_000, 1_000_000),
        ..tax(USCents(4_444_444))
    }
    .facts();
    let res = rules().penalty(&f, day(2027, 5, 1)).unwrap();
    let lines = res.value().lines();
    assert_eq!(lines.len(), 1);
    let parts = lines[0].parts();
    assert_eq!(parts.len(), 2);
    for (i, (from, to, days, rate, charge)) in [
        (day(2026, 5, 1), day(2026, 6, 30), 60, percent(8), 13_151),
        (day(2026, 6, 30), day(2026, 9, 15), 77, percent(9), 19_250),
    ]
    .into_iter()
    .enumerate()
    {
        let p = &parts[i];
        assert_eq!(p.from(), from, "part {}", i + 1);
        assert_eq!(p.to(), to, "part {}", i + 1);
        assert_eq!(p.days(), days, "part {}", i + 1);
        assert_eq!(p.rate(), rate, "part {}", i + 1);
        assert_eq!(p.charge(), USCents(charge), "part {}", i + 1);
    }
    assert_eq!(res.value().total(), USCents(32_401));
    assert!(
        because(&res.required(), PositionKind::UnpublishedRate)
            .contains("8% times the days over 365")
    );
}

/// Days at 9% are charged at the .00025 Form 760C prints, not 9% over 365.
#[test]
fn estimated_interest_uses_the_printed_daily_rate() {
    // 90% of $444,444.44 is $400,000, $100,000 a quarter.
    let k = 10_000_000;
    let f = Household {
        payments: q(k, k, 0, k),
        ..tax(USCents(44_444_444))
    }
    .facts();
    let res = rules().penalty(&f, day(2026, 12, 31)).unwrap();
    let lines = res.value().lines();
    assert_eq!(lines.len(), 1);
    assert_eq!(lines[0].installment(), 3);
    assert_eq!(lines[0].total(), DOLLAR * 2_675);
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
}

/// The addition runs to May 1, 2027 at the latest, and the rate from
/// January 1, 2027 is not published, so those days are reported rather than
/// charged.
#[test]
fn estimated_penalty_stops_on_may_first() {
    let k = 10_000_000;
    let f = Household {
        payments: q(k, k, 0, k),
        ..tax(USCents(44_444_444))
    }
    .facts();
    let res = rules().penalty(&f, day(2027, 6, 30)).unwrap();
    let p = res.value();
    assert_eq!(p.as_of(), day(2027, 5, 1));
    let l = p.lines()[0].clone();
    assert_eq!(l.days(), 228);
    assert_eq!(l.total(), DOLLAR * 2_675);
    assert!(
        because(&res.required(), PositionKind::UnpublishedRate)
            .contains("rate for the days after Dec 31, 2026 isn't published yet")
    );
}

/// Exception 1 needs a twelve-month return last year that showed tax.
#[test]
fn estimated_exception_one_conditions() {
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 4_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    for (name, p, want) in [
        (
            "a full year with tax",
            prior(DOLLAR * 4_000),
            "Exception 1 is open",
        ),
        (
            "a full year, no tax",
            prior(USCents(0)),
            "Exception 1 is not open",
        ),
        ("six months", Some(short), "Exception 1 is not open"),
    ] {
        let h = Household { prior: p, ..ten() };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(applied(&res.applied()).contains(want), "{name}");
    }
}

/// Exception 2 cancels the addition for each installment whose payments by
/// its date reach 25%, 50%, 75% and 100% of the tax on last year's income.
#[test]
fn estimated_exception_two() {
    let mut h = Household {
        extras: extras(new_estimated_facts().prior_year_income_tax(DOLLAR * 8_000)),
        payments: q(199_999, 200_001, 200_000, 200_000),
        ..tax(DOLLAR * 20_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    let want = [
        Status::Short,
        Status::Excused,
        Status::Excused,
        Status::Excused,
    ];
    for (i, inst) in sched.iter().enumerate() {
        assert_eq!(inst.status(), want[i], "installment {}", i + 1);
        assert!(inst.shortfall().0 != 0, "installment {}", i + 1);
    }
    let lines = rules()
        .penalty(&h.facts(), day(2027, 5, 1))
        .unwrap()
        .value()
        .lines();
    assert!(lines.len() == 1 && lines[0].installment() == 1);
    let k = 200_000;
    h.payments = q(k, k, k, k);
    let p = rules()
        .penalty(&h.facts(), day(2027, 5, 1))
        .unwrap()
        .value();
    assert_eq!(p.total(), USCents(0));
}

/// 23VAC10-112-43, Example 4: Virginia tax of $2,048, $2,755 and $4,722 on
/// annualized income needs $460.80, $1,239.75 and $3,187.35, and $1,100 a
/// quarter meets it.
#[test]
fn estimated_exception_three_matches_regulation_example() {
    let k = 110_000;
    let h = Household {
        annualised: vec![
            DOLLAR * 2_048,
            DOLLAR * 2_755,
            DOLLAR * 4_722,
            DOLLAR * 5_000,
        ],
        payments: q(k, k, k, k),
        ..tax(DOLLAR * 5_000)
    };
    let res = rules().schedule(&h.facts()).unwrap();
    let sched = res.value();
    for (i, want) in [112_500, 225_000, 337_500, 450_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), USCents(want), "installment {}", i + 1);
        assert_eq!(sched[i].status(), Status::Excused, "installment {}", i + 1);
    }
    assert!(applied(&res.applied()).contains("$460.80, $1239.75 and $3187.35"));
    let p = rules()
        .penalty(&h.facts(), day(2027, 5, 1))
        .unwrap()
        .value();
    assert_eq!(p.total(), USCents(0));
}

/// Exception 3 at ten times the example, where the $1,000 exception does not
/// apply.
#[test]
fn estimated_exception_three() {
    for (may, may_case) in [(460_800, Status::Excused), (460_799, Status::Short)] {
        let h = Household {
            annualised: vec![
                DOLLAR * 20_480,
                DOLLAR * 27_550,
                DOLLAR * 47_220,
                DOLLAR * 50_000,
            ],
            payments: q(may, 1_000_000, 1_000_000, 1_000_000),
            ..tax(DOLLAR * 50_000)
        };
        let sched = rules().schedule(&h.facts()).unwrap().value();
        let want = [may_case, Status::Excused, Status::Short, Status::Short];
        for (i, inst) in sched.iter().enumerate() {
            assert_eq!(inst.status(), want[i], "May {may}: installment {}", i + 1);
        }
    }
}

/// 23VAC10-112-43, Example 5: tax of $505, $969 and $3,154 on the income
/// received to date needs 90% of it, and $975 a quarter meets it.
#[test]
fn estimated_exception_four_matches_regulation_example() {
    let k = 97_500;
    let h = Household {
        prior: prior(DOLLAR * 4_000),
        extras: extras(new_estimated_facts().income_to_date_tax(
            DOLLAR * 505,
            DOLLAR * 969,
            DOLLAR * 3_154,
        )),
        payments: q(k, k, k, k),
        ..tax(DOLLAR * 5_000)
    };
    let res = rules().schedule(&h.facts()).unwrap();
    let sched = res.value();
    assert!(applied(&res.applied()).contains("$454.50, $872.10 and $2838.60"));
    for (i, want) in [100_000, 200_000, 300_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), USCents(want), "installment {}", i + 1);
        assert_eq!(sched[i].status(), Status::Excused, "installment {}", i + 1);
    }
    let p = rules()
        .penalty(&h.facts(), day(2027, 5, 1))
        .unwrap()
        .value();
    assert_eq!(p.total(), USCents(0));
}

/// Exception 4 at ten times the example: $4,545 by May 1 meets it and a cent
/// less does not.
#[test]
fn estimated_exception_four() {
    let e = extras(new_estimated_facts().income_to_date_tax(
        DOLLAR * 5_050,
        DOLLAR * 9_690,
        DOLLAR * 31_540,
    ));
    for (may, lines) in [(454_500, 0), (454_499, 1)] {
        let h = Household {
            extras: e.clone(),
            payments: q(may, 500_000, 2_000_000, 2_000_000 - may),
            ..tax(DOLLAR * 50_000)
        };
        let sched = rules().schedule(&h.facts()).unwrap().value();
        assert_eq!(sched[1].status(), Status::Excused, "May {may}");
        let got = rules()
            .penalty(&h.facts(), day(2027, 5, 1))
            .unwrap()
            .value()
            .lines()
            .len();
        assert_eq!(got, lines, "May {may}");
    }
}

/// Form 760ES Section IV: filing and paying in full by March 1, 2027 means
/// the January payment is not required.
#[test]
fn estimated_return_filed_by_march_first() {
    let k = 225_000;
    for (filed, excused) in [(day(2027, 3, 1), true), (day(2027, 3, 2), false)] {
        let f = Household {
            payments: q(k, k, k, 0),
            return_filed: filed,
            ..ten()
        }
        .facts();
        let jan = rules().schedule(&f).unwrap().value()[3];
        assert_eq!(jan.status() == Status::Excused, excused, "filed {filed}");
        let p = rules().penalty(&f, day(2027, 5, 1)).unwrap().value();
        assert_eq!(p.as_of(), filed);
        assert_eq!(p.lines().is_empty(), excused, "filed {filed}");
    }
}

/// Form 760ES's payment schedule: a requirement first met after April 15 is
/// paid in thirds from June 15, after June 1 in halves from September 15,
/// and after September 1 in full on January 15.
#[test]
fn estimated_late_start() {
    for (met, need) in [
        (day(2026, 4, 15), [225_000, 450_000, 675_000, 900_000]),
        (day(2026, 4, 16), [0, 300_000, 600_000, 900_000]),
        (day(2026, 6, 1), [0, 300_000, 600_000, 900_000]),
        (day(2026, 6, 2), [0, 0, 450_000, 900_000]),
        (day(2026, 9, 1), [0, 0, 450_000, 900_000]),
        (day(2026, 9, 2), [0, 0, 0, 900_000]),
    ] {
        let h = Household {
            first_met: met,
            ..ten()
        };
        let sched = rules().schedule(&h.facts()).unwrap().value();
        for (i, inst) in sched.iter().enumerate() {
            assert_eq!(inst.needed_by(), USCents(need[i]), "first met {met}");
            if need[i] == 0 {
                assert_eq!(inst.status(), Status::NothingDue, "first met {met}");
            }
        }
    }
    let res = rules().penalty(&ten().facts(), day(2027, 5, 1)).unwrap();
    assert!(has(&res.required(), PositionKind::RequirementFirstMet));
    let h = Household {
        first_met: day(2026, 1, 1),
        ..ten()
    };
    let res = rules().penalty(&h.facts(), day(2027, 5, 1)).unwrap();
    assert!(!has(&res.required(), PositionKind::RequirementFirstMet));
}

/// Withholding is divided by four unless the filer elects to show when it
/// was withheld.
#[test]
fn estimated_withholding_as_withheld() {
    let mut h = Household {
        withheld: DOLLAR * 36_000,
        ..tax(DOLLAR * 40_000)
    };
    for inst in rules().schedule(&h.facts()).unwrap().value() {
        assert_eq!(
            inst.shortfall(),
            USCents(0),
            "installment {}",
            inst.number()
        );
    }
    h.as_withheld = true;
    for (i, inst) in rules()
        .schedule(&h.facts())
        .unwrap()
        .value()
        .iter()
        .enumerate()
    {
        let want = if i == 3 { USCents(0) } else { DOLLAR * 9_000 };
        assert_eq!(inst.shortfall(), want, "installment {}", i + 1);
    }
}

/// Neither the Code nor the forms provide a waiver, so asking for one
/// changes nothing and says so.
#[test]
fn estimated_waiver_is_not_available() {
    let plain = rules().penalty(&ten().facts(), day(2026, 12, 31)).unwrap();
    let h = Household {
        waiver: Some(WaiverKind::Casualty),
        ..ten()
    };
    let asked = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
    assert_eq!(asked.value().total(), plain.value().total());
    assert!(plain.value().total().0 != 0);
    assert!(applied(&asked.applied()).contains("provide a waiver"));
    let o = asked.value().waiver_outcome().unwrap();
    assert!(o.refused());
    assert_eq!(asked.value().if_waived(), asked.value().total());
    // Retiring in 2026 at 64 meets the federal conditions, but Virginia has
    // no such waiver.
    let retired = estimated::new_positions()
        .waiver_for(WaiverCause::Retirement, day(2026, 6, 1))
        .born(day(1962, 3, 1))
        .build()
        .unwrap();
    let h = Household {
        positions: Some(retired),
        ..ten()
    };
    let res = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
    let o = res.value().waiver_outcome().unwrap();
    assert!(o.refused() && o.reason().contains("retirement"));
    assert_eq!(res.value().total(), plain.value().total());
}

/// Under the income floor no target is needed, so no question about last
/// year's return or this year's tax is asked.
#[test]
fn estimated_income_floor_asks_nothing_about_the_target() {
    let h = Household {
        no_tax: true,
        agi: Some(DOLLAR * 5_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(res.value().exempt());
    for q in res.required() {
        assert!(
            !q.effect().to_lowercase().contains("target"),
            "{}",
            q.effect()
        );
    }
    assert!(!has(&res.required(), PositionKind::ThisYearTax));
    assert!(!has(&res.required(), PositionKind::PriorYearReturn));
}

/// Two-thirds of gross income from farming, fishing or work as a merchant
/// seaman puts a filer under Form 760F, which is not built.
#[test]
fn estimated_refuses_what_is_not_built() {
    for year in [2025, 2027] {
        let err = estimated_tax(year).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{year}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let base = || {
        let mut b = estimated::new_facts(VA, 2026);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    let seaman = new_estimated_facts()
        .merchant_seaman_share(Rate(6_667))
        .build()
        .unwrap();
    let mixed = new_estimated_facts()
        .merchant_seaman_share(Rate(3_000))
        .build()
        .unwrap();
    for (name, f, unsupported) in [
        (
            "a farmer",
            base().farm_share(Rate(6_667)).build().unwrap(),
            true,
        ),
        (
            "a fisherman",
            base().fish_share(Rate(6_667)).build().unwrap(),
            true,
        ),
        (
            "a merchant seaman",
            base().state(seaman).build().unwrap(),
            true,
        ),
        (
            "farming and seafaring",
            base().farm_share(Rate(3_667)).state(mixed).build().unwrap(),
            true,
        ),
        (
            "just under two-thirds",
            base().farm_share(Rate(6_666)).build().unwrap(),
            false,
        ),
    ] {
        let got = rules()
            .requirement(&f)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::Unsupported));
        assert_eq!(got, unsupported, "{name}");
    }
    let period = |m, d| {
        estimated::new_period()
            .end(day(2026, m, d))
            .build()
            .unwrap()
    };
    for (name, f) in [
        (
            "Pennsylvania's facts",
            estimated::new_facts(estimated::PENNSYLVANIA, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(VA, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            base().state("not Virginia's").build().unwrap(),
        ),
        (
            "wrong period ends",
            base()
                .elections(estimated::new_elections().annualised().build().unwrap())
                .periods(&[period(3, 31), period(5, 31), period(8, 31), period(12, 31)])
                .build()
                .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        new_estimated_facts()
            .merchant_seaman_share(Rate(10_001))
            .build()
            .is_err()
    );
    // Not told about seafaring, the rules ask and assume none.
    let res = rules().requirement(&base().build().unwrap()).unwrap();
    assert!(because(&res.required(), PositionKind::JurisdictionFact).contains("merchant seaman"));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&ten().facts()).unwrap();
    let want = [
        (
            "When estimated payments are required",
            "more than $1,000",
            "23VAC10-112-20",
        ),
        (
            "The small-underpayment exception",
            "$150 or less",
            "2026 Acts c. 7",
        ),
        (
            "The January installment for a return filed by March 1",
            "for any filer",
            "§ 58.1-490(I)",
        ),
        (
            "A requirement first met after April 15",
            "withholding is still divided by four",
            "58.1-492(C)(1) and (D)(2)",
        ),
        (
            "Exceptions 3 and 4 and the January installment",
            "do not apply to the fourth installment",
            "(C)(1)(c) and (C)(2)",
        ),
        (
            "Withholding on the dates withheld",
            "that period's column",
            "58.1-492(B)(2) and (D)(2)",
        ),
        ("The daily rate", ".00025", "23VAC10-112-42"),
    ];
    let ds = res.divergences();
    assert_eq!(ds.len(), want.len());
    for d in &ds {
        let Some((_, form, statute)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(d.form().contains(form) && d.statute_source().contains(statute));
        assert!(!d.form_source().is_empty() && !d.statute().is_empty());
        let uses = if d.rule() == "The small-underpayment exception" {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert_eq!(d.uses(), uses, "{:?}", d.rule());
        assert_eq!(
            d.affects_household(),
            d.rule() == "The daily rate",
            "{:?}",
            d.rule()
        );
    }
    // Shortfalls of exactly $1,000 are excused by the law and not by the
    // 2025 form's $150.
    for d in rules()
        .requirement(&tax(USCents(111_111)).facts())
        .unwrap()
        .divergences()
    {
        if d.rule() == "The small-underpayment exception" {
            assert!(d.affects_household());
        }
    }
}

#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    let text = format!(
        "{}{}{}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$1,000",
        "May 1",
        "90%",
        "100%",
        "no 110% tier",
        "March 1, 2027",
        "8%",
        "9%",
        ".00025",
        "pays that installment before an earlier one",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
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
    let f = estimated::new_facts(VA, 2026)
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
