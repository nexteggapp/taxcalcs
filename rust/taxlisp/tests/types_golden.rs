//! The typechecker golden vectors: golden/testdata/taxlisp/types.json,
//! which golden/taxlisp_test.go writes from taxlisp/types.

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::collections::{BTreeMap, HashMap, HashSet};

use proto::taxlispv1;
use serde::Deserialize;
use taxcalcs_taxlisp::parser;
use taxcalcs_taxlisp::types::{self, Checker, Type};

#[derive(Deserialize)]
struct TypeCase {
    name: String,
    #[serde(default)]
    src: String,
    #[serde(default)]
    expr: String,
    error: String,
    #[serde(default)]
    globals: BTreeMap<String, String>,
    #[serde(default)]
    expr_type: String,
}

#[derive(Deserialize)]
struct TypeVectors {
    federal: TypeCase,
    cases: Vec<TypeCase>,
}

fn new_checker() -> Checker {
    Checker::new(types::global_files().expect("descriptors"))
}

/// The checker's globals that are not builtins of the same type.
fn defined_globals(c: &Checker, builtins: &HashMap<String, Type>) -> BTreeMap<String, String> {
    c.globals
        .iter()
        .filter(|(name, t)| {
            builtins
                .get(*name)
                .is_none_or(|b| b.to_string() != t.to_string())
        })
        .map(|(name, t)| (name.clone(), t.to_string()))
        .collect()
}

/// What golden/taxlisp_test.go's checkTypeCase records for tc.
fn check_case(tc: &TypeCase) -> (String, BTreeMap<String, String>, String) {
    let builtins = new_checker().globals;
    let mf = parser::parse_file("test.tlisp", &tc.src).expect("parse");
    let mut c = new_checker();
    if let Err(e) = c.check_module(&mf) {
        return (e.to_string(), BTreeMap::new(), String::new());
    }
    let globals = defined_globals(&c, &builtins);
    if tc.expr.is_empty() {
        return (String::new(), globals, String::new());
    }
    let e = parser::parse_expr(&tc.expr).expect("parse expr");
    match c.check_expr(&e, &HashMap::new(), &HashSet::new()) {
        Ok(t) => (String::new(), globals, t.to_string()),
        Err(err) => (err.to_string(), globals, String::new()),
    }
}

#[test]
fn type_cases() {
    let v: TypeVectors = golden::read("taxlisp/types.json");
    for tc in &v.cases {
        if tc.name == "builtins only" {
            let got: BTreeMap<String, String> = new_checker()
                .globals
                .iter()
                .map(|(n, t)| (n.clone(), t.to_string()))
                .collect();
            golden::assert_same("builtins", &got, &tc.globals);
            continue;
        }
        let (err, globals, expr_type) = check_case(tc);
        assert_eq!(err, tc.error, "{}: error", tc.name);
        if tc.error.is_empty() || !tc.expr.is_empty() && !tc.globals.is_empty() {
            golden::assert_same(&tc.name, &globals, &tc.globals);
        }
        assert_eq!(expr_type, tc.expr_type, "{}: expression type", tc.name);
    }
}

#[test]
fn federal_rules() {
    let v: TypeVectors = golden::read("taxlisp/types.json");
    let dir = golden::repo_path("returns/rules/federal");
    let mut names: Vec<String> = std::fs::read_dir(&dir)
        .expect("federal rules dir")
        .filter_map(|e| e.ok())
        .map(|e| e.file_name().to_string_lossy().into_owned())
        .filter(|n| n.ends_with(".tlisp"))
        .collect();
    names.sort();
    let mut combined = taxlispv1::ModuleFile::default();
    for name in &names {
        let src = std::fs::read_to_string(dir.join(name)).expect("read rule file");
        let m = parser::parse_file(name, &src).expect("parse rule file");
        combined.expressions.extend(m.expressions);
    }
    let builtins = new_checker().globals;
    let mut c = new_checker();
    match c.check_module(&combined) {
        Ok(()) => {
            assert_eq!(v.federal.error, "", "Rust accepted what Go rejected");
            golden::assert_same(
                "federal",
                &defined_globals(&c, &builtins),
                &v.federal.globals,
            );
        }
        Err(e) => assert_eq!(e.to_string(), v.federal.error),
    }
}
