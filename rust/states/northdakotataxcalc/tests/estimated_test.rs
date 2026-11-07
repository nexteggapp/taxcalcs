//! North Dakota's estimated tax rules for 2026, ported from the Go package's
//! estimated_test.go. (Go also compares every figure against its extract in
//! testdata/; those checks stay in Go.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, Elections, EstimatedTax, Facts, Month, Payment, Penalty, Positions, PriorReturn,
    ResidencePeriod, Status, WaiverCause, WaiverKind, Weekday,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_northdakotataxcalc::{
    EstimatedFacts, EstimatedFactsBuilder, estimated_tax, new_estimated_facts,
};
use working::{PositionKind, Reading, Required};

const ND: estimated::Jurisdiction = estimated::NORTH_DAKOTA;

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

fn applied<T>(res: &working::Result<T>) -> String {
    res.applied().iter().map(|r| r.applied() + "\n").collect()
}

fn pay(on: Date, a: USCents) -> Payment {
    estimated::new_payment().on(on).amount(a).build().unwrap()
}

fn residence(state: &str, from: Date, to: Date) -> ResidencePeriod {
    estimated::new_residence_period(state)
        .from(from)
        .to(to)
        .build()
        .unwrap()
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
    prior: Option<PriorReturn>,
    no_prior: bool,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    return_filed: Date,
    elections: Option<Elections>,
    annualised: Vec<USCents>,
    farm_share: Rate,
    residence: Vec<ResidencePeriod>,
    prior_residence: Vec<ResidencePeriod>,
    waiver: Option<WaiverKind>,
    positions: Option<Positions>,
}

impl Household {
    fn facts(&self) -> Facts {
        let figures = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(ND, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(figures)
            .paid(&self.payments)
            .return_filed(self.return_filed)
            .farm_share(self.farm_share)
            .residence(&self.residence)
            .prior_residence(&self.prior_residence);
        if self.withheld.0 > 0 {
            b.withheld(&[estimated::new_withheld()
                .on(day(2026, 12, 15))
                .amount(self.withheld)
                .build()
                .unwrap()]);
        }
        if let Some(p) = self.prior {
            b.prior_year(p);
        } else if !self.no_prior {
            b.prior_year(prior(DOLLAR * 20_000));
        }
        if let Some(e) = self.extras {
            b.state(e);
        }
        if let Some(e) = self.elections {
            b.elections(e);
        }
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        if !self.annualised.is_empty() {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            let periods: Vec<_> = self
                .annualised
                .iter()
                .enumerate()
                .map(|(i, t)| {
                    estimated::new_period()
                        .end(ends[i])
                        .annualised_tax(*t)
                        .build()
                        .unwrap()
                })
                .collect();
            b.periods(&periods)
                .elections(estimated::new_elections().annualised().build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

fn prior(tax: USCents) -> PriorReturn {
    estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(tax).build().unwrap())
        .build()
        .unwrap()
}

fn extras(b: &mut EstimatedFactsBuilder) -> Option<EstimatedFacts> {
    Some(b.build().unwrap())
}

fn penalty(h: &Household) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap()
}

/// $8,000 of tax and nothing paid: 90% is $7,200, $1,800 a quarter.
fn base() -> Household {
    Household {
        tax: DOLLAR * 8_000,
        ..Household::default()
    }
}

fn tax(t: USCents) -> Household {
    Household {
        tax: t,
        ..Household::default()
    }
}

/// The Gregorian Easter Sunday, for Good Friday.
fn easter(y: i64) -> Date {
    let (a, b, c) = (y % 19, y / 100, y % 100);
    let (d, e) = (b / 4, b % 4);
    let g = (8 * b + 13) / 25;
    let h = (19 * a + b - d - g + 15) % 30;
    let (i, k) = (c / 4, c % 4);
    let l = (32 + 2 * e + 2 * i - h - k) % 7;
    let m = (a + 11 * h + 22 * l) / 451;
    let month = (h + l - 7 * m + 114) / 31;
    day(y, month, (h + l - 7 * m + 114) % 31 + 1)
}

fn nth_weekday(y: i64, m: i64, wd: Weekday, n: i64) -> Date {
    let mut d = day(y, m, 1);
    while d.weekday() != wd {
        d = d.add_days(1);
    }
    d.add_days(7 * (n - 1))
}

/// The § 1-03-01 holidays from April 2026 to April 2027. None is a due date,
/// and nor is April 15, 2027, so a weekends-only calendar moves nothing a
/// full one would.
#[test]
fn estimated_no_date_is_a_north_dakota_holiday() {
    assert_eq!(easter(2026), day(2026, 4, 5));
    assert_eq!(easter(2027), day(2027, 3, 28));
    let mut holidays = Vec::new();
    for y in [2026, 2027] {
        let mut last_may = day(y, 5, 31);
        while last_may.weekday() != Weekday::MONDAY {
            last_may = last_may.add_days(-1);
        }
        holidays.extend([
            day(y, 1, 1),
            nth_weekday(y, 1, Weekday::MONDAY, 3),
            nth_weekday(y, 2, Weekday::MONDAY, 3),
            easter(y).add_days(-2),
            last_may,
            day(y, 7, 4),
            day(y, 7, 3),
            nth_weekday(y, 9, Weekday::MONDAY, 1),
            day(y, 11, 11),
            nth_weekday(y, 11, Weekday::THURSDAY, 4),
            day(y, 12, 25),
        ]);
    }
    let s = rules().schedule(&base().facts()).unwrap().value();
    let mut dates = vec![day(2027, 4, 15)];
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        dates.push(inst.due());
    }
    for d in dates {
        let wd = d.weekday();
        assert!(
            wd != Weekday::SATURDAY && wd != Weekday::SUNDAY,
            "{d} is a {wd}"
        );
        assert!(!holidays.contains(&d), "{d} is a North Dakota holiday");
    }
}

/// A quarter of $7,200 by each date.
#[test]
fn estimated_schedule_for_twenty_twenty_six() {
    let s = rules().schedule(&base().facts()).unwrap().value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.needed_by(), DOLLAR * 1_800 * (i as i64 + 1));
        assert_eq!(inst.shortfall(), DOLLAR * 1_800);
        assert_eq!(inst.status(), Status::Short);
    }
}

