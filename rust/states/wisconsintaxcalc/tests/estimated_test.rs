//! Wisconsin's estimated tax rules for 2026, checked against the Department
//! of Revenue's own documents and Chapter 71 of the statutes.
//!
//! Wisconsin publishes no exhaustive oracle for estimated tax. The strongest
//! checks are the Schedule U instructions' three worked examples of carrying
//! an overpayment forward and back, whose published answers fix the order
//! payments are applied in. The interest arithmetic, the exceptions and the
//! boundaries are hand-written cases. (The Go tests that only compare
//! figures against the extracts in testdata/ check the Go package's
//! documents, not code, and are not ported; the calendar and rate checks
//! are unit tests in src/export_test.rs.)

#![allow(clippy::unwrap_used)]

use estimated::{
    Basis, Date, EstimatedTax, Facts, Jurisdiction, Month, Payment, Penalty, PenaltyLine,
    Positions, PriorReturn, Status, WaiverCause, WaiverKind, WaiverOutcome, Withheld,
};
use money::{DOLLAR, Rate, USCents, percent};
use taxcalcs_wisconsintaxcalc as wisconsintaxcalc;
use wisconsintaxcalc::EstimatedFacts;
use working::{PositionKind, Reading, Required, Rule};

const WI: Jurisdiction = Jurisdiction::from_static("WI");

fn day(y: i64, m: i64, d: i64) -> Date {
    estimated::must_date(y, Month(m), d)
}

fn rules() -> Box<dyn EstimatedTax> {
    wisconsintaxcalc::estimated_tax(2026).unwrap()
}

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

