//! The answers a jurisdiction gives. Each has the same shape in every
//! jurisdiction, because the questions are the same, and each is built
//! through a builder that checks it is internally consistent.

use std::collections::HashSet;
use std::fmt;

use money::{FineRate, Rate, USCents};

use crate::waiver::WaiverOutcome;
use crate::{Date, Error, WaiverKind, invalid};

/// What a required annual payment option is a percentage of.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Basis {
    /// No basis; an option refuses it.
    #[default]
    Unspecified,

    /// This year's tax: 90% of it federally and in most states.
    ThisYearTax,

    /// Last year's tax: 100%, or 110% at higher income.
    PriorYearTax,

    /// Last year's income taxed at this year's rate, which Pennsylvania uses
    /// instead of last year's tax.
    PriorYearIncome,

    /// An exception under which no estimated payments are needed: tax less
    /// withholding under a threshold, no tax last year, or a jurisdiction's
    /// own exemption. Its amount is zero.
    Exception,
}

/// Names the basis in plain words.
impl fmt::Display for Basis {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Basis::ThisYearTax => "this year's tax",
            Basis::PriorYearTax => "last year's tax",
            Basis::PriorYearIncome => "last year's income at this year's rate",
            Basis::Exception => "an exception",
            Basis::Unspecified => "unspecified basis",
        })
    }
}

/// One way of measuring the required annual payment.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct TargetOption {
    basis: Basis,
    percent: Rate,
    amount: USCents,
    available: bool,
    why_not: String,
    label: String,
}

impl TargetOption {
    /// What the option is a percentage of.
    pub fn basis(&self) -> Basis {
        self.basis
    }

    /// The percentage.
    pub fn percent(&self) -> Rate {
        self.percent
    }

    /// The amount the option gives.
    pub fn amount(&self) -> USCents {
        self.amount
    }

    /// Whether the option is open to this filer.
    pub fn available(&self) -> bool {
        self.available
    }

    /// Says why an unavailable option is unavailable, in plain words.
    pub fn why_not(&self) -> String {
        self.why_not.clone()
    }

    /// Names the option in plain words, as "90% of this year's tax" or "Tax
    /// less withholding under $1,000".
    pub fn label(&self) -> String {
        self.label.clone()
    }
}

/// Builds a [`TargetOption`].
#[derive(Clone, Debug, Default)]
pub struct TargetOptionBuilder {
    o: TargetOption,
    /// Labelled by hand through [`new_exception`].
    free: bool,
}

/// Starts an option that is available unless
/// [`TargetOptionBuilder::unavailable`] is called.
pub fn new_target_option(basis: Basis, percent: Rate, amount: USCents) -> TargetOptionBuilder {
    TargetOptionBuilder {
        o: TargetOption {
            basis,
            percent,
            amount,
            available: true,
            ..TargetOption::default()
        },
        free: false,
    }
}

impl TargetOptionBuilder {
    /// Marks the option as not open to this filer, and says why.
    pub fn unavailable(&mut self, why: impl Into<String>) -> &mut Self {
        self.o.available = false;
        self.o.why_not = why.into();
        self
    }

    /// Replaces the label `build` would give the option.
    pub fn label(&mut self, label: impl Into<String>) -> &mut Self {
        self.o.label = label.into();
        self
    }

    /// Checks the option, and labels it by its basis and percentage when no
    /// label was given. An exception is always available, at no amount and
    /// no percentage, and a label given to [`new_exception`] names no amount.
    pub fn build(&self) -> Result<TargetOption, Error> {
        let mut o = self.o.clone();
        if o.label.is_empty() {
            o.label = default_label(&o);
        }
        if o.basis == Basis::Exception && (o.amount.0 != 0 || o.percent.0 != 0 || !o.available) {
            return Err(invalid!(
                "estimated: an exception is available, at no amount and no percentage"
            ));
        }
        if o.basis == Basis::Exception && o.label.is_empty() {
            return Err(invalid!("estimated: an exception needs a label"));
        }
        if self.free && o.label.contains(['$', '%']) {
            return Err(invalid!(
                "estimated: exception {} names a figure; use ExceptionBelow or ExceptionAtMost",
                working::quote(&o.label)
            ));
        }
        if o.basis == Basis::Unspecified {
            return Err(invalid!("estimated: an option needs a basis"));
        }
        if o.percent.0 < 0 || o.amount.0 < 0 {
            return Err(invalid!(
                "estimated: an option's percentage and amount cannot be negative"
            ));
        }
        if !o.available && o.why_not.is_empty() {
            return Err(invalid!("estimated: an unavailable option must say why"));
        }
        if !o.available && o.amount.0 != 0 {
            return Err(invalid!(
                "estimated: the unavailable {} option carries {}; an option that isn't open has no amount",
                o.basis,
                o.amount
            ));
        }
        if o.available && !o.why_not.is_empty() {
            return Err(invalid!(
                "estimated: an available option has no reason to be unavailable"
            ));
        }
        Ok(o)
    }
}

