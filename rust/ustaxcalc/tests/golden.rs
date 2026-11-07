//! Every exported calculator against the vectors golden/ustaxcalc_test.go
//! wrote to golden/testdata/ustaxcalc: the table cases of the Go tests and a
//! sweep of years, filing statuses and amounts, errors included.

#![allow(clippy::unwrap_used)]

use estimated::{Month, must_date};
use estimatedtest::{assert_same_json, read_gzip};
use filing::Status;
use money::USCents;
use serde_json::{Value, json};
use taxcalcs_ustaxcalc as us;
use us::{
    CapitalGains, CapitalNet, Conditions, Deduction, Election, Error, GainsTax, IraContribution,
    IraLimits, Ladder, RetirementDistribution, SafeHarbour, Seca, StateRefund, Table, TaxWithGains,
};

fn cents(a: &Value, k: &str) -> USCents {
    USCents(a[k].as_i64().unwrap_or_else(|| panic!("{k} in {a}")))
}

fn int(a: &Value, k: &str) -> i64 {
    a[k].as_i64().unwrap_or_else(|| panic!("{k} in {a}"))
}

fn flag(a: &Value, k: &str) -> bool {
    a[k].as_bool().unwrap_or_else(|| panic!("{k} in {a}"))
}

fn year(a: &Value) -> i64 {
    int(a, "year")
}

fn status(a: &Value) -> Status {
    Status::from_i32(int(a, "status") as i32)
}

fn err_json(e: &Error) -> Value {
    json!({"error": e.to_string(), "no_table": e.is(&Error::NoTable)})
}

fn built<T>(r: Result<T, Error>, value: impl Fn(&T) -> Value) -> Value {
    match r {
        Ok(v) => json!({"value": value(&v)}),
        Err(e) => err_json(&e),
    }
}

/// A working result as golden/estimated_test.go's resultJSON writes it.
fn result<T: Clone>(r: Result<working::Result<T>, Error>, value: impl Fn(&T) -> Value) -> Value {
    let r = match r {
        Ok(r) => r,
        Err(e) => return err_json(&e),
    };
    assert!(r.divergences().is_empty());
    let applied: Vec<Value> = r
        .applied()
        .iter()
        .map(|a| json!({"cite": a.cite(), "applied": a.applied()}))
        .collect();
    let required: Vec<Value> = r
        .required()
        .iter()
        .map(|q| {
            json!({"position": q.position().to_string(), "because": q.because(),
                "question": q.question(), "effect": q.effect(), "asked": q.asked()})
        })
        .collect();
    json!({"value": value(&r.value()), "applied": applied, "required": required,
        "divergences": [], "needs": r.needs()})
}

fn c(a: &USCents) -> Value {
    json!(a.0)
}

fn table_json(t: &Table) -> Value {
    let brackets: Vec<Value> = t
        .ordinary
        .iter()
        .map(|b| json!({"over": b.over.0, "rate": b.rate.0}))
        .collect();
    json!({
        "year": t.year, "status": t.status as i32, "ordinary": brackets,
        "standard_deduction": t.standard_deduction.0, "additional_aged_blind": t.additional_aged_blind.0,
        "max_zero_rate_amount": t.max_zero_rate_amount.0, "max_15_rate_amount": t.max_15_rate_amount.0,
        "social_security_wage_base": t.social_security_wage_base.0,
        "additional_medicare_threshold": t.additional_medicare_threshold.0,
        "prior_year_agi_step": t.prior_year_agi_step.0,
    })
}

fn ladder_json(l: &Ladder) -> Value {
    let bands: Vec<Value> = l
        .bands
        .iter()
        .map(|b| {
            json!({"rate": b.rate.0, "from": b.from.0, "to": b.to.0, "income": b.income.0, "tax": b.tax.0})
        })
        .collect();
    json!({"bands": bands, "total": l.total.0, "marginal": l.marginal.0, "room_in_bracket": l.room_in_bracket.0})
}

fn gains_json(g: &GainsTax) -> Value {
    json!({"at_zero": g.at_zero.0, "at_15": g.at_15.0, "at_20": g.at_20.0, "total": g.total.0})
}

fn capital_gains_json(g: &CapitalGains) -> Value {
    json!({"short_term": g.short_term().0, "long_term": g.long_term().0,
        "short_term_carryover": g.short_term_carryover().0, "long_term_carryover": g.long_term_carryover().0,
        "distributions": g.distributions().0})
}

fn capital_gains(a: &Value) -> Result<CapitalGains, Error> {
    us::new_capital_gains()
        .short_term(cents(a, "short_term"))
        .long_term(cents(a, "long_term"))
        .short_term_carryover(cents(a, "short_term_carryover"))
        .long_term_carryover(cents(a, "long_term_carryover"))
        .distributions(cents(a, "distributions"))
        .build()
}

fn capital_net_json(n: &CapitalNet) -> Value {
    json!({"short_term": n.short_term().0, "long_term": n.long_term().0, "combined": n.combined().0,
        "in_income": n.in_income().0, "net_capital_gain": n.net_capital_gain().0})
}

