//! The federal estimated tax rules for 2026, ported from estimated_test.go
//! and estimated_refund_test.go.
//!
//! Not ported: the tests that read the Go package's testdata extracts
//! (TestEstimatedFiguresAreInTheirSources,
//! TestEstimatedRatesMatchTheRevenueRulings, the Form 2210 example and
//! table tests, and the Publication 509 holiday parse in
//! TestEstimatedDueDatesForTwentyTwentySix, whose due-date and § 6654(h)
//! checks are ported).

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, PriorReturn, Status as InstallmentStatus,
    WaiverCause, WaiverKind, Weekday,
};
use filing::Status;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_ustaxcalc::estimated_tax;
use working::{PositionKind, Required};

const D: USCents = DOLLAR;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

#[derive(Clone, Default)]
struct Household {
    status: Option<Status>,
    tax: USCents,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    return_filed: Date,
    waiver: Option<WaiverKind>,
    positions: Option<estimated::Positions>,
    tax_unknown: bool,
}

impl Household {
    fn facts(&self) -> Facts {
        let mut fb = estimated::new_figures();
        if !self.tax_unknown {
            fb.tax(self.tax);
        }
        let mut b = estimated::new_facts(estimated::FEDERAL, 2026);
        b.filing_status(self.status.unwrap_or(Status::Single))
            .this_year(fb.build().unwrap())
            .paid(&self.payments)
            .return_filed(self.return_filed);
        if self.withheld > USCents(0) {
            let w = estimated::new_withheld()
                .on(day(2026, 12, 15))
                .amount(self.withheld)
                .build()
                .unwrap();
            b.withheld(&[w]);
        }
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        if let Some(p) = &self.positions {
            b.positions(*p);
        }
        b.build().unwrap()
    }
}

fn prior_return(tax: USCents, agi: USCents) -> Option<PriorReturn> {
    let f = estimated::new_figures()
        .tax(tax)
        .federal_agi(agi)
        .build()
        .unwrap();
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(f)
            .full_year_resident(true)
            .build()
            .unwrap(),
    )
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

#[test]
fn due_dates_for_twenty_twenty_six() {
    let h = Household {
        tax: D * 20_000,
        ..Household::default()
    };
    let res = rules().schedule(&h.facts()).unwrap();
    let want = [
        day(2026, 4, 15),
        day(2026, 6, 15),
        day(2026, 9, 15),
        day(2027, 1, 15),
    ];
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), want[i]);
        assert!(![Weekday::SATURDAY, Weekday::SUNDAY].contains(&inst.due().weekday()));
    }
    for (filed, excused) in [
        (day(2027, 1, 29), true),
        (day(2027, 2, 1), true),
        (day(2027, 2, 2), false),
    ] {
        let h = Household {
            tax: D * 20_000,
            return_filed: filed,
            ..Household::default()
        };
        let res = rules().schedule(&h.facts()).unwrap();
        assert_eq!(
            res.value()[3].status() == InstallmentStatus::Excused,
            excused,
            "filed {filed}"
        );
    }
}

