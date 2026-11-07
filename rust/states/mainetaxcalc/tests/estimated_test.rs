//! Maine's estimated tax rules for 2026, checked against hand-worked cases of
//! Form 2210ME's arithmetic. Ported from estimated_test.go; the Go test that
//! finds each figure in its extract in testdata/ reads the Go package's
//! citations and is not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status, WaiverCause,
    WaiverKind, Weekday,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_mainetaxcalc as mainetaxcalc;
use working::{PositionKind, Reading, Required};

const ME: estimated::Jurisdiction = estimated::MAINE;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    mainetaxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

fn count(rs: &[Required], k: PositionKind) -> usize {
    rs.iter().filter(|r| r.position() == k).count()
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
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    /// `None`: no unusual event.
    extras: Option<mainetaxcalc::EstimatedFacts>,
    no_extras: bool,
    return_filed: Date,
    annualised: Option<Vec<USCents>>,
    as_withheld: bool,
    waiver: Option<WaiverKind>,
    positions: Option<Positions>,
}

impl Household {
    fn facts(&self) -> Facts {
        let mut fig = estimated::new_figures();
        if !self.no_tax {
            fig.tax(self.tax);
        }
        let mut b = estimated::new_facts(ME, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(fig.build().unwrap())
            .paid(&self.payments)
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
        } else if !self.no_extras {
            b.state(
                mainetaxcalc::new_estimated_facts()
                    .no_unusual_event()
                    .build()
                    .unwrap(),
            );
        }
        let mut el = estimated::new_elections();
        if let Some(ann) = &self.annualised {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            let ps: Vec<_> = ann
                .iter()
                .enumerate()
                .map(|(i, &tax)| {
                    estimated::new_period()
                        .end(ends[i])
                        .annualised_tax(tax)
                        .build()
                        .unwrap()
                })
                .collect();
            b.periods(&ps);
            el.annualised();
        }
        if self.as_withheld {
            el.withholding_as_withheld();
        }
        b.elections(el.build().unwrap());
        if let Some(k) = self.waiver {
            b.positions(estimated::new_positions().waiver(k).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// Last year's Maine liability, Form 2210ME line 5, for a 12-month year.
fn last(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .build()
            .unwrap(),
    )
}

/// The household most tests start from: $4,444.44 of tax, so 90% is $4,000
/// and $1,000 a quarter, and last year's $10,000 is no lower.
fn base() -> Household {
    Household {
        tax: USCents(444_444),
        prior: last(DOLLAR * 10_000),
        ..Household::default()
    }
}

// The 2026 dates are the ones Form 1040ES-ME prints. Each is a weekday and
// none is a Maine legal holiday under 4 M.R.S. § 1051 (Patriot's Day is
// April 20, 2026), so none moves.
#[test]
fn due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&base().facts()).unwrap();
    for (i, s) in res.value().iter().enumerate() {
        assert_eq!(s.due(), dues()[i]);
        let wd = s.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
        assert_eq!(s.needed_by(), DOLLAR * 1_000 * (i as i64 + 1));
    }
}

// The two $1,000 tests, one cent either side. This year's is tax less
// withholding; last year's is its liability.
#[test]
fn threshold_boundaries() {
    for (name, withheld, prior, exempt) in [
        ("this year $999.99", 400_001, 1_000_000, true),
        ("this year $1,000.00", 400_000, 1_000_000, false),
        ("last year $999.99", 0, 99_999, true),
        ("last year $1,000.00", 0, 100_000, false),
    ] {
        let hh = Household {
            tax: DOLLAR * 5_000,
            withheld: USCents(withheld),
            prior: last(USCents(prior)),
            ..Household::default()
        };
        let r = rules().requirement(&hh.facts()).unwrap().value();
        assert_eq!(r.exempt(), exempt, "{name}: {}", r.why());
        if exempt {
            let c = r.chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && c.label().ends_with(" under $1,000"),
                "{name}: {c:?}"
            );
            let p = rules()
                .penalty(&hh.facts(), day(2027, 4, 15))
                .unwrap()
                .value();
            assert_eq!(p.total(), USCents(0), "{name}");
        }
    }
}

