//! Utah's prepayment rules, Utah Code §§ 59-10-516 and 59-1-401(5), as the
//! shared [`estimated::EstimatedTax`] trait. Ported from estimated.go.
//!
//! Utah has no estimated tax for individuals. Nothing is due during the year,
//! and a return filed and paid by its due date owes no prepayment penalty
//! however little was paid before. The one rule is the extension test: a filer
//! who uses the automatic six-month extension must have paid, by the original
//! due date, the lesser of 90% of this year's tax or 100% of last year's. A
//! filer who did not is charged 2% a month of the tax unpaid on that date,
//! for the months of the extension.
//!
//! So Requirement is that one test, Schedule is that one date, and Penalty is
//! the extension penalty. Interest and the late-payment penalty on a balance
//! paid after the due date are a different charge and are not computed here.
//!
//! The figure follows the Tax Commission's current documents: the TC-546
//! worksheet (Rev. 10/26) for the test, and Publication 58 (Rev. 11/25) for
//! the arithmetic. The result's Divergences say where the Utah Code reads
//! otherwise. No act changed § 59-10-516 after 2021, and the 2026 amendment to
//! § 59-1-401 left subsection (5) as it was (research/estimated-payments/ut.md),
//! so no document here predates the law it describes.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    PartKind, Penalty, Requirement, WaiverKind,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// Utah's postal code.
const JURISDICTION: Jurisdiction = estimated::UTAH;

/// The facts Utah's rules read that no other jurisdiction does. Carry them in
/// [`estimated::FactsBuilder::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    extension: bool,
    extension_answered: bool,
}

impl EstimatedFacts {
    /// Whether the filer will file after the original due date under the
    /// automatic extension, and whether the caller said, as `(extends,
    /// answered)`. The prepayment test applies only then.
    pub fn files_on_extension(&self) -> (bool, bool) {
        (self.extension, self.extension_answered)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts [`EstimatedFacts`] with nothing said.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Answers whether the filer will use the extension. False is an answer:
    /// the return will be filed and paid by the original due date.
    pub fn files_on_extension(&mut self, extends: bool) -> &mut Self {
        self.e.extension = extends;
        self.e.extension_answered = true;
        self
    }

    /// The facts.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// § 59-10-516(2)(b): the share of this year's tax that must be paid by
    /// the due date.
    pub(crate) this_year: Rate,
    /// § 59-10-516(2)(b): the share of last year's liability.
    pub(crate) prior_year: Rate,

    /// The original due date of the return.
    pub(crate) due: Date,
    /// The last day of the automatic extension.
    pub(crate) extension_ends: Date,

    pub(crate) calendar: Calendar,

    /// Publication 58's annual rate, 2% a month, charged by the day.
    pub(crate) penalty: Rate,
    /// The most the penalty can come to, six months of 2%.
    pub(crate) penalty_cap: Rate,
    pub(crate) days_in_year: i64,
}

/// Returns Utah's prepayment rules for a tax year, or
/// [`estimated::Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(UtahEstimated { y }))
}

struct UtahEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
struct Assessment {
    requirement: Requirement,

    /// The test cannot apply: the return was or will be filed and paid by
    /// the due date.
    exempt: bool,

    tax_known: bool,
    tax: USCents,
    due: Date,
    ends: Date,

    /// Withholding and payments made by the due date.
    credited: USCents,

    notes: Notes,
}

/// Go's `%T` for the extras a caller is most likely to pass by mistake.
fn type_name(s: &(dyn Any + Send + Sync)) -> &'static str {
    if s.is::<&str>() || s.is::<String>() {
        "string"
    } else {
        "another jurisdiction's extras"
    }
}

