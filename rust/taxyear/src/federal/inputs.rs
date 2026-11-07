//! The rules engine's inputs for a scenario, ported from Go's
//! `taxyear/federal/inputs.go`.

use std::collections::HashMap;

use buffa::{DefaultInstance, EnumValue, Inline, MessageField, ProtoBox};
use filing::Status;
use money::USCents;
use proto::commonv1::{self, Party, StateTaxWithholding};
use proto::factsv1::{
    self, __buffa::oneof::election::Choice, __buffa::oneof::payment_purpose::Purpose,
    __buffa::oneof::position::Answer, __buffa::oneof::prior_year_return::Filed, CarryoverKind,
    DeductionMethod, HsaCoverage, Relationship as FactsRelationship, Reply,
};
use proto::federalv1::{
    FilingStatus, Form1040, Form1098, Form1099B, Form1099DIV, Form1099G, Form1099INT, Form1099MISC,
    Form1099R, FormSSA1099, FormW2, FormW2G, ScheduleA, form_w2::CodedAmount,
    form1099b::HoldingPeriod,
};
use proto::returnsv1::{self, HDHPCoverageType};
use proto::scenariosv1::{
    self, __buffa::oneof::account_part::Part as AccountPartKind,
    __buffa::oneof::account_part::from_forms::form::Form as AccountForm,
    __buffa::oneof::broker_part::Part as BrokerPartKind,
    __buffa::oneof::employer_part::Part as EmployerPartKind, __buffa::oneof::item::Figures,
    __buffa::oneof::itemised_model::Kind as ItemisedKind,
    __buffa::oneof::mortgage_part::Part as MortgagePartKind,
    __buffa::oneof::other_income_part::Part as OtherIncomePartKind,
    __buffa::oneof::other_income_part::from_forms::form::Form as OtherForm, Form8949Box, GainTerm,
    ItemStatus, OtherIncomeFigures, OtherIncomeKind, OtherIncomeProjection, SaleLot,
    ScenarioInputs, TaxPaymentFigures, YearProjection,
    other_income_part::from_forms::Form as OtherIncomeForm,
};

use super::prior_standard_deduction;
use crate::internal::civil;
use crate::model::{self, Context, MoneyRef, Notes, retirement};

/// The message, or its default instance for none, as Go's getters read a
/// nil message.
fn or_default<T: DefaultInstance>(o: Option<&T>) -> &T {
    o.unwrap_or_else(|| T::default_instance())
}

/// money2 copies an amount, unset as zero dollars.
pub(super) fn money2<M: MoneyRef + ?Sized, P: ProtoBox<commonv1::Money>>(
    m: &M,
) -> MessageField<commonv1::Money, P> {
    MessageField::some(model::money(model::value(m)))
}

/// An amount as a set Money field.
pub(super) fn money_of<P: ProtoBox<commonv1::Money>>(
    c: USCents,
) -> MessageField<commonv1::Money, P> {
    MessageField::some(model::money(c))
}

fn party<P: ProtoBox<Party>>(name: &str) -> MessageField<Party, P> {
    MessageField::some(Party {
        name: name.to_string(),
        ..Default::default()
    })
}

/// Inputs is the rules engine's inputs for a scenario: the household and
/// elections from the facts, and one document per model carrying the
/// model's full-year figures from the projection. A form a model holds as
/// a known part is copied and its projected boxes set over it; a 1099-B's
/// lines are copied as they are. The notes say what the inputs leave out.
pub fn inputs(
    ctx: &Context,
    proj: &YearProjection,
    in_: &ScenarioInputs,
) -> (returnsv1::ReturnInputs, Notes) {
    let facts: &factsv1::TaxYearFacts = &in_.facts;
    let (status, _) = filing_status(&in_.facts);
    let mut a = Adapter {
        ctx,
        proj,
        in_,
        facts,
        status,
        notes: Notes::default(),
        fed: returnsv1::Federal1040Inputs {
            documents: MessageField::some(returnsv1::Documents::default()),
            deductions: MessageField::some(returnsv1::Deductions {
                state_and_local_taxes: MessageField::some(returnsv1::StateLocalTaxes::default()),
                ..Default::default()
            }),
            payments: MessageField::some(returnsv1::Payments::default()),
            elections: MessageField::some(returnsv1::Elections {
                filing_status: election_of(status).into(),
                ..Default::default()
            }),
            ..Default::default()
        },
        refunds: Vec::new(),
    };
    a.household();
    a.elections();
    a.wages();
    a.accounts();
    a.brokers();
    a.businesses();
    a.retirement();
    a.other_income();
    a.itemised();
    a.payments();
    a.prior_year();
    a.state_refunds();
    a.left_out();
    (
        returnsv1::ReturnInputs {
            schema_version: 1,
            tax_year: ctx.year() as i32,
            federal: MessageField::some(a.fed),
            ..Default::default()
        },
        a.notes,
    )
}

/// adapter holds one scenario's inputs as they are built.
struct Adapter<'a> {
    ctx: &'a Context,
    proj: &'a YearProjection,
    in_: &'a ScenarioInputs,
    facts: &'a factsv1::TaxYearFacts,
    status: Status,
    fed: returnsv1::Federal1040Inputs,
    /// notes holds what the inputs leave out.
    notes: Notes,
    /// refunds are last year's state refunds, in the year's 1099-Gs.
    refunds: Vec<Refund>,
}

/// refund is a state refund's 1099-G, by its index in the government
/// payments, and its model's word on whether the year that deducted the
/// tax itemised, None when unsaid.
struct Refund {
    doc: usize,
    itemised: Option<bool>,
}

pub(super) fn election_of(s: Status) -> returnsv1::FilingStatusElection {
    use returnsv1::FilingStatusElection as E;
    match s {
        Status::MarriedFilingJointly => E::FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
        Status::MarriedFilingSeparately => E::FILING_STATUS_ELECTION_MARRIED_FILING_SEPARATELY,
        Status::HeadOfHousehold => E::FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD,
        Status::SurvivingSpouse => E::FILING_STATUS_ELECTION_QUALIFYING_SURVIVING_SPOUSE,
        _ => E::FILING_STATUS_ELECTION_SINGLE,
    }
}

pub(super) fn marital_of(s: Status) -> returnsv1::MaritalStatus {
    use returnsv1::MaritalStatus as M;
    match s {
        Status::MarriedFilingJointly | Status::MarriedFilingSeparately => M::MARITAL_STATUS_MARRIED,
        Status::SurvivingSpouse => M::MARITAL_STATUS_WIDOWED,
        _ => M::MARITAL_STATUS_NEVER_MARRIED,
    }
}

/// spouseID is the spouse's id on a joint return whose facts don't hold
/// them yet.
pub(super) const SPOUSE_ID: &str = "spouse";