/// Starts an exception option with a plain label that names no amount or
/// percentage, as "No tax due last year" or "Return filed and paid by Jan
/// 31, 2027". An exception that compares an amount with a threshold is made
/// with [`exception_below`] or [`exception_at_most`], which check the
/// comparison.
pub fn new_exception(label: impl Into<String>) -> TargetOptionBuilder {
    TargetOptionBuilder {
        o: TargetOption {
            basis: Basis::Exception,
            available: true,
            label: label.into(),
            ..TargetOption::default()
        },
        free: true,
    }
}

/// The exception that applies because `what` is under `limit`, labelled
/// "`<what>` under `<limit>`". It is an error when `amount` is not under
/// `limit`, so the label is true of the figures it was made from.
pub fn exception_below(what: &str, amount: USCents, limit: USCents) -> Result<TargetOption, Error> {
    if amount >= limit {
        return Err(invalid!(
            "estimated: {what} of {amount} is not under {limit}"
        ));
    }
    TargetOptionBuilder {
        o: TargetOption {
            basis: Basis::Exception,
            available: true,
            label: format!("{what} under {}", limit.whole()),
            ..TargetOption::default()
        },
        free: false,
    }
    .build()
}

/// The exception that applies because `what` is no more than `limit`,
/// labelled "`<what>` of `<limit>` or less". It is an error when `amount` is over
/// `limit`.
pub fn exception_at_most(
    what: &str,
    amount: USCents,
    limit: USCents,
) -> Result<TargetOption, Error> {
    if amount > limit {
        return Err(invalid!("estimated: {what} of {amount} is over {limit}"));
    }
    TargetOptionBuilder {
        o: TargetOption {
            basis: Basis::Exception,
            available: true,
            label: format!("{what} of {} or less", limit.whole()),
            ..TargetOption::default()
        },
        free: false,
    }
    .build()
}

/// Names an option by its basis and percentage.
fn default_label(o: &TargetOption) -> String {
    match o.basis {
        Basis::ThisYearTax => format!("{} of this year's tax", o.percent),
        Basis::PriorYearTax => format!("{} of last year's tax", o.percent),
        Basis::PriorYearIncome => "Last year's income at this year's rate".to_string(),
        _ => String::new(),
    }
}

/// Whether a household has to make estimated payments, and what total keeps
/// it clear of the penalty.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct Requirement {
    required: bool,
    why: String,
    target: USCents,
    options: Vec<TargetOption>,
    chosen: i64,
    exempt: bool,
    no_target: bool,
}

/// The zero Requirement, as Go's: nothing chosen.
impl Default for Requirement {
    fn default() -> Requirement {
        Requirement {
            required: false,
            why: String::new(),
            target: USCents(0),
            options: Vec::new(),
            chosen: 0,
            exempt: false,
            no_target: false,
        }
    }
}

impl Requirement {
    /// Reports whether estimated payments are needed on top of the
    /// withholding.
    pub fn required(&self) -> bool {
        self.required
    }

    /// Says, in plain words, why payments are or are not required.
    pub fn why(&self) -> String {
        self.why.clone()
    }

    /// The required annual payment: what withholding and estimated payments
    /// together must reach to avoid the penalty. It is zero when an
    /// exception is the target, and when no option can be measured.
    pub fn target(&self) -> USCents {
        self.target
    }

    /// Reports that an exception means no penalty can arise, whatever is
    /// paid. The exception is then the chosen option.
    pub fn exempt(&self) -> bool {
        self.exempt
    }

    /// Reports whether any option could be measured. It is false when this
    /// year's tax was not given and no other option is available, and then
    /// there is no target, no schedule and no penalty to show.
    pub fn target_known(&self) -> bool {
        !self.no_target
    }

    /// Every way of measuring the target, including those this filer cannot
    /// use and why.
    pub fn options(&self) -> Vec<TargetOption> {
        self.options.clone()
    }

    /// The option the target came from: exactly one whenever
    /// [`Requirement::target_known`], an exception included, and none
    /// otherwise.
    pub fn chosen(&self) -> Option<TargetOption> {
        if self.chosen < 0 {
            return None;
        }
        // Go's zero Requirement indexes an empty slice and panics; this
        // returns None instead.
        self.options.get(self.chosen as usize).cloned()
    }
}

/// Builds a [`Requirement`].
#[derive(Clone, Debug)]
pub struct RequirementBuilder {
    r: Requirement,
    chosen: Option<Basis>,
    exempt: bool,
    exception: TargetOption,
    no_target: bool,
}

/// Starts a requirement.
pub fn new_requirement() -> RequirementBuilder {
    RequirementBuilder {
        r: Requirement {
            chosen: -1,
            ..Requirement::default()
        },
        chosen: None,
        exempt: false,
        exception: TargetOption::default(),
        no_target: false,
    }
}

impl RequirementBuilder {
    /// Records whether payments are required, and why.
    pub fn required(&mut self, required: bool, why: impl Into<String>) -> &mut Self {
        self.r.required = required;
        self.r.why = why.into();
        self
    }

