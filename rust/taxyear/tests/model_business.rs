//! Go's taxyear/model/business tests, with the models in the protobuf JSON
//! form.

// Amounts are in cents, written as Go writes them: 3500_00 is $3,500. A
// test unwraps what it builds.
#![allow(clippy::inconsistent_digit_grouping, clippy::unwrap_used)]

use proto::{commonv1, scenariosv1};
use serde_json::{Value, json};
use taxcalcs_taxyear::model::{self, Context};

fn usd(dollars: i64) -> Value {
    json!({"minor": dollars * 100, "currency": "USD"})
}

fn jdate(year: i32, month: i32, day: i32) -> Value {
    json!({"year": year, "month": month, "day": day})
}

fn doc_ref(id: &str) -> Value {
    json!({"name": id})
}

fn ctx(year: i32, month: i32, day: i32) -> Context {
    let as_of = commonv1::Date {
        year,
        month,
        day,
        ..Default::default()
    };
    Context::new(2026, Some(&as_of), None).unwrap()
}

fn business(v: Value) -> scenariosv1::BusinessModel {
    serde_json::from_value(v).unwrap()
}

fn entry(id: &str, d: Value, kind: &str, amount: i64) -> Value {
    json!({"id": id, "date": d, "kind": format!("BUSINESS_ENTRY_KIND_{kind}"), "amount": usd(amount)})
}

fn with(mut v: Value, key: &str, val: Value) -> Value {
    v[key] = val;
    v
}

const PACE: &str = r#"{"id": "p-default", "paceSoFar": {}}"#;

#[test]
fn expand_ledger_and_projections() {
    struct Case {
        name: &'static str,
        as_of: (i32, i32, i32),
        model: Value,
        /// Receipts, fees and expenses so far.
        so_far: [i64; 3],
        /// Receipts, fees, expenses and net profit for the year.
        year: [i64; 4],
        expected: Vec<&'static str>,
    }
    let pace: Value = serde_json::from_str(PACE).unwrap();
    let depreciation = |e: Value| with(e, "line", json!("SCHEDULE_C_EXPENSE_DEPRECIATION"));
    let cases = vec![
        Case {
            name: "ledger receipts, fees, expenses, and one-off depreciation without default pace",
            as_of: (2026, 6, 30),
            model: json!({"id": "biz-ledger", "parts": [{"id": "p-ledger", "fromLedger": {"entries": [
                with(entry("e-1", jdate(2026, 2, 10), "RECEIPT", 5000), "client", json!("ClientA")),
                with(entry("e-2", jdate(2026, 3, 15), "FEE", 150), "client", json!("ClientA")),
                entry("e-3", jdate(2026, 4, 20), "EXPENSE", 800),
                depreciation(entry("e-4", jdate(2026, 5, 1), "EXPENSE", 2000)),
            ]}}]}),
            so_far: [5000_00, 150_00, 2800_00],
            year: [5000_00, 150_00, 2800_00, 2050_00],
            expected: vec![],
        },
        Case {
            name: "pace so far repeats recurring entries and leaves out one-off depreciation",
            as_of: (2026, 3, 31),
            model: json!({"id": "biz-pace",
            "clients": [{"name": "ClientA", "form": "CLIENT_FORM_1099_NEC"}, {"name": "PlatformB", "form": "CLIENT_FORM_1099_K"}],
            "defaultPart": pace,
            "parts": [{"id": "p-ledger", "fromLedger": {"entries": [
                entry("e-jan", jdate(2026, 1, 15), "RECEIPT", 3000),
                entry("e-feb", jdate(2026, 2, 15), "RECEIPT", 3000),
                entry("e-mar", jdate(2026, 3, 15), "RECEIPT", 3000),
                entry("e-exp-jan", jdate(2026, 1, 20), "EXPENSE", 300),
                entry("e-exp-feb", jdate(2026, 2, 20), "EXPENSE", 300),
                entry("e-exp-mar", jdate(2026, 3, 20), "EXPENSE", 300),
                // One-off depreciation in March should not repeat in Apr-Dec.
                depreciation(entry("e-depr", jdate(2026, 3, 25), "EXPENSE", 1800)),
            ]}}]}),
            // 900 recurring + 1800 one-off; 9000 + 9 * 3000; 2700 + 9 * 300.
            so_far: [9000_00, 0, 2700_00],
            year: [36000_00, 0, 5400_00, 30600_00],
            expected: vec!["Form1099Nec", "Form1099K"],
        },
        Case {
            name: "Monthly part overrides remaining months from its start date",
            as_of: (2026, 6, 30),
            model: json!({"id": "biz-monthly", "defaultPart": pace, "parts": [
                {"id": "p-ledger", "fromLedger": {"entries": [entry("e-jun", jdate(2026, 6, 10), "RECEIPT", 2000)]}},
                {"id": "p-monthly", "monthly": {"from": jdate(2026, 7, 1), "receipts": usd(4000), "expenses": usd(500)}},
            ]}),
            // 2000 + 6 * 4000; 6 * 500.
            so_far: [2000_00, 0, 0],
            year: [26000_00, 0, 3000_00, 23000_00],
            expected: vec![],
        },
        Case {
            name: "YearTotal shares remaining year amount across remaining months",
            as_of: (2026, 6, 30),
            model: json!({"id": "biz-yeartotal", "defaultPart": pace, "parts": [
                {"id": "p-ledger", "fromLedger": {"entries": [entry("e-1", jdate(2026, 3, 10), "RECEIPT", 10000)]}},
                {"id": "p-yt", "yearTotal": {"receipts": usd(40000), "expenses": usd(6000)}},
            ]}),
            so_far: [10000_00, 0, 0],
            year: [40000_00, 0, 6000_00, 34000_00],
            expected: vec![],
        },
        Case {
            name: "Stopped business cuts off future months after stop date",
            as_of: (2026, 3, 31),
            model: json!({"id": "biz-stopped", "defaultPart": pace, "parts": [
                {"id": "p-monthly", "monthly": {"from": jdate(2026, 4, 1), "receipts": usd(5000)}},
                {"id": "p-stops", "stops": {"on": jdate(2026, 6, 30)}},
            ]}),
            // Apr, May, Jun only (3 * 5000).
            so_far: [0, 0, 0],
            year: [15000_00, 0, 0, 15000_00],
            expected: vec![],
        },
    ];
    for c in cases {
        let ctx = ctx(c.as_of.0, c.as_of.1, c.as_of.2);
        let (proj, _) = model::business::expand(&ctx, &business(c.model)).unwrap();
        let s = &proj.so_far.boxes;
        assert_eq!(
            [
                s.gross_receipts.minor,
                s.returns_and_allowances.minor,
                s.total_expenses.minor
            ],
            c.so_far,
            "{}: so far",
            c.name
        );
        let y = &proj.year.boxes;
        assert_eq!(
            [
                y.gross_receipts.minor,
                y.returns_and_allowances.minor,
                y.total_expenses.minor,
                y.net_profit.minor
            ],
            c.year,
            "{}: year",
            c.name
        );
        let forms: Vec<&str> = proj.expected.iter().map(|e| e.form.as_str()).collect();
        assert_eq!(forms, c.expected, "{}", c.name);
    }
}

