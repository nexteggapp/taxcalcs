//! Ported from taxyear/model/employer/employer_test.go and helpers_test.go.

#![allow(clippy::unwrap_used, clippy::expect_used, clippy::type_complexity)]

use std::sync::Arc;

use money::USCents;
use proto::commonv1::{self, Money, StateTaxWithholding};
use proto::factsv1::{
    self, FormW4, PayFigures, PaycheckKind, Person, PretaxKind, ResidencePeriod, TaxYearFacts,
    pay_figures::Pretax,
};
use proto::federalv1::{
    FilingStatus, FormW2,
    form_w2::{CodedAmount, OtherItem},
};
use proto::scenariosv1::{
    self,
    __buffa::oneof::employer_default::Rest,
    __buffa::oneof::employer_part::Part,
    __buffa::oneof::item::Figures,
    __buffa::oneof::r#match::To,
    __buffa::oneof::retirement_part::Part as RetirementKind,
    __buffa::oneof::work_place::Place,
    Actual, EmployerDefault, EmployerModel, EmployerPart, EmployerProjection, Item, ItemKind,
    ItemStatus, LastYearW2, Override, PayFrequency, PayTotals, RetirementPart, RetirementPlan,
    StateWithholding, SupplementalMethod, W4, WorkPlace,
    employer_part::{
        Ended, Ends, FromDeposits, FromPaystubs, FromW2, OneOff, PayRate, YearTotal,
        ends::FinalPayment, from_deposits::Deposit, from_paystubs::Stub,
    },
    origin::Basis,
    retirement_part::WorkplacePlanTotal,
    w2boxes,
    work_place::{Split, split::StateDays},
};
use taxcalcs_taxyear::internal::civil::{self, Date};
use taxcalcs_taxyear::model::{self, Context, employer, employer::Job};

// --- helpers_test.go ---

/// An amount in dollars and cents, written as a literal: usd(5833.33).
fn usd(d: f64) -> Money {
    let c = if d < 0.0 {
        (d * 100.0 - 0.5) as i64
    } else {
        (d * 100.0 + 0.5) as i64
    };
    Money {
        minor: c,
        currency: "USD".to_string(),
        ..Default::default()
    }
}

fn day(m: i32, d: i32) -> commonv1::Date {
    commonv1::Date {
        year: 2026,
        month: m,
        day: d,
        ..Default::default()
    }
}

fn ctx_at(m: i32, d: i32, facts: Option<TaxYearFacts>) -> Context {
    Context::new(2026, Some(&day(m, d)), facts.map(Arc::new)).unwrap()
}

fn pretax(kind: PretaxKind, amount: f64) -> Pretax {
    Pretax {
        kind: kind.into(),
        amount: usd(amount).into(),
        ..Default::default()
    }
}

fn swh(state: &str, income: Option<f64>, withheld: Option<f64>) -> StateTaxWithholding {
    StateTaxWithholding {
        state: state.to_string(),
        state_income: income.map(usd).into(),
        tax_withheld: withheld.map(usd).into(),
        ..Default::default()
    }
}

/// $5,833.33 gross with a $500 401(k) line, $700 federal and $300
/// California withheld, and $4,000 net.
fn paycheck() -> PayFigures {
    PayFigures {
        gross: usd(5833.33).into(),
        pretax: vec![pretax(PretaxKind::PRETAX_KIND_401K, 500.0)],
        federal_withheld: usd(700.0).into(),
        social_security_withheld: usd(361.67).into(),
        medicare_withheld: usd(84.58).into(),
        states: vec![swh("CA", Some(5333.33), Some(300.0))],
        net: usd(4000.0).into(),
        ..Default::default()
    }
}

fn gross(g: f64) -> PayFigures {
    PayFigures {
        gross: usd(g).into(),
        ..Default::default()
    }
}

fn stub(id: &str, m: i32, d: i32, kind: PaycheckKind, f: PayFigures) -> Stub {
    Stub {
        id: id.to_string(),
        source: factsv1::DocumentRef {
            name: id.to_string(),
            ..Default::default()
        }
        .into(),
        paystub: factsv1::Paystub {
            pay_date: day(m, d).into(),
            kind: kind.into(),
            this_paycheck: f.into(),
            ..Default::default()
        }
        .into(),
        ..Default::default()
    }
}

fn regular(id: &str, m: i32, d: i32) -> Stub {
    stub(id, m, d, PaycheckKind::PAYCHECK_KIND_REGULAR, paycheck())
}

fn part(id: &str, p: Part) -> EmployerPart {
    EmployerPart {
        id: id.to_string(),
        part: Some(p),
        ..Default::default()
    }
}

fn stubs(id: &str, s: Vec<Stub>) -> EmployerPart {
    part(
        id,
        Part::FromPaystubs(Box::new(FromPaystubs {
            stubs: s,
            ..Default::default()
        })),
    )
}

fn deposit(id: &str, m: i32, d: i32, amount: f64) -> Deposit {
    Deposit {
        id: id.to_string(),
        date: day(m, d).into(),
        amount: usd(amount).into(),
        source: factsv1::LedgerRef {
            vault_ref: format!("txn_{id}"),
            posted: day(m, d).into(),
            ..Default::default()
        }
        .into(),
        ..Default::default()
    }
}

fn deposits(id: &str, d: Vec<Deposit>) -> EmployerPart {
    part(
        id,
        Part::FromDeposits(Box::new(FromDeposits {
            deposits: d,
            ..Default::default()
        })),
    )
}

fn state_work(s: &str) -> WorkPlace {
    WorkPlace {
        place: Some(Place::State(s.to_string())),
        ..Default::default()
    }
}

fn split_work(days: &[(&str, i32)]) -> WorkPlace {
    WorkPlace {
        place: Some(Place::Split(Box::new(Split {
            days: days
                .iter()
                .map(|(s, d)| StateDays {
                    state: s.to_string(),
                    days: *d,
                    ..Default::default()
                })
                .collect(),
            ..Default::default()
        }))),
        ..Default::default()
    }
}

fn where_lived() -> WorkPlace {
    WorkPlace {
        place: Some(Place::WhereLived(Box::default())),
        ..Default::default()
    }
}

fn each_paycheck(id: &str, f: PayFigures) -> EmployerDefault {
    EmployerDefault {
        id: id.to_string(),
        rest: Some(Rest::EachPaycheck(Box::new(f))),
        ..Default::default()
    }
}

/// A twice-monthly 401(k) job in California, flat supplemental, that
/// repeats its latest paystub.
fn job(parts: Vec<EmployerPart>) -> EmployerModel {
    EmployerModel {
        id: "acme".to_string(),
        name: "Acme Corp".to_string(),
        person: "you".to_string(),
        pay_frequency: PayFrequency::PAY_FREQUENCY_SEMI_MONTHLY.into(),
        first_pay_date: day(1, 15).into(),
        retirement_plan: RetirementPlan::RETIREMENT_PLAN_401K.into(),
        work: state_work("CA").into(),
        supplemental: SupplementalMethod::SUPPLEMENTAL_METHOD_FLAT.into(),
        parts,
        default_part: EmployerDefault {
            id: "acme-default".to_string(),
            rest: Some(Rest::SameAsLatest(Box::default())),
            ..Default::default()
        }
        .into(),
        ..Default::default()
    }
}

/// Expands and projects one job with no max-out.
fn expand<'a>(
    ctx: &'a Context,
    m: &'a EmployerModel,
    overrides: &[Override],
) -> (EmployerProjection, Job<'a>) {
    let mut j = employer::expand(ctx, m, overrides).unwrap();
    let p = j.projection().unwrap();
    (p, j)
}

fn item<'p>(p: &'p EmployerProjection, id: &str) -> &'p Item {
    match p.items.iter().find(|it| it.id == id) {
        Some(it) => it,
        None => {
            let ids: Vec<&str> = p.items.iter().map(|it| it.id.as_str()).collect();
            panic!("no item {id}; items are {ids:?}")
        }
    }
}

fn pay(it: &Item) -> &PayFigures {
    match &it.figures {
        Some(Figures::Paycheck(f)) => f,
        _ => panic!("{} has no paycheck", it.id),
    }
}

fn cents<M: model::MoneyRef + ?Sized>(m: &M) -> i64 {
    model::value(m).0
}

fn question_ids(n: &model::Notes) -> Vec<String> {
    n.questions().iter().map(|q| q.id().to_string()).collect()
}

fn has_question(j: &Job<'_>, id: &str) -> bool {
    question_ids(j.notes()).iter().any(|q| q == id)
}

fn dates(ds: &[Date]) -> Vec<String> {
    ds.iter().map(|d| d.to_string()).collect()
}