fn applied(rs: &[Rule], s: &str) -> bool {
    rs.iter()
        .any(|r| r.applied().contains(s) || r.cite().contains(s))
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

#[derive(Clone, Default)]
struct Household {
    /// Schedule U line 3: net tax less refundable credits.
    tax: USCents,
    no_tax: bool,
    withheld: Vec<Withheld>,
    prior: Option<PriorReturn>,
    payments: Vec<Payment>,
    extras: Option<EstimatedFacts>,
    first_met: Date,
    return_filed: Date,
    /// Schedule U line 41 for each period.
    annualised: Option<Vec<USCents>>,
    as_withheld: bool,
    waiver: Option<WaiverKind>,
    /// Replaces waiver when set.
    positions: Option<Positions>,
}

impl Household {
    fn facts(&self) -> Facts {
        let mut fig = estimated::new_figures().tax(self.tax).build().unwrap();
        if self.no_tax {
            fig = estimated::new_figures().build().unwrap();
        }
        let mut b = estimated::new_facts(WI, 2026);
        b.filing_status(filing::Status::Single)
            .this_year(fig)
            .withheld(&self.withheld)
            .paid(&self.payments)
            .requirement_first_met(self.first_met)
            .return_filed(self.return_filed);
        if let Some(p) = self.prior {
            b.prior_year(p);
        }
        match self.extras {
            Some(e) => b.state(e),
            None => b.state(
                wisconsintaxcalc::new_estimated_facts()
                    .prior_year_farmer_or_fisher(false)
                    .federal_disaster_extension(false)
                    .active_duty_outside_us(false)
                    .build()
                    .unwrap(),
            ),
        };
        let mut el = estimated::new_elections();
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
        if self.as_withheld {
            el.withholding_as_withheld();
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
}

fn withheld(on: Date, a: USCents) -> Withheld {
    estimated::new_withheld().on(on).amount(a).build().unwrap()
}

/// A filed 12-month 2025 return as a full-year resident.
fn last(tax: USCents) -> Option<PriorReturn> {
    Some(
        estimated::new_prior_return()
            .filed(true)
            .figures(estimated::new_figures().tax(tax).build().unwrap())
            .full_year_resident(true)
            .build()
            .unwrap(),
    )
}

/// A household whose required annual payment is $4,000, last year's tax, so
/// $1,000 is due by each date: 90% of this year's $10,000 is $9,000, more
/// than $4,000.
fn quarters(payments: Vec<Payment>) -> Household {
    Household {
        tax: DOLLAR * 10_000,
        prior: last(DOLLAR * 4_000),
        payments,
        ..Household::default()
    }
}

fn taxed(tax: USCents) -> Household {
    Household {
        tax,
        ..Household::default()
    }
}

fn penalty_of(h: &Household, as_of: Date) -> working::Result<Penalty> {
    rules().penalty(&h.facts(), as_of).unwrap()
}

fn line_for(p: &Penalty, installment: i64) -> Option<PenaltyLine> {
    p.lines()
        .into_iter()
        .find(|l| l.installment() == installment)
}

/// The 2026 dates are the ones the 1-ES prints, a quarter of $4,000 by each.
#[test]
fn estimated_due_dates_for_twenty_twenty_six() {
    let res = rules().schedule(&quarters(vec![]).facts()).unwrap();
    for (i, inst) in res.value().iter().enumerate() {
        assert_eq!(inst.due(), dues()[i]);
        assert_eq!(inst.needed_by(), DOLLAR * 1_000 * (i as i64 + 1));
    }
}

/// The $500 threshold, one cent either side.
#[test]
fn estimated_threshold_boundaries() {
    for (w, exempt) in [(10_001, true), (10_000, false)] {
        let h = Household {
            withheld: vec![withheld(day(2026, 6, 1), USCents(w))],
            ..taxed(DOLLAR * 600)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(
            res.value().exempt() == exempt && res.value().required() != exempt,
            "withheld {w}: {}",
            res.value().why()
        );
        let c = res.value().chosen().unwrap_or_default();
        let is_exception = c.basis() == Basis::Exception
            && c.amount().0 == 0
            && c.label() == "Tax less withholding under $500";
        assert_eq!(is_exception, exempt, "withheld {w}");
        assert!(!res.value().why().contains("$500, which is under $500"));
        let p = penalty_of(&h, day(2027, 4, 15));
        assert_eq!(p.value().total().0 == 0, exempt, "withheld {w}");
    }
}

/// The target is the smaller of 90% of this year's tax and 100% of last
/// year's. Last year's needs a filed 12-month return, and there is no 110%.
#[test]
fn estimated_target_options() {
    let res = rules().requirement(&quarters(vec![]).facts()).unwrap();
    let c = res.value().chosen().unwrap();
    assert!(
        c.basis() == Basis::PriorYearTax
            && c.percent() == percent(100)
            && res.value().target() == DOLLAR * 4_000
    );
    let o = &res.value().options()[0];
    assert!(o.amount() == DOLLAR * 9_000 && o.percent() == percent(90));

    let mut h = quarters(vec![]);
    h.tax = DOLLAR * 4_000;
    assert_eq!(
        rules().requirement(&h.facts()).unwrap().value().target(),
        DOLLAR * 3_600
    );

    // High income last year: still 100%.
    let rich = estimated::new_prior_return()
        .filed(true)
        .figures(
            estimated::new_figures()
                .tax(DOLLAR * 4_000)
                .federal_agi(DOLLAR * 5_000_000)
                .state_agi(DOLLAR * 5_000_000)
                .build()
                .unwrap(),
        )
        .full_year_resident(true)
        .build()
        .unwrap();
    let mut h = quarters(vec![]);
    h.prior = Some(rich);
    assert_eq!(
        rules().requirement(&h.facts()).unwrap().value().target(),
        DOLLAR * 4_000
    );

    let not_filed = estimated::new_prior_return()
        .filed(false)
        .full_year_resident(true)
        .build()
        .unwrap();
    let short = estimated::new_prior_return()
        .filed(true)
        .months(6)
        .figures(estimated::new_figures().tax(USCents(100)).build().unwrap())
        .full_year_resident(true)
        .build()
        .unwrap();
    for (name, p, ask) in [
        ("not filed", Some(not_filed), false),
        ("short year", Some(short), false),
        ("none given", None, true),
    ] {
        let h = Household {
            prior: p,
            ..taxed(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert!(
            !res.value().options()[1].available() && res.value().target() == DOLLAR * 9_000,
            "{name}"
        );
        assert_eq!(
            has(&res.required(), PositionKind::PriorYearReturn),
            ask,
            "{name}"
        );
    }
}

/// Without this year's tax the target is last year's tax alone, the
/// installments are measured against it, and the interest is an upper
/// bound. With no prior year either there is no target. An exception that
/// needs nothing about this year is still exact.
#[test]
fn estimated_without_this_years_tax() {
    let mut h = quarters(vec![]);
    h.no_tax = true;
    let res = rules().requirement(&h.facts()).unwrap();
    let r = res.value();
    assert!(r.required() && r.target() == DOLLAR * 4_000 && r.target_known());
    let o = &r.options()[0];
    assert!(!o.available() && o.why_not() == estimated::NEEDS_THIS_YEAR_TAX);
    assert!(has(&res.required(), PositionKind::ThisYearTax));
    let sched = rules().schedule(&h.facts()).unwrap();
    for (i, inst) in sched.value().iter().enumerate() {
        assert_eq!(inst.needed_by(), DOLLAR * 1_000 * (i as i64 + 1));
    }
    let p = penalty_of(&h, day(2027, 4, 15));
    assert!(p.value().upper_bound() && p.value().total() == USCents(31_924));

    let none = Household {
        no_tax: true,
        ..Household::default()
    };
    let res = rules().requirement(&none.facts()).unwrap();
    assert!(!res.value().target_known() && !res.value().required());
    let Err(err) = rules().schedule(&none.facts()) else {
        panic!("schedule accepted");
    };
    assert!(err.is(&estimated::Error::NeedsThisYearTax));
    let Err(err) = rules().penalty(&none.facts(), day(2027, 4, 15)) else {
        panic!("penalty accepted");
    };
    assert!(err.is(&estimated::Error::NeedsThisYearTax));

    let zero = Household {
        no_tax: true,
        prior: last(USCents(0)),
        ..Household::default()
    };
    let p = penalty_of(&zero, day(2027, 4, 15));
    assert!(p.value().total().0 == 0 && !p.value().upper_bound());
}

/// Schedule U lines 26 to 28: each shortfall × days ÷ 365 × .12, to the
/// cent: $120.00, $99.95, $69.70 and $29.59. The 2025 short method prints
/// .0798 for the same thing in one step, which on $4,000 gives $319.20.
#[test]
fn estimated_interest_arithmetic() {
    let p = penalty_of(&quarters(vec![]), day(2027, 4, 15)).value();
    let want_days = [365, 304, 212, 90];
    let want_charge = [12_000, 9_995, 6_970, 2_959];
    for i in 0..4 {
        let l = line_for(&p, i as i64 + 1).unwrap();
        let parts = l.parts();
        assert!(
            parts.len() == 1
                && parts[0].days() == want_days[i]
                && parts[0].charge() == USCents(want_charge[i])
                && parts[0].rate() == percent(12),
            "line {}",
            i + 1
        );
    }
    assert_eq!(p.total(), USCents(31_924));
    let short = USCents(4_000 * 798 / 100);
    let d = p.total() - short;
    assert!(d.0 >= 0 && d.0 <= 10);
}

/// A shortfall that runs from September 2026 into 2027 is one span at 12%,
/// and nothing is unrated however late `as_of` is.
#[test]
fn estimated_interest_across_the_year_end_is_one_span() {
    let d = dues();
    let h = quarters(vec![
        pay(d[0], DOLLAR * 1_000),
        pay(d[1], DOLLAR * 1_000),
        pay(d[3], DOLLAR * 2_000),
    ]);
    let res = penalty_of(&h, day(2027, 12, 31));
    let p = res.value();
    assert_eq!(p.as_of(), day(2027, 4, 15));
    let l = line_for(&p, 3).unwrap();
    assert_eq!(p.lines().len(), 1);
    // January's $2,000 covers January first; the $1,000 over goes back to
    // September on January 15: 122 days, $40.11.
    let parts = l.parts();
    assert!(
        parts.len() == 1
            && parts[0].from() == d[2]
            && parts[0].to() == d[3]
            && parts[0].days() == 122
            && parts[0].charge() == USCents(4_011)
    );
    assert!(!has(&res.required(), PositionKind::UnpublishedRate));
}

/// A piece of a penalty line: principal, paid on, days and charge in cents.
type Part = (USCents, Date, i64, i64);

/// A worked example: its name, the payments, each installment's shortfall
/// in dollars, each line's parts, and the total in cents.
type ExampleCase = (
    &'static str,
    Vec<Payment>,
    [i64; 4],
    Vec<(i64, Vec<Part>)>,
    i64,
);

/// The Schedule U instructions' three worked examples, with $1,000 due each
/// date. Their published answers are what lines 22 and 23 carry.
#[test]
fn estimated_schedule_u_examples() {
    let stop = day(2027, 4, 15);
    let d = dues();
    let cases: [ExampleCase; 3] = [
        (
            "carryforward",
            quarterly(&[2_000, 600, 600, 600]),
            [0, 0, 0, 200],
            vec![(4, vec![(DOLLAR * 200, stop, 90, 592)])],
            592,
        ),
        (
            "carryback",
            quarterly(&[500, 500, 500, 2_200]),
            [500, 500, 500, 0],
            vec![
                (
                    1,
                    vec![
                        (DOLLAR * 200, d[3], 275, 1_808),
                        (DOLLAR * 300, stop, 365, 3_600),
                    ],
                ),
                (2, vec![(DOLLAR * 500, d[3], 214, 3_518)]),
                (3, vec![(DOLLAR * 500, d[3], 122, 2_005)]),
            ],
            10_931,
        ),
        (
            "carryforward and carryback",
            quarterly(&[1_500, 400, 1_500, 400]),
            [0, 100, 0, 200],
            vec![
                (2, vec![(DOLLAR * 100, d[2], 92, 302)]),
                (4, vec![(DOLLAR * 200, stop, 90, 592)]),
            ],
            894,
        ),
    ];
    for (name, payments, shortfall, parts, total) in cases {
        let h = quarters(payments);
        let sched = rules().schedule(&h.facts()).unwrap().value();
        for (i, inst) in sched.iter().enumerate() {
            assert_eq!(
                inst.shortfall(),
                DOLLAR * shortfall[i],
                "{name}: installment {}",
                i + 1
            );
        }
        let p = penalty_of(&h, stop).value();
        assert_eq!(p.lines().len(), parts.len(), "{name}");
        for (n, want) in &parts {
            let l = line_for(&p, *n).unwrap();
            let got = l.parts();
            assert_eq!(got.len(), want.len(), "{name}: installment {n}");
            for (i, w) in want.iter().enumerate() {
                let g = &got[i];
                assert!(
                    g.principal() == w.0
                        && g.to() == w.1
                        && g.days() == w.2
                        && g.charge() == USCents(w.3),
                    "{name}: installment {n} part {} is {} to {}, {} days, {}",
                    i + 1,
                    g.principal(),
                    g.to(),
                    g.days(),
                    g.charge()
                );
            }
        }
        assert_eq!(p.total(), USCents(total), "{name}");
    }
}

/// A payment after January 15 goes to the January shortfall first, then
/// back to September, never to April first.
#[test]
fn estimated_late_payment_goes_to_january_first() {
    let h = quarters(vec![pay(day(2027, 2, 10), DOLLAR * 1_500)]);
    let p = penalty_of(&h, day(2027, 4, 15)).value();
    let jan = line_for(&p, 4).unwrap().parts();
    assert!(
        jan.len() == 1
            && jan[0].principal() == DOLLAR * 1_000
            && jan[0].to() == day(2027, 2, 10)
            && jan[0].days() == 26
            && jan[0].charge() == USCents(855)
    );
    let sep = line_for(&p, 3).unwrap().parts();
    assert!(
        sep.len() == 2 && sep[0].principal() == DOLLAR * 500 && sep[0].to() == day(2027, 2, 10)
    );
    let apr = line_for(&p, 1).unwrap().parts();
    assert!(apr.len() == 1 && apr[0].to() == day(2027, 4, 15));
}

/// A payment counts first toward its own period. A June 1 payment does not
/// pay April's shortfall until June's own installment is covered.
#[test]
fn estimated_payment_covers_its_own_period_first() {
    let d = dues();
    let h = quarters(vec![
        pay(day(2026, 6, 1), DOLLAR * 1_500),
        pay(d[2], DOLLAR * 1_000),
        pay(d[3], DOLLAR * 1_000),
    ]);
    let sched = rules().schedule(&h.facts()).unwrap().value();
    assert!(sched[0].shortfall() == DOLLAR * 1_000 && sched[1].shortfall().0 == 0);
    let p = penalty_of(&h, day(2027, 4, 15)).value();
    // $500 of April paid June 1, 47 days, $7.73; $500 never paid, $60.00.
    assert_eq!(p.total(), USCents(773 + 6_000));
}

/// Last year's overpayment applied forward counts on April 15.
#[test]
fn estimated_overpayment_applied_forward() {
    let forward = estimated::new_payment()
        .amount(DOLLAR * 4_000)
        .from_prior_year()
        .build()
        .unwrap();
    let p = penalty_of(&quarters(vec![forward]), day(2027, 4, 15));
    assert_eq!(p.value().total().0, 0);
}

/// Schedule U Part IV, with its recapture: each installment is the smaller
/// of the annualised amount and the regular quarter plus what was lowered
/// before.
#[test]
fn estimated_annualised_method() {
    let mut h = Household {
        annualised: Some(vec![
            DOLLAR * 1_000,
            DOLLAR * 2_000,
            DOLLAR * 4_000,
            DOLLAR * 10_000,
        ]),
        ..taxed(DOLLAR * 10_000)
    };
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, want) in [DOLLAR * 225, DOLLAR * 900, DOLLAR * 2_700, DOLLAR * 9_000]
        .into_iter()
        .enumerate()
    {
        assert_eq!(sched[i].needed_by(), want, "installment {}", i + 1);
    }
    // A smaller tax for the whole year lowers the last installment.
    if let Some(a) = h.annualised.as_mut() {
        a[3] = DOLLAR * 8_000;
    }
    let got = rules().schedule(&h.facts()).unwrap().value()[3].needed_by();
    assert_eq!(got, DOLLAR * 7_200);
    // A large annualised tax early cannot raise an installment over the
    // regular quarter.
    h.annualised = Some(vec![
        DOLLAR * 20_000,
        DOLLAR * 20_000,
        DOLLAR * 20_000,
        DOLLAR * 10_000,
    ]);
    let sched = rules().schedule(&h.facts()).unwrap().value();
    for (i, inst) in sched.iter().enumerate() {
        assert_eq!(
            inst.needed_by(),
            DOLLAR * 2_250 * (i as i64 + 1),
            "capped: {}",
            i + 1
        );
    }
}

/// § 71.09(14): a return filed and paid by January 31, moved to Monday
/// February 1, 2027, replaces the January payment only when nothing was
/// required by September 15.
#[test]
fn estimated_return_filed_by_january_31() {
    let mut late = Household {
        annualised: Some(vec![USCents(0), USCents(0), USCents(0), DOLLAR * 10_000]),
        ..taxed(DOLLAR * 10_000)
    };
    for (filed, excused, total) in [(day(2027, 2, 1), true, 0), (day(2027, 2, 2), false, 5_326)] {
        late.return_filed = filed;
        let sched = rules().schedule(&late.facts()).unwrap().value();
        assert_eq!(
            sched[3].status() == Status::Excused,
            excused,
            "filed {filed}"
        );
        for inst in sched.iter().take(3) {
            assert_eq!(inst.status(), Status::NothingDue, "filed {filed}");
        }
        let p = penalty_of(&late, day(2027, 4, 15));
        assert_eq!(p.value().total(), USCents(total), "filed {filed}");
    }

    // Equal quarters: something was due earlier, so filing early does not
    // replace January.
    let mut h = quarters(vec![]);
    h.return_filed = day(2027, 1, 20);
    let res = rules().schedule(&h.facts()).unwrap();
    assert_ne!(res.value()[3].status(), Status::Excused);
    assert!(applied(
        &res.applied(),
        "does not replace the January installment"
    ));
}

/// The interest stops on the day the return's balance was paid, and never
/// runs past April 15, 2027.
#[test]
fn estimated_interest_stops() {
    let mut h = quarters(vec![]);
    h.return_filed = day(2027, 3, 1);
    let res = penalty_of(&h, day(2027, 4, 15));
    assert_eq!(res.value().as_of(), day(2027, 3, 1));
    assert!(applied(
        &res.applied(),
        "balance due on the return was paid"
    ));
    let p = penalty_of(&quarters(vec![]), day(2027, 6, 30));
    assert_eq!(p.value().as_of(), day(2027, 4, 15));
}

/// Code 08: withholding counted in the period it was withheld.
#[test]
fn estimated_withholding_as_withheld() {
    let mut h = quarters(vec![]);
    h.withheld = vec![
        withheld(day(2026, 3, 1), DOLLAR * 2_500),
        withheld(day(2026, 12, 15), DOLLAR * 500),
    ];
    let even = rules().schedule(&h.facts()).unwrap().value();
    for (i, inst) in even.iter().enumerate() {
        assert_eq!(inst.shortfall(), DOLLAR * 250, "spread evenly: {}", i + 1);
    }
    h.as_withheld = true;
    let res = rules().schedule(&h.facts()).unwrap();
    for (i, want) in [USCents(0), USCents(0), DOLLAR * 500, DOLLAR * 500]
        .into_iter()
        .enumerate()
    {
        assert_eq!(res.value()[i].shortfall(), want, "as withheld: {}", i + 1);
    }
    assert!(applied(&res.applied(), "code 08"));
}

/// A late starter still owes a quarter from April 15. Here the requirement
/// arose in July, and September's $3,000 pays June's $1,000 and then
/// April's $1,000 on September 15.
#[test]
fn estimated_late_starter_is_charged_from_april() {
    let d = dues();
    let mut h = quarters(vec![pay(d[2], DOLLAR * 3_000), pay(d[3], DOLLAR * 1_000)]);
    h.first_met = day(2026, 7, 1);
    let res = penalty_of(&h, day(2027, 4, 15));
    assert_eq!(res.value().total(), USCents(5_030 + 3_025));
    assert!(applied(&res.applied(), "first met on July 1, 2026"));
    assert!(!has(&res.required(), PositionKind::RequirementFirstMet));
}

#[test]
fn estimated_exceptions() {
    let zero_figures = estimated::new_figures().tax(USCents(0)).build().unwrap();
    let pr = |f: &dyn Fn(&mut estimated::PriorReturnBuilder)| {
        let mut b = estimated::new_prior_return();
        f(&mut b);
        Some(b.build().unwrap())
    };
    let none = PositionKind::Unspecified;
    // zero is whether the interest is nil anyway: a filed 12-month return
    // with no tax makes the prior-year target $0, resident or not.
    for (name, p, exempt, zero, ask) in [
        (
            "code 02",
            pr(&|b| {
                b.filed(true).figures(zero_figures).full_year_resident(true);
            }),
            true,
            true,
            none,
        ),
        (
            "code 02, not required to file",
            pr(&|b| {
                b.not_required_to_file().full_year_resident(true);
            }),
            true,
            true,
            none,
        ),
        (
            "not required to file, part-year",
            pr(&|b| {
                b.not_required_to_file().full_year_resident(false);
            }),
            false,
            false,
            none,
        ),
        (
            "not required to file, not said",
            pr(&|b| {
                b.not_required_to_file();
            }),
            false,
            false,
            PositionKind::Residency,
        ),
        (
            "filed, part-year resident",
            pr(&|b| {
                b.filed(true)
                    .figures(zero_figures)
                    .full_year_resident(false);
            }),
            false,
            true,
            none,
        ),
        (
            "short year",
            pr(&|b| {
                b.filed(true)
                    .months(11)
                    .figures(zero_figures)
                    .full_year_resident(true);
            }),
            false,
            false,
            none,
        ),
        ("tax last year", last(DOLLAR * 1_000), false, false, none),
    ] {
        let h = Household {
            prior: p,
            ..taxed(DOLLAR * 10_000)
        };
        let res = rules().requirement(&h.facts()).unwrap();
        assert_eq!(
            res.value().exempt(),
            exempt,
            "{name}: {}",
            res.value().why()
        );
        if ask != none {
            assert!(has(&res.required(), ask), "{name}: {ask} is not asked");
        }
        let p = penalty_of(&h, day(2027, 4, 15));
        assert_eq!(p.value().total().0 == 0, zero, "{name}");
    }

    // Code 10: a federal disaster extension means no interest.
    let disaster = wisconsintaxcalc::new_estimated_facts()
        .federal_disaster_extension(true)
        .build()
        .unwrap();
    let mut h = quarters(vec![]);
    h.extras = Some(disaster);
    assert!(rules().requirement(&h.facts()).unwrap().value().exempt());
    assert_eq!(penalty_of(&h, day(2027, 4, 15)).value().total().0, 0);

    // Not answered: asked, and assumed not.
    let mut h = quarters(vec![]);
    h.extras = Some(EstimatedFacts::default());
    let res = rules().requirement(&h.facts()).unwrap();
    let n = res
        .required()
        .iter()
        .filter(|r| r.position() == PositionKind::JurisdictionFact)
        .count();
    assert!(n == 2 && !res.value().exempt(), "{n} Wisconsin facts asked");

    // Codes 05 and 06 are waivers the department decides: the interest is
    // shown before them.
    let full = penalty_of(&quarters(vec![]), day(2027, 4, 15))
        .value()
        .total();
    for (k, code) in [
        (WaiverKind::Casualty, "code 05"),
        (WaiverKind::RetiredOrDisabled, "code 06"),
    ] {
        let mut h = quarters(vec![]);
        h.waiver = Some(k);
        let res = penalty_of(&h, day(2027, 4, 15));
        assert!(
            res.value().total() == full
                && res.value().waiver() == k
                && applied(&res.applied(), code),
            "{code}"
        );
        assert!(!has(&res.required(), PositionKind::PenaltyWaiver), "{code}");
    }
    let res = penalty_of(&quarters(vec![]), day(2027, 4, 15));
    assert!(has(&res.required(), PositionKind::PenaltyWaiver));

    // Code 07 follows the form: a waiver to apply for, the interest shown
    // before it.
    let abroad = wisconsintaxcalc::new_estimated_facts()
        .prior_year_farmer_or_fisher(false)
        .federal_disaster_extension(false)
        .active_duty_outside_us(true)
        .build()
        .unwrap();
    let mut h = quarters(vec![]);
    h.extras = Some(abroad);
    let res = penalty_of(&h, day(2027, 4, 15));
    assert!(res.value().total() == full && applied(&res.applied(), "code 07"));

    // Withholding that reaches the required annual payment ends Schedule U
    // at line 8, whatever the election.
    let mut h = quarters(vec![]);
    h.withheld = vec![withheld(day(2026, 12, 15), DOLLAR * 4_000)];
    h.as_withheld = true;
    let res2 = rules().requirement(&h.facts()).unwrap();
    assert!(!res2.value().required(), "{}", res2.value().why());
    assert_eq!(penalty_of(&h, day(2027, 4, 15)).value().total().0, 0);
}

/// Each divergence is reported with both readings and both sources.
#[test]
fn estimated_divergences_pin_both_readings() {
    let res = rules().requirement(&quarters(vec![]).facts()).unwrap();
    let want = [
        (
            "Retirement or disability",
            [
                "reasonable cause",
                "Schedule U Instructions (2025)",
                "no reasonable-cause condition",
                "§ 71.09(11)(d)",
            ],
            Reading::Form,
        ),
        (
            "Active duty outside the continental United States",
            [
                "apply for a waiver",
                "Schedule U Instructions (2025)",
                "does not apply to such a person",
                "§ 71.09(2)",
            ],
            Reading::Form,
        ),
        (
            "A requirement first met after March 31",
            [
                "1/2, 3/4 or all",
                "Form 1-ES",
                "25%",
                "§ 71.09(12), (13)(a)",
            ],
            Reading::Statute,
        ),
    ];
    assert_eq!(res.divergences().len(), want.len());
    for d in res.divergences() {
        let Some((_, w, uses)) = want.iter().find(|w| w.0 == d.rule()) else {
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
        // Equal quarters with no waiver asked, no active duty and no late
        // start: none of the three reaches these facts.
        assert!(
            d.uses() == *uses && !d.affects_household(),
            "{:?}",
            d.rule()
        );
    }
    let abroad = wisconsintaxcalc::new_estimated_facts()
        .active_duty_outside_us(true)
        .build()
        .unwrap();
    let mut h = quarters(vec![]);
    h.extras = Some(abroad);
    for d in rules().requirement(&h.facts()).unwrap().divergences() {
        assert_eq!(
            d.affects_household(),
            d.rule() == "Active duty outside the continental United States",
            "active duty: {:?}",
            d.rule()
        );
    }
}

#[test]
fn estimated_refuses_what_is_not_built() {
    for year in [2025, 2027] {
        let Err(err) = wisconsintaxcalc::estimated_tax(year) else {
            panic!("{year}: rules returned");
        };
        assert!(err.is(&estimated::Error::NoRules), "{year}: {err}");
    }
    let this = estimated::new_figures()
        .tax(DOLLAR * 10_000)
        .build()
        .unwrap();
    let base = |j: Jurisdiction, year: i64| {
        let mut b = estimated::new_facts(j, year);
        b.filing_status(filing::Status::Single).this_year(this);
        b
    };
    let prior_farmer = wisconsintaxcalc::new_estimated_facts()
        .prior_year_farmer_or_fisher(true)
        .build()
        .unwrap();
    for (name, f) in [
        (
            "a farmer",
            base(WI, 2026).farm_share(Rate(6_700)).build().unwrap(),
        ),
        (
            "a fisher",
            base(WI, 2026).fish_share(Rate(6_700)).build().unwrap(),
        ),
        (
            "a farmer last year",
            base(WI, 2026).state(prior_farmer).build().unwrap(),
        ),
    ] {
        let Err(err) = rules().requirement(&f) else {
            panic!("{name}: accepted");
        };
        assert!(err.is(&estimated::Error::Unsupported), "{name}: {err}");
    }
    rules()
        .requirement(&base(WI, 2026).farm_share(Rate(6_600)).build().unwrap())
        .unwrap();
    let mut periods = vec![
        estimated::new_period()
            .end(day(2026, 4, 30))
            .annualised_tax(USCents(100))
            .build()
            .unwrap(),
    ];
    for e in [day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)] {
        periods.push(
            estimated::new_period()
                .end(e)
                .annualised_tax(USCents(100))
                .build()
                .unwrap(),
        );
    }
    let period_end = base(WI, 2026)
        .periods(&periods)
        .elections(estimated::new_elections().annualised().build().unwrap())
        .build()
        .unwrap();
    for (name, f) in [
        (
            "California's facts",
            base(estimated::CALIFORNIA, 2026).build().unwrap(),
        ),
        ("2027", base(WI, 2027).build().unwrap()),
        (
            "other extras",
            base(WI, 2026).state("not Wisconsin's").build().unwrap(),
        ),
        ("a period end", period_end),
    ] {
        assert!(rules().requirement(&f).is_err(), "{name}: accepted");
    }
    assert!(
        rules()
            .penalty(&quarters(vec![]).facts(), Date::default())
            .is_err()
    );
}

#[test]
fn estimated_explanation_names_the_differences() {
    let e = rules().explanation();
    let text = format!(
        "{} {} {}",
        e.summary(),
        e.body().join(" "),
        e.versus_federal()
    );
    for s in [
        "$500",
        "12%",
        "90%",
        "100%",
        "no 110%",
        "most recent one first",
        "full-year Wisconsin resident",
        "February 1, 2027",
        "annualized",
    ] {
        assert!(text.contains(s), "the explanation does not mention {s:?}");
    }
    assert!(!e.versus_federal().is_empty() && !e.sources().is_empty());
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
    let ws: Vec<_> = (1..=12)
        .map(|m| withheld(day(2026, m, 28), DOLLAR * 300))
        .collect();
    let f = estimated::new_facts(WI, 2026)
        .filing_status(filing::Status::Single)
        .this_year(figures(DOLLAR * 3_000))
        .prior_year(last)
        .withheld(&ws)
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

/// A waiver case: its name, the positions, the outcome wanted and a phrase
/// of its reason.
type WaiverCase = (
    &'static str,
    Positions,
    fn(&WaiverOutcome) -> bool,
    &'static str,
);

/// § 71.09(11)(c) and (d) and the Schedule U instructions grant the federal
/// pair: a casualty or disaster, and retiring after 62 or becoming disabled
/// in the year or the year before.
#[test]
fn estimated_waiver_outcomes() {
    let full = penalty_of(&quarters(vec![]), day(2027, 4, 15))
        .value()
        .total();
    let pos = |c: WaiverCause, on: Date, born: Date| {
        let mut b = estimated::new_positions();
        b.waiver_for(c, on);
        if !born.is_zero() {
            b.born(born);
        }
        b.build().unwrap()
    };
    let cases: [WaiverCase; 4] = [
        (
            "retired in 2026 at 64",
            pos(WaiverCause::Retirement, day(2026, 6, 1), day(1962, 3, 1)),
            WaiverOutcome::eligible,
            "after reaching 62",
        ),
        (
            "retired in 2020",
            pos(WaiverCause::Retirement, day(2020, 6, 1), day(1955, 3, 1)),
            WaiverOutcome::refused,
            "2025 or 2026",
        ),
        (
            "retired in 2026 at 60",
            pos(WaiverCause::Retirement, day(2026, 6, 1), day(1966, 3, 1)),
            WaiverOutcome::refused,
            "reached 62",
        ),
        (
            "a disaster",
            pos(WaiverCause::Disaster, day(2026, 8, 1), Date::default()),
            WaiverOutcome::eligible,
            "Department of Revenue",
        ),
    ];
    for (name, p, want, inn) in cases {
        let mut h = quarters(vec![]);
        h.positions = Some(p);
        let res = penalty_of(&h, day(2027, 4, 15));
        let o = res.value().waiver_outcome().unwrap();
        assert!(
            want(&o) && o.reason().contains(inn),
            "{name}: {:?}",
            o.reason()
        );
        assert!(res.value().total() == full && full.0 != 0, "{name}");
        let want_if = if o.eligible() { USCents(0) } else { full };
        assert_eq!(res.value().if_waived(), want_if, "{name}");
    }
}

/// Under an exception, no question says its answer changes the target.
#[test]
fn estimated_exempt_asks_nothing_about_the_target() {
    let disaster = wisconsintaxcalc::new_estimated_facts()
        .prior_year_farmer_or_fisher(false)
        .federal_disaster_extension(true)
        .active_duty_outside_us(false)
        .build()
        .unwrap();
    let h = Household {
        no_tax: true,
        extras: Some(disaster),
        ..Household::default()
    };
    let res = rules().requirement(&h.facts()).unwrap();
    assert!(res.value().exempt());
    for q in res.required() {
        assert!(
            !q.effect().to_lowercase().contains("target"),
            "{:?}",
            q.effect()
        );
    }
    assert!(!has(&res.required(), PositionKind::ThisYearTax));
}
