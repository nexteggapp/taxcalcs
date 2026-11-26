// Package properties_test holds the rules to relations every return must
// satisfy, over generated households, rather than to fixed figures: tax does
// not fall as wages rise, does not jump by more than a plausible marginal
// rate, and, federally, is the same for a married couple as for two single
// filers where the brackets double.
package properties_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// jurisdictions is every state and DC. The states without an income tax are
// included: their tax must stay zero, which is monotone.
var jurisdictions = []string{
	"AK", "AL", "AR", "AZ", "CA", "CO", "CT", "DC", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS",
	"KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV",
	"NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY",
}

func usd(c int64) *commonv1.Money { return &commonv1.Money{Currency: "USD", Minor: c} }

func boolPtr(b bool) *bool { return &b }

func person(id string) *returnsv1.Person {
	return &returnsv1.Person{
		Id:          id,
		FirstName:   id,
		DateOfBirth: &commonv1.Date{Year: 1985, Month: 6, Day: 1},
		Tin: &returnsv1.TaxpayerID{
			Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
			ValidForEmployment: boolPtr(true),
		},
	}
}

// household is a filer aged 40, or a couple of that age with the wages split
// evenly, with no dependants and wages only, resident in the given state.
func household(state string, joint bool, wages int64) *returnsv1.ReturnInputs {
	hh := &returnsv1.Household{MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED, Taxpayer: person("ana")}
	status := returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE
	shares := map[string]int64{"ana": wages}
	if joint {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("ben")
		status = returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY
		shares = map[string]int64{"ana": wages - wages/2, "ben": wages / 2}
	}
	docs := &returnsv1.Documents{}
	for _, id := range []string{"ana", "ben"} {
		w, ok := shares[id]
		if !ok {
			continue
		}
		docs.W2S = append(docs.W2S, &returnsv1.W2Document{Id: "w2_" + id, Person: id, Form: &formw2.FormW2{
			WagesTipsOtherCompensation: usd(w),
			SocialSecurityWages:        usd(w),
			MedicareWagesAndTips:       usd(w),
			StateTaxWithholding:        []*commonv1.StateTaxWithholding{{State: state, StateIncome: usd(w)}},
		}})
	}
	in := &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: docs,
		},
	}
	if state != "" {
		in.States = []*returnsv1.StateInputs{stateInputs(state)}
	}
	return in
}

// stateInputs asks for the state's return with its inputs message set but
// empty, which takes each default: the standard deduction, no adjustments.
// California and Oregon have no default deduction method, so they are told
// the standard deduction.
func stateInputs(state string) *returnsv1.StateInputs {
	si := &returnsv1.StateInputs{State: state}
	m := si.ProtoReflect()
	fd := m.Descriptor().Oneofs().ByName("inputs").Fields().ByName(protoreflect.Name(strings.ToLower(state)))
	if fd == nil {
		return si
	}
	msg := m.NewField(fd).Message()
	switch state {
	case "CA":
		msg = (&returnsv1.CaliforniaInputs{DeductionMethod: returnsv1.DeductionMethodElection_DEDUCTION_METHOD_ELECTION_STANDARD, FullYearHealthCoverage: boolPtr(true)}).ProtoReflect()
	case "OR":
		msg = (&returnsv1.OregonInputs{DeductionMethod: returnsv1.DeductionMethodElection_DEDUCTION_METHOD_ELECTION_STANDARD}).ProtoReflect()
	}
	m.Set(fd, protoreflect.ValueOfMessage(msg))
	return si
}

// stateTax is the total tax on a prepared state return's summary.
func stateTax(t *testing.T, out *returnsv1.ReturnOutputs) int64 {
	t.Helper()
	if len(out.GetStates()) != 1 {
		t.Fatalf("%d state returns, want 1", len(out.GetStates()))
	}
	m := out.GetStates()[0].ProtoReflect()
	od := m.Descriptor().Oneofs().ByName("return")
	fd := m.WhichOneof(od)
	if fd == nil {
		return 0
	}
	ret := m.Get(fd).Message()
	sum := ret.Descriptor().Fields().ByName(protoreflect.Name("summary"))
	if sum == nil {
		t.Fatalf("%s has no summary", ret.Descriptor().FullName())
	}
	s := ret.Get(sum).Message().Interface().(*returnsv1.Summary)
	return s.GetTotalTax().GetMinor()
}

const step int64 = 2_500_00

// fall is a place where the law lets tax fall as income rises, and by how
// much at most.
type fall struct {
	state string
	joint bool
	most  int64
	why   string
}

