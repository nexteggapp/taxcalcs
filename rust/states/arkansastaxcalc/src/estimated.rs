//! Arkansas's estimated tax rules under the Income Tax Act of 1987, as the
//! shared [`EstimatedTax`] interface.
//!
//! The figure follows the Department of Finance and Administration's forms:
//! the 2026 AR1000ES, and the 2025 AR2210, its instructions and AR2210A,
//! which are the latest published. The Arkansas Code's text was not
//! retrieved, so the statutory side of each divergence is the restatement of
//! the law on page 1 of the AR2210 instructions, cited as such. No act
//! changing the estimated tax rules for 2026 was found
//! (research/estimated-payments/ar.md), so no form here predates the law it
//! describes.
//!
//! Arkansas reads no fact that other jurisdictions do not, so it has no
//! extras type, and facts carrying any are refused.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, FactorSeries, Facts, Installment,
    LedgerEntry, PartKind, Payment, Penalty, PenaltyPart, Requirement, Rounding,
    TargetOptionBuilder,
};
use money::{Rate, USCents};
use num_bigint::BigInt;
use working::{Divergence, DivergenceBuilder, Notes, PositionKind, Reading};

const JURISDICTION: estimated::Jurisdiction = estimated::ARKANSAS;

/// The statutory source of every divergence: the Code itself was not
/// retrieved, and the instructions restate it in the Code's own words.
const RESTATED_SOURCE: &str = "Arkansas Income Tax Act, Ark. Code Ann. title 26, chapter 51, as restated in the AR2210 Instructions (2025), page 1; the Code's own text was not retrieved";

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// AR2210 line 4: no penalty when this year's net tax less Arkansas
    /// withholding is this much or less.
    pub(crate) threshold: USCents,

    /// AR2210 lines 2 and 5: the required annual payment is the smaller of
    /// 90% of this year's net tax and 100% of last year's.
    pub(crate) this_year_percent: Rate,
    pub(crate) prior_year_percent: Rate,

    /// AR2210 lines 7 and 8: a quarter of the required annual payment is
    /// due on each date, and payments are treated as made in the same
    /// quarters.
    pub(crate) shares: Vec<Rate>,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// AR2210 Exception 2: a return filed and paid by this date takes the
    /// year out of the penalty.
    pub(crate) file_and_pay_by: Date,

    /// AR2210 line 15: the penalty runs no later than April 15 of the next
    /// year.
    pub(crate) stop: Date,

    /// AR2210 line 16: the shortfall × days ÷ 365 × 10%. The rate is fixed,
    /// not reset each period.
    pub(crate) rate: Rate,
    pub(crate) rate_source: &'static str,
    pub(crate) year_days: i64,

    /// AR2210A: the periods on its line 1, the percentages on line 20 and
    /// the daily factor on line 43.
    pub(crate) period_ends: Vec<Date>,
    pub(crate) annualised_percentages: Vec<Rate>,
    pub(crate) annualised_factor: FactorSeries,
}

/// Returns Arkansas's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(ArkansasEstimated { y }))
}

struct ArkansasEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,

    /// No penalty can arise, so nothing is needed by any date.
    exempt: bool,
    /// It is Exception 2, the return filed and paid by January 31, which
    /// excuses every installment.
    filed_early: bool,

    /// Whether this year's tax was given. Without it the target is last
    /// year's tax alone, and the penalty is an upper bound.
    tax_known: bool,

    /// The installments are AR2210A's, and the penalty is charged at its
    /// printed daily factor.
    annualised: bool,

    dues: Vec<Date>,
    /// Each installment's own amount.
    installments: Vec<USCents>,
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

/// Which divergences change these facts' figures or answers.
#[derive(Clone, Copy, Debug, Default)]
struct Affected {
    threshold: bool,
    payments: bool,
    prior_figure: bool,
    filed_early: bool,
    late_start: bool,
    factor: bool,
}

