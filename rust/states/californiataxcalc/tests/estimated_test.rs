//! California's estimated tax rules for 2026. Ported from
//! estimated_test.go, worksheet_i_test.go and estimated_refund_test.go.
//!
//! Go's TestEstimatedFiguresAreInTheirSources, TestWorksheetIIsInItsSource,
//! TestDaysPerRatePeriodMatchFTB5805 (which parses its expected days from the
//! instructions' text) and the extract checks in the other tests read
//! testdata/ and are not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, ResidencePeriod,
    Status, WaiverCause, Weekday,
};
use filing::Status as FilingStatus;
use money::{DOLLAR, Rate, USCents, percent};
use num_rational::BigRational;
use num_traits::One;
use taxcalcs_californiataxcalc::{
    WorksheetIColumn, estimated_tax, new_estimated_facts, new_worksheet_i_column, worksheet_i,
};
use working::{PositionKind, Reading, Required};

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

#[derive(Clone, Default)]
struct Household {
    status: FilingStatus,
    tax: USCents,
    /// This year's California AGI.
    agi: Option<USCents>,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    group: bool,
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

impl Household {
    fn facts(&self) -> Facts {
        let status = if self.status == FilingStatus::Unspecified {
            FilingStatus::Single
        } else {
            self.status
        };
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        fb.state_agi(self.agi.unwrap_or(DOLLAR * 100_000));
        let mut b = estimated::new_facts(estimated::CALIFORNIA, 2026);
        b.filing_status(status)
            .this_year(fb.build().unwrap())
            .paid(&self.payments);
        if self.withheld.0 > 0 {
            b.withheld(&[estimated::new_withheld()
                .on(day(2026, 11, 30))
                .amount(self.withheld)
                .build()
                .unwrap()]);
        }
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        if self.group {
            b.state(new_estimated_facts().group_return().build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

fn prior(tax: USCents, agi: USCents, withheld: USCents) -> Option<PriorReturn> {
    let f = estimated::new_figures()
        .tax(tax)
        .state_agi(agi)
        .withheld(withheld)
        .build()
        .unwrap();
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(f)
            .build()
            .unwrap(),
    )
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

#[test]
fn estimated_rates_match_the_ftb_page() {
    let f = Household {
        prior: prior(DOLLAR * 40_000, DOLLAR * 100_000, USCents(0)),
        ..tax(DOLLAR * 40_000)
    }
    .facts();
    let res = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    let parts = res.value().lines()[0].parts();
    assert_eq!(parts.len(), 2);
    for (i, (to, period)) in [
        (day(2026, 6, 30), "07/01/2025 – 06/30/2026"),
        (day(2026, 12, 31), "07/01/2026 – 12/31/2026"),
    ]
    .iter()
    .enumerate()
    {
        assert_eq!(parts[i].to(), *to);
        assert_eq!(parts[i].rate(), percent(7));
        assert!(parts[i].source().ends_with(period));
    }
    assert!(has(&res.required(), PositionKind::UnpublishedRate));
}

#[test]
fn estimated_schedule_is_thirty_forty_zero_thirty() {
    // 90% of $20,000 is $18,000, less than 110% of $17,000 = $18,700.
    let f = Household {
        withheld: DOLLAR * 10_000,
        prior: prior(DOLLAR * 17_000, DOLLAR * 200_000, USCents(0)),
        ..tax(DOLLAR * 20_000)
    }
    .facts();
    let res = rules().schedule(&f).unwrap();
    let want_due = [
        day(2026, 4, 15),
        day(2026, 6, 15),
        day(2026, 9, 15),
        day(2027, 1, 15),
    ];
    let want_needed = [5_400, 12_600, 12_600, 18_000];
    let want_credited = [3_000, 7_000, 7_000, 10_000];
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), want_due[i]);
        assert_eq!(inst.needed_by(), DOLLAR * want_needed[i]);
        assert_eq!(inst.credited_by(), DOLLAR * want_credited[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    // September adds nothing of its own, but 70% is needed by it.
    let sep = &res.value()[2];
    assert_eq!(sep.status(), Status::Short);
    assert_eq!(sep.shortfall(), USCents(0));
    assert!(!res.divergences().is_empty());
}

#[test]
fn estimated_threshold_boundary() {
    let p = prior(DOLLAR * 50_000, DOLLAR * 100_000, USCents(0));
    for (status, owed, required) in [
        (FilingStatus::Single, DOLLAR * 500 - USCents(1), false),
        (FilingStatus::Single, DOLLAR * 500, true),
        (
            FilingStatus::MarriedFilingSeparately,
            DOLLAR * 250 - USCents(1),
            false,
        ),
        (FilingStatus::MarriedFilingSeparately, DOLLAR * 250, true),
    ] {
        let f = Household {
            status,
            withheld: DOLLAR * 2_000,
            prior: p,
            ..tax(DOLLAR * 2_000 + owed)
        }
        .facts();
        let res = rules().requirement(&f).unwrap();
        assert_eq!(
            res.value().required(),
            required,
            "{status} owing {owed}: {}",
            res.value().why()
        );
    }
}

/// 110% above $150,000 of 2025 California AGI ($75,000 separately), and no
/// prior-year test at all from $1,000,000 of 2026 California AGI ($500,000
/// separately).
#[test]
fn estimated_high_income_boundaries() {
    let s = FilingStatus::Single;
    let m = FilingStatus::MarriedFilingSeparately;
    let c1 = USCents(1);
    for (status, prior_agi, this_agi, available, pct) in [
        (s, DOLLAR * 150_000, DOLLAR * 200_000, true, percent(100)),
        (
            s,
            DOLLAR * 150_000 + c1,
            DOLLAR * 200_000,
            true,
            percent(110),
        ),
        (m, DOLLAR * 75_000, DOLLAR * 200_000, true, percent(100)),
        (
            m,
            DOLLAR * 75_000 + c1,
            DOLLAR * 200_000,
            true,
            percent(110),
        ),
        (
            s,
            DOLLAR * 900_000,
            DOLLAR * 1_000_000 - c1,
            true,
            percent(110),
        ),
        (s, DOLLAR * 900_000, DOLLAR * 1_000_000, false, Rate(0)),
        (
            m,
            DOLLAR * 400_000,
            DOLLAR * 500_000 - c1,
            true,
            percent(110),
        ),
        (m, DOLLAR * 400_000, DOLLAR * 500_000, false, Rate(0)),
    ] {
        let f = Household {
            status,
            agi: Some(this_agi),
            prior: prior(DOLLAR * 10_000, prior_agi, USCents(0)),
            ..tax(DOLLAR * 90_000)
        }
        .facts();
        let res = rules().requirement(&f).unwrap();
        let opt = &res.value().options()[1];
        assert_eq!(
            opt.available(),
            available,
            "{status}, {prior_agi}, {this_agi}"
        );
        if available {
            assert_eq!(opt.percent(), pct);
        } else {
            assert_eq!(res.value().target(), DOLLAR * 81_000);
        }
    }
    // This year's AGI unknown: the prior-year test is withheld, and the gap
    // reported.
    let f = estimated::new_facts(estimated::CALIFORNIA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 90_000)
                .build()
                .unwrap(),
        )
        .prior_year(prior(DOLLAR * 10_000, DOLLAR * 100_000, USCents(0)).unwrap())
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    assert!(!res.value().options()[1].available());
    assert!(has(&res.required(), PositionKind::AdjustedGrossIncome));
}

/// No penalty when either year's tax less withholding was under $500, or
/// last year had no liability, or the filer is a nonresident in a group
/// return.
#[test]
fn estimated_exceptions() {
    let zero = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(USCents(0)).build().unwrap())
        .build()
        .unwrap();
    let thirty = || tax(DOLLAR * 30_000);
    for (name, h, exempt, label) in [
        (
            "last year under $500",
            Household {
                prior: prior(DOLLAR * 5_000, DOLLAR * 90_000, DOLLAR * 4_500 + USCents(1)),
                ..thirty()
            },
            true,
            "Last year's tax less withholding under $500",
        ),
        (
            "last year exactly $500",
            Household {
                prior: prior(DOLLAR * 5_000, DOLLAR * 90_000, DOLLAR * 4_500),
                ..thirty()
            },
            false,
            "",
        ),
        (
            "no liability last year",
            Household {
                prior: Some(zero),
                ..thirty()
            },
            true,
            "No California tax liability last year",
        ),
        (
            "group return",
            Household {
                group: true,
                ..thirty()
            },
            true,
            "Nonresident in a group return",
        ),
        (
            "this year under $500",
            Household {
                withheld: DOLLAR * 2_000,
                ..tax(DOLLAR * 2_400)
            },
            true,
            "Tax less withholding under $500",
        ),
        (
            "none of them",
            Household {
                prior: prior(DOLLAR * 20_000, DOLLAR * 90_000, USCents(0)),
                ..thirty()
            },
            false,
            "",
        ),
    ] {
        let f = h.facts();
        let res = rules().requirement(&f).unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "{name}: {}",
            res.value().why()
        );
        if exempt {
            let c = res.value().chosen().unwrap();
            assert_eq!(c.basis(), Basis::Exception, "{name}");
            assert_eq!(c.amount(), USCents(0), "{name}");
            assert_eq!(c.label(), label, "{name}");
            let pen = rules().penalty(&f, day(2027, 4, 15)).unwrap();
            assert_eq!(pen.value().total(), USCents(0), "{name}");
            assert_eq!(pen.value().reason(), format!("{label}."), "{name}");
        }
    }
}

/// Each divergence the research found is reported, with both readings and
/// both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 30_000).facts()).unwrap();
    let want = [
        (
            "Nonresidents in a group return",
            "This exception isn't in the 2026 Form 540-ES instructions or the 2025 FTB 5805",
            "§ 19136(j)",
            Reading::Statute,
        ),
        (
            "Underpayments caused by the FTB",
            "An underpayment the FTB caused isn't mentioned",
            "§ 19136.7(a)",
            Reading::Form,
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
                && !d.statute().is_empty()
                && d.uses() == w.3,
            "{} does not pin both readings",
            d.rule()
        );
        assert!(!d.affects_household(), "{}", d.rule());
    }
    let group = Household {
        group: true,
        ..tax(DOLLAR * 30_000)
    };
    let res = rules().requirement(&group.facts()).unwrap();
    let ds = res.divergences();
    let d = ds
        .iter()
        .find(|d| d.rule() == "Nonresidents in a group return")
        .unwrap();
    assert!(d.affects_household());
}

/// FTB 5805's example: $500 short on April 15, then $1,200 paid June 10 for
/// June's $1,200. $500 goes to April and $700 to June.
#[test]
fn payment_application_matches_ftb5805_example() {
    let dues = [day(2025, 4, 15), day(2025, 6, 15)];
    let got = estimated::apply(
        &dues,
        &[USCents(100_000), USCents(120_000)],
        &[
            pay(day(2025, 4, 15), USCents(50_000)),
            pay(day(2025, 6, 10), USCents(120_000)),
        ],
    )
    .unwrap();
    let on = got[0].pieces()[0].paid_on().unwrap();
    assert_eq!(got[0].shortfall(), USCents(50_000));
    assert_eq!(on, day(2025, 6, 10));
    assert_eq!(got[1].shortfall(), USCents(50_000));
}

/// A shortfall across both 2026 rate periods.
#[test]
fn estimated_penalty_across_two_rate_periods() {
    let f = Household {
        prior: prior(DOLLAR * 20_000, DOLLAR * 100_000, USCents(0)),
        payments: vec![
            pay(day(2026, 6, 15), DOLLAR * 8_000),
            pay(day(2026, 8, 14), DOLLAR * 6_000),
            pay(day(2027, 1, 15), DOLLAR * 6_000),
        ],
        ..tax(DOLLAR * 40_000)
    }
    .facts();
    let res = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    let lines = res.value().lines();
    assert_eq!(lines.len(), 2);
    assert_eq!(lines[0].total(), USCents(7_019));
    let june = lines[1].parts();
    assert_eq!(june.len(), 2);
    assert_eq!(june[0].charge(), USCents(1_726));
    assert_eq!(june[1].charge(), USCents(5_178));
    assert_ne!(june[0].source(), june[1].source());
    assert_eq!(res.value().total(), USCents(7_019 + 1_726 + 5_178));
}

/// January 31, 2027 is a Sunday, and the 540-ES does not say it moves.
#[test]
fn estimated_file_and_pay_by_january_31() {
    for (filed, excused) in [(day(2027, 1, 29), true), (day(2027, 2, 1), false)] {
        let f = estimated::new_facts(estimated::CALIFORNIA, 2026)
            .filing_status(FilingStatus::Single)
            .this_year(
                estimated::new_figures()
                    .tax(DOLLAR * 20_000)
                    .state_agi(DOLLAR * 100_000)
                    .build()
                    .unwrap(),
            )
            .return_filed(filed)
            .build()
            .unwrap();
        let res = rules().schedule(&f).unwrap();
        assert_eq!(res.value()[3].status() == Status::Excused, excused);
    }
}

/// The annualised method uses 27%, 63%, 63% and 90%.
#[test]
fn estimated_annualised_percentages() {
    let periods: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 5, 31),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .iter()
    .map(|e| {
        estimated::new_period()
            .end(*e)
            .annualised_tax(DOLLAR * 10_000)
            .build()
            .unwrap()
    })
    .collect();
    let f = estimated::new_facts(estimated::CALIFORNIA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 20_000)
                .state_agi(DOLLAR * 100_000)
                .build()
                .unwrap(),
        )
        .elections(estimated::new_elections().annualised().build().unwrap())
        .periods(&periods)
        .build()
        .unwrap();
    let res = rules().schedule(&f).unwrap();
    for (i, want) in [2_700, 6_300, 6_300, 9_000].iter().enumerate() {
        assert_eq!(res.value()[i].needed_by(), DOLLAR * *want);
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
    let fisher = estimated::new_facts(estimated::CALIFORNIA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(this)
        .fish_share(Rate(7000))
        .build()
        .unwrap();
    assert!(
        rules()
            .requirement(&fisher)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::Unsupported))
    );
    let wrong = estimated::new_facts(estimated::CALIFORNIA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(this)
        .state("not California's")
        .build()
        .unwrap();
    assert_eq!(
        rules().requirement(&wrong).err().unwrap().to_string(),
        "californiataxcalc: the facts carry string, not californiataxcalc.EstimatedFacts"
    );
}

