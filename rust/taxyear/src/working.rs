//! The working behind every figure: working.go.

use std::collections::HashMap;

use buffa::{Message, MessageName};
use buffa_descriptor::{DynamicMessage, Value};
use money::USCents;
use proto::scenariosv1::{
    __buffa::oneof::term::Of, Item, ItemStatus, ScenarioResult, Term, Working,
};

use crate::reflect::{self, MONEY};

/// term is one signed figure a line adds up, by path.
#[derive(Clone, Debug)]
struct TermPath {
    sign: i32,
    path: String,
}

fn plus(paths: &[&str]) -> Vec<TermPath> {
    paths
        .iter()
        .map(|p| TermPath {
            sign: 1,
            path: p.to_string(),
        })
        .collect()
}

fn minus(paths: &[&str]) -> Vec<TermPath> {
    paths
        .iter()
        .map(|p| TermPath {
            sign: -1,
            path: p.to_string(),
        })
        .collect()
}

fn both(a: Vec<TermPath>, b: Vec<TermPath>) -> Vec<TermPath> {
    let mut out = a;
    out.extend(b);
    out
}

/// formLines are the federal lines in the result that the forms define as
/// sums of other lines, each with its definitions in order: the first that
/// adds up to the line is its working. A line held to a floor or a limit
/// adds up under none, and keeps only the rules that made it. They are in
/// the order of their names, as Go sorts them.
fn form_lines() -> Vec<(&'static str, Vec<Vec<TermPath>>)> {
    let mut out: Vec<(&'static str, Vec<Vec<TermPath>>)> = vec![
        (
            "form_1040.total_wages",
            vec![plus(&[
                "form_1040.w2_wages",
                "form_1040.household_employee_wages",
                "form_1040.unreported_tip_income",
                "form_1040.medicaid_waiver_payments",
                "form_1040.taxable_dependent_care_benefits",
                "form_1040.employer_provided_adoption_benefits",
                "form_1040.wages_from_form_8919",
                "form_1040.other_earned_income",
            ])],
        ),
        (
            "form_1040.total_income",
            vec![plus(&[
                "form_1040.total_wages",
                "form_1040.taxable_interest",
                "form_1040.ordinary_dividends",
                "form_1040.ira_distributions_taxable",
                "form_1040.pensions_and_annuities_taxable",
                "form_1040.social_security_benefits_taxable",
                "form_1040.capital_gain_or_loss",
                "form_1040.additional_income",
            ])],
        ),
        (
            "form_1040.additional_income",
            vec![plus(&["schedule_1.total_additional_income"])],
        ),
        (
            "form_1040.adjustments_to_income",
            vec![plus(&["schedule_1.total_adjustments"])],
        ),
        (
            "form_1040.deduction",
            vec![plus(&["schedule_a.total_itemized_deductions"])],
        ),
        (
            "form_1040.total_deductions",
            vec![plus(&[
                "form_1040.deduction",
                "form_1040.qualified_business_income_deduction",
                "form_1040.additional_deductions",
            ])],
        ),
        (
            "form_1040.adjusted_gross_income",
            vec![both(
                plus(&["form_1040.total_income"]),
                minus(&["form_1040.adjustments_to_income"]),
            )],
        ),
        (
            "form_1040.taxable_income",
            vec![both(
                plus(&["form_1040.adjusted_gross_income"]),
                minus(&["form_1040.total_deductions"]),
            )],
        ),
        (
            "form_1040.capital_gain_or_loss",
            vec![plus(&["schedule_d.net_gain_or_loss"])],
        ),
        (
            "form_1040.total_withheld",
            vec![plus(&[
                "form_1040.withheld_from_w2",
                "form_1040.withheld_from_1099",
                "form_1040.withheld_from_other_forms",
            ])],
        ),
        (
            "form_1040.other_taxes",
            vec![plus(&["schedule_2.total_part_two"])],
        ),
        (
            "form_1040.total_tax",
            vec![
                plus(&["form_1040.tax_after_credits", "form_1040.other_taxes"]),
                plus(&["form_1040.tax", "form_1040.other_taxes"]),
            ],
        ),
        (
            "form_1040.total_payments",
            vec![
                plus(&[
                    "form_1040.total_withheld",
                    "form_1040.estimated_tax_payments",
                    "form_1040.total_other_payments",
                ]),
                plus(&[
                    "form_1040.total_withheld",
                    "form_1040.estimated_tax_payments",
                    "schedule_3.excess_social_security_withheld",
                ]),
            ],
        ),
        (
            "form_1040.amount_owed",
            vec![
                both(
                    plus(&["form_1040.total_tax", "form_1040.estimated_tax_penalty"]),
                    minus(&["form_1040.total_payments"]),
                ),
                both(
                    plus(&["form_1040.total_tax"]),
                    minus(&["form_1040.total_payments"]),
                ),
            ],
        ),
        (
            "form_1040.overpaid",
            vec![
                both(
                    plus(&["form_1040.total_payments"]),
                    minus(&["form_1040.total_tax", "form_1040.estimated_tax_penalty"]),
                ),
                both(
                    plus(&["form_1040.total_payments"]),
                    minus(&["form_1040.total_tax"]),
                ),
            ],
        ),
        (
            "schedule_1.total_additional_income",
            vec![plus(&[
                "schedule_1.taxable_state_local_refunds",
                "schedule_1.alimony_received",
                "schedule_1.business_income_or_loss",
                "schedule_1.other_gains_or_losses",
                "schedule_1.rental_royalty_partnership_s_corp_trust",
                "schedule_1.farm_income_or_loss",
                "schedule_1.unemployment_compensation",
                "schedule_1.total_other_income",
            ])],
        ),
        (
            "schedule_1.total_adjustments",
            vec![plus(&[
                "schedule_1.educator_expenses",
                "schedule_1.reservist_performing_artist_official_expenses",
                "schedule_1.health_savings_account_deduction",
                "schedule_1.armed_forces_moving_expenses",
                "schedule_1.deductible_part_of_self_employment_tax",
                "schedule_1.self_employed_retirement_plans",
                "schedule_1.self_employed_health_insurance",
                "schedule_1.early_withdrawal_penalty",
                "schedule_1.alimony_paid",
                "schedule_1.ira_deduction",
                "schedule_1.student_loan_interest_deduction",
                "schedule_1.archer_msa_deduction",
                "schedule_1.total_other_adjustments",
            ])],
        ),
        (
            "schedule_a.total_taxes_paid",
            vec![plus(&[
                "schedule_a.state_and_local_taxes_after_cap",
                "schedule_a.other_taxes",
            ])],
        ),
        (
            "schedule_a.total_itemized_deductions",
            vec![plus(&[
                "schedule_a.deductible_medical_and_dental",
                "schedule_a.total_taxes_paid",
                "schedule_a.total_interest_paid",
                "schedule_a.total_gifts_to_charity",
                "schedule_a.casualty_and_theft_losses",
            ])],
        ),
    ];
    out.sort_by(|a, b| a.0.cmp(b.0));
    out
}

