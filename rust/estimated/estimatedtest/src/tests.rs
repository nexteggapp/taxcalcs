//! The harness's own tests: the households and the reference rules against
//! the vectors golden/estimated_test.go wrote, and the copy checks on cases
//! worked by hand.

use estimated::{
    Basis, Calendar, Date, EstimatedTax, Explanation, Facts, Installment, Jurisdiction,
    LedgerEntry, Month, Penalty, RateSeries, Requirement,
};
use money::{DOLLAR, FineRate, Rate, USCents, percent};
use serde_json::Value;
use working::{Notes, PositionKind, Reading};

use super::*;

#[test]
fn households_match_go() {
    let want = read_gzip("estimated/households.json.gz");
    for j in [
        estimated::FEDERAL,
        estimated::CALIFORNIA,
        estimated::WASHINGTON,
    ] {
        let got: Vec<Value> = households(&j).iter().map(facts_json).collect();
        assert_same_json(
            &format!("households of {j}"),
            &Value::Array(got),
            &want[j.as_str()],
        );
    }
    assert_eq!(households(&estimated::FEDERAL).len(), 23);
    assert_eq!(households(&estimated::WASHINGTON).len(), 22);
}

#[test]
fn reference_rules_match_go() {
    assert_golden(&REFERENCE, new_reference);
}

#[test]
fn every_jurisdiction_has_vectors() {
    for j in estimated::jurisdictions() {
        let v = read_gzip(&golden_file(&j));
        assert_eq!(v["jurisdiction"], j.as_str());
        assert_eq!(
            v["years"].as_array().map(Vec::len),
            Some(GOLDEN_YEARS.len())
        );
    }
}

#[test]
fn reference_answers_are_in_shape() {
    let Ok(rules) = new_reference(2026) else {
        panic!("no reference rules for 2026");
    };
    let as_ = ask(&rules, &households(&REFERENCE));
    assert_eq!(check_answers(&as_), Vec::<String>::new());
    assert_eq!(
        check_copy(&rules, &texts(Some(&rules as &dyn EstimatedTax), &as_)),
        Vec::<String>::new()
    );
    let a = as_
        .iter()
        .find(|a| a.household().name() == "neither year's tax");
    let Some(a) = a else {
        panic!("no household without either year's tax");
    };
    assert!(expected(a.schedule().err().as_ref()));
}

#[test]
fn sentences_split_where_go_splits() {
    assert_eq!(
        sentences("One. Two? “Three.” (Four) five!  $6 more."),
        ["One.", "Two?", "“Three.”", "(Four) five!", "$6 more."]
    );
    assert_eq!(sentences("No end"), ["No end"]);
    assert_eq!(sentences("A.b. c"), ["A.b. c"]);
}

/// Each copy rule finds what it is for, and the sentence and length limits
/// hold, in Go's words.
#[test]
fn check_copy_finds_each_breach() {
    let t = |text: &str, kind: TextKind, limit: i64| Text {
        location: "h: why".into(),
        text: text.into(),
        limit,
        kind,
    };
    let Ok(rules) = new_reference(2026) else {
        panic!("no reference rules");
    };
    let got = check_copy(
        &rules,
        &[
            t("You owe $1,234.50 here.", TextKind::Other, 300),
            t("It can for a casualty.", TextKind::Two, 300),
            t("One. Two. Three.", TextKind::Two, 300),
            t("Is it due", TextKind::Question, 200),
            t("We farm.", TextKind::Body, 300),
            t(&"x".repeat(90), TextKind::Other, 80),
            t("−$5 is left.", TextKind::Other, 300),
            t("Aowe and showed are fine.", TextKind::Other, 300),
        ],
    );
    assert_eq!(
        got,
        [
            r#"h: why: "$1,234.50" breaks "money in whole dollars, no cents" in "You owe $1,234.50 here.""#,
            r#"h: why: "owe" breaks "\"due\", not \"owe\"" in "You owe $1,234.50 here.""#,
            r#"h: why: "here" breaks "never \"here\": the person is not inside the software" in "You owe $1,234.50 here.""#,
            r#"h: why: "It can for" is a modal with no verb: say what it can do, in "It can for a casualty.""#,
            r#"h: why: 3 sentences, more than two: "One. Two. Three.""#,
            r#"h: why: not a question: "Is it due""#,
            r#"h: why: "farm" is a case for the reasons and divergences, not the explanation: "We farm.""#,
            r#"h: why: "We" breaks "never \"we\"" in "We farm.""#,
            &format!(
                "h: why: a sentence of 90 characters, more than 80: \"{}\"",
                "x".repeat(90)
            ),
            &format!(
                "h: why: 90 characters, more than 80: \"{}\"",
                "x".repeat(90)
            ),
            r#"h: why: "−$5" breaks "a negative balance reads as a refund, not a minus sign" in "−$5 is left.""#,
        ]
    );
}

