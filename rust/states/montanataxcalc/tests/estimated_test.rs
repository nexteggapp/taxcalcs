//! Montana's estimated tax rules for 2026. Ported from estimated_test.go.
//!
//! Go's TestEstimatedFiguresAreInTheirSources and the extract checks at the
//! top of the form-example and waiver tests read testdata/ and are not
//! ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, Payment, Penalty, Person, Positions,
    PriorReturn, ResidencePeriod, Status, WaiverCause, Weekday,
};
use filing::Status as FilingStatus;
use money::{DOLLAR, Rate, USCents, dollars, percent};
use taxcalcs_montanataxcalc::{EstimatedFacts, estimated_tax, new_estimated_facts};
use working::{PositionKind, Reading, Required};

const MT: Jurisdiction = Jurisdiction::from_static("MT");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
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
    withheld: USCents,
    refundable: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    people: Vec<Person>,
    residence: Vec<ResidencePeriod>,
    first_met: Date,
    return_filed: Date,
    farm_share: Rate,
    fish_share: Rate,
    annualised: Option<Vec<USCents>>,
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
        let figures = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(MT, 2026);
        b.filing_status(FilingStatus::Single)
            .this_year(figures)
            .paid(&self.payments)
            .people(&self.people)
            .residence(&self.residence)
            .requirement_first_met(self.first_met)
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
        match self.extras {
            Some(e) => b.state(e),
            None => b.state(
                new_estimated_facts()
                    .refundable_credits(self.refundable)
                    .composite_return(false)
                    .build()
                    .unwrap(),
            ),
        };
        if let Some(ann) = &self.annualised {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            for (i, t) in ann.iter().enumerate() {
                b.periods(&[estimated::new_period()
                    .end(ends[i])
                    .annualised_tax(*t)
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

fn prior(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .build()
            .unwrap(),
    )
}

fn penalty(h: &Household) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap()
}

/// EST-I's Example 3 with the 2026 dates: $4,000 required each quarter;
/// $2,000 paid April 30, $3,000 June 15, $4,000 September 15, $4,000 January
/// 15 and $3,000 April 15, 2027.
#[test]
fn estimated_interest_follows_the_form_example() {
    let d = dues();
    let h = Household {
        prior: prior(DOLLAR * 16_000),
        payments: vec![
            pay(day(2026, 4, 30), DOLLAR * 2_000),
            pay(d[1], DOLLAR * 3_000),
            pay(d[2], DOLLAR * 4_000),
            pay(d[3], DOLLAR * 4_000),
            pay(day(2027, 4, 15), DOLLAR * 3_000),
        ],
        ..tax(DOLLAR * 20_000)
    };
    let res = penalty(&h);
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    let want: [(i64, &[i64], &[i64]); 4] = [
        (400_000, &[15, 61], &[576, 2_342]),
        (300_000, &[92], &[5_299]),
        (300_000, &[122], &[7_027]),
        (300_000, &[90], &[5_184]),
    ];
    for (i, l) in lines.iter().enumerate() {
        let parts = l.parts();
        assert_eq!(l.shortfall(), USCents(want[i].0), "column {}", i + 1);
        assert_eq!(parts.len(), want[i].1.len(), "column {}", i + 1);
        for (j, p) in parts.iter().enumerate() {
            assert_eq!(p.days(), want[i].1[j], "column {} part {j}", i + 1);
            assert_eq!(
                p.charge(),
                USCents(want[i].2[j]),
                "column {} part {j}",
                i + 1
            );
        }
    }
    assert_eq!(res.value().total(), USCents(20_428));
}

/// Nothing paid on $9,000 required: each quarter's $2,250 to April 15, 2027
/// at 0.000192 a day, at 2027's rate for the whole year.
#[test]
fn estimated_interest_arithmetic() {
    let res = penalty(&ten());
    let want_days = [365, 304, 212, 90];
    let want_charge = [15_768, 13_133, 9_158, 3_888];
    for (i, l) in res.value().lines().iter().enumerate() {
        let parts = l.parts();
        assert_eq!(parts.len(), 1, "column {}", i + 1);
        assert_eq!(parts[0].days(), want_days[i]);
        assert_eq!(parts[0].charge(), USCents(want_charge[i]));
        assert_eq!(parts[0].rate(), percent(7));
        assert!(parts[0].source().contains("2027"));
    }
    assert_eq!(res.value().total(), USCents(41_947));
    assert!(because(
        &res.required(),
        PositionKind::UnpublishedRate,
        "third quarter of 2026, 7%"
    ));
    let p = rules().penalty(&ten().facts(), day(2027, 6, 1)).unwrap();
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
}

/// The 2026 dates are the statute's and none moves. January 31, 2027 is a
/// Sunday, so § 1-1-307 moves the January rule to Monday, February 1.
#[test]
fn estimated_schedule_and_the_january_rule() {
    let s = rules().schedule(&ten().facts()).unwrap().value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    for (filed, excused) in [
        (day(2027, 1, 31), true),
        (day(2027, 2, 1), true),
        (day(2027, 2, 2), false),
    ] {
        let s = rules()
            .schedule(
                &Household {
                    return_filed: filed,
                    ..ten()
                }
                .facts(),
            )
            .unwrap()
            .value();
        assert_eq!(s[3].status() == Status::Excused, excused, "filed {filed}");
    }
    // Filed and paid February 1: the first three quarters run to that day.
    let res = penalty(&Household {
        return_filed: day(2027, 2, 1),
        ..ten()
    });
    assert_eq!(res.value().as_of(), day(2027, 2, 1));
    assert_eq!(res.value().lines().len(), 3);
}

/// $500 or more after withholding and refundable credits.
#[test]
fn estimated_threshold_boundary() {
    for (withheld, refundable, exempt) in [
        (950_001, 0, true),
        (950_000, 0, false),
        (900_000, 50_001, true),
        (900_000, 50_000, false),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    withheld: USCents(withheld),
                    refundable: USCents(refundable),
                    ..ten()
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "withheld {withheld}, refundable {refundable}"
        );
    }
}

/// The lesser of 90% of this year's tax and 100% of last year's, which needs
/// a filed twelve-month return.
#[test]
fn estimated_target_options() {
    let res = rules()
        .requirement(
            &Household {
                prior: prior(DOLLAR * 8_000),
                ..ten()
            }
            .facts(),
        )
        .unwrap();
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::PriorYearTax);
    assert_eq!(c.percent(), percent(100));
    assert_eq!(res.value().target(), DOLLAR * 8_000);
    let res = rules()
        .requirement(
            &Household {
                prior: prior(DOLLAR * 9_500),
                ..ten()
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().chosen().unwrap().basis(), Basis::ThisYearTax);
    assert_eq!(res.value().target(), DOLLAR * 9_000);
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .build()
        .unwrap();
    for (name, p) in [
        ("none on record", None),
        ("not filed", Some(not_filed)),
        ("eleven months", Some(short)),
    ] {
        let res = rules()
            .requirement(&Household { prior: p, ..ten() }.facts())
            .unwrap();
        assert!(!res.value().options()[1].available(), "{name}");
    }
}

/// The exemptions: farmers and ranchers, retirement after 62 or disability,
/// a composite return, no liability last year, and a nonresident not
/// required to file last year.
#[test]
fn estimated_exemptions() {
    let retired = estimated::new_person(64)
        .retired_after_62()
        .build()
        .unwrap();
    let disabled = estimated::new_person(40).disabled().build().unwrap();
    let composite = new_estimated_facts()
        .refundable_credits(USCents(0))
        .composite_return(true)
        .build()
        .unwrap();
    let nr = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let part_year = estimated::new_residence_period("MT")
        .from(day(2026, 6, 1))
        .to(day(2026, 12, 31))
        .build()
        .unwrap();
    let full_year = estimated::new_residence_period("MT")
        .from(day(2026, 1, 1))
        .to(day(2026, 12, 31))
        .build()
        .unwrap();
    for (name, h, exempt) in [
        (
            "farmer",
            Household {
                farm_share: Rate(6_667),
                ..ten()
            },
            true,
        ),
        (
            "farm share under two-thirds",
            Household {
                farm_share: Rate(6_666),
                ..ten()
            },
            false,
        ),
        (
            "fisher",
            Household {
                fish_share: Rate(9_000),
                ..ten()
            },
            false,
        ),
        (
            "retired after 62",
            Household {
                people: vec![retired],
                ..ten()
            },
            true,
        ),
        (
            "disabled",
            Household {
                people: vec![disabled],
                ..ten()
            },
            true,
        ),
        (
            "composite return",
            Household {
                extras: Some(composite),
                ..ten()
            },
            true,
        ),
        (
            "no liability last year",
            Household {
                prior: prior(USCents(0)),
                ..ten()
            },
            true,
        ),
        (
            "part-year, not required",
            Household {
                prior: Some(nr),
                residence: vec![part_year.clone()],
                ..ten()
            },
            true,
        ),
        (
            "full-year resident not required",
            Household {
                prior: Some(nr),
                residence: vec![full_year.clone()],
                ..ten()
            },
            true,
        ),
    ] {
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "{name}: {}",
            res.value().why()
        );
    }
    // The threshold exception is the chosen option, at $0, with its figures.
    let res = rules()
        .requirement(
            &Household {
                withheld: USCents(960_000),
                ..ten()
            }
            .facts(),
        )
        .unwrap();
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::Exception);
    assert_eq!(c.amount(), USCents(0));
    assert_eq!(
        c.label(),
        "Tax less withholding and refundable credits under $500"
    );
    assert!(res.value().why().contains("leaves $400, under $500"));
    let farmer = Household {
        farm_share: Rate(6_667),
        ..ten()
    };
    let res = rules().requirement(&farmer.facts()).unwrap();
    assert!(res.value().why().contains("farming or ranching"));
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::Exception);
    assert_eq!(
        c.label(),
        "Two-thirds or more of your income from farming or ranching"
    );
    assert_eq!(
        penalty(&farmer).value().reason(),
        "Two-thirds or more of your income from farming or ranching."
    );
    // Not required to file and no residence given: the no-liability test
    // applies to a twelve-month year, and residence is not needed.
    let not_required_short = estimated::new_prior_return()
        .not_required_to_file()
        .months(6)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                prior: Some(not_required_short),
                ..ten()
            }
            .facts(),
        )
        .unwrap();
    assert!(!res.value().exempt());
    assert!(has(&res.required(), PositionKind::Residency));
}