/// projectionLines are federal lines that add up one year figure from each
/// model of a list, or for tax paid, from the US model: the payments made,
/// so far's, as the return counts only those. In the order of their names.
fn projection_lines() -> Vec<(&'static str, &'static str, &'static [&'static str])> {
    let mut out: Vec<(&'static str, &'static str, &'static [&'static str])> = vec![
        (
            "form_1040.taxable_interest",
            "accounts",
            &["boxes.interest", "boxes.treasury_interest"],
        ),
        (
            "form_1040.ordinary_dividends",
            "accounts",
            &["boxes.ordinary_dividends"],
        ),
        (
            "form_1040.qualified_dividends",
            "accounts",
            &["boxes.qualified_dividends"],
        ),
        (
            "form_1040.tax_exempt_interest",
            "accounts",
            &["boxes.tax_exempt_interest"],
        ),
        (
            "form_1040.withheld_from_w2",
            "employers",
            &["boxes.federal_withheld"],
        ),
        (
            "form_1040.withheld_from_1099",
            "other_income",
            &["boxes.federal_withheld"],
        ),
        (
            "form_1040.estimated_tax_payments",
            "tax_paid",
            &["so_far.counted"],
        ),
    ];
    out.sort_by(|a, b| a.0.cmp(b.0));
    out
}

