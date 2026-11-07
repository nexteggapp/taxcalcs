//! Massachusetts's estimated tax rules for 2026, checked against the
//! Department of Revenue's own documents and the General Laws. Ported from
//! estimated_test.go.
//!
//! Massachusetts publishes no worked example for estimated tax. The penalty
//! cases below are worked by hand from M-2210 lines 13 to 24. Go's
//! TestEstimatedFiguresAreInTheirSources and the extract checks at the top of
//! TestEstimatedWaiverOutcomes read testdata/ and are not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, Payment, Positions, PositionsBuilder,
    PriorReturn, Status, WaiverCause, WaiverKind, Weekday,
};
use filing::Status as FilingStatus;
use money::{DOLLAR, Rate, USCents, dollars, percent};
use taxcalcs_massachusettstaxcalc::{
    EstimatedFacts, EstimatedFactsBuilder, LateStart, estimated_tax, new_estimated_facts,
};
use working::{PositionKind, Reading, Required};

const MA: Jurisdiction = Jurisdiction::from_static("MA");

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

fn dues() -> [Date; 4] {
    [
        day(2026, 4, 15),
        day(2026, 6, 15),
        day(2026, 9, 15),
        day(2027, 1, 15),
    ]
}

/// One payment of each amount on each due date.
fn quarterly(a: &[USCents]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .map(|(i, x)| pay(dues()[i], *x))
        .collect()
}

