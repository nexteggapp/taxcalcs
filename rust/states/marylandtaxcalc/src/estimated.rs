//! Maryland's estimated income tax rules, Md. Code, Tax-General §§ 10-815,
//! 10-820, 10-902, 13-602 and 13-604, as the shared [`EstimatedTax`] trait.
//!
//! Maryland's rules are close to the federal ones in outline and differ in
//! the numbers and in how the charge is figured. The prior-year test is 110%
//! for everybody. Withholding counts on the dates it was withheld. Form
//! 502UP's columns are cumulative, so an unpaid amount is charged again in
//! every later column until it is paid. The charge is interest at the
//! Comptroller's rate for the calendar year, which Form 502UP prints as one
//! factor for each span between due dates.
//!
//! The figure follows Form 502UP for 2025, the latest published, and the
//! 2026 payment voucher worksheet. The result's Divergences say where the
//! Tax-General Article reads otherwise. No act since 2024 changed these
//! rules (research/estimated-payments/md.md), so no form here predates the
//! law it describes. The 2026 Form 502UP is not published, so its factors
//! are not known, and the interest is figured by the month as § 13-604(a)
//! says; the penalty reports that in Required.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    LedgerEntry, Month, PartKind, Payment, Penalty, PenaltyPart, RatePeriod, RateSeries,
    Requirement, Rounding, TargetOption, WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents, percent};
use num_bigint::BigInt;
use working::{Divergence, DivergenceBuilder, Notes, PositionKind, Reading};

use crate::estimated_y2026::year_2026;

const JURISDICTION: Jurisdiction = estimated::MARYLAND;

/// The facts Maryland's rules read that no other jurisdiction does. Carry
/// them in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    other_state: Option<bool>,
}

impl EstimatedFacts {
    /// Whether at least 90% of the year's taxable income is also taxable by
    /// another state, or `None` when the caller did not say. Form 502UP then
    /// charges no interest when the underpayment is not greater than the
    /// local tax, [`estimated::Figures::local_tax`] (code 302).
    pub fn taxable_by_another_state(&self) -> Option<bool> {
        self.other_state
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts an [`EstimatedFacts`] with nothing answered.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Answers whether at least 90% of taxable income is also taxable by
    /// another state. false is an answer.
    pub fn taxable_by_another_state(&mut self, taxable: bool) -> &mut Self {
        self.e.other_state = Some(taxable);
        self
    }

    /// Returns the facts. It never fails.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// The tax after withholding at or under which no interest is due, Form
    /// 502UP exception A.
    pub(crate) threshold: USCents,

    /// Form 502UP line 8.
    pub(crate) this_year_share: Rate,
    /// Form 502UP line 9b.
    pub(crate) prior_year_share: Rate,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// The day by which a return filed and paid in full takes the place of
    /// the January installment, before it is moved for a weekend.
    pub(crate) file_and_pay_by: Date,

    /// The return's due date, where Form 502UP's last column ends.
    pub(crate) stop: Date,

    pub(crate) period_ends: Vec<Date>,

    /// One period for each calendar year the interest can run in, January 1
    /// to December 31, at the rate the Comptroller sets for that year under
    /// § 13-604(b).
    pub(crate) rates: RateSeries,
}

/// § 10-902(a)(1): at least 25% of the estimate with each declaration. Form
/// 502UP line 11 takes one-fourth, one-half and three-fourths of the year's
/// income by default, which is the same.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// Returns Maryland's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(MarylandEstimated { y }))
}

struct MarylandEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// Form 502UP line 13: the running total required by each due date.
    needed: Vec<USCents>,

    excused_january: bool,
    extras: EstimatedFacts,
    notes: Notes,
}

/// Go's `%T` for the extras a caller passed, as far as Rust can tell it.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else if v.is::<i64>() {
        "int"
    } else if v.is::<bool>() {
        "bool"
    } else {
        "unknown"
    }
}

fn build_all(bs: &[&mut DivergenceBuilder]) -> Result<Vec<Divergence>, Error> {
    let mut out = Vec::new();
    for b in bs {
        out.push(b.build()?);
    }
    Ok(out)
}

/// An option's label and amount, for the middle of a sentence.
fn option_label(o: &TargetOption) -> String {
    format!("{}, {}", o.label(), o.amount().whole())
}

