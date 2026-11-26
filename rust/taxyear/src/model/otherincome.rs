//! Expands an OtherIncomeModel, one source of other income, into its items.
//! Ported from the Go package `taxyear/model/otherincome`.
//!
//! It has no slots. A whole-year form states the source's year: its assumed
//! parts stand aside, except a one-off whose answer lists the form as a
//! different payment. A YearAmount is the year's figures, evidence included:
//! it adds the total less the evidence, floored at zero, monthly from the
//! month after the evidence for a recurring source, else on December 31, and
//! stands aside once the evidence exceeds it. See docs/models.md.

use std::collections::HashSet;

use money::USCents;
use proto::scenariosv1::{
    self,
    __buffa::oneof::item::Figures as ItemFigures,
    __buffa::oneof::other_income_part::{
        Part,
        from_forms::form::{Form as FormKind, Supplement},
        one_off::Event,
    },
    Actual, ExpectedDocument, ItemKind, ItemStatus, OtherIncomeFigures, OtherIncomeKind,
    OtherIncomeModel, OtherIncomePart, OtherIncomeProjection, OtherIncomeTotals, RentalExpenses,
    origin::Basis,
};

use super::account::origin;
use super::{Context, Error, Notes};
use crate::internal::civil::{self, Date};
use crate::model;

#[derive(Clone, Debug, Default)]
struct Figures {
    gross: USCents,
    taxable: USCents,
    federal: USCents,
    state: USCents,
    expenses: USCents,
    taxable_set: bool,
    codes: Vec<String>,
    ira: bool,
}

impl Figures {
    fn proto(&self) -> OtherIncomeFigures {
        let mut out = OtherIncomeFigures {
            gross: model::money(self.gross).into(),
            federal_withheld: model::money(self.federal).into(),
            state_withheld: model::money(self.state).into(),
            expenses: model::money(self.expenses).into(),
            distribution_codes: self.codes.clone(),
            ira: self.ira,
            ..Default::default()
        };
        if self.taxable_set {
            out.taxable = model::money(self.taxable).into();
        }
        out
    }

    fn plus(mut self, o: &Figures) -> Figures {
        self.gross += o.gross;
        self.federal += o.federal;
        self.state += o.state;
        self.expenses += o.expenses;
        self.taxable += o.taxable_or_gross();
        self.taxable_set = true;
        self
    }

    fn taxable_or_gross(&self) -> USCents {
        if self.taxable_set {
            return self.taxable;
        }
        self.gross
    }
}

struct Expansion<'a> {
    ctx: &'a Context,
    m: &'a OtherIncomeModel,
    notes: Notes,
    items: Vec<scenariosv1::Item>,
    forms: Vec<String>,
    through: Date,
}

/// Expands one other income model.
pub fn expand(
    ctx: &Context,
    m: &OtherIncomeModel,
) -> Result<(OtherIncomeProjection, Notes), Error> {
    let mut e = Expansion {
        ctx,
        m,
        notes: Notes::default(),
        items: Vec::new(),
        forms: Vec::new(),
        through: Date::default(),
    };
    e.evidence();
    e.assumed()?;
    let p = e.projection();
    Ok((p, e.notes))
}

fn read(f: &OtherIncomeFigures) -> Figures {
    let mut x = Figures {
        gross: model::value(&f.gross),
        federal: model::value(&f.federal_withheld),
        state: model::value(&f.state_withheld),
        expenses: model::value(&f.expenses),
        codes: f.distribution_codes.clone(),
        ira: f.ira,
        ..Default::default()
    };
    if f.taxable.is_set() {
        x.taxable = model::value(&f.taxable);
        x.taxable_set = true;
    }
    x
}

fn figures_of(it: &scenariosv1::Item) -> &OtherIncomeFigures {
    match &it.figures {
        Some(ItemFigures::OtherIncome(f)) => f,
        _ => <OtherIncomeFigures as buffa::DefaultInstance>::default_instance(),
    }
}

