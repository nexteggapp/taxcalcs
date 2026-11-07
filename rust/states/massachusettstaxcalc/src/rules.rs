//! Massachusetts's estimated tax rules, M.G.L. c. 62B §§ 13 and 14, as the
//! shared [`estimated::EstimatedTax`] interface.
//!
//! The shape is federal: the lesser of a share of this year's tax and all of
//! last year's, four equal installments, simple interest on each shortfall.
//! The numbers are not: 80% rather than 90%, 100% of last year's tax at every
//! income, a $400 threshold, annualised percentages of 20%, 40%, 60% and 80%,
//! and a rate set each quarter.
//!
//! The figure follows the department's forms, the 2026 Form 1-ES and Form
//! M-2210, and the result's divergences say where c. 62B reads otherwise.

use std::any::Any;
use std::fmt;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    LedgerEntry, Penalty, RateSeries, Requirement, TargetOptionBuilder, WaiverKind,
};
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

/// Massachusetts's postal code.
const JURISDICTION: Jurisdiction = estimated::MASSACHUSETTS;

/// The reading used for a filer whose requirement first arises after March
/// 31. No Massachusetts source settles it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum LateStart {
    /// No answer. The rules then use [`LateStart::RemainingDates`] and say
    /// so in Required.
    #[default]
    Unanswered,

    /// The department's reading: the required annual payment is divided
    /// equally over the dates that remain, from June 15, September 15 or
    /// January 15, and nothing is required before. Form 1-ES (2026), AP 241
    /// § 241.3, Form M-2210 line 8.
    RemainingDates,

    /// The statute's reading: four required installments of 25% whenever
    /// the requirement arose, so the earlier ones are short until caught up.
    /// M.G.L. c. 62B § 14(c).
    FourQuarters,
}

/// Names the reading.
impl fmt::Display for LateStart {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            LateStart::RemainingDates => "equal parts over the remaining dates",
            LateStart::FourQuarters => "four installments of 25%",
            LateStart::Unanswered => "unanswered",
        })
    }
}

/// The facts and readings Massachusetts's rules read that no other
/// jurisdiction does. Carry them in [`estimated::Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    late_start: LateStart,
    prior_farmer: Option<bool>,
    part_year_prior: Option<bool>,
}

impl EstimatedFacts {
    /// The reading to use when [`estimated::Facts::requirement_first_met`]
    /// is after March 31.
    pub fn late_start(&self) -> LateStart {
        self.late_start
    }

    /// Whether last year's return showed at least two-thirds of gross income
    /// from farming or fishing, which makes the filer a farmer or fisherman
    /// for this year under c. 62B § 14(h), or `None` when the caller did not
    /// say. [`estimated::Facts::farm_share`] covers this year only.
    pub fn prior_year_farmer_or_fisher(&self) -> Option<bool> {
        self.prior_farmer
    }