impl MarylandEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "marylandtaxcalc: the facts carry {}, not marylandtaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "marylandtaxcalc: these are Maryland's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "marylandtaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or fisherman with two-thirds of gross income from farming or fishing pays once, by January 15, under Form 502UP's special calculation, which is not built",
            ));
        }
        if f.elections().annualised() {
            let periods = f.periods();
            for (i, p) in periods.iter().enumerate() {
                if let Some(end) = self.y.period_ends.get(i)
                    && p.end() != *end
                {
                    return Err(Error::Invalid(format!(
                        "marylandtaxcalc: Form 502UP line 11 period {} ends on {}, not {}",
                        i + 1,
                        end,
                        p.end()
                    )));
                }
                if i > 0 && p.income() < periods[i - 1].income() {
                    return Err(Error::Invalid(format!(
                        "marylandtaxcalc: Form 502UP line 11 is income received from January 1, so period {} cannot have less than period {i}",
                        i + 1
                    )));
                }
            }
            if periods.get(3).is_none_or(|p| p.income().0 <= 0) {
                return Err(Error::Invalid(
                    "marylandtaxcalc: Form 502UP line 12 divides by the year's income, which must be more than zero".into(),
                ));
            }
        }
        Ok(())
    }

    /// Where Maryland's forms and the Tax-General Article part ways on a
    /// rule the requirement uses, each marked with whether it changes
    /// anything for these facts. They are found in
    /// research/estimated-payments/md.md, Verification, and checked there
    /// against both. The rules the interest uses are in
    /// `penalty_divergences`.
    fn divergences(
        &self,
        at_five_hundred: bool,
        prior_chosen: bool,
        income_not_up: bool,
    ) -> Result<Vec<Divergence>, Error> {
        build_all(&[
            working::new_divergence("No interest at $500")
                .form(
                    "No interest is due when your tax after Maryland withholding is $500 or less. Exactly $500 is free of interest. The 2025 booklet says less than $500.",
                    "Form 502UP (2025), Exceptions, A; 2025 Maryland resident booklet, Underpayment of Estimated Tax",
                )
                .statute(
                    "No interest may be charged on unpaid tax less than one-half the federal $1,000. Exactly $500 of unpaid tax can carry interest.",
                    "Md. Code, Tax-Gen. § 13-602(c); 26 U.S.C. § 6654(e)(1)",
                )
                .uses(Reading::Form)
                .affects(at_five_hundred),
            working::new_divergence("Who may use 110% of last year's tax")
                .form(
                    "Form 502UP offers 110% of last year's tax to everyone. The 2026 payment worksheet has a condition. It needs 110% \"if you will receive more income than you did in 2025\".",
                    "Form 502UP (2025), Exceptions, B, and lines 9b and 10; Payment Voucher Worksheet (2026), How to estimate your 2026 tax",
                )
                .statute(
                    "An estimate below 90% of this year's tax and 110% of last year's draws interest. There's no condition on income.",
                    "Md. Code, Tax-Gen. § 13-602(a)(2)",
                )
                .uses(Reading::Form)
                .affects(prior_chosen && income_not_up),
            working::new_divergence("Last year's tax")
                .form(
                    "Last year's tax is Form 502 line 34, less the Part CC credits on Form 502CR.",
                    "Form 502UP (2025), line 9a and its instructions",
                )
                .statute(
                    "The test is 110% of the tax paid for last year. That tax is reduced by the credit for tax paid to another state.",
                    "Md. Code, Tax-Gen. § 13-602(a)(2)(ii)",
                )
                .uses(Reading::Form)
                .affects(prior_chosen),
        ])
    }

    /// The interest factors, a third-period payment made from December 15,
    /// and the penalty of up to 25%. The factors and the 25% affect any
    /// interest charged, and the December rule a third-period shortfall paid
    /// from December 15.
    fn penalty_divergences(&self, charged: bool, december: bool) -> Result<Vec<Divergence>, Error> {
        build_all(&[
            working::new_divergence("The interest factors")
                .form(
                    "Each span between due dates is charged once at a printed factor. A factor is the span's days over 365 times the annual rate. For 2025 they are 0.0192, 0.0289, 0.0381 and 0.0266. The 2026 form isn't published yet.",
                    "Form 502UP (2025), line 16 and its instructions",
                )
                .statute(
                    "Interest is one-twelfth of the annual rate for each month or part of a month. For 2025's spans that is 0.0191, 0.0287, 0.0377 and 0.0270.",
                    "Md. Code, Tax-Gen. § 13-604(a)",
                )
                .uses(Reading::Statute)
                .affects(charged),
            working::new_divergence("A third-period payment made from December 15")
                .form(
                    "A third-period payment before December 15 is charged for the months it was late. One made on or after December 15 is charged the whole printed factor. The next due date is still January 15.",
                    "Form 502UP (2025), Line 16 instructions, 3rd Period",
                )
                .statute(
                    "Interest runs by the month or part of one, from the due date to the day paid. The next installment is due January 15.",
                    "Md. Code, Tax-Gen. §§ 10-820(b)(1), 13-602(a), 13-604(a)",
                )
                .uses(Reading::Statute)
                .affects(december),
            working::new_divergence("The penalty of up to 25%")
                .form("Only interest is figured.", "Form 502UP (2025), lines 16 to 18")
                .statute(
                    "A penalty of up to 25% of the amount underestimated is also to be charged.",
                    "Md. Code, Tax-Gen. § 13-702(a)",
                )
                .uses(Reading::Form)
                .affects(charged),
        ])
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment {
            extras,
            dues: estimated::move_dates(&y.calendar, &y.dues),
            ..Assessment::default()
        };
        let last = y.year - 1;
        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let prior_opt = f.prior_year();
        let has_prior = prior_opt.is_some();
        let prior = prior_opt.unwrap_or_default();

        // 90% of this year's tax, Form 502UP line 8.
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
                "Md. Code, Tax-Gen. § 13-602(a)(2)(i); Form 502UP (2025), lines 7 and 8",
                format!(
                    "{} of the {} of Maryland and local tax after credits is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // 110% of last year's tax, Form 502UP line 9b, at every income.
        let unavailable = |why: String| {
            let mut b =
                estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0));
            b.unavailable(why);
            b
        };
        // Questions whose answer can change the target, asked only when no
        // exception sets it.
        let mut target_asks: Vec<(PositionKind, String, String)> = Vec::new();
        let prior_option = if !has_prior {
            target_asks.push((
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Maryland return, and what tax did it show?"),
                format!(
                    "With it, your target could be {} of last year's tax, not {} of this year's.",
                    y.prior_year_share, y.this_year_share
                ),
            ));
            unavailable(format!("Needs your {last} Maryland return"))
        } else if prior.not_required_to_file() {
            target_asks.push((
                PositionKind::UnsettledRule,
                String::new(),
                format!(
                    "Maryland's documents don't say if not needing a {last} return means no {last} tax. That would make your target zero, and the figures don't count it."
                ),
            ));
            unavailable(format!("You didn't need to file a {last} Maryland return"))
        } else if !prior.filed() {
            a.notes.rule(
                "Form 502UP (2025), line 10",
                format!(
                    "no {last} return was filed, so as a first-time filer only {} of this year's tax counts",
                    y.this_year_share
                ),
            );
            unavailable(format!(
                "You didn't file a {last} Maryland return, so this year's tax sets your target"
            ))
        } else {
            let pf = prior.figures().unwrap_or_default();
            let prior_tax = pf.tax().unwrap_or_default();
            let amount = y.prior_year_share.of(prior_tax);
            a.notes.rule(
                "Md. Code, Tax-Gen. § 13-602(a)(2)(ii); Form 502UP (2025), lines 9a and 9b",
                format!(
                    "{} of the {} of {last} Maryland and local tax is {}; Maryland uses {} at every income",
                    y.prior_year_share, prior_tax, amount, y.prior_year_share
                ),
            );
            if prior.months() != 12 {
                a.notes.rule(
                    "Md. Code, Tax-Gen. § 13-602(a)(2)(ii); Form 502UP (2025), line 9a",
                    format!(
                        "neither sets a condition on how many months the {last} return covered, so the tax on its {} months is used",
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
        if !tax_known {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!(
                    "What do you expect your {} Maryland and local tax to be?",
                    y.year
                ),
                format!(
                    "With it, {} of this year's tax could set a lower target. Tax of {} or less after withholding needs no payments.",
                    y.this_year_share,
                    y.threshold.whole()
                ),
            );
        }

        let chosen_opt = if chosen == Basis::PriorYearTax {
            &prior_opt
        } else {
            &this_year
        };
        if tax_known && tax - withheld <= y.threshold {
            a.exempt = true;
            let ex =
                estimated::exception_at_most("Tax less withholding", tax - withheld, y.threshold)?;
            let why = if tax < withheld {
                format!(
                    "Your Maryland and local tax is {}. Your Maryland withholding of {} {}. No estimated payments are needed, and no interest is charged.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                )
            } else if tax == withheld {
                format!(
                    "Your Maryland withholding covers all {} of your Maryland and local tax. No estimated payments are needed, and no interest is charged.",
                    tax.whole()
                )
            } else {
                format!(
                    "Your Maryland and local tax is {}. Your Maryland withholding of {} {}. That is {} or less, so no estimated payments are needed. No interest is charged.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld),
                    y.threshold.whole()
                )
            };
            rb.required(false, why).exempt(ex);
            a.notes.rule(
                "Form 502UP (2025), Exceptions, A; Payment Voucher Worksheet (2026), Who must file a declaration",
                format!(
                    "{} of tax less {} withheld is {}, not more than {}, so no interest is due",
                    tax,
                    withheld,
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "There's no target until you have your {} Maryland tax or your {last} return.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your target is {}. Your withholding of {} reaches it. No estimated payments are needed, whatever your {} tax turns out to be.",
                    option_label(&prior_opt),
                    withheld.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {}. It keeps you clear of interest whatever your {} tax turns out to be. After {} of withholding, you need {} of estimated payments.",
                    option_label(&prior_opt),
                    y.year,
                    withheld.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        } else if target.0 == 0 {
            rb.required(
                false,
                format!(
                    "Your target is {}. No estimated payments are needed.",
                    option_label(chosen_opt)
                ),
            )
            .choose(chosen);
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Maryland withholding of {} reaches your target of {}. No estimated payments are needed if enough is withheld by each date.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            let lead = if (tax - withheld).whole() == y.threshold.whole() {
                format!(
                    "Your Maryland and local tax less withholding is more than {}.",
                    y.threshold.whole()
                )
            } else {
                format!(
                    "Your Maryland and local tax less withholding leaves {}, over {}.",
                    (tax - withheld).whole(),
                    y.threshold.whole()
                )
            };
            rb.required(
                true,
                format!(
                    "{} Your target is {}. You need {} of estimated payments.",
                    lead,
                    option_label(chosen_opt),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        if !a.exempt {
            for (position, question, effect) in target_asks {
                a.notes.ask(position, question, effect);
            }
        }
        a.requirement = rb.build()?;
        let mut income_not_up = false;
        if has_prior && let Some(pf) = prior.figures() {
            income_not_up = match (pf.state_agi(), f.this_year().state_agi()) {
                (Some(was), Some(now)) => now <= was,
                _ => false,
            };
        }
        let prior_chosen =
            !a.exempt && a.requirement.target_known() && chosen == Basis::PriorYearTax;
        let divergences = self.divergences(
            tax_known && tax - withheld == y.threshold,
            prior_chosen,
            income_not_up,
        )?;
        a.notes.diverges(&divergences);

        if a.exempt {
            a.needed = vec![USCents(0); a.dues.len()];
            return Ok(a);
        }

        // Form 502UP lines 11 to 13: the target times the share of the
        // year's income received by each period's end.
        if f.elections().annualised() {
            let periods = f.periods();
            let year = periods[3].income();
            a.needed = periods
                .iter()
                .map(|p| share_of(target, p.income().max(USCents(0)), year))
                .collect();
            a.notes.rule(
                "Form 502UP (2025), lines 11 to 13",
                format!(
                    "by election, the {} required is taken in the shares of the year's {} of income received by March 31, May 31, August 31 and December 31: {}, {}, {} and {} by the four dates",
                    target, year, a.needed[0], a.needed[1], a.needed[2], a.needed[3]
                ),
            );
        } else {
            a.needed = estimated::cumulative_shares(target, &quarters())?;
            a.notes.rule(
                "Md. Code, Tax-Gen. §§ 10-820(b)(1), 10-902(a)(1); Form 502UP (2025), lines 11 to 13",
                format!(
                    "a quarter of the {} required is due by each of {}, {}, {} and {}: {}, {}, {} and {} in all",
                    target,
                    a.dues[0].long(),
                    a.dues[1].long(),
                    a.dues[2].long(),
                    a.dues[3].long(),
                    a.needed[0],
                    a.needed[1],
                    a.needed[2],
                    a.needed[3]
                ),
            );
        }

        // A return filed and paid in full by January 31 takes the place of
        // the January installment. January 31, 2027 is a Sunday, and
        // § 1-201(b) moves it to Monday, February 1.
        let file_by = y.calendar.next_business_day(y.file_and_pay_by);
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(file_by) {
            a.excused_january = true;
            a.needed[3] = a.needed[2];
            a.notes.rule(
                "Md. Code, Tax-Gen. §§ 1-201(b), 10-820(b)(3), 10-902(b); Payment Voucher Worksheet (2026), Filing a return instead of fourth payment",
                format!(
                    "the return was filed and paid on {}, by {} ({}, moved past a weekend or holiday), so it takes the place of the {} installment",
                    filed.long(),
                    y.file_and_pay_by.long(),
                    file_by.long(),
                    a.dues[3].long()
                ),
            );
        }
        Ok(a)
    }

    /// What Form 502UP line 14 counts: withholding on the dates it was
    /// withheld, and estimated payments on theirs.
    fn credits(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Payment>, Notes), Error> {
        let cs = estimated::credits_as_withheld(f, &a.dues)?;
        let mut notes = Notes::default();
        notes.rule(
            "Form 502UP (2025), line 14",
            format!(
                "the {} withheld counts on the dates it was withheld, from the beginning of the year to each due date, \
                 and estimated payments on the dates paid; Maryland does not spread withholding evenly",
                f.withheld_total()
            ),
        );
        Ok((cs, notes))
    }

    /// Applies the credits to the installments, each installment being what
    /// line 13 adds by its date.
    fn ledger(
        &self,
        f: &Facts,
        a: &Assessment,
    ) -> Result<(Vec<LedgerEntry>, Vec<Payment>, Notes), Error> {
        let (cs, notes) = self.credits(f, a)?;
        let ledger = estimated::apply(&a.dues, &estimated::increments(&a.needed), &cs)?;
        Ok((ledger, cs, notes))
    }

    /// Applies the exception for income taxable by another state: no
    /// interest when at least 90% of taxable income is also taxable by
    /// another state and the underpayment is not greater than the local tax.
    /// The underpayment is taken as the largest on line 15.
    fn other_state(&self, f: &Facts, a: &Assessment, line15: &[USCents]) -> (bool, Notes) {
        let mut notes = Notes::default();
        let largest = line15.iter().copied().max().unwrap_or_default();
        if largest.0 <= 0 {
            return (false, notes);
        }
        const CITE: &str =
            "Md. Code, Tax-Gen. § 13-602(b); Form 502UP (2025), Income Taxable by Another State";
        let local = f.this_year().local_tax();
        match (a.extras.taxable_by_another_state(), local) {
            (None, _) => {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    "Is at least 90% of your income also taxable by another state?",
                    "If so, no interest is charged on a shortfall no larger than your county tax.",
                );
            }
            (Some(false), _) => {}
            (Some(true), None) => {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    format!("What is your {} county income tax?", self.y.year),
                    "No interest is charged on a shortfall no larger than your county tax. This applies when at least 90% of your income is taxable by another state.",
                );
            }
            (Some(true), Some(local)) if largest <= local => {
                notes.rule(
                    CITE,
                    format!(
                        "at least 90% of taxable income is also taxable by another state, and the largest underpayment on line 15, {largest}, is not greater than the {local} of local tax, so no interest is due"
                    ),
                );
                return (true, notes);
            }
            (Some(true), Some(local)) => {
                notes.rule(
                    CITE,
                    format!(
                        "at least 90% of taxable income is also taxable by another state, but the largest underpayment on line 15, {largest}, is more than the {local} of local tax, so the exception does not apply"
                    ),
                );
            }
        }
        (false, notes)
    }
}

/// `a` × `num` ÷ `den`, rounded half up to the cent.
fn share_of(a: USCents, num: USCents, den: USCents) -> USCents {
    let n = BigInt::from(a.0) * BigInt::from(num.0);
    Rounding::RoundToCent.div(&n, &BigInt::from(den.0))
}

/// Form 502UP line 15: line 13 less line 14 by each due date, and never
/// below zero. Each includes any earlier underpayment still unpaid.
fn underpayments(a: &Assessment, ledger: &[LedgerEntry]) -> Vec<USCents> {
    ledger
        .iter()
        .enumerate()
        .map(|(i, l)| (a.needed[i] - l.credited_by()).max(USCents(0)))
        .collect()
}

/// The due dates with what was needed and credited by each. `excused` is the
/// exception for income taxable by another state, which covers every date.
fn installments(
    a: &Assessment,
    ledger: &[LedgerEntry],
    excused: bool,
) -> Result<Vec<Installment>, Error> {
    let mut out = Vec::with_capacity(ledger.len());
    for (i, l) in ledger.iter().enumerate() {
        let mut b = estimated::new_installment(i as i64 + 1, l.due());
        b.needed_by(a.needed[i])
            .credited_by(l.credited_by())
            .shortfall(l.shortfall());
        if excused || (a.excused_january && i == 3) {
            b.excused();
        }
        out.push(b.build()?);
    }
    Ok(out)
}

impl EstimatedTax for MarylandEstimated {
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
                    "with no {} Maryland return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, _, notes) = self.ledger(f, &a)?;
        let (mut excused, mut more) = (false, Notes::default());
        if !a.exempt {
            (excused, more) = self.other_state(f, &a, &underpayments(&a, &ledger));
        }
        let out = installments(&a, &ledger, excused)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .with(&more)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "marylandtaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Maryland return to measure against, there is no target to figure a penalty from",
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
        let waiver = waiver_opt.unwrap_or_default();
        let (ledger, credits, mut notes) = self.ledger(f, &a)?;
        let line15 = underpayments(&a, &ledger);
        // A third-period underpayment paid from December 15, before the
        // January due date.
        let mut december = false;
        if !a.exempt && line15[2].0 > 0 {
            let from = estimated::must_date(y.year, Month::DECEMBER, 15);
            december = credits
                .iter()
                .any(|c| !c.on().before(from) && c.on().before(a.dues[3]) && c.amount().0 > 0);
        }
        let none =
            |schedule: &[Installment], ns: &[&Notes]| -> Result<working::Result<Penalty>, Error> {
                let p = estimated::new_penalty(stop)
                    .waiver(waiver)
                    .because(a.requirement.clone(), schedule)
                    .build()?;
                let ds = self.penalty_divergences(false, false)?;
                let mut dn = Notes::default();
                dn.diverges(&ds);
                let mut b = working::new_result(p);
                b.with(&a.notes);
                for n in ns {
                    b.with(n);
                }
                Ok(b.with(&dn).build()?)
            };
        if a.exempt {
            return none(&[], &[]);
        }
        let (excused, more) = self.other_state(f, &a, &line15);
        let schedule = installments(&a, &ledger, excused)?;
        if excused {
            return none(&schedule, &[&notes, &more]);
        }

        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut unrated = Date::default();
        let mut charged = false;
        for (i, &short) in line15.iter().enumerate() {
            let due = a.dues[i];
            if short.0 == 0 || !stop.after(due) {
                continue;
            }
            let mut end = y.stop;
            if i + 1 < a.dues.len() {
                end = a.dues[i + 1];
            }
            if stop.before(end) {
                end = stop;
            }
            let mut lb = estimated::new_penalty_line(i as i64 + 1, due, short);
            let mut last_day = due;
            let mut charge = |amount: USCents, to: Date| -> Result<(), Error> {
                let (parts, gap) = monthly_interest(amount, due, to, &y.rates)?;
                if !gap.is_zero() && (unrated.is_zero() || gap.before(unrated)) {
                    unrated = gap;
                }
                for p in parts {
                    lb.part(p);
                }
                if to.after(last_day) {
                    last_day = to;
                }
                Ok(())
            };
            // A payment made before the end of the span pays part of the
            // underpayment, which is charged only the months it was late.
            let mut left = short;
            for c in &credits {
                if left.0 == 0 || !c.on().after(due) || !c.on().before(end) || c.amount().0 == 0 {
                    continue;
                }
                let piece = left.min(c.amount());
                charge(piece, c.on())?;
                left -= piece;
            }
            if left.0 > 0 {
                charge(left, end)?;
            }
            let line = lb.days(last_day.days_since(due)).build()?;
            pb.line(line);
            charged = true;
        }
        if !a.tax_known {
            pb.upper_bound();
        }
        let mut p = pb.build()?;

        if paid_in_full {
            notes.rule(
                "Md. Code, Tax-Gen. § 13-602(a); Form 502UP (2025), Line 16 instructions",
                format!(
                    "the return was filed with the whole balance paid on {}, so every underpayment still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "Md. Code, Tax-Gen. §§ 13-602(a), 13-604(a); Form 502UP (2025), lines 15 to 18",
            format!(
                "each column's underpayment, which includes any earlier one still unpaid, is charged from its due date to the next due date, or {} for the last, \
                 or to the day a later payment paid it; each month or fraction of a month is one-twelfth of the annual rate for the year it ends in; the interest to {} is {}",
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if !a.tax_known {
            notes.rule(
                "Md. Code, Tax-Gen. § 13-602(a)(2); Form 502UP (2025), line 10",
                format!(
                    "this year's Maryland tax was not given, so the interest is figured against {} of the {} tax alone; \
                     line 10 is the lesser of that and {} of this year's tax, so {} is the most the interest can be, and it is less if this year's tax turns out lower",
                    y.prior_year_share,
                    y.year - 1,
                    y.this_year_share,
                    p.total()
                ),
            );
        }
        let ds = self.penalty_divergences(p.total().0 > 0, december)?;
        notes.diverges(&ds);
        if charged {
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "Until Maryland publishes {} interest factors, the law's monthly rate is used. For 2025 it differed from the printed factors by up to 0.0004 of the shortfall.",
                    y.year
                ),
            );
        }
        if !unrated.is_zero() {
            p = p.to_builder().rated_through(unrated.add_days(-1)).build()?;
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The Maryland rate for the days after {} isn't published yet.",
                    unrated.add_days(-1).long()
                ),
            );
        }
        if p.total().0 > 0 {
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                "The law allows a separate penalty of up to 25% of the shortfall. No published rule sets the percentage, so the figures show interest only.",
            );
            if waiver_opt.is_none() {
                notes.ask(
                    PositionKind::PenaltyWaiver,
                    "Are you asking Maryland to waive the interest or penalty for reasonable cause?",
                    "The Comptroller decides reasonable cause; interest is shown before any waiver.",
                );
            }
        }
        if waiver_opt.is_some() && waiver != WaiverKind::None {
            notes.rule(
                "Md. Code, Tax-Gen. §§ 13-606(a), 13-714(a)",
                format!(
                    "a waiver for {waiver} is asked for; Maryland waives for reasonable cause, which the Comptroller decides, so the interest shown is before it"
                ),
            );
        }
        if let Some(o) = maryland_waiver(f)
            && p.total().0 > 0
        {
            let undecided = o.undecided();
            p = p.to_builder().waiver_outcome(o).build()?;
            if undecided {
                notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    "Maryland's law allows a waiver for reasonable cause without listing the causes. It doesn't say whether a casualty or disaster is one.",
                );
            }
        }
        Ok(working::new_result(p)
            .with(&a.notes)
            .with(&more)
            .with(&notes)
            .build()?)
    }

    fn explanation(&self) -> Explanation {
        MARYLAND_EXPLANATION.clone()
    }
}

/// Decides a waiver asked for. Tax-General §§ 13-606(a) and 13-714(a) let
/// the tax collector waive interest and penalty for reasonable cause,
/// without listing causes. Maryland has no retirement or disability waiver.
fn maryland_waiver(f: &Facts) -> Option<WaiverOutcome> {
    match f.positions().waiver() {
        None | Some(WaiverKind::None) => None,
        Some(WaiverKind::Casualty) => Some(estimated::waiver_undecided(
            "Maryland can waive interest for reasonable cause, but its law lists no causes. The Comptroller decides if a casualty or disaster counts.",
        )),
        Some(_) => Some(estimated::waiver_refused(
            "Maryland waives only for reasonable cause, not retirement or disability as such. The Comptroller decides reasonable cause.",
        )),
    }
}

/// Charges `principal` for each month or fraction of a month after `from`
/// up to `to`, at one-twelfth of the annual rate for the calendar year in
/// which that month ends, as § 13-604(a) charges interest. Months in a row
/// at one rate are one part, rounded to the cent.
///
/// The returned date is the first day not charged because a month ends in
/// a year with no published rate, or the zero Date.
fn monthly_interest(
    principal: USCents,
    from: Date,
    to: Date,
    rates: &RateSeries,
) -> Result<(Vec<PenaltyPart>, Date), Error> {
    let mut parts = Vec::new();
    let (mut start, mut prev_end) = (from, from);
    let mut cur: Option<RatePeriod> = None;
    let mut n = 0;
    let flush = |parts: &mut Vec<PenaltyPart>,
                 start: &mut Date,
                 n: &mut i64,
                 cur: &Option<RatePeriod>,
                 prev_end: Date|
     -> Result<(), Error> {
        let Some(cur) = cur else {
            return Ok(());
        };
        if *n == 0 {
            return Ok(());
        }
        let num = BigInt::from(principal.0) * BigInt::from(*n) * BigInt::from(cur.fine_rate().0);
        let p = estimated::new_penalty_part(PartKind::Interest, principal, Rate(0))
            .fine_rate(cur.fine_rate())
            .span(*start, prev_end)
            .days(prev_end.days_since(*start))
            .months(*n)
            .charge(Rounding::RoundToCent.div(&num, &BigInt::from(12_000_000)))
            .source(cur.source())
            .build()?;
        parts.push(p);
        (*start, *n) = (prev_end, 0);
        Ok(())
    };
    let months = estimated::months_or_part(from, to);
    for i in 1..=months {
        let mut end = from.add_months(i);
        if end.after(to) {
            end = to;
        }
        let Some(rate) = rates.on(end) else {
            flush(&mut parts, &mut start, &mut n, &cur, prev_end)?;
            return Ok((parts, prev_end.add_days(1)));
        };
        if n > 0 && cur.as_ref().is_some_and(|c| rate.from() != c.from()) {
            flush(&mut parts, &mut start, &mut n, &cur, prev_end)?;
        }
        (cur, prev_end) = (Some(rate), end);
        n += 1;
    }
    flush(&mut parts, &mut start, &mut n, &cur, prev_end)?;
    Ok((parts, Date::default()))
}

/// Written by hand and checked against research/estimated-payments/md.md.
static MARYLAND_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Maryland expects estimated payments when your tax will be more than $500. That tax is Maryland and county tax less Maryland withholding. The charge for paying too little is interest on each quarter's shortfall.",
    )
    .paragraph("Maryland charges nothing when your tax after withholding is $500 or less. Otherwise, withholding and estimated payments must reach a target. Short of it, interest is due. The target is 90% of this year's Maryland and local tax. If 110% of last year's tax is less, that is the target. The 110% applies at every income; there's no 100% figure as there is federally. County income tax is paid with the state's, so the payments have to cover both.")
    .paragraph("A quarter of the target is due by April 15, June 15 and September 15. The last quarter is due by January 15 of the next year. A date on a weekend or holiday shifts to the next business day. Withholding counts on the dates it was taken, not spread evenly over the year. Late-year withholding doesn't cover an early quarter. Filing your return and paying in full by January 31 replaces January's payment. If your income came unevenly, there's another way to measure each quarter. Use the share of the year's income you'd received by then.")
    .paragraph("Interest is figured on what was short at each due date. An earlier shortfall still unpaid counts again at every later date. Interest runs until the shortfall is paid or the return is due on April 15. The rate is set for each calendar year: 10.8133% for 2026 and 9.995% for 2027. Interest is charged by the month, and part of a month counts as a whole one. The Comptroller may waive the interest for reasonable cause. The law also allows a separate penalty of up to 25% of the shortfall. The underpayment form doesn't figure that penalty.")
    .versus_federal("Maryland differs from federal rules in six ways. Its $500 threshold counts county tax. Everyone uses 110% of last year's tax. Withholding counts on its dates. Uneven income uses the share of the year's income received, not annualizing. A shortfall is charged again at every later quarter. The yearly rate has a 9% floor and is charged by the month.")
    .source("Md. Code, Tax-General §§ 1-201, 10-815, 10-820, 10-902, 13-602, 13-604, 13-606, 13-702, 13-714")
    .source("Payment Voucher Worksheet for Estimated Tax and Extension Payments (PVW), tax year 2026")
    .source("Form 502UP, Underpayment of Estimated Income Tax by Individuals, and its instructions, 2025")
    .source("2025 Maryland State and Local Tax Forms and Instructions (resident booklet)")
    .source("Maryland Register 53:18 (September 4, 2026), Notice of Interest Rate on Refunds and Moneys Owed to the State")
    .build()
    .unwrap_or_default()
});