// The target is the smaller of 90% of this year's tax and 100% of last
// year's, and last year's needs a 12-month year with its liability.
#[test]
fn target_options() {
    let r = rules().requirement(&base().facts()).unwrap().value();
    assert!(r.chosen().unwrap().basis() == Basis::ThisYearTax && r.target() == DOLLAR * 4_000);
    let hh = Household {
        prior: last(DOLLAR * 3_000),
        ..base()
    };
    let r = rules().requirement(&hh.facts()).unwrap().value();
    let c = r.chosen().unwrap();
    assert!(
        c.basis() == Basis::PriorYearTax
            && c.percent() == percent(100)
            && r.target() == DOLLAR * 3_000
    );

    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(estimated::new_figures().tax(DOLLAR * 500).build().unwrap())
        .build()
        .unwrap();
    let no_figures = estimated::new_prior_return().build().unwrap();
    for (name, p) in [
        ("no return on record", None),
        ("no liability given", Some(no_figures)),
        ("a short year", Some(short)),
    ] {
        let hh = Household { prior: p, ..base() };
        let res = rules().requirement(&hh.facts()).unwrap();
        let o = res.value().options()[1].clone();
        assert!(!o.available() && !o.why_not().is_empty(), "{name}");
        assert!(!res.value().exempt(), "{name}");
        assert!(
            has(&res.required(), PositionKind::PriorYearReturn),
            "{name}"
        );
    }
}

// An unusual event is income not withheld that is at least $500,000 above
// the same period of last year. 90% of the tax on it is due with that
// period's installment, and last year's liability does not cover it.
#[test]
fn unusual_event() {
    let event = |increase: i64, tax: USCents, pass_through: Option<bool>| {
        let mut b = mainetaxcalc::new_estimated_facts();
        b.income_not_withheld(1, DOLLAR * 50_000, DOLLAR * 50_000)
            .income_not_withheld(2, DOLLAR * 100_000 + USCents(increase), DOLLAR * 100_000)
            .income_not_withheld(3, USCents(0), USCents(0))
            .income_not_withheld(4, USCents(0), USCents(0));
        if tax.0 > 0 {
            b.unusual_event_tax(2, tax);
        }
        if let Some(p) = pass_through {
            b.pass_through_waiver(p);
        }
        Some(b.build().unwrap())
    };
    // $100,000 of tax this year and $20,000 last year; $42,000 of tax on the
    // June period's excess, 90% of which is $37,800.
    let mut hh = Household {
        tax: DOLLAR * 100_000,
        prior: last(DOLLAR * 20_000),
        ..Household::default()
    };

    hh.extras = event(49_999_999, DOLLAR * 42_000, Some(false));
    let r = rules().requirement(&hh.facts()).unwrap().value();
    assert_eq!(r.target(), DOLLAR * 20_000);

    hh.extras = event(50_000_000, DOLLAR * 42_000, Some(false));
    let r = rules().requirement(&hh.facts()).unwrap().value();
    assert!(r.chosen().unwrap().basis() == Basis::PriorYearTax && r.target() == USCents(5_780_000));
    let sched = rules().schedule(&hh.facts()).unwrap();
    for (i, want) in [500_000, 4_780_000, 5_280_000, 5_780_000]
        .into_iter()
        .enumerate()
    {
        assert_eq!(
            sched.value()[i].needed_by(),
            USCents(want),
            "installment {}",
            i + 1
        );
    }
    assert!(!has(&sched.required(), PositionKind::PenaltyWaiver));

    // Under $1,000 last year waives everything but the unusual event.
    hh.prior = last(DOLLAR * 900);
    let r = rules().requirement(&hh.facts()).unwrap().value();
    assert!(!r.exempt() && r.target() == USCents(3_780_000));
    let s = rules().schedule(&hh.facts()).unwrap().value();
    assert!(
        s[0].status() == Status::NothingDue
            && s[1].needed_by() == USCents(3_780_000)
            && s[3].needed_by() == USCents(3_780_000)
    );

    // The four pass-through conditions take the event out.
    hh.extras = event(50_000_000, DOLLAR * 42_000, Some(true));
    let r = rules().requirement(&hh.facts()).unwrap().value();
    assert!(r.exempt(), "{}", r.why());

    // Unanswered pass-through conditions and a missing tax are both asked.
    hh.prior = last(DOLLAR * 20_000);
    hh.extras = event(50_000_000, USCents(0), None);
    let res = rules().requirement(&hh.facts()).unwrap();
    assert!(
        has(&res.required(), PositionKind::PenaltyWaiver)
            && count(&res.required(), PositionKind::JurisdictionFact) == 1
            && res.value().target() == DOLLAR * 20_000,
        "{:?}",
        res.required()
    );

    // Nothing said at all: asked, and no event assumed.
    hh.extras = None;
    hh.no_extras = true;
    let res = rules().requirement(&hh.facts()).unwrap();
    assert_eq!(count(&res.required(), PositionKind::JurisdictionFact), 1);

    // More unusual-event tax than this year's tax is refused.
    hh.no_extras = false;
    hh.extras = event(50_000_000, DOLLAR * 100_000 + USCents(1), Some(false));
    assert!(rules().requirement(&hh.facts()).is_err());
}