fn build(bs: &[DivergenceBuilder]) -> Result<Vec<Divergence>, Error> {
    let mut out = Vec::new();
    for b in bs {
        out.push(b.build()?);
    }
    Ok(out)
}

impl ArkansasEstimated {
    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "arkansastaxcalc: these are Arkansas's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "arkansastaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if let Some(s) = f.state() {
            return Err(Error::Invalid(format!(
                "arkansastaxcalc: Arkansas's rules read no extras, and the facts carry {}",
                type_name(s)
            )));
        }
        if i64::from(f.farm_share().0) * 3 >= 20_000 {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer with two-thirds of gross income from farming pays by the fifteenth day of the second month after the year, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "arkansastaxcalc: AR2210A period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and the instructions' restatement of
    /// the law part ways on a rule every Arkansas computation uses. They are
    /// found in research/estimated-payments/ar.md, questions 4 and 10.
    fn divergences(&self, inn: Affected) -> Result<Vec<Divergence>, Error> {
        build(&[
            working::new_divergence("When a penalty can arise")
                .form("Line 4 takes this year's net tax less Arkansas withholding. If that is $1,000 or less, there's no penalty. A declaration is needed only when the estimated tax is more than $1,000.",
                    "AR2210 (2025), line 4; AR1000ES Instructions (2026), Who Must File a Declaration")
                .statute("A declaration is needed when the estimated tax is $1,000 or more. That applies only while withholding is under 90% of the estimated tax. The penalty is excused only below $1,000, so exactly $1,000 is inside the rules. The AR1000ES worksheet agrees.",
                    format!("{RESTATED_SOURCE}, paragraph 1(a); AR1000ES Worksheet (2026), line 8"))
                .uses(Reading::Form)
                .affects(inn.threshold)
                .clone(),
            working::new_divergence("When estimated payments count as paid")
                .form("Line 8 takes a quarter of line 3, which holds withholding only. The instructions' example puts all payments on line 3. It counts them in four equal parts on the due dates, whenever they were made.",
                    "AR2210 (2025), lines 3 and 8; AR2210 Instructions (2025), page 2, Penalty Computation Example")
                .statute("The penalty applies unless 90% of the amount due is paid by each quarterly date. A payment therefore counts from the day it's made.",
                    format!("{RESTATED_SOURCE}, paragraph 2(b); AR1000ES Instructions (2026), Underestimate of Tax"))
                .uses(Reading::Form)
                .affects(inn.payments)
                .clone(),
            working::new_divergence("The prior-year figure")
                .form("Line 5 takes last year's net tax with no condition, so any filed return counts. That includes a short year and a return with no tax. A return with no tax makes the required payment $0.",
                    "AR2210 (2025), lines 5 and 6")
                .statute("Last year's tax counts only from a filed return showing a liability. That return must be for a 12-month year.",
                    format!("{RESTATED_SOURCE}, paragraph 1(a)"))
                .uses(Reading::Form)
                .affects(inn.prior_figure)
                .clone(),
            working::new_divergence("A return filed and paid by January 31")
                .form("Exception 2 is in Part I, and if you qualify you stop there. So a return filed and paid by January 31 removes the penalty for the whole year. Subject 603 lists it as a waiver of the penalty.",
                    "AR2210 (2025), Part I; Subject 603 (rev. 12/22/2020), item 3")
                .statute("The return replaces the fourth installment, so it excuses that installment only.",
                    format!("{RESTATED_SOURCE}, Exceptions, paragraph 2"))
                .uses(Reading::Form)
                .affects(inn.filed_early)
                .clone(),
            working::new_divergence("A requirement first met after April 15")
                .form("Line 7 is a quarter of the required annual payment in every column. That holds whenever the requirement arose.",
                    "AR2210 (2025), line 7")
                .statute(format!("Meeting the requirement after April 15 means filing by the next quarterly date. The AR1000ES worksheet spreads your estimated tax over the vouchers left. That is a third from June 15, half from Sep 15, and all on Jan 15, {}.", self.y.year + 1),
                    format!("{RESTATED_SOURCE}, paragraph 1(d); AR1000ES Worksheet (2026)"))
                .uses(Reading::Form)
                .affects(inn.late_start)
                .clone(),
        ])
    }

    /// The one divergence only AR2210A uses.
    fn annualised_divergence(&self, inn: Affected) -> Result<Vec<Divergence>, Error> {
        build(&[working::new_divergence("The daily penalty factor on AR2210A")
            .form(
                "Each day is charged at a printed .00027397. That is 10% divided by 365, rounded to eight places.",
                "AR2210A (2025), line 43",
            )
            .statute(
                "The penalty is 10% a year. The AR2210 charges the days divided by 365, times 10%.",
                format!("{RESTATED_SOURCE}, paragraph 2(b); AR2210 (2025), line 16"),
            )
            .uses(Reading::Form)
            .affects(inn.factor)
            .clone()])
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let y = &self.y;
        let mut a = Assessment::default();
        let mut inn = Affected::default();
        let last = y.year - 1;

        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        for i in 0..a.dues.len() {
            if a.dues[i] != y.dues[i] {
                a.notes.rule(
                    "AR1000ES (2026), Important Notice",
                    format!(
                        "the installment due {} falls on a weekend or legal holiday, so it is due {}",
                        y.dues[i].long(),
                        a.dues[i].long()
                    ),
                );
            }
        }

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior_opt_ret = f.prior_year();
        let has_prior = prior_opt_ret.is_some();
        let prior = prior_opt_ret.unwrap_or_default();
        let mut prior_tax = USCents(0);
        if has_prior && let Some(pf) = prior.figures() {
            prior_tax = pf.tax().unwrap_or_default();
        }

        // AR2210 line 2: 90% of this year's net tax.
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
                "AR2210 (2025), line 2; AR1000ES (2026), Underestimate of Tax",
                format!(
                    "{} of this year's Arkansas net tax of {} is {}",
                    y.this_year_percent,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // AR2210 line 5: 100% of last year's net tax, from a filed return.
        let unavailable = |why: String| -> TargetOptionBuilder {
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_percent, USCents(0))
                .unavailable(why)
                .clone()
        };
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Arkansas return"))
        } else if !prior.filed() {
            let mut why = format!("You didn't file a {last} Arkansas return");
            if prior.not_required_to_file() {
                why = format!("You weren't required to file a {last} Arkansas return");
            }
            unavailable(why)
        } else {
            let amount = y.prior_year_percent.of(prior_tax);
            a.notes.rule(
                "AR2210 (2025), lines 5 and 6",
                format!(
                    "{} of the {} net tax on the {last} return is {}; Arkansas has no higher percentage at higher income",
                    y.prior_year_percent, prior_tax, amount
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_percent, amount)
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        // Exception 3: no liability for a twelve-month year, as a resident
        // all of it.
        let no_liability = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax.0 == 0) || prior.not_required_to_file());
        let resident_opt = prior.full_year_resident();
        let answered = resident_opt.is_some();
        let resident = resident_opt.unwrap_or_default();
        if no_liability && !answered {
            a.notes.ask(
                PositionKind::Residency,
                format!("Did you live in Arkansas all of {last}?"),
                "If so, with no Arkansas tax liability for that year, there's no penalty.",
            );
        }

        // Exception 2: a return filed and paid by January 31. January 31,
        // 2027 is a Sunday; the instructions print January 31 and say
        // nothing of moving it, so it is not moved.
        let filed = f.return_filed();
        a.filed_early = !filed.is_zero() && !filed.after(y.file_and_pay_by);
        let moved = y.calendar.next_business_day(y.file_and_pay_by);
        if filed == moved && moved != y.file_and_pay_by {
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "The instructions don't say if the {} deadline moves off a {}. Filing and paying on {}, the next business day, counts as late.",
                    short_date(y.file_and_pay_by, y.year),
                    y.file_and_pay_by.weekday(),
                    short_date(moved, y.year)
                ),
            );
        }

        if no_liability && answered && resident {
            a.exempt = true;
            let exception =
                estimated::new_exception("No Arkansas tax liability last year").build()?;
            rb.required(false, format!("You had no Arkansas tax liability for {last}, a full year as an Arkansas resident. No estimated payments are needed for {}, and no penalty can arise.", y.year)).exempt(exception);
            a.notes.rule(
                "AR2210 Instructions (2025), Exception 3",
                format!(
                    "the {last} return showed no liability for a full 12 months as a resident, so there is no penalty for {}",
                    y.year
                ),
            );
            if prior.not_required_to_file() {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!("Did you have any Arkansas tax liability for {last}?"),
                    "The figures assume none for now, since you weren't required to file that year.",
                );
            }
        } else if a.filed_early {
            a.exempt = true;
            let exception =
                estimated::new_exception("Return filed and paid by the end of January").build()?;
            inn.filed_early = true;
            rb.required(
                false,
                format!(
                    "You filed your {} return and paid the tax on {}, by {}. No penalty arises for the year.",
                    y.year,
                    short_date(filed, y.year),
                    short_date(y.file_and_pay_by, y.year)
                ),
            )
            .exempt(exception);
            a.notes.rule(
                "AR2210 (2025), Part I; AR2210 Instructions (2025), Exception 2",
                format!(
                    "the return was filed and paid on {}, by {}, so Exception 2 applies and Part II and Part III are not completed",
                    filed.long(),
                    y.file_and_pay_by.long()
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target needs your {} Arkansas tax, as your {last} return can't set one.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your target is {} of your {last} Arkansas net tax, {}. Your withholding of {} already reaches it. No estimated payments are needed, whatever your {} tax turns out to be.",
                    y.prior_year_percent,
                    target.whole(),
                    withheld.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {} of your {last} Arkansas net tax, {}. That keeps you clear of a penalty whatever your {} tax turns out to be. After {} of withholding, you need {} of estimated payments. A quarter is due by each date.",
                    y.prior_year_percent,
                    target.whole(),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else if tax - withheld <= y.threshold {
            a.exempt = true;
            let exception =
                estimated::exception_at_most("Tax less withholding", tax - withheld, y.threshold)?;
            let mut at_most = format!(" That is {} or less.", y.threshold.whole());
            if tax < withheld {
                at_most = String::new();
            }
            rb.required(
                false,
                format!(
                    "Arkansas net tax of {} less {} withheld {}.{} No estimated payments are needed, and no penalty arises.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld),
                    at_most
                ),
            )
            .exempt(exception);
            a.notes.rule(
                "AR2210 (2025), line 4",
                format!(
                    "{} of net tax less {} withheld is {}, {} or less, so the form stops and there is no penalty",
                    tax,
                    withheld,
                    tax - withheld,
                    y.threshold
                ),
            );
            if tax - withheld == y.threshold {
                inn.threshold = true;
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "AR2210 line 4 says tax less withholding of exactly {} needs no payments. The AR1000ES and AR2210 instructions differ, and the figures follow line 4.",
                        y.threshold.whole()
                    ),
                );
            }
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Arkansas withholding of {} meets the {} required for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Arkansas net tax of {} less {} of withholding leaves {}. That is more than {}, and withholding is short of the {} required. You need {} of estimated payments, a quarter by each date.",
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

        // These answers change the target. An exception sets the target, so
        // they are asked only when none applies.
        if !a.exempt && !has_prior {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Arkansas return, and what was its net tax?"),
                format!(
                    "With it, {} of last year's net tax may set a target below {} of this year's.",
                    y.prior_year_percent, y.this_year_percent
                ),
            );
        }
        if !a.exempt && !tax_known {
            let mut effect =
                "Without it, the target can only come from last year's tax.".to_string();
            if !prior_opt.available() {
                effect = format!(
                    "Without it, there's no target, because your {last} return can't set one."
                );
            }
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Arkansas net tax to be?", y.year),
                effect,
            );
        }

        // What the form allows that the restatement of the law does not,
        // where it decides the target.
        if let Some(c) = a.requirement.chosen()
            && c.basis() == Basis::PriorYearTax
        {
            if prior.months() != 12 {
                inn.prior_figure = true;
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "Line 5 of the AR2210 takes your {last} return of {} months, as do the figures. The instructions count only a 12-month year.",
                        prior.months()
                    ),
                );
            }
            if prior_tax.0 == 0 {
                inn.prior_figure = true;
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "The AR2210's line 6 lets your {last} return, which showed no tax, set a $0 target. The figures follow it, though the instructions need a return with a liability."
                    ),
                );
            }
        }

        // AR2210 line 7: a quarter on each date, whenever the requirement
        // arose.
        let cum = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cum);
        if !a.exempt {
            a.notes.rule(
                "AR2210 (2025), line 7; AR1000ES (2026), vouchers",
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
            let met = f.requirement_first_met();
            if met.after(a.dues[0]) {
                inn.late_start = true;
                a.notes.rule(
                    "AR2210 (2025), line 7",
                    format!(
                        "the requirement was first met on {}, but the form still requires a quarter on every date; the divergence on a requirement first met after April 15 says what the law reads",
                        met.long()
                    ),
                );
            }
        }

        // AR2210A: the annualised installments, against a quarter of 90% of
        // this year's tax rather than of the required annual payment.
        if f.elections().annualised() && !a.exempt {
            if !tax_known {
                a.notes.rule(
                    "AR2210A (2025), lines 24 to 26",
                    "the annualised method measures each period against a quarter of 90% of this year's net tax, which was not given, so the regular method is used",
                );
            } else {
                let regular_cum =
                    estimated::cumulative_shares(y.this_year_percent.of(tax), &y.shares)?;
                let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
                let ann = estimated::annualised(
                    &taxes,
                    &y.annualised_percentages,
                    &estimated::increments(&regular_cum),
                )?;
                a.notes.rule(
                    "AR2210A (2025), lines 19 to 30; AR2210 Instructions (2025), Exception 6",
                    format!(
                        "under the annualised method the installments are {}, {}, {} and {}: {}, {}, {} and {} of the annualised tax less earlier installments, and never more than a quarter of 90% of this year's tax plus any shortfall carried forward",
                        ann[0],
                        ann[1],
                        ann[2],
                        ann[3],
                        y.annualised_percentages[0],
                        y.annualised_percentages[1],
                        y.annualised_percentages[2],
                        y.annualised_percentages[3]
                    ),
                );
                (a.installments, a.annualised) = (ann, true);
            }
        }

        if f.fish_share().0 > 0 && i64::from(f.fish_share().0 + f.farm_share().0) * 3 >= 20_000 {
            a.notes.rule(
                "AR1000ES (2026), Who Must File a Declaration, Exception",
                "the later dates are for income from farming only; fishing is not mentioned, so the general rules apply",
            );
        }

        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        // Which divergences change these figures turns on the payments and
        // the shortfalls.
        if !a.exempt {
            let last_due = a.dues[a.dues.len() - 1];
            for p in f.payments() {
                if p.from_prior_year() || !p.on().after(last_due) {
                    inn.payments = true;
                }
            }
            let (ledger, _) = self.ledger(f, &a)?;
            for l in &ledger {
                inn.factor = inn.factor || l.shortfall().0 > 0;
            }
        }
        let divergences = self.divergences(inn)?;
        a.notes.diverges(&divergences);
        if a.annualised {
            let ds = self.annualised_divergence(inn)?;
            a.notes.diverges(&ds);
        }
        Ok(a)
    }

    /// Applies the credits to the installments.
    ///
    /// AR2210 line 8 and the instructions' example treat withholding and
    /// every payment made by the last due date as paid in four equal parts
    /// on the due dates. A payment after the last due date counts on the day
    /// it was made and pays the oldest shortfall first.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let y = &self.y;
        let last = a.dues[a.dues.len() - 1];
        let mut spread = f.withheld_total();
        let mut estimates = USCents(0);
        let mut after = Vec::new();
        for p in f.payments() {
            if p.from_prior_year() || !p.on().after(last) {
                spread += p.amount();
                estimates += p.amount();
            } else {
                after.push(p);
            }
        }
        let cum = estimated::cumulative_shares(spread, &y.shares)?;
        let mut credits: Vec<Payment> = Vec::new();
        for (i, amount) in estimated::increments(&cum).into_iter().enumerate() {
            if amount.0 == 0 {
                continue;
            }
            credits.push(
                estimated::new_payment()
                    .on(a.dues[i])
                    .amount(amount)
                    .build()?,
            );
        }
        credits.extend(after);
        let ledger = estimated::apply(&a.dues, &a.installments, &credits)?;

        let mut notes = Notes::default();
        notes.rule(
            "AR2210 (2025), line 8; AR2210 Instructions (2025), Penalty Computation Example",
            format!(
                "the {} withheld and {} of estimated payments made by {} are treated as paid in four equal parts on the due dates, whenever they were made; a payment after that counts on its day and first pays the oldest underpayment",
                f.withheld_total(),
                estimates,
                last.long()
            ),
        );
        if estimates.0 > 0 {
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "AR2210 line 8, its example and the law differ on when estimates count as paid. As in the example, the figures spread your {} evenly over the four dates.",
                    estimates.whole()
                ),
            );
        }
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "AR2210 (2025), line 8",
                "Arkansas has no election to credit withholding on the dates it was withheld, so it is spread evenly",
            );
        }
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

    fn charge(
        &self,
        a: &Assessment,
        amount: USCents,
        from: Date,
        to: Date,
    ) -> Result<Vec<PenaltyPart>, Error> {
        let y = &self.y;
        if a.annualised {
            let (parts, gap) = estimated::daily_factor(amount, from, to, &y.annualised_factor)?;
            if !gap.is_zero() {
                return Err(Error::Invalid(format!(
                    "arkansastaxcalc: AR2210A's factor does not cover {gap}"
                )));
            }
            return Ok(parts);
        }
        let p = daily_to_dollar(amount, from, to, y.rate, y.year_days, y.rate_source)?;
        Ok(vec![p])
    }
}