    /// Adds an option.
    pub fn option(&mut self, o: TargetOption) -> &mut Self {
        self.r.options.push(o);
        self
    }

    /// Sets the target from the available option with this basis.
    pub fn choose(&mut self, basis: Basis) -> &mut Self {
        self.chosen = Some(basis);
        self
    }

    /// Makes the exception the target: no penalty can arise, so the target
    /// is zero. The exception is added to the options and chosen.
    pub fn exempt(&mut self, exception: TargetOption) -> &mut Self {
        self.exempt = true;
        self.exception = exception;
        self
    }

    /// Sets no target, because no option can be measured: this year's tax
    /// was not given and every other option is unavailable.
    pub fn no_target(&mut self) -> &mut Self {
        self.no_target = true;
        self
    }

    /// Checks the target is the chosen option's amount, and that the chosen
    /// option is available and is the smallest available one: the rules
    /// give the filer the lesser of their options, never a larger one.
    pub fn build(&self) -> Result<Requirement, Error> {
        let mut r = self.r.clone();
        if r.why.is_empty() {
            return Err(invalid!("estimated: a requirement must say why"));
        }
        if self.no_target {
            if self.exempt || self.chosen.is_some() {
                return Err(invalid!(
                    "estimated: a requirement with no target has no chosen option and no exemption"
                ));
            }
            if r.required {
                return Err(invalid!(
                    "estimated: a requirement with no target cannot say payments are required"
                ));
            }
            for o in &r.options {
                if o.available {
                    return Err(invalid!(
                        "estimated: {} is available, so there is a target",
                        o.basis
                    ));
                }
            }
            r.no_target = true;
            return Ok(r);
        }
        if r.options.iter().any(|o| o.basis == Basis::Exception) {
            return Err(invalid!(
                "estimated: an exception is added with Exempt, which chooses it"
            ));
        }
        if self.exempt {
            if self.chosen.is_some() {
                return Err(invalid!(
                    "estimated: an exempt requirement chooses its exception and nothing else"
                ));
            }
            if r.required {
                return Err(invalid!(
                    "estimated: an exempt household is not required to pay"
                ));
            }
            if self.exception.basis != Basis::Exception || self.exception.label.is_empty() {
                return Err(invalid!(
                    "estimated: an exemption needs an exception made by NewException, ExceptionBelow or ExceptionAtMost"
                ));
            }
            r.options.push(self.exception.clone());
            r.chosen = r.options.len() as i64 - 1;
            r.exempt = true;
            r.target = USCents(0);
            return Ok(r);
        }
        let Some(chosen) = self.chosen else {
            return Err(invalid!(
                "estimated: a requirement needs a chosen option or an exemption"
            ));
        };
        for (i, o) in r.options.iter().enumerate() {
            if o.basis == chosen {
                r.chosen = i as i64;
            }
        }
        if r.chosen < 0 || !r.options[r.chosen as usize].available {
            return Err(invalid!(
                "estimated: the chosen option, {chosen}, is not an available option"
            ));
        }
        r.target = r.options[r.chosen as usize].amount;
        for o in &r.options {
            if o.available && o.amount < r.target {
                return Err(invalid!(
                    "estimated: {} gives {}, less than the chosen {}",
                    o.basis,
                    o.amount,
                    r.target
                ));
            }
        }
        Ok(r)
    }
}

/// Where an installment stands.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Status {
    /// No status.
    #[default]
    Unspecified,

    /// A due date by which everything needed had been credited.
    Met,

    /// A due date by which less than was needed had been credited.
    Short,

    /// A due date by which nothing is needed.
    NothingDue,

    /// An installment on which no penalty arises, whatever was paid: the
    /// January installment for a return filed and paid by the end of
    /// January, or an installment an exception covers, as Pennsylvania's
    /// REV-1630 Exceptions 1 and 2. An excused installment can still show a
    /// shortfall, which then bears no penalty.
    Excused,
}

/// The status every jurisdiction gives a due date: nothing due when nothing
/// is needed by it, met when what was credited by it reaches what was
/// needed, excused when it is not met but an exception covers it, and short
/// otherwise.
pub fn status_of(needed_by: USCents, credited_by: USCents, excused: bool) -> Status {
    if needed_by.0 <= 0 {
        Status::NothingDue
    } else if credited_by >= needed_by {
        Status::Met
    } else if excused {
        Status::Excused
    } else {
        Status::Short
    }
}

/// Names the status: "met", "short", "nothing due" or "excused".
impl fmt::Display for Status {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Status::Met => "met",
            Status::Short => "short",
            Status::NothingDue => "nothing due",
            Status::Excused => "excused",
            Status::Unspecified => "unspecified status",
        })
    }
}

/// One due date: what must have been paid by it, what had been credited by
/// it, and what was short.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Installment {
    number: i64,
    due: Date,
    needed_by: USCents,
    credited_by: USCents,
    shortfall: USCents,
    excused: bool,
    pub(crate) status: Status,
}

