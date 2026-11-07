//! Go's taxyear/model/retirement tests, with the models in the protobuf
//! JSON form. The HSA limit's tests are beside it in the module.

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

fn retirement(parts: Value) -> scenariosv1::RetirementModel {
    serde_json::from_value(json!({"id": "ret", "person": "you", "parts": parts})).unwrap()
}

#[test]
fn expand_retirement() {
    let as_of = commonv1::Date {
        year: 2026,
        month: 12,
        day: 31,
        ..Default::default()
    };
    let ctx = Context::new(2026, Some(&as_of), None).unwrap();
    let ira = |id: &str, d: Value, kind: &str, amount: i64| json!({"id": id, "iraContribution": {"date": d, "kind": kind, "amount": usd(amount)}});
    // Items; so far, rest and year, each traditional, Roth and HSA.
    let cases: Vec<(&str, Value, usize, [[i64; 3]; 3])> = vec![
        (
            "assumed direct Traditional IRA, Roth IRA, and HSA contributions",
            json!([
                ira("p-trad", jdate(2026, 4, 1), "IRA_KIND_TRADITIONAL", 3500),
                ira("p-roth", jdate(2026, 5, 1), "IRA_KIND_ROTH", 3500),
                {"id": "p-hsa", "hsaContribution": {"date": jdate(2026, 6, 1), "amount": usd(2000)}},
            ]),
            3,
            [
                [0, 0, 0],
                [3500_00, 3500_00, 2000_00],
                [3500_00, 3500_00, 2000_00],
            ],
        ),
        (
            "Form 5498 Traditional and Roth IRA contributions",
            json!([{"id": "p-forms", "fromForms": {"forms": [{"id": "f-5498", "source": doc_ref("doc-5498"),
                "form5498": {"iraContributions": usd(4000), "rothIraContributions": usd(3000)}}]}}]),
            2,
            [[4000_00, 3000_00, 0], [0, 0, 0], [4000_00, 3000_00, 0]],
        ),
        (
            "Form 5498-SA HSA and Archer MSA contributions",
            json!([{"id": "p-forms", "fromForms": {"forms": [{"id": "f-5498sa", "source": doc_ref("doc-5498sa"),
                "form5498Sa": {"totalContributionsMadeInYear": usd(2500),
                    "contributionsMadeInNextYearForYear": usd(1500), "archerMsaContributions": usd(400)}}]}}]),
            1,
            [[0, 0, 4400_00], [0, 0, 0], [0, 0, 4400_00]],
        ),
        (
            "combined Form 5498, Form 5498-SA, and assumed contributions",
            json!([
                {"id": "p-forms", "fromForms": {"forms": [
                    {"id": "f-5498", "source": doc_ref("doc-5498"), "form5498": {"iraContributions": usd(5000)}},
                    {"id": "f-5498sa", "source": doc_ref("doc-5498sa"), "form5498Sa": {"totalContributionsMadeInYear": usd(3000)}},
                ]}},
                ira("p-trad-next-apr", jdate(2027, 4, 10), "IRA_KIND_TRADITIONAL", 2000),
                // An unset date defaults to the year's end.
                {"id": "p-hsa-direct", "hsaContribution": {"amount": usd(1400)}},
            ]),
            4,
            [
                [5000_00, 0, 3000_00],
                [2000_00, 0, 1400_00],
                [7000_00, 0, 4400_00],
            ],
        ),
    ];
    for (name, parts, items, want) in cases {
        let proj = model::retirement::expand(&ctx, &retirement(parts)).unwrap();
        assert_eq!(proj.items.len(), items, "{name}");
        let row = |t: &scenariosv1::RetirementTotals| {
            [
                t.items.traditional_ira.minor,
                t.items.roth_ira.minor,
                t.items.hsa.minor,
            ]
        };
        assert_eq!(
            [row(&proj.so_far), row(&proj.rest), row(&proj.year)],
            want,
            "{name}"
        );
    }
}

#[test]
fn plan_part() {
    let cases = [
        (
            "MaxOutWorkplacePlan returned",
            json!([{"id": "p-maxout", "maxOutWorkplacePlan": {}}]),
            "p-maxout",
        ),
        (
            "WorkplacePlanTotal returned",
            json!([{"id": "p-total", "workplacePlanTotal": {"total": usd(23000)}}]),
            "p-total",
        ),
        (
            "non-workplace parts return nil",
            json!([{"id": "p-ira", "iraContribution": {}}]),
            "",
        ),
    ];
    for (name, parts, want) in cases {
        let m = retirement(parts);
        let got = model::retirement::plan_part(&m)
            .map(|p| p.id.as_str())
            .unwrap_or("");
        assert_eq!(got, want, "{name}");
    }
}
