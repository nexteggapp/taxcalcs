//! Ported from rhodeislandtaxcalc/estimated_test.go: Rhode Island's
//! estimated tax rules for 2026, checked against the Division of Taxation's
//! own documents and the General Laws.
//!
//! Rhode Island publishes no exhaustive oracle and no worked example with an
//! answer: neither the RI-1040ES nor the RI-2210 works a case. The nearest
//! thing is the RI-2210A shortcut factor, 7.9808%, which the tests derive
//! from the 12% rate and the day counts these rules compute. The penalty
//! cases are worked by hand from RI-2210 lines 13 to 22. They check what the
//! Go package's author read the form to say, not an answer the Division
//! published. The Go checks that compare each figure and quote against the
//! extracts in testdata/ are not ported: they check the Go package's
//! citations, not the rules.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, Payment, Positions, PriorReturn,
    Status as InstallmentStatus, WaiverCause, WaiverKind, Weekday,
};
use filing::Status;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_rhodeislandtaxcalc::{
    EstimatedFacts, EstimatedFactsBuilder, JanuaryReturn, estimated_tax, holidays,
    new_estimated_facts,
};
use working::{PositionKind, Reading, Required};

const RI: Jurisdiction = Jurisdiction::from_static("RI");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn count(rs: &[Required], k: PositionKind) -> usize {
    rs.iter().filter(|r| r.position() == k).count()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    count(rs, k) > 0
}

fn required_about(rs: &[Required], k: PositionKind, phrase: &str) -> bool {
    rs.iter().any(|r| {
        r.position() == k && (r.question().contains(phrase) || r.effect().contains(phrase))
    })
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

/// One payment of `a` on each due date; a zero is no payment.
fn quarterly(a: &[USCents]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .filter(|(_, x)| x.0 > 0)
        .map(|(i, &x)| pay(dues()[i], x))
        .collect()
}

fn period_ends() -> [Date; 4] {
    [
        day(2026, 3, 31),
        day(2026, 5, 31),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
}

#[derive(Clone, Default)]
struct Household {
    /// After credits, RI-2210 line 1.
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    as_withheld: bool,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    /// RI-2210 Part 5 line 26 for each period.
    annualised: Option<Vec<USCents>>,
    period_ends: Option<Vec<Date>>,
    waiver: Option<WaiverKind>,
    positions: Option<Positions>,
    farm: Rate,
    fish: Rate,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let fig = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(RI, 2026);
        b.filing_status(Status::Single)
            .this_year(fig)
            .paid(&self.payments)
            .requirement_first_met(self.first_met)
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
        match self.extras {
            Some(e) => b.state(e),
            None => b.state(
                new_estimated_facts()
                    .prior_year_no_federal_liability(false)
                    .build()
                    .unwrap(),
            ),
        };
        let mut el = estimated::new_elections();
        if self.as_withheld {
            el.withholding_as_withheld();
        }
        if let Some(ann) = &self.annualised {
            el.annualised();
            let ends = self.period_ends.clone().unwrap_or(period_ends().to_vec());
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
        }
        b.elections(el.build().unwrap());
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed twelve-month Rhode Island return with its tax.
fn prior(tax: USCents, resident: bool) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .full_year_resident(resident)
            .build()
            .unwrap(),
    )
}

fn extras(b: &mut EstimatedFactsBuilder) -> Option<EstimatedFacts> {
    Some(b.build().unwrap())
}

fn penalty(h: &Household, as_of: Date) -> working::Result<estimated::Penalty> {
    rules().penalty(&h.facts(), as_of).unwrap()
}

/// The nth Monday of a month, or the last for n = -1.
fn nth_monday(year: i64, m: i64, n: i64) -> Date {
    if n < 0 {
        let mut d = day(year, m + 1, 1).add_days(-1);
        while d.weekday() != Weekday::MONDAY {
            d = d.add_days(-1);
        }
        return d;
    }
    let mut d = day(year, m, 1);
    while d.weekday() != Weekday::MONDAY {
        d = d.add_days(1);
    }
    d.add_days(7 * (n - 1))
}

/// A fixed-date holiday, moved to the Monday after a weekend as § 25-1-1
/// says state employees celebrate it.
fn fixed(year: i64, m: i64, dd: i64) -> Date {
    let d = day(year, m, dd);
    match d.weekday() {
        Weekday::SATURDAY => d.add_days(2),
        Weekday::SUNDAY => d.add_days(1),
        _ => d,
    }
}

/// The calendar is § 25-1-1's holidays from the first due date to February
/// 16, 2027, each derived here from the words of the section. No due date is
/// one, and none falls on a weekend; the RI-1040ES's February 15, 2027 is
/// Washington's Birthday.
#[test]
fn estimated_holidays_follow_the_statute() {
    let want = [
        fixed(2026, 5, 4),
        nth_monday(2026, 5, -1),
        fixed(2026, 6, 19),
        fixed(2026, 7, 4),
        nth_monday(2026, 8, 2),
        nth_monday(2026, 9, 1),
        nth_monday(2026, 10, 2),
        fixed(2026, 11, 11),
        fixed(2026, 12, 25),
        fixed(2027, 1, 1),
        nth_monday(2027, 1, 3),
        nth_monday(2027, 2, 3),
    ];
    let got = holidays(2026).unwrap();
    assert_eq!(got.len(), want.len());
    for (i, h) in got.iter().enumerate() {
        assert_eq!(h.on(), want[i], "holiday {} ({})", i + 1, h.name());
    }
    assert_eq!(want[11], day(2027, 2, 15));
    for d in dues() {
        let wd = d.weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY, "{d}");
        assert!(!want.contains(&d), "{d} is a holiday");
    }
}

