//! Ported from vermonttaxcalc/estimated_test.go: Vermont's estimated tax
//! rules for 2026, checked against 32 V.S.A. and the research's quotes from
//! the Department of Taxes' documents.
//!
//! Vermont publishes no exhaustive oracle for estimated tax, and IN-152
//! carries no worked example. The strongest checks are the four examples on
//! the Department's Calculate Interest and Penalties page, three of which
//! state their answer. The interest and penalty arithmetic, the payment
//! order, the exceptions and the boundaries are hand-computed from IN-152's
//! lines. The Go checks that compare each figure and quote against the
//! extracts in testdata/ are not ported: they check the Go package's
//! citations, not the rules.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, PartKind, Payment, Positions,
    PriorReturn, Status as InstallmentStatus, WaiverCause, WaiverKind, Weekday,
};
use filing::Status;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_vermonttaxcalc::{EstimatedFacts, calendar, estimated_tax, new_estimated_facts};
use working::{PositionKind, Reading, Required};

const VT: Jurisdiction = Jurisdiction::from_static("VT");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn asked(rs: &[Required], k: PositionKind, about: &str) -> bool {
    rs.iter()
        .any(|r| r.position() == k && (r.question().contains(about) || r.effect().contains(about)))
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

/// One payment of `a` on each due date.
fn quarterly(a: &[USCents]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .map(|(i, &x)| pay(dues()[i], x))
        .collect()
}

#[derive(Clone, Default)]
struct Household {
    /// IN-111 line 20 minus line 26c.
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    return_filed: Date,
    farm: Rate,
    fish: Rate,
    annualised: bool,
    as_withheld: bool,
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
        let this = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(VT, 2026);
        b.filing_status(Status::Single)
            .this_year(this)
            .paid(&self.payments)
            .return_filed(self.return_filed)
            .farm_share(self.farm)
            .fish_share(self.fish);
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
        match &self.extras {
            Some(e) => b.state(e.clone()),
            None => b.state(
                new_estimated_facts()
                    .prior_year_farmer_or_fisher(false)
                    .build()
                    .unwrap(),
            ),
        };
        let mut el = estimated::new_elections();
        if self.annualised {
            el.annualised();
            let periods: Vec<_> = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ]
            .iter()
            .map(|&end| estimated::new_period().end(end).build().unwrap())
            .collect();
            b.periods(&periods);
        }
        if self.as_withheld {
            el.withholding_as_withheld();
        }
        b.elections(el.build().unwrap());
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed twelve-month return with its tax minus credits.
fn prior(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .build()
            .unwrap(),
    )
}

/// A household whose 90% is $4,000.00, a quarter of which is $1,000: 90% of
/// $4,444.44 is $3,999.996, which rounds to $4,000.00.
fn four_thousand() -> Household {
    tax(USCents(444_444))
}

fn penalty(h: &Household, as_of: Date) -> working::Result<estimated::Penalty> {
    rules().penalty(&h.facts(), as_of).unwrap()
}

/// The 2026 dates are the ones IN-114 prints. Each is a weekday and none is
/// a Vermont state holiday, so none moves.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&tax(DOLLAR * 10_000).facts()).unwrap();
    let cal = calendar(2026).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i], "installment {}", i + 1);
        assert!(cal.is_business_day(inst.due()));
        assert_eq!(inst.needed_by(), DOLLAR * 2_250 * (i as i64 + 1));
    }
    // IN-152 line 7 stops on April 15, 2027, a Thursday.
    assert!(cal.is_business_day(day(2027, 4, 15)));
}

/// The calendar is the Department's State Holiday Closures list, and it
/// moves a date on a weekend or a Vermont holiday to the next business day.
/// The page itself is not checked in, so the holiday dates are not checked
/// against it.
#[test]
fn estimated_holiday_calendar_moves_dates() {
    assert_eq!(day(2026, 11, 28).weekday(), Weekday::SATURDAY);
    let cal = calendar(2026).unwrap();
    assert_eq!(cal.holidays().len(), 23);
    for (from, want, why) in [
        (
            day(2027, 1, 16),
            day(2027, 1, 19),
            "a Saturday before Martin Luther King, Jr. Day",
        ),
        (day(2027, 3, 2), day(2027, 3, 3), "Town Meeting Day"),
        (
            day(2026, 11, 26),
            day(2026, 11, 26),
            "Thanksgiving 2026, which the calendar does not carry",
        ),
        (
            day(2027, 11, 25),
            day(2027, 11, 29),
            "Thanksgiving and the day after, then a weekend",
        ),
        (day(2026, 4, 15), day(2026, 4, 15), "a Wednesday"),
    ] {
        assert_eq!(cal.next_business_day(from), want, "{from} ({why})");
    }
}

