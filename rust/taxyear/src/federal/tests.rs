use proto::federalv1::Form1040;

use super::*;

// The overpayment, line 34, divides as the filed return divides it: line
// 36 applied to next year, held to this overpayment, and the rest refunded
// on line 35a. Without a filed return it stays whole.
#[test]
fn split_overpayment() {
    let filed = |applied: i64| Form1040 {
        applied_to_next_year: MessageField::some(model::money(USCents(applied))),
        ..Default::default()
    };
    for (filed, refunded, applied) in [
        (Some(filed(20_500)), 0, 20_500),
        (Some(filed(10_000)), 10_500, 10_000),
        (Some(filed(90_000)), 0, 20_500),
        (None, 0, 0),
    ] {
        let mut r = Return {
            own: Box::new(factsv1::FederalReturn {
                form_1040: MessageField::some(Form1040 {
                    overpaid: MessageField::some(model::money(USCents(20_500))),
                    ..Default::default()
                }),
                ..Default::default()
            }),
            ..Default::default()
        };
        r.split_overpayment(filed.as_ref());
        let f = &r.form().form_1040;
        assert_eq!(
            (f.refunded.minor, f.applied_to_next_year.minor),
            (refunded, applied),
            "filed {filed:?}"
        );
    }
}

// Loading the rules engine succeeds, and only once: the years it has rules
// for are 2025 and 2026.
#[test]
fn load_and_has_rules() {
    load().expect("loading the rules engine");
    assert!(has_rules(2025) && has_rules(2026));
    assert!(!has_rules(2024) && !has_rules(2027));
}

// TestEngineLoadCost: logs what loading the rules engine costs natively,
// the time to parse, typecheck and initialise the rules. Compute loads it
// once per thread.
#[test]
fn engine_load_cost() {
    let start = std::time::Instant::now();
    let eng = returns::rules::federal::load_engine();
    let took = start.elapsed();
    assert!(eng.is_ok(), "{:?}", eng.err());
    println!("rules engine: loaded in {took:?}");
}
