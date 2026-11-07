//! The golden vectors: every answer the rules give the households, as JSON,
//! in the shape golden/estimated_test.go's `estimatedAnswers` writes.
//!
//! Amounts are integer cents, rates integer basis points (and millionths
//! for a fine rate), dates YYYY-MM-DD with "" for the zero Date, enums the
//! Go `String()` text, and every other text verbatim. A method's error is
//! `{"error": <text>}` in place of its result.

use std::fmt::Display;

use estimated::{
    Date, EstimatedTax, Explanation, Facts, Figures, Installment, Jurisdiction, Penalty,
    PenaltyLine, PenaltyPart, Requirement, TargetOption, WaiverOutcome,
};
use serde_json::{Value, json};
use working::{Divergence, Required, Rule};

use crate::{Answers, Household, ask, check_answers, check_copy, households, texts};

/// The tax years every jurisdiction's vectors cover.
pub const GOLDEN_YEARS: [i64; 2] = [2025, 2026];

fn date(d: Date) -> Value {
    if d.is_zero() {
        json!("")
    } else {
        json!(d.to_string())
    }
}

fn rule(r: &Rule) -> Value {
    json!({"cite": r.cite(), "applied": r.applied()})
}

fn required(r: &Required) -> Value {
    json!({
        "position": r.position().to_string(),
        "because": r.because(),
        "question": r.question(),
        "effect": r.effect(),
        "asked": r.asked(),
    })
}

fn divergence(d: &Divergence) -> Value {
    json!({
        "rule": d.rule(),
        "form": d.form(),
        "form_source": d.form_source(),
        "statute": d.statute(),
        "statute_source": d.statute_source(),
        "uses": d.uses().to_string(),
        "affects_household": d.affects_household(),
    })
}

fn result<T: Clone>(
    r: &Result<working::Result<T>, estimated::Error>,
    value: impl Fn(&T) -> Value,
) -> Value {
    match r {
        Err(e) => json!({"error": e.to_string()}),
        Ok(r) => json!({
            "value": value(&r.value()),
            "applied": r.applied().iter().map(rule).collect::<Vec<_>>(),
            "required": r.required().iter().map(required).collect::<Vec<_>>(),
            "divergences": r.divergences().iter().map(divergence).collect::<Vec<_>>(),
            "needs": r.needs(),
        }),
    }
}

fn option(o: &TargetOption) -> Value {
    json!({
        "basis": o.basis().to_string(),
        "percent": o.percent().0,
        "amount": o.amount().0,
        "available": o.available(),
        "why_not": o.why_not(),
        "label": o.label(),
    })
}

fn requirement(r: &Requirement) -> Value {
    json!({
        "required": r.required(),
        "why": r.why(),
        "target": r.target().0,
        "exempt": r.exempt(),
        "target_known": r.target_known(),
        "options": r.options().iter().map(option).collect::<Vec<_>>(),
        "chosen": r.chosen().as_ref().map_or(Value::Null, option),
    })
}

fn installment(i: &Installment) -> Value {
    json!({
        "number": i.number(),
        "due": date(i.due()),
        "needed_by": i.needed_by().0,
        "credited_by": i.credited_by().0,
        "shortfall": i.shortfall().0,
        "status": i.status().to_string(),
    })
}

fn part(p: &PenaltyPart) -> Value {
    json!({
        "kind": p.kind().to_string(),
        "principal": p.principal().0,
        "from": date(p.from()),
        "to": date(p.to()),
        "days": p.days(),
        "months": p.months(),
        "rate": p.rate().0,
        "fine_rate": p.fine_rate().0,
        "charge": p.charge().0,
        "source": p.source(),
    })
}

fn line(l: &PenaltyLine) -> Value {
    json!({
        "installment": l.installment(),
        "due": date(l.due()),
        "shortfall": l.shortfall().0,
        "days": l.days(),
        "parts": l.parts().iter().map(part).collect::<Vec<_>>(),
        "total": l.total().0,
    })
}

fn outcome(o: &WaiverOutcome) -> Value {
    json!({
        "eligible": o.eligible(),
        "refused": o.refused(),
        "undecided": o.undecided(),
        "reason": o.reason(),
        "kept": o.kept().iter().map(|k| k.to_string()).collect::<Vec<_>>(),
    })
}