    /// The caller's reading of whether last year's part-year or nonresident
    /// return opens the 100% prior-year test, or `None` when the caller gave
    /// none. No Massachusetts source says.
    pub fn part_year_prior_return_counts(&self) -> Option<bool> {
        self.part_year_prior
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
    /// Sets the late-start reading.
    pub fn late_start(&mut self, l: LateStart) -> &mut Self {
        self.e.late_start = l;
        self
    }

    /// Says whether last year's return makes the filer a farmer or
    /// fisherman.
    pub fn prior_year_farmer_or_fisher(&mut self, farmer: bool) -> &mut Self {
        self.e.prior_farmer = Some(farmer);
        self
    }

    /// Says whether last year's part-year or nonresident return opens the
    /// prior-year test.
    pub fn part_year_prior_return_counts(&mut self, counts: bool) -> &mut Self {
        self.e.part_year_prior = Some(counts);
        self
    }

    /// Returns the facts. Go's Build refuses a late-start reading it does
    /// not know; a Rust [`LateStart`] is always one it knows.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// The tax after credits and withholding at or under which no penalty
    /// applies.
    pub(crate) floor: USCents,

    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,

    pub(crate) dues: Vec<Date>,

    /// The first days of the 1-ES's late-start groups: a requirement first
    /// met on or after `later_groups[i]` starts at installment i+2.
    pub(crate) later_groups: Vec<Date>,

    pub(crate) calendar: Calendar,

    /// The fifteenth day of the fourth month after the year, and the last
    /// day the M-2210 rate lines count, the day before.
    pub(crate) stop: Date,
    pub(crate) last_charged: Date,

    pub(crate) period_ends: Vec<Date>,
    pub(crate) applicable: Vec<Rate>,
    pub(crate) rates: RateSeries,
}

impl EstimatedYear {
    /// `d` for a sentence: "Aug 31" in the tax year, "Jan 15, 2027" outside
    /// it.
    fn date(&self, d: Date) -> String {
        let month = d.month().to_string();
        let m: String = month.chars().take(3).collect();
        if d.year() == self.year {
            return format!("{m} {}", d.day());
        }
        format!("{m} {}, {}", d.day(), d.year())
    }
}

/// c. 62B § 14(c): each required installment is 25% of the required annual
/// payment. § 14(f) deems withholding paid in the same equal parts.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// The year this build carries, or `None`.
fn estimated_year(year: i64) -> Option<Result<EstimatedYear, Error>> {
    match year {
        2026 => Some(crate::estimated_y2026::year_2026()),
        _ => None,
    }
}

/// Returns Massachusetts's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    match estimated_year(year) {
        Some(y) => Ok(Box::new(MassachusettsEstimated { y: y? })),
        None => Err(estimated::no_rules(&JURISDICTION, year)),
    }
}

struct MassachusettsEstimated {
    y: EstimatedYear,
}

#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// M-2210 line 8, the required installments.
    installments: Vec<USCents>,

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

fn invalid(text: String) -> Error {
    Error::Invalid(text)
}

/// A question asked only when no exception sets the target.
struct Ask {
    position: PositionKind,
    question: String,
    effect: String,
}

