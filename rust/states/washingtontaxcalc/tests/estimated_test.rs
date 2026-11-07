//! Washington's capital gains tax payment rules for 2026, checked against
//! the state's own documents.
//!
//! Washington publishes no worked example with figures for these penalties.
//! The oracles are the statute's own arithmetic, a percentage of the late
//! tax and a yearly interest rate, and WAC 458-20-300 Examples 3 and 4,
//! which state a conclusion without a figure. The boundaries, the exceptions
//! and the open questions are hand-written cases. Go's
//! TestEstimatedFiguresAreInTheirSources and the testdata/ phrase checks in
//! the other tests are not ported.

#![allow(clippy::unwrap_used)]

use std::collections::HashMap;

use estimated::{
    Basis, Date, EstimatedTax, Facts, FactsBuilder, Month, PartKind, Payment, PenaltyPart,
    Positions, Status, WaiverCause, WaiverKind, WaiverOutcome, Weekday,
};
use money::{DOLLAR, USCents, percent};
use taxcalcs_washingtontaxcalc as washingtontaxcalc;
use working::{PositionKind, Reading, Required};

const WA: estimated::Jurisdiction = estimated::WASHINGTON;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn due() -> Date {
    day(2027, 4, 15)
}

fn rules() -> Box<dyn EstimatedTax> {
    washingtontaxcalc::estimated_tax(2026).unwrap()
}