impl Installment {
    /// The installment's position, from 1.
    pub fn number(&self) -> i64 {
        self.number
    }

    /// The due date, already moved for weekends and holidays.
    pub fn due(&self) -> Date {
        self.due
    }

    /// The running total required by the due date.
    pub fn needed_by(&self) -> USCents {
        self.needed_by
    }

    /// The running total of withholding and payments credited by the due
    /// date.
    pub fn credited_by(&self) -> USCents {
        self.credited_by
    }

    /// This installment's own underpayment on its due date, as Form 2210
    /// line 17 and FTB 5805 Worksheet II line 8 compute it. An earlier
    /// installment's underpayment is not repeated here.
    pub fn shortfall(&self) -> USCents {
        self.shortfall
    }

    /// [`status_of`] the running totals.
    pub fn status(&self) -> Status {
        self.status
    }
}

/// Builds an [`Installment`].
#[derive(Clone, Debug, Default)]
pub struct InstallmentBuilder {
    i: Installment,
}

/// Starts installment `number`, due on `due`.
pub fn new_installment(number: i64, due: Date) -> InstallmentBuilder {
    InstallmentBuilder {
        i: Installment {
            number,
            due,
            ..Installment::default()
        },
    }
}

impl InstallmentBuilder {
    /// Sets the running total needed.
    pub fn needed_by(&mut self, a: USCents) -> &mut Self {
        self.i.needed_by = a;
        self
    }

    /// Sets the running total credited.
    pub fn credited_by(&mut self, a: USCents) -> &mut Self {
        self.i.credited_by = a;
        self
    }

    /// Sets the shortfall.
    pub fn shortfall(&mut self, a: USCents) -> &mut Self {
        self.i.shortfall = a;
        self
    }

    /// Records that an exception covers the installment, so no penalty
    /// arises on it whatever was paid.
    pub fn excused(&mut self) -> &mut Self {
        self.i.excused = true;
        self
    }

    /// Checks the amounts are not negative, sets the status by
    /// [`status_of`], and checks the shortfall agrees with it: only a short
    /// or excused installment has one.
    pub fn build(&self) -> Result<Installment, Error> {
        let mut i = self.i;
        i.status = status_of(i.needed_by, i.credited_by, i.excused);
        if i.number < 1 {
            return Err(invalid!("estimated: installment number {}", i.number));
        }
        if i.due.is_zero() {
            return Err(invalid!("estimated: an installment needs a due date"));
        }
        if i.needed_by.0 < 0 || i.credited_by.0 < 0 || i.shortfall.0 < 0 {
            return Err(invalid!(
                "estimated: an installment's amounts cannot be negative"
            ));
        }
        if i.shortfall.0 > 0 && i.status != Status::Short && i.status != Status::Excused {
            return Err(invalid!(
                "estimated: installment {} is {} with a shortfall of {}",
                i.number,
                i.status,
                i.shortfall
            ));
        }
        Ok(i)
    }
}

/// What kind of charge a penalty part is.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum PartKind {
    /// No kind; a part refuses it.
    #[default]
    Unspecified,

    /// Interest on the unpaid amount for the days or months it stayed
    /// unpaid.
    Interest,

    /// A flat percentage of the shortfall, as Indiana's 10%.
    Flat,

    /// A percentage per month or part of a month, as Vermont's and
    /// Michigan's penalty on top of interest.
    Monthly,
}

/// Names the kind: "interest", "flat" or "monthly".
impl fmt::Display for PartKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            PartKind::Interest => "interest",
            PartKind::Flat => "flat",
            PartKind::Monthly => "monthly",
            PartKind::Unspecified => "unspecified part",
        })
    }
}

/// One computation on a penalty worksheet: an amount, a span, a rate and the
/// charge.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct PenaltyPart {
    pub(crate) kind: PartKind,
    principal: USCents,
    from: Date,
    to: Date,
    days: i64,
    months: i64,
    rate: FineRate,
    pub(crate) charge: USCents,
    source: String,
}

impl PenaltyPart {
    /// The kind of charge.
    pub fn kind(&self) -> PartKind {
        self.kind
    }

    /// The unpaid amount the charge is on.
    pub fn principal(&self) -> USCents {
        self.principal
    }

    /// The start of the span. Interest runs for the days after it up to and
    /// including [`PenaltyPart::to`].
    pub fn from(&self) -> Date {
        self.from
    }

    /// The end of the span.
    pub fn to(&self) -> Date {
        self.to
    }

    /// The days counted, for interest by the day.
    pub fn days(&self) -> i64 {
        self.days
    }

    /// The months counted, for a charge by the month.
    pub fn months(&self) -> i64 {
        self.months
    }

    /// The annual rate for interest, the monthly rate for a monthly charge,
    /// or the percentage for a flat one, to the nearest basis point.
    pub fn rate(&self) -> Rate {
        self.rate.nearest()
    }

    /// [`PenaltyPart::rate`] as published, which differs from it only for a
    /// rate basis points cannot hold.
    pub fn fine_rate(&self) -> FineRate {
        self.rate
    }

