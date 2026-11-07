//! The federal engine and the ReturnPreparer service against the vectors in
//! golden/testdata/returns, which golden/returns_test.go writes: every
//! return the Go federal rule tests prepare (and each for the other tax
//! year), the first-slice fixture, and the service's methods.

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use proto::returnsv1;
use serde::Deserialize;
use taxcalcs_returns::preparer::Server;
use taxcalcs_returns::rules::federal;

#[derive(Deserialize)]
struct ReturnsCase {
    name: String,
    inputs: String,
    outputs: String,
    outputs_error: String,
    federal_same: bool,
    federal: String,
    federal_error: String,
}

#[derive(Deserialize)]
struct PreparerCase {
    name: String,
    method: String,
    request: String,
    response: String,
    error: String,
}

#[test]
fn federal_engine_matches_go() {
    let cases: Vec<ReturnsCase> = golden::read("returns/federal.json");
    assert!(cases.len() >= 40, "only {} cases", cases.len());
    let eng = federal::default_engine().expect("default engine");
    for (i, c) in cases.iter().enumerate() {
        let ctx = format!("case {i} ({})", c.name);
        let inputs: returnsv1::ReturnInputs = golden::decode(&c.inputs);

        let got = eng.prepare(&inputs);
        let want_outputs = if c.outputs_error.is_empty() {
            let want: returnsv1::ReturnOutputs = golden::decode(&c.outputs);
            match &got {
                Ok(out) => golden::assert_same(&format!("{ctx}: Prepare"), out, &want),
                Err(e) => panic!("{ctx}: Prepare failed: {e}"),
            }
            Some(want)
        } else {
            match &got {
                Ok(_) => panic!(
                    "{ctx}: Prepare succeeded, Go failed with {}",
                    c.outputs_error
                ),
                Err(e) => assert_eq!(e.to_string(), c.outputs_error, "{ctx}: Prepare error"),
            }
            None
        };

        let got = eng.prepare_federal(&inputs);
        if !c.federal_error.is_empty() {
            match &got {
                Ok(_) => panic!(
                    "{ctx}: PrepareFederal succeeded, Go failed with {}",
                    c.federal_error
                ),
                Err(e) => assert_eq!(
                    e.to_string(),
                    c.federal_error,
                    "{ctx}: PrepareFederal error"
                ),
            }
            continue;
        }
        let want: returnsv1::FederalReturn = if c.federal_same {
            want_outputs
                .and_then(|o| o.federal.into_option())
                .unwrap_or_default()
        } else {
            golden::decode(&c.federal)
        };
        match &got {
            Ok(fed) => golden::assert_same(&format!("{ctx}: PrepareFederal"), fed, &want),
            Err(e) => panic!("{ctx}: PrepareFederal failed: {e}"),
        }
    }
}

/// Runs one method on the decoded request and compares the response.
fn check<Req, Resp, E>(c: &PreparerCase, method: impl Fn(&Req) -> Result<Resp, E>)
where
    Req: buffa::Message + Default,
    Resp: buffa::Message + Default + PartialEq + std::fmt::Debug,
    E: std::fmt::Display,
{
    let req: Box<Req> = Box::new(golden::decode(&c.request));
    let got = method(&req).map(Box::new);
    if !c.error.is_empty() {
        match got {
            Ok(_) => panic!("{}: succeeded, Go failed with {}", c.name, c.error),
            Err(e) => assert_eq!(e.to_string(), c.error, "{}", c.name),
        }
        return;
    }
    let want: Box<Resp> = Box::new(golden::decode(&c.response));
    match got {
        Ok(resp) => golden::assert_same(&c.name, &resp, &want),
        Err(e) => panic!("{}: failed: {e}", c.name),
    }
}

#[test]
fn preparer_matches_go() {
    let cases: Vec<PreparerCase> = golden::read("returns/preparer.json");
    assert!(cases.len() >= 15, "only {} cases", cases.len());
    let srv = Server::new().expect("server");
    for c in &cases {
        match c.method.as_str() {
            "ListTaxYears" => check(c, |r| srv.list_tax_years(r)),
            "DescribeInputs" => check(c, |r| srv.describe_inputs(r)),
            "Prepare" => check(c, |r| srv.prepare(r)),
            "BuildSubmission" => check(c, |r| srv.build_submission(r)),
            m => panic!("{}: unknown method {m}", c.name),
        }
    }
}
