//! The federal estimated tax rules, 26 U.S.C. § 6654, as the shared
//! `estimated::EstimatedTax` trait. The required annual payment is
//! [`crate::required_annual_payment`], the installments are
//! [`crate::instalments`] and the withholding credit is
//! [`crate::withholding_credited_by`]: this module adds the dates moved for
//! weekends and holidays, the exceptions, the penalty, and the explanation.
//!
//! A year's figures are in estimated_y2026.rs and its successors. A year
//! with no module is `estimated::Error::NoRules`.

use estimated::{
    Basis, Calendar, Date, EstimatedTax, Explanation, Facts, Installment, LedgerEntry, Penalty,
    RateSeries, Requirement,
};
use money::{Rate, USCents};
use working::{Notes, PositionKind};

use crate::{instalments, percent, required_annual_payment, table_for, withholding_credited_by};

/// One tax year's published figures for § 6654.
pub(crate) struct EstimatedYear {
    pub(crate) year: i64,

    /// § 6654(e)(1): no penalty when tax less withholding is under it.
    pub(crate) threshold: USCents,

    /// The legal holidays that move a due date under § 7503.
    pub(crate) calendar: Calendar,

    /// § 6654(h): a return filed and paid by this date, before moving for a
    /// weekend, excuses the January installment.
    pub(crate) file_and_pay_by: Date,

    /// § 6654(b)(2)(B): the penalty runs no later than April 15th of the
    /// next year.
    pub(crate) stop: Date,

    /// The Schedule AI annualisation period ends.
    pub(crate) period_ends: Vec<Date>,

    /// The § 6621(a)(2) underpayment rate, quarter by quarter, as far as it
    /// has been published.
    pub(crate) rates: RateSeries,
}

/// § 6654(d)(1)(A): each installment is 25% of the required annual payment,
/// and § 6654(g)(1) deems withholding paid in the same equal parts.
const INSTALLMENT_SHARES: [Rate; 4] = [Rate(2500), Rate(2500), Rate(2500), Rate(2500)];

/// § 6654(d)(2)(C)(ii), Schedule AI line 20.
const ANNUALISED_PERCENTAGES: [Rate; 4] = [Rate(2250), Rate(4500), Rate(6750), Rate(9000)];

/// Returns the federal estimated tax rules for a tax year, or
/// `estimated::Error::NoRules` for a year this build does not carry.
pub fn estimated_tax(year: i64) -> Result<Box<dyn EstimatedTax>, estimated::Error> {
    let y = match year {
        2026 => crate::estimated_y2026::year_2026()?,
        _ => return Err(estimated::no_rules(&estimated::FEDERAL, year)),
    };
    Ok(Box::new(FederalEstimated { y }))
}

struct FederalEstimated {
    y: EstimatedYear,
}

/// What all three questions share: the requirement, the dates and amounts
/// by installment, and the working behind them.
struct Assessment {
    requirement: Requirement,

    /// No penalty can arise, so nothing is needed by any date.
    exempt: bool,

    /// Whether this year's tax was given. Without it the target is last
    /// year's option alone, and the penalty is an upper bound.
    tax_known: bool,

    dues: Vec<Date>,
    /// Each installment's own amount, before excusing.
    installments: Vec<USCents>,
    /// The January installment, for filing early.
    excused: bool,

    notes: Notes,
}

impl FederalEstimated {
    fn check(&self, f: &Facts) -> Result<(), estimated::Error> {
        if f.jurisdiction() != estimated::FEDERAL {
            return Err(estimated::Error::Invalid(format!(
                "ustaxcalc: these are the federal rules, and the facts are for {}",
                f.jurisdiction()
            )));
        }
        if f.tax_year() != self.y.year {
            return Err(estimated::Error::Invalid(format!(
                "ustaxcalc: these are the {} rules, and the facts are for {}",
                self.y.year,
                f.tax_year()
            )));
        }
        if f.farmer_or_fisher() {
            return Err(estimated::Error::with_detail(
                estimated::Error::Unsupported,
                "§ 6654(i) puts farmers and fishermen under rules of their own",
            ));
        }
        if f.elections().annualised() {
            for (i, p) in f.periods().iter().enumerate() {
                let want = self.y.period_ends.get(i).copied().unwrap_or_default();
                if p.end() != want {
                    return Err(estimated::Error::Invalid(format!(
                        "ustaxcalc: Schedule AI period {} ends on {}, not {}",
                        i + 1,
                        want,
                        p.end()
                    )));
                }
            }
        }
        Ok(())
    }

