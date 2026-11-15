//! The text builtins against the cases of taxlisp/eval/text_test.go: each
//! expression and the value it prints as, or the end of the error it fails
//! with.

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use taxcalcs_taxlisp::eval::Evaluator;
use taxcalcs_taxlisp::{parser, types};

/// An expression, the value it prints as or the end of its error, and
/// whether it skips the typechecker so that the evaluator's own error for an
/// ill-typed call is what is tested.
struct TextCase {
    src: &'static str,
    want: Result<&'static str, &'static str>,
    unchecked: bool,
}

const fn c(
    src: &'static str,
    want: Result<&'static str, &'static str>,
    unchecked: bool,
) -> TextCase {
    TextCase {
        src,
        want,
        unchecked,
    }
}

const TEXT_CASES: &[TextCase] = &[
    // concat joins with nothing between, and keeps empty strings' places.
    c(
        r#"(concat ["About " "$37" "."])"#,
        Ok(r#""About $37.""#),
        false,
    ),
    c(r#"(concat ["a" "" "b"])"#, Ok(r#""ab""#), false),
    c(
        r#"(concat ((inst empty-slice String)))"#,
        Ok(r#""""#),
        false,
    ),
    c(r#"(concat ["−$1,200"])"#, Ok(r#""−$1,200""#), false),
    c(
        r#"(concat 1)"#,
        Err(r#"concat expects Slice, got eval.IntVal"#),
        true,
    ),
    c(
        r#"(concat [1 2])"#,
        Err(r#"concat expects String elements, got eval.IntVal"#),
        true,
    ),
    c(
        r#"(concat [1 2])"#,
        Err(r#"argument 1 has type (Slice Int), want (Slice String)"#),
        false,
    ),
    // int-to-string is decimal, with a minus sign below zero.
    c(r#"(int-to-string 2026)"#, Ok(r#""2026""#), false),
    c(r#"(int-to-string 0)"#, Ok(r#""0""#), false),
    c(r#"(int-to-string -42)"#, Ok(r#""-42""#), false),
    c(
        r#"(int-to-string -9223372036854775808)"#,
        Ok(r#""-9223372036854775808""#),
        false,
    ),
    c(
        r#"(int-to-string 1.5)"#,
        Err(r#"int-to-string expects Int, got eval.FloatVal"#),
        true,
    ),
    // money-to-cents rounds halves away from zero, as round-cents does.
    c(r#"(money-to-cents #usd"1234.56")"#, Ok("123456"), false),
    c(r#"(money-to-cents #usd"-0.01")"#, Ok("-1"), false),
    c(r#"(money-to-cents (/ #usd"0.01" 2))"#, Ok("1"), false),
    c(r#"(money-to-cents (/ #usd"-0.01" 2))"#, Ok("-1"), false),
    c(r#"(money-to-cents (/ #usd"0.01" 3))"#, Ok("0"), false),
    c(
        r#"(money-to-cents (* #usd"92233720368547758.07" 1))"#,
        Ok("9223372036854775807"),
        false,
    ),
    c(
        r#"(money-to-cents (* #usd"92233720368547758.07" 2))"#,
        Err(r#"money-to-cents: the result is out of range"#),
        false,
    ),
    c(
        r#"(money-to-cents 1)"#,
        Err(r#"money-to-cents expects Money, got eval.IntVal"#),
        true,
    ),
];

fn eval_text_case(tc: &TextCase) -> Result<String, String> {
    let mf = parser::parse_file("test.tlisp", tc.src).map_err(|e| e.to_string())?;
    if !tc.unchecked {
        let mut checker = types::Checker::new(types::global_files().expect("pool"));
        checker.check_module(&mf).map_err(|e| e.to_string())?;
    }
    let ev = Evaluator::new(None).expect("evaluator");
    ev.eval_module(&mf)
        .map(|v| v.to_string())
        .map_err(|e| e.to_string())
}

#[test]
fn text_builtins() {
    let mut failures = Vec::new();
    for tc in TEXT_CASES {
        let got = eval_text_case(tc);
        let ok = match (&got, tc.want) {
            (Ok(v), Ok(want)) => v == want,
            (Err(e), Err(want)) => e.ends_with(want),
            _ => false,
        };
        if !ok {
            failures.push(format!("{}: got {got:?}, want {:?}", tc.src, tc.want));
        }
    }
    assert!(failures.is_empty(), "{}", failures.join("\n"));
}
