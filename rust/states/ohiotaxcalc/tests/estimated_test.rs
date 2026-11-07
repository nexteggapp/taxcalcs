//! Ported from ohiotaxcalc/estimated_test.go: Ohio's estimated tax rules for
//! 2026, checked against the Department of Taxation's own documents and the
//! Revised Code.
//!
//! Ohio publishes no worked example of the interest penalty. The strongest
//! check is the 2025 IT/SD 2210's eight printed line 15 factors: the formula
//! the 2026 rules use reproduces every one, including the third column's,
//! which spans the change from 8% in 2025 to 7% in 2026. The penalty cases
//! are worked by hand from the form's line arithmetic. The Go checks that
//! compare each figure and quote against the extracts in testdata/ are not
//! ported: they check the Go package's citations, not the rules.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, Elections, EstimatedTax, Facts, FactsBuilder, Jurisdiction, Month, Payment,
    Positions, PriorReturn, Status as InstallmentStatus, WaiverCause, WaiverKind, Weekday,
};
use filing::Status;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_ohiotaxcalc::{
    EstimatedFacts, calendar_for, estimated_tax, factor_for, new_estimated_facts, rates_for,
};
use working::{PositionKind, Required, Rule};

const OHIO: Jurisdiction = Jurisdiction::from_static("OH");

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

fn dues() -> [Date; 4] {
    [
        day(2026, 4, 15),
        day(2026, 6, 15),
        day(2026, 9, 15),
        day(2027, 1, 15),
    ]
}

fn timely(on: bool) -> Option<EstimatedFacts> {
    Some(
        new_estimated_facts()
            .prior_year_return_timely(on)
            .build()
            .unwrap(),
    )
}

#[derive(Clone, Default)]
struct Household {
    /// After credits, IT 1040 line 10 less line 16.
    tax: USCents,
    no_tax: bool,
    /// Withheld on December 15.
    withheld: USCents,
    prior: Option<PriorReturn>,
    /// `None` is a timely prior return.
    extras: Option<EstimatedFacts>,
    no_extras: bool,
    payments: Vec<Payment>,
    return_filed: Date,
    elections: Option<Elections>,
    waiver: Option<WaiverKind>,
    first_met: Date,
    positions: Option<Positions>,
}

impl Household {
    fn facts(&self) -> Facts {
        let fig = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(OHIO, 2026);
        b.filing_status(Status::Single)
            .this_year(fig)
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
        if !self.no_extras {
            b.state(self.extras.or(timely(true)).unwrap());
        }
        if let Some(e) = self.elections {
            b.elections(e);
        }
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed twelve-month return with its tax after credits.
fn prior(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .build()
            .unwrap(),
    )
}

/// A household whose target is $40,000: 90% of $44,444.44, and no prior
/// return on record. Columns need $10,000, $20,000, $30,000 and $40,000.
fn forty_thousand() -> Household {
    Household {
        tax: USCents(4_444_444),
        ..Household::default()
    }
}

fn penalty(h: &Household, as_of: Date) -> working::Result<estimated::Penalty> {
    rules().penalty(&h.facts(), as_of).unwrap()
}

/// The rate series is 7% for calendar year 2026 and nothing else.
#[test]
fn estimated_rates_are_seven_percent_for_twenty_twenty_six() {
    let ps = rates_for(2026).unwrap().periods();
    assert!(
        ps.len() == 1
            && ps[0].from() == day(2026, 1, 1)
            && ps[0].to() == day(2026, 12, 31)
            && ps[0].rate() == percent(7)
    );
}

/// The 2026 dates are the ones IT 1040ES prints. Each is a weekday and none
/// is an ORC 1.14 holiday, so none moves.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&forty_thousand().facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i], "installment {}", i + 1);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
        assert_eq!(inst.needed_by(), DOLLAR * 10_000 * (i as i64 + 1));
    }
}