/// The $500 threshold, one cent either side. $500.00 exactly is charged, as
/// the instructions' "less than $500" reads, and the question is reported.
#[test]
fn estimated_threshold_boundaries() {
    // $499.99 left, $500.00, $500.01.
    for (withheld, exempt, want_asked) in [
        (50_001, true, false),
        (50_000, false, true),
        (49_999, false, false),
    ] {
        let f = Household {
            withheld: USCents(withheld),
            ..tax(DOLLAR * 1_000)
        }
        .facts();
        let res = rules().requirement(&f).unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "withheld {withheld}: {}",
            res.value().why()
        );
        if exempt {
            let c = res.value().chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount() == USCents(0)
                    && c.label() == "Tax less withholding under $500"
            );
        }
        for d in res.divergences() {
            if d.rule() == "The $500 threshold" {
                assert_eq!(d.affects_household(), exempt, "withheld {withheld}");
            }
        }
        assert_eq!(
            asked(&res.required(), PositionKind::UnsettledRule, "exactly $500"),
            want_asked,
            "withheld {withheld}"
        );
        let p = rules().penalty(&f, day(2027, 4, 15)).unwrap();
        assert_eq!(
            p.value().total() == USCents(0),
            exempt,
            "withheld {withheld}"
        );
    }
}

/// The target is the lesser of 90% of this year's tax and 100% of last
/// year's, one cent either side of where they meet.
#[test]
fn estimated_target_is_the_lesser_option() {
    for (p, chosen, target) in [
        (899_999, Basis::PriorYearTax, 899_999),
        (900_000, Basis::ThisYearTax, 900_000),
        (900_001, Basis::ThisYearTax, 900_000),
    ] {
        let h = Household {
            prior: prior(USCents(p)),
            ..tax(DOLLAR * 10_000)
        };
        let r = rules().requirement(&h.facts()).unwrap().value();
        assert_eq!(r.chosen().unwrap().basis(), chosen, "last year {p}");
        assert_eq!(r.target(), USCents(target), "last year {p}");
        let opts = r.options();
        assert!(
            opts[0].percent() == percent(90)
                && opts[1].percent() == percent(100)
                && opts[1].amount() == USCents(p)
        );
    }
}

/// Last year's option needs a filed twelve-month return. A filed return
/// with no tax makes the target zero.
#[test]
fn estimated_prior_year_option_availability() {
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .build()
        .unwrap();
    for (name, p, ask) in [
        ("no return on record", None, true),
        ("not filed", Some(not_filed), false),
        ("not required", Some(not_required), false),
        ("six months", Some(short), false),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let o = &res.value().options()[1];
        assert!(!o.available() && !o.why_not().is_empty(), "{name}");
        assert_eq!(
            asked(&res.required(), PositionKind::PriorYearReturn, "2025"),
            ask,
            "{name}"
        );
    }
    let f = Household {
        prior: prior(USCents(0)),
        ..tax(DOLLAR * 10_000)
    }
    .facts();
    let r = rules().requirement(&f).unwrap().value();
    assert!(!r.required() && r.target() == USCents(0) && !r.exempt());
    assert_eq!(
        rules()
            .penalty(&f, day(2027, 4, 15))
            .unwrap()
            .value()
            .total(),
        USCents(0)
    );
}