impl MassachusettsEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(invalid(format!(
                    "massachusettstaxcalc: the facts carry {}, not massachusettstaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, e: &EstimatedFacts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(invalid(format!(
                "massachusettstaxcalc: these are Massachusetts's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(invalid(format!(
                "massachusettstaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or fisherman owes 66 2/3% and no penalty after filing and paying by March 1 (c. 62B § 14(c), (h)), which is not built",
            ));
        }
        if e.prior_year_farmer_or_fisher() == Some(true) {
            return Err(Error::with_detail(
                Error::Unsupported,
                "last year's return makes the filer a farmer or fisherman under c. 62B § 14(h), whose rules are not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                if p.end() != self.y.period_ends[i] {
                    return Err(invalid(format!(
                        "massachusettstaxcalc: Form M-2210 Part 3 period {} ends on {}, not {}",
                        i + 1,
                        self.y.period_ends[i],
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and c. 62B part ways on a rule every
    /// Massachusetts computation uses: conflicts C1, C2, C3 and C5 of the
    /// research file. Each says whether it changes anything for `f`.
    fn divergences(
        &self,
        f: &Facts,
        extras: &EstimatedFacts,
        a: &Assessment,
        at_floor: bool,
    ) -> Result<Vec<Divergence>, Error> {
        let y = &self.y;
        let measured = !a.exempt && a.requirement.target_known();
        let late_start = measured
            && !f.elections().annualised()
            && estimated::late_start(f.requirement_first_met(), &y.later_groups) > 0;
        let late_reading = if extras.late_start() == LateStart::FourQuarters {
            Reading::Statute
        } else {
            Reading::Form
        };
        // A shortfall changes the charge between simple and compound
        // interest, and one still unpaid on the stop date changes the last
        // day charged.
        let (mut short, mut unpaid_at_stop) = (false, false);
        if measured {
            let (ledger, _) = self.ledger(f, a)?;
            let filed = f.return_filed();
            let filed_early = !filed.is_zero() && filed.before(y.stop);
            for l in &ledger {
                if l.shortfall().0 <= 0 {
                    continue;
                }
                short = true;
                for piece in l.pieces() {
                    let paid_on = piece.paid_on();
                    if !filed_early && paid_on.is_none_or(|on| !on.before(y.stop)) {
                        unpaid_at_stop = true;
                    }
                }
            }
        }
        Ok(vec![
            working::new_divergence("The $400 floor")
                .form("No penalty if tax after credits and withholding is $400 or less. Exactly $400 bears none.",
                    "Form M-2210 (2025; draft 2026), Exceptions which avoid the penalty; AP 241 § 241.4, Exception 1")
                .statute("No penalty only if tax less credits and withholding is under $400. Exactly $400 is charged.",
                    "M.G.L. c. 62B § 14(d)(i)")
                .uses(Reading::Form)
                .affects(at_floor)
                .build()?,
            working::new_divergence("Daily compounding")
                .form("The charge is simple interest. It's the underpayment, times the days over 365, times the quarter's rate.",
                    "Form M-2210 (2025; draft 2026), Part 2, lines 20 to 23")
                .statute("Unpaid tax bears the federal short-term rate plus four points, compounded daily. The estimated tax section applies it without compounding, matching the form.",
                    "M.G.L. c. 62C § 32(a); TIR 26-5; M.G.L. c. 62B § 14(a)")
                .uses(Reading::Form)
                .affects(short)
                .build()?,
            working::new_divergence("A requirement first met after March 31")
                .form("You pay the year's amount in equal parts on the remaining dates. A change from Apr 1 to May 31 starts at Jun 15, and Jun 1 to Aug 31 at Sep 15. After that, it's all on Jan 15, 2027.",
                    "Form 1-ES (2026), What if my tax liability changes during the year?; AP 241 § 241.3; Form M-2210 (2025; draft 2026), line 8")
                .statute("There are four installments, each 25% of the required annual payment. That holds whenever the requirement arose. Only the annualised method lowers the earlier ones.",
                    "M.G.L. c. 62B § 14(c)")
                .uses(late_reading)
                .affects(late_start)
                .build()?,
            working::new_divergence("The last day charged")
                .form("Line 15 counts the days to Apr 15, 2027. The rate lines charge only days before 4/15/27, so Apr 15 bears no interest.",
                    "Form M-2210 (draft 2026), Part 2, lines 14, 15 and 19")
                .statute("The underpayment runs to the 15th day of the fourth month after the year. A part paid earlier stops on the day it's paid.",
                    "M.G.L. c. 62B § 14(b)")
                .uses(Reading::Form)
                .affects(unpaid_at_stop)
                .build()?,
        ])
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let extras = self.extras(f)?;
        self.check(f, &extras)?;
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
        let withheld = f.withheld_total();
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let prior_tax = prior
            .and_then(|p| p.figures())
            .and_then(|pf| pf.tax())
            .unwrap_or_default();
        let residence = prior.and_then(|p| p.full_year_resident());
        let (resident, residence_answered) = (residence == Some(true), residence.is_some());

        // 80% of this year's tax, M-2210 line 4.
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
                "M.G.L. c. 62B § 14(c)(i); Form M-2210 (2025; draft 2026), line 4",
                format!(
                    "{} of this year's Massachusetts tax after credits of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // 100% of last year's tax, M-2210 line 5, for a filed twelve-month
        // return.
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        // Questions whose answer can change the target, asked only when no
        // exception sets it.
        let mut target_asks: Vec<Ask> = Vec::new();
        let part_year = extras.part_year_prior_return_counts();
        let (part_year_counts, part_year_answered) = (part_year == Some(true), part_year.is_some());
        let prior_option = match prior {
            None => {
                target_asks.push(Ask {
                    position: PositionKind::PriorYearReturn,
                    question: format!(
                        "Did you file a {last} Massachusetts return, and what was its tax?"
                    ),
                    effect: format!(
                        "With it, 100% of last year's tax could be a target below {} of this year's.",
                        y.this_year_share
                    ),
                });
                unavailable(format!("Needs your {last} Massachusetts return"))
            }
            Some(p) if !p.filed() => {
                unavailable(format!("You didn't file a {last} Massachusetts return"))
            }
            Some(p) if p.months() != 12 => unavailable(format!(
                "Your {last} Massachusetts return covered {} months, not twelve",
                p.months()
            )),
            Some(_)
                if residence_answered && !resident && part_year_answered && !part_year_counts =>
            {
                unavailable(format!(
                    "Your {last} Massachusetts part-year or nonresident return doesn't count"
                ))
            }
            Some(_) => {
                let amount = y.prior_year_share.of(prior_tax);
                a.notes.rule(
                    "M.G.L. c. 62B § 14(c)(ii); Form M-2210 (2025; draft 2026), line 5",
                    format!(
                        "{} of the {} tax on the {last} return is {}",
                        y.prior_year_share, prior_tax, amount
                    ),
                );
                if residence_answered && !resident && !part_year_answered {
                    target_asks.push(Ask {
                        position: PositionKind::JurisdictionFact,
                        question: format!(
                            "Should your {last} part-year or nonresident Massachusetts return set the target?"
                        ),
                        effect: "No Massachusetts source says whether it counts for the last-year target. The figures count it.".into(),
                    });
                }
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, amount)
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
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!(
                    "What do you expect your {} Massachusetts tax after credits to be?",
                    y.year
                ),
                format!(
                    "It sets the {} option and the {} test.",
                    y.this_year_share,
                    y.floor.whole()
                ),
            );
        }

        // No liability last year: c. 62B § 14(d)(ii) needs a twelve-month
        // year and residence throughout it.
        let no_tax_last_year = prior.is_some_and(|p| {
            p.months() == 12 && ((p.filed() && prior_tax.0 == 0) || p.not_required_to_file())
        }) && has_prior;
        if no_tax_last_year && !residence_answered {
            a.notes.ask(
                PositionKind::Residency,
                format!("Did you live in Massachusetts all of {last}?"),
                format!(
                    "If you did, having no Massachusetts tax for {last} means no penalty for {}.",
                    y.year
                ),
            );
        }

        let mut at_floor = false;
        if no_tax_last_year && resident {
            a.exempt = true;
            let none =
                estimated::new_exception("No tax due last year as a full-year resident").build()?;
            rb.required(false, format!("You lived in Massachusetts all of {last} and had no Massachusetts tax for it. No estimated payments are needed for {}.", y.year))
                .exempt(none);
            a.notes.rule(
                "M.G.L. c. 62B § 14(d)(ii); Form M-2210 (2025; draft 2026), Exceptions which avoid the penalty",
                format!(
                    "no liability for {last}, a twelve-month year of residence, so there is no penalty for {}",
                    y.year
                ),
            );
        } else if tax_known && tax - withheld <= y.floor {
            a.exempt = true;
            at_floor = tax - withheld == y.floor;
            let left = (tax - withheld).max(USCents(0));
            let small = estimated::exception_at_most("Tax less withholding", left, y.floor)?;
            let under = if tax < withheld {
                String::new()
            } else {
                format!(", which is {} or less", y.floor.whole())
            };
            rb.required(
                false,
                format!(
                    "Your Massachusetts tax after credits is {}, and {} was withheld. That {}{}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld),
                    under
                ),
            )
            .exempt(small);
            a.notes.rule(
                "Form M-2210 (2025; draft 2026), Exceptions which avoid the penalty; M.G.L. c. 62B §§ 13, 14(d)(i)",
                format!(
                    "{} of tax less {} withheld is {}, not more than {}, so there is no penalty",
                    tax,
                    withheld,
                    (tax - withheld).max(USCents(0)),
                    y.floor
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "A target needs your {} Massachusetts tax or a full-year {last} return. There's no target yet.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} reaches 100% of your {last} tax, {}. No estimated payments are needed, whatever your {} tax is.",
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
                    "Your target is 100% of your {last} tax, {}. That holds whatever your {} tax turns out to be. After {} of withholding, that's {} of estimated payments. Pay a quarter by each date.",
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
                    "Your Massachusetts withholding of {} already reaches the {} target. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Massachusetts tax after credits is {}, and {} was withheld. That leaves {}, more than {}, and the target is {}. You need {} of estimated payments, a quarter by each date.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.floor.whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        if !a.exempt {
            for ask in target_asks {
                a.notes.ask(ask.position, ask.question, ask.effect);
            }
        }
        a.requirement = rb.build()?;
        if !a.exempt
            && a.requirement.target_known()
            && extras.prior_year_farmer_or_fisher().is_none()
        {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "Did at least two-thirds of your {last} gross income come from farming or fishing?"
                ),
                format!(
                    "If so, Massachusetts's farmer and fisherman rules apply to you for {} instead.",
                    y.year
                ),
            );
        }

        if a.exempt || !a.requirement.target_known() {
            a.installments = vec![USCents(0); a.dues.len()];
        } else {
            a.installments = self.installments(f, &extras, &mut a, target)?;
        }
        let divergences = self.divergences(f, &extras, &a, at_floor)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// M-2210 line 8: Part 3 line 31 for the annualised method, and
    /// otherwise line 6 divided by the number of installments required.
    fn installments(
        &self,
        f: &Facts,
        extras: &EstimatedFacts,
        a: &mut Assessment,
        target: USCents,
    ) -> Result<Vec<USCents>, Error> {
        let y = &self.y;
        let cum = estimated::cumulative_shares(target, &quarters())?;
        let regular = estimated::increments(&cum);

        if f.elections().annualised() {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &y.applicable, &regular)?;
            a.notes.rule(
                "M.G.L. c. 62B § 14(c); Form M-2210 (2025; draft 2026), Part 3, lines 23 to 31",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: 20%, 40%, 60% and 80% of the annualised tax after credits, \
                     less earlier installments, and never more than a quarter of {} plus any reduction carried forward",
                    ann[0], ann[1], ann[2], ann[3], target
                ),
            );
            return Ok(ann);
        }

        let first_met = f.requirement_first_met();
        let mut reading = extras.late_start();
        let first = estimated::late_start(first_met, &y.later_groups);
        if first_met.is_zero() && a.requirement.required() && reading != LateStart::FourQuarters {
            a.notes.ask(
                PositionKind::RequirementFirstMet,
                format!(
                    "When did you first expect to need estimated payments for {}?",
                    y.year
                ),
                format!(
                    "If it was after {}, you pay fewer, larger installments. They start from a later date.",
                    y.date(y.later_groups[0].add_days(-1))
                ),
            );
        }
        if first == 0 {
            a.notes.rule(
                "M.G.L. c. 62B § 14(c); Form 1-ES (2026); Form M-2210 (2025; draft 2026), line 8",
                format!(
                    "a quarter of {}, {}, is due on each of {}, {}, {} and {}",
                    target,
                    regular[0],
                    a.dues[0].long(),
                    a.dues[1].long(),
                    a.dues[2].long(),
                    a.dues[3].long()
                ),
            );
            return Ok(regular);
        }
        let first_due = a.dues[first as usize];
        if reading == LateStart::Unanswered {
            reading = LateStart::RemainingDates;
            a.notes.ask(
                PositionKind::JurisdictionFact,
                "Do you want your installments figured as four of 25% each, as the statute reads?",
                format!(
                    "Until then, the year's amount is spread over the dates from {}. That's what the department says.",
                    y.date(first_due)
                ),
            );
        }
        if reading == LateStart::FourQuarters {
            a.notes.rule(
                "M.G.L. c. 62B § 14(c)",
                format!(
                    "by the reading given, the requirement arising on {} changes nothing: a quarter of {}, {}, is still required on each of the four dates",
                    first_met.long(),
                    target,
                    regular[0]
                ),
            );
            return Ok(regular);
        }
        let n = a.dues.len() as i64;
        let late = estimated::equal_shares(target, n, first)?;
        let out = estimated::increments(&late);
        a.notes.rule(
            "Form 1-ES (2026), What if my tax liability changes during the year?; AP 241 § 241.3; Form M-2210 (2025; draft 2026), line 8",
            format!(
                "the requirement first arose on {}, so nothing is required before {} and {} is divided over the {} remaining dates: {}, {}, {} and {}",
                first_met.long(),
                first_due.long(),
                target,
                n - first,
                out[0],
                out[1],
                out[2],
                out[3]
            ),
        );
        Ok(out)
    }

    /// Applies the credits to the required installments.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &quarters())?
        };
        let ledger = estimated::apply(&a.dues, &a.installments, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "M.G.L. c. 62B § 14(f); Form M-2210 (2025; draft 2026), line 9 instructions",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "M.G.L. c. 62B § 14(f); Form M-2210 (2025; draft 2026), line 9 instructions",
                format!(
                    "the {} withheld is treated as paid in equal parts on the four due dates, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        notes.rule(
            "M.G.L. c. 62B § 14(b); Form M-2210 (2025; draft 2026), line 12 instructions",
            "a payment is credited to the oldest unpaid installment first, and an overpayment shortens the period of an earlier underpayment rather than reducing it",
        );
        Ok((ledger, notes))
    }

    /// The installments after the credits, with the ledger they come from.
    fn schedule_of(
        &self,
        f: &Facts,
        a: &Assessment,
    ) -> Result<(Vec<Installment>, Vec<LedgerEntry>, Notes), Error> {
        let (ledger, notes) = self.ledger(f, a)?;
        let mut out = Vec::with_capacity(ledger.len());
        for (i, l) in ledger.iter().enumerate() {
            out.push(
                estimated::new_installment(i as i64 + 1, l.due())
                    .needed_by(l.needed_by())
                    .credited_by(l.credited_by())
                    .shortfall(l.shortfall())
                    .build()?,
            );
        }
        Ok((out, ledger, notes))
    }
}

impl EstimatedTax for MassachusettsEstimated {
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
                    "with no {} Massachusetts return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (out, _, notes) = self.schedule_of(f, &a)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(invalid(
                "massachusettstaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Massachusetts return to measure against, there is no target to figure a penalty from",
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
        let waiver_answered = f.positions().waiver();
        let waiver = waiver_answered.unwrap_or_default();
        let (sched, ledger, mut notes) = self.schedule_of(f, &a)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &sched);
        let mut unrated = Date::default();
        for l in &ledger {
            if l.shortfall().0 <= 0 {
                continue;
            }
            let mut lb = estimated::new_penalty_line(l.number(), l.due(), l.shortfall());
            let mut last_day = l.due();
            for piece in l.pieces() {
                let mut to = stop;
                if let Some(on) = piece.paid_on()
                    && on.before(stop)
                {
                    to = on;
                }
                if !to.after(l.due()) {
                    continue;
                }
                if to.after(last_day) {
                    last_day = to;
                }
                // Lines 16 to 19 count only days before April 15; line 15
                // counts to it.
                let mut charged = to;
                if y.last_charged.before(charged) {
                    charged = y.last_charged;
                }
                if !charged.after(l.due()) {
                    continue;
                }
                let (parts, gap) =
                    estimated::simple_daily(piece.amount(), l.due(), charged, &y.rates)?;
                if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                    unrated = gap;
                }
                for p in parts {
                    lb.part(p);
                }
            }
            pb.line(lb.days(last_day.days_since(l.due())).build()?);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            notes.rule(
                "M.G.L. c. 62B § 14(c)",
                format!(
                    "this year's Massachusetts tax was not given, so the penalty is figured against 100% of the {} tax alone; \
                     the required annual payment is the lesser of that and 80% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "M.G.L. c. 62B § 14(b); Form M-2210 (2025; draft 2026), line 14",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "M.G.L. c. 62B § 14(a), (b); Form M-2210 (2025; draft 2026), Part 2",
            format!(
                "each shortfall is charged simple interest by the day at the quarter's rate, from its due date until paid or {}, \
                 with each payment applied to the oldest shortfall first; the penalty to {} is {}",
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
                    y.date(unrated.add_days(-1))
                ),
            );
        }
        if p.total().0 > 0 && waiver_answered.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking for the penalty to be waived?",
                "The commissioner can waive it for a casualty, disaster or other unusual event. Retiring after 62 or disability this year or last counts with reasonable cause.",
            );
        } else if waiver_answered.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "M.G.L. c. 62B § 14(e); Form M-2210 (2025; draft 2026), Waiver of underpayment penalty",
                format!(
                    "a waiver for {waiver} is asked for; the commissioner decides it, so the penalty shown is before it"
                ),
            );
        }
        // § 14(e) grants the federal pair: a casualty, disaster or unusual
        // circumstance, and retirement after 62 or disability in this year or
        // last.
        if let Some(o) = estimated::standard_waiver(f, "the Massachusetts commissioner", 62)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        massachusetts_explanation()
    }
}

