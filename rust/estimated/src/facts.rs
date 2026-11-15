//! Every type in this file has private fields, accessors that return copies,
//! and a builder whose `build` validates. A value cannot change after it has
//! been checked, so a jurisdiction's rules never see a Facts that `build`
//! would have refused.

use std::any::Any;
use std::fmt;
use std::sync::Arc;

use filing::Status as FilingStatus;
use money::{Rate, USCents};

use crate::date::Month;
use crate::{Date, Error, Jurisdiction, invalid};

/// One year's figures for one jurisdiction.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Figures {
    pub(crate) tax: Option<USCents>,
    federal_agi: Option<USCents>,
    state_agi: Option<USCents>,
    gross_income: Option<USCents>,
    taxable_income: Option<USCents>,
    local_tax: Option<USCents>,
    withheld: Option<USCents>,
}

impl Figures {
    /// The year's tax after credits, before withholding and estimated
    /// payments, as this jurisdiction measures it, if the caller gave it.
    /// Federally that is Form 2210 line 4; for California it is FTB 5805
    /// Part II line 1; for Pennsylvania it is PA-40 line 12 less lines 21 to
    /// 23. A jurisdiction whose penalty form starts from the tax before
    /// credits takes the credits in its own extras and says so.
    ///
    /// This year's tax is often not known while the year is under way.
    /// Without it, the rules offer every option that does not need it, such
    /// as last year's tax, and report the question; see
    /// [`Error::NeedsThisYearTax`]. A prior return's figures always carry it.
    pub fn tax(&self) -> Option<USCents> {
        self.tax
    }

    /// Federal adjusted gross income, Form 1040 line 11.
    pub fn federal_agi(&self) -> Option<USCents> {
        self.federal_agi
    }

    /// The state's own adjusted gross income, such as California AGI from
    /// Form 540 line 17.
    pub fn state_agi(&self) -> Option<USCents> {
        self.state_agi
    }

    /// Gross income, which some states test instead of tax.
    pub fn gross_income(&self) -> Option<USCents> {
        self.gross_income
    }

    /// Taxable income as this jurisdiction measures it.
    pub fn taxable_income(&self) -> Option<USCents> {
        self.taxable_income
    }

    /// Local income tax paid with the state's, as in Maryland and Indiana.
    /// Included in the tax by whoever computed it; held separately so a
    /// jurisdiction that tests it apart can.
    pub fn local_tax(&self) -> Option<USCents> {
        self.local_tax
    }

    /// The year's total withholding. For the current year the dated
    /// [`Withheld`] entries on [`Facts`] are what counts; this is for a prior
    /// year, whose withholding some exceptions test.
    pub fn withheld(&self) -> Option<USCents> {
        self.withheld
    }
}

/// Builds [`Figures`].
#[derive(Clone, Debug, Default)]
pub struct FiguresBuilder {
    f: Figures,
}

/// Starts a year's figures.
pub fn new_figures() -> FiguresBuilder {
    FiguresBuilder::default()
}

impl FiguresBuilder {
    /// Sets the tax.
    pub fn tax(&mut self, a: USCents) -> &mut Self {
        self.f.tax = Some(a);
        self
    }

    /// Sets federal adjusted gross income.
    pub fn federal_agi(&mut self, a: USCents) -> &mut Self {
        self.f.federal_agi = Some(a);
        self
    }

    /// Sets the state's adjusted gross income.
    pub fn state_agi(&mut self, a: USCents) -> &mut Self {
        self.f.state_agi = Some(a);
        self
    }

    /// Sets gross income.
    pub fn gross_income(&mut self, a: USCents) -> &mut Self {
        self.f.gross_income = Some(a);
        self
    }

    /// Sets taxable income.
    pub fn taxable_income(&mut self, a: USCents) -> &mut Self {
        self.f.taxable_income = Some(a);
        self
    }

    /// Sets local tax.
    pub fn local_tax(&mut self, a: USCents) -> &mut Self {
        self.f.local_tax = Some(a);
        self
    }

    /// Sets the year's withholding.
    pub fn withheld(&mut self, a: USCents) -> &mut Self {
        self.f.withheld = Some(a);
        self
    }

