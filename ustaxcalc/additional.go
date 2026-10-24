package ustaxcalc

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nexteggapp/taxcalcs/working"
)

// RetirementDistribution is one Form 1099-R: the taxable amount in box 2a
// and the distribution code in box 7.
type RetirementDistribution struct {
	taxable USCents
	code    string
}

func (d RetirementDistribution) Taxable() USCents { return d.taxable }
func (d RetirementDistribution) Code() string     { return d.code }

// NewRetirementDistribution is a distribution's taxable amount and its box 7
// code, such as "1" for an early distribution with no known exception or "7"
// for a normal one.
func NewRetirementDistribution(taxable USCents, code string) (RetirementDistribution, error) {
	switch {
	case taxable < 0:
		return RetirementDistribution{}, errors.New("ustaxcalc: a distribution's taxable amount cannot be negative")
	case code == "":
		return RetirementDistribution{}, errors.New("ustaxcalc: a distribution needs its box 7 code")
	}
	return RetirementDistribution{taxable: taxable, code: code}, nil
}

// EarlyDistributionTax is the 10% additional tax of 26 U.S.C. § 72(t)(1) on
// the distributions with code 1, Schedule 2 (Form 1040) line 8.
//
// The Instructions for Form 5329 (2025) let a filer whose Forms 1099-R all
// show code 1, and who owes the tax on the full amount, report 10% of it on
// Schedule 2 line 8 without Form 5329. That is what this computes. An
// exception claimed on Form 5329, and the other codes' rules, such as 25%
// on a SIMPLE IRA's code S, are not computed; [EarlyDistributionTaxOther]
// reports a distribution with such a code.
//
// The Form 2210 instructions (2025) count Schedule 2 line 8's additional
// tax on distributions in the tax the estimated tax rules measure.
func EarlyDistributionTax(year int, ds []RetirementDistribution) (Result[USCents], error) {
	if _, err := TableFor(year, Single); err != nil {
		return Result[USCents]{}, err
	}
	var subject USCents
	for _, d := range ds {
		if strings.Contains(d.code, "1") && !strings.Contains(d.code, "P") {
			subject += d.taxable
		}
	}
	tax := Rate(1_000).Of(subject)
	var n working.Notes
	if subject > 0 {
		n.Rule("26 U.S.C. § 72(t)(1); Form 5329 (2025), Part I, line 4; Instructions for Form 5329 (2025)",
			fmt.Sprintf("10%% of the %s of early distributions shown with code 1 is %s, on Schedule 2 (Form 1040), line 8", subject, tax))
	}
	return working.NewResult(tax).With(n).Build()
}

// EarlyDistributionTaxOther reports a distribution whose code isn't one
// [HandledDistributionCodes] lists, whose income or additional tax is then
// not worked out.
func EarlyDistributionTaxOther(ds []RetirementDistribution) bool {
	return len(UnhandledDistributionCodes(ds)) > 0
}

// handledCodes are the Form 1099-R box 7 codes whose 2026 income and
// additional tax the Instructions for Forms 1099-R and 5498 (2026) and the
// Form 1040 instructions settle:
//   - 1, early with no known exception: box 2a is income, and 10% is due;
//   - 2, early with an exception, 3, disability, 4, death, and 7, normal:
//     box 2a is income, with no additional tax;
//   - G and H, direct rollovers: box 2a is income, which the payer reports
//     as $0 for a rollover that isn't to a Roth IRA, with no additional tax;
//   - P, taxable in 2025 or a previous year: none of it is 2026 income.
var handledCodes = "12347GHP"

// HandledDistributionCodes lists the box 7 codes, alone or paired, whose
// income and additional tax are worked out.
func HandledDistributionCodes() []string {
	return []string{"1", "2", "3", "4", "7", "G", "H", "P"}
}

