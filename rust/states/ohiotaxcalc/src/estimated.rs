//! Ohio's estimated income tax rules, Ohio Revised Code 5747.09, as the
//! shared [`EstimatedTax`] interface.
//!
//! Ohio's rules look like the federal ones in outline: 90% of this year's
//! tax or 100% of last year's, in four quarters. They differ in how the
//! penalty is figured. The department's form, IT/SD 2210, first tests the
//! whole year: if withholding and timely payments reach the target, there is
//! no penalty however they were spread. Otherwise it compares cumulative
//! amounts at each due date and charges each shortfall at one factor for the
//! span to the next due date, the last span ending April 15. A payment made
//! between two due dates counts only from the next one.
//!
//! The figure follows the 2025 IT/SD 2210, the latest published, and the
//! 2026 IT 1040ES. The result's Divergences say where ORC 5747.09 reads
//! otherwise. HB 96 amended 5747.09 from tax year 2025, effective September
//! 30, 2025; the 2025 IT/SD 2210 was published after it, and the amendment
//! changed no figure the form computes, so the form is followed. The 2026 IT
//! 1040ES worksheet prints 2025's tax brackets, which HB 96 replaced for
//! 2026. These rules take this year's tax as given, so they use no bracket,
//! and the divergence says which brackets to compute it with.
//!
//! Ohio's school district income tax and city income taxes take their own
//! estimated payments, and none of them is covered here.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Factor, Facts, Installment,
    Jurisdiction, LedgerEntry, Penalty, RateSeries, Requirement, Rounding, TargetOptionBuilder,
    WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

const JURISDICTION: Jurisdiction = estimated::OHIO;

/// The facts Ohio's rules read that no other jurisdiction does. Carry them
/// in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_timely: Option<bool>,
}

impl EstimatedFacts {
    /// Whether last year's Ohio return was filed on time, or `None` when the
    /// caller has not said. IT/SD 2210 line 6 offers last year's tax as the
    /// target only after a timely return.
    pub fn prior_year_return_timely(&self) -> Option<bool> {
        self.prior_timely
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
    /// Answers whether last year's Ohio return was filed on time. `false` is
    /// an answer.
    pub fn prior_year_return_timely(&mut self, timely: bool) -> &mut Self {
        self.e.prior_timely = Some(timely);
        self
    }

    /// Builds the facts; every answer is accepted.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,
    /// The tax after credits, less withholding, at or under which no
    /// estimated payments are needed and no penalty arises.
    pub(crate) threshold: USCents,
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,
    /// Each column's step in IT/SD 2210 lines 10 and 11: 25% each, so 25%,
    /// 50%, 75% and 100% by the four dates.
    pub(crate) shares: Vec<Rate>,
    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,
    /// Where the form's last span ends: the return's due date.
    pub(crate) stop: Date,
    pub(crate) rates: RateSeries,
    /// The days in the form's year, in hundredths: 365.25 is 36525.
    pub(crate) day_count: i64,
    /// How many decimal places the form prints a factor to.
    pub(crate) places: u32,
    /// How a charge is rounded.
    pub(crate) rounding: Rounding,
}

fn year_for(year: i64) -> Result<EstimatedYear, Error> {
    match year {
        2026 => year_2026(),
        _ => Err(estimated::no_rules(&JURISDICTION, year)),
    }
}

/// Returns Ohio's estimated tax rules for a tax year, or [`Error::NoRules`]
/// for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = year_for(year)?;
    Ok(Box::new(OhioEstimated {
        y,
        explanation: ohio_explanation()?,
    }))
}

/// The IT/SD 2210 line 15 factor a year's rules work out for the days after
/// `from` up to and including `to`, at the given rates, as printed. It is
/// empty when no day of the span has a rate. For the tests: not part of the
/// crate's API.
#[doc(hidden)]
pub fn factor_for(year: i64, from: Date, to: Date, rates: RateSeries) -> Result<String, Error> {
    let mut y = year_for(year)?;
    y.rates = rates;
    let sf = y.factor(from, to)?;
    if !sf.charged {
        return Ok(String::new());
    }
    Ok(sf.factor.to_string())
}

