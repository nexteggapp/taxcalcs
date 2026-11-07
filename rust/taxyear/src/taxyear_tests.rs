//! Go's cache_test.go, which reaches inside the package: the cache against
//! computing without one, ApplyChanges against apply, what can_add offers,
//! the workings adding up, whole dollars, and the summaries.

#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::collections::HashMap;

use buffa_descriptor::{DynamicMessage, Value};
use money::USCents;
use proto::scenariosv1::{
    __buffa::oneof::{item::Figures, term::Of},
    Item, ScenarioInputs, ScenarioInputsDiff, ScenarioResult, TaxYearResult, TaxYearScenarios,
};

use crate::compute::{COMPUTED_BY, compute_scenario};
use crate::reflect::{self, MONEY};
use crate::validate::{check_file, check_ids, check_scenarios};
use crate::{Cache, Resolved, compute, parse, resolve};

const EXAMPLE: &str = include_str!("../../../taxyear/testdata/example.jsonc");

/// Runs f on a thread with a large stack, as the messages are large for a
/// debug build's test thread.
fn big_stack<F: FnOnce() + Send + 'static>(f: F) {
    std::thread::Builder::new()
        .stack_size(256 << 20)
        .spawn(f)
        .expect("spawn")
        .join()
        .expect("the test thread panicked");
}

/// uncached is Compute as it was before the cache: the file's checks,
/// Resolve, CheckScenarios, then every scenario computed.
fn uncached(file: &TaxYearScenarios) -> TaxYearResult {
    let mut out = TaxYearResult {
        schema_version: file.schema_version,
        tax_year: file.tax_year,
        as_of: file.as_of.clone(),
        computed_by: COMPUTED_BY.to_string(),
        ..Default::default()
    };
    out.problems = check_file(file);
    out.problems.extend(check_ids(&file.shared));
    if !out.problems.is_empty() {
        return out;
    }
    out.status = crate::compute::year_status(file).into();
    let (resolved, problems) = resolve(file);
    let compared = check_scenarios(file, &resolved);
    out.problems = compared.get("").cloned().unwrap_or_default();
    if !out.problems.is_empty() {
        return out;
    }
    let by_id: HashMap<&str, &Resolved> = resolved
        .iter()
        .map(|r| (r.scenario().id.as_str(), r))
        .collect();
    for s in &file.scenarios {
        let mut res = ScenarioResult {
            scenario: s.id.clone(),
            ..Default::default()
        };
        res.problems = problems.get(&s.id).cloned().unwrap_or_default();
        res.problems
            .extend(compared.get(&s.id).cloned().unwrap_or_default());
        if let Some(r) = by_id.get(s.id.as_str())
            && res.problems.is_empty()
        {
            compute_scenario(None, file, r.inputs(), &mut res);
        }
        out.scenarios.push(res);
    }
    out
}

/// variants are the example with one edit each, in an order that edits a
/// scenario, then shared, then breaks a chain, so a cache sees each kind.
const VARIANTS: [(&str, &str, &str); 9] = [
    ("the example", "", ""),
    (
        "a raise edited",
        r#""minor": "625000""#,
        r#""minor": "700000""#,
    ),
    ("the raise back", "", ""),
    (
        "shared edited",
        r#""minor": "583333""#,
        r#""minor": "600000""#,
    ),
    (
        "a missing parent",
        r#""parent": "plan""#,
        r#""parent": "nope""#,
    ),
    (
        "a diff that doesn't apply",
        r#""add": { "id": "acme-v2k8n5zd""#,
        r#""add": { "id": "acme-h4c8w2pq""#,
    ),
    (
        "an identifier in shared",
        r#""Aug 14 paystub""#,
        r#""123456789""#,
    ),
    (
        "a second child",
        "] } } ] } } }\n  ]",
        "] } } ] } } },\n    { \"id\": \"raise2\", \"name\": \"Raise again\", \"parent\": \"raise\" }\n  ]",
    ),
    ("the example again", "", ""),
];

fn variant(old: &str, new: &str) -> TaxYearScenarios {
    let mut text = EXAMPLE.to_string();
    if !old.is_empty() {
        assert!(text.contains(old), "the example has no {old:?}");
        text = text.replacen(old, new, 1);
    }
    parse(text.as_bytes()).expect("the variant parses")
}

