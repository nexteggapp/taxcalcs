//! Mississippi's estimated tax rules for 2026, ported from the Go package's
//! estimated_test.go.
//!
//! Mississippi publishes one oracle: the worked example on Form 80-320 (Rev.
//! 09/25), with its inputs and its $30.30 of interest.
//! `estimated_interest_matches_the_form_example` reproduces it with the 2026
//! dates, which give the same months. The other cases are hand-written. (Go
//! also compares every figure against its extract in testdata/; those checks
//! stay in Go.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, Elections, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status,
    WaiverCause, WaiverKind, Weekday,
};
use money::{DOLLAR, USCents, dollars, percent};
use taxcalcs_mississippitaxcalc::{EstimatedFacts, estimated_tax, new_estimated_facts};
use working::{PositionKind, Reading, Required};

const MS: estimated::Jurisdiction = estimated::MISSISSIPPI;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    estimated_tax(2026).unwrap()
}

fn because(rs: &[Required], k: PositionKind, phrase: &str) -> bool {
    rs.iter().any(|r| {
        r.position() == k && (r.question().contains(phrase) || r.effect().contains(phrase))
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

#[derive(Default, Clone)]
struct Household {
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    withheld_on: Date,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    elections: Option<Elections>,
    positions: Option<Positions>,
}

impl Household {
    fn facts(&self) -> Facts {
        let figures = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(MS, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(figures)
            .paid(&self.payments)
            .requirement_first_met(self.first_met)
            .return_filed(self.return_filed);
        if self.withheld.0 > 0 {
            let mut on = self.withheld_on;
            if on.is_zero() {
                on = day(2026, 4, 1);
            }
            b.withheld(&[estimated::new_withheld()
                .on(on)
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
        if let Some(e) = self.elections {
            b.elections(e);
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
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

/// Form 80-320's example: $6,520 of liability and $4,510 last year, so four
/// installments of $1,127.50; $1,500, $1,000, $500 and nothing credited by
/// the dates. Column C's running underestimate of $382.50 at 2% is $7.65,
/// column D's $1,510.00 at 1.5% is $22.65, $30.30 in all.
#[test]
fn estimated_interest_matches_the_form_example() {
    let h = Household {
        tax: DOLLAR * 6_520,
        withheld: DOLLAR * 1_500,
        prior: prior(DOLLAR * 4_510),
        payments: vec![pay(dues()[1], DOLLAR * 1_000), pay(dues()[2], DOLLAR * 500)],
        ..Household::default()
    };
    let req = rules().requirement(&h.facts()).unwrap().value();
    assert_eq!(req.target(), DOLLAR * 4_510);
    assert_eq!(req.options()[0].amount(), DOLLAR * 5_216);
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let lines = res.value().lines();
    assert_eq!(lines.len(), 2, "want columns C and D");
    for (l, (col, under, months, charge)) in lines
        .iter()
        .zip([(3, 38_250, 4, 765), (4, 151_000, 3, 2_265)])
    {
        let parts = l.parts();
        assert_eq!(l.installment(), col);
        assert_eq!(l.shortfall(), USCents(under));
        assert_eq!(parts.len(), 1);
        assert_eq!(parts[0].months(), months);
        assert_eq!(parts[0].charge(), USCents(charge));
    }
    assert_eq!(res.value().total(), USCents(3_030));
}

/// The dates are the ones Rule 100(2) and the booklet print, a quarter each,
/// and none moves.
#[test]
fn estimated_schedule_for_twenty_twenty_six() {
    let s = rules()
        .schedule(&tax(DOLLAR * 5_000).facts())
        .unwrap()
        .value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
        assert_eq!(inst.needed_by(), DOLLAR * 1_000 * (i as i64 + 1));
    }
    let wd = day(2027, 4, 15).weekday();
    assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
}

/// Form 80-320 line 1: a liability of $200 or less owes nothing.
#[test]
fn estimated_threshold_boundary() {
    for (t, exempt) in [(20_000, true), (20_001, false)] {
        let res = rules().requirement(&tax(USCents(t)).facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "liability {t}");
        if exempt {
            let c = res.value().chosen().unwrap();
            assert_eq!(c.basis(), Basis::Exception);
            assert_eq!(c.amount(), USCents(0));
            assert_eq!(c.label(), "Mississippi liability of $200 or less");
            let p = rules()
                .penalty(&tax(USCents(t)).facts(), day(2027, 4, 15))
                .unwrap();
            assert_eq!(p.value().reason(), "Mississippi liability of $200 or less.");
        }
    }
}

/// The target is the lesser of 80% of this year's liability and 100% of last
/// year's, which needs a filed twelve-month return.
#[test]
fn estimated_target_options() {
    let h = Household {
        tax: DOLLAR * 10_000,
        prior: prior(DOLLAR * 9_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().chosen().unwrap().basis(), Basis::ThisYearTax);
    assert_eq!(res.value().target(), DOLLAR * 8_000);
    let h = Household {
        tax: DOLLAR * 10_000,
        prior: prior(DOLLAR * 7_999),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::PriorYearTax);
    assert_eq!(c.percent(), percent(100));
    assert_eq!(res.value().target(), DOLLAR * 7_999);

    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .build()
        .unwrap();
    for (name, p) in [
        ("none on record", None),
        ("not filed", Some(not_filed)),
        ("six months", Some(short)),
    ] {
        let h = Household {
            tax: DOLLAR * 10_000,
            prior: p,
            ..Household::default()
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(!res.value().options()[1].available(), "{name}");
    }
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    assert!(has(&res.required(), PositionKind::PriorYearReturn));
}

/// Form 80-320 line 4 with a zero line 3: 80% of this year's when no
/// estimated payments were made, and the lesser, zero, when some were.
#[test]
fn estimated_zero_prior_year_follows_line_four() {
    let h = Household {
        tax: DOLLAR * 10_000,
        prior: prior(USCents(0)),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_000);
    assert!(!res.value().options()[1].available());
    let h = Household {
        payments: vec![pay(dues()[0], DOLLAR * 100)],
        ..h
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().target(), USCents(0));
    assert!(!res.value().required());
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), USCents(0));
}

/// Rule 100(3) measures a filer not required to file last year against the
/// tax on last year's income at this year's rates.
#[test]
fn estimated_not_required_to_file_is_an_open_question() {
    let nr = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let h = Household {
        tax: DOLLAR * 10_000,
        prior: Some(nr),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_000);
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "at 2026 Mississippi rates"
    ));
    let e = new_estimated_facts()
        .prior_year_income_at_this_years_rates(DOLLAR * 3_000)
        .build()
        .unwrap();
    let h = Household {
        extras: Some(e),
        ..h
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(
        res.value().chosen().unwrap().basis(),
        Basis::PriorYearIncome
    );
    assert_eq!(res.value().target(), DOLLAR * 3_000);
    assert!(!because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "at 2026 Mississippi rates"
    ));
}

/// All withholding counts on April 15, whenever it was withheld.
#[test]
fn estimated_withholding_counts_in_column_a() {
    const Q: &str = "all withholding toward Apr 15";
    let mut h = Household {
        tax: DOLLAR * 10_000,
        withheld: DOLLAR * 8_000,
        withheld_on: day(2026, 12, 15),
        ..Household::default()
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(res.value().total(), USCents(0));
    assert!(because(&res.required(), PositionKind::JurisdictionFact, Q));
    assert!(!rules().requirement(&h.facts()).unwrap().value().required());
    h.elections = Some(
        estimated::new_elections()
            .withholding_as_withheld()
            .build()
            .unwrap(),
    );
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    // $2,000 short on April 15 for 2 months, $4,000 on June 15 for 3, $6,000
    // on September 15 to December 15: 3 months. $20 + $60 + $90.
    assert_eq!(res.value().total(), DOLLAR * 170);
    assert!(!because(&res.required(), PositionKind::JurisdictionFact, Q));
    let early = Household {
        tax: DOLLAR * 10_000,
        withheld: DOLLAR * 8_000,
        withheld_on: day(2026, 4, 15),
        ..Household::default()
    };
    let res = rules().penalty(&early.facts(), day(2027, 4, 15)).unwrap();
    assert!(!because(&res.required(), PositionKind::JurisdictionFact, Q));
}

/// An unpaid first installment carries through every column: $1,000 at ½%
/// for 2, 3, 4 and 3 months, $60.
#[test]
fn estimated_shortfall_carries_through_every_column() {
    let d = dues();
    let h = Household {
        tax: DOLLAR * 5_000,
        payments: vec![
            pay(d[1], DOLLAR * 1_000),
            pay(d[2], DOLLAR * 1_000),
            pay(d[3], DOLLAR * 1_000),
        ],
        ..Household::default()
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let want = [1_000, 1_500, 2_000, 1_500];
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    for (i, l) in lines.iter().enumerate() {
        assert_eq!(l.shortfall(), DOLLAR * 1_000, "column {}", i + 1);
        assert_eq!(l.total(), USCents(want[i]), "column {}", i + 1);
    }
    assert_eq!(res.value().total(), DOLLAR * 60);
    assert!(has(&res.required(), PositionKind::RequirementFirstMet));
}

/// A payment between due dates stops the charge on what it pays. Paid May 1,
/// April's $1,000 runs half a month: counted as a whole month by default,
/// $5.00, and asked; answered with completed months only, nothing.
#[test]
fn estimated_part_month_is_an_open_question() {
    let d = dues();
    let payments = vec![
        pay(day(2026, 5, 1), DOLLAR * 1_000),
        pay(d[1], DOLLAR * 1_000),
        pay(d[2], DOLLAR * 1_000),
        pay(d[3], DOLLAR * 1_000),
    ];
    let h = Household {
        tax: DOLLAR * 5_000,
        payments,
        ..Household::default()
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(res.value().total(), USCents(500));
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "part of a month"
    ));
    let parts = res.value().lines()[0].parts();
    assert_eq!(parts.len(), 1);
    assert_eq!(parts[0].to(), day(2026, 5, 1));
    assert_eq!(parts[0].months(), 1);
    let e = new_estimated_facts()
        .part_month_counts_whole(false)
        .build()
        .unwrap();
    let h = Household {
        extras: Some(e),
        ..h
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(res.value().total(), USCents(0));
    assert!(!because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "part of a month"
    ));
}

/// A requirement first met on July 1 computes interest only from September:
/// $3,000 for 4 months and $4,000 for 3, $120.
#[test]
fn estimated_requirement_met_late() {
    let h = Household {
        tax: DOLLAR * 5_000,
        first_met: day(2026, 7, 1),
        ..Household::default()
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(res.value().total(), DOLLAR * 120);
    assert!(!has(&res.required(), PositionKind::RequirementFirstMet));
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].status(), Status::Excused);
    assert_eq!(s[1].status(), Status::Excused);
    assert_eq!(s[2].status(), Status::Short);
}

/// The interest stops on the day the return is filed and paid, and never
/// runs past April 15, 2027.
#[test]
fn estimated_interest_stops() {
    let d = dues();
    let mut h = Household {
        tax: DOLLAR * 5_000,
        payments: vec![
            pay(d[0], DOLLAR * 1_000),
            pay(d[1], DOLLAR * 1_000),
            pay(d[2], DOLLAR * 1_000),
        ],
        ..Household::default()
    };
    let res = rules().penalty(&h.facts(), day(2027, 6, 30)).unwrap();
    assert_eq!(res.value().as_of(), day(2027, 4, 15));
    assert_eq!(res.value().total(), DOLLAR * 15);
    // Filed and paid February 15: January's $1,000 for one month.
    h.return_filed = day(2027, 2, 15);
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(res.value().as_of(), day(2027, 2, 15));
    assert_eq!(res.value().total(), DOLLAR * 5);
}

/// Without this year's liability, the target is last year's alone and the
/// interest is the most it can be.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 4_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required());
    assert_eq!(r.target(), DOLLAR * 4_000);
    assert_eq!(r.options()[0].why_not(), estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(p.value().upper_bound() && p.value().total().0 != 0);
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let err = rules().schedule(&none.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
    let err = rules()
        .penalty(&none.facts(), day(2027, 4, 15))
        .err()
        .unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax), "{err}");
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want = [
        ("The interest rate", "½% a month", "R. 100(1)"),
        ("The prior-year test", "lesser of 80%", "§ 27-7-327"),
        (
            "Last year's liability was zero",
            "80% of this year's",
            "R. 100(3)",
        ),
    ];
    let ds = res.divergences();
    assert_eq!(ds.len(), want.len());
    for d in &ds {
        let Some((_, form, statute)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        // 80% of this year's is chosen, nothing is paid, and last year is not
        // zero: only the interest rate touches these facts.
        assert_eq!(d.affects_household(), d.rule() == "The interest rate");
        assert_eq!(d.uses(), Reading::Form);
        assert!(d.form().contains(form) && d.statute_source().contains(statute));
        assert!(!d.form_source().is_empty() && !d.statute().is_empty());
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    let err = estimated_tax(2025).err().unwrap();
    assert!(err.is(&estimated::Error::NoRules));
    let this = estimated::new_figures()
        .tax(USCents(100_000))
        .build()
        .unwrap();
    let base = |j: estimated::Jurisdiction, y: i64| {
        let mut b = estimated::new_facts(j, y);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    for f in [
        base(MS, 2026)
            .farm_share(money::Rate(7000))
            .build()
            .unwrap(),
        base(MS, 2026)
            .fish_share(money::Rate(7000))
            .build()
            .unwrap(),
    ] {
        let err = rules().requirement(&f).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{err}");
    }
    for (name, f) in [
        (
            "California's facts",
            base(estimated::CALIFORNIA, 2026).build().unwrap(),
        ),
        ("2027", base(MS, 2027).build().unwrap()),
        ("other extras", base(MS, 2026).state(1).build().unwrap()),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&tax(USCents(100)).facts(), Date::default())
            .is_err()
    );
    // The annualized method and waivers do not exist; the regular answer is
    // given and says so.
    let period = |m, d| {
        estimated::new_period()
            .end(day(2026, m, d))
            .build()
            .unwrap()
    };
    let ann = base(MS, 2026)
        .elections(estimated::new_elections().annualised().build().unwrap())
        .periods(&[period(3, 31), period(5, 31), period(8, 31), period(12, 31)])
        .positions(
            estimated::new_positions()
                .waiver(WaiverKind::Casualty)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules().penalty(&ann, day(2027, 4, 15)).unwrap();
    let text: String = res.applied().iter().map(|r| r.applied() + " ").collect();
    assert!(text.contains("no annualized income method"), "{text}");
    assert!(text.contains("no waiver"), "{text}");
    assert!(res.value().total().0 != 0);
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
        "$200",
        "80%",
        "100%",
        "½% a month",
        "April payment",
        "January 15 to April 15",
        "no annualized method",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
}

/// Withholding over the tax never reads as a negative balance.
#[test]
fn withholding_over_the_tax_has_no_negative_balance() {
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
    let f = estimated::new_facts(MS, 2026)
        .filing_status(filing::Status::Single)
        .this_year(figures(3_000, 60_000))
        .prior_year(prior)
        .withheld(&withheld)
        .build()
        .unwrap();
    let got = rules().requirement(&f).unwrap().value().why();
    assert!(!got.contains("−$") && !got.contains("-$"), "why = {got:?}");
}

fn retired_on(on: Date, born: Date) -> Positions {
    estimated::new_positions()
        .waiver_for(WaiverCause::Retirement, on)
        .born(born)
        .build()
        .unwrap()
}

/// Mississippi publishes no waiver and no rule refusing one, so the outcome
/// is left to the department and reported as unsettled.
#[test]
fn estimated_waiver_is_undecided() {
    for (name, pos) in [
        (
            "retired in 2026 at 64",
            retired_on(day(2026, 6, 1), day(1962, 3, 1)),
        ),
        (
            "a disaster",
            estimated::new_positions()
                .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
                .build()
                .unwrap(),
        ),
    ] {
        let h = Household {
            tax: DOLLAR * 10_000,
            positions: Some(pos),
            ..Household::default()
        };
        let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        let p = res.value();
        let o = p.waiver_outcome().unwrap();
        assert!(o.undecided(), "{name}");
        assert!(p.total().0 != 0 && p.if_waived() == p.total(), "{name}");
        assert!(
            because(&res.required(), PositionKind::UnsettledRule, "waived"),
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
            "{}",
            q.effect()
        );
    }
}