fn penalty(p: &Penalty) -> Value {
    json!({
        "reason": p.reason(),
        "total": p.total().0,
        "lines": p.lines().iter().map(line).collect::<Vec<_>>(),
        "waiver": p.waiver().to_string(),
        "as_of": date(p.as_of()),
        "upper_bound": p.upper_bound(),
        "rated_through": date(p.rated_through()),
        "waiver_outcome": p.waiver_outcome().as_ref().map_or(Value::Null, outcome),
        "if_waived": p.if_waived().0,
        "short_due_dates": p.short_due_dates(),
        "summary": p.summary(),
    })
}

fn explanation(e: &Explanation) -> Value {
    json!({
        "summary": e.summary(),
        "body": e.body(),
        "versus_federal": e.versus_federal(),
        "sources": e.sources(),
    })
}

fn answers(a: &Answers) -> Value {
    json!({
        "name": a.household.name,
        "requirement": result(&a.requirement, requirement),
        "schedule": result(&a.schedule, |is: &Vec<Installment>| {
            Value::Array(is.iter().map(installment).collect())
        }),
        "penalty": result(&a.penalty, penalty),
    })
}

/// Every answer `rules_for(year)` gives the households of `j`, for each of
/// [`GOLDEN_YEARS`], as the JSON in
/// `golden/testdata/estimated/<code>.json.gz`. A year whose rules cannot be
/// had records the error.
///
/// `rules_for` is a jurisdiction's constructor, as `estimated_tax`.
pub fn golden_answers<R, E, F>(j: &Jurisdiction, rules_for: F) -> Value
where
    R: EstimatedTax,
    E: Display,
    F: Fn(i64) -> Result<R, E>,
{
    let hs = households(j);
    let years: Vec<Value> = GOLDEN_YEARS
        .iter()
        .map(|&year| match rules_for(year) {
            Err(e) => json!({"year": year, "error": e.to_string()}),
            Ok(rules) => {
                let as_ = ask(&rules, &hs);
                json!({
                    "year": year,
                    "explanation": explanation(&rules.explanation()),
                    "households": as_.iter().map(answers).collect::<Vec<_>>(),
                    "answer_problems": check_answers(&as_),
                    "copy_problems": check_copy(&rules, &texts(Some(&rules as &dyn EstimatedTax), &as_)),
                })
            }
        })
        .collect();
    json!({"jurisdiction": j.as_str(), "years": years})
}

fn figures(f: &Figures) -> Value {
    let c = |a: Option<money::USCents>| a.map_or(Value::Null, |a| json!(a.0));
    json!({
        "tax": c(f.tax()),
        "federal_agi": c(f.federal_agi()),
        "state_agi": c(f.state_agi()),
        "gross_income": c(f.gross_income()),
        "taxable_income": c(f.taxable_income()),
        "local_tax": c(f.local_tax()),
        "withheld": c(f.withheld()),
    })
}

/// A household's facts, every accessor's value, as golden/testdata/
/// estimated/households.json.gz holds them.
pub fn facts_json(h: &Household) -> Value {
    let f: &Facts = &h.facts;
    let residence = |ps: Vec<estimated::ResidencePeriod>| {
        ps.iter()
            .map(|p| json!({"state": p.state(), "from": date(p.from()), "to": date(p.to())}))
            .collect::<Vec<_>>()
    };
    let pos = f.positions();
    json!({
        "name": h.name,
        "jurisdiction": f.jurisdiction().as_str(),
        "tax_year": f.tax_year(),
        "filing_status": f.filing_status().to_string(),
        "residence": residence(f.residence()),
        "prior_residence": residence(f.prior_residence()),
        "this_year": figures(&f.this_year()),
        "prior_year": f.prior_year().map_or(Value::Null, |p| json!({
            "filed": p.filed(),
            "not_required_to_file": p.not_required_to_file(),
            "months": p.months(),
            "figures": p.figures().as_ref().map_or(Value::Null, figures),
            "full_year_resident": p.full_year_resident(),
        })),
        "withholding": f.withholding().iter().map(|w| json!({"on": date(w.on()), "amount": w.amount().0})).collect::<Vec<_>>(),
        "payments": f.payments().iter().map(|p| json!({
            "on": date(p.on()), "amount": p.amount().0, "from_prior_year": p.from_prior_year(),
        })).collect::<Vec<_>>(),
        "return_filed": date(f.return_filed()),
        "requirement_first_met": date(f.requirement_first_met()),
        "people": f.people().iter().map(|p| json!({
            "age": p.age(), "retired_after_62": p.retired_after_62(), "disabled": p.disabled(),
        })).collect::<Vec<_>>(),
        "farm_share": f.farm_share().0,
        "fish_share": f.fish_share().0,
        "elections": json!({
            "annualised": f.elections().annualised(),
            "withholding_as_withheld": f.elections().withholding_as_withheld(),
        }),
        "positions": json!({
            "waiver": pos.waiver().map_or(Value::Null, |k| json!(k.to_string())),
            "waiver_on": date(pos.waiver_on()),
            "waiver_cause": pos.waiver_cause().to_string(),
            "born": date(pos.born()),
        }),
        "periods": f.periods().iter().map(|p| json!({
            "end": date(p.end()), "income": p.income().0, "annualised_tax": p.annualised_tax().0,
        })).collect::<Vec<_>>(),
        "withheld_total": f.withheld_total().0,
        "farmer_or_fisher": f.farmer_or_fisher(),
        "has_state": f.state().is_some(),
    })
}