    /// The charge.
    pub fn charge(&self) -> USCents {
        self.charge
    }

    /// Cites the document that set the rate.
    pub fn source(&self) -> String {
        self.source.clone()
    }
}

/// Builds a [`PenaltyPart`].
#[derive(Clone, Debug, Default)]
pub struct PenaltyPartBuilder {
    p: PenaltyPart,
}

/// Starts a part of the given kind on `principal` at `rate`.
pub fn new_penalty_part(kind: PartKind, principal: USCents, rate: Rate) -> PenaltyPartBuilder {
    PenaltyPartBuilder {
        p: PenaltyPart {
            kind,
            principal,
            rate: rate.fine(),
            ..PenaltyPart::default()
        },
    }
}

impl PenaltyPartBuilder {
    /// Replaces the rate given to [`new_penalty_part`] with one basis points
    /// cannot hold.
    pub fn fine_rate(&mut self, r: FineRate) -> &mut Self {
        self.p.rate = r;
        self
    }

    /// Sets the span.
    pub fn span(&mut self, from: Date, to: Date) -> &mut Self {
        self.p.from = from;
        self.p.to = to;
        self
    }

    /// Sets the days counted.
    pub fn days(&mut self, n: i64) -> &mut Self {
        self.p.days = n;
        self
    }

    /// Sets the months counted.
    pub fn months(&mut self, n: i64) -> &mut Self {
        self.p.months = n;
        self
    }

    /// Sets the charge.
    pub fn charge(&mut self, a: USCents) -> &mut Self {
        self.p.charge = a;
        self
    }

    /// Sets the source of the rate.
    pub fn source(&mut self, cite: impl Into<String>) -> &mut Self {
        self.p.source = cite.into();
        self
    }

    /// Requires a kind and a source, figures that are not negative, and for
    /// interest a span that runs forward.
    pub fn build(&self) -> Result<PenaltyPart, Error> {
        let p = &self.p;
        if p.kind == PartKind::Unspecified {
            return Err(invalid!("estimated: a penalty part needs a kind"));
        }
        if p.principal.0 < 0 || p.charge.0 < 0 || p.rate.0 < 0 || p.days < 0 || p.months < 0 {
            return Err(invalid!(
                "estimated: a penalty part's figures cannot be negative"
            ));
        }
        if p.source.is_empty() {
            return Err(invalid!(
                "estimated: a penalty part must cite the source of its rate"
            ));
        }
        if p.kind == PartKind::Interest && (p.from.is_zero() || p.to.before(p.from)) {
            return Err(invalid!(
                "estimated: an interest part needs a span that runs forward"
            ));
        }
        Ok(p.clone())
    }
}

/// The penalty on one installment's shortfall.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct PenaltyLine {
    installment: i64,
    due: Date,
    shortfall: USCents,
    days: i64,
    pub(crate) parts: Vec<PenaltyPart>,
    total: USCents,
}

impl PenaltyLine {
    /// The number of the installment this line is for.
    pub fn installment(&self) -> i64 {
        self.installment
    }

    /// The installment's due date.
    pub fn due(&self) -> Date {
        self.due
    }

    /// The installment's underpayment on its due date.
    pub fn shortfall(&self) -> USCents {
        self.shortfall
    }

    /// How long the last of the shortfall stayed unpaid, up to the date the
    /// penalty was figured to.
    pub fn days(&self) -> i64 {
        self.days
    }

    /// The computations, one for each amount and rate period.
    pub fn parts(&self) -> Vec<PenaltyPart> {
        self.parts.clone()
    }

    /// The sum of the parts.
    pub fn total(&self) -> USCents {
        self.total
    }
}

/// Builds a [`PenaltyLine`].
#[derive(Clone, Debug, Default)]
pub struct PenaltyLineBuilder {
    l: PenaltyLine,
}

/// Starts the line for installment `installment`, due on `due`, short by
/// `shortfall`.
pub fn new_penalty_line(installment: i64, due: Date, shortfall: USCents) -> PenaltyLineBuilder {
    PenaltyLineBuilder {
        l: PenaltyLine {
            installment,
            due,
            shortfall,
            ..PenaltyLine::default()
        },
    }
}

impl PenaltyLineBuilder {
    /// Sets how long the shortfall stayed unpaid.
    pub fn days(&mut self, n: i64) -> &mut Self {
        self.l.days = n;
        self
    }

    /// Adds a part.
    pub fn part(&mut self, p: PenaltyPart) -> &mut Self {
        self.l.parts.push(p);
        self
    }

