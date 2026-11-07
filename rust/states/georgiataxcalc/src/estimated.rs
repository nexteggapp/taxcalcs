//! Georgia's estimated tax rules for individuals, O.C.G.A. §§ 48-7-114 to
//! 48-7-120, as the shared [`EstimatedTax`] trait.
//!
//! Georgia differs from the federal rules in shape. Whether payments are
//! required turns on gross income, not on tax. The current-year test is 70%
//! of this year's tax, and the prior-year test is 100% of last year's at
//! every income. Each payment goes first to the installment of its own
//! period, and only what is left over pays an earlier shortfall. Three
//! exceptions excuse an installment when the payments to its date reach
//! them. The penalty is a fixed 9% a year, by the day.
//!
//! The statute is published officially only through LexisNexis, and could
//! not be read (research/estimated-payments/ga.md). Every rule here comes
//! from the Department of Revenue's Form 500 UET (Rev. 07/15/25) and Form
//! 500-ES (Rev. 05/12/25), and the 2026 figures from HB 463 (2026). With no
//! statute text there is no form-against-statute divergence to report except
//! where an act postdates the form.

use std::any::Any;
use std::collections::BTreeMap;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Figures, Installment,
    Jurisdiction, Payment, Penalty, RateSeries, Requirement, WaiverCause, WaiverKind,
    WaiverOutcome,
};
use filing::Status;
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

/// Georgia's postal code.
const JURISDICTION: Jurisdiction = estimated::GEORGIA;

/// The facts Georgia's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
///
/// They are for the test of whether estimated tax is required at all, which
/// compares gross income ([`Figures::gross_income`]) with the dependent
/// exemptions, the estimated deductions and $1,000.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    dependents: Option<i64>,
    deductions: Option<USCents>,
}

impl EstimatedFacts {
    /// The number of dependents the filer will claim an exemption for on
    /// Form 500, or `None` when the caller did not say.
    pub fn dependents(&self) -> Option<i64> {
        self.dependents
    }

    /// The filer's estimated deductions for the year, the standard deduction
    /// or the itemised total, or `None` when the caller did not say.
    pub fn deductions(&self) -> Option<USCents> {
        self.deductions
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
    /// Sets the number of dependents.
    pub fn dependents(&mut self, n: i64) -> &mut Self {
        self.e.dependents = Some(n);
        self
    }

    /// Sets the estimated deductions.
    pub fn deductions(&mut self, a: USCents) -> &mut Self {
        self.e.deductions = Some(a);
        self
    }

    /// Refuses a negative count of dependents or negative deductions.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        if self.e.dependents.unwrap_or(0) < 0 || self.e.deductions.unwrap_or_default().0 < 0 {
            return Err(Error::Invalid(
                "georgiataxcalc: dependents and deductions cannot be negative".into(),
            ));
        }
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// The income tax rate, O.C.G.A. § 48-7-20(a.1), which Exceptions 1 and
    /// 3 apply to income.
    pub(crate) rate: Rate,

    /// Form 500 UET line 5.
    pub(crate) this_year_share: Rate,
    /// Form 500 UET line 4.
    pub(crate) prior_year_share: Rate,

    /// The $1,000 of income not subject to withholding in the test of
    /// whether estimated tax is required.
    pub(crate) unwithheld: USCents,

    /// The deduction for each dependent, O.C.G.A. § 48-7-26.
    pub(crate) exemption: USCents,
    /// The standard deduction by filing status, O.C.G.A. § 48-7-26.
    pub(crate) standard: BTreeMap<Status, USCents>,

    /// The 2025 dependent exemption Form 500-ES still prints, for the
    /// divergence.
    pub(crate) form_exemption: USCents,
    /// The 2025 standard deductions Form 500-ES still prints.
    pub(crate) form_standard: BTreeMap<Status, USCents>,

    pub(crate) dues: Vec<Date>,

    /// The first days of Form 500 UET's later rows: a requirement first met
    /// on or after `later_groups[i]` starts at installment i+2.
    pub(crate) later_groups: Vec<Date>,

    pub(crate) calendar: Calendar,
    pub(crate) file_and_pay_by: Date,
    pub(crate) stop: Date,
    pub(crate) period_ends: Vec<Date>,

