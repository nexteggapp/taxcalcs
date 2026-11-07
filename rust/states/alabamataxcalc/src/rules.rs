//! Alabama's estimated tax rules, Code of Ala. § 40-18-80, as the shared
//! [`estimated::EstimatedTax`] interface.
//!
//! § 40-18-80(a) adopts 26 U.S.C. § 6654 with three changes: $500 replaces
//! the $1,000 small-tax amount in § 6654(e)(1), and § 6654(d)(2)(C)(iii) and
//! § 6654(f)(2), both about self-employment tax, do not apply. Everything
//! else in § 6654 applies, with Alabama's income tax in place of the federal
//! one. This crate imports no other jurisdiction, so the § 6654 arithmetic
//! is written here again rather than taken from ustaxcalc.
//!
//! The figure follows the Department of Revenue's forms: the 2026 Form 40ES
//! and the 2025 Form 2210AL, the latest penalty form. Those forms leave out
//! three rules § 6654 grants and give a late starter a schedule § 6654 does
//! not; the result's divergences say where. § 40-18-80 has not been amended
//! since Act 2010-685 (research/estimated-payments/al.md), so no form here
//! predates the law it describes.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Figures, Installment,
    Jurisdiction, LedgerEntry, Penalty, RateSeries, Requirement, TargetOption, TargetOptionBuilder,
};
use filing::Status as FilingStatus;
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// Alabama's postal code.
const JURISDICTION: Jurisdiction = estimated::ALABAMA;

/// The facts Alabama's rules read that no other jurisdiction built here
/// does. Carry them in [`estimated::Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_farmer: Option<bool>,
}

impl EstimatedFacts {
    /// Whether at least two-thirds of last year's gross income was from
    /// farming or fishing, or `None` when the caller did not say. Form
    /// 2210AL sends a filer to its Part IV, for farmers and fishermen, when
    /// either year's income was, and [`estimated::Facts`] carries only this
    /// year's share.
    pub fn prior_year_farmer_or_fisher(&self) -> Option<bool> {
        self.prior_farmer
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
    /// Answers whether two-thirds of last year's gross income was from
    /// farming or fishing.
    pub fn prior_year_farmer_or_fisher(&mut self, farmer: bool) -> &mut Self {
        self.e.prior_farmer = Some(farmer);
        self
    }

    /// Has nothing to refuse: the only fact is a yes or no.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// § 6654(e)(1) as § 40-18-80(a)(2) amends it: no penalty when tax less
    /// withholding is under it.
    pub(crate) threshold: USCents,

    /// The 90%, 100% and 110% of § 6654(d)(1)(B) and (C).
    pub(crate) this_year_percent: Rate,
    pub(crate) prior_year_percent: Rate,
    pub(crate) high_income_percent: Rate,

    /// Last year's AGI above which the prior-year test is
    /// `high_income_percent`, and the step for a married filer on a separate
    /// return.
    pub(crate) prior_year_step: USCents,
    pub(crate) prior_year_step_separate: USCents,

    /// Each installment's part of the required annual payment, and the part
    /// of the year's withholding deemed paid on each due date.
    pub(crate) shares: Vec<Rate>,

    /// Schedule AI line 17.
    pub(crate) annualised_percentages: Vec<Rate>,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// The first days of the Form 40ES schedule's second, third and fourth
    /// rows: a requirement first met on or after `late_groups[i]` starts, on
    /// that schedule, at installment i+2.
    pub(crate) late_groups: Vec<Date>,

    /// § 6654(h)'s January 31, which no Alabama form prints.
    pub(crate) file_and_pay_by: Date,

    /// The last day the penalty runs, April 15 of the next year.
    pub(crate) stop: Date,

    pub(crate) period_ends: Vec<Date>,

    /// The rate Form 2210AL line 11 prints for the whole year.
    pub(crate) form_rate: Rate,

    /// The Department of Revenue's published rates, quarter by quarter, as
    /// far as they have been published.
    pub(crate) rates: RateSeries,
}

/// The year this build carries, or `None`.
fn estimated_year(year: i64) -> Option<Result<EstimatedYear, Error>> {
    match year {
        2026 => Some(crate::estimated_y2026::year_2026()),
        _ => None,
    }
}

/// Returns Alabama's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    match estimated_year(year) {
        Some(y) => Ok(Box::new(AlabamaEstimated { y: y? })),
        None => Err(estimated::no_rules(&JURISDICTION, year)),
    }
}

