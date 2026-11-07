//! The model vectors in golden/testdata/models, which golden/models_test.go
//! writes: every model of every taxyear test case expanded by the modules
//! under `model`, and the date and money helpers they share.

use std::sync::Arc;

use money::USCents;
use proto::{commonv1, factsv1, scenariosv1};
use serde::Deserialize;
use taxcalcs_taxyear::internal::civil::{self, Date};
use taxcalcs_taxyear::model::{self, Context, Notes};

#[derive(Deserialize)]
struct ModelsContext {
    cases: Vec<String>,
    year: i64,
    as_of: String,
    facts: String,
    error: String,
    hsa_limits: Vec<HsaVector>,
    models: Vec<ModelVector>,
}

#[derive(Deserialize)]
struct HsaVector {
    person: String,
    limit: i64,
    ok: bool,
    questions: Vec<String>,
    left_out: Vec<String>,
}

#[derive(Deserialize)]
struct ModelVector {
    list: String,
    id: String,
    model: String,
    projection: String,
    error: String,
    questions: Vec<String>,
    left_out: Vec<String>,
    #[serde(default)]
    plan_part: String,
    #[serde(default)]
    payments: Vec<PaymentVector>,
    #[serde(default)]
    open: Vec<[i64; 3]>,
    #[serde(default)]
    spread: String,
}

/// A payment as the vectors compare it: date, amount, last year's, made.
type PaymentRow = ([i64; 3], i64, bool, bool);

#[derive(Deserialize)]
struct PaymentVector {
    on: [i64; 3],
    amount: i64,
    from_last_year: bool,
    made: bool,
}

fn ymd(d: Date) -> [i64; 3] {
    [d.year(), d.month(), d.day()]
}

fn optional<M: buffa::Message + Default>(b64: &str) -> Option<M> {
    if b64.is_empty() {
        None
    } else {
        Some(golden::decode(b64))
    }
}

/// Checks a model's notes against the vector's questions and left out.
fn check_notes(at: &str, n: &Notes, questions: &[String], left_out: &[String]) {
    let got: Vec<scenariosv1::Question> = n.questions().iter().map(|q| q.proto()).collect();
    let want: Vec<scenariosv1::Question> = questions.iter().map(|q| golden::decode(q)).collect();
    golden::assert_same(&format!("{at}: questions"), &got, &want);
    golden::assert_same(
        &format!("{at}: left out"),
        &n.left_out(),
        &left_out.to_vec(),
    );
}

fn check<P: buffa::Message + Default + PartialEq + std::fmt::Debug>(
    at: &str,
    v: &ModelVector,
    got: Result<(P, Notes), model::Error>,
) {
    match got {
        Err(e) => assert_eq!(e.to_string(), v.error, "{at}"),
        Ok((p, n)) => {
            assert_eq!("", v.error, "{at}: Go fails, Rust does not");
            golden::assert_same(at, &p, &golden::decode::<P>(&v.projection));
            check_notes(at, &n, &v.questions, &v.left_out);
        }
    }
}

