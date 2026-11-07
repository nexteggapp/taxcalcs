//! Indiana's estimated tax rules for 2026, checked against the Department
//! of Revenue's forms and the Indiana Code.
//!
//! Go's TestEstimatedFiguresAreInTheirSources and the testdata/ phrase
//! checks inside the other tests are not ported.

#![allow(clippy::unwrap_used)]

use std::collections::HashMap;

use estimated::{
    Basis, Date, EstimatedTax, Facts, Figures, Month, PartKind, Payment, Positions, PriorReturn,
    ResidencePeriod, Status, TargetOption, WaiverCause, WaiverKind, Weekday,
};
use filing::Status as FilingStatus;
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_indianataxcalc as indianataxcalc;
use working::{PositionKind, Reading, Required};

use indianataxcalc::EstimatedFacts;

const INDIANA: estimated::Jurisdiction = estimated::INDIANA;

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    indianataxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

fn count(rs: &[Required], k: PositionKind) -> usize {
    rs.iter().filter(|r| r.position() == k).count()
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
    /// State and county, IT-2210 line 3.
    tax: USCents,
    no_tax: bool,
    no_local: bool,
    status: FilingStatus,
    withheld: USCents,
    withheld_on: Date,
    as_withheld: bool,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    return_filed: Date,
    residence: Vec<ResidencePeriod>,
    /// IT-2210A line 10, one per column.
    annualised: Option<Vec<USCents>>,
    waiver: Option<WaiverKind>,
    positions: Option<Positions>,
    farm: Rate,
    fish: Rate,
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
        let mut b = estimated::new_facts(INDIANA, 2026);
        b.filing_status(status)
            .this_year(self.figures())
            .paid(&self.payments)
            .return_filed(self.return_filed)
            .residence(&self.residence)
            .farm_share(self.farm)
            .fish_share(self.fish);
        if self.withheld.0 > 0 {
            let on = if self.withheld_on.is_zero() {
                day(2026, 12, 15)
            } else {
                self.withheld_on
            };
            b.withheld(&[estimated::new_withheld()
                .on(on)
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
                indianataxcalc::new_estimated_facts()
                    .pass_through_entity_tax_credit(USCents(0))
                    .build()
                    .unwrap(),
            ),
        };
        let mut el = estimated::new_elections();
        if self.as_withheld {
            el.withholding_as_withheld();
        }
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
            el.annualised();
        }
        b.elections(el.build().unwrap());
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        if let Some(p) = self.positions {
            b.positions(p);
        }
        b.build().unwrap()
    }

    fn figures(&self) -> Figures {
        let mut b = estimated::new_figures();
        if !self.no_tax {
            b.tax(self.tax);
            if !self.no_local {
                b.local_tax(USCents(0));
            }
        }
        b.build().unwrap()
    }
}

/// A filed full-year resident return with its tax and federal AGI.
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

fn extras() -> EstimatedFactsBuilderExt {
    EstimatedFactsBuilderExt(indianataxcalc::new_estimated_facts())
}

/// A builder that starts with the pass-through credit answered as zero.
struct EstimatedFactsBuilderExt(indianataxcalc::EstimatedFactsBuilder);

impl EstimatedFactsBuilderExt {
    fn with(
        mut self,
        f: impl FnOnce(&mut indianataxcalc::EstimatedFactsBuilder),
    ) -> EstimatedFacts {
        self.0.pass_through_entity_tax_credit(USCents(0));
        f(&mut self.0);
        self.0.build().unwrap()
    }
}

/// The 2026 dates are the ones ES-40 prints, and none moves. The
/// file-and-pay date, January 31, 2027, is a Sunday, and IB3 moves it to
/// Monday, February 1.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&tax(DOLLAR * 10_000).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
    }
    assert_eq!(day(2027, 1, 31).weekday(), Weekday::SUNDAY);
    for (filed, excused) in [
        (day(2027, 1, 29), true),
        (day(2027, 2, 1), true),
        (day(2027, 2, 2), false),
    ] {
        let h = Household {
            return_filed: filed,
            ..tax(DOLLAR * 10_000)
        };
        let s = rules().schedule(&h.facts()).unwrap().value();
        assert_eq!(s[3].status() == Status::Excused, excused, "filed {filed}");
    }
}