/// Nothing paid on $7,200: $1,800 a quarter at 12% to April 15, 2027. $216,
/// $180, $126 and $54, $576 in all.
#[test]
fn estimated_interest_arithmetic() {
    let res = penalty(&base());
    let want_days = [365, 304, 212, 90];
    let want_charge = [21_600, 18_000, 12_600, 5_400];
    let want_factor = [
        "= 1.000, × 12% = 0.120",
        "= 0.833, × 12% = 0.100",
        "= 0.581, × 12% = 0.070",
        "= 0.247, × 12% = 0.030",
    ];
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    for i in 0..4 {
        let parts = lines[i].parts();
        assert_eq!(parts.len(), 1, "installment {}", i + 1);
        assert_eq!(parts[0].days(), want_days[i], "installment {}", i + 1);
        assert_eq!(
            parts[0].charge(),
            USCents(want_charge[i]),
            "installment {}",
            i + 1
        );
        assert_eq!(parts[0].rate(), percent(12));
        assert!(
            parts[0].source().contains(want_factor[i]),
            "{:?}",
            parts[0].source()
        );
    }
    assert_eq!(res.value().total(), USCents(57_600));
}

/// Pays the first three quarters on their dates and January's on `paid`.
fn on_time_but_january(paid: Date) -> Household {
    let d = dues();
    let k = DOLLAR * 1_800;
    Household {
        payments: vec![pay(d[0], k), pay(d[1], k), pay(d[2], k), pay(paid, k)],
        ..base()
    }
}