/// addWorkings gives every figure in the result a working: each federal
/// line the forms define as a sum, and every figure of every model's
/// projection, whose year is its so far and its rest, and whose so far and
/// rest add up their items. A working is kept only when its terms add up to
/// the figure; the figures that already have one keep it. kept holds the
/// workings of the models the cache finished, by projection path; the
/// jobs' and tax paid's are worked out here.
pub(crate) fn add_workings(res: &mut ScenarioResult, kept: &HashMap<String, Vec<Working>>) {
    let proj = res.projection.as_option().and_then(reflect::to_dyn);
    let mut w = indexed(res, proj.as_ref(), works_out);
    for (line, defs) in form_lines() {
        for def in defs {
            if w.add(&format!("federal.{line}"), &prefixed("federal.", &def), &[]) {
                break;
            }
        }
    }
    for (line, list, figures) in projection_lines() {
        let mut ts = Vec::new();
        for id in projection_ids(proj.as_ref(), list, "US") {
            for f in figures {
                // A figure names its period when it isn't the year's.
                let f = if f.contains("so_far.") {
                    f.to_string()
                } else {
                    format!("year.{f}")
                };
                ts.push(TermPath {
                    sign: 1,
                    path: format!("projection.{list}[{id}].{f}"),
                });
            }
        }
        w.add(&format!("federal.{line}"), &ts, &[]);
    }
    if let Some(p) = proj.as_ref() {
        w.projections(p, kept);
    }
    res.working = w.list;
}

/// modelWorkings is the workings of one model's figures, at its path.
pub(crate) fn model_workings<P: Message + MessageName>(at: &str, p: &P) -> Vec<Working> {
    let mut w = Workings::default();
    let Some(d) = reflect::to_dyn(p) else {
        return Vec::new();
    };
    w.index_periods(at, &d, true);
    w.periods(at, &d);
    w.list
}

fn prefixed(prefix: &str, ts: &[TermPath]) -> Vec<TermPath> {
    ts.iter()
        .map(|t| TermPath {
            sign: t.sign,
            path: format!("{prefix}{}", t.path),
        })
        .collect()
}

/// workings is a result's workings being added to, by path, with every
/// money figure the workings read, by path.
#[derive(Default)]
pub(crate) struct Workings {
    list: Vec<Working>,
    /// The index in list of the working of each path, the last for a path
    /// held twice.
    by_path: HashMap<String, usize>,
    pub(crate) amounts: HashMap<String, USCents>,
}

/// newWorkings indexes every money figure of the result's return and
/// projections, as the builder and the tests read them.
#[cfg(test)]
pub(crate) fn new_workings(res: &ScenarioResult) -> Workings {
    let proj = res.projection.as_option().and_then(reflect::to_dyn);
    indexed(res, proj.as_ref(), |_| true)
}

/// worksOut reports whether addWorkings works out a list's model workings,
/// rather than the cache's finished models holding them.
fn works_out(list: &str) -> bool {
    list == "employers" || list == "tax_paid"
}