/// Whether an item's amount is its exact amount shown in whole dollars:
/// items are rounded with a running total, so each is within a dollar of
/// what it was.
fn near(got: i64, want: i64) -> bool {
    got % 100 == 0 && got - want < 100 && want - got < 100
}

/// An exact total as the projection shows it.
fn dollars(v: i64) -> i64 {
    model::dollars(USCents(v)).0
}

fn year_total(id: &str, year: PayFigures, typed: commonv1::Date) -> EmployerPart {
    part(
        id,
        Part::YearTotal(Box::new(YearTotal {
            year: year.into(),
            typed_against: typed.into(),
            ..Default::default()
        })),
    )
}

fn pay_rate(id: &str, from: commonv1::Date, g: f64) -> EmployerPart {
    part(
        id,
        Part::PayRate(Box::new(PayRate {
            from: from.into(),
            paycheck: gross(g).into(),
            ..Default::default()
        })),
    )
}

fn ended(id: &str, on: commonv1::Date) -> EmployerPart {
    part(
        id,
        Part::Ended(Box::new(Ended {
            last_paycheck: on.into(),
            ..Default::default()
        })),
    )
}

fn ends(id: &str, on: commonv1::Date, f: Option<FinalPayment>) -> EmployerPart {
    part(
        id,
        Part::Ends(Box::new(Ends {
            last_paycheck: on.into(),
            r#final: f.into(),
            ..Default::default()
        })),
    )
}

fn one_off(id: &str, on: commonv1::Date, amount: f64) -> EmployerPart {
    part(
        id,
        Part::OneOff(Box::new(OneOff {
            date: on.into(),
            kind: PaycheckKind::PAYCHECK_KIND_BONUS.into(),
            amount: usd(amount).into(),
            ..Default::default()
        })),
    )
}

fn max_part() -> RetirementPart {
    RetirementPart {
        id: "you-max".to_string(),
        part: Some(RetirementKind::MaxOutWorkplacePlan(Box::default())),
        ..Default::default()
    }
}

fn born(year: i32) -> TaxYearFacts {
    TaxYearFacts {
        people: vec![Person {
            id: "you".to_string(),
            date_of_birth: commonv1::Date {
                year,
                month: 1,
                day: 1,
                ..Default::default()
            }
            .into(),
            ..Default::default()
        }],
        ..Default::default()
    }
}

fn lived(periods: &[(&str, (i32, i32), (i32, i32))]) -> TaxYearFacts {
    TaxYearFacts {
        residence: periods
            .iter()
            .map(|(s, f, t)| ResidencePeriod {
                id: s.to_lowercase(),
                state: s.to_string(),
                from: day(f.0, f.1).into(),
                to: day(t.0, t.1).into(),
                ..Default::default()
            })
            .collect(),
        ..Default::default()
    }
}

fn stub_paystub(s: &mut Stub) -> &mut factsv1::Paystub {
    s.paystub.get_or_insert_default()
}

fn year_line<'p>(p: &'p PayTotals, state: &str) -> Option<&'p StateTaxWithholding> {
    p.items.states.iter().find(|s| s.state == state)
}

fn box_line<'p>(p: &'p PayTotals, state: &str) -> Option<&'p w2boxes::State> {
    p.boxes.states.iter().rfind(|s| s.state == state)
}

// --- employer_test.go ---

// Slots: pay dates from first_pay_date and the frequency, weekend dates
// moved to the Friday before, none before starts.
#[test]
fn pay_dates() {
    let semi = employer::pay_dates(
        2026,
        PayFrequency::PAY_FREQUENCY_SEMI_MONTHLY.into(),
        civil::must(2026, 1, 15),
        Date::default(),
    );
    assert_eq!(semi.len(), 24);
    let got = dates(&semi).join(" ");
    for want in [
        "2026-08-14",
        "2026-08-31",
        "2026-10-30",
        "2026-11-13",
        "2026-12-31",
    ] {
        assert!(got.contains(want), "twice-monthly dates {got} lack {want}");
    }
    let bi = employer::pay_dates(
        2026,
        PayFrequency::PAY_FREQUENCY_BIWEEKLY.into(),
        civil::must(2026, 1, 9),
        Date::default(),
    );
    assert!(
        bi.len() == 26 && bi[0].to_string() == "2026-01-09" && bi[25].to_string() == "2026-12-25",
        "{:?}",
        dates(&bi)
    );
    let monthly = employer::pay_dates(
        2026,
        PayFrequency::PAY_FREQUENCY_MONTHLY.into(),
        Date::default(),
        civil::must(2026, 6, 1),
    );
    assert!(
        monthly.len() == 7
            && monthly[0].to_string() == "2026-06-30"
            && monthly[4].to_string() == "2026-10-30",
        "{:?}",
        dates(&monthly)
    );
}

// Evidence fills every pay date up to the latest evidence date; the default
// part, SameAsLatest, the pay dates after it. A covered pay date with no
// paystub, deposit or year-to-date column is an expected paycheck copying
// the template, so the $140,000 year is $140,000.
#[test]
fn evidence_then_default() {
    let m = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 14)])]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    let got = item(&p, "employers/acme/2026-08-14/regular");
    assert!(
        got.status == ItemStatus::ITEM_STATUS_MADE && got.origin.basis == Basis::BASIS_DOCUMENT
    );
    for it in &p.items {
        if it.date.month < 8 {
            assert!(
                it.status == ItemStatus::ITEM_STATUS_EXPECTED
                    && it.origin.template == "acme-s1"
                    && !it.estimated.is_empty(),
                "a covered pay date's item is {it:?}"
            );
        }
    }
    let rest = item(&p, "employers/acme/2026-12-31/regular");
    assert!(rest.status == ItemStatus::ITEM_STATUS_EXPECTED && rest.origin.part == "acme-default");
    assert!(near(cents(&pay(rest).gross), 583333));
    assert_eq!(cents(&pay(rest).federal_withheld), 70000);
    assert_eq!(p.items.len(), 24);
    assert_eq!(cents(&p.year.items.gross), dollars(24 * 583333));
}

// A regular paystub fills the pay date nearest it; payroll moves a weekend
// pay date to the business day before.
#[test]
fn paystub_fills_nearest_pay_date() {
    let m = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 13)])]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    assert_eq!(item(&p, "employers/acme/2026-08-14/regular").date.day, 13);
}

// Deposits within three days of a paystub that sum to its net pair with
// it, so split deposits pair too.
#[test]
fn split_deposits_pair_by_net() {
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        deposits(
            "acme-d",
            vec![
                deposit("acme-d1", 8, 14, 3000.0),
                deposit("acme-d2", 8, 15, 1000.0),
                deposit("acme-d3", 8, 14, 12.50),
            ],
        ),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, j) = expand(&ctx, &m, &[]);
    let got = item(&p, "employers/acme/2026-08-14/regular");
    assert!(cents(&got.take_home) == 400000 && got.origin.evidence.len() == 3);
    let extra = item(&p, "employers/acme/2026-08-14/deposit/acme-d3");
    assert!(
        extra.kind == ItemKind::ITEM_KIND_DEPOSIT
            && has_question(&j, "employers/acme/deposit/acme-d3")
    );
}

