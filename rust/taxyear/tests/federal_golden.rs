//! The federal vectors in golden/testdata/federal, which
//! golden/federal_test.go writes: for every scenario of every taxyear test
//! case that reaches the return layer, what federal.Compute is given and
//! what it returns.

use std::io::Read as _;
use std::sync::Arc;

use proto::{commonv1, factsv1, federalv1, returnsv1, scenariosv1};
use serde::Deserialize;
use taxcalcs_taxyear::federal;
use taxcalcs_taxyear::model::Context;

#[derive(Deserialize)]
struct FederalVector {
    case: String,
    year: i64,
    as_of: String,
    facts: String,
    projection: String,
    inputs: String,
    error: String,
    filed: String,
    has_rules: bool,
    status: i32,
    return_inputs: String,
    outputs: String,
    form: String,
    summary: String,
    working: Vec<String>,
    questions: Vec<String>,
    left_out: Vec<String>,
    agi: i64,
    total_tax: i64,
    excess_social_security: i64,
    tax_2210: i64,
    excess_contribution_tax: i64,
    split: String,
}

fn vectors() -> Vec<FederalVector> {
    let p = golden::path("federal/federal.json.gz");
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

fn questions(n: &taxcalcs_taxyear::model::Notes) -> Vec<scenariosv1::Question> {
    n.questions().iter().map(|q| q.proto()).collect()
}

/// A vector's message, boxed: the return's messages are tens of
/// kilobytes, too much for a debug build's test thread stack.
fn dec<M: buffa::Message + Default>(b64: &str) -> Box<M> {
    Box::new(golden::decode(b64))
}

#[test]
fn federal_compute() {
    let vs = vectors();
    assert!(vs.len() > 50, "only {} vectors", vs.len());
    for v in &vs {
        check(v);
    }
}

/// Checks one scenario's return.
fn check(v: &FederalVector) {
    let at = &v.case;
    let as_of: Box<commonv1::Date> = dec(&v.as_of);
    let facts: Box<factsv1::TaxYearFacts> = dec(&v.facts);
    let ctx = Context::new(v.year, Some(&as_of), Some(Arc::new(*facts)))
        .unwrap_or_else(|e| panic!("{at}: {e}"));
    let proj: Box<scenariosv1::YearProjection> = dec(&v.projection);
    let in_: Box<scenariosv1::ScenarioInputs> = dec(&v.inputs);
    assert_eq!(federal::has_rules(v.year), v.has_rules, "{at}: has_rules");

    let mut r = match federal::compute(&ctx, &proj, &in_) {
        Err(e) => {
            assert_eq!(e.to_string(), v.error, "{at}: error");
            return;
        }
        Ok(r) => {
            assert_eq!(v.error, "", "{at}: Go fails, Rust does not");
            Box::new(r)
        }
    };
    check_return(v, &ctx, &proj, &in_, &r);

    // The overpayment split by the filed return.
    let filed: Option<Box<federalv1::Form1040>> = (!v.filed.is_empty()).then(|| dec(&v.filed));
    r.split_overpayment(filed.as_deref());
    golden::assert_same(
        &format!("{at}: split"),
        &*r.form().form_1040,
        &*dec::<federalv1::Form1040>(&v.split),
    );
}

fn check_return(
    v: &FederalVector,
    ctx: &Context,
    proj: &scenariosv1::YearProjection,
    in_: &scenariosv1::ScenarioInputs,
    r: &federal::Return,
) {
    let at = &v.case;
    assert_eq!(r.status() as i32, v.status, "{at}: status");
    let (status, _) = federal::filing_status(&in_.facts);
    assert_eq!(status, r.status(), "{at}: filing_status");
    let (inputs, _) = federal::inputs(ctx, proj, in_);
    golden::assert_same(&format!("{at}: inputs"), &inputs, r.inputs());
    golden::assert_same(
        &format!("{at}: return inputs"),
        r.inputs(),
        &*dec::<returnsv1::ReturnInputs>(&v.return_inputs),
    );
    golden::assert_same(
        &format!("{at}: outputs"),
        r.outputs(),
        &*dec::<returnsv1::ReturnOutputs>(&v.outputs),
    );
    golden::assert_same(
        &format!("{at}: form"),
        r.form(),
        &*dec::<factsv1::FederalReturn>(&v.form),
    );
    golden::assert_same(
        &format!("{at}: summary"),
        r.summary(),
        &*dec::<scenariosv1::ScenarioSummary>(&v.summary),
    );
    golden::assert_same(
        &format!("{at}: working"),
        &r.working().to_vec(),
        &decode_all::<scenariosv1::Working>(&v.working),
    );
    golden::assert_same(
        &format!("{at}: questions"),
        &questions(r.notes()),
        &decode_all::<scenariosv1::Question>(&v.questions),
    );
    assert_eq!(&r.notes().left_out(), &v.left_out, "{at}: left out");
    assert_eq!(r.agi().0, v.agi, "{at}: agi");
    assert_eq!(r.total_tax().0, v.total_tax, "{at}: total tax");
    assert_eq!(
        r.excess_social_security().0,
        v.excess_social_security,
        "{at}: excess social security"
    );
    assert_eq!(r.tax_2210().0, v.tax_2210, "{at}: tax 2210");
    assert_eq!(
        federal::excess_contribution_tax(r.form()).0,
        v.excess_contribution_tax,
        "{at}: excess contribution tax"
    );
}
