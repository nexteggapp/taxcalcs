//! Kansas's estimated tax rules, K.S.A. 79-32,101 to 79-32,103 and
//! 79-32,107, as the shared [`EstimatedTax`] interface.
//!
//! The figure follows the Department of Revenue's forms: the 2026 K-40ES for
//! who must pay and when, and Schedule K-210 (2025) for the penalty, which is
//! the latest published. The result's Divergences say where the statutes
//! read otherwise. K.S.A. 79-32,107 was last amended by L. 2024, ch. 81,
//! effective July 1, 2024, and the 2026 composite listing shows no later
//! change to any of these sections, so neither form predates the law it
//! describes.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Payment, Penalty,
    RateSeries, Requirement, TargetOption, TargetOptionBuilder, WaiverKind,
};
use money::{DOLLAR, Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

const JURISDICTION: estimated::Jurisdiction = estimated::KANSAS;

/// The facts and positions Kansas's rules read that no other jurisdiction
/// does. Carry them in [`estimated::FactsBuilder::state`].
///
/// For Kansas, [`estimated::Figures::tax`] is the total tax balance, Form
/// K-40 line 18: the tax after the nonrefundable credits, before
/// withholding, the refundable credits and the credit for tax paid on the
/// K-120S.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    pass_through: USCents,
    refundable: USCents,

    short_prior_year: bool,
    short_prior_year_answered: bool,
    late_start: bool,
    late_start_answered: bool,
    january_moves: bool,
    january_moves_answered: bool,
}

impl EstimatedFacts {
    /// The credit for tax paid on the K-120S by an electing pass-through
    /// entity, Form K-40 line 25. Schedule K-210 line 7 treats it as paid a
    /// quarter on each due date, as it treats withholding.
    pub fn pass_through_credit(&self) -> USCents {
        self.pass_through
    }

    /// The refundable credits on Form K-40 lines 22 and 23. They count
    /// against the $500 threshold and not toward the installments, because
    /// Schedule K-210 line 8 does not add them.
    pub fn refundable_credits(&self) -> USCents {
        self.refundable
    }

    /// The caller's position on whether last year's tax counts for the
    /// prior-year test when last year's return covered fewer than twelve
    /// months, and whether the caller took one: `(use, answered)`. The
    /// K-40ES requires a twelve-month return; no statute or regulation found
    /// does.
    pub fn short_prior_year(&self) -> (bool, bool) {
        (self.short_prior_year, self.short_prior_year_answered)
    }

    /// The caller's position on whether a filer who first meets the
    /// requirement after April 1 owes the fewer, larger installments of
    /// K.S.A. 79-32,103(a)(2) to (4), rather than the quarter on each date
    /// that Schedule K-210 prints, and whether the caller took one:
    /// `(use, answered)`.
    pub fn late_start_installments(&self) -> (bool, bool) {
        (self.late_start, self.late_start_answered)
    }

    /// The caller's position on whether the January 31 date for filing and
    /// paying in full moves to the next workday when it falls on a weekend,
    /// as January 31, 2027 does, and whether the caller took one:
    /// `(moves, answered)`.
    pub fn january_thirty_first_moves(&self) -> (bool, bool) {
        (self.january_moves, self.january_moves_answered)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts Kansas's extras.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Sets the credit for tax paid on the K-120S.
    pub fn pass_through_credit(&mut self, a: USCents) -> &mut Self {
        self.e.pass_through = a;
        self
    }

    /// Sets the refundable credits.
    pub fn refundable_credits(&mut self, a: USCents) -> &mut Self {
        self.e.refundable = a;
        self
    }

    /// Takes a position on a short prior year.
    pub fn short_prior_year(&mut self, use_: bool) -> &mut Self {
        (self.e.short_prior_year, self.e.short_prior_year_answered) = (use_, true);
        self
    }

    /// Takes a position on the statute's late-start installments.
    pub fn late_start_installments(&mut self, use_: bool) -> &mut Self {
        (self.e.late_start, self.e.late_start_answered) = (use_, true);
        self
    }

    /// Takes a position on whether January 31 moves off a weekend.
    pub fn january_thirty_first_moves(&mut self, moves: bool) -> &mut Self {
        (self.e.january_moves, self.e.january_moves_answered) = (moves, true);
        self
    }

    /// Refuses negative credits.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        if self.e.pass_through.0 < 0 || self.e.refundable.0 < 0 {
            return Err(Error::Invalid(
                "kansastaxcalc: credits cannot be negative".into(),
            ));
        }
        Ok(self.e)
    }
}