    /// Refuses a negative tax, withholding, local tax or gross income.
    /// Adjusted gross income can be negative and is not checked. The tax may
    /// be absent; a prior return's figures need it, which
    /// [`PriorReturnBuilder::build`] checks.
    ///
    /// Go checks the four in map order, which is random; this checks them in
    /// the order named, so with two negatives the message may name another.
    pub fn build(&self) -> Result<Figures, Error> {
        let f = self.f;
        for (name, a) in [
            ("tax", f.tax),
            ("withholding", f.withheld),
            ("local tax", f.local_tax),
            ("gross income", f.gross_income),
        ] {
            if let Some(v) = a
                && v.0 < 0
            {
                return Err(invalid!("estimated: {name} of {v} is negative"));
            }
        }
        Ok(f)
    }
}

/// Last year's return for this jurisdiction.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct PriorReturn {
    filed: bool,
    not_required: bool,
    months: i64,
    figures: Option<Figures>,
    full_year_resident: Option<bool>,
}

impl PriorReturn {
    /// Reports whether a return was filed for last year.
    pub fn filed(&self) -> bool {
        self.filed
    }

    /// Reports that no return was filed because none was required. Federally
    /// and in California that counts as having had no liability.
    pub fn not_required_to_file(&self) -> bool {
        self.not_required
    }

    /// How many months last year's return covered. Twelve for a full year;
    /// fewer for a short year, which no prior-year test accepts.
    pub fn months(&self) -> i64 {
        self.months
    }

    /// Last year's figures, if a return was filed.
    pub fn figures(&self) -> Option<Figures> {
        self.figures
    }

    /// Whether the filer was resident all of last year, if the caller said.
    /// Federally this is citizen or resident alien for the whole year, which
    /// the no-liability exception requires; for a state it is residence in
    /// that state.
    pub fn full_year_resident(&self) -> Option<bool> {
        self.full_year_resident
    }
}

/// Builds a [`PriorReturn`].
#[derive(Clone, Debug)]
pub struct PriorReturnBuilder {
    p: PriorReturn,
}

/// Starts last year's return. Months defaults to twelve.
pub fn new_prior_return() -> PriorReturnBuilder {
    PriorReturnBuilder {
        p: PriorReturn {
            months: 12,
            ..PriorReturn::default()
        },
    }
}

impl PriorReturnBuilder {
    /// Records whether a return was filed.
    pub fn filed(&mut self, filed: bool) -> &mut Self {
        self.p.filed = filed;
        self
    }

    /// Records that no return was required.
    pub fn not_required_to_file(&mut self) -> &mut Self {
        self.p.not_required = true;
        self
    }

    /// Records the months the return covered.
    pub fn months(&mut self, m: i64) -> &mut Self {
        self.p.months = m;
        self
    }

    /// Records the return's figures.
    pub fn figures(&mut self, f: Figures) -> &mut Self {
        self.p.figures = Some(f);
        self
    }

    /// Records whether the filer was resident all year.
    pub fn full_year_resident(&mut self, resident: bool) -> &mut Self {
        self.p.full_year_resident = Some(resident);
        self
    }

    /// Requires figures with the tax for a filed return, refuses figures and
    /// "not required" together with each other's opposite, and checks the
    /// months.
    pub fn build(&self) -> Result<PriorReturn, Error> {
        let p = self.p;
        if p.months < 1 || p.months > 12 {
            return Err(invalid!(
                "estimated: a return covers 1 to 12 months, not {}",
                p.months
            ));
        }
        if p.filed && p.figures.is_none() {
            return Err(invalid!(
                "estimated: a filed prior return needs its figures"
            ));
        }
        if p.figures.is_some_and(|f| f.tax.is_none()) {
            return Err(invalid!("estimated: a prior return's figures need its tax"));
        }
        if p.filed && p.not_required {
            return Err(invalid!(
                "estimated: a prior return cannot be both filed and not required"
            ));
        }
        Ok(p)
    }
}

/// Tax withheld on a date.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Withheld {
    pub(crate) on: Date,
    pub(crate) amount: USCents,
}

impl Withheld {
    /// The day it was withheld.
    pub fn on(&self) -> Date {
        self.on
    }

    /// The amount withheld.
    pub fn amount(&self) -> USCents {
        self.amount
    }
}

/// Builds a [`Withheld`].
#[derive(Clone, Debug, Default)]
pub struct WithheldBuilder {
    w: Withheld,
}

/// Starts an amount withheld.
pub fn new_withheld() -> WithheldBuilder {
    WithheldBuilder::default()
}