// The penalty is .75% a month, a part of a month a whole one, compounded
// monthly. $1,000 short on April 15: one day or one month is $7.50; one
// month and a day is $15.06 ($1,000 × (1.0075² − 1) = $15.05625).
#[test]
fn penalty_compounds_by_the_month() {
    let f = base().facts();
    for (as_of, months, charge) in [
        (day(2026, 4, 16), 1, 750),
        (day(2026, 5, 15), 1, 750),
        (day(2026, 5, 16), 2, 1_506),
        (day(2026, 6, 14), 2, 1_506),
    ] {
        let p = rules().penalty(&f, as_of).unwrap().value();
        let parts = p.lines()[0].parts();
        assert!(
            parts.len() == 1
                && parts[0].months() == months
                && parts[0].charge() == USCents(charge)
                && parts[0].rate() == percent(9),
            "to {as_of}: {parts:?}"
        );
    }
    // To July 10: April's three months, $22.67, and June's one, $7.50.
    assert_eq!(
        rules()
            .penalty(&f, day(2026, 7, 10))
            .unwrap()
            .value()
            .total(),
        USCents(3_017)
    );
    // $100,000 short for nine months: $6,956.08 to the cent.
    let big = Household {
        tax: USCents(44_444_444),
        prior: last(DOLLAR * 1_000_000),
        ..Household::default()
    }
    .facts();
    let l = rules()
        .penalty(&big, day(2026, 12, 31))
        .unwrap()
        .value()
        .lines()[0]
        .clone();
    assert_eq!(l.total(), USCents(695_608));
    // A payment pays the oldest shortfall first and stops its interest.
    let paid = Household {
        payments: vec![pay(day(2026, 5, 1), DOLLAR * 1_000)],
        ..base()
    };
    let p = rules()
        .penalty(&paid.facts(), day(2026, 6, 15))
        .unwrap()
        .value();
    assert!(p.lines()[0].total() == USCents(750) && p.total() == USCents(750));
}

// The penalty across the end of 2026. A month that begins in 2026 is charged
// at 2026's rate. To April 15, 2027 the penalty is what it is to December
// 31, 2026, and the months after are reported.
#[test]
fn penalty_across_the_year_end() {
    let f = base().facts();
    // April 9 months, $69.56; June 7, $53.70; September 4, $30.34.
    let want = [6_956, 5_370, 3_034];
    for as_of in [day(2026, 12, 31), day(2027, 4, 15)] {
        let res = rules().penalty(&f, as_of).unwrap();
        let lines = res.value().lines();
        for (i, w) in want.into_iter().enumerate() {
            assert_eq!(
                lines[i].total(),
                USCents(w),
                "to {as_of}, installment {}",
                i + 1
            );
        }
        assert!(lines.len() == 4 && lines[3].total().0 == 0, "to {as_of}");
        assert_eq!(
            has(&res.required(), PositionKind::UnpublishedRate),
            as_of.year() == 2027
        );
        assert_eq!(res.value().total(), USCents(15_360));
    }
    // Never past the return's due date.
    assert_eq!(
        rules()
            .penalty(&f, day(2027, 6, 30))
            .unwrap()
            .value()
            .as_of(),
        day(2027, 4, 15)
    );
}