#[test]
fn expand_forms_and_reconciliation() {
    let ctx = ctx(2026, 12, 31);
    let nec = |amount: i64| {
        json!({"id": "f-nec", "source": doc_ref("doc-nec"), "client": "ClientNEC",
            "form1099Nec": {"nonemployeeCompensation": usd(amount)}})
    };
    let receipt = |id: &str, d: Value, amount: i64, client: &str| {
        with(entry(id, d, "RECEIPT", amount), "client", json!(client))
    };
    let cases: Vec<(&str, Value, Value, i64, usize)> = vec![
        (
            "Form 1099-NEC with no ledger entries adds full adjustment",
            json!([]),
            json!([nec(10000)]),
            10000_00,
            1,
        ),
        (
            "Form 1099-NEC matching ledger entries exactly produces zero adjustment items",
            json!([receipt("e-nec", jdate(2026, 5, 1), 10000, "ClientNEC")]),
            json!([nec(10000)]),
            10000_00,
            0,
        ),
        (
            "Form 1099-MISC aggregates all business receipt boxes and reconciles with ledger by payer name",
            json!([receipt("e-misc", jdate(2026, 4, 1), 3000, "PayerMISC")]),
            json!([{"id": "f-misc", "source": doc_ref("doc-misc"), "form1099Misc": {
                "payer": {"name": "PayerMISC"}, "rents": usd(1000), "royalties": usd(800),
                "otherIncome": usd(500), "medicalAndHealthCarePayments": usd(600),
                "cropInsuranceProceeds": usd(400), "fishingBoatProceeds": usd(300),
                "fishPurchasedForResale": usd(200), "grossProceedsPaidToAttorney": usd(700),
                "nonqualifiedDeferredCompensation": usd(500)}}]),
            // 3000 ledger + 2000 adjustment.
            5000_00,
            1,
        ),
        (
            "Form 1099-NEC, 1099-MISC, and 1099-K combined across multiple clients",
            json!([
                receipt("e-nec", jdate(2026, 3, 1), 8000, "ClientNEC"),
                receipt("e-misc", jdate(2026, 4, 1), 3000, "PayerMISC"),
            ]),
            json!([
                nec(10000),
                {"id": "f-misc", "source": doc_ref("doc-misc"), "form1099Misc": {
                    "payer": {"name": "PayerMISC"}, "rents": usd(2000), "royalties": usd(1500), "otherIncome": usd(500)}},
                {"id": "f-k", "source": doc_ref("doc-k"), "client": "PlatformK",
                    "form1099K": {"grossPaymentTransactions": usd(15000)}},
            ]),
            // 10000 + 4000 + 15000.
            29000_00,
            3,
        ),
    ];
    for (name, entries, forms, receipts, adjustments) in cases {
        let m = business(json!({"id": "biz", "parts": [
            {"id": "p-ledger", "fromLedger": {"entries": entries}},
            {"id": "p-forms", "fromForms": {"forms": forms}},
        ]}));
        let (proj, _) = model::business::expand(&ctx, &m).unwrap();
        assert_eq!(proj.year.boxes.gross_receipts.minor, receipts, "{name}");
        let n = proj
            .items
            .iter()
            .filter(|it| it.kind == scenariosv1::ItemKind::ITEM_KIND_ADJUSTMENT)
            .count();
        assert_eq!(n, adjustments, "{name}: adjustments");
    }
}
