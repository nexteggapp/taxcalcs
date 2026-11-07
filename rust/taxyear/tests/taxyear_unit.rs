//! Go's taxyear_test.go: the doc example end to end, Parse's refusals, the
//! file and model checks one break at a time, the AGI working, dependents
//! and exported_from.

#![allow(clippy::expect_used, clippy::unwrap_used)]

use buffa::MessageField;
use proto::factsv1::{self, PayFigures, Person, Position, Relationship, Reply};
use proto::scenariosv1::{
    __buffa::oneof::{
        employer_part::Part as EmployerPartKind, r#match::To,
        retirement_part::Part as RetirementPartKind, tax_paid_default::Rest as TaxPaidRest,
        tax_paid_part::Part as TaxPaidPartKind, term::Of,
    },
    Actual, EmployerModel, EmployerPart, Match, Override, RetirementModel, RetirementPart,
    SupplementalMethod, TaxPaidDefault, TaxPaidModel, TaxPaidPart, TaxYearResult, TaxYearScenarios,
    employer_part, retirement_part, tax_paid_default, tax_paid_part,
};
use proto::{commonv1, factsv1::__buffa::oneof::position::Answer};

const EXAMPLE: &str = include_str!("../../../taxyear/testdata/example.jsonc");

fn read_example() -> TaxYearScenarios {
    taxcalcs_taxyear::parse(EXAMPLE.as_bytes()).expect("the example parses")
}

/// usd is dollars and cents written as a literal, rounded to the cent.
fn usd(d: f64) -> commonv1::Money {
    commonv1::Money {
        minor: (d * 100.0).round() as i64,
        currency: "USD".to_string(),
        ..Default::default()
    }
}

fn date(m: i32, d: i32) -> commonv1::Date {
    commonv1::Date {
        year: 2026,
        month: m,
        day: d,
        ..Default::default()
    }
}

fn compute(f: &TaxYearScenarios) -> TaxYearResult {
    taxcalcs_taxyear::compute(f).expect("compute").proto()
}

fn figure(r: &taxcalcs_taxyear::TaxYearResult, scenario: &str, path: &str) -> i64 {
    let s = r
        .scenario(scenario)
        .unwrap_or_else(|| panic!("no scenario {scenario}"));
    let p = taxcalcs_taxyear::parse_path(path).expect("a path");
    let f = s
        .figure(&p)
        .unwrap_or_else(|| panic!("{scenario}: no figure at {path}"));
    f.amount()
        .unwrap_or_else(|| panic!("{scenario}: {path} is not an amount"))
        .0
}

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

/// TestExampleEndToEnd computes the doc's example. Acme pays $5,833.33
/// twice a month; the Aug 14 paystub and Aug 31 deposit are so far. The
/// paystub has no year-to-date column, so the 14 pay dates before it copy
/// it, as expected paychecks, and the year comes to $140,000. Sep 15 to Dec
/// 31 repeat the paystub in Your plan, and pay $6,250 from Oct 1 in Raise
/// in Oct. The paystub prints no federal line, so the expected paychecks
/// compute it by Publication 15-T for a single filer.
#[test]
fn example_end_to_end() {
    big_stack(|| {
        let r = taxcalcs_taxyear::compute(&read_example()).expect("compute");
        assert!(r.problems().is_empty(), "problems: {:?}", r.problems());
        for (scenario, path, want) in [
            // The Aug 14 paystub and the Aug 31 deposit are two parts, each
            // $5,833.33 rounded on its own: so far adds $5,833 twice.
            (
                "plan",
                "projection.employers[acme].so_far.boxes.wages",
                1_166_600,
            ),
            (
                "plan",
                "projection.employers[acme].rest.boxes.wages",
                12_833_400,
            ),
            // Items show whole dollars: $930.58 withheld and $4,102.18 taken
            // home.
            (
                "plan",
                "projection.employers[acme].items[employers/acme/2026-12-31/regular].paycheck.federal_withheld",
                93_100,
            ),
            (
                "plan",
                "projection.employers[acme].items[employers/acme/2026-08-31/regular].take_home",
                410_200,
            ),
            (
                "raise",
                "projection.employers[acme].rest.boxes.wages",
                13_083_400,
            ),
            (
                "raise",
                "projection.employers[acme].items[employers/acme/2026-10-15/regular].paycheck.gross",
                625_000,
            ),
            ("plan", "federal.form_1040.w2_wages", 14_000_000),
            (
                "plan",
                "federal.form_1040.taxable_income",
                14_000_000 - 1_610_000,
            ),
            // $1,240 + $4,560 + $12,166, and 24% of $18,200 over $105,700.
            ("plan", "federal.form_1040.tax", 2_233_400),
        ] {
            assert_eq!(figure(&r, scenario, path), want, "{scenario} {path}");
        }
        let plan = r.scenario("plan").expect("plan").proto();
        let raise = r.scenario("raise").expect("raise").proto();
        assert!(
            !plan.estimated.is_empty() && plan.estimated[0].jurisdiction == "US",
            "estimated {:?}, want US first",
            plan.estimated
        );
        assert_eq!(
            raise.projection.employers[0].items.len(),
            24,
            "Raise in Oct has one item a pay date"
        );
    });
}