/// The calendar is ORC 1.14's holidays, worked out here from the section's
/// own rules, and it moves a weekend or holiday to the next business day.
#[test]
fn estimated_calendar_moves_weekends_and_holidays() {
    let nth = |year: i64, m: i64, wd: Weekday, n: i64| -> Date {
        let mut d = day(year, m, 1);
        while d.weekday() != wd {
            d = d.add_days(1);
        }
        d.add_days(7 * (n - 1))
    };
    let last_monday = |year: i64, m: i64| -> Date {
        let mut d = day(year, m + 1, 1).add_days(-1);
        while d.weekday() != Weekday::MONDAY {
            d = d.add_days(-1);
        }
        d
    };
    let mut want = Vec::new();
    for y in [2026, 2027] {
        want.extend([
            day(y, 1, 1),
            nth(y, 1, Weekday::MONDAY, 3),
            nth(y, 2, Weekday::MONDAY, 3),
        ]);
        if y == 2026 {
            want.extend([
                last_monday(y, 5),
                day(y, 6, 19),
                day(y, 7, 4),
                nth(y, 9, Weekday::MONDAY, 1),
                nth(y, 10, Weekday::MONDAY, 2),
                day(y, 11, 11),
                nth(y, 11, Weekday::THURSDAY, 4),
                day(y, 12, 25),
            ]);
        }
    }
    let c = calendar_for(2026).unwrap();
    let got = c.holidays();
    assert_eq!(got.len(), want.len());
    for (i, h) in got.iter().enumerate() {
        assert_eq!(h.on(), want[i], "holiday {} ({})", i + 1, h.name());
        assert_ne!(
            h.on().weekday(),
            Weekday::SUNDAY,
            "{} falls on a Sunday",
            h.name()
        );
    }
    for (from, to) in [
        // Juneteenth, a Friday.
        (day(2026, 6, 19), day(2026, 6, 22)),
        // A Saturday, then Martin Luther King day.
        (day(2027, 1, 16), day(2027, 1, 19)),
        // The Sunday the 2025 form moves to 6/16/25.
        (day(2025, 6, 15), day(2025, 6, 16)),
        (day(2026, 6, 15), day(2026, 6, 15)),
    ] {
        assert_eq!(c.next_business_day(from), to, "{from}");
    }
}

/// The $500 test, one cent either side: tax after credits less withholding
/// of $500 or less needs no estimates and owes no penalty.
#[test]
fn estimated_threshold_boundaries() {
    // $500.00 left, $500.01 left, $499.99 left.
    for (withheld, exempt) in [
        (DOLLAR * 500, true),
        (USCents(49_999), false),
        (USCents(50_001), true),
    ] {
        let f = Household {
            tax: DOLLAR * 1_000,
            withheld,
            ..Household::default()
        }
        .facts();
        let req = rules().requirement(&f).unwrap().value();
        assert!(
            req.exempt() == exempt && req.required() != exempt,
            "withheld {withheld}: {}",
            req.why()
        );
        let p = rules().penalty(&f, day(2027, 4, 15)).unwrap().value();
        assert_eq!(p.total() == USCents(0), exempt, "withheld {withheld}");
        // The exception is the chosen option, at $0, and the penalty's
        // reason.
        if exempt {
            let c = req.chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount() == USCents(0)
                    && c.label() == "Tax less withholding of $500 or less"
                    && p.reason() == "Tax less withholding of $500 or less.",
                "withheld {withheld}: {:?}, {:?}",
                c.label(),
                p.reason()
            );
        }
    }
}

