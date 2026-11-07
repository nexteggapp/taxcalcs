//! Go's override_test.go: overrides of figures so far flow into every
//! total that reads them, and every printed total adds up.

#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::collections::{BTreeMap, HashMap};

use buffa::MessageField;
use buffa_descriptor::{DynamicMessage, Value};
use money::USCents;
use proto::scenariosv1::{
    __buffa::oneof::{employer_default::Rest, employer_part::Part, term::Of, work_place::Place},
    EmployerDefault, EmployerModel, EmployerPart, EmployerProjection, Item, ItemStatus, Override,
    PayFrequency, ScenarioResult, SupplementalMethod, TaxYearScenarios, WorkPlace, employer_part,
    work_place,
};
use proto::{commonv1, factsv1};

use crate::reflect::{self, MONEY};
use crate::{compute, overridable_path, parse};

fn big_stack<F: FnOnce() + Send + 'static>(f: F) {
    std::thread::Builder::new()
        .stack_size(256 << 20)
        .spawn(f)
        .expect("spawn")
        .join()
        .expect("the test thread panicked");
}

fn read(dir: &str, name: &str) -> TaxYearScenarios {
    let p = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../taxyear/testdata")
        .join(dir)
        .join(name);
    parse(&std::fs::read(&p).expect("the file")).unwrap_or_else(|e| panic!("{name}: {e}"))
}

fn files(dir: &str) -> Vec<(String, TaxYearScenarios)> {
    let d = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"))
        .join("../../taxyear/testdata")
        .join(dir);
    let mut names: Vec<String> = std::fs::read_dir(&d)
        .expect("the directory")
        .map(|e| {
            e.expect("an entry")
                .file_name()
                .to_string_lossy()
                .into_owned()
        })
        .collect();
    names.sort();
    names
        .into_iter()
        .map(|n| (n.clone(), read(dir, &n)))
        .collect()
}

fn usd(minor: i64) -> commonv1::Money {
    commonv1::Money {
        minor,
        currency: "USD".to_string(),
        ..Default::default()
    }
}

fn add_override(f: &mut TaxYearScenarios, path: &str, minor: i64) {
    f.shared.get_or_insert_default().overrides.push(Override {
        path: path.to_string(),
        amount: MessageField::some(usd(minor)),
        ..Default::default()
    });
}

/// overridable lists the figures so far an override can name in a
/// result, with their values: each money line of every projection's so
/// far items, and a job's pre-tax and state lines.
fn overridable(s: &ScenarioResult) -> BTreeMap<String, i64> {
    let mut out = BTreeMap::new();
    let Some(proj) = reflect::to_dyn(&*s.projection) else {
        return out;
    };
    for list in reflect::set_fields(&proj) {
        if list.name() == "tax_paid" {
            continue;
        }
        for p in reflect::list(&proj, list)
            .iter()
            .filter_map(reflect::as_message)
        {
            let id = reflect::string_named(p, "id");
            let Some(so_far) = reflect::message_named(p, "so_far") else {
                continue;
            };
            let at = format!("projection.{}[{id}].so_far.items", list.name());
            let Some(items) = reflect::message_named(so_far, "items") else {
                if reflect::field_of(so_far, "items").is_some_and(reflect::is_money) {
                    out.insert(format!("{at}.amount"), 0);
                }
                continue;
            };
            if reflect::full_name(items) == MONEY {
                out.insert(format!("{at}.amount"), reflect::minor(items));
                continue;
            }
            for fd in reflect::set_fields(items) {
                if !reflect::is_message_kind(fd) || reflect::is_map(fd) {
                    continue;
                }
                if !reflect::is_list(fd) {
                    if let Some(m) = reflect::message(items, fd)
                        && reflect::full_name(m) == MONEY
                    {
                        out.insert(format!("{at}.{}", fd.name()), reflect::minor(m));
                    }
                    continue;
                }
                for (k, x) in reflect::list(items, fd)
                    .iter()
                    .filter_map(reflect::as_message)
                    .enumerate()
                {
                    let money = |name: &str| {
                        reflect::message_named(x, name)
                            .map(reflect::minor)
                            .unwrap_or(0)
                    };
                    if reflect::field_of(x, "state_income").is_some()
                        && reflect::field_of(x, "tax_withheld").is_some()
                    {
                        let st = reflect::string_named(x, "state");
                        out.insert(
                            format!("{at}.states[{st}].state_income"),
                            money("state_income"),
                        );
                        out.insert(
                            format!("{at}.states[{st}].tax_withheld"),
                            money("tax_withheld"),
                        );
                    } else if reflect::field_of(x, "amount").is_some() && fd.name() == "pretax" {
                        out.insert(format!("{at}.pretax[{k}].amount"), money("amount"));
                    }
                }
            }
        }
    }
    out
}

