//! Alabama's estimated tax rules for 2026, checked against the Department of
//! Revenue's own documents. Ported from estimated_test.go and
//! estimated_refund_test.go.
//!
//! Go's TestEstimatedFiguresAreInTheirSources and the extract checks in the
//! due-date, divergence and waiver tests read testdata/ and are not ported.
//! TestEstimatedRatesMatchTheADORPage reads the 2026 rates from the rates
//! page's extract; here they are written out, 6%, 7% and 7%.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, Payment, Positions, PriorReturn, Status,
    WaiverCause, WaiverKind, Weekday,
};
use filing::Status as FilingStatus;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_alabamataxcalc::{EstimatedFacts, estimated_tax, new_estimated_facts};
use working::{PositionKind, Reading, Required, Rule};

const AL: Jurisdiction = Jurisdiction::from_static("AL");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

/// Whether a rule's working contains `s`.
fn applied(rs: &[Rule], s: &str) -> bool {
    rs.iter().any(|r| r.applied().contains(s))
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
    status: FilingStatus,
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    as_withheld: bool,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    waiver: Option<WaiverKind>,
    /// A waiver asked for, with its cause and dates.
    positions: Option<Positions>,
    /// Schedule AI line 16 for each period.
    annualised: Option<Vec<USCents>>,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

/// $40,000 of tax and the usual prior return.
fn usual() -> Household {
    Household {
        prior: last_year(),
        ..tax(DOLLAR * 40_000)
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
        let mut b = estimated::new_facts(AL, 2026);
        b.filing_status(status)
            .this_year(fb.build().unwrap())
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
        if self.as_withheld {
            b.elections(
                estimated::new_elections()
                    .withholding_as_withheld()
                    .build()
                    .unwrap(),
            );
        }
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        match self.extras {
            Some(e) => b.state(e),
            None => b.state(
                new_estimated_facts()
                    .prior_year_farmer_or_fisher(false)
                    .build()
                    .unwrap(),
            ),
        };
        if let Some(k) = self.waiver {
            b.positions(estimated::new_positions().waiver(k).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
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
        b.build().unwrap()
    }
}

/// A filed twelve-month 2025 return with its tax and its Alabama and
/// federal AGI.
fn prior(tax: USCents, alabama_agi: USCents, federal_agi: USCents) -> Option<PriorReturn> {
    let f = estimated::new_figures()
        .tax(tax)
        .state_agi(alabama_agi)
        .federal_agi(federal_agi)
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

/// The usual prior return: $20,000 of tax on $100,000 of AGI.
fn last_year() -> Option<PriorReturn> {
    prior(DOLLAR * 20_000, DOLLAR * 100_000, DOLLAR * 100_000)
}

#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let nth = |y: i64, m: i64, n: i64| {
        let mut d = day(y, m, 1);
        while d.weekday() != Weekday::MONDAY {
            d = d.add_days(1);
        }
        d.add_days(7 * (n - 1))
    };
    let holidays = [
        nth(2026, 4, 4),
        nth(2026, 6, 1),
        day(2026, 6, 19),
        nth(2026, 9, 1),
        day(2027, 1, 1),
        nth(2027, 1, 3),
    ];
    assert_eq!(holidays[0], day(2026, 4, 27));
    assert_eq!(holidays[5], day(2027, 1, 18));
    let res = rules().schedule(&usual().facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
        assert!(!holidays.contains(&inst.due()));
    }
}

/// The threshold, as the 40ES words it: at least $500 after withholding
/// means payments are needed, and under $500 there is no penalty.
#[test]
fn estimated_threshold_boundary() {
    for (withheld, exempt) in [(USCents(950_001), true), (USCents(950_000), false)] {
        let f = Household {
            withheld,
            ..tax(DOLLAR * 10_000)
        }
        .facts();
        let res = rules().requirement(&f).unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {withheld}");
        let c = res.value().chosen().unwrap_or_default();
        assert_eq!(c.basis() == Basis::Exception, exempt);
        if exempt {
            assert_eq!(c.amount(), USCents(0));
            assert_eq!(c.label(), "Tax less withholding under $500");
            let p = rules().penalty(&f, day(2027, 4, 15)).unwrap().value();
            assert_eq!(p.total(), USCents(0));
            assert_eq!(p.reason(), "Tax less withholding under $500.");
        }
    }
}

/// The prior-year test is 100% up to $150,000 of last year's Alabama AGI and
/// 110% a cent above, $75,000 on a separate return. It reads last year's
/// AGI, so this year's AGI does not move it.
#[test]
fn estimated_prior_year_step_boundaries() {
    for (status, agi, pct) in [
        (FilingStatus::Single, 15_000_000, percent(100)),
        (FilingStatus::Single, 15_000_001, percent(110)),
        (FilingStatus::MarriedFilingJointly, 15_000_001, percent(110)),
        (
            FilingStatus::MarriedFilingSeparately,
            7_500_000,
            percent(100),
        ),
        (
            FilingStatus::MarriedFilingSeparately,
            7_500_001,
            percent(110),
        ),
    ] {
        let h = Household {
            status,
            prior: prior(DOLLAR * 10_000, USCents(agi), USCents(agi)),
            ..tax(DOLLAR * 100_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let o = &res.value().options()[1];
        assert_eq!(o.percent(), pct, "{status:?}, AGI {agi}");
        assert_eq!(o.amount(), pct.of(DOLLAR * 10_000));
        assert_eq!(res.value().chosen().unwrap().basis(), Basis::PriorYearTax);
    }
    // This year's AGI is not the test: $1,000,000 this year, $100,000 last.
    let f = estimated::new_facts(AL, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 100_000)
                .state_agi(DOLLAR * 1_000_000)
                .build()
                .unwrap(),
        )
        .prior_year(last_year().unwrap())
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    assert_eq!(res.value().options()[1].percent(), percent(100));
}

/// The target is the smaller option, and the prior-year option needs a
/// filed twelve-month return. Without last year's AGI, 110% is assumed and
/// asked.
#[test]
fn estimated_target_options() {
    let res = rules().requirement(&usual().facts()).unwrap();
    assert_eq!(res.value().chosen().unwrap().basis(), Basis::PriorYearTax);
    assert_eq!(res.value().target(), DOLLAR * 20_000);
    let res = rules()
        .requirement(
            &Household {
                prior: last_year(),
                ..tax(DOLLAR * 20_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().chosen().unwrap().basis(), Basis::ThisYearTax);
    assert_eq!(res.value().target(), DOLLAR * 18_000);

    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(9)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 1_000)
                .state_agi(DOLLAR * 50_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    for (name, p, ask) in [
        ("no return on record", None, PositionKind::PriorYearReturn),
        ("not filed", Some(not_filed), PositionKind::Unspecified),
        (
            "not required",
            Some(not_required),
            PositionKind::Unspecified,
        ),
        ("nine months", Some(short), PositionKind::Unspecified),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: p,
                    ..tax(DOLLAR * 40_000)
                }
                .facts(),
            )
            .unwrap();
        let o = &res.value().options()[1];
        assert!(!o.available() && !o.why_not().is_empty(), "{name}");
        assert_eq!(res.value().chosen().unwrap().basis(), Basis::ThisYearTax);
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}");
        }
    }

    // No Alabama AGI for last year: 110% of $20,000, $22,000, and the AGI is
    // asked for.
    let no_agi = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 20_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                prior: Some(no_agi),
                ..tax(DOLLAR * 40_000)
            }
            .facts(),
        )
        .unwrap();
    let o = &res.value().options()[1];
    assert_eq!(o.percent(), percent(110));
    assert_eq!(o.amount(), DOLLAR * 22_000);
    assert!(has(&res.required(), PositionKind::AdjustedGrossIncome));
}

