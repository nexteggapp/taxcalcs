//! Maryland's estimated tax rules for 2026, checked against the
//! Comptroller's forms and the Tax-General Article.
//!
//! Go's TestEstimatedFiguresAreInTheirSources and the testdata/ phrase
//! checks at the end of the divergence test are not ported.

#![allow(clippy::unwrap_used)]

use std::collections::HashMap;

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status, WaiverCause,
    WaiverKind, Weekday, Withheld,
};
use money::{DOLLAR, FineRate, Rate, USCents, dollars, percent};
use taxcalcs_marylandtaxcalc as marylandtaxcalc;
use working::{PositionKind, Reading, Required, Rule};

use marylandtaxcalc::EstimatedFacts;

const MD: estimated::Jurisdiction = estimated::MARYLAND;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    marylandtaxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind, phrase: &str) -> bool {
    rs.iter()
        .any(|r| r.position() == k && format!("{} {}", r.question(), r.effect()).contains(phrase))
}

fn applied(rs: &[Rule], phrase: &str) -> bool {
    rs.iter().any(|r| r.applied().contains(phrase))
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

fn withheld(on: Date, a: USCents) -> Withheld {
    estimated::new_withheld().on(on).amount(a).build().unwrap()
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
    a.iter().zip(dues()).map(|(&x, d)| pay(d, x)).collect()
}

#[derive(Clone, Default)]
struct Household {
    /// Form 502UP line 7.
    tax: USCents,
    local: USCents,
    no_tax: bool,
    prior: Option<PriorReturn>,
    withholding: Vec<Withheld>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    return_filed: Date,
    /// Income to each period end, Form 502UP line 11.
    periods: Option<Vec<USCents>>,
    waiver: Option<WaiverKind>,
    positions: Option<Positions>,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

fn with_prior(t: USCents, p: USCents) -> Household {
    Household {
        prior: filed(p),
        ..tax(t)
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        if self.local.0 > 0 {
            fb.local_tax(self.local);
        }
        let mut b = estimated::new_facts(MD, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(fb.build().unwrap())
            .withheld(&self.withholding)
            .paid(&self.payments)
            .return_filed(self.return_filed);
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        match self.extras {
            Some(e) => b.state(e),
            None => b.state(
                marylandtaxcalc::new_estimated_facts()
                    .taxable_by_another_state(false)
                    .build()
                    .unwrap(),
            ),
        };
        if let Some(ps) = &self.periods {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            for (i, &inc) in ps.iter().enumerate() {
                b.periods(&[estimated::new_period()
                    .end(ends[i])
                    .income(inc)
                    .build()
                    .unwrap()]);
            }
            b.elections(estimated::new_elections().annualised().build().unwrap());
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

/// A filed prior return with its tax, Form 502UP line 9a.
fn filed(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .build()
            .unwrap(),
    )
}

/// The 2026 dates are the ones the worksheet prints, and none moves. The
/// January 31 file-and-pay date is a Sunday in 2027.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&tax(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    assert_eq!(day(2027, 1, 31).weekday(), Weekday::SUNDAY);
}

/// § 1-201(b) moves January 31, 2027 to Monday, February 1.
#[test]
fn estimated_january_thirty_first_moves_to_february_first() {
    for (filed_on, excused) in [
        (day(2027, 1, 29), true),
        (day(2027, 2, 1), true),
        (day(2027, 2, 2), false),
    ] {
        let h = Household {
            return_filed: filed_on,
            payments: quarterly(&[DOLLAR * 2_250, DOLLAR * 2_250, DOLLAR * 2_250]),
            ..tax(DOLLAR * 10_000)
        };
        let s = rules().schedule(&h.facts()).unwrap().value();
        let got = s[3].status() == Status::Met && s[3].needed_by() == s[2].needed_by();
        assert_eq!(got, excused, "filed {filed_on}");
        if !excused {
            assert_eq!(s[3].status(), Status::Short);
        }
        let p = rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .value();
        let res = rules().requirement(&h.facts()).unwrap();
        if excused {
            assert!(applied(
                &res.applied(),
                "by January 31, 2027 (February 1, 2027,"
            ));
            assert_eq!(p.total().0, 0);
        } else {
            assert_ne!(p.total().0, 0);
        }
    }
}

/// Form 502UP exception A: no interest at $500 or less of tax after
/// withholding. One cent more and the rules apply.
#[test]
fn estimated_threshold_boundaries() {
    for (w, exempt) in [(950_000, true), (949_999, false)] {
        let h = Household {
            withholding: vec![withheld(day(2026, 3, 1), USCents(w))],
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {w}");
        if exempt {
            let c = res.value().chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && c.label() == "Tax less withholding of $500 or less"
            );
        }
    }
    for (w, none) in [(950_000, true), (949_999, false)] {
        let h = Household {
            withholding: vec![withheld(day(2026, 12, 31), USCents(w))],
            ..tax(DOLLAR * 10_000)
        };
        let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        assert_eq!(
            p.value().total().0 == 0,
            none,
            "withheld {w} on December 31"
        );
    }
}

/// The target is the lesser of 90% of this year's tax and 110% of last
/// year's, at every income.
#[test]
fn estimated_target_options() {
    for (p, chosen, target) in [
        (900_001, Basis::ThisYearTax, 990_000),
        (900_000, Basis::ThisYearTax, 990_000),
        (899_999, Basis::PriorYearTax, 989_999),
    ] {
        let r = rules()
            .requirement(&with_prior(DOLLAR * 11_000, USCents(p)).facts())
            .unwrap()
            .value();
        assert!(
            r.chosen().unwrap().basis() == chosen && r.target() == USCents(target),
            "prior {p}"
        );
        assert_eq!(r.options()[1].percent(), percent(110));
    }

    // 110% at a high income too.
    let r = rules()
        .requirement(&with_prior(DOLLAR * 1_000_000, DOLLAR * 500_000).facts())
        .unwrap()
        .value();
    assert_eq!(r.target(), DOLLAR * 550_000);

    // A first-time filer uses 90% of this year's tax.
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let h = Household {
        prior: Some(not_filed),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(!r.options()[1].available() && r.target() == DOLLAR * 9_000);

    // No return on record, or one not required: asked, and not offered.
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    for (name, p, kind) in [
        ("none on record", None, PositionKind::PriorYearReturn),
        (
            "not required",
            Some(not_required),
            PositionKind::UnsettledRule,
        ),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(
            !res.value().options()[1].available() && has(&res.required(), kind, ""),
            "{name}"
        );
    }

    // A short prior year is used: nothing in Maryland requires twelve months.
    let short = estimated::new_prior_return()
        .filed(true)
        .months(9)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 5_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let h = Household {
        prior: Some(short),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert_eq!(r.target(), DOLLAR * 5_500);
}

/// Form 502UP line 9b of zero makes line 10 zero, so no interest arises.
#[test]
fn estimated_no_tax_last_year_means_no_interest() {
    let f = with_prior(DOLLAR * 10_000, USCents(0)).facts();
    let r = rules().requirement(&f).unwrap().value();
    assert!(!r.required() && r.target().0 == 0);
    for inst in rules().schedule(&f).unwrap().value() {
        assert_eq!(inst.status(), Status::NothingDue);
    }
    let p = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total().0, 0);
}

/// 502UP's only worked figures: $500 paid each quarter is $500, $1,000,
/// $1,500 and $2,000 on line 14.
#[test]
fn estimated_credits_are_cumulative_as_line_14_says() {
    let h = Household {
        payments: quarterly(&[DOLLAR * 500; 4]),
        ..tax(DOLLAR * 10_000)
    };
    for (i, inst) in rules()
        .schedule(&h.facts())
        .unwrap()
        .value()
        .iter()
        .enumerate()
    {
        assert_eq!(inst.credited_by(), DOLLAR * 500 * (i as i64 + 1));
    }
}

/// Nothing paid on a $36,000 target: line 15 is $9,000, $18,000, $27,000
/// and $36,000. The third column spans the change from 10.8133% for 2026 to
/// 9.995% for 2027.
#[test]
fn estimated_interest_by_the_month_across_the_rate_change() {
    let f = with_prior(DOLLAR * 40_000, DOLLAR * 40_000).facts();
    let res = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    let lines = res.value().lines();
    let want: [(i64, &[i64], &[i64]); 4] = [
        (900_000, &[2], &[16_220]),
        (1_800_000, &[3], &[48_660]),
        (2_700_000, &[3, 1], &[72_990, 22_489]),
        (3_600_000, &[3], &[89_955]),
    ];
    assert_eq!(lines.len(), 4);
    for (i, w) in want.iter().enumerate() {
        let l = &lines[i];
        assert_eq!(l.shortfall(), USCents(w.0), "column {}", i + 1);
        let parts = l.parts();
        assert_eq!(parts.len(), w.1.len(), "column {}", i + 1);
        for (j, p) in parts.iter().enumerate() {
            assert_eq!(
                (p.months(), p.charge()),
                (w.1[j], USCents(w.2[j])),
                "column {} part {}",
                i + 1,
                j + 1
            );
        }
    }
    let parts = lines[2].parts();
    assert!(parts[0].rate() != parts[1].rate() && parts[1].to() == day(2027, 1, 15));
    assert_eq!(res.value().total(), USCents(250_314));
    // The working carries each rate as published, not cut to basis points.
    assert!(parts[0].fine_rate() == FineRate(108_133) && parts[1].fine_rate() == FineRate(99_950));

    // At a hundred times the size the rates show to their last digit.
    let big = with_prior(DOLLAR * 4_000_000, DOLLAR * 4_000_000).facts();
    let lines = rules()
        .penalty(&big, day(2027, 4, 15))
        .unwrap()
        .value()
        .lines();
    assert_eq!(lines[0].parts()[0].charge(), USCents(1_621_995));
    assert_eq!(lines[2].parts()[1].charge(), USCents(2_248_875));
}

/// A payment after its due date but before the next is charged the months
/// it was late, a part of a month counting as one.
#[test]
fn estimated_late_payment_counts_whole_months() {
    for (paid, charge) in [(day(2026, 5, 15), 8_110), (day(2026, 5, 16), 16_220)] {
        let mut payments = vec![pay(paid, DOLLAR * 9_000)];
        payments.extend_from_slice(
            &quarterly(&[USCents(0), DOLLAR * 9_000, DOLLAR * 9_000, DOLLAR * 9_000])[1..],
        );
        let h = Household {
            payments,
            ..with_prior(DOLLAR * 40_000, DOLLAR * 40_000)
        };
        let p = rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .value();
        assert!(
            p.lines().len() == 1 && p.total() == USCents(charge),
            "paid {paid}: {}",
            p.total()
        );
    }
}

/// Withholding counts on the dates withheld.
#[test]
fn estimated_withholding_counts_on_its_dates() {
    let mut h = Household {
        withholding: vec![withheld(day(2026, 12, 15), DOLLAR * 36_000)],
        ..with_prior(DOLLAR * 40_000, DOLLAR * 40_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(!r.required(), "{}", r.why());
    let p = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!(p.lines().len() == 3 && p.total() == USCents(16_220 + 48_660 + 72_990));
    h.withholding = [
        day(2026, 4, 15),
        day(2026, 6, 15),
        day(2026, 9, 15),
        day(2026, 12, 15),
    ]
    .into_iter()
    .map(|d| withheld(d, DOLLAR * 9_000))
    .collect();
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total().0, 0);
}

/// Form 502UP lines 11 to 13 by election: the target in the shares of the
/// year's income received by each period's end.
#[test]
fn estimated_income_share_method() {
    let mut h = Household {
        periods: Some(vec![
            USCents(0),
            DOLLAR * 10_000,
            DOLLAR * 20_000,
            DOLLAR * 100_000,
        ]),
        payments: quarterly(&[USCents(0), DOLLAR * 3_600, DOLLAR * 3_600, DOLLAR * 28_800]),
        ..with_prior(DOLLAR * 40_000, DOLLAR * 40_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [0, 360_000, 720_000, 3_600_000].into_iter().enumerate() {
        assert_eq!(s[i].needed_by(), USCents(want), "column {}", i + 1);
    }
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total().0, 0);
    h.periods = None;
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_ne!(p.value().total().0, 0);

    let bad = Household {
        periods: Some(vec![
            USCents(5_000),
            USCents(4_000),
            USCents(6_000),
            USCents(10_000),
        ]),
        ..tax(DOLLAR * 40_000)
    };
    assert!(rules().requirement(&bad.facts()).is_err());
}

/// At least 90% of income taxable by another state and an underpayment no
/// greater than the local tax: no interest (code 302).
#[test]
fn estimated_income_taxable_by_another_state() {
    let yes = marylandtaxcalc::new_estimated_facts()
        .taxable_by_another_state(true)
        .build()
        .unwrap();
    let payments = quarterly(&[DOLLAR * 1_500; 4]);
    for (local, none) in [(DOLLAR * 3_000, true), (USCents(299_999), false)] {
        let h = Household {
            local,
            payments: payments.clone(),
            extras: Some(yes),
            ..tax(DOLLAR * 10_000)
        };
        let p = rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .value();
        assert_eq!(p.total().0 == 0, none, "local tax {local}");
        let s = rules().schedule(&h.facts()).unwrap().value();
        assert_eq!(s[0].status() == Status::Excused, none, "local tax {local}");
    }
    let unsaid = marylandtaxcalc::new_estimated_facts().build().unwrap();
    let h = Household {
        local: DOLLAR * 3_000,
        payments: payments.clone(),
        extras: Some(unsaid),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(
        has(
            &res.required(),
            PositionKind::JurisdictionFact,
            "taxable by another state"
        ) && res.value().total().0 != 0
    );
    let h = Household {
        payments,
        extras: Some(yes),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(
        has(
            &res.required(),
            PositionKind::JurisdictionFact,
            "county income tax"
        ) && res.value().total().0 != 0
    );
}

/// The interest stops on April 15, 2027, or on the day the return was filed
/// and paid.
#[test]
fn estimated_interest_stops() {
    let f = with_prior(DOLLAR * 40_000, DOLLAR * 40_000).facts();
    let p = rules().penalty(&f, day(2027, 6, 30)).unwrap().value();
    assert!(p.as_of() == day(2027, 4, 15) && p.total() == USCents(250_314));
    let h = Household {
        return_filed: day(2027, 2, 20),
        ..with_prior(DOLLAR * 40_000, DOLLAR * 40_000)
    };
    let f = h.facts();
    let p = rules().penalty(&f, day(2027, 4, 15)).unwrap().value();
    assert_eq!(p.as_of(), day(2027, 2, 20));
    let last = p.lines()[3].parts();
    assert!(last.len() == 1 && last[0].months() == 2 && last[0].charge() == USCents(59_970));
    // Before a due date has passed, nothing has accrued on it.
    let p = rules().penalty(&f, day(2026, 6, 1)).unwrap().value();
    assert!(p.lines().len() == 1 && p.total() == USCents(16_220));
}

/// What the research could not settle is asked, with the default named.
#[test]
fn estimated_open_questions_are_reported() {
    let f = with_prior(DOLLAR * 40_000, DOLLAR * 40_000).facts();
    let res = rules().penalty(&f, day(2027, 4, 15)).unwrap();
    for (kind, phrase) in [
        (PositionKind::UnpublishedRate, "2026 interest factors"),
        (PositionKind::UnpublishedRate, "up to 25%"),
        (PositionKind::PenaltyWaiver, "reasonable cause"),
    ] {
        assert!(has(&res.required(), kind, phrase), "{kind} {phrase:?}");
    }
    let h = Household {
        waiver: Some(WaiverKind::Casualty),
        ..with_prior(DOLLAR * 40_000, DOLLAR * 40_000)
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(
        !has(&res.required(), PositionKind::PenaltyWaiver, "")
            && res.value().waiver() == WaiverKind::Casualty
    );
    // Nothing short: no question about the factors or the 25% penalty.
    let paid = Household {
        payments: quarterly(&[DOLLAR * 9_000; 4]),
        ..with_prior(DOLLAR * 40_000, DOLLAR * 40_000)
    };
    let res = rules().penalty(&paid.facts(), day(2027, 4, 15)).unwrap();
    assert!(!has(&res.required(), PositionKind::UnpublishedRate, ""));
}

/// Maryland's reasonable-cause waiver lists no causes, so a casualty or
/// disaster is left open and the open point asked. Retirement and
/// disability have no waiver.
#[test]
fn estimated_waiver_outcomes() {
    let pos = |c: WaiverCause, on: Date, born: Option<Date>| {
        let mut b = estimated::new_positions();
        b.waiver_for(c, on);
        if let Some(d) = born {
            b.born(d);
        }
        b.build().unwrap()
    };
    for (name, p, refused, want) in [
        (
            "disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
            false,
            "if a casualty or disaster counts",
        ),
        (
            "retired in 2026 at 64",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1962, 3, 1)),
            ),
            true,
            "not retirement or disability",
        ),
        (
            "retired in 2020",
            pos(
                WaiverCause::Retirement,
                day(2020, 6, 1),
                Some(day(1955, 3, 1)),
            ),
            true,
            "not retirement or disability",
        ),
    ] {
        let h = Household {
            positions: Some(p),
            ..with_prior(DOLLAR * 40_000, DOLLAR * 40_000)
        };
        let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        let p = res.value();
        let o = p.waiver_outcome().unwrap();
        assert!(
            o.refused() == refused && o.undecided() != refused && o.reason().contains(want),
            "{name}: {o:?}"
        );
        assert!(p.total().0 != 0 && p.if_waived() == p.total(), "{name}");
        assert_eq!(
            has(
                &res.required(),
                PositionKind::UnsettledRule,
                "casualty or disaster"
            ),
            !refused,
            "{name}"
        );
    }
}

/// When the $500 exception sets the target, last year's return can't change
/// it, so it isn't asked for.
#[test]
fn estimated_exempt_asks_nothing_of_the_target() {
    let res = rules().requirement(&tax(DOLLAR * 400).facts()).unwrap();
    assert!(res.value().exempt() && !has(&res.required(), PositionKind::PriorYearReturn, ""));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules()
        .penalty(&tax(DOLLAR * 10_000).facts(), day(2027, 4, 15))
        .unwrap();
    let want: HashMap<&str, [&str; 3]> = HashMap::from([
        (
            "No interest at $500",
            ["$500 or less", "§ 13-602(c)", "less than one-half"],
        ),
        (
            "Who may use 110% of last year's tax",
            [
                "more income than you did in 2025",
                "§ 13-602(a)(2)",
                "no condition on income",
            ],
        ),
        (
            "Last year's tax",
            ["line 34", "§ 13-602(a)(2)(ii)", "tax paid"],
        ),
        (
            "The interest factors",
            [
                "0.0192, 0.0289, 0.0381 and 0.0266",
                "§ 13-604(a)",
                "0.0191, 0.0287, 0.0377 and 0.0270",
            ],
        ),
        (
            "A third-period payment made from December 15",
            [
                "on or after December 15",
                "§§ 10-820(b)(1), 13-602(a), 13-604(a)",
                "due January 15",
            ],
        ),
        (
            "The penalty of up to 25%",
            ["Only interest", "§ 13-702(a)", "up to 25%"],
        ),
    ]);
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let rule = d.rule();
        let w = want[rule.as_str()];
        assert!(
            d.form().contains(w[0])
                && d.statute_source().contains(w[1])
                && d.statute().contains(w[2])
                && !d.form_source().is_empty(),
            "{rule:?}"
        );
        let want_affects = rule == "The interest factors" || rule == "The penalty of up to 25%";
        assert_eq!(d.affects_household(), want_affects, "{rule:?}");
        let want_uses = if rule == "The interest factors"
            || rule == "A third-period payment made from December 15"
        {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert_eq!(d.uses(), want_uses, "{rule:?}");
    }
    // Exactly $500 after withholding is where the form and the statute part.
    let h = Household {
        withholding: vec![withheld(day(2026, 3, 1), USCents(950_000))],
        ..tax(DOLLAR * 10_000)
    };
    let at = rules().requirement(&h.facts()).unwrap();
    for d in at.divergences() {
        if d.rule() == "No interest at $500" {
            assert!(d.affects_household());
        }
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for y in [2025, 2027] {
        let err = marylandtaxcalc::estimated_tax(y).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{y}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let base = |j: estimated::Jurisdiction, y: i64| {
        let mut b = estimated::new_facts(j, y);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    for (name, f) in [
        (
            "a farmer",
            base(MD, 2026).farm_share(Rate(7000)).build().unwrap(),
        ),
        (
            "a fisherman",
            base(MD, 2026).fish_share(Rate(7000)).build().unwrap(),
        ),
    ] {
        let err = rules().requirement(&f).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    for (name, f) in [
        (
            "Pennsylvania's facts",
            base(estimated::PENNSYLVANIA, 2026).build().unwrap(),
        ),
        ("2027", base(MD, 2027).build().unwrap()),
        (
            "other extras",
            base(MD, 2026).state("not Maryland's").build().unwrap(),
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
        "{}{}{}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$500",
        "110%",
        "county",
        "on the dates",
        "10.8133%",
        "9.995%",
        "by the month",
        "January 31",
        "25%",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
}

/// Without this year's tax the target is 110% of last year's, and the
/// interest figured against it is the most it can be.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: filed(DOLLAR * 10_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 11_000, "{}", r.why());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax, ""));
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [275_000, 550_000, 825_000, 1_100_000]
        .into_iter()
        .enumerate()
    {
        assert_eq!(s[i].needed_by(), USCents(want));
    }
    let p = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!(p.upper_bound() && p.total().0 != 0);

    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    assert!(
        !rules()
            .requirement(&none.facts())
            .unwrap()
            .value()
            .target_known()
    );
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
    let err = rules()
        .penalty(&none.facts(), day(2027, 4, 15))
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
}

/// Withholding over the tax leaves a refund, never a negative balance.
#[test]
fn withholding_over_the_tax_reads_as_a_refund() {
    let w: Vec<_> = (1..=12)
        .map(|m| withheld(day(2026, m, 28), dollars(300)))
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
    let f = estimated::new_facts(MD, 2026)
        .filing_status(filing::Status::Single)
        .this_year(figures(3_000, 60_000))
        .prior_year(prior)
        .withheld(&w)
        .build()
        .unwrap();
    let got = rules().requirement(&f).unwrap().value().why();
    assert!(
        got.contains("a $600 refund") && !got.contains("−$") && !got.contains("-$"),
        "why = {got:?}"
    );
}
