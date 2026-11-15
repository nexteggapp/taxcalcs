//! Every type in this file has private fields and accessors that return
//! copies, and is made by a builder whose `build` validates. A result cannot
//! change after the calculator that made it has returned it.

use crate::divergence::{Divergence, DivergenceBuilder};
use crate::{Error, PositionKind};

type StdResult<T, E> = std::result::Result<T, E>;

/// A published rule and what applying it to the caller's figures did.
///
/// Every calculator returns these so that a product can show its working.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct Rule {
    cite: String,
    applied: String,
}

impl Rule {
    /// Names the authority, as "26 U.S.C. § 1(j)(2)(C)",
    /// "Rev. Proc. 2025-32 § 3.14(3)" or "RCW 82.87.040(1)".
    pub fn cite(&self) -> String {
        self.cite.clone()
    }

    /// Says what the rule did, in the caller's own numbers.
    pub fn applied(&self) -> String {
        self.applied.clone()
    }
}

/// Builds a [`Rule`].
#[derive(Clone, Debug, Default)]
pub struct RuleBuilder {
    r: Rule,
}

/// Starts a rule citing an authority.
pub fn new_rule(cite: impl Into<String>) -> RuleBuilder {
    RuleBuilder {
        r: Rule {
            cite: cite.into(),
            applied: String::new(),
        },
    }
}

impl RuleBuilder {
    /// Records what the rule did.
    pub fn applied(&mut self, text: impl Into<String>) -> &mut Self {
        self.r.applied = text.into();
        self
    }

    /// Requires both the citation and what the rule did. A rule with either
    /// missing is one nobody can check.
    pub fn build(&self) -> StdResult<Rule, Error> {
        if self.r.cite.is_empty() {
            return Err(Error::RuleNeedsCite);
        }
        if self.r.applied.is_empty() {
            return Err(Error::RuleNeedsApplied(self.r.cite.clone()));
        }
        Ok(self.r.clone())
    }
}

/// A position the caller has not supplied.
///
/// A calculator returns one instead of guessing. What it means is the
/// caller's business: a product asks the user, a script reads a
/// configuration, a preparer's tool renders a form field.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct Required {
    position: PositionKind,
    because: String,
    question: String,
    effect: String,
    asked: bool,
}

impl Required {
    /// The kind of choice or fact the caller owes.
    pub fn position(&self) -> PositionKind {
        self.position
    }

    /// States the rule that makes this a choice rather than arithmetic, and
    /// the default the calculator used instead. For a position made with
    /// [`Notes::ask`] it is the question and the effect together.
    pub fn because(&self) -> String {
        self.because.clone()
    }

    /// A question the person can answer, as "What do you expect your 2026
    /// California tax to be?", or empty when the position is not theirs to
    /// answer, as a rate not yet published. Set only through [`Notes::ask`].
    pub fn question(&self) -> String {
        self.question.clone()
    }

    /// One or two plain sentences saying what the position changes, as
    /// "Without it, the target can only come from last year's tax." Set only
    /// through [`Notes::ask`].
    pub fn effect(&self) -> String {
        self.effect.clone()
    }

    /// Reports that the position was made with [`Notes::ask`], so it has an
    /// effect and, where the person can answer it, a question.
    pub fn asked(&self) -> bool {
        self.asked
    }
}

/// Builds a [`Required`].
#[derive(Clone, Debug, Default)]
pub struct RequiredBuilder {
    r: Required,
}

/// Starts a position of the given kind.
pub fn new_required(position: PositionKind) -> RequiredBuilder {
    RequiredBuilder {
        r: Required {
            position,
            ..Required::default()
        },
    }
}

impl RequiredBuilder {
    /// Records the rule that makes it a position.
    pub fn because(&mut self, text: impl Into<String>) -> &mut Self {
        self.r.because = text.into();
        self
    }

    /// Requires a known kind and a reason. A position made with
    /// [`Notes::ask`] also needs its effect, and a question, if it has one,
    /// that ends with a question mark.
    pub fn build(&self) -> StdResult<Required, Error> {
        let r = &self.r;
        if r.position == PositionKind::Unspecified {
            return Err(Error::RequiredNeedsKind);
        }
        if r.because.is_empty() {
            return Err(Error::RequiredNeedsBecause(r.position));
        }
        if r.asked && r.effect.is_empty() {
            return Err(Error::RequiredNeedsEffect(r.position));
        }
        if r.asked && !r.question.is_empty() && !r.question.ends_with('?') {
            return Err(Error::NotAQuestion(r.question.clone()));
        }
        Ok(r.clone())
    }
}

/// A computed figure, the rules that produced it, and the positions the
/// caller still owes.
///
/// A non-empty `required` does not mean `value` is wrong. It means `value`
/// rests on the calculator's stated default, and the caller can change the
/// answer by supplying the position.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Result<T> {
    value: T,
    applied: Vec<Rule>,
    required: Vec<Required>,
    divergences: Vec<Divergence>,
}

impl<T: Clone> Result<T> {
    /// The figure, as a copy; the element types every calculator here
    /// returns are themselves immutable.
    pub fn value(&self) -> T {
        self.value.clone()
    }
}

impl<T> Result<T> {
    /// The rules that produced the figure, in the order applied.
    pub fn applied(&self) -> Vec<Rule> {
        self.applied.clone()
    }

    /// The positions the caller has not supplied, each with the default used
    /// instead.
    pub fn required(&self) -> Vec<Required> {
        self.required.clone()
    }

