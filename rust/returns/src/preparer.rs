//! The stateless ReturnPreparer service backed by the taxlisp federal rule
//! engine, ported from the Go package `returns/preparer`. Go serves it over
//! gRPC/Connect; here each method takes and returns the returnsv1 messages,
//! with no transport.

use std::collections::{HashMap, HashSet};
use std::rc::Rc;
use std::sync::Arc;

use buffa::{ExtensionSet, MessageField};
use buffa_descriptor::reflect::{ReflectMessage, Value as DynValue};
use buffa_descriptor::{
    DescriptorPool, DynamicMessage, FieldDescriptor, FieldKind, MessageDescriptor, SingularKind,
};
use proto::commonv1;
use proto::returnsv1;
use proto::returnsv1::__buffa::oneof::state_return::Return as StateReturnKind;
use proto::taxlispv1;
use proto::taxlispv1::__buffa::oneof::atom::Primitive;
use proto::taxlispv1::__buffa::oneof::expression::Value as ExprValue;

use crate::rules::federal;

/// An error from a ReturnPreparer method; its text is the Go error's.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
#[error("{0}")]
pub struct Error(pub String);

/// noIncomeTaxStates lists US states without a general individual
/// wage/income tax.
const NO_INCOME_TAX_STATES: &[&str] = &["AK", "FL", "NH", "NV", "SD", "TN", "TX", "WA", "WY"];

/// Server implements the ReturnPreparer service using the embedded federal
/// taxlisp rule engine.
pub struct Server {
    engine: Rc<federal::Engine>,
    defn_cites: HashMap<String, Vec<returnsv1::Citation>>,
}

impl Server {
    /// NewServer constructs a ReturnPreparer server initialized with the
    /// shared default federal taxlisp Engine.
    pub fn new() -> Result<Server, Error> {
        let eng = federal::default_engine().map_err(|e| Error(e.to_string()))?;
        Ok(Server::new_with_engine(eng))
    }

    /// NewServerWithEngine constructs a ReturnPreparer server using the
    /// provided federal taxlisp Engine.
    pub fn new_with_engine(eng: Rc<federal::Engine>) -> Server {
        let defn_cites = index_defn_citations(Some(&eng.module));
        Server {
            engine: eng,
            defn_cites,
        }
    }