#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    for phrase in [
        "30%, 40%, 0% and 30%",
        "$500",
        "$1,000,000",
        "27%, 63%, 63% and 90%",
    ] {
        assert!(e.versus_federal().contains(phrase), "{phrase}");
    }
    assert!(e.body().len() >= 3);
}

/// A return filed with the whole balance paid stops the penalty that day.
#[test]
fn estimated_penalty_stops_when_the_return_is_paid() {
    let f = estimated::new_facts(estimated::CALIFORNIA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 20_000)
                .state_agi(DOLLAR * 100_000)
                .build()
                .unwrap(),
        )
        .return_filed(day(2027, 2, 26))
        .build()
        .unwrap();
    let res = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    assert_eq!(res.value().as_of(), day(2027, 2, 26));
    assert_ne!(res.value().total(), USCents(0));
    for l in res.value().lines() {
        assert_eq!(l.days(), day(2027, 2, 26).days_since(l.due()));
    }
}

/// Without this year's tax, California's target is last year's tax alone.
#[test]
fn estimated_without_this_years_tax_uses_last_years_alone() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 5_000, DOLLAR * 100_000, USCents(0)),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required());
    assert_eq!(r.target(), DOLLAR * 5_000);
    let o = &r.options()[0];
    assert!(!o.available());
    assert_eq!(o.why_not(), "Needs this year's tax");
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [1_500, 3_500, 3_500, 5_000].iter().enumerate() {
        assert_eq!(sched.value()[i].needed_by(), DOLLAR * *want);
    }
    let bound = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(bound.value().upper_bound() || !bound.value().rated_through().is_zero());
    assert_ne!(bound.value().total(), USCents(0));
    let k = Household {
        no_tax: false,
        tax: DOLLAR * 4_000,
        ..h
    };
    let known = rules().penalty(&k.facts(), day(2027, 4, 15)).unwrap();
    assert!(known.value().total() < bound.value().total());
    assert!(!known.value().upper_bound());
}

