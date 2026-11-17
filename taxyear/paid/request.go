package paid

import (
	"time"

	"github.com/nexteggapp/taxcalcs/money"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	"google.golang.org/protobuf/proto"
)

// request is the ReturnInputs that asks the jurisdiction's estimated tax
// rules: this year's tax as a caller estimate, last year's return, the
// residence, the withholding, the payments — made only, or every one that
// counts — and the elections. It is asked as of December 31 of the next
// year, after every due date, so the penalty runs as far as the rules let it
// and every payment given counts as made; the payments planned are told
// apart by leaving them out.
func (r *runner) request(madeOnly bool) (*returnsv1.ReturnInputs, error) {
	year := r.in.ctx.Year()
	est := &returnsv1.EstimatedTaxInputs{
		AsOf:          civil.Must(year+1, time.December, 31).Proto(),
		Jurisdictions: []string{r.j},
	}
	fed := &returnsv1.Federal1040Inputs{Elections: &returnsv1.Elections{FilingStatus: returnsv1.FilingStatusElection(r.in.status)}}
	in := &returnsv1.ReturnInputs{SchemaVersion: 1, TaxYear: int32(year), Federal: fed, EstimatedTax: est}
	if r.j != "US" {
		in.States = []*returnsv1.StateInputs{{State: r.j}}
	}
	if err := r.thisYear(est); err != nil {
		return nil, err
	}
	r.priorYear(in)
	r.residence(est)
	if err := r.payments(est, madeOnly); err != nil {
		return nil, err
	}
	r.elections(est)
	if s := r.stateInputs(); s != nil {
		est.StateInputs = append(est.StateInputs, s)
	}
	return in, nil
}

func usd(c money.USCents) *commonv1.Money { return model.Money(c) }

// thisYear gives this year's tax, when known, and the withholding, in
// total and by date: a caller estimate, or last year's figures alone when
// there is neither.
func (r *runner) thisYear(est *returnsv1.EstimatedTaxInputs) error {
	withheld, err := r.withholding()
	if err != nil {
		return err
	}
	cj := &returnsv1.EstimatedTaxInputs_CallerEstimate_Jurisdiction{Jurisdiction: r.j}
	if r.taxKnown {
		cj.Tax = usd(r.tax)
	}
	var total money.USCents
	for _, w := range withheld {
		total += w.v
		est.Withholding = append(est.Withholding, &returnsv1.EstimatedTaxInputs_Withholding{Jurisdiction: r.j, Date: w.on.Proto(), Amount: usd(w.v)})
	}
	if len(withheld) > 0 {
		cj.Withheld = usd(total)
	}
	if cj.Tax == nil && cj.Withheld == nil {
		est.ThisYear = &returnsv1.EstimatedTaxInputs_LastYearOnly_{LastYearOnly: &returnsv1.EstimatedTaxInputs_LastYearOnly{}}
		return nil
	}
	est.ThisYear = &returnsv1.EstimatedTaxInputs_CallerEstimate_{CallerEstimate: &returnsv1.EstimatedTaxInputs_CallerEstimate{
		Jurisdictions: []*returnsv1.EstimatedTaxInputs_CallerEstimate_Jurisdiction{cj}}}
	return nil
}

// hasPrior reports a return on record for last year: a status, or figures.
func (p prior) hasPrior() bool {
	return p.status != factsv1.PriorYearReturn_STATUS_UNSPECIFIED || p.taxSet
}

// hasFigures reports last year's figures from a return filed.
func (p prior) hasFigures() bool {
	return p.hasPrior() && p.status != factsv1.PriorYearReturn_STATUS_NONE && p.taxSet
}

