package golden

// Each answer's worksheet holds the figures its core came from, so the two
// can't disagree. docs/estimated-tax-design.md, "Verification", item 3.

import (
	"fmt"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	formpitund "github.com/nexteggapp/taxprotos/go/us/state/de/v1/form_pit_und"
	formn210 "github.com/nexteggapp/taxprotos/go/us/state/hi/v1/form_n_210"
	formil2210 "github.com/nexteggapp/taxprotos/go/us/state/il/v1/form_il_2210"
)

// worksheetColumn is one due date's column of a worksheet: what the form
// requires on that date, and the underpayment it finds.
type worksheetColumn struct {
	required, underpayment *commonv1.Money
}

// worksheetTotals are the figures every jurisdiction's worksheet shares with
// the answer's core, wherever its form prints them.
type worksheetTotals struct {
	// form names the worksheet, for a message.
	form string
	// required is the required annual payment, and penalty the penalty.
	required, penalty *commonv1.Money
	columns           []worksheetColumn
	// problems are where the form disagrees with itself.
	problems []string
	// penaltyApart says the form's penalty leaves out part of the answer's,
	// so the two aren't compared.
	penaltyApart bool
}

// worksheetProblems says where an answer's worksheet disagrees with the
// answer's core: the required annual payment with the target, each
// column's required instalment with the instalment's own amount, its
// underpayment with the instalment's shortfall, and the penalty with the
// penalty's total. An answer with no target has no worksheet. A required
// instalment of nothing in the last column is the January instalment a
// return filed early excuses (26 U.S.C. § 6654(h)).
func worksheetProblems(in *returnsv1.ReturnInputs, a *returnsv1.EstimatedAnswer) []string {
	var totals *worksheetTotals
	switch w := a.GetWorksheet().(type) {
	case *returnsv1.EstimatedAnswer_Us:
		totals = form2210Totals(w.Us)
	case *returnsv1.EstimatedAnswer_Pattern:
		totals = form2210Totals(w.Pattern.GetForm_2210())
	case *returnsv1.EstimatedAnswer_De:
		totals = pitUndTotals(w.De)
	case *returnsv1.EstimatedAnswer_Hi:
		totals = n210Totals(w.Hi)
	case *returnsv1.EstimatedAnswer_Il:
		totals = il2210Totals(in, w.Il)
	}
	computed := a.GetOutcome() == returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED &&
		a.GetRequirement().GetStatus() != returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN
	switch {
	case !computed && a.GetWorksheet() != nil:
		return []string{"a worksheet without a target"}
	case !computed:
		return nil
	case totals == nil:
		return []string{"no worksheet"}
	}
	out := append([]string(nil), totals.problems...)
	differ := func(what string, got, want *commonv1.Money) {
		if got.GetMinor() != want.GetMinor() {
			out = append(out, fmt.Sprintf("%s %s is %d, want %d", totals.form, what, got.GetMinor(), want.GetMinor()))
		}
	}
	differ("required annual payment", totals.required, a.GetRequirement().GetTarget())
	if !totals.penaltyApart {
		differ("penalty", totals.penalty, a.GetPenalty().GetTotal())
	}
	cols, insts := totals.columns, a.GetInstallments()
	if len(cols) != len(insts) {
		return append(out, fmt.Sprintf("%s has %d columns for %d instalments", totals.form, len(cols), len(insts)))
	}
	var before int64
	for i, inst := range insts {
		own := inst.GetNeededBy().GetMinor() - before
		before = inst.GetNeededBy().GetMinor()
		required := cols[i].required.GetMinor()
		excused := i == len(insts)-1 && required == 0 && in.GetEstimatedTax().GetReturnFiled() != nil
		if required != own && !excused {
			out = append(out, fmt.Sprintf("%s column %d required instalment is %d, want %d", totals.form, i+1, required, own))
		}
		differ(fmt.Sprintf("column %d underpayment", i+1), cols[i].underpayment, inst.GetShortfall())
	}
	return out
}

// form2210Totals reads Form 2210, federal or a state's that follows it: the
// required annual payment on line 9, each column's lines 10 and 17, and the
// penalty on line 19.
func form2210Totals(f *form1040.Form2210) *worksheetTotals {
	if f == nil {
		return nil
	}
	t := &worksheetTotals{form: "Form 2210", required: f.GetRequiredAnnualPayment(), penalty: f.GetEstimatedTaxPenalty()}
	for _, p := range f.GetPaymentPeriods() {
		t.columns = append(t.columns, worksheetColumn{required: p.GetRequiredInstallment(), underpayment: p.GetUnderpayment()})
	}
	return t
}

