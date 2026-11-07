//! Nebraska's estimated tax rules for 2026, checked against the Department
//! of Revenue's own documents and the Nebraska Revised Statutes.
//!
//! Nebraska publishes no worked penalty example. Form 2210N defines the line
//! arithmetic, and these tests follow it: each column's underpayment, by the
//! day ÷ 365 at 8%, from its due date until paid or the next due date, and
//! the last column to April 15. The cases are hand-written. (The Go test
//! that compares each figure against its extract in testdata/ checks Go's
//! source documents, not code, and is not ported.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, Elections, EstimatedTax, Facts, Jurisdiction, Month, Payment, Penalty, Positions,
    PriorReturn, Status, WaiverCause, Weekday,
};
use money::{DOLLAR, Rate, USCents, dollars, percent};
use taxcalcs_nebraskataxcalc as nebraskataxcalc;
use working::{PositionKind, Reading, Required};

const NE: Jurisdiction = Jurisdiction::from_static("NE");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    nebraskataxcalc::estimated_tax(2026).unwrap()
}

fn because(rs: &[Required], k: PositionKind, phrase: &str) -> bool {
    rs.iter().any(|r| {
        r.position() == k
            && r.asked()
            && (r.question().contains(phrase) || r.effect().contains(phrase))
    })
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    because(rs, k, "")
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

#[derive(Clone, Default)]
struct Household {
    tax: USCents,
    no_tax: bool,
    status: filing::Status,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<nebraskataxcalc::EstimatedFacts>,
    return_filed: Date,
    elections: Option<Elections>,
    annualised: Option<Vec<USCents>>,
    farm_share: Rate,
    fish_share: Rate,
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
        let mut figures = estimated::new_figures().tax(self.tax).build().unwrap();
        if self.no_tax {
            figures = estimated::new_figures().build().unwrap();
        }
        let mut status = self.status;
        if status == filing::Status::Unspecified {
            status = filing::Status::Single;
        }
        let mut b = estimated::new_facts(NE, 2026);
        b.filing_status(status)
            .this_year(figures)
            .paid(&self.payments)
            .return_filed(self.return_filed)
            .farm_share(self.farm_share)
            .fish_share(self.fish_share);
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
        if let Some(e) = self.elections {
            b.elections(e);
        }
        if let Some(ann) = &self.annualised {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            let periods: Vec<_> = ann
                .iter()
                .enumerate()
                .map(|(i, tax)| {
                    estimated::new_period()
                        .end(ends[i])
                        .annualised_tax(*tax)
                        .build()
                        .unwrap()
                })
                .collect();
            b.periods(&periods);
            b.elections(estimated::new_elections().annualised().build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

fn prior(tax: USCents, agi: USCents) -> Option<PriorReturn> {
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

fn penalty(h: &Household) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap()
}

/// The printed dates, 25% each, and none moves.
#[test]
fn estimated_schedule_for_twenty_twenty_six() {
    let s = rules()
        .schedule(&taxed(DOLLAR * 10_000).facts())
        .unwrap()
        .value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.due(), dues()[i], "installment {}", i + 1);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
        assert_eq!(inst.needed_by(), DOLLAR * 2_250 * (i as i64 + 1));
    }
}

/// January 31, 2027 is a Sunday and Form 2210N does not say whether it
/// moves. Filed by it, the balance counts as paid on January 15. Filed on
/// Monday, February 1, it does not by default, and the question is asked;
/// the answer moves it.
#[test]
fn estimated_january_thirty_first() {
    let res = penalty(&Household {
        return_filed: day(2027, 1, 29),
        ..taxed(DOLLAR * 10_000)
    });
    assert_eq!(res.value().as_of(), day(2027, 1, 15));
    assert_eq!(res.value().total(), USCents(30_131));
    let res = penalty(&Household {
        return_filed: day(2027, 2, 1),
        ..taxed(DOLLAR * 10_000)
    });
    assert_eq!(res.value().as_of(), day(2027, 2, 1));
    assert!(because(
        &res.required(),
        PositionKind::UnsettledRule,
        "a Sunday"
    ));
    let moves = nebraskataxcalc::new_estimated_facts()
        .january_thirty_first_moves(true)
        .build()
        .unwrap();
    let res = penalty(&Household {
        return_filed: day(2027, 2, 1),
        extras: Some(moves),
        ..taxed(DOLLAR * 10_000)
    });
    assert_eq!(res.value().as_of(), day(2027, 1, 15));
    assert!(!because(
        &res.required(),
        PositionKind::UnsettledRule,
        "a Sunday"
    ));
    let s = rules()
        .schedule(
            &Household {
                return_filed: day(2027, 2, 2),
                extras: Some(moves),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    assert_ne!(
        s[3].status(),
        Status::Excused,
        "filed February 2: January is excused"
    );
}

/// Form 2210N line 6: $500 or more after withholding.
#[test]
fn estimated_threshold_boundary() {
    for (withheld, exempt) in [(USCents(950_001), true), (USCents(950_000), false)] {
        let res = rules()
            .requirement(
                &Household {
                    withheld,
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {withheld}");
    }
    // Under the threshold the exception is the chosen option, at $0.
    let h = Household {
        withheld: USCents(960_000),
        ..taxed(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let c = res.value().chosen().unwrap();
    assert!(
        c.basis() == Basis::Exception
            && c.amount().0 == 0
            && c.label() == "Tax less withholding under $500"
    );
    assert!(
        res.value().why().contains("leaves $400, under $500"),
        "why {:?}",
        res.value().why()
    );
    assert_eq!(
        penalty(&h).value().reason(),
        "Tax less withholding under $500."
    );
    // $400 is under the form's $500 but not the regulation's $300, so the
    // threshold divergence affects this household.
    for d in res.divergences() {
        if d.rule() == "The $500 threshold" {
            assert!(d.affects_household());
        }
    }
}

/// 100% of last year's tax, 110% above $150,000 of last year's federal AGI
/// ($75,000 married filing separately this year).
#[test]
fn estimated_prior_year_step() {
    for (status, agi, want) in [
        (filing::Status::Single, USCents(15_000_000), DOLLAR * 8_000),
        (filing::Status::Single, USCents(15_000_001), DOLLAR * 8_800),
        (
            filing::Status::MarriedFilingSeparately,
            USCents(7_500_000),
            DOLLAR * 8_000,
        ),
        (
            filing::Status::MarriedFilingSeparately,
            USCents(7_500_001),
            DOLLAR * 8_800,
        ),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    status,
                    prior: prior(DOLLAR * 8_000, agi),
                    ..taxed(DOLLAR * 20_000)
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(res.value().target(), want, "{status:?}, AGI {agi}");
    }
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
    let res = rules()
        .requirement(
            &Household {
                prior: Some(no_agi),
                ..taxed(DOLLAR * 20_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_800);
    assert!(has(&res.required(), PositionKind::AdjustedGrossIncome));
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(
            estimated::new_figures()
                .tax(USCents(100))
                .federal_agi(USCents(1))
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    for (name, p) in [
        ("none on record", None),
        ("not filed", Some(not_filed)),
        ("eleven months", Some(short)),
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
        assert!(
            !res.value().options()[1].available(),
            "{name}: the prior-year test is offered"
        );
    }
}

/// Nothing paid on $9,000 required: each column's underpayment to the next
/// due date at 8%. September's column spans the 2026 and 2027 rate periods;
/// Revenue Ruling 99-26-2 keeps 8% in both, so the boundary splits the
/// charge without changing the rate.
#[test]
fn estimated_penalty_arithmetic() {
    let res = penalty(&taxed(DOLLAR * 10_000));
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    let want: [(i64, &[i64], &[i64]); 4] = [
        (225_000, &[61], &[3_008]),
        (450_000, &[92], &[9_074]),
        (675_000, &[107, 15], &[15_830, 2_219]),
        (900_000, &[90], &[17_753]),
    ];
    for (i, l) in lines.iter().enumerate() {
        let parts = l.parts();
        assert_eq!(l.shortfall(), USCents(want[i].0), "column {}", i + 1);
        assert_eq!(parts.len(), want[i].1.len(), "column {}", i + 1);
        for (j, p) in parts.iter().enumerate() {
            assert!(
                p.days() == want[i].1[j]
                    && p.charge() == USCents(want[i].2[j])
                    && p.rate() == percent(8),
                "column {} part {j}: {} days {} at {}",
                i + 1,
                p.days(),
                p.charge(),
                p.rate()
            );
        }
    }
    assert!(lines[2].parts()[1].source().contains("2027"));
    assert_eq!(res.value().total(), USCents(47_884));
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));
    let p = rules()
        .penalty(&taxed(DOLLAR * 10_000).facts(), day(2027, 7, 1))
        .unwrap();
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
}

/// A payment between due dates stops the charge on what it pays: April's
/// $2,250 paid April 30 runs 15 days, $7.40.
#[test]
fn estimated_payment_between_due_dates() {
    let d = dues();
    let h = Household {
        payments: vec![
            pay(day(2026, 4, 30), DOLLAR * 2_250),
            pay(d[1], DOLLAR * 2_250),
            pay(d[2], DOLLAR * 2_250),
            pay(d[3], DOLLAR * 2_250),
        ],
        ..taxed(DOLLAR * 10_000)
    };
    let res = penalty(&h);
    let l = res.value().lines();
    assert!(l.len() == 1 && l[0].days() == 15 && res.value().total() == USCents(740));
}

/// Withholding counts a quarter on each date unless the filer shows the
/// dates.
#[test]
fn estimated_withholding() {
    let p = penalty(&Household {
        withheld: DOLLAR * 9_000,
        ..taxed(DOLLAR * 10_000)
    });
    assert_eq!(p.value().total().0, 0, "spread evenly");
    let as_withheld = estimated::new_elections()
        .withholding_as_withheld()
        .build()
        .unwrap();
    let p = penalty(&Household {
        withheld: DOLLAR * 9_000,
        elections: Some(as_withheld),
        ..taxed(DOLLAR * 10_000)
    });
    assert_ne!(p.value().total().0, 0, "on its December date: no penalty");
}

/// The exceptions: no liability last year as a U.S. citizen or resident,
/// and a first-time filer.
#[test]
fn estimated_exceptions() {
    let citizen = nebraskataxcalc::new_estimated_facts()
        .prior_year_us_citizen_or_resident(true)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                prior: prior(USCents(0), DOLLAR * 10_000),
                extras: Some(citizen),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(
        res.value().exempt(),
        "no liability as a citizen: {}",
        res.value().why()
    );
    let res = rules()
        .requirement(
            &Household {
                prior: prior(USCents(0), DOLLAR * 10_000),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(!res.value().exempt());
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "U.S. citizen or resident"
    ));
    let first = nebraskataxcalc::new_estimated_facts()
        .first_time_filer(true)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                extras: Some(first),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().exempt());
    assert!(because(
        &res.required(),
        PositionKind::UnsettledRule,
        "first-time filer"
    ));
}

/// Farmers, ranchers and fishermen in either year are under rules of their
/// own.
#[test]
fn estimated_farmers_are_not_built() {
    for (name, h) in [
        (
            "farmer",
            Household {
                farm_share: Rate(7000),
                ..taxed(DOLLAR * 10_000)
            },
        ),
        (
            "fisher",
            Household {
                fish_share: Rate(7000),
                ..taxed(DOLLAR * 10_000)
            },
        ),
    ] {
        let Err(err) = rules().requirement(&h.facts()) else {
            panic!("{name}: accepted");
        };
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    let last = nebraskataxcalc::new_estimated_facts()
        .farmer_last_year(true)
        .build()
        .unwrap();
    let Err(err) = rules().requirement(
        &Household {
            extras: Some(last),
            ..taxed(DOLLAR * 10_000)
        }
        .facts(),
    ) else {
        panic!("a farmer last year: accepted");
    };
    assert!(err.is(&estimated::Error::Unsupported));
    let res = rules()
        .requirement(
            &Household {
                farm_share: Rate(1000),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "farmers' rules"
    ));
}

/// The annualized method, figured as on the federal schedule.
#[test]
fn estimated_annualized_method() {
    let h = Household {
        annualised: Some(vec![
            DOLLAR * 2_000,
            DOLLAR * 4_000,
            DOLLAR * 8_000,
            DOLLAR * 10_000,
        ]),
        ..taxed(DOLLAR * 10_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [DOLLAR * 450, DOLLAR * 1_800, DOLLAR * 5_400, DOLLAR * 9_000]
        .into_iter()
        .enumerate()
    {
        assert_eq!(s[i].needed_by(), want, "installment {}", i + 1);
    }
}

/// Without this year's tax, the target is last year's alone and the penalty
/// is the most it can be.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000, DOLLAR * 100_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(
        r.required()
            && r.target() == DOLLAR * 8_000
            && r.options()[0].why_not() == estimated::NEEDS_THIS_YEAR_TAX
            && has(&res.required(), PositionKind::ThisYearTax)
    );
    let p = penalty(&h);
    assert!(p.value().upper_bound() && p.value().total().0 != 0);
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let Err(err) = rules().schedule(&none.facts()) else {
        panic!("schedule accepted");
    };
    assert!(
        err.is(&estimated::Error::NeedsThisYearTax),
        "schedule: {err}"
    );
    let Err(err) = rules().penalty(&none.facts(), day(2027, 4, 15)) else {
        panic!("penalty accepted");
    };
    assert!(
        err.is(&estimated::Error::NeedsThisYearTax),
        "penalty: {err}"
    );
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules()
        .requirement(&taxed(DOLLAR * 10_000).facts())
        .unwrap();
    let want = [
        ("The $500 threshold", "under $500", "REG-20-001.01"),
        (
            "No liability last year",
            "U.S. citizenship",
            "REG-20-007.06A",
        ),
        ("First-time filers", "first-time filer", "REG-20-007.06"),
        (
            "A requirement that arises late",
            "25% in every column",
            "REG-20-007.01",
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some(w) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(
            d.form().contains(w.1)
                && d.statute_source().contains(w.2)
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{:?} does not pin both readings",
            d.rule()
        );
        // The household claims no exception and starts on time, so neither
        // reading changes its figures.
        assert!(
            d.uses() == Reading::Form && !d.affects_household(),
            "{:?}",
            d.rule()
        );
    }
    let first = nebraskataxcalc::new_estimated_facts()
        .first_time_filer(true)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                extras: Some(first),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    for d in res.divergences() {
        assert_eq!(
            d.affects_household(),
            d.rule() == "First-time filers",
            "first-time filer: {:?}",
            d.rule()
        );
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    let Err(err) = nebraskataxcalc::estimated_tax(2025) else {
        panic!("2025: rules returned");
    };
    assert!(err.is(&estimated::Error::NoRules));
    let this = estimated::new_figures()
        .tax(USCents(100_000))
        .build()
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
            estimated::new_facts(NE, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(NE, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state(true)
                .build()
                .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&taxed(USCents(100)).facts(), Date::default())
            .is_err()
    );
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
        "$500",
        "8%",
        "110%",
        "$150,000",
        "first Nebraska return",
        "January 15",
        "every other year",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
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
    let f = estimated::new_facts(NE, 2026)
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

fn waived(pos: Positions) -> Penalty {
    rules()
        .penalty(
            &Household {
                positions: Some(pos),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap()
        .value()
}

fn retired_on(on: Date, born: Date) -> Positions {
    estimated::new_positions()
        .waiver_for(WaiverCause::Retirement, on)
        .born(born)
        .build()
        .unwrap()
}

/// Nebraska grants the federal waivers, with the same window and age.
#[test]
fn estimated_waiver_follows_the_federal_one() {
    let p = waived(retired_on(day(2026, 6, 1), day(1962, 3, 1)));
    let o = p.waiver_outcome().unwrap();
    assert!(o.eligible() && p.total() == USCents(47_884) && p.if_waived().0 == 0);
    let p = waived(retired_on(day(2020, 6, 1), day(1955, 3, 1)));
    let o = p.waiver_outcome().unwrap();
    assert!(o.refused() && o.reason().contains("2025 or 2026") && p.if_waived() == p.total());
    let p = waived(
        estimated::new_positions()
            .waiver_for(WaiverCause::Disability, day(2025, 9, 1))
            .build()
            .unwrap(),
    );
    let o = p.waiver_outcome().unwrap();
    assert!(o.eligible() && o.reason().contains("reasonable cause"));
    let p = waived(
        estimated::new_positions()
            .waiver_for(WaiverCause::Casualty, day(2026, 8, 1))
            .build()
            .unwrap(),
    );
    let o = p.waiver_outcome().unwrap();
    assert!(o.eligible() && o.reason().contains("Nebraska's Department of Revenue"));
}

/// Under an exception no question may say its answer changes the target, so
/// last year's return isn't asked for.
#[test]
fn estimated_exempt_asks_nothing_of_the_target() {
    let res = rules().requirement(&taxed(DOLLAR * 100).facts()).unwrap();
    assert!(res.value().exempt(), "$100 of tax is not exempt");
    assert!(!has(&res.required(), PositionKind::PriorYearReturn));
    for q in res.required() {
        assert!(
            !q.effect().to_lowercase().contains("target"),
            "under an exception a question says {:?}",
            q.effect()
        );
    }
}
