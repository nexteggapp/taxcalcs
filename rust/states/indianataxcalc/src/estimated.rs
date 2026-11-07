//! Indiana's estimated tax rules, IC 6-3-4-4.1 and IC 6-8.1-10-2.1(b), as
//! the shared [`EstimatedTax`] trait.
//!
//! IC 6-3-4-4.1(a) has the payments made as 26 U.S.C. § 6654 provides, so
//! the tests look federal: 90% of this year's tax or 100% of last year's,
//! 110% above $150,000 of federal AGI, four quarters, nothing under $1,000.
//! The charge does not. It is a flat 10% of each quarter's shortfall, so a
//! payment one day late costs as much as one made months late, and making up
//! a shortfall later does not reduce it. County income tax is collected with
//! the state's and counts in every figure.
//!
//! The figure follows the Department of Revenue's forms: Form ES-40 for
//! 2026, and Schedules IT-2210 and IT-2210A for 2025, the latest published.
//! Where the forms and the Indiana Code differ the result's Divergences say
//! so. The 2025 IT-2210 predates IC 6-3-4-4.1(a)(3), effective January 1,
//! 2026, so that rule is computed from the law.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Figures, Installment,
    Jurisdiction, Month, PartKind, Payment, Penalty, Requirement, WaiverKind,
};
use filing::Status;
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

const JURISDICTION: Jurisdiction = estimated::INDIANA;

/// The facts Indiana's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    ptet: Option<USCents>,
    prior_annualised: Option<USCents>,
    prior_separate: Option<bool>,
    twelve_month: Option<bool>,
}

impl EstimatedFacts {
    /// This year's credit for pass-through entity tax paid on the filer's
    /// behalf, Schedule 5 line 3, or `None` when not given. IT-2210 line 5
    /// counts it with withholding, in four equal parts, and
    /// [`Figures::tax`] is before it.
    pub fn pass_through_entity_tax_credit(&self) -> Option<USCents> {
        self.ptet
    }

    /// Last year's tax worked out on a full-year basis, for a filer who was
    /// a part-year Indiana resident last year: the IT-2210 line 7 figure
    /// that its part-year example reaches in four steps. When given it is
    /// used as last year's tax.
    pub fn prior_year_annualised_tax(&self) -> Option<USCents> {
        self.prior_annualised
    }

    /// Whether last year's return was married filing separately, or `None`
    /// when the caller did not say. IT-2210's CAUTION box tests the $75,000
    /// step on last year's filing status.
    pub fn prior_year_married_separately(&self) -> Option<bool> {
        self.prior_separate
    }

    /// Whether the caller wants the prior-year test refused for a return
    /// that covered fewer than twelve months, or `None` when the caller did
    /// not say. Indiana's forms state no such condition; 26 U.S.C. § 6654,
    /// which IC 6-3-4-4.1(a) applies, has one. Which reading holds is
    /// unsettled.
    pub fn twelve_month_rule(&self) -> Option<bool> {
        self.twelve_month
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts an [`EstimatedFacts`] with nothing answered.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Sets the pass-through entity tax credit.
    pub fn pass_through_entity_tax_credit(&mut self, a: USCents) -> &mut Self {
        self.e.ptet = Some(a);
        self
    }

    /// Sets last year's tax on a full-year basis.
    pub fn prior_year_annualised_tax(&mut self, a: USCents) -> &mut Self {
        self.e.prior_annualised = Some(a);
        self
    }

    /// Answers whether last year's return was married filing separately.
    pub fn prior_year_married_separately(&mut self, separate: bool) -> &mut Self {
        self.e.prior_separate = Some(separate);
        self
    }

    /// Answers whether the twelve-month rule applies.
    pub fn twelve_month_rule(&mut self, applies: bool) -> &mut Self {
        self.e.twelve_month = Some(applies);
        self
    }

    /// Refuses a negative credit or tax.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        let e = self.e;
        if e.ptet.unwrap_or_default().0 < 0 || e.prior_annualised.unwrap_or_default().0 < 0 {
            return Err(Error::Invalid(
                "indianataxcalc: a credit or tax cannot be negative".into(),
            ));
        }
        Ok(e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// IT-2210 line 6: no penalty when tax less withholding and the
    /// pass-through credit is under it.
    pub(crate) threshold: USCents,

    /// The IT-2210 line 4 share of this year's tax.
    pub(crate) this_year: Rate,
    /// The line 7 share of last year's.
    pub(crate) prior: Rate,
    /// The line 7 share above the step.
    pub(crate) prior_high: Rate,

    /// The prior-year federal AGI above which the line 7 share is
    /// `prior_high`.
    pub(crate) step: USCents,
    /// The same, married filing separately.
    pub(crate) step_separate: USCents,

    /// The flat penalty on each underpaid installment, IT-2210 line 21.
    pub(crate) rate: Rate,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// The IB3 date by which a return filed and paid in full excuses the
    /// fourth installment, before moving for a weekend.
    pub(crate) file_and_pay_by: Date,

    /// The ends of the installment periods of IT-2210 line 14 and the
    /// IT-2210A columns, which are the same.
    pub(crate) period_ends: Vec<Date>,

    /// The IT-2210A line 11 percentages.
    pub(crate) annualised: Vec<Rate>,

    /// IC 6-3-4-4.1(a)(3), in force from 2026.
    pub(crate) zero_prior_tax: bool,
}

/// IT-2210 lines 14 and 15: the required annual payment and the withholding
/// are each divided by four.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// Returns Indiana's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(IndianaEstimated { y }))
}

struct IndianaEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,