/// figureAt is the money at a path in a result: a field path whose list
/// items are named by id, or a state's line by its state.
fn figure_at(s: &ScenarioResult, path: &str) -> Option<i64> {
    let d = reflect::to_dyn(s)?;
    let mut m = &d;
    for part in path.split('.') {
        let part = part.strip_suffix(']').unwrap_or(part);
        let (name, key) = part.split_once('[').unwrap_or((part, ""));
        let fd = reflect::field_of(m, name)?;
        if !reflect::is_list(fd) {
            m = reflect::message(m, fd)?;
            continue;
        }
        let mut found = None;
        for x in reflect::list(m, fd).iter().filter_map(reflect::as_message) {
            for k in ["id", "state"] {
                if reflect::field_of(x, k).is_some() && reflect::string_named(x, k) == key {
                    found = Some(x);
                }
            }
        }
        m = found?;
    }
    (reflect::full_name(m) == MONEY).then(|| reflect::minor(m))
}

/// The money figures of a result, by path, as the workings read them.
fn amounts(s: &ScenarioResult) -> HashMap<String, USCents> {
    crate::working::new_workings(s).amounts
}

fn item_amount(it: Option<&Item>, path: &str) -> USCents {
    let Some(m) = it.and_then(reflect::to_dyn) else {
        return USCents(0);
    };
    let Some(f) = reflect::which_named(&m, "figures").and_then(|w| reflect::message(&m, w)) else {
        return USCents(0);
    };
    let field = path
        .split_once(".items.")
        .map(|(_, f)| f)
        .unwrap_or("amount");
    USCents(
        reflect::message_named(f, field)
            .map(reflect::minor)
            .unwrap_or(0),
    )
}

/// The set money fields of a totals message, one message deep.
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

