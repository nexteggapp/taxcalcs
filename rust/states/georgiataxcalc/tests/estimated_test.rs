//! Georgia's estimated tax rules for 2026, checked against the Department of
//! Revenue's forms and HB 463.
//!
//! Georgia publishes no exhaustive oracle for estimated tax. The strongest
//! check is Form 500 UET's worked example of Exception 3, in which the
//! fourth installment is met by its own period's payment although the year
//! as a whole is short. The boundaries, the other exceptions, the January 31
//! rule and the penalty arithmetic are hand-written cases. Go's
//! TestEstimatedFiguresAreInTheirSources and the testdata/ phrase checks in
//! the example test are not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status, WaiverCause,
    WaiverKind, Weekday,
};
use filing::Status as FilingStatus;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_georgiataxcalc as georgiataxcalc;
use working::{PositionKind, Reading, Required};

use georgiataxcalc::EstimatedFacts;

const GEORGIA: estimated::Jurisdiction = estimated::GEORGIA;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    georgiataxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind, text: &str) -> bool {
    rs.iter().any(|r| {
        r.position() == k && r.asked() && (r.question().contains(text) || r.effect().contains(text))
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

/// One payment of each amount on each due date.
fn quarterly(a: &[USCents]) -> Vec<Payment> {
    a.iter().zip(dues()).map(|(&x, d)| pay(d, x)).collect()
}

/// One Form 500 UET computation period: the taxable income to its end, for
/// Exception 3, and the tax on that income annualised, for Exception 2.
#[derive(Clone, Copy)]
struct Period {
    income: USCents,
    annualised_tax: USCents,
}

fn period(income: USCents, annualised_tax: USCents) -> Period {
    Period {
        income,
        annualised_tax,
    }
}

#[derive(Clone, Default)]
struct Household {
    status: FilingStatus,
    /// Form 500 UET line 3: tax after credits.
    tax: USCents,
    no_tax: bool,
    /// Zero means $100,000.
    gross: USCents,
    no_gross: bool,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    periods: Option<Vec<Period>>,
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let status = if self.status == FilingStatus::Unspecified {
            FilingStatus::Single
        } else {
            self.status
        };
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        if !self.no_gross {
            let gross = if self.gross.0 == 0 {
                DOLLAR * 100_000
            } else {
                self.gross
            };
            fb.gross_income(gross);
        }
        let mut b = estimated::new_facts(GEORGIA, 2026);
        b.filing_status(status)
            .this_year(fb.build().unwrap())
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
        match self.extras {
            Some(e) => b.state(e),
            None => b.state(
                georgiataxcalc::new_estimated_facts()
                    .dependents(0)
                    .deductions(DOLLAR * 15_000)
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
            for (i, p) in ps.iter().enumerate() {
                b.periods(&[estimated::new_period()
                    .end(ends[i])
                    .income(p.income)
                    .annualised_tax(p.annualised_tax)
                    .build()
                    .unwrap()]);
            }
            b.elections(estimated::new_elections().annualised().build().unwrap());
        }
        b.build().unwrap()
    }
}

/// A filed 12-month return with its tax and taxable income.
fn prior(tax: USCents, taxable: USCents) -> Option<PriorReturn> {
    let f = estimated::new_figures()
        .tax(tax)
        .taxable_income(taxable)
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

/// The 2026 dates are the 500-ES's. None falls on a weekend, so none moves.
/// January 31, 2027, the Instruction F date, is a Sunday; the form does not
/// move it.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&tax(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    assert_eq!(day(2027, 1, 31).weekday(), Weekday::SUNDAY);
    let paid = quarterly(&[DOLLAR * 1_750, DOLLAR * 1_750, DOLLAR * 1_750]);
    let h = Household {
        payments: paid.clone(),
        return_filed: day(2027, 1, 31),
        ..tax(DOLLAR * 10_000)
    };
    let sunday = rules().schedule(&h.facts()).unwrap();
    assert_eq!(sunday.value()[3].status(), Status::Met);
    let h = Household {
        payments: paid,
        return_filed: day(2027, 2, 1),
        ..tax(DOLLAR * 10_000)
    };
    let monday = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(has(
        &monday.required(),
        PositionKind::UnsettledRule,
        "Jan 31, 2027, a Sunday"
    ));
}

/// Estimated tax is required when gross income exceeds the deductions, the
/// dependent exemptions and $1,000. Equal to the sum is not over it.
#[test]
fn estimated_threshold_boundaries() {
    let two = georgiataxcalc::new_estimated_facts()
        .dependents(2)
        .build()
        .unwrap();
    for (name, status, at) in [
        // $15,000 standard deduction, two dependents at $5,000, and $1,000.
        (
            "single, two dependents",
            FilingStatus::Single,
            DOLLAR * 26_000,
        ),
        (
            "head of household, two dependents",
            FilingStatus::HeadOfHousehold,
            DOLLAR * 26_000,
        ),
        (
            "separate, two dependents",
            FilingStatus::MarriedFilingSeparately,
            DOLLAR * 26_000,
        ),
        // $30,000 for a joint return.
        (
            "joint, two dependents",
            FilingStatus::MarriedFilingJointly,
            DOLLAR * 41_000,
        ),
    ] {
        for gross in [at, at + USCents(1)] {
            let h = Household {
                status,
                gross,
                extras: Some(two),
                ..tax(DOLLAR * 2_000)
            };
            let res = rules().requirement(&h.facts()).unwrap();
            assert_eq!(res.value().exempt(), gross == at, "{name}, gross {gross}");
            if gross == at {
                // The exception is chosen at $0, and the form's 2025 figures,
                // $5,000 lower or more, would have required payments.
                let c = res.value().chosen().unwrap();
                assert!(
                    c.basis() == Basis::Exception
                        && c.amount().0 == 0
                        && c.label() == format!("Gross income of {} or less", at.whole()),
                    "{name}: {c:?}"
                );
                let ds = res.divergences();
                assert!(ds.len() == 1 && ds[0].affects_household(), "{name}");
            }
            // The deductions were not given, so the standard deduction is a
            // stated default.
            assert!(
                has(
                    &res.required(),
                    PositionKind::JurisdictionFact,
                    "standard deduction"
                ),
                "{name}"
            );
        }
    }

    // A surviving spouse has no standard deduction of its own in the act.
    let h = Household {
        status: FilingStatus::SurvivingSpouse,
        gross: DOLLAR * 16_000,
        extras: Some(EstimatedFacts::default()),
        ..tax(DOLLAR * 2_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(
        res.value().exempt()
            && has(
                &res.required(),
                PositionKind::JurisdictionFact,
                "uses the $15,000 single amount"
            )
    );

    // Without gross income the requirement is taken as met, and asked.
    let h = Household {
        no_gross: true,
        ..tax(DOLLAR * 2_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(
        !res.value().exempt()
            && has(
                &res.required(),
                PositionKind::JurisdictionFact,
                "gross income"
            )
    );
}

/// The target is the smaller of 70% of this year's tax and 100% of last
/// year's, whatever last year's income.
#[test]
fn estimated_target_options() {
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(estimated::new_figures().tax(USCents(1)).build().unwrap())
        .build()
        .unwrap();
    let not_filed = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let rich = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 6_000)
                .federal_agi(DOLLAR * 5_000_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    for (name, p, target, basis, open, kind, asked) in [
        (
            "70% is smaller",
            prior(DOLLAR * 8_000, USCents(0)),
            DOLLAR * 7_000,
            Basis::ThisYearTax,
            true,
            PositionKind::UnsettledRule,
            "before or after credits",
        ),
        (
            "100% is smaller",
            prior(DOLLAR * 6_000, USCents(0)),
            DOLLAR * 6_000,
            Basis::PriorYearTax,
            true,
            PositionKind::Unspecified,
            "",
        ),
        (
            "no 110% at high income",
            Some(rich),
            DOLLAR * 6_000,
            Basis::PriorYearTax,
            true,
            PositionKind::Unspecified,
            "",
        ),
        (
            "no tax last year",
            prior(USCents(0), USCents(0)),
            USCents(0),
            Basis::PriorYearTax,
            true,
            PositionKind::Unspecified,
            "",
        ),
        (
            "no prior return",
            None,
            DOLLAR * 7_000,
            Basis::ThisYearTax,
            false,
            PositionKind::PriorYearReturn,
            "Did you file a 2025 Georgia return",
        ),
        (
            "none filed",
            Some(not_filed),
            DOLLAR * 7_000,
            Basis::ThisYearTax,
            false,
            PositionKind::UnsettledRule,
            "open with no 2025 Georgia return",
        ),
        (
            "a short year",
            Some(short),
            DOLLAR * 7_000,
            Basis::ThisYearTax,
            false,
            PositionKind::Unspecified,
            "",
        ),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let r = res.value();
        let c = r.chosen().unwrap_or_default();
        assert!(r.target() == target && c.basis() == basis, "{name}");
        let o = &r.options()[1];
        assert!(
            o.available() == open && o.percent() == percent(100),
            "{name}"
        );
        assert_eq!(r.options()[0].percent(), percent(70));
        if !asked.is_empty() {
            assert!(has(&res.required(), kind, asked), "{name}: {asked:?}");
        }
    }
    // No tax last year: nothing is required, by the form's arithmetic.
    let h = Household {
        prior: prior(USCents(0), USCents(0)),
        ..tax(DOLLAR * 10_000)
    };
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total().0, 0);
}

/// A requirement first met later in the year is paid in fewer, larger
/// installments, by Form 500 UET Instruction B's chart.
#[test]
fn estimated_late_start_chart() {
    for (met, needed) in [
        (day(2026, 3, 31), [175_000, 350_000, 525_000, 700_000]),
        // $7,000 in thirds: $2,333.33, $4,666.67 and $7,000.
        (day(2026, 4, 1), [0, 233_333, 466_667, 700_000]),
        (day(2026, 5, 31), [0, 233_333, 466_667, 700_000]),
        (day(2026, 6, 1), [0, 0, 350_000, 700_000]),
        (day(2026, 8, 31), [0, 0, 350_000, 700_000]),
        (day(2026, 9, 1), [0, 0, 0, 700_000]),
    ] {
        let h = Household {
            first_met: met,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().schedule(&h.facts()).unwrap();
        for (i, inst) in res.value().iter().enumerate() {
            assert_eq!(
                inst.needed_by(),
                USCents(needed[i]),
                "first met {met}: {}",
                i + 1
            );
        }
        // Nothing short before the first installment that applies.
        let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        let zeros = needed.iter().filter(|&&x| x == 0).count();
        assert_eq!(p.value().lines().len(), 4 - zeros, "first met {met}");
    }
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    assert!(has(
        &res.required(),
        PositionKind::RequirementFirstMet,
        "on or after Apr 1"
    ));
}

/// Nothing paid on $1,750 a quarter: 9% a year by the day to April 15, 2027.
#[test]
fn estimated_penalty_arithmetic() {
    let res = rules()
        .penalty(&tax(DOLLAR * 10_000).facts(), day(2027, 6, 30))
        .unwrap();
    let p = res.value();
    assert_eq!(p.as_of(), day(2027, 4, 15));
    let want_days = [365, 304, 212, 90];
    let want_charge = [15_750, 13_118, 9_148, 3_884];
    let lines = p.lines();
    assert_eq!(lines.len(), 4);
    for (i, l) in lines.iter().enumerate() {
        let parts = l.parts();
        assert!(
            l.shortfall() == DOLLAR * 1_750
                && parts.len() == 1
                && parts[0].days() == want_days[i]
                && parts[0].charge() == USCents(want_charge[i])
                && parts[0].rate() == percent(9),
            "line {}",
            i + 1
        );
    }
    assert_eq!(p.total(), USCents(41_900));
    assert!(!p.upper_bound() && !has(&res.required(), PositionKind::UnpublishedRate, ""));
}

/// A payment counts first toward its own period's installment.
#[test]
fn estimated_payments_go_to_their_own_period_first() {
    let d = dues();
    let h = Household {
        payments: vec![
            pay(d[0], DOLLAR * 1_000),
            pay(day(2026, 5, 1), DOLLAR * 750),
            pay(d[1], DOLLAR * 1_750),
            pay(d[2], DOLLAR * 1_750),
            pay(d[3], DOLLAR * 1_750),
        ],
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let lines = res.value().lines();
    assert!(
        lines.len() == 1 && lines[0].installment() == 1 && lines[0].shortfall() == DOLLAR * 750
    );
    let parts = lines[0].parts();
    assert!(
        parts.len() == 1
            && parts[0].to() == d[1]
            && parts[0].charge() == USCents(1_128)
            && res.value().total() == USCents(1_128)
    );
    assert!(has(
        &res.required(),
        PositionKind::UnsettledRule,
        "pays off an earlier shortfall"
    ));
}

/// Form 500 UET's example of Exception 3, on a joint return with $779 of
/// tax.
#[test]
fn estimated_matches_form_500_uet_example() {
    // Exception 2 is put out of reach with a large annualised tax.
    let big = DOLLAR * 1_000_000;
    let mut h = Household {
        status: FilingStatus::MarriedFilingJointly,
        payments: quarterly(&[DOLLAR * 109, DOLLAR * 73, DOLLAR * 109, DOLLAR * 145]),
        periods: Some(vec![
            period(USCents(0), big),
            period(DOLLAR * 1_800, big),
            period(DOLLAR * 5_800, big),
            period(DOLLAR * 15_000, big),
        ]),
        ..tax(DOLLAR * 779)
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    let want = [
        Status::Excused,
        Status::Excused,
        Status::Excused,
        Status::Short,
    ];
    let want_short = [2_733, 6_332, 2_733, 0];
    for (i, inst) in sched.value().iter().enumerate() {
        assert!(
            inst.status() == want[i] && inst.shortfall() == USCents(want_short[i]),
            "installment {}: {inst:?}",
            i + 1
        );
    }
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total().0, 0);
    // Without Exception 3 the first three are charged, and January is still
    // met by its own payment.
    h.periods = None;
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let lines = p.value().lines();
    assert!(lines.len() == 3 && lines[2].installment() == 3);
}

/// Exception 1: last year's $120,000 of taxable income at 4.99% is $5,988, a
/// quarter of which is $1,497.
#[test]
fn estimated_exception_one() {
    let mut h = Household {
        prior: prior(DOLLAR * 8_000, DOLLAR * 120_000),
        payments: quarterly(&[
            USCents(149_700),
            DOLLAR * 1_503,
            DOLLAR * 1_500,
            DOLLAR * 1_500,
        ]),
        ..tax(DOLLAR * 10_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, inst) in sched.value().iter().enumerate() {
        assert!(
            inst.status() == Status::Excused && inst.shortfall().0 != 0,
            "installment {}",
            i + 1
        );
    }
    assert!(has(
        &sched.required(),
        PositionKind::UnsettledRule,
        "income at current rates and exemptions"
    ));

    h.payments = quarterly(&[
        DOLLAR * 1_490,
        DOLLAR * 1_510,
        DOLLAR * 1_500,
        DOLLAR * 1_500,
    ]);
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let lines = p.value().lines();
    assert!(
        lines.len() == 1
            && lines[0].installment() == 1
            && lines[0].shortfall() == DOLLAR * 260
            && p.value().total() == USCents(2_340)
    );

    // Without last year's taxable income the exception is not applied.
    let no_income = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 8_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    h.prior = Some(no_income);
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(has(
        &res.required(),
        PositionKind::PriorYearReturn,
        "What was your 2025 Georgia taxable income?"
    ));
}

/// Exception 2 is 70% of the annualised tax, by a quarter for each
/// installment so far. Exception 3 is 90% of 4.99% of the income to date.
#[test]
fn estimated_exceptions_two_and_three() {
    let big = DOLLAR * 10_000_000;
    let four = DOLLAR * 4_000;
    let mut h = Household {
        payments: quarterly(&[DOLLAR * 700, DOLLAR * 700, DOLLAR * 700, DOLLAR * 700]),
        periods: Some(vec![period(big, four); 4]),
        ..tax(DOLLAR * 10_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, inst) in sched.value().iter().enumerate() {
        assert_eq!(inst.status(), Status::Excused, "Exception 2: {}", i + 1);
    }
    assert!(has(
        &sched.required(),
        PositionKind::UnsettledRule,
        "25% for the first, then 50%, 75% and 100%"
    ));
    h.payments = quarterly(&[USCents(69_999), DOLLAR * 700, DOLLAR * 700, DOLLAR * 700]);
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].status(), Status::Short);

    for (paid, excused) in [(89_820, true), (89_819, false)] {
        let h = Household {
            payments: quarterly(&[USCents(paid)]),
            periods: Some(vec![
                period(DOLLAR * 20_000, big),
                period(big, big),
                period(big, big),
                period(big, big),
            ]),
            ..tax(DOLLAR * 10_000)
        };
        let s = rules().schedule(&h.facts()).unwrap().value();
        assert_eq!(
            s[0].status() == Status::Excused,
            excused,
            "Exception 3 with {paid}"
        );
    }
    // Exception 3 never excuses January.
    let h = Household {
        periods: Some(vec![period(USCents(0), big); 4]),
        ..tax(DOLLAR * 10_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert!(s[2].status() == Status::Excused && s[3].status() == Status::Short);
    // The periods must be Form 500 UET's.
    let periods: Vec<_> = [
        day(2026, 3, 30),
        day(2026, 5, 31),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .into_iter()
    .map(|d| estimated::new_period().end(d).build().unwrap())
    .collect();
    let bad = estimated::new_facts(GEORGIA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(estimated::new_figures().tax(USCents(1)).build().unwrap())
        .periods(&periods)
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    assert!(rules().requirement(&bad).is_err());
}

/// Instruction F: the balance paid with a return filed by January 31 counts
/// as paid on January 15.
#[test]
fn estimated_return_filed_by_january_31() {
    let h = Household {
        return_filed: day(2027, 1, 20),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(res.value().total() == USCents(26_364) && res.value().lines().len() == 3);
    assert_eq!(res.value().as_of(), day(2027, 1, 20));
    let paid = quarterly(&[DOLLAR * 1_750, DOLLAR * 1_750, DOLLAR * 1_750]);
    let late = Household {
        payments: paid.clone(),
        return_filed: day(2027, 2, 10),
        ..tax(DOLLAR * 10_000)
    };
    let p = rules().penalty(&late.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(1_122));
    let monday = Household {
        payments: paid,
        return_filed: day(2027, 2, 1),
        ..tax(DOLLAR * 10_000)
    };
    // 17 days: $1,750 × 9% × 17 ÷ 365 = $7.34.
    let p = rules().penalty(&monday.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(734));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let ds = res.divergences();
    assert_eq!(ds.len(), 1);
    let d = &ds[0];
    assert!(d.uses() == Reading::Statute && !d.affects_household());
    assert!(
        d.rule() == "The deductions in the test of whether estimated tax is required"
            && d.form().contains("$12,000")
            && d.form().contains("$4,000")
            && d.form().contains("out of date")
            && d.form_source().contains("Rev. 05/12/25")
            && d.statute().contains("$15,000")
            && d.statute().contains("$30,000")
            && d.statute().contains("$5,000")
            && d.statute().contains("May 11, 2026")
            && d.statute_source().contains("HB 463"),
        "{d:?}"
    );
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for y in [2025, 2027] {
        let err = georgiataxcalc::estimated_tax(y).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{y}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    for (farm, fish) in [(7_000, 0), (0, 7_000), (4_000, 3_000)] {
        let f = estimated::new_facts(GEORGIA, 2026)
            .filing_status(FilingStatus::Single)
            .this_year(this)
            .farm_share(Rate(farm))
            .fish_share(Rate(fish))
            .build()
            .unwrap();
        let err = rules().requirement(&f).err().unwrap();
        assert!(
            err.is(&estimated::Error::Unsupported),
            "{farm}, {fish}: {err}"
        );
    }
    for (name, f) in [
        (
            "California's facts",
            estimated::new_facts(estimated::CALIFORNIA, 2026)
                .filing_status(FilingStatus::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(GEORGIA, 2027)
                .filing_status(FilingStatus::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(GEORGIA, 2026)
                .filing_status(FilingStatus::Single)
                .this_year(this)
                .state("not Georgia's")
                .build()
                .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        georgiataxcalc::new_estimated_facts()
            .dependents(-1)
            .build()
            .is_err()
    );
    assert!(
        rules()
            .penalty(&tax(USCents(1)).facts(), Date::default())
            .is_err()
    );
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
        "70% of this year's tax",
        "100% of last year's",
        "no 110% tier",
        "9% a year",
        "4.99%",
        "gross income",
        "$1,000",
        "$15,000",
        "$5,000",
        "first toward its own period's installment",
        "January 31",
        "no casualty, retirement or disability waiver",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
}

/// Without this year's tax, the target is 100% of last year's, and the
/// penalty against it is an upper bound. With no prior return either, there
/// is no target.
#[test]
fn estimated_without_this_years_tax() {
    // Last year's $200,000 of taxable income puts Exception 1 out of reach.
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000, DOLLAR * 200_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 8_000, "{}", r.why());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax, ""));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, inst) in sched.value().iter().enumerate() {
        assert_eq!(inst.needed_by(), DOLLAR * 2_000 * (i as i64 + 1));
    }
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(p.value().upper_bound() && p.value().total().0 != 0);

    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let r = rules().requirement(&none.facts()).unwrap().value();
    assert!(!r.target_known() && !r.required());
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
    let err = rules()
        .penalty(&none.facts(), day(2027, 4, 15))
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
}

/// An exception sets the target, so no question may say its answer changes
/// the target.
#[test]
fn estimated_exception_asks_nothing_of_the_target() {
    let f = estimated::new_facts(GEORGIA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .gross_income(DOLLAR * 1_000)
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
fn waiver_penalty(pos: Positions) -> working::Result<estimated::Penalty> {
    let f = estimated::new_facts(GEORGIA, 2026)
        .filing_status(FilingStatus::Single)
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

/// Georgia has no waiver for a casualty, retirement or disability, so those
/// are refused. A disaster is left open.
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
    assert_ne!(before.0, 0);
    for (name, pos, want, reason) in [
        (
            "retired in 2026 at 64",
            waiver_for(WaiverCause::Retirement, day(2026, 6, 1), day(1962, 3, 1)),
            "refused",
            "no waiver of the penalty for retirement or disability",
        ),
        (
            "retired in 2020",
            waiver_for(WaiverCause::Retirement, day(2020, 6, 1), day(1955, 3, 1)),
            "refused",
            "retirement or disability",
        ),
        (
            "disabled in 2026",
            waiver_for(WaiverCause::Disability, day(2026, 2, 1), Date::default()),
            "refused",
            "retirement or disability",
        ),
        (
            "a casualty",
            waiver_for(WaiverCause::Casualty, day(2026, 8, 1), Date::default()),
            "refused",
            "for a casualty",
        ),
        (
            "a disaster",
            waiver_for(WaiverCause::Disaster, day(2026, 8, 1), Date::default()),
            "undecided",
            "announces relief",
        ),
    ] {
        let p = waiver_penalty(pos).value();
        let o = p.waiver_outcome().unwrap();
        let (mut got, mut if_waived) = ("undecided", p.total());
        if o.eligible() {
            (got, if_waived) = ("eligible", USCents(0));
        } else if o.refused() {
            got = "refused";
        }
        assert!(got == want && o.reason().contains(reason), "{name}: {got}");
        assert!(p.total() == before && p.if_waived() == if_waived, "{name}");
    }
    let res = waiver_penalty(waiver_for(
        WaiverCause::Disaster,
        day(2026, 8, 1),
        Date::default(),
    ));
    assert!(has(
        &res.required(),
        PositionKind::UnsettledRule,
        "Department announces it"
    ));
    let res = waiver_penalty(waiver_for(
        WaiverCause::Casualty,
        day(2026, 8, 1),
        Date::default(),
    ));
    assert!(!has(
        &res.required(),
        PositionKind::UnsettledRule,
        "Department announces it"
    ));
}

/// Withholding over the tax says it reaches the target, never a minus
/// amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let rules = rules();
    let mut asked = false;
    for a in estimatedtest::ask(&rules, &estimatedtest::households(&GEORGIA)) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked = true;
        let why = a.requirement().unwrap().value().why();
        assert!(
            why.contains("$3,600 of Georgia withholding reaches the $2,100 needed"),
            "why {why:?}"
        );
        for x in estimatedtest::texts(Some(&rules), &[a]) {
            assert!(
                !x.text().contains("−$") && !x.text().contains("-$"),
                "{}: {:?}",
                x.where_(),
                x.text()
            );
        }
    }
    assert!(asked, "no household withholding over the tax");
}
