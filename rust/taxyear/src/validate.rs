//! The file's and each scenario's checks: validate.go.

use std::collections::{BTreeMap, HashMap, HashSet};
use std::sync::Arc;

use buffa_descriptor::{
    DynamicMessage, FieldDescriptor, FieldKind, MessageDescriptor, SingularKind, Value,
};
use proto::commonv1;
use proto::scenariosv1::{
    __buffa::oneof::{employer_part::Part as EmployerPartKind, r#match::To},
    EmployerModel, Match, PayFrequency, Problem, ScenarioInputs, SupplementalMethod, TaxYearModel,
    TaxYearScenarios,
};

use crate::internal::civil::{self, Date};
use crate::model::{Context, employer};
use crate::models::ModelSet;
use crate::resolve::problem;
use crate::{Resolved, goquote, reflect};

/// CheckFile runs the checks that need only the file and no parent
/// scenario: the version and year, and no identifiers in any form. Each
/// failure is a problem with its path. See docs/scenarios.md.
pub fn check_file(file: &TaxYearScenarios) -> Vec<Problem> {
    let mut out = Vec::new();
    header_checks(file, &mut out);
    if let Some(d) = reflect::to_dyn(file) {
        identifiers("", &d, &mut out);
    }
    out
}

/// headerChecks checks the version, the year and as_of.
fn header_checks(file: &TaxYearScenarios, out: &mut Vec<Problem>) {
    if file.schema_version != 1 {
        out.push(problem(
            "schema_version",
            format!(
                "Schema version {} is not one this build reads; it reads 1.",
                file.schema_version
            ),
        ));
    }
    let y = file.shared.facts.tax_year;
    if y != 0 && y != file.tax_year {
        out.push(problem(
            "shared.facts.tax_year",
            format!("The facts are for {y} and the file for {}.", file.tax_year),
        ));
    }
    if civil::from_field(&file.as_of).is_err() || file.as_of.is_unset() {
        out.push(problem("as_of", "as_of is not a day."));
    }
}

/// checkFileWith is CheckFile with the identifiers under shared already
/// found.
pub(crate) fn check_file_with(file: &TaxYearScenarios, shared: &[Problem]) -> Vec<Problem> {
    let mut out = Vec::new();
    header_checks(file, &mut out);
    let Some(d) = reflect::to_dyn(file) else {
        return out;
    };
    for f in reflect::set_fields(&d) {
        if f.name() == "shared" {
            out.extend_from_slice(shared);
        } else if let Some(v) = d.field_by_number(f.number()) {
            identifiers_in("", reflect::full_name(&d), false, f, v, &mut out);
        }
    }
    out
}

/// cleared are the fields the facts doc clears on every form.
const CLEARED: [&str; 17] = [
    "tin",
    "account_number",
    "control_number",
    "state_id",
    "payer_routing_number",
    "refund_routing_number",
    "refund_account_number",
    "account_or_escrow_number",
    "marketplace_identifier",
    "marketplace_assigned_policy_number",
    "spouse_tin",
    "account_beneficiary_tin",
    "proprietor_tin",
    "employer_id_number",
    "taxpayer_tin",
    "buyer_tin",
    "filer_tin",
];

/// opaque are text fields that name a build or a reference, never a
/// person or an account, which the nine-digit check skips.
const OPAQUE: [&str; 1] = ["nexteggapp.taxcalcs.scenarios.v1.TaxYearScenarios.exported_from"];

/// nineDigits is nine digits in a row, with or without dashes between
/// them: `\d(?:-?\d){8}`.
fn nine_digits(s: &str) -> bool {
    let b = s.as_bytes();
    for i in 0..b.len() {
        if !b[i].is_ascii_digit() {
            continue;
        }
        let (mut count, mut j) = (1, i + 1);
        while count < 9 {
            if j < b.len() && b[j].is_ascii_digit() {
                j += 1;
            } else if j + 1 < b.len() && b[j] == b'-' && b[j + 1].is_ascii_digit() {
                j += 2;
            } else {
                break;
            }
            count += 1;
        }
        if count == 9 {
            return true;
        }
    }
    false
}

/// identifiers records each cleared field that is set, each Party address
/// or telephone, and each text field holding nine digits in a row.
pub(crate) fn identifiers(path: &str, m: &DynamicMessage, out: &mut Vec<Problem>) {
    let name = reflect::full_name(m);
    let party = name.rsplit('.').next() == Some("Party");
    for f in reflect::set_fields(m) {
        if let Some(v) = m.field_by_number(f.number()) {
            identifiers_in(path, name, party, f, v, out);
        }
    }
}

/// identifiersIn is identifiers on one field of a message, of type msg, at
/// path.
fn identifiers_in(
    path: &str,
    msg: &str,
    party: bool,
    f: &FieldDescriptor,
    v: &Value,
    out: &mut Vec<Problem>,
) {
    let p = if path.is_empty() {
        f.name().to_string()
    } else {
        format!("{path}.{}", f.name())
    };
    match (f.kind(), v) {
        (FieldKind::List(SingularKind::Message(_)), Value::List(l)) => {
            for (i, item) in l.iter().enumerate() {
                if let Some(m) = reflect::as_message(item) {
                    identifiers(&format!("{p}[{i}]"), m, out);
                }
            }
        }
        (FieldKind::Singular(SingularKind::Message(_)), Value::Message(m)) => {
            identifiers(&p, m, out)
        }
        (
            FieldKind::Singular(SingularKind::Scalar(buffa_descriptor::ScalarType::String)),
            Value::String(s),
        ) => {
            let full = format!("{msg}.{}", f.name());
            if OPAQUE.contains(&full.as_str()) {
                return;
            }
            if CLEARED.contains(&f.name())
                || (party && (f.name() == "address" || f.name() == "telephone"))
            {
                out.push(problem(
                    &p,
                    "This field must be empty: the file carries nothing that identifies a person or reaches an account.",
                ));
            } else if nine_digits(s) {
                out.push(problem(
                    &p,
                    "This holds nine digits in a row, which could be a Social Security number or an account number.",
                ));
            }
        }
        _ => {}
    }
}

/// kinds says, for one model's part message, which oneof cases are known,
/// which may appear more than once, which are ends, which is the known
/// end, which are whole-year or rest-of-year parts, of which a model holds
/// one, and which are period parts.
#[derive(Clone, Copy, Default)]
struct Kinds {
    known: &'static [&'static str],
    multi: &'static [&'static str],
    ends: &'static [&'static str],
    known_end: &'static str,
    totals: &'static [&'static str],
    periods: &'static [&'static str],
}

