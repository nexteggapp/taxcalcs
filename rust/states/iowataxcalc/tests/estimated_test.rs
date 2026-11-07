//! Ported from iowataxcalc/estimated_test.go and estimated_refund_test.go:
//! Iowa's estimated tax rules for 2026, checked against the Department of
//! Revenue's documents and the Iowa Code.
//!
//! Iowa publishes no exhaustive oracle for estimated tax, and the IA 2210
//! carries no worked example with a penalty figure. The strongest check is
//! the worked example in the 1040 expanded instructions for line 35, which
//! states how each payment is applied and how many days each piece is
//! charged: the test reproduces its application and its day counts. The
//! boundaries, the exceptions and the late start are hand-written cases
//! following the IA 2210's line arithmetic. The Go checks that compare each
//! figure and quote against the extracts in testdata/ are not ported: they
//! check the Go package's citations, not the rules.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, FactsBuilder, Jurisdiction, Month, Payment, Penalty,
    Positions, PriorReturn, Status as InstallmentStatus, WaiverCause, WaiverKind, Weekday,
};
use filing::Status;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_iowataxcalc::{EstimatedFacts, estimated_tax, new_estimated_facts};
use working::{PositionKind, Reading, Required, Rule};

const IOWA: Jurisdiction = Jurisdiction::from_static("IA");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

fn applied(rs: &[Rule], s: &str) -> bool {
    rs.iter().any(|r| r.applied().contains(s))
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

/// The 2026 dates after the weekend rule.
fn dues() -> [Date; 4] {
    [
        day(2026, 4, 30),
        day(2026, 6, 30),
        day(2026, 9, 30),
        day(2027, 2, 1),
    ]
}

#[derive(Clone, Default)]
struct Household {
    status: Status,
    /// IA 2210 line 3.
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
        let status = if self.status == Status::Unspecified {
            Status::Single
        } else {
            self.status
        };
        let figures = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(IOWA, 2026);
        b.filing_status(status)
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
                    .prior_year_modifications(USCents(0))
                    .build()
                    .unwrap(),
            ),
        };
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed twelve-month 2025 return with its tax and federal AGI.
fn prior(tax: USCents, agi: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(
                estimated::new_figures()
                    .tax(tax)
                    .federal_agi(agi)
                    .build()
                    .unwrap(),
            )
            .build()
            .unwrap(),
    )
}

fn mods(a: USCents) -> Option<EstimatedFacts> {
    Some(
        new_estimated_facts()
            .prior_year_modifications(a)
            .build()
            .unwrap(),
    )
}

fn penalty(h: &Household, as_of: Date) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), as_of).unwrap()
}

#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    assert_eq!(day(2027, 1, 31).weekday(), Weekday::SUNDAY);
    let res = rules().schedule(&tax(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i], "installment {}", i + 1);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    assert!(
        applied(
            &res.applied(),
            "January 31, 2027 falls on a weekend or holiday, so it is due February 1, 2027"
        ),
        "the move is not explained"
    );
}

/// § 422.16(12)(a)(1): $1,000 or more after withholding requires estimates,
/// and under it nothing is required and no penalty arises.
#[test]
fn estimated_threshold_boundaries() {
    // $999.99 left; then $1,000.00 left, short of 90% of $5,000.
    for (withheld, exempt) in [(400_001, true), (400_000, false)] {
        let h = Household {
            withheld: USCents(withheld),
            ..tax(DOLLAR * 5_000)
        };
        let r = rules().requirement(&h.facts()).unwrap().value();
        assert!(
            r.exempt() == exempt && r.required() != exempt,
            "withheld {withheld}: {}",
            r.why()
        );
        if exempt {
            let c = r.chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount() == USCents(0)
                    && c.label() == "Tax less withholding under $1,000"
            );
        }
        let p = penalty(&h, day(2027, 4, 30)).value();
        assert_eq!(p.total() == USCents(0), exempt, "withheld {withheld}");
    }
}

