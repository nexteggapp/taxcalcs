//! Oregon's estimated tax rules for 2026, checked against the worked examples
//! of Publication OR-ESTIMATE and the Form OR-10 instructions. Ported from
//! estimated_test.go. The Go tests that find each figure in its extract in
//! testdata/ (TestEstimatedFiguresAreInTheirSources,
//! TestEstimatedRulesWereNotAmendedIn2026, and the extract checks at the head
//! of several tests) read the Go package's citations and are not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, ResidencePeriod,
    Rounding, Status, WaiverCause, WaiverKind, Weekday,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_oregontaxcalc as oregontaxcalc;
use working::{PositionKind, Required};

const OR: estimated::Jurisdiction = estimated::OREGON;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    oregontaxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
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
        .map(|(i, &x)| pay(dues()[i], DOLLAR * x))
        .collect()
}

#[derive(Clone, Default)]
struct Household {
    /// After all credits, Form OR-10 worksheet line 3.
    tax: USCents,
    /// Withheld on December 15, spread by the rules.
    withheld: USCents,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<oregontaxcalc::EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    annualised: Option<Vec<USCents>>,
    no_tax: bool,
    farm: Rate,
    fish: Rate,
    waiver: Option<WaiverKind>,
    residence: Vec<ResidencePeriod>,
    gross: USCents,
    as_withheld: bool,
    positions: Option<Positions>,
}

fn h(tax: i64) -> Household {
    Household {
        tax: DOLLAR * tax,
        ..Household::default()
    }
}

fn hc(tax: i64) -> Household {
    Household {
        tax: USCents(tax),
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
        let mut b = estimated::new_facts(OR, 2026);
        b.filing_status(filing::Status::MarriedFilingJointly)
            .this_year(fb.build().unwrap())
            .paid(&self.payments)
            .requirement_first_met(self.first_met)
            .return_filed(self.return_filed)
            .farm_share(self.farm)
            .fish_share(self.fish)
            .residence(&self.residence);
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
                oregontaxcalc::new_estimated_facts()
                    .prior_return_filed_first(true)
                    .gross_income_test_met(true)
                    .build()
                    .unwrap(),
            ),
        };
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

/// A filed 12-month return with its tax after credits.
fn prior(tax: USCents, full_year: bool) -> Option<PriorReturn> {
    let f = estimated::new_figures().tax(tax).build().unwrap();
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(f)
            .full_year_resident(full_year)
            .build()
            .unwrap(),
    )
}

fn facts_extras(e: &oregontaxcalc::EstimatedFactsBuilder) -> Option<oregontaxcalc::EstimatedFacts> {
    Some(e.build().unwrap())
}

#[test]
fn holidays_follow_ors187() {
    let nth = |year: i64, month: i64, wd: Weekday, n: i64| {
        let mut d = day(year, month, 1);
        while d.weekday() != wd {
            d = d.add_days(1);
        }
        d.add_days(7 * (n - 1))
    };
    let last_monday = |year: i64| {
        let mut d = day(year, 5, 31);
        while d.weekday() != Weekday::MONDAY {
            d = d.add_days(-1);
        }
        d
    };
    let want = [
        (day(2026, 1, 1), "New Year's Day"),
        (
            nth(2026, 1, Weekday::MONDAY, 3),
            "Martin Luther King, Jr.'s Birthday",
        ),
        (nth(2026, 2, Weekday::MONDAY, 3), "Presidents Day"),
        (last_monday(2026), "Memorial Day"),
        (day(2026, 6, 19), "Juneteenth"),
        (day(2026, 7, 4), "Independence Day"),
        (
            day(2026, 7, 4).add_days(-1),
            "Independence Day, observed on the Friday before",
        ),
        (nth(2026, 9, Weekday::MONDAY, 1), "Labor Day"),
        (day(2026, 11, 11), "Veterans Day"),
        (nth(2026, 11, Weekday::THURSDAY, 4), "Thanksgiving Day"),
        (day(2026, 12, 25), "Christmas Day"),
        (day(2027, 1, 1), "New Year's Day"),
        (
            nth(2027, 1, Weekday::MONDAY, 3),
            "Martin Luther King, Jr.'s Birthday",
        ),
        (nth(2027, 2, Weekday::MONDAY, 3), "Presidents Day"),
    ];
    assert_eq!(day(2026, 7, 4).weekday(), Weekday::SATURDAY);
    let cal = oregontaxcalc::estimated_calendar(2026).unwrap();
    let got = cal.holidays();
    assert_eq!(got.len(), want.len());
    for hol in got {
        assert!(
            want.iter().any(|(d, n)| *d == hol.on() && *n == hol.name()),
            "{} {:?} is not an ORS 187.010 holiday in the 2026 window",
            hol.on(),
            hol.name()
        );
    }
    // The calendar moves a date on a weekend or holiday to the next
    // business day. June 15, 2025 was a Sunday, and the 2025 instructions
    // print June 16.
    for (from, to) in [
        (day(2025, 6, 15), day(2025, 6, 16)),
        (day(2026, 6, 19), day(2026, 6, 22)),
        (day(2026, 7, 3), day(2026, 7, 6)),
        (day(2027, 1, 16), day(2027, 1, 19)),
        (day(2026, 11, 26), day(2026, 11, 27)),
        (day(2026, 4, 15), day(2026, 4, 15)),
    ] {
        assert_eq!(cal.next_business_day(from), to, "{from}");
    }
}