/// youID is the filer's id when the facts don't hold them.
pub(super) const YOU_ID: &str = "you";

fn person(p: &factsv1::Person) -> returnsv1::Person {
    let mut out = returnsv1::Person {
        id: p.id.clone(),
        first_name: p.name.clone(),
        blind: Some(p.blind),
        ..Default::default()
    };
    if p.date_of_birth.year != 0 {
        out.date_of_birth = p.date_of_birth.clone();
    }
    if p.permanently_disabled {
        out.permanently_disabled = Some(true);
    }
    if p.full_time_student {
        out.full_time_student = Some(true);
    }
    out
}

fn relationship(r: EnumValue<FactsRelationship>) -> returnsv1::Relationship {
    use returnsv1::Relationship as R;
    match r.as_known() {
        Some(FactsRelationship::RELATIONSHIP_CHILD) => R::RELATIONSHIP_CHILD,
        Some(FactsRelationship::RELATIONSHIP_PARENT) => R::RELATIONSHIP_PARENT,
        Some(FactsRelationship::RELATIONSHIP_SIBLING) => R::RELATIONSHIP_SIBLING,
        Some(FactsRelationship::RELATIONSHIP_OTHER_RELATIVE) => R::RELATIONSHIP_OTHER,
        Some(FactsRelationship::RELATIONSHIP_NOT_RELATED) => R::RELATIONSHIP_NOT_RELATED,
        _ => R::RELATIONSHIP_UNSPECIFIED,
    }
}

/// reply is the facts' answer to a catalogue question about a person.
fn reply(f: &factsv1::TaxYearFacts, question: &str, person: &str) -> Reply {
    for pos in &f.positions {
        if pos.question != question {
            continue;
        }
        if pos.subjects.iter().any(|s| s == person) {
            return match &pos.answer {
                Some(Answer::Reply(r)) => r.as_known().unwrap_or(Reply::REPLY_UNSPECIFIED),
                _ => Reply::REPLY_UNSPECIFIED,
            };
        }
    }
    Reply::REPLY_UNSPECIFIED
}

/// The sale lot an item holds, or the default one.
fn sale_of(it: &scenariosv1::Item) -> Option<&SaleLot> {
    match &it.figures {
        Some(Figures::Sale(s)) => Some(s),
        _ => None,
    }
}

/// The other income figures an item holds, or the default ones.
fn other_of(it: &scenariosv1::Item) -> &OtherIncomeFigures {
    match &it.figures {
        Some(Figures::OtherIncome(x)) => x,
        _ => OtherIncomeFigures::default_instance(),
    }
}

/// The tax payment figures an item holds, or the default ones.
fn payment_of(it: &scenariosv1::Item) -> &TaxPaymentFigures {
    match &it.figures {
        Some(Figures::Payment(x)) => x,
        _ => TaxPaymentFigures::default_instance(),
    }
}

/// Go's map from id to model: a later model with the same id wins.
fn by_id<T>(models: &[T], id: impl Fn(&T) -> &str) -> HashMap<&str, &T> {
    let mut out = HashMap::new();
    for m in models {
        out.insert(id(m), m);
    }
    out
}

/// codedAmounts is box 12 with the projected codes set: the known W-2's
/// other codes are kept, and a projected code with no amount is left off.
fn coded_amounts(known: &[CodedAmount], projected: &[(&str, &dyn MoneyRef)]) -> Vec<CodedAmount> {
    let mut out: Vec<CodedAmount> = known
        .iter()
        .filter(|c| !projected.iter().any(|(code, _)| *code == c.code))
        .cloned()
        .collect();
    for (code, v) in projected {
        if model::value(*v) != USCents(0) {
            out.push(CodedAmount {
                code: code.to_string(),
                amount: money2(*v),
                ..Default::default()
            });
        }
    }
    out
}

/// The Form 8949 box's letter, or "".
fn box_letter(b: EnumValue<Form8949Box>) -> &'static str {
    match b.as_known() {
        Some(Form8949Box::FORM_8949_BOX_A) => "A",
        Some(Form8949Box::FORM_8949_BOX_B) => "B",
        Some(Form8949Box::FORM_8949_BOX_C) => "C",
        Some(Form8949Box::FORM_8949_BOX_D) => "D",
        Some(Form8949Box::FORM_8949_BOX_E) => "E",
        Some(Form8949Box::FORM_8949_BOX_F) => "F",
        _ => "",
    }
}

/// saleLine is a 1099-B line for a sale lot no 1099-B reports yet.
fn sale_line(m: &scenariosv1::BrokerModel, it: &scenariosv1::Item, s: &SaleLot) -> Form1099B {
    let term = if s.term == GainTerm::GAIN_TERM_LONG {
        HoldingPeriod::HOLDING_PERIOD_LONG_TERM
    } else {
        HoldingPeriod::HOLDING_PERIOD_SHORT_TERM
    };
    let b = s.r#box;
    let reported = b == Form8949Box::FORM_8949_BOX_A || b == Form8949Box::FORM_8949_BOX_D;
    Form1099B {
        payer: party(&m.name),
        form_8949_checkbox: box_letter(b).to_string(),
        description_of_property: s.holding.clone(),
        date_acquired: s.acquired.clone(),
        date_sold_or_disposed: it.date.clone(),
        proceeds: money2(&s.proceeds),
        cost_or_other_basis: money2(&s.basis),
        wash_sale_loss_disallowed: money2(&s.wash_sale_disallowed),
        holding_period: term.into(),
        basis_reported_to_irs: reported,
        noncovered_security: !reported,
        ..Default::default()
    }
}

/// statementLines is each 1099-B line by the id of the sale lot item the
/// broker model makes from it.
fn statement_lines(m: &scenariosv1::BrokerModel) -> HashMap<String, &Form1099B> {
    let mut out = HashMap::new();
    for p in &m.parts {
        let Some(BrokerPartKind::FromForms(ff)) = &p.part else {
            continue;
        };
        for st in &ff.statements {
            if !model::usable(&st.source) {
                continue;
            }
            for (i, t) in st.statement.transactions.iter().enumerate() {
                let sold = civil::from_field(&t.date_sold_or_disposed).unwrap_or_default();
                let id = model::item_id(&[
                    &model::item_id(&["brokers", &m.id]),
                    &sold.to_string(),
                    "sale_lot",
                    &format!("{}-{i}", st.id),
                ]);
                out.insert(id, t);
            }
        }
    }
    out
}

