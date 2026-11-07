//! Arkansas's estimated tax rules for 2026, checked against the Department
//! of Finance and Administration's own documents.
//!
//! Arkansas publishes no exhaustive oracle for estimated tax. The strongest
//! check is the worked penalty example in the AR2210 instructions: inputs
//! and answer, $67, both from the department, with one shortfall paid in two
//! pieces across the year end. The exceptions, the annualised method and the
//! boundaries are hand-written cases, worked by hand in their comments. (The
//! Go test comparing each figure against its extract in testdata/ checks the
//! Go package's documents, not code, and is not ported.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, Payment, Penalty, Positions,
    PriorReturn, Status, WaiverCause, WaiverKind, Weekday,
};
use money::{DOLLAR, USCents, percent};
use taxcalcs_arkansastaxcalc as arkansastaxcalc;
use working::{Divergence, PositionKind, Reading, Required};

const AR: Jurisdiction = Jurisdiction::from_static("AR");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    arkansastaxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

/// Whether a position of kind k mentions `about`.
fn asked(rs: &[Required], k: PositionKind, about: &str) -> bool {
    rs.iter()
        .any(|r| r.position() == k && format!("{} {}", r.question(), r.effect()).contains(about))
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
    /// Arkansas net tax, AR1000F line 38.
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    return_filed: Date,
    first_met: Date,
    /// AR2210A line 19, each period.
    annualised: Option<Vec<USCents>>,
    waiver: Option<WaiverKind>,
}

fn taxed(tax: USCents) -> Household {
    Household {
        tax,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        let mut b = estimated::new_facts(AR, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(fb.build().unwrap())
            .paid(&self.payments)
            .return_filed(self.return_filed)
            .requirement_first_met(self.first_met);
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
                .map(|(i, tax)| {
                    estimated::new_period()
                        .end(ends[i])
                        .annualised_tax(*tax)
                        .build()
                        .unwrap()
                })
                .collect();
            b.periods(&periods);
            b.elections(estimated::new_elections().annualised().build().unwrap());
        }
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        b.build().unwrap()
    }
}

/// A filed twelve-month return by a full-year resident.
fn prior(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .full_year_resident(true)
            .build()
            .unwrap(),
    )
}

/// Withholding over the tax reads as a refund, never as a minus amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let rules = rules();
    let mut asked_any = false;
    for a in estimatedtest::ask(&rules, &estimatedtest::households(&estimated::ARKANSAS)) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked_any = true;
        let res = a.requirement().unwrap();
        let why = res.value().why();
        assert!(
            why.contains("leaves a $600 refund. No estimated payments are needed"),
            "why {why:?}"
        );
        for x in estimatedtest::texts(Some(&rules), std::slice::from_ref(&a)) {
            assert!(
                !x.text().contains("−$") && !x.text().contains("-$"),
                "{}: {:?} has a minus amount",
                x.where_(),
                x.text()
            );
        }
    }
    assert!(asked_any, "no household withholding over the tax");
}

/// The 2026 dates are the ones the AR1000ES vouchers print. None falls on a
/// weekend, so no 2026 date moves. The file-and-pay date, January 31, 2027,
/// is a Sunday, and is not moved.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&taxed(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    assert_eq!(day(2027, 1, 31).weekday(), Weekday::SUNDAY);
}

/// The AR2210 instructions' example, moved a year on: $5,000 of tax, $2,000
/// paid, last year's $3,000. $67 in all. September's second piece runs from
/// 2026 into 2027 as one charge. Arkansas's 10% is fixed, so no unpublished
/// rate is reported.
#[test]
fn estimated_penalty_matches_the_ar2210_example() {
    let h = Household {
        withheld: DOLLAR * 2_000,
        prior: prior(DOLLAR * 3_000),
        ..taxed(DOLLAR * 5_000)
    };
    assert_eq!(
        rules().requirement(&h.facts()).unwrap().value().target(),
        DOLLAR * 3_000
    );
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let want: [&[(i64, i64, i64)]; 4] = [
        &[(250, 61, 4)],
        &[(500, 92, 13)],
        &[(500, 122, 17), (250, 212, 15)],
        &[(750, 90, 18)],
    ];
    let lines = res.value().lines();
    assert_eq!(lines.len(), want.len());
    for (i, l) in lines.iter().enumerate() {
        let parts = l.parts();
        assert_eq!(parts.len(), want[i].len(), "line {}", i + 1);
        for (j, p) in parts.iter().enumerate() {
            let w = want[i][j];
            assert!(
                p.principal() == DOLLAR * w.0
                    && p.days() == w.1
                    && p.charge() == DOLLAR * w.2
                    && p.rate() == percent(10),
                "line {} part {}: {} for {} days at {} is {}",
                i + 1,
                j + 1,
                p.principal(),
                p.days(),
                p.rate(),
                p.charge()
            );
        }
    }
    assert_eq!(res.value().total(), DOLLAR * 67);
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
    // Past the stop, the penalty is figured to April 15, 2027.
    let late = rules().penalty(&h.facts(), day(2027, 6, 30)).unwrap();
    assert_eq!(late.value().as_of(), day(2027, 4, 15));
    assert_eq!(late.value().total(), DOLLAR * 67);
}