// The 2026 dates are the ones OR-ESTIMATE and OR-40-V print, and none moves.
#[test]
fn due_dates_for_twenty_twenty_six() {
    let cal = oregontaxcalc::estimated_calendar(2026).unwrap();
    let res = rules().schedule(&h(10_000).facts()).unwrap();
    assert_eq!(res.value().len(), 4);
    for (i, s) in res.value().iter().enumerate() {
        assert_eq!(s.due(), dues()[i]);
        assert!(cal.is_business_day(s.due()));
    }
}

// Publication OR-ESTIMATE's Example 3: Allan and Louise owe $3,912 after
// credits and expect $721 of withholding. 90% is $3,521, so $2,800 of
// estimates, $700 a quarter. The publication rounds to dollars; this does
// not, so 90% is $3,520.80 and the estimates $2,799.80.
#[test]
fn requirement_matches_or_estimate_example() {
    let mut hh = Household {
        withheld: DOLLAR * 721,
        ..h(3_912)
    };
    let r = rules().requirement(&hh.facts()).unwrap().value();
    assert!(
        r.required() && r.target() == USCents(352_080),
        "{}",
        r.why()
    );
    let est = r.target() - hh.withheld;
    assert_eq!(USCents((est.0 + 50) / 100 * 100), DOLLAR * 2_800);
    let sched = rules().schedule(&hh.facts()).unwrap();
    for (i, s) in sched.value().iter().enumerate() {
        let n = i as i64 + 1;
        assert_eq!(s.needed_by(), USCents(88_020 * n));
        assert_eq!(s.credited_by(), USCents(18_025 * n));
    }
    // Paying the published $700 on each date leaves nothing short.
    hh.payments = quarterly(&[700, 700, 700, 700]);
    for s in rules().schedule(&hh.facts()).unwrap().value() {
        assert_eq!(s.status(), Status::Met);
    }
    assert_eq!(
        rules()
            .penalty(&hh.facts(), day(2027, 4, 15))
            .unwrap()
            .value()
            .total(),
        USCents(0)
    );
}

