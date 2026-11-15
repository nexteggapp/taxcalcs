//! The builtins that do date arithmetic for estimated tax, ported from the Go
//! file `taxlisp/eval/date.go`: what `estimated.Date` and
//! `estimated.Calendar` do for due dates and penalty periods.
//!
//! Days are counted in the proleptic Gregorian calendar by Howard Hinnant's
//! days-from-civil algorithm, which the Go engine uses too, so the two agree
//! on every date a [`DateVal`] can hold. Day 0 is 1970-01-01.

use std::collections::HashSet;

use super::{ArithOp, DateVal, EvalError, Evaluator, Node, Value, arg, err_at};

/// The day number of `y`-`m`-`d`, which must be a date.
fn days_from_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = if y < 0 { y - 399 } else { y } / 400;
    let yoe = y - era * 400;
    let mp = if m <= 2 { m + 9 } else { m - 3 };
    let doy = (153 * mp + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146097 + doe - 719468
}

/// The first day a [`DateVal`]'s `i32` year can hold.
fn min_day() -> i64 {
    days_from_civil(i64::from(i32::MIN), 1, 1)
}

/// The last day a [`DateVal`]'s `i32` year can hold.
fn max_day() -> i64 {
    days_from_civil(i64::from(i32::MAX), 12, 31)
}

/// The date of day number `z`, or `None` when no [`DateVal`] can hold it.
fn civil_from_days(z: i64) -> Option<DateVal> {
    if z < min_day() || z > max_day() {
        return None;
    }
    let z = z + 719468;
    let era = if z < 0 { z - 146096 } else { z } / 146097;
    let doe = z - era * 146097;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp >= 10 { mp - 9 } else { mp + 3 };
    let y = yoe + era * 400 + i64::from(m <= 2);
    Some(DateVal {
        year: i32::try_from(y).ok()?,
        month: i32::try_from(m).ok()?,
        day: i32::try_from(d).ok()?,
    })
}

/// Whether year `y` has a February 29th.
fn is_leap(y: i64) -> bool {
    y % 4 == 0 && (y % 100 != 0 || y % 400 == 0)
}

/// The number of days in month `m`, from 1 to 12, of year `y`.
fn days_in_month(y: i64, m: i64) -> i64 {
    match m {
        2 if is_leap(y) => 29,
        2 => 28,
        4 | 6 | 9 | 11 => 30,
        _ => 31,
    }
}

/// The date `y`-`m`-`d`, or `None` when that day does not exist or its year
/// is outside `i32`: February 30th is not a date.
fn to_date(y: i64, m: i64, d: i64) -> Option<DateVal> {
    if !(1..=12).contains(&m) || d < 1 || d > days_in_month(y, m) {
        return None;
    }
    Some(DateVal {
        year: i32::try_from(y).ok()?,
        month: i32::try_from(m).ok()?,
        day: i32::try_from(d).ok()?,
    })
}

/// The error for a day that does not exist, as `estimated.NewDate` reports
/// one.
fn not_a_date(expr: Option<&Node>, name: &str, y: i64, m: i64, d: i64) -> EvalError {
    err_at(expr, format!("{name}: {y:04}-{m:02}-{d:02} is not a date"))
}

/// The error for a result no [`DateVal`] can hold.
fn out_of_range(expr: Option<&Node>, name: &str) -> EvalError {
    err_at(expr, format!("{name}: the result is out of range"))
}

/// The Date argument at `i` of builtin `name`, checked to be a date.
fn date_arg(
    name: &str,
    args: &[Value],
    i: usize,
    expr: Option<&Node>,
) -> Result<DateVal, EvalError> {
    match arg(args, i, expr)? {
        Value::Date(d) => {
            let (y, m, day) = (i64::from(d.year), i64::from(d.month), i64::from(d.day));
            to_date(y, m, day).ok_or_else(|| not_a_date(expr, name, y, m, day))
        }
        v => Err(err_at(
            expr,
            format!("{name} expects Date, got {}", v.go_type()),
        )),
    }
}

/// The day number of the Date argument at `i` of builtin `name`.
fn day_arg(name: &str, args: &[Value], i: usize, expr: Option<&Node>) -> Result<i64, EvalError> {
    let d = date_arg(name, args, i, expr)?;
    Ok(days_from_civil(
        i64::from(d.year),
        i64::from(d.month),
        i64::from(d.day),
    ))
}

/// The Int argument at `i` of builtin `name`.
fn int_arg(name: &str, args: &[Value], i: usize, expr: Option<&Node>) -> Result<i64, EvalError> {
    match arg(args, i, expr)? {
        Value::Int(v) => Ok(*v),
        v => Err(err_at(
            expr,
            format!("{name} expects Int, got {}", v.go_type()),
        )),
    }
}