/// A requirement that arises after April 15 catches up at June 15 with 50%,
/// then 25% and 25%. One that arises on April 15 itself is not after it.
#[test]
fn estimated_late_starter() {
    let with_met = |d: Date| {
        rules()
            .schedule(
                &Household {
                    first_met: d,
                    ..ten()
                }
                .facts(),
            )
            .unwrap()
            .value()
    };
    let s = with_met(day(2026, 6, 1));
    for (i, want) in [0, 4_500, 6_750, 9_000].iter().enumerate() {
        assert_eq!(s[i].needed_by(), DOLLAR * *want);
    }
    let s = with_met(day(2026, 7, 1));
    assert_eq!(s[1].needed_by(), USCents(0));
    assert_eq!(s[2].needed_by(), DOLLAR * 6_750);
    let s = with_met(day(2026, 4, 15));
    assert_eq!(s[0].needed_by(), DOLLAR * 2_250);
    assert!(has(
        &penalty(&ten()).required(),
        PositionKind::RequirementFirstMet
    ));
}

/// The annualized method: 22.5%, 45%, 67.5% and 90%, with recapture.
#[test]
fn estimated_annualized_method() {
    let h = Household {
        annualised: Some(vec![
            DOLLAR * 2_000,
            DOLLAR * 4_000,
            DOLLAR * 8_000,
            DOLLAR * 10_000,
        ]),
        ..ten()
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [450, 1_800, 5_400, 9_000].iter().enumerate() {
        assert_eq!(s[i].needed_by(), DOLLAR * *want);
    }
}

/// § 15-30-2512(5)(e): payments reaching the target in approximately equal
/// installments owe nothing. EST-I omits it, so it is asked and applied only
/// when answered.
#[test]
fn estimated_approximately_equal_installments() {
    let late = vec![
        pay(day(2026, 4, 25), DOLLAR * 2_250),
        pay(day(2026, 6, 25), DOLLAR * 2_250),
        pay(day(2026, 9, 25), DOLLAR * 2_250),
        pay(day(2027, 1, 25), DOLLAR * 2_250),
    ];
    let res = penalty(&Household {
        payments: late.clone(),
        ..ten()
    });
    assert_ne!(res.value().total(), USCents(0));
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "approximately equal"
    ));
    let e = new_estimated_facts()
        .refundable_credits(USCents(0))
        .composite_return(false)
        .approximately_equal_installments(true)
        .build()
        .unwrap();
    let p = penalty(&Household {
        payments: late.clone(),
        extras: Some(e),
        ..ten()
    });
    assert_eq!(p.value().total(), USCents(0));
    // The answer does not help when the year's payments fall short.
    let p = penalty(&Household {
        payments: late[..3].to_vec(),
        extras: Some(e),
        ..ten()
    });
    assert_ne!(p.value().total(), USCents(0));
}

