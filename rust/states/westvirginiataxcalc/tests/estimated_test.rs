//! West Virginia's estimated tax rules for 2026, checked against the Tax
//! Division's documents and the West Virginia Code.
//!
//! West Virginia publishes no exhaustive oracle and no worked example with
//! amounts. The strongest checks are IT-210's own: its chart of the days and
//! blended daily rates for each column of the 2025 form, which spans the
//! real change from 12.25% to 11.5%, and its example of a June payment that
//! first pays April's underpayment. The exceptions, the boundaries and the
//! penalty amounts are hand-written cases. (The Go tests that only compare
//! figures against the extracts in testdata/ check the Go package's
//! documents, not code, and are not ported.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, Payment, Positions, PriorReturn,
    Rounding, Status, WaiverCause, WaiverKind, WaiverOutcome, Weekday,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_westvirginiataxcalc as westvirginiataxcalc;
use westvirginiataxcalc::EstimatedFacts;
use working::{PositionKind, Reading, Required, Rule};

const WV: Jurisdiction = Jurisdiction::from_static("WV");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    westvirginiataxcalc::estimated_tax(2026).unwrap()
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

/// One payment of each amount on each due date.
fn quarterly(a: &[i64]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .map(|(i, x)| pay(dues()[i], DOLLAR * *x))
        .collect()
}

/// A tax of $4,444.44 makes 90% $4,000.00, so $1,000 a quarter.
const THOUSAND_A_QUARTER: USCents = USCents(444_444);

#[derive(Clone, Default)]
struct Household {
    /// IT-210 Part I line 3.
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    withheld_on: Date,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    /// IT-210 Part II line 10 for each period.
    annualised: Option<Vec<USCents>>,
    as_withheld: bool,
    waiver: Option<WaiverKind>,
    /// Replaces waiver when set.
    positions: Option<Positions>,
    farm_share: Rate,
    fish_share: Rate,
}

fn taxed(tax: USCents) -> Household {
    Household {
        tax,
        ..Household::default()
    }
}