fn coverage(c: EnumValue<HsaCoverage>) -> HDHPCoverageType {
    match c.as_known() {
        Some(HsaCoverage::HSA_COVERAGE_NONE) => HDHPCoverageType::HDHP_COVERAGE_TYPE_NONE,
        Some(HsaCoverage::HSA_COVERAGE_SELF_ONLY) => HDHPCoverageType::HDHP_COVERAGE_TYPE_SELF_ONLY,
        Some(HsaCoverage::HSA_COVERAGE_FAMILY) => HDHPCoverageType::HDHP_COVERAGE_TYPE_FAMILY,
        _ => HDHPCoverageType::HDHP_COVERAGE_TYPE_UNSPECIFIED,
    }
}

/// knownOther is the last usable form an other income model holds, or
/// None.
fn known_other(m: &scenariosv1::OtherIncomeModel) -> Option<&OtherIncomeForm> {
    let mut out = None;
    for p in &m.parts {
        if let Some(OtherIncomePartKind::FromForms(ff)) = &p.part {
            for f in &ff.forms {
                if model::usable(&f.source) {
                    out = Some(f);
                }
            }
        }
    }
    out
}

/// The kind of form a known other income form is, if it is that kind.
macro_rules! known_form {
    ($known:expr, $variant:ident) => {
        match $known.and_then(|k| k.form.as_ref()) {
            Some(OtherForm::$variant(f)) => Some(&**f),
            _ => None,
        }
    };
}

/// A per-person sum of the year's contributions.
#[derive(Default)]
struct Totals {
    traditional: USCents,
    roth: USCents,
    hsa: USCents,
}

/// The federal return in a prior year's return, if it is one.
fn federal_of(p: &factsv1::PriorYearReturn) -> Option<&factsv1::FederalReturn> {
    match &p.filed {
        Some(Filed::Federal(f)) => Some(f),
        _ => None,
    }
}

impl<'a> Adapter<'a> {
    fn married(&self) -> bool {
        self.status == Status::MarriedFilingJointly
            || self.status == Status::MarriedFilingSeparately
    }

    fn documents(&mut self) -> &mut returnsv1::Documents {
        self.fed.documents.get_or_insert_default()
    }

    /// household fills the filer, the spouse on a joint or separate
    /// return, and the people the facts say count as dependents.
    fn household(&mut self) {
        let mut h = returnsv1::Household {
            marital_status: marital_of(self.status).into(),
            residence: self.facts.residence.clone(),
            ..Default::default()
        };
        for p in &self.facts.people {
            if p.relationship == FactsRelationship::RELATIONSHIP_YOU {
                h.taxpayer = MessageField::some(person(p));
            } else if p.relationship == FactsRelationship::RELATIONSHIP_SPOUSE {
                if self.married() {
                    h.spouse = MessageField::some(person(p));
                }
            } else if let Some(d) = self.dependent(p) {
                h.dependents.push(d);
            }
        }
        if h.taxpayer.is_unset() {
            h.taxpayer = MessageField::some(returnsv1::Person {
                id: YOU_ID.to_string(),
                ..Default::default()
            });
        }
        if self.status == Status::MarriedFilingJointly && h.spouse.is_unset() {
            h.spouse = MessageField::some(returnsv1::Person {
                id: SPOUSE_ID.to_string(),
                ..Default::default()
            });
        }
        self.fed.household = MessageField::some(h);
    }

    /// dependent is a person other than the filer and spouse, with the
    /// replies the facts hold to the qualifying relative questions. A yes
    /// to "was their gross income under the limit" is given as no gross
    /// income.
    fn dependent(&self, p: &factsv1::Person) -> Option<returnsv1::Dependent> {
        let mut d = returnsv1::Dependent {
            id: p.id.clone(),
            first_name: p.name.clone(),
            relationship: relationship(p.relationship).into(),
            months_lived_with_you: Some(p.months_lived_with_you),
            full_time_student: Some(p.full_time_student),
            permanently_disabled: Some(p.permanently_disabled),
            ..Default::default()
        };
        if p.date_of_birth.year != 0 {
            d.date_of_birth = p.date_of_birth.clone();
        }
        match reply(self.facts, "dependent.gross_income_under_limit", &p.id) {
            Reply::REPLY_YES => d.gross_income = money_of(USCents(0)),
            Reply::REPLY_NO => return None,
            _ => {}
        }
        match reply(self.facts, "dependent.more_than_half_support", &p.id) {
            Reply::REPLY_YES => d.you_provided_over_half_support = Some(true),
            Reply::REPLY_NO => d.you_provided_over_half_support = Some(false),
            _ => {}
        }
        Some(d)
    }

    fn elections(&mut self) {
        for e in &self.facts.elections {
            let Some(Choice::DeductionMethod(m)) = &e.choice else {
                continue;
            };
            let el = self.fed.elections.get_or_insert_default();
            if *m == DeductionMethod::DEDUCTION_METHOD_STANDARD {
                el.deduction_method =
                    returnsv1::DeductionMethodElection::DEDUCTION_METHOD_ELECTION_STANDARD.into();
            } else if *m == DeductionMethod::DEDUCTION_METHOD_ITEMISED {
                el.deduction_method =
                    returnsv1::DeductionMethodElection::DEDUCTION_METHOD_ELECTION_ITEMIZED.into();
            }
        }
    }

    /// personOf is the person a model names, or the filer.
    fn person_of(&self, id: &str) -> String {
        if !id.is_empty() {
            return id.to_string();
        }
        self.fed.household.taxpayer.id.clone()
    }

    /// wages adds a W-2 per job, with the job's projected year in its
    /// boxes. A job's known W-2 is the base, so its other boxes carry over.
    fn wages(&mut self) {
        let models = &self.in_.model.employers;
        let by = by_id(models, |m| &m.id);
        for e in &self.proj.employers {
            let m = or_default(by.get(e.id.as_str()).copied());
            let mut w = FormW2::default();
            for p in &m.parts {
                if let Some(EmployerPartKind::FromW2(f)) = &p.part
                    && model::usable(&f.source)
                {
                    w = (*f.w2).clone();
                }
            }
            if w.employer.is_unset() && !m.name.is_empty() {
                w.employer = party(&m.name);
            }
            let b = &e.year.boxes;
            w.wages_tips_other_compensation = money2(&b.wages);
            w.federal_tax_withheld = money2(&b.federal_withheld);
            w.social_security_wages = money2(&b.social_security_wages);
            w.social_security_tax_withheld = money2(&b.social_security_withheld);
            w.medicare_wages_and_tips = money2(&b.medicare_wages);
            w.medicare_tax_withheld = money2(&b.medicare_withheld);
            w.coded_amounts = coded_amounts(
                &w.coded_amounts,
                &[
                    ("D", &b.k401),
                    ("E", &b.k403b),
                    ("AA", &b.roth_401k),
                    ("BB", &b.roth_403b),
                    ("W", &b.hsa),
                ],
            );
            let deferred = model::value(&b.k401)
                + model::value(&b.k403b)
                + model::value(&b.roth_401k)
                + model::value(&b.roth_403b);
            w.retirement_plan =
                w.retirement_plan || m.covered_by_plan.unwrap_or(false) || deferred > USCents(0);
            w.state_tax_withholding = b
                .states
                .iter()
                .map(|s| StateTaxWithholding {
                    state: s.state.clone(),
                    state_income: money2(&s.wages),
                    tax_withheld: money2(&s.withheld),
                    ..Default::default()
                })
                .collect();
            let doc = returnsv1::W2Document {
                id: format!("employers/{}", e.id),
                person: self.person_of(&m.person),
                form: MessageField::some(w),
                ..Default::default()
            };
            self.documents().w2s.push(doc);
        }
    }