/// Adds a rental's Schedule E lines 5 to 19, depreciation included. Go
/// ranges over the message's set fields by reflection; these are every
/// field of RentalExpenses, all Money.
fn rental_expenses(r: &RentalExpenses) -> USCents {
    [
        &r.advertising,
        &r.auto_and_travel,
        &r.cleaning_and_maintenance,
        &r.commissions,
        &r.insurance,
        &r.legal_and_professional,
        &r.management_fees,
        &r.mortgage_interest,
        &r.other_interest,
        &r.repairs,
        &r.supplies,
        &r.taxes,
        &r.utilities,
        &r.depreciation,
        &r.other,
    ]
    .into_iter()
    .map(model::value)
    .sum()
}

fn form_for(k: OtherIncomeKind) -> &'static str {
    match k {
        OtherIncomeKind::OTHER_INCOME_KIND_RETIREMENT_WITHDRAWALS => "Form1099R",
        OtherIncomeKind::OTHER_INCOME_KIND_SOCIAL_SECURITY => "FormSsa1099",
        OtherIncomeKind::OTHER_INCOME_KIND_UNEMPLOYMENT
        | OtherIncomeKind::OTHER_INCOME_KIND_STATE_REFUND => "Form1099G",
        OtherIncomeKind::OTHER_INCOME_KIND_GAMBLING => "FormW2G",
        _ => "",
    }
}

impl<'a> Expansion<'a> {
    fn prefix(&self) -> String {
        model::item_id(&["other_income", &self.m.id])
    }

    fn path(&self) -> String {
        format!("model.other_income[{}]", self.m.id)
    }

    #[allow(clippy::too_many_arguments)]
    fn add(
        &mut self,
        id: String,
        d: Date,
        status: ItemStatus,
        basis: Basis,
        part: &str,
        evidence: &[&str],
        f: &Figures,
    ) {
        self.items.push(scenariosv1::Item {
            id,
            date: d.field(),
            status: status.into(),
            kind: ItemKind::ITEM_KIND_PAYMENT.into(),
            origin: origin(basis, part, evidence).into(),
            figures: Some(f.proto().into()),
            ..Default::default()
        });
        if status == ItemStatus::ITEM_STATUS_MADE {
            self.through = civil::later(self.through, d);
        }
    }

    fn evidence(&mut self) {
        let m = self.m;
        let made = ItemStatus::ITEM_STATUS_MADE;
        for p in &m.parts {
            match &p.part {
                Some(Part::FromForms(ff)) => {
                    for f in &ff.forms {
                        if !model::usable(&f.source) {
                            continue;
                        }
                        let (x, d) = self.form(f);
                        self.forms.push(f.id.clone());
                        self.add(
                            model::item_id(&[&self.prefix(), &d.to_string(), "payment", &f.id]),
                            d,
                            made,
                            Basis::BASIS_DOCUMENT,
                            &p.id,
                            &[&f.id],
                            &x,
                        );
                    }
                }
                Some(Part::FromLedger(fl)) => {
                    for c in &fl.credits {
                        let d = civil::from_field(&c.date).unwrap_or_default();
                        self.add(
                            model::item_id(&[&self.prefix(), &d.to_string(), "payment", &c.id]),
                            d,
                            made,
                            Basis::BASIS_LEDGER,
                            &p.id,
                            &[&c.id],
                            &Figures {
                                gross: model::value(&c.amount),
                                ..Default::default()
                            },
                        );
                    }
                }
                _ => {}
            }
        }
    }

