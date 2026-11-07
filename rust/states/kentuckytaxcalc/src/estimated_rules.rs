//! Kentucky's estimated tax rules, KRS 141.305, as the shared
//! [`estimated::EstimatedTax`] trait. Ported from estimated.go.
//!
//! KRS 141.305(3) adopts 26 U.S.C. § 6654 with four exceptions, so the shape
//! is the federal one: 90% of this year's tax or 100% of last year's, 110%
//! above $150,000 of last year's Kentucky AGI, four quarters on the federal
//! dates, and interest by the day. What differs is the threshold, $500 rather
//! than $1,000; two income tests that decide whether payments are needed at
//! all; a Kentucky rate fixed for each calendar year; and waivers only for
//! reasonable cause under KRS 131.175.
//!
//! The figure follows the department's forms, the 2026 voucher instructions
//! (42A740-S4, 9-25) and the 2025 Form 2210-K (42A740-S1, 10-25), and the
//! result's Divergences say where KRS 141.305 reads otherwise. KRS 141.305
//! has not been amended since 2019. KRS 131.183, which sets the rate, was
//! amended by 2026 Ky. Acts ch. 161, effective July 15, 2026, after the 2210-K
//! was printed; whether that act changed the estimated tax rate is not
//! established (research/estimated-payments/ky.md), so the form's rate is
//! used and the question is reported in Required.

use std::any::Any;
use std::sync::LazyLock;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    LedgerEntry, Penalty, PriorReturn, RateSeries, Requirement, TargetOption, TargetOptionBuilder,
    WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents, percent};
use working::{Divergence, Notes, PositionKind, Reading};

/// Kentucky's postal code.
const JURISDICTION: Jurisdiction = estimated::KENTUCKY;

/// The facts Kentucky's rules read that no other jurisdiction does. Carry
/// them in [`estimated::FactsBuilder::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    non_wage_income: Option<USCents>,
    filing_required: Option<bool>,
    us_resident: Option<bool>,
    combined: Option<bool>,
    spouse_agi: Option<USCents>,
}

impl EstimatedFacts {
    /// The year's gross income from sources other than wages on which
    /// Kentucky tax is withheld, if given. KRS 141.305(1)(a) requires
    /// estimated payments when it can be expected to exceed $5,000.
    pub fn non_wage_income(&self) -> Option<USCents> {
        self.non_wage_income
    }

    /// Whether this year's Kentucky AGI can be expected to be at least the
    /// amount for which KRS 141.180 requires a return, the other test in KRS
    /// 141.305(1), and whether the caller said, as `(met, answered)`.
    pub fn filing_threshold_met(&self) -> (bool, bool) {
        answer(self.filing_required)
    }

    /// Whether the filer was a U.S. citizen or resident alien for all of last
    /// year, which Form 2210-K's no-liability exception needs, and whether
    /// the caller said, as `(resident, answered)`. It is not Kentucky
    /// residence.
    pub fn prior_year_us_resident(&self) -> (bool, bool) {
        answer(self.us_resident)
    }

    /// Whether a married couple filing separately does so on one combined
    /// Form 740, and whether the caller said, as `(combined, answered)`.
    /// KY-TAM-19-02 adds the couple's incomes together for the 110% test.
    pub fn combined_return(&self) -> (bool, bool) {
        answer(self.combined)
    }

    /// The spouse's Kentucky AGI for last year, for a couple on a combined
    /// return, if given. It is added to [`estimated::Figures::state_agi`] of
    /// the prior return for the 110% test.
    pub fn spouse_prior_year_agi(&self) -> Option<USCents> {
        self.spouse_agi
    }
}

