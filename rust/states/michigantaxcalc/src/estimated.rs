//! Michigan's estimated income tax rules, MCL 206.301 with the interest and
//! penalties of MCL 205.23 and 205.24, as the shared [`EstimatedTax`]
//! interface.
//!
//! MCL 206.301(11) adopts the federal computation of the installments and
//! of the period of underpayment, so the required payment, the quarters and
//! the interest period look federal. The charge does not. Michigan charges
//! two things on each quarter's shortfall: interest by the day at a factor
//! Treasury prints for each half-year, and a flat penalty of 25% or 10% with
//! $25 and $10 minimums. The two are figured on different underpayments:
//! interest applies a later payment to the oldest shortfall first, and the
//! penalty counts a payment only in the quarter it arrives.
//!
//! The figure follows Treasury's forms, the 2026 MI-1040ES and the 2025
//! MI-2210, and the result's Divergences say where the statutes read
//! otherwise.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, FactorSeries, Facts, Installment,
    LedgerEntry, PartKind, Payment, Penalty, Requirement, Rounding, TargetOptionBuilder,
    WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents, percent};
use num_bigint::BigInt;
use working::{Divergence, Notes, PositionKind, Reading};

const JURISDICTION: estimated::Jurisdiction = estimated::MICHIGAN;

/// The facts Michigan's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    seafaring_share: Rate,

    prior_estimates_required: bool,
    prior_estimates_answered: bool,

    prior_separate: bool,
    prior_separate_answered: bool,
}

impl EstimatedFacts {
    /// The share of gross income from seafaring, in basis points. MCL
    /// 206.301(6) puts a seafarer under the farmers' and fishermen's rules,
    /// which take a filer with at least two-thirds of gross income from the
    /// three together, in this year or last. Pass the larger year's shares,
    /// here and in [`estimated::FactsBuilder::farm_share`] and
    /// [`estimated::FactsBuilder::fish_share`]. Zero when not given, as the
    /// farm and fish shares are.
    pub fn seafaring_share(&self) -> Rate {
        self.seafaring_share
    }

    /// Whether the filer was required to make estimated payments for last
    /// year, or `None` if the caller did not say. MCL 205.24(6) charges no
    /// penalty, though interest still runs, when they were not. When it is
    /// not said, the rules work it out from last year's tax and withholding
    /// if both were given.
    pub fn prior_year_estimates_required(&self) -> Option<bool> {
        self.prior_estimates_answered
            .then_some(self.prior_estimates_required)
    }

    /// Whether last year's return was married filing separately, or `None`
    /// if the caller did not say. The 2025 MI-2210 tests last year's AGI
    /// against $75,000 for that status and $150,000 otherwise. When it is
    /// not said, this year's filing status is used.
    pub fn prior_year_married_filing_separately(&self) -> Option<bool> {
        self.prior_separate_answered.then_some(self.prior_separate)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts an [`EstimatedFacts`] with nothing given.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Gives the share of gross income from seafaring.
    pub fn seafaring_share(&mut self, r: Rate) -> &mut Self {
        self.e.seafaring_share = r;
        self
    }

    /// Says whether estimated payments were required for last year.
    pub fn prior_year_estimates_required(&mut self, required: bool) -> &mut Self {
        (
            self.e.prior_estimates_required,
            self.e.prior_estimates_answered,
        ) = (required, true);
        self
    }

    /// Says whether last year's return was married filing separately.
    pub fn prior_year_married_filing_separately(&mut self, separate: bool) -> &mut Self {
        (self.e.prior_separate, self.e.prior_separate_answered) = (separate, true);
        self
    }

    /// Refuses a seafaring share outside 0% to 100%.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        let s = self.e.seafaring_share;
        if s.0 < 0 || s.0 > 10_000 {
            return Err(Error::Invalid(format!(
                "michigantaxcalc: a seafaring share of {s} is not a share"
            )));
        }
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// The tax after withholding and credits at or below which no penalty
    /// or interest arises.
    pub(crate) threshold: USCents,

    /// Last year's AGI above which the prior-year test is 110%: $150,000, or
    /// $75,000 if last year's return was separate.
    pub(crate) prior_year_step: USCents,
    pub(crate) prior_year_step_separate: USCents,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// The January 31 date before it is moved for a weekend.
    pub(crate) file_and_pay_by: Date,
    pub(crate) stop: Date,
    pub(crate) period_ends: Vec<Date>,

    /// The first days of the MI-1040ES chart's second, third and fourth
    /// rows, for reporting a late start.
    pub(crate) late_groups: Vec<Date>,

    pub(crate) daily: FactorSeries,

    /// The flat penalty: `not_paid_rate` for a quarter with nothing paid,
    /// `short_rate` for one paid short, each with its minimum.
    pub(crate) not_paid_rate: Rate,
    pub(crate) short_rate: Rate,
    pub(crate) not_paid_minimum: USCents,
    pub(crate) short_minimum: USCents,
}

impl EstimatedYear {
    /// Prints `d` for a sentence: "Aug 31" in the tax year, "Jan 15, 2027"
    /// outside it.
    fn date(&self, d: Date) -> String {
        let month = d.month().to_string();
        let m: String = month.chars().take(3).collect();
        if d.year() == self.year {
            return format!("{m} {}", d.day());
        }
        format!("{m} {}, {}", d.day(), d.year())
    }
}

/// MCL 206.301(1) and MI-2210 line 9: a quarter of the required payment on
/// each date. The MI-2210 line 10 instructions deem withholding paid in the
/// same quarters.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// MI-2210 Annualized Income Worksheet line 9.
const ANNUALISED_PERCENTAGES: [Rate; 4] = [Rate(2_250), Rate(4_500), Rate(6_750), Rate(9_000)];

/// Returns Michigan's estimated income tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(MichiganEstimated { y }))
}