fn has(list: &[&str], s: &str) -> bool {
    list.contains(&s)
}

const PKG: &str = "nexteggapp.taxcalcs.scenarios.v1.";

fn part_kinds(full_name: &str) -> Option<Kinds> {
    let name = full_name.strip_prefix(PKG)?;
    Some(match name {
        "EmployerPart" => Kinds {
            known: &["from_paystubs", "from_deposits", "from_w2", "ended"],
            multi: &["from_w2"],
            ends: &["ended", "ends"],
            known_end: "ended",
            totals: &["year_total"],
            periods: &["pay_rate"],
        },
        "BusinessPart" => Kinds {
            known: &["from_ledger", "from_forms", "stopped"],
            ends: &["stopped", "stops"],
            known_end: "stopped",
            totals: &["year_total"],
            periods: &["monthly"],
            ..Kinds::default()
        },
        "AccountPart" => Kinds {
            known: &["from_credits", "from_forms", "closed"],
            ends: &["closed", "closes"],
            known_end: "closed",
            totals: &["year_total"],
            periods: &["at_rate"],
            ..Kinds::default()
        },
        "BrokerPart" => Kinds {
            known: &["from_trades", "from_forms", "purchases"],
            ..Kinds::default()
        },
        "MortgagePart" => Kinds {
            known: &[
                "from_form_1098",
                "from_statements",
                "paid_from_bank",
                "repaid",
            ],
            ends: &["repaid", "paid_off"],
            known_end: "repaid",
            totals: &["year_total", "rest_of_year"],
            periods: &["refinanced"],
            ..Kinds::default()
        },
        "PropertyTaxPart" => Kinds {
            known: &["from_bill", "paid_from_bank"],
            totals: &["year_total", "rest_of_year"],
            ..Kinds::default()
        },
        "CharityPart" => Kinds {
            known: &["gift_from_bank", "gift_with_receipt"],
            totals: &["rest_of_year"],
            ..Kinds::default()
        },
        "MedicalPart" => Kinds {
            known: &["paid_from_bank", "paid_with_receipt"],
            totals: &["year_total", "rest_of_year"],
            ..Kinds::default()
        },
        "OtherIncomePart" => Kinds {
            known: &["from_forms", "from_ledger"],
            totals: &["year_amount"],
            ..Kinds::default()
        },
        "TaxPaidPart" => Kinds {
            known: &["payment_made", "overpayment_applied"],
            multi: &["payment_made", "overpayment_applied"],
            ..Kinds::default()
        },
        "RetirementPart" => Kinds {
            known: &["from_forms"],
            ..Kinds::default()
        },
        _ => return None,
    })
}

