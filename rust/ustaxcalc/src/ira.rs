//! Traditional and Roth IRA contributions: the § 219 deduction and the
//! limit the two kinds share.

use crate::{
    DOLLAR, Error, FilingStatus, MARRIED_FILING_JOINTLY, MARRIED_FILING_SEPARATELY, PositionKind,
    SURVIVING_SPOUSE, USCents, invalid,
};

/// One person's traditional and Roth IRA contributions for a year, and the
/// facts 26 U.S.C. § 219 reads to decide how much of the traditional one is
/// deductible.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct IraContribution {
    amount: USCents,
    roth: USCents,
    catch_up: bool,
    covered: bool,
    covered_answered: bool,
    spouse_covered: bool,
    spouse_answered: bool,
    lived_apart: bool,
}

impl IraContribution {
    /// The traditional contribution for the year.
    pub fn amount(&self) -> USCents {
        self.amount
    }

    /// The Roth contribution for the year.
    pub fn roth(&self) -> USCents {
        self.roth
    }

    /// Reports a contributor who is 50 or older by the end of the year.
    pub fn catch_up(&self) -> bool {
        self.catch_up
    }

    /// Whether the contributor is covered by a retirement plan at work, as
    /// § 219(g)(5) defines it, and whether the caller said: Go's
    /// `(covered, answered)`.
    pub fn active_participant(&self) -> (bool, bool) {
        (self.covered, self.covered_answered)
    }

    /// Whether the contributor's spouse is covered by a retirement plan at
    /// work, and whether the caller said.
    pub fn spouse_active_participant(&self) -> (bool, bool) {
        (self.spouse_covered, self.spouse_answered)
    }

    /// Reports a married couple filing separately who lived apart all year,
    /// whom § 219(g)(4) treats as unmarried.
    pub fn lived_apart(&self) -> bool {
        self.lived_apart
    }
}

/// Builds an [`IraContribution`].
#[derive(Clone, Debug, Default)]
pub struct IraContributionBuilder {
    c: IraContribution,
}

/// Starts a contribution of `amount` to a traditional IRA.
pub fn new_ira_contribution(amount: USCents) -> IraContributionBuilder {
    IraContributionBuilder {
        c: IraContribution {
            amount,
            ..IraContribution::default()
        },
    }
}

impl IraContributionBuilder {
    /// Sets the year's Roth contribution.
    pub fn roth(&mut self, amount: USCents) -> &mut Self {
        self.c.roth = amount;
        self
    }

    /// Marks a contributor who is 50 or older by the end of the year.
    pub fn catch_up(&mut self) -> &mut Self {
        self.c.catch_up = true;
        self
    }

    /// Says whether the contributor is covered by a plan at work.
    pub fn active_participant(&mut self, covered: bool) -> &mut Self {
        (self.c.covered, self.c.covered_answered) = (covered, true);
        self
    }

    /// Says whether the contributor's spouse is covered by a plan at work.
    pub fn spouse_active_participant(&mut self, covered: bool) -> &mut Self {
        (self.c.spouse_covered, self.c.spouse_answered) = (covered, true);
        self
    }

    /// Marks a separate filer who lived apart from their spouse all year.
    pub fn lived_apart(&mut self) -> &mut Self {
        self.c.lived_apart = true;
        self
    }

    /// Refuses a negative contribution.
    pub fn build(&self) -> Result<IraContribution, Error> {
        if self.c.amount < USCents(0) || self.c.roth < USCents(0) {
            return Err(invalid!(
                "ustaxcalc: an IRA contribution cannot be negative"
            ));
        }
        Ok(self.c)
    }
}

/// A § 219(g) range: the deduction falls from the full limit at `from` to
/// nothing at `from` plus `width`.
#[derive(Clone, Copy, Debug)]
struct PhaseOut {
    from: USCents,
    width: USCents,
}

const fn d(n: i64) -> USCents {
    USCents(n * DOLLAR.0)
}