/// cases parses every case in testdata/cases, and the doc example.
fn cases() -> Vec<(String, TaxYearScenarios)> {
    let dir =
        std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../taxyear/testdata/cases");
    let mut names: Vec<String> = std::fs::read_dir(&dir)
        .expect("the cases")
        .map(|e| {
            e.expect("an entry")
                .file_name()
                .to_string_lossy()
                .into_owned()
        })
        .collect();
    names.sort();
    let mut out = vec![("example".to_string(), variant("", ""))];
    for n in names {
        let text = std::fs::read(dir.join(&n)).expect("a case");
        out.push((
            n.clone(),
            parse(&text).unwrap_or_else(|e| panic!("{n}: {e}")),
        ));
    }
    out
}

fn case(name: &str) -> TaxYearScenarios {
    let p = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../taxyear/testdata/cases")
        .join(name);
    parse(&std::fs::read(p).expect("the case")).expect("the case parses")
}

/// TestCacheMatchesUncached computes each variant through caches of two
/// sizes, one small enough to drop entries, and through no cache, and
/// wants one result.
#[test]
fn cache_matches_uncached() {
    big_stack(|| {
        let small = Cache::new(1, &[]);
        let large = Cache::new(64, &[]);
        for (name, old, new) in VARIANTS {
            let f = variant(old, new);
            let want = uncached(&f);
            assert_eq!(
                compute(&f).expect("compute").proto(),
                want,
                "{name}, no cache"
            );
            assert_eq!(
                small.compute(&f).expect("compute").proto(),
                want,
                "{name}, small"
            );
            assert_eq!(
                large.compute(&f).expect("compute").proto(),
                want,
                "{name}, large"
            );
        }
    });
}

