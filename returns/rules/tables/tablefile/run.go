package tablefile

import (
	"sort"
	"testing"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

// Figure returns the rules' figure, in cents, for taxable income of the
// given whole dollars and a filing status.
type Figure func(t *testing.T, status string, income int64) int64

// NewFigure makes a Figure for one subtest. Each filing status runs as a
// parallel subtest, and an engine is not shared between goroutines, so each
// subtest makes its own.
type NewFigure func(t *testing.T) Figure

// CheckTable probes every row of the table for every filing status, at the
// row's lowest and highest whole-dollar income, and requires the printed
// figure. name is the TSV's file name, as known gaps list it. It returns
// once every status is done.
func CheckTable(t *testing.T, name string, tab *Table, c *Checker, newFigure NewFigure) {
	t.Run(name, func(t *testing.T) {
		for _, status := range tab.Statuses {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				n := 0
				for _, r := range tab.Rows {
					want := r.Tax[status] * 100
					for _, income := range []int64{r.AtLeast, r.LessThan - 1} {
						c.Check(t, name, r.AtLeast, income, status, want, figure(t, status, income))
						n++
					}
				}
				t.Logf("%d rows, %d probes", len(tab.Rows), n)
			})
		}
	})
}

// CheckSchedule probes the schedule for every filing status at each
// bracket edge, one dollar either side of it, and the middle of each
// bracket, from the income from up. want is the test's own calculator of the
// printed figure, in cents. It returns once every status is done.
func CheckSchedule(t *testing.T, name string, s *Schedule, from int64, c *Checker, want func(b Bracket, income int64) int64, newFigure NewFigure) {
	t.Run(name, func(t *testing.T) {
		for _, status := range s.Statuses() {
			t.Run(status, func(t *testing.T) {
				t.Parallel()
				figure := newFigure(t)
				probes := s.Probes(status, from)
				for _, income := range probes {
					b, ok := s.For(status, income)
					if !ok {
						t.Fatalf("%s: no %s bracket holds %d", name, status, income)
					}
					c.Check(t, name, b.Over, income, status, want(b, income), figure(t, status, income))
				}
				t.Logf("%d probes", len(probes))
			})
		}
	})
}

// Probes returns the incomes, from the income from up, at which to check a
// status's brackets: each edge, a dollar either side of it, and the middle
// of each bracket that has a top.
func (s *Schedule) Probes(status string, from int64) []int64 {
	set := map[int64]bool{}
	for _, b := range s.Brackets {
		if b.Status != status {
			continue
		}
		for _, n := range []int64{b.Over - 1, b.Over, b.Over + 1} {
			set[n] = true
		}
		if !b.Top {
			set[(b.Over+b.ButNotOver)/2] = true
		}
	}
	var out []int64
	for n := range set {
		if n >= from {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Elections maps a TSV's filing status names to the return's elections.
var Elections = map[string]returnsv1.FilingStatusElection{
	"single": returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE,
	"mfj":    returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_JOINTLY,
	"mfs":    returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_MARRIED_FILING_SEPARATELY,
	"hoh":    returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_HEAD_OF_HOUSEHOLD,
	"qss":    returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_QUALIFYING_SURVIVING_SPOUSE,
}

// WageInputs is a 2025 return for a filer under 65 with one W-2 of the given
// wages, in whole dollars, and nothing else, filing with the given status.
// A married filer has a spouse with no income. Callers add any state.
func WageInputs(status string, wages int64) *returnsv1.ReturnInputs {
	person := func(id string) *returnsv1.Person {
		return &returnsv1.Person{
			Id:          id,
			FirstName:   id,
			DateOfBirth: &commonv1.Date{Year: 1980, Month: 6, Day: 1},
			Tin: &returnsv1.TaxpayerID{
				Kind:               returnsv1.TaxpayerIDKind_TAXPAYER_ID_KIND_SSN,
				ValidForEmployment: &[]bool{true}[0],
			},
		}
	}
	hh := &returnsv1.Household{
		MaritalStatus: returnsv1.MaritalStatus_MARITAL_STATUS_NEVER_MARRIED,
		Taxpayer:      person("filer"),
	}
	switch status {
	case "mfj", "mfs":
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_MARRIED
		hh.Spouse = person("spouse")
	case "qss":
		hh.MaritalStatus = returnsv1.MaritalStatus_MARITAL_STATUS_WIDOWED
	}
	return &returnsv1.ReturnInputs{
		SchemaVersion: 1,
		TaxYear:       2025,
		Federal: &returnsv1.Federal1040Inputs{
			Household: hh,
			Elections: &returnsv1.Elections{FilingStatus: Elections[status]},
			Documents: &returnsv1.Documents{
				W2S: []*returnsv1.W2Document{{
					Id:     "w2",
					Person: "filer",
					Form: &formw2.FormW2{
						WagesTipsOtherCompensation: &commonv1.Money{Currency: "USD", Minor: wages * 100},
					},
				}},
			},
		},
	}
}