#[test]
fn parse_refuses_unknown_field() {
    let bad = EXAMPLE.replacen(
        r#""taxYear": 2026,"#,
        r#""taxYear": 2026, "taxOwed": 1,"#,
        1,
    );
    assert!(
        taxcalcs_taxyear::parse(bad.as_bytes()).is_err(),
        "an unknown field was accepted"
    );
}

#[test]
fn parse_refuses_unspecified_enum() {
    let bad = EXAMPLE.replacen(
        r#""PAY_FREQUENCY_SEMI_MONTHLY""#,
        r#""PAY_FREQUENCY_UNSPECIFIED""#,
        1,
    );
    let err =
        taxcalcs_taxyear::parse(bad.as_bytes()).expect_err("an UNSPECIFIED enum was accepted");
    assert!(
        err.to_string().contains("PAY_FREQUENCY_UNSPECIFIED"),
        "{err}"
    );
}

#[test]
fn parse_refuses_empty_oneof() {
    let old = r#""defaultPart": { "id": "acme-m9t3r7xe", "sameAsLatest": {} }"#;
    assert!(
        EXAMPLE.contains(old),
        "the example has no default part to empty"
    );
    let bad = EXAMPLE.replacen(old, r#""defaultPart": { "id": "acme-m9t3r7xe" }"#, 1);
    let err =
        taxcalcs_taxyear::parse(bad.as_bytes()).expect_err("a oneof with no case was accepted");
    assert!(err.to_string().contains("rest"), "{err}");
}

/// fileProblems computes and returns the file's problems and every
/// scenario's, as text.
fn file_problems(f: &TaxYearScenarios) -> String {
    let r = compute(f);
    let mut out = Vec::new();
    for p in &r.problems {
        out.push(format!("{}: {}", p.path, p.message.text));
    }
    for s in &r.scenarios {
        for p in &s.problems {
            out.push(format!("{} {}: {}", s.scenario, p.path, p.message.text));
        }
    }
    out.join("\n")
}

fn acme(f: &mut TaxYearScenarios) -> &mut EmployerModel {
    &mut f
        .shared
        .get_or_insert_default()
        .model
        .get_or_insert_default()
        .employers[0]
}

fn part(id: &str, p: EmployerPartKind) -> EmployerPart {
    EmployerPart {
        id: id.to_string(),
        part: Some(p),
        ..Default::default()
    }
}

fn pay_rate(from: commonv1::Date, gross: f64) -> EmployerPartKind {
    EmployerPartKind::from(employer_part::PayRate {
        from: MessageField::some(from),
        paycheck: MessageField::some(PayFigures {
            gross: MessageField::some(usd(gross)),
            ..Default::default()
        }),
        ..Default::default()
    })
}

type Break = fn(&mut TaxYearScenarios);

/// TestValidate breaks the example one way at a time and checks each break
/// is a problem naming its place: the checks that need only the file, and
/// the models doc's "Validate" checks.
#[test]
fn validate() {
    big_stack(|| {
        let cases: Vec<(&str, &str, Break)> = vec![
            ("schema version", "schema_version", |f| f.schema_version = 2),
            ("a tin", "tin", |f| {
                if let Some(EmployerPartKind::FromPaystubs(p)) = &mut acme(f).parts[0].part {
                    p.stubs[0].paystub.get_or_insert_default().employer =
                        MessageField::some(commonv1::Party {
                            name: "Acme".to_string(),
                            tin: "12-3456789".to_string(),
                            ..Default::default()
                        });
                }
            }),
            ("nine digits", "nine digits", |f| {
                acme(f).name = "Acme 123-45-6789".to_string()
            }),
            ("plan first", "Your plan", |f| f.scenarios.swap(0, 1)),
            ("1 ids unique", "used again", |f| {
                let id = acme(f).parts[0].id.clone();
                acme(f).parts[1].id = id;
            }),
            ("3 evidence ids unique", "used twice", |f| {
                if let Some(EmployerPartKind::FromDeposits(p)) = &mut acme(f).parts[1].part {
                    p.deposits[0].id = "acme-p7r2k9sd".to_string();
                }
            }),
            (
                "3 one known part of each kind",
                "at most one from_deposits",
                |f| {
                    let mut p = acme(f).parts[1].clone();
                    p.id = "acme-zzzzzzzz".to_string();
                    if let Some(EmployerPartKind::FromDeposits(d)) = &mut p.part {
                        d.deposits[0].id = "acme-dzzzzzzz".to_string();
                    }
                    acme(f).parts.push(p);
                },
            ),
            ("4 default part", "default part", |f| {
                acme(f).default_part = MessageField::none()
            }),
            ("5 period starts", "start on", |f| {
                for id in ["acme-r1", "acme-r2"] {
                    acme(f).parts.push(part(id, pay_rate(date(10, 1), 1.0)));
                }
            }),
            ("5 period before starts", "before the job", |f| {
                acme(f).starts = MessageField::some(date(9, 1));
                acme(f)
                    .parts
                    .push(part("acme-early", pay_rate(date(8, 1), 1.0)));
            }),
            ("6 one end", "one end", |f| {
                for id in ["acme-e1", "acme-e2"] {
                    let e = employer_part::Ends {
                        last_paycheck: MessageField::some(date(12, 15)),
                        ..Default::default()
                    };
                    acme(f).parts.push(part(id, EmployerPartKind::from(e)));
                }
            }),
            ("6 known end by as_of", "after as_of", |f| {
                let e = employer_part::Ended {
                    last_paycheck: MessageField::some(date(12, 15)),
                    ..Default::default()
                };
                acme(f)
                    .parts
                    .push(part("acme-e1", EmployerPartKind::from(e)));
            }),
            ("7 one year total", "one total", |f| {
                for id in ["acme-y1", "acme-y2"] {
                    let y = employer_part::YearTotal {
                        year: MessageField::some(PayFigures {
                            gross: MessageField::some(usd(1.0)),
                            ..Default::default()
                        }),
                        typed_against: MessageField::some(date(8, 14)),
                        ..Default::default()
                    };
                    acme(f).parts.push(part(id, EmployerPartKind::from(y)));
                }
            }),
            ("7 one plan part a person", "at most one plan", |f| {
                let max =
                    || RetirementPartKind::from(retirement_part::MaxOutWorkplacePlan::default());
                f.shared
                    .get_or_insert_default()
                    .model
                    .get_or_insert_default()
                    .retirement = vec![RetirementModel {
                    id: "you-r".to_string(),
                    person: "you".to_string(),
                    parts: vec![
                        RetirementPart {
                            id: "you-r1".to_string(),
                            part: Some(max()),
                            ..Default::default()
                        },
                        RetirementPart {
                            id: "you-r2".to_string(),
                            part: Some(max()),
                            ..Default::default()
                        },
                    ],
                    ..Default::default()
                }];
            }),
            (
                "8 matches a pay date",
                "not one of the job's pay dates",
                |f| {
                    if let Some(EmployerPartKind::FromDeposits(p)) = &mut acme(f).parts[1].part {
                        p.deposits[0].matches = MessageField::some(Match {
                            to: Some(To::Slot(Box::new(date(8, 30)))),
                            ..Default::default()
                        });
                    }
                },
            ),
            ("8 actual names evidence", "names no evidence", |f| {
                let o = employer_part::OneOff {
                    date: MessageField::some(date(12, 1)),
                    kind: factsv1::PaycheckKind::PAYCHECK_KIND_BONUS.into(),
                    amount: MessageField::some(usd(1.0)),
                    actual: MessageField::some(Actual {
                        is: "acme-nothing".to_string(),
                        ..Default::default()
                    }),
                    ..Default::default()
                };
                acme(f)
                    .parts
                    .push(part("acme-b1", EmployerPartKind::from(o)));
            }),
            ("8 override path", "can be overridden", |f| {
                f.shared.get_or_insert_default().overrides = vec![Override {
                    path: "federal.form_1040.tax".to_string(),
                    amount: MessageField::some(usd(1.0)),
                    ..Default::default()
                }];
            }),
            ("9 supplemental", "supplemental", |f| {
                acme(f).supplemental = SupplementalMethod::SUPPLEMENTAL_METHOD_UNSPECIFIED.into();
            }),
            ("2 item ids unique", "share the id", |f| {
                let planned = |id: &str| TaxPaidPart {
                    id: id.to_string(),
                    part: Some(TaxPaidPartKind::from(tax_paid_part::PlannedPayment {
                        due: MessageField::some(date(9, 15)),
                        amount: MessageField::some(usd(100.0)),
                        ..Default::default()
                    })),
                    ..Default::default()
                };
                f.shared
                    .get_or_insert_default()
                    .model
                    .get_or_insert_default()
                    .tax_paid = vec![TaxPaidModel {
                    id: "us".to_string(),
                    jurisdiction: "US".to_string(),
                    parts: vec![planned("us-1"), planned("us-2")],
                    default_part: MessageField::some(TaxPaidDefault {
                        id: "us-d".to_string(),
                        rest: Some(TaxPaidRest::from(tax_paid_default::SpreadTheRest::default())),
                        ..Default::default()
                    }),
                    ..Default::default()
                }];
            }),
        ];
        let mut failures = Vec::new();
        for (name, want, break_it) in cases {
            let mut f = read_example();
            break_it(&mut f);
            let got = file_problems(&f);
            if !got.contains(want) {
                failures.push(format!(
                    "{name}: problems:\n{got}\nwant one mentioning {want:?}"
                ));
            }
        }
        // The known part in shared: the raise adds an Ended part.
        let added = EXAMPLE.replacen(
            r#""payRate": {
            "from": { "year": 2026, "month": 10, "day": 1 },
            "paycheck": { "gross": { "minor": "625000", "currency": "USD" } } }"#,
            r#""ended": { "lastPaycheck": { "year": 2026, "month": 8, "day": 31 } }"#,
            1,
        );
        assert_ne!(added, EXAMPLE, "the example has no raise to change");
        let got = file_problems(&taxcalcs_taxyear::parse(added.as_bytes()).expect("parses"));
        if !got.contains("is in shared") {
            failures.push(format!("3 known parts in shared: problems:\n{got}"));
        }
        assert!(failures.is_empty(), "{}", failures.join("\n\n"));
        assert_eq!(
            file_problems(&read_example()),
            "",
            "the example has problems"
        );
    });
}

