//! Utah's prepayment rules for 2026, checked against the Tax Commission's own
//! documents and the Utah Code. Ported from estimated_test.go.
//!
//! Utah publishes no worked example of the extension penalty. The oracle is
//! Publication 58's formula, (underpayment) x .24 x days / 365 with a 12% cap,
//! computed by hand for the cases below. The boundaries and the exceptions are
//! hand-written cases. The Go test that compares every figure with its extract
//! in testdata/ reads the Go package's citations and is not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status, WaiverCause,
    WaiverKind,
};
use money::{DOLLAR, USCents, percent};
use taxcalcs_utahtaxcalc as utahtaxcalc;
use working::{PositionKind, Reading, Required};

const UTAH: estimated::Jurisdiction = estimated::UTAH;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    utahtaxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

fn due() -> Date {
    day(2027, 4, 15)
}

fn ends() -> Date {
    day(2027, 10, 15)
}

#[derive(Clone, Default)]
struct Household {
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extends: Option<bool>,
    return_filed: Date,
    waiver: bool,
    positions: Option<Positions>,
}

impl Household {
    fn facts(&self) -> Facts {
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        let mut b = estimated::new_facts(UTAH, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(fb.build().unwrap())
            .paid(&self.payments)
            .return_filed(self.return_filed);
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
        if let Some(e) = self.extends {
            b.state(
                utahtaxcalc::new_estimated_facts()
                    .files_on_extension(e)
                    .build()
                    .unwrap(),
            );
        }
        if self.waiver {
            b.positions(
                estimated::new_positions()
                    .waiver(WaiverKind::None)
                    .build()
                    .unwrap(),
            );
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed 2025 return with its Utah tax liability.
fn prior(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .build()
            .unwrap(),
    )
}

fn not_filed() -> Option<PriorReturn> {
    Some(estimated::new_prior_return().filed(false).build().unwrap())
}

fn h(tax: i64) -> Household {
    Household {
        tax: DOLLAR * tax,
        extends: Some(true),
        ..Household::default()
    }
}

// The one date is April 15, 2027, a Thursday, and the extension ends on
// Friday, October 15, 2027. Neither moves.
#[test]
fn schedule_is_the_one_due_date() {
    let s = rules()
        .schedule(
            &Household {
                withheld: DOLLAR * 9_000,
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    assert_eq!(s.len(), 1);
    assert_eq!(s[0].due(), due());
    assert_eq!(s[0].needed_by(), DOLLAR * 9_000);
    assert_eq!(s[0].status(), Status::Met);
    for d in [due(), ends()] {
        let wd = d.weekday();
        assert!(
            wd != estimated::Weekday::SATURDAY && wd != estimated::Weekday::SUNDAY,
            "{d} falls on a {wd}"
        );
    }
    // The extension's end is where an unfiled return's penalty stops.
    let p = rules()
        .penalty(&h(10_000).facts(), day(2027, 12, 31))
        .unwrap()
        .value();
    assert_eq!(p.as_of(), ends());
}

// The minimum is met at exactly the lesser figure, and missed a cent below.
#[test]
fn threshold_boundaries() {
    for (withheld, met) in [(899_999, false), (900_000, true)] {
        let hh = Household {
            withheld: USCents(withheld),
            ..h(10_000)
        };
        let req = rules().requirement(&hh.facts()).unwrap().value();
        assert!(
            req.required() != met && req.target() == DOLLAR * 9_000,
            "withheld {withheld}: required {}, target {} ({})",
            req.required(),
            req.target(),
            req.why()
        );
        let i = rules().schedule(&hh.facts()).unwrap().value()[0];
        assert_eq!(i.status() == Status::Met, met, "withheld {withheld}");
        let pen = rules().penalty(&hh.facts(), ends()).unwrap().value();
        assert_eq!(pen.total().0 == 0, met, "withheld {withheld}");
    }
    // A prepayment on the due date counts; one the day after does not.
    for (on, met) in [(due(), true), (due().add_days(1), false)] {
        let hh = Household {
            payments: vec![pay(on, DOLLAR * 9_000)],
            ..h(10_000)
        };
        let got = rules().schedule(&hh.facts()).unwrap().value()[0].status() == Status::Met;
        assert_eq!(got, met, "paid {on}");
    }
}

// The two options, each chosen when it is the lesser, and each when it is
// unavailable.
#[test]
fn target_options() {
    let cases = [
        (
            "90% is the lesser",
            Household {
                prior: prior(DOLLAR * 12_000),
                ..h(10_000)
            },
            DOLLAR * 9_000,
            Basis::ThisYearTax,
            PositionKind::Unspecified,
        ),
        (
            "100% of 2025 is less",
            Household {
                prior: prior(DOLLAR * 8_000),
                ..h(10_000)
            },
            DOLLAR * 8_000,
            Basis::PriorYearTax,
            PositionKind::Unspecified,
        ),
        (
            "no 2025 return on record",
            h(10_000),
            DOLLAR * 9_000,
            Basis::ThisYearTax,
            PositionKind::PriorYearReturn,
        ),
        (
            "no 2025 return filed",
            Household {
                prior: not_filed(),
                ..h(10_000)
            },
            DOLLAR * 9_000,
            Basis::ThisYearTax,
            PositionKind::UnsettledRule,
        ),
        (
            "this year's tax not given",
            Household {
                no_tax: true,
                prior: prior(DOLLAR * 8_000),
                ..h(0)
            },
            DOLLAR * 8_000,
            Basis::PriorYearTax,
            PositionKind::ThisYearTax,
        ),
    ];
    for (name, hh, target, basis, ask) in cases {
        let res = rules().requirement(&hh.facts()).unwrap();
        let r = res.value();
        let c = r.chosen();
        assert!(
            c.as_ref().is_some_and(|c| c.basis() == basis) && r.target() == target,
            "{name}: chose {c:?} for {}",
            r.target()
        );
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}: {ask} is not asked");
        }
        let opts = r.options();
        assert!(
            opts.len() == 2
                && opts[0].percent() == percent(90)
                && opts[1].percent() == percent(100),
            "{name}: options {opts:?}"
        );
    }
    // Without this year's tax the 90% option says why it is unavailable.
    let o = rules()
        .requirement(
            &Household {
                no_tax: true,
                prior: prior(USCents(100)),
                ..h(0)
            }
            .facts(),
        )
        .unwrap()
        .value()
        .options()[0]
        .clone();
    assert!(!o.available());
    assert_eq!(o.why_not(), estimated::NEEDS_THIS_YEAR_TAX);
    // With neither, there is no minimum, and nothing to schedule or charge.
    let f = Household {
        no_tax: true,
        ..h(0)
    }
    .facts();
    let r = rules().requirement(&f).unwrap().value();
    assert!(!r.target_known() && !r.required());
    assert!(
        rules()
            .schedule(&f)
            .unwrap_err()
            .is(&estimated::Error::NeedsThisYearTax)
    );
    assert!(
        rules()
            .penalty(&f, ends())
            .unwrap_err()
            .is(&estimated::Error::NeedsThisYearTax)
    );
}

// The test applies only to an extended return. Filing by the due date, or
// saying the filer will, takes it away; saying nothing assumes the extension
// and asks.
#[test]
fn only_an_extended_return_is_tested() {
    let tax = DOLLAR * 10_000;
    let base = Household {
        tax,
        ..Household::default()
    };
    let cases = [
        (
            "will extend",
            Household {
                extends: Some(true),
                ..base.clone()
            },
            false,
            false,
        ),
        (
            "will not extend",
            Household {
                extends: Some(false),
                ..base.clone()
            },
            true,
            false,
        ),
        ("not said", base.clone(), false, true),
        (
            "filed by April 15",
            Household {
                return_filed: due(),
                ..base.clone()
            },
            true,
            false,
        ),
        (
            "filed the day after",
            Household {
                return_filed: due().add_days(1),
                ..base.clone()
            },
            false,
            true,
        ),
        (
            "not said, filed on time",
            Household {
                return_filed: day(2027, 3, 1),
                ..base.clone()
            },
            true,
            false,
        ),
    ];
    for (name, hh, exempt, ask) in cases {
        let res = rules().requirement(&hh.facts()).unwrap();
        let r = res.value();
        assert!(
            r.exempt() == exempt && r.required() != exempt,
            "{name}: exempt {}, required {} ({})",
            r.exempt(),
            r.required(),
            r.why()
        );
        if exempt {
            let c = r.chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && r.target().0 == 0
                    && !c.label().is_empty(),
                "{name}: chosen {c:?}"
            );
        }
        assert!(
            r.why()
                .starts_with("Utah does not require estimated payments."),
            "{name}: {}",
            r.why()
        );
        assert_eq!(
            has(&res.required(), PositionKind::JurisdictionFact),
            ask,
            "{name}"
        );
        let p = rules().penalty(&hh.facts(), ends()).unwrap().value();
        assert_eq!(p.total().0 == 0, exempt, "{name}: penalty {}", p.total());
    }
    // Filing by the due date is the exception, and the penalty's reason.
    let f = Household {
        return_filed: due(),
        ..base
    }
    .facts();
    let c = rules().requirement(&f).unwrap().value().chosen().unwrap();
    assert_eq!(c.label(), "Return filed and paid by the due date");
    assert_eq!(
        rules().penalty(&f, ends()).unwrap().value().reason(),
        "Return filed and paid by the due date."
    );
}

// Publication 58's formula: $10,000 of tax with $5,000 withheld misses the
// $9,000 minimum, so the whole $5,000 unpaid is charged. Filed June 14, 2027,
// 60 days: $5,000 × .24 × 60 / 365 = $197.26.
#[test]
fn penalty_arithmetic() {
    let mut hh = Household {
        withheld: DOLLAR * 5_000,
        return_filed: day(2027, 6, 14),
        ..h(10_000)
    };
    let res = rules().penalty(&hh.facts(), ends()).unwrap();
    let p = res.value();
    let lines = p.lines();
    assert!(
        lines.len() == 1 && lines[0].shortfall() == DOLLAR * 5_000 && lines[0].days() == 60,
        "lines {lines:?}"
    );
    let parts = lines[0].parts();
    assert!(
        parts.len() == 1 && parts[0].rate() == percent(24) && parts[0].charge() == USCents(19_726),
        "parts {parts:?}"
    );
    assert_eq!(p.total(), USCents(19_726));
    assert_eq!(p.as_of(), day(2027, 6, 14));
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));

