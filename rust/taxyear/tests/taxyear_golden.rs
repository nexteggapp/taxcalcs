//! The taxyear vectors in golden/testdata/taxyear, which
//! golden/taxyear_test.go writes: every TaxYearScenarios file the taxyear
//! tests read, parsed and computed (files.json.gz); Parse of the doc
//! example with one edit each (parse.json); cache_test.go's edits computed
//! one after another through two caches (cache.json.gz); and, written on
//! feat/wasm-entry by golden/taxyear_noworking_test.go, every file computed
//! without the engine's working (no_working.json.gz).
//!
//! Go writes "proto:" and a space, U+0020 or U+00A0 by build, before a
//! protobuf error; the vectors hold U+0020, as the port writes.

#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::io::Read as _;

use proto::scenariosv1::{ScenarioResult, TaxYearResult, TaxYearScenarios};
use serde::Deserialize;
use taxcalcs_taxyear::{Cache, CacheOption};

#[derive(Deserialize)]
struct FileVector {
    file: String,
    parsed: String,
    parse_error: String,
    result: String,
    compute_error: String,
}

#[derive(Deserialize)]
struct ParseVector {
    name: String,
    old: String,
    new: String,
    text: String,
    #[serde(default)]
    text_bytes: String,
    parsed: String,
    error: String,
}

#[derive(Deserialize)]
struct CacheVector {
    name: String,
    old: String,
    new: String,
    small: String,
    large: String,
}

#[derive(Deserialize)]
struct NoWorkingVector {
    file: String,
    result: String,
    error: String,
}

fn gz<T: serde::de::DeserializeOwned>(name: &str) -> Option<T> {
    let p = golden::path(name);
    let f = std::fs::File::open(&p).ok()?;
    let mut text = String::new();
    flate2::read::GzDecoder::new(f)
        .read_to_string(&mut text)
        .unwrap_or_else(|e| panic!("decompressing {}: {e}", p.display()));
    Some(serde_json::from_str(&text).unwrap_or_else(|e| panic!("parsing {}: {e}", p.display())))
}

fn example() -> String {
    let p = golden::repo_path("taxyear/testdata/example.jsonc");
    std::fs::read_to_string(&p).unwrap_or_else(|e| panic!("reading {}: {e}", p.display()))
}

fn read_file(rel: &str) -> Vec<u8> {
    let p = golden::repo_path(rel);
    std::fs::read(&p).unwrap_or_else(|e| panic!("reading {}: {e}", p.display()))
}

/// Runs f on a thread with a large stack: the messages are large for a
/// debug build's test thread.
fn big_stack<F: FnOnce() + Send + 'static>(f: F) {
    std::thread::Builder::new()
        .stack_size(256 << 20)
        .spawn(f)
        .expect("spawn")
        .join()
        .expect("the test thread panicked");
}

/// Names the parts of a scenario's result that differ, and the module each
/// comes from, for a failure's message.
fn scenario_differences(got: &ScenarioResult, want: &ScenarioResult) -> Vec<String> {
    let mut out = Vec::new();
    let mut check = |name: &str, module: &str, same: bool| {
        if !same {
            out.push(format!("{name} ({module})"));
        }
    };
    check("problems", "taxyear", got.problems == want.problems);
    check(
        "dependents",
        "taxyear dependents",
        got.dependents == want.dependents,
    );
    let (gp, wp) = (&got.projection, &want.projection);
    check(
        "projection.employers",
        "model/employer",
        gp.employers == wp.employers,
    );
    check(
        "projection.businesses",
        "model/business",
        gp.businesses == wp.businesses,
    );
    check(
        "projection.accounts",
        "model/account",
        gp.accounts == wp.accounts,
    );
    check(
        "projection.brokers",
        "model/broker",
        gp.brokers == wp.brokers,
    );
    check(
        "projection.retirement",
        "model/retirement",
        gp.retirement == wp.retirement,
    );
    check(
        "projection.itemised",
        "model/itemised",
        gp.itemised == wp.itemised,
    );
    check(
        "projection.other_income",
        "model/otherincome",
        gp.other_income == wp.other_income,
    );
    check(
        "projection.tax_paid",
        "model/taxpaid, paid",
        gp.tax_paid == wp.tax_paid,
    );
    check("projection (rest)", "taxyear", **gp == **wp);
    check("federal", "federal", got.federal == want.federal);
    check(
        "estimated",
        "paid, taxyear summary",
        got.estimated == want.estimated,
    );
    check("states", "taxyear", got.states == want.states);
    check(
        "summary",
        "federal summary, taxyear summary",
        got.summary == want.summary,
    );
    check("questions", "all", got.questions == want.questions);
    check("not_included", "all", got.not_included == want.not_included);
    check(
        "working",
        "taxyear working, federal working, model workings",
        got.working == want.working,
    );
    out
}

