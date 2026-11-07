//! Louisiana's estimated tax rules for 2026, under Act 307 of 2026, checked
//! against the department's forms, the Act and R.S. 47:116 to 47:118. The
//! cases are hand-written and worked in their comments; the ledger follows
//! R-210R-i's own example. (The Go test comparing figures against the
//! extracts in testdata/ checks the Go package's documents, not code, and
//! is not ported.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, Payment, Positions, PriorReturn, Status,
    WaiverCause, WaiverKind, Weekday,
};
use louisianataxcalc::EstimatedFacts;
use money::{DOLLAR, Rate, USCents, dollars, percent};
use taxcalcs_louisianataxcalc as louisianataxcalc;
use working::{PositionKind, Reading, Required, Rule};

const LA: Jurisdiction = Jurisdiction::from_static("LA");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    louisianataxcalc::estimated_tax(2026).unwrap()
}

/// Whether a rule applied says s.
fn applied(rs: &[Rule], s: &str) -> bool {
    rs.iter().any(|r| r.applied().contains(s))
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
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

/// One payment of each amount, in dollars, on each due date, skipping
/// zeros.
fn quarterly(a: &[i64]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .filter(|(_, x)| **x > 0)
        .map(|(i, x)| pay(dues()[i], DOLLAR * *x))
        .collect()
}

#[derive(Clone, Default)]
struct Household {
    status: filing::Status,
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    /// The tax on the income earned to each period end, for Exception 5.
    period_tax: Option<Vec<USCents>>,
    as_withheld: bool,
    waiver: bool,
    positions: Option<Positions>,
}

fn taxed(tax: USCents) -> Household {
    Household {
        tax,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let mut status = self.status;
        if status == filing::Status::Unspecified {
            status = filing::Status::Single;
        }
        let mut fig = estimated::new_figures().tax(self.tax).build().unwrap();
        if self.no_tax {
            fig = estimated::new_figures().build().unwrap();
        }
        let mut first_met = self.first_met;
        if first_met.is_zero() {
            first_met = day(2026, 1, 1);
        }
        let mut b = estimated::new_facts(LA, 2026);
        b.filing_status(status)
            .this_year(fig)
            .paid(&self.payments)
            .requirement_first_met(first_met)
            .return_filed(self.return_filed);
        if self.withheld.0 > 0 {
            b.withheld(&[estimated::new_withheld()
                .on(day(2026, 12, 15))
                .amount(self.withheld)
                .build()
                .unwrap()]);
        }
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        if let Some(e) = self.extras {
            b.state(e);
        }
        let mut eb = estimated::new_elections();
        if let Some(pt) = &self.period_tax {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            for (i, tax) in pt.iter().enumerate() {
                b.periods(&[estimated::new_period()
                    .end(ends[i])
                    .annualised_tax(*tax)
                    .build()
                    .unwrap()]);
            }
            eb.annualised();
        }
        if self.as_withheld {
            eb.withholding_as_withheld();
        }
        b.elections(eb.build().unwrap());
        if self.waiver {
            b.positions(
                estimated::new_positions()
                    .waiver(WaiverKind::None)
                    .build()
                    .unwrap(),
            );
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed full-year resident return.
fn prior(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .full_year_resident(true)
            .build()
            .unwrap(),
    )
}

fn facts_tax(a: USCents) -> Option<EstimatedFacts> {
    Some(
        louisianataxcalc::new_estimated_facts()
            .prior_year_facts_tax(a)
            .build()
            .unwrap(),
    )
}

#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&taxed(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
}

/// Act 307 sets 70% of this year's tax. R-210R-i's 90% would give $9,000.
#[test]
fn estimated_this_years_share_is_seventy_percent() {
    let res = rules()
        .requirement(&taxed(DOLLAR * 10_000).facts())
        .unwrap();
    let o = &res.value().options()[0];
    assert!(
        o.basis() == Basis::ThisYearTax
            && o.percent() == percent(70)
            && o.amount() == DOLLAR * 7_000
    );
    assert_eq!(res.value().target(), DOLLAR * 7_000);
}

/// The required annual payment is the lesser of 70% of this year's tax and
/// 100% of last year's, one cent either side.
#[test]
fn estimated_required_annual_payment_is_the_lesser() {
    for (prior_tax, chosen) in [
        (700_001, Basis::ThisYearTax),
        (700_000, Basis::ThisYearTax),
        (699_999, Basis::PriorYearTax),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: prior(USCents(prior_tax)),
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap();
        let c = res.value().chosen().unwrap();
        assert!(
            c.basis() == chosen && res.value().target() == USCents(prior_tax.min(700_000)),
            "last year {prior_tax}"
        );
        assert_eq!(res.value().options()[1].percent(), percent(100));
    }
}

/// The threshold is $1,000 after withholding, or the department's $2,000 on
/// a joint return, and "not more than" excuses: one cent either side.
#[test]
fn estimated_threshold_boundaries() {
    let statute = louisianataxcalc::new_estimated_facts()
        .statute_joint_threshold(true)
        .build()
        .unwrap();
    let form = louisianataxcalc::new_estimated_facts()
        .statute_joint_threshold(false)
        .build()
        .unwrap();
    use filing::Status as S;
    for (status, extras, owed, exempt, asked) in [
        (S::Single, None, 100_000, true, false),
        (S::Single, None, 100_001, false, false),
        (S::HeadOfHousehold, None, 100_001, false, false),
        (S::MarriedFilingSeparately, None, 100_000, true, false),
        (S::MarriedFilingJointly, None, 200_000, true, true),
        (S::MarriedFilingJointly, None, 200_001, false, false),
        (S::MarriedFilingJointly, None, 100_000, true, false),
        (S::MarriedFilingJointly, Some(form), 200_000, true, false),
        (S::MarriedFilingJointly, Some(statute), 100_000, true, false),
        (
            S::MarriedFilingJointly,
            Some(statute),
            100_001,
            false,
            false,
        ),
    ] {
        let h = Household {
            status,
            tax: USCents(owed) + DOLLAR * 5_000,
            withheld: DOLLAR * 5_000,
            extras,
            ..Household::default()
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "{status:?} owing {owed}: {}",
            res.value().why()
        );
        if exempt {
            let c = res.value().chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && c.label().starts_with("Tax less withholding of $"),
                "{status:?} owing {owed}"
            );
        }
        assert_eq!(
            has(&res.required(), PositionKind::UnsettledRule),
            asked,
            "{status:?} owing {owed}"
        );
    }
}

/// Last year's tax needs a filed full-year resident return. A filed return
/// showing no tax gives a required annual payment of nothing.
#[test]
fn estimated_prior_year_option_availability() {
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let unanswered = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .build()
        .unwrap();
    let part_year = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .full_year_resident(false)
        .build()
        .unwrap();
    let none = PositionKind::Unspecified;
    for (name, p, ask) in [
        ("no return on record", None, PositionKind::PriorYearReturn),
        ("not filed", Some(not_filed), none),
        (
            "residence not given",
            Some(unanswered),
            PositionKind::Residency,
        ),
        ("part-year", Some(part_year), none),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: p,
                    extras: facts_tax(USCents(50)),
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap();
        let opts = res.value().options();
        assert!(!opts[1].available() && !opts[2].available(), "{name}");
        if ask != none {
            assert!(has(&res.required(), ask), "{name}: {ask} is not asked");
        }
    }

    let h = Household {
        prior: prior(USCents(0)),
        ..taxed(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().required() && res.value().target().0 == 0);
    let p = rules().penalty(&h.facts(), day(2027, 5, 15)).unwrap();
    assert_eq!(p.value().total().0, 0);
}

/// R-210R line 5 divides line 3 by the installments and rounds to the
/// nearest dollar. With nothing paid, April's shortfall is that amount.
#[test]
fn estimated_installments_are_whole_dollars() {
    for (line3, each) in [(100_200, 25_100), (100_198, 25_000)] {
        let res = rules()
            .schedule(
                &Household {
                    prior: prior(USCents(line3)),
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(
            res.value()[0].shortfall(),
            USCents(each),
            "line 3 of {line3}"
        );
    }
}

/// R-210R-i's example under line 7: $400 required each period, $300, $600,
/// $350 and nothing paid, leaving $100 short in April and $350 in January:
/// $2.01 and $13.82.
#[test]
fn estimated_ledger_matches_r210r_example() {
    let h = Household {
        prior: prior(DOLLAR * 1_600),
        payments: quarterly(&[300, 600, 350, 0]),
        waiver: true,
        ..taxed(DOLLAR * 3_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    let want = [DOLLAR * 100, USCents(0), USCents(0), DOLLAR * 350];
    for (i, inst) in sched.iter().enumerate() {
        assert_eq!(inst.shortfall(), want[i], "installment {}", i + 1);
    }
    let p = rules()
        .penalty(&h.facts(), day(2027, 12, 31))
        .unwrap()
        .value();
    let lines = p.lines();
    assert!(
        lines.len() == 2
            && lines[0].days() == 61
            && lines[0].total() == USCents(201)
            && lines[1].days() == 120
            && lines[1].total() == USCents(1_382)
    );
    assert_eq!(p.total(), USCents(1_583));
}

/// R-210R-i's line 18 example: 30 days late is a factor of 0.00987. On a
/// $1,000 shortfall that is $9.87, where 12% over 365 days gives $9.86.
#[test]
fn estimated_penalty_uses_the_printed_daily_factor() {
    let d = dues();
    let h = Household {
        prior: prior(DOLLAR * 16_000),
        payments: vec![
            pay(d[0], DOLLAR * 3_000),
            pay(day(2026, 5, 15), DOLLAR * 1_000),
            pay(d[1], DOLLAR * 4_000),
            pay(d[2], DOLLAR * 4_000),
            pay(d[3], DOLLAR * 4_000),
        ],
        ..taxed(DOLLAR * 30_000)
    };
    let p = rules()
        .penalty(&h.facts(), day(2027, 5, 15))
        .unwrap()
        .value();
    assert!(p.lines().len() == 1 && p.lines()[0].days() == 30 && p.total() == USCents(987));
}

/// Act 307 runs the penalty to May 15, 2027: 395 days on $1,750, $227.42.
#[test]
fn estimated_penalty_stops_on_may_fifteenth() {
    let p = rules()
        .penalty(&taxed(DOLLAR * 10_000).facts(), day(2027, 6, 30))
        .unwrap()
        .value();
    assert_eq!(p.as_of(), day(2027, 5, 15));
    let l = &p.lines()[0];
    assert!(l.days() == 395 && l.total() == USCents(22_742));
    assert_eq!(p.lines()[3].days(), 120);
    let p = rules()
        .penalty(
            &Household {
                return_filed: day(2027, 3, 1),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
            day(2027, 5, 15),
        )
        .unwrap();
    assert_eq!(p.value().as_of(), day(2027, 3, 1));
}

/// Louisiana's 12% is fixed, so there is no rate period boundary to cross:
/// a shortfall from September 15, 2026 paid February 1, 2027 is charged in
/// one part for 139 days, $80.03.
#[test]
fn estimated_span_across_the_year_end() {
    let d = dues();
    let h = Household {
        payments: vec![
            pay(d[0], DOLLAR * 1_750),
            pay(d[1], DOLLAR * 1_750),
            pay(day(2027, 2, 1), DOLLAR * 3_500),
        ],
        ..taxed(DOLLAR * 10_000)
    };
    let p = rules()
        .penalty(&h.facts(), day(2027, 5, 15))
        .unwrap()
        .value();
    let lines = p.lines();
    assert_eq!(lines.len(), 2);
    let parts = lines[0].parts();
    assert!(
        parts.len() == 1
            && parts[0].days() == 139
            && parts[0].charge() == USCents(8_003)
            && parts[0].rate() == percent(12)
    );
    assert!(lines[1].days() == 17 && lines[1].total() == USCents(979));
}

/// Last year's facts at this year's rate excuse an installment; they do not
/// lower it.
#[test]
fn estimated_facts_exception_excuses_but_does_not_lower() {
    let mut h = Household {
        prior: prior(DOLLAR * 16_000),
        extras: facts_tax(DOLLAR * 10_000),
        payments: quarterly(&[2_600, 2_600, 2_600, 2_600]),
        ..taxed(DOLLAR * 20_000)
    };
    let req = rules().requirement(&h.facts()).unwrap().value();
    let c = req.chosen().unwrap();
    assert!(c.basis() == Basis::PriorYearIncome && req.target() == DOLLAR * 10_000);
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, inst) in sched.value().iter().enumerate() {
        let n = i as i64 + 1;
        assert!(
            inst.status() == Status::Excused && inst.shortfall().0 != 0,
            "installment {n}"
        );
        assert_eq!(inst.needed_by(), DOLLAR * 3_500 * n);
        let want = DOLLAR * 2_500 * n;
        assert!(
            applied(&sched.applied(), &format!("which needs {want} by then")),
            "installment {n}"
        );
    }
    let p = rules().penalty(&h.facts(), day(2027, 5, 15)).unwrap();
    assert_eq!(p.value().total().0, 0);

    // April $2,400: $1,100 short against $3,500, made up June 15: $22.08.
    h.payments = quarterly(&[2_400, 2_600, 2_500, 2_500]);
    let p = rules()
        .penalty(&h.facts(), day(2027, 5, 15))
        .unwrap()
        .value();
    assert!(
        p.lines().len() == 1
            && p.lines()[0].shortfall() == DOLLAR * 1_100
            && p.total() == USCents(2_208)
    );

    // Without the figure the exception is not offered, and it is asked for.
    h.extras = None;
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().options()[2].available());
    assert!(has(&res.required(), PositionKind::JurisdictionFact));
}

/// Exception 2 needs a liability on a twelve-month return; here it excuses
/// nothing a payment did not already meet.
#[test]
fn estimated_exception_two_needs_the_whole_quarter() {
    let mut h = Household {
        prior: prior(DOLLAR * 16_000),
        payments: quarterly(&[3_999, 4_001, 4_000, 4_000]),
        ..taxed(DOLLAR * 30_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert!(s[0].status() == Status::Short && s[0].shortfall() == USCents(100));
    h.payments = quarterly(&[4_000, 4_000, 4_000, 4_000]);
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].status(), Status::Met);
}

/// Exception 5 is 70% of the tax on the income actually earned to each
/// period end, with no annualizing.
#[test]
fn estimated_exception_five_uses_income_to_date() {
    let mut h = Household {
        period_tax: Some(vec![
            DOLLAR * 1_000,
            DOLLAR * 3_000,
            DOLLAR * 8_000,
            DOLLAR * 20_000,
        ]),
        payments: quarterly(&[700, 1_400, 3_500, 8_400]),
        ..taxed(DOLLAR * 20_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    let want_needed = [
        DOLLAR * 3_500,
        DOLLAR * 7_000,
        DOLLAR * 10_500,
        DOLLAR * 14_000,
    ];
    let want_exception = [DOLLAR * 700, DOLLAR * 2_100, DOLLAR * 5_600];
    let want_status = [
        Status::Excused,
        Status::Excused,
        Status::Excused,
        Status::Met,
    ];
    for (i, inst) in sched.value().iter().enumerate() {
        assert!(
            inst.needed_by() == want_needed[i] && inst.status() == want_status[i],
            "installment {}",
            i + 1
        );
        if i < want_exception.len() {
            assert!(applied(
                &sched.applied(),
                &format!("which needs {} by then", want_exception[i])
            ));
        }
    }
    let p = rules().penalty(&h.facts(), day(2027, 5, 15)).unwrap();
    assert_eq!(p.value().total().0, 0);
    h.period_tax = None;
    let p = rules().penalty(&h.facts(), day(2027, 5, 15)).unwrap();
    assert_ne!(p.value().total().0, 0);
    // The periods must be R-210R's.
    let periods: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 6, 30),
        day(2026, 9, 30),
        day(2026, 12, 31),
    ]
    .into_iter()
    .map(|e| estimated::new_period().end(e).build().unwrap())
    .collect();
    let bad = estimated::new_facts(LA, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .periods(&periods)
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    assert!(rules().requirement(&bad).is_err());
}

/// A requirement first met later divides line 3 among the dates left, at
/// the boundaries R-210R prints.
#[test]
fn estimated_late_start() {
    for (met, want) in [
        (day(2026, 4, 1), [175_000, 350_000, 525_000, 700_000]),
        (day(2026, 4, 2), [0, 233_300, 466_600, 699_900]),
        (day(2026, 6, 1), [0, 233_300, 466_600, 699_900]),
        (day(2026, 6, 2), [0, 0, 350_000, 700_000]),
        (day(2026, 9, 1), [0, 0, 350_000, 700_000]),
        (day(2026, 9, 2), [0, 0, 0, 700_000]),
    ] {
        let s = rules()
            .schedule(
                &Household {
                    first_met: met,
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        for (i, inst) in s.iter().enumerate() {
            assert_eq!(
                inst.needed_by(),
                USCents(want[i]),
                "first met {met}: {}",
                i + 1
            );
            if want[i] == 0 {
                assert_eq!(
                    inst.status(),
                    Status::NothingDue,
                    "first met {met}: {}",
                    i + 1
                );
            }
        }
    }
    let f = estimated::new_facts(LA, 2026)
        .filing_status(filing::Status::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    assert!(has(&res.required(), PositionKind::RequirementFirstMet));
}

/// A return filed and paid by January 31 excuses the January installment.
/// January 31, 2027 is a Sunday; nothing says it moves.
#[test]
fn estimated_return_filed_by_january_thirty_first() {
    for (filed, excused) in [
        (day(2027, 1, 29), true),
        (day(2027, 1, 31), true),
        (day(2027, 2, 1), false),
    ] {
        let s = rules()
            .schedule(
                &Household {
                    return_filed: filed,
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        assert_eq!(s[3].status() == Status::Excused, excused, "filed {filed}");
        assert_eq!(s[0].status(), Status::Short, "filed {filed}");
    }
}

/// Withholding is an equal part on each date, or on its dates by election.
#[test]
fn estimated_withholding() {
    let mut h = Household {
        withheld: DOLLAR * 4_000,
        ..taxed(DOLLAR * 10_000)
    };
    let got = rules().schedule(&h.facts()).unwrap().value()[0].credited_by();
    assert_eq!(got, DOLLAR * 1_000);
    h.as_withheld = true;
    let got = rules().schedule(&h.facts()).unwrap().value()[0].credited_by();
    assert_eq!(got.0, 0);
}

/// The waiver is the secretary's to grant, so it is asked and not applied.
#[test]
fn estimated_waiver_is_asked() {
    let mut h = taxed(DOLLAR * 10_000);
    let res = rules().penalty(&h.facts(), day(2027, 5, 15)).unwrap();
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));
    h.waiver = true;
    let res = rules().penalty(&h.facts(), day(2027, 5, 15)).unwrap();
    assert!(!has(&res.required(), PositionKind::PenaltyWaiver));
}

/// Circumstances beyond your control are presumed good faith; retirement
/// and disability have no waiver, at any age or date.
#[test]
fn estimated_waiver_outcomes() {
    let pos = |c: WaiverCause, on: Date, born: Date| {
        let mut b = estimated::new_positions();
        b.waiver_for(c, on);
        if !born.is_zero() {
            b.born(born);
        }
        b.build().unwrap()
    };
    let none = Date::default();
    for (name, p, eligible, want) in [
        (
            "disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), none),
            true,
            "beyond your control",
        ),
        (
            "retired in 2026 at 64",
            pos(WaiverCause::Retirement, day(2026, 6, 1), day(1962, 3, 1)),
            false,
            "no waiver for retirement or disability",
        ),
        (
            "retired in 2020",
            pos(WaiverCause::Retirement, day(2020, 6, 1), day(1955, 3, 1)),
            false,
            "no waiver for retirement or disability",
        ),
        (
            "disabled in 2026",
            pos(WaiverCause::Disability, day(2026, 6, 1), none),
            false,
            "no waiver for retirement or disability",
        ),
    ] {
        let pen = rules()
            .penalty(
                &Household {
                    positions: Some(p),
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
                day(2027, 5, 15),
            )
            .unwrap()
            .value();
        let o = pen.waiver_outcome().unwrap();
        assert!(
            o.eligible() == eligible && o.refused() != eligible && o.reason().contains(want),
            "{name}: {:?}",
            o.reason()
        );
        assert_ne!(pen.total().0, 0, "{name}");
        let want_if = if eligible { USCents(0) } else { pen.total() };
        assert_eq!(pen.if_waived(), want_if, "{name}");
    }
}

/// An exception that sets the target leaves no question about last year's
/// return, whose answer can't change it.
#[test]
fn estimated_exempt_asks_nothing_of_the_target() {
    let res = rules().requirement(&taxed(DOLLAR * 500).facts()).unwrap();
    assert!(res.value().exempt() && !has(&res.required(), PositionKind::PriorYearReturn));
}

/// Each divergence is reported with both readings and both sources. The
/// four that name a document predating Act 307 follow the Act.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules()
        .penalty(&taxed(DOLLAR * 10_000).facts(), day(2027, 5, 15))
        .unwrap();
    let want = [
        ("This year's share of the tax", "90%", "70%", "Act 307"),
        (
            "When the penalty stops",
            "April 15",
            "May 15, 2027",
            "Act 307",
        ),
        (
            "The annualized income exception",
            "Exception 4",
            "repealed",
            "Act 307",
        ),
        (
            "Last year's figures at this year's rate",
            "filing status",
            "standard deduction",
            "Act 307",
        ),
        (
            "The threshold for a joint return",
            "$2,000",
            "$1,000",
            "47:116(A)",
        ),
        (
            "When withholding counts as paid",
            "number of months",
            "equal part",
            "47:118(E)(2)",
        ),
        (
            "Last year's tests after a part-year return",
            "part-year",
            "no condition",
            "47:118(B)(2)(b)(ii)",
        ),
        (
            "Needing to pay only after April 1",
            "1/3, 1/2",
            "25%",
            "47:118(B)(2)(a)",
        ),
        (
            "Which shortfall a payment pays first",
            "carried to the next",
            "exceeds that date's installment",
            "47:118(C)(2)",
        ),
        ("The daily rate", "0.000329", "0.000328767", "47:118(A)"),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some(w) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(
            d.form().contains(w.1)
                && d.statute().contains(w.2)
                && d.statute_source().contains(w.3)
                && !d.form_source().is_empty(),
            "{:?} does not pin both readings",
            d.rule()
        );
        if d.statute_source().contains("Act 307") {
            for s in ["out of date", "predate"] {
                assert!(d.form().contains(s), "{:?} does not say {s}", d.rule());
            }
            for s in [
                "May 22, 2026",
                "January 1, 2026",
                "1/26",
                "February 27, 2025",
                "R.S. 47:118 page",
            ] {
                assert!(
                    d.form_source().contains(s),
                    "{:?} does not cite {s:?}",
                    d.rule()
                );
            }
            assert_eq!(d.uses(), Reading::Statute, "{:?}", d.rule());
        }
    }

    // $10,000 of tax, nothing paid and no return on record: the 70% share,
    // the stop date and the daily rate change the figures, and nothing else.
    for d in res.divergences() {
        let want = d.rule() == "This year's share of the tax"
            || d.rule() == "When the penalty stops"
            || d.rule() == "The daily rate";
        assert_eq!(d.affects_household(), want, "{:?}", d.rule());
    }
    // A joint return between $1,000 and $2,000 is affected by the joint
    // threshold, and held to the statute when the position says so.
    let statute = louisianataxcalc::new_estimated_facts()
        .statute_joint_threshold(true)
        .build()
        .unwrap();
    for (extras, uses) in [(None, Reading::Form), (Some(statute), Reading::Statute)] {
        let h = Household {
            status: filing::Status::MarriedFilingJointly,
            withheld: DOLLAR * 5_000,
            extras,
            ..taxed(DOLLAR * 6_500)
        };
        for d in rules().requirement(&h.facts()).unwrap().divergences() {
            if d.rule() == "The threshold for a joint return" {
                assert!(d.affects_household() && d.uses() == uses);
            }
        }
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for y in [2025, 2027] {
        let Err(err) = louisianataxcalc::estimated_tax(y) else {
            panic!("{y}: rules returned");
        };
        assert!(err.is(&estimated::Error::NoRules), "{y}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    for (name, fish) in [("a farmer", false), ("a fisherman", true)] {
        let mut b = estimated::new_facts(LA, 2026);
        b.filing_status(filing::Status::Single).this_year(this);
        if fish {
            b.fish_share(Rate(7000));
        } else {
            b.farm_share(Rate(7000));
        }
        let Err(err) = rules().requirement(&b.build().unwrap()) else {
            panic!("{name}: accepted");
        };
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    for (name, f) in [
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
            estimated::new_facts(LA, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(LA, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("not Louisiana's")
                .build()
                .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        louisianataxcalc::new_estimated_facts()
            .prior_year_facts_tax(USCents(-1))
            .build()
            .is_err()
    );
}

/// Without this year's tax the target is last year's tax, or last year's
/// facts when smaller, and the penalty against last year's tax is an upper
/// bound. With no return to use there is no target.
#[test]
fn estimated_without_this_years_tax() {
    let mut h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 8_000, "{}", r.why());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let p = rules()
        .penalty(&h.facts(), day(2027, 5, 15))
        .unwrap()
        .value();
    assert!(p.upper_bound() && p.total().0 != 0);

    h.extras = facts_tax(DOLLAR * 6_000);
    h.payments = quarterly(&[1_500, 1_500, 1_500, 1_500]);
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert_eq!(r.target(), DOLLAR * 6_000);
    let p = rules()
        .penalty(&h.facts(), day(2027, 5, 15))
        .unwrap()
        .value();
    assert_eq!(p.total().0, 0);

    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    assert!(
        !rules()
            .requirement(&none.facts())
            .unwrap()
            .value()
            .target_known()
    );
    let Err(err) = rules().schedule(&none.facts()) else {
        panic!("schedule accepted");
    };
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
    let Err(err) = rules().penalty(&none.facts(), day(2027, 5, 15)) else {
        panic!("penalty accepted");
    };
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
}

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
        "$1,000", "$2,000", "70%", "Act 307", "May 15", "12%", "no 110%", "R-20128",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
}

/// Withholding over the tax leaves a refund, never a negative balance.
#[test]
fn withholding_over_the_tax_reads_as_a_refund() {
    let withheld: Vec<_> = (1..=12)
        .map(|m| {
            estimated::new_withheld()
                .on(day(2026, m, 28))
                .amount(dollars(300))
                .build()
                .unwrap()
        })
        .collect();
    let figures = |tax: i64, agi: i64| {
        estimated::new_figures()
            .tax(dollars(tax))
            .federal_agi(dollars(agi))
            .state_agi(dollars(agi))
            .build()
            .unwrap()
    };
    let prior = estimated::new_prior_return()
        .filed(true)
        .months(12)
        .full_year_resident(true)
        .figures(figures(2_800, 55_000))
        .build()
        .unwrap();
    let f = estimated::new_facts(LA, 2026)
        .filing_status(filing::Status::Single)
        .this_year(figures(3_000, 60_000))
        .prior_year(prior)
        .withheld(&withheld)
        .build()
        .unwrap();
    let got = rules().requirement(&f).unwrap().value().why();
    assert!(
        got.contains("a $600 refund") && !got.contains("−$") && !got.contains("-$"),
        "why = {got:?}"
    );
}