/// The 2026 dates are the ones the RI-1040ES prints, and none moves.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&tax(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i], "installment {}", i + 1);
        assert_eq!(inst.needed_by(), DOLLAR * 2_000 * (i as i64 + 1));
    }
    for r in res.applied() {
        assert!(
            !r.cite().starts_with("R.I. Gen. Laws § 44-30-91(b)"),
            "a date moved: {}",
            r.applied()
        );
    }
}

/// RI-2210 line 4: no charge at $250.00 or less of tax after withholding.
/// The law asks for payments at $250 or more, so $250.00 exactly owes
/// estimates and no charge.
#[test]
fn estimated_threshold_boundaries() {
    // $249.99, $250.00, $250.01.
    for (withheld, exempt) in [(75_001, true), (75_000, true), (74_999, false)] {
        let h = Household {
            withheld: USCents(withheld),
            ..tax(DOLLAR * 1_000)
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
                    && c.label() == "Tax less withholding of $250 or less"
            );
        }
        if withheld == 75_000 {
            assert!(
                r.why()
                    .contains("asks for estimated payments at $250 or more"),
                "{}",
                r.why()
            );
        }
    }
}

/// The target is the smaller of 80% of this year's tax and 100% of last
/// year's. RI-2210 line 5 sets no condition on last year's return beyond its
/// tax, so a short year counts; a return not filed does not.
#[test]
fn estimated_target_options() {
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 5_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    for (name, p, target, basis, prior_open) in [
        (
            "last year's is smaller",
            prior(DOLLAR * 7_000, true),
            DOLLAR * 7_000,
            Basis::PriorYearTax,
            true,
        ),
        (
            "this year's is smaller",
            prior(DOLLAR * 9_000, false),
            DOLLAR * 8_000,
            Basis::ThisYearTax,
            true,
        ),
        (
            "a six-month year",
            Some(short),
            DOLLAR * 5_000,
            Basis::PriorYearTax,
            true,
        ),
        (
            "no return filed",
            Some(not_filed),
            DOLLAR * 8_000,
            Basis::ThisYearTax,
            false,
        ),
        (
            "no return on record",
            None,
            DOLLAR * 8_000,
            Basis::ThisYearTax,
            false,
        ),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let r = res.value();
        assert_eq!(r.target(), target, "{name}");
        assert_eq!(r.chosen().unwrap().basis(), basis, "{name}");
        let opts = r.options();
        assert!(opts[0].percent() == percent(80) && opts[1].percent() == percent(100));
        assert_eq!(opts[1].available(), prior_open, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn),
            p.is_none(),
            "{name}"
        );
    }
}

