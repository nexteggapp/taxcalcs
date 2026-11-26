//! A person's HSA limit, ported from Go's `taxyear/model/retirement/hsa.go`.

use money::{USCents, dollars};
use proto::factsv1::{__buffa::oneof::position::Answer, HsaCoverage, Reply};

use crate::model::{self, Context, Notes};

/// § 223(b)(2)'s annual limitation by year, self-only and family: Rev. Proc.
/// 2024-25 § 2.01(1) for 2025, Rev. Proc. 2025-19 § 2.01(1) for 2026.
fn hsa_annual(year: i64) -> Option<[USCents; 2]> {
    match year {
        2025 => Some([dollars(4_300), dollars(8_550)]),
        2026 => Some([dollars(4_400), dollars(8_750)]),
        _ => None,
    }
}

/// § 223(b)(3)(B)'s additional contribution amount for 2009 and after, for
/// someone 55 or older by the year's end.
fn hsa_catch_up() -> USCents {
    dollars(1_000)
}

/// The catalogue's question for § 223(b)(8): was the person an eligible
/// individual on December 1, and will they stay one through the testing
/// period, to December 31 of next year?
pub const LAST_MONTH_RULE: &str = "hsa.last_month_rule";

/// A person's HSA limit for the year, § 223(b)(1): the monthly limitations,
/// each a twelfth of the annual one with the catch-up, for the months they
/// had the coverage, months unsaid being the whole year. For someone covered
/// part of the year, the last-month rule, § 223(b)(8), counts every month
/// when they were covered on December 1 and stay covered through the testing
/// period; that is asked, and until it is answered yes the limit counts the
/// months covered. No HSA-eligible plan is a limit of nothing. False while
/// the coverage is unsaid, or without the year's figures.
pub fn hsa_limit(ctx: &Context, person: &str, notes: &mut Notes) -> (USCents, bool) {
    let p = ctx.person(person);
    let (age, aged) = ctx.age_at_year_end(person);
    let mut months = p.map(|p| i64::from(p.hsa_months)).unwrap_or(0);
    if months <= 0 || months > 12 {
        months = 12;
    }
    let coverage = p
        .and_then(|p| p.hsa_coverage.as_known())
        .unwrap_or(HsaCoverage::HSA_COVERAGE_UNSPECIFIED);
    let raw = p.map(|p| p.hsa_coverage.to_i32()).unwrap_or(0);
    if months < 12
        && raw != HsaCoverage::HSA_COVERAGE_NONE as i32
        && raw != HsaCoverage::HSA_COVERAGE_UNSPECIFIED as i32
        && last_month(ctx, person, notes)
    {
        months = 12;
    }
    limit_for(ctx.year(), coverage, months, age, aged)
}

/// The limit for a year, coverage, months covered and age at the year's
/// end.
fn limit_for(
    year: i64,
    coverage: HsaCoverage,
    months: i64,
    age: i64,
    aged: bool,
) -> (USCents, bool) {
    let Some(annual) = hsa_annual(year) else {
        return (USCents(0), false);
    };
    let mut limit = match coverage {
        // Without an HSA-eligible health plan nobody is an eligible
        // individual, § 223(c)(1), so the limit is nothing.
        HsaCoverage::HSA_COVERAGE_NONE => return (USCents(0), true),
        HsaCoverage::HSA_COVERAGE_SELF_ONLY => annual[0],
        HsaCoverage::HSA_COVERAGE_FAMILY => annual[1],
        _ => return (USCents(0), false),
    };
    if aged && age >= 55 {
        limit += hsa_catch_up();
    }
    (limit * months / 12, true)
}

