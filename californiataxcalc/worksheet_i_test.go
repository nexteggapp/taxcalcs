package californiataxcalc_test

import (
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/californiataxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Worksheet I's lines, as the FTB 5805 instructions print them.
func TestWorksheetIIsInItsSource(t *testing.T) {
	mustContain(t, "ftb-5805-2025-instructions.txt",
		"Line 12 – Form 540NR filers. Complete Worksheet I, Prorated Tax for Form 540NR Filers, below.",
		"Prorate the credits on line 51 through line 53 using the ratio from Worksheet I, line N.",
		"C. Annualized California tax rate: Divide line A by line B",
		"H. Annualized itemized or standard deduction rate. Divide line F by line G. Do not enter more than 1.0000",
		"K. Annualized California taxable income. Subtract line J from line F",
		"P. Annualized California tax after exemption credits. Subtract line O from line L. Enter this amount here and on Part III, line 12",
	)
}

// column is one Worksheet I column with $6,000 of tax on $94,500 of total
// taxable income, a $5,500 deduction, $153 of exemption credits and $100 of
// credits to prorate.
func column(t *testing.T, caAGI, totalAGI money.USCents) californiataxcalc.WorksheetIColumn {
	t.Helper()
	return must(californiataxcalc.NewWorksheetIColumn().TaxOnTotal(6_000 * money.Dollar).TotalTaxable(94_500 * money.Dollar).
		CaliforniaAGI(caAGI).AnnualisedTotalAGI(totalAGI).Deduction(5_500 * money.Dollar).ExemptionCredits(153 * money.Dollar).
		ProratedCredits(100 * money.Dollar).Build())
}

// Column (d), worked by hand: C is 6,000 ÷ 94,500. F is $54,000 × 1. H is
// 54,000 ÷ 100,000 = 0.54. J is $5,500 × 0.54 = $2,970. K is $51,030. L is
// $51,030 × C = $3,240. N is 51,030 ÷ 94,500 = 0.54. O is $153 × 0.54 =
// $82.62. P is $3,157.38. Line 13 is $100 × 0.54 = $54, so line 14c is
// $3,103.38.
func TestWorksheetIColumnByHand(t *testing.T) {
	w := must(californiataxcalc.WorksheetI(3, column(t, 54_000*money.Dollar, 100_000*money.Dollar)))
	for _, c := range []struct {
		line      string
		got, want money.USCents
	}{
		{"F", w.F(), 54_000 * money.Dollar},
		{"J", w.J(), 2_970 * money.Dollar},
		{"K", w.K(), 51_030 * money.Dollar},
		{"L", w.L(), 3_240 * money.Dollar},
		{"O", w.O(), 8_262},
		{"P", w.P(), 315_738},
		{"13", w.Line13(), 54 * money.Dollar},
		{"14c", w.Line14c(), 310_338},
	} {
		if c.got != c.want {
			t.Errorf("line %s is %v, want %v", c.line, c.got, c.want)
		}
	}
	if h, _ := w.RateH().Float64(); h != 0.54 {
		t.Errorf("line H is %v, want 0.54", h)
	}
	// Column (a) annualises by 4: $25,000 is $100,000, the whole total AGI,
	// so H is 1 and nothing is prorated away.
	a := must(californiataxcalc.WorksheetI(0, column(t, 25_000*money.Dollar, 100_000*money.Dollar)))
	if a.F() != 100_000*money.Dollar || a.J() != 5_500*money.Dollar || a.P() != 584_700 {
		t.Errorf("column (a): F %v, J %v, P %v", a.F(), a.J(), a.P())
	}
	// H is capped at 1.0000 when California AGI exceeds total AGI.
	over := must(californiataxcalc.WorksheetI(3, column(t, 120_000*money.Dollar, 100_000*money.Dollar)))
	if h, _ := over.RateH().Float64(); h != 1 || over.J() != 5_500*money.Dollar {
		t.Errorf("H over 1: %v, J %v", h, over.J())
	}
	if _, err := californiataxcalc.WorksheetI(4, column(t, 1, 1)); err == nil {
		t.Error("a fifth column was worked")
	}
	if _, err := californiataxcalc.NewWorksheetIColumn().TaxOnTotal(1).Build(); err == nil {
		t.Error("a column with no total taxable income was built")
	}
}

// moved is a single filer who lived in California until Jul 14, 2026 and in
// Oregon from Jul 15, with $3,200 of Form 540NR tax and $5,000 of 2025
// tax as a full-year resident.
func moved(t *testing.T, annualised bool, columns ...californiataxcalc.WorksheetIColumn) estimated.Facts {
	t.Helper()
	b := estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(3_200*money.Dollar).StateAGI(54_000*money.Dollar).Build())).
		PriorYear(*prior(5_000*money.Dollar, 90_000*money.Dollar, 0)).
		Residence(
			must(estimated.NewResidencePeriod("CA").From(day(2026, 1, 1)).To(day(2026, 7, 14)).Build()),
			must(estimated.NewResidencePeriod("OR").From(day(2026, 7, 15)).To(day(2026, 12, 31)).Build()))
	if annualised {
		for _, e := range []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)} {
			b.Periods(must(estimated.NewPeriod().End(e).AnnualisedTax(6_000 * money.Dollar).Build()))
		}
		b.Elections(must(estimated.NewElections().Annualised().Build()))
	}
	if len(columns) > 0 {
		b.State(must(californiataxcalc.NewEstimatedFacts().WorksheetI(columns...).Build()))
	}
	return must(b.Build())
}