#[test]
fn requirement_matches_publication_505() {
    let h = Household {
        tax: D * 71_253,
        prior: prior_return(D * 42_581, D * 180_000),
        ..Household::default()
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    let chosen = r.chosen().unwrap();
    assert_eq!(chosen.basis(), Basis::PriorYearTax);
    assert_eq!(chosen.percent(), percent(110));
    let diff = r.target() - D * 46_839;
    assert!(diff.0.abs() <= 100, "target {}", r.target());
    assert!(r.required());
}

#[test]
fn threshold_boundary() {
    let prior = prior_return(D * 50_000, D * 100_000);
    for (owed, required) in [(D * 1_000 - USCents(1), false), (D * 1_000, true)] {
        let f = Household {
            tax: D * 5_000 + owed,
            withheld: D * 5_000,
            prior,
            ..Household::default()
        }
        .facts();
        let res = rules().requirement(&f).unwrap();
        let r = res.value();
        assert_eq!(r.required(), required, "owing {owed}");
        let is_exception = r.chosen().is_some_and(|c| {
            c.basis() == Basis::Exception
                && c.amount() == USCents(0)
                && c.label() == "Tax less withholding under $1,000"
        });
        assert!(
            is_exception != required && r.exempt() != required,
            "owing {owed}"
        );
        let pen = rules().penalty(&f, day(2026, 12, 31)).unwrap();
        if !required {
            assert_eq!(pen.value().total(), USCents(0));
        }
    }
}

#[test]
fn prior_year_step_boundary() {
    for (status, agi, want) in [
        (Status::Single, D * 150_000, percent(100)),
        (Status::Single, D * 150_000 + USCents(1), percent(110)),
        (
            Status::MarriedFilingJointly,
            D * 150_000 + USCents(1),
            percent(110),
        ),
        (Status::MarriedFilingSeparately, D * 75_000, percent(100)),
        (
            Status::MarriedFilingSeparately,
            D * 75_000 + USCents(1),
            percent(110),
        ),
    ] {
        let f = Household {
            status: Some(status),
            tax: D * 90_000,
            prior: prior_return(D * 10_000, agi),
            ..Household::default()
        }
        .facts();
        let opt = rules().requirement(&f).unwrap().value().options()[1].clone();
        assert_eq!(opt.percent(), want, "{status} {agi}");
        assert_eq!(opt.amount(), want.of(D * 10_000));
    }
}

#[test]
fn prior_year_option_availability() {
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .build()
        .unwrap();
    let not_filed = estimated::new_prior_return().build().unwrap();
    let no_agi = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(D * 10_000).build().unwrap())
        .build()
        .unwrap();
    for (name, prior, available, position) in [
        (
            "no return on record",
            None,
            false,
            PositionKind::PriorYearReturn,
        ),
        (
            "eleven months",
            Some(short),
            false,
            PositionKind::Unspecified,
        ),
        (
            "not filed",
            Some(not_filed),
            false,
            PositionKind::Unspecified,
        ),
        (
            "AGI not given",
            Some(no_agi),
            true,
            PositionKind::AdjustedGrossIncome,
        ),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    tax: D * 30_000,
                    prior,
                    ..Household::default()
                }
                .facts(),
            )
            .unwrap();
        let opt = res.value().options()[1].clone();
        assert_eq!(opt.available(), available, "{name}");
        if !available {
            assert!(!opt.why_not().is_empty(), "{name}");
            assert_eq!(res.value().target(), D * 27_000, "{name}");
        }
        if position != PositionKind::Unspecified {
            assert!(has(&res.required(), position), "{name}");
        }
        if name == "AGI not given" {
            assert_eq!(opt.percent(), percent(110));
        }
    }
}

#[test]
fn no_prior_liability_exception() {
    let zero = estimated::new_figures()
        .tax(USCents(0))
        .federal_agi(D * 20_000)
        .build()
        .unwrap();
    let resident = estimated::new_prior_return()
        .filed(true)
        .figures(zero)
        .full_year_resident(true)
        .build()
        .unwrap();
    let unanswered = estimated::new_prior_return()
        .filed(true)
        .figures(zero)
        .build()
        .unwrap();
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let part_year = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(zero)
        .full_year_resident(true)
        .build()
        .unwrap();
    for (name, prior, exempt) in [
        ("zero tax, resident", resident, true),
        ("not required to file", not_required, true),
        ("residence not answered", unanswered, false),
        ("six-month prior return", part_year, false),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    tax: D * 30_000,
                    prior: Some(prior),
                    ..Household::default()
                }
                .facts(),
            )
            .unwrap();
        let r = res.value();
        assert_eq!(r.exempt() && r.target() == USCents(0), exempt, "{name}");
        if exempt {
            let c = r.chosen().unwrap();
            assert_eq!(c.basis(), Basis::Exception);
            assert_eq!(c.label(), "No federal tax liability last year");
        }
        if name == "residence not answered" {
            assert!(has(&res.required(), PositionKind::Residency));
        }
    }
}

#[test]
fn withholding_is_spread_evenly() {
    let f = Household {
        tax: D * 8_000,
        withheld: D * 4_000,
        prior: prior_return(D * 8_000, D * 90_000),
        ..Household::default()
    }
    .facts();
    let res = rules().schedule(&f).unwrap();
    let short = [800, 1_600, 1_800, 1_800];
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.credited_by(), D * 1_000 * (i as i64 + 1));
        assert_eq!(inst.shortfall(), D * short[i]);
        assert_eq!(inst.status(), InstallmentStatus::Short);
    }
}

