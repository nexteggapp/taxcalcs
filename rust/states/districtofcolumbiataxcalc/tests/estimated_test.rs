//! The District of Columbia's estimated tax rules for 2026. Ported from
//! estimated_test.go and estimated_refund_test.go.
//!
//! Go's TestEstimatedFiguresAreInTheirSources and
//! TestEstimatedStatuteUnchangedForTwentyTwentySix, and the extract check at
//! the top of TestEstimatedWaiverOutcomes, read testdata/ and are not
//! ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, Payment, Positions, PriorReturn, Status,
    WaiverCause, WaiverKind, Weekday,
};
use filing::Status as FilingStatus;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_districtofcolumbiataxcalc::estimated_tax;
use working::{PositionKind, Reading, Required, Rule};

const DC: Jurisdiction = Jurisdiction::from_static("DC");

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
        .filter(|r| r.position() == k && r.asked())
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

fn dues() -> [Date; 4] {
    [
        day(2026, 4, 15),
        day(2026, 6, 15),
        day(2026, 9, 15),
        day(2027, 1, 15),
    ]
}

fn quarterly(a: &[USCents]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .map(|(i, x)| pay(dues()[i], *x))
        .collect()
}

#[derive(Clone, Default)]
struct Household {
    /// D-40 line 24 less line 30.
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    withheld_on: Date,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    return_filed: Date,
    first_met: Date,
    /// Income to each period end.
    periods: Option<Vec<USCents>>,
    as_withheld: bool,
    waiver: Option<WaiverKind>,
    farm_share: Rate,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

fn forty() -> Household {
    tax(DOLLAR * 40_000)
}

impl Household {
    fn facts(&self) -> Facts {
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        let mut b = estimated::new_facts(DC, 2026);
        b.filing_status(FilingStatus::Single)
            .this_year(fb.build().unwrap())
            .paid(&self.payments)
            .return_filed(self.return_filed)
            .requirement_first_met(self.first_met)
            .farm_share(self.farm_share);
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
        let mut eb = estimated::new_elections();
        if let Some(ps) = &self.periods {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            for (i, inc) in ps.iter().enumerate() {
                b.periods(&[estimated::new_period()
                    .end(ends[i])
                    .income(*inc)
                    .build()
                    .unwrap()]);
            }
            eb.annualised();
        }
        if self.as_withheld {
            eb.withholding_as_withheld();
        }
        b.elections(eb.build().unwrap());
        if let Some(k) = self.waiver {
            b.positions(estimated::new_positions().waiver(k).build().unwrap());
        }
        b.build().unwrap()
    }
}

/// A filed twelve-month DC return by a full-year resident.
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

#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&forty().facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    assert!(applied(&res.applied()).contains("no due date this year is a Saturday or Sunday"));
    for (filed, excused) in [(day(2027, 1, 15), true), (day(2027, 1, 16), false)] {
        let res = rules()
            .schedule(
                &Household {
                    return_filed: filed,
                    ..forty()
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(
            res.value()[3].status() == Status::Excused,
            excused,
            "filed {filed}"
        );
        assert_eq!(res.value()[0].status(), Status::Short, "filed {filed}");
    }
}

/// $500 of tax: $400.01 withheld leaves $99.99, exempt; $400 leaves $100,
/// required, and $3 of interest.
#[test]
fn estimated_threshold_boundary() {
    for (withheld, exempt, interest) in [
        (USCents(40_001), true, USCents(0)),
        (USCents(40_000), false, DOLLAR * 3),
    ] {
        let h = Household {
            withheld,
            ..tax(DOLLAR * 500)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {withheld}");
        assert_ne!(res.value().required(), exempt, "withheld {withheld}");
        let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        assert_eq!(p.value().total(), interest, "withheld {withheld}");
    }
}

/// D-2210 excuses a balance under 10% of the year's tax, whenever it was
/// paid.
#[test]
fn estimated_ten_percent_boundary() {
    for (paid, excused) in [(USCents(900_000), false), (USCents(900_001), true)] {
        let h = Household {
            payments: vec![pay(dues()[3], paid)],
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        assert_eq!(res.value().total().0 == 0, excused, "paid {paid}");
        assert_eq!(
            because(&res.required(), PositionKind::UnsettledRule)
                .contains("less than 10% of the year's tax"),
            excused,
            "paid {paid}"
        );
        let s = rules().schedule(&h.facts()).unwrap().value();
        assert_eq!(
            s[0].status() == Status::Excused && s[0].shortfall().0 > 0,
            excused,
            "paid {paid}"
        );
    }
    // Withholding alone that leaves under 10% makes the year exempt, and the
    // exception is the chosen option.
    let res = rules()
        .requirement(
            &Household {
                withheld: USCents(900_001),
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().exempt(), "{}", res.value().why());
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::Exception);
    assert_eq!(c.amount(), USCents(0));
    assert_eq!(c.label(), "Tax less withholding under $1,000");
    for d in res.divergences() {
        assert_eq!(
            d.affects_household(),
            d.rule() == "A balance under 10% of the year's tax",
            "{}",
            d.rule()
        );
    }
}

/// The target is the smaller of 90% of this year's tax and 110% of last
/// year's, with no 100% tier and no income test. The 110% test needs a
/// filed DC return by someone who lived in DC all of last year.
#[test]
fn estimated_target_options() {
    let res = rules()
        .requirement(
            &Household {
                prior: prior(DOLLAR * 15_000),
                ..tax(DOLLAR * 20_000)
            }
            .facts(),
        )
        .unwrap();
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::PriorYearTax);
    assert_eq!(res.value().target(), DOLLAR * 16_500);
    assert_eq!(c.percent(), percent(110));
    let res = rules()
        .requirement(
            &Household {
                prior: prior(DOLLAR * 17_000),
                ..tax(DOLLAR * 20_000)
            }
            .facts(),
        )
        .unwrap();
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::ThisYearTax);
    assert_eq!(res.value().target(), DOLLAR * 18_000);
    assert_eq!(c.percent(), percent(90));
    // A high income keeps 110%, not more.
    let f = estimated::new_figures()
        .tax(DOLLAR * 100_000)
        .federal_agi(DOLLAR * 2_000_000)
        .build()
        .unwrap();
    let rich = estimated::new_prior_return()
        .filed(true)
        .figures(f)
        .full_year_resident(true)
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
    let o = &res.value().options()[1];
    assert_eq!(o.percent(), percent(110));
    assert_eq!(o.amount(), DOLLAR * 110_000);

    let t = estimated::new_figures()
        .tax(DOLLAR * 5_000)
        .build()
        .unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(t)
        .full_year_resident(true)
        .build()
        .unwrap();
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let unanswered = estimated::new_prior_return()
        .filed(true)
        .figures(t)
        .build()
        .unwrap();
    let part_year = estimated::new_prior_return()
        .filed(true)
        .figures(t)
        .full_year_resident(false)
        .build()
        .unwrap();
    for (name, p, ask, rule) in [
        (
            "no return on record",
            None,
            PositionKind::PriorYearReturn,
            "",
        ),
        ("not filed", Some(not_filed), PositionKind::Unspecified, ""),
        (
            "not required",
            Some(not_required),
            PositionKind::Unspecified,
            "",
        ),
        (
            "a six-month year",
            Some(short),
            PositionKind::Unspecified,
            "",
        ),
        (
            "residence not given",
            Some(unanswered),
            PositionKind::Residency,
            "",
        ),
        (
            "a part-year return",
            Some(part_year),
            PositionKind::Unspecified,
            "would allow 110% of the $5000.00 tax on it, $5500.00",
        ),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: p,
                    ..forty()
                }
                .facts(),
            )
            .unwrap();
        assert!(!res.value().options()[1].available(), "{name}");
        assert_eq!(res.value().target(), DOLLAR * 36_000, "{name}");
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}");
        }
        if name == "a part-year return" {
            let o = &res.value().options()[1];
            assert_eq!(o.amount(), USCents(0));
            assert_eq!(o.why_not(), "You didn't live in DC all of 2025");
        }
        if !rule.is_empty() {
            assert!(applied(&res.applied()).contains(rule), "{name}");
        }
    }
}