// pitUndTotals reads Delaware's Form PIT-UND: the required annual amount on
// line C, each column's lines 28 and 37, and the total penalty on line 43,
// which must also be line 42 of every column added.
func pitUndTotals(f *formpitund.FormPitUnd) *worksheetTotals {
	if f == nil {
		return nil
	}
	t := &worksheetTotals{form: "PIT-UND", required: f.GetRequiredAnnualPayment(), penalty: f.GetTotalPenalty()}
	var columns int64
	for _, p := range f.GetPaymentPeriods() {
		t.columns = append(t.columns, worksheetColumn{required: p.GetRequiredInstallment(), underpayment: p.GetUnderpayment()})
		columns += p.GetPenalty().GetMinor()
	}
	if columns != f.GetTotalPenalty().GetMinor() {
		t.problems = append(t.problems, fmt.Sprintf("PIT-UND line 43 is %d, and line 42 added is %d", f.GetTotalPenalty().GetMinor(), columns))
	}
	return t
}

// n210Totals reads Hawaii's Form N-210: the required annual payment on line
// 8, each column's lines 9 and 16, and the penalty on line 21, which must
// also be line 20 of every column added.
func n210Totals(f *formn210.FormN210) *worksheetTotals {
	if f == nil {
		return nil
	}
	t := &worksheetTotals{form: "N-210", required: f.GetRequiredAnnualPayment(), penalty: f.GetPenalty()}
	var columns int64
	for _, p := range f.GetPaymentPeriods() {
		t.columns = append(t.columns, worksheetColumn{required: p.GetRequiredInstallment(), underpayment: p.GetUnderpayment()})
		columns += p.GetPenalty().GetMinor()
	}
	if columns != f.GetPenalty().GetMinor() {
		t.problems = append(t.problems, fmt.Sprintf("N-210 line 21 is %d, and line 20 added is %d", f.GetPenalty().GetMinor(), columns))
	}
	return t
}

// il2210Totals reads Illinois's Form IL-2210: the required annual payment on
// line 7, each quarter's line 9b, and the penalty on line 22, which must
// also be column I of Penalty Worksheet 1 added. A quarter's underpayment is
// its unpaid amount, line 13 when positive, less what Penalty Worksheet 1
// applies to it from payments made by its due date. Line 22 is the form's
// flat penalty alone, so it is the penalty's total only when no interest is
// asked for on top of it.
func il2210Totals(in *returnsv1.ReturnInputs, f *formil2210.FormIl2210) *worksheetTotals {
	if f == nil {
		return nil
	}
	t := &worksheetTotals{form: "IL-2210", required: f.GetRequiredAnnualPayment(), penalty: f.GetUnderpaymentPenalty()}
	for _, s := range in.GetEstimatedTax().GetStateInputs() {
		if s.GetIl().GetLateInstallmentInterest() == returnsv1.IllinoisEstimatedInputs_LATE_INSTALLMENT_INTEREST_CHARGED {
			t.penaltyApart = true
		}
	}
	paid := map[int32]int64{}
	var columnI int64
	for _, r := range f.GetUnderpaymentRows() {
		columnI += r.GetPenalty().GetMinor()
		if dateKey(r.GetPaymentDate()) <= dateKey(r.GetDueDate()) {
			paid[r.GetPeriod()] += r.GetPaymentApplied().GetMinor()
		}
	}
	if columnI != f.GetUnderpaymentPenalty().GetMinor() {
		t.problems = append(t.problems, fmt.Sprintf("IL-2210 line 22 is %d, and column I added is %d", f.GetUnderpaymentPenalty().GetMinor(), columnI))
	}
	for _, q := range f.GetQuarters() {
		unpaid := max(q.GetUnpaidInstallment().GetMinor(), 0)
		t.columns = append(t.columns, worksheetColumn{
			required:     q.GetRequiredInstallment(),
			underpayment: &commonv1.Money{Minor: unpaid - paid[q.GetQuarter()]},
		})
	}
	return t
}

// dateKey orders a date as a number, yyyymmdd.
func dateKey(d *commonv1.Date) int64 {
	return int64(d.GetYear())*10000 + int64(d.GetMonth())*100 + int64(d.GetDay())
}