/// The form treats estimated payments made by January 15 as paid in four
/// equal parts, whenever they were made. A payment after January 15 counts
/// on its day: $3,000 paid March 1, 2027 gives $66, $53, $34 and $9, $162.
#[test]
fn estimated_payments_count_in_equal_parts() {
    let mut h = Household {
        prior: prior(DOLLAR * 3_000),
        payments: vec![pay(day(2027, 1, 15), DOLLAR * 2_000)],
        ..taxed(DOLLAR * 5_000)
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(res.value().total(), DOLLAR * 67);
    assert!(asked(
        &res.required(),
        PositionKind::UnsettledRule,
        "line 8"
    ));
    let sched = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(sched[0].credited_by(), DOLLAR * 500);

    h.payments = vec![pay(day(2027, 3, 1), DOLLAR * 3_000)];
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(res.value().total(), DOLLAR * 162);
    for l in res.value().lines() {
        for p in l.parts() {
            assert_eq!(p.to(), day(2027, 3, 1), "line {}", l.installment());
        }
    }
}

/// AR2210 line 4: no penalty when tax less withholding is $1,000 or less. At
/// exactly $1,000 the documents disagree, and the question is reported.
#[test]
fn estimated_threshold_boundaries() {
    for (tax, withheld, exempt, ask) in [
        (99_999, 0, true, false),
        (200_000, 100_000, true, true),
        (200_000, 99_999, false, false),
        (100_001, 0, false, false),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    withheld: USCents(withheld),
                    ..taxed(USCents(tax))
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "tax {tax}, withheld {withheld}"
        );
        if exempt {
            let c = res.value().chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && c.label() == "Tax less withholding of $1,000 or less"
            );
        }
        assert_eq!(
            asked(&res.required(), PositionKind::UnsettledRule, "exactly"),
            ask,
            "tax {tax}, withheld {withheld}"
        );
    }
}