/// TestCacheRecomputesOnlyWhatChanged edits the raise, then shared, and
/// checks which scenarios the cache resolved again: Go compares the
/// results' identity, and the port counts the cache's scenario entries.
#[test]
fn cache_recomputes_only_what_changed() {
    big_stack(|| {
        let c = Cache::new(8, &[]);
        let count = |c: &Cache| c.lock().scenario_count();
        c.compute(&variant("", "")).expect("compute");
        assert_eq!(count(&c), 2);
        c.compute(&variant(r#""minor": "625000""#, r#""minor": "700000""#))
            .expect("compute");
        assert_eq!(
            count(&c),
            3,
            "an edit to the raise recomputed Your plan, or not the raise"
        );
        c.compute(&variant(r#""minor": "583333""#, r#""minor": "600000""#))
            .expect("compute");
        assert_eq!(
            count(&c),
            5,
            "an edit to shared did not recompute every scenario"
        );
    });
}

/// TestCacheKeepsCopies changes a file after computing it, and wants the
/// cache's next result unchanged.
#[test]
fn cache_keeps_copies() {
    big_stack(|| {
        let c = Cache::new(8, &[]);
        let mut f = variant("", "");
        let first = c.compute(&f).expect("compute").proto();
        let g = f.clone();
        f.shared
            .get_or_insert_default()
            .model
            .get_or_insert_default()
            .employers[0]
            .name = "123456789".to_string();
        let again = c.compute(&g).expect("compute").proto();
        assert_eq!(
            first, again,
            "changing a computed file changed the cache's result for its copy"
        );
    });
}

/// Go's NewCache refuses a size under 1; the port's keeps one.
#[test]
fn cache_of_no_size_keeps_one() {
    big_stack(|| {
        let c = Cache::new(0, &[]);
        let f = variant("", "");
        assert_eq!(c.compute(&f).expect("compute").proto(), uncached(&f));
        assert!(c.lock().scenario_count() <= 1);
    });
}

/// TestApplyChangesMatchesApply wants ApplyChanges, which hands apply only
/// what a diff touches, to give what apply gives on the whole parent, for
/// every scenario of every case.
#[test]
fn apply_changes_matches_apply() {
    big_stack(|| {
        for (name, file) in cases() {
            let (resolved, _) = resolve(&file);
            let by_id: HashMap<String, ScenarioInputs> = resolved
                .iter()
                .map(|r| (r.scenario().id.clone(), r.inputs().clone()))
                .collect();
            for r in &resolved {
                let parent = match r.scenario().parent.as_str() {
                    "" => file.shared.as_option().cloned().unwrap_or_default(),
                    p => by_id[p].clone(),
                };
                let changes = r
                    .scenario()
                    .changes
                    .as_option()
                    .cloned()
                    .unwrap_or_default();
                let want = protodiffgen::apply(&parent, &changes)
                    .unwrap_or_else(|e| panic!("{name} {}: {e}", r.scenario().id));
                assert_eq!(&want, r.inputs(), "{name} {}", r.scenario().id);
            }
        }
    });
}

/// TestComputeLeavesInputsAlone computes every scenario of every case and
/// wants each scenario's resolved inputs unchanged after.
#[test]
fn compute_leaves_inputs_alone() {
    big_stack(|| {
        for (name, file) in cases() {
            let (resolved, _) = resolve(&file);
            for r in &resolved {
                let before = r.inputs().clone();
                compute_scenario(None, &file, r.inputs(), &mut ScenarioResult::default());
                assert_eq!(
                    &before,
                    r.inputs(),
                    "{name}: computing changed {}'s inputs",
                    r.scenario().id
                );
            }
        }
    });
}

#[test]
fn known_kinds() {
    let pool = reflect::pool().expect("the pool");
    let names = |full: &str| {
        let md = pool.message_by_name(full).expect("the message");
        crate::known_kinds(md).map(|fs| {
            fs.iter()
                .map(|f| f.name().to_string())
                .collect::<Vec<_>>()
                .join(" ")
        })
    };
    assert_eq!(
        names("nexteggapp.taxcalcs.scenarios.v1.EmployerPart").as_deref(),
        Some("from_paystubs from_deposits from_w2 ended")
    );
    assert_eq!(
        names("nexteggapp.taxcalcs.scenarios.v1.RetirementPart").as_deref(),
        Some("from_forms")
    );
    assert_eq!(
        names("nexteggapp.taxcalcs.scenarios.v1.EmployerModel"),
        None,
        "EmployerModel is a part"
    );
}

/// TestCanAddPartsAreValid adds each part the full household's projections
/// offer, one at a time, to its model in shared, and wants Your plan to
/// compute without a problem.
#[test]
fn can_add_parts_are_valid() {
    big_stack(|| {
        let file = case("full-2026.jsonc");
        let r = compute(&file).expect("compute");
        let proj = reflect::to_dyn(&*r.message().scenarios[0].projection).expect("dynamic");
        let mut offered = 0;
        let mut failures = Vec::new();
        for list in reflect::set_fields(&proj) {
            for p in reflect::list(&proj, list)
                .iter()
                .filter_map(reflect::as_message)
            {
                let id = reflect::string_named(p, "id");
                for v in reflect::list_named(p, "can_add") {
                    let Some(mut part) = reflect::as_message(v).cloned() else {
                        continue;
                    };
                    offered += 1;
                    if reflect::full_name(&part).ends_with(".ItemisedPartChoice") {
                        let which = reflect::which_named(&part, "part").expect("a choice");
                        part = reflect::message(&part, which).expect("a part").clone();
                    }
                    let kind = reflect::which_named(&part, "part")
                        .map(|f| f.name())
                        .unwrap_or("");
                    let problems = with_part(&file, list.name(), id, &part);
                    if !problems.is_empty() {
                        failures.push(format!("{}[{id}] can_add {kind}: {problems}", list.name()));
                    }
                }
            }
        }
        assert!(offered > 0, "nothing is offered");
        assert!(failures.is_empty(), "{}", failures.join("\n"));
    });
}

/// withPart computes the file with a copy of part added to a model in
/// shared, and returns Your plan's problems.
fn with_part(file: &TaxYearScenarios, list: &str, id: &str, part: &DynamicMessage) -> String {
    let mut f = file.clone();
    let mut models = reflect::to_dyn(&*f.shared.model).expect("dynamic");
    let fd = reflect::field_of(&models, list).expect("the list");
    for v in reflect::list_mut(&mut models, fd).expect("the list") {
        let Some(m) = reflect::as_message_mut(v) else {
            continue;
        };
        if reflect::string_named(m, "id") != id {
            continue;
        }
        let m = match reflect::which_named(m, "kind") {
            Some(k) => reflect::mutable(m, k).expect("the kind"),
            None => m,
        };
        let mut added = part.clone();
        let idf = reflect::field_of(&added, "id").expect("an id");
        reflect::set(&mut added, idf, Value::String(format!("{id}-added")));
        let pf = reflect::field_of(m, "parts").expect("parts");
        reflect::list_mut(m, pf)
            .expect("parts")
            .push(Value::Message(added));
    }
    f.shared.get_or_insert_default().model =
        reflect::from_dyn::<proto::scenariosv1::TaxYearModel>(&models)
            .expect("a model")
            .into();
    let r = compute(&f).expect("compute");
    let m = r.message();
    let mut out = Vec::new();
    for p in m.problems.iter().chain(&m.scenarios[0].problems) {
        out.push(format!("{}: {}", p.path, p.message.text));
    }
    out.join("; ")
}

/// Every money figure in a result, by path, as working.go's newWorkings
/// reads them.
fn amounts(s: &ScenarioResult) -> HashMap<String, USCents> {
    crate::working::new_workings(s).amounts
}

/// TestWorkingsAddUp wants every working's terms, in every scenario of
/// every case, to add up to its figure: a path term by its amount, an item
/// term by the item's amount of the same figure.
#[test]
fn workings_add_up() {
    big_stack(|| {
        let mut failures = Vec::new();
        for (name, file) in cases() {
            let r = compute(&file).expect("compute");
            for s in &r.message().scenarios {
                let w = amounts(s);
                let mut items: HashMap<&str, &Item> = HashMap::new();
                for list in crate::compute::all_items(&s.projection) {
                    for it in list {
                        items.insert(&it.id, it);
                    }
                }
                for x in &s.working {
                    if x.terms.is_empty() {
                        continue;
                    }
                    let Some(&total) = w.get(&x.path) else {
                        failures.push(format!(
                            "{name} {}: {} has a working and no amount",
                            s.scenario, x.path
                        ));
                        continue;
                    };
                    let mut sum = USCents(0);
                    for t in &x.terms {
                        let v = match &t.of {
                            Some(Of::Item(id)) => {
                                item_amount(items.get(id.as_str()).copied(), &x.path)
                            }
                            Some(Of::Path(p)) => w.get(p).copied(),
                            None => None,
                        };
                        let Some(v) = v else {
                            failures.push(format!(
                                "{name} {}: {}'s term {t:?} has no amount",
                                s.scenario, x.path
                            ));
                            continue;
                        };
                        sum += USCents(i64::from(t.sign) * v.0);
                    }
                    if sum != total {
                        failures.push(format!(
                            "{name} {}: {} is {total} and its terms add up to {sum}",
                            s.scenario, x.path
                        ));
                    }
                }
            }
        }
        assert!(failures.is_empty(), "{}", failures.join("\n"));
    });
}

/// itemAmount is an item's amount of the figure a working's path names:
/// for ".items.gross" the item's gross, and for a total that is itself
/// money, its amount.
fn item_amount(it: Option<&Item>, path: &str) -> Option<USCents> {
    let m = reflect::to_dyn(it?)?;
    let which = reflect::which_named(&m, "figures")?;
    let figures = reflect::message(&m, which)?;
    let field = path
        .split_once(".items.")
        .map(|(_, f)| f)
        .unwrap_or("amount");
    let v = reflect::message_named(figures, field)?;
    Some(USCents(reflect::minor(v)))
}

/// TestApplyChangesPrunesLists checks ApplyChanges against apply where the
/// pruning has the most to put back: a patched model beside one left
/// alone, a removed part, a patch that moves a part to another oneof case,
/// an added part and an added model.
#[test]
fn apply_changes_prunes_lists() {
    let pool = reflect::pool().expect("the pool");
    let parent = r#"{ "model": {
        "employers": [ { "id": "acme", "name": "Acme", "parts": [
            { "id": "a", "payRate": { "paycheck": { "gross": { "minor": "100", "currency": "USD" } } } },
            { "id": "b", "payRate": { "paycheck": { "gross": { "minor": "200", "currency": "USD" } } } },
            { "id": "c", "oneOff": { "amount": { "minor": "300", "currency": "USD" } } } ] },
          { "id": "beta", "name": "Beta", "parts": [ { "id": "x", "oneOff": { "amount": { "minor": "1", "currency": "USD" } } } ] } ] },
        "overrides": [ { "path": "federal.form_1040.tax", "amount": { "minor": "5", "currency": "USD" } } ] }"#;
    let changes = r#"{ "model": { "employers": [
        { "id": "acme", "patch": { "name": "Acme 2", "parts": [
            { "id": "a", "remove": {} },
            { "id": "b", "patch": { "oneOff": { "amount": { "minor": "5", "currency": "USD" } } } },
            { "id": "d", "add": { "id": "d", "oneOff": { "amount": { "minor": "7", "currency": "USD" } } } } ] } },
        { "id": "gamma", "add": { "id": "gamma", "name": "Gamma" } } ] } }"#;
    let parent: ScenarioInputs = reflect::from_dyn(
        &crate::protojson::unmarshal(
            pool,
            "nexteggapp.taxcalcs.scenarios.v1.ScenarioInputs",
            parent.as_bytes(),
        )
        .expect("the parent"),
    )
    .expect("inputs");
    let changes: ScenarioInputsDiff = reflect::from_dyn(
        &crate::protojson::unmarshal(
            pool,
            "nexteggapp.taxcalcs.scenarios.v1.ScenarioInputsDiff",
            changes.as_bytes(),
        )
        .expect("the changes"),
    )
    .expect("a diff");
    let before = parent.clone();
    let want = protodiffgen::apply(&parent, &changes).expect("apply");
    let got = crate::apply_changes(&parent, &changes).expect("ApplyChanges");
    assert_eq!(want, got, "ApplyChanges differs from apply");
    assert_eq!(before, parent, "ApplyChanges changed the parent");
}

