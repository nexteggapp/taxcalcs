//! Go's taxyear/model/account tests. The models are written in the
//! protobuf JSON form, which says the same as Go's literals in a fraction
//! of the space.

// Amounts are in cents, written as Go writes them: 3500_00 is $3,500. A
// test unwraps what it builds.
#![allow(clippy::inconsistent_digit_grouping, clippy::unwrap_used)]

use proto::{commonv1, scenariosv1};
use serde_json::{Value, json};
use taxcalcs_taxyear::model::{self, Context};

fn usd(dollars: i64) -> Value {
    json!({"minor": dollars * 100, "currency": "USD"})
}

fn pct(p: i64) -> Value {
    json!({"units": p, "scale": 0})
}

fn date(year: i32, month: i32, day: i32) -> commonv1::Date {
    commonv1::Date {
        year,
        month,
        day,
        ..Default::default()
    }
}

fn jdate(year: i32, month: i32, day: i32) -> Value {
    json!({"year": year, "month": month, "day": day})
}

fn doc_ref(id: &str) -> Value {
    json!({"vaultRef": id, "name": id})
}

fn ctx(as_of: commonv1::Date) -> Context {
    Context::new(2026, Some(&as_of), None).unwrap()
}

fn account(v: Value) -> scenariosv1::AccountModel {
    serde_json::from_value(v).unwrap()
}

/// The year's boxes: interest, treasury, exempt, foreign, ordinary,
/// qualified, gain distributions, section 199A.
fn year_boxes(p: &scenariosv1::AccountProjection) -> [i64; 8] {
    let b = &p.year.boxes;
    [
        b.interest.minor,
        b.treasury_interest.minor,
        b.tax_exempt_interest.minor,
        b.foreign_tax_paid.minor,
        b.ordinary_dividends.minor,
        b.qualified_dividends.minor,
        b.capital_gain_distributions.minor,
        b.section_199a_dividends.minor,
    ]
}

#[test]
fn expand_account_forms() {
    let ctx = ctx(date(2026, 12, 31));
    let cases: Vec<(&str, Value, [i64; 8])> = vec![
        (
            "Form 1099-INT alone",
            json!([{"id": "f-int", "source": doc_ref("doc-int"), "form1099Int": {
                "interestIncome": usd(1200), "savingsBondAndTreasuryInterest": usd(400),
                "taxExemptInterest": usd(250), "foreignTaxPaid": usd(30)}}]),
            [1200_00, 400_00, 250_00, 30_00, 0, 0, 0, 0],
        ),
        (
            "Form 1099-DIV alone",
            json!([{"id": "f-div", "source": doc_ref("doc-div"), "form1099Div": {
                "ordinaryDividends": usd(2500), "qualifiedDividends": usd(1800),
                "totalCapitalGainDistribution": usd(600), "section199aDividends": usd(150),
                "exemptInterestDividends": usd(100), "foreignTaxPaid": usd(70)}}]),
            [0, 0, 100_00, 70_00, 2500_00, 1800_00, 600_00, 150_00],
        ),
        (
            "Form 1099-OID nets acquisition and bond premium against OID, periodic interest, and market discount",
            json!([{"id": "f-oid", "source": doc_ref("doc-oid"), "form1099Oid": {
                "originalIssueDiscount": usd(1000), "otherPeriodicInterest": usd(200),
                "marketDiscount": usd(150), "acquisitionPremium": usd(50), "bondPremium": usd(100),
                "originalIssueDiscountOnTreasuryObligations": usd(300), "taxExemptOid": usd(450)}}]),
            // 1000 + 200 + 150 - 50 - 100
            [1200_00, 300_00, 450_00, 0, 0, 0, 0, 0],
        ),
        (
            "Form 1099-OID where bond premium exceeds OID floors taxable interest at zero",
            json!([{"id": "f-oid-neg", "source": doc_ref("doc-oid-neg"), "form1099Oid": {
                "originalIssueDiscount": usd(200), "bondPremium": usd(350),
                "originalIssueDiscountOnTreasuryObligations": usd(100)}}]),
            [0, 100_00, 0, 0, 0, 0, 0, 0],
        ),
        (
            "Form 1099-INT, 1099-DIV, and 1099-OID combined with corrected form replacement",
            json!([
                // Replaced by f-int-new, so counts for nothing.
                {"id": "f-int-old", "source": doc_ref("doc-int-old"), "form1099Int": {"interestIncome": usd(9999)}},
                {"id": "f-int-new", "source": doc_ref("doc-int-new"), "replaces": [doc_ref("doc-int-old")],
                 "form1099Int": {"interestIncome": usd(500), "taxExemptInterest": usd(100), "foreignTaxPaid": usd(20)}},
                {"id": "f-div", "source": doc_ref("doc-div"), "form1099Div": {
                    "ordinaryDividends": usd(1000), "qualifiedDividends": usd(800),
                    "exemptInterestDividends": usd(50), "foreignTaxPaid": usd(30)}},
                {"id": "f-oid", "source": doc_ref("doc-oid"), "form1099Oid": {
                    "originalIssueDiscount": usd(300), "taxExemptOid": usd(50)}},
            ]),
            // 500 INT + 300 OID; 100 INT + 50 DIV + 50 OID; 20 INT + 30 DIV.
            [800_00, 0, 200_00, 50_00, 1000_00, 800_00, 0, 0],
        ),
    ];
    for (name, forms, want) in cases {
        let m = account(json!({"id": "acct", "name": "Brokerage", "parts": [
            {"id": "p-forms", "fromForms": {"forms": forms}}]}));
        let (proj, _) = model::account::expand(&ctx, &m).unwrap();
        assert_eq!(year_boxes(&proj), want, "{name}");
        assert!(
            proj.expected.is_empty(),
            "{name}: expected documents with forms present"
        );
    }
}

