package paid

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/nexteggapp/taxcalcs/money"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	usv1 "github.com/nexteggapp/taxcalcs/us/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

var errMissingDate = errors.New("paid: a date is missing")

// date is a proto date as a civil one, or the zero Date.
func date(d *commonv1.Date) civil.Date {
	out, err := civil.FromProto(d)
	if err != nil {
		return civil.Date{}
	}
	return out
}

func wire(d *commonv1.Date) *usv1.CalendarDate {
	if d == nil || d.GetYear() == 0 {
		return nil
	}
	return &usv1.CalendarDate{Year: d.GetYear(), Month: d.GetMonth(), Day: d.GetDay()}
}

func cents(m *commonv1.Money) int64 { return int64(model.Value(m)) }

// requirementOf maps the requirement and its options.
func requirementOf(r *returnsv1.EstimatedRequirement) *usv1.EstimatedRequirement {
	status := r.GetStatus()
	out := &usv1.EstimatedRequirement{
		Required:    status == returnsv1.RequirementStatus_REQUIREMENT_STATUS_REQUIRED,
		Exempt:      status == returnsv1.RequirementStatus_REQUIREMENT_STATUS_EXEMPT,
		TargetKnown: status != returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN && status != returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNSPECIFIED,
		Why:         r.GetWhy(),
	}
	for _, o := range r.GetOptions() {
		basis, percent := optionBasis(o)
		available := o.GetStatus() != returnsv1.OptionStatus_OPTION_STATUS_UNAVAILABLE
		opt := &usv1.EstimatedTargetOption{Basis: basis, PercentBasisPoints: percent, Available: available, WhyNot: o.GetWhyNot(),
			Chosen: o.GetStatus() == returnsv1.OptionStatus_OPTION_STATUS_CHOSEN, Label: o.GetLabel()}
		if available {
			opt.AmountCents = cents(o.GetAmount())
		}
		if opt.Chosen {
			out.TargetCents = cents(r.GetTarget())
		}
		out.Options = append(out.Options, opt)
	}
	return out
}

// optionID is an option's id: this-year-<percent>, last-year-<percent>,
// last-year-income or exception-<words>.
var optionID = regexp.MustCompile(`^(this-year|last-year)-([0-9]+)(?:\.([0-9]{2}))?$`)

// optionBasis is the basis and percent an option's id names, the percent in
// basis points: last year's income is 100%, and an exception none.
func optionBasis(o *returnsv1.EstimatedTargetOption) (usv1.TargetBasis, int32) {
	id := o.GetId()
	switch {
	case o.GetBasis() == returnsv1.TargetBasis_TARGET_BASIS_EXCEPTION || strings.HasPrefix(id, "exception-"):
		return usv1.TargetBasis_TARGET_BASIS_EXCEPTION, 0
	case id == "last-year-income":
		return usv1.TargetBasis_TARGET_BASIS_LAST_YEAR_INCOME, 10_000
	}
	basis := usv1.TargetBasis_TARGET_BASIS_UNSPECIFIED
	switch o.GetBasis() {
	case returnsv1.TargetBasis_TARGET_BASIS_THIS_YEAR:
		basis = usv1.TargetBasis_TARGET_BASIS_THIS_YEAR_TAX
	case returnsv1.TargetBasis_TARGET_BASIS_LAST_YEAR:
		basis = usv1.TargetBasis_TARGET_BASIS_LAST_YEAR_TAX
	}
	m := optionID.FindStringSubmatch(id)
	if m == nil {
		return basis, 0
	}
	whole, _ := strconv.ParseInt(m[2], 10, 32)
	frac, _ := strconv.ParseInt("0"+m[3], 10, 32)
	return basis, int32(whole*100 + frac)
}

// toMeet is the smallest payment on each due date after today that brings
// what is credited up to what is needed by then, counting what earlier
// payments here add. An excused installment needs nothing.
func toMeet(installments []*returnsv1.EstimatedInstallment, today civil.Date) []*usv1.DuePayment {
	var out []*usv1.DuePayment
	var added money.USCents
	for _, i := range installments {
		if !date(i.GetDue()).After(today) || i.GetStatus() == returnsv1.InstallmentStatus_INSTALLMENT_STATUS_EXCUSED {
			continue
		}
		extra := model.Value(i.GetNeededBy()) - model.Value(i.GetCreditedBy()) - added
		if extra <= 0 {
			continue
		}
		added += extra
		out = append(out, &usv1.DuePayment{On: wire(i.GetDue()), AmountCents: int64(extra)})
	}
	return out
}

