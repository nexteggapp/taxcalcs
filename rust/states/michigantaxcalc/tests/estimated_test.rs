//! Michigan's estimated tax rules for 2026.
//!
//! Michigan publishes no exhaustive oracle for estimated tax. The strongest
//! checks are worked examples with stated answers: the MI-2210
//! instructions' example of a $1,000 shortfall paid on June 10 (56 days of
//! interest, a $100 penalty), and RAB 2021-17 Examples 6 and 7, which give
//! each quarter's required installment under the annualised method and the
//! penalty on it. The exceptions, the boundaries and the open questions are
//! hand-written cases. (The Go test comparing figures against the extracts
//! in testdata/ checks the Go package's documents, not code, and is not
//! ported.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, PartKind, Payment, Penalty, PenaltyLine,
    PenaltyPart, Positions, PriorReturn, Status, WaiverCause, Weekday,
};
use michigantaxcalc::EstimatedFacts;
use money::{DOLLAR, Rate, USCents, dollars, percent};
use taxcalcs_michigantaxcalc as michigantaxcalc;
use working::{PositionKind, Reading, Required};

const MI: Jurisdiction = Jurisdiction::from_static("MI");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    michigantaxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

/// Whether a position of kind k says s in its question or effect.
fn asked(rs: &[Required], k: PositionKind, s: &str) -> bool {
    rs.iter()
        .any(|r| r.position() == k && (r.question().contains(s) || r.effect().contains(s)))
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
    /// After credits.
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    as_withheld: bool,
    status: filing::Status,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    /// MI-2210 Worksheet line 8 for each period.
    annualised: Option<Vec<USCents>>,
    positions: Option<Positions>,
}