    /// Totals the parts and checks no part charges interest on more than the
    /// shortfall.
    pub fn build(&self) -> Result<PenaltyLine, Error> {
        let mut l = self.l.clone();
        if l.installment < 1 || l.due.is_zero() {
            return Err(invalid!(
                "estimated: a penalty line needs its installment and due date"
            ));
        }
        if l.shortfall.0 <= 0 {
            return Err(invalid!("estimated: a penalty line needs a shortfall"));
        }
        l.total = USCents(0);
        for p in &l.parts {
            if p.principal > l.shortfall {
                return Err(invalid!(
                    "estimated: a part charges on {}, more than the shortfall of {}",
                    p.principal,
                    l.shortfall
                ));
            }
            l.total += p.charge;
        }
        Ok(l)
    }
}

/// The underpayment penalty, installment by installment.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct Penalty {
    lines: Vec<PenaltyLine>,
    total: USCents,
    waiver: WaiverKind,
    as_of: Date,
    upper_bound: bool,
    reason: String,
    through: Date,
    outcome: WaiverOutcome,
    /// The jurisdiction's sentence for a $0 penalty, if it has one.
    none: String,

    // What the reason was derived from, kept for to_builder.
    req: Requirement,
    schedule: Vec<Installment>,
    context: String,
}

impl Penalty {
    /// One or two plain sentences saying what decided the penalty: the
    /// exception that applies, that every due date was met, or how many were
    /// short. `build` derives it from the requirement, the schedule and the
    /// lines, so it states only what those figures show.
    pub fn reason(&self) -> String {
        self.reason.clone()
    }

    /// The penalty before any waiver.
    pub fn total(&self) -> USCents {
        self.total
    }

    /// One per installment that was short.
    pub fn lines(&self) -> Vec<PenaltyLine> {
        self.lines.clone()
    }

    /// The waiver the filer asks for. The tax authority decides it, so
    /// [`Penalty::total`] does not subtract it.
    pub fn waiver(&self) -> WaiverKind {
        self.waiver
    }

    /// The date the penalty was figured to.
    pub fn as_of(&self) -> Date {
        self.as_of
    }

    /// Reports that the penalty is the most it can be rather than what it
    /// is: this year's tax was not given, so it is figured against a target
    /// that can only be too high, and it is less if this year's tax turns
    /// out lower. It is false when the interest stops at
    /// [`Penalty::rated_through`], because the penalty then leaves days out
    /// and is no maximum.
    pub fn upper_bound(&self) -> bool {
        self.upper_bound && self.through.is_zero()
    }

    /// The last day a published rate covers, when the penalty stops there
    /// with shortfalls still running; the zero Date when it runs to its end.
    /// Days after it bear more interest at a rate not yet published.
    pub fn rated_through(&self) -> Date {
        self.through
    }

    /// What the rules make of the waiver asked for, and `None` when none was
    /// asked for or no penalty arises.
    pub fn waiver_outcome(&self) -> Option<WaiverOutcome> {
        if self.outcome.is_set() {
            Some(self.outcome.clone())
        } else {
            None
        }
    }

    /// The penalty if an eligible waiver is granted: the charges of the
    /// kinds it leaves, [`WaiverOutcome::kept`], and nothing when it covers
    /// them all. For a waiver refused or undecided, or none asked for, it is
    /// the penalty.
    pub fn if_waived(&self) -> USCents {
        if !self.outcome.eligible() {
            return self.total;
        }
        let mut left = USCents(0);
        for l in &self.lines {
            for part in &l.parts {
                for k in self.outcome.kept_ref() {
                    if part.kind == *k {
                        left += part.charge;
                    }
                }
            }
        }
        left
    }

    /// How many due dates in the schedule the penalty was figured from are
    /// short: what was credited by them falls short of what was needed. The
    /// reason's count is this number.
    pub fn short_due_dates(&self) -> i64 {
        short_and_excused(&self.schedule).0
    }

    /// The penalty in one sentence a person reads: "No penalty.", "About
    /// $109.", "At most $109, and less if this year's tax turns out lower.",
    /// "About $109 through Dec 31, 2026, then more at a rate not yet
    /// published.", or, for a shortfall that only runs past the last
    /// published rate, "No penalty through Jan 15, 2027; after that it
    /// accrues at an unpublished rate."
    pub fn summary(&self) -> String {
        if self.total.0 == 0 && self.through.is_zero() && !self.none.is_empty() {
            self.none.clone()
        } else if self.total.0 == 0 && self.through.is_zero() {
            "No penalty.".to_string()
        } else if self.total.0 == 0 {
            format!(
                "No penalty through {}; after that it accrues at an unpublished rate.",
                short_date(self.through)
            )
        } else if !self.through.is_zero() {
            format!(
                "About {} through {}, then more at a rate not yet published.",
                self.total.whole(),
                short_date(self.through)
            )
        } else if self.upper_bound {
            format!(
                "At most {}, and less if this year's tax turns out lower.",
                self.total.whole()
            )
        } else {
            format!("About {}.", self.total.whole())
        }
    }

    /// Starts a builder holding everything in `self`, for a rule that
    /// computes a penalty with a helper and then adds to it.
    pub fn to_builder(&self) -> PenaltyBuilder {
        PenaltyBuilder {
            p: Penalty {
                lines: self.lines.clone(),
                waiver: self.waiver,
                as_of: self.as_of,
                upper_bound: self.upper_bound,
                through: self.through,
                outcome: self.outcome.clone(),
                none: self.none.clone(),
                ..Penalty::default()
            },
            req: self.req.clone(),
            schedule: self.schedule.clone(),
            because: true,
            context: self.context.clone(),
        }
    }
}