/// § 6654(d)(1)(C) does not say whether it tests federal or Alabama AGI.
/// The question is asked whenever the answer could change the percentage.
#[test]
fn estimated_federal_or_alabama_agi_is_an_open_question() {
    let no_federal = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 20_000)
                .state_agi(DOLLAR * 100_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let p = |al: i64, fed: i64| prior(DOLLAR * 20_000, DOLLAR * al, DOLLAR * fed);
    for (name, pr, asked, pct) in [
        (
            "both under",
            p(140_000, 145_000),
            PositionKind::Unspecified,
            percent(100),
        ),
        (
            "both over",
            p(160_000, 170_000),
            PositionKind::Unspecified,
            percent(110),
        ),
        (
            "federal over",
            p(140_000, 160_000),
            PositionKind::UnsettledRule,
            percent(100),
        ),
        (
            "federal under",
            p(160_000, 140_000),
            PositionKind::UnsettledRule,
            percent(110),
        ),
        (
            "federal not given",
            Some(no_federal),
            PositionKind::AdjustedGrossIncome,
            percent(100),
        ),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: pr,
                    ..tax(DOLLAR * 40_000)
                }
                .facts(),
            )
            .unwrap();
        for k in [
            PositionKind::AdjustedGrossIncome,
            PositionKind::UnsettledRule,
        ] {
            assert_eq!(has(&res.required(), k), k == asked, "{name}: {k}");
        }
        assert_eq!(res.value().options()[1].percent(), pct, "{name}");
    }
}