fn answered() -> EstimatedFacts {
    michigantaxcalc::new_estimated_facts()
        .prior_year_estimates_required(true)
        .prior_year_married_filing_separately(false)
        .build()
        .unwrap()
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
        let mut figures = estimated::new_figures().tax(self.tax).build().unwrap();
        if self.no_tax {
            figures = estimated::new_figures().build().unwrap();
        }
        let mut b = estimated::new_facts(MI, 2026);
        b.filing_status(status)
            .this_year(figures)
            .paid(&self.payments)
            .requirement_first_met(self.first_met)
            .return_filed(self.return_filed);
        if self.withheld.0 > 0 {
            b.withheld(&[estimated::new_withheld()
                .on(day(2026, 12, 15))
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
        if let Some(ann) = &self.annualised {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            for (i, tax) in ann.iter().enumerate() {
                b.periods(&[estimated::new_period()
                    .end(ends[i])
                    .annualised_tax(*tax)
                    .build()
                    .unwrap()]);
            }
            b.elections(estimated::new_elections().annualised().build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A 12-month 2025 return with its tax and federal AGI.
fn filed(tax: USCents, agi: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(
                estimated::new_figures()
                    .tax(tax)
                    .federal_agi(agi)
                    .build()
                    .unwrap(),
            )
            .build()
            .unwrap(),
    )
}

fn line(p: &Penalty, installment: i64) -> Option<PenaltyLine> {
    p.lines()
        .into_iter()
        .find(|l| l.installment() == installment)
}

/// A line's interest parts and its flat part.
fn split(l: &PenaltyLine) -> (Vec<PenaltyPart>, Option<PenaltyPart>) {
    let (mut interest, mut flat) = (Vec::new(), None);
    for p in l.parts() {
        match p.kind() {
            PartKind::Interest => interest.push(p),
            PartKind::Flat => flat = Some(p),
            _ => {}
        }
    }
    (interest, flat)
}

fn flat_total(p: &Penalty) -> USCents {
    p.lines()
        .iter()
        .filter_map(|l| split(l).1)
        .map(|f| f.charge())
        .sum()
}

fn penalty(h: &Household) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap()
}

#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&taxed(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    assert!(
        day(2027, 1, 31).weekday() == Weekday::SUNDAY
            && day(2027, 2, 1).weekday() == Weekday::MONDAY
            && day(2027, 4, 15).weekday() == Weekday::THURSDAY
    );
}

/// MCL 206.301(1) and the MI-2210: more than $500 after withholding and
/// credits; $500.00 exactly is not.
#[test]
fn estimated_threshold_boundary() {
    for (withheld, exempt) in [(950_000, true), (949_999, false)] {
        let res = rules()
            .requirement(
                &Household {
                    withheld: USCents(withheld),
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {withheld}");
        let c = res.value().chosen().unwrap();
        assert_eq!(c.basis() == Basis::Exception, exempt, "withheld {withheld}");
        if exempt {
            assert!(c.amount().0 == 0 && c.label() == "Tax less withholding of $500 or less");
        }
    }
}

/// The prior-year test is 110% above $150,000 of last year's AGI, or
/// $75,000 if last year's return was separate.
#[test]
fn estimated_prior_year_step_boundaries() {
    let sep = michigantaxcalc::new_estimated_facts()
        .prior_year_estimates_required(true)
        .prior_year_married_filing_separately(true)
        .build()
        .unwrap();
    for (agi, extras, want) in [
        (15_000_000, None, percent(100)),
        (15_000_001, None, percent(110)),
        (7_500_000, Some(sep), percent(100)),
        (7_500_001, Some(sep), percent(110)),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: filed(DOLLAR * 10_000, USCents(agi)),
                    extras,
                    ..taxed(DOLLAR * 100_000)
                }
                .facts(),
            )
            .unwrap();
        let o = &res.value().options()[1];
        assert!(
            o.percent() == want && o.amount() == want.of(DOLLAR * 10_000),
            "AGI {agi}"
        );
    }

    // This year separate and last year's status not said: $75,000 is used,
    // and the question is asked because $100,000 lies between the steps.
    let none = michigantaxcalc::new_estimated_facts()
        .prior_year_estimates_required(true)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                status: filing::Status::MarriedFilingSeparately,
                prior: filed(DOLLAR * 10_000, DOLLAR * 100_000),
                extras: Some(none),
                ..taxed(DOLLAR * 100_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().options()[1].percent(), percent(110));
    assert!(asked(
        &res.required(),
        PositionKind::JurisdictionFact,
        "married filing separately"
    ));

    // No AGI: 110% is assumed and the AGI is asked for.
    let p = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                prior: Some(p),
                ..taxed(DOLLAR * 100_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().options()[1].percent(), percent(110));
    assert!(has(&res.required(), PositionKind::AdjustedGrossIncome));
}

/// The target is the smaller available option, and the prior-year option
/// needs a filed 12-month return.
#[test]
fn estimated_target_options() {
    let res = rules()
        .requirement(
            &Household {
                prior: filed(DOLLAR * 8_000, DOLLAR * 90_000),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    let c = res.value().chosen().unwrap();
    assert!(c.basis() == Basis::PriorYearTax && res.value().target() == DOLLAR * 8_000);
    let res = rules()
        .requirement(
            &Household {
                prior: filed(DOLLAR * 9_500, DOLLAR * 90_000),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    let c = res.value().chosen().unwrap();
    assert!(c.basis() == Basis::ThisYearTax && res.value().target() == DOLLAR * 9_000);

    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .build()
        .unwrap();
    for (name, p, why) in [
        (
            "no return on record",
            None,
            "Needs your 2025 Michigan return",
        ),
        (
            "not filed",
            Some(not_filed),
            "You didn't file a 2025 Michigan return",
        ),
        (
            "not required to file",
            Some(not_required),
            "You weren't required to file a 2025 Michigan return",
        ),
        ("eleven months", Some(short), "covered 11 months"),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: p,
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap();
        let o = &res.value().options()[1];
        assert!(!o.available() && o.why_not().contains(why), "{name}");
        if p.is_none() {
            assert!(
                has(&res.required(), PositionKind::PriorYearReturn),
                "{name}"
            );
        }
    }
}

/// The MI-2210 instructions' example: $2,000 due each quarter, $1,000 short
/// in April, $2,000 paid June 10. Interest is on $1,000 for 56 days, $13,
/// and the penalty is $100 in the first quarter and nothing in the second.
#[test]
fn estimated_interest_and_penalty_match_mi2210_example() {
    // 90% of $8,888.89 is $8,000.00.
    let h = Household {
        payments: vec![
            pay(dues()[0], DOLLAR * 1_000),
            pay(day(2026, 6, 10), DOLLAR * 2_000),
        ],
        ..taxed(USCents(888_889))
    };
    let p = penalty(&h).value();
    let q1 = line(&p, 1).unwrap();
    let (interest, flat) = split(&q1);
    assert!(
        q1.shortfall() == DOLLAR * 1_000
            && interest.len() == 1
            && interest[0].days() == 56
            && interest[0].charge() == DOLLAR * 13
    );
    let flat = flat.unwrap();
    assert!(flat.charge() == DOLLAR * 100 && flat.rate() == percent(10));
    let q2 = line(&p, 2).unwrap();
    assert_eq!(q2.shortfall(), DOLLAR * 1_000);
    assert!(split(&q2).1.is_none(), "June penalty, want none");
}

/// RAB 2021-17 Example 6, in 2026 dates: a 10% penalty of $23 on the third
/// quarter's $225, and of $10 on the fourth's $75, or none if the return is
/// filed and paid by January 31. Interest $5, $1 and $6.
#[test]
fn estimated_penalty_matches_rab2021_17_example6() {
    let mut h = Household {
        prior: filed(DOLLAR * 5_100, DOLLAR * 124_400),
        annualised: Some(vec![
            DOLLAR * 3_878,
            DOLLAR * 4_898,
            DOLLAR * 8_404,
            DOLLAR * 6_003,
        ]),
        payments: quarterly(&[1_200, 1_200, 1_200, 1_200]),
        ..taxed(DOLLAR * 6_003)
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [87_255, 220_410, 382_500, 510_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), USCents(want), "installment {}", i + 1);
    }

    let p = penalty(&h).value();
    assert_eq!(p.lines().len(), 2);
    let q3 = line(&p, 3).unwrap();
    let (interest, flat) = split(&q3);
    assert!(q3.shortfall() == DOLLAR * 225 && flat.unwrap().charge() == DOLLAR * 23);
    assert!(
        interest.len() == 2
            && interest[0].days() == 107
            && interest[1].days() == 15
            && interest[0].charge() == DOLLAR * 5
            && interest[1].charge() == DOLLAR
    );
    let q4 = line(&p, 4).unwrap();
    let (interest, flat) = split(&q4);
    assert!(
        q4.shortfall() == DOLLAR * 300
            && interest.len() == 1
            && interest[0].days() == 90
            && interest[0].charge() == DOLLAR * 6
    );
    let flat = flat.unwrap();
    assert!(flat.principal() == DOLLAR * 75 && flat.charge() == DOLLAR * 10);
    assert_eq!(p.total(), DOLLAR * 45);

    h.return_filed = day(2027, 1, 29);
    let p = penalty(&h).value();
    let q4 = line(&p, 4).unwrap();
    let (interest, flat) = split(&q4);
    assert!(flat.is_none() && !interest.is_empty());
}

/// RAB 2021-17 Example 7, in 2026 dates. The fourth quarter's penalty here
/// is $29 on $294, following the form rather than the example's $59, and
/// $588 is its shortfall for interest.
#[test]
fn estimated_penalty_follows_the_form_against_rab2021_17_example7() {
    let h = Household {
        prior: filed(DOLLAR * 6_188, DOLLAR * 150_000),
        annualised: Some(vec![
            USCents(388_000),
            USCents(693_778),
            USCents(579_111),
            USCents(727_778),
        ]),
        payments: quarterly(&[1_400, 1_400, 1_400, 1_400]),
        ..taxed(USCents(727_778))
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [87_300, 309_400, 390_900, 618_800].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), USCents(want), "installment {}", i + 1);
    }
    let p = penalty(&h).value();
    let flat = split(&line(&p, 2).unwrap()).1.unwrap();
    assert!(flat.principal() == DOLLAR * 294 && flat.charge() == DOLLAR * 29);
    let q4 = line(&p, 4).unwrap();
    assert_eq!(q4.shortfall(), DOLLAR * 588);
    let flat = split(&q4).1.unwrap();
    assert!(flat.principal() == DOLLAR * 294 && flat.charge() == DOLLAR * 29);
}

/// A shortfall unpaid from April 15, 2026 to April 15, 2027 runs through
/// three rate periods with three different factors. Nothing was paid, so
/// the penalty is 25%, $2,500.
#[test]
fn estimated_penalty_across_rate_periods() {
    // 90% of $44,444.44 is $40,000, a quarter of which is $10,000.
    let res = rules()
        .penalty(&taxed(USCents(4_444_444)).facts(), day(2027, 6, 30))
        .unwrap();
    let p = res.value();
    assert_eq!(p.as_of(), day(2027, 4, 15));
    let q1 = line(&p, 1).unwrap();
    let (interest, flat) = split(&q1);
    let want = [
        (76, Rate(848), DOLLAR * 177),
        (184, Rate(785), DOLLAR * 396),
        (105, Rate(777), DOLLAR * 224),
    ];
    assert_eq!(interest.len(), want.len());
    for (i, w) in want.iter().enumerate() {
        assert!(
            interest[i].days() == w.0 && interest[i].rate() == w.1 && interest[i].charge() == w.2,
            "part {}",
            i + 1
        );
    }
    let flat = flat.unwrap();
    assert!(flat.rate() == percent(25) && flat.charge() == DOLLAR * 2_500);
    assert_eq!(q1.total(), DOLLAR * 3_297);
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
}

/// The penalty is 25% with a $25 minimum for a quarter with nothing paid,
/// and 10% with a $10 minimum for one paid short, each in whole dollars.
#[test]
fn estimated_flat_penalty_rates_and_minimums() {
    for (name, prior, payments, rate, charge) in [
        ("nothing paid on $50", 200, vec![], percent(25), 25),
        ("nothing paid on $102", 408, vec![], percent(25), 26),
        ("$5 short", 200, quarterly(&[45]), percent(10), 10),
        ("$95 short", 4_000, quarterly(&[905]), percent(10), 10),
        ("$105 short", 4_000, quarterly(&[895]), percent(10), 11),
    ] {
        let h = Household {
            prior: filed(DOLLAR * prior, DOLLAR * 50_000),
            payments,
            ..taxed(DOLLAR * 100_000)
        };
        let p = penalty(&h).value();
        let flat = split(&line(&p, 1).unwrap()).1.unwrap();
        assert!(
            flat.rate() == rate && flat.charge() == DOLLAR * charge,
            "{name}"
        );
    }
}

/// Withholding is a quarter on each date, and counts as paid when choosing
/// between 25% and 10%. By election it counts on the dates withheld.
#[test]
fn estimated_withholding() {
    let h = Household {
        withheld: DOLLAR * 2_000,
        ..taxed(USCents(444_444))
    };
    let p = penalty(&h).value();
    for i in 1..=4 {
        let flat = split(&line(&p, i).unwrap()).1.unwrap();
        assert!(
            flat.rate() == percent(10) && flat.charge() == DOLLAR * 50,
            "quarter {i}"
        );
    }

    let mut h = Household {
        withheld: DOLLAR * 8_000,
        ..taxed(USCents(888_889))
    };
    for inst in rules().schedule(&h.facts()).unwrap().value() {
        assert_eq!(inst.status(), Status::Met, "spread: {}", inst.number());
    }
    h.as_withheld = true;
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, inst) in sched.iter().enumerate() {
        assert_eq!(
            inst.status() == Status::Short,
            i < 3,
            "as withheld: {}",
            i + 1
        );
    }
}

/// The exceptions for last year, and what each leaves.
#[test]
fn estimated_exceptions() {
    let interest_only = |name: &str, h: &Household| {
        let res = penalty(h);
        assert!(
            flat_total(&res.value()).0 == 0 && res.value().total().0 != 0,
            "{name}: want interest alone"
        );
        res
    };

    // A filed 12-month return with no tax: the target is nothing.
    let h = Household {
        prior: filed(USCents(0), DOLLAR * 20_000),
        ..taxed(DOLLAR * 4_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(res.value().exempt() && !res.value().required());
    let c = res.value().chosen().unwrap();
    assert!(
        c.basis() == Basis::Exception && c.amount().0 == 0 && c.label() == "No tax due last year"
    );
    let p = penalty(&h);
    assert!(p.value().total().0 == 0 && p.value().reason() == "No tax due last year.");

    // No return required, or a short return with no tax: no penalty, and
    // interest is the open question, charged by default.
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(8)
        .figures(estimated::new_figures().tax(USCents(0)).build().unwrap())
        .build()
        .unwrap();
    for (name, p) in [
        ("not required to file", not_required),
        ("no tax on an 8-month return", short),
    ] {
        let res = interest_only(
            name,
            &Household {
                prior: Some(p),
                ..taxed(DOLLAR * 4_000)
            },
        );
        assert!(
            asked(
                &res.required(),
                PositionKind::UnsettledRule,
                "interest is still charged"
            ),
            "{name}"
        );
    }

    // MCL 205.24(6): no estimates required last year, as said, or worked out
    // from last year's $500.00 of tax with nothing withheld.
    let not_req = michigantaxcalc::new_estimated_facts()
        .prior_year_estimates_required(false)
        .build()
        .unwrap();
    interest_only(
        "said not required",
        &Household {
            extras: Some(not_req),
            ..taxed(DOLLAR * 4_000)
        },
    );
    let silent = michigantaxcalc::new_estimated_facts()
        .prior_year_married_filing_separately(false)
        .build()
        .unwrap();
    let last_year = |tax: i64| {
        Some(
            estimated::new_prior_return()
                .filed(true)
                .figures(
                    estimated::new_figures()
                        .tax(USCents(tax))
                        .federal_agi(DOLLAR * 50_000)
                        .withheld(USCents(0))
                        .build()
                        .unwrap(),
                )
                .build()
                .unwrap(),
        )
    };
    interest_only(
        "$500.00 last year",
        &Household {
            prior: last_year(50_000),
            extras: Some(silent),
            ..taxed(DOLLAR * 4_000)
        },
    );
    let p = penalty(&Household {
        prior: last_year(50_001),
        extras: Some(silent),
        ..taxed(DOLLAR * 4_000)
    });
    assert_ne!(flat_total(&p.value()).0, 0, "$500.01 last year: no penalty");

    // Not said and not worked out: asked, and the penalty is charged.
    let res2 = penalty(&Household {
        extras: Some(silent),
        ..taxed(DOLLAR * 4_000)
    });
    assert_ne!(flat_total(&res2.value()).0, 0);
    assert!(asked(
        &res2.required(),
        PositionKind::JurisdictionFact,
        "required to make estimated payments for 2025"
    ));
    assert!(has(&res2.required(), PositionKind::PenaltyWaiver));
}

/// Filing and paying by January 31 takes the penalty off the fourth quarter
/// and leaves its interest. January 31, 2027 is a Sunday, so February 1 is
/// in time, and that reading is asked about.
#[test]
fn estimated_file_and_pay_by_january_31() {
    let base = Household {
        payments: quarterly(&[1_000, 1_000, 1_000]),
        ..taxed(USCents(444_444))
    };
    for (filed, excused, was_asked) in [
        (day(2027, 1, 29), true, false),
        (day(2027, 2, 1), true, true),
        (day(2027, 2, 2), false, false),
    ] {
        let h = Household {
            return_filed: filed,
            ..base.clone()
        };
        let res = penalty(&h);
        let q4 = line(&res.value(), 4).unwrap();
        let (interest, flat) = split(&q4);
        assert!(
            flat.is_none() == excused && !interest.is_empty(),
            "filed {filed}"
        );
        assert_eq!(
            asked(&res.required(), PositionKind::UnsettledRule, "Feb 1, 2027"),
            was_asked,
            "filed {filed}"
        );
        assert_eq!(res.value().as_of(), filed);
    }
    // February 1: 17 days at 0.0002129 on $1,000, $3.62, so $4.
    let h = Household {
        return_filed: day(2027, 2, 1),
        ..base
    };
    let q4 = line(&penalty(&h).value(), 4).unwrap();
    let (interest, _) = split(&q4);
    assert!(interest.len() == 1 && interest[0].days() == 17 && interest[0].charge() == DOLLAR * 4);
}

/// A payment counts for the penalty only in the quarter it arrives. Nothing
/// by April 15 and $1,000 on May 1 is 25% on April's $1,000, $250, plus $4
/// of interest. Whether a late payment is 25% or 10% is asked.
#[test]
fn estimated_late_payment_counts_in_its_own_quarter() {
    let d = dues();
    let h = Household {
        payments: vec![
            pay(day(2026, 5, 1), DOLLAR * 1_000),
            pay(d[1], DOLLAR * 1_000),
            pay(d[2], DOLLAR * 1_000),
            pay(d[3], DOLLAR * 1_000),
        ],
        ..taxed(USCents(444_444))
    };
    let res = penalty(&h);
    assert!(res.value().total() == DOLLAR * 254 && res.value().lines().len() == 1);
    assert!(asked(
        &res.required(),
        PositionKind::UnsettledRule,
        "paying late"
    ));
}

/// A payment on January 16, 2027 is left out of the fourth quarter by
/// default, so the quarter has nothing paid: $250, and it is asked.
#[test]
fn estimated_january_sixteenth_payment_is_asked() {
    let d = dues();
    let mut h = Household {
        payments: vec![
            pay(d[0], DOLLAR * 1_000),
            pay(d[1], DOLLAR * 1_000),
            pay(d[2], DOLLAR * 1_000),
            pay(day(2027, 1, 16), DOLLAR * 1_000),
        ],
        ..taxed(USCents(444_444))
    };
    let res = penalty(&h);
    let flat = split(&line(&res.value(), 4).unwrap()).1.unwrap();
    assert_eq!(flat.charge(), DOLLAR * 250);
    assert!(asked(
        &res.required(),
        PositionKind::UnsettledRule,
        "Jan 16, 2027"
    ));
    // Paid on January 15, nothing is asked.
    h.payments[3] = pay(d[3], DOLLAR * 1_000);
    let res = penalty(&h);
    assert!(
        !asked(&res.required(), PositionKind::UnsettledRule, "Jan 16, 2027")
            && res.value().total().0 == 0
    );
}

/// A late start is reported; the MI-2210 still charges the earlier
/// quarters.
#[test]
fn estimated_late_start_is_asked() {
    let mut h = Household {
        first_met: day(2026, 7, 1),
        payments: quarterly(&[0, 0, 3_000, 1_000]),
        ..taxed(USCents(444_444))
    };
    let res = penalty(&h);
    assert!(asked(
        &res.required(),
        PositionKind::UnsettledRule,
        "pay first on Sep 15"
    ));
    assert!(line(&res.value(), 1).is_some(), "April is not charged");
    // The chart's rows start April 1, June 1 and September 1.
    for (met, first) in [
        (day(2026, 5, 31), "Jun 15."),
        (day(2026, 6, 1), "Sep 15."),
        (day(2026, 9, 1), "Jan 15, 2027."),
    ] {
        h.first_met = met;
        let res = penalty(&h);
        assert!(
            asked(
                &res.required(),
                PositionKind::UnsettledRule,
                &format!("pay first on {first}")
            ),
            "first met {met}"
        );
    }
    h.first_met = day(2026, 3, 31);
    let res = penalty(&h);
    assert!(!asked(
        &res.required(),
        PositionKind::UnsettledRule,
        "to pay first on"
    ));
}

/// Michigan has no standing waiver. A casualty or disaster is left open, and
/// the notices' reach is asked. Retirement and disability have no waiver,
/// even where the federal one applies.
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
    for (name, p, refused, want) in [
        (
            "disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), Date::default()),
            false,
            "granted relief for by notice",
        ),
        (
            "retired in 2026 at 64",
            pos(WaiverCause::Retirement, day(2026, 6, 1), day(1962, 3, 1)),
            true,
            "no waiver for retirement or disability",
        ),
        (
            "retired in 2020",
            pos(WaiverCause::Retirement, day(2020, 6, 1), day(1955, 3, 1)),
            true,
            "no waiver for retirement or disability",
        ),
    ] {
        let res = penalty(&Household {
            positions: Some(p),
            ..taxed(DOLLAR * 4_000)
        });
        let pen = res.value();
        let o = pen.waiver_outcome().unwrap();
        assert!(
            o.refused() == refused && o.undecided() != refused && o.reason().contains(want),
            "{name}: {:?}",
            o.reason()
        );
        assert!(
            pen.total().0 != 0 && pen.if_waived() == pen.total(),
            "{name}"
        );
        assert_eq!(
            has(&res.required(), PositionKind::UnsettledRule),
            !refused,
            "{name}"
        );
    }
}

/// When the $500 exception sets the target, last year's return can't change
/// it, so it isn't asked for.
#[test]
fn estimated_exempt_asks_nothing_of_the_target() {
    let res = rules().requirement(&taxed(DOLLAR * 400).facts()).unwrap();
    assert!(res.value().exempt() && !has(&res.required(), PositionKind::PriorYearReturn));
}

/// Each divergence is reported with both readings and both sources, and the
/// interest follows the printed factors.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules()
        .requirement(&taxed(DOLLAR * 10_000).facts())
        .unwrap();
    let want = [
        ("How interest accrues", "0.0002150", "MCL 205.23(2)"),
        (
            "Which flat penalty applies",
            "at least $25",
            "MCL 205.23(3), (4)",
        ),
        (
            "A return filed and paid by January 31, 2027",
            "by February 1, 2027",
            "MCL 8.6",
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some(w) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        // Nothing paid on $10,000: interest and a flat penalty on every
        // quarter, and no return filed on February 1.
        let affects = d.rule() != "A return filed and paid by January 31, 2027";
        assert!(
            d.affects_household() == affects && d.uses() == Reading::Form,
            "{:?}",
            d.rule()
        );
        assert!(
            d.form().contains(w.1)
                && d.statute_source().contains(w.2)
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{:?} does not pin both readings",
            d.rule()
        );
    }

    // 90% of $4,444,444.44 is $4,000,000, a quarter of which is $1,000,000.
    let p = penalty(&taxed(USCents(444_444_444))).value();
    let (interest, _) = split(&line(&p, 1).unwrap());
    assert!(
        interest.len() >= 2
            && interest[0].days() == 76
            && interest[0].charge() == DOLLAR * 17_662
            && interest[1].days() == 184
            && interest[1].charge() == DOLLAR * 39_560
    );
}

#[test]
fn estimated_refuses_what_is_not_built() {
    let Err(err) = michigantaxcalc::estimated_tax(2025) else {
        panic!("2025: rules returned");
    };
    assert!(err.is(&estimated::Error::NoRules));
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let base = || {
        let mut b = estimated::new_facts(MI, 2026);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    let sea = |r: i32| {
        michigantaxcalc::new_estimated_facts()
            .seafaring_share(Rate(r))
            .build()
            .unwrap()
    };
    for (name, f) in [
        ("a farmer", base().farm_share(Rate(6_667)).build().unwrap()),
        (
            "a fisherman",
            base().fish_share(Rate(6_667)).build().unwrap(),
        ),
        ("a seafarer", base().state(sea(6_667)).build().unwrap()),
        (
            "farming and seafaring",
            base()
                .farm_share(Rate(3_333))
                .state(sea(3_334))
                .build()
                .unwrap(),
        ),
        (
            "seafaring and fishing, 6666",
            base()
                .fish_share(Rate(3_333))
                .state(sea(3_333))
                .build()
                .unwrap(),
        ),
    ] {
        let err = rules().requirement(&f).err();
        let want = name != "seafaring and fishing, 6666";
        assert_eq!(
            err.is_some_and(|e| e.is(&estimated::Error::Unsupported)),
            want,
            "{name}"
        );
    }
    assert!(
        michigantaxcalc::new_estimated_facts()
            .seafaring_share(Rate(10_001))
            .build()
            .is_err()
    );
    let mut wrong_periods = base();
    for end in [
        day(2026, 3, 31),
        day(2026, 6, 30),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ] {
        wrong_periods.periods(&[estimated::new_period().end(end).build().unwrap()]);
    }
    wrong_periods.elections(estimated::new_elections().annualised().build().unwrap());
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
            estimated::new_facts(MI, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            base().state("not Michigan's").build().unwrap(),
        ),
        ("periods off by one", wrong_periods.build().unwrap()),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
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
        "$500",
        "10%",
        "25%",
        "$25",
        "$10",
        "only for the quarter in which it arrives",
        "110%",
        "$150,000",
        "no waiver for retirement or disability",
        "but not its interest",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
}

/// Without this year's tax, the target is last year's tax, and the penalty
/// is the most it can be.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: filed(DOLLAR * 4_000, DOLLAR * 100_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 4_000, "{}", r.why());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearTax);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [100_000, 200_000, 300_000, 400_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), USCents(want));
    }
    let pen = penalty(&h).value();
    assert!(pen.upper_bound() && pen.total().0 != 0);

    // No return to measure against: no target.
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
    let Err(err) = rules().penalty(&none.facts(), day(2027, 4, 15)) else {
        panic!("penalty accepted");
    };
    assert!(err.is(&estimated::Error::NeedsThisYearTax));

    // No 2025 liability on a full-year return needs nothing about this year.
    let zero = Household {
        no_tax: true,
        prior: filed(USCents(0), DOLLAR * 20_000),
        ..Household::default()
    };
    let p = penalty(&zero).value();
    assert!(p.total().0 == 0 && !p.upper_bound());
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
    let f = estimated::new_facts(MI, 2026)
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
