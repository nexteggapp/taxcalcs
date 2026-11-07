//! Oklahoma's estimated tax rules for 2026, checked against the Tax
//! Commission's forms and the statutes as the Legislature publishes them.
//! Ported from estimated_test.go.
//!
//! Oklahoma publishes no exhaustive oracle and no worked example for
//! estimated tax. The arithmetic is tested with hand-worked cases, each worked
//! in its comment as OW-8-P computes it. The Go tests that compare figures
//! with their extracts in testdata/ read the Go package's citations and are
//! not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status, WaiverCause,
    WaiverKind, Weekday,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_oklahomataxcalc as oklahomataxcalc;
use working::{PositionKind, Reading, Required, Rule};

const OK: estimated::Jurisdiction = estimated::OKLAHOMA;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    oklahomataxcalc::estimated_tax(2026).unwrap()
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

#[derive(Clone, Default)]
struct Household {
    tax: USCents,
    no_tax: bool,
    status: filing::Status,
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<oklahomataxcalc::EstimatedFacts>,
    farm_share: Rate,
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
        let mut b = estimated::new_facts(OK, 2026);
        b.filing_status(status)
            .this_year(figures)
            .paid(&self.payments)
            .return_filed(self.return_filed)
            .farm_share(self.farm_share);
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
                oklahomataxcalc::new_estimated_facts()
                    .prior_year_farm_share(Rate(0))
                    .build()
                    .unwrap(),
            ),
        };
        let mut e = estimated::new_elections();
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
                .map(|(i, &a)| {
                    estimated::new_period()
                        .end(ends[i])
                        .annualised_tax(a)
                        .build()
                        .unwrap()
                })
                .collect();
            b.periods(&ps);
            e.annualised();
        }
        if self.as_withheld {
            e.withholding_as_withheld();
        }
        b.elections(e.build().unwrap());
        if let Some(k) = self.waiver {
            b.positions(estimated::new_positions().waiver(k).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

fn filed(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .full_year_resident(true)
            .build()
            .unwrap(),
    )
}

/// The nth `wd` of a month, or the last when `n` is negative.
fn nth(year: i64, month: i64, wd: Weekday, n: i64) -> Date {
    if n < 0 {
        let mut d = if month == 12 {
            day(year + 1, 1, 1)
        } else {
            day(year, month + 1, 1)
        }
        .add_days(-1);
        while d.weekday() != wd {
            d = d.add_days(-1);
        }
        return d;
    }
    let mut d = day(year, month, 1);
    while d.weekday() != wd {
        d = d.add_days(1);
    }
    d.add_days(7 * (n - 1))
}

/// § 82.1(C): a holiday on a Saturday is kept the Friday before, and one on
/// a Sunday the Monday after.
fn observed(d: Date) -> Date {
    match d.weekday() {
        Weekday::SATURDAY => d.add_days(-1),
        Weekday::SUNDAY => d.add_days(1),
        _ => d,
    }
}

#[test]
fn calendar_is_section_82_point_1() {
    let want = [
        observed(day(2026, 1, 1)),
        nth(2026, 1, Weekday::MONDAY, 3),
        nth(2026, 2, Weekday::MONDAY, 3),
        nth(2026, 5, Weekday::MONDAY, -1),
        observed(day(2026, 7, 4)),
        nth(2026, 9, Weekday::MONDAY, 1),
        observed(day(2026, 11, 11)),
        nth(2026, 11, Weekday::THURSDAY, 4),
        nth(2026, 11, Weekday::THURSDAY, 4).add_days(1),
        day(2026, 12, 25),
        observed(day(2027, 1, 1)),
        nth(2027, 1, Weekday::MONDAY, 3),
        nth(2027, 2, Weekday::MONDAY, 3),
    ];
    assert_eq!(want[4], day(2026, 7, 3));
    assert_eq!(want[3], day(2026, 5, 25));
    assert_eq!(want[11], day(2027, 1, 18));
    let cal = oklahomataxcalc::estimated_calendar(2026).unwrap();
    let got = cal.holidays();
    assert_eq!(got.len(), want.len());
    for (i, hol) in got.iter().enumerate() {
        assert_eq!(hol.on(), want[i], "holiday {} ({})", i + 1, hol.name());
    }
    // The calendar moves a date that falls on a weekend or holiday.
    for (from, to) in [
        (day(2026, 7, 3), day(2026, 7, 6)), // a holiday, then a weekend
        (day(2026, 11, 26), day(2026, 11, 30)), // Thanksgiving and the day after
        (day(2027, 1, 31), day(2027, 2, 1)), // a Sunday
        (day(2027, 1, 16), day(2027, 1, 19)), // a weekend, then MLK Day
        (day(2026, 4, 15), day(2026, 4, 15)),
    ] {
        assert_eq!(cal.next_business_day(from), to, "{from}");
    }
    assert!(
        oklahomataxcalc::estimated_calendar(2027)
            .unwrap_err()
            .is(&estimated::Error::NoRules)
    );
}

// The 2026 dates are the ones OW-8-ES prints. Each is a business day, so
// none moves.
#[test]
fn due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&h(10_000).facts()).unwrap();
    let cal = oklahomataxcalc::estimated_calendar(2026).unwrap();
    for (i, s) in res.value().iter().enumerate() {
        assert_eq!(s.due(), dues()[i]);
        assert!(cal.is_business_day(s.due()));
    }
    assert!(!applied(
        &res.applied(),
        "falls on a weekend or legal holiday"
    ));
}

