package taxyear

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// Each model's can_add, as the employer and tax paid models fill their
// own: each part kind that can be added now, with its defaults.
// Nothing is offered once the year's form is in. A total or an end is
// offered once, while the model has none, with the totals filled from the
// projection; a dated part starts the day after the evidence.

// offer is what one model's rules need: the day a new part starts, and
// which part kinds the model holds.
type offer struct {
	from civil.Date
	end  *commonv1.Date
	kind map[string]bool
}

// newOffer reads a model's parts. A part starts the day after the
// evidence, or after as_of without any, and no later than December 31.
func newOffer[P proto.Message](ctx *model.Context, parts []P, through *commonv1.Date) offer {
	o := offer{from: ctx.AsOf().AddDays(1), end: ctx.YearEnd().Proto(), kind: map[string]bool{}}
	if d, err := civil.FromProto(through); err == nil && through != nil {
		o.from = d.AddDays(1)
	}
	if o.from.After(ctx.YearEnd()) {
		o.from = ctx.YearEnd()
	}
	for _, p := range parts {
		_, kind, _ := partCase(p.ProtoReflect())
		o.kind[kind] = true
	}
	return o
}

// has reports whether the model holds a part of any of the kinds.
func (o offer) has(kinds ...string) bool {
	for _, k := range kinds {
		if o.kind[k] {
			return true
		}
	}
	return false
}

func (o offer) start() *commonv1.Date { return o.from.Proto() }

// filled returns a new message of the type of into with each money field
// that from has by the same name.
func filled[M proto.Message](into M, from proto.Message) M {
	dst := into.ProtoReflect()
	if from == nil {
		return into
	}
	src := from.ProtoReflect()
	fields := dst.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		sf := src.Descriptor().Fields().ByName(f.Name())
		if sf == nil || f.Message() == nil || sf.Message() == nil || f.Message().FullName() != sf.Message().FullName() || !src.Has(sf) {
			continue
		}
		dst.Set(f, protoreflect.ValueOfMessage(proto.Clone(src.Get(sf).Message().Interface()).ProtoReflect()))
	}
	return into
}

func businessCanAdd(ctx *model.Context, m *scenariosv1.BusinessModel, p *scenariosv1.BusinessProjection) []*scenariosv1.BusinessPart {
	o := newOffer(ctx, m.GetParts(), p.GetSoFarThrough())
	out := []*scenariosv1.BusinessPart{{Part: &scenariosv1.BusinessPart_Monthly_{Monthly: &scenariosv1.BusinessPart_Monthly{
		From: o.start(), Receipts: model.Money(0), Expenses: model.Money(0)}}}}
	if !o.has("stopped", "stops") {
		out = append(out, &scenariosv1.BusinessPart{Part: &scenariosv1.BusinessPart_Stops_{Stops: &scenariosv1.BusinessPart_Stops{On: o.end}}})
	}
	if !o.has("year_total") {
		total := filled(&scenariosv1.BusinessPart_YearTotal{}, p.GetYear().GetItems())
		out = append(out, &scenariosv1.BusinessPart{Part: &scenariosv1.BusinessPart_YearTotal_{YearTotal: total}})
	}
	return out
}

func accountCanAdd(ctx *model.Context, m *scenariosv1.AccountModel, p *scenariosv1.AccountProjection) []*scenariosv1.AccountPart {
	o := newOffer(ctx, m.GetParts(), p.GetSoFarThrough())
	if o.has("from_forms") {
		return nil
	}
	out := []*scenariosv1.AccountPart{
		{Part: &scenariosv1.AccountPart_AtRate_{AtRate: &scenariosv1.AccountPart_AtRate{From: o.start()}}},
		{Part: &scenariosv1.AccountPart_YearEndDistribution_{YearEndDistribution: &scenariosv1.AccountPart_YearEndDistribution{}}},
	}
	if !o.has("closed", "closes") {
		out = append(out, &scenariosv1.AccountPart{Part: &scenariosv1.AccountPart_Closes_{Closes: &scenariosv1.AccountPart_Closes{On: o.end}}})
	}
	if !o.has("year_total") {
		total := filled(&scenariosv1.AccountPart_YearTotal{}, p.GetYear().GetItems())
		out = append(out, &scenariosv1.AccountPart{Part: &scenariosv1.AccountPart_YearTotal_{YearTotal: total}})
	}
	return out
}