/// `THOUSAND_A_QUARTER` of tax against a big prior year.
fn thousand() -> Household {
    Household {
        prior: big(),
        ..taxed(THOUSAND_A_QUARTER)
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let mut figures = estimated::new_figures().tax(self.tax).build().unwrap();
        if self.no_tax {
            figures = estimated::new_figures().build().unwrap();
        }
        let mut b = estimated::new_facts(WV, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(figures)
            .paid(&self.payments)
            .requirement_first_met(self.first_met)
            .return_filed(self.return_filed)
            .farm_share(self.farm_share)
            .fish_share(self.fish_share);
        if self.withheld.0 > 0 {
            let mut on = self.withheld_on;
            if on.is_zero() {
                on = day(2026, 12, 15);
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
        let mut eb = estimated::new_elections();
        if let Some(ann) = &self.annualised {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            for (i, tax) in ann.iter().enumerate() {
                b.periods(&[estimated::new_period()
                    .end(ends[i])
                    .annualised_tax(*tax)
                    .build()
                    .unwrap()]);
            }
            eb.annualised();
        }
        if self.as_withheld {
            eb.withholding_as_withheld();
        }
        b.elections(eb.build().unwrap());
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed return covering `months` months with its tax.
fn prior(tax: USCents, months: i64) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .months(months)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .build()
            .unwrap(),
    )
}

fn not_filed() -> Option<PriorReturn> {
    Some(estimated::new_prior_return().filed(false).build().unwrap())
}

fn not_required() -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .not_required_to_file()
            .build()
            .unwrap(),
    )
}

fn us_resident(yes: bool) -> Option<EstimatedFacts> {
    Some(
        westvirginiataxcalc::new_estimated_facts()
            .prior_year_us_citizen_or_resident(yes)
            .build()
            .unwrap(),
    )
}

/// A prior-year tax large enough that 90% of this year's is the target.
fn big() -> Option<PriorReturn> {
    prior(DOLLAR * 1_000_000, 12)
}

/// The § 2-2-1(a) holidays for a year, moved under § 2-2-1(b), worked out
/// here from the statute's own rules rather than copied from the package.
fn wv_holidays(year: i64) -> Vec<Date> {
    let nth = |m: i64, wd: Weekday, n: i64| {
        let mut d = day(year, m, 1);
        while d.weekday() != wd {
            d = d.add_days(1);
        }
        d.add_days(7 * (n - 1))
    };
    let last_monday = |m: i64| {
        let mut d = day(year, m + 1, 1).add_days(-1);
        while d.weekday() != Weekday::MONDAY {
            d = d.add_days(-1);
        }
        d
    };
    let observed = |d: Date| match d.weekday() {
        Weekday::SATURDAY => d.add_days(-1),
        Weekday::SUNDAY => d.add_days(1),
        _ => d,
    };
    let thanksgiving = nth(11, Weekday::THURSDAY, 4);
    vec![
        observed(day(year, 1, 1)),
        nth(1, Weekday::MONDAY, 3),
        nth(2, Weekday::MONDAY, 3),
        last_monday(5),
        observed(day(year, 6, 20)),
        observed(day(year, 7, 4)),
        nth(9, Weekday::MONDAY, 1),
        nth(10, Weekday::MONDAY, 2),
        observed(day(year, 11, 11)),
        thanksgiving,
        thanksgiving.add_days(1),
        observed(day(year, 12, 25)),
    ]
}

/// The 2026 dates are the ones the IT-140ESI prints. None falls on a weekend
/// or a § 2-2-1 holiday, so none moves.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let mut holidays = wv_holidays(2026);
    holidays.extend(wv_holidays(2027));
    assert_eq!(holidays[4], day(2026, 6, 19));
    assert_eq!(holidays[5], day(2026, 7, 3));
    let res = rules().schedule(&taxed(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
        assert!(!holidays.contains(&inst.due()), "installment {}", i + 1);
    }
}

/// § 11-10-18a(i) excuses the January installment for a return filed and
/// paid by the last day of January. January 31, 2027 is a Sunday, and
/// § 2-2-1(e) moves it to Monday, February 1. The penalty stops on the day
/// the return is paid.
#[test]
fn estimated_return_filed_by_january_thirty_first_moves() {
    assert_eq!(day(2027, 1, 31).weekday(), Weekday::SUNDAY);
    for (filed, excused) in [
        (day(2027, 1, 29), true),
        (day(2027, 2, 1), true),
        (day(2027, 2, 2), false),
    ] {
        let h = Household {
            payments: quarterly(&[1_000, 1_000, 1_000]),
            return_filed: filed,
            ..thousand()
        };
        let s = rules().schedule(&h.facts()).unwrap().value();
        // Excused, January needs nothing more than September, so the $3,000
        // paid meets it; otherwise it is $1,000 short.
        let is_excused = s[3].needed_by() == s[2].needed_by()
            && s[3].status() == Status::Met
            && s[3].shortfall().0 == 0;
        let short = s[3].needed_by() == DOLLAR * 4_000
            && s[3].status() == Status::Short
            && s[3].shortfall() == DOLLAR * 1_000;
        assert!(is_excused == excused && short != excused, "filed {filed}");
        let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        assert_eq!(p.value().as_of(), filed);
        // Filed February 2: January's $1,000 is short for 18 days, all of
        // 2027, which has no published rate.
        assert_eq!(p.value().lines().len() == 1, !excused, "filed {filed}");
    }
}

/// IT-210's chart for 2025 is the nearest real change of rate: 12.25%
/// (0.000336 a day) for 2025 and 11.5% (0.000315) for 2026. Splitting each
/// column at December 31 and charging each year at its own factor gives the
/// chart's blended rates. This checks the method the package uses, with the
/// shared helpers, against the form.
#[test]
fn estimated_rate_change_matches_the_blended_chart() {
    let series = estimated::new_factor_series(Rounding::RoundToCent)
        .period(
            day(2025, 1, 1),
            day(2025, 12, 31),
            estimated::parse_factor("0.000336").unwrap(),
            Rate(1225),
            "IT-210 (2025)",
        )
        .period(
            day(2026, 1, 1),
            day(2026, 12, 31),
            estimated::parse_factor("0.000315").unwrap(),
            Rate(1150),
            "IT-210 (2025)",
        )
        .build()
        .unwrap();
    let weekends = estimated::new_calendar("weekends").build().unwrap();
    let moved = estimated::move_dates(
        &weekends,
        &[
            day(2025, 4, 15),
            day(2025, 6, 15),
            day(2025, 9, 15),
            day(2026, 1, 15),
        ],
    );
    assert_eq!(moved[1], day(2025, 6, 16));
    let want_days = [365, 303, 212, 90];
    let want_rate = ["0.000330", "0.000329", "0.000326", "0.000315"];
    let principal = DOLLAR * 1_000_000;
    for (i, due) in moved.iter().enumerate() {
        let (parts, gap) =
            estimated::daily_factor(principal, *due, day(2026, 4, 15), &series).unwrap();
        assert!(gap.is_zero(), "column {}", i + 1);
        let days: i64 = parts.iter().map(|p| p.days()).sum();
        let charge: i64 = parts.iter().map(|p| p.charge().0).sum();
        assert_eq!(days, want_days[i], "column {}", i + 1);
        // The blended daily rate, to six places: charge ÷ principal ÷ days.
        let micro = (charge * 1_000_000 / days * 10 / principal.0 + 5) / 10;
        assert_eq!(format!("0.{micro:06}"), want_rate[i], "column {}", i + 1);
    }
}

/// $600 or more of tax after withholding brings the penalty in; $599.99
/// does not. The IT-140ESI's 10% test excuses payments when withholding
/// leaves less than 10% of the tax.
#[test]
fn estimated_threshold_boundaries() {
    for (withheld, exempt) in [(940_001, true), (940_000, false)] {
        let res = rules()
            .requirement(
                &Household {
                    withheld: USCents(withheld),
                    prior: big(),
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(res.value().exempt(), exempt, "withheld {withheld}");
        let c = res.value().chosen().unwrap_or_default();
        let is_exception = c.basis() == Basis::Exception
            && c.amount().0 == 0
            && c.label() == "Tax less withholding under $600";
        assert_eq!(is_exception, exempt, "withheld {withheld}");
        assert!(!res.value().why().contains("$600, which is under $600"));
    }
    for (withheld, required, small_share) in [
        (899_999, true, false),
        (900_000, false, false),
        (900_001, false, true),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    withheld: USCents(withheld),
                    prior: big(),
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap();
        let r = res.value();
        assert!(
            r.required() == required && r.why().contains("less than 10%") == small_share,
            "withheld {withheld}: required {}, why {:?}",
            r.required(),
            r.why()
        );
    }
    let p = rules()
        .penalty(
            &Household {
                withheld: USCents(940_001),
                prior: big(),
                ..taxed(DOLLAR * 10_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert!(p.value().total().0 == 0 && p.value().lines().is_empty());
}

/// The target is the smaller of 90% of this year's tax and 100% of last
/// year's, for a 12-month return.
#[test]
fn estimated_target_options() {
    for (name, p, target, chosen, ask) in [
        (
            "last year lower",
            prior(DOLLAR * 8_000, 12),
            DOLLAR * 8_000,
            Basis::PriorYearTax,
            PositionKind::Unspecified,
        ),
        (
            "this year lower",
            prior(DOLLAR * 9_500, 12),
            DOLLAR * 9_000,
            Basis::ThisYearTax,
            PositionKind::Unspecified,
        ),
        (
            "an 11-month return",
            prior(DOLLAR * 8_000, 11),
            DOLLAR * 9_000,
            Basis::ThisYearTax,
            PositionKind::Unspecified,
        ),
        (
            "no return filed",
            not_filed(),
            DOLLAR * 9_000,
            Basis::ThisYearTax,
            PositionKind::Unspecified,
        ),
        (
            "no return on record",
            None,
            DOLLAR * 9_000,
            Basis::ThisYearTax,
            PositionKind::PriorYearReturn,
        ),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: p,
                    ..taxed(DOLLAR * 10_000)
                }
                .facts(),
            )
            .unwrap();
        let r = res.value();
        let c = r.chosen().unwrap();
        assert!(
            r.target() == target && c.basis() == chosen && r.required(),
            "{name}"
        );
        assert_eq!(r.options()[1].percent(), percent(100), "{name}");
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}: {ask} not asked");
        }
    }
}

/// IT-210 lowers each installment to a quarter of the prior-year figure.
/// Paying $1,900 in April against a $2,000 prior-year quarter is charged, by
/// the form, on $100.
#[test]
fn estimated_prior_year_test_lowers_the_installment() {
    let h = Household {
        prior: prior(DOLLAR * 8_000, 12),
        payments: quarterly(&[1_900, 2_100, 2_000, 2_000]),
        ..taxed(DOLLAR * 10_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert!(
        s[0].shortfall() == DOLLAR * 100
            && s[0].status() == Status::Short
            && s[1].status() == Status::Met
    );
    // $100 for the 61 days to June 15 at 0.000315 is $1.92, so $2.
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), DOLLAR * 2);
}

/// IT-210 line 8: a zero tax last year makes the target zero while this
/// year's tax is not more than $5,000. For a filer who was not required to
/// file, the booklet's exception and § 11-10-18a(e)(2) need this year's tax
/// under $5,000 and a year as a United States citizen or resident.
#[test]
fn estimated_no_liability_last_year() {
    for (name, tax, p, extras, exempt, ask) in [
        (
            "filed, zero, $5,000.00",
            500_000,
            prior(USCents(0), 12),
            None,
            true,
            false,
        ),
        (
            "filed, zero, $5,000.01",
            500_001,
            prior(USCents(0), 12),
            None,
            false,
            false,
        ),
        (
            "filed, zero, 11 months",
            400_000,
            prior(USCents(0), 11),
            None,
            false,
            false,
        ),
        (
            "not required, $4,999.99",
            499_999,
            not_required(),
            us_resident(true),
            true,
            false,
        ),
        (
            "not required, $5,000.00",
            500_000,
            not_required(),
            us_resident(true),
            false,
            false,
        ),
        (
            "not required, not a US resident",
            400_000,
            not_required(),
            us_resident(false),
            false,
            false,
        ),
        (
            "not required, residence not given",
            400_000,
            not_required(),
            None,
            false,
            true,
        ),
    ] {
        let res = rules()
            .requirement(
                &Household {
                    prior: p,
                    extras,
                    ..taxed(USCents(tax))
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "{name}: {}",
            res.value().why()
        );
        assert_eq!(
            has(&res.required(), PositionKind::JurisdictionFact),
            ask,
            "{name}"
        );
        if !exempt {
            assert_eq!(res.value().target(), percent(90).of(USCents(tax)), "{name}");
        }
    }
}

/// Shortfalls unpaid to December 31, 2026, charged at 0.000315 a day and
/// rounded to the dollar: $82, $63 and $34. January's has no 2026 days.
#[test]
fn estimated_penalty_arithmetic() {
    let res = rules()
        .penalty(&thousand().facts(), day(2026, 12, 31))
        .unwrap();
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    for (i, want) in [82, 63, 34, 0].into_iter().enumerate() {
        assert!(
            lines[i].shortfall() == DOLLAR * 1_000 && lines[i].total() == DOLLAR * want,
            "line {}: {} on {}",
            i + 1,
            lines[i].total(),
            lines[i].shortfall()
        );
    }
    assert_eq!(res.value().total(), DOLLAR * 179);
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
}

/// Each column runs to April 15, 2027, as IT-210 line 10 counts: 365, 304,
/// 212 and 90 days. The days after December 31, 2026 have no published
/// rate, so each line's charge stops there and the gap is reported.
#[test]
fn estimated_penalty_across_the_rate_period_boundary() {
    let res = rules()
        .penalty(&thousand().facts(), day(2027, 6, 30))
        .unwrap();
    assert_eq!(res.value().as_of(), day(2027, 4, 15));
    let lines = res.value().lines();
    for (i, want) in [365, 304, 212, 90].into_iter().enumerate() {
        assert_eq!(lines[i].days(), want, "line {}", i + 1);
        for p in lines[i].parts() {
            assert!(!p.to().after(day(2026, 12, 31)), "line {}", i + 1);
        }
    }
    assert_eq!(res.value().total(), DOLLAR * 179);
    assert!(has(&res.required(), PositionKind::UnpublishedRate));
    for r in res.required() {
        if r.position() == PositionKind::UnpublishedRate {
            assert!(
                r.question().is_empty() && r.effect().contains("rate for the days after Dec 31 "),
                "question {:?}, effect {:?}",
                r.question(),
                r.effect()
            );
        }
    }
}

/// IT-210's example: a June 15 payment first pays April's underpayment, and
/// what is left goes to June. A part payment splits the charge at the day it
/// was paid.
#[test]
fn estimated_payments_pay_the_oldest_underpayment_first() {
    let mut h = Household {
        payments: quarterly(&[0, 2_000, 1_000, 1_000]),
        ..thousand()
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert!(s[0].status() == Status::Short && s[1].status() == Status::Met);
    // April's $1,000 for 61 days: $19.22, so $19.
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total(), DOLLAR * 19);

    // $400 on May 15 and the rest of April on June 15: $4 and $12.
    let d = dues();
    h.payments = vec![
        pay(day(2026, 5, 15), DOLLAR * 400),
        pay(d[1], DOLLAR * 1_600),
        pay(d[2], DOLLAR * 1_000),
        pay(d[3], DOLLAR * 1_000),
    ];
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let parts = p.value().lines()[0].parts();
    assert!(
        parts.len() == 2
            && parts[0].days() == 30
            && parts[1].days() == 61
            && p.value().total() == DOLLAR * 16
    );
}

/// IT-210 prints 0.000315 a day, not 11.5% ÷ 365. On a $100,000 shortfall
/// for 260 days the two differ by $2: $8,190 printed, $8,191.78 from the
/// rate.
#[test]
fn estimated_interest_uses_the_printed_daily_factor() {
    // 90% of $444,444.44 is $400,000.00, a quarter of which is $100,000.
    let f = Household {
        prior: big(),
        ..taxed(USCents(44_444_444))
    }
    .facts();
    let parts = rules()
        .penalty(&f, day(2026, 12, 31))
        .unwrap()
        .value()
        .lines()[0]
        .parts();
    assert!(
        parts.len() == 1
            && parts[0].days() == 260
            && parts[0].charge() == DOLLAR * 8_190
            && parts[0].rate() == Rate(1150)
    );
}

/// Withholding counts a quarter on each date unless the filer shows the
/// dates it was withheld. $9,000 of a $10,000 tax, all withheld on December
/// 15, leaves April, June and September $2,250 short until then: $173, $130
/// and $64.
#[test]
fn estimated_withholding_on_the_dates_withheld() {
    let mut h = Household {
        prior: big(),
        withheld: DOLLAR * 9_000,
        ..taxed(DOLLAR * 10_000)
    };
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total().0, 0, "spread evenly");
    h.as_withheld = true;
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(p.value().total() == DOLLAR * 367 && p.value().lines().len() == 3);
}

/// IT-210 Part II: 22.5%, 45%, 67.5% and 90% of each period's annualised
/// tax, less what earlier dates required, against the regular quarter plus
/// what was carried. The four installments add to the regular total.
#[test]
fn estimated_annualised_recapture() {
    let mut h = Household {
        annualised: Some(vec![
            DOLLAR * 400,
            DOLLAR * 2_000,
            DOLLAR * 3_000,
            THOUSAND_A_QUARTER,
        ]),
        payments: quarterly(&[90, 810, 1_125, 1_975]),
        ..thousand()
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [90, 900, 2_025, 4_000].into_iter().enumerate() {
        assert!(
            s[i].needed_by() == DOLLAR * want && s[i].status() == Status::Met,
            "installment {} needs {} ({})",
            i + 1,
            s[i].needed_by(),
            s[i].status()
        );
    }
    h.annualised = None;
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_ne!(p.value().total().0, 0);
}

/// Last year's overpayment applied forward counts on April 15.
#[test]
fn estimated_prior_year_overpayment() {
    let carried = estimated::new_payment()
        .amount(DOLLAR * 1_000)
        .from_prior_year()
        .build()
        .unwrap();
    let mut payments = vec![carried];
    payments.extend(quarterly(&[0, 1_000, 1_000, 1_000]));
    let h = Household {
        payments,
        ..thousand()
    };
    let s = rules().schedule(&h.facts()).unwrap();
    assert!(s.value()[0].status() == Status::Met && s.value()[0].credited_by() == DOLLAR * 1_000);
    assert!(applied(&s.applied(), "overpayment"));
}

/// A requirement first met after April 1 still owes the quarters IT-210
/// prints, and the working says so.
#[test]
fn estimated_late_start_uses_the_quarters() {
    for (met, note) in [
        (day(2026, 4, 1), false),
        (day(2026, 4, 2), true),
        (day(2026, 9, 2), true),
    ] {
        let res = rules()
            .schedule(
                &Household {
                    first_met: met,
                    ..thousand()
                }
                .facts(),
            )
            .unwrap();
        assert_eq!(
            res.value()[0].needed_by(),
            DOLLAR * 1_000,
            "first met {met}"
        );
        assert_eq!(
            applied(&res.applied(), "first met"),
            note,
            "first met {met}"
        );
    }
}

/// The penalty is shown before any waiver, and the working names the
/// waivers West Virginia has.
#[test]
fn estimated_waivers() {
    for (waiver, says) in [
        (Some(WaiverKind::Casualty), "casualty, disaster"),
        (
            Some(WaiverKind::RetiredOrDisabled),
            "no waiver for retirement",
        ),
        (None, "reasonable cause"),
    ] {
        let res = rules()
            .penalty(
                &Household {
                    waiver,
                    ..thousand()
                }
                .facts(),
                day(2027, 4, 15),
            )
            .unwrap();
        assert!(
            res.value().total() == DOLLAR * 179 && applied(&res.applied(), says),
            "waiver {waiver:?}"
        );
    }
}

/// A waiver case: its name, the positions, the outcome wanted, a phrase of
/// its reason, and whether the open point is asked.
type WaiverCase = (
    &'static str,
    Positions,
    fn(&WaiverOutcome) -> bool,
    &'static str,
    bool,
);

/// § 11-10-18a(e)(3) grants the federal casualty waiver. Retiring after 62,
/// which meets the federal conditions, is no waiver in West Virginia; the
/// booklet's reasonable-cause waiver leaves open whether it counts.
#[test]
fn estimated_waiver_outcomes() {
    let disaster = estimated::new_positions()
        .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
        .build()
        .unwrap();
    let retired = estimated::new_positions()
        .waiver_for(WaiverCause::Retirement, day(2026, 6, 1))
        .born(day(1962, 3, 1))
        .build()
        .unwrap();
    let cases: [WaiverCase; 2] = [
        (
            "a disaster",
            disaster,
            WaiverOutcome::eligible,
            "Tax Commissioner can waive",
            false,
        ),
        (
            "retired in 2026 at 64",
            retired,
            WaiverOutcome::undecided,
            "no waiver for retirement or disability",
            true,
        ),
    ];
    for (name, pos, want, inn, open) in cases {
        let res = rules()
            .penalty(
                &Household {
                    positions: Some(pos),
                    ..thousand()
                }
                .facts(),
                day(2027, 4, 15),
            )
            .unwrap();
        let o = res.value().waiver_outcome().unwrap();
        assert!(
            want(&o) && o.reason().contains(inn),
            "{name}: {:?}",
            o.reason()
        );
        assert_eq!(res.value().total(), DOLLAR * 179, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::UnsettledRule),
            open,
            "{name}"
        );
        if o.eligible() {
            assert_eq!(res.value().if_waived().0, 0, "{name}");
        }
    }
}

/// Under an exception, no question says its answer changes the target.
#[test]
fn estimated_exempt_asks_nothing_about_the_target() {
    let res = rules().requirement(&taxed(DOLLAR * 500).facts()).unwrap();
    assert!(res.value().exempt(), "not exempt under $600");
    for q in res.required() {
        assert!(
            !q.effect().to_lowercase().contains("target"),
            "{:?}",
            q.effect()
        );
    }
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules()
        .requirement(&taxed(DOLLAR * 10_000).facts())
        .unwrap();
    let want = [
        (
            "Who must make estimated payments",
            "under 10%",
            "§ 11-21-55(a)",
        ),
        (
            "The prior-year test",
            "smaller of 90%",
            "§ 11-10-18a(b)(1) and (d)(1)",
        ),
        ("No liability last year", "$5,000 or less", "(e)(2)"),
        (
            "A requirement first met after April 1",
            "every filer",
            "§§ 11-21-55(d), 11-21-56(a)",
        ),
        (
            "A return filed and paid by January 31",
            "February 1",
            "§ 2-2-1(e)",
        ),
        (
            "The daily interest factor",
            "0.000315",
            "§§ 11-10-17a(c), 11-10-18a(a)",
        ),
        (
            "How the rate is set",
            "every six months",
            "§ 11-10-17a(a) and (c)",
        ),
        (
            "Withholding on the dates withheld",
            "One box",
            "§ 11-10-18a(g)",
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some(w) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert!(
            d.form().contains(w.1)
                && d.statute_source().contains(w.2)
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{:?} does not pin both readings",
            d.rule()
        );
    }

    // Which reading each follows, and which these facts reach. $10,000 of
    // tax, nothing withheld or paid, and last year's return not on record:
    // payments are required and every quarter is short, so the answer, the
    // daily factor and nothing else is touched.
    let statute = [
        "A return filed and paid by January 31",
        "How the rate is set",
    ];
    let affects = [
        "Who must make estimated payments",
        "The daily interest factor",
    ];
    for d in res.divergences() {
        let want = if statute.contains(&d.rule().as_str()) {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert!(
            d.uses() == want && d.affects_household() == affects.contains(&d.rule().as_str()),
            "{:?} uses {} and affects {}",
            d.rule(),
            d.uses(),
            d.affects_household()
        );
    }
    // A return filed on Monday, February 1, 2027 is the one the IT-140ESI's
    // January 31 would not excuse; one filed January 29 is excused either
    // way.
    for (filed, want) in [(day(2027, 2, 1), true), (day(2027, 1, 29), false)] {
        let h = Household {
            payments: quarterly(&[1_000, 1_000, 1_000]),
            return_filed: filed,
            ..thousand()
        };
        for d in rules().requirement(&h.facts()).unwrap().divergences() {
            if d.rule() == "A return filed and paid by January 31" {
                assert_eq!(d.affects_household(), want, "filed {filed}");
            }
        }
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for year in [2025, 2027] {
        let Err(err) = westvirginiataxcalc::estimated_tax(year) else {
            panic!("{year}: rules returned");
        };
        assert!(err.is(&estimated::Error::NoRules), "{year}: {err}");
    }
    let Err(err) = rules().requirement(
        &Household {
            farm_share: Rate(6_667),
            ..taxed(USCents(100))
        }
        .facts(),
    ) else {
        panic!("a farmer: accepted");
    };
    assert!(err.is(&estimated::Error::Unsupported), "a farmer: {err}");
    // West Virginia's farmer rule names farmers only.
    rules()
        .requirement(
            &Household {
                fish_share: Rate(7_000),
                ..taxed(USCents(100))
            }
            .facts(),
        )
        .unwrap();
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let mut periods = vec![
        estimated::new_period()
            .end(day(2026, 4, 30))
            .build()
            .unwrap(),
    ];
    for e in [day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)] {
        periods.push(estimated::new_period().end(e).build().unwrap());
    }
    let late = estimated::new_facts(WV, 2026)
        .filing_status(filing::Status::Single)
        .this_year(this)
        .periods(&periods)
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    for (name, f) in [
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
            estimated::new_facts(WV, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(WV, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("not West Virginia's")
                .build()
                .unwrap(),
        ),
        ("a period ending late", late),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&taxed(USCents(100)).facts(), Date::default())
            .is_err()
    );
}

#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    let all = format!(
        "{}{}{}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$600",
        "$5,000",
        "11.5%",
        "0.000315",
        "no 110%",
        "February 1, 2027",
        "prime rate plus three points",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.sources().is_empty() && !e.versus_federal().is_empty());
}

/// Without this year's tax the target is 100% of last year's, the penalty is
/// an upper bound, and a zero last year cannot be used because it depends on
/// this year's tax being $5,000 or less.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 4_000, 12),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 4_000, "{}", r.why());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [1_000, 2_000, 3_000, 4_000].into_iter().enumerate() {
        assert_eq!(sched.value()[i].needed_by(), DOLLAR * want);
    }
    let pen = rules().penalty(&h.facts(), day(2026, 12, 31)).unwrap();
    assert!(pen.value().upper_bound() && pen.value().total() == DOLLAR * 179);

    for (name, p) in [
        ("no return on record", None),
        ("a zero last year", prior(USCents(0), 12)),
        ("not required to file", not_required()),
    ] {
        let h = Household {
            no_tax: true,
            prior: p,
            extras: us_resident(true),
            ..Household::default()
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(
            !res.value().target_known() && !res.value().exempt(),
            "{name}"
        );
        let Err(err) = rules().schedule(&h.facts()) else {
            panic!("{name}: schedule accepted");
        };
        assert!(err.is(&estimated::Error::NeedsThisYearTax), "{name}: {err}");
        let Err(err) = rules().penalty(&h.facts(), day(2027, 4, 15)) else {
            panic!("{name}: penalty accepted");
        };
        assert!(err.is(&estimated::Error::NeedsThisYearTax), "{name}: {err}");
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
    let f = estimated::new_facts(WV, 2026)
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
        assert!(!s.contains("−$") && !s.contains("-$"), "{s:?}");
    }
}
