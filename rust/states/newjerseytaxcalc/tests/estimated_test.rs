//! New Jersey's estimated tax rules for 2026, checked against the
//! Division of Taxation's forms and the statutes.
//!
//! Go's TestEstimatedFiguresAreInTheirSources and the testdata/ phrase
//! checks inside the other tests are not ported.

#![allow(clippy::unwrap_used)]

use std::collections::HashMap;

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status, WaiverCause,
    Weekday,
};
use filing::Status as FilingStatus;
use money::{DOLLAR, Rate, USCents, dollars, percent};
use taxcalcs_newjerseytaxcalc as newjerseytaxcalc;
use working::{PositionKind, Reading, Required};

use newjerseytaxcalc::EstimatedFacts;

const NJ: estimated::Jurisdiction = estimated::NEW_JERSEY;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    newjerseytaxcalc::estimated_tax(2026).unwrap()
}

fn because(rs: &[Required], k: PositionKind, phrase: &str) -> bool {
    rs.iter().any(|r| {
        r.position() == k
            && r.asked()
            && (r.question().contains(phrase) || r.effect().contains(phrase))
    })
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    because(rs, k, "")
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

/// One payment of each amount on each due date, leaving out zeros.
fn quarterly(a: &[USCents]) -> Vec<Payment> {
    a.iter()
        .zip(dues())
        .filter(|(x, _)| x.0 > 0)
        .map(|(&x, d)| pay(d, x))
        .collect()
}

#[derive(Clone, Default)]
struct Household {
    tax: USCents,
    no_tax: bool,
    gross: USCents,
    status: FilingStatus,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    return_filed: Date,
    annualised: Option<Vec<USCents>>,
    farm_share: Rate,
    fish_share: Rate,
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
        let mut fb = estimated::new_figures();
        if !self.no_tax {
            fb.tax(self.tax);
        }
        if self.gross.0 > 0 {
            fb.gross_income(self.gross);
        }
        let status = if self.status == FilingStatus::Unspecified {
            FilingStatus::Single
        } else {
            self.status
        };
        let mut b = estimated::new_facts(NJ, 2026);
        b.filing_status(status)
            .this_year(fb.build().unwrap())
            .paid(&self.payments)
            .return_filed(self.return_filed)
            .farm_share(self.farm_share)
            .fish_share(self.fish_share);
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
                newjerseytaxcalc::new_estimated_facts()
                    .refundable_credits(USCents(0))
                    .build()
                    .unwrap(),
            ),
        };
        if let Some(ann) = &self.annualised {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            for (i, &t) in ann.iter().enumerate() {
                b.periods(&[estimated::new_period()
                    .end(ends[i])
                    .annualised_tax(t)
                    .build()
                    .unwrap()]);
            }
            b.elections(estimated::new_elections().annualised().build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

fn prior(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .build()
            .unwrap(),
    )
}

fn penalty(h: &Household) -> working::Result<estimated::Penalty> {
    rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap()
}

fn extras() -> newjerseytaxcalc::EstimatedFactsBuilder {
    let mut b = newjerseytaxcalc::new_estimated_facts();
    b.refundable_credits(USCents(0));
    b
}

/// GIT-8's two examples: $150 due a quarter, $90, $90, $90 and $240 paid on
/// the dates.
#[test]
fn estimated_interest_follows_the_git8_examples() {
    let base = Household {
        prior: prior(DOLLAR * 600),
        payments: quarterly(&[DOLLAR * 90, DOLLAR * 90, DOLLAR * 90, DOLLAR * 240]),
        ..tax(DOLLAR * 750)
    };
    let res = penalty(&base);
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    for (i, (balance, months, charge)) in [(6_000, 2, 100), (12_000, 3, 300), (18_000, 4, 600)]
        .into_iter()
        .enumerate()
    {
        let parts = lines[i].parts();
        assert!(
            lines[i].shortfall() == USCents(balance)
                && parts.len() == 1
                && parts[0].months() == months
                && parts[0].charge() == USCents(charge),
            "period {}",
            i + 1
        );
    }
    assert!(lines[3].shortfall() == USCents(9_000) && lines[3].parts().is_empty());
    assert!(
        res.value().total() == USCents(1_000)
            && because(
                &res.required(),
                PositionKind::UnpublishedRate,
                "January 15, 2027"
            )
    );

    let e = extras()
        .prior_year_income_at_this_years_rates(DOLLAR * 420)
        .tax_on_income_to_date(DOLLAR * 100, DOLLAR * 172, DOLLAR * 327)
        .build()
        .unwrap();
    let met = Household {
        extras: Some(e),
        annualised: Some(vec![DOLLAR * 430, DOLLAR * 445, DOLLAR * 525, DOLLAR * 750]),
        ..base
    };
    let res = penalty(&met);
    let lines = res.value().lines();
    assert!(
        lines.len() == 1
            && lines[0].installment() == 3
            && lines[0].shortfall() == USCents(6_000)
            && res.value().total() == USCents(200)
    );
    let s = rules().schedule(&met.facts()).unwrap().value();
    for (i, want) in [
        Status::Excused,
        Status::Excused,
        Status::Short,
        Status::Excused,
    ]
    .into_iter()
    .enumerate()
    {
        assert_eq!(s[i].status(), want, "quarter {}", i + 1);
    }
}

/// Each exception at its boundary.
#[test]
fn estimated_exception_boundaries() {
    let ex2 = extras()
        .prior_year_income_at_this_years_rates(DOLLAR * 420)
        .build()
        .unwrap();
    let ex4 = extras()
        .tax_on_income_to_date(USCents(0), DOLLAR * 172, USCents(0))
        .build()
        .unwrap();
    let plain = extras().build().unwrap();
    let big = DOLLAR * 20_000;
    for (name, e, annual, short, exact, quarter) in [
        (
            "Exception 3 in April",
            plain,
            Some(vec![DOLLAR * 430, big, big, big]),
            [8_599, 15_001, 15_000, 15_000],
            [8_600, 15_000, 15_000, 15_000],
            1,
        ),
        (
            "Exception 4 in June",
            ex4,
            None,
            [15_000, 479, 30_000, 15_000],
            [15_000, 480, 30_000, 15_000],
            2,
        ),
        (
            "Exception 2 in January",
            ex2,
            None,
            [10_500, 10_500, 10_500, 10_499],
            [10_500, 10_500, 10_500, 10_500],
            4,
        ),
    ] {
        for (payments, want) in [(short, Status::Short), (exact, Status::Excused)] {
            let h = Household {
                payments: quarterly(&payments.map(USCents)),
                extras: Some(e.clone()),
                annualised: annual.clone(),
                ..tax(DOLLAR * 750)
            };
            let s = rules().schedule(&h.facts()).unwrap().value();
            assert_eq!(s[quarter - 1].status(), want, "{name}, paid {payments:?}");
        }
    }
}

/// Nothing paid on $8,000 required: $2,000 a quarter at 10% for 2, 3 and 4
/// months.
#[test]
fn estimated_interest_arithmetic() {
    let res = penalty(&tax(DOLLAR * 10_000));
    let lines = res.value().lines();
    for (i, w) in [3_333, 10_000, 20_000].into_iter().enumerate() {
        assert!(
            lines[i].total() == USCents(w) && lines[i].parts()[0].rate() == percent(10),
            "period {}",
            i + 1
        );
    }
    assert_eq!(res.value().total(), USCents(33_333));
    let p = rules()
        .penalty(&tax(DOLLAR * 10_000).facts(), day(2026, 12, 31))
        .unwrap();
    let l = &p.value().lines()[2];
    assert!(l.parts()[0].months() == 4 && l.total() == USCents(20_000));
    let p = rules()
        .penalty(&tax(DOLLAR * 10_000).facts(), day(2027, 7, 1))
        .unwrap();
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
}

/// April's $2,000 paid April 30 runs half a month, counted as one: $16.67.
#[test]
fn estimated_part_of_a_month_counts_as_a_month() {
    let d = dues();
    let h = Household {
        payments: vec![
            pay(day(2026, 4, 30), DOLLAR * 2_000),
            pay(d[1], DOLLAR * 2_000),
            pay(d[2], DOLLAR * 2_000),
            pay(d[3], DOLLAR * 2_000),
        ],
        ..tax(DOLLAR * 10_000)
    };
    let res = penalty(&h);
    let l = res.value().lines();
    assert!(l.len() == 1 && l[0].parts()[0].months() == 1 && res.value().total() == USCents(1_667));
}

/// The dates are the printed ones and none moves.
#[test]
fn estimated_schedule_for_twenty_twenty_six() {
    let s = rules()
        .schedule(&tax(DOLLAR * 10_000).facts())
        .unwrap()
        .value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
        assert_eq!(inst.needed_by(), DOLLAR * 2_000 * (i as i64 + 1));
    }
}

