//! Go's taxyear/model/otherincome tests, with the models in the protobuf
//! JSON form.

// Amounts are in cents, written as Go writes them: 3500_00 is $3,500. A
// test unwraps what it builds.
#![allow(clippy::inconsistent_digit_grouping, clippy::unwrap_used)]

use proto::{commonv1, scenariosv1};
use serde_json::{Value, json};
use taxcalcs_taxyear::model::{self, Context, Notes};

fn usd(major: i64) -> Value {
    json!({"currency": "USD", "minor": major * 100})
}

fn jdate(y: i32, m: i32, d: i32) -> Value {
    json!({"year": y, "month": m, "day": d})
}

fn context_at(y: i32, m: i32, d: i32) -> Context {
    let as_of = commonv1::Date {
        year: y,
        month: m,
        day: d,
        ..Default::default()
    };
    Context::new(i64::from(y), Some(&as_of), None).unwrap()
}

fn doc_ref(id: &str) -> Value {
    json!({"name": id})
}

/// Sets each key that is not null.
fn set(mut v: Value, fields: &[(&str, Value)]) -> Value {
    for (k, x) in fields {
        if !x.is_null() {
            v[*k] = x.clone();
        }
    }
    v
}

/// A model with one part holding one form.
fn one_form(id: &str, part: &str, form: Value) -> scenariosv1::OtherIncomeModel {
    serde_json::from_value(
        json!({"id": id, "parts": [{"id": part, "fromForms": {"forms": [form]}}]}),
    )
    .unwrap()
}

fn expand(
    ctx: &Context,
    m: &scenariosv1::OtherIncomeModel,
) -> (scenariosv1::OtherIncomeProjection, Notes) {
    model::otherincome::expand(ctx, m).unwrap()
}

fn left_out_has(n: &Notes, text: &str) -> bool {
    n.left_out().iter().any(|l| l.contains(text))
}

const NONE: Value = Value::Null;

#[test]
fn expand_form_1099_misc() {
    let ctx = context_at(2026, 12, 31);
    let cases = [
        (
            "rents and royalties only",
            json!({"taxYear": 2026, "rents": usd(1000), "royalties": usd(500)}),
            [1500_00, 0, 0],
            "",
        ),
        (
            "all income boxes with federal and multi-state withholding",
            json!({"taxYear": 2026, "rents": usd(1000), "royalties": usd(500), "otherIncome": usd(200),
                "fishingBoatProceeds": usd(300), "medicalAndHealthCarePayments": usd(400),
                "cropInsuranceProceeds": usd(600), "grossProceedsPaidToAttorney": usd(700),
                "fishPurchasedForResale": usd(100), "federalTaxWithheld": usd(350),
                "stateTaxWithholding": [{"taxWithheld": usd(100)}, {"taxWithheld": usd(50)}]}),
            [3800_00, 350_00, 150_00],
            "",
        ),
        (
            "section 409A and NQDC emits left-out note",
            json!({"taxYear": 2026, "otherIncome": usd(1000), "nonqualifiedDeferredCompensation": usd(800),
                "section409aDeferrals": usd(200)}),
            [1800_00, 0, 0],
            "409A",
        ),
    ];
    for (name, form, want, left_out) in cases {
        let m = one_form(
            "misc",
            "p-forms",
            json!({"id": "f-misc", "source": doc_ref("doc-misc"), "form1099Misc": form}),
        );
        let (proj, notes) = expand(&ctx, &m);
        let it = &proj.year.items;
        assert_eq!(
            [
                it.gross.minor,
                it.federal_withheld.minor,
                it.state_withheld.minor
            ],
            want,
            "{name}"
        );
        assert!(notes.questions().is_empty(), "{name}: questions");
        if left_out.is_empty() {
            assert!(
                notes.left_out().is_empty(),
                "{name}: {:?}",
                notes.left_out()
            );
        } else {
            assert!(
                left_out_has(&notes, left_out),
                "{name}: {:?}",
                notes.left_out()
            );
        }
    }
}