func brokerCanAdd(ctx *model.Context, m *scenariosv1.BrokerModel, p *scenariosv1.BrokerProjection) []*scenariosv1.BrokerPart {
	o := newOffer(ctx, m.GetParts(), p.GetSoFarThrough())
	if o.has("from_forms") {
		return nil
	}
	return []*scenariosv1.BrokerPart{
		{Part: &scenariosv1.BrokerPart_PlannedSale_{PlannedSale: &scenariosv1.BrokerPart_PlannedSale{
			Date: o.start(), Price: &scenariosv1.BrokerPart_PlannedSale_Assumption{Assumption: scenariosv1.PriceAssumption_PRICE_ASSUMPTION_TODAY}}}},
		{Part: &scenariosv1.BrokerPart_PlannedGain_{PlannedGain: &scenariosv1.BrokerPart_PlannedGain{
			Date: o.start(), Term: scenariosv1.GainTerm_GAIN_TERM_SHORT, Amount: model.Money(0)}}},
	}
}

func retirementCanAdd(ctx *model.Context, m *scenariosv1.RetirementModel) []*scenariosv1.RetirementPart {
	o := newOffer(ctx, m.GetParts(), nil)
	var out []*scenariosv1.RetirementPart
	if !o.has("max_out_workplace_plan", "workplace_plan_total") {
		out = append(out,
			&scenariosv1.RetirementPart{Part: &scenariosv1.RetirementPart_MaxOutWorkplacePlan_{MaxOutWorkplacePlan: &scenariosv1.RetirementPart_MaxOutWorkplacePlan{}}},
			&scenariosv1.RetirementPart{Part: &scenariosv1.RetirementPart_WorkplacePlanTotal_{WorkplacePlanTotal: &scenariosv1.RetirementPart_WorkplacePlanTotal{Total: model.Money(0)}}})
	}
	return append(out,
		&scenariosv1.RetirementPart{Part: &scenariosv1.RetirementPart_IraContribution_{IraContribution: &scenariosv1.RetirementPart_IraContribution{
			Date: o.start(), Kind: scenariosv1.IraKind_IRA_KIND_TRADITIONAL, Amount: model.Money(0)}}},
		&scenariosv1.RetirementPart{Part: &scenariosv1.RetirementPart_HsaContribution_{HsaContribution: &scenariosv1.RetirementPart_HsaContribution{
			Date: o.start(), Amount: model.Money(0)}}})
}

func otherIncomeCanAdd(ctx *model.Context, m *scenariosv1.OtherIncomeModel, p *scenariosv1.OtherIncomeProjection) []*scenariosv1.OtherIncomePart {
	o := newOffer(ctx, m.GetParts(), p.GetSoFarThrough())
	if o.has("from_forms") {
		return nil
	}
	out := []*scenariosv1.OtherIncomePart{{Part: &scenariosv1.OtherIncomePart_OneOff_{OneOff: &scenariosv1.OtherIncomePart_OneOff{
		Date: o.start(), Event: &scenariosv1.OtherIncomePart_OneOff_Distribution_{Distribution: &scenariosv1.OtherIncomePart_OneOff_Distribution{
			Kind: scenariosv1.DistributionKind_DISTRIBUTION_KIND_WITHDRAWAL, Gross: model.Money(0)}}}}}}
	if !o.has("year_amount") {
		amount := filled(&scenariosv1.OtherIncomePart_YearAmount{}, p.GetYear().GetItems())
		out = append(out, &scenariosv1.OtherIncomePart{Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: amount}})
	}
	return out
}