/// The 2026 rates the penalty charges are the ones on the department's
/// page: an April shortfall left unpaid to December 31 is charged the
/// second, third and fourth quarters' rates in turn.
#[test]
fn estimated_rates_match_the_ador_page() {
    let want = [percent(6), percent(7), percent(7)];
    let parts = rules()
        .penalty(&usual().facts(), day(2026, 12, 31))
        .unwrap()
        .value()
        .lines()[0]
        .parts();
    let want_days = [76, 92, 92];
    assert_eq!(parts.len(), want.len());
    for (i, p) in parts.iter().enumerate() {
        assert_eq!(p.rate(), want[i]);
        assert_eq!(p.days(), want_days[i]);
    }
}

/// A shortfall that spans the change from 6% to 7% on July 1, 2026: $105.62.
#[test]
fn estimated_penalty_across_two_rate_periods() {
    let f = Household {
        payments: vec![
            pay(day(2026, 6, 15), DOLLAR * 5_000),
            pay(day(2026, 8, 14), DOLLAR * 5_000),
            pay(day(2026, 9, 15), DOLLAR * 5_000),
            pay(day(2027, 1, 15), DOLLAR * 5_000),
        ],
        ..usual()
    }
    .facts();
    let res = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    let lines = res.value().lines();
    assert_eq!(lines.len(), 2);
    let p = lines[0].parts();
    assert!(p.len() == 1 && p[0].days() == 61 && p[0].charge() == USCents(5_014));
    let june = lines[1].parts();
    assert_eq!(june.len(), 2);
    assert!(june[0].days() == 15 && june[0].charge() == USCents(1_233));
    assert!(june[1].days() == 45 && june[1].charge() == USCents(4_315));
    assert_eq!(june[0].rate(), percent(6));
    assert_eq!(june[1].rate(), percent(7));
    assert_eq!(res.value().total(), USCents(10_562));
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
}

/// Nothing paid at all. The penalty runs into 2027, whose rate the
/// department has not published, so the days from January 1 are not charged
/// and the gap is reported.
#[test]
fn estimated_penalty_into_an_unpublished_rate() {
    let f = usual().facts();
    let year = rules().penalty(&f, day(2026, 12, 31)).unwrap();
    let full = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    assert!(!has(&year.required(), PositionKind::UnpublishedRate));
    assert!(has(&full.required(), PositionKind::UnpublishedRate));
    assert_eq!(full.value().total(), year.value().total());
    assert_ne!(full.value().total(), USCents(0));
    let lines = full.value().lines();
    assert_eq!(lines.len(), 4);
    assert_eq!(lines[2].total(), USCents(10_260));
    assert_eq!(lines[3].total(), USCents(0));
}