/// A day fraction of .004 or less rounds to a factor of zero.
#[test]
fn estimated_rounding_to_three_places() {
    for (paid, want) in [(day(2027, 1, 16), 0), (day(2027, 1, 17), 180)] {
        let res = penalty(&on_time_but_january(paid));
        assert_eq!(res.value().total(), USCents(want), "paid {paid}");
    }
}

/// Schedule ND-1UT applies a payment to its own quarter first.
#[test]
fn estimated_payment_pays_its_own_quarter_first() {
    let mut h = Household {
        payments: vec![pay(day(2026, 5, 1), DOLLAR * 3_600)],
        ..base()
    };
    let res = penalty(&h);
    let lines = res.value().lines();
    assert_eq!(lines.len(), 3);
    assert_eq!(lines[0].installment(), 1);
    assert_eq!(lines[0].days(), 61);
    assert_eq!(lines[0].total(), USCents(3_600));
    assert_eq!(lines[1].installment(), 3);
    assert_eq!(res.value().total(), USCents(3_600 + 12_600 + 5_400));
    assert!(because(
        &res.required(),
        PositionKind::UnsettledRule,
        "between due dates"
    ));

    // $1,800 on June 15 pays June, and April runs the whole year.
    h.payments = vec![pay(dues()[1], DOLLAR * 1_800)];
    let res = penalty(&h);
    let l = res.value().lines();
    assert_eq!(l[0].installment(), 1);
    assert_eq!(l[0].total(), USCents(21_600));
    assert_eq!(l[1].installment(), 3);
    assert!(!because(
        &res.required(),
        PositionKind::UnsettledRule,
        "between due dates"
    ));
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].status(), Status::Short);
    assert_eq!(s[1].status(), Status::Short);
    assert_eq!(s[1].shortfall(), USCents(0));
}

/// $2,700 in June's column pays June and half of April.
#[test]
fn estimated_part_of_a_shortfall_offset() {
    let h = Household {
        payments: vec![pay(day(2026, 6, 1), DOLLAR * 2_700)],
        ..base()
    };
    let res = penalty(&h);
    let parts = res.value().lines()[0].parts();
    assert_eq!(parts.len(), 2);
    assert_eq!(parts[0].principal(), DOLLAR * 900);
    assert_eq!(parts[0].charge(), USCents(1_800));
    assert_eq!(parts[1].principal(), DOLLAR * 900);
    assert_eq!(parts[1].charge(), USCents(10_800));
}

/// After January 15 a payment pays the earliest shortfall on its own date.
#[test]
fn estimated_payment_after_the_last_due_date() {
    let h = Household {
        payments: vec![pay(day(2027, 2, 15), DOLLAR * 1_800)],
        ..base()
    };
    let res = penalty(&h);
    let l = res.value().lines()[0].clone();
    assert_eq!(l.days(), 306);
    assert_eq!(l.total(), USCents(18_180));
    assert_eq!(
        res.value().total(),
        USCents(18_180 + 18_000 + 12_600 + 5_400)
    );
}

/// The interest stops on April 15, 2027, or on the day the return is filed
/// and paid.
#[test]
fn estimated_interest_stops() {
    let p = rules()
        .penalty(&base().facts(), day(2027, 12, 31))
        .unwrap()
        .value();
    assert_eq!(p.as_of(), day(2027, 4, 15));
    assert_eq!(p.total(), USCents(57_600));
    let h = Household {
        return_filed: day(2027, 3, 1),
        ..base()
    };
    let p = penalty(&h).value();
    assert_eq!(p.as_of(), day(2027, 3, 1));
    assert_eq!(p.lines()[3].days(), 45);
}

