//! California taxable income and the exemption credits. Ported from
//! income.go.

use filing::Status;
use money::USCents;
use working::{Notes, PositionKind};

use crate::tables::{Error, table_for};

/// Which deduction method a taxpayer has chosen, where they have.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub enum Election {
    /// Go's `ElectLarger`: takes whichever figure is bigger. Right for almost
    /// everybody, and the behaviour of a caller who has not thought about
    /// it.
    #[default]
    Larger,

    /// Go's `ElectStandard`: takes the standard deduction whatever the
    /// itemised total is.
    Standard,

    /// Go's `ElectItemise`: takes the itemised total whatever the standard
    /// deduction is.
    Itemise,
}

/// Whether the filer was a California resident for the whole tax year.
///
/// It exists so that the residency position can be answered rather than
/// only reported. Form 540 is a full-year resident's return and these
/// figures are a full-year resident's, so a caller who says otherwise gets an
/// error rather than a number computed under the wrong form.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
pub enum Residency {
    /// Go's `ResidencyUnstated`, the zero value. The calculators compute a
    /// full-year resident's tax and return the position unanswered.
    #[default]
    Unstated,

    /// Answers the position. Nothing else changes; the figures were already
    /// this filer's.
    FullYearResident,

    /// Makes [`taxable_income`] refuse. Rev. & Tax. Code § 17041(b)
    /// apportions such a filer's tax by the share of income from California
    /// sources, on Form 540NR, which this crate does not compute.
    PartYearOrNonResident,
}

/// The differences between a federal return and a California one, as
/// Schedule CA (540) totals them.
///
/// California conforms to the federal definition of gross income only as of
/// a fixed date, so the two figures differ for anybody with tax-exempt
/// interest from another state's bonds, a health savings account, or several
/// dozen other items. Schedule CA is where a filer works the difference out;
/// this struct is where they hand the answer over.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Adjustments {
    /// Schedule CA (540) Part I, line 27, column B: income California does
    /// not tax that the federal return did. California treats social
    /// security benefits this way, which is the case most filers meet.
    pub subtractions: USCents,

    /// Part I, line 27, column C: income California taxes that the federal
    /// return did not.
    pub additions: USCents,

    /// The California itemised total from Schedule CA Part II, or zero where
    /// nothing has been totalled.
    pub itemised_deductions: USCents,

    /// Settles the standard-versus-itemised choice. The zero value takes the
    /// larger figure and reports the position as unanswered.
    pub election: Election,

    /// Settles whether the filer was a California resident all year. The
    /// zero value reports the position as unanswered.
    pub residency: Residency,
}

/// California taxable income and how it was reached.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Income {
    /// Federal adjusted gross income after the Schedule CA adjustments. Form
    /// 540 line 17.
    pub california_agi: USCents,

    /// What was subtracted.
    pub deduction: USCents,
    /// Whether `deduction` was the itemised total rather than the standard
    /// deduction.
    pub itemised: bool,

    /// The standard deduction, whether or not it was taken. A caller showing
    /// its working renders both figures so the choice is checkable.
    pub standard: USCents,

    /// California taxable income, Form 540 line 19. This is what
    /// [`crate::ordinary_income_tax`] and
    /// [`crate::behavioral_health_services_tax`] want.
    pub amount: USCents,
}

/// Works California taxable income out from federal adjusted gross income.
///
/// `federal_agi` is federal adjusted gross income — Form 1040 line 11b,
/// which is the line Form 540 line 13 names — taken as a plain amount. This
/// crate does not import the federal calculators, so a caller with the
/// figure from a spreadsheet, a prior-year return or another library can use
/// it.
///
/// The result is Form 540 line 19, which both [`crate::ordinary_income_tax`]
/// and [`crate::behavioral_health_services_tax`] take. Note that it includes
/// any capital gain in full, because California taxes one at the ordinary
/// rates.
///
/// A negative result is returned as zero. Deductions exceeding income
/// produce no taxable income rather than a negative amount, and the loss
/// carryforward rules that would apply are not modelled here.
pub fn taxable_income(
    year: i64,
    status: Status,
    federal_agi: USCents,
    adj: Adjustments,
) -> Result<working::Result<Income>, Error> {
    let table = table_for(year, status)?;
    if adj.residency == Residency::PartYearOrNonResident {
        return Err(Error::Invalid(
            "californiataxcalc: a part-year resident or non-resident files Form 540NR, \
             which apportions the tax by the share of income from California sources \
             and is not computed here"
                .to_string(),
        ));
    }

    let mut out = Income {
        california_agi: federal_agi - adj.subtractions + adj.additions,
        standard: table.standard_deduction,
        ..Income::default()
    };

    out.deduction = out.standard;
    if adj.election == Election::Itemise {
        (out.deduction, out.itemised) = (adj.itemised_deductions, true);
    } else if adj.election == Election::Standard {
        // Already the standard deduction.
    } else if adj.itemised_deductions > out.standard {
        (out.deduction, out.itemised) = (adj.itemised_deductions, true);
    }

    out.amount = out.california_agi - out.deduction;
    if out.amount.0 < 0 {
        out.amount = USCents(0);
    }

    let mut notes = Notes::default();
    notes.rule(
        "Cal. Rev. & Tax. Code § 17072",
        format!(
            "federal adjusted gross income of {federal_agi}, less {} of subtractions and plus {} of additions, gives California adjusted gross income of {}",
            adj.subtractions, adj.additions, out.california_agi
        ),
    );
    notes.rule(
        format!(
            "Cal. Rev. & Tax. Code § 17073.5, as adjusted for {}",
            table.year
        ),
        format!(
            "the California standard deduction for a {} filer is {}",
            table.status, out.standard
        ),
    );
    if adj.itemised_deductions.0 > 0 {
        notes.rule(
            "Cal. Rev. & Tax. Code § 17073",
            format!(
                "itemised deductions of {} against a standard deduction of {}, so {} was taken",
                adj.itemised_deductions, out.standard, out.deduction
            ),
        );
    }

    if adj.election == Election::Larger {
        notes.require(
            PositionKind::DeductionMethod,
            "California makes itemising an election of its own. Rev. & Tax. Code \
             § 17073.5 does not bind a filer to what the federal return elected, and \
             the California standard deduction is small enough that itemising often \
             wins here when it lost federally",
        );
    }
    if adj.residency == Residency::Unstated {
        notes.require(
            PositionKind::Residency,
            "these figures are a full-year resident's, who files Form 540. A part-year \
             resident or a non-resident files Form 540NR, which apportions the tax by the \
             share of income from California sources. Answer it with Adjustments.Residency",
        );
    }
    Ok(working::new_result(out).with(&notes).build()?)
}

