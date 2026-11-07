//! Arizona's estimated tax rules, A.R.S. § 43-581, with the penalty of
//! § 42-1125(Q) and the interest of § 42-1123, as the shared
//! [`EstimatedTax`] trait.
//!
//! § 43-581(D) asks the department for rules "similar to" the Internal
//! Revenue Code, and the forms follow 26 U.S.C. § 6654 closely: four equal
//! installments on the federal dates, withholding deemed paid evenly, and
//! the same annualised method. Nothing here imports the federal crate; what
//! Arizona shares with § 6654 is written again with Arizona's citations.
//!
//! The figure follows the department's forms, the 2026 Form 140ES and the
//! 2025 Form 221, and the result's Divergences say where the statutes read
//! otherwise. The research found no act since 2021 changing § 43-581
//! (research/estimated-payments/az.md). Form 221's 2025 instructions say the
//! thresholds "were amended for tax year 2025"; the 2026 Form 140ES prints
//! the statute's current $75,000 and $150,000 for both years, so no form
//! used here predates the law it describes.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Figures, Installment,
    Jurisdiction, LedgerEntry, Month, Penalty, PenaltyLine, PenaltyPart, RateSeries, Requirement,
    Rounding,
};
use filing::Status;
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

const JURISDICTION: Jurisdiction = estimated::ARIZONA;

/// The facts Arizona's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
///
/// The first two are federal figures. § 43-581(E)(1) takes the filer out of
/// Arizona's penalty when a federal exception under 26 U.S.C. § 6654
/// applies, and Form 221 line 1 names two: no federal tax liability last
/// year, and federal tax less federal withholding under $1,000 this year.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    federal_tax_less_withholding: Option<USCents>,
    no_federal_liability: Option<bool>,
    prior_required: Option<bool>,
    nonresident_alien: bool,
}

impl EstimatedFacts {
    /// The total tax on this year's federal return minus federal income tax
    /// withheld, as Form 221's instructions word the exception, or `None`
    /// when the caller did not give it. It can be negative.
    pub fn federal_tax_less_withholding(&self) -> Option<USCents> {
        self.federal_tax_less_withholding
    }

    /// Whether the filer had no federal tax liability last year, was a U.S.
    /// citizen or resident for all of it, and filed (or would have filed) a
    /// return for a full 12 months, or `None` when the caller did not say.
    pub fn no_federal_liability_last_year(&self) -> Option<bool> {
        self.no_federal_liability
    }

    /// Whether the filer was required to file last year's Arizona return, or
    /// `None` when the caller did not say. The 2026 Form 140ES offers last
    /// year's tax as the target only to a filer who was required to file and
    /// did.
    pub fn prior_year_return_required(&self) -> Option<bool> {
        self.prior_required
    }

    /// Reports a filer who elects to be treated as a nonresident alien on
    /// the federal return, who makes three payments under rules of their
    /// own.
    pub fn nonresident_alien(&self) -> bool {
        self.nonresident_alien
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
    /// Sets federal tax less federal withholding.
    pub fn federal_tax_less_withholding(&mut self, a: USCents) -> &mut Self {
        self.e.federal_tax_less_withholding = Some(a);
        self
    }

    /// Answers whether there was no federal tax liability last year.
    pub fn no_federal_liability_last_year(&mut self, none: bool) -> &mut Self {
        self.e.no_federal_liability = Some(none);
        self
    }

    /// Answers whether last year's Arizona return was required.
    pub fn prior_year_return_required(&mut self, required: bool) -> &mut Self {
        self.e.prior_required = Some(required);
        self
    }

    /// Marks a filer who elects nonresident alien treatment.
    pub fn nonresident_alien(&mut self) -> &mut Self {
        self.e.nonresident_alien = true;
        self
    }

    /// Accepts any combination. Federal tax less withholding can be negative
    /// when more was withheld than owed.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// The Arizona gross income that must be exceeded this year and last
    /// before payments are required, § 43-581(A).
    pub(crate) income_threshold: USCents,
    /// The same on a joint return.
    pub(crate) joint_threshold: USCents,

    /// § 43-581(E)(2): no penalty when tax less withholding and credits is
    /// under it.
    pub(crate) tax_threshold: USCents,
    /// The federal exception Form 221 line 1 names.
    pub(crate) federal_threshold: USCents,

    /// § 43-581(A)'s 90%.
    pub(crate) this_year_percent: Rate,
    /// § 43-581(A)'s 100%.
    pub(crate) prior_year_percent: Rate,

    /// § 42-1125(Q): the penalty on an underpayment never exceeds this share
    /// of it.
    pub(crate) penalty_cap: Rate,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// The date the 140ES prints for filing and paying in full instead of
    /// the fourth installment, already moved off a weekend.
    pub(crate) file_and_pay_by: Date,

    /// The last day Form 221 charges.
    pub(crate) stop: Date,

    pub(crate) period_ends: Vec<Date>,
    pub(crate) annualised_percentages: Vec<Rate>,

    /// The § 42-1123 rate by Form 221 rate period, a calendar quarter each,
    /// as far as the department has published it.
    pub(crate) rates: RateSeries,
}

/// Form 221 line 11, the required annual payment divided by four, and its
/// instructions for line 12, which deem a quarter of the withholding paid on
/// each due date.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// Returns Arizona's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(ArizonaEstimated { y }))
}