/// Without this year's tax or AGI there is no target, and both questions
/// are asked.
#[test]
fn estimated_without_this_years_tax_or_agi_has_no_target() {
    let facts = estimated::new_facts(estimated::CALIFORNIA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(estimated::new_figures().build().unwrap())
        .prior_year(prior(DOLLAR * 5_000, DOLLAR * 100_000, USCents(0)).unwrap())
        .build()
        .unwrap();
    let res = rules().requirement(&facts).unwrap();
    assert!(!res.value().target_known() && !res.value().required());
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    assert!(has(&res.required(), PositionKind::AdjustedGrossIncome));
    assert!(
        rules()
            .schedule(&facts)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NeedsThisYearTax))
    );
    assert!(
        rules()
            .penalty(&facts, day(2027, 4, 15))
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NeedsThisYearTax))
    );
}

/// The waivers are decided from the conditions the FTB 5805 instructions
/// state. The penalty is still figured in full.
#[test]
fn estimated_waiver_is_decided_from_its_conditions() {
    let base = Household {
        prior: prior(DOLLAR * 40_000, DOLLAR * 100_000, USCents(0)),
        ..tax(DOLLAR * 40_000)
    };
    let before = rules().penalty(&base.facts(), day(2027, 4, 15)).unwrap();
    assert_ne!(before.value().total(), USCents(0));
    let pos = |c: WaiverCause, on: Date, born: Option<Date>| {
        let mut b = estimated::new_positions();
        b.waiver_for(c, on);
        if let Some(d) = born {
            b.born(d);
        }
        b.build().unwrap()
    };
    let r = WaiverCause::Retirement;
    for (name, positions, eligible, refused, reason) in [
        (
            "retired in 2026 at 64",
            pos(r, day(2026, 6, 1), Some(day(1962, 3, 1))),
            true,
            false,
            "after reaching 62",
        ),
        (
            "retired in 2025, the year before",
            pos(r, day(2025, 6, 1), Some(day(1960, 3, 1))),
            true,
            false,
            "You retired in 2025",
        ),
        (
            "retired in 2024",
            pos(r, day(2024, 6, 1), Some(day(1955, 3, 1))),
            false,
            true,
            "2025 or 2026",
        ),
        (
            "retired in 2020",
            pos(r, day(2020, 6, 1), Some(day(1955, 3, 1))),
            false,
            true,
            "You retired in 2020",
        ),
        (
            "retired at 61",
            pos(r, day(2026, 6, 1), Some(day(1965, 3, 1))),
            false,
            true,
            "You were 61",
        ),
        (
            "disabled in 2026",
            pos(WaiverCause::Disability, day(2026, 2, 1), None),
            true,
            false,
            "became disabled in 2026",
        ),
        (
            "a disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
            true,
            false,
            "a disaster",
        ),
    ] {
        let h = Household {
            positions: Some(positions),
            ..base.clone()
        };
        let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        let o = p.value().waiver_outcome().unwrap();
        assert_eq!(o.eligible(), eligible, "{name}");
        assert_eq!(o.refused(), refused, "{name}");
        assert!(o.reason().contains(reason), "{name}: {}", o.reason());
        if eligible {
            assert!(
                o.reason()
                    .to_lowercase()
                    .contains("the franchise tax board"),
                "{name}"
            );
        }
        assert_eq!(p.value().total(), before.value().total(), "{name}");
        let want = if eligible {
            USCents(0)
        } else {
            p.value().total()
        };
        assert_eq!(p.value().if_waived(), want, "{name}");
        assert!(!has(&p.required(), PositionKind::PenaltyWaiver), "{name}");
    }
}