/// KnownKinds returns a model part message's known cases, in field order:
/// the cases that hold evidence, or an end that has happened. Every other
/// case is assumed. It returns None for a message that isn't a model's
/// part, and no cases for a part with none, as RetirementPart.
pub fn known_kinds(part: &MessageDescriptor) -> Option<Vec<&FieldDescriptor>> {
    let k = part_kinds(part.full_name())?;
    Some(
        part.fields()
            .iter()
            .filter(|f| has(k.known, f.name()))
            .collect(),
    )
}

/// sourced is one model in a scenario's inputs, found by reflection.
#[derive(Clone, Debug)]
pub(crate) struct Sourced {
    pub(crate) list: String,
    pub(crate) id: String,
    /// The model, or for itemised its kind's model.
    pub(crate) msg: DynamicMessage,
    /// The model message itself.
    pub(crate) top: DynamicMessage,
    /// The parts, when the model has a parts field.
    pub(crate) parts: Option<Vec<DynamicMessage>>,
    /// The parts' message type's full name, or "".
    part_name: String,
    pub(crate) path: String,
}

/// models lists every model in a TaxYearModel.
pub(crate) fn models(m: &TaxYearModel) -> Vec<Sourced> {
    let mut out = Vec::new();
    let Some(r) = reflect::to_dyn(m) else {
        return out;
    };
    let Some(md) = reflect::desc(&r) else {
        return out;
    };
    for f in md.fields() {
        for top in reflect::list(&r, f).iter().filter_map(reflect::as_message) {
            let id = reflect::string_named(top, "id").to_string();
            let mut inner = top;
            if let Some(which) = reflect::which_named(top, "kind")
                && let Some(sub) = reflect::message(top, which)
            {
                inner = sub;
            }
            let mut s = Sourced {
                list: f.name().to_string(),
                id: id.clone(),
                msg: inner.clone(),
                top: top.clone(),
                parts: None,
                part_name: String::new(),
                path: format!("model.{}[{id}]", f.name()),
            };
            if let Some(pf) = reflect::field_of(inner, "parts") {
                s.parts = Some(
                    reflect::list(inner, pf)
                        .iter()
                        .filter_map(reflect::as_message)
                        .cloned()
                        .collect(),
                );
                s.part_name = reflect::message_name(pf).to_string();
            }
            out.push(s);
        }
    }
    out
}

impl Sourced {
    fn part_name(&self) -> &str {
        &self.part_name
    }

    fn parts(&self) -> &[DynamicMessage] {
        self.parts.as_deref().unwrap_or(&[])
    }
}

/// partCase returns a part's id, the name of its oneof case and the case's
/// message.
pub(crate) fn part_case(part: &DynamicMessage) -> (String, String, Option<&DynamicMessage>) {
    let id = reflect::string_named(part, "id").to_string();
    let Some(md) = reflect::desc(part) else {
        return (id, String::new(), None);
    };
    let Some(o) = reflect::oneof(md, "part") else {
        return (id, String::new(), None);
    };
    let Some(which) = reflect::which_oneof(part, o) else {
        return (id, String::new(), None);
    };
    (id, which.name().to_string(), reflect::message(part, which))
}

/// Validate runs the models doc's checks on one scenario's resolved
/// inputs: each failure is a problem with its path in the inputs. Check 2,
/// that no two items share an id, runs on the result. See docs/models.md.
pub fn validate(ctx: &Context, in_: &ScenarioInputs) -> Vec<Problem> {
    validate_with(ctx, in_, &ModelSet::uncached(ctx))
}

