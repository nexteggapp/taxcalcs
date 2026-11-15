//! Divergences: where a tax authority's form and its statute disagree.

use std::fmt;

use crate::Error;

/// A rule on which a tax authority's form and its statute disagree.
///
/// A calculator in this module computes what the current form computes,
/// because the form is what a person filing follows and, as far as anything
/// published shows, what is assessed. It reports the statute's reading beside
/// it, with the source for each, so that a reviewer can see both and a later
/// change to either is noticed.
///
/// A statutory exception the form omits is a divergence too. It is reported
/// as available rather than applied or ignored.
///
/// The fields are private and set only through [`DivergenceBuilder`], so a
/// value cannot change after it is checked.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct Divergence {
    rule: String,
    form: String,
    form_source: String,
    statute: String,
    statute_source: String,
    uses: Reading,
    affects: bool,
    affects_set: bool,
}

/// Which of the two documents a figure follows.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum Reading {
    /// No reading; a divergence refuses it.
    #[default]
    Unspecified,

    /// The form's reading: the usual case.
    Form,

    /// The statute's reading, used where the form predates a change in the
    /// law.
    Statute,
}

/// Names the reading: "the form" or "the statute".
impl fmt::Display for Reading {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            Reading::Form => "the form",
            Reading::Statute => "the statute",
            Reading::Unspecified => "unspecified reading",
        })
    }
}

impl Divergence {
    /// Names the rule the two documents disagree about.
    pub fn rule(&self) -> String {
        self.rule.clone()
    }

    /// Says what the form does, in plain words.
    pub fn form(&self) -> String {
        self.form.clone()
    }

    /// Cites the form, its line or section, and its year.
    pub fn form_source(&self) -> String {
        self.form_source.clone()
    }

    /// Says what the statute says instead.
    pub fn statute(&self) -> String {
        self.statute.clone()
    }

    /// Cites the statute.
    pub fn statute_source(&self) -> String {
        self.statute_source.clone()
    }

    /// The reading the figures follow.
    pub fn uses(&self) -> Reading {
        self.uses
    }

    /// Reports whether the two readings give this household different
    /// figures or answers. A divergence that does not is still reported, so
    /// a reviewer sees every rule that was read, and a product can leave it
    /// out of what it shows the household.
    pub fn affects_household(&self) -> bool {
        self.affects
    }
}

/// Builds a [`Divergence`].
#[derive(Clone, Debug, Default)]
pub struct DivergenceBuilder {
    d: Divergence,
}

/// Starts a divergence on the named rule.
pub fn new_divergence(rule: impl Into<String>) -> DivergenceBuilder {
    DivergenceBuilder {
        d: Divergence {
            rule: rule.into(),
            ..Divergence::default()
        },
    }
}

impl DivergenceBuilder {
    pub(crate) fn from_divergence(d: Divergence) -> DivergenceBuilder {
        DivergenceBuilder { d }
    }

    /// Records what the form does and where it says so.
    pub fn form(&mut self, does: impl Into<String>, source: impl Into<String>) -> &mut Self {
        self.d.form = does.into();
        self.d.form_source = source.into();
        self
    }

    /// Records what the statute says and where.
    pub fn statute(&mut self, says: impl Into<String>, source: impl Into<String>) -> &mut Self {
        self.d.statute = says.into();
        self.d.statute_source = source.into();
        self
    }

    /// Records the reading the figures follow.
    pub fn uses(&mut self, r: Reading) -> &mut Self {
        self.d.uses = r;
        self
    }

    /// Records whether the two readings change anything for the facts the
    /// rules were given.
    pub fn affects(&mut self, affects: bool) -> &mut Self {
        self.d.affects = affects;
        self.d.affects_set = true;
        self
    }

    /// Checks that every part is present. A divergence missing either
    /// reading or either source cannot be checked by anybody, which is the
    /// reason to report it.
    pub fn build(&self) -> Result<Divergence, Error> {
        let d = &self.d;
        if d.rule.is_empty() {
            return Err(Error::DivergenceNeedsRule);
        }
        if d.form.is_empty() || d.form_source.is_empty() {
            return Err(Error::DivergenceNeedsForm);
        }
        if d.statute.is_empty() || d.statute_source.is_empty() {
            return Err(Error::DivergenceNeedsStatute);
        }
        if d.uses != Reading::Form && d.uses != Reading::Statute {
            return Err(Error::DivergenceNeedsReading);
        }
        if !d.affects_set {
            return Err(Error::DivergenceNeedsAffects);
        }
        Ok(d.clone())
    }
}
