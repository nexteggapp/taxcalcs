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
	var soFarTrad, soFarRoth, soFarHsa int64
	var restTrad, restRoth, restHsa int64
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
				restRoth += c.GetAmount().GetMinor()
			} else {
				restTrad += c.GetAmount().GetMinor()
			}
			d, err := civil.FromProto(c.GetDate())
			if err != nil {
				return nil, fmt.Errorf("model.retirement[%s].parts[%s]: %w", m.GetId(), p.GetId(), err)
			}
			date, f = &d, &scenariosv1.ContributionFigures{Kind: kind, Amount: model.Money(model.Value(c.GetAmount()))}
		case p.GetHsaContribution() != nil:
			c := p.GetHsaContribution()
			restHsa += c.GetAmount().GetMinor()
			d, err := civil.FromProto(c.GetDate())
			if err != nil {
				return nil, fmt.Errorf("model.retirement[%s].parts[%s]: %w", m.GetId(), p.GetId(), err)
			}
			date, f = &d, &scenariosv1.ContributionFigures{Kind: scenariosv1.ContributionKind_CONTRIBUTION_KIND_HSA, Amount: model.Money(model.Value(c.GetAmount()))}
		case p.GetFromForms() != nil:
			for _, form := range p.GetFromForms().GetForms() {
				if !model.Usable(form.GetSource()) {
					continue
				}
				d := ctx.YearEnd()
				switch {
				case form.GetForm_5498() != nil:
					f5498 := form.GetForm_5498()
					if trad := model.Value(f5498.GetIraContributions()); trad > 0 {
						soFarTrad += int64(trad)
						fig := &scenariosv1.ContributionFigures{Kind: scenariosv1.ContributionKind_CONTRIBUTION_KIND_TRADITIONAL_IRA, Amount: model.Money(trad)}
						out.Items = append(out.Items, &scenariosv1.Item{
							Id:      model.ItemID(prefix, d.String(), "contribution", form.GetId(), "trad"),
							Date:    d.Proto(),
							Status:  scenariosv1.ItemStatus_ITEM_STATUS_MADE,
							Kind:    scenariosv1.ItemKind_ITEM_KIND_CONTRIBUTION,
							Origin:  &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_DOCUMENT, Part: p.GetId(), Evidence: []string{form.GetId()}},
							Figures: &scenariosv1.Item_Contribution{Contribution: fig},
						})
					}
					if rothVal := model.Value(f5498.GetRothIraContributions()); rothVal > 0 {
						soFarRoth += int64(rothVal)
						fig := &scenariosv1.ContributionFigures{Kind: scenariosv1.ContributionKind_CONTRIBUTION_KIND_ROTH_IRA, Amount: model.Money(rothVal)}
						out.Items = append(out.Items, &scenariosv1.Item{
							Id:      model.ItemID(prefix, d.String(), "contribution", form.GetId(), "roth"),
							Date:    d.Proto(),
							Status:  scenariosv1.ItemStatus_ITEM_STATUS_MADE,
							Kind:    scenariosv1.ItemKind_ITEM_KIND_CONTRIBUTION,
							Origin:  &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_DOCUMENT, Part: p.GetId(), Evidence: []string{form.GetId()}},
							Figures: &scenariosv1.Item_Contribution{Contribution: fig},
						})
					}
				case form.GetForm_5498Sa() != nil:
					f5498sa := form.GetForm_5498Sa()
					hsaVal := model.Value(f5498sa.GetTotalContributionsMadeInYear()) +
						model.Value(f5498sa.GetContributionsMadeInNextYearForYear()) +
						model.Value(f5498sa.GetArcherMsaContributions())
					if hsaVal > 0 {
						soFarHsa += int64(hsaVal)
						fig := &scenariosv1.ContributionFigures{Kind: scenariosv1.ContributionKind_CONTRIBUTION_KIND_HSA, Amount: model.Money(hsaVal)}
						out.Items = append(out.Items, &scenariosv1.Item{
							Id:      model.ItemID(prefix, d.String(), "contribution", form.GetId()),
							Date:    d.Proto(),
							Status:  scenariosv1.ItemStatus_ITEM_STATUS_MADE,
							Kind:    scenariosv1.ItemKind_ITEM_KIND_CONTRIBUTION,
							Origin:  &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_DOCUMENT, Part: p.GetId(), Evidence: []string{form.GetId()}},
							Figures: &scenariosv1.Item_Contribution{Contribution: fig},
						})
					}
				}
			}
			continue
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
	soFar := &scenariosv1.ContributionTotals{TraditionalIra: &commonv1.Money{Minor: soFarTrad, Currency: "USD"},
		RothIra: &commonv1.Money{Minor: soFarRoth, Currency: "USD"}, Hsa: &commonv1.Money{Minor: soFarHsa, Currency: "USD"}}
	rest := &scenariosv1.ContributionTotals{TraditionalIra: &commonv1.Money{Minor: restTrad, Currency: "USD"},
		RothIra: &commonv1.Money{Minor: restRoth, Currency: "USD"}, Hsa: &commonv1.Money{Minor: restHsa, Currency: "USD"}}
	year := &scenariosv1.ContributionTotals{TraditionalIra: &commonv1.Money{Minor: soFarTrad + restTrad, Currency: "USD"},
		RothIra: &commonv1.Money{Minor: soFarRoth + restRoth, Currency: "USD"}, Hsa: &commonv1.Money{Minor: soFarHsa + restHsa, Currency: "USD"}}
	out.SoFar = &scenariosv1.RetirementTotals{Items: soFar}
	out.Rest = &scenariosv1.RetirementTotals{Items: rest}
	out.Year = &scenariosv1.RetirementTotals{Items: year}
	return out, nil
}