// § 5228(9): no penalty on the fourth installment when the return is filed
// and paid in full by the last day of January. January 31, 2027 is a Sunday,
// and § 153(2) moves it to Monday, February 1.
#[test]
fn return_filed_and_paid() {
    for (filed, excused) in [
        (day(2027, 1, 29), true),
        (day(2027, 2, 1), true),
        (day(2027, 2, 2), false),
    ] {
        let hh = Household {
            return_filed: filed,
            ..base()
        };
        let res = rules().schedule(&hh.facts()).unwrap();
        assert_eq!(
            res.value()[3].status() == Status::Excused,
            excused,
            "filed {filed}"
        );
    }
    let hh = Household {
        return_filed: day(2027, 3, 1),
        ..base()
    };
    assert_eq!(
        rules()
            .penalty(&hh.facts(), day(2027, 4, 15))
            .unwrap()
            .value()
            .as_of(),
        day(2027, 3, 1)
    );
}

// Form 2210ME line 10a spreads withholding 25% a quarter whenever it was
// withheld, and has no election to use the dates.
#[test]
fn withholding_is_spread_evenly() {
    for as_withheld in [false, true] {
        // 90% of $44,444.44 is $40,000, all withheld on December 15.
        let hh = Household {
            tax: USCents(4_444_444),
            prior: last(DOLLAR * 1_000_000),
            withheld: DOLLAR * 40_000,
            as_withheld,
            ..Household::default()
        };
        let res = rules().schedule(&hh.facts()).unwrap();
        for (i, s) in res.value().iter().enumerate() {
            assert!(
                s.status() == Status::Met && s.credited_by() == DOLLAR * 10_000 * (i as i64 + 1),
                "election {as_withheld}, installment {}",
                i + 1
            );
        }
    }
}

// The annualized method: 22.5%, 45%, 67.5% and 90% of the annualized tax,
// less earlier installments, capped by the regular installment with what the
// method lowered carried forward.
#[test]
fn annualised_method() {
    // 90% of $40,000 is $36,000, $9,000 a quarter. Annualized tax of $8,000,
    // $20,000, $40,000 and $40,000 gives $1,800, $7,200, $18,000 and $9,000.
    let hh = Household {
        tax: DOLLAR * 40_000,
        prior: last(DOLLAR * 50_000),
        annualised: Some(vec![
            DOLLAR * 8_000,
            DOLLAR * 20_000,
            DOLLAR * 40_000,
            DOLLAR * 40_000,
        ]),
        ..Household::default()
    };
    let res = rules().schedule(&hh.facts()).unwrap();
    for (i, want) in [1_800, 9_000, 27_000, 36_000].into_iter().enumerate() {
        assert_eq!(res.value()[i].needed_by(), DOLLAR * want);
    }
    let ps: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 6, 30),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .into_iter()
    .map(|end| estimated::new_period().end(end).build().unwrap())
    .collect();
    let wrong = estimated::new_facts(ME, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .periods(&ps)
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    assert!(rules().requirement(&wrong).is_err());
}

// Maine's waiver is the assessor's, "for cause". It is asked for when there
// is a penalty, and a waiver asked for is reported without changing the
// total.
#[test]
fn waiver() {
    let res = rules().penalty(&base().facts(), day(2027, 4, 15)).unwrap();
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));
    let hh = Household {
        waiver: Some(WaiverKind::Casualty),
        ..base()
    };
    let asked = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
    assert!(
        asked.value().waiver() == WaiverKind::Casualty
            && asked.value().total() == res.value().total()
            && !has(&asked.required(), PositionKind::PenaltyWaiver)
    );
}