/// Without this year's tax the target is 100% of last year's, and the charge
/// is the most it can be. With no prior year either there is no target. The
/// no-liability exception needs nothing about this year, and its zero is
/// exact.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 6_000, true),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 6_000, "{}", r.why());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [150_000, 300_000, 450_000, 600_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), USCents(want), "installment {}", i + 1);
    }
    let pen = penalty(&h, day(2026, 12, 31));
    assert!(pen.value().upper_bound() && pen.value().total().0 != 0);
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
    let exempt = Household {
        no_tax: true,
        prior: prior(DOLLAR * 900, true),
        extras: extras(new_estimated_facts().prior_year_no_federal_liability(true)),
        ..Household::default()
    };
    let p = penalty(&exempt, day(2027, 4, 15)).value();
    assert!(
        p.total() == USCents(0)
            && !p.upper_bound()
            && p.reason() == "No federal tax liability last year.",
        "{:?}",
        p.reason()
    );
}

/// RI-2210 line 5 enters zero for last year's tax after a year with no
/// federal liability, for a full-year Rhode Island resident.
#[test]
fn estimated_no_federal_liability_last_year() {
    let none = extras(new_estimated_facts().prior_year_no_federal_liability(true));
    let unanswered = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(DOLLAR * 900).build().unwrap())
        .build()
        .unwrap();
    for (name, h, exempt, ask) in [
        (
            "resident",
            Household {
                prior: prior(DOLLAR * 900, true),
                extras: none,
                ..tax(DOLLAR * 10_000)
            },
            true,
            PositionKind::Unspecified,
        ),
        (
            "part-year",
            Household {
                prior: prior(DOLLAR * 900, false),
                extras: none,
                ..tax(DOLLAR * 10_000)
            },
            false,
            PositionKind::Unspecified,
        ),
        (
            "residence not given",
            Household {
                prior: Some(unanswered),
                extras: none,
                ..tax(DOLLAR * 10_000)
            },
            false,
            PositionKind::Residency,
        ),
        (
            "no prior return",
            Household {
                extras: none,
                ..tax(DOLLAR * 10_000)
            },
            false,
            PositionKind::Residency,
        ),
        (
            "liability not said",
            Household {
                prior: prior(DOLLAR * 900, true),
                extras: extras(&mut new_estimated_facts()),
                ..tax(DOLLAR * 10_000)
            },
            false,
            PositionKind::JurisdictionFact,
        ),
    ] {
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "{name}: {}",
            res.value().why()
        );
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}: {ask} is not asked");
        }
    }
    // Answered, nothing about federal liability is asked.
    let h = Household {
        prior: prior(DOLLAR * 900, true),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!has(&res.required(), PositionKind::JurisdictionFact));
}