struct AlabamaEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
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

/// Which divergences change these facts' figures or answers.
#[derive(Default)]
struct Affected {
    rate: bool,
    agi_year: bool,
    january: bool,
    no_liability: bool,
    late_start: bool,
}

impl AlabamaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(invalid(format!(
                    "alabamataxcalc: the facts carry {}, not alabamataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, e: &EstimatedFacts) -> Result<(), Error> {
        let farmer_last_year = e.prior_year_farmer_or_fisher() == Some(true);
        if f.jurisdiction() != JURISDICTION {
            return Err(invalid(format!(
                "alabamataxcalc: these are Alabama's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(invalid(format!(
                "alabamataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() || farmer_last_year {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or fisherman with two-thirds of gross income from farming or fishing files Form 2210AL Part IV, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                if p.end() != self.y.period_ends[i] {
                    return Err(invalid(format!(
                        "alabamataxcalc: Schedule AI period {} ends on {}, not {}",
                        i + 1,
                        self.y.period_ends[i],
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the Department of Revenue's forms and § 40-18-80, through the §
    /// 6654 it adopts, part ways. They are found in
    /// research/estimated-payments/al.md, questions 3, 5, 7 and 10 and its
    /// verification, and in the statute text in testdata/.
    fn divergences(&self, inn: &Affected) -> Result<Vec<Divergence>, Error> {
        let y = &self.y;
        Ok(vec![
            working::new_divergence("The interest rate")
                .form(
                    format!(
                        "The {} Form 2210AL prints one rate, {}, for every day of the penalty. That was the rate for every quarter of {}. \
                         For {} the department publishes a rate for each quarter. It is 6% from April to June and 7% after.",
                        y.year - 1,
                        y.form_rate,
                        y.year - 1,
                        y.year
                    ),
                    "Form 2210AL (2025), Part III line 11; ADOR, Quarterly Interest Rates",
                )
                .statute(
                    "The penalty is interest at the federal underpayment rate. It runs for the time the payment is late. When the rate changes, each rate applies to the days it covers.",
                    "Code of Ala. §§ 40-18-80(a), 40-1-44(a); 26 U.S.C. § 6654(a)",
                )
                .uses(Reading::Statute)
                .affects(inn.rate)
                .build()?,
            working::new_divergence("Which year's AGI sets the 110% test")
                .form(
                    format!(
                        "The {} Form 2210AL instructions test the penalty year's own AGI. For the {} penalty they ask for \"AGI for {}\". The {} Form 40ES tests your {} Alabama AGI for {}.",
                        y.year - 1,
                        y.year - 1,
                        y.year - 1,
                        y.year,
                        y.year - 1,
                        y.year
                    ),
                    "Form 2210AL instructions (2025), Higher income taxpayers; Form 40ES instructions (2026), Special Rule for Higher Income Taxpayers",
                )
                .statute(
                    "110% applies if last year's AGI was over $150,000, or $75,000 filing separately.",
                    "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(d)(1)(C)",
                )
                .uses(Reading::Statute)
                .affects(inn.agi_year)
                .build()?,
            working::new_divergence("A return filed and paid by January 31")
                .form(
                    format!(
                        "Neither form excuses the January payment for a return filed early. That means filed and paid in full by {}.",
                        short_date(y.file_and_pay_by, y.year)
                    ),
                    "Form 40ES instructions (2026); Form 2210AL (2025) and its instructions",
                )
                .statute(
                    "The January payment has no penalty if you file and pay in full by January 31. That January 31 is in the next year.",
                    "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(h)",
                )
                .uses(Reading::Form)
                .affects(inn.january)
                .build()?,
            working::new_divergence("No tax liability last year")
                .form(
                    "Form 2210AL has no exception for a year with no tax liability before it. A full-year return showing no tax already makes the target $0. The gap matters only if you had no return to file.",
                    "Form 2210AL (2025), Part I, and its instructions",
                )
                .statute(
                    "No penalty follows a full 12-month year with no Alabama income tax liability. You must also have been a U.S. citizen or resident all that year.",
                    "Code of Ala. § 40-18-80(a), (b)(1); 26 U.S.C. § 6654(e)(2)",
                )
                .uses(Reading::Form)
                .affects(inn.no_liability)
                .build()?,
            working::new_divergence("A requirement first met after April 1")
                .form(
                    format!(
                        "The 40ES covers a requirement that first arises after April 1. It says to pay a third of your estimated tax by June 15. \
                         After June 1 it says half by Sep 15, and after Sep 1 all of it by Jan 15, {}. \
                         Form 2210AL still requires 25% by every date.",
                        y.year + 1
                    ),
                    "Form 40ES instructions (2026), Changes In Tax and the voucher schedule; Form 2210AL (2025), Part III line 1",
                )
                .statute(
                    "There are four payments, each 25% of the required annual payment. No rule covers a requirement that first arises late in the year.",
                    "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(c), (d)(1)(A)",
                )
                .uses(Reading::Statute)
                .affects(inn.late_start)
                .build()?,
            working::new_divergence("Nonresident aliens")
                .form(
                    "Neither form has separate rules for a nonresident alien.",
                    "Form 40ES instructions (2026); Form 2210AL (2025) and its instructions",
                )
                .statute(
                    format!(
                        "Some nonresident aliens have three payments: June 15, Sep 15 and Jan 15, {}. Half of the required annual payment is due in the first.",
                        y.year + 1
                    ),
                    "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(j)",
                )
                .uses(Reading::Form)
                .affects(false)
                .build()?,
        ])
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let extras = self.extras(f)?;
        self.check(f, &extras)?;
        let y = &self.y;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let mut target_qs = TargetQuestions::default();
        let mut inn = Affected::default();

        let step = if f.filing_status() == FilingStatus::MarriedFilingSeparately {
            y.prior_year_step_separate
        } else {
            y.prior_year_step
        };
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior = f.prior_year();
        let last = y.year - 1;

        // 90% of this year's tax: Form 2210AL Part I line 4.
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
                "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(d)(1)(B)(i); Form 2210AL (2025), Part I line 4",
                format!(
                    "{} of this year's Alabama tax of {} is {}",
                    y.this_year_percent,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // Last year's tax: Form 2210AL Part I line 7.
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_percent, USCents(0));
            b.unavailable(why);
            b
        };
        let prior_option = match prior {
            None => {
                target_qs.ask(
                    PositionKind::PriorYearReturn,
                    format!("Did you file a {last} Alabama return, and what was its tax?"),
                    format!(
                        "With it, last year's tax could set the target. {} of it may be lower than {} of this year's tax.",
                        y.prior_year_percent, y.this_year_percent
                    ),
                );
                unavailable(format!("Needs your {last} Alabama return"))
            }
            Some(p) if !p.filed() && p.not_required_to_file() => unavailable(format!(
                "You weren't required to file a {last} Alabama return"
            )),
            Some(p) if !p.filed() => {
                unavailable(format!("You didn't file a {last} Alabama return"))
            }
            Some(p) if p.months() != 12 => unavailable(format!(
                "Your {last} Alabama return covered {} months, not a full year",
                p.months()
            )),
            Some(p) => {
                let pf = p.figures().unwrap_or_default();
                let prior_tax = pf.tax().unwrap_or_default();
                let agi_opt = pf.state_agi();
                let agi = agi_opt.unwrap_or_default();
                let mut rate = y.prior_year_percent;
                let mut why = format!("{last} Alabama AGI of {agi} is not more than {step}");
                match agi_opt {
                    None => {
                        rate = y.high_income_percent;
                        why = format!("{last} Alabama AGI was not given");
                        target_qs.ask(
                            PositionKind::AdjustedGrossIncome,
                            format!("What was your {last} Alabama AGI?"),
                            format!(
                                "Until then, the target is {} of last year's tax. That is the rate over {}, rather than {}.",
                                y.high_income_percent,
                                step.whole(),
                                y.prior_year_percent
                            ),
                        );
                    }
                    Some(agi) if agi > step => {
                        rate = y.high_income_percent;
                        why = format!("{last} Alabama AGI of {agi} is more than {step}");
                    }
                    Some(_) => {}
                }
                if agi_opt.is_some() {
                    self.federal_agi_question(&mut a, &pf, agi, step);
                }
                let amount = rate.of(prior_tax);
                inn.agi_year =
                    self.agi_year_matters(f, rate, prior_tax, step, &this_year, tax_known);
                a.notes.rule(
                    "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(d)(1)(B)(ii), (C); Form 40ES instructions (2026), General Rule and Special Rule for Higher Income Taxpayers",
                    format!(
                        "{why}, so the prior-year test is {rate} of the {prior_tax} tax on the {last} return, or {amount}"
                    ),
                );
                estimated::new_target_option(Basis::PriorYearTax, rate, amount)
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
            let effect = if prior_opt.available() {
                "Without it, the target can only come from last year's tax.".to_string()
            } else {
                format!("Without it, there's no target, because your {last} return can't set one.")
            };
            target_qs.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Alabama tax to be?", y.year),
                effect,
            );
        }

        // § 6654(e)(2) is in the statute and not on the form. It is reported,
        // and noted when these facts meet it, but not applied.
        if prior.is_some_and(|p| !p.filed() && p.not_required_to_file() && p.months() == 12) {
            inn.no_liability = true;
            a.notes.rule(
                "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(e)(2)",
                format!(
                    "no {last} Alabama return was required; the statute excuses the penalty after a twelve-month year with no Alabama liability for a U.S. citizen or resident throughout it, \
                     but Form 2210AL has no such exception, so it is not applied here (see Divergences)"
                ),
            );
        }

        if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target needs your {} Alabama tax, because your {last} return can't set one.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} covers {} of your {last} Alabama tax, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
                    withheld.whole(),
                    prior_opt.percent(),
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {} of your {last} Alabama tax, {}. It keeps you clear of a penalty whatever your {} tax turns out to be. After {} of withholding, that's {} of estimated payments. Pay a quarter by each date.",
                    prior_opt.percent(),
                    target.whole(),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else if tax - withheld < y.threshold {
            a.exempt = true;
            let exception =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            let under = if tax < withheld {
                String::new()
            } else {
                format!(", under {}", y.threshold.whole())
            };
            rb.required(
                false,
                format!(
                    "Your Alabama tax is {}, and {} is withheld. That {}{}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld),
                    under
                ),
            )
            .exempt(exception);
            a.notes.rule(
                "Code of Ala. § 40-18-80(a)(2); 26 U.S.C. § 6654(e)(1); Form 2210AL (2025), Part I line 6",
                format!(
                    "{} of tax less {} withheld is {}, under {}, so there is no penalty",
                    tax,
                    withheld,
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Alabama withholding of {} covers the {} required for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Alabama tax of {} less {} withheld leaves {}. That is at least {}. Withholding falls short of the {} required. You need {} of estimated payments, a quarter by each date.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.threshold.whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        target_qs.ask_unless_exempt(&mut a.notes, a.exempt);

        if !a.exempt && extras.prior_year_farmer_or_fisher().is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!("Was two-thirds of your {last} gross income from farming or fishing?"),
                "If so, Form 2210AL Part IV applies; until then the figures assume not.",
            );
        }

        let cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cumulative);
        for i in 0..a.dues.len() {
            if a.dues[i] != y.dues[i] {
                a.notes.rule(
                    "Form 40ES instructions (2026), Payment of Estimated Tax",
                    format!(
                        "the installment due {} falls on a Saturday, Sunday or State holiday, so it is due {}",
                        y.dues[i].long(),
                        a.dues[i].long()
                    ),
                );
            }
        }
        a.notes.rule(
            "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(c), (d)(1)(A); Form 2210AL (2025), Part III line 1",
            format!(
                "installments of {} of {}, {} each, are due {}, {}, {} and {}",
                y.shares[0],
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
            let ap = &y.annualised_percentages;
            a.notes.rule(
                "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(d)(2); Form 2210AL (2025), Schedule AI",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: {}, {}, {} and {} of the annualised tax, \
                     less earlier installments, and never more than the regular installment plus any shortfall the method carried forward",
                    ann[0], ann[1], ann[2], ann[3], ap[0], ap[1], ap[2], ap[3]
                ),
            );
            a.installments = ann;
        }

        inn.late_start = self.late_start(f, &mut a);

        // § 6654(h) is in the statute and not on the forms. January 31, 2027
        // is a Sunday; no Alabama document says whether it moves, and since
        // the rule is not applied, nothing here depends on it.
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(y.file_and_pay_by) && !a.exempt {
            a.notes.rule(
                "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(h)",
                format!(
                    "the return was filed and paid on {}, by {}, which the statute says excuses the January installment; \
                     Alabama's forms do not, so it is still charged here (see Divergences)",
                    filed.long(),
                    y.file_and_pay_by.long()
                ),
            );
        }

        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        // Which divergences change these figures turns on the shortfalls.
        if !a.exempt {
            let (ledger, _) = self.ledger(f, &a)?;
            inn.rate = self.rate_matters(&ledger);
            inn.january = !filed.is_zero()
                && !filed.after(y.file_and_pay_by)
                && ledger.last().is_some_and(|l| l.shortfall().0 > 0);
        }
        inn.no_liability = inn.no_liability && !a.exempt;
        let divergences = self.divergences(&inn)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// Whether a shortfall runs into a quarter whose published rate is not
    /// the rate Form 2210AL prints.
    fn rate_matters(&self, ledger: &[LedgerEntry]) -> bool {
        for l in ledger {
            if l.shortfall().0 <= 0 {
                continue;
            }
            for p in self.y.rates.periods() {
                if p.rate() != self.y.form_rate && p.to().after(l.due()) {
                    return true;
                }
            }
        }
        false
    }

    /// Whether testing this year's Alabama AGI against the step, as the 2025
    /// Form 2210AL does, would give a different target. When this year's AGI
    /// is not in the facts, whether the other percentage would.
    fn agi_year_matters(
        &self,
        f: &Facts,
        rate: Rate,
        prior_tax: USCents,
        step: USCents,
        this_year: &TargetOption,
        tax_known: bool,
    ) -> bool {
        let mut other = self.y.prior_year_percent;
        if rate == other {
            other = self.y.high_income_percent;
        }
        if let Some(agi) = f.this_year().state_agi() {
            other = self.y.prior_year_percent;
            if agi > step {
                other = self.y.high_income_percent;
            }
        }
        if other == rate {
            return false;
        }
        let lesser = |a: USCents| {
            if tax_known && this_year.amount() < a {
                this_year.amount()
            } else {
                a
            }
        };
        lesser(rate.of(prior_tax)) != lesser(other.of(prior_tax))
    }

    /// Reports that § 6654(d)(1)(C), read into Alabama law, does not say
    /// whose adjusted gross income it tests, when the answer could change
    /// the percentage: federal AGI was not given, or lies on the other side
    /// of the step from Alabama AGI.
    fn federal_agi_question(&self, a: &mut Assessment, pf: &Figures, agi: USCents, step: USCents) {
        let last = self.y.year - 1;
        match pf.federal_agi() {
            None => {
                a.notes.ask(
                    PositionKind::AdjustedGrossIncome,
                    format!("What was your {last} federal AGI?"),
                    format!(
                        "Alabama law doesn't say if the {} test reads federal or Alabama AGI. Until then, the figures use the Alabama AGI the {} Form 40ES reads.",
                        step.whole(),
                        self.y.year
                    ),
                );
            }
            Some(fed) if (fed > step) != (agi > step) => {
                let other = if fed > step {
                    self.y.high_income_percent
                } else {
                    self.y.prior_year_percent
                };
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "With your {last} federal AGI of {}, the {} test would give {other}. The law doesn't say which AGI; the {} Form 40ES and the figures use Alabama's.",
                        fed.whole(),
                        step.whole(),
                        self.y.year
                    ),
                );
            }
            Some(_) => {}
        }
    }

    /// Notes what the Form 40ES tells a filer whose requirement first arose
    /// after April 1, when the caller said so, and reports whether it did.
    /// The figure does not use it: Form 2210AL and § 6654(d)(1)(A) charge
    /// 25% by every date.
    fn late_start(&self, f: &Facts, a: &mut Assessment) -> bool {
        let met = f.requirement_first_met();
        if met.is_zero() || a.exempt {
            return false;
        }
        let mut first = 0;
        for (i, g) in self.y.late_groups.iter().enumerate() {
            if !met.before(*g) {
                first = i + 1;
            }
        }
        if first == 0 {
            return false;
        }
        let remaining = a.dues.len() - first;
        let share = if remaining > 1 {
            format!("1/{remaining}")
        } else {
            "all".to_string()
        };
        a.notes.rule(
            "Form 40ES instructions (2026), Changes In Tax; Form 2210AL (2025), Part III line 1",
            format!(
                "the requirement first arose on {}; the 40ES would have you pay {share} of the estimate by {}, but Form 2210AL charges {} by every date, so the earlier installments are charged here (see Divergences)",
                met.long(),
                a.dues[first].long(),
                self.y.shares[0]
            ),
        );
        true
    }

    /// Applies the credits to the installments.
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
                "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(g)(1); Form 2210AL (2025), Part II box D",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(g)(1); Form 2210AL (2025), Part II box D",
                format!(
                    "the {} withheld is treated as paid in equal parts on the due dates, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        for p in f.payments() {
            if p.from_prior_year() {
                notes.rule(
                    "Form 40ES instructions (2026), item 4",
                    format!(
                        "last year's overpayment of {} applied forward counts on {}; the 40ES lets it go to the earliest installment, which is never later than dividing it among them",
                        p.amount(),
                        a.dues[0].long()
                    ),
                );
            }
        }
        notes.rule(
            "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(b)(3); Form 2210AL (2025), Part III lines 3 to 9",
            "a payment after a due date pays the oldest unpaid installment first, whichever date it was meant for",
        );
        Ok((ledger, notes))
    }

    /// The schedule the ledger gives.
    fn installments(
        &self,
        a: &Assessment,
        ledger: &[LedgerEntry],
    ) -> Result<Vec<Installment>, Error> {
        let mut out = Vec::with_capacity(ledger.len());
        let mut needed = USCents(0);
        for (i, l) in ledger.iter().enumerate() {
            needed += a.installments[i];
            out.push(
                estimated::new_installment(i as i64 + 1, l.due())
                    .needed_by(needed)
                    .credited_by(l.credited_by())
                    .shortfall(l.shortfall())
                    .build()?,
            );
        }
        Ok(out)
    }
}

impl EstimatedTax for AlabamaEstimated {
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
                    "with no {} Alabama return to measure against, there is no target to schedule",
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
                "alabamataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Alabama return to measure against, there is no target to figure a penalty from",
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
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let schedule = self.installments(&a, &ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let (mut p, unrated) =
            estimated::simple_penalty(&ledger, stop, &y.rates, waiver, &a.requirement, &schedule)?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(d)(1)(B)",
                format!(
                    "this year's Alabama tax was not given, so the penalty is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and {} of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    y.this_year_percent,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "Code of Ala. § 40-18-80(a); 26 U.S.C. § 6654(b)(2)",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "Code of Ala. §§ 40-18-80(a), 40-1-44(a); 26 U.S.C. § 6654(a), (b)(2); Form 2210AL (2025), Part III lines 10 to 12; ADOR, Quarterly Interest Rates",
            format!(
                "each shortfall is charged the department's rate for each quarter, divided by 365, for each day from its due date until paid or {}; \
                 Form 2210AL asks for whole dollars, and these figures are to the cent; the penalty to {} is {}",
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
        if p.total().0 > 0 && waiver_answered.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Will you ask the department to waive the penalty?",
                "It can waive it for a casualty, disaster or other unusual event. Retiring after 62 or becoming disabled also counts, with reasonable cause.",
            );
        }
        if let Some(o) = estimated::standard_waiver(f, "the Alabama Department of Revenue", 62)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        alabama_explanation()
    }
}

/// "Aug 31" inside the tax year and "Jan 15, 2027" outside it.
fn short_date(d: Date, year: i64) -> String {
    let month = d.month().to_string();
    let m = &month[..3];
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

/// Written by hand and checked against research/estimated-payments/al.md.
fn alabama_explanation() -> Explanation {
    estimated::new_explanation(
        "Alabama uses the federal estimated tax rules, with a $500 threshold, not $1,000. The penalty is interest on each quarter's shortfall.")
        .paragraph("Withholding and estimated payments avoid the penalty if they reach a target. The target is the smaller of 90% of this year's tax or 100% of last year's. The second is 110% if last year's Alabama AGI was over $150,000. The AGI limit is $75,000 for married filing separately. Last year's figure is open only if you filed a return for a full twelve months. There is no penalty if this year's tax less withholding is under $500.")
        .paragraph("Pay a quarter each by April 15, June 15, September 15 and the next January 15. A date on a Saturday, Sunday or State holiday shifts to the next business day. Withholding counts as paid evenly on the four dates. You can instead show when it was actually withheld. The department sends no record of your payments, so keep your own.")
        .paragraph("The penalty is interest on each shortfall for the days it stays unpaid. The department's rate follows the federal underpayment rate each quarter. Interest stops on April 15 of the next year. A payment goes first to the oldest shortfall. The 2025 penalty form prints 7% for the whole year. For 2026 the rate is 6% from April to June and 7% after. If income came unevenly, Schedule AI's annualised method can cut early payments. The department can waive the penalty for a casualty or disaster. It can also do so for retirement after 62 or disability, with reasonable cause.")
        .paragraph("By adopting the federal rules, the statute grants two things the forms omit. The January payment has no penalty if you file and pay in full by January 31. There is no penalty after a full year with no Alabama tax liability. The department's forms apply neither. If you first need to pay after April 1, Form 40ES says a third by June 15. After June 1 it says half by September 15. The penalty form still charges a quarter by every date.")
        .versus_federal("Alabama's threshold is $500 rather than $1,000, the federal figure. The 110% test reads last year's Alabama AGI. Alabama's State holidays move a due date. The forms charge the January payment even if you file and pay by January 31. They also charge someone who had no tax last year. The statute excuses both.")
        .source("Code of Ala. §§ 40-18-80, 40-1-44 and 1-3-8; 26 U.S.C. § 6654")
        .source("Form 40ES and its instructions, 2026")
        .source("Form 2210AL and its instructions, 2025")
        .source("Alabama Department of Revenue, Quarterly Interest Rates")
        .build()
        .unwrap_or_default()
}

/// A question whose answer can change the target. It is asked only when no
/// exception sets the target.
struct TargetQuestion {
    position: PositionKind,
    question: String,
    effect: String,
}

#[derive(Default)]
struct TargetQuestions(Vec<TargetQuestion>);

impl TargetQuestions {
    fn ask(&mut self, position: PositionKind, question: String, effect: String) {
        self.0.push(TargetQuestion {
            position,
            question,
            effect,
        });
    }

    /// Adds the questions to `notes` unless an exception set the target.
    fn ask_unless_exempt(self, notes: &mut Notes, exempt: bool) {
        if exempt {
            return;
        }
        for t in self.0 {
            notes.ask(t.position, t.question, t.effect);
        }
    }
}