/// The penalty stops on April 15, 2027, or on the day the return is filed
/// and paid if that is earlier.
#[test]
fn estimated_penalty_stops() {
    let mut h = usual();
    let p = rules().penalty(&h.facts(), day(2027, 6, 30)).unwrap();
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
    h.return_filed = day(2027, 2, 26);
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().as_of(), day(2027, 2, 26));
    for l in p.value().lines() {
        assert_eq!(l.days(), day(2027, 2, 26).days_since(l.due()));
    }
    assert!(rules().penalty(&h.facts(), Date::default()).is_err());
}

/// Withholding counts in four equal parts on the due dates, whenever it was
/// withheld, unless the filer elects the dates it was actually withheld.
#[test]
fn estimated_withholding_is_credited_evenly_unless_elected() {
    let mut h = Household {
        withheld: DOLLAR * 20_000,
        ..usual()
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, inst) in sched.value().iter().enumerate() {
        assert_eq!(inst.status(), Status::Met);
        assert_eq!(inst.credited_by(), DOLLAR * 5_000 * (i as i64 + 1));
    }
    h.as_withheld = true;
    let sched = rules().schedule(&h.facts()).unwrap();
    for inst in &sched.value()[..3] {
        assert_eq!(inst.status(), Status::Short);
    }
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_ne!(p.value().total(), USCents(0));
}

/// Schedule AI uses 22.5%, 45%, 67.5% and 90% of the annualised tax. On a
/// flat $10,000 each installment is $2,250, against a regular $5,000.
#[test]
fn estimated_annualised_percentages() {
    let res = rules()
        .schedule(
            &Household {
                annualised: Some(vec![DOLLAR * 10_000; 4]),
                ..usual()
            }
            .facts(),
        )
        .unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.needed_by(), DOLLAR * 2_250 * (i as i64 + 1));
    }
    // A period that ends on another day is refused.
    let period = |d: Date| estimated::new_period().end(d).build().unwrap();
    let f = estimated::new_facts(AL, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 40_000)
                .build()
                .unwrap(),
        )
        .elections(estimated::new_elections().annualised().build().unwrap())
        .periods(&[
            period(day(2026, 3, 31)),
            period(day(2026, 6, 30)),
            period(day(2026, 8, 31)),
            period(day(2026, 12, 31)),
        ])
        .build()
        .unwrap();
    assert!(rules().schedule(&f).is_err());
}

/// The waivers are the filer's to ask for and the department's to grant, so
/// the penalty is shown before any waiver.
#[test]
fn estimated_waiver_is_asked_not_applied() {
    let mut h = usual();
    let before = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
    assert!(has(&before.required(), PositionKind::PenaltyWaiver));
    h.waiver = Some(WaiverKind::Casualty);
    let after = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
    assert!(!has(&after.required(), PositionKind::PenaltyWaiver));
    assert_eq!(after.value().waiver(), WaiverKind::Casualty);
    assert_eq!(after.value().total(), before.value().total());
}