fn answer(a: Option<bool>) -> (bool, bool) {
    (a.unwrap_or_default(), a.is_some())
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
    /// Sets the year's gross income other than wages with Kentucky
    /// withholding.
    pub fn non_wage_income(&mut self, a: USCents) -> &mut Self {
        self.e.non_wage_income = Some(a);
        self
    }

    /// Answers whether Kentucky AGI will reach the filing threshold.
    pub fn filing_threshold_met(&mut self, met: bool) -> &mut Self {
        self.e.filing_required = Some(met);
        self
    }

    /// Answers whether the filer was a U.S. citizen or resident all of last
    /// year.
    pub fn prior_year_us_resident(&mut self, resident: bool) -> &mut Self {
        self.e.us_resident = Some(resident);
        self
    }

    /// Answers whether a couple filing separately does so on one combined
    /// return.
    pub fn combined_return(&mut self, combined: bool) -> &mut Self {
        self.e.combined = Some(combined);
        self
    }

    /// Sets the spouse's Kentucky AGI for last year.
    pub fn spouse_prior_year_agi(&mut self, a: USCents) -> &mut Self {
        self.e.spouse_agi = Some(a);
        self
    }

    /// Refuses negative gross income, and a spouse's AGI without a combined
    /// return.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        let e = self.e;
        if let Some(n) = e.non_wage_income
            && n.0 < 0
        {
            return Err(Error::Invalid(format!(
                "kentuckytaxcalc: gross income of {n} is negative"
            )));
        }
        if e.spouse_agi.is_some() && e.combined != Some(true) {
            return Err(Error::Invalid(
                "kentuckytaxcalc: a spouse's AGI is read only for a combined return".to_string(),
            ));
        }
        Ok(e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// KRS 141.305(1)(a): non-wage gross income above it requires payments.
    pub(crate) income_test: USCents,
    /// KRS 141.305(2) and Form 2210-K line 5: tax after withholding of it or
    /// less requires none.
    pub(crate) threshold: USCents,

    /// Last year's Kentucky AGI above which the prior-year test is 110%, and
    /// the same for married filing separately.
    pub(crate) prior_year_step: USCents,
    pub(crate) prior_year_step_separate: USCents,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// The day after which a requirement that first arises lets a return
    /// filed and paid by `file_and_pay_by`, moved for a weekend, take the
    /// place of the January voucher.
    pub(crate) late_start: Date,
    pub(crate) file_and_pay_by: Date,

    pub(crate) stop: Date,
    pub(crate) period_ends: Vec<Date>,
    pub(crate) rates: RateSeries,
}

/// Form 2210-K line 9, 25% of the required annual payment for each date, and
/// line 10, withholding spread 25% to each.
fn quarters() -> [Rate; 4] {
    [percent(25), percent(25), percent(25), percent(25)]
}

/// Form 2210-K Part III.
const ANNUALISED_PERCENTAGES: [Rate; 4] = [Rate(2250), Rate(4500), Rate(6750), Rate(9000)];

/// Returns Kentucky's estimated tax rules for a tax year, or
/// [`estimated::Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(KentuckyEstimated { y }))
}

struct KentuckyEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,
    installments: Vec<USCents>,
    excused: bool,
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