/// The target is the lesser of 90% of this year's tax and 100% of last
/// year's, and last year's is open only after a timely filed return.
#[test]
fn estimated_target_options() {
    let not_filed = estimated::new_prior_return().build().unwrap();
    for (name, p, extras, chosen, target, ask) in [
        (
            "last year lower",
            prior(DOLLAR * 20_000),
            None,
            Basis::PriorYearTax,
            DOLLAR * 20_000,
            PositionKind::Unspecified,
        ),
        (
            "this year lower",
            prior(DOLLAR * 50_000),
            None,
            Basis::ThisYearTax,
            DOLLAR * 40_000,
            PositionKind::Unspecified,
        ),
        (
            "a cent apart",
            prior(USCents(3_999_999)),
            None,
            Basis::PriorYearTax,
            USCents(3_999_999),
            PositionKind::Unspecified,
        ),
        (
            "filed late",
            prior(DOLLAR * 20_000),
            timely(false),
            Basis::ThisYearTax,
            DOLLAR * 40_000,
            PositionKind::Unspecified,
        ),
        (
            "timeliness not given",
            prior(DOLLAR * 20_000),
            Some(EstimatedFacts::default()),
            Basis::ThisYearTax,
            DOLLAR * 40_000,
            PositionKind::JurisdictionFact,
        ),
        (
            "no return on record",
            None,
            None,
            Basis::ThisYearTax,
            DOLLAR * 40_000,
            PositionKind::PriorYearReturn,
        ),
        (
            "no return filed",
            Some(not_filed),
            None,
            Basis::ThisYearTax,
            DOLLAR * 40_000,
            PositionKind::Unspecified,
        ),
        (
            "nothing owed and filed",
            prior(USCents(0)),
            None,
            Basis::PriorYearTax,
            USCents(0),
            PositionKind::Unspecified,
        ),
    ] {
        let h = Household {
            prior: p,
            extras,
            ..forty_thousand()
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let r = res.value();
        assert_eq!(r.chosen().unwrap().basis(), chosen, "{name}");
        assert_eq!(r.target(), target, "{name}");
        let opt = &r.options()[1];
        assert_eq!(
            opt.available(),
            chosen == Basis::PriorYearTax || name == "this year lower",
            "{name}: {}",
            opt.why_not()
        );
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}: {ask} is not asked");
        }
    }
    // The form asks nothing about a short year, so a six-month return
    // counts.
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 20_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let h = Household {
        prior: Some(short),
        ..forty_thousand()
    };
    assert_eq!(
        rules().requirement(&h.facts()).unwrap().value().target(),
        DOLLAR * 20_000
    );
}

/// The 2025 IT/SD 2210 prints eight line 15 factors. The formula the 2026
/// rules use, 8% for 2025 and 7% for 2026 times the days late ÷ 365.25 to
/// six places, gives every one. The third columns span the change of rate
/// on January 1, 2026, the nearest published change: 107 or 77 days at 8%
/// and 15 at 7%.
#[test]
fn estimated_factors_reproduce_the_twenty_twenty_five_form() {
    let rates = estimated::new_rate_series()
        .period(
            day(2025, 1, 1),
            day(2025, 12, 31),
            percent(8),
            "IT/SD 2210 (2025): 8% for 2025",
        )
        .period(
            day(2026, 1, 1),
            day(2026, 12, 31),
            percent(7),
            "IT/SD 2210 (2025): 7% for 2026",
        )
        .build()
        .unwrap();
    for (from, to, want) in [
        // Page 2, individuals.
        (day(2025, 4, 15), day(2025, 6, 16), "0.013580"),
        (day(2025, 6, 16), day(2025, 9, 15), "0.019932"),
        (day(2025, 9, 15), day(2026, 1, 15), "0.026311"),
        (day(2026, 1, 15), day(2026, 4, 15), "0.017248"),
        // Page 3, pass-through entities, due in the 4th, 7th and 10th
        // months.
        (day(2025, 4, 15), day(2025, 7, 15), "0.019932"),
        (day(2025, 7, 15), day(2025, 10, 15), "0.020151"),
        (day(2025, 10, 15), day(2026, 1, 15), "0.019740"),
    ] {
        assert_eq!(
            factor_for(2026, from, to, rates.clone()).unwrap(),
            want,
            "{from} to {to}"
        );
    }
    // At 8% alone the third column would be 0.026721, not the printed one.
    let eight = estimated::new_rate_series()
        .period(
            day(2025, 1, 1),
            day(2026, 12, 31),
            percent(8),
            "8% throughout",
        )
        .build()
        .unwrap();
    assert_eq!(
        factor_for(2026, day(2025, 9, 15), day(2026, 1, 15), eight).unwrap(),
        "0.026721"
    );
}