fn tax_with_gains_json(t: &TaxWithGains) -> Value {
    json!({"ordinary_income": t.ordinary_income().0, "preferential_income": t.preferential_income().0,
        "ladder": ladder_json(&t.ladder()), "gains": gains_json(&t.gains()),
        "all_ordinary": t.all_ordinary().0, "total": t.total().0})
}

fn conditions(a: &Value) -> Conditions {
    Conditions {
        aged_65_or_older: flag(a, "aged_65_or_older"),
        blind: flag(a, "blind"),
        spouse_aged_65_or_older: flag(a, "spouse_aged_65_or_older"),
        spouse_blind: flag(a, "spouse_blind"),
    }
}

fn deduction_json(d: &Deduction) -> Value {
    json!({"amount": d.amount.0, "itemised": d.itemised, "standard": d.standard.0})
}

fn seca_json(s: &Seca) -> Value {
    json!({"net_earnings": s.net_earnings.0, "social_security": s.social_security.0,
        "medicare": s.medicare.0, "additional_medicare": s.additional_medicare.0, "total": s.total.0,
        "above_the_line_deduction": s.above_the_line_deduction.0})
}

fn harbour_json(h: &SafeHarbour) -> Value {
    json!({"required": h.required.0, "current_year_test": h.current_year_test.0,
        "prior_year_test": h.prior_year_test.0, "prior_year_rate": h.prior_year_rate.0})
}

fn ira_contribution(a: &Value) -> Result<IraContribution, Error> {
    let mut b = us::new_ira_contribution(cents(a, "amount"));
    b.roth(cents(a, "roth"));
    if flag(a, "catch_up") {
        b.catch_up();
    }
    if flag(a, "lived_apart") {
        b.lived_apart();
    }
    if let Some(v) = a["covered"].as_bool() {
        b.active_participant(v);
    }
    if let Some(v) = a["spouse_covered"].as_bool() {
        b.spouse_active_participant(v);
    }
    b.build()
}

fn ira_json(c: &IraContribution) -> Value {
    let (covered, answered) = c.active_participant();
    let (spouse, spouse_answered) = c.spouse_active_participant();
    json!({"amount": c.amount().0, "roth": c.roth().0, "catch_up": c.catch_up(),
        "covered": covered, "covered_answered": answered, "spouse_covered": spouse,
        "spouse_answered": spouse_answered, "lived_apart": c.lived_apart()})
}

fn ira_limits_json(l: &IraLimits) -> Value {
    json!({"combined": l.combined().0, "roth_limit": l.roth_limit().0,
        "roth_allowed": l.roth_allowed().0, "deduction": l.deduction().0,
        "traditional_excess": l.traditional_excess().0, "roth_excess": l.roth_excess().0,
        "excess_tax": l.excess_tax().0})
}

fn distributions(a: &Value) -> Vec<RetirementDistribution> {
    a["distributions"]
        .as_array()
        .unwrap()
        .iter()
        .map(|d| {
            us::new_retirement_distribution(cents(d, "taxable"), d["code"].as_str().unwrap())
                .unwrap()
        })
        .collect()
}

fn state_refund(a: &Value) -> Result<StateRefund, Error> {
    let mut b = us::new_state_refund(cents(a, "refund"), status(a));
    b.prior_income_tax(cents(a, "prior_income_tax"))
        .prior_salt(cents(a, "prior_salt_paid"), cents(a, "prior_salt_deducted"))
        .prior_itemized(cents(a, "prior_itemized"))
        .prior_boxes(int(a, "prior_boxes"));
    if flag(a, "spouse_itemized") {
        b.spouse_itemized_separately();
    }
    b.build()
}

fn strings(v: Vec<String>) -> Value {
    json!(v)
}