// falls are the drops the forms print. Each is cited; any other drop fails.
var falls = []fall{
	{"ND", true, 312_00, "the marriage penalty credit (Form ND-1 line 22) starts only when taxable income is over $81,036 and each spouse's qualified income is over $47,550, up to $312 (2025 North Dakota booklet, Marriage Penalty Credit Worksheet, lines 2, 5 and 13). Line 20 comes from the Tax Table and the worksheet from the rate schedule, so cents can remain."},
}

// allowedFall is how far tax may fall in one step for the state and status.
func allowedFall(state string, joint bool) int64 {
	for _, f := range falls {
		if f.state == state && f.joint == joint {
			return f.most
		}
	}
	return 0
}

// TestTaxDoesNotFallAsWagesRise prepares each jurisdiction's return at wages
// from $0 to $400,000 in steps of $2,500, single and married filing jointly,
// and requires each step's total tax to be no less than the last, and no more
// than half the extra wages above it. Half is far above any state's top rate
// and its recaptures; a step past it is a mistake, not a policy. The only
// drops allowed are those in falls.
func TestTaxDoesNotFallAsWagesRise(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range jurisdictions {
		for _, joint := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/joint=%v", st, joint), func(t *testing.T) {
				t.Parallel()
				prev := int64(-1)
				may := allowedFall(st, joint)
				for wages := int64(0); wages <= 400_000_00; wages += step {
					out, err := eng.Prepare(household(st, joint, wages))
					if err != nil {
						t.Fatalf("wages %d: %v", wages/100, err)
					}
					tax := stateTax(t, out)
					if prev >= 0 && tax < prev-may {
						t.Errorf("wages $%d: tax $%.2f, less than $%.2f at $%d", wages/100, float64(tax)/100, float64(prev)/100, (wages-step)/100)
					}
					if prev >= 0 && tax-prev > step/2 {
						t.Errorf("wages $%d: tax rose $%.2f on $%d more wages", wages/100, float64(tax-prev)/100, step/100)
					}
					prev = tax
				}
			})
		}
	}
}

// TestFederalTaxDoesNotFallAsWagesRise holds Form 1040's total tax (line 24)
// to the same rule, bounded by the 37% top rate plus the 0.9% Additional
// Medicare Tax, with a dollar for rounding.
func TestFederalTaxDoesNotFallAsWagesRise(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, joint := range []bool{false, true} {
		prev := int64(-1)
		for wages := int64(0); wages <= 1_000_000_00; wages += step {
			ret, err := eng.PrepareFederal(household("", joint, wages))
			if err != nil {
				t.Fatalf("wages %d: %v", wages/100, err)
			}
			tax := ret.GetForms().GetForm_1040().GetTotalTax().GetMinor()
			if prev >= 0 && tax < prev {
				t.Errorf("joint=%v wages $%d: tax $%.2f, less than $%.2f", joint, wages/100, float64(tax)/100, float64(prev)/100)
			}
			if max := step*379/1000 + 100; prev >= 0 && tax-prev > max {
				t.Errorf("joint=%v wages $%d: tax rose $%.2f on $%d more wages, more than 37.9%%", joint, wages/100, float64(tax-prev)/100, step/100)
			}
			prev = tax
		}
	}
}

// TestFederalJointBracketsDouble: for 2025 the married filing jointly
// brackets, and the standard deduction, are twice the single ones up to the
// top of the 32% bracket (single taxable income $250,525; Rev. Proc. 2024-40
// as amended). A couple with twice a single filer's wages has twice the
// taxable income, and so twice the Form 1040 line 16 tax, within a dollar of
// rounding. Taxable incomes start at $100,000, where the Tax Table, which
// rounds to its row's midpoint, gives way to the worksheet.
func TestFederalJointBracketsDouble(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	for wages := int64(116_000_00); wages <= 265_000_00; wages += 1_000_00 {
		single, err := eng.PrepareFederal(household("", false, wages))
		if err != nil {
			t.Fatal(err)
		}
		joint, err := eng.PrepareFederal(household("", true, 2*wages))
		if err != nil {
			t.Fatal(err)
		}
		s, j := single.GetForms().GetForm_1040(), joint.GetForms().GetForm_1040()
		if got, want := j.GetTaxableIncome().GetMinor(), 2*s.GetTaxableIncome().GetMinor(); got != want {
			t.Errorf("wages $%d: joint taxable income %d, want twice single, %d", wages/100, got, want)
			continue
		}
		if diff := j.GetTax().GetMinor() - 2*s.GetTax().GetMinor(); diff < -100 || diff > 100 {
			t.Errorf("wages $%d: joint line 16 $%.2f, single $%.2f: not twice within $1", wages/100,
				float64(j.GetTax().GetMinor())/100, float64(s.GetTax().GetMinor())/100)
		}
	}
}
