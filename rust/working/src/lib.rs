//! What a calculator returns besides the number: the rules it applied, and
//! the choices it could not make for you. Ported from the Go package
//! `working`.
//!
//! A tax figure a user cannot check is one they have to take on trust, and
//! they cannot tell when it is wrong. So every calculator in this workspace
//! returns a [`Result`], which carries the figure, the [`Rule`]s that produced
//! it in the caller's own numbers, and any [`Required`] position the caller
//! still owes.
//!
//! The wire mirror of this package is us/v1/working.proto, and the names
//! match deliberately.
//!
//! # Why this is shared across jurisdictions
//!
//! Showing your working is not a federal idea. California's standard-versus-
//! itemised election and the federal one are the same kind of unanswerable
//! question, and a product rendering an "unanswered" list wants one list
//! rather than one per jurisdiction. Sharing the envelope is what lets a
//! caller loop over federal and state results together.
//!
//! What is not shared is the content. A [`Rule`] cites whatever authority the
//! calculator that made it answers to — 26 U.S.C. for the federal
//! calculators, Rev. & Tax. Code for California, ORS for Oregon, RCW for
//! Washington.
//!
//! # From Go
//!
//! Go's `Result[T]` is [`Result<T>`]; refer to it as `working::Result` beside
//! the standard library's. Builders take `&mut self` and return `&mut Self`,
//! so they chain as Go's do and can also be called statement by statement;
//! `build` takes `&self` and validates. Go's variadic arguments are slices.

use std::fmt;

mod divergence;
mod result;

pub use divergence::{Divergence, DivergenceBuilder, Reading, new_divergence};
pub use result::{
    Notes, Required, RequiredBuilder, Result, ResultBuilder, Rule, RuleBuilder, new_required,
    new_result, new_rule,
};

/// An error from one of this package's builders. Its text is the Go error's.
#[derive(Clone, Debug, PartialEq, Eq, thiserror::Error)]
pub enum Error {
    /// A rule with no citation.
    #[error("working: a rule needs the authority it cites")]
    RuleNeedsCite,
    /// A rule that does not say what it did; it holds the citation.
    #[error("working: the rule citing {0} needs to say what it did")]
    RuleNeedsApplied(String),
    /// A position of no known kind.
    #[error("working: a required position needs its kind")]
    RequiredNeedsKind,
    /// A position with no reason.
    #[error("working: the {0} position needs the rule that makes it one")]
    RequiredNeedsBecause(PositionKind),
    /// A position made with [`Notes::ask`] with no effect.
    #[error("working: the {0} position needs its effect")]
    RequiredNeedsEffect(PositionKind),
    /// A question that does not end with a question mark.
    #[error("working: {} is not a question", quote(.0))]
    NotAQuestion(String),
    /// A divergence with no rule.
    #[error("working: a divergence needs the rule it is about")]
    DivergenceNeedsRule,
    /// A divergence missing the form's reading or its source.
    #[error("working: a divergence needs what the form does and its source")]
    DivergenceNeedsForm,
    /// A divergence missing the statute's reading or its source.
    #[error("working: a divergence needs what the statute says and its source")]
    DivergenceNeedsStatute,
    /// A divergence that does not say which reading the figures follow.
    #[error("working: a divergence needs the reading the figures follow")]
    DivergenceNeedsReading,
    /// A divergence that does not say whether it affects the facts.
    #[error("working: a divergence needs whether it affects these facts")]
    DivergenceNeedsAffects,
}

/// Names a choice that the figures alone do not determine.
///
/// These are the places tax law admits more than one answer, or where the
/// answer depends on something outside the numbers a calculator was handed.
/// The kinds are jurisdiction-neutral because the questions are: a state that
/// conforms to the federal definition of adjusted gross income inherits the
/// federal questions along with it. Which statute makes a given case a choice
/// is said in [`Required::because`] by the calculator that returns it.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, PartialOrd, Ord, Hash)]
pub enum PositionKind {
    /// No kind; every builder refuses it.
    #[default]
    Unspecified,