fn indexed(
    res: &ScenarioResult,
    proj: Option<&DynamicMessage>,
    all_periods: fn(&str) -> bool,
) -> Workings {
    let mut w = Workings {
        list: res.working.clone(),
        ..Workings::default()
    };
    for (i, x) in w.list.iter().enumerate() {
        w.by_path.insert(x.path.clone(), i);
    }
    if let Some(f) = res.federal.as_option().and_then(reflect::to_dyn) {
        w.index("federal", &f, 2);
    }
    // The summary's own figures, such as the itemised limits, and each
    // entry of its lists by id or person.
    if let Some(sum) = res.summary.as_option().and_then(reflect::to_dyn) {
        w.index("summary", &sum, 1);
        for list in reflect::set_fields(&sum) {
            if !reflect::is_list(list) || !reflect::is_message_kind(list) {
                continue;
            }
            for e in reflect::list(&sum, list)
                .iter()
                .filter_map(reflect::as_message)
            {
                for k in ["id", "person"] {
                    if let Some(kf) = reflect::field_of(e, k) {
                        let at = format!("summary.{}[{}]", list.name(), reflect::string(e, kf));
                        w.index(&at, e, 1);
                    }
                }
            }
        }
    }
    if let Some(proj) = proj {
        for list in reflect::set_fields(proj) {
            for p in reflect::list(proj, list)
                .iter()
                .filter_map(reflect::as_message)
            {
                let at = format!(
                    "projection.{}[{}]",
                    list.name(),
                    reflect::string_named(p, "id")
                );
                w.index_periods(&at, p, all_periods(list.name()));
                // A projection's own figures, such as a job's hsa_over.
                w.index(&at, p, 1);
            }
        }
    }
    w
}

impl Workings {
    /// indexPeriods indexes one projection's year, and with all, its so
    /// far and rest.
    fn index_periods(&mut self, at: &str, p: &DynamicMessage, all: bool) {
        for period in ["so_far", "rest", "year"] {
            if !all && period != "year" {
                continue;
            }
            if let Some(m) = reflect::message_named(p, period) {
                self.index(&format!("{at}.{period}"), m, 2);
            }
        }
    }

    /// index records the money figures in m, depth messages deep, by path.
    /// A path into a list is not recorded.
    fn index(&mut self, at: &str, m: &DynamicMessage, depth: i32) {
        for fd in reflect::set_fields(m) {
            if !reflect::is_message(fd) {
                continue;
            }
            let Some(sub) = reflect::message(m, fd) else {
                continue;
            };
            let path = format!("{at}.{}", fd.name());
            if reflect::full_name(sub) == MONEY {
                self.amounts.insert(path, USCents(reflect::minor(sub)));
            } else if depth > 1 {
                self.index(&path, sub, depth - 1);
            }
        }
    }

    /// add records a figure's terms, the set ones of ts by path and items
    /// by id, when the figure is set, has no terms yet, and the path terms
    /// add up to it. It reports whether the figure has its terms now.
    fn add(&mut self, path: &str, ts: &[TermPath], items: &[String]) -> bool {
        let Some(&total) = self.amounts.get(path) else {
            return false;
        };
        if let Some(&i) = self.by_path.get(path)
            && !self.list[i].terms.is_empty()
        {
            return true;
        }
        let mut sum = USCents(0);
        let mut terms = Vec::new();
        for t in ts {
            let Some(&v) = self.amounts.get(&t.path) else {
                continue;
            };
            sum += USCents(i64::from(t.sign) * v.0);
            terms.push(Term {
                of: Some(Of::Path(t.path.clone())),
                sign: t.sign,
                ..Default::default()
            });
        }
        if (terms.is_empty() && items.is_empty()) || (!terms.is_empty() && sum != total) {
            return false;
        }
        for it in items {
            terms.push(Term {
                of: Some(Of::Item(it.clone())),
                sign: 1,
                ..Default::default()
            });
        }
        let i = match self.by_path.get(path) {
            Some(&i) => i,
            None => {
                self.list.push(Working {
                    path: path.to_string(),
                    ..Default::default()
                });
                let i = self.list.len() - 1;
                self.by_path.insert(path.to_string(), i);
                i
            }
        };
        self.list[i].terms = terms;
        true
    }

    /// projections adds the workings of every model's figures, in the
    /// projection's order: the jobs' and tax paid's worked out here, each
    /// other model's as the cache finished it.
    fn projections(&mut self, proj: &DynamicMessage, kept: &HashMap<String, Vec<Working>>) {
        for list in reflect::set_fields(proj) {
            for p in reflect::list(proj, list)
                .iter()
                .filter_map(reflect::as_message)
            {
                let at = format!(
                    "projection.{}[{}]",
                    list.name(),
                    reflect::string_named(p, "id")
                );
                if works_out(list.name()) {
                    self.periods(&at, p);
                    continue;
                }
                if let Some(ws) = kept.get(&at) {
                    self.list.extend(ws.iter().cloned());
                }
            }
        }
    }