// The regular method takes the Form 540NR tax as it is: the target is the
// smaller of 90% of $3,200 and 100% of last year's $5,000, $2,880, due 30%,
// 40%, 0% and 30%. FTB 5805 prorates nothing else, so January still needs its
// 30%.
func TestEstimatedPartYearResidentRegularMethod(t *testing.T) {
	sched := must(rules(t).Schedule(moved(t, false))).Value()
	for i, w := range []money.USCents{86_400, 201_600, 201_600, 288_000} {
		if sched[i].NeededBy() != w {
			t.Errorf("installment %d needs %v, want %v", i+1, sched[i].NeededBy(), w)
		}
	}
}

// With the annualised method, Worksheet I prorates each period's tax. The
// columns' lines 14c, worked as column (d) is above, are $5,747.00,
// $5,747.05, $4,685.18 and $3,103.38. At 27%, 63%, 63% and 90%, against the
// regular $864, $1,152, $0 and $864:
//   - April: 27% of $5,747.00 is $1,551.69, more than $864, so $864.
//   - June: 63% of $5,747.05 is $3,620.64, less $864 is $2,756.64, more than
//     $1,152, so $1,152.
//   - September: nothing is regular, so $0.
//   - January: 90% of $3,103.38 is $2,793.04, less $2,016 is $777.04, less
//     than $864, so $777.04.
func TestEstimatedPartYearResidentWorksheetI(t *testing.T) {
	cols := []californiataxcalc.WorksheetIColumn{
		column(t, 25_000*money.Dollar, 100_000*money.Dollar),
		column(t, 41_667*money.Dollar, 100_000*money.Dollar),
		column(t, 54_000*money.Dollar, 112_500*money.Dollar),
		column(t, 54_000*money.Dollar, 100_000*money.Dollar),
	}
	res := must(rules(t).Schedule(moved(t, true, cols...)))
	for i, w := range []money.USCents{86_400, 201_600, 201_600, 279_304} {
		if got := res.Value()[i].NeededBy(); got != w {
			t.Errorf("installment %d needs %v, want %v", i+1, got, w)
		}
	}
	if !has(res.Required(), working.PositionUnsettledRule) {
		t.Errorf("the rounding Worksheet I doesn't state is not reported: %v", res.Required())
	}

	// Without the columns, the period figures stand and they are asked for.
	without := must(rules(t).Schedule(moved(t, true)))
	if !has(without.Required(), working.PositionJurisdictionFact) {
		t.Errorf("the Worksheet I figures are not asked for: %v", without.Required())
	}

	// Worksheet I is for a Form 540NR filer, not a full-year resident.
	f := must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).
		ThisYear(must(estimated.NewFigures().Tax(3_200 * money.Dollar).StateAGI(54_000 * money.Dollar).Build())).
		Residence(must(estimated.NewResidencePeriod("CA").From(day(2026, 1, 1)).To(day(2026, 12, 31)).Build())).
		State(must(californiataxcalc.NewEstimatedFacts().WorksheetI(cols...).Build())).Build())
	if _, err := rules(t).Requirement(f); err == nil {
		t.Error("Worksheet I was accepted for a full-year resident")
	}
	if _, err := californiataxcalc.NewEstimatedFacts().WorksheetI(cols[:2]...).Build(); err == nil {
		t.Error("two Worksheet I columns were accepted")
	}
}

