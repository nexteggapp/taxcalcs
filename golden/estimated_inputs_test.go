package golden

// The estimated tax households as data: each household estimatedtest builds,
// written out as the ReturnInputs that asks taxlisp the same question, so the
// households outlive the Go calculators. TestEstimated writes
// testdata/estimated/inputs/<code>.json.gz beside each jurisdiction's
// answers, keyed as the answers are: by year, then by household, in order.
//
// Each household is
//
//	{"name": ..., "inputs": ReturnInputs as protojson with proto names,
//	 "state_extras": {...}, "unmapped": {...}}
//
// state_extras are the values of the jurisdiction's own facts (Facts.State),
// by accessor name, until EstimatedStateInputs has a message for that state.
// unmapped are the Facts the protos have no place for, by Facts path, so
// nothing is dropped silently. A state whose EstimatedStateInputs message
// holds Facts the core doesn't, such as this year's state AGI, has them
// written there by estimatedStateInputs, and they leave unmapped.
//
// How the Facts map, as docs/estimated-tax-design.md says:
//   - this year's tax, withholding total and annualised periods are a
//     caller_estimate for the jurisdiction. A household with neither this
//     year's tax nor withholding is last_year_only; one with withholding but
//     no tax is a caller_estimate with the tax unset, because last_year_only
//     has no place for withholding.
//   - the federal prior year is ReturnInputs.federal.prior_year (AGI and
//     total tax), federal_prior_return and
//     prior_year_us_citizen_or_resident. A state's is an
//     EstimatedTaxInputs.PriorYear, with the household's federal AGI in
//     federal.prior_year as well, and the full-year-resident answer as both
//     the residency and prior_year_us_citizen_or_resident, since the Go facts
//     have one answer for both. Facts have no prior filing status, so last
//     year's is this year's, which is what the Go rules read.
//   - no prior year on record is federal_prior_return unset, or a PriorYear
//     entry with only the jurisdiction, so it stays apart from "not filed".
//   - withholding is written dated whatever the election, so the dates
//     survive; WITHHOLDING_CREDIT_AS_WITHHELD is the election to read them.
//   - the year the inputs are keyed by is ReturnInputs.tax_year and every
//     payment's designated_tax_year. Go asks every year's rules about the same
//     2026 households, so a 2025 entry carries the 2026 dates.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/nexteggapp/taxcalcs/californiataxcalc"
	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/estimated/estimatedtest"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type (
	estIn       = returnsv1.EstimatedTaxInputs
	estPrior    = returnsv1.EstimatedTaxInputs_PriorYear
	estCaller   = returnsv1.EstimatedTaxInputs_CallerEstimate
	estCallerJ  = returnsv1.EstimatedTaxInputs_CallerEstimate_Jurisdiction
	estPeriod   = returnsv1.EstimatedTaxInputs_CallerEstimate_PeriodTax
	estWithheld = returnsv1.EstimatedTaxInputs_Withholding
)

func cents(a money.USCents) *commonv1.Money { return &commonv1.Money{Currency: "USD", Minor: int64(a)} }

func protoDate(d estimated.Date) *commonv1.Date {
	if d.IsZero() {
		return nil
	}
	return &commonv1.Date{Year: int32(d.Year()), Month: int32(d.Month()), Day: int32(d.Day())}
}

// estimatedInputsFile is the inputs file for j: every household as
// ReturnInputs, for every year in estimatedYears.
func estimatedInputsFile(j estimated.Jurisdiction) (map[string]any, error) {
	hs := estimatedtest.Households(j)
	years := []any{}
	for _, year := range estimatedYears {
		households := []any{}
		for _, h := range hs {
			in, unmapped := returnInputs(h.Facts(), year)
			b, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(in)
			if err != nil {
				return nil, err
			}
			var inputs any
			if err := json.Unmarshal(b, &inputs); err != nil {
				return nil, err
			}
			households = append(households, map[string]any{
				"name": h.Name(), "inputs": inputs, "state_extras": stateExtras(h.Facts().State()), "unmapped": unmapped,
			})
		}
		years = append(years, map[string]any{"year": year, "households": households})
	}
	return map[string]any{"jurisdiction": string(j), "years": years}, nil
}