/// Without this year's tax the target is last year's, and the penalty is an
/// upper bound. Without last year's either there is no target.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 8_000, "{}", r.why());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(asked(
        &res.required(),
        PositionKind::ThisYearTax,
        "the most it can be"
    ));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, inst) in sched.value().iter().enumerate() {
        assert_eq!(inst.needed_by(), DOLLAR * 2_000 * (i as i64 + 1));
    }
    let p = penalty(&h, day(2027, 4, 15)).value();
    assert!(p.total().0 != 0 && (p.upper_bound() || !p.rated_through().is_zero()));
    let paid = Household {
        payments: quarterly(&[DOLLAR * 2_000; 4]),
        ..h
    };
    let p = penalty(&paid, day(2027, 4, 15)).value();
    assert!(p.total() == USCents(0) && !p.upper_bound());
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
        .penalty(&none.facts(), day(2027, 4, 15))
        .err()
        .unwrap();
    assert!(
        err.is(&estimated::Error::NeedsThisYearTax),
        "penalty: {err}"
    );
}

/// IN-152 lines 8a to 13 on $1,000 short each quarter, figured to December
/// 31, 2026. Interest is $1,000 × days ÷ 365 × 7.75%; the penalty is 1% for
/// each month or part of one.
#[test]
fn estimated_interest_and_penalty_arithmetic() {
    let p = penalty(&four_thousand(), day(2026, 12, 31)).value();
    // $55.205..., and April 15 to December 31 is 8 months and part of a 9th;
    // $42.253...; $22.719....
    let want = [(260, 5_521, 9), (199, 4_225, 7), (107, 2_272, 4)];
    let lines = p.lines();
    assert_eq!(
        lines.len(),
        want.len(),
        "the January installment is not yet due"
    );
    for (i, (days, interest, months)) in want.into_iter().enumerate() {
        let parts = lines[i].parts();
        assert!(lines[i].shortfall() == DOLLAR * 1_000 && parts.len() == 2);
        let (int, pen) = (&parts[0], &parts[1]);
        assert!(
            int.kind() == PartKind::Interest
                && int.days() == days
                && int.rate() == Rate(775)
                && int.charge() == USCents(interest),
            "line {} interest",
            i + 1
        );
        assert!(
            pen.kind() == PartKind::Monthly
                && pen.months() == months
                && pen.charge() == DOLLAR * 10 * months,
            "line {} penalty",
            i + 1
        );
    }
    assert_eq!(p.total(), USCents(32_018));
}

/// A shortfall that runs past January 1, 2027 spans two rate periods.
/// IN-152 counts the days to January 1 at the due year's rate and the rest
/// at the next year's. The 2027 rate is not published, so those days are
/// reported rather than charged, and the 1% a month is charged in full.
#[test]
fn estimated_shortfall_spanning_two_rate_periods() {
    let res = penalty(&four_thousand(), day(2027, 4, 15));
    let p = res.value();
    // April 15 to January 1 inclusive, IN-152 line 8a; the January
    // installment's days are all in 2027.
    let want = [
        (261, 5_542, 12),
        (200, 4_247, 10),
        (108, 2_293, 7),
        (0, 0, 3),
    ];
    let lines = p.lines();
    assert_eq!(lines.len(), 4);
    for (i, (want_days, want_interest, want_months)) in want.into_iter().enumerate() {
        let (mut interest, mut days, mut months) = (USCents(0), 0, 0);
        for part in lines[i].parts() {
            match part.kind() {
                PartKind::Interest => {
                    interest += part.charge();
                    days += part.days();
                    assert_eq!(part.to(), day(2027, 1, 1), "line {}", i + 1);
                }
                PartKind::Monthly => months = part.months(),
                _ => {}
            }
        }
        assert_eq!(
            (days, interest, months),
            (want_days, USCents(want_interest), want_months),
            "line {}",
            i + 1
        );
    }
    assert_eq!(p.total(), USCents(44_082));
    assert!(asked(
        &res.required(),
        PositionKind::UnpublishedRate,
        "Interest for 2027 isn't charged"
    ));
    // Paid on January 1, 2027, everything is at the 2026 rate. Paid a day
    // later, one day falls in 2027 and the gap is reported.
    for (filed, gap) in [(day(2027, 1, 1), false), (day(2027, 1, 2), true)] {
        let h = Household {
            return_filed: filed,
            ..four_thousand()
        };
        let res = penalty(&h, day(2027, 4, 15));
        let first = &res.value().lines()[0].parts()[0];
        assert_eq!(first.days(), 261, "paid {filed}");
        assert_eq!(
            asked(
                &res.required(),
                PositionKind::UnpublishedRate,
                "Interest for 2027"
            ),
            gap,
            "paid {filed}"
        );
    }
}

