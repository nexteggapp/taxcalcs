//! Ported from newyorktaxcalc/estimated_test.go: New York's estimated tax
//! rules for 2026, checked against the Department of Taxation and Finance's
//! own documents and the Consolidated Laws.
//!
//! New York publishes no worked penalty example for Part 3 of Form
//! IT-2105.9, the regular method; the short method's one example uses the
//! 2025 rate. The tests follow the instructions' arithmetic: each part of
//! each shortfall, by the day ÷ 365, at each period's rate, a period ending
//! at the payment, the end of the rate quarter or the next due date. The
//! cases are hand-written. The Go test that compares every figure against
//! its extract in testdata/ is not ported: it checks the Go package's
//! citations, not the rules.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, Elections, EstimatedTax, Facts, Jurisdiction, Month, Payment, Penalty,
    PositionsBuilder, PriorReturn, Status as InstallmentStatus, WaiverCause, Weekday,
};
use filing::Status;
use money::{DOLLAR, Rate, USCents, dollars};
use taxcalcs_newyorktaxcalc::{EstimatedFacts, estimated_tax, new_estimated_facts};
use working::{PositionKind, Reading, Required};

const NY: Jurisdiction = Jurisdiction::from_static("NY");

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
    local: USCents,
    no_tax: bool,
    status: Status,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    return_filed: Date,
    elections: Option<Elections>,
    annualised: Option<Vec<USCents>>,
    farm_share: Rate,
    fish_share: Rate,
    positions: Option<estimated::Positions>,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let fb = if self.no_tax {
            estimated::new_figures()
        } else {
            let mut fb = estimated::new_figures();
            fb.tax(self.tax).local_tax(self.local);
            fb
        };
        let status = if self.status == Status::Unspecified {
            Status::Single
        } else {
            self.status
        };
        let mut b = estimated::new_facts(NY, 2026);
        b.filing_status(status)
            .this_year(fb.build().unwrap())
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
        if let Some(p) = &self.prior {
            b.prior_year(*p);
        }
        match self.extras {
            Some(e) => b.state(e),
            None => b.state(
                new_estimated_facts()
                    .subject_to(false, false, false)
                    .build()
                    .unwrap(),
            ),
        };
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
                .map(|(i, &t)| {
                    estimated::new_period()
                        .end(ends[i])
                        .annualised_tax(t)
                        .build()
                        .unwrap()
                })
                .collect();
            b.periods(&periods);
            b.elections(estimated::new_elections().annualised().build().unwrap());
        }
        if let Some(p) = &self.positions {
            b.positions(*p);
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
                    .state_agi(agi)
                    .build()
                    .unwrap(),
            )
            .full_year_resident(true)
            .build()
            .unwrap(),
    )
}

fn penalty(h: &Household) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap()
}

fn subject(nyc: bool, yonkers: bool, mctmt: bool) -> Option<EstimatedFacts> {
    Some(
        new_estimated_facts()
            .subject_to(nyc, yonkers, mctmt)
            .build()
            .unwrap(),
    )
}

/// Nothing paid on $9,000 required. April's $2,250 is charged in five
/// periods: 61 and 15 days at 8.5%, then from July 1 at 9.5% for 77, 15 and
/// 92 days; the rate change splits it. The days from January 1, 2027 have
/// no published rate.
#[test]
fn estimated_penalty_arithmetic() {
    let res = penalty(&tax(DOLLAR * 10_000));
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    let april = lines[0].parts();
    let want_days = [61, 15, 77, 15, 92];
    let want_rates = [850, 850, 950, 950, 950];
    let want_charges = [3_196, 786, 4_509, 878, 5_388];
    assert_eq!(april.len(), want_days.len());
    for (i, p) in april.iter().enumerate() {
        assert_eq!(
            (p.days(), p.rate(), p.charge()),
            (want_days[i], Rate(want_rates[i]), USCents(want_charges[i])),
            "April part {i}"
        );
    }
    for (i, want) in [14_757, 11_561, 6_266, 0].into_iter().enumerate() {
        assert_eq!(lines[i].total(), USCents(want), "installment {}", i + 1);
    }
    assert_eq!(res.value().total(), USCents(32_584));
    assert!(because(
        &res.required(),
        PositionKind::UnpublishedRate,
        "December 31, 2026"
    ));
    assert!(
        has(&res.required(), PositionKind::PenaltyWaiver),
        "the waiver is not asked"
    );
}

