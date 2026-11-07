//! Kentucky's estimated tax rules for 2026, checked against Form 2210-K's
//! arithmetic and hand-worked cases. Ported from estimated_test.go; the Go
//! test that finds each figure in its extract in testdata/ reads the Go
//! package's citations and is not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status, WaiverCause,
    WaiverKind, Weekday,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_kentuckytaxcalc as kentuckytaxcalc;
use working::{PositionKind, Reading, Required, Rule};

const KY: estimated::Jurisdiction = estimated::KENTUCKY;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    kentuckytaxcalc::estimated_tax(2026).unwrap()
}

fn find(rs: &[Required], k: PositionKind) -> Option<Required> {
    rs.iter().find(|r| r.position() == k).cloned()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    find(rs, k).is_some()
}

fn count(rs: &[Required], k: PositionKind) -> usize {
    rs.iter().filter(|r| r.position() == k).count()
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

/// One payment of each amount on its due date.
fn quarterly(a: &[i64]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .filter(|(_, x)| **x > 0)
        .map(|(i, &x)| pay(dues()[i], DOLLAR * x))
        .collect()
}

/// Extras that settle every Kentucky question, so that a test sees only the
/// Required entries it is about.
fn answered() -> kentuckytaxcalc::EstimatedFacts {
    kentuckytaxcalc::new_estimated_facts()
        .non_wage_income(DOLLAR * 100_000)
        .filing_threshold_met(true)
        .prior_year_us_resident(true)
        .combined_return(false)
        .build()
        .unwrap()
}

#[derive(Clone, Default)]
struct Household {
    status: filing::Status,
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<kentuckytaxcalc::EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    annualised: Option<Vec<USCents>>,
    as_withheld: bool,
    waiver: Option<WaiverKind>,
    positions: Option<Positions>,
}

fn h(tax: i64) -> Household {
    Household {
        tax: DOLLAR * tax,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let status = if self.status == filing::Status::Unspecified {
            filing::Status::Single
        } else {
            self.status
        };
        let figures = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(KY, 2026);
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
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        b.state(self.extras.unwrap_or_else(answered));
        let mut eb = estimated::new_elections();
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
            eb.annualised();
        }
        if self.as_withheld {
            eb.withholding_as_withheld();
        }
        b.elections(eb.build().unwrap());
        if let Some(k) = self.waiver {
            b.positions(estimated::new_positions().waiver(k).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed full-year Kentucky return with its tax and Kentucky AGI.
fn prior(tax: USCents, agi: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(
                estimated::new_figures()
                    .tax(tax)
                    .state_agi(agi)
                    .build()
                    .unwrap(),
            )
            .build()
            .unwrap(),
    )
}

// The 2026 dates are the ones the voucher instructions print, all weekdays,
// so none moves. January 31, 2027 is a Sunday, and the file-and-pay date the
// instructions print, February 1, 2027, is it moved to the Monday.
#[test]
fn due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&h(10_000).facts()).unwrap();
    for (i, s) in res.value().iter().enumerate() {
        assert_eq!(s.due(), dues()[i]);
        let wd = s.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    assert_eq!(day(2027, 1, 31).weekday(), Weekday::SUNDAY);
    for (filed, excused) in [
        (day(2027, 1, 31), true),
        (day(2027, 2, 1), true),
        (day(2027, 2, 2), false),
    ] {
        let hh = Household {
            first_met: day(2026, 10, 1),
            return_filed: filed,
            ..h(10_000)
        };
        let got = rules().schedule(&hh.facts()).unwrap().value()[3].status() == Status::Excused;
        assert_eq!(got, excused, "filed {filed}");
    }
}

// $500 or less of tax after withholding owes nothing; $500.01 does.
#[test]
fn five_hundred_dollar_boundary() {
    for (withheld, exempt) in [(950_000, true), (949_999, false)] {
        let r = rules()
            .requirement(
                &Household {
                    withheld: USCents(withheld),
                    ..h(10_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        assert_eq!(r.exempt(), exempt, "withheld {withheld}: {}", r.why());
        if exempt {
            let c = r.chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && c.label() == "Tax less withholding of $500 or less",
                "{c:?}"
            );
        }
    }
}

// KRS 141.305(1): more than $5,000 of income without Kentucky withholding, or
// AGI at the filing threshold, brings in the rules. Neither, both answered,
// means no payments are required.
#[test]
fn income_tests() {
    for (non_wage, exempt) in [(500_000, true), (500_001, false)] {
        let e = kentuckytaxcalc::new_estimated_facts()
            .non_wage_income(USCents(non_wage))
            .filing_threshold_met(false)
            .prior_year_us_resident(true)
            .build()
            .unwrap();
        let r = rules()
            .requirement(
                &Household {
                    extras: Some(e),
                    ..h(10_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        assert_eq!(r.exempt(), exempt, "non-wage income {non_wage}");
    }
    // Exempt by the income tests, neither this year's tax nor last year's
    // return can change the target, so neither is asked for.
    let low = kentuckytaxcalc::new_estimated_facts()
        .non_wage_income(USCents(0))
        .filing_threshold_met(false)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                no_tax: true,
                extras: Some(low),
                ..Household::default()
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().exempt());
    assert!(!has(&res.required(), PositionKind::ThisYearTax));
    assert!(!has(&res.required(), PositionKind::PriorYearReturn));
    // The filing threshold alone brings the rules in.
    let e = kentuckytaxcalc::new_estimated_facts()
        .non_wage_income(USCents(0))
        .filing_threshold_met(true)
        .build()
        .unwrap();
    let r = rules()
        .requirement(
            &Household {
                extras: Some(e),
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    assert!(r.required(), "{}", r.why());
    // Neither answered: the rules are applied, and the question asked.
    let e = kentuckytaxcalc::new_estimated_facts()
        .prior_year_us_resident(true)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                extras: Some(e),
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().required() && has(&res.required(), PositionKind::JurisdictionFact));
}

// The two options and the 110% step, one cent either side of $150,000 and,
// married filing separately, of $75,000.
#[test]
fn target_options() {
    // This year's 90% is the smaller.
    let r = rules()
        .requirement(
            &Household {
                prior: prior(DOLLAR * 12_000, DOLLAR * 100_000),
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    assert!(r.chosen().unwrap().basis() == Basis::ThisYearTax && r.target() == DOLLAR * 9_000);

    for (status, agi, want) in [
        (filing::Status::Single, 15_000_000, DOLLAR * 8_000),
        (filing::Status::Single, 15_000_001, DOLLAR * 8_800),
        (
            filing::Status::MarriedFilingJointly,
            15_000_001,
            DOLLAR * 8_800,
        ),
        (
            filing::Status::MarriedFilingSeparately,
            7_500_000,
            DOLLAR * 8_000,
        ),
        (
            filing::Status::MarriedFilingSeparately,
            7_500_001,
            DOLLAR * 8_800,
        ),
    ] {
        let r = rules()
            .requirement(
                &Household {
                    status,
                    prior: prior(DOLLAR * 8_000, USCents(agi)),
                    ..h(20_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        assert!(
            r.chosen().unwrap().basis() == Basis::PriorYearTax && r.target() == want,
            "{status:?} with {agi}: {}",
            r.target()
        );
    }

    // Without last year's AGI, 110% is assumed and the AGI asked for.
    let no_agi = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 8_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                prior: Some(no_agi),
                ..h(20_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_800);
    assert!(has(&res.required(), PositionKind::AdjustedGrossIncome));

    // When the prior-year option is not open, and why.
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(
            estimated::new_figures()
                .tax(USCents(100))
                .state_agi(USCents(1_000))
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    for (name, p, why, ask) in [
        (
            "no return on record",
            None,
            "Needs your 2025 Kentucky return",
            true,
        ),
        (
            "not filed",
            Some(not_filed),
            "didn't file a 2025 Kentucky return",
            false,
        ),
        ("a short year", Some(short), "covered 6 months", false),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: p,
                    ..h(10_000)
                }
                .facts(),
            )
            .unwrap();
        let o = res.value().options()[1].clone();
        assert!(!o.available() && o.why_not().contains(why), "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn),
            ask,
            "{name}"
        );
    }
}

// KY-TAM-19-02 adds the incomes of a couple filing separately on a combined
// return. Which step the sum is tested against is not settled, so between
// $75,000 and $150,000 the $75,000 step is used and the question reported.
#[test]
fn combined_return() {
    let combined = |spouse: Option<USCents>| {
        let mut b = kentuckytaxcalc::new_estimated_facts();
        b.non_wage_income(DOLLAR * 100_000)
            .filing_threshold_met(true)
            .prior_year_us_resident(true)
            .combined_return(true);
        if let Some(s) = spouse {
            b.spouse_prior_year_agi(s);
        }
        b.build().unwrap()
    };
    let mut hh = Household {
        status: filing::Status::MarriedFilingSeparately,
        prior: prior(DOLLAR * 8_000, DOLLAR * 50_000),
        extras: Some(combined(Some(DOLLAR * 50_000))),
        ..h(20_000)
    };
    let res = rules().requirement(&hh.facts()).unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_800);
    let r = find(&res.required(), PositionKind::UnsettledRule).unwrap();
    assert!(
        r.question().is_empty() && r.effect().contains("$75,000 for married filing separately")
    );

    // Under $75,000 together, 100% and no question.
    hh.extras = Some(combined(Some(DOLLAR * 20_000)));
    let res = rules().requirement(&hh.facts()).unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_000);
    assert!(!has(&res.required(), PositionKind::AdjustedGrossIncome));

    // The spouse's AGI not given: 110% assumed and asked.
    hh.extras = Some(combined(None));
    let res = rules().requirement(&hh.facts()).unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_800);
    assert!(has(&res.required(), PositionKind::AdjustedGrossIncome));

    // Married filing separately, combined or not unsaid: asked, own AGI used.
    hh.extras = Some(
        kentuckytaxcalc::new_estimated_facts()
            .non_wage_income(DOLLAR * 100_000)
            .filing_threshold_met(true)
            .prior_year_us_resident(true)
            .build()
            .unwrap(),
    );
    let res = rules().requirement(&hh.facts()).unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_000);
    assert!(has(&res.required(), PositionKind::JurisdictionFact));
}

// No Kentucky liability last year, on a full-year return, as a U.S. citizen
// or resident alien, means no penalty.
#[test]
fn no_liability_last_year() {
    let zero = prior(USCents(0), DOLLAR * 30_000);
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let short_zero = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(estimated::new_figures().tax(USCents(0)).build().unwrap())
        .build()
        .unwrap();
    for (name, p, exempt) in [
        ("no tax shown", zero, true),
        ("no return required", Some(not_required), true),
        ("no tax, eleven months", Some(short_zero), false),
    ] {
        let hh = Household {
            prior: p,
            ..h(10_000)
        };
        assert_eq!(
            rules().requirement(&hh.facts()).unwrap().value().exempt(),
            exempt,
            "{name}"
        );
        let pen = rules()
            .penalty(&hh.facts(), day(2027, 4, 15))
            .unwrap()
            .value();
        if exempt {
            assert_eq!(pen.total(), USCents(0), "{name}");
        }
    }
    // Citizenship or residence not said: asked, and not applied.
    let e = kentuckytaxcalc::new_estimated_facts()
        .non_wage_income(DOLLAR * 100_000)
        .filing_threshold_met(true)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                prior: zero,
                extras: Some(e),
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(!res.value().exempt() && has(&res.required(), PositionKind::JurisdictionFact));
}

// Four quarters of 25%, with a payment applied to the oldest shortfall
// first, charged 9% a year by the day. April's $1,000 shortfall is paid by
// the June payment: 61 days × 9% ÷ 365 × $1,000 is $15.04.
#[test]
fn penalty_arithmetic() {
    let hh = Household {
        payments: quarterly(&[1_250, 3_250, 2_250, 2_250]),
        ..h(10_000)
    };
    let sched = rules().schedule(&hh.facts()).unwrap().value();
    for (i, want) in [2_250, 4_500, 6_750, 9_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), DOLLAR * want);
    }
    assert!(
        sched[0].status() == Status::Short
            && sched[0].shortfall() == DOLLAR * 1_000
            && sched[1].status() == Status::Met
    );
    let p = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
    let lines = p.value().lines();
    assert!(lines.len() == 1 && lines[0].days() == 61 && p.value().total() == USCents(1_504));
    let parts = lines[0].parts();
    assert!(parts.len() == 1 && parts[0].rate() == percent(9));

    // The penalty never runs past April 15, 2027, and stops on the day a
    // return is filed and paid.
    let p = rules()
        .penalty(&h(10_000).facts(), day(2027, 6, 30))
        .unwrap()
        .value();
    assert_eq!(p.as_of(), day(2027, 4, 15));
    let p = rules()
        .penalty(
            &Household {
                return_filed: day(2027, 3, 1),
                ..h(10_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap()
        .value();
    assert_eq!(p.as_of(), day(2027, 3, 1));
}

// Kentucky publishes one rate for each calendar year, and the 2026 rate is
// the only one a 2026 shortfall can run under before 2027.
#[test]
fn penalty_across_rate_periods() {
    // April's $1,000, paid on September 15 with nothing in between: 153 days
    // in one part at 9%, $37.73.
    let hh = Household {
        payments: vec![
            pay(dues()[0], DOLLAR * 1_250),
            pay(dues()[2], DOLLAR * 1_000),
        ],
        ..h(10_000)
    };
    let p = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
    let parts = p.value().lines()[0].parts();
    assert!(
        parts.len() == 1 && parts[0].days() == 153 && parts[0].charge() == USCents(3_773),
        "{parts:?}"
    );

    // September's $2,250, never paid: 107 days of 2026 at 9%, $59.36, and
    // the 2027 days are reported rather than charged.
    let hh = Household {
        payments: quarterly(&[2_250, 2_250]),
        ..h(10_000)
    };
    let p = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
    let lines = p.value().lines();
    assert!(lines.len() == 2 && lines[0].installment() == 3);
    let parts = lines[0].parts();
    assert!(
        parts.len() == 1
            && parts[0].days() == 107
            && parts[0].charge() == USCents(5_936)
            && parts[0].to() == day(2026, 12, 31),
        "{parts:?}"
    );
    assert!(lines[1].parts().is_empty());
    let gap = p.required().iter().any(|r| {
        r.position() == PositionKind::UnpublishedRate
            && r.effect()
                .contains("Kentucky rate for the days after December 31, 2026 isn't published yet")
    });
    assert!(gap && count(&p.required(), PositionKind::UnpublishedRate) == 2);
    assert_eq!(p.value().total(), USCents(5_936));
}

// The rate is the form's 9%, and the 7% reading is an open question.
#[test]
fn rate_is_an_open_question() {
    let p = rules()
        .penalty(&h(10_000).facts(), day(2026, 12, 31))
        .unwrap();
    assert!(p.required().iter().any(|r| {
        r.position() == PositionKind::UnpublishedRate
            && r.effect().contains("9%")
            && r.effect().contains("7%")
    }));
    // The figure uses the form's 9%, and the reading affects this penalty.
    for d in p.divergences() {
        if d.rule() == "The penalty rate for 2026" {
            assert!(
                d.uses() == Reading::Form
                    && d.affects_household()
                    && d.form().contains("July 15, 2026")
            );
        }
    }
    // No penalty, no question.
    let p = rules()
        .penalty(
            &Household {
                payments: quarterly(&[2_250, 2_250, 2_250, 2_250]),
                ..h(10_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert_eq!(p.value().total(), USCents(0));
    assert!(!has(&p.required(), PositionKind::UnpublishedRate));
}

// The January voucher is excused for a return filed and paid by February 1,
// 2027 only when the requirement first arose after September 1, 2026.
#[test]
fn january_installment() {
    for (first_met, filed, excused) in [
        (day(2026, 9, 2), day(2027, 2, 1), true),
        (day(2026, 9, 1), day(2027, 2, 1), false),
        (day(2026, 9, 2), day(2027, 2, 2), false),
        (day(2026, 1, 1), day(2027, 1, 20), false),
    ] {
        let hh = Household {
            first_met,
            return_filed: filed,
            ..h(10_000)
        };
        let got = rules().schedule(&hh.facts()).unwrap().value()[3].status() == Status::Excused;
        assert_eq!(got, excused, "first met {first_met}, filed {filed}");
    }
    let res = rules()
        .requirement(
            &Household {
                return_filed: day(2027, 1, 20),
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(has(&res.required(), PositionKind::RequirementFirstMet));
}

// Form 2210-K Part III: 22.5%, 45%, 67.5% and 90% of the annualised tax,
// less earlier installments, capped by the regular installment plus what was
// carried forward.
#[test]
fn annualised() {
    let hh = Household {
        annualised: Some(vec![
            DOLLAR * 2_000,
            DOLLAR * 4_000,
            DOLLAR * 8_000,
            DOLLAR * 10_000,
        ]),
        ..h(10_000)
    };
    let sched = rules().schedule(&hh.facts()).unwrap().value();
    for (i, want) in [450, 1_800, 5_400, 9_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), DOLLAR * want);
    }
    // The periods must be the form's.
    let ps: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 6, 30),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .into_iter()
    .map(|end| estimated::new_period().end(end).build().unwrap())
    .collect();
    let f = estimated::new_facts(KY, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .elections(estimated::new_elections().annualised().build().unwrap())
        .periods(&ps)
        .build()
        .unwrap();
    assert!(rules().schedule(&f).is_err());
}

// Withholding is spread 25% to each date, even when the election is made.
#[test]
fn withholding_is_spread_evenly() {
    let hh = Household {
        withheld: DOLLAR * 4_000,
        as_withheld: true,
        ..h(10_000)
    };
    let res = rules().schedule(&hh.facts()).unwrap();
    assert_eq!(res.value()[0].credited_by(), DOLLAR * 1_000);
    assert!(applied(&res.applied(), "offers no election"));
}

// Kentucky's waivers are for reasonable cause only.
#[test]
fn waivers() {
    let mut hh = h(10_000);
    let p = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
    assert!(has(&p.required(), PositionKind::PenaltyWaiver));
    hh.waiver = Some(WaiverKind::RetiredOrDisabled);
    let p = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
    assert!(!has(&p.required(), PositionKind::PenaltyWaiver));
    assert!(applied(
        &p.applied(),
        "no waiver for retirement or disability"
    ));
    assert!(p.value().waiver() == WaiverKind::RetiredOrDisabled && p.value().total().0 != 0);
}

// A casualty can be waived as reasonable cause; retirement can't, at any age
// or date, unlike the federal waiver.
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
    for (name, p, eligible, want) in [
        (
            "disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
            true,
            "destroyed or kept from you",
        ),
        (
            "retired in 2026 at 64",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1962, 3, 1)),
            ),
            false,
            "not retirement or disability alone",
        ),
        (
            "retired in 2020",
            pos(
                WaiverCause::Retirement,
                day(2020, 6, 1),
                Some(day(1955, 3, 1)),
            ),
            false,
            "not retirement or disability alone",
        ),
        (
            "disabled in 2026",
            pos(WaiverCause::Disability, day(2026, 6, 1), None),
            false,
            "not retirement or disability alone",
        ),
    ] {
        let pen = rules()
            .penalty(
                &Household {
                    positions: Some(p),
                    ..h(10_000)
                }
                .facts(),
                day(2027, 4, 15),
            )
            .unwrap()
            .value();
        let o = pen.waiver_outcome().unwrap();
        assert!(
            o.eligible() == eligible && o.refused() != eligible && o.reason().contains(want),
            "{name}: {o:?}"
        );
        assert!(pen.total().0 != 0, "{name}");
        let want_waived = if eligible { USCents(0) } else { pen.total() };
        assert_eq!(pen.if_waived(), want_waived, "{name}");
    }
}

// Each divergence is reported with both readings and both sources, and
// follows the form.
#[test]
fn divergences_pin_both_readings() {
    let res = rules()
        .penalty(&h(10_000).facts(), day(2027, 4, 15))
        .unwrap();
    let want: [(&str, [&str; 4], bool); 4] = [
        (
            "Exactly $500",
            ["$500 or less", "line 5", "$500 or less", "KRS 141.305(2)"],
            false,
        ),
        (
            "The penalty rate for 2026",
            ["9%", "line 21", "7%", "2026 Ky. Acts ch. 161"],
            true,
        ),
        (
            "A return filed and paid by February 1",
            [
                "after September 1, 2026",
                "42A740-S4",
                "files and pays in full by January 31",
                "§ 6654(h)",
            ],
            false,
        ),
        (
            "When withholding counts as paid",
            [
                "25% on each due date",
                "line 10",
                "actually withheld",
                "§ 6654(g)(1)",
            ],
            false,
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, w, affects)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(
            d.form().contains(w[0])
                && d.form_source().contains(w[1])
                && d.statute().contains(w[2])
                && d.statute_source().contains(w[3]),
            "{:?}",
            d.rule()
        );
        assert_eq!(d.uses(), Reading::Form);
        // $10,000 of tax, nothing withheld and no return filed: only the
        // rate reaches these facts.
        assert_eq!(d.affects_household(), *affects, "{:?}", d.rule());
    }
    let affects = |hh: Household, rule: &str| {
        rules()
            .requirement(&hh.facts())
            .unwrap()
            .divergences()
            .iter()
            .any(|d| d.rule() == rule && d.affects_household())
    };
    assert!(affects(
        Household {
            withheld: USCents(950_000),
            ..h(10_000)
        },
        "Exactly $500"
    ));
    assert!(affects(
        Household {
            first_met: day(2026, 9, 1),
            return_filed: day(2027, 2, 1),
            ..h(10_000)
        },
        "A return filed and paid by February 1"
    ));
    assert!(affects(
        Household {
            withheld: DOLLAR * 1_000,
            ..h(10_000)
        },
        "When withholding counts as paid"
    ));
    assert!(!affects(
        Household {
            first_met: day(2026, 9, 2),
            return_filed: day(2027, 2, 1),
            ..h(10_000)
        },
        "A return filed and paid by February 1"
    ));
}

#[test]
fn refuses_what_is_not_built() {
    for year in [2025, 2027] {
        assert!(
            kentuckytaxcalc::estimated_tax(year)
                .err()
                .is_some_and(|e| e.is(&estimated::Error::NoRules))
        );
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    for (name, f) in [
        (
            "a farmer",
            estimated::new_facts(KY, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .farm_share(Rate(6_667))
                .build()
                .unwrap(),
        ),
        (
            "a fisher",
            estimated::new_facts(KY, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .fish_share(Rate(6_667))
                .build()
                .unwrap(),
        ),
    ] {
        assert!(
            rules()
                .requirement(&f)
                .unwrap_err()
                .is(&estimated::Error::Unsupported),
            "{name}"
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
            estimated::new_facts(KY, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(KY, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("not Kentucky's")
                .build()
                .unwrap(),
        ),
    ];
    for (name, f) in cases {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        kentuckytaxcalc::new_estimated_facts()
            .non_wage_income(USCents(-1))
            .build()
            .is_err()
    );
    assert!(
        kentuckytaxcalc::new_estimated_facts()
            .spouse_prior_year_agi(USCents(1))
            .build()
            .is_err()
    );
}

#[test]
fn explanation_names_the_differences() {
    let e = rules().explanation();
    let all = format!(
        "{}{}{}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$500",
        "$5,000",
        "110%",
        "combined return",
        "9%",
        "7%",
        "September 1",
        "reasonable cause",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
}

// Without this year's tax the target is last year's figure alone, and the
// penalty against it is an upper bound.
#[test]
fn without_this_years_tax() {
    let hh = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000, DOLLAR * 100_000),
        ..Household::default()
    };
    let res = rules().requirement(&hh.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 8_000, "{}", r.why());
    let o = r.options()[0].clone();
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let p = rules()
        .penalty(&hh.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!((p.upper_bound() || !p.rated_through().is_zero()) && p.total().0 != 0);

    // No return on record either: no target.
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
    assert!(
        rules()
            .schedule(&none.facts())
            .unwrap_err()
            .is(&estimated::Error::NeedsThisYearTax)
    );
    assert!(
        rules()
            .penalty(&none.facts(), day(2027, 4, 15))
            .unwrap_err()
            .is(&estimated::Error::NeedsThisYearTax)
    );

    // No liability last year needs nothing about this year: an exact zero.
    let zero = Household {
        no_tax: true,
        prior: prior(USCents(0), DOLLAR * 20_000),
        ..Household::default()
    };
    let p = rules()
        .penalty(&zero.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!(p.total().0 == 0 && !p.upper_bound());
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
    let f = estimated::new_facts(KY, 2026)
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