impl UtahEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "utahtaxcalc: the facts carry {}, not utahtaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "utahtaxcalc: these are Utah's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "utahtaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        Ok(())
    }

    /// Where the Tax Commission's documents and the Utah Code part ways on a
    /// rule these computations use. They are found in
    /// research/estimated-payments/ut.md, Conflicts resolved, and checked
    /// there against both. `zero_last`, `no_return` and `short` say which
    /// facts each one turns on.
    fn divergences(
        &self,
        zero_last: bool,
        no_return: bool,
        short: bool,
    ) -> Result<Vec<Divergence>, Error> {
        let mut out = Vec::new();
        for b in [
            working::new_divergence("Last year's tax was zero")
                .form("TC-546 and Publication 58 use last year's tax, so with none the minimum is zero. The 2025 TC-40 instructions ask for 90% of this year's tax instead.",
                    "TC-40 Instructions (2025), Prepayment Requirements for Filing Extension; TC-546 (Rev. 10/26), Payment Worksheet lines 4 and 5; Publication 58 (Rev. 11/25), Insufficient Prepayment")
                .statute("The minimum is the lesser of 90% of this year's tax or 100% of last year's. There is no exception when last year's tax was zero.",
                    "Utah Code § 59-10-516(2)(b)")
                .uses(Reading::Form).affects(zero_last).clone(),
            working::new_divergence("No return last year")
                .form("Publication 58 and the 2025 TC-40 instructions ask for 90% of this year's tax. That applies if you didn't file last year or are filing for the first time.",
                    "Publication 58 (Rev. 11/25), Insufficient Prepayment; TC-40 Instructions (2025), Prepayment Requirements for Filing Extension")
                .statute("The minimum is the lesser of 90% of this year's tax or 100% of last year's. It applies whether or not you filed last year.",
                    "Utah Code § 59-10-516(2)(b)")
                .uses(Reading::Form).affects(no_return).clone(),
            working::new_divergence("By the day or by the month")
                .form("Publication 58 charges 24% a year by the day, up to 12%. It runs from the due date until you file or the extension ends.",
                    "Publication 58 (Rev. 11/25), Insufficient Prepayment (Extension Penalty)")
                .statute("The penalty is 2% of the unpaid tax for each month of the extension. The law doesn't say how part of a month counts.",
                    "Utah Code § 59-1-401(5)(b); TC-546 (Rev. 10/26), Penalties")
                .uses(Reading::Form).affects(short).clone(),
            working::new_divergence("What the penalty is charged on")
                .form("Publication 58 charges the \"underpayment\". That could mean only the amount below the minimum. TC-546 charges the whole tax unpaid on the due date.",
                    "Publication 58 (Rev. 11/25), Insufficient Prepayment (Extension Penalty); TC-546 (Rev. 10/26), Penalties")
                .statute("The penalty is 2% of the return's tax still unpaid on its due date.",
                    "Utah Code §§ 59-1-401(5)(b), 59-10-516(3)")
                .uses(Reading::Statute).affects(short).clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// `d` as a person reads it: "Dec 15" in the tax year, "Apr 15, 2027"
    /// outside it.
    fn date(&self, d: Date) -> String {
        if d.year() == self.y.year {
            return d.format("Jan 2");
        }
        d.format("Jan 2, 2006")
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let last = y.year - 1;
        let due = y.calendar.next_business_day(y.due);
        let ends = y.calendar.next_business_day(y.extension_ends);
        let mut notes = Notes::default();

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        let withheld = f.withheld_total();
        let mut credited = withheld;
        for p in f.payments() {
            if p.from_prior_year() || !p.on().after(due) {
                credited += p.amount();
            }
        }

        // 90% of this year's tax.
        let this_year = if tax_known {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year, y.this_year.of(tax))
                .build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year, USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            notes.rule(
                "Utah Code § 59-10-516(2)(b)(i); TC-546 (Rev. 10/26), Payment Worksheet lines 1 to 3",
                format!(
                    "{} of this year's Utah tax of {} is {}",
                    y.this_year,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // 100% of last year's liability. TC-546 and Publication 58 take it as
        // filed, zero included; Publication 58 refuses it when no return was
        // filed.
        let prior = f.prior_year();
        let unavailable = |why: String| {
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year, USCents(0))
                .unavailable(why)
                .build()
        };
        let prior_opt = match &prior {
            None => unavailable(format!("Needs your {last} Utah return"))?,
            Some(p) if !p.filed() => {
                notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "With no {last} return, Publication 58 and the Utah Code differ on last year's tax. The figures use Publication 58's {} of this year's tax.",
                        y.this_year
                    ),
                );
                unavailable(format!("You didn't file a {last} Utah return"))?
            }
            Some(p) => {
                let prior_tax = p.figures().and_then(|pf| pf.tax()).unwrap_or_default();
                notes.rule(
                    "Utah Code § 59-10-516(2)(b)(ii); TC-546 (Rev. 10/26), Payment Worksheet line 4",
                    format!(
                        "{} of the {last} Utah tax liability of {prior_tax} is {}",
                        y.prior_year,
                        y.prior_year.of(prior_tax)
                    ),
                );
                estimated::new_target_option(
                    Basis::PriorYearTax,
                    y.prior_year,
                    y.prior_year.of(prior_tax),
                )
                .build()?
            }
        };

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if tax_known || prior_opt.available() {
            notes.rule(
                "Utah Code § 59-10-516(2)(b); TC-546 (Rev. 10/26), Payment Worksheet line 5",
                format!(
                    "the minimum due by {} is the lesser of the options, {target}",
                    due.long()
                ),
            );
        }

        let (extends, answered) = extras.files_on_extension();
        let filed = f.return_filed();
        let filed_by_due = !filed.is_zero() && !filed.after(due);
        let plain = "Utah does not require estimated payments.";
        // reach says the minimum meets the test whatever this year's tax is,
        // when it is last year's tax alone.
        let reach = if tax_known {
            String::new()
        } else {
            format!(
                " That is 100% of your {last} tax, which is enough whatever your {} tax is.",
                y.year
            )
        };
        let has_prior = prior.is_some();
        let prior_filed = prior.as_ref().is_some_and(|p| p.filed());
        let mut exempt = false;
        if filed_by_due {
            exempt = true;
            let ex = estimated::new_exception("Return filed and paid by the due date").build()?;
            rb.required(
                false,
                format!(
                    "{plain} You filed and paid your {} return on {}. That is by the {} due date. So the extension prepayment test doesn't apply. There's no prepayment penalty.",
                    y.year,
                    self.date(filed),
                    self.date(due)
                ),
            )
            .exempt(ex);
            notes.rule(
                "Utah Code §§ 59-10-516(2), 59-1-401(5)(a); Publication 58 (Rev. 11/25)",
                format!(
                    "the return was filed and paid on {}, so no extension was used",
                    filed.long()
                ),
            );
        } else if answered && !extends {
            exempt = true;
            let ex = estimated::new_exception("Filing and paying by the due date").build()?;
            rb.required(
                false,
                format!(
                    "{plain} You plan to file and pay your {} return by {}. The extension prepayment test then doesn't apply. There's no prepayment penalty, however little you paid during the year.",
                    y.year,
                    self.date(due)
                ),
            )
            .exempt(ex);
            notes.rule(
                "Utah Code §§ 59-10-516(2), 59-1-401(5)(a)",
                format!(
                    "the return will be filed by {}, so the extension prepayment test does not apply",
                    due.long()
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            let mut measure = format!(
                "the lesser of 90% of your {} Utah tax or 100% of your {last} tax",
                y.year
            );
            if has_prior && !prior_filed {
                measure = format!(
                    "90% of your {} Utah tax, since you didn't file for {last}",
                    y.year
                );
            }
            rb.required(
                false,
                format!(
                    "{plain} To file on the automatic extension, you must pay a minimum by {}. It is {measure}.",
                    self.date(due)
                ),
            )
            .no_target();
        } else if credited >= target {
            rb.required(
                false,
                format!(
                    "{plain} To extend your {} return, have {} withheld or prepaid by {}.{reach} Your withholding and prepayments already come to {}. Nothing more is needed.",
                    y.year,
                    target.whole(),
                    self.date(due),
                    credited.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "{plain} To avoid the extension penalty, pay or withhold {} by {}.{reach} You have {} so far. Prepay {} more by then, or file and pay by then.",
                    target.whole(),
                    self.date(due),
                    credited.whole(),
                    (target - credited).whole()
                ),
            )
            .choose(chosen);
        }
        // Filing by the due date sets the minimum at nothing, so neither
        // figure can change it.
        if !has_prior && !exempt {
            notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Utah return, and what was its tax?"),
                format!(
                    "With it, {} of last year's tax could set a lower extension minimum.",
                    y.prior_year
                ),
            );
        }
        let this_share = y.this_year;
        if !tax_known && !exempt {
            notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Utah tax to be?", y.year),
                format!(
                    "It may set a {this_share} extension minimum, and the extension penalty is charged on it."
                ),
            );
        }
        if !answered && !filed_by_due {
            notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "Will you file your {} return on the automatic extension, after {}?",
                    y.year,
                    self.date(due)
                ),
                "Until you answer, the minimum assumes you will, the only case the test covers.",
            );
        }
        let mut prior_tax = USCents(-1);
        if let Some(p) = &prior
            && p.filed()
            && let Some(t) = p.figures().and_then(|pf| pf.tax())
        {
            prior_tax = t;
        }
        let short = !exempt && credited < target;
        let divergences = self.divergences(
            !exempt && prior_tax.0 == 0,
            !exempt && has_prior && !prior_filed,
            short,
        )?;
        notes.diverges(&divergences);
        let requirement = rb.build()?;
        Ok(Assessment {
            requirement,
            exempt,
            tax_known,
            tax,
            due,
            ends,
            credited,
            notes,
        })
    }

    /// The one date, with what is needed and credited by it.
    fn installment(&self, a: &Assessment) -> Result<Installment, Error> {
        let (mut needed, mut short) = (a.requirement.target(), USCents(0));
        if a.exempt {
            needed = USCents(0);
        } else if a.credited < needed {
            short = needed - a.credited;
        }
        estimated::new_installment(1, a.due)
            .needed_by(needed)
            .credited_by(a.credited)
            .shortfall(short)
            .build()
    }
}