/// RI-2210 line 19b: days × 12% × underpayment ÷ 365, on each piece of a
/// shortfall until it is paid. A payment goes to the oldest shortfall first.
#[test]
fn estimated_penalty_arithmetic() {
    // 80% of $10,000 is $8,000, a quarter $2,000. April is $1,000 short and
    // paid with June's: 61 days × 12% × $1,000 ÷ 365 = $20.05.
    let mut h = Household {
        payments: quarterly(&[
            DOLLAR * 1_000,
            DOLLAR * 3_000,
            DOLLAR * 2_000,
            DOLLAR * 2_000,
        ]),
        ..tax(DOLLAR * 10_000)
    };
    let p = penalty(&h, day(2027, 4, 15)).value();
    let lines = p.lines();
    assert!(
        lines.len() == 1
            && lines[0].installment() == 1
            && lines[0].shortfall() == DOLLAR * 1_000
            && lines[0].days() == 61
            && p.total() == USCents(2_005),
        "penalty {}",
        p.total()
    );
    let parts = lines[0].parts();
    assert!(parts.len() == 1 && parts[0].rate() == percent(12));
    // Half paid May 15, 30 days: $4.93; half with June's, 61 days: $10.03.
    let d = dues();
    h.payments = vec![
        pay(d[0], DOLLAR * 1_000),
        pay(day(2026, 5, 15), DOLLAR * 500),
        pay(d[1], DOLLAR * 2_500),
        pay(d[2], DOLLAR * 2_000),
        pay(d[3], DOLLAR * 2_000),
    ];
    let p = penalty(&h, day(2027, 4, 15)).value();
    let parts = p.lines()[0].parts();
    assert!(
        parts.len() == 2
            && parts[0].days() == 30
            && parts[0].charge() == USCents(493)
            && parts[1].days() == 61
            && parts[1].charge() == USCents(1_003)
            && p.total() == USCents(1_496)
    );
    // Figured to June 1, the shortfall has run 47 days: $15.45.
    h.payments = quarterly(&[
        DOLLAR * 1_000,
        DOLLAR * 3_000,
        DOLLAR * 2_000,
        DOLLAR * 2_000,
    ]);
    let p = penalty(&h, day(2026, 6, 1)).value();
    assert!(p.total() == USCents(1_545) && p.as_of() == day(2026, 6, 1));
}

/// The rate is set for each calendar year, and the RI-2210 splits the days
/// at January 1 (lines 19 and 20). Rhode Island's rate was 12% for 2025 and
/// is 12% for 2026, and the 2027 rate is not published, so no published
/// change falls inside the 2026 window: this checks the split at the year
/// end and the unpublished-rate gap instead.
#[test]
fn estimated_shortfall_spanning_the_year_end() {
    // September is $2,000 short. January's $2,000 pays it on January 15, and
    // January's own installment is paid February 1.
    let d = dues();
    let mut h = Household {
        payments: vec![
            pay(d[0], DOLLAR * 2_000),
            pay(d[1], DOLLAR * 2_000),
            pay(d[3], DOLLAR * 2_000),
            pay(day(2027, 2, 1), DOLLAR * 2_000),
        ],
        ..tax(DOLLAR * 10_000)
    };
    let res = penalty(&h, day(2027, 4, 15));
    let lines = res.value().lines();
    assert!(lines.len() == 2 && lines[0].installment() == 3 && lines[0].days() == 122);
    let parts = lines[0].parts();
    // September 15 to December 31: 107 days × 12% × $2,000 ÷ 365 = $70.36.
    assert!(
        parts.len() == 1
            && parts[0].to() == day(2026, 12, 31)
            && parts[0].days() == 107
            && parts[0].charge() == USCents(7_036)
    );
    assert!(lines[1].parts().is_empty() && res.value().total() == USCents(7_036));
    assert!(required_about(
        &res.required(),
        PositionKind::UnpublishedRate,
        "rate for the days after Dec 31, 2026 isn't published yet"
    ));
    // Paid on December 31, nothing runs into 2027.
    h.payments = vec![
        pay(d[0], DOLLAR * 2_000),
        pay(d[1], DOLLAR * 2_000),
        pay(day(2026, 12, 31), DOLLAR * 2_000),
        pay(d[3], DOLLAR * 2_000),
    ];
    let res = penalty(&h, day(2027, 4, 15));
    assert!(
        res.value().total() == USCents(7_036)
            && !has(&res.required(), PositionKind::UnpublishedRate)
    );
}

/// `n / d` printed to six places, rounded half away from zero, as Go's
/// `big.Rat.FloatString(6)` prints a positive fraction.
fn float_string6(n: i64, d: i64) -> String {
    let units = (2 * n * 1_000_000 + d) / (2 * d);
    format!("{}.{:06}", units / 1_000_000, units % 1_000_000)
}