// UnhandledDistributionCodes are the codes among ds that aren't handled,
// each once.
func UnhandledDistributionCodes(ds []RetirementDistribution) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range ds {
		for _, c := range d.code {
			if !strings.ContainsRune(handledCodes, c) && !seen[string(c)] {
				seen[string(c)] = true
				out = append(out, string(c))
			}
		}
	}
	return out
}

// DistributionIncome is the 2026 income from the distributions: box 2a of
// each, except one coded P, which was income in an earlier year. A
// distribution with an unhandled code counts its box 2a, as the payer
// reports it.
func DistributionIncome(ds []RetirementDistribution) USCents {
	var income USCents
	for _, d := range ds {
		if !strings.Contains(d.code, "P") {
			income += d.taxable
		}
	}
	return income
}

// RentalLossAllowed is the part of a net loss from rental real estate the
// filer actively participated in that 26 U.S.C. § 469(i) lets offset other
// income: up to $25,000, less half of modified AGI over $100,000; on a
// separate return, $12,500 less half of modified AGI over $50,000 for a
// filer who lived apart from their spouse all year, and nothing otherwise.
// magi is adjusted gross income figured without the loss and without the
// IRA deduction, among the amounts § 469(i)(3)(E) leaves out.
func RentalLossAllowed(status FilingStatus, loss, magi USCents, livedApart bool) (Result[USCents], error) {
	if loss < 0 {
		return Result[USCents]{}, errors.New("ustaxcalc: a rental loss is given as a positive amount")
	}
	limit, start := 25_000*Dollar, 100_000*Dollar
	var n working.Notes
	if status == MarriedFilingSeparately {
		if !livedApart {
			n.Rule("26 U.S.C. § 469(i)(5)(B)", "a married person filing separately who lived with their spouse at any time in the year gets no allowance, so none of the loss is allowed")
			return working.NewResult(USCents(0)).With(n).Build()
		}
		limit, start = 12_500*Dollar, 50_000*Dollar
	}
	limit = max(limit-max(magi-start, 0)/2, 0)
	allowed := min(loss, limit)
	n.Rule("26 U.S.C. § 469(i)(1) to (3), (5)",
		fmt.Sprintf("modified AGI of %s leaves an allowance of %s, so %s of the %s rental loss is allowed", magi, limit, allowed, loss))
	return working.NewResult(allowed).With(n).Build()
}

// StateRefund is what the State and Local Income Tax Refund Worksheet in the
// Form 1040 instructions reads: the refund, and last year's Schedule A.
type StateRefund struct {
	refund            USCents
	priorIncomeTax    USCents
	priorSALTPaid     USCents
	priorSALTDeducted USCents
	priorItemized     USCents
	priorStatus       FilingStatus
	priorBoxes        int
	spouseItemized    bool
}

// Refund is the state or local income tax refund on Form 1099-G.
func (r StateRefund) Refund() USCents { return r.refund }

// StateRefundBuilder builds a [StateRefund].
type StateRefundBuilder struct{ r StateRefund }

// NewStateRefund starts the worksheet for a refund of last year's state or
// local income tax, for a filer whose last year's return used status.
func NewStateRefund(refund USCents, status FilingStatus) *StateRefundBuilder {
	return &StateRefundBuilder{r: StateRefund{refund: refund, priorStatus: status}}
}

// PriorIncomeTax is the state and local income tax included in last year's
// Schedule A line 5d, which caps line 1.
func (b *StateRefundBuilder) PriorIncomeTax(a USCents) *StateRefundBuilder {
	b.r.priorIncomeTax = a
	return b
}

// PriorSALT is last year's Schedule A line 5d, the state and local taxes
// paid, and line 5e, what was deducted after the limit.
func (b *StateRefundBuilder) PriorSALT(paid, deducted USCents) *StateRefundBuilder {
	b.r.priorSALTPaid, b.r.priorSALTDeducted = paid, deducted
	return b
}