impl WithheldBuilder {
    /// Sets the day.
    pub fn on(&mut self, d: Date) -> &mut Self {
        self.w.on = d;
        self
    }

    /// Sets the amount.
    pub fn amount(&mut self, a: USCents) -> &mut Self {
        self.w.amount = a;
        self
    }

    /// Requires a date and refuses a negative amount.
    pub fn build(&self) -> Result<Withheld, Error> {
        if self.w.on.is_zero() {
            return Err(invalid!("estimated: withholding needs a date"));
        }
        if self.w.amount.0 < 0 {
            return Err(invalid!(
                "estimated: withholding of {} is negative",
                self.w.amount
            ));
        }
        Ok(self.w)
    }
}

/// An estimated payment, or last year's overpayment applied forward.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Payment {
    pub(crate) on: Date,
    pub(crate) amount: USCents,
    pub(crate) prior_year: bool,
}

impl Payment {
    /// The date paid. For an overpayment applied forward it is ignored: the
    /// rules treat it as paid on the first installment's due date.
    pub fn on(&self) -> Date {
        self.on
    }

    /// The amount paid.
    pub fn amount(&self) -> USCents {
        self.amount
    }

    /// Reports that this is last year's overpayment applied to this year's
    /// estimated tax.
    pub fn from_prior_year(&self) -> bool {
        self.prior_year
    }
}

/// Builds a [`Payment`].
#[derive(Clone, Debug, Default)]
pub struct PaymentBuilder {
    p: Payment,
}

/// Starts a payment.
pub fn new_payment() -> PaymentBuilder {
    PaymentBuilder::default()
}

impl PaymentBuilder {
    /// Sets the day paid.
    pub fn on(&mut self, d: Date) -> &mut Self {
        self.p.on = d;
        self
    }

    /// Sets the amount.
    pub fn amount(&mut self, a: USCents) -> &mut Self {
        self.p.amount = a;
        self
    }

    /// Marks the payment as last year's overpayment applied forward.
    pub fn from_prior_year(&mut self) -> &mut Self {
        self.p.prior_year = true;
        self
    }

    /// Requires a date for anything but an overpayment applied forward, and
    /// refuses a negative amount.
    pub fn build(&self) -> Result<Payment, Error> {
        if self.p.on.is_zero() && !self.p.prior_year {
            return Err(invalid!("estimated: a payment needs a date"));
        }
        if self.p.amount.0 < 0 {
            return Err(invalid!(
                "estimated: a payment of {} is negative",
                self.p.amount
            ));
        }
        Ok(self.p)
    }
}

/// A dated stretch of residence in one jurisdiction.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct ResidencePeriod {
    state: String,
    from: Date,
    to: Date,
}

impl ResidencePeriod {
    /// The state, by postal code.
    pub fn state(&self) -> String {
        self.state.clone()
    }

    /// The first day.
    pub fn from(&self) -> Date {
        self.from
    }

    /// The last day.
    pub fn to(&self) -> Date {
        self.to
    }
}

/// Builds a [`ResidencePeriod`].
#[derive(Clone, Debug, Default)]
pub struct ResidencePeriodBuilder {
    r: ResidencePeriod,
}

/// Starts a period of residence in a state, named by its postal code.
pub fn new_residence_period(state: impl Into<String>) -> ResidencePeriodBuilder {
    ResidencePeriodBuilder {
        r: ResidencePeriod {
            state: state.into(),
            ..ResidencePeriod::default()
        },
    }
}

impl ResidencePeriodBuilder {
    /// Sets the first day.
    pub fn from(&mut self, d: Date) -> &mut Self {
        self.r.from = d;
        self
    }

    /// Sets the last day.
    pub fn to(&mut self, d: Date) -> &mut Self {
        self.r.to = d;
        self
    }

    /// Requires a postal code and both dates, in order.
    pub fn build(&self) -> Result<ResidencePeriod, Error> {
        let r = &self.r;
        if r.state.len() != 2 {
            return Err(invalid!(
                "estimated: {} is not a postal code",
                working::quote(&r.state)
            ));
        }
        if r.from.is_zero() || r.to.is_zero() {
            return Err(invalid!("estimated: a residence period needs both dates"));
        }
        if r.to.before(r.from) {
            return Err(invalid!(
                "estimated: residence from {} ends before it starts, on {}",
                r.from,
                r.to
            ));
        }
        Ok(r.clone())
    }
}

