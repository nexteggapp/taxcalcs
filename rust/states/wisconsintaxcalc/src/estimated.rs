//! Wisconsin's estimated tax rules, Wis. Stat. §§ 71.09 and 71.84, as the
//! shared [`EstimatedTax`] interface.
//!
//! Wisconsin calls its charge underpayment interest. It is 12% a year, set
//! by § 71.84(1), on each installment's shortfall. The required annual
//! payment is the smaller of 90% of this year's tax and 100% of last year's,
//! with no 110% tier, in four equal installments. The threshold is $500.
//!
//! Schedule U applies payments in its own order. A payment counts first
//! toward the installment of the period it was made in. Only what is left
//! over goes back to an earlier shortfall, the most recent one first, and
//! then forward. A payment after January 15 goes to the January shortfall
//! first. That is why this crate keeps its own ledger rather than using
//! [`estimated::apply`], which pays the oldest shortfall first.
//!
//! The figure follows the department's forms: the 2026 Form 1-ES
//! instructions and the 2025 Schedule U and its instructions, which are the
//! latest published. The result's Divergences say where Chapter 71 reads
//! otherwise. The statute text published on October 1, 2026 still has the
//! $500 threshold, the 90% and 100% tests, the four dates and the 12% rate
//! both forms use, so neither form predates a change in the law for 2026.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Payment, Penalty,
    RateSeries, Requirement, TargetOptionBuilder, WaiverKind,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// jurisdiction is Wisconsin's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::WISCONSIN;

/// The facts Wisconsin's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_year_farmer: Option<bool>,
    disaster_extension: Option<bool>,
    active_duty_abroad: Option<bool>,
}

impl EstimatedFacts {
    /// Whether at least two-thirds of last year's gross income was from
    /// farming or fishing, or `None` if the caller did not say.
    /// § 71.09(1)(a) makes either year's income enough to be a farmer or
    /// fisher. [`Facts::farmer_or_fisher`] covers this year.
    pub fn prior_year_farmer_or_fisher(&self) -> Option<bool> {
        self.prior_year_farmer
    }

    /// Whether the filer qualifies for a federal extension of time to file
    /// or pay under 26 U.S.C. § 7508A, for a federally declared disaster or
    /// terroristic or military action, or `None` if the caller did not say.
    /// Schedule U exception code 10.
    pub fn federal_disaster_extension(&self) -> Option<bool> {
        self.disaster_extension
    }

    /// Whether the filer was on active duty with the U.S. armed forces while
    /// stationed outside the continental United States, or `None` if the
    /// caller did not say. Schedule U exception code 07.
    pub fn active_duty_outside_us(&self) -> Option<bool> {
        self.active_duty_abroad
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
    /// Says whether two-thirds of last year's gross income was from farming
    /// or fishing.
    pub fn prior_year_farmer_or_fisher(&mut self, v: bool) -> &mut Self {
        self.e.prior_year_farmer = Some(v);
        self
    }

    /// Says whether the filer qualifies for a federal disaster extension.
    pub fn federal_disaster_extension(&mut self, v: bool) -> &mut Self {
        self.e.disaster_extension = Some(v);
        self
    }

    /// Says whether the filer was on active duty outside the continental
    /// United States.
    pub fn active_duty_outside_us(&mut self, v: bool) -> &mut Self {
        self.e.active_duty_abroad = Some(v);
        self
    }

    /// Has nothing to refuse: every fact is a yes or no.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// What tax less withholding must reach before any interest applies.
    pub(crate) threshold: USCents,

    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,

    /// The installments' shares of the required annual payment, and the
    /// shares of withholding deemed paid on each date.
    pub(crate) shares: Vec<Rate>,

    /// Schedule U Part IV line 42.
    pub(crate) annualised: Vec<Rate>,
    pub(crate) period_ends: Vec<Date>,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// § 71.09(14)'s January 31, before it is moved.
    pub(crate) file_and_pay_by: Date,

    /// § 71.84(1)'s 15th day of the 4th month after the year ends.
    pub(crate) stop: Date,
    pub(crate) rates: RateSeries,
}

/// The year's figures, or `None` for a year this build does not carry.
pub(crate) fn estimated_year(year: i64) -> Result<Option<EstimatedYear>, Error> {
    match year {
        2026 => Ok(Some(crate::estimated_y2026::year_2026()?)),
        _ => Ok(None),
    }
}

/// Returns Wisconsin's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    match estimated_year(year)? {
        Some(y) => Ok(Box::new(WisconsinEstimated { y })),
        None => Err(estimated::no_rules(&JURISDICTION, year)),
    }
}

