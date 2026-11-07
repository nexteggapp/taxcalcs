//! North Carolina's estimated tax rules, N.C. Gen. Stat. § 105-163.15, with
//! the rate under § 105-241.21, as the shared [`estimated::EstimatedTax`]
//! interface. North Carolina calls the charge interest.
//!
//! North Carolina follows the federal rules closely: four 25% installments,
//! the lesser of 90% of this year's tax or 100% of last year's, the $1,000
//! threshold the statute takes from 26 U.S.C. § 6654(e), and payments applied
//! to the earliest installment first. It differs in having no 110% tier, a
//! no-liability exception with no twelve-month or residence condition, no
//! waiver for casualty, retirement or disability, and a rate the Secretary
//! sets for each half-year.
//!
//! The figure follows Form D-422 for 2025, the latest published, with the
//! 2026 dates and rates, and the result's divergences say where the statute
//! reads otherwise. § 105-163.15 has not been amended since 2007
//! (research/estimated-payments/nc.md), so the form does not predate the law
//! it describes.
//!
//! The rate from January 1, 2027 is not published, so those days are not
//! charged and the penalty reports the gap.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    LedgerEntry, Month, Penalty, RateSeries, Requirement, TargetOptionBuilder, WaiverKind,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// North Carolina's postal code.
const JURISDICTION: Jurisdiction = estimated::NORTH_CAROLINA;

/// The facts North Carolina's rules read that no other jurisdiction does.
/// Carry them in [`estimated::Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    farmer_last: Option<bool>,
    moves: Option<bool>,
}

impl EstimatedFacts {
    /// Whether at least two-thirds of the gross income on last year's return
    /// was from farming or fishing, which puts this year under the farmers'
    /// rules, or `None` when the caller did not say.
    pub fn farmer_last_year(&self) -> Option<bool> {
        self.farmer_last
    }

    /// Whether a return filed and paid on Monday, February 1, 2027 counts as
    /// filed by January 31, a Sunday, or `None` when the caller did not say.
    /// No source says.
    pub fn january_thirty_first_moves(&self) -> Option<bool> {
        self.moves
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts an [`EstimatedFacts`].
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Says whether two-thirds of last year's gross income was from farming
    /// or fishing.
    pub fn farmer_last_year(&mut self, farmer: bool) -> &mut Self {
        self.e.farmer_last = Some(farmer);
        self
    }

    /// Says whether a return filed on the business day after January 31
    /// counts as filed by it.
    pub fn january_thirty_first_moves(&mut self, moves: bool) -> &mut Self {
        self.e.moves = Some(moves);
        self
    }

    /// Returns the facts.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// D-422 Part I line 6, the § 6654(e) amount § 105-163.15(f) adopts.
    pub(crate) threshold: USCents,

    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,
    pub(crate) shares: Vec<Rate>,
    pub(crate) annualised: Vec<Rate>,
    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    pub(crate) period_ends: Vec<Date>,

    /// D-422 line 17: the tax paid with a return filed by `file_and_pay_by`
    /// counts in the January column, as paid on `deemed_paid`.
    pub(crate) file_and_pay_by: Date,
    pub(crate) deemed_paid: Date,

    pub(crate) stop: Date,
    pub(crate) rates: RateSeries,
}

/// The year this build carries, or `None`.
fn estimated_year(year: i64) -> Option<Result<EstimatedYear, Error>> {
    match year {
        2026 => Some(crate::estimated_y2026::year_2026()),
        _ => None,
    }
}

/// Returns North Carolina's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    match estimated_year(year) {
        Some(y) => Ok(Box::new(NorthCarolinaEstimated { y: y? })),
        None => Err(estimated::no_rules(&JURISDICTION, year)),
    }
}

struct NorthCarolinaEstimated {
    y: EstimatedYear,
}

#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    installments: Vec<USCents>,
    filed_by_jan31: bool,
    notes: Notes,
}

/// Go's `%T` of a value handed in as a jurisdiction's extras, for the types
/// a caller is likely to have passed.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<String>() || v.is::<&str>() {
        "string"
    } else if v.is::<i64>() {
        "int"
    } else {
        "an unknown type"
    }
}

/// A plain error, as Go's `errors.New` or `fmt.Errorf` without `%w`.
fn invalid(text: String) -> Error {
    Error::Invalid(text)
}