#[test]
fn expand_form_1099_sa() {
    let ctx = context_at(2026, 12, 31);
    // Gross, qualified expenses, rollover; taxable and the penalty note.
    let cases = [
        (
            "unset qualified expenses defaults to full distribution taxable",
            usd(2500),
            NONE,
            NONE,
            2500_00,
            true,
        ),
        (
            "partial qualified medical expenses reduces taxable distribution",
            usd(2500),
            usd(1500),
            NONE,
            1000_00,
            true,
        ),
        (
            "full qualified medical expenses makes distribution tax-free",
            usd(2500),
            usd(2500),
            NONE,
            0,
            false,
        ),
        (
            "qualified medical expenses exceeding distribution floors taxable at zero",
            usd(2500),
            usd(3200),
            NONE,
            0,
            false,
        ),
        (
            "rollover and qualified expenses combined",
            usd(2500),
            usd(1200),
            usd(500),
            800_00,
            true,
        ),
        (
            "full rollover makes distribution tax-free",
            usd(2500),
            NONE,
            usd(2500),
            0,
            false,
        ),
    ];
    for (name, gross, qualified, rollover, taxable, penalty) in cases {
        let m = one_form(
            "hsa-dist",
            "p-sa",
            json!({"id": "f-sa", "source": doc_ref("doc-sa"),
            "form1099SaSupplement": set(json!({}), &[("qualifiedExpenses", qualified), ("rollover", rollover)]),
            "form1099Sa": set(json!({"taxYear": 2026}), &[("grossDistribution", gross)])}),
        );
        let (proj, notes) = expand(&ctx, &m);
        assert_eq!(proj.year.items.gross.minor, 2500_00, "{name}");
        assert_eq!(proj.year.items.taxable.minor, taxable, "{name}");
        assert!(notes.questions().is_empty(), "{name}: questions");
        assert_eq!(
            left_out_has(&notes, "20%"),
            penalty,
            "{name}: {:?}",
            notes.left_out()
        );
    }
}

#[test]
fn expand_form_1099_s() {
    let ctx = context_at(2026, 12, 31);
    // Proceeds, basis, selling expenses, exclusion; gross, expenses, taxable.
    let cases = [
        (
            "unset basis and exclusion counts full gross proceeds",
            usd(450000),
            NONE,
            NONE,
            NONE,
            [450000_00, 0, 450000_00],
        ),
        (
            "basis and selling expenses reduce taxable gain",
            usd(500000),
            usd(300000),
            usd(25000),
            NONE,
            [500000_00, 325000_00, 175000_00],
        ),
        (
            "basis, selling expenses, and partial section 121 exclusion",
            usd(800000),
            usd(250000),
            usd(30000),
            usd(250000),
            [800000_00, 280000_00, 270000_00],
        ),
        (
            "section 121 exclusion covering entire gain results in zero taxable",
            usd(450000),
            usd(250000),
            usd(20000),
            usd(250000),
            [450000_00, 270000_00, 0],
        ),
        (
            "basis exceeding gross proceeds floors gain and taxable at zero",
            usd(300000),
            usd(350000),
            usd(15000),
            NONE,
            [300000_00, 365000_00, 0],
        ),
    ];
    for (name, proceeds, basis, selling, excluded, want) in cases {
        let m = one_form(
            "home-sale",
            "p-s",
            json!({"id": "f-s", "source": doc_ref("doc-s"),
            "form1099SSupplement": set(json!({}), &[("basis", basis), ("sellingExpenses", selling), ("excludedAmount", excluded)]),
            "form1099S": set(json!({"taxYear": 2026, "dateOfClosing": jdate(2026, 6, 15)}), &[("grossProceeds", proceeds)])}),
        );
        let (proj, notes) = expand(&ctx, &m);
        let it = &proj.year.items;
        assert_eq!(
            [it.gross.minor, it.expenses.minor, it.taxable.minor],
            want,
            "{name}"
        );
        assert!(notes.questions().is_empty(), "{name}: questions");
    }
}