// § 2385.9(A)(1)'s January 31, 2027 is a Sunday, and § 82.1(C) moves it to
// Monday, February 1. The waiver is reported there and not applied, because
// the forms have no step for it.
#[test]
fn january_thirty_first_moves_and_is_not_applied() {
    let mut hh = Household {
        return_filed: day(2027, 2, 1),
        ..h(10_000)
    };
    let res = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
    assert!(applied(
        &res.applied(),
        "January 31, 2027 is a weekend or legal holiday, so the date moves to February 1, 2027"
    ));
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    // $1,750 for the 17 days from January 15 to February 1: $16.30.
    assert_eq!(lines[3].total(), USCents(1_630));
    assert!(applied(
        &res.applied(),
        "the January 15 installment carries $16.30 of interest, which the statute's waiver would remove"
    ));
    hh.return_filed = day(2027, 2, 2);
    assert!(!applied(
        &rules()
            .penalty(&hh.facts(), day(2027, 4, 15))
            .unwrap()
            .applied(),
        "statute waives"
    ));
}

// The thresholds, one cent either side: $500 of tax over withholding, tax
// of $1,000, and a farm share of two-thirds this year or last.
#[test]
fn threshold_boundaries() {
    for (name, tax, withheld, exempt, label) in [
        (
            "$499.99 over withholding",
            200_000,
            150_001,
            true,
            "Tax less withholding under $500",
        ),
        ("$500.00 over withholding", 200_000, 150_000, false, ""),
        (
            "tax of $999.99",
            99_999,
            0,
            true,
            "Tax after credits under $1,000",
        ),
        ("tax of $1,000.00", 100_000, 0, false, ""),
    ] {
        let hh = Household {
            tax: USCents(tax),
            withheld: USCents(withheld),
            ..Household::default()
        };
        let r = rules().requirement(&hh.facts()).unwrap().value();
        assert_eq!(r.exempt(), exempt, "{name}: {}", r.why());
        if exempt {
            let c = r.chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception && c.amount().0 == 0 && c.label() == label,
                "{name}: {c:?}"
            );
        } else {
            // $1,500 withheld already reaches 70% of $2,000; nothing
            // withheld reaches nothing.
            assert_eq!(r.required(), withheld == 0, "{name}: {}", r.why());
        }
    }
    for (this, last, exempt) in [
        (6_666, 0, false),
        (6_667, 0, true),
        (0, 6_666, false),
        (0, 6_667, true),
    ] {
        let e = oklahomataxcalc::new_estimated_facts()
            .prior_year_farm_share(Rate(last))
            .build()
            .unwrap();
        let r = rules()
            .requirement(
                &Household {
                    farm_share: Rate(this),
                    extras: Some(e),
                    ..h(10_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        assert_eq!(
            r.exempt(),
            exempt,
            "farm share {this} this year, {last} last"
        );
    }
}

// The target is the lesser of 70% of this year's tax and 100% of last
// year's, and last year's needs a filed 12-month return.
#[test]
fn target_options() {
    // 70% of $10,000 is $7,000; last year's $8,000 is more.
    let r = rules()
        .requirement(
            &Household {
                prior: filed(DOLLAR * 8_000),
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    assert_eq!(r.chosen().unwrap().basis(), Basis::ThisYearTax);
    assert_eq!(r.target(), DOLLAR * 7_000);
    // Last year's $6,000 is less, and 100% at any income.
    let r = rules()
        .requirement(
            &Household {
                prior: filed(DOLLAR * 6_000),
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    let c = r.chosen().unwrap();
    assert!(
        c.basis() == Basis::PriorYearTax
            && c.percent() == percent(100)
            && r.target() == DOLLAR * 6_000
    );

    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .build()
        .unwrap();
    let unfiled = estimated::new_prior_return().filed(false).build().unwrap();
    for (name, p, ask) in [
        ("no return on record", None, true),
        ("not filed", Some(unfiled), false),
        ("an 11-month return", Some(short), false),
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
        assert!(!o.available() && !o.why_not().is_empty(), "{name}");
        assert_eq!(res.value().target(), DOLLAR * 7_000, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn),
            ask,
            "{name}"
        );
    }
}

// Without this year's tax the target is last year's tax alone, the
// interest is an upper bound, and with no last year there is no target.
#[test]
fn without_this_years_tax() {
    let mut hh = Household {
        no_tax: true,
        prior: filed(DOLLAR * 8_000),
        ..Household::default()
    };
    let res = rules().requirement(&hh.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 8_000, "{}", r.why());
    let o = r.options()[0].clone();
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&hh.facts()).unwrap().value();
    for (i, want) in [200_000, 400_000, 600_000, 800_000].into_iter().enumerate() {
        assert_eq!(sched[i].needed_by(), USCents(want));
    }
    let pen = rules()
        .penalty(&hh.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!(pen.upper_bound() && pen.total().0 != 0);

    // Withholding that reaches last year's tax needs nothing more.
    hh.withheld = DOLLAR * 8_000;
    assert!(!rules().requirement(&hh.facts()).unwrap().value().required());

    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let r = rules().requirement(&none.facts()).unwrap().value();
    assert!(!r.target_known() && !r.required());
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

    // An exception that needs nothing about this year still applies, and
    // its zero is exact.
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let exact = rules()
        .penalty(
            &Household {
                no_tax: true,
                prior: Some(not_required),
                ..Household::default()
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap()
        .value();
    assert!(exact.total().0 == 0 && !exact.upper_bound());
}

// OW-8-P Part 3 on $10,000 of tax and nothing paid: $1,750 a quarter, each
// charged 20% ÷ 365 a day to April 15, 2027.
//
//	April 15:     $1,750 × 365 days × 20% ÷ 365 = $350.00
//	June 15:      $1,750 × 304 days             = $291.51
//	September 15: $1,750 × 212 days             = $203.29
//	January 15:   $1,750 × 90 days              =  $86.30
#[test]
fn interest_arithmetic() {
    let f = h(10_000).facts();
    for as_of in [day(2027, 4, 15), day(2027, 12, 31)] {
        let res = rules().penalty(&f, as_of).unwrap();
        let p = res.value();
        assert_eq!(p.as_of(), day(2027, 4, 15));
        let want_days = [365, 304, 212, 90];
        let want_charge = [35_000, 29_151, 20_329, 8_630];
        let lines = p.lines();
        assert_eq!(lines.len(), 4);
        for (i, l) in lines.iter().enumerate() {
            let parts = l.parts();
            assert!(
                l.shortfall() == DOLLAR * 1_750
                    && parts.len() == 1
                    && parts[0].days() == want_days[i]
                    && parts[0].rate() == percent(20)
                    && l.total() == USCents(want_charge[i]),
                "line {}: {l:?}",
                i + 1
            );
        }
        assert_eq!(p.total(), USCents(93_110));
        assert!(!has(&res.required(), PositionKind::UnpublishedRate));
    }
}

// A payment goes to the earliest balance first. $1,000 on April 15 leaves
// $750 of April's $1,750 short; $2,000 on May 15 pays it after 30 days and
// leaves $1,250 toward June, which is then $500 short.
//
//	April:     $750 × 30 days × 20% ÷ 365 = $12.33
//	June:      $500 × 304 days            = $83.29
//	September: $1,750 × 212 days          = $203.29
//	January:   $1,750 × 90 days           = $86.30
#[test]
fn payments_apply_earliest_first() {
    let f = Household {
        payments: vec![
            pay(day(2026, 4, 15), DOLLAR * 1_000),
            pay(day(2026, 5, 15), DOLLAR * 2_000),
        ],
        ..h(10_000)
    }
    .facts();
    let p = rules().penalty(&f, day(2027, 4, 15)).unwrap().value();
    let want = [1_233, 8_329, 20_329, 8_630];
    for (i, l) in p.lines().iter().enumerate() {
        assert_eq!(l.total(), USCents(want[i]), "line {}", i + 1);
    }
    assert_eq!(p.total(), USCents(38_521));
    let sched = rules().schedule(&f).unwrap().value();
    assert_eq!(sched[0].shortfall(), DOLLAR * 750);
    assert_eq!(sched[1].shortfall(), DOLLAR * 500);

    // Last year's overpayment applied forward counts on April 15.
    let over = estimated::new_payment()
        .amount(DOLLAR * 1_750)
        .from_prior_year()
        .build()
        .unwrap();
    let s = rules()
        .schedule(
            &Household {
                payments: vec![over],
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    assert!(s[0].status() == Status::Met && s[0].credited_by() == DOLLAR * 1_750);
}

// Withholding counts a quarter on each due date, whenever withheld, unless
// the filer shows the dates.
#[test]
fn withholding() {
    // $4,000 withheld in December; 70% of $10,000 is $7,000, $1,750 a
    // quarter. $1,000 counts on each date, so April is $750 short, and each
    // later $1,000 first pays the oldest balance.
    let mut hh = Household {
        withheld: DOLLAR * 4_000,
        ..h(10_000)
    };
    let s = rules().schedule(&hh.facts()).unwrap().value();
    let want_short = [75_000, 150_000, 175_000, 175_000];
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.credited_by(), DOLLAR * 1_000 * (i as i64 + 1));
        assert_eq!(inst.shortfall(), USCents(want_short[i]));
    }
    hh.as_withheld = true;
    let res = rules().schedule(&hh.facts()).unwrap();
    assert_eq!(res.value()[0].shortfall(), DOLLAR * 1_750);
    assert!(has(&res.required(), PositionKind::UnsettledRule));
    assert!(applied(
        &res.applied(),
        "credited on the dates it was withheld"
    ));
    // Withholding at the target: nothing required, and no interest.
    let hh = Household {
        withheld: DOLLAR * 7_000,
        ..h(10_000)
    };
    assert!(!rules().requirement(&hh.facts()).unwrap().value().required());
    assert_eq!(
        rules()
            .penalty(&hh.facts(), day(2027, 4, 15))
            .unwrap()
            .value()
            .total(),
        USCents(0)
    );
}

// OW-8-P-SUP-I with the 2026 periods. Tax after credits on the annualised
// income of $2,000, $4,000, $8,000 and $9,000; the regular quarter is $1,750.
//
//	17.5% of $2,000 = $350, under $1,750:          $350
//	35% of $4,000 = $1,400 less $350 = $1,050:     $1,050
//	52.5% of $8,000 = $4,200 less $1,400 = $2,800: $2,800, under $1,750 plus the $2,100 not yet made up
//	70% of $9,000 = $6,300 less $4,200 = $2,100:   $2,100, under $1,750 plus the $1,050 not yet made up
#[test]
fn annualised_method() {
    let hh = Household {
        annualised: Some(vec![
            DOLLAR * 2_000,
            DOLLAR * 4_000,
            DOLLAR * 8_000,
            DOLLAR * 9_000,
        ]),
        ..h(10_000)
    };
    let s = rules().schedule(&hh.facts()).unwrap().value();
    for (i, want) in [35_000, 140_000, 420_000, 630_000].into_iter().enumerate() {
        assert_eq!(s[i].needed_by(), USCents(want), "installment {}", i + 1);
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
    let bad = estimated::new_facts(OK, 2026)
        .filing_status(filing::Status::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .elections(estimated::new_elections().annualised().build().unwrap())
        .periods(&ps)
        .build()
        .unwrap();
    assert!(rules().schedule(&bad).is_err());
}

// The exceptions: tax under $1,000, under $500 over withholding, no return
// required last year, a filed return showing no tax, and farming.
#[test]
fn exceptions() {
    // Under $1,000: exempt, with the test's basis asked about.
    let res = rules().requirement(&h(900).facts()).unwrap();
    assert!(res.value().exempt() && has(&res.required(), PositionKind::UnsettledRule));
    assert_eq!(
        rules()
            .penalty(&h(900).facts(), day(2027, 4, 15))
            .unwrap()
            .value()
            .total(),
        USCents(0)
    );

    // Not required to file last year: the form excuses everyone, and the
    // statute only a full-year resident.
    for (name, resident, ask) in [
        ("resident", Some(true), PositionKind::Unspecified),
        ("residence not given", None, PositionKind::Residency),
        ("part-year", Some(false), PositionKind::UnsettledRule),
    ] {
        let mut b = estimated::new_prior_return();
        b.not_required_to_file();
        if let Some(r) = resident {
            b.full_year_resident(r);
        }
        let p = b.build().unwrap();
        let res = rules()
            .penalty(
                &Household {
                    prior: Some(p),
                    ..h(50_000)
                }
                .facts(),
                day(2027, 4, 15),
            )
            .unwrap();
        assert_eq!(res.value().total(), USCents(0), "{name}");
        if ask == PositionKind::Unspecified {
            assert!(res.required().is_empty(), "{name}: {:?}", res.required());
        } else {
            assert!(has(&res.required(), ask), "{name}: {ask} is not asked");
        }
    }

    // A filed return showing no tax gives a target of nothing.
    let r = rules()
        .requirement(
            &Household {
                prior: filed(USCents(0)),
                ..h(50_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    assert!(!r.required() && r.target().0 == 0);

    // A farmer this year is exempt; a fisher has no exception.
    let r = rules()
        .requirement(
            &Household {
                farm_share: Rate(7_000),
                ..h(50_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    assert!(r.exempt() && r.why().contains("two-thirds"), "{}", r.why());
    let fisher = estimated::new_facts(OK, 2026)
        .filing_status(filing::Status::Single)
        .fish_share(Rate(7_000))
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 50_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules().requirement(&fisher).unwrap();
    assert!(!res.value().exempt() && res.value().required());
    // Last year's farm share not given is asked for.
    assert!(has(&res.required(), PositionKind::JurisdictionFact));

    // A waiver asked for is not given.
    let res = rules()
        .penalty(
            &Household {
                waiver: Some(WaiverKind::Casualty),
                ..h(10_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert_eq!(res.value().total(), USCents(93_110));
    assert!(applied(&res.applied(), "have no waiver"));
}

// A return filed with the balance paid stops the interest that day.
#[test]
fn return_filed_stops_interest() {
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
    // April's $1,750 for 320 days: $306.85.
    assert_eq!(p.lines()[0].total(), USCents(30_685));
}

// Each divergence is reported with both readings and both sources.
#[test]
fn divergences_pin_both_readings() {
    let res = rules().requirement(&h(10_000).facts()).unwrap();
    let want: [(&str, [&str; 4]); 6] = [
        (
            "No liability last year",
            [
                "didn't have to file",
                "OW-8-P (2025), Part 1 line 7",
                "lived in Oklahoma all of a 12-month prior year",
                "§ 2385.13(C)",
            ],
        ),
        (
            "A return filed and paid by January 31",
            [
                "OW-8-P has no step for it",
                "OW-8-P (2025), Part 3",
                "waives the January 15 installment",
                "§ 2385.9(A)(1)",
            ],
        ),
        (
            "Who must make estimated payments",
            [
                "70% of this year's tax and last year's tax",
                "OW-8-ES (revised 6-2025)",
                "no condition on withholding",
                "§ 2385.7(A)",
            ],
        ),
        (
            "Married individuals",
            [
                "its own tax and withholding",
                "OW-8-ES (revised 6-2025)",
                "combined tax over withholding",
                "§ 2385.7(A)(2)",
            ],
        ),
        (
            "The $1,000 test",
            [
                "after refundable and nonrefundable credits",
                "OW-8-P (2025), Part 1 lines 1 to 3",
                "doesn't say whether",
                "§ 2385.13(C)",
            ],
        ),
        (
            "The farmer share",
            [
                "66.67% (two-thirds)",
                "line 22",
                "66 2/3%",
                "§ 2385.7(B)(2)",
            ],
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, w)) = want.iter().find(|w| w.0 == d.rule()) else {
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
    }
    // The figure follows the form on each: a filer who filed separately is
    // tested on their own return.
    let r = rules()
        .requirement(
            &Household {
                status: filing::Status::MarriedFilingSeparately,
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(r.value().required() && applied(&r.applied(), "combined liability"));
    // A single filer is untouched by the married rule; the separate filer is.
    for (res, affects) in [(&res, false), (&r, true)] {
        for d in res.divergences() {
            if d.rule() == "Married individuals" {
                assert_eq!(d.affects_household(), affects);
                assert_eq!(d.uses(), Reading::Form);
            }
        }
    }
}

// Facts for another jurisdiction, another year or with another state's
// extras are refused, and a year not built is NoRules.
#[test]
fn refuses_what_is_not_built() {
    assert!(
        oklahomataxcalc::estimated_tax(2025)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NoRules))
    );
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let cases: [(&str, Facts); 3] = [
        (
            "Pennsylvania's facts",
            estimated::new_facts(estimated::PENNSYLVANIA, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "2027",
            estimated::new_facts(OK, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(OK, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("not Oklahoma's")
                .build()
                .unwrap(),
        ),
    ];
    for (name, f) in cases {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&h(10_000).facts(), Date::default())
            .is_err()
    );
    for r in [-1, 10_001] {
        assert!(
            oklahomataxcalc::new_estimated_facts()
                .prior_year_farm_share(Rate(r))
                .build()
                .is_err(),
            "a farm share of {r} was accepted"
        );
    }
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
        "70%",
        "20% a year",
        "$1,000",
        "January 31",
        "17.5%",
        "no 110% tier",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.versus_federal().is_empty() && !e.sources().is_empty());
}

// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
// never as a negative amount.
#[test]
fn withholding_over_the_tax_reads_as_a_refund() {
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
    let f = estimated::new_facts(OK, 2026)
        .filing_status(filing::Status::Single)
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
        assert!(!s.contains("−$") && !s.contains("-$"), "{s:?}");
    }
}

// Oklahoma's estimated tax statutes, 68 O.S. §§ 2385.7 and 2385.13, have no
// casualty, retirement or disability waiver, so even a retirement the
// federal rules would waive is refused.
#[test]
fn waiver_outcomes() {
    let mut hh = h(10_000);
    let before = rules()
        .penalty(&hh.facts(), day(2027, 4, 15))
        .unwrap()
        .value()
        .total();
    assert!(before.0 != 0);
    let born = day(1962, 3, 1);
    let cases = [
        (
            "retired in 2026 at 64",
            "no waiver of this interest for retirement or disability",
            estimated::new_positions()
                .waiver_for(WaiverCause::Retirement, day(2026, 6, 1))
                .born(born)
                .build()
                .unwrap(),
        ),
        (
            "retired in 2020",
            "for retirement or disability",
            estimated::new_positions()
                .waiver_for(WaiverCause::Retirement, day(2020, 6, 1))
                .born(born)
                .build()
                .unwrap(),
        ),
        (
            "disaster",
            "postpones the due dates by order",
            estimated::new_positions()
                .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
                .build()
                .unwrap(),
        ),
    ];
    for (name, phrase, pos) in cases {
        hh.positions = Some(pos);
        let p = rules()
            .penalty(&hh.facts(), day(2027, 4, 15))
            .unwrap()
            .value();
        let o = p.waiver_outcome().unwrap();
        assert!(
            o.refused()
                && o.reason().contains(phrase)
                && p.total() == before
                && p.if_waived() == before,
            "{name}: {o:?}"
        );
    }
}