/// This year's tax less withholding under $1,000 owes nothing.
#[test]
fn estimated_threshold_boundary() {
    for (withheld, exempt) in [(700_001, true), (700_000, false)] {
        let h = Household {
            withheld: USCents(withheld),
            ..tax(DOLLAR * 8_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {withheld}");
    }
}

/// Last year's net tax liability under $1,000 means no estimates; a filer
/// not required to file counts as zero; no return on record is asked.
#[test]
fn estimated_prior_year_gate() {
    for (t, exempt) in [(99_999, true), (100_000, false)] {
        let h = Household {
            prior: Some(prior(USCents(t))),
            ..tax(DOLLAR * 8_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "last year {t}");
        if exempt {
            let c = res.value().chosen().unwrap();
            assert_eq!(c.basis(), Basis::Exception);
            assert_eq!(c.amount(), USCents(0));
            assert_eq!(c.label(), "Last year's North Dakota tax under $1,000");
        }
    }
    let nr = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let h = Household {
        prior: Some(nr),
        ..tax(DOLLAR * 8_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(res.value().exempt(), "{}", res.value().why());
    assert_eq!(penalty(&h).value().total(), USCents(0));
    let h = Household {
        no_prior: true,
        ..tax(DOLLAR * 8_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().exempt());
    assert!(!res.value().options()[1].available());
    assert!(because(
        &res.required(),
        PositionKind::PriorYearReturn,
        "Under $1,000, no estimates are needed"
    ));
}

/// Only a filer who must pay federal estimates owes North Dakota's.
#[test]
fn estimated_federal_condition() {
    let mut h = base();
    h.extras = extras(new_estimated_facts().federal_estimates_required(false));
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(res.value().exempt(), "{}", res.value().why());
    let res = rules().requirement(&base().facts()).unwrap();
    assert!(!res.value().exempt());
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "pay federal estimated tax"
    ));
    h.extras = extras(new_estimated_facts().federal_estimates_required(true));
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().exempt());
    assert!(!because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "pay federal estimated tax"
    ));
}

/// The lesser of 90% of this year's tax and 100% of last year's, with no
/// twelve-month test; a filer who moved in last year has only the 90% test.
#[test]
fn estimated_target_options() {
    let with_prior = |p: PriorReturn| Household {
        prior: Some(p),
        ..tax(DOLLAR * 8_000)
    };
    let res = rules()
        .requirement(&with_prior(prior(DOLLAR * 7_199)).facts())
        .unwrap();
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::PriorYearTax);
    assert_eq!(c.percent(), percent(100));
    assert_eq!(res.value().target(), DOLLAR * 7_199);
    let res = rules()
        .requirement(&with_prior(prior(DOLLAR * 7_201)).facts())
        .unwrap();
    let c = res.value().chosen().unwrap();
    assert_eq!(c.basis(), Basis::ThisYearTax);
    assert_eq!(c.percent(), percent(90));
    assert_eq!(res.value().target(), DOLLAR * 7_200);
    let figs = || {
        estimated::new_figures()
            .tax(DOLLAR * 5_000)
            .build()
            .unwrap()
    };
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(figs())
        .build()
        .unwrap();
    let res = rules().requirement(&with_prior(short).facts()).unwrap();
    assert_eq!(res.value().target(), DOLLAR * 5_000);
    let not_filed = estimated::new_prior_return()
        .filed(false)
        .figures(figs())
        .build()
        .unwrap();
    let res = rules().requirement(&with_prior(not_filed).facts()).unwrap();
    assert!(!res.value().options()[1].available());
    let mover = Household {
        prior_residence: vec![
            residence("MN", day(2025, 1, 1), day(2025, 6, 30)),
            residence("ND", day(2025, 7, 1), day(2025, 12, 31)),
        ],
        ..with_prior(prior(DOLLAR * 5_000))
    };
    let res = rules().requirement(&mover.facts()).unwrap();
    assert!(!res.value().options()[1].available());
    assert_eq!(res.value().target(), DOLLAR * 7_200);
    assert_eq!(
        res.value().options()[1].why_not(),
        "You moved into North Dakota during 2025"
    );
    let stayed = Household {
        prior_residence: vec![residence("ND", day(2025, 1, 1), day(2025, 12, 31))],
        ..mover
    };
    let res = rules().requirement(&stayed.facts()).unwrap();
    assert_eq!(res.value().target(), DOLLAR * 5_000);
    let unsure = estimated::new_prior_return()
        .filed(true)
        .full_year_resident(false)
        .figures(figs())
        .build()
        .unwrap();
    let res = rules().requirement(&with_prior(unsure).facts()).unwrap();
    assert_eq!(res.value().target(), DOLLAR * 5_000);
    assert!(because(
        &res.required(),
        PositionKind::Residency,
        "If you moved in that year"
    ));
}

/// Withholding reaching the target needs no payments; it counts in equal
/// parts unless the filer shows the dates.
#[test]
fn estimated_withholding() {
    let h = Household {
        withheld: DOLLAR * 10_800,
        ..tax(DOLLAR * 12_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!res.value().required() && !res.value().exempt());
    assert_eq!(penalty(&h).value().total(), USCents(0));
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.credited_by(), DOLLAR * 2_700 * (i as i64 + 1));
    }
    let h = Household {
        elections: Some(
            estimated::new_elections()
                .withholding_as_withheld()
                .build()
                .unwrap(),
        ),
        ..h
    };
    assert!(penalty(&h).value().total().0 != 0);
}

