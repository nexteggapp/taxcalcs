//! New Mexico's estimated tax rules for 2026, checked against the Taxation
//! and Revenue Department's own documents and the statute.
//!
//! New Mexico publishes no worked penalty example. Form RPD-41272 defines
//! the line arithmetic, and these tests follow it: each part of each
//! shortfall, by the day ÷ 365, at each quarter's rate. The cases are
//! hand-written. Go's TestEstimatedFiguresAreInTheirSources, which reads the
//! testdata/ extracts, is not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, Elections, EstimatedTax, Facts, Month, Payment, Positions, PositionsBuilder,
    PriorReturn, Status, WaiverCause, Weekday,
};
use money::{DOLLAR, Rate, USCents, dollars, percent};
use taxcalcs_newmexicotaxcalc as newmexicotaxcalc;
use working::{PositionKind, Reading, Required};

use newmexicotaxcalc::EstimatedFacts;

const NM: estimated::Jurisdiction = estimated::NEW_MEXICO;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    newmexicotaxcalc::estimated_tax(2026).unwrap()
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
    first_met: Date,
    return_filed: Date,
    elections: Option<Elections>,
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
        let mut b = estimated::new_facts(NM, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(figures)
            .paid(&self.payments)
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
                newmexicotaxcalc::new_estimated_facts()
                    .first_year_resident(false)
                    .build()
                    .unwrap(),
            ),
        };
        if let Some(e) = self.elections {
            b.elections(e);
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

fn prior(tax: USCents, resident: bool) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .full_year_resident(resident)
            .build()
            .unwrap(),
    )
}

fn penalty(h: &Household) -> working::Result<estimated::Penalty> {
    rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap()
}

/// Nothing paid on $9,000 required. April's $2,250 runs 76 days at 6% to
/// June 30 and 184 days at 7% to December 31; the rate changes on July 1, so
/// the shortfall is charged in two parts. The days from January 1, 2027 have
/// no published rate.
#[test]
fn estimated_penalty_arithmetic() {
    let res = penalty(&tax(DOLLAR * 10_000));
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    let want: [(&[i64], &[Rate], &[i64]); 4] = [
        (&[76, 184], &[percent(6), percent(7)], &[2_811, 7_940]),
        (&[15, 184], &[percent(6), percent(7)], &[555, 7_940]),
        (&[107], &[percent(7)], &[4_617]),
        (&[], &[], &[]),
    ];
    for (i, l) in lines.iter().enumerate() {
        let parts = l.parts();
        assert_eq!(parts.len(), want[i].0.len(), "installment {}", i + 1);
        for (j, p) in parts.iter().enumerate() {
            assert_eq!(
                (p.days(), p.rate(), p.charge().0),
                (want[i].0[j], want[i].1[j], want[i].2[j]),
                "installment {} part {j}",
                i + 1
            );
        }
    }
    assert_eq!(res.value().total(), USCents(23_863));
    assert!(because(
        &res.required(),
        PositionKind::UnpublishedRate,
        "December 31, 2026"
    ));
    let p = rules()
        .penalty(&tax(DOLLAR * 10_000).facts(), day(2026, 12, 31))
        .unwrap();
    assert!(
        p.value().total() == USCents(23_863) && !has(&p.required(), PositionKind::UnpublishedRate)
    );
}

/// A payment stops the charge on what it pays: April's $2,250 paid May 1
/// runs 16 days at 6%, $5.92.
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
        l.len() == 1 && l[0].days() == 16 && res.value().total() == USCents(592),
        "penalty {} over {:?}",
        res.value().total(),
        l
    );
}

/// The dates the statute gives, a quarter each, and none moves.
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

/// A filer first required to pay after March 31 catches up: 50% by June 15
/// from April 1, 75% by September 15 from June 1, all by January 15 from
/// September 1.
#[test]
fn estimated_late_start() {
    for (met, need) in [
        (day(2026, 3, 31), [225_000, 450_000, 675_000, 900_000]),
        (day(2026, 4, 1), [0, 450_000, 675_000, 900_000]),
        (day(2026, 5, 31), [0, 450_000, 675_000, 900_000]),
        (day(2026, 6, 1), [0, 0, 675_000, 900_000]),
        (day(2026, 9, 1), [0, 0, 0, 900_000]),
    ] {
        let h = Household {
            first_met: met,
            ..tax(DOLLAR * 10_000)
        };
        let s = rules().schedule(&h.facts()).unwrap().value();
        for (i, want) in need.iter().enumerate() {
            assert_eq!(
                s[i].needed_by(),
                USCents(*want),
                "first met {met}: installment {}",
                i + 1
            );
        }
    }
    let p = penalty(&tax(DOLLAR * 10_000));
    assert!(has(&p.required(), PositionKind::RequirementFirstMet));
}