// The Form OR-10 instructions' example: Catelyn owes $5,000 a period, has
// $3,000 withheld a period, and pays $1,200 on May 23, $1,300 on June 6,
// $2,500 on July 17 and $1,500 on August 14. With 2025's dates and 0.000247
// the worksheet gives $19, $3 and $11, $33 by September 15.
#[test]
fn interest_matches_or10_example() {
    let f2025 = estimated::parse_factor("0.000247").unwrap();
    let f2026 = estimated::parse_factor("0.000219").unwrap();
    let series = estimated::new_factor_series(Rounding::RoundToDollar)
        .period(
            day(2025, 1, 1),
            day(2025, 12, 31),
            f2025,
            percent(9),
            "Form OR-10 instructions (2025)",
        )
        .period(
            day(2026, 1, 1),
            day(2026, 12, 31),
            f2026,
            percent(8),
            "Form OR-10 instructions (2025)",
        )
        .build()
        .unwrap();
    let dues2025 = [
        day(2025, 4, 15),
        day(2025, 6, 16),
        day(2025, 9, 15),
        day(2026, 1, 15),
    ];
    let (five, three) = (DOLLAR * 5_000, DOLLAR * 3_000);
    let payments = [
        pay(day(2025, 5, 23), DOLLAR * 1_200),
        pay(day(2025, 6, 6), DOLLAR * 1_300),
        pay(day(2025, 7, 17), DOLLAR * 2_500),
        pay(day(2025, 8, 14), DOLLAR * 1_500),
    ];
    let (parts, unrated) = oregontaxcalc::worksheet_interest(
        &dues2025,
        &[five; 4],
        &[three; 4],
        &payments,
        day(2025, 9, 15),
        &series,
    )
    .unwrap();
    assert!(unrated.is_zero());
    let want = [(2_000, 38, 19), (800, 14, 3), (1_500, 31, 11)];
    assert_eq!(parts.len(), want.len());
    let mut total = USCents(0);
    for (i, (balance, days, charge)) in want.into_iter().enumerate() {
        let p = &parts[i];
        assert!(
            p.principal() == DOLLAR * balance && p.days() == days && p.charge() == DOLLAR * charge,
            "line {}: {p:?}",
            i + 1
        );
        total += p.charge();
    }
    assert_eq!(total, DOLLAR * 33);

    // The same household under the 2026 rules: June 15 rather than June 16,
    // so 32 days for the June balance, at 0.000219. $16.64, $2.45 and $10.51
    // round to $17, $2 and $11.
    let hh = Household {
        withheld: DOLLAR * 12_000,
        payments: vec![
            pay(day(2026, 5, 23), DOLLAR * 1_200),
            pay(day(2026, 6, 6), DOLLAR * 1_300),
            pay(day(2026, 7, 17), DOLLAR * 2_500),
            pay(day(2026, 8, 14), DOLLAR * 1_500),
        ],
        ..hc(2_222_222)
    };
    assert_eq!(
        rules().requirement(&hh.facts()).unwrap().value().target(),
        DOLLAR * 20_000
    );
    let p = rules()
        .penalty(&hh.facts(), day(2026, 9, 15))
        .unwrap()
        .value();
    assert_eq!(p.total(), DOLLAR * 30);
    let lines = p.lines();
    assert!(
        lines.len() == 2
            && lines[0].parts().len() == 2
            && lines[1].parts()[0].days() == 32
            && lines[1].total() == DOLLAR * 11,
        "{lines:?}"
    );
}

// A balance carried past December 31, 2026 is charged to that day at 8% and
// stops: no rate is published from January 1, 2027.
#[test]
fn interest_stops_where_the_rates_end() {
    let hh = hc(4_444_444); // 90% is $40,000
    let p = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
    let lines = p.value().lines();
    assert_eq!(lines.len(), 4);
    let want: [&[i64]; 4] = [&[131], &[2, 399], &[4, 703], &[]];
    for (i, w) in want.iter().enumerate() {
        let parts = lines[i].parts();
        assert_eq!(parts.len(), w.len(), "line {}", i + 1);
        for (j, &c) in w.iter().enumerate() {
            assert!(
                parts[j].charge() == DOLLAR * c && parts[j].rate() == percent(8),
                "line {} part {}",
                i + 1,
                j + 1
            );
        }
    }
    let last = lines[2].parts()[1].clone();
    assert!(
        last.to() == day(2026, 12, 31) && last.days() == 107 && last.principal() == DOLLAR * 30_000
    );
    assert_eq!(lines[3].shortfall(), DOLLAR * 40_000);
    assert_eq!(p.value().total(), DOLLAR * 1_239);
    assert!(has(&p.required(), PositionKind::UnpublishedRate));
    // To December 31 every day has a rate, and nothing is reported.
    let p = rules().penalty(&hh.facts(), day(2026, 12, 31)).unwrap();
    assert!(!has(&p.required(), PositionKind::UnpublishedRate));
    assert_eq!(p.value().total(), DOLLAR * 1_239);
}

// The interest follows the printed 0.000219, not 8% ÷ 365. On $1,000,000
// for 60 days the two give $13,140 and $13,151.
#[test]
fn interest_uses_the_printed_daily_factor() {
    let hh = hc(444_444_444); // 90% is $4,000,000, $1,000,000 a quarter
    let p = rules()
        .penalty(&hh.facts(), day(2026, 6, 15))
        .unwrap()
        .value();
    let parts = p.lines()[0].parts();
    assert!(
        parts.len() == 1 && parts[0].principal() == DOLLAR * 1_000_000 && parts[0].days() == 60
    );
    assert_eq!(parts[0].charge(), DOLLAR * 13_140);
}