#[test]
fn models() {
    let contexts: Vec<ModelsContext> = golden::read("models/models.json");
    assert!(!contexts.is_empty());
    let mut models = 0;
    for c in &contexts {
        let name = match c.cases.len() {
            1 => c.cases[0].clone(),
            n => format!("{} and {} more", c.cases[0], n - 1),
        };
        let as_of: Option<commonv1::Date> = optional(&c.as_of);
        let facts: factsv1::TaxYearFacts = golden::decode(&c.facts);
        let ctx = match Context::new(c.year, as_of.as_ref(), Some(Arc::new(facts))) {
            Ok(ctx) => ctx,
            Err(e) => {
                assert_eq!(e.to_string(), c.error, "{name}");
                continue;
            }
        };
        assert_eq!("", c.error, "{name}");
        let people: Vec<String> = ctx.facts().people.iter().map(|p| p.id.clone()).collect();
        assert_eq!(people.len(), c.hsa_limits.len(), "{name}");
        for h in &c.hsa_limits {
            let mut n = Notes::default();
            let (limit, ok) = model::retirement::hsa_limit(&ctx, &h.person, &mut n);
            assert_eq!(
                (limit.0, ok),
                (h.limit, h.ok),
                "{name}: HSA limit of {}",
                h.person
            );
            check_notes(
                &format!("{name}: HSA limit of {}", h.person),
                &n,
                &h.questions,
                &h.left_out,
            );
        }
        for v in &c.models {
            models += 1;
            let at = format!("{name}: {}[{}]", v.list, v.id);
            match v.list.as_str() {
                "businesses" => check(
                    &at,
                    v,
                    model::business::expand(&ctx, &golden::decode(&v.model)),
                ),
                "accounts" => check(
                    &at,
                    v,
                    model::account::expand(&ctx, &golden::decode(&v.model)),
                ),
                "brokers" => check(
                    &at,
                    v,
                    model::broker::expand(&ctx, &golden::decode(&v.model)),
                ),
                "itemised" => check(
                    &at,
                    v,
                    model::itemised::expand(&ctx, &golden::decode(&v.model)),
                ),
                "other_income" => check(
                    &at,
                    v,
                    model::otherincome::expand(&ctx, &golden::decode(&v.model)),
                ),
                "retirement" => {
                    let m: scenariosv1::RetirementModel = golden::decode(&v.model);
                    let got = model::retirement::expand(&ctx, &m).map(|p| (p, Notes::default()));
                    check(&at, v, got);
                    let part = model::retirement::plan_part(&m)
                        .map(|p| p.id.clone())
                        .unwrap_or_default();
                    assert_eq!(part, v.plan_part, "{at}: plan part");
                }
                "tax_paid" => {
                    let m: scenariosv1::TaxPaidModel = golden::decode(&v.model);
                    let mut p =
                        match model::taxpaid::expand(&ctx, &m, &ctx.facts().adjacent_payments) {
                            Err(e) => {
                                assert_eq!(e.to_string(), v.error, "{at}");
                                continue;
                            }
                            Ok(p) => p,
                        };
                    assert_eq!("", v.error, "{at}");
                    let proj = p.projection();
                    golden::assert_same(&at, &proj, &golden::decode(&v.projection));
                    check_notes(&at, p.notes(), &v.questions, &v.left_out);
                    let got: Vec<PaymentRow> = p
                        .payments()
                        .iter()
                        .map(|x| (ymd(x.on()), x.amount().0, x.from_last_year(), x.made()))
                        .collect();
                    let want: Vec<PaymentRow> = v
                        .payments
                        .iter()
                        .map(|x| (x.on, x.amount, x.from_last_year, x.made))
                        .collect();
                    assert_eq!(got, want, "{at}: payments");
                    let y = ctx.year();
                    let dues = [
                        civil::must(y, 4, 15),
                        civil::must(y, 6, 15),
                        civil::must(y, 9, 15),
                        civil::must(y + 1, 1, 15),
                    ];
                    let open = p.open(&dues);
                    let got: Vec<[i64; 3]> = open.iter().map(|d| ymd(*d)).collect();
                    assert_eq!(got, v.open, "{at}: open");
                    p.spread(&open, USCents(1_234_567));
                    golden::assert_same(
                        &format!("{at}: spread"),
                        &p.projection(),
                        &golden::decode(&v.spread),
                    );
                }
                other => panic!("{at}: no model list {other}"),
            }
        }
    }
    assert!(models > 0);
}

#[derive(Deserialize)]
struct Helpers {
    civil: Vec<CivilVector>,
    pairs: Vec<PairVector>,
    cadences: Vec<CadenceVector>,
    shares: Vec<Vec<i64>>,
    scales: Vec<[i64; 4]>,
    dollars: Vec<[i64; 2]>,
    percents: Vec<[i64; 4]>,
    forms_due: Vec<[i64; 4]>,
    rounds: Vec<Vec<i64>>,
    contexts: Vec<CtxVector>,
    from_protos: Vec<FromProto>,
}

#[derive(Deserialize)]
struct CivilVector {
    #[serde(rename = "in")]
    input: [i64; 3],
    error: String,
    string: String,
    long: String,
    short: String,
    weekday: i64,
    plus_40: [i64; 3],
    minus_400: [i64; 3],
    end_of_month: [i64; 3],
    business_day: [i64; 3],
    month_of: [i64; 3],
    proto: String,
}

#[derive(Deserialize)]
struct PairVector {
    a: [i64; 3],
    b: [i64; 3],
    days_from: i64,
    compare: i64,
    later: [i64; 3],
}

#[derive(Deserialize)]
struct CadenceVector {
    dates: Vec<[i64; 3]>,
    months: i64,
    next: [i64; 3],
    through: Vec<[i64; 3]>,
}

#[derive(Deserialize)]
struct FromProto {
    #[serde(rename = "in")]
    input: [i64; 3],
    nil: bool,
    out: [i64; 3],
    zero: bool,
    error: String,
}

#[derive(Deserialize)]
struct CtxVector {
    year: i64,
    as_of: [i64; 3],
    facts: String,
    error: String,
    evidence_through: [i64; 3],
    evidence_months: i64,
    finished: bool,
    lived_in: Vec<String>,
    ages: Vec<i64>,
}

/// The date of a vector's [y, m, d]: Go's zero time is 0001-01-01, the zero
/// Date.
fn date(x: [i64; 3]) -> Date {
    civil::must(x[0], x[1], x[2])
}