    /// IT-2210 line 8: withholding and the pass-through credit reach the
    /// required annual payment, so there is no penalty whenever the
    /// withholding came.
    covered: bool,

    dues: Vec<Date>,
    /// Each installment's own amount, before excusing.
    installments: Vec<USCents>,
    /// The fourth installment, for filing early.
    excused: bool,
    ptet: USCents,

    notes: Notes,
}

/// Go's `%T` for the extras a caller passed, as far as Rust can tell it.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else if v.is::<i64>() {
        "int"
    } else if v.is::<bool>() {
        "bool"
    } else {
        "unknown"
    }
}

/// `amount` in whole dollars, or "just over" or "just under" `limit` when
/// rounding would show it as `limit`.
fn near(amount: USCents, limit: USCents) -> String {
    if amount > limit && amount.whole() == limit.whole() {
        return format!("just over {}", limit.whole());
    }
    if amount < limit && amount.whole() == limit.whole() {
        return format!("just under {}", limit.whole());
    }
    amount.whole()
}

impl IndianaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "indianataxcalc: the facts carry {}, not indianataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "indianataxcalc: these are Indiana's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "indianataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or fisherman with two-thirds of gross income from farming or fishing has one due date and a March 1 filing option under IT-2210 Section A, which is not built",
            ));
        }
        if let Some(tax) = f.this_year().tax()
            && let Some(local) = f.this_year().local_tax()
            && local > tax
        {
            return Err(Error::Invalid(format!(
                "indianataxcalc: county tax of {local} is more than the {tax} of state and county tax together"
            )));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                if let Some(end) = self.y.period_ends.get(i)
                    && p.end() != *end
                {
                    return Err(Error::Invalid(format!(
                        "indianataxcalc: IT-2210A column {} ends on {}, not {}",
                        i + 1,
                        end,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and the Indiana Code part ways on a rule
    /// every Indiana computation uses. They are found in
    /// research/estimated-payments/in.md, items 1, 2 and 10 and the
    /// verification's conflicts. `exactly` and `zero_prior` say whether each
    /// changes anything for the facts.
    fn divergences(&self, exactly: bool, zero_prior: bool) -> Result<Vec<Divergence>, Error> {
        let t = self.y.threshold.whole();
        let mut out = Vec::new();
        for b in [
            working::new_divergence(format!("Exactly {t} due after withholding"))
                .form(
                    format!(
                        "Form ES-40 and Bulletin #3 want estimated tax if you expect over {t} due. That leaves out exactly {t}. Schedule IT-2210 line 6 stops the penalty only below {t}."
                    ),
                    "Form ES-40 (R26 / 9-25), Instructions; Information Bulletin #3 (March 2023), Penalties; Schedule IT-2210 (R27 / 9-25), line 6",
                )
                .statute(
                    format!(
                        "Estimated payments are excused only for tax reasonably expected under {t}. So exactly {t} is covered."
                    ),
                    "IC 6-3-4-4.1(b)",
                )
                .uses(Reading::Form)
                .affects(exactly),
            working::new_divergence("Last year's tax when no return was filed")
                .form(
                    "If you filed no return for the year before, you enter N/A on line 7. Only 90% of this year's tax is then open. The form was revised in September 2025, before the 2026 change.",
                    "Schedule IT-2210 (R27 / 9-25), instructions for lines 7 and 8",
                )
                .statute(
                    "From 2026, last year's tax counts as $0 if you filed no return for it. You must also have had no tax due for it. So the required annual payment is $0.",
                    "IC 6-3-4-4.1(a)(3), added by P.L.205-2025, SEC.10, effective January 1, 2026",
                )
                .uses(Reading::Statute)
                .affects(zero_prior),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let last = y.year - 1;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let ptet_opt = extras.pass_through_entity_tax_credit();
        let ptet = ptet_opt.unwrap_or_default();
        a.ptet = ptet;
        let withheld = f.withheld_total();
        let credited = withheld + ptet;
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();
        let prior_figures = if has_prior {
            prior.figures().unwrap_or_default()
        } else {
            Figures::default()
        };

        // IT-2210 line 4: 90% of this year's state and county tax after
        // credits.
        let this_year = if tax_known {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year, y.this_year.of(tax))
                .build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year, USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            let county = match f.this_year().local_tax() {
                Some(local) => format!(", {local} of it county tax,"),
                None => String::new(),
            };
            a.notes.rule(
                "IC 6-3-4-4.1(a); Schedule IT-2210 (2025), Section C, lines 1 to 4",
                format!(
                    "{} of this year's state and county tax after credits{} at {} is {}",
                    tax,
                    county,
                    y.this_year,
                    this_year.amount()
                ),
            );
        }

        // IT-2210 line 7: last year's tax, 100% or 110%.
        let mut zero_prior = false;
        let unavailable = |why: String| {
            let mut b = estimated::new_target_option(Basis::PriorYearTax, y.prior, USCents(0));
            b.unavailable(why);
            b
        };
        let prior_annualised = extras.prior_year_annualised_tax();
        let residence = prior.full_year_resident();
        let twelve = extras.twelve_month_rule();
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Indiana return"))
        } else if !prior.filed() && prior.not_required_to_file() && y.zero_prior_tax {
            zero_prior = true;
            a.notes.rule(
                "IC 6-3-4-4.1(a)(3)",
                format!(
                    "no {last} return was filed and none was required, so the {last} tax is treated as $0 and the required annual payment is $0"
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior, USCents(0))
        } else if !prior.filed() {
            if y.zero_prior_tax {
                a.notes.ask(
                    PositionKind::PriorYearReturn,
                    format!("Did you have to file a {last} Indiana return?"),
                    format!(
                        "If not, and no {last} tax was due, last year's tax and your target count as $0."
                    ),
                );
            }
            unavailable(format!("You didn't file a {last} Indiana return"))
        } else if prior.months() != 12 && twelve == Some(true) {
            unavailable(format!(
                "Your {last} Indiana return covered {} months, not 12",
                prior.months()
            ))
        } else if residence == Some(false) && prior_annualised.is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!("What would your {last} Indiana tax have been on a full-year basis?"),
                format!(
                    "Last year's test uses it if you were a {last} part-year resident. A full-year nonresident uses the tax as filed instead."
                ),
            );
            unavailable(format!(
                "Needs your {last} Indiana tax on a full-year basis"
            ))
        } else {
            let mut base = prior_figures.tax().unwrap_or_default();
            if let Some(pa) = prior_annualised {
                base = pa;
                a.notes.rule(
                    "Schedule IT-2210 (2025), line 7, part-year example",
                    format!(
                        "the {last} tax annualised for a part-year resident is {base}, and is used as last year's tax"
                    ),
                );
            } else if residence.is_none() {
                a.notes.ask(
                    PositionKind::Residency,
                    format!("Were you a full-year Indiana resident in {last}?"),
                    format!(
                        "A part-year resident's {last} tax is annualised for last year's test."
                    ),
                );
            }
            if prior.months() != 12 && twelve.is_none() {
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "Indiana's forms let your {}-month return set last year's test. The federal rule Indiana follows needs 12 months; the figures follow the forms.",
                        prior.months()
                    ),
                );
            }
            let (rate, why) = self.prior_share(f, &extras, &prior_figures, &mut a.notes);
            let amount = rate.of(base);
            a.notes.rule(
                "IC 6-3-4-4.1(a); Schedule IT-2210 (2025), line 7 and CAUTION box",
                format!(
                    "{why}, so the prior-year test is {rate} of the {base} {last} tax, or {amount}"
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, rate, amount)
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known && !zero_prior {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Indiana tax to be?", y.year),
                format!(
                    "It sets the {} option and decides whether you're under the {} threshold.",
                    y.this_year,
                    y.threshold.whole()
                ),
            );
        }

        // IT-2210 line 14 divides by the installment periods of residence.
        let (share, periods) = self.residence_shares(f)?;
        let spread = if periods != 4 {
            format!("Spread them over the {periods} quarters you lived in Indiana.")
        } else {
            "Pay a quarter by each date.".to_string()
        };
        if zero_prior {
            a.exempt = true;
            let none =
                estimated::new_exception("No Indiana return or tax due last year").build()?;
            rb.required(
                false,
                format!(
                    "You filed no {last} Indiana return and none was required. Indiana then counts your {last} tax as $0. No estimated payments are needed for {}.",
                    y.year
                ),
            )
            .exempt(none);
        } else if tax_known && tax - credited < y.threshold {
            a.exempt = true;
            let under = estimated::exception_below(
                "Tax less withholding and credits",
                tax - credited,
                y.threshold,
            )?;
            let leaves = if tax < credited {
                format!(
                    "Withholding and pass-through credit of {} leave a {} refund.",
                    credited.whole(),
                    (credited - tax).whole()
                )
            } else {
                format!(
                    "Withholding and pass-through credit cover {}, leaving {}. That is under {}.",
                    credited.whole(),
                    near(tax - credited, y.threshold),
                    y.threshold.whole()
                )
            };
            rb.required(
                false,
                format!(
                    "Your Indiana state and county tax is {}. {} No estimated payments are needed, and no penalty can arise.",
                    tax.whole(),
                    leaves
                ),
            )
            .exempt(under);
            a.notes.rule(
                "IC 6-3-4-4.1(b); Schedule IT-2210 (2025), line 6",
                format!(
                    "{} of tax less {} of withholding and pass-through credit is {}, under {}, so there is no penalty",
                    tax,
                    credited,
                    tax - credited,
                    y.threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target needs your {} Indiana tax, because your {last} return can't set one.",
                    y.year
                ),
            )
            .no_target();
        } else if target.0 == 0 && chosen == Basis::PriorYearTax {
            a.covered = true;
            rb.required(
                false,
                format!(
                    "Your {last} Indiana tax was $0, so your target is $0. No estimated payments are needed."
                ),
            )
            .choose(chosen);
        } else if !tax_known && credited >= target {
            a.covered = true;
            rb.required(
                false,
                format!(
                    "Your target is {} of your {last} tax, {}. Your withholding and pass-through credit of {} already reach it. No estimated payments are needed, whatever your {} tax turns out to be.",
                    prior_opt.percent(),
                    target.whole(),
                    credited.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {} of your {last} tax, {}. Withholding and pass-through credit cover {} of it. So you need {} of estimated payments. {} That keeps you clear of a penalty whatever your {} tax turns out to be.",
                    prior_opt.percent(),
                    target.whole(),
                    credited.whole(),
                    (target - credited).whole(),
                    spread,
                    y.year
                ),
            )
            .choose(chosen);
        } else if credited >= target {
            a.covered = true;
            rb.required(
                false,
                format!(
                    "Your target is {}. Your Indiana withholding and pass-through credit of {} already reach it. No estimated payments are needed.",
                    target.whole(),
                    credited.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Indiana state and county tax is {}. Withholding and pass-through credit cover {}, leaving {}. That is {} or more, so you need estimated payments. To reach your target of {}, they must come to {}. {}",
                    tax.whole(),
                    credited.whole(),
                    near(tax - credited, y.threshold),
                    y.threshold.whole(),
                    target.whole(),
                    (target - credited).whole(),
                    spread
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        // Under an exception last year's return can't change the target.
        if !has_prior && !a.exempt {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Indiana return, and what was its tax?"),
                format!(
                    "With it, your target could be {} of last year's tax, not {} of this year's.",
                    y.prior, y.this_year
                ),
            );
        }
        if a.covered {
            a.notes.rule(
                "Schedule IT-2210 (2025), line 8",
                format!(
                    "the required annual payment of {target} is no more than the {credited} of withholding and pass-through credit, so there is no penalty"
                ),
            );
        }
        if !zero_prior {
            if tax_known && f.this_year().local_tax().is_none() {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!("Does your {} tax include your county income tax?", y.year),
                    "Indiana's tests count county tax too, and the figures assume yours includes it.",
                );
            }
            if ptet_opt.is_none() {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "Do you have a pass-through entity tax credit for {}?",
                        y.year
                    ),
                    "It counts with your withholding and can lower what's needed.",
                );
            }
        }

        // IT-2210 line 14: the required annual payment divided by four, or by
        // the number of installment periods of residence for a part-year
        // resident.
        let mut cum = Vec::with_capacity(share.len());
        let mut k = 0;
        for &lived in &share {
            if lived {
                k += 1;
            }
            cum.push(USCents(
                (a.requirement.target().0 * k * 2 + periods) / (2 * periods),
            ));
        }
        a.installments = estimated::increments(&cum);
        if periods == 4 {
            a.notes.rule(
                "IC 6-3-4-4.1(a); Schedule IT-2210 (2025), line 14; Form ES-40 (2026)",
                format!(
                    "a quarter of {}, {}, is due on each of {}, {}, {} and {}",
                    a.requirement.target(),
                    a.installments[0],
                    a.dues[0].long(),
                    a.dues[1].long(),
                    a.dues[2].long(),
                    a.dues[3].long()
                ),
            );
        } else {
            a.notes.rule(
                "Schedule IT-2210 (2025), instructions for line 14",
                format!(
                    "you were an Indiana resident in {} of the four installment periods, so {} is divided by {}: {}, {}, {} and {} are due on {}, {}, {} and {}",
                    periods,
                    a.requirement.target(),
                    periods,
                    a.installments[0],
                    a.installments[1],
                    a.installments[2],
                    a.installments[3],
                    a.dues[0].long(),
                    a.dues[1].long(),
                    a.dues[2].long(),
                    a.dues[3].long()
                ),
            );
        }

        if f.elections().annualised() && !a.exempt {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &y.annualised, &a.installments)?;
            a.notes.rule(
                "Schedule IT-2210A (2025), Section 3, lines 11 to 19",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: {}, {}, {} and {} of the annualised tax, \
                     less earlier installments, and never more than the regular installment plus any amount carried forward",
                    ann[0],
                    ann[1],
                    ann[2],
                    ann[3],
                    y.annualised[0],
                    y.annualised[1],
                    y.annualised[2],
                    y.annualised[3]
                ),
            );
            a.installments = ann;
        }

        // IB3: a return filed and paid in full by January 31 excuses the
        // fourth installment. January 31, 2027 is a Sunday, and IB3 moves a
        // weekend date to the next business day, as the 2025 IT-2210 prints
        // Feb. 2, 2026.
        let file_by = y.calendar.next_business_day(y.file_and_pay_by);
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(file_by) && !a.exempt {
            a.excused = true;
            a.notes.rule(
                "Information Bulletin #3 (March 2023), Avoiding Penalty for the Fourth Installment; Schedule IT-2210 (2025), Section B",
                format!(
                    "the return was filed and paid on {}, by {}, so the fourth installment is excused",
                    filed.long(),
                    file_by.long()
                ),
            );
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        let exactly = tax_known && !a.exempt && tax - credited == y.threshold;
        let divergences = self.divergences(exactly, zero_prior)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// The IT-2210 line 7 percentage: 110% when last year's federal AGI was
    /// over the step, 100% otherwise, and 110% when it was not given, with
    /// why in words for the rule it goes in.
    fn prior_share(
        &self,
        f: &Facts,
        extras: &EstimatedFacts,
        pf: &Figures,
        notes: &mut Notes,
    ) -> (Rate, String) {
        let y = &self.y;
        let last = y.year - 1;
        let Some(agi) = pf.federal_agi() else {
            notes.ask(
                PositionKind::AdjustedGrossIncome,
                format!("What was your {last} federal AGI?"),
                format!(
                    "Above {} ({} married filing separately) last year's test is {}. It is {} otherwise, so until then the figures use {}.",
                    y.step.whole(),
                    y.step_separate.whole(),
                    y.prior_high,
                    y.prior,
                    y.prior_high
                ),
            );
            return (y.prior_high, format!("{last} federal AGI was not given"));
        };
        let separate = match extras.prior_year_married_separately() {
            Some(s) => s,
            None => {
                if agi > y.step_separate && agi <= y.step {
                    notes.ask(
                        PositionKind::JurisdictionFact,
                        format!("Was your {last} return married filing separately?"),
                        format!(
                            "If so, last year's test is {} above {} of federal AGI, not {}.",
                            y.prior_high,
                            y.step_separate.whole(),
                            y.step.whole()
                        ),
                    );
                }
                f.filing_status() == Status::MarriedFilingSeparately
            }
        };
        let step = if separate { y.step_separate } else { y.step };
        if agi > step {
            return (
                y.prior_high,
                format!("{last} federal AGI of {agi} is more than {step}"),
            );
        }
        (
            y.prior,
            format!("{last} federal AGI of {agi} is not more than {step}"),
        )
    }

    /// Which installment periods the filer lived in Indiana for, and how
    /// many. No residence given, or none in Indiana, is taken as all four:
    /// IT-2210 line 14 divides by fewer only for a part-year resident.
    fn residence_shares(&self, f: &Facts) -> Result<(Vec<bool>, i64), Error> {
        let ends = &self.y.period_ends;
        let mut lived = vec![false; ends.len()];
        let mut given = false;
        for p in f.residence() {
            if p.state() != JURISDICTION.as_str() {
                continue;
            }
            given = true;
            let mut start = estimated::must_date(self.y.year, Month::JANUARY, 1);
            for (i, &end) in ends.iter().enumerate() {
                if !p.from().after(end) && !p.to().before(start) {
                    lived[i] = true;
                }
                start = end.add_days(1);
            }
        }
        if !given {
            lived.iter_mut().for_each(|l| *l = true);
        }
        let n = lived.iter().filter(|&&l| l).count() as i64;
        if n == 0 {
            return Err(Error::Invalid(
                "indianataxcalc: no installment period of Indiana residence".into(),
            ));
        }
        Ok((lived, n))
    }

    /// Withholding, the pass-through credit and payments, dated.
    /// Withholding is spread in four equal parts on the due dates unless the
    /// filer elects the dates it was withheld; the pass-through credit
    /// always is, as IT-2210 line 15 divides it by four.
    fn credits(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Payment>, Notes), Error> {
        let mut notes = Notes::default();
        let mut credits = if f.elections().withholding_as_withheld() {
            notes.rule(
                "Information Bulletin #3 (March 2023), Treatment of Withholding Payments",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            notes.rule(
                "Information Bulletin #3 (March 2023), Treatment of Withholding Payments; Schedule IT-2210 (2025), line 15",
                format!(
                    "the {} withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
            estimated::credits(f, &a.dues, &quarters())?
        };
        if a.ptet.0 > 0 {
            let cum = estimated::cumulative_shares(a.ptet, &quarters())?;
            for (i, amt) in estimated::increments(&cum).into_iter().enumerate() {
                if amt.0 == 0 {
                    continue;
                }
                credits.push(estimated::new_payment().on(a.dues[i]).amount(amt).build()?);
            }
            notes.rule(
                "Schedule IT-2210 (2025), lines 5 and 15",
                format!(
                    "the {} pass-through entity tax credit is counted in four equal parts on the due dates",
                    a.ptet
                ),
            );
        }
        credits.sort_by(|x, y| {
            if x.on().before(y.on()) {
                std::cmp::Ordering::Less
            } else if y.on().before(x.on()) {
                std::cmp::Ordering::Greater
            } else {
                std::cmp::Ordering::Equal
            }
        });
        Ok((credits, notes))
    }

    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Column>, Notes), Error> {
        let (credits, mut notes) = self.credits(f, a)?;
        let mut required = a.installments.clone();
        if a.excused {
            required[3] = USCents(0);
        }
        let (mut cols, late) = section_e(&a.dues, &required, &credits);
        notes.rule(
            "Information Bulletin #3 (March 2023), Underpayment; Schedule IT-2210 (2025), lines 16 to 19",
            "a payment after a due date counts in the next period, and an overpayment first makes up earlier underpayments, without reducing the penalty on them, before any of it carries forward",
        );
        if late.0 > 0 {
            notes.rule(
                "Information Bulletin #3 (March 2023), Methods for Payment of Estimated Tax",
                format!(
                    "{} paid after {} is not a {} installment payment; the department returns it or credits it to the next year",
                    late,
                    a.dues[3].long(),
                    self.y.year
                ),
            );
        }
        if a.covered {
            for c in &mut cols {
                c.shortfall = USCents(0);
            }
        }
        Ok((cols, notes))
    }
}

/// One IT-2210 Section E column.
#[derive(Clone, Copy, Debug)]
struct Column {
    due: Date,
    /// Everything credited by the due date.
    credited_by: USCents,
    /// Line 19.
    shortfall: USCents,
}

/// Applies credits to the installments the way Schedule IT-2210 Section E
/// and Bulletin #3 do, for a penalty that is a flat share of each shortfall
/// rather than interest on it.
///
/// A credit counts in the column whose due date is the first on or after
/// it: a payment after a due date is in the next period. An overpayment in a
/// column first makes up earlier underpayments, which does not reduce the
/// penalty on them, and only what is left carries to the next column. A
/// column that is short carries nothing. Credits after the last due date
/// count in no column.
///
/// [`estimated::apply`] is not used: it charges a payment made between two
/// due dates against the older shortfall first and leaves the later
/// installment short by as much, which is right for interest and charges a
/// flat penalty twice on the same dollars. The second value returned is the
/// total of credits after the last due date.
fn section_e(dues: &[Date], required: &[USCents], credits: &[Payment]) -> (Vec<Column>, USCents) {
    let mut cols = Vec::with_capacity(dues.len());
    let (mut credited_by, mut carry, mut owed) = (USCents(0), USCents(0), USCents(0));
    let mut next = 0;
    for (i, &due) in dues.iter().enumerate() {
        let mut paid = USCents(0);
        while next < credits.len() && !credits[next].on().after(due) {
            paid += credits[next].amount();
            next += 1;
        }
        credited_by += paid;
        let total = paid + carry;
        let mut c = Column {
            due,
            credited_by,
            shortfall: USCents(0),
        };
        if total >= required[i] {
            let over = total - required[i];
            let made = over.min(owed);
            owed -= made;
            carry = over - made;
        } else {
            c.shortfall = required[i] - total;
            owed += c.shortfall;
            carry = USCents(0);
        }
        cols.push(c);
    }
    let late = credits[next..].iter().map(|c| c.amount()).sum();
    (cols, late)
}

/// The installments the columns give. The fourth is excused for a return
/// filed and paid in time.
fn schedule_of(cols: &[Column], a: &Assessment) -> Result<Vec<Installment>, Error> {
    let mut out = Vec::with_capacity(cols.len());
    let mut needed = USCents(0);
    for (i, c) in cols.iter().enumerate() {
        needed += a.installments[i];
        let mut b = estimated::new_installment(i as i64 + 1, c.due);
        b.needed_by(needed)
            .credited_by(c.credited_by)
            .shortfall(c.shortfall);
        if a.excused && i == 3 {
            b.excused();
        }
        out.push(b.build()?);
    }
    Ok(out)
}

impl EstimatedTax for IndianaEstimated {
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
                    "with no {} Indiana return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (cols, notes) = self.ledger(f, &a)?;
        let out = schedule_of(&cols, &a)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    /// 10% of the shortfall of each installment due on or before `as_of`.
    /// The charge arises on the due date and does not grow, so a return
    /// filed later does not stop it and there is no rate to run out.
    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "indianataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Indiana return to measure against, there is no target to figure a penalty from",
                    self.y.year - 1
                ),
            ));
        }
        let y = &self.y;
        let waiver_opt = f.positions().waiver();
        let waiver = waiver_opt.unwrap_or_default();
        let (cols, mut notes) = self.ledger(f, &a)?;
        let sched = schedule_of(&cols, &a)?;
        if a.exempt {
            let p = estimated::new_penalty(as_of)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }
        let mut pb = estimated::new_penalty(as_of);
        pb.waiver(waiver).because(a.requirement.clone(), &sched);
        for (i, c) in cols.iter().enumerate() {
            if c.shortfall.0 == 0 || c.due.after(as_of) {
                continue;
            }
            let part = estimated::new_penalty_part(PartKind::Flat, c.shortfall, y.rate)
                .charge(y.rate.of(c.shortfall))
                .source("IC 6-8.1-10-2.1(b); Schedule IT-2210 (2025), lines 20 and 21")
                .build()?;
            let line = estimated::new_penalty_line(i as i64 + 1, c.due, c.shortfall)
                .part(part)
                .build()?;
            pb.line(line);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        let pending = if cols.last().is_some_and(|c| as_of.before(c.due)) {
            "; installments due after that are not yet charged"
        } else {
            ""
        };
        notes.rule(
            "IC 6-3-4-4.1(b); IC 6-8.1-10-2.1(b); Schedule IT-2210 (2025), lines 19 to 21",
            format!(
                "each installment's underpayment is charged a flat {}, however late it is paid; the penalty to {} is {}{}",
                y.rate,
                as_of.long(),
                p.total(),
                pending
            ),
        );
        if !a.tax_known {
            notes.rule(
                "IC 6-3-4-4.1(a); Schedule IT-2210 (2025), line 8",
                format!(
                    "this year's tax was not given, so the penalty is figured against {} of the {} tax alone; \
                     the required annual payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if p.total().0 > 0 && waiver_opt.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Do you want a waiver for casualty, disaster, retirement after 62 or disability?",
                "Indiana hasn't said if the federal waivers or its reasonable-cause waiver apply. So the figures show the full penalty for an estimated tax shortfall.",
            );
        } else if p.total().0 > 0 && waiver != WaiverKind::None {
            // No Indiana document says whether IC 6-3-4-4.1(a) carries over
            // the waivers of 26 U.S.C. § 6654(e)(3), and IC
            // 6-8.1-10-2.1(d) doesn't name this penalty among those
            // reasonable cause can waive.
            p = p
                .to_builder()
                .waiver_outcome(estimated::waiver_undecided(
                    "Indiana has no stated waiver for a casualty, disaster, retirement or disability.",
                ))
                .build()?;
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                "The federal waivers cover a casualty, disaster, retirement or disability. Indiana hasn't said if they or its reasonable-cause waiver cover estimated tax.",
            );
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        INDIANA_EXPLANATION.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/in.md.
static INDIANA_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Indiana wants estimated tax if you expect $1,000 or more due after withholding. The $1,000 counts state and county income tax. Its penalty is a flat 10% of each quarter's shortfall, not interest.",
    )
    .paragraph("You are clear of the penalty if you pay the smaller of two amounts. They are 90% of this year's state and county tax and 100% of last year's. Withholding, pass-through entity tax credits and estimated payments all count. Last year's figure becomes 110% if your federal AGI then was over $150,000. The limit is $75,000 if you were married filing separately. County income tax counts throughout. Indiana collects it on the same return and payment voucher.")
    .paragraph("The payments are a quarter each. They are due April 15, June 15 and September 15, 2026, and January 15, 2027. Withholding counts as paid evenly on the four dates. You can instead show the dates it was actually taken. Filing and paying in full by January 31 replaces the January payment. For 2026 the date is February 1, 2027, because January 31 is a Sunday. If your income came unevenly, use the annualised method on Schedule IT-2210A. It can lower the early payments.")
    .paragraph("The penalty is 10% of what each quarter was short. It does not grow with time. A payment one day late costs as much as one made months later. A late payment counts toward the next quarter. An overpayment first makes up earlier shortfalls, and the rest carries forward. Making up a shortfall does not take back the 10% already charged on it. A payment made after January 15, 2027 does not count toward 2026 at all. No Indiana document says whether the federal waivers apply. They cover a casualty, disaster, retirement or disability.")
    .paragraph("Subtract withholding and pass-through credits from this year's tax. There is no penalty if what is left is under $1,000. From 2026, last year's tax can count as zero. That needs no Indiana return filed last year and no Indiana tax for it. Then nothing is required.")
    .versus_federal("Indiana charges a flat 10% of each quarter's shortfall, not interest by the day. How late you pay doesn't matter, and catching up later doesn't reduce it. County income tax counts in the $1,000 threshold and both targets. Payments after January 15 don't count.")
    .source("IC 6-3-4-4.1; IC 6-8.1-10-2.1(b), (d)")
    .source("Form ES-40, Estimated Tax Payment Form, 2026 (R26 / 9-25)")
    .source("Schedule IT-2210, Underpayment of Estimated Tax by Individuals, and its instructions, 2025 (R27 / 9-25)")
    .source("Schedule IT-2210A, Annualized Income Schedule, 2025 (R25 / 9-25)")
    .source("Income Tax Information Bulletin #3, Payment of Indiana Estimated Tax by Individuals (March 2023)")
    .source("Indiana Department of Revenue, Legislative Synopsis 2026")
    .build()
    .unwrap_or_default()
});