/// A payment stops the charge on what it pays: April's $2,250 paid May 1
/// runs 16 days at 8.5%, $8.38.
#[test]
fn estimated_payment_stops_the_charge() {
    let d = dues();
    let h = Household {
        payments: vec![
            pay(day(2026, 5, 1), DOLLAR * 2_250),
            pay(d[1], DOLLAR * 2_250),
            pay(d[2], DOLLAR * 2_250),
            pay(d[3], DOLLAR * 2_250),
        ],
        ..tax(DOLLAR * 10_000)
    };
    let res = penalty(&h);
    let l = res.value().lines();
    assert!(
        l.len() == 1 && l[0].days() == 16 && res.value().total() == USCents(838),
        "penalty {}",
        res.value().total()
    );
}

/// The dates § 685(c)(1) gives, a quarter each, and none moves. January 31,
/// 2027 is a Sunday: IT-2105-I prints the date and does not move it, so a
/// return filed on February 1 does not take the penalty off January.
#[test]
fn estimated_schedule_for_twenty_twenty_six() {
    let s = rules()
        .schedule(&tax(DOLLAR * 10_000).facts())
        .unwrap()
        .value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.due(), dues()[i], "installment {}", i + 1);
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
        assert_eq!(
            s[3].status() == InstallmentStatus::Excused,
            excused,
            "filed {filed}"
        );
    }
}