// A paystub with no net pairs by date with the deposits within three days,
// taken as one set.
#[test]
fn netless_stub_pairs_by_date() {
    let mut f = paycheck();
    f.net = None.into();
    let m = job(vec![
        stubs(
            "acme-p",
            vec![stub(
                "acme-s1",
                8,
                14,
                PaycheckKind::PAYCHECK_KIND_REGULAR,
                f,
            )],
        ),
        deposits(
            "acme-d",
            vec![
                deposit("acme-d1", 8, 14, 3000.0),
                deposit("acme-d2", 8, 14, 999.0),
            ],
        ),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    assert_eq!(
        cents(&item(&p, "employers/acme/2026-08-14/regular").take_home),
        399900
    );
}

// Two candidate sets for one paystub are a question; until it is answered
// the set nearest in date pairs.
#[test]
fn two_deposit_sets_pair_the_nearest() {
    let mut f = paycheck();
    f.net = None.into();
    let m = job(vec![
        stubs(
            "acme-p",
            vec![stub(
                "acme-s1",
                8,
                14,
                PaycheckKind::PAYCHECK_KIND_REGULAR,
                f,
            )],
        ),
        deposits(
            "acme-d",
            vec![
                deposit("acme-d1", 8, 15, 3000.0),
                deposit("acme-d2", 8, 17, 999.0),
            ],
        ),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, j) = expand(&ctx, &m, &[]);
    assert_eq!(
        cents(&item(&p, "employers/acme/2026-08-14/regular").take_home),
        300000
    );
    assert!(has_question(&j, "employers/acme/paystub/acme-s1/deposits"));
    // By net: two deposits each equal to it.
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        deposits(
            "acme-d",
            vec![
                deposit("acme-d1", 8, 17, 4000.0),
                deposit("acme-d2", 8, 14, 4000.0),
            ],
        ),
    ]);
    let (p, j) = expand(&ctx, &m, &[]);
    let got = item(&p, "employers/acme/2026-08-14/regular");
    assert!(got.origin.evidence.len() == 2 && got.origin.evidence[1] == "acme-d2");
    assert!(has_question(&j, "employers/acme/paystub/acme-s1/deposits"));
}

// A deposit in a pay period no paystub fills is that paycheck, copying the
// latest regular paystub before it, marked as estimated.
#[test]
fn deposit_fills_pay_date_from_template() {
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        deposits("acme-d", vec![deposit("acme-d1", 8, 31, 4000.0)]),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    let got = item(&p, "employers/acme/2026-08-31/regular");
    assert_eq!(got.status, ItemStatus::ITEM_STATUS_MADE);
    assert!(got.origin.basis == Basis::BASIS_LEDGER && got.origin.template == "acme-s1");
    assert!(near(cents(&pay(got).gross), 583333) && !got.estimated.is_empty());
}

// Any other unpaired deposit is an extra paycheck, its gross estimated from
// the latest regular paystub, with nothing withheld, and the result asks.
#[test]
fn extra_deposit_counts_with_nothing_withheld() {
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        deposits("acme-d", vec![deposit("acme-d1", 8, 22, 400.0)]),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, j) = expand(&ctx, &m, &[]);
    let got = item(&p, "employers/acme/2026-08-22/deposit/acme-d1");
    assert!(cents(&pay(got).federal_withheld) == 0 && near(cents(&pay(got).gross), 583333));
    assert!(has_question(&j, "employers/acme/deposit/acme-d1"));
}

// An entry answered as not this source's leaves the model.
#[test]
fn not_this_source_leaves_the_model() {
    let mut d = deposit("acme-d1", 8, 22, 400.0);
    d.matches = scenariosv1::Match {
        to: Some(To::NotThisSource(Box::default())),
        ..Default::default()
    }
    .into();
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        deposits("acme-d", vec![d]),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    assert!(p.items.iter().all(|it| !it.id.contains("acme-d1")));
}

// Bonus, vest and final paystubs are additive, by their evidence id.
#[test]
fn bonus_stub_is_additive() {
    let bonus = stub(
        "acme-b1",
        8,
        20,
        PaycheckKind::PAYCHECK_KIND_BONUS,
        PayFigures {
            gross: usd(10000.0).into(),
            federal_withheld: usd(2200.0).into(),
            ..Default::default()
        },
    );
    let m = job(vec![stubs(
        "acme-p",
        vec![regular("acme-s1", 8, 14), bonus],
    )]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    assert_eq!(
        item(&p, "employers/acme/2026-08-20/bonus/acme-b1").kind,
        ItemKind::ITEM_KIND_BONUS
    );
}

// A year-to-date ADJUSTMENT covers the pay dates with neither a paystub nor
// a deposit, at the latest paystub with a year-to-date column.
#[test]
fn year_to_date_adjustment() {
    let mut s = regular("acme-s1", 8, 14);
    let ytd = PayFigures {
        gross: usd(5833.33 * 15.0).into(),
        federal_withheld: usd(700.0 * 15.0).into(),
        ..Default::default()
    };
    stub_paystub(&mut s).year_to_date = ytd.clone().into();
    let m = job(vec![stubs("acme-p", vec![s])]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    let adj = item(&p, "employers/acme/2026-08-14/regular/adjustment");
    assert!(
        adj.kind == ItemKind::ITEM_KIND_ADJUSTMENT && cents(&pay(adj).federal_withheld) == 980000
    );
    assert_eq!(cents(&p.so_far.items.gross), dollars(cents(&ytd.gross)));
}

// A YearTotal fills the pay dates after the evidence with equal shares of
// the year less the evidence, for the lines it states only.
#[test]
fn year_total_shares_what_is_left() {
    let yt = year_total("acme-yt", gross(5833.33 * 15.0 + 900.0 * 9.0), day(8, 14));
    let mut s = regular("acme-s1", 8, 14);
    stub_paystub(&mut s).year_to_date = gross(5833.33 * 15.0).into();
    let m = job(vec![stubs("acme-p", vec![s]), yt]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    let dec = item(&p, "employers/acme/2026-12-31/regular");
    assert!(cents(&pay(dec).gross) == 90000 && dec.origin.part == "acme-yt");
    let k = &pay(dec).pretax;
    assert!(k.len() == 1 && cents(&k[0].amount) == 50000);
    assert!(
        dec.estimated
            .join(" ")
            .contains("paycheck.federal_withheld")
    );
    assert_eq!(
        cents(&p.year.items.gross),
        dollars(cents(&usd(5833.33 * 15.0 + 900.0 * 9.0)))
    );
}

// A pay YearTotal stands aside once a paystub newer than typed_against is
// read, and the result says why.
#[test]
fn year_total_stands_aside_for_newer_paystub() {
    let yt = year_total("acme-yt", gross(1.0), day(7, 31));
    let m = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 14)]), yt]);
    let ctx = ctx_at(9, 8, None);
    let (p, j) = expand(&ctx, &m, &[]);
    assert_eq!(
        item(&p, "employers/acme/2026-12-31/regular").origin.part,
        "acme-default"
    );
    assert!(has_question(&j, "employers/acme/stands-aside/acme-yt"));
}

// An assumed period part fills from its start until the next one's; its
// unset pre-tax lines are none and its unset withholding is computed.
#[test]
fn pay_rate_from_its_start() {
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        pay_rate("acme-raise", day(10, 1), 6250.0),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    assert_eq!(
        item(&p, "employers/acme/2026-09-30/regular").origin.part,
        "acme-default"
    );
    let oct = item(&p, "employers/acme/2026-10-15/regular");
    assert!(
        oct.origin.part == "acme-raise"
            && cents(&pay(oct).gross) == 625000
            && pay(oct).pretax.is_empty()
    );
    assert_ne!(cents(&pay(oct).federal_withheld), 0);
}

// An unset start is the day after the evidence; an earlier start is clipped
// to it; of several clipped to one day, the latest-started counts and the
// others stand aside.
#[test]
fn pay_rates_clipped_to_the_evidence() {
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        pay_rate("acme-early", day(3, 1), 1000.0),
        pay_rate("acme-later", day(7, 1), 2000.0),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, j) = expand(&ctx, &m, &[]);
    assert_eq!(
        item(&p, "employers/acme/2026-08-31/regular").origin.part,
        "acme-later"
    );
    assert!(has_question(&j, "employers/acme/stands-aside/acme-early"));
}

// A job with no paystub yet fills its pay dates from EachPaycheck.
#[test]
fn each_paycheck_for_a_job_with_no_paystub() {
    let mut m = job(vec![]);
    m.starts = day(11, 1).into();
    m.default_part = each_paycheck("acme-default", gross(4000.0)).into();
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    assert_eq!(p.items.len(), 4);
    assert_eq!(cents(&p.year.items.gross), 1_600_000);
}

// No pay date after a known end is filled; evidence after it counts, and
// the result asks about the date.
#[test]
fn known_end() {
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        ended("acme-end", day(7, 31)),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, j) = expand(&ctx, &m, &[]);
    for it in &p.items {
        assert!(
            !(it.date.month > 7 && it.id != "employers/acme/2026-08-14/regular"),
            "{} is after the end and isn't evidence",
            it.id
        );
    }
    assert!(has_question(&j, "employers/acme/ended/acme-end"));
}

// An assumed end stops the pay dates after it and adds the final payment,
// withheld as given; one on or before the latest evidence stands aside.
#[test]
fn assumed_end() {
    let f = FinalPayment {
        amount: usd(3000.0).into(),
        federal_withheld: usd(660.0).into(),
        state_withheld: usd(100.0).into(),
        ..Default::default()
    };
    let ctx = ctx_at(9, 8, None);
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        ends("acme-ends", day(10, 15), Some(f)),
    ]);
    let (p, _) = expand(&ctx, &m, &[]);
    assert_eq!(
        p.items.last().unwrap().id,
        "employers/acme/2026-10-15/regular"
    );
    let fi = item(&p, "employers/acme/2026-10-15/final/acme-ends");
    assert!(
        cents(&pay(fi).federal_withheld) == 66000
            && cents(&pay(fi).states[0].tax_withheld) == 10000
    );
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        ends("acme-ends", day(8, 1), None),
    ]);
    let (_, j) = expand(&ctx, &m, &[]);
    assert!(has_question(&j, "employers/acme/stands-aside/acme-ends"));
    // An end on the day of the latest evidence counts: no evidence is after
    // it.
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        ends("acme-ends", day(8, 14), None),
    ]);
    let (p, j) = expand(&ctx, &m, &[]);
    assert!(
        !has_question(&j, "employers/acme/stands-aside/acme-ends")
            && p.items.last().unwrap().date.month == 8
    );
}

