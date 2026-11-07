//! Hawaii's estimated tax rules for 2026, checked against Form N-210's
//! arithmetic and the department's own example. Ported from estimated_test.go
//! and estimated_refund_test.go. The Go tests that find each figure in its
//! extract in testdata/ (TestEstimatedFiguresAreInTheirSources,
//! TestEstimatedStatuteUnchangedSince2003) read the Go package's citations and
//! are not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, FactsBuilder, Month, Payment, Positions, PriorReturn, Status,
    WaiverCause, WaiverKind, Weekday,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_hawaiitaxcalc as hawaiitaxcalc;
use working::{PositionKind, Reading, Required, Rule};

const HI: estimated::Jurisdiction = estimated::HAWAII;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    hawaiitaxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

fn asks(rs: &[Required], k: PositionKind) -> String {
    rs.iter()
        .filter(|r| r.position() == k)
        .map(|r| format!("{} {}", r.question(), r.effect()))
        .collect::<Vec<_>>()
        .join(" | ")
}

fn applied(rs: &[Rule]) -> String {
    rs.iter()
        .map(|r| format!("{}: {}", r.cite(), r.applied()))
        .collect::<Vec<_>>()
        .join(" | ")
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

/// The 2026 dates the department prints.
fn printed() -> [Date; 4] {
    [
        day(2026, 4, 20),
        day(2026, 6, 20),
        day(2026, 9, 20),
        day(2027, 1, 20),
    ]
}

/// The dates they move to.
fn dues() -> [Date; 4] {
    [
        day(2026, 4, 20),
        day(2026, 6, 22),
        day(2026, 9, 21),
        day(2027, 1, 20),
    ]
}

#[derive(Clone, Default)]
struct Household {
    status: filing::Status,
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    withheld_on: Date,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<hawaiitaxcalc::EstimatedFacts>,
    no_extras: bool,
    return_filed: Date,
    first_met: Date,
    periods: Option<Vec<USCents>>,
    as_withheld: bool,
    waiver: Option<WaiverKind>,
    positions: Option<Positions>,
}

fn h(tax: i64) -> Household {
    Household {
        tax: DOLLAR * tax,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let status = if self.status == filing::Status::Unspecified {
            filing::Status::Single
        } else {
            self.status
        };
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        let mut b = estimated::new_facts(HI, 2026);
        b.filing_status(status)
            .this_year(fb.build().unwrap())
            .paid(&self.payments)
            .return_filed(self.return_filed)
            .requirement_first_met(self.first_met);
        if self.withheld.0 > 0 {
            let on = if self.withheld_on.is_zero() {
                day(2026, 12, 15)
            } else {
                self.withheld_on
            };
            b.withheld(&[estimated::new_withheld()
                .on(on)
                .amount(self.withheld)
                .build()
                .unwrap()]);
        }
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        if let Some(e) = self.extras {
            b.state(e);
        } else if !self.no_extras {
            b.state(
                hawaiitaxcalc::new_estimated_facts()
                    .prior_year_farmer_or_fisher(false)
                    .nonresident_alien_without_wages(false)
                    .build()
                    .unwrap(),
            );
        }
        let mut eb = estimated::new_elections();
        if let Some(periods) = &self.periods {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            let ps: Vec<_> = periods
                .iter()
                .enumerate()
                .map(|(i, &tax)| {
                    estimated::new_period()
                        .end(ends[i])
                        .annualised_tax(tax)
                        .build()
                        .unwrap()
                })
                .collect();
            b.periods(&ps);
            eb.annualised();
        }
        if self.as_withheld {
            eb.withholding_as_withheld();
        }
        b.elections(eb.build().unwrap());
        if let Some(k) = self.waiver {
            b.positions(estimated::new_positions().waiver(k).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed twelve-month Hawaii return with its tax, for a full-year
/// resident.
fn prior(tax: USCents) -> Option<PriorReturn> {
    let f = estimated::new_figures().tax(tax).build().unwrap();
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(f)
            .full_year_resident(true)
            .build()
            .unwrap(),
    )
}

fn weekend(d: Date) -> bool {
    d.weekday() == Weekday::SATURDAY || d.weekday() == Weekday::SUNDAY
}

// The department prints April 20, June 20, September 20 and January 20.
// June 20, 2026 is a Saturday and September 20, 2026 a Sunday, so under HRS
// § 231-21 those two are due Monday, June 22 and Monday, September 21. The
// other two are weekdays and do not move. No HRS § 8-1 holiday falls on a due
// date or on the day it moves to.
//
// January 31, 2027, the file-and-pay date, is a Sunday. Neither HAR
// § 18-235-97(h)(2) nor the forms say it moves, so it is not moved, and a
// return filed on Monday, February 1 is asked about.
#[test]
fn due_dates_for_twenty_twenty_six() {
    let holidays = [
        (day(2026, 5, 25), "Memorial Day"),
        (day(2026, 6, 11), "King Kamehameha I Day"),
        (day(2026, 8, 21), "Statehood Day"),
        (day(2026, 9, 7), "Labor Day"),
        (day(2027, 1, 1), "New Year's Day"),
        (day(2027, 1, 18), "Dr. Martin Luther King, Jr., Day"),
        (
            day(2027, 3, 26),
            "Prince Jonah Kuhio Kalanianaole Day and Good Friday",
        ),
    ];
    for (d, _) in holidays {
        assert!(!weekend(d), "{d} is listed as a weekday holiday");
    }
    let holiday = |d: Date| holidays.iter().find(|(h, _)| *h == d).map(|(_, n)| *n);

    let res = rules().schedule(&h(40_000).facts()).unwrap();
    for (i, s) in res.value().iter().enumerate() {
        assert_eq!(s.due(), dues()[i], "installment {}", i + 1);
        let mut moved = printed()[i];
        while weekend(moved) {
            moved = moved.add_days(1);
        }
        assert_eq!(moved, dues()[i]);
        assert!(holiday(dues()[i]).is_none());
    }
    assert_eq!(printed()[1].weekday(), Weekday::SATURDAY);
    assert_eq!(printed()[2].weekday(), Weekday::SUNDAY);
    for s in [
        "June 20, 2026 falls on a Saturday, so a payment on June 22, 2026 is on time",
        "September 20, 2026 falls on a Sunday, so a payment on September 21, 2026 is on time",
    ] {
        assert!(
            applied(&res.applied()).contains(s),
            "the working does not say {s:?}"
        );
    }
    assert!(holiday(day(2027, 2, 1)).is_none());
    assert!(holiday(day(2027, 4, 20)).is_none() && !weekend(day(2027, 4, 20)));

    // A quarter of 60% of $40,000 is $6,000. Paid on the Monday each date
    // moved to, each installment is met.
    let d = dues();
    let paid = Household {
        payments: d.iter().map(|&on| pay(on, DOLLAR * 6_000)).collect(),
        ..h(40_000)
    };
    for (i, s) in rules()
        .schedule(&paid.facts())
        .unwrap()
        .value()
        .iter()
        .enumerate()
    {
        assert_eq!(s.status(), Status::Met, "installment {}", i + 1);
    }
    assert_eq!(
        rules()
            .penalty(&paid.facts(), day(2027, 4, 20))
            .unwrap()
            .value()
            .total(),
        USCents(0)
    );

    assert_eq!(day(2027, 1, 31).weekday(), Weekday::SUNDAY);
    for (filed, excused, asked) in [
        (day(2027, 1, 29), true, false),
        (day(2027, 1, 31), true, false),
        (day(2027, 2, 1), false, true),
        (day(2027, 2, 2), false, false),
    ] {
        let res = rules()
            .schedule(
                &Household {
                    return_filed: filed,
                    ..h(40_000)
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(
            res.value()[3].status() == Status::Excused,
            excused,
            "filed {filed}"
        );
        assert_eq!(
            asks(&res.required(), PositionKind::UnsettledRule).contains("Sunday"),
            asked,
            "filed {filed}"
        );
    }
}

// N-210 Exception 1: no penalty when the tax less withholding is under $500.
// $1,000 of tax keeps 60% of it, $600, above the withholding, so the
// installment that is not exempt is short and owes a penalty.
#[test]
fn threshold_boundary() {
    for (withheld, exempt) in [(50_001, true), (50_000, false)] {
        let hh = Household {
            withheld: USCents(withheld),
            ..h(1_000)
        };
        let res = rules().requirement(&hh.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "{}", res.value().why());
        assert_ne!(res.value().required(), exempt);
        if exempt {
            let c = res.value().chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && c.label() == "Tax less withholding under $500"
            );
        }
        let p = rules().penalty(&hh.facts(), day(2027, 4, 20)).unwrap();
        assert_eq!(p.value().total().0 == 0, exempt, "withheld {withheld}");
    }
    // At $500 the worksheet's own test, the $600 target less $500 withheld,
    // is $100, under $500. The working says why payments are still shown.
    let res = rules()
        .requirement(
            &Household {
                withheld: USCents(50_000),
                ..h(1_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(applied(&res.applied()).contains("Worksheet line 13"));
}

// The target is the smaller of 60% of this year's tax and 100% of last
// year's, at any income, and the prior-year test needs a filed twelve-month
// Hawaii return.
#[test]
fn target_options() {
    // 60% of $10,000 is $6,000.
    for (prior_tax, basis, target) in [
        (599_999, Basis::PriorYearTax, 599_999),
        (600_001, Basis::ThisYearTax, 600_000),
    ] {
        let r = rules()
            .requirement(
                &Household {
                    prior: prior(USCents(prior_tax)),
                    ..h(10_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        assert_eq!(r.chosen().unwrap().basis(), basis);
        assert_eq!(r.target(), USCents(target));
        assert_eq!(r.options()[0].percent(), percent(60));
    }
    // No 110% step: $1,000,000 of federal AGI last year still gives 100%.
    let rich = estimated::new_prior_return()
        .filed(true)
        .full_year_resident(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 50_000)
                .federal_agi(DOLLAR * 1_000_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                prior: Some(rich),
                ..h(100_000)
            }
            .facts(),
        )
        .unwrap();
    let o = res.value().options()[1].clone();
    assert!(
        o.percent() == percent(100)
            && o.amount() == DOLLAR * 50_000
            && res.value().target() == DOLLAR * 50_000
    );
    assert!(!has(&res.required(), PositionKind::AdjustedGrossIncome));

    let f = estimated::new_figures()
        .tax(DOLLAR * 1_000)
        .build()
        .unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(f)
        .full_year_resident(false)
        .build()
        .unwrap();
    let not_filed = estimated::new_prior_return()
        .filed(false)
        .full_year_resident(false)
        .build()
        .unwrap();
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(false)
        .build()
        .unwrap();
    for (name, p, ask) in [
        ("no return on record", None, true),
        ("not filed", Some(not_filed), false),
        ("not required", Some(not_required), false),
        ("a six-month year", Some(short), false),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: p,
                    ..h(10_000)
                }
                .facts(),
            )
            .unwrap();
        assert!(!res.value().options()[1].available(), "{name}");
        assert_eq!(res.value().target(), DOLLAR * 6_000, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn),
            ask,
            "{name}"
        );
    }

    // Withholding that reaches the target needs no payments; a cent short
    // does.
    for (withheld, required) in [(600_000, false), (599_999, true)] {
        let r = rules()
            .requirement(
                &Household {
                    withheld: USCents(withheld),
                    ..h(10_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        assert_eq!(r.required(), required, "withheld {withheld}");
    }
}

// $40,000 of tax, nothing withheld or paid, no return last year: the target
// is 60%, $24,000, and each installment $6,000. Figured to April 20, 2027,
// N-210 line 19 prints the months as 12, 10, 7 and 3, counted from the
// printed dates, not the moved ones:
//
//	April 20:     6,000 × .00667 × 12 = 480.24
//	June 20:      6,000 × .00667 × 10 = 400.20
//	September 20: 6,000 × .00667 × 7  = 280.14
//	January 20:   6,000 × .00667 × 3  = 120.06
//
// The total is $1,280.64.
#[test]
fn penalty_months_match_the_form() {
    let res = rules()
        .penalty(&h(40_000).facts(), day(2027, 4, 20))
        .unwrap();
    let p = res.value();
    let lines = p.lines();
    assert_eq!(lines.len(), 4);
    let months = [12, 10, 7, 3];
    let charges = [48_024, 40_020, 28_014, 12_006];
    for (i, l) in lines.iter().enumerate() {
        let parts = l.parts();
        assert!(
            l.shortfall() == DOLLAR * 6_000 && l.due() == dues()[i] && parts.len() == 1,
            "line {}",
            i + 1
        );
        let pt = &parts[0];
        assert!(
            pt.months() == months[i]
                && pt.charge() == USCents(charges[i])
                && pt.from() == printed()[i]
                && pt.to() == day(2027, 4, 20)
                && pt.rate() == percent(8),
            "line {}: {pt:?}",
            i + 1
        );
    }
    assert_eq!(p.total(), USCents(128_064));
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));
    // The stop: figured to June 30, 2027, it is still to April 20.
    let late = rules()
        .penalty(&h(40_000).facts(), day(2027, 6, 30))
        .unwrap()
        .value();
    assert_eq!(late.as_of(), day(2027, 4, 20));
    assert_eq!(late.total(), USCents(128_064));
}

// The form multiplies by .00667 a month; the statute's two-thirds of one
// percent is .006666…. 60% of $66,666.67 is $40,000.00, $10,000 a quarter.
// Nothing is paid until $10,000 on January 20, 2027, which pays April's
// shortfall first, nine months after it fell due. The form gives
// 10,000 × .00667 × 9 = $600.30 and the statute 10,000 × 2/3% × 9 = $600.00.
// The figure is the form's.
#[test]
fn charge_uses_the_printed_factor() {
    let hh = Household {
        tax: USCents(6_666_667),
        payments: vec![pay(day(2027, 1, 20), DOLLAR * 10_000)],
        ..Household::default()
    };
    let lines = rules()
        .penalty(&hh.facts(), day(2027, 4, 20))
        .unwrap()
        .value()
        .lines();
    assert!(!lines.is_empty() && lines[0].installment() == 1);
    let parts = lines[0].parts();
    assert!(
        parts.len() == 1
            && parts[0].months() == 9
            && parts[0].to() == day(2027, 1, 20)
            && parts[0].charge() == USCents(60_030),
        "{parts:?}"
    );
    let statute = USCents(10_000 * 100 * 2 * 9 / 300);
    assert_eq!(statute, USCents(60_000));
    assert_eq!(parts[0].charge() - statute, USCents(30));
}

// A month runs from the 21st to the 20th. $1,200 short in April (60% of
// $8,000 is $4,800, $1,200 a quarter):
//
//	paid May 20:  1 month,  1,200 × .00667     = 8.004  → $8.00
//	paid May 21:  2 months, 1,200 × .00667 × 2 = 16.008 → $16.01
//	paid June 21 or 22, the Sunday and Monday after June 20, a Saturday: the
//	  form counts it as paid June 20, so 2 months, $16.01
//	paid June 23: 3 months, 1,200 × .00667 × 3 = 24.012 → $24.01
#[test]
fn months_run_from_the_21st() {
    for (paid, months, charge) in [
        (day(2026, 5, 20), 1, 800),
        (day(2026, 5, 21), 2, 1_601),
        (day(2026, 6, 21), 2, 1_601),
        (day(2026, 6, 22), 2, 1_601),
        (day(2026, 6, 23), 3, 2_401),
    ] {
        let hh = Household {
            payments: vec![pay(paid, DOLLAR * 1_200)],
            ..h(8_000)
        };
        let lines = rules()
            .penalty(&hh.facts(), day(2026, 7, 31))
            .unwrap()
            .value()
            .lines();
        assert!(
            !lines.is_empty() && lines[0].installment() == 1,
            "paid {paid}"
        );
        let parts = lines[0].parts();
        assert!(
            parts.len() == 1 && parts[0].months() == months && parts[0].charge() == USCents(charge),
            "paid {paid}: {parts:?}"
        );
    }
}

// The N-210 instructions' example: Mr. Aloha is $1,000 short in April, and
// June's installment is $2,500. His $2,500 on June 10 pays April's $1,000
// first, which the form figures "to June 10th (2 months)", and $1,500 toward
// June, which is left $1,000 short. 1,000 × .00667 × 2 = $13.34.
#[test]
fn months_match_mr_alohas_example() {
    // 60% of $16,666.67 is $10,000.00, $2,500 a quarter.
    let hh = Household {
        tax: USCents(1_666_667),
        payments: vec![
            pay(dues()[0], DOLLAR * 1_500),
            pay(day(2026, 6, 10), DOLLAR * 2_500),
        ],
        ..Household::default()
    };
    let sched = rules().schedule(&hh.facts()).unwrap().value();
    assert_eq!(sched[0].shortfall(), DOLLAR * 1_000);
    assert_eq!(sched[1].shortfall(), DOLLAR * 1_000);
    let lines = rules()
        .penalty(&hh.facts(), day(2026, 6, 30))
        .unwrap()
        .value()
        .lines();
    let parts = lines[0].parts();
    assert!(
        parts.len() == 1
            && parts[0].months() == 2
            && parts[0].to() == day(2026, 6, 10)
            && parts[0].charge() == USCents(1_334),
        "{parts:?}"
    );
}

// HRS § 235-97(g): withholding is paid in equal parts on the due dates,
// unless the filer shows when it was actually withheld. Last year's
// overpayment applied forward counts on April 20.
#[test]
fn withholding() {
    // $8,000 withheld in December, against $6,000 a quarter.
    let mut hh = Household {
        withheld: DOLLAR * 8_000,
        ..h(40_000)
    };
    let s = rules().schedule(&hh.facts()).unwrap().value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.credited_by(), DOLLAR * 2_000 * (i as i64 + 1));
    }
    hh.as_withheld = true;
    let s = rules().schedule(&hh.facts()).unwrap().value();
    for (i, want) in [0, 0, 0, 800_000].into_iter().enumerate() {
        assert_eq!(s[i].credited_by(), USCents(want));
    }
    let hh = Household {
        payments: vec![
            estimated::new_payment()
                .amount(DOLLAR * 6_000)
                .from_prior_year()
                .build()
                .unwrap(),
        ],
        ..h(40_000)
    };
    let s = rules().schedule(&hh.facts()).unwrap().value();
    assert_eq!(s[0].status(), Status::Met);
}

// N-210 Schedule A: 15%, 30%, 45% and 60% of the tax after credits on
// annualised income, less earlier installments, capped by the regular
// installment plus what was lowered earlier. $20,000 of tax, so a regular
// $3,000 a quarter; annualised tax of $4,000, $8,000, $12,000 and $20,000:
//
//	(a) 15% × 4,000 = 600;                cap 3,000;                    600
//	(b) 30% × 8,000 = 2,400, less 600 = 1,800; cap 3,000 + 2,400 = 5,400; 1,800
//	(c) 45% × 12,000 = 5,400, less 2,400 = 3,000; cap 3,000 + 3,600 = 6,600; 3,000
//	(d) 60% × 20,000 = 12,000, less 5,400 = 6,600; cap 3,000 + 3,600 = 6,600; 6,600
#[test]
fn annualised_installments() {
    let mut periods = vec![
        DOLLAR * 4_000,
        DOLLAR * 8_000,
        DOLLAR * 12_000,
        DOLLAR * 20_000,
    ];
    let sched = |periods: &[USCents]| {
        rules()
            .schedule(
                &Household {
                    periods: Some(periods.to_vec()),
                    ..h(20_000)
                }
                .facts(),
            )
            .unwrap()
            .value()
    };
    let s = sched(&periods);
    for (i, want) in [60_000, 240_000, 540_000, 1_200_000]
        .into_iter()
        .enumerate()
    {
        assert_eq!(s[i].needed_by(), USCents(want), "installment {}", i + 1);
    }
    // $15,000 for the year instead: (d) is 60% × 15,000 = 9,000, less 5,400
    // = 3,600, under the cap of 6,600, so $9,000 is needed by January.
    periods[3] = DOLLAR * 15_000;
    assert_eq!(sched(&periods)[3].needed_by(), USCents(900_000));
    // $40,000 of annualised tax by March 31 is 15%, $6,000, capped at the
    // regular $3,000.
    periods[0] = DOLLAR * 40_000;
    assert_eq!(sched(&periods)[0].needed_by(), USCents(300_000));
    // The periods must be Schedule A's.
    let ps: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 6, 30),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .into_iter()
    .map(|end| estimated::new_period().end(end).build().unwrap())
    .collect();
    let f = estimated::new_facts(HI, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .periods(&ps)
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    assert!(rules().schedule(&f).is_err());
}

// The exceptions: under $500 (threshold_boundary), the fourth installment for
// a return paid by January 31, no liability last year, the waivers, and
// farmers and nonresident aliens (refused).
#[test]
fn exceptions() {
    // No liability last year, as a full-year Hawaii resident: no return was
    // required, or one showed no tax. Exempt, and the zero is exact.
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    for (name, pr) in [
        ("not required", Some(not_required)),
        ("no tax", prior(USCents(0))),
    ] {
        let hh = Household {
            prior: pr,
            ..h(40_000)
        };
        let res = rules().requirement(&hh.facts()).unwrap();
        assert!(
            res.value().exempt() && !res.value().required(),
            "{name}: {}",
            res.value().why()
        );
        let p = rules()
            .penalty(&hh.facts(), day(2027, 4, 20))
            .unwrap()
            .value();
        assert!(p.total().0 == 0 && !p.upper_bound(), "{name}");
    }
    // The same, not a Hawaii resident all year: the form's exception does
    // not apply, and the adopted federal text is reported as the filer's to
    // claim.
    let away = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(false)
        .build()
        .unwrap();
    let res = rules()
        .penalty(
            &Household {
                prior: Some(away),
                ..h(40_000)
            }
            .facts(),
            day(2027, 4, 20),
        )
        .unwrap();
    assert_eq!(res.value().total(), USCents(128_064));
    assert!(applied(&res.applied()).contains(
        "U.S. citizen or resident throughout 2025, and if you were, the exception is yours to claim"
    ));
    // Residence not said: asked, and not applied.
    let unsaid = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let res = rules()
        .penalty(
            &Household {
                prior: Some(unsaid),
                ..h(40_000)
            }
            .facts(),
            day(2027, 4, 20),
        )
        .unwrap();
    assert!(has(&res.required(), PositionKind::Residency) && res.value().total().0 != 0);

    // A waiver asked for is not subtracted.
    let res = rules()
        .penalty(
            &Household {
                waiver: Some(WaiverKind::Casualty),
                ..h(40_000)
            }
            .facts(),
            day(2027, 4, 20),
        )
        .unwrap();
    assert!(
        res.value().total() == USCents(128_064)
            && res.value().waiver() == WaiverKind::Casualty
            && !has(&res.required(), PositionKind::PenaltyWaiver)
    );

    // Filed and paid by January 31: January is excused, and the earlier
    // shortfalls stop on January 20. $6,000 each for 9, 7 and 4 months:
    // 360.18 + 280.14 + 160.08 = $800.40.
    let p = rules()
        .penalty(
            &Household {
                return_filed: day(2027, 1, 29),
                ..h(40_000)
            }
            .facts(),
            day(2027, 4, 20),
        )
        .unwrap()
        .value();
    assert!(p.as_of() == day(2027, 1, 20) && p.total() == USCents(80_040) && p.lines().len() == 3);
    // Filed and paid March 1: every shortfall is paid that day. 11, 9, 6 and
    // 2 months: 440.22 + 360.18 + 240.12 + 80.04 = $1,120.56.
    let p = rules()
        .penalty(
            &Household {
                return_filed: day(2027, 3, 1),
                ..h(40_000)
            }
            .facts(),
            day(2027, 4, 20),
        )
        .unwrap()
        .value();
    assert!(p.as_of() == day(2027, 3, 1) && p.total() == USCents(112_056));

    // A late start: the rule's schedule is reported and the form's quarters
    // are charged.
    let late = rules()
        .penalty(
            &Household {
                first_met: day(2026, 7, 1),
                ..h(40_000)
            }
            .facts(),
            day(2027, 4, 20),
        )
        .unwrap();
    assert!(
        late.value().total() == USCents(128_064)
            && applied(&late.applied()).contains("HAR § 18-235-97(e)(2)(A)")
    );
    let early = rules()
        .penalty(
            &Household {
                first_met: day(2026, 3, 31),
                ..h(40_000)
            }
            .facts(),
            day(2027, 4, 20),
        )
        .unwrap();
    assert!(!applied(&early.applied()).contains("HAR § 18-235-97(e)(2)(A)"));
}

// Each divergence is reported with both readings and both sources.
#[test]
fn divergences_pin_both_readings() {
    let res = rules().requirement(&h(40_000).facts()).unwrap();
    let want: [(&str, [&str; 4]); 3] = [
        (
            "No liability last year",
            [
                "full-year Hawaii resident",
                "Part I box A",
                "U.S. citizen or resident",
                "6654(e)(2)",
            ],
        ),
        (
            "The monthly rate",
            [
                ".00667",
                "line 20",
                "two-thirds of one per cent",
                "HRS § 235-97(f)",
            ],
        ),
        (
            "A requirement first met after March 31",
            [
                "a quarter of the required annual payment",
                "line 9",
                "three equal parts from June 20",
                "§ 18-235-97(e)(2)(A)",
            ],
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, w)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(
            d.form().contains(w[0])
                && d.form_source().contains(w[1])
                && d.statute().contains(w[2])
                && d.statute_source().contains(w[3]),
            "{:?}",
            d.rule()
        );
        assert_eq!(d.uses(), Reading::Form);
        // $40,000 unpaid: the rate changes the charge, and neither last
        // year's residence nor a late start is in the facts.
        assert_eq!(d.affects_household(), d.rule() == "The monthly rate");
    }
    // No tax last year and not a resident all of it: the residence test is
    // what decides the exception.
    let away = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(false)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                prior: Some(away),
                ..h(40_000)
            }
            .facts(),
        )
        .unwrap();
    for d in res.divergences() {
        if d.rule() == "No liability last year" {
            assert!(d.affects_household());
        }
    }
}

#[test]
fn refuses_what_is_not_built() {
    for y in [2025, 2027] {
        assert!(
            hawaiitaxcalc::estimated_tax(y)
                .err()
                .is_some_and(|e| e.is(&estimated::Error::NoRules)),
            "{y}"
        );
    }
    let this = estimated::new_figures()
        .tax(DOLLAR * 40_000)
        .build()
        .unwrap();
    let base = || -> FactsBuilder {
        let mut b = estimated::new_facts(HI, 2026);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    let prior_farmer = hawaiitaxcalc::new_estimated_facts()
        .prior_year_farmer_or_fisher(true)
        .build()
        .unwrap();
    let alien = hawaiitaxcalc::new_estimated_facts()
        .nonresident_alien_without_wages(true)
        .build()
        .unwrap();
    let unsupported: [(&str, Facts); 5] = [
        ("a farmer", base().farm_share(Rate(7000)).build().unwrap()),
        (
            "a fisherman",
            base().fish_share(Rate(7000)).build().unwrap(),
        ),
        (
            "farming and fishing",
            base()
                .farm_share(Rate(3400))
                .fish_share(Rate(3300))
                .build()
                .unwrap(),
        ),
        (
            "a farmer last year",
            base().state(prior_farmer).build().unwrap(),
        ),
        (
            "a nonresident alien no wage",
            base().state(alien).build().unwrap(),
        ),
    ];
    for (name, f) in unsupported {
        assert!(
            rules()
                .requirement(&f)
                .unwrap_err()
                .is(&estimated::Error::Unsupported),
            "{name}"
        );
    }
    assert!(
        rules()
            .requirement(&base().farm_share(Rate(6666)).build().unwrap())
            .is_ok()
    );
    let refused: [(&str, Facts); 3] = [
        (
            "California's facts",
            estimated::new_facts(estimated::CALIFORNIA, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(HI, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            base().state("not Hawaii's").build().unwrap(),
        ),
    ];
    for (name, f) in refused {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&base().build().unwrap(), Date::default())
            .is_err()
    );
    // Neither Hawaii fact said: both asked for.
    let res = rules().requirement(&base().build().unwrap()).unwrap();
    let asked = asks(&res.required(), PositionKind::JurisdictionFact);
    assert!(
        asked.contains("farming or fishing") && asked.contains("nonresident alien"),
        "{asked}"
    );
}

#[test]
fn explanation_names_the_differences() {
    let e = rules().explanation();
    let all = format!(
        "{} {} {}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$500",
        "60%",
        "100% of last year's",
        "no 110%",
        "on the 20th",
        "June 22",
        "September 21",
        "two-thirds of one percent",
        "month or part of a month",
        "21st to the 20th",
        ".00667",
        "April 20",
        "January 31",
        "January 20",
        "15%, 30%, 45% and 60%",
        "retired after 62",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.versus_federal().is_empty() && !e.sources().is_empty());
}

// Without this year's tax the target is last year's tax alone, and the
// penalty against it is an upper bound. $8,000 last year, $2,000 a quarter,
// unpaid to April 20, 2027: 2,000 × .00667 × (12 + 10 + 7 + 3) = $426.88.
#[test]
fn without_this_years_tax_uses_last_years_alone() {
    let hh = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000),
        ..Household::default()
    };
    let res = rules().requirement(&hh.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 8_000 && !r.exempt());
    let o = r.options()[0].clone();
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearTax);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&hh.facts()).unwrap().value();
    for (i, want) in [200_000, 400_000, 600_000, 800_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), USCents(want));
    }
    let pen = rules()
        .penalty(&hh.facts(), day(2027, 4, 20))
        .unwrap()
        .value();
    assert!(pen.upper_bound() && pen.total() == USCents(42_688));
    let paid = Household {
        payments: dues().iter().map(|&d| pay(d, DOLLAR * 2_000)).collect(),
        ..hh.clone()
    };
    assert_eq!(
        rules()
            .penalty(&paid.facts(), day(2027, 4, 20))
            .unwrap()
            .value()
            .total(),
        USCents(0)
    );

    // No liability last year still exempts, exactly.
    let none = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let free = Household {
        no_tax: true,
        prior: Some(none),
        ..Household::default()
    };
    assert!(rules().requirement(&free.facts()).unwrap().value().exempt());
    let pen = rules()
        .penalty(&free.facts(), day(2027, 4, 20))
        .unwrap()
        .value();
    assert!(pen.total().0 == 0 && !pen.upper_bound());

    // With no return last year either, there is no target.
    let nothing = Household {
        no_tax: true,
        ..Household::default()
    };
    let r = rules().requirement(&nothing.facts()).unwrap().value();
    assert!(!r.target_known() && !r.required());
    assert!(
        rules()
            .schedule(&nothing.facts())
            .unwrap_err()
            .is(&estimated::Error::NeedsThisYearTax)
    );
    assert!(
        rules()
            .penalty(&nothing.facts(), day(2027, 4, 20))
            .unwrap_err()
            .is(&estimated::Error::NeedsThisYearTax)
    );
}

