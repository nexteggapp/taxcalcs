//! Rhode Island's estimated tax rules, R.I. Gen. Laws §§ 44-30-55, 44-30-56
//! and 44-30-84, as the shared [`EstimatedTax`] interface.
//!
//! The shape is federal: the smaller of a share of this year's tax and all
//! of last year's, four equal installments, simple interest on each
//! shortfall. The numbers are not: 80% rather than 90%, 100% of last year's
//! tax at every income, a $250 floor, and a rate set once for each calendar
//! year, 12% for 2026. Due dates move for Rhode Island's own legal holidays.
//!
//! The figure follows the Division of Taxation's forms, the 2026 RI-1040ES
//! and the 2025 RI-2210, the latest published. The result's Divergences say
//! where the General Laws read otherwise. No law in 2024, 2025 or 2026
//! amended the sections these rules rest on
//! (research/estimated-payments/ri.md), so no form here predates the law it
//! describes.

use std::any::Any;
use std::fmt;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Holiday, Installment,
    Jurisdiction, LedgerEntry, Month, Penalty, RateSeries, Requirement, TargetOptionBuilder,
    WaiverKind,
};
use money::{DOLLAR, Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

/// jurisdiction is Rhode Island's postal code.
const JURISDICTION: Jurisdiction = estimated::RHODE_ISLAND;

/// The reading used for a return filed early in place of the January
/// installment. The Division's two current forms give different dates, and
/// no public source says which one its computation applies.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum JanuaryReturn {
    /// No answer. The rules then use [`JanuaryReturn::PenaltyForm`] and say
    /// so in Required when the answer would change the figure.
    #[default]
    Unanswered,
    /// The RI-2210 reading: a return filed with the balance paid on or
    /// before January 31 counts in column D, as paid by the January due
    /// date. RI-2210 (2025), line 16 instructions.
    PenaltyForm,
    /// The RI-1040ES reading: the January payment is not needed if the
    /// return is filed and paid by February 15. That is the fifteenth day of
    /// the second month in § 44-30-55(e), and § 44-30-91(b) moves it past a
    /// weekend or Rhode Island legal holiday.
    Coupon,
}

/// Names the reading, as Go's `String()`.
impl fmt::Display for JanuaryReturn {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            JanuaryReturn::PenaltyForm => "RI-2210, January 31",
            JanuaryReturn::Coupon => "RI-1040ES, February 15",
            JanuaryReturn::Unanswered => "unanswered",
        })
    }
}

/// The facts and readings Rhode Island's rules read that no other
/// jurisdiction does. Carry them in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    no_federal_liability: Option<bool>,
    january: JanuaryReturn,
    minimum: Option<bool>,
}

impl EstimatedFacts {
    /// Whether last year's federal return, for a full twelve months, showed
    /// no federal tax liability, or would have had one been required, or
    /// `None` when the caller has not said. RI-2210 line 5 lets a filer who
    /// was also a Rhode Island resident all last year enter zero for last
    /// year's tax. [`Facts::prior_year`] is last year's Rhode Island return,
    /// which does not carry it.
    pub fn prior_year_no_federal_liability(&self) -> Option<bool> {
        self.no_federal_liability
    }

    /// The reading to use for a return filed and paid between the two
    /// forms' dates.
    pub fn january_return(&self) -> JanuaryReturn {
        self.january
    }