fn asked(rs: &[Required], k: PositionKind, about: &str) -> bool {
    rs.iter().any(|r| {
        r.position() == k && r.asked() && format!("{} {}", r.question(), r.effect()).contains(about)
    })
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

#[derive(Clone, Default)]
struct Household {
    tax: USCents,
    no_tax: bool,
    payments: Vec<Payment>,
    /// `None` leaves it unanswered.
    extension: Option<bool>,
    return_filed: Date,
    /// `None` answers "no waiver".
    waiver: Option<WaiverKind>,
    /// Leaves the waiver unanswered.
    no_waiver: bool,
    /// Replaces `waiver` when set.
    positions: Option<Positions>,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

fn paying(t: USCents, payments: Vec<Payment>) -> Household {
    Household { payments, ..tax(t) }
}

impl Household {
    fn facts(&self) -> Facts {
        let figures = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let w = self.waiver.unwrap_or(WaiverKind::None);
        let mut b = estimated::new_facts(WA, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(figures)
            .paid(&self.payments)
            .return_filed(self.return_filed);
        if let Some(p) = self.positions {
            b.positions(p);
        } else if !self.no_waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        if let Some(e) = self.extension {
            b.state(
                washingtontaxcalc::new_estimated_facts()
                    .extension(e)
                    .build()
                    .unwrap(),
            );
        }
        b.build().unwrap()
    }
}

/// Figures the penalty and splits it into the late payment or low estimate
/// penalty, and interest.
fn penalty(h: &Household, as_of: Date) -> (USCents, USCents, working::Result<estimated::Penalty>) {
    let res = rules().penalty(&h.facts(), as_of).unwrap();
    let (mut flat, mut interest) = (USCents(0), USCents(0));
    for l in res.value().lines() {
        for p in l.parts() {
            match p.kind() {
                PartKind::Flat => flat += p.charge(),
                PartKind::Interest => interest += p.charge(),
                k => panic!("unexpected {k} part"),
            }
        }
    }
    assert_eq!(flat + interest, res.value().total());
    (flat, interest, res)
}

/// One option, the whole of this year's tax. There is no prior-year safe
/// harbor, and any tax at all, down to a cent, is due.
#[test]
fn estimated_requirement_is_the_whole_tax() {
    let r = rules()
        .requirement(&tax(DOLLAR * 10_000).facts())
        .unwrap()
        .value();
    assert!(r.required() && r.target() == DOLLAR * 10_000, "{}", r.why());
    let opts = r.options();
    assert!(
        opts.len() == 2
            && opts[0].basis() == Basis::ThisYearTax
            && opts[0].percent() == percent(100)
            && opts[0].available()
    );
    assert!(
        opts[1].basis() == Basis::PriorYearTax
            && !opts[1].available()
            && opts[1].why_not().contains("no prior-year safe harbor")
    );
    for s in [
        "no quarterly estimated payments",
        "$10,000",
        "Apr 15, 2027",
        "Oct 15",
    ] {
        assert!(
            r.why().contains(s),
            "the reason does not say {s:?}: {}",
            r.why()
        );
    }

    for (t, required) in [(USCents(0), false), (USCents(1), true)] {
        let r = rules().requirement(&tax(t).facts()).unwrap().value();
        assert!(r.required() == required && r.target() == t, "tax {t}");
        // Washington has no exception: even with no tax, the target is the
        // whole of this year's tax.
        let c = r.chosen().unwrap();
        assert!(
            !r.exempt()
                && c.basis() == Basis::ThisYearTax
                && c.amount() == t
                && c.label() == "100% of this year's tax",
            "tax {t}: {c:?}"
        );
    }
}

/// The 2026 tax is due April 15, 2027, a Thursday that is no holiday, and
/// payments from October 15, 2026 count toward it.
#[test]
fn estimated_schedule_is_one_payment() {
    assert_eq!(due().weekday(), Weekday::THURSDAY);
    let mut h = paying(
        DOLLAR * 10_000,
        vec![
            pay(day(2026, 10, 15), DOLLAR * 4_000),
            pay(due(), DOLLAR * 5_000),
            pay(due().add_days(1), DOLLAR * 1_000),
        ],
    );
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s.len(), 1);
    let inst = &s[0];
    assert!(
        inst.due() == due()
            && inst.needed_by() == DOLLAR * 10_000
            && inst.credited_by() == DOLLAR * 9_000
            && inst.shortfall() == DOLLAR * 1_000
            && inst.status() == Status::Short,
        "{inst:?}"
    );
    h.payments.truncate(2);
    h.payments.push(pay(due(), DOLLAR * 1_000));
    let inst = rules().schedule(&h.facts()).unwrap().value()[0];
    assert!(inst.status() == Status::Met && inst.shortfall().0 == 0);
    let inst = rules()
        .schedule(&Household::default().facts())
        .unwrap()
        .value()[0];
    assert_eq!(inst.status(), Status::NothingDue);
}

/// RCW 82.87.115(1) opens payment six months before the due date: October
/// 15, 2026 and not a day earlier.
#[test]
fn estimated_prepayment_opens_october_fifteenth() {
    let early = paying(DOLLAR * 100, vec![pay(day(2026, 10, 14), DOLLAR * 100)]);
    assert!(rules().requirement(&early.facts()).is_err());
    let on_time = paying(DOLLAR * 100, vec![pay(day(2026, 10, 15), DOLLAR * 100)]);
    assert!(rules().requirement(&on_time.facts()).is_ok());
}

/// The state legal holidays for a year, worked out here from the rules RCW
/// 1.16.050(1) and (5) state rather than read from the crate.
fn rcw1_16_050(year: i64) -> HashMap<Date, &'static str> {
    let nth = |m: i64, wd: Weekday, n: i64| {
        let mut d = day(year, m, 1);
        while d.weekday() != wd {
            d = d.add_days(1);
        }
        d.add_days(7 * (n - 1))
    };
    let mut last_monday = day(year, 5, 31);
    while last_monday.weekday() != Weekday::MONDAY {
        last_monday = last_monday.add_days(-1);
    }
    let observed = |d: Date| match d.weekday() {
        Weekday::SUNDAY => d.add_days(1),
        Weekday::SATURDAY => d.add_days(-1),
        _ => d,
    };
    let thanksgiving = nth(11, Weekday::THURSDAY, 4);
    HashMap::from([
        (observed(day(year, 1, 1)), "New Year's Day"),
        (nth(1, Weekday::MONDAY, 3), "Martin Luther King, Jr."),
        (nth(2, Weekday::MONDAY, 3), "Presidents' Day"),
        (last_monday, "Memorial Day"),
        (observed(day(year, 6, 19)), "Juneteenth"),
        (observed(day(year, 7, 4)), "Independence Day"),
        (nth(9, Weekday::MONDAY, 1), "Labor Day"),
        (observed(day(year, 11, 11)), "Veterans Day"),
        (thanksgiving, "Thanksgiving Day"),
        (thanksgiving.add_days(1), "Native American Heritage Day"),
        (observed(day(year, 12, 25)), "Christmas Day"),
    ])
}

/// Of the 2027 holidays, Memorial Day alone falls on a date the rules use:
/// May 31, the last day of the month after the due date. The step to 19%
/// moves to June 1, so a payment that day is still at 9%, and the rules say
/// that reading is an open question.
#[test]
fn estimated_memorial_day_moves_the_second_step() {
    let holidays = rcw1_16_050(2027);
    for d in [due(), day(2027, 5, 31), day(2027, 6, 30)] {
        let name = holidays.get(&d);
        assert_eq!(name.is_some(), d == day(2027, 5, 31), "{d}");
        if let Some(n) = name {
            assert_eq!(*n, "Memorial Day");
        }
    }
    for (paid, want, want_asked) in [
        (day(2027, 5, 31), DOLLAR * 900, false),
        (day(2027, 6, 1), DOLLAR * 900, true),
        (day(2027, 6, 2), DOLLAR * 1_900, false),
    ] {
        let h = paying(DOLLAR * 10_000, vec![pay(paid, DOLLAR * 10_000)]);
        let (flat, _, res) = penalty(&h, day(2027, 12, 31));
        assert_eq!(flat, want, "paid {paid}");
        assert_eq!(
            asked(
                &res.required(),
                PositionKind::UnsettledRule,
                "is a legal holiday"
            ),
            want_asked,
            "paid {paid}"
        );
    }
}

/// 9% the day after the due date, 19% after June 1 and 29% after June 30,
/// on $10,000 never paid. Interest starts only after April 30.
#[test]
fn estimated_late_payment_steps() {
    for (as_of, want) in [
        (due(), USCents(0)),
        (due().add_days(1), DOLLAR * 900),
        (day(2027, 6, 1), DOLLAR * 900),
        (day(2027, 6, 2), DOLLAR * 1_900),
        (day(2027, 6, 30), DOLLAR * 1_900),
        (day(2027, 7, 1), DOLLAR * 2_900),
        (day(2027, 12, 31), DOLLAR * 2_900),
    ] {
        let (flat, _, res) = penalty(&tax(DOLLAR * 10_000), as_of);
        assert_eq!(flat, want, "to {as_of}");
        if want.0 == 0 {
            assert!(res.value().lines().is_empty(), "to {as_of}");
        }
    }
}

/// RCW 82.32.090(1): no late payment penalty is less than $5.
#[test]
fn estimated_late_payment_minimum() {
    for (t, want) in [(5_544, 500), (5_556, 500), (5_567, 501)] {
        let (flat, _, _) = penalty(&tax(USCents(t)), due().add_days(1));
        assert_eq!(flat, USCents(want), "{t} late");
    }
}

/// $6,000 of a $10,000 tax paid May 10 and the rest never paid. 9% of
/// $10,000, then 10% and 10% more of the $4,000 still out: $1,700.
#[test]
fn estimated_part_payment_between_steps() {
    let h = paying(DOLLAR * 10_000, vec![pay(day(2027, 5, 10), DOLLAR * 6_000)]);
    let (flat, interest, res) = penalty(&h, day(2027, 7, 1));
    assert_eq!(flat, DOLLAR * 1_700);
    // $6,000 for the 10 days to May 10, $9.86; $4,000 for the 62 days to
    // July 1, $40.77; both at 6% over 365 days.
    assert_eq!(interest, USCents(986 + 4_077));
    assert!(asked(
        &res.required(),
        PositionKind::UnsettledRule,
        "part payment"
    ));
    let (_, _, res) = penalty(&tax(DOLLAR * 10_000), day(2027, 7, 1));
    assert!(!asked(
        &res.required(),
        PositionKind::UnsettledRule,
        "part payment"
    ));
}

/// RCW 82.32.050(1)(c)(ii): interest from the last day of April, by the day.
#[test]
fn estimated_interest_starts_after_april_thirty() {
    for (paid, want) in [(day(2027, 4, 30), 0), (day(2027, 5, 1), 164)] {
        let h = paying(DOLLAR * 10_000, vec![pay(paid, DOLLAR * 10_000)]);
        let (_, interest, res) = penalty(&h, day(2027, 12, 31));
        assert_eq!(interest, USCents(want), "paid {paid}");
        assert_eq!(
            asked(
                &res.required(),
                PositionKind::UnsettledRule,
                "how days are counted"
            ),
            want > 0,
            "paid {paid}"
        );
    }
}

/// The interest stops at December 31, 2027 and says so. $10,000 for the 245
/// days from April 30 is $402.74.
#[test]
fn estimated_interest_stops_where_the_rates_end() {
    for as_of in [day(2027, 12, 31), day(2028, 3, 31)] {
        let (flat, interest, res) = penalty(&tax(DOLLAR * 10_000), as_of);
        assert!(
            flat == DOLLAR * 2_900 && interest == USCents(40_274),
            "to {as_of}"
        );
        let parts: Vec<PenaltyPart> = res.value().lines()[0]
            .parts()
            .into_iter()
            .filter(|p| p.kind() == PartKind::Interest)
            .collect();
        assert!(
            parts.len() == 1
                && parts[0].to() == day(2027, 12, 31)
                && parts[0].days() == 245
                && parts[0].rate() == percent(6),
            "to {as_of}: {parts:?}"
        );
        assert_eq!(
            asked(
                &res.required(),
                PositionKind::UnpublishedRate,
                "Dec 31, 2027"
            ),
            as_of.year() == 2028
        );
    }
}

/// With an extension, an estimate paid by April 15 avoids the late payment
/// penalty. Paying 80% of the tax avoids the 5% penalty too; a cent less
/// owes 5% of the whole $10,000.
#[test]
fn estimated_extension_with_a_timely_estimate() {
    let rest = |estimate: USCents| Household {
        extension: Some(true),
        return_filed: day(2027, 10, 15),
        ..paying(DOLLAR * 10_000, vec![pay(due(), estimate)])
    };
    for (estimate, want_flat, want_interest) in [
        (DOLLAR * 8_000, USCents(0), USCents(5_523)),
        (USCents(799_999), DOLLAR * 500, USCents(5_523)),
    ] {
        let (flat, interest, res) = penalty(&rest(estimate), day(2027, 12, 31));
        assert_eq!((flat, interest), (want_flat, want_interest), "{estimate}");
        assert_eq!(res.value().as_of(), day(2027, 10, 15));
        assert!(!asked(
            &res.required(),
            PositionKind::JurisdictionFact,
            "extension"
        ));
    }

    // Without an extension the same payments are late: $580.
    let mut h = rest(DOLLAR * 8_000);
    h.extension = Some(false);
    assert_eq!(penalty(&h, day(2027, 12, 31)).0, DOLLAR * 580);
    h.extension = None;
    let (flat, _, res) = penalty(&h, day(2027, 12, 31));
    assert!(
        flat == DOLLAR * 580 && asked(&res.required(), PositionKind::JurisdictionFact, "extension")
    );
    // An extension with nothing paid by April 15 is late in full.
    let h = Household {
        extension: Some(true),
        ..tax(DOLLAR * 10_000)
    };
    assert_eq!(penalty(&h, day(2027, 12, 31)).0, DOLLAR * 2_900);
}

/// The 5% penalty needs the estimate to be $1,000 or more short, read as the
/// shortfall. Where the two readings of "at least $1,000" part, the rules
/// ask.
#[test]
fn estimated_thousand_dollar_floor() {
    for (t, estimate, want, want_asked) in [
        (DOLLAR * 4_000, DOLLAR * 3_000, DOLLAR * 200, false),
        (DOLLAR * 4_000, USCents(300_001), USCents(0), true),
        (DOLLAR * 5_000, DOLLAR * 500, DOLLAR * 250, true),
        (DOLLAR * 1_200, DOLLAR * 900, USCents(0), false),
    ] {
        let h = Household {
            extension: Some(true),
            ..paying(t, vec![pay(due(), estimate)])
        };
        let (flat, _, res) = penalty(&h, day(2027, 4, 20));
        assert_eq!(flat, want, "{estimate} of {t}");
        assert_eq!(
            asked(
                &res.required(),
                PositionKind::UnsettledRule,
                "at least $1,000"
            ),
            want_asked,
            "{estimate} of {t}"
        );
    }
}

/// WAC 458-20-300 Examples 3 and 4, moved to 2026's dates.
#[test]
fn estimated_wac_458_20_300_examples() {
    let jeannette = Household {
        extension: Some(false),
        ..paying(
            DOLLAR * 10_000,
            vec![pay(day(2027, 4, 20), DOLLAR * 10_000)],
        )
    };
    let (flat, interest, _) = penalty(&jeannette, day(2027, 12, 31));
    assert!(flat == DOLLAR * 900 && interest.0 == 0);
    let gil = Household {
        extension: Some(true),
        return_filed: day(2027, 10, 13),
        ..paying(
            DOLLAR * 10_000,
            vec![pay(day(2027, 10, 13), DOLLAR * 10_000)],
        )
    };
    // 166 days from April 30 to October 13 on $10,000 at 6%: $272.88.
    let (flat, interest, _) = penalty(&gil, day(2027, 12, 31));
    assert!(flat == DOLLAR * 2_900 && interest == USCents(27_288));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want: HashMap<&str, [&str; 4]> = HashMap::from([
        (
            "Late payment penalty after a timely estimated payment",
            [
                "no late payment penalty",
                "Interim statement",
                "apply to the balance",
                "RCW 82.87.110(3)",
            ],
        ),
        (
            "The penalty on a low estimated payment",
            [
                "assessment penalty",
                "April 7, 2023",
                "5% of the actual tax due",
                "July 27, 2025",
            ],
        ),
        (
            "When interest starts",
            [
                "Jan 31",
                "2/29/2024",
                "last day of the next April",
                "July 27, 2025",
            ],
        ),
    ]);
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let w = want[d.rule().as_str()];
        assert!(
            d.form().contains(w[0])
                && d.form_source().contains(w[1])
                && d.statute().contains(w[2])
                && d.statute_source().contains(w[3]),
            "{:?} does not pin both readings",
            d.rule()
        );
    }

    let uses: HashMap<&str, Reading> = HashMap::from([
        (
            "Late payment penalty after a timely estimated payment",
            Reading::Form,
        ),
        ("The penalty on a low estimated payment", Reading::Statute),
        ("When interest starts", Reading::Statute),
    ]);
    let ext = |p: USCents| Household {
        extension: Some(true),
        ..paying(DOLLAR * 10_000, vec![pay(due(), p)])
    };
    for (name, h, affects) in [
        ("nothing paid", tax(DOLLAR * 10_000), [false, false, true]),
        ("paid in full", ext(DOLLAR * 10_000), [false, false, false]),
        ("extension, 90%", ext(DOLLAR * 9_000), [true, false, true]),
        ("extension, 70%", ext(DOLLAR * 7_000), [true, true, true]),
    ] {
        let ds = rules().requirement(&h.facts()).unwrap().divergences();
        for (i, d) in ds.iter().enumerate() {
            assert!(
                d.uses() == uses[d.rule().as_str()] && d.affects_household() == affects[i],
                "{name}: {:?}",
                d.rule()
            );
        }
    }

    // The figures follow the department on the first and the statute on the
    // other two.
    let h = Household {
        extension: Some(true),
        ..paying(
            DOLLAR * 10_000,
            vec![
                pay(due(), DOLLAR * 9_000),
                pay(day(2027, 4, 30), DOLLAR * 1_000),
            ],
        )
    };
    let (flat, interest, _) = penalty(&h, day(2027, 12, 31));
    assert!(flat.0 == 0 && interest.0 == 0);
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for year in [2025, 2027] {
        let err = washingtontaxcalc::estimated_tax(year).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{year}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let base = |j: estimated::Jurisdiction, year: i64| -> FactsBuilder {
        let mut b = estimated::new_facts(j, year);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    let prior = estimated::new_payment()
        .from_prior_year()
        .amount(USCents(100))
        .build()
        .unwrap();
    let err = rules()
        .requirement(&base(WA, 2026).paid(&[prior]).build().unwrap())
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::Unsupported), "{err}");
    let periods: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 5, 31),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .into_iter()
    .map(|d| estimated::new_period().end(d).build().unwrap())
    .collect();
    let cases = [
        (
            "Pennsylvania's facts",
            base(estimated::PENNSYLVANIA, 2026).build().unwrap(),
        ),
        ("2027", base(WA, 2027).build().unwrap()),
        (
            "other extras",
            base(WA, 2026).state("not Washington's").build().unwrap(),
        ),
        (
            "withholding",
            base(WA, 2026)
                .withheld(&[estimated::new_withheld()
                    .on(day(2026, 6, 1))
                    .amount(USCents(100))
                    .build()
                    .unwrap()])
                .build()
                .unwrap(),
        ),
        (
            "annualised",
            base(WA, 2026)
                .elections(estimated::new_elections().annualised().build().unwrap())
                .periods(&periods)
                .build()
                .unwrap(),
        ),
    ];
    for (name, f) in cases {
        let err = rules().requirement(&f).err();
        assert!(
            err.as_ref()
                .is_some_and(|e| !e.is(&estimated::Error::Unsupported)),
            "{name}: {err:?}, want a refusal"
        );
    }
    // Washington has no separate rules for farmers.
    let f = base(WA, 2026)
        .farm_share(money::Rate(10_000))
        .build()
        .unwrap();
    assert!(rules().requirement(&f).is_ok());
}