    // A payment after the due date does not shorten it: the penalty runs
    // until the return is filed.
    hh.payments = vec![pay(day(2027, 5, 1), DOLLAR * 5_000)];
    assert_eq!(
        rules()
            .penalty(&hh.facts(), ends())
            .unwrap()
            .value()
            .total(),
        USCents(19_726)
    );

    // Unfiled to the end of the extension: 183 days would be 12.03%, so the
    // 12% cap, $600.
    let mut unfiled = Household {
        withheld: DOLLAR * 5_000,
        ..h(10_000)
    };
    let total = |hh: &Household, d: Date| rules().penalty(&hh.facts(), d).unwrap().value().total();
    assert_eq!(total(&unfiled, day(2027, 12, 31)), DOLLAR * 600);
    // 182 days, to October 14: $5,000 × .24 × 182 / 365 = $598.36, under the
    // cap.
    assert_eq!(total(&unfiled, day(2027, 10, 14)), USCents(59_836));
    // Nothing accrues by the due date itself.
    assert_eq!(total(&unfiled, due()), USCents(0));
    // Answering the waiver question removes it.
    unfiled.waiver = true;
    let res = rules().penalty(&unfiled.facts(), ends()).unwrap();
    assert!(!has(&res.required(), PositionKind::PenaltyWaiver));
}

