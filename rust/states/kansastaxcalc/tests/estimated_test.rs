//! Kansas's estimated tax rules for 2026, ported from the Go package's
//! estimated_test.go. (Go also compares every figure and the composite
//! listing against their extracts in testdata/; those checks stay in Go.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Penalty, Positions, PriorReturn, Status,
    WaiverCause, WaiverKind, Weekday,
};
use money::{DOLLAR, Rate, USCents, dollars, percent};
use taxcalcs_kansastaxcalc::{
    EstimatedFacts, EstimatedFactsBuilder, estimated_tax, new_estimated_facts,
};
use working::{PositionKind, Reading, Required};

const KS: estimated::Jurisdiction = estimated::KANSAS;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
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

/// One payment of each amount on each due date.
fn quarterly(a: &[USCents]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .map(|(i, x)| pay(dues()[i], *x))
        .collect()
}

#[derive(Default, Clone)]
struct Household {
    /// Form K-40 line 18.
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    /// The annualised tax for each period.
    annualised: Vec<USCents>,
    as_withheld: bool,
    waiver: Option<WaiverKind>,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

fn ten() -> Household {
    tax(DOLLAR * 10_000)
}

impl Household {
    fn facts(&self) -> Facts {
        let figures = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(KS, 2026);
        b.filing_status(filing::Status::Single)
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
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        if let Some(e) = self.extras {
            b.state(e);
        }
        let mut eb = estimated::new_elections();
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

/// A filed return covering `months` months with its tax.
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

fn not_filed() -> Option<PriorReturn> {
    Some(estimated::new_prior_return().filed(false).build().unwrap())
}

fn not_required() -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .not_required_to_file()
            .build()
            .unwrap(),
    )
}

fn extras_of(b: &mut EstimatedFactsBuilder) -> Option<EstimatedFacts> {
    Some(b.build().unwrap())
}

fn penalty(h: &Household, as_of: Date) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), as_of).unwrap()
}

/// The 2026 dates are the ones the K-40ES prints, and none moves.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&ten().facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
}

/// $500 or more of tax after withholding and credits brings the rules in;
/// $499.99 does not.
#[test]
fn estimated_threshold_boundary() {
    for (name, withheld, extras, exempt) in [
        ("$499.99 after withholding", 950_001, None, true),
        ("$500.00 after withholding", 950_000, None, false),
        (
            "$499.99 after refundable credits",
            900_000,
            extras_of(new_estimated_facts().refundable_credits(USCents(50_001))),
            true,
        ),
        (
            "$500.00 after refundable credits",
            900_000,
            extras_of(new_estimated_facts().refundable_credits(USCents(50_000))),
            false,
        ),
        (
            "$499.99 after the K-120S credit",
            900_000,
            extras_of(new_estimated_facts().pass_through_credit(USCents(50_001))),
            true,
        ),
    ] {
        let h = Household {
            withheld: USCents(withheld),
            extras,
            ..ten()
        };
        let res = rules().requirement(&h.facts()).unwrap();
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
            assert_eq!(
                c.label(),
                "Tax less withholding and credits under $500",
                "{name}"
            );
        }
    }
}

