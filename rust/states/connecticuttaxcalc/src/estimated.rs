//! Connecticut's estimated income tax rules, Conn. Gen. Stat.
//! §§ 12-701(a)(12) and 12-722, as the shared [`EstimatedTax`] interface.
//!
//! The arithmetic is federal in shape: 90% of this year's tax or 100% of
//! last year's, four equal quarters, nothing under $1,000, the annualised
//! method at 22.5%, 45%, 67.5% and 90%. Connecticut reenacts those rules in
//! its own statute rather than adopting 26 U.S.C. § 6654, and nothing here
//! borrows from the federal package. What differs is the charge: 1% for each
//! month or part of a month that an underpayment stays unpaid.
//!
//! The figure follows the department's forms, CT-1040ES (Rev. 01/26) for
//! 2026 and CT-2210 (Rev. 12/25) for 2025, and the result's Divergences say
//! where § 12-722 reads otherwise. No act changed § 12-722 or
//! § 12-701(a)(12) for 2026 (research/estimated-payments/ct.md,
//! Verification, 2026 changes), so no form here predates the law it
//! describes.

use std::any::Any;

use estimated::{
    Basis, Calendar, Date, Error, EstimatedTax, Explanation, Facts, Installment, Payment, Penalty,
    PriorReturn, Requirement, TargetOptionBuilder, WaiverKind, WaiverOutcome,
};
use money::{Rate, USCents};
use working::{Divergence, Notes, PositionKind, Reading};

/// jurisdiction is Connecticut's postal code.
const JURISDICTION: estimated::Jurisdiction = estimated::CONNECTICUT;

/// The facts Connecticut's rules read that no other jurisdiction does.
/// Carry them in [`Facts::state`].
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct EstimatedFacts {
    pe_tax_credit: Option<USCents>,
    prior_source_income: bool,
    prior_source_answered: bool,
}

impl EstimatedFacts {
    /// This year's Pass-Through Entity Tax Credit, Schedule CT-PE line 1,
    /// which CT-2210 Part 2 line 3a takes, or `None` if the caller did not
    /// give it. The forms count it like withholding: against the $1,000
    /// test, and a quarter of it by each due date.
    /// [`estimated::Figures::tax`] is the tax before it.
    pub fn pe_tax_credit(&self) -> Option<USCents> {
        self.pe_tax_credit
    }

    /// Whether a filer who was not a full-year Connecticut resident last
    /// year had income from Connecticut sources then, or `None` if the
    /// caller did not say. The no-liability exception, § 12-722(j)(2), is
    /// open to such a filer only if they did (CT-2210 Box G); without it
    /// they must pay 90% of this year's tax (Box E).
    pub fn prior_year_connecticut_source_income(&self) -> Option<bool> {
        self.prior_source_answered
            .then_some(self.prior_source_income)
    }
}

/// Builds [`EstimatedFacts`].
#[derive(Clone, Debug, Default)]
pub struct EstimatedFactsBuilder {
    e: EstimatedFacts,
}

/// Starts an [`EstimatedFacts`] with nothing given.
pub fn new_estimated_facts() -> EstimatedFactsBuilder {
    EstimatedFactsBuilder::default()
}

impl EstimatedFactsBuilder {
    /// Gives this year's PE Tax Credit.
    pub fn pe_tax_credit(&mut self, a: USCents) -> &mut Self {
        self.e.pe_tax_credit = Some(a);
        self
    }

    /// Says whether a part-year or nonresident filer had Connecticut income
    /// last year.
    pub fn prior_year_connecticut_source_income(&mut self, had: bool) -> &mut Self {
        (self.e.prior_source_income, self.e.prior_source_answered) = (had, true);
        self
    }

    /// Refuses a negative credit.
    pub fn build(&self) -> Result<EstimatedFacts, Error> {
        let v = self.e.pe_tax_credit.unwrap_or_default();
        if v.0 < 0 {
            return Err(Error::Invalid(format!(
                "connecticuttaxcalc: a PE Tax Credit of {v} is negative"
            )));
        }
        Ok(self.e)
    }
}