/// An exception sets a $0 target, so a question whose answer would only
/// change the target is not asked under one.
#[test]
fn estimated_exception_asks_nothing_about_the_target() {
    for (name, h, exempt) in [
        (
            "little due last year, this year's tax not given",
            Household {
                no_tax: true,
                prior: prior(DOLLAR * 600, DOLLAR * 30_000, DOLLAR * 400),
                ..Household::default()
            },
            true,
        ),
        (
            "much due last year, this year's tax not given",
            Household {
                no_tax: true,
                prior: prior(DOLLAR * 6_000, DOLLAR * 70_000, DOLLAR * 1_000),
                ..Household::default()
            },
            false,
        ),
    ] {
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::ThisYearTax),
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

/// Withholding over last year's tax reads as a refund, never as a minus
/// amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let rules = rules();
    let mut asked = false;
    for a in estimatedtest::ask(&rules, &estimatedtest::households(&estimated::CALIFORNIA)) {
        if a.household().name() != "withholding over last year's tax" {
            continue;
        }
        asked = true;
        let why = a.requirement().unwrap().value().why();
        assert!(
            why.contains("and $2,136 was withheld. That left a $136 refund. California"),
            "why {why}"
        );
        for x in estimatedtest::texts(Some(&rules), &[a]) {
            assert!(
                !x.text().contains("−$") && !x.text().contains("-$"),
                "{}: {}",
                x.where_(),
                x.text()
            );
        }
    }
    assert!(asked, "no household withholding over last year's tax");
}