// A bonus is withheld at 22% under the flat method, and 37% on supplemental
// wages over $1 million from the employer in the year.
#[test]
fn one_off_flat_supplemental() {
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        one_off("acme-b1", day(12, 1), 10000.0),
        one_off("acme-b2", day(12, 2), 1_000_000.0),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    assert_eq!(
        cents(&pay(item(&p, "employers/acme/2026-12-01/bonus/acme-b1")).federal_withheld),
        220000
    );
    // $990,000 at 22% and $10,000 at 37%.
    assert_eq!(
        cents(&pay(item(&p, "employers/acme/2026-12-02/bonus/acme-b2")).federal_withheld),
        21_780_000 + 370_000
    );
}

// Under the aggregate method a bonus is withheld on itself plus its
// period's paycheck, less that paycheck's own.
#[test]
fn one_off_aggregate_supplemental() {
    let mut m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        one_off("acme-b1", day(12, 10), 10000.0),
    ]);
    m.supplemental = SupplementalMethod::SUPPLEMENTAL_METHOD_AGGREGATE.into();
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    let got = cents(&pay(item(&p, "employers/acme/2026-12-10/bonus/acme-b1")).federal_withheld);
    assert!(got > 220000, "aggregate withholding {got}");
}

// Evidence of a bonus within 31 days of a planned one is asked about; both
// count until answered, and answered yes only the evidence counts.
#[test]
fn planned_bonus_against_actual() {
    let bonus = stub(
        "acme-b9",
        8,
        20,
        PaycheckKind::PAYCHECK_KIND_BONUS,
        gross(5000.0),
    );
    let mut planned = one_off("acme-plan", day(9, 1), 5000.0);
    let ctx = ctx_at(9, 8, None);
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14), bonus.clone()]),
        planned.clone(),
    ]);
    let (p, j) = expand(&ctx, &m, &[]);
    assert!(has_question(&j, "employers/acme/planned/acme-plan/acme-b9"));
    item(&p, "employers/acme/2026-09-01/bonus/acme-plan");
    if let Some(Part::OneOff(o)) = &mut planned.part {
        o.actual = Actual {
            is: "acme-b9".to_string(),
            ..Default::default()
        }
        .into();
    }
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14), bonus]),
        planned,
    ]);
    let (p, _) = expand(&ctx, &m, &[]);
    assert!(p.items.iter().all(|it| !it.id.ends_with("acme-plan")));
}

// Social Security stops at the wage base for each job; Medicare adds 0.9%
// on the job's wages over $200,000.
#[test]
fn social_security_cap_and_additional_medicare() {
    let mut m = job(vec![]);
    m.default_part = each_paycheck("acme-default", gross(20000.0)).into();
    let ctx = ctx_at(1, 2, None);
    let (p, _) = expand(&ctx, &m, &[]);
    assert_eq!(cents(&p.year.items.social_security_withheld), 1_143_900);
    assert_eq!(cents(&p.year.boxes.social_security_wages), 18_450_000);
    // $480,000 of wages: 1.45% of all, and 0.9% of $280,000.
    assert_eq!(cents(&p.year.items.medicare_withheld), 696_000 + 252_000);
}

// Item totals are always the sum of the items, and box 1 is gross less the
// pre-tax lines.
#[test]
fn totals_are_the_items_sum() {
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        pay_rate("acme-r", day(11, 1), 7000.0),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    let (mut g, mut fed) = (0, 0);
    for it in &p.items {
        g += cents(&pay(it).gross);
        fed += cents(&pay(it).federal_withheld);
    }
    assert_eq!(
        (g, fed),
        (
            cents(&p.year.items.gross),
            cents(&p.year.items.federal_withheld)
        )
    );
    let year = &p.year;
    let pre: i64 = year.items.pretax.iter().map(|x| cents(&x.amount)).sum();
    // Box 1 is gross less pre-tax, so far and the rest each rounded to the
    // dollar and added.
    let (got, want) = (cents(&year.boxes.wages), cents(&year.items.gross) - pre);
    assert!(
        got >= want - 100 && got <= want + 100 && got % 100 == 0,
        "box 1 {got}, want {want}"
    );
    assert_eq!(
        cents(&p.rest.boxes.wages),
        cents(&year.boxes.wages) - cents(&p.so_far.boxes.wages)
    );
}

// A job worked across states splits box 16 by days, and its paychecks'
// state lines too.
#[test]
fn split_work_by_days() {
    let mut m = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 14)])]);
    m.work = split_work(&[("CA", 150), ("OR", 50)]).into();
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    let states = &p.year.boxes.states;
    assert!(states.len() == 2 && states[0].state == "CA" && states[1].state == "OR");
    let box1 = cents(&p.year.boxes.wages);
    let ca = cents(&states[0].wages);
    assert!(
        ca >= box1 * 3 / 4 - 1 && ca <= box1 * 3 / 4 + 1,
        "{ca} of {box1}"
    );
}

// Without a W-4 federal withholding uses the paystub's filing status alone,
// and with one it reads Step 2 and Step 4(c).
#[test]
fn withholding_reads_the_w4() {
    let mut f = paycheck();
    f.federal_withheld = None.into();
    let base = || {
        job(vec![stubs(
            "acme-p",
            vec![stub(
                "acme-s1",
                8,
                14,
                PaycheckKind::PAYCHECK_KIND_REGULAR,
                f.clone(),
            )],
        )])
    };
    let ctx = ctx_at(9, 8, None);
    let fed = |m: &EmployerModel| {
        let (p, _) = expand(&ctx, m, &[]);
        cents(&pay(item(&p, "employers/acme/2026-12-31/regular")).federal_withheld)
    };
    let single = fed(&base());
    // $5,333.33 taxable twice a month is $127,999.92 a year; less the
    // $16,100 standard deduction, the 2026 single brackets give $19,453.98,
    // a 24th of which is $810.58.
    assert!(near(single, 81_058), "{single}");
    let mut m = base();
    m.w4 = W4 {
        form: FormW4 {
            filing_status: FilingStatus::FILING_STATUS_MARRIED_FILING_JOINTLY.into(),
            extra_withholding: usd(50.0).into(),
            ..Default::default()
        }
        .into(),
        ..Default::default()
    }
    .into();
    assert!(fed(&m) < single);
    m.w4.get_or_insert_default()
        .form
        .get_or_insert_default()
        .multiple_jobs = true;
    assert!(fed(&m) > single / 2);
}

fn over(path: &str, amount: f64) -> Override {
    Override {
        path: format!("{}{path}", employer::override_prefix("acme")),
        amount: usd(amount).into(),
        ..Default::default()
    }
}

// An override of a figure so far adds an ADJUSTMENT dated so_far_through
// that brings the items to the typed figure.
#[test]
fn override_adjustment() {
    let m = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 14)])]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[over("federal_withheld", 9000.0)]);
    let adj = item(&p, "employers/acme/override/federal_withheld");
    assert!(
        adj.origin.basis == Basis::BASIS_OVERRIDE && cents(&pay(adj).federal_withheld) == 830000
    );
    assert_eq!(cents(&p.so_far.items.federal_withheld), 900000);
}

#[test]
fn max_out_shares_the_limit_across_jobs() {
    let ctx = ctx_at(9, 8, Some(born(1990)));
    let ma = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 14)])]);
    let mut other = job(vec![]);
    other.id = "beta".to_string();
    other.name = "Beta".to_string();
    other.default_part = each_paycheck("beta-default", gross(300.0)).into();
    other.starts = day(12, 1).into();
    let mut jobs = vec![
        employer::expand(&ctx, &ma, &[]).unwrap(),
        employer::expand(&ctx, &other, &[]).unwrap(),
    ];
    employer::max_out(&ctx, "you", &max_part(), &mut jobs).unwrap();
    let pa = jobs[0].projection().unwrap();
    let pb = jobs[1].projection().unwrap();
    assert_eq!(
        cents(&pa.year.boxes.k401) + cents(&pb.year.boxes.k401),
        2_450_000
    );
    // Beta's $300 paychecks can't take an equal share: each takes what is
    // left after $22.95 of Social Security and Medicare. Acme's take the
    // rest.
    let dec = item(&pb, "employers/beta/2026-12-15/regular");
    assert_eq!(cents(&pay(dec).pretax[0].amount), 27700);
}

