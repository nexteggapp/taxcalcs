//! Hawaii's estimated tax rules, HRS § 235-97 and HAR § 18-235-97, as the
//! shared [`estimated::EstimatedTax`] trait. Ported from estimated.go.
//!
//! HRS § 235-97 sets Hawaii's own rate, threshold, due dates and percentages,
//! and § 235-97(f)(4) adopts subsections (d), (e)(2), (e)(3), (h), (i), (j),
//! (k) and (l) of 26 U.S.C. § 6654 with the due dates moved to the 20th and
//! the required annual payment set at 60% or 100%. Nothing here imports the
//! federal crate: what Hawaii takes from § 6654 is written out below and
//! cited to § 235-97(f)(4).
//!
//! The figure follows the department's forms, Form N-210 and its
//! instructions for 2025 and Tax Facts 2019-3 (revised February 2026), and the
//! result's Divergences say where the statute or the rule reads otherwise.
//! HRS § 235-97 was last amended in 2003 and the department's summary of the
//! 2026 session, Announcement No. 2026-06, lists no change to it, so no form
//! here predates the law it describes.
//!
//! Where last year's return was joint and this year's is not, or the other
//! way round, the caller passes last year's tax combined or split as federal
//! Publication 505 directs, which Form N-210 line 7 adopts.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Factor, Facts, Installment,
    Jurisdiction, LedgerEntry, Month, PartKind, Payment, Penalty, Requirement, Rounding,
    WaiverKind,
};
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

/// Hawaii's postal code.
const JURISDICTION: Jurisdiction = estimated::HAWAII;

/// The facts Hawaii's rules read that no other jurisdiction does. Carry them
/// in [`estimated::FactsBuilder::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    prior_farmer: bool,
    prior_farmer_answered: bool,
    nonresident_alien: bool,
    nonresident_answered: bool,
}

impl EstimatedFacts {
    /// Whether last year's gross income was at least two-thirds from farming
    /// or fishing, which puts the filer under the farmer and fisherman rules
    /// this year (N-210 instructions; 26 U.S.C. § 6654(i)(2)(B), adopted by
    /// HRS § 235-97(f)(4)), and whether the caller said, as `(farmer,
    /// answered)`. This year's share is [`estimated::Facts::farm_share`] and
    /// [`estimated::Facts::fish_share`].
    pub fn prior_year_farmer_or_fisher(&self) -> (bool, bool) {
        (self.prior_farmer, self.prior_farmer_answered)
    }

    /// Whether the filer is a nonresident alien with no wages subject to
    /// Hawaii withholding, who pays on a schedule of their own (N-210
    /// instructions, Part III; 26 U.S.C. § 6654(j)), and whether the caller
    /// said, as `(nonresident, answered)`.
    pub fn nonresident_alien_without_wages(&self) -> (bool, bool) {
        (self.nonresident_alien, self.nonresident_answered)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts [`EstimatedFacts`] with nothing said.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Answers whether last year's gross income was two-thirds from farming
    /// or fishing.
    pub fn prior_year_farmer_or_fisher(&mut self, farmer: bool) -> &mut Self {
        self.e.prior_farmer = farmer;
        self.e.prior_farmer_answered = true;
        self
    }

    /// Answers whether the filer is a nonresident alien with no wages subject
    /// to Hawaii withholding.
    pub fn nonresident_alien_without_wages(&mut self, nonresident: bool) -> &mut Self {
        self.e.nonresident_alien = nonresident;
        self.e.nonresident_answered = true;
        self
    }

    /// Has nothing to refuse: each fact is a yes or a no.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// N-210 Exception 1 and HRS § 235-97(a)(6): no penalty when tax less
    /// withholding is under it.
    pub(crate) threshold: USCents,

    /// The two tests of § 235-97(f)(4).
    pub(crate) this_year_percent: Rate,
    pub(crate) prior_percent: Rate,

    /// The dates the statute and the forms print, before moving for a
    /// weekend or holiday. The penalty's months are counted from them.
    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// § 6654(h) and HAR § 18-235-97(h)(2): a return filed and paid by it
    /// excuses the fourth installment.
    pub(crate) file_and_pay_by: Date,

    /// § 235-97(f)(2)(A): the twentieth day of the fourth month after the
    /// year.
    pub(crate) stop: Date,

    /// The factor N-210 line 20 multiplies by for each month or part of a
    /// month; `annual_rate` is the rate it stands for, twelve months of
    /// § 235-97(f)'s two-thirds of one percent, for the working.
    pub(crate) monthly: Factor,
    pub(crate) annual_rate: Rate,
    pub(crate) rate_source: String,

    /// N-210 Schedule A's column headings and line 17.
    pub(crate) period_ends: Vec<Date>,
    pub(crate) annualised_percentages: Vec<Rate>,
}

/// § 6654(d)(1)(A), adopted by § 235-97(f)(4), and N-210 line 9: each
/// installment is a quarter of the required annual payment. § 235-97(g) deems
/// withholding paid in the same equal parts.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// Returns Hawaii's estimated tax rules for a tax year, or
/// [`estimated::Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(HawaiiEstimated { y }))
}

