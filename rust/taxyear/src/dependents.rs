//! Who counts as a dependent: dependents.go.

use money::USCents;
use proto::factsv1::{self, __buffa::oneof::position::Answer, Person, Relationship, Reply};
use proto::federalv1::Dependent;
use proto::scenariosv1::{Dependency, DependencyStatus, DependencyTest, Statement};

use crate::model::{self, Context, Notes};

/// relativeIncomeLimit is a qualifying relative's gross income limit, the
/// exemption amount of 26 U.S.C. § 152(d)(1)(B) as each year's revenue
/// procedure sets it.
fn relative_income_limit(year: i64) -> Option<USCents> {
    match year {
        2025 => Some(money::dollars(5_200)),
        2026 => Some(money::dollars(5_300)),
        _ => None,
    }
}

/// The catalogue's questions a qualifying relative waits on.
const INCOME_QUESTION: &str = "dependent.gross_income_under_limit";
const SUPPORT_QUESTION: &str = "dependent.more_than_half_support";

/// dependents works out whether each person other than you and your spouse
/// counts as your dependent: a qualifying child by relationship, residence
/// and age; otherwise a qualifying relative by income and support, asked
/// as questions. It asks those questions in notes.
pub(crate) fn dependents(ctx: &Context, notes: &mut Notes) -> Vec<Dependency> {
    let mut out = Vec::new();
    for p in &ctx.facts().people {
        if let Some(Relationship::RELATIONSHIP_YOU | Relationship::RELATIONSHIP_SPOUSE) =
            p.relationship.as_known()
        {
            continue;
        }
        out.push(dependency(ctx, p, notes));
    }
    out
}

fn dependency(ctx: &Context, p: &Person, notes: &mut Notes) -> Dependency {
    let mut d = Dependency {
        person: p.id.clone(),
        ..Default::default()
    };
    let say = |mut d: Dependency,
               status: DependencyStatus,
               test: Option<DependencyTest>,
               text: &str|
     -> Dependency {
        d.status = status.into();
        d.test = match test {
            Some(t) => t.into(),
            None => buffa::EnumValue::from(0),
        };
        d.text = Statement {
            text: text.to_string(),
            ..Default::default()
        }
        .into();
        d
    };
    let (age, born) = ctx.age_at_year_end(&p.id);
    let rel = p.relationship.as_known();
    let child_kind = matches!(
        rel,
        Some(Relationship::RELATIONSHIP_CHILD | Relationship::RELATIONSHIP_SIBLING)
    );
    let child = DependencyTest::DEPENDENCY_TEST_QUALIFYING_CHILD;
    let relative = DependencyTest::DEPENDENCY_TEST_QUALIFYING_RELATIVE;
    if child_kind && !born {
        return say(
            d,
            DependencyStatus::DEPENDENCY_STATUS_UNKNOWN,
            Some(child),
            "Whether they count as your dependent goes by their date of birth",
        );
    }
    if child_kind
        && p.months_lived_with_you >= 7
        && (p.permanently_disabled || age < 19 || (age < 24 && p.full_time_student))
    {
        return say(
            d,
            DependencyStatus::DEPENDENCY_STATUS_COUNTS,
            Some(child),
            "Counts as your qualifying child",
        );
    }
    if rel == Some(Relationship::RELATIONSHIP_NOT_RELATED) && p.months_lived_with_you < 12 {
        return say(
            d,
            DependencyStatus::DEPENDENCY_STATUS_DOES_NOT_COUNT,
            None,
            "Doesn’t count as your dependent: they didn’t live with you all year",
        );
    }
    let Some(limit) = relative_income_limit(ctx.year()) else {
        let text = format!(
            "Whether {} as your dependent for {} isn’t worked out yet",
            counts(p),
            ctx.year()
        );
        return say(
            d,
            DependencyStatus::DEPENDENCY_STATUS_UNKNOWN,
            Some(relative),
            &text,
        );
    };
    let (income, support) = ask_relative(ctx, p, limit, notes, &mut d);
    let (no, yes) = (Reply::REPLY_NO as i32, Reply::REPLY_YES as i32);
    let unspecified = Reply::REPLY_UNSPECIFIED as i32;
    if income == no {
        return say(
            d,
            DependencyStatus::DEPENDENCY_STATUS_DOES_NOT_COUNT,
            Some(relative),
            "Doesn’t count as your dependent: their income is over the limit",
        );
    }
    if support == no {
        return say(
            d,
            DependencyStatus::DEPENDENCY_STATUS_DOES_NOT_COUNT,
            Some(relative),
            "Doesn’t count as your dependent: you didn’t provide more than half of their support",
        );
    }
    if income == yes && support == yes {
        return say(
            d,
            DependencyStatus::DEPENDENCY_STATUS_COUNTS,
            Some(relative),
            "Counts as your qualifying relative",
        );
    }
    if income == unspecified || support == unspecified {
        return say(
            d,
            DependencyStatus::DEPENDENCY_STATUS_COULD_COUNT,
            Some(relative),
            "Could count as your qualifying relative",
        );
    }
    say(
        d,
        DependencyStatus::DEPENDENCY_STATUS_MAY_COUNT,
        Some(relative),
        "May count as your qualifying relative",
    )
}