impl NorthCarolinaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(invalid(format!(
                    "northcarolinataxcalc: the facts carry {}, not northcarolinataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, extras: &EstimatedFacts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(invalid(format!(
                "northcarolinataxcalc: these are North Carolina's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(invalid(format!(
                "northcarolinataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "farmers and fishermen make one installment by January 15 or file and pay by March 1, which is not built",
            ));
        }
        if extras.farmer_last_year() == Some(true) {
            return Err(Error::with_detail(
                Error::Unsupported,
                "two-thirds of last year's gross income from farming or fishing puts this year under the farmers' rules, which are not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                if p.end() != self.y.period_ends[i] {
                    return Err(invalid(format!(
                        "northcarolinataxcalc: D-422A period {} ends on {}, not {}",
                        i + 1,
                        self.y.period_ends[i],
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and the statute part ways on a rule
    /// every North Carolina computation uses. The weekend rule and the farmer
    /// test are in research/estimated-payments/nc.md; the January 31 rule was
    /// found reading D-422 against § 105-163.15(h). Each is marked as
    /// affecting these facts only where the two readings give them different
    /// figures.
    fn divergences(
        &self,
        f: &Facts,
        a: &Assessment,
        extras: &EstimatedFacts,
    ) -> Result<Vec<Divergence>, Error> {
        let mut moved = false;
        for (i, d) in a.dues.iter().enumerate() {
            moved = moved || *d != self.y.dues[i];
        }
        let first = f.requirement_first_met();
        let late_start = !first.is_zero() && first.after(a.dues[0]) && !a.exempt;
        let farmer_answered = extras.farmer_last_year().is_some();
        let farm_open = f.farm_share().0 + f.fish_share().0 > 0 && !farmer_answered;
        Ok(vec![
            working::new_divergence("A return filed and paid by January 31")
                .form("Tax paid with a return filed by Jan 31 goes in the January 15 column. It counts as paid on Jan 15. Interest on every earlier shortfall stops then.",
                    "Form D-422 (2025), Line 17 instructions")
                .statute("No interest is charged on a short fourth installment. Earlier installments are charged until paid.",
                    "N.C. Gen. Stat. § 105-163.15(h)")
                .uses(Reading::Form)
                .affects(a.filed_by_jan31)
                .build()?,
            working::new_divergence("Due dates on a weekend or holiday")
                .form("A due date on a Saturday, Sunday or holiday moves to the next business day.",
                    "Form D-422 (2025), Part III; Form NC-40 (2026), When To Pay")
                .statute("The installments are due April 15, June 15, September 15 and January 15. There is no shift for a weekend.",
                    "N.C. Gen. Stat. § 105-163.15(c)")
                .uses(Reading::Form)
                .affects(moved)
                .build()?,
            working::new_divergence("A requirement that arises late")
                .form("NC-40 says to pay half your estimate in June if you first need to pay then. It says three-quarters if you first need to pay in September. D-422 charges a quarter in every column.",
                    "Form NC-40 (2026), When To Pay; Form D-422 (2025), line 16")
                .statute("Every installment is 25% of the required annual payment. Only the annualized method lowers one.",
                    "N.C. Gen. Stat. § 105-163.15(d), (e)")
                .uses(Reading::Statute)
                .affects(late_start)
                .build()?,
            working::new_divergence("Who counts as a farmer")
                .form("NC-40 tests two-thirds of this year's estimated gross income. D-422 tests 2024 or 2025.",
                    "Form NC-40 (2026), Farmers and Fishermen; Form D-422 (2025), Exceptions")
                .statute("The test is this year's gross income or the gross income on last year's return.",
                    "N.C. Gen. Stat. § 105-163.15(i)(3)")
                .uses(Reading::Form)
                .affects(farm_open)
                .build()?,
        ])
    }
}

/// "Jan 15" inside the tax year and "Jan 15, 2027" outside it.
fn short_date(d: Date, year: i64) -> String {
    let month = d.month().to_string();
    let m = &month[..3];
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

/// What the target leaves to pay after withholding, for a Why.
fn target_why(target: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!(
            "Your target is {}, a quarter by each due date.",
            target.whole()
        );
    }
    format!(
        "Your target is {}, which leaves {} after withholding. Pay a quarter of that by each due date.",
        target.whole(),
        (target - withheld).whole()
    )
}

/// What estimated payments the target leaves after withholding, for a Why.
fn left_why(target: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!(
            "That's {} of estimated payments, a quarter by each due date.",
            target.whole()
        );
    }
    format!(
        "After {} of withholding, you need {} of estimated payments. Pay a quarter by each due date.",
        withheld.whole(),
        (target - withheld).whole()
    )
}

/// What tax less withholding leaves, as a sentence for a Why.
fn owed_why(tax: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!(
            "Your North Carolina tax is {}, with no withholding.",
            tax.whole()
        );
    }
    format!(
        "North Carolina tax of {} less {} withheld {}.",
        tax.whole(),
        withheld.whole(),
        estimated::leaves(tax - withheld)
    )
}

/// A question asked only when no exception sets the target.
struct Ask {
    position: PositionKind,
    question: String,
    effect: String,
}

impl NorthCarolinaEstimated {
    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let extras = self.extras(f)?;
        self.check(f, &extras)?;
        let y = &self.y;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let last = y.year - 1;

        if f.farm_share().0 + f.fish_share().0 > 0 && extras.farmer_last_year().is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!("Did farming or fishing make up two-thirds of your {last} return's gross income?"),
                "Two-thirds or more counts. Until then, the figures use the four-installment rules, not the farmers' rules.",
            );
        }

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior = f.prior_year();
        let prior_tax = prior
            .and_then(|p| p.figures())
            .and_then(|pf| pf.tax())
            .unwrap_or_default();

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
                "N.C. Gen. Stat. § 105-163.15(d)(1); Form D-422 (2025), line 4",
                format!(
                    "{} of this year's North Carolina tax after credits of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        // Questions about the target, asked only when no exception sets it.
        let mut unless_exempt: Vec<Ask> = Vec::new();
        let prior_option = match prior {
            None => {
                unless_exempt.push(Ask {
                    position: PositionKind::PriorYearReturn,
                    question: format!(
                        "Did you file a {last} North Carolina return, and what was its tax?"
                    ),
                    effect: format!(
                        "With it, {} of last year's tax may set a target below {} of this year's.",
                        y.prior_year_share, y.this_year_share
                    ),
                });
                unavailable(format!("No {last} North Carolina return on record"))
            }
            Some(p) if !p.filed() => {
                unavailable(format!("You didn't file a {last} North Carolina return"))
            }
            Some(p) if p.months() != 12 => unavailable(format!(
                "Your {last} North Carolina return covered {} months, not 12",
                p.months()
            )),
            Some(_) => {
                a.notes.rule(
                    "N.C. Gen. Stat. § 105-163.15(d)(2); Form D-422 (2025), line 7",
                    format!(
                        "the prior-year test is {} of the {} tax after credits on the {last} return",
                        y.prior_year_share, prior_tax
                    ),
                );
                estimated::new_target_option(
                    Basis::PriorYearTax,
                    y.prior_year_share,
                    y.prior_year_share.of(prior_tax),
                )
            }
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            unless_exempt.push(Ask {
                position: PositionKind::ThisYearTax,
                question: format!(
                    "What do you expect your {} North Carolina tax after credits to be?",
                    y.year
                ),
                effect: estimated::without_this_year_tax(prior_opt.available()),
            });
        }

        // § 105-163.15(f): no liability last year, with no twelve-month or
        // residence condition.
        let no_liability =
            prior.is_some_and(|p| (p.filed() && prior_tax.0 == 0) || p.not_required_to_file());
        if no_liability {
            a.exempt = true;
            let none =
                estimated::new_exception("No North Carolina tax liability last year").build()?;
            rb.required(false, format!("You had no North Carolina income tax liability for {last}. No interest is charged on underpaid estimates for {}.", y.year))
                .exempt(none);
            a.notes.rule(
                "N.C. Gen. Stat. § 105-163.15(f); Form D-422 (2025), Exceptions",
                format!("no liability for {last} means no interest for {}", y.year),
            );
        } else if tax_known && tax - withheld < y.threshold {
            a.exempt = true;
            let below =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            let mut why = format!(
                "{} That's under {}, so no estimated payments are needed.",
                owed_why(tax, withheld),
                y.threshold.whole()
            );
            if withheld > tax {
                why = owed_why(tax, withheld) + " No estimated payments are needed.";
            } else if withheld == tax {
                why = format!(
                    "Your North Carolina withholding of {} covers your tax of {}. No estimated payments are needed.",
                    withheld.whole(),
                    tax.whole()
                );
            }
            rb.required(false, why).exempt(below);
            a.notes.rule(
                "N.C. Gen. Stat. § 105-163.15(f); Form D-422 (2025), line 6",
                format!(
                    "{} of tax less {} withheld is {}, under {}, so there is no interest",
                    tax,
                    withheld,
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(false, format!("You have no full-year {last} North Carolina return to measure against. Your target is {} of your {} tax.", y.this_year_share, y.year))
                .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(false, format!("Your withholding of {} already reaches 100% of your {last} tax, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
                withheld.whole(), target.whole(), y.year))
                .choose(chosen);
        } else if !tax_known {
            rb.required(true, format!("Your target is 100% of your {last} tax, {}. Meeting it keeps you clear of interest whatever your {} tax is. {}",
                target.whole(), y.year, left_why(target, withheld)))
                .choose(chosen);
        } else if withheld >= target {
            rb.required(false, format!("Your North Carolina withholding is {}. That already meets the {} needed for the year. No estimated payments are needed.", withheld.whole(), target.whole()))
                .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "{} That's {} or more, so estimated payments are needed. {}",
                    owed_why(tax, withheld),
                    y.threshold.whole(),
                    target_why(target, withheld)
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        if !a.exempt {
            for ask in unless_exempt {
                a.notes.ask(ask.position, ask.question, ask.effect);
            }
        }

        let cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "N.C. Gen. Stat. § 105-163.15(c), (d); Form D-422 (2025), line 16",
            format!(
                "25% of {} is due on each of {}, {}, {} and {}",
                a.requirement.target(),
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );

        if f.elections().annualised() && !a.exempt {
            let annual: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&annual, &y.annualised, &a.installments)?;
            a.notes.rule(
                "N.C. Gen. Stat. § 105-163.15(e); Form D-422A (2025)",
                format!(
                    "under the annualized method the installments are {}, {}, {} and {}: 22.5%, 45%, 67.5% and 90% of the annualized tax, \
                     less earlier installments, and never more than the regular installment plus any reduction carried forward",
                    ann[0], ann[1], ann[2], ann[3]
                ),
            );
            a.installments = ann;
        }

        // The D-422 line 17 rule. January 31, 2027 is a Sunday, and no source
        // says whether the day moves.
        let mut file_by = y.file_and_pay_by;
        let moves = extras.january_thirty_first_moves();
        if moves == Some(true) {
            file_by = y.calendar.next_business_day(file_by);
        }
        let filed = f.return_filed();
        if !filed.is_zero() && !a.exempt {
            if moves.is_none()
                && filed.after(file_by)
                && !filed.after(y.calendar.next_business_day(file_by))
            {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "Does North Carolina count your return filed and paid on {} as on time?",
                        short_date(filed, y.year)
                    ),
                    format!(
                        "The deadline is {}, a Sunday. Until then, it's treated as late and its tax stays out of the January column.",
                        short_date(file_by, y.year)
                    ),
                );
            }
            if !filed.after(file_by) {
                a.filed_by_jan31 = true;
                a.notes.rule(
                    "Form D-422 (2025), Line 17 instructions; N.C. Gen. Stat. § 105-163.15(h)",
                    format!(
                        "the return was filed and paid on {}, by {}, so the tax paid with it counts in the January column, as paid on {}",
                        filed.long(),
                        file_by.long(),
                        y.deemed_paid.long()
                    ),
                );
            }
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }
        let divergences = self.divergences(f, &a, &extras)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &self.y.shares)?
        };
        let ledger = estimated::apply(&a.dues, &a.installments, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "N.C. Gen. Stat. § 105-163.15(g)",
                format!(
                    "as established, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "N.C. Gen. Stat. § 105-163.15(b), (g); Form D-422 (2025), lines 17 and 25",
                format!(
                    "an equal part of the {} withheld is treated as paid on each due date, and a payment goes first to the earliest underpayment",
                    f.withheld_total()
                ),
            );
        }
        Ok((ledger, notes))
    }

    /// The schedule from the ledger. The January installment is excused when
    /// the return was filed and paid by January 31.
    fn installments(
        &self,
        a: &Assessment,
        ledger: &[LedgerEntry],
    ) -> Result<Vec<Installment>, Error> {
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
        Ok(out)
    }
}