/// "Dec 31, 2026".
fn short_date(d: Date) -> String {
    let m = d.month.to_string();
    format!("{} {}, {}", &m[..3.min(m.len())], d.day, d.year)
}

/// Builds a [`Penalty`].
#[derive(Clone, Debug, Default)]
pub struct PenaltyBuilder {
    p: Penalty,
    req: Requirement,
    schedule: Vec<Installment>,
    because: bool,
    context: String,
}

/// Starts a penalty figured to `as_of`.
pub fn new_penalty(as_of: Date) -> PenaltyBuilder {
    PenaltyBuilder {
        p: Penalty {
            as_of,
            ..Penalty::default()
        },
        ..PenaltyBuilder::default()
    }
}

/// The sentence a penalty's reason gives when an exception is the target:
/// its label, as a sentence.
pub fn exception_applies(o: &TargetOption) -> String {
    format!("{}.", o.label)
}

/// Says what decided the penalty, from the requirement, the schedule and the
/// lines.
fn penalty_reason(r: &Requirement, schedule: &[Installment], p: &Penalty) -> Result<String, Error> {
    if r.exempt {
        if p.total.0 != 0 {
            return Err(invalid!(
                "estimated: an exception applies, yet the penalty is {}",
                p.total
            ));
        }
        return Ok(exception_applies(&r.options[r.chosen as usize]));
    }
    let (short, excused) = short_and_excused(schedule);
    Ok(if short == 1 {
        "Payments fall short on one due date.".to_string()
    } else if short > 1 {
        format!("Payments fall short on {} due dates.", count(short))
    } else if p.total.0 > 0 {
        "A penalty is charged on what was paid late.".to_string()
    } else if excused == 0 {
        "Your payments meet every due date.".to_string()
    } else {
        "The due dates that fall short are excused.".to_string()
    })
}

/// Counts the installments that are short, and those that are short but
/// excused.
fn short_and_excused(schedule: &[Installment]) -> (i64, i64) {
    let (mut short, mut excused) = (0, 0);
    for i in schedule {
        match i.status {
            Status::Short => short += 1,
            Status::Excused => excused += 1,
            _ => {}
        }
    }
    (short, excused)
}

fn count(n: i64) -> String {
    const WORDS: [&str; 10] = [
        "no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
    ];
    if (0..10).contains(&n) {
        return WORDS[n as usize].to_string();
    }
    n.to_string()
}

impl PenaltyBuilder {
    /// Records the requirement and the schedule the penalty was figured
    /// from, which `build` reads for [`Penalty::reason`]. Every penalty
    /// needs it.
    pub fn because(&mut self, r: Requirement, schedule: &[Installment]) -> &mut Self {
        self.req = r;
        self.schedule = schedule.to_vec();
        self.because = true;
        self
    }

    /// Adds a line.
    pub fn line(&mut self, l: PenaltyLine) -> &mut Self {
        self.p.lines.push(l);
        self
    }

    /// Records the waiver asked for.
    pub fn waiver(&mut self, k: WaiverKind) -> &mut Self {
        self.p.waiver = k;
        self
    }

    /// Adds a second sentence to the derived reason: the reason "Payments
    /// fell short on two due dates." with the sentence "The January payment
    /// still counts after a move." reads "Payments fell short on two due
    /// dates. The January payment still counts after a move." It is for a
    /// rule of the jurisdiction's that bears on the reason, and names no
    /// amount or percentage: those come only from the figures the reason is
    /// derived from.
    pub fn context(&mut self, sentence: impl Into<String>) -> &mut Self {
        self.context = sentence.into();
        self
    }

    /// Records what the rules make of the waiver asked for. Every penalty
    /// where a waiver was asked for and a penalty arises needs one.
    pub fn waiver_outcome(&mut self, o: WaiverOutcome) -> &mut Self {
        self.p.outcome = o;
        self
    }

    /// Records that the interest stops at `d`, the last day a published
    /// rate covers, with shortfalls still running after it.
    pub fn rated_through(&mut self, d: Date) -> &mut Self {
        self.p.through = d;
        self
    }

    /// Sets the sentence [`Penalty::summary`] gives when the penalty is $0,
    /// in place of "No penalty.", for a jurisdiction whose own rule says why
    /// there is none.
    pub fn no_penalty(&mut self, sentence: impl Into<String>) -> &mut Self {
        self.p.none = sentence.into();
        self
    }

    /// Marks the penalty as the most it can be.
    pub fn upper_bound(&mut self) -> &mut Self {
        self.p.upper_bound = true;
        self
    }