/// The RI-2210A shortcut multiplies the underpayment by 7.9808% and charges
/// 0.000329 a day. Both come from the 12% rate: 12% × (365 + 304 + 212 +
/// 90) ÷ 365 ÷ 4, the days from each due date to April 15, and 12% ÷ 365.
/// The rules count the same days for a filer who paid nothing, and charge
/// the 2026 days at 12% ÷ 365.
#[test]
fn estimated_shortcut_factor_follows_from_the_rate() {
    let res = penalty(&tax(DOLLAR * 10_000), day(2027, 4, 15));
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    let want_days = [365, 304, 212, 90];
    // 260, 199 and 107 days of 2026.
    let want_2026 = [17_096, 13_085, 7_036, 0];
    let mut total = 0;
    for (i, l) in lines.iter().enumerate() {
        assert!(
            l.shortfall() == DOLLAR * 2_000 && l.days() == want_days[i],
            "line {}",
            i + 1
        );
        assert_eq!(l.total(), USCents(want_2026[i]), "line {}", i + 1);
        total += l.days();
    }
    let rate = i64::from(percent(12).0);
    assert_eq!(float_string6(rate * total, 10_000 * 365 * 4), "0.079808");
    assert_eq!(float_string6(rate, 10_000 * 365), "0.000329");
}

/// A return filed with the balance paid by January 31 counts in column D,
/// as paid by January 15. Between January 31 and the RI-1040ES's February
/// 15, moved to February 16 for Washington's Birthday, the reading is asked
/// for.
#[test]
fn estimated_january_return() {
    // September is $2,000 short, January unpaid.
    let base = Household {
        payments: quarterly(&[DOLLAR * 2_000, DOLLAR * 2_000]),
        ..tax(DOLLAR * 10_000)
    };
    let with = |filed: Date, reading: JanuaryReturn| -> Household {
        Household {
            return_filed: filed,
            extras: extras(
                new_estimated_facts()
                    .prior_year_no_federal_liability(false)
                    .january_return(reading),
            ),
            ..base.clone()
        }
    };
    for (filed, reading, met, asked) in [
        (day(2027, 1, 29), JanuaryReturn::Unanswered, true, false),
        (day(2027, 1, 31), JanuaryReturn::Unanswered, true, false),
        (day(2027, 2, 1), JanuaryReturn::Unanswered, false, true),
        (day(2027, 2, 16), JanuaryReturn::Unanswered, false, true),
        (day(2027, 2, 17), JanuaryReturn::Unanswered, false, false),
        (day(2027, 2, 1), JanuaryReturn::PenaltyForm, false, false),
        (day(2027, 2, 16), JanuaryReturn::Coupon, true, false),
        (day(2027, 2, 17), JanuaryReturn::Coupon, false, false),
    ] {
        let h = with(filed, reading);
        let sched = rules().schedule(&h.facts()).unwrap();
        assert_eq!(
            sched.value()[3].status() == InstallmentStatus::Excused,
            met,
            "filed {filed}, {reading}"
        );
        assert_eq!(
            has(&sched.required(), PositionKind::JurisdictionFact),
            asked,
            "filed {filed}, {reading}"
        );
        let p = penalty(&h, day(2027, 4, 15)).value();
        let want_as_of = if met { dues()[3] } else { filed };
        assert_eq!(p.as_of(), want_as_of, "filed {filed}, {reading}");
        if met {
            let l = p.lines();
            assert!(l.len() == 1 && l[0].installment() == 3 && l[0].days() == 122);
        }
    }
    // The question names both dates and the holiday that moves the later
    // one.
    let sched = rules()
        .schedule(&with(day(2027, 2, 1), JanuaryReturn::Unanswered).facts())
        .unwrap();
    assert!(required_about(
        &sched.required(),
        PositionKind::JurisdictionFact,
        "moves to Feb 16, 2027 for Washington's Birthday"
    ));
}

/// A quarter of 80% of the tax, paid with June's: 61 days at 12%.
fn late(t: USCents, e: Option<EstimatedFacts>) -> Household {
    let q = percent(20).of(t);
    let d = dues();
    Household {
        extras: e,
        payments: vec![pay(d[1], q * 2), pay(d[2], q), pay(d[3], q)],
        ..tax(t)
    }
}