// Worksheet I, from worksheet_i_test.go.

/// One Worksheet I column with $6,000 of tax on $94,500 of total taxable
/// income, a $5,500 deduction, $153 of exemption credits and $100 of credits
/// to prorate.
fn column(ca_agi: USCents, total_agi: USCents) -> WorksheetIColumn {
    new_worksheet_i_column()
        .tax_on_total(DOLLAR * 6_000)
        .total_taxable(DOLLAR * 94_500)
        .california_agi(ca_agi)
        .annualised_total_agi(total_agi)
        .deduction(DOLLAR * 5_500)
        .exemption_credits(DOLLAR * 153)
        .prorated_credits(DOLLAR * 100)
        .build()
        .unwrap()
}

/// Column (d), worked by hand.
#[test]
fn worksheet_i_column_by_hand() {
    let w = worksheet_i(3, column(DOLLAR * 54_000, DOLLAR * 100_000)).unwrap();
    for (line, got, want) in [
        ("F", w.f(), DOLLAR * 54_000),
        ("J", w.j(), DOLLAR * 2_970),
        ("K", w.k(), DOLLAR * 51_030),
        ("L", w.l(), DOLLAR * 3_240),
        ("O", w.o(), USCents(8_262)),
        ("P", w.p(), USCents(315_738)),
        ("13", w.line13(), DOLLAR * 54),
        ("14c", w.line14c(), USCents(310_338)),
    ] {
        assert_eq!(got, want, "line {line}");
    }
    assert_eq!(w.rate_h(), BigRational::new(54.into(), 100.into()));
    // Column (a) annualises by 4: $25,000 is $100,000, the whole total AGI,
    // so H is 1 and nothing is prorated away.
    let a = worksheet_i(0, column(DOLLAR * 25_000, DOLLAR * 100_000)).unwrap();
    assert_eq!(a.f(), DOLLAR * 100_000);
    assert_eq!(a.j(), DOLLAR * 5_500);
    assert_eq!(a.p(), USCents(584_700));
    // H is capped at 1.0000 when California AGI exceeds total AGI.
    let over = worksheet_i(3, column(DOLLAR * 120_000, DOLLAR * 100_000)).unwrap();
    assert_eq!(over.rate_h(), BigRational::one());
    assert_eq!(over.j(), DOLLAR * 5_500);
    assert!(worksheet_i(4, column(USCents(1), USCents(1))).is_err());
    assert!(
        new_worksheet_i_column()
            .tax_on_total(USCents(1))
            .build()
            .is_err()
    );
}