/// IA 2210 line 7: 110% of last year's tax when last year's federal AGI
/// plus Iowa net modifications exceeds $150,000, or $75,000 married filing
/// separately; 100% at exactly the step.
#[test]
fn estimated_prior_year_step_boundaries() {
    for (status, agi, m, want) in [
        (
            Status::Single,
            DOLLAR * 140_000,
            DOLLAR * 10_000,
            DOLLAR * 10_000,
        ),
        (
            Status::Single,
            DOLLAR * 140_000,
            USCents(1_000_001),
            DOLLAR * 11_000,
        ),
        (
            Status::Single,
            DOLLAR * 160_000,
            DOLLAR * -10_000,
            DOLLAR * 10_000,
        ),
        (
            Status::MarriedFilingJointly,
            DOLLAR * 150_000,
            USCents(0),
            DOLLAR * 10_000,
        ),
        (
            Status::MarriedFilingSeparately,
            DOLLAR * 75_000,
            USCents(0),
            DOLLAR * 10_000,
        ),
        (
            Status::MarriedFilingSeparately,
            USCents(7_500_001),
            USCents(0),
            DOLLAR * 11_000,
        ),
    ] {
        let h = Household {
            status,
            prior: prior(DOLLAR * 10_000, agi),
            extras: mods(m),
            ..tax(DOLLAR * 20_000)
        };
        let r = rules().requirement(&h.facts()).unwrap().value();
        assert_eq!(r.target(), want, "{status:?}, AGI {agi}, modifications {m}");
    }
}

/// The target is the smaller of the two tests. The prior-year test needs a
/// filed twelve-month return; an unknown AGI or modification assumes 110%.
#[test]
fn estimated_target_options() {
    let h = Household {
        prior: prior(DOLLAR * 12_000, DOLLAR * 100_000),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert_eq!(r.chosen().unwrap().basis(), Basis::ThisYearTax);
    assert_eq!(r.target(), DOLLAR * 9_000);
    let h = Household {
        prior: prior(DOLLAR * 8_000, DOLLAR * 100_000),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearTax);
    assert_eq!(r.target(), DOLLAR * 8_000);
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 5_000)
                .federal_agi(DOLLAR * 50_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    for (name, p, ask) in [
        ("no return on record", None, PositionKind::PriorYearReturn),
        ("not filed", Some(not_filed), PositionKind::Unspecified),
        ("eleven months", Some(short), PositionKind::Unspecified),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let o = &res.value().options()[1];
        assert!(
            !o.available() && !o.why_not().is_empty(),
            "{name}: the prior-year test is offered"
        );
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}: {ask} is not asked");
        }
    }
    let no_agi = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 5_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let unanswered = new_estimated_facts().build().unwrap();
    for (name, h, ask) in [
        (
            "AGI unknown",
            Household {
                prior: Some(no_agi),
                ..tax(DOLLAR * 20_000)
            },
            PositionKind::AdjustedGrossIncome,
        ),
        (
            "modifications unknown",
            Household {
                prior: prior(DOLLAR * 5_000, DOLLAR * 10_000),
                extras: Some(unanswered),
                ..tax(DOLLAR * 20_000)
            },
            PositionKind::JurisdictionFact,
        ),
    ] {
        let res = rules().requirement(&h.facts()).unwrap();
        let o = &res.value().options()[1];
        assert!(
            o.percent() == percent(110)
                && o.amount() == DOLLAR * 5_500
                && has(&res.required(), ask),
            "{name}: {} of last year, {}",
            o.percent(),
            o.amount()
        );
    }
}

