//! Pennsylvania's estimated tax rules for 2026. Ported from
//! estimated_test.go.
//!
//! Go's TestEstimatedFiguresAreInTheirSources and the extract checks in the
//! other tests read testdata/ and are not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status, WaiverCause,
    WaiverKind, Weekday,
};
use filing::Status as FilingStatus;
use money::{DOLLAR, Rate, USCents};
use taxcalcs_pennsylvaniataxcalc::{EstimatedFacts, estimated_tax, new_estimated_facts};
use working::{PositionKind, Required};

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
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

/// One payment of each amount on each due date.
fn quarterly(a: &[USCents]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .map(|(i, x)| pay(dues()[i], *x))
        .collect()
}

#[derive(Clone, Default)]
struct Household {
    /// After credits, PA-40 line 12 less lines 21 to 23.
    tax: USCents,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    /// Income to each period end, for Exception 2.
    periods: Option<Vec<USCents>>,
    no_tax: bool,
    /// A waiver asked for, with its cause and dates.
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

fn usual_extras(income: USCents) -> EstimatedFacts {
    new_estimated_facts()
        .income_without_withholding(income)
        .credits(USCents(0))
        .prior_year_forgiveness(USCents(0))
        .prior_year_full_forgiveness(false)
        .build()
        .unwrap()
}

impl Household {
    fn facts(&self) -> Facts {
        let figures = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(estimated::PENNSYLVANIA, 2026);
        b.filing_status(FilingStatus::Single)
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
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        b.state(
            self.extras
                .unwrap_or_else(|| usual_extras(DOLLAR * 100_000)),
        );
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
            b.elections(estimated::new_elections().annualised().build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

fn return_with(taxable: USCents, tax: USCents, withheld: USCents, resident: bool) -> PriorReturn {
    let f = estimated::new_figures()
        .tax(tax)
        .taxable_income(taxable)
        .withheld(withheld)
        .build()
        .unwrap();
    estimated::new_prior_return()
        .filed(true)
        .figures(f)
        .full_year_resident(resident)
        .build()
        .unwrap()
}

/// A filed full-year resident return with its taxable income.
fn prior(taxable: USCents, tax: USCents, withheld: USCents) -> Option<PriorReturn> {
    Some(return_with(taxable, tax, withheld, true))
}

fn part_year(taxable: USCents, tax: USCents, withheld: USCents) -> Option<PriorReturn> {
    Some(return_with(taxable, tax, withheld, false))
}

#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&ten().facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
}

/// REV-1630's example of when the penalty applies: $3,599 of tax, $1,344 of
/// withholding and credits, 90% of the tax is $3,239 and last year's income
/// at this year's rate is $3,502.
#[test]
fn estimated_requirement_matches_rev1630_example() {
    let f = Household {
        withheld: DOLLAR * 1_344,
        prior: prior(USCents(11_407_166), DOLLAR * 3_000, USCents(0)),
        ..tax(DOLLAR * 3_599)
    }
    .facts();
    let res = rules().requirement(&f).unwrap();
    let r = res.value();
    assert!(r.required(), "{}", r.why());
    assert_eq!(r.target(), USCents(323_910));
    let opts = r.options();
    assert_eq!(opts[1].basis(), Basis::PriorYearIncome);
    assert_eq!(opts[1].amount(), USCents(350_200));
    assert!(opts[1].available());
    assert_eq!(r.chosen().unwrap().basis(), Basis::ThisYearTax);
}

/// REV-1630's example of the interest: $474 short on each date, unpaid.
#[test]
fn estimated_interest_matches_rev1630_example() {
    let f = tax(USCents(210_667)).facts();
    for as_of in [day(2026, 12, 31), day(2027, 4, 15)] {
        let res = rules().penalty(&f, as_of).unwrap();
        let lines = res.value().lines();
        assert_eq!(lines.len(), 4);
        let want_days = [260, 199, 107];
        let want_charge = [24, 18, 10];
        for i in 0..want_days.len() {
            assert_eq!(lines[i].shortfall(), DOLLAR * 474);
            let parts = lines[i].parts();
            assert_eq!(parts.len(), 1);
            assert_eq!(parts[0].days(), want_days[i]);
            assert_eq!(parts[0].charge(), DOLLAR * want_charge[i]);
        }
        assert_eq!(res.value().total(), DOLLAR * 52, "to {as_of}");
        assert_eq!(
            has(&res.required(), PositionKind::UnpublishedRate),
            as_of.year() == 2027
        );
    }
}

/// The interest follows REV-1611's printed 0.000192 a day, not 7% ÷ 365.
#[test]
fn estimated_interest_uses_the_printed_daily_factor() {
    let f = tax(USCents(44_444_444)).facts();
    let res = rules().penalty(&f, day(2026, 12, 31)).unwrap();
    let parts = res.value().lines()[0].parts();
    assert_eq!(parts.len(), 1);
    assert_eq!(parts[0].principal(), DOLLAR * 100_000);
    assert_eq!(parts[0].days(), 260);
    assert_eq!(parts[0].charge(), DOLLAR * 4_992);
}

/// Exception 1 excuses an installment; it does not lower one.
#[test]
fn estimated_exception_one_excuses_but_does_not_lower() {
    let last = prior(USCents(41_693_811), DOLLAR * 12_000, USCents(0));
    let f = Household {
        prior: last,
        payments: quarterly(&[DOLLAR * 3_300; 4]),
        ..tax(USCents(1_777_778))
    }
    .facts();
    let req = rules().requirement(&f).unwrap();
    assert_eq!(req.value().target(), DOLLAR * 12_800);
    assert_eq!(req.value().options()[1].amount() / 4, DOLLAR * 3_200);
    let sched = rules().schedule(&f).unwrap();
    for (i, inst) in sched.value().iter().enumerate() {
        assert_eq!(inst.status(), Status::Met, "installment {}", i + 1);
        assert_eq!(inst.shortfall(), USCents(0));
        assert_eq!(inst.needed_by(), DOLLAR * 3_200 * (i as i64 + 1));
    }
    let p = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));