// The $1,000 test is on tax after credits less withholding, and exactly
// $1,000 is charged.
#[test]
fn threshold_boundaries() {
    for (withheld, exempt) in [(900_001, true), (900_000, false)] {
        let res = rules()
            .requirement(
                &Household {
                    withheld: USCents(withheld),
                    ..h(10_000)
                }
                .facts(),
            )
            .unwrap();
        let r = res.value();
        assert_eq!(r.exempt(), exempt, "withheld {withheld}: {}", r.why());
        if exempt {
            let c = r.chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && c.label() == "Tax less withholding under $1,000"
            );
        }
        // Only exactly $1,000 left turns on the "Exactly $1,000" reading.
        for d in res.divergences() {
            if d.rule() == "Exactly $1,000" {
                assert_eq!(d.affects_household(), !exempt, "withheld {withheld}");
            }
        }
    }
    // Whether payments are needed: withholding one cent short of the
    // required annual payment needs them; reaching it does not.
    for (withheld, required) in [(1_799_999, true), (1_800_000, false)] {
        let r = rules()
            .requirement(
                &Household {
                    withheld: USCents(withheld),
                    ..h(20_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        assert!(
            r.required() == required && !r.exempt(),
            "withheld {withheld}"
        );
    }
    // A shortfall under $1,000 after withholding is still charged, as Form
    // OR-10 charges it, though the estimate worksheet says no payments are
    // required.
    let hh = Household {
        withheld: DOLLAR * 8_500,
        ..h(10_000)
    };
    let r = rules().requirement(&hh.facts()).unwrap().value();
    assert!(r.required() && r.why().contains("worksheet"), "{}", r.why());
    assert!(
        rules()
            .penalty(&hh.facts(), day(2027, 4, 15))
            .unwrap()
            .value()
            .total()
            .0
            != 0
    );
}

// Oregon has no 110% tier: last year's tax is 100% at any income.
#[test]
fn target_options() {
    for (prior_tax, basis, target) in [
        (15_000, Basis::PriorYearTax, 15_000),
        (19_000, Basis::ThisYearTax, 18_000),
    ] {
        let hh = Household {
            prior: prior(DOLLAR * prior_tax, true),
            gross: DOLLAR * 2_000_000,
            ..h(20_000)
        };
        let r = rules().requirement(&hh.facts()).unwrap().value();
        assert!(r.chosen().unwrap().basis() == basis && r.target() == DOLLAR * target);
        let o = r.options()[1].clone();
        assert!(o.percent() == percent(100) && o.amount() == DOLLAR * prior_tax);
    }

    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(9)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .build()
        .unwrap();
    let late = facts_extras(
        oregontaxcalc::new_estimated_facts()
            .prior_return_filed_first(false)
            .gross_income_test_met(true),
    );
    let unsaid = facts_extras(oregontaxcalc::new_estimated_facts().gross_income_test_met(true));
    let cases = [
        (
            "no return on record",
            Household::default(),
            false,
            PositionKind::PriorYearReturn,
        ),
        (
            "not filed",
            Household {
                prior: Some(not_filed),
                ..Household::default()
            },
            false,
            PositionKind::Unspecified,
        ),
        (
            "short year",
            Household {
                prior: Some(short),
                ..Household::default()
            },
            false,
            PositionKind::Unspecified,
        ),
        (
            "filed after this year's",
            Household {
                prior: prior(USCents(100), true),
                extras: late,
                ..Household::default()
            },
            false,
            PositionKind::Unspecified,
        ),
        (
            "filing order not said",
            Household {
                prior: prior(USCents(100), true),
                extras: unsaid,
                ..Household::default()
            },
            true,
            PositionKind::JurisdictionFact,
        ),
        (
            "part-year return",
            Household {
                prior: prior(USCents(100), false),
                ..Household::default()
            },
            true,
            PositionKind::Unspecified,
        ),
    ];
    for (name, mut hh, available, ask) in cases {
        hh.tax = DOLLAR * 10_000;
        let res = rules().requirement(&hh.facts()).unwrap();
        let o = res.value().options()[1].clone();
        assert_eq!(o.available(), available, "{name}");
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}: {ask} is not asked");
        }
        if !o.available() {
            assert_eq!(o.amount(), USCents(0), "{name}");
        }
    }
    for (hh, want) in [
        (h(10_000), "No 2025 Oregon return on record"),
        (
            Household {
                prior: Some(not_filed),
                ..h(10_000)
            },
            "You didn't file a 2025 Oregon return",
        ),
    ] {
        let o = rules().requirement(&hh.facts()).unwrap().value().options()[1].clone();
        assert!(!o.available() && o.amount().0 == 0 && o.why_not() == want);
    }
}