impl EstimatedTax for UtahEstimated {
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, Error> {
        let a = self.assess(f)?;
        Ok(working::new_result(a.requirement).with(&a.notes).build()?)
    }

    /// The one date: the original due date of the return.
    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, Error> {
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Utah return to measure against, there is no minimum to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let i = self.installment(&a)?;
        let mut notes = Notes::default();
        notes.rule(
            "TC-40 Instructions (2025), When to File and Pay and Prepayment Requirements for Filing Extension; TC-546 (Rev. 10/26)",
            format!(
                "Utah has no installments; the whole minimum is due on the original due date, {}, and withholding of {} counts toward it whenever it was withheld",
                a.due.long(),
                f.withheld_total()
            ),
        );
        Ok(working::new_result(vec![i])
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    /// The extension penalty: 2% a month, by the day, of the tax unpaid on
    /// the due date, from then until the return is filed or the extension
    /// ends.
    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "utahtaxcalc: a penalty needs the date to figure it to".to_string(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Utah return to measure against, there is no minimum to figure a penalty from",
                    self.y.year - 1
                ),
            ));
        }
        let y = &self.y;
        let filed = f.return_filed();
        if !filed.is_zero() && filed.after(a.ends) {
            return Err(Error::with_detail(
                Error::Unsupported,
                format!(
                    "a return filed on {} is after the extension ended on {}, so the late-filing and late-payment penalties of Utah Code § 59-1-401(6) apply instead, and they are not built",
                    filed.long(),
                    a.ends.long()
                ),
            ));
        }
        let mut stop = as_of;
        if a.ends.before(stop) {
            stop = a.ends;
        }
        if !filed.is_zero() && filed.before(stop) {
            stop = filed;
        }
        let waiver_answer = f.positions().waiver();
        let waiver = waiver_answer.unwrap_or_default();
        let i = self.installment(&a)?;
        let schedule = vec![i];
        let mut notes = Notes::default();
        let empty = |notes: &Notes| -> Result<working::Result<Penalty>, Error> {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            Ok(working::new_result(p).with(&a.notes).with(notes).build()?)
        };
        if a.exempt {
            return empty(&notes);
        }
        if a.credited >= a.requirement.target() {
            notes.rule(
                "Utah Code § 59-10-516(2)(b); TC-546 (Rev. 10/26)",
                format!(
                    "{} was paid by {}, which meets the {} minimum, so there is no extension penalty",
                    a.credited,
                    a.due.long(),
                    a.requirement.target()
                ),
            );
            return empty(&notes);
        }
        if !a.tax_known {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "the {} paid by {} is under the minimum, and the extension penalty is charged on the whole tax then unpaid, which needs this year's tax",
                    a.credited,
                    a.due.long()
                ),
            ));
        }
        let unpaid = a.tax - a.credited;
        if !stop.after(a.due) {
            notes.rule(
                "Utah Code § 59-1-401(5)(b); Publication 58 (Rev. 11/25)",
                format!(
                    "the penalty is figured to {}, which is not after the {} due date, so nothing has accrued",
                    stop.long(),
                    a.due.long()
                ),
            );
            return empty(&notes);
        }
        let days = stop.days_since(a.due);
        let mut charge = daily_charge(unpaid, y.penalty, days, y.days_in_year);
        let capped = y.penalty_cap.of(unpaid);
        let source = "Publication 58 (Rev. 11/25), Insufficient Prepayment (Extension Penalty): 2 percent per month, annual 24 percent, at most 12 percent";
        if charge > capped {
            charge = capped;
            notes.rule(
                "Publication 58 (Rev. 11/25), Insufficient Prepayment (Extension Penalty)",
                format!(
                    "{days} days at {} a year would be more than six months of 2%, so the penalty is capped at {} of {unpaid}, {capped}",
                    y.penalty, y.penalty_cap
                ),
            );
        }
        let part = estimated::new_penalty_part(PartKind::Interest, unpaid, y.penalty)
            .span(a.due, stop)
            .days(days)
            .charge(charge)
            .source(source)
            .build()?;
        let line = estimated::new_penalty_line(1, a.due, unpaid)
            .days(days)
            .part(part)
            .build()?;
        let mut p = estimated::new_penalty(stop)
            .waiver(waiver)
            .line(line)
            .because(a.requirement.clone(), &schedule)
            .build()?;
        notes.rule(
            "Utah Code §§ 59-10-516(3), 59-1-401(5)(b); Publication 58 (Rev. 11/25)",
            format!(
                "{} paid by {} misses the {} minimum, so the whole {unpaid} of tax then unpaid is charged {} a year for the {days} days to {}, {}; \
                 a payment after the due date does not shorten it, because the penalty runs until the return is filed or the extension ends",
                a.credited,
                a.due.long(),
                a.requirement.target(),
                y.penalty,
                stop.long(),
                p.total()
            ),
        );
        if filed.is_zero() && as_of.after(a.ends) {
            notes.rule(
                "Utah Code § 59-1-401(6); Publication 58 (Rev. 11/25)",
                format!(
                    "the penalty is figured to {}, the end of the extension; a return not filed by then loses the extension, \
                     and the late-filing and late-payment penalties replace this one",
                    a.ends.long()
                ),
            );
        }
        if waiver_answer.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Will you ask the Tax Commission for a waiver or reduction for reasonable cause?",
                "The penalty shown is before any waiver.",
            );
        }
        if waiver_answer.is_some() && waiver != WaiverKind::None && p.total().0 > 0 {
            // § 59-1-401(14) and Publication 58 give one waiver, for
            // reasonable cause, and name no casualty, retirement or
            // disability waiver.
            let mut what = "a casualty or disaster";
            if waiver == WaiverKind::RetiredOrDisabled {
                what = "retirement or disability";
            }
            let o = estimated::waiver_undecided(format!(
                "Utah has no waiver for {what} as such. The Tax Commission can waive or reduce the penalty for reasonable cause shown."
            ));
            p = p.to_builder().waiver_outcome(o).build()?;
            notes.rule(
                "Utah Code § 59-1-401(14); Publication 58 (Rev. 11/25), Waivers",
                "the only waiver is the Tax Commission's for reasonable cause shown",
            );
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "The extension penalty can be waived for reasonable cause. Utah's law and Publication 58 don't say if {what} counts."
                ),
            );
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        UTAH_EXPLANATION.clone()
    }
}