    /// The caller's reading of whether § 44-30-84(a)(1), which makes
    /// interest under $2.00 not payable, reaches the interest on estimates,
    /// or `None` when the caller gave none.
    pub fn two_dollar_minimum(&self) -> Option<bool> {
        self.minimum
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
    /// Says whether last year showed no federal tax liability.
    pub fn prior_year_no_federal_liability(&mut self, none: bool) -> &mut Self {
        self.e.no_federal_liability = Some(none);
        self
    }

    /// Sets the reading for a return filed in place of the January
    /// installment.
    pub fn january_return(&mut self, j: JanuaryReturn) -> &mut Self {
        self.e.january = j;
        self
    }

    /// Gives the reading of the $2 minimum.
    pub fn two_dollar_minimum(&mut self, applies: bool) -> &mut Self {
        self.e.minimum = Some(applies);
        self
    }

    /// Builds the facts. Go refuses a reading it does not know; every
    /// [`JanuaryReturn`] is one this crate knows.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,
    /// RI-2210 line 4: no charge when this year's tax less withholding is
    /// this or less.
    pub(crate) floor: USCents,
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,
    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    /// The RI-2210 date for a return that counts in column D.
    pub(crate) penalty_form_by: Date,
    /// The RI-1040ES date, before moving for a holiday.
    pub(crate) coupon_by: Date,
    /// The fifteenth day of the fourth month after the year.
    pub(crate) stop: Date,
    pub(crate) period_ends: Vec<Date>,
    pub(crate) applicable: Vec<Rate>,
    pub(crate) rates: RateSeries,
    /// § 44-30-84(a)(1): interest under it is not payable.
    pub(crate) minimum: USCents,
}

/// A year's figures, refused when a rate period is not one calendar year.
/// § 44-1-7(b) sets the rate each January 1 for that calendar year.
fn year_for(year: i64) -> Result<EstimatedYear, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    for p in y.rates.periods() {
        let jan1 = estimated::must_date(p.from().year(), Month::JANUARY, 1);
        let dec31 = estimated::must_date(p.from().year(), Month::DECEMBER, 31);
        if p.from() != jan1 || p.to() != dec31 {
            return Err(Error::Invalid(format!(
                "rhodeislandtaxcalc: the rate from {} to {} is not one calendar year",
                p.from(),
                p.to()
            )));
        }
    }
    Ok(y)
}

/// RI-2210 line 13, the required annual payment divided by four, and line
/// 16, withholding deemed paid one-fourth on each due date.
const QUARTERS: [Rate; 4] = [Rate(2500), Rate(2500), Rate(2500), Rate(2500)];

/// Returns Rhode Island's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = year_for(year)?;
    Ok(Box::new(RhodeIslandEstimated {
        y,
        explanation: rhode_island_explanation()?,
    }))
}

/// A year's holiday calendar, so that the tests can check each date against
/// the rule in § 25-1-1 that makes it one. Not part of the crate's API.
#[doc(hidden)]
pub fn holidays(year: i64) -> Result<Vec<Holiday>, Error> {
    Ok(year_for(year)?.calendar.holidays())
}

struct RhodeIslandEstimated {
    y: EstimatedYear,
    explanation: Explanation,
}

#[derive(Clone, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    /// RI-2210 line 15: line 13, or line 14 under the annualised method.
    installments: Vec<USCents>,
    /// A return filed and paid in time to count in column D.
    january_met: bool,
    /// Which divergences change these facts' figures.
    affects: Affects,
    notes: Notes,
}

/// Records, for each divergence, whether its two readings give these facts
/// different figures or answers.
#[derive(Clone, Copy, Default)]
struct Affects {
    prior_option: bool,
    floor: bool,
    no_liability: bool,
    january: bool,
    late_start: bool,
    minimum: bool,
    /// The statute's $2 minimum, applied by the reading given.
    minimum_applied: bool,
}

/// Go's `%T` for the extras a caller passed, as far as Rust can tell it.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else if v.is::<()>() {
        "struct {}"
    } else {
        "unknown"
    }
}

/// The holiday's common name, the part of the calendar's name in
/// parentheses.
fn holiday_name(c: &Calendar, d: Date) -> String {
    if let Some(name) = c.is_holiday(d) {
        if let (Some(i), Some(j)) = (name.rfind('('), name.rfind(')'))
            && j > i
        {
            return name[i + 1..j].to_string();
        }
        return name;
    }
    "a weekend".into()
}