// Maine's "for cause" waiver lists no causes, so a casualty or disaster is
// left open and the open point asked. Retirement and disability have no
// waiver, even where the federal one applies.
#[test]
fn waiver_outcomes() {
    let pos = |c: WaiverCause, on: Date, born: Option<Date>| {
        let mut b = estimated::new_positions();
        b.waiver_for(c, on);
        if let Some(born) = born {
            b.born(born);
        }
        b.build().unwrap()
    };
    for (name, p, refused, want) in [
        (
            "disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
            false,
            "doesn't say if a casualty or disaster is cause for this penalty",
        ),
        (
            "retired in 2026 at 64",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1962, 3, 1)),
            ),
            true,
            "no waiver for retirement or disability",
        ),
        (
            "retired in 2020",
            pos(
                WaiverCause::Retirement,
                day(2020, 6, 1),
                Some(day(1955, 3, 1)),
            ),
            true,
            "no waiver for retirement or disability",
        ),
    ] {
        let hh = Household {
            positions: Some(p),
            ..base()
        };
        let res = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
        let pen = res.value();
        let o = pen.waiver_outcome().unwrap();
        assert!(
            o.refused() == refused && o.undecided() != refused && o.reason().contains(want),
            "{name}: {o:?}"
        );
        assert!(
            pen.total().0 != 0 && pen.if_waived() == pen.total(),
            "{name}"
        );
        assert_eq!(
            has(&res.required(), PositionKind::UnsettledRule),
            !refused,
            "{name}"
        );
    }
}