/// TestCacheKeepsModelsAnEditLeaves edits the raise's new pay on the full
/// household, and wants the raise's business, charity and broker
/// projections kept from before the edit: Go compares their identity, and
/// the port wants no new model entry in the cache, and the same figures.
#[test]
fn cache_keeps_models_an_edit_leaves() {
    big_stack(|| {
        let p = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"))
            .join("../../taxyear/testdata/cases/full-2026.jsonc");
        let text = std::fs::read_to_string(p).expect("the case");
        let c = Cache::new(16, &[]);
        let raise = |gross: &str| {
            let at = text.find(r#""id": "raise""#).expect("a raise");
            let s = format!(
                "{}{}",
                &text[..at],
                text[at..].replacen(
                    r#""gross": { "minor": "583333""#,
                    &format!(r#""gross": {{ "minor": "{gross}""#),
                    1
                )
            );
            let f = parse(s.as_bytes()).expect("parses");
            let r = c.compute(&f).expect("compute");
            let models = c.lock().model_count();
            (r.scenario("raise").expect("raise").proto(), models)
        };
        let (before, n) = raise("583333");
        let (after, m) = raise("600000");
        assert_eq!(
            n, m,
            "an edit to the raise's pay re-expanded a model it leaves alone"
        );
        assert_eq!(
            before.projection.businesses[0],
            after.projection.businesses[0]
        );
        assert_eq!(before.projection.itemised[0], after.projection.itemised[0]);
        assert_eq!(before.projection.brokers[0], after.projection.brokers[0]);
        assert_ne!(
            before.projection.employers[0], after.projection.employers[0],
            "an edit to the raise's pay kept the job's old projection"
        );
    });
}

/// TestFiguresAreWholeDollars wants every amount in every scenario's
/// result in whole dollars, as displayed: rounding happens once, at each
/// source. A working's exact figure, before rounding, is the exception.
#[test]
fn figures_are_whole_dollars() {
    big_stack(|| {
        let mut failures = Vec::new();
        for (name, file) in cases() {
            let r = compute(&file).expect("compute");
            for s in &r.message().scenarios {
                let mut s = s.clone();
                for w in &mut s.working {
                    w.exact = buffa::MessageField::none();
                }
                // can_add holds parts to add, inputs for the household to
                // change, not figures.
                if let Some(p) = s.projection.as_option_mut() {
                    for e in &mut p.employers {
                        e.can_add.clear();
                    }
                }
                let mut seen = std::collections::HashSet::new();
                let d = reflect::to_dyn(&s).expect("dynamic");
                whole_dollars(&d, "", &mut |path, minor| {
                    if seen.insert(path_shape(path)) {
                        failures.push(format!(
                            "{name} {}: {path} is {minor} cents, not whole dollars",
                            s.scenario
                        ));
                    }
                });
            }
        }
        assert!(failures.is_empty(), "{}", failures.join("\n"));
    });
}

/// wholeDollars calls bad with every Money under m that holds cents.
fn whole_dollars(m: &DynamicMessage, at: &str, bad: &mut dyn FnMut(&str, i64)) {
    if reflect::full_name(m) == MONEY {
        let minor = reflect::minor(m);
        if minor % 100 != 0 {
            bad(at, minor);
        }
        return;
    }
    for fd in reflect::set_fields(m) {
        if !reflect::is_message_kind(fd) || reflect::is_map(fd) {
            continue;
        }
        if reflect::is_list(fd) {
            for (i, v) in reflect::list(m, fd).iter().enumerate() {
                if let Some(sub) = reflect::as_message(v) {
                    whole_dollars(sub, &format!("{at}.{}[{i}]", fd.name()), bad);
                }
            }
        } else if let Some(sub) = reflect::message(m, fd) {
            whole_dollars(sub, &format!("{at}.{}", fd.name()), bad);
        }
    }
}

/// pathShape is a path with its indexes left out, so a failure is reported
/// once a figure.
fn path_shape(path: &str) -> String {
    let mut out = String::new();
    let mut skip = false;
    for c in path.chars() {
        match c {
            '[' => skip = true,
            ']' => skip = false,
            c if !skip => out.push(c),
            _ => {}
        }
    }
    out
}

/// TestSummariesAddUp checks the summary figures on the full household
/// against the figures they add: wages against the jobs, what's credited
/// against withholding and payments, the balance against the tax, and
/// state and local tax against Schedule A.
#[test]
fn summaries_add_up() {
    big_stack(|| {
        let r = compute(&case("full-2026.jsonc")).expect("compute");
        for s in &r.message().scenarios {
            let sum = &s.summary;
            let gross: i64 = s
                .projection
                .employers
                .iter()
                .map(|e| e.year.items.gross.minor)
                .sum();
            assert!(
                sum.wages_gross.minor == gross && gross != 0,
                "{}: wages",
                s.scenario
            );
            let salt = &sum.salt;
            let sa = &s.federal.schedule_a;
            assert_eq!(
                salt.withholding.minor + salt.estimates.minor + salt.last_year_balance.minor,
                sa.state_local_income_or_sales_taxes.minor,
                "{}: state tax paid by where",
                s.scenario
            );
            assert!(
                sum.standard_deduction.minor != 0 && !sum.retirement.is_empty(),
                "{}: want a standard deduction and each saver",
                s.scenario
            );
            for e in &s.estimated {
                let x = &e.summary;
                assert!(
                    x.applied_to_next_year.is_set() && x.credited.is_set(),
                    "{} {}: applied and credited",
                    s.scenario,
                    e.jurisdiction
                );
                assert_eq!(
                    x.estimated.minor, x.paid.minor,
                    "{} {}: estimated",
                    s.scenario, e.jurisdiction
                );
                if e.jurisdiction != "US" {
                    continue;
                }
                let f = &s.federal.form_1040;
                assert_eq!(
                    x.credited.minor, f.total_payments.minor,
                    "{}: credited",
                    s.scenario
                );
                assert_eq!(
                    x.with_return.minor,
                    f.amount_owed.minor - f.overpaid.minor,
                    "{}: with the return",
                    s.scenario
                );
            }
        }
    });
}

/// TestSpreadPaymentsAreNotPaid: in the full household's "leaves", the tax
/// paid model's SpreadTheRest suggests payments for the due dates ahead.
/// They are planned, not paid, so the return's line 26 counts only the
/// payments made.
#[test]
fn spread_payments_are_not_paid() {
    big_stack(|| {
        let r = compute(&case("full-2026.jsonc")).expect("compute");
        let s = r.scenario("leaves").expect("leaves").proto();
        let (mut made, mut ahead) = (0, 0);
        for tp in &s.projection.tax_paid {
            if tp.jurisdiction == "US" {
                made = tp.so_far.counted.minor;
                ahead = tp.rest.counted.minor;
            }
        }
        assert_ne!(
            ahead, 0,
            "leaves suggests no payments ahead; the test needs some"
        );
        assert_eq!(
            s.federal.form_1040.estimated_tax_payments.minor, made,
            "line 26"
        );
    });
}

fn must_plan(f: &TaxYearScenarios) -> ScenarioResult {
    let r = compute(f).expect("compute");
    let m = r.message();
    assert!(m.problems.is_empty(), "problems {:?}", m.problems);
    let s = r.scenario("plan").expect("plan").proto();
    assert!(s.problems.is_empty(), "problems {:?}", s.problems);
    s
}

/// TestSpouseHasARetirementEntry: a married couple filing jointly where
/// only you save. Sam, the spouse, has no retirement model and no date of
/// birth, and still has an entry: the plan limit without a catch-up, and a
/// question for the date of birth at it.
#[test]
fn spouse_has_a_retirement_entry() {
    big_stack(|| {
        let f = variant(
            r#""facts": { "taxYear": 2026 },"#,
            r#""facts": { "taxYear": 2026,
      "people": [ { "id": "you", "relationship": "RELATIONSHIP_YOU", "dateOfBirth": { "year": 1980, "month": 1, "day": 1 } },
                  { "id": "sam", "name": "Sam", "relationship": "RELATIONSHIP_SPOUSE" } ],
      "elections": [ { "id": "filing-status", "filingStatus": "FILING_STATUS_MARRIED_FILING_JOINTLY" } ] },"#,
        );
        let s = must_plan(&f);
        for id in ["you", "sam"] {
            let x = s
                .summary
                .retirement
                .iter()
                .find(|x| x.person == id)
                .expect("an entry");
            assert_eq!(x.plan_limit.minor, 2_450_000, "{id}'s plan limit");
        }
        assert!(
            s.questions
                .iter()
                .any(|q| q.id == "retirement/sam/date-of-birth"
                    && q.at == "summary.retirement[sam].plan_limit"),
            "no question for Sam's date of birth at the plan limit"
        );
    });
}

/// TestSpouseBeforeTheirEntry: filing jointly before the spouse's entry is
/// in the facts. The return still has a spouse, named "spouse", with a
/// retirement entry: nothing deducted, the plan limit without a catch-up,
/// and the date of birth asked.
#[test]
fn spouse_before_their_entry() {
    big_stack(|| {
        let f = variant(
            r#""facts": { "taxYear": 2026 },"#,
            r#""facts": { "taxYear": 2026,
      "elections": [ { "id": "filing-status", "filingStatus": "FILING_STATUS_MARRIED_FILING_JOINTLY" } ] },"#,
        );
        let r = compute(&f).expect("compute");
        let s = r.scenario("plan").expect("plan").proto();
        let entry = s
            .summary
            .retirement
            .iter()
            .find(|x| x.person == "spouse")
            .expect("the spouse's entry");
        assert!(
            entry.deducted.is_set()
                && entry.deducted.minor == 0
                && entry.plan_limit.minor == 2_450_000
        );
        assert!(
            s.questions
                .iter()
                .any(|q| q.at == "summary.retirement[spouse].plan_limit"),
            "no question for the spouse's date of birth"
        );
    });
}