/// Without this year's tax there is no target: Washington has no prior-year
/// figure to fall back on.
#[test]
fn estimated_without_this_years_tax() {
    let f = Household {
        no_tax: true,
        ..Household::default()
    }
    .facts();
    let res = rules().requirement(&f).unwrap();
    let r = res.value();
    assert!(!r.target_known() && !r.required() && r.target().0 == 0);
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(
        asked(&res.required(), PositionKind::ThisYearTax, "")
            && asked(&res.required(), PositionKind::JurisdictionFact, "extension")
    );
    let err = rules().schedule(&f).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
    let err = rules().penalty(&f, day(2027, 12, 31)).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
}

/// The penalty is shown before any waiver.
#[test]
fn estimated_waiver_is_reported_not_applied() {
    let h = Household {
        no_waiver: true,
        ..tax(DOLLAR * 10_000)
    };
    let (_, _, res) = penalty(&h, day(2027, 5, 1));
    assert!(asked(
        &res.required(),
        PositionKind::PenaltyWaiver,
        "five on-time years"
    ));
    let h = Household {
        waiver: Some(WaiverKind::Casualty),
        ..tax(DOLLAR * 10_000)
    };
    let (_, _, waived) = penalty(&h, day(2027, 5, 1));
    assert!(
        waived.value().total() == res.value().total()
            && waived.value().waiver() == WaiverKind::Casualty
    );
}

