//! The tests of returns/preparer/preparer_test.go.

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::collections::HashMap;

use buffa::MessageField;
use proto::returnsv1::__buffa::oneof::state_return::Return as StateReturnKind;
use proto::returnsv1::__buffa::oneof::taxpayer_id::Value as Tin;
use proto::{commonv1, federalv1, returnsv1};
use taxcalcs_returns::preparer::Server;

fn money(dollars: i64) -> commonv1::Money {
    commonv1::Money {
        currency: "USD".into(),
        minor: dollars * 100,
        ..Default::default()
    }
}

fn date(year: i32, month: i32, day: i32) -> commonv1::Date {
    commonv1::Date {
        year,
        month,
        day,
        ..Default::default()
    }
}

fn w2_inputs(
    first: &str,
    last: &str,
    dob: commonv1::Date,
    wages: i64,
    withheld: i64,
) -> returnsv1::ReturnInputs {
    returnsv1::ReturnInputs {
        tax_year: 2025,
        federal: MessageField::some(returnsv1::Federal1040Inputs {
            household: MessageField::some(returnsv1::Household {
                marital_status: returnsv1::MaritalStatus::MARITAL_STATUS_NEVER_MARRIED.into(),
                taxpayer: MessageField::some(returnsv1::Person {
                    id: "you".into(),
                    first_name: first.into(),
                    last_name: last.into(),
                    date_of_birth: MessageField::some(dob),
                    ..Default::default()
                }),
                ..Default::default()
            }),
            documents: MessageField::some(returnsv1::Documents {
                w2s: vec![returnsv1::W2Document {
                    id: "w2-1".into(),
                    person: "you".into(),
                    form: MessageField::some(federalv1::FormW2 {
                        wages_tips_other_compensation: MessageField::some(money(wages)),
                        federal_tax_withheld: MessageField::some(money(withheld)),
                        ..Default::default()
                    }),
                    ..Default::default()
                }],
                ..Default::default()
            }),
            ..Default::default()
        }),
        ..Default::default()
    }
}

#[test]
fn list_tax_years() {
    let srv = Server::new().expect("NewServer() failed");
    let resp = srv
        .list_tax_years(&returnsv1::ListTaxYearsRequest::default())
        .expect("ListTaxYears() failed");
    assert_eq!(resp.tax_years.len(), 2, "len(TaxYears)");
    assert_eq!(
        (resp.tax_years[0].tax_year, resp.tax_years[1].tax_year),
        (2025, 2026)
    );
    assert_eq!(resp.tax_years[0].rules_version.version, "2025.1.0");
}

#[test]
fn describe_inputs() {
    let srv = Server::new().expect("NewServer() failed");
    let resp = srv
        .describe_inputs(&returnsv1::DescribeInputsRequest {
            inputs: MessageField::some(returnsv1::ReturnInputs {
                tax_year: 2025,
                federal: MessageField::some(returnsv1::Federal1040Inputs {
                    household: MessageField::some(returnsv1::Household {
                        marital_status: returnsv1::MaritalStatus::MARITAL_STATUS_NEVER_MARRIED
                            .into(),
                        ..Default::default()
                    }),
                    ..Default::default()
                }),
                ..Default::default()
            }),
            ..Default::default()
        })
        .expect("DescribeInputs() failed");
    assert!(
        !resp.fields.is_empty(),
        "DescribeInputs() returned 0 fields"
    );
    let by_path: HashMap<&str, &returnsv1::InputFieldDescriptor> =
        resp.fields.iter().map(|f| (f.path.as_str(), f)).collect();
    let ms = by_path
        .get("federal.household.marital_status")
        .expect("missing field descriptor for federal.household.marital_status");
    assert!(ms.populated, "marital_status Populated = false, want true");
    assert!(
        ms.required_for_completion,
        "marital_status RequiredForCompletion = false, want true"
    );
    let dob = by_path
        .get("federal.household.taxpayer.date_of_birth")
        .expect("missing field descriptor for federal.household.taxpayer.date_of_birth");
    assert!(!dob.populated, "date_of_birth Populated = true, want false");
    assert!(
        dob.required_for_completion,
        "date_of_birth RequiredForCompletion = false, want true"
    );
}