    /// Exception 2: 70% of the tax on annualised income.
    pub(crate) annualised_share: Rate,
    /// Exception 3: 90% of the tax on the income to date.
    pub(crate) actual_share: Rate,

    /// The 9% a year Form 500 UET line 20 charges.
    pub(crate) rates: RateSeries,
}

impl EstimatedYear {
    fn standard_for(&self, s: Status) -> Option<USCents> {
        self.standard.get(&s).copied()
    }

    fn form_standard_for(&self, s: Status) -> Option<USCents> {
        self.form_standard.get(&s).copied()
    }
}

/// Form 500 UET line 9: withholding is treated as paid equally each
/// quarter. They are also the cumulative shares used for Exceptions 1 and 2,
/// which the form does not state beyond the first quarter.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// Returns Georgia's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(GeorgiaEstimated { y }))
}

struct GeorgiaEstimated {
    y: EstimatedYear,
}

#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// Each installment's own amount: the target divided by the number of
    /// installments, from the first one the requirement applies to.
    required: Vec<USCents>,
    first: i64,

    /// The cumulative amounts that excuse an installment under Exception 1,
    /// or `None` where the exception is not open.
    exception1: Option<Vec<USCents>>,
    /// Likewise for Exception 2.
    exception2: Option<Vec<USCents>>,
    /// Likewise for Exception 3, which never applies to the fourth.
    exception3: Vec<USCents>,

    /// Form 500 UET Instruction F: the balance paid with a return filed by
    /// January 31 counts as paid on January 15.
    paid_by_january_31: bool,

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