/// principal × rate × days ÷ days in the year, rounded half up to the cent:
/// Publication 58's formula.
fn daily_charge(principal: USCents, rate: Rate, days: i64, year: i64) -> USCents {
    let n = principal.0 * i64::from(rate.0) * days;
    let d = 10_000 * year;
    USCents((n + d / 2) / d)
}

/// Written by hand and checked against research/estimated-payments/ut.md.
static UTAH_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Utah has no estimated tax, and nothing is due during the year. The only prepayment rule is for a return filed late on an extension. Utah's extension is automatic and lasts six months.")
        .paragraph("A 2026 Utah return filed and paid by April 15, 2027 has no prepayment penalty. That holds however little was withheld or paid during the year. You can still prepay at any time if you want to.")
        .paragraph("Utah gives six more months to file without asking, to October 15, 2027. But by April 15 your withholding and prepayments must reach a minimum. It is the lesser of 90% of your 2026 Utah tax or 100% of your 2025 Utah tax. Withholding counts in full whenever it was taken. If you filed for 2025 with no tax due, the minimum is nothing. If you didn't file at all, the Tax Commission asks for 90% of this year's tax.")
        .paragraph("Miss the minimum and the penalty is steep. It is 2% a month of all the tax still unpaid on April 15. It is worked out by the day until you file, for up to six months, or 12%. It is charged on the whole unpaid balance, not just the shortfall. The late-payment penalty and interest on unpaid tax are added from April 15. If you do not file by October 15, the extension is lost. The late-filing and late-payment penalties then apply instead. The Tax Commission can waive a penalty for reasonable cause.")
        .versus_federal("Utah differs from federal in kind. There are no quarterly payments and no quarterly penalty. A return filed by April 15 has no prepayment penalty at all. The one test is on April 15, for an extended return. It has no 110% tier for high income. The penalty is 2% a month of everything unpaid, not interest on each shortfall. So a small shortfall can bring a large penalty.")
        .source("Utah Code §§ 59-10-514, 59-10-516, 59-1-401")
        .source("TC-40, Utah Individual Income Tax Instructions, 2025")
        .source("TC-546, Individual Income Tax Prepayment Coupon (Rev. 10/26)")
        .source("Publication 58, Utah Interest and Penalties (Rev. 11/25)")
        .build()
        .unwrap_or_default()
});