/// Asks the last-month rule's question about the person and reports whether
/// it is answered yes.
fn last_month(ctx: &Context, person: &str, notes: &mut Notes) -> bool {
    let mut name = ctx.person(person).map(|p| p.name.as_str()).unwrap_or("");
    if name.is_empty() {
        name = "you";
    }
    let next = ctx.year() + 1;
    notes.ask(model::new_question(
        format!("{LAST_MONTH_RULE}.{person}"),
        format!("summary.retirement[{person}].hsa_limit"),
        format!(
            "Was {name} covered by an HSA-eligible health plan on December 1, {}, and will that coverage last through December 31, {next}?",
            ctx.year()
        ),
        format!(
            "Until it is answered yes, the HSA limit counts the months covered. Yes counts the whole year, under the last-month rule; leaving the coverage before the end of {next} would put the extra in that year's income, with a 10% additional tax."
        ),
        "facts.positions",
        &["Yes", "No", "Not sure"],
    ));
    for pos in &ctx.facts().positions {
        if pos.question != LAST_MONTH_RULE {
            continue;
        }
        if pos.subjects.iter().any(|s| s == person) {
            return matches!(&pos.answer, Some(Answer::Reply(r)) if *r == Reply::REPLY_YES);
        }
    }
    false
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use proto::{commonv1, factsv1};

    use super::*;

    fn months(m: i32) -> i64 {
        if m <= 0 { 12 } else { i64::from(m) }
    }

    // The HSA limit is the year's annual limitation for the coverage, with
    // the $1,000 catch-up at 55, a twelfth for each month covered: Rev.
    // Proc. 2025-19 § 2.01(1) gives 2026's $4,400 self-only and $8,750
    // family.
    #[test]
    fn hsa_limit_for() {
        let (s, f) = (
            HsaCoverage::HSA_COVERAGE_SELF_ONLY,
            HsaCoverage::HSA_COVERAGE_FAMILY,
        );
        for (year, coverage, m, age, want, ok) in [
            (2026, s, 12, 40, 440_000, true),
            (2026, f, 0, 40, 875_000, true),
            (2026, f, 12, 55, 975_000, true),
            (2026, s, 6, 40, 220_000, true),
            (2025, s, 12, 40, 430_000, true),
            (2025, f, 12, 60, 955_000, true),
            // No HSA-eligible plan: nothing can go in, § 223(c)(1).
            (2026, HsaCoverage::HSA_COVERAGE_NONE, 12, 40, 0, true),
            (
                2026,
                HsaCoverage::HSA_COVERAGE_UNSPECIFIED,
                12,
                40,
                0,
                false,
            ),
            (2027, s, 12, 40, 0, false),
        ] {
            let got = limit_for(year, coverage, months(m), age, true);
            assert_eq!(
                got,
                (USCents(want), ok),
                "{year} {coverage:?} for {m} months at {age}"
            );
        }
    }

    // Covered half the year, the limit is half the year's until the
    // last-month rule's question is answered yes: then it is the whole
    // year's.
    #[test]
    fn hsa_last_month_rule() {
        let mut facts = factsv1::TaxYearFacts {
            tax_year: 2026,
            people: vec![factsv1::Person {
                id: "you".to_string(),
                relationship: factsv1::Relationship::RELATIONSHIP_YOU.into(),
                date_of_birth: commonv1::Date {
                    year: 1980,
                    month: 1,
                    day: 1,
                    ..Default::default()
                }
                .into(),
                hsa_coverage: HsaCoverage::HSA_COVERAGE_FAMILY.into(),
                hsa_months: 6,
                ..Default::default()
            }],
            ..Default::default()
        };
        let limit = |facts: &factsv1::TaxYearFacts| {
            let as_of = commonv1::Date {
                year: 2026,
                month: 9,
                day: 8,
                ..Default::default()
            };
            let ctx = Context::new(2026, Some(&as_of), Some(Arc::new(facts.clone()))).unwrap();
            let mut n = Notes::default();
            let (l, _) = hsa_limit(&ctx, "you", &mut n);
            (l, n)
        };
        let (l, n) = limit(&facts);
        assert_eq!(
            (l, n.questions().len()),
            (USCents(437_500), 1),
            "unanswered"
        );
        facts.positions = vec![factsv1::Position {
            id: "p".to_string(),
            question: LAST_MONTH_RULE.to_string(),
            subjects: vec!["you".to_string()],
            answer: Some(Answer::Reply(Reply::REPLY_YES.into())),
            ..Default::default()
        }];
        assert_eq!(limit(&facts).0, USCents(875_000), "answered yes");
    }
}
