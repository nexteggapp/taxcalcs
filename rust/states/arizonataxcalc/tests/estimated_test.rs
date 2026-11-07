//! Arizona's estimated tax rules for 2026, checked against the Department
//! of Revenue's forms and the Arizona Revised Statutes.
//!
//! Go's TestEstimatedFiguresAreInTheirSources and the testdata/ phrase check
//! in the waiver test are not ported.

#![allow(clippy::unwrap_used)]

use std::collections::HashMap;

use estimated::{
    Basis, Date, EstimatedTax, Facts, Month, Payment, Positions, PriorReturn, Status, WaiverCause,
    WaiverKind, Weekday,
};
use filing::Status as FilingStatus;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_arizonataxcalc as arizonataxcalc;
use working::{PositionKind, Reading, Required, Rule};

use arizonataxcalc::EstimatedFacts;

const AZ: estimated::Jurisdiction = estimated::ARIZONA;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    arizonataxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

fn because(rs: &[Required], k: PositionKind) -> String {
    rs.iter()
        .filter(|r| r.position() == k)
        .map(|r| format!("{} {}", r.question(), r.effect()))
        .collect::<Vec<_>>()
        .join(" | ")
}

fn applied(rs: &[Rule]) -> String {
    rs.iter()
        .map(|r| r.applied())
        .collect::<Vec<_>>()
        .join(" | ")
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

/// Extras with every Arizona fact answered so that no exception applies.
fn answered() -> EstimatedFacts {
    arizonataxcalc::new_estimated_facts()
        .no_federal_liability_last_year(false)
        .federal_tax_less_withholding(DOLLAR * 50_000)
        .prior_year_return_required(true)
        .build()
        .unwrap()
}

#[derive(Clone, Default)]
struct Household {
    status: FilingStatus,
    /// After credits, Form 221 line 4.
    tax: USCents,
    no_tax: bool,
    /// This year's Arizona gross income; $200,000 if zero.
    gross: USCents,
    no_gross: bool,
    /// This year's federal AGI, given only when set.
    agi: USCents,
    withheld: USCents,
    as_withheld: bool,
    /// A filed return with $1,000,000 of tax if `None`.
    prior: Option<PriorReturn>,
    no_prior: bool,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    return_filed: Date,
    /// Each period's annualised tax, Form 221 worksheet line 14.
    annualised: Option<Vec<USCents>>,
    waiver: bool,
    /// A waiver asked for, with its cause and dates.
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
            let g = if self.gross.0 == 0 {
                DOLLAR * 200_000
            } else {
                self.gross
            };
            fb.gross_income(g);
        }
        if self.agi.0 != 0 {
            fb.federal_agi(self.agi);
        }
        let mut b = estimated::new_facts(AZ, 2026);
        b.filing_status(status)
            .this_year(fb.build().unwrap())
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
        } else if !self.no_prior {
            b.prior_year(prior(DOLLAR * 1_000_000, DOLLAR * 200_000));
        }
        b.state(self.extras.unwrap_or_else(answered));
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
        }
        let mut eb = estimated::new_elections();
        if self.annualised.is_some() {
            eb.annualised();
        }
        if self.as_withheld {
            eb.withholding_as_withheld();
        }
        b.elections(eb.build().unwrap());
        if self.waiver {
            b.positions(
                estimated::new_positions()
                    .waiver(WaiverKind::None)
                    .build()
                    .unwrap(),
            );
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

/// A filed 2025 return with its tax after credits and its Arizona gross
/// income.
fn prior(tax: USCents, gross: USCents) -> PriorReturn {
    let f = estimated::new_figures()
        .tax(tax)
        .gross_income(gross)
        .build()
        .unwrap();
    estimated::new_prior_return()
        .filed(true)
        .figures(f)
        .full_year_resident(true)
        .build()
        .unwrap()
}

/// The computed dates are the ones the 2026 Form 140ES prints. None of them
/// falls on a weekend or a holiday, so none moves. The file-and-pay date
/// does: January 31, 2027 is a Sunday, and the 140ES prints Monday,
/// February 1.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&tax(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    assert!(!applied(&res.applied()).contains("falls on a weekend"));
    assert_eq!(day(2027, 1, 31).weekday(), Weekday::SUNDAY);
    for (filed, excused) in [(day(2027, 2, 1), true), (day(2027, 2, 2), false)] {
        let h = Household {
            return_filed: filed,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().schedule(&h.facts()).unwrap();
        assert_eq!(
            res.value()[3].status() == Status::Excused,
            excused,
            "filed {filed}"
        );
    }
}

/// § 43-581(A): Arizona gross income must exceed $75,000 ($150,000 joint)
/// this year and have been greater than it last year.
#[test]
fn estimated_income_threshold_boundaries() {
    for (status, this, before, exempt) in [
        (FilingStatus::Single, 7_500_000, 20_000_000, true),
        (FilingStatus::Single, 7_500_001, 20_000_000, false),
        (FilingStatus::Single, 20_000_000, 7_500_000, true),
        (FilingStatus::Single, 20_000_000, 7_500_001, false),
        (
            FilingStatus::MarriedFilingJointly,
            15_000_000,
            20_000_000,
            true,
        ),
        (
            FilingStatus::MarriedFilingJointly,
            15_000_001,
            20_000_000,
            false,
        ),
        (
            FilingStatus::MarriedFilingJointly,
            20_000_000,
            15_000_000,
            true,
        ),
        (
            FilingStatus::MarriedFilingJointly,
            20_000_000,
            15_000_001,
            false,
        ),
        (
            FilingStatus::MarriedFilingJointly,
            10_000_000,
            10_000_000,
            true,
        ),
        (
            FilingStatus::MarriedFilingSeparately,
            7_500_000,
            20_000_000,
            true,
        ),
        (
            FilingStatus::MarriedFilingSeparately,
            7_500_001,
            7_500_001,
            false,
        ),
        (FilingStatus::HeadOfHousehold, 7_500_000, 20_000_000, true),
        (FilingStatus::HeadOfHousehold, 7_500_001, 7_500_001, false),
    ] {
        let h = Household {
            status,
            gross: USCents(this),
            prior: Some(prior(DOLLAR * 9_000, USCents(before))),
            ..tax(DOLLAR * 10_000)
        };
        let r = rules().requirement(&h.facts()).unwrap().value();
        assert!(
            r.exempt() == exempt && r.required() != exempt,
            "{status:?} with {this} this year and {before} last: {}",
            r.why()
        );
        if exempt {
            let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
            assert_eq!(p.value().total().0, 0);
        }
    }
}

/// § 43-581(E)(2) and Form 221 line 1.
#[test]
fn estimated_tax_threshold_boundaries() {
    for (w, exempt) in [(900_001, true), (900_000, false)] {
        let h = Household {
            withheld: USCents(w),
            ..tax(DOLLAR * 10_000)
        };
        let r = rules().requirement(&h.facts()).unwrap().value();
        assert_eq!(r.exempt(), exempt, "withheld {w}: {}", r.why());
        if exempt {
            let c = r.chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && c.label() == "Tax after credits less withholding under $1,000"
            );
        }
    }
    for (federal, exempt) in [
        (USCents(99_999), true),
        (USCents(100_000), false),
        (DOLLAR * -5_000, true),
    ] {
        let e = arizonataxcalc::new_estimated_facts()
            .no_federal_liability_last_year(false)
            .federal_tax_less_withholding(federal)
            .prior_year_return_required(true)
            .build()
            .unwrap();
        let h = Household {
            extras: Some(e),
            ..tax(DOLLAR * 10_000)
        };
        let r = rules().requirement(&h.facts()).unwrap().value();
        assert_eq!(r.exempt(), exempt, "federal {federal}: {}", r.why());
        if exempt {
            let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
            assert_eq!(p.value().total().0, 0);
        }
    }
}