/// IT-2210 line 6: no penalty below $1,000 of state and county tax less
/// withholding and the pass-through credit. Exactly $1,000 is covered.
#[test]
fn estimated_threshold_boundaries() {
    for (w, ptet, exempt) in [
        (900_001, 0, true),
        (900_000, 0, false),
        (800_000, 100_001, true),
        (800_000, 100_000, false),
    ] {
        let e = indianataxcalc::new_estimated_facts()
            .pass_through_entity_tax_credit(USCents(ptet))
            .build()
            .unwrap();
        let h = Household {
            withheld: USCents(w),
            extras: Some(e),
            ..tax(DOLLAR * 10_000)
        };
        let r = rules().requirement(&h.facts()).unwrap().value();
        assert_eq!(
            r.exempt(),
            exempt,
            "withheld {w}, credit {ptet}: {}",
            r.why()
        );
        if exempt {
            let c = r.chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && c.label() == "Tax less withholding and credits under $1,000"
            );
        }
    }
}

/// IT-2210's CAUTION box: 110% of last year's tax when last year's federal
/// AGI was more than $150,000, or $75,000 married filing separately.
#[test]
fn estimated_prior_year_step() {
    let option = |h: Household| -> TargetOption {
        rules().requirement(&h.facts()).unwrap().value().options()[1].clone()
    };
    let o = option(Household {
        prior: prior(DOLLAR * 5_000, DOLLAR * 158_000),
        ..tax(DOLLAR * 100_000)
    });
    assert!(o.amount() == DOLLAR * 5_500 && o.percent() == percent(110));
    let sep = extras().with(|b| {
        b.prior_year_married_separately(true);
    });
    let joint = extras().with(|b| {
        b.prior_year_married_separately(false);
    });
    for (agi, e, want) in [
        (15_000_000, None, percent(100)),
        (15_000_001, None, percent(110)),
        (7_500_000, Some(sep), percent(100)),
        (7_500_001, Some(sep), percent(110)),
        (7_500_001, Some(joint), percent(100)),
    ] {
        let o = option(Household {
            prior: prior(DOLLAR * 5_000, USCents(agi)),
            extras: e,
            ..tax(DOLLAR * 100_000)
        });
        assert_eq!(o.percent(), want, "AGI {agi}");
    }

    // Last year's status not given: this year's is used, and asked about
    // only where the two steps give different answers.
    let h = Household {
        status: FilingStatus::MarriedFilingSeparately,
        prior: prior(DOLLAR * 5_000, DOLLAR * 100_000),
        ..tax(DOLLAR * 100_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(
        res.value().options()[1].percent() == percent(110)
            && count(&res.required(), PositionKind::JurisdictionFact) == 1
    );
    let h = Household {
        prior: prior(DOLLAR * 5_000, DOLLAR * 200_000),
        ..tax(DOLLAR * 100_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(!has(&res.required(), PositionKind::JurisdictionFact));

    // Last year's AGI not given: 110%, and asked.
    let no_agi = estimated::new_prior_return()
        .filed(true)
        .full_year_resident(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 5_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let h = Household {
        prior: Some(no_agi),
        ..tax(DOLLAR * 100_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(
        res.value().options()[1].amount() == DOLLAR * 5_500
            && has(&res.required(), PositionKind::AdjustedGrossIncome)
    );
}

/// The lesser of 90% of this year's tax and last year's figure.
#[test]
fn estimated_target_options() {
    for (t, prior_tax, want, target) in [
        (
            DOLLAR * 10_000,
            DOLLAR * 9_000 + USCents(1),
            Basis::ThisYearTax,
            DOLLAR * 9_000,
        ),
        (
            DOLLAR * 10_000,
            DOLLAR * 8_999,
            Basis::PriorYearTax,
            DOLLAR * 8_999,
        ),
    ] {
        let h = Household {
            prior: prior(prior_tax, DOLLAR * 50_000),
            ..tax(t)
        };
        let r = rules().requirement(&h.facts()).unwrap().value();
        assert!(
            r.chosen().unwrap().basis() == want && r.target() == target && r.required(),
            "last year {prior_tax}"
        );
    }

    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let part_year = estimated::new_prior_return()
        .filed(true)
        .full_year_resident(false)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 500)
                .federal_agi(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let unanswered = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 500)
                .federal_agi(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    for (name, p, available, ask) in [
        (
            "no return on record",
            None,
            false,
            PositionKind::PriorYearReturn,
        ),
        (
            "no return filed",
            Some(not_filed),
            false,
            PositionKind::PriorYearReturn,
        ),
        (
            "part-year, no annualised tax",
            Some(part_year),
            false,
            PositionKind::JurisdictionFact,
        ),
        (
            "residence not given",
            Some(unanswered),
            true,
            PositionKind::Residency,
        ),
        (
            "filed, full-year, AGI given",
            prior(DOLLAR * 500, DOLLAR * 10_000),
            true,
            PositionKind::Unspecified,
        ),
    ] {
        let h = Household {
            prior: p,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let o = &res.value().options()[1];
        assert!(
            o.available() == available && (o.available() || !o.why_not().is_empty()),
            "{name}"
        );
        if ask != PositionKind::Unspecified {
            assert!(has(&res.required(), ask), "{name}: {ask} is not asked");
        }
    }

    // IT-2210's part-year example: Jane's annualised 2024 tax is $1,231.
    let jane = extras().with(|b| {
        b.prior_year_annualised_tax(DOLLAR * 1_231);
    });
    let h = Household {
        prior: Some(part_year),
        extras: Some(jane),
        ..tax(DOLLAR * 10_000)
    };
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(r.chosen().unwrap().basis() == Basis::PriorYearTax && r.target() == DOLLAR * 1_231);
}

/// IC 6-3-4-4.1(a)(3), from 2026: no return last year and no liability for
/// it means last year's tax is $0, so nothing is required.
#[test]
fn estimated_no_return_and_no_liability_last_year() {
    let none = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    for h in [
        Household {
            prior: Some(none),
            ..tax(DOLLAR * 50_000)
        },
        Household {
            no_tax: true,
            prior: Some(none),
            ..Household::default()
        },
    ] {
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(res.value().exempt() && !res.value().required() && res.value().target_known());
        assert!(!has(&res.required(), PositionKind::ThisYearTax));
        let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        assert!(p.value().total().0 == 0 && !p.value().upper_bound());
    }
}

/// Indiana's forms state no 12-month condition; whether § 6654's carries
/// over is unresolved.
#[test]
fn estimated_twelve_month_question() {
    let short = estimated::new_prior_return()
        .filed(true)
        .months(9)
        .full_year_resident(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 2_000)
                .federal_agi(DOLLAR * 60_000)
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
    assert!(
        res.value().options()[1].available() && has(&res.required(), PositionKind::UnsettledRule)
    );
    for applies in [true, false] {
        let e = extras().with(|b| {
            b.twelve_month_rule(applies);
        });
        let h = Household {
            prior: Some(short),
            extras: Some(e),
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(
            res.value().options()[1].available() != applies
                && !has(&res.required(), PositionKind::UnsettledRule),
            "rule {applies}"
        );
    }
}

/// The penalty is 10% of each installment's shortfall.
#[test]
fn estimated_penalty_is_a_flat_ten_percent() {
    let f = tax(DOLLAR * 10_000).facts();
    for (as_of, lines, total) in [
        (day(2026, 4, 14), 0, USCents(0)),
        (day(2026, 4, 15), 1, DOLLAR * 225),
        (day(2026, 7, 1), 2, DOLLAR * 450),
        (day(2027, 1, 15), 4, DOLLAR * 900),
        (day(2028, 6, 30), 4, DOLLAR * 900),
    ] {
        let res = rules().penalty(&f, as_of).unwrap();
        let p = res.value();
        assert!(
            p.lines().len() == lines && p.total() == total && p.as_of() == as_of,
            "to {as_of}"
        );
        for l in p.lines() {
            let parts = l.parts();
            assert!(
                parts.len() == 1
                    && parts[0].kind() == PartKind::Flat
                    && parts[0].rate() == percent(10)
                    && parts[0].charge() == DOLLAR * 225
            );
        }
        assert!(!has(&res.required(), PositionKind::UnpublishedRate));
    }

    // April paid one day late, or five months late, costs the same.
    for late in [day(2026, 4, 16), day(2026, 9, 15)] {
        let mut payments = vec![pay(late, DOLLAR * 2_250)];
        payments.extend(quarterly(&[
            USCents(0),
            DOLLAR * 2_250,
            DOLLAR * 2_250,
            DOLLAR * 2_250,
        ]));
        let h = Household {
            payments,
            ..tax(DOLLAR * 10_000)
        };
        let p = rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .value();
        assert!(
            p.total() == DOLLAR * 225 && p.lines().len() == 1 && p.lines()[0].installment() == 1,
            "April paid {late}"
        );
    }
}

/// IT-2210's Joe: four $200 payments, the third on October 1.
#[test]
fn estimated_late_payment_counts_in_the_next_period() {
    let d = dues();
    let h = Household {
        prior: prior(DOLLAR * 800, DOLLAR * 40_000),
        payments: vec![
            pay(d[0], USCents(20_000)),
            pay(d[1], USCents(20_000)),
            pay(day(2026, 10, 1), USCents(20_000)),
            pay(d[3], USCents(20_000)),
        ],
        ..tax(DOLLAR * 2_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [0, 0, 20_000, 0].into_iter().enumerate() {
        assert_eq!(s[i].shortfall(), USCents(want), "installment {}", i + 1);
    }
    assert!(s[2].status() == Status::Short && s[3].status() == Status::Met);
    let p = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert_eq!(p.total(), DOLLAR * 20);
}

/// IT-2210's Dana and IT-2210A's carry examples.
#[test]
fn estimated_overpayment_makes_up_earlier_shortfalls() {
    for (name, paid, short) in [
        (
            "Dana, with $970 in September",
            [90_000, 113_000, 97_000, 100_000],
            [10_000, 0, 0, 0],
        ),
        (
            "Dana, with $969.99 in September",
            [90_000, 113_000, 96_999, 100_000],
            [10_000, 0, 1, 0],
        ),
        (
            "$100 short, then exactly $1,000",
            [90_000, 100_000, 100_000, 100_000],
            [10_000, 0, 0, 0],
        ),
        (
            "IT-2210A, $80 then $50",
            [92_000, 105_000, 100_000, 100_000],
            [8_000, 0, 0, 0],
        ),
        (
            "IT-2210A, $80 then $50, then $9.99",
            [92_000, 105_000, 99_001, 100_000],
            [8_000, 0, 999, 0],
        ),
    ] {
        let h = Household {
            payments: quarterly(&paid.map(USCents)),
            ..tax(USCents(444_444))
        };
        let s = rules().schedule(&h.facts()).unwrap().value();
        let mut want = USCents(0);
        for (i, inst) in s.iter().enumerate() {
            assert_eq!(inst.shortfall(), USCents(short[i]), "{name}: {}", i + 1);
            want += percent(10).of(USCents(short[i]));
        }
        let p = rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .value();
        assert_eq!(p.total(), want, "{name}");
    }
}

/// IB3: a payment after January 15 is returned or credited to the next year.
#[test]
fn estimated_payments_after_january_fifteen_do_not_count() {
    let mut h = Household {
        payments: quarterly(&[DOLLAR * 2_250, DOLLAR * 2_250, DOLLAR * 2_250]),
        ..tax(DOLLAR * 10_000)
    };
    let before = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    h.payments.push(pay(day(2027, 1, 16), DOLLAR * 2_250));
    let after = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(
        before.value().total() == DOLLAR * 225 && after.value().total() == before.value().total()
    );
    assert!(
        after
            .applied()
            .iter()
            .any(|r| r.applied().contains("is not a 2026 installment payment"))
    );
}

/// IT-2210 line 14: a part-year resident divides the required annual payment
/// by the installment periods of residence.
#[test]
fn estimated_part_year_resident() {
    let inp = estimated::new_residence_period("IN")
        .from(day(2026, 6, 10))
        .to(day(2026, 12, 31))
        .build()
        .unwrap();
    let before = estimated::new_residence_period("OH")
        .from(day(2026, 1, 1))
        .to(day(2026, 6, 9))
        .build()
        .unwrap();
    let mut h = Household {
        residence: vec![before, inp],
        ..tax(DOLLAR * 10_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    let want_needed = [USCents(0), USCents(0), DOLLAR * 4_500, DOLLAR * 9_000];
    let want_status = [
        Status::NothingDue,
        Status::NothingDue,
        Status::Short,
        Status::Short,
    ];
    for (i, inst) in s.iter().enumerate() {
        assert!(
            inst.needed_by() == want_needed[i] && inst.status() == want_status[i],
            "installment {}",
            i + 1
        );
    }
    let p = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert_eq!(p.total(), DOLLAR * 900);
    let why = rules().requirement(&h.facts()).unwrap().value().why();
    assert!(
        why.contains(
            "they must come to $9,000. Spread them over the 2 quarters you lived in Indiana."
        ),
        "{why}"
    );
    // A full-year resident is divided by four.
    let full = estimated::new_residence_period("IN")
        .from(day(2026, 1, 1))
        .to(day(2026, 12, 31))
        .build()
        .unwrap();
    h.residence = vec![full];
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[0].needed_by(), DOLLAR * 2_250);
}

/// IB3 credits withholding evenly unless the filer shows the dates.
#[test]
fn estimated_withholding() {
    let mut h = Household {
        withheld: DOLLAR * 8_000,
        ..tax(DOLLAR * 10_000)
    };
    let p = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert_eq!(p.total(), DOLLAR * 100);
    h.as_withheld = true;
    let p = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert_eq!(p.total(), DOLLAR * 675);
    h.withheld = DOLLAR * 9_000;
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(res.value().total().0, 0);
    assert!(!rules().requirement(&h.facts()).unwrap().value().required());

    // The pass-through credit counts with withholding, in four parts.
    let e = indianataxcalc::new_estimated_facts()
        .pass_through_entity_tax_credit(DOLLAR * 4_000)
        .build()
        .unwrap();
    let h = Household {
        extras: Some(e),
        payments: quarterly(&[DOLLAR * 1_250; 4]),
        ..tax(DOLLAR * 10_000)
    };
    let p = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert_eq!(p.total().0, 0);
}

/// IT-2210A: 22.5%, 45%, 67.5% and 90% of the annualised tax, less earlier
/// installments, capped by the regular installment plus what was carried.
#[test]
fn estimated_annualised_method() {
    let h = Household {
        annualised: Some(vec![
            USCents(0),
            DOLLAR * 2_000,
            DOLLAR * 8_000,
            DOLLAR * 10_000,
        ]),
        ..tax(DOLLAR * 10_000)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    let want = [USCents(0), DOLLAR * 900, DOLLAR * 5_400, DOLLAR * 9_000];
    for (i, inst) in s.iter().enumerate() {
        assert_eq!(inst.needed_by(), want[i], "installment {}", i + 1);
    }
    let periods: Vec<_> = [
        day(2026, 3, 31),
        day(2026, 6, 30),
        day(2026, 9, 30),
        day(2026, 12, 31),
    ]
    .into_iter()
    .map(|d| estimated::new_period().end(d).build().unwrap())
    .collect();
    let wrong = estimated::new_facts(INDIANA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(h.figures())
        .periods(&periods)
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    assert!(rules().schedule(&wrong).is_err());
}

/// Whether the federal waivers apply in Indiana is unresolved.
#[test]
fn estimated_waiver_question() {
    let retired = estimated::new_positions()
        .waiver_for(WaiverCause::Retirement, day(2026, 6, 1))
        .born(day(1962, 3, 1))
        .build()
        .unwrap();
    for (name, waiver, positions, ask, undecided) in [
        ("unanswered", None, None, true, false),
        ("none", Some(WaiverKind::None), None, false, false),
        ("a casualty", Some(WaiverKind::Casualty), None, false, true),
        ("retired in 2026 at 64", None, Some(retired), false, true),
    ] {
        let h = Household {
            waiver,
            positions,
            ..tax(DOLLAR * 10_000)
        };
        let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
        assert!(
            res.value().total() == DOLLAR * 900
                && has(&res.required(), PositionKind::PenaltyWaiver) == ask,
            "{name}"
        );
        if let Some(w) = waiver {
            assert_eq!(res.value().waiver(), w, "{name}");
        }
        let o = res.value().waiver_outcome();
        assert!(
            o.is_some() == undecided
                && o.as_ref().is_none_or(|o| o.undecided())
                && res.value().if_waived() == res.value().total(),
            "{name}"
        );
        let unsettled = res.required().iter().any(|q| {
            q.position() == PositionKind::UnsettledRule && q.effect().contains("federal waivers")
        });
        assert_eq!(unsettled, undecided, "{name}");
    }
    // No penalty, no question.
    let h = Household {
        withheld: DOLLAR * 9_000,
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(!has(&res.required(), PositionKind::PenaltyWaiver));
}

/// County tax is in the state's figure. Not given, it is asked about.
#[test]
fn estimated_county_tax() {
    let h = Household {
        no_local: true,
        ..tax(DOLLAR * 10_000)
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert_eq!(count(&res.required(), PositionKind::JurisdictionFact), 1);
    let f = estimated::new_facts(INDIANA, 2026)
        .filing_status(FilingStatus::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .local_tax(DOLLAR * 2_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    assert!(
        res.applied()
            .iter()
            .any(|r| r.applied().contains("$2000.00 of it county tax"))
    );
    // Nothing in the extras: the county question is not asked, the credit is.
    assert_eq!(count(&res.required(), PositionKind::JurisdictionFact), 1);
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    let want: HashMap<&str, [&str; 4]> = HashMap::from([
        (
            "Exactly $1,000 due after withholding",
            [
                "over $1,000 due",
                "ES-40",
                "expected under $1,000",
                "IC 6-3-4-4.1(b)",
            ],
        ),
        (
            "Last year's tax when no return was filed",
            [
                "N/A",
                "R27 / 9-25",
                "$0",
                "P.L.205-2025, SEC.10, effective January 1, 2026",
            ],
        ),
    ]);
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let rule = d.rule();
        let w = want[rule.as_str()];
        assert!(
            d.form().contains(w[0])
                && d.form_source().contains(w[1])
                && d.statute().contains(w[2])
                && d.statute_source().contains(w[3]),
            "{rule:?}"
        );
        assert!(!d.affects_household(), "{rule:?}");
        let want_uses = if rule == "Last year's tax when no return was filed" {
            Reading::Statute
        } else {
            Reading::Form
        };
        assert_eq!(d.uses(), want_uses, "{rule:?}");
    }
    let none = estimated::new_prior_return()
        .not_required_to_file()
        .build()
        .unwrap();
    for (rule, h) in [
        (
            "Exactly $1,000 due after withholding",
            Household {
                withheld: DOLLAR * 9_000,
                ..tax(DOLLAR * 10_000)
            },
        ),
        (
            "Last year's tax when no return was filed",
            Household {
                prior: Some(none),
                ..tax(DOLLAR * 10_000)
            },
        ),
    ] {
        for d in rules().requirement(&h.facts()).unwrap().divergences() {
            if d.rule() == rule {
                assert!(d.affects_household(), "{rule:?}");
            }
        }
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for year in [2025, 2027] {
        let err = indianataxcalc::estimated_tax(year).err().unwrap();
        assert!(err.is(&estimated::Error::NoRules), "{year}: {err}");
    }
    for (name, h) in [
        (
            "a farmer",
            Household {
                farm: Rate(7000),
                ..tax(USCents(100))
            },
        ),
        (
            "a fisherman",
            Household {
                fish: Rate(7000),
                ..tax(USCents(100))
            },
        ),
    ] {
        let err = rules().requirement(&h.facts()).err().unwrap();
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    let base = |j: estimated::Jurisdiction, y: i64, fig: Figures| {
        let mut b = estimated::new_facts(j, y);
        b.filing_status(FilingStatus::Single).this_year(fig);
        b
    };
    for (name, f) in [
        (
            "Pennsylvania's facts",
            base(estimated::PENNSYLVANIA, 2026, this).build().unwrap(),
        ),
        ("2027", base(INDIANA, 2027, this).build().unwrap()),
        (
            "other extras",
            base(INDIANA, 2026, this)
                .state("not Indiana's")
                .build()
                .unwrap(),
        ),
        (
            "county over the tax",
            base(
                INDIANA,
                2026,
                estimated::new_figures()
                    .tax(USCents(100))
                    .local_tax(USCents(101))
                    .build()
                    .unwrap(),
            )
            .build()
            .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        indianataxcalc::new_estimated_facts()
            .pass_through_entity_tax_credit(USCents(-1))
            .build()
            .is_err()
    );
    assert!(
        rules()
            .penalty(&tax(USCents(100)).facts(), Date::default())
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
        "$1,000",
        "flat 10%",
        "county",
        "110%",
        "$150,000",
        "February 1, 2027",
        "after January 15",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.sources().is_empty() && !e.versus_federal().is_empty());
}

/// Without this year's tax the target is last year's figure alone, and the
/// penalty figured against it is an upper bound.
#[test]
fn estimated_without_this_years_tax() {
    let mut h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 4_000, DOLLAR * 60_000),
        payments: quarterly(&[DOLLAR * 1_000, DOLLAR * 1_000, DOLLAR * 900, DOLLAR * 1_000]),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 4_000, "{}", r.why());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[2].shortfall(), DOLLAR * 100);
    let p = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!(p.total() == DOLLAR * 10 && p.upper_bound());

    h.prior = None;
    let r = rules().requirement(&h.facts()).unwrap().value();
    assert!(!r.target_known() && !r.required());
    let err = rules().schedule(&h.facts()).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
    let err = rules().penalty(&h.facts(), day(2027, 4, 15)).err().unwrap();
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
}

/// Under the $1,000 exception the target is $0, so last year's return isn't
/// asked for as a way to change it. Above it, it is.
#[test]
fn estimated_prior_return_asked_only_when_it_can_set_the_target() {
    let under = rules().requirement(&tax(DOLLAR * 800).facts()).unwrap();
    assert!(under.value().exempt() && !has(&under.required(), PositionKind::PriorYearReturn));
    let over = rules().requirement(&tax(DOLLAR * 10_000).facts()).unwrap();
    assert!(has(&over.required(), PositionKind::PriorYearReturn));
}

/// Withholding over the tax prints no minus amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let rules = rules();
    let mut asked = false;
    for a in estimatedtest::ask(&rules, &estimatedtest::households(&INDIANA)) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked = true;
        let why = a.requirement().unwrap().value().why();
        assert!(
            why.contains("pass-through credit of $3,600 leave a $600 refund."),
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
