//! Checks any jurisdiction's estimated tax rules against what every
//! jurisdiction must satisfy: the shape of its answers and the copy rules
//! for every sentence a person reads. Ported from the Go package
//! `estimated/estimatedtest`.
//!
//! It runs the rules over a fixed set of households chosen to reach the
//! common branches: tax known and unknown, last year's return filed, not
//! filed and absent, no tax last year, a late start, a part-year resident,
//! the annualised method, an early return, a waiver asked for, and payments
//! made late. State extras are never supplied, so each state's defaults and
//! questions are exercised too.
//!
//! [`golden_answers`] and [`assert_golden`] compare a jurisdiction's answers
//! over these households with the golden vectors the Go code wrote, in
//! `golden/testdata/estimated/<postal code>.json.gz`.
//!
//! Go's `CheckSource`, in source.go, measures the string literals of a Go
//! package's source and is not ported: it reads Go syntax trees.

use std::collections::{BTreeSet, HashSet};
use std::sync::LazyLock;

use estimated::{
    Basis, Date, EstimatedTax, Facts, FactsBuilder, Figures, Installment, Jurisdiction, Month,
    PartKind, Payment, Penalty, PriorReturn, Requirement, ResidencePeriod, Status, WaiverCause,
    WaiverKind, Withheld,
};
use money::USCents;
use regex::Regex;
use working::{Divergence, PositionKind, Required, quote};

mod golden;

pub use golden::{
    GOLDEN_YEARS, assert_golden, assert_same_json, facts_json, golden_answers, golden_file,
    read_gzip,
};

/// One set of facts and its name.
#[derive(Clone, Debug)]
pub struct Household {
    name: String,
    facts: Facts,
}

impl Household {
    /// The household's name, as "nothing paid".
    pub fn name(&self) -> String {
        self.name.clone()
    }

    /// The facts.
    pub fn facts(&self) -> Facts {
        self.facts.clone()
    }
}

const YEAR: i64 = 2026;

fn d(m: Month, day: i64) -> Date {
    estimated::must_date(YEAR, m, day)
}

fn dollars(n: i64) -> USCents {
    money::dollars(n)
}

/// This year's figures: a tax, and federal and state AGI, each left out when
/// negative.
fn figures(tax: i64, agi: i64) -> Figures {
    let mut b = estimated::new_figures();
    if tax >= 0 {
        b.tax(dollars(tax));
    }
    if agi >= 0 {
        b.federal_agi(dollars(agi)).state_agi(dollars(agi));
    }
    b.build().unwrap_or_default()
}

fn prior(tax: i64, agi: i64) -> PriorReturn {
    estimated::new_prior_return()
        .filed(true)
        .months(12)
        .full_year_resident(true)
        .figures(figures(tax, agi))
        .build()
        .unwrap_or_default()
}

/// A prior return whose figures carry the year's withholding too.
fn prior_withheld(tax: i64, withheld: i64, agi: i64) -> PriorReturn {
    let f = estimated::new_figures()
        .tax(dollars(tax))
        .withheld(dollars(withheld))
        .federal_agi(dollars(agi))
        .state_agi(dollars(agi))
        .build()
        .unwrap_or_default();
    estimated::new_prior_return()
        .filed(true)
        .months(12)
        .full_year_resident(true)
        .figures(f)
        .build()
        .unwrap_or_default()
}

fn not_filed() -> PriorReturn {
    estimated::new_prior_return()
        .filed(false)
        .build()
        .unwrap_or_default()
}

fn withheld(total: i64) -> Vec<Withheld> {
    (1..=12)
        .map(|m| {
            estimated::new_withheld()
                .on(estimated::must_date(YEAR, Month(m), 28))
                .amount(dollars(total) / 12)
                .build()
                .unwrap_or_default()
        })
        .collect()
}

fn pay(m: Month, day: i64, n: i64) -> Payment {
    estimated::new_payment()
        .on(estimated_date(YEAR, m, day))
        .amount(dollars(n))
        .build()
        .unwrap_or_default()
}

fn quarterly(n: i64) -> Vec<Payment> {
    vec![
        pay(Month::APRIL, 15, n),
        pay(Month::JUNE, 15, n),
        pay(Month::SEPTEMBER, 15, n),
    ]
}

fn resident(j: &Jurisdiction, from: Date, to: Date) -> ResidencePeriod {
    estimated::new_residence_period(j.as_str())
        .from(from)
        .to(to)
        .build()
        .unwrap_or_default()
}

