//! Minnesota's estimated tax rules for 2026, checked against the worked
//! figures of Schedule M15. Ported from estimated_test.go; the Go test that
//! finds every figure in its extract in testdata/ reads the Go package's
//! citations and is not ported.

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, Elections, EstimatedTax, Facts, Month, Payment, Penalty, Positions, PriorReturn,
    ResidencePeriod, Status, WaiverCause, WaiverKind,
};
use money::{DOLLAR, USCents, percent};
use taxcalcs_minnesotataxcalc as minnesotataxcalc;
use working::{PositionKind, Required};

const MN: estimated::Jurisdiction = estimated::MINNESOTA;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    minnesotataxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

fn because(rs: &[Required], k: PositionKind, phrase: &str) -> bool {
    rs.iter().any(|r| {
        r.position() == k && (r.question().contains(phrase) || r.effect().contains(phrase))
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

#[derive(Clone, Default)]
struct Household {
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    refundable: Option<USCents>,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    residence: Vec<ResidencePeriod>,
    return_filed: Date,
    elections: Option<Elections>,
    periods: Option<Vec<USCents>>,
    extras: Option<minnesotataxcalc::EstimatedFacts>,
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
        let figures = if self.no_tax {
            estimated::new_figures().build().unwrap()
        } else {
            estimated::new_figures().tax(self.tax).build().unwrap()
        };
        let mut b = estimated::new_facts(MN, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(figures)
            .paid(&self.payments)
            .residence(&self.residence)
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
            Some(e) => {
                b.state(e);
            }
            None => {
                b.state(
                    minnesotataxcalc::new_estimated_facts()
                        .refundable_credits(self.refundable.unwrap_or_default())
                        .build()
                        .unwrap(),
                );
            }
        }
        if let Some(e) = self.elections {
            b.elections(e);
        }
        if let Some(periods) = &self.periods {
            let ends = [
                day(2026, 3, 31),
                day(2026, 5, 31),
                day(2026, 8, 31),
                day(2026, 12, 31),
            ];
            let ps: Vec<_> = periods
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
            b.periods(&ps)
                .elections(estimated::new_elections().annualised().build().unwrap());
        }
        if let Some(k) = self.waiver {
            b.positions(estimated::new_positions().waiver(k).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }
}

fn prior(tax: USCents, agi: USCents) -> Option<PriorReturn> {
    let f = estimated::new_figures()
        .tax(tax)
        .federal_agi(agi)
        .build()
        .unwrap();
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(f)
            .full_year_resident(true)
            .build()
            .unwrap(),
    )
}

fn part_year(tax: USCents, federal_agi: USCents, minnesota_agi: USCents) -> Option<PriorReturn> {
    let f = estimated::new_figures()
        .tax(tax)
        .federal_agi(federal_agi)
        .state_agi(minnesota_agi)
        .build()
        .unwrap();
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(f)
            .full_year_resident(false)
            .build()
            .unwrap(),
    )
}

#[test]
fn schedule_for_twenty_twenty_six() {
    let res = rules().schedule(&h(10_000).facts()).unwrap();
    for (i, s) in res.value().iter().enumerate() {
        assert_eq!(s.due(), dues()[i], "installment {}", i + 1);
        let wd = s.due().weekday();
        assert!(wd != estimated::Weekday::SATURDAY && wd != estimated::Weekday::SUNDAY);
        assert_eq!(s.needed_by(), DOLLAR * 2_250 * (i as i64 + 1));
    }
    assert_eq!(day(2027, 1, 31).weekday(), estimated::Weekday::SUNDAY);
    for (filed, excused) in [(day(2027, 1, 31), true), (day(2027, 2, 1), false)] {
        let s = rules()
            .schedule(
                &Household {
                    return_filed: filed,
                    ..h(10_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        assert_eq!(s[3].status() == Status::Excused, excused, "filed {filed}");
    }
}

#[test]
fn threshold_boundary() {
    for (withheld, refundable, exempt) in [
        (950_001, 0, true),  // $499.99
        (950_000, 0, false), // $500.00
        (900_000, 50_001, true),
        (900_000, 50_000, false),
    ] {
        let hh = Household {
            withheld: USCents(withheld),
            refundable: Some(USCents(refundable)),
            ..h(10_000)
        };
        let r = rules().requirement(&hh.facts()).unwrap().value();
        assert_eq!(r.exempt(), exempt, "withheld {withheld}: {}", r.why());
        if exempt {
            let c = r.chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && c.label() == "Tax less withholding and credits under $500",
                "chosen {c:?}"
            );
        }
    }
}

#[test]
fn prior_year_step() {
    for (agi, want, rate) in [
        (15_000_000, DOLLAR * 8_000, percent(100)),
        (15_000_001, DOLLAR * 8_800, percent(110)),
    ] {
        let r = rules()
            .requirement(
                &Household {
                    prior: prior(DOLLAR * 8_000, USCents(agi)),
                    ..h(20_000)
                }
                .facts(),
            )
            .unwrap()
            .value();
        let c = r.chosen().unwrap();
        assert!(
            c.basis() == Basis::PriorYearTax && r.target() == want && c.percent() == rate,
            "AGI {agi}: {c:?}"
        );
        assert_eq!(r.options()[0].amount(), DOLLAR * 18_000);
    }
    let r = rules()
        .requirement(
            &Household {
                prior: prior(DOLLAR * 8_000, DOLLAR * 100_000),
                ..h(5_000)
            }
            .facts(),
        )
        .unwrap()
        .value();
    assert_eq!(r.chosen().unwrap().basis(), Basis::ThisYearTax);
    assert_eq!(r.target(), DOLLAR * 4_500);
    let no_agi = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 8_000)
                .build()
                .unwrap(),
        )
        .full_year_resident(true)
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
}

#[test]
fn prior_year_option_availability() {
    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 5_000)
                .federal_agi(DOLLAR * 50_000)
                .build()
                .unwrap(),
        )
        .full_year_resident(true)
        .build()
        .unwrap();
    let cases = [
        (
            "no return on record",
            None,
            false,
            PositionKind::PriorYearReturn,
        ),
        (
            "not filed",
            Some(not_filed),
            false,
            PositionKind::Unspecified,
        ),
        (
            "eleven months",
            Some(short),
            false,
            PositionKind::Unspecified,
        ),
        (
            "no liability",
            prior(USCents(0), DOLLAR * 50_000),
            false,
            PositionKind::Unspecified,
        ),
        (
            "nonresident with $0.99 of tax",
            part_year(USCents(99), DOLLAR * 50_000, DOLLAR * 1_000),
            false,
            PositionKind::Unspecified,
        ),
        (
            "nonresident with $1.00 of tax",
            part_year(USCents(100), DOLLAR * 50_000, DOLLAR * 1_000),
            true,
            PositionKind::Unspecified,
        ),
        (
            "resident with $0.99 of tax",
            prior(USCents(99), DOLLAR * 50_000),
            true,
            PositionKind::Unspecified,
        ),
    ];
    for (name, p, available, ask) in cases {
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
        assert_eq!(o.available(), available, "{name}: {}", o.why_not());
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}: {ask} is not asked");
        }
    }
}