// A part-year resident on the regular method is told plainly what FTB 5805
// does with a move: last year's tax isn't reduced, January still applies,
// and the annualised method, with Worksheet I, is asked about.
func TestEstimatedPartYearResidentIsToldWhatAMoveChanges(t *testing.T) {
	facts := func(residence []estimated.ResidencePeriod, annualised bool) estimated.Facts {
		b := estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).
			ThisYear(must(estimated.NewFigures().StateAGI(54_000 * money.Dollar).Build())).
			PriorYear(*prior(8_412*money.Dollar, 90_000*money.Dollar, 0)).
			Withheld(must(estimated.NewWithheld().On(day(2026, 7, 14)).Amount(5_129 * money.Dollar).Build())).
			Residence(residence...)
		if annualised {
			for _, e := range []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)} {
				b.Periods(must(estimated.NewPeriod().End(e).AnnualisedTax(6_000 * money.Dollar).Build()))
			}
			b.Elections(must(estimated.NewElections().Annualised().Build()))
		}
		return must(b.Build())
	}
	movedOut := []estimated.ResidencePeriod{
		must(estimated.NewResidencePeriod("CA").From(day(2026, 1, 1)).To(day(2026, 7, 14)).Build()),
		must(estimated.NewResidencePeriod("OR").From(day(2026, 7, 15)).To(day(2026, 12, 31)).Build()),
	}
	allYear := []estimated.ResidencePeriod{must(estimated.NewResidencePeriod("CA").From(day(2026, 1, 1)).To(day(2026, 12, 31)).Build())}
	const (
		target   = "Your target is 100% of your 2025 California tax, $8,412."
		reduced  = "California doesn't reduce last year's tax for the months you lived elsewhere."
		january  = " The January payment still counts after a move."
		question = "Did your income come unevenly through the year?"
		effect   = "If so, the annualised method with FTB 5805 Worksheet I can lower your payments."
	)
	uneven := func(rs []working.Required) bool {
		for _, r := range rs {
			if r.Position() == working.PositionJurisdictionFact && r.Question() == question && r.Effect() == effect {
				return true
			}
		}
		return false
	}
	f := facts(movedOut, false)
	req := must(rules(t).Requirement(f))
	if why := req.Value().Why(); !strings.Contains(why, target) || !strings.HasSuffix(why, reduced) {
		t.Errorf("the reason is %q", why)
	}
	if !uneven(req.Required()) {
		t.Errorf("the annualised method is not asked about: %v", req.Required())
	}
	pen := must(rules(t).Penalty(f, day(2027, 12, 31)))
	if reason := pen.Value().Reason(); reason != "Payments fall short on four due dates."+january {
		t.Errorf("the penalty's reason is %q", reason)
	}
	if !uneven(pen.Required()) {
		t.Errorf("the penalty doesn't ask about the annualised method: %v", pen.Required())
	}

	// A full-year resident, and a part-year resident on the annualised
	// method, are not told it.
	for name, f := range map[string]estimated.Facts{"full year": facts(allYear, false), "annualised": facts(movedOut, true)} {
		req := must(rules(t).Requirement(f))
		if why := req.Value().Why(); strings.Contains(why, reduced) {
			t.Errorf("%s: the reason is %q", name, why)
		}
		if uneven(req.Required()) {
			t.Errorf("%s: the annualised method is asked about", name)
		}
		if reason := must(rules(t).Penalty(f, day(2027, 12, 31))).Value().Reason(); strings.Contains(reason, "after a move") {
			t.Errorf("%s: the penalty's reason is %q", name, reason)
		}
	}
}