type Spec = (&'static str, Box<dyn Fn(&mut FactsBuilder, &Jurisdiction)>);

fn specs() -> Vec<Spec> {
    vec![
        (
            "withholding over the tax",
            Box::new(|b, _| {
                b.this_year(figures(3_000, 60_000))
                    .prior_year(prior(2_800, 55_000))
                    .withheld(&withheld(3_600));
            }),
        ),
        (
            "withholding over last year's tax",
            Box::new(|b, _| {
                b.this_year(figures(6_000, 70_000))
                    .prior_year(prior_withheld(2_000, 2_136, 50_000))
                    .withheld(&withheld(1_000));
            }),
        ),
        (
            "this year's tax unknown, little due last year",
            Box::new(|b, _| {
                b.this_year(figures(-1, 40_000))
                    .prior_year(prior_withheld(600, 400, 30_000))
                    .withheld(&withheld(500));
            }),
        ),
        (
            "withholding covers nearly all",
            Box::new(|b, _| {
                b.this_year(figures(5_000, 80_000))
                    .prior_year(prior(4_000, 75_000))
                    .withheld(&withheld(4_500));
            }),
        ),
        (
            "large balance, paid quarterly",
            Box::new(|b, _| {
                b.this_year(figures(30_000, 160_000))
                    .prior_year(prior(20_000, 140_000))
                    .withheld(&withheld(10_000))
                    .paid(&quarterly(2_000));
            }),
        ),
        (
            "nothing paid",
            Box::new(|b, _| {
                b.this_year(figures(12_000, 120_000))
                    .prior_year(prior(9_000, 110_000));
            }),
        ),
        (
            "this year's tax unknown",
            Box::new(|b, _| {
                b.this_year(figures(-1, -1))
                    .prior_year(prior(8_000, 100_000))
                    .withheld(&withheld(2_000));
            }),
        ),
        (
            "neither year's tax",
            Box::new(|b, _| {
                b.this_year(figures(-1, 60_000)).withheld(&withheld(1_000));
            }),
        ),
        (
            "last year not filed",
            Box::new(|b, _| {
                b.this_year(figures(9_000, 90_000)).prior_year(not_filed());
            }),
        ),
        (
            "no return on record",
            Box::new(|b, _| {
                b.this_year(figures(9_000, 90_000))
                    .withheld(&withheld(1_000));
            }),
        ),
        (
            "no tax last year",
            Box::new(|b, _| {
                b.this_year(figures(9_000, 90_000))
                    .prior_year(prior(0, 20_000));
            }),
        ),
        (
            "high income",
            Box::new(|b, _| {
                b.filing_status(filing::Status::MarriedFilingJointly)
                    .this_year(figures(90_000, 600_000))
                    .prior_year(prior(70_000, 500_000))
                    .withheld(&withheld(40_000))
                    .paid(&quarterly(8_000));
            }),
        ),
        (
            "paid late",
            Box::new(|b, _| {
                b.this_year(figures(12_000, 120_000))
                    .prior_year(prior(9_000, 110_000))
                    .paid(&[
                        pay(Month::MAY, 20, 3_000),
                        pay(Month::AUGUST, 1, 3_000),
                        pay(Month::DECEMBER, 1, 3_000),
                    ]);
            }),
        ),
        (
            "late start",
            Box::new(|b, _| {
                b.this_year(figures(12_000, 120_000))
                    .prior_year(prior(9_000, 110_000))
                    .requirement_first_met(d(Month::JULY, 1));
            }),
        ),
        (
            "part-year resident",
            Box::new(|b, j| {
                b.this_year(figures(6_000, 70_000))
                    .prior_year(prior(5_000, 60_000))
                    .residence(&[resident(j, d(Month::JANUARY, 1), d(Month::JUNE, 30))]);
            }),
        ),
        (
            "full-year resident",
            Box::new(|b, j| {
                b.this_year(figures(6_000, 70_000))
                    .prior_year(prior(5_000, 60_000))
                    .residence(&[resident(j, d(Month::JANUARY, 1), d(Month::DECEMBER, 31))])
                    .withheld(&withheld(3_000));
            }),
        ),
        (
            "return filed in January",
            Box::new(|b, _| {
                b.this_year(figures(12_000, 120_000))
                    .prior_year(prior(9_000, 110_000))
                    .paid(&quarterly(2_000))
                    .return_filed(estimated_date(YEAR + 1, Month::JANUARY, 25));
            }),
        ),
        (
            "waiver asked for",
            Box::new(|b, _| {
                let pos = estimated::new_positions()
                    .waiver(WaiverKind::Casualty)
                    .build()
                    .unwrap_or_default();
                b.this_year(figures(12_000, 120_000))
                    .prior_year(prior(9_000, 110_000))
                    .positions(pos);
            }),
        ),
        (
            "retired in 2026 at 64",
            Box::new(|b, _| {
                let pos = estimated::new_positions()
                    .waiver_for(WaiverCause::Retirement, d(Month::JUNE, 1))
                    .born(estimated::must_date(1962, Month::MARCH, 1))
                    .build()
                    .unwrap_or_default();
                b.this_year(figures(12_000, 120_000))
                    .prior_year(prior(9_000, 110_000))
                    .positions(pos);
            }),
        ),
        (
            "retired in 2020",
            Box::new(|b, _| {
                let pos = estimated::new_positions()
                    .waiver_for(
                        WaiverCause::Retirement,
                        estimated::must_date(2020, Month::JUNE, 1),
                    )
                    .born(estimated::must_date(1955, Month::MARCH, 1))
                    .build()
                    .unwrap_or_default();
                b.this_year(figures(12_000, 120_000))
                    .prior_year(prior(9_000, 110_000))
                    .positions(pos);
            }),
        ),
        (
            "a disaster",
            Box::new(|b, _| {
                let pos = estimated::new_positions()
                    .waiver_for(WaiverCause::Disaster, d(Month::AUGUST, 1))
                    .build()
                    .unwrap_or_default();
                b.this_year(figures(12_000, 120_000))
                    .prior_year(prior(9_000, 110_000))
                    .positions(pos);
            }),
        ),
        (
            "withholding on its dates",
            Box::new(|b, _| {
                let el = estimated::new_elections()
                    .withholding_as_withheld()
                    .build()
                    .unwrap_or_default();
                b.this_year(figures(12_000, 120_000))
                    .prior_year(prior(9_000, 110_000))
                    .withheld(&withheld(6_000))
                    .elections(el);
            }),
        ),
        (
            "annualised",
            Box::new(|b, _| {
                let el = estimated::new_elections()
                    .annualised()
                    .build()
                    .unwrap_or_default();
                let ends = [
                    d(Month::MARCH, 31),
                    d(Month::MAY, 31),
                    d(Month::AUGUST, 31),
                    d(Month::DECEMBER, 31),
                ];
                let ps: Vec<_> = ends
                    .iter()
                    .enumerate()
                    .map(|(i, &end)| {
                        let i = i as i64;
                        estimated::new_period()
                            .end(end)
                            .income(dollars(10_000 * (i + 1)))
                            .annualised_tax(dollars(1_000 * (i + 1)))
                            .build()
                            .unwrap_or_default()
                    })
                    .collect();
                b.this_year(figures(12_000, 120_000))
                    .prior_year(prior(9_000, 110_000))
                    .elections(el)
                    .periods(&ps);
            }),
        ),
    ]
}

/// Returns the fixed set of households for `j` and tax year 2026. A
/// combination the facts builder refuses is left out.
pub fn households(j: &Jurisdiction) -> Vec<Household> {
    let mut out = Vec::new();
    for (name, build) in specs() {
        let mut b = estimated::new_facts(j.clone(), YEAR);
        b.filing_status(filing::Status::Single);
        build(&mut b, j);
        if *j == estimated::WASHINGTON {
            // Washington's capital gains tax has no withholding, no
            // annualised method, and takes payments only from Oct 15 of the
            // year of the sale, so those facts are refused rather than
            // answered. Its households carry the same tax and payments on
            // dates it accepts.
            if name == "annualised" {
                continue;
            }
            b = washington(&b, YEAR);
        }
        let Ok(facts) = b.build() else {
            continue;
        };
        out.push(Household {
            name: name.to_string(),
            facts,
        });
    }
    out
}

fn estimated_date(y: i64, m: Month, d: i64) -> Date {
    estimated::must_date(y, m, d)
}

/// What the rules said about one household. An error from a method is kept
/// in place of its result.
#[derive(Clone, Debug)]
pub struct Answers {
    household: Household,
    requirement: Result<working::Result<Requirement>, estimated::Error>,
    schedule: Result<working::Result<Vec<Installment>>, estimated::Error>,
    penalty: Result<working::Result<Penalty>, estimated::Error>,
}

impl Answers {
    /// The household the answers are for.
    pub fn household(&self) -> Household {
        self.household.clone()
    }

    /// What `requirement` returned.
    pub fn requirement(&self) -> Result<working::Result<Requirement>, estimated::Error> {
        self.requirement.clone()
    }

    /// What `schedule` returned.
    pub fn schedule(&self) -> Result<working::Result<Vec<Installment>>, estimated::Error> {
        self.schedule.clone()
    }

    /// What `penalty` returned.
    pub fn penalty(&self) -> Result<working::Result<Penalty>, estimated::Error> {
        self.penalty.clone()
    }

    fn errs(&self) -> [Option<&estimated::Error>; 3] {
        [
            self.requirement.as_ref().err(),
            self.schedule.as_ref().err(),
            self.penalty.as_ref().err(),
        ]
    }
}

/// The date every penalty is asked to: the end of the next year, which every
/// jurisdiction's rules stop before. Go's `PenaltyAsOf`.
pub fn penalty_as_of() -> Date {
    estimated::must_date(2027, Month::DECEMBER, 31)
}

/// Runs `rules` over every household.
pub fn ask(rules: &dyn EstimatedTax, hs: &[Household]) -> Vec<Answers> {
    hs.iter()
        .map(|h| Answers {
            household: h.clone(),
            requirement: rules.requirement(&h.facts),
            schedule: rules.schedule(&h.facts),
            penalty: rules.penalty(&h.facts, penalty_as_of()),
        })
        .collect()
}

/// Reports whether `err` is one a jurisdiction may return for valid facts:
/// rules not built for them, or a figure that needs this year's tax. `None`
/// is no error.
pub fn expected(err: Option<&estimated::Error>) -> bool {
    match err {
        None => true,
        Some(e) => {
            e.is(&estimated::Error::Unsupported) || e.is(&estimated::Error::NeedsThisYearTax)
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum TextKind {
    Other,
    /// An explanation's summary or a body paragraph.
    Body,
    /// Exactly one sentence: a penalty's summary.
    Sentence,
    /// One or two sentences: a question's effect, a note, a reason.
    Two,
    /// One sentence ending in a question mark.
    Question,
}

/// One string a person reads, and where it came from.
#[derive(Clone, Debug)]
pub struct Text {
    location: String,
    text: String,
    limit: i64,
    kind: TextKind,
}

impl Text {
    /// Where it came from, as "nothing paid: why". Go's `Where`.
    pub fn where_(&self) -> String {
        self.location.clone()
    }

    /// The text.
    pub fn text(&self) -> String {
        self.text.clone()
    }
}

fn add_kind(out: &mut Vec<Text>, location: String, s: String, limit: i64, kind: TextKind) {
    if !s.is_empty() {
        out.push(Text {
            location,
            text: s,
            limit,
            kind,
        });
    }
}

fn add(out: &mut Vec<Text>, location: String, s: String, limit: i64) {
    add_kind(out, location, s, limit, TextKind::Other);
}

fn notes(out: &mut Vec<Text>, h: &str, req: &[Required], ds: &[Divergence]) {
    for r in req {
        let location = format!("{h}: {}", r.position());
        if !r.asked() {
            out.push(Text {
                location,
                text: r.because(),
                kind: TextKind::Other,
                limit: -1,
            });
            continue;
        }
        add_kind(
            out,
            format!("{location} question"),
            r.question(),
            200,
            TextKind::Question,
        );
        add_kind(
            out,
            format!("{location} effect"),
            r.effect(),
            250,
            TextKind::Two,
        );
        let unanswerable = r.position() == PositionKind::UnpublishedRate
            || r.position() == PositionKind::UnsettledRule;
        if unanswerable && !r.question().is_empty() {
            out.push(Text {
                location,
                text: r.question(),
                kind: TextKind::Other,
                limit: -2,
            });
        } else if !unanswerable && r.question().is_empty() {
            out.push(Text {
                location,
                text: r.effect(),
                kind: TextKind::Other,
                limit: -3,
            });
        }
    }
    for d in ds {
        add(out, format!("{h}: divergence rule"), d.rule(), 120);
        add(out, format!("{h}: divergence form"), d.form(), 300);
        add(out, format!("{h}: divergence statute"), d.statute(), 300);
    }
}

/// Returns every string a person reads in the explanation, when `rules` is
/// given, and the answers: the requirement's reason, each option's label and
/// why it is not available, every question, every divergence, and the
/// penalty's reason. Citations are not included: a person sees them as the
/// titles of the documents they link to, not as sentences. Nor is a rule's
/// working note, which the payments API does not return.
pub fn texts(rules: Option<&dyn EstimatedTax>, as_: &[Answers]) -> Vec<Text> {
    let mut out = Vec::new();
    if let Some(rules) = rules {
        let e = rules.explanation();
        add_kind(
            &mut out,
            "explanation summary".into(),
            e.summary(),
            300,
            TextKind::Body,
        );
        for (i, p) in e.body().into_iter().enumerate() {
            add_kind(
                &mut out,
                format!("explanation paragraph {}", i + 1),
                p,
                700,
                TextKind::Body,
            );
        }
        add(
            &mut out,
            "explanation versus federal".into(),
            e.versus_federal(),
            400,
        );
    }
    for a in as_ {
        let h = &a.household.name;
        if let Ok(req) = &a.requirement {
            let r = req.value();
            add(&mut out, format!("{h}: why"), r.why(), 300);
            for o in r.options() {
                add(&mut out, format!("{h}: option label"), o.label(), 80);
                add(&mut out, format!("{h}: option why not"), o.why_not(), 200);
            }
            notes(&mut out, h, &req.required(), &req.divergences());
        }
        if let Ok(s) = &a.schedule {
            notes(&mut out, h, &s.required(), &s.divergences());
        }
        if let Ok(p) = &a.penalty {
            let v = p.value();
            add_kind(
                &mut out,
                format!("{h}: penalty reason"),
                v.reason(),
                200,
                TextKind::Two,
            );
            add_kind(
                &mut out,
                format!("{h}: penalty summary"),
                v.summary(),
                200,
                TextKind::Sentence,
            );
            if let Some(o) = v.waiver_outcome() {
                add_kind(
                    &mut out,
                    format!("{h}: waiver outcome"),
                    o.reason(),
                    250,
                    TextKind::Two,
                );
            }
            notes(&mut out, h, &p.required(), &p.divergences());
        }
    }
    out
}

fn re(pattern: &str) -> Regex {
    match Regex::new(pattern) {
        Ok(r) => r,
        Err(e) => panic!("estimatedtest: bad pattern {pattern}: {e}"),
    }
}

/// The patterns are Go's RE2 patterns, with `\b` as RE2's ASCII word
/// boundary and `\s` as RE2's ASCII space class.
struct Rule {
    re: Regex,
    rule: &'static str,
}

/// The patterns the copy rules exclude, each with the rule.
static FORBIDDEN: LazyLock<Vec<Rule>> = LazyLock::new(|| {
    [
        (r"\$[0-9][0-9,]*\.[0-9]{2}(?-u:\b)", "money in whole dollars, no cents"),
        (r"\$[0-9]{4,}", "money with a thousands separator"),
        (r"(?i)(?-u:\b)ow(e|es|ed|ing)(?-u:\b)", r#""due", not "owe""#),
        (r"(?i)(?-u:\b)(we|we're|we've|our|ours)(?-u:\b)", r#"never "we""#),
        (
            r"(?i)(?-u:\b)this estimate(?-u:\b)|(?-u:\b)the estimate(?-u:\b)|(?-u:\b)modell?ed(?-u:\b)|(?-u:\b)prototype(?-u:\b)|(?-u:\b)fixtures?(?-u:\b)|(?-u:\b)the builder(?-u:\b)",
            "never about the estimate or the software",
        ),
        (
            r"(?i)(?-u:\b)(not|was|were|is|if|when) given(?-u:\b)|(?-u:\b)wasn't given(?-u:\b)|(?-u:\b)weren't given(?-u:\b)",
            "never about what was or wasn't entered",
        ),
        (r"(?i)(?-u:\b)here(?-u:\b)", r#"never "here": the person is not inside the software"#),
        (
            r"(?i)(?-u:\b)this follows(?-u:\b)|(?-u:\b)the default(?-u:\b)|(?-u:\b)defaults?(?-u:\b)|(?-u:\b)not built(?-u:\b)|(?-u:\b)this build(?-u:\b)|(?-u:\b)the library(?-u:\b)|(?-u:\b)the caller(?-u:\b)",
            "never about the software's choices",
        ),
        (
            r"(?i)can'?t be worked out|cannot be worked out|counts only the days|(?-u:\b)isn't known(?-u:\b)|(?-u:\b)is not known(?-u:\b)|(?-u:\b)not known(?-u:\b)|(?-u:\b)unknown(?-u:\b)",
            "never about what the software can't do",
        ),
        (r"(?i)research/|\.md(?-u:\b)|estimated-payments/", "never a file path"),
        (
            r"§|U\.S\.C\.|(?-u:\b)RSMo(?-u:\b)|(?-u:\b)O\.S\.(?-u:\b)|(?-u:\b)IC [0-9]",
            "no statute citations in a sentence; they go in the sources",
        ),
        (r"%!|%\(|\(MISSING\)|(?-u:\b)EXTRA(?-u:\b)", "a formatting error"),
        (r"[−-][\t\n\f\r ]?\$[0-9]", "a negative balance reads as a refund, not a minus sign"),
        (r"(?i)last day a published", "the penalty's summary carries where interest stops"),
    ]
    .into_iter()
    .map(|(p, rule)| Rule { re: re(p), rule })
    .collect()
});

/// Shortenings that drop a word a sentence needs, each with what is
/// missing.
static FRAGMENTS: LazyLock<Vec<Rule>> = LazyLock::new(|| {
    const S: &str = r"[\t\n\f\r ]";
    [
        // "It can for a casualty", "It can also if you retired".
        (
            format!(r"(?-u:\b)(?:[Ii]t|[Tt]hey|[Tt]his|[Tt]hat){S}+(?:can|could|must|may|might|will|would|should|does|do|did){S}+(?:also{S}+|still{S}+|only{S}+|too{S}+)?(?:for|if|when|after|because|by|on|from|with|unless|once|but)(?-u:\b)"),
            "a modal with no verb: say what it can do",
        ),
        // "Nor can it for a casualty".
        (
            format!(r"(?-u:\b)(?:can|could|must|may|will|would|does|do|did){S}+(?:it|they){S}+(?:also{S}+)?(?:for|if|when|after|because|unless)(?-u:\b)"),
            "a modal with no verb: say what it can do",
        ),
        // "Nor is there if you had no tax".
        (
            format!(r"(?-u:\b)is there{S}+(?:if|for|when|unless)(?-u:\b)"),
            "a missing noun: say there is none, or any",
        ),
        // "either year's is under $500".
        (
            format!(r"(?-u:\b)(?:year|month|quarter|period)'s{S}+(?:is|was|are|were|can|could|may|will|would|has|had)(?-u:\b)"),
            "a possessive with no noun: say whose what",
        ),
    ]
    .into_iter()
    .map(|(p, rule)| Rule { re: re(&p), rule })
    .collect()
});

/// The cases an explanation's body leaves to the reasons and divergences,
/// which say whether they apply to the household.
static NICHE: LazyLock<Regex> = LazyLock::new(|| {
    re(
        r"(?i)group return|nonresident|non-resident|part-year|part year|new resident|first-year|first year resident|(?-u:\b)mov(e|es|ed|ing) (out|in|into|to|from|away)(?-u:\b)|(?-u:\b)you move(?-u:\b)|(?-u:\b)farm|(?-u:\b)fisher|(?-u:\b)fishing(?-u:\b)|military|nursing home",
    )
});

/// Finds the end of a sentence followed by another.
static SENTENCE_END: LazyLock<Regex> =
    LazyLock::new(|| re(r#"[.?!]["”’)]?[\t\n\f\r ]+[A-Z0-9(“"$]"#));

/// The most characters a sentence a person reads may have.
pub const MAX_SENTENCE: usize = 80;

/// Splits `text` at each sentence end the sentence pattern finds.
pub fn sentences(text: &str) -> Vec<String> {
    let mut out = Vec::new();
    let mut start = 0;
    for m in SENTENCE_END.find_iter(text) {
        // The match ends on the next sentence's first character.
        let size = text[..m.end()]
            .chars()
            .next_back()
            .map_or(0, char::len_utf8);
        let end = m.end() - size;
        out.push(go_trim_space(&text[start..end]).to_string());
        start = end;
    }
    out.push(go_trim_space(&text[start..]).to_string());
    out
}

/// Go's `strings.TrimSpace`, which trims Unicode white space.
fn go_trim_space(s: &str) -> &str {
    s.trim_matches(|c: char| c.is_whitespace())
}

/// Returns each breach of the copy rules in `texts`, and each text longer
/// than its kind allows. An explanation may have at most four paragraphs,
/// and its summary and body describe the ordinary household: group returns,
/// moves, farmers and the like are left to the reasons and divergences. A
/// penalty's summary is one sentence; a penalty's reason, a waiver's reason
/// and a question's effect are one or two. A question is one sentence
/// ending in a question mark. Every sentence a person reads is at most
/// [`MAX_SENTENCE`] characters. Every question comes from `Notes::ask`, with
/// no question for a rate not yet published or a rule left open, and a
/// question for everything else.
pub fn check_copy(rules: &dyn EstimatedTax, texts: &[Text]) -> Vec<String> {
    let mut out = Vec::new();
    let mut seen: HashSet<String> = HashSet::new();
    for t in texts {
        match t.limit {
            -1 => {
                out.push(format!(
                    "{}: made with Require, not Ask: {}",
                    t.location,
                    quote(&t.text)
                ));
                continue;
            }
            -2 => {
                out.push(format!(
                    "{}: asks {}, though the person can't answer it",
                    t.location,
                    quote(&t.text)
                ));
                continue;
            }
            -3 => {
                out.push(format!(
                    "{}: has no question for the person, only {}",
                    t.location,
                    quote(&t.text)
                ));
                continue;
            }
            _ => {}
        }
        match t.kind {
            TextKind::Body => {
                if let Some(m) = NICHE.find(&t.text) {
                    out.push(format!(
                        "{}: {} is a case for the reasons and divergences, not the explanation: {}",
                        t.location,
                        quote(m.as_str()),
                        quote(&t.text)
                    ));
                }
            }
            TextKind::Sentence | TextKind::Question => {
                if SENTENCE_END.is_match(&t.text) {
                    out.push(format!(
                        "{}: more than one sentence: {}",
                        t.location,
                        quote(&t.text)
                    ));
                }
                if t.kind == TextKind::Question && !t.text.ends_with('?') {
                    out.push(format!(
                        "{}: not a question: {}",
                        t.location,
                        quote(&t.text)
                    ));
                }
            }
            TextKind::Two => {
                let n = sentences(&t.text).len();
                if n > 2 {
                    out.push(format!(
                        "{}: {n} sentences, more than two: {}",
                        t.location,
                        quote(&t.text)
                    ));
                }
            }
            TextKind::Other => {}
        }
        for f in FRAGMENTS.iter() {
            if let Some(m) = f.re.find(&t.text)
                && seen.insert(format!("{}{}", f.rule, t.text))
            {
                out.push(format!(
                    "{}: {} is {}, in {}",
                    t.location,
                    quote(m.as_str()),
                    f.rule,
                    quote(&t.text)
                ));
            }
        }
        for f in FORBIDDEN.iter() {
            if let Some(m) = f.re.find(&t.text) {
                let msg = format!(
                    "{}: {} breaks {} in {}",
                    t.location,
                    quote(m.as_str()),
                    quote(f.rule),
                    quote(&t.text)
                );
                if seen.insert(format!("{}{}", f.rule, t.text)) {
                    out.push(msg);
                }
            }
        }
        for sentence in sentences(&t.text) {
            let n = sentence.chars().count();
            if n > MAX_SENTENCE && seen.insert(format!("sentence{sentence}")) {
                out.push(format!(
                    "{}: a sentence of {n} characters, more than {MAX_SENTENCE}: {}",
                    t.location,
                    quote(&sentence)
                ));
            }
        }
        if t.text.len() as i64 > t.limit && seen.insert(format!("len{}", t.text)) {
            out.push(format!(
                "{}: {} characters, more than {}: {}",
                t.location,
                t.text.len(),
                t.limit,
                quote(&t.text)
            ));
        }
    }
    let n = rules.explanation().body().len();
    if n > 4 {
        out.push(format!(
            "the explanation has {n} paragraphs, more than four"
        ));
    }
    out
}

/// Returns each breach of the answers' invariants:
///   - when a target is known exactly one option is chosen, and none
///     otherwise;
///   - an exempt requirement's chosen option is an exception at $0, and the
///     target is $0;
///   - every penalty has a reason, and under an exception it is the
///     exception's label and the penalty is $0;
///   - every installment's status is [`estimated::status_of`] its running
///     totals;
///   - a jurisdiction's methods return only the errors [`expected`] allows.
pub fn check_answers(as_: &[Answers]) -> Vec<String> {
    let mut out = Vec::new();
    for a in as_ {
        let h = &a.household.name;
        for (i, err) in a.errs().iter().enumerate() {
            if !expected(*err)
                && let Some(err) = err
            {
                out.push(format!(
                    "{h}: {}: {err}",
                    ["Requirement", "Schedule", "Penalty"][i]
                ));
            }
        }
        let mut ds = Vec::new();
        if let Ok(req) = &a.requirement {
            ds.extend(req.divergences());
        }
        if let Ok(p) = &a.penalty {
            ds.extend(p.divergences());
        }
        for d in &ds {
            let why = same_readings(d);
            if !why.is_empty() {
                out.push(format!(
                    "{h}: divergence {} doesn't diverge: {why}",
                    quote(&d.rule())
                ));
            }
        }
        let Ok(req) = &a.requirement else {
            continue;
        };
        let r = req.value();
        let chosen_opt = r.chosen();
        let chosen = r
            .options()
            .iter()
            .filter(|o| chosen_opt.as_ref() == Some(*o))
            .count();
        let ok = chosen_opt.is_some();
        let c = chosen_opt.unwrap_or_default();
        if r.target_known() && (!ok || chosen != 1) {
            out.push(format!(
                "{h}: a target is known but {chosen} options are chosen"
            ));
        } else if !r.target_known() && ok {
            out.push(format!(
                "{h}: no target is known but {} is chosen",
                quote(&c.label())
            ));
        } else if r.exempt()
            && (c.basis() != Basis::Exception || c.amount().0 != 0 || r.target().0 != 0)
        {
            out.push(format!(
                "{h}: exempt, but the chosen option is {} at {}",
                c.basis(),
                c.amount()
            ));
        } else if ok && c.basis() == Basis::Exception && !r.exempt() {
            out.push(format!(
                "{h}: an exception is chosen but the requirement is not exempt"
            ));
        }
        if let Ok(s) = &a.schedule {
            for i in s.value() {
                let want = estimated::status_of(
                    i.needed_by(),
                    i.credited_by(),
                    i.status() == Status::Excused,
                );
                if i.status() != want {
                    out.push(format!(
                        "{h}: installment {} is {}, want {want}",
                        i.number(),
                        i.status()
                    ));
                }
            }
        }
        if let Ok(pen) = &a.penalty
            && !pen.value().rated_through().is_zero()
        {
            // The interest stops at the last published rate, so the penalty
            // is no maximum: nothing may call it one, and the unpublished
            // rate is a question of its own.
            let p = pen.value();
            if p.upper_bound() || p.summary().contains("At most") {
                out.push(format!(
                    "{h}: the penalty stops at {} but is called a maximum: {}",
                    p.rated_through(),
                    quote(&p.summary())
                ));
            }
            let asked = pen
                .required()
                .iter()
                .any(|q| q.position() == PositionKind::UnpublishedRate);
            if !asked {
                out.push(format!(
                    "{h}: the penalty stops at {} with no unpublished-rate question",
                    p.rated_through()
                ));
            }
            for t in texts(None, std::slice::from_ref(a)) {
                if t.text.to_lowercase().contains("at most") {
                    out.push(format!(
                        "{h}: the penalty stops at {}, yet {} says {}",
                        p.rated_through(),
                        t.location,
                        quote(&t.text)
                    ));
                }
            }
        }
        {
            // An effect agrees with the options in the same result: one that
            // says the target comes from last year needs last year's option
            // open, and one that says there's no target needs none.
            let last_open = r.options().iter().any(|o| {
                o.available()
                    && (o.basis() == Basis::PriorYearTax || o.basis() == Basis::PriorYearIncome)
            });
            for q in req.required() {
                let e = q.effect().to_lowercase();
                if e.contains("target can only come from last year") && !last_open {
                    out.push(format!(
                        "{h}: {}, but last year's option isn't open",
                        quote(&q.effect())
                    ));
                } else if e.contains("there's no target") && r.target_known() {
                    out.push(format!(
                        "{h}: {}, but there is a target",
                        quote(&q.effect())
                    ));
                }
            }
        }
        if r.exempt() {
            // An exception sets the target, so no answer changes it.
            for q in req.required() {
                if q.effect().to_lowercase().contains("target") {
                    out.push(format!(
                        "{h}: an exception sets the target, yet a question says {}",
                        quote(&q.effect())
                    ));
                }
            }
        }
        if let Ok(pen) = &a.penalty {
            let p = pen.value();
            // If an eligible waiver is granted, what's left is the charges
            // of the kinds it keeps; one that says it leaves the interest
            // keeps the interest.
            if let Some(o) = p.waiver_outcome()
                && o.eligible()
            {
                let mut left = USCents(0);
                for l in p.lines() {
                    for part in l.parts() {
                        for k in o.kept() {
                            if part.kind() == k {
                                left += part.charge();
                            }
                        }
                    }
                }
                let keeps_interest = o.kept().contains(&PartKind::Interest);
                if p.if_waived() != left {
                    out.push(format!(
                        "{h}: if waived {}, but the charges the waiver keeps are {left}",
                        p.if_waived()
                    ));
                } else if o.reason().contains("not the interest") && !keeps_interest {
                    out.push(format!(
                        "{h}: {}, but the waiver leaves no interest",
                        quote(&o.reason())
                    ));
                }
            }
            // A waiver asked for, where a penalty arises, has an outcome.
            if let Some(k) = a.household.facts.positions().waiver()
                && k != WaiverKind::None
                && p.total().0 > 0
                && p.waiver_outcome().is_none()
            {
                out.push(format!(
                    "{h}: a {k} waiver was asked for, and the penalty says nothing of it"
                ));
            }
            // A waiver asked for is reported on the penalty, which is
            // figured before it, wherever a penalty can arise.
            if let Some(asked) = a.household.facts.positions().waiver()
                && !r.exempt()
                && p.waiver() != asked
            {
                out.push(format!(
                    "{h}: the {asked} waiver asked for is reported as {}",
                    p.waiver()
                ));
            }
            if p.rated_through().is_zero() {
                for q in pen.required() {
                    if q.position() == PositionKind::UnpublishedRate
                        && q.effect().contains("rate for the days after")
                    {
                        out.push(format!(
                            "{h}: {}, but the penalty doesn't record the last rated day",
                            quote(&q.effect())
                        ));
                    }
                }
            }
            if p.reason().is_empty() {
                out.push(format!("{h}: the penalty has no reason"));
            } else if r.exempt()
                && (p.total().0 != 0 || p.reason() != estimated::exception_applies(&c))
            {
                out.push(format!(
                    "{h}: exempt under {}, but the penalty is {} because {}",
                    quote(&c.label()),
                    p.total(),
                    quote(&p.reason())
                ));
            }
            // The reason counts the due dates the schedule shows short, and
            // so does short_due_dates.
            if let Ok(s) = &a.schedule {
                let short = s
                    .value()
                    .iter()
                    .filter(|i| i.status() == Status::Short)
                    .count() as i64;
                if p.short_due_dates() != short {
                    out.push(format!(
                        "{h}: the penalty counts {} short due dates, the schedule {short}",
                        p.short_due_dates()
                    ));
                }
            }
            const WORDS: [&str; 10] = [
                "no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine",
            ];
            let n = p.short_due_dates();
            if !r.exempt()
                && n > 0
                && n < WORDS.len() as i64
                && !p
                    .reason()
                    .contains(&format!("fall short on {} due date", WORDS[n as usize]))
            {
                out.push(format!(
                    "{h}: {n} due dates are short, but the reason is {}",
                    quote(&p.reason())
                ));
            }
        }
    }
    out
}

/// Rebuilds `b` with no withholding and every payment of the tax year moved
/// to Oct 15 or later.
fn washington(b: &FactsBuilder, year: i64) -> FactsBuilder {
    let Ok(f) = b.build() else {
        return b.clone();
    };
    let mut out = estimated::new_facts(f.jurisdiction(), f.tax_year());
    out.filing_status(f.filing_status())
        .this_year(f.this_year())
        .residence(&f.residence())
        .prior_residence(&f.prior_residence())
        .people(&f.people())
        .positions(f.positions());
    if let Some(p) = f.prior_year() {
        out.prior_year(p);
    }
    if !f.return_filed().is_zero() {
        out.return_filed(f.return_filed());
    }
    if !f.requirement_first_met().is_zero() {
        out.requirement_first_met(f.requirement_first_met());
    }
    let oct15 = estimated::must_date(year, Month::OCTOBER, 15);
    for p in f.payments() {
        let on = if p.on().before(oct15) { oct15 } else { p.on() };
        if let Ok(moved) = estimated::new_payment().on(on).amount(p.amount()).build() {
            out.paid(&[moved]);
        }
    }
    out
}

/// An amount, a percentage or a date in a reading.
static FIGURE: LazyLock<Regex> = LazyLock::new(|| {
    re(
        r"\$[0-9][0-9,]*|[0-9][0-9.,/]*%|(?-u:\b)(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]* [0-9]+(?-u:\b)",
    )
});

fn figures_in(s: &str) -> String {
    let set: BTreeSet<&str> = FIGURE.find_iter(s).map(|m| m.as_str()).collect();
    set.into_iter().collect::<Vec<_>>().join(", ")
}

static NOT_WORDS: LazyLock<Regex> = LazyLock::new(|| re(r"[^a-z0-9%$]+"));

/// Says why a divergence's two readings agree, or "" when they differ: the
/// same text, or the same figures with the statute's reading calling them
/// "the same".
fn same_readings(d: &Divergence) -> String {
    let norm = |s: &str| go_trim_space(&NOT_WORDS.replace_all(&s.to_lowercase(), " ")).to_string();
    if norm(&d.form()) == norm(&d.statute()) {
        return "the form and the statute read alike".to_string();
    }
    let f = figures_in(&d.form());
    if !f.is_empty()
        && f == figures_in(&d.statute())
        && d.statute().to_lowercase().contains("the same")
    {
        return format!("both readings give {f}, and the statute's calls them the same");
    }
    String::new()
}

#[cfg(test)]
mod tests;