/// The target is the smaller of 90% of this year's tax and 100% of last
/// year's, with no 110% tier.
#[test]
fn estimated_target_options() {
    let u = PositionKind::Unspecified;
    for (name, p, extras, target, chosen, ask) in [
        (
            "last year is less",
            prior(DOLLAR * 8_000, 12),
            None,
            DOLLAR * 8_000,
            Basis::PriorYearTax,
            u,
        ),
        (
            "this year is less",
            prior(DOLLAR * 9_500, 12),
            None,
            DOLLAR * 9_000,
            Basis::ThisYearTax,
            u,
        ),
        (
            "a cent apart",
            prior(USCents(899_999), 12),
            None,
            USCents(899_999),
            Basis::PriorYearTax,
            u,
        ),
        (
            "no return on record",
            None,
            None,
            DOLLAR * 9_000,
            Basis::ThisYearTax,
            PositionKind::PriorYearReturn,
        ),
        (
            "a short year, not answered",
            prior(DOLLAR * 5_000, 6),
            None,
            DOLLAR * 9_000,
            Basis::ThisYearTax,
            PositionKind::UnsettledRule,
        ),
        (
            "a short year, not used",
            prior(DOLLAR * 5_000, 6),
            extras_of(new_estimated_facts().short_prior_year(false)),
            DOLLAR * 9_000,
            Basis::ThisYearTax,
            u,
        ),
        (
            "a short year, used",
            prior(DOLLAR * 5_000, 6),
            extras_of(new_estimated_facts().short_prior_year(true)),
            DOLLAR * 5_000,
            Basis::PriorYearTax,
            u,
        ),
    ] {
        let h = Household {
            prior: p,
            extras,
            ..ten()
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let r = res.value();
        assert_eq!(r.target(), target, "{name}");
        assert_eq!(r.chosen().unwrap_or_default().basis(), chosen, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn),
            ask == PositionKind::PriorYearReturn,
            "{name}"
        );
        assert_eq!(
            has(&res.required(), PositionKind::UnsettledRule),
            ask == PositionKind::UnsettledRule,
            "{name}"
        );
        assert_eq!(r.options()[1].percent(), percent(100), "{name}");
    }
    let h = Household {
        prior: prior(DOLLAR * 5_000, 6),
        ..ten()
    };
    let short = rules().requirement(&h.facts()).unwrap();
    let o = short.value().options()[1].clone();
    assert!(!o.available() && o.why_not().contains("12-month"));
    assert!(short.value().required() && short.value().why().contains("$9,000"));
}

/// Schedule K-210 takes last year's figure as zero when no return was filed
/// or it showed no tax, whatever the months, so no penalty arises. A small
/// liability is used as it is.
#[test]
fn estimated_no_prior_year_liability() {
    for (name, p) in [
        ("not filed", not_filed()),
        ("not required", not_required()),
        ("no tax", prior(USCents(0), 12)),
        ("no tax, 6 months", prior(USCents(0), 6)),
    ] {
        let f = Household { prior: p, ..ten() }.facts();
        let res = rules().requirement(&f).unwrap();
        assert!(res.value().exempt() && !res.value().required(), "{name}");
        let label = if p.unwrap().filed() {
            "No Kansas tax last year"
        } else {
            "No Kansas return filed last year"
        };
        let c = res.value().chosen().unwrap();
        assert_eq!(c.basis(), Basis::Exception, "{name}");
        assert_eq!(c.label(), label, "{name}");
        assert_eq!(c.amount(), USCents(0), "{name}");
        let pen = rules().penalty(&f, day(2027, 4, 15)).unwrap();
        assert_eq!(pen.value().total(), USCents(0), "{name}");
        assert!(pen.value().lines().is_empty(), "{name}");
        assert_eq!(pen.value().reason(), format!("{label}."), "{name}");
    }
    // $150 of tax last year: the form uses it; the statute would use zero.
    let h = Household {
        prior: prior(DOLLAR * 150, 12),
        ..ten()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().exempt());
    assert_eq!(res.value().target(), DOLLAR * 150);
}

/// Nothing paid on a $4,000 target: $1,000 short for 61 days at 8% is
/// $13.37, $2,000 for 92 days $40.33, and $3,000 for 107 days $70.36.
#[test]
fn estimated_penalty_columns_match_k210_days() {
    let h = Household {
        prior: prior(DOLLAR * 4_000, 12),
        ..ten()
    };
    let res = penalty(&h, day(2027, 4, 15));
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    let want_short = [1_000, 2_000, 3_000, 4_000];
    let want_days = [61, 92, 107 + 15, 90];
    for (i, l) in lines.iter().enumerate() {
        assert_eq!(l.shortfall(), DOLLAR * want_short[i], "column {}", i + 1);
        assert_eq!(l.days(), want_days[i], "column {}", i + 1);
    }
    for (i, want) in [1_337, 4_033, 7_036].into_iter().enumerate() {
        let parts = lines[i].parts();
        assert_eq!(parts.len(), 1, "column {}", i + 1);
        assert_eq!(parts[0].charge(), USCents(want), "column {}", i + 1);
        assert_eq!(parts[0].rate(), percent(8), "column {}", i + 1);
    }
    assert_eq!(res.value().total(), USCents(12_406));
    assert!(has(&res.required(), PositionKind::UnpublishedRate));
}

