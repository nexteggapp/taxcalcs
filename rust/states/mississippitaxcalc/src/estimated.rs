//! Mississippi's estimated tax rules, Miss. Code § 27-7-327 and 35 Miss.
//! Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100, as the shared
//! [`EstimatedTax`] interface.
//!
//! Mississippi differs from the federal rules in shape. The current-year test
//! is 80%, and the prior-year test is 100% with no higher tier. All
//! withholding counts toward the April installment. The charge is interest of
//! ½% a month on the running underestimate, column by column, from each due
//! date to the next or until paid, and the fourth column runs to April 15.
//!
//! The figure follows Form 80-320 (Rev. 09/25), the latest published; no 2026
//! edition is posted. 2025 House Bill 1 changed the tax rates for 2026 and not
//! the estimated tax sections, so the form does not predate the law it
//! describes (research/estimated-payments/ms.md).
//!
//! Not reported in Required, because no 2026 figure turns on them: no source
//! says whether a due date on a weekend or holiday moves, and every 2026 date
//! and the April 15, 2027 end are weekdays; and no source states a January 31
//! file-and-pay rule, so there is none here.

use std::any::Any;

use estimated::{
    Basis, Date, Error, EstimatedTax, Explanation, Facts, Installment, PartKind, Payment, Penalty,
    PenaltyPart, Requirement, TargetOption, TargetOptionBuilder, WaiverKind,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// Mississippi's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::MISSISSIPPI;

/// The facts Mississippi's rules read that no other jurisdiction does. Carry
/// them in [`estimated::FactsBuilder::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    part_month_whole: bool,
    part_month_answered: bool,
    prior_income_tax: Option<USCents>,
}

impl EstimatedFacts {
    /// How a span shorter than a whole number of months is charged, and
    /// whether the caller said: `Some(true)` counts the part month as a whole
    /// one, `Some(false)` counts only the months completed. § 27-7-327
    /// charges "per month" and says neither.
    pub fn part_month_counts_whole(&self) -> Option<bool> {
        self.part_month_answered.then_some(self.part_month_whole)
    }

    /// The tax on last year's income at this year's Mississippi rates and
    /// exemptions, which Rule 100(3) uses as the prior-year test for a filer
    /// who was not required to file a resident return last year. Giving it
    /// applies that test.
    pub fn prior_year_income_at_this_years_rates(&self) -> Option<USCents> {
        self.prior_income_tax
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts Mississippi's extras.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Answers how a part month is charged.
    pub fn part_month_counts_whole(&mut self, whole: bool) -> &mut Self {
        self.e.part_month_whole = whole;
        self.e.part_month_answered = true;
        self
    }

    /// Gives the tax on last year's income at this year's rates.
    pub fn prior_year_income_at_this_years_rates(&mut self, a: USCents) -> &mut Self {
        self.e.prior_income_tax = Some(a);
        self
    }

    /// Refuses a negative tax on last year's income.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        if self.e.prior_income_tax.is_some_and(|a| a.0 < 0) {
            return Err(Error::Invalid(
                "mississippitaxcalc: the tax on last year's income cannot be negative".into(),
            ));
        }
        Ok(self.e)
    }
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// Form 80-320 line 1: no interest when the year's liability is this or
    /// less.
    pub(crate) threshold: USCents,

    /// Form 80-320 lines 2 and 4.
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,

    /// Form 80-320 line 5, and `withholding_shares` line 6: all withholding
    /// in column A.
    pub(crate) shares: Vec<Rate>,
    pub(crate) withholding_shares: Vec<Rate>,

    pub(crate) dues: Vec<Date>,

    /// The end of column D.
    pub(crate) stop: Date,

    /// The § 27-7-327(e) rate per month.
    pub(crate) monthly: Rate,
    pub(crate) rate_source: &'static str,
}

/// Returns Mississippi's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026(),
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(MississippiEstimated { y }))
}

