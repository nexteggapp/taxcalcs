//! Ported from estimated/answers_test.go: the answers' invariants,
//! exceptions as options, one status rule, and a penalty reason that says
//! only what the figures show.

#![allow(clippy::unwrap_used)]

mod common;

use common::*;
use estimated::{Basis, PartKind, Status, WaiverCause, WaiverKind};
use money::{DOLLAR, Rate, USCents};

/// An exception that compares an amount with a threshold can only be made
/// when the comparison holds, so its label is true of the figures.
#[test]
fn exception_labels_are_true_of_their_figures() {
    let o =
        estimated::exception_below("Tax less withholding", DOLLAR * 800, DOLLAR * 1_000).unwrap();
    assert_eq!(o.label(), "Tax less withholding under $1,000");
    assert_eq!(o.basis(), Basis::Exception);
    assert_eq!(o.amount(), USCents(0));
    assert!(o.available());
    // $5,742 is not under $1,000, and $1,000 is not under itself.
    for a in [DOLLAR * 5_742, DOLLAR * 1_000] {
        assert!(estimated::exception_below("Tax less withholding", a, DOLLAR * 1_000).is_err());
    }
    assert_eq!(
        estimated::exception_below("Tax less withholding", DOLLAR * 5_742, DOLLAR * 1_000)
            .unwrap_err()
            .to_string(),
        "estimated: Tax less withholding of $5742.00 is not under $1000.00"
    );
    let o =
        estimated::exception_at_most("Tax less withholding", DOLLAR * 500, DOLLAR * 500).unwrap();
    assert_eq!(o.label(), "Tax less withholding of $500 or less");
    assert!(
        estimated::exception_at_most("Tax less withholding", USCents(50_001), DOLLAR * 500)
            .is_err()
    );
    // A hand-written label names no figure.
    assert_eq!(
        estimated::new_exception("Tax under $1,000")
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: exception \"Tax under $1,000\" names a figure; use ExceptionBelow or ExceptionAtMost"
    );
}

#[test]
fn options_are_labelled_by_basis() {
    for (b, pct, want) in [
        (Basis::ThisYearTax, 9000, "90% of this year's tax"),
        (Basis::PriorYearTax, 11_000, "110% of last year's tax"),
        (Basis::ThisYearTax, 6667, "66.67% of this year's tax"),
    ] {
        let o = estimated::new_target_option(b, Rate(pct), USCents(100))
            .build()
            .unwrap();
        assert_eq!(o.label(), want);
    }
    assert!(
        estimated::new_target_option(Basis::Exception, Rate(0), USCents(0))
            .build()
            .is_err(),
        "an exception with no label was built"
    );
    // An option that isn't open carries no amount.
    assert_eq!(
        estimated::new_target_option(Basis::PriorYearTax, Rate(10_000), USCents(500))
            .unavailable("You didn't file a 2025 return")
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: the unavailable last year's tax option carries $5.00; an option that isn't open has no amount"
    );
}

/// An exception is the chosen $0 target, and an exception listed beside the
/// options without being chosen is refused.
#[test]
fn exception_is_the_only_chosen_option() {
    let ninety = estimated::new_target_option(Basis::ThisYearTax, Rate(9000), DOLLAR * 5_827)
        .build()
        .unwrap();
    let last = estimated::new_target_option(Basis::PriorYearTax, Rate(10_000), DOLLAR * 6_100)
        .build()
        .unwrap();
    let under =
        estimated::exception_below("Tax less withholding", DOLLAR * 600, DOLLAR * 1_000).unwrap();
    let r = estimated::new_requirement()
        .required(false, "x")
        .option(ninety.clone())
        .option(last)
        .exempt(under.clone())
        .build()
        .unwrap();
    let c = r.chosen().unwrap();
    assert_eq!(c.label(), under.label());
    assert_eq!(r.target(), USCents(0));
    assert_eq!(r.options().len(), 3);
    assert!(
        estimated::new_requirement()
            .required(false, "x")
            .option(ninety)
            .option(under)
            .choose(Basis::ThisYearTax)
            .build()
            .is_err(),
        "an exception was listed without being chosen"
    );
}