impl GeorgiaEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "georgiataxcalc: the facts carry {}, not georgiataxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "georgiataxcalc: these are Georgia's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "georgiataxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "farmers and fishermen file by March 1 or are charged on the last quarter alone under Form 500 UET Instruction G, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let Some(end) = self.y.period_ends.get(i) else {
                    continue;
                };
                if p.end() != *end {
                    return Err(Error::Invalid(format!(
                        "georgiataxcalc: Form 500 UET period {} ends on {}, not {}",
                        i + 1,
                        end,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where a Department of Revenue form and the law part ways. The statute
    /// could not be read, so the only one found is a form printed before the
    /// act that changed its figures (research/estimated-payments/ga.md). The
    /// figures follow the act. `affects` is whether the form's figures would
    /// decide the requirement differently.
    fn divergences(&self, affects: bool) -> Result<Vec<Divergence>, Error> {
        let y = &self.y;
        let w = |a: Option<USCents>| a.unwrap_or_default().whole();
        let d = working::new_divergence(
            "The deductions in the test of whether estimated tax is required",
        )
        .form(
            format!(
                "The voucher form listed for {} prints the {} standard deduction of {}. It shows {} for a joint return and a dependent exemption of {}. It predates HB 463 and is out of date for {}.",
                y.year,
                y.year - 1,
                w(y.form_standard_for(Status::Single)),
                w(y.form_standard_for(Status::MarriedFilingJointly)),
                y.form_exemption.whole(),
                y.year
            ),
            "Form 500-ES (Rev. 05/12/25), Standard Deduction and Exemption Amount for Tax Year 2025",
        )
        .statute(
            format!(
                "From tax year {}, the standard deduction is {}. It is {} for a joint return, and the dependent exemption is {}. The act was signed on May 11, 2026.",
                y.year,
                w(y.standard_for(Status::Single)),
                w(y.standard_for(Status::MarriedFilingJointly)),
                y.exemption.whole()
            ),
            "HB 463 (2026), §§ 2-2, 2-3 and 5-1, amending O.C.G.A. § 48-7-26; Governor's press release of May 11, 2026",
        )
        .uses(Reading::Statute)
        .affects(affects)
        .build()?;
        Ok(vec![d])
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
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
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();

        // Form 500 UET line 5: 70% of this year's tax after credits.
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
                "Form 500 UET (Rev. 07/15/25), lines 3 and 5",
                format!(
                    "{} of this year's Georgia tax after credits of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // Form 500 UET line 4: 100% of last year's tax, from a 12-month
        // return.
        let unavailable = |why: String| {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        let mut prior_figures = Figures::default();
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Georgia return"))
        } else if !prior.filed() {
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "The form doesn't say if the {} option is open with no {last} Georgia return. It asks only for a 12-month return, and the figures treat the option as closed.",
                    y.prior_year_share
                ),
            );
            unavailable(format!("You didn't file a {last} Georgia return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} return covered {} months, not 12",
                prior.months()
            ))
        } else {
            prior_figures = prior.figures().unwrap_or_default();
            let prior_tax = prior_figures.tax().unwrap_or_default();
            let amount = y.prior_year_share.of(prior_tax);
            a.notes.rule(
                "Form 500 UET (Rev. 07/15/25), line 4",
                format!(
                    "{} of the {} tax on the {last} return is {}; Georgia has no higher percentage at higher incomes",
                    y.prior_year_share, prior_tax, amount
                ),
            );
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "The form doesn't say if the {} option uses {last} tax before or after credits. The figures use the tax your return shows.",
                    y.prior_year_share
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
        // Whether estimated tax is required at all: gross income over the
        // dependent exemptions, the estimated deductions and $1,000.
        let gross_opt = f.this_year().gross_income();
        let gross_known = gross_opt.is_some();
        let gross = gross_opt.unwrap_or_default();
        let (threshold, form_threshold) = self.threshold(f, &extras, &mut a.notes);
        let not_required = gross_known && gross <= threshold;
        let divergences =
            self.divergences(gross_known && not_required != (gross <= form_threshold))?;
        a.notes.diverges(&divergences);

        if not_required {
            a.exempt = true;
            let under = estimated::exception_at_most("Gross income", gross, threshold)?;
            rb.required(
                false,
                format!(
                    "Your expected gross income of {} is no more than {}. That is your exemptions, deductions and {} together. So Georgia doesn't require estimated tax for {}.",
                    gross.whole(),
                    threshold.whole(),
                    y.unwithheld.whole(),
                    y.year
                ),
            )
            .exempt(under);
            a.notes.rule(
                "Form 500-ES (Rev. 05/12/25), Who Must File Estimated Tax; O.C.G.A. § 48-7-114",
                format!(
                    "gross income of {gross} does not exceed {threshold}, so no estimated tax is required"
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "With no {last} Georgia return, there is no target until your {} tax is figured.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Withholding of {} already reaches {} of your {last} Georgia tax, {}. So no estimated payments are needed, whatever your {} tax turns out to be.",
                    withheld.whole(),
                    y.prior_year_share,
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {} of your {last} Georgia tax, {}. It keeps you clear of a penalty whatever your {} tax turns out to be. After {} withheld, you pay {} in equal estimated installments.",
                    y.prior_year_share,
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
                    "Your {} of Georgia withholding reaches the {} needed for the year. So no estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            let of = if prior_opt.available() {
                format!(
                    "the smaller of {} of this year's Georgia tax and {} of last year's",
                    y.this_year_share, y.prior_year_share
                )
            } else {
                format!("{} of this year's Georgia tax", y.this_year_share)
            };
            rb.required(
                true,
                format!(
                    "Your target for the year is {}. It is {}. After {} withheld, you pay {} in equal estimated installments.",
                    target.whole(),
                    of,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        // These answers change the target. An exception sets the target, so
        // they are asked only when none applies.
        if !a.exempt && !has_prior {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Georgia return, and what was its tax?"),
                format!(
                    "It could make {} of last year's tax your target, if under {} of this year's.",
                    y.prior_year_share, y.this_year_share
                ),
            );
        }
        if !a.exempt && !tax_known {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Georgia tax to be?", y.year),
                if a.requirement.target_known() {
                    "Until then, your target can only come from last year's tax."
                } else {
                    "Until then, there's no target."
                },
            );
        }
        if a.exempt {
            a.required = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }
        if !gross_known {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!("What do you expect your {} gross income to be?", y.year),
                format!(
                    "Estimated tax starts above {}, your exemptions, deductions and {}.",
                    threshold.whole(),
                    y.unwithheld.whole()
                ),
            );
        }

        // The installments: the target in equal parts from the first
        // installment the requirement applies to.
        a.first = self.late_start(f, &mut a);
        let n = a.dues.len() as i64 - a.first;
        let cum = estimated::equal_shares(a.requirement.target(), a.dues.len() as i64, a.first)?;
        a.required = estimated::increments(&cum);
        a.notes.rule(
            "Form 500 UET (Rev. 07/15/25), lines 6 to 8 and Instruction B; Form 500-ES (Rev. 05/12/25)",
            format!(
                "the {} target is due in {} equal installments from {}",
                a.requirement.target(),
                n,
                a.dues[a.first as usize].long()
            ),
        );

        // The exceptions, Form 500 UET lines 13 to 16 and Instruction D.
        self.exceptions(f, &mut a, &prior_figures, prior_opt.available())?;

        // Instruction F: the balance paid with a return filed by January 31
        // counts as paid on January 15. January 31, 2027 is a Sunday; the
        // form says nothing of moving it, so it is not moved.
        let filed = f.return_filed();
        if !filed.is_zero() {
            if !filed.after(y.file_and_pay_by) {
                a.paid_by_january_31 = true;
                a.notes.rule(
                    "Form 500 UET (Rev. 07/15/25), Instruction F",
                    format!(
                        "the return was filed and paid on {}, by {}, so the balance paid with it counts as paid on {}",
                        filed.long(),
                        y.file_and_pay_by.long(),
                        a.dues[3].long()
                    ),
                );
            } else if filed == y.calendar.next_business_day(y.file_and_pay_by) {
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "The form doesn't say if {}, a {}, moves to the next workday. The figures keep it: a balance paid on {} isn't paid by {}.",
                        short(y.file_and_pay_by, y.year),
                        y.file_and_pay_by.weekday(),
                        short(filed, y.year),
                        short(a.dues[3], y.year)
                    ),
                );
            }
        }
        Ok(a)
    }

    /// The dependent exemptions, the estimated deductions and $1,000: what
    /// gross income must exceed before estimated tax is required, and the
    /// same total with the 2025 figures Form 500-ES prints.
    ///
    /// The working goes in `n` whether or not the household is exempt,
    /// because an exemption can rest on a default it reports.
    fn threshold(&self, f: &Facts, e: &EstimatedFacts, n: &mut Notes) -> (USCents, USCents) {
        let y = &self.y;
        let (deductions, form_deductions) = match e.deductions() {
            Some(d) => (d, d),
            None => {
                let listed = y.standard_for(f.filing_status());
                let form_std = y
                    .form_standard_for(f.filing_status())
                    .or_else(|| y.form_standard_for(Status::Single))
                    .unwrap_or_default();
                let std = match listed {
                    None => {
                        let std = y.standard_for(Status::Single).unwrap_or_default();
                        n.ask(
                            PositionKind::JurisdictionFact,
                            format!("How much do you expect to deduct for {}?", y.year),
                            format!(
                                "Georgia's law sets no standard deduction for your filing status. Until then, the test for estimated payments uses the {} single amount.",
                                std.whole()
                            ),
                        );
                        std
                    }
                    Some(std) => {
                        n.ask(
                            PositionKind::JurisdictionFact,
                            format!("How much do you expect to deduct for {}?", y.year),
                            format!(
                                "Until then, the estimated tax test uses the {} standard deduction.",
                                std.whole()
                            ),
                        );
                        std
                    }
                };
                (std, form_std)
            }
        };
        let dependents = e.dependents();
        if dependents.is_none() {
            n.ask(
                PositionKind::JurisdictionFact,
                format!("How many dependents will you claim for {}?", y.year),
                format!(
                    "Each adds {} to the income you can have before estimated tax is required.",
                    y.exemption.whole()
                ),
            );
        }
        let dependents = dependents.unwrap_or(0);
        let exemptions = dependents * y.exemption;
        let total = deductions + exemptions + y.unwithheld;
        let form = form_deductions + dependents * y.form_exemption + y.unwithheld;
        n.rule(
            "Form 500-ES (Rev. 05/12/25), Who Must File Estimated Tax; HB 463 (2026), §§ 2-2, 2-3",
            format!(
                "estimated tax is required when gross income exceeds {} of deductions, {} of exemptions for {} dependents and {}, together {}",
                deductions, exemptions, dependents, y.unwithheld, total
            ),
        );
        (total, form)
    }

    /// The installment the requirement first applies to, from Form 500 UET
    /// Instruction B: a requirement first met from April 1 starts in June,
    /// from June 1 in September, and from September 1 in January.
    fn late_start(&self, f: &Facts, a: &mut Assessment) -> i64 {
        let first_met = f.requirement_first_met();
        if first_met.is_zero() {
            a.notes.ask(
                PositionKind::RequirementFirstMet,
                format!(
                    "When did you first expect to need estimated payments for {}?",
                    self.y.year
                ),
                format!(
                    "Until then, the figures assume you could expect to pay from Jan 1. Starting on or after {} means fewer, larger installments.",
                    short(self.y.later_groups[0], self.y.year)
                ),
            );
            return 0;
        }
        let first = estimated::late_start(first_met, &self.y.later_groups);
        if first > 0 {
            a.notes.rule(
                "Form 500 UET (Rev. 07/15/25), Instruction B",
                format!(
                    "the requirement was first met on {}, so {} installments are due, the first on {}",
                    first_met.long(),
                    self.y.dues.len() as i64 - first,
                    self.y.dues[first as usize].long()
                ),
            );
        }
        first
    }

    /// Fills in the amounts that excuse an installment.
    fn exceptions(
        &self,
        f: &Facts,
        a: &mut Assessment,
        pf: &Figures,
        prior_usable: bool,
    ) -> Result<(), Error> {
        let y = &self.y;
        let last = y.year - 1;

        // Exception 1: last year's income at this year's rate.
        if prior_usable {
            if let Some(income) = pf.taxable_income() {
                let ex = y.rate.of(income);
                let cum = estimated::cumulative_shares(ex, &quarters())?;
                a.notes.rule(
                    "Form 500 UET (Rev. 07/15/25), line 14 and Instruction D, Exception 1",
                    format!(
                        "the {last} taxable income of {income} at the {} rate of {} is {ex}; an installment is excused when the payments to its date reach {}, {}, {} or {}",
                        y.year, y.rate, cum[0], cum[1], cum[2], cum[3]
                    ),
                );
                a.exception1 = Some(cum);
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "The form's Exception 1 uses your {last} income at current rates and exemptions. It omits each installment's share; the figures use a quarter for each so far."
                    ),
                );
            } else {
                a.notes.ask(
                    PositionKind::PriorYearReturn,
                    format!("What was your {last} Georgia taxable income?"),
                    "With it, Exception 1 taxes it at this year's rate and can excuse an installment.",
                );
            }
        }

        if !f.elections().annualised() {
            return Ok(());
        }
        let periods = f.periods();
        let qs = quarters();
        // Exception 2: 70% of the tax on annualised income, by a quarter for
        // each installment so far. Exception 3: 90% of the tax on the income
        // to date, for the first three.
        let mut ex2 = vec![USCents(0); periods.len()];
        let mut ex3 = vec![USCents(0); periods.len().saturating_sub(1)];
        let mut running = Rate(0);
        for (i, p) in periods.iter().enumerate() {
            running = Rate(running.0 + qs[i].0);
            ex2[i] = running.of(y.annualised_share.of(p.annualised_tax()));
            if i < ex3.len() {
                ex3[i] = y.actual_share.of(y.rate.of(p.income()));
            }
        }
        a.notes.rule(
            "Form 500 UET (Rev. 07/15/25), line 15 and Instruction D, Exception 2",
            format!(
                "an installment is excused when the payments to its date reach {} of the tax on the annualised income, by a quarter for each installment so far: {}, {}, {} and {}",
                y.annualised_share, ex2[0], ex2[1], ex2[2], ex2[3]
            ),
        );
        a.notes.rule(
            "Form 500 UET (Rev. 07/15/25), line 16 and Instruction D, Exception 3",
            format!(
                "the first three installments are also excused when the payments to their dates reach {} of the tax at {} on the taxable income to the period's end: {}, {} and {}; \
                 the exception does not apply to the fourth",
                y.actual_share, y.rate, ex3[0], ex3[1], ex3[2]
            ),
        );
        a.notes.ask(
            PositionKind::UnsettledRule,
            "",
            "The form doesn't say how much of Exception 2's 70% each later installment needs. The figures follow its example's 25% for the first, then 50%, 75% and 100%.",
        );
        a.exception2 = Some(ex2);
        a.exception3 = ex3;
        Ok(())
    }

    fn ledger_of(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Entry>, Notes), Error> {
        let mut credits = estimated::credits(f, &a.dues, &quarters())?;
        let mut notes = Notes::default();
        if a.paid_by_january_31 {
            // The balance paid with the return counts as paid on January 15:
            // enough to bring what was credited by then to what was required.
            let need: USCents = a.required.iter().copied().sum();
            let by: USCents = credits
                .iter()
                .filter(|c| !c.on().after(a.dues[3]))
                .map(|c| c.amount())
                .sum();
            if need > by {
                let deemed = estimated::new_payment()
                    .on(a.dues[3])
                    .amount(need - by)
                    .build()?;
                credits.push(deemed);
                notes.rule(
                    "Form 500 UET (Rev. 07/15/25), Instruction F",
                    format!(
                        "{} of the balance paid with the return is counted as paid on {}",
                        need - by,
                        a.dues[3].long()
                    ),
                );
            }
        }
        notes.rule(
            "Form 500 UET (Rev. 07/15/25), lines 9 to 13",
            format!(
                "the {} withheld is treated as paid equally each quarter, whenever it was actually withheld; \
                 a payment counts first toward the installment of its own period, and only what is left over pays an earlier shortfall",
                f.withheld_total()
            ),
        );
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "Form 500 UET (Rev. 07/15/25), line 9",
                "Georgia has no election to credit withholding on the dates it was withheld, so it is spread equally",
            );
        }
        Ok((ledger(&a.dues, &a.required, &credits), notes))
    }
}

