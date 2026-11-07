//! Oregon taxable income from federal adjusted gross income. Ported from
//! income.go.

use filing::Status;
use money::USCents;
use working::{Notes, PositionKind};

use crate::tables::{Error, Table, table_for};

/// Which deduction method a taxpayer has chosen, where they have.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub enum Election {
    /// Takes whichever figure is bigger.
    #[default]
    Larger,

    /// Takes the standard deduction whatever the itemised total is.
    Standard,

    /// Takes the itemised total whatever the standard deduction is.
    Itemise,
}

/// The facts about a taxpayer that change the standard deduction.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Conditions {
    /// Adds one unit of the addition for age. Oregon measures age at 1
    /// January of the following year, so somebody turning 65 on New Year's
    /// Day qualifies for the year before.
    pub aged_65_or_older: bool,
    /// Adds one unit of the addition for blindness. A taxpayer who is both
    /// aged and blind gets it twice.
    pub blind: bool,

    /// Adds the same again for the spouse on a joint return. Ignored for
    /// every other filing status.
    pub spouse_aged_65_or_older: bool,
    /// Adds the same again for the spouse on a joint return. Ignored for
    /// every other filing status.
    pub spouse_blind: bool,

    /// Reports that a separately filing spouse itemised, which makes this
    /// filer's standard deduction nothing at all. Ignored for every status
    /// but married filing separately.
    ///
    /// A rule with no federal counterpart in this form: the federal code
    /// makes the filer itemise too, where Oregon leaves them able to take a
    /// standard deduction of zero.
    pub spouse_itemises: bool,
}

/// Answers whether the filer was an Oregon resident for the whole tax year.
///
/// It exists so that the residency position can be answered rather than
/// only reported. Form OR-40 is a full-year resident's return, so a caller
/// who says otherwise gets an error rather than a number computed under the
/// wrong form.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub enum Residency {
    /// The zero value. The calculators compute a full-year resident's tax
    /// and return the position unanswered.
    #[default]
    Unstated,

    /// Answers the position and changes nothing else.
    FullYearResident,

    /// Makes [`taxable_income`] refuse. Such a filer uses Form OR-40-P or
    /// OR-40-N, which prorate the tax by the share of income from Oregon
    /// sources and are not computed here.
    PartYearOrNonResident,
}

/// The differences between a federal return and an Oregon one, as Schedule
/// OR-ASC totals them, plus the deduction the filer takes.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Adjustments {
    /// The Schedule OR-ASC Section A total: income Oregon taxes that the
    /// federal return did not.
    pub additions: USCents,

    /// The Section B total, which does not include the federal tax
    /// subtraction, which [`crate::federal_tax_subtraction`] computes because
    /// it is capped by income.
    pub subtractions: USCents,

    /// What [`crate::federal_tax_subtraction`] returned. Passed in rather
    /// than recomputed, so that a caller who has already worked it out and
    /// shown it to a user cannot get a different figure here.
    pub federal_tax_subtraction: USCents,

    /// The Schedule OR-A total, or zero where nothing has been totalled.
    pub itemised_deductions: USCents,

    /// Settles the standard-versus-itemised choice. The default takes the
    /// larger figure and reports the position as unanswered.
    pub election: Election,

    /// Settles whether the filer was an Oregon resident all year. The
    /// default reports the position as unanswered.
    pub residency: Residency,
}

/// What was subtracted, and which method produced it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Deduction {
    /// The deduction taken.
    pub amount: USCents,
    /// Whether it is the itemised total.
    pub itemised: bool,

    /// The standard deduction including any addition for age or blindness,
    /// whether or not it was taken.
    pub standard: USCents,

    /// The standard deduction was forced to nothing because a separately
    /// filing spouse itemised.
    pub spouse_itemises: bool,
}

/// Oregon taxable income and how it was reached.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub struct Income {
    /// Federal adjusted gross income after the Schedule OR-ASC additions and
    /// subtractions. Form OR-40 line 7.
    pub oregon_agi: USCents,

    /// The deduction taken and how.
    pub deduction: Deduction,

    /// What was subtracted for federal income tax.
    pub federal_tax_subtraction: USCents,

    /// Oregon taxable income, Form OR-40 line 19. This is what
    /// [`crate::ordinary_income_tax`] wants.
    pub amount: USCents,
}