impl KentuckyEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "kentuckytaxcalc: the facts carry {}, not kentuckytaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "kentuckytaxcalc: these are Kentucky's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "kentuckytaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "a farmer or fisher with two-thirds of gross income from farming or fishing pays 66 2/3% of the tax by January 15 or files and pays by March 1, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let Some(&end) = self.y.period_ends.get(i) else {
                    return Err(Error::Invalid(format!(
                        "kentuckytaxcalc: Form 2210-K Part III has no period {}",
                        i + 1
                    )));
                };
                if p.end() != end {
                    return Err(Error::Invalid(format!(
                        "kentuckytaxcalc: Form 2210-K Part III period {} ends on {}, not {}",
                        i + 1,
                        end,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's documents and KRS 141.305 part ways on a rule
    /// a Kentucky computation uses, each marked with whether it changes
    /// anything for `f`. They are found in research/estimated-payments/ky.md,
    /// items 1 and 10 and its verification. The rate is a penalty rule,
    /// reported with the penalty.
    fn divergences(&self, f: &Facts) -> Result<Vec<Divergence>, Error> {
        let y = &self.y;
        let tax_opt = f.this_year().tax();
        let withheld = f.withheld_total();
        let filed = f.return_filed();
        let file_by = y.calendar.next_business_day(y.file_and_pay_by);
        let by_february = !filed.is_zero() && !filed.after(file_by);
        let mut out = Vec::new();
        for b in [
            working::new_divergence("Exactly $500")
                .form("Form 2210-K stops at $500 or less of tax. That tax is after withholding and refundable credits. Kentucky's voucher instructions say payments are needed from $500. The 2210-K instructions say under $500.",
                    "Form 2210-K (2025), 42A740-S1 (10-25), line 5 and its instructions; 42A740-S4 (9-25)")
                .statute("No estimated tax is needed if you can reasonably expect $500 or less of tax.",
                    "KRS 141.305(2)")
                .uses(Reading::Form)
                .affects(tax_opt.is_some_and(|tax| tax - withheld == y.threshold))
                .clone(),
            working::new_divergence("A return filed and paid by February 1")
                .form(format!("Filing and paying in full by {} replaces the January payment. That holds only if you first needed to pay after {}.",
                    file_by.long(), y.late_start.long()),
                    "42A740-S4 (9-25), the note to the 2026 due dates")
                .statute(format!("Kentucky uses federal timing. Anyone who files and pays in full by January 31 can skip the January payment. For {} returns, that date is {}.",
                    y.year, file_by.long()),
                    "KRS 141.305(3); 26 U.S.C. § 6654(h)")
                .uses(Reading::Form)
                .affects(by_february && !f.requirement_first_met().after(y.late_start))
                .clone(),
            working::new_divergence("When withholding counts as paid")
                .form("Withholding counts as paid 25% on each due date. You can't count it on the dates it was withheld.",
                    "Form 2210-K (2025), 42A740-S1 (10-25), line 10")
                .statute("Kentucky adopts the federal method. It lets you count withholding on the dates it was actually withheld.",
                    "KRS 141.305(3); 26 U.S.C. § 6654(g)(1)")
                .uses(Reading::Form)
                .affects(withheld.0 > 0)
                .clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    /// The 2026 penalty rate. It affects a penalty charged on any day of
    /// 2026.
    fn rate_divergence(&self, charged: bool) -> Result<Divergence, Error> {
        Ok(working::new_divergence("The penalty rate for 2026")
            .form("Every 2026 day is charged at 9%, the 7% tax interest rate plus 2%. The form was revised in October 2025, before a 2026 act amended the rate law. That act took effect on July 15, 2026.",
                "Form 2210-K (2025), 42A740-S1 (10-25), line 21")
            .statute("The rate is the tax interest rate, set at 7% for 2026. The added 2% is for taxes not paid on time. The law keeps the estimated tax penalty out of it.",
                "KRS 141.305(3)(d); KRS 131.183(1)(a), (1)(b)1, (2)(a), as amended by 2026 Ky. Acts ch. 161, sec. 2, effective July 15, 2026; DOR, Tax Interest Rate Update for 01-01-26")
            .uses(Reading::Form)
            .affects(charged)
            .build()?)
    }

    /// A date as a person reads it: "Aug 31" in the tax year, and "Jan 15,
    /// 2027" outside it.
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
        let mut a = Assessment::default();
        let divergences = self.divergences(f)?;
        a.notes.diverges(&divergences);

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let has_prior = f.prior_year().is_some();
        let prior = f.prior_year().unwrap_or_default();
        let last = y.year - 1;
        let separate = f.filing_status() == filing::Status::MarriedFilingSeparately;

        // 90% of this year's tax.
        let this_year = if tax_known {
            estimated::new_target_option(Basis::ThisYearTax, percent(90), percent(90).of(tax))
                .build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, percent(90), USCents(0))
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        if tax_known {
            a.notes.rule(
                "KRS 141.305(3); 26 U.S.C. § 6654(d)(1)(B)(i); 42A740-S4 (9-25)",
                format!(
                    "90% of this year's Kentucky tax of {tax} is {}",
                    this_year.amount()
                ),
            );
        }

        // Last year's tax: 100%, or 110% above the step.
        let prior_tax = if has_prior {
            prior.figures().and_then(|pf| pf.tax()).unwrap_or_default()
        } else {
            USCents(0)
        };
        let unavailable = |why: String| {
            estimated::new_target_option(Basis::PriorYearTax, percent(100), USCents(0))
                .unavailable(why)
                .build()
        };
        // Questions whose answer can change the target, asked only when no
        // exception sets it.
        let mut target_asks: Vec<(PositionKind, String, String)> = Vec::new();
        let prior_opt = if !has_prior {
            target_asks.push((
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Kentucky return, and what tax did it show?"),
                "With it, last year's tax could set your target instead of 90% of this year's."
                    .to_string(),
            ));
            unavailable(format!("Needs your {last} Kentucky return"))?
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Kentucky return"))?
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} Kentucky return covered {} months, and this option needs all 12",
                prior.months()
            ))?
        } else {
            self.prior_year_option(&extras, &prior, prior_tax, separate, &mut a.notes)
                .build()?
        };

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        if !tax_known {
            target_asks.push((
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Kentucky tax to be?", y.year),
                format!(
                    "90% of it may lower your target. Tax of {} or less after withholding needs no payments.",
                    y.threshold.whole()
                ),
            ));
        }

        // KRS 141.305(1): payments are needed only when an income test is
        // met.
        let mut income_met = true;
        let non_wage_opt = extras.non_wage_income();
        let non_wage = non_wage_opt.unwrap_or_default();
        let (filing_met, filing_answered) = extras.filing_threshold_met();
        if non_wage_opt.is_some() && non_wage > y.income_test {
            a.notes.rule(
                "KRS 141.305(1)(a)",
                format!(
                    "{non_wage} of gross income other than wages with Kentucky withholding is more than {}, so the estimated tax rules apply",
                    y.income_test
                ),
            );
        } else if filing_answered && filing_met {
            a.notes.rule(
                "KRS 141.305(1)(b)",
                "Kentucky AGI is expected to reach the amount for which KRS 141.180 requires a return, so the estimated tax rules apply",
            );
        } else if non_wage_opt.is_some() && filing_answered {
            income_met = false;
        } else {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!(
                    "Will you need a Kentucky return or have over {} not withheld for Kentucky?",
                    y.income_test.whole()
                ),
                "Payments are needed only if one is true, and the figures assume one is for now.",
            );
        }

        // Form 2210-K: no liability last year, as a U.S. citizen or resident
        // alien, on a full-year return.
        let no_liability = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax.0 == 0) || prior.not_required_to_file());
        let (resident, resident_answered) = extras.prior_year_us_resident();
        if no_liability && !resident_answered {
            a.notes.ask(
                PositionKind::JurisdictionFact,
                format!("Were you a U.S. citizen or resident for all of {last}?"),
                format!(
                    "If so, no {last} Kentucky tax means no estimated payments or penalty for {}.",
                    y.year
                ),
            );
        }

        let label = |o: &TargetOption| format!("{}, {}", o.label(), o.amount().whole());
        if !income_met {
            let ex = estimated::new_exception("Income too low for Kentucky estimated payments")
                .build()?;
            a.exempt = true;
            rb.required(
                false,
                format!(
                    "Your {} of income without Kentucky withholding is {} or less. Your Kentucky AGI won't reach the level that requires a return. Kentucky therefore needs no estimated payments for {}.",
                    non_wage.whole(),
                    y.income_test.whole(),
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "KRS 141.305(1)",
                format!(
                    "{non_wage} of non-wage gross income is not more than {} and AGI does not reach the KRS 141.180 filing amount, so no estimated payments are required",
                    y.income_test
                ),
            );
        } else if no_liability && resident_answered && resident {
            let ex = estimated::new_exception("No Kentucky tax last year").build()?;
            a.exempt = true;
            rb.required(
                false,
                format!(
                    "You had no Kentucky tax for {last}, a full year as a U.S. citizen or resident. No estimated payments are needed for {}, and no penalty can arise.",
                    y.year
                ),
            )
            .exempt(ex);
            a.notes.rule(
                "Form 2210-K (2025), 42A740-S1 (10-25), exceptions; 42A740-S4 (9-25)",
                format!(
                    "the {last} return showed no liability for a full 12 months, so there is no penalty for {}",
                    y.year
                ),
            );
        } else if tax_known && tax - withheld <= y.threshold {
            let ex =
                estimated::exception_at_most("Tax less withholding", tax - withheld, y.threshold)?;
            let why = if tax < withheld {
                format!(
                    "Your Kentucky tax of {} less {} withheld {}. No estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    estimated::leaves(tax - withheld)
                )
            } else if tax == withheld {
                format!(
                    "Your withholding of {} covers your Kentucky tax of {}. No estimated payments are needed.",
                    withheld.whole(),
                    tax.whole()
                )
            } else {
                format!(
                    "Your Kentucky tax of {} less {} of withholding leaves {}. That is {} or less, so no estimated payments are needed.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.threshold.whole()
                )
            };
            a.exempt = true;
            rb.required(false, why).exempt(ex);
            a.notes.rule(
                "KRS 141.305(2); Form 2210-K (2025), 42A740-S1 (10-25), line 5",
                format!(
                    "{tax} of tax less {withheld} withheld is {}, {} or less, so there is no penalty",
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "There's no target until you have your {} Kentucky tax or your {last} return.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your target is {}. Your withholding of {} already reaches it. No estimated payments are needed whatever your {} tax turns out to be.",
                    label(&prior_opt),
                    withheld.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {}. It avoids a penalty whatever your {} tax turns out to be. After {} of withholding, you need {} of estimated payments.",
                    label(&prior_opt),
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
                    label(&prior_opt)
                ),
            )
            .choose(chosen);
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Your Kentucky withholding of {} already reaches your target of {}. No estimated payments are needed.",
                    withheld.whole(),
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
            let lead = if (tax - withheld).whole() == y.threshold.whole() {
                format!(
                    "Your Kentucky tax less withholding is more than {}.",
                    y.threshold.whole()
                )
            } else {
                format!(
                    "Your Kentucky tax less withholding leaves {}, which is more than {}.",
                    (tax - withheld).whole(),
                    y.threshold.whole()
                )
            };
            rb.required(
                true,
                format!(
                    "{lead} Your target is {}. You need {} of estimated payments.",
                    label(c),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        if !a.exempt {
            for (k, q, e) in target_asks {
                a.notes.ask(k, q, e);
            }
        }
        a.requirement = rb.build()?;

        // The dates. KY-TAM-19-02 moves one on a weekend or holiday to the
        // next working day.
        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        for i in 0..a.dues.len() {
            if a.dues[i] != y.dues[i] {
                a.notes.rule(
                    "KY-TAM-19-02",
                    format!(
                        "the installment due {} falls on a weekend or holiday, so it is due {}",
                        y.dues[i].long(),
                        a.dues[i].long()
                    ),
                );
            }
        }
        let cumulative = estimated::cumulative_shares(a.requirement.target(), &quarters())?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "KRS 141.305(3); 42A740-S4 (9-25); Form 2210-K (2025), 42A740-S1 (10-25), line 9",
            format!(
                "25% of {}, {}, is due on each of {}, {}, {} and {}",
                a.requirement.target(),
                a.installments[0],
                a.dues[0].long(),
                a.dues[1].long(),
                a.dues[2].long(),
                a.dues[3].long()
            ),
        );

        if f.elections().annualised() && !a.exempt {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &ANNUALISED_PERCENTAGES, &a.installments)?;
            a.notes.rule(
                "KRS 141.305(3); 26 U.S.C. § 6654(d)(2); Form 2210-K (2025), 42A740-S1 (10-25), Part III",
                format!(
                    "under the annualised method, used for every date, the installments are {}, {}, {} and {}: 22.5%, 45%, 67.5% and 90% of the annualised tax, \
                     less earlier installments, and never more than the regular 25% plus any shortfall carried forward",
                    ann[0], ann[1], ann[2], ann[3]
                ),
            );
            a.installments = ann;
        }

        if !a.exempt {
            self.january(f, &mut a);
        }
        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }
        Ok(a)
    }

    /// 100% or 110% of last year's tax on a filed full-year return.
    fn prior_year_option(
        &self,
        extras: &EstimatedFacts,
        prior: &PriorReturn,
        prior_tax: USCents,
        separate: bool,
        notes: &mut Notes,
    ) -> TargetOptionBuilder {
        let y = &self.y;
        let last = y.year - 1;
        let pf = prior.figures().unwrap_or_default();
        let agi_opt = pf.state_agi();
        let mut known = agi_opt.is_some();
        let mut agi = agi_opt.unwrap_or_default();
        let step = if separate {
            y.prior_year_step_separate
        } else {
            y.prior_year_step
        };
        let mut whose = format!("{last} Kentucky AGI");
        let mut spouse_missing = false;
        if separate {
            let (combined, answered) = extras.combined_return();
            if !answered {
                notes.ask(
                    PositionKind::JurisdictionFact,
                    "Do you and your spouse file separately on one combined Kentucky return?",
                    "Until then, only your own income counts for the 110% test. A combined return adds your spouse's.",
                );
            } else if combined {
                match extras.spouse_prior_year_agi() {
                    None => {
                        (known, spouse_missing) = (false, true);
                    }
                    Some(spouse) => {
                        agi += spouse;
                        whose = format!("the couple's combined {last} Kentucky AGI");
                        if known && agi > step && agi <= y.prior_year_step {
                            notes.ask(
                                PositionKind::UnsettledRule,
                                "",
                                format!(
                                    "Kentucky doesn't say which limit tests your combined {last} AGI of {}. The figures use {} for married filing separately, not {}.",
                                    agi.whole(),
                                    step.whole(),
                                    y.prior_year_step.whole()
                                ),
                            );
                        }
                    }
                }
            }
        }
        let mut rate = percent(100);
        if !known {
            rate = percent(110);
            let (question, effect) = if spouse_missing {
                (
                    format!("What was your spouse's {last} Kentucky AGI?"),
                    format!(
                        "Until then, last year's option is figured at 110%. Your spouse's AGI is added to yours for the test at {}.",
                        step.whole()
                    ),
                )
            } else {
                (
                    format!("What was your {last} Kentucky AGI?"),
                    format!(
                        "Until then, last year's option is figured at 110%. That is the rate when your AGI is more than {}.",
                        step.whole()
                    ),
                )
            };
            notes.ask(PositionKind::AdjustedGrossIncome, question, effect);
        } else if agi > step {
            rate = percent(110);
        }
        let amount = rate.of(prior_tax);
        let why = if !known {
            format!("{whose} was not given")
        } else if rate == percent(110) {
            format!("{whose} of {agi} is more than {step}")
        } else {
            format!("{whose} of {agi} is not more than {step}")
        };
        notes.rule(
            "KRS 141.305(3); 26 U.S.C. § 6654(d)(1)(B)(ii), (C); 42A740-S4 (9-25)",
            format!(
                "{why}, so the prior-year test is {rate} of the {prior_tax} tax on the {last} return, or {amount}"
            ),
        );
        estimated::new_target_option(Basis::PriorYearTax, rate, amount)
    }

    /// The voucher instructions' note: a requirement that first arises after
    /// September 1 needs no January voucher if the return is filed and paid
    /// by February 1, 2027.
    fn january(&self, f: &Facts, a: &mut Assessment) {
        let y = &self.y;
        let filed = f.return_filed();
        let file_by = y.calendar.next_business_day(y.file_and_pay_by);
        if filed.is_zero() || filed.after(file_by) {
            return;
        }
        let first_met = f.requirement_first_met();
        if first_met.is_zero() {
            a.notes.ask(
                PositionKind::RequirementFirstMet,
                format!(
                    "When did you first expect to need estimated payments for {}?",
                    y.year
                ),
                format!(
                    "Until then, the January payment is still due. If after {}, a return filed and paid by {} replaces it.",
                    self.short_date(y.late_start),
                    self.short_date(file_by)
                ),
            );
        } else if first_met.after(y.late_start) {
            a.excused = true;
            a.notes.rule(
                "42A740-S4 (9-25), the note to the 2026 due dates",
                format!(
                    "the requirement first arose on {}, after {}, and the return was filed and paid on {}, by {}, so the January installment is excused",
                    first_met.long(),
                    y.late_start.long(),
                    filed.long(),
                    file_by.long()
                ),
            );
        } else {
            a.notes.rule(
                "42A740-S4 (9-25), the note to the 2026 due dates",
                format!(
                    "the return was filed and paid on {}, by {}, but the requirement arose on {}, not after {}, so the January installment is still due; \
                     the statute's reading is in Divergences",
                    filed.long(),
                    file_by.long(),
                    first_met.long(),
                    y.late_start.long()
                ),
            );
        }
    }

    /// Applies the credits to the installments, with withholding spread 25%
    /// to each date as Form 2210-K line 10 does.
    fn ledger(&self, f: &Facts, a: &Assessment) -> Result<(Vec<LedgerEntry>, Notes), Error> {
        let credits = estimated::credits(f, &a.dues, &quarters())?;
        let mut required = a.installments.clone();
        if a.excused {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        notes.rule(
            "Form 2210-K (2025), 42A740-S1 (10-25), line 10",
            format!(
                "the {} withheld is treated as paid 25% on each due date, whenever it was actually withheld",
                f.withheld_total()
            ),
        );
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "Form 2210-K (2025), 42A740-S1 (10-25), line 10",
                "Form 2210-K offers no election to credit withholding on the dates withheld, so it is spread evenly; the statute's reading is in Divergences",
            );
        }
        Ok((ledger, notes))
    }
}