struct WisconsinEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// Schedule U line 16, or line 50 under the annualised method.
    installments: Vec<USCents>,
    excused_january: bool,
    extras: EstimatedFacts,
    notes: Notes,
}

/// Go's `%T` of the extras, as far as it can be told from here.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else {
        "an unknown type"
    }
}

/// A question asked only when no exception sets the target.
struct Ask {
    kind: PositionKind,
    question: String,
    effect: String,
}

impl WisconsinEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "wisconsintaxcalc: the facts carry {}, not wisconsintaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, extras: &EstimatedFacts) -> Result<(), Error> {
        let last = self.y.year - 1;
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "wisconsintaxcalc: these are Wisconsin's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "wisconsintaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or fisher with two-thirds of gross income from farming or fishing pays by January 15 or files by March 1 (Schedule U exception code 04), which is not built",
            ));
        }
        if extras.prior_year_farmer_or_fisher() == Some(true) {
            return Err(Error::with_detail(
                Error::Unsupported,
                format!(
                    "two-thirds of {last} gross income from farming or fishing makes a farmer or fisher for {} under Wis. Stat. § 71.09(1)(a) (Schedule U exception code 04), which is not built",
                    self.y.year
                ),
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "wisconsintaxcalc: Schedule U Part IV period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and Chapter 71 part ways on a rule a
    /// Wisconsin computation uses. The first two are in the verification
    /// section of the research file. The third is the 1-ES's table of later
    /// first payment dates, which neither the statute nor Schedule U has.
    /// Each affects the household only when its facts reach the rule.
    fn divergences(&self, f: &Facts, a: &Assessment) -> Result<Vec<Divergence>, Error> {
        let scheduled = a.requirement.target_known() && !a.exempt;
        let waiver = f.positions().waiver().unwrap_or_default();
        let abroad = a.extras.active_duty_outside_us().unwrap_or_default();
        let first = f.requirement_first_met();
        let mut out = Vec::new();
        for b in [
            working::new_divergence("Retirement or disability")
                .form("You can apply for a waiver if you retired after age 62 or became disabled. The retirement or disability must be this year or last. The underpayment must also be due to reasonable cause, not willful neglect.",
                    "Schedule U Instructions (2025), Waiver of Underpayment Interest")
                .statute("The department can find that you retired after age 62 or became disabled. If that was in the year or the year before, no interest is due. There's no reasonable-cause condition.",
                    "Wis. Stat. § 71.09(11)(d)")
                .uses(Reading::Form)
                .affects(scheduled && waiver == WaiverKind::RetiredOrDisabled)
                .clone(),
            working::new_divergence("Active duty outside the continental United States")
                .form("You can apply for a waiver of interest for active duty in the US armed forces. You must have been stationed outside the continental United States.",
                    "Schedule U Instructions (2025), Waiver of Underpayment Interest")
                .statute("The estimated tax law does not apply to such a person. No estimated tax is due and no interest arises.",
                    "Wis. Stat. § 71.09(2)")
                .uses(Reading::Form)
                .affects(scheduled && abroad)
                .clone(),
            working::new_divergence("A requirement first met after March 31")
                .form("If you first need to pay after March 31, Form 1-ES lets you start later. Your first payment is 1/2, 3/4 or all, on June 15, September 15 or January 15. Schedule U still asks for a quarter by every date.",
                    "2026 Form 1-ES Instructions, When to Pay Your Estimated Tax, item 1; Schedule U (2025), line 16, and its instructions")
                .statute("Each of the four payments is 25% of the required annual payment. There is no later first date. The annualized method is the only way to lower one.",
                    "Wis. Stat. § 71.09(12), (13)(a) and (d)")
                .uses(Reading::Statute)
                .affects(scheduled && !first.is_zero() && first.after(self.y.period_ends[0]))
                .clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// Answers the three questions and adds the divergences.
    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let mut a = self.figure(f)?;
        let ds = self.divergences(f, &a)?;
        a.notes.diverges(&ds);
        Ok(a)
    }

    /// The requirement and the required installments.
    fn figure(&self, f: &Facts) -> Result<Assessment, Error> {
        let extras = self.extras(f)?;
        self.check(f, &extras)?;
        let y = &self.y;
        let last = y.year - 1;
        let mut a = Assessment {
            extras,
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior_ret = f.prior_year();
        let has_prior = prior_ret.is_some();
        let prior = prior_ret.unwrap_or_default();
        let mut prior_tax = USCents(0);
        if has_prior && let Some(pf) = prior.figures() {
            prior_tax = pf.tax().unwrap_or_default();
        }

        // 90% of this year's tax: Schedule U lines 3 and 4.
        let this_year = if tax_known {
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
                "Wis. Stat. § 71.09(13)(a)1.; Schedule U (2025), Part I, lines 3 and 4",
                format!(
                    "{} of this year's Wisconsin tax of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        let mut asks: Vec<Ask> = Vec::new();

        // 100% of last year's tax: Schedule U line 7. No 110% tier.
        let unavailable = |why: String| -> TargetOptionBuilder {
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
                .unavailable(why)
                .clone()
        };
        let prior_option = if !has_prior {
            asks.push(Ask {
                kind: PositionKind::PriorYearReturn,
                question: format!("What was the tax on your {last} Wisconsin return?"),
                effect: "Your target could be 100% of last year's tax, or a $0 tax could excuse interest.".into(),
            });
            unavailable(format!("Needs your {last} Wisconsin return"))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Wisconsin return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Wisconsin return covered {} months, not all 12",
                prior.months()
            ))
        } else {
            a.notes.rule(
                "Wis. Stat. § 71.09(13)(a)2., (b); Schedule U (2025), Part I, line 7",
                format!(
                    "the {last} return covered 12 months, so the prior-year test is {} of its {} tax, {}; Wisconsin has no 110% tier",
                    y.prior_year_share,
                    prior_tax,
                    y.prior_year_share.of(prior_tax)
                ),
            );
            estimated::new_target_option(
                Basis::PriorYearTax,
                y.prior_year_share,
                y.prior_year_share.of(prior_tax),
            )
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            asks.push(Ask {
                kind: PositionKind::ThisYearTax,
                question: format!("What do you expect your {} Wisconsin tax to be?", y.year),
                effect: format!(
                    "It could make 90% of this year's tax your target and test the {} threshold.",
                    y.threshold.whole()
                ),
            });
        }

        // Exception code 02: a 12-month year last year with no liability, as
        // a full-year resident.
        let zero_last_year = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax.0 == 0) || prior.not_required_to_file());
        let resident_opt = prior.full_year_resident();
        let resident = resident_opt.unwrap_or_default();
        if zero_last_year && resident_opt.is_none() {
            asks.push(Ask {
                kind: PositionKind::Residency,
                question: format!("Were you a full-year Wisconsin resident in {last}?"),
                effect:
                    "If you were, having no Wisconsin tax for those 12 months excuses the interest."
                        .into(),
            });
        }
        let disaster_opt = extras.federal_disaster_extension();
        let disaster = disaster_opt.unwrap_or_default();

        let owed = tax - withheld;
        if disaster {
            a.exempt = true;
            let ext = estimated::new_exception("Federal disaster extension").build()?;
            rb.required(
                false,
                format!(
                    "You qualify for a federal disaster extension. Wisconsin charges no underpayment interest for {}. No estimated payments are needed.",
                    y.year
                ),
            )
            .exempt(ext);
            a.notes.rule(
                "Wis. Stat. § 71.09(11)(e); Schedule U Instructions (2025), Exceptions (4), code 10",
                "a federal extension under 26 U.S.C. § 7508A for a declared disaster or terroristic or military action means no interest",
            );
        } else if zero_last_year && resident {
            a.exempt = true;
            let none = estimated::new_exception("No Wisconsin tax due last year").build()?;
            rb.required(
                false,
                format!(
                    "You had no Wisconsin tax for all 12 months of {last} as a full-year resident. No estimated payments are needed, and no interest can arise."
                ),
            )
            .exempt(none);
            a.notes.rule(
                "Wis. Stat. § 71.09(11)(b); Schedule U Instructions (2025), Exceptions (1), code 02",
                format!(
                    "the {last} return showed no liability for 12 months as a full-year resident, so there is no interest for {}",
                    y.year
                ),
            );
        } else if tax_known && owed < y.threshold {
            a.exempt = true;
            let under = estimated::exception_below("Tax less withholding", owed, y.threshold)?;
            let mut why = format!(
                "Your Wisconsin tax of {} less {} of withholding leaves {}. The rest is under {}, so no estimated payments are needed.",
                tax.whole(),
                withheld.whole(),
                owed.whole(),
                y.threshold.whole()
            );
            if owed.0 < 0 {
                why = format!(
                    "Your Wisconsin tax of {} less {} withheld {}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(owed)
                );
            } else if owed.0 == 0 {
                why = format!(
                    "Your withholding of {} covers your Wisconsin tax of {}. No estimated payments are needed.",
                    withheld.whole(),
                    tax.whole()
                );
            } else if owed.whole() == y.threshold.whole() {
                why = format!(
                    "Your Wisconsin tax less withholding is just under {}. No estimated payments are needed.",
                    y.threshold.whole()
                );
            }
            rb.required(false, why).exempt(under);
            a.notes.rule(
                "Wis. Stat. § 71.09(11)(a); Schedule U (2025), Part I, line 6",
                format!(
                    "{} of tax less {} withheld is {}, under {}, so there is no interest",
                    tax,
                    withheld,
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your {last} return gives no target. Unless an exception applies, your target is 90% of your {} Wisconsin tax.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} already reaches {} of your {last} tax, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
                    withheld.whole(),
                    prior_opt.percent(),
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            let mut why = format!(
                "Your target is {} of your {last} Wisconsin tax, {}. It keeps you clear of interest whatever your {} tax turns out to be. ",
                prior_opt.percent(),
                target.whole(),
                y.year
            );
            if withheld.0 > 0 {
                why.push_str(&format!(
                    "After {} of withholding, you need {} of estimated payments. Pay a quarter by each date.",
                    withheld.whole(),
                    (target - withheld).whole()
                ));
            } else {
                why.push_str("Pay it in estimated payments, a quarter by each date.");
            }
            rb.required(true, why).choose(chosen);
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Wisconsin withholding of {} reaches the {} needed for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
            a.notes.rule(
                "Schedule U Instructions (2025), Line 8",
                format!(
                    "withholding of {withheld} is at least the required annual payment of {target}, so there is no underpayment interest"
                ),
            );
        } else if withheld.0 == 0 {
            rb.required(
                true,
                format!(
                    "Your Wisconsin tax of {} is {} or more, and you have no withholding. You need estimated payments of all {} required, a quarter by each date.",
                    tax.whole(),
                    y.threshold.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Wisconsin tax of {} less {} of withholding leaves {}. The rest is {} or more, and withholding is short of the {} required. You need estimated payments of {}, a quarter by each date.",
                    tax.whole(),
                    withheld.whole(),
                    owed.whole(),
                    y.threshold.whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;

        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }
        for q in &asks {
            a.notes.ask(q.kind, q.question.clone(), q.effect.clone());
        }
        if extras.prior_year_farmer_or_fisher().is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "Was at least two-thirds of your {last} gross income from farming or fishing?"
                ),
                format!(
                    "If so, Wisconsin's farmer and fisher rules apply for {}.",
                    y.year
                ),
            );
        }
        if disaster_opt.is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                "Do you qualify for a federal disaster extension of time to file or pay?",
                "If you do, Wisconsin charges no underpayment interest.",
            );
        }

        // Schedule U line 16: a quarter of line 8 by each date.
        let cum = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cum);
        a.notes.rule(
            "Wis. Stat. § 71.09(12), (13)(a); Schedule U (2025), line 16",
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

        let first = f.requirement_first_met();
        if !first.is_zero() && first.after(y.period_ends[0]) {
            a.notes.rule(
                "Schedule U (2025), line 16, and its instructions; Wis. Stat. § 71.09(13)",
                format!(
                    "the requirement was first met on {}, but Schedule U still requires a quarter by every date from {}; only the annualised method in Part IV lowers the earlier installments",
                    first.long(),
                    a.dues[0].long()
                ),
            );
        }

        if f.elections().annualised() {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &y.annualised, &a.installments)?;
            a.notes.rule(
                "Wis. Stat. § 71.09(13)(d); Schedule U (2025), Part IV, lines 42 to 50",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: {}, {}, {} and {} of the annualised tax, less earlier installments, and never more than the regular quarter plus any amount the method lowered before",
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

        // § 71.09(14): a return filed and paid in full by January 31 takes
        // the place of the January payment, when nothing was required by the
        // earlier dates. January 31, 2027 is a Sunday and moves to February
        // 1 under § 990.001(4)(b), as the 2025 Schedule U moved Saturday
        // January 31, 2026 to February 2.
        let filed = f.return_filed();
        if !filed.is_zero() {
            let by = y.calendar.next_business_day(y.file_and_pay_by);
            let earlier = a.installments[0] + a.installments[1] + a.installments[2];
            if filed.after(by) {
            } else if earlier.0 == 0 {
                a.excused_january = true;
                a.notes.rule(
                    "Wis. Stat. §§ 71.09(14), 990.001(4)(b); Schedule U Instructions (2025), Line 17",
                    format!(
                        "the return was filed and paid on {}, by {}, and nothing was required by {} or earlier, so the payment with it counts as the January installment",
                        filed.long(),
                        by.long(),
                        a.dues[2].long()
                    ),
                );
            } else {
                a.notes.rule(
                    "Wis. Stat. § 71.09(14); Schedule U Instructions (2025), Line 17",
                    format!(
                        "the return was filed and paid on {}, by {}, but {} was required by {}, so the payment with it does not replace the January installment",
                        filed.long(),
                        by.long(),
                        earlier,
                        a.dues[2].long()
                    ),
                );
            }
        }
        Ok(a)
    }

    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Column>, Notes), Error> {
        let mut notes = Notes::default();
        let credits = if f.elections().withholding_as_withheld()
            && f.withheld_total() >= a.requirement.target()
        {
            // Schedule U stops at line 8 when withholding reaches the
            // required annual payment, so the election is never reached.
            notes.rule(
                "Schedule U Instructions (2025), Lines 8 and 18",
                format!(
                    "the {} withheld reaches the required annual payment, so it is treated as paid one-fourth on each due date and the election to credit it when withheld does not arise",
                    f.withheld_total()
                ),
            );
            estimated::credits(f, &a.dues, &self.y.shares)?
        } else if f.elections().withholding_as_withheld() {
            notes.rule(
                "Schedule U Instructions (2025), Line 18, code 08",
                format!(
                    "by election, the {} withheld is credited to the period it was actually withheld in",
                    f.withheld_total()
                ),
            );
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            notes.rule(
                "Schedule U Instructions (2025), Line 18",
                format!(
                    "the {} withheld is treated as paid one-fourth on each due date, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
            estimated::credits(f, &a.dues, &self.y.shares)?
        };
        let mut required = a.installments.clone();
        if a.excused_january {
            required[3] = USCents(0);
        }
        notes.rule(
            "Schedule U (2025), lines 17 to 24, and its instructions for lines 17, 22 and 23",
            "a payment counts first toward the installment of the period it was made in; an overpayment is carried back to the most recent earlier shortfall first, then forward; a payment after the January due date goes to the January shortfall first, then back",
        );
        Ok((schedule_u(&a.dues, &required, &credits), notes))
    }

    /// The columns as due dates. A January installment the return replaces
    /// is excused.
    fn installments(&self, a: &Assessment, cols: &[Column]) -> Result<Vec<Installment>, Error> {
        let mut out = Vec::with_capacity(cols.len());
        let mut needed = USCents(0);
        for (i, c) in cols.iter().enumerate() {
            needed += a.installments[i];
            let mut b = estimated::new_installment(i as i64 + 1, c.due);
            b.needed_by(needed)
                .credited_by(c.credited_by)
                .shortfall(c.shortfall());
            if a.excused_january && i == 3 {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok(out)
    }
}

/// Part of an installment's shortfall and the day it was paid, or the zero
/// Date if it never was.
#[derive(Clone, Copy, Debug, Default)]
struct Piece {
    amount: USCents,
    paid_on: Date,
}

/// One installment after Schedule U lines 16 to 24.
#[derive(Clone, Debug, Default)]
struct Column {
    due: Date,
    credited_by: USCents,

    /// Line 20.
    underpaid: USCents,
    /// Line 23.
    forward: USCents,

    /// Line 22: what was carried back or paid late, and when.
    paid: Vec<Piece>,
}

impl Column {
    /// What was unpaid on the due date: line 20 less line 23.
    fn shortfall(&self) -> USCents {
        self.underpaid - self.forward
    }

    fn outstanding(&self) -> USCents {
        let mut o = self.shortfall();
        for p in &self.paid {
            o -= p.amount;
        }
        o
    }

    /// The shortfall broken up by when it was paid, with the unpaid rest
    /// last and undated.
    fn pieces(&self) -> Vec<Piece> {
        let mut out = self.paid.clone();
        let o = self.outstanding();
        if o.0 > 0 {
            out.push(Piece {
                amount: o,
                paid_on: Date::default(),
            });
        }
        out
    }
}

/// Carries `p` back from column `from`, the most recent shortfall first,
/// and returns what is left.
fn back(cols: &mut [Column], from: i64, p: Piece) -> USCents {
    let mut left = p.amount;
    let mut j = from;
    while j >= 0 && left.0 > 0 {
        let c = &mut cols[j as usize];
        let o = c.outstanding();
        if o.0 > 0 {
            let pay = o.min(left);
            c.paid.push(Piece {
                amount: pay,
                paid_on: p.paid_on,
            });
            left -= pay;
        }
        j -= 1;
    }
    left
}

/// Applies credits to the installments as Schedule U lines 16 to 24 and
/// their instructions do.
///
/// A credit belongs to the period it falls in: after the previous due date,
/// up to and including this one. Each period's credits, in date order, first
/// cover that period's own installment. An overpayment is what is left,
/// dated by the credit that created it. Taking the periods in order, each
/// overpayment is carried back to the earlier shortfalls, the most recent
/// first (line 22), and what remains is carried forward to the next periods
/// with an underpayment (line 23). A credit after the last due date is a
/// late payment and goes to the last shortfall first, then back.
///
/// This is not [`estimated::apply`], which pays the oldest shortfall first
/// and lets a credit pay an earlier shortfall before its own period's.
fn schedule_u(dues: &[Date], required: &[USCents], credits: &[Payment]) -> Vec<Column> {
    let mut cs = credits.to_vec();
    cs.sort_by(|x, y| x.on().compare(y.on()).cmp(&0));
    let n = dues.len();
    let mut cols = vec![Column::default(); n];
    let mut covered = vec![USCents(0); n];
    let mut over: Vec<Vec<Piece>> = vec![Vec::new(); n];
    let mut late = Vec::new();
    let mut k = 0;
    for c in &cs {
        while k < n && c.on().after(dues[k]) {
            k += 1;
        }
        if k == n {
            late.push(Piece {
                amount: c.amount(),
                paid_on: c.on(),
            });
            continue;
        }
        let take = c.amount().min(required[k] - covered[k]);
        covered[k] += take;
        let rest = c.amount() - take;
        if rest.0 > 0 {
            over[k].push(Piece {
                amount: rest,
                paid_on: c.on(),
            });
        }
    }
    for i in 0..n {
        cols[i] = Column {
            due: dues[i],
            underpaid: required[i] - covered[i],
            ..Column::default()
        };
        for c in &cs {
            if !c.on().after(dues[i]) {
                cols[i].credited_by += c.amount();
            }
        }
    }
    for (i, ps) in over.iter().enumerate() {
        for &p in ps {
            let mut left = back(&mut cols, i as i64 - 1, p);
            let mut j = i + 1;
            while j < n && left.0 > 0 {
                let fw = cols[j].shortfall().min(left);
                cols[j].forward += fw;
                left -= fw;
                j += 1;
            }
        }
    }
    for p in late {
        back(&mut cols, n as i64 - 1, p);
    }
    cols
}

impl EstimatedTax for WisconsinEstimated {
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
                    "with no {} Wisconsin return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (cols, notes) = self.ledger(f, &a)?;
        let out = self.installments(&a, &cols)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "wisconsintaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Wisconsin return to measure against, there is no target to figure a penalty from",
                    self.y.year - 1
                ),
            ));
        }
        let y = &self.y;
        let mut stop = as_of;
        if y.stop.before(stop) {
            stop = y.stop;
        }
        let mut paid_in_full = false;
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(stop) {
            (stop, paid_in_full) = (filed, true);
        }
        let waiver_opt = f.positions().waiver();
        let waiver_answered = waiver_opt.is_some();
        let waiver = waiver_opt.unwrap_or_default();
        let (cols, mut notes) = self.ledger(f, &a)?;
        let schedule = self.installments(&a, &cols)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        for (i, c) in cols.iter().enumerate() {
            if (a.excused_january && i == 3) || c.shortfall().0 <= 0 {
                continue;
            }
            let mut lb = estimated::new_penalty_line(i as i64 + 1, c.due, c.shortfall());
            let mut last_day = c.due;
            for p in c.pieces() {
                let mut to = stop;
                if !p.paid_on.is_zero() && p.paid_on.before(stop) {
                    to = p.paid_on;
                }
                if !to.after(c.due) {
                    continue;
                }
                if to.after(last_day) {
                    last_day = to;
                }
                let (parts, unrated) = estimated::simple_daily(p.amount, c.due, to, &y.rates)?;
                if !unrated.is_zero() {
                    // The rate is statutory and the series runs to the stop.
                    return Err(Error::Invalid(format!(
                        "wisconsintaxcalc: no rate for {unrated}"
                    )));
                }
                for part in parts {
                    lb.part(part);
                }
            }
            let line = lb.days(last_day.days_since(c.due)).build()?;
            pb.line(line);
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "Wis. Stat. § 71.09(13)(a); Schedule U (2025), Part I, line 8",
                format!(
                    "this year's Wisconsin tax was not given, so the interest is figured against {} of the {} tax alone; the required payment is the smaller of that and 90% of this year's tax, so {} is the most the interest can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "Wis. Stat. § 71.84(1); Schedule U (2025), line 26",
                format!(
                    "the balance due on the return was paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        let rate = y
            .rates
            .periods()
            .first()
            .map(|p| p.rate())
            .unwrap_or_default();
        notes.rule(
            "Wis. Stat. § 71.84(1); Schedule U (2025), lines 25 to 29",
            format!(
                "each shortfall is charged {} a year, days ÷ 365, from its due date until paid or {}; the interest to {} is {}",
                rate,
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if let Some(k) = f.positions().waiver()
            && k != WaiverKind::None
        {
            let code = if k == WaiverKind::RetiredOrDisabled {
                "06"
            } else {
                "05"
            };
            notes.rule(
                "Wis. Stat. § 71.09(11)(c), (d); Schedule U Instructions (2025), Waiver of Underpayment Interest",
                format!(
                    "a waiver for {k} is applied for with code {code} and decided by the department; the interest shown is before it"
                ),
            );
        }
        // § 71.09(11)(c) and (d) grant the federal pair, at 62.
        if let Some(o) = estimated::standard_waiver(f, "the Department of Revenue", 62)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        if a.extras.active_duty_outside_us() == Some(true) {
            notes.rule(
                "Schedule U Instructions (2025), Waiver of Underpayment Interest, code 07; Wis. Stat. § 71.09(2)",
                "active duty outside the continental United States is applied for as a waiver with code 07; the interest shown is before it, and § 71.09(2) says none is owed",
            );
        }
        if p.total().0 > 0 && !waiver_answered {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Will you ask to waive interest, as for active duty outside the continental US?",
                "The department can waive it for a casualty, disaster or other unusual event too. Retiring after 62 or disability can qualify too, with reasonable cause.",
            );
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    /// Written by hand and checked against research/estimated-payments/wi.md.
    fn explanation(&self) -> Explanation {
        estimated::new_explanation(
            "Wisconsin expects estimated payments if $500 or more would be due for 2026. The $500 is the tax left after withholding and credits. It charges underpayment interest of 12% a year on each quarter's shortfall.",
        )
        .paragraph("You avoid interest once withholding and estimated payments reach a target. It is the smaller of 90% of this year's Wisconsin tax or 100% of last year's. Last year's figure needs a Wisconsin return that covered 12 months. There is no 110% figure for higher incomes.")
        .paragraph("There are four equal payments, due April 15, June 15 and September 15. The last is due January 15 of the next year. A date on a weekend or Wisconsin legal holiday becomes the next business day. Withholding counts as paid a quarter on each date. You can instead show when it was actually withheld. With uneven income, Schedule U's annualized method can lower the early quarters.")
        .paragraph("Interest runs daily on each quarter's shortfall until it is paid. It stops on April 15 of the next year. A payment first covers the quarter it was made in. Only the extra goes back to an earlier shortfall, the most recent one first. A payment after January 15 goes to the January shortfall first. Schedule U expects a quarter from April 15, even if you first need to pay later. Only the annualized method lowers those earlier quarters.")
        .paragraph("There is no interest if this year's tax less withholding is under $500. There is also none if you had no Wisconsin tax for all 12 months of last year. You must have been a full-year Wisconsin resident that year. A federal disaster extension also means no interest. Filing and paying in full by January 31 replaces the January payment. It does so only if nothing was due by the earlier dates. For 2026 the date is February 1, 2027, because January 31 is a Sunday. You can apply for a waiver for a casualty or disaster. You can also apply for retiring after 62 or becoming disabled.")
        .versus_federal("Wisconsin differs from federal in six ways that change a number. Its threshold is $500, not $1,000, and it has no 110% figure at higher incomes. Its 12% rate is fixed by law. A payment first covers its own quarter, not the oldest shortfall as in federal. The no-tax-last-year exception needs full-year residence. Filing by January 31 replaces the last payment only if nothing was due earlier.")
        .source("Wis. Stat. §§ 71.09, 71.84, 990.001(4), 995.20")
        .source("2026 Form 1-ES Instructions (D-101A, R. 1-26)")
        .source("2025 Schedule U (D-104, R. 05-25) and its instructions (D-114, R. 11-25)")
        .build()
        .unwrap_or_default()
    }
}