#[test]
fn expand_credits_and_projections() {
    struct Case {
        name: &'static str,
        as_of: commonv1::Date,
        model: Value,
        want: [i64; 8],
        adjustment_interest: i64,
        expected: Vec<&'static str>,
    }
    let credit = |id: &str, d: Value, kind: &str, amount: i64| json!({"id": id, "date": d, "kind": kind, "amount": usd(amount)});
    let cases = vec![
        Case {
            name: "all credit kinds with qualified dividend share from last_year_qualified_share",
            as_of: date(2026, 6, 30),
            model: json!({"id": "acct-credits", "name": "Bank", "lastYearQualifiedShare": pct(60),
            "parts": [{"id": "p-credits", "fromCredits": {"credits": [
                credit("c-1", jdate(2026, 1, 31), "CREDIT_KIND_INTEREST", 100),
                credit("c-2", jdate(2026, 2, 28), "CREDIT_KIND_TREASURY_INTEREST", 50),
                credit("c-3", jdate(2026, 3, 31), "CREDIT_KIND_DIVIDEND", 300),
                credit("c-4", jdate(2026, 4, 30), "CREDIT_KIND_CAPITAL_GAIN_DISTRIBUTION", 150),
                credit("c-5", jdate(2026, 5, 31), "CREDIT_KIND_TAX_EXEMPT_INTEREST", 80),
                credit("c-6", jdate(2026, 6, 30), "CREDIT_KIND_FOREIGN_TAX", 25),
            ]}}]}),
            // 60% of 300 is qualified.
            want: [100_00, 50_00, 80_00, 25_00, 300_00, 180_00, 150_00, 0],
            adjustment_interest: 0,
            expected: vec!["Form1099Int", "Form1099Div"],
        },
        Case {
            name: "reconciling ledger credits with 1099-INT adds December 31 adjustment",
            as_of: date(2026, 12, 31),
            model: json!({"id": "acct-recon", "name": "Savings", "parts": [
                {"id": "p-credits", "fromCredits": {"credits": [
                    credit("c-1", jdate(2026, 3, 31), "CREDIT_KIND_INTEREST", 400)]}},
                {"id": "p-forms", "fromForms": {"forms": [{"id": "f-int", "source": doc_ref("doc-int"),
                    "form1099Int": {"interestIncome": usd(650)}}]}},
            ]}),
            want: [650_00, 0, 0, 0, 0, 0, 0, 0],
            adjustment_interest: 250_00,
            expected: vec![],
        },
        Case {
            name: "AtRate part computes monthly interest on balance for remaining months",
            as_of: date(2026, 6, 30),
            // 6% of $120,000 = $7,200/yr = $600/mo for 6 months (Jul-Dec) = $3,600
            model: json!({"id": "acct-rate", "name": "High Yield Savings", "balance": usd(120000),
                "parts": [{"id": "p-rate", "atRate": {"from": jdate(2026, 7, 1), "rate": pct(6)}}]}),
            want: [3600_00, 0, 0, 0, 0, 0, 0, 0],
            adjustment_interest: 0,
            expected: vec!["Form1099Int"],
        },
    ];
    for c in cases {
        let ctx = ctx(c.as_of.clone());
        let (proj, _) = model::account::expand(&ctx, &account(c.model)).unwrap();
        assert_eq!(year_boxes(&proj), c.want, "{}", c.name);
        let adj: i64 = proj
            .items
            .iter()
            .filter(|it| it.kind == scenariosv1::ItemKind::ITEM_KIND_ADJUSTMENT)
            .map(|it| match &it.figures {
                Some(scenariosv1::__buffa::oneof::item::Figures::Account(f)) => f.interest.minor,
                _ => 0,
            })
            .sum();
        assert_eq!(
            adj, c.adjustment_interest,
            "{}: adjustment interest",
            c.name
        );
        let forms: Vec<&str> = proj.expected.iter().map(|e| e.form.as_str()).collect();
        assert_eq!(forms, c.expected, "{}", c.name);
    }
}