/// Part of an installment's shortfall and the day it was paid, or the zero
/// Date if it never was.
#[derive(Clone, Copy, Debug)]
struct Piece {
    amount: USCents,
    paid_on: Date,
}

/// One installment after the credits have been applied.
#[derive(Clone, Debug)]
struct Entry {
    due: Date,
    credited_by: USCents,
    shortfall: USCents,
    required: USCents,
    pieces: Vec<Piece>,
}

impl Entry {
    fn owed(&self) -> USCents {
        self.shortfall - self.pieces.iter().map(|p| p.amount).sum::<USCents>()
    }
}

/// Applies credits as Form 500 UET lines 9 to 12 do. A credit counts first
/// toward the installment of its own period, the days after the last due
/// date up to and including this one. What is left of it pays the oldest
/// earlier shortfall still outstanding, on the credit's date, and then
/// carries forward as line 10's overpayment. Credits after the last due date
/// pay what is outstanding.
///
/// This differs from [`estimated::apply`], which pays the oldest shortfall
/// first whichever period a credit falls in.
fn ledger(dues: &[Date], required: &[USCents], credits: &[Payment]) -> Vec<Entry> {
    let mut cs = credits.to_vec();
    cs.sort_by(|a, b| {
        if a.on().before(b.on()) {
            std::cmp::Ordering::Less
        } else if b.on().before(a.on()) {
            std::cmp::Ordering::Greater
        } else {
            std::cmp::Ordering::Equal
        }
    });
    let mut out: Vec<Entry> = dues
        .iter()
        .zip(required)
        .map(|(&due, &required)| Entry {
            due,
            credited_by: USCents(0),
            shortfall: USCents(0),
            required,
            pieces: Vec::new(),
        })
        .collect();
    let pay_older = |out: &mut [Entry], before: usize, mut amount: USCents, on: Date| {
        for e in out.iter_mut().take(before) {
            if amount.0 <= 0 {
                break;
            }
            let o = e.owed();
            if o.0 > 0 {
                let p = o.min(amount);
                e.pieces.push(Piece {
                    amount: p,
                    paid_on: on,
                });
                amount -= p;
            }
        }
        amount
    };
    let (mut carry, mut credited) = (USCents(0), USCents(0));
    let mut next = 0;
    for (i, &due) in dues.iter().enumerate() {
        let mut covered = carry.min(required[i]);
        carry -= covered;
        while next < cs.len() && !cs[next].on().after(due) {
            let c = cs[next];
            next += 1;
            credited += c.amount();
            let take = c.amount().min(required[i] - covered);
            covered += take;
            carry += pay_older(&mut out, i, c.amount() - take, c.on());
        }
        out[i].credited_by = credited;
        out[i].shortfall = required[i] - covered;
    }
    for c in &cs[next..] {
        pay_older(&mut out, dues.len(), c.amount(), c.on());
    }
    for e in &mut out {
        let o = e.owed();
        if o.0 > 0 {
            e.pieces.push(Piece {
                amount: o,
                paid_on: Date::default(),
            });
        }
    }
    out
}