struct ArizonaEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,

    /// No penalty can arise, so nothing is needed by any date.
    exempt: bool,

    /// Whether this year's tax was given. Without it the target is last
    /// year's tax alone, and the penalty is an upper bound.
    tax_known: bool,

    dues: Vec<Date>,
    /// Each installment's own amount, before excusing.
    installments: Vec<USCents>,
    /// The January installment, for filing early.
    excused: bool,

    notes: Notes,
}

/// Which divergences change these facts' figures or answers.
#[derive(Clone, Copy, Debug, Default)]
struct Affected {
    boundary: bool,
    prior_return: bool,
    stop: bool,
    rates: bool,
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

/// A year's Arizona gross income: [`Figures::gross_income`], or federal AGI
/// when only that was given, which the second value reports.
fn gross_income(fig: &Figures) -> Option<(USCents, bool)> {
    if let Some(gi) = fig.gross_income() {
        return Some((gi, false));
    }
    fig.federal_agi().map(|agi| (agi, true))
}

/// A question whose answer can change the target. It is asked only when no
/// exception sets the target.
#[derive(Clone, Debug, Default)]
struct TargetQuestions(Vec<(PositionKind, String, String)>);

impl TargetQuestions {
    fn ask(
        &mut self,
        position: PositionKind,
        question: impl Into<String>,
        effect: impl Into<String>,
    ) {
        self.0.push((position, question.into(), effect.into()));
    }

