package ustaxcalcsvc

import (
	usv1 "github.com/nexteggapp/taxcalcs/us/v1"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
)

// This file is the whole of the translation between the wire contract and the
// calculators. It is deliberately dull and deliberately in one place: every
// function here is a field-for-field copy, so a field added to one side and not
// the other is a compile error or a visibly missing line rather than a figure
// that silently arrives as zero.

var filingStatuses = map[usv1.FilingStatus]ustaxcalc.FilingStatus{
	usv1.FilingStatus_FILING_STATUS_SINGLE:                    ustaxcalc.Single,
	usv1.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY:    ustaxcalc.MarriedFilingJointly,
	usv1.FilingStatus_FILING_STATUS_MARRIED_FILING_SEPARATELY: ustaxcalc.MarriedFilingSeparately,
	usv1.FilingStatus_FILING_STATUS_HEAD_OF_HOUSEHOLD:         ustaxcalc.HeadOfHousehold,
	usv1.FilingStatus_FILING_STATUS_SURVIVING_SPOUSE:          ustaxcalc.SurvivingSpouse,
}

var filingStatusProtos = map[ustaxcalc.FilingStatus]usv1.FilingStatus{
	ustaxcalc.Single:                  usv1.FilingStatus_FILING_STATUS_SINGLE,
	ustaxcalc.MarriedFilingJointly:    usv1.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY,
	ustaxcalc.MarriedFilingSeparately: usv1.FilingStatus_FILING_STATUS_MARRIED_FILING_SEPARATELY,
	ustaxcalc.HeadOfHousehold:         usv1.FilingStatus_FILING_STATUS_HEAD_OF_HOUSEHOLD,
	ustaxcalc.SurvivingSpouse:         usv1.FilingStatus_FILING_STATUS_SURVIVING_SPOUSE,
}

var positionKinds = map[ustaxcalc.PositionKind]usv1.PositionKind{
	ustaxcalc.PositionDeductionMethod:        usv1.PositionKind_POSITION_KIND_DEDUCTION_METHOD,
	ustaxcalc.PositionLotSelection:           usv1.PositionKind_POSITION_KIND_LOT_SELECTION,
	ustaxcalc.PositionIRADeductibility:       usv1.PositionKind_POSITION_KIND_IRA_DEDUCTIBILITY,
	ustaxcalc.PositionSubstantiallyIdentical: usv1.PositionKind_POSITION_KIND_SUBSTANTIALLY_IDENTICAL,
	ustaxcalc.PositionTradeOrBusiness:        usv1.PositionKind_POSITION_KIND_TRADE_OR_BUSINESS,
	ustaxcalc.PositionDependentQualifies:     usv1.PositionKind_POSITION_KIND_DEPENDENT_QUALIFIES,
}

// conditions reads the § 63(f) facts off a Filer.
func conditions(f *usv1.Filer) ustaxcalc.Conditions {
	return ustaxcalc.Conditions{
		Aged65OrOlder:       f.GetAged_65OrOlder(),
		Blind:               f.GetBlind(),
		SpouseAged65OrOlder: f.GetSpouseAged_65OrOlder(),
		SpouseBlind:         f.GetSpouseBlind(),
	}
}

func rules(rs []ustaxcalc.Rule) []*usv1.Rule {
	if len(rs) == 0 {
		return nil
	}
	out := make([]*usv1.Rule, 0, len(rs))
	for _, r := range rs {
		out = append(out, &usv1.Rule{Cite: r.Cite(), Applied: r.Applied()})
	}
	return out
}

func positions(rs []ustaxcalc.Required) []*usv1.Position {
	if len(rs) == 0 {
		return nil
	}
	out := make([]*usv1.Position, 0, len(rs))
	for _, r := range rs {
		out = append(out, &usv1.Position{
			Kind:    positionKinds[r.Position()],
			Because: r.Because(),
		})
	}
	return out
}