/// No payments against a $40,000 target. Each column's line 14 is the
/// cumulative shortfall, charged at the span's factor and rounded to the
/// dollar: $10,000 × 0.011691 is $116.91, so $117; $20,000 × 0.017632 is
/// $352.64, so $353; $30,000 × 0.020507 for September 15 to December 31 is
/// $615.21, so $615. The 2027 rate is not published, so the third column's
/// January days and the whole fourth column are not charged.
#[test]
fn estimated_penalty_arithmetic() {
    let res = penalty(&forty_thousand(), day(2027, 4, 15));
    let p = res.value();
    let lines = p.lines();
    assert_eq!(lines.len(), 4);
    let want = [
        (DOLLAR * 10_000, 61, "0.011691", DOLLAR * 117),
        (DOLLAR * 20_000, 92, "0.017632", DOLLAR * 353),
        (DOLLAR * 30_000, 107, "0.020507", DOLLAR * 615),
        (DOLLAR * 40_000, 0, "", USCents(0)),
    ];
    for (i, (short, days, factor, charge)) in want.into_iter().enumerate() {
        let l = &lines[i];
        assert_eq!(
            (l.shortfall(), l.days(), l.total()),
            (short, days, charge),
            "line {}",
            i + 1
        );
        let parts = l.parts();
        if factor.is_empty() {
            assert!(parts.is_empty(), "line {}", i + 1);
            continue;
        }
        assert!(
            parts.len() == 1 && parts[0].rate() == percent(7) && parts[0].source().contains(factor),
            "line {}",
            i + 1
        );
    }
    assert!(p.total() == DOLLAR * 1_085 && !p.upper_bound());
    assert_eq!(lines[2].parts()[0].to(), day(2026, 12, 31));
    let n = res
        .required()
        .iter()
        .filter(|r| r.position() == PositionKind::UnpublishedRate)
        .count();
    assert_eq!(n, 2);
    // Figured to May 15, 2026, only 30 days of the first column: 7% × 30 ÷
    // 365.25 is 0.005749, and $57.49 rounds to $57. No 2027 day is reached.
    let res = penalty(&forty_thousand(), day(2026, 5, 15));
    assert!(
        res.value().total() == DOLLAR * 57
            && res.value().lines().len() == 1
            && res.value().as_of() == day(2026, 5, 15)
    );
    let n = res
        .required()
        .iter()
        .filter(|r| r.position() == PositionKind::UnpublishedRate)
        .count();
    assert_eq!(n, 1);
}

/// A payment between two due dates counts only from the next one. April's
/// $10,000 paid on May 1 is still charged April's whole span, $117, and the
/// June and September columns are short by $10,000 and $20,000 less than
/// before: $176 and $410. The statute would charge April's 16 days alone.
#[test]
fn estimated_payment_between_dates_counts_at_the_next_date() {
    let h = Household {
        payments: vec![pay(day(2026, 5, 1), DOLLAR * 10_000)],
        ..forty_thousand()
    };
    let p = penalty(&h, day(2027, 4, 15)).value();
    let lines = p.lines();
    assert!(
        lines[0].shortfall() == DOLLAR * 10_000
            && lines[0].days() == 61
            && lines[0].total() == DOLLAR * 117
    );
    assert!(lines[1].shortfall() == DOLLAR * 10_000 && lines[2].shortfall() == DOLLAR * 20_000);
    assert_eq!(p.total(), DOLLAR * 703);
}