    /// Whether to take the standard deduction or itemise. Itemising wins only
    /// if the itemised total is larger, and a calculator cannot know the
    /// total without being given it.
    ///
    /// 26 U.S.C. § 63(e) makes it an election federally. California and
    /// Oregon each pose the same question on their own returns, and neither
    /// is bound by what the federal return elected.
    DeductionMethod,

    /// Which lots a sale consumed, which decides basis and holding period.
    /// The default is first in, first out; specific identification requires
    /// the seller to have identified the lots at the time of sale.
    LotSelection,

    /// Whether a traditional IRA contribution is deductible, which under 26
    /// U.S.C. § 219(g) depends on modified AGI and on whether the taxpayer or
    /// their spouse is covered by a workplace plan.
    IraDeductibility,

    /// Whether a security bought inside the wash-sale window under 26 U.S.C.
    /// § 1091 is substantially identical to the one sold. Two index funds
    /// tracking one index are the unsettled case.
    SubstantiallyIdentical,

    /// Whether an activity is a trade or business under 26 U.S.C. § 162,
    /// rather than a hobby under § 183. It decides whether net earnings are
    /// subject to self-employment tax.
    TradeOrBusiness,

    /// Whether a person is a qualifying child or relative under 26 U.S.C.
    /// § 152, which turns on relationship, residency, age and support tests
    /// that are facts about a household rather than figures.
    DependentQualifies,

    /// Whether the filer was a resident of the state for the whole tax year,
    /// which decides both the form they file and how much of their income
    /// the state reaches.
    ///
    /// No jurisdiction's figures answer it. Residency turns on domicile and
    /// on days present, which are facts about a person rather than about
    /// their income, and the state calculators in this module compute a
    /// full-year resident's tax and say so.
    Residency,

    /// Whether a capital asset's gain is allocated to a state at all, which
    /// for intangible property turns on where the filer was domiciled when
    /// the sale settled rather than on where the asset was.
    ///
    /// Washington's capital gains excise tax under RCW 82.87.100 is the case
    /// this exists for.
    CapitalAssetSitus,

    /// Whether the filer asks for the estimated tax penalty to be waived: for
    /// a casualty, disaster or other unusual circumstance, or for retirement
    /// after 62 or disability with reasonable cause. 26 U.S.C. § 6654(e)(3)
    /// and its state counterparts leave the request to the filer and the
    /// decision to the tax authority, so a calculator reports the penalty
    /// before any waiver.
    PenaltyWaiver,

    /// What last year's return showed: whether one was filed, whether it
    /// covered twelve months, and its tax. The estimated tax safe harbours
    /// and the no-liability exception read it, and a calculator not given it
    /// cannot offer them.
    PriorYearReturn,

    /// A year's adjusted gross income that a threshold tests and the caller
    /// did not supply, such as the § 6654(d)(1)(C) step to 110% of last
    /// year's tax.
    AdjustedGrossIncome,

    /// Not a choice. It reports that a figure depends on an interest rate
    /// the authority has not yet published, so the figure stops at the last
    /// day a published rate covers. The caller cannot answer it; a later
    /// build of this library that carries the rate does.
    UnpublishedRate,

    /// When the filer could first expect to have to make estimated payments
    /// for the year. Several states give a filer who first meets the
    /// requirement after a due date fewer installments, or excuse the earlier
    /// ones, and a calculator not told assumes the requirement was met from
    /// the start of the year.
    RequirementFirstMet,

    /// A fact only one jurisdiction's rules read, carried in that
    /// jurisdiction's own extras, that the caller did not supply.
    /// [`Required::because`] names the fact and the default used.
    JurisdictionFact,

    /// This year's tax, which the estimated tax rules measure one option for
    /// the target against, and which the caller did not give. The rules then
    /// use the options that do not need it, and say so.
    ThisYearTax,

