//! Ported from delawaretaxcalc/estimated_test.go and
//! estimated_refund_test.go: Delaware's estimated tax rules for 2026,
//! checked against the Division of Revenue's own forms and the Delaware
//! Code.
//!
//! Delaware publishes no exhaustive oracle and no worked example with a
//! stated answer. The penalty arithmetic, the boundaries and the exceptions
//! are hand-written cases, worked by hand in their comments. The Go checks
//! that compare each figure and quote against the extracts in testdata/, and
//! that the statute lists no recent amendment, are not ported: they check
//! the Go package's citations, not the rules.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, FactsBuilder, Jurisdiction, Month, Payment, Penalty,
    Positions, PriorReturn, ResidencePeriod, Status as InstallmentStatus, WaiverCause, WaiverKind,
    Weekday,
};
use filing::Status;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_delawaretaxcalc::estimated_tax;
use working::{PositionKind, Reading, Required, Rule};

const DELAWARE: Jurisdiction = Jurisdiction::from_static("DE");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

fn because(rs: &[Required], k: PositionKind) -> String {
    rs.iter()
        .filter(|r| r.position() == k && r.asked())
        .map(|r| format!("{} {}", r.question(), r.effect()))
        .collect::<Vec<_>>()
        .join(" | ")
}

fn applied(rs: &[Rule]) -> String {
    rs.iter()
        .map(|r| format!("{}: {}", r.cite(), r.applied()))
        .collect::<Vec<_>>()
        .join(" | ")
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

fn dues() -> [Date; 4] {
    [
        day(2026, 4, 30),
        day(2026, 6, 15),
        day(2026, 9, 15),
        day(2027, 1, 15),
    ]
}

fn quarterly(a: &[USCents]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .map(|(i, &x)| pay(dues()[i], x))
        .collect()
}

fn resident(state: &str, from: Date, to: Date) -> ResidencePeriod {
    estimated::new_residence_period(state)
        .from(from)
        .to(to)
        .build()
        .unwrap()
}

#[derive(Clone, Default)]
struct Household {
    status: Status,
    /// PIT-RES line 33 less line 34.
    tax: USCents,
    no_tax: bool,
    withheld: Vec<(Date, USCents)>,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    /// Full-year Delaware when `None`.
    residence: Option<Vec<ResidencePeriod>>,
    no_residence: bool,
    return_filed: Date,
    first_met: Date,
    /// PIT-UND line 18 for each period.
    annualised: Option<Vec<USCents>>,
    as_withheld: bool,
    waiver: Option<WaiverKind>,
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
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        let mut b = estimated::new_facts(DELAWARE, 2026);
        b.filing_status(status)
            .this_year(fb.build().unwrap())
            .paid(&self.payments)
            .return_filed(self.return_filed)
            .requirement_first_met(self.first_met);
        let withheld: Vec<_> = self
            .withheld
            .iter()
            .map(|&(on, a)| estimated::new_withheld().on(on).amount(a).build().unwrap())
            .collect();
        b.withheld(&withheld);
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        match &self.residence {
            Some(r) => {
                b.residence(r);
            }
            None if !self.no_residence => {
                b.residence(&[resident("DE", day(2026, 1, 1), day(2026, 12, 31))]);
            }
            None => {}
        }
        let mut eb = estimated::new_elections();
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
            eb.annualised();
        }
        if self.as_withheld {
            eb.withholding_as_withheld();
        }
        b.elections(eb.build().unwrap());
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        b.build().unwrap()
    }
}

/// A filed twelve-month Delaware return with its tax and last year's
/// Delaware AGI.
fn prior(tax: USCents, delaware_agi: USCents) -> Option<PriorReturn> {
    let f = estimated::new_figures()
        .tax(tax)
        .state_agi(delaware_agi)
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

fn prior_with(f: estimated::Figures, months: i64) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .months(months)
            .figures(f)
            .build()
            .unwrap(),
    )
}

fn penalty(h: &Household, as_of: Date) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), as_of).unwrap()
}

/// The 2026 dates are the ones PIT-EST, its voucher and the PIT-RES
/// instructions print. None falls on a weekend, so none moves, and neither
/// does April 30, 2027, where the penalty stops.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&tax(DOLLAR * 40_000).facts()).unwrap();
    assert_eq!(res.value().len(), 4);
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i], "installment {}", i + 1);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    assert!(!applied(&res.applied()).contains("§ 551(d)"));
    assert_eq!(day(2027, 4, 30).weekday(), Weekday::FRIDAY);
}