struct MississippiEstimated {
    y: EstimatedYear,
}

#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    installments: Vec<USCents>,
    extras: EstimatedFacts,

    /// Form 80-320 line 4 read with a zero line 3.
    zero_line3: bool,
    /// Rule 100(3)'s tax on last year's income, as given.
    prior_income: bool,

    notes: Notes,
}

/// Go's `%T` of a jurisdiction's extras, for the types a caller is likely to
/// pass by mistake.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<String>() || v.is::<&str>() {
        "string"
    } else if v.is::<i64>() || v.is::<i32>() {
        "int"
    } else {
        "unknown"
    }
}

impl MississippiEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "mississippitaxcalc: the facts carry {}, not mississippitaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "mississippitaxcalc: these are Mississippi's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "mississippitaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "farmers and fishermen owe no interest if 80% is paid by January 15 or the return is filed and paid by March 1, which is not built",
            ));
        }
        Ok(())
    }

    /// Where the department's form, its rule and the statute part ways on a
    /// rule every Mississippi computation uses. They are in the research
    /// file, checked there against all three. Each says whether it changes
    /// anything for `f`.
    fn divergences(&self, f: &Facts, a: &Assessment) -> Result<Vec<Divergence>, Error> {
        let (mut short, mut prior_chosen) = (false, false);
        if !a.exempt && a.requirement.target_known() {
            let (credits, _) = self.credits(f)?;
            let ledger = estimated::apply(&self.y.dues, &a.installments, &credits)?;
            for l in &ledger {
                short = short || l.shortfall().0 > 0;
            }
            let c = a.requirement.chosen().unwrap_or_default();
            prior_chosen = c.basis() != Basis::ThisYearTax;
        }
        let zero_reading = if a.prior_income {
            Reading::Statute
        } else {
            Reading::Form
        };
        let mut out = Vec::new();
        for b in [
            working::new_divergence("The interest rate")
                .form("Interest is ½% a month.",
                    "Form 80-320 (Rev. 09/25), line 9; Form 80-100 (2025), Interest and Penalty Provisions")
                .statute("The statute sets ½% a month for taxes assessed from Jan 1, 2019. The department's rule still prints the earlier 1% a month.",
                    "Miss. Code § 27-7-327(e), as amended by Laws 2014, H.B. 799 § 7; 35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(1)")
                .uses(Reading::Form)
                .affects(short)
                .clone(),
            working::new_divergence("The prior-year test")
                .form("You need the lesser of 80% of this year's liability and 100% of last year's. A quarter is due by each date.",
                    "Form 80-320 (Rev. 09/25), lines 1 to 5")
                .statute("The statute requires estimates of at least 80% of the tax actually due. It has no prior-year test. The department's rule instead has a prior-year test. It charges no interest when the year's payments reach last year's liability. That liability must be on a filed twelve-month return.",
                    "Miss. Code § 27-7-327; 35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(3)")
                .uses(Reading::Form)
                .affects(prior_chosen)
                .clone(),
            working::new_divergence("Last year's liability was zero")
                .form("If last year's liability is zero, line 4 depends on your estimated payments. With none, it takes 80% of this year's liability. With any, it takes the lesser of the two, which is zero.",
                    "Form 80-320 (Rev. 09/25), Line 4 instructions")
                .statute("If no resident return was required last year, the department's rule differs. It measures payments against the tax on last year's income. That tax uses this year's rates and exemptions. The statute has no prior-year test.",
                    "35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(3); Miss. Code § 27-7-327")
                .uses(zero_reading)
                .affects(!a.exempt && (a.zero_line3 || a.prior_income))
                .clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// The requirement and installments, with the divergences.
    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let mut a = self.measure(f)?;
        let divergences = self.divergences(f, &a)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// Form 80-320 line 4 when line 3 is zero. `why_not` says why last year
    /// is zero, for a person; `why` says it for the working.
    fn zero_rule(
        &self,
        a: &mut Assessment,
        paid_estimates: bool,
        why_not: &str,
        why: &str,
    ) -> TargetOptionBuilder {
        a.zero_line3 = true;
        if !paid_estimates {
            let mut b = estimated::new_target_option(
                Basis::PriorYearTax,
                self.y.prior_year_share,
                USCents(0),
            );
            b.unavailable(format!(
                "{why_not}. With no estimated payments, this year's figure is used"
            ));
            return b;
        }
        a.notes.rule(
            "Form 80-320 (Rev. 09/25), Line 4 instructions",
            format!("{why} and estimated payments were made, so line 4 is the lesser of line 2 and zero, which is zero"),
        );
        estimated::new_target_option(Basis::PriorYearTax, self.y.prior_year_share, USCents(0))
    }

    fn measure(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment {
            extras,
            ..Assessment::default()
        };
        let last = y.year - 1;

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let prior = prior.unwrap_or_default();
        let prior_tax = prior.figures().and_then(|pf| pf.tax()).unwrap_or_default();
        let paid_estimates = f.payments().iter().any(|p| p.amount().0 > 0);

        let this_year: TargetOption = if tax_known {
            estimated::new_target_option(
                Basis::ThisYearTax,
                y.this_year_share,
                y.this_year_share.of(tax),
            )
            .build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year_share, USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            a.notes.rule(
                "Miss. Code § 27-7-327; Form 80-320 (Rev. 09/25), line 2",
                format!(
                    "{} of this year's Mississippi liability of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        let unavailable = |basis: Basis, why: String| {
            let mut b = estimated::new_target_option(basis, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        let prior_option = if !has_prior {
            unavailable(
                Basis::PriorYearTax,
                format!("Needs your {last} Mississippi return"),
            )
        } else if prior.not_required_to_file() {
            if let Some(amount) = extras.prior_year_income_at_this_years_rates() {
                a.prior_income = true;
                a.notes.rule(
                    "35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(3)",
                    format!(
                        "no {last} resident return was required, so the prior-year test is the tax on {last} income at {} rates and exemptions, {amount}, as given",
                        y.year
                    ),
                );
                estimated::new_target_option(Basis::PriorYearIncome, y.prior_year_share, amount)
            } else {
                let b = self.zero_rule(
                    &mut a,
                    paid_estimates,
                    &format!("You didn't have to file a {last} Mississippi return"),
                    &format!("no {last} return was required, so line 3 is zero"),
                );
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "What is the tax on your {last} income at {} Mississippi rates and exemptions?",
                        y.year
                    ),
                    format!(
                        "You didn't have to file a {last} resident return. So the department uses that tax as the last-year test."
                    ),
                );
                b
            }
        } else if !prior.filed() {
            unavailable(
                Basis::PriorYearTax,
                format!("You didn't file a {last} Mississippi return"),
            )
        } else if prior.months() != 12 {
            unavailable(
                Basis::PriorYearTax,
                format!(
                    "Your {last} Mississippi return covered {} months, not twelve",
                    prior.months()
                ),
            )
        } else if prior_tax.0 == 0 {
            self.zero_rule(
                &mut a,
                paid_estimates,
                &format!("Your {last} Mississippi return showed no liability"),
                &format!("the {last} return showed no liability, so line 3 is zero"),
            )
        } else {
            let amount = y.prior_year_share.of(prior_tax);
            a.notes.rule(
                "Form 80-320 (Rev. 09/25), lines 3 and 4; 35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(3)",
                format!(
                    "the prior-year test is {} of the {} liability on the {last} return, or {amount}",
                    y.prior_year_share, prior_tax
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, amount)
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (prior_opt.basis(), prior_opt.amount());
        }
        if !tax_known {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!(
                    "What do you expect your {} Mississippi liability to be?",
                    y.year
                ),
                format!(
                    "It sets the {} option and the {} test.",
                    y.this_year_share,
                    y.threshold.whole()
                ),
            );
        }

        if tax_known && tax <= y.threshold {
            a.exempt = true;
            let small = estimated::exception_at_most("Mississippi liability", tax, y.threshold)?;
            rb.required(
                false,
                format!(
                    "Your Mississippi liability of {} is {} or less. So no estimated payments are needed.",
                    tax.whole(),
                    y.threshold.whole()
                ),
            )
            .exempt(small);
            a.notes.rule(
                "Form 80-320 (Rev. 09/25), Line 1 instructions; Form 80-106 (Rev. 09/25)",
                format!(
                    "the liability of {} is {} or less, so no interest is due",
                    tax, y.threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "A target needs your {} Mississippi liability or a full-year {last} return. There is no target yet.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} already reaches your {last} figure, {}. All of it counts toward Apr 15. No estimated payments are needed, whatever your {} liability turns out to be.",
                    withheld.whole(),
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is your {last} figure, {}. All {} of withholding counts toward Apr 15. That leaves {} of estimated payments, whatever your {} liability is.",
                    target.whole(),
                    withheld.whole(),
                    (target - withheld).whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Mississippi withholding of {} already reaches the {} target. All of it counts toward Apr 15, so no estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Mississippi liability of {} is over {}. Withholding of {} falls short of the {} target. You need {} of estimated payments. All withholding counts toward the Apr 15 installment, a quarter of the target.",
                    tax.whole(),
                    y.threshold.whole(),
                    withheld.whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        // Under an exception last year's return can't change the target.
        if !has_prior && !a.exempt {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Mississippi return, and what was its liability?"),
                format!(
                    "With it, last year's liability could be a lower target than {} of this year's.",
                    y.this_year_share
                ),
            );
        }

        let cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(2); Form 80-320 (Rev. 09/25), line 5",
            format!(
                "a quarter of {}, {}, is due on each of {}, {}, {} and {}",
                a.requirement.target(),
                a.installments[0],
                y.dues[0].long(),
                y.dues[1].long(),
                y.dues[2].long(),
                y.dues[3].long()
            ),
        );
        if f.elections().annualised() {
            a.notes.rule(
                "35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100; Form 80-320 (Rev. 09/25)",
                "Mississippi has no annualized income method for individuals, so the four equal installments are used",
            );
        }
        if a.exempt {
            a.installments = vec![USCents(0); y.dues.len()];
        }
        Ok(a)
    }

    /// Withholding and payments, dated as Form 80-320 line 6 counts them,
    /// earliest first.
    fn credits(&self, f: &Facts) -> Result<(Vec<Payment>, Notes), Error> {
        let mut notes = Notes::default();
        let mut credits = if f.elections().withholding_as_withheld() {
            let c = estimated::credits_as_withheld(f, &self.y.dues);
            notes.rule(
                "Form 80-320 (Rev. 09/25), Line 6 instructions",
                format!(
                    "as the caller answered, the {} withheld is counted on the dates it was withheld",
                    f.withheld_total()
                ),
            );
            c
        } else {
            let c = estimated::credits(f, &self.y.dues, &self.y.withholding_shares);
            notes.rule(
                "Form 80-320 (Rev. 09/25), line 6",
                format!(
                    "the {} withheld is all counted in column A, on {}, whenever it was withheld",
                    f.withheld_total(),
                    self.y.dues[0].long()
                ),
            );
            if f.withholding()
                .iter()
                .any(|w| w.on().after(self.y.dues[0]) && w.amount().0 > 0)
            {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    "Do you want your withholding after Apr 15 counted on the dates it was withheld?",
                    "Only Form 80-320 covers it, so the figures count all withholding toward Apr 15.",
                );
            }
            c
        }?;
        credits.sort_by_key(|c| c.on().days());
        Ok((credits, notes))
    }

    /// The installments after the credits, with the credits they come from.
    /// A due date before the requirement was met is excused.
    fn schedule_with(
        &self,
        f: &Facts,
        a: &Assessment,
    ) -> Result<(Vec<Installment>, Vec<Payment>, Notes), Error> {
        let (credits, notes) = self.credits(f)?;
        let ledger = estimated::apply(&self.y.dues, &a.installments, &credits)?;
        let first_met = f.requirement_first_met();
        let mut out = Vec::with_capacity(ledger.len());
        for (i, l) in ledger.iter().enumerate() {
            let mut b = estimated::new_installment(i as i64 + 1, l.due());
            b.needed_by(l.needed_by())
                .credited_by(l.credited_by())
                .shortfall(l.shortfall());
            if !first_met.is_zero() && first_met.after(l.due()) {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok((out, credits, notes))
    }
}

/// Counts the whole months from `from` to `to`, leaving out a part month at
/// the end.
fn completed_months(from: Date, to: Date) -> i64 {
    let mut k = 0;
    while !from.add_months(k + 1).after(to) {
        k += 1;
    }
    k
}

impl EstimatedTax for MississippiEstimated {
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, Error> {
        let a = self.assess(f)?;
        Ok(working::new_result(a.requirement).with(&a.notes).build()?)
    }

    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, Error> {
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Mississippi return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (out, _, notes) = self.schedule_with(f, &a)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "mississippitaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Mississippi return to measure against, there is no target to figure the interest from",
                    self.y.year - 1
                ),
            ));
        }
        let y = &self.y;
        let mut stop = as_of;
        if y.stop.before(stop) {
            stop = y.stop;
        }
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(stop) {
            stop = filed;
        }
        let waiver_asked = f.positions().waiver();
        let waiver = waiver_asked.unwrap_or_default();
        let (sched, credits, mut notes) = self.schedule_with(f, &a)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let answered = a.extras.part_month_counts_whole();
        let whole = answered.unwrap_or(true);
        let mut part_month = false;
        let mut charge = |principal: USCents, from: Date, to: Date| -> Result<PenaltyPart, Error> {
            let mut months = completed_months(from, to);
            if estimated::months_or_part(from, to) != months {
                part_month = true;
                if whole {
                    months = estimated::months_or_part(from, to);
                }
            }
            let n = principal.0 * i64::from(y.monthly.0) * months;
            let c = USCents((n + 5_000) / 10_000);
            estimated::new_penalty_part(PartKind::Monthly, principal, y.monthly)
                .span(from, to)
                .months(months)
                .charge(c)
                .source(y.rate_source)
                .build()
        };

        // Form 80-320 lines 5 to 10: the running underestimate on each due
        // date, charged until paid or the next due date, whichever is
        // earlier.
        let first_met = f.requirement_first_met();
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &sched);
        let mut sum = USCents(0);
        let required: Vec<USCents> = a.installments[..y.dues.len()]
            .iter()
            .map(|i| {
                sum += *i;
                sum
            })
            .collect();
        let cols = estimated::columns(&y.dues, &required, &credits, y.stop, stop)?;
        let mut early = false;
        for col in &cols {
            let due = col.due();
            if col.under().0 <= 0 {
                continue;
            }
            if !first_met.is_zero() && first_met.after(due) {
                notes.rule(
                    "Form 80-320 (Rev. 09/25), Exceptions; Form 80-100 (2025)",
                    format!(
                        "the filing requirement was met on {}, after the {} due date, so no interest is computed in that column",
                        first_met.long(),
                        due.long()
                    ),
                );
                continue;
            }
            let mut lb = estimated::new_penalty_line(col.number(), due, col.under());
            for piece in col.pieces() {
                let to = piece.paid_on().unwrap_or_else(|| col.end());
                lb.part(charge(piece.amount(), due, to)?);
            }
            let line = lb.days(col.through().days_since(due)).build()?;
            if line.total().0 > 0 && col.number() < y.dues.len() as i64 {
                early = true;
            }
            pb.line(line);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            notes.rule(
                "Form 80-320 (Rev. 09/25), line 4",
                format!(
                    "this year's Mississippi liability was not given, so the interest is figured against the {} figure alone; \
                     the required amount is the lesser of that and 80% of this year's liability, so {} is the most the interest can be, and it is less if this year's liability turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        notes.rule(
            "Miss. Code § 27-7-327(e); Form 80-320 (Rev. 09/25), lines 5 to 11",
            format!(
                "each column's running underestimate is charged {} a month from its due date until paid or the next due date, whichever is earlier, and column D runs to {}; \
                 the interest to {} is {}",
                y.monthly,
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if part_month && answered.is_none() {
            notes.ask(
                PositionKind::JurisdictionFact,
                "Should interest for part of a month be charged as a whole month?",
                "The law charges per month and says nothing about part months. The figures count a part month as whole, as other Mississippi interest rules do.",
            );
        }
        if early && first_met.is_zero() {
            notes.ask(
                PositionKind::RequirementFirstMet,
                format!(
                    "When did you first have to make estimated payments for {}?",
                    y.year
                ),
                "Interest runs only from the due dates after that.",
            );
        }
        if waiver_asked.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "Form 80-320 (Rev. 09/25); Form 80-100 (2025)",
                "Mississippi's published estimated tax rules have no waiver for casualty, retirement or disability; the interest is shown in full",
            );
            // The research found no waiver and no rule refusing one.
            if p.total().0 > 0 {
                let o = estimated::waiver_undecided(
                    "Mississippi publishes no retirement, disability, casualty or disaster waiver. Only the Department of Revenue can say whether it will waive the interest.",
                );
                p = p.to_builder().waiver_outcome(o).build()?;
                notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    "Interest might be waived for retirement, disability, casualty or disaster. No Mississippi statute, rule or form says, so it is shown in full.",
                );
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        mississippi_explanation()
    }
}

/// Written by hand and checked against research/estimated-payments/ms.md.
fn mississippi_explanation() -> Explanation {
    estimated::new_explanation(
        "Mississippi expects estimated payments when your year's liability is over $200. None are expected if withholding covers 80% or more of that liability. Falling short costs interest of ½% a month.",
    )
    .paragraph("The target is 80% of this year's Mississippi liability or 100% of last year's. It is whichever is lower. You are clear if withholding and estimated payments reach it. A quarter is due by each of April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. If last year's liability was zero, the form uses 80% of this year's instead. That holds only if you made no estimated payments.")
    .paragraph("All your withholding counts toward the April payment, whenever it was taken. So if withholding covers the year's requirement, nothing is due. Withholding taken late in the year is credited early.")
    .paragraph("The interest is figured column by column on Form 80-320. On each due date, any amount still short is charged ½% a month. That includes any shortfall carried from earlier dates. The charge runs until the next due date or until you pay it. The last column runs from January 15 to April 15. A liability of $200 or less has no interest due.")
    .paragraph("Payments may first be required partway through the year. Then interest is figured only from the due dates after that. There is no annualized method and no relief for filing by January 31. There is no published waiver for retirement, disability or a casualty.")
    .versus_federal("Mississippi's current-year test is 80%, not 90%. The prior-year test is always 100%. The threshold is $200 of liability, not $1,000 after withholding. All withholding counts toward the first installment. The charge is a flat ½% a month on the running shortfall. The federal charge is a daily rate on each installment.")
    .source("Miss. Code § 27-7-327, as amended by Laws 2014, H.B. 799 § 7")
    .source("35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100")
    .source("Form 80-320, Interest and Penalty Worksheet, and its instructions and example (Rev. 09/25)")
    .source("Form 80-106 (Rev. 09/25) and Form 80-100, 2025 Resident Individual Income Tax Instructions")
    .build()
    .unwrap_or_default()
}