/// validateWith is Validate with each model's own checks kept in ms's
/// cache: they read the model and the context alone. The ids each model
/// records are replayed into one map, so an id two models share is still
/// a repeat.
pub(crate) fn validate_with(
    ctx: &Context,
    in_: &ScenarioInputs,
    ms: &ModelSet<'_>,
) -> Vec<Problem> {
    let mut v = Validator::new(Some(ctx));
    v.facts(in_);
    for s in models(&in_.model) {
        let c = ms.checked(&s, || check_model(ctx, &s));
        for e in &c.ids {
            v.id(&e.id, &e.path);
        }
        v.out.extend(c.problems.iter().cloned());
    }
    v.plans(in_);
    v.override_paths(in_);
    v.out.sort_by(|a, b| a.path.cmp(&b.path));
    v.out
}

struct Validator<'c> {
    ctx: Option<&'c Context>,
    /// idsOnly runs checks 1 and 3's id checks alone.
    ids_only: bool,
    out: Vec<Problem>,
    /// ids maps every id to the path it was first seen at.
    ids: HashMap<String, String>,
    /// record, when set, collects each id instead of checking it.
    record: Option<Vec<IdEvent>>,
}

/// idEvent is one id a model's checks record, at its path.
#[derive(Clone, Debug)]
pub(crate) struct IdEvent {
    id: String,
    path: String,
}

/// checkEntry is one model's own checks: the ids it records, in order,
/// and every other problem. Nothing in it is changed once it is made.
#[derive(Clone, Debug, Default)]
pub(crate) struct CheckEntry {
    ids: Vec<IdEvent>,
    problems: Vec<Problem>,
}

/// checkModel runs one model's checks, recording its ids.
fn check_model(ctx: &Context, s: &Sourced) -> Arc<CheckEntry> {
    let mut r = Validator::new(Some(ctx));
    r.record = Some(Vec::new());
    r.model(s);
    Arc::new(CheckEntry {
        ids: r.record.unwrap_or_default(),
        problems: r.out,
    })
}

impl<'c> Validator<'c> {
    fn new(ctx: Option<&'c Context>) -> Self {
        Validator {
            ctx,
            ids_only: false,
            out: Vec::new(),
            ids: HashMap::new(),
            record: None,
        }
    }

    fn fail(&mut self, path: &str, text: impl Into<String>) {
        self.out.push(problem(path, text));
    }

    /// id records an id, failing check 1 on a repeat.
    fn id(&mut self, id: &str, path: &str) {
        if let Some(r) = self.record.as_mut() {
            r.push(IdEvent {
                id: id.to_string(),
                path: path.to_string(),
            });
            return;
        }
        if id.is_empty() {
            self.fail(path, "An id is required.");
            return;
        }
        if let Some(first) = self.ids.get(id) {
            let text = format!(
                "The id {} is used again; it is {first}'s.",
                goquote::quote(id)
            );
            self.fail(path, text);
            return;
        }
        self.ids.insert(id.to_string(), path.to_string());
    }

    fn facts(&mut self, in_: &ScenarioInputs) {
        let f = &in_.facts;
        for p in &f.people {
            self.id(&p.id, &format!("facts.people[{}]", p.id));
        }
        for r in &f.residence {
            self.id(&r.id, &format!("facts.residence[{}]", r.id));
        }
        for e in &f.elections {
            self.id(&e.id, &format!("facts.elections[{}]", e.id));
        }
        for p in &f.positions {
            self.id(&p.id, &format!("facts.positions[{}]", p.id));
        }
        for c in &f.carryovers {
            self.id(&c.id, &format!("facts.carryovers[{}]", c.id));
        }
        for p in &f.prior_year {
            self.id(&p.id, &format!("facts.prior_year[{}]", p.id));
        }
        for u in &f.unassigned {
            self.id(&u.id, &format!("facts.unassigned[{}]", u.id));
        }
    }