// Utah's only waiver is for reasonable cause, with no window or age, so a
// waiver the federal rules would allow or refuse is left to the Tax
// Commission, and the open point is reported.
#[test]
fn waiver_is_reasonable_cause_only() {
    let cases = [
        (
            "a disaster",
            estimated::new_positions()
                .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
                .build()
                .unwrap(),
        ),
        (
            "retired at 64",
            estimated::new_positions()
                .waiver_for(WaiverCause::Retirement, day(2026, 6, 1))
                .born(day(1962, 3, 1))
                .build()
                .unwrap(),
        ),
        (
            "retired in 2020",
            estimated::new_positions()
                .waiver_for(WaiverCause::Retirement, day(2020, 6, 1))
                .born(day(1955, 3, 1))
                .build()
                .unwrap(),
        ),
    ];
    for (name, pos) in cases {
        let hh = Household {
            withheld: DOLLAR * 5_000,
            return_filed: day(2027, 6, 14),
            positions: Some(pos),
            ..h(10_000)
        };
        let res = rules().penalty(&hh.facts(), ends()).unwrap();
        let p = res.value();
        let o = p.waiver_outcome();
        assert!(
            p.total() == USCents(19_726)
                && o.as_ref()
                    .is_some_and(|o| o.undecided() && o.reason().contains("reasonable cause")),
            "{name}: penalty {}, outcome {o:?}",
            p.total()
        );
        assert!(
            p.if_waived() == p.total() && has(&res.required(), PositionKind::UnsettledRule),
            "{name}"
        );
    }
}