    fn pool(&self) -> &'static Arc<DescriptorPool> {
        self.engine.evaluator.pool
    }

    /// ListTaxYears returns the tax years supported by the engine and their
    /// rule-pack versions.
    pub fn list_tax_years(
        &self,
        _req: &returnsv1::ListTaxYearsRequest,
    ) -> Result<returnsv1::ListTaxYearsResponse, Error> {
        let supported_no_tax_states: Vec<String> =
            ["AK", "FL", "NH", "NV", "SD", "TN", "TX", "WA", "WY"]
                .iter()
                .map(|s| s.to_string())
                .collect();
        let year = |tax_year: i32,
                    version: &str,
                    status: returnsv1::RulesStatus,
                    published_on: commonv1::Date| {
            returnsv1::SupportedTaxYear {
                tax_year,
                rules_version: MessageField::some(returnsv1::RulesVersion {
                    tax_year,
                    version: version.to_string(),
                    status: status.into(),
                    published_on: MessageField::some(published_on),
                    ..Default::default()
                }),
                supported_states: supported_no_tax_states.clone(),
                ..Default::default()
            }
        };
        Ok(returnsv1::ListTaxYearsResponse {
            tax_years: vec![
                year(
                    2025,
                    "2025.1.0",
                    returnsv1::RulesStatus::RULES_STATUS_FINAL,
                    date(2025, 11, 1),
                ),
                year(
                    2026,
                    "2026.1.0",
                    returnsv1::RulesStatus::RULES_STATUS_PRELIMINARY,
                    date(2025, 11, 15),
                ),
            ],
            ..Default::default()
        })
    }

    /// DescribeInputs inspects partial ReturnInputs and returns the
    /// annotated input fields with their population status and completion
    /// requirements.
    pub fn describe_inputs(
        &self,
        req: &returnsv1::DescribeInputsRequest,
    ) -> Result<returnsv1::DescribeInputsResponse, Error> {
        let inputs: &returnsv1::ReturnInputs = &req.inputs;
        let pool = self.pool();
        let msg = DynamicMessage::try_from_message(inputs, Arc::clone(pool))
            .map_err(|e| Error(e.to_string()))?;
        let mut fields = Vec::new();
        let mut visited = HashSet::new();
        collect_input_descriptors(
            pool,
            Some(&msg),
            msg.message_descriptor(),
            "",
            inputs,
            &mut visited,
            &mut fields,
        );
        Ok(returnsv1::DescribeInputsResponse {
            fields,
            ..Default::default()
        })
    }

    /// Prepare computes the federal and requested state tax returns for the
    /// given ReturnInputs.
    pub fn prepare(
        &self,
        req: &returnsv1::PrepareRequest,
    ) -> Result<returnsv1::PrepareResponse, Error> {
        let outputs = self.prepare_outputs(&req.inputs, req.include_working)?;
        Ok(returnsv1::PrepareResponse {
            outputs: MessageField::some(outputs),
            ..Default::default()
        })
    }

    /// The outputs Prepare returns for the inputs (an unset message field
    /// reads as the default ReturnInputs, as Go's nil does).
    fn prepare_outputs(
        &self,
        inputs: &returnsv1::ReturnInputs,
        include_working: bool,
    ) -> Result<returnsv1::ReturnOutputs, Error> {
        let mut outputs = self
            .engine
            .prepare(inputs)
            .map_err(|e| Error(format!("evaluating federal return: {e}")))?;

        if !inputs.states.is_empty() {
            let prepared = std::mem::take(&mut outputs.states);
            outputs.states = prepare_states(inputs, prepared);
        }

        if include_working {
            outputs.working = MessageField::some(self.build_working_trace(inputs, &outputs)?);
        }
        Ok(outputs)
    }

    /// BuildSubmission validates ReturnInputs and assembles e-file
    /// transmission payloads for requested jurisdictions.
    pub fn build_submission(
        &self,
        req: &returnsv1::BuildSubmissionRequest,
    ) -> Result<returnsv1::BuildSubmissionResponse, Error> {
        // Go calls Prepare with IncludeWorking false and takes its outputs.
        let outputs = self.prepare_outputs(&req.inputs, false)?;
        let inputs: &returnsv1::ReturnInputs = &req.inputs;

        let mut jurisdictions = req.jurisdictions.clone();
        if jurisdictions.is_empty() {
            jurisdictions = vec!["US".to_string()];
            for st in &inputs.states {
                let code = go_to_upper(st.state.trim());
                if !code.is_empty() {
                    jurisdictions.push(code);
                }
            }
        }

        let mut submissions = Vec::with_capacity(jurisdictions.len());
        for j in &jurisdictions {
            let code = go_to_upper(j.trim());
            if code == "US" {
                submissions.push(build_federal_submission(inputs, &outputs));
            } else {
                submissions.push(returnsv1::EFileSubmission {
                    jurisdiction: code.clone(),
                    ready_to_transmit: false,
                    diagnostics: vec![diagnostic(
                        "EFILE_STATE_UNSUPPORTED",
                        returnsv1::Severity::SEVERITY_WARNING,
                        &format!("State e-file transmission payload generation for {code} is not supported yet."),
                        &[],
                        &[],
                    )],
                    ..Default::default()
                });
            }
        }

        Ok(returnsv1::BuildSubmissionResponse {
            outputs: MessageField::some(outputs),
            submissions,
            ..Default::default()
        })
    }

    /// buildWorkingTrace constructs a cell-by-cell lineage trace for
    /// populated fields on ReturnOutputs.Federal.
    fn build_working_trace(
        &self,
        inputs: &returnsv1::ReturnInputs,
        outputs: &returnsv1::ReturnOutputs,
    ) -> Result<returnsv1::Working, Error> {
        let mut cells = Vec::new();
        let mut missing_inputs = Vec::new();
        if inputs.tax_year == 0 {
            missing_inputs.push("tax_year".to_string());
        }
        let marital = inputs
            .federal
            .as_option()
            .and_then(|f| f.household.as_option())
            .map(|h| h.marital_status)
            .unwrap_or_default();
        if marital == returnsv1::MaritalStatus::MARITAL_STATUS_UNSPECIFIED {
            missing_inputs.push("federal.household.marital_status".to_string());
        }

        if let Some(fed) = outputs.federal.as_option() {
            let default_cites = self
                .defn_cites
                .get("prepare-federal-return")
                .map(Vec::as_slice)
                .unwrap_or(&[]);
            let pool = self.pool();
            if let Some(summary) = fed.summary.as_option() {
                let msg = to_dynamic(summary, pool)?;
                collect_message_cells(&msg, "federal.summary", default_cites, &mut cells);
            }
            if let Some(forms) = fed.forms.as_option() {
                let msg = to_dynamic(forms, pool)?;
                collect_message_cells(&msg, "federal.forms", default_cites, &mut cells);
            }
            if let Some(workpapers) = fed.workpapers.as_option() {
                let msg = to_dynamic(workpapers, pool)?;
                collect_message_cells(&msg, "federal.workpapers", default_cites, &mut cells);
            }
        }

        cells.sort_by(|a: &returnsv1::Cell, b: &returnsv1::Cell| a.path.cmp(&b.path));

        Ok(returnsv1::Working {
            cells,
            missing_inputs,
            ..Default::default()
        })
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

fn citation(authority: &str) -> returnsv1::Citation {
    returnsv1::Citation {
        authority: authority.to_string(),
        ..Default::default()
    }
}

fn diagnostic(
    code: &str,
    severity: returnsv1::Severity,
    message: &str,
    field_paths: &[&str],
    citations: &[&str],
) -> returnsv1::Diagnostic {
    returnsv1::Diagnostic {
        code: code.to_string(),
        severity: severity.into(),
        message: message.to_string(),
        field_paths: field_paths.iter().map(|s| s.to_string()).collect(),
        citations: citations.iter().map(|c| citation(c)).collect(),
        ..Default::default()
    }
}

fn to_dynamic<M: buffa::Message + buffa::MessageName>(
    m: &M,
    pool: &Arc<DescriptorPool>,
) -> Result<DynamicMessage, Error> {
    DynamicMessage::try_from_message(m, Arc::clone(pool)).map_err(|e| Error(e.to_string()))
}

/// Go's strings.ToUpper, which maps each rune to its single uppercase rune
/// (leaving one with no single-rune uppercase, such as 'ß', as it is).
fn go_to_upper(s: &str) -> String {
    s.chars()
        .map(|c| {
            let mut up = c.to_uppercase();
            match (up.next(), up.next()) {
                (Some(u), None) => u,
                _ => c,
            }
        })
        .collect()
}

/// The protobuf package of a full name (`a.b.C` -> `a.b`).
fn parent_name(full: &str) -> &str {
    match full.rfind('.') {
        Some(i) => &full[..i],
        None => "",
    }
}

fn collect_input_descriptors(
    pool: &'static Arc<DescriptorPool>,
    msg: Option<&DynamicMessage>,
    md: &MessageDescriptor,
    prefix: &str,
    root: &returnsv1::ReturnInputs,
    visited: &mut HashSet<String>,
    out: &mut Vec<returnsv1::InputFieldDescriptor>,
) {
    for fd in md.fields() {
        let path = if prefix.is_empty() {
            fd.name().to_string()
        } else {
            format!("{prefix}.{}", fd.name())
        };
        if visited.contains(&path) {
            continue;
        }
        visited.insert(path.clone());

        let populated = is_field_populated(msg, fd);
        if let Some(spec) = fd
            .options()
            .and_then(|opts| opts.extension(&returnsv1::INPUT))
        {
            let required = is_required_for_completion(&path, &spec, root);
            out.push(returnsv1::InputFieldDescriptor {
                path: path.clone(),
                spec: MessageField::some(spec),
                populated,
                required_for_completion: required,
                ..Default::default()
            });
        }

        // Recurse into singular composite submessages so nested input
        // fields (e.g. federal.household.*, federal.elections.*) are
        // discovered.
        if let FieldKind::Singular(SingularKind::Message(sub_idx)) = fd.kind() {
            let pool_ref: &'static DescriptorPool = pool;
            let sub_md = pool_ref.message(sub_idx);
            if parent_name(sub_md.full_name()).starts_with("nexteggapp.taxcalcs.returns.v1") {
                let sub_msg = match msg {
                    Some(m) if m.has(fd) => match m.field_by_number(fd.number()) {
                        Some(DynValue::Message(sm)) => Some(sm),
                        _ => None,
                    },
                    _ => None,
                };
                collect_input_descriptors(pool, sub_msg, sub_md, &path, root, visited, out);
            }
        }
    }
}

fn is_field_populated(msg: Option<&DynamicMessage>, fd: &FieldDescriptor) -> bool {
    match msg {
        // Has is false for an empty repeated or map field.
        Some(m) => m.has(fd),
        None => false,
    }
}

fn is_required_for_completion(
    path: &str,
    spec: &returnsv1::InputSpec,
    root: &returnsv1::ReturnInputs,
) -> bool {
    match path {
        "tax_year"
        | "federal.household.marital_status"
        | "federal.household.taxpayer.date_of_birth" => true,
        "federal.household.spouse.date_of_birth" => {
            let ms = root
                .federal
                .as_option()
                .and_then(|f| f.household.as_option())
                .map(|h| h.marital_status)
                .unwrap_or_default();
            ms == returnsv1::MaritalStatus::MARITAL_STATUS_MARRIED
        }
        _ => {
            spec.category == returnsv1::InputCategory::INPUT_CATEGORY_HOUSEHOLD
                && spec.required_when.is_empty()
        }
    }
}

fn prepare_states(
    inputs: &returnsv1::ReturnInputs,
    prepared: Vec<returnsv1::StateReturn>,
) -> Vec<returnsv1::StateReturn> {
    let mut states = Vec::with_capacity(inputs.states.len());
    let mut prepared = prepared.into_iter();
    for st in &inputs.states {
        // The rules return the states in the order asked for. Keep a
        // return they prepared.
        if let Some(p) = prepared.next()
            && !matches!(p.r#return, Some(StateReturnKind::Unsupported(_)))
        {
            states.push(p);
            continue;
        }
        let code = go_to_upper(st.state.trim());
        if NO_INCOME_TAX_STATES.contains(&code.as_str()) {
            states.push(returnsv1::StateReturn {
                state: code.clone(),
                r#return: Some(StateReturnKind::Unsupported(Box::new(
                    returnsv1::UnsupportedStateReturn {
                        status: returnsv1::ReturnStatus::RETURN_STATUS_NOT_REQUIRED.into(),
                        summary: MessageField::some(returnsv1::Summary {
                            jurisdiction: code.clone(),
                            total_income: MessageField::some(commonv1::Money::default()),
                            adjusted_gross_income: MessageField::some(commonv1::Money::default()),
                            total_deductions: MessageField::some(commonv1::Money::default()),
                            taxable_income: MessageField::some(commonv1::Money::default()),
                            total_tax: MessageField::some(commonv1::Money::default()),
                            total_payments_and_refundable_credits: MessageField::some(
                                commonv1::Money::default(),
                            ),
                            refund: MessageField::some(commonv1::Money::default()),
                            amount_owed: MessageField::some(commonv1::Money::default()),
                            ..Default::default()
                        }),
                        diagnostics: vec![diagnostic(
                            "STATE_NO_INCOME_TAX",
                            returnsv1::Severity::SEVERITY_INFO,
                            &format!("{code} does not impose a general individual income tax."),
                            &["states"],
                            &[],
                        )],
                        ..Default::default()
                    },
                ))),
                ..Default::default()
            });
            continue;
        }
        states.push(returnsv1::StateReturn {
            state: code.clone(),
            r#return: Some(StateReturnKind::Unsupported(Box::new(returnsv1::UnsupportedStateReturn {
                status: returnsv1::ReturnStatus::RETURN_STATUS_INCOMPLETE.into(),
                diagnostics: vec![diagnostic(
                    "STATE_RULES_NOT_YET_LOADED",
                    returnsv1::Severity::SEVERITY_WARNING,
                    &format!("State income tax calculator for {code} is not yet loaded in this engine."),
                    &["states"],
                    &[],
                )],
                ..Default::default()
            }))),
            ..Default::default()
        });
    }
    states
}

