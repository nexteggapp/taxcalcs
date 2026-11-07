//! Minnesota's estimated tax rules, Minn. Stat. §§ 289A.25 and 270C.40, as
//! the shared [`estimated::EstimatedTax`] trait. Ported from estimated.go.
//!
//! Minnesota follows the federal shape: four 25% installments, the lesser of
//! 90% of this year's tax or 100% of last year's, and 110% above $150,000 of
//! last year's AGI. It differs in the $500 threshold, the single rate fixed for
//! the whole tax year, refundable credits counted as payments, and the
//! no-liability exception, which needs a full year of Minnesota residence.
//!
//! The figure follows the 2026 Schedule M15, marked "FINAL DRAFT — 10/1/26",
//! and the result's Divergences say where the statute reads otherwise. No act
//! since 2005 changed §§ 289A.25 or 270C.40 (research/estimated-payments/mn.md),
//! so the form does not predate the law it describes.
//!
//! Not reported in Required, because no 2026 figure turns on them: the 2025
//! M15 prints 7% where § 270C.40 gives 8% for tax year 2025, and farmers are
//! not built, so the form's 66.7% against the statute's 66⅔% is not used.

use std::any::Any;
use std::collections::HashSet;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    LedgerEntry, Penalty, RateSeries, Requirement, WaiverKind,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// Minnesota's postal code.
const JURISDICTION: Jurisdiction = estimated::MINNESOTA;

/// The facts Minnesota's rules read that no other jurisdiction does. Carry
/// them in [`estimated::FactsBuilder::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    refundable_credits: Option<USCents>,
    step_on_share: bool,
    step_answered: bool,
}

impl EstimatedFacts {
    /// This year's refundable credits, Form M1 line 22, if given. Schedule
    /// M15 counts them with withholding on line 2, and § 289A.25 subd. 8
    /// treats them as paid evenly on the four due dates.
    pub fn refundable_credits(&self) -> Option<USCents> {
        self.refundable_credits
    }

    /// Whether the $150,000 test on last year's AGI reads the Minnesota share
    /// of AGI rather than federal AGI, and whether the caller said, as
    /// `(on_share, answered)`. § 289A.25 subd. 5(2)(i) uses the Minnesota
    /// share "for an individual who is not a Minnesota resident for the
    /// entire year" without saying which year.
    pub fn prior_year_step_on_minnesota_share(&self) -> (bool, bool) {
        (self.step_on_share, self.step_answered)
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
    /// Sets this year's refundable credits, Form M1 line 22.
    pub fn refundable_credits(&mut self, a: USCents) -> &mut Self {
        self.e.refundable_credits = Some(a);
        self
    }

    /// Answers which AGI the $150,000 test reads for a filer who was not a
    /// Minnesota resident all of one of the two years.
    pub fn prior_year_step_on_minnesota_share(&mut self, on_share: bool) -> &mut Self {
        self.e.step_on_share = on_share;
        self.e.step_answered = true;
        self
    }

    /// Refuses negative refundable credits.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        if self.e.refundable_credits.unwrap_or_default().0 < 0 {
            return Err(Error::Invalid(
                "minnesotataxcalc: refundable credits cannot be negative".to_string(),
            ));
        }
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// M15 line 3: no penalty when tax less withholding and credits is under
    /// it.
    pub(crate) threshold: USCents,

    /// M15 lines 4 and 5: 90% of this year's tax, 100% of last year's, or
    /// 110% above `step`.
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,
    pub(crate) prior_year_high: Rate,
    pub(crate) step: USCents,

    /// The least prior-year tax a nonresident or part-year resident needs
    /// for the prior-year test.
    pub(crate) nonresident_minimum: USCents,

    pub(crate) shares: Vec<Rate>,
    pub(crate) annualised: Vec<Rate>,
    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    pub(crate) period_ends: Vec<Date>,