/// A twelve-month 2025 with no Iowa tax, or with no return because none was
/// required, makes the required payment nothing (IA 2210 lines 7 and 8;
/// 701—308.6(1)(a)). That needs nothing about this year, so it holds without
/// this year's tax and its zero is exact.
#[test]
fn estimated_no_liability_last_year() {
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    for (name, p) in [
        ("no tax", prior(USCents(0), DOLLAR * 20_000)),
        ("not required", Some(not_required)),
    ] {
        for no_tax in [false, true] {
            let h = Household {
                no_tax,
                prior: p,
                ..tax(DOLLAR * 50_000)
            };
            let res = rules().requirement(&h.facts()).unwrap();
            assert!(
                res.value().exempt() && !res.value().required(),
                "{name} (this year's tax known {}): {}",
                !no_tax,
                res.value().why()
            );
            assert!(
                !has(&res.required(), PositionKind::ThisYearTax),
                "{name}: this year's tax is asked for though it cannot matter"
            );
            let pen = penalty(&h, day(2027, 4, 30)).value();
            assert!(pen.total() == USCents(0) && !pen.upper_bound(), "{name}");
        }
    }
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(
            estimated::new_figures()
                .tax(USCents(0))
                .federal_agi(USCents(0))
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let h = Household {
        prior: Some(short),
        ..tax(DOLLAR * 50_000)
    };
    assert!(
        !rules().requirement(&h.facts()).unwrap().value().exempt(),
        "an eleven-month year with no tax excuses the penalty"
    );
}

/// The IA 2210 table: a requirement first met after April 1 starts at the
/// next row, and IA 2210 line 17 divides the year's amount among the
/// installments that remain.
#[test]
fn estimated_late_start() {
    // 90% of $11,111.11 is $10,000.00.
    let t = USCents(1_111_111);
    let four = [250_000, 500_000, 750_000, 1_000_000];
    let three = [0, 333_333, 666_667, 1_000_000];
    let two = [0, 0, 500_000, 1_000_000];
    let one = [0, 0, 0, 1_000_000];
    for (met, want) in [
        (day(2026, 4, 1), four),
        (day(2026, 4, 2), three),
        (day(2026, 6, 1), three),
        (day(2026, 6, 2), two),
        (day(2026, 9, 1), two),
        (day(2026, 9, 2), one),
    ] {
        let h = Household {
            first_met: met,
            ..tax(t)
        };
        let sched = rules().schedule(&h.facts()).unwrap().value();
        for (i, inst) in sched.iter().enumerate() {
            assert_eq!(
                inst.needed_by(),
                USCents(want[i]),
                "first met {met}: installment {}",
                i + 1
            );
            if want[i] == 0 {
                assert_eq!(inst.status(), InstallmentStatus::NothingDue);
            }
        }
    }
    let res = rules().requirement(&tax(t).facts()).unwrap();
    assert!(has(&res.required(), PositionKind::RequirementFirstMet));
    let h = Household {
        first_met: day(2026, 1, 1),
        ..tax(t)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!has(&res.required(), PositionKind::RequirementFirstMet));
}

/// The 1040 expanded instructions for line 35, IA 2210 Example 2: $1,000 due
/// each quarter, paid $500 on June 15, $2,000 on June 30, $500 on January 31
/// and $1,000 on February 25. Its April pieces are charged 46 and 61 days,
/// its September $500 92 days in the first year and 31 in the next, and its
/// fourth $1,000 25 days from January 31. In 2026 the fourth installment is
/// due February 1, 2027, so that piece is 24 days, and the 2027 days have no
/// published rate.
/// A penalty line: its installment, shortfall, days and parts (principal,
/// days, charge).
type WantLine = (i64, USCents, i64, Vec<(USCents, i64, i64)>);

#[test]
fn estimated_penalty_matches_line_thirty_five_example() {
    let h = Household {
        prior: prior(DOLLAR * 4_000, DOLLAR * 100_000),
        payments: vec![
            pay(day(2026, 6, 15), DOLLAR * 500),
            pay(day(2026, 6, 30), DOLLAR * 2_000),
            pay(day(2027, 1, 31), DOLLAR * 500),
            pay(day(2027, 2, 25), DOLLAR * 1_000),
        ],
        ..tax(DOLLAR * 10_000)
    };
    let res = penalty(&h, day(2027, 4, 30));
    let lines = res.value().lines();
    assert_eq!(lines.len(), 3, "want April, September and February");
    let want: [WantLine; 3] = [
        // $500 × 46 × .00027397 = $6.30; × 61 = $8.36.
        (
            1,
            DOLLAR * 1_000,
            61,
            vec![(DOLLAR * 500, 46, 630), (DOLLAR * 500, 61, 836)],
        ),
        // $500 × 92 × .00027397 = $12.60; the 31 days of 2027 are unrated.
        (3, DOLLAR * 500, 123, vec![(DOLLAR * 500, 92, 1_260)]),
        (4, DOLLAR * 1_000, 24, vec![]),
    ];
    for (i, (installment, shortfall, days, parts)) in want.iter().enumerate() {
        let l = &lines[i];
        assert_eq!(
            (l.installment(), l.shortfall(), l.days()),
            (*installment, *shortfall, *days),
            "line {}",
            i + 1
        );
        let got = l.parts();
        assert_eq!(got.len(), parts.len(), "line {}", i + 1);
        for (j, p) in got.iter().enumerate() {
            assert_eq!(
                (p.principal(), p.days(), p.charge()),
                (parts[j].0, parts[j].1, USCents(parts[j].2)),
                "line {} part {}",
                i + 1,
                j + 1
            );
        }
    }
    assert_eq!(res.value().total(), USCents(2_726));
    assert!(has(&res.required(), PositionKind::UnpublishedRate));
}

/// The penalty over the year end. The published rates for 2025 and 2026 are
/// both 10%, and no 2026 shortfall can reach back into 2025, so the only
/// boundary inside a 2026 penalty is December 31, 2026, after which the rate
/// is not published. A $1,000 April shortfall never paid runs 245 days in
/// 2026: $1,000 × 245 × .00027397 = $67.12. Past the year end the charge is
/// the same and the gap is reported, and the penalty stops at April 30,
/// 2027.
#[test]
fn estimated_penalty_across_the_year_end() {
    let h = Household {
        prior: prior(DOLLAR * 4_000, DOLLAR * 100_000),
        ..tax(DOLLAR * 10_000)
    };
    for (as_of, stop, unrated) in [
        (day(2026, 12, 31), day(2026, 12, 31), false),
        (day(2027, 4, 30), day(2027, 4, 30), true),
        (day(2027, 6, 30), day(2027, 4, 30), true),
    ] {
        let res = penalty(&h, as_of);
        let parts = res.value().lines()[0].parts();
        assert!(
            parts.len() == 1
                && parts[0].days() == 245
                && parts[0].charge() == USCents(6_712)
                && parts[0].to() == day(2026, 12, 31),
            "to {as_of}"
        );
        assert_eq!(res.value().as_of(), stop, "to {as_of}");
        assert_eq!(
            has(&res.required(), PositionKind::UnpublishedRate),
            unrated,
            "to {as_of}"
        );
    }
}

/// The interest follows the printed .00027397 a day, not 10% ÷ 365. On a
/// $100,000 shortfall for 245 days the two give $6,712.27 and $6,712.33.
#[test]
fn estimated_interest_uses_the_printed_daily_factor() {
    // 90% of $444,444.44 is $400,000.00, a quarter of which is $100,000.
    let res = penalty(&tax(USCents(44_444_444)), day(2026, 12, 31));
    let parts = res.value().lines()[0].parts();
    assert!(
        parts.len() == 1
            && parts[0].principal() == DOLLAR * 100_000
            && parts[0].days() == 245
            && parts[0].charge() == USCents(671_227)
    );
}

/// A return filed with the balance paid stops the penalty that day.
#[test]
fn estimated_return_filed_stops_the_penalty() {
    let h = Household {
        return_filed: day(2027, 3, 1),
        ..tax(DOLLAR * 10_000)
    };
    let p = penalty(&h, day(2027, 4, 30));
    assert_eq!(p.value().as_of(), day(2027, 3, 1));
    assert!(applied(
        &p.applied(),
        "every shortfall still outstanding was paid that day"
    ));
}

/// IA 2210 line 20: withholding is spread in equal parts over the dates
/// unless the filer shows when it was withheld.
#[test]
fn estimated_withholding_credit() {
    // 90% of $20,000 is $18,000, all of it withheld on December 15.
    let mut h = Household {
        withheld: DOLLAR * 18_000,
        ..tax(DOLLAR * 20_000)
    };
    for (i, inst) in rules()
        .schedule(&h.facts())
        .unwrap()
        .value()
        .iter()
        .enumerate()
    {
        assert_eq!(inst.status(), InstallmentStatus::Met, "spread: {}", i + 1);
    }
    h.as_withheld = true;
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [
        InstallmentStatus::Short,
        InstallmentStatus::Short,
        InstallmentStatus::Short,
        InstallmentStatus::Met,
    ]
    .into_iter()
    .enumerate()
    {
        assert_eq!(sched[i].status(), want, "as withheld: {}", i + 1);
    }
}

/// The penalty is before any waiver, and the waiver is asked for until
/// answered.
#[test]
fn estimated_waiver_is_asked() {
    let mut h = tax(DOLLAR * 10_000);
    assert!(has(
        &penalty(&h, day(2027, 4, 30)).required(),
        PositionKind::PenaltyWaiver
    ));
    h.waiver = Some(WaiverKind::RetiredOrDisabled);
    let res = penalty(&h, day(2027, 4, 30));
    assert!(
        !has(&res.required(), PositionKind::PenaltyWaiver)
            && res.value().waiver() == WaiverKind::RetiredOrDisabled
            && res.value().total().0 != 0
    );
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want = [
        (
            "The small-balance threshold",
            [
                "below $200",
                "10/22/2025",
                "below $1,000 of Iowa tax after withholding",
                "2025 Iowa Acts ch. 148, § 59, effective January 1, 2026",
            ],
        ),
        (
            "The daily interest factor",
            [
                ".00027397",
                "lines 24a and 24b",
                "0.000273973",
                "§§ 422.16(12)(d)(3), 421.7(2)",
            ],
        ),
        (
            "The waiver for disability in the year before",
            [
                "disability, in the tax year",
                "Waiver of Penalty",
                "disability in the year before",
                "701—308.6(2)(b)",
            ],
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, w)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(
            d.form().contains(w[0])
                && d.form_source().contains(w[1])
                && d.statute().contains(w[2])
                && d.statute_source().contains(w[3]),
            "{:?} does not pin both readings",
            d.rule()
        );
        // $10,000 due with nothing withheld is over both thresholds, and no
        // waiver is asked for.
        let want_uses = if d.rule() == "The small-balance threshold" {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert_eq!(d.uses(), want_uses, "{:?}", d.rule());
        assert!(
            d.rule() == "The daily interest factor" || !d.affects_household(),
            "{:?} affects a household it does not touch",
            d.rule()
        );
    }
    // $500 left after withholding is under $1,000 but not under $200, so the
    // two thresholds differ.
    let h = Household {
        withheld: DOLLAR * 4_500,
        ..tax(DOLLAR * 5_000)
    };
    for d in rules().requirement(&h.facts()).unwrap().divergences() {
        assert!(d.rule() != "The small-balance threshold" || d.affects_household());
    }
    // On a $100,000 shortfall for 245 days the printed factor and the rate
    // give different penalties.
    for d in rules()
        .requirement(&tax(USCents(44_444_444)).facts())
        .unwrap()
        .divergences()
    {
        assert!(d.rule() != "The daily interest factor" || d.affects_household());
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for y in [2025, 2027] {
        let err = estimated_tax(y).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{y}: {err}");
    }
    let this = estimated::new_figures()
        .tax(DOLLAR * 10_000)
        .build()
        .unwrap();
    let base = || -> FactsBuilder {
        let mut b = estimated::new_facts(IOWA, 2026);
        b.filing_status(Status::Single).this_year(this);
        b
    };
    let periods: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 5, 31),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .iter()
    .map(|&e| estimated::new_period().end(e).build().unwrap())
    .collect();
    for (name, f) in [
        ("a farmer", base().farm_share(Rate(7000)).build().unwrap()),
        ("a fisher", base().fish_share(Rate(7000)).build().unwrap()),
        (
            "annualised",
            base()
                .elections(estimated::new_elections().annualised().build().unwrap())
                .periods(&periods)
                .build()
                .unwrap(),
        ),
    ] {
        let err = rules().requirement(&f).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
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
            estimated::new_facts(IOWA, 2027)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        ("other extras", base().state("not Iowa's").build().unwrap()),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&base().build().unwrap(), Date::default())
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
        "$1,000",
        "$200",
        "90%",
        "110%",
        "net modifications",
        "$150,000",
        "April 30",
        "February 1, 2027",
        "fewer",
        "10% a year",
        "IA 2210F",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
}

/// Without this year's tax the target is last year's test alone, the
/// penalty is an upper bound, and with no prior return there is no target.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 4_000, DOLLAR * 100_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(
        r.required() && r.target() == DOLLAR * 4_000 && r.target_known(),
        "{}",
        r.why()
    );
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearTax);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [1_000, 2_000, 3_000, 4_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), DOLLAR * want, "installment {}", i + 1);
    }
    let pen = penalty(&h, day(2027, 4, 30)).value();
    assert!((pen.upper_bound() || !pen.rated_through().is_zero()) && pen.total().0 != 0);
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let r = rules().requirement(&none.facts()).unwrap().value();
    assert!(!r.target_known() && !r.required());
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(
        err.is(&estimated::Error::NeedsThisYearTax),
        "schedule: {err}"
    );
    let err = rules()
        .penalty(&none.facts(), day(2027, 4, 30))
        .err()
        .unwrap();
    assert!(
        err.is(&estimated::Error::NeedsThisYearTax),
        "penalty: {err}"
    );
}