/// One tax year's published figures.
#[derive(Clone, Debug)]
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// § 12-722(j)(1): no interest when tax less withholding, and on the
    /// forms less the PE Tax Credit, is under it.
    pub(crate) threshold: USCents,

    /// § 12-701(a)(12): 90% of this year's tax, or 100% of the tax on last
    /// year's 12-month return.
    pub(crate) this_year_share: Rate,
    pub(crate) prior_year_share: Rate,

    /// § 12-722(d)(1), 25% each; withholding and the PE Tax Credit are
    /// spread in the same parts.
    pub(crate) shares: Vec<Rate>,

    /// The § 12-722(d)(2)(C) applicable percentages, CT-2210 Schedule A
    /// line 13.
    pub(crate) annualised: Vec<Rate>,

    pub(crate) dues: Vec<Date>,
    pub(crate) calendar: Calendar,

    /// § 12-722(l): a return filed and paid in full by this date takes the
    /// interest off the fourth installment. It is not moved for a weekend;
    /// neither the statute nor the form says it moves.
    pub(crate) file_and_pay_by: Date,

    /// § 12-722(b): the interest runs no later than the fifteenth day of the
    /// fourth month of the next year.
    pub(crate) stop: Date,

    /// The CT-2210 Schedule A period ends for individuals.
    pub(crate) period_ends: Vec<Date>,

    /// § 12-722(a): 1% a month or fraction of a month.
    pub(crate) monthly_rate: Rate,
}

/// Returns Connecticut's estimated tax rules for a tax year, or
/// [`Error::NoRules`] for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&JURISDICTION, year)),
    };
    Ok(Box::new(ConnecticutEstimated { y }))
}

struct ConnecticutEstimated {
    y: EstimatedYear,
}

/// What the three questions share.
#[derive(Clone, Debug, Default)]
struct Assessment {
    requirement: Requirement,
    exempt: bool,
    tax_known: bool,
    dues: Vec<Date>,

    /// Each installment's own amount, before excusing.
    installments: Vec<USCents>,

    /// The fourth installment is excused, for a return filed and paid by
    /// January 31.
    excused: bool,

    /// The PE Tax Credit, zero when not given.
    pe: USCents,

    notes: Notes,
}

impl Assessment {
    /// Each installment's amount, with the fourth set to nothing when filing
    /// early excused it.
    fn required(&self) -> Vec<USCents> {
        let mut out = self.installments.clone();
        if self.excused {
            out[3] = USCents(0);
        }
        out
    }
}

/// Go's `%T` of the extras, as far as it can be told from here.
fn type_name(v: &(dyn Any + Send + Sync)) -> &'static str {
    if v.is::<&str>() || v.is::<String>() {
        "string"
    } else {
        "an unknown type"
    }
}

impl ConnecticutEstimated {
    fn extras(&self, f: &Facts) -> Result<EstimatedFacts, Error> {
        match f.state() {
            None => Ok(EstimatedFacts::default()),
            Some(s) => match s.downcast_ref::<EstimatedFacts>() {
                Some(e) => Ok(*e),
                None => Err(Error::Invalid(format!(
                    "connecticuttaxcalc: the facts carry {}, not connecticuttaxcalc.EstimatedFacts",
                    type_name(s)
                ))),
            },
        }
    }