    let f = Household {
        prior: last,
        payments: quarterly(&[
            DOLLAR * 3_100,
            DOLLAR * 3_300,
            DOLLAR * 3_300,
            DOLLAR * 3_300,
        ]),
        ..tax(USCents(1_777_778))
    }
    .facts();
    let p = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    let lines = p.value().lines();
    assert_eq!(lines.len(), 1);
    assert_eq!(lines[0].installment(), 1);
    assert_eq!(lines[0].shortfall(), DOLLAR * 900);
    assert_eq!(p.value().total(), DOLLAR * 11);
}

/// Exception 2 is 90% of the tax on the income actually received to each
/// period end, not annualised.
#[test]
fn estimated_exception_two_uses_income_to_date() {
    let mut h = Household {
        periods: Some(vec![
            DOLLAR * 5_000,
            DOLLAR * 20_000,
            DOLLAR * 200_000,
            DOLLAR * 600_000,
        ]),
        payments: quarterly(&[DOLLAR * 200, DOLLAR * 400, DOLLAR * 5_000, DOLLAR * 10_400]),
        ..tax(USCents(1_777_778))
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    let want_needed = [13_815, 55_260, 552_600, 1_600_000];
    for (i, inst) in sched.value().iter().enumerate() {
        assert_eq!(
            inst.needed_by(),
            USCents(want_needed[i]),
            "installment {}",
            i + 1
        );
        assert_eq!(inst.status(), Status::Met);
        assert_eq!(inst.shortfall(), USCents(0));
    }
    assert!(
        sched
            .applied()
            .iter()
            .any(|a| a.applied().contains("$138.15, $552.60, $5526.00"))
    );
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));
    h.periods = None;
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_ne!(p.value().total(), USCents(0));
}

