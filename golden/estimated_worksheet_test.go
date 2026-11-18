package golden

// Each answer's worksheet holds the figures its core came from, so the two
// can't disagree. docs/estimated-tax.md, "Verification".

import (
	"fmt"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// worksheetLines are the lines every jurisdiction's underpayment form has,
// whatever it numbers them: the required annual payment, each due date's
// required instalment and underpayment, and the penalty.
type worksheetLines struct {
	target, penalty *commonv1.Money
	columns         []worksheetColumn
	// cumulative is set for a form whose underpayment line is the running
	// total short by the due date, what is needed by it less what is
	// credited by it, rather than the instalment's own shortfall.
	cumulative bool
	// problems are where the form disagrees with itself.
	problems []string
	// penaltyApart says the form's penalty leaves out part of the answer's,
	// so the two aren't compared.
	penaltyApart bool
}

// worksheetColumn is one due date's column.
type worksheetColumn struct {
	required, underpayment *commonv1.Money
}

// form2210Lines are Form 2210's, federal or a state's that follows it: lines
// 9 and 19, and lines 10 and 17 of each column.
func form2210Lines(form *form1040.Form2210) *worksheetLines {
	if form == nil {
		return nil
	}
	w := &worksheetLines{target: form.GetRequiredAnnualPayment(), penalty: form.GetEstimatedTaxPenalty()}
	for _, c := range form.GetPaymentPeriods() {
		w.columns = append(w.columns, worksheetColumn{c.GetRequiredInstallment(), c.GetUnderpayment()})
	}
	return w
}

// worksheetOf is the answer's worksheet as the lines every form has, or nil
// for none. Each jurisdiction's own form is one case.
func worksheetOf(in *returnsv1.ReturnInputs, a *returnsv1.EstimatedAnswer) *worksheetLines {
	switch w := a.GetWorksheet().(type) {
	case *returnsv1.EstimatedAnswer_Us:
		return form2210Lines(w.Us)
	case *returnsv1.EstimatedAnswer_Pattern:
		return form2210Lines(w.Pattern.GetForm_2210())
	case *returnsv1.EstimatedAnswer_Ar:
		return ar2210Lines(w.Ar)
	case *returnsv1.EstimatedAnswer_ArAnnualized:
		return ar2210aLines(w.ArAnnualized)
	case *returnsv1.EstimatedAnswer_Az:
		return form221Lines(w.Az)
	case *returnsv1.EstimatedAnswer_Ca:
		return ftb5805Lines(w.Ca)
	case *returnsv1.EstimatedAnswer_Ct:
		return ct2210Lines(w.Ct)
	case *returnsv1.EstimatedAnswer_Dc:
		return d2210Lines(w.Dc)
	case *returnsv1.EstimatedAnswer_De:
		return pitUndLines(w.De)
	case *returnsv1.EstimatedAnswer_Ga:
		return uetLines(w.Ga)
	case *returnsv1.EstimatedAnswer_Hi:
		return n210Lines(w.Hi)
	case *returnsv1.EstimatedAnswer_Il:
		return il2210Lines(in, w.Il)
	case *returnsv1.EstimatedAnswer_In:
		return it2210Lines(w.In)
	case *returnsv1.EstimatedAnswer_InAnnualized:
		return it2210aLines(w.InAnnualized)
	case *returnsv1.EstimatedAnswer_La:
		return r210rLines(w.La)
	case *returnsv1.EstimatedAnswer_Ma:
		return m2210Lines(w.Ma)
	case *returnsv1.EstimatedAnswer_Mo:
		return mo2210Lines(w.Mo)
	case *returnsv1.EstimatedAnswer_Ms:
		return ms80320Lines(w.Ms)
	case *returnsv1.EstimatedAnswer_Nd:
		return nd1utLines(w.Nd)
	case *returnsv1.EstimatedAnswer_Md:
		return form502upLines(w.Md)
	case *returnsv1.EstimatedAnswer_Me:
		return form2210meLines(w.Me)
	case *returnsv1.EstimatedAnswer_Mi:
		return mi2210Lines(w.Mi)
	case *returnsv1.EstimatedAnswer_Pa:
		return rev1630Lines(in, w.Pa)
	case *returnsv1.EstimatedAnswer_Ut:
		return tc546Lines(w.Ut)
	case *returnsv1.EstimatedAnswer_Va:
		return form760cLines(w.Va)
	case *returnsv1.EstimatedAnswer_Vt:
		return in152Lines(w.Vt)
	case *returnsv1.EstimatedAnswer_VtAnnualized:
		return in152aLines(w.VtAnnualized)
	case *returnsv1.EstimatedAnswer_Wa:
		return capitalGainsPaymentLines(w.Wa)
	case *returnsv1.EstimatedAnswer_Wi:
		return scheduleULines(w.Wi)
	case *returnsv1.EstimatedAnswer_Nj:
		return nj2210Lines(w.Nj)
	case *returnsv1.EstimatedAnswer_Ny:
		return it21059Lines(w.Ny)
	case *returnsv1.EstimatedAnswer_Oh:
		return itsd2210Lines(w.Oh)
	}
	return nil
}

// withoutForm are the jurisdictions with nothing to estimate, and so no
// underpayment form: the eight with no individual income tax, and Idaho,
// which asks no estimates of individuals.
var withoutForm = map[string]bool{
	"AK": true, "FL": true, "ID": true, "NH": true, "NV": true, "SD": true, "TN": true, "TX": true, "WY": true,
}

// worksheetProblems says where an answer's worksheet disagrees with the
// answer's core: the required annual payment with the target, each column's
// required instalment with the instalment's own amount, its underpayment
// with the instalment's shortfall, and the penalty with the penalty's total.
// An answer with no target has no worksheet. A required instalment of
// nothing in the last column is the January instalment a return filed early
// excuses (26 U.S.C. § 6654(h)). A jurisdiction with no form has no
// worksheet.
func worksheetProblems(in *returnsv1.ReturnInputs, a *returnsv1.EstimatedAnswer) []string {
	form := worksheetOf(in, a)
	computed := a.GetOutcome() == returnsv1.EstimatedOutcome_ESTIMATED_OUTCOME_COMPUTED &&
		a.GetRequirement().GetStatus() != returnsv1.RequirementStatus_REQUIREMENT_STATUS_UNKNOWN
	switch {
	case !computed && a.GetWorksheet() != nil:
		return []string{"a worksheet without a target"}
	case !computed:
		return nil
	case withoutForm[a.GetJurisdiction()]:
		if a.GetWorksheet() != nil {
			return []string{"a worksheet for a jurisdiction with no form"}
		}
		return nil
	case form == nil:
		return []string{"no worksheet"}
	}
	out := append([]string(nil), form.problems...)
	differ := func(what string, got, want *commonv1.Money) {
		if got.GetMinor() != want.GetMinor() {
			out = append(out, fmt.Sprintf("%s is %d, want %d", what, got.GetMinor(), want.GetMinor()))
		}
	}
	if form.target != nil {
		// A form with no line for it, such as Arkansas's AR2210A, leaves it
		// nil.
		differ("required annual payment", form.target, a.GetRequirement().GetTarget())
	}
	if !form.penaltyApart {
		differ("penalty", form.penalty, a.GetPenalty().GetTotal())
	}
	cols, insts := form.columns, a.GetInstallments()
	if len(cols) != len(insts) {
		return append(out, fmt.Sprintf("%d columns for %d instalments", len(cols), len(insts)))
	}
	var before int64
	for i, inst := range insts {
		own := inst.GetNeededBy().GetMinor() - before
		before = inst.GetNeededBy().GetMinor()
		required := cols[i].required.GetMinor()
		excused := i == len(insts)-1 && required == 0 && in.GetEstimatedTax().GetReturnFiled() != nil
		if required != own && !excused {
			out = append(out, fmt.Sprintf("column %d required instalment is %d, want %d", i+1, required, own))
		}
		if excused && form.cumulative {
			// A running underpayment carries the excused instalment's
			// share, which the instalment's own figures no longer do.
			continue
		}
		want := inst.GetShortfall()
		if form.cumulative {
			want = &commonv1.Money{Minor: max(0, inst.GetNeededBy().GetMinor()-inst.GetCreditedBy().GetMinor())}
		}
		differ(fmt.Sprintf("column %d underpayment", i+1), cols[i].underpayment, want)
	}
	return out
}