/// The due dates with what was needed and credited by each.
fn installments(a: &Assessment, ledger: &[LedgerEntry]) -> Result<Vec<Installment>, Error> {
    let mut out = Vec::with_capacity(ledger.len());
    let mut needed = USCents(0);
    for (i, l) in ledger.iter().enumerate() {
        needed += a.installments[i];
        let mut b = estimated::new_installment(i as i64 + 1, l.due());
        b.needed_by(needed)
            .credited_by(l.credited_by())
            .shortfall(l.shortfall());
        if a.excused && i == 3 {
            b.excused();
        }
        out.push(b.build()?);
    }
    Ok(out)
}

/// Decides a waiver asked for. KRS 141.305(3)(e) sends every waiver through
/// KRS 131.175, and 103 KAR 1:040 Section 1 lists the reasonable causes: a
/// catastrophic event that destroys or withholds records is one, and
/// retirement and disability are not.
fn kentucky_waiver(f: &Facts) -> Option<WaiverOutcome> {
    match f.positions().waiver()? {
        WaiverKind::None => None,
        WaiverKind::Casualty => Some(estimated::waiver_eligible(
            "Records a casualty or disaster destroyed or kept from you are reasonable cause. Kentucky's Department of Revenue decides.",
        )),
        WaiverKind::RetiredOrDisabled => Some(estimated::waiver_refused(
            "Kentucky waives only for reasonable cause, not retirement or disability alone. A serious illness or a disaster that destroyed your records can qualify.",
        )),
    }
}