/// mefSubmissionEnvelope: the IRS MeF return Go marshals with
/// encoding/xml, in field order.
struct MefSubmissionEnvelope {
    return_version: String,
    tax_year: i32,
    taxpayer_tin: String,
    spouse_tin: String,
    filing_status: String,
    total_income: i64,
    agi: i64,
    taxable_income: i64,
    total_tax: i64,
    total_payments: i64,
    refund_amt: i64,
    amount_owed_amt: i64,
}

impl MefSubmissionEnvelope {
    /// xml.MarshalIndent(env, "", "  "): the `a>b>c` paths of consecutive
    /// fields share their common parent elements, and an omitempty field
    /// that is empty writes nothing.
    fn marshal_indent(&self) -> String {
        let mut out = String::new();
        out.push_str(&format!(
            "<Return returnVersion=\"{}\">",
            xml_escape(&self.return_version)
        ));
        out.push_str("\n  <ReturnHeader>");
        out.push_str(&format!("\n    <TaxYr>{}</TaxYr>", self.tax_year));
        out.push_str("\n    <Filer>");
        out.push_str(&format!(
            "\n      <PrimarySSN>{}</PrimarySSN>",
            xml_escape(&self.taxpayer_tin)
        ));
        if !self.spouse_tin.is_empty() {
            out.push_str(&format!(
                "\n      <SpouseSSN>{}</SpouseSSN>",
                xml_escape(&self.spouse_tin)
            ));
        }
        out.push_str("\n    </Filer>");
        out.push_str("\n  </ReturnHeader>");
        out.push_str("\n  <ReturnData>");
        out.push_str("\n    <IRS1040>");
        let elem = |out: &mut String, name: &str, v: &str| {
            out.push_str(&format!("\n      <{name}>{v}</{name}>"));
        };
        elem(
            &mut out,
            "IndividualReturnFilingStatusCd",
            &xml_escape(&self.filing_status),
        );
        elem(&mut out, "TotalIncomeAmt", &self.total_income.to_string());
        elem(&mut out, "AdjustedGrossIncomeAmt", &self.agi.to_string());
        elem(
            &mut out,
            "TaxableIncomeAmt",
            &self.taxable_income.to_string(),
        );
        elem(&mut out, "TotalTaxAmt", &self.total_tax.to_string());
        elem(
            &mut out,
            "TotalPaymentsAmt",
            &self.total_payments.to_string(),
        );
        if self.refund_amt != 0 {
            elem(&mut out, "RefundAmt", &self.refund_amt.to_string());
        }
        if self.amount_owed_amt != 0 {
            elem(&mut out, "AmountOwedAmt", &self.amount_owed_amt.to_string());
        }
        out.push_str("\n    </IRS1040>");
        out.push_str("\n  </ReturnData>");
        out.push_str("\n</Return>");
        out
    }
}