    /// model runs checks 1 and 3 to 9 on one model.
    fn model(&mut self, s: &Sourced) {
        self.id(&s.id, &s.path);
        let mut t = Tally::default();
        for part in s.parts() {
            self.part(s, part, &mut t);
        }
        if self.ids_only {
            return;
        }
        if t.ends.len() > 1 {
            self.fail(&s.path, "A model has at most one end part.");
        }
        if t.totals.len() > 1 {
            self.fail(
                &s.path,
                "A model has at most one total for the year or for the rest of it.",
            );
        }
        let mut seen: HashMap<String, String> = HashMap::new();
        for st in &t.starts {
            let key = st.at.to_string();
            if let Some(other) = seen.get(&key) {
                let when = if st.at.is_zero() {
                    "an unset start".to_string()
                } else {
                    st.at.long()
                };
                let text = format!("Two changes, this and {other}, start on {when}.");
                self.fail(&format!("{}.parts[{}]", s.path, st.part), text);
            }
            seen.insert(key, st.part.clone());
        }
        self.default_part(s);
        self.answers(s, &t.evidence);
        if s.list == "employers" {
            self.employer(s, &t.starts);
        }
    }

    /// part checks one part's id and kind, and tallies it.
    fn part(&mut self, s: &Sourced, part: &DynamicMessage, t: &mut Tally) {
        let k = part_kinds(s.part_name());
        let (id, kind, body) = part_case(part);
        let path = format!("{}.parts[{id}]", s.path);
        self.id(&id, &path);
        if kind.is_empty() {
            self.fail(&path, "The part has no kind set.");
            return;
        }
        let Some(k) = k else {
            return;
        };
        if has(k.known, &kind)
            && let Some(body) = body
        {
            collect_evidence(body, &path, &mut t.evidence, self);
        }
        if self.ids_only {
            return;
        }
        let count = t.count.entry(kind.clone()).or_insert(0);
        *count += 1;
        if has(k.known, &kind) && *count > 1 && !has(k.multi, &kind) {
            self.fail(&path, format!("A model has at most one {kind} part."));
        }
        if has(k.ends, &kind) {
            t.ends.push(kind.clone());
            if kind == k.known_end {
                self.known_end(body, &path);
            }
        }
        if has(k.totals, &kind) {
            t.totals.push(id.clone());
        }
        if has(k.periods, &kind) {
            let from = body.and_then(|b| date_field(b, "from"));
            let d = match civil::from_proto(from.as_ref()) {
                Ok(d) => d,
                Err(_) => {
                    self.fail(&format!("{path}.{kind}.from"), "This is not a day.");
                    Date::default()
                }
            };
            t.starts.push(Start { part: id, at: d });
        }
    }

    /// knownEnd checks 6: a known end is on or before as_of.
    fn known_end(&mut self, body: Option<&DynamicMessage>, path: &str) {
        let d = body.and_then(|b| date_field(b, "last_paycheck").or_else(|| date_field(b, "on")));
        let at = match civil::from_proto(d.as_ref()) {
            Ok(at) if !at.is_zero() => at,
            _ => {
                self.fail(path, "An end needs its date.");
                return;
            }
        };
        let Some(ctx) = self.ctx else {
            return;
        };
        if at.after(ctx.as_of()) {
            let text = format!(
                "A known end, on {}, can't be after as_of, {}; record it as expected instead.",
                at.long(),
                ctx.as_of().long()
            );
            self.fail(path, text);
        }
    }

    /// defaultPart checks 4: a model with slots after its evidence has a
    /// default part. A model whose year a whole-year document states needs
    /// none.
    fn default_part(&mut self, s: &Sourced) {
        let Some(f) = reflect::field_of(&s.msg, "default_part") else {
            return;
        };
        if reflect::has(&s.msg, f) {
            return;
        }
        if self.ctx.is_some_and(Context::finished) {
            return;
        }
        for part in s.parts() {
            let (_, kind, _) = part_case(part);
            if matches!(
                kind.as_str(),
                "from_w2" | "from_form_1098" | "ended" | "stopped" | "closed" | "repaid"
            ) {
                return;
            }
        }
        self.fail(
            &format!("{}.default_part", s.path),
            "A model with dates still to come needs a default part.",
        );
    }