/// One filer on the return, for the waivers that turn on age, retirement or
/// disability.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Person {
    age: i64,
    retired_after_62: bool,
    disabled: bool,
}

impl Person {
    /// The person's age at the end of the tax year.
    pub fn age(&self) -> i64 {
        self.age
    }

    /// Reports retirement after reaching 62 in this year or last.
    pub fn retired_after_62(&self) -> bool {
        self.retired_after_62
    }

    /// Reports becoming disabled in this year or last.
    pub fn disabled(&self) -> bool {
        self.disabled
    }
}

/// Builds a [`Person`].
#[derive(Clone, Debug, Default)]
pub struct PersonBuilder {
    p: Person,
}

/// Starts a person of the given age.
pub fn new_person(age: i64) -> PersonBuilder {
    PersonBuilder {
        p: Person {
            age,
            ..Person::default()
        },
    }
}

impl PersonBuilder {
    /// Records retirement after 62.
    pub fn retired_after_62(&mut self) -> &mut Self {
        self.p.retired_after_62 = true;
        self
    }

    /// Records disability.
    pub fn disabled(&mut self) -> &mut Self {
        self.p.disabled = true;
        self
    }

    /// Refuses an implausible age, and retirement after 62 below 62.
    pub fn build(&self) -> Result<Person, Error> {
        if self.p.age < 0 || self.p.age > 130 {
            return Err(invalid!(
                "estimated: an age of {} is not plausible",
                self.p.age
            ));
        }
        if self.p.retired_after_62 && self.p.age < 62 {
            return Err(invalid!(
                "estimated: a person aged {} cannot have retired after 62",
                self.p.age
            ));
        }
        Ok(self.p)
    }
}

/// The methods the filer chooses.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Elections {
    pub(crate) annualised: bool,
    withholding_as_withheld: bool,
}

impl Elections {
    /// Reports the method that measures each installment by the income
    /// earned up to a period end rather than by an even share of the year:
    /// Schedule AI federally, FTB 5805 Part III in California, REV-1630
    /// Exception 2 in Pennsylvania, which uses the income to date without
    /// annualising it. It needs [`Facts::periods`].
    pub fn annualised(&self) -> bool {
        self.annualised
    }

    /// Reports crediting withholding on the dates it was withheld, rather
    /// than spread across the installments as the rules otherwise deem it.
    pub fn withholding_as_withheld(&self) -> bool {
        self.withholding_as_withheld
    }
}

/// Builds [`Elections`].
#[derive(Clone, Debug, Default)]
pub struct ElectionsBuilder {
    e: Elections,
}

/// Starts the elections.
pub fn new_elections() -> ElectionsBuilder {
    ElectionsBuilder::default()
}

impl ElectionsBuilder {
    /// Elects the annualised method.
    pub fn annualised(&mut self) -> &mut Self {
        self.e.annualised = true;
        self
    }

    /// Elects withholding on its dates.
    pub fn withholding_as_withheld(&mut self) -> &mut Self {
        self.e.withholding_as_withheld = true;
        self
    }

    /// Returns the elections; every combination is valid.
    pub fn build(&self) -> Result<Elections, Error> {
        Ok(self.e)
    }
}

/// A ground for waiving the estimated tax penalty.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum WaiverKind {
    /// An answer: the filer asks for no waiver.
    #[default]
    None,

    /// A casualty, disaster or other unusual circumstance that would make
    /// the penalty inequitable. 26 U.S.C. § 6654(e)(3)(A).
    Casualty,

    /// Retirement after 62 or disability, in this year or last, with
    /// reasonable cause. 26 U.S.C. § 6654(e)(3)(B).
    RetiredOrDisabled,
}

/// Names the ground in plain words.
impl fmt::Display for WaiverKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            WaiverKind::Casualty => "casualty, disaster or unusual circumstance",
            WaiverKind::RetiredOrDisabled => {
                "retirement after 62 or disability, with reasonable cause"
            }
            WaiverKind::None => "no waiver",
        })
    }
}

/// What a waiver request rests on, as the person gives it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum WaiverCause {
    /// Not said.
    #[default]
    Unspecified,
    /// A casualty.
    Casualty,
    /// A disaster.
    Disaster,
    /// Retirement.
    Retirement,
    /// Disability.
    Disability,
}