/// The open questions a non-zero interest carries.
#[test]
fn estimated_open_questions() {
    let res = penalty(&ten());
    assert!(because(
        &res.required(),
        PositionKind::UnsettledRule,
        "1.5% a month"
    ));
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "retire after reaching 62"
    ));
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));
    let none = new_estimated_facts().build().unwrap();
    let res = penalty(&Household {
        extras: Some(none),
        ..ten()
    });
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "composite return"
    ));
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "refundable credits"
    ));
}

/// Without this year's tax, the target is last year's alone and the interest
/// is the most it can be.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required());
    assert_eq!(r.target(), DOLLAR * 8_000);
    assert_eq!(r.options()[0].why_not(), estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let p = penalty(&h);
    assert!(p.value().upper_bound());
    assert_ne!(p.value().total(), USCents(0));
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
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

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&ten().facts()).unwrap();
    let want = [
        ("The daily factor", "0.000192", "§ 15-1-216(7)(a)(i)"),
        (
            "No liability last year",
            "was zero",
            "§ 15-30-2512(1)(b)(ii)",
        ),
        (
            "Not required to file last year",
            "nonresident or part-year resident",
            "ARM 42.17.314(1)",
        ),
        (
            "When payments are required",
            "more than $500",
            "§ 15-30-2512(1)(b)(i)",
        ),
        (
            "Retirement after 62",
            "at the end of the previous tax year",
            "§ 15-30-2512(1)(b)(iv)",
        ),
        (
            "Approximately equal installments",
            "no exception",
            "§ 15-30-2512(5)(e)",
        ),
        (
            "The late payment penalty",
            "interest only",
            "§ 15-1-216(2)(b)",
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some(w) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {}", d.rule());
        };
        assert!(
            d.form().contains(w.1)
                && d.statute_source().contains(w.2)
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{} does not pin both readings",
            d.rule()
        );
        let want_uses = if d.rule() == "Retirement after 62" {
            Reading::Statute
        } else {
            Reading::Form
        };
        let want_affects = d.rule() == "The daily factor" || d.rule() == "The late payment penalty";
        assert_eq!(d.uses(), want_uses, "{}", d.rule());
        assert_eq!(d.affects_household(), want_affects, "{}", d.rule());
    }
    // Exactly $500 after withholding: only the readings of the threshold
    // differ.
    let res = rules()
        .requirement(
            &Household {
                withheld: USCents(950_000),
                ..ten()
            }
            .facts(),
        )
        .unwrap();
    for d in res.divergences() {
        if d.rule() == "When payments are required" {
            assert!(d.affects_household());
        }
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    assert!(
        estimated_tax(2025)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NoRules))
    );
    let this = estimated::new_figures()
        .tax(USCents(100_000))
        .build()
        .unwrap();
    let other = estimated::new_facts(MT, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(this)
        .state(3_i64)
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
            estimated::new_facts(MT, 2027)
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
        "montanataxcalc: the facts carry int, not montanataxcalc.EstimatedFacts"
    );
    assert!(
        rules()
            .penalty(&tax(USCents(100)).facts(), Date::default())
            .is_err()
    );
    assert!(
        new_estimated_facts()
            .refundable_credits(USCents(-1))
            .build()
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
        "110%",
        "retired after 62",
        "2027 rate",
        "February 1",
    ] {
        assert!(text.contains(s), "{s}");
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
    let f = estimated::new_facts(MT, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(figures(3_000, 60_000))
        .prior_year(prior)
        .withheld(&withheld)
        .build()
        .unwrap();
    let got = rules().requirement(&f).unwrap().value().why();
    assert!(got.contains("a $600 refund") && !got.contains("−$") && !got.contains("-$"));
}

fn waived(pos: Positions) -> Penalty {
    penalty(&Household {
        positions: Some(pos),
        ..ten()
    })
    .value()
}

fn retired_on(on: Date, born: Date) -> Positions {
    estimated::new_positions()
        .waiver_for(WaiverCause::Retirement, on)
        .born(born)
        .build()
        .unwrap()
}

/// Montana's window and age are the federal ones, but retiring after 62 or
/// becoming disabled needs no reasonable cause, and a casualty needs good
/// cause.
#[test]
fn estimated_waiver_differs_from_the_federal_one() {
    let p = waived(retired_on(day(2025, 6, 1), day(1962, 3, 1)));
    let o = p.waiver_outcome().unwrap();
    assert!(o.eligible());
    assert!(!o.reason().contains("reasonable cause"));
    assert!(o.reason().contains("the year after"));
    assert_ne!(p.total(), USCents(0));
    assert_eq!(p.if_waived(), USCents(0));
    let p = waived(retired_on(day(2020, 6, 1), day(1955, 3, 1)));
    let o = p.waiver_outcome().unwrap();
    assert!(o.refused() && o.reason().contains("2025 or 2026"));
    assert_eq!(p.if_waived(), p.total());
    let p = waived(retired_on(day(2026, 6, 1), day(1970, 3, 1)));
    assert!(p.waiver_outcome().unwrap().refused());
    let p = waived(
        estimated::new_positions()
            .waiver_for(WaiverCause::Disability, day(2026, 2, 1))
            .build()
            .unwrap(),
    );
    let o = p.waiver_outcome().unwrap();
    assert!(o.eligible() && !o.reason().contains("reasonable cause"));
    let p = waived(
        estimated::new_positions()
            .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
            .build()
            .unwrap(),
    );
    let o = p.waiver_outcome().unwrap();
    assert!(o.eligible() && o.reason().contains("good cause"));
}

/// Under an exception no question may say its answer changes the target, so
/// last year's return isn't asked for.
#[test]
fn estimated_exempt_asks_nothing_of_the_target() {
    let res = rules().requirement(&tax(DOLLAR * 100).facts()).unwrap();
    assert!(res.value().exempt());
    assert!(!has(&res.required(), PositionKind::PriorYearReturn));
    for q in res.required() {
        assert!(
            !q.effect().to_lowercase().contains("target"),
            "{}",
            q.effect()
        );
    }
}