/// TestBalanceWithoutAModel: full-2025 has no California tax paid model,
/// and paid California's $371 balance in April 2026. The payment is listed
/// under California and leaves nothing to pay with the return, without
/// counting toward the year's installments.
#[test]
fn balance_without_a_model() {
    big_stack(|| {
        use proto::factsv1::__buffa::oneof::payment_purpose::Purpose;
        let r = compute(&case("full-2025.jsonc")).expect("compute");
        let s = r.scenario("plan").expect("plan").proto();
        let found = s
            .projection
            .tax_paid
            .iter()
            .filter(|t| t.jurisdiction == "CA")
            .any(|t| {
                t.items.iter().any(|it| match &it.figures {
                    Some(Figures::Payment(p)) => {
                        matches!(p.purpose.purpose, Some(Purpose::BalanceFor(2025)))
                            && p.amount.minor == 37_100
                            && !p.counts
                    }
                    _ => false,
                })
            });
        assert!(
            found,
            "no uncounted $371 California balance payment under tax paid"
        );
        for e in s.estimated.iter().filter(|e| e.jurisdiction == "CA") {
            let sum = &e.summary;
            assert!(
                sum.left_with_return.is_set()
                    && sum.left_with_return.minor == 0
                    && sum.paid.minor == 0,
                "California left with the return {:?}, paid {:?}",
                sum.left_with_return,
                sum.paid
            );
        }
    });
}