#[test]
fn nonresident_step_is_an_open_question() {
    const Q: &str = "Minnesota share of AGI or your federal AGI";
    let last = part_year(DOLLAR * 8_000, DOLLAR * 200_000, DOLLAR * 100_000);
    let res = rules()
        .requirement(
            &Household {
                prior: last,
                ..h(20_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_000);
    assert!(because(&res.required(), PositionKind::JurisdictionFact, Q));
    let federal = minnesotataxcalc::new_estimated_facts()
        .refundable_credits(USCents(0))
        .prior_year_step_on_minnesota_share(false)
        .build()
        .unwrap();
    let res = rules()
        .requirement(
            &Household {
                prior: last,
                extras: Some(federal),
                ..h(20_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_800);
    assert!(!because(&res.required(), PositionKind::JurisdictionFact, Q));
    let this_year = estimated::new_residence_period("MN")
        .from(day(2026, 1, 1))
        .to(day(2026, 6, 30))
        .build()
        .unwrap();
    let full = prior(DOLLAR * 8_000, DOLLAR * 200_000);
    let res = rules()
        .requirement(
            &Household {
                prior: full,
                residence: vec![this_year],
                ..h(20_000)
            }
            .facts(),
        )
        .unwrap();
    assert_eq!(res.value().target(), DOLLAR * 8_800);
    assert!(because(&res.required(), PositionKind::JurisdictionFact, Q));
    let res = rules()
        .requirement(
            &Household {
                prior: full,
                ..h(20_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(!because(&res.required(), PositionKind::JurisdictionFact, Q));
}

#[test]
fn penalty_arithmetic() {
    let res = rules()
        .penalty(&h(10_000).facts(), day(2027, 4, 15))
        .unwrap();
    let lines = res.value().lines();
    let want_days = [365, 304, 212, 90];
    let want_charge = [15_750, 13_118, 9_148, 3_884];
    assert_eq!(lines.len(), 4);
    for (i, l) in lines.iter().enumerate() {
        let parts = l.parts();
        assert!(
            parts.len() == 1
                && parts[0].days() == want_days[i]
                && parts[0].charge() == USCents(want_charge[i])
                && parts[0].rate() == percent(7),
            "line {}: {parts:?}",
            i + 1
        );
    }
    assert_eq!(res.value().total(), USCents(41_900));
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
    let res = rules()
        .penalty(&h(10_000).facts(), day(2026, 12, 31))
        .unwrap();
    let l = res.value().lines()[0].clone();
    assert_eq!(l.days(), 260);
    assert_eq!(l.total(), USCents(11_219));
    let res = rules()
        .penalty(&h(10_000).facts(), day(2027, 6, 30))
        .unwrap();
    assert_eq!(res.value().as_of(), day(2027, 4, 15));
    assert_eq!(res.value().total(), USCents(41_900));
}

#[test]
fn shortfall_paid_in_part_follows_line_seventeen() {
    let d = dues();
    let hh = Household {
        payments: vec![
            pay(day(2026, 5, 15), DOLLAR * 1_000),
            pay(d[1], DOLLAR * 3_500),
            pay(d[2], DOLLAR * 2_250),
            pay(d[3], DOLLAR * 2_250),
        ],
        ..h(10_000)
    };
    let res = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
    let lines = res.value().lines();
    assert!(
        lines.len() == 1 && lines[0].installment() == 1 && lines[0].days() == 61,
        "lines {lines:?}"
    );
    let parts = lines[0].parts();
    assert!(parts.len() == 1 && parts[0].principal() == DOLLAR * 2_250);
    assert_eq!(res.value().total(), USCents(2_632));
}

#[test]
fn return_filed_by_january_thirty_first() {
    let p = |filed: Date| {
        rules()
            .penalty(
                &Household {
                    return_filed: filed,
                    ..h(10_000)
                }
                .facts(),
                day(2027, 4, 15),
            )
            .unwrap()
            .value()
    };
    let v = p(day(2027, 1, 29));
    assert_eq!(v.as_of(), day(2027, 1, 15));
    assert_eq!(v.total(), USCents(26_364));
    assert_eq!(p(day(2027, 2, 1)).as_of(), day(2027, 2, 1));
    assert_eq!(p(day(2027, 1, 10)).as_of(), day(2027, 1, 10));
}

#[test]
fn withholding_and_refundable_credits() {
    let p = rules()
        .penalty(
            &Household {
                withheld: DOLLAR * 9_000,
                ..h(10_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert_eq!(p.value().total(), USCents(0));
    let as_withheld = estimated::new_elections()
        .withholding_as_withheld()
        .build()
        .unwrap();
    let p = rules()
        .penalty(
            &Household {
                withheld: DOLLAR * 9_000,
                elections: Some(as_withheld),
                ..h(10_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert!(p.value().total().0 != 0 && p.value().lines().len() == 3);
    let p = rules()
        .penalty(
            &Household {
                refundable: Some(DOLLAR * 9_000),
                elections: Some(as_withheld),
                ..h(10_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert_eq!(p.value().total(), USCents(0));
    let f = estimated::new_facts(MN, 2026)
        .filing_status(filing::Status::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    assert!(because(
        &res.required(),
        PositionKind::JurisdictionFact,
        "refundable credits"
    ));
}

#[test]
fn no_liability_exception() {
    let res = rules()
        .requirement(
            &Household {
                prior: prior(USCents(0), DOLLAR * 20_000),
                ..h(10_000)
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().exempt(), "{}", res.value().why());
    let c = res.value().chosen().unwrap();
    assert!(
        c.basis() == Basis::Exception
            && c.label() == "No tax due last year as a full-year resident"
    );
    let not_required = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let exempt = |p: Option<PriorReturn>| {
        rules()
            .requirement(
                &Household {
                    prior: p,
                    ..h(10_000)
                }
                .facts(),
            )
            .unwrap()
    };
    assert!(exempt(Some(not_required)).value().exempt());
    assert!(
        !exempt(part_year(USCents(0), DOLLAR * 20_000, DOLLAR * 5_000))
            .value()
            .exempt()
    );
    let unanswered = estimated::new_prior_return()
        .filed(true)
        .figures(estimated::new_figures().tax(USCents(0)).build().unwrap())
        .build()
        .unwrap();
    let res = exempt(Some(unanswered));
    assert!(!res.value().exempt());
    assert!(has(&res.required(), PositionKind::Residency));
}

#[test]
fn annualized_method() {
    let hh = Household {
        periods: Some(vec![
            DOLLAR * 2_000,
            DOLLAR * 4_000,
            DOLLAR * 8_000,
            DOLLAR * 10_000,
        ]),
        ..h(10_000)
    };
    let s = rules().schedule(&hh.facts()).unwrap().value();
    for (i, want) in [450, 1_800, 5_400, 9_000].into_iter().enumerate() {
        assert_eq!(s[i].needed_by(), DOLLAR * want, "installment {}", i + 1);
    }
    let period = |m: i64, d: i64| {
        estimated::new_period()
            .end(day(2026, m, d))
            .build()
            .unwrap()
    };
    let f = estimated::new_facts(MN, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .elections(estimated::new_elections().annualised().build().unwrap())
        .periods(&[period(3, 31), period(6, 30), period(8, 31), period(12, 31)])
        .build()
        .unwrap();
    assert!(rules().schedule(&f).is_err());
}

#[test]
fn without_this_years_tax() {
    let hh = Household {
        no_tax: true,
        prior: prior(DOLLAR * 8_000, DOLLAR * 100_000),
        ..Household::default()
    };
    let res = rules().requirement(&hh.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 8_000);
    let o = r.options()[0].clone();
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let p = rules().penalty(&hh.facts(), day(2027, 4, 15)).unwrap();
    assert!(p.value().upper_bound() && p.value().total().0 != 0);
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
}

#[test]
fn waiver_is_asked() {
    let res = rules()
        .penalty(&h(10_000).facts(), day(2027, 4, 15))
        .unwrap();
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));
    let k = WaiverKind::RetiredOrDisabled;
    let res = rules()
        .penalty(
            &Household {
                waiver: Some(k),
                ..h(10_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap();
    assert!(!has(&res.required(), PositionKind::PenaltyWaiver));
    assert_eq!(res.value().waiver(), k);
    assert_eq!(res.value().total(), USCents(41_900));
}

#[test]
fn divergences_pin_both_readings() {
    let res = rules().requirement(&h(10_000).facts()).unwrap();
    let want = [
        (
            "When a payment counts as made",
            "must receive",
            "§ 270C.395",
        ),
        ("A shortfall paid in part", "paid in full", "subd. 3(a)(2)"),
        (
            "A return filed and paid by January 31",
            "Jan 15, 2027",
            "subd. 9",
        ),
        ("January 31, 2027 is a Sunday", "not moved", "§ 645.151"),
        ("The optional short method", "2%", "subd. 2"),
        (
            "Waivers for retirement, disability or casualty",
            "IRS waived",
            "subd. 7",
        ),
        ("Who counts as a farmer", "this year only", "§ 6654(i)(2)"),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, form, statute)) = want.iter().find(|w| w.0 == d.rule()) else {
            panic!("unexpected divergence {:?}", d.rule());
        };
        assert_eq!(
            d.affects_household(),
            d.rule() == "The optional short method",
            "{:?}",
            d.rule()
        );
        assert!(
            d.form().contains(form)
                && d.statute_source().contains(statute)
                && !d.form_source().is_empty()
                && !d.statute().is_empty(),
            "{:?}",
            d.rule()
        );
    }
}

#[test]
fn refuses_what_is_not_built() {
    assert!(
        minnesotataxcalc::estimated_tax(2025)
            .err()
            .is_some_and(|e| e.is(&estimated::Error::NoRules))
    );
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let farmer = estimated::new_facts(MN, 2026)
        .filing_status(filing::Status::Single)
        .this_year(this)
        .farm_share(money::Rate(7000))
        .build()
        .unwrap();
    assert!(
        rules()
            .requirement(&farmer)
            .unwrap_err()
            .is(&estimated::Error::Unsupported)
    );
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
            estimated::new_facts(MN, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(MN, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("not Minnesota's")
                .build()
                .unwrap(),
        ),
    ];
    for (name, f) in cases {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    let f = Household {
        tax: USCents(100),
        ..Household::default()
    }
    .facts();
    assert!(rules().penalty(&f, Date::default()).is_err());
    assert!(
        minnesotataxcalc::new_estimated_facts()
            .refundable_credits(USCents(-1))
            .build()
            .is_err()
    );
}

#[test]
fn explanation_names_the_differences() {
    let e = rules().explanation();
    let text = format!(
        "{}{}{}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$500",
        "7%",
        "$150,000",
        "refundable credits",
        "full year of Minnesota residence",
        "January 15",
        "paid in full",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
}

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
    let f = estimated::new_facts(MN, 2026)
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

fn waived(pos: Positions) -> Penalty {
    rules()
        .penalty(
            &Household {
                positions: Some(pos),
                ..h(10_000)
            }
            .facts(),
            day(2027, 4, 15),
        )
        .unwrap()
        .value()
}

fn retired_on(on: Date, born: Date) -> Positions {
    estimated::new_positions()
        .waiver_for(WaiverCause::Retirement, on)
        .born(born)
        .build()
        .unwrap()
}

#[test]
fn waiver_follows_the_federal_one() {
    let p = waived(retired_on(day(2026, 6, 1), day(1962, 3, 1)));
    let o = p.waiver_outcome().unwrap();
    assert!(o.eligible() && p.total() == USCents(41_900) && p.if_waived().0 == 0);
    let p = waived(retired_on(day(2020, 6, 1), day(1955, 3, 1)));
    let o = p.waiver_outcome().unwrap();
    assert!(o.refused() && o.reason().contains("2025 or 2026") && p.if_waived() == p.total());
    let p = waived(retired_on(day(2026, 6, 1), day(1970, 3, 1)));
    let o = p.waiver_outcome().unwrap();
    assert!(o.refused() && o.reason().contains("62"));
    let p = waived(
        estimated::new_positions()
            .waiver_for(WaiverCause::Disaster, day(2026, 8, 1))
            .build()
            .unwrap(),
    );
    let o = p.waiver_outcome().unwrap();
    assert!(o.eligible() && o.reason().contains("Minnesota's Department of Revenue"));
}

#[test]
fn exempt_asks_nothing_of_the_target() {
    let res = rules().requirement(&h(100).facts()).unwrap();
    assert!(res.value().exempt());
    assert!(!has(&res.required(), PositionKind::PriorYearReturn));
    for q in res.required() {
        assert!(
            !q.effect().to_lowercase().contains("target"),
            "under an exception a question says {:?}",
            q.effect()
        );
    }
}