/// The target is the smaller of 90% of this year's tax and 100% of last
/// year's, with no 110% tier at any income.
#[test]
fn estimated_target_options() {
    let h = Household {
        prior: Some(prior(DOLLAR * 9_500, DOLLAR * 200_000)),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(r.chosen().unwrap().basis() == Basis::ThisYearTax && r.target() == DOLLAR * 9_000);
    let h = Household {
        gross: DOLLAR * 5_000_000,
        prior: Some(prior(DOLLAR * 8_000, DOLLAR * 5_000_000)),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    let c = r.chosen().unwrap();
    assert!(
        c.basis() == Basis::PriorYearTax
            && c.percent() == percent(100)
            && r.target() == DOLLAR * 8_000
    );
    assert!(
        r.required() && r.why().contains("$8,000 of estimated payments"),
        "{}",
        r.why()
    );
}

/// The prior-year option needs a filed 2025 return that, under the 2026
/// Form 140ES, the filer was required to file.
#[test]
fn estimated_prior_year_option_availability() {
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let voluntary = arizonataxcalc::new_estimated_facts()
        .no_federal_liability_last_year(false)
        .federal_tax_less_withholding(DOLLAR * 50_000)
        .prior_year_return_required(false)
        .build()
        .unwrap();
    for (name, h, ask) in [
        (
            "no return on record",
            Household {
                no_prior: true,
                ..Household::default()
            },
            PositionKind::PriorYearReturn,
        ),
        (
            "not required",
            Household {
                prior: Some(not_required),
                ..Household::default()
            },
            PositionKind::Unspecified,
        ),
        (
            "not filed",
            Household {
                prior: Some(not_filed),
                ..Household::default()
            },
            PositionKind::Unspecified,
        ),
        (
            "filed voluntarily",
            Household {
                extras: Some(voluntary),
                ..Household::default()
            },
            PositionKind::Unspecified,
        ),
    ] {
        let h = Household {
            tax: DOLLAR * 10_000,
            ..h
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let o = &res.value().options()[1];
        assert!(!o.available() && !o.why_not().is_empty(), "{name}");
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}");
        }
    }

    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 4_000)
                .gross_income(DOLLAR * 100_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let h = Household {
        prior: Some(short),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(res.value().options()[1].available() && res.value().target() == DOLLAR * 4_000);
    assert!(because(&res.required(), PositionKind::UnsettledRule).contains("6 months"));

    let unanswered = arizonataxcalc::new_estimated_facts()
        .no_federal_liability_last_year(false)
        .federal_tax_less_withholding(DOLLAR * 50_000)
        .build()
        .unwrap();
    let h = Household {
        prior: Some(prior(DOLLAR * 4_000, DOLLAR * 100_000)),
        extras: Some(unanswered),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(
        because(&res.required(), PositionKind::JurisdictionFact)
            .contains("Were you required to file your 2025 Arizona return?")
    );
    let h = Household {
        extras: Some(unanswered),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(
        !because(&res.required(), PositionKind::JurisdictionFact)
            .contains("Were you required to file your 2025 Arizona return?")
    );
}

/// April's $10,000 shortfall spans the rate change on July 1.
#[test]
fn estimated_penalty_across_the_rate_change() {
    let h = Household {
        payments: vec![
            pay(day(2026, 7, 31), DOLLAR * 10_000),
            pay(dues()[2], DOLLAR * 30_000),
        ],
        ..tax(USCents(4_444_444))
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let lines = res.value().lines();
    assert_eq!(lines.len(), 2);
    let want: [[(i64, Rate, i64); 2]; 2] = [
        [(76, percent(6), 12_493), (31, percent(7), 5_945)],
        [(15, percent(6), 2_466), (77, percent(7), 14_767)],
    ];
    for (i, w) in want.iter().enumerate() {
        let parts = lines[i].parts();
        assert_eq!(parts.len(), w.len(), "line {}", i + 1);
        for (j, p) in parts.iter().enumerate() {
            assert!(
                p.days() == w[j].0
                    && p.rate() == w[j].1
                    && p.charge() == USCents(w[j].2)
                    && p.principal() == DOLLAR * 10_000,
                "line {} part {}: {p:?}",
                i + 1,
                j + 1
            );
        }
    }
    assert_eq!(res.value().total(), USCents(35_671));
    let s = applied(&res.applied());
    for phrase in [
        "to the nearest whole dollar, $357.00",
        "under the cap of 10% of it, $1000.00",
    ] {
        assert!(
            s.contains(phrase),
            "the working does not say {phrase:?}: {s}"
        );
    }
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
}

/// Shortfalls never paid run through every rate period, as Form 221's Chart
/// of Total Days per Period counts them.
#[test]
fn estimated_days_match_the_form_221_chart() {
    let res = rules()
        .penalty(&tax(USCents(4_444_444)).facts(), day(2027, 6, 30))
        .unwrap();
    assert_eq!(res.value().as_of(), day(2027, 4, 15));
    let lines = res.value().lines();
    assert_eq!(lines.len(), 4);
    let chart: [&[i64]; 4] = [&[76, 92, 92], &[15, 92, 92], &[15, 92], &[]];
    let fourth = [105, 105, 105, 90];
    for (i, want) in chart.iter().enumerate() {
        let parts = lines[i].parts();
        assert_eq!(parts.len(), want.len(), "line {}", i + 1);
        for (j, p) in parts.iter().enumerate() {
            assert_eq!(p.days(), want[j], "line {} rate period {}", i + 1, j + 1);
        }
        let from = if i == 3 { dues()[3] } else { day(2026, 12, 31) };
        assert_eq!(res.value().as_of().days_since(from), fourth[i]);
    }
    assert!(lines[3].days() == 90 && lines[0].days() == 365);
    assert!(because(&res.required(), PositionKind::UnpublishedRate).contains("Dec 31, 2026"));
    assert!(applied(&res.applied()).contains(
        "the $477.81 of penalty on the April 15, 2026 installment outstanding on January 1, 2027 is added to its principal"
    ));
}

/// The exceptions that leave nothing to pay.
#[test]
fn estimated_exceptions() {
    let none = arizonataxcalc::new_estimated_facts()
        .no_federal_liability_last_year(true)
        .build()
        .unwrap();
    let h = Household {
        extras: Some(none),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap();
    assert!(r.value().exempt() && applied(&r.applied()).contains("federal exception"));
    let c = r.value().chosen().unwrap();
    assert!(
        c.basis() == Basis::Exception
            && c.amount().0 == 0
            && c.label() == "No federal tax liability last year"
    );
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().reason(), "No federal tax liability last year.");
    assert_eq!(p.value().total().0, 0);

    let h = Household {
        withheld: DOLLAR * 9_000,
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap();
    assert!(!r.value().required() && !r.value().exempt());
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(p.value().total().0, 0);

    let h = Household {
        return_filed: day(2027, 2, 1),
        ..tax(DOLLAR * 10_000)
    };
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(p.value().as_of() == day(2027, 2, 1) && p.value().lines().len() == 3);
    assert!(applied(&p.applied()).contains("fourth installment is excused"));
}

/// A penalty is shown before any waiver, and the waiver is asked about.
#[test]
fn estimated_waiver_is_asked() {
    let p = rules()
        .penalty(&tax(DOLLAR * 10_000).facts(), day(2027, 4, 15))
        .unwrap();
    assert!(p.value().total().0 != 0 && has(&p.required(), PositionKind::PenaltyWaiver));
    let h = Household {
        waiver: true,
        ..tax(DOLLAR * 10_000)
    };
    let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(!has(&p.required(), PositionKind::PenaltyWaiver));
}

/// Form 221's annualised worksheet, lines 15 to 23.
#[test]
fn estimated_annualised_worksheet() {
    let h = Household {
        annualised: Some(vec![
            DOLLAR * 8_000,
            DOLLAR * 20_000,
            DOLLAR * 30_000,
            DOLLAR * 40_000,
        ]),
        ..tax(USCents(4_444_444))
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [
        DOLLAR * 1_800,
        DOLLAR * 9_000,
        DOLLAR * 20_250,
        DOLLAR * 36_000,
    ]
    .into_iter()
    .enumerate()
    {
        assert_eq!(s[i].needed_by(), want, "installment {}", i + 1);
    }
    let periods: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 6, 30),
        day(2026, 8, 31),
        day(2026, 12, 31),
    ]
    .into_iter()
    .map(|d| estimated::new_period().end(d).build().unwrap())
    .collect();
    let wrong = estimated::new_facts(AZ, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(h.facts().this_year())
        .elections(estimated::new_elections().annualised().build().unwrap())
        .periods(&periods)
        .build()
        .unwrap();
    assert!(rules().schedule(&wrong).is_err());
}

/// Withholding counts a quarter on each date, unless the filer shows when it
/// was withheld.
#[test]
fn estimated_withholding_credit() {
    let mut h = Household {
        withheld: DOLLAR * 4_000,
        prior: Some(prior(DOLLAR * 4_000, DOLLAR * 200_000)),
        ..tax(DOLLAR * 20_000)
    };
    for inst in rules().schedule(&h.facts()).unwrap().value() {
        assert_eq!(inst.status(), Status::Met);
    }
    h.as_withheld = true;
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [Status::Short, Status::Short, Status::Short, Status::Met]
        .into_iter()
        .enumerate()
    {
        assert_eq!(s[i].status(), want, "installment {}", i + 1);
    }
}

/// Arizona gross income comes from GrossIncome; with only federal AGI given
/// it is taken from that and asked about.
#[test]
fn estimated_gross_income_when_not_given() {
    let h = Household {
        no_gross: true,
        agi: DOLLAR * 70_000,
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(
        res.value().exempt()
            && applied(&res.applied()).contains("federal AGI, taken as Arizona gross income")
    );
    let h = Household {
        no_gross: true,
        agi: DOLLAR * 90_000,
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(
        !res.value().exempt()
            && because(&res.required(), PositionKind::AdjustedGrossIncome)
                .contains("your Arizona gross income if you lived in Arizona all year")
    );
    let h = Household {
        no_gross: true,
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(
        res.value().required()
            && because(&res.required(), PositionKind::AdjustedGrossIncome)
                .contains("over $75,000 in both 2025 and 2026")
    );
    let e = arizonataxcalc::new_estimated_facts().build().unwrap();
    let h = Household {
        extras: Some(e),
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let n = res
        .required()
        .iter()
        .filter(|r| r.position() == PositionKind::JurisdictionFact)
        .count();
    assert_eq!(n, 2);
}

/// Each divergence is reported with both readings, both sources, the
/// reading the figures follow, and whether it changes these facts.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want: HashMap<&str, (&str, &str, &str, Reading, bool)> = HashMap::from([
        (
            "Arizona gross income of exactly the threshold",
            (
                "only below the threshold",
                "§ 43-581(A)",
                "exactly $75,000 isn't subject",
                Reading::Statute,
                false,
            ),
        ),
        (
            "The prior-year option needs a filed return",
            (
                "required to file a 2025 Arizona return, and filed it",
                "§ 43-581(A), (D)",
                "no condition that a return was required, filed or covered 12 months",
                Reading::Form,
                false,
            ),
        ),
        (
            "When the penalty stops",
            (
                "April 15 of the next year",
                "§ 42-1125(Q)",
                "can't exceed 10% of that amount",
                Reading::Form,
                true,
            ),
        ),
        (
            "The 2026 rates",
            (
                "prints 7%",
                "§ 42-1123(A)",
                "federal short-term rate",
                Reading::Statute,
                true,
            ),
        ),
    ]);
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let rule = d.rule();
        let w = want[rule.as_str()];
        assert!(
            d.form().contains(w.0)
                && d.statute_source().contains(w.1)
                && !d.form_source().is_empty()
                && d.statute().contains(w.2),
            "{rule:?}"
        );
        assert!(d.uses() == w.3 && d.affects_household() == w.4, "{rule:?}");
    }
    let affects = |h: Household, rule: &str| -> bool {
        rules()
            .requirement(&h.facts())
            .unwrap()
            .divergences()
            .iter()
            .find(|d| d.rule() == rule)
            .unwrap_or_else(|| panic!("no divergence {rule:?}"))
            .affects_household()
    };
    assert!(affects(
        Household {
            gross: DOLLAR * 75_000,
            ..tax(DOLLAR * 10_000)
        },
        "Arizona gross income of exactly the threshold"
    ));
    assert!(!affects(
        Household {
            withheld: DOLLAR * 9_000,
            ..tax(DOLLAR * 10_000)
        },
        "When the penalty stops"
    ));
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    assert!(affects(
        Household {
            prior: Some(not_filed),
            ..tax(DOLLAR * 10_000)
        },
        "The prior-year option needs a filed return"
    ));
    let h = Household {
        gross: DOLLAR * 75_000,
        ..tax(DOLLAR * 10_000)
    };
    assert!(rules().requirement(&h.facts()).unwrap().value().exempt());
    let p = rules()
        .penalty(&tax(DOLLAR * 10_000).facts(), day(2027, 12, 31))
        .unwrap();
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for year in [2025, 2027] {
        let err = arizonataxcalc::estimated_tax(year).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{year}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let base = |j: estimated::Jurisdiction, y: i64| {
        let mut b = estimated::new_facts(j, y);
        b.filing_status(FilingStatus::Single).this_year(this);
        b
    };
    for (name, f) in [
        (
            "a farmer",
            base(AZ, 2026).farm_share(Rate(7000)).build().unwrap(),
        ),
        (
            "a fisherman",
            base(AZ, 2026).fish_share(Rate(7000)).build().unwrap(),
        ),
        (
            "a nonresident alien",
            base(AZ, 2026)
                .state(
                    arizonataxcalc::new_estimated_facts()
                        .nonresident_alien()
                        .build()
                        .unwrap(),
                )
                .build()
                .unwrap(),
        ),
    ] {
        let err = rules().requirement(&f).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    for (name, f) in [
        (
            "California's facts",
            base(estimated::CALIFORNIA, 2026).build().unwrap(),
        ),
        ("2027", base(AZ, 2027).build().unwrap()),
        (
            "other extras",
            base(AZ, 2026).state("not Arizona's").build().unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
        assert!(
            rules().penalty(&f, day(2027, 4, 15)).is_err(),
            "{name}: penalty accepted"
        );
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
        "$75,000",
        "$150,000",
        "Arizona gross income",
        "$1,000",
        "no 110%",
        "10% of the shortfall",
        "January 1",
        "federal exception",
        "required to file",
        "February 1, 2027",
        "6%",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.versus_federal().is_empty() && !e.sources().is_empty());
}

/// Without this year's tax, the target is 100% of last year's, the penalty
/// against it is an upper bound, and with no 2025 return there is no target.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: Some(prior(DOLLAR * 8_000, DOLLAR * 200_000)),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(
        r.required() && r.target() == DOLLAR * 8_000 && r.target_known(),
        "{}",
        r.why()
    );
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert_eq!(r.chosen().unwrap().basis(), Basis::PriorYearTax);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [2_000, 4_000, 6_000, 8_000].into_iter().enumerate() {
        assert_eq!(sched.value()[i].needed_by(), DOLLAR * want);
    }
    let pen = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!(pen.total().0 != 0 && (pen.upper_bound() || !pen.rated_through().is_zero()));

    let none = Household {
        no_tax: true,
        no_prior: true,
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

    let no_fed = arizonataxcalc::new_estimated_facts()
        .no_federal_liability_last_year(true)
        .build()
        .unwrap();
    for (i, h) in [
        Household {
            no_tax: true,
            prior: Some(prior(DOLLAR * 8_000, DOLLAR * 60_000)),
            ..Household::default()
        },
        Household {
            no_tax: true,
            extras: Some(no_fed),
            ..Household::default()
        },
    ]
    .into_iter()
    .enumerate()
    {
        let p = rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .value();
        assert!(p.total().0 == 0 && !p.upper_bound(), "case {}", i + 1);
    }
}

/// The waivers are decided from the conditions the Form 221 instructions
/// state. The penalty is still figured in full.
#[test]
fn estimated_waiver_is_decided_from_its_conditions() {
    let base = tax(DOLLAR * 10_000);
    let before = rules().penalty(&base.facts(), day(2027, 4, 15)).unwrap();
    assert_ne!(before.value().total().0, 0);
    let pos = |c: WaiverCause, on: Date, born: Option<Date>| {
        let mut b = estimated::new_positions();
        b.waiver_for(c, on);
        if let Some(d) = born {
            b.born(d);
        }
        b.build().unwrap()
    };
    for (name, positions, eligible, refused, reason) in [
        (
            "retired in 2026 at 64",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1962, 3, 1)),
            ),
            true,
            false,
            "after reaching 62",
        ),
        (
            "retired in 2025, the year before",
            pos(
                WaiverCause::Retirement,
                day(2025, 6, 1),
                Some(day(1960, 3, 1)),
            ),
            true,
            false,
            "You retired in 2025",
        ),
        (
            "retired in 2024",
            pos(
                WaiverCause::Retirement,
                day(2024, 6, 1),
                Some(day(1955, 3, 1)),
            ),
            false,
            true,
            "2025 or 2026",
        ),
        (
            "retired in 2020",
            pos(
                WaiverCause::Retirement,
                day(2020, 6, 1),
                Some(day(1955, 3, 1)),
            ),
            false,
            true,
            "You retired in 2020",
        ),
        (
            "retired at 61",
            pos(
                WaiverCause::Retirement,
                day(2026, 6, 1),
                Some(day(1965, 3, 1)),
            ),
            false,
            true,
            "You were 61",
        ),
        (
            "disabled in 2026",
            pos(WaiverCause::Disability, day(2026, 2, 1), None),
            true,
            false,
            "became disabled in 2026",
        ),
        (
            "a disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), None),
            true,
            false,
            "a disaster",
        ),
    ] {
        let h = Household {
            positions: Some(positions),
            ..base.clone()
        };
        let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        let o = p.value().waiver_outcome().unwrap();
        assert!(
            o.eligible() == eligible && o.refused() == refused,
            "{name}: {:?}",
            o.reason()
        );
        assert!(o.reason().contains(reason), "{name}: {:?}", o.reason());
        if eligible {
            assert!(
                o.reason()
                    .to_lowercase()
                    .contains("the arizona department of revenue"),
                "{name}"
            );
        }
        assert_eq!(p.value().total(), before.value().total(), "{name}");
        let want = if eligible {
            USCents(0)
        } else {
            p.value().total()
        };
        assert_eq!(p.value().if_waived(), want, "{name}");
        assert!(!has(&p.required(), PositionKind::PenaltyWaiver), "{name}");
    }
}

/// An exception sets a $0 target, so a question whose answer would only
/// change the target is not asked under one.
#[test]
fn estimated_exception_asks_nothing_about_the_target() {
    for (name, h, exempt) in [
        (
            "tax under the threshold, no 2025 return",
            Household {
                no_prior: true,
                ..tax(DOLLAR * 400)
            },
            true,
        ),
        (
            "tax over the threshold, no 2025 return",
            Household {
                no_prior: true,
                ..tax(DOLLAR * 10_000)
            },
            false,
        ),
    ] {
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(res.value().exempt(), exempt, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn),
            !exempt,
            "{name}"
        );
        if exempt {
            for q in res.required() {
                assert!(!q.effect().to_lowercase().contains("target"), "{name}");
            }
        }
    }
}

/// Withholding over the tax reads as a refund, never as a minus amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let h = Household {
        withheld: DOLLAR * 3_600,
        ..tax(DOLLAR * 3_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert_eq!(
        r.why(),
        "Your Arizona tax after credits is $3,000, and $3,600 was withheld. That leaves a $600 refund. No estimated payments are needed."
    );
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
    for s in texts {
        assert!(!s.contains("−$") && !s.contains("-$"), "{s:?}");
    }
}