#[test]
fn status_of() {
    for (needed, credited, excused, want) in [
        (0, 0, false, Status::NothingDue),
        (0, 500, true, Status::NothingDue),
        (1_000, 1_000, false, Status::Met),
        (1_000, 1_200, true, Status::Met),
        (1_000, 999, false, Status::Short),
        (1_000, 999, true, Status::Excused),
    ] {
        assert_eq!(
            estimated::status_of(USCents(needed), USCents(credited), excused),
            want
        );
    }
    // California's September 15 needs 70% by then. It is met or short, not
    // "nothing due", because something is needed by it.
    let i = estimated::new_installment(3, d(2026, 9, 15))
        .needed_by(USCents(7_000))
        .credited_by(USCents(7_500))
        .build()
        .unwrap();
    assert_eq!(i.status(), Status::Met);
}

/// The reason states the test that decided the penalty, from the figures it
/// was decided on.
#[test]
fn penalty_reason_states_what_decided_it() {
    let under =
        estimated::exception_below("Tax less withholding", DOLLAR * 600, DOLLAR * 1_000).unwrap();
    let exempt = estimated::new_requirement()
        .required(false, "x")
        .exempt(under)
        .build()
        .unwrap();
    let p = estimated::new_penalty(d(2027, 4, 15))
        .because(exempt.clone(), &[])
        .build()
        .unwrap();
    assert_eq!(p.reason(), "Tax less withholding under $1,000.");

    let met = [
        installment(1, 100, 100, 0, false),
        installment(2, 200, 250, 0, false),
    ];
    let p = estimated::new_penalty(d(2027, 4, 15))
        .because(chosen(), &met)
        .build()
        .unwrap();
    assert_eq!(p.reason(), "Your payments meet every due date.");

    let part = estimated::new_penalty_part(PartKind::Interest, USCents(100), Rate(700))
        .span(d(2026, 4, 15), d(2026, 5, 15))
        .days(30)
        .charge(USCents(1))
        .source("s")
        .build()
        .unwrap();
    let line1 = estimated::new_penalty_line(1, d(2026, 4, 15), USCents(100))
        .part(part.clone())
        .build()
        .unwrap();
    let line2 = estimated::new_penalty_line(2, d(2026, 6, 15), USCents(100))
        .part(part)
        .build()
        .unwrap();
    let short = [
        installment(1, 200, 100, 100, false),
        installment(2, 400, 200, 100, false),
    ];
    let p = estimated::new_penalty(d(2027, 4, 15))
        .line(line1.clone())
        .line(line2)
        .because(chosen(), &short)
        .build()
        .unwrap();
    assert_eq!(p.reason(), "Payments fall short on two due dates.");

    let excused = [
        installment(1, 100, 100, 0, false),
        installment(2, 200, 100, 100, true),
    ];
    let p = estimated::new_penalty(d(2027, 4, 15))
        .because(chosen(), &excused)
        .build()
        .unwrap();
    assert!(p.reason().contains("excused"), "{}", p.reason());

    // A jurisdiction can add a sentence, with no figure in it.
    let p = estimated::new_penalty(d(2027, 4, 15))
        .because(chosen(), &met)
        .context("The January payment still counts after a move.")
        .build()
        .unwrap();
    assert_eq!(
        p.reason(),
        "Your payments meet every due date. The January payment still counts after a move."
    );
    let b = p.to_builder().upper_bound().build().unwrap();
    assert_eq!(b.reason(), p.reason(), "ToBuilder lost the context");
    assert!(
        estimated::new_penalty(d(2027, 4, 15))
            .because(chosen(), &met)
            .context("A $50 charge.")
            .build()
            .is_err(),
        "a context naming a figure was accepted"
    );
    assert_eq!(
        estimated::new_penalty(d(2027, 4, 15))
            .because(chosen(), &met)
            .context("a clause")
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: the penalty's context \"a clause\" must be one sentence with no figure"
    );

    // An exception and a penalty cannot both be true.
    assert_eq!(
        estimated::new_penalty(d(2027, 4, 15))
            .line(line1)
            .because(exempt, &[])
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: an exception applies, yet the penalty is $0.01"
    );
    assert!(
        estimated::new_penalty(d(2027, 4, 15)).build().is_err(),
        "a penalty with no requirement to explain it was built"
    );
}