/// PIT-UND line E: no penalty when the required annual payment less
/// withholding is $800 or less. $10,000 of tax and no prior return gives a
/// required payment of $9,000; $8,200 withheld leaves $800.00, exempt, and
/// $8,199.99 leaves $800.01, which is not.
#[test]
fn estimated_threshold_boundary() {
    for (withheld, exempt) in [(820_000, true), (819_999, false)] {
        let h = Household {
            withheld: vec![(day(2026, 12, 15), USCents(withheld))],
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {withheld}");
        assert_ne!(res.value().required(), exempt, "withheld {withheld}");
        let p = penalty(&h, day(2027, 4, 30));
        assert_eq!(
            p.value().total() == USCents(0),
            exempt,
            "withheld {withheld}"
        );
    }
    // The test is on the required payment, not this year's tax: $10,000 of
    // tax with nothing withheld, against last year's $800, is exempt.
    let h = Household {
        prior: prior(DOLLAR * 800, DOLLAR * 50_000),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(res.value().exempt(), "{}", res.value().why());
    // The exception is the chosen option, at $0, and the other options keep
    // their amounts.
    let c = res.value().chosen().unwrap();
    assert!(
        c.basis() == Basis::Exception
            && c.amount() == USCents(0)
            && c.label() == "Required payment less withholding of $800 or less"
    );
    let o = &res.value().options()[1];
    assert!(o.basis() == Basis::PriorYearTax && o.amount() == DOLLAR * 800);
    // The statute's test is on this year's tax, $10,000 with nothing
    // withheld, so it would require payments.
    for d in res.divergences() {
        if d.rule() == "The $800 test" {
            assert!(d.affects_household());
        }
    }
    let h = Household {
        prior: prior(USCents(80_001), DOLLAR * 50_000),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().exempt() && res.value().target() == USCents(80_001));
}

/// 110% when last year's AGI exceeds $150,000, or $75,000 for filing status
/// 3. Delaware AGI is the default, as PIT-UND says.
#[test]
fn estimated_prior_year_step_boundary() {
    for (status, agi, want) in [
        (Status::Single, 15_000_000, percent(100)),
        (Status::Single, 15_000_001, percent(110)),
        (Status::MarriedFilingJointly, 15_000_000, percent(100)),
        (Status::MarriedFilingJointly, 15_000_001, percent(110)),
        (Status::MarriedFilingSeparately, 7_500_000, percent(100)),
        (Status::MarriedFilingSeparately, 7_500_001, percent(110)),
        (Status::MarriedFilingJointly, 7_500_001, percent(100)),
    ] {
        let h = Household {
            status,
            prior: prior(DOLLAR * 10_000, USCents(agi)),
            ..tax(DOLLAR * 100_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let o = &res.value().options()[1];
        assert!(
            o.percent() == want && o.amount() == want.of(DOLLAR * 10_000),
            "{status:?}, AGI {agi}"
        );
    }
    // Which AGI is open: asked whenever the two could disagree.
    let figures = |state: Option<USCents>, federal: Option<USCents>| {
        let mut b = estimated::new_figures();
        b.tax(DOLLAR * 10_000);
        if let Some(s) = state {
            b.state_agi(s);
        }
        if let Some(f) = federal {
            b.federal_agi(f);
        }
        b.build().unwrap()
    };
    for (name, p, want, asked, open) in [
        (
            "both agree",
            prior_with(figures(Some(DOLLAR * 140_000), Some(DOLLAR * 145_000)), 12),
            percent(100),
            false,
            false,
        ),
        (
            "they disagree",
            prior_with(figures(Some(DOLLAR * 140_000), Some(DOLLAR * 160_000)), 12),
            percent(100),
            false,
            true,
        ),
        (
            "Delaware only",
            prior(DOLLAR * 10_000, DOLLAR * 140_000),
            percent(100),
            true,
            false,
        ),
        (
            "federal only",
            prior_with(figures(None, Some(DOLLAR * 160_000)), 12),
            percent(110),
            true,
            false,
        ),
        (
            "neither, assumed",
            prior_with(figures(None, None), 12),
            percent(110),
            true,
            false,
        ),
        (
            "federal only, low",
            prior_with(figures(None, Some(DOLLAR * 100_000)), 12),
            percent(100),
            true,
            false,
        ),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 100_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().options()[1].percent(), want, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::AdjustedGrossIncome),
            asked,
            "{name}"
        );
        // Which AGI applies is the forms' disagreement, not the person's to
        // answer.
        assert_eq!(
            because(&res.required(), PositionKind::UnsettledRule)
                .contains("prior-year percentage follows"),
            open,
            "{name}"
        );
    }
}

/// The target is the smaller of 90% of this year's tax and the prior-year
/// test, which needs a filed Delaware return showing a tax. No Delaware
/// source limits it to a twelve-month year.
#[test]
fn estimated_target_options() {
    // 90% of $40,000 is $36,000; 100% of $20,000 is $20,000.
    let h = Household {
        prior: prior(DOLLAR * 20_000, DOLLAR * 100_000),
        ..tax(DOLLAR * 40_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(r.chosen().unwrap().basis() == Basis::PriorYearTax && r.target() == DOLLAR * 20_000);
    // 100% of $37,000 is more than $36,000.
    let h = Household {
        prior: prior(DOLLAR * 37_000, DOLLAR * 100_000),
        ..tax(DOLLAR * 40_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(r.chosen().unwrap().basis() == Basis::ThisYearTax && r.target() == DOLLAR * 36_000);
    assert_eq!(r.options()[0].percent(), percent(90));
    let short = prior_with(
        estimated::new_figures()
            .tax(DOLLAR * 5_000)
            .state_agi(DOLLAR * 50_000)
            .build()
            .unwrap(),
        6,
    );
    let h = Household {
        prior: short,
        ..tax(DOLLAR * 40_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(r.options()[1].available() && r.target() == DOLLAR * 5_000);
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    for (name, p, ask) in [
        ("no return on record", None, true),
        ("not filed", Some(not_filed), false),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 40_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(!res.value().options()[1].available(), "{name}");
        assert_eq!(res.value().target(), DOLLAR * 36_000, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn),
            ask,
            "{name}"
        );
    }
}

/// $40,000 of tax, nothing withheld or paid, no return last year: the
/// required payment is 90%, $36,000, and each installment $9,000. Each is
/// charged 0.0005 a day from line 39's date to April 30, 2027:
///
/// ```text
/// May 1:    364 days × 0.0005 = 0.1820 × 9,000 = $1,638.00
/// June 15:  319 days × 0.0005 = 0.1595 × 9,000 = $1,435.50
/// Sept 15:  227 days × 0.0005 = 0.1135 × 9,000 = $1,021.50
/// Jan 15:   105 days × 0.0005 = 0.0525 × 9,000 =   $472.50
/// ```
///
/// The total is $4,567.50.
#[test]
fn estimated_penalty_arithmetic() {
    let res = penalty(&tax(DOLLAR * 40_000), day(2027, 4, 30));
    let p = res.value();
    let want = [
        (day(2026, 5, 1), 364, 163_800),
        (day(2026, 6, 15), 319, 143_550),
        (day(2026, 9, 15), 227, 102_150),
        (day(2027, 1, 15), 105, 47_250),
    ];
    let lines = p.lines();
    assert_eq!(lines.len(), 4);
    for (i, l) in lines.iter().enumerate() {
        let parts = l.parts();
        assert!(
            l.shortfall() == DOLLAR * 9_000 && parts.len() == 1,
            "line {}",
            i + 1
        );
        let (from, days, charge) = want[i];
        assert!(
            parts[0].from() == from
                && parts[0].days() == days
                && parts[0].charge() == USCents(charge)
                && parts[0].rate() == percent(18),
            "line {}",
            i + 1
        );
    }
    assert_eq!(p.total(), USCents(456_750));
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));
    // It stops on April 30, 2027.
    let later = penalty(&tax(DOLLAR * 40_000), day(2027, 6, 30));
    assert!(later.value().as_of() == day(2027, 4, 30) && later.value().total() == USCents(456_750));
    // A payment pays the oldest shortfall first. $9,000 on May 15 pays the
    // first: 14 days from May 1, 9,000 × 0.0070 = $63.00.
    let h = Household {
        payments: vec![pay(day(2026, 5, 15), DOLLAR * 9_000)],
        ..tax(DOLLAR * 40_000)
    };
    let p = penalty(&h, day(2026, 6, 1)).value();
    let l = p.lines();
    assert!(
        !l.is_empty()
            && l[0].installment() == 1
            && l[0].total() == USCents(6_300)
            && p.total() == USCents(6_300)
    );
    // The penalty stops on the day the return is filed and paid.
    let h = Household {
        return_filed: day(2027, 3, 1),
        ..tax(DOLLAR * 40_000)
    };
    assert_eq!(
        penalty(&h, day(2027, 4, 30)).value().as_of(),
        day(2027, 3, 1)
    );
}

/// PIT-UND line 30 counts withholding in the period it was withheld:
/// January to March toward April 30, April and May toward June 15, June to
/// August toward September 15, September to December toward January 15.
/// $1,000 on each side of each period end shows where it lands.
#[test]
fn estimated_withholding_counts_in_its_period() {
    for (on, first) in [
        (day(2026, 3, 31), 0),
        (day(2026, 4, 1), 1),
        (day(2026, 5, 31), 1),
        (day(2026, 6, 1), 2),
        (day(2026, 8, 31), 2),
        (day(2026, 9, 1), 3),
        (day(2026, 12, 31), 3),
    ] {
        let h = Household {
            withheld: vec![(on, DOLLAR * 1_000)],
            ..tax(DOLLAR * 40_000)
        };
        let s = rules().schedule(&h.facts()).unwrap().value();
        for (i, inst) in s.iter().enumerate() {
            let want = if i >= first {
                DOLLAR * 1_000
            } else {
                USCents(0)
            };
            assert_eq!(
                inst.credited_by(),
                want,
                "withheld {on}: installment {}",
                i + 1
            );
        }
    }
    // There is no election; asking for one changes nothing.
    let h = Household {
        withheld: vec![(day(2026, 4, 1), DOLLAR * 1_000)],
        as_withheld: true,
        ..tax(DOLLAR * 40_000)
    };
    let res = rules().schedule(&h.facts()).unwrap();
    assert!(
        res.value()[0].credited_by() == USCents(0)
            && applied(&res.applied()).contains("no election about withholding")
    );
    // Last year's overpayment counts toward the first installment.
    let h = Household {
        payments: vec![
            estimated::new_payment()
                .amount(DOLLAR * 9_000)
                .from_prior_year()
                .build()
                .unwrap(),
        ],
        ..tax(DOLLAR * 40_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].status(), InstallmentStatus::Met);
}

/// PIT-UND Part 3 with line 18 of $8,000, $20,000, $30,000 and $40,000, and
/// a required payment of $36,000, $9,000 a quarter: $1,800, $9,000, $20,250
/// and $36,000 are needed by the four dates.
#[test]
fn estimated_annualised_installments() {
    let mut h = Household {
        annualised: Some(vec![
            DOLLAR * 8_000,
            DOLLAR * 20_000,
            DOLLAR * 30_000,
            DOLLAR * 40_000,
        ]),
        ..tax(DOLLAR * 40_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [180_000, 900_000, 2_025_000, 3_600_000]
        .into_iter()
        .enumerate()
    {
        assert_eq!(s[i].needed_by(), USCents(want), "installment {}", i + 1);
    }
    // With line 18 for the full year at $32,000, column 4's 90% binds: line
    // 22 is 28,800 − 20,250 = $8,550, under line 25's $15,750, so $28,800 is
    // needed by January 15.
    if let Some(a) = h.annualised.as_mut() {
        a[3] = DOLLAR * 32_000;
    }
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[3].needed_by(), USCents(2_880_000));
    // The periods must be the form's.
    let periods: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 6, 30),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .iter()
    .map(|&e| estimated::new_period().end(e).build().unwrap())
    .collect();
    let f = estimated::new_facts(DELAWARE, 2026)
        .filing_status(Status::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .periods(&periods)
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    assert!(
        rules().schedule(&f).is_err(),
        "a period ending June 30 is accepted"
    );
}

/// The exceptions: the $800 test, no liability last year, a part-year
/// resident, and farmers (refused). The waivers are asked for after
/// assessment, so the penalty is shown before them.
#[test]
fn estimated_exceptions() {
    let zero = prior_with(
        estimated::new_figures().tax(USCents(0)).build().unwrap(),
        12,
    );
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    for (name, p) in [
        ("no tax shown", zero),
        ("no return required", Some(not_required)),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 40_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(res.value().exempt(), "{name}: {}", res.value().why());
        assert_eq!(
            penalty(&h, day(2027, 4, 30)).value().total(),
            USCents(0),
            "{name}"
        );
    }
    // No liability for a six-month year: PIT-EST's twelve-month condition is
    // the default, so the exception is not applied, and the question is
    // asked.
    let short_zero = estimated::new_prior_return()
        .not_required_to_file()
        .months(6)
        .build()
        .unwrap();
    let h = Household {
        prior: Some(short_zero),
        ..tax(DOLLAR * 40_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(
        !res.value().exempt()
            && because(&res.required(), PositionKind::UnsettledRule).contains("twelve-month")
    );
    // A part-year resident owes no penalty; a full-year nonresident does.
    let part = Household {
        residence: Some(vec![
            resident("PA", day(2026, 1, 1), day(2026, 6, 30)),
            resident("DE", day(2026, 7, 1), day(2026, 12, 31)),
        ]),
        ..tax(DOLLAR * 40_000)
    };
    let res = rules().requirement(&part.facts()).unwrap();
    assert!(res.value().exempt() && res.value().why().contains("part-year resident"));
    let split = Household {
        residence: Some(vec![
            resident("DE", day(2026, 1, 1), day(2026, 6, 30)),
            resident("DE", day(2026, 7, 1), day(2026, 12, 31)),
        ]),
        ..tax(DOLLAR * 40_000)
    };
    assert!(
        !rules()
            .requirement(&split.facts())
            .unwrap()
            .value()
            .exempt()
    );
    let non = Household {
        residence: Some(vec![resident("PA", day(2026, 1, 1), day(2026, 12, 31))]),
        ..tax(DOLLAR * 40_000)
    };
    let res = rules().requirement(&non.facts()).unwrap();
    assert!(
        !res.value().exempt()
            && res.value().required()
            && !has(&res.required(), PositionKind::Residency)
    );
    let unknown = Household {
        no_residence: true,
        ..tax(DOLLAR * 40_000)
    };
    let res = rules().requirement(&unknown.facts()).unwrap();
    assert!(!res.value().exempt() && has(&res.required(), PositionKind::Residency));
    // A waiver asked for is not subtracted.
    let h = Household {
        waiver: Some(WaiverKind::Casualty),
        ..tax(DOLLAR * 40_000)
    };
    let pen = penalty(&h, day(2027, 4, 30));
    assert!(
        pen.value().total() == USCents(456_750)
            && pen.value().waiver() == WaiverKind::Casualty
            && !has(&pen.required(), PositionKind::PenaltyWaiver)
    );
    assert!(applied(&pen.applied()).contains("Tax Conferee"));
}

/// PIT-UND Part 2, open to a filer who made no payments or four equal ones
/// on time:
///
/// ```text
/// no payments, return paid March 1, 2027: 36,000 × 12% = $4,320.00, less
/// 60 days × 0.0005 × 36,000 = $1,080.00, is $3,240.00
/// four $5,000 payments on time, to April 30, 2027: 16,000 × 12% = $1,920.00
/// ```
///
/// The second household's regular figure is 4,000 × (0.1820 + 0.1595 +
/// 0.1135 + 0.0525) = $2,030.00, so the Short Method is the cheaper.
#[test]
fn estimated_short_method() {
    let h = Household {
        return_filed: day(2027, 3, 1),
        ..tax(DOLLAR * 40_000)
    };
    let res = penalty(&h, day(2027, 4, 30));
    let a = applied(&res.applied());
    assert!(
        a.contains("less $1080.00 for 60 days before April 30, 2027, is $3240.00"),
        "{a}"
    );
    let h = Household {
        payments: quarterly(&[DOLLAR * 5_000; 4]),
        ..tax(DOLLAR * 40_000)
    };
    let res = penalty(&h, day(2027, 4, 30));
    assert_eq!(res.value().total(), USCents(203_000));
    let a = applied(&res.applied());
    assert!(
        a.contains("12% of the $16000.00 underpayment is $1920.00, less $0.00 for 0 days"),
        "{a}"
    );
    // One payment a day late, or unequal payments, close it.
    let d = dues();
    for (name, ps) in [
        (
            "late",
            vec![
                pay(day(2026, 5, 1), DOLLAR * 5_000),
                pay(d[1], DOLLAR * 5_000),
                pay(d[2], DOLLAR * 5_000),
                pay(d[3], DOLLAR * 5_000),
            ],
        ),
        (
            "unequal",
            quarterly(&[
                DOLLAR * 5_000,
                DOLLAR * 5_000,
                DOLLAR * 5_000,
                DOLLAR * 6_000,
            ]),
        ),
        ("three", quarterly(&[DOLLAR * 5_000; 3])),
    ] {
        let h = Household {
            payments: ps,
            ..tax(DOLLAR * 40_000)
        };
        let res = penalty(&h, day(2027, 4, 30));
        assert!(
            !applied(&res.applied()).contains("Short Method instead"),
            "{name}: the Short Method is offered"
        );
    }
}

/// PIT-EST item 7: a requirement first arising after April 1 is declared by
/// June 15, after June 1 by September 15, after September 1 by January 15.
/// PIT-UND still charges a quarter on each date, so the schedule is
/// unchanged and the rule is reported.
#[test]
fn estimated_late_start() {
    for (met, want) in [
        (day(2026, 4, 1), ""),
        (day(2026, 4, 2), "declare by June 15, 2026"),
        (day(2026, 6, 1), "declare by June 15, 2026"),
        (day(2026, 6, 2), "declare by September 15, 2026"),
        (day(2026, 9, 1), "declare by September 15, 2026"),
        (day(2026, 9, 2), "declare by January 15, 2027"),
    ] {
        let h = Household {
            first_met: met,
            ..tax(DOLLAR * 40_000)
        };
        let res = rules().schedule(&h.facts()).unwrap();
        let a = applied(&res.applied());
        assert!(
            a.contains("PIT-EST (2026), item 7") == !want.is_empty() && a.contains(want),
            "first met {met}: want {want:?} in {a}"
        );
        assert_eq!(res.value()[0].needed_by(), DOLLAR * 9_000);
    }
}

/// The two open questions about a single day. Line 39 counts the first
/// installment from May 1, so $9,000 paid on May 1 costs nothing and on May
/// 2 one day, 9,000 × 0.0005 = $4.50. The instructions count fourth-period
/// payments through January 16 where the form heads its column January 15;
/// the form is followed, so $9,000 paid on January 16, 2027 is one day late,
/// $4.50, and the question is asked.
#[test]
fn estimated_open_questions_on_the_first_and_last_days() {
    for (on, want) in [(day(2026, 5, 1), 0), (day(2026, 5, 2), 450)] {
        let h = Household {
            payments: vec![pay(on, DOLLAR * 9_000)],
            ..tax(DOLLAR * 40_000)
        };
        let res = penalty(&h, day(2026, 6, 1));
        assert_eq!(res.value().total(), USCents(want), "paid {on}");
        assert_eq!(
            because(&res.required(), PositionKind::UnsettledRule).contains("from May 1"),
            want > 0,
            "paid {on}"
        );
    }
    let mut ps = quarterly(&[DOLLAR * 9_000; 3]);
    ps.push(pay(day(2027, 1, 16), DOLLAR * 9_000));
    let h = Household {
        payments: ps,
        ..tax(DOLLAR * 40_000)
    };
    let res = penalty(&h, day(2027, 4, 30));
    assert_eq!(res.value().total(), USCents(450));
    assert!(because(&res.required(), PositionKind::UnsettledRule).contains("Jan 16, 2027 payment"));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 40_000).facts()).unwrap();
    let want = [
        (
            "The penalty by the day or by the month",
            [
                "0.05% for each day",
                "lines 40 to 42",
                "1.5% a month, or part of a month",
                "§ 535(b)",
            ],
        ),
        (
            "The day the first installment's penalty starts",
            [
                "from May 1",
                "lines 39 and 40",
                "Apr 30 for the first",
                "§§ 535(b), 1170(a)",
            ],
        ),
        (
            "The $800 test",
            [
                "required payment",
                "line E",
                "this year's tax less withholding",
                "§§ 1169(a), (b), 535(b)",
            ],
        ),
        (
            "Part-year residents",
            [
                "only part of the year",
                "Exceptions to the Penalty",
                "no exception for a part-year resident",
                "§§ 1169(a), 535(b)",
            ],
        ),
        (
            "A requirement first met after Apr 1",
            [
                "whenever the requirement arose",
                "line 23",
                "equal installments on the remaining dates",
                "§ 1170(a)",
            ],
        ),
        (
            "The optional Short Method",
            [
                "12% of the year's total underpayment",
                "Part 2",
                "each installment's underpayment",
                "§ 535(b)",
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
        assert_eq!(d.uses(), Reading::Form, "{:?}", d.rule());
        // Nothing is paid, so every installment is short and the Short
        // Method is open; the household is a full-year resident who started
        // on time, and this year's tax less withholding is over $800 on
        // either reading.
        let untouched = [
            "Part-year residents",
            "A requirement first met after Apr 1",
            "The $800 test",
        ]
        .contains(&d.rule().as_str());
        assert_eq!(d.affects_household(), !untouched, "{:?}", d.rule());
    }
    // The figure follows the form on each. By the statute, $9,000 one day
    // late costs a full month, 9,000 × 1.5% = $135.00, from April 30; by the
    // form it costs nothing on May 1 and $4.50 on May 2.
    let h = Household {
        payments: vec![pay(day(2026, 5, 2), DOLLAR * 9_000)],
        ..tax(DOLLAR * 40_000)
    };
    assert_eq!(penalty(&h, day(2026, 6, 1)).value().total(), USCents(450));
    let statute = estimated::monthly(
        DOLLAR * 9_000,
        day(2026, 4, 30),
        day(2026, 5, 2),
        Rate(150),
        "30 Del. C. § 535(b)",
    )
    .unwrap();
    assert!(statute.months() == 1 && statute.charge() == USCents(13_500));
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for y in [2025, 2027] {
        let err = estimated_tax(y).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{y}: {err}");
    }
    let this = estimated::new_figures()
        .tax(DOLLAR * 40_000)
        .build()
        .unwrap();
    let base = || -> FactsBuilder {
        let mut b = estimated::new_facts(DELAWARE, 2026);
        b.filing_status(Status::Single).this_year(this);
        b
    };
    for (name, f) in [
        ("a farmer", base().farm_share(Rate(7000)).build().unwrap()),
        (
            "a fisherman",
            base().fish_share(Rate(7000)).build().unwrap(),
        ),
        (
            "farming and fishing",
            base()
                .farm_share(Rate(3400))
                .fish_share(Rate(3300))
                .build()
                .unwrap(),
        ),
    ] {
        let err = rules().requirement(&f).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    assert!(
        rules()
            .requirement(&base().farm_share(Rate(6666)).build().unwrap())
            .is_ok()
    );
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
            estimated::new_facts(DELAWARE, 2027)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        ("extras", base().state("not Delaware's").build().unwrap()),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
}

#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    let all = format!(
        "{} {} {}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$800",
        "90%",
        "$150,000",
        "April 30, not April 15",
        "1.5% a month",
        "0.05% a day",
        "period it was taken out",
        "22.5%, 45%, 67.5% and 90%",
        "Short Method",
        "12%",
        "Tax Conferee",
        "Form 2210",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.versus_federal().is_empty() && !e.sources().is_empty());
}

/// Without this year's tax the target is last year's tax alone, and the
/// penalty against it is an upper bound. The $800 test is on the required
/// payment, which can only be lower than last year's figure, so an
/// exemption on last year's figure is exact.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000, DOLLAR * 100_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(
        r.required() && r.target() == DOLLAR * 8_000 && !r.exempt(),
        "{}",
        r.why()
    );
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearTax);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [200_000, 400_000, 600_000, 800_000].into_iter().enumerate() {
        assert_eq!(
            sched.value()[i].needed_by(),
            USCents(want),
            "installment {}",
            i + 1
        );
    }
    let pen = penalty(&h, day(2027, 4, 30));
    assert!(pen.value().upper_bound() && pen.value().total().0 != 0);
    let paid = Household {
        payments: quarterly(&[DOLLAR * 2_000; 4]),
        ..h
    };
    assert_eq!(penalty(&paid, day(2027, 4, 30)).value().total(), USCents(0));
    let small = Household {
        no_tax: true,
        prior: prior(DOLLAR * 800, DOLLAR * 50_000),
        ..Household::default()
    };
    let res = rules().requirement(&small.facts()).unwrap();
    assert!(res.value().exempt(), "{}", res.value().why());
    let pen = penalty(&small, day(2027, 4, 30));
    assert!(pen.value().total() == USCents(0) && !pen.value().upper_bound());
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let res = rules().requirement(&none.facts()).unwrap();
    assert!(!res.value().target_known() && !res.value().required());
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

/// An exception sets the target, so no question may say its answer changes
/// the target. $500 of tax last year, with this year's not given.
#[test]
fn estimated_exception_asks_nothing_of_the_target() {
    let f = estimated::new_facts(DELAWARE, 2026)
        .filing_status(Status::Single)
        .this_year(estimated::new_figures().build().unwrap())
        .prior_year(
            estimated::new_prior_return()
                .filed(true)
                .figures(estimated::new_figures().tax(DOLLAR * 500).build().unwrap())
                .full_year_resident(true)
                .build()
                .unwrap(),
        )
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
fn waiver_penalty(pos: Positions) -> working::Result<Penalty> {
    let f = estimated::new_facts(DELAWARE, 2026)
        .filing_status(Status::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .positions(pos)
        .build()
        .unwrap();
    rules().penalty(&f, day(2027, 4, 15)).unwrap()
}

fn waiver_for(c: WaiverCause, on: Date, born: Date) -> Positions {
    estimated::new_positions()
        .waiver_for(c, on)
        .born(born)
        .build()
        .unwrap()
}

/// PIT-UND lists retirement after 62 or disability in the year or the year
/// before, and a casualty, disaster or other unusual circumstance. Unlike
/// the federal pair, neither names reasonable cause or a penalty that would
/// be inequitable. The penalty stays the figure before any waiver, and
/// `if_waived` shows $0 only when the waiver is open to the facts.
#[test]
fn estimated_waiver_outcomes() {
    let before = waiver_penalty(
        estimated::new_positions()
            .waiver(WaiverKind::None)
            .build()
            .unwrap(),
    )
    .value()
    .total();
    assert_ne!(
        before,
        USCents(0),
        "no penalty arises, so no waiver is decided"
    );
    for (name, pos, want, reason) in [
        (
            "retired in 2026 at 64",
            waiver_for(WaiverCause::Retirement, day(2026, 6, 1), day(1962, 3, 1)),
            "eligible",
            "retired after reaching 62 in 2026",
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
            "disabled in 2025",
            waiver_for(WaiverCause::Disability, day(2025, 2, 1), Date::default()),
            "eligible",
            "became disabled in 2025",
        ),
        (
            "a disaster",
            waiver_for(WaiverCause::Disaster, day(2026, 8, 1), Date::default()),
            "eligible",
            "due to a disaster",
        ),
    ] {
        let p = waiver_penalty(pos).value();
        let o = p.waiver_outcome().unwrap();
        let (got, if_waived) = if o.eligible() {
            ("eligible", USCents(0))
        } else if o.refused() {
            ("refused", p.total())
        } else {
            ("undecided", p.total())
        };
        assert!(
            got == want && o.reason().contains(reason),
            "{name}: {got}, {:?}",
            o.reason()
        );
        assert!(p.total() == before && p.if_waived() == if_waived, "{name}");
    }
    // The difference from federal: no reasonable-cause or unfairness test.
    for pos in [
        waiver_for(WaiverCause::Retirement, day(2026, 6, 1), day(1962, 3, 1)),
        waiver_for(WaiverCause::Casualty, day(2026, 8, 1), Date::default()),
    ] {
        let o = waiver_penalty(pos).value().waiver_outcome().unwrap();
        assert!(
            !o.reason().contains("reasonable cause") && !o.reason().contains("unfair"),
            "{:?}",
            o.reason()
        );
    }
}

/// Withholding over the tax reads as a refund, never as a minus amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let rules = rules();
    let mut asked = false;
    for a in estimatedtest::ask(&rules, &estimatedtest::households(&estimated::DELAWARE)) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked = true;
        let why = a.requirement().unwrap().value().why();
        assert!(
            why.contains(
                "Less $3,600 of withholding, that leaves a $900 refund. No estimated payments are needed."
            ),
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