// Without this year's tax, the target is last year's tax, and the interest
// figured against it is the most it can be.
#[test]
fn without_this_years_tax() {
    let hh = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000, true),
        ..Household::default()
    };
    let res = rules().requirement(&hh.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 8_000, "{}", r.why());
    let o = r.options()[0].clone();
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&hh.facts()).unwrap();
    for (i, s) in sched.value().iter().enumerate() {
        assert_eq!(s.needed_by(), DOLLAR * 2_000 * (i as i64 + 1));
    }
    let pen = rules()
        .penalty(&hh.facts(), day(2026, 12, 31))
        .unwrap()
        .value();
    assert!(pen.upper_bound() && pen.total().0 != 0);

    // No return either: no target, and nothing to schedule.
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

    // Exception 2 needs nothing about this year.
    let zero = Household {
        no_tax: true,
        prior: prior(USCents(0), true),
        ..Household::default()
    };
    let r = rules().requirement(&zero.facts()).unwrap().value();
    assert!(r.exempt(), "{}", r.why());
}

// Exception 1: two-thirds of gross income from farming or fishing, together.
// OR-ESTIMATE's Felicia has $50,000 of $90,000 from fishing, under it.
#[test]
fn farmers_and_fishers() {
    for (farm, fish, exempt) in [
        (6_667, 0, true),
        (6_666, 0, false),
        (0, 6_667, true),
        (3_334, 3_333, true),
        (0, 5_556, false), // Felicia
    ] {
        let hh = Household {
            farm: Rate(farm),
            fish: Rate(fish),
            ..h(10_000)
        };
        let r = rules().requirement(&hh.facts()).unwrap().value();
        assert!(
            r.exempt() == exempt && (!exempt || r.why().contains("farming or fishing")),
            "farm {farm}, fish {fish}: {}",
            r.why()
        );
        if exempt {
            assert_eq!(
                rules()
                    .penalty(&hh.facts(), day(2027, 4, 15))
                    .unwrap()
                    .value()
                    .total(),
                USCents(0)
            );
        }
    }
}