    /// One whole-year form's figures and the date it is paid on.
    fn form(&mut self, f: &scenariosv1::other_income_part::from_forms::Form) -> (Figures, Date) {
        let mut x = Figures::default();
        let mut d = self.ctx.year_end();
        let sup = f.supplement.as_ref();
        match &f.form {
            Some(FormKind::Form1099R(r)) => {
                // Form 1099-R (26 U.S.C. §§ 72, 402, 408; Form 1040 lines 4a–5b):
                // Box 1 (gross_distribution) is the total distribution. When box 2a
                // (taxable_amount) is populated and box 2b (taxable_amount_not_determined)
                // is not checked, box 2a states the taxable portion; otherwise the full
                // gross distribution is treated as taxable. Box 4 (federal_tax_withheld)
                // flows to Form 1040 line 25b, and box 14 (state_tax_withholding) sums
                // state tax withheld across states.
                x = Figures {
                    gross: model::value(&r.gross_distribution),
                    federal: model::value(&r.federal_tax_withheld),
                    codes: r.distribution_codes.clone(),
                    ira: r.ira_sep_simple,
                    ..Default::default()
                };
                if r.taxable_amount.is_set() && !r.taxable_amount_not_determined {
                    x.taxable = model::value(&r.taxable_amount);
                    x.taxable_set = true;
                }
                for s in &r.state_tax_withholding {
                    x.state += model::value(&s.tax_withheld);
                }
                if let Ok(pd) = civil::from_field(&r.date_of_payment)
                    && !pd.is_zero()
                {
                    d = pd;
                }
            }
            Some(FormKind::FormSsa1099(s)) => {
                // Form SSA-1099 (26 U.S.C. § 86; Form 1040 lines 6a–6b):
                // Box 5 (net_benefits = box 3 benefits paid less box 4 benefits repaid)
                // is the net Social Security benefit for the year; box 6 is voluntary
                // federal income tax withheld (Form 1040 line 25b).
                x = Figures {
                    gross: model::value(&s.net_benefits),
                    federal: model::value(&s.federal_tax_withheld),
                    ..Default::default()
                };
            }
            Some(FormKind::Form1099G(g)) => {
                // Form 1099-G (26 U.S.C. §§ 85, 111; Schedule 1 lines 1 and 7):
                // Box 1 (unemployment_compensation) is taxable under § 85 (Schedule 1
                // line 7); box 2 (state_or_local_refund) is evaluated under the § 111
                // tax benefit rule (Schedule 1 line 1). Box 4 is federal tax withheld.
                x = Figures {
                    gross: model::value(&g.unemployment_compensation)
                        + model::value(&g.state_or_local_refund),
                    federal: model::value(&g.federal_tax_withheld),
                    ..Default::default()
                };
            }
            Some(FormKind::FormW2g(w)) => {
                // Form W-2G (26 U.S.C. § 61; Schedule 1 line 8b):
                // Box 1 (reportable_winnings) is gross gambling winnings; box 4 is
                // federal income tax withheld (Form 1040 line 25b).
                x = Figures {
                    gross: model::value(&w.reportable_winnings),
                    federal: model::value(&w.federal_tax_withheld),
                    ..Default::default()
                };
                if let Ok(dw) = civil::from_field(&w.date_won)
                    && !dw.is_zero()
                {
                    d = dw;
                }
            }
            Some(FormKind::Form1099Misc(mi)) => {
                // Form 1099-MISC (26 U.S.C. § 61; Schedule 1 lines 5 and 8z):
                // Sum the taxable income boxes reported on Form 1099-MISC: box 1 (rents),
                // box 2 (royalties), box 3 (other_income), box 5 (fishing_boat_proceeds),
                // box 6 (medical_and_health_care_payments), box 9 (crop_insurance_proceeds),
                // box 10 (gross_proceeds_paid_to_attorney), box 11 (fish_purchased_for_resale),
                // and box 15 (nonqualified_deferred_compensation, includible under § 409A(a)(1)).
                // Box 14 (excess_golden_parachute_payments) is already included in box 3 per
                // the Instructions for Form 1099-MISC, so it is not added again.
                let gross = model::value(&mi.other_income)
                    + model::value(&mi.rents)
                    + model::value(&mi.royalties)
                    + model::value(&mi.fishing_boat_proceeds)
                    + model::value(&mi.medical_and_health_care_payments)
                    + model::value(&mi.crop_insurance_proceeds)
                    + model::value(&mi.gross_proceeds_paid_to_attorney)
                    + model::value(&mi.fish_purchased_for_resale)
                    + model::value(&mi.nonqualified_deferred_compensation);
                x = Figures {
                    gross,
                    federal: model::value(&mi.federal_tax_withheld),
                    ..Default::default()
                };
                for s in &mi.state_tax_withholding {
                    x.state += model::value(&s.tax_withheld);
                }
                if mi.section_409a_deferrals.is_set()
                    || mi.nonqualified_deferred_compensation.is_set()
                {
                    self.notes.leave_out(
                        "Any excise tax on excess golden parachute payments or § 409A penalties on Form 1099-MISC",
                    );
                }
            }
            Some(FormKind::Form1099Sa(s)) => {
                // Form 1099-SA and Form 8889 Part II (26 U.S.C. § 223(f); Schedule 1 line 8f):
                // Form 8889 line 14a is total distributions (Form 1099-SA box 1, gross_distribution).
                // Subtracting qualified rollovers (Form 8889 line 14b, sup.GetRollover()) yields
                // net distributions on Form 8889 line 14c. Subtracting unreimbursed qualified
                // medical expenses (Form 8889 line 15, sup.GetQualifiedExpenses()), floored at zero,
                // gives the taxable HSA distribution on Form 8889 line 16 (§ 223(f)(1)–(2)).
                let (rollover, qualified) = match sup {
                    Some(Supplement::Form1099SaSupplement(s)) => (
                        model::value(&s.rollover),
                        model::value(&s.qualified_expenses),
                    ),
                    _ => (USCents(0), USCents(0)),
                };
                let gross = model::value(&s.gross_distribution);
                let net = (gross - rollover).max(USCents(0));
                let taxable = (net - qualified).max(USCents(0));
                x = Figures {
                    gross,
                    taxable,
                    taxable_set: true,
                    ..Default::default()
                };
                if taxable.0 > 0 {
                    self.notes.leave_out(
                        "The 20% additional tax on non-qualified HSA distributions (Form 8889 Part II)",
                    );
                }
            }
            Some(FormKind::Form1099S(s)) => {
                // Form 1099-S (26 U.S.C. §§ 1001, 1011, 121; Publication 523; Form 8949 / Schedule D):
                // Amount realized is Form 1099-S box 2 (gross_proceeds) less selling expenses
                // (sup.GetSellingExpenses()), and realized gain is amount realized less adjusted
                // cost basis (sup.GetBasis()), equivalent to gross - (basis + selling_expenses).
                // Gain is floored at zero because a loss on the sale of a personal residence is
                // nondeductible under 26 U.S.C. § 165(c). Subtracting the § 121 principal
                // residence exclusion (sup.GetExcludedAmount(), up to $250,000 / $500,000 joint),
                // floored at zero, gives the taxable gain.
                let (basis, selling, excluded) = match sup {
                    Some(Supplement::Form1099SSupplement(s)) => (
                        model::value(&s.basis),
                        model::value(&s.selling_expenses),
                        model::value(&s.excluded_amount),
                    ),
                    _ => (USCents(0), USCents(0), USCents(0)),
                };
                let gross = model::value(&s.gross_proceeds);
                let expenses = basis + selling;
                let gain = (gross - expenses).max(USCents(0));
                let taxable = (gain - excluded).max(USCents(0));
                x = Figures {
                    gross,
                    taxable,
                    expenses,
                    taxable_set: true,
                    ..Default::default()
                };
                if let Ok(c) = civil::from_field(&s.date_of_closing)
                    && !c.is_zero()
                {
                    d = c;
                }
            }
            Some(FormKind::Form1099C(c)) => {
                // Form 1099-C and Form 982 (26 U.S.C. §§ 61(a)(11), 108(a); Schedule 1 line 8c):
                // Discharged debt reported in Form 1099-C box 2 (amount_of_debt_discharged) is
                // ordinary cancellation-of-debt income under § 61(a)(11), reduced (not below zero)
                // by any amount excluded under § 108 on Form 982 (sup.GetExcludedAmount(), such as
                // bankruptcy, insolvency up to the extent insolvent under § 108(a)(3), or
                // qualified principal residence indebtedness).
                let excluded = match sup {
                    Some(Supplement::Form1099CSupplement(s)) => model::value(&s.excluded_amount),
                    _ => USCents(0),
                };
                let gross = model::value(&c.amount_of_debt_discharged);
                let taxable = (gross - excluded).max(USCents(0));
                x = Figures {
                    gross,
                    taxable,
                    taxable_set: true,
                    ..Default::default()
                };
                if let Ok(ed) = civil::from_field(&c.date_of_identifiable_event)
                    && !ed.is_zero()
                {
                    d = ed;
                }
            }
            Some(FormKind::Form1099Q(q)) => {
                // Form 1099-Q (26 U.S.C. §§ 529(c)(3), 530(d)(2), 72; Publication 970; Schedule 1 line 8z):
                // Under § 529(c)(3)(B), if adjusted qualified education expenses (sup.GetQualifiedExpenses())
                // equal or exceed the net distribution (box 1 gross_distribution less sup.GetRollover()),
                // no part of the distribution is taxable. Otherwise, the earnings portion (box 2,
                // q.GetEarnings()) is prorated by the ratio of non-qualified distributions to the
                // net distribution: taxable = earnings * (net - qual) / net. If neither box 2 (earnings)
                // nor box 3 (basis) is populated, the entire distribution is conservatively treated as earnings.
                let (rollover, qual) = match sup {
                    Some(Supplement::Form1099QSupplement(s)) => (
                        model::value(&s.rollover),
                        model::value(&s.qualified_expenses),
                    ),
                    _ => (USCents(0), USCents(0)),
                };
                let gross = model::value(&q.gross_distribution);
                let mut earnings = model::value(&q.earnings);
                if earnings.0 == 0 && gross.0 > 0 && !q.basis.is_set() {
                    earnings = gross;
                }
                let net = (gross - rollover).max(USCents(0));
                let mut taxable = USCents(0);
                if net.0 > 0 && qual < net {
                    taxable = model::scale(earnings, net - qual, net);
                }
                x = Figures {
                    gross,
                    taxable,
                    taxable_set: true,
                    ..Default::default()
                };
                if taxable.0 > 0 {
                    self.notes.leave_out(
                        "The 10% additional tax on non-qualified education distributions (Form 1099-Q)",
                    );
                }
            }
            Some(FormKind::ScheduleK11065(k)) => {
                // Schedule K-1 (Form 1065) (26 U.S.C. §§ 702, 707(c); Schedule E Part I & Part II; Schedule 1 line 5):
                // Sum the partner's distributive share of ordinary pass-through income items:
                // box 1 (ordinary_business_income), box 2 (net_rental_real_estate_income),
                // box 3 (other_net_rental_income), box 4c (total_guaranteed_payments), and
                // box 7 (royalties).
                let gross = model::value(&k.ordinary_business_income)
                    + model::value(&k.net_rental_real_estate_income)
                    + model::value(&k.other_net_rental_income)
                    + model::value(&k.total_guaranteed_payments)
                    + model::value(&k.royalties);
                x = Figures {
                    gross,
                    taxable: gross,
                    taxable_set: true,
                    ..Default::default()
                };
                self.notes.leave_out(
                    "Partner basis limits, at-risk limits, and passive activity loss limitations for Schedule K-1 (Form 1065)",
                );
            }
            Some(FormKind::ScheduleK11120S(k)) => {
                // Schedule K-1 (Form 1120-S) (26 U.S.C. § 1366; Schedule E Part I & Part II; Schedule 1 line 5):
                // Sum the S corporation shareholder's pro rata share of ordinary pass-through
                // income items: box 1 (ordinary_business_income), box 2 (net_rental_real_estate_income),
                // box 3 (other_net_rental_income), and box 6 (royalties).
                let gross = model::value(&k.ordinary_business_income)
                    + model::value(&k.net_rental_real_estate_income)
                    + model::value(&k.other_net_rental_income)
                    + model::value(&k.royalties);
                x = Figures {
                    gross,
                    taxable: gross,
                    taxable_set: true,
                    ..Default::default()
                };
                self.notes.leave_out(
                    "Shareholder basis limits, at-risk limits, and passive activity loss limitations for Schedule K-1 (Form 1120-S)",
                );
            }
            Some(FormKind::ScheduleK11041(k)) => {
                // Schedule K-1 (Form 1041) (26 U.S.C. §§ 652, 662; Schedule E Part III; Schedule 1 line 5):
                // Sum the beneficiary's share of ordinary estate or trust income items:
                // box 5 (ordinary_business_income), box 6 (net_rental_real_estate_income),
                // box 7 (other_rental_income), and box 8 (other_portfolio_and_nonbusiness_income).
                let gross = model::value(&k.ordinary_business_income)
                    + model::value(&k.net_rental_real_estate_income)
                    + model::value(&k.other_rental_income)
                    + model::value(&k.other_portfolio_and_nonbusiness_income);
                x = Figures {
                    gross,
                    taxable: gross,
                    taxable_set: true,
                    ..Default::default()
                };
                self.notes.leave_out(
                    "Beneficiary basis limits and passive activity loss limitations for Schedule K-1 (Form 1041)",
                );
            }
            None => {}
        }
        (x, d)
    }

