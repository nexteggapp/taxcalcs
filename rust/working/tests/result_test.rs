//! Ported from working/result_test.go.

#![allow(clippy::unwrap_used)]

use taxcalcs_working as working;
use working::{Notes, PositionKind, Reading, Rule};

#[test]
fn result_carries_its_working() {
    let d = working::new_divergence("A rule")
        .form("does one thing", "Form 1")
        .statute("says another", "§ 1")
        .uses(Reading::Form)
        .affects(true)
        .build()
        .unwrap();
    let mut n = Notes::default();
    n.rule("§ 2", "noted first")
        .require(PositionKind::Residency, "residence was not given")
        .diverges(&[d]);
    let r = working::new_result(42)
        .rule("§ 1", "did a thing")
        .with(&n)
        .rule("§ 3", "after the notes")
        .build()
        .unwrap();
    assert_eq!(r.value(), 42);
    let cites: Vec<String> = r.applied().iter().map(Rule::cite).collect();
    assert_eq!(cites, ["§ 1", "§ 2", "§ 3"]);
    assert!(r.needs());
    assert_eq!(r.required()[0].position(), PositionKind::Residency);
    assert_eq!(r.required()[0].because(), "residence was not given");
    assert_eq!(r.divergences().len(), 1);
    assert_eq!(r.divergences()[0].rule(), "A rule");

    // Notes changed after With do not reach the result.
    n.rule("§ 4", "too late");
    assert_eq!(
        r.applied().len(),
        3,
        "a note added after Build reached the result"
    );
}

#[test]
fn result_cannot_be_changed() {
    let mut values = vec![1, 2, 3];
    let r = working::new_result(values.clone())
        .rule("§ 1", "listed them")
        .build()
        .unwrap();
    values[0] = 99;
    let mut got = r.value();
    assert_eq!(got[0], 1);
    got[1] = 99;
    assert_eq!(r.value()[1], 2);
    let mut applied = r.applied();
    applied[0] = Rule::default();
    assert_eq!(r.applied()[0].cite(), "§ 1");
}

#[test]
fn build_refuses_working_nobody_can_check() {
    let cases: Vec<(&str, working::ResultBuilder<i64>)> = vec![
        ("rule without a cite", {
            let mut b = working::new_result(1);
            b.rule("", "did a thing");
            b
        }),
        ("rule without what it did", {
            let mut b = working::new_result(1);
            b.rule("§ 1", "");
            b
        }),
        ("position without a kind", {
            let mut b = working::new_result(1);
            b.require(PositionKind::Unspecified, "because");
            b
        }),
        ("position without a reason", {
            let mut b = working::new_result(1);
            b.require(PositionKind::Residency, "");
            b
        }),
        ("divergence half made", {
            let mut b = working::new_result(1);
            b.diverges(&[working::Divergence::default()]);
            b
        }),
    ];
    for (name, b) in cases {
        assert!(b.build().is_err(), "{name}: Build accepted it");
    }
    assert!(working::new_rule("§ 1").build().is_err());
    let r = working::new_rule("§ 1").applied("did it").build().unwrap();
    assert_eq!((r.cite().as_str(), r.applied().as_str()), ("§ 1", "did it"));
    assert!(
        working::new_required(PositionKind::LotSelection)
            .build()
            .is_err()
    );
    let r = working::new_required(PositionKind::LotSelection)
        .because("FIFO")
        .build()
        .unwrap();
    assert_eq!(r.position(), PositionKind::LotSelection);
}

/// A divergence says which reading the figures follow and whether it changes
/// anything for these facts, and is refused without either.
#[test]
fn divergence_needs_its_reading_and_whether_it_affects() {
    let base = || {
        let mut b = working::new_divergence("A rule");
        b.form("does one thing", "Form 1")
            .statute("says another", "§ 1");
        b
    };
    assert!(base().affects(false).build().is_err());
    assert!(base().uses(Reading::Statute).build().is_err());
    let d = base()
        .uses(Reading::Statute)
        .affects(false)
        .build()
        .unwrap();
    assert_eq!(d.uses(), Reading::Statute);
    assert!(!d.affects_household());
}

/// A position asked as a question carries the question and its effect, and is
/// refused without an effect or with a question that isn't one.
#[test]
fn ask_carries_a_question_and_its_effect() {
    let mut n = Notes::default();
    n.ask(
        PositionKind::ThisYearTax,
        "What do you expect your 2026 tax to be?",
        "Without it, the target can only come from last year's tax.",
    );
    n.ask(
        PositionKind::UnpublishedRate,
        "",
        "Interest after Dec 31, 2026 isn't included until the rate is published.",
    );
    let r = working::new_result(1).with(&n).build().unwrap();
    let q = r.required();
    assert!(q[0].asked());
    assert_eq!(q[0].question(), "What do you expect your 2026 tax to be?");
    assert_eq!(
        q[0].effect(),
        "Without it, the target can only come from last year's tax."
    );
    assert_eq!(q[1].question(), "");
    assert!(!q[1].effect().is_empty());
    assert_eq!(q[1].because(), q[1].effect());

    let bad: [fn(&mut Notes); 2] = [
        |n| {
            n.ask(PositionKind::ThisYearTax, "What is it?", "");
        },
        |n| {
            n.ask(
                PositionKind::ThisYearTax,
                "Your 2026 tax.",
                "It sets the target.",
            );
        },
    ];
    for f in bad {
        let mut n = Notes::default();
        f(&mut n);
        assert!(working::new_result(1).with(&n).build().is_err());
    }
    let mut n = Notes::default();
    n.ask(
        PositionKind::ThisYearTax,
        "Your 2026 tax.",
        "It sets the target.",
    );
    assert_eq!(
        working::new_result(1)
            .with(&n)
            .build()
            .unwrap_err()
            .to_string(),
        "working: \"Your 2026 tax.\" is not a question"
    );
}