/// $40,000 of tax, nothing withheld or paid, no return last year: $2,515 at
/// the printed factors, in whole dollars.
#[test]
fn estimated_interest_at_the_printed_factors() {
    let res = rules().penalty(&forty().facts(), day(2027, 4, 15)).unwrap();
    let p = res.value();
    let want = [
        (DOLLAR * 9_000, 61, DOLLAR * 158),
        (DOLLAR * 18_000, 92, DOLLAR * 477),
        (DOLLAR * 27_000, 122, DOLLAR * 948),
        (DOLLAR * 36_000, 90, DOLLAR * 932),
    ];
    let lines = p.lines();
    assert_eq!(lines.len(), 4);
    for (i, l) in lines.iter().enumerate() {
        let parts = l.parts();
        assert_eq!(l.shortfall(), want[i].0, "line {}", i + 1);
        assert_eq!(parts.len(), 1, "line {}", i + 1);
        assert_eq!(parts[0].days(), want[i].1);
        assert_eq!(parts[0].charge(), want[i].2);
        assert_eq!(parts[0].rate(), percent(10));
    }
    assert_eq!(p.total(), DOLLAR * 2_515);
    assert_eq!(p.as_of(), day(2027, 4, 15));
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));
    // Figured past April 15, 2027, it stops there.
    let p = rules()
        .penalty(&forty().facts(), day(2027, 6, 30))
        .unwrap()
        .value();
    assert_eq!(p.as_of(), day(2027, 4, 15));
    assert_eq!(p.total(), DOLLAR * 2_515);

    // A payment during a span counts only from the next due date.
    let h = Household {
        payments: vec![pay(day(2026, 5, 1), DOLLAR * 9_000)],
        ..forty()
    };
    let l = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value()
        .lines();
    assert_eq!(l[0].total(), DOLLAR * 158);
    assert_eq!(l[1].shortfall(), DOLLAR * 9_000);
    assert_eq!(l[1].total(), DOLLAR * 239);

    // Figured to May 1, 2026, April's span is 16 of 61 days in.
    let res = rules().penalty(&forty().facts(), day(2026, 5, 1)).unwrap();
    assert_eq!(res.value().total(), DOLLAR * 41);
    assert!(applied(&res.applied()).contains("16 of the 61 days"));
    // Nothing before the first due date.
    let p = rules()
        .penalty(&forty().facts(), day(2026, 4, 15))
        .unwrap()
        .value();
    assert_eq!(p.total(), USCents(0));
}