impl EstimatedTax for ArkansasEstimated {
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
                    "with no {} Arkansas return to measure against, there is no target to schedule",
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
                "arkansastaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Arkansas return to measure against, there is no target to figure a penalty from",
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
                for p in self.charge(&a, piece.amount(), l.due(), to)? {
                    lb.part(p);
                }
            }
            let line = lb.days(last_day.days_since(l.due())).build()?;
            pb.line(line);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        // AR2210 Instructions, Exceptions 4 and 5: the federal pair, decided
        // by the Commissioner.
        if let Some(o) = estimated::standard_waiver(f, "the Commissioner", 62)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }

        if !a.tax_known {
            notes.rule(
                "AR2210 (2025), line 6",
                format!(
                    "this year's tax was not given, so the penalty is figured against {} of the {} net tax alone; the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    y.prior_year_percent,
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "AR2210 (2025), line 15",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        if a.annualised {
            let factor = y
                .annualised_factor
                .periods()
                .first()
                .map(|p| p.factor().to_string())
                .unwrap_or_default();
            notes.rule(
                "AR2210A (2025), lines 40 to 46",
                format!(
                    "each shortfall is charged {} for each day from its due date until paid or {}, rounded to the whole dollar as the AR2210 example rounds; the penalty to {} is {}",
                    factor,
                    y.stop.long(),
                    stop.long(),
                    p.total()
                ),
            );
        } else {
            notes.rule(
                "AR2210 (2025), lines 15 to 17; AR2210 Instructions (2025), Penalty Computation Example",
                format!(
                    "each shortfall is charged the days from its due date until paid or {}, divided by {}, times {}, rounded to the whole dollar as the example rounds each charge; the penalty to {} is {}",
                    y.stop.long(),
                    y.year_days,
                    y.rate,
                    stop.long(),
                    p.total()
                ),
            );
        }
        if p.total().0 > 0 && !waiver_answered {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking the Commissioner to excuse the penalty?",
                "The Commissioner can excuse it for a casualty, disaster or other unusual event. Retiring after 62 or becoming disabled, with reasonable cause, also counts.",
            );
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    /// Written by hand and checked against research/estimated-payments/ar.md.
    fn explanation(&self) -> Explanation {
        estimated::new_explanation(
            "Arkansas expects estimated payments if your tax after withholding tops $1,000. The penalty is a flat 10% a year on each quarter's shortfall while it's unpaid.",
        )
        .paragraph("You avoid the penalty if withholding and estimated payments reach a target. It is the smaller of 90% of this year's Arkansas tax and 100% of last year's. There is no 110% tier, so last year's tax works at any income. There's no penalty if tax less Arkansas withholding is $1,000 or less this year. There is also none if you had no Arkansas tax liability last year. That year must be a full year in which you lived in Arkansas throughout.")
        .paragraph("A quarter is due April 15, June 15 and September 15, and January 15 next year. The department's penalty form spreads withholding evenly across the four dates. It does the same with estimated payments made by January 15. Both count that way whenever they were actually made. So paying late in the year still counts toward the earlier dates. Filing your return and paying the tax by January 31 removes the year's penalty.")
        .paragraph("The penalty works like interest at a fixed 10% a year. Each shortfall is charged for each day from its due date until it's paid. The charge stops at April 15 of the next year if that comes first. A payment goes to the oldest shortfall first. If your income came unevenly, Form AR2210A can lower the early installments. The Commissioner can excuse the penalty for a casualty or disaster. The Commissioner can also excuse it if you retired after 62 or became disabled. That needs reasonable cause.")
        .paragraph("The department's own documents disagree on a few details. One is whether exactly $1,000 is inside the rules. Another is whether an estimated payment counts from the day it's made.")
        .versus_federal("Arkansas differs from federal in five ways that can change a number. The penalty is a fixed 10% a year rather than the quarterly federal rate. There's no 110% tier. Estimated payments count as made evenly through the year. Filing and paying by January 31 removes the penalty for the whole year. The no-liability exception needs Arkansas residence all of last year.")
        .source("AR1000ES, Estimated Tax Declaration Vouchers and Instructions for Tax Year 2026")
        .source("AR2210, Penalty for Underpayment of Estimated Tax, and its instructions, 2025")
        .source("AR2210A, Annualized Penalty for Underpayment of Estimated Income Tax, 2025")
        .source("DFA Subject 603, Penalty for Underpayment of Estimated Tax (rev. 12/22/2020)")
        .build()
        .unwrap_or_default()
    }
}

/// AR2210 line 16 as the instructions' example works it: the amount × days
/// ÷ year_days × rate, rounded to the whole dollar, half up. The example
/// charges each piece of a shortfall on its own, so a quarter paid in two
/// pieces is two charges, each rounded.
///
/// [`estimated::simple_daily`] rounds to the cent and divides by 366 in a
/// leap year, and neither is what this form does.
fn daily_to_dollar(
    amount: USCents,
    from: Date,
    to: Date,
    rate: Rate,
    year_days: i64,
    source: &str,
) -> Result<PenaltyPart, Error> {
    let days = to.days_since(from);
    let n = BigInt::from(amount.0) * BigInt::from(days) * BigInt::from(rate.0);
    let charge = Rounding::RoundToDollar.div(&n, &BigInt::from(10_000 * year_days));
    estimated::new_penalty_part(PartKind::Interest, amount, rate)
        .span(from, to)
        .days(days)
        .charge(charge)
        .source(source)
        .build()
}

/// "Aug 31" inside the tax year and "Jan 15, 2027" outside it.
fn short_date(d: Date, year: i64) -> String {
    let month = d.month().to_string();
    let m: String = month.chars().take(3).collect();
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}