impl EstimatedTax for KentuckyEstimated {
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
                    "with no {} Kentucky return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let out = installments(&a, &ledger)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "kentuckytaxcalc: a penalty needs the date to figure it to".to_string(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Kentucky return to measure against, there is no target to figure a penalty from",
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
        let waiver_answer = f.positions().waiver();
        let waiver = waiver_answer.unwrap_or_default();
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &[])
                .build()?;
            let rate = self.rate_divergence(false)?;
            let mut rn = Notes::default();
            rn.diverges(&[rate]);
            return Ok(working::new_result(p).with(&a.notes).with(&rn).build()?);
        }

        let (ledger, mut notes) = self.ledger(f, &a)?;
        let schedule = installments(&a, &ledger)?;
        let (mut p, unrated) =
            estimated::simple_penalty(&ledger, stop, &y.rates, waiver, &a.requirement, &schedule)?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "KRS 141.305(3); 26 U.S.C. § 6654(d)(1)(B); 42A740-S4 (9-25)",
                format!(
                    "this year's Kentucky tax was not given, so the penalty is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "KRS 141.305(3); 26 U.S.C. § 6654(b)(2); Form 2210-K (2025), 42A740-S1 (10-25)",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "KRS 141.305(3)(d); Form 2210-K (2025), 42A740-S1 (10-25), lines 17 to 21",
            format!(
                "each shortfall is charged simple interest by the day at the rate the form prints for its calendar year, from its due date until paid or {}, \
                 with each payment applied to the oldest shortfall first; the penalty to {} is {}",
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        let charged = p.lines().iter().any(|l| !l.parts().is_empty());
        let rate = self.rate_divergence(charged)?;
        notes.diverges(&[rate]);
        if charged {
            let first = y
                .rates
                .periods()
                .first()
                .map(|r| r.rate())
                .unwrap_or_default();
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The form charges {first} on your {} shortfalls, the 7% tax interest rate plus 2%. The figures follow the form, though the law may mean 7%, about 7/9 the penalty.",
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
                    "The Kentucky rate for the days after {} isn't published yet.",
                    unrated.add_days(-1).long()
                ),
            );
        }
        if p.total().0 > 0 {
            match waiver_answer {
                None => {
                    notes.ask(
                        PositionKind::PenaltyWaiver,
                        "Are you asking Kentucky to waive your penalty for reasonable cause?",
                        "It's before any waiver, which a disaster that destroyed your records can earn. So can the death or serious illness of you or your immediate family.",
                    );
                }
                Some(WaiverKind::RetiredOrDisabled) => {
                    notes.rule(
                        "KRS 141.305(3)(e); KRS 131.175; 103 KAR 1:040 Section 1",
                        "Kentucky has no waiver for retirement or disability as such; a waiver needs reasonable cause under 103 KAR 1:040, and the penalty shown is before any waiver",
                    );
                }
                Some(WaiverKind::Casualty) => {
                    notes.rule(
                        "KRS 141.305(3)(e); KRS 131.175; 103 KAR 1:040 Section 1(5)",
                        "a casualty is reasonable cause only as 103 KAR 1:040 describes it, such as a catastrophic event that destroys or withholds records; the department decides, and the penalty shown is before any waiver",
                    );
                }
                Some(WaiverKind::None) => {}
            }
            if let Some(o) = kentucky_waiver(f) {
                p = p.to_builder().waiver_outcome(o).build()?;
            }
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        KENTUCKY_EXPLANATION.clone()
    }
}