/// $300 for each of the state, City and Yonkers taxes; the MCTMT adds none.
#[test]
fn estimated_threshold_boundaries() {
    for (extras, withheld, exempt) in [
        (subject(false, false, false), 970_001, true),
        (subject(false, false, false), 970_000, false),
        (subject(true, false, false), 940_001, true),
        (subject(true, false, false), 940_000, false),
        (subject(true, true, false), 910_001, true),
        (subject(false, false, true), 970_000, false),
    ] {
        let h = Household {
            withheld: USCents(withheld),
            extras,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "withheld {withheld}: {}",
            res.value().why()
        );
    }
    // Under the threshold the exception is the chosen option, at $0.
    let h = Household {
        withheld: USCents(945_000),
        extras: subject(true, false, false),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::Exception);
    assert_eq!(c.amount(), USCents(0));
    assert_eq!(c.label(), "Tax less withholding under $600");
    assert!(
        res.value()
            .why()
            .contains("leaves $550. That is under $600. The threshold is $300 for each"),
        "why {:?}",
        res.value().why()
    );
    let none = new_estimated_facts().build().unwrap();
    let h = Household {
        local: DOLLAR * 1_000,
        extras: Some(none),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(
        because(
            &res.required(),
            PositionKind::JurisdictionFact,
            "New York City and Yonkers"
        ),
        "which taxes apply is not asked"
    );
}

/// 100% of last year's tax, or 110% above $150,000 of last year's New York
/// AGI ($75,000 separately) or of MCTD self-employment earnings.
#[test]
fn estimated_prior_year_step() {
    for (status, agi, want) in [
        (Status::Single, 15_000_000, DOLLAR * 8_000),
        (Status::Single, 15_000_001, DOLLAR * 8_800),
        (Status::MarriedFilingSeparately, 7_500_000, DOLLAR * 8_000),
        (Status::MarriedFilingSeparately, 7_500_001, DOLLAR * 8_800),
    ] {
        let h = Household {
            status,
            prior: prior(DOLLAR * 8_000, USCents(agi)),
            ..tax(DOLLAR * 20_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().target(), want, "{status:?}, AGI {agi}");
    }
    let mctd = new_estimated_facts()
        .subject_to(false, false, true)
        .prior_year_mctd_earnings(USCents(15_000_001))
        .build()
        .unwrap();
    let h = Household {
        prior: prior(DOLLAR * 8_000, DOLLAR * 100_000),
        extras: Some(mctd),
        ..tax(DOLLAR * 20_000)
    };
    assert_eq!(
        rules().requirement(&h.facts()).unwrap().value().target(),
        DOLLAR * 8_800
    );
    let unknown = new_estimated_facts()
        .subject_to(false, false, true)
        .build()
        .unwrap();
    let h = Household {
        prior: prior(DOLLAR * 8_000, DOLLAR * 100_000),
        extras: Some(unknown),
        ..tax(DOLLAR * 20_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_800);
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "MCTD"
    ));
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
    let h = Household {
        prior: Some(no_agi),
        ..tax(DOLLAR * 20_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_800);
    assert!(has(&res.required(), PositionKind::AdjustedGrossIncome));
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(
            estimated::new_figures()
                .tax(USCents(100))
                .state_agi(USCents(1))
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
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(
            !res.value().options()[1].available(),
            "{name}: the prior-year test is offered"
        );
    }
}

/// No liability last year excuses the penalty for a resident, or a
/// nonresident with New York source income.
#[test]
fn estimated_no_liability_exception() {
    let h = Household {
        prior: prior(USCents(0), DOLLAR * 10_000),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(res.value().exempt(), "resident: {}", res.value().why());
    let nonresident = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(USCents(0)).build().unwrap())
        .full_year_resident(false)
        .build()
        .unwrap();
    let source = new_estimated_facts()
        .subject_to(false, false, false)
        .prior_year_new_york_source_income(true)
        .build()
        .unwrap();
    let h = Household {
        prior: Some(nonresident),
        extras: Some(source),
        ..tax(DOLLAR * 10_000)
    };
    assert!(rules().requirement(&h.facts()).unwrap().value().exempt());
    let no_source = new_estimated_facts()
        .subject_to(false, false, false)
        .prior_year_new_york_source_income(false)
        .build()
        .unwrap();
    let h = Household {
        prior: Some(nonresident),
        extras: Some(no_source),
        ..tax(DOLLAR * 10_000)
    };
    assert!(!rules().requirement(&h.facts()).unwrap().value().exempt());
    let h = Household {
        prior: Some(nonresident),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().exempt() && has(&res.required(), PositionKind::Residency));
}

/// Withholding counts in equal parts unless the filer proves the dates.
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
    for (i, want) in [450, 1_800, 5_400, 9_000].into_iter().enumerate() {
        assert_eq!(s[i].needed_by(), DOLLAR * want, "installment {}", i + 1);
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
    assert!(r.required() && r.target() == DOLLAR * 8_000);
    assert_eq!(r.options()[0].why_not(), estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let p = penalty(&h).value();
    assert!((p.upper_bound() || !p.rated_through().is_zero()) && p.total().0 != 0);
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(
        err.is(&estimated::Error::NeedsThisYearTax),
        "schedule: {err}"
    );
    let err = rules()
        .penalty(&none.facts(), day(2027, 4, 15))
        .err()
        .unwrap();
    assert!(
        err.is(&estimated::Error::NeedsThisYearTax),
        "penalty: {err}"
    );
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want = [
        ("January 31, 2027 is a Sunday", "not moved", "§ 25-a"),
        (
            "Casualty or disaster",
            "no waiver for a casualty",
            "§ 685(d)(4)(A)",
        ),
        (
            "A requirement that arises late",
            "25% in every column",
            "§ 685(c)(3)(A)",
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, form, statute)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(
            d.form().contains(form)
                && d.statute_source().contains(statute)
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{:?} does not pin both readings",
            d.rule()
        );
        // No return filed, no waiver asked and an on-time start: neither
        // reading changes the figures. The casualty waiver follows the
        // statute, which grants it.
        let want = if d.rule() == "Casualty or disaster" {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert!(d.uses() == want && !d.affects_household(), "{:?}", d.rule());
    }
    // Filed and paid on Monday, February 1, 2027: only the Sunday rule
    // changes the figures.
    let h = Household {
        return_filed: day(2027, 2, 1),
        ..tax(DOLLAR * 10_000)
    };
    for d in rules().requirement(&h.facts()).unwrap().divergences() {
        assert_eq!(
            d.affects_household(),
            d.rule() == "January 31, 2027 is a Sunday",
            "filed February 1: {:?}",
            d.rule()
        );
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    let err = estimated_tax(2025).err().unwrap();
    assert!(err.is(&estimated::Error::NoRules), "2025: {err}");
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
        let err = rules().requirement(&h.facts()).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    let last = new_estimated_facts()
        .farmer_last_year(true)
        .build()
        .unwrap();
    let h = Household {
        extras: Some(last),
        ..tax(DOLLAR * 10_000)
    };
    let err = rules().requirement(&h.facts()).err().unwrap();
    assert!(
        err.is(&estimated::Error::Unsupported),
        "a farmer last year: {err}"
    );
    let h = Household {
        farm_share: Rate(1000),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "farmers' rules"
    ));
    let this = estimated::new_figures()
        .tax(USCents(100_000))
        .build()
        .unwrap();
    for (name, f) in [
        (
            "California's facts",
            estimated::new_facts(estimated::CALIFORNIA, 2026)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(NY, 2027)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(NY, 2026)
                .filing_status(Status::Single)
                .this_year(this)
                .state(())
                .build()
                .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&tax(USCents(100)).facts(), Date::default())
            .is_err()
    );
    assert!(
        new_estimated_facts()
            .prior_year_mctd_earnings(USCents(-1))
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
        "$300",
        "New York City",
        "Yonkers",
        "MCTMT",
        "110%",
        "5½ points",
        "8.5%",
        "9.5%",
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
    let f = estimated::new_facts(NY, 2026)
        .filing_status(Status::Single)
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

/// Tax Law § 685(d)(4) grants the federal pair: a casualty or disaster, and
/// retirement after 62 or disability in the tax year or the one before.
#[test]
fn estimated_waiver_outcomes() {
    let mut h = tax(DOLLAR * 10_000);
    let before = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value()
        .total();
    assert_ne!(before, USCents(0), "no penalty to waive");
    let born = day(1962, 3, 1);
    let pos = |cause: WaiverCause, on: Date, born: Option<Date>| -> PositionsBuilder {
        let mut b = estimated::new_positions();
        b.waiver_for(cause, on);
        if let Some(born) = born {
            b.born(born);
        }
        b
    };
    for (name, want, phrase, pb) in [
        (
            "retired in 2026 at 64",
            "eligible",
            "retired in 2026 after reaching 62",
            pos(WaiverCause::Retirement, day(2026, 6, 1), Some(born)),
        ),
        (
            "retired in 2025 at 63",
            "eligible",
            "retired in 2025",
            pos(WaiverCause::Retirement, day(2025, 6, 1), Some(born)),
        ),
        (
            "retired in 2020",
            "refused",
            "covers retiring in 2025 or 2026",
            pos(WaiverCause::Retirement, day(2020, 6, 1), Some(born)),
        ),
        (
            "retired at 61",
            "refused",
            "reached 62",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1965, 1, 1)),
            ),
        ),
        (
            "disaster",
            "eligible",
            "The New York Tax Department can waive",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
        ),
        (
            "disabled in 2023",
            "refused",
            "became disabled in 2023",
            pos(WaiverCause::Disability, day(2023, 11, 1), None),
        ),
    ] {
        h.positions = Some(pb.build().unwrap());
        let p = rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .value();
        let o = p.waiver_outcome().unwrap();
        let got = if o.refused() {
            "refused"
        } else if o.undecided() {
            "undecided"
        } else if o.eligible() {
            "eligible"
        } else {
            ""
        };
        let waived = if got == "eligible" {
            USCents(0)
        } else {
            before
        };
        assert!(
            got == want
                && o.reason().contains(phrase)
                && p.total() == before
                && p.if_waived() == waived,
            "{name}: {got}, {:?}, total {}, if waived {}",
            o.reason(),
            p.total(),
            p.if_waived()
        );
    }
}