/// Names the cause in plain words, as "a casualty".
impl fmt::Display for WaiverCause {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            WaiverCause::Casualty => "a casualty",
            WaiverCause::Disaster => "a disaster",
            WaiverCause::Retirement => "retirement",
            WaiverCause::Disability => "disability",
            WaiverCause::Unspecified => "unspecified cause",
        })
    }
}

impl WaiverCause {
    /// The waiver a cause asks for.
    fn kind(self) -> WaiverKind {
        match self {
            WaiverCause::Casualty | WaiverCause::Disaster => WaiverKind::Casualty,
            WaiverCause::Retirement | WaiverCause::Disability => WaiverKind::RetiredOrDisabled,
            WaiverCause::Unspecified => WaiverKind::None,
        }
    }
}

/// Judgement calls the filer has answered.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Positions {
    waiver: Option<WaiverKind>,
    waiver_on: Date,
    cause: WaiverCause,
    born: Date,
}

impl Positions {
    /// The waiver the filer asks for, if they answered at all.
    pub fn waiver(&self) -> Option<WaiverKind> {
        self.waiver
    }

    /// The day of what the waiver rests on: the casualty or disaster, the
    /// retirement, or the disability. The zero Date when not given.
    pub fn waiver_on(&self) -> Date {
        self.waiver_on
    }

    /// What the waiver rests on, when the person said.
    pub fn waiver_cause(&self) -> WaiverCause {
        self.cause
    }

    /// The date of birth of the person a retirement waiver is for, which
    /// decides whether they had reached the age the rules name. The zero
    /// Date when not given.
    pub fn born(&self) -> Date {
        self.born
    }
}

/// Builds [`Positions`].
#[derive(Clone, Debug, Default)]
pub struct PositionsBuilder {
    p: Positions,
}

/// Starts the positions.
pub fn new_positions() -> PositionsBuilder {
    PositionsBuilder::default()
}

impl PositionsBuilder {
    /// Answers the waiver question. [`WaiverKind::None`] is an answer.
    pub fn waiver(&mut self, k: WaiverKind) -> &mut Self {
        self.p.waiver = Some(k);
        self
    }

    /// Records the day of what the waiver rests on.
    pub fn waiver_on(&mut self, d: Date) -> &mut Self {
        self.p.waiver_on = d;
        self
    }

    /// Answers the waiver question with what it rests on and when, which
    /// sets the waiver kind.
    pub fn waiver_for(&mut self, c: WaiverCause, on: Date) -> &mut Self {
        self.p.waiver = Some(c.kind());
        self.p.cause = c;
        self.p.waiver_on = on;
        self
    }

    /// Records the date of birth of the person a retirement waiver is for.
    pub fn born(&mut self, d: Date) -> &mut Self {
        self.p.born = d;
        self
    }

    /// Refuses a date with no waiver asked for. Go also refuses an unknown
    /// waiver kind or cause, which the enums here cannot hold.
    pub fn build(&self) -> Result<Positions, Error> {
        if !self.p.waiver_on.is_zero() && self.p.waiver.unwrap_or_default() == WaiverKind::None {
            return Err(invalid!(
                "estimated: a waiver date with no waiver asked for"
            ));
        }
        Ok(self.p)
    }
}

/// One annualisation period for the annualised income installment method:
/// the day it ends, the income from the start of the year to then, and the
/// jurisdiction's tax after credits on that income annualised.
///
/// The tax is supplied rather than computed here, because computing it needs
/// the jurisdiction's rate tables, deductions and credits for the year, and
/// this package holds none of them. Federally it is Schedule AI line 19; in
/// California, FTB 5805 Part III line 14c. A jurisdiction that computes the
/// period's tax itself from the income, as Pennsylvania does at a flat rate,
/// reads [`Period::income`] and ignores it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Period {
    pub(crate) end: Date,
    income: USCents,
    annualised_tax: USCents,
}

impl Period {
    /// The day the period ends.
    pub fn end(&self) -> Date {
        self.end
    }

    /// The income from the start of the year to the end.
    pub fn income(&self) -> USCents {
        self.income
    }

    /// The tax after credits on that income annualised.
    pub fn annualised_tax(&self) -> USCents {
        self.annualised_tax
    }
}

/// Builds a [`Period`].
#[derive(Clone, Debug, Default)]
pub struct PeriodBuilder {
    p: Period,
}