/// The third column runs across the period boundary at December 31: its 107
/// days of 2026 are charged at 8%, and its 15 days of 2027 are reported as
/// having no published rate.
#[test]
fn estimated_penalty_across_the_rate_period_boundary() {
    let h = Household {
        prior: prior(DOLLAR * 4_000, 12),
        payments: quarterly(&[DOLLAR * 1_000, DOLLAR * 1_000]),
        ..ten()
    };
    for (as_of, days, unrated) in [
        (day(2026, 12, 31), 107, false),
        (day(2027, 1, 15), 122, true),
    ] {
        let res = penalty(&h, as_of);
        let lines = res.value().lines();
        assert_eq!(lines.len(), 1, "to {as_of}");
        assert_eq!(lines[0].installment(), 3, "to {as_of}");
        assert_eq!(lines[0].days(), days, "to {as_of}");
        let parts = lines[0].parts();
        assert_eq!(parts.len(), 1, "to {as_of}");
        assert_eq!(parts[0].days(), 107);
        assert_eq!(parts[0].to(), day(2026, 12, 31));
        assert_eq!(parts[0].charge(), USCents(2_345));
        assert_eq!(has(&res.required(), PositionKind::UnpublishedRate), unrated);
    }
}

/// A late payment ends a column's charge on the day it is paid. A part
/// payment lowers the charge from that day.
#[test]
fn estimated_late_payment_shortens_the_column() {
    let thousand = DOLLAR * 1_000;
    let d = dues();
    let late = Household {
        prior: prior(DOLLAR * 4_000, 12),
        payments: vec![
            pay(d[0], thousand),
            pay(day(2026, 6, 28), thousand),
            pay(d[2], thousand),
            pay(d[3], thousand),
        ],
        ..ten()
    };
    let res = penalty(&late, day(2027, 4, 15));
    let lines = res.value().lines();
    // $1,000 × 13 × 8% ÷ 365 is $2.85.
    assert_eq!(lines.len(), 1);
    assert_eq!(lines[0].installment(), 2);
    assert_eq!(lines[0].days(), 13);
    assert_eq!(res.value().total(), USCents(285));
    let sched = rules().schedule(&late.facts()).unwrap().value();
    assert_eq!(sched[1].status(), Status::Short);
    assert_eq!(sched[1].shortfall(), thousand);
    assert_eq!(sched[2].status(), Status::Met);

    let part = Household {
        payments: vec![
            pay(d[0], thousand),
            pay(day(2026, 6, 28), DOLLAR * 400),
            pay(day(2026, 7, 15), DOLLAR * 600),
            pay(d[2], thousand),
            pay(d[3], thousand),
        ],
        ..late.clone()
    };
    let res = penalty(&part, day(2027, 4, 15));
    let parts = res.value().lines()[0].parts();
    // $2.85 for 13 days on $1,000, then $600 × 17 × 8% ÷ 365, $2.24.
    assert_eq!(parts.len(), 2);
    assert_eq!(parts[1].principal(), DOLLAR * 600);
    assert_eq!(parts[1].days(), 17);
    assert_eq!(res.value().total(), USCents(509));

    // June unpaid until September: the second column runs its whole 92
    // days, $20.16, and the third column is met.
    let catch_up = Household {
        payments: vec![
            pay(d[0], thousand),
            pay(d[2], thousand * 2),
            pay(d[3], thousand),
        ],
        ..late
    };
    let res = penalty(&catch_up, day(2027, 4, 15));
    let lines = res.value().lines();
    assert_eq!(lines.len(), 1);
    assert_eq!(lines[0].days(), 92);
    assert_eq!(res.value().total(), USCents(2_016));
}

