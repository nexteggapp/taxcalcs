//! Worksheet I of the FTB 5805 instructions, "Prorated Tax for Form 540NR
//! Filers", prorates a nonresident's or part-year resident's annualised tax
//! to California. Ported from worksheet_i.go.
//!
//! It is part of the annualised income installment method: FTB 5805 Part
//! III line 12 for a Form 540NR filer is Worksheet I line P, and the credits
//! on Part III line 13 are prorated by its line N. The regular method has no
//! proration; Part II takes the tax from Form 540NR as filed.
//!
//! The instructions don't say how many decimal places lines C, H and N
//! carry, or how the amounts round. The ratios here are exact and each
//! amount is rounded to the cent, half up, and the rules report that as
//! unsettled.

use estimated::Error;
use money::USCents;
use num_bigint::BigInt;
use num_integer::Integer;
use num_rational::BigRational;
use num_traits::{One, ToPrimitive};

/// Line E, the same as Part III line 2: 4, 2.4, 1.5 and 1.
fn worksheet_i_annualisation(i: usize) -> BigRational {
    let (n, d) = [(4, 1), (12, 5), (3, 2), (1, 1)][i];
    BigRational::new(BigInt::from(n), BigInt::from(d))
}

/// One period's figures for Worksheet I and the Part III lines that follow
/// it, each as the Form 540NR filer works it out for the period.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct WorksheetIColumn {
    tax_on_total: USCents,         // line A: Part III line 10
    total_taxable: USCents,        // line B: Part III line 9
    california_agi: USCents,       // line D: Schedule CA (540NR) Part IV line 1 for the period
    annualised_total_agi: USCents, // line G: Part III line 3
    deduction: USCents,            // line I: Part III line 8
    exemption_credits: USCents,    // line M: Part III line 11
    prorated_credits: USCents,     // Form 540NR lines 51 to 53, prorated by line N
    other_credits: USCents,        // the rest of Part III line 13, as refigured
    other_tax: USCents,            // Part III line 14b
}

impl WorksheetIColumn {
    /// Line A: the tax on the annualised total taxable income.
    pub fn tax_on_total(&self) -> USCents {
        self.tax_on_total
    }
    /// Line B: the annualised total taxable income.
    pub fn total_taxable(&self) -> USCents {
        self.total_taxable
    }
    /// Line D: California AGI for the period, before annualising.
    pub fn california_agi(&self) -> USCents {
        self.california_agi
    }
    /// Line G: the annualised total AGI, from all sources.
    pub fn annualised_total_agi(&self) -> USCents {
        self.annualised_total_agi
    }
    /// Line I: the larger of the annualised itemised deductions and the
    /// standard deduction.
    pub fn deduction(&self) -> USCents {
        self.deduction
    }
    /// Line M: Part III line 11.
    pub fn exemption_credits(&self) -> USCents {
        self.exemption_credits
    }
    /// The credits on Form 540NR lines 51 to 53.
    pub fn prorated_credits(&self) -> USCents {
        self.prorated_credits
    }
    /// The rest of Part III line 13.
    pub fn other_credits(&self) -> USCents {
        self.other_credits
    }
    /// Part III line 14b.
    pub fn other_tax(&self) -> USCents {
        self.other_tax
    }
}

/// Builds a [`WorksheetIColumn`].
#[derive(Clone, Debug, Default)]
pub struct WorksheetIColumnBuilder {
    c: WorksheetIColumn,
}

/// Starts a [`WorksheetIColumn`].
pub fn new_worksheet_i_column() -> WorksheetIColumnBuilder {
    WorksheetIColumnBuilder::default()
}

impl WorksheetIColumnBuilder {
    /// Line A: the tax on the annualised total taxable income, Part III line
    /// 10.
    pub fn tax_on_total(&mut self, a: USCents) -> &mut Self {
        self.c.tax_on_total = a;
        self
    }

    /// Line B: the annualised total taxable income, Part III line 9.
    pub fn total_taxable(&mut self, a: USCents) -> &mut Self {
        self.c.total_taxable = a;
        self
    }

    /// Line D: California AGI for the period, from Schedule CA (540NR) Part
    /// IV line 1, before annualising.
    pub fn california_agi(&mut self, a: USCents) -> &mut Self {
        self.c.california_agi = a;
        self
    }

    /// Line G: the annualised total AGI, Part III line 3, from all sources.
    pub fn annualised_total_agi(&mut self, a: USCents) -> &mut Self {
        self.c.annualised_total_agi = a;
        self
    }

    /// Line I: Part III line 8, the larger of the annualised itemised
    /// deductions and the standard deduction.
    pub fn deduction(&mut self, a: USCents) -> &mut Self {
        self.c.deduction = a;
        self
    }

    /// Line M: Part III line 11.
    pub fn exemption_credits(&mut self, a: USCents) -> &mut Self {
        self.c.exemption_credits = a;
        self
    }

    /// The credits on Form 540NR lines 51 to 53, which Part III line 13
    /// prorates by line N.
    pub fn prorated_credits(&mut self, a: USCents) -> &mut Self {
        self.c.prorated_credits = a;
        self
    }

    /// The rest of Part III line 13, Form 540NR lines 54 to 62 as refigured
    /// for the period.
    pub fn other_credits(&mut self, a: USCents) -> &mut Self {
        self.c.other_credits = a;
        self
    }

    /// Part III line 14b: the alternative minimum tax and behavioural health
    /// services tax for the period.
    pub fn other_tax(&mut self, a: USCents) -> &mut Self {
        self.c.other_tax = a;
        self
    }