// returnInputs is the ReturnInputs that asks taxlisp about f for year, and
// the facts with no place in it.
func returnInputs(f estimated.Facts, year int) (*returnsv1.ReturnInputs, map[string]any) {
	j := string(f.Jurisdiction())
	federal := j == string(estimated.Federal)
	unmapped := map[string]any{}
	est := &estIn{
		AsOf:          protoDate(estimatedtest.PenaltyAsOf),
		Jurisdictions: []string{j},
	}
	fed := &returnsv1.Federal1040Inputs{
		Elections: &returnsv1.Elections{FilingStatus: returnsv1.FilingStatusElection(f.FilingStatus())},
	}
	in := &returnsv1.ReturnInputs{SchemaVersion: 1, TaxYear: int32(year), Federal: fed, EstimatedTax: est}
	if !federal && f.Jurisdiction() != referenceCode {
		in.States = []*returnsv1.StateInputs{{State: j}}
	}

	// This year.
	ty := f.ThisYear()
	optional := func(path string, get func() (money.USCents, bool)) {
		a, ok := get()
		if ok {
			unmapped[path] = int64(a)
		}
	}
	optional("this_year.federal_agi", ty.FederalAGI)
	optional("this_year.state_agi", ty.StateAGI)
	optional("this_year.gross_income", ty.GrossIncome)
	optional("this_year.taxable_income", ty.TaxableIncome)
	optional("this_year.local_tax", ty.LocalTax)
	optional("this_year.withheld", ty.Withheld)
	tax, taxKnown := ty.Tax()
	cj := &estCallerJ{Jurisdiction: j}
	if taxKnown {
		cj.Tax = cents(tax)
	}
	if len(f.Withholding()) > 0 {
		cj.Withheld = cents(f.WithheldTotal())
	}
	for _, p := range f.Periods() {
		cj.Periods = append(cj.Periods, &estPeriod{End: protoDate(p.End()), AnnualisedTax: cents(p.AnnualisedTax()), Income: cents(p.Income())})
	}
	if f.Elections().Annualised() != (len(f.Periods()) > 0) {
		unmapped["elections.annualised"] = f.Elections().Annualised()
	}
	if cj.Tax == nil && cj.Withheld == nil && len(cj.Periods) == 0 {
		est.ThisYear = &returnsv1.EstimatedTaxInputs_LastYearOnly_{LastYearOnly: &returnsv1.EstimatedTaxInputs_LastYearOnly{}}
	} else {
		est.ThisYear = &returnsv1.EstimatedTaxInputs_CallerEstimate_{CallerEstimate: &estCaller{Jurisdictions: []*estCallerJ{cj}}}
	}

	// Withholding, payments and the election.
	for _, w := range f.Withholding() {
		est.Withholding = append(est.Withholding, &estWithheld{Jurisdiction: j, Date: protoDate(w.On()), Amount: cents(w.Amount())})
	}
	if f.Elections().WithholdingAsWithheld() {
		est.WithholdingCredit = returnsv1.EstimatedTaxInputs_WITHHOLDING_CREDIT_AS_WITHHELD
	} else {
		est.WithholdingCredit = returnsv1.EstimatedTaxInputs_WITHHOLDING_CREDIT_EVENLY
	}
	for i, p := range f.Payments() {
		est.Payments = append(est.Payments, &returnsv1.EstimatedPayment{
			Jurisdiction: j, DatePaid: protoDate(p.On()), DesignatedTaxYear: int32(year), Amount: cents(p.Amount()),
		})
		if p.FromPriorYear() {
			unmapped[fmt.Sprintf("payments[%d].from_prior_year", i)] = true
		}
	}

	// Last year.
	if p, ok := f.PriorYear(); ok {
		status := returnsv1.EstimatedTaxInputs_PRIOR_RETURN_NOT_FILED
		switch {
		case p.Filed() && p.Months() == 12:
			status = returnsv1.EstimatedTaxInputs_PRIOR_RETURN_FULL_YEAR
		case p.Filed():
			status = returnsv1.EstimatedTaxInputs_PRIOR_RETURN_SHORT_YEAR
			unmapped["prior_year.months"] = p.Months()
		case p.NotRequiredToFile():
			status = returnsv1.EstimatedTaxInputs_PRIOR_RETURN_NOT_REQUIRED
		}
		resident, answered := p.FullYearResident()
		if answered {
			// A state's Go facts say whether the household lived in the
			// state all last year, which is the prior year's residency
			// below, not US citizenship or residence.
			if federal {
				est.PriorYearUsCitizenOrResident = proto.Bool(resident)
			}
			if !resident {
				// Part-year or nonresident: the Go facts don't say which.
				unmapped["prior_year.full_year_resident"] = false
			}
		}
		figs, hasFigs := p.Figures()
		priorTax, _ := figs.Tax()
		if hasFigs {
			optional("prior_year.gross_income", figs.GrossIncome)
			optional("prior_year.taxable_income", figs.TaxableIncome)
			optional("prior_year.local_tax", figs.LocalTax)
			optional("prior_year.withheld", figs.Withheld)
			if agi, ok := figs.FederalAGI(); ok {
				fed.PriorYear = &returnsv1.PriorYear{PriorYearAgi: cents(agi)}
			}
		}
		if federal {
			est.FederalPriorReturn = status
			if hasFigs {
				if fed.PriorYear == nil {
					fed.PriorYear = &returnsv1.PriorYear{}
				}
				fed.PriorYear.PriorYearTotalTax = cents(priorTax)
				optional("prior_year.state_agi", figs.StateAGI)
			}
		} else {
			py := &estPrior{Jurisdiction: j, Return: status}
			if answered && resident {
				py.Residency = returnsv1.EstimatedTaxInputs_RESIDENCY_FULL_YEAR
			}
			if hasFigs {
				py.Tax = cents(priorTax)
				if agi, ok := figs.StateAGI(); ok {
					py.Agi = cents(agi)
				}
				py.FilingStatus = form1040.FilingStatus(f.FilingStatus())
			}
			est.PriorYears = append(est.PriorYears, py)
		}
	} else if !federal {
		// No return on record: an entry that names the state and leaves the
		// return unspecified, which means unknown, apart from no entry, which
		// means not filed.
		est.PriorYears = append(est.PriorYears, &estPrior{Jurisdiction: j})
	}

	// Residence.
	periods := func(ps []estimated.ResidencePeriod) []any {
		out := []any{}
		for _, p := range ps {
			out = append(out, map[string]any{"state": p.State(), "from": dateJSON(p.From()), "to": dateJSON(p.To())})
		}
		return out
	}
	if federal {
		if len(f.Residence()) > 0 {
			// A federal household's periods name "US", which is no state.
			unmapped["residence"] = periods(f.Residence())
		}
	} else {
		for _, r := range f.Residence() {
			est.Residence = append(est.Residence, &returnsv1.EstimatedTaxInputs_ResidencePeriod{
				State: r.State(), From: protoDate(r.From()), To: protoDate(r.To()),
			})
		}
	}
	if len(f.PriorResidence()) > 0 {
		unmapped["prior_residence"] = periods(f.PriorResidence())
	}

	// Filing, the first day of the requirement, farming and fishing.
	est.ReturnFiled = protoDate(f.ReturnFiled())
	est.RequirementFirstMet = protoDate(f.RequirementFirstMet())
	if f.FarmShare() != 0 {
		unmapped["farm_share"] = int64(f.FarmShare())
	}
	if f.FishShare() != 0 {
		unmapped["fish_share"] = int64(f.FishShare())
	}

	// The waiver, and the people a retirement waiver needs.
	pos := f.Positions()
	kind, answered := pos.Waiver()
	switch {
	case kind == estimated.WaiverCasualty || kind == estimated.WaiverRetiredOrDisabled:
		causes := map[estimated.WaiverCause]returnsv1.EstimatedTaxInputs_WaiverCause{
			estimated.CauseCasualty:   returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_CASUALTY,
			estimated.CauseDisaster:   returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_DISASTER,
			estimated.CauseRetirement: returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_RETIREMENT,
			estimated.CauseDisability: returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_DISABILITY,
		}
		cause, ok := causes[pos.WaiverCause()]
		if !ok {
			// A waiver asked for without its cause: the proto always names
			// one, so take the first of the kind, and record that it wasn't
			// given.
			cause = returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_CASUALTY
			if kind == estimated.WaiverRetiredOrDisabled {
				cause = returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_RETIREMENT
			}
			unmapped["positions.waiver_cause"] = pos.WaiverCause().String()
		}
		est.Waiver = &returnsv1.EstimatedTaxInputs_Waiver{Cause: cause, Occurred: protoDate(pos.WaiverOn()), Born: protoDate(pos.Born())}
		if !pos.Born().IsZero() {
			fed.Household = &returnsv1.Household{Taxpayer: &returnsv1.Person{Id: "you", DateOfBirth: protoDate(pos.Born())}}
		}
	case answered:
		// Answered: no waiver.
		est.Waiver = &returnsv1.EstimatedTaxInputs_Waiver{Cause: returnsv1.EstimatedTaxInputs_WAIVER_CAUSE_NONE}
	}
	// Unanswered leaves the waiver unset, which the rules ask about.
	if ps := f.People(); len(ps) > 0 {
		people := []any{}
		for _, p := range ps {
			people = append(people, map[string]any{"age": p.Age(), "retired_after_62": p.RetiredAfter62(), "disabled": p.Disabled()})
		}
		unmapped["people"] = people
	}
	if f.FilingStatus() < filing.Single || f.FilingStatus() > filing.SurvivingSpouse {
		unmapped["filing_status"] = f.FilingStatus().String()
	}

	// The jurisdiction's own facts, where its EstimatedStateInputs message
	// has a place for Facts the core doesn't hold.
	if s, mapped := estimatedStateInputs(j, f); s != nil {
		est.StateInputs = append(est.StateInputs, s)
		for _, path := range mapped {
			delete(unmapped, path)
		}
	}
	return in, unmapped
}