/// Withholding and the K-120S credit count a quarter on each date, however
/// late they were paid. Last year's overpayment counts on April 15, which no
/// Kansas document states, so it is asked.
#[test]
fn estimated_credits_are_spread_evenly() {
    let h = Household {
        withheld: DOLLAR * 4_000,
        as_withheld: true,
        extras: extras_of(new_estimated_facts().pass_through_credit(DOLLAR * 400)),
        payments: vec![
            estimated::new_payment()
                .from_prior_year()
                .amount(DOLLAR * 500)
                .build()
                .unwrap(),
        ],
        ..ten()
    };
    let res = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [160_000, 270_000, 380_000, 490_000].into_iter().enumerate() {
        assert_eq!(
            res.value()[i].credited_by(),
            USCents(want),
            "installment {}",
            i + 1
        );
    }
    assert!(has(&res.required(), PositionKind::UnsettledRule));
}

/// Under the annualised method each column needs the lesser of the regular
/// amount and 22.5%, 45%, 67.5% or 90% of the period's annualised tax.
#[test]
fn estimated_annualised_takes_the_lesser_column() {
    let mut h = Household {
        prior: prior(DOLLAR * 4_000, 12),
        annualised: vec![
            DOLLAR * 2_000,
            DOLLAR * 3_000,
            DOLLAR * 4_000,
            DOLLAR * 10_000,
        ],
        payments: quarterly(&[DOLLAR * 450, DOLLAR * 900, DOLLAR * 1_350, DOLLAR * 1_300]),
        ..ten()
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [45_000, 135_000, 270_000, 400_000].into_iter().enumerate() {
        let inst = &sched.value()[i];
        assert_eq!(inst.needed_by(), USCents(want), "installment {}", i + 1);
        assert_eq!(inst.status(), Status::Met, "installment {}", i + 1);
    }
    assert_eq!(penalty(&h, day(2027, 4, 15)).value().total(), USCents(0));
    h.annualised = Vec::new();
    assert!(penalty(&h, day(2026, 12, 31)).value().total().0 != 0);
    let period = |m, d| {
        estimated::new_period()
            .end(day(2026, m, d))
            .build()
            .unwrap()
    };
    let bad = estimated::new_facts(KS, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .periods(&[period(3, 31), period(6, 30), period(8, 31), period(12, 31)])
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    assert!(rules().requirement(&bad).is_err());
}

/// Filing and paying by January 31 excuses the January installment. January
/// 31, 2027 is a Sunday; no Kansas document says whether it moves, so it is
/// not moved and a return filed on Monday, February 1 asks.
#[test]
fn estimated_file_and_pay_by_january_31() {
    let moves = extras_of(new_estimated_facts().january_thirty_first_moves(true));
    for (filed, extras, excused, asked) in [
        (day(2027, 1, 29), None, true, false),
        (day(2027, 1, 31), None, true, false),
        (day(2027, 2, 1), None, false, true),
        (day(2027, 2, 1), moves, true, false),
        (day(2027, 2, 2), moves, false, false),
    ] {
        let h = Household {
            return_filed: filed,
            extras,
            ..ten()
        };
        let res = rules().schedule(&h.facts()).unwrap();
        let jan = &res.value()[3];
        assert_eq!(jan.status() == Status::Excused, excused, "filed {filed}");
        if excused {
            assert_eq!(jan.needed_by(), res.value()[2].needed_by(), "filed {filed}");
        }
        assert_eq!(
            has(&res.required(), PositionKind::UnsettledRule),
            asked,
            "filed {filed}"
        );
    }
    let p = penalty(
        &Household {
            return_filed: day(2027, 3, 1),
            ..ten()
        },
        day(2027, 4, 15),
    );
    assert_eq!(p.value().as_of(), day(2027, 3, 1));
    // K.S.A. 79-32,107(a): never beyond the 15th day of the fourth month.
    let p = penalty(&ten(), day(2027, 6, 30));
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
}

/// A late starter is measured against a quarter by every date, as Schedule
/// K-210 prints, and asked. With the statute's installments taken, nothing
/// is needed before the group's first date and the rest is in equal parts.
#[test]
fn estimated_late_start() {
    let statute = extras_of(new_estimated_facts().late_start_installments(true));
    let mut h = Household {
        first_met: day(2026, 7, 1),
        payments: quarterly(&[USCents(0), USCents(0), DOLLAR * 4_500, DOLLAR * 4_500]),
        ..ten()
    };
    let res = rules().schedule(&h.facts()).unwrap();
    assert_eq!(res.value()[0].needed_by(), DOLLAR * 2_250);
    assert!(has(&res.required(), PositionKind::UnsettledRule));
    assert!(penalty(&h, day(2026, 12, 31)).value().total().0 != 0);
    h.extras = statute;
    let res = rules().schedule(&h.facts()).unwrap();
    assert!(!has(&res.required(), PositionKind::UnsettledRule));
    assert!(!has(&res.required(), PositionKind::RequirementFirstMet));
    assert_eq!(penalty(&h, day(2027, 4, 15)).value().total(), USCents(0));

    for (met, want) in [
        (day(2026, 4, 1), [225_000, 450_000, 675_000, 900_000]),
        (day(2026, 4, 2), [0, 300_000, 600_000, 900_000]),
        (day(2026, 6, 1), [0, 300_000, 600_000, 900_000]),
        (day(2026, 6, 2), [0, 0, 450_000, 900_000]),
        (day(2026, 9, 1), [0, 0, 450_000, 900_000]),
        (day(2026, 9, 2), [0, 0, 0, 900_000]),
    ] {
        let h = Household {
            first_met: met,
            extras: statute,
            ..ten()
        };
        let s = rules().schedule(&h.facts()).unwrap().value();
        for (i, want) in want.into_iter().enumerate() {
            assert_eq!(
                s[i].needed_by(),
                USCents(want),
                "first met {met}: installment {}",
                i + 1
            );
        }
    }
    // A third of last year's $100.00 rounds to $33.33 and two thirds to
    // $66.67.
    let h = Household {
        prior: prior(DOLLAR * 100, 12),
        first_met: day(2026, 5, 1),
        extras: statute,
        ..ten()
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[1].needed_by(), USCents(3_333));
    assert_eq!(s[2].needed_by(), USCents(6_667));
    assert_eq!(s[3].needed_by(), USCents(10_000));
}

/// The penalty is before any waiver, and the statute's $1 floor is reported
/// rather than applied.
#[test]
fn estimated_waiver_and_the_dollar_floor() {
    // April $40 short for 61 days: $0.53.
    let mut small = Household {
        prior: prior(DOLLAR * 4_000, 12),
        payments: quarterly(&[DOLLAR * 960, DOLLAR * 1_040, DOLLAR * 1_000, DOLLAR * 1_000]),
        ..ten()
    };
    let res = penalty(&small, day(2027, 4, 15));
    assert_eq!(res.value().total(), USCents(53));
    assert!(res.applied().iter().any(
        |r| r.cite() == "K.S.A. 79-32,107(a)" && r.applied().contains("does not exceed $1.00")
    ));
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));
    small.waiver = Some(WaiverKind::None);
    let res = penalty(&small, day(2027, 4, 15));
    assert!(!has(&res.required(), PositionKind::PenaltyWaiver));
    small.waiver = Some(WaiverKind::Casualty);
    let res = penalty(&small, day(2027, 4, 15));
    assert_eq!(res.value().total(), USCents(53));
    assert_eq!(res.value().waiver(), WaiverKind::Casualty);
}