impl EstimatedTax for NorthCarolinaEstimated {
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
                    "with no {} North Carolina return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let out = self.installments(&a, &ledger)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(invalid(
                "northcarolinataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} North Carolina return to measure against, there is no target to figure the interest from",
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
        if a.filed_by_jan31 && y.deemed_paid.before(stop) {
            stop = y.deemed_paid;
        }
        let waiver_asked = f.positions().waiver();
        let waiver = waiver_asked.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let schedule = self.installments(&a, &ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        // D-422 Section B: each part of each shortfall, by the day ÷ 365, at
        // the rate for the days, until paid or April 15.
        let (mut p, unrated) =
            estimated::simple_penalty(&ledger, stop, &y.rates, waiver, &a.requirement, &schedule)?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "N.C. Gen. Stat. § 105-163.15(d)",
                format!(
                    "this year's North Carolina tax was not given, so the interest is figured against the {} tax alone; \
                     the required payment is the lesser of that and 90% of this year's tax, so {} is the most the interest can be, and it is less if this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        notes.rule(
            "N.C. Gen. Stat. §§ 105-163.15(a), (b), 105-241.21; Form D-422 (2025), lines 25 to 27",
            format!(
                "each part of each shortfall is charged the rate by the day ÷ 365, from its due date until paid or {}; \
                 the interest to {} is {}",
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
                    short_date(unrated.add_days(-1), 0)
                ),
            );
        }
        if waiver_asked.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "N.C. Gen. Stat. § 105-237(a)(2)",
                "the Secretary may waive interest only for a period of bankruptcy, so there is no waiver for casualty, retirement or disability; the interest is shown in full",
            );
            if p.total().0 > 0 {
                let o = estimated::waiver_refused(
                    "A casualty, disaster, retirement or disability doesn't waive this interest. The Secretary of Revenue can waive it only for a period of bankruptcy.",
                );
                p = p.to_builder().waiver_outcome(o).build()?;
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        north_carolina_explanation()
    }
}