/// encoding/xml's EscapeText, used for both text and attribute values.
fn xml_escape(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for c in s.chars() {
        match c {
            '"' => out.push_str("&#34;"),
            '\'' => out.push_str("&#39;"),
            '&' => out.push_str("&amp;"),
            '<' => out.push_str("&lt;"),
            '>' => out.push_str("&gt;"),
            '\t' => out.push_str("&#x9;"),
            '\n' => out.push_str("&#xA;"),
            '\r' => out.push_str("&#xD;"),
            c if is_xml_char(c) => out.push(c),
            _ => out.push('\u{FFFD}'),
        }
    }
    out
}

/// The XML 1.0 Char production.
fn is_xml_char(c: char) -> bool {
    matches!(c as u32,
        0x09 | 0x0A | 0x0D | 0x20..=0xD7FF | 0xE000..=0xFFFD | 0x10000..=0x10FFFF)
}

fn money_minor(m: Option<&commonv1::Money>) -> i64 {
    m.map_or(0, |m| m.minor)
}

fn build_federal_submission(
    inputs: &returnsv1::ReturnInputs,
    outputs: &returnsv1::ReturnOutputs,
) -> returnsv1::EFileSubmission {
    let mut diags = Vec::new();
    let fed: &returnsv1::FederalReturn = &outputs.federal;
    if fed.status != returnsv1::ReturnStatus::RETURN_STATUS_COMPLETE {
        diags.push(diagnostic(
            "EFILE_RETURN_INCOMPLETE",
            returnsv1::Severity::SEVERITY_ERROR,
            "Federal return status must be RETURN_STATUS_COMPLETE before building an e-file submission.",
            &["federal.status"],
            &[],
        ));
    }

    let hh = inputs
        .federal
        .as_option()
        .and_then(|f| f.household.as_option());
    let tp_tin = extract_tin_value(
        hh.and_then(|h| h.taxpayer.as_option())
            .and_then(|p| p.tin.as_option()),
    );
    if tp_tin.is_empty() {
        diags.push(diagnostic(
            "EFILE_MISSING_TAXPAYER_TIN",
            returnsv1::Severity::SEVERITY_ERROR,
            "Primary taxpayer SSN/ITIN is required in federal.household.taxpayer.tin.number for IRS MeF e-file transmission.",
            &["federal.household.taxpayer.tin.number"],
            &["26 U.S.C. § 6109(a)"],
        ));
    }

    let sp_tin = extract_tin_value(
        hh.and_then(|h| h.spouse.as_option())
            .and_then(|p| p.tin.as_option()),
    );
    let marital = hh.map(|h| h.marital_status).unwrap_or_default();
    if marital == returnsv1::MaritalStatus::MARITAL_STATUS_MARRIED && sp_tin.is_empty() {
        diags.push(diagnostic(
            "EFILE_MISSING_SPOUSE_TIN",
            returnsv1::Severity::SEVERITY_ERROR,
            "Spouse SSN/ITIN is required in federal.household.spouse.tin.number for married returns.",
            &["federal.household.spouse.tin.number"],
            &["26 U.S.C. § 6109(a)"],
        ));
    }

    let ready = diags.is_empty();
    let mut payload = Vec::new();
    if ready {
        let summary: &returnsv1::Summary = &fed.summary;
        let tax_year = outputs.header.as_option().map_or(0, |h| h.tax_year);
        let env = MefSubmissionEnvelope {
            return_version: format!("{tax_year}.1.0"),
            tax_year,
            taxpayer_tin: tp_tin,
            spouse_tin: sp_tin,
            filing_status: summary.filing_status.to_string(),
            total_income: money_minor(summary.total_income.as_option()) / 100,
            agi: money_minor(summary.adjusted_gross_income.as_option()) / 100,
            taxable_income: money_minor(summary.taxable_income.as_option()) / 100,
            total_tax: money_minor(summary.total_tax.as_option()) / 100,
            total_payments: money_minor(summary.total_payments_and_refundable_credits.as_option())
                / 100,
            refund_amt: money_minor(summary.refund.as_option()) / 100,
            amount_owed_amt: money_minor(summary.amount_owed.as_option()) / 100,
        };
        payload = env.marshal_indent().into_bytes();
    }

    returnsv1::EFileSubmission {
        jurisdiction: "US".to_string(),
        ready_to_transmit: ready,
        diagnostics: diags,
        payload,
        ..Default::default()
    }
}