    /// Refuses a negative figure, and a total taxable income or total AGI of
    /// zero, which lines C, H and N divide by.
    pub fn build(&self) -> Result<WorksheetIColumn, Error> {
        let c = self.c;
        for a in [
            c.tax_on_total,
            c.total_taxable,
            c.california_agi,
            c.annualised_total_agi,
            c.deduction,
            c.exemption_credits,
            c.prorated_credits,
            c.other_credits,
            c.other_tax,
        ] {
            if a.0 < 0 {
                return Err(Error::Invalid(
                    "californiataxcalc: a Worksheet I figure cannot be negative".into(),
                ));
            }
        }
        if c.total_taxable.0 == 0 || c.annualised_total_agi.0 == 0 {
            return Err(Error::Invalid(
                "californiataxcalc: Worksheet I divides by the total taxable income and the total AGI, so neither can be zero".into(),
            ));
        }
        Ok(c)
    }
}

/// One column's lines C to P, and Part III lines 13 and 14c that follow from
/// them.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct WorksheetILines {
    rate_c: BigRational,
    rate_h: BigRational,
    rate_n: BigRational,
    f: USCents,
    j: USCents,
    k: USCents,
    l: USCents,
    o: USCents,
    p: USCents,
    line13: USCents,
    line14c: USCents,
}

impl WorksheetILines {
    /// Line C, exactly.
    pub fn rate_c(&self) -> BigRational {
        self.rate_c.clone()
    }
    /// Line H, exactly.
    pub fn rate_h(&self) -> BigRational {
        self.rate_h.clone()
    }
    /// Line N, exactly.
    pub fn rate_n(&self) -> BigRational {
        self.rate_n.clone()
    }
    /// The annualised California AGI.
    pub fn f(&self) -> USCents {
        self.f
    }
    /// The prorated deduction.
    pub fn j(&self) -> USCents {
        self.j
    }
    /// The annualised California taxable income.
    pub fn k(&self) -> USCents {
        self.k
    }
    /// The annualised California prorated tax before exemption credits.
    pub fn l(&self) -> USCents {
        self.l
    }
    /// The annualised prorated exemption credits.
    pub fn o(&self) -> USCents {
        self.o
    }
    /// The annualised California tax after exemption credits: Part III line
    /// 12.
    pub fn p(&self) -> USCents {
        self.p
    }
    /// Part III line 13: the prorated credits and the other credits.
    pub fn line13(&self) -> USCents {
        self.line13
    }
    /// Part III line 14c: line 12 less line 13, not below zero, plus line
    /// 14b. It is the annualised tax the applicable percentage is taken of.
    pub fn line14c(&self) -> USCents {
        self.line14c
    }
}

/// Rounds an exact amount of cents half up, as Go's floored `big.Int`
/// division of 2n + d by 2d.
fn cents(x: &BigRational) -> USCents {
    let (n, d) = (x.numer(), x.denom());
    let q = (n * BigInt::from(2) + d).div_floor(&(d * BigInt::from(2)));
    USCents(q.to_i64().unwrap_or_default())
}

fn rat(a: USCents) -> BigRational {
    BigRational::from_integer(BigInt::from(a.0))
}

/// Works lines C to P of the column for the period numbered `i`, from 0, and
/// Part III lines 13 and 14c after them.
pub fn worksheet_i(i: i64, c: WorksheetIColumn) -> Result<WorksheetILines, Error> {
    if !(0..4).contains(&i) {
        return Err(Error::Invalid(format!(
            "californiataxcalc: Worksheet I has four columns, not column {}",
            i + 1
        )));
    }
    // A column made other than by its builder can hold the zeros Go's
    // big.Rat would panic dividing by.
    if c.total_taxable.0 == 0 || c.annualised_total_agi.0 == 0 {
        return Err(Error::Invalid(
            "californiataxcalc: Worksheet I divides by the total taxable income and the total AGI, so neither can be zero".into(),
        ));
    }
    // C. Annualized California tax rate: line A ÷ line B.
    let rate_c = rat(c.tax_on_total) / rat(c.total_taxable);
    // F. Annualized California AGI: line D × line E.
    let f = cents(&(rat(c.california_agi) * worksheet_i_annualisation(i as usize)));
    // H. Line F ÷ line G, not more than 1.0000.
    let mut rate_h = rat(f) / rat(c.annualised_total_agi);
    if rate_h > BigRational::one() {
        rate_h = BigRational::one();
    }
    // J. Line I × line H.
    let j = cents(&(rat(c.deduction) * &rate_h));
    // K. Line F − line J.
    let k = f - j;
    // L. Line K × line C.
    let l = cents(&(rat(k) * &rate_c));
    // N. Line K ÷ line B.
    let rate_n = rat(k) / rat(c.total_taxable);
    // O. Line M × line N.
    let o = cents(&(rat(c.exemption_credits) * &rate_n));
    // P. Line L − line O: Part III line 12.
    let p = l - o;
    // Part III line 13: lines 51 to 53 prorated by line N, and the rest.
    let line13 = cents(&(rat(c.prorated_credits) * &rate_n)) + c.other_credits;
    // Part III lines 14a to 14c.
    let line14c = (p - line13).max(USCents(0)) + c.other_tax;
    Ok(WorksheetILines {
        rate_c,
        rate_h,
        rate_n,
        f,
        j,
        k,
        l,
        o,
        p,
        line13,
        line14c,
    })
}
