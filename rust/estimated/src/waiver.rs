//! What the rules make of a waiver asked for.

use crate::output::PartKind;
use crate::{Date, Error, Facts, WaiverCause, WaiverKind, invalid};

#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Hash)]
enum WaiverState {
    #[default]
    Unset,
    Eligible,
    Refused,
    Undecided,
}

/// What a jurisdiction's rules make of a waiver asked for: whether the facts
/// given meet its conditions, and why. The tax authority decides; an
/// eligible waiver is one it may grant, so the penalty is still figured in
/// full and [`crate::Penalty::if_waived`] shows what would remain.
#[derive(Clone, Debug, Default, PartialEq, Eq, Hash)]
pub struct WaiverOutcome {
    state: WaiverState,
    reason: String,
    kept: Vec<PartKind>,
}

/// A waiver whose conditions the facts meet.
pub fn waiver_eligible(reason: impl Into<String>) -> WaiverOutcome {
    WaiverOutcome {
        state: WaiverState::Eligible,
        reason: reason.into(),
        kept: Vec::new(),
    }
}

/// A waiver whose conditions the facts meet, which covers every charge but
/// those of the kinds kept: Washington's waiver lifts the late-payment
/// penalty but not the interest, so [`PartKind::Interest`] is kept.
pub fn waiver_eligible_except(reason: impl Into<String>, kept: &[PartKind]) -> WaiverOutcome {
    WaiverOutcome {
        state: WaiverState::Eligible,
        reason: reason.into(),
        kept: kept.to_vec(),
    }
}

/// A waiver whose conditions the facts don't meet, or that the jurisdiction
/// doesn't grant.
pub fn waiver_refused(reason: impl Into<String>) -> WaiverOutcome {
    WaiverOutcome {
        state: WaiverState::Refused,
        reason: reason.into(),
        kept: Vec::new(),
    }
}

/// A waiver that needs a fact not given, such as the date of retirement.
pub fn waiver_undecided(reason: impl Into<String>) -> WaiverOutcome {
    WaiverOutcome {
        state: WaiverState::Undecided,
        reason: reason.into(),
        kept: Vec::new(),
    }
}

impl WaiverOutcome {
    /// The kinds of charge an eligible waiver leaves due.
    pub fn kept(&self) -> Vec<PartKind> {
        self.kept.clone()
    }

    pub(crate) fn kept_ref(&self) -> &[PartKind] {
        &self.kept
    }

    pub(crate) fn is_set(&self) -> bool {
        self.state != WaiverState::Unset
    }

    /// Whether the facts meet the waiver's conditions.
    pub fn eligible(&self) -> bool {
        self.state == WaiverState::Eligible
    }

    /// Whether the waiver is refused.
    pub fn refused(&self) -> bool {
        self.state == WaiverState::Refused
    }

    /// Whether the waiver needs a fact not given.
    pub fn undecided(&self) -> bool {
        self.state == WaiverState::Undecided
    }

    /// Says why in plain words, in one or two sentences.
    pub fn reason(&self) -> String {
        self.reason.clone()
    }

    pub(crate) fn check(&self) -> Result<(), Error> {
        if self.state == WaiverState::Unset {
            return Err(invalid!(
                "estimated: a waiver outcome must be eligible, refused or undecided"
            ));
        }
        if self.reason.is_empty() {
            return Err(invalid!("estimated: a waiver outcome must say why"));
        }
        let body = self.reason.strip_suffix('.').unwrap_or(&self.reason);
        if body.matches(". ").count() > 1 {
            return Err(invalid!(
                "estimated: a waiver outcome's reason is at most two sentences, not {}",
                working::quote(&self.reason)
            ));
        }
        Ok(())
    }
}

/// The age on `d` of someone born on `born`.
fn years_old(born: Date, d: Date) -> i64 {
    let mut age = d.year - born.year;
    if d.month < born.month || (d.month == born.month && d.day < born.day) {
        age -= 1;
    }
    age
}

/// Decides the two waivers federal law and many states grant, with the
/// conditions as 26 U.S.C. § 6654(e)(3) and the Form 2210 instructions
/// state them:
///   - a casualty, disaster or other unusual circumstance that makes the
///     penalty inequitable, which only the authority can judge;
///   - retirement after reaching `min_age`, or disability, in the tax year
///     or the year before, with reasonable cause, which only the authority
///     can judge.
///
/// `authority` names who decides, as "the IRS". The reason says whether the
/// waiver can apply, what it needs, and who decides. It returns `None` when
/// no waiver was asked for.
pub fn standard_waiver(f: &Facts, authority: &str, min_age: i64) -> Option<WaiverOutcome> {
    let p = f.positions();
    let kind = p.waiver()?;
    if kind == WaiverKind::None {
        return None;
    }
    let year = f.tax_year();
    let window = format!("{} or {}", year - 1, year);
    if kind == WaiverKind::Casualty {
        return Some(waiver_eligible(format!(
            "{} can waive all or part of the penalty. It must be for {} that made it unfair.",
            capital(authority),
            cause_words(p.waiver_cause())
        )));
    }
    let on = p.waiver_on();
    Some(match p.waiver_cause() {
        WaiverCause::Disability => {
            if on.is_zero() {
                waiver_undecided(format!(
                    "The disability waiver needs the date you became disabled, in {window}."
                ))
            } else if on.year != year && on.year != year - 1 {
                waiver_refused(format!(
                    "You became disabled in {}; the waiver needs it to be in {window}.",
                    on.year
                ))
            } else {
                waiver_eligible(format!(
                    "You became disabled in {}, so the penalty can be waived. {} needs reasonable cause for the shortfall.",
                    on.year,
                    capital(authority)
                ))
            }
        }
        _ => {
            if on.is_zero() {
                return Some(waiver_undecided(format!(
                    "The retirement waiver needs the date you retired, in {window}."
                )));
            }
            if on.year != year && on.year != year - 1 {
                return Some(waiver_refused(format!(
                    "You retired in {}, and the waiver covers retiring in {window}.",
                    on.year
                )));
            }
            let born = p.born();
            if born.is_zero() {
                return Some(waiver_undecided(format!(
                    "The retirement waiver needs your date of birth, to show you'd reached {min_age}."
                )));
            }
            let age = years_old(born, on);
            if age < min_age {
                return Some(waiver_refused(format!(
                    "You were {age} when you retired, and the waiver needs you to have reached {min_age}."
                )));
            }
            waiver_eligible(format!(
                "You retired in {} after reaching {min_age}, so the penalty can be waived. {} needs reasonable cause for the shortfall.",
                on.year,
                capital(authority)
            ))
        }
    })
}

fn cause_words(c: WaiverCause) -> &'static str {
    match c {
        WaiverCause::Casualty => "a casualty",
        WaiverCause::Disaster => "a disaster",
        _ => "a casualty, disaster or other unusual event",
    }
}

/// `s` with its first byte upper-cased, as Go's `strings.ToUpper(s[:1]) +
/// s[1:]` does for text that starts with an ASCII letter.
fn capital(s: &str) -> String {
    let mut c = s.chars();
    match c.next() {
        None => String::new(),
        Some(first) if first.is_ascii() => {
            format!("{}{}", first.to_ascii_uppercase(), c.as_str())
        }
        Some(_) => s.to_string(),
    }
}