/// What the Rust port gives for one case.
fn answer(f: &str, a: &Value) -> Value {
    match f {
        "TableFor" => built(us::table_for(year(a), status(a)), table_json),
        "Years" => json!(us::years()),
        "Dollars" => json!(us::dollars(int(a, "d")).0),
        "Percent" => json!(us::percent(int(a, "p") as i32).0),
        "OrdinaryIncomeTax" => result(
            us::ordinary_income_tax(year(a), status(a), cents(a, "income")),
            ladder_json,
        ),
        "EffectiveRate" => json!(us::effective_rate(cents(a, "tax"), cents(a, "income")).0),
        "LongTermGainsTax" => result(
            us::long_term_gains_tax(year(a), status(a), cents(a, "ordinary"), cents(a, "gain")),
            gains_json,
        ),
        "CapitalGainsBuilder.Build" => built(capital_gains(a), capital_gains_json),
        "NetCapitalGains" => result(
            us::net_capital_gains(year(a), status(a), capital_gains(a).unwrap()),
            capital_net_json,
        ),
        "TaxOnIncome" => result(
            us::tax_on_income(
                year(a),
                status(a),
                cents(a, "taxable"),
                cents(a, "qualified_dividends"),
                cents(a, "net_capital_gain"),
            ),
            tax_with_gains_json,
        ),
        "Aged65OrOlder" => {
            let born: Vec<i64> = a["born"]
                .as_str()
                .unwrap()
                .split('-')
                .map(|p| p.parse().unwrap())
                .collect();
            json!(us::aged_65_or_older(
                year(a),
                must_date(born[0], Month(born[1]), born[2])
            ))
        }
        "StandardDeduction" => result(us::standard_deduction(year(a), status(a), conditions(a)), c),
        "DeductionTaken" => {
            let e = match int(a, "election") {
                0 => Election::Larger,
                1 => Election::Standard,
                2 => Election::Itemise,
                n => panic!("election {n}"),
            };
            result(
                us::deduction_taken(
                    year(a),
                    status(a),
                    conditions(a),
                    cents(a, "itemised_total"),
                    e,
                ),
                deduction_json,
            )
        }
        "SelfEmploymentTax" => result(
            us::self_employment_tax(
                year(a),
                status(a),
                cents(a, "net_profit"),
                cents(a, "w2_social_security_wages"),
                cents(a, "other_medicare_wages"),
            ),
            seca_json,
        ),
        "RequiredAnnualPayment" => result(
            us::required_annual_payment(
                year(a),
                status(a),
                cents(a, "current_year_tax"),
                cents(a, "prior_year_tax"),
                cents(a, "prior_year_agi"),
                flag(a, "prior_year_filed"),
            ),
            harbour_json,
        ),
        "Instalments" => {
            let out: Vec<Value> = us::instalments(year(a), cents(a, "required_annual"))
                .iter()
                .map(|i| json!({"due": i.due.to_string(), "cumulative_target": i.cumulative_target.0}))
                .collect();
            json!(out)
        }
        "WithholdingCreditedBy" => {
            json!(us::withholding_credited_by(cents(a, "total_withholding"), int(a, "quarter")).0)
        }
        "IRAContributionBuilder.Build" => built(ira_contribution(a), ira_json),
        "IRADeduction" => result(
            us::ira_deduction(
                year(a),
                status(a),
                ira_contribution(a).unwrap(),
                cents(a, "magi"),
            ),
            c,
        ),
        "IRAContributionLimits" => result(
            us::ira_contribution_limits(
                year(a),
                status(a),
                ira_contribution(a).unwrap(),
                cents(a, "magi"),
            ),
            ira_limits_json,
        ),
        "ItemisedLimits" => result(
            us::itemised_limits(
                year(a),
                status(a),
                cents(a, "itemised"),
                cents(a, "charity"),
                cents(a, "agi"),
            ),
            c,
        ),
        "NewRetirementDistribution" => built(
            us::new_retirement_distribution(cents(a, "taxable"), a["code"].as_str().unwrap()),
            |d| json!({"taxable": d.taxable().0, "code": d.code()}),
        ),
        "EarlyDistributionTax" => result(us::early_distribution_tax(year(a), &distributions(a)), c),
        "EarlyDistributionTaxOther" => json!(us::early_distribution_tax_other(&distributions(a))),
        "UnhandledDistributionCodes" => {
            strings(us::unhandled_distribution_codes(&distributions(a)))
        }
        "DistributionIncome" => json!(us::distribution_income(&distributions(a)).0),
        "HandledDistributionCodes" => strings(us::handled_distribution_codes()),
        "RentalLossAllowed" => result(
            us::rental_loss_allowed(
                status(a),
                cents(a, "loss"),
                cents(a, "magi"),
                flag(a, "lived_apart"),
            ),
            c,
        ),
        "StateRefundBuilder.Build" => built(state_refund(a), |r| json!({"refund": r.refund().0})),
        "StateRefundTaxable" => result(
            us::state_refund_taxable(year(a), state_refund(a).unwrap()),
            c,
        ),
        _ => panic!("no Rust case for {f}"),
    }
}

fn check(name: &str) {
    let file = format!("ustaxcalc/{name}.json.gz");
    let v = read_gzip(&file);
    let cases = v["cases"].as_array().unwrap();
    assert!(!cases.is_empty());
    for (i, case) in cases.iter().enumerate() {
        let f = case["fn"].as_str().unwrap();
        let got = answer(f, &case["args"]);
        assert_same_json(
            &format!("{file} case {i}, {f}({})", case["args"]),
            &got,
            &case["out"],
        );
    }
}

#[test]
fn tables() {
    check("tables");
}

#[test]
fn ladder() {
    check("ladder");
}

#[test]
fn gains() {
    check("gains");
}

#[test]
fn capital() {
    check("capital");
}

#[test]
fn deductions() {
    check("deductions");
}

#[test]
fn seca() {
    check("seca");
}

#[test]
fn safeharbour() {
    check("safeharbour");
}

#[test]
fn ira() {
    check("ira");
}

#[test]
fn itemised() {
    check("itemised");
}

#[test]
fn additional() {
    check("additional");
}
