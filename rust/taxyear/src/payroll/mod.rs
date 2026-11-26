//! How an employer withholds from a paycheck: federal income tax by
//! Publication 15-T's percentage method, supplemental wages, Social Security
//! and Medicare tax, and the workplace plan limits. Ported from the Go
//! package `taxyear/payroll`.
//!
//! The income tax brackets, standard deduction, Social Security wage base and
//! workplace plan limits are the federal taxlisp rules' (see figures.rs).
//! What this module adds is the payroll rules that read them. Each constant
//! names its authority. The Publication 15-T construction is read from the
//! 2025 publication's Worksheet 1A; the 2026 publication was not checked
//! against it, and its figures belong in the rules with their primary source
//! once it is.

mod figures;

use filing::Status;
use money::{DOLLAR, Rate, USCents};

use figures::figures_for;

/// An error from payroll, with Go's text.
#[derive(Clone, Debug, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    /// A W-4 or pay period count payroll refuses.
    #[error("{0}")]
    Invalid(String),
    /// Go's [`Error::NO_YEAR`], wrapped: the federal rules carry no
    /// withholding figures for the tax year. The text is the whole message.
    #[error("{0}")]
    NoYear(String),
    /// The federal rules could not be read.
    #[error("{0}")]
    Rules(String),
}

impl Error {
    /// The text of Go's ErrNoYear. Payroll refuses a year without figures
    /// rather than apply another year's figures to it.
    pub const NO_YEAR: &str = "payroll: no withholding figures for this tax year";

    /// Reports whether the error is or wraps ErrNoYear, as Go's
    /// `errors.Is(err, payroll.ErrNoYear)`.
    pub fn is_no_year(&self) -> bool {
        matches!(self, Error::NoYear(_))
    }

    /// The error with `prefix` and ": " before its text, keeping its kind,
    /// as Go's `fmt.Errorf("prefix: %w", err)`.
    fn wrap(self, prefix: &str) -> Error {
        match self {
            Error::Invalid(s) => Error::Invalid(format!("{prefix}: {s}")),
            Error::NoYear(s) => Error::NoYear(format!("{prefix}: {s}")),
            Error::Rules(s) => Error::Rules(format!("{prefix}: {s}")),
        }
    }
}

/// The rule a figure came from and what applying it did, so a caller can
/// show its working.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Rule {
    cite: String,
    applied: String,
}

impl Rule {
    /// Names the authority, such as "Publication 15-T, Worksheet 1A".
    pub fn cite(&self) -> &str {
        &self.cite
    }

    /// Says what the rule did, in the caller's own numbers.
    pub fn applied(&self) -> &str {
        &self.applied
    }
}

/// A Rule citing Worksheet 1A of Publication 15-T.
fn worksheet_1a(applied: String) -> Rule {
    Rule {
        cite: "Publication 15-T, Worksheet 1A".to_string(),
        applied,
    }
}

/// The employee's old-age, survivors and disability insurance tax,
/// 26 U.S.C. § 3101(a).
pub const SOCIAL_SECURITY_RATE: Rate = Rate(620);

/// The employee's hospital insurance tax, § 3101(b)(1).
pub const MEDICARE_RATE: Rate = Rate(145);

/// The extra hospital insurance tax, § 3101(b)(2).
pub const ADDITIONAL_MEDICARE_RATE: Rate = Rate(90);

/// The wages from one employer over which the employer withholds the extra
/// tax, whatever the filing status, § 3102(f)(1).
pub const ADDITIONAL_MEDICARE_WITHHOLDING: USCents = USCents(200_000 * DOLLAR.0);

/// The optional flat rate on supplemental wages,
/// 26 C.F.R. § 31.3402(g)-1(a)(7)(iii)(F).
pub const SUPPLEMENTAL_RATE: Rate = Rate(2200);

/// Applies to supplemental wages from one employer over
/// [`MANDATORY_THRESHOLD`] in the year, § 31.3402(g)-1(a)(2): the highest
/// rate in § 1.
pub const MANDATORY_SUPPLEMENTAL_RATE: Rate = Rate(3700);

/// $1 million of supplemental wages in the year, § 31.3402(g)-1(a)(2).
pub const MANDATORY_THRESHOLD: USCents = USCents(1_000_000 * DOLLAR.0);

/// Last year's Social Security wages from an employer over which a catch-up
/// contribution must be Roth, § 414(v)(7), as adjusted for 2026 by
/// Notice 2025-67.
pub const ROTH_CATCH_UP_WAGES: USCents = USCents(150_000 * DOLLAR.0);

