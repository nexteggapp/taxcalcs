//! The protodiffgen golden vectors, which golden/protodiffgen_test.go writes
//! from Go's apply package: every taxyear scenario's changes applied to its
//! parent, and the diff back; diffs the real schema refuses; and the cases
//! of apply's own tests on protodiffgen's test schema.

#![allow(clippy::expect_used, clippy::unwrap_used)]

use std::sync::Arc;

use buffa_descriptor::{DescriptorPool, DynamicMessage, MapKey};
use proto::scenariosv1::{ScenarioInputs, ScenarioInputsDiff, TaxYearModel};
use serde::Deserialize;

use protodiffgen::{
    Item, Target, apply, apply_dynamic, diff, diff_dynamic, equal, equal_dynamic, overlap_dynamic,
    to_dynamic, validate, validate_dynamic,
};
use taxcalcs_protodiffgen as protodiffgen;

#[derive(Deserialize)]
struct Scenario {
    id: String,
    parent: i64,
    changes: i64,
    result: i64,
    error: String,
    diff: i64,
    diff_error: String,
}

#[derive(Deserialize)]
struct Case {
    file: String,
    scenarios: Vec<Scenario>,
}

#[derive(Deserialize)]
struct Scenarios {
    messages: Vec<String>,
    cases: Vec<Case>,
}

fn msg<M: buffa::Message + Default>(messages: &[String], i: i64) -> M {
    golden::decode(&messages[usize::try_from(i).expect("an index")])
}

#[test]
fn scenarios() {
    let v: Scenarios = golden::read("protodiffgen/scenarios.json");
    let mut n = 0;
    for c in &v.cases {
        for s in &c.scenarios {
            let at = format!("{} {}", c.file, s.id);
            let parent: ScenarioInputs = msg(&v.messages, s.parent);
            let changes: ScenarioInputsDiff = msg(&v.messages, s.changes);
            let got = apply(&parent, &changes);
            if !s.error.is_empty() {
                assert_eq!(
                    got.err().map(|e| e.to_string()),
                    Some(s.error.clone()),
                    "{at}"
                );
                continue;
            }
            let got = got.unwrap_or_else(|e| panic!("{at}: {e}"));
            let want: ScenarioInputs = msg(&v.messages, s.result);
            golden::assert_same(&at, &got, &want);
            let mut d = ScenarioInputsDiff::default();
            let err = diff(&parent, &got, &mut d);
            if !s.diff_error.is_empty() {
                assert_eq!(
                    err.err().map(|e| e.to_string()),
                    Some(s.diff_error.clone()),
                    "{at}"
                );
                continue;
            }
            err.unwrap_or_else(|e| panic!("{at}: diff: {e}"));
            let want_d: ScenarioInputsDiff = msg(&v.messages, s.diff);
            golden::assert_same(&format!("{at} diff"), &d, &want_d);
            let back =
                apply(&parent, &d).unwrap_or_else(|e| panic!("{at}: applying the diff: {e}"));
            assert!(
                equal(&back, &want, &d),
                "{at}: Diff then Apply doesn't round-trip"
            );
            n += 1;
        }
    }
    assert!(n > 40, "only {n} scenarios");
}

#[derive(Deserialize)]
struct Refusal {
    name: String,
    parent: i64,
    changes: i64,
    error: String,
    result: i64,
}

#[derive(Deserialize)]
struct Typed {
    name: String,
    error: String,
}

#[derive(Deserialize)]
struct Refusals {
    messages: Vec<String>,
    refusals: Vec<Refusal>,
    typed: Vec<Typed>,
}