fn hsa_example(coverage: &str, pretax: Option<&str>) -> String {
    let mut text = EXAMPLE.replacen(
        r#""facts": { "taxYear": 2026 }"#,
        &format!(
            r#""facts": {{ "taxYear": 2026, "people": [ {{ "id": "you",
		"relationship": "RELATIONSHIP_YOU", "dateOfBirth": {{ "year": 1980, "month": 1, "day": 1 }}, "hsaCoverage": "{coverage}" }} ] }}"#
        ),
        1,
    );
    if let Some(minor) = pretax {
        text = text.replacen(
            r#""thisPaycheck": { "gross": { "minor": "583333", "currency": "USD" },"#,
            &format!(
                r#""thisPaycheck": {{ "gross": {{ "minor": "583333", "currency": "USD" }}, "pretax": [ {{ "kind": "PRETAX_KIND_HSA", "amount": {{ "minor": "{minor}", "currency": "USD" }} }} ],"#
            ),
            1,
        );
    }
    text
}

fn hsa_pretax(e: &proto::scenariosv1::EmployerProjection) -> (i64, i64) {
    let (mut hsa, mut all) = (0, 0);
    for x in &e.year.items.pretax {
        all += x.amount.minor;
        if x.kind.as_known() == Some(proto::factsv1::PretaxKind::PRETAX_KIND_HSA) {
            hsa += x.amount.minor;
        }
    }
    (hsa, all)
}

