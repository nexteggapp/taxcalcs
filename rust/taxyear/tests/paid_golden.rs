//! The paid vectors in golden/testdata/paid, which golden/paid_test.go
//! writes: for every scenario of every taxyear test case that reaches the
//! estimated-payment rules, what taxyear gives paid.Compute and what it
//! returns, and paid/variants.json.gz, one 2026 case with its facts,
//! projection or overrides changed to reach every jurisdiction and the
//! refusals.

use std::io::Read as _;
use std::sync::Arc;

use proto::{commonv1, factsv1, scenariosv1};
use serde::Deserialize;
use taxcalcs_taxyear::model::{Context, taxpaid};
use taxcalcs_taxyear::paid;

#[derive(Deserialize)]
struct PaidVector {
    case: String,
    year: i64,
    as_of: String,
    facts: String,
    projection: String,
    status: i32,
    tax_2210: i64,
    agi: i64,
    excess_social_security: i64,
    tax_paid: Vec<String>,
    overrides: Vec<String>,
    error: String,
    estimated: Vec<String>,
    questions: Vec<String>,
    left_out: Vec<String>,
    federal_payments: i64,
    tax_paid_projections: Vec<String>,
}

fn vectors(name: &str) -> Vec<PaidVector> {
    let p = golden::path(name);
    let f = std::fs::File::open(&p).unwrap_or_else(|e| panic!("reading {}: {e}", p.display()));
    let mut text = String::new();
    flate2::read::GzDecoder::new(f)
        .read_to_string(&mut text)
        .unwrap_or_else(|e| panic!("decompressing {}: {e}", p.display()));
    serde_json::from_str(&text).unwrap_or_else(|e| panic!("parsing {}: {e}", p.display()))
}

fn decode_all<M: buffa::Message + Default>(list: &[String]) -> Vec<M> {
    list.iter().map(|s| golden::decode(s)).collect()
}

/// A vector's message, boxed: the facts and projections are large for a
/// debug build's test thread stack.
fn dec<M: buffa::Message + Default>(b64: &str) -> Box<M> {
    Box::new(golden::decode(b64))
}

fn status_of(n: i32) -> filing::Status {
    match n {
        1 => filing::Status::Single,
        2 => filing::Status::MarriedFilingJointly,
        3 => filing::Status::MarriedFilingSeparately,
        4 => filing::Status::HeadOfHousehold,
        5 => filing::Status::SurvivingSpouse,
        _ => filing::Status::Unspecified,
    }
}

#[test]
fn paid_compute() {
    let vs = vectors("paid/paid.json.gz");
    assert!(vs.len() > 50, "only {} vectors", vs.len());
    for v in &vs {
        check(v);
    }
}

#[test]
fn paid_variants() {
    let vs = vectors("paid/variants.json.gz");
    assert!(vs.len() > 50, "only {} vectors", vs.len());
    for v in &vs {
        check(v);
    }
}

fn check(v: &PaidVector) {
    let at = &v.case;
    let as_of: Box<commonv1::Date> = dec(&v.as_of);
    let facts: Box<factsv1::TaxYearFacts> = dec(&v.facts);
    let ctx = Context::new(v.year, Some(&as_of), Some(Arc::new(*facts)))
        .unwrap_or_else(|e| panic!("{at}: {e}"));
    let proj: Box<scenariosv1::YearProjection> = dec(&v.projection);
    let overrides = decode_all::<scenariosv1::Override>(&v.overrides);
    let status = status_of(v.status);
    assert_eq!(status as i32, v.status, "{at}: status");

    let mut b = paid::new_inputs(Some(&ctx), status, &proj);
    b.federal(
        money::USCents(v.tax_2210),
        money::USCents(v.agi),
        money::USCents(v.excess_social_security),
    )
    .overrides(&overrides);
    for m in decode_all::<scenariosv1::TaxPaidModel>(&v.tax_paid) {
        let p = taxpaid::expand(&ctx, &m, &ctx.facts().adjacent_payments)
            .unwrap_or_else(|e| panic!("{at}: expand: {e}"));
        b.payments(p);
    }
    let mut inputs = b.build().unwrap_or_else(|e| panic!("{at}: {e}"));
    let answer = match paid::compute(&mut inputs) {
        Err(e) => {
            assert_eq!(e.to_string(), v.error, "{at}: error");
            return;
        }
        Ok(a) => {
            assert_eq!(v.error, "", "{at}: Go fails, Rust does not");
            a
        }
    };
    let want = decode_all::<scenariosv1::Estimated>(&v.estimated);
    assert_eq!(answer.estimated().len(), want.len(), "{at}: estimated");
    for (got, want) in answer.estimated().iter().zip(&want) {
        golden::assert_same(
            &format!("{at}: estimated[{}]", want.jurisdiction),
            got,
            want,
        );
    }
    let questions: Vec<scenariosv1::Question> = answer
        .notes()
        .questions()
        .iter()
        .map(|q| q.proto())
        .collect();
    golden::assert_same(
        &format!("{at}: questions"),
        &questions,
        &decode_all::<scenariosv1::Question>(&v.questions),
    );
    assert_eq!(answer.notes().left_out(), v.left_out, "{at}: left out");
    assert_eq!(
        answer.federal_payments().0,
        v.federal_payments,
        "{at}: federal payments"
    );
    let projections: Vec<scenariosv1::TaxPaidProjection> = inputs
        .into_payments()
        .iter_mut()
        .map(|p| p.projection())
        .collect();
    golden::assert_same(
        &format!("{at}: tax paid projections"),
        &projections,
        &decode_all::<scenariosv1::TaxPaidProjection>(&v.tax_paid_projections),
    );
}
