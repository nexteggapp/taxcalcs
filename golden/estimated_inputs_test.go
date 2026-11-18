package golden

// The estimated tax households as data: testdata/estimated/inputs/<code>.json.gz,
// keyed as the answers in testdata/estimated/<code>.json.gz are: by year,
// then by household, in order. They were written from the Go calculators'
// households (estimatedtest) before those were deleted, and are now fixed
// files; see testdata/estimated/README.md.
//
// Each household is
//
//	{"name": ..., "inputs": ReturnInputs as protojson with proto names,
//	 "state_extras": {...}, "unmapped": {...}}
//
// state_extras were the values of the jurisdiction's own Go facts, by
// accessor name, and unmapped the Go facts the protos had no place for, by
// path, so nothing was dropped silently. Only "inputs" is read; the rest is
// the record of what the Go households held.
//
// How the Go facts were mapped:
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
//     payment's designated_tax_year. Go asked every year's rules about the same
//     2026 households, so a 2025 entry carries the 2026 dates.

import (
	"encoding/json"
	"fmt"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

// readInputs reads one jurisdiction's inputs file back: per year, each
// household's name and ReturnInputs.
func readInputs(j string) (map[int][]namedInputs, error) {
	var file struct {
		Years []struct {
			Year       int `json:"year"`
			Households []struct {
				Name   string          `json:"name"`
				Inputs json.RawMessage `json:"inputs"`
			} `json:"households"`
		} `json:"years"`
	}
	if err := readGzipJSON("estimated/inputs/"+j+".json.gz", &file); err != nil {
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