    /// accounts adds a 1099-INT and a 1099-DIV per account, each where the
    /// account's year has figures of its kind.
    fn accounts(&mut self) {
        let by = by_id(&self.in_.model.accounts, |m| &m.id);
        for acct in &self.proj.accounts {
            let m = or_default(by.get(acct.id.as_str()).copied());
            let mut known_int: Option<&Form1099INT> = None;
            let mut known_div: Option<&Form1099DIV> = None;
            for p in &m.parts {
                let Some(AccountPartKind::FromForms(ff)) = &p.part else {
                    continue;
                };
                for f in &ff.forms {
                    if !model::usable(&f.source) {
                        continue;
                    }
                    match &f.form {
                        Some(AccountForm::Form1099Int(x)) => known_int = Some(x),
                        Some(AccountForm::Form1099Div(x)) => known_div = Some(x),
                        _ => {}
                    }
                }
            }
            let b = &acct.year.boxes;
            let interest = model::value(&b.interest)
                + model::value(&b.treasury_interest)
                + model::value(&b.tax_exempt_interest);
            if interest != USCents(0) || known_int.is_some() {
                let mut f = match known_int {
                    Some(k) => k.clone(),
                    None => Form1099INT {
                        payer: party(&m.name),
                        ..Default::default()
                    },
                };
                f.interest_income = money2(&b.interest);
                f.savings_bond_and_treasury_interest = money2(&b.treasury_interest);
                f.tax_exempt_interest = money2(&b.tax_exempt_interest);
                if b.foreign_tax_paid.is_set() && model::value(&b.ordinary_dividends) == USCents(0)
                {
                    f.foreign_tax_paid = money2(&b.foreign_tax_paid);
                }
                let doc = returnsv1::Form1099INTDocument {
                    id: format!("accounts/{}/1099-int", acct.id),
                    person: self.person_of(""),
                    form: MessageField::some(f),
                    ..Default::default()
                };
                self.documents().interest.push(doc);
            }
            let dividends =
                model::value(&b.ordinary_dividends) + model::value(&b.capital_gain_distributions);
            if dividends != USCents(0) || known_div.is_some() {
                let mut f = match known_div {
                    Some(k) => k.clone(),
                    None => Form1099DIV {
                        payer: party(&m.name),
                        ..Default::default()
                    },
                };
                f.ordinary_dividends = money2(&b.ordinary_dividends);
                f.qualified_dividends = money2(&b.qualified_dividends);
                f.total_capital_gain_distribution = money2(&b.capital_gain_distributions);
                f.section_199a_dividends = money2(&b.section_199a_dividends);
                f.foreign_tax_paid = money2(&b.foreign_tax_paid);
                let doc = returnsv1::Form1099DIVDocument {
                    id: format!("accounts/{}/1099-div", acct.id),
                    person: self.person_of(""),
                    form: MessageField::some(f),
                    ..Default::default()
                };
                self.documents().dividends.push(doc);
            }
        }
    }

    /// brokers adds a statement per broker: each sale lot from a 1099-B is
    /// that line, copied; each other sale lot, traded or planned, is a
    /// 1099-B line made from the lot, in the Form 8949 box the projection
    /// puts it in.
    fn brokers(&mut self) {
        let by = by_id(&self.in_.model.brokers, |m| &m.id);
        for b in &self.proj.brokers {
            let m = or_default(by.get(b.id.as_str()).copied());
            let lines = statement_lines(m);
            let mut st = returnsv1::BrokerStatement {
                id: format!("brokers/{}", b.id),
                person: self.person_of(""),
                ..Default::default()
            };
            for it in &b.items {
                let Some(s) = sale_of(it) else {
                    continue;
                };
                let f = match lines.get(&it.id) {
                    Some(f) => (*f).clone(),
                    None => sale_line(m, it, s),
                };
                st.transactions.push(returnsv1::BrokerTransaction {
                    id: it.id.clone(),
                    form: MessageField::some(f),
                    ..Default::default()
                });
            }
            if !st.transactions.is_empty() {
                self.documents().brokers.push(st);
            }
        }
    }

    /// businesses adds a Schedule C per business from its projected lines.
    /// The expenses are one total, as the projection keeps them.
    fn businesses(&mut self) {
        let by = by_id(&self.in_.model.businesses, |m| &m.id);
        for b in &self.proj.businesses {
            let m = or_default(by.get(b.id.as_str()).copied());
            let l = &b.year.boxes;
            let biz = returnsv1::Business {
                id: format!("businesses/{}", b.id),
                owner: self.person_of(&m.person),
                name: m.name.clone(),
                materially_participated: Some(true),
                cash_and_unreported_receipts: money2(&l.gross_receipts),
                returns_and_allowances: money2(&l.returns_and_allowances),
                expenses: MessageField::some(returnsv1::BusinessExpenses {
                    other_expenses: vec![returnsv1::OtherExpenseLine {
                        description: "Expenses".to_string(),
                        amount: money2(&l.total_expenses),
                        ..Default::default()
                    }],
                    ..Default::default()
                }),
                ..Default::default()
            };
            self.fed.businesses.push(biz);
        }
    }