/// A filer who first meets the requirement in July and owed no estimates
/// last year has April and June excused by paying 75% by September 15.
#[test]
fn estimated_late_starter_special_exception() {
    let d = dues();
    let payments = vec![pay(d[2], DOLLAR * 12_000), pay(d[3], DOLLAR * 4_000)];
    let small = part_year(DOLLAR * 10_000, DOLLAR * 300, USCents(0));
    let mut h = Household {
        prior: small,
        payments: payments.clone(),
        first_met: day(2026, 7, 1),
        ..tax(USCents(1_777_778))
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    let want_needed = [0, 0, 12_000, 16_000];
    for (i, inst) in sched.value().iter().enumerate() {
        assert_eq!(inst.needed_by(), DOLLAR * want_needed[i]);
        if i < 2 {
            assert_eq!(inst.status(), Status::NothingDue);
            assert_eq!(inst.shortfall(), USCents(0));
        }
    }
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));

    // Short of the whole catch-up by September 15, the exception applies to
    // no installment.
    let short = Household {
        prior: small,
        first_met: day(2026, 7, 1),
        payments: vec![pay(d[2], DOLLAR * 11_999), pay(d[3], DOLLAR * 4_001)],
        ..tax(USCents(1_777_778))
    };
    let s = rules().schedule(&short.facts()).unwrap().value();
    assert_eq!(s[0].status(), Status::Short);
    assert_eq!(s[1].status(), Status::Short);

    // The same filer, not told when the requirement arose, is asked.
    h.first_met = Date::default();
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(has(&res.required(), PositionKind::RequirementFirstMet));
    assert_ne!(res.value().total(), USCents(0));

    // Line A of $5,000: no special exception.
    let h = Household {
        prior: part_year(DOLLAR * 10_000, DOLLAR * 5_000, USCents(0)),
        payments,
        first_met: day(2026, 7, 1),
        ..tax(USCents(1_777_778))
    };
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), DOLLAR * 189);

    // The form's groups: March 31 is in the first, April 1 in the second.
    for (met, excused) in [(day(2026, 3, 31), false), (day(2026, 4, 1), true)] {
        let h = Household {
            prior: small,
            first_met: met,
            payments: vec![
                pay(d[1], DOLLAR * 8_000),
                pay(d[2], DOLLAR * 4_000),
                pay(d[3], DOLLAR * 4_000),
            ],
            ..tax(USCents(1_777_778))
        };
        let got = rules().schedule(&h.facts()).unwrap().value()[0].status() == Status::NothingDue;
        assert_eq!(got, excused, "first met {met}");
    }
}

/// The thresholds, as the form words them: $14,000 or more of income without
/// withholding starts the rules, and $430 or more of tax after withholding.
#[test]
fn estimated_threshold_boundaries() {
    for (income, withheld, exempt) in [
        (1_399_999, 0, true),
        (1_400_000, 0, false),
        (1_400_000, 957_001, true),
        (1_400_000, 957_000, false),
    ] {
        let e = usual_extras(USCents(income));
        let res = rules()
            .requirement(
                &Household {
                    withheld: USCents(withheld),
                    extras: Some(e),
                    ..ten()
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "income {income}, withheld {withheld}: {}",
            res.value().why()
        );
        let want = if income >= 1_400_000 {
            "Tax less withholding and credits under $430"
        } else {
            "Income without withholding under $14,000"
        };
        if exempt {
            let c = res.value().chosen().unwrap();
            assert_eq!(c.basis(), Basis::Exception);
            assert_eq!(c.amount(), USCents(0));
            assert_eq!(c.label(), want);
        }
    }
}

/// 100% Tax Forgiveness last year takes this year out of the penalty, and
/// the credits are added back before the 90% is taken.
#[test]
fn estimated_forgiveness_and_credits() {
    let e = new_estimated_facts()
        .prior_year_full_forgiveness(true)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                extras: Some(e),
                ..ten()
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().exempt() && !res.value().required());
    assert_eq!(
        res.value().chosen().unwrap().label(),
        "Full Tax Forgiveness last year"
    );

    let e = new_estimated_facts()
        .income_without_withholding(DOLLAR * 300_000)
        .credits(DOLLAR * 1_000)
        .prior_year_forgiveness(USCents(0))
        .prior_year_full_forgiveness(false)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                extras: Some(e),
                ..tax(DOLLAR * 9_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_000);

    // Nothing said: every Pennsylvania fact is asked for.
    let f = estimated::new_facts(estimated::PENNSYLVANIA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 9_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    let n = res
        .required()
        .iter()
        .filter(|r| r.position() == PositionKind::JurisdictionFact)
        .count();
    assert_eq!(n, 3);
}

/// The prior-year test needs a filed full-year resident return with its
/// taxable income.
#[test]
fn estimated_prior_year_option_availability() {
    let unanswered = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(USCents(100))
                .taxable_income(USCents(10_000))
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let no_income = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .full_year_resident(true)
        .build()
        .unwrap();
    for (name, p, ask) in [
        (
            "part-year",
            part_year(DOLLAR * 100_000, DOLLAR * 3_070, USCents(0)),
            PositionKind::Unspecified,
        ),
        (
            "residence not given",
            Some(unanswered),
            PositionKind::Residency,
        ),
        (
            "taxable income unset",
            Some(no_income),
            PositionKind::PriorYearReturn,
        ),
        ("no return on record", None, PositionKind::PriorYearReturn),
    ] {
        let res = rules()
            .requirement(&Household { prior: p, ..ten() }.facts())
            .unwrap();
        assert!(!res.value().options()[1].available(), "{name}");
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}");
        }
    }
}