/// One tax year's published figures.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// What tax less withholding and credits must reach before estimated
    /// payments are required and the penalty can apply.
    pub(crate) threshold: USCents,

    /// Schedule K-210 lines 2 and 3.
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// The first days of K.S.A. 79-32,102(a)'s later groups: a requirement
    /// first met on or after `later_groups[i]` starts at installment i+2.
    pub(crate) later_groups: Vec<Date>,

    pub(crate) file_and_pay_by: Date,
    pub(crate) stop: Date,
    pub(crate) period_ends: Vec<Date>,
    pub(crate) rates: RateSeries,

    /// The penalty K.S.A. 79-32,107(a) does not add.
    pub(crate) de_minimis: USCents,
}

/// Schedule K-210 lines 5, 7 and 9: 25%, 50%, 75% and 100% by the four
/// dates.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// Schedule K-210 line 10a: the share of each period's annualised tax due by
/// its date, cumulatively.
const ANNUALISED_SHARES: [Rate; 4] = [Rate(2250), Rate(4500), Rate(6750), Rate(9000)];

/// Returns Kansas's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(KansasEstimated { y }))
}

struct KansasEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// The running total needed by each date: Schedule K-210 line 9, or the
    /// lesser of lines 9 and 10a under the annualised method.
    required: Vec<USCents>,

    excused_january: bool,
    extras: EstimatedFacts,
    notes: Notes,
}

/// Go's `%T` of a jurisdiction's extras, for the types a caller is likely to
/// pass by mistake.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<String>() || v.is::<&str>() {
        "string"
    } else if v.is::<i64>() || v.is::<i32>() {
        "int"
    } else {
        "unknown"
    }
}