/// Each item total's items, as working.go's carrying finds them.
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
        let Some(fig) = reflect::which_named(m, "figures").and_then(|w| reflect::message(m, w))
        else {
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

/// typedCA reports whether a job's California wages so far were
/// overridden, which then stand as typed.
fn typed_ca(e: &EmployerProjection) -> bool {
    e.items
        .iter()
        .any(|it| it.origin.r#override.ends_with(".states[CA].state_income"))
}

/// addsUp lists each way the scenario's figures fail to add up: a working
/// whose terms don't add to its figure; a year figure of a projection that
/// isn't its so far and its rest; a so far or rest item total that isn't
/// its items; and state and local tax by where it was paid that isn't
/// Schedule A's line 5a.
fn adds_up(s: &ScenarioResult) -> Vec<String> {
    let mut out = Vec::new();
    let w = amounts(s);
    let amount = |p: &str| w.get(p).copied().unwrap_or(USCents(0));
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
        let total = amount(&x.path);
        let mut sum = USCents(0);
        for t in &x.terms {
            let v = match &t.of {
                Some(Of::Item(id)) => item_amount(items.get(id.as_str()).copied(), &x.path),
                Some(Of::Path(p)) => amount(p),
                None => USCents(0),
            };
            sum += USCents(i64::from(t.sign) * v.0);
        }
        if sum != total {
            out.push(format!(
                "{} is {total} and its working adds up to {sum}",
                x.path
            ));
        }
    }
    if let Some(proj) = reflect::to_dyn(&*s.projection) {
        for list in reflect::set_fields(&proj) {
            for p in reflect::list(&proj, list)
                .iter()
                .filter_map(reflect::as_message)
            {
                let at = format!(
                    "projection.{}[{}]",
                    list.name(),
                    reflect::string_named(p, "id")
                );
                let Some(year) = reflect::message_named(p, "year") else {
                    continue;
                };
                for f in money_fields(year, "") {
                    let (y, sf, r) = (
                        amount(&format!("{at}.year.{f}")),
                        amount(&format!("{at}.so_far.{f}")),
                        amount(&format!("{at}.rest.{f}")),
                    );
                    if y != sf + r {
                        out.push(format!(
                            "{at}.year.{f} is {y}, so far {sf} and the rest {r}"
                        ));
                    }
                }
                let carried = carrying(reflect::list_named(p, "items"));
                for period in ["so_far", "rest"] {
                    let Some(m) = reflect::message_named(p, period) else {
                        continue;
                    };
                    for f in money_fields(m, "") {
                        let Some(ids) = carried.get(&format!("{period}.{f}")) else {
                            continue;
                        };
                        if !f.starts_with("items") {
                            continue;
                        }
                        let total = amount(&format!("{at}.{period}.{f}"));
                        let sum: i64 = ids
                            .iter()
                            .map(|id| {
                                item_amount(
                                    items.get(id.as_str()).copied(),
                                    &format!("{at}.{period}.{f}"),
                                )
                                .0
                            })
                            .sum();
                        // Items are rounded together, so the total is their
                        // exact sum rounded: within a dollar of each rounded
                        // alone.
                        let n = i64::try_from(ids.len()).unwrap_or(0);
                        if (total.0 - sum).abs() > 100 * n {
                            out.push(format!(
                                "{at}.{period}.{f} is {total} and its items add up to {sum}"
                            ));
                        }
                    }
                }
            }
        }
    }
    // A job's California wages earned there alone are box 1, the payroll
    // HSA and the employer's, in whole dollars; a W-2's, as printed.
    for e in &s.projection.employers {
        if e.expected.is_empty() {
            continue;
        }
        for (name, p) in [("so_far", &e.so_far), ("rest", &e.rest)] {
            let states = &p.items.states;
            if states.len() != 1 || states[0].state != "CA" || e.so_far.is_unset() {
                continue;
            }
            let hsa: i64 = p
                .items
                .pretax
                .iter()
                .filter(|x| x.kind.as_known() == Some(factsv1::PretaxKind::PRETAX_KIND_HSA))
                .map(|x| x.amount.minor)
                .sum();
            for st in p.boxes.states.iter().filter(|st| st.state == "CA") {
                let want = p.boxes.wages.minor + hsa + st.employer_hsa.minor;
                if st.wages.minor != want && !typed_ca(e) {
                    out.push(format!(
                        "projection.employers[{}].{name}.boxes.states[CA].wages is {}, box 1, the payroll HSA and the employer's add up to {want}",
                        e.id, st.wages.minor
                    ));
                }
            }
        }
    }
    // An itemised model's year is what Schedule A takes from it.
    for m in &s.projection.itemised {
        let mut b = 0;
        if let Some(boxes) = reflect::to_dyn(&*m.year.boxes) {
            for fd in reflect::set_fields(&boxes) {
                b += reflect::message(&boxes, fd)
                    .map(reflect::minor)
                    .unwrap_or(0);
            }
        }
        if m.year.items.minor != b {
            out.push(format!(
                "projection.itemised[{}].year.items is {}, and Schedule A takes {b} from it",
                m.id, m.year.items.minor
            ));
        }
    }
    let sa = &s.federal.schedule_a;
    if sa.total_itemized_deductions.is_set()
        && !s.working.iter().any(|x| {
            x.path == "federal.schedule_a.total_itemized_deductions" && !x.terms.is_empty()
        })
    {
        out.push(
            "federal.schedule_a.total_itemized_deductions has no working that adds up".to_string(),
        );
    }
    if s.summary.itemised_bracket_limit.minor < 0 {
        out.push(format!(
            "summary.itemised_bracket_limit is {}, below zero",
            s.summary.itemised_bracket_limit.minor
        ));
    }
    let salt = &s.summary.salt;
    if s.federal.schedule_a.is_set() && s.summary.salt.is_set() {
        let got = salt.withholding.minor + salt.estimates.minor + salt.last_year_balance.minor;
        if got != sa.state_local_income_or_sales_taxes.minor {
            out.push(format!(
                "state tax paid by where adds to {got}, Schedule A line 5a is {}",
                sa.state_local_income_or_sales_taxes.minor
            ));
        }
    }
    out
}

fn plan(f: &TaxYearScenarios) -> ScenarioResult {
    let r = compute(f).expect("compute");
    let s = r.message().scenarios[0].clone();
    assert!(s.problems.is_empty(), "problems {:?}", s.problems);
    s
}

/// A small deterministic generator, in place of Go's math/rand: the
/// overrides it picks differ from Go's, and are the same each run.
struct Lcg(u64);

impl Lcg {
    fn intn(&mut self, n: usize) -> usize {
        self.0 = self
            .0
            .wrapping_mul(6364136223846793005)
            .wrapping_add(1442695040888963407);
        ((self.0 >> 33) as usize) % n
    }
}

/// TestOverridesAddUp overrides random figures so far in every case and
/// scenario and wants every figure to add up as it did before: an
/// override's figure flows into every total that reads it.
#[test]
fn overrides_add_up() {
    big_stack(|| {
        let mut r = Lcg(1);
        let mut failures = Vec::new();
        for (name, file) in files("cases") {
            let base = compute(&file).expect("compute");
            let paths = overridable(&base.message().scenarios[0]);
            let keys: Vec<&String> = paths.keys().filter(|k| overridable_path(k)).collect();
            if keys.is_empty() {
                continue;
            }
            for _ in 0..4 {
                let mut f = file.clone();
                let mut picked: std::collections::HashSet<String> =
                    f.shared.overrides.iter().map(|o| o.path.clone()).collect();
                let mut chosen = Vec::new();
                for _ in 0..1 + r.intn(3) {
                    let k = keys[r.intn(keys.len())];
                    if !picked.insert(k.clone()) {
                        continue;
                    }
                    let was = paths[k];
                    let mut want = was + i64::try_from(r.intn(200_001)).unwrap_or(0) - 100_000;
                    if want < 0 {
                        want = was / 2;
                    }
                    want = want / 100 * 100;
                    add_override(&mut f, k, want);
                    chosen.push(format!("{k}={want}"));
                }
                let res = compute(&f).expect("compute");
                for s in &res.message().scenarios {
                    if !s.problems.is_empty() {
                        failures.push(format!(
                            "{name} {} with {chosen:?}: problems {:?}",
                            s.scenario, s.problems
                        ));
                        continue;
                    }
                    for bad in adds_up(s) {
                        failures.push(format!("{name} {} with {chosen:?}: {bad}", s.scenario));
                    }
                }
            }
        }
        assert!(failures.is_empty(), "{}", failures.join("\n"));
    });
}

/// TestOverridesTakeEffect: each figure so far, overridden alone, is the
/// typed figure in the result.
#[test]
fn overrides_take_effect() {
    big_stack(|| {
        let file = read("cases", "full-2026.jsonc");
        let base = compute(&file).expect("compute");
        let paths = overridable(&base.message().scenarios[0]);
        let mut failures = Vec::new();
        for (k, v) in &paths {
            let mut f = file.clone();
            let want = v + 12_300;
            add_override(&mut f, k, want);
            let res = compute(&f).expect("compute");
            let got = overridable(&res.message().scenarios[0]).get(k).copied();
            if got != Some(want) {
                failures.push(format!("overriding {k} to {want}: the result has {got:?}"));
            }
        }
        assert!(!paths.is_empty());
        assert!(failures.is_empty(), "{}", failures.join("\n"));
    });
}

/// TestOverridesReachTheReturn: an override moves every figure that reads
/// it by the same amount.
#[test]
fn overrides_reach_the_return() {
    big_stack(|| {
        let file = read("cases", "full-2026.jsonc");
        let base_plan = plan(&file);
        let with = |path: &str, by: i64| {
            let mut f = file.clone();
            let base = overridable(&base_plan)[path];
            add_override(&mut f, path, base + by);
            plan(&f)
        };
        for (path, reads) in [
            (
                "projection.employers[acme].so_far.items.gross",
                vec![
                    "projection.employers[acme].so_far.boxes.wages",
                    "projection.employers[acme].so_far.boxes.states[CA].wages",
                    "projection.employers[acme].year.boxes.states[CA].wages",
                    "federal.form_1040.w2_wages",
                ],
            ),
            (
                "projection.employers[acme].so_far.items.states[CA].tax_withheld",
                vec![
                    "projection.employers[acme].year.boxes.states[CA].withheld",
                    "summary.salt.withholding",
                    "federal.schedule_a.state_local_income_or_sales_taxes",
                ],
            ),
            (
                "projection.itemised[mortgage].so_far.items.amount",
                vec![
                    "projection.itemised[mortgage].year.boxes.mortgage_interest",
                    "federal.schedule_a.total_mortgage_interest_and_points",
                ],
            ),
        ] {
            let s = with(path, 100_000);
            for at in reads {
                let (was, got) = (figure_at(&base_plan, at), figure_at(&s, at));
                assert!(
                    was.is_some() && got.is_some() && got.unwrap() - was.unwrap() == 100_000,
                    "$1,000 more at {path}: {at} is {got:?}, was {was:?}"
                );
            }
        }
    });
}

/// TestItemisedLimitsAreParts: $1,000,000 of gross pay so far at Acme puts
/// the full household in the 37% bracket. Schedule A line 14 is the gifts
/// less the charity floor, a figure with a working, and line 14's working
/// takes it off. Go skips the § 68 2/37 limit, which the engine lacks.
#[test]
fn itemised_limits_are_parts() {
    big_stack(|| {
        let mut f = read("cases", "full-2026.jsonc");
        add_override(
            &mut f,
            "projection.employers[acme].so_far.items.gross",
            100_000_000,
        );
        let s = plan(&f);
        let bad = adds_up(&s);
        assert!(bad.is_empty(), "{}", bad.join("\n"));
        let floor = figure_at(&s, "summary.itemised[charity].floor").expect("a charity floor");
        assert!(floor > 0, "charity floor {floor}");
        assert_eq!(figure_at(&s, "summary.itemised_limitation"), Some(floor));
        let mut terms = HashMap::new();
        let mut rules = HashMap::new();
        for x in &s.working {
            rules.insert(x.path.as_str(), !x.rules.is_empty());
            if x.path == "federal.schedule_a.total_gifts_to_charity" {
                for t in &x.terms {
                    if let Some(Of::Path(p)) = &t.of {
                        terms.insert(p.clone(), t.sign);
                    }
                }
            }
        }
        assert_eq!(
            terms.get("summary.itemised[charity].floor"),
            Some(&-1),
            "line 14's terms {terms:?}"
        );
        assert_eq!(
            rules.get("summary.itemised[charity].floor"),
            Some(&true),
            "the floor's working"
        );
    });
}

/// TestSocialSecurityOverIsWithholding: a second job from Oct 1 at $20,000
/// a paycheck puts the year's Social Security withheld over the limit, so
/// Schedule 3 credits the excess.
#[test]
fn social_security_over_is_withholding() {
    big_stack(|| {
        let mut f = read("cases", "full-2026.jsonc");
        let oct = commonv1::Date {
            year: 2026,
            month: 10,
            day: 1,
            ..Default::default()
        };
        let pay = |minor| factsv1::PayFigures {
            gross: MessageField::some(usd(minor)),
            ..Default::default()
        };
        f.shared
            .get_or_insert_default()
            .model
            .get_or_insert_default()
            .employers
            .push(EmployerModel {
                id: "added-1".to_string(),
                name: "New job".to_string(),
                person: "you".to_string(),
                pay_frequency: PayFrequency::PAY_FREQUENCY_SEMI_MONTHLY.into(),
                starts: MessageField::some(oct.clone()),
                work: MessageField::some(WorkPlace {
                    place: Some(Place::from(work_place::WhereLived::default())),
                    ..Default::default()
                }),
                supplemental: SupplementalMethod::SUPPLEMENTAL_METHOD_FLAT.into(),
                parts: vec![EmployerPart {
                    id: "added-1-rate".to_string(),
                    part: Some(Part::from(employer_part::PayRate {
                        from: MessageField::some(oct),
                        paycheck: MessageField::some(pay(2_000_000)),
                        ..Default::default()
                    })),
                    ..Default::default()
                }],
                default_part: MessageField::some(EmployerDefault {
                    id: "added-1-default".to_string(),
                    rest: Some(Rest::EachPaycheck(Box::new(pay(0)))),
                    ..Default::default()
                }),
                ..Default::default()
            });
        let s = plan(&f);
        let over = s.federal.schedule_3.excess_social_security_withheld.minor;
        assert!(
            over > 0,
            "Schedule 3 excess Social Security {over}, want some"
        );
        for e in s.estimated.iter().filter(|e| e.jurisdiction == "US") {
            let x = &e.summary;
            assert_eq!(
                x.withholding.minor,
                s.federal.form_1040.total_withheld.minor + over
            );
            assert_eq!(x.social_security_over.minor, over);
            assert_eq!(x.credited.minor, x.withholding.minor + x.paid.minor);
        }
    });
}

/// A typed year total holds against every adjustment: an override of a
/// figure so far, the HSA limit and a workplace plan total change only the
/// paychecks the model works out.
#[test]
fn typed_year_total_holds() {
    big_stack(|| {
        let at = "projection.employers[acme].year.items.";
        for (file, auto) in [
            ("full-year-total-hsa.jsonc", vec![]),
            ("full-year-total-plan.jsonc", vec!["pretax[0].amount"]),
        ] {
            let r = compute(&read("cases", file)).expect("compute");
            let res = r.scenario("plan").expect("plan").proto();
            assert!(
                res.problems.is_empty(),
                "{file}: problems {:?}",
                res.problems
            );
            for (path, want) in [
                ("states[CA].tax_withheld", 792_700),
                ("federal_withheld", 1_863_700),
            ] {
                assert_eq!(
                    figure_at(&res, &format!("{at}{path}")),
                    Some(want),
                    "{file}: {path}"
                );
            }
            for path in auto {
                assert!(
                    res.working
                        .iter()
                        .any(|w| w.path == format!("{at}{path}") && w.automatic.is_set()),
                    "{file}: {path} has no automatic"
                );
            }
        }
    });
}

/// Max-out across a job ending and a typed new one leaves every paycheck
/// to come paying out at least $0, items being whole dollars.
#[test]
fn max_out_across_a_job_change_pays_out() {
    big_stack(|| {
        let r = compute(&read("fixtures", "full-maxout-job-change.jsonc")).expect("compute");
        let s = r.scenario("plan").expect("plan").proto();
        for e in &s.projection.employers {
            for it in &e.items {
                if it.status.as_known() != Some(ItemStatus::ITEM_STATUS_EXPECTED)
                    || it.date.month < 9
                {
                    continue;
                }
                let Some(proto::scenariosv1::__buffa::oneof::item::Figures::Paycheck(c)) =
                    &it.figures
                else {
                    continue;
                };
                let mut out = c.federal_withheld.minor
                    + c.social_security_withheld.minor
                    + c.medicare_withheld.minor;
                out += c.pretax.iter().map(|x| x.amount.minor).sum::<i64>();
                out += c.states.iter().map(|x| x.tax_withheld.minor).sum::<i64>();
                assert!(
                    c.gross.minor - out >= -100,
                    "{} pays out {}",
                    it.id,
                    c.gross.minor - out
                );
            }
        }
    });
}

/// An override of an other income model's taxable amount so far, where
/// its items leave taxable unset and so count their gross, keeps every
/// item adding up to the totals.
#[test]
fn other_income_taxable_override_adds_up() {
    big_stack(|| {
        let mut f = read("cases", "simple-upload-view.jsonc");
        add_override(
            &mut f,
            "projection.other_income[ssa].so_far.items.taxable",
            2_366_000,
        );
        let res = compute(&f).expect("compute");
        for s in &res.message().scenarios {
            assert!(
                s.problems.is_empty(),
                "{}: problems {:?}",
                s.scenario,
                s.problems
            );
            let bad = adds_up(s);
            assert!(bad.is_empty(), "{}: {}", s.scenario, bad.join("\n"));
            assert_eq!(
                figure_at(s, "projection.other_income[ssa].so_far.items.taxable"),
                Some(2_366_000),
                "{}",
                s.scenario
            );
        }
    });
}

/// Every printed total is the sum of its printed lines, in every scenario
/// of every case and fixture.
#[test]
fn every_total_adds_up() {
    big_stack(|| {
        let mut failures = Vec::new();
        for (name, f) in files("cases").into_iter().chain(files("fixtures")) {
            let r = compute(&f).expect("compute");
            for s in &r.message().scenarios {
                for bad in adds_up(s) {
                    failures.push(format!("{name} {}: {bad}", s.scenario));
                }
            }
        }
        assert!(failures.is_empty(), "{}", failures.join("\n"));
    });
}
