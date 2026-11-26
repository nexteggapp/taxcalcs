//! The estimated-payment rules' answers as the us/v1 messages carry them,
//! ported from Go's `taxyear/paid/output.go`.

use std::collections::HashMap;
use std::sync::LazyLock;

use buffa::{Inline, MessageField};
use money::USCents;
use proto::usv1::{
    CalendarDate, DuePayment, EstimatedExplanation, EstimatedInstallment, EstimatedPenalty,
    EstimatedPenaltyLine, EstimatedRequirement, EstimatedTargetOption, InstallmentStatus,
    TargetBasis, WaiverOutcome,
};
use proto::{commonv1, returnsv1};
use regex::Regex;

use crate::internal::civil;
use crate::model;

/// A date field of a returns/v1 message.
pub(super) type DateField = MessageField<commonv1::Date, Inline<commonv1::Date>>;

/// A proto date as a civil one, or the zero Date.
pub(super) fn date(d: &DateField) -> civil::Date {
    civil::from_field(d).unwrap_or_default()
}

fn wire(d: &DateField) -> MessageField<CalendarDate, Inline<CalendarDate>> {
    match d.as_option() {
        Some(d) if d.year != 0 => MessageField::some(CalendarDate {
            year: d.year,
            month: d.month,
            day: d.day,
            ..Default::default()
        }),
        _ => MessageField::none(),
    }
}

fn cents(m: &MessageField<commonv1::Money, Inline<commonv1::Money>>) -> i64 {
    model::value(m).0
}

/// Maps the requirement and its options.
pub(super) fn requirement_of(
    r: &MessageField<returnsv1::EstimatedRequirement, Inline<returnsv1::EstimatedRequirement>>,
) -> EstimatedRequirement {
    use returnsv1::{OptionStatus, RequirementStatus};
    let status = r.status.as_known();
    let mut out = EstimatedRequirement {
        required: status == Some(RequirementStatus::REQUIREMENT_STATUS_REQUIRED),
        exempt: status == Some(RequirementStatus::REQUIREMENT_STATUS_EXEMPT),
        target_known: r.status.to_i32() != RequirementStatus::REQUIREMENT_STATUS_UNKNOWN as i32
            && r.status.to_i32() != RequirementStatus::REQUIREMENT_STATUS_UNSPECIFIED as i32,
        why: r.why.clone(),
        ..Default::default()
    };
    for o in &r.options {
        let (basis, percent) = option_basis(o);
        let available = o.status.to_i32() != OptionStatus::OPTION_STATUS_UNAVAILABLE as i32;
        let mut opt = EstimatedTargetOption {
            basis: basis.into(),
            percent_basis_points: percent,
            available,
            why_not: o.why_not.clone(),
            chosen: o.status.as_known() == Some(OptionStatus::OPTION_STATUS_CHOSEN),
            label: o.label.clone(),
            ..Default::default()
        };
        if available {
            opt.amount_cents = cents(&o.amount);
        }
        if opt.chosen {
            out.target_cents = cents(&r.target);
        }
        out.options.push(opt);
    }
    out
}

/// An option's id: this-year-<percent>, last-year-<percent>,
/// last-year-income or exception-<words>.
static OPTION_ID: LazyLock<Option<Regex>> =
    LazyLock::new(|| Regex::new(r"^(this-year|last-year)-([0-9]+)(?:\.([0-9]{2}))?$").ok());

/// Go's strconv.ParseInt(s, 10, 32), its error ignored: out of range is the
/// nearest int32.
fn parse_i32(s: &str) -> i64 {
    match s.parse::<i64>() {
        Ok(v) => v.clamp(i64::from(i32::MIN), i64::from(i32::MAX)),
        Err(_) if !s.is_empty() && s.bytes().all(|b| b.is_ascii_digit()) => i64::from(i32::MAX),
        Err(_) => 0,
    }
}