/// § 44-30-84(a)(1) makes interest under $2.00 not payable. The RI-2210 has
/// no minimum, so a charge under $2 is shown and the reading asked for.
#[test]
fn estimated_two_dollar_minimum() {
    // $497.40: a quarter of $99.48, which costs $2.00.
    let res = penalty(&late(USCents(49_740), None), day(2027, 4, 15));
    assert!(
        res.value().total() == USCents(200)
            && !has(&res.required(), PositionKind::JurisdictionFact)
    );
    // $497.35: a quarter of $99.47, which costs $1.99.
    let res = penalty(&late(USCents(49_735), None), day(2027, 4, 15));
    assert!(
        res.value().total() == USCents(199)
            && required_about(
                &res.required(),
                PositionKind::JurisdictionFact,
                "under $2 isn't payable"
            )
    );
    let applied = extras(
        new_estimated_facts()
            .prior_year_no_federal_liability(false)
            .two_dollar_minimum(true),
    );
    let p = penalty(&late(USCents(49_735), applied), day(2027, 4, 15)).value();
    assert!(p.total() == USCents(0) && p.lines().is_empty());
    let not_applied = extras(
        new_estimated_facts()
            .prior_year_no_federal_liability(false)
            .two_dollar_minimum(false),
    );
    let p = penalty(&late(USCents(49_735), not_applied), day(2027, 4, 15));
    assert!(
        p.value().total() == USCents(199) && !has(&p.required(), PositionKind::JurisdictionFact)
    );
}

/// Withholding counts one-fourth on each date unless the filer shows when it
/// was withheld. Last year's overpayment counts on the first date.
#[test]
fn estimated_withholding() {
    let mut h = Household {
        withheld: DOLLAR * 4_000,
        payments: vec![
            estimated::new_payment()
                .amount(DOLLAR * 500)
                .from_prior_year()
                .build()
                .unwrap(),
        ],
        ..tax(DOLLAR * 10_000)
    };
    let even = rules().schedule(&h.facts()).unwrap().value();
    assert!(even[0].credited_by() == DOLLAR * 1_500 && even[3].credited_by() == DOLLAR * 4_500);
    h.as_withheld = true;
    let dated = rules().schedule(&h.facts()).unwrap().value();
    assert!(
        dated[0].credited_by() == DOLLAR * 500
            && dated[2].credited_by() == DOLLAR * 500
            && dated[3].credited_by() == DOLLAR * 4_500
    );
}

/// RI-2210 Part 5: 20%, 40%, 60% and 80% of the tax on annualised income,
/// less earlier installments, never more than the regular installment plus
/// what the method carried forward.
#[test]
fn estimated_annualised() {
    let mut h = Household {
        annualised: Some(vec![
            DOLLAR * 2_000,
            DOLLAR * 5_000,
            DOLLAR * 8_000,
            DOLLAR * 10_000,
        ]),
        ..tax(DOLLAR * 10_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    // 20% of $2,000 is $400; 40% of $5,000 less $400 is $1,600; 60% of
    // $8,000 less $2,000 is $2,800; 80% of $10,000 less $4,800 is $3,200.
    for (i, want) in [400, 2_000, 4_800, 8_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), DOLLAR * want, "installment {}", i + 1);
    }
    // With $9,000 for the year, the last is 80% of $9,000 less $4,800,
    // $2,400, under the $3,200 the regular method would carry.
    if let Some(a) = h.annualised.as_mut() {
        a[3] = DOLLAR * 9_000;
    }
    assert_eq!(
        rules().schedule(&h.facts()).unwrap().value()[3].needed_by(),
        DOLLAR * 7_200
    );
    h.period_ends = Some(vec![
        day(2026, 3, 31),
        day(2026, 6, 30),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]);
    assert!(
        rules().schedule(&h.facts()).is_err(),
        "a period ending June 30 is accepted"
    );
}