// Hawaii adopts the federal waivers, and Form N-210's instructions state
// them. The penalty is the same $1,280.64 in each case; only the outcome
// differs.
#[test]
fn waiver_outcomes() {
    let pos = |c: WaiverCause, on: Date, born: Option<Date>| {
        let mut b = estimated::new_positions();
        b.waiver_for(c, on);
        if let Some(born) = born {
            b.born(born);
        }
        b.build().unwrap()
    };
    let cases = [
        (
            "retired in 2026 at 64",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1962, 3, 1)),
            ),
            "eligible",
            "after reaching 62",
        ),
        (
            "retired in 2025",
            pos(
                WaiverCause::Retirement,
                day(2025, 6, 1),
                Some(day(1960, 3, 1)),
            ),
            "eligible",
            "retired in 2025",
        ),
        (
            "retired in 2020",
            pos(
                WaiverCause::Retirement,
                day(2020, 6, 1),
                Some(day(1955, 3, 1)),
            ),
            "refused",
            "2025 or 2026",
        ),
        (
            "retired at 60",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1966, 3, 1)),
            ),
            "refused",
            "reached 62",
        ),
        (
            "disabled in 2025",
            pos(WaiverCause::Disability, day(2025, 9, 1), None),
            "eligible",
            "became disabled in 2025",
        ),
        (
            "a disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
            "eligible",
            "Hawaii Department of Taxation",
        ),
        (
            "retirement date not given",
            estimated::new_positions()
                .waiver(WaiverKind::RetiredOrDisabled)
                .build()
                .unwrap(),
            "undecided",
            "date you retired",
        ),
    ];
    for (name, p, want, says) in cases {
        let pen = rules()
            .penalty(
                &Household {
                    positions: Some(p),
                    ..h(40_000)
                }
                .facts(),
                day(2027, 4, 20),
            )
            .unwrap()
            .value();
        let o = pen.waiver_outcome().unwrap();
        let got = if o.eligible() {
            "eligible"
        } else if o.refused() {
            "refused"
        } else if o.undecided() {
            "undecided"
        } else {
            ""
        };
        assert_eq!(pen.total(), USCents(128_064), "{name}");
        assert!(
            got == want && o.reason().contains(says),
            "{name}: {got} {:?}",
            o.reason()
        );
        assert_eq!(o.eligible(), pen.if_waived().0 == 0, "{name}");
    }
}

// Under the $500 exception the target is $0, so last year's return isn't
// asked for as a way to change it. Above it, it is.
#[test]
fn prior_return_asked_only_when_it_can_set_the_target() {
    let under = rules().requirement(&h(400).facts()).unwrap();
    assert!(under.value().exempt() && !has(&under.required(), PositionKind::PriorYearReturn));
    let over = rules().requirement(&h(4_000).facts()).unwrap();
    assert!(asks(&over.required(), PositionKind::PriorYearReturn).contains("your target could be"));
}

// Withholding over the tax reads as a refund, never as a minus amount.
#[test]
fn refund_reads_as_refund() {
    let rules = rules();
    let mut asked = false;
    for a in estimatedtest::ask(&rules, &estimatedtest::households(&HI)) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked = true;
        let res = a.requirement().unwrap();
        let why = res.value().why();
        assert!(
            why.contains("leaves a $600 refund. No estimated payments are needed"),
            "why {why:?}"
        );
        for x in estimatedtest::texts(Some(&*rules), std::slice::from_ref(&a)) {
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
