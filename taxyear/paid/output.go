package paid

import (
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
	usv1 "github.com/nexteggapp/taxcalcs/us/v1"
)

// requirementOf maps the requirement and its options.
func requirementOf(r estimated.Requirement) *usv1.EstimatedRequirement {
	chosen, hasChosen := r.Chosen()
	out := &usv1.EstimatedRequirement{Required: r.Required(), Why: r.Why(), Exempt: r.Exempt(), TargetKnown: r.TargetKnown()}
	if hasChosen {
		out.TargetCents = int64(r.Target())
	}
	for _, o := range r.Options() {
		opt := &usv1.EstimatedTargetOption{Basis: basisOf(o.Basis()), PercentBasisPoints: int32(o.Percent()),
			Available: o.Available(), WhyNot: o.WhyNot(), Chosen: hasChosen && o.Basis() == chosen.Basis(), Label: o.Label()}
		if o.Available() {
			opt.AmountCents = int64(o.Amount())
		}
		out.Options = append(out.Options, opt)
	}
	return out
}

func basisOf(b estimated.Basis) usv1.TargetBasis {
	switch b {
	case estimated.BasisThisYearTax:
		return usv1.TargetBasis_TARGET_BASIS_THIS_YEAR_TAX
	case estimated.BasisPriorYearTax:
		return usv1.TargetBasis_TARGET_BASIS_LAST_YEAR_TAX
	case estimated.BasisPriorYearIncome:
		return usv1.TargetBasis_TARGET_BASIS_LAST_YEAR_INCOME
	case estimated.BasisException:
		return usv1.TargetBasis_TARGET_BASIS_EXCEPTION
	}
	return usv1.TargetBasis_TARGET_BASIS_UNSPECIFIED
}

// toMeet is the smallest payment on each due date after today that brings
// what is credited up to what is needed by then, counting what earlier
// payments here add. An excused installment needs nothing.
func toMeet(installments []estimated.Installment, today estimated.Date) []*usv1.DuePayment {
	var out []*usv1.DuePayment
	var added money.USCents
	for _, i := range installments {
		if !i.Due().After(today) || i.Status() == estimated.StatusExcused {
			continue
		}
		extra := i.NeededBy() - i.CreditedBy() - added
		if extra <= 0 {
			continue
		}
		added += extra
		out = append(out, &usv1.DuePayment{On: wire(i.Due()), AmountCents: int64(extra)})
	}
	return out
}

// installmentOf maps one installment of the schedule over every payment.
// made is the same installment over the payments made by today.
// installmentOf is one due date: what is needed by it, and what the
// payments made credit to it, and its shortfall, as the rules measure
// them. Payments planned or suggested are not paid: they count only in
// the status, as Planned, and in the penalty with the planned payments.
// planned reports a payment planned on its date.
func installmentOf(i, made estimated.Installment, today estimated.Date, planned bool) *usv1.EstimatedInstallment {
	return &usv1.EstimatedInstallment{Number: int32(i.Number()), Due: wire(i.Due()), NeededByCents: int64(i.NeededBy()),
		CreditedByCents: int64(made.CreditedBy()), ShortfallCents: int64(made.Shortfall()), Status: statusOf(i, made, today, planned)}
}

// statusOf is the one status rule for every jurisdiction: nothing due; met by
// the payments made by today; excused; short once the date has passed; else
// planned, planned but short when a payment planned on the date doesn't
// meet it, or not planned.
func statusOf(all, made estimated.Installment, today estimated.Date, planned bool) usv1.InstallmentStatus {
	switch {
	case all.Status() == estimated.StatusNothingDue:
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_NOTHING_DUE
	case made.CreditedBy() >= all.NeededBy():
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_MET
	case all.Status() == estimated.StatusExcused:
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_EXCUSED
	case !all.Due().After(today):
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_SHORT
	case all.Status() == estimated.StatusMet:
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_PLANNED
	case planned:
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_PLANNED_SHORT
	}
	return usv1.InstallmentStatus_INSTALLMENT_STATUS_NOT_PLANNED
}

func penaltyOf(p estimated.Penalty) *usv1.EstimatedPenalty {
	out := &usv1.EstimatedPenalty{TotalCents: int64(p.Total()), AsOf: wire(p.AsOf()), UpperBound: p.UpperBound(), Reason: p.Reason(),
		RatedThrough: wire(p.RatedThrough()), Summary: p.Summary(), ShortDueDates: int32(p.ShortDueDates()), IfWaivedCents: int64(p.IfWaived())}
	if o, ok := p.WaiverOutcome(); ok {
		out.WaiverOutcomeReason = o.Reason()
		switch {
		case o.Eligible():
			out.WaiverOutcome = usv1.WaiverOutcome_WAIVER_OUTCOME_ELIGIBLE
		case o.Refused():
			out.WaiverOutcome = usv1.WaiverOutcome_WAIVER_OUTCOME_REFUSED
		default:
			out.WaiverOutcome = usv1.WaiverOutcome_WAIVER_OUTCOME_UNDECIDED
		}
	}
	for _, l := range p.Lines() {
		line := &usv1.EstimatedPenaltyLine{Installment: int32(l.Installment()), Due: wire(l.Due()), ShortfallCents: int64(l.Shortfall()),
			Days: int32(l.Days()), TotalCents: int64(l.Total())}
		for _, c := range l.Parts() {
			line.Charges = append(line.Charges, &usv1.EstimatedPenaltyCharge{Kind: chargeKindOf(c.Kind()), PrincipalCents: int64(c.Principal()),
				From: wire(c.From()), To: wire(c.To()), Days: int32(c.Days()), Months: int32(c.Months()),
				RateBasisPoints: int32(c.Rate()), ChargeCents: int64(c.Charge()), Source: c.Source()})
		}
		out.Lines = append(out.Lines, line)
	}
	return out
}

func chargeKindOf(k estimated.PartKind) usv1.PenaltyChargeKind {
	switch k {
	case estimated.PartInterest:
		return usv1.PenaltyChargeKind_PENALTY_CHARGE_KIND_INTEREST
	case estimated.PartFlat:
		return usv1.PenaltyChargeKind_PENALTY_CHARGE_KIND_FLAT
	case estimated.PartMonthly:
		return usv1.PenaltyChargeKind_PENALTY_CHARGE_KIND_MONTHLY
	}
	return usv1.PenaltyChargeKind_PENALTY_CHARGE_KIND_UNSPECIFIED
}

func explanationOf(e estimated.Explanation) *usv1.EstimatedExplanation {
	return &usv1.EstimatedExplanation{Summary: e.Summary(), Paragraphs: e.Body(), VersusFederal: e.VersusFederal(), Sources: e.Sources()}
}
