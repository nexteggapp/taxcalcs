package main

import (
	"fmt"
	"math/rand"
	"strings"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1099int "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_int"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	formssa1099 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_ssa_1099"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// jurisdictions is every state and DC.
var jurisdictions = []string{
	"AK", "AL", "AR", "AZ", "CA", "CO", "CT", "DC", "DE", "FL", "GA", "HI", "IA", "ID", "IL", "IN", "KS",
	"KY", "LA", "MA", "MD", "ME", "MI", "MN", "MO", "MS", "MT", "NC", "ND", "NE", "NH", "NJ", "NM", "NV",
	"NY", "OH", "OK", "OR", "PA", "RI", "SC", "SD", "TN", "TX", "UT", "VA", "VT", "WA", "WI", "WV", "WY",
}

// noWageTax are the states that tax no wages; their state figures are not
// compared.
var noWageTax = map[string]bool{
	"AK": true, "FL": true, "NH": true, "NV": true, "SD": true, "TN": true, "TX": true, "WA": true, "WY": true,
}

// Person is one adult of a generated household. Amounts are whole dollars.
type Person struct {
	Age                                      int
	Wages, Interest, Pension, SocialSecurity int64
}

// Household is a generated household: one adult filing single, or a married
// couple filing jointly, with no dependants, resident all year in State.
type Household struct {
	ID     string
	State  string
	People []Person
}

// generate makes per households for each jurisdiction: workers with wages
// and some interest, and retirees with a pension and Social Security, single
// and married, over a spread of incomes.
func generate(r *rand.Rand, per int) []Household {
	var hs []Household
	for _, st := range jurisdictions {
		for i := 0; i < per; i++ {
			joint := i%2 == 1
			retired := i%3 == 2
			n := 1
			if joint {
				n = 2
			}
			h := Household{ID: fmt.Sprintf("%s-%d", strings.ToLower(st), i), State: st}
			for j := 0; j < n; j++ {
				p := Person{Age: 25 + r.Intn(39)}
				if retired {
					p.Age = 66 + r.Intn(15)
					p.Pension = int64(r.Intn(60)) * 1000
					p.SocialSecurity = 12000 + int64(r.Intn(24))*1000
				} else {
					p.Wages = int64(r.Intn(250)) * 1000
				}
				if r.Intn(2) == 0 {
					p.Interest = int64(r.Intn(5000))
				}
				h.People = append(h.People, p)
			}
			hs = append(hs, h)
		}
	}
	return hs
}

// Describe is the household in a few words, for the report.
func (h Household) Describe() string {
	var parts []string
	for _, p := range h.People {
		s := fmt.Sprintf("age %d", p.Age)
		for _, f := range []struct {
			v    int64
			name string
		}{{p.Wages, "wages"}, {p.Interest, "interest"}, {p.Pension, "pension"}, {p.SocialSecurity, "Social Security"}} {
			if f.v > 0 {
				s += fmt.Sprintf(", %s $%d", f.name, f.v)
			}
		}
		parts = append(parts, s)
	}
	status := "single"
	if len(h.People) == 2 {
		status = "married filing jointly"
	}
	return fmt.Sprintf("%s, %s: %s", h.State, status, strings.Join(parts, "; "))
}

// theirInput is the household as the runner reads it.
func (h Household) theirInput() map[string]any {
	var people []map[string]any
	for _, p := range h.People {
		people = append(people, map[string]any{
			"age": p.Age, "wages": p.Wages, "interest": p.Interest,
			"pension": p.Pension, "social_security": p.SocialSecurity,
		})
	}
	return map[string]any{"id": h.ID, "year": 2025, "state": h.State, "joint": len(h.People) == 2, "people": people}
}

func usd(dollars int64) *commonv1.Money {
	return &commonv1.Money{Currency: "USD", Minor: dollars * 100}
}

func boolPtr(b bool) *bool { return &b }

// socialSecurityTax is W-2 box 4: 6.2% of wages up to the 2025 wage base of
// $176,100, to the cent.
func socialSecurityTax(wages int64) *commonv1.Money {
	return &commonv1.Money{Currency: "USD", Minor: (min(wages, 176100)*100*62 + 500) / 1000}
}

// medicareTax is W-2 box 6: 1.45% of wages, plus the 0.9% Additional Medicare
// Tax the employer withholds on wages over $200,000, to the cent.
func medicareTax(wages int64) *commonv1.Money {
	cents := wages * 100 * 145
	if wages > 200000 {
		cents += (wages - 200000) * 100 * 90
	}
	return &commonv1.Money{Currency: "USD", Minor: (cents + 5000) / 10000}
}

// Inputs is the household as the taxcalcs preparer takes it.
func (h Household) Inputs() *returnsv1.ReturnInputs {
	hh := &returnsv1.Household{MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED}
	status := returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE
	if len(h.People) == 2 {
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		status = returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY
	}
	docs := &returnsv1.Documents{}
	for i, p := range h.People {
		id := fmt.Sprintf("p%d", i)
		person := &returnsv1.Person{
			Id:          id,
			FirstName:   id,
			DateOfBirth: &commonv1.Date{Year: int32(2025 - p.Age), Month: 6, Day: 1},
			Tin:         &returnsv1.TaxpayerID{Kind: returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN, ValidForEmployment: boolPtr(true)},
		}
		if i == 0 {
			hh.Taxpayer = person
		} else {
			hh.Spouse = person
		}
		if p.Wages > 0 {
			docs.W2S = append(docs.W2S, &returnsv1.W2Document{Id: "w2-" + id, Person: id, Form: &formw2.FormW2{
				WagesTipsOtherCompensation: usd(p.Wages),
				SocialSecurityWages:        usd(p.Wages),
				SocialSecurityTaxWithheld:  socialSecurityTax(p.Wages),
				MedicareWagesAndTips:       usd(p.Wages),
				MedicareTaxWithheld:        medicareTax(p.Wages),
				StateTaxWithholding:        []*commonv1.StateTaxWithholding{{State: h.State, StateIncome: usd(p.Wages)}},
			}})
		}
		if p.Interest > 0 {
			docs.Interest = append(docs.Interest, &returnsv1.Form1099INTDocument{Id: "int-" + id, Person: id, Form: &form1099int.Form1099INT{InterestIncome: usd(p.Interest)}})
		}
		if p.Pension > 0 {
			docs.RetirementDistributions = append(docs.RetirementDistributions, &returnsv1.Form1099RDocument{Id: "r-" + id, Person: id, Form: &form1099r.Form1099R{GrossDistribution: usd(p.Pension), TaxableAmount: usd(p.Pension)}})
		}
		if p.SocialSecurity > 0 {
			docs.SocialSecurity = append(docs.SocialSecurity, &returnsv1.FormSSA1099Document{Id: "ssa-" + id, Person: id, Form: &formssa1099.FormSSA1099{BenefitsPaid: usd(p.SocialSecurity), NetBenefits: usd(p.SocialSecurity)}})
		}
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: status},
			Documents: docs,
		},
		States: []*returnsv1.StateInputs{stateInputs(h.State)},
	}
}

// stateInputs asks for the state's return with its inputs message set but
// empty, which takes each default. California and Oregon have no default
// deduction method, so they are told the standard deduction.
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
