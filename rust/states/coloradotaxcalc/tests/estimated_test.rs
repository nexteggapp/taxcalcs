//! Colorado's estimated tax rules for 2026, ported from the Go package's
//! estimated_test.go and estimated_refund_test.go. (Go also compares every
//! figure and the Red Books against their extracts in testdata/; those checks
//! stay in Go.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Penalty, Positions, PriorReturn, Status,
    WaiverCause, WaiverKind, Weekday,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_coloradotaxcalc::{EstimatedFacts, estimated_tax, new_estimated_facts};
use working::{PositionKind, Reading, Required, Rule};

const COLORADO: estimated::Jurisdiction = estimated::COLORADO;

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

fn quarterly(a: [USCents; 4]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .map(|(i, x)| pay(dues()[i], *x))
        .collect()
}

#[derive(Default, Clone)]
struct Household {
    status: Option<filing::Status>,
    /// DR 0204 line 1.
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    withheld_on: Date,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    no_extras: bool,
    return_filed: Date,
    /// Colorado taxable income to each period end.
    periods: Vec<USCents>,
    as_withheld: bool,
    waiver: Option<WaiverKind>,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let status = self.status.unwrap_or(filing::Status::Single);
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        let mut b = estimated::new_facts(COLORADO, 2026);
        b.filing_status(status)
            .this_year(fb.build().unwrap())
            .paid(&self.payments)
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
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        if let Some(e) = self.extras {
            b.state(e);
        } else if !self.no_extras {
            b.state(
                new_estimated_facts()
                    .prior_year_farmer_or_fisher(false)
                    .federal_annualised(true)
                    .build()
                    .unwrap(),
            );
        }
        let mut eb = estimated::new_elections();
        if !self.periods.is_empty() {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            let periods: Vec<_> = self
                .periods
                .iter()
                .enumerate()
                .map(|(i, inc)| {
                    estimated::new_period()
                        .end(ends[i])
                        .income(*inc)
                        .build()
                        .unwrap()
                })
                .collect();
            b.periods(&periods);
            eb.annualised();
        }
        if self.as_withheld {
            eb.withholding_as_withheld();
        }
        b.elections(eb.build().unwrap());
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        b.build().unwrap()
    }
}

/// A filed twelve-month Colorado return with its tax and last year's federal
/// AGI.
fn prior(tax: USCents, federal_agi: USCents) -> Option<PriorReturn> {
    let f = estimated::new_figures()
        .tax(tax)
        .federal_agi(federal_agi)
        .build()
        .unwrap();
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(f)
            .full_year_resident(true)
            .build()
            .unwrap(),
    )
}

fn penalty(h: &Household) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap()
}

/// The 2026 dates are the ones DR 0104EP prints. None falls on a weekend, so
/// none moves. The file-and-pay date, January 31, 2027, is a Sunday and is
/// not moved.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&tax(DOLLAR * 40_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    assert_eq!(day(2027, 1, 31).weekday(), Weekday::SUNDAY);
    for (filed, excused, asked) in [
        (day(2027, 1, 29), true, false),
        (day(2027, 1, 31), true, false),
        (day(2027, 2, 1), false, true), // the next business day: an open question
        (day(2027, 2, 2), false, false),
    ] {
        let h = Household {
            return_filed: filed,
            ..tax(DOLLAR * 40_000)
        };
        let res = rules().schedule(&h.facts()).unwrap();
        assert_eq!(
            res.value()[3].status() == Status::Excused,
            excused,
            "filed {filed}"
        );
        assert_eq!(
            because(&res.required(), PositionKind::UnsettledRule).contains("Sunday"),
            asked,
            "filed {filed}"
        );
    }
}