#[test]
fn expand_form_1099_c() {
    let ctx = context_at(2026, 12, 31);
    let cases = [
        (
            "unset exclusion counts full discharged debt as taxable",
            usd(12000),
            NONE,
            12000_00,
        ),
        (
            "partial insolvency exclusion reduces taxable canceled debt",
            usd(12000),
            usd(7000),
            5000_00,
        ),
        (
            "full bankruptcy or insolvency exclusion zeroes taxable canceled debt",
            usd(12000),
            usd(15000),
            0,
        ),
    ];
    for (name, discharged, excluded, taxable) in cases {
        let m = one_form(
            "debt",
            "p-c",
            json!({"id": "f-c", "source": doc_ref("doc-c"),
            "form1099CSupplement": set(json!({}), &[("excludedAmount", excluded)]),
            "form1099C": set(json!({"taxYear": 2026, "dateOfIdentifiableEvent": jdate(2026, 9, 1)}),
                &[("amountOfDebtDischarged", discharged)])}),
        );
        let (proj, notes) = expand(&ctx, &m);
        assert_eq!(proj.year.items.gross.minor, 12000_00, "{name}");
        assert_eq!(proj.year.items.taxable.minor, taxable, "{name}");
        assert!(notes.questions().is_empty(), "{name}: questions");
    }
}

#[test]
fn expand_form_1099_q() {
    let ctx = context_at(2026, 12, 31);
    // Gross, earnings, basis, qualified expenses, rollover; taxable and the
    // penalty note.
    let cases = [
        (
            "unset qualified expenses taxes all earnings",
            usd(10000),
            usd(3000),
            usd(7000),
            NONE,
            NONE,
            3000_00,
            true,
        ),
        (
            "unset earnings and basis conservatively taxes full gross distribution",
            usd(10000),
            NONE,
            NONE,
            NONE,
            NONE,
            10000_00,
            true,
        ),
        // 40% non-qualified -> 40% of $3,000 = $1,200 taxable.
        (
            "partial qualified education expenses pro-rates taxable earnings (Pub 970)",
            usd(10000),
            usd(3000),
            usd(7000),
            usd(6000),
            NONE,
            1200_00,
            true,
        ),
        // Net distribution = $8,000; 50% of net covered -> 50% of $3,000 = $1,500 taxable.
        (
            "rollover and partial qualified education expenses",
            usd(10000),
            usd(3000),
            usd(7000),
            usd(4000),
            usd(2000),
            1500_00,
            true,
        ),
        (
            "full qualified education expenses makes 529 distribution tax-free",
            usd(10000),
            usd(3000),
            usd(7000),
            usd(10000),
            NONE,
            0,
            false,
        ),
    ];
    for (name, gross, earnings, basis, qualified, rollover, taxable, penalty) in cases {
        let m = one_form(
            "edu-dist",
            "p-q",
            json!({"id": "f-q", "source": doc_ref("doc-q"),
            "form1099QSupplement": set(json!({}), &[("qualifiedExpenses", qualified), ("rollover", rollover)]),
            "form1099Q": set(json!({"taxYear": 2026}), &[("grossDistribution", gross), ("earnings", earnings), ("basis", basis)])}),
        );
        let (proj, notes) = expand(&ctx, &m);
        assert_eq!(proj.year.items.gross.minor, 10000_00, "{name}");
        assert_eq!(proj.year.items.taxable.minor, taxable, "{name}");
        assert!(notes.questions().is_empty(), "{name}: questions");
        assert_eq!(
            left_out_has(&notes, "10%"),
            penalty,
            "{name}: {:?}",
            notes.left_out()
        );
    }
}

#[test]
fn expand_schedule_k1() {
    let ctx = context_at(2026, 12, 31);
    let cases = [
        (
            "Schedule K-1 (Form 1065) partnership",
            json!({"id": "f-1065", "source": doc_ref("doc-1065"), "scheduleK11065": {"taxYear": 2026,
                "ordinaryBusinessIncome": usd(5000), "netRentalRealEstateIncome": usd(2000),
                "otherNetRentalIncome": usd(500), "totalGuaranteedPayments": usd(1000), "royalties": usd(400)}}),
            8900_00,
            "Partner basis limits",
        ),
        (
            "Schedule K-1 (Form 1120-S) S corporation",
            json!({"id": "f-1120s", "source": doc_ref("doc-1120s"), "scheduleK11120S": {"taxYear": 2026,
                "ordinaryBusinessIncome": usd(3000), "netRentalRealEstateIncome": usd(1000),
                "otherNetRentalIncome": usd(200), "royalties": usd(100)}}),
            4300_00,
            "Shareholder basis limits",
        ),
        (
            "Schedule K-1 (Form 1041) trust or estate",
            json!({"id": "f-1041", "source": doc_ref("doc-1041"), "scheduleK11041": {"taxYear": 2026,
                "ordinaryBusinessIncome": usd(1500), "netRentalRealEstateIncome": usd(500),
                "otherRentalIncome": usd(250), "otherPortfolioAndNonbusinessIncome": usd(350)}}),
            2600_00,
            "Beneficiary basis limits",
        ),
    ];
    for (name, form, taxable, left_out) in cases {
        let (proj, notes) = expand(&ctx, &one_form("k1", "p-k1", form));
        assert_eq!(proj.year.items.taxable.minor, taxable, "{name}");
        assert!(
            left_out_has(&notes, left_out),
            "{name}: {:?}",
            notes.left_out()
        );
    }
}