/// The basis and percent an option's id names, the percent in basis
/// points: last year's income is 100%, and an exception none.
fn option_basis(o: &returnsv1::EstimatedTargetOption) -> (TargetBasis, i32) {
    let id = o.id.as_str();
    if o.basis.as_known() == Some(returnsv1::TargetBasis::TARGET_BASIS_EXCEPTION)
        || id.starts_with("exception-")
    {
        return (TargetBasis::TARGET_BASIS_EXCEPTION, 0);
    }
    if id == "last-year-income" {
        return (TargetBasis::TARGET_BASIS_LAST_YEAR_INCOME, 10_000);
    }
    let basis = match o.basis.as_known() {
        Some(returnsv1::TargetBasis::TARGET_BASIS_THIS_YEAR) => {
            TargetBasis::TARGET_BASIS_THIS_YEAR_TAX
        }
        Some(returnsv1::TargetBasis::TARGET_BASIS_LAST_YEAR) => {
            TargetBasis::TARGET_BASIS_LAST_YEAR_TAX
        }
        _ => TargetBasis::TARGET_BASIS_UNSPECIFIED,
    };
    let Some(m) = OPTION_ID.as_ref().and_then(|re| re.captures(id)) else {
        return (basis, 0);
    };
    let whole = parse_i32(m.get(2).map_or("", |g| g.as_str()));
    let frac = parse_i32(&format!("0{}", m.get(3).map_or("", |g| g.as_str())));
    (basis, (whole * 100 + frac) as i32)
}

/// The smallest payment on each due date after today that brings what is
/// credited up to what is needed by then, counting what earlier payments
/// here add. An excused installment needs nothing.
pub(super) fn to_meet(
    installments: &[returnsv1::EstimatedInstallment],
    today: civil::Date,
) -> Vec<DuePayment> {
    let mut out = Vec::new();
    let mut added = USCents(0);
    for i in installments {
        if !date(&i.due).after(today)
            || i.status.as_known() == Some(returnsv1::InstallmentStatus::INSTALLMENT_STATUS_EXCUSED)
        {
            continue;
        }
        let extra = model::value(&i.needed_by) - model::value(&i.credited_by) - added;
        if extra.0 <= 0 {
            continue;
        }
        added += extra;
        out.push(DuePayment {
            on: wire(&i.due),
            amount_cents: extra.0,
            ..Default::default()
        });
    }
    out
}

/// One due date: what is needed by it, and what the payments made credit to
/// it, and its shortfall, as the rules measure them. `i` is the installment
/// over every payment, `made` the same installment over the payments made
/// by today. Payments planned or suggested are not paid: they count only in
/// the status, as Planned, and in the penalty with the planned payments.
/// `planned` reports a payment planned on its date.
pub(super) fn installment_of(
    i: &returnsv1::EstimatedInstallment,
    made: &returnsv1::EstimatedInstallment,
    today: civil::Date,
    planned: bool,
) -> EstimatedInstallment {
    EstimatedInstallment {
        number: i.number,
        due: wire(&i.due),
        needed_by_cents: cents(&i.needed_by),
        credited_by_cents: cents(&made.credited_by),
        shortfall_cents: cents(&made.shortfall),
        status: status_of(i, made, today, planned).into(),
        ..Default::default()
    }
}

/// The one status rule for every jurisdiction: nothing due; met by the
/// payments made by today; excused; short once the date has passed; else
/// planned, planned but short when a payment planned on the date doesn't
/// meet it, or not planned. The rules are asked after every due date, so
/// their status is met, short, excused or nothing due.
fn status_of(
    all: &returnsv1::EstimatedInstallment,
    made: &returnsv1::EstimatedInstallment,
    today: civil::Date,
    planned: bool,
) -> InstallmentStatus {
    use returnsv1::InstallmentStatus as R;
    let s = all.status.as_known();
    if s == Some(R::INSTALLMENT_STATUS_NOTHING_DUE) {
        InstallmentStatus::INSTALLMENT_STATUS_NOTHING_DUE
    } else if model::value(&made.credited_by) >= model::value(&all.needed_by) {
        InstallmentStatus::INSTALLMENT_STATUS_MET
    } else if s == Some(R::INSTALLMENT_STATUS_EXCUSED) {
        InstallmentStatus::INSTALLMENT_STATUS_EXCUSED
    } else if !date(&all.due).after(today) {
        InstallmentStatus::INSTALLMENT_STATUS_SHORT
    } else if s == Some(R::INSTALLMENT_STATUS_MET) {
        InstallmentStatus::INSTALLMENT_STATUS_PLANNED
    } else if planned {
        InstallmentStatus::INSTALLMENT_STATUS_PLANNED_SHORT
    } else {
        InstallmentStatus::INSTALLMENT_STATUS_NOT_PLANNED
    }
}