/// Compares two results, failing with which scenarios differ, where, and
/// the first differing line.
fn same_result(context: &str, got: &TaxYearResult, want: &TaxYearResult) -> Result<(), String> {
    if got == want {
        return Ok(());
    }
    let mut lines = vec![format!("{context}: the result differs")];
    if got.problems != want.problems {
        lines.push(format!(
            "  file problems: got {:?}, want {:?}",
            got.problems, want.problems
        ));
    }
    if got.scenarios.len() != want.scenarios.len() {
        lines.push(format!(
            "  {} scenarios, want {}",
            got.scenarios.len(),
            want.scenarios.len()
        ));
    }
    for (g, w) in got.scenarios.iter().zip(&want.scenarios) {
        let d = scenario_differences(g, w);
        if !d.is_empty() {
            lines.push(format!("  {}: {}", w.scenario, d.join(", ")));
            let (gs, ws) = (format!("{g:#?}"), format!("{w:#?}"));
            if let Some((i, (a, b))) = gs
                .lines()
                .zip(ws.lines())
                .enumerate()
                .find(|(_, (a, b))| a != b)
            {
                let start = i.saturating_sub(12);
                let ctx: Vec<&str> = ws.lines().skip(start).take(i - start).collect();
                lines.push(format!(
                    "    at line {i}:\n{}\n    got:  {a}\n    want: {b}",
                    ctx.join("\n")
                ));
            }
        }
    }
    let rest_differs = got.schema_version != want.schema_version
        || got.tax_year != want.tax_year
        || got.as_of != want.as_of
        || got.computed_by != want.computed_by
        || got.status != want.status;
    if rest_differs {
        lines.push("  the header differs".to_string());
    }
    Err(lines.join("\n"))
}

#[test]
fn parse_cases() {
    let example = example();
    let cases: Vec<ParseVector> = golden::read("taxyear/parse.json");
    let mut failures = Vec::new();
    for c in &cases {
        let text = if !c.text_bytes.is_empty() {
            golden::bytes(&c.text_bytes)
        } else if c.old.is_empty() {
            c.text.clone().into_bytes()
        } else {
            assert!(
                example.contains(&c.old),
                "{}: the example has no {:?}",
                c.name,
                c.old
            );
            example.replacen(&c.old, &c.new, 1).into_bytes()
        };
        match taxcalcs_taxyear::parse(&text) {
            Ok(f) => {
                if !c.error.is_empty() {
                    failures.push(format!("{}: parsed, want error {:?}", c.name, c.error));
                } else if f != golden::decode::<TaxYearScenarios>(&c.parsed) {
                    failures.push(format!("{}: the parsed file differs", c.name));
                }
            }
            Err(e) => {
                if e.to_string() != c.error {
                    failures.push(format!(
                        "{}: error\n  got  {:?}\n  want {:?}",
                        c.name,
                        e.to_string(),
                        c.error
                    ));
                }
            }
        }
    }
    assert!(
        failures.is_empty(),
        "{} of {} cases:\n{}",
        failures.len(),
        cases.len(),
        failures.join("\n")
    );
}

#[test]
fn files_parse() {
    let vs: Vec<FileVector> = gz("taxyear/files.json.gz").expect("taxyear/files.json.gz");
    let mut failures = Vec::new();
    for v in &vs {
        match taxcalcs_taxyear::parse(&read_file(&v.file)) {
            Ok(f) => {
                if !v.parse_error.is_empty() {
                    failures.push(format!("{}: parsed, want {:?}", v.file, v.parse_error));
                } else if f != golden::decode::<TaxYearScenarios>(&v.parsed) {
                    failures.push(format!("{}: the parsed file differs", v.file));
                }
            }
            Err(e) => {
                if e.to_string() != v.parse_error {
                    failures.push(format!("{}: {e}, want {:?}", v.file, v.parse_error));
                }
            }
        }
    }
    assert!(failures.is_empty(), "{}", failures.join("\n"));
}

/// The files to compute: all of them, or those whose path holds
/// TAXYEAR_GOLDEN_FILTER.
fn wanted(file: &str) -> bool {
    match std::env::var("TAXYEAR_GOLDEN_FILTER") {
        Ok(f) if !f.is_empty() => file.contains(&f),
        _ => true,
    }
}

#[test]
fn files_compute() {
    big_stack(|| {
        let vs: Vec<FileVector> = gz("taxyear/files.json.gz").expect("taxyear/files.json.gz");
        let mut failures = Vec::new();
        let mut matched = Vec::new();
        for v in vs
            .iter()
            .filter(|v| wanted(&v.file) && v.parse_error.is_empty())
        {
            let file: TaxYearScenarios = golden::decode(&v.parsed);
            match taxcalcs_taxyear::compute(&file) {
                Ok(r) => match same_result(&v.file, r.message(), &golden::decode(&v.result)) {
                    Ok(()) => matched.push(v.file.clone()),
                    Err(e) => failures.push(e),
                },
                Err(e) => {
                    if e.to_string() != v.compute_error {
                        failures.push(format!("{}: {e}, want {:?}", v.file, v.compute_error));
                    }
                }
            }
        }
        eprintln!(
            "taxyear files: {} match: {}",
            matched.len(),
            matched.join(", ")
        );
        assert!(
            failures.is_empty(),
            "{} files differ:\n{}",
            failures.len(),
            failures.join("\n\n")
        );
    });
}