impl KansasEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "kansastaxcalc: the facts carry {}, not kansastaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "kansastaxcalc: these are Kansas's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "kansastaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or fisher with two-thirds of gross income from farming or fishing makes one payment by January 15 at 66 2/3%, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "kansastaxcalc: Schedule K-210 line 10a period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and the statutes part ways on a rule a
    /// Kansas computation uses, each marked with whether it changes anything
    /// for `f`. Those on last year's return are in
    /// research/estimated-payments/ks.md, under Conflicts resolved; the late
    /// start was found by comparing Schedule K-210 with K.S.A. 79-32,103.
    /// The $1 floor is a penalty rule, reported with the penalty.
    fn divergences(&self, f: &Facts, extras: &EstimatedFacts) -> Result<Vec<Divergence>, Error> {
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let prior = prior.unwrap_or_default();
        let prior_tax = prior.figures().and_then(|pf| pf.tax()).unwrap_or_default();
        let filed_with_tax = has_prior && prior.filed() && prior_tax.0 > 0;
        let short_reading = if extras.short_prior_year().0 {
            Reading::Statute
        } else {
            Reading::Form
        };
        let late_reading = if extras.late_start_installments().0 {
            Reading::Statute
        } else {
            Reading::Form
        };
        let mut out = Vec::new();
        for b in [
            working::new_divergence("No return filed last year")
                .form("With no Kansas return last year, the penalty schedule counts its tax as zero. Then no payments are needed and no penalty arises.",
                    "Schedule K-210 (2025), instructions for lines 1 through 4")
                .statute("Last year's tax counts as zero only if you weren't required to file. If you had to file and didn't, only the 90% and annualized tests apply.",
                    "K.S.A. 79-32,107(b)(1) and (b)(2)")
                .uses(Reading::Form)
                .affects(has_prior && !prior.filed() && !prior.not_required_to_file())
                .clone(),
            working::new_divergence("Last year's tax under $200")
                .form("The penalty schedule uses last year's tax however small. It counts it as zero only when it was zero.",
                    "Schedule K-210 (2025), line 3 and the instructions for lines 1 through 4")
                .statute("Last year's tax counts as zero if it was less than $200.",
                    "K.S.A. 79-32,107(b)(2)")
                .uses(Reading::Form)
                .affects(filed_with_tax && prior_tax < DOLLAR * 200)
                .clone(),
            working::new_divergence("A return for less than 12 months")
                .form("Kansas's estimated tax form uses last year's tax only from a 12-month return. That return must also show tax. The penalty schedule itself states no such condition.",
                    "2026 K-40ES, Estimated Tax Penalty; Schedule K-210 (2025), line 3")
                .statute("The law uses the tax shown on last year's return, with no 12-month condition. It leaves short years to regulations.",
                    "K.S.A. 79-32,107(b)(1); K.S.A. 79-32,101(d), 79-32,102(d), 79-32,103(d)")
                .uses(short_reading)
                .affects(filed_with_tax && prior.months() != 12)
                .clone(),
            working::new_divergence("Needing to pay only after April 1")
                .form("The penalty schedule has everyone pay 25%, 50%, 75% and 100% by the four dates. The worksheet differs for a start after April 15. It has you pay 1/3, 1/2 or all of the year's amount on the later dates.",
                    "Schedule K-210 (2025), lines 9 and 10a; 2026 K-40ES, worksheet line 9")
                .statute("First needing to pay April 2 to June 1 means three equal payments from June 15. From June 2 to September 1, it means two from September 15. After September 1, it means all of it on January 15.",
                    "K.S.A. 79-32,102(a); K.S.A. 79-32,103(a)(2) to (4)")
                .uses(late_reading)
                .affects(estimated::late_start(f.requirement_first_met(), &self.y.later_groups) > 0)
                .clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// The $1 floor of K.S.A. 79-32,107(a), which Schedule K-210 does not
    /// have. It affects a penalty of more than zero and at most $1.
    fn floor_divergence(&self, total: USCents) -> Result<Divergence, Error> {
        Ok(working::new_divergence("A penalty of $1 or less")
            .form(
                "The penalty schedule has no minimum: the penalty is entered whatever its amount.",
                "Schedule K-210 (2025), lines 17 and 18",
            )
            .statute(
                "The penalty is not added if it does not exceed $1.",
                "K.S.A. 79-32,107(a)",
            )
            .uses(Reading::Form)
            .affects(total.0 > 0 && total <= self.y.de_minimis)
            .build()?)
    }

    /// A date as a person reads it: "Aug 31" in the tax year, and
    /// "Jan 15, 2027" outside it.
    fn short_date(&self, d: Date) -> String {
        if d.year() == self.y.year {
            return d.format("Jan 2");
        }
        d.format("Jan 2, 2006")
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment {
            extras,
            ..Assessment::default()
        };
        let divergences = self.divergences(f, &extras)?;
        a.notes.diverges(&divergences);
        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        let last = y.year - 1;
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let credited = withheld + extras.pass_through;
        let owed = tax - credited - extras.refundable;

        // Schedule K-210 line 2: 90% of this year's tax.
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
                "K.S.A. 79-32,107(a); Schedule K-210 (2025), lines 1 and 2",
                format!(
                    "{} of this year's Kansas tax of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // Schedule K-210 line 3: last year's tax.
        let prior = f.prior_year();
        let has_prior = prior.is_some();
        let prior = prior.unwrap_or_default();
        let prior_tax = prior.figures().and_then(|pf| pf.tax()).unwrap_or_default();
        let (short_use, short_answered) = extras.short_prior_year();
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        let mut no_liability = false;
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Kansas return"))
        } else if !prior.filed() {
            no_liability = true;
            a.notes.rule(
                "Schedule K-210 (2025), instructions for lines 1 through 4",
                format!(
                    "no {last} Kansas return was filed, so line 3 is zero, and so is the required payment"
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
        } else if prior_tax.0 == 0 {
            no_liability = true;
            a.notes.rule(
                "Schedule K-210 (2025), instructions for lines 1 through 4; K.S.A. 79-32,107(b)(2)",
                format!(
                    "the {last} return showed no Kansas tax, so line 3 is zero, and so is the required payment"
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
        } else if prior.months() != 12 && !short_use {
            unavailable(format!(
                "Your {last} return covered {} months, and Kansas's form asks for a 12-month return",
                prior.months()
            ))
        } else {
            let amount = y.prior_year_share.of(prior_tax);
            a.notes.rule(
                "K.S.A. 79-32,107(b)(1); Schedule K-210 (2025), line 3; 2026 K-40ES, Estimated Tax Penalty",
                format!(
                    "{} of the {} tax on the {last} return is {amount}",
                    y.prior_year_share, prior_tax
                ),
            );
            if prior.months() != 12 {
                a.notes.rule(
                    "K.S.A. 79-32,107(b)(1)",
                    format!(
                        "by the position given in kansastaxcalc.EstimatedFacts, the {}-month {last} return is used, though the K-40ES asks for 12 months",
                        prior.months()
                    ),
                );
            }
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, amount)
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }

        let label = |o: &TargetOption| format!("{}, {}", o.label(), o.amount().whole());
        if no_liability {
            a.exempt = true;
            let (name, why) = if prior.filed() {
                (
                    "No Kansas tax last year",
                    format!(
                        "Your {last} Kansas return showed no tax. No estimated payments are needed for {} and no penalty can arise.",
                        y.year
                    ),
                )
            } else {
                (
                    "No Kansas return filed last year",
                    format!(
                        "You filed no {last} Kansas return, so Kansas counts last year's tax as zero. No estimated payments are needed for {} and no penalty can arise.",
                        y.year
                    ),
                )
            };
            let ex = estimated::new_exception(name).build()?;
            rb.required(false, why).exempt(ex);
        } else if tax_known && owed < y.threshold {
            a.exempt = true;
            let ex =
                estimated::exception_below("Tax less withholding and credits", owed, y.threshold)?;
            let why = if owed.0 < 0 {
                format!(
                    "Your Kansas tax is {}, and withholding and credits are {}. That {}, so no estimated payments are needed.",
                    tax.whole(),
                    (tax - owed).whole(),
                    estimated::leaves(owed)
                )
            } else if owed.0 == 0 {
                format!(
                    "Your withholding and credits of {} cover your Kansas tax of {}. No estimated payments are needed.",
                    (tax - owed).whole(),
                    tax.whole()
                )
            } else if owed.whole() == y.threshold.whole() {
                format!(
                    "Your Kansas tax less withholding and credits is under {}. No estimated payments are needed.",
                    y.threshold.whole()
                )
            } else {
                format!(
                    "Your Kansas tax is {}, and withholding and credits are {}. That leaves {}, under {}, so no estimated payments are needed.",
                    tax.whole(),
                    (tax - owed).whole(),
                    owed.whole(),
                    y.threshold.whole()
                )
            };
            rb.required(false, why).exempt(ex);
            a.notes.rule(
                "K.S.A. 79-32,101(a)(1); Schedule K-210 (2025), Who May Use This Schedule; 2026 K-40ES, worksheet line 8",
                format!(
                    "{tax} of tax less {withheld} of withholding and {} of credits is {owed}, under {}, so there is no penalty",
                    extras.pass_through + extras.refundable,
                    y.threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target is set by your {} Kansas tax or your {last} return. There's no target until you have one of them.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && credited >= target {
            rb.required(
                false,
                format!(
                    "Your target is {}. Your withholding and credits of {} already reach it. No estimated payments are needed whatever your {} tax is.",
                    label(&prior_opt),
                    credited.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {}. It keeps you clear of a penalty whatever your {} tax is. Withholding and credits of {} leave {} to pay in estimated tax.",
                    label(&prior_opt),
                    y.year,
                    credited.whole(),
                    (target - credited).whole()
                ),
            )
            .choose(chosen);
        } else if credited >= target {
            rb.required(
                false,
                format!(
                    "Your Kansas withholding and credits of {} reach your {} target. No estimated payments are needed.",
                    credited.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            let c = if chosen == Basis::PriorYearTax {
                &prior_opt
            } else {
                &this_year
            };
            rb.required(
                true,
                format!(
                    "Your Kansas tax less withholding and credits leaves {}, at least {}. Your target is {}. You need {} of estimated payments.",
                    owed.whole(),
                    y.threshold.whole(),
                    label(c),
                    (target - credited).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        // Under an exception neither return can change the target.
        if !a.exempt {
            if !has_prior {
                a.notes.ask(
                    PositionKind::PriorYearReturn,
                    format!("Did you file a {last} Kansas return, and what tax did it show?"),
                    "If you filed none or it showed no tax, no estimated payments are needed. Otherwise its tax may set a lower target.",
                );
            }
            if has_prior
                && prior.filed()
                && prior_tax.0 != 0
                && prior.months() != 12
                && !short_use
                && !short_answered
            {
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "Kansas's estimated tax form won't let your {}-month {last} return set your target. The law differs, and the figures follow the form.",
                        prior.months()
                    ),
                );
            }
            if !tax_known {
                a.notes.ask(
                    PositionKind::ThisYearTax,
                    format!("What do you expect your {} Kansas tax to be?", y.year),
                    format!(
                        "With it, 90% of this year's tax could set a lower target. Under {} after withholding and credits, no payments are needed.",
                        y.threshold.whole()
                    ),
                );
            }
        }
        if a.exempt || !a.requirement.target_known() {
            a.required = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }

        a.required = self.regular(f, &mut a, target)?;

        // Schedule K-210 line 10a: the lesser of the regular and annualised
        // running totals, column by column.
        if f.elections().annualised() {
            let periods = f.periods();
            let mut ann = vec![USCents(0); periods.len()];
            for (i, p) in periods.iter().enumerate() {
                ann[i] = ANNUALISED_SHARES[i].of(p.annualised_tax());
                a.required[i] = a.required[i].min(ann[i]);
            }
            let ann4 = |i: usize| ann.get(i).copied().unwrap_or_default();
            a.notes.rule(
                "K.S.A. 79-32,107(b)(3); Schedule K-210 (2025), lines 10a and 11",
                format!(
                    "under the annualised method 22.5%, 45%, 67.5% and 90% of each period's annualised tax are {}, {}, {} and {}, \
                     and each date needs the lesser of that and the regular amount: {}, {}, {} and {}",
                    ann4(0),
                    ann4(1),
                    ann4(2),
                    ann4(3),
                    a.required[0],
                    a.required[1],
                    a.required[2],
                    a.required[3]
                ),
            );
        }

        // January 31. It is a Sunday in 2027.
        let mut pay_by = y.file_and_pay_by;
        let (moves, moves_answered) = extras.january_thirty_first_moves();
        if moves {
            pay_by = y.calendar.next_business_day(pay_by);
        }
        let filed = f.return_filed();
        if !filed.is_zero() {
            if !filed.after(pay_by) {
                a.excused_january = true;
                a.required[3] = a.required[2];
                a.notes.rule(
                    "K.S.A. 79-32,101(c); 2026 K-40ES, When to File; Schedule K-210 (2025), Who May Use This Schedule",
                    format!(
                        "the return was filed and paid on {}, by {}, so no January installment is required",
                        filed.long(),
                        pay_by.long()
                    ),
                );
            } else if !moves_answered
                && !filed.after(y.calendar.next_business_day(y.file_and_pay_by))
            {
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "Kansas hasn't said if its {} file-and-pay date moves off a {}. The figures count your {} return as late, so January's payment is due.",
                        self.short_date(y.file_and_pay_by),
                        y.file_and_pay_by.weekday(),
                        self.short_date(filed)
                    ),
                );
            }
        }
        Ok(a)
    }

    /// The running total needed by each date before the annualised method:
    /// a quarter of the target a time, as Schedule K-210 line 9 prints, or
    /// the late start of K.S.A. 79-32,103 when the caller takes that
    /// position.
    fn regular(
        &self,
        f: &Facts,
        a: &mut Assessment,
        target: USCents,
    ) -> Result<Vec<USCents>, Error> {
        let y = &self.y;
        let first_met = f.requirement_first_met();
        let group = estimated::late_start(first_met, &y.later_groups);
        let (use_, answered) = a.extras.late_start_installments();
        let quarterly = estimated::cumulative_shares(target, &quarters())?;
        let quarterly_rule = |a: &mut Assessment| {
            a.notes.rule(
                "K.S.A. 79-32,103(a)(1); Schedule K-210 (2025), line 9; 2026 K-40ES, When to File",
                format!(
                    "25%, 50%, 75% and 100% of {target}, {}, {}, {} and {}, are needed by {}, {}, {} and {}",
                    quarterly[0],
                    quarterly[1],
                    quarterly[2],
                    quarterly[3],
                    a.dues[0].long(),
                    a.dues[1].long(),
                    a.dues[2].long(),
                    a.dues[3].long()
                ),
            );
        };
        if group == 0 {
            if use_ && first_met.is_zero() {
                a.notes.ask(
                    PositionKind::RequirementFirstMet,
                    format!(
                        "When did you first expect {} or more of Kansas tax for {}?",
                        y.threshold.whole(),
                        y.year
                    ),
                    "It means tax after withholding and credits. Until then, the figures assume January 1, so a quarter is due on every date.",
                );
            }
            quarterly_rule(a);
            return Ok(quarterly);
        }
        if use_ {
            let n = a.dues.len() as i64 - group;
            let out = estimated::equal_shares(target, a.dues.len() as i64, group)?;
            a.notes.rule(
                "K.S.A. 79-32,102(a), 79-32,103(a)(2) to (4); 2026 K-40ES, worksheet line 9",
                format!(
                    "the requirement was first met on {}, so nothing is needed before {} and {target} is paid in {n} equal installments from then: {}, {}, {} and {} by the four dates",
                    first_met.long(),
                    a.dues[group as usize].long(),
                    out[0],
                    out[1],
                    out[2],
                    out[3]
                ),
            );
            return Ok(out);
        }
        quarterly_rule(a);
        a.notes.rule(
            "Schedule K-210 (2025), line 9",
            format!(
                "the requirement was first met on {}; Schedule K-210 prints the same quarters for every filer, so they are used",
                first_met.long()
            ),
        );
        if !answered {
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "By law, first needing to pay on {} means fewer, larger installments. The penalty schedule differs, and the figures follow its 25% by every date.",
                    self.short_date(first_met)
                ),
            );
        }
        Ok(quarterly)
    }

    /// What Schedule K-210 line 8 counts, dated: withholding and the credit
    /// for tax paid on the K-120S a quarter on each due date, and estimated
    /// payments on the days they were paid.
    fn credits(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Payment>, Notes), Error> {
        let mut notes = Notes::default();
        let mut out = estimated::credits(f, &a.dues, &quarters())?;
        let p = a.extras.pass_through;
        if p.0 > 0 {
            let cum = estimated::cumulative_shares(p, &quarters())?;
            for (i, inc) in estimated::increments(&cum).into_iter().enumerate() {
                out.push(estimated::new_payment().on(a.dues[i]).amount(inc).build()?);
            }
        }
        notes.rule(
            "Schedule K-210 (2025), lines 5 to 8",
            format!(
                "the {} withheld and the {} credit for tax paid on the K-120S are treated as paid a quarter on each due date, whenever they were actually paid; \
                 estimated payments count on the day they were paid",
                f.withheld_total(),
                a.extras.pass_through
            ),
        );
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "Schedule K-210 (2025), line 5",
                "Kansas has no election to credit withholding on the dates it was withheld, so it is spread evenly",
            );
        }
        if let Some(p) = f.payments().iter().find(|p| p.from_prior_year()) {
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "Kansas doesn't say when your {} overpayment from last year counts as paid. The figures count it on the first due date, {}, as federal rules do.",
                    p.amount().whole(),
                    self.short_date(a.dues[0])
                ),
            );
        }
        Ok((out, notes))
    }
}

/// The total of the credits paid on or before `d`.
fn credited_by(cs: &[Payment], d: Date) -> USCents {
    cs.iter()
        .filter(|c| !c.on().after(d))
        .map(|c| c.amount())
        .sum()
}

/// The due dates with what was needed and credited by each.
fn installments(a: &Assessment, credits: &[Payment]) -> Result<Vec<Installment>, Error> {
    let zero = USCents(0);
    let mut out = Vec::with_capacity(a.dues.len());
    let mut before = zero;
    for (i, due) in a.dues.iter().enumerate() {
        let paid = credited_by(credits, *due);
        let short = (a.required[i] - paid).max(zero);
        // What is still unpaid of the earlier dates is that date's
        // shortfall, not this one's.
        let own = (short - (before - paid).max(zero)).max(zero);
        let mut b = estimated::new_installment(i as i64 + 1, *due);
        b.needed_by(a.required[i]).credited_by(paid).shortfall(own);
        if a.excused_january && i == 3 {
            b.excused();
        }
        out.push(b.build()?);
        before = a.required[i];
    }
    Ok(out)
}

/// Schedule K-210 Part II. Each due date is a column: what was still short
/// in total on that date (line 11) is charged by the day, falling as later
/// credits pay it, until the next due date, or for the last column until
/// `stop`. A column's days in each rate period are charged at that period's
/// rate and rounded to the cent.
///
/// The returned date is the first day the series has no rate for, when any
/// column runs into it, and the zero Date otherwise.
///
/// `req` and `schedule` are what the penalty was figured from, for its
/// reason.
#[allow(clippy::too_many_arguments)]
fn column_penalty(
    dues: &[Date],
    required: &[USCents],
    credits: &[Payment],
    stop: Date,
    rates: &RateSeries,
    waiver: WaiverKind,
    req: &Requirement,
    schedule: &[Installment],
) -> Result<(Penalty, Date), Error> {
    let mut pb = estimated::new_penalty(stop);
    pb.waiver(waiver).because(req.clone(), schedule);
    let mut unrated = Date::default();
    for (i, due) in dues.iter().enumerate() {
        let due = *due;
        if !due.before(stop) {
            break;
        }
        let mut end = stop;
        if i + 1 < dues.len() && dues[i + 1].before(end) {
            end = dues[i + 1];
        }
        let mut owed = required[i] - credited_by(credits, due);
        if owed.0 <= 0 {
            continue;
        }
        let mut lb = estimated::new_penalty_line(i as i64 + 1, due, owed);
        let mut cur = due;
        let mut stops = paid_between(credits, due, end);
        stops.push(end);
        for d in stops {
            let (parts, gap) = estimated::simple_daily(owed, cur, d, rates)?;
            if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                unrated = gap;
            }
            for p in parts {
                lb.part(p);
            }
            cur = d;
            owed = required[i] - credited_by(credits, d);
            if owed.0 <= 0 {
                break;
            }
        }
        pb.line(lb.days(cur.days_since(due)).build()?);
    }
    Ok((pb.build()?, unrated))
}