    /// Totals the lines and checks there is one per installment at most.
    pub fn build(&self) -> Result<Penalty, Error> {
        let mut p = self.p.clone();
        if p.as_of.is_zero() {
            return Err(invalid!(
                "estimated: a penalty needs the date it is figured to"
            ));
        }
        let mut seen = HashSet::new();
        p.total = USCents(0);
        for l in &p.lines {
            if !seen.insert(l.installment) {
                return Err(invalid!(
                    "estimated: installment {} has two penalty lines",
                    l.installment
                ));
            }
            p.total += l.total;
        }
        if !self.because {
            return Err(invalid!(
                "estimated: a penalty needs the requirement and schedule it was figured from"
            ));
        }
        if p.outcome.is_set() {
            p.outcome.check()?;
        }
        let mut reason = penalty_reason(&self.req, &self.schedule, &p)?;
        if !self.context.is_empty() {
            let body = self.context.strip_suffix('.').unwrap_or(&self.context);
            let first_upper = body
                .as_bytes()
                .first()
                .is_some_and(|b| b.is_ascii() && !b.is_ascii_lowercase());
            if body.is_empty()
                || !self.context.ends_with('.')
                || body.contains(['$', '%', '.', '?', '!', ';'])
                || !first_upper
            {
                return Err(invalid!(
                    "estimated: the penalty's context {} must be one sentence with no figure",
                    working::quote(&self.context)
                ));
            }
            reason.push(' ');
            reason.push_str(&self.context);
        }
        p.reason = reason;
        p.req = self.req.clone();
        p.schedule = self.schedule.clone();
        p.context = self.context.clone();
        Ok(p)
    }
}

/// How a jurisdiction's estimated tax and its penalty work, as a tax
/// professional would put it to a client. It is written by hand for each
/// jurisdiction and checked against that jurisdiction's research file.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct Explanation {
    summary: String,
    body: Vec<String>,
    versus_federal: String,
    sources: Vec<String>,
}

impl Explanation {
    /// One or two sentences.
    pub fn summary(&self) -> String {
        self.summary.clone()
    }

    /// A few short paragraphs.
    pub fn body(&self) -> Vec<String> {
        self.body.clone()
    }

    /// Says how the jurisdiction differs from the federal rules. It is empty
    /// for the federal explanation itself.
    pub fn versus_federal(&self) -> String {
        self.versus_federal.clone()
    }

    /// Cites the documents the explanation rests on.
    pub fn sources(&self) -> Vec<String> {
        self.sources.clone()
    }
}

/// Builds an [`Explanation`].
#[derive(Clone, Debug, Default)]
pub struct ExplanationBuilder {
    e: Explanation,
}

/// Starts an explanation with its summary.
pub fn new_explanation(summary: impl Into<String>) -> ExplanationBuilder {
    ExplanationBuilder {
        e: Explanation {
            summary: summary.into(),
            ..Explanation::default()
        },
    }
}

impl ExplanationBuilder {
    /// Adds a paragraph to the body.
    pub fn paragraph(&mut self, p: impl Into<String>) -> &mut Self {
        self.e.body.push(p.into());
        self
    }

    /// Sets how the jurisdiction differs from the federal rules.
    pub fn versus_federal(&mut self, s: impl Into<String>) -> &mut Self {
        self.e.versus_federal = s.into();
        self
    }

    /// Adds a source.
    pub fn source(&mut self, cite: impl Into<String>) -> &mut Self {
        self.e.sources.push(cite.into());
        self
    }

    /// Requires a summary, a body with no empty paragraph, and a source.
    pub fn build(&self) -> Result<Explanation, Error> {
        let e = &self.e;
        if e.summary.is_empty() {
            return Err(invalid!("estimated: an explanation needs a summary"));
        }
        if e.body.is_empty() {
            return Err(invalid!("estimated: an explanation needs a body"));
        }
        if e.sources.is_empty() {
            return Err(invalid!("estimated: an explanation must cite its sources"));
        }
        if e.body.iter().any(String::is_empty) {
            return Err(invalid!("estimated: an explanation has an empty paragraph"));
        }
        Ok(e.clone())
    }
}

/// Words what is left after a subtraction, for a sentence: "leaves $742"
/// when something is due, and "leaves a $136 refund" when the subtraction
/// goes below zero. A refund is never written with a minus sign.
pub fn leaves(left: USCents) -> String {
    if left.0 < 0 {
        return format!("leaves a {} refund", (-left).whole());
    }
    format!("leaves {}", left.whole())
}

/// [`leaves`] in the past tense, for last year: "left $742" or "left a $136
/// refund".
pub fn left(left: USCents) -> String {
    if left.0 < 0 {
        return format!("left a {} refund", (-left).whole());
    }
    format!("left {}", left.whole())
}

/// The effect of asking for this year's tax: while another option sets the
/// target, "Without it, the target can only come from last year's tax.";
/// with none open, "Without it, there's no target."
pub fn without_this_year_tax(other_open: bool) -> String {
    if other_open {
        "Without it, the target can only come from last year's tax.".to_string()
    } else {
        "Without it, there's no target.".to_string()
    }
}