/// Kansas publishes no casualty, disaster, retirement or disability waiver.
/// A waiver asked for is undecided, the open point is asked, and the penalty
/// is in full.
#[test]
fn estimated_waiver_outcomes() {
    let h = Household {
        prior: prior(DOLLAR * 4_000, 12),
        ..ten()
    };
    let base = penalty(&h, day(2027, 4, 15)).value().total();
    assert!(base.0 != 0);
    let cases: [(&str, Positions); 2] = [
        (
            "retired in 2026 at 64",
            estimated::new_positions()
                .waiver_for(WaiverCause::Retirement, day(2026, 6, 1))
                .born(day(1962, 3, 1))
                .build()
                .unwrap(),
        ),
        (
            "a disaster",
            estimated::new_positions()
                .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
                .build()
                .unwrap(),
        ),
    ];
    for (name, pos) in cases {
        let f = estimated::new_facts(KS, 2026)
            .filing_status(filing::Status::Single)
            .this_year(estimated::new_figures().tax(h.tax).build().unwrap())
            .prior_year(h.prior.unwrap())
            .elections(estimated::new_elections().build().unwrap())
            .positions(pos)
            .build()
            .unwrap();
        let res = rules().penalty(&f, day(2027, 4, 15)).unwrap();
        let p = res.value();
        let o = p.waiver_outcome().unwrap();
        let unsettled = res.required().iter().any(|q| {
            q.position() == PositionKind::UnsettledRule && q.effect().contains("reasonable cause")
        });
        assert_eq!(p.total(), base, "{name}");
        assert!(o.undecided(), "{name}");
        assert!(
            o.reason()
                .contains("no casualty, disaster, retirement or disability waiver"),
            "{name}"
        );
        assert_eq!(p.if_waived(), base, "{name}");
        assert!(unsettled, "{name}");
    }
}