const fn po(from: i64, width: i64) -> PhaseOut {
    PhaseOut {
        from: d(from),
        width: d(width),
    }
}

/// One year's published § 219 figures.
struct IraYear {
    limit: USCents,
    catch_up: USCents,
    source: &'static str,
    // The phase-out ranges, by who is covered and how the return is filed.
    single: PhaseOut,
    joint: PhaseOut,
    spouse_only: PhaseOut,
    separate: PhaseOut,
    // Takes the joint applicable amount, which the notice gives for a
    // qualifying widow(er), over the $10,000 width § 219(g)(2)(A)(ii) gives
    // every return but a joint one.
    surviving_spouse: PhaseOut,
    // The § 408A(c)(3) Roth ranges. The notice gives a qualifying widow(er)
    // the joint start; the width is the $15,000 § 408A(c)(3)(A)(ii) gives
    // every return but a joint or separate one.
    roth_single: PhaseOut,
    roth_joint: PhaseOut,
    roth_separate: PhaseOut,
    roth_surviving_spouse: PhaseOut,
}

/// The published figures. 2026 is Notice 2025-67; no other year is held.
fn ira_year(year: i64) -> Option<IraYear> {
    match year {
        2026 => Some(IraYear {
            limit: d(7_500),
            catch_up: d(1_100),
            source: "Notice 2025-67",
            single: po(81_000, 10_000),
            joint: po(129_000, 20_000),
            spouse_only: po(242_000, 10_000),
            separate: po(0, 10_000),
            surviving_spouse: po(129_000, 10_000),

            roth_single: po(153_000, 15_000),
            roth_joint: po(242_000, 10_000),
            roth_separate: po(0, 10_000),
            roth_surviving_spouse: po(242_000, 15_000),
        }),
        _ => None,
    }
}

/// The deductible part of a traditional IRA contribution under § 219(b)(1)
/// and (g): the contribution, up to the year's limit, less the reduction for
/// a taxpayer or spouse covered by a plan at work.
///
/// `magi` is adjusted gross income figured without the IRA deduction, as
/// § 219(g)(3)(A) has it. The exclusions it also adds back, such as foreign
/// earned income and student loan interest, are the caller's to include.
/// The limit is not compared with the contributor's compensation; a caller
/// whose compensation is under the limit passes the smaller contribution.
///
/// A year with no published figures is [`Error::NoTable`].
pub fn ira_deduction(
    year: i64,
    status: FilingStatus,
    c: IraContribution,
    magi: USCents,
) -> Result<working::Result<USCents>, Error> {
    let Some(y) = ira_year(year) else {
        return Err(Error::with_detail(
            Error::NoTable,
            format!("no IRA deduction limits for {year}"),
        ));
    };
    let mut limit = y.limit;
    let mut b = working::Notes::default();
    let cite = format!("26 U.S.C. § 219(b)(1), (b)(5); {}", y.source);
    if c.catch_up {
        limit += y.catch_up;
        b.rule(
            cite,
            format!(
                "the {} limit is {}, and {} more at 50 or older, {}",
                year, y.limit, y.catch_up, limit
            ),
        );
    } else {
        b.rule(cite, format!("the {year} limit is {limit}"));
    }

    let married =
        (status == MARRIED_FILING_JOINTLY || status == MARRIED_FILING_SEPARATELY) && !c.lived_apart;
    let (covered, answered) = c.active_participant();
    if !answered {
        b.ask(
            PositionKind::IraDeductibility,
            "Are you covered by a retirement plan at work?",
            "If you are, the IRA deduction is reduced or lost at higher incomes; until then, the full contribution is deducted.",
        );
    }
    let mut spouse_covered = false;
    if married {
        let spouse_answered;
        (spouse_covered, spouse_answered) = c.spouse_active_participant();
        if !spouse_answered {
            b.ask(
                PositionKind::IraDeductibility,
                "Is your spouse covered by a retirement plan at work?",
                "If your spouse is, your IRA deduction is reduced or lost at higher incomes; until then, it isn't reduced for that.",
            );
        }
    }

    let (r, which) = if !covered && !spouse_covered {
        let deduction = c.amount.min(limit);
        b.rule(
            "26 U.S.C. § 219(g)(1)",
            format!(
                "neither you nor a spouse is covered by a plan at work, so {} of the {} contribution is deductible",
                deduction, c.amount
            ),
        );
        return Ok(working::new_result(deduction).with(&b).build()?);
    } else if status == MARRIED_FILING_SEPARATELY && married {
        (y.separate, "a married person filing separately")
    } else if covered && status == MARRIED_FILING_JOINTLY {
        (y.joint, "a covered contributor on a joint return")
    } else if covered && status == SURVIVING_SPOUSE {
        (y.surviving_spouse, "a covered qualifying surviving spouse")
    } else if covered {
        (y.single, "a covered single filer or head of household")
    } else {
        (y.spouse_only, "a contributor not covered whose spouse is")
    };

    let reduced = phased_out(limit, magi, r);
    let deduction = c.amount.min(reduced);
    b.rule(
        format!("26 U.S.C. § 219(g)(1) to (3), (7); {}", y.source),
        format!(
            "for {} the {} range is {} to {}; modified AGI of {} leaves a limit of {}, so {} of the {} contribution is deductible",
            which,
            year,
            r.from,
            r.from + r.width,
            magi,
            reduced,
            deduction,
            c.amount
        ),
    );
    Ok(working::new_result(deduction).with(&b).build()?)
}