// priorYear gives last year's return: federally, ReturnInputs.prior_year,
// federal_prior_return and whether the household were US residents all
// year; for a state, its PriorYear entry, with the federal AGI the state's
// return started from in ReturnInputs.prior_year. No return on record is
// federal_prior_return unset, or an entry that names the state alone.
func (r *runner) priorYear(in *returnsv1.ReturnInputs) {
	est, fed, p := in.GetEstimatedTax(), in.GetFederal(), r.prior
	status := returnsv1.EstimatedTaxInputs_PRIOR_RETURN_UNSPECIFIED
	switch {
	case !p.hasPrior():
	case p.status == factsv1.PriorYearReturn_STATUS_NONE:
		status = returnsv1.EstimatedTaxInputs_PRIOR_RETURN_NOT_FILED
	default:
		status = returnsv1.EstimatedTaxInputs_PRIOR_RETURN_FULL_YEAR
	}
	if p.hasFigures() && p.agiSet {
		fed.PriorYear = &returnsv1.PriorYear{PriorYearAgi: usd(p.agi)}
	}
	if r.j == "US" {
		est.FederalPriorReturn = status
		if p.hasPrior() && p.fullYear != nil {
			est.PriorYearUsCitizenOrResident = proto.Bool(*p.fullYear)
		}
		if p.hasFigures() {
			if fed.PriorYear == nil {
				fed.PriorYear = &returnsv1.PriorYear{}
			}
			fed.PriorYear.PriorYearTotalTax = usd(p.tax)
		}
		return
	}
	py := &returnsv1.EstimatedTaxInputs_PriorYear{Jurisdiction: r.j, Return: status}
	if p.hasPrior() && p.fullYear != nil {
		py.Residency = returnsv1.EstimatedTaxInputs_RESIDENCY_PART_YEAR
		if *p.fullYear {
			py.Residency = returnsv1.EstimatedTaxInputs_RESIDENCY_FULL_YEAR
		}
	}
	if p.hasFigures() {
		py.Tax = usd(p.tax)
		if p.stateAGISet {
			py.Agi = usd(p.stateAGI)
		}
		py.FilingStatus = form1040.FilingStatus(r.in.status)
	}
	est.PriorYears = append(est.PriorYears, py)
}

// residence gives a state the facts' residence periods, an open end read as
// the year's first or last day.
func (r *runner) residence(est *returnsv1.EstimatedTaxInputs) {
	if r.j == "US" {
		return
	}
	for _, res := range r.in.ctx.Facts().GetResidence() {
		from, err1 := civil.FromProto(res.GetFrom())
		to, err2 := civil.FromProto(res.GetTo())
		if err1 != nil || err2 != nil {
			continue
		}
		if from.IsZero() {
			from = r.in.ctx.YearStart()
		}
		if to.IsZero() {
			to = r.in.ctx.YearEnd()
		}
		est.Residence = append(est.Residence, &returnsv1.EstimatedTaxInputs_ResidencePeriod{State: res.GetState(), From: from.Proto(), To: to.Proto()})
	}
}

// payments gives the jurisdiction's payments that count, and when payments
// could first be expected. Last year's overpayment applied is credited on
// January 1, so it meets the first installment, as it does on the return.
func (r *runner) payments(est *returnsv1.EstimatedTaxInputs, madeOnly bool) error {
	p := r.in.payments[r.j]
	if p == nil {
		return nil
	}
	year := r.in.ctx.Year()
	for _, x := range p.Payments() {
		if madeOnly && !x.Made() {
			continue
		}
		on := x.On()
		if x.FromLastYear() {
			on = r.in.ctx.YearStart()
		}
		if on.IsZero() {
			return errMissingDate
		}
		est.Payments = append(est.Payments, &returnsv1.EstimatedPayment{Jurisdiction: r.j, DatePaid: on.Proto(),
			DesignatedTaxYear: int32(year), Amount: usd(x.Amount())})
	}
	if from := p.PaymentsFrom(); !from.IsZero() {
		est.RequirementFirstMet = from.Proto()
	}
	return nil
}

// elections gives the withholding-dates election and a waiver request.
// Withholding is credited evenly unless the household elected otherwise.
func (r *runner) elections(est *returnsv1.EstimatedTaxInputs) {
	est.WithholdingCredit = returnsv1.EstimatedTaxInputs_WITHHOLDING_CREDIT_EVENLY
	for _, x := range r.in.ctx.Facts().GetElections() {
		if w := x.GetWithholdingDates(); w != nil && w.GetJurisdiction() == r.j && w.GetAsWithheld() {
			est.WithholdingCredit = returnsv1.EstimatedTaxInputs_WITHHOLDING_CREDIT_AS_WITHHELD
		}
		if w := x.GetWaiver(); w != nil && w.GetJurisdiction() == r.j {
			if wv := waiverOf(w); wv != nil {
				est.Waiver = wv
			}
		}
	}
}