// estimatedStateInputs is the jurisdiction's own EstimatedStateInputs entry
// for f, or nil for a jurisdiction with no message of its own, and the Facts
// paths the entry holds. Each state's case is one entry of the switch.
func estimatedStateInputs(j string, f estimated.Facts) (*returnsv1.EstimatedStateInputs, []string) {
	ty := f.ThisYear()
	switch estimated.Jurisdiction(j) {
	case estimated.Alabama:
		al := &returnsv1.AlabamaEstimatedInputs{}
		if agi, ok := ty.StateAGI(); ok {
			al.AlabamaAgi = cents(agi)
		}
		return &returnsv1.EstimatedStateInputs{State: j, Inputs: &returnsv1.EstimatedStateInputs_Al{Al: al}},
			[]string{"this_year.state_agi"}
	case estimated.California:
		ca := &returnsv1.CaliforniaEstimatedInputs{}
		var mapped []string
		if agi, ok := ty.StateAGI(); ok {
			ca.CaliforniaAgi = cents(agi)
			mapped = append(mapped, "this_year.state_agi")
		}
		if p, ok := f.PriorYear(); ok {
			figs, _ := p.Figures()
			if withheld, known := figs.Withheld(); known {
				ca.PriorYearWithholding = cents(withheld)
				mapped = append(mapped, "prior_year.withheld")
			}
		}
		if s, ok := f.State().(californiataxcalc.EstimatedFacts); ok {
			ca.GroupReturn = s.GroupReturn()
			for _, c := range s.WorksheetI() {
				ca.WorksheetI = append(ca.WorksheetI, &returnsv1.WorksheetIColumn{
					TaxOnTotal: cents(c.TaxOnTotal()), TotalTaxable: cents(c.TotalTaxable()),
					CaliforniaAgi: cents(c.CaliforniaAGI()), AnnualisedTotalAgi: cents(c.AnnualisedTotalAGI()),
					Deduction: cents(c.Deduction()), ExemptionCredits: cents(c.ExemptionCredits()),
					ProratedCredits: cents(c.ProratedCredits()), OtherCredits: cents(c.OtherCredits()),
					OtherTax: cents(c.OtherTax()),
				})
			}
		}
		return &returnsv1.EstimatedStateInputs{State: j, Inputs: &returnsv1.EstimatedStateInputs_Ca{Ca: ca}}, mapped
	case estimated.NewMexico:
		p, ok := f.PriorYear()
		figs, hasFigs := p.Figures()
		withheld, known := figs.Withheld()
		if !ok || !hasFigs || !known {
			return nil, nil
		}
		nm := &returnsv1.NewMexicoEstimatedInputs{PriorYearWithheld: cents(withheld)}
		return &returnsv1.EstimatedStateInputs{State: j, Inputs: &returnsv1.EstimatedStateInputs_Nm{Nm: nm}},
			[]string{"prior_year.withheld"}
	}
	return nil, nil
}

