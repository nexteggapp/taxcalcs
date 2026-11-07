//! The tests of taxlisp/eval/eval_test.go: modules evaluated, then called
//! with values built on the Rust side.

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use buffa::MessageField;
use proto::{commonv1, federalv1, returnsv1};
use taxcalcs_taxlisp::eval::{DateVal, Evaluator, MoneyVal, SliceVal, SymbolVal, Value};
use taxcalcs_taxlisp::{parser, types};

fn must_compile_and_eval(src: &str) -> Evaluator {
    let mf = parser::parse_file("test.tlisp", src).expect("ParseFile failed");
    let mut checker = types::Checker::new(types::global_files().expect("pool"));
    checker.check_module(&mf).expect("CheckModule failed");
    let ev = Evaluator::new(None).expect("NewEvaluator");
    ev.eval_module(&mf).expect("EvalModule failed");
    ev
}

fn usd_dollars(dollars: i64) -> commonv1::Money {
    commonv1::Money {
        currency: "USD".into(),
        minor: dollars * 100,
        ..Default::default()
    }
}

fn message<M: buffa::Message + Default>(v: Value) -> M {
    match v {
        Value::ProtoMessage(pm) => pm.to_message().expect("to message"),
        other => panic!("got {other}, want a message"),
    }
}

fn minor(m: &MessageField<commonv1::Money, impl buffa::ProtoBox<commonv1::Money>>) -> i64 {
    m.as_option().map_or(0, |m| m.minor)
}

const SS_AND_QDCGT: &str = r#"
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)