/// TestHsaExcessReachesTheReturn: $3,000 a paycheck to an HSA, self-only.
/// The two paychecks made hold $6,000 against a $4,400 limit, so the 22 to
/// come lose all of theirs to wages and $1,600 made is other income on
/// Schedule 1; the excess's working adds up.
#[test]
fn hsa_excess_reaches_the_return() {
    big_stack(|| {
        let f = parse(hsa_example("HSA_COVERAGE_SELF_ONLY", Some("300000")).as_bytes())
            .expect("parses");
        let s = must_plan(&f);
        let e = &s.projection.employers[0];
        assert_eq!(e.hsa_over_in_income.minor, 160_000);
        assert_eq!(e.hsa_over.minor, 6_760_000);
        assert_eq!(
            s.federal.schedule_1.total_other_income.minor, 160_000,
            "Schedule 1 other income"
        );
        let w = amounts(&s);
        let x = s
            .working
            .iter()
            .find(|x| x.path == "projection.employers[acme].hsa_over")
            .expect("hsa_over has no working");
        let sum: i64 = x
            .terms
            .iter()
            .filter_map(|t| match &t.of {
                Some(Of::Path(p)) => w.get(p).map(|v| v.0),
                _ => None,
            })
            .sum();
        let total = w.get(&x.path).map(|v| v.0).unwrap_or(0);
        assert!(
            sum == total && total != 0,
            "hsa_over is {total} and its terms add to {sum}"
        );
    });
}