/// `limit` after a § 219(g)(2) reduction across `r`, which § 408A(c)(3)(A)
/// also uses for a Roth: the reduction bears to the limit the ratio of
/// income over the start of the range to the range's width, is rounded to
/// the next lowest $10, and leaves no less than $200 unless it is the whole
/// limit.
fn phased_out(limit: USCents, magi: USCents, r: PhaseOut) -> USCents {
    let over = (magi - r.from).max(USCents(0));
    if over >= r.width {
        return USCents(0);
    }
    if over == USCents(0) {
        return limit;
    }
    // Go's big.Int: exact, truncated toward zero, then back to int64.
    let reduction = (i128::from(limit.0) * i128::from(over.0) / i128::from(r.width.0)) as i64;
    let ten = 10 * DOLLAR.0;
    (limit - USCents(reduction / ten * ten)).max(d(200))
}

/// One person's year under the limit their traditional and Roth IRA
/// contributions share.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct IraLimits {
    combined: USCents,
    roth_limit: USCents,
    roth_allowed: USCents,
    deduction: USCents,
    traditional_excess: USCents,
    roth_excess: USCents,
    excess_tax: USCents,
}

impl IraLimits {
    /// The limit the two kinds of contribution share: the § 219(b) limit,
    /// with the age-50 addition, before any reduction for income.
    pub fn combined(&self) -> USCents {
        self.combined
    }

    /// The most that could go to a Roth IRA at this income, before any
    /// traditional contribution is taken off: § 408A(c)(3)(A).
    pub fn roth_limit(&self) -> USCents {
        self.roth_limit
    }

    /// The Roth contribution allowed after the traditional one: the smaller
    /// of `roth_limit` and what the traditional contribution leaves of
    /// `combined`, § 408A(c)(2).
    pub fn roth_allowed(&self) -> USCents {
        self.roth_allowed
    }

    /// The deductible part of the traditional contribution, as
    /// [`ira_deduction`] has it. A Roth contribution is never deducted.
    pub fn deduction(&self) -> USCents {
        self.deduction
    }

    /// The traditional contribution over `combined`, § 4973(b)(1). A
    /// contribution that is allowed but not deductible is not an excess.
    pub fn traditional_excess(&self) -> USCents {
        self.traditional_excess
    }

    /// The Roth contribution over `roth_allowed`, § 4973(f)(1).
    pub fn roth_excess(&self) -> USCents {
        self.roth_excess
    }