/// Iowa's waivers are the federal pair, but the 2025 IA 2210, whose reading
/// is followed, covers retiring or becoming disabled in the tax year only,
/// where federal law also covers the year before. Disability in 2025, which
/// the rule covers, is refused by the form and reported as the divergence.
/// Retiring in 2025 is open in both documents. The penalty is the same
/// before any waiver in each case.
#[test]
fn estimated_waiver_outcomes() {
    let mut h = tax(DOLLAR * 10_000);
    let base = penalty(&h, day(2027, 4, 30)).value().total();
    assert_ne!(base, USCents(0), "no penalty to waive");
    let pos = |cause: WaiverCause, on: Date, born: Option<Date>| -> Positions {
        let mut b = estimated::new_positions();
        b.waiver_for(cause, on);
        if let Some(born) = born {
            b.born(born);
        }
        b.build().unwrap()
    };
    for (name, p, want, says, unsettled, diverges) in [
        (
            "retired in 2026 at 64",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1962, 3, 1)),
            ),
            "eligible",
            "at 64",
            false,
            false,
        ),
        (
            "retired in 2026 at 60",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1966, 3, 1)),
            ),
            "refused",
            "62 or older",
            false,
            false,
        ),
        (
            "retired in 2020",
            pos(
                WaiverCause::Retirement,
                day(2020, 6, 1),
                Some(day(1955, 3, 1)),
            ),
            "refused",
            "covers retiring in 2026",
            false,
            false,
        ),
        (
            "retired in 2025",
            pos(
                WaiverCause::Retirement,
                day(2025, 6, 1),
                Some(day(1960, 3, 1)),
            ),
            "undecided",
            "year before",
            true,
            false,
        ),
        (
            "disabled in 2026",
            pos(WaiverCause::Disability, day(2026, 2, 1), None),
            "eligible",
            "became disabled in 2026",
            false,
            false,
        ),
        (
            "disabled in 2025",
            pos(WaiverCause::Disability, day(2025, 9, 1), None),
            "refused",
            "only for becoming disabled in 2026",
            false,
            true,
        ),
        (
            "a disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
            "eligible",
            "Iowa Department of Revenue",
            false,
            false,
        ),
    ] {
        h.positions = Some(p);
        let res = penalty(&h, day(2027, 4, 30));
        let p = res.value();
        let o = p.waiver_outcome().unwrap();
        let got = format!(
            "{}{}{}",
            if o.eligible() { "eligible" } else { "" },
            if o.refused() { "refused" } else { "" },
            if o.undecided() { "undecided" } else { "" }
        );
        assert_eq!(p.total(), base, "{name}: before any waiver");
        assert!(
            got == want && o.reason().contains(says),
            "{name}: {got} {:?}",
            o.reason()
        );
        assert_eq!(o.eligible(), p.if_waived() == USCents(0), "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::UnsettledRule),
            unsettled,
            "{name}"
        );
        for d in res.divergences() {
            if d.rule() == "The waiver for disability in the year before" {
                assert_eq!(d.affects_household(), diverges, "{name}");
            }
        }
    }
}

/// Under the $1,000 exception the target is $0, so last year's return isn't
/// asked for as a way to change it. Above it, it is.
#[test]
fn estimated_prior_return_asked_only_when_it_can_set_the_target() {
    let under = rules().requirement(&tax(DOLLAR * 800).facts()).unwrap();
    assert!(under.value().exempt() && !has(&under.required(), PositionKind::PriorYearReturn));
    let over = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    assert!(has(&over.required(), PositionKind::PriorYearReturn));
}

/// Withholding over the tax reads as a refund, never as a minus amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let rules = rules();
    let mut asked = false;
    for a in estimatedtest::ask(&rules, &estimatedtest::households(&estimated::IOWA)) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked = true;
        let why = a.requirement().unwrap().value().why();
        assert!(
            why.contains("leaves a $600 refund. No estimated payments are needed"),
            "why {why:?}"
        );
        for x in estimatedtest::texts(Some(&rules as &dyn EstimatedTax), std::slice::from_ref(&a)) {
            assert!(
                !x.text().contains("−$") && !x.text().contains("-$"),
                "{}: {:?} has a minus amount",
                x.where_(),
                x.text()
            );
        }
    }
    assert!(asked, "no household withholding over the tax");
}