/// Starts a period.
pub fn new_period() -> PeriodBuilder {
    PeriodBuilder::default()
}

impl PeriodBuilder {
    /// Sets the end.
    pub fn end(&mut self, d: Date) -> &mut Self {
        self.p.end = d;
        self
    }

    /// Sets the income.
    pub fn income(&mut self, a: USCents) -> &mut Self {
        self.p.income = a;
        self
    }

    /// Sets the annualised tax.
    pub fn annualised_tax(&mut self, a: USCents) -> &mut Self {
        self.p.annualised_tax = a;
        self
    }

    /// Requires an end and refuses a negative tax.
    pub fn build(&self) -> Result<Period, Error> {
        if self.p.end.is_zero() {
            return Err(invalid!("estimated: a period needs its end date"));
        }
        if self.p.annualised_tax.0 < 0 {
            return Err(invalid!(
                "estimated: annualised tax of {} is negative",
                self.p.annualised_tax
            ));
        }
        Ok(self.p)
    }
}

/// Reports whether one of `periods` puts the filer in `j` from January 1 to
/// December 31 of `year`. Two periods that meet do not add up to a year: the
/// residence questions several states ask are about one residence held all
/// year.
pub fn resident_all_year(periods: &[ResidencePeriod], j: &Jurisdiction, year: i64) -> bool {
    let jan1 = Date {
        year,
        month: Month::JANUARY,
        day: 1,
    };
    let dec31 = Date {
        year,
        month: Month::DECEMBER,
        day: 31,
    };
    periods
        .iter()
        .any(|p| p.state == j.as_str() && p.from == jan1 && p.to == dec31)
}

/// A jurisdiction's own extras, as Go's `any`.
#[derive(Clone)]
struct Extras(Arc<dyn Any + Send + Sync>);

impl fmt::Debug for Extras {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str("Extras(..)")
    }
}

/// What a jurisdiction's estimated tax rules read about a household for one
/// tax year.
///
/// A fact only one jurisdiction reads is not here. It lives in a type that
/// jurisdiction's crate defines, carried by [`Facts::state`].
#[derive(Clone, Debug, Default)]
pub struct Facts {
    jurisdiction: Jurisdiction,
    tax_year: i64,
    status: FilingStatus,
    residence: Vec<ResidencePeriod>,
    prior_residence: Vec<ResidencePeriod>,
    this_year: Figures,
    prior_year: Option<PriorReturn>,
    pub(crate) withholding: Vec<Withheld>,
    pub(crate) payments: Vec<Payment>,
    return_filed: Date,
    first_met: Date,
    people: Vec<Person>,
    farm_share: Rate,
    fish_share: Rate,
    elections: Elections,
    positions: Positions,
    periods: Vec<Period>,
    state: Option<Extras>,
}

impl Facts {
    /// The jurisdiction.
    pub fn jurisdiction(&self) -> Jurisdiction {
        self.jurisdiction.clone()
    }

    /// The tax year.
    pub fn tax_year(&self) -> i64 {
        self.tax_year
    }

    /// The filing status.
    pub fn filing_status(&self) -> FilingStatus {
        self.status
    }

    /// Where the filer lived this year, in dated periods.
    pub fn residence(&self) -> Vec<ResidencePeriod> {
        self.residence.clone()
    }

    /// Where the filer lived last year.
    pub fn prior_residence(&self) -> Vec<ResidencePeriod> {
        self.prior_residence.clone()
    }

    /// This year's figures, as this jurisdiction measures them.
    pub fn this_year(&self) -> Figures {
        self.this_year
    }

    /// Last year's return, or `None` when none is on record.
    pub fn prior_year(&self) -> Option<PriorReturn> {
        self.prior_year
    }

    /// This year's withholding for this jurisdiction, by date.
    pub fn withholding(&self) -> Vec<Withheld> {
        self.withholding.clone()
    }

    /// This year's estimated payments, made or planned.
    pub fn payments(&self) -> Vec<Payment> {
        self.payments.clone()
    }

    /// The day this year's return was filed with the whole balance paid, or
    /// the zero Date if it has not been. It decides the waiver of the
    /// January installment for filing early, and the penalty stops on it:
    /// every shortfall still outstanding is paid that day.
    pub fn return_filed(&self) -> Date {
        self.return_filed
    }