    fn evidence_total(&self) -> Figures {
        let mut t = Figures::default();
        for it in &self.items {
            if it.status == ItemStatus::ITEM_STATUS_MADE {
                t = t.plus(&read(figures_of(it)));
            }
        }
        t
    }

    /// Adds the YearAmount and one-offs, unless a whole-year form stands
    /// them aside.
    fn assumed(&mut self) -> Result<(), Error> {
        let m = self.m;
        let expected = ItemStatus::ITEM_STATUS_EXPECTED;
        for p in &m.parts {
            match &p.part {
                Some(Part::YearAmount(_)) => {
                    if !self.forms.is_empty() {
                        self.stand_aside(
                            &p.id,
                            "A form states this source's year, so the total for the year counts for nothing.",
                        );
                        continue;
                    }
                    self.year_amount(p);
                }
                Some(Part::OneOff(o)) => {
                    if !o.actual.is.is_empty() {
                        continue;
                    }
                    if self.form_not_listed(&o.actual) {
                        self.stand_aside(
                            &p.id,
                            "A form states this source's year. Is your planned payment on it?",
                        );
                        continue;
                    }
                    let d = match civil::from_field(&o.date) {
                        Ok(d) if !d.is_zero() => d,
                        _ => {
                            return Err(Error::Expansion(format!(
                                "{}.parts[{}].one_off.date is not a day",
                                self.path(),
                                p.id
                            )));
                        }
                    };
                    let mut x = Figures::default();
                    if let Some(Event::Distribution(dist)) = &o.event {
                        x = Figures {
                            gross: model::value(&dist.gross),
                            federal: model::value(&dist.federal_withheld),
                            ..Default::default()
                        };
                        if dist.taxable.is_set() {
                            x.taxable = model::value(&dist.taxable);
                            x.taxable_set = true;
                        }
                        if !dist.code.is_empty() {
                            x.codes = vec![dist.code.clone()];
                        }
                    }
                    if let Some(Event::HomeSale(hs)) = &o.event {
                        // Planned home sale (26 U.S.C. §§ 1001, 1011, 121; Publication 523):
                        // Total cost combines adjusted basis and selling expenses; taxable gain is
                        // realized gain (gross - expenses, floored at zero per § 165(c)) less any
                        // § 121 exclusion, floored at zero.
                        let gross = model::value(&hs.price);
                        let expenses = model::value(&hs.basis) + model::value(&hs.selling_expenses);
                        let gain = (gross - expenses).max(USCents(0));
                        let taxable = (gain - model::value(&hs.excluded_amount)).max(USCents(0));
                        x = Figures {
                            gross,
                            taxable,
                            expenses,
                            taxable_set: true,
                            ..Default::default()
                        };
                    }
                    self.add(
                        model::item_id(&[&self.prefix(), &d.to_string(), "payment", &p.id]),
                        d,
                        expected,
                        Basis::BASIS_ASSUMED,
                        &p.id,
                        &[],
                        &x,
                    );
                }
                _ => {}
            }
        }
        Ok(())
    }

