//! The evaluator against golden/testdata/returns/eval.json, which the Go
//! test golden/returns_test.go writes: each module's last value as Go
//! prints it, the message it builds, or the stage that fails and its error.

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use serde::Deserialize;
use taxcalcs_taxlisp::eval::{Evaluator, Value};
use taxcalcs_taxlisp::{parser, types};

#[derive(Deserialize)]
struct Case {
    name: String,
    source: String,
    checked: bool,
    value: String,
    message: String,
    message_type: String,
    stage: String,
    error: String,
}

/// What Rust computes for a case, in the vector's terms.
fn run(c: &Case) -> (String, String, String, Option<Value>) {
    let mf = match parser::parse_file("test.tlisp", &c.source) {
        Ok(mf) => mf,
        Err(e) => return (String::new(), "parse".into(), e.to_string(), None),
    };
    if c.checked {
        let pool = types::global_files().expect("descriptor pool");
        let mut checker = types::Checker::new(pool);
        if let Err(e) = checker.check_module(&mf) {
            return (String::new(), "check".into(), e.to_string(), None);
        }
    }
    let ev = Evaluator::new(None).expect("evaluator");
    match ev.eval_module(&mf) {
        Ok(v) => (v.to_string(), String::new(), String::new(), Some(v)),
        Err(e) => (String::new(), "eval".into(), e.to_string(), None),
    }
}

#[test]
fn eval_matches_go() {
    let cases: Vec<Case> = golden::read("returns/eval.json");
    assert!(cases.len() > 150, "only {} cases", cases.len());
    let mut failures = Vec::new();
    for c in &cases {
        let (value, stage, error, v) = run(c);
        if (value.as_str(), stage.as_str(), error.as_str())
            != (c.value.as_str(), c.stage.as_str(), c.error.as_str())
        {
            failures.push(format!(
                "{}:\n  Go:   value={:?} stage={:?} error={:?}\n  Rust: value={value:?} stage={stage:?} error={error:?}",
                c.name, c.value, c.stage, c.error
            ));
            continue;
        }
        if !c.message_type.is_empty() {
            let Some(Value::ProtoMessage(pm)) = v else {
                failures.push(format!("{}: want a {} message", c.name, c.message_type));
                continue;
            };
            if pm.full_name() != c.message_type {
                failures.push(format!(
                    "{}: message {} != {}",
                    c.name,
                    pm.full_name(),
                    c.message_type
                ));
                continue;
            }
            let pool = taxcalcs_taxlisp::eval::default_pool().expect("pool");
            let idx = pool.message_index(&c.message_type).expect("message type");
            let want = buffa_descriptor::DynamicMessage::decode(
                std::sync::Arc::clone(pool),
                idx,
                &golden::bytes(&c.message),
            )
            .expect("decode Go message");
            let got = pm.msg.to_dynamic().expect("to dynamic");
            if got != want {
                failures.push(format!(
                    "{}: message differs\n  Go:   {want:?}\n  Rust: {got:?}",
                    c.name
                ));
            }
        }
    }
    assert!(
        failures.is_empty(),
        "{} of {} cases differ:\n{}",
        failures.len(),
        cases.len(),
        failures.join("\n")
    );
}