/// The statute charges 10% compounded daily; the form charges a printed
/// factor for each whole span.
#[test]
fn estimated_interest_diverges_from_the_statutes_compounding() {
    let statute = estimated::new_rate_series()
        .period(
            day(2026, 1, 1),
            day(2027, 12, 31),
            percent(10),
            "D.C. Code § 47-4201(d)(2)",
        )
        .build()
        .unwrap();
    let compound = |from: Date, to: Date| -> USCents {
        let (parts, gap) = estimated::compound_daily(DOLLAR * 10_000, from, to, &statute).unwrap();
        assert!(gap.is_zero());
        parts.iter().map(|p| p.charge()).sum()
    };
    let d = dues();
    let h = Household {
        prior: prior(USCents(3_636_364)),
        payments: vec![
            pay(day(2026, 5, 1), DOLLAR * 10_000),
            pay(d[1], DOLLAR * 10_000),
            pay(d[2], DOLLAR * 10_000),
            pay(d[3], DOLLAR * 10_000),
        ],
        ..tax(DOLLAR * 100_000)
    };
    let form = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert_eq!(form.total(), DOLLAR * 175);
    assert_eq!(form.lines().len(), 1);
    assert_eq!(compound(d[0], day(2026, 5, 31)), USCents(12_681));

    let h = Household {
        prior: prior(USCents(3_636_364)),
        payments: vec![
            pay(d[1], DOLLAR * 10_000),
            pay(d[2], DOLLAR * 10_000),
            pay(d[3], DOLLAR * 10_000),
        ],
        ..tax(DOLLAR * 100_000)
    };
    let form = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert_eq!(form.total(), DOLLAR * 1_050);
    assert_eq!(compound(d[0], day(2027, 4, 15)), USCents(105_156));

    let res = rules().requirement(&h.facts()).unwrap();
    let ds = res.divergences();
    let Some(d) = ds.iter().find(|d| d.rule() == "The interest factors") else {
        panic!("the factor divergence is not reported");
    };
    assert!(d.form().contains(".0175, .0265, .0351 and .0259"));
    assert!(d.statute().contains("compounded daily"));
    assert!(d.statute().contains("end of that month"));
}