/// The holiday calendar a year's rules move due dates by. For the tests:
/// not part of the crate's API.
#[doc(hidden)]
pub fn calendar_for(year: i64) -> Result<Calendar, Error> {
    Ok(year_for(year)?.calendar)
}

/// The rate series a year's rules charge at. For the tests: not part of the
/// crate's API.
#[doc(hidden)]
pub fn rates_for(year: i64) -> Result<RateSeries, Error> {
    Ok(year_for(year)?.rates)
}

struct OhioEstimated {
    y: EstimatedYear,
    explanation: Explanation,
}

/// One IT/SD 2210 line 15 factor and what it was worked out from.
#[derive(Clone, Debug, Default)]
struct SpanFactor {
    factor: Factor,
    /// false when no day of the span has a rate
    charged: bool,
    /// the rate on the span's first day
    rate: Rate,
    /// the last day a published rate covers
    rated_to: Date,
    /// the first day none does, or the zero Date
    unrated: Date,
    source: String,
}

impl EstimatedYear {
    /// Works out the line 15 factor for the days after `from` up to and
    /// including `to`, by the formula on page 4 of the 2025 IT/SD 2210: the
    /// rate times the days late, divided by 365.25. A span that crosses a
    /// change of rate adds each part's rate times its days, as the 2025
    /// form's third column does, and the sum is rounded once to the places
    /// the form prints.
    fn factor(&self, from: Date, to: Date) -> Result<SpanFactor, Error> {
        let mut out = SpanFactor::default();
        let mut rate_days: i64 = 0;
        let mut sources: Vec<String> = Vec::new();
        let mut cur = from;
        while cur.before(to) {
            let day = cur.add_days(1);
            let Some(p) = self.rates.on(day) else {
                out.unrated = day;
                break;
            };
            let mut end = p.to();
            if to.before(end) {
                end = to;
            }
            if !out.charged {
                out.rate = p.rate();
                out.charged = true;
            }
            if sources.last() != Some(&p.source()) {
                sources.push(p.source());
            }
            rate_days += i64::from(p.rate().0) * end.days_since(cur);
            out.rated_to = end;
            cur = end;
        }
        if !out.charged || rate_days == 0 {
            out.charged = false;
            return Ok(out);
        }
        let scale = 10i64.pow(self.places);
        // rate in basis points × days ÷ 10,000 ÷ (day_count ÷ 100), in units
        // of 10^-places, rounded half up.
        let d = 100 * self.day_count;
        let units = (2 * rate_days * scale + d) / (2 * d);
        if units == 0 || units >= scale {
            return Err(Error::Invalid(format!(
                "ohiotaxcalc: a factor of {units} in {} places is not one the form prints",
                self.places
            )));
        }
        let f =
            estimated::parse_factor(&format!("0.{units:0width$}", width = self.places as usize))?;
        out.source = format!(
            "IT/SD 2210 (2025), page 4: rate × days late ÷ 365.25 = {f} for {} to {}; {}",
            from.long(),
            out.rated_to.long(),
            sources.join("; ")
        );
        out.factor = f;
        Ok(out)
    }
}

/// What the three questions share.
#[derive(Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    target: USCents,
    dues: Vec<Date>,
    stop: Date,
    /// IT/SD 2210 line 10: the target times 25%, 50%, 75% and 100%.
    needed: Vec<USCents>,
    /// Whether the statute's test for last year's return would give a
    /// different target from the form's.
    prior_differs: bool,
    notes: Notes,
}

/// What [`OhioEstimated::ledger`] returns: the ledger, each column's IT/SD
/// 2210 line 14, whether the whole-year tests are met, and the notes.
type Ledger = (Vec<LedgerEntry>, Vec<USCents>, bool, Notes);

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

/// "Jan 15" inside the tax year and "Jan 15, 2027" outside it.
fn short_date(d: Date, year: i64) -> String {
    let month = d.month().to_string();
    let m = month.get(..3).unwrap_or(&month);
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

/// What the target leaves to pay after withholding, for a Why.
fn target_why(target: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!(
            "Your target is {}, a quarter by each due date.",
            target.whole()
        );
    }
    format!(
        "Your target is {}. After withholding that's {}, a quarter by each due date.",
        target.whole(),
        (target - withheld).whole()
    )
}

