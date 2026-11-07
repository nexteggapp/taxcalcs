//! North Carolina's estimated tax rules for 2026, checked against the
//! Department of Revenue's own documents and the General Statutes. Ported
//! from estimated_test.go.
//!
//! North Carolina publishes no worked interest example. Form D-422 defines
//! the line arithmetic, and these tests follow it: each part of each
//! shortfall, by the day ÷ 365, at the rate, from its due date until paid or
//! April 15. The cases are hand-written.
//!
//! Go's TestEstimatedFiguresAreInTheirSources, which compares each figure
//! against its extract in testdata/, is not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, Elections, EstimatedTax, Facts, Jurisdiction, Month, Payment, Penalty, Positions,
    PositionsBuilder, PriorReturn, Status, WaiverCause, WaiverKind, Weekday,
};
use filing::Status as FilingStatus;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_northcarolinataxcalc::{EstimatedFacts, estimated_tax, new_estimated_facts};
use working::{PositionKind, Reading, Required};

const NC: Jurisdiction = Jurisdiction::from_static("NC");

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
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    return_filed: Date,
    elections: Option<Elections>,
    annualised: Option<Vec<USCents>>,
    farm_share: Rate,
    fish_share: Rate,
    positions: Option<Positions>,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let figures = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(NC, 2026);
        b.filing_status(FilingStatus::Single)
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

fn prior(tax: USCents, months: i64) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .months(months)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .build()
            .unwrap(),
    )
}

fn penalty(h: &Household) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap()
}

/// Nothing paid on $9,000 required: each quarter's $2,250 at 7% by the day
/// to December 31, 260, 199 and 107 days. North Carolina's 7% covers all of
/// 2026; the boundary is January 1, 2027, whose rate is not published, and
/// the days after it are not charged.
#[test]
fn estimated_interest_arithmetic() {
    let res = penalty(&tax(DOLLAR * 10_000));
    let want_days = [260, 199, 107];
    let want_charge = [USCents(11_219), USCents(8_587), USCents(4_617)];
    let lines = res.value().lines();
    for (i, w) in want_days.iter().enumerate() {
        let parts = lines[i].parts();
        assert_eq!(parts.len(), 1, "installment {}", i + 1);
        assert_eq!(parts[0].days(), *w);
        assert_eq!(parts[0].charge(), want_charge[i]);
        assert_eq!(parts[0].rate(), percent(7));
    }
    assert_eq!(res.value().total(), USCents(24_423));
    assert!(because(
        &res.required(),
        PositionKind::UnpublishedRate,
        "Dec 31, 2026"
    ));
}

/// A payment stops the charge on what it pays: April's $2,250 paid May 15
/// runs 30 days, $12.95.
#[test]
fn estimated_payment_stops_the_charge() {
    let d = dues();
    let h = Household {
        payments: vec![
            pay(day(2026, 5, 15), DOLLAR * 2_250),
            pay(d[1], DOLLAR * 2_250),
            pay(d[2], DOLLAR * 2_250),
            pay(d[3], DOLLAR * 2_250),
        ],
        ..tax(DOLLAR * 10_000)
    };
    let res = penalty(&h);
    let l = res.value().lines();
    assert_eq!(l.len(), 1);
    assert_eq!(l[0].days(), 30);
    assert_eq!(res.value().total(), USCents(1_295));
}

/// The statute's dates, a quarter each, and none moves.
#[test]
fn estimated_schedule_for_twenty_twenty_six() {
    let s = rules()
        .schedule(&tax(DOLLAR * 10_000).facts())
        .unwrap()
        .value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
        assert_eq!(inst.needed_by(), DOLLAR * 2_250 * (i as i64 + 1));
    }
}

/// Filed and paid by January 31, 2027, the tax counts in the January column:
/// the earlier shortfalls stop on January 15 and January's bears nothing.
/// January 31 is a Sunday and no source says whether it moves.
#[test]
fn estimated_january_thirty_first() {
    let res = penalty(&Household {
        return_filed: day(2027, 1, 29),
        ..tax(DOLLAR * 10_000)
    });
    assert_eq!(res.value().as_of(), day(2027, 1, 15));
    assert_eq!(res.value().total(), USCents(24_423));
    for (filed, moves, excused, asked) in [
        (day(2027, 1, 31), false, true, false),
        (day(2027, 2, 1), false, false, true),
        (day(2027, 2, 1), true, true, false),
    ] {
        let mut h = Household {
            return_filed: filed,
            ..tax(DOLLAR * 10_000)
        };
        if moves {
            h.extras = Some(
                new_estimated_facts()
                    .january_thirty_first_moves(true)
                    .build()
                    .unwrap(),
            );
        }
        let res = rules().schedule(&h.facts()).unwrap();
        assert_eq!(
            res.value()[3].status() == Status::Excused,
            excused,
            "filed {filed}, moves {moves}"
        );
        assert_eq!(
            because(&res.required(), PositionKind::JurisdictionFact, "a Sunday"),
            asked,
            "filed {filed}, moves {moves}"
        );
    }
}