    /// Reports whether a form states the year and the answer doesn't list
    /// every form as a different payment.
    fn form_not_listed(&self, a: &Actual) -> bool {
        let not: HashSet<&str> = a.is_not.iter().map(|s| s.as_str()).collect();
        self.forms.iter().any(|f| !not.contains(f.as_str()))
    }

    fn stand_aside(&mut self, part: &str, why: &str) {
        let prefix = self.prefix();
        self.notes.ask(model::new_question(
            model::item_id(&[&prefix, "stands-aside", part]),
            prefix.clone(),
            why,
            "This change counts for nothing until it is changed or removed.",
            format!("{}.parts[{part}]", self.path()),
            &["Remove it"],
        ));
    }

    /// Adds the year's figures less the evidence.
    fn year_amount(&mut self, p: &OtherIncomePart) {
        let Some(Part::YearAmount(y)) = &p.part else {
            return;
        };
        let have = self.evidence_total();
        let mut want = Figures {
            gross: model::value(&y.gross),
            federal: model::value(&y.federal_withheld),
            expenses: model::value(&y.expenses),
            ..Default::default()
        };
        if let Some(r) = y.rental_expenses.as_option() {
            want.expenses = rental_expenses(r);
        }
        if y.taxable.is_set() {
            want.taxable = model::value(&y.taxable);
            want.taxable_set = true;
        }
        if have.gross > want.gross {
            self.stand_aside(
                &p.id,
                &format!(
                    "The evidence so far, {}, is more than your total for the year, {}.",
                    have.gross, want.gross
                ),
            );
            return;
        }
        let mut left = Figures {
            gross: want.gross - have.gross,
            federal: (want.federal - have.federal).max(USCents(0)),
            expenses: (want.expenses - have.expenses).max(USCents(0)),
            ..Default::default()
        };
        if want.taxable_set {
            left.taxable = (want.taxable - have.taxable_or_gross()).max(USCents(0));
            left.taxable_set = true;
        }
        let mut dates = vec![self.ctx.year_end()];
        if y.recurring {
            dates.clear();
            let start = civil::later(self.through, self.ctx.evidence_through());
            for m in 1..=12 {
                let mo = civil::end_of_month(self.ctx.year(), m);
                if mo.after(start) {
                    dates.push(mo);
                }
            }
            if dates.is_empty() {
                return;
            }
        }
        let n = dates.len() as i64;
        let (gross, taxable) = (model::share(left.gross, n), model::share(left.taxable, n));
        let (fed, exp) = (
            model::share(left.federal, n),
            model::share(left.expenses, n),
        );
        for (i, d) in dates.iter().enumerate() {
            let mut x = Figures {
                gross: gross[i],
                federal: fed[i],
                expenses: exp[i],
                ..Default::default()
            };
            if left.taxable_set {
                x.taxable = taxable[i];
                x.taxable_set = true;
            }
            let id = model::item_id(&[&self.prefix(), &d.to_string(), "payment", &p.id]);
            self.add(
                id,
                *d,
                ItemStatus::ITEM_STATUS_EXPECTED,
                Basis::BASIS_ASSUMED,
                &p.id,
                &[],
                &x,
            );
        }
    }

