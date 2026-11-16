//! The estimated tax rules against golden/testdata/estimated/taxlisp.json,
//! which golden/estimated_answer_test.go writes from the Go engine once its
//! answers match the Go calculators' vectors: every household of each landed
//! jurisdiction, so the Rust engine runs the same rules to the same answers.
//!
//! The reference jurisdiction ZZ's rules are a test file,
//! golden/testdata/estimated/reference.tlisp, loaded after the production
//! rules and called through reference-prepare, as the Go test does.

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use proto::returnsv1;
use serde::Deserialize;
use taxcalcs_returns::rules::federal;
use taxlisp::eval::{Evaluator, ProtoMessageVal, Value};
use taxlisp::{expand, parser, types};

#[derive(Deserialize)]
struct EstimatedCase {
    jurisdiction: String,
    year: i64,
    name: String,
    inputs: String,
    estimated: String,
}

/// The federal and state rules with the reference rules after them.
fn reference_engine() -> Evaluator {
    let mut combined = federal::load_engine().expect("engine").module;
    let path = golden::path("estimated/reference.tlisp");
    let src = std::fs::read_to_string(&path).expect("reference rules");
    let mut module = parser::parse_file("testdata/estimated/reference.tlisp", &src).expect("parse");
    expand::expand_file(&mut module).expect("expand");
    combined.expressions.extend(module.expressions);
    let mut checker = types::Checker::new(types::global_files().expect("pool"));
    checker.check_module(&combined).expect("typecheck");
    let pool = taxlisp::eval::default_pool().expect("pool");
    let ev = Evaluator::new(Some(pool)).expect("evaluator");
    ev.eval_module(&combined).expect("initialize");
    ev
}

fn reference_prepare(ev: &Evaluator, inputs: &returnsv1::ReturnInputs) -> returnsv1::ReturnOutputs {
    let arg = ProtoMessageVal::from_message(ev.pool, inputs).expect("inputs");
    match ev
        .call("reference-prepare", vec![Value::ProtoMessage(arg)])
        .expect("reference-prepare")
    {
        Value::ProtoMessage(pm) => pm.to_message().expect("outputs"),
        other => panic!("reference-prepare returned {}", other.go_type()),
    }
}

#[test]
fn estimated_answers_match_go() {
    let cases: Vec<EstimatedCase> = golden::read("estimated/taxlisp.json");
    assert!(cases.len() >= 46, "only {} cases", cases.len());
    let eng = federal::default_engine().expect("default engine");
    let reference = reference_engine();
    for c in &cases {
        let ctx = format!("{} {} {:?}", c.jurisdiction, c.year, c.name);
        let inputs: returnsv1::ReturnInputs = golden::decode(&c.inputs);
        let want: returnsv1::ReturnOutputs = golden::decode(&c.estimated);
        let got = if c.jurisdiction == "ZZ" {
            reference_prepare(&reference, &inputs)
        } else {
            eng.prepare(&inputs)
                .unwrap_or_else(|e| panic!("{ctx}: Prepare failed: {e}"))
        };
        golden::assert_same(&ctx, &got.estimated, &want.estimated);
    }
}