/// RI-2210 line 13 divides by four whenever the requirement arose; the
/// statute's late-start table is reported as a divergence.
#[test]
fn estimated_late_start_is_four_quarters() {
    let h = Household {
        first_met: day(2026, 7, 1),
        payments: quarterly(&[USCents(0), USCents(0), DOLLAR * 6_000, DOLLAR * 2_000]),
        ..tax(DOLLAR * 10_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    let s = sched.value();
    assert!(
        s[0].status() == InstallmentStatus::Short
            && s[1].status() == InstallmentStatus::Short
            && s[0].needed_by() == DOLLAR * 2_000
    );
    assert!(
        sched.applied().iter().any(|r| r
            .applied()
            .contains("the requirement first arose on July 1, 2026")),
        "the late start is not noted"
    );
}

/// The penalty stops on April 15, 2027, or on the day the return is filed
/// and paid.
#[test]
fn estimated_penalty_stops() {
    let mut h = tax(DOLLAR * 10_000);
    assert_eq!(
        penalty(&h, day(2027, 6, 30)).value().as_of(),
        day(2027, 4, 15)
    );
    h.return_filed = day(2027, 3, 1);
    assert_eq!(
        penalty(&h, day(2027, 4, 15)).value().as_of(),
        day(2027, 3, 1)
    );
}

/// Rhode Island's law adopts no casualty, retirement or disability waiver,
/// and its regulation lets the Division follow the federal ones. The charge
/// is shown before any.
#[test]
fn estimated_waiver() {
    let mut h = tax(DOLLAR * 10_000);
    let res = penalty(&h, day(2026, 12, 31));
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));
    h.waiver = Some(WaiverKind::Casualty);
    let asked = penalty(&h, day(2026, 12, 31));
    assert!(
        !has(&asked.required(), PositionKind::PenaltyWaiver)
            && asked.value().waiver() == WaiverKind::Casualty
            && asked.value().total() == res.value().total()
    );
}