#[test]
fn penalty_across_two_rate_periods() {
    let f = Household {
        tax: D * 80_000,
        prior: prior_return(D * 40_000, D * 100_000),
        payments: vec![
            pay(day(2026, 8, 14), D * 10_000),
            pay(day(2026, 8, 14), D * 10_000),
            pay(day(2026, 9, 15), D * 10_000),
            pay(day(2027, 1, 15), D * 10_000),
        ],
        ..Household::default()
    }
    .facts();
    let res = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    let lines = res.value().lines();
    assert_eq!(lines.len(), 2);
    for (l, want) in lines.iter().zip([[12_493, 8_630], [2_466, 8_630]]) {
        let charges: Vec<USCents> = l.parts().iter().map(|p| p.charge()).collect();
        assert_eq!(charges, want.map(USCents));
    }
    assert_eq!(res.value().total(), USCents(12_493 + 8_630 + 2_466 + 8_630));
    assert_eq!(lines[0].days(), 121);
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));

    let answered = Household {
        tax: D * 80_000,
        prior: prior_return(D * 40_000, D * 100_000),
        waiver: Some(WaiverKind::None),
        ..Household::default()
    }
    .facts();
    let res = rules().penalty(&answered, day(2026, 12, 31)).unwrap();
    assert!(!has(&res.required(), PositionKind::PenaltyWaiver));
}

#[test]
fn refuses_other_years_and_jurisdictions() {
    for year in [2025, 2027] {
        let err = estimated_tax(year).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{err}");
        assert_eq!(
            err.to_string(),
            format!("estimated: no estimated tax rules for this jurisdiction and year: US {year}")
        );
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let facts = |j: estimated::Jurisdiction, farm: i32| {
        estimated::new_facts(j, 2026)
            .filing_status(Status::Single)
            .this_year(this)
            .farm_share(Rate(farm))
            .build()
            .unwrap()
    };
    assert!(
        rules()
            .requirement(&facts(estimated::CALIFORNIA, 0))
            .is_err()
    );
    let err = rules()
        .requirement(&facts(estimated::FEDERAL, 6667))
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::Unsupported), "{err}");
    assert!(
        rules()
            .requirement(&facts(estimated::FEDERAL, 6666))
            .is_ok()
    );
}

#[test]
fn explanation_reads_as_prose() {
    let e = rules().explanation();
    assert!(e.body().len() >= 3 && !e.summary().is_empty() && !e.sources().is_empty());
    assert_eq!(e.versus_federal(), "");
}