/// A return filed and paid by January 31 is the fourth installment, and the
/// penalty stops the day it is paid.
#[test]
fn estimated_return_filed_and_paid() {
    for (filed, excused) in [(day(2027, 1, 29), true), (day(2027, 2, 1), false)] {
        let res = rules()
            .schedule(
                &Household {
                    return_filed: filed,
                    ..ten()
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(res.value()[3].status() == Status::Excused, excused);
    }
    let p = rules()
        .penalty(
            &Household {
                return_filed: day(2027, 3, 1),
                ..ten()
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert_eq!(p.value().as_of(), day(2027, 3, 1));
    let p = rules().penalty(&ten().facts(), day(2027, 6, 30)).unwrap();
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&ten().facts()).unwrap();
    let want = [
        ("When the rules start", "$14,000 or more", "§ 7325(a)(1)"),
        (
            "A requirement first met after April 1",
            "special exception",
            "§§ 7325(d), 7326(a)",
        ),
        (
            "A return filed and paid by January 31",
            "Any final return",
            "§ 7325(h)",
        ),
        (
            "The prior-year test after a part-year return",
            "only if you were a full-year resident",
            "§ 7352(d)(2)(A)",
        ),
        ("The daily interest factor", "0.000192", "§ 806"),
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
                && !d.statute().is_empty()
                && d.uses() == working::Reading::Form,
            "{} does not pin both readings",
            d.rule()
        );
        assert_eq!(
            d.affects_household(),
            d.rule() == "The daily interest factor",
            "{}",
            d.rule()
        );
    }
    let affects = |h: Household, rule: &str| -> bool {
        rules()
            .requirement(&h.facts())
            .unwrap()
            .divergences()
            .iter()
            .find(|d| d.rule() == rule)
            .map(|d| d.affects_household())
            .unwrap()
    };
    let exactly = usual_extras(DOLLAR * 14_000);
    for (name, h, rule, want) in [
        (
            "exactly $14,000",
            Household {
                extras: Some(exactly),
                ..ten()
            },
            "When the rules start",
            true,
        ),
        (
            "first met in July",
            Household {
                first_met: day(2026, 7, 1),
                ..ten()
            },
            "A requirement first met after April 1",
            true,
        ),
        (
            "filed January 29",
            Household {
                return_filed: day(2027, 1, 29),
                ..ten()
            },
            "A return filed and paid by January 31",
            true,
        ),
        (
            "part-year last year",
            Household {
                prior: part_year(DOLLAR * 100_000, DOLLAR * 3_070, USCents(0)),
                ..ten()
            },
            "The prior-year test after a part-year return",
            true,
        ),
        (
            "paid in full",
            Household {
                payments: quarterly(&[DOLLAR * 2_250; 4]),
                ..ten()
            },
            "The daily interest factor",
            false,
        ),
    ] {
        assert_eq!(affects(h, rule), want, "{name}");
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    assert!(
        estimated_tax(2025)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NoRules))
    );
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let base = || {
        let mut b = estimated::new_facts(estimated::PENNSYLVANIA, 2026);
        b.filing_status(FilingStatus::Single).this_year(this);
        b
    };
    let farmer = base().farm_share(Rate(7000)).build().unwrap();
    assert!(
        rules()
            .requirement(&farmer)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::Unsupported))
    );
    // Pennsylvania's farmer rules are for farming alone.
    let fisher = base().fish_share(Rate(7000)).build().unwrap();
    assert!(rules().requirement(&fisher).is_ok());
    let other = base().state("not Pennsylvania's").build().unwrap();
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
            estimated::new_facts(estimated::PENNSYLVANIA, 2027)
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
        "pennsylvaniataxcalc: the facts carry string, not pennsylvaniataxcalc.EstimatedFacts"
    );
    assert!(new_estimated_facts().credits(USCents(-1)).build().is_err());
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
        "$14,000",
        "$430",
        "3.07%",
        "last year's income at this year's rate",
        "excuse a quarter",
        "without annualising",
    ] {
        assert!(text.contains(s), "{s}");
    }
}