// A deposit with no paystub before it copies the first paystub after it;
// with none, an assumed PayRate; with neither, its gross is the deposit,
// unwithheld, and the result asks.
#[test]
fn deposit_template_chain() {
    let ctx = ctx_at(9, 8, None);
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        deposits("acme-d", vec![deposit("acme-d1", 7, 31, 4000.0)]),
    ]);
    let (p, _) = expand(&ctx, &m, &[]);
    assert_eq!(
        item(&p, "employers/acme/2026-07-31/regular")
            .origin
            .template,
        "acme-s1"
    );
    let m = job(vec![
        deposits("acme-d", vec![deposit("acme-d1", 7, 31, 4000.0)]),
        pay_rate("acme-r", day(10, 1), 6000.0),
    ]);
    let (p, _) = expand(&ctx, &m, &[]);
    let got = item(&p, "employers/acme/2026-07-31/regular");
    assert!(got.origin.template == "acme-r" && cents(&pay(got).gross) == 600000);
    let m = job(vec![deposits(
        "acme-d",
        vec![deposit("acme-d1", 7, 31, 4000.0)],
    )]);
    let (p, j) = expand(&ctx, &m, &[]);
    let got = item(&p, "employers/acme/2026-07-31/regular");
    assert!(
        cents(&pay(got).gross) == 400000
            && cents(&pay(got).federal_withheld) == 0
            && has_question(&j, "employers/acme/deposit/acme-d1")
    );
}

// With a year-to-date column the ADJUSTMENT covers the pay dates before
// it, and no copies are made for them.
#[test]
fn year_to_date_covers_gaps() {
    let mut s = regular("acme-s1", 8, 14);
    stub_paystub(&mut s).year_to_date = gross(5833.33 * 15.0).into();
    let m = job(vec![stubs("acme-p", vec![s])]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    assert!(p.items.iter().all(|it| it.date.month >= 8));
}

// Copied Social Security stops at the wage base for each employer.
#[test]
fn copied_social_security_stops_at_the_base() {
    let f = PayFigures {
        gross: usd(20000.0).into(),
        social_security_withheld: usd(1240.0).into(),
        medicare_withheld: usd(290.0).into(),
        ..Default::default()
    };
    let m = job(vec![stubs(
        "acme-p",
        vec![stub(
            "acme-s1",
            1,
            15,
            PaycheckKind::PAYCHECK_KIND_REGULAR,
            f,
        )],
    )]);
    let ctx = ctx_at(1, 20, None);
    let (p, _) = expand(&ctx, &m, &[]);
    assert_eq!(cents(&p.year.items.social_security_withheld), 1_143_900);
}

// A new state is withheld at the latest paystub's state rate, marked
// estimated, and asked about.
#[test]
fn new_state_at_the_paystub_rate() {
    let mut m = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 14)])]);
    m.work = state_work("OR").into();
    let ctx = ctx_at(9, 8, None);
    let (p, j) = expand(&ctx, &m, &[]);
    let st = &pay(item(&p, "employers/acme/2026-12-31/regular")).states;
    // California's $300 on $5,333.33 of wages.
    assert!(st.len() == 1 && st[0].state == "OR" && cents(&st[0].tax_withheld) == 30000);
    assert!(has_question(&j, "employers/acme/state-withholding/OR"));
}

// The Roth catch-up rule applies only once the plan's roth_option is known:
// unset, the result asks; false, no catch-up is allowed; true, it is Roth.
#[test]
fn roth_catch_up_needs_the_roth_option() {
    let run = |option: Option<bool>| -> (EmployerProjection, Vec<String>) {
        let ctx = ctx_at(9, 8, Some(born(1970)));
        let mut s = regular("acme-s1", 8, 14);
        stub_paystub(&mut s).year_to_date = PayFigures {
            gross: usd(5833.33 * 15.0).into(),
            pretax: vec![pretax(PretaxKind::PRETAX_KIND_401K, 7500.0)],
            ..Default::default()
        }
        .into();
        let mut m = job(vec![stubs("acme-p", vec![s])]);
        m.last_year = LastYearW2 {
            w2: FormW2 {
                social_security_wages: usd(160000.0).into(),
                ..Default::default()
            }
            .into(),
            ..Default::default()
        }
        .into();
        m.roth_option = option;
        let mut jobs = vec![employer::expand(&ctx, &m, &[]).unwrap()];
        let n = employer::max_out(&ctx, "you", &max_part(), &mut jobs).unwrap();
        let p = jobs[0].projection().unwrap();
        (p, question_ids(&n))
    };
    let (p, asked) = run(None);
    assert!(
        asked.join(" ").contains("employers/acme/roth-option"),
        "{asked:?}"
    );
    assert_eq!(cents(&p.year.boxes.k401), 3_250_000);
    assert_eq!(cents(&run(Some(false)).0.year.boxes.k401), 2_450_000);
    assert_eq!(cents(&run(Some(true)).0.year.boxes.roth_401k), 800_000);
}

// Once the year is over, a W-2 states it: the items add up to the form's
// pay, gross being box 1 plus every pre-tax line.
#[test]
fn w2_states_the_year() {
    let ctx = Context::new(
        2026,
        Some(&commonv1::Date {
            year: 2027,
            month: 3,
            day: 1,
            ..Default::default()
        }),
        None,
    )
    .unwrap();
    let w2 = FormW2 {
        tax_year: 2026,
        wages_tips_other_compensation: usd(120000.0).into(),
        federal_tax_withheld: usd(15000.0).into(),
        social_security_tax_withheld: usd(8000.0).into(),
        medicare_tax_withheld: usd(2000.0).into(),
        coded_amounts: vec![CodedAmount {
            code: "D".to_string(),
            amount: usd(12000.0).into(),
            ..Default::default()
        }],
        other_items: vec![OtherItem {
            description: "Other pre-tax benefits".to_string(),
            amount: usd(1000.0).into(),
            ..Default::default()
        }],
        state_tax_withholding: vec![swh("CA", Some(121000.0), Some(6000.0))],
        ..Default::default()
    };
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        part(
            "acme-w2",
            Part::FromW2(Box::new(FromW2 {
                w2: w2.into(),
                ..Default::default()
            })),
        ),
    ]);
    let (p, _) = expand(&ctx, &m, &[]);
    let y = &p.year.items;
    assert_eq!(cents(&y.gross), 13_300_000);
    assert_eq!(cents(&y.federal_withheld), 1_500_000);
    assert_eq!(cents(&y.social_security_withheld), 800_000);
    assert_eq!(cents(&y.medicare_withheld), 200_000);
    assert_eq!(cents(&y.states[0].tax_withheld), 600_000);
}

// The employer's own HSA is spread over the pay dates, and California,
// which taxes it, counts so far's share in its wages.
#[test]
fn employer_hsa_in_california_wages() {
    let mut m = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 14)])]);
    m.employer_hsa = usd(480.0).into();
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    // 15 of 24 pay dates are through Aug 14: $480 × 15/24 is $300.
    assert_eq!(cents(&p.so_far.boxes.hsa), 30_000);
    let items = cents(&p.so_far.items.states[0].state_income);
    let boxes = cents(&p.so_far.boxes.states[0].wages);
    assert_eq!(boxes - items, 30_000);
}

fn hsa_paycheck(adjusted: bool) -> PayFigures {
    let mut f = paycheck();
    f.pretax.push(pretax(PretaxKind::PRETAX_KIND_HSA, 400.0));
    if adjusted {
        // Social Security and Medicare on the $5,433.33 the HSA leaves.
        f.social_security_withheld = usd(336.87).into();
        f.medicare_withheld = usd(78.78).into();
    }
    f
}

fn fifteen_stubs(f: &PayFigures) -> Vec<Stub> {
    [
        (1, 15),
        (1, 30),
        (2, 13),
        (2, 27),
        (3, 13),
        (3, 31),
        (4, 15),
        (4, 30),
        (5, 15),
        (5, 29),
        (6, 15),
        (6, 30),
        (7, 15),
        (7, 31),
        (8, 14),
    ]
    .iter()
    .map(|&(m, d)| {
        stub(
            &format!("acme-s{m}-{d}"),
            m,
            d,
            PaycheckKind::PAYCHECK_KIND_REGULAR,
            f.clone(),
        )
    })
    .collect()
}