// stateExtras are a jurisdiction's own facts, by accessor: each exported
// method with no arguments, and its value, or null for a (value, false)
// pair. Keys are sorted by encoding/json.
func stateExtras(s any) map[string]any {
	out := map[string]any{}
	if s == nil {
		return out
	}
	v := reflect.ValueOf(s)
	for i := 0; i < v.NumMethod(); i++ {
		m, name := v.Method(i), v.Type().Method(i).Name
		if m.Type().NumIn() != 0 || name == "String" {
			continue
		}
		rs := m.Call(nil)
		switch {
		case len(rs) == 1:
			out[name] = extraJSON(rs[0])
		case len(rs) == 2 && rs[1].Kind() == reflect.Bool:
			if rs[1].Bool() {
				out[name] = extraJSON(rs[0])
			} else {
				out[name] = nil
			}
		}
	}
	return out
}

// extraJSON is a state extra's value in a stable JSON form: amounts as
// integer cents, dates as YYYY-MM-DD, a value with a String method as that,
// and slices and structs field by field.
func extraJSON(v reflect.Value) any {
	switch x := v.Interface().(type) {
	case money.USCents:
		return int64(x)
	case money.Rate:
		return int64(x)
	case estimated.Date:
		return dateJSON(x)
	case bool, string:
		return x
	case fmt.Stringer:
		return x.String()
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	case reflect.Slice:
		out := []any{}
		for i := 0; i < v.Len(); i++ {
			out = append(out, extraJSON(v.Index(i)))
		}
		return out
	case reflect.Struct:
		if v.NumMethod() > 0 {
			return stateExtras(v.Interface())
		}
	}
	return fmt.Sprint(v.Interface())
}