/// Tax less withholding under $1,000 owes nothing; exactly $1,000 can.
#[test]
fn estimated_threshold_boundary() {
    for (withheld, exempt) in [(900_001, true), (900_000, false)] {
        let h = Household {
            withheld: USCents(withheld),
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {withheld}");
    }
    let h = Household {
        withheld: USCents(900_001),
        ..tax(DOLLAR * 10_000)
    };
    let c = rules()
        .requirement(&h.facts())
        .unwrap()
        .value()
        .chosen()
        .unwrap();
    assert!(
        c.basis() == Basis::Exception
            && c.amount().0 == 0
            && c.label() == "Tax less withholding under $1,000"
    );
    // Exactly $1,000 with nothing paid: the department's page says no
    // penalty, the form and the statute say there can be one.
    let h = Household {
        withheld: USCents(900_000),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    for d in res.divergences() {
        if d.rule() == "The $1,000 test" {
            assert!(d.affects_household(), "exactly $1,000");
        }
    }
}

/// The lesser of 90% of this year's tax and 100% of last year's, from a
/// filed twelve-month New Mexico return.
#[test]
fn estimated_target_options() {
    let h = Household {
        prior: prior(DOLLAR * 8_999, true),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearTax);
    assert_eq!(r.target(), DOLLAR * 8_999);
    let h = Household {
        prior: prior(DOLLAR * 9_001, true),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert_eq!(r.chosen().unwrap().basis(), Basis::ThisYearTax);
    assert_eq!(r.target(), DOLLAR * 9_000);
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

/// First-year residents and full-year residents with no liability last year
/// owe nothing.
#[test]
fn estimated_exemptions() {
    let first = newmexicotaxcalc::new_estimated_facts()
        .first_year_resident(true)
        .build()
        .unwrap();
    let h = Household {
        extras: Some(first),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(r.exempt() && r.why().contains("first year as a New Mexico resident"));
    let c = r.chosen().unwrap();
    assert!(
        c.basis() == Basis::Exception
            && c.amount().0 == 0
            && c.label() == "First year as a New Mexico resident"
    );
    assert_eq!(
        penalty(&h).value().reason(),
        "First year as a New Mexico resident."
    );
    let h = Household {
        prior: prior(USCents(0), true),
        ..tax(DOLLAR * 10_000)
    };
    assert!(rules().requirement(&h.facts()).unwrap().value().exempt());
    let h = Household {
        prior: prior(USCents(0), false),
        ..tax(DOLLAR * 10_000)
    };
    assert!(!rules().requirement(&h.facts()).unwrap().value().exempt());
    let unanswered = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(USCents(0)).build().unwrap())
        .build()
        .unwrap();
    let h = Household {
        prior: Some(unanswered),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().exempt() && has(&res.required(), PositionKind::Residency));
    let none = newmexicotaxcalc::new_estimated_facts().build().unwrap();
    let h = Household {
        extras: Some(none),
        ..tax(DOLLAR * 10_000)
    };
    let p = penalty(&h);
    assert!(
        because(
            &p.required(),
            PositionKind::JurisdictionFact,
            "first year as a New Mexico resident"
        ) && has(&p.required(), PositionKind::PenaltyWaiver)
    );
}

/// January 31, 2027 is a Sunday and no source says whether it moves.
#[test]
fn estimated_january_thirty_first() {
    for (filed, moves, excused, asked) in [
        (day(2027, 1, 29), false, true, false),
        (day(2027, 2, 1), false, false, true),
        (day(2027, 2, 1), true, true, false),
        (day(2027, 2, 2), true, false, false),
    ] {
        let mut eb = newmexicotaxcalc::new_estimated_facts();
        eb.first_year_resident(false);
        if moves {
            eb.january_thirty_first_moves(true);
        }
        let h = Household {
            return_filed: filed,
            extras: Some(eb.build().unwrap()),
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().schedule(&h.facts()).unwrap();
        assert_eq!(
            res.value()[3].status() == Status::Excused,
            excused,
            "filed {filed}, moves {moves}"
        );
        assert_eq!(
            because(&res.required(), PositionKind::UnsettledRule, "a Sunday"),
            asked,
            "filed {filed}, moves {moves}"
        );
    }
}

/// Withholding counts in equal parts unless the filer shows the dates.
#[test]
fn estimated_withholding() {
    let h = Household {
        withheld: DOLLAR * 9_000,
        ..tax(DOLLAR * 10_000)
    };
    assert_eq!(penalty(&h).value().total().0, 0);
    let as_withheld = estimated::new_elections()
        .withholding_as_withheld()
        .build()
        .unwrap();
    let h = Household {
        elections: Some(as_withheld),
        ..h
    };
    assert_ne!(penalty(&h).value().total().0, 0);
}

/// Without this year's tax, the target is last year's alone and the penalty
/// is the most it can be.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000, true),
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
    let p = penalty(&h).value();
    assert!((p.upper_bound() || !p.rated_through().is_zero()) && p.total().0 != 0);
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
            "Paying the required annual payment",
            "timely installments",
            "(H)(3)",
        ),
        ("The withholding line 4 compares", "2024 PIT-1", "(H)(1)"),
        ("The $1,000 test", "more than $1,000", "(H)(1)"),
        ("An annualized method", "late start", "§ 7-2-12.2(C)"),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let w = want
            .iter()
            .find(|w| w.0 == d.rule())
            .unwrap_or_else(|| panic!("unexpected divergence {:?}", d.rule()));
        assert!(
            d.form().contains(w.1)
                && d.statute_source().contains(w.2)
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{:?} does not pin both readings",
            d.rule()
        );
        // $10,000 due, nothing withheld or paid, not annualized: no reading
        // changes the figures.
        let want_uses = if d.rule() == "The withholding line 4 compares" {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert!(
            d.uses() == want_uses && !d.affects_household(),
            "{:?}",
            d.rule()
        );
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    let err = newmexicotaxcalc::estimated_tax(2025).err().unwrap();
    assert!(err.is(&estimated::Error::NoRules), "{err}");
    let h = Household {
        farm_share: Rate(7000),
        ..tax(DOLLAR * 10_000)
    };
    let err = rules().requirement(&h.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::Unsupported), "{err}");
    let last = newmexicotaxcalc::new_estimated_facts()
        .farmer_last_year(true)
        .build()
        .unwrap();
    let h = Household {
        extras: Some(last),
        ..tax(DOLLAR * 10_000)
    };
    let err = rules().requirement(&h.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::Unsupported), "{err}");
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
    let h = Household {
        fish_share: Rate(7000),
        ..tax(DOLLAR * 10_000)
    };
    assert!(rules().requirement(&h.facts()).is_ok());
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
            estimated::new_facts(NM, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(NM, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state(Vec::<i64>::new())
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
    for s in ["$1,000", "110%", "March 31", "6%", "7%", "April 1 to 15"] {
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
    let f = estimated::new_facts(NM, 2026)
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

/// New Mexico has no casualty, retirement or disability waiver. Its one
/// waiver, NMSA 1978 § 7-2-12.2(H)(4), turns on the department finding no
/// fraud, negligence or disregard, so a retirement the federal rules would
/// waive is undecided.
#[test]
fn estimated_waiver_outcomes() {
    let mut h = tax(DOLLAR * 10_000);
    let before = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value()
        .total();
    assert_ne!(before.0, 0, "no penalty to waive");
    let born = day(1962, 3, 1);
    let pos = |c: WaiverCause, on: Date, born: Option<Date>| {
        let mut b: PositionsBuilder = estimated::new_positions();
        b.waiver_for(c, on);
        if let Some(d) = born {
            b.born(d);
        }
        b.build().unwrap()
    };
    for (name, want, phrase, p) in [
        (
            "retired in 2026 at 64",
            "undecided",
            "no waiver for retirement or disability",
            pos(WaiverCause::Retirement, day(2026, 6, 1), Some(born)),
        ),
        (
            "retired in 2020",
            "undecided",
            "fraud, negligence or disregard",
            pos(WaiverCause::Retirement, day(2020, 6, 1), Some(born)),
        ),
        (
            "disaster",
            "undecided",
            "no waiver for a casualty or disaster as such",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
        ),
    ] {
        h.positions = Some(p);
        let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        let p = res.value();
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
        assert!(
            got == want
                && o.reason().contains(phrase)
                && p.total() == before
                && p.if_waived() == waived,
            "{name}: {got}, {:?}",
            o.reason()
        );
        assert!(has(&res.required(), PositionKind::UnsettledRule), "{name}");
    }
}