/// A penalty that stops at the last published rate is no maximum, and its
/// summary says how far it runs.
#[test]
fn penalty_cut_short_is_no_maximum() {
    let part = estimated::new_penalty_part(PartKind::Interest, USCents(10_000), Rate(700))
        .span(d(2026, 4, 15), d(2026, 12, 31))
        .days(260)
        .charge(USCents(10_900))
        .source("s")
        .build()
        .unwrap();
    let line = estimated::new_penalty_line(1, d(2026, 4, 15), USCents(10_000))
        .part(part)
        .build()
        .unwrap();
    let short = [installment(1, 20_000, 10_000, 10_000, false)];
    let p = estimated::new_penalty(d(2027, 4, 15))
        .line(line)
        .because(chosen(), &short)
        .upper_bound()
        .build()
        .unwrap();
    assert!(p.upper_bound());
    assert_eq!(
        p.summary(),
        "At most $109, and less if this year's tax turns out lower."
    );
    let p = p
        .to_builder()
        .rated_through(d(2026, 12, 31))
        .build()
        .unwrap();
    assert!(!p.upper_bound());
    assert_eq!(p.rated_through(), d(2026, 12, 31));
    assert_eq!(
        p.summary(),
        "About $109 through Dec 31, 2026, then more at a rate not yet published."
    );
    let again = p.to_builder().build().unwrap();
    assert_eq!(
        again.rated_through(),
        p.rated_through(),
        "ToBuilder lost the last rated day"
    );
}

#[test]
fn leaves_words_a_refund_without_a_minus() {
    for (a, now, before) in [
        (74_200, "leaves $742", "left $742"),
        (0, "leaves $0", "left $0"),
        (-13_600, "leaves a $136 refund", "left a $136 refund"),
    ] {
        assert_eq!(estimated::leaves(USCents(a)), now);
        assert_eq!(estimated::left(USCents(a)), before);
    }
}

fn waiver_facts(pos: &estimated::PositionsBuilder) -> estimated::Facts {
    let p = pos.build().unwrap();
    base().positions(p).build().unwrap()
}

fn positions(
    c: WaiverCause,
    on: estimated::Date,
    born: Option<estimated::Date>,
) -> estimated::PositionsBuilder {
    let mut b = estimated::new_positions();
    b.waiver_for(c, on);
    if let Some(born) = born {
        b.born(born);
    }
    b
}

/// The federal-style waivers, as the Form 2210 instructions state them: a
/// casualty or disaster, which the IRS judges; and retirement after reaching
/// 62, or disability, in the tax year or the one before.
#[test]
fn standard_waiver() {
    let born = Some(d(1962, 3, 1));
    let zero = estimated::Date::default();
    for (name, pos, want, reason) in [
        (
            "disaster",
            positions(WaiverCause::Disaster, d(2026, 8, 1), None),
            "eligible",
            "The IRS can waive all or part of the penalty. It must be for a disaster that made it unfair.",
        ),
        (
            "retired at 64 in 2026",
            positions(WaiverCause::Retirement, d(2026, 6, 1), born),
            "eligible",
            "You retired in 2026 after reaching 62, so the penalty can be waived. The IRS needs reasonable cause for the shortfall.",
        ),
        (
            "retired at 63 in 2025",
            positions(WaiverCause::Retirement, d(2025, 6, 1), born),
            "eligible",
            "",
        ),
        (
            "retired in 2020",
            positions(WaiverCause::Retirement, d(2020, 6, 1), born),
            "refused",
            "You retired in 2020, and the waiver covers retiring in 2025 or 2026.",
        ),
        (
            "retired at 61",
            positions(WaiverCause::Retirement, d(2026, 6, 1), Some(d(1965, 1, 1))),
            "refused",
            "You were 61 when you retired, and the waiver needs you to have reached 62.",
        ),
        (
            "62 the day after retiring",
            positions(WaiverCause::Retirement, d(2026, 2, 28), Some(d(1964, 3, 1))),
            "refused",
            "",
        ),
        (
            "retired, no date of birth",
            positions(WaiverCause::Retirement, d(2026, 6, 1), None),
            "undecided",
            "The retirement waiver needs your date of birth, to show you'd reached 62.",
        ),
        (
            "retired, no date",
            positions(WaiverCause::Retirement, zero, born),
            "undecided",
            "The retirement waiver needs the date you retired, in 2025 or 2026.",
        ),
        (
            "disabled in 2025",
            positions(WaiverCause::Disability, d(2025, 11, 1), None),
            "eligible",
            "You became disabled in 2025, so the penalty can be waived. The IRS needs reasonable cause for the shortfall.",
        ),
        (
            "disabled in 2023",
            positions(WaiverCause::Disability, d(2023, 11, 1), None),
            "refused",
            "You became disabled in 2023; the waiver needs it to be in 2025 or 2026.",
        ),
    ] {
        let o = estimated::standard_waiver(&waiver_facts(&pos), "the IRS", 62).unwrap();
        let got = if o.eligible() {
            "eligible"
        } else if o.refused() {
            "refused"
        } else if o.undecided() {
            "undecided"
        } else {
            ""
        };
        assert_eq!(got, want, "{name}: {}", o.reason());
        assert!(!o.reason().is_empty());
        if !reason.is_empty() {
            assert_eq!(o.reason(), reason, "{name}");
        }
    }
    let mut none = estimated::new_positions();
    none.waiver(WaiverKind::None);
    assert!(estimated::standard_waiver(&waiver_facts(&none), "the IRS", 62).is_none());
}