/// counts is "Rosa counts", or "they count" for someone unnamed.
fn counts(p: &Person) -> String {
    if p.name.is_empty() {
        return "they count".to_string();
    }
    format!("{} counts", p.name)
}

/// askRelative asks the qualifying relative's two questions, answered or
/// not, and returns their replies, as numbers.
fn ask_relative(
    ctx: &Context,
    p: &Person,
    limit: USCents,
    notes: &mut Notes,
    d: &mut Dependency,
) -> (i32, i32) {
    let whose = if p.name.is_empty() {
        "their".to_string()
    } else {
        format!("{}’s", p.name)
    };
    let mut ask = |question: &str, text: String, effect: &str| -> i32 {
        let id = format!("{question}.{}", p.id);
        d.questions.push(id.clone());
        notes.ask(model::new_question(
            id,
            format!("dependents[{}]", p.id),
            text,
            effect,
            "facts.positions",
            &["Yes", "No", "Not sure"],
        ));
        reply(ctx.facts(), question, &p.id)
    };
    let income = ask(
        INCOME_QUESTION,
        format!("Was {whose} gross income under {}?", limit.whole()),
        "Until it is answered yes, they could count as your qualifying relative; no means they don’t.",
    );
    let support = ask(
        SUPPORT_QUESTION,
        format!("Did you provide more than half of {whose} support?"),
        "Until it is answered yes, they could count as your qualifying relative; no means they don’t.",
    );
    (income, support)
}

/// reply is the facts' answer to a catalogue question about a person.
fn reply(f: &factsv1::TaxYearFacts, question: &str, person: &str) -> i32 {
    for pos in &f.positions {
        if pos.question != question {
            continue;
        }
        if pos.subjects.iter().any(|s| s == person) {
            return match &pos.answer {
                Some(Answer::Reply(r)) => r.to_i32(),
                _ => 0,
            };
        }
    }
    Reply::REPLY_UNSPECIFIED as i32
}

/// dependentRows lists those who count as the Form 1040 dependents block
/// names them. The credit checkboxes stay clear: the credits aren't worked
/// out yet.
pub(crate) fn dependent_rows(ctx: &Context, ds: &[Dependency]) -> Vec<Dependent> {
    let mut out = Vec::new();
    for d in ds {
        if d.status.as_known() != Some(DependencyStatus::DEPENDENCY_STATUS_COUNTS) {
            continue;
        }
        let p = ctx.person(&d.person);
        out.push(Dependent {
            name: p.map(|p| p.name.clone()).unwrap_or_default(),
            relationship: p
                .map(|p| relationship_words(p.relationship.to_i32()))
                .unwrap_or("")
                .to_string(),
            ..Default::default()
        });
    }
    out
}

/// relationshipWords is a relationship as the dependents block prints it.
fn relationship_words(r: i32) -> &'static str {
    match r {
        x if x == Relationship::RELATIONSHIP_CHILD as i32 => "child",
        x if x == Relationship::RELATIONSHIP_PARENT as i32 => "parent",
        x if x == Relationship::RELATIONSHIP_SIBLING as i32 => "sibling",
        x if x == Relationship::RELATIONSHIP_OTHER_RELATIVE as i32 => "relative",
        x if x == Relationship::RELATIONSHIP_NOT_RELATED as i32 => "member of household",
        _ => "",
    }
}