#[test]
fn prepare_with_working_and_states() {
    let srv = Server::new().expect("NewServer() failed");
    let mut inputs = w2_inputs("", "", date(1990, 4, 10), 100000, 15000);
    inputs.states = ["TX", "CA"]
        .iter()
        .map(|s| returnsv1::StateInputs {
            state: s.to_string(),
            ..Default::default()
        })
        .collect();
    let resp = srv
        .prepare(&returnsv1::PrepareRequest {
            include_working: true,
            inputs: MessageField::some(inputs),
            ..Default::default()
        })
        .expect("Prepare() failed");
    let out = &resp.outputs;
    assert_eq!(
        out.federal.status,
        returnsv1::ReturnStatus::RETURN_STATUS_COMPLETE
    );
    assert_eq!(out.states.len(), 2, "len(States)");
    let status = |i: usize| match &out.states[i].r#return {
        Some(StateReturnKind::Unsupported(u)) => u.status,
        _ => Default::default(),
    };
    assert_eq!(
        status(0),
        returnsv1::ReturnStatus::RETURN_STATUS_NOT_REQUIRED,
        "TX status"
    );
    assert_eq!(
        status(1),
        returnsv1::ReturnStatus::RETURN_STATUS_INCOMPLETE,
        "CA status"
    );

    let working = &out.working;
    assert!(
        working.is_set() && !working.cells.is_empty(),
        "Working trace was not populated"
    );
    let agi = working
        .cells
        .iter()
        .find(|c| c.path == "federal.forms.form_1040.adjusted_gross_income")
        .expect("did not find federal.forms.form_1040.adjusted_gross_income in Working.Cells");
    assert_eq!(
        agi.state,
        returnsv1::CellState::CELL_STATE_KNOWN,
        "AGI cell state"
    );
    assert!(
        !agi.citations.is_empty(),
        "expected citations on AGI working cell"
    );
}

#[test]
fn build_submission() {
    let srv = Server::new().expect("NewServer() failed");
    let mut inputs = w2_inputs("Ada", "Lovelace", date(1990, 12, 10), 85000, 12000);

    // 1. Without TIN digits, BuildSubmission should report
    // EFILE_MISSING_TAXPAYER_TIN.
    let missing = srv
        .build_submission(&returnsv1::BuildSubmissionRequest {
            inputs: MessageField::some(inputs.clone()),
            ..Default::default()
        })
        .expect("BuildSubmission(missing TIN) failed");
    assert_eq!(missing.submissions.len(), 1, "len(Submissions)");
    let sub = &missing.submissions[0];
    assert!(
        !sub.ready_to_transmit,
        "ReadyToTransmit = true when TIN is missing"
    );
    assert_eq!(
        sub.diagnostics.first().map(|d| d.code.as_str()),
        Some("EFILE_MISSING_TAXPAYER_TIN")
    );

    // 2. With TIN digits provided, BuildSubmission should succeed and emit
    // an XML payload.
    let tp = inputs
        .federal
        .get_or_insert_default()
        .household
        .get_or_insert_default()
        .taxpayer
        .get_or_insert_default();
    tp.tin = MessageField::some(returnsv1::TaxpayerID {
        kind: returnsv1::TaxpayerIDKind::TAXPAYER_ID_KIND_SSN.into(),
        value: Some(Tin::Number("123456789".into())),
        ..Default::default()
    });
    let ready = srv
        .build_submission(&returnsv1::BuildSubmissionRequest {
            inputs: MessageField::some(inputs),
            ..Default::default()
        })
        .expect("BuildSubmission(with TIN) failed");
    let ready_sub = &ready.submissions[0];
    assert!(
        ready_sub.ready_to_transmit,
        "ReadyToTransmit = false (diags: {:?})",
        ready_sub.diagnostics
    );
    let xml = String::from_utf8_lossy(&ready_sub.payload);
    assert!(
        xml.contains("<PrimarySSN>123456789</PrimarySSN>"),
        "XML payload missing PrimarySSN: {xml}"
    );
    assert!(
        xml.contains("<AdjustedGrossIncomeAmt>85000</AdjustedGrossIncomeAmt>"),
        "XML payload missing AdjustedGrossIncomeAmt: {xml}"
    );
}