/// Without this year's tax, Pennsylvania's target is last year's income at
/// this year's rate, and the installments are measured against it.
#[test]
fn estimated_without_this_years_tax_uses_last_years_income_alone() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 100_000, DOLLAR * 3_000, USCents(0)),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required());
    assert_eq!(r.target(), DOLLAR * 3_070);
    let o = &r.options()[0];
    assert!(!o.available());
    assert_eq!(o.why_not(), "Needs this year's tax");
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearIncome);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [76_750, 153_500, 230_250, 307_000].iter().enumerate() {
        assert_eq!(sched.value()[i].needed_by(), USCents(*want));
    }
    assert!(
        rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NeedsThisYearTax))
    );
    let paid = Household {
        payments: quarterly(&[USCents(76_750); 4]),
        ..h
    };
    let pen = rules().penalty(&paid.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(pen.value().total(), USCents(0));
    assert!(!pen.value().upper_bound());
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
    let mut b = estimated::new_facts(estimated::PENNSYLVANIA, 2026);
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
        assert!(!s.contains("−$") && !s.contains("-$"), "{s}");
    }
}

/// An exception sets a $0 target, so a question whose answer would only
/// change the target is not asked under one.
#[test]
fn estimated_exception_asks_nothing_about_the_target() {
    for (name, h, exempt) in [
        (
            "withholding over the tax, no 2025 return",
            Household {
                withheld: DOLLAR * 2_000,
                ..tax(DOLLAR * 1_000)
            },
            true,
        ),
        ("no withholding, no 2025 return", ten(), false),
    ] {
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn),
            !exempt,
            "{name}"
        );
        if exempt {
            for q in res.required() {
                assert!(!q.effect().to_lowercase().contains("target"), "{name}");
            }
        }
    }
}

/// Pennsylvania's estimated tax rules have none of the federal waivers, so
/// every waiver asked for is refused and the penalty stands.
#[test]
fn estimated_waiver_is_refused_unlike_federal() {
    let before = rules().penalty(&ten().facts(), day(2027, 6, 30)).unwrap();
    assert_ne!(before.value().total(), USCents(0));
    let mut retired = estimated::new_positions();
    retired
        .waiver_for(WaiverCause::Retirement, day(2026, 6, 1))
        .born(day(1962, 3, 1));
    for (name, positions) in [
        ("retired in 2026 at 64", retired.build().unwrap()),
        (
            "a disaster",
            estimated::new_positions()
                .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
                .build()
                .unwrap(),
        ),
        (
            "a casualty",
            estimated::new_positions()
                .waiver(WaiverKind::Casualty)
                .build()
                .unwrap(),
        ),
    ] {
        let p = rules()
            .penalty(
                &Household {
                    positions: Some(positions),
                    ..ten()
                }
                .facts(),
                day(2027, 6, 30),
            )
            .unwrap();
        let o = p.value().waiver_outcome().unwrap();
        assert!(o.refused(), "{name}: {}", o.reason());
        assert!(o.reason().contains("Board of Appeals"), "{name}");
        assert_eq!(p.value().total(), before.value().total(), "{name}");
        assert_eq!(p.value().if_waived(), p.value().total(), "{name}");
    }
}