/// The days after `from` and before `to` on which a credit was paid,
/// earliest first.
fn paid_between(cs: &[Payment], from: Date, to: Date) -> Vec<Date> {
    let mut out: Vec<Date> = Vec::new();
    for c in cs {
        let d = c.on();
        if d.after(from) && d.before(to) && !out.contains(&d) {
            out.push(d);
        }
    }
    out.sort_by_key(|d| d.days());
    out
}

impl EstimatedTax for KansasEstimated {
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
                    "with no {} Kansas return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (credits, notes) = self.credits(f, &a)?;
        let out = installments(&a, &credits)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "kansastaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Kansas return to measure against, there is no target to figure a penalty from",
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
        let (credits, mut notes) = self.credits(f, &a)?;
        let schedule = installments(&a, &credits)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            let floor = self.floor_divergence(USCents(0))?;
            let mut fnotes = Notes::default();
            fnotes.diverges(&[floor]);
            return Ok(working::new_result(p)
                .with(&a.notes)
                .with(&fnotes)
                .build()?);
        }
        let (mut p, unrated) = column_penalty(
            &a.dues,
            &a.required,
            &credits,
            stop,
            &y.rates,
            waiver,
            &a.requirement,
            &schedule,
        )?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "Schedule K-210 (2025), lines 9 and 11",
                format!(
                    "this year's Kansas tax was not given, so the penalty is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "K.S.A. 79-32,107(a); Schedule K-210 (2025), instructions for line 14",
                format!(
                    "the return was filed with the whole balance paid on {}, so the penalty stops that day",
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
            "K.S.A. 79-32,107(a), 79-2968; Schedule K-210 (2025), Part II",
            format!(
                "at each due date the total still short is charged {rate} a year, by the day over 365, until the next due date or until paid, and after January 15 until paid or {}; \
                 each charge is rounded to the cent; the penalty to {} is {}",
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
                    "The Kansas rate for the days after {} isn't published yet.",
                    unrated.add_days(-1).long()
                ),
            );
        }
        let t = p.total();
        if t.0 > 0 && t <= y.de_minimis {
            notes.rule(
                "K.S.A. 79-32,107(a)",
                format!(
                    "the penalty of {t} does not exceed {}, and the statute does not add a penalty that small; Schedule K-210 has no such floor, so the figure shows it",
                    y.de_minimis
                ),
            );
        }
        let floor = self.floor_divergence(p.total())?;
        notes.diverges(&[floor]);
        if p.total().0 > 0 && waiver_answered.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking Kansas to waive or reduce your penalty for reasonable cause?",
                "Kansas hasn't said if the law's reasonable-cause waiver reaches this penalty. The penalty shown is before any waiver.",
            );
        } else if waiver_answered.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "K.S.A. 79-32,107(g)",
                format!(
                    "Kansas publishes no waiver for {waiver}; the secretary can waive for reasonable cause, and the penalty shown is before any waiver"
                ),
            );
            if p.total().0 > 0 {
                // No Kansas document has a casualty, disaster, retirement or
                // disability waiver, and § 79-32,107(g) doesn't say whether
                // its reasonable-cause waiver reaches the subsection (a)
                // penalty.
                p = p
                    .to_builder()
                    .waiver_outcome(estimated::waiver_undecided(
                        "Kansas has no casualty, disaster, retirement or disability waiver. It hasn't said if the secretary's reasonable-cause waiver covers this penalty.",
                    ))
                    .build()?;
                notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    "Kansas law lets the department waive a penalty for reasonable cause. It doesn't say whether that reaches the estimated tax penalty.",
                );
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        kansas_explanation()
    }
}