func itemisedCanAdd(ctx *model.Context, m *scenariosv1.ItemisedModel, p *scenariosv1.ItemisedProjection) []*scenariosv1.ItemisedPartChoice {
	year, rest := p.GetYear().GetItems(), p.GetRest().GetItems()
	restOfYear := func() *scenariosv1.RestOfYear { return &scenariosv1.RestOfYear{Amount: money0(rest)} }
	var out []*scenariosv1.ItemisedPartChoice
	switch k := m.GetKind().(type) {
	case *scenariosv1.ItemisedModel_Mortgage:
		o := newOffer(ctx, k.Mortgage.GetParts(), p.GetSoFarThrough())
		if o.has("from_form_1098") {
			return nil
		}
		add := func(part *scenariosv1.MortgagePart) {
			out = append(out, &scenariosv1.ItemisedPartChoice{Part: &scenariosv1.ItemisedPartChoice_Mortgage{Mortgage: part}})
		}
		add(&scenariosv1.MortgagePart{Part: &scenariosv1.MortgagePart_Refinanced_{Refinanced: &scenariosv1.MortgagePart_Refinanced{From: o.start()}}})
		if !o.has("repaid", "paid_off") {
			add(&scenariosv1.MortgagePart{Part: &scenariosv1.MortgagePart_PaidOff_{PaidOff: &scenariosv1.MortgagePart_PaidOff{On: o.end}}})
		}
		if !o.has("year_total", "rest_of_year") {
			add(&scenariosv1.MortgagePart{Part: &scenariosv1.MortgagePart_YearTotal_{YearTotal: &scenariosv1.MortgagePart_YearTotal{Interest: money0(year)}}})
			add(&scenariosv1.MortgagePart{Part: &scenariosv1.MortgagePart_RestOfYear{RestOfYear: restOfYear()}})
		}
	case *scenariosv1.ItemisedModel_PropertyTax:
		o := newOffer(ctx, k.PropertyTax.GetParts(), p.GetSoFarThrough())
		add := func(part *scenariosv1.PropertyTaxPart) {
			out = append(out, &scenariosv1.ItemisedPartChoice{Part: &scenariosv1.ItemisedPartChoice_PropertyTax{PropertyTax: part}})
		}
		add(&scenariosv1.PropertyTaxPart{Part: &scenariosv1.PropertyTaxPart_PlannedInstallment_{PlannedInstallment: &scenariosv1.PropertyTaxPart_PlannedInstallment{
			Due: o.start(), Amount: model.Money(0)}}})
		if !o.has("year_total", "rest_of_year") {
			add(&scenariosv1.PropertyTaxPart{Part: &scenariosv1.PropertyTaxPart_YearTotal_{YearTotal: &scenariosv1.PropertyTaxPart_YearTotal{Total: money0(year)}}})
			add(&scenariosv1.PropertyTaxPart{Part: &scenariosv1.PropertyTaxPart_RestOfYear{RestOfYear: restOfYear()}})
		}
	case *scenariosv1.ItemisedModel_Charity:
		o := newOffer(ctx, k.Charity.GetParts(), p.GetSoFarThrough())
		add := func(part *scenariosv1.CharityPart) {
			out = append(out, &scenariosv1.ItemisedPartChoice{Part: &scenariosv1.ItemisedPartChoice_Charity{Charity: part}})
		}
		add(&scenariosv1.CharityPart{Part: &scenariosv1.CharityPart_PlannedGift_{PlannedGift: &scenariosv1.CharityPart_PlannedGift{Date: o.start(), Amount: model.Money(0)}}})
		if !o.has("like_last_year", "rest_of_year") {
			add(&scenariosv1.CharityPart{Part: &scenariosv1.CharityPart_LikeLastYear_{LikeLastYear: &scenariosv1.CharityPart_LikeLastYear{From: o.start()}}})
			add(&scenariosv1.CharityPart{Part: &scenariosv1.CharityPart_RestOfYear{RestOfYear: restOfYear()}})
		}
	case *scenariosv1.ItemisedModel_Medical:
		o := newOffer(ctx, k.Medical.GetParts(), p.GetSoFarThrough())
		add := func(part *scenariosv1.MedicalPart) {
			out = append(out, &scenariosv1.ItemisedPartChoice{Part: &scenariosv1.ItemisedPartChoice_Medical{Medical: part}})
		}
		add(&scenariosv1.MedicalPart{Part: &scenariosv1.MedicalPart_PlannedExpense_{PlannedExpense: &scenariosv1.MedicalPart_PlannedExpense{Date: o.start(), Amount: model.Money(0)}}})
		if !o.has("year_total", "rest_of_year") {
			add(&scenariosv1.MedicalPart{Part: &scenariosv1.MedicalPart_YearTotal_{YearTotal: &scenariosv1.MedicalPart_YearTotal{Total: money0(year)}}})
			add(&scenariosv1.MedicalPart{Part: &scenariosv1.MedicalPart_RestOfYear{RestOfYear: restOfYear()}})
		}
	}
	return out
}

// money0 is a copy of an amount, or $0 when it is unset.
func money0(m *commonv1.Money) *commonv1.Money {
	if m == nil {
		return model.Money(0)
	}
	return proto.Clone(m).(*commonv1.Money)
}
