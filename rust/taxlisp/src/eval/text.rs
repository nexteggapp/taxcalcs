//! The builtins that write figures into sentences, ported from the Go file
//! `taxlisp/eval/text.go`: joining text, an integer in decimal, and an
//! amount as a whole number of cents, from which estimated tax's rules write
//! dollars with thousands separators themselves.

use num_bigint::BigInt;
use num_rational::BigRational;
use num_traits::ToPrimitive;

use super::{
    EvalError, Evaluator, Node, Value, arg, err_at, round_rat_scale, want_money, want_slice,
};

/// `(concat strings)`: the strings joined with nothing between them.
pub(super) fn builtin_concat(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let s = want_slice(arg(args, 0, expr)?, expr, |t| {
        format!("concat expects Slice, got {t}")
    })?;
    let mut out = String::new();
    for e in s.elems.iter() {
        match e {
            Value::String(w) => out.push_str(w),
            v => {
                return Err(err_at(
                    expr,
                    format!("concat expects String elements, got {}", v.go_type()),
                ));
            }
        }
    }
    Ok(Value::string(&out))
}

/// `(int-to-string n)`: `n` in decimal, with a minus sign below zero.
pub(super) fn builtin_int_to_string(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    match arg(args, 0, expr)? {
        Value::Int(n) => Ok(Value::string(&n.to_string())),
        v => Err(err_at(
            expr,
            format!("int-to-string expects Int, got {}", v.go_type()),
        )),
    }
}

/// `(money-to-cents m)`: `m` in cents, halves rounded away from zero as
/// `round-cents` rounds them.
pub(super) fn builtin_money_to_cents(
    _: &Evaluator,
    args: &[Value],
    expr: Option<&Node>,
) -> Result<Value, EvalError> {
    let m = want_money("money-to-cents", args, expr)?;
    let cents = round_rat_scale(&m.r, 100) * BigRational::from_integer(BigInt::from(100));
    cents.to_integer().to_i64().map(Value::Int).ok_or_else(|| {
        err_at(
            expr,
            "money-to-cents: the result is out of range".to_string(),
        )
    })
}