    fn assess(&self, f: &Facts) -> Result<Assessment, estimated::Error> {
        self.check(f)?;
        let y = &self.y;
        let mut notes = Notes::default();
        let mut exempt = false;
        let mut target_qs = TargetQuestions::default();
        let tax_given = f.this_year().tax();
        let tax_known = tax_given.is_some();
        let tax = tax_given.unwrap_or_default();
        let withheld = f.withheld_total();
        let prior_given = f.prior_year();
        let has_prior = prior_given.is_some();
        let prior = prior_given.unwrap_or_default();
        let last = y.year - 1;
        let zero = USCents(0);

        // The two ways of measuring the required annual payment. The
        // prior-year test goes through required_annual_payment, which
        // carries the § 6654(d) arithmetic and its working.
        let usable = has_prior && prior.filed() && prior.months() == 12;
        let (mut prior_tax, mut prior_agi) = (zero, zero);
        let mut agi_known = false;
        if usable {
            let pf = prior.figures().unwrap_or_default();
            prior_tax = pf.tax().unwrap_or_default();
            if let Some(agi) = pf.federal_agi() {
                (prior_agi, agi_known) = (agi, true);
            }
        }
        let harbour = required_annual_payment(
            y.year,
            f.filing_status(),
            tax,
            prior_tax,
            prior_agi,
            usable && agi_known,
        )?;
        // required_annual_payment's working is the current-year rule, then
        // either the prior-year rule and the lesser of the two, or that
        // there is no prior-year test. Only what applies to these facts is
        // kept: without last year's AGI the prior-year test is figured below
        // with its assumption stated, and without this year's tax neither
        // the current-year rule nor the lesser of the two applies.
        let harbour_rules = harbour.applied();
        if usable && !agi_known && tax_known {
            notes.applied(&harbour_rules[..1]);
        } else if usable && !agi_known {
        } else if !tax_known && usable {
            notes.applied(&harbour_rules[1..2]);
        } else if !tax_known {
            notes.applied(&harbour_rules[1..]);
        } else {
            notes.applied(&harbour_rules);
        }

        let hv = harbour.value();
        let this_year = if tax_known {
            estimated::new_target_option(Basis::ThisYearTax, percent(90), hv.current_year_test)
                .build()?
        } else {
            estimated::new_target_option(Basis::ThisYearTax, percent(90), zero)
                .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                .build()?
        };
        let mut prior_option = estimated::new_target_option(
            Basis::PriorYearTax,
            hv.prior_year_rate,
            hv.prior_year_test,
        );
        if !has_prior {
            prior_option = estimated::new_target_option(Basis::PriorYearTax, percent(100), zero);
            prior_option.unavailable(format!("Needs your {last} return"));
            target_qs.ask(
                PositionKind::PriorYearReturn,
                format!("What was the total tax on your {last} return?"),
                "With it, last year's tax could set your target instead of this year's.",
            );
        } else if !prior.filed() {
            let mut why = format!("You didn't file a {last} return");
            if prior.not_required_to_file() {
                why = format!(
                    "You didn't need to file a {last} return, so it has no tax to measure against"
                );
            }
            prior_option = estimated::new_target_option(Basis::PriorYearTax, percent(100), zero);
            prior_option.unavailable(why);
        } else if prior.months() != 12 {
            prior_option = estimated::new_target_option(Basis::PriorYearTax, percent(100), zero);
            prior_option.unavailable(format!(
                "Your {} return covered {} months, and this test needs all 12",
                last,
                prior.months()
            ));
        } else if !agi_known {
            // § 6654(d)(1)(C) needs last year's AGI. Without it, assume the
            // higher percentage: the target can only be too high, never too
            // low.
            let table = table_for(y.year, f.filing_status())?;
            let amount = percent(110).of(prior_tax);
            prior_option = estimated::new_target_option(Basis::PriorYearTax, percent(110), amount);
            notes.rule(
                "26 U.S.C. § 6654(d)(1)(C)(i)",
                format!(
                    "110% of the {prior_tax} tax on the {last} return is {amount}; 110% rather than 100% because the {last} AGI was not given"
                ),
            );
            target_qs.ask(
                PositionKind::AdjustedGrossIncome,
                format!("What was your {last} adjusted gross income?"),
                format!(
                    "Until then, the target is 110% of last year's tax. If it was {} or less, the target would be 100%.",
                    table.prior_year_agi_step.whole()
                ),
            );
        }
        let prior_opt = prior_option.build()?;

        let mut rb = estimated::new_requirement();
        rb.option(this_year.clone()).option(prior_opt.clone());
        let mut chosen = Basis::ThisYearTax;
        if prior_opt.available() && (!tax_known || prior_opt.amount() < this_year.amount()) {
            chosen = Basis::PriorYearTax;
        }
        let mut target = this_year.amount();
        if chosen == Basis::PriorYearTax {
            target = prior_opt.amount();
        }
        if !tax_known {
            target_qs.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} tax to be?", y.year),
                estimated::without_this_year_tax(prior_opt.available()),
            );
        }

        // § 6654(e)(2): no liability last year means no penalty this year.
        let no_liability = has_prior
            && prior.months() == 12
            && ((prior.filed() && prior_tax == zero) || prior.not_required_to_file());
        let residence = prior.full_year_resident();
        let (resident, answered) = (residence.unwrap_or(false), residence.is_some());
        if no_liability && !answered {
            notes.ask(
                PositionKind::Residency,
                format!("Were you a U.S. citizen or resident for all of {last}?"),
                format!(
                    "If you were, having no tax liability for {} means no penalty for {}.",
                    last, y.year
                ),
            );
        }

        if no_liability && answered && resident {
            exempt = true;
            let none = estimated::new_exception("No federal tax liability last year").build()?;
            rb.required(
                false,
                format!(
                    "You were a U.S. citizen or resident all of {}, with no federal tax liability. So no estimated payments are needed for {}.",
                    last, y.year
                ),
            )
            .exempt(none);
            notes.rule(
                "26 U.S.C. § 6654(e)(2)",
                format!(
                    "the {} return showed no liability for a full 12 months, so there is no penalty for {}",
                    last, y.year
                ),
            );
        } else if !tax_known && !prior_opt.available() {
            rb.required(
                false,
                format!(
                    "Your target is 90% of your {} tax, or 100% of a full-year {} return's tax. Without either figure, there is no target to pay toward yet.",
                    y.year, last
                ),
            )
            .no_target();
        } else if !tax_known && withheld >= target {
            rb.required(
                false,
                format!(
                    "Your {} of withholding already reaches {} of your {} tax, {}. So no estimated payments are needed, whatever your {} tax turns out to be.",
                    withheld.whole(),
                    prior_opt.percent(),
                    last,
                    target.whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if !tax_known {
            rb.required(
                true,
                format!(
                    "Your target is {} of your {} tax, {}. After {} of withholding, that's {} of estimated payments. Paying it keeps you clear of a penalty whatever your {} tax turns out to be.",
                    prior_opt.percent(),
                    last,
                    target.whole(),
                    withheld.whole(),
                    (target - withheld).whole(),
                    y.year
                ),
            )
            .choose(chosen);
        } else if tax - withheld < y.threshold {
            // § 6654(e)(1) tests tax less withholding only; estimated
            // payments do not count toward it.
            exempt = true;
            let under =
                estimated::exception_below("Tax less withholding", tax - withheld, y.threshold)?;
            let mut why = format!(
                "Your tax is {}, and {} is withheld. That {}. So no estimated payments are needed.",
                tax.whole(),
                withheld.whole(),
                left_under(false, tax - withheld, y.threshold)
            );
            if withheld == zero {
                why = format!(
                    "You have no withholding, and your tax of {} is under {}. So no estimated payments are needed.",
                    tax.whole(),
                    y.threshold.whole()
                );
            } else if withheld >= tax {
                why = format!(
                    "Your {} withheld covers your {} tax. So no estimated payments are needed.",
                    withheld.whole(),
                    tax.whole()
                );
            }
            rb.required(false, why).exempt(under);
            notes.rule(
                "26 U.S.C. § 6654(e)(1)",
                format!(
                    "{} of tax less {} withheld is {}, under {}, so there is no penalty",
                    tax,
                    withheld,
                    tax - withheld,
                    y.threshold
                ),
            );
        } else if withheld >= target {
            rb.required(
                false,
                format!(
                    "Withholding of {} already reaches your {} target. So no estimated payments are needed.",
                    withheld.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else if withheld == zero {
            rb.required(
                true,
                format!(
                    "You have no withholding, and your {} tax is {} or more. So you need estimated payments that reach your {} target across the year.",
                    tax.whole(),
                    y.threshold.whole(),
                    target.whole()
                ),
            )
            .choose(chosen);
        } else {
            rb.required(
                true,
                format!(
                    "Your tax is {}, and {} is withheld. That leaves {}, which is {} or more. Withholding is {} short of your {} target. So you need {} of estimated payments across the year.",
                    tax.whole(),
                    withheld.whole(),
                    (tax - withheld).whole(),
                    y.threshold.whole(),
                    (target - withheld).whole(),
                    target.whole(),
                    (target - withheld).whole()
                ),
            )
            .choose(chosen);
        }
        let requirement = rb.build()?;
        target_qs.ask_unless_exempt(&mut notes, exempt);

        // The due dates. § 6654(c)(2) fixes them and § 7503 moves one that
        // falls on a weekend or legal holiday.
        let ins = instalments(y.year, requirement.target());
        let statutory: Vec<Date> = ins.iter().map(|i| i.due).collect();
        let cumulative: Vec<USCents> = ins.iter().map(|i| i.cumulative_target).collect();
        let dues = estimated::move_dates(&y.calendar, &statutory);
        for (s, d) in statutory.iter().zip(&dues) {
            if d != s {
                notes.rule(
                    "26 U.S.C. § 7503",
                    format!(
                        "the installment due {} falls on a weekend or legal holiday, so it is due {}",
                        s.long(),
                        d.long()
                    ),
                );
            }
        }
        let mut installments = estimated::increments(&cumulative);
        notes.rule(
            "26 U.S.C. § 6654(c)(2), (d)(1)(A)",
            format!(
                "installments of 25% of {} are due {}, {}, {} and {}",
                requirement.target(),
                dues[0].long(),
                dues[1].long(),
                dues[2].long(),
                dues[3].long()
            ),
        );

        if f.elections().annualised() && !exempt {
            let taxes: Vec<USCents> = f.periods().iter().map(|p| p.annualised_tax()).collect();
            let ann = estimated::annualised(&taxes, &ANNUALISED_PERCENTAGES, &installments)?;
            notes.rule(
                "26 U.S.C. § 6654(d)(2); Form 2210 Schedule AI",
                format!(
                    "under the annualised method the installments are {}, {}, {} and {}: 22.5%, 45%, 67.5% and 90% of the annualised tax, \
                     less earlier installments, and never more than the regular installment plus any shortfall the method carried forward",
                    ann[0], ann[1], ann[2], ann[3]
                ),
            );
            installments = ann;
        }

        // § 6654(h): filing and paying by January 31st excuses the last
        // installment. January 31, 2027 is a Sunday, so § 7503 moves it to
        // February 1st, as the 2026 Form 1040-ES prints it.
        let mut excused = false;
        let file_by = y.calendar.next_business_day(y.file_and_pay_by);
        let filed = f.return_filed();
        if !filed.is_zero() && !filed.after(file_by) && !exempt {
            excused = true;
            notes.rule(
                "26 U.S.C. § 6654(h)",
                format!(
                    "the return was filed and paid on {}, by {}, so the January installment is excused",
                    filed.long(),
                    file_by.long()
                ),
            );
        }

        if exempt {
            installments = vec![zero; dues.len()];
        }
        Ok(Assessment {
            requirement,
            exempt,
            tax_known,
            dues,
            installments,
            excused,
            notes,
        })
    }

    /// Applies the credits to the installments, with the January
    /// installment set to nothing when filing early excused it.
    fn ledger(
        &self,
        f: &Facts,
        a: &Assessment,
    ) -> Result<(Vec<LedgerEntry>, Notes), estimated::Error> {
        let as_withheld = f.elections().withholding_as_withheld();
        let credits = if as_withheld {
            estimated::credits_as_withheld(f, &a.dues)?
        } else {
            estimated::credits(f, &a.dues, &INSTALLMENT_SHARES)?
        };
        let mut required = a.installments.clone();
        if a.excused {
            required[3] = USCents(0);
        }
        let ledger = estimated::apply(&a.dues, &required, &credits)?;
        let mut notes = Notes::default();
        if as_withheld {
            notes.rule(
                "26 U.S.C. § 6654(g)(1)",
                format!(
                    "by election, the {} withheld is credited on the dates it was withheld",
                    f.withheld_total()
                ),
            );
        } else {
            notes.rule(
                "26 U.S.C. § 6654(g)(1)",
                format!(
                    "the {} withheld is treated as paid in four equal parts on the due dates, {} by the first, whenever it was actually withheld",
                    f.withheld_total(),
                    withholding_credited_by(f.withheld_total(), 1)
                ),
            );
        }
        Ok((ledger, notes))
    }
}

/// Words an amount under `limit`: "leaves $742, under $1,000", "leaves just
/// under $1,000" when whole dollars would show the two as equal, or "leaves
/// a $136 refund" below zero. `past` uses "left".
fn left_under(past: bool, amount: USCents, limit: USCents) -> String {
    let (verb, words): (&str, fn(USCents) -> String) = if past {
        ("left", estimated::left)
    } else {
        ("leaves", estimated::leaves)
    };
    if amount < USCents(0) {
        return words(amount);
    }
    if amount.whole() == limit.whole() {
        return format!("{verb} just under {}", limit.whole());
    }
    format!("{}, under {}", words(amount), limit.whole())
}

/// The installments from the ledger, with the January installment excused
/// when filing early excused it.
fn schedule(a: &Assessment, ledger: &[LedgerEntry]) -> Result<Vec<Installment>, estimated::Error> {
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

impl EstimatedTax for FederalEstimated {
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, estimated::Error> {
        let a = self.assess(f)?;
        Ok(working::new_result(a.requirement).with(&a.notes).build()?)
    }

    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, estimated::Error> {
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(estimated::Error::with_detail(
                estimated::Error::NeedsThisYearTax,
                format!(
                    "with no {} return to measure against, there is no target to schedule",
                    self.y.year - 1
                ),
            ));
        }
        let (ledger, notes) = self.ledger(f, &a)?;
        let out = schedule(&a, &ledger)?;
        Ok(working::new_result(out)
            .with(&a.notes)
            .with(&notes)
            .build()?)
    }

    fn penalty(
        &self,
        f: &Facts,
        as_of: Date,
    ) -> Result<working::Result<Penalty>, estimated::Error> {
        if as_of.is_zero() {
            return Err(estimated::Error::Invalid(
                "ustaxcalc: a penalty needs the date to figure it to".into(),
            ));
        }
        let a = self.assess(f)?;
        if !a.requirement.target_known() {
            return Err(estimated::Error::with_detail(
                estimated::Error::NeedsThisYearTax,
                format!(
                    "with no {} return to measure against, there is no target to figure a penalty from",
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
        let waiver_given = f.positions().waiver();
        let (waiver, waiver_answered) = (waiver_given.unwrap_or_default(), waiver_given.is_some());

        let (ledger, mut notes) = self.ledger(f, &a)?;
        let sched = schedule(&a, &ledger)?;

        if a.exempt {
            let p = estimated::new_penalty(stop)
                .waiver(waiver)
                .because(a.requirement.clone(), &sched)
                .build()?;
            return Ok(working::new_result(p).with(&a.notes).build()?);
        }

        let (mut p, unrated) =
            estimated::simple_penalty(&ledger, stop, &y.rates, waiver, &a.requirement, &sched)?;
        if !a.tax_known {
            p = p.to_builder().upper_bound().build()?;
            let prior_percent = a
                .requirement
                .options()
                .get(1)
                .map(|o| o.percent())
                .unwrap_or_default();
            notes.rule(
                "26 U.S.C. § 6654(d)(1)(B)",
                format!(
                    "this year's tax was not given, so the penalty is figured against {} of the {} tax alone; \
                     the required payment is the lesser of that and 90% of this year's tax, so {} is the most the penalty can be, and it is less if this year's tax turns out lower",
                    prior_percent,
                    y.year - 1,
                    p.total()
                ),
            );
        }
        if paid_in_full {
            notes.rule(
                "26 U.S.C. § 6654(b)(2)",
                format!(
                    "the return was filed with the whole balance paid on {}, so every shortfall still outstanding was paid that day",
                    stop.long()
                ),
            );
        }
        notes.rule(
            "26 U.S.C. § 6654(a), (b)(2); 26 U.S.C. § 6621(a)(2)",
            format!(
                "each shortfall is charged simple interest at the underpayment rate for each quarter, by the day, from its due date until paid or {}, \
                 with each payment applied to the oldest shortfall first; the penalty to {} is {}",
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
                    unrated.add_days(-1).long()
                ),
            );
        }
        if p.total() > USCents(0) && !waiver_answered {
            notes.ask(
                PositionKind::PenaltyWaiver,
                "Are you asking the IRS to waive the penalty?",
                "A waiver for a casualty, disaster or other unusual event leaves no penalty due. So does one for retiring after 62 or disability, with reasonable cause.",
            );
        }
        if let Some(o) = estimated::standard_waiver(f, "the IRS", 62)
            && p.total() > USCents(0)
        {
            p = p.to_builder().waiver_outcome(o).build()?;
        }
        Ok(working::new_result(p).with(&a.notes).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        federal_explanation()
    }
}

/// Written by hand. It describes § 6654 as it stands for the years this
/// module carries; a change in the law changes it here.
fn federal_explanation() -> Explanation {
    estimated::new_explanation(
        "The IRS expects tax to be paid as income is earned. If withholding does not cover enough of it, you make four estimated payments. Paying too little or too late costs interest on the shortfall.")
        .paragraph("To avoid the penalty, withholding and estimated payments must reach a target. The target is 90% of this year's tax or, if lower, 100% of last year's tax. That 100% becomes 110% if last year's adjusted gross income was over $150,000. For married filing separately, the limit is $75,000. Most people with uneven or rising income use last year's figure. They do so because it is fixed before the year starts. Meeting it protects you however this year turns out. It is available only if last year's return covered a full twelve months.")
        .paragraph("There is no penalty if this year's tax, less withholding, is under $1,000. There is also no penalty if you had no tax liability last year. That applies only if you were a U.S. citizen or resident for all of last year.")
        .paragraph("Each payment is a quarter of the yearly figure. They are due April 15, June 15, September 15 and January 15 of the next year. A date on a weekend or holiday shifts to the next business day. Withholding counts as paid evenly across those dates, whenever it was taken. So raising withholding late in the year still counts toward the earlier dates. You can skip the January payment by filing and paying in full by January 31.")
        .paragraph("The penalty is interest, not a fine. Each shortfall costs the IRS underpayment rate for each day it stays unpaid. The rate is reset every quarter. The charge stops on April 15 of the next year. A payment goes first to the oldest shortfall, whichever date you meant it for. Schedule AI's annualised method can lower early installments for uneven income. The IRS can waive the penalty for a casualty or disaster. It can also waive it for retiring after 62 or disability, with reasonable cause.")
        .source("26 U.S.C. § 6654")
        .source("2026 Form 1040-ES, Estimated Tax for Individuals")
        .source("2025 Form 2210 and its instructions")
        .source("Rev. Rul. 2026-5, 2026-10 and 2026-15 (underpayment rates)")
        .build()
        .unwrap_or_default()
}

/// A question whose answer can change the target. It is asked only when no
/// exception sets the target.
struct TargetQuestion {
    position: PositionKind,
    question: String,
    effect: String,
}

#[derive(Default)]
struct TargetQuestions(Vec<TargetQuestion>);

impl TargetQuestions {
    fn ask(
        &mut self,
        position: PositionKind,
        question: impl Into<String>,
        effect: impl Into<String>,
    ) {
        self.0.push(TargetQuestion {
            position,
            question: question.into(),
            effect: effect.into(),
        });
    }

    /// Adds the questions to notes unless an exception set the target.
    fn ask_unless_exempt(&self, notes: &mut Notes, exempt: bool) {
        if exempt {
            return;
        }
        for t in &self.0 {
            notes.ask(t.position, t.question.clone(), t.effect.clone());
        }
    }
}