/// Written by hand and checked against research/estimated-payments/nc.md.
fn north_carolina_explanation() -> Explanation {
    estimated::new_explanation(
        "North Carolina expects estimated payments if $1,000 or more of tax would be due. That is the tax left after withholding and credits. Falling short costs interest, 7% a year for 2026, on each quarter's shortfall.")
        .paragraph("You're clear if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. A quarter is due by each of April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. Last year's figure needs a full-year return you filed. Withholding counts as paid evenly unless you show when it was taken.")
        .paragraph("There is no interest if this year's tax less withholding is under $1,000. Nor is there any if you had no North Carolina income tax liability last year. A return filed with the balance paid by January 31 counts in the January column. It counts as paid on January 15.")
        .paragraph("The interest runs by the day from each due date until the shortfall is paid. It stops on April 15 of the next year. Each payment goes first to the oldest shortfall. The Secretary of Revenue sets the rate for each half-year. North Carolina has no waiver for a casualty, retirement or disability. Annualizing can lower early payments when income arrives late in the year.")
        .versus_federal("Unlike federal, there is no 110% tier. The no-liability exception has no twelve-month or residence condition. There is no waiver for casualty, retirement or disability. The state sets the rate each half-year. A return filed by January 31 counts as paid on January 15. The $1,000 threshold is the federal one.")
        .source("N.C. Gen. Stat. §§ 105-163.15, 105-241.21, 105-237")
        .source("Form D-422, 2025 Underpayment of Estimated Tax by Individuals, and its instructions")
        .source("Form NC-40, Individual Estimated Income Tax (for tax years beginning on or after January 1, 2026)")
        .source("NCDOR, Interest Rate; Estimated Income Tax")
        .build()
        .unwrap_or_default()
}

/// A shorthand for the year files.
pub(crate) fn date(y: i64, m: Month, d: i64) -> Date {
    estimated::must_date(y, m, d)
}