/// February 15, 2027 is Presidents' Day.
#[test]
fn estimated_february_fifteenth() {
    assert_eq!(day(2027, 2, 15).weekday(), Weekday::MONDAY);
    let h = Household {
        return_filed: day(2027, 2, 12),
        ..tax(DOLLAR * 10_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[3].status(), Status::Excused);
    let res = penalty(&h);
    let l = res.value().lines();
    assert!(
        res.value().as_of() == day(2027, 2, 12)
            && l.len() == 4
            && l[3].shortfall() == DOLLAR * 6_000
    );
    let h = Household {
        return_filed: day(2027, 2, 16),
        ..tax(DOLLAR * 10_000)
    };
    let res = penalty(&h);
    assert!(
        res.value().lines().len() == 4
            && because(
                &res.required(),
                PositionKind::UnsettledRule,
                "Presidents' Day"
            )
    );
    let moves = extras().february_fifteenth_moves(true).build().unwrap();
    let h = Household {
        return_filed: day(2027, 2, 16),
        extras: Some(moves),
        ..tax(DOLLAR * 10_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[3].status(), Status::Excused);
}

/// NJ-2210 line 3 stops under $400; exactly $400 goes on. The filing
/// threshold takes a low income out entirely.
#[test]
fn estimated_thresholds() {
    for (w, exempt) in [(960_001, true), (960_000, false)] {
        let h = Household {
            withheld: USCents(w),
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {w}");
    }
    for (status, gross, exempt) in [
        (FilingStatus::Single, 1_000_000, true),
        (FilingStatus::Single, 1_000_001, false),
        (FilingStatus::MarriedFilingSeparately, 1_000_000, true),
        (FilingStatus::MarriedFilingJointly, 2_000_000, true),
        (FilingStatus::MarriedFilingJointly, 2_000_001, false),
        (FilingStatus::HeadOfHousehold, 2_000_000, true),
        (FilingStatus::SurvivingSpouse, 2_000_000, true),
    ] {
        let h = Household {
            status,
            gross: USCents(gross),
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "{status:?}, gross {gross}");
    }
    for (h, label) in [
        (
            Household {
                withheld: USCents(960_001),
                ..tax(DOLLAR * 10_000)
            },
            "Tax less withholding and credits under $400",
        ),
        (
            Household {
                gross: USCents(1_000_000),
                ..tax(DOLLAR * 10_000)
            },
            "Gross income of $10,000 or less",
        ),
    ] {
        let res = rules().requirement(&h.facts()).unwrap();
        let c = res.value().chosen().unwrap();
        assert!(
            c.basis() == Basis::Exception && c.amount().0 == 0 && c.label() == label,
            "{label}"
        );
        assert_eq!(penalty(&h).value().reason(), format!("{label}."));
    }
}

/// The lesser of 80% of this year's tax and 100% of last year's, from a
/// twelve-month return; zero when no return was required.
#[test]
fn estimated_target_options() {
    let h = Household {
        prior: prior(DOLLAR * 7_999),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(r.chosen().unwrap().basis() == Basis::PriorYearTax && r.target() == DOLLAR * 7_999);
    let h = Household {
        prior: prior(DOLLAR * 8_001),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(r.chosen().unwrap().basis() == Basis::ThisYearTax && r.target() == DOLLAR * 8_000);
    let nr = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let h = Household {
        prior: Some(nr),
        ..tax(DOLLAR * 10_000)
    };
    assert_eq!(
        rules().requirement(&h.facts()).unwrap().value().target().0,
        0
    );
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .build()
        .unwrap();
    for (name, p) in [
        ("none on record", None),
        ("not filed", Some(not_filed)),
        ("eleven months", Some(short)),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(!res.value().options()[1].available(), "{name}");
    }
}

/// Last year's overpayment counts toward June, as NJ-2210 line 6 says, and
/// the question is asked; answered, it counts toward April.
#[test]
fn estimated_prior_year_overpayment() {
    let over = estimated::new_payment()
        .amount(DOLLAR * 2_000)
        .from_prior_year()
        .build()
        .unwrap();
    let mut payments = vec![over];
    payments.extend(quarterly(&[
        USCents(0),
        DOLLAR * 2_000,
        DOLLAR * 2_000,
        DOLLAR * 2_000,
    ]));
    let h = Household {
        payments: payments.clone(),
        ..tax(DOLLAR * 10_000)
    };
    let res = penalty(&h);
    let l = res.value().lines();
    assert!(
        !l.is_empty()
            && l[0].installment() == 1
            && l[0].total() == USCents(3_333)
            && because(&res.required(), PositionKind::UnsettledRule, "overpayment")
    );
    let april = extras()
        .prior_year_overpayment_in_april(true)
        .build()
        .unwrap();
    let h = Household {
        payments,
        extras: Some(april),
        ..tax(DOLLAR * 10_000)
    };
    assert_eq!(penalty(&h).value().total().0, 0);
}

/// Withholding and refundable credits count a quarter on each date.
#[test]
fn estimated_withholding_and_credits() {
    let h = Household {
        withheld: DOLLAR * 8_000,
        ..tax(DOLLAR * 10_000)
    };
    assert_eq!(penalty(&h).value().total().0, 0);
    let e = newjerseytaxcalc::new_estimated_facts()
        .refundable_credits(DOLLAR * 8_000)
        .build()
        .unwrap();
    let h = Household {
        extras: Some(e),
        ..tax(DOLLAR * 10_000)
    };
    assert_eq!(penalty(&h).value().total().0, 0);
    let none = newjerseytaxcalc::new_estimated_facts().build().unwrap();
    let h = Household {
        extras: Some(none),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "refundable credits"
    ));
}

/// Without this year's tax, the target is last year's alone and the
/// interest is the most it can be.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(
        r.required()
            && r.target() == DOLLAR * 8_000
            && r.options()[0].why_not() == estimated::NEEDS_THIS_YEAR_TAX
            && has(&res.required(), PositionKind::ThisYearTax)
    );
    let p = penalty(&h).value();
    assert!((p.upper_bound() || !p.rated_through().is_zero()) && p.total().0 != 0);
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
    let err = rules()
        .penalty(&none.facts(), day(2027, 4, 15))
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want: HashMap<&str, [&str; 2]> = HashMap::from([
        ("The $400 boundary", ["under $400", "54A:8-4(a)"]),
        (
            "A twelve-month prior year",
            ["full 12 months", "54A:9-6(c)(2)"],
        ),
        (
            "A requirement that arises late",
            ["by four", "54A:8-5(a)(2)-(4)"],
        ),
        (
            "Exception 1 with no prior-year return",
            ["zero if no return was required", "54A:9-6(d)(1)(A)"],
        ),
        (
            "110% in Exceptions 1 and 2",
            ["at any income", "54A:9-6(d)(3)"],
        ),
        ("When a year's rate starts", ["January 16", "54:49-3"]),
        ("Last year's overpayment", ["column B", "54A:9-6"]),
    ]);
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let rule = d.rule();
        let w = want[rule.as_str()];
        assert!(
            d.form().contains(w[0])
                && d.statute_source().contains(w[1])
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{rule:?}"
        );
        assert!(
            d.uses() == Reading::Form
                && d.affects_household() == (rule == "When a year's rate starts"),
            "{rule:?}"
        );
    }
    let h = Household {
        payments: quarterly(&[DOLLAR * 2_000; 4]),
        ..tax(DOLLAR * 10_000)
    };
    for d in rules().requirement(&h.facts()).unwrap().divergences() {
        assert!(!d.affects_household(), "paid in full: {:?}", d.rule());
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    let err = newjerseytaxcalc::estimated_tax(2025).err().unwrap();
    assert!(err.is(&estimated::Error::NoRules), "{err}");
    let h = Household {
        farm_share: Rate(7000),
        ..tax(DOLLAR * 10_000)
    };
    let err = rules().requirement(&h.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::Unsupported), "{err}");
    let h = Household {
        fish_share: Rate(7000),
        ..tax(DOLLAR * 10_000)
    };
    assert!(rules().requirement(&h.facts()).is_ok());
    let this = estimated::new_figures()
        .tax(USCents(100_000))
        .build()
        .unwrap();
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
            estimated::new_facts(NJ, 2027)
                .filing_status(FilingStatus::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(NJ, 2026)
                .filing_status(FilingStatus::Single)
                .this_year(this)
                .state(1.5_f64)
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
        "{}{}{}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$400",
        "80%",
        "10%",
        "part of a month",
        "February 15",
        "last year's income at this year's rates",
        "110%",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
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
    let f = estimated::new_facts(NJ, 2026)
        .filing_status(FilingStatus::Single)
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

/// New Jersey has no waiver of this interest: the Director may waive only
/// interest above prime plus three points, which is the whole rate.
#[test]
fn estimated_waiver_is_refused() {
    let retired = estimated::new_positions()
        .waiver_for(WaiverCause::Retirement, day(2026, 6, 1))
        .born(day(1962, 3, 1))
        .build()
        .unwrap();
    let disaster = estimated::new_positions()
        .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
        .build()
        .unwrap();
    for (name, pos) in [("retired in 2026 at 64", retired), ("a disaster", disaster)] {
        let h = Household {
            positions: Some(pos),
            ..tax(DOLLAR * 10_000)
        };
        let p = penalty(&h).value();
        let o = p.waiver_outcome().unwrap();
        assert!(
            o.refused()
                && o.reason().contains("three points over prime")
                && p.total().0 != 0
                && p.if_waived() == p.total(),
            "{name}"
        );
    }
}

/// Under an exception no question may say its answer changes the target, so
/// last year's return isn't asked for.
#[test]
fn estimated_exempt_asks_nothing_of_the_target() {
    let res = rules().requirement(&tax(DOLLAR * 100).facts()).unwrap();
    assert!(res.value().exempt());
    assert!(!has(&res.required(), PositionKind::PriorYearReturn));
    for q in res.required() {
        assert!(
            !q.effect().to_lowercase().contains("target"),
            "{:?}",
            q.effect()
        );
    }
}