    /// Not a choice the caller can make. It reports that the jurisdiction's
    /// own documents leave a rule open, or disagree, and nothing the caller
    /// could supply settles it. [`Required::because`] names the readings and
    /// the one the figure uses. Like [`PositionKind::UnpublishedRate`], it is
    /// answered by a later build of this library, once the jurisdiction says
    /// which reading holds.
    UnsettledRule,
}

/// Names the position as the rule that creates it does. A caller rendering
/// an unanswered position needs a word rather than an ordinal, and without
/// this every one of them prints as its integer.
impl fmt::Display for PositionKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(match self {
            PositionKind::DeductionMethod => "deduction method",
            PositionKind::LotSelection => "lot selection",
            PositionKind::IraDeductibility => "IRA deductibility",
            PositionKind::SubstantiallyIdentical => "substantially identical security",
            PositionKind::TradeOrBusiness => "trade or business",
            PositionKind::DependentQualifies => "dependent qualifies",
            PositionKind::Residency => "residency",
            PositionKind::CapitalAssetSitus => "capital asset situs",
            PositionKind::PenaltyWaiver => "penalty waiver",
            PositionKind::PriorYearReturn => "prior year return",
            PositionKind::AdjustedGrossIncome => "adjusted gross income",
            PositionKind::UnpublishedRate => "unpublished rate",
            PositionKind::RequirementFirstMet => "requirement first met",
            PositionKind::JurisdictionFact => "jurisdiction fact",
            PositionKind::ThisYearTax => "this year's tax",
            PositionKind::UnsettledRule => "unsettled rule",
            PositionKind::Unspecified => "unspecified position",
        })
    }
}

/// `s` quoted as Go's `strconv.Quote` and the `%q` verb quote it: in double
/// quotes, with `"` and `\` escaped, the usual escapes for control
/// characters, and `\u` or `\U` for any other character Go does not print,
/// such as a space other than U+0020.
pub fn quote(s: &str) -> String {
    let mut out = String::with_capacity(s.len() + 2);
    out.push('"');
    for c in s.chars() {
        match c {
            '"' => out.push_str("\\\""),
            '\\' => out.push_str("\\\\"),
            '\u{7}' => out.push_str("\\a"),
            '\u{8}' => out.push_str("\\b"),
            '\u{c}' => out.push_str("\\f"),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            '\u{b}' => out.push_str("\\v"),
            c if is_go_print(c) => out.push(c),
            c if (c as u32) < 0x20 || c == '\u{7f}' => {
                out.push_str(&format!("\\x{:02x}", c as u32));
            }
            c if (c as u32) < 0x10000 => out.push_str(&format!("\\u{:04x}", c as u32)),
            c => out.push_str(&format!("\\U{:08x}", c as u32)),
        }
    }
    out.push('"');
    out
}

/// Whether Go's `strconv.IsPrint` holds for `c`: letters, marks, numbers,
/// punctuation, symbols and the ASCII space. The standard library has no
/// Unicode category tables, so this refuses the controls, every space but
/// U+0020, the format characters prose meets, and private use.
fn is_go_print(c: char) -> bool {
    if c == ' ' {
        return true;
    }
    if c.is_control() || c.is_whitespace() {
        return false;
    }
    !matches!(c as u32,
        0x00AD | 0x0600..=0x0605 | 0x061C | 0x06DD | 0x070F | 0x180E
        | 0x200B..=0x200F | 0x202A..=0x202E | 0x2060..=0x2064 | 0x2066..=0x206F
        | 0xFEFF | 0xFFF9..=0xFFFB | 0xE000..=0xF8FF | 0xF0000..=0x10FFFF)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn quotes_as_go_does() {
        assert_eq!(quote("a \"b\" \\ c"), r#""a \"b\" \\ c""#);
        assert_eq!(quote("tab\there\n"), r#""tab\there\n""#);
        assert_eq!(quote("−$1 § 2 ’"), "\"−$1 § 2 ’\"");
        assert_eq!(quote("a\u{a0}b\u{1}"), "\"a\\u00a0b\\x01\"");
    }
}