struct MichiganEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// MI-2210 line 9: what each date requires, for interest and for the
    /// penalty alike.
    installments: Vec<USCents>,

    /// The January 31 rule, which takes the flat penalty off the fourth
    /// quarter and leaves its interest.
    excused_january: bool,

    /// Why no flat penalty is charged on any quarter, or empty.
    no_flat: String,
    no_flat_cite: String,

    /// Whether MCL 205.24(6) could not be decided.
    prior_estimates_unknown: bool,

    notes: Notes,
}

impl Assessment {
    /// Whether quarter `i` bears a flat penalty.
    fn flat_charged(&self, i: usize, c: &Column) -> bool {
        c.under.0 > 0
            && self.no_flat.is_empty()
            && !(self.excused_january && i == self.dues.len() - 1)
    }
}

/// The installments after the credits, with the credits, the ledger they
/// come from, and the notes on how they were credited.
type Scheduled = (Vec<Installment>, Vec<Payment>, Vec<LedgerEntry>, Notes);

/// One quarter of MI-2210 Part 3.
#[derive(Clone, Copy, Debug, Default)]
struct Column {
    /// Line 23.
    under: USCents,
    /// Line 24.
    rate: Rate,
    minimum: USCents,
    /// Line 25, in whole dollars.
    charge: USCents,
}

/// Go's `%T` of the extras, as far as it can be told from here.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else {
        "an unknown type"
    }
}