struct HawaiiEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,

    /// The due dates moved for weekends and holidays, as a schedule shows
    /// them. The ledger and the penalty's months use the unmoved dates.
    dues: Vec<Date>,

    /// Each installment's own amount: a quarter of the target, or N-210
    /// Schedule A line 24 under the annualised method.
    installments: Vec<USCents>,

    /// A return filed and paid by January 31: the fourth installment is
    /// excused and the earlier ones stop on January 20.
    filed_early: bool,

    notes: Notes,
}

/// Go's `%T` for the extras a caller is most likely to pass by mistake.
fn type_name(s: &(dyn Any + Send + Sync)) -> &'static str {
    if s.is::<&str>() || s.is::<String>() {
        "string"
    } else {
        "another jurisdiction's extras"
    }
}

/// `amount` in whole dollars, or "just over" or "just under" `limit` when
/// rounding would show it as `limit`.
fn near(amount: USCents, limit: USCents) -> String {
    if amount > limit && amount.whole() == limit.whole() {
        return format!("just over {}", limit.whole());
    }
    if amount < limit && amount.whole() == limit.whole() {
        return format!("just under {}", limit.whole());
    }
    amount.whole()
}

/// The first three letters of the month's name.
fn short_month(d: Date) -> String {
    d.month().to_string().chars().take(3).collect()
}

/// "Apr 20" inside the tax year and "Jan 31, 2027" outside it.
fn short_date(d: Date, year: i64) -> String {
    let m = short_month(d);
    if d.year() == year {
        return format!("{m} {}", d.day());
    }
    format!("{m} {}, {}", d.day(), d.year())
}

fn earliest(a: Date, b: Date) -> Date {
    if b.before(a) { b } else { a }
}