// Filing by the due date sets the minimum at nothing, so neither figure is
// asked for.
#[test]
fn exempt_does_not_ask_for_the_figures() {
    let res = rules()
        .requirement(
            &Household {
                no_tax: true,
                extends: Some(false),
                ..Household::default()
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().exempt());
    assert!(!has(&res.required(), PositionKind::PriorYearReturn));
    assert!(!has(&res.required(), PositionKind::ThisYearTax));
}

// Without this year's tax a missed minimum cannot be charged, because the
// charge is on the whole unpaid tax; a met one is an exact zero.
#[test]
fn penalty_without_this_years_tax() {
    let mut hh = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000),
        withheld: DOLLAR * 7_999,
        ..h(0)
    };
    assert!(
        rules()
            .penalty(&hh.facts(), ends())
            .unwrap_err()
            .is(&estimated::Error::NeedsThisYearTax)
    );
    hh.withheld = DOLLAR * 8_000;
    let p = rules().penalty(&hh.facts(), ends()).unwrap().value();
    assert!(p.total().0 == 0 && !p.upper_bound());
}

// The exceptions: a 2025 return with no tax sets the minimum at nothing, and
// a return filed after the extension falls under penalties not built.
#[test]
fn exceptions() {
    let r = rules()
        .requirement(
            &Household {
                prior: prior(USCents(0)),
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    assert!(r.target().0 == 0 && !r.required());
    let late = Household {
        return_filed: ends().add_days(1),
        ..h(10_000)
    };
    assert!(
        rules()
            .penalty(&late.facts(), day(2027, 12, 31))
            .unwrap_err()
            .is(&estimated::Error::Unsupported)
    );
    let on_time = Household {
        return_filed: ends(),
        ..h(10_000)
    };
    assert_eq!(
        rules()
            .penalty(&on_time.facts(), day(2027, 12, 31))
            .unwrap()
            .value()
            .total(),
        DOLLAR * 1_200
    );
}

// Each divergence is reported with both readings and both sources, and the
// figures follow the reading this package chose.
#[test]
fn divergences_pin_both_readings() {
    let res = rules().requirement(&h(10_000).facts()).unwrap();
    let want = [
        (
            "Last year's tax was zero",
            "TC-546 and Publication 58",
            "§ 59-10-516(2)(b)",
        ),
        (
            "No return last year",
            "90% of this year's tax",
            "§ 59-10-516(2)(b)",
        ),
        (
            "By the day or by the month",
            "24% a year by the day",
            "§ 59-1-401(5)(b)",
        ),
        (
            "What the penalty is charged on",
            "whole tax unpaid",
            "§§ 59-1-401(5)(b), 59-10-516(3)",
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
    }
    // The penalty is charged on the whole unpaid tax, the statute's reading;
    // the rest follow the forms. With nothing paid the minimum is missed, so
    // the penalty readings affect these facts and the last-year ones do not.
    for d in res.divergences() {
        let uses = if d.rule() == "What the penalty is charged on" {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert_eq!(d.uses(), uses, "{:?}", d.rule());
        let penalty = d.rule() == "By the day or by the month"
            || d.rule() == "What the penalty is charged on";
        assert_eq!(d.affects_household(), penalty, "{:?}", d.rule());
    }
    // A filer who met the minimum and filed last year with tax is touched by
    // none of them.
    let met = rules()
        .requirement(
            &Household {
                withheld: DOLLAR * 9_000,
                prior: prior(DOLLAR * 12_000),
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap();
    for d in met.divergences() {
        assert!(!d.affects_household(), "met the minimum: {:?}", d.rule());
    }
    let target = |hh: Household| rules().requirement(&hh.facts()).unwrap().value().target();
    // Zero last year: the TC-40 instructions' reading would be $9,000; the
    // worksheet's is nothing.
    assert_eq!(
        target(Household {
            prior: prior(USCents(0)),
            ..h(10_000)
        }),
        USCents(0)
    );
    // No return last year: Publication 58's 90%, not the statute's zero.
    assert_eq!(
        target(Household {
            prior: not_filed(),
            ..h(10_000)
        }),
        DOLLAR * 9_000
    );
    // 60 days: by the day $197.26; two months or part at 2% would be $200.
    // On the shortfall below the minimum, $4,000, it would be $157.81.
    let hh = Household {
        withheld: DOLLAR * 5_000,
        return_filed: day(2027, 6, 14),
        ..h(10_000)
    };
    assert_eq!(
        rules()
            .penalty(&hh.facts(), ends())
            .unwrap()
            .value()
            .total(),
        USCents(19_726)
    );
}

#[test]
fn refuses_what_is_not_built() {
    assert!(
        utahtaxcalc::estimated_tax(2025)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NoRules))
    );
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let cases: [(&str, Facts); 3] = [
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
            estimated::new_facts(UTAH, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(UTAH, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("not Utah's")
                .build()
                .unwrap(),
        ),
    ];
    for (name, f) in cases {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    let f = Household {
        tax: USCents(100),
        ..Household::default()
    }
    .facts();
    assert!(rules().penalty(&f, Date::default()).is_err());
}

#[test]
fn explanation_names_the_differences() {
    let e = rules().explanation();
    let text = format!(
        "{} {} {}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "no estimated tax",
        "April 15, 2027",
        "October 15, 2027",
        "90%",
        "100%",
        "2% a month",
        "12%",
        "whole unpaid balance",
        "no 110% tier",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
}

// $3,600 withheld on $3,000 of tax never reads as a negative amount. Utah's
// prepayment test compares what was paid with the minimum, so no refund is
// stated.
#[test]
fn withholding_over_the_tax_has_no_negative_amount() {
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
    let withheld: Vec<_> = (1..=12)
        .map(|m| {
            estimated::new_withheld()
                .on(day(2026, m, 28))
                .amount(DOLLAR * 300)
                .build()
                .unwrap()
        })
        .collect();
    let f = estimated::new_facts(UTAH, 2026)
        .filing_status(filing::Status::Single)
        .this_year(figures(DOLLAR * 3_000))
        .prior_year(last)
        .withheld(&withheld)
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    let r = res.value();
    let mut texts = vec![r.why()];
    for o in r.options() {
        texts.extend([o.label(), o.why_not()]);
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
            "a negative amount where a refund belongs: {s:?}"
        );
    }
}