/// Written by hand and checked against research/estimated-payments/ma.md.
fn massachusetts_explanation() -> Explanation {
    estimated::new_explanation(
        "Massachusetts expects estimated payments if over $400 is due after withholding. The penalty is simple interest on each quarter's shortfall, at a quarterly rate.")
        .paragraph("You avoid the penalty if withholding and estimated payments reach a target. The target is the smaller of 80% of this year's tax or 100% of last year's. Last year's figure needs a return you filed for a full twelve-month year. It stays at 100% however high your income. The 4% surtax on income over $1,107,750 is part of the tax to cover.")
        .paragraph("The payments are four equal quarters. They're due April 15, June 15, September 15 and the next January 15. A weekend or Massachusetts legal holiday shifts a date to the next business day. None of the 2026 dates is shifted. Withholding counts as paid in equal parts on the four dates. You can instead show when it was actually withheld. Filing your return early does not replace the January payment. You may only learn after March 31 that you must pay. The department's instructions then spread the year's amount over the dates left.")
        .paragraph("Tax after credits and withholding of $400 or less this year means no penalty. Nor is there one if you lived in Massachusetts all of last year and had no tax. Otherwise the penalty is interest, not compounded, on each shortfall. It runs from the due date until paid or April 15 of the next year. The rate is the federal short-term rate plus four points. That's 7% from April to June 2026 and 8% after. Form M-2210's annualised method can lower early quarters for late income. The department can waive the penalty for a casualty or disaster. It can also waive it for retiring after 62 or disability, with reasonable cause.")
        .versus_federal("Massachusetts uses 80% of this year's tax, not 90%. It uses 100% of last year's at every income, with no 110% tier. The threshold is $400, not $1,000. The annualised percentages are 20%, 40%, 60% and 80%. There's no relief for filing and paying by January 31. The rate is the federal short-term rate plus four points, set each quarter. The retirement waiver needs age 62.")
        .source("M.G.L. c. 62B §§ 13, 14; c. 62C § 32(a); c. 4 § 7, clause Eighteenth")
        .source("Form 1-ES, Massachusetts Estimated Income Tax, 2026")
        .source("Form M-2210, Underpayment of Massachusetts Estimated Income Tax, and its instructions, 2025, and the draft 2026 Form M-2210")
        .source("AP 241, Estimated Income Tax Payments (01/29/2025)")
        .source("TIR 26-5, Interest Rate on Overpayments and Underpayments (09/01/2026)")
        .build()
        .unwrap_or_default()
}