/// TestHsaOnAPayChange: $1,000 a paycheck to an HSA, self-only, typed on a
/// pay change from Oct 1 rather than a paystub. The six paychecks put in
/// $6,000 against a $4,400 limit, so $1,600 stays in box 1.
#[test]
fn hsa_on_a_pay_change() {
    big_stack(|| {
        let text = hsa_example("HSA_COVERAGE_SELF_ONLY", None);
        let at = r#"{ "id": "acme-b6n3x8tf", "fromDeposits""#;
        assert!(text.contains(at), "the example has no Acme deposits part");
        let rate = r#"{ "id": "acme-hsa-rate", "payRate": { "from": { "year": 2026, "month": 10, "day": 1 },
            "paycheck": { "gross": { "minor": "583333", "currency": "USD" }, "pretax": [ { "kind": "PRETAX_KIND_HSA", "amount": { "minor": "100000", "currency": "USD" } } ] } } },
          { "id": "acme-b6n3x8tf", "fromDeposits""#;
        let f = parse(text.replacen(at, rate, 1).as_bytes()).expect("parses");
        let s = must_plan(&f);
        let e = &s.projection.employers[0];
        let (hsa, pretax) = hsa_pretax(e);
        assert_eq!((e.hsa_over_in_wages.minor, hsa), (160_000, 440_000));
        assert_eq!(
            e.year.boxes.wages.minor,
            e.year.items.gross.minor - pretax,
            "box 1"
        );
    });
}

/// TestNoHsaPlanHoldsNothing: $100 a paycheck to an HSA through payroll,
/// but no HSA-eligible plan. The limit is nothing, so the paychecks to come
/// put none of theirs aside, and what was made is other income.
#[test]
fn no_hsa_plan_holds_nothing() {
    big_stack(|| {
        let f = parse(hsa_example("HSA_COVERAGE_NONE", Some("10000")).as_bytes()).expect("parses");
        let s = must_plan(&f);
        let e = &s.projection.employers[0];
        assert_eq!(
            (e.hsa_over.minor, e.hsa_over_in_income.minor),
            (240_000, 20_000)
        );
        assert_eq!(hsa_pretax(e).0, 20_000, "HSA through payroll counted");
    });
}