#[test]
fn penalty_carries_its_waiver_outcome() {
    let part = estimated::new_penalty_part(PartKind::Interest, USCents(10_000), Rate(700))
        .span(d(2026, 4, 15), d(2026, 12, 31))
        .days(260)
        .charge(USCents(10_900))
        .source("s")
        .build()
        .unwrap();
    let line = estimated::new_penalty_line(1, d(2026, 4, 15), USCents(10_000))
        .part(part)
        .build()
        .unwrap();
    let short = [installment(1, 20_000, 10_000, 10_000, false)];
    let p = estimated::new_penalty(d(2027, 4, 15))
        .line(line)
        .because(chosen(), &short)
        .waiver(WaiverKind::Casualty)
        .waiver_outcome(estimated::waiver_eligible("The IRS can waive it."))
        .build()
        .unwrap();
    let o = p.waiver_outcome().unwrap();
    assert!(o.eligible());
    assert_eq!(p.if_waived(), USCents(0));
    assert_eq!(p.total(), USCents(10_900));
    let again = p.to_builder().build().unwrap();
    assert!(
        again.waiver_outcome().unwrap().eligible(),
        "ToBuilder lost the waiver outcome"
    );
    assert_eq!(
        estimated::new_penalty(d(2027, 4, 15))
            .because(chosen(), &short)
            .waiver_outcome(estimated::waiver_refused("No. Really. Truly."))
            .build()
            .unwrap_err()
            .to_string(),
        "estimated: a waiver outcome's reason is at most two sentences, not \"No. Really. Truly.\""
    );
}

/// A waiver that lifts the penalty but not the interest leaves the interest.
#[test]
fn waiver_that_leaves_the_interest() {
    let interest = estimated::new_penalty_part(PartKind::Interest, USCents(10_000), Rate(700))
        .span(d(2026, 4, 15), d(2026, 12, 31))
        .days(260)
        .charge(USCents(164))
        .source("s")
        .build()
        .unwrap();
    let flat = estimated::new_penalty_part(PartKind::Flat, USCents(10_000), Rate(900))
        .charge(USCents(900))
        .source("s")
        .build()
        .unwrap();
    let line = estimated::new_penalty_line(1, d(2026, 4, 15), USCents(10_000))
        .part(flat)
        .part(interest)
        .build()
        .unwrap();
    let short = [installment(1, 20_000, 10_000, 10_000, false)];
    let p = estimated::new_penalty(d(2027, 4, 15))
        .line(line)
        .because(chosen(), &short)
        .waiver(WaiverKind::Casualty)
        .waiver_outcome(estimated::waiver_eligible_except(
            "The penalty can be waived, though not the interest.",
            &[PartKind::Interest],
        ))
        .build()
        .unwrap();
    assert_eq!(p.total(), USCents(1_064));
    assert_eq!(p.if_waived(), USCents(164));
}

/// The sentinel errors compare as Go's errors.Is does, through wrapping.
#[test]
fn errors_compare_as_errors_is_does() {
    let e = estimated::no_rules(&estimated::CALIFORNIA, 2025);
    assert_eq!(
        e.to_string(),
        "estimated: no estimated tax rules for this jurisdiction and year: CA 2025"
    );
    assert!(e.is(&estimated::Error::NoRules));
    assert!(!e.is(&estimated::Error::Unsupported));
    let twice = estimated::Error::with_detail(e, "again");
    assert!(twice.is(&estimated::Error::NoRules));
    assert!(estimated::Error::NeedsThisYearTax.is(&estimated::Error::NeedsThisYearTax));
}