    /// The M15 line 14 rule: tax paid with a return filed by
    /// `file_and_pay_by` counts as paid on `deemed_paid`.
    pub(crate) file_and_pay_by: Date,
    pub(crate) deemed_paid: Date,

    pub(crate) stop: Date,

    /// The one rate § 270C.40 subd. 4 fixes for the whole tax year, over
    /// every day a 2026 underpayment can run.
    pub(crate) rates: RateSeries,
}

/// Returns Minnesota's estimated tax rules for a tax year, or
/// [`estimated::Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(MinnesotaEstimated { y }))
}

struct MinnesotaEstimated {
    y: EstimatedYear,
}

#[derive(Clone, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    installments: Vec<USCents>,
    refundable: USCents,
    filed_by_jan31: bool,
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

impl MinnesotaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "minnesotataxcalc: the facts carry {}, not minnesotataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "minnesotataxcalc: these are Minnesota's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "minnesotataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "farmers and commercial fishermen make one payment by January 15 at 66.7%, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let Some(&end) = self.y.period_ends.get(i) else {
                    return Err(Error::Invalid(format!(
                        "minnesotataxcalc: M15 annualized worksheet has no period {}",
                        i + 1
                    )));
                };
                if p.end() != end {
                    return Err(Error::Invalid(format!(
                        "minnesotataxcalc: M15 annualized worksheet period {} ends on {}, not {}",
                        i + 1,
                        end,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's publications and the statutes part ways on a
    /// rule every Minnesota computation uses. The first five and the last are
    /// in the research file; the line 17 rule was found reading the 2026 M15
    /// against § 289A.25 subd. 3. Each says whether it changes anything for
    /// `f`.
    fn divergences(&self, f: &Facts, a: &Assessment) -> Result<Vec<Divergence>, Error> {
        let y = &self.y;
        let (mut short, mut part_paid, mut early_return, mut february1, mut waiver_asked) =
            (false, false, false, false, false);
        if !a.exempt && a.requirement.target_known() {
            let (ledger, _) = self.ledger(f, a)?;
            for (i, l) in ledger.iter().enumerate() {
                if l.shortfall().0 <= 0 {
                    continue;
                }
                short = true;
                let mut ends = HashSet::new();
                for piece in l.pieces() {
                    let paid_on = piece.paid_on();
                    let on = paid_on.unwrap_or_default();
                    ends.insert(on);
                    // An earlier shortfall still unpaid on January 15 runs on
                    // under the statute.
                    if a.filed_by_jan31 && i < 3 && (paid_on.is_none() || on.after(y.deemed_paid)) {
                        early_return = true;
                    }
                }
                part_paid = part_paid || ends.len() > 1;
            }
            let next = y.calendar.next_business_day(y.file_and_pay_by);
            february1 = next != y.file_and_pay_by && f.return_filed() == next;
            let waiver = f.positions().waiver();
            waiver_asked = short && waiver.is_some_and(|w| w != WaiverKind::None);
        }
        let paid = short && !f.payments().is_empty();
        let mut out = Vec::new();
        for b in [
            working::new_divergence("When a payment counts as made")
                .form("A payment can be due on a weekend or holiday. If so, the department must receive it by the next business day.",
                    "Minnesota Department of Revenue, Estimated Tax Payments for Individuals (updated January 7, 2026)")
                .statute("A payment sent by US mail counts as made on its postmark date. An electronic one counts as made on its confirmation time-and-date stamp.",
                    "Minn. Stat. § 270C.395 subd. 1")
                .uses(Reading::Form).affects(paid).clone(),
            working::new_divergence("A shortfall paid in part")
                .form("Line 17 takes the date the underpayment was paid in full. The whole shortfall is charged until then.",
                    "2026 Schedule M15, lines 16 to 21 and the Line 17 instructions")
                .statute("The underpayment period ends, for any part of it, on the date that part is paid.",
                    "Minn. Stat. § 289A.25 subd. 3(a)(2)")
                .uses(Reading::Form).affects(part_paid).clone(),
            working::new_divergence("A return filed and paid by January 31")
                .form("Tax paid with a return filed by Jan 31, 2027 counts as paid on Jan 15, 2027. Every shortfall still unpaid on that date stops being charged that day.",
                    "2026 Schedule M15, Line 14 instructions")
                .statute("No penalty is charged on a shortfall in the fourth installment. Earlier installments are charged until paid.",
                    "Minn. Stat. § 289A.25 subd. 9")
                .uses(Reading::Form).affects(early_return).clone(),
            working::new_divergence("January 31, 2027 is a Sunday")
                .form("The form prints Jan 31, 2027 and says nothing of moving it for the weekend. The date is therefore not moved.",
                    "2026 Schedule M15, Line 14 instructions")
                .statute("A document due on a Saturday, Sunday or legal holiday can be filed later. It is on time if filed the next day that is none of these. A return filed and paid on Feb 1, 2027 would therefore be on time.",
                    "Minn. Stat. § 645.151")
                .uses(Reading::Form).affects(february1).clone(),
            working::new_divergence("The optional short method")
                .form("The short method charges 2% of the year's underpayment. It subtracts 0.000192 a day for amounts paid before Apr 15, 2027. The form also offers the regular method.",
                    "2026 Schedule M15, lines 7 to 12")
                .statute("The charge is the year's rate on each underpayment for the time it's unpaid. At 7%, that is about 4.66% for four quarters held to Apr 15, 2027.",
                    "Minn. Stat. § 289A.25 subd. 2; § 270C.40 subd. 4")
                .uses(Reading::Statute).affects(short).clone(),
            working::new_divergence("Waivers for retirement, disability or casualty")
                .form("If the IRS waived your federal penalty, you don't complete Schedule M15. Retirement, disability, casualty, disaster and unusual circumstances all count.",
                    "2026 Schedule M15 instructions, Exceptions to the Penalty")
                .statute("The federal waiver covers retirement, disability, casualty or disaster. No penalty is charged to the extent the commissioner finds it applies.",
                    "Minn. Stat. § 289A.25 subd. 7")
                .uses(Reading::Form).affects(waiver_asked).clone(),
            working::new_divergence("Who counts as a farmer")
                .form("A farmer or commercial fisherman meets a two-thirds test on this year only. At least two-thirds of annual gross income must come from farming or fishing.",
                    "2026 Schedule M15 instructions, Farmers and commercial fishermen")
                .statute("The federal definition applies. It also counts two-thirds of gross income shown on last year's return.",
                    "Minn. Stat. § 289A.25 subd. 10; 26 U.S.C. § 6654(i)(2)(B)")
                // Last year's farm share is not among the facts.
                .uses(Reading::Form).affects(false).clone(),
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

    fn measure(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let last = y.year - 1;

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let refundable_opt = extras.refundable_credits();
        let refundable_known = refundable_opt.is_some();
        let refundable = refundable_opt.unwrap_or_default();
        a.refundable = refundable;
        let credited = withheld + refundable;
        let has_prior = f.prior_year().is_some();
        let prior = f.prior_year().unwrap_or_default();
        let prior_figures = prior.figures().unwrap_or_default();
        let prior_tax = prior_figures.tax().unwrap_or_default();

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
                "Minn. Stat. § 289A.25 subd. 5(1); 2026 Schedule M15, line 4",
                format!(
                    "{} of this year's Minnesota tax of {tax} is {}",
                    y.this_year_share,
                    this_year.amount()
                ),
            );
        }

        // Residence in each year decides the $1 minimum and which AGI the
        // step reads. This year's residence counts as full-year when not
        // given.
        let prior_resident_opt = prior.full_year_resident();
        let prior_answered = prior_resident_opt.is_some();
        let prior_resident = prior_resident_opt.unwrap_or_default();
        let this_resident = f.residence().is_empty()
            || estimated::resident_all_year(&f.residence(), &JURISDICTION, y.year);
        let nonresident = (prior_answered && !prior_resident) || !this_resident;

        let unavailable = |why: String| {
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
                .unavailable(why)
                .build()
        };
        let prior_opt = if !has_prior {
            unavailable(format!("Needs your {last} Minnesota return"))?
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Minnesota return"))?
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Minnesota return covered {} months, not 12",
                prior.months()
            ))?
        } else if prior_tax.0 == 0 {
            unavailable(format!("Your {last} return showed no Minnesota tax"))?
        } else if nonresident && prior_tax < y.nonresident_minimum {
            unavailable(format!(
                "This test has a minimum for a part-year resident or nonresident. You need at least {} of {last} Minnesota tax for it",
                y.nonresident_minimum.whole()
            ))?
        } else {
            let (mut on_share, answered) = extras.prior_year_step_on_minnesota_share();
            if !answered {
                on_share = prior_answered && !prior_resident;
                if nonresident {
                    let which = if on_share {
                        "your Minnesota share"
                    } else {
                        "your federal AGI"
                    };
                    a.notes.ask(
                        PositionKind::JurisdictionFact,
                        format!(
                            "Should the {} step use your Minnesota share of AGI or your federal AGI?",
                            y.step.whole()
                        ),
                        format!(
                            "For anyone not resident all year, the law uses the share but names no year. The figures go by your {last} residence and use {which}."
                        ),
                    );
                }
            }
            let (agi_opt, agi_name, agi_ask) = if on_share {
                (
                    prior_figures.state_agi(),
                    "Minnesota share of AGI",
                    "Minnesota share of adjusted gross income",
                )
            } else {
                (
                    prior_figures.federal_agi(),
                    "federal AGI",
                    "federal adjusted gross income",
                )
            };
            let agi = agi_opt.unwrap_or_default();
            let mut rate = y.prior_year_share;
            let mut why = format!("{last} {agi_name} of {agi} is not more than {}", y.step);
            if agi_opt.is_none() {
                rate = y.prior_year_high;
                why = format!("{last} {agi_name} was not given");
                a.notes.ask(
                    PositionKind::AdjustedGrossIncome,
                    format!("What was your {last} {agi_ask}?"),
                    format!(
                        "Until then, the last-year option is {} of last year's tax. At {} or less it would be {}.",
                        y.prior_year_high,
                        y.step.whole(),
                        y.prior_year_share
                    ),
                );
            } else if agi > y.step {
                rate = y.prior_year_high;
                why = format!("{last} {agi_name} of {agi} is more than {}", y.step);
            }
            let amount = rate.of(prior_tax);
            a.notes.rule(
                "Minn. Stat. § 289A.25 subd. 5(2); 2026 Schedule M15, line 5",
                format!(
                    "{why}, so the prior-year test is {rate} of the {prior_tax} tax on the {last} return, or {amount}"
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, rate, amount).build()?
        };

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Minnesota tax to be?", y.year),
                format!(
                    "It sets the {} option and the {} test.",
                    y.this_year_share,
                    y.threshold.whole()
                ),
            );
        }

        // § 289A.25 subd. 6: no liability last year, a 12-month year, and a
        // full year of Minnesota residence.
        let no_liability = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax.0 == 0) || prior.not_required_to_file());
        if no_liability && !prior_answered {
            a.notes.ask(
                PositionKind::Residency,
                format!("Did you live in Minnesota all of {last}?"),
                format!(
                    "If you did, having no Minnesota tax for {last} means no penalty for {}.",
                    y.year
                ),
            );
        }

        if no_liability && prior_answered && prior_resident {
            a.exempt = true;
            let none =
                estimated::new_exception("No tax due last year as a full-year resident").build()?;
            rb.required(
                false,
                format!(
                    "You had no Minnesota tax for {last}, a full year as a Minnesota resident. That means no estimated payments are needed for {}.",
                    y.year
                ),
            )
            .exempt(none);
            a.notes.rule(
                "Minn. Stat. § 289A.25 subd. 6; 2026 Schedule M15 instructions",
                format!(
                    "the {last} return showed no liability for a full 12 months of Minnesota residence, so there is no penalty for {}",
                    y.year
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "A target needs your {} Minnesota tax or a full-year {last} return. There's no target yet.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && credited >= target {
            rb.required(
                false,
                format!(
                    "Your target is {} of your {last} tax, {}. Your withholding and refundable credits of {} already reach it. No estimated payments are needed, whatever your {} tax turns out to be.",
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
                    "Your target is {} of your {last} tax, {}. After {} of withholding and credits, pay {} in estimated payments. Pay a quarter by each date. This holds whatever your {} tax turns out to be.",
                    prior_opt.percent(),
                    target.whole(),
                    credited.whole(),
                    (target - credited).whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if tax - credited < y.threshold {
            a.exempt = true;
            let small = estimated::exception_below(
                "Tax less withholding and credits",
                tax - credited,
                y.threshold,
            )?;
            let so = if tax < credited {
                "That means no estimated payments are needed.".to_string()
            } else {
                format!(
                    "That's under {}, so no estimated payments are needed.",
                    y.threshold.whole()
                )
            };
            rb.required(
                false,
                format!(
                    "Your withholding and refundable credits are {}. Your Minnesota tax of {} less that {}. {so}",
                    credited.whole(),
                    tax.whole(),
                    estimated::leaves(tax - credited)
                ),
            )
            .exempt(small);
            a.notes.rule(
                "Minn. Stat. § 289A.25 subd. 4; 2026 Schedule M15, line 3",
                format!(
                    "{tax} of tax less {credited} of withholding and credits is {}, under {}, so there is no penalty",
                    tax - credited,
                    y.threshold
                ),
            );
        } else if credited >= target {
            rb.required(
                false,
                format!(
                    "Your Minnesota withholding and refundable credits are {}. That already reaches the {} target, so no estimated payments are needed.",
                    credited.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your withholding and refundable credits are {}. Your Minnesota tax of {} less that leaves {}. That's {} or more, so estimated payments are needed. You need {} of them to reach the {} target. Pay a quarter by each date.",
                    credited.whole(),
                    tax.whole(),
                    (tax - credited).whole(),
                    y.threshold.whole(),
                    (target - credited).whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        // Under an exception last year's return can't change the target.
        if !has_prior && !a.exempt {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Minnesota return, and what was its tax?"),
                format!(
                    "With it, last year's tax could be a lower target than {} of this year's.",
                    y.this_year_share
                ),
            );
        }
        if !a.exempt && !refundable_known {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "What are your {} Minnesota refundable credits, from Form M1 line 22?",
                    y.year
                ),
                "They count as paid evenly on the four dates; until then only withholding counts.",
            );
        }

        let cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "Minn. Stat. § 289A.25 subds. 3(b), 5; 2026 Schedule M15, line 13",
            format!(
                "installments of 25% of {} are due {}, {}, {} and {}",
                a.requirement.target(),
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );

        if f.elections().annualised() && !a.exempt {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &y.annualised, &a.installments)?;
            a.notes.rule(
                "Minn. Stat. § 289A.25 subd. 5(3); 2026 Schedule M15, Worksheet - Annualized Income Installment",
                format!(
                    "under the annualized method the installments are {}, {}, {} and {}: 22.5%, 45%, 67.5% and 90% of the annualized tax, \
                     less earlier installments, and never more than the regular 25% plus any reduction carried forward",
                    ann[0], ann[1], ann[2], ann[3]
                ),
            );
            a.installments = ann;
        }

        // The M15 line 14 rule. January 31, 2027 is a Sunday; the form prints
        // January 31 and says nothing of moving it, so it is not moved.
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(y.file_and_pay_by) && !a.exempt {
            a.filed_by_jan31 = true;
            a.notes.rule(
                "2026 Schedule M15, Line 14 instructions; Minn. Stat. § 289A.25 subd. 9",
                format!(
                    "the return was filed and paid on {}, by {}, so the tax paid with it counts as paid on {}",
                    filed.long(),
                    y.file_and_pay_by.long(),
                    y.deemed_paid.long()
                ),
            );
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }
        Ok(a)
    }

    /// Applies withholding, refundable credits and payments to the
    /// installments.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let mut credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &self.y.shares)?
        };
        // § 289A.25 subd. 8: refundable credits are paid evenly on the due
        // dates.
        let cum = estimated::cumulative_shares(a.refundable, &self.y.shares)?;
        for (i, amt) in estimated::increments(&cum).into_iter().enumerate() {
            if amt.0 == 0 {
                continue;
            }
            credits.push(estimated::new_payment().on(a.dues[i]).amount(amt).build()?);
        }
        let ledger = estimated::apply(&a.dues, &a.installments, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "Minn. Stat. § 289A.25 subd. 8; 2026 Schedule M15, Line 14 instructions",
                format!(
                    "the {} withheld is credited on the dates it was withheld, as shown; the {} of refundable credits is treated as paid evenly on the due dates",
                    f.withheld_total(),
                    a.refundable
                ),
            );
        } else {
            notes.rule(
                "Minn. Stat. § 289A.25 subd. 8; 2026 Schedule M15, Line 14 instructions",
                format!(
                    "the {} withheld and the {} of refundable credits are treated as paid in four equal parts on the due dates, whenever they actually came",
                    f.withheld_total(),
                    a.refundable
                ),
            );
        }
        Ok((ledger, notes))
    }

    /// The installments after the credits, with the ledger they come from. A
    /// return filed and paid by January 31 excuses the fourth.
    fn schedule_with(
        &self,
        f: &Facts,
        a: &Assessment,
    ) -> Result<(Vec<Installment>, Vec<LedgerEntry>, Notes), Error> {
        let (ledger, notes) = self.ledger(f, a)?;
        let mut out = Vec::with_capacity(ledger.len());
        for (i, l) in ledger.iter().enumerate() {
            let mut b = estimated::new_installment(i as i64 + 1, l.due());
            b.needed_by(l.needed_by())
                .credited_by(l.credited_by())
                .shortfall(l.shortfall());
            if a.filed_by_jan31 && i == 3 {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok((out, ledger, notes))
    }
}

impl EstimatedTax for MinnesotaEstimated {
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
                    "with no {} Minnesota return to measure against, there is no target to schedule",
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
                "minnesotataxcalc: a penalty needs the date to figure it to".to_string(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Minnesota return to measure against, there is no target to figure a penalty from",
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
        if a.filed_by_jan31 && y.deemed_paid.before(stop) {
            stop = y.deemed_paid;
        }
        let waiver_answer = f.positions().waiver();
        let waiver = waiver_answer.unwrap_or_default();
        let (sched, ledger, mut notes) = self.schedule_with(f, &a)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        // M15 lines 16 to 21: each column's whole underpayment, from its due
        // date to the date it was paid in full or the stop, at the year's
        // rate.
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &sched);
        let mut unrated = Date::default();
        for l in &ledger {
            if l.shortfall().0 <= 0 {
                continue;
            }
            let mut to = l.due();
            for piece in l.pieces() {
                let mut on = match piece.paid_on() {
                    Some(on) => on,
                    None => stop,
                };
                if on.after(stop) {
                    on = stop;
                }
                if on.after(to) {
                    to = on;
                }
            }
            let mut lb = estimated::new_penalty_line(l.number(), l.due(), l.shortfall());
            if to.after(l.due()) {
                let (parts, gap) = estimated::simple_daily(l.shortfall(), l.due(), to, &y.rates)?;
                if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                    unrated = gap;
                }
                for p in parts {
                    lb.part(p);
                }
            }
            let line = lb.days(to.days_since(l.due())).build()?;
            pb.line(line);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            notes.rule(
                "Minn. Stat. § 289A.25 subd. 5",
                format!(
                    "this year's Minnesota tax was not given, so the penalty is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "Minn. Stat. § 289A.25 subd. 3(a); 2026 Schedule M15, Line 17 instructions",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    f.return_filed().long()
                ),
            );
        }
        notes.rule(
            "Minn. Stat. §§ 289A.25 subds. 2, 3, 270C.40 subd. 4; 2026 Schedule M15, lines 16 to 22",
            format!(
                "each shortfall is charged the {} rate for the days from its due date until it was paid in full or {}, \
                 with each payment applied to the oldest shortfall first; the penalty to {} is {}",
                y.year,
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if !unrated.is_zero() {
            p = p.to_builder().rated_through(unrated.add_days(-1)).build()?;
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The rate for the days after {} isn't published yet.",
                    unrated.add_days(-1).long()
                ),
            );
        }
        // § 289A.25 subd. 7 adopts the federal waivers of § 6654(e)(3).
        if let Some(o) = estimated::standard_waiver(f, "Minnesota's Department of Revenue", 62)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        if p.total().0 > 0 && waiver_answer.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking Minnesota to waive the penalty?",
                "It follows the IRS's decision, for a casualty, disaster or unusual circumstance. The same goes for retirement after 62 or disability with reasonable cause.",
            );
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        MINNESOTA_EXPLANATION.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/mn.md.
static MINNESOTA_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Minnesota expects estimated payments once $500 or more would be due. That figure is after withholding and refundable credits. The penalty is interest on each quarter's shortfall. It uses one rate for the year, 7% for 2026.")
        .paragraph("Withholding, refundable credits and estimated payments count toward a target. It is the smaller of 90% of this year's Minnesota tax or 100% of last year's. Reaching it keeps you clear of the penalty. Last year's figure becomes 110% if last year's federal AGI was over $150,000. There is no lower figure for married filing separately. Last year's figure needs a full-year return that showed some Minnesota tax.")
        .paragraph("The payments are a quarter each. They are due April 15, June 15, September 15 and January 15 of the next year. Withholding and refundable credits count as paid evenly across those dates. This holds unless you show when they were actually paid. A return filed with the tax paid by January 31 counts as paid on January 15.")
        .paragraph("No penalty is due if this year's tax less withholding and credits is under $500. The same holds if you had no Minnesota tax last year and lived there all of it. If the IRS waives your federal penalty, Minnesota does too. That covers retiring, becoming disabled, a casualty or a disaster.")
        .paragraph("The penalty is figured by the day on Schedule M15. It runs from each due date until the shortfall is paid in full. It stops on April 15 of the next year. The rate is set once from the prime rate and holds for the whole tax year. An annualized method can lower early payments if income comes late in the year.")
        .versus_federal("Minnesota's threshold is $500, not $1,000. The $150,000 step has no $75,000 figure for married filing separately. Refundable credits count as payments spread over the year. The no-liability exception needs a full year of Minnesota residence. One rate covers the whole tax year. A return filed by January 31 counts as paid on January 15.")
        .source("Minn. Stat. §§ 289A.25, 270C.40, 270C.395, 645.151")
        .source("2026 Schedule M15 and its instructions (final draft, October 1, 2026)")
        .source("Minnesota Department of Revenue, Estimated Tax Payments for Individuals (updated January 7, 2026)")
        .source("Minnesota Department of Revenue, Penalties and Interest for Individuals (interest rate table)")
        .build()
        .unwrap_or_default()
});