/// § 47-4203(d)(3): withholding is paid in equal parts on the due dates,
/// unless the filer establishes when all of it was withheld. Last year's
/// overpayment applied forward counts toward April.
#[test]
fn estimated_withholding() {
    let mut h = Household {
        withheld: DOLLAR * 8_000,
        ..forty()
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.credited_by(), DOLLAR * 2_000 * (i as i64 + 1));
    }
    h.as_withheld = true;
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [0, 0, 0, 8_000].iter().enumerate() {
        assert_eq!(s[i].credited_by(), DOLLAR * *want);
    }
    let h = Household {
        payments: vec![
            estimated::new_payment()
                .amount(DOLLAR * 9_000)
                .from_prior_year()
                .build()
                .unwrap(),
        ],
        ..forty()
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].status(), Status::Met);
}

/// D-2210's annualised method prorates the required annual payment by the
/// income to each period's end.
#[test]
fn estimated_annualised_proration() {
    let mut h = Household {
        periods: Some(vec![
            DOLLAR * 10_000,
            DOLLAR * 20_000,
            DOLLAR * 60_000,
            DOLLAR * 100_000,
        ]),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [90_000, 180_000, 540_000, 900_000].iter().enumerate() {
        assert_eq!(res.value()[i].needed_by(), USCents(*want));
    }
    assert!(
        because(&res.required(), PositionKind::UnsettledRule).contains("22.5%, 45%, 67.5% and 90%")
    );
    // No income for the year: the regular quarters.
    h.periods = Some(vec![USCents(0); 4]);
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].needed_by(), USCents(225_000));
    // The periods must end where the statute's months do.
    let period = |d: Date| estimated::new_period().end(d).build().unwrap();
    let f = estimated::new_facts(DC, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .periods(&[
            period(day(2026, 3, 31)),
            period(day(2026, 6, 15)),
            period(day(2026, 8, 31)),
            period(day(2026, 12, 31)),
        ])
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    assert!(rules().schedule(&f).is_err());
}