    /// retirement adds each person's IRA contributions, and their HSA with
    /// the coverage the facts give and the contributions made directly.
    fn retirement(&mut self) {
        let mut people: HashMap<&str, String> = HashMap::new();
        for m in &self.in_.model.retirement {
            people.insert(&m.id, self.person_of(&m.person));
        }
        let mut by: HashMap<String, Totals> = HashMap::new();
        let mut order: Vec<String> = Vec::new();
        for r in &self.proj.retirement {
            let id = people.get(r.id.as_str()).cloned().unwrap_or_default();
            if !by.contains_key(&id) {
                by.insert(id.clone(), Totals::default());
                order.push(id.clone());
            }
            let t = &r.year.items;
            let tot = by.entry(id).or_default();
            tot.traditional += model::value(&t.traditional_ira);
            tot.roth += model::value(&t.roth_ira);
            tot.hsa += model::value(&t.hsa);
        }
        let mut ret = returnsv1::Retirement::default();
        for id in &order {
            let t = &by[id];
            if t.traditional != USCents(0) || t.roth != USCents(0) {
                ret.persons.push(returnsv1::PersonRetirement {
                    person: id.clone(),
                    traditional_ira_contributions: money_of(t.traditional),
                    roth_ira_contributions: money_of(t.roth),
                    ..Default::default()
                });
            }
        }
        if !ret.persons.is_empty() {
            self.fed.retirement = MessageField::some(ret);
        }
        let mut health = returnsv1::Health::default();
        let mut seen = std::collections::HashSet::new();
        for id in &order {
            self.add_hsa(&mut health, &mut seen, id, by[id].hsa);
        }
        let employers: Vec<String> = self
            .in_
            .model
            .employers
            .iter()
            .map(|m| self.person_of(&m.person))
            .collect();
        for id in &employers {
            self.add_hsa(&mut health, &mut seen, id, USCents(0));
        }
        if !health.hsa_accounts.is_empty() {
            self.fed.health = MessageField::some(health);
        }
    }

    /// Adds a person's HSA, once, with the coverage the facts give and the
    /// contributions made directly.
    fn add_hsa(
        &self,
        health: &mut returnsv1::Health,
        seen: &mut std::collections::HashSet<String>,
        id: &str,
        direct: USCents,
    ) {
        if !seen.insert(id.to_string()) {
            return;
        }
        let p = or_default(self.ctx.person(id));
        let cov = coverage(p.hsa_coverage);
        if cov == HDHPCoverageType::HDHP_COVERAGE_TYPE_UNSPECIFIED
            && direct == USCents(0)
            && self.payroll_hsa(id) == USCents(0)
        {
            return;
        }
        let mut h = returnsv1::PersonHSA {
            person: id.to_string(),
            annual_coverage: cov.into(),
            direct_contributions: money_of(direct),
            ..Default::default()
        };
        let m = p.hsa_months;
        if m > 0 && m < 12 && cov != HDHPCoverageType::HDHP_COVERAGE_TYPE_NONE {
            // The facts give how many months, not which: the last ones, so
            // a yes to the last-month rule question, § 223(b)(8), holds.
            h.annual_coverage = HDHPCoverageType::HDHP_COVERAGE_TYPE_UNSPECIFIED.into();
            if reply(self.facts, retirement::LAST_MONTH_RULE, id) == Reply::REPLY_YES {
                h.elected_last_month_rule = Some(true);
            }
            for i in 1..=12 {
                let c = if i <= 12 - m {
                    HDHPCoverageType::HDHP_COVERAGE_TYPE_NONE
                } else {
                    cov
                };
                h.monthly_coverage.push(returnsv1::HSAMonthlyCoverage {
                    month: i,
                    coverage: c.into(),
                    ..Default::default()
                });
            }
        }
        health.hsa_accounts.push(h);
    }

    /// payrollHsa is a person's box 12 code W across their jobs.
    fn payroll_hsa(&self, id: &str) -> USCents {
        let mut t = USCents(0);
        for w in &self.fed.documents.w2s {
            if w.person != id {
                continue;
            }
            for c in &w.form.coded_amounts {
                if c.code == "W" {
                    t += model::value(&c.amount);
                }
            }
        }
        t
    }

    /// otherIncome adds each other income model's year as the form its
    /// kind arrives on. A model's known form is the base.
    fn other_income(&mut self) {
        let by = by_id(&self.in_.model.other_income, |m| &m.id);
        for o in &self.proj.other_income {
            let m = or_default(by.get(o.id.as_str()).copied());
            let year = &o.year;
            let y: &OtherIncomeFigures = if year.boxes.is_set() {
                &year.boxes
            } else {
                &year.items
            };
            let (gross, withheld) = (money2(&y.gross), money2(&y.federal_withheld));
            let taxable = if y.taxable.is_set() {
                money2(&y.taxable)
            } else {
                gross.clone()
            };
            let id = format!("other_income/{}", o.id);
            let person = self.person_of("");
            let payer = party(&m.payer);
            let known = known_other(m);
            match m.kind.as_known() {
                Some(OtherIncomeKind::OTHER_INCOME_KIND_RETIREMENT_WITHDRAWALS) => {
                    let forms = self.distributions(o, &payer, known_form!(known, Form1099R));
                    for (i, f) in forms.into_iter().enumerate() {
                        self.documents().retirement_distributions.push(
                            returnsv1::Form1099RDocument {
                                id: format!("{id}/{i}"),
                                person: person.clone(),
                                form: MessageField::some(f),
                                ..Default::default()
                            },
                        );
                    }
                }
                Some(OtherIncomeKind::OTHER_INCOME_KIND_SOCIAL_SECURITY) => {
                    let mut f = match known_form!(known, FormSsa1099) {
                        Some(k) => k.clone(),
                        None => FormSSA1099::default(),
                    };
                    f.net_benefits = gross.clone();
                    f.benefits_paid = gross;
                    f.federal_tax_withheld = withheld;
                    self.documents()
                        .social_security
                        .push(returnsv1::FormSSA1099Document {
                            id,
                            person,
                            form: MessageField::some(f),
                            ..Default::default()
                        });
                }
                Some(OtherIncomeKind::OTHER_INCOME_KIND_UNEMPLOYMENT) => {
                    let mut f = match known_form!(known, Form1099G) {
                        Some(k) => k.clone(),
                        None => Form1099G {
                            payer,
                            ..Default::default()
                        },
                    };
                    f.unemployment_compensation = gross;
                    f.federal_tax_withheld = withheld;
                    self.documents()
                        .government_payments
                        .push(returnsv1::Form1099GDocument {
                            id,
                            person,
                            form: MessageField::some(f),
                            ..Default::default()
                        });
                }
                Some(OtherIncomeKind::OTHER_INCOME_KIND_STATE_REFUND) => {
                    let mut f = match known_form!(known, Form1099G) {
                        Some(k) => k.clone(),
                        None => Form1099G {
                            payer,
                            ..Default::default()
                        },
                    };
                    f.state_or_local_refund = gross;
                    f.federal_tax_withheld = withheld;
                    if m.refund_year_itemised == Some(false) {
                        // Its own model says the year that deducted the tax
                        // didn't itemise, so none of it is income; its
                        // withholding counts.
                        f.state_or_local_refund = money_of(USCents(0));
                    }
                    let docs = self.documents();
                    docs.government_payments.push(returnsv1::Form1099GDocument {
                        id,
                        person,
                        form: MessageField::some(f),
                        ..Default::default()
                    });
                    let doc = docs.government_payments.len() - 1;
                    self.refunds.push(Refund {
                        doc,
                        itemised: m.refund_year_itemised,
                    });
                }
                Some(OtherIncomeKind::OTHER_INCOME_KIND_GAMBLING) => {
                    let mut f = match known_form!(known, FormW2g) {
                        Some(k) => k.clone(),
                        None => FormW2G {
                            payer,
                            ..Default::default()
                        },
                    };
                    f.reportable_winnings = gross;
                    f.federal_tax_withheld = withheld;
                    self.documents().w2gs.push(returnsv1::W2GDocument {
                        id,
                        person,
                        form: MessageField::some(f),
                        ..Default::default()
                    });
                }
                Some(OtherIncomeKind::OTHER_INCOME_KIND_RENTAL) => {
                    self.fed.rentals.push(returnsv1::RentalProperty {
                        id,
                        owner: person,
                        description: m.property.clone(),
                        rents_received: gross,
                        other_expenses: money2(&y.expenses),
                        ..Default::default()
                    });
                    if model::value(&withheld) != USCents(0) {
                        let p = self.fed.payments.get_or_insert_default();
                        p.other_federal_withholding = money_of(
                            model::value(&p.other_federal_withholding) + model::value(&withheld),
                        );
                    }
                }
                Some(OtherIncomeKind::OTHER_INCOME_KIND_ALIMONY) => {
                    self.notes.leave_out("Alimony received: the rules engine has no input for it yet, so it is left out of Schedule 1 line 2a");
                }
                Some(OtherIncomeKind::OTHER_INCOME_KIND_HOME_SALE) => {
                    self.notes.leave_out("The gain on a home sale over the § 121 exclusion: the projection doesn't hold the sale's dates and basis the rules need");
                }
                _ => {
                    let mut f = match known_form!(known, Form1099Misc) {
                        Some(k) => k.clone(),
                        None => Form1099MISC {
                            payer,
                            ..Default::default()
                        },
                    };
                    f.other_income = taxable;
                    f.federal_tax_withheld = withheld;
                    self.documents()
                        .miscellaneous
                        .push(returnsv1::Form1099MISCDocument {
                            id,
                            person,
                            form: MessageField::some(f),
                            ..Default::default()
                        });
                }
            }
        }
    }

