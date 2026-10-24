// Package retirement expands a RetirementModel's additive parts, IRA and
// direct HSA contributions, into items. Its adjustment parts,
// MaxOutWorkplacePlan and WorkplacePlanTotal, change the person's employer
// items, and model/employer's MaxOut applies them. See docs/models.md.
package retirement

import (
	"fmt"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// PlanPart returns the person's plan adjustment part, or nil.
func PlanPart(m *scenariosv1.RetirementModel) *scenariosv1.RetirementPart {
	for _, p := range m.GetParts() {
		if p.GetMaxOutWorkplacePlan() != nil || p.GetWorkplacePlanTotal() != nil {
			return p
		}
	}
	return nil
}

// Expand returns the person's contributions as items. An IRA contribution
// may be dated up to next April 15 and still count for this year.
func Expand(ctx *model.Context, m *scenariosv1.RetirementModel) (*scenariosv1.RetirementProjection, error) {
	out := &scenariosv1.RetirementProjection{Id: m.GetId()}
	var traditional, roth, hsa int64
	prefix := model.ItemID("retirement", m.GetId())
	for _, p := range m.GetParts() {
		var date *civil.Date
		var f *scenariosv1.ContributionFigures
		switch {
		case p.GetIraContribution() != nil:
			c := p.GetIraContribution()
			kind := scenariosv1.ContributionKind_CONTRIBUTION_KIND_TRADITIONAL_IRA
			if c.GetKind() == scenariosv1.IraKind_IRA_KIND_ROTH {
				kind = scenariosv1.ContributionKind_CONTRIBUTION_KIND_ROTH_IRA
				roth += c.GetAmount().GetMinor()
			} else {
				traditional += c.GetAmount().GetMinor()
			}
			d, err := civil.FromProto(c.GetDate())
			if err != nil {
				return nil, fmt.Errorf("model.retirement[%s].parts[%s]: %w", m.GetId(), p.GetId(), err)
			}
			date, f = &d, &scenariosv1.ContributionFigures{Kind: kind, Amount: model.Money(model.Value(c.GetAmount()))}
		case p.GetHsaContribution() != nil:
			c := p.GetHsaContribution()
			hsa += c.GetAmount().GetMinor()
			d, err := civil.FromProto(c.GetDate())
			if err != nil {
				return nil, fmt.Errorf("model.retirement[%s].parts[%s]: %w", m.GetId(), p.GetId(), err)
			}
			date, f = &d, &scenariosv1.ContributionFigures{Kind: scenariosv1.ContributionKind_CONTRIBUTION_KIND_HSA, Amount: model.Money(model.Value(c.GetAmount()))}
		default:
			continue
		}
		if date.IsZero() {
			*date = ctx.YearEnd()
		}
		out.Items = append(out.Items, &scenariosv1.Item{Id: model.ItemID(prefix, date.String(), "contribution", p.GetId()), Date: date.Proto(),
			Status: scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED, Kind: scenariosv1.ItemKind_ITEM_KIND_CONTRIBUTION,
			Origin:  &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_ASSUMED, Part: p.GetId()},
			Figures: &scenariosv1.Item_Contribution{Contribution: f}})
	}
	model.SortItems(out.Items)
	zero := &scenariosv1.ContributionTotals{TraditionalIra: model.Money(0), RothIra: model.Money(0), Hsa: model.Money(0)}
	all := &scenariosv1.ContributionTotals{TraditionalIra: &commonv1.Money{Minor: traditional, Currency: "USD"},
		RothIra: &commonv1.Money{Minor: roth, Currency: "USD"}, Hsa: &commonv1.Money{Minor: hsa, Currency: "USD"}}
	out.SoFar = &scenariosv1.RetirementTotals{Items: zero}
	out.Rest = &scenariosv1.RetirementTotals{Items: all}
	out.Year = &scenariosv1.RetirementTotals{Items: all}
	return out, nil
}