/// Tax less withholding under $1,000 owes nothing.
#[test]
fn estimated_threshold_boundary() {
    for (withheld, exempt) in [(USCents(900_001), true), (USCents(900_000), false)] {
        let res = rules()
            .requirement(
                &Household {
                    withheld,
                    ..tax(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {withheld}");
        if exempt {
            let c = res.value().chosen().unwrap();
            assert_eq!(c.basis(), Basis::Exception);
            assert_eq!(c.amount(), USCents(0));
            assert_eq!(c.label(), "Tax less withholding under $1,000");
        }
    }
}

/// The lesser of 90% of this year's tax and 100% of last year's, from a
/// filed twelve-month return.
#[test]
fn estimated_target_options() {
    let res = rules()
        .requirement(
            &Household {
                prior: prior(DOLLAR * 8_999, 12),
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::PriorYearTax);
    assert_eq!(c.percent(), percent(100));
    assert_eq!(res.value().target(), DOLLAR * 8_999);
    let res = rules()
        .requirement(
            &Household {
                prior: prior(DOLLAR * 9_001, 12),
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().chosen().unwrap().basis(), Basis::ThisYearTax);
    assert_eq!(res.value().target(), DOLLAR * 9_000);
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    for (name, p) in [
        ("none on record", None),
        ("not filed", Some(not_filed)),
        ("eleven months", prior(USCents(100), 11)),
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
        assert!(
            !res.value().options()[1].available(),
            "{name}: the prior-year test is offered"
        );
    }
}

/// No liability last year means no interest, with no twelve-month or
/// residence condition.
#[test]
fn estimated_no_liability_exception() {
    for (name, p) in [
        ("twelve months", prior(USCents(0), 12)),
        ("six months", prior(USCents(0), 6)),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(res.value().exempt(), "{name}: {}", res.value().why());
        let c = res.value().chosen().unwrap();
        assert_eq!(c.basis(), Basis::Exception);
        assert_eq!(c.label(), "No North Carolina tax liability last year");
        let p = penalty(&h).value();
        assert_eq!(p.total(), USCents(0));
        assert_eq!(p.reason(), "No North Carolina tax liability last year.");
    }
    let nr = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                prior: Some(nr),
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().exempt(), "not required to file");
    let res = rules()
        .requirement(
            &Household {
                prior: prior(USCents(1), 12),
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(
        !res.value().exempt(),
        "one cent of liability last year is exempt"
    );
}

/// Withholding counts in equal parts unless the filer establishes the dates.
#[test]
fn estimated_withholding() {
    let h = Household {
        withheld: DOLLAR * 9_000,
        ..tax(DOLLAR * 10_000)
    };
    assert_eq!(penalty(&h).value().total(), USCents(0));
    let as_withheld = estimated::new_elections()
        .withholding_as_withheld()
        .build()
        .unwrap();
    let h = Household {
        elections: Some(as_withheld),
        ..h
    };
    assert_ne!(penalty(&h).value().total(), USCents(0));
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
        ..tax(DOLLAR * 10_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [DOLLAR * 450, DOLLAR * 1_800, DOLLAR * 5_400, DOLLAR * 9_000]
        .iter()
        .enumerate()
    {
        assert_eq!(s[i].needed_by(), *want, "installment {}", i + 1);
    }
}

/// Without this year's tax, the target is last year's alone and the interest
/// is the most it can be.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000, 12),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required());
    assert_eq!(r.target(), DOLLAR * 8_000);
    assert_eq!(r.options()[0].why_not(), estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let p = penalty(&h).value();
    assert!(p.upper_bound() || !p.rated_through().is_zero());
    assert_ne!(p.total(), USCents(0));
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
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    // None of them changes anything for a household that filed nothing
    // early, has no farm income and was required from the start.
    for d in res.divergences() {
        assert!(
            !d.affects_household(),
            "{} affects a plain household",
            d.rule()
        );
    }
    let filed = rules()
        .schedule(
            &Household {
                return_filed: day(2027, 1, 29),
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    for d in filed.divergences() {
        if d.rule() == "A return filed and paid by January 31" {
            assert!(d.affects_household() && d.uses() == Reading::Form);
        }
    }
    let want = [
        (
            "A return filed and paid by January 31",
            "January 15 column",
            "§ 105-163.15(h)",
        ),
        (
            "Due dates on a weekend or holiday",
            "next business day",
            "§ 105-163.15(c)",
        ),
        (
            "A requirement that arises late",
            "a quarter in every column",
            "§ 105-163.15(d)",
        ),
        (
            "Who counts as a farmer",
            "2024 or 2025",
            "§ 105-163.15(i)(3)",
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, form, source)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {}", d.rule());
        };
        assert!(
            d.form().contains(form)
                && d.statute_source().contains(source)
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{} does not pin both readings",
            d.rule()
        );
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    assert!(
        estimated_tax(2025)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NoRules))
    );
    for (name, h) in [
        (
            "farmer",
            Household {
                farm_share: Rate(7000),
                ..tax(DOLLAR * 10_000)
            },
        ),
        (
            "fisher",
            Household {
                fish_share: Rate(7000),
                ..tax(DOLLAR * 10_000)
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
    let last = new_estimated_facts()
        .farmer_last_year(true)
        .build()
        .unwrap();
    assert!(
        rules()
            .requirement(
                &Household {
                    extras: Some(last),
                    ..tax(DOLLAR * 10_000)
                }
                .facts()
            )
            .err()
            .is_some_and(|e| e.is(&estimated::Error::Unsupported))
    );
    let res = rules()
        .requirement(
            &Household {
                fish_share: Rate(1000),
                ..tax(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "farmers' rules"
    ));
    let this = estimated::new_figures()
        .tax(USCents(100_000))
        .build()
        .unwrap();
    let other = estimated::new_facts(NC, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(this)
        .state(7_i64)
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
            estimated::new_facts(NC, 2027)
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
        "northcarolinataxcalc: the facts carry int, not northcarolinataxcalc.EstimatedFacts"
    );
    assert!(
        rules()
            .penalty(&tax(USCents(100)).facts(), Date::default())
            .is_err()
    );
    let f = estimated::new_facts(NC, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .positions(
            estimated::new_positions()
                .waiver(WaiverKind::Casualty)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    let applied: String = res.applied().iter().map(|r| r.applied()).collect();
    assert!(applied.contains("bankruptcy") && res.value().total().0 != 0);
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
        "7%",
        "110%",
        "half-year",
        "January 15",
        "casualty, retirement or disability",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s}");
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
    let mut b = estimated::new_facts(NC, 2026);
    b.filing_status(FilingStatus::Single)
        .this_year(figures(DOLLAR * 3_000))
        .prior_year(last);
    for m in 1..=12 {
        b.withheld(&[estimated::new_withheld()
            .on(day(2026, m, 28))
            .amount(DOLLAR * 300)
            .build()
            .unwrap()]);
    }
    let res = rules().requirement(&b.build().unwrap()).unwrap();
    let r = res.value();
    assert!(r.why().contains("a $600 refund"), "why = {}", r.why());
    let mut texts = vec![r.why()];
    for o in r.options() {
        texts.push(o.label());
        texts.push(o.why_not());
    }
    for q in res.required() {
        texts.extend([q.because(), q.question(), q.effect()]);
    }
    for d in res.divergences() {
        texts.extend([d.rule(), d.form(), d.statute()]);
    }
    for s in texts {
        assert!(
            !s.contains("−$") && !s.contains("-$"),
            "a negative amount where a refund belongs: {s}"
        );
    }
}

/// North Carolina waives this interest only for a period of bankruptcy,
/// N.C. Gen. Stat. § 105-237(a)(2), so even a retirement the federal rules
/// would waive is refused.
#[test]
fn estimated_waiver_outcomes() {
    let mut h = tax(DOLLAR * 10_000);
    let before = penalty(&h).value().total();
    assert_ne!(before, USCents(0), "no penalty to waive");
    let born = day(1962, 3, 1);
    let with = |c: WaiverCause, on: Date, born: Option<Date>| -> PositionsBuilder {
        let mut b = estimated::new_positions();
        b.waiver_for(c, on);
        if let Some(d) = born {
            b.born(d);
        }
        b
    };
    for (name, want, phrase, pos) in [
        (
            "retired in 2026 at 64",
            "refused",
            "only for a period of bankruptcy",
            with(WaiverCause::Retirement, day(2026, 6, 1), Some(born)),
        ),
        (
            "retired in 2020",
            "refused",
            "only for a period of bankruptcy",
            with(WaiverCause::Retirement, day(2020, 6, 1), Some(born)),
        ),
        (
            "disaster",
            "refused",
            "casualty, disaster, retirement or disability doesn't waive",
            with(WaiverCause::Disaster, day(2026, 8, 1), None),
        ),
    ] {
        h.positions = Some(pos.build().unwrap());
        let p = penalty(&h).value();
        let o = p.waiver_outcome().unwrap();
        let mut got = if o.eligible() { "eligible" } else { "" };
        if o.refused() {
            got = "refused";
        }
        if o.undecided() {
            got = "undecided";
        }
        let waived = if got == "eligible" {
            USCents(0)
        } else {
            before
        };
        assert_eq!(got, want, "{name}");
        assert!(o.reason().contains(phrase), "{name}: {}", o.reason());
        assert_eq!(p.total(), before, "{name}");
        assert_eq!(p.if_waived(), waived, "{name}");
    }
}