    /// distributions is a model's year as one 1099-R per box 7 code and IRA
    /// checkbox, as a payer issues them. A distribution without a code, or
    /// with a negative taxable amount, can't be taxed: it is left out, and
    /// only its withholding counts. The model's known 1099-R is the base.
    fn distributions(
        &mut self,
        o: &OtherIncomeProjection,
        payer: &MessageField<Party, Inline<Party>>,
        known: Option<&Form1099R>,
    ) -> Vec<Form1099R> {
        let mut by_key: HashMap<String, usize> = HashMap::new();
        let mut out: Vec<Form1099R> = Vec::new();
        let mut withheld_only: Option<Form1099R> = None;
        for it in &o.items {
            let x = other_of(it);
            let codes: Vec<String> = x
                .distribution_codes
                .iter()
                .map(|c| c.trim())
                .filter(|c| !c.is_empty())
                .map(str::to_string)
                .collect();
            if codes.is_empty() || x.taxable.is_set() && model::value(&x.taxable) < USCents(0) {
                self.notes.leave_out("A Form 1099-R distribution without a box 7 code, or with a negative taxable amount, which can't be taxed; it is left out of lines 4 and 5");
                let w = model::value(&x.federal_withheld);
                if w != USCents(0) {
                    let f = withheld_only.get_or_insert_with(|| Form1099R {
                        payer: payer.clone(),
                        gross_distribution: money_of(USCents(0)),
                        taxable_amount: money_of(USCents(0)),
                        federal_tax_withheld: money_of(USCents(0)),
                        ..Default::default()
                    });
                    f.federal_tax_withheld = money_of(model::value(&f.federal_tax_withheld) + w);
                }
                continue;
            }
            // Go's fmt.Sprint(codes, ira), which only keys the map.
            let key = format!("[{}] {}", codes.join(" "), x.ira);
            let i = match by_key.get(&key) {
                Some(&i) => i,
                None => {
                    let mut f = match known {
                        Some(k) => k.clone(),
                        None => Form1099R {
                            payer: payer.clone(),
                            ..Default::default()
                        },
                    };
                    f.gross_distribution = money_of(USCents(0));
                    f.taxable_amount = money_of(USCents(0));
                    f.federal_tax_withheld = money_of(USCents(0));
                    f.distribution_codes = codes;
                    f.ira_sep_simple = x.ira;
                    out.push(f);
                    by_key.insert(key, out.len() - 1);
                    out.len() - 1
                }
            };
            let f = &mut out[i];
            let gross = model::value(&x.gross);
            let taxable = if x.taxable.is_set() {
                model::value(&x.taxable)
            } else {
                gross
            };
            f.gross_distribution = money_of(model::value(&f.gross_distribution) + gross);
            f.taxable_amount = money_of(model::value(&f.taxable_amount) + taxable);
            f.federal_tax_withheld =
                money_of(model::value(&f.federal_tax_withheld) + model::value(&x.federal_withheld));
        }
        if let Some(w) = withheld_only {
            out.push(w);
        }
        out
    }

    fn prior(&mut self) -> &mut returnsv1::PriorYear {
        self.fed.prior_year.get_or_insert_default()
    }

    /// itemised adds Schedule A's inputs: medical expenses, real estate
    /// tax, a Form 1098 per mortgage and a cash gift per charity model.
    fn itemised(&mut self) {
        let by = by_id(&self.in_.model.itemised, |m| &m.id);
        let (mut medical, mut real_estate) = (USCents(0), USCents(0));
        for it in &self.proj.itemised {
            let m = or_default(by.get(it.id.as_str()).copied());
            let b = &it.year.boxes;
            medical += model::value(&b.medical);
            real_estate += model::value(&b.real_estate_taxes);
            let mortgage = match &m.kind {
                Some(ItemisedKind::Mortgage(x)) => Some(&**x),
                _ => None,
            };
            let v = model::value(&b.mortgage_interest);
            if v != USCents(0) || mortgage.is_some() {
                let mut f = Form1098 {
                    lender: party(&m.name),
                    ..Default::default()
                };
                for p in mortgage.map(|x| x.parts.as_slice()).unwrap_or_default() {
                    if let Some(MortgagePartKind::FromForm1098(k)) = &p.part
                        && model::usable(&k.source)
                    {
                        f = (*k.form).clone();
                    }
                }
                f.mortgage_interest_received = money_of(v);
                let mut doc = returnsv1::Form1098Document {
                    id: format!("itemised/{}", it.id),
                    person: self.person_of(""),
                    form: MessageField::some(f),
                    r#use: returnsv1::MortgageUse::MORTGAGE_USE_HOME_ACQUISITION.into(),
                    ..Default::default()
                };
                if let Some(bal) = mortgage.and_then(|x| x.loan.balance.as_option()) {
                    doc.average_balance = money2(bal);
                }
                self.documents().mortgage_interest.push(doc);
            }
            let v = model::value(&b.gifts_by_cash);
            if v != USCents(0) {
                let gift = returnsv1::CharitableGift {
                    donee_name: m.name.clone(),
                    limit_category: returnsv1::CharitableLimitCategory::CHARITABLE_LIMIT_CATEGORY_CASH_PUBLIC_CHARITY_60_PCT.into(),
                    cash_amount: money_of(v),
                    has_written_acknowledgment: Some(true),
                    ..Default::default()
                };
                self.fed
                    .deductions
                    .get_or_insert_default()
                    .charitable_gifts
                    .push(gift);
            }
        }
        let d = self.fed.deductions.get_or_insert_default();
        if medical != USCents(0) {
            d.medical = MessageField::some(returnsv1::MedicalExpenses {
                unreimbursed_medical_and_dental: money_of(medical),
                ..Default::default()
            });
        }
        d.state_and_local_taxes
            .get_or_insert_default()
            .real_estate_taxes = money_of(real_estate);
    }