/// What estimated payments the target leaves after withholding, for a Why.
fn left_why(target: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!(
            "That's {} of estimated payments, a quarter by each due date.",
            target.whole()
        );
    }
    format!(
        "After {} of withholding, that's {} of estimated payments. Pay a quarter by each due date.",
        withheld.whole(),
        (target - withheld).whole()
    )
}

/// What tax less withholding leaves, for a Why.
fn owed_why(tax: USCents, withheld: USCents) -> String {
    if withheld.0 == 0 {
        return format!("Your Ohio tax is {}, with no withholding.", tax.whole());
    }
    format!(
        "Your Ohio tax of {} less {} of withholding {}.",
        tax.whole(),
        withheld.whole(),
        estimated::leaves(tax - withheld)
    )
}

/// Decides a waiver asked for. Ohio has no casualty, retirement or
/// disability waiver; ORC 5747.09(D), as amended by HB 96, lets the Tax
/// Commissioner abate the penalty from tax year 2025 and names no grounds,
/// so every request is undecided.
fn ohio_waiver(f: &Facts) -> Option<WaiverOutcome> {
    match f.positions().waiver() {
        None | Some(WaiverKind::None) => None,
        Some(WaiverKind::Casualty) => Some(estimated::waiver_undecided(
            "Ohio has no waiver for a casualty or disaster as such. From 2025 the Tax Commissioner can abate all or part of the penalty.",
        )),
        Some(WaiverKind::RetiredOrDisabled) => Some(estimated::waiver_undecided(
            "Ohio has no waiver for retirement or disability. From 2025 the Tax Commissioner can abate all or part of the penalty.",
        )),
    }
}