// Under last year's small-liability exception, this year's tax can't change
// the target, so it isn't asked for.
#[test]
fn exempt_asks_nothing_of_the_target() {
    let res = rules()
        .requirement(
            &Household {
                no_tax: true,
                prior: last(DOLLAR * 500),
                ..Household::default()
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().exempt() && !has(&res.required(), PositionKind::ThisYearTax));
}

// Each divergence is reported with both readings and both sources.
#[test]
fn divergences_pin_both_readings() {
    let res = rules().requirement(&base().facts()).unwrap();
    let want = [
        (
            "Last year's liability",
            "after refundable credits",
            "§ 5228(2)(D), (3)(A)",
        ),
        (
            "The $1,000 test on this year's tax",
            "tax less withholding",
            "§ 5228(1)(A), (2)(C)",
        ),
        (
            "When withholding counts as paid",
            "25% on each due date",
            "§ 5228(1)(A), (5)(A)",
        ),
        (
            "A return filed and paid by January 31",
            "doesn't mention the relief",
            "P.L. 2025, c. 113",
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, form, statute)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(
            d.form().contains(form)
                && d.statute_source().contains(statute)
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{:?}",
            d.rule()
        );
        // The base household has no withholding, no payments and no return
        // filed, and 90% of this year's tax sets its target: none reaches
        // it.
        assert!(!d.affects_household(), "{:?}", d.rule());
        let want_uses = if d.rule() == "A return filed and paid by January 31" {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert_eq!(d.uses(), want_uses, "{:?}", d.rule());
    }

    // Each one affects the facts it turns on.
    for (rule, hh) in [
        (
            "Last year's liability",
            Household {
                prior: last(DOLLAR * 3_000),
                ..base()
            },
        ),
        (
            "The $1,000 test on this year's tax",
            Household {
                payments: vec![pay(dues()[0], DOLLAR * 3_500)],
                ..base()
            },
        ),
        (
            "When withholding counts as paid",
            Household {
                withheld: DOLLAR * 1_000,
                ..base()
            },
        ),
        (
            "A return filed and paid by January 31",
            Household {
                return_filed: day(2027, 1, 29),
                ..base()
            },
        ),
    ] {
        let ds = rules().requirement(&hh.facts()).unwrap().divergences();
        let d = ds.iter().find(|d| d.rule() == rule);
        assert!(d.is_some_and(|d| d.affects_household()), "{rule:?}");
    }
}

#[test]
fn refuses_what_is_not_built() {
    assert!(
        mainetaxcalc::estimated_tax(2025)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NoRules))
    );
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    for farmer in [true, false] {
        let mut b = estimated::new_facts(ME, 2026);
        b.filing_status(filing::Status::Single).this_year(this);
        if farmer {
            b.farm_share(Rate(7000));
        } else {
            b.fish_share(Rate(7000));
        }
        assert!(
            rules()
                .requirement(&b.build().unwrap())
                .unwrap_err()
                .is(&estimated::Error::Unsupported)
        );
    }
    let cases: [(&str, Facts); 3] = [
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
            estimated::new_facts(ME, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(ME, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("not Maine's")
                .build()
                .unwrap(),
        ),
    ];
    for (name, f) in cases {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    let z = USCents(0);
    let builders = [
        (
            "one period",
            mainetaxcalc::new_estimated_facts()
                .income_not_withheld(1, z, z)
                .clone(),
        ),
        (
            "installment 5",
            mainetaxcalc::new_estimated_facts()
                .income_not_withheld(5, z, z)
                .clone(),
        ),
        (
            "periods and none",
            mainetaxcalc::new_estimated_facts()
                .income_not_withheld(1, z, z)
                .income_not_withheld(2, z, z)
                .income_not_withheld(3, z, z)
                .income_not_withheld(4, z, z)
                .no_unusual_event()
                .clone(),
        ),
        (
            "a tax with no income",
            mainetaxcalc::new_estimated_facts()
                .unusual_event_tax(1, USCents(100))
                .clone(),
        ),
        (
            "a period twice",
            mainetaxcalc::new_estimated_facts()
                .income_not_withheld(1, z, z)
                .income_not_withheld(1, z, z)
                .clone(),
        ),
    ];
    for (name, b) in builders {
        assert!(b.build().is_err(), "{name}: built");
    }
    assert!(rules().penalty(&base().facts(), Date::default()).is_err());
}

#[test]
fn explanation_names_the_differences() {
    let e = rules().explanation();
    let text = format!(
        "{}{}{}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$1,000",
        "90%",
        "100%",
        "$500,000",
        "unusual event",
        "0.75% a month",
        "compounded monthly",
        "part of a month",
        "February 1, 2027",
        "no 110% tier",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
}

// Without this year's tax the target is last year's liability alone, and the
// penalty against it is an upper bound. Under $1,000 last year still exempts
// exactly; with no liability on record there is no target.
#[test]
fn without_this_years_tax() {
    let mut hh = Household {
        no_tax: true,
        prior: last(DOLLAR * 20_000),
        ..Household::default()
    };
    let res = rules().requirement(&hh.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 20_000, "{}", r.why());
    let o = r.options()[0].clone();
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let pen = rules()
        .penalty(&hh.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!((pen.upper_bound() || !pen.rated_through().is_zero()) && pen.total().0 != 0);

    hh.prior = last(DOLLAR * 999);
    let pen = rules()
        .penalty(&hh.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!(pen.total().0 == 0 && !pen.upper_bound());

    hh.prior = None;
    assert!(
        !rules()
            .requirement(&hh.facts())
            .unwrap()
            .value()
            .target_known()
    );
    assert!(
        rules()
            .schedule(&hh.facts())
            .unwrap_err()
            .is(&estimated::Error::NeedsThisYearTax)
    );
    assert!(
        rules()
            .penalty(&hh.facts(), day(2027, 4, 15))
            .unwrap_err()
            .is(&estimated::Error::NeedsThisYearTax)
    );
}

// Withholding over the tax leaves a refund, never a negative balance.
#[test]
fn withholding_over_the_tax_reads_as_a_refund() {
    let withheld: Vec<_> = (1..=12)
        .map(|m| {
            estimated::new_withheld()
                .on(day(2026, m, 28))
                .amount(money::dollars(300))
                .build()
                .unwrap()
        })
        .collect();
    let figures = |tax: i64, agi: i64| {
        estimated::new_figures()
            .tax(money::dollars(tax))
            .federal_agi(money::dollars(agi))
            .state_agi(money::dollars(agi))
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
    let f = estimated::new_facts(ME, 2026)
        .filing_status(filing::Status::Single)
        .this_year(figures(3_000, 60_000))
        .prior_year(prior)
        .withheld(&withheld)
        .build()
        .unwrap();
    let got = rules().requirement(&f).unwrap().value().why();
    assert!(
        got.contains("a $600 refund") && !got.contains("−$") && !got.contains("-$"),
        "why = {got:?}"
    );
}