/// The name of `j`'s vector file under golden/testdata.
pub fn golden_file(j: &Jurisdiction) -> String {
    format!("estimated/{}.json.gz", j.as_str())
}

/// Reads and parses a gzipped vector file under golden/testdata.
///
/// # Panics
///
/// When the file is missing or is not gzipped JSON.
pub fn read_gzip(name: &str) -> Value {
    use std::io::Read as _;
    let p = golden::path(name);
    let f = std::fs::File::open(&p).unwrap_or_else(|e| panic!("reading {}: {e}", p.display()));
    let mut text = String::new();
    flate2::read::GzDecoder::new(f)
        .read_to_string(&mut text)
        .unwrap_or_else(|e| panic!("decompressing {}: {e}", p.display()));
    serde_json::from_str(&text).unwrap_or_else(|e| panic!("parsing {}: {e}", p.display()))
}

/// `v` with every object's keys in order, as Go writes a map.
fn sorted(v: &Value) -> Value {
    match v {
        Value::Object(m) => {
            let mut keys: Vec<&String> = m.keys().collect();
            keys.sort();
            Value::Object(
                keys.into_iter()
                    .map(|k| (k.clone(), sorted(&m[k])))
                    .collect(),
            )
        }
        Value::Array(a) => Value::Array(a.iter().map(sorted).collect()),
        other => other.clone(),
    }
}

/// Fails, showing the first differing line of the two pretty-printed
/// values and the lines around it, unless `got` equals `want`.
///
/// # Panics
///
/// When the two differ.
#[track_caller]
pub fn assert_same_json(context: &str, got: &Value, want: &Value) {
    if got == want {
        return;
    }
    let pretty = |v: &Value| serde_json::to_string_pretty(&sorted(v)).unwrap_or_default();
    let (g, w) = (pretty(got), pretty(want));
    let (gl, wl): (Vec<&str>, Vec<&str>) = (g.lines().collect(), w.lines().collect());
    let at = gl
        .iter()
        .zip(&wl)
        .position(|(a, b)| a != b)
        .unwrap_or(gl.len().min(wl.len()));
    let from = at.saturating_sub(12);
    panic!(
        "{context}: Rust differs from Go at line {}\n--- Go\n{}\n--- Rust\n{}",
        at + 1,
        wl[from..(at + 6).min(wl.len())].join("\n"),
        gl[from..(at + 6).min(gl.len())].join("\n"),
    );
}

/// Compares `j`'s golden vectors with what `rules_for` answers: the
/// one-line test every jurisdiction's crate runs,
///
/// ```ignore
/// estimatedtest::assert_golden(&estimated::CALIFORNIA, californiataxcalc::estimated_tax);
/// ```
///
/// # Panics
///
/// When the vector file is missing or the answers differ from Go's.
#[track_caller]
pub fn assert_golden<R, E, F>(j: &Jurisdiction, rules_for: F)
where
    R: EstimatedTax,
    E: Display,
    F: Fn(i64) -> Result<R, E>,
{
    let name = golden_file(j);
    let want = read_gzip(&name);
    assert_same_json(&name, &golden_answers(j, rules_for), &want);
}