    /// The places where the authority's form and its statute disagree about
    /// a rule this result used. The figure follows the form, and each entry
    /// says what the statute says instead. Most calculators never set it.
    pub fn divergences(&self) -> Vec<Divergence> {
        self.divergences.clone()
    }

    /// Reports whether the result depends on a position the caller has not
    /// supplied.
    pub fn needs(&self) -> bool {
        !self.required.is_empty()
    }
}

/// The rules, positions and divergences behind a figure that is not yet
/// known.
///
/// A calculator that answers several questions from one assessment collects
/// its working here first, then hands it to each [`ResultBuilder`] with
/// [`ResultBuilder::with`]. Notes is part of a builder: it is checked when
/// that builder's `build` runs, not before.
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Notes {
    rules: Vec<Rule>,
    required: Vec<Required>,
    divergences: Vec<Divergence>,
}

impl Notes {
    /// Records that the rule citing `cite` did what `applied` says.
    pub fn rule(&mut self, cite: impl Into<String>, applied: impl Into<String>) -> &mut Self {
        self.rules.push(Rule {
            cite: cite.into(),
            applied: applied.into(),
        });
        self
    }

    /// Records rules already built, as those of another result.
    pub fn applied(&mut self, rules: &[Rule]) -> &mut Self {
        self.rules.extend_from_slice(rules);
        self
    }

    /// Records a position the caller owes, and the rule that makes it one.
    pub fn require(&mut self, position: PositionKind, because: impl Into<String>) -> &mut Self {
        self.required.push(Required {
            position,
            because: because.into(),
            ..Required::default()
        });
        self
    }

    /// Records a position as a question the person can answer and the effect
    /// of answering it. `question` is empty when the position is not the
    /// person's to answer, as a rate not yet published or a rule the
    /// authority leaves open; `effect` is one or two sentences.
    pub fn ask(
        &mut self,
        position: PositionKind,
        question: impl Into<String>,
        effect: impl Into<String>,
    ) -> &mut Self {
        let (question, effect) = (question.into(), effect.into());
        self.required.push(Required {
            position,
            because: format!("{question} {effect}").trim().to_string(),
            question,
            effect,
            asked: true,
        });
        self
    }

    /// Records positions already built, as those of another result.
    pub fn required(&mut self, rs: &[Required]) -> &mut Self {
        self.required.extend_from_slice(rs);
        self
    }

    /// Records where a form and its statute disagree on a rule used.
    pub fn diverges(&mut self, ds: &[Divergence]) -> &mut Self {
        self.divergences.extend_from_slice(ds);
        self
    }

    /// How many rules have been recorded.
    pub fn len(&self) -> i64 {
        self.rules.len() as i64
    }

    /// Whether no rule has been recorded.
    pub fn is_empty(&self) -> bool {
        self.rules.is_empty()
    }

    /// Validates every entry, as [`RuleBuilder::build`] and
    /// [`RequiredBuilder::build`] would.
    fn check(&self) -> StdResult<(), Error> {
        for r in &self.rules {
            RuleBuilder { r: r.clone() }.build()?;
        }
        for r in &self.required {
            RequiredBuilder { r: r.clone() }.build()?;
        }
        for d in &self.divergences {
            DivergenceBuilder::from_divergence(d.clone()).build()?;
        }
        Ok(())
    }
}

/// Builds a [`Result`].
#[derive(Clone, Debug, Default)]
pub struct ResultBuilder<T> {
    value: T,
    notes: Notes,
}

/// Starts a result for a figure.
pub fn new_result<T>(value: T) -> ResultBuilder<T> {
    ResultBuilder {
        value,
        notes: Notes::default(),
    }
}

impl<T: Clone> ResultBuilder<T> {
    /// Records that the rule citing `cite` did what `applied` says.
    pub fn rule(&mut self, cite: impl Into<String>, applied: impl Into<String>) -> &mut Self {
        self.notes.rule(cite, applied);
        self
    }

    /// Records rules already built, as those of another result.
    pub fn applied(&mut self, rules: &[Rule]) -> &mut Self {
        self.notes.applied(rules);
        self
    }

    /// Records a position the caller owes.
    pub fn require(&mut self, position: PositionKind, because: impl Into<String>) -> &mut Self {
        self.notes.require(position, because);
        self
    }

    /// Records a position as a question and its effect, as [`Notes::ask`].
    pub fn ask(
        &mut self,
        position: PositionKind,
        question: impl Into<String>,
        effect: impl Into<String>,
    ) -> &mut Self {
        self.notes.ask(position, question, effect);
        self
    }

    /// Records positions already built, as those of another result.
    pub fn required(&mut self, rs: &[Required]) -> &mut Self {
        self.notes.required(rs);
        self
    }

    /// Records where a form and its statute disagree on a rule used.
    pub fn diverges(&mut self, ds: &[Divergence]) -> &mut Self {
        self.notes.diverges(ds);
        self
    }

    /// Adds everything in `n` after what is recorded so far. Later changes
    /// to `n` do not reach this builder.
    pub fn with(&mut self, n: &Notes) -> &mut Self {
        self.notes.applied(&n.rules);
        self.notes.required(&n.required);
        self.notes.diverges(&n.divergences);
        self
    }

    /// Checks every rule, position and divergence, and returns a result
    /// nothing can change.
    pub fn build(&self) -> StdResult<Result<T>, Error> {
        self.notes.check()?;
        Ok(Result {
            value: self.value.clone(),
            applied: self.notes.rules.clone(),
            required: self.notes.required.clone(),
            divergences: self.notes.divergences.clone(),
        })
    }
}