/// Item C: the federal January installment excused zeroes North Dakota's.
#[test]
fn estimated_federal_fourth_quarter_exception() {
    let h = Household {
        extras: extras(new_estimated_facts().federal_fourth_quarter_excused(true)),
        ..base()
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[3].status(), Status::Excused);
    assert_eq!(s[3].needed_by(), DOLLAR * 5_400);
    assert_eq!(
        penalty(&h).value().total(),
        USCents(21_600 + 18_000 + 12_600)
    );
    for (filed, asked) in [(day(2027, 2, 1), true), (day(2027, 2, 2), false)] {
        let h = Household {
            return_filed: filed,
            ..base()
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(
            because(
                &res.required(),
                PositionKind::JurisdictionFact,
                "nothing is due on your North Dakota January payment"
            ),
            asked,
            "filed {filed}"
        );
    }
}

/// Item D: a nonresident or new resident whose North Dakota income starts
/// after April 15 pays thirds, halves or all in January.
#[test]
fn estimated_part_year_exception() {
    let nonresident = vec![residence("MN", day(2026, 1, 1), day(2026, 12, 31))];
    let newcomer = vec![
        residence("MN", day(2026, 1, 1), day(2026, 4, 30)),
        residence("ND", day(2026, 5, 1), day(2026, 12, 31)),
    ];
    for (first, want) in [
        (day(2026, 4, 15), [1_800, 3_600, 5_400, 7_200]),
        (day(2026, 4, 16), [0, 2_400, 4_800, 7_200]),
        (day(2026, 5, 31), [0, 2_400, 4_800, 7_200]),
        (day(2026, 6, 1), [0, 0, 3_600, 7_200]),
        (day(2026, 8, 31), [0, 0, 3_600, 7_200]),
        (day(2026, 9, 1), [0, 0, 0, 7_200]),
    ] {
        for (name, res) in [("nonresident", &nonresident), ("moved in", &newcomer)] {
            let h = Household {
                residence: res.clone(),
                extras: extras(new_estimated_facts().first_north_dakota_income(first)),
                ..base()
            };
            let s = rules().schedule(&h.facts()).unwrap().value();
            for (i, w) in want.into_iter().enumerate() {
                assert_eq!(
                    s[i].needed_by(),
                    DOLLAR * w,
                    "{name}, first income {first}: installment {}",
                    i + 1
                );
            }
        }
    }
    let h = Household {
        residence: nonresident,
        ..base()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(because(
        &res.required(),
        PositionKind::RequirementFirstMet,
        "first receive North Dakota income"
    ));
    let h = Household {
        residence: vec![
            residence("ND", day(2026, 1, 1), day(2026, 6, 30)),
            residence("MN", day(2026, 7, 1), day(2026, 12, 31)),
        ],
        extras: extras(new_estimated_facts().first_north_dakota_income(day(2026, 9, 1))),
        ..base()
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(
        s[0].needed_by(),
        DOLLAR * 1_800,
        "moved out: Item D applied"
    );
    let h = Household {
        extras: extras(new_estimated_facts().first_north_dakota_income(day(2026, 9, 1))),
        ..base()
    };
    let res = rules().schedule(&h.facts()).unwrap();
    assert_eq!(res.value()[0].needed_by(), DOLLAR * 1_800);
    assert!(because(
        &res.required(),
        PositionKind::Residency,
        "fewer, larger installments"
    ));
}

/// The annualized method only if it was used federally: 22.5%, 45%, 67.5%
/// and 90%, with recapture.
#[test]
fn estimated_annualized_method() {
    let mut h = Household {
        annualised: vec![
            DOLLAR * 2_000,
            DOLLAR * 4_000,
            DOLLAR * 8_000,
            DOLLAR * 8_000,
        ],
        ..base()
    };
    let res = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [450, 1_800, 5_400, 7_200].into_iter().enumerate() {
        assert_eq!(
            res.value()[i].needed_by(),
            DOLLAR * want,
            "installment {}",
            i + 1
        );
    }
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "Form 2210"
    ));
    h.extras = extras(new_estimated_facts().annualised_federally(false));
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].needed_by(), DOLLAR * 1_800);
    h.extras = extras(new_estimated_facts().annualised_federally(true));
    let res = rules().schedule(&h.facts()).unwrap();
    assert_eq!(res.value()[0].needed_by(), DOLLAR * 450);
    assert!(!because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "Form 2210"
    ));
}