/// The exceptions and the dates that change the figure.
#[test]
fn estimated_exceptions() {
    let with_prior = |p: PriorReturn| Household {
        prior: Some(p),
        ..forty()
    };
    let zero = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let res = rules().requirement(&with_prior(zero).facts()).unwrap();
    assert!(res.value().exempt(), "{}", res.value().why());
    let away = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(false)
        .build()
        .unwrap();
    let pen = rules()
        .penalty(&with_prior(away).facts(), day(2027, 4, 15))
        .unwrap();
    assert_ne!(pen.value().total(), USCents(0));
    assert!(applied(&pen.applied()).contains("§ 47-4203(e)(2): you had no DC liability for 2025"));
    let unsaid = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let res = rules().requirement(&with_prior(unsaid).facts()).unwrap();
    assert!(!res.value().exempt());
    assert!(because(&res.required(), PositionKind::Residency).contains("no DC tax that year"));

    // Under $100 once payments are counted, though paid late.
    let mut h = Household {
        payments: vec![pay(dues()[3], DOLLAR * 810)],
        ..tax(DOLLAR * 900)
    };
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));
    assert!(applied(&p.applied()).contains("leaves $90.00, under $100.00"));
    // Paid after January 15, it is not an estimated payment.
    h.payments = vec![pay(day(2027, 2, 1), DOLLAR * 810)];
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_ne!(p.value().total(), USCents(0));

    // A return filed and paid by January 15 takes the place of the fourth
    // payment; the earlier columns are still charged.
    let p = rules()
        .penalty(
            &Household {
                return_filed: day(2027, 1, 12),
                ..forty()
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert_eq!(p.value().total(), DOLLAR * 1_583);
    assert_eq!(p.value().lines().len(), 3);
    // Filed later, the fourth factor is charged in full whatever the day.
    let p = rules()
        .penalty(
            &Household {
                return_filed: day(2027, 2, 1),
                ..forty()
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert_eq!(p.value().total(), DOLLAR * 2_515);
    assert!(applied(&p.applied()).contains("whatever day the balance was paid"));

    // A waiver asked for is not subtracted.
    let p = rules()
        .penalty(
            &Household {
                waiver: Some(WaiverKind::Casualty),
                ..forty()
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert_eq!(p.value().total(), DOLLAR * 2_515);
    assert_eq!(p.value().waiver(), WaiverKind::Casualty);
    assert!(!has(&p.required(), PositionKind::PenaltyWaiver));
    assert!(applied(&p.applied()).contains("the Mayor decides it"));

    // Farmers and fishermen are under the general rules.
    let res = rules()
        .requirement(
            &Household {
                farm_share: Rate(8_000),
                ..forty()
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().target(), DOLLAR * 36_000);
    assert!(
        applied(&res.applied())
            .contains("separate rule for farmers or fishermen, so the general rules apply")
    );
}

/// § 47-1812.08(i)(4) moves the first declaration for a requirement first
/// met after April 1, June 1 or September 1.
#[test]
fn estimated_late_start() {
    for (met, date) in [
        (day(2026, 4, 1), ""),
        (day(2026, 4, 2), "due Jun 15."),
        (day(2026, 6, 1), "due Jun 15."),
        (day(2026, 6, 2), "due Sep 15."),
        (day(2026, 9, 1), "due Sep 15."),
        (day(2026, 9, 2), "due Jan 15, 2027."),
    ] {
        let res = rules()
            .schedule(
                &Household {
                    first_met: met,
                    ..forty()
                }
                .facts(),
            )
            .unwrap();
        let got = because(&res.required(), PositionKind::UnsettledRule);
        assert_eq!(date.is_empty(), got.is_empty(), "first met {met}: {got}");
        assert!(got.contains(date), "first met {met}: {got}");
        assert_eq!(res.value()[0].needed_by(), DOLLAR * 9_000);
    }
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&forty().facts()).unwrap();
    let want = [
        (
            "Who must make estimated payments",
            [
                "$100 or more",
                "D-40ES",
                "more than $100",
                "§ 47-1812.08(i)(1)",
            ],
        ),
        (
            "Tax under $100",
            [
                "no condition that the payments were on time",
                "item A",
                "timely made",
                "§ 47-4203(e)(1)",
            ],
        ),
        (
            "The prior-year test",
            [
                "lived in DC all of last year",
                "lines 4 and 5",
                "consisting of 12 months, with no condition of residence",
                "§ 47-4203(b)(1)(B)(ii)(II)",
            ],
        ),
        (
            "No liability last year",
            [
                "entire 12 months",
                "exceptions",
                "no condition of residence",
                "§ 47-4203(e)(2)",
            ],
        ),
        (
            "A balance under 10% of the year's tax",
            [
                "less than 10%",
                "exceptions",
                "no such exception",
                "§ 47-4203(e)",
            ],
        ),
        (
            "The 110% item in the exception list",
            [
                "without saying by when",
                "lines 6 to 11",
                "25% of the lesser",
                "(c)(2)(A)",
            ],
        ),
        (
            "The annualised income method",
            [
                "proration",
                "line 7",
                "22.5%, 45%, 67.5% and 90%",
                "§ 47-4203(b)(2)",
            ],
        ),
        (
            "The interest factors",
            [
                ".0175, .0265, .0351 and .0259",
                "lines 8 to 11",
                "compounded daily",
                "§§ 47-4201(d)(2), 47-4203(c)",
            ],
        ),
        (
            "A requirement first met after Apr 1",
            [
                "each of the four dates",
                "lines 6 and 7",
                "Jun 15, Sep 15 or Jan 15",
                "47-1812.08(i)(4)",
            ],
        ),
        (
            "Retirement, disability, casualty and death",
            [
                "no waiver",
                "exceptions",
                "retired after 62",
                "§ 47-4203(e)(3) to (5)",
            ],
        ),
        (
            "Due dates on a weekend or holiday",
            [
                "legal holiday",
                "D-40ES",
                "no rule for a weekend or holiday",
                "§ 47-1812.08(i)(4)",
            ],
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, w)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {}", d.rule());
        };
        assert!(
            d.form().contains(w[0])
                && d.form_source().contains(w[1])
                && d.statute().contains(w[2])
                && d.statute_source().contains(w[3]),
            "{} does not pin both readings",
            d.rule()
        );
        let want_uses = if d.rule() == "The 110% item in the exception list" {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert_eq!(d.uses(), want_uses, "{}", d.rule());
        assert_eq!(
            d.affects_household(),
            d.rule() == "The interest factors",
            "{}",
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
    let this = estimated::new_figures()
        .tax(DOLLAR * 40_000)
        .build()
        .unwrap();
    let other = estimated::new_facts(DC, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(this)
        .state("not the District's")
        .build()
        .unwrap();
    for (name, f) in [
        (
            "California's facts",
            estimated::new_facts(estimated::CALIFORNIA, 2026)
                .filing_status(FilingStatus::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(DC, 2027)
                .filing_status(FilingStatus::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        ("other extras", other.clone()),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}");
    }
    assert_eq!(
        rules().requirement(&other).err().unwrap().to_string(),
        "districtofcolumbiataxcalc: the District's rules read no extras, and the facts carry string"
    );
    assert!(rules().penalty(&forty().facts(), Date::default()).is_err());
}

#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    let all = format!(
        "{} {} {}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$100",
        "110%",
        "no 100% tier",
        "underpayment interest",
        ".0175, .0265, .0351 and .0259",
        "compounded daily",
        "next due date",
        "10% of the year's tax",
        "retired after 62",
        "January 15",
        "proration by income",
    ] {
        assert!(all.contains(s), "{s}");
    }
    assert!(!e.versus_federal().is_empty() && !e.sources().is_empty());
}

/// Without this year's tax the target is 110% of last year's alone, and the
/// interest against it is an upper bound.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && !r.exempt());
    assert_eq!(r.target(), DOLLAR * 8_800);
    let o = &r.options()[0];
    assert!(!o.available());
    assert_eq!(o.why_not(), estimated::NEEDS_THIS_YEAR_TAX);
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearTax);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [220_000, 440_000, 660_000, 880_000].iter().enumerate() {
        assert_eq!(sched.value()[i].needed_by(), USCents(*want));
    }
    let pen = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(pen.value().upper_bound());
    assert_ne!(pen.value().total(), USCents(0));
    let paid = Household {
        payments: quarterly(&[DOLLAR * 2_200; 4]),
        ..h.clone()
    };
    let pen = rules().penalty(&paid.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(pen.value().total(), USCents(0));

    // No liability last year still exempts, exactly.
    let zero = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let z = Household {
        no_tax: true,
        prior: Some(zero),
        ..Household::default()
    };
    assert!(rules().requirement(&z.facts()).unwrap().value().exempt());
    let p = rules().penalty(&z.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));
    assert!(!p.value().upper_bound());

    // With no return last year either, there is no target.
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

/// An exception sets the target, so no question may say its answer changes
/// the target. $50 of tax, with last year's return not given.
#[test]
fn estimated_exception_asks_nothing_of_the_target() {
    let res = rules().requirement(&tax(DOLLAR * 50).facts()).unwrap();
    assert!(res.value().exempt());
    for r in res.required() {
        assert!(
            !r.effect().to_lowercase().contains("target"),
            "{}",
            r.effect()
        );
    }
}

/// The penalty on $10,000 of tax with nothing paid or withheld, figured to
/// Apr 15, 2027, with the waiver asked for as `pos`.
fn waiver_penalty(pos: Positions) -> estimated::Penalty {
    let f = estimated::new_facts(DC, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .positions(pos)
        .build()
        .unwrap();
    rules().penalty(&f, day(2027, 4, 15)).unwrap().value()
}

fn waiver_for(c: WaiverCause, on: Date, born: Option<Date>) -> Positions {
    let mut b = estimated::new_positions();
    b.waiver_for(c, on);
    if let Some(d) = born {
        b.born(d);
    }
    b.build().unwrap()
}

/// § 47-4203(e)(3) and (4) are the federal pair. The statute names the
/// Mayor; the Office of Tax and Revenue decides for the Mayor.
#[test]
fn estimated_waiver_outcomes() {
    let before = waiver_penalty(
        estimated::new_positions()
            .waiver(WaiverKind::None)
            .build()
            .unwrap(),
    )
    .total();
    assert_ne!(before, USCents(0));
    let r = WaiverCause::Retirement;
    for (name, pos, want, reason) in [
        (
            "retired in 2026 at 64",
            waiver_for(r, day(2026, 6, 1), Some(day(1962, 3, 1))),
            "eligible",
            "The Office of Tax and Revenue needs",
        ),
        (
            "retired in 2020",
            waiver_for(r, day(2020, 6, 1), Some(day(1955, 3, 1))),
            "refused",
            "2025 or 2026",
        ),
        (
            "retired, no birth date",
            waiver_for(r, day(2026, 6, 1), None),
            "undecided",
            "date of birth",
        ),
        (
            "disabled in 2025",
            waiver_for(WaiverCause::Disability, day(2025, 2, 1), None),
            "eligible",
            "disabled in 2025",
        ),
        (
            "a disaster",
            waiver_for(WaiverCause::Disaster, day(2026, 8, 1), None),
            "eligible",
            "a disaster",
        ),
    ] {
        let p = waiver_penalty(pos);
        let o = p.waiver_outcome().unwrap();
        let (got, if_waived) = if o.eligible() {
            ("eligible", USCents(0))
        } else if o.refused() {
            ("refused", p.total())
        } else {
            ("undecided", p.total())
        };
        assert_eq!(got, want, "{name}");
        assert!(o.reason().contains(reason), "{name}: {}", o.reason());
        assert_eq!(p.total(), before, "{name}");
        assert_eq!(p.if_waived(), if_waived, "{name}");
    }
}

/// Withholding over the tax reads as a refund, never as a minus amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let rules = rules();
    let mut asked = false;
    for a in estimatedtest::ask(
        &rules,
        &estimatedtest::households(&estimated::DISTRICT_OF_COLUMBIA),
    ) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked = true;
        let why = a.requirement().unwrap().value().why();
        assert!(
            why.contains("leaves a $600 refund. No estimated payments are needed"),
            "why {why}"
        );
        for x in estimatedtest::texts(Some(&rules), &[a]) {
            assert!(
                !x.text().contains("−$") && !x.text().contains("-$"),
                "{}: {}",
                x.where_(),
                x.text()
            );
        }
    }
    assert!(asked, "no household withholding over the tax");
}