impl RhodeIslandEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "rhodeislandtaxcalc: the facts carry {}, not rhodeislandtaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "rhodeislandtaxcalc: these are Rhode Island's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "rhodeislandtaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or fisherman uses 66.67% and column D alone, and owes nothing after filing and paying by March 1 (RI-2210 instructions), which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "rhodeislandtaxcalc: RI-2210 Part 5 period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the Division's forms and the General Laws part ways on a rule
    /// every Rhode Island computation uses. Items 10 and "Conflicts
    /// resolved" in research/estimated-payments/ri.md cover all but the late
    /// start, which is § 44-30-56(a) read against RI-2210 line 13.
    fn divergences(&self, af: Affects) -> Result<Vec<Divergence>, Error> {
        let minimum = if af.minimum_applied {
            Reading::Statute
        } else {
            Reading::Form
        };
        let mut out = Vec::new();
        for b in [
            working::new_divergence("The prior-year option")
                .form(
                    "Your target is the smaller of two figures. One is 80% of this year's tax; the other, 100% of last year's Rhode Island tax. There is no condition of a full year or of residence.",
                    "RI-2210 (2025), Part 1, lines 2, 5 and 6; RI-1040ES (2026), worksheet lines 6 and 7",
                )
                .statute(
                    "Interest is charged when payments fall short of 80% of each installment. The law adopts only the federal small-tax and no-liability exceptions. It creates no prior-year option.",
                    "R.I. Gen. Laws § 44-30-84(b)",
                )
                .uses(Reading::Form)
                .affects(af.prior_option),
            working::new_divergence("The $250 floor")
                .form(
                    "This year's tax less Rhode Island withholding of $250 or less means no charge.",
                    "RI-2210 (2025) and RI-2210A (2025), line 4",
                )
                .statute(
                    "Estimated payments are required when the tax is $250 or more above credits. Exactly $250 therefore needs them. The law adopts the federal small-tax exception. It doesn't say whether that amount is $250 or the federal $1,000.",
                    "R.I. Gen. Laws §§ 44-30-55(a), 44-30-84(b)",
                )
                .uses(Reading::Form)
                .affects(af.floor),
            working::new_divergence("No liability last year")
                .form(
                    "Last year's tax counts as zero if you had no federal tax liability for it. That needs a full-year federal return and Rhode Island residence all year.",
                    "RI-2210 (2025) and RI-2210A (2025), line 5 instructions",
                )
                .statute(
                    "The federal no-liability exception applies to Rhode Island amounts. The test is then no Rhode Island liability last year. The 100% option already sets a zero target when last year's tax was zero.",
                    "R.I. Gen. Laws § 44-30-84(b)",
                )
                .uses(Reading::Form)
                .affects(af.no_liability),
            working::new_divergence("A return filed in place of the January installment")
                .form(
                    "A return filed and paid on or before January 31 counts as paid by January 15. January 31 doesn't move for a weekend. The payment coupon says February 15, moved for weekends and state holidays.",
                    "RI-2210 (2025), line 16 instructions; RI-1040ES (2026), When and where to make estimates",
                )
                .statute(
                    "The return counts as the declaration if filed and paid by a set date. That date is the fifteenth day of the second month after the year. It doesn't count if one was required earlier. Feb 15, 2027 is Washington's Birthday, so Feb 16 is on time. A missed January payment's interest isn't waived.",
                    "R.I. Gen. Laws §§ 44-30-55(e), 44-30-91(b), 25-1-1",
                )
                .uses(Reading::Form)
                .affects(af.january),
            working::new_divergence("A requirement first met after April 15")
                .form(
                    "The required annual payment is divided by four. A quarter is due on each date, whenever the requirement arose.",
                    "RI-2210 (2025), Part 3, line 13",
                )
                .statute(
                    "Installments follow the date the declaration was due. If due June 15, pay equal installments on June 15, September 15 and January 15. If due September 15, pay on September 15 and January 15. If due January 15, pay all at once.",
                    "R.I. Gen. Laws §§ 44-30-55(c), 44-30-56(a)",
                )
                .uses(Reading::Form)
                .affects(af.late_start),
            working::new_divergence("Interest under $2")
                .form(
                    "Every charge is payable, however small. The penalty form has no minimum.",
                    "RI-2210 (2025), Part 4, lines 18 to 22",
                )
                .statute(
                    "Interest under $2 is not payable. That sentence is in the subsection on tax paid late. The subsection on estimates doesn't repeat it.",
                    "R.I. Gen. Laws § 44-30-84(a)(1), (b)",
                )
                .uses(minimum)
                .affects(af.minimum),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// `d` as a person reads it: "Dec 15" in the tax year, "Jan 15, 2027"
    /// outside it.
    fn date(&self, d: Date) -> String {
        if d.year() == self.y.year {
            return d.format("Jan 2");
        }
        d.format("Jan 2, 2006")
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let last = y.year - 1;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        for (i, due) in a.dues.iter().enumerate() {
            if *due != y.dues[i] {
                a.notes.rule(
                    "R.I. Gen. Laws § 44-30-91(b)",
                    format!(
                        "the installment due {} falls on a weekend or Rhode Island legal holiday, so it is due {}",
                        y.dues[i].long(),
                        due.long()
                    ),
                );
            }
        }
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();
        let prior_tax = if has_prior {
            prior.figures().and_then(|pf| pf.tax()).unwrap_or_default()
        } else {
            USCents(0)
        };
        // 80% of this year's tax, RI-2210 line 2.
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
                "RI-2210 (2025), Part 1, lines 1 and 2",
                format!(
                    "{} of this year's Rhode Island tax after credits of {tax} is {}",
                    y.this_year_share,
                    this_year.amount()
                ),
            );
        }
        // 100% of last year's Rhode Island tax, RI-2210 line 5. The form sets
        // no condition on the return beyond its tax.
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Rhode Island return"))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Rhode Island return"))
        } else {
            let amount = y.prior_year_share.of(prior_tax);
            a.notes.rule(
                "RI-2210 (2025), Part 1, line 5; RI-1040ES (2026), worksheet line 6",
                format!(
                    "{} of the {prior_tax} tax on the {last} Rhode Island return is {amount}",
                    y.prior_year_share
                ),
            );
            if prior.months() != 12 {
                a.notes.rule(
                    "RI-2210 (2025), Part 1, line 5",
                    format!(
                        "the {last} return covered {} months; the form sets no condition of a twelve-month year on line 5, so the option stands",
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
        // RI-2210 line 5: zero for last year's tax after a year with no
        // federal liability, as a Rhode Island resident, on a twelve-month
        // federal return.
        let no_federal_answer = extras.prior_year_no_federal_liability();
        let no_federal = no_federal_answer == Some(true);
        let residence = prior.full_year_resident();
        let no_liability = no_federal && has_prior && residence == Some(true);
        if no_federal && (!has_prior || residence.is_none()) {
            a.notes.ask(
                PositionKind::Residency,
                format!("Did you live in Rhode Island all of {last}?"),
                format!(
                    "If so, no federal tax liability for {last} means no estimated payments are needed."
                ),
            );
        }
        if no_liability {
            a.exempt = true;
            let ex = estimated::new_exception("No federal tax liability last year").build()?;
            rb.required(
                false,
                format!(
                    "You had no federal tax liability for {last} and lived in Rhode Island all of it. Last year's tax therefore counts as zero. No estimated payments are needed for {}.",
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "RI-2210 (2025), Part 1, lines 5 and 6",
                format!(
                    "no federal liability for {last} on a twelve-month return, and a full year of Rhode Island residence, so line 5 is zero and there is no charge for {}",
                    y.year
                ),
            );
        } else if tax_known && tax - withheld <= y.floor {
            a.exempt = true;
            let ex = estimated::exception_at_most("Tax less withholding", tax - withheld, y.floor)?;
            let mut why = format!(
                "Your Rhode Island tax after credits is {}, and {} is withheld. That leaves {}, which is {} or less. No estimated payments are needed.",
                tax.whole(),
                withheld.whole(),
                (tax - withheld).whole(),
                y.floor.whole()
            );
            if tax < withheld {
                why = format!(
                    "Your Rhode Island tax after credits is {}, and {} is withheld. That {}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                );
            } else if tax - withheld == y.floor {
                why = format!(
                    "Your Rhode Island tax after credits is {}, and {} is withheld. That leaves exactly {}. The law asks for estimated payments at {} or more. The penalty form charges nothing at {} or less.",
                    tax.whole(),
                    withheld.whole(),
                    y.floor.whole(),
                    y.floor.whole(),
                    y.floor.whole()
                );
            }
            rb.required(false, why).exempt(ex);
            a.notes.rule(
                "RI-2210 (2025), Part 1, line 4",
                format!(
                    "{tax} of tax less {withheld} withheld is {}, not more than {}, so there is no charge",
                    std::cmp::max(USCents(0), tax - withheld),
                    y.floor
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            let mut why = format!(
                "Your target is the smaller of two figures. They are 80% of your {} Rhode Island tax and 100% of your {last} tax.",
                y.year
            );
            if has_prior && !prior.filed() {
                why = format!(
                    "Your target is 80% of your {} Rhode Island tax. That is because you didn't file a {last} return.",
                    y.year
                );
            }
            rb.required(false, why).no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} covers 100% of your {last} tax, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
                    withheld.whole(),
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is 100% of your {last} tax, {}. It keeps you clear of the charge whatever your {} tax turns out to be. After {} of withholding, that's {} of estimated payments. Pay a quarter by each date.",
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
                    "Your Rhode Island withholding is {}. That covers the {} needed for the year. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Rhode Island tax after credits is {}, and {} is withheld. That leaves {} due, and withholding falls short of your {} target. You need estimated payments of {}, a quarter by each date.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;
        // An exception sets the target, so neither figure can change it.
        if !has_prior && !a.exempt {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Rhode Island return, and what was its tax?"),
                "With it, 100% of last year's tax could set a lower target.",
            );
        }
        if !tax_known && !a.exempt {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!(
                    "What do you expect your {} Rhode Island tax after credits to be?",
                    y.year
                ),
                format!(
                    "With it, 80% of this year's tax could be your target. A balance of {} or less would need no payments.",
                    y.floor.whole()
                ),
            );
        }
        if !a.exempt && a.requirement.target_known() && no_federal_answer.is_none() {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!("Did you have no federal tax liability for {last}, on a full-year return?"),
                "If so, you also need to have lived in Rhode Island all that year. Then no estimated payments are needed.",
            );
        }
        // What each divergence turns on in these facts.
        let balance = tax - withheld;
        a.affects.prior_option =
            !a.exempt && a.requirement.target_known() && chosen == Basis::PriorYearTax;
        a.affects.floor =
            tax_known && !no_liability && balance >= y.floor && balance < DOLLAR * 1_000;
        a.affects.no_liability = no_liability && prior_tax.0 > 0;
        let met = f.requirement_first_met();
        if !a.exempt && !met.is_zero() && met.after(y.dues[0]) {
            a.affects.late_start = true;
        }
        if a.exempt || !a.requirement.target_known() {
            a.installments = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }
        a.installments = self.installments(f, &mut a, target)?;
        self.january(f, &extras, &mut a);
        Ok(a)
    }

    /// RI-2210 line 15: line 13, the required annual payment divided by
    /// four, or line 14 under the annualised method.
    fn installments(
        &self,
        f: &Facts,
        a: &mut Assessment,
        target: USCents,
    ) -> Result<Vec<USCents>, Error> {
        let y = &self.y;
        let cum = estimated::cumulative_shares(target, &QUARTERS)?;
        let regular = estimated::increments(&cum);
        a.notes.rule(
            "R.I. Gen. Laws § 44-30-56(a); RI-1040ES (2026); RI-2210 (2025), Part 3, line 13",
            format!(
                "a quarter of {target}, {}, is due on each of {}, {}, {} and {}",
                regular[0],
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );
        let met = f.requirement_first_met();
        if !met.is_zero() && met.after(y.dues[0]) {
            a.notes.rule(
                "RI-2210 (2025), Part 3, line 13",
                format!(
                    "the requirement first arose on {}; the form still requires a quarter on each of the four dates, so the earlier ones are short until caught up (see Divergences for § 44-30-56(a))",
                    met.long()
                ),
            );
        }
        if !f.elections().annualised() {
            return Ok(regular);
        }
        let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
        let ann = estimated::annualised(&taxes, &y.applicable, &regular)?;
        a.notes.rule(
            "RI-2210 (2025), Part 5, lines 23 to 33",
            format!(
                "under the annualised method the installments are {}, {}, {} and {}: 20%, 40%, 60% and 80% of the Rhode Island tax on annualised income, less earlier installments, and never more than a quarter of {target} plus any reduction carried forward",
                ann[0], ann[1], ann[2], ann[3]
            ),
        );
        Ok(ann)
    }

    /// Decides whether the return was filed and paid in time to count in
    /// column D. Under RI-2210 that payment is treated as made by January
    /// 15, so the January installment is met and nothing runs after that
    /// day.
    fn january(&self, f: &Facts, extras: &EstimatedFacts, a: &mut Assessment) {
        let y = &self.y;
        let filed = f.return_filed();
        if filed.is_zero() {
            return;
        }
        let coupon = y.calendar.next_business_day(y.coupon_by);
        let reading = extras.january_return();
        let mut by = y.penalty_form_by;
        if reading == JanuaryReturn::Coupon {
            by = coupon;
        }
        if reading == JanuaryReturn::Unanswered
            && filed.after(y.penalty_form_by)
            && !filed.after(coupon)
        {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "Will the Division count your return filed and paid {} toward January?",
                    self.date(filed)
                ),
                format!(
                    "Until then, January counts as missed; the penalty form's date is {}. The coupon's {} moves to {} for {}.",
                    self.date(y.penalty_form_by),
                    self.date(y.coupon_by),
                    self.date(coupon),
                    holiday_name(&y.calendar, y.coupon_by)
                ),
            );
        }
        a.affects.january = !filed.after(coupon);
        if filed.after(by) {
            return;
        }
        a.january_met = true;
        let cite = if reading == JanuaryReturn::Coupon {
            "RI-1040ES (2026), When and where to make estimates; R.I. Gen. Laws §§ 44-30-55(e), 44-30-91(b)"
        } else {
            "RI-2210 (2025), line 16 instructions"
        };
        a.notes.rule(
            cite,
            format!(
                "the return was filed with the balance paid on {}, by {}, so the payment counts in column D as paid by {}: the January installment is met, and no shortfall runs after that day",
                filed.long(),
                by.long(),
                a.dues[3].long()
            ),
        );
    }

    /// Applies the credits to the required installments.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &QUARTERS)?
        };
        let mut required = a.installments.clone();
        if a.january_met {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "RI-2210 (2025), line 16 instructions",
                format!(
                    "the {} withheld is credited on the dates it was withheld, as the filer can show",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "RI-2210 (2025), line 16 instructions",
                format!(
                    "the {} withheld is treated as paid one-fourth on each due date, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        notes.rule(
            "RI-2210 (2025), line 17 instructions",
            "an underpayment carries forward and is paid by the next payments first, and an overpayment carries forward to the next installment",
        );
        Ok((ledger, notes))
    }

    /// The installments from the ledger. A January installment met by the
    /// return is excused.
    fn schedule_of(
        &self,
        a: &Assessment,
        ledger: &[LedgerEntry],
    ) -> Result<Vec<Installment>, Error> {
        let mut out = Vec::with_capacity(ledger.len());
        let mut needed = USCents(0);
        for (i, l) in ledger.iter().enumerate() {
            needed += a.installments[i];
            let mut b = estimated::new_installment(i as i64 + 1, l.due());
            b.needed_by(needed)
                .credited_by(l.credited_by())
                .shortfall(l.shortfall());
            if a.january_met && i == 3 {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn diverges(&self, af: Affects) -> Result<Notes, Error> {
        let ds = self.divergences(af)?;
        let mut notes = Notes::default();
        notes.diverges(&ds);
        Ok(notes)
    }
}

impl EstimatedTax for RhodeIslandEstimated {
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, Error> {
        let a = self.assess(f)?;
        let notes = self.diverges(a.affects)?;
        Ok(working::new_result(a.requirement)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, Error> {
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Rhode Island return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let out = self.schedule_of(&a, &ledger)?;
        let ds = self.divergences(a.affects)?;
        notes.diverges(&ds);
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "rhodeislandtaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let mut a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Rhode Island return to measure against, there is no target to figure a penalty from",
                    self.y.year - 1
                ),
            ));
        }
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut stop = as_of;
        if y.stop.before(stop) {
            stop = y.stop;
        }
        let mut paid_in_full = false;
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(stop) {
            stop = filed;
            paid_in_full = true;
        }
        if a.january_met && a.dues[3].before(stop) {
            stop = a.dues[3];
        }
        let waiver_answer = f.positions().waiver();
        let waiver = waiver_answer.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let schedule = self.schedule_of(&a, &ledger)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            let dn = self.diverges(a.affects)?;
            return Ok(working::new_result(p).with(&a.notes).with(&dn).build()?);
        }
        let (mut p, unrated) =
            estimated::simple_penalty(&ledger, stop, &y.rates, waiver, &a.requirement, &schedule)?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "RI-2210 (2025), Part 1, line 6",
                format!(
                    "this year's Rhode Island tax was not given, so the charge is figured against 100% of the {} tax alone; the required annual payment is the smaller of that and 80% of this year's tax, so {} is the most the charge can be, and it is less if this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full && !a.january_met {
            notes.rule(
                "R.I. Gen. Laws § 44-30-84(b); RI-2210 (2025), line 18 instructions",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "R.I. Gen. Laws §§ 44-30-84(b), 44-1-7; RI-2210 (2025), Part 4, lines 18 to 22",
            format!(
                "each shortfall is charged simple interest by the day, at the rate for each calendar year over 365 days, from its due date until paid or {}, with each payment going to the oldest shortfall first; the charge to {} is {}",
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
                    y.rates.ends().format("Jan 2, 2006")
                ),
            );
        }
        if p.total().0 > 0 && p.total() < y.minimum {
            let reading = extras.two_dollar_minimum();
            a.affects.minimum = true;
            a.affects.minimum_applied = reading == Some(true);
            match reading {
                None => {
                    notes.ask(
                        PositionKind::JurisdictionFact,
                        format!(
                            "Does the Division apply its under-{} rule to your estimated tax charge?",
                            y.minimum.whole()
                        ),
                        format!(
                            "Only the law on tax paid late says interest under {} isn't payable. Until then, the charge counts in full.",
                            y.minimum.whole()
                        ),
                    );
                }
                Some(true) => {
                    notes.rule(
                        "R.I. Gen. Laws § 44-30-84(a)(1)",
                        format!(
                            "by the reading given, the {} charge is under {} and is not payable",
                            p.total(),
                            y.minimum
                        ),
                    );
                    let mut pb = estimated::new_penalty(stop);
                    pb.waiver(waiver).because(a.requirement.clone(), &schedule);
                    if !a.tax_known {
                        pb.upper_bound();
                    }
                    p = pb.build()?;
                }
                Some(false) => {}
            }
        }
        if p.total().0 > 0 && waiver_answer.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Will you ask the Division to waive the charge?",
                "The federal waivers cover a casualty, disaster, retiring after 62 or disability. The charge shown is before any waiver; Rhode Island hasn't said it grants them.",
            );
        } else if waiver_answer.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "280-RICR-20-55-1 § 1.6(A)(1)",
                format!(
                    "a waiver for {waiver} is asked for; the Division decides it, and no Rhode Island source says it grants one, so the charge shown is before it"
                ),
            );
            if p.total().0 > 0 {
                // § 44-30-84(b) adopts only § 6654(e)(1) and (2); the
                // regulation says federal exceptions "may be followed". Facts
                // the federal waiver refuses are refused; any other is the
                // Division's call.
                let mut o = estimated::standard_waiver(f, "the Division", 62).unwrap_or_default();
                if !o.refused() {
                    o = estimated::waiver_undecided(
                        "Rhode Island law doesn't adopt the federal waivers. Its rules only say federal exceptions may be followed, so the Division decides.",
                    );
                    notes.ask(
                        PositionKind::UnsettledRule,
                        "",
                        "Rhode Island hasn't said whether the Division grants the federal waivers. They cover a casualty, disaster, retirement or disability.",
                    );
                }
                p = p.to_builder().waiver_outcome(o).build()?;
            }
        }
        let ds = self.divergences(a.affects)?;
        notes.diverges(&ds);
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        self.explanation.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/ri.md.
fn rhode_island_explanation() -> Result<Explanation, Error> {
    estimated::new_explanation(
        "Take your Rhode Island tax after credits and withholding. If it comes to $250 or more, Rhode Island expects estimated payments. The charge for paying too little is interest on each quarter's shortfall. It is 12% a year for 2026.",
    )
    .paragraph("There is no charge if withholding and estimated payments reach a target. The target is the smaller of 80% of this year's tax or 100% of last year's. Last year's figure stays at 100% however high your income was. There is also no charge if this year's tax less withholding is $250 or less. There is none either if you had no federal tax liability last year. You must also have lived in Rhode Island all that year.")
    .paragraph("Pay four equal quarters, by April 15, June 15, September 15 and January 15. The January date is in the next year. A weekend or Rhode Island legal holiday shifts a date to the next business day. None of the 2026 dates falls on one. Withholding counts as paid evenly on the four dates. You can instead show when it was actually withheld. A payment goes first to the oldest shortfall.")
    .paragraph("The interest runs by the day from each due date until the shortfall is paid. It stops on April 15 of the next year. The rate is set once a year, at the prime rate plus 2%, but never below 12%. It was 12% for 2025 and is 12% for 2026. The annualised method on Form RI-2210 can lower early quarters. It helps when income arrives late in the year.")
    .paragraph("Filing your return early can stand in for the January payment. The penalty form counts a return filed and paid by January 31. The payment coupon says February 15. For 2026 the later date is February 16, 2027. February 15 is Washington's Birthday. The Division hasn't said which date it applies.")
    .versus_federal("Rhode Island uses 80% of this year's tax rather than 90%. It uses 100% of last year's at every income, with no 110% tier. Its floor is $250 rather than $1,000. Its rate is fixed for each calendar year rather than reset each quarter. Due dates move for Rhode Island's own holidays. The law adopts no waiver for a casualty, retirement or disability.")
    .source("R.I. Gen. Laws §§ 44-30-55, 44-30-56, 44-30-84, 44-30-91, 44-1-7 and 25-1-1")
    .source("280-RICR-20-55-1, Estimated Payments")
    .source("2026 RI-1040ES, Rhode Island Resident and Nonresident Estimated Payment Coupons")
    .source("2025 Form RI-2210 and its instructions, and 2025 Form RI-2210A")
    .source("ADV 2025-23, Interest rates posted for 2026 (November 26, 2025)")
    .build()
}