/// A payment between due dates pays the oldest underpayment first, on its
/// date. A payment on a due date pays that installment first, and only the
/// excess reaches an earlier one (32 V.S.A. § 5859(b)(2)(B)). Either way the
/// whole underpayment is charged until it is paid in full (IN-152 line 7).
#[test]
fn estimated_late_payments_and_full_payment() {
    // April missed, caught up on May 1: 16 days, $3.40, and one month, $10.
    let mut payments = vec![pay(day(2026, 5, 1), DOLLAR * 1_000)];
    payments.extend(quarterly(&[
        USCents(0),
        DOLLAR * 1_000,
        DOLLAR * 1_000,
        DOLLAR * 1_000,
    ]));
    let h = Household {
        payments,
        ..four_thousand()
    };
    let res = penalty(&h, day(2027, 4, 15));
    let lines = res.value().lines();
    assert!(lines.len() == 1 && lines[0].days() == 16 && res.value().total() == USCents(1_340));
    assert!(asked(
        &res.required(),
        PositionKind::UnsettledRule,
        "the figures follow the law"
    ));
    // April missed, $1,500 on June 15: June is paid first, and $500 reaches
    // April. April's whole $1,000 runs to April 15, 2027. By the running
    // totals June is still short, $1,500 of $2,000, with no shortfall of its
    // own.
    let mut h = Household {
        payments: quarterly(&[USCents(0), DOLLAR * 1_500, DOLLAR * 1_000, DOLLAR * 1_000]),
        ..four_thousand()
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    assert!(
        sched[0].status() == InstallmentStatus::Short
            && sched[1].status() == InstallmentStatus::Short
            && sched[1].shortfall() == USCents(0)
    );
    let p = penalty(&h, day(2027, 4, 15)).value();
    assert!(
        p.lines().len() == 1
            && p.lines()[0].shortfall() == DOLLAR * 1_000
            && p.total() == USCents(5_542 + 12_000)
    );
    // The last $500 paid February 1, 2027: the whole $1,000 runs to then, 10
    // months, not just the $500 still owed.
    h.payments.push(pay(day(2027, 2, 1), DOLLAR * 500));
    assert_eq!(
        penalty(&h, day(2027, 4, 15)).value().total(),
        USCents(5_542 + 10_000)
    );
}

/// Withholding counts 25% on each due date, whenever it was withheld, and
/// IN-152 has no line for the actual dates.
#[test]
fn estimated_withholding_is_spread_evenly() {
    for as_withheld in [false, true] {
        // $9,000 required, all withheld December 15, $1,000 left to pay.
        let h = Household {
            withheld: DOLLAR * 9_000,
            as_withheld,
            ..tax(DOLLAR * 10_000)
        };
        let res = penalty(&h, day(2027, 4, 15));
        assert_eq!(res.value().total(), USCents(0), "election {as_withheld}");
        let noted = res.applied().iter().any(|a| {
            a.applied()
                .contains("no line for the dates withholding was taken")
        });
        assert_eq!(noted, as_withheld);
    }
}

/// A filer with two-thirds of gross income from farming or fishing, this
/// year or last, makes no estimated payments. One basis point either side.
#[test]
fn estimated_farmers_and_fishers_are_exempt() {
    for (farm, fish, prior_year, exempt) in [
        (6_667, 0, false, true),
        (6_666, 0, false, false),
        // Farming and fishing together.
        (4_000, 2_667, false, true),
        (3_333, 3_333, false, false),
        (0, 0, true, true),
    ] {
        let e = new_estimated_facts()
            .prior_year_farmer_or_fisher(prior_year)
            .build()
            .unwrap();
        let h = Household {
            farm: Rate(farm),
            fish: Rate(fish),
            extras: Some(e),
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "farm {farm}, fish {fish}");
        // The farmer rule is stated in the reason, not the explanation.
        if exempt {
            assert!(res.value().why().contains("two-thirds"));
        }
        let p = penalty(&h, day(2027, 4, 15));
        assert_eq!(p.value().total() == USCents(0), exempt);
    }
    // Last year's farming not said: asked, and taken as not.
    let f = estimated::new_facts(VT, 2026)
        .filing_status(Status::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    assert!(
        !res.value().exempt()
            && asked(
                &res.required(),
                PositionKind::JurisdictionFact,
                "come from farming or fishing"
            )
    );
}

/// IN-152A: each federal Schedule AI installment over the federal required
/// annual payment, times the Vermont one.
#[test]
fn estimated_annualised_installments() {
    let e = new_estimated_facts()
        .prior_year_farmer_or_fisher(false)
        .federal_annualised(
            DOLLAR * 20_000,
            &[
                DOLLAR * 1_000,
                DOLLAR * 3_000,
                DOLLAR * 6_000,
                DOLLAR * 10_000,
            ],
        )
        .build()
        .unwrap();
    let mut h = Household {
        annualised: true,
        extras: Some(e),
        ..tax(DOLLAR * 10_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    // $9,000 ÷ $20,000 of $1,000, $3,000, $6,000 and $10,000.
    for (i, want) in [450, 1_800, 4_500, 9_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), DOLLAR * want, "installment {}", i + 1);
    }
    // Without the federal figures, the regular quarters, and the question.
    h.extras = None;
    let res = rules().schedule(&h.facts()).unwrap();
    assert!(
        res.value()[0].needed_by() == DOLLAR * 2_250
            && asked(
                &res.required(),
                PositionKind::JurisdictionFact,
                "Schedule AI"
            )
    );
    for (name, required, installments) in [
        (
            "three installments",
            USCents(100),
            vec![USCents(1), USCents(2), USCents(3)],
        ),
        (
            "no federal payment",
            USCents(0),
            vec![USCents(1), USCents(2), USCents(3), USCents(4)],
        ),
        (
            "negative",
            USCents(100),
            vec![USCents(1), USCents(-2), USCents(3), USCents(4)],
        ),
    ] {
        assert!(
            new_estimated_facts()
                .federal_annualised(required, &installments)
                .build()
                .is_err(),
            "{name}: accepted"
        );
    }
}

/// The four examples on the Department's Calculate Interest and Penalties
/// page. The first three state their answer; the fourth stops before it,
/// and under the $500 rule it owes nothing.
#[test]
fn estimated_department_examples() {
    let last = prior(DOLLAR * 800);
    let q = |a: USCents| quarterly(&[a, a, a, a]);
    for (name, h, want_penalty, required) in [
        // $800 a quarter is $200, and $150 leaves $50 short each quarter:
        // $2.77 + $6, $2.12 + $5, $1.15 + $3.50 and $1.50, with the 2027
        // days unpriced.
        (
            "one",
            Household {
                prior: last,
                payments: q(DOLLAR * 150),
                ..tax(DOLLAR * 1_000)
            },
            2_204,
            true,
        ),
        (
            "two",
            Household {
                withheld: DOLLAR * 100,
                prior: last,
                ..tax(DOLLAR * 400)
            },
            0,
            false,
        ),
        (
            "three",
            Household {
                withheld: DOLLAR * 400,
                prior: last,
                payments: q(DOLLAR * 100),
                ..tax(DOLLAR * 1_000)
            },
            0,
            true,
        ),
        (
            "four",
            Household {
                withheld: DOLLAR * 110,
                prior: last,
                ..tax(DOLLAR * 600)
            },
            0,
            false,
        ),
    ] {
        let f = h.facts();
        let r = rules().requirement(&f).unwrap().value();
        assert_eq!(r.required(), required, "example {name}: {}", r.why());
        assert_eq!(
            rules()
                .penalty(&f, day(2027, 4, 15))
                .unwrap()
                .value()
                .total(),
            USCents(want_penalty),
            "example {name}"
        );
    }
}

/// The penalty stops on the day the return is filed and paid, and never
/// runs past April 15, 2027.
#[test]
fn estimated_penalty_stops() {
    let h = Household {
        return_filed: day(2027, 3, 1),
        ..four_thousand()
    };
    assert_eq!(
        penalty(&h, day(2027, 4, 15)).value().as_of(),
        day(2027, 3, 1)
    );
    let p = penalty(&four_thousand(), day(2027, 6, 30)).value();
    assert!(p.as_of() == day(2027, 4, 15) && p.total() == USCents(44_082));
    assert!(
        rules()
            .penalty(&four_thousand().facts(), Date::default())
            .is_err()
    );
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want = [
        ("The $500 threshold", "less than $500", "§ 5857"),
        (
            "What a shortfall is measured against",
            "lesser of 90%",
            "§ 5859(b)(1)(A)",
        ),
        ("The prior-year figure", "100% of last year", "§ 5859(c)(1)"),
        (
            "When withholding counts",
            "25% on each due date",
            "§ 5852(c)",
        ),
        (
            "How a later payment is applied",
            "paid in full",
            "§ 5859(b)(2)(A)(ii)",
        ),
        ("The annualised installment", "Schedule AI", "§ 5852(b)"),
        (
            "A due date on a weekend or holiday",
            "next business day",
            "§ 3004",
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, form, source)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(
            d.form().contains(form)
                && d.statute_source().contains(source)
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{:?} does not pin both readings",
            d.rule()
        );
        // Every figure follows the forms. With no prior return, no payments
        // and no election, nothing these facts hold turns on a divergence.
        assert!(
            d.uses() == Reading::Form && !d.affects_household(),
            "{:?}",
            d.rule()
        );
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    let err = estimated_tax(2025).err().unwrap();
    assert!(err.is(&estimated::Error::NoRules), "2025: {err}");
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
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
            estimated::new_facts(VT, 2027)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(VT, 2026)
                .filing_status(Status::Single)
                .this_year(this)
                .state("not Vermont's")
                .build()
                .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
        assert!(rules().schedule(&f).is_err(), "{name}: scheduled");
        assert!(
            rules().penalty(&f, day(2027, 4, 15)).is_err(),
            "{name}: penalty figured"
        );
    }
}

#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    let all = format!(
        "{}{}{}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$500",
        "1% a month",
        "7.75%",
        "110%",
        "Child Care Contribution",
        "paid in full",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.sources().is_empty());
}

/// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
/// never as a negative amount.
#[test]
fn estimated_withholding_over_the_tax_reads_as_a_refund() {
    let figures = |t: USCents| {
        estimated::new_figures()
            .tax(t)
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
    let f = estimated::new_facts(VT, 2026)
        .filing_status(Status::Single)
        .this_year(figures(DOLLAR * 3_000))
        .prior_year(last)
        .withheld(&withheld)
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    let r = res.value();
    assert!(r.why().contains("a $600 refund"), "why = {:?}", r.why());
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

/// Vermont has no casualty, disaster, retirement or disability waiver, so a
/// waiver the federal rules would allow is refused and the penalty stands.
#[test]
fn estimated_waiver_is_refused() {
    let pos = |cause: WaiverCause, on: Date, born: Option<Date>| -> Positions {
        let mut b = estimated::new_positions();
        b.waiver_for(cause, on);
        if let Some(born) = born {
            b.born(born);
        }
        b.build().unwrap()
    };
    for (name, p) in [
        (
            "casualty",
            estimated::new_positions()
                .waiver(WaiverKind::Casualty)
                .build()
                .unwrap(),
        ),
        (
            "disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
        ),
        (
            "retired at 64",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1962, 3, 1)),
            ),
        ),
        (
            "disabled this year",
            pos(WaiverCause::Disability, day(2026, 6, 1), None),
        ),
    ] {
        let h = Household {
            prior: prior(DOLLAR * 20_000),
            positions: Some(p),
            ..tax(DOLLAR * 10_000)
        };
        let p = penalty(&h, day(2027, 4, 15)).value();
        let o = p.waiver_outcome().unwrap();
        assert!(
            p.total().0 != 0 && o.refused() && o.reason().contains("no waiver"),
            "{name}"
        );
        assert_eq!(p.if_waived(), p.total(), "{name}");
    }
}

/// An exception sets the target, so last year's return is not asked for.
#[test]
fn estimated_exempt_does_not_ask_for_last_year() {
    let res = rules().requirement(&tax(DOLLAR * 400).facts()).unwrap();
    assert!(res.value().exempt() && !asked(&res.required(), PositionKind::PriorYearReturn, "2025"));
}