    fn check(&self, f: &Facts) -> Result<(), Error> {
        if f.jurisdiction() != JURISDICTION {
            return Err(Error::Invalid(format!(
                "connecticuttaxcalc: these are Connecticut's rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(Error::Invalid(format!(
                "connecticuttaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(Error::with_detail(
                Error::Unsupported,
                "Conn. Gen. Stat. § 12-722(m) gives farmers and fishermen one installment, due January 15, which is not built",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(Error::Invalid(format!(
                        "connecticuttaxcalc: CT-2210 Schedule A period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    /// Where the department's forms and § 12-722 part ways on a rule a
    /// Connecticut computation uses. They come from
    /// research/estimated-payments/ct.md, Verification, and from reading
    /// CT-2210's instructions against § 12-722(l). The figures follow the
    /// forms. Each affects flag says whether the statute's reading would
    /// change these facts' answer.
    fn divergences(
        &self,
        pe_test: bool,
        pe_payment: bool,
        early: bool,
    ) -> Result<Vec<Divergence>, Error> {
        let mut out = Vec::new();
        for b in [
            working::new_divergence("The PE Tax Credit in the $1,000 test")
                .form("The form subtracts withholding and the PE Tax Credit from your tax. It charges interest only if $1,000 or more is left.",
                    "CT-2210 (Rev. 12/25), My Taxes Are Underpaid and Part 2 line 4; CT-1040ES (Rev. 01/26), Who Is Required to Make Estimated Payments and worksheet line 17")
                .statute("The law tests your tax reduced by the tax withheld. It doesn't name the PE Tax Credit. The law granting the credit doesn't make it withholding.",
                    "Conn. Gen. Stat. § 12-722(j)(1); § 12-699(f)(1), as read in research/estimated-payments/ct.md")
                .uses(Reading::Form)
                .affects(pe_test)
                .clone(),
            working::new_divergence("The PE Tax Credit as a payment by each due date")
                .form("The form counts a quarter of the PE Tax Credit as paid by each due date. It does this like withholding. That holds for the credit even if you show when your tax was actually withheld.",
                    "CT-2210 (Rev. 12/25), Part 3 lines 12 to 13")
                .statute("The law treats only tax withheld as a payment of estimated tax. It counts it in equal parts on the due dates, or on the dates withheld. No law has been found that treats the PE Tax Credit as one.",
                    "Conn. Gen. Stat. § 12-722(k); §§ 12-699, 12-699a, as read in research/estimated-payments/ct.md")
                .uses(Reading::Form)
                .affects(pe_payment)
                .clone(),
            working::new_divergence("A return filed and paid by Jan 31, 2027")
                .form("Say you file and pay in full by Jan 31, 2027. The form then charges no interest after Jan 15, 2027 on any installment. That includes an earlier one still unpaid then.",
                    "CT-2210 (Rev. 12/25), general instructions, \"Do not complete Schedule B, Worksheet D\"")
                .statute("The law excuses only the fourth installment's underpayment. An earlier installment's underpayment keeps bearing interest until it's paid.",
                    "Conn. Gen. Stat. § 12-722(b), (l)")
                .uses(Reading::Form)
                .affects(early)
                .clone(),
        ] {
            out.push(b.build()?);
        }
        Ok(out)
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, Error> {
        self.check(f)?;
        let extras = self.extras(f)?;
        let y = &self.y;
        let mut a = Assessment::default();

        let tax_opt = f.this_year().tax();
        let tax_known = tax_opt.is_some();
        let tax = tax_opt.unwrap_or_default();
        a.tax_known = tax_known;
        let withheld = f.withheld_total();
        let pe_opt = extras.pe_tax_credit();
        let pe_known = pe_opt.is_some();
        let pe = pe_opt.unwrap_or_default();
        a.pe = pe;
        let credited = withheld + pe;
        let prior_ret = f.prior_year();
        let has_prior = prior_ret.is_some();
        let prior = prior_ret.unwrap_or_default();
        let last = y.year - 1;
        let mut prior_tax = USCents(0);
        if has_prior && let Some(pf) = prior.figures() {
            prior_tax = pf.tax().unwrap_or_default();
        }

        // § 12-701(a)(12)(A): 90% of this year's tax.
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
                "Conn. Gen. Stat. § 12-701(a)(12)(A); CT-1040ES (Rev. 01/26), worksheet line 13",
                format!(
                    "{} of this year's Connecticut tax of {} is {}",
                    y.this_year_share,
                    tax,
                    this_year.amount()
                ),
            );
        }

        // § 12-701(a)(12)(B): 100% of last year's tax, for a filed 12-month
        // return. There is no higher percentage at any income.
        let unavailable = |why: String| -> TargetOptionBuilder {
            estimated::new_target_option(Basis::PriorYearTax, y.prior_year_share, USCents(0))
                .unavailable(why)
                .clone()
        };
        let prior_option = if !has_prior {
            unavailable(format!("Needs your {last} Connecticut return"))
        } else if prior.not_required_to_file() && no_source_income(&prior, &extras) {
            unavailable(format!(
                "You had no Connecticut income in {last} and filed no Connecticut return"
            ))
        } else if prior.not_required_to_file() {
            unavailable(format!(
                "You didn't need to file a {last} Connecticut return"
            ))
        } else if !prior.filed() {
            unavailable(format!("You didn't file a {last} Connecticut return"))
        } else if prior.months() != 12 {
            unavailable(format!(
                "Your {last} return covered {} months, not 12",
                prior.months()
            ))
        } else {
            a.notes.rule(
                "Conn. Gen. Stat. § 12-701(a)(12)(B); CT-1040ES (Rev. 01/26), worksheet line 14",
                format!(
                    "{} of the {} tax on the 12-month {last} return is {}; Connecticut has no higher percentage at higher income",
                    y.prior_year_share,
                    prior_tax,
                    y.prior_year_share.of(prior_tax)
                ),
            );
            estimated::new_target_option(
                Basis::PriorYearTax,
                y.prior_year_share,
                y.prior_year_share.of(prior_tax),
            )
        };
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let (mut chosen, mut target) = (Basis::ThisYearTax, this_year.amount());
        if prior_opt.available() && (!tax_known || prior_opt.amount() < target) {
            (chosen, target) = (Basis::PriorYearTax, prior_opt.amount());
        }
        // § 12-722(j)(2): no liability last year, a 12-month year, as a
        // resident or with Connecticut-source income. CT-2210 Boxes F and G
        // for a filer who filed nothing; a filed return showing no tax gives
        // a prior-year target of nothing, which comes to the same.
        let no_liability = has_prior
            && prior.months() == 12
            && (prior.not_required_to_file() || (prior.filed() && prior_tax.0 == 0));
        let (mut exception, mut boxed) = (false, "");
        if no_liability {
            let resident = prior.full_year_resident();
            let source = extras.prior_year_connecticut_source_income();
            if resident.is_none() && !prior.not_required_to_file() {
                // A filed return showing no tax already gives a target of
                // nothing.
            } else if resident.is_none() {
                a.notes.ask(
                    PositionKind::Residency,
                    format!(
                        "Did you live in Connecticut all of {last}, or have Connecticut income that year?"
                    ),
                    format!(
                        "If either is true, no Connecticut tax for {last} means no interest for {}.",
                        y.year
                    ),
                );
            } else if resident == Some(true) {
                (exception, boxed) = (true, "Box F");
            } else if source.is_none() && !prior.not_required_to_file() {
            } else if source.is_none() {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!("Did you have any Connecticut income in {last}?"),
                    format!(
                        "If you did, having no Connecticut tax for {last} means no interest for {}.",
                        y.year
                    ),
                );
            } else if source == Some(true) {
                (exception, boxed) = (true, "Box G");
            } else if prior.not_required_to_file() {
                a.notes.rule(
                    "Conn. Gen. Stat. § 12-722(j)(2); CT-2210 (Rev. 12/25), Part 1, Box E",
                    format!(
                        "a {last} nonresident or part-year resident with no Connecticut-source income gets no no-liability exception, and must pay {} of this year's tax",
                        y.this_year_share
                    ),
                );
            }
        }

        let (mut credit_words, mut exception_what) = ("withholding", "Tax less withholding");
        if pe.0 > 0 {
            (credit_words, exception_what) = (
                "withholding and PE Tax Credit",
                "Tax less withholding and PE Tax Credit",
            );
        }
        // This year's tax less what is credited against it. With a PE Tax
        // Credit it takes two sentences, to keep each under 80 characters.
        let tax_leaves = || {
            format!(
                "Your Connecticut tax is {}. After {} of {}, it {}.",
                tax.whole(),
                credited.whole(),
                credit_words,
                estimated::leaves(tax - credited)
            )
        };
        if exception {
            a.exempt = true;
            let none = estimated::new_exception("No Connecticut tax last year").build()?;
            rb.required(
                false,
                format!(
                    "You had no Connecticut tax for {last}, a full 12-month year. That means no estimated payments are needed for {}.",
                    y.year
                ),
            )
            .exempt(none);
            a.notes.rule(
                format!("Conn. Gen. Stat. § 12-722(j)(2); CT-2210 (Rev. 12/25), Part 1, {boxed}"),
                format!(
                    "no Connecticut liability for the 12 months of {last}, so there is no interest for {}",
                    y.year
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "With no {last} Connecticut return, your {} tax must be figured to set a target.",
                    y.year
                ),
            )
            .no_target();
        } else if !tax_known && credited >= target {
            rb.required(
                false,
                format!(
                    "Your target is {} of your {last} Connecticut tax, {}. Your {} of {} already reaches it. No estimated payments are needed, whatever your {} tax turns out to be.",
                    prior_opt.percent(),
                    target.whole(),
                    credit_words,
                    credited.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {} of your {last} Connecticut tax, {}. Reaching it keeps you clear of interest, whatever your {} tax is. After {} of {}, {} is left. Pay it in estimated payments, a quarter by each date.",
                    prior_opt.percent(),
                    target.whole(),
                    y.year,
                    credited.whole(),
                    credit_words,
                    (target - credited).whole()
                ),
            )
            .choose(chosen);
        } else if tax - credited < y.threshold {
            a.exempt = true;
            let below = estimated::exception_below(exception_what, tax - credited, y.threshold)?;
            let mut under = format!(
                " That is under {}, so no estimated payments are needed.",
                y.threshold.whole()
            );
            if tax < credited {
                under = " No estimated payments are needed.".into();
            }
            rb.required(false, tax_leaves() + &under).exempt(below);
            a.notes.rule(
                "Conn. Gen. Stat. § 12-722(j)(1); CT-2210 (Rev. 12/25), Part 2 line 4",
                format!(
                    "{} of tax less {} withheld and {} of PE Tax Credit is {}, under {}, so there is no interest",
                    tax,
                    withheld,
                    pe,
                    tax - credited,
                    y.threshold
                ),
            );
            if tax - withheld >= y.threshold {
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "The forms count your {} PE Tax Credit in the {} test. The law may not, and the figures follow the forms.",
                        pe.whole(),
                        y.threshold.whole()
                    ),
                );
            }
        } else if credited >= target {
            rb.required(
                false,
                format!(
                    "You have {} of Connecticut {}. That already reaches the {} needed for the year. No estimated payments are needed.",
                    credited.whole(),
                    credit_words,
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                tax_leaves()
                    + &format!(
                        " That is {} or more, so estimated payments are needed. To reach your {} target, pay {}, a quarter by each date.",
                        y.threshold.whole(),
                        target.whole(),
                        (target - credited).whole()
                    ),
            )
            .choose(chosen);
        }
        a.requirement = rb.build()?;

        // These answers change the target. An exception sets the target, so
        // they are asked only when none applies.
        if !a.exempt && !has_prior {
            a.notes.ask(
                PositionKind::PriorYearReturn,
                format!("Did you file a {last} Connecticut return, and what was its tax?"),
                format!(
                    "With it, {} of last year's tax can be the target if under {} of this year's.",
                    y.prior_year_share, y.this_year_share
                ),
            );
        }
        if !a.exempt && !tax_known {
            let for_now = if a.requirement.target_known() {
                "only last year's tax sets the target"
            } else {
                "there's no target"
            };
            a.notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} Connecticut tax to be?", y.year),
                format!(
                    "For now, {for_now}. The {} exception is open.",
                    y.threshold.whole()
                ),
            );
        }
        if !a.exempt {
            if !pe_known {
                a.notes.ask(
                    PositionKind::JurisdictionFact,
                    format!(
                        "Do you have a {} Pass-Through Entity Tax Credit, and how much is it?",
                        y.year
                    ),
                    "With it, the credit counts like withholding and can lower what you need to pay.",
                );
            } else if pe.0 > 0 {
                a.notes.ask(
                    PositionKind::UnsettledRule,
                    "",
                    format!(
                        "The forms count your {} PE Tax Credit as a payment on each due date. The law may not, and the figures follow the forms.",
                        pe.whole()
                    ),
                );
            }
            for p in f.payments() {
                if p.from_prior_year() {
                    a.notes.ask(
                        PositionKind::UnsettledRule,
                        "",
                        format!(
                            "Neither Connecticut form says when your {} overpayment from {last} counts. The figures follow the law, counting it on the first due date.",
                            p.amount().whole()
                        ),
                    );
                }
            }
        }

        // The due dates. § 12-722(c) fixes them; CT-1040ES moves one that
        // falls on a Saturday, Sunday or legal holiday to the next business
        // day.
        a.dues = estimated::move_dates(&y.calendar, &y.dues);
        for i in 0..a.dues.len() {
            if a.dues[i] != y.dues[i] {
                a.notes.rule(
                    "CT-1040ES (Rev. 01/26), When to File",
                    format!(
                        "the installment due {} falls on a weekend or legal holiday, so it is due {}",
                        y.dues[i].long(),
                        a.dues[i].long()
                    ),
                );
            }
        }
        let cumulative = estimated::cumulative_shares(a.requirement.target(), &y.shares)?;
        a.installments = estimated::increments(&cumulative);
        a.notes.rule(
            "Conn. Gen. Stat. § 12-722(c), (d)(1); CT-1040ES (Rev. 01/26), When to File and worksheet line 18",
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
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &y.annualised, &a.installments)?;
            a.notes.rule(
                "Conn. Gen. Stat. § 12-722(d)(2); CT-2210 (Rev. 12/25), Schedule A",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: {}, {}, {} and {} of the annualised tax, less earlier installments, and never more than the regular quarter plus any reduction not yet recaptured",
                    ann[0],
                    ann[1],
                    ann[2],
                    ann[3],
                    y.annualised[0],
                    y.annualised[1],
                    y.annualised[2],
                    y.annualised[3]
                ),
            );
            a.installments = ann;
        }

        // § 12-722(l): filing and paying in full by January 31 takes the
        // interest off the fourth installment. January 31, 2027 is a Sunday;
        // the statute and CT-1040ES print January 31 and say nothing of
        // moving it, so it is not moved.
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(y.file_and_pay_by) && !a.exempt {
            a.excused = true;
            a.notes.rule(
                "Conn. Gen. Stat. § 12-722(l); CT-1040ES (Rev. 01/26), Interest",
                format!(
                    "the return was filed and paid in full on {}, by {}, so the January installment bears no interest",
                    filed.long(),
                    y.file_and_pay_by.long()
                ),
            );
        }

        if a.exempt {
            a.installments = vec![USCents(0); a.dues.len()];
        }

        // The statute's reading of the PE Tax Credit changes the $1,000 test
        // only when the credit is what brings the balance under it, and the
        // payments by each date whenever there is a credit and a schedule.
        // The January 31 rule matters only to a return filed and paid by
        // then.
        let pe_test = pe.0 > 0
            && tax_known
            && !exception
            && ((tax - credited < y.threshold) != (tax - withheld < y.threshold));
        let divergences = self.divergences(pe_test, pe.0 > 0 && !exception, a.excused)?;
        a.notes.diverges(&divergences);
        Ok(a)
    }

    /// Withholding, the PE Tax Credit and payments, dated as CT-2210 Part 3
    /// counts them: withholding in equal parts on the due dates unless the
    /// filer elects the dates withheld (Box C), and the PE Tax Credit in
    /// equal parts either way.
    fn credits(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Payment>, Notes), Error> {
        let mut credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &self.y.shares)?
        };
        let mut notes = Notes::default();
        if f.elections().withholding_as_withheld() {
            notes.rule(
                "Conn. Gen. Stat. § 12-722(k); CT-2210 (Rev. 12/25), Part 1 Box C and Part 3 line 11b",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "Conn. Gen. Stat. § 12-722(k); CT-2210 (Rev. 12/25), Part 3 lines 11 to 11b",
                format!(
                    "the {} withheld is treated as paid in four equal parts on the due dates, whenever it was actually withheld",
                    f.withheld_total()
                ),
            );
        }
        if a.pe.0 > 0 {
            let cum = estimated::cumulative_shares(a.pe, &self.y.shares)?;
            for (i, part) in estimated::increments(&cum).into_iter().enumerate() {
                if part.0 == 0 {
                    continue;
                }
                credits.push(
                    estimated::new_payment()
                        .on(a.dues[i])
                        .amount(part)
                        .build()?,
                );
            }
            notes.rule(
                "CT-2210 (Rev. 12/25), Part 3 lines 12 to 13",
                format!(
                    "the {} PE Tax Credit is treated as paid in four equal parts on the due dates",
                    a.pe
                ),
            );
        }
        credits.sort_by(|x, y| x.on().compare(y.on()).cmp(&0));
        Ok((credits, notes))
    }

    /// The installments and the notes on how they were credited.
    fn schedule_of(&self, f: &Facts, a: &Assessment) -> Result<(Vec<Installment>, Notes), Error> {
        let (credits, notes) = self.credits(f, a)?;
        let ledger = estimated::apply(&a.dues, &a.required(), &credits)?;
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
        Ok((out, notes))
    }
}