#[test]
fn penalty_stops_when_the_return_is_paid() {
    let mut h = Household {
        tax: D * 80_000,
        prior: prior_return(D * 40_000, D * 100_000),
        payments: vec![
            pay(day(2026, 9, 15), D * 20_000),
            pay(day(2027, 1, 15), D * 10_000),
        ],
        ..Household::default()
    };
    let unfiled = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
    h.return_filed = day(2027, 1, 4);
    let filed = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(unfiled.value().total(), filed.value().total());

    let h = Household {
        tax: D * 80_000,
        prior: prior_return(D * 40_000, D * 100_000),
        return_filed: day(2027, 3, 1),
        ..Household::default()
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(res.value().as_of(), day(2027, 3, 1));
    for l in res.value().lines() {
        for p in l.parts() {
            assert!(!p.to().after(day(2027, 3, 1)));
        }
    }
}

#[test]
fn withholding_as_withheld_by_election() {
    let this = estimated::new_figures().tax(D * 8_000).build().unwrap();
    let w = estimated::new_withheld()
        .on(day(2026, 12, 15))
        .amount(D * 4_000)
        .build()
        .unwrap();
    let elected = estimated::new_elections()
        .withholding_as_withheld()
        .build()
        .unwrap();
    let f = estimated::new_facts(estimated::FEDERAL, 2026)
        .filing_status(Status::Single)
        .this_year(this)
        .prior_year(prior_return(D * 8_000, D * 90_000).unwrap())
        .withheld(&[w])
        .elections(elected)
        .build()
        .unwrap();
    let res = rules().schedule(&f).unwrap();
    let credited: Vec<USCents> = res.value().iter().map(|i| i.credited_by()).collect();
    assert_eq!(credited, [USCents(0), USCents(0), USCents(0), D * 4_000]);
}

#[test]
fn annualised_percentages() {
    let ends = [
        day(2026, 3, 31),
        day(2026, 5, 31),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ];
    let taxes = [0, 4_000, 12_000, 12_000];
    let periods: Vec<estimated::Period> = ends
        .iter()
        .zip(taxes)
        .map(|(&e, t)| {
            estimated::new_period()
                .end(e)
                .annualised_tax(D * t)
                .build()
                .unwrap()
        })
        .collect();
    let this = estimated::new_figures().tax(D * 12_000).build().unwrap();
    let ann = estimated::new_elections().annualised().build().unwrap();
    let f = estimated::new_facts(estimated::FEDERAL, 2026)
        .filing_status(Status::Single)
        .this_year(this)
        .prior_year(prior_return(D * 20_000, D * 90_000).unwrap())
        .elections(ann)
        .periods(&periods)
        .build()
        .unwrap();
    let res = rules().schedule(&f).unwrap();
    let needed: Vec<USCents> = res.value().iter().map(|i| i.needed_by()).collect();
    assert_eq!(needed, [0, 1_800, 8_100, 10_800].map(|n| D * n));

    let mut wrong = periods[..3].to_vec();
    wrong.push(
        estimated::new_period()
            .end(day(2026, 11, 30))
            .build()
            .unwrap(),
    );
    let f = estimated::new_facts(estimated::FEDERAL, 2026)
        .filing_status(Status::Single)
        .this_year(this)
        .elections(ann)
        .periods(&wrong)
        .build()
        .unwrap();
    assert!(rules().schedule(&f).is_err());
}

#[test]
fn without_this_years_tax_uses_last_years_alone() {
    let h = Household {
        tax_unknown: true,
        withheld: D * 2_000,
        prior: prior_return(D * 10_000, D * 100_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == D * 10_000 && r.target_known() && !r.exempt());
    let opts = r.options();
    assert_eq!(opts[0].basis(), Basis::ThisYearTax);
    assert!(!opts[0].available());
    assert_eq!(opts[0].why_not(), "Needs this year's tax");
    assert_eq!(opts[0].amount(), USCents(0));
    let c = r.chosen().unwrap();
    assert_eq!((c.basis(), c.amount()), (Basis::PriorYearTax, D * 10_000));
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    for a in res.applied() {
        assert!(
            !a.applied().contains("90% of this year's"),
            "{}",
            a.applied()
        );
    }

    let sched = rules().schedule(&h.facts()).unwrap();
    let needed: Vec<USCents> = sched.value().iter().map(|i| i.needed_by()).collect();
    assert_eq!(needed, [2_500, 5_000, 7_500, 10_000].map(|n| D * n));

    let bound = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!(
        (bound.upper_bound() || !bound.rated_through().is_zero()) && bound.total() > USCents(0)
    );
    for (tax, same) in [(D * 1_000_000, true), (D * 9_000, false)] {
        let k = Household {
            tax_unknown: false,
            tax,
            ..h.clone()
        };
        let known = rules()
            .penalty(&k.facts(), day(2027, 4, 15))
            .unwrap()
            .value();
        assert!(!known.upper_bound());
        assert_eq!(known.total() == bound.total(), same, "{tax}");
        assert!(known.total() <= bound.total());
    }
}

#[test]
fn with_no_option_has_no_target() {
    let f = Household {
        tax_unknown: true,
        withheld: D * 2_000,
        ..Household::default()
    }
    .facts();
    let res = rules().requirement(&f).unwrap();
    let r = res.value();
    assert!(!r.target_known() && !r.required() && r.target() == USCents(0) && !r.exempt());
    assert!(r.chosen().is_none());
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    assert!(has(&res.required(), PositionKind::PriorYearReturn));
    let err = rules().schedule(&f).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
    let err = rules().penalty(&f, day(2027, 4, 15)).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
}

#[test]
fn without_this_years_tax_still_applies_no_liability() {
    let f = Household {
        tax_unknown: true,
        prior: prior_return(USCents(0), D * 40_000),
        ..Household::default()
    }
    .facts();
    let r = rules().requirement(&f).unwrap().value();
    assert!(r.exempt() && !r.required(), "{}", r.why());
    let pen = rules().penalty(&f, day(2027, 4, 15)).unwrap().value();
    assert!(pen.total() == USCents(0) && !pen.upper_bound());
}

#[test]
fn waiver_is_decided_from_its_conditions() {
    let base = Household {
        tax: D * 80_000,
        prior: prior_return(D * 40_000, D * 100_000),
        ..Household::default()
    };
    let before = rules().penalty(&base.facts(), day(2026, 12, 31)).unwrap();
    assert!(before.value().total() > USCents(0));
    let positions = |cause, on: Date, born: Option<Date>| {
        let mut b = estimated::new_positions();
        b.waiver_for(cause, on);
        if let Some(born) = born {
            b.born(born);
        }
        b.build().unwrap()
    };
    for (name, p, eligible, refused, reason) in [
        (
            "retired in 2026 at 64",
            positions(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1962, 3, 1)),
            ),
            true,
            false,
            "after reaching 62",
        ),
        (
            "retired in 2025, the year before",
            positions(
                WaiverCause::Retirement,
                day(2025, 6, 1),
                Some(day(1960, 3, 1)),
            ),
            true,
            false,
            "You retired in 2025",
        ),
        (
            "retired in 2024",
            positions(
                WaiverCause::Retirement,
                day(2024, 6, 1),
                Some(day(1955, 3, 1)),
            ),
            false,
            true,
            "2025 or 2026",
        ),
        (
            "retired in 2020",
            positions(
                WaiverCause::Retirement,
                day(2020, 6, 1),
                Some(day(1955, 3, 1)),
            ),
            false,
            true,
            "You retired in 2020",
        ),
        (
            "retired at 61",
            positions(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1965, 3, 1)),
            ),
            false,
            true,
            "You were 61",
        ),
        (
            "disabled in 2026",
            positions(WaiverCause::Disability, day(2026, 2, 1), None),
            true,
            false,
            "became disabled in 2026",
        ),
        (
            "a disaster",
            positions(WaiverCause::Disaster, day(2026, 8, 1), None),
            true,
            false,
            "a disaster",
        ),
    ] {
        let h = Household {
            positions: Some(p),
            ..base.clone()
        };
        let p = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
        let o = p.value().waiver_outcome().unwrap();
        assert_eq!((o.eligible(), o.refused()), (eligible, refused), "{name}");
        assert!(o.reason().contains(reason), "{name}: {}", o.reason());
        if eligible {
            assert!(o.reason().to_lowercase().contains("the irs"), "{name}");
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

#[test]
fn exception_asks_nothing_about_the_target() {
    for (name, prior, exempt) in [
        (
            "no liability last year, this year's tax not given",
            prior_return(USCents(0), D * 50_000),
            true,
        ),
        (
            "tax last year, this year's tax not given",
            prior_return(D * 4_000, D * 50_000),
            false,
        ),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    tax_unknown: true,
                    prior,
                    ..Household::default()
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(res.value().exempt(), exempt, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::ThisYearTax),
            !exempt,
            "{name}"
        );
        for q in res.required() {
            assert!(
                !(exempt && q.effect().to_lowercase().contains("target")),
                "{name}"
            );
        }
    }
}

/// estimated_refund_test.go: withholding over the tax says it covers the
/// tax, never a minus amount.
#[test]
fn refund_reads_as_refund() {
    let rules = rules();
    let as_ = estimatedtest::ask(&*rules, &estimatedtest::households(&estimated::FEDERAL));
    let a = as_
        .iter()
        .find(|a| a.household().name() == "withholding over the tax")
        .unwrap();
    let why = a.requirement().as_ref().unwrap().value().why();
    assert!(
        why.contains("Your $3,600 withheld covers your $3,000 tax"),
        "{why}"
    );
    for x in estimatedtest::texts(Some(&*rules), std::slice::from_ref(a)) {
        assert!(
            !x.text().contains("−$") && !x.text().contains("-$"),
            "{}: {}",
            x.where_(),
            x.text()
        );
    }
}