/// 0 for Sunday through 6 for Saturday, as Go's `time.Weekday` counts. Day
/// 0, 1970-01-01, was a Thursday.
fn weekday(n: i64) -> i64 {
    (n + 4).rem_euclid(7)
}

/// `(make-date year month day)`.
pub(super) fn builtin_make_date(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let y = int_arg("make-date", args, 0, expr)?;
    let m = int_arg("make-date", args, 1, expr)?;
    let d = int_arg("make-date", args, 2, expr)?;
    to_date(y, m, d)
        .map(Value::Date)
        .ok_or_else(|| not_a_date(expr, "make-date", y, m, d))
}

/// `(date-add-days d n)`.
pub(super) fn builtin_date_add_days(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let day = day_arg("date-add-days", args, 0, expr)?;
    let n = int_arg("date-add-days", args, 1, expr)?;
    day.checked_add(n)
        .and_then(civil_from_days)
        .map(Value::Date)
        .ok_or_else(|| out_of_range(expr, "date-add-days"))
}

/// `(date-add-months d n)`: the same day `n` months later, clamped to the
/// later month's last day.
pub(super) fn builtin_date_add_months(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let d = date_arg("date-add-months", args, 0, expr)?;
    let n = int_arg("date-add-months", args, 1, expr)?;
    let months = (i64::from(d.year) * 12 + i64::from(d.month) - 1)
        .checked_add(n)
        .ok_or_else(|| out_of_range(expr, "date-add-months"))?;
    let (y, m) = (months.div_euclid(12), months.rem_euclid(12) + 1);
    let day = i64::from(d.day).min(days_in_month(y, m));
    to_date(y, m, day)
        .map(Value::Date)
        .ok_or_else(|| out_of_range(expr, "date-add-months"))
}

/// `(days-between from to)`: positive when `to` is later.
pub(super) fn builtin_days_between(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let from = day_arg("days-between", args, 0, expr)?;
    let to = day_arg("days-between", args, 1, expr)?;
    Ok(Value::Int(to - from))
}

/// `(date-weekday d)`.
pub(super) fn builtin_date_weekday(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    Ok(Value::Int(weekday(day_arg("date-weekday", args, 0, expr)?)))
}

/// `(month-end year month)`.
pub(super) fn builtin_month_end(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let y = int_arg("month-end", args, 0, expr)?;
    let m = int_arg("month-end", args, 1, expr)?;
    to_date(y, m, days_in_month(y, m))
        .map(Value::Date)
        .ok_or_else(|| err_at(expr, format!("month-end: {y:04}-{m:02} is not a month")))
}

/// `(days-in-year year)`.
pub(super) fn builtin_days_in_year(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let y = int_arg("days-in-year", args, 0, expr)?;
    Ok(Value::Int(if is_leap(y) { 366 } else { 365 }))
}

/// `(next-business-day d holidays)`: `d`, or the first day after it that is
/// neither a Saturday, a Sunday nor one of `holidays`.
pub(super) fn builtin_next_business_day(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let mut day = day_arg("next-business-day", args, 0, expr)?;
    let holidays = match arg(args, 1, expr)? {
        Value::Slice(s) => {
            let mut set = HashSet::with_capacity(s.elems.len());
            for i in 0..s.elems.len() {
                set.insert(day_arg("next-business-day", &s.elems, i, expr)?);
            }
            set
        }
        v => {
            return Err(err_at(
                expr,
                format!(
                    "next-business-day expects Slice as second argument, got {}",
                    v.go_type()
                ),
            ));
        }
    };
    while matches!(weekday(day), 0 | 6) || holidays.contains(&day) {
        day += 1;
    }
    civil_from_days(day)
        .map(Value::Date)
        .ok_or_else(|| out_of_range(expr, "next-business-day"))
}

/// `min` or `max` over Date operands, keeping the earlier operand on a tie
/// as Go's loop does.
pub(super) fn fold_dates(
    expr: &Node,
    op: ArithOp,
    first: DateVal,
    rest: &[Value],
) -> Result<Value, EvalError> {
    let mut acc = first;
    for v in rest {
        let Value::Date(d) = v else {
            return Err(super::mismatch(expr, op));
        };
        let c = super::compare_date(d, &acc);
        if (op == ArithOp::Min && c < 0) || (op == ArithOp::Max && c > 0) {
            acc = *d;
        }
    }
    Ok(Value::Date(acc))
}