/// Lines 1 and 5: withholding and timely payments that reach the target for
/// the year owe nothing, whenever in the year they came. A cent short, or
/// the same payment after January 15, and every column is charged.
#[test]
fn estimated_whole_year_test() {
    for (name, payment, total) in [
        (
            "the whole target on January 15",
            pay(day(2027, 1, 15), DOLLAR * 40_000),
            USCents(0),
        ),
        (
            "a cent short",
            pay(day(2027, 1, 15), USCents(3_999_999)),
            DOLLAR * 1_085,
        ),
        (
            "the whole target on January 19",
            pay(day(2027, 1, 19), DOLLAR * 40_000),
            DOLLAR * 1_085,
        ),
    ] {
        let h = Household {
            payments: vec![payment],
            ..forty_thousand()
        };
        assert_eq!(
            penalty(&h, day(2027, 4, 15)).value().total(),
            total,
            "{name}"
        );
    }
    let h = Household {
        payments: vec![pay(day(2027, 1, 15), DOLLAR * 40_000)],
        ..forty_thousand()
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, inst) in sched.iter().enumerate() {
        let want = if i == 3 {
            InstallmentStatus::Met
        } else {
            InstallmentStatus::Excused
        };
        assert_eq!(inst.status(), want, "installment {}", i + 1);
        if i < 3 {
            assert_eq!(inst.shortfall(), DOLLAR * 10_000);
        }
    }
}

/// Line 7: after a timely return, payments for the year that reach last
/// year's tax owe nothing. After a late one the target is 90% of this
/// year's.
#[test]
fn estimated_prior_year_safe_harbour() {
    let mut h = Household {
        prior: prior(DOLLAR * 20_000),
        payments: vec![pay(day(2027, 1, 15), DOLLAR * 20_000)],
        ..forty_thousand()
    };
    assert_eq!(penalty(&h, day(2027, 4, 15)).value().total(), USCents(0));
    h.extras = timely(false);
    assert_eq!(
        penalty(&h, day(2027, 4, 15)).value().total(),
        DOLLAR * 1_085
    );
}

/// Line 11: withholding counts 25% by each date, whenever it was withheld,
/// and the election to use the dates withheld has no line on the form.
/// $20,000 withheld in December against $40,000: each column is half short.
/// $5,000 × 0.011691 is $58.46, so $58; $10,000 × 0.017632 is $176.32, so
/// $176; $15,000 × 0.020507 is $307.61, so $308.
#[test]
fn estimated_withholding_is_spread_evenly() {
    let mut h = Household {
        withheld: DOLLAR * 20_000,
        ..forty_thousand()
    };
    let p = penalty(&h, day(2027, 4, 15));
    assert_eq!(p.value().total(), DOLLAR * 542);
    h.elections = Some(
        estimated::new_elections()
            .withholding_as_withheld()
            .build()
            .unwrap(),
    );
    let q = penalty(&h, day(2027, 4, 15));
    assert_eq!(q.value().total(), p.value().total());
    assert!(applied(
        &q.applied(),
        "no line for crediting withholding on the dates it was withheld"
    ));
}

/// Last year's overpayment applied forward counts on April 15. $10,000 of
/// it meets April; June and September are short $10,000 and $20,000.
#[test]
fn estimated_prior_year_overpayment() {
    let h = Household {
        payments: vec![
            estimated::new_payment()
                .amount(DOLLAR * 10_000)
                .from_prior_year()
                .build()
                .unwrap(),
        ],
        ..forty_thousand()
    };
    let p = penalty(&h, day(2027, 4, 15)).value();
    let lines = p.lines();
    assert!(lines.len() == 3 && lines[0].installment() == 2 && p.total() == DOLLAR * 586);
}