// installmentOf is one due date: what is needed by it, and what the
// payments made credit to it, and its shortfall, as the rules measure them.
// i is the installment over every payment, made the same installment over
// the payments made by today. Payments planned or suggested are not paid:
// they count only in the status, as Planned, and in the penalty with the
// planned payments. planned reports a payment planned on its date.
func installmentOf(i, made *returnsv1.EstimatedInstallment, today civil.Date, planned bool) *usv1.EstimatedInstallment {
	return &usv1.EstimatedInstallment{Number: i.GetNumber(), Due: wire(i.GetDue()), NeededByCents: cents(i.GetNeededBy()),
		CreditedByCents: cents(made.GetCreditedBy()), ShortfallCents: cents(made.GetShortfall()), Status: statusOf(i, made, today, planned)}
}

// statusOf is the one status rule for every jurisdiction: nothing due; met by
// the payments made by today; excused; short once the date has passed; else
// planned, planned but short when a payment planned on the date doesn't
// meet it, or not planned. The rules are asked after every due date, so
// their status is met, short, excused or nothing due.
func statusOf(all, made *returnsv1.EstimatedInstallment, today civil.Date, planned bool) usv1.InstallmentStatus {
	switch {
	case all.GetStatus() == returnsv1.InstallmentStatus_INSTALLMENT_STATUS_NOTHING_DUE:
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_NOTHING_DUE
	case model.Value(made.GetCreditedBy()) >= model.Value(all.GetNeededBy()):
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_MET
	case all.GetStatus() == returnsv1.InstallmentStatus_INSTALLMENT_STATUS_EXCUSED:
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_EXCUSED
	case !date(all.GetDue()).After(today):
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_SHORT
	case all.GetStatus() == returnsv1.InstallmentStatus_INSTALLMENT_STATUS_MET:
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_PLANNED
	case planned:
		return usv1.InstallmentStatus_INSTALLMENT_STATUS_PLANNED_SHORT
	}
	return usv1.InstallmentStatus_INSTALLMENT_STATUS_NOT_PLANNED
}

// penaltyOf maps a penalty figured from schedule. Upper bound is a penalty
// figured against last year's tax alone while this year's isn't known,
// which can only be too high, unless an exception excuses the year; the
// rated-through date is set only while the penalty
// stops at the last published rate. Each line's days run from its due date
// to the day it was paid. The rules' charges and their reason are their own
// form's to show, and aren't given.
func penaltyOf(p *returnsv1.EstimatedPenalty, schedule []*returnsv1.EstimatedInstallment, taxKnown, exempt bool) *usv1.EstimatedPenalty {
	if p == nil {
		return nil
	}
	out := &usv1.EstimatedPenalty{TotalCents: cents(p.GetTotal()), AsOf: wire(p.GetFiguredTo()), UpperBound: !taxKnown && !exempt && !p.GetUpperBound(),
		Summary: p.GetSummary(), IfWaivedCents: cents(p.GetIfWaived()), WaiverOutcomeReason: p.GetWaiverOutcomeReason()}
	if p.GetUpperBound() {
		out.RatedThrough = wire(p.GetRatedThrough())
	}
	switch p.GetWaiverOutcome() {
	case returnsv1.WaiverOutcome_WAIVER_OUTCOME_ELIGIBLE:
		out.WaiverOutcome = usv1.WaiverOutcome_WAIVER_OUTCOME_ELIGIBLE
	case returnsv1.WaiverOutcome_WAIVER_OUTCOME_REFUSED:
		out.WaiverOutcome = usv1.WaiverOutcome_WAIVER_OUTCOME_REFUSED
	case returnsv1.WaiverOutcome_WAIVER_OUTCOME_UNDECIDED:
		out.WaiverOutcome = usv1.WaiverOutcome_WAIVER_OUTCOME_UNDECIDED
	}
	due := map[int32]*commonv1.Date{}
	for _, i := range schedule {
		due[i.GetNumber()] = i.GetDue()
		if i.GetStatus() == returnsv1.InstallmentStatus_INSTALLMENT_STATUS_SHORT {
			out.ShortDueDates++
		}
	}
	for _, l := range p.GetInstalments() {
		line := &usv1.EstimatedPenaltyLine{Installment: l.GetNumber(), Due: wire(due[l.GetNumber()]), ShortfallCents: cents(l.GetShortfall()),
			TotalCents: cents(l.GetPenalty())}
		if d, paid := date(due[l.GetNumber()]), date(l.GetPaidOn()); !d.IsZero() && !paid.IsZero() {
			line.Days = int32(paid.DaysFrom(d))
		}
		out.Lines = append(out.Lines, line)
	}
	return out
}

func explanationOf(e *returnsv1.EstimatedExplanation) *usv1.EstimatedExplanation {
	return &usv1.EstimatedExplanation{Summary: e.GetSummary(), Paragraphs: e.GetParagraphs(), VersusFederal: e.GetVersusFederal(), Sources: e.GetSources()}
}