    /// The day the filer could first expect to have to make estimated
    /// payments for the year, or the zero Date if the caller did not say.
    /// Several states give a filer who first meets the requirement after a
    /// due date fewer installments, or excuse the earlier ones; the federal
    /// rules do not.
    pub fn requirement_first_met(&self) -> Date {
        self.first_met
    }

    /// The filers on the return.
    pub fn people(&self) -> Vec<Person> {
        self.people.clone()
    }

    /// The share of gross income from farming, in basis points.
    pub fn farm_share(&self) -> Rate {
        self.farm_share
    }

    /// The share of gross income from fishing, in basis points.
    pub fn fish_share(&self) -> Rate {
        self.fish_share
    }

    /// The methods the filer chose.
    pub fn elections(&self) -> Elections {
        self.elections
    }

    /// The judgement calls the filer answered.
    pub fn positions(&self) -> Positions {
        self.positions
    }

    /// The annualisation periods, for the annualised method.
    pub fn periods(&self) -> Vec<Period> {
        self.periods.clone()
    }

    /// The jurisdiction's own extras, or `None`. Its type is defined in that
    /// jurisdiction's crate, such as californiataxcalc's `EstimatedFacts`;
    /// read it with `downcast_ref`.
    pub fn state(&self) -> Option<&(dyn Any + Send + Sync)> {
        self.state.as_ref().map(|e| &*e.0)
    }

    /// The sum of this year's withholding.
    pub fn withheld_total(&self) -> USCents {
        self.withholding.iter().map(|w| w.amount).sum()
    }

    /// Reports whether at least two-thirds of gross income is from farming
    /// and fishing together, which puts a filer under separate rules
    /// federally and in most states.
    pub fn farmer_or_fisher(&self) -> bool {
        i64::from(self.farm_share.0.wrapping_add(self.fish_share.0)) * 3 >= 20_000
    }
}

/// Builds [`Facts`].
#[derive(Clone, Debug, Default)]
pub struct FactsBuilder {
    f: Facts,
    has_this_year: bool,
}

/// Starts the facts for one jurisdiction and tax year.
pub fn new_facts(j: Jurisdiction, tax_year: i64) -> FactsBuilder {
    FactsBuilder {
        f: Facts {
            jurisdiction: j,
            tax_year,
            ..Facts::default()
        },
        has_this_year: false,
    }
}

impl FactsBuilder {
    /// Sets the filing status.
    pub fn filing_status(&mut self, s: FilingStatus) -> &mut Self {
        self.f.status = s;
        self
    }

    /// Adds periods of residence this year.
    pub fn residence(&mut self, p: &[ResidencePeriod]) -> &mut Self {
        self.f.residence.extend_from_slice(p);
        self
    }

    /// Adds periods of residence last year.
    pub fn prior_residence(&mut self, p: &[ResidencePeriod]) -> &mut Self {
        self.f.prior_residence.extend_from_slice(p);
        self
    }

    /// Sets this year's figures.
    pub fn this_year(&mut self, f: Figures) -> &mut Self {
        self.f.this_year = f;
        self.has_this_year = true;
        self
    }

    /// Sets last year's return.
    pub fn prior_year(&mut self, p: PriorReturn) -> &mut Self {
        self.f.prior_year = Some(p);
        self
    }

    /// Adds withholding.
    pub fn withheld(&mut self, w: &[Withheld]) -> &mut Self {
        self.f.withholding.extend_from_slice(w);
        self
    }

    /// Adds estimated payments.
    pub fn paid(&mut self, p: &[Payment]) -> &mut Self {
        self.f.payments.extend_from_slice(p);
        self
    }

    /// Sets the day the return was filed and paid.
    pub fn return_filed(&mut self, d: Date) -> &mut Self {
        self.f.return_filed = d;
        self
    }

    /// Records the day the filer could first expect to have to make
    /// estimated payments.
    pub fn requirement_first_met(&mut self, d: Date) -> &mut Self {
        self.f.first_met = d;
        self
    }

    /// Adds filers.
    pub fn people(&mut self, p: &[Person]) -> &mut Self {
        self.f.people.extend_from_slice(p);
        self
    }

    /// Sets the farming share.
    pub fn farm_share(&mut self, r: Rate) -> &mut Self {
        self.f.farm_share = r;
        self
    }

    /// Sets the fishing share.
    pub fn fish_share(&mut self, r: Rate) -> &mut Self {
        self.f.fish_share = r;
        self
    }