// readInputs reads one jurisdiction's inputs file back: per year, each
// household's name and ReturnInputs.
func readInputs(j estimated.Jurisdiction) (map[int][]namedInputs, error) {
	var file struct {
		Years []struct {
			Year       int `json:"year"`
			Households []struct {
				Name   string          `json:"name"`
				Inputs json.RawMessage `json:"inputs"`
			} `json:"households"`
		} `json:"years"`
	}
	if err := readGzipJSON("estimated/inputs/"+string(j)+".json.gz", &file); err != nil {
		return nil, err
	}
	out := map[int][]namedInputs{}
	for _, y := range file.Years {
		for _, h := range y.Households {
			in := &returnsv1.ReturnInputs{}
			if err := protojson.Unmarshal(h.Inputs, in); err != nil {
				return nil, fmt.Errorf("%s %d %q: %w", j, y.Year, h.Name, err)
			}
			out[y.Year] = append(out[y.Year], namedInputs{h.Name, in})
		}
	}
	return out, nil
}

type namedInputs struct {
	name   string
	inputs *returnsv1.ReturnInputs
}

// unmappedPaths are the Facts paths any household leaves unmapped, with
// indexes dropped, sorted.
func unmappedPaths(files map[estimated.Jurisdiction]map[string]any) []string {
	set := map[string]bool{}
	for _, f := range files {
		for _, y := range f["years"].([]any) {
			for _, h := range y.(map[string]any)["households"].([]any) {
				for k := range h.(map[string]any)["unmapped"].(map[string]any) {
					if i := strings.Index(k, "["); i >= 0 {
						k = k[:i] + "[]" + k[strings.Index(k, "]")+1:]
					}
					set[k] = true
				}
			}
		}
	}
	var out []string
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