#[test]
fn helpers() {
    let h: Helpers = golden::read("models/helpers.json");
    for c in &h.civil {
        let at = format!("{:?}", c.input);
        match civil::new(c.input[0], c.input[1], c.input[2]) {
            Err(e) => assert_eq!(e.to_string(), c.error, "{at}"),
            Ok(d) => {
                assert_eq!("", c.error, "{at}");
                assert_eq!(d.to_string(), c.string, "{at}");
                assert_eq!(d.long(), c.long, "{at}");
                assert_eq!(d.short(), c.short, "{at}");
                assert_eq!(d.weekday() as i64, c.weekday, "{at}");
                assert_eq!(ymd(d.add_days(40)), c.plus_40, "{at}");
                assert_eq!(ymd(d.add_days(-400)), c.minus_400, "{at}");
                assert_eq!(
                    ymd(civil::end_of_month(d.year(), d.month())),
                    c.end_of_month,
                    "{at}"
                );
                assert_eq!(ymd(d.business_day_on_or_before()), c.business_day, "{at}");
                assert_eq!(ymd(model::month_of(d)), c.month_of, "{at}");
                let want: Option<commonv1::Date> = optional(&c.proto);
                assert_eq!(d.proto(), want, "{at}");
            }
        }
    }
    for f in &h.from_protos {
        let p = commonv1::Date {
            year: f.input[0] as i32,
            month: f.input[1] as i32,
            day: f.input[2] as i32,
            ..Default::default()
        };
        let got = civil::from_proto(if f.nil { None } else { Some(&p) });
        let (d, err) = match got {
            Ok(d) => (d, String::new()),
            Err(e) => (Date::default(), e.to_string()),
        };
        assert_eq!(
            (ymd(d), d.is_zero(), err),
            (f.out, f.zero, f.error.clone()),
            "{:?}",
            f.input
        );
    }
    for p in &h.pairs {
        let (a, b) = (date(p.a), date(p.b));
        assert_eq!(a.days_from(b), p.days_from, "{:?} {:?}", p.a, p.b);
        assert_eq!(a.compare(b), p.compare, "{:?} {:?}", p.a, p.b);
        assert_eq!(ymd(civil::later(a, b)), p.later, "{:?} {:?}", p.a, p.b);
    }
    for c in &h.cadences {
        let ds: Vec<Date> = c.dates.iter().map(|d| date(*d)).collect();
        let cad = model::cadence_of(&ds);
        assert_eq!(cad.months(), c.months, "{:?}", c.dates);
        let last = ds.last().copied().unwrap_or(civil::must(2026, 1, 31));
        assert_eq!(ymd(cad.next(last)), c.next, "{:?}", c.dates);
        let through: Vec<[i64; 3]> = cad
            .through(last, civil::must(2026, 12, 31))
            .into_iter()
            .map(ymd)
            .collect();
        assert_eq!(through, c.through, "{:?}", c.dates);
    }
    for s in &h.shares {
        let got: Vec<i64> = model::share(USCents(s[0]), s[1])
            .iter()
            .map(|v| v.0)
            .collect();
        assert_eq!(got, s[2..], "{s:?}");
    }
    for s in &h.scales {
        assert_eq!(
            model::scale(USCents(s[0]), USCents(s[1]), USCents(s[2])).0,
            s[3],
            "{s:?}"
        );
    }
    for d in &h.dollars {
        assert_eq!(model::dollars(USCents(d[0])).0, d[1], "{d:?}");
    }
    for p in &h.percents {
        let q = commonv1::Quantity {
            units: p[0],
            scale: p[1] as i32,
            ..Default::default()
        };
        let (r, ok) = model::percent(Some(&q));
        assert_eq!((r.0, i64::from(ok)), (p[2], p[3]), "{p:?}");
    }
    assert!(!model::percent(None).1);
    for f in &h.forms_due {
        assert_eq!(ymd(model::forms_due(f[0])), [f[1], f[2], f[3]], "{f:?}");
    }
    for run in &h.rounds {
        let mut r = model::Rounder::new();
        for pair in run.chunks(2) {
            assert_eq!(r.round("line", USCents(pair[0])).0, pair[1], "{run:?}");
        }
    }
    for c in &h.contexts {
        let at = format!("{} {:?}", c.year, c.as_of);
        let as_of = (c.as_of != [0, 0, 0]).then(|| commonv1::Date {
            year: c.as_of[0] as i32,
            month: c.as_of[1] as i32,
            day: c.as_of[2] as i32,
            ..Default::default()
        });
        let facts: Option<factsv1::TaxYearFacts> = optional(&c.facts);
        let ctx = match Context::new(c.year, as_of.as_ref(), facts.map(Arc::new)) {
            Err(e) => {
                assert_eq!(e.to_string(), c.error, "{at}");
                continue;
            }
            Ok(ctx) => ctx,
        };
        assert_eq!("", c.error, "{at}");
        assert_eq!(ymd(ctx.evidence_through()), c.evidence_through, "{at}");
        assert_eq!(ctx.evidence_months(), c.evidence_months, "{at}");
        assert_eq!(ctx.finished(), c.finished, "{at}");
        let mut lived = Vec::new();
        let mut d = ctx.year_start().add_days(-3);
        while !d.after(ctx.year_end().add_days(3)) {
            lived.push(ctx.lived_in(d));
            d = d.add_days(5);
        }
        assert_eq!(lived, c.lived_in, "{at}");
        let ages: Vec<i64> = ctx
            .facts()
            .people
            .iter()
            .map(|p| match ctx.age_at_year_end(&p.id) {
                (a, true) => a,
                _ => -1,
            })
            .collect();
        assert_eq!(ages, c.ages, "{at}");
    }
}
