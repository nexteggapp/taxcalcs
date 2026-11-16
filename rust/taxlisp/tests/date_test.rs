//! The date builtins against the cases of taxlisp/eval/date_test.go: each
//! expression and the value it prints as, or the end of the error it fails
//! with.

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use taxcalcs_taxlisp::eval::Evaluator;
use taxcalcs_taxlisp::{parser, types};

/// An expression, the value it prints as or the end of its error, and
/// whether it skips the typechecker so that the evaluator's own error for an
/// ill-typed call is what is tested.
struct DateCase {
    src: &'static str,
    want: Result<&'static str, &'static str>,
    unchecked: bool,
}

const fn c(
    src: &'static str,
    want: Result<&'static str, &'static str>,
    unchecked: bool,
) -> DateCase {
    DateCase {
        src,
        want,
        unchecked,
    }
}

const DATE_CASES: &[DateCase] = &[
    // make-date, and the days that do not exist.
    c(
        r#"(make-date 2026 4 15)"#,
        Ok(r#"#date"2026-04-15""#),
        false,
    ),
    c(
        r#"(make-date 2024 2 29)"#,
        Ok(r#"#date"2024-02-29""#),
        false,
    ),
    c(
        r#"(make-date 2000 2 29)"#,
        Ok(r#"#date"2000-02-29""#),
        false,
    ),
    c(r#"(make-date -1 1 1)"#, Ok(r#"#date"-001-01-01""#), false),
    c(
        r#"(make-date 2026 2 29)"#,
        Err(r#"make-date: 2026-02-29 is not a date"#),
        false,
    ),
    c(
        r#"(make-date 1900 2 29)"#,
        Err(r#"make-date: 1900-02-29 is not a date"#),
        false,
    ),
    c(
        r#"(make-date 2026 2 30)"#,
        Err(r#"make-date: 2026-02-30 is not a date"#),
        false,
    ),
    c(
        r#"(make-date 2026 4 31)"#,
        Err(r#"make-date: 2026-04-31 is not a date"#),
        false,
    ),
    c(
        r#"(make-date 2026 13 1)"#,
        Err(r#"make-date: 2026-13-01 is not a date"#),
        false,
    ),
    c(
        r#"(make-date 2026 0 1)"#,
        Err(r#"make-date: 2026-00-01 is not a date"#),
        false,
    ),
    c(
        r#"(make-date 2026 4 0)"#,
        Err(r#"make-date: 2026-04-00 is not a date"#),
        false,
    ),
    c(
        r#"(make-date 2147483648 1 1)"#,
        Err(r#"make-date: 2147483648-01-01 is not a date"#),
        false,
    ),
    c(
        r#"(make-date "2026" 1 1)"#,
        Err(r#"make-date expects Int, got eval.StringVal"#),
        true,
    ),
    // date-add-days across month, year and leap day.
    c(
        r#"(date-add-days #date"2026-01-31" 1)"#,
        Ok(r#"#date"2026-02-01""#),
        false,
    ),
    c(
        r#"(date-add-days #date"2024-02-28" 1)"#,
        Ok(r#"#date"2024-02-29""#),
        false,
    ),
    c(
        r#"(date-add-days #date"2026-02-28" 1)"#,
        Ok(r#"#date"2026-03-01""#),
        false,
    ),
    c(
        r#"(date-add-days #date"2026-12-31" 1)"#,
        Ok(r#"#date"2027-01-01""#),
        false,
    ),
    c(
        r#"(date-add-days #date"2026-03-01" -1)"#,
        Ok(r#"#date"2026-02-28""#),
        false,
    ),
    c(
        r#"(date-add-days #date"2026-04-15" 56)"#,
        Ok(r#"#date"2026-06-10""#),
        false,
    ),
    c(
        r#"(date-add-days #date"2026-04-15" 0)"#,
        Ok(r#"#date"2026-04-15""#),
        false,
    ),
    c(
        r#"(date-add-days (make-proto 'nexteggapp.taxprotos.common.v1/Date :year 2025) 1)"#,
        Err(r#"date-add-days: 2025-00-00 is not a date"#),
        false,
    ),
    c(
        r#"(date-add-days (make-date 2147483647 12 31) 1)"#,
        Err(r#"date-add-days: the result is out of range"#),
        false,
    ),
    c(
        r#"(date-add-days #date"2026-01-01" 9223372036854775807)"#,
        Err(r#"date-add-days: the result is out of range"#),
        false,
    ),
    c(
        r#"(date-add-days 1 1)"#,
        Err(r#"date-add-days expects Date, got eval.IntVal"#),
        true,
    ),
    c(
        r#"(date-add-days #date"2026-01-01" 1.5)"#,
        Err(r#"date-add-days expects Int, got eval.FloatVal"#),
        true,
    ),
    // date-add-months clamps to the month's last day.
    c(
        r#"(date-add-months #date"2026-04-15" 1)"#,
        Ok(r#"#date"2026-05-15""#),
        false,
    ),
    c(
        r#"(date-add-months #date"2026-01-31" 1)"#,
        Ok(r#"#date"2026-02-28""#),
        false,
    ),
    c(
        r#"(date-add-months #date"2024-01-31" 1)"#,
        Ok(r#"#date"2024-02-29""#),
        false,
    ),
    c(
        r#"(date-add-months #date"2026-11-30" 3)"#,
        Ok(r#"#date"2027-02-28""#),
        false,
    ),
    c(
        r#"(date-add-months #date"2026-03-31" -1)"#,
        Ok(r#"#date"2026-02-28""#),
        false,
    ),
    c(
        r#"(date-add-months #date"2026-01-15" -1)"#,
        Ok(r#"#date"2025-12-15""#),
        false,
    ),
    c(
        r#"(date-add-months #date"2026-05-31" -24)"#,
        Ok(r#"#date"2024-05-31""#),
        false,
    ),
    c(
        r#"(date-add-months (make-date 2147483647 12 1) 1)"#,
        Err(r#"date-add-months: the result is out of range"#),
        false,
    ),
    c(
        r#"(date-add-months #date"2026-01-01" 9223372036854775807)"#,
        Err(r#"date-add-months: the result is out of range"#),
        false,
    ),
    // days-between counts as Form 2210 and FTB 5805 do.
    c(
        r#"(days-between #date"2026-04-15" #date"2026-06-10")"#,
        Ok(r#"56"#),
        false,
    ),
    c(
        r#"(days-between #date"2026-06-10" #date"2026-04-15")"#,
        Ok(r#"-56"#),
        false,
    ),
    c(
        r#"(days-between #date"2026-04-15" #date"2026-04-15")"#,
        Ok(r#"0"#),
        false,
    ),
    c(
        r#"(days-between #date"2025-12-31" #date"2026-12-31")"#,
        Ok(r#"365"#),
        false,
    ),
    c(
        r#"(days-between #date"2023-12-31" #date"2024-12-31")"#,
        Ok(r#"366"#),
        false,
    ),
    c(
        r#"(days-between #date"2025-07-01" #date"2026-04-15")"#,
        Ok(r#"288"#),
        false,
    ),
    c(
        r#"(days-between #date"2026-01-01" (make-proto 'nexteggapp.taxprotos.common.v1/Date))"#,
        Err(r#"days-between: 0000-00-00 is not a date"#),
        false,
    ),
    // date-weekday is 0 for Sunday through 6 for Saturday.
    c(r#"(date-weekday #date"1970-01-01")"#, Ok(r#"4"#), false),
    c(r#"(date-weekday #date"1969-12-31")"#, Ok(r#"3"#), false),
    c(r#"(date-weekday #date"2026-04-15")"#, Ok(r#"3"#), false),
    c(r#"(date-weekday #date"2026-11-14")"#, Ok(r#"6"#), false),
    c(r#"(date-weekday #date"2026-11-15")"#, Ok(r#"0"#), false),
    c(r#"(date-weekday #date"1600-03-01")"#, Ok(r#"3"#), false),
    // month-end.
    c(r#"(month-end 2026 2)"#, Ok(r#"#date"2026-02-28""#), false),
    c(r#"(month-end 2024 2)"#, Ok(r#"#date"2024-02-29""#), false),
    c(r#"(month-end 1900 2)"#, Ok(r#"#date"1900-02-28""#), false),
    c(r#"(month-end 2000 2)"#, Ok(r#"#date"2000-02-29""#), false),
    c(r#"(month-end 2026 4)"#, Ok(r#"#date"2026-04-30""#), false),
    c(r#"(month-end 2026 12)"#, Ok(r#"#date"2026-12-31""#), false),
    c(
        r#"(month-end 2026 13)"#,
        Err(r#"month-end: 2026-13 is not a month"#),
        false,
    ),
    c(
        r#"(month-end 2026 0)"#,
        Err(r#"month-end: 2026-00 is not a month"#),
        false,
    ),
    // days-in-year.
    c(
        r#"(slice (days-in-year 2026) (days-in-year 2024) (days-in-year 1900) (days-in-year 2000) (days-in-year -4))"#,
        Ok(r#"[365 366 365 366 366]"#),
        false,
    ),
    // next-business-day skips weekends and the holidays given.
    c(
        r#"(next-business-day #date"2026-04-15" ((inst empty-slice Date)))"#,
        Ok(r#"#date"2026-04-15""#),
        false,
    ),
    c(
        r#"(next-business-day #date"2026-11-14" ((inst empty-slice Date)))"#,
        Ok(r#"#date"2026-11-16""#),
        false,
    ),
    c(
        r#"(next-business-day #date"2026-11-15" ((inst empty-slice Date)))"#,
        Ok(r#"#date"2026-11-16""#),
        false,
    ),
    c(
        r#"(next-business-day #date"2026-04-15" [#date"2026-04-15"])"#,
        Ok(r#"#date"2026-04-16""#),
        false,
    ),
    c(
        r#"(next-business-day #date"2028-01-15" [#date"2028-01-17"])"#,
        Ok(r#"#date"2028-01-18""#),
        false,
    ),
    c(
        r#"(next-business-day #date"2027-04-15" [#date"2027-04-15" #date"2027-04-16" #date"2027-04-19"])"#,
        Ok(r#"#date"2027-04-20""#),
        false,
    ),
    c(
        r#"(next-business-day #date"2026-04-15" [(make-proto 'nexteggapp.taxprotos.common.v1/Date)])"#,
        Err(r#"next-business-day: 0000-00-00 is not a date"#),
        false,
    ),
    c(
        r#"(next-business-day (make-date 2147483647 12 31) [(make-date 2147483647 12 31)])"#,
        Err(r#"next-business-day: the result is out of range"#),
        false,
    ),
    c(
        r#"(next-business-day #date"2026-04-15" 1)"#,
        Err(r#"next-business-day expects Slice as second argument, got eval.IntVal"#),
        true,
    ),
    // min and max over dates.
    c(
        r#"(min #date"2026-04-15" #date"2026-01-15" #date"2026-06-15")"#,
        Ok(r#"#date"2026-01-15""#),
        false,
    ),
    c(
        r#"(max #date"2026-04-15" #date"2026-01-15" #date"2026-06-15")"#,
        Ok(r#"#date"2026-06-15""#),
        false,
    ),
    c(
        r#"(min #date"2026-04-15" 1)"#,
        Err(r#"min requires all arguments to have type Date, got Int"#),
        false,
    ),
    c(
        r#"(+ #date"2026-04-15" #date"2026-04-15")"#,
        Err(r#"+ not supported on Date"#),
        false,
    ),
    c(
        r#"(min #date"2026-04-15" 1)"#,
        Err(r#"mismatched operand types for min"#),
        true,
    ),
    c(
        r#"(+ #date"2026-04-15" #date"2026-04-15")"#,
        Err(r#"unsupported type eval.DateVal for +"#),
        true,
    ),
];

fn eval_date_case(tc: &DateCase) -> Result<String, String> {
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
fn date_builtins() {
    let mut failures = Vec::new();
    for tc in DATE_CASES {
        let got = eval_date_case(tc);
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