impl EstimatedTax for ConnecticutEstimated {
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
                    "with no {} Connecticut return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (out, notes) = self.schedule_of(f, &a)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    /// Figures the interest as CT-2210 does, one line per column of Part 3.
    ///
    /// A column's shortfall is its Line 16: everything required by its due
    /// date less everything credited by then, so it carries any earlier
    /// installment still unpaid. Schedule B charges it 1% for each month
    /// from the due date to the next one, or to April 15 for the last, a
    /// month running from the 16th to the 15th. A month in which any part is
    /// still unpaid is charged in full, on what was unpaid at its start; a
    /// payment during a month lowers the next month's charge. That is
    /// § 12-722(a) and (b) installment by installment, with payments applied
    /// oldest first, because every 2026 due date falls on a 15th. The rate is
    /// set by statute, so no month lacks one.
    fn penalty(&self, f: &Facts, as_of: Date) -> Result<working::Result<Penalty>, Error> {
        if as_of.is_zero() {
            return Err(Error::Invalid(
                "connecticuttaxcalc: interest needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(Error::with_detail(
                Error::NeedsThisYearTax,
                format!(
                    "with no {} Connecticut return to measure against, there is no target to figure interest from",
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
        let waiver_answered = waiver_opt.is_some();
        let waiver = waiver_opt.unwrap_or_default();
        let (schedule, mut notes) = self.schedule_of(f, &a)?;
        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &schedule)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let (credits, _) = self.credits(f, &a)?;
        let required = a.required();
        let source = "Conn. Gen. Stat. § 12-722(a): 1% per month or fraction thereof";
        let mut pb = estimated::new_penalty(stop);
        pb.waiver(waiver).because(a.requirement.clone(), &schedule);
        let mut needed = USCents(0);
        let mut carried = false;
        for (k, &due) in a.dues.iter().enumerate() {
            needed += required[k];
            if a.excused && k == 3 {
                // Worksheet D is not completed.
                continue;
            }
            let paid: USCents = credits
                .iter()
                .filter(|c| !c.on().after(due))
                .map(|c| c.amount())
                .sum();
            let under = needed - paid;
            if under.0 <= 0 {
                continue;
            }
            let mut end = y.stop;
            if k + 1 < a.dues.len() {
                end = a.dues[k + 1];
            }
            let mut lb = estimated::new_penalty_line(k as i64 + 1, due, under);
            let (mut base, mut last_day) = (under, due);
            let mut j = 0;
            while base.0 > 0 {
                let (from, to) = (due.add_months(j), due.add_months(j + 1));
                if !from.before(end) || !from.before(stop) {
                    break;
                }
                let part = estimated::monthly(base, from, to, y.monthly_rate, source)?;
                lb.part(part);
                last_day = earlier(to, stop);
                for c in &credits {
                    if c.on().after(from) && !c.on().after(to) && base.0 > 0 {
                        base -= c.amount();
                        if base.0 <= 0 {
                            (base, last_day) = (USCents(0), earlier(c.on(), stop));
                        }
                    }
                }
                j += 1;
            }
            if a.excused && k == 2 && base.0 > 0 {
                carried = true;
            }
            if last_day == due {
                continue;
            }
            let line = lb.days(last_day.days_since(due)).build()?;
            pb.line(line);
        }
        let mut p = pb.build()?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            notes.rule(
                "Conn. Gen. Stat. § 12-701(a)(12)",
                format!(
                    "this year's tax was not given, so the interest is figured against {} of the {} tax alone; the required annual payment is the lesser of that and {} of this year's tax, so {} is the most the interest can be, and it is less if this year's tax turns out lower",
                    a.requirement.options()[1].percent(),
                    y.year - 1,
                    y.this_year_share,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "Conn. Gen. Stat. § 12-722(b); CT-2210 (Rev. 12/25), Form CT-2210 Instructions",
                format!(
                    "the return was filed with the whole balance paid on {}, so every underpayment still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        if carried {
            notes.rule(
                "CT-2210 (Rev. 12/25), general instructions",
                "the return was filed and paid by January 31, so Schedule B Worksheet D is not completed and nothing is charged after January 15, on any installment",
            );
        }
        notes.rule(
            "Conn. Gen. Stat. § 12-722(a), (b); CT-2210 (Rev. 12/25), Part 3 lines 8 to 17 and Schedule B",
            format!(
                "each due date's underpayment, after withholding, the PE Tax Credit and payments made by then, is charged {} for each month or part of a month, a month running from the 16th to the 15th, until paid or {}; a payment during a month lowers the next month's charge; the interest to {} is {}",
                y.monthly_rate,
                y.stop.long(),
                stop.long(),
                p.total()
            ),
        );
        if waiver_answered && waiver != WaiverKind::None {
            notes.rule(
                "Conn. Gen. Stat. § 12-722; CT-2210 (Rev. 12/25)",
                format!(
                    "neither § 12-722 nor CT-2210 provides a waiver for {waiver}; the interest shown is not reduced"
                ),
            );
            if p.total().0 > 0 {
                p = p
                    .to_builder()
                    .waiver_outcome(connecticut_waiver(waiver))
                    .build()?;
            }
        } else if !waiver_answered && p.total().0 > 0 {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Do you want interest waived for a casualty, disaster, retirement or disability?",
                "Connecticut's estimated tax rules give no waiver for these. The interest shown stands unless another law lets the department waive it.",
            );
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    /// Written by hand and checked against research/estimated-payments/ct.md.
    fn explanation(&self) -> Explanation {
        estimated::new_explanation(
            "Connecticut expects estimated payments if your tax will be $1,000 or more. The tax is counted after withholding and any Pass-Through Entity Tax Credit. Paying too little by a due date costs interest of 1% a month on the shortfall.",
        )
        .paragraph("There is no interest if your payments together reach the smaller of two figures. One is 90% of this year's Connecticut tax. The other is 100% of the tax on last year's Connecticut return. Withholding and the Pass-Through Entity Tax Credit count as payments. Last year's figure needs a return that covered twelve months. Unlike the federal rules, there is no 110% figure at higher incomes.")
        .paragraph("Pay a quarter by each of Apr 15, Jun 15, Sep 15 and Jan 15 of the next year. A date on a weekend or legal holiday becomes the next business day. Withholding counts as paid evenly across the four dates. You can instead show the dates it was actually withheld. The Pass-Through Entity Tax Credit is always spread evenly. Filing and paying in full by January 31 clears interest on the January payment.")
        .paragraph("Withholding and the credit are taken off this year's tax. There is no interest if under $1,000 is left. Nor is there any if you lived in Connecticut all last year and had no tax there.")
        .paragraph("Connecticut calls the charge interest rather than a penalty. It is 1% for each month or part of a month that a shortfall stays unpaid. Months run from the 16th to the 15th, so a day late is charged a whole month. The rate is fixed by law and doesn't follow market rates. It stops when the shortfall is paid, and no later than April 15 of next year. With uneven income, Form CT-2210's annualised method can lower early payments. Connecticut's rules have no waiver for a casualty or disaster. Nor do they have one for retirement or disability.")
        .versus_federal("Connecticut charges 1% for each month or part of one, about 12% a year. Federal interest is a quarterly rate counted by the day. There is no 110% figure at higher income. The Pass-Through Entity Tax Credit counts like withholding. There is no waiver for a casualty, disaster, retirement or disability. The $1,000 threshold, the 90% and 100% tests and four quarters match federal.")
        .source("Conn. Gen. Stat. §§ 12-701(a)(12), 12-722")
        .source("Form CT-1040ES, 2026 Estimated Connecticut Income Tax Payment Coupon for Individuals (Rev. 01/26)")
        .source("Form CT-2210, Underpayment of Estimated Income Tax by Individuals, Trusts, and Estates, and its instructions (Rev. 12/25)")
        .build()
        .unwrap_or_default()
    }
}

/// Refuses the waiver asked for: neither § 12-722 nor the CT-2210
/// instructions provide one for a casualty, disaster, retirement or
/// disability.
fn connecticut_waiver(k: WaiverKind) -> WaiverOutcome {
    if k == WaiverKind::Casualty {
        return estimated::waiver_refused(
            "There is no waiver of the interest for a casualty or disaster. Neither Connecticut's estimated tax law nor Form CT-2210 gives one.",
        );
    }
    estimated::waiver_refused(
        "There is no waiver of the interest for retirement or disability. Neither Connecticut's estimated tax law nor Form CT-2210 gives one.",
    )
}

/// Whether the prior year was spent outside Connecticut, or partly outside
/// it, with no Connecticut income: CT-2210 Box E.
fn no_source_income(prior: &PriorReturn, extras: &EstimatedFacts) -> bool {
    prior.months() == 12
        && prior.full_year_resident() == Some(false)
        && extras.prior_year_connecticut_source_income() == Some(false)
}

fn earlier(a: Date, b: Date) -> Date {
    if a.before(b) { a } else { b }
}