/// DR 0204 exempts tax less withholding of $1,000 or less. $1,000.00 is
/// exempt; $1,000.01 is not.
#[test]
fn estimated_threshold_boundary() {
    for (withheld, exempt) in [(200_000, true), (199_999, false)] {
        let h = Household {
            withheld: USCents(withheld),
            ..tax(DOLLAR * 3_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "{}", res.value().why());
        if exempt {
            let c = res.value().chosen().unwrap();
            assert_eq!(c.basis(), Basis::Exception);
            assert_eq!(c.amount(), USCents(0));
            assert_eq!(c.label(), "Tax less withholding of $1,000 or less");
        }
        assert_ne!(res.value().required(), exempt);
        let p = penalty(&h);
        assert_eq!(p.value().total().0 == 0, exempt);
    }
}

/// § 39-22-605(6)(c): 110% when last year's federal AGI exceeds $150,000,
/// or $75,000 on a separate return this year.
#[test]
fn estimated_prior_year_step_boundary() {
    use filing::Status::*;
    for (status, agi, want) in [
        (Single, 15_000_000, percent(100)),
        (Single, 15_000_001, percent(110)),
        (MarriedFilingJointly, 15_000_000, percent(100)),
        (MarriedFilingJointly, 15_000_001, percent(110)),
        (MarriedFilingSeparately, 7_500_000, percent(100)),
        (MarriedFilingSeparately, 7_500_001, percent(110)),
        (MarriedFilingJointly, 7_500_001, percent(100)),
    ] {
        let h = Household {
            status: Some(status),
            prior: prior(DOLLAR * 10_000, USCents(agi)),
            ..tax(DOLLAR * 100_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let o = res.value().options()[1].clone();
        assert_eq!(o.percent(), want, "{status}, AGI {agi}");
        assert_eq!(o.amount(), want.of(DOLLAR * 10_000), "{status}, AGI {agi}");
    }
    // Without last year's AGI, 110% is assumed and asked about.
    let f = estimated::new_figures()
        .tax(DOLLAR * 10_000)
        .build()
        .unwrap();
    let p = estimated::new_prior_return()
        .filed(true)
        .figures(f)
        .build()
        .unwrap();
    let h = Household {
        prior: Some(p),
        ..tax(DOLLAR * 100_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().options()[1].percent(), percent(110));
    assert!(has(&res.required(), PositionKind::AdjustedGrossIncome));
}

/// The target is the smaller of 70% of this year's tax and the prior-year
/// test, and the prior-year test needs a filed twelve-month Colorado return.
#[test]
fn estimated_target_options() {
    let h = Household {
        prior: prior(DOLLAR * 20_000, DOLLAR * 100_000),
        ..tax(DOLLAR * 40_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().chosen().unwrap().basis(), Basis::PriorYearTax);
    assert_eq!(res.value().target(), DOLLAR * 20_000);
    let h = Household {
        prior: prior(DOLLAR * 30_000, DOLLAR * 100_000),
        ..tax(DOLLAR * 40_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().chosen().unwrap().basis(), Basis::ThisYearTax);
    assert_eq!(res.value().target(), DOLLAR * 28_000);
    assert_eq!(res.value().options()[0].percent(), percent(70));

    let f = estimated::new_figures()
        .tax(DOLLAR * 5_000)
        .federal_agi(DOLLAR * 50_000)
        .build()
        .unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(f)
        .build()
        .unwrap();
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    for (name, p, ask) in [
        ("no return on record", None, true),
        ("not filed", Some(not_filed), false),
        ("not required", Some(not_required), false),
        ("a six-month year", Some(short), false),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 40_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(!res.value().options()[1].available(), "{name}");
        assert_eq!(res.value().target(), DOLLAR * 28_000, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn),
            ask,
            "{name}"
        );
    }
}

/// $40,000 of tax, nothing withheld or paid, no return last year: each
/// installment $7,000. Colorado's rate changes on January 1: 11% for 2026,
/// 10% for 2027. The total is $1,970.74.
#[test]
fn estimated_penalty_across_two_rate_periods() {
    let res = penalty(&tax(DOLLAR * 40_000));
    let p = res.value();
    let want: [&[(i64, Rate, i64)]; 4] = [
        &[(260, percent(11), 54_849), (105, percent(10), 20_137)],
        &[(199, percent(11), 41_981), (105, percent(10), 20_137)],
        &[(107, percent(11), 22_573), (105, percent(10), 20_137)],
        &[(90, percent(10), 17_260)],
    ];
    let lines = p.lines();
    assert_eq!(lines.len(), 4);
    for (i, l) in lines.iter().enumerate() {
        assert_eq!(l.shortfall(), DOLLAR * 7_000, "line {}", i + 1);
        let parts = l.parts();
        assert_eq!(parts.len(), want[i].len(), "line {}", i + 1);
        for (j, (days, rate, charge)) in want[i].iter().enumerate() {
            assert_eq!(parts[j].days(), *days, "line {} part {}", i + 1, j + 1);
            assert_eq!(parts[j].rate(), *rate, "line {} part {}", i + 1, j + 1);
            assert_eq!(
                parts[j].charge(),
                USCents(*charge),
                "line {} part {}",
                i + 1,
                j + 1
            );
        }
    }
    assert_eq!(p.total(), USCents(197_074));
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));

    // A payment pays the oldest shortfall first. $7,000 on May 15 pays
    // April: 30 days at 11%, $63.29.
    let h = Household {
        payments: vec![pay(day(2026, 5, 15), DOLLAR * 7_000)],
        ..tax(DOLLAR * 40_000)
    };
    let p = rules()
        .penalty(&h.facts(), day(2026, 6, 1))
        .unwrap()
        .value();
    let l = p.lines();
    assert!(!l.is_empty() && l[0].installment() == 1 && l[0].total() == USCents(6_329));
    assert_eq!(p.total(), USCents(6_329));
}

/// CDOR's interest example: $200 due April 15, 2022, paid February 22, 2024,
/// at 3%, 5% and 8%, is $4.27, $10.00 and $2.32, $16.59 in all.
#[test]
fn interest_matches_the_departments_example() {
    let page = "CDOR, Tax Topics: Penalties and Interest";
    let s = estimated::new_rate_series()
        .period(day(2022, 1, 1), day(2022, 12, 31), percent(3), page)
        .period(day(2023, 1, 1), day(2023, 12, 31), percent(5), page)
        .period(day(2024, 1, 1), day(2024, 12, 31), percent(8), page)
        .build()
        .unwrap();
    let (parts, gap) =
        estimated::simple_daily(DOLLAR * 200, day(2022, 4, 15), day(2024, 2, 22), &s).unwrap();
    assert!(gap.is_zero());
    let got: Vec<i64> = parts.iter().map(|p| p.charge().0).collect();
    assert_eq!(got, [427, 1_000, 232]);
    assert_eq!(got.iter().sum::<i64>(), 1_659);
}

/// § 39-22-605(8)(a): withholding is paid in equal parts on the due dates,
/// unless the filer establishes when it was actually withheld.
#[test]
fn estimated_withholding() {
    let mut h = Household {
        withheld: DOLLAR * 8_000,
        ..tax(DOLLAR * 40_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.credited_by(), DOLLAR * 2_000 * (i as i64 + 1));
    }
    h.as_withheld = true;
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [0, 0, 0, 8_000].into_iter().enumerate() {
        assert_eq!(s[i].credited_by(), DOLLAR * want, "installment {}", i + 1);
    }
    // Last year's overpayment counts toward April.
    let h = Household {
        payments: vec![
            estimated::new_payment()
                .amount(DOLLAR * 7_000)
                .from_prior_year()
                .build()
                .unwrap(),
        ],
        ..tax(DOLLAR * 40_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].status(), Status::Met);
}

/// DR 0204 Part 4: Colorado taxable income to each period end, times 4,
/// 2.4, 1.5 and 1, at 4.4%, times 17.5%, 35%, 52.5% and 70%, less earlier
/// installments.
#[test]
fn estimated_annualised_installments() {
    let mut h = Household {
        periods: vec![
            DOLLAR * 10_000,
            DOLLAR * 20_000,
            DOLLAR * 60_000,
            DOLLAR * 200_000,
        ],
        ..tax(DOLLAR * 8_800)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [30_800, 73_920, 207_900, 616_000].into_iter().enumerate() {
        assert_eq!(s[i].needed_by(), USCents(want), "installment {}", i + 1);
    }
    // The form enters line 26 on line 7 as it is, without the federal cap.
    h.periods[0] = DOLLAR * 100_000;
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].needed_by(), USCents(308_000));
    // Not annualising federally: the regular quarters.
    h.extras = Some(
        new_estimated_facts()
            .prior_year_farmer_or_fisher(false)
            .federal_annualised(false)
            .build()
            .unwrap(),
    );
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].needed_by(), USCents(154_000));
    // Not said: the election is honoured and the question asked.
    h.extras = None;
    h.no_extras = true;
    let res = rules().schedule(&h.facts()).unwrap();
    assert_eq!(res.value()[0].needed_by(), USCents(308_000));
    assert!(
        because(&res.required(), PositionKind::JurisdictionFact)
            .contains("annualised installments for federal income tax")
    );
    // The periods must be the form's.
    let period = |m, d| {
        estimated::new_period()
            .end(day(2026, m, d))
            .build()
            .unwrap()
    };
    let f = estimated::new_facts(COLORADO, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .periods(&[period(3, 31), period(6, 30), period(8, 31), period(12, 31)])
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    assert!(rules().schedule(&f).is_err());
}

/// The statute's no-liability exception and its waivers are not on DR 0204:
/// they are reported and not applied.
#[test]
fn estimated_exceptions() {
    let zero = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let res = penalty(&Household {
        prior: Some(zero),
        ..tax(DOLLAR * 40_000)
    });
    assert!(res.value().total().0 != 0);
    assert!(
        applied(&res.applied())
            .contains("§ 39-22-605(7)(b): you had no Colorado liability for 2025")
    );
    // A waiver asked for is not subtracted; a casualty is only good cause.
    let res = penalty(&Household {
        waiver: Some(WaiverKind::Casualty),
        ..tax(DOLLAR * 40_000)
    });
    assert_eq!(res.value().total(), USCents(197_074));
    assert_eq!(res.value().waiver(), WaiverKind::Casualty);
    assert!(!has(&res.required(), PositionKind::PenaltyWaiver));
    assert!(applied(&res.applied()).contains("no waiver for a casualty or disaster as such"));
    // The penalty stops on the day the return is paid, and by April 15.
    let p = penalty(&Household {
        return_filed: day(2027, 3, 1),
        ..tax(DOLLAR * 40_000)
    });
    assert_eq!(p.value().as_of(), day(2027, 3, 1));
    let p = rules()
        .penalty(&tax(DOLLAR * 40_000).facts(), day(2027, 6, 30))
        .unwrap();
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
    assert_eq!(p.value().total(), USCents(197_074));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 40_000).facts()).unwrap();
    let want: [(&str, [&str; 4]); 5] = [
        (
            "Tax of exactly $1,000 after withholding",
            [
                "$1,000 or less",
                "Exception 2",
                "less than $1,000",
                "§ 39-22-605(7)(a)",
            ],
        ),
        (
            "The interest rates DR 0204 prints",
            [
                "12% to December 31 and 11% after",
                "lines 15 and 18",
                "11% for 2026 and 10% for 2027",
                "39-21-110.5",
            ],
        ),
        (
            "No liability last year",
            [
                "isn't one of them",
                "Part 1",
                "resident throughout",
                "§ 39-22-605(7)(b)",
            ],
        ),
        (
            "Waivers for good cause, retirement or disability",
            [
                "no waiver",
                "DR 0204",
                "retired after 62",
                "§ 39-22-605(7)(c), (d)",
            ],
        ),
        (
            "Due dates on a weekend or holiday",
            [
                "weekend or legal holiday",
                "DR 0104EP",
                "legal federal holiday",
                "§ 39-22-605(5)(c)",
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
        // Nothing paid on $40,000 of tax: only the rate changes these figures.
        let rate = d.rule() == "The interest rates DR 0204 prints";
        let uses = if rate {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert_eq!(d.uses(), uses, "{:?}", d.rule());
        assert_eq!(d.affects_household(), rate, "{:?}", d.rule());
    }
    let affects = |h: Household, rule: &str| -> bool {
        let res = rules().requirement(&h.facts()).unwrap();
        let ds = res.divergences();
        let Some(d) = ds.iter().find(|d| d.rule() == rule) else {
            panic!("no divergence {rule:?}");
        };
        d.affects_household()
    };
    let exact = Household {
        withheld: DOLLAR * 2_000,
        ..tax(DOLLAR * 3_000)
    };
    assert!(affects(
        exact.clone(),
        "Tax of exactly $1,000 after withholding"
    ));
    assert!(!affects(exact, "The interest rates DR 0204 prints"));
    let zero = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    assert!(affects(
        Household {
            prior: Some(zero),
            ..tax(DOLLAR * 40_000)
        },
        "No liability last year"
    ));
    assert!(affects(
        Household {
            waiver: Some(WaiverKind::Casualty),
            ..tax(DOLLAR * 40_000)
        },
        "Waivers for good cause, retirement or disability"
    ));
    // The rates the figure uses are the statute's, not the form's.
    for l in penalty(&tax(DOLLAR * 40_000)).value().lines() {
        for part in l.parts() {
            assert!(part.rate() == percent(11) || part.rate() == percent(10));
        }
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for y in [2025, 2027] {
        let err = estimated_tax(y).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{y}: {err}");
    }
    let this = estimated::new_figures()
        .tax(DOLLAR * 40_000)
        .build()
        .unwrap();
    let base = || {
        let mut b = estimated::new_facts(COLORADO, 2026);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    let prior_farmer = new_estimated_facts()
        .prior_year_farmer_or_fisher(true)
        .build()
        .unwrap();
    for (name, f) in [
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
    ] {
        let err = rules().requirement(&f).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    rules()
        .requirement(&base().farm_share(Rate(6666)).build().unwrap())
        .unwrap();
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
            estimated::new_facts(COLORADO, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            base().state("not Colorado's").build().unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    // Last year's farming not said: asked for.
    let res = rules().requirement(&base().build().unwrap()).unwrap();
    assert!(
        because(&res.required(), PositionKind::JurisdictionFact).contains("farming or fishing")
    );
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
        "70%",
        "$150,000",
        "11% for 2026 and 10% for 2027",
        "once a year",
        "annualise federally",
        "17.5%, 35%, 52.5% and 70%",
        "Colorado residence",
        "good cause",
        "$1,000 or less",
        "January 31",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.versus_federal().is_empty() && !e.sources().is_empty());
}

/// Without this year's tax the target is last year's tax alone, and the
/// penalty against it is an upper bound.
#[test]
fn estimated_without_this_years_tax_uses_last_years_alone() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000, DOLLAR * 100_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 8_000 && !r.exempt());
    let o = r.options()[0].clone();
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearTax);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [200_000, 400_000, 600_000, 800_000].into_iter().enumerate() {
        assert_eq!(sched.value()[i].needed_by(), USCents(want));
    }
    let pen = penalty(&h);
    assert!(pen.value().upper_bound() && pen.value().total().0 != 0);
    let k = DOLLAR * 2_000;
    let paid = Household {
        payments: quarterly([k, k, k, k]),
        ..h
    };
    assert_eq!(penalty(&paid).value().total(), USCents(0));

    // With no return last year either, there is no target.
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let res = rules().requirement(&none.facts()).unwrap();
    assert!(!res.value().target_known() && !res.value().required());
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
    let err = rules()
        .penalty(&none.facts(), day(2027, 4, 15))
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
}

/// An exception sets the target, so no question may say its answer changes
/// the target. $500 of tax, with last year's return not given.
#[test]
fn estimated_exception_asks_nothing_of_the_target() {
    let f = estimated::new_facts(COLORADO, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(DOLLAR * 500).build().unwrap())
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    assert!(res.value().exempt());
    for r in res.required() {
        assert!(
            !r.effect().to_lowercase().contains("target"),
            "{}: {:?}",
            r.position(),
            r.effect()
        );
    }
}

/// The penalty on $10,000 of tax with nothing paid or withheld, figured to
/// Apr 15, 2027, with the waiver asked for as `pos`.
fn waiver_penalty(pos: Positions) -> working::Result<Penalty> {
    let f = estimated::new_facts(COLORADO, 2026)
        .filing_status(filing::Status::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .positions(pos)
        .build()
        .unwrap();
    rules().penalty(&f, day(2027, 4, 15)).unwrap()
}

fn waiver_for(c: WaiverCause, on: Date, born: Date) -> Positions {
    estimated::new_positions()
        .waiver_for(c, on)
        .born(born)
        .build()
        .unwrap()
}

/// § 39-22-605(7)(d) is the federal retirement and disability waiver. There
/// is no casualty waiver as such: (7)(c) waives for good cause instead.
#[test]
fn estimated_waiver_outcomes() {
    let before = waiver_penalty(
        estimated::new_positions()
            .waiver(WaiverKind::None)
            .build()
            .unwrap(),
    )
    .value()
    .total();
    assert!(before.0 != 0);
    let none = Date::default();
    for (name, pos, want, reason) in [
        (
            "retired in 2026 at 64",
            waiver_for(WaiverCause::Retirement, day(2026, 6, 1), day(1962, 3, 1)),
            "eligible",
            "The executive director needs",
        ),
        (
            "retired in 2020",
            waiver_for(WaiverCause::Retirement, day(2020, 6, 1), day(1955, 3, 1)),
            "refused",
            "2025 or 2026",
        ),
        (
            "disabled in 2025",
            waiver_for(WaiverCause::Disability, day(2025, 2, 1), none),
            "eligible",
            "disabled in 2025",
        ),
        // The difference from federal: good cause, not a casualty waiver.
        (
            "a casualty",
            waiver_for(WaiverCause::Casualty, day(2026, 8, 1), none),
            "eligible",
            "good cause",
        ),
        (
            "a disaster",
            waiver_for(WaiverCause::Disaster, day(2026, 8, 1), none),
            "eligible",
            "no waiver for a casualty or disaster as such",
        ),
    ] {
        let p = waiver_penalty(pos).value();
        let o = p.waiver_outcome().unwrap();
        let (got, if_waived) = if o.eligible() {
            ("eligible", USCents(0))
        } else if o.refused() {
            ("refused", p.total())
        } else {
            ("undecided", p.total())
        };
        assert_eq!(got, want, "{name}: {:?}", o.reason());
        assert!(o.reason().contains(reason), "{name}: {:?}", o.reason());
        assert_eq!(p.total(), before, "{name}");
        assert_eq!(p.if_waived(), if_waived, "{name}");
    }
}

/// Withholding over the tax reads as a refund, never as a minus amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let r = rules();
    let mut asked = false;
    for a in estimatedtest::ask(&r, &estimatedtest::households(&COLORADO)) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked = true;
        let why = a.requirement().unwrap().value().why();
        assert!(
            why.contains("leaves a $600 refund. No estimated payments are needed"),
            "why {why:?}"
        );
        for x in estimatedtest::texts(Some(&r), std::slice::from_ref(&a)) {
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