    /// payments adds the year's tax payments made: federal estimated
    /// payments and last year's overpayment applied, and state and local
    /// payments dated in the year, which Schedule A line 5a deducts. A
    /// state payment toward an earlier year's balance is that line's
    /// balance for prior years.
    fn payments(&mut self) {
        let year = self.ctx.year();
        let (mut applied, mut balance) = (USCents(0), USCents(0));
        let mut estimated = Vec::new();
        for t in &self.proj.tax_paid {
            let us = t.jurisdiction == "US";
            for it in &t.items {
                let x = payment_of(it);
                let d = civil::from_field(&it.date).unwrap_or_default();
                let amount = model::value(&x.amount);
                let balance_for = match &x.purpose.purpose {
                    Some(Purpose::BalanceFor(y)) => i64::from(*y),
                    _ => 0,
                };
                let not_tax = matches!(&x.purpose.purpose, Some(Purpose::NotTax(_)));
                let payment = |jurisdiction: &str| returnsv1::EstimatedPayment {
                    jurisdiction: jurisdiction.to_string(),
                    date_paid: it.date.clone(),
                    designated_tax_year: year as i32,
                    amount: money_of(amount),
                    ..Default::default()
                };
                if us && x.counts && x.from_last_year {
                    applied += amount;
                } else if us && x.counts && it.status == ItemStatus::ITEM_STATUS_MADE {
                    estimated.push(payment("US"));
                } else if us || d.year() != year || not_tax || x.from_last_year {
                } else if balance_for != 0 && balance_for < year {
                    balance += amount;
                } else {
                    estimated.push(payment(&t.jurisdiction));
                }
            }
        }
        let p = self.fed.payments.get_or_insert_default();
        p.estimated_payments.extend(estimated);
        p.prior_year_federal_overpayment_applied = money_of(applied);
        self.fed
            .deductions
            .get_or_insert_default()
            .state_and_local_taxes
            .get_or_insert_default()
            .balance_paid_for_prior_years = money_of(balance);
    }

    /// priorYear adds last year's federal return and its carryovers: the
    /// facts' carryovers, else what last year's return says it carries
    /// forward.
    fn prior_year(&mut self) {
        self.prior();
        let prior = self.prior_federal();
        if let Some(prior) = prior {
            let f = federal_of(prior);
            let py = self.prior();
            if let Some(f) = f {
                py.prior_federal_return = MessageField::some(f.clone());
            }
            let f = or_default(f);
            let mut agi = prior.adjusted_gross_income.clone();
            let mut tax = prior.total_tax.clone();
            if agi.is_unset() {
                agi = f.form_1040.adjusted_gross_income.clone();
            }
            if tax.is_unset() {
                tax = f.form_1040.total_tax.clone();
            }
            py.prior_year_agi = agi;
            py.prior_year_total_tax = tax;
            if let Some(r) = prior.full_year_resident {
                py.prior_year_full_year_citizen_or_resident = Some(r);
            }
        }
        let short = self.carryover(CarryoverKind::CARRYOVER_KIND_SHORT_TERM_CAPITAL_LOSS);
        let long = self.carryover(CarryoverKind::CARRYOVER_KIND_LONG_TERM_CAPITAL_LOSS);
        if short != USCents(0) || long != USCents(0) {
            self.prior().capital_loss_carryover =
                MessageField::some(returnsv1::CapitalLossCarryover {
                    short_term_carryover: money_of(short),
                    long_term_carryover: money_of(long),
                    ..Default::default()
                });
        }
        let c = self.carryover(CarryoverKind::CARRYOVER_KIND_CHARITABLE_CONTRIBUTION);
        if c != USCents(0) {
            let origin = (self.ctx.year() - 1) as i32;
            self.prior()
                .charitable_carryovers
                .push(returnsv1::CharitableCarryover {
                    origin_tax_year: origin,
                    cash_60_pct_amount: money_of(c),
                    ..Default::default()
                });
        }
    }

    /// The amount of the refund at an index.
    fn refund_amount(&self, r: &Refund) -> USCents {
        model::value(
            &self.fed.documents.government_payments[r.doc]
                .form
                .state_or_local_refund,
        )
    }

