//! Ported from taxlisp/parser/parser_test.go.

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use proto::taxlispv1;
use proto::taxlispv1::__buffa::oneof::atom::Primitive;
use proto::taxlispv1::__buffa::oneof::expression::Value;
use taxcalcs_taxlisp::parser;

const SAMPLE: &str = r#"; Top-level comment
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)

(= std-deduction #usd"15750.00")

(def (sum-by T) ((f (Fn (T) Money)) (items (Slice T))) Money
  (fold (fn ((acc Money) (item T)) Money (+ acc (f item))) #usd"0.00" items))

(def sample ((in ret/ReturnInputs)) (Union Money Nil)
  (withs (rate 0.0765
          dt #date"2025-04-15"
          xs [1 2 3])
    (if (> (len xs) 0)
        (round-dollar (* std-deduction rate))
        nil)))
"#;

fn symbol_of(e: &taxlispv1::Expression) -> &taxlispv1::Symbol {
    match &e.value {
        Some(Value::Atom(a)) => match &a.primitive {
            Some(Primitive::Symbol(s)) => s,
            other => panic!("not a symbol: {other:?}"),
        },
        other => panic!("not an atom: {other:?}"),
    }
}

fn list_of(e: &taxlispv1::Expression) -> &taxlispv1::List {
    match &e.value {
        Some(Value::List(l)) => l,
        other => panic!("not a list: {other:?}"),
    }
}

fn strip_syntax(mf: &mut taxlispv1::ModuleFile) {
    for e in &mut mf.expressions {
        strip_expr_syntax(e);
    }
}

fn strip_expr_syntax(e: &mut taxlispv1::Expression) {
    match &mut e.value {
        Some(Value::Atom(a)) => a.syntax = Default::default(),
        Some(Value::List(l)) => {
            l.syntax = Default::default();
            for child in &mut l.expressions {
                strip_expr_syntax(child);
            }
        }
        None => {}
    }
}

#[test]
fn parse_and_round_trip() {
    let mut mf1 = parser::parse_file("sample.tlisp", SAMPLE).expect("ParseFile failed");
    assert_eq!(mf1.expressions.len(), 5, "top-level expressions");
    let first_comments = &list_of(&mf1.expressions[0])
        .syntax
        .as_option()
        .expect("syntax")
        .leading_comments;
    assert_eq!(first_comments, &vec![" Top-level comment".to_string()]);

    let formatted = parser::format_file(&mf1);
    let mut mf2 = parser::parse_file("sample.tlisp", &formatted)
        .unwrap_or_else(|e| panic!("re-parse of formatted output failed: {e}\n{formatted}"));
    let formatted2 = parser::format_file(&mf2);
    assert_eq!(formatted, formatted2, "FormatFile is not idempotent");

    // Clear SyntaxDetails and verify structural proto equality.
    strip_syntax(&mut mf1);
    strip_syntax(&mut mf2);
    assert_eq!(mf1, mf2, "ASTs differ after round-trip");
}

#[test]
fn symbol_package_splitting() {
    let e = parser::parse_expr("fed/Form1040").expect("parse");
    let sym = symbol_of(&e);
    assert_eq!(
        (sym.package.as_str(), sym.name.as_str()),
        ("fed", "Form1040")
    );

    let div = parser::parse_expr("(/ a b)").expect("parse");
    let div_sym = symbol_of(&list_of(&div).expressions[0]);
    assert_eq!((div_sym.package.as_str(), div_sym.name.as_str()), ("", "/"));
}

#[test]
fn syntax_errors() {
    let cases = [
        "(unclosed",
        "[unclosed",
        ")",
        r#"#usd"12.345""#,
        r#"#date"2025-13-01""#,
        r#"#unknown"abc""#,
        r#""unclosed string"#,
    ];
    for tc in cases {
        assert!(
            parser::parse_file("bad.tlisp", tc).is_err(),
            "ParseFile({tc:?}) succeeded, expected error"
        );
    }
}

#[test]
fn go_strconv_helpers() {
    assert_eq!(parser::go_format_float_g(1e21), "1e+21");
    assert_eq!(parser::go_format_float_g(123456.0), "123456");
    assert_eq!(parser::go_format_float_g(1e-5), "1e-05");
    assert_eq!(parser::format_float(3.0), "3.0");
    assert_eq!(parser::quote("a\u{0}\u{a0}é"), "\"a\\x00\\u00a0é\"");
    assert_eq!(parser::quote_rune(')'), "')'");
    assert_eq!(parser::parse_float("0x1.8p1"), Some(3.0));
    assert_eq!(parser::parse_float("1_0.5"), Some(10.5));
    assert_eq!(parser::parse_float("1__0.5"), None);
    assert_eq!(parser::parse_float("1e400"), None);
}

/// A syntax error carries its position as data, and prints as Go's error
/// does.
#[test]
fn errors_carry_their_position() {
    let err = parser::parse_file("f.tlisp", "(def x\n  (+ 1 2)").unwrap_err();
    let pos = err.pos.clone().expect("a position");
    assert_eq!((pos.file.as_str(), pos.line, pos.column), ("f.tlisp", 1, 1));
    assert_eq!(err.to_string(), format!("f.tlisp:1:1: {}", err.message));
}