// HSA through payroll over the limit comes off the paychecks to come, so it
// is in their wages; what is over on paychecks already made is income.
// California's wage lines stay, as they count HSA already.
#[test]
fn hold_hsa() {
    let m = job(vec![stubs(
        "acme-p",
        vec![stub(
            "acme-s1",
            8,
            14,
            PaycheckKind::PAYCHECK_KIND_REGULAR,
            hsa_paycheck(true),
        )],
    )]);
    let ctx = ctx_at(9, 8, None);
    let (before, _) = expand(&ctx, &m, &[]);
    let mut jobs = vec![employer::expand(&ctx, &m, &[]).unwrap()];
    // 24 paychecks put in $9,600 against a $4,400 limit: $5,200 over. Only
    // Aug 14 is made; the 23 pay dates without evidence are to come, and
    // hold all of it.
    employer::hold_hsa("you", money::dollars(4_400), &mut jobs).unwrap();
    let p = jobs[0].projection().unwrap();
    assert!(
        cents(&p.hsa_over_in_wages) == 520_000
            && cents(&p.hsa_over_in_income) == 0
            && cents(&p.hsa_over) == 520_000
    );
    assert_eq!(
        cents(&p.year.boxes.wages) - cents(&before.year.boxes.wages),
        520_000
    );
    assert_eq!(
        cents(&p.year.boxes.states[0].wages),
        cents(&before.year.boxes.states[0].wages)
    );
    assert!(!jobs[0].workings().is_empty(), "the excess has no working");
    // Social Security and Medicare wages rise by the excess too, and their
    // tax is worked out on them. The cut runs from Dec 31 back through the
    // four pay dates before the paystub, Jun 15 to Jul 31, which copy it:
    // on Jul 31, its HSA line cut to nothing, it is 6.2% and 1.45% of the
    // whole $5,833.33, not the paystub's figures on $5,433.33.
    let jul = pay(item(&p, "employers/acme/2026-07-31/regular"));
    let (ss, med) = (
        cents(&jul.social_security_withheld),
        cents(&jul.medicare_withheld),
    );
    assert!(near(ss, 36_167) && near(med, 8_458), "{ss} {med}");
}

// An excess beyond what the paychecks to come hold is on paychecks already
// made: other income.
#[test]
fn hold_hsa_on_paychecks_made() {
    let m = job(vec![stubs("acme-p", fifteen_stubs(&hsa_paycheck(false)))]);
    let ctx = ctx_at(9, 8, None);
    let mut jobs = vec![employer::expand(&ctx, &m, &[]).unwrap()];
    // 15 paychecks made hold $6,000 and 9 to come $3,600: $5,200 over a
    // $4,400 limit, $3,600 of it in wages and $1,600 income.
    employer::hold_hsa("you", money::dollars(4_400), &mut jobs).unwrap();
    let p = jobs[0].projection().unwrap();
    assert!(cents(&p.hsa_over_in_wages) == 360_000 && cents(&p.hsa_over_in_income) == 160_000);
}

// A typed YearTotal fixes the year's figures on the paychecks it fills: the
// HSA over the limit comes off only the paychecks the model works out, and
// the rest of it is income. The figures the split would have made are each
// working's automatic.
#[test]
fn hold_hsa_keeps_typed_year_total() {
    // The year as 24 paychecks like the paystubs.
    let year = PayFigures {
        gross: usd(5833.33 * 24.0).into(),
        pretax: vec![pretax(PretaxKind::PRETAX_KIND_HSA, 400.0 * 24.0)],
        federal_withheld: usd(700.0 * 24.0).into(),
        social_security_withheld: usd(336.87 * 24.0).into(),
        medicare_withheld: usd(78.78 * 24.0).into(),
        ..Default::default()
    };
    let m = job(vec![
        stubs("acme-p", fifteen_stubs(&hsa_paycheck(true))),
        year_total("acme-yt", year.clone(), day(8, 14)),
    ]);
    let ctx = ctx_at(9, 8, None);
    let mut jobs = vec![employer::expand(&ctx, &m, &[]).unwrap()];
    // $9,600 against a $4,400 limit: $5,200 over, none of it on paychecks
    // the model works out.
    employer::hold_hsa("you", money::dollars(4_400), &mut jobs).unwrap();
    let p = jobs[0].projection().unwrap();
    assert!(cents(&p.hsa_over_in_wages) == 0 && cents(&p.hsa_over_in_income) == 520_000);
    let y = &p.year.items;
    for (name, got, want) in [
        (
            "Social Security",
            &y.social_security_withheld,
            &year.social_security_withheld,
        ),
        ("Medicare", &y.medicare_withheld, &year.medicare_withheld),
        ("federal", &y.federal_withheld, &year.federal_withheld),
    ] {
        assert!(near(cents(got), cents(want)), "{name} {got:?} {want:?}");
    }
    let hsa = y
        .pretax
        .iter()
        .filter(|x| x.kind == PretaxKind::PRETAX_KIND_HSA)
        .map(|x| cents(&x.amount))
        .next_back()
        .unwrap_or(0);
    assert!(near(hsa, 960_000), "{hsa}");
    // The split would have taken the $3,600 the typed paychecks hold off
    // their HSA lines, and worked out Social Security and Medicare on it.
    let mut auto = std::collections::HashMap::new();
    for w in jobs[0].workings() {
        if w.automatic.is_set() {
            auto.insert(w.path.clone(), cents(&w.automatic));
        }
    }
    let at = "projection.employers[acme].year.items.";
    let get = |k: &str| auto.get(k).copied().unwrap_or(0);
    assert!(near(
        get(&format!("{at}social_security_withheld")),
        cents(&year.social_security_withheld) + dollars(cents(&usd(3600.0 * 0.062)))
    ));
    assert!(near(
        get(&format!("{at}medicare_withheld")),
        cents(&year.medicare_withheld) + dollars(cents(&usd(3600.0 * 0.0145)))
    ));
    assert_eq!(get(&format!("{at}pretax[1].amount")), 600_000);
    assert_eq!(get("projection.employers[acme].hsa_over_in_wages"), 360_000);
}

// Living in California through August, then Texas, which taxes no wages.
// The paychecks from September have Texas lines with their wages, rounded
// to the dollar, and nothing withheld, so Texas has the eight paychecks'
// wages; California's line has its employer HSA, $0.
#[test]
fn move_to_a_state_with_no_wage_tax() {
    let facts = lived(&[("CA", (1, 1), (8, 31)), ("TX", (9, 1), (12, 31))]);
    let mut m = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 14)])]);
    m.work = where_lived().into();
    let ctx = ctx_at(9, 8, Some(facts));
    let (p, _) = expand(&ctx, &m, &[]);
    let dec = pay(item(&p, "employers/acme/2026-12-31/regular"));
    let tx = dec
        .states
        .iter()
        .rfind(|s| s.state == "TX")
        .expect("a Texas line");
    assert!(
        near(cents(&tx.state_income), 533_333)
            && tx.tax_withheld.is_set()
            && cents(&tx.tax_withheld) == 0
    );
    let rest_tx = box_line(&p.rest, "TX").expect("the rest's Texas line");
    assert!(near(cents(&rest_tx.wages), 8 * 533_333));
    let ca = box_line(&p.year, "CA").expect("California's year line");
    assert!(ca.employer_hsa.is_set() && cents(&ca.employer_hsa) == 0);
}

// Living in California through August, then Vermont, with a year total
// typed with a California line. State wages follow the state of each
// period: California on the paychecks to Aug 31, Vermont from Sep 15. Each
// state's wages for the year are the sum of its paychecks' lines, with
// California's share of the employer HSA.
#[test]
fn year_total_follows_the_work_state() {
    let facts = lived(&[("CA", (1, 1), (8, 31)), ("VT", (9, 1), (12, 31))]);
    let year = PayFigures {
        gross: usd(140_000.0).into(),
        pretax: vec![pretax(PretaxKind::PRETAX_KIND_401K, 12_000.0)],
        states: vec![swh("CA", Some(90_000.0), Some(5_000.0))],
        ..Default::default()
    };
    let mut m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        year_total("acme-yt", year, day(8, 14)),
    ]);
    m.work = where_lived().into();
    m.employer_hsa = usd(600.0).into();
    m.state_withholding = vec![StateWithholding {
        state: "VT".to_string(),
        per_paycheck: usd(200.0).into(),
        ..Default::default()
    }];
    let ctx = ctx_at(9, 8, Some(facts));
    let (p, _) = expand(&ctx, &m, &[]);
    let state_of = |id: &str| -> Vec<String> {
        pay(item(&p, id))
            .states
            .iter()
            .map(|s| s.state.clone())
            .collect()
    };
    assert_eq!(state_of("employers/acme/2026-08-31/regular"), vec!["CA"]);
    assert_eq!(state_of("employers/acme/2026-12-31/regular"), vec!["VT"]);
    for period in [&p.so_far, &p.rest, &p.year] {
        for b in &period.boxes.states {
            let items = period
                .items
                .states
                .iter()
                .rfind(|s| s.state == b.state)
                .map(|s| cents(&s.state_income))
                .unwrap_or(0);
            assert_eq!(
                cents(&b.wages),
                items + cents(&b.employer_hsa),
                "{}",
                b.state
            );
        }
    }
}