/// RCW 82.32.105(1) and WAC 458-20-228(9)(a): the department must waive the
/// penalties for circumstances beyond the filer's control.
#[test]
fn estimated_waiver_outcomes() {
    let mut h = tax(DOLLAR * 10_000);
    let plain = rules().penalty(&h.facts(), day(2027, 5, 1)).unwrap();
    let disaster = estimated::new_positions()
        .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
        .build()
        .unwrap();
    let retired = estimated::new_positions()
        .waiver_for(WaiverCause::Retirement, day(2026, 6, 1))
        .born(day(1962, 3, 1))
        .build()
        .unwrap();
    let disabled = estimated::new_positions()
        .waiver_for(WaiverCause::Disability, day(2026, 6, 1))
        .build()
        .unwrap();
    type Want = fn(&WaiverOutcome) -> bool;
    let cases: [(&str, Positions, Want, &str); 3] = [
        (
            "a disaster",
            disaster,
            WaiverOutcome::eligible,
            "beyond your control",
        ),
        (
            "retired in 2026 at 64",
            retired,
            WaiverOutcome::refused,
            "no waiver for retirement",
        ),
        (
            "disabled in 2026",
            disabled,
            WaiverOutcome::undecided,
            "no waiver for disability as such",
        ),
    ];
    for (name, pos, want, phrase) in cases {
        h.positions = Some(pos);
        let res = rules().penalty(&h.facts(), day(2027, 5, 1)).unwrap();
        let o = res.value().waiver_outcome().unwrap();
        assert!(want(&o) && o.reason().contains(phrase), "{name}: {o:?}");
        assert!(
            res.value().total() == plain.value().total() && plain.value().total().0 != 0,
            "{name}"
        );
        if name == "a disaster" {
            let interest: USCents = res
                .value()
                .lines()
                .iter()
                .flat_map(|l| l.parts())
                .filter(|p| p.kind() == PartKind::Interest)
                .map(|p| p.charge())
                .sum();
            assert!(
                res.value().if_waived() == interest
                    && interest.0 != 0
                    && interest != res.value().total()
            );
        }
        let got = res.required().iter().any(|r| {
            r.position() == PositionKind::UnsettledRule && r.effect().contains("disability")
        });
        assert_eq!(got, name == "disabled in 2026", "{name}");
    }
}