/// Written by hand and checked against research/estimated-payments/ky.md.
static KENTUCKY_EXPLANATION: LazyLock<Explanation> = LazyLock::new(|| {
    estimated::new_explanation(
        "Kentucky follows the federal rules closely, with a lower threshold. You need estimated payments once tax after withholding is more than $500. The penalty is interest on each quarter's shortfall.")
        .paragraph("Payments are needed if you expect over $500 of tax after withholding. Refundable credits count with withholding. You must also expect over $5,000 of income without Kentucky withholding. Expecting income high enough to need a Kentucky return counts instead. Your target is 90% of this year's Kentucky tax or 100% of last year's, if less. Withholding and payments that reach it avoid the penalty. Last year's figure needs a full-year return. It's 110% if last year's Kentucky AGI was over $150,000. That limit is $75,000 for married filing separately.")
        .paragraph("A quarter is due April 15, June 15, September 15 and the next January 15. A date on a weekend or holiday shifts to the next working day. Withholding counts as paid evenly on the four dates, whenever it was taken.")
        .paragraph("There's no penalty if this year's tax after withholding is $500 or less. There's none either if you had no Kentucky tax last year on a full-year return. As federally, each shortfall is charged for each day it stays unpaid. Charges stop at the next April 15. Each payment goes to the oldest shortfall first. Form 2210-K's annualized method can lower early payments for uneven income. Once used, it must be used for every date.")
        .paragraph("The rate is fixed for each calendar year. The department's form charges 9% for 2026. Its announced 2026 tax interest rate is 7%, and the law can be read either way. The penalty can be waived only for reasonable cause. A serious illness or a disaster that destroys your records can be one. Retirement or disability alone is not.")
        .versus_federal("Kentucky differs in its $500 threshold. Its income tests decide whether payments are needed at all. The 110% test is on Kentucky AGI, adding a combined return's incomes. The rate is fixed for the year, 9% on the form for 2026. Filing by Feb 1 excuses January only if you first had to pay after September 1. You can't elect to count withholding when withheld. Waivers need reasonable cause.")
        .source("KRS 141.305; KRS 131.183; KRS 131.175; 103 KAR 1:040")
        .source("Instructions for Filing Estimated Tax Vouchers 2026, 42A740-S4 (9-25), and Form 740-ES 2026, 42A740ES (10/25)")
        .source("Form 2210-K, Underpayment of Estimated Tax by Individuals, 2025, 42A740-S1 (10-25), and its instructions")
        .source("KY-TAM-19-02, Requirement to Pay Estimated Tax Installments")
        .source("Department of Revenue, Tax Interest Rate Update for 01-01-26")
        .build()
        .unwrap_or_default()
});