#[test]
fn same_readings_finds_divergences_that_do_not_diverge() {
    let d = |form: &str, statute: &str| {
        working::new_divergence("r")
            .form(form, "f")
            .statute(statute, "s")
            .uses(Reading::Form)
            .affects(true)
            .build()
            .unwrap()
    };
    assert_eq!(
        same_readings(&d("It is $500.", "it is $500")),
        "the form and the statute read alike"
    );
    assert_eq!(
        same_readings(&d(
            "The form uses $500 from Apr 15.",
            "The statute gives the same $500 from Apr 15."
        )),
        "both readings give $500, Apr 15, and the statute's calls them the same"
    );
    assert_eq!(same_readings(&d("It is $500.", "It is $600.")), "");
}

// The reference rules, as golden/estimated_test.go writes them: a small
// jurisdiction, "ZZ", that exercises the shared helpers and the encoding.
// They are not any jurisdiction's law.

const REFERENCE: Jurisdiction = estimated::Jurisdiction::from_static("ZZ");

struct Reference {
    year: i64,
    calendar: Calendar,
    rates: RateSeries,
}

fn d(y: i64, m: Month, day: i64) -> Date {
    estimated::must_date(y, m, day)
}

fn new_reference(year: i64) -> Result<Reference, estimated::Error> {
    if year != 2026 {
        return Err(estimated::no_rules(&REFERENCE, year));
    }
    let calendar = estimated::new_calendar("Reference holidays")
        .holiday(d(2026, Month::JUNE, 15), "Reference Day")
        .build()?;
    let rates = estimated::new_rate_series()
        .period(
            d(2026, Month::JANUARY, 1),
            d(2026, Month::JUNE, 30),
            percent(7),
            "Reference rate H1",
        )
        .fine_period(
            d(2026, Month::JULY, 1),
            d(2027, Month::MARCH, 31),
            FineRate(108_133),
            "Reference rate H2",
        )
        .build()?;
    Ok(Reference {
        year,
        calendar,
        rates,
    })
}

const SHARES: [Rate; 4] = [Rate(2500), Rate(2500), Rate(2500), Rate(2500)];

impl Reference {
    fn dues(&self) -> Vec<Date> {
        estimated::move_dates(
            &self.calendar,
            &[
                d(self.year, Month::APRIL, 15),
                d(self.year, Month::JUNE, 15),
                d(self.year, Month::SEPTEMBER, 15),
                d(self.year + 1, Month::JANUARY, 15),
            ],
        )
    }

    fn assess(&self, f: &Facts) -> Result<(Requirement, Notes), estimated::Error> {
        let mut notes = Notes::default();
        let withheld = f.withheld_total();
        let mut b = estimated::new_requirement();
        let tax = f.this_year().tax();
        match tax {
            Some(tax) => b.option(
                estimated::new_target_option(Basis::ThisYearTax, percent(90), percent(90).of(tax))
                    .build()?,
            ),
            None => b.option(
                estimated::new_target_option(Basis::ThisYearTax, percent(90), USCents(0))
                    .unavailable(estimated::NEEDS_THIS_YEAR_TAX)
                    .build()?,
            ),
        };
        let prior = f.prior_year();
        let prior_figures = prior.and_then(|p| p.figures());
        let prior_tax = prior_figures.and_then(|f| f.tax()).unwrap_or_default();
        let last_open = prior.is_some_and(|p| p.filed()) && prior_figures.is_some();
        if last_open {
            b.option(
                estimated::new_target_option(Basis::PriorYearTax, percent(100), prior_tax)
                    .build()?,
            );
        } else {
            b.option(
                estimated::new_target_option(Basis::PriorYearTax, percent(100), USCents(0))
                    .unavailable("No return was filed for last year.")
                    .build()?,
            );
        }
        if tax.is_none() {
            notes.ask(
                PositionKind::ThisYearTax,
                format!("What do you expect your {} tax to be?", self.year),
                estimated::without_this_year_tax(last_open),
            );
        }
        notes.rule("Reference § 1", format!("withholding of {withheld}"));
        match tax {
            Some(tax) if tax - withheld < DOLLAR * 1_000 => {
                let e = estimated::exception_below(
                    "Tax less withholding",
                    tax - withheld,
                    DOLLAR * 1_000,
                )?;
                b.required(
                    false,
                    format!("Your tax {}.", estimated::leaves(tax - withheld)),
                )
                .exempt(e);
            }
            _ if last_open && prior_tax.0 == 0 => {
                let e = estimated::new_exception("No tax due last year").build()?;
                b.required(false, "You had no tax last year.").exempt(e);
            }
            None if !last_open => {
                b.required(false, "No target can be set yet.").no_target();
            }
            _ => {
                let (mut basis, mut target) = (Basis::PriorYearTax, prior_tax);
                if let Some(tax) = tax
                    && (!last_open || percent(90).of(tax) < prior_tax)
                {
                    (basis, target) = (Basis::ThisYearTax, percent(90).of(tax));
                }
                b.required(
                    target > withheld,
                    format!(
                        "The target is {}, and {} is withheld.",
                        target.whole(),
                        withheld.whole()
                    ),
                )
                .choose(basis);
            }
        }
        Ok((b.build()?, notes))
    }