/// The form runs the last span to April 15, 2027 whenever the return was
/// filed, and never past it.
#[test]
fn estimated_return_filed_does_not_shorten_the_form() {
    let h = Household {
        return_filed: day(2027, 2, 1),
        ..forty_thousand()
    };
    let res = penalty(&h, day(2027, 6, 30));
    assert!(res.value().as_of() == day(2027, 4, 15) && res.value().total() == DOLLAR * 1_085);
    assert!(applied(
        &res.applied(),
        "filing early does not shorten the penalty"
    ));
}

/// Ohio has no waiver. From tax year 2025 the commissioner may abate the
/// penalty, which is shown before any abatement.
#[test]
fn estimated_abatement_is_the_commissioners() {
    let h = Household {
        waiver: Some(WaiverKind::Casualty),
        ..forty_thousand()
    };
    let res = penalty(&h, day(2027, 4, 15));
    assert!(
        res.value().total() == DOLLAR * 1_085
            && res.value().waiver() == WaiverKind::Casualty
            && applied(&res.applied(), "may abate the penalty in whole or in part")
    );
}

/// The annualised test and a late start have no line on the form, so
/// neither changes the figure, and the rules say so.
#[test]
fn estimated_statutory_tests_the_form_omits() {
    let h = forty_thousand();
    let periods: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 5, 31),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .iter()
    .map(|&e| estimated::new_period().end(e).build().unwrap())
    .collect();
    let f = estimated::new_facts(OHIO, 2026)
        .filing_status(Status::Single)
        .this_year(estimated::new_figures().tax(h.tax).build().unwrap())
        .state(timely(true).unwrap())
        .elections(estimated::new_elections().annualised().build().unwrap())
        .periods(&periods)
        .build()
        .unwrap();
    let res = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    assert!(
        res.value().total() == DOLLAR * 1_085
            && applied(&res.applied(), "the annualised election is not applied")
    );
    let h = Household {
        first_met: day(2026, 7, 1),
        ..forty_thousand()
    };
    let res = penalty(&h, day(2027, 4, 15));
    assert!(
        res.value().total() == DOLLAR * 1_085
            && applied(&res.applied(), "Ohio has no exception for a late start")
    );
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&forty_thousand().facts()).unwrap();
    let want = [
        (
            "Last year's tax as the target",
            "Ohio return on time",
            "5747.09(A)(4)(b)",
            "on time or late",
        ),
        (
            "The whole-year tests",
            "however they were spread",
            "5747.09(D)(1) to (4) and (E)",
            "by its own date",
        ),
        (
            "What each column is short",
            "25%, 50%, 75% or 100%",
            "5747.09(A)(4) and (D)(1) to (4)",
            "only after the installment then due is covered",
        ),
        (
            "When the interest stops",
            "to the next due date, and the last to April 15",
            "ORC 5747.09(D)",
            "to the date the payment is made",
        ),
        (
            "Withholding on the dates withheld",
            "no line for the dates",
            "ORC 5747.09(B)",
            "establish the dates",
        ),
        (
            "The annualised income test",
            "withdrawn from tax year 2017",
            "ORC 5747.09(E)(1)",
            "annualised to the end of the month before",
        ),
        (
            "The 2026 worksheet's tax brackets",
            "3.125% over $100,000",
            "5747.02(A)(3)(c)",
            "$332 plus 2.75%",
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, form, source, statute)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(
            d.form().contains(form)
                && d.statute_source().contains(source)
                && d.statute().contains(statute)
                && !d.form_source().is_empty(),
            "{:?} does not pin both readings",
            d.rule()
        );
        // With nothing paid and no elections, the payment-order,
        // withholding, annualising and bracket readings change nothing.
        if [
            "What each column is short",
            "When the interest stops",
            "Withholding on the dates withheld",
            "The annualised income test",
            "The 2026 worksheet's tax brackets",
        ]
        .contains(&d.rule().as_str())
        {
            assert!(!d.affects_household(), "{:?}", d.rule());
        }
        // The out-of-date worksheet is dated against the act that replaced
        // it.
        if d.rule() == "The 2026 worksheet's tax brackets" {
            assert!(
                d.form().contains("out of date")
                    && d.statute_source().contains("effective September 30, 2025")
            );
        }
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    let err = estimated_tax(2025).err().unwrap();
    assert!(err.is(&estimated::Error::NoRules), "2025: {err}");
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let base = || -> FactsBuilder {
        let mut b = estimated::new_facts(OHIO, 2026);
        b.filing_status(Status::Single).this_year(this);
        b
    };
    for (name, f) in [
        ("a farmer", base().farm_share(Rate(6_667)).build().unwrap()),
        ("a fisher", base().fish_share(Rate(6_667)).build().unwrap()),
        (
            "both, two-thirds together",
            base()
                .farm_share(Rate(4_000))
                .fish_share(Rate(2_667))
                .build()
                .unwrap(),
        ),
    ] {
        let err = rules().requirement(&f).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    let under = base().farm_share(Rate(6_666)).build().unwrap();
    assert!(rules().requirement(&under).is_ok());
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
            estimated::new_facts(OHIO, 2027)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        ("other extras", base().state("not Ohio's").build().unwrap()),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&forty_thousand().facts(), Date::default())
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
        "more than $500",
        "90%",
        "100%",
        "on time",
        "whole year",
        "next date",
        "April 15",
        "7% for 2026",
        "365.25",
        "may reduce or remove it",
        "school district",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.versus_federal().is_empty() && !e.sources().is_empty());
}