    /// Sets the elections.
    pub fn elections(&mut self, e: Elections) -> &mut Self {
        self.f.elections = e;
        self
    }

    /// Sets the positions.
    pub fn positions(&mut self, p: Positions) -> &mut Self {
        self.f.positions = p;
        self
    }

    /// Adds annualisation periods.
    pub fn periods(&mut self, p: &[Period]) -> &mut Self {
        self.f.periods.extend_from_slice(p);
        self
    }

    /// Attaches the jurisdiction's own extras.
    pub fn state<S: Any + Send + Sync>(&mut self, extras: S) -> &mut Self {
        self.f.state = Some(Extras(Arc::new(extras)));
        self
    }

    /// Validates the facts as a whole and returns a value nothing can
    /// change.
    ///
    /// It checks what is true of every jurisdiction: a jurisdiction and
    /// year, a filing status, this year's figures, withholding inside the
    /// tax year, payments no earlier than it, residence inside it, shares
    /// that add to no more than the whole, and four periods for the
    /// annualised method. Whether a fact suits a particular jurisdiction is
    /// that jurisdiction's check.
    pub fn build(&self) -> Result<Facts, Error> {
        let f = &self.f;
        if f.jurisdiction.as_str().len() != 2 {
            return Err(invalid!(
                "estimated: {} is not a jurisdiction code",
                working::quote(f.jurisdiction.as_str())
            ));
        }
        if f.tax_year < 1913 || f.tax_year > 9999 {
            return Err(invalid!("estimated: {} is not a tax year", f.tax_year));
        }
        if f.status == FilingStatus::Unspecified {
            return Err(invalid!("estimated: no filing status given"));
        }
        if !self.has_this_year {
            return Err(invalid!("estimated: this year's figures are required"));
        }

        let jan1 = Date {
            year: f.tax_year,
            month: Month::JANUARY,
            day: 1,
        };
        let dec31 = Date {
            year: f.tax_year,
            month: Month::DECEMBER,
            day: 31,
        };
        let in_year = |d: Date| !d.before(jan1) && !d.after(dec31);

        for w in &f.withholding {
            if !in_year(w.on) {
                return Err(invalid!(
                    "estimated: withholding on {} is outside tax year {}",
                    w.on,
                    f.tax_year
                ));
            }
        }
        for p in &f.payments {
            if !p.prior_year && p.on.before(jan1) {
                return Err(invalid!(
                    "estimated: a payment on {} is before tax year {} began",
                    p.on,
                    f.tax_year
                ));
            }
        }
        for r in &f.residence {
            if !in_year(r.from) || !in_year(r.to) {
                return Err(invalid!(
                    "estimated: residence {} to {} is outside tax year {}",
                    r.from,
                    r.to,
                    f.tax_year
                ));
            }
        }
        for r in &f.prior_residence {
            if r.from.year() != f.tax_year - 1 || r.to.year() != f.tax_year - 1 {
                return Err(invalid!(
                    "estimated: prior residence {} to {} is outside {}",
                    r.from,
                    r.to,
                    f.tax_year - 1
                ));
            }
        }
        if !f.first_met.is_zero() && !in_year(f.first_met) {
            return Err(invalid!(
                "estimated: the requirement cannot first be met on {}, outside tax year {}",
                f.first_met,
                f.tax_year
            ));
        }
        if !f.return_filed.is_zero() && f.return_filed.year() <= f.tax_year {
            return Err(invalid!(
                "estimated: a {} return cannot be filed on {}",
                f.tax_year,
                f.return_filed
            ));
        }
        if f.farm_share.0 < 0
            || f.fish_share.0 < 0
            || f.farm_share.0.wrapping_add(f.fish_share.0) > 10_000
        {
            return Err(invalid!(
                "estimated: farming {} and fishing {} are not shares of one income",
                f.farm_share,
                f.fish_share
            ));
        }
        if f.elections.annualised && f.periods.len() != 4 {
            return Err(invalid!(
                "estimated: the annualised method needs four periods, not {}",
                f.periods.len()
            ));
        }
        for i in 1..f.periods.len() {
            if !f.periods[i].end.after(f.periods[i - 1].end) {
                return Err(invalid!(
                    "estimated: annualisation periods must end in order"
                ));
            }
        }
        Ok(f.clone())
    }
}