fn residence(state: &str, from: Date, to: Date) -> ResidencePeriod {
    estimated::new_residence_period(state)
        .from(from)
        .to(to)
        .build()
        .unwrap()
}

fn annual_periods() -> Vec<estimated::Period> {
    [
        day(2026, 3, 31),
        day(2026, 5, 31),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .iter()
    .map(|e| {
        estimated::new_period()
            .end(*e)
            .annualised_tax(DOLLAR * 6_000)
            .build()
            .unwrap()
    })
    .collect()
}

fn moved(annualised: bool, columns: &[WorksheetIColumn]) -> Facts {
    let mut b = estimated::new_facts(estimated::CALIFORNIA, 2026);
    b.filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 3_200)
                .state_agi(DOLLAR * 54_000)
                .build()
                .unwrap(),
        )
        .prior_year(prior(DOLLAR * 5_000, DOLLAR * 90_000, USCents(0)).unwrap())
        .residence(&[
            residence("CA", day(2026, 1, 1), day(2026, 7, 14)),
            residence("OR", day(2026, 7, 15), day(2026, 12, 31)),
        ]);
    if annualised {
        b.periods(&annual_periods());
        b.elections(estimated::new_elections().annualised().build().unwrap());
    }
    if !columns.is_empty() {
        b.state(new_estimated_facts().worksheet_i(columns).build().unwrap());
    }
    b.build().unwrap()
}

/// The regular method takes the Form 540NR tax as it is.
#[test]
fn estimated_part_year_resident_regular_method() {
    let sched = rules().schedule(&moved(false, &[])).unwrap().value();
    for (i, w) in [86_400, 201_600, 201_600, 288_000].iter().enumerate() {
        assert_eq!(sched[i].needed_by(), USCents(*w), "installment {}", i + 1);
    }
}