    /// stateRefunds gives the State and Local Income Tax Refund Worksheet
    /// what it reads of last year: a refund is income only as far as
    /// deducting the tax lowered last year's tax. With last year's Form
    /// 1040 and Schedule A in the facts, it gets their figures. Without
    /// them, a refund from a year that itemised counts in full and the
    /// result asks for them; with no word on itemising, the refunds whose
    /// own model doesn't say count in full and the result asks. Counting in
    /// full is given to the worksheet as a year whose state income tax all
    /// lowered its tax.
    fn state_refunds(&mut self) {
        let (mut undecided, mut itemised) = (USCents(0), USCents(0));
        for r in &self.refunds {
            match r.itemised {
                None => undecided += self.refund_amount(r),
                Some(true) => itemised += self.refund_amount(r),
                Some(false) => {}
            }
        }
        if undecided + itemised == USCents(0) {
            return;
        }
        let prior = self.prior_federal();
        let fed = prior.and_then(federal_of);
        let f = fed.and_then(|x| x.form_1040.as_option());
        let sa = fed.and_then(|x| x.schedule_a.as_option());
        let said = prior.is_some_and(|p| p.itemised.is_some());
        let prior_itemised = prior.and_then(|p| p.itemised).unwrap_or(false);
        if let (Some(f), Some(sa)) = (f, sa)
            && f.itemized
        {
            if let Some(s) = self.prior_schedule(f, sa) {
                self.prior().itemized_summary = MessageField::some(s);
                return;
            }
        } else if f.is_some_and(|f| !f.itemized) || said && !prior_itemised {
            self.zero_undecided();
            if itemised == USCents(0) {
                self.prior().itemized_summary =
                    MessageField::some(returnsv1::PriorYearItemizedSummary {
                        itemized_in_prior_year: Some(false),
                        ..Default::default()
                    });
                return;
            }
        } else if f.is_none() && !said && undecided > USCents(0) {
            self.notes.ask(model::new_question(
                "federal/state-refund",
                "federal.schedule_1.taxable_state_local_refunds",
                "Did you itemise deductions on last year's federal return?",
                "Your state refund counts as income until you say.",
                "facts.prior_year",
                &[],
            ));
            if itemised == USCents(0) {
                self.count_in_full();
                return;
            }
        }
        self.notes.leave_out(format!(
            "How much of a state refund is taxable: without your {} return, all of it is counted",
            self.ctx.year() - 1
        ));
        self.notes.ask(model::new_question(
            "federal/state-refund-schedule-a",
            "federal.schedule_1.taxable_state_local_refunds",
            "What were the figures on last year's Schedule A?",
            "All of your state refund counts as income until you give them.",
            "facts.prior_year",
            &[],
        ));
        self.count_in_full();
    }

    /// zeroUndecided takes the refunds whose model doesn't say out of
    /// income: last year didn't itemise.
    fn zero_undecided(&mut self) {
        let docs: Vec<usize> = self
            .refunds
            .iter()
            .filter(|r| r.itemised.is_none())
            .map(|r| r.doc)
            .collect();
        for i in docs {
            self.documents().government_payments[i]
                .form
                .get_or_insert_default()
                .state_or_local_refund = money_of(USCents(0));
        }
    }

    /// countInFull gives the worksheet a last year whose state income tax,
    /// at least the refunds, all lowered its tax.
    fn count_in_full(&mut self) {
        let total: USCents = self.refunds.iter().map(|r| self.refund_amount(r)).sum();
        let all = money_of(total);
        self.prior().itemized_summary = MessageField::some(returnsv1::PriorYearItemizedSummary {
            itemized_in_prior_year: Some(true),
            prior_schedule_a_line_5a_income_tax: all.clone(),
            prior_schedule_a_line_5d_total_salt: all.clone(),
            prior_schedule_a_line_5e_capped_salt: all.clone(),
            prior_total_itemized_deductions: all,
            prior_standard_deduction: money_of(USCents(0)),
            ..Default::default()
        });
    }

    /// priorSchedule is the worksheet's figures from last year's Form 1040
    /// and Schedule A, with last year's standard deduction from the rules
    /// engine. It is None when the engine has no rules for last year.
    fn prior_schedule(
        &self,
        f: &Form1040,
        sa: &ScheduleA,
    ) -> Option<returnsv1::PriorYearItemizedSummary> {
        let std = prior_standard_deduction(self.ctx, f)?;
        let total = if sa.total_itemized_deductions.is_set() {
            sa.total_itemized_deductions.clone()
        } else {
            f.deduction.clone()
        };
        Some(returnsv1::PriorYearItemizedSummary {
            itemized_in_prior_year: Some(true),
            deducted_sales_tax_instead_of_income_tax: Some(sa.elected_general_sales_taxes),
            prior_schedule_a_line_5a_income_tax: sa.state_local_income_or_sales_taxes.clone(),
            prior_schedule_a_line_5d_total_salt: sa.total_state_and_local_taxes.clone(),
            prior_schedule_a_line_5e_capped_salt: money_of(
                model::value(&sa.state_and_local_taxes_after_cap)
                    .min(model::value(&sa.total_state_and_local_taxes)),
            ),
            prior_total_itemized_deductions: total,
            prior_standard_deduction: std,
            prior_taxable_income: f.taxable_income.clone(),
            ..Default::default()
        })
    }

    /// priorFederal is last year's federal return in the facts, the
    /// household's own before a separate one's, or None.
    fn prior_federal(&self) -> Option<&'a factsv1::PriorYearReturn> {
        let mut out: Option<&'a factsv1::PriorYearReturn> = None;
        for p in &self.facts.prior_year {
            if p.jurisdiction == "US"
                && (out.is_none()
                    || p.people.is_empty() && out.is_some_and(|o| !o.people.is_empty()))
            {
                out = Some(p);
            }
        }
        out
    }

    /// carryover is what last year carried into this one of a kind.
    fn carryover(&self, kind: CarryoverKind) -> USCents {
        let mut t = USCents(0);
        let mut found = false;
        for co in &self.facts.carryovers {
            if co.kind == kind {
                t += model::value(&co.amount);
                found = true;
            }
        }
        if found {
            return t;
        }
        for p in &self.facts.prior_year {
            if p.jurisdiction != "US" || !p.people.is_empty() {
                continue;
            }
            for cf in &p.carries_forward {
                if cf.kind == kind {
                    t += model::value(&cf.amount);
                }
            }
        }
        t
    }

    /// leftOut notes what the engine could work out but the models hold no
    /// figures for.
    fn left_out(&mut self) {
        self.notes.leave_out(
            "Education credits, and other credits that need forms the models don't hold yet, such as Form 1098-T",
        );
        let y = self.ctx.year();
        if (2025..=2028).contains(&y) {
            self.notes.leave_out(
                "The deductions for tips, overtime and car loan interest, whose figures the models don't hold yet",
            );
        }
    }
}

/// FilingStatus returns the household's elected filing status, or single
/// and true when none is elected.
pub fn filing_status(facts: &factsv1::TaxYearFacts) -> (Status, bool) {
    for e in &facts.elections {
        if let Some(Choice::FilingStatus(s)) = &e.choice
            && s.to_i32() != FilingStatus::FILING_STATUS_UNSPECIFIED as i32
        {
            return (status_of(*s), false);
        }
    }
    (Status::Single, true)
}

pub(super) fn status_of(s: EnumValue<FilingStatus>) -> Status {
    match s.as_known() {
        Some(FilingStatus::FILING_STATUS_MARRIED_FILING_JOINTLY) => Status::MarriedFilingJointly,
        Some(FilingStatus::FILING_STATUS_MARRIED_FILING_SEPARATELY) => {
            Status::MarriedFilingSeparately
        }
        Some(FilingStatus::FILING_STATUS_HEAD_OF_HOUSEHOLD) => Status::HeadOfHousehold,
        Some(FilingStatus::FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE) => Status::SurvivingSpouse,
        _ => Status::Single,
    }
}