/// How often a job pays.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub enum Frequency {
    /// Not a frequency.
    #[default]
    Unspecified,
    /// Pays 52 times a year.
    Weekly,
    /// Pays 26 times a year.
    Biweekly,
    /// Pays 24 times a year.
    SemiMonthly,
    /// Pays 12 times a year.
    Monthly,
}

impl Frequency {
    /// The number of pay periods a year, Publication 15-T's Table 3.
    pub fn periods(self) -> i64 {
        match self {
            Frequency::Weekly => 52,
            Frequency::Biweekly => 26,
            Frequency::SemiMonthly => 24,
            Frequency::Monthly => 12,
            Frequency::Unspecified => 0,
        }
    }
}

/// What Worksheet 1A reads from a Form W-4.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct W4 {
    status: Status,
    multiple_jobs: bool,
    dependents: USCents,
    other_income: USCents,
    deductions: USCents,
    extra: USCents,
    exempt: bool,
}

impl W4 {
    /// Step 1(c).
    pub fn status(&self) -> Status {
        self.status
    }

    /// Step 2's checkbox.
    pub fn multiple_jobs(&self) -> bool {
        self.multiple_jobs
    }
}

/// Builds a [`W4`].
#[derive(Clone, Copy, Debug, Default)]
pub struct W4Builder {
    w: W4,
}

/// Starts a W-4 with the filing status of Step 1(c). A W-4 the employee
/// never gave is this alone, as Publication 15-T treats one.
pub fn new_w4(status: Status) -> W4Builder {
    W4Builder {
        w: W4 {
            status,
            ..W4::default()
        },
    }
}

impl W4Builder {
    /// Checks Step 2.
    pub fn multiple_jobs(&mut self) -> &mut Self {
        self.w.multiple_jobs = true;
        self
    }

    /// Sets Step 3.
    pub fn dependents(&mut self, a: USCents) -> &mut Self {
        self.w.dependents = a;
        self
    }

    /// Sets Step 4(a).
    pub fn other_income(&mut self, a: USCents) -> &mut Self {
        self.w.other_income = a;
        self
    }

    /// Sets Step 4(b).
    pub fn deductions(&mut self, a: USCents) -> &mut Self {
        self.w.deductions = a;
        self
    }

    /// Sets Step 4(c), extra withholding each pay period.
    pub fn extra(&mut self, a: USCents) -> &mut Self {
        self.w.extra = a;
        self
    }

    /// Checks the exemption box below Step 4(c).
    pub fn exempt(&mut self) -> &mut Self {
        self.w.exempt = true;
        self
    }

    /// Refuses a status Publication 15-T has no table for, and negative
    /// amounts.
    pub fn build(&self) -> Result<W4, Error> {
        if self.w.status == Status::Unspecified {
            return Err(Error::Invalid(
                "payroll: a W-4 needs a filing status".to_string(),
            ));
        }
        for a in [
            self.w.dependents,
            self.w.other_income,
            self.w.deductions,
            self.w.extra,
        ] {
            if a.0 < 0 {
                return Err(Error::Invalid(format!(
                    "payroll: a W-4 amount of {a} is negative"
                )));
            }
        }
        Ok(self.w)
    }
}

/// Worksheet 1A line 1g: the deduction the standard tables build in when
/// Step 2 is not checked.
fn line1g(s: Status) -> USCents {
    if s == Status::MarriedFilingJointly {
        return USCents(12_900 * DOLLAR.0);
    }
    USCents(8_600 * DOLLAR.0)
}

/// The Publication 15-T table a W-4 status reads: married filing jointly;
/// single or married filing separately; head of household.
fn table_status(s: Status) -> Status {
    match s {
        Status::MarriedFilingJointly | Status::SurvivingSpouse => Status::MarriedFilingJointly,
        Status::HeadOfHousehold => Status::HeadOfHousehold,
        _ => Status::Single,
    }
}