// waiverOf is the waiver asked for, or nil while the question is
// unanswered.
func waiverOf(w *factsv1.WaiverRequest) *returnsv1.EstimatedTaxInputs_Waiver {
	causes := map[factsv1.WaiverRequest_Reason]returnsv1.EstimatedTaxInputs_WaiverCause{
		factsv1.WaiverRequest_REASON_NONE:     returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_NONE,
		factsv1.WaiverRequest_REASON_CASUALTY: returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_CASUALTY,
		factsv1.WaiverRequest_REASON_DISASTER: returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_DISASTER,
		factsv1.WaiverRequest_REASON_RETIRED:  returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_RETIREMENT,
		factsv1.WaiverRequest_REASON_DISABLED: returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_DISABILITY,
	}
	cause, ok := causes[w.GetReason()]
	if !ok {
		return nil
	}
	out := &returnsv1.EstimatedTaxInputs_Waiver{Cause: cause}
	if cause != returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_NONE {
		if d, err := civil.FromProto(w.GetOn()); err == nil && !d.IsZero() {
			out.Occurred = d.Proto()
		}
	}
	return out
}

// stateInputs is the state's own EstimatedStateInputs entry, for a state
// whose rules read a figure the core doesn't hold, or nil: this year's AGI
// as the state measures it, this year's federal AGI, and last year's
// withholding and taxable income.
func (r *runner) stateInputs() *returnsv1.EstimatedStateInputs {
	stateAGI, lived := r.stateAGI()
	agi := func() *commonv1.Money {
		if !lived {
			return nil
		}
		return usd(stateAGI)
	}
	p := r.prior
	priorWithheld := func() *commonv1.Money {
		if !p.hasFigures() || !p.withheldSet {
			return nil
		}
		return usd(p.withheld)
	}
	switch r.j {
	case "AL":
		return &returnsv1.EstimatedStateInputs{State: r.j, Inputs: &returnsv1.EstimatedStateInputs_Al{Al: &returnsv1.AlabamaEstimatedInputs{AlabamaAgi: agi()}}}
	case "AZ":
		return &returnsv1.EstimatedStateInputs{State: r.j, Inputs: &returnsv1.EstimatedStateInputs_Az{Az: &returnsv1.ArizonaEstimatedInputs{FederalAgi: usd(r.in.federalAGI)}}}
	case "CA":
		return &returnsv1.EstimatedStateInputs{State: r.j, Inputs: &returnsv1.EstimatedStateInputs_Ca{Ca: &returnsv1.CaliforniaEstimatedInputs{CaliforniaAgi: agi(), PriorYearWithholding: priorWithheld()}}}
	case "IL":
		if w := priorWithheld(); w != nil {
			return &returnsv1.EstimatedStateInputs{State: r.j, Inputs: &returnsv1.EstimatedStateInputs_Il{Il: &returnsv1.IllinoisEstimatedInputs{PriorYearWithheld: w}}}
		}
	case "MD":
		return &returnsv1.EstimatedStateInputs{State: r.j, Inputs: &returnsv1.EstimatedStateInputs_Md{Md: &returnsv1.MarylandEstimatedInputs{MarylandAgi: agi()}}}
	case "MI":
		return &returnsv1.EstimatedStateInputs{State: r.j, Inputs: &returnsv1.EstimatedStateInputs_Mi{Mi: &returnsv1.MichiganEstimatedInputs{PriorYearWithheld: priorWithheld()}}}
	case "NM":
		if w := priorWithheld(); w != nil {
			return &returnsv1.EstimatedStateInputs{State: r.j, Inputs: &returnsv1.EstimatedStateInputs_Nm{Nm: &returnsv1.NewMexicoEstimatedInputs{PriorYearWithheld: w}}}
		}
	case "PA":
		pa := &returnsv1.PennsylvaniaEstimatedInputs{PriorYearWithheld: priorWithheld()}
		if p.hasFigures() && p.taxableSet {
			pa.PriorYearTaxableIncome = usd(p.taxable)
		}
		return &returnsv1.EstimatedStateInputs{State: r.j, Inputs: &returnsv1.EstimatedStateInputs_Pa{Pa: pa}}
	case "VA":
		return &returnsv1.EstimatedStateInputs{State: r.j, Inputs: &returnsv1.EstimatedStateInputs_Va{Va: &returnsv1.VirginiaEstimatedInputs{VirginiaAgi: agi()}}}
	}
	return nil
}