impl OhioEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "ohiotaxcalc: the facts carry {}, not ohiotaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "ohiotaxcalc: these are Ohio's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "ohiotaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "Ohio Adm. Code 5703-7-04 gives a filer with two-thirds of gross income from farming or fishing other ways to file and pay, which are not built",
            ));
        }
        Ok(())
    }

    /// Where the department's forms and the Revised Code part ways on a rule
    /// every Ohio computation uses. They are found in
    /// research/estimated-payments/oh.md, conflicts 3 to 6, 11 and 12, except
    /// the whole-year tests, which the research does not list. Each is
    /// marked as affecting these facts only where the readings give them
    /// different figures.
    fn divergences(&self, f: &Facts, a: &Assessment) -> Result<Vec<Divergence>, Error> {
        let (mut whole_year, mut column, mut stops) = (false, false, false);
        let active = !a.exempt && a.requirement.target_known();
        if active {
            let (ledger, line14, year_met, _) = self.ledger(f, a)?;
            for (i, short) in line14.iter().enumerate() {
                if short.0 == 0 {
                    continue;
                }
                whole_year = whole_year || year_met;
                if year_met {
                    continue;
                }
                column = column || ledger[ledger.len() - 1].credited_by() > ledger[i].credited_by();
                let end = if i + 1 < a.dues.len() {
                    a.dues[i + 1]
                } else {
                    a.stop
                };
                for p in f.payments() {
                    stops = stops || (p.on().after(a.dues[i]) && p.on().before(end));
                }
            }
        }
        let as_withheld =
            active && f.elections().withholding_as_withheld() && f.withheld_total().0 > 0;
        let annualised = active && f.elections().annualised();
        let mut out = Vec::new();
        for b in [
            working::new_divergence("Last year's tax as the target")
                .form(
                    "Last year's tax counts only if you filed last year's Ohio return on time. The form asks nothing about how many months last year covered.",
                    "IT/SD 2210 (2025), page 2, lines 6, 7 and 9; IT 1040ES (2026), worksheet line 11",
                )
                .statute(
                    "Last year's tax counts if you filed a return for last year, on time or late. Last year must also have been twelve months long.",
                    "ORC 5747.09(A)(4)(b) and (E)(2)",
                )
                .uses(Reading::Form)
                .affects(a.prior_differs),
            working::new_divergence("The whole-year tests")
                .form(
                    "Take the year's withholding and timely estimated payments. There's no penalty if they reach 90% of this year's tax. Last year's tax after a timely return also works. That holds however they were spread across the year.",
                    "IT/SD 2210 (2025), page 2, lines 1, 5 and 7; Ohio Tax Alert, Updates to Ohio Form IT/SD 2210 (2017)",
                )
                .statute(
                    "Each installment is measured by the tax paid by its own date. Reasonable cause excuses it when the tax paid reaches one of two figures. One is 90% of the tax on income annualised to the month before. The other is 100% of last year's tax.",
                    "ORC 5747.09(D)(1) to (4) and (E)",
                )
                .uses(Reading::Form)
                .affects(whole_year),
            working::new_divergence("What each column is short")
                .form(
                    "Each column's shortfall is 25%, 50%, 75% or 100% of the target. Withholding and payments to that date are subtracted. An amount still unpaid therefore carries into the next column.",
                    "IT/SD 2210 (2025), page 2, lines 10 to 14",
                )
                .statute(
                    "Each shortfall is the installment, 25% of the target, less tax paid by its date. Payments reach old shortfalls only after the installment then due is covered.",
                    "ORC 5747.09(A)(4) and (D)(1) to (4)",
                )
                .uses(Reading::Form)
                .affects(column),
            working::new_divergence("When the interest stops")
                .form(
                    "Each column's shortfall is charged at one factor. It runs from its due date to the next due date, and the last to April 15. That holds whenever within the span it was paid. The department's FAQ ends it at the later of payment and the return's due date.",
                    "IT/SD 2210 (2025), page 2, line 15, and page 4, Fiscal Year Filers; FAQ question 6 (research/estimated-payments/oh.md)",
                )
                .statute(
                    "The underpayment period runs from the due date to the date the payment is made.",
                    "ORC 5747.09(D)",
                )
                .uses(Reading::Form)
                .affects(stops),
            working::new_divergence("Withholding on the dates withheld")
                .form(
                    "Withholding counts as 25%, 50%, 75% and 100% of the yearly total by each date. The form has no line for the dates it was actually withheld.",
                    "IT/SD 2210 (2025), page 2, line 11",
                )
                .statute(
                    "Withholding counts in equal amounts on each due date. You can instead establish the dates all of it was withheld. No form or instruction says how to claim that.",
                    "ORC 5747.09(B)",
                )
                .uses(Reading::Form)
                .affects(as_withheld),
            working::new_divergence("The annualised income test")
                .form(
                    "There's no annualised calculation. The long form for it was withdrawn from tax year 2017.",
                    "IT/SD 2210 (2025); Ohio Tax Alert, Updates to Ohio Form IT/SD 2210 (2017)",
                )
                .statute(
                    "Reasonable cause, and no penalty, apply if the tax paid reaches 90% of a figure. It is the tax on income annualised to the end of the month before the due date. No form says how to claim it.",
                    "ORC 5747.09(E)(1)",
                )
                .uses(Reading::Form)
                .affects(annualised),
            working::new_divergence("The 2026 worksheet's tax brackets")
                .form(
                    "The worksheet's tax table prints 2025's brackets and is out of date. They run from $342 plus 2.75% over $26,050 up to 3.125% over $100,000. HB 96 set one bracket for 2026.",
                    "IT 1040ES (2026), worksheet line 6; 2025 IT 1040 booklet, tax table",
                )
                .statute(
                    "From 2026, the tax is $332 plus 2.75% of income over $26,050. Work out this year's tax with it.",
                    "ORC 5747.02(A)(3)(c), as amended by HB 96 (136th General Assembly), effective September 30, 2025",
                )
                .uses(Reading::Statute)
                .affects(false),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// The requirement and installments, with the divergences for these
    /// facts.
    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let mut a = self.measure(f)?;
        let divergences = self.divergences(f, &a)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    fn measure(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment {
            dues: estimated::move_dates(&y.calendar, &y.dues),
            stop: y.calendar.next_business_day(y.stop),
            ..Assessment::default()
        };
        let last = y.year - 1;
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        // 90% of this year's tax, IT/SD 2210 line 4.
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
                "ORC 5747.09(A)(4)(a); IT/SD 2210 (2025), page 2, lines 2 and 4",
                format!(
                    "{} of the {tax} of {} Ohio tax after credits is {}",
                    y.this_year_share,
                    y.year,
                    this_year.amount()
                ),
            );
        }
        // Last year's tax, lines 3, 6 and 9.
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();
        let timely = extras.prior_year_return_timely();
        let unavailable = |why: String| -> TargetOptionBuilder {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        // Questions about the target, asked only when no exception sets it.
        let mut unless_exempt: Vec<(PositionKind, String, String)> = Vec::new();
        let prior_option = if !has_prior {
            unless_exempt.push((
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Ohio return on time, and what was its tax?"),
                "With it, your target could be 100% of last year's tax, not 90% of this year's."
                    .into(),
            ));
            unavailable(format!("No {last} Ohio return on record"))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Ohio return"))
        } else if timely.is_none() {
            unless_exempt.push((
                PositionKind::JurisdictionFact,
                format!("Did you file your {last} Ohio return on time?"),
                "Until then, last year's tax can't be your target. Ohio's form counts it only after a timely return.".into(),
            ));
            unavailable(format!(
                "Depends on whether you filed your {last} Ohio return on time"
            ))
        } else if timely != Some(true) {
            unavailable(format!("Your {last} Ohio return wasn't filed on time"))
        } else {
            let prior_tax = prior.figures().and_then(|pf| pf.tax()).unwrap_or_default();
            let amount = y.prior_year_share.of(prior_tax);
            a.notes.rule(
                "ORC 5747.09(A)(4)(b); IT/SD 2210 (2025), page 2, lines 3, 6 and 9",
                format!(
                    "the {last} Ohio return was filed on time and showed {prior_tax} of tax after credits; {} of it is {amount}",
                    y.prior_year_share
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, amount)
        };
        let prior_opt = prior_option.build()?;
        // The statute's test: any filed return for a twelve-month year. A
        // return that does not say its months is taken as twelve.
        if has_prior
            && (prior.filed() && (prior.months() == 0 || prior.months() == 12))
                != prior_opt.available()
        {
            let prior_tax = prior.figures().and_then(|pf| pf.tax()).unwrap_or_default();
            a.prior_differs = !tax_known || y.prior_year_share.of(prior_tax) < this_year.amount();
        }
        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            unless_exempt.push((
                PositionKind::ThisYearTax,
                format!(
                    "What do you expect your {} Ohio tax after credits to be?",
                    y.year
                ),
                estimated::without_this_year_tax(prior_opt.available()),
            ));
        }
        if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target is 90% of your {} Ohio tax. You have no timely {last} Ohio return to measure against.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} covers last year's tax of {}. No estimated payments are needed, whatever your {} tax turns out to be.",
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
                    "Your target is last year's tax of {}. It keeps you clear of a penalty whatever your {} tax turns out to be. {}",
                    target.whole(),
                    y.year,
                    left_why(target, withheld)
                ),
            )
            .choose(chosen);
        } else if tax - withheld <= y.threshold {
            a.exempt = true;
            let at_most =
                estimated::exception_at_most("Tax less withholding", tax - withheld, y.threshold)?;
            let mut why = format!(
                "{} That is {} or less, so no estimated payments are needed.",
                owed_why(tax, withheld),
                y.threshold.whole()
            );
            if withheld > tax {
                why = format!(
                    "Your Ohio tax of {} less {} of withholding {}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                );
            } else if withheld == tax {
                why = format!(
                    "Your Ohio withholding of {} covers your tax of {}. No estimated payments are needed.",
                    withheld.whole(),
                    tax.whole()
                );
            }
            rb.required(false, why).exempt(at_most);
            a.notes.rule(
                "ORC 5747.09(B); IT 1040ES (2026); IT/SD 2210 (2025), page 2, line 8",
                format!(
                    "{tax} of tax less {withheld} of withholding is {}, not more than {}, so there is no penalty",
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if withheld >= target {
            let mut why = format!(
                "Your Ohio withholding of {} covers the {} needed for the year. No estimated payments are needed.",
                withheld.whole(),
                target.whole()
            );
            if target.0 == 0 {
                why = format!(
                    "Your {last} Ohio tax was $0, so your target is $0. No estimated payments are needed."
                );
            }
            rb.required(false, why).choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "{} That is more than {}, so estimated payments are needed. {}",
                    owed_why(tax, withheld),
                    y.threshold.whole(),
                    target_why(target, withheld)
                ),
            )
            .choose(chosen);
        }
        if a.exempt {
            a.prior_differs = false;
        }
        a.requirement = rb.build()?;
        if !a.exempt {
            for (k, q, e) in unless_exempt {
                a.notes.ask(k, q, e);
            }
        }
        if a.exempt || !a.requirement.target_known() {
            a.needed = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }
        a.target = target;
        a.needed = estimated::cumulative_shares(target, &y.shares)?;
        a.notes.rule(
            "ORC 5747.09(A)(4), (C); IT/SD 2210 (2025), page 2, line 10; IT 1040ES (2026), worksheet line 14",
            format!(
                "the target is {target}, so {}, {}, {} and {} are needed by {}, {}, {} and {}",
                a.needed[0],
                a.needed[1],
                a.needed[2],
                a.needed[3],
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );
        if !tax_known {
            a.notes.rule(
                "ORC 5747.09(A)(4); IT/SD 2210 (2025), page 2, line 9",
                format!(
                    "this year's tax was not given, so the target is the {last} tax of {target}; line 9 is the lesser of that and 90% of this year's tax, so the target can only be lower once this year's tax is known"
                ),
            );
        }
        let met = f.requirement_first_met();
        if !met.is_zero() && met.after(a.dues[0]) {
            a.notes.rule(
                "ORC 5747.09(B); IT/SD 2210 (2025), page 2, line 10; FAQ question 7 (research/estimated-payments/oh.md)",
                format!(
                    "the requirement was first met on {}, but the form measures every column by its share of the year, and Ohio has no exception for a late start; the statute moves only the first declaration, and the department asks new residents to pay from the next due date",
                    met.long()
                ),
            );
        }
        Ok(a)
    }

    /// Applies the credits to the installments. It returns, with the
    /// ledger, each column's IT/SD 2210 line 14 and whether the whole-year
    /// tests on lines 5 and 7 are met.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<Ledger, Error> {
        let mut notes = Notes::default();
        let credits = estimated::credits(f, &a.dues, &self.y.shares)?;
        let ledger = estimated::apply(&a.dues, &estimated::increments(&a.needed), &credits)?;
        let line14: Vec<USCents> = ledger
            .iter()
            .enumerate()
            .map(|(i, l)| std::cmp::max(USCents(0), a.needed[i] - l.credited_by()))
            .collect();
        notes.rule(
            "ORC 5747.09(B); IT/SD 2210 (2025), page 2, lines 11 to 14",
            format!(
                "the {} withheld is treated as paid 25% by each due date, whenever it was withheld; estimated payments count from the day paid, and last year's overpayment applied forward from {}; each column is short by what is needed to its date less what was credited to it",
                f.withheld_total(),
                a.dues[0].long()
            ),
        );
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "IT/SD 2210 (2025), page 2, line 11; ORC 5747.09(B)",
                "the form has no line for crediting withholding on the dates it was withheld, so it is spread evenly; the statute allows the dates withheld, and Divergences reports it",
            );
        }
        if f.elections().annualised() {
            notes.rule(
                "Ohio Tax Alert, Updates to Ohio Form IT/SD 2210 (2017); ORC 5747.09(E)(1)",
                "the form has no annualised calculation, so the annualised election is not applied; the statute allows it, and Divergences reports it",
            );
        }
        // Lines 1, 5 and 7: withholding, timely estimated payments and last
        // year's overpayment against the target. The form does not define
        // "timely paid"; a payment counts if made by the last due date.
        let last_due = a.dues[a.dues.len() - 1];
        let mut paid = f.withheld_total();
        for p in f.payments() {
            if p.from_prior_year() || !p.on().after(last_due) {
                paid += p.amount();
            }
        }
        if a.exempt {
            return Ok((ledger, line14, false, notes));
        }
        let met = paid >= a.target;
        let verdict = if met { "reaching" } else { "short of" };
        notes.rule(
            "IT/SD 2210 (2025), page 2, lines 1, 5, 7 and 9",
            format!(
                "withholding, estimated payments made by {} and last year's overpayment applied forward come to {paid}, {verdict} the target of {}; when they reach it there is no penalty",
                last_due.long(),
                a.target
            ),
        );
        Ok((ledger, line14, met, notes))
    }

    /// The schedule from the ledger. Every short column is excused when the
    /// whole-year test is met.
    fn installments(
        &self,
        a: &Assessment,
        ledger: &[LedgerEntry],
        year_met: bool,
    ) -> Result<Vec<Installment>, Error> {
        let mut out = Vec::with_capacity(ledger.len());
        for (i, l) in ledger.iter().enumerate() {
            let mut b = estimated::new_installment(i as i64 + 1, l.due());
            b.needed_by(a.needed[i])
                .credited_by(l.credited_by())
                .shortfall(l.shortfall());
            if year_met {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok(out)
    }
}

impl EstimatedTax for OhioEstimated {
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
                    "with no {} Ohio return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, _, year_met, notes) = self.ledger(f, &a)?;
        let out = self.installments(&a, &ledger, year_met)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "ohiotaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Ohio return to measure against, there is no target to figure a penalty from",
                    self.y.year - 1
                ),
            ));
        }
        let y = &self.y;
        let mut stop = as_of;
        if a.stop.before(stop) {
            stop = a.stop;
        }
        let waiver = f.positions().waiver().unwrap_or_default();
        let mut more = Notes::default();
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(a.stop) {
            more.rule(
                "IT/SD 2210 (2025), page 2, line 15; FAQ question 6 (research/estimated-payments/oh.md)",
                format!(
                    "the return was filed on {}, but the form charges the last column to {} whenever the balance was paid, so filing early does not shorten the penalty",
                    filed.long(),
                    a.stop.long()
                ),
            );
        }
        if waiver != WaiverKind::None {
            more.rule(
                "ORC 5747.09(D), as amended by HB 96 (136th General Assembly)",
                "Ohio has no casualty, retirement or disability waiver; from tax year 2025 the Tax Commissioner may abate the penalty in whole or in part, so the request goes to the commissioner, and the penalty is shown before any abatement",
            );
        }
        let (ledger, line14, year_met, mut notes) = self.ledger(f, &a)?;
        let schedule = self.installments(&a, &ledger, year_met)?;
        let none = |ns: &[&Notes]| -> Result<working::Result<Penalty>, Error> {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            let mut b = working::new_result(p);
            b.with(&a.notes);
            for n in ns {
                b.with(n);
            }
            Ok(b.with(&more).build()?)
        };
        if a.exempt {
            return none(&[]);
        }
        if year_met {
            return none(&[&notes]);
        }
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut unrated = Date::default();
        let mut factors: Vec<String> = Vec::new();
        for (i, short) in line14.iter().enumerate() {
            let due = a.dues[i];
            if short.0 == 0 || !stop.after(due) {
                continue;
            }
            let mut end = if i + 1 < a.dues.len() {
                a.dues[i + 1]
            } else {
                a.stop
            };
            if stop.before(end) {
                end = stop;
            }
            let sf = y.factor(due, end)?;
            if !sf.unrated.is_zero() && (unrated.is_zero() || sf.unrated.before(unrated)) {
                unrated = sf.unrated;
            }
            let mut lb = estimated::new_penalty_line(ledger[i].number(), due, *short);
            if sf.charged {
                let part = estimated::span_factor(
                    *short,
                    due,
                    sf.rated_to,
                    &sf.factor,
                    sf.rate,
                    y.rounding,
                    sf.source.clone(),
                )?;
                lb.part(part).days(sf.rated_to.days_since(due));
                factors.push(format!("{} from {}", sf.factor, due.long()));
            }
            pb.line(lb.build()?);
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;
        notes.rule(
            "ORC 5747.09(D); IT/SD 2210 (2025), page 2, lines 14 to 17, and page 4",
            format!(
                "each column's underpayment is charged once, at a factor for the span from its due date to the next due date, or {} for the last: the rate times the days divided by 365.25, rounded to {} places; a payment made within a span counts from the next due date; each charge is rounded {}; the penalty to {} is {}",
                a.stop.long(),
                y.places,
                y.rounding,
                stop.long(),
                p.total()
            ),
        );
        if !a.tax_known {
            notes.rule(
                "ORC 5747.09(A)(4); IT/SD 2210 (2025), page 2, line 9",
                format!(
                    "this year's Ohio tax was not given, so the penalty is figured against the {} tax alone; {} is the most it can be, and it is less if 90% of this year's tax turns out lower",
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if !factors.is_empty() {
            notes.rule(
                "IT/SD 2210 (2025), page 4",
                format!(
                    "the {} factors are worked out by the 2025 form's formula, rate × days late ÷ 365.25, which gives every 2025 factor exactly: {}",
                    y.year,
                    factors.join(", ")
                ),
            );
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "Ohio hasn't published its {} penalty factors yet. Until then, the figures use the {} form: the rate times days late over 365.25.",
                    y.year,
                    y.year - 1
                ),
            );
        }
        if !unrated.is_zero() {
            p = p.to_builder().rated_through(unrated.add_days(-1)).build()?;
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The rate for the days after {} isn't published yet.",
                    short_date(unrated.add_days(-1), 0)
                ),
            );
        }
        if let Some(o) = ohio_waiver(f)
            && p.total().0 > 0
        {
            p = p.to_builder().waiver_outcome(o).build()?;
            notes.ask(
                PositionKind::UnsettledRule,
                "",
                "Ohio's law lets the Tax Commissioner abate all or part of the penalty from 2025. It doesn't say whether a casualty, disaster, retirement or disability qualifies.",
            );
        }
        Ok(working::new_result(p)
            .with(&a.notes)
            .with(&notes)
            .with(&more)
            .build()?)
    }

    fn explanation(&self) -> Explanation {
        self.explanation.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/oh.md.
fn ohio_explanation() -> Result<Explanation, Error> {
    estimated::new_explanation(
        "Take this year's Ohio tax after credits, less Ohio withholding. If that is more than $500, Ohio expects estimated payments. The penalty is interest on what was short at each due date.",
    )
    .paragraph("There is no penalty if withholding and estimated payments reach a target. The target is the smaller of 90% of this year's Ohio tax or 100% of last year's. Last year's figure counts only if you filed last year's Ohio return on time. So if you filed no Ohio return last year, only the 90% figure is open to you. There is no 110% tier for higher incomes.")
    .paragraph("Pay a quarter each by April 15, June 15, September 15 and the next January 15. Withholding counts as paid evenly on the four dates, whenever it was withheld. The department's form looks at the whole year first. Withholding and payments made by the January date may reach the target. If so, there is no penalty, even when the earlier quarters were short.")
    .paragraph("Otherwise the form compares each date's need with what had been paid by then. Each shortfall is charged interest from that date to the next one. The last is charged to April 15. A payment made between two dates counts only from the next date. The rate is set once for each calendar year, 7% for 2026. The form divides the year into 365.25 days.")
    .paragraph("Since tax year 2025 the law says the penalty may be added. The Tax Commissioner may reduce or remove it. Ohio's school district and city income taxes take their own estimated payments. Those are separate from these.")
    .versus_federal("Unlike federal, Ohio's threshold is more than $500 after withholding. The federal one is $1,000. There is no 110% tier. Last year's tax counts only after a timely Ohio return. Meeting the whole year's target means no penalty however payments were spread. The interest runs to April 15 at a yearly rate. There's no casualty or retirement waiver.")
    .source("Ohio Revised Code 5747.09, as amended by HB 96 (136th General Assembly); 5703.47; 1.14")
    .source("IT/SD 2210, Interest Penalty on Underpayment of Tax, and its instructions, 2025")
    .source("IT 1040ES, Ohio Estimated Income Tax Payment Worksheet and Instructions, 2026")
    .source("Ohio Tax Alert, Updates to Ohio Form IT/SD 2210 (tax year 2017)")
    .build()
}