/// Maps a penalty figured from schedule. Upper bound is a penalty figured
/// against last year's tax alone while this year's isn't known, which can
/// only be too high, unless an exception excuses the year; the rated-through
/// date is set only while the penalty stops at the last published rate.
/// Each line's days run from its due date to the day it was paid. The
/// rules' charges and their reason are their own form's to show, and aren't
/// given.
pub(super) fn penalty_of(
    p: &MessageField<returnsv1::EstimatedPenalty, Inline<returnsv1::EstimatedPenalty>>,
    schedule: &[returnsv1::EstimatedInstallment],
    tax_known: bool,
    exempt: bool,
) -> Option<EstimatedPenalty> {
    let p = p.as_option()?;
    let mut out = EstimatedPenalty {
        total_cents: cents(&p.total),
        as_of: wire(&p.figured_to),
        upper_bound: !tax_known && !exempt && !p.upper_bound,
        summary: p.summary.clone(),
        if_waived_cents: cents(&p.if_waived),
        waiver_outcome_reason: p.waiver_outcome_reason.clone(),
        ..Default::default()
    };
    if p.upper_bound {
        out.rated_through = wire(&p.rated_through);
    }
    out.waiver_outcome = match p.waiver_outcome.as_known() {
        Some(returnsv1::WaiverOutcome::WAIVER_OUTCOME_ELIGIBLE) => {
            WaiverOutcome::WAIVER_OUTCOME_ELIGIBLE
        }
        Some(returnsv1::WaiverOutcome::WAIVER_OUTCOME_REFUSED) => {
            WaiverOutcome::WAIVER_OUTCOME_REFUSED
        }
        Some(returnsv1::WaiverOutcome::WAIVER_OUTCOME_UNDECIDED) => {
            WaiverOutcome::WAIVER_OUTCOME_UNDECIDED
        }
        _ => WaiverOutcome::WAIVER_OUTCOME_UNSPECIFIED,
    }
    .into();
    let mut due: HashMap<i32, &DateField> = HashMap::new();
    for i in schedule {
        due.insert(i.number, &i.due);
        if i.status.as_known() == Some(returnsv1::InstallmentStatus::INSTALLMENT_STATUS_SHORT) {
            out.short_due_dates += 1;
        }
    }
    let none = MessageField::none();
    for l in &p.instalments {
        let d = due.get(&l.number).copied().unwrap_or(&none);
        let mut line = EstimatedPenaltyLine {
            installment: l.number,
            due: wire(d),
            shortfall_cents: cents(&l.shortfall),
            total_cents: cents(&l.penalty),
            ..Default::default()
        };
        let (d, paid) = (date(d), date(&l.paid_on));
        if !d.is_zero() && !paid.is_zero() {
            line.days = paid.days_from(d) as i32;
        }
        out.lines.push(line);
    }
    Some(out)
}

pub(super) fn explanation_of(
    e: &MessageField<returnsv1::EstimatedExplanation, Inline<returnsv1::EstimatedExplanation>>,
) -> EstimatedExplanation {
    EstimatedExplanation {
        summary: e.summary.clone(),
        paragraphs: e.paragraphs.clone(),
        versus_federal: e.versus_federal.clone(),
        sources: e.sources.clone(),
        ..Default::default()
    }
}
