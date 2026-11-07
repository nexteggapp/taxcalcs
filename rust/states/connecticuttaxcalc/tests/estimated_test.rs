//! Connecticut's estimated income tax rules for 2026, checked against the
//! Department of Revenue Services' documents and Conn. Gen. Stat. § 12-722.
//! The cases are hand-written and worked in their comments. (The Go test
//! that compares each figure against its extract in testdata/ checks the Go
//! package's documents, not code, and is not ported.)

#![allow(clippy::unwrap_used)]

use connecticuttaxcalc::EstimatedFacts;
use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, Payment, Positions, PriorReturn, Status,
    WaiverCause, WaiverKind, Weekday,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_connecticuttaxcalc as connecticuttaxcalc;
use working::{PositionKind, Reading, Required};

const CT: Jurisdiction = Jurisdiction::from_static("CT");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    connecticuttaxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind, contains: &str) -> bool {
    rs.iter().any(|r| {
        r.position() == k
            && r.asked()
            && (r.question().contains(contains) || r.effect().contains(contains))
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

/// One payment of each amount, in dollars, on each due date.
fn quarterly(a: &[i64]) -> Vec<Payment> {
    a.iter()
        .enumerate()
        .map(|(i, x)| pay(dues()[i], DOLLAR * *x))
        .collect()
}

fn extras(pe: USCents) -> Option<EstimatedFacts> {
    Some(
        connecticuttaxcalc::new_estimated_facts()
            .pe_tax_credit(pe)
            .build()
            .unwrap(),
    )
}

#[derive(Clone, Default)]
struct Household {
    /// CT-1040 line 14.
    tax: USCents,
    no_tax: bool,
    withheld: USCents,
    /// December 15, 2026 unless set.
    withheld_on: Date,
    as_withheld: bool,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    /// A PE Tax Credit of nothing unless set.
    extras: Option<EstimatedFacts>,
    return_filed: Date,
    /// CT-2210 Schedule A line 12, for each period.
    annualised: Option<Vec<USCents>>,
    waiver: Option<WaiverKind>,
}

fn taxed(tax: USCents, prior: Option<PriorReturn>) -> Household {
    Household {
        tax,
        prior,
        ..Household::default()
    }
}

impl Household {
    fn facts(&self) -> Facts {
        let mut figures = estimated::new_figures().tax(self.tax).build().unwrap();
        if self.no_tax {
            figures = estimated::new_figures().build().unwrap();
        }
        let mut b = estimated::new_facts(CT, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(figures)
            .paid(&self.payments)
            .return_filed(self.return_filed);
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
        match self.extras {
            Some(e) => b.state(e),
            None => b.state(extras(USCents(0)).unwrap()),
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
            for (i, tax) in ann.iter().enumerate() {
                b.periods(&[estimated::new_period()
                    .end(ends[i])
                    .annualised_tax(*tax)
                    .build()
                    .unwrap()]);
            }
            el.annualised();
        }
        b.elections(el.build().unwrap());
        if let Some(w) = self.waiver {
            b.positions(estimated::new_positions().waiver(w).build().unwrap());
        }
        b.build().unwrap()
    }
}

/// A filed 12-month return by a full-year resident.
fn prior(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .full_year_resident(true)
            .build()
            .unwrap(),
    )
}

/// Withholding over the tax reads as a refund, never as a minus amount.
#[test]
fn estimated_refund_reads_as_refund() {
    let rules = rules();
    let mut asked = false;
    for a in estimatedtest::ask(&rules, &estimatedtest::households(&estimated::CONNECTICUT)) {
        if a.household().name() != "withholding over the tax" {
            continue;
        }
        asked = true;
        let why = a.requirement().unwrap().value().why();
        assert!(
            why.contains("leaves a $600 refund. No estimated payments are needed."),
            "why {why:?}"
        );
        for x in estimatedtest::texts(Some(&rules), std::slice::from_ref(&a)) {
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

#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules()
        .schedule(&taxed(DOLLAR * 10_000, prior(DOLLAR * 20_000)).facts())
        .unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        let wd = inst.due().weekday();
        assert!(wd != Weekday::SATURDAY && wd != Weekday::SUNDAY);
        assert_eq!(inst.needed_by(), DOLLAR * 2_250 * (i as i64 + 1));
    }
    // January 31, 2027, the file-and-pay date, is a Sunday and does not
    // move.
    assert_eq!(day(2027, 1, 31).weekday(), Weekday::SUNDAY);
}

/// The $1,000 test, as the forms word it: the tax less withholding and the
/// PE Tax Credit must be $1,000 or more.
#[test]
fn estimated_threshold_boundaries() {
    for (withheld, pe, exempt) in [
        (900_001, 0, true),
        (900_000, 0, false),
        (800_000, 100_001, true),
        (800_000, 100_000, false),
    ] {
        let h = Household {
            withheld: USCents(withheld),
            extras: extras(USCents(pe)),
            ..taxed(DOLLAR * 10_000, prior(DOLLAR * 100_000))
        };
        let r = rules().requirement(&h.facts()).unwrap().value();
        assert_eq!(
            r.exempt(),
            exempt,
            "withheld {withheld}, PE {pe}: {}",
            r.why()
        );
    }
}

/// 90% of this year's tax or 100% of last year's, whichever is less, with no
/// higher percentage at any income; and when last year's is not open.
#[test]
fn estimated_target_options() {
    let r = rules()
        .requirement(&taxed(DOLLAR * 40_000, prior(DOLLAR * 50_000)).facts())
        .unwrap()
        .value();
    let c = r.chosen().unwrap();
    assert!(c.basis() == Basis::ThisYearTax && r.target() == DOLLAR * 36_000);

    // A high earner's prior-year test is still 100%.
    let rich = estimated::new_prior_return()
        .filed(true)
        .full_year_resident(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 30_000)
                .federal_agi(DOLLAR * 2_000_000)
                .state_agi(DOLLAR * 2_000_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let r = rules()
        .requirement(&taxed(DOLLAR * 40_000, Some(rich)).facts())
        .unwrap()
        .value();
    let c = r.chosen().unwrap();
    assert!(
        c.basis() == Basis::PriorYearTax
            && c.percent() == percent(100)
            && r.target() == DOLLAR * 30_000
    );

    let not_filed = estimated::new_prior_return().filed(false).build().unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(11)
        .full_year_resident(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 1_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    for (name, p, asked) in [
        ("no return on record", None, true),
        ("not filed", Some(not_filed), false),
        ("an 11-month return", Some(short), false),
    ] {
        let res = rules()
            .requirement(&taxed(DOLLAR * 40_000, p).facts())
            .unwrap();
        let r = res.value();
        let o = &r.options()[1];
        assert!(!o.available() && !o.why_not().is_empty(), "{name}");
        assert_eq!(r.target(), DOLLAR * 36_000, "{name}");
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn, ""),
            asked,
            "{name}"
        );
    }
}

/// § 12-722(j)(2): no liability last year over twelve months, as a resident
/// (CT-2210 Box F) or a nonresident with Connecticut-source income (Box G).
/// A nonresident without it must pay 90% of this year's tax (Box E).
#[test]
fn estimated_no_liability_exception() {
    let not_required = |resident: Option<bool>, months: i64| {
        let mut b = estimated::new_prior_return();
        b.not_required_to_file().months(months);
        if let Some(r) = resident {
            b.full_year_resident(r);
        }
        Some(b.build().unwrap())
    };
    let sourced = |had: bool| {
        Some(
            connecticuttaxcalc::new_estimated_facts()
                .pe_tax_credit(USCents(0))
                .prior_year_connecticut_source_income(had)
                .build()
                .unwrap(),
        )
    };
    let none = PositionKind::Unspecified;
    for (name, p, extras, exempt, ask, says) in [
        (
            "Box F, a resident",
            not_required(Some(true), 12),
            None,
            true,
            none,
            "",
        ),
        (
            "Box G, Connecticut-source income",
            not_required(Some(false), 12),
            sourced(true),
            true,
            none,
            "",
        ),
        (
            "Box E, no Connecticut-source income",
            not_required(Some(false), 12),
            sourced(false),
            false,
            none,
            "",
        ),
        (
            "source income not given",
            not_required(Some(false), 12),
            None,
            false,
            PositionKind::JurisdictionFact,
            "Did you have any Connecticut income in 2025?",
        ),
        (
            "residence not given",
            not_required(None, 12),
            None,
            false,
            PositionKind::Residency,
            "Did you live in Connecticut all of 2025",
        ),
        (
            "a short year",
            not_required(Some(true), 11),
            None,
            false,
            none,
            "",
        ),
        (
            "a filed return showing no tax",
            prior(USCents(0)),
            None,
            true,
            none,
            "",
        ),
    ] {
        let h = Household {
            extras,
            ..taxed(DOLLAR * 40_000, p)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        let r = res.value();
        assert_eq!(r.exempt(), exempt, "{name}: {}", r.why());
        if !exempt && name != "a filed return showing no tax" {
            assert_eq!(r.target(), DOLLAR * 36_000, "{name}");
        }
        if ask != none {
            assert!(
                has(&res.required(), ask, says),
                "{name}: {ask} is not asked"
            );
        }
        if name == "Box E, no Connecticut-source income" {
            let o = &r.options()[1];
            assert!(
                !o.available()
                    && o.amount().0 == 0
                    && o.why_not().contains("no Connecticut income in 2025"),
                "{name}"
            );
        }
        if name == "Box F, a resident" {
            // The exception is the chosen option, at $0.
            let c = r.chosen().unwrap();
            assert!(
                c.basis() == Basis::Exception
                    && c.amount().0 == 0
                    && c.label() == "No Connecticut tax last year"
                    && r.target().0 == 0,
                "{name}"
            );
        }
        if exempt {
            let p = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
            assert!(
                p.value().total().0 == 0 && p.value().lines().is_empty(),
                "{name}"
            );
        }
    }
}

/// The interest is 1% for each month or part of a month, the months running
/// from the 16th to the 15th, and stops on April 15, 2027. Each case is
/// $40,000 of tax and a $50,000 prior year, so $9,000 is due each quarter.
#[test]
fn estimated_interest_by_the_month() {
    let last = prior(DOLLAR * 50_000);

    // September is $5,000 short, and nothing more is paid until $9,000 on
    // January 15: $200 on column C and $150 on column D.
    let h = Household {
        payments: quarterly(&[9_000, 9_000, 4_000, 9_000]),
        ..taxed(DOLLAR * 40_000, last)
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    let p = res.value();
    assert!(p.total() == DOLLAR * 350 && p.lines().len() == 2);
    for (i, (installment, months, total, days)) in
        [(3, 4, DOLLAR * 200, 122), (4, 3, DOLLAR * 150, 90)]
            .into_iter()
            .enumerate()
    {
        let l = &p.lines()[i];
        assert!(
            l.installment() == installment
                && l.shortfall() == DOLLAR * 5_000
                && l.parts().len() == months
                && l.total() == total
                && l.days() == days,
            "line {}",
            i + 1
        );
        for part in l.parts() {
            assert!(
                part.rate() == percent(1)
                    && part.months() == 1
                    && part.principal() == DOLLAR * 5_000
                    && part.charge() == DOLLAR * 50,
                "line {} part {} to {}",
                i + 1,
                part.from(),
                part.to()
            );
        }
    }
    assert!(!has(&res.required(), PositionKind::UnpublishedRate, ""));

    // A part of a month is a whole one.
    let d = dues();
    for (paid, want) in [
        (day(2026, 4, 15), USCents(0)),
        (day(2026, 4, 16), DOLLAR * 90),
        (day(2026, 5, 15), DOLLAR * 90),
        (day(2026, 5, 16), DOLLAR * 180),
    ] {
        let h = Household {
            payments: vec![
                pay(paid, DOLLAR * 9_000),
                pay(d[1], DOLLAR * 9_000),
                pay(d[2], DOLLAR * 9_000),
                pay(d[3], DOLLAR * 9_000),
            ],
            ..taxed(DOLLAR * 40_000, last)
        };
        let got = rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .value()
            .total();
        assert_eq!(got, want, "April paid {paid}");
    }

    // A payment during a month lowers the next month's charge, not that
    // month's: $100.
    let mut payments = quarterly(&[9_000, 9_000, 4_000, 9_000]);
    payments.push(pay(day(2026, 10, 20), DOLLAR * 5_000));
    let h = Household {
        payments,
        ..taxed(DOLLAR * 40_000, last)
    };
    let p = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!(p.total() == DOLLAR * 100 && p.lines().len() == 1 && p.lines()[0].days() == 35);

    // Figured to a date: nothing on April 15, a month from April 16.
    let unpaid = taxed(DOLLAR * 40_000, last);
    for (as_of, want) in [
        (day(2026, 4, 15), USCents(0)),
        (day(2026, 4, 16), DOLLAR * 90),
        (day(2026, 6, 15), DOLLAR * 180),
    ] {
        let got = rules()
            .penalty(&unpaid.facts(), as_of)
            .unwrap()
            .value()
            .total();
        assert_eq!(got, want, "unpaid, to {as_of}");
    }
    // Never beyond April 15, 2027.
    let got = rules()
        .penalty(&unpaid.facts(), day(2027, 6, 30))
        .unwrap()
        .value()
        .as_of();
    assert_eq!(got, day(2027, 4, 15));
}

/// § 12-722(l): a return filed and paid by January 31 takes the interest off
/// the January installment. CT-2210 skips Worksheet D, so an earlier
/// shortfall still unpaid is not charged after January 15 either.
#[test]
fn estimated_return_filed_and_paid() {
    let last = prior(DOLLAR * 50_000);
    let payments = quarterly(&[9_000, 9_000, 4_000]);

    let h = Household {
        payments: payments.clone(),
        return_filed: day(2027, 1, 25),
        ..taxed(DOLLAR * 40_000, last)
    };
    let s = rules().schedule(&h.facts()).unwrap().value();
    assert_eq!(s[3].status(), Status::Excused);
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(res.value().total() == DOLLAR * 200 && res.value().as_of() == day(2027, 1, 25));
    for d in res.divergences() {
        assert_eq!(
            d.affects_household(),
            d.rule() == "A return filed and paid by Jan 31, 2027",
            "{:?}",
            d.rule()
        );
    }

    // January 31, a Sunday, is the last day; February 1 is too late.
    for (filed, excused, want) in [
        (day(2027, 1, 31), true, DOLLAR * 200),
        (day(2027, 2, 1), false, DOLLAR * 340),
    ] {
        let h = Household {
            payments: payments.clone(),
            return_filed: filed,
            ..taxed(DOLLAR * 40_000, last)
        };
        let got = rules().schedule(&h.facts()).unwrap().value()[3].status() == Status::Excused;
        assert_eq!(got, excused, "filed {filed}");
        let got = rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .value()
            .total();
        assert_eq!(got, want, "filed {filed}");
    }
}

/// Withholding is spread in four equal parts unless the filer shows the
/// dates it was withheld (§ 12-722(k), CT-2210 Box C): $720 spread, $360 on
/// the date withheld.
#[test]
fn estimated_withholding_on_the_dates_withheld() {
    let mut h = Household {
        withheld: DOLLAR * 18_000,
        withheld_on: day(2026, 1, 5),
        ..taxed(DOLLAR * 30_000, prior(DOLLAR * 40_000))
    };
    let got = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value()
        .total();
    assert_eq!(got, DOLLAR * 720, "spread");
    h.as_withheld = true;
    let got = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value()
        .total();
    assert_eq!(got, DOLLAR * 360, "as withheld");
}

/// The PE Tax Credit counts like withholding, a quarter by each date even
/// under Box C, and the open question about its statutory basis is reported.
#[test]
fn estimated_pe_tax_credit() {
    for as_withheld in [false, true] {
        let h = Household {
            extras: extras(DOLLAR * 4_000),
            payments: quarterly(&[3_500, 3_500, 3_500, 3_500]),
            as_withheld,
            ..taxed(DOLLAR * 20_000, prior(DOLLAR * 30_000))
        };
        let sched = rules().schedule(&h.facts()).unwrap();
        for (i, inst) in sched.value().iter().enumerate() {
            assert!(
                inst.credited_by() == DOLLAR * 4_500 * (i as i64 + 1)
                    && inst.status() == Status::Met,
                "Box C {as_withheld}: installment {}",
                i + 1
            );
        }
        assert!(has(
            &sched.required(),
            PositionKind::UnsettledRule,
            "PE Tax Credit as a payment on each due date"
        ));
        let got = rules()
            .penalty(&h.facts(), day(2027, 4, 15))
            .unwrap()
            .value()
            .total();
        assert_eq!(got.0, 0, "Box C {as_withheld}");
    }

    // The credit takes $4,800 of tax under $1,000 on the forms, which the
    // statute's words would not; the exemption follows the forms and says
    // so.
    let res = rules()
        .requirement(
            &Household {
                extras: extras(DOLLAR * 3_900),
                ..taxed(DOLLAR * 4_800, prior(DOLLAR * 10_000))
            }
            .facts(),
        )
        .unwrap();
    assert!(res.value().exempt());
    assert!(has(
        &res.required(),
        PositionKind::UnsettledRule,
        "PE Tax Credit in the $1,000 test"
    ));
    let c = res.value().chosen().unwrap();
    assert!(
        c.basis() == Basis::Exception
            && c.amount().0 == 0
            && c.label() == "Tax less withholding and PE Tax Credit under $1,000"
    );
    // Both PE Tax Credit divergences change this household's answer.
    for d in res.divergences() {
        assert_eq!(
            d.affects_household(),
            d.rule().starts_with("The PE Tax Credit"),
            "{:?}",
            d.rule()
        );
    }

    // Not given: none is assumed, and it is asked for.
    let f = estimated::new_facts(CT, 2026)
        .filing_status(filing::Status::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 20_000)
                .build()
                .unwrap(),
        )
        .build()
        .unwrap();
    let res = rules().requirement(&f).unwrap();
    assert!(has(
        &res.required(),
        PositionKind::JurisdictionFact,
        "Pass-Through Entity Tax Credit"
    ));
}

/// Schedule A: 22.5%, 45%, 67.5% and 90% of the annualised tax, less earlier
/// installments, never more than the regular $9,000 plus what earlier
/// installments saved.
#[test]
fn estimated_annualised_recapture() {
    let ann = vec![
        DOLLAR * 8_000,
        DOLLAR * 16_000,
        DOLLAR * 30_000,
        DOLLAR * 40_000,
    ];
    let h = Household {
        annualised: Some(ann.clone()),
        ..taxed(DOLLAR * 40_000, prior(DOLLAR * 50_000))
    };
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [
        DOLLAR * 1_800,
        DOLLAR * 7_200,
        DOLLAR * 20_250,
        DOLLAR * 36_000,
    ]
    .into_iter()
    .enumerate()
    {
        assert_eq!(sched.value()[i].needed_by(), want, "installment {}", i + 1);
    }
    // Without this year's tax, the 90% binds the fourth installment.
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 50_000),
        annualised: Some(ann),
        ..Household::default()
    };
    let got = rules().schedule(&h.facts()).unwrap().value()[3].needed_by();
    assert_eq!(got, DOLLAR * 36_000);
    // A period that does not end where Schedule A's does is refused.
    let periods: Vec<_> = [
        day(2026, 2, 28),
        day(2026, 4, 30),
        day(2026, 7, 31),
        day(2026, 11, 30),
    ]
    .into_iter()
    .map(|e| estimated::new_period().end(e).build().unwrap())
    .collect();
    let bad = estimated::new_facts(CT, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .periods(&periods)
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    assert!(rules().requirement(&bad).is_err());
}

/// Last year's overpayment applied forward counts on April 15, and the
/// question of its date is reported.
#[test]
fn estimated_prior_year_overpayment() {
    let applied = estimated::new_payment()
        .amount(DOLLAR * 9_000)
        .from_prior_year()
        .build()
        .unwrap();
    let d = dues();
    let h = Household {
        payments: vec![
            applied,
            pay(d[1], DOLLAR * 9_000),
            pay(d[2], DOLLAR * 9_000),
            pay(d[3], DOLLAR * 9_000),
        ],
        ..taxed(DOLLAR * 40_000, prior(DOLLAR * 50_000))
    };
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(res.value().total().0 == 0);
    assert!(has(
        &res.required(),
        PositionKind::UnsettledRule,
        "overpayment"
    ));
}

/// Connecticut has no casualty, retirement or disability waiver. A filer who
/// asks is told so and the interest is not reduced; one who does not is told
/// the question of other chapter 229 provisions is open.
#[test]
fn estimated_no_waiver() {
    let mut h = taxed(DOLLAR * 40_000, prior(DOLLAR * 50_000));
    let res = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert!(has(
        &res.required(),
        PositionKind::PenaltyWaiver,
        "interest shown stands unless another law lets the department waive it"
    ));
    h.waiver = Some(WaiverKind::Casualty);
    let asked = rules().penalty(&h.facts(), day(2027, 4, 15)).unwrap();
    assert_eq!(asked.value().total(), res.value().total());
    assert!(!has(&asked.required(), PositionKind::PenaltyWaiver, ""));
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules()
        .requirement(&taxed(DOLLAR * 10_000, None).facts())
        .unwrap();
    let want = [
        (
            "The PE Tax Credit in the $1,000 test",
            [
                "subtracts withholding and the PE Tax Credit",
                "Part 2 line 4",
                "reduced by the tax withheld",
                "§ 12-722(j)(1)",
            ],
        ),
        (
            "The PE Tax Credit as a payment by each due date",
            [
                "a quarter of the PE Tax Credit as paid by each due date",
                "Part 3 lines 12 to 13",
                "only tax withheld",
                "§ 12-722(k)",
            ],
        ),
        (
            "A return filed and paid by Jan 31, 2027",
            [
                "no interest after Jan 15, 2027 on any installment",
                "Do not complete Schedule B, Worksheet D",
                "only the fourth installment's underpayment",
                "§ 12-722(b), (l)",
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
            "{:?} does not pin both readings",
            d.rule()
        );
        // The figures follow the forms, and with no PE Tax Credit and no
        // early return neither reading changes anything.
        assert!(
            d.uses() == Reading::Form && !d.affects_household(),
            "{:?}",
            d.rule()
        );
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for y in [2025, 2027] {
        let Err(err) = connecticuttaxcalc::estimated_tax(y) else {
            panic!("{y}: rules returned");
        };
        assert!(err.is(&estimated::Error::NoRules), "{y}: {err}");
    }
    let this = estimated::new_figures().tax(USCents(100)).build().unwrap();
    for (name, fish) in [("a farmer", false), ("a fisherman", true)] {
        let mut b = estimated::new_facts(CT, 2026);
        b.filing_status(filing::Status::Single).this_year(this);
        if fish {
            b.fish_share(Rate(7_000));
        } else {
            b.farm_share(Rate(7_000));
        }
        let Err(err) = rules().requirement(&b.build().unwrap()) else {
            panic!("{name}: accepted");
        };
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
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
            estimated::new_facts(CT, 2027)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .build()
                .unwrap(),
        ),
        (
            "other extras",
            estimated::new_facts(CT, 2026)
                .filing_status(filing::Status::Single)
                .this_year(this)
                .state("not Connecticut's")
                .build()
                .unwrap(),
        ),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
        assert!(rules().penalty(&f, day(2027, 4, 15)).is_err(), "{name}");
    }
    assert!(
        connecticuttaxcalc::new_estimated_facts()
            .pe_tax_credit(USCents(-1))
            .build()
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
        "$1,000",
        "1% a month",
        "part of a month",
        "16th to the 15th",
        "no 110%",
        "Pass-Through Entity Tax Credit",
        "no waiver",
        "interest rather than a penalty",
        "January 31",
        "April 15",
    ] {
        assert!(all.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.sources().is_empty() && !e.versus_federal().is_empty());
}

/// Without this year's tax, the target is 100% of last year's, and the
/// interest figured against it is the most it can be: $2,400.
#[test]
fn estimated_without_this_years_tax() {
    let h = Household {
        no_tax: true,
        prior: prior(DOLLAR * 30_000),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 30_000, "{}", r.why());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax, ""));
    let pen = rules()
        .penalty(&h.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!(pen.total() == DOLLAR * 2_400 && pen.upper_bound());

    // No prior year either: no target, and nothing to schedule.
    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let r = rules().requirement(&none.facts()).unwrap().value();
    assert!(!r.target_known() && !r.required());
    let Err(err) = rules().schedule(&none.facts()) else {
        panic!("schedule accepted");
    };
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
    let Err(err) = rules().penalty(&none.facts(), day(2027, 4, 15)) else {
        panic!("interest accepted");
    };
    assert!(err.is(&estimated::Error::NeedsThisYearTax));

    // The no-liability exception needs nothing about this year, and its zero
    // is exact.
    let box_f = estimated::new_prior_return()
        .not_required_to_file()
        .full_year_resident(true)
        .build()
        .unwrap();
    let exempt = Household {
        no_tax: true,
        prior: Some(box_f),
        ..Household::default()
    };
    let p = rules()
        .penalty(&exempt.facts(), day(2027, 4, 15))
        .unwrap()
        .value();
    assert!(p.total().0 == 0 && !p.upper_bound());
}

/// An exception sets the target, so no question may say its answer changes
/// the target. $500 of tax, with last year's return not given.
#[test]
fn estimated_exception_asks_nothing_of_the_target() {
    let f = estimated::new_facts(CT, 2026)
        .filing_status(filing::Status::Single)
        .this_year(estimated::new_figures().tax(DOLLAR * 500).build().unwrap())
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
fn waiver_penalty(pos: Positions) -> estimated::Penalty {
    let f = estimated::new_facts(CT, 2026)
        .filing_status(filing::Status::Single)
        .this_year(
            estimated::new_figures()
                .tax(DOLLAR * 10_000)
                .build()
                .unwrap(),
        )
        .positions(pos)
        .build()
        .unwrap();
    rules().penalty(&f, day(2027, 4, 15)).unwrap().value()
}

fn waiver_for(c: WaiverCause, on: Date, born: Date) -> Positions {
    estimated::new_positions()
        .waiver_for(c, on)
        .born(born)
        .build()
        .unwrap()
}

/// Connecticut gives no waiver of the interest, so each is refused,
/// including a retirement the federal waiver would cover. The penalty stays
/// the figure before any waiver.
#[test]
fn estimated_waiver_outcomes() {
    let before = waiver_penalty(
        estimated::new_positions()
            .waiver(WaiverKind::None)
            .build()
            .unwrap(),
    )
    .total();
    assert_ne!(before.0, 0, "no penalty arises, so no waiver is decided");
    let none = Date::default();
    for (name, pos, reason) in [
        (
            "retired in 2026 at 64",
            waiver_for(WaiverCause::Retirement, day(2026, 6, 1), day(1962, 3, 1)),
            "no waiver of the interest for retirement or disability",
        ),
        (
            "retired in 2020",
            waiver_for(WaiverCause::Retirement, day(2020, 6, 1), day(1955, 3, 1)),
            "retirement or disability",
        ),
        (
            "disabled in 2026",
            waiver_for(WaiverCause::Disability, day(2026, 2, 1), none),
            "retirement or disability",
        ),
        (
            "a casualty",
            waiver_for(WaiverCause::Casualty, day(2026, 8, 1), none),
            "for a casualty or disaster",
        ),
        (
            "a disaster",
            waiver_for(WaiverCause::Disaster, day(2026, 8, 1), none),
            "for a casualty or disaster",
        ),
    ] {
        let p = waiver_penalty(pos);
        let Some(o) = p.waiver_outcome() else {
            panic!("{name}: no waiver outcome");
        };
        assert!(
            o.refused() && o.reason().contains(reason),
            "{name}: {:?}",
            o.reason()
        );
        assert!(p.total() == before && p.if_waived() == p.total(), "{name}");
    }
}
