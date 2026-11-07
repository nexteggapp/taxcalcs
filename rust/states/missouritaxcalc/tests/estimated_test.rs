//! Missouri's estimated tax rules for 2026, ported from the Go package's
//! estimated_test.go. (Go also compares every figure against its extract in
//! testdata/; that check stays in Go.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, Elections, EstimatedTax, Facts, Month, Payment, Penalty, Positions, PriorReturn,
    Status, WaiverCause, Weekday,
};
use money::{DOLLAR, Rate, USCents, dollars, percent};
use taxcalcs_missouritaxcalc::{EstimatedFacts, estimated_tax, new_estimated_facts};
use working::{PositionKind, Required};

const MO: estimated::Jurisdiction = estimated::MISSOURI;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn because(rs: &[Required], k: PositionKind, phrase: &str) -> bool {
    rs.iter().any(|r| {
        r.position() == k && (r.question().contains(phrase) || r.effect().contains(phrase))
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

fn quarterly(a: [USCents; 4]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .filter(|(_, x)| x.0 > 0)
        .map(|(i, x)| pay(dues()[i], *x))
        .collect()
}

#[derive(Default, Clone)]
struct Household {
    tax: USCents,
    no_tax: bool,
    /// Withheld on December 15.
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    elections: Option<Elections>,
    annualised: Vec<USCents>,
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
        let mut b = estimated::new_facts(MO, 2026);
        b.filing_status(filing::Status::MarriedFilingJointly)
            .this_year(figures)
            .paid(&self.payments)
            .requirement_first_met(self.first_met)
            .return_filed(self.return_filed)
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
        match &self.extras {
            Some(e) => b.state(e.clone()),
            None => b.state(new_estimated_facts().no_combat_zone().build().unwrap()),
        };
        if let Some(e) = self.elections {
            b.elections(e);
        }
        if !self.annualised.is_empty() {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            let periods: Vec<_> = self
                .annualised
                .iter()
                .enumerate()
                .map(|(i, t)| {
                    estimated::new_period()
                        .end(ends[i])
                        .annualised_tax(*t)
                        .build()
                        .unwrap()
                })
                .collect();
            b.periods(&periods)
                .elections(estimated::new_elections().annualised().build().unwrap());
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

/// The 2026 dates are the printed ones and none moves. January 31, 2027 is a
/// Sunday; the 2025 form printed January 31, 2026, a Saturday, without
/// moving it, so a return filed on Monday, February 1 is not counted as paid
/// on January 15.
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
    for (filed, excused) in [(day(2027, 1, 31), true), (day(2027, 2, 1), false)] {
        let h = Household {
            return_filed: filed,
            ..tax(DOLLAR * 10_000)
        };
        let s = rules().schedule(&h.facts()).unwrap().value();
        assert_eq!(s[3].status() == Status::Excused, excused, "filed {filed}");
    }
}

/// MO-2210 line 4 stops under $500; the statute's declaration threshold is
/// $100. The form is followed, the gap is asked about, and the answer
/// applies $100.
#[test]
fn estimated_threshold_boundaries() {
    let statute = new_estimated_facts()
        .no_combat_zone()
        .statutory_threshold(true)
        .build()
        .unwrap();
    for (withheld, extras, exempt, asked) in [
        (950_001, None, true, true), // $499.99
        (950_000, None, false, false),
        (990_001, None, true, false), // $99.99
        (990_001, Some(statute.clone()), true, false),
        (990_000, Some(statute.clone()), false, false), // $100.00
        (960_000, Some(statute.clone()), false, false),
    ] {
        let statutory = extras.is_some();
        let h = Household {
            withheld: USCents(withheld),
            extras,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {withheld}");
        let limit = if statutory { "$100" } else { "$500" };
        if exempt {
            let c = res.value().chosen().unwrap();
            assert_eq!(c.basis(), Basis::Exception);
            assert_eq!(c.amount(), USCents(0));
            assert_eq!(c.label(), format!("Tax less withholding under {limit}"));
        }
        // Only tax less withholding from $100 to under $500 reads differently
        // under the statute.
        for d in res.divergences() {
            let want = withheld == 950_001 || withheld == 960_000 || withheld == 990_000;
            if d.rule() == "The $500 stop" {
                assert_eq!(d.affects_household(), want, "withheld {withheld}");
            }
        }
        assert_eq!(
            because(
                &res.required(),
                PositionKind::JurisdictionFact,
                "start at $100"
            ),
            asked,
            "withheld {withheld}"
        );
    }
}

/// The required annual payment is the smaller of 90% of this year's tax and
/// 100% of last year's, which needs a twelve-month return showing a
/// liability.
#[test]
fn estimated_target_options() {
    let h = Household {
        prior: prior(DOLLAR * 8_000, 12),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().chosen().unwrap().basis(), Basis::PriorYearTax);
    assert_eq!(res.value().target(), DOLLAR * 8_000);
    let h = Household {
        prior: prior(DOLLAR * 9_500, 12),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().chosen().unwrap().basis(), Basis::ThisYearTax);
    assert_eq!(res.value().target(), DOLLAR * 9_000);
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    for (name, p) in [("none on record", None), ("not filed", Some(not_filed))] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(!res.value().options()[1].available(), "{name}");
    }
}

/// Part I line 5 takes last year's tax with no condition; Exception 1 and
/// the statute need twelve months and a liability. The latter is used and
/// asked about; the caller can take line 5 as printed.
#[test]
fn estimated_zero_or_short_prior_year_is_an_open_question() {
    const Q: &str = "MO-2210 line 5 count";
    let as_printed = new_estimated_facts()
        .no_combat_zone()
        .line_five_as_printed(true)
        .build()
        .unwrap();
    for (name, p) in [
        ("no liability", prior(USCents(0), 12)),
        ("six months", prior(DOLLAR * 5_000, 6)),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(!res.value().options()[1].available(), "{name}");
        assert!(
            because(&res.required(), PositionKind::JurisdictionFact, Q),
            "{name}"
        );
        let want = p.unwrap().figures().unwrap().tax().unwrap();
        let h = Household {
            extras: Some(as_printed.clone()),
            ..h
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().target(), want, "{name}");
        assert!(
            !because(&res.required(), PositionKind::JurisdictionFact, Q),
            "{name}"
        );
    }
}

/// Nothing paid on $9,000 required: each column's total underpayment to the
/// next due date. The September column spans the 2026 and 2027 rate
/// periods; both are 7%, so the boundary splits the charge without changing
/// the rate.
#[test]
fn estimated_penalty_arithmetic() {
    let res = penalty(&tax(DOLLAR * 10_000));
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    let want: [(i64, &[i64], &[i64]); 4] = [
        (225_000, &[61], &[2_632]),
        (450_000, &[92], &[7_940]),
        (675_000, &[107, 15], &[13_851, 1_942]),
        (900_000, &[90], &[15_534]),
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
            assert_eq!(p.rate(), percent(7));
        }
    }
    let parts = lines[2].parts();
    assert!(parts[0].source().contains("2026") && parts[1].source().contains("2027"));
    assert_eq!(res.value().total(), USCents(41_899));
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
    // Never past April 15, 2027.
    let p = rules()
        .penalty(&tax(DOLLAR * 10_000).facts(), day(2027, 7, 1))
        .unwrap();
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
    assert_eq!(p.value().total(), USCents(41_899));
}

/// A payment between due dates stops the charge on what it pays: April's
/// $2,250 paid May 15 runs 30 days, $12.95.
#[test]
fn estimated_payment_between_due_dates() {
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

/// Filed and paid by January 31, 2027, the balance counts as paid on
/// January 15: the first three columns stop then and January's bears
/// nothing.
#[test]
fn estimated_return_filed_by_january_thirty_first() {
    let res = penalty(&Household {
        return_filed: day(2027, 1, 29),
        ..tax(DOLLAR * 10_000)
    });
    assert_eq!(res.value().as_of(), day(2027, 1, 15));
    assert_eq!(res.value().total(), USCents(26_365));
    let res = penalty(&Household {
        return_filed: day(2027, 3, 1),
        ..tax(DOLLAR * 10_000)
    });
    assert_eq!(res.value().as_of(), day(2027, 3, 1));
}

/// Withholding counts in equal parts on the four dates unless the filer
/// shows the dates.
#[test]
fn estimated_withholding() {
    let h = Household {
        withheld: DOLLAR * 9_000,
        ..tax(DOLLAR * 10_000)
    };
    assert_eq!(penalty(&h).value().total(), USCents(0));
    let h = Household {
        elections: Some(
            estimated::new_elections()
                .withholding_as_withheld()
                .build()
                .unwrap(),
        ),
        ..h
    };
    assert!(penalty(&h).value().total().0 != 0);
}

/// A requirement first met on July 1 starts at September 15 with two
/// installments of $4,500. The table's boundaries: April 1 is in the first
/// row, April 2 in the second.
#[test]
fn estimated_late_starter() {
    let h = Household {
        first_met: day(2026, 7, 1),
        ..tax(DOLLAR * 10_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [0, 0, 4_500, 9_000].into_iter().enumerate() {
        assert_eq!(s[i].needed_by(), DOLLAR * want, "installment {}", i + 1);
    }
    assert_eq!(s[0].status(), Status::NothingDue);
    assert_eq!(s[1].status(), Status::NothingDue);
    let p = penalty(&h);
    assert_eq!(p.value().total(), USCents(26_063));
    assert!(!has(&p.required(), PositionKind::RequirementFirstMet));
    for (met, need) in [
        (day(2026, 4, 1), DOLLAR * 2_250),
        (day(2026, 4, 2), USCents(0)),
    ] {
        let h = Household {
            first_met: met,
            ..tax(DOLLAR * 10_000)
        };
        let s = rules().schedule(&h.facts()).unwrap().value();
        assert_eq!(s[0].needed_by(), need, "first met {met}");
    }
    let h = Household {
        first_met: day(2026, 4, 2),
        ..tax(DOLLAR * 10_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[1].needed_by(), DOLLAR * 3_000);
    let p = penalty(&tax(DOLLAR * 10_000));
    assert!(has(&p.required(), PositionKind::RequirementFirstMet));
}

/// Exception 2: payments reaching each date's share of the tax on last
/// year's income at this year's rates excuse the installment.
#[test]
fn estimated_exception_two() {
    let e = new_estimated_facts()
        .no_combat_zone()
        .prior_year_income_at_this_years_rates(DOLLAR * 4_000)
        .build()
        .unwrap();
    let k = DOLLAR * 1_000;
    let mut h = Household {
        extras: Some(e.clone()),
        payments: quarterly([k, k, k, k]),
        ..tax(DOLLAR * 10_000)
    };
    assert_eq!(penalty(&h).value().total(), USCents(0));
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.status(), Status::Excused, "installment {}", i + 1);
        assert!(inst.shortfall().0 != 0, "installment {}", i + 1);
    }
    h.payments = quarterly([DOLLAR * 999, DOLLAR * 1_001, k, k]);
    assert!(penalty(&h).value().total().0 != 0);
    let nr = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let h = Household {
        extras: Some(e),
        prior: Some(nr),
        payments: quarterly([k, k, k, k]),
        ..tax(DOLLAR * 10_000)
    };
    assert!(penalty(&h).value().total().0 != 0);
}

/// Exceptions 3 and 4 excuse the first three installments only.
#[test]
fn estimated_exceptions_three_and_four() {
    let payments = quarterly([DOLLAR * 900, DOLLAR * 900, DOLLAR * 3_600, DOLLAR * 3_600]);
    let mut h = Household {
        payments: payments.clone(),
        annualised: vec![
            DOLLAR * 4_000,
            DOLLAR * 4_000,
            DOLLAR * 8_000,
            DOLLAR * 10_000,
        ],
        ..tax(DOLLAR * 10_000)
    };
    assert_eq!(penalty(&h).value().total(), USCents(0));
    let e = new_estimated_facts()
        .no_combat_zone()
        .tax_on_income_to_date(DOLLAR * 1_000, DOLLAR * 2_000, DOLLAR * 6_000)
        .build()
        .unwrap();
    let with_e = |payments: Vec<Payment>| Household {
        payments,
        extras: Some(e.clone()),
        ..tax(DOLLAR * 10_000)
    };
    assert_eq!(
        penalty(&with_e(payments.clone())).value().total(),
        USCents(0)
    );
    let plain = Household {
        payments: payments.clone(),
        ..tax(DOLLAR * 10_000)
    };
    assert!(penalty(&plain).value().total().0 != 0);
    // One cent under each exception's $900 in April is charged.
    let cent = quarterly([
        USCents(89_999),
        USCents(90_001),
        DOLLAR * 3_600,
        DOLLAR * 3_600,
    ]);
    h.payments = cent.clone();
    let l = penalty(&h).value().lines();
    assert!(l.len() == 1 && l[0].installment() == 1, "{l:?}");
    let l = penalty(&with_e(cent)).value().lines();
    assert!(l.len() == 1 && l[0].installment() == 1, "{l:?}");
    // January's shortfall is never excused by them.
    let short = quarterly([DOLLAR * 900, DOLLAR * 900, DOLLAR * 3_600, DOLLAR * 3_000]);
    let l = penalty(&with_e(short)).value().lines();
    assert!(l.len() == 1 && l[0].installment() == 4, "{l:?}");
}

/// An excused April shortfall carries into June's column, where line 25
/// charges it: June's $3,600 includes April's $1,350, for 92 days, $63.52.
#[test]
fn estimated_excused_shortfall_carries_forward() {
    let h = Household {
        annualised: vec![
            DOLLAR * 4_000,
            DOLLAR * 20_000,
            DOLLAR * 20_000,
            DOLLAR * 10_000,
        ],
        payments: quarterly([DOLLAR * 900, USCents(0), DOLLAR * 6_750, DOLLAR * 2_250]),
        ..tax(DOLLAR * 10_000)
    };
    let p = penalty(&h);
    let l = p.value().lines();
    assert_eq!(l.len(), 1);
    assert_eq!(l[0].installment(), 2);
    assert_eq!(l[0].shortfall(), DOLLAR * 3_600);
    assert_eq!(p.value().total(), USCents(6_352));
}

/// Installments due during service in a combat zone are exempt. Not
/// answered, the question is asked when there is a penalty.
#[test]
fn estimated_combat_zone() {
    let e = new_estimated_facts()
        .combat_zone(day(2026, 3, 1), day(2026, 7, 31))
        .build()
        .unwrap();
    let p = penalty(&Household {
        extras: Some(e),
        ..tax(DOLLAR * 10_000)
    });
    assert_eq!(p.value().total(), USCents(13_851 + 1_942 + 15_534));
    assert!(!because(
        &p.required(),
        PositionKind::JurisdictionFact,
        "combat zone"
    ));
    let none = new_estimated_facts().build().unwrap();
    let p = penalty(&Household {
        extras: Some(none),
        ..tax(DOLLAR * 10_000)
    });
    assert!(because(
        &p.required(),
        PositionKind::JurisdictionFact,
        "combat zone"
    ));
    assert!(
        new_estimated_facts()
            .combat_zone(day(2026, 8, 1), day(2026, 7, 1))
            .build()
            .is_err()
    );
}

/// Without this year's tax, the target is last year's alone and the penalty
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
    let p = penalty(&h);
    assert!(p.value().upper_bound() && p.value().total().0 != 0);
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
    let err = rules()
        .penalty(&none.facts(), day(2027, 4, 15))
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want = [
        (
            "What the penalty is measured against",
            "smaller of two amounts. They are 90%",
            "143.761.2",
        ),
        ("The $500 stop", "under $500", "143.521.1"),
        (
            "A prior-year return with no tax, or a short year",
            "Exception 1 on the same form",
            "143.761.4(1)",
        ),
        (
            "Exception 4, tax on income to date",
            "90% of the tax on the income",
            "143.761.4(3)",
        ),
        (
            "An excused installment's shortfall",
            "Line 19d",
            "143.761.4",
        ),
    ];
    let ds = res.divergences();
    assert_eq!(ds.len(), want.len());
    for d in &ds {
        let Some((_, form, statute)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        // 90% of this year's is chosen and nothing is excused, so none of
        // the divergences touches these facts.
        assert!(!d.affects_household(), "{:?}", d.rule());
        assert!(d.form().contains(form) && d.statute_source().contains(statute));
        assert!(!d.form_source().is_empty() && !d.statute().is_empty());
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    let err = estimated_tax(2025).err().unwrap();
    assert!(err.is(&estimated::Error::NoRules));
    let this = estimated::new_figures()
        .tax(USCents(100_000))
        .build()
        .unwrap();
    let base = |j: estimated::Jurisdiction, y: i64| {
        let mut b = estimated::new_facts(j, y);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    let farmer = base(MO, 2026).farm_share(Rate(7000)).build().unwrap();
    let err = rules().requirement(&farmer).err().unwrap();
    assert!(err.is(&estimated::Error::Unsupported), "{err}");
    // 12 CSR 10-2.067(2)(B): fishing is not farming.
    let fisher = Household {
        fish_share: Rate(7000),
        ..tax(DOLLAR * 10_000)
    };
    rules().requirement(&fisher.facts()).unwrap();
    for (name, f) in [
        (
            "California's facts",
            base(estimated::CALIFORNIA, 2026).build().unwrap(),
        ),
        ("2027", base(MO, 2027).build().unwrap()),
        ("other extras", base(MO, 2026).state("x").build().unwrap()),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&tax(USCents(100)).facts(), Date::default())
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
        "$100",
        "$500",
        "7%",
        "100%",
        "last year's income taxed at this year's rates",
        "fewer, larger installments",
        "reasonable cause",
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
    let f = estimated::new_facts(MO, 2026)
        .filing_status(filing::Status::Single)
        .this_year(figures(3_000, 60_000))
        .prior_year(prior)
        .withheld(&withheld)
        .build()
        .unwrap();
    let got = rules().requirement(&f).unwrap().value().why();
    assert!(got.contains("a $600 refund"), "why = {got:?}");
    assert!(!got.contains("−$") && !got.contains("-$"), "why = {got:?}");
}

fn retired_on(on: Date, born: Date) -> Positions {
    estimated::new_positions()
        .waiver_for(WaiverCause::Retirement, on)
        .born(born)
        .build()
        .unwrap()
}

/// Missouri has no waiver: the addition is charged even with reasonable
/// cause.
#[test]
fn estimated_waiver_is_refused() {
    for (name, pos) in [
        (
            "retired in 2026 at 64",
            retired_on(day(2026, 6, 1), day(1962, 3, 1)),
        ),
        (
            "a disaster",
            estimated::new_positions()
                .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
                .build()
                .unwrap(),
        ),
    ] {
        let h = Household {
            positions: Some(pos),
            ..tax(DOLLAR * 10_000)
        };
        let p = penalty(&h).value();
        let o = p.waiver_outcome().unwrap();
        assert!(
            o.refused() && o.reason().contains("reasonable cause"),
            "{name}"
        );
        assert!(p.total().0 != 0 && p.if_waived() == p.total(), "{name}");
    }
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