impl MichiganEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "michigantaxcalc: the facts carry {}, not michigantaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, e: &EstimatedFacts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "michigantaxcalc: these are Michigan's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "michigantaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        let share = i64::from(f.farm_share().0)
            + i64::from(f.fish_share().0)
            + i64::from(e.seafaring_share().0);
        if share > 10_000 {
            return Err(Error::Invalid(
                "michigantaxcalc: farming, fishing and seafaring shares add to more than the whole income".into(),
            ));
        }
        if share * 3 >= 20_000 {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a filer with two-thirds of gross income from farming, fishing or seafaring pays once by January 15 or files by March 1 (MCL 206.301(6)), which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "michigantaxcalc: MI-2210 annualised period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where Treasury's forms and the statutes part ways on a rule every
    /// Michigan computation uses. They are found in the research file, and
    /// each statute is in the Go package's testdata/. `interest`, `flat` and
    /// `feb1` say whether the facts have a shortfall, a flat penalty, or a
    /// return filed on February 1.
    fn divergences(
        &self,
        interest: bool,
        flat: bool,
        feb1: bool,
    ) -> Result<Vec<Divergence>, Error> {
        let mut out = Vec::new();
        for b in [
            working::new_divergence("How interest accrues")
                .form("Interest is charged at a daily factor Treasury prints, in whole dollars. The factor is 0.0002324 for Jan to Jun 2026 and 0.0002150 for Jul to Dec 2026. It is 0.0002129 for Jan to Jun 2027. The first two aren't the annual rate over 365.",
                    "2025 MI-2210, Part 2, lines 19 to 21; RAB 2026-5; RAB 2026-10")
                .statute("Interest runs from the time the payment was due until it's paid. Its monthly rate is one point above the adjusted prime rate, divided by 12.",
                    "MCL 205.23(2)")
                .uses(Reading::Form)
                .affects(interest)
                .clone(),
            working::new_divergence("Which flat penalty applies")
                .form("A quarter with nothing paid is charged 25% of its underpayment, at least $25. A quarter paid short is charged 10%, at least $10. Either applies whatever the reason.",
                    "2025 MI-2210, Part 3 and the Line 24 instructions; RAB 2021-17, Penalties, notes 30 and 31")
                .statute("The 10% penalty applies when the shortfall is due to negligence. It is waived for reasonable cause. The 25% penalty applies to intentional disregard of the law or department rules.",
                    "MCL 205.23(3), (4)")
                .uses(Reading::Form)
                .affects(flat)
                .clone(),
            working::new_divergence("A return filed and paid by January 31, 2027")
                .form("The 2026 MI-1040ES says \"before February 1, 2027\". It also says \"by February 1, 2027\". Treasury's bulletin says \"on or before January 31\". A return filed and paid by Monday, Feb 1, 2027 counts as in time.",
                    "2026 MI-1040ES, Income Changes During the Year; RAB 2021-17, Penalties")
                .statute("Jan 31, 2027 is a Sunday. A deadline on a weekend or legal holiday moves to the next day that isn't one. So the deadline moves to Monday, Feb 1, 2027.",
                    "MCL 8.6; MCL 206.301(11), adopting 26 U.S.C. § 6654(h) (RAB 2021-17, note 29)")
                .uses(Reading::Form)
                .affects(feb1)
                .clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// The requirement and installments, with the divergences and whether
    /// each changes anything for `f`.
    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let mut a = self.measure(f)?;
        let (mut interest, mut flat, mut feb1) = (false, false, false);
        if !a.exempt && a.requirement.target_known() {
            let (credits, _) = self.credits(f, &a)?;
            let ledger = estimated::apply(&a.dues, &a.installments, &credits)?;
            for l in &ledger {
                interest = interest || l.shortfall().0 > 0;
            }
            for (i, c) in self.columns(&credits, &a, false).iter().enumerate() {
                flat = flat || a.flat_charged(i, c);
            }
            let file_by = self.y.calendar.next_business_day(self.y.file_and_pay_by);
            feb1 = file_by != self.y.file_and_pay_by && f.return_filed() == file_by;
        }
        let divergences = self.divergences(interest, flat, feb1)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    fn measure(&self, f: &Facts) -> Result<Assessment, Error> {
        let extras = self.extras(f)?;
        self.check(f, &extras)?;
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
        let prior_ret = f.prior_year();
        let has_prior = prior_ret.is_some();
        let prior = prior_ret.unwrap_or_default();
        let mut prior_figures = estimated::Figures::default();
        if has_prior {
            prior_figures = prior.figures().unwrap_or_default();
        }
        let prior_tax = prior_figures.tax().unwrap_or_default();

        // 90% of this year's tax, MI-2210 line 6.
        let this_year = if tax_known {
            estimated::new_target_option(Basis::ThisYearTax, percent(90), percent(90).of(tax))
                .build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, percent(90), USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            a.notes.rule(
                "MCL 206.301(11); 2025 MI-2210, Part 1, lines 5 and 6; 2026 MI-1040ES, Who Must File",
                format!(
                    "90% of this year's Michigan tax of {} is {}",
                    tax,
                    this_year.amount()
                ),
            );
        }

        // Last year's tax, MI-2210 line 4: 100%, or 110% above the step, for
        // a filed 12-month return.
        let unavailable = |why: String| -> TargetOptionBuilder {
            estimated::new_target_option(Basis::PriorYearTax, percent(100), USCents(0))
                .unavailable(why)
                .clone()
        };
        // Asks for the prior-year return when no exception sets the target.
        let mut ask_prior = false;
        let prior_option = if !has_prior {
            ask_prior = true;
            unavailable(format!("Needs your {last} Michigan return"))
        } else if prior.not_required_to_file() {
            unavailable(format!(
                "You weren't required to file a {last} Michigan return"
            ))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Michigan return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Michigan return covered {} months, not 12",
                prior.months()
            ))
        } else {
            let agi_opt = prior_figures.federal_agi();
            let agi = agi_opt.unwrap_or_default();
            let separate_opt = extras.prior_year_married_filing_separately();
            let separate = separate_opt
                .unwrap_or(f.filing_status() == filing::Status::MarriedFilingSeparately);
            let mut step = y.prior_year_step;
            if separate {
                step = y.prior_year_step_separate;
            }
            let mut rate = percent(100);
            let mut why = format!("{last} AGI of {agi} is not more than {step}");
            if agi_opt.is_none() {
                rate = percent(110);
                why = format!("{last} AGI was not given");
                a.notes.ask(
                    PositionKind::AdjustedGrossIncome,
                    format!("What was your {last} federal adjusted gross income?"),
                    format!(
                        "Until then, last year's tax counts at 110%, or 100% if AGI was {} or less.",
                        step.whole()
                    ),
                );
            } else if agi > step {
                rate = percent(110);
                why = format!("{last} AGI of {agi} is more than {step}");
            }
            if agi_opt.is_some()
                && separate_opt.is_none()
                && agi > y.prior_year_step_separate
                && agi <= y.prior_year_step
            {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!("Was your {last} return married filing separately?"),
                    format!(
                        "If so, your {last} AGI of {} is over its {} step. Last year's tax then counts at 110%.",
                        agi.whole(),
                        y.prior_year_step_separate.whole()
                    ),
                );
            }
            let amount = rate.of(prior_tax);
            a.notes.rule(
                "MCL 206.301(11); 2025 MI-2210, Part 1, line 4; 2026 MI-1040ES, Who Must File; RAB 2021-17",
                format!(
                    "{why}, so the prior-year test is {rate} of the {prior_tax} tax on the {last} return, or {amount}"
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
        if !tax_known {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!(
                    "What do you expect your {} Michigan tax after credits to be?",
                    y.year
                ),
                format!(
                    "It sets the 90% option and the {} test.",
                    y.threshold.whole()
                ),
            );
        }

        // The exceptions. A filed 12-month return with no tax makes the
        // target nothing at all. A filed return with no tax that was not for
        // 12 months, or no return because none was required, removes the
        // flat penalty, and whether interest is still charged is open.
        let no_liability_full =
            has_prior && prior.filed() && prior_tax.0 == 0 && prior.months() == 12;
        let no_liability_short =
            has_prior && prior.filed() && prior_tax.0 == 0 && prior.months() != 12;
        let not_required = has_prior && prior.not_required_to_file() && prior.months() == 12;
        if no_liability_short || not_required {
            let mut what = format!("you had no Michigan tax for {last}");
            let mut because = format!("You had no Michigan tax for {last}.");
            if not_required {
                what = format!(
                    "you needed no {last} Michigan return and your {last} federal one covered 12 months"
                );
                because = format!(
                    "You needed no {last} Michigan return, and your {last} federal one covered 12 months."
                );
            }
            a.no_flat = what.clone();
            a.no_flat_cite = "2025 MI-2210, General Instructions".into();
            a.notes.rule(
                a.no_flat_cite.clone(),
                format!("{what}, so no 25% or 10% penalty is charged for {}", y.year),
            );
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                because + " So the 10% or 25% penalty is excused.",
            );
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                "No published source says if interest is still charged then. The figures charge it.",
            );
        }

        // MCL 205.24(6): no penalty when no estimates were required last
        // year.
        let mut prior_est_opt = extras.prior_year_estimates_required();
        let mut derived = String::new();
        if prior_est_opt.is_none() && has_prior {
            if prior.not_required_to_file() {
                prior_est_opt = Some(false);
                derived = format!("no {last} return was required");
            } else if prior.filed()
                && let Some(w) = prior_figures.withheld()
            {
                let est = prior_tax - w > y.threshold;
                prior_est_opt = Some(est);
                derived = format!(
                    "the {last} tax of {} less {} of withholding is {}, which is ",
                    prior_tax,
                    w,
                    prior_tax - w
                );
                if est {
                    derived.push_str(&format!("more than {}", y.threshold));
                } else {
                    derived.push_str(&format!("not more than {}", y.threshold));
                }
            }
        }
        a.prior_estimates_unknown = prior_est_opt.is_none();
        if prior_est_opt == Some(false) && a.no_flat.is_empty() && !no_liability_full {
            a.no_flat = format!("you were not required to make estimated payments for {last}");
            a.no_flat_cite = "MCL 205.24(6); 2025 MI-2210, General Instructions".into();
            let mut because = "as the caller said".to_string();
            if !derived.is_empty() {
                because = derived + " (MCL 206.301(1), unchanged since 2011)";
            }
            a.notes.rule(
                a.no_flat_cite.clone(),
                format!(
                    "{}, {because}, so no 25% or 10% penalty is charged for {}; interest is still charged",
                    a.no_flat, y.year
                ),
            );
        }

        if no_liability_full {
            a.exempt = true;
            let none = estimated::new_exception("No tax due last year").build()?;
            rb.required(
                false,
                format!(
                    "Your {last} Michigan return covered a full 12 months and showed no tax. That means no estimated payments are needed for {}.",
                    y.year
                ),
            )
            .exempt(none);
            a.notes.rule(
                "2025 MI-2210, General Instructions and line 7; RAB 2021-17",
                format!(
                    "100% of the $0.00 tax on the {last} return is $0.00, the smaller test, so no installment can be short"
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "There's no target yet. It needs your {} Michigan tax or a full-year {last} return.",
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
            rb.required(
                true,
                format!(
                    "Your target is {} of your {last} tax, {}. After {} withheld, you need {} of estimated payments. Pay a quarter by each date, whatever your {} tax turns out to be.",
                    prior_opt.percent(),
                    target.whole(),
                    withheld.whole(),
                    (target - withheld).whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if tax - withheld <= y.threshold {
            a.exempt = true;
            let left = (tax - withheld).max(USCents(0));
            let small = estimated::exception_at_most("Tax less withholding", left, y.threshold)?;
            let mut under = format!(", which is {} or less", y.threshold.whole());
            if tax < withheld {
                under = String::new();
            }
            rb.required(
                false,
                format!(
                    "Your Michigan tax after credits is {}, and {} is withheld. That {}{}. So no estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld),
                    under
                ),
            )
            .exempt(small);
            a.notes.rule(
                "MCL 206.301(1); 2025 MI-2210, Line-by-Line Instructions",
                format!(
                    "{} of tax less {} withheld is {}, not more than {}, so the form is not completed",
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
                    "Your Michigan withholding of {} already reaches the {} target. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Michigan tax after credits is {}, and {} is withheld. That leaves {}, more than {}. You need {} of estimated payments to reach the {} target. Pay a quarter by each date.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.threshold.whole(),
                    (target - withheld).whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        }
        if ask_prior && !a.exempt {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Michigan return, and what was its tax?"),
                "With it, last year's tax could be a lower target than 90% of this year's.",
            );
        }
        a.requirement = rb.build()?;
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }

        let cum = estimated::cumulative_shares(a.requirement.target(), &quarters())?;
        a.installments = estimated::increments(&cum);
        a.notes.rule(
            "MCL 206.301(1); 2025 MI-2210, Part 1, line 9; 2026 MI-1040ES, Payment Due Dates",
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

        if f.elections().annualised() {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &ANNUALISED_PERCENTAGES, &a.installments)?;
            a.notes.rule(
                "MCL 206.301(11); 2025 MI-2210, Annualized Income Worksheet, lines 9 to 16; RAB 2021-17",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: 22.5%, 45%, 67.5% and 90% of the annualised tax, less earlier installments, and never more than the regular quarter plus any amount the annualised method saved earlier",
                    ann[0], ann[1], ann[2], ann[3]
                ),
            );
            a.installments = ann;
        }

        // January 31: no penalty on the fourth quarter, interest unchanged.
        let file_by = y.calendar.next_business_day(y.file_and_pay_by);
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(file_by) {
            a.excused_january = true;
            a.notes.rule(
                "RAB 2021-17, Penalties and Example 6; 2026 MI-1040ES, Income Changes During the Year; MCL 8.6",
                format!(
                    "the return was filed and paid on {}, by {} (January 31, 2027 is a Sunday), so no penalty is charged on the {} installment; its interest still runs",
                    filed.long(),
                    file_by.long(),
                    a.dues[3].long()
                ),
            );
            if filed == file_by {
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "Treasury's instructions say both \"before\" and \"by\" Jan 31, a Sunday. The figures treat filing and paying on {} as in time.",
                        y.date(filed)
                    ),
                );
            }
        }

        // A late start: the MI-1040ES chart, which the MI-2210 does not
        // apply.
        let met = f.requirement_first_met();
        if !met.is_zero() && !met.before(y.late_groups[0]) {
            let first = estimated::late_start(met, &y.late_groups);
            let first_due = a
                .dues
                .get(usize::try_from(first).unwrap_or_default())
                .copied()
                .unwrap_or_default();
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "Your income changed on {}. The MI-1040ES says to pay first on {}.",
                    y.date(met),
                    y.date(first_due)
                ),
            );
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                "The MI-2210 has no late-start rule, and the figures charge all four quarters.",
            );
        }
        Ok(a)
    }

    /// What counts as paid, dated: withholding a quarter on each due date,
    /// or on the dates withheld by election, payments on their dates, and
    /// last year's overpayment on the first due date.
    fn credits(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Payment>, Notes), Error> {
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            let credits = estimated::credits_as_withheld(f, &a.dues)?;
            notes.rule(
                "2025 MI-2210, Line 10 instructions; RAB 2021-17, Withholding and Overpayments",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
            return Ok((credits, notes));
        }
        let credits = estimated::credits(f, &a.dues, &quarters())?;
        notes.rule(
            "2025 MI-2210, Line 10 instructions; RAB 2021-17, Withholding and Overpayments",
            format!(
                "one-fourth of the {} withheld is treated as paid on each due date, whenever it was actually withheld, and last year's overpayment applied forward counts toward the first",
                f.withheld_total()
            ),
        );
        Ok((credits, notes))
    }

    /// MI-2210 Part 3. A credit counts only in the quarter whose window it
    /// falls in: on or before April 15, then after each due date up to and
    /// including the next. An overpayment is carried forward and never back,
    /// and a credit after the last due date counts in no quarter. With
    /// `jan16`, a credit on the day after the last due date counts in the
    /// fourth quarter.
    fn columns(&self, credits: &[Payment], a: &Assessment, jan16: bool) -> Vec<Column> {
        let y = &self.y;
        let mut out = Vec::with_capacity(a.dues.len());
        let mut carry = USCents(0);
        for (i, &due) in a.dues.iter().enumerate() {
            let mut paid = USCents(0);
            for c in credits {
                let on = c.on();
                let mut in_window = (i == 0 || on.after(a.dues[i - 1])) && !on.after(due);
                if jan16 && i == a.dues.len() - 1 && on == due.add_days(1) {
                    in_window = true;
                }
                if in_window {
                    paid += c.amount();
                }
            }
            let mut col = Column::default();
            let have = paid + carry;
            let required = a.installments[i];
            if have >= required {
                carry = have - required;
            } else {
                carry = USCents(0);
                col.under = required - have;
                (col.rate, col.minimum) = (y.short_rate, y.short_minimum);
                if have.0 == 0 {
                    (col.rate, col.minimum) = (y.not_paid_rate, y.not_paid_minimum);
                }
                // The MI-2210 rounds every money item to the whole dollar.
                let charge = Rounding::RoundToDollar.div(
                    &(BigInt::from(col.under.0) * BigInt::from(col.rate.0)),
                    &BigInt::from(10_000),
                );
                col.charge = col.minimum.max(charge);
            }
            out.push(col);
        }
        out
    }

    /// The first quarter charged the 25% penalty after which a payment was
    /// made, if there is one.
    fn paid_late(&self, f: &Facts, a: &Assessment, cols: &[Column], stop: Date) -> Option<Date> {
        for (i, c) in cols.iter().enumerate() {
            if !a.flat_charged(i, c) || c.rate != self.y.not_paid_rate || !stop.after(a.dues[i]) {
                continue;
            }
            for pay in f.payments() {
                if !pay.from_prior_year() && pay.on().after(a.dues[i]) {
                    return Some(a.dues[i]);
                }
            }
        }
        None
    }

    /// The installments after the credits, with the credits and the ledger
    /// they come from.
    fn schedule_of(&self, f: &Facts, a: &Assessment) -> Result<Scheduled, Error> {
        let (credits, notes) = self.credits(f, a)?;
        let ledger = estimated::apply(&a.dues, &a.installments, &credits)?;
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
        Ok((out, credits, ledger, notes))
    }
}