    /// answers checks 8: a planned part's actual names evidence in the
    /// model.
    fn answers(&mut self, s: &Sourced, evidence: &HashSet<String>) {
        for part in s.parts() {
            let (id, kind, body) = part_case(part);
            let Some(body) = body else {
                continue;
            };
            let Some(a) = reflect::message_named(body, "actual") else {
                continue;
            };
            let path = format!("{}.parts[{id}].{kind}.actual", s.path);
            let mut names = vec![reflect::string_named(a, "is").to_string()];
            for v in reflect::list_named(a, "is_not") {
                if let Value::String(x) = v {
                    names.push(x.clone());
                }
            }
            for e in names {
                if !e.is_empty() && !evidence.contains(&e) {
                    self.fail(
                        &path,
                        format!("{} names no evidence in this model.", goquote::quote(&e)),
                    );
                }
            }
        }
    }

    /// employer runs the employer's own checks: required fields (9),
    /// matches naming a pay date (8), and period parts within the job (5).
    fn employer(&mut self, s: &Sourced, starts: &[Start]) {
        let m: EmployerModel = reflect::from_dyn(&s.top).unwrap_or_default();
        let need = |v: &mut Self, ok: bool, field: &str, text: &str| {
            if !ok {
                v.fail(&format!("{}.{field}", s.path), text);
            }
        };
        need(
            self,
            !m.person.is_empty(),
            "person",
            "Whose job it is is required.",
        );
        need(
            self,
            m.pay_frequency.to_i32() != PayFrequency::PAY_FREQUENCY_UNSPECIFIED as i32,
            "pay_frequency",
            "How often the job pays is required.",
        );
        need(
            self,
            m.supplemental.to_i32() != SupplementalMethod::SUPPLEMENTAL_METHOD_UNSPECIFIED as i32,
            "supplemental",
            "How the employer withholds from a bonus is required.",
        );
        let weekly = matches!(
            m.pay_frequency.as_known(),
            Some(PayFrequency::PAY_FREQUENCY_WEEKLY | PayFrequency::PAY_FREQUENCY_BIWEEKLY)
        );
        need(
            self,
            !weekly || m.first_pay_date.is_set(),
            "first_pay_date",
            "A weekly or every-other-week job needs its first pay date.",
        );
        let first = civil::from_field(&m.first_pay_date).unwrap_or_default();
        let from = civil::from_field(&m.starts).unwrap_or_default();
        let Some(ctx) = self.ctx else {
            return;
        };
        let slots: HashSet<String> = employer::pay_dates(ctx.year(), m.pay_frequency, first, from)
            .iter()
            .map(ToString::to_string)
            .collect();
        let end = self.employer_parts(s, &m, &slots);
        for st in starts {
            let path = format!("{}.parts[{}]", s.path, st.part);
            if !st.at.is_zero() && !from.is_zero() && st.at.before(from) {
                self.fail(
                    &path,
                    format!(
                        "A change can't start before the job does, on {}.",
                        from.long()
                    ),
                );
            }
            if !st.at.is_zero() && !end.is_zero() && st.at.after(end) {
                self.fail(
                    &path,
                    format!(
                        "A change can't start after the job's planned end on {}.",
                        end.long()
                    ),
                );
            }
        }
    }

    /// employerParts checks a job's one-offs have their date and amount,
    /// and that every answer naming a pay date names one of the job's. It
    /// returns the job's assumed end, or zero.
    fn employer_parts(&mut self, s: &Sourced, m: &EmployerModel, slots: &HashSet<String>) -> Date {
        let mut end = Date::default();
        for p in &m.parts {
            let path = format!("{}.parts[{}]", s.path, p.id);
            let mut matches: Vec<&Match> = Vec::new();
            match &p.part {
                Some(EmployerPartKind::Ends(e)) => {
                    end = civil::from_field(&e.last_paycheck).unwrap_or_default();
                }
                Some(EmployerPartKind::OneOff(o)) => {
                    if o.date.is_unset() || o.amount.is_unset() {
                        self.fail(
                            &format!("{path}.one_off"),
                            "A one-off needs its date and amount.",
                        );
                    }
                }
                Some(EmployerPartKind::FromPaystubs(f)) => {
                    matches.extend(f.stubs.iter().map(|st| &*st.matches));
                }
                Some(EmployerPartKind::FromDeposits(f)) => {
                    matches.extend(f.deposits.iter().map(|d| &*d.matches));
                }
                _ => {}
            }
            for mt in matches {
                let Some(To::Slot(slot)) = &mt.to else {
                    continue;
                };
                let (d, ok) = match civil::from_proto(Some(slot)) {
                    Ok(d) => (d, true),
                    Err(_) => (Date::default(), false),
                };
                if !ok || !slots.contains(&d.to_string()) {
                    self.fail(
                        &path,
                        format!("An answer names {d}, which is not one of the job's pay dates."),
                    );
                }
            }
        }
        end
    }

