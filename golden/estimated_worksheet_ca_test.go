package golden

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/californiataxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/estimated/estimatedtest"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	formftb5805 "github.com/nexteggapp/taxprotos/go/us/state/ca/v1/form_ftb_5805"
)

// ftb5805Lines are California's Form FTB 5805: Part II line 6 and line 13,
// and Worksheet II lines 1 and 8 of each column.
func ftb5805Lines(form *formftb5805.FormFtb5805) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetEstimatedTaxPenalty()}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	return w
}

// No household in the vectors gives Worksheet I, so this one does: the part-
// year resident of californiataxcalc's TestEstimatedPartYearResidentWorksheetI,
// annualising with four columns. The taxlisp answer must match the Go rules'
// instalments, shortfalls and penalty, and its FTB 5805 must hold the
// columns that test works by hand: column (d)'s line P of $3,157.38 and line
// 14c of $3,103.38.
func TestCaliforniaWorksheetI(t *testing.T) {
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	day := estimated.MustDate
	column := func(caAGI, totalAGI money.USCents) californiataxcalc.WorksheetIColumn {
		c, err := californiataxcalc.NewWorksheetIColumn().TaxOnTotal(6_000 * money.Dollar).TotalTaxable(94_500 * money.Dollar).
			CaliforniaAGI(caAGI).AnnualisedTotalAGI(totalAGI).Deduction(5_500 * money.Dollar).ExemptionCredits(153 * money.Dollar).
			ProratedCredits(100 * money.Dollar).Build()
		must(err)
		return c
	}
	state, err := californiataxcalc.NewEstimatedFacts().WorksheetI(
		column(25_000*money.Dollar, 100_000*money.Dollar),
		column(41_667*money.Dollar, 100_000*money.Dollar),
		column(54_000*money.Dollar, 112_500*money.Dollar),
		column(54_000*money.Dollar, 100_000*money.Dollar)).Build()
	must(err)
	this, err := estimated.NewFigures().Tax(3_200 * money.Dollar).StateAGI(54_000 * money.Dollar).Build()
	must(err)
	last, err := estimated.NewFigures().Tax(5_000 * money.Dollar).StateAGI(90_000 * money.Dollar).Withheld(0).Build()
	must(err)
	prior, err := estimated.NewPriorReturn().Filed(true).Figures(last).Build()
	must(err)
	inCA, err := estimated.NewResidencePeriod("CA").From(day(2026, 1, 1)).To(day(2026, 7, 14)).Build()
	must(err)
	inOR, err := estimated.NewResidencePeriod("OR").From(day(2026, 7, 15)).To(day(2026, 12, 31)).Build()
	must(err)
	elections, err := estimated.NewElections().Annualised().Build()
	must(err)
	b := estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).PriorYear(prior).
		Residence(inCA, inOR).Elections(elections).State(state)
	for _, end := range []estimated.Date{day(2026, 3, 31), day(2026, 5, 31), day(2026, 8, 31), day(2026, 12, 31)} {
		p, err := estimated.NewPeriod().End(end).AnnualisedTax(6_000 * money.Dollar).Build()
		must(err)
		b.Periods(p)
	}
	facts, err := b.Build()
	must(err)

	rules, err := californiataxcalc.EstimatedTax(2026)
	must(err)
	sched, err := rules.Schedule(facts)
	must(err)
	pen, err := rules.Penalty(facts, estimatedtest.PenaltyAsOf)
	must(err)

	in, unmapped := returnInputs(facts, 2026)
	for path := range unmapped {
		if path != "this_year.federal_agi" && path != "elections.annualised" {
			t.Errorf("%s has no place in the inputs", path)
		}
	}
	eng, err := federal.DefaultEngine()
	must(err)
	out, err := eng.Prepare(in)
	must(err)
	a := out.GetEstimated()[0]
	insts := a.GetInstallments()
	if len(insts) != len(sched.Value()) {
		t.Fatalf("%d instalments, want %d", len(insts), len(sched.Value()))
	}
	for i, want := range sched.Value() {
		got := insts[i]
		if got.GetNeededBy().GetMinor() != int64(want.NeededBy()) || got.GetShortfall().GetMinor() != int64(want.Shortfall()) {
			t.Errorf("instalment %d needs %d short %d, want %v short %v", i+1,
				got.GetNeededBy().GetMinor(), got.GetShortfall().GetMinor(), want.NeededBy(), want.Shortfall())
		}
	}
	if got, want := a.GetPenalty().GetTotal().GetMinor(), int64(pen.Value().Total()); got != want {
		t.Errorf("penalty %d, want %d", got, want)
	}
	unsettled := false
	for _, q := range a.GetQuestions() {
		unsettled = unsettled || q.GetPosition() == "unsettled rule"
	}
	if !unsettled {
		t.Errorf("the rounding Worksheet I doesn't state is not reported: %v", a.GetQuestions())
	}
	for _, p := range worksheetProblems(in, a) {
		t.Errorf("the worksheet's %s", p)
	}
	form := a.GetCa()
	if n := len(form.GetProratedTaxPeriods()); n != 4 {
		t.Fatalf("%d Worksheet I columns, want 4", n)
	}
	d := form.GetProratedTaxPeriods()[3]
	if d.GetTaxAfterExemptionCredits().GetMinor() != 315_738 || d.GetDeductionRate().GetUnits() != 54 || d.GetDeductionRate().GetScale() != 2 {
		t.Errorf("column (d) of Worksheet I: P %d, H %v", d.GetTaxAfterExemptionCredits().GetMinor(), d.GetDeductionRate())
	}
	if got := form.GetAnnualizationPeriods()[3].GetTotalTax().GetMinor(); got != 310_338 {
		t.Errorf("column (d) of Part III: line 14c is %d, want 310338", got)
	}
}