    /// periods adds one model's workings.
    fn periods(&mut self, at: &str, p: &DynamicMessage) {
        let Some(year) = reflect::message_named(p, "year") else {
            return;
        };
        for f in money_fields(year, "") {
            self.add(
                &format!("{at}.year.{f}"),
                &plus(&[&format!("{at}.so_far.{f}"), &format!("{at}.rest.{f}")]),
                &[],
            );
        }
        let carried = carrying(reflect::list_named(p, "items"));
        for period in ["so_far", "rest"] {
            let Some(m) = reflect::message_named(p, period) else {
                continue;
            };
            for f in money_fields(m, "") {
                if let Some(ids) = carried.get(&format!("{period}.{f}"))
                    && !ids.is_empty()
                {
                    self.add(&format!("{at}.{period}.{f}"), &[], ids);
                }
            }
        }
    }
}

/// projectionIDs lists a projection list's model ids, in order: of a list
/// whose projections name a jurisdiction, only that jurisdiction's.
fn projection_ids(proj: Option<&DynamicMessage>, list: &str, jurisdiction: &str) -> Vec<String> {
    let Some(proj) = proj else {
        return Vec::new();
    };
    let mut out = Vec::new();
    for m in reflect::list_named(proj, list)
        .iter()
        .filter_map(reflect::as_message)
    {
        if let Some(j) = reflect::field_of(m, "jurisdiction")
            && reflect::string(m, j) != jurisdiction
        {
            continue;
        }
        out.push(reflect::string_named(m, "id").to_string());
    }
    out
}

/// moneyFields lists the paths of the set money fields in a totals
/// message, one message deep, such as "items.gross" or "counted".
fn money_fields(m: &DynamicMessage, prefix: &str) -> Vec<String> {
    let mut out = Vec::new();
    for fd in reflect::set_fields(m) {
        if !reflect::is_message(fd) {
            continue;
        }
        let name = format!("{prefix}{}", fd.name());
        if reflect::message_name(fd) == MONEY {
            out.push(name);
        } else if prefix.is_empty()
            && let Some(sub) = reflect::message(m, fd)
        {
            out.extend(money_fields(sub, &format!("{name}.")));
        }
    }
    out
}

/// carrying maps each so far or rest item total, such as
/// "so_far.items.gross", to the ids of the items that add to it: the made
/// items for so far and the expected for the rest, each with the figure
/// set. A total that is itself money, as an itemised model's "items", adds
/// every item, and tax paid's "counted" every payment that counts. Box
/// figures are worked out from the item totals, so no item carries them.
fn carrying(items: &[Value]) -> HashMap<String, Vec<String>> {
    let mut out: HashMap<String, Vec<String>> = HashMap::new();
    for m in items.iter().filter_map(reflect::as_message) {
        let it: Item = reflect::from_dyn(m).unwrap_or_default();
        let period = if it.status.as_known() == Some(ItemStatus::ITEM_STATUS_MADE) {
            "so_far."
        } else {
            "rest."
        };
        out.entry(format!("{period}items"))
            .or_default()
            .push(it.id.clone());
        if let Some(proto::scenariosv1::__buffa::oneof::item::Figures::Payment(p)) = &it.figures
            && p.counts
        {
            out.entry(format!("{period}counted"))
                .or_default()
                .push(it.id.clone());
        }
        let Some(which) = reflect::which_named(m, "figures") else {
            continue;
        };
        let Some(fig) = reflect::message(m, which) else {
            continue;
        };
        for fd in reflect::set_fields(fig) {
            out.entry(format!("{period}items.{}", fd.name()))
                .or_default()
                .push(it.id.clone());
        }
    }
    out
}