/// Without this year's tax the target is last year's tax, the most line 9
/// can be, and the penalty against it is an upper bound unless the year's
/// payments reach it.
#[test]
fn estimated_without_this_years_tax() {
    let mut h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 20_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 20_000, "{}", r.why());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [5_000, 10_000, 15_000, 20_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), DOLLAR * want, "installment {}", i + 1);
    }
    // $5,000, $10,000 and $15,000 short: $58 + $176 + $308.
    let pen = penalty(&h, day(2027, 4, 15)).value();
    assert!(pen.total() == DOLLAR * 542 && (pen.upper_bound() || !pen.rated_through().is_zero()));
    h.payments = vec![pay(day(2027, 1, 15), DOLLAR * 20_000)];
    let pen = penalty(&h, day(2027, 4, 15)).value();
    assert!(pen.total() == USCents(0) && !pen.upper_bound());
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let r = rules().requirement(&none.facts()).unwrap().value();
    assert!(!r.target_known() && !r.required(), "{}", r.why());
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

/// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
/// never as a negative amount.
#[test]
fn estimated_withholding_over_the_tax_reads_as_a_refund() {
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
    let f = estimated::new_facts(OHIO, 2026)
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

/// Ohio has no casualty, retirement or disability waiver. From tax year
/// 2025 the Tax Commissioner may abate the penalty, ORC 5747.09(D), which
/// names no grounds, so a retirement the federal rules would waive is
/// undecided.
#[test]
fn estimated_waiver_outcomes() {
    let mut h = forty_thousand();
    let before = penalty(&h, day(2027, 4, 15)).value().total();
    assert_ne!(before, USCents(0), "no penalty to waive");
    let born = day(1962, 3, 1);
    let pos = |cause: WaiverCause, on: Date, born: Option<Date>| -> Positions {
        let mut b = estimated::new_positions();
        b.waiver_for(cause, on);
        if let Some(born) = born {
            b.born(born);
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
            "abate all or part",
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
        let res = penalty(&h, day(2027, 4, 15));
        let p = res.value();
        let o = p.waiver_outcome().unwrap();
        let got = if o.undecided() {
            "undecided"
        } else if o.refused() {
            "refused"
        } else if o.eligible() {
            "eligible"
        } else {
            ""
        };
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
        assert!(
            has(&res.required(), PositionKind::UnsettledRule),
            "{name}: the open point is not reported"
        );
    }
}