    /// The § 4973(a) tax: 6% of the two excesses. Form 5329 lines 17 and 25
    /// carry it to Schedule 2 line 8, and so into Form 1040's total tax.
    /// Form 2210 line 2 takes line 8 for the tax on distributions only, so
    /// it is not part of the tax that sets the required annual payment.
    pub fn excess_tax(&self) -> USCents {
        self.excess_tax
    }
}

/// Works out one person's traditional and Roth IRA year: the shared limit,
/// the Roth limit at this income, the traditional deduction, and any excess
/// with the 6% tax on it.
///
/// `magi` is adjusted gross income figured without the IRA deduction, as
/// § 219(g)(3)(A) has it; § 408A(c)(3)(B)(i) reads the same figure for the
/// Roth, but without income from converting to a Roth IRA, which the caller
/// leaves out. As in [`ira_deduction`], the limit is not compared with the
/// person's compensation.
///
/// The 6% tax is on this year's excess only. An excess left in from an
/// earlier year, § 4973(b)(2) and (f)(2), is the caller's to add, and the
/// tax is no more than 6% of the accounts' value at the end of the year,
/// which this does not know. A contribution withdrawn under § 408(d)(4) is
/// treated as not made, so the caller passes the contributions net of it.
///
/// A year with no published figures is [`Error::NoTable`].
pub fn ira_contribution_limits(
    year: i64,
    status: FilingStatus,
    c: IraContribution,
    magi: USCents,
) -> Result<working::Result<IraLimits>, Error> {
    let Some(y) = ira_year(year) else {
        return Err(Error::with_detail(
            Error::NoTable,
            format!("no IRA contribution limits for {year}"),
        ));
    };
    let mut l = IraLimits::default();
    let mut b = working::Notes::default();
    l.combined = y.limit;
    if c.catch_up {
        l.combined += y.catch_up;
    }
    let zero = USCents(0);
    if c.amount > zero {
        let d = ira_deduction(year, status, c, magi)?;
        b.applied(&d.applied()).required(&d.required());
        l.deduction = d.value();
    }

    let married =
        (status == MARRIED_FILING_JOINTLY || status == MARRIED_FILING_SEPARATELY) && !c.lived_apart;
    let (r, which) = if status == MARRIED_FILING_JOINTLY {
        (y.roth_joint, "a joint return")
    } else if status == MARRIED_FILING_SEPARATELY && married {
        (y.roth_separate, "a married person filing separately")
    } else if status == SURVIVING_SPOUSE {
        (y.roth_surviving_spouse, "a qualifying surviving spouse")
    } else {
        (y.roth_single, "a single filer or head of household")
    };
    l.roth_limit = phased_out(l.combined, magi, r);
    l.roth_allowed = l.roth_limit.min((l.combined - c.amount).max(zero));
    let mut after = String::new();
    if c.amount > zero {
        after = format!(
            "; after {} of traditional contributions, {} may go to a Roth",
            c.amount, l.roth_allowed
        );
    }
    b.rule(
        format!("26 U.S.C. § 408A(c)(1) to (3); {}", y.source),
        format!(
            "the {} limit is {} across traditional and Roth IRAs; for {} the Roth range is {} to {}, and modified AGI of {} leaves a Roth limit of {}{}. A Roth contribution is never deducted",
            year,
            l.combined,
            which,
            r.from,
            r.from + r.width,
            magi,
            l.roth_limit,
            after
        ),
    );

    l.traditional_excess = (c.amount - l.combined).max(zero);
    l.roth_excess = (c.roth - l.roth_allowed).max(zero);
    let excess = l.traditional_excess + l.roth_excess;
    if excess > zero {
        l.excess_tax = (excess * 6 + USCents(50)) / 100;
        b.rule(
            "26 U.S.C. § 4973(a), (b)(1), (f)(1)",
            format!(
                "{} of traditional and {} of Roth contributions are over the limits; the tax is 6% of {}, {}",
                l.traditional_excess, l.roth_excess, excess, l.excess_tax
            ),
        );
    }
    Ok(working::new_result(l).with(&b).build()?)
}