#[test]
fn refusals() {
    let v: Refusals = golden::read("protodiffgen/refusals.json");
    for r in &v.refusals {
        let parent: ScenarioInputs = msg(&v.messages, r.parent);
        let changes: ScenarioInputsDiff = msg(&v.messages, r.changes);
        let got = apply(&parent, &changes);
        let verr = validate(&parent, &changes)
            .err()
            .map(|e| e.to_string())
            .unwrap_or_default();
        assert_eq!(verr, r.error, "{}: validate", r.name);
        match got {
            Err(e) => assert_eq!(e.to_string(), r.error, "{}", r.name),
            Ok(got) => {
                assert_eq!(r.error, "", "{}: Rust applied the diff", r.name);
                let want: ScenarioInputs = msg(&v.messages, r.result);
                golden::assert_same(&r.name, &got, &want);
            }
        }
    }
    // The inputs Go's typed cases use, rebuilt: full-2026's shared inputs,
    // with a person twice and with unknown fields in the model.
    let dup = v
        .refusals
        .iter()
        .find(|r| r.name == "a with two items for one key")
        .expect("the case");
    let unknown = v
        .refusals
        .iter()
        .find(|r| r.name == "unknown fields in a")
        .expect("the case");
    let shared = v
        .refusals
        .iter()
        .find(|r| r.name == "removes a key the list lacks")
        .expect("the case");
    let dup: ScenarioInputs = msg(&v.messages, dup.parent);
    let unknown: ScenarioInputs = msg(&v.messages, unknown.parent);
    let shared: ScenarioInputs = msg(&v.messages, shared.parent);
    let err =
        |r: Result<(), protodiffgen::Error>| r.err().map(|e| e.to_string()).unwrap_or_default();
    let diff_err = |a: &ScenarioInputs, b: &ScenarioInputs| {
        let mut d = ScenarioInputsDiff::default();
        err(diff(a, b, &mut d))
    };
    for t in &v.typed {
        let got = match t.name.as_str() {
            "Apply with a diff that is not a patch body" => {
                err(apply(&ScenarioInputs::default(), &ScenarioInputs::default()).map(|_| ()))
            }
            "Validate with a target of another type" => err(validate(
                &TaxYearModel::default(),
                &ScenarioInputsDiff::default(),
            )),
            "Diff with a and b of different types" => {
                let mut out = to_dynamic(&ScenarioInputsDiff::default()).expect("a diff");
                err(diff_dynamic(
                    &to_dynamic(&ScenarioInputs::default()).expect("inputs"),
                    &to_dynamic(&TaxYearModel::default()).expect("a model"),
                    &mut out,
                ))
            }
            "Diff with a diff for another target" => {
                let mut out = ScenarioInputsDiff::default();
                err(diff(
                    &TaxYearModel::default(),
                    &TaxYearModel::default(),
                    &mut out,
                ))
            }
            "Diff with a with two items for one key" => diff_err(&dup, &shared),
            "Diff with b with two items for one key" => diff_err(&shared, &dup),
            "Diff with a with unknown fields" => diff_err(&unknown, &shared),
            other => panic!("no Rust for the typed case {other}"),
        };
        assert_eq!(got, t.error, "{}", t.name);
    }
}

// The test schema.

#[derive(Deserialize)]
struct Check {
    name: String,
    a: String,
    d: String,
    error: String,
    result: String,
}

#[derive(Deserialize)]
struct DiffCase {
    name: String,
    a: String,
    b: String,
    diff: String,
    error: String,
}

#[derive(Deserialize)]
struct EqualCase {
    x: String,
    y: String,
    equal: bool,
}

#[derive(Deserialize, Debug, Clone, PartialEq)]
struct StepV {
    list: String,
    key: String,
    index: i64,
}

#[derive(Deserialize, Debug, Clone, PartialEq)]
struct ItemV {
    text: String,
    field: u32,
    path: Vec<StepV>,
}

#[derive(Deserialize)]
struct OverlapCase {
    name: String,
    d1: String,
    d2: String,
    items: Option<Vec<ItemV>>,
    back: Option<Vec<ItemV>>,
    error: String,
}

#[derive(Deserialize)]
struct Pair {
    a: usize,
    b: usize,
    diff: String,
    error: String,
}

#[derive(Deserialize)]
struct TestSchema {
    schema: String,
    validate: Vec<Check>,
    apply: Vec<Check>,
    diff: Vec<DiffCase>,
    equal: Vec<EqualCase>,
    overlap: Vec<OverlapCase>,
    target: Vec<Check>,
    roots: Vec<String>,
    pairs: Vec<Pair>,
}

struct Schema {
    pool: Arc<DescriptorPool>,
}

impl Schema {
    fn decode(&self, name: &str, b64: &str) -> DynamicMessage {
        let idx = self
            .pool
            .message_index(name)
            .expect("a message of the test schema");
        DynamicMessage::decode(Arc::clone(&self.pool), idx, &golden::bytes(b64)).expect("a message")
    }
    fn root(&self, b64: &str) -> DynamicMessage {
        self.decode("pdgtest.v1.Root", b64)
    }
    fn diff(&self, b64: &str) -> DynamicMessage {
        self.decode("pdgtest.v1.diff.RootDiff", b64)
    }
}

