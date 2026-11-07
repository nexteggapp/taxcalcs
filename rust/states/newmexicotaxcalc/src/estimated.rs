//! New Mexico's estimated tax rules, NMSA 1978 § 7-2-12.2, as the shared
//! [`EstimatedTax`] trait. New Mexico calls the charge a penalty and figures
//! it as interest.
//!
//! New Mexico follows the federal shape: four 25% installments, the lesser
//! of 90% of this year's tax or 100% of last year's, payments applied to the
//! earliest installment first, and the federal underpayment rate quarter by
//! quarter. It differs in the $1,000 threshold, no 110% tier, a full
//! exemption for first-year residents, a no-liability exception only for
//! full-year residents, and a late start instead of an annualized method.
//!
//! The figure follows Form RPD-41272 for 2025, the latest published, with
//! the 2026 dates and rates, and the result's Divergences say where the
//! statute reads otherwise. The statute is the text Laws 2011, HB 470
//! enacted for tax years from 2012; the 2024 to 2026 Tables of Changes list
//! no amendment to it or to § 7-1-67 (research/estimated-payments/nm.md), so
//! the form does not predate the law it describes.
//!
//! The rate for the first quarter after the tax year also covers April 1 to
//! 15. The rate from January 1, 2027 is not published, so those days are not
//! charged and the penalty reports the gap.
//!
//! Not built: withholding under the Oil and Gas Proceeds and Pass-Through
//! Entity Withholding Tax Act, which a caller passes with the rest; and the
//! separate election for wage and other withholding, which here is one
//! election for all of it.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    LedgerEntry, Month, Penalty, RateSeries, Requirement, WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

/// New Mexico's postal code.
const JURISDICTION: Jurisdiction = estimated::NEW_MEXICO;

/// The facts New Mexico's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    first_year: Option<bool>,
    farmer_last: Option<bool>,
    moves: Option<bool>,
}

impl EstimatedFacts {
    /// Whether this is the filer's first year as a New Mexico resident,
    /// which § 7-2-12.2(M) takes out of estimated tax, or `None` when the
    /// caller did not say.
    pub fn first_year_resident(&self) -> Option<bool> {
        self.first_year
    }

    /// Whether at least two-thirds of last year's gross income was from
    /// farming or ranching, which puts this year under the farmers' rules,
    /// or `None` when the caller did not say.
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

/// Starts an [`EstimatedFacts`] with nothing answered.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Answers whether this is the filer's first year as a resident.
    pub fn first_year_resident(&mut self, first: bool) -> &mut Self {
        self.e.first_year = Some(first);
        self
    }

    /// Answers whether two-thirds of last year's gross income was from
    /// farming or ranching.
    pub fn farmer_last_year(&mut self, farmer: bool) -> &mut Self {
        self.e.farmer_last = Some(farmer);
        self
    }

    /// Answers whether January 31 moves to the next business day.
    pub fn january_thirty_first_moves(&mut self, moves: bool) -> &mut Self {
        self.e.moves = Some(moves);
        self
    }

    /// Returns the facts. It never fails.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// RPD-41272 line 4 and § 7-2-12.2(H)(1).
    pub(crate) threshold: USCents,

    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,
    pub(crate) shares: Vec<Rate>,
    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// The first days of the late-start groups in § 7-2-12.2(C): a
    /// requirement that first arises on or after `later_groups[i]` starts at
    /// installment i+2.
    pub(crate) later_groups: Vec<Date>,

    /// § 7-2-12.2(I).
    pub(crate) file_and_pay_by: Date,

    pub(crate) stop: Date,
    pub(crate) rates: RateSeries,
}

/// Returns New Mexico's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(NewMexicoEstimated { y }))
}

struct NewMexicoEstimated {
    y: EstimatedYear,
}

#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    installments: Vec<USCents>,
    excused: bool,
    extras: EstimatedFacts,
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

/// The facts each divergence's readings turn on.
#[derive(Clone, Copy, Debug, Default)]
struct Touches {
    paid_late: bool,
    withholding_year: bool,
    page_threshold: bool,
    annualised: bool,
}