// Exception 2: no tax last year as a full-year resident. A part-year
// resident can't claim it, but last year's $0 still sets a $0 target.
#[test]
fn no_liability_last_year() {
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let unanswered = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(USCents(0)).build().unwrap())
        .build()
        .unwrap();
    for (name, p, exempt, ask) in [
        (
            "$0 as a full-year resident",
            prior(USCents(0), true),
            true,
            PositionKind::Unspecified,
        ),
        (
            "not required to file",
            Some(not_required),
            true,
            PositionKind::Unspecified,
        ),
        (
            "$0 as a part-year resident",
            prior(USCents(0), false),
            false,
            PositionKind::Unspecified,
        ),
        (
            "residence not given",
            Some(unanswered),
            false,
            PositionKind::Residency,
        ),
        (
            "$1 as a full-year resident",
            prior(USCents(100), true),
            false,
            PositionKind::Unspecified,
        ),
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
        assert_eq!(res.value().exempt(), exempt, "{name}");
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}");
        }
    }
    let r = rules()
        .requirement(
            &Household {
                prior: prior(USCents(0), false),
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    assert!(r.target().0 == 0 && !r.required());
}

// Exceptions 3 and 4 are the department's decision, so the interest is
// shown before them and the request is recorded.
#[test]
fn waivers_are_recorded_not_applied() {
    let mut hh = h(10_000);
    let base = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
    assert!(has(&base.required(), PositionKind::PenaltyWaiver));
    for k in [WaiverKind::Casualty, WaiverKind::RetiredOrDisabled] {
        hh.waiver = Some(k);
        let p = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
        assert!(
            p.value().total() == base.value().total()
                && p.value().waiver() == k
                && !has(&p.required(), PositionKind::PenaltyWaiver),
            "{k}"
        );
    }
}

// A late starter pays in 3, 2 or 1 equal installments, by OR-ESTIMATE's
// chart. Withholding is spread over the same dates.
#[test]
fn late_start() {
    for (met, need) in [
        (day(2026, 4, 1), [225_000, 450_000, 675_000, 900_000]),
        (day(2026, 4, 2), [0, 300_000, 600_000, 900_000]),
        (day(2026, 6, 1), [0, 300_000, 600_000, 900_000]),
        (day(2026, 6, 2), [0, 0, 450_000, 900_000]),
        (day(2026, 9, 1), [0, 0, 450_000, 900_000]),
        (day(2026, 9, 2), [0, 0, 0, 900_000]),
    ] {
        let hh = Household {
            first_met: met,
            ..h(10_000)
        };
        let res = rules().schedule(&hh.facts()).unwrap();
        for (i, s) in res.value().iter().enumerate() {
            assert_eq!(
                s.needed_by(),
                USCents(need[i]),
                "first met {met}, installment {}",
                i + 1
            );
        }
        assert_eq!(
            has(&res.required(), PositionKind::UnsettledRule),
            need[0] == 0,
            "first met {met}"
        );
    }
    let hh = Household {
        withheld: DOLLAR * 3_000,
        first_met: day(2026, 7, 1),
        ..h(10_000)
    };
    for (i, s) in rules()
        .schedule(&hh.facts())
        .unwrap()
        .value()
        .iter()
        .enumerate()
    {
        assert_eq!(s.credited_by(), USCents([0, 0, 150_000, 300_000][i]));
    }
    // Not told, the rules ask and use four.
    let res = rules().schedule(&h(10_000).facts()).unwrap();
    assert!(has(&res.required(), PositionKind::RequirementFirstMet));
}

// The annualised method: 22.5%, 45%, 67.5% and 90% of the net annualised
// tax, less earlier installments, capped by the regular installment plus
// what the method lowered before.
#[test]
fn annualised_method() {
    let needed = |ann: [i64; 4]| {
        let hh = Household {
            annualised: Some(ann.iter().map(|&a| USCents(a)).collect()),
            ..hc(1_111_111) // 90% is $10,000
        };
        rules().schedule(&hh.facts()).unwrap().value()
    };
    let s = needed([200_000, 400_000, 800_000, 1_111_111]);
    for (i, want) in [45_000, 180_000, 540_000, 1_000_000]
        .into_iter()
        .enumerate()
    {
        assert_eq!(s[i].needed_by(), USCents(want));
    }
    // A lower last period: 90% of $10,000 of annualised tax is $9,000.
    assert_eq!(
        needed([200_000, 400_000, 800_000, 1_000_000])[3].needed_by(),
        USCents(900_000)
    );
    // A large first period is capped at the regular $2,500.
    assert_eq!(
        needed([2_000_000, 2_000_000, 2_000_000, 1_111_111])[0].needed_by(),
        USCents(250_000)
    );
    // Periods that do not end on the worksheet's dates are refused.
    let ps: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 6, 30),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .into_iter()
    .map(|end| estimated::new_period().end(end).build().unwrap())
    .collect();
    let f = estimated::new_facts(OR, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .elections(estimated::new_elections().annualised().build().unwrap())
        .periods(&ps)
        .build()
        .unwrap();
    assert!(rules().requirement(&f).is_err());
}

// Payments go to the oldest unpaid installment first, and a balance that is
// paid stops accruing interest.
#[test]
fn payments_pay_the_oldest_first() {
    // $2,000 a quarter. Nothing in April; $4,000 on June 15.
    let hh = Household {
        payments: vec![pay(dues()[1], DOLLAR * 4_000)],
        ..hc(888_889)
    };
    let s = rules().schedule(&hh.facts()).unwrap().value();
    assert!(
        s[0].shortfall() == DOLLAR * 2_000
            && s[0].status() == Status::Short
            && s[1].status() == Status::Met
    );
    // April's $2,000 runs April 16 to June 14, 60 days: $26.28, so $26. June
    // 15 is the withholding line's own day: $0.44, so nothing.
    assert_eq!(
        rules()
            .penalty(&hh.facts(), day(2026, 9, 14))
            .unwrap()
            .value()
            .total(),
        DOLLAR * 26
    );
    // Last year's overpayment applied forward counts on April 15.
    let fwd = estimated::new_payment()
        .from_prior_year()
        .amount(DOLLAR * 8_000)
        .build()
        .unwrap();
    let hh = Household {
        payments: vec![fwd],
        ..hc(888_889)
    };
    for s in rules().schedule(&hh.facts()).unwrap().value() {
        assert_eq!(s.status(), Status::Met);
    }
    // Withholding on its dates, when the filer shows them: December's
    // withholding pays nothing in April.
    let hh = Household {
        withheld: DOLLAR * 18_000,
        as_withheld: true,
        ..h(20_000)
    };
    let s = rules().schedule(&hh.facts()).unwrap().value();
    assert!(s[0].status() == Status::Short && s[3].status() == Status::Met);
}

// Interest stops on April 15, 2027, or the day the return is filed and paid.
#[test]
fn interest_stops() {
    let mut hh = h(10_000);
    assert_eq!(
        rules()
            .penalty(&hh.facts(), day(2027, 6, 30))
            .unwrap()
            .value()
            .as_of(),
        day(2027, 4, 15)
    );
    hh.return_filed = day(2027, 2, 1);
    assert_eq!(
        rules()
            .penalty(&hh.facts(), day(2027, 4, 15))
            .unwrap()
            .value()
            .as_of(),
        day(2027, 2, 1)
    );
}

// ORS 316.563(1)'s gross income test: gross income over $20,000 meets it for
// every filer. At $20,000 or below the rules ask, and a filer who says it is
// not met needs no declaration, though the interest is still figured.
#[test]
fn gross_income_test() {
    let unsaid = facts_extras(oregontaxcalc::new_estimated_facts().prior_return_filed_first(true));
    for (gross, ask) in [(2_000_000, true), (2_000_001, false), (0, true)] {
        let res = rules()
            .requirement(
                &Household {
                    gross: USCents(gross),
                    extras: unsaid.clone(),
                    ..h(10_000)
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(
            has(&res.required(), PositionKind::JurisdictionFact),
            ask,
            "gross income {gross}"
        );
    }
    let not_met = facts_extras(
        oregontaxcalc::new_estimated_facts()
            .prior_return_filed_first(true)
            .gross_income_test_met(false),
    );
    let hh = Household {
        extras: not_met,
        ..h(10_000)
    };
    let r = rules().requirement(&hh.facts()).unwrap().value();
    assert!(!r.required() && !r.exempt() && r.target() == DOLLAR * 9_000);
    assert!(
        rules()
            .penalty(&hh.facts(), day(2027, 4, 15))
            .unwrap()
            .value()
            .total()
            .0
            != 0
    );
}

// Each divergence is reported with both readings and both sources.
#[test]
fn divergences_pin_both_readings() {
    let res = rules().requirement(&h(10_000).facts()).unwrap();
    let want = [
        ("Who must declare", "no gross income test", "ORS 316.563(1)"),
        (
            "When payments are required",
            "worksheet line 23",
            "OAR 150-316-0493(3)(d)",
        ),
        (
            "Exactly $1,000",
            "more than $1,000",
            "ORS 316.563(2), 316.588(1)",
        ),
        (
            "The prior-year return",
            "filed before the 2026 return",
            "ORS 316.587(8)(b)",
        ),
        (
            "A requirement first met after April 1",
            "3, 2 or 1 equal installments",
            "ORS 316.577, 316.579(3) to (5)",
        ),
        (
            "The daily interest factor",
            "0.000219",
            "OAR 150-305-0140(1), (3)(b)",
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
        // A household with no late start, no prior return and $10,000 left
        // after withholding is untouched by these readings.
        if d.rule() != "The daily interest factor" {
            assert!(!d.affects_household(), "{:?}", d.rule());
        }
    }
}

#[test]
fn refuses_what_is_not_built() {
    assert!(
        oregontaxcalc::estimated_tax(2025)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NoRules))
    );
    assert!(
        oregontaxcalc::estimated_calendar(2027)
            .unwrap_err()
            .is(&estimated::Error::NoRules)
    );
    for (name, state) in [("full year", "OR"), ("nonresident", "WA")] {
        let res = vec![
            estimated::new_residence_period(state)
                .from(day(2026, 1, 1))
                .to(day(2026, 12, 31))
                .build()
                .unwrap(),
        ];
        let hh = Household {
            residence: res,
            ..h(10_000)
        };
        assert!(rules().requirement(&hh.facts()).is_ok(), "{name}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
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
            estimated::new_facts(OR, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(OR, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("not Oregon's")
                .build()
                .unwrap(),
        ),
    ];
    for (name, f) in cases {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(rules().penalty(&hc(100).facts(), Date::default()).is_err());
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
        "$1,000",
        "90%",
        "100%",
        "no 110% tier",
        "Oregon legal holiday",
        "running balance",
        "8%",
        "whole dollar",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
}

/// A household that moved from California to Oregon on Jul 15, 2026.
fn moved_in() -> Vec<ResidencePeriod> {
    vec![
        estimated::new_residence_period("CA")
            .from(day(2026, 1, 1))
            .to(day(2026, 7, 14))
            .build()
            .unwrap(),
        estimated::new_residence_period("OR")
            .from(day(2026, 7, 15))
            .to(day(2026, 12, 31))
            .build()
            .unwrap(),
    ]
}

// Form OR-10 instructions, Line 3, note for part-year residents: the
// required annual payment is divided by the installment periods of Oregon
// residence. Moving in on Jul 15 puts residence in the periods ending Sep 15
// and Jan 15, so 90% of $4,000, $3,600, is $1,800 on each, and nothing is
// needed by Apr 15 or Jun 15. The $1,000 withheld is divided the same way.
#[test]
fn part_year_resident_divides_by_periods_of_residence() {
    let mut hh = Household {
        withheld: DOLLAR * 1_000,
        residence: moved_in(),
        ..h(4_000)
    };
    let res = rules().schedule(&hh.facts()).unwrap();
    let want = [0, 0, 1_800, 3_600];
    let credited = [0, 0, 500, 1_000];
    let got = res.value();
    assert_eq!(got.len(), 4);
    for (i, s) in got.iter().enumerate() {
        assert_eq!(s.needed_by(), DOLLAR * want[i], "installment {}", i + 1);
        assert_eq!(
            s.credited_by(),
            DOLLAR * credited[i],
            "installment {}",
            i + 1
        );
    }
    assert!(got[0].status() == Status::NothingDue && got[1].status() == Status::NothingDue);
    assert!(has(&res.required(), PositionKind::UnsettledRule));
    assert!(has(&res.required(), PositionKind::JurisdictionFact));

    // Oregon business income in the period ending Jun 15 adds that period:
    // $1,200 by each of the last three.
    hh.extras = facts_extras(
        oregontaxcalc::new_estimated_facts()
            .prior_return_filed_first(true)
            .gross_income_test_met(true)
            .business_income_in(&[2]),
    );
    let got = rules().schedule(&hh.facts()).unwrap().value();
    for (i, w) in [0, 1_200, 2_400, 3_600].into_iter().enumerate() {
        assert_eq!(got[i].needed_by(), DOLLAR * w, "installment {}", i + 1);
    }
    assert!(
        oregontaxcalc::new_estimated_facts()
            .business_income_in(&[5])
            .build()
            .is_err()
    );
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
    let f = estimated::new_facts(OR, 2026)
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

// ORS 316.587(5)(b) and (c) grant the federal pair: a casualty or disaster,
// and retirement at 62 or older or disability in the tax year or the one
// before.
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
    let pos = |c: WaiverCause, on: Date, born: Option<Date>| {
        let mut b = estimated::new_positions();
        b.waiver_for(c, on);
        if let Some(born) = born {
            b.born(born);
        }
        b.build().unwrap()
    };
    for (name, want, phrase, p) in [
        (
            "retired in 2026 at 64",
            "eligible",
            "retired in 2026 after reaching 62",
            pos(WaiverCause::Retirement, day(2026, 6, 1), Some(born)),
        ),
        (
            "retired in 2020",
            "refused",
            "covers retiring in 2025 or 2026",
            pos(WaiverCause::Retirement, day(2020, 6, 1), Some(born)),
        ),
        (
            "retired at 61",
            "refused",
            "reached 62",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1965, 1, 1)),
            ),
        ),
        (
            "disaster",
            "eligible",
            "The Oregon Department of Revenue can waive",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
        ),
        (
            "disabled in 2025",
            "eligible",
            "became disabled in 2025",
            pos(WaiverCause::Disability, day(2025, 11, 1), None),
        ),
    ] {
        hh.positions = Some(p);
        let pen = rules()
            .penalty(&hh.facts(), day(2027, 4, 15))
            .unwrap()
            .value();
        let o = pen.waiver_outcome().unwrap();
        let got = if o.eligible() {
            "eligible"
        } else if o.refused() {
            "refused"
        } else {
            "undecided"
        };
        let waived = if got == "eligible" {
            USCents(0)
        } else {
            before
        };
        assert!(
            got == want
                && o.reason().contains(phrase)
                && pen.total() == before
                && pen.if_waived() == waived,
            "{name}: {got}, {:?}",
            o.reason()
        );
    }
}

// A part-year resident $10 short on Sep 15 owes no interest: the worksheet
// rounds each line's interest to the nearest whole dollar, and 107 days on
// $10 is 23 cents. The installment stays short, and the penalty sentence
// says why there is none.
#[test]
fn shortfall_whose_interest_rounds_to_nothing() {
    let mut hh = Household {
        withheld: DOLLAR * 1_000,
        residence: moved_in(),
        payments: vec![
            pay(day(2026, 9, 15), DOLLAR * 1_290),
            pay(day(2026, 12, 1), DOLLAR * 1_400),
        ],
        ..h(4_000)
    };
    let f = hh.facts();
    let sep = rules().schedule(&f).unwrap().value()[2];
    assert!(sep.status() == Status::Short && sep.shortfall() == DOLLAR * 10);
    let p = rules().penalty(&f, day(2027, 12, 31)).unwrap().value();
    assert_eq!(p.total(), USCents(0));
    assert_eq!(
        p.summary(),
        "No penalty: Oregon's worksheet rounds the interest on the shortfall to $0."
    );
    // Met on every date, the sentence is the plain one.
    hh.payments = vec![
        pay(day(2026, 9, 15), DOLLAR * 1_300),
        pay(day(2026, 12, 1), DOLLAR * 1_400),
    ];
    assert_eq!(
        rules()
            .penalty(&hh.facts(), day(2027, 12, 31))
            .unwrap()
            .value()
            .summary(),
        "No penalty."
    );
}