// A year total with $6,000 of HSA through payroll and a California line
// from before, $10,000 short. A part has no field for California wages, so
// the year's California wages are box 1 and the payroll HSA, not the stale
// line.
#[test]
fn year_total_california_follows_box1() {
    let year = PayFigures {
        gross: usd(140_000.0).into(),
        pretax: vec![
            pretax(PretaxKind::PRETAX_KIND_401K, 12_000.0),
            pretax(PretaxKind::PRETAX_KIND_HSA, 6_000.0),
        ],
        states: vec![swh("CA", Some(112_000.0), None)],
        ..Default::default()
    };
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        year_total("acme-yt", year, day(8, 14)),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    let y = &p.year;
    let hsa: i64 = y
        .items
        .pretax
        .iter()
        .filter(|x| x.kind == PretaxKind::PRETAX_KIND_HSA)
        .map(|x| cents(&x.amount))
        .sum();
    let ca = &y.boxes.states[0];
    assert!(ca.state == "CA" && cents(&ca.wages) == cents(&y.boxes.wages) + hsa);
}

// As of Sep 8, Acme's last paycheck was Aug 31, with no paystub for it yet,
// and Beta started Sep 1. Aug 31 is paid already, so max-out can't set it:
// it keeps the paystub's $500. What is left of the limit is shared equally
// over Beta's paychecks to come.
#[test]
fn max_out_across_a_job_change() {
    let ctx = ctx_at(9, 8, Some(born(1990)));
    let ma = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        ends("acme-end", day(8, 31), None),
    ]);
    let mut other = job(vec![]);
    other.id = "beta".to_string();
    other.name = "Beta".to_string();
    other.default_part = each_paycheck("beta-default", gross(6000.0)).into();
    other.starts = day(9, 1).into();
    let mut jobs = vec![
        employer::expand(&ctx, &ma, &[]).unwrap(),
        employer::expand(&ctx, &other, &[]).unwrap(),
    ];
    employer::max_out(&ctx, "you", &max_part(), &mut jobs).unwrap();
    let pa = jobs[0].projection().unwrap();
    let pb = jobs[1].projection().unwrap();
    assert_eq!(
        cents(&pay(item(&pa, "employers/acme/2026-08-31/regular")).pretax[0].amount),
        50000
    );
    assert_eq!(
        cents(&pa.year.boxes.k401) + cents(&pb.year.boxes.k401),
        2_450_000
    );
}

// A job made with only its basic fields, no paystub and no figures per
// paycheck, and a year total typed with its gross and 401(k). The year
// total fills the pay dates after as_of with equal shares.
#[test]
fn year_total_on_a_job_with_no_paychecks() {
    let ctx = ctx_at(9, 8, None);
    let yt = year_total(
        "acme-yt",
        PayFigures {
            gross: usd(36_000.0).into(),
            pretax: vec![pretax(PretaxKind::PRETAX_KIND_401K, 9_000.0)],
            ..Default::default()
        },
        day(9, 8),
    );
    let mut m = job(vec![yt]);
    m.starts = day(9, 1).into();
    for d in [
        EmployerDefault {
            id: "acme-default".to_string(),
            rest: Some(Rest::SameAsLatest(Box::default())),
            ..Default::default()
        },
        each_paycheck("acme-default", PayFigures::default()),
    ] {
        m.default_part = d.into();
        let (p, _) = expand(&ctx, &m, &[]);
        assert_eq!(cents(&p.year.items.gross), 3_600_000);
        assert_eq!(cents(&p.year.boxes.k401), 900_000);
    }
}

// A job with no paystubs whose last paycheck, Jan 10, came before its first
// pay date, so it has no pay dates. Its typed year total still holds, on
// one adjustment item.
#[test]
fn year_total_with_no_pay_dates_left() {
    let year = PayFigures {
        gross: usd(10_000.0).into(),
        federal_withheld: usd(1_000.0).into(),
        states: vec![swh("CA", None, Some(500.0))],
        ..Default::default()
    };
    let m = job(vec![
        year_total("acme-yt", year, day(9, 8)),
        ended("acme-end", day(1, 10)),
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    let y = &p.year;
    assert!(cents(&y.items.gross) == 1_000_000 && cents(&y.items.federal_withheld) == 100_000);
    assert_eq!(
        year_line(y, "CA").map(|c| cents(&c.tax_withheld)),
        Some(50_000)
    );
}

// A typed California line on a job whose paychecks to come are all worked
// in Oregon. The line has no paycheck to go on, and still holds: $5,900 of
// California withholding for the year.
#[test]
fn year_total_state_line_with_no_paycheck_there() {
    let year = PayFigures {
        gross: usd(100_000.0).into(),
        federal_withheld: usd(12_000.0).into(),
        states: vec![swh("CA", None, Some(5_900.0))],
        ..Default::default()
    };
    let mut m = job(vec![year_total("acme-yt", year, day(9, 8))]);
    m.work = state_work("OR").into();
    let ctx = ctx_at(9, 8, None);
    let (p, j) = expand(&ctx, &m, &[]);
    assert_eq!(
        year_line(&p.year, "CA").map(|c| cents(&c.tax_withheld)),
        Some(590_000)
    );
    assert_eq!(cents(&p.year.items.federal_withheld), 1_200_000);
    assert!(j.workings().iter().any(|w| w.path
        == "projection.employers[acme].year.items.states[CA].tax_withheld"
        && w.parts == vec!["acme-yt".to_string()]));
}

// Acme's last paycheck is Sep 30 and Beta starts Oct 1 with a year total,
// whose figures max-out leaves alone, so Acme's two paychecks to come take
// what is left of the limit. Max-out can't put more into a paycheck than is
// left of it after Social Security and Medicare, which the 401(k) doesn't
// lower, so no paycheck to come pays out less than nothing, and the year
// stays within the limit.
#[test]
fn max_out_leaves_take_home() {
    let ctx = ctx_at(9, 8, Some(born(1990)));
    // Acme's paystub has HSA and other benefits, which California's wages
    // keep or lose differently from box 1.
    let mut f = paycheck();
    f.pretax.push(pretax(PretaxKind::PRETAX_KIND_HSA, 150.0));
    f.pretax
        .push(pretax(PretaxKind::PRETAX_KIND_OTHER_BENEFITS, 48.0));
    let ma = job(vec![
        stubs(
            "acme-p",
            vec![stub(
                "acme-s1",
                8,
                14,
                PaycheckKind::PAYCHECK_KIND_REGULAR,
                f,
            )],
        ),
        ends("acme-end", day(9, 30), None),
    ]);
    let mut beta = job(vec![year_total(
        "beta-yt",
        PayFigures {
            gross: usd(1_800.0).into(),
            pretax: vec![pretax(PretaxKind::PRETAX_KIND_401K, 600.0)],
            social_security_withheld: usd(111.60).into(),
            medicare_withheld: usd(26.10).into(),
            ..Default::default()
        },
        day(9, 8),
    )]);
    beta.id = "beta".to_string();
    beta.name = "Beta".to_string();
    beta.starts = day(10, 1).into();
    let mut jobs = vec![
        employer::expand(&ctx, &ma, &[]).unwrap(),
        employer::expand(&ctx, &beta, &[]).unwrap(),
    ];
    employer::max_out(&ctx, "you", &max_part(), &mut jobs).unwrap();
    let mut total = 0;
    for j in &mut jobs {
        let p = j.projection().unwrap();
        total += cents(&p.year.boxes.k401);
        for it in &p.items {
            if it.status != ItemStatus::ITEM_STATUS_EXPECTED || it.date.month < 9 {
                continue;
            }
            let c = pay(it);
            let mut out = cents(&c.federal_withheld)
                + cents(&c.social_security_withheld)
                + cents(&c.medicare_withheld);
            out += c.pretax.iter().map(|x| cents(&x.amount)).sum::<i64>();
            out += c.states.iter().map(|s| cents(&s.tax_withheld)).sum::<i64>();
            // Items are whole dollars, each rounded on its own.
            assert!(
                cents(&c.gross) - out >= -100,
                "{} pays out {}",
                it.id,
                cents(&c.gross) - out
            );
        }
    }
    assert!(total <= 2_450_000, "{total}");
}

// Beta is added with only its basic fields, so its retirement plan isn't
// said, at $500 a paycheck, $12,000 for the year. A $24,000 plan total is
// shared over both jobs' paychecks to come. Until the result's question is
// answered, Beta's share has no pre-tax effect, so no assumption lowers
// the tax.
#[test]
fn plan_total_on_a_job_with_no_plan_said() {
    let ctx = ctx_at(9, 8, Some(born(1990)));
    let ma = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 14)])]);
    let beta = EmployerModel {
        id: "beta".to_string(),
        name: "Beta".to_string(),
        person: "you".to_string(),
        pay_frequency: PayFrequency::PAY_FREQUENCY_SEMI_MONTHLY.into(),
        work: state_work("CA").into(),
        supplemental: SupplementalMethod::SUPPLEMENTAL_METHOD_FLAT.into(),
        default_part: each_paycheck("beta-default", gross(500.0)).into(),
        ..Default::default()
    };
    let mut jobs = vec![
        employer::expand(&ctx, &ma, &[]).unwrap(),
        employer::expand(&ctx, &beta, &[]).unwrap(),
    ];
    let total = RetirementPart {
        id: "you-total".to_string(),
        part: Some(RetirementKind::WorkplacePlanTotal(Box::new(
            WorkplacePlanTotal {
                total: usd(24_000.0).into(),
                ..Default::default()
            },
        ))),
        ..Default::default()
    };
    let notes = employer::max_out(&ctx, "you", &total, &mut jobs).unwrap();
    let pa = jobs[0].projection().unwrap();
    let pb = jobs[1].projection().unwrap();
    // Beta's share has no pre-tax effect until its plan is said: it is on
    // the Roth line, and Beta's box 1 is all of its pay.
    let yb = &pb.year.boxes;
    let (in_a, in_b) = (cents(&pa.year.boxes.k401), cents(&yb.roth_401k));
    assert!(in_b > 0 && in_a + in_b == 2_400_000, "{in_a} {in_b}");
    assert!(cents(&yb.k401) == 0 && cents(&yb.wages) == 1_200_000);
    assert!(
        question_ids(&notes)
            .iter()
            .any(|q| q == "employers/beta/retirement-plan")
    );
}