#[derive(Clone, Default)]
struct Household {
    /// After credits, M-2210 line 3.
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
    /// M-2210 Part 3 line 22 for each period.
    annualised: Option<Vec<USCents>>,
    period_ends: Option<Vec<Date>>,
    waiver: Option<WaiverKind>,
    positions: Option<Positions>,
    farm: Rate,
    fish: Rate,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let fig = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(MA, 2026);
        b.filing_status(FilingStatus::Single)
            .this_year(fig)
            .paid(&self.payments)
            .requirement_first_met(self.first_met)
            .return_filed(self.return_filed)
            .farm_share(self.farm)
            .fish_share(self.fish);
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
        match self.extras {
            Some(e) => b.state(e),
            None => b.state(
                new_estimated_facts()
                    .prior_year_farmer_or_fisher(false)
                    .build()
                    .unwrap(),
            ),
        };
        let mut el = estimated::new_elections();
        if self.as_withheld {
            el.withholding_as_withheld();
        }
        if let Some(ann) = &self.annualised {
            el.annualised();
            let ends = self.period_ends.clone().unwrap_or_else(|| {
                vec![
                    day(2026, 3, 31),
                    day(2026, 5, 31),
                    day(2026, 8, 31),
                    day(2026, 12, 31),
                ]
            });
            for (i, t) in ann.iter().enumerate() {
                b.periods(&[estimated::new_period()
                    .end(ends[i])
                    .annualised_tax(*t)
                    .build()
                    .unwrap()]);
            }
        }
        b.elections(el.build().unwrap());
        if let Some(k) = self.waiver {
            b.positions(estimated::new_positions().waiver(k).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed twelve-month return by a full-year resident.
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

fn part_year(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .full_year_resident(false)
            .build()
            .unwrap(),
    )
}

fn extras(b: &mut EstimatedFactsBuilder) -> Option<EstimatedFacts> {
    Some(b.prior_year_farmer_or_fisher(false).build().unwrap())
}

/// The nth Monday of a month, or the last for n = -1.
fn nth_monday(year: i64, m: i64, n: i64) -> Date {
    if n < 0 {
        let mut d = estimated::date_of(year, Month(m + 1), 1).add_days(-1);
        while d.weekday() != Weekday::MONDAY {
            d = d.add_days(-1);
        }
        return d;
    }
    let mut d = day(year, m, 1);
    while d.weekday() != Weekday::MONDAY {
        d = d.add_days(1);
    }
    d.add_days(7 * (n - 1))
}

/// The 2026 dates are the ones Form 1-ES and the draft M-2210 print. None is
/// a weekend or a Massachusetts legal holiday under c. 4 § 7, so none moves.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let holidays = [
        nth_monday(2026, 4, 3),
        nth_monday(2026, 5, -1),
        day(2026, 6, 17),
        day(2026, 6, 19),
        day(2026, 7, 4),
        nth_monday(2026, 9, 1),
        nth_monday(2026, 10, 2),
        day(2026, 11, 11),
        day(2026, 12, 25),
        day(2027, 1, 1),
        nth_monday(2027, 1, 3),
    ];
    assert_eq!(holidays[0], day(2026, 4, 20));
    assert_eq!(holidays[1], day(2026, 5, 25));
    let res = rules().schedule(&tax(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
        assert!(!holidays.contains(&inst.due()));
    }
}

/// January 31, 2027 is a Sunday, and Massachusetts has no rule for it:
/// filing and paying by then does not excuse the January installment. The
/// penalty stops on the day the return is paid, and never runs past April
/// 15, 2027.
#[test]
fn estimated_no_january_relief() {
    let h = Household {
        return_filed: day(2027, 1, 31),
        ..tax(DOLLAR * 5_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    assert_eq!(sched.value()[3].status(), Status::Short);
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().as_of(), day(2027, 1, 31));
    let p = rules()
        .penalty(&tax(DOLLAR * 5_000).facts(), day(2027, 6, 30))
        .unwrap();
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
}

/// The form's $400 floor: $400.00 after credits and withholding owes no
/// penalty, $400.01 does.
#[test]
fn estimated_threshold_boundaries() {
    for (withheld, exempt) in [
        (USCents(60_000), true),
        (USCents(59_999), false),
        (USCents(60_001), true),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    withheld,
                    ..tax(DOLLAR * 1_000)
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {withheld}");
        if exempt {
            let c = res.value().chosen().unwrap();
            assert_eq!(c.basis(), Basis::Exception);
            assert_eq!(c.amount(), USCents(0));
            assert_eq!(c.label(), "Tax less withholding of $400 or less");
        }
        for d in res.divergences() {
            if d.rule() == "The $400 floor" {
                assert_eq!(d.affects_household(), withheld == USCents(60_000));
            }
        }
        assert!(res.value().exempt() || res.value().required());
    }
}

/// The target is the lesser of 80% of this year's tax and 100% of last
/// year's, with no 110% step at any income.
#[test]
fn estimated_target_options() {
    let res = rules()
        .requirement(
            &Household {
                prior: prior(USCents(799_999)),
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    let r = res.value();
    let c = r.chosen().unwrap();
    assert_eq!(c.basis(), Basis::PriorYearTax);
    assert_eq!(r.target(), USCents(799_999));
    assert_eq!(c.percent(), percent(100));
    let o = &r.options()[0];
    assert_eq!(o.percent(), percent(80));
    assert_eq!(o.amount(), DOLLAR * 8_000);
    let res = rules()
        .requirement(
            &Household {
                prior: prior(USCents(800_001)),
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().chosen().unwrap().basis(), Basis::ThisYearTax);
    assert_eq!(res.value().target(), DOLLAR * 8_000);

    // A $2,000,000 earner keeps 100%.
    let rich = estimated::new_prior_return()
        .filed(true)
        .full_year_resident(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 100_000)
                .federal_agi(DOLLAR * 2_000_000)
                .state_agi(DOLLAR * 2_000_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                prior: Some(rich),
                ..tax(DOLLAR * 200_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().target(), DOLLAR * 100_000);

    let not_filed = estimated::new_prior_return().build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .build()
        .unwrap();
    for (name, p, ask) in [
        ("no return on record", None, PositionKind::PriorYearReturn),
        ("not filed", Some(not_filed), PositionKind::Unspecified),
        ("eleven months", Some(short), PositionKind::Unspecified),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: p,
                    ..tax(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap();
        assert!(!res.value().options()[1].available(), "{name}");
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}: {ask} is not asked");
        }
    }
}

/// No source says whether a part-year return opens the prior-year test. It
/// is offered and asked about, and the caller can withdraw it.
#[test]
fn estimated_part_year_prior_return() {
    let res = rules()
        .requirement(
            &Household {
                prior: part_year(DOLLAR * 1_000),
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().options()[1].available());
    assert_eq!(res.value().target(), DOLLAR * 1_000);
    assert!(has(&res.required(), PositionKind::JurisdictionFact));
    let no = extras(new_estimated_facts().part_year_prior_return_counts(false));
    let res = rules()
        .requirement(
            &Household {
                prior: part_year(DOLLAR * 1_000),
                extras: no,
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(!res.value().options()[1].available());
    assert_eq!(res.value().target(), DOLLAR * 8_000);
    assert!(!has(&res.required(), PositionKind::JurisdictionFact));
}

/// § 14(d)(ii): no liability last year and residence all of it excuses the
/// penalty. Without the residence it is asked, and a part-year resident does
/// not get the exception.
#[test]
fn estimated_no_liability_last_year() {
    let res = rules()
        .requirement(
            &Household {
                prior: prior(USCents(0)),
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().exempt(), "{}", res.value().why());
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::Exception);
    assert_eq!(c.amount(), USCents(0));
    assert_eq!(c.label(), "No tax due last year as a full-year resident");
    let with_prior = |p: PriorReturn| Household {
        prior: Some(p),
        ..tax(DOLLAR * 10_000)
    };
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    assert!(
        rules()
            .requirement(&with_prior(not_required).facts())
            .unwrap()
            .value()
            .exempt()
    );
    let unanswered = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let res = rules()
        .requirement(&with_prior(unanswered).facts())
        .unwrap();
    assert!(!res.value().exempt());
    assert!(has(&res.required(), PositionKind::Residency));
    let moved = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(false)
        .build()
        .unwrap();
    let res = rules().requirement(&with_prior(moved).facts()).unwrap();
    assert!(!res.value().exempt());
    assert_eq!(res.value().target(), DOLLAR * 8_000);
    // The exception needs nothing about this year, so its zero is exact.
    let p = rules()
        .penalty(
            &Household {
                no_tax: true,
                prior: prior(USCents(0)),
                ..Household::default()
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert_eq!(p.value().total(), USCents(0));
    assert!(!p.value().upper_bound());
    assert_eq!(
        p.value().reason(),
        "No tax due last year as a full-year resident."
    );
}

/// M-2210 lines 16 to 23 by hand: 80% of $5,000 is $4,000, so $1,000 a
/// quarter, none paid. TIR 26-5's 7% for April to June and 8% after:
///
/// ```text
/// April 15:  76 days at 7% $14.58, 92 at 8% $20.16, 92 at 8% $20.16
/// June 15:   15 days at 7%  $2.88, 92 at 8% $20.16, 92 at 8% $20.16
/// September: 15 days at 8%  $3.29, 92 at 8% $20.16
/// ```
///
/// $121.55 in all. The days of 2027 have no published rate.
#[test]
fn estimated_penalty_across_rate_periods() {
    let f = tax(DOLLAR * 5_000).facts();
    let want: [&[i64]; 3] = [&[1_458, 2_016, 2_016], &[288, 2_016, 2_016], &[329, 2_016]];
    let want_days: [&[i64]; 3] = [&[76, 92, 92], &[15, 92, 92], &[15, 92]];
    let want_rates: [&[i32]; 3] = [&[700, 800, 800], &[700, 800, 800], &[800, 800]];
    for as_of in [day(2026, 12, 31), day(2027, 4, 15)] {
        let res = rules().penalty(&f, as_of).unwrap();
        let lines = res.value().lines();
        assert_eq!(lines.len(), 4);
        for i in 0..want.len() {
            let parts = lines[i].parts();
            assert_eq!(parts.len(), want[i].len(), "line {}", i + 1);
            for (j, p) in parts.iter().enumerate() {
                assert_eq!(
                    p.charge(),
                    USCents(want[i][j]),
                    "line {} part {}",
                    i + 1,
                    j + 1
                );
                assert_eq!(p.days(), want_days[i][j]);
                assert_eq!(p.rate(), Rate(want_rates[i][j]));
            }
        }
        assert_eq!(res.value().total(), USCents(12_155), "to {as_of}");
        assert_eq!(
            has(&res.required(), PositionKind::UnpublishedRate),
            as_of.year() == 2027
        );
        if as_of.year() == 2027 {
            assert_eq!(lines[0].days(), 365);
        }
    }
}

/// A June shortfall paid July 15 spans the change from 7% to 8%: 15 days at
/// 7% on $1,000 is $2.88, 15 days at 8% is $3.29. A payment pays the oldest
/// shortfall first.
#[test]
fn estimated_shortfall_spanning_two_rates() {
    let d = dues();
    let h = Household {
        payments: vec![
            pay(d[0], DOLLAR * 1_000),
            pay(day(2026, 7, 15), DOLLAR * 1_000),
            pay(d[2], DOLLAR * 1_000),
            pay(d[3], DOLLAR * 1_000),
        ],
        ..tax(DOLLAR * 5_000)
    };
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let lines = p.value().lines();
    assert_eq!(lines.len(), 1);
    assert_eq!(lines[0].installment(), 2);
    let parts = lines[0].parts();
    assert_eq!(parts.len(), 2);
    assert_eq!(parts[0].charge(), USCents(288));
    assert_eq!(parts[1].charge(), USCents(329));
    assert_eq!(parts[0].rate(), Rate(700));
    assert_eq!(parts[1].rate(), Rate(800));
    assert_eq!(p.value().total(), USCents(617));
}

/// Withholding counts in equal parts on the four dates, whenever it was
/// withheld, unless the filer shows the dates.
#[test]
fn estimated_withholding() {
    let mut h = Household {
        withheld: DOLLAR * 8_000,
        withheld_on: day(2026, 12, 1),
        ..tax(DOLLAR * 10_000)
    };
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));
    h.as_withheld = true;
    let sched = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(sched[0].status(), Status::Short);
    assert_eq!(sched[0].shortfall(), DOLLAR * 2_000);
}

/// Form 1-ES and AP 241 put a late starter on the remaining dates; the
/// statute keeps four quarters. The default is the department's, asked
/// about.
#[test]
fn estimated_late_start() {
    // 80% of $5,000 is $4,000. From May 1 it is thirds from June 15.
    let d = dues();
    let pays = vec![
        pay(d[1], USCents(133_333)),
        pay(d[2], USCents(133_334)),
        pay(d[3], USCents(133_333)),
    ];
    let mut h = Household {
        first_met: day(2026, 5, 1),
        payments: pays,
        ..tax(DOLLAR * 5_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [0, 133_333, 266_667, 400_000].iter().enumerate() {
        assert_eq!(sched.value()[i].needed_by(), USCents(*want));
    }
    assert_eq!(sched.value()[0].status(), Status::NothingDue);
    assert!(has(&sched.required(), PositionKind::JurisdictionFact));
    for d in sched.divergences() {
        if d.rule() == "A requirement first met after March 31" {
            assert!(d.affects_household() && d.uses() == Reading::Form);
        }
    }
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));

    // The statute's reading: April is $1,000 short until June 15's payment,
    // 61 days at 7%, $11.70.
    h.extras = extras(new_estimated_facts().late_start(LateStart::FourQuarters));
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let lines = p.value().lines();
    assert!(!lines.is_empty());
    assert_eq!(lines[0].total(), USCents(1_170));
    assert!(!has(&p.required(), PositionKind::JurisdictionFact));
    for d in p.divergences() {
        if d.rule() == "A requirement first met after March 31" {
            assert_eq!(d.uses(), Reading::Statute);
        }
    }

    // The groups: March 31 is the first, April 1 the second, June 1 the
    // third and September 1 the last.
    for (met, need) in [
        (day(2026, 3, 31), [100_000, 200_000, 300_000, 400_000]),
        (day(2026, 4, 1), [0, 133_333, 266_667, 400_000]),
        (day(2026, 5, 31), [0, 133_333, 266_667, 400_000]),
        (day(2026, 6, 1), [0, 0, 200_000, 400_000]),
        (day(2026, 8, 31), [0, 0, 200_000, 400_000]),
        (day(2026, 9, 1), [0, 0, 0, 400_000]),
    ] {
        let s = rules()
            .schedule(
                &Household {
                    first_met: met,
                    ..tax(DOLLAR * 5_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        for (i, want) in need.iter().enumerate() {
            assert_eq!(s[i].needed_by(), USCents(*want), "first met {met}");
        }
    }

    // Not told when the requirement arose: four quarters, and asked.
    let res = rules().schedule(&tax(DOLLAR * 5_000).facts()).unwrap();
    assert!(has(&res.required(), PositionKind::RequirementFirstMet));
}

/// Part 3: 20%, 40%, 60% and 80% of the annualised tax, less earlier
/// installments, never more than the regular quarter plus what was carried.
#[test]
fn estimated_annualised() {
    let mut h = Household {
        annualised: Some(vec![
            DOLLAR * 2_000,
            DOLLAR * 5_000,
            DOLLAR * 10_000,
            DOLLAR * 10_000,
        ]),
        payments: quarterly(&[DOLLAR * 400, DOLLAR * 1_600, DOLLAR * 4_000, DOLLAR * 2_000]),
        ..tax(DOLLAR * 10_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [400, 2_000, 6_000, 8_000].iter().enumerate() {
        assert_eq!(sched.value()[i].needed_by(), DOLLAR * *want);
    }
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));
    h.annualised = None;
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_ne!(p.value().total(), USCents(0));
    h.annualised = Some(vec![USCents(1), USCents(2), USCents(3), USCents(4)]);
    h.period_ends = Some(vec![
        day(2026, 3, 31),
        day(2026, 6, 30),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]);
    assert!(rules().schedule(&h.facts()).is_err());
}

/// The commissioner decides a waiver, so the penalty is shown before it.
#[test]
fn estimated_waiver() {
    let p = rules()
        .penalty(&tax(DOLLAR * 5_000).facts(), day(2027, 4, 15))
        .unwrap();
    assert!(has(&p.required(), PositionKind::PenaltyWaiver));
    let p = rules()
        .penalty(
            &Household {
                waiver: Some(WaiverKind::Casualty),
                ..tax(DOLLAR * 5_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert!(!has(&p.required(), PositionKind::PenaltyWaiver));
    assert_eq!(p.value().waiver(), WaiverKind::Casualty);
    assert_eq!(p.value().total(), USCents(12_155));
}

/// Massachusetts grants the federal pair: a casualty or disaster, and
/// retirement after 62 or disability in this year or last.
#[test]
fn estimated_waiver_outcomes() {
    let with = |c: WaiverCause, on: Date, born: Option<Date>| -> PositionsBuilder {
        let mut b = estimated::new_positions();
        b.waiver_for(c, on);
        if let Some(d) = born {
            b.born(d);
        }
        b
    };
    for (name, pos, state, want) in [
        (
            "disaster",
            with(WaiverCause::Disaster, day(2026, 8, 1), None),
            "eligible",
            "a disaster",
        ),
        (
            "retired in 2026 at 64",
            with(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1962, 3, 1)),
            ),
            "eligible",
            "retired in 2026 after reaching 62",
        ),
        (
            "retired in 2025 at 61",
            with(
                WaiverCause::Retirement,
                day(2025, 6, 1),
                Some(day(1964, 3, 1)),
            ),
            "refused",
            "You were 61",
        ),
        (
            "retired in 2020",
            with(
                WaiverCause::Retirement,
                day(2020, 6, 1),
                Some(day(1955, 3, 1)),
            ),
            "refused",
            "retiring in 2025 or 2026",
        ),
        (
            "retired, no birth date",
            with(WaiverCause::Retirement, day(2026, 6, 1), None),
            "undecided",
            "date of birth",
        ),
    ] {
        let p = rules()
            .penalty(
                &Household {
                    positions: Some(pos.build().unwrap()),
                    ..tax(DOLLAR * 5_000)
                }
                .facts(),
                day(2027, 4, 15),
            )
            .unwrap()
            .value();
        let o = p.waiver_outcome().unwrap();
        let got = [
            (o.eligible(), "eligible"),
            (o.refused(), "refused"),
            (o.undecided(), "undecided"),
        ]
        .iter()
        .filter(|x| x.0)
        .map(|x| x.1)
        .collect::<String>();
        assert_eq!(got, state, "{name}");
        assert!(o.reason().contains(want), "{name}: {}", o.reason());
        let waived = if state == "eligible" {
            USCents(0)
        } else {
            p.total()
        };
        assert_eq!(p.total(), USCents(12_155), "{name}");
        assert_eq!(p.if_waived(), waived, "{name}");
    }
}

/// When the $400 exception sets the target, last year's return can't change
/// it, so it isn't asked for.
#[test]
fn estimated_exempt_asks_nothing_of_the_target() {
    let res = rules().requirement(&tax(DOLLAR * 300).facts()).unwrap();
    assert!(res.value().exempt());
    assert!(!has(&res.required(), PositionKind::PriorYearReturn));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want = [
        (
            "The $400 floor",
            ["$400 or less", "Form M-2210", "under $400", "§ 14(d)(i)"],
        ),
        (
            "Daily compounding",
            [
                "simple interest",
                "lines 20 to 23",
                "without compounding",
                "c. 62C § 32(a)",
            ],
        ),
        (
            "A requirement first met after March 31",
            [
                "remaining dates",
                "AP 241 § 241.3",
                "four installments, each 25%",
                "§ 14(c)",
            ],
        ),
        (
            "The last day charged",
            [
                "before 4/15/27",
                "line",
                "15th day of the fourth month",
                "§ 14(b)",
            ],
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, w)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {}", d.rule());
        };
        // $10,000 of tax, nothing paid: short on every date, not at the $400
        // floor, and not a late start.
        let want_affects = d.rule() == "Daily compounding" || d.rule() == "The last day charged";
        assert_eq!(d.affects_household(), want_affects, "{}", d.rule());
        assert_eq!(d.uses(), Reading::Form);
        assert!(
            d.form().contains(w[0])
                && d.form_source().contains(w[1])
                && d.statute().contains(w[2])
                && d.statute_source().contains(w[3]),
            "{} does not pin both readings",
            d.rule()
        );
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for y in [2025, 2027] {
        assert!(
            estimated_tax(y)
                .err()
                .is_some_and(|e| e.is(&estimated::Error::NoRules))
        );
    }
    let farmer_last_year = Some(
        new_estimated_facts()
            .prior_year_farmer_or_fisher(true)
            .build()
            .unwrap(),
    );
    for (name, h) in [
        (
            "a farmer",
            Household {
                farm: Rate(7_000),
                ..tax(USCents(100))
            },
        ),
        (
            "a fisherman",
            Household {
                fish: Rate(7_000),
                ..tax(USCents(100))
            },
        ),
        (
            "farming and fishing",
            Household {
                farm: Rate(3_400),
                fish: Rate(3_400),
                ..tax(USCents(100))
            },
        ),
        (
            "a farmer on last year's",
            Household {
                extras: farmer_last_year,
                ..tax(USCents(100))
            },
        ),
    ] {
        assert!(
            rules()
                .requirement(&h.facts())
                .err()
                .is_some_and(|e| e.is(&estimated::Error::Unsupported)),
            "{name}"
        );
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let other = estimated::new_facts(MA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(this)
        .state("not Massachusetts's")
        .build()
        .unwrap();
    for (name, f) in [
        (
            "Pennsylvania's facts",
            estimated::new_facts(estimated::PENNSYLVANIA, 2026)
                .filing_status(FilingStatus::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(MA, 2027)
                .filing_status(FilingStatus::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        ("other extras", other.clone()),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert_eq!(
        rules().requirement(&other).err().unwrap().to_string(),
        "massachusettstaxcalc: the facts carry string, not massachusettstaxcalc.EstimatedFacts"
    );
}

/// Without this year's tax the target is 100% of last year's, and the
/// penalty against it is the most the penalty can be.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 4_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required());
    assert_eq!(r.target(), DOLLAR * 4_000);
    let o = &r.options()[0];
    assert!(!o.available());
    assert_eq!(o.why_not(), estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [1_000, 2_000, 3_000, 4_000].iter().enumerate() {
        assert_eq!(sched.value()[i].needed_by(), DOLLAR * *want);
    }
    let p = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
    assert!(p.value().upper_bound());
    assert_eq!(p.value().total(), USCents(12_155));

    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let res = rules().requirement(&none.facts()).unwrap();
    assert!(!res.value().target_known() && !res.value().required());
    assert!(
        rules()
            .schedule(&none.facts())
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NeedsThisYearTax))
    );
    assert!(
        rules()
            .penalty(&none.facts(), day(2027, 4, 15))
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NeedsThisYearTax))
    );
}

/// A typical filer is asked only what changes the figure.
#[test]
fn estimated_asks_only_what_matters() {
    let f = Household {
        prior: prior(DOLLAR * 9_000),
        first_met: day(2026, 1, 1),
        ..tax(DOLLAR * 10_000)
    }
    .facts();
    let res = rules().requirement(&f).unwrap();
    assert!(res.required().is_empty(), "asked {:?}", res.required());
    let bare = estimated::new_facts(MA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules().requirement(&bare).unwrap();
    assert_eq!(count(&res.required(), PositionKind::JurisdictionFact), 1);
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
        "$400",
        "80%",
        "100% of last year's",
        "no 110% tier",
        "20%, 40%, 60% and 80%",
        "January 31",
        "7%",
        "8%",
        "$1,107,750",
        "dates left",
        "age 62",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s}");
    }
}

/// The README's example. April's $250 runs 61 days at 7% to June 15, $2.92;
/// June's $500 runs 15 days at 7% and 77 at 8% to September 15, $1.44 and
/// $8.44; September's $750 runs 15 days and 92 days at 8% to the end of
/// 2026, $2.47 and $15.12. $30.39.
#[test]
fn estimated_readme_example() {
    let f = Household {
        withheld: DOLLAR * 4_000,
        prior: prior(DOLLAR * 9_000),
        payments: quarterly(&[DOLLAR * 1_000; 4]),
        ..tax(DOLLAR * 12_000)
    }
    .facts();
    assert_eq!(
        rules().requirement(&f).unwrap().value().target(),
        DOLLAR * 9_000
    );
    let sched = rules().schedule(&f).unwrap().value();
    for (i, want) in [250, 500, 750, 1_000].iter().enumerate() {
        assert_eq!(sched[i].shortfall(), DOLLAR * *want);
    }
    let p = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(3_039));
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
    let f = estimated::new_facts(MA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(figures(3_000, 60_000))
        .prior_year(prior)
        .withheld(&withheld)
        .build()
        .unwrap();
    let got = rules().requirement(&f).unwrap().value().why();
    assert!(got.contains("a $600 refund") && !got.contains("−$") && !got.contains("-$"));
}