    /// Adds the questions to `notes` unless an exception set the target.
    fn ask_unless_exempt(self, notes: &mut Notes, exempt: bool) {
        if exempt {
            return;
        }
        for (position, question, effect) in self.0 {
            notes.ask(position, question, effect);
        }
    }
}

impl ArizonaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "arizonataxcalc: the facts carry {}, not arizonataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, e: &EstimatedFacts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "arizonataxcalc: these are Arizona's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "arizonataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or fisherman for federal purposes makes one Arizona installment, due January 15, which is not built",
            ));
        }
        if e.nonresident_alien() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a nonresident alien makes three Arizona installments, with 50% in the first, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                if let Some(end) = self.y.period_ends.get(i)
                    && p.end() != *end
                {
                    return Err(Error::Invalid(format!(
                        "arizonataxcalc: Form 221 annualisation period {} ends on {}, not {}",
                        i + 1,
                        p.end(),
                        end
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and the Arizona Revised Statutes part
    /// ways on a rule every Arizona computation uses.
    fn divergences(&self, aff: Affected) -> Result<Vec<Divergence>, Error> {
        let y = &self.y;
        let mut out = Vec::new();
        for b in [
            working::new_divergence("Arizona gross income of exactly the threshold")
                .form(
                    format!(
                        "Form 221's instructions excuse the penalty only below the threshold. That's Arizona gross income under it in either year. So exactly {} ({} joint) would be subject to it. \
                         The 2026 Form 140ES, and Form 221's own table, require more than the threshold.",
                        y.income_threshold.whole(),
                        y.joint_threshold.whole()
                    ),
                    "Form 221 (2025) instructions, Am I Subject to the Penalty?; 2026 Form 140ES instructions, Required Payments",
                )
                .statute(
                    format!(
                        "Payments are required only if Arizona gross income exceeds {} this year. It must also have been greater than that last year. The joint figure is {}, and exactly {} isn't subject.",
                        y.income_threshold.whole(),
                        y.joint_threshold.whole(),
                        y.income_threshold.whole()
                    ),
                    "A.R.S. § 43-581(A)",
                )
                .uses(Reading::Statute)
                .affects(aff.boundary),
            working::new_divergence("The prior-year option needs a filed return")
                .form(
                    "The 2026 Form 140ES offers 100% of last year's tax under two conditions. You must have been required to file a 2025 Arizona return, and filed it. Form 221 (2025) line 8 offers it only if you filed. \
                     Neither asks for a 12-month return.",
                    "2026 Form 140ES instructions, How Much Should My Estimated Payments Total?, and worksheet line 23; Form 221 (2025) instructions, line 8",
                )
                .statute(
                    "Payments must reach 100% of the tax for the year before. There's no condition that a return was required, filed or covered 12 months. \
                     The law asks the department for rules similar to the federal ones.",
                    "A.R.S. § 43-581(A), (D)",
                )
                .uses(Reading::Form)
                .affects(aff.prior_return),
            working::new_divergence("When the penalty stops")
                .form(
                    "Each shortfall is charged until it's paid or April 15 of the next year. The charge stops at whichever comes first. A balance paid with a late return counts as paid on April 15.",
                    "Form 221 (2025), Part B lines 26 and 27, and its instructions for Rate Periods 1 to 4",
                )
                .statute(
                    format!(
                        "The penalty is the interest the unpaid amount would build up while unpaid. It can't exceed {} of that amount, and it has no April 15 end.",
                        y.penalty_cap
                    ),
                    "A.R.S. § 42-1125(Q)",
                )
                .uses(Reading::Form)
                .affects(aff.stop),
            working::new_divergence("The 2026 rates")
                .form(
                    "Form 221 (2025) prints 7% for each of its 2025 rate periods. It leaves Jan 1 to Apr 15, 2026 to be set later. \
                     For 2026 the department publishes 6% from April 1 to June 30. It publishes 7% from July 1 to December 31.",
                    "Form 221 (2025), Part B lines 16 to 27",
                )
                .statute(
                    "The rate is the federal short-term rate plus three percentage points. It's applied as the federal rate is, so it's set for each quarter.",
                    "A.R.S. § 42-1123(A); ADOR, Interest Rates",
                )
                .uses(Reading::Statute)
                .affects(aff.rates),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let extras = self.extras(f)?;
        self.check(f, &extras)?;
        let y = &self.y;
        let mut a = Assessment::default();
        let mut target_qs = TargetQuestions::default();
        let mut aff = Affected::default();

        let last = y.year - 1;
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();
        let prior_figures = if has_prior {
            prior.figures().unwrap_or_default()
        } else {
            Figures::default()
        };
        let threshold = if f.filing_status() == Status::MarriedFilingJointly {
            y.joint_threshold
        } else {
            y.income_threshold
        };

        // 90% of this year's tax after credits: Form 221 lines 4 and 7.
        let this_year = if tax_known {
            estimated::new_target_option(
                Basis::ThisYearTax,
                y.this_year_percent,
                y.this_year_percent.of(tax),
            )
            .build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, y.this_year_percent, USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            a.notes.rule(
                "A.R.S. § 43-581(A); Form 221 (2025), lines 4 and 7",
                format!(
                    "{} of this year's Arizona tax after credits of {} is {}",
                    y.this_year_percent,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // 100% of last year's tax after credits: Form 221 line 8, the 140ES
        // worksheet line 23. There is no 110% tier.
        let required_to_file = extras.prior_year_return_required();
        let unavailable = |why: String| {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_percent, USCents(0));
            b.unavailable(why);
            b
        };
        let prior_option = if !has_prior {
            target_qs.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Arizona return, and what was its tax?"),
                format!(
                    "With it, {} of last year's tax could be a target below {} of this year's.",
                    y.prior_year_percent, y.this_year_percent
                ),
            );
            unavailable(format!("Needs your {last} Arizona return"))
        } else if prior.not_required_to_file() {
            aff.prior_return = true;
            unavailable(format!(
                "You weren't required to file a {last} Arizona return"
            ))
        } else if !prior.filed() {
            aff.prior_return = true;
            unavailable(format!("You didn't file a {last} Arizona return"))
        } else if required_to_file == Some(false) {
            aff.prior_return = true;
            unavailable(format!(
                "You weren't required to file your {last} Arizona return"
            ))
        } else {
            let prior_tax = prior_figures.tax().unwrap_or_default();
            let amount = y.prior_year_percent.of(prior_tax);
            a.notes.rule(
                "A.R.S. § 43-581(A); 2026 Form 140ES worksheet, line 23; Form 221 (2025), line 8",
                format!(
                    "{} of the {} tax after credits on the {last} return is {}; Arizona has no higher percentage at higher income",
                    y.prior_year_percent, prior_tax, amount
                ),
            );
            if prior.months() != 12 {
                target_qs.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "The federal test needs a 12-month return, and Arizona's law and forms don't. So the figures let your {last} return of {} months set the target.",
                        prior.months()
                    ),
                );
            }
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_percent, amount)
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            let effect = if prior_opt.available() {
                "Without it, the target can only come from last year's tax.".to_string()
            } else {
                format!("Without it, there's no target, because your {last} return can't set one.")
            };
            target_qs.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Arizona tax to be?", y.year),
                effect,
            );
        }

        // The income test, in both years.
        let this_gi = gross_income(&f.this_year());
        let prior_gi = gross_income(&prior_figures);
        let measured = |year: i64, from_agi: bool| {
            if from_agi {
                format!("{year} federal AGI, taken as Arizona gross income,")
            } else {
                format!("{year} Arizona gross income")
            }
        };
        let no_federal = extras.no_federal_liability_last_year();
        let federal = extras.federal_tax_less_withholding();

        aff.boundary = this_gi.is_some_and(|(v, _)| v == threshold)
            || prior_gi.is_some_and(|(v, _)| v == threshold);
        match (this_gi, prior_gi) {
            (Some((this_v, this_agi)), _) if this_v <= threshold => {
                a.exempt = true;
                let exception = estimated::exception_at_most(
                    &format!("{} Arizona gross income", y.year),
                    this_v,
                    threshold,
                )?;
                rb.required(
                    false,
                    format!(
                        "Your {} Arizona gross income of {} isn't more than {}. Arizona doesn't require estimated payments for {} and charges no penalty.",
                        y.year,
                        this_v.whole(),
                        threshold.whole(),
                        y.year
                    ),
                )
                .exempt(exception);
                a.notes.rule(
                    "A.R.S. § 43-581(A); 2026 Form 140ES instructions, Required Payments",
                    format!(
                        "the {} {} does not exceed {}, so no estimated payments are required",
                        measured(y.year, this_agi),
                        this_v,
                        threshold
                    ),
                );
            }
            (_, Some((prior_v, prior_agi))) if prior_v <= threshold => {
                a.exempt = true;
                let exception = estimated::exception_at_most(
                    &format!("{last} Arizona gross income"),
                    prior_v,
                    threshold,
                )?;
                rb.required(
                    false,
                    format!(
                        "Your {last} Arizona gross income of {} wasn't more than {}. Arizona doesn't require estimated payments for {} and charges no penalty.",
                        prior_v.whole(),
                        threshold.whole(),
                        y.year
                    ),
                )
                .exempt(exception);
                a.notes.rule(
                    "A.R.S. § 43-581(A); 2026 Form 140ES instructions, Required Payments",
                    format!(
                        "the {} {} was not greater than {}, so no estimated payments are required for {}",
                        measured(last, prior_agi),
                        prior_v,
                        threshold,
                        y.year
                    ),
                );
            }
            _ if no_federal == Some(true) => {
                a.exempt = true;
                let exception =
                    estimated::new_exception("No federal tax liability last year").build()?;
                rb.required(
                    false,
                    format!(
                        "You had no federal tax liability for {last}. That was a full year as a U.S. citizen or resident. Arizona follows that federal exception, so no penalty can arise for {}.",
                        y.year
                    ),
                )
                .exempt(exception);
                a.notes.rule(
                    "A.R.S. § 43-581(E)(1); 26 U.S.C. § 6654(e)(2); Form 221 (2025), line 1",
                    format!(
                        "no federal tax liability for {last} on a 12-month return is a federal exception, so there is no Arizona penalty for {}",
                        y.year
                    ),
                );
            }
            _ if federal.is_some_and(|v| v < y.federal_threshold) => {
                let federal = federal.unwrap_or_default();
                a.exempt = true;
                let exception = estimated::exception_below(
                    "Federal tax less federal withholding",
                    federal,
                    y.federal_threshold,
                )?;
                rb.required(
                    false,
                    format!(
                        "Your {} federal tax less federal withholding is {}, under {}. Arizona follows that federal exception, so no Arizona penalty can arise.",
                        y.year,
                        federal.whole(),
                        y.federal_threshold.whole()
                    ),
                )
                .exempt(exception);
                a.notes.rule(
                    "A.R.S. § 43-581(E)(1); 26 U.S.C. § 6654(e)(1); Form 221 (2025), line 1",
                    format!(
                        "federal tax less federal withholding of {} is under {}, a federal exception, so there is no Arizona penalty",
                        federal, y.federal_threshold
                    ),
                );
            }
            _ if !tax_known && !prior_opt.available() => {
                rb.required(
                    false,
                    format!(
                        "Your target needs your {} Arizona tax, because your {last} return can't set one.",
                        y.year
                    ),
                )
                .no_target();
            }
            _ if !tax_known && withheld >= target => {
                rb.required(
                    false,
                    format!(
                        "Your withholding of {} reaches {} of your {last} Arizona tax, {}. No estimated payments are needed, whatever your {} tax is.",
                        withheld.whole(),
                        y.prior_year_percent,
                        target.whole(),
                        y.year
                    ),
                )
                .choose(chosen);
            }
            _ if !tax_known => {
                rb.required(
                    true,
                    format!(
                        "Your target is {} of your {last} Arizona tax, {}. That keeps you clear of a penalty whatever your {} tax turns out to be. After {} of withholding, that's {} of estimated payments. Pay a quarter by each date.",
                        y.prior_year_percent,
                        target.whole(),
                        y.year,
                        withheld.whole(),
                        (target - withheld).whole()
                    ),
                )
                .choose(chosen);
            }
            _ if tax - withheld < y.tax_threshold => {
                a.exempt = true;
                let exception = estimated::exception_below(
                    "Tax after credits less withholding",
                    tax - withheld,
                    y.tax_threshold,
                )?;
                let under = if tax < withheld {
                    String::new()
                } else {
                    format!(", which is under {}", y.tax_threshold.whole())
                };
                rb.required(
                    false,
                    format!(
                        "Your Arizona tax after credits is {}, and {} was withheld. That {}{}. No estimated payments are needed.",
                        tax.whole(),
                        withheld.whole(),
                        estimated::leaves(tax - withheld),
                        under
                    ),
                )
                .exempt(exception);
                a.notes.rule(
                    "A.R.S. § 43-581(E)(2); Form 221 (2025), line 6",
                    format!(
                        "{} of tax after credits less {} withheld is {}, under {}, so there is no penalty",
                        tax,
                        withheld,
                        tax - withheld,
                        y.tax_threshold
                    ),
                );
            }
            _ if withheld >= target => {
                rb.required(
                    false,
                    format!(
                        "Your Arizona withholding of {} covers the {} required for the year. No estimated payments are needed.",
                        withheld.whole(),
                        target.whole()
                    ),
                )
                .choose(chosen);
            }
            _ => {
                let mut why = format!(
                    "Your Arizona tax of {} less {} withheld leaves {}. That's {} or more, and withholding falls short of the {} required. You need {} of estimated payments, a quarter by each date.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.tax_threshold.whole(),
                    target.whole(),
                    (target - withheld).whole()
                );
                if this_gi.is_some() && prior_gi.is_some() {
                    why = format!(
                        "Your Arizona gross income is over {} in both years. {why}",
                        threshold.whole()
                    );
                }
                rb.required(true, why).choose(chosen);
            }
        }
        a.requirement = rb.build()?;
        target_qs.ask_unless_exempt(&mut a.notes, a.exempt);

        if !a.exempt {
            if let (Some((this_v, this_agi)), Some((prior_v, prior_agi))) = (this_gi, prior_gi) {
                a.notes.rule(
                    "A.R.S. § 43-581(A); 2026 Form 140ES instructions, Required Payments",
                    format!(
                        "the {} {} exceeds {} and the {} {} was greater than it, so estimated payments are required",
                        measured(y.year, this_agi),
                        this_v,
                        threshold,
                        measured(last, prior_agi),
                        prior_v
                    ),
                );
            }
            for (year, g) in [(y.year, this_gi), (last, prior_gi)] {
                match g {
                    None => {
                        a.notes.ask(
                            PositionKind::AdjustedGrossIncome,
                            format!("What was your {year} Arizona gross income?"),
                            format!(
                                "Until then, the figures take it as over {}. Payments are needed only when it's over {} in both {last} and {}.",
                                threshold.whole(),
                                threshold.whole(),
                                y.year
                            ),
                        );
                    }
                    Some((_, true)) => {
                        a.notes.ask(
                            PositionKind::AdjustedGrossIncome,
                            format!("What was your {year} Arizona gross income?"),
                            "Until then, the figures use your federal AGI. That's your Arizona gross income if you lived in Arizona all year.",
                        );
                    }
                    Some((_, false)) => {}
                }
            }
            if no_federal.is_none() {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!("Did you have no federal tax liability for {last}?"),
                    "That needs a full-year return as a U.S. citizen or resident. If so, there's no Arizona penalty.",
                );
            }
            if federal.is_none() {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "What is your {} federal tax less federal withholding?",
                        y.year
                    ),
                    format!(
                        "If it's under {}, there's no Arizona penalty.",
                        y.federal_threshold.whole()
                    ),
                );
            }
            if a.requirement
                .chosen()
                .is_some_and(|c| c.basis() == Basis::PriorYearTax)
                && required_to_file.is_none()
            {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!("Were you required to file your {last} Arizona return?"),
                    format!(
                        "If not, the {} Form 140ES doesn't offer {} of last year's tax as a target.",
                        y.year, y.prior_year_percent
                    ),
                );
            }
        }

        // The due dates are the federal ones, § 43-581(A), moved off a
        // weekend or legal holiday as the 140ES says.
        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        for (i, due) in a.dues.iter().enumerate() {
            if *due != y.dues[i] {
                a.notes.rule(
                    "2026 Form 140ES",
                    format!(
                        "the installment due {} falls on a weekend or legal holiday, so it is due {}",
                        y.dues[i].long(),
                        due.long()
                    ),
                );
            }
        }
        let cumulative = estimated::cumulative_shares(a.requirement.target(), &quarters())?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "A.R.S. § 43-581(A); 2026 Form 140ES instructions; Form 221 (2025), line 11",
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

        if f.elections().annualised() && !a.exempt {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &y.annualised_percentages, &a.installments)?;
            let p = &y.annualised_percentages;
            a.notes.rule(
                "A.R.S. § 43-581(D); Form 221 (2025), Annualized Income Installment Worksheet",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: {}, {}, {} and {} of the annualised tax, \
                     less earlier installments, and never more than the regular installment plus any shortfall the method carried forward",
                    ann[0], ann[1], ann[2], ann[3], p[0], p[1], p[2], p[3]
                ),
            );
            a.installments = ann;
        }

        // Filing and paying in full by the date the 140ES prints excuses the
        // fourth installment.
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(y.file_and_pay_by) && !a.exempt {
            a.excused = true;
            a.notes.rule(
                "2026 Form 140ES instructions, item 1; Form 221 (2025) instructions, line 11",
                format!(
                    "the return was filed and paid on {}, by {}, so the fourth installment is excused",
                    filed.long(),
                    y.file_and_pay_by.long()
                ),
            );
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        // Which divergences change these figures turns on the shortfalls.
        aff.prior_return = aff.prior_return && !a.exempt;
        if !a.exempt {
            let (ledger, _) = self.ledger(f, &a)?;
            (aff.stop, aff.rates) = self.shortfalls_matter(f, &ledger);
        }
        let divergences = self.divergences(aff)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// Reports whether a shortfall is still unpaid after the form's April 15
    /// stop, and whether one accrues in 2026, whose rates no Form 221
    /// prints.
    fn shortfalls_matter(&self, f: &Facts, ledger: &[LedgerEntry]) -> (bool, bool) {
        let filed = f.return_filed();
        let paid_by_stop = !filed.is_zero() && !filed.after(self.y.stop);
        let year_end = estimated::must_date(self.y.year, Month::DECEMBER, 31);
        let (mut past_stop, mut in_2026) = (false, false);
        for l in ledger {
            if l.shortfall().0 <= 0 {
                continue;
            }
            if l.due().before(year_end) {
                in_2026 = true;
            }
            for p in l.pieces() {
                if !paid_by_stop && p.paid_on().is_none_or(|on| on.after(self.y.stop)) {
                    past_stop = true;
                }
            }
        }
        (past_stop, in_2026)
    }

    /// Applies the credits to the installments, with the fourth set to
    /// nothing when filing early excused it.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &quarters())?
        };
        let mut required = a.installments.clone();
        if a.excused {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "Form 221 (2025) instructions, line 12",
                format!(
                    "the {} withheld is credited on the dates it was withheld, as the instructions allow a filer who can show it",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "Form 221 (2025) instructions, line 12",
                format!(
                    "the {} withheld is treated as paid a quarter on each due date, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        Ok((ledger, notes))
    }

    /// The schedule the ledger gives, with January excused when the return
    /// was filed and paid early.
    fn installments(
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

    /// The penalty on one installment's shortfall, as a column of Form 221
    /// Part B computes it.
    ///
    /// Each piece of the shortfall is charged simple interest by the day
    /// from the due date until it was paid or `stop`, one part for each
    /// quarter's rate period. If any of it is still unpaid on January 1, the
    /// penalty accrued on the installment through December 31 is added to
    /// its principal, as line 27 and A.R.S. § 42-1123(B) do, and charged
    /// from then until the last of the shortfall is paid. The line is then
    /// capped at the share of the shortfall § 42-1125(Q) allows, line 29, by
    /// cutting the charge where the cap is reached.
    fn line(
        &self,
        l: &LedgerEntry,
        stop: Date,
        notes: &mut Notes,
    ) -> Result<(PenaltyLine, Date), Error> {
        let y = &self.y;
        let mut unrated = Date::default();
        let mut gap_at = |g: Date| {
            if !g.is_zero() && (unrated.is_zero() || g.before(unrated)) {
                unrated = g;
            }
        };
        let mut parts: Vec<PenaltyPart> = Vec::new();
        let mut last = l.due();
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
            if to.after(last) {
                last = to;
            }
            let (ps, gap) = estimated::simple_daily(piece.amount(), l.due(), to, &y.rates)?;
            gap_at(gap);
            parts.extend(ps);
        }

        let year_end = estimated::must_date(y.year, Month::DECEMBER, 31);
        if last.after(year_end) {
            let accrued: USCents = parts
                .iter()
                .filter(|p| !p.to().after(year_end))
                .map(|p| p.charge())
                .sum();
            if accrued.0 > 0 {
                let (ps, gap) = estimated::simple_daily(accrued, year_end, last, &y.rates)?;
                gap_at(gap);
                parts.extend(ps);
                notes.rule(
                    "A.R.S. § 42-1123(B); Form 221 (2025), line 27",
                    format!(
                        "the {} of penalty on the {} installment outstanding on {} is added to its principal and charged until {}",
                        accrued,
                        l.due().long(),
                        year_end.add_days(1).long(),
                        last.long()
                    ),
                );
            }
        }

        let limit = y.penalty_cap.of(l.shortfall());
        let (parts, capped) = cap_parts(&parts, limit)?;
        let mut lb = estimated::new_penalty_line(l.number(), l.due(), l.shortfall());
        lb.days(last.days_since(l.due()));
        for p in parts {
            lb.part(p);
        }
        let line = lb.build()?;
        let how = if capped { "held to" } else { "under" };
        notes.rule(
            "A.R.S. § 42-1125(Q); Form 221 (2025), line 29",
            format!(
                "the penalty on the {} installment's {} shortfall is {}, {} the cap of {} of it, {}",
                l.due().long(),
                l.shortfall(),
                line.total(),
                how,
                y.penalty_cap,
                limit
            ),
        );
        Ok((line, unrated))
    }
}

/// Cuts the charges so that together they come to no more than `limit`,
/// keeping the earlier parts whole. It reports whether any was cut.
fn cap_parts(parts: &[PenaltyPart], limit: USCents) -> Result<(Vec<PenaltyPart>, bool), Error> {
    let mut out = Vec::with_capacity(parts.len());
    let mut sum = USCents(0);
    let mut capped = false;
    for p in parts {
        let mut charge = p.charge();
        if sum + charge > limit {
            (charge, capped) = ((limit - sum).max(USCents(0)), true);
        }
        sum += charge;
        if charge == p.charge() {
            out.push(p.clone());
            continue;
        }
        out.push(
            estimated::new_penalty_part(p.kind(), p.principal(), p.rate())
                .span(p.from(), p.to())
                .days(p.days())
                .charge(charge)
                .source(p.source())
                .build()?,
        );
    }
    Ok((out, capped))
}

/// "Aug 31" inside the tax year and "Jan 15, 2027" outside it.
fn short_date(d: Date, year: i64) -> String {
    let m: String = d.month().to_string().chars().take(3).collect();
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

impl EstimatedTax for ArizonaEstimated {
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
                    "with no {} Arizona return to measure against, there is no target to schedule",
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
            return Err(Error::Invalid(
                "arizonataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Arizona return to measure against, there is no target to figure a penalty from",
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
        let waiver = waiver_opt.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let schedule = self.installments(&a, &ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut unrated = Date::default();
        for l in &ledger {
            if l.shortfall().0 <= 0 {
                continue;
            }
            let (line, gap) = self.line(l, stop, &mut notes)?;
            if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                unrated = gap;
            }
            pb.line(line);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            notes.rule(
                "A.R.S. § 43-581(A); Form 221 (2025), line 9",
                format!(
                    "this year's Arizona tax was not given, so the penalty is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and {} of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    y.prior_year_percent,
                    y.year - 1,
                    y.this_year_percent,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "Form 221 (2025) instructions, Rate Period 4",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "A.R.S. §§ 42-1123, 42-1125(Q); Form 221 (2025), Part B",
            format!(
                "each shortfall is charged the department's rate for each quarter, by the day over 365, from its due date until paid or {}, \
                 with each payment applied to the oldest shortfall first; the penalty to {} is {}",
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if p.total().0 > 0 {
            notes.rule(
                "Form 221 (2025) instructions, Box 30c",
                format!(
                    "the form rounds the total penalty to the nearest whole dollar, {}",
                    Rounding::RoundToDollar.round(p.total())
                ),
            );
        }
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
        if p.total().0 > 0 && waiver_opt.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking for the penalty to be waived?",
                "The department can waive it for a casualty, disaster or other unusual event. Retiring after 62 or becoming disabled also counts, with reasonable cause.",
            );
        }
        if let Some(o) = estimated::standard_waiver(f, "the Arizona Department of Revenue", 62)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        ARIZONA_EXPLANATION.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/az.md.
static ARIZONA_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Arizona asks for estimated payments only above a gross income threshold. Your Arizona gross income must be over $75,000 both last year and this year. On a joint return the threshold is $150,000. The penalty is interest on each quarter's shortfall. It can never be more than 10% of the shortfall.",
    )
    .paragraph("Your Arizona gross income may be at or under the threshold in either year. Then Arizona doesn't expect estimated payments and charges no penalty. That holds however much tax is due. There's also no penalty for a small balance. That's Arizona tax after credits, less Arizona withholding, under $1,000.")
    .paragraph("If you do have to pay, withholding and estimated payments must reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. There is no 110% rule for higher incomes. The department's 2026 instructions limit last year's figure. You must have been required to file a 2025 Arizona return, and filed it. Federal relief carries over. With no federal tax liability last year, there's no Arizona penalty either. Federal tax less federal withholding under $1,000 this year does the same.")
    .paragraph("The payments are a quarter each. They're due April 15, June 15 and September 15, 2026, and January 15, 2027. A date on a weekend or legal holiday shifts to the next business day. Withholding counts as paid evenly on those dates. You can instead show when it was actually taken. You can file your 2026 return and pay in full by February 1, 2027. That replaces the January payment.")
    .paragraph("The penalty is figured like interest. The rate is the federal short-term rate plus three points. That's 6% for April to June 2026 and 7% from July. Each shortfall is charged by the day until paid. The department's form stops counting on April 15 of the next year. Penalty still unpaid on January 1 is added to the amount it's charged on. Form 221's annualised worksheet can lower early payments for uneven income. The department can waive the penalty for a casualty or disaster. Retiring after 62 or becoming disabled also counts, with reasonable cause.")
    .versus_federal("Arizona differs from federal where a number can change. Whether you must pay turns on Arizona gross income in both years. The line is over $75,000, or $150,000 joint. There's no 110% tier. The penalty can never exceed 10% of the shortfall. Penalty unpaid on January 1 is itself charged. A federal exception removes the Arizona penalty too.")
    .source("A.R.S. §§ 43-581, 42-1123, 42-1125(Q)")
    .source("Arizona Form 140ES and its instructions, 2026")
    .source("Arizona Form 221, Underpayment of Estimated Tax by Individuals, and its instructions, 2025")
    .source("ADOR, Interest Rates")
    .build()
    .unwrap_or_default()
});