/// m as Rust encodes it once decoded again, so two messages with the same
/// fields compare equal however they were built.
fn canon(m: &DynamicMessage) -> Vec<u8> {
    DynamicMessage::decode(Arc::clone(m.pool()), m.message_index(), &m.encode_to_vec())
        .expect("a message")
        .encode_to_vec()
}

#[track_caller]
fn assert_msg(at: &str, got: &DynamicMessage, want: &DynamicMessage) {
    if canon(got) != canon(want) {
        panic!("{at}: Rust differs from Go\n--- Go\n{want:#?}\n--- Rust\n{got:#?}");
    }
}

fn err_text<T>(r: &Result<T, protodiffgen::Error>) -> String {
    r.as_ref().err().map(|e| e.to_string()).unwrap_or_default()
}

fn items(its: &[Item]) -> Vec<ItemV> {
    its.iter()
        .map(|it| ItemV {
            text: it.to_string(),
            field: it.field(),
            path: it
                .path()
                .iter()
                .map(|s| StepV {
                    list: s.list().to_string(),
                    key: s.key().map(key_text).unwrap_or_default(),
                    index: s.index(),
                })
                .collect(),
        })
        .collect()
}

fn key_text(k: &MapKey) -> String {
    match k {
        MapKey::Bool(b) => b.to_string(),
        MapKey::I32(n) => n.to_string(),
        MapKey::I64(n) => n.to_string(),
        MapKey::U32(n) => n.to_string(),
        MapKey::U64(n) => n.to_string(),
        MapKey::String(s) => s.clone(),
    }
}

fn test_schema() -> (TestSchema, Schema) {
    let v: TestSchema = golden::read("protodiffgen/pdgtest.json");
    let pool = DescriptorPool::decode(&golden::bytes(&v.schema)).expect("the test schema");
    (
        v,
        Schema {
            pool: Arc::new(pool),
        },
    )
}

#[test]
fn test_schema_validate() {
    let (v, s) = test_schema();
    for c in &v.validate {
        let (a, d) = (s.root(&c.a), s.diff(&c.d));
        assert_eq!(
            err_text(&validate_dynamic(&a, &d)),
            c.error,
            "{}: validate",
            c.name
        );
        let got = apply_dynamic(&a, &d);
        assert_eq!(err_text(&got), c.error, "{}: apply", c.name);
        if let Ok(got) = got {
            assert_msg(&c.name, &got, &s.root(&c.result));
        }
    }
}

#[test]
fn test_schema_apply() {
    let (v, s) = test_schema();
    for c in &v.apply {
        let (a, d) = (s.root(&c.a), s.diff(&c.d));
        let before = canon(&a);
        let got = apply_dynamic(&a, &d).unwrap_or_else(|e| panic!("{}: {e}", c.name));
        assert_msg(&c.name, &got, &s.root(&c.result));
        assert_eq!(canon(&a), before, "{}: Apply changed a", c.name);
    }
}

#[test]
fn test_schema_diff() {
    let (v, s) = test_schema();
    for c in &v.diff {
        let (a, b) = (s.root(&c.a), s.root(&c.b));
        let mut out = s.diff("");
        let got = diff_dynamic(&a, &b, &mut out);
        assert_eq!(err_text(&got), c.error, "{}", c.name);
        if got.is_ok() {
            assert_msg(&c.name, &out, &s.diff(&c.diff));
        }
    }
    // Diff refuses a and b of different types, a diff type for another
    // target, and an out that is no diff.
    let member = s.decode("pdgtest.v1.Member", "");
    let mut out = s.diff("");
    assert_eq!(
        err_text(&diff_dynamic(&s.root(""), &member, &mut out)),
        "a is pdgtest.v1.Root but b is pdgtest.v1.Member"
    );
    assert_eq!(
        err_text(&diff_dynamic(&member, &member, &mut out)),
        "pdgtest.v1.diff.RootDiff diffs pdgtest.v1.Root, not pdgtest.v1.Member"
    );
    let mut root = s.root("");
    assert_eq!(
        err_text(&diff_dynamic(&s.root(""), &s.root(""), &mut root)),
        "pdgtest.v1.Root is not a protodiffgen patch body"
    );
}

#[test]
fn test_schema_equal() {
    let (v, s) = test_schema();
    let d = s.diff("");
    for c in &v.equal {
        let (x, y) = (s.root(&c.x), s.root(&c.y));
        assert_eq!(equal_dynamic(&x, &y, &d), c.equal, "Equal({x:?}, {y:?})");
    }
}