/// Worksheet 1A of Publication 15-T, the percentage method for automated
/// payroll systems: the federal income tax withheld from one paycheck of
/// taxable wages, gross less pre-tax lines.
///
/// The annual tables are built from the year's § 1(j) brackets and § 63(c)
/// standard deduction as Publication 15-T builds them: the standard table
/// taxes annual wages less the standard deduction plus line 1g's amount, and
/// the Step 2 table taxes twice the wages less the standard deduction,
/// halved. The published tables round their thresholds to the dollar, so a
/// figure here can differ from one read off them by a few cents.
pub fn federal_withholding(
    year: i64,
    w: &W4,
    taxable: USCents,
    periods: i64,
) -> Result<(USCents, Rule), Error> {
    if periods <= 0 {
        return Err(Error::Invalid(format!(
            "payroll: {periods} pay periods a year"
        )));
    }
    if w.exempt {
        return Ok((
            USCents(0),
            worksheet_1a(
                "The W-4 claims exemption, so no federal income tax is withheld".to_string(),
            ),
        ));
    }
    let status = table_status(w.status);
    let fig = figures_for(year)?;
    let standard = fig.standard(status);
    let annual = taxable * periods + w.other_income;
    let mut adjusted = annual - w.deductions;
    let tentative = if w.multiple_jobs {
        halve(fig.tax(status, 2 * adjusted - standard))
    } else {
        adjusted -= line1g(w.status);
        fig.tax(status, adjusted - (standard - line1g(w.status)))
    };
    let per = divide(tentative, periods);
    let credit = divide(w.dependents, periods);
    let withheld = USCents(0).max(per - credit) + w.extra;
    let step2 = if w.multiple_jobs {
        " with Step 2 checked"
    } else {
        ""
    };
    Ok((
        withheld,
        worksheet_1a(format!(
            "{taxable} of taxable wages a paycheck, {periods} paychecks a year, withheld at the {status} table{step2}: {withheld}"
        )),
    ))
}

/// The federal tax withheld on a supplemental payment by the flat rate: 22%,
/// and 37% on what takes the employer's supplemental wages for the year past
/// $1 million. before is the supplemental wages paid earlier in the year.
pub fn flat_supplemental(amount: USCents, before: USCents) -> USCents {
    let over = USCents(0).max(before + amount - MANDATORY_THRESHOLD);
    let over = over.min(amount);
    SUPPLEMENTAL_RATE.of(amount - over) + MANDATORY_SUPPLEMENTAL_RATE.of(over)
}

/// The Social Security tax on a paycheck's Social Security wages, given the
/// wages the job has already paid in the year: nothing past the wage base.
pub fn social_security(year: i64, wages: USCents, before: USCents) -> Result<USCents, Error> {
    let base = wage_base(year)?;
    let taxed = USCents(0).max(wages.min(base - before));
    Ok(SOCIAL_SECURITY_RATE.of(taxed))
}

/// The year's Social Security wage base, § 3121(a)(1), from the federal
/// rules' figures.
pub fn wage_base(year: i64) -> Result<USCents, Error> {
    Ok(figures_for(year)?.wage_base)
}

/// The Medicare tax on a paycheck's Medicare wages, given the job's Medicare
/// wages earlier in the year: 1.45%, and 0.9% more on wages past $200,000
/// from this employer.
pub fn medicare(wages: USCents, before: USCents) -> USCents {
    let over = USCents(0).max(before + wages - ADDITIONAL_MEDICARE_WITHHOLDING);
    let over = over.min(USCents(0).max(wages));
    MEDICARE_RATE.of(wages) + ADDITIONAL_MEDICARE_RATE.of(over)
}

/// A person's § 402(g) limit for the year with the § 414(v) catch-up their
/// age at the end of the year allows, from the federal rules'
/// plan-elective-deferral-limit and plan-catch-up: $23,500 for 2025, plus
/// $7,500 from 50, or $11,250 at 60 to 63 (Notice 2024-80); $24,500 for
/// 2026, plus $8,000 from 50, or $11,250 at 60 to 63 (Notice 2025-67). It
/// returns the base and the catch-up separately, and [`Error::NoYear`] for
/// a year with no figures.
pub fn plan_limit(year: i64, age: i64) -> Result<(USCents, USCents), Error> {
    let fig =
        figures_for(year).map_err(|e| e.wrap(&format!("payroll: no § 402(g) limit for {year}")))?;
    let catch_up = fig.catch_up(age)?;
    Ok((fig.deferral_limit, catch_up))
}

/// Reports whether a state taxes no wages, so nothing is withheld for it:
/// Alaska, Florida, Nevada, New Hampshire, South Dakota, Tennessee, Texas,
/// Washington and Wyoming.
pub fn no_wage_tax(state: &str) -> bool {
    matches!(
        state,
        "AK" | "FL" | "NV" | "NH" | "SD" | "TN" | "TX" | "WA" | "WY"
    )
}

fn halve(a: USCents) -> USCents {
    divide(a, 2)
}

/// Rounds a / n half away from zero.
fn divide(a: USCents, n: i64) -> USCents {
    let d = n;
    if a.0 < 0 {
        return USCents(-((-a.0 + d / 2) / d));
    }
    USCents((a.0 + d / 2) / d)
}