// A job's work split by days, with California at 0 days, while its paystubs
// withheld California tax. The tax was paid, so California's box line keeps
// it, for so far and the year.
#[test]
fn split_work_keeps_state_withholding() {
    let mut m = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 14)])]);
    m.work = split_work(&[("CA", 0)]).into();
    let ctx = ctx_at(9, 8, None);
    let (p, _) = expand(&ctx, &m, &[]);
    for (name, period) in [("so far", &p.so_far), ("year", &p.year)] {
        let items = year_line(period, "CA").expect("California on the items");
        assert_ne!(cents(&items.tax_withheld), 0, "{name}");
        let b = box_line(period, "CA").expect("California's box line");
        assert_eq!(cents(&b.withheld), cents(&items.tax_withheld), "{name}");
    }
}

// A finished year read from the W-2, with federal and California
// withholding so far overridden. The W-2 boxes follow the overrides as the
// items do.
#[test]
fn override_on_a_w2_year() {
    let ctx = Context::new(
        2026,
        Some(&commonv1::Date {
            year: 2027,
            month: 3,
            day: 1,
            ..Default::default()
        }),
        None,
    )
    .unwrap();
    let w2 = FormW2 {
        tax_year: 2026,
        wages_tips_other_compensation: usd(120000.0).into(),
        federal_tax_withheld: usd(17500.0).into(),
        social_security_tax_withheld: usd(7440.0).into(),
        medicare_tax_withheld: usd(1740.0).into(),
        state_tax_withholding: vec![swh("CA", Some(120000.0), Some(6513.0))],
        ..Default::default()
    };
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        part(
            "acme-w2",
            Part::FromW2(Box::new(FromW2 {
                w2: w2.into(),
                ..Default::default()
            })),
        ),
    ]);
    let (p, _) = expand(
        &ctx,
        &m,
        &[
            over("federal_withheld", 8750.0),
            over("states[CA].tax_withheld", 6514.0),
        ],
    );
    for (name, period) in [("so far", &p.so_far), ("year", &p.year)] {
        let b = &period.boxes;
        assert_eq!(cents(&b.federal_withheld), 875_000, "{name}");
        assert!(
            !b.states.is_empty() && cents(&b.states[0].withheld) == 651_400,
            "{name}"
        );
    }
}

// Work split by days with California among the states. California doesn't
// exclude HSA contributions, so its box wages are its share of box 1 and
// the payroll HSA, and its share of the employer's HSA, which its line
// names: the line's wages are its items' wages and that employer HSA, as
// without a split.
#[test]
fn split_work_california_hsa() {
    let mut f = paycheck();
    f.pretax.push(pretax(PretaxKind::PRETAX_KIND_HSA, 150.0));
    for days in [240, 0] {
        let mut m = job(vec![stubs(
            "acme-p",
            vec![stub(
                "acme-s1",
                8,
                14,
                PaycheckKind::PAYCHECK_KIND_REGULAR,
                f.clone(),
            )],
        )]);
        m.employer_hsa = usd(480.0).into();
        m.work = split_work(&[("CA", days)]).into();
        let ctx = ctx_at(9, 8, None);
        let (p, _) = expand(&ctx, &m, &[]);
        let so = &p.so_far;
        let items = year_line(so, "CA").expect("California on the items");
        let b = box_line(so, "CA").expect("California's box line");
        assert!(
            cents(&b.employer_hsa) == 30_000
                && cents(&b.wages) == cents(&items.state_income) + cents(&b.employer_hsa),
            "CA {days} days: {b:?} {items:?}"
        );
    }
}

// A year total typed with California withholding, and a pay change from
// Oct 1 with its own California line. The whole-year part fills first, so
// the typed total holds, and the result says the pay change doesn't change
// it.
#[test]
fn year_total_holds_over_a_pay_change() {
    let yt = year_total(
        "acme-yt",
        PayFigures {
            gross: usd(140_000.0).into(),
            states: vec![swh("CA", None, Some(8_604.0))],
            ..Default::default()
        },
        day(8, 14),
    );
    let mut raise = pay_rate("acme-r", day(10, 1), 7000.0);
    if let Some(Part::PayRate(r)) = &mut raise.part {
        r.paycheck.get_or_insert_default().states = vec![swh("CA", None, Some(450.0))];
    }
    let m = job(vec![
        stubs("acme-p", vec![regular("acme-s1", 8, 14)]),
        yt,
        raise,
    ]);
    let ctx = ctx_at(9, 8, None);
    let (p, j) = expand(&ctx, &m, &[]);
    assert_eq!(
        year_line(&p.year, "CA").map(|c| cents(&c.tax_withheld)),
        Some(860_400)
    );
    assert!(has_question(&j, "employers/acme/year-total/holds/acme-r"));
}

// Not in the Go tests: the errors Go's callers can meet, by their text.
#[test]
fn errors() {
    let ctx = ctx_at(9, 8, None);
    let m = job(vec![stubs("acme-p", vec![regular("acme-s1", 8, 14)])]);
    let mut jobs = vec![employer::expand(&ctx, &m, &[]).unwrap()];
    jobs[0].projection().unwrap();
    assert_eq!(
        jobs[0].projection().unwrap_err().to_string(),
        "model.employers[acme]: Projection called twice"
    );
    assert_eq!(
        employer::max_out(&ctx, "you", &max_part(), &mut jobs)
            .unwrap_err()
            .to_string(),
        "employer: MaxOut after Projection"
    );
    assert_eq!(
        employer::hold_hsa("you", money::dollars(1), &mut jobs)
            .unwrap_err()
            .to_string(),
        "employer: HoldHsa after Projection"
    );
    let err = employer::expand(&ctx, &m, &[over("nope", 1.0)]).unwrap_err();
    assert_eq!(
        err.to_string(),
        "override projection.employers[acme].so_far.items.nope: \"nope\" is not a line of pay figures"
    );
    let err = employer::expand(&ctx, &m, &[over("pretax[3].amount", 1.0)]).unwrap_err();
    assert_eq!(
        err.to_string(),
        "override projection.employers[acme].so_far.items.pretax[3].amount: \"pretax[3].amount\" names no pre-tax line so far"
    );
    let mut bad = job(vec![]);
    bad.first_pay_date = commonv1::Date {
        year: 2026,
        month: 2,
        day: 30,
        ..Default::default()
    }
    .into();
    let err = employer::expand(&ctx, &bad, &[]).unwrap_err();
    assert_eq!(
        err.to_string(),
        "employers[acme].first_pay_date: civil: 2026-02-30 is not a day"
    );
    let mut none = job(vec![]);
    none.pay_frequency = PayFrequency::PAY_FREQUENCY_UNSPECIFIED.into();
    none.default_part = each_paycheck("acme-default", gross(1000.0)).into();
    assert!(
        employer::expand(&ctx, &none, &[])
            .unwrap()
            .projection()
            .unwrap()
            .items
            .is_empty()
    );
}
