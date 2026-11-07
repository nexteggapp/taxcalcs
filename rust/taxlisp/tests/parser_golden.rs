//! The parser golden vectors: golden/testdata/taxlisp/parse.json, which
//! golden/taxlisp_test.go writes from taxlisp/parser.

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use proto::{commonv1, taxlispv1};
use serde::Deserialize;
use taxcalcs_taxlisp::parser;

#[derive(Deserialize)]
struct ParsedFile {
    path: String,
    #[serde(default)]
    federal: bool,
    src: String,
    module: String,
    formatted: String,
    error: String,
}

#[derive(Deserialize)]
struct ParsedExpr {
    src: String,
    expr: String,
    formatted: String,
    error: String,
}

#[derive(Deserialize)]
struct FormattedFloat {
    bits: u64,
    formatted: String,
}

#[derive(Deserialize)]
struct FormattedUsd {
    minor: i64,
    formatted: String,
}

#[derive(Deserialize)]
struct ParseVectors {
    files: Vec<ParsedFile>,
    exprs: Vec<ParsedExpr>,
    floats: Vec<FormattedFloat>,
    usd: Vec<FormattedUsd>,
}

fn vectors() -> ParseVectors {
    golden::read("taxlisp/parse.json")
}

#[test]
fn parse_files() {
    let v = vectors();
    let mut federal = 0;
    for f in &v.files {
        let src = if f.federal {
            federal += 1;
            let p = golden::repo_path("returns/rules/federal").join(&f.path);
            std::fs::read_to_string(&p).unwrap_or_else(|e| panic!("{}: {e}", p.display()))
        } else {
            f.src.clone()
        };
        let ctx = format!("{} {:?}", f.path, f.src);
        match parser::parse_file(&f.path, &src) {
            Ok(mf) => {
                assert_eq!(f.error, "", "{ctx}: Rust parsed what Go rejected");
                let want: taxlispv1::ModuleFile = golden::decode(&f.module);
                golden::assert_same(&ctx, &mf, &want);
                assert_eq!(parser::format_file(&mf), f.formatted, "{ctx}: formatted");
            }
            Err(e) => assert_eq!(e.to_string(), f.error, "{ctx}: error"),
        }
    }
    assert!(federal > 0, "no federal rule files in the vectors");
}

#[test]
fn parse_exprs() {
    for x in &vectors().exprs {
        match parser::parse_expr(&x.src) {
            Ok(e) => {
                assert_eq!(x.error, "", "{:?}: Rust parsed what Go rejected", x.src);
                let want: taxlispv1::Expression = golden::decode(&x.expr);
                golden::assert_same(&x.src, &e, &want);
                assert_eq!(parser::format_expr(&e), x.formatted, "{:?}", x.src);
            }
            Err(e) => assert_eq!(e.to_string(), x.error, "{:?}", x.src),
        }
    }
}

#[test]
fn format_floats() {
    for f in &vectors().floats {
        let x = f64::from_bits(f.bits);
        assert_eq!(
            parser::format_float(x),
            f.formatted,
            "{x:e} ({:#x})",
            f.bits
        );
    }
}

#[test]
fn format_usd() {
    for u in &vectors().usd {
        let m = commonv1::Money {
            currency: "USD".to_string(),
            minor: u.minor,
            ..Default::default()
        };
        assert_eq!(parser::format_usd(&m), u.formatted, "{}", u.minor);
    }
}