#[test]
fn test_schema_overlap() {
    let (v, s) = test_schema();
    for c in &v.overlap {
        let (d1, d2) = (
            s.diff(&c.d1),
            if c.name == "diffs of two types" {
                s.decode("pdgtest.v1.diff.MemberDiff", &c.d2)
            } else {
                s.diff(&c.d2)
            },
        );
        let got = overlap_dynamic(&d1, &d2);
        assert_eq!(err_text(&got), c.error, "{}", c.name);
        let Ok(got) = got else { continue };
        assert_eq!(
            items(&got),
            c.items.clone().unwrap_or_default(),
            "{}",
            c.name
        );
        let back = overlap_dynamic(&d2, &d1).unwrap_or_else(|e| panic!("{}: {e}", c.name));
        assert_eq!(
            items(&back),
            c.back.clone().unwrap_or_default(),
            "{}: back",
            c.name
        );
    }
}

#[test]
fn test_schema_target() {
    let (v, s) = test_schema();
    let body = s
        .pool
        .message_index("pdgtest.v1.diff.RootDiff")
        .expect("RootDiff");
    for c in &v.target {
        let got = match c.name.as_str() {
            "the zero Target" => Target::default().apply(&s.diff("")).map(|_| String::new()),
            "a diff of the wrong type" => {
                let t = Target::new(Arc::new(s.root(&c.a)), &s.pool, body).expect("a target");
                t.apply(&s.root(&c.d)).map(|_| String::new())
            }
            _ => {
                let a = Arc::new(s.root(&c.a));
                let before = canon(&a);
                match Target::new(Arc::clone(&a), &s.pool, body) {
                    Err(e) => Err(protodiffgen::Error::Refused(format!("NewTarget: {e}"))),
                    Ok(t) => {
                        let r = t.apply(&s.diff(&c.d));
                        assert_eq!(
                            canon(&a),
                            before,
                            "{}: Target.Apply changed its message",
                            c.name
                        );
                        match r {
                            Ok(next) => {
                                let m = next.message().expect("a message");
                                assert_msg(&c.name, m, &s.root(&c.result));
                                // The same as Apply.
                                let want = apply_dynamic(&a, &s.diff(&c.d)).expect("Apply");
                                assert_msg(&c.name, m, &want);
                                Ok(String::new())
                            }
                            Err(e) => {
                                assert_eq!(
                                    err_text(&apply_dynamic(&a, &s.diff(&c.d))),
                                    e.to_string(),
                                    "{}: Apply and Target.Apply differ",
                                    c.name
                                );
                                Err(protodiffgen::Error::Refused(format!("Apply: {e}")))
                            }
                        }
                    }
                }
            }
        };
        let text = match (&got, c.name.as_str()) {
            (Err(e), "the zero Target" | "a diff of the wrong type") => format!("Apply: {e}"),
            (Err(e), _) => e.to_string(),
            (Ok(_), _) => String::new(),
        };
        assert_eq!(text, c.error, "{}", c.name);
    }
}

#[test]
fn test_schema_pairs() {
    let (v, s) = test_schema();
    let roots: Vec<DynamicMessage> = v.roots.iter().map(|r| s.root(r)).collect();
    for p in &v.pairs {
        let (a, b) = (&roots[p.a], &roots[p.b]);
        let mut out = s.diff("");
        let got = diff_dynamic(a, b, &mut out);
        assert_eq!(err_text(&got), p.error, "pair {} {}", p.a, p.b);
        if got.is_err() {
            continue;
        }
        assert_msg(&format!("pair {} {}", p.a, p.b), &out, &s.diff(&p.diff));
        let back = apply_dynamic(a, &out).unwrap_or_else(|e| panic!("pair {} {}: {e}", p.a, p.b));
        assert!(
            equal_dynamic(&back, b, &out),
            "pair {} {}: Diff then Apply doesn't round-trip",
            p.a,
            p.b
        );
        assert_eq!(
            equal_dynamic(a, b, &out),
            protodiffgen_is_empty(&out),
            "pair {} {}: Equal(a, b) disagrees with the diff",
            p.a,
            p.b
        );
    }
}

fn protodiffgen_is_empty(m: &DynamicMessage) -> bool {
    use buffa_descriptor::ReflectMessage as _;
    let mut empty = true;
    m.for_each_set(&mut |_, _| empty = false);
    empty
}