/// With the annualised method, Worksheet I prorates each period's tax.
#[test]
fn estimated_part_year_resident_worksheet_i() {
    let cols = [
        column(DOLLAR * 25_000, DOLLAR * 100_000),
        column(DOLLAR * 41_667, DOLLAR * 100_000),
        column(DOLLAR * 54_000, DOLLAR * 112_500),
        column(DOLLAR * 54_000, DOLLAR * 100_000),
    ];
    let res = rules().schedule(&moved(true, &cols)).unwrap();
    for (i, w) in [86_400, 201_600, 201_600, 279_304].iter().enumerate() {
        assert_eq!(
            res.value()[i].needed_by(),
            USCents(*w),
            "installment {}",
            i + 1
        );
    }
    assert!(has(&res.required(), PositionKind::UnsettledRule));

    // Without the columns, the period figures stand and they are asked for.
    let without = rules().schedule(&moved(true, &[])).unwrap();
    assert!(has(&without.required(), PositionKind::JurisdictionFact));

    // Worksheet I is for a Form 540NR filer, not a full-year resident.
    let f = estimated::new_facts(estimated::CALIFORNIA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 3_200)
                .state_agi(DOLLAR * 54_000)
                .build()
                .unwrap(),
        )
        .residence(&[residence("CA", day(2026, 1, 1), day(2026, 12, 31))])
        .state(new_estimated_facts().worksheet_i(&cols).build().unwrap())
        .build()
        .unwrap();
    assert!(rules().requirement(&f).is_err());
    assert!(
        new_estimated_facts()
            .worksheet_i(&cols[..2])
            .build()
            .is_err()
    );
}

/// A part-year resident on the regular method is told plainly what FTB 5805
/// does with a move.
#[test]
fn estimated_part_year_resident_is_told_what_a_move_changes() {
    let facts = |residence: &[ResidencePeriod], annualised: bool| {
        let mut b = estimated::new_facts(estimated::CALIFORNIA, 2026);
        b.filing_status(FilingStatus::Single)
            .this_year(
                estimated::new_figures()
                    .state_agi(DOLLAR * 54_000)
                    .build()
                    .unwrap(),
            )
            .prior_year(prior(DOLLAR * 8_412, DOLLAR * 90_000, USCents(0)).unwrap())
            .withheld(&[estimated::new_withheld()
                .on(day(2026, 7, 14))
                .amount(DOLLAR * 5_129)
                .build()
                .unwrap()])
            .residence(residence);
        if annualised {
            b.periods(&annual_periods());
            b.elections(estimated::new_elections().annualised().build().unwrap());
        }
        b.build().unwrap()
    };
    let moved_out = [
        residence("CA", day(2026, 1, 1), day(2026, 7, 14)),
        residence("OR", day(2026, 7, 15), day(2026, 12, 31)),
    ];
    let all_year = [residence("CA", day(2026, 1, 1), day(2026, 12, 31))];
    let target = "Your target is 100% of your 2025 California tax, $8,412.";
    let reduced = "California doesn't reduce last year's tax for the months you lived elsewhere.";
    let january = " The January payment still counts after a move.";
    let question = "Did your income come unevenly through the year?";
    let effect = "If so, the annualised method with FTB 5805 Worksheet I can lower your payments.";
    let uneven = |rs: &[Required]| {
        rs.iter().any(|r| {
            r.position() == PositionKind::JurisdictionFact
                && r.question() == question
                && r.effect() == effect
        })
    };
    let f = facts(&moved_out, false);
    let req = rules().requirement(&f).unwrap();
    let why = req.value().why();
    assert!(why.contains(target) && why.ends_with(reduced), "{why}");
    assert!(uneven(&req.required()));
    let pen = rules().penalty(&f, day(2027, 12, 31)).unwrap();
    assert_eq!(
        pen.value().reason(),
        format!("Payments fall short on four due dates.{january}")
    );
    assert!(uneven(&pen.required()));

    for (name, f) in [
        ("full year", facts(&all_year, false)),
        ("annualised", facts(&moved_out, true)),
    ] {
        let req = rules().requirement(&f).unwrap();
        assert!(!req.value().why().contains(reduced), "{name}");
        assert!(!uneven(&req.required()), "{name}");
        let reason = rules()
            .penalty(&f, day(2027, 12, 31))
            .unwrap()
            .value()
            .reason();
        assert!(!reason.contains("after a move"), "{name}: {reason}");
    }
}