/// Returns the ORS 316.695(9) deduction with any addition for age or
/// blindness.
pub fn standard_deduction(
    year: i64,
    status: Status,
    c: Conditions,
) -> Result<working::Result<USCents>, Error> {
    let table = table_for(year, status)?;
    let (amount, units, zeroed) = standard_from(&table, status, c);

    let mut notes = Notes::default();
    notes.rule(
        format!("Or. Rev. Stat. § 316.695(9), as adjusted for {year}"),
        format!(
            "the standard deduction for a {status} filer is {}",
            table.standard_deduction
        ),
    );
    if zeroed {
        notes.rule(
            "Or. Rev. Stat. § 316.695(9)(c)",
            "the spouse on the other separate return itemised, so this filer's standard deduction is nothing",
        );
        return Ok(working::new_result(USCents(0)).with(&notes).build()?);
    }
    if units > 0 {
        notes.rule(
            "Or. Rev. Stat. § 316.695(9)(b)",
            format!(
                "{units} addition(s) of {} for age or blindness bring it to {amount}",
                table.additional_aged_blind
            ),
        );
    }
    Ok(working::new_result(amount).with(&notes).build()?)
}

fn standard_from(table: &Table, status: Status, c: Conditions) -> (USCents, i64, bool) {
    if status == Status::MarriedFilingSeparately && c.spouse_itemises {
        return (USCents(0), 0, true);
    }
    let mut units = 0;
    if c.aged_65_or_older {
        units += 1;
    }
    if c.blind {
        units += 1;
    }
    if status == Status::MarriedFilingJointly || status == Status::SurvivingSpouse {
        if c.spouse_aged_65_or_older {
            units += 1;
        }
        if c.spouse_blind {
            units += 1;
        }
    }
    (
        table.standard_deduction + table.additional_aged_blind * units,
        units,
        false,
    )
}

/// Works Oregon taxable income out from federal adjusted gross income.
///
/// `federal_agi` is federal adjusted gross income — Form 1040 line 11, which
/// is what Form OR-40 line 7 asks for — taken as a plain amount so that a
/// caller with the figure from anywhere can use it.
///
/// The result is Form OR-40 line 19, which [`crate::ordinary_income_tax`]
/// takes.
///
/// A negative result is returned as zero.
pub fn taxable_income(
    year: i64,
    status: Status,
    federal_agi: USCents,
    c: Conditions,
    adj: Adjustments,
) -> Result<working::Result<Income>, Error> {
    let table = table_for(year, status)?;
    if adj.residency == Residency::PartYearOrNonResident {
        return Err(Error::Invalid(
            "oregontaxcalc: a part-year resident files Form OR-40-P and a non-resident \
             Form OR-40-N, both of which prorate the tax by the share of income from \
             Oregon sources and are not computed here"
                .to_string(),
        ));
    }

    let (standard, units, zeroed) = standard_from(&table, status, c);

    let mut out = Income {
        oregon_agi: federal_agi + adj.additions - adj.subtractions,
        federal_tax_subtraction: adj.federal_tax_subtraction,
        deduction: Deduction {
            amount: standard,
            standard,
            spouse_itemises: zeroed,
            itemised: false,
        },
        amount: USCents(0),
    };
    match adj.election {
        Election::Itemise => {
            out.deduction.amount = adj.itemised_deductions;
            out.deduction.itemised = true;
        }
        // Already the standard deduction.
        Election::Standard => {}
        Election::Larger => {
            if adj.itemised_deductions > standard {
                out.deduction.amount = adj.itemised_deductions;
                out.deduction.itemised = true;
            }
        }
    }

    out.amount =
        (out.oregon_agi - out.deduction.amount - out.federal_tax_subtraction).max(USCents(0));

    let mut notes = Notes::default();
    notes.rule(
        "Or. Rev. Stat. § 316.048",
        format!(
            "federal adjusted gross income of {federal_agi}, plus {} of additions and less {} of subtractions, gives Oregon adjusted gross income of {}",
            adj.additions, adj.subtractions, out.oregon_agi
        ),
    );
    notes.rule(
        format!("Or. Rev. Stat. § 316.695(9), as adjusted for {}", table.year),
        format!(
            "the standard deduction for a {} filer, after {units} addition(s) for age or blindness, is {standard}",
            table.status
        ),
    );
    if adj.federal_tax_subtraction.0 > 0 {
        notes.rule(
            "Or. Rev. Stat. § 316.680(1)(b)",
            format!(
                "{} of federal income tax is subtracted",
                adj.federal_tax_subtraction
            ),
        );
    }

    if adj.election == Election::Larger {
        notes.require(
            PositionKind::DeductionMethod,
            "Oregon makes itemising an election of its own under ORS 316.695, and \
             does not bind a filer to what the federal return elected",
        );
    }
    if adj.residency == Residency::Unstated {
        notes.require(
            PositionKind::Residency,
            "these figures are a full-year resident's, who files Form OR-40. A part-year \
             resident files Form OR-40-P and a non-resident Form OR-40-N, both of which \
             prorate the tax by the share of income from Oregon sources. Answer it with \
             Adjustments.Residency",
        );
    }
    Ok(working::new_result(out).with(&notes).build()?)
}