fn extract_tin_value(tin: Option<&returnsv1::TaxpayerID>) -> String {
    use proto::returnsv1::__buffa::oneof::taxpayer_id::Value as Tin;
    match tin.and_then(|t| t.value.as_ref()) {
        Some(Tin::Number(n)) => n.trim().to_string(),
        _ => String::new(),
    }
}

fn collect_message_cells(
    msg: &DynamicMessage,
    prefix: &str,
    default_cites: &[returnsv1::Citation],
    out: &mut Vec<returnsv1::Cell>,
) {
    let md = msg.message_descriptor();
    for fd in md.fields() {
        if !msg.has(fd) {
            continue;
        }
        let path = format!("{prefix}.{}", fd.name());
        match fd.kind() {
            FieldKind::List(_) | FieldKind::Map { .. } => continue,
            FieldKind::Singular(SingularKind::Message(sub_idx)) => {
                let sub_name = msg.pool().message(sub_idx).full_name();
                let msg_name = sub_name.rsplit('.').next().unwrap_or(sub_name);
                if msg_name == "Money" || msg_name == "Quantity" || msg_name == "Date" {
                    out.push(cell(&path, default_cites));
                    continue;
                }
                if let Some(DynValue::Message(sub)) = msg.field_by_number(fd.number()) {
                    collect_message_cells(sub, &path, default_cites, out);
                }
                continue;
            }
            FieldKind::Singular(_) => out.push(cell(&path, default_cites)),
        }
    }
}