/// TestAGIWorkingTakesAwayAdjustments wants AGI's working to add total
/// income and take away the adjustments.
#[test]
fn agi_working_takes_away_adjustments() {
    big_stack(|| {
        let r = compute(&read_example());
        let mut signs = Vec::new();
        for w in &r.scenarios[0].working {
            if w.path != "federal.form_1040.adjusted_gross_income" {
                continue;
            }
            for t in &w.terms {
                if let Some(Of::Path(p)) = &t.of {
                    signs.push(format!("{p} {}", t.sign));
                }
            }
        }
        assert_eq!(
            signs.join(", "),
            "federal.form_1040.total_income 1, federal.form_1040.adjustments_to_income -1"
        );
    });
}

fn person(
    id: &str,
    name: &str,
    r: Relationship,
    months: i32,
    born: Option<commonv1::Date>,
) -> Person {
    Person {
        id: id.to_string(),
        name: name.to_string(),
        relationship: r.into(),
        months_lived_with_you: months,
        date_of_birth: born.into(),
        ..Default::default()
    }
}

/// TestDependents checks each test a person is measured by, and that a
/// qualifying relative waits on its two questions until they are answered.
#[test]
fn dependents() {
    big_stack(|| {
        let mut f = read_example();
        let facts = f
            .shared
            .get_or_insert_default()
            .facts
            .get_or_insert_default();
        let born = commonv1::Date {
            year: 2016,
            month: 3,
            day: 1,
            ..Default::default()
        };
        facts.people = vec![
            person("you", "", Relationship::RELATIONSHIP_YOU, 0, None),
            person(
                "kid",
                "Sam",
                Relationship::RELATIONSHIP_CHILD,
                12,
                Some(born),
            ),
            person("baby", "", Relationship::RELATIONSHIP_CHILD, 12, None),
            person("rosa", "Rosa", Relationship::RELATIONSHIP_PARENT, 0, None),
            person(
                "friend",
                "",
                Relationship::RELATIONSHIP_NOT_RELATED,
                6,
                None,
            ),
            person(
                "aunt",
                "",
                Relationship::RELATIONSHIP_OTHER_RELATIVE,
                0,
                None,
            ),
        ];
        let answer = |q: &str, who: &str, r: Reply| Position {
            id: format!("{q}.{who}"),
            question: q.to_string(),
            subjects: vec![who.to_string()],
            answer: Some(Answer::Reply(r.into())),
            ..Default::default()
        };
        facts.positions = vec![
            answer(
                "dependent.gross_income_under_limit",
                "aunt",
                Reply::REPLY_YES,
            ),
            answer("dependent.more_than_half_support", "aunt", Reply::REPLY_YES),
        ];
        let r = compute(&f);
        let s = &r.scenarios[0];
        let got: Vec<String> = s
            .dependents
            .iter()
            .map(|d| {
                format!(
                    "{}: {} ({} questions)",
                    d.person,
                    d.text.text,
                    d.questions.len()
                )
            })
            .collect();
        let want = [
            "kid: Counts as your qualifying child (0 questions)",
            "baby: Whether they count as your dependent goes by their date of birth (0 questions)",
            "rosa: Could count as your qualifying relative (2 questions)",
            "friend: Doesn’t count as your dependent: they didn’t live with you all year (0 questions)",
            "aunt: Counts as your qualifying relative (2 questions)",
        ];
        assert_eq!(got.join("\n"), want.join("\n"));
        let asked = s
            .questions
            .iter()
            .find(|q| q.id == "dependent.gross_income_under_limit.rosa")
            .map(|q| q.question.text.clone());
        assert_eq!(
            asked.as_deref(),
            Some("Was Rosa’s gross income under $5,300?")
        );
        let rows: Vec<String> = s
            .federal
            .form_1040
            .dependents
            .iter()
            .map(|d| format!("{} {}", d.name, d.relationship))
            .collect();
        assert_eq!(rows.join(", "), "Sam child,  relative");
    });
}