/// § 6654(h) and § 6654(e)(2) are in the statute and not on the forms. The
/// figure follows the forms: neither is applied, and when the facts meet one
/// the working says so.
#[test]
fn estimated_statutory_rules_the_forms_omit() {
    for (filed, noted) in [
        (day(2027, 1, 29), true),
        (day(2027, 1, 31), true),
        (day(2027, 2, 1), false),
    ] {
        let h = Household {
            return_filed: filed,
            ..usual()
        };
        let res = rules().schedule(&h.facts()).unwrap();
        assert_eq!(res.value()[3].status(), Status::Short, "filed {filed}");
        assert_eq!(
            applied(
                &res.applied(),
                "which the statute says excuses the January installment"
            ),
            noted,
            "filed {filed}"
        );
    }

    let none = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let h = Household {
        prior: Some(none),
        ..tax(DOLLAR * 40_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().exempt() && res.value().required());
    assert!(applied(&res.applied(), "Form 2210AL has no such exception"));
    let p = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
    assert_ne!(p.value().total(), USCents(0));

    // A filed full-year return showing no tax needs nothing on the form's
    // own arithmetic, so no penalty arises either way.
    let zero = prior(USCents(0), DOLLAR * 30_000, DOLLAR * 30_000);
    let p = rules()
        .penalty(
            &Household {
                prior: zero,
                ..tax(DOLLAR * 40_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert_eq!(p.value().total(), USCents(0));
}

/// The 40ES gives a late starter fewer, larger payments; Form 2210AL charges
/// 25% by every date. The figure follows Form 2210AL and the working names
/// the 40ES schedule, whose groups start April 2, June 2 and September 2.
#[test]
fn estimated_late_start_follows_form_2210al() {
    for (met, note) in [
        (day(2026, 4, 1), ""),
        (day(2026, 4, 2), "pay 1/3 of the estimate by June 15, 2026"),
        (day(2026, 6, 1), "pay 1/3 of the estimate by June 15, 2026"),
        (
            day(2026, 6, 2),
            "pay 1/2 of the estimate by September 15, 2026",
        ),
        (
            day(2026, 9, 1),
            "pay 1/2 of the estimate by September 15, 2026",
        ),
        (
            day(2026, 9, 2),
            "pay all of the estimate by January 15, 2027",
        ),
    ] {
        let res = rules()
            .schedule(
                &Household {
                    first_met: met,
                    ..usual()
                }
                .facts(),
            )
            .unwrap();
        if note.is_empty() {
            assert!(!applied(&res.applied(), "the 40ES would have you pay"));
        } else {
            assert!(applied(&res.applied(), note), "first met {met}: {note}");
        }
        let s = &res.value()[0];
        assert_eq!(s.needed_by(), DOLLAR * 5_000);
        assert_eq!(s.status(), Status::Short);
    }
}

/// Each divergence is reported with both readings, both sources, the
/// reading the figures follow, and whether it changes these facts.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&usual().facts()).unwrap();
    let want = [
        (
            "The interest rate",
            "prints one rate, 7%, for every day",
            "§ 6654(a)",
            Reading::Statute,
            true,
        ),
        (
            "Which year's AGI sets the 110% test",
            "they ask for \"AGI for 2025\"",
            "§ 6654(d)(1)(C)",
            Reading::Statute,
            true,
        ),
        (
            "A return filed and paid by January 31",
            "Neither form excuses the January payment",
            "§ 6654(h)",
            Reading::Form,
            false,
        ),
        (
            "No tax liability last year",
            "Form 2210AL has no exception",
            "§ 6654(e)(2)",
            Reading::Form,
            false,
        ),
        (
            "A requirement first met after April 1",
            "Form 2210AL still requires 25% by every date",
            "§ 6654(c), (d)(1)(A)",
            Reading::Statute,
            false,
        ),
        (
            "Nonresident aliens",
            "Neither form has separate rules",
            "§ 6654(j)",
            Reading::Form,
            false,
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
        assert_eq!(d.uses(), w.3, "{}", d.rule());
        assert_eq!(d.affects_household(), w.4, "{}", d.rule());
    }
}

/// A divergence affects only the facts it turns on.
#[test]
fn estimated_divergences_affect_only_their_facts() {
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
    assert!(!affects(
        Household {
            withheld: DOLLAR * 20_000,
            ..usual()
        },
        "The interest rate"
    ));
    assert!(affects(
        Household {
            return_filed: day(2027, 1, 29),
            ..usual()
        },
        "A return filed and paid by January 31"
    ));
    assert!(affects(
        Household {
            first_met: day(2026, 6, 2),
            ..usual()
        },
        "A requirement first met after April 1"
    ));
    assert!(!affects(
        Household {
            prior: last_year(),
            ..tax(DOLLAR * 10_000)
        },
        "Which year's AGI sets the 110% test"
    ));
}

#[test]
fn estimated_refuses_what_is_not_built() {
    assert!(
        estimated_tax(2025)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NoRules))
    );
    let this = estimated::new_figures()
        .tax(DOLLAR * 10_000)
        .build()
        .unwrap();
    let base = || {
        let mut b = estimated::new_facts(AL, 2026);
        b.filing_status(FilingStatus::Single).this_year(this);
        b
    };
    for (name, f) in [
        ("a farmer", base().farm_share(Rate(7000)).build().unwrap()),
        ("a fisher", base().fish_share(Rate(7000)).build().unwrap()),
        (
            "a farmer last year",
            base()
                .state(
                    new_estimated_facts()
                        .prior_year_farmer_or_fisher(true)
                        .build()
                        .unwrap(),
                )
                .build()
                .unwrap(),
        ),
    ] {
        assert!(
            rules()
                .requirement(&f)
                .err()
                .is_some_and(|e| e.is(&estimated::Error::Unsupported)),
            "{name}"
        );
    }
    let other = base().state("not Alabama's").build().unwrap();
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
            estimated::new_facts(AL, 2027)
                .filing_status(FilingStatus::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        ("other extras", other.clone()),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}");
        assert!(rules().penalty(&f, day(2027, 4, 15)).is_err(), "{name}");
    }
    assert_eq!(
        rules().requirement(&other).err().unwrap().to_string(),
        "alabamataxcalc: the facts carry string, not alabamataxcalc.EstimatedFacts"
    );
    // Last year's farming share, not given, is asked for.
    let res = rules().requirement(&base().build().unwrap()).unwrap();
    assert!(has(&res.required(), PositionKind::JurisdictionFact));
}

#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    for s in [
        "$500 rather than $1,000",
        "last year's Alabama AGI",
        "State holidays",
        "January 31",
        "no tax last year",
    ] {
        assert!(e.versus_federal().contains(s), "{s}");
    }
    let body = e.body().join(" ");
    for s in [
        "90%",
        "110%",
        "$150,000",
        "6% from April to June",
        "Schedule AI",
        "a third by June 15",
    ] {
        assert!(body.contains(s), "{s}");
    }
}