fn cell(path: &str, default_cites: &[returnsv1::Citation]) -> returnsv1::Cell {
    returnsv1::Cell {
        path: path.to_string(),
        state: returnsv1::CellState::CELL_STATE_KNOWN.into(),
        source: classify_cell_source(path).into(),
        formula: format!("Computed by taxlisp federal rule pack ({path})"),
        citations: citations_for_cell_path(path, default_cites),
        ..Default::default()
    }
}

fn classify_cell_source(path: &str) -> returnsv1::CellSource {
    if path.contains("standard_deduction") {
        returnsv1::CellSource::CELL_SOURCE_STATUTORY_PARAMETER
    } else if path.contains("deduction_amount") || path.contains("filing_status") {
        returnsv1::CellSource::CELL_SOURCE_ELECTION
    } else {
        returnsv1::CellSource::CELL_SOURCE_COMPUTED
    }
}

fn citations_for_cell_path(
    path: &str,
    fallback: &[returnsv1::Citation],
) -> Vec<returnsv1::Citation> {
    let cites = |list: &[&str]| list.iter().map(|c| citation(c)).collect();
    if path.contains("schedule_c") {
        cites(&["26 U.S.C. § 162", "Schedule C (Form 1040) Instructions"])
    } else if path.contains("schedule_e") {
        cites(&["26 U.S.C. § 469", "Schedule E (Form 1040) Instructions"])
    } else if path.contains("schedule_se") {
        cites(&["26 U.S.C. § 1401", "26 U.S.C. § 1402"])
    } else if path.contains("form_8995") {
        cites(&["26 U.S.C. § 199A"])
    } else if path.contains("form_6251") {
        cites(&["26 U.S.C. § 55"])
    } else if path.contains("form_8606") {
        cites(&["26 U.S.C. § 408(d)(2)"])
    } else if path.contains("form_8812") {
        cites(&["26 U.S.C. § 24"])
    } else if path.contains("form_8863") {
        cites(&["26 U.S.C. § 25A"])
    } else if path.contains("form_2441") {
        cites(&["26 U.S.C. § 21"])
    } else if !fallback.is_empty() {
        fallback.to_vec()
    } else {
        cites(&["26 U.S.C. § 1", "Form 1040 Instructions"])
    }
}