/// Where an installment stands once the exceptions are applied: the
/// exception that covers it, if any, and whether its own shortfall bears a
/// penalty.
#[derive(Clone, Debug, Default)]
struct Standing {
    why: &'static str,
    charged: bool,
}

impl Assessment {
    fn standings(&self, l: &[Entry]) -> Vec<Standing> {
        l.iter()
            .enumerate()
            .map(|(i, e)| {
                let mut s = Standing::default();
                if self
                    .exception1
                    .as_ref()
                    .is_some_and(|x| e.credited_by >= x[i])
                {
                    s.why =
                        "Exception 1: the payments reached last year's income at this year's rate";
                } else if self
                    .exception2
                    .as_ref()
                    .is_some_and(|x| e.credited_by >= x[i])
                {
                    s.why =
                        "Exception 2: the payments reached 70% of the tax on the annualised income";
                } else if i < self.exception3.len() && e.credited_by >= self.exception3[i] {
                    s.why =
                        "Exception 3: the payments reached 90% of the tax on the income to date";
                }
                s.charged = e.shortfall.0 > 0 && s.why.is_empty();
                s
            })
            .collect()
    }

    /// The installments, with the status [`estimated::status_of`] their
    /// running totals, excused where an exception covers them.
    fn schedule_of(&self, l: &[Entry]) -> Result<Vec<Installment>, Error> {
        let mut out = Vec::with_capacity(l.len());
        let mut needed = USCents(0);
        for (i, s) in self.standings(l).iter().enumerate() {
            needed += l[i].required;
            let mut b = estimated::new_installment(i as i64 + 1, l[i].due);
            b.needed_by(needed)
                .credited_by(l[i].credited_by)
                .shortfall(l[i].shortfall);
            if !s.why.is_empty() {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok(out)
    }
}

impl EstimatedTax for GeorgiaEstimated {
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
                    "with no {} Georgia return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (l, mut notes) = self.ledger_of(f, &a)?;
        let out = a.schedule_of(&l)?;
        for (i, s) in a.standings(&l).iter().enumerate() {
            if !s.why.is_empty() && l[i].shortfall.0 > 0 {
                notes.rule(
                    "Form 500 UET (Rev. 07/15/25), Instruction D",
                    format!(
                        "the {} installment was {} short and bears no penalty under {}",
                        l[i].due.long(),
                        l[i].shortfall,
                        s.why
                    ),
                );
            }
        }
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "georgiataxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Georgia return to measure against, there is no target to figure a penalty from",
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
        let (l, mut notes) = self.ledger_of(f, &a)?;
        let schedule = a.schedule_of(&l)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut later_excess = false;
        for (i, s) in a.standings(&l).iter().enumerate() {
            let e = &l[i];
            if !s.charged {
                continue;
            }
            let mut lb = estimated::new_penalty_line(i as i64 + 1, e.due, e.shortfall);
            let mut last_day = e.due;
            for p in &e.pieces {
                let mut to = stop;
                if !p.paid_on.is_zero() {
                    later_excess = true;
                    if p.paid_on.before(stop) {
                        to = p.paid_on;
                    }
                }
                if !to.after(e.due) {
                    continue;
                }
                if to.after(last_day) {
                    last_day = to;
                }
                let (parts, _) = estimated::simple_daily(p.amount, e.due, to, &y.rates)?;
                for part in parts {
                    lb.part(part);
                }
            }
            let line = lb.days(last_day.days_since(e.due)).build()?;
            pb.line(line);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            notes.rule(
                "Form 500 UET (Rev. 07/15/25), lines 4 to 8",
                format!(
                    "this year's tax was not given, so the penalty is figured against 100% of the {} tax alone; \
                     the required payment is the lesser of that and 70% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "Form 500 UET (Rev. 07/15/25), line 18",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        if later_excess {
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                "The form doesn't say which payment pays off an earlier shortfall. The figures pay it when a later period first pays more than its own installment.",
            );
        }
        notes.rule(
            "Form 500 UET (Rev. 07/15/25), lines 17 to 21; IT-511 (2025)",
            format!(
                "each underpayment no exception covers is charged 9% a year, by the day, from its due date to the date paid or {}, whichever is earlier; \
                 the penalty to {} is {}",
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if waiver_opt.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "Form 500 UET (Rev. 07/15/25)",
                "Georgia's forms have no waiver for a casualty, retirement or disability; the Department grants disaster relief by announcement, so the penalty shown is before any such relief",
            );
            if p.total().0 > 0 {
                let o = georgia_waiver(waiver, f.positions().waiver_cause());
                if o.undecided() {
                    notes.ask(
                        PositionKind::UnsettledRule,
                        "",
                        "Georgia postpones due dates for a disaster only if the Department announces it.",
                    );
                }
                p = p.to_builder().waiver_outcome(o).build()?;
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        GEORGIA_EXPLANATION.clone()
    }
}

/// Decides the waiver asked for. Georgia's forms have no waiver for a
/// casualty, retirement or disability. For a disaster the Department
/// postpones due dates by announcement, so a disaster, or a casualty that
/// may be one, is left open.
fn georgia_waiver(k: WaiverKind, c: WaiverCause) -> WaiverOutcome {
    if k == WaiverKind::RetiredOrDisabled {
        estimated::waiver_refused(
            "Georgia's forms have no waiver of the penalty for retirement or disability.",
        )
    } else if c == WaiverCause::Casualty {
        estimated::waiver_refused("Georgia's forms have no waiver of the penalty for a casualty.")
    } else if c == WaiverCause::Disaster {
        estimated::waiver_undecided(
            "Georgia has no disaster waiver. The Department postpones due dates for a disaster when it announces relief.",
        )
    } else {
        estimated::waiver_undecided(
            "Georgia has no casualty waiver. The Department postpones due dates for a disaster when it announces relief.",
        )
    }
}

/// Writes a date as a person reads it: "Apr 1" in the tax year, and "Jan 31,
/// 2027" outside it.
fn short(d: Date, year: i64) -> String {
    let m: String = d.month().to_string().chars().take(3).collect();
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

/// Written by hand and checked against research/estimated-payments/ga.md.
static GEORGIA_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Georgia expects estimated payments on income that tax isn't withheld from. It charges 9% a year on each installment you underpay. Its safe harbours are lower than federal. They are 70% of this year's tax or 100% of last year's.",
    )
    .paragraph("Whether you have to pay at all turns on income, not on tax. Estimated tax is required when expected gross income exceeds three amounts. They are your dependent exemptions, your deductions and $1,000, added together. For 2026, each exemption is $5,000 and the standard deduction is $15,000. A joint return's standard deduction is $30,000. Payments aren't needed if your employer agrees to withhold enough extra.")
    .paragraph("You avoid the penalty if withholding and payments reach a target. The target is 70% of this year's Georgia tax or, if lower, 100% of last year's. Last year's counts only from a return covering twelve months. There is no 110% tier for higher incomes. The amount is paid in equal installments on April 15, June 15 and September 15. The last is due January 15 of the next year. Becoming required later in the year means fewer, larger installments. Withholding counts as paid equally each quarter, whenever it was taken. If you file and pay the balance by January 31, it counts as paid on January 15.")
    .paragraph("Each payment counts first toward its own period's installment. Only what is left over makes up an earlier shortfall. Three tests can also excuse an installment, based on what you paid by its date. One is last year's income taxed at this year's 4.99% rate. Another is 70% of the tax on your annualised income. The third, except in January, is 90% of the tax on income actually received.")
    .paragraph("The penalty is 9% a year on each shortfall, by the day from its due date. It runs until the shortfall is paid or April 15 of the next year. The rate is fixed rather than tied to the federal rate. Georgia's forms have no waiver for a casualty, retirement or disability. The Department does postpone deadlines for declared disasters.")
    .versus_federal("Georgia's test is gross income over exemptions, deductions and $1,000. It is not $1,000 of tax. The tests are 70% of this year's tax and 100% of last year's, with no 110% tier. A payment goes first to its own period's installment. Three exceptions can excuse an installment. The penalty is a fixed 9% a year. There is no casualty, retirement or disability waiver.")
    .source("O.C.G.A. §§ 48-7-114 to 48-7-120, as applied by the Department of Revenue's forms")
    .source("Form 500 UET, Underpayment of Estimated Tax by Individuals/Fiduciary (Rev. 07/15/25)")
    .source("Form 500-ES, Individual and Fiduciary Estimated Tax (Rev. 05/12/25)")
    .source("HB 463 (2026), §§ 2-1 to 2-3 and 5-1")
    .build()
    .unwrap_or_default()
});