/// exported_from names the build that exported the file, which may hold
/// nine digits in a row, and is no identifier.
#[test]
fn exported_from_is_opaque() {
    big_stack(|| {
        let mut f = read_example();
        f.exported_from = "y2026.41/d3/123456789".to_string();
        let p = compute(&f).problems;
        assert!(p.is_empty(), "problems {p:?}, want none for exported_from");
    });
}

/// models_test.go's TestOverrideWas: an override's was is the figure the
/// person saw; when today's computed figure differs, its working says the
/// sources changed.
#[test]
fn override_was() {
    big_stack(|| {
        for (was, changed) in [(11666.66, false), (9999.0, true)] {
            let mut f = read_example();
            f.shared.get_or_insert_default().overrides = vec![Override {
                path: "projection.employers[acme].so_far.items.gross".to_string(),
                amount: MessageField::some(usd(12000.0)),
                was: MessageField::some(usd(was)),
                ..Default::default()
            }];
            let r = taxcalcs_taxyear::compute(&f).expect("compute");
            let p = taxcalcs_taxyear::parse_path("projection.employers[acme].so_far.items.gross")
                .expect("a path");
            let plan = r.scenario("plan").expect("plan");
            let fig = plan.figure(&p).expect("the figure");
            let w = fig.working().expect("no working for the override");
            assert_eq!(w.sources_changed, changed, "was {was}");
            assert_eq!(w.automatic.minor, 1_166_700, "was {was}");
        }
    });
}