// PriorItemized is last year's Schedule A line 17. Zero for a filer who
// took the standard deduction.
func (b *StateRefundBuilder) PriorItemized(a USCents) *StateRefundBuilder {
	b.r.priorItemized = a
	return b
}

// PriorBoxes is the number of worksheet line 6 boxes checked: each of you
// and a spouse 65 or older, and each blind, last year.
func (b *StateRefundBuilder) PriorBoxes(n int) *StateRefundBuilder {
	b.r.priorBoxes = n
	return b
}

// SpouseItemizedSeparately marks a separate return last year whose spouse
// itemized, for whom the worksheet skips the standard deduction.
func (b *StateRefundBuilder) SpouseItemizedSeparately() *StateRefundBuilder {
	b.r.spouseItemized = true
	return b
}

// Build refuses negative figures, more than four boxes, and a deduction
// after the limit larger than what was paid.
func (b *StateRefundBuilder) Build() (StateRefund, error) {
	r := b.r
	switch {
	case r.refund < 0 || r.priorIncomeTax < 0 || r.priorSALTPaid < 0 || r.priorSALTDeducted < 0 || r.priorItemized < 0:
		return StateRefund{}, errors.New("ustaxcalc: a state refund worksheet figure cannot be negative")
	case r.priorBoxes < 0 || r.priorBoxes > 4:
		return StateRefund{}, fmt.Errorf("ustaxcalc: %d boxes on worksheet line 6", r.priorBoxes)
	case r.priorSALTDeducted > r.priorSALTPaid:
		return StateRefund{}, errors.New("ustaxcalc: more state and local tax deducted than paid")
	case r.priorStatus == FilingStatusUnspecified:
		return StateRefund{}, errors.New("ustaxcalc: last year's filing status is needed")
	}
	return r, nil
}

// StateRefundTaxable is the taxable part of a refund of last year's state or
// local income tax, Schedule 1 (Form 1040) line 1, by the State and Local
// Income Tax Refund Worksheet in the 2025 Form 1040 instructions. A refund is
// taxable only as far as deducting the tax lowered last year's tax.
//
// Lines 5 and 6 are last year's standard deduction and the addition for age
// or blindness, from last year's table. The worksheet doesn't cover a refund
// for an earlier year, the alternative minimum tax, unused credits and the
// other cases its exception lists, which Publication 525 handles.
func StateRefundTaxable(year int, r StateRefund) (Result[USCents], error) {
	prior, err := TableFor(year-1, r.priorStatus)
	if err != nil {
		return Result[USCents]{}, err
	}
	var n working.Notes
	const cite = "Form 1040 instructions (2025), State and Local Income Tax Refund Worksheet—Schedule 1, Line 1"
	done := func(taxable USCents, why string) (Result[USCents], error) {
		n.Rule(cite, why)
		return working.NewResult(taxable).With(n).Build()
	}
	line1 := min(r.refund, r.priorIncomeTax)
	line3 := line1
	if r.priorSALTPaid > r.priorSALTDeducted {
		line2 := r.priorSALTPaid - r.priorSALTDeducted
		if line1 <= line2 {
			return done(0, fmt.Sprintf("the %s refund is no more than the %s of state and local taxes the limit kept from being deducted, so none is taxable", line1, line2))
		}
		line3 = line1 - line2
	}
	line8 := r.priorItemized
	if !(r.priorStatus == MarriedFilingSeparately && r.spouseItemized) {
		line7 := prior.StandardDeduction + USCents(r.priorBoxes)*prior.AdditionalAgedBlind
		if line7 >= r.priorItemized {
			return done(0, fmt.Sprintf("last year's itemized deductions of %s were no more than the %s standard deduction, so none of the refund is taxable", r.priorItemized, line7))
		}
		line8 = r.priorItemized - line7
	}
	taxable := min(line3, line8)
	return done(taxable, fmt.Sprintf("the smaller of %s of refund (line 3) and %s of itemized deductions over the standard deduction (line 8) is taxable: %s", line3, line8, taxable))
}