func ladder(l ustaxcalc.Ladder) *usv1.Ladder {
	bands := make([]*usv1.Band, 0, len(l.Bands))
	for _, b := range l.Bands {
		bands = append(bands, &usv1.Band{
			RateBasisPoints: int32(b.Rate),
			FromCents:       int64(b.From),
			ToCents:         int64(b.To),
			IncomeCents:     int64(b.Income),
			TaxCents:        int64(b.Tax),
		})
	}
	return &usv1.Ladder{
		Bands:                   bands,
		TotalCents:              int64(l.Total),
		MarginalRateBasisPoints: int32(l.Marginal),
		RoomInBracketCents:      int64(l.RoomInBracket),
	}
}

func gains(g ustaxcalc.GainsTax) *usv1.GainsTax {
	return &usv1.GainsTax{
		AtZeroCents:    int64(g.AtZero),
		AtFifteenCents: int64(g.At15),
		AtTwentyCents:  int64(g.At20),
		TotalCents:     int64(g.Total),
	}
}

func deduction(d ustaxcalc.Deduction) *usv1.Deduction {
	return &usv1.Deduction{
		AmountCents:   int64(d.Amount),
		Itemised:      d.Itemised,
		StandardCents: int64(d.Standard),
	}
}

func seca(s ustaxcalc.SECA) *usv1.Seca {
	return &usv1.Seca{
		NetEarningsCents:           int64(s.NetEarnings),
		SocialSecurityCents:        int64(s.SocialSecurity),
		MedicareCents:              int64(s.Medicare),
		AdditionalMedicareCents:    int64(s.AdditionalMedicare),
		TotalCents:                 int64(s.Total),
		AboveTheLineDeductionCents: int64(s.AboveTheLineDeduction),
	}
}

func safeHarbour(h ustaxcalc.SafeHarbour) *usv1.SafeHarbour {
	return &usv1.SafeHarbour{
		RequiredCents:            int64(h.Required),
		CurrentYearTestCents:     int64(h.CurrentYearTest),
		PriorYearTestCents:       int64(h.PriorYearTest),
		PriorYearRateBasisPoints: int32(h.PriorYearRate),
	}
}

func instalments(is []ustaxcalc.Instalment) []*usv1.Instalment {
	out := make([]*usv1.Instalment, 0, len(is))
	for _, i := range is {
		// Split rather than formatted, because a statutory date is a calendar
		// date and not an instant. Rendering it needs a time zone this package
		// has no business choosing.
		out = append(out, &usv1.Instalment{
			DueYear:               int32(i.Due.Year()),
			DueMonth:              int32(i.Due.Month()),
			DueDay:                int32(i.Due.Day()),
			CumulativeTargetCents: int64(i.CumulativeTarget),
		})
	}
	return out
}

func rateTable(t ustaxcalc.Table) *usv1.RateTable {
	ordinary := make([]*usv1.Bracket, 0, len(t.Ordinary))
	for _, b := range t.Ordinary {
		ordinary = append(ordinary, &usv1.Bracket{
			OverCents:       int64(b.Over),
			RateBasisPoints: int32(b.Rate),
		})
	}
	return &usv1.RateTable{
		TaxYear:                          int32(t.Year),
		FilingStatus:                     filingStatusProtos[t.Status],
		Ordinary:                         ordinary,
		StandardDeductionCents:           int64(t.StandardDeduction),
		AdditionalAgedBlindCents:         int64(t.AdditionalAgedBlind),
		MaxZeroRateAmountCents:           int64(t.MaxZeroRateAmount),
		MaxFifteenRateAmountCents:        int64(t.Max15RateAmount),
		SocialSecurityWageBaseCents:      int64(t.SocialSecurityWageBase),
		AdditionalMedicareThresholdCents: int64(t.AdditionalMedicareThreshold),
		PriorYearAgiStepCents:            int64(t.PriorYearAGIStep),
	}
}
