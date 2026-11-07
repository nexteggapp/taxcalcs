//! The estimated-payment rules' answers as the us/v1 messages carry them,
//! ported from Go's `taxyear/paid/output.go`.

use estimated::{Basis, Date, Explanation, Installment, PartKind, Penalty, Requirement, Status};
use money::USCents;
use proto::usv1::{
    DuePayment, EstimatedExplanation, EstimatedInstallment, EstimatedPenalty,
    EstimatedPenaltyCharge, EstimatedPenaltyLine, EstimatedRequirement, EstimatedTargetOption,
    InstallmentStatus, PenaltyChargeKind, TargetBasis, WaiverOutcome,
};

use super::wire;

/// Maps the requirement and its options.
pub(super) fn requirement_of(r: &Requirement) -> EstimatedRequirement {
    let chosen = r.chosen();
    let mut out = EstimatedRequirement {
        required: r.required(),
        why: r.why(),
        exempt: r.exempt(),
        target_known: r.target_known(),
        ..Default::default()
    };
    if chosen.is_some() {
        out.target_cents = r.target().0;
    }
    for o in r.options() {
        let mut opt = EstimatedTargetOption {
            basis: basis_of(o.basis()).into(),
            percent_basis_points: o.percent().0,
            available: o.available(),
            why_not: o.why_not(),
            chosen: chosen.as_ref().is_some_and(|c| o.basis() == c.basis()),
            label: o.label(),
            ..Default::default()
        };
        if o.available() {
            opt.amount_cents = o.amount().0;
        }
        out.options.push(opt);
    }
    out
}

fn basis_of(b: Basis) -> TargetBasis {
    match b {
        Basis::ThisYearTax => TargetBasis::TARGET_BASIS_THIS_YEAR_TAX,
        Basis::PriorYearTax => TargetBasis::TARGET_BASIS_LAST_YEAR_TAX,
        Basis::PriorYearIncome => TargetBasis::TARGET_BASIS_LAST_YEAR_INCOME,
        Basis::Exception => TargetBasis::TARGET_BASIS_EXCEPTION,
        _ => TargetBasis::TARGET_BASIS_UNSPECIFIED,
    }
}

/// The smallest payment on each due date after today that brings what is
/// credited up to what is needed by then, counting what earlier payments
/// here add. An excused installment needs nothing.
pub(super) fn to_meet(installments: &[Installment], today: Date) -> Vec<DuePayment> {
    let mut out = Vec::new();
    let mut added = USCents(0);
    for i in installments {
        if !i.due().after(today) || i.status() == Status::Excused {
            continue;
        }
        let extra = i.needed_by() - i.credited_by() - added;
        if extra.0 <= 0 {
            continue;
        }
        added += extra;
        out.push(DuePayment {
            on: wire(i.due()),
            amount_cents: extra.0,
            ..Default::default()
        });
    }
    out
}

/// One due date: what is needed by it, and what the payments made credit to
/// it, and its shortfall, as the rules measure them. `i` is the installment
/// of the schedule over every payment; `made` is the same installment over
/// the payments made by today. Payments planned or suggested are not paid:
/// they count only in the status, as Planned, and in the penalty with the
/// planned payments. `planned` reports a payment planned on its date.
pub(super) fn installment_of(
    i: &Installment,
    made: &Installment,
    today: Date,
    planned: bool,
) -> EstimatedInstallment {
    EstimatedInstallment {
        number: i.number() as i32,
        due: wire(i.due()),
        needed_by_cents: i.needed_by().0,
        credited_by_cents: made.credited_by().0,
        shortfall_cents: made.shortfall().0,
        status: status_of(i, made, today, planned).into(),
        ..Default::default()
    }
}

/// The one status rule for every jurisdiction: nothing due; met by the
/// payments made by today; excused; short once the date has passed; else
/// planned, planned but short when a payment planned on the date doesn't
/// meet it, or not planned.
pub(super) fn status_of(
    all: &Installment,
    made: &Installment,
    today: Date,
    planned: bool,
) -> InstallmentStatus {
    if all.status() == Status::NothingDue {
        return InstallmentStatus::INSTALLMENT_STATUS_NOTHING_DUE;
    }
    if made.credited_by() >= all.needed_by() {
        return InstallmentStatus::INSTALLMENT_STATUS_MET;
    }
    if all.status() == Status::Excused {
        return InstallmentStatus::INSTALLMENT_STATUS_EXCUSED;
    }
    if !all.due().after(today) {
        return InstallmentStatus::INSTALLMENT_STATUS_SHORT;
    }
    if all.status() == Status::Met {
        return InstallmentStatus::INSTALLMENT_STATUS_PLANNED;
    }
    if planned {
        return InstallmentStatus::INSTALLMENT_STATUS_PLANNED_SHORT;
    }
    InstallmentStatus::INSTALLMENT_STATUS_NOT_PLANNED
}

pub(super) fn penalty_of(p: &Penalty) -> EstimatedPenalty {
    let mut out = EstimatedPenalty {
        total_cents: p.total().0,
        as_of: wire(p.as_of()),
        upper_bound: p.upper_bound(),
        reason: p.reason(),
        rated_through: wire(p.rated_through()),
        summary: p.summary(),
        short_due_dates: p.short_due_dates() as i32,
        if_waived_cents: p.if_waived().0,
        ..Default::default()
    };
    if let Some(o) = p.waiver_outcome() {
        out.waiver_outcome_reason = o.reason();
        out.waiver_outcome = if o.eligible() {
            WaiverOutcome::WAIVER_OUTCOME_ELIGIBLE
        } else if o.refused() {
            WaiverOutcome::WAIVER_OUTCOME_REFUSED
        } else {
            WaiverOutcome::WAIVER_OUTCOME_UNDECIDED
        }
        .into();
    }
    for l in p.lines() {
        let mut line = EstimatedPenaltyLine {
            installment: l.installment() as i32,
            due: wire(l.due()),
            shortfall_cents: l.shortfall().0,
            days: l.days() as i32,
            total_cents: l.total().0,
            ..Default::default()
        };
        for c in l.parts() {
            line.charges.push(EstimatedPenaltyCharge {
                kind: charge_kind_of(c.kind()).into(),
                principal_cents: c.principal().0,
                from: wire(c.from()),
                to: wire(c.to()),
                days: c.days() as i32,
                months: c.months() as i32,
                rate_basis_points: c.rate().0,
                charge_cents: c.charge().0,
                source: c.source(),
                ..Default::default()
            });
        }
        out.lines.push(line);
    }
    out
}

fn charge_kind_of(k: PartKind) -> PenaltyChargeKind {
    match k {
        PartKind::Interest => PenaltyChargeKind::PENALTY_CHARGE_KIND_INTEREST,
        PartKind::Flat => PenaltyChargeKind::PENALTY_CHARGE_KIND_FLAT,
        PartKind::Monthly => PenaltyChargeKind::PENALTY_CHARGE_KIND_MONTHLY,
        PartKind::Unspecified => PenaltyChargeKind::PENALTY_CHARGE_KIND_UNSPECIFIED,
    }
}

pub(super) fn explanation_of(e: &Explanation) -> EstimatedExplanation {
    EstimatedExplanation {
        summary: e.summary(),
        paragraphs: e.body(),
        versus_federal: e.versus_federal(),
        sources: e.sources(),
        ..Default::default()
    }
}