/// The statute adopts only § 6654(e)(1) and (2), and the regulation says the
/// federal exceptions may be followed, so a waiver the federal rules would
/// allow is left to the Division, and the open point is reported.
#[test]
fn estimated_waiver_is_undecided() {
    for (name, pos) in [
        (
            "casualty",
            estimated::new_positions()
                .waiver(WaiverKind::Casualty)
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
    ] {
        let h = Household {
            positions: Some(pos),
            ..tax(DOLLAR * 10_000)
        };
        let res = penalty(&h, day(2026, 12, 31));
        let p = res.value();
        let o = p.waiver_outcome().unwrap();
        assert!(
            p.total().0 != 0 && o.undecided() && o.reason().contains("Division decides"),
            "{name}"
        );
        assert_eq!(p.if_waived(), p.total(), "{name}");
        assert!(
            required_about(
                &res.required(),
                PositionKind::UnsettledRule,
                "federal waiver"
            ),
            "{name}"
        );
    }
}

/// A waiver the federal rules refuse stays refused, whichever reading the
/// Division takes.
#[test]
fn estimated_waiver_outside_the_federal_window_is_refused() {
    let pos = estimated::new_positions()
        .waiver_for(WaiverCause::Retirement, day(2020, 6, 1))
        .born(day(1955, 3, 1))
        .build()
        .unwrap();
    let h = Household {
        positions: Some(pos),
        ..tax(DOLLAR * 10_000)
    };
    let res = penalty(&h, day(2026, 12, 31));
    let o = res.value().waiver_outcome().unwrap();
    assert!(
        o.refused()
            && o.reason().contains("2020")
            && !has(&res.required(), PositionKind::UnsettledRule)
    );
}

/// An exception sets the target, so last year's return is not asked for.
#[test]
fn estimated_exempt_does_not_ask_for_last_year() {
    let res = rules().requirement(&tax(DOLLAR * 200).facts()).unwrap();
    assert!(res.value().exempt() && !has(&res.required(), PositionKind::PriorYearReturn));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want = [
        (
            "The prior-year option",
            "100% of last year's Rhode Island tax",
            "§ 44-30-84(b)",
            "creates no prior-year option",
        ),
        (
            "The $250 floor",
            "$250 or less",
            "§§ 44-30-55(a), 44-30-84(b)",
            "$250 or the federal $1,000",
        ),
        (
            "No liability last year",
            "no federal tax liability",
            "§ 44-30-84(b)",
            "no Rhode Island liability",
        ),
        (
            "A return filed in place of the January installment",
            "on or before January 31",
            "§§ 44-30-55(e), 44-30-91(b), 25-1-1",
            "fifteenth day of the second month",
        ),
        (
            "A requirement first met after April 15",
            "divided by four",
            "§§ 44-30-55(c), 44-30-56(a)",
            "pay equal installments on June 15, September 15 and January 15",
        ),
        (
            "Interest under $2",
            "no minimum",
            "§ 44-30-84(a)(1)",
            "under $2 is not payable",
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
        // Every reading followed is the forms'. Nothing in these facts turns
        // on a divergence: no prior return, a balance over $1,000, no early
        // return, no late start.
        assert!(
            d.uses() == Reading::Form && !d.affects_household(),
            "{:?}",
            d.rule()
        );
    }
    // A late start is touched by the late-start reading.
    let h = Household {
        first_met: day(2026, 7, 1),
        ..tax(DOLLAR * 10_000)
    };
    for d in rules().requirement(&h.facts()).unwrap().divergences() {
        assert_eq!(
            d.affects_household(),
            d.rule() == "A requirement first met after April 15",
            "late start: {:?}",
            d.rule()
        );
    }
    // The $2 minimum applied follows the statute.
    let applied = extras(
        new_estimated_facts()
            .prior_year_no_federal_liability(false)
            .two_dollar_minimum(true),
    );
    for d in penalty(&late(USCents(49_735), applied), day(2027, 4, 15)).divergences() {
        if d.rule() == "Interest under $2" {
            assert!(d.uses() == Reading::Statute && d.affects_household());
        }
    }
}

/// Go also checks that an unknown January reading is refused; a Rust
/// [`JanuaryReturn`] cannot hold one, so that case is not ported.
#[test]
fn estimated_refuses_what_is_not_built() {
    let err = estimated_tax(2025).err().unwrap();
    assert!(err.is(&estimated::Error::NoRules), "2025: {err}");
    for (name, h) in [
        (
            "a farmer",
            Household {
                farm: Rate(7_000),
                ..tax(USCents(100))
            },
        ),
        (
            "a fisherman",
            Household {
                fish: Rate(7_000),
                ..tax(USCents(100))
            },
        ),
    ] {
        let err = rules().requirement(&h.facts()).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    for (name, f) in [
        (
            "Massachusetts's facts",
            estimated::new_facts(estimated::MASSACHUSETTS, 2026)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(RI, 2027)
                .filing_status(Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(RI, 2026)
                .filing_status(Status::Single)
                .this_year(this)
                .state("not Rhode Island's")
                .build()
                .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&tax(USCents(100)).facts(), Date::default())
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
        "$250",
        "80%",
        "100%",
        "12%",
        "April 15",
        "January 31",
        "February 16, 2027",
        "Rhode Island legal holiday",
        "no 110% tier",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.sources().is_empty() && !e.versus_federal().is_empty());
}

/// The worked example in README.md.
#[test]
fn estimated_readme_example() {
    let f = Household {
        withheld: DOLLAR * 2_000,
        prior: prior(DOLLAR * 4_400, true),
        payments: quarterly(&[DOLLAR * 500, DOLLAR * 500, DOLLAR * 500, DOLLAR * 500]),
        ..tax(DOLLAR * 6_000)
    }
    .facts();
    assert_eq!(
        rules().requirement(&f).unwrap().value().target(),
        DOLLAR * 4_400
    );
    let sched = rules().schedule(&f).unwrap().value();
    for (i, want) in [100, 200, 300, 400].into_iter().enumerate() {
        assert_eq!(sched[i].shortfall(), DOLLAR * want, "installment {}", i + 1);
    }
    // $100 for 61 days, $2.01; $200 for 92, $6.05; $300 for the 107 days of
    // 2026, $10.55. The 2027 days have no published rate.
    let res = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    assert!(
        res.value().total() == USCents(1_861)
            && has(&res.required(), PositionKind::UnpublishedRate)
    );
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
    let f = estimated::new_facts(RI, 2026)
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