/// A return filed with the balance paid stops the penalty that day.
#[test]
fn estimated_return_filed_stops_the_penalty() {
    let h = Household {
        extension: Some(false),
        return_filed: day(2027, 5, 10),
        ..tax(DOLLAR * 10_000)
    };
    let (flat, interest, res) = penalty(&h, day(2027, 12, 31));
    assert!(
        res.value().as_of() == day(2027, 5, 10)
            && flat == DOLLAR * 900
            && interest == USCents(1_644)
    );
    assert!(res.applied().iter().any(|r| r.cite() == "RCW 82.87.110(6)"));
}

#[test]
fn estimated_explanation_names_the_rules() {
    let e = rules().explanation();
    let text = format!("{} {}", e.summary(), e.body().join(" "));
    for s in [
        "no quarterly estimated payments",
        "April 15, 2027",
        "October 15, 2026",
        "9%",
        "19%",
        "29%",
        "$5",
        "80%",
        "$1,000",
        "5% of the whole tax",
        "April 30, 2027",
        "6%",
        "five years",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(e.versus_federal().contains("no quarterly payments") && !e.sources().is_empty());
}

/// Paying more than the tax never reads as a negative amount.
#[test]
fn estimated_overpayment_has_no_negative_amount() {
    let h = paying(DOLLAR * 3_000, vec![pay(day(2027, 4, 1), DOLLAR * 3_600)]);
    let req = rules().requirement(&h.facts()).unwrap();
    let (_, _, pen) = penalty(&h, day(2027, 12, 31));
    let mut texts = vec![req.value().why(), pen.value().reason()];
    for o in req.value().options() {
        texts.push(o.label());
        texts.push(o.why_not());
    }
    for q in req.required().iter().chain(pen.required().iter()) {
        texts.push(q.because());
        texts.push(q.question());
        texts.push(q.effect());
    }
    for s in texts {
        assert!(
            !s.contains("−$") && !s.contains("-$"),
            "a negative amount: {s:?}"
        );
    }
}