/// Decides a waiver asked for. Michigan has no standing casualty, disaster,
/// retirement or disability waiver. Treasury waives penalty and interest
/// for a declared disaster by a notice for each event, and RAB 2025-15
/// waives the 10% or 25% penalty, not the interest, for reasonable cause,
/// which includes records destroyed by casualty.
fn michigan_waiver(f: &Facts) -> Option<WaiverOutcome> {
    let k = f.positions().waiver()?;
    match k {
        WaiverKind::None => None,
        WaiverKind::Casualty => Some(estimated::waiver_undecided(
            "Records destroyed by a casualty can waive the 10% or 25% penalty, not interest. Treasury waives both only for a disaster it has granted relief for by notice.",
        )),
        _ => Some(estimated::waiver_refused(
            "Michigan has no waiver for retirement or disability. For reasonable cause, Treasury can waive the 10% or 25% penalty, not interest.",
        )),
    }
}

impl EstimatedTax for MichiganEstimated {
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
                    "with no {} Michigan return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (out, _, _, notes) = self.schedule_of(f, &a)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "michigantaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Michigan return to measure against, there is no target to figure a penalty from",
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
        let (sched, credits, ledger, mut notes) = self.schedule_of(f, &a)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }
        let cols = self.columns(&credits, &a, false);

        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &sched);
        let mut unrated = Date::default();
        let (mut interest, mut flat) = (USCents(0), USCents(0));
        for (i, l) in ledger.iter().enumerate() {
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
                let (parts, gap) = estimated::daily_factor(piece.amount(), l.due(), to, &y.daily)?;
                if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                    unrated = gap;
                }
                for p in parts {
                    interest += p.charge();
                    lb.part(p);
                }
            }
            let c = &cols[i];
            if a.flat_charged(i, c) && stop.after(l.due()) {
                let part = estimated::new_penalty_part(PartKind::Flat, c.under, c.rate)
                    .span(l.due(), l.due())
                    .charge(c.charge)
                    .source(format!(
                        "2025 MI-2210, Part 3, lines 23 to 25: {}, at least {}; MCL 205.23(3), (4)",
                        c.rate, c.minimum
                    ))
                    .build()?;
                flat += c.charge;
                lb.part(part);
            }
            let line = lb.days(last_day.days_since(l.due())).build()?;
            pb.line(line);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;

        if paid_in_full {
            notes.rule(
                "MCL 205.23(2); 2025 MI-2210, Part 2",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "MCL 205.23(2); 2025 MI-2210, Part 2; RAB 2021-17, Interest",
            format!(
                "each shortfall is charged the daily factor Treasury prints for its half-year, in whole dollars, from its due date until paid or {}, with each payment applied to the oldest shortfall first; the interest to {} is {}",
                y.stop.long(),
                stop.long(),
                interest
            ),
        );
        if !a.no_flat.is_empty() {
            notes.rule(
                a.no_flat_cite.clone(),
                format!(
                    "{}, so no 25% or 10% penalty is charged; the total is the interest alone",
                    a.no_flat
                ),
            );
        } else {
            notes.rule(
                "MCL 205.23(3), (4); 2025 MI-2210, Part 3; RAB 2021-17, Penalties",
                format!(
                    "each quarter's underpayment, counting a payment only in the quarter it is received and carrying any overpayment forward, is charged {} (at least {}) when nothing was paid for the quarter and {} (at least {}) when it was paid short; the penalty is {}, for a total of {}",
                    y.not_paid_rate,
                    y.not_paid_minimum,
                    y.short_rate,
                    y.short_minimum,
                    flat,
                    p.total()
                ),
            );
        }
        if !a.tax_known {
            notes.rule(
                "MCL 206.301(11); 2025 MI-2210, Part 1, line 7",
                format!(
                    "this year's Michigan tax was not given, so interest and penalty are figured against {} of the {} tax alone; the required payment is the lesser of that and 90% of this year's tax, so {} is the most they can be, and they are less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }

        if flat.0 > 0 {
            // A credit on the day after the last due date: the MI-2210
            // counts the fourth quarter's payments through January 15 and
            // calls only those after January 16 not estimates.
            let alt = self.columns(&credits, &a, true);
            let mut alt_flat = USCents(0);
            for (i, c) in alt.iter().enumerate() {
                if a.flat_charged(i, c) && stop.after(a.dues[i]) {
                    alt_flat += c.charge;
                }
            }
            if alt_flat != flat {
                let jan16 = a.dues[a.dues.len() - 1].add_days(1);
                notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "The MI-2210 doesn't say if paying on {} counts toward January. The figures leave it out, for a penalty of {} rather than {}.",
                        y.date(jan16),
                        flat.whole(),
                        alt_flat.whole()
                    ),
                );
            }

            // A quarter charged 25% that was paid after its due date.
            if let Some(due) = self.paid_late(f, &a, &cols, stop) {
                notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "The MI-2210 charges 25% when the {} quarter is paid late. The MI-1040ES charges 10% for paying late, and the figures charge 25%.",
                        y.date(due)
                    ),
                );
            }

            if a.prior_estimates_unknown {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "Were you required to make estimated payments for {}?",
                        y.year - 1
                    ),
                    "If you weren't, there's no 10% or 25% penalty, only interest.",
                );
            }
        }

        if !unrated.is_zero() {
            p = p.to_builder().rated_through(unrated.add_days(-1)).build()?;
        }
        if !unrated.is_zero() {
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The rate for the days after {} isn't published yet.",
                    y.date(unrated.add_days(-1))
                ),
            );
        }
        if p.total().0 > 0 && !waiver_answered {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking Treasury to waive the penalty?",
                "Reasonable cause shown in writing waives the 10% or 25% penalty, not interest. After a declared disaster, Treasury waives interest too, for people who ask.",
            );
        } else if waiver == WaiverKind::RetiredOrDisabled {
            notes.rule(
                "RAB 2025-15, Waiver for Reasonable Cause; 2025 MI-2210, General Instructions",
                "Michigan has no waiver for retirement after 62 or disability; a written request showing reasonable cause can waive the 10% or 25% penalty, not the interest",
            );
        } else if waiver == WaiverKind::Casualty {
            notes.rule(
                "RAB 2025-15, Waiver for Reasonable Cause; Treasury notice of March 19, 2026",
                "Treasury grants disaster relief by a notice for each event, which waives penalty and interest for taxpayers who ask; a casualty that destroyed records is reasonable cause, which waives the 10% or 25% penalty only",
            );
        }
        if let Some(o) = michigan_waiver(f)
            && p.total().0 > 0
        {
            let undecided = o.undecided();
            p = p.to_builder().waiver_outcome(o).build()?;
            if undecided {
                notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "Treasury's {} disaster notices grant relief for an affected period. They don't say whether it covers estimated payments due in that period.",
                        y.year
                    ),
                );
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    /// Written by hand and checked against research/estimated-payments/mi.md.
    fn explanation(&self) -> Explanation {
        estimated::new_explanation(
            "Michigan expects estimated payments when more than $500 will be due. The $500 is measured after withholding and credits. Falling short costs interest on each quarter's shortfall. A flat penalty of 10% or 25% of that shortfall is added.",
        )
        .paragraph("You are clear if withholding and estimated payments reach a target. It is the lesser of 90% of this year's Michigan tax or 100% of last year's. Last year's figure is 110% when its adjusted gross income was over $150,000. The limit is $75,000 if last year's return was married filing separately. Last year's figure also needs a return you filed for a full 12 months.")
        .paragraph("The payments are four equal quarters. They are due April 15, June 15, September 15 and January 15 of the next year. A date on a weekend or legal holiday shifts to the next business day. Withholding counts a quarter on each date, unless you show when it was taken. Form MI-2210's annualised worksheet can lower early quarters for uneven income.")
        .paragraph("Interest runs by the day on each quarter's shortfall from its due date. It stops when the shortfall is paid, or on April 15 of the next year. Treasury sets the rate each January and July at one point above the prime rate. A later payment pays the oldest shortfall first. For the flat penalty, a payment counts only for the quarter in which it arrives. Catching up later stops the interest but not the penalty. It is 25% of the shortfall, at least $25, for a quarter with nothing paid. It is 10%, at least $10, for a quarter paid short.")
        .paragraph("This year's tax after withholding and credits may be $500 or less. If so, nothing is charged. Nothing is charged if last year's return covered 12 months and showed no tax. No flat penalty applies if last year needed no estimated payments or no return. Interest can still be due. You can file and pay in full by the end of January. That removes the January quarter's penalty but not its interest. Treasury waives the penalty for reasonable cause shown in a written request. That waiver doesn't cover interest. After a declared disaster, Treasury can waive both for people who ask. There is no waiver for retirement or disability.")
        .versus_federal("Michigan follows the federal required payment, quarters and interest period. Its threshold is $500 rather than $1,000. It adds a flat 10% or 25% penalty on each quarter's shortfall to the interest. That penalty is figured without applying later payments to earlier quarters. The interest rate is prime plus one point, set twice a year. There is no waiver for retirement or disability.")
        .source("MCL 206.301; MCL 205.23(2) to (4); MCL 205.24(6); MCL 8.6")
        .source("2026 MI-1040ES, Michigan Estimated Income Tax for Individuals (Rev. 04-25)")
        .source("2025 MI-2210, Underpayment of Estimated Income Tax, and its instructions (Rev. 10-25)")
        .source("Revenue Administrative Bulletin 2021-17, Estimated Payments for Individuals and Fiduciaries")
        .source("Revenue Administrative Bulletins 2026-5 and 2026-10 (interest rates)")
        .source("Revenue Administrative Bulletin 2025-15, Penalty Provisions")
        .build()
        .unwrap_or_default()
    }
}