    /// plans checks 7: a person has at most one plan adjustment part.
    fn plans(&mut self, in_: &ScenarioInputs) {
        use proto::scenariosv1::__buffa::oneof::retirement_part::Part;
        let mut by_person: HashMap<String, i64> = HashMap::new();
        for m in &in_.model.retirement {
            for p in &m.parts {
                if matches!(
                    p.part,
                    Some(Part::MaxOutWorkplacePlan(_) | Part::WorkplacePlanTotal(_))
                ) {
                    let n = by_person.entry(m.person.clone()).or_insert(0);
                    *n += 1;
                    if *n > 1 {
                        self.fail(
                            &format!("model.retirement[{}].parts[{}]", m.id, p.id),
                            "A person has at most one plan for their workplace plan.",
                        );
                    }
                }
            }
        }
    }

    /// overridePaths checks the scenarios doc's check 8: an override names
    /// a path in its table.
    fn override_paths(&mut self, in_: &ScenarioInputs) {
        for o in &in_.overrides {
            if !overridable_path(&o.path) {
                self.fail(
                    &format!("overrides[{}]", o.path),
                    "Only a figure so far, last year's figures, or a state's tax can be overridden.",
                );
            }
        }
    }
}

/// tally is what one model's parts hold, as the checks count it.
#[derive(Default)]
struct Tally {
    evidence: HashSet<String>,
    count: HashMap<String, i64>,
    ends: Vec<String>,
    totals: Vec<String>,
    starts: Vec<Start>,
}

/// start is a period part's start, zero when unset.
#[derive(Clone, Debug)]
struct Start {
    part: String,
    at: Date,
}

/// collectEvidence records each evidence entry's id in a known part: every
/// message inside it with an id field. Check 3: ids are unique in a model.
fn collect_evidence(
    m: &DynamicMessage,
    path: &str,
    seen: &mut HashSet<String>,
    v: &mut Validator<'_>,
) {
    for f in reflect::set_fields(m) {
        if !reflect::is_message_kind(f) || !reflect::is_list(f) {
            continue;
        }
        for e in reflect::list(m, f).iter().filter_map(reflect::as_message) {
            let Some(idf) = reflect::field_of(e, "id") else {
                continue;
            };
            let id = reflect::string(e, idf).to_string();
            let p = format!("{path}.{}[{id}]", f.name());
            if id.is_empty() {
                v.fail(&p, "An evidence entry needs an id.");
            } else if seen.contains(&id) {
                v.fail(
                    &p,
                    format!(
                        "The evidence id {} is used twice in this model.",
                        goquote::quote(&id)
                    ),
                );
            }
            seen.insert(id);
        }
    }
    for name in ["transfer", "confirmation"] {
        if let Some(e) = reflect::message_named(m, name) {
            let id = reflect::string_named(e, "id").to_string();
            if !id.is_empty() {
                if seen.contains(&id) {
                    v.fail(
                        path,
                        format!(
                            "The evidence id {} is used twice in this model.",
                            goquote::quote(&id)
                        ),
                    );
                }
                seen.insert(id);
            }
        }
    }
}

/// The date field of a part's message, when it is set.
fn date_field(m: &DynamicMessage, name: &str) -> Option<commonv1::Date> {
    reflect::from_dyn(reflect::message_named(m, name)?)
}

/// CheckIDs runs the id checks alone, 1 and the first of 3, on shared
/// before any scenario is resolved: apply can't key a list with a repeated
/// id, so a repeat there would be reported as every scenario failing.
pub fn check_ids(in_: &ScenarioInputs) -> Vec<Problem> {
    let mut v = Validator::new(None);
    v.ids_only = true;
    v.facts(in_);
    for s in models(&in_.model) {
        v.model(&s);
    }
    for p in &mut v.out {
        p.path = format!("shared.{}", p.path);
    }
    v.out
}