#[test]
fn expand_existing_forms_and_assumptions() {
    struct Case {
        name: &'static str,
        as_of: (i32, i32, i32),
        model: Value,
        /// Gross, taxable, federal withheld, expenses.
        want: [i64; 4],
        question: bool,
    }
    let forms = |id: &str, kind: &str, part: &str, form: Value| json!({"id": id, "kind": kind, "parts": [{"id": part, "fromForms": {"forms": [form]}}]});
    let cases = vec![
        Case {
            name: "Form 1099-R with explicit taxable amount and IRA flag",
            as_of: (2026, 12, 31),
            model: forms(
                "ret",
                "OTHER_INCOME_KIND_RETIREMENT_WITHDRAWALS",
                "p-r",
                json!({"id": "f-r",
                "source": doc_ref("doc-r"), "form1099R": {"taxYear": 2026, "grossDistribution": usd(10000),
                "taxableAmount": usd(8500), "federalTaxWithheld": usd(1000), "distributionCodes": ["7"],
                "iraSepSimple": true}}),
            ),
            want: [10000_00, 8500_00, 1000_00, 0],
            question: false,
        },
        Case {
            name: "Form SSA-1099 Social Security benefits",
            as_of: (2026, 12, 31),
            model: forms(
                "ssa",
                "OTHER_INCOME_KIND_SOCIAL_SECURITY",
                "p-ssa",
                json!({"id": "f-ssa",
                "source": doc_ref("doc-ssa"), "formSsa1099": {"taxYear": 2026, "netBenefits": usd(24000),
                "federalTaxWithheld": usd(2400)}}),
            ),
            want: [24000_00, 24000_00, 2400_00, 0],
            question: false,
        },
        Case {
            name: "Form 1099-G unemployment and state refund",
            as_of: (2026, 12, 31),
            model: forms(
                "edd",
                "OTHER_INCOME_KIND_UNEMPLOYMENT",
                "p-g",
                json!({"id": "f-g",
                "source": doc_ref("doc-g"), "form1099G": {"taxYear": 2026, "unemploymentCompensation": usd(6000),
                "federalTaxWithheld": usd(600)}}),
            ),
            want: [6000_00, 6000_00, 600_00, 0],
            question: false,
        },
        Case {
            name: "Form W-2G gambling winnings",
            as_of: (2026, 12, 31),
            model: forms(
                "casino",
                "OTHER_INCOME_KIND_GAMBLING",
                "p-w2g",
                json!({"id": "f-w2g",
                "source": doc_ref("doc-w2g"), "formW2g": {"taxYear": 2026, "reportableWinnings": usd(5000),
                "federalTaxWithheld": usd(1200), "dateWon": jdate(2026, 5, 10)}}),
            ),
            want: [5000_00, 5000_00, 1200_00, 0],
            question: false,
        },
        Case {
            name: "rental YearAmount with Schedule E line expenses and ledger credits",
            as_of: (2026, 6, 30),
            model: json!({"id": "rental", "kind": "OTHER_INCOME_KIND_RENTAL", "parts": [
                {"id": "p-ledger", "fromLedger": {"credits": [{"id": "c1", "date": jdate(2026, 3, 1), "amount": usd(12000)}]}},
                {"id": "p-year", "yearAmount": {"gross": usd(24000), "recurring": true, "rentalExpenses": {
                    "insurance": usd(1200), "mortgageInterest": usd(6000), "repairs": usd(800), "taxes": usd(2000)}}},
            ]}),
            want: [24000_00, 24000_00, 0, 10000_00],
            question: false,
        },
        Case {
            name: "YearAmount stands aside when a whole-year form is present",
            as_of: (2026, 12, 31),
            model: json!({"id": "edd-conflict", "kind": "OTHER_INCOME_KIND_UNEMPLOYMENT", "parts": [
                {"id": "p-g", "fromForms": {"forms": [{"id": "f-g", "source": doc_ref("doc-g"),
                    "form1099G": {"taxYear": 2026, "unemploymentCompensation": usd(4000)}}]}},
                {"id": "p-year", "yearAmount": {"gross": usd(8000)}},
            ]}),
            want: [4000_00, 4000_00, 0, 0],
            question: true,
        },
        Case {
            name: "planned OneOff.HomeSale with basis and selling expenses but no exclusion",
            as_of: (2026, 6, 1),
            model: json!({"id": "home-plan", "kind": "OTHER_INCOME_KIND_HOME_SALE", "parts": [
                {"id": "p-hs", "oneOff": {"date": jdate(2026, 10, 15), "homeSale": {
                    "price": usd(600000), "basis": usd(350000), "sellingExpenses": usd(30000)}}}]}),
            want: [600000_00, 220000_00, 0, 380000_00],
            question: false,
        },
        Case {
            name: "planned OneOff.HomeSale with basis, selling expenses, and section 121 exclusion",
            as_of: (2026, 6, 1),
            model: json!({"id": "home-plan-excl", "kind": "OTHER_INCOME_KIND_HOME_SALE", "parts": [
                {"id": "p-hs-excl", "oneOff": {"date": jdate(2026, 10, 15), "homeSale": {
                    "price": usd(700000), "basis": usd(350000), "sellingExpenses": usd(30000),
                    "excludedAmount": usd(250000)}}}]}),
            want: [700000_00, 70000_00, 0, 380000_00],
            question: false,
        },
    ];
    for c in cases {
        let ctx = context_at(c.as_of.0, c.as_of.1, c.as_of.2);
        let m: scenariosv1::OtherIncomeModel = serde_json::from_value(c.model).unwrap();
        let (proj, notes) = expand(&ctx, &m);
        let it = &proj.year.items;
        assert_eq!(
            [
                it.gross.minor,
                it.taxable.minor,
                it.federal_withheld.minor,
                it.expenses.minor
            ],
            c.want,
            "{}",
            c.name
        );
        assert_eq!(
            !notes.questions().is_empty(),
            c.question,
            "{}: {:?}",
            c.name,
            notes.questions()
        );
    }
}