/// The counts a filer enters on Form 540 lines 7 to 10.
///
/// Counts rather than amounts, because the AGI limitation reduces the credit
/// per exemption, so the arithmetic needs to know how many there are.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Exemptions {
    /// Line 7: one for a single, separate or head of household filer, two
    /// for a joint filer or surviving spouse.
    pub personal: i64,

    /// Line 8, one per qualifying person, so 0, 1 or 2.
    pub blind: i64,
    /// Line 9, one per qualifying person, so 0, 1 or 2.
    pub senior: i64,

    /// Line 10, one per dependent, and carries a much larger credit than the
    /// three above.
    pub dependents: i64,
}

/// Computes the Rev. & Tax. Code § 17054 credits, reduced by the adjusted
/// gross income limitation where it applies.
///
/// `federal_agi` decides the limitation, which is why it is wanted here as
/// well as in [`taxable_income`]. Above the threshold for the filing status,
/// each exemption loses six dollars for every whole or part step of AGI above
/// it — a step being $2,500, or $1,250 on a separate return.
///
/// The result is a credit against tax. A caller subtracts it from the figure
/// [`crate::ordinary_income_tax`] returned, floors the result at zero, and
/// only then adds any surcharge from
/// [`crate::behavioral_health_services_tax`].
pub fn exemption_credit(
    year: i64,
    status: Status,
    federal_agi: USCents,
    e: Exemptions,
) -> Result<working::Result<USCents>, Error> {
    let table = table_for(year, status)?;

    let personal_count = e.personal + e.blind + e.senior;
    let dependent_count = e.dependents;

    let mut personal = table.personal_exemption_credit * personal_count;
    let mut dependent = table.dependent_exemption_credit * dependent_count;

    let mut notes = Notes::default();
    notes.rule(
        format!("Cal. Rev. & Tax. Code § 17054, as adjusted for {}", table.year),
        format!(
            "{} personal, blind and senior exemptions at {} and {} dependents at {} give {} before any limitation",
            personal_count,
            table.personal_exemption_credit,
            e.dependents,
            table.dependent_exemption_credit,
            personal + dependent
        ),
    );

    let excess = federal_agi - table.exemption_phase_out_agi;
    if excess.0 <= 0 {
        return Ok(working::new_result(personal + dependent)
            .with(&notes)
            .build()?);
    }

    // The worksheet rounds the number of steps up, so a single dollar over
    // the threshold costs a whole step.
    let step = table.exemption_phase_out_step.0;
    let steps = (excess.0 + step - 1) / step;
    let per_exemption = table.exemption_phase_out_per_step * steps;

    personal -= per_exemption * personal_count;
    if personal.0 < 0 {
        personal = USCents(0);
    }
    dependent -= per_exemption * dependent_count;
    if dependent.0 < 0 {
        dependent = USCents(0);
    }

    notes.rule(
        "Cal. Rev. & Tax. Code § 17054(f)",
        format!(
            "adjusted gross income of {federal_agi} is {excess} above the {} threshold, which is {steps} step(s) of {}, so each exemption loses {per_exemption} and the credit becomes {}",
            table.exemption_phase_out_agi,
            table.exemption_phase_out_step,
            personal + dependent
        ),
    );

    Ok(working::new_result(personal + dependent)
        .with(&notes)
        .build()?)
}