impl NewMexicoEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "newmexicotaxcalc: the facts carry {}, not newmexicotaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, extras: &EstimatedFacts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "newmexicotaxcalc: these are New Mexico's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "newmexicotaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if i64::from(f.farm_share().0) * 3 >= 20_000 {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or rancher pays once by January 15 or files and pays by March 1, and owes a penalty only past a one-third underpayment, which is not built",
            ));
        }
        if extras.farmer_last_year() == Some(true) {
            return Err(Error::with_detail(
                Error::Unsupported,
                "two-thirds of last year's gross income from farming or ranching puts this year under the farmers' rules, which are not built",
            ));
        }
        Ok(())
    }

    /// Where the department's documents and the statute part ways on a rule
    /// every New Mexico computation uses. The web page, the line 4 wording
    /// and FYI-320 are in research/estimated-payments/nm.md; the (H)(3)
    /// exception was found reading RPD-41272 against the statute.
    fn divergences(&self, t: Touches) -> Result<Vec<Divergence>, Error> {
        let mut out = Vec::new();
        for b in [
            working::new_divergence("Paying the required annual payment")
                .form(
                    "No penalty if the required annual payment was paid in timely installments.",
                    "Form RPD-41272 (2025) instructions, How To Avoid a Penalty",
                )
                .statute(
                    "No penalty if you paid the required annual payment, on time or not. Withholding and estimated payments both count.",
                    "NMSA 1978 § 7-2-12.2(H)(3), as amended by Laws 2011, ch. 156 (HB 470)",
                )
                .uses(Reading::Form)
                .affects(t.paid_late),
            working::new_divergence("The withholding line 4 compares")
                .form(
                    "Line 4 compares this year's tax with the income tax withheld on the 2024 PIT-1. The form's own header note uses this year's withholding.",
                    "Form RPD-41272 (2025), line 4 and How To Use This Form",
                )
                .statute(
                    "The test is the tax for the year less the amount withheld for that year.",
                    "NMSA 1978 § 7-2-12.2(H)(1)",
                )
                .uses(Reading::Statute)
                .affects(t.withholding_year),
            working::new_divergence("The $1,000 test")
                .form(
                    "The department's page subtracts estimated payments and withholding from the tax. It computes a penalty when the rest is more than $1,000. Form RPD-41272 stops when tax less withholding is under $1,000.",
                    "Taxation and Revenue Department, Estimated Payments; Form RPD-41272 (2025), line 4",
                )
                .statute(
                    "No penalty if tax less withholding alone is under $1,000. At exactly $1,000 the penalty can apply.",
                    "NMSA 1978 § 7-2-12.2(H)(1)",
                )
                .uses(Reading::Form)
                .affects(t.page_threshold),
            working::new_divergence("An annualized method")
                .form(
                    "Form RPD-41272 has no annualized method. It has only a late start for a filer not required to pay until after March 31. FYI-320 (Rev. 6/2014) describes a modified annualization.",
                    "Form RPD-41272 (2025), Section 3 instruction 9; FYI-320 (Rev. 6/2014)",
                )
                .statute(
                    "The statute has only the late start. It is 50%, 75% or 100% at the first installment that applies.",
                    "NMSA 1978 § 7-2-12.2(C)",
                )
                .uses(Reading::Form)
                .affects(t.annualised),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let extras = self.extras(f)?;
        self.check(f, &extras)?;
        let y = &self.y;
        let mut a = Assessment {
            extras,
            ..Assessment::default()
        };
        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        let last = y.year - 1;

        if f.farm_share().0 > 0 && extras.farmer_last_year().is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "Was at least two-thirds of your {last} gross income from farming or ranching?"
                ),
                "If so, New Mexico's farmers' rules apply instead.",
            );
        }

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();
        let mut prior_tax = USCents(0);
        if has_prior && let Some(pf) = prior.figures() {
            prior_tax = pf.tax().unwrap_or_default();
        }

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
                "NMSA 1978 § 7-2-12.2(B)(1)(a); Form RPD-41272 (2025), line 5",
                format!(
                    "{} of this year's New Mexico tax after rebates and credits of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        let unavailable = |why: String| {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        // Questions about the target, asked only when no exception sets it.
        let mut unless_exempt: Vec<(PositionKind, String, String)> = Vec::new();
        let prior_option = if !has_prior {
            unless_exempt.push((
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} New Mexico return, and what was its tax?"),
                format!(
                    "With it, 100% of your {last} tax could be a lower target than 90% of this year's."
                ),
            ));
            unavailable(format!("Needs your {last} New Mexico return"))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} New Mexico return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} New Mexico return covered {} months, not a full year",
                prior.months()
            ))
        } else {
            let amount = y.prior_year_share.of(prior_tax.max(USCents(0)));
            a.notes.rule(
                "NMSA 1978 § 7-2-12.2(B)(1)(b); Form RPD-41272 (2025), line 6",
                format!(
                    "the prior-year test is {} of the {} tax after rebates and credits on the {last} return",
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
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            let question = format!("What do you expect your {} New Mexico tax to be?", y.year);
            if prior_opt.available() {
                unless_exempt.push((
                    PositionKind::ThisYearTax,
                    question,
                    format!(
                        "90% of it could be a lower target than your {last} tax. It also decides if the {} threshold excuses you.",
                        y.threshold.whole()
                    ),
                ));
            } else {
                unless_exempt.push((
                    PositionKind::ThisYearTax,
                    question,
                    format!(
                        "With no full-year {last} New Mexico return, there's no target without it."
                    ),
                ));
            }
        }

        let first = extras.first_year_resident().unwrap_or(false);
        let no_liability = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax.0 == 0) || prior.not_required_to_file());
        let residence = prior.full_year_resident();
        let residence_answered = residence.is_some();
        let resident = residence.unwrap_or(false);
        if no_liability && !residence_answered {
            a.notes.ask(
                PositionKind::Residency,
                format!("Did you live in New Mexico all of {last}?"),
                format!(
                    "If you did, no New Mexico tax liability for {last} means no penalty for {}.",
                    y.year
                ),
            );
        }
        let exempt_by_other = first || (no_liability && residence_answered && resident);
        if first {
            let exception =
                estimated::new_exception("First year as a New Mexico resident").build()?;
            a.exempt = true;
            rb.required(
                false,
                format!(
                    "This is your first year as a New Mexico resident. New Mexico requires no estimated payments of first-year residents. None are required for {}.",
                    y.year
                ),
            )
            .exempt(exception);
            a.notes.rule(
                "NMSA 1978 § 7-2-12.2(M); Form RPD-41272 (2025), Section 2 note",
                "the section does not apply to first-year residents",
            );
        } else if no_liability && residence_answered && resident {
            let exception =
                estimated::new_exception("No New Mexico tax due last year as a full-year resident")
                    .build()?;
            a.exempt = true;
            rb.required(
                false,
                format!(
                    "You had no New Mexico tax liability for all 12 months of {last} as a resident. No estimated payments are required for {}.",
                    y.year
                ),
            )
            .exempt(exception);
            a.notes.rule(
                "NMSA 1978 § 7-2-12.2(H)(2)",
                format!(
                    "the {last} return showed no liability for a full 12 months of residence, so there is no penalty for {}",
                    y.year
                ),
            );
        } else if tax_known && tax - withheld < y.threshold {
            let exception =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            a.exempt = true;
            let mut why = format!(
                "That's under {}, so no estimated payments are needed.",
                y.threshold.whole()
            );
            if tax < withheld {
                why = "No estimated payments are needed.".to_string();
            }
            rb.required(
                false,
                format!(
                    "Your {} New Mexico tax less {} withheld {}. {}",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld),
                    why
                ),
            )
            .exempt(exception);
            a.notes.rule(
                "NMSA 1978 § 7-2-12.2(H)(1); Form RPD-41272 (2025), line 4",
                format!(
                    "{} of tax less {} withheld is {}, under {}, so there is no penalty",
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
                    "There's no target yet. It needs your {} New Mexico tax or a full-year {last} New Mexico return.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} already reaches your {last} tax, {}. No estimated payments are needed, whatever your {} tax is.",
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
                    "Your target is your {last} tax, {}. It keeps you clear of a penalty whatever your {} tax turns out to be. After {} of withholding, that's {} of estimated payments. Pay a quarter by each due date.",
                    target.whole(),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your New Mexico withholding of {} covers the {} needed for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your {} New Mexico tax less {} withheld leaves {}. That's at least {}, so you need {} of estimated payments. Pay a quarter by each due date.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.threshold.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        if !a.exempt {
            for (position, question, effect) in unless_exempt {
                a.notes.ask(position, question, effect);
            }
        }

        let mut cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        // § 7-2-12.2(C): a filer first required to pay after March 31 catches
        // up at the first installment that applies.
        let mut start = 0;
        let met = f.requirement_first_met();
        if !met.is_zero() {
            for (i, g) in y.later_groups.iter().enumerate() {
                if !met.before(*g) {
                    start = i + 1;
                }
            }
        }
        for c in cumulative.iter_mut().take(start) {
            *c = USCents(0);
        }
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "NMSA 1978 § 7-2-12.2(C), (D); Form RPD-41272 (2025), Section 3 instructions 8 and 9",
            format!(
                "of {}, {}, {}, {} and {} are due on {}, {}, {} and {}",
                a.requirement.target(),
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
        if f.elections().annualised() {
            a.notes.rule(
                "NMSA 1978 § 7-2-12.2(C); Form RPD-41272 (2025)",
                "New Mexico has no annualized method; a filer whose income came late gives the date the requirement arose, and the late-start schedule applies",
            );
        }

        // § 7-2-12.2(I). January 31, 2027 is a Sunday, and no source says
        // whether the day moves.
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
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "{}, the date to file and pay by, is a {}. No source says if {} counts, and the figures treat it as late.",
                        file_by.long(),
                        file_by.weekday(),
                        filed.long()
                    ),
                );
            }
            if !filed.after(file_by) {
                a.excused = true;
                a.notes.rule(
                    "NMSA 1978 § 7-2-12.2(I); Form RPD-41272 (2025) instructions, How To Avoid a Penalty",
                    format!(
                        "the return was filed and paid on {}, by {}, so the January installment bears no penalty",
                        filed.long(),
                        file_by.long()
                    ),
                );
            }
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        let paid: USCents = f.payments().iter().map(|p| p.amount()).sum();
        let mut t = Touches {
            annualised: f.elections().annualised() && !a.exempt,
            ..Touches::default()
        };
        if tax_known && !exempt_by_other {
            t.page_threshold =
                tax - withheld >= y.threshold && tax - withheld - paid <= y.threshold;
            t.withholding_year = tax - withheld < y.threshold;
            if has_prior
                && let Some(pf) = prior.figures()
                && let Some(prior_withheld) = pf.withheld()
            {
                t.withholding_year =
                    (tax - prior_withheld < y.threshold) != (tax - withheld < y.threshold);
            }
        }
        if !a.exempt && a.requirement.target_known() && withheld + paid >= a.requirement.target() {
            let (ledger, _) = self.ledger(f, &a)?;
            if ledger.iter().any(|l| l.shortfall().0 > 0) {
                t.paid_late = true;
            }
        }
        let divergences = self.divergences(t)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &self.y.shares)?
        };
        let mut required = a.installments.clone();
        if a.excused {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "NMSA 1978 § 7-2-12.2(F); Form RPD-41272 (2025), exception 1",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "NMSA 1978 § 7-2-12.2(F), (G)(3); Form RPD-41272 (2025), Section 3 instruction 11",
                format!(
                    "an equal part of the {} withheld is treated as paid on each due date, and a payment goes first to the earliest installment",
                    f.withheld_total()
                ),
            );
        }
        Ok((ledger, notes))
    }

    /// The installments the ledger gives.
    fn schedule_of(
        &self,
        a: &Assessment,
        ledger: &[LedgerEntry],
    ) -> Result<Vec<Installment>, Error> {
        let mut out = Vec::with_capacity(ledger.len());
        let mut needed = USCents(0);
        for (i, l) in ledger.iter().enumerate() {
            needed += a.installments[i];
            let mut b = estimated::new_installment(i as i64 + 1, l.due());
            b.needed_by(needed)
                .credited_by(l.credited_by())
                .shortfall(l.shortfall());
            if a.excused && i == 3 {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok(out)
    }
}

impl EstimatedTax for NewMexicoEstimated {
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
                    "with no {} New Mexico return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let out = self.schedule_of(&a, &ledger)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "newmexicotaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} New Mexico return to measure against, there is no target to figure a penalty from",
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
        let waiver_opt = f.positions().waiver();
        let waiver = waiver_opt.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let schedule = self.schedule_of(&a, &ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        // RPD-41272 Sections 5 and 6: each part of each shortfall, by the day
        // ÷ 365, at each quarter's rate, until paid or April 15.
        let (mut p, unrated) =
            estimated::simple_penalty(&ledger, stop, &y.rates, waiver, &a.requirement, &schedule)?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "NMSA 1978 § 7-2-12.2(B)(1)",
                format!(
                    "this year's New Mexico tax was not given, so the penalty is figured against the {} tax alone; \
                     the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        notes.rule(
            "NMSA 1978 §§ 7-2-12.2(G), 7-1-67(B); Form RPD-41272 (2025), lines 21 to 34",
            format!(
                "each part of each shortfall is charged the quarter's rate by the day ÷ 365, from its due date until paid or {}; \
                 the penalty to {} is {}",
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
        if p.total().0 > 0 {
            if waiver_opt.is_none() {
                notes.ask(
                    PositionKind::PenaltyWaiver,
                    "Are you asking for a waiver for lack of fraud, negligence or disregard of rules?",
                    "The figures show the full penalty until the department finds that and waives it.",
                );
            }
            if a.extras.first_year_resident().is_none() {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    format!("Is {} your first year as a New Mexico resident?", y.year),
                    "If it is, you need no estimated payments and no penalty is due.",
                );
            }
            if f.requirement_first_met().is_zero() {
                notes.ask(
                    PositionKind::RequirementFirstMet,
                    format!(
                        "When in {} did your income first make estimated payments necessary?",
                        y.year
                    ),
                    "If after March 31, you catch up at a later due date. Until then the figures assume you needed them from January 1.",
                );
            }
        }
        if let Some(o) = new_mexico_waiver(f)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                "Waivers need the department to find no fraud, negligence or disregard of rules. The law doesn't say if casualty, disaster, retirement or disability shows that.",
            );
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        NEW_MEXICO_EXPLANATION.clone()
    }
}