/// Go sums RentalExpenses by reflection over its set fields; the port
/// lists them, so every field must be a Money the list holds.
#[test]
fn rental_expenses_are_every_field() {
    use buffa_descriptor::{DescriptorPool, FieldKind, SingularKind};
    let pool = DescriptorPool::decode(proto::DESCRIPTORS).unwrap();
    let money = pool
        .message_index("nexteggapp.taxprotos.common.v1.Money")
        .unwrap();
    let m = pool
        .message_by_name("nexteggapp.taxcalcs.scenarios.v1.RentalExpenses")
        .unwrap();
    assert_eq!(m.fields().len(), 15);
    for f in m.fields() {
        assert_eq!(
            f.kind(),
            FieldKind::Singular(SingularKind::Message(money)),
            "{}",
            f.name()
        );
    }
    // A dollar in each of the fifteen lines is fifteen dollars of expenses.
    let mut lines = serde_json::Map::new();
    for f in m.fields() {
        lines.insert(f.json_name().to_string(), usd(1));
    }
    let ctx = context_at(2026, 12, 31);
    let model: scenariosv1::OtherIncomeModel = serde_json::from_value(json!({"id": "r", "parts": [
        {"id": "y", "yearAmount": {"gross": usd(100), "rentalExpenses": lines}}]}))
    .unwrap();
    let (proj, _) = expand(&ctx, &model);
    assert_eq!(proj.year.items.expenses.minor, 15_00);
}