/// The symbol name of an atom (Go's GetAtom().GetSymbol().GetName()), or "".
fn atom_symbol_name(e: &taxlispv1::Expression) -> &str {
    match &e.value {
        Some(ExprValue::Atom(a)) => match &a.primitive {
            Some(Primitive::Symbol(s)) => &s.name,
            _ => "",
        },
        _ => "",
    }
}

/// The string of a string atom (Go's GetAtom().GetStringValue()), or "".
fn atom_string_value(e: &taxlispv1::Expression) -> &str {
    match &e.value {
        Some(ExprValue::Atom(a)) => match &a.primitive {
            Some(Primitive::StringValue(s)) => s,
            _ => "",
        },
        _ => "",
    }
}

fn atom_keyword(e: &taxlispv1::Expression) -> &str {
    match &e.value {
        Some(ExprValue::Atom(a)) => match &a.primitive {
            Some(Primitive::Keyword(k)) => k,
            _ => "",
        },
        _ => "",
    }
}

fn index_defn_citations(
    module: Option<&taxlispv1::ModuleFile>,
) -> HashMap<String, Vec<returnsv1::Citation>> {
    let mut result: HashMap<String, Vec<returnsv1::Citation>> = HashMap::new();
    let Some(module) = module else {
        return result;
    };
    for expr in &module.expressions {
        let Some(ExprValue::List(list)) = &expr.value else {
            continue;
        };
        let elems = &list.expressions;
        if elems.len() < 3 {
            continue;
        }
        let head = atom_symbol_name(&elems[0]);
        if head != "def" && head != "defn" {
            continue;
        }
        let name = atom_symbol_name(&elems[1]);
        if name.is_empty() {
            continue;
        }
        let mut i = 2;
        while i + 1 < elems.len() {
            if atom_keyword(&elems[i]) == "cite" {
                let val = &elems[i + 1];
                let c = atom_string_value(val);
                if !c.is_empty() {
                    result
                        .entry(name.to_string())
                        .or_default()
                        .push(citation(c));
                } else if let Some(ExprValue::List(sub)) = &val.value {
                    for item in &sub.expressions {
                        let sc = atom_string_value(item);
                        if !sc.is_empty() {
                            result
                                .entry(name.to_string())
                                .or_default()
                                .push(citation(sc));
                        }
                    }
                }
            }
            i += 1;
        }
    }
    result
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn xml_escape_matches_go() {
        assert_eq!(
            xml_escape("a<b>&\"'\t\n\r"),
            "a&lt;b&gt;&amp;&#34;&#39;&#x9;&#xA;&#xD;"
        );
        assert_eq!(xml_escape("\u{1}"), "\u{FFFD}");
    }

    #[test]
    fn to_upper_keeps_runes_without_single_uppercase() {
        assert_eq!(go_to_upper("ca"), "CA");
        assert_eq!(go_to_upper("ß"), "ß");
    }
}