/// Written by hand and checked against research/estimated-payments/ks.md.
fn kansas_explanation() -> Explanation {
    estimated::new_explanation(
        "You pay estimated tax once Kansas tax less withholding and credits reaches $500. The penalty is interest on what was short at each quarter. The rate is set for each calendar year.",
    )
    .paragraph("Your target is 90% of this year's Kansas tax or 100% of last year's, if smaller. Withholding and estimated payments should reach it a quarter at a time. If they do, you're clear of the penalty. There's no 110% tier for higher incomes. If you filed no Kansas return last year, or it showed no tax, no penalty arises. Kansas's penalty schedule counts last year's tax as zero in either case. The law is narrower. It gives zero only if no return was required, or last year's tax was under $200.")
    .paragraph("Four equal quarters are due April 15, June 15, September 15 and next January 15. A date on a weekend or legal holiday is due the next workday instead. Withholding counts as paid evenly across the four dates, whenever it was taken. Filing and paying in full by January 31 replaces the January payment.")
    .paragraph("The penalty is figured on Schedule K-210. At each date, interest is charged by the day on the total still short. It runs until the next date or until it's paid. After January 15, it runs until it's paid or April 15. Each year's rate is the prior July's federal underpayment rate plus one point. It is 8% for 2026. If your income came unevenly, the annualized method can lower early quarters.")
    .versus_federal("Unlike federal, the threshold is $500, not $1,000, and there's no 110% tier. No return or no tax last year means no penalty, with no federal full-year test. The rate is set once a year at the federal rate plus one point. Withholding can't be credited on the dates it was taken. Kansas has no casualty, retirement or disability waiver.")
    .source("K.S.A. 79-32,101, 79-32,102, 79-32,103, 79-32,107 and 79-2968")
    .source("Kansas Department of Revenue, K-40ES, Kansas 2026 Individual Estimated Tax (rev. 8-26-25)")
    .source("Kansas Department of Revenue, Schedule K-210, Kansas Underpayment of Estimated Tax, 2025, and its instructions (rev. 9-15-25)")
    .source("Kansas Department of Revenue, 2025 Kansas Individual Income Tax booklet, Form K-40 lines 18 to 31")
    .build()
    .unwrap_or_default()
}
