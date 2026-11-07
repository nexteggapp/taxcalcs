//! Ported from taxlisp/types/types_test.go.

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use taxcalcs_taxlisp::parser;
use taxcalcs_taxlisp::types::{self, Checker, TypeError};

fn check_source(src: &str) -> Result<Checker, TypeError> {
    let mf = parser::parse_file("test.tlisp", src).map_err(|e| TypeError::new(e.to_string()))?;
    let mut c = Checker::new(types::global_files()?);
    c.check_module(&mf)?;
    Ok(c)
}

#[test]
fn valid_programs_typecheck() {
    let src = r#"
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)

(deftype (Option T) (Union T Nil))

(defstruct Bracket ()
  (floor Money)
  (rate Float))

(defstruct (Chosen T) (T)
  (value T)
  (label String))

(= std-deduction-2025 (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_SINGLE #usd"15750.00"
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY #usd"31500.00"))

(def (unwrap-or T) ((opt (Option T)) (fallback T)) T
  (if (nil? opt)
      fallback
      opt))

(def (sum-by T) ((f (Fn (T) Money)) (items (Slice T))) Money
  (fold (fn ((acc Money) (item T)) Money (+ acc (f item)))
        #usd"0.00"
        items))

(def describe-tin ((tin ret/TaxpayerID)) String
  (typecase (v (. tin 'value))
    String v
    ret/Withheld "withheld"
    Nil "unset"))

(def total-w2-wages ((docs ret/Documents)) Money
  (round-dollar
    (sum-by (fn ((w2 ret/W2Document)) Money
              (. w2 'form.wages_tips_other_compensation))
            (. docs 'w2s))))

(def build-1040 ((in ret/ReturnInputs)) fed/Form1040
  (withs (wages (total-w2-wages (. in 'federal.documents))
          std (lookup-or std-deduction-2025 'fed/FILING_STATUS_SINGLE #usd"0.00")
          taxable (clamp-zero (- wages std)))
    (make-proto 'fed/Form1040
      :filing_status 'fed/FILING_STATUS_SINGLE
      :w2_wages wages
      :adjusted_gross_income wages
      :deduction std
      :taxable_income taxable)))
"#;
    if let Err(e) = check_source(src) {
        panic!("expected valid program to typecheck, got: {e}");
    }
}

#[test]
fn type_errors_rejected() {
    let cases = [
        (
            "money plus int",
            "(def bad ((m Money) (n Int)) Money (+ m n))",
            "+ requires all arguments to have type Money, got Int",
        ),
        (
            "wrong return type",
            "(def bad ((m Money)) Int m)",
            "body has type Money, want Int",
        ),
        (
            "non-exhaustive typecase",
            r#"
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(def bad ((tin ret/TaxpayerID)) String
  (typecase (v (. tin 'value))
    String v
    Nil "none"))
"#,
            "non-exhaustive typecase",
        ),
        (
            "unknown protobuf field",
            r#"
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(def bad ((w Money)) fed/Form1040
  (make-proto 'fed/Form1040 :non_existent_line w))
"#,
            "has no field \"non_existent_line\"",
        ),
        (
            "wrong protobuf field type",
            r#"
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(def bad () fed/Form1040
  (make-proto 'fed/Form1040 :taxable_income 42))
"#,
            "expects Money, got Int",
        ),
        (
            "missing struct field",
            r#"
(defstruct Pair () (a Money) (b Money))
(def bad () Pair (make-struct Pair :a #usd"1.00"))
"#,
            "missing required field :b",
        ),
    ];
    for (name, src, want_sub) in cases {
        match check_source(src) {
            Ok(_) => panic!("{name}: expected type error containing {want_sub:?}, got nil"),
            Err(e) => assert!(
                e.to_string().contains(want_sub),
                "{name}: got error {:?}, want substring {want_sub:?}",
                e.to_string()
            ),
        }
    }
}