/// Decides a waiver asked for. New Mexico has no casualty, retirement or
/// disability waiver; NMSA 1978 § 7-2-12.2(H)(4) waives the penalty when the
/// secretary finds the underpayment wasn't due to fraud, negligence or
/// disregard of the rules, and names no causes, so every request is
/// undecided.
fn new_mexico_waiver(f: &Facts) -> Option<WaiverOutcome> {
    match f.positions().waiver() {
        None | Some(WaiverKind::None) => None,
        Some(WaiverKind::Casualty) => Some(estimated::waiver_undecided(
            "New Mexico has no waiver for a casualty or disaster as such. The department waives it if it finds no fraud, negligence or disregard of rules.",
        )),
        Some(_) => Some(estimated::waiver_undecided(
            "New Mexico has no waiver for retirement or disability. The department waives it if it finds no fraud, negligence or disregard of rules.",
        )),
    }
}

/// Written by hand and checked against research/estimated-payments/nm.md.
static NEW_MEXICO_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "New Mexico expects estimated payments from anyone who files a return. It charges no penalty unless your tax less withholding is $1,000 or more. The penalty is interest on each quarter's shortfall. It is charged at the federal underpayment rate.",
    )
    .paragraph("You are clear if withholding and estimated payments reach your target on time. It is the smaller of 90% of this year's New Mexico tax or 100% of last year's. A quarter is due by each of April 15, June 15, September 15 and January 15. There is no 110% figure for higher incomes. Last year's figure needs a full-year New Mexico return. Withholding counts as paid evenly unless you show when it was taken.")
    .paragraph("You need no estimated payments if last year's full-year return had no tax due. You must also have lived in New Mexico all of that year. Late income can mean you first have to pay after March 31. If so, you catch up at the next date instead. Pay half by June 15, three-quarters by September 15, or all of it by January 15. Filing and paying in full by January 31 clears the January payment's penalty.")
    .paragraph("The penalty runs daily from each due date until paid or the next April 15. Each payment goes first to the oldest shortfall. The rate follows the federal one quarter by quarter. It is 6% from April to June 2026 and 7% from July. The department can waive the penalty. That needs an underpayment not due to fraud or negligence.")
    .versus_federal("New Mexico has no 110% tier. The no-liability exception needs a full year of New Mexico residence. There is no annualized method. A late start catches up at the first date that applies. The first quarter's rate after the year also covers April 1 to 15.")
    .source("NMSA 1978 § 7-2-12.2, as amended by Laws 2011, HB 470")
    .source("Form RPD-41272, 2025 Calculation of Estimated Personal Income Tax Underpayment Penalty, and its instructions")
    .source("Instructions for PIT-ES, New Mexico Estimated Tax Payment Voucher")
    .source("Taxation and Revenue Department, Estimated Payments and Penalty Interest Rates")
    .build()
    .unwrap_or_default()
});

/// A shorthand for the year files.
pub(crate) fn date(y: i64, m: Month, d: i64) -> Date {
    estimated::must_date(y, m, d)
}