/// Under an exception the target is $0, so neither return is asked for as a
/// way to change it. Without one, last year's return is.
#[test]
fn estimated_target_questions_only_when_the_target_is_open() {
    let under = rules().requirement(&tax(DOLLAR * 400).facts()).unwrap();
    assert!(under.value().exempt());
    assert!(!has(&under.required(), PositionKind::PriorYearReturn));
    let no_tax = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(USCents(0)).build().unwrap())
        .build()
        .unwrap();
    let h = Household {
        no_tax: true,
        prior: Some(no_tax),
        ..Household::default()
    };
    let none = rules().requirement(&h.facts()).unwrap();
    assert!(none.value().exempt());
    assert!(!has(&none.required(), PositionKind::ThisYearTax));
    let over = rules().requirement(&ten().facts()).unwrap();
    assert!(has(&over.required(), PositionKind::PriorYearReturn));
}

/// Each divergence is reported with both readings and both sources, follows
/// the form, and affects none of these facts.
#[test]
fn estimated_divergences_pin_both_readings() {
    let as_of = day(2027, 4, 15);
    let res = penalty(
        &Household {
            prior: prior(DOLLAR * 4_000, 12),
            ..ten()
        },
        as_of,
    );
    let want: [(&str, [&str; 4]); 5] = [
        (
            "No return filed last year",
            [
                "no Kansas return",
                "K-210",
                "only if you weren't required to file",
                "79-32,107(b)(1) and (b)(2)",
            ],
        ),
        (
            "Last year's tax under $200",
            [
                "however small",
                "line 3",
                "less than $200",
                "79-32,107(b)(2)",
            ],
        ),
        (
            "A return for less than 12 months",
            [
                "12-month return",
                "K-40ES",
                "no 12-month condition",
                "79-32,101(d)",
            ],
        ),
        (
            "Needing to pay only after April 1",
            [
                "25%, 50%, 75% and 100%",
                "worksheet line 9",
                "three equal payments",
                "79-32,103(a)(2) to (4)",
            ],
        ),
        (
            "A penalty of $1 or less",
            [
                "no minimum",
                "lines 17 and 18",
                "does not exceed $1",
                "79-32,107(a)",
            ],
        ),
    ];
    let ds = res.divergences();
    assert_eq!(ds.len(), want.len());
    for d in &ds {
        let Some((_, w)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(d.form().contains(w[0]), "{:?}", d.rule());
        assert!(d.form_source().contains(w[1]), "{:?}", d.rule());
        assert!(d.statute().contains(w[2]), "{:?}", d.rule());
        assert!(d.statute_source().contains(w[3]), "{:?}", d.rule());
        assert_eq!(d.uses(), Reading::Form, "{:?}", d.rule());
        assert!(!d.affects_household(), "{:?}", d.rule());
    }

    // Each one affects the facts it turns on, and the statute is used when
    // the position taken says so.
    let find = |h: &Household, rule: &str| {
        let res = penalty(h, as_of);
        let ds = res.divergences();
        let Some(d) = ds.iter().find(|d| d.rule() == rule) else {
            panic!("no divergence {rule:?}");
        };
        d.clone()
    };
    for (rule, h, uses) in [
        (
            "No return filed last year",
            Household {
                prior: not_filed(),
                ..ten()
            },
            Reading::Form,
        ),
        (
            "Last year's tax under $200",
            Household {
                prior: prior(DOLLAR * 150, 12),
                ..ten()
            },
            Reading::Form,
        ),
        (
            "A return for less than 12 months",
            Household {
                prior: prior(DOLLAR * 5_000, 6),
                extras: extras_of(new_estimated_facts().short_prior_year(true)),
                ..ten()
            },
            Reading::Statute,
        ),
        (
            "Needing to pay only after April 1",
            Household {
                first_met: day(2026, 7, 1),
                ..ten()
            },
            Reading::Form,
        ),
        (
            "A penalty of $1 or less",
            Household {
                prior: prior(DOLLAR * 4_000, 12),
                payments: quarterly(&[
                    DOLLAR * 960,
                    DOLLAR * 1_040,
                    DOLLAR * 1_000,
                    DOLLAR * 1_000,
                ]),
                ..ten()
            },
            Reading::Form,
        ),
    ] {
        let d = find(&h, rule);
        assert!(d.affects_household(), "{rule:?}");
        assert_eq!(d.uses(), uses, "{rule:?}");
    }
    let d = find(
        &Household {
            prior: not_required(),
            ..ten()
        },
        "No return filed last year",
    );
    assert!(!d.affects_household());
}

#[test]
fn estimated_refuses_what_is_not_built() {
    let err = estimated_tax(2025).err().unwrap();
    assert!(err.is(&estimated::Error::NoRules));
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let base = |j: estimated::Jurisdiction, y: i64| {
        let mut b = estimated::new_facts(j, y);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    for (name, f) in [
        (
            "a farmer",
            base(KS, 2026).farm_share(Rate(7000)).build().unwrap(),
        ),
        (
            "a fisher",
            base(KS, 2026).fish_share(Rate(7000)).build().unwrap(),
        ),
    ] {
        let err = rules().requirement(&f).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    for (name, f) in [
        (
            "California's facts",
            base(estimated::CALIFORNIA, 2026).build().unwrap(),
        ),
        ("2027", base(KS, 2027).build().unwrap()),
        (
            "other extras",
            base(KS, 2026).state("not Kansas's").build().unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        new_estimated_facts()
            .refundable_credits(USCents(-1))
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
        "$500",
        "90%",
        "100%",
        "no 110% tier",
        "as zero",
        "8% for 2026",
        "January 31",
        "Schedule K-210",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
}

/// Without this year's tax the target is last year's tax alone, the penalty
/// is an upper bound, and with no prior-year figure there is no target. No
/// liability last year still exempts, exactly.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 4_000, 12),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 4_000 && r.target_known());
    let o = r.options()[0].clone();
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [100_000, 200_000, 300_000, 400_000].into_iter().enumerate() {
        assert_eq!(sched.value()[i].needed_by(), USCents(want));
    }
    let pen = penalty(&h, day(2026, 12, 31));
    assert_eq!(pen.value().total(), USCents(12_406));
    assert!(pen.value().upper_bound());

    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let res = rules().requirement(&none.facts()).unwrap();
    assert!(!res.value().target_known(), "{}", res.value().why());
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
    let err = rules()
        .penalty(&none.facts(), day(2027, 4, 15))
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");

    let exempt = Household {
        no_tax: true,
        prior: not_filed(),
        ..Household::default()
    };
    let p = penalty(&exempt, day(2027, 4, 15));
    assert_eq!(p.value().total(), USCents(0));
    assert!(!p.value().upper_bound());
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
    let f = estimated::new_facts(KS, 2026)
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