/// OverridablePath reports whether a path is one the scenarios doc's table
/// lets an override name: a figure so far in item terms, last year's tax,
/// AGI or withholding, or a state's tax for the year.
pub fn overridable_path(p: &str) -> bool {
    if p.starts_with("projection.") && p.contains("].so_far.items.") {
        return true;
    }
    if p.starts_with("estimated[") {
        for f in [
            "].last_year_tax",
            "].last_year_agi",
            "].last_year_withheld",
            "].tax_for_year",
        ] {
            if p.ends_with(f) {
                return !(f == "].tax_for_year" && p.starts_with("estimated[US]"));
            }
        }
    }
    false
}

/// CheckScenarios runs the checks that compare a scenario with shared: no
/// scenario changes or adds a known part, every override sits where the
/// table puts it, and Your plan's changes hold only what they may.
pub fn check_scenarios(
    file: &TaxYearScenarios,
    resolved: &[Resolved],
) -> HashMap<String, Vec<Problem>> {
    let checks = check_shared(&file.shared);
    let mut out = HashMap::new();
    if !checks.problems.is_empty() {
        out.insert(String::new(), checks.problems.clone());
    }
    for r in resolved {
        let p = checks.scenario(r.inputs());
        if !p.is_empty() {
            out.insert(r.scenario().id.clone(), p);
        }
    }
    out
}

/// sharedChecks is what CheckScenarios reads from shared: its known parts
/// and overrides, and the problems with shared alone.
#[derive(Clone, Debug, Default)]
pub(crate) struct SharedChecks {
    known: HashMap<String, DynamicMessage>,
    overrides: HashSet<String>,
    pub(crate) problems: Vec<Problem>,
}

pub(crate) fn check_shared(shared: &ScenarioInputs) -> SharedChecks {
    let mut c = SharedChecks {
        known: known_parts(shared),
        ..SharedChecks::default()
    };
    for o in &shared.overrides {
        c.overrides.insert(o.path.clone());
        if o.path.ends_with("].tax_for_year") {
            c.problems.push(problem(
                &format!("shared.overrides[{}]", o.path),
                "A state's tax for the year is overridden in a scenario's changes, not in shared.",
            ));
        }
    }
    c
}

impl SharedChecks {
    /// scenario runs the checks on one scenario's resolved inputs, in path
    /// order.
    pub(crate) fn scenario(&self, in_: &ScenarioInputs) -> Vec<Problem> {
        let mut out = Vec::new();
        let mine: BTreeMap<String, DynamicMessage> = known_parts(in_).into_iter().collect();
        for (path, part) in &mine {
            if self.known.get(path) != Some(part) {
                out.push(problem(
                    path,
                    "A known part, which holds evidence, is in shared; no scenario adds or changes one.",
                ));
            }
        }
        for o in &in_.overrides {
            if !self.overrides.contains(&o.path) && !o.path.ends_with("].tax_for_year") {
                out.push(problem(
                    &format!("overrides[{}]", o.path),
                    "A figure so far or last year's figure is overridden in shared, for every scenario.",
                ));
            }
        }
        out
    }
}

/// knownParts maps each known part's path to the part.
fn known_parts(in_: &ScenarioInputs) -> HashMap<String, DynamicMessage> {
    let mut out = HashMap::new();
    for s in models(&in_.model) {
        let k = part_kinds(s.part_name()).unwrap_or_default();
        for part in s.parts() {
            let (id, kind, _) = part_case(part);
            if has(k.known, &kind) {
                out.insert(format!("{}.parts[{id}]", s.path), part.clone());
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn nine_digits_as_the_regexp() {
        for (s, want) in [
            ("123456789", true),
            ("123-45-6789", true),
            ("12-3456789", true),
            ("12345678", false),
            ("1234-5678", false),
            ("1--23456789", false),
            ("ab 1-2-3-4-5-6-7-8-9", true),
            ("y2026.41/d3/123456789", true),
            ("12345678-", false),
        ] {
            assert_eq!(nine_digits(s), want, "{s}");
        }
    }

    #[test]
    fn overridable_paths() {
        assert!(overridable_path(
            "projection.accounts[a].so_far.items.interest"
        ));
        assert!(overridable_path("estimated[CA].tax_for_year"));
        assert!(!overridable_path("estimated[US].tax_for_year"));
        assert!(overridable_path("estimated[US].last_year_tax"));
        assert!(!overridable_path("federal.form_1040.tax"));
    }
}