impl HawaiiEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "hawaiitaxcalc: the facts carry {}, not hawaiitaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts, extras: &EstimatedFacts) -> Result<(), Error> {
        let (farmer_last_year, _) = extras.prior_year_farmer_or_fisher();
        let (nonresident, _) = extras.nonresident_alien_without_wages();
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "hawaiitaxcalc: these are Hawaii's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "hawaiitaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() || farmer_last_year {
            return Err(Error::with_detail(
                Error::Unsupported,
                "HRS § 235-97(f)(4) adopts 26 U.S.C. § 6654(i), and Form N-210 gives farmers and fishermen one installment by January 20, \
                 or no penalty for a return filed and paid by March 1; that is not built, and whether § 6654(i)'s 66 2/3% applies in place of the 60% the form uses is not settled",
            ));
        }
        if nonresident {
            return Err(Error::with_detail(
                Error::Unsupported,
                "Form N-210 and 26 U.S.C. § 6654(j) give a nonresident alien without Hawaii wage withholding one-half by June 20 and a quarter on each later date, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let Some(&end) = self.y.period_ends.get(i) else {
                    return Err(Error::Invalid(format!(
                        "hawaiitaxcalc: N-210 Schedule A has no period {}",
                        i + 1
                    )));
                };
                if p.end() != end {
                    return Err(Error::Invalid(format!(
                        "hawaiitaxcalc: N-210 Schedule A period {} ends on {}, not {}",
                        i + 1,
                        end,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and HRS § 235-97, HAR § 18-235-97 or the
    /// federal text § 235-97(f)(4) adopts part ways on a rule every Hawaii
    /// computation uses. They are found in research/estimated-payments/hi.md
    /// and checked against the texts in testdata/. The flags say whether each
    /// changes anything for the facts.
    fn divergences(
        &self,
        residence: bool,
        rate: bool,
        late_start: bool,
    ) -> Result<Vec<Divergence>, Error> {
        let mut out = Vec::new();
        for b in [
            working::new_divergence("No liability last year")
                .form("There's no penalty if last year had no tax liability and was twelve months long. \
                       You must also have been a full-year Hawaii resident that year. A nonresident or part-year resident doesn't qualify.",
                    "Form N-210 (Rev. 2025), Part I box A and instructions, Exceptions to the Penalty 2; Tax Facts 2019-3 (rev. February 2026), questions 5 and 6")
                .statute("There's no penalty if last year had no tax liability and was twelve months long. \
                          You must also have been a U.S. citizen or resident for all of it. The rule has no Hawaii residence test.",
                    "HRS § 235-97(f)(4), adopting 26 U.S.C. § 6654(e)(2); HAR § 18-235-97(b)(2)(B)")
                .uses(Reading::Form).affects(residence).clone(),
            working::new_divergence("The monthly rate")
                .form("Each month or part of a month is charged at .00667 of the underpayment.",
                    "Form N-210 (Rev. 2025), Part IV line 20 and instructions, Part IV")
                .statute("The charge is two-thirds of one per cent a month or fraction of a month. That is .006666…",
                    "HRS § 235-97(f)")
                .uses(Reading::Form).affects(rate).clone(),
            working::new_divergence("A requirement first met after March 31")
                .form("Each installment is a quarter of the required annual payment. Under Schedule A, it is the annualised amount instead. \
                       That holds whenever you first had to pay.",
                    "Form N-210 (Rev. 2025), line 9 and instructions, Figure Your Underpayment")
                .statute("If you first had to pay in April or May, you pay three equal parts from June 20. \
                          If it was June, July or August, you pay two equal parts from September 20. If it was after August 31, you pay it all by January 20.",
                    "HAR § 18-235-97(e)(2)(A); HRS § 235-97(d)")
                .uses(Reading::Form).affects(late_start).clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        let extras = self.extras(f)?;
        self.check(f, &extras)?;
        let y = &self.y;
        let mut a = Assessment::default();

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let has_prior = f.prior_year().is_some();
        let prior = f.prior_year().unwrap_or_default();
        let last = y.year - 1;

        // 60% of this year's tax. § 235-97(f)(4); N-210 line 6.
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
                "HRS § 235-97(f)(4); Form N-210 (Rev. 2025), line 6",
                format!(
                    "{} of this year's Hawaii tax of {tax} is {}",
                    y.this_year_percent,
                    this_year.amount()
                ),
            );
        }

        // 100% of last year's tax, for a filed twelve-month Hawaii return.
        // There is no higher percentage at any income.
        let prior_tax = if has_prior {
            prior.figures().and_then(|pf| pf.tax()).unwrap_or_default()
        } else {
            USCents(0)
        };
        let unavailable = |why: String| {
            estimated::new_target_option(Basis::PriorYearTax, y.prior_percent, USCents(0))
                .unavailable(why)
                .build()
        };
        let prior_opt = if !has_prior {
            unavailable(format!("Needs your {last} Hawaii return"))?
        } else if prior.not_required_to_file() {
            unavailable(format!("You didn't have to file a {last} Hawaii return"))?
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Hawaii return"))?
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Hawaii return covered {} months, not 12",
                prior.months()
            ))?
        } else {
            let amount = y.prior_percent.of(prior_tax);
            a.notes.rule(
                "HRS § 235-97(f)(4); Form N-210 (Rev. 2025), line 7",
                format!(
                    "{} of the {prior_tax} tax on the {last} Hawaii return is {amount}, at any income",
                    y.prior_percent
                ),
            );
            estimated::new_target_option(Basis::PriorYearTax, y.prior_percent, amount).build()?
        };

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Hawaii tax to be?", y.year),
                format!(
                    "It sets the {} option and decides whether you're under the {} threshold.",
                    y.this_year_percent,
                    y.threshold.whole()
                ),
            );
        }

        // No liability last year. The forms need a full-year Hawaii resident;
        // § 6654(e)(2), which § 235-97(f)(4) adopts, needs U.S. residence. The
        // form is followed, and the statute's reading is reported.
        let no_liability = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax.0 == 0) || prior.not_required_to_file());
        let resident_opt = prior.full_year_resident();
        let residence_answered = resident_opt.is_some();
        let resident = resident_opt.unwrap_or_default();
        if no_liability {
            if !residence_answered {
                a.notes.ask(
                    PositionKind::Residency,
                    format!("Were you a Hawaii resident for all of {last}?"),
                    format!(
                        "If so, no Hawaii tax due for {last} means there's no penalty for {}.",
                        y.year
                    ),
                );
            } else if !resident {
                a.notes.rule(
                    "HRS § 235-97(f)(4); 26 U.S.C. § 6654(e)(2)",
                    format!(
                        "you had no Hawaii liability for {last}, a twelve-month year, but were not a Hawaii resident all of it, so Form N-210's exception does not apply and the figures here are the form's; \
                         the federal text Hawaii adopts asks only that you were a U.S. citizen or resident throughout {last}, and if you were, the exception is yours to claim"
                    ),
                );
            }
        }

        let spread = if f.elections().annualised() {
            "They follow the annualised installments."
        } else {
            "They are due a quarter by each date."
        };
        if no_liability && residence_answered && resident {
            a.exempt = true;
            let none = estimated::new_exception("No Hawaii tax due last year").build()?;
            rb.required(
                false,
                format!(
                    "You had no Hawaii tax due for {last}, a full twelve-month year. You were a Hawaii resident all of it. No estimated payments are needed for {}, and no penalty can arise.",
                    y.year
                ),
            )
            .exempt(none);
            a.notes.rule(
                "Form N-210 (Rev. 2025), Exceptions to the Penalty 2; HRS § 235-97(f)(4); HAR § 18-235-97(b)(2)(B)",
                format!(
                    "the {last} return showed no liability for a full twelve months as a Hawaii resident, so there is no penalty for {}",
                    y.year
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target needs your {} Hawaii tax, because your {last} return can't set one.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your withholding of {} already reaches {} of your {last} tax, {}. No estimated payments are needed, whatever your {} tax turns out to be.",
                    withheld.whole(),
                    prior_opt.percent(),
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {} of your {last} tax, {}. After {} of withholding, that's {} of estimated payments. {spread} They keep you clear of a penalty whatever your {} tax turns out to be.",
                    prior_opt.percent(),
                    target.whole(),
                    withheld.whole(),
                    (target - withheld).whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if tax - withheld < y.threshold {
            a.exempt = true;
            let under =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            let leaves = if tax < withheld {
                estimated::leaves(tax - withheld)
            } else {
                format!(
                    "leaves {}. That is under {}",
                    near(tax - withheld, y.threshold),
                    y.threshold.whole()
                )
            };
            rb.required(
                false,
                format!(
                    "Your Hawaii tax is {}, and {} was withheld. That {leaves}. No estimated payments are needed, and no penalty can arise.",
                    tax.whole(),
                    withheld.whole()
                ),
            )
            .exempt(under);
            a.notes.rule(
                "Form N-210 (Rev. 2025), line 5 and Exceptions to the Penalty 1; HRS § 235-97(a)(6); HAR § 18-235-97(b)(2)(A)",
                format!(
                    "{tax} of tax less {withheld} withheld is {}, under {}, so there is no penalty",
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Hawaii withholding of {} already reaches your target of {}. No estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your Hawaii tax is {}, and {} was withheld. That leaves {}. That is {} or more, so estimated payments are needed. \
                     You need {} of estimated payments to reach your target of {}. {spread}",
                    tax.whole(),
                    withheld.whole(),
                    near(tax - withheld, y.threshold),
                    y.threshold.whole(),
                    (target - withheld).whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
            if target - withheld < y.threshold {
                a.notes.rule(
                    "Tax Facts 2019-3 (rev. February 2026), Estimated Tax Worksheet line 13; Form N-210 (Rev. 2025), lines 5 to 16",
                    format!(
                        "the worksheet says no estimated payments are required at this time when the required annual payment less withholding, here {}, is under {}; \
                         Form N-210 tests the tax less withholding instead, {} here, and still charges on each quarter that is short, so the payments are shown as needed",
                        target - withheld,
                        y.threshold,
                        tax - withheld
                    ),
                );
            }
        }
        a.requirement = rb.build()?;
        if !a.exempt {
            // Under an exception last year's return can't change the target.
            if !has_prior {
                a.notes.ask(
                    PositionKind::PriorYearReturn,
                    format!("Did you file a {last} Hawaii return, and what was its tax?"),
                    format!(
                        "With it, your target could be {} of last year's tax, not {} of this year's.",
                        y.prior_percent, y.this_year_percent
                    ),
                );
            }
            if !extras.prior_year_farmer_or_fisher().1 {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "Was at least two-thirds of your {last} gross income from farming or fishing?"
                    ),
                    "If so, Hawaii's farmer and fisherman rules apply instead.",
                );
            }
            if !extras.nonresident_alien_without_wages().1 {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    "Are you a nonresident alien with no wages subject to Hawaii withholding?",
                    "If so, your payments follow a schedule of their own.",
                );
            }
        }

        // The due dates. § 235-97(a)(2) fixes them on the 20th, and HRS
        // § 231-21 moves one that falls on a weekend or legal holiday.
        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        for i in 0..a.dues.len() {
            if a.dues[i] != y.dues[i] {
                a.notes.rule(
                    "HRS § 231-21; Tax Facts 2019-3 (rev. February 2026), question 8; Form N-210 (Rev. 2025) instructions, line 10 rule 6",
                    format!(
                        "the installment due {} falls on a {}, so a payment on {} is on time and counts as paid on {}",
                        y.dues[i].long(),
                        y.dues[i].weekday(),
                        a.dues[i].long(),
                        y.dues[i].long()
                    ),
                );
            }
        }
        let cumulative = estimated::cumulative_shares(a.requirement.target(), &quarters())?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "HRS § 235-97(a)(2), (f)(4); Form N-210 (Rev. 2025), line 9",
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

        if f.elections().annualised() && !a.exempt {
            self.annualise(f, &mut a)?;
        }

        let march31 = estimated::must_date(y.year, Month::MARCH, 31);
        let met = f.requirement_first_met();
        if !met.is_zero() && met.after(march31) && !a.exempt {
            a.notes.rule(
                "HAR § 18-235-97(e)(2)(A); Form N-210 (Rev. 2025), line 9",
                format!(
                    "you first had to pay estimated tax on {}; the rule lets a late starter spread the year's estimate over the remaining dates, \
                     but Form N-210 measures every installment as a quarter, or by Schedule A, so the figures here are the form's and the earlier installments are not excused",
                    met.long()
                ),
            );
        }

        // § 6654(h), adopted by § 235-97(f)(4), and HAR § 18-235-97(h)(2): a
        // return filed and paid by January 31 excuses the fourth installment.
        // January 31, 2027 is a Sunday. Neither the rule nor the forms say the
        // date moves, so it is not moved here.
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(y.file_and_pay_by) && !a.exempt {
            a.filed_early = true;
            a.notes.rule(
                "HRS § 235-97(f)(4), adopting 26 U.S.C. § 6654(h); HAR § 18-235-97(h)(2); Form N-210 (Rev. 2025) instructions, Part IV",
                format!(
                    "the return was filed and paid on {}, by {}, so there is no penalty on the fourth installment, and the earlier installments' penalty stops on {}",
                    filed.long(),
                    y.file_and_pay_by.long(),
                    y.dues[3].long()
                ),
            );
        }
        let moved = y.calendar.next_business_day(y.file_and_pay_by);
        if !a.exempt && filed.after(y.file_and_pay_by) && !filed.after(moved) {
            let by_date = format!(
                "{}, {} {}",
                y.file_and_pay_by.weekday(),
                short_month(y.file_and_pay_by),
                y.file_and_pay_by.day()
            );
            a.notes.ask(
                PositionKind::UnsettledRule,
                "",
                format!(
                    "Hawaii hasn't said if filing and paying on {} is by {by_date}. \
                     The figures treat it as late, so your January installment isn't excused.",
                    short_date(moved, y.year)
                ),
            );
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        // What each divergence turns on in these facts.
        let residence = no_liability && residence_answered && !resident;
        let late_start = !a.exempt && f.requirement_first_met().after(march31);
        let mut rate = false;
        if !a.exempt && a.requirement.target_known() {
            rate = self.rate_affects(f, &a)?;
        }
        let divergences = self.divergences(residence, rate, late_start)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// Whether the printed .00667 and the statute's two-thirds of one percent
    /// give a different penalty, figured to the latest day the penalty can
    /// run to.
    fn rate_affects(&self, f: &Facts, a: &Assessment) -> Result<bool, Error> {
        let y = &self.y;
        let (ledger, _) = self.ledger(f, a)?;
        let mut stop = y.stop;
        let filed = f.return_filed();
        if a.filed_early {
            stop = y.dues[3];
        } else if !filed.is_zero() && filed.before(stop) {
            stop = filed;
        }
        for (i, l) in ledger.iter().enumerate() {
            if l.shortfall().0 <= 0 {
                continue;
            }
            for piece in l.pieces() {
                let to = match piece.paid_on() {
                    Some(on) => earliest(on, stop),
                    None => stop,
                };
                if !to.after(y.dues[i]) {
                    continue;
                }
                let months = estimated::months_or_part(y.dues[i], to);
                // Two-thirds of one percent is 2 / 300, rounded to the cent.
                let n = piece.amount().0 * months * 2;
                let exact = USCents((n + 150) / 300);
                if exact != y.monthly.of(piece.amount(), months, Rounding::RoundToCent) {
                    return Ok(true);
                }
            }
        }
        Ok(false)
    }

    /// Replaces the installments with N-210 Schedule A line 24: the
    /// applicable percentage of the period's tax after credits on annualised
    /// income, less earlier installments, and never more than the regular
    /// installment plus what the method lowered earlier. That is
    /// § 6654(d)(2), as § 235-97(f)(4) adopts it, with Hawaii's 15%, 30%, 45%
    /// and 60%.
    fn annualise(&self, f: &Facts, a: &mut Assessment) -> Result<(), Error> {
        let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
        let ann = estimated::annualised(&taxes, &self.y.annualised_percentages, &a.installments)?;
        let p = &self.y.annualised_percentages;
        a.notes.rule(
            "HRS § 235-97(f)(4), adopting 26 U.S.C. § 6654(d)(2); Form N-210 (Rev. 2025), Schedule A, lines 17 to 24",
            format!(
                "under the annualised method the installments are {}, {}, {} and {}: {}, {}, {} and {} of the tax after credits on annualised income, \
                 less earlier installments, and never more than the regular installment plus what the method lowered earlier",
                ann[0], ann[1], ann[2], ann[3], p[0], p[1], p[2], p[3]
            ),
        );
        a.installments = ann;
        Ok(())
    }

    /// Moves a credit made after an unmoved due date, up to the day the date
    /// moved to, back onto the due date. N-210 instructions, line 10 rule 6:
    /// a payment made on the next working day "will be considered paid on the
    /// payment due date".
    fn on_the_due_date(&self, credits: &[Payment], moved: &[Date]) -> Result<Vec<Payment>, Error> {
        let mut out = Vec::with_capacity(credits.len());
        for c in credits {
            let mut on = c.on();
            for (j, &due) in self.y.dues.iter().enumerate() {
                if on.after(due) && !on.after(moved[j]) {
                    on = due;
                }
            }
            out.push(estimated::new_payment().on(on).amount(c.amount()).build()?);
        }
        Ok(out)
    }

    /// Applies the credits to the installments on the unmoved due dates, with
    /// the fourth set to nothing when filing early excused it.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &self.y.dues)?
        } else {
            estimated::credits(f, &self.y.dues, &quarters())?
        };
        let credits = self.on_the_due_date(&credits, &a.dues)?;
        let mut required = a.installments.clone();
        if a.filed_early {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&self.y.dues, &required, &credits)?;
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "HRS § 235-97(g); Form N-210 (Rev. 2025), Part I box D",
                format!(
                    "the {} withheld is credited on the dates it was actually withheld, which the filer must show",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "HRS § 235-97(g); Form N-210 (Rev. 2025) instructions, line 10 rule 1",
                format!(
                    "the {} withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        notes.rule(
            "HRS § 235-97(f)(2)(B); Form N-210 (Rev. 2025) instructions, line 10 rule 2 and line 19",
            format!(
                "a payment is credited against the unpaid installments in the order they fall due, and last year's overpayment applied forward counts as paid on {}, as the form treats it when last year's return was paid in full by its due date",
                self.y.dues[0].long()
            ),
        );
        Ok((ledger, notes))
    }

    /// N-210 Part IV: for each piece of each shortfall, the months from the
    /// unmoved due date to the day it was paid or `stop`, any part of a month
    /// counted whole, times the printed factor, times the piece. The
    /// 20th-to-20th count of [`estimated::months_or_part`] is the form's month
    /// from the 21st to the 20th.
    fn charge(
        &self,
        ledger: &[LedgerEntry],
        moved: &[Date],
        stop: Date,
        waiver: WaiverKind,
        req: &Requirement,
        sched: &[Installment],
    ) -> Result<Penalty, Error> {
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(req.clone(), sched);
        for (i, l) in ledger.iter().enumerate() {
            if l.shortfall().0 <= 0 {
                continue;
            }
            let from = self.y.dues[i];
            let mut lb = estimated::new_penalty_line(l.number(), moved[i], l.shortfall());
            let mut last = from;
            for piece in l.pieces() {
                let to = match piece.paid_on() {
                    Some(on) => earliest(on, stop),
                    None => stop,
                };
                if !to.after(from) {
                    continue;
                }
                if to.after(last) {
                    last = to;
                }
                let months = estimated::months_or_part(from, to);
                let part = estimated::new_penalty_part(
                    PartKind::Interest,
                    piece.amount(),
                    self.y.annual_rate,
                )
                .span(from, to)
                .days(to.days_since(from))
                .months(months)
                .charge(
                    self.y
                        .monthly
                        .of(piece.amount(), months, Rounding::RoundToCent),
                )
                .source(self.y.rate_source.clone())
                .build()?;
                lb.part(part);
            }
            let line = lb.days(last.days_since(from)).build()?;
            pb.line(line);
        }
        pb.build()
    }
}

/// The installments the ledger gives, on the moved dates. The fourth is
/// excused for a return filed and paid by January 31.
fn schedule(ledger: &[LedgerEntry], a: &Assessment) -> Result<Vec<Installment>, Error> {
    let mut out = Vec::with_capacity(ledger.len());
    let mut needed = USCents(0);
    for (i, l) in ledger.iter().enumerate() {
        needed += a.installments[i];
        let mut b = estimated::new_installment(i as i64 + 1, a.dues[i]);
        b.needed_by(needed)
            .credited_by(l.credited_by())
            .shortfall(l.shortfall());
        if a.filed_early && i == 3 {
            b.excused();
        }
        out.push(b.build()?);
    }
    Ok(out)
}

impl EstimatedTax for HawaiiEstimated {
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
                    "with no {} Hawaii return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let out = schedule(&ledger, &a)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "hawaiitaxcalc: a penalty needs the date to figure it to".to_string(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Hawaii return to measure against, there is no target to figure a penalty from",
                    self.y.year - 1
                ),
            ));
        }
        let y = &self.y;
        let mut stop = earliest(as_of, y.stop);
        let mut paid_in_full = false;
        let filed = f.return_filed();
        if a.filed_early {
            stop = earliest(stop, y.dues[3]);
        } else if !filed.is_zero() && filed.before(stop) {
            (stop, paid_in_full) = (filed, true);
        }
        let waiver_answer = f.positions().waiver();
        let waiver = waiver_answer.unwrap_or_default();
        let (ledger, mut notes) = self.ledger(f, &a)?;
        let sched = schedule(&ledger, &a)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let mut p = self.charge(&ledger, &a.dues, stop, waiver, &a.requirement, &sched)?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "HRS § 235-97(f)(4)",
                format!(
                    "this year's Hawaii tax was not given, so the penalty is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and {} of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    y.this_year_percent,
                    p.total()
                ),
            );
        }
        if let Some(o) = estimated::standard_waiver(f, "the Hawaii Department of Taxation", 62)
            && p.total().0 > 0
        {
            // HRS § 235-97(f)(4) adopts the waivers of 26 U.S.C. § 6654(e)(3),
            // as Form N-210's instructions state them.
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        if paid_in_full {
            notes.rule(
                "HRS § 235-97(f)(2)(B)",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "HRS § 235-97(f), (f)(2); Form N-210 (Rev. 2025), Part IV, lines 18 to 20",
            format!(
                "each shortfall is charged {} of itself for each month or part of a month from its due date until paid or {}, a month running from the 21st to the 20th, \
                 with each payment applied to the oldest shortfall first; the penalty to {} is {}",
                y.monthly,
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        notes.rule(
            "Form N-11 (Rev. 2025) instructions, Rounding Off to Whole Dollars",
            "each charge here is rounded to the cent; the return rounds its entries to whole dollars, \
             either each entry or only the total as the filer chooses, so the figure entered can differ by less than a dollar a line",
        );
        if p.total().0 > 0 && waiver_answer.is_none() {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Do you want a waiver for a casualty, disaster, retiring after 62 or disability?",
                "Unusual circumstances count too; retiring or disability needs reasonable cause. \
                 Until Hawaii grants a waiver, the figures include the full penalty.",
            );
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        HAWAII_EXPLANATION.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/hi.md.
static HAWAII_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Hawaii expects estimated payments if you expect $500 or more of Hawaii tax due. That is the tax left after withholding and credits. \
         Each quarter's shortfall is charged 2/3 of 1% for each month it stays unpaid. Part of a month counts as a full month.")
        .paragraph("Your target is 60% of this year's Hawaii tax or 100% of last year's, if smaller. Withholding and estimated payments that reach it keep you clear of the penalty. \
                    There is no 110% figure for higher incomes. Last year's figure needs a Hawaii return covering a full twelve months. \
                    Someone who filed no Hawaii return last year has only the 60% figure.")
        .paragraph("Each payment is a quarter, due April 20, June 20, September 20 and January 20. January 20 is in the next year. \
                    A date on a weekend or holiday shifts to the next business day. In 2026 the June date shifts to June 22 and the September date to September 21. \
                    Withholding counts as paid evenly over the four dates. That holds unless you show when it was withheld. \
                    Filing and paying in full by January 31 removes the January payment's penalty. The charge on earlier shortfalls then stops at January 20.")
        .paragraph("There is no penalty if this year's tax less withholding is under $500. Nor is there one if you had no Hawaii tax due last year. \
                    That year must be twelve months, all as a Hawaii resident. The department can waive the penalty for a casualty or disaster. \
                    The same goes if you retired after 62 or became disabled with reasonable cause.")
        .paragraph("The charge is two-thirds of one percent of the shortfall, 8% a year. \
                    It applies for each month or part of a month the shortfall stays unpaid. A month runs from the 21st to the 20th, so one day late costs a full month. \
                    Form N-210 multiplies by .00667 a month. The charge stops when the shortfall is paid, or on April 20 of the next year. That is when the return is due. \
                    With uneven income, Schedule A of Form N-210 can lower the early payments. It uses 15%, 30%, 45% and 60% of the tax on annualised income.")
        .versus_federal("Hawaii's test on this year's tax is 60%, not 90%. Last year's test is 100% at every income, with no 110% step. \
                         The threshold is $500, not $1,000, and payments fall on the 20th, not the 15th. \
                         The charge is 2/3 of 1% for each month or part of one, not a daily rate. One day late costs a full month.")
        .source("HRS §§ 235-97, 231-21")
        .source("HAR § 18-235-97")
        .source("Tax Facts 2019-3, Estimated Income Tax for Individuals (rev. February 2026)")
        .source("Form N-210, Underpayment of Estimated Tax by Individuals, Estates, and Trusts, and its instructions (Rev. 2025)")
        .source("26 U.S.C. § 6654(e), (h), as adopted by HRS § 235-97(f)(4)")
        .build()
        .unwrap_or_default()
});