    fn projection(&mut self) -> OtherIncomeProjection {
        model::sort_items(&mut self.items);
        model::round_items(&mut self.items);
        let mut out = OtherIncomeProjection {
            id: self.m.id.clone(),
            items: self.items.clone(),
            so_far_through: self.through.field(),
            ..Default::default()
        };
        let (mut made, mut rest) = (Figures::default(), Figures::default());
        for it in &self.items {
            if it.status == ItemStatus::ITEM_STATUS_MADE {
                made = made.plus(&read(figures_of(it)));
            } else {
                rest = rest.plus(&read(figures_of(it)));
            }
        }
        let year = made.clone().plus(&rest);
        let totals = |f: &Figures| OtherIncomeTotals {
            items: f.proto().into(),
            boxes: f.proto().into(),
            ..Default::default()
        };
        out.so_far = totals(&made).into();
        out.rest = totals(&rest).into();
        out.year = totals(&year).into();
        if self.forms.is_empty() && !self.items.is_empty() {
            let form = form_for(self.m.kind.as_known().unwrap_or_default());
            if !form.is_empty() {
                out.expected = vec![ExpectedDocument {
                    form: form.to_string(),
                    due: model::forms_due(self.ctx.year()).field(),
                    ..Default::default()
                }];
            }
        }
        out
    }
}