#[test]
fn files_compute_without_working() {
    big_stack(|| {
        let Some(vs) = gz::<Vec<NoWorkingVector>>("taxyear/no_working.json.gz") else {
            eprintln!("no taxyear/no_working.json.gz; it is written on feat/wasm-entry");
            return;
        };
        let mut failures = Vec::new();
        for v in vs.iter().filter(|v| wanted(&v.file)) {
            let file = taxcalcs_taxyear::parse(&read_file(&v.file)).expect("the file parses");
            let c = Cache::new(
                file.scenarios.len() + 1,
                &[CacheOption::WithoutEngineWorking],
            );
            match c.compute(&file) {
                Ok(r) => {
                    if let Err(e) = same_result(&v.file, r.message(), &golden::decode(&v.result)) {
                        failures.push(e);
                    }
                }
                Err(e) => {
                    if e.to_string() != v.error {
                        failures.push(format!("{}: {e}, want {:?}", v.file, v.error));
                    }
                }
            }
        }
        assert!(
            failures.is_empty(),
            "{} files differ:\n{}",
            failures.len(),
            failures.join("\n\n")
        );
    });
}

#[test]
fn cache_variants() {
    big_stack(|| {
        let example = example();
        let vs: Vec<CacheVector> = gz("taxyear/cache.json.gz").expect("taxyear/cache.json.gz");
        let small = Cache::new(1, &[]);
        let large = Cache::new(64, &[]);
        let mut failures = Vec::new();
        for v in &vs {
            let text = if v.old.is_empty() {
                example.clone()
            } else {
                example.replacen(&v.old, &v.new, 1)
            };
            let f = taxcalcs_taxyear::parse(text.as_bytes()).expect("the variant parses");
            for (name, c, want) in [("small", &small, &v.small), ("large", &large, &v.large)] {
                let r = c.compute(&f).expect("compute");
                if let Err(e) = same_result(
                    &format!("{}, {name}", v.name),
                    r.message(),
                    &golden::decode(want),
                ) {
                    failures.push(e);
                }
            }
        }
        assert!(failures.is_empty(), "{}", failures.join("\n\n"));
    });
}

#[derive(Deserialize)]
struct OverrideVector {
    file: String,
    path: String,
    amount: i64,
    plan: String,
}

#[test]
fn overrides() {
    big_stack(|| {
        let vs: Vec<OverrideVector> =
            gz("taxyear/overrides.json.gz").expect("taxyear/overrides.json.gz");
        let mut failures = Vec::new();
        for v in &vs {
            let mut f = taxcalcs_taxyear::parse(&read_file(&v.file)).expect("the file parses");
            f.shared
                .get_or_insert_default()
                .overrides
                .push(proto::scenariosv1::Override {
                    path: v.path.clone(),
                    amount: buffa::MessageField::some(proto::commonv1::Money {
                        minor: v.amount,
                        currency: "USD".to_string(),
                        ..Default::default()
                    }),
                    ..Default::default()
                });
            let r = taxcalcs_taxyear::compute(&f).expect("compute");
            let got = TaxYearResult {
                scenarios: vec![r.message().scenarios[0].clone()],
                ..Default::default()
            };
            let want = TaxYearResult {
                scenarios: vec![golden::decode(&v.plan)],
                ..Default::default()
            };
            if let Err(e) = same_result(&format!("{} with {}", v.file, v.path), &got, &want) {
                failures.push(e);
            }
        }
        assert!(
            failures.is_empty(),
            "{} of {} differ:\n{}",
            failures.len(),
            vs.len(),
            failures.join("\n\n")
        );
    });
}

#[derive(Deserialize)]
struct FixtureVector {
    test: String,
    file: String,
    result: String,
    error: String,
}

/// The households taxyear's models_test.go, new_forms_test.go and
/// property_test.go build in code, computed as those tests compute them.
#[test]
fn fixtures() {
    big_stack(|| {
        let vs: Vec<FixtureVector> =
            gz("taxyear/fixtures.json.gz").expect("taxyear/fixtures.json.gz");
        let mut failures = Vec::new();
        for (i, v) in vs.iter().enumerate() {
            let f: TaxYearScenarios = golden::decode(&v.file);
            match taxcalcs_taxyear::compute(&f) {
                Ok(r) => {
                    if let Err(e) = same_result(
                        &format!("{} #{i}", v.test),
                        r.message(),
                        &golden::decode(&v.result),
                    ) {
                        failures.push(e);
                    }
                }
                Err(e) => {
                    if e.to_string() != v.error {
                        failures.push(format!("{} #{i}: {e}, want {:?}", v.test, v.error));
                    }
                }
            }
        }
        assert!(
            failures.is_empty(),
            "{} of {} differ:\n{}",
            failures.len(),
            vs.len(),
            failures.join("\n\n")
        );
    });
}