/// The target is the smaller of 90% of this year's tax and 100% of last
/// year's, with no 110% tier at any income.
#[test]
fn estimated_target_options() {
    let big = estimated::new_prior_return()
        .filed(true)
        .full_year_resident(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 8_000)
                .federal_agi(DOLLAR * 2_000_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    for (name, p, target, basis) in [
        (
            "last year is smaller",
            prior(DOLLAR * 8_000),
            DOLLAR * 8_000,
            Basis::PriorYearTax,
        ),
        (
            "this year is smaller",
            prior(DOLLAR * 9_500),
            DOLLAR * 9_000,
            Basis::ThisYearTax,
        ),
        (
            "no 110% at $2,000,000 of AGI",
            Some(big),
            DOLLAR * 8_000,
            Basis::PriorYearTax,
        ),
        (
            "a cent under 90% of this year",
            prior(USCents(899_999)),
            USCents(899_999),
            Basis::PriorYearTax,
        ),
    ] {
        let r = rules()
            .requirement(
                &Household {
                    prior: p,
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        let c = r.chosen().unwrap();
        assert!(r.target() == target && c.basis() == basis, "{name}");
        assert_eq!(r.options()[1].percent(), percent(100), "{name}");
        let o = &r.options()[0];
        assert!(
            o.percent() == percent(90) && o.amount() == DOLLAR * 9_000,
            "{name}"
        );
    }
}

/// Last year's option needs a filed return. The form takes any filed
/// return's net tax, including a short year's or a zero, and the question is
/// reported.
#[test]
fn estimated_prior_year_option_availability() {
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .full_year_resident(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 5_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let zero_part_year = estimated::new_prior_return()
        .filed(true)
        .full_year_resident(false)
        .figures(estimated::new_figures().tax(USCents(0)).build().unwrap())
        .build()
        .unwrap();
    let req = |p: Option<PriorReturn>| {
        rules()
            .requirement(
                &Household {
                    prior: p,
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap()
    };

    let res = req(None);
    assert!(!res.value().options()[1].available());
    assert!(has(&res.required(), PositionKind::PriorYearReturn));
    let res = req(Some(not_filed));
    assert!(!res.value().options()[1].available() && res.value().target() == DOLLAR * 9_000);
    let res = req(Some(short));
    assert_eq!(res.value().target(), DOLLAR * 5_000);
    assert!(asked(
        &res.required(),
        PositionKind::UnsettledRule,
        "6 months"
    ));
    let res = req(Some(zero_part_year));
    let r = res.value();
    assert!(
        !r.exempt()
            && !r.required()
            && r.target().0 == 0
            && asked(
                &res.required(),
                PositionKind::UnsettledRule,
                "showed no tax"
            ),
        "no tax last year as a part-year resident: exempt {}, required {}, target {}",
        r.exempt(),
        r.required(),
        r.target()
    );
}

/// Exception 3: no liability for a twelve-month year as a resident
/// throughout.
#[test]
fn estimated_no_liability_last_year() {
    let unanswered = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(USCents(0)).build().unwrap())
        .build()
        .unwrap();
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let not_required_away = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(false)
        .build()
        .unwrap();
    let req = |p: Option<PriorReturn>| {
        rules()
            .requirement(
                &Household {
                    prior: p,
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap()
    };

    let res = req(prior(USCents(0)));
    assert!(
        res.value().exempt(),
        "no liability as a resident: {}",
        res.value().why()
    );
    let res = req(Some(unanswered));
    assert!(!res.value().exempt() && has(&res.required(), PositionKind::Residency));
    let res = req(Some(not_required));
    assert!(res.value().exempt());
    assert!(asked(
        &res.required(),
        PositionKind::JurisdictionFact,
        "weren't required to file"
    ));
    let res = req(Some(not_required_away));
    assert!(!res.value().exempt() && res.value().target() == DOLLAR * 9_000);
}

/// Exception 2, as the form applies it: a return filed and paid by January
/// 31 takes the whole year out of the penalty. January 31, 2027 is a Sunday
/// and is not moved, so a return on Monday, February 1 is charged, up to
/// that day, and the question is reported.
#[test]
fn estimated_return_filed_by_january_31() {
    for filed in [day(2027, 1, 29), day(2027, 1, 31)] {
        let h = Household {
            return_filed: filed,
            ..taxed(DOLLAR * 10_000)
        };
        let r = rules().requirement(&h.facts()).unwrap().value();
        assert!(r.exempt(), "filed {filed}: not exempt");
        // The exception is the chosen option, so nothing is needed by any
        // date.
        let c = r.chosen().unwrap();
        assert!(
            c.basis() == Basis::Exception
                && c.amount().0 == 0
                && c.label() == "Return filed and paid by the end of January"
        );
        for inst in rules().schedule(&h.facts()).unwrap().value() {
            assert_eq!(inst.status(), Status::NothingDue, "filed {filed}");
        }
        let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        assert!(
            p.value().total().0 == 0
                && p.value().reason() == "Return filed and paid by the end of January."
        );
    }
    let h = Household {
        return_filed: day(2027, 2, 1),
        ..taxed(DOLLAR * 10_000)
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(res.value().total().0 != 0 && res.value().as_of() == day(2027, 2, 1));
    assert!(asked(
        &res.required(),
        PositionKind::UnsettledRule,
        "Sunday"
    ));
}

/// Exceptions 4 and 5 are the Commissioner's to grant: the penalty is shown
/// before the waiver, which is carried with it.
#[test]
fn estimated_waivers_are_the_commissioners() {
    let mut h = taxed(DOLLAR * 10_000);
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));
    let total = res.value().total();
    for k in [WaiverKind::Casualty, WaiverKind::RetiredOrDisabled] {
        h.waiver = Some(k);
        let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        assert!(
            res.value().waiver() == k
                && res.value().total() == total
                && !has(&res.required(), PositionKind::PenaltyWaiver),
            "{k}"
        );
    }
}

/// AR2210A, worked by hand. $10,000 of tax, so line 26 is a quarter of
/// $9,000, $2,250. Annualised tax of $2,000, $4,000, $8,000 and $10,000
/// gives installments of $450, $1,350, $3,600 and $3,600. $6,000 withheld is
/// $1,500 a quarter. September is $900 short to January 15, $30; January is
/// $3,000 short to April 15, $74. $104 in all.
#[test]
fn estimated_annualised_method() {
    let mut h = Household {
        withheld: DOLLAR * 6_000,
        annualised: Some(vec![
            DOLLAR * 2_000,
            DOLLAR * 4_000,
            DOLLAR * 8_000,
            DOLLAR * 10_000,
        ]),
        ..taxed(DOLLAR * 10_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    let want_needed = [DOLLAR * 450, DOLLAR * 1_800, DOLLAR * 5_400, DOLLAR * 9_000];
    let want_short = [USCents(0), USCents(0), DOLLAR * 900, DOLLAR * 3_000];
    for (i, inst) in sched.iter().enumerate() {
        assert!(
            inst.needed_by() == want_needed[i] && inst.shortfall() == want_short[i],
            "installment {}: needs {} and is {} short",
            i + 1,
            inst.needed_by(),
            inst.shortfall()
        );
    }
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let lines = res.value().lines();
    assert!(
        lines.len() == 2
            && lines[0].parts()[0].charge() == DOLLAR * 30
            && lines[1].parts()[0].charge() == DOLLAR * 74
    );
    assert_eq!(res.value().total(), DOLLAR * 104);
    for l in &lines {
        assert_eq!(l.parts()[0].rate(), percent(10), "line {}", l.installment());
    }

    // All the income in the last period, whose annualised tax is $8,000:
    // nothing is due until January and then $7,200.
    h.annualised = Some(vec![USCents(0), USCents(0), USCents(0), DOLLAR * 8_000]);
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [USCents(0), USCents(0), USCents(0), DOLLAR * 7_200]
        .into_iter()
        .enumerate()
    {
        assert_eq!(
            sched[i].needed_by(),
            want,
            "all income late: installment {}",
            i + 1
        );
    }

    // A period that does not end where AR2210A's does is refused.
    let periods: Vec<_> = [
        day(2026, 3, 30),
        day(2026, 5, 31),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .into_iter()
    .map(|e| estimated::new_period().end(e).build().unwrap())
    .collect();
    let f = estimated::new_facts(AR, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .periods(&periods)
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    assert!(rules().requirement(&f).is_err());
}

/// A requirement first met in July still needs a quarter on every date
/// under line 7; the divergence reports the law's reading.
#[test]
fn estimated_late_starter_still_owes_a_quarter_a_date() {
    let sched = rules()
        .schedule(
            &Household {
                first_met: day(2026, 7, 1),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    for (i, inst) in sched.iter().enumerate() {
        assert_eq!(inst.needed_by(), DOLLAR * 2_250 * (i as i64 + 1));
    }
}

/// Each divergence is reported with both readings and both sources, follows
/// the form, and says whether it changes these facts.
#[test]
fn estimated_divergences_pin_both_readings() {
    let want = [
        (
            "When a penalty can arise",
            "$1,000 or less",
            "paragraph 1(a)",
        ),
        (
            "When estimated payments count as paid",
            "four equal parts",
            "paragraph 2(b)",
        ),
        (
            "The prior-year figure",
            "with no condition",
            "paragraph 1(a)",
        ),
        (
            "A return filed and paid by January 31",
            "whole year",
            "Exceptions, paragraph 2",
        ),
        (
            "A requirement first met after April 15",
            "a quarter of the required annual payment in every column",
            "paragraph 1(d)",
        ),
        (
            "The daily penalty factor on AR2210A",
            ".00027397",
            "paragraph 2(b)",
        ),
    ];
    let check = |name: &str, ds: Vec<Divergence>, n: usize, affects: &[&str]| {
        assert_eq!(ds.len(), n, "{name}");
        for d in ds {
            let Some(w) = want.iter().find(|w| w.0 == d.rule()) else {
                panic!("{name}: unexpected divergence {:?}", d.rule());
            };
            assert!(
                d.form().contains(w.1)
                    && d.statute_source().contains(w.2)
                    && d.statute_source().contains("not retrieved")
                    && !d.form_source().is_empty()
                    && !d.statute().is_empty(),
                "{name}: {:?} does not pin both readings",
                d.rule()
            );
            assert!(
                d.uses() == Reading::Form
                    && d.affects_household() == affects.contains(&d.rule().as_str()),
                "{name}: {:?} affects {}",
                d.rule(),
                d.affects_household()
            );
        }
    };
    check(
        "regular",
        rules()
            .requirement(&taxed(DOLLAR * 10_000).facts())
            .unwrap()
            .divergences(),
        5,
        &[],
    );
    let h = Household {
        withheld: DOLLAR * 8_000,
        annualised: Some(vec![USCents(0); 4]),
        ..taxed(DOLLAR * 10_000)
    };
    check(
        "annualised, met",
        rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .divergences(),
        6,
        &[],
    );
    let h = Household {
        withheld: DOLLAR * 6_000,
        annualised: Some(vec![
            DOLLAR * 2_000,
            DOLLAR * 4_000,
            DOLLAR * 8_000,
            DOLLAR * 10_000,
        ]),
        ..taxed(DOLLAR * 10_000)
    };
    check(
        "annualised, short",
        rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .divergences(),
        6,
        &["The daily penalty factor on AR2210A"],
    );
    let paid = Household {
        payments: vec![pay(day(2026, 6, 1), DOLLAR * 2_000)],
        first_met: day(2026, 7, 1),
        ..taxed(DOLLAR * 10_000)
    };
    check(
        "paid, late start",
        rules().requirement(&paid.facts()).unwrap().divergences(),
        5,
        &[
            "When estimated payments count as paid",
            "A requirement first met after April 15",
        ],
    );
    let early = Household {
        return_filed: day(2027, 1, 29),
        ..taxed(DOLLAR * 10_000)
    };
    check(
        "filed early",
        rules().requirement(&early.facts()).unwrap().divergences(),
        5,
        &["A return filed and paid by January 31"],
    );
    let edge = Household {
        withheld: USCents(100_000),
        ..taxed(USCents(200_000))
    };
    check(
        "exactly $1,000",
        rules().requirement(&edge.facts()).unwrap().divergences(),
        5,
        &["When a penalty can arise"],
    );
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for y in [2025, 2027] {
        let Err(err) = arkansastaxcalc::estimated_tax(y) else {
            panic!("{y}: rules returned");
        };
        assert!(err.is(&estimated::Error::NoRules), "{y}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let farm = |share: i32, fish: bool| {
        let mut b = estimated::new_facts(AR, 2026);
        b.filing_status(filing::Status::Single).this_year(this);
        if fish {
            b.fish_share(money::Rate(share));
        } else {
            b.farm_share(money::Rate(share));
        }
        b.build().unwrap()
    };
    let Err(err) = rules().requirement(&farm(6_667, false)) else {
        panic!("a farmer: accepted");
    };
    assert!(err.is(&estimated::Error::Unsupported), "a farmer: {err}");
    // Just under two-thirds is not a farmer.
    rules().requirement(&farm(6_666, false)).unwrap();
    // The AR1000ES's exception is for farming alone.
    rules().requirement(&farm(7_000, true)).unwrap();
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
            estimated::new_facts(AR, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "extras",
            estimated::new_facts(AR, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("not Arkansas's")
                .build()
                .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&taxed(USCents(100)).facts(), Date::default())
            .is_err()
    );
}

#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    let text = format!(
        "{} {} {}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$1,000 or less",
        "fixed 10% a year",
        "no 110% tier",
        "January 31",
        "evenly",
        "Arkansas residence",
        "AR2210A",
        "Commissioner",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.versus_federal().is_empty() && !e.sources().is_empty());
}

/// Without this year's tax the target is last year's, the installments are
/// a quarter of it, and the penalty is an upper bound. $4,000 last year,
/// nothing paid: $100, $83, $58 and $25, $266.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 4_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 4_000 && r.target_known());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    for (i, inst) in rules()
        .schedule(&h.facts())
        .unwrap()
        .value()
        .iter()
        .enumerate()
    {
        assert_eq!(inst.needed_by(), DOLLAR * 1_000 * (i as i64 + 1));
    }
    let pen = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(pen.value().upper_bound() && pen.value().total() == DOLLAR * 266);

    // With no return either, there is no target.
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let r = rules().requirement(&none.facts()).unwrap().value();
    assert!(!r.target_known() && !r.required());
    let Err(err) = rules().schedule(&none.facts()) else {
        panic!("schedule accepted");
    };
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
    let Err(err) = rules().penalty(&none.facts(), day(2027, 4, 15)) else {
        panic!("penalty accepted");
    };
    assert!(err.is(&estimated::Error::NeedsThisYearTax));

    // Exception 3 needs nothing about this year, so its zero is exact.
    let exempt = Household {
        no_tax: true,
        prior: prior(USCents(0)),
        ..Household::default()
    };
    let pen = rules().penalty(&exempt.facts(), day(2027, 4, 15)).unwrap();
    assert!(pen.value().total().0 == 0 && !pen.value().upper_bound());
}

/// An exception sets the target, so no question may say its answer changes
/// the target. A return filed and paid by Jan 31, 2027, with this year's tax
/// and last year's return not given.
#[test]
fn estimated_exception_asks_nothing_of_the_target() {
    let f = estimated::new_facts(AR, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().build().unwrap())
        .return_filed(day(2027, 1, 20))
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    assert!(res.value().exempt(), "no exception applies");
    for r in res.required() {
        assert!(
            !r.effect().to_lowercase().contains("target"),
            "{}: {:?}",
            r.position(),
            r.effect()
        );
    }
}

/// The penalty on $10,000 of tax with nothing paid or withheld, figured to
/// Apr 15, 2027, with the waiver asked for as `pos`.
fn waiver_penalty(pos: Positions) -> Penalty {
    let f = estimated::new_facts(AR, 2026)
        .filing_status(filing::Status::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .positions(pos)
        .build()
        .unwrap();
    rules().penalty(&f, day(2027, 4, 15)).unwrap().value()
}

fn waiver_for(c: WaiverCause, on: Date, born: Date) -> Positions {
    estimated::new_positions()
        .waiver_for(c, on)
        .born(born)
        .build()
        .unwrap()
}

/// Exceptions 4 and 5 are the federal pair, decided by the Commissioner:
/// retirement after 62 or disability in the tax year or the year before.
/// The penalty stays the figure before any waiver, and `if_waived` shows $0
/// only when the waiver is open to the facts.
#[test]
fn estimated_waiver_outcomes() {
    let before = waiver_penalty(
        estimated::new_positions()
            .waiver(WaiverKind::None)
            .build()
            .unwrap(),
    )
    .total();
    assert_ne!(before.0, 0, "no penalty arises, so no waiver is decided");
    let none = Date::default();
    for (name, pos, want, reason) in [
        (
            "retired in 2026 at 64",
            waiver_for(WaiverCause::Retirement, day(2026, 6, 1), day(1962, 3, 1)),
            "eligible",
            "The Commissioner needs",
        ),
        (
            "retired in 2025 at 65",
            waiver_for(WaiverCause::Retirement, day(2025, 6, 1), day(1960, 3, 1)),
            "eligible",
            "retired in 2025",
        ),
        (
            "retired in 2020",
            waiver_for(WaiverCause::Retirement, day(2020, 6, 1), day(1955, 3, 1)),
            "refused",
            "2025 or 2026",
        ),
        (
            "retired at 60",
            waiver_for(WaiverCause::Retirement, day(2026, 6, 1), day(1966, 3, 1)),
            "refused",
            "reached 62",
        ),
        (
            "retired, no date",
            waiver_for(WaiverCause::Retirement, none, none),
            "undecided",
            "the date you retired",
        ),
        (
            "disabled in 2026",
            waiver_for(WaiverCause::Disability, day(2026, 2, 1), none),
            "eligible",
            "disabled in 2026",
        ),
        (
            "a casualty",
            waiver_for(WaiverCause::Casualty, day(2026, 8, 1), none),
            "eligible",
            "a casualty",
        ),
    ] {
        let p = waiver_penalty(pos);
        let Some(o) = p.waiver_outcome() else {
            panic!("{name}: no waiver outcome");
        };
        let (got, if_waived) = if o.eligible() {
            ("eligible", USCents(0))
        } else if o.refused() {
            ("refused", p.total())
        } else {
            ("undecided", p.total())
        };
        assert!(
            got == want && o.reason().contains(reason),
            "{name}: {got}, {:?}; want {want}, mentioning {reason:?}",
            o.reason()
        );
        assert!(p.total() == before && p.if_waived() == if_waived, "{name}");
    }
}