    fn ledger(
        &self,
        f: &Facts,
        req: &Requirement,
    ) -> Result<(Vec<Installment>, Vec<LedgerEntry>), estimated::Error> {
        let dues = self.dues();
        let cum = estimated::cumulative_shares(req.target(), &SHARES)?;
        let credits = if f.elections().withholding_as_withheld() {
            estimated::credits_as_withheld(f, &dues)?
        } else {
            estimated::credits(f, &dues, &SHARES)?
        };
        let ledger = estimated::apply(&dues, &estimated::increments(&cum), &credits)?;
        let mut out = Vec::new();
        for (i, e) in ledger.iter().enumerate() {
            let mut b = estimated::new_installment(i as i64 + 1, dues[i]);
            b.needed_by(cum[i]).credited_by(e.credited_by());
            if e.credited_by() < cum[i] {
                b.shortfall(e.shortfall());
            }
            if req.exempt() {
                b.excused();
            }
            out.push(b.build()?);
        }
        Ok((out, ledger))
    }
}

impl EstimatedTax for Reference {
    fn requirement(&self, f: &Facts) -> Result<working::Result<Requirement>, estimated::Error> {
        let (req, notes) = self.assess(f)?;
        let d = estimated::new_divergence("The reference threshold")
            .form(
                "The form tests tax less withholding against $1,000.",
                "Reference Form 1, line 3",
            )
            .statute(
                "The statute tests it against $1,000 or 10% of the tax.",
                "Reference § 2",
            )
            .uses(Reading::Form)
            .affects(false)
            .build()?;
        Ok(working::new_result(req)
            .with(&notes)
            .diverges(&[d])
            .build()?)
    }

    fn schedule(&self, f: &Facts) -> Result<working::Result<Vec<Installment>>, estimated::Error> {
        let (req, notes) = self.assess(f)?;
        if !req.target_known() {
            return Err(estimated::Error::NeedsThisYearTax);
        }
        let (is, _) = self.ledger(f, &req)?;
        Ok(working::new_result(is)
            .with(&notes)
            .rule("Reference § 3", "four equal installments")
            .build()?)
    }

    fn penalty(
        &self,
        f: &Facts,
        as_of: Date,
    ) -> Result<working::Result<Penalty>, estimated::Error> {
        let (req, mut notes) = self.assess(f)?;
        if !req.target_known() {
            return Err(estimated::Error::NeedsThisYearTax);
        }
        let (is, ledger) = self.ledger(f, &req)?;
        let waiver = f.positions().waiver().unwrap_or_default();
        if req.exempt() {
            let p = estimated::new_penalty(as_of)
                .waiver(waiver)
                .because(req, &is)
                .build()?;
            return Ok(working::new_result(p).with(&notes).build()?);
        }
        let mut stop = d(self.year + 1, Month::APRIL, 15);
        if as_of.before(stop) {
            stop = as_of;
        }
        let filed = f.return_filed();
        if !filed.is_zero() && filed.before(stop) {
            stop = filed;
        }
        let (p, unrated) =
            estimated::simple_penalty(&ledger, stop, &self.rates, waiver, &req, &is)?;
        let mut b = p.to_builder();
        // The first shortfall also bears compound interest to the stop, as a
        // second charge on the same line, to exercise compound_daily.
        if !p.lines().is_empty() && f.elections().annualised() {
            b = estimated::new_penalty(stop);
            b.waiver(waiver).because(req.clone(), &is);
            for (i, l) in p.lines().into_iter().enumerate() {
                if i > 0 {
                    b.line(l);
                    continue;
                }
                let (parts, _) =
                    estimated::compound_daily(l.shortfall(), l.due(), stop, &self.rates)?;
                let mut lb = estimated::new_penalty_line(l.installment(), l.due(), l.shortfall());
                lb.days(l.days());
                for part in parts {
                    lb.part(part);
                }
                b.line(lb.build()?);
            }
            if !unrated.is_zero() {
                b.rated_through(unrated.add_days(-1));
            }
        }
        if !unrated.is_zero() {
            notes.ask(
                PositionKind::UnpublishedRate,
                "",
                format!(
                    "The rate for the days after {} isn't published yet.",
                    self.rates.ends().long()
                ),
            );
        }
        if let Some(o) = estimated::standard_waiver(f, "the reference authority", 62)
            && p.total().0 > 0
        {
            b.waiver_outcome(o);
        }
        if !f.requirement_first_met().is_zero() {
            b.context("A late start does not change the due dates.");
        }
        let out = b.build()?;
        Ok(working::new_result(out).with(&notes).build()?)
    }

    fn explanation(&self) -> Explanation {
        estimated::new_explanation("The reference rules ask for four equal payments.")
            .paragraph("Pay a quarter of the target by each due date.")
            .paragraph("Interest runs on what is short until it is paid.")
            .source("Reference § 1")
            .source("Reference Form 1")
            .build()
            .unwrap_or_default()
    }
}