/// Without this year's tax, the target is last year's alone and the interest
/// is the most it can be.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: Some(prior(DOLLAR * 6_000)),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required());
    assert_eq!(r.target(), DOLLAR * 6_000);
    assert_eq!(r.options()[0].why_not(), estimated::NEEDS_THIS_YEAR_TAX);
    assert!(because(&res.required(), PositionKind::ThisYearTax, ""));
    let p = penalty(&h);
    assert!(p.value().upper_bound() && p.value().total().0 != 0);
    let none = Household {
        no_tax: true,
        no_prior: true,
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

/// The waivers North Dakota has, and the $5.00 rule.
#[test]
fn estimated_waivers_and_small_interest() {
    let res = penalty(&base());
    assert!(because(
        &res.required(),
        PositionKind::PenaltyWaiver,
        "good cause"
    ));
    let h = Household {
        waiver: Some(WaiverKind::Casualty),
        ..base()
    };
    let res = penalty(&h);
    assert!(applied(&res).contains("IRS disaster extension"));
    assert_eq!(res.value().total(), USCents(57_600));
    assert_eq!(res.value().waiver(), WaiverKind::Casualty);
    // January's $1,800 fifteen days late is $9.00; five days late is $3.60,
    // under $5.00.
    let res = penalty(&on_time_but_january(day(2027, 1, 30)));
    assert_eq!(res.value().total(), USCents(900));
    assert!(!applied(&res).contains("under $5.00 need not be sent"));
    let res = penalty(&on_time_but_january(day(2027, 1, 20)));
    assert_eq!(res.value().total(), USCents(360));
    assert!(applied(&res).contains("under $5.00 need not be sent"));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&base().facts()).unwrap();
    let want = [
        (
            "A filer who moved into North Dakota last year",
            "income before the move",
            "§ 57-38-62(1)(b)(1)",
        ),
        (
            "The federal fourth-quarter exception",
            "enters zero for the fourth quarter",
            "§§ 57-38-62(1), 57-38-63",
        ),
        (
            "A nonresident or new resident with no North Dakota income until after April 15",
            "June 1 to 15 or September 1 to 15",
            "§ 57-38-63",
        ),
        (
            "Which shortfall a payment pays",
            "goes first to that quarter",
            "§ 57-38-62(3)",
        ),
        (
            "How the days are counted",
            "rounded to three decimal places",
            "§ 57-38-62(3)",
        ),
    ];
    let ds = res.divergences();
    assert_eq!(ds.len(), want.len());
    for d in &ds {
        // A full-year resident with no federal exception and no late start:
        // only the rounding and the payment order change these figures.
        if d.rule() != "How the days are counted" && d.rule() != "Which shortfall a payment pays" {
            assert!(!d.affects_household(), "{:?}", d.rule());
        }
        let Some((_, form, statute)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(d.form().contains(form) && d.statute_source().contains(statute));
        assert!(!d.form_source().is_empty() && !d.statute().is_empty());
    }
    let h = Household {
        extras: extras(new_estimated_facts().federal_fourth_quarter_excused(true)),
        ..base()
    };
    for d in rules().requirement(&h.facts()).unwrap().divergences() {
        if d.rule() == "The federal fourth-quarter exception" {
            assert!(d.affects_household());
            assert_eq!(d.uses(), Reading::Form);
        }
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    let err = estimated_tax(2025).err().unwrap();
    assert!(err.is(&estimated::Error::NoRules));
    let farmer = Household {
        farm_share: Rate(7000),
        ..tax(DOLLAR * 8_000)
    };
    let err = rules().requirement(&farmer.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::Unsupported), "{err}");
    let this = estimated::new_figures()
        .tax(USCents(100_000))
        .build()
        .unwrap();
    let base_f = |j: estimated::Jurisdiction, y: i64| {
        let mut b = estimated::new_facts(j, y);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    for (name, f) in [
        (
            "California's facts",
            base_f(estimated::CALIFORNIA, 2026).build().unwrap(),
        ),
        ("2027", base_f(ND, 2027).build().unwrap()),
        ("other extras", base_f(ND, 2026).state(7).build().unwrap()),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(rules().penalty(&base().facts(), Date::default()).is_err());
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
        "$1,000",
        "12%",
        "110%",
        "federal estimated tax",
        "three decimal places",
        "its own quarter",
        "April 15",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
}

/// $3,600 withheld on $3,000 of tax leaves a refund, which reads as one and
/// never as a negative amount.
#[test]
fn estimated_withholding_over_the_tax_reads_as_a_refund() {
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
    let f = estimated::new_facts(ND, 2026)
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
        texts.push(o.label());
        texts.push(o.why_not());
    }
    for q in res.required() {
        texts.push(q.because());
        texts.push(q.question());
        texts.push(q.effect());
    }
    for d in res.divergences() {
        texts.push(d.rule());
        texts.push(d.form());
        texts.push(d.statute());
    }
    for s in texts {
        assert!(
            !s.contains("−$") && !s.contains("-$"),
            "a negative amount where a refund belongs: {s:?}"
        );
    }
}

/// North Dakota has no casualty, retirement or disability waiver; every
/// request is undecided.
#[test]
fn estimated_waiver_outcomes() {
    let before = penalty(&base()).value().total();
    assert!(before.0 != 0);
    let born = day(1962, 3, 1);
    let retired = |on: Date| {
        estimated::new_positions()
            .waiver_for(WaiverCause::Retirement, on)
            .born(born)
            .build()
            .unwrap()
    };
    for (name, phrase, pos) in [
        (
            "retired in 2026 at 64",
            "no waiver for retirement or disability",
            retired(day(2026, 6, 1)),
        ),
        (
            "retired in 2020",
            "good cause shown",
            retired(day(2020, 6, 1)),
        ),
        (
            "disaster",
            "follows IRS disaster extensions",
            estimated::new_positions()
                .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
                .build()
                .unwrap(),
        ),
    ] {
        let h = Household {
            positions: Some(pos),
            ..base()
        };
        let res = penalty(&h);
        let p = res.value();
        let o = p.waiver_outcome().unwrap();
        assert!(o.undecided(), "{name}");
        assert!(o.reason().contains(phrase), "{name}: {:?}", o.reason());
        assert_eq!(p.total(), before, "{name}");
        assert_eq!(p.if_waived(), before, "{name}");
        assert!(
            because(&res.required(), PositionKind::UnsettledRule, "good cause"),
            "{name}"
        );
    }
}
