package golden

import (
	"os"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	formftb5805 "github.com/nexteggapp/taxprotos/go/us/state/ca/v1/form_ftb_5805"
	"google.golang.org/protobuf/encoding/protojson"
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
// year resident of the Go calculator's Worksheet I test, annualising with
// four columns, in testdata/estimated/ca_worksheet_i.json. The taxlisp
// answer must match the instalments, shortfalls and penalty the Go rules
// gave it, frozen in caWorksheetIWant, and its FTB 5805 must hold the
// columns that test worked by hand: column (d)'s line P of $3,157.38 and
// line 14c of $3,103.38.
func TestCaliforniaWorksheetI(t *testing.T) {
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	src, err := os.ReadFile("testdata/estimated/ca_worksheet_i.json")
	must(err)
	in := &returnsv1.ReturnInputs{}
	must(protojson.Unmarshal(src, in))
	eng, err := federal.DefaultEngine()
	must(err)
	out, err := eng.Prepare(in)
	must(err)
	a := out.GetEstimated()[0]
	insts := a.GetInstallments()
	goAnswer := caWorksheetIWant
	if len(insts) != len(goAnswer.instalments) {
		t.Fatalf("%d instalments, want %d", len(insts), len(goAnswer.instalments))
	}
	for i, w := range goAnswer.instalments {
		got := insts[i]
		if got.GetNeededBy().GetMinor() != w.neededBy || got.GetShortfall().GetMinor() != w.shortfall {
			t.Errorf("instalment %d needs %d short %d, want %d short %d", i+1,
				got.GetNeededBy().GetMinor(), got.GetShortfall().GetMinor(), w.neededBy, w.shortfall)
		}
	}
	if got, want := a.GetPenalty().GetTotal().GetMinor(), goAnswer.penalty; got != want {
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

// caWorksheetIWant is what the Go rules answered for the Worksheet I
// household: each instalment's cumulative amount needed and its shortfall,
// and the penalty, in cents.
var caWorksheetIWant = struct {
	instalments []struct{ neededBy, shortfall int64 }
	penalty     int64
}{
	instalments: []struct{ neededBy, shortfall int64 }{
		{86_400, 86_400}, {201_600, 115_200}, {201_600, 0}, {279_304, 77_704},
	},
	penalty: 8_704,
}