(= ss-tier1-base (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY #usd"32000.00"
    'fed/FILING_STATUS_SINGLE                 #usd"25000.00"
    'fed/FILING_STATUS_HEAD_OF_HOUSEHOLD      #usd"25000.00"
    'fed/FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE #usd"25000.00"
    'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY   #usd"0.00"))

(= ss-tier2-spread (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY #usd"12000.00"
    'fed/FILING_STATUS_SINGLE                 #usd"9000.00"
    'fed/FILING_STATUS_HEAD_OF_HOUSEHOLD      #usd"9000.00"
    'fed/FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE #usd"9000.00"
    'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY   #usd"0.00"))

(def sum-ssa-net-benefits ((docs (Slice ret/FormSSA1099Document))) Money
  (sum (fn ((d ret/FormSSA1099Document)) Money
         (. d 'form.net_benefits))
       docs))

(def compute-ss-benefits-worksheet
  ((status fed/FilingStatus)
   (lived-apart-all-year Bool)
   (ssa-docs (Slice ret/FormSSA1099Document))
   (other-income Money)
   (tax-exempt-interest Money)
   (adjustments-excluding-student-loan Money))
  ret/SSBenefitsWorksheet

  (withs (line1  (round-dollar (clamp-zero (sum-ssa-net-benefits ssa-docs)))
          line2  (round-dollar (* line1 0.50))
          line3  other-income
          line4  tax-exempt-interest
          line5  (+ line2 line3 line4)
          line6  adjustments-excluding-student-loan
          line7  (- line5 line6)
          mfs-zero (and (is status 'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY)
                        (not lived-apart-all-year))
          line8  (if mfs-zero
                     #usd"0.00"
                     (lookup-or ss-tier1-base status #usd"25000.00"))
          line9  (clamp-zero (- line7 line8))
          line10 (if mfs-zero
                     #usd"0.00"
                     (lookup-or ss-tier2-spread status #usd"9000.00"))
          line11 (clamp-zero (- line9 line10))
          line12 (min line9 line10)
          line13 (round-dollar (* line12 0.50))
          line14 (min line2 line13)
          line15 (round-dollar (* line11 0.85))
          line16 (+ line14 line15)
          line17 (round-dollar (* line1 0.85))
          line18 (if (<= line7 line8)
                     #usd"0.00"
                     (min line16 line17)))
    (make-proto 'ret/SSBenefitsWorksheet
      :line_7_provisional_income line7
      :line_14_taxable_from_tier_1 line14
      :line_15_eighty_five_percent_of_tier_2 line15
      :line_18_taxable_benefits line18)))

(defstruct Bracket ()
  (floor Money)
  (rate  Float))

(= ordinary-brackets-2025-mfj (Slice Bracket)
  [(Bracket #usd"0.00"      0.10)
   (Bracket #usd"23850.00"  0.12)
   (Bracket #usd"96950.00"  0.22)
   (Bracket #usd"206700.00" 0.24)
   (Bracket #usd"394600.00" 0.32)
   (Bracket #usd"501050.00" 0.35)
   (Bracket #usd"751600.00" 0.37)])

(def compute-bracket-tax ((income Money) (brackets (Slice Bracket))) Money
  (let n Int (len brackets)
    (round-dollar
      (fold (fn ((acc Money) (idx Int)) Money
              (withs (b     (elt brackets idx)
                      floor (. b 'floor)
                      rate  (. b 'rate)
                      ceil  (if (< (+ idx 1) n)
                                (. (elt brackets (+ idx 1)) 'floor)
                                income)
                      slice-amt (clamp-zero (- (min income ceil) floor)))
                (+ acc (* slice-amt rate))))
            #usd"0.00"
            [0 1 2 3 4 5 6]))))

(def compute-qdcgt-worksheet-mfj-2025
  ((taxable-income Money)
   (qualified-dividends Money)
   (net-ltcg-and-cap-gain-dist Money))
  ret/QDCGTWorksheet
  (withs (line1  taxable-income
          line2  qualified-dividends
          line3  (clamp-zero net-ltcg-and-cap-gain-dist)
          line4  (+ line2 line3)
          line5  (clamp-zero (- line1 line4))
          line6  #usd"96700.00"
          line7  (min line1 line6)
          line8  (min line5 line7)
          line9  (- line7 line8)
          line10 (min line1 line4)
          line11 line9
          line12 (- line10 line11)
          line13 #usd"600050.00"
          line14 (min line1 line13)
          line15 (+ line5 line9)
          line16 (clamp-zero (- line14 line15))
          line17 (min line12 line16)
          line18 (round-dollar (* line17 0.15))
          line19 (+ line9 line17)
          line20 (- line10 line19)
          line21 (round-dollar (* line20 0.20))
          line22 (compute-bracket-tax line5 ordinary-brackets-2025-mfj)
          line23 (+ line18 line21 line22)
          line24 (compute-bracket-tax line1 ordinary-brackets-2025-mfj)
          line25 (min line23 line24))
    (make-proto 'ret/QDCGTWorksheet
      :line_9_taxed_at_zero_percent line9
      :line_17_taxed_at_fifteen_percent line17
      :line_18_tax_at_fifteen_percent line18
      :line_22_tax_on_ordinary_income line22
      :line_24_regular_tax_on_all_taxable_income line24
      :line_25_tax_on_all_taxable_income line25)))
"#;

#[test]
fn ss_benefits_worksheet_and_qdcgt_worksheet() {
    let ev = must_compile_and_eval(SS_AND_QDCGT);

    // 1. MFJ filer with $30,000 net SSA benefits, $35,000 other income, $0
    // tax-exempt interest, $0 adjustments.
    let ssa_doc = returnsv1::FormSSA1099Document {
        form: MessageField::some(federalv1::FormSSA1099 {
            net_benefits: MessageField::some(usd_dollars(30000)),
            ..Default::default()
        }),
        ..Default::default()
    };
    let res = ev
        .call(
            "compute-ss-benefits-worksheet",
            vec![
                Value::Symbol(SymbolVal::new(
                    "fed",
                    "FILING_STATUS_MARRIED_FILING_JOINTLY",
                )),
                Value::Bool(false),
                Value::Slice(SliceVal::new(vec![ev.wrap_proto(&ssa_doc).expect("wrap")])),
                Value::Money(MoneyVal::new_cents(35000 * 100)),
                Value::Money(MoneyVal::new_cents(0)),
                Value::Money(MoneyVal::new_cents(0)),
            ],
        )
        .expect("compute-ss-benefits-worksheet failed");
    let ws: returnsv1::SSBenefitsWorksheet = message(res);
    assert_eq!(minor(&ws.line_7_provisional_income), 50000 * 100);
    assert_eq!(minor(&ws.line_14_taxable_from_tier_1), 6000 * 100);
    assert_eq!(minor(&ws.line_15_eighty_five_percent_of_tier_2), 5100 * 100);
    assert_eq!(minor(&ws.line_18_taxable_benefits), 11100 * 100);

    // 2. MFJ filer in 2025 with $150,000 taxable income, $10,000 qualified
    // dividends, $20,000 net LTCG.
    let q = ev
        .call(
            "compute-qdcgt-worksheet-mfj-2025",
            vec![
                Value::Money(MoneyVal::new_cents(150000 * 100)),
                Value::Money(MoneyVal::new_cents(10000 * 100)),
                Value::Money(MoneyVal::new_cents(20000 * 100)),
            ],
        )
        .expect("compute-qdcgt-worksheet-mfj-2025 failed");
    let qws: returnsv1::QDCGTWorksheet = message(q);
    assert_eq!(minor(&qws.line_9_taxed_at_zero_percent), 0);
    assert_eq!(minor(&qws.line_17_taxed_at_fifteen_percent), 30000 * 100);
    assert_eq!(minor(&qws.line_18_tax_at_fifteen_percent), 4500 * 100);
    assert_eq!(minor(&qws.line_22_tax_on_ordinary_income), 16228 * 100);
    assert_eq!(
        minor(&qws.line_24_regular_tax_on_all_taxable_income),
        22828 * 100
    );
    assert_eq!(minor(&qws.line_25_tax_on_all_taxable_income), 20728 * 100);
}

const UNIONS_GENERICS: &str = r#"
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)

(deftype (Option T) (Union T Nil))

(def (unwrap-or T) ((x (Option T)) (fallback T)) T
  (if (nil? x) fallback x))

(def describe-tin ((tin ret/TaxpayerID)) String
  (typecase (v (. tin 'value))
    String v
    ret/Withheld "WITHHELD"
    Nil "NONE"))

(def full-name ((first String) (last String)) String
  (join-words [first "" last]))

(def born-before-cutoff? ((dob Date) (cutoff Date)) Bool
  (< dob cutoff))

(def patch-w2-wages ((w2 ret/W2Document) (new-wages Money)) ret/W2Document
  (let updated-form ret/W2Document
    (proto-with w2
      :form (proto-with (. w2 'form)
              :wages_tips_other_compensation new-wages))
    updated-form))
"#;

#[test]
fn unions_generics_dates_and_proto_with() {
    use proto::returnsv1::__buffa::oneof::taxpayer_id::Value as Tin;
    let ev = must_compile_and_eval(UNIONS_GENERICS);

    // TaxpayerID oneof variants.
    let cases = [
        (Some(Tin::Number("123456789".into())), "123456789"),
        (Some(Tin::Withheld(Box::default())), "WITHHELD"),
        (None, "NONE"),
    ];
    for (value, want) in cases {
        let tin = returnsv1::TaxpayerID {
            value,
            ..Default::default()
        };
        let got = ev
            .call("describe-tin", vec![ev.wrap_proto(&tin).expect("wrap")])
            .expect("describe-tin failed");
        assert_eq!(
            got.to_string(),
            format!("{want:?}"),
            "describe-tin({tin:?})"
        );
    }

    // join-words skips empty strings.
    let name = ev
        .call(
            "full-name",
            vec![Value::string("Ada"), Value::string("Lovelace")],
        )
        .expect("full-name");
    assert_eq!(name.to_string(), "\"Ada Lovelace\"");

    // Date comparison.
    let before = ev
        .call(
            "born-before-cutoff?",
            vec![
                Value::Date(DateVal {
                    year: 1959,
                    month: 12,
                    day: 31,
                }),
                Value::Date(DateVal {
                    year: 1960,
                    month: 1,
                    day: 2,
                }),
            ],
        )
        .expect("born-before-cutoff?");
    assert!(
        matches!(before, Value::Bool(true)),
        "1959-12-31 < 1960-01-02 = {before}"
    );

    // proto-with is an immutable update.
    let orig = returnsv1::W2Document {
        id: "w2-1".into(),
        form: MessageField::some(federalv1::FormW2 {
            wages_tips_other_compensation: MessageField::some(usd_dollars(100000)),
            ..Default::default()
        }),
        ..Default::default()
    };
    let orig_val = ev.wrap_proto(&orig).expect("wrap");
    let patched = ev
        .call(
            "patch-w2-wages",
            vec![
                orig_val.clone(),
                Value::Money(MoneyVal::new_cents(125000 * 100)),
            ],
        )
        .expect("patch-w2-wages failed");
    let patched: returnsv1::W2Document = message(patched);
    let still: returnsv1::W2Document = message(orig_val);
    assert_eq!(
        minor(&still.form.wages_tips_other_compensation),
        100000 * 100,
        "original W2 was mutated"
    );
    assert_eq!(
        minor(&patched.form.wages_tips_other_compensation),
        125000 * 100
    );
}