/// Without this year's tax the target is last year's tax alone, the
/// installments are a quarter of it, and the penalty is the most it can be.
#[test]
fn estimated_without_this_years_tax_uses_last_years_alone() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 5_000, DOLLAR * 100_000, DOLLAR * 100_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required());
    assert_eq!(r.target(), DOLLAR * 5_000);
    let o = &r.options()[0];
    assert!(!o.available());
    assert_eq!(o.why_not(), estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, inst) in sched.value().iter().enumerate() {
        assert_eq!(inst.needed_by(), DOLLAR * 1_250 * (i as i64 + 1));
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

/// Without this year's tax or a usable 2025 return there is no target, and
/// Schedule and Penalty say so.
#[test]
fn estimated_without_this_years_tax_or_last_years_has_no_target() {
    let f = Household {
        no_tax: true,
        ..Household::default()
    }
    .facts();
    let res = rules().requirement(&f).unwrap();
    assert!(!res.value().target_known() && !res.value().required());
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    assert!(has(&res.required(), PositionKind::PriorYearReturn));
    assert!(
        rules()
            .schedule(&f)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NeedsThisYearTax))
    );
    assert!(
        rules()
            .penalty(&f, day(2027, 4, 15))
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NeedsThisYearTax))
    );
}

/// The waivers are decided from the conditions the Form 2210AL instructions
/// state. The penalty is still figured in full.
#[test]
fn estimated_waiver_is_decided_from_its_conditions() {
    let base = usual();
    let before = rules().penalty(&base.facts(), day(2026, 12, 31)).unwrap();
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
        let p = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
        let o = p.value().waiver_outcome().unwrap();
        assert_eq!(o.eligible(), eligible, "{name}");
        assert_eq!(o.refused(), refused, "{name}");
        assert!(o.reason().contains(reason), "{name}: {}", o.reason());
        if eligible {
            assert!(
                o.reason()
                    .to_lowercase()
                    .contains("the alabama department of revenue"),
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
        ("tax under $500, no 2025 return", tax(DOLLAR * 400), true),
        ("tax over $500, no 2025 return", tax(DOLLAR * 10_000), false),
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

/// Withholding over the tax reads as a refund, never as a minus amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let rules = rules();
    let mut asked = false;
    for a in estimatedtest::ask(&rules, &estimatedtest::households(&estimated::ALABAMA)) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked = true;
        let why = a.requirement().unwrap().value().why();
        assert!(
            why.contains("leaves a $600 refund. No estimated payments are needed"),
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
    assert!(asked, "no household withholding over the tax");
}
