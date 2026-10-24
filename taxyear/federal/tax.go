package federal

import (
	"fmt"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// medicalFloor is the share of adjusted gross income medical expenses must
// pass to be deductible, 26 U.S.C. § 213(a).
const medicalFloor = money.Rate(750)

// saltLimit is the § 164(b)(6) limit on state and local taxes, as § 164(b)(7)
// sets it for 2025 and 2026 (Pub. L. 119-21, sec. 70120): the applicable
// limitation, less 30% of modified adjusted gross income over the threshold,
// but not below $10,000; each halved for a separate return.
func saltLimit(year int, status filing.Status, magi money.USCents) (money.USCents, error) {
	var limit, threshold money.USCents
	switch year {
	case 2025:
		limit, threshold = 40_000*money.Dollar, 500_000*money.Dollar
	case 2026:
		limit, threshold = 40_400*money.Dollar, 505_000*money.Dollar
	default:
		return 0, fmt.Errorf("federal: no § 164(b)(7) limit for %d: %w", year, ustaxcalc.ErrNoTable)
	}
	floor := 10_000 * money.Dollar
	if status == filing.MarriedFilingSeparately {
		limit, threshold, floor = limit/2, threshold/2, floor/2
	}
	reduced := limit - money.Rate(3000).Of(max(0, magi-threshold))
	return max(reduced, floor), nil
}

// deduction works out Schedule A from the itemised models, the state income
// tax withheld and paid in the year and the property tax, then takes the
// standard deduction or the itemised total as elected, or the larger.
func (c *calc) deduction() error {
	sa := c.r.form.GetScheduleA()
	var medical, realEstate, mortgage, gifts money.USCents
	for _, it := range c.proj.GetItemised() {
		b := it.GetYear().GetBoxes()
		medical += model.Value(b.GetMedical())
		realEstate += model.Value(b.GetRealEstateTaxes())
		mortgage += model.Value(b.GetMortgageInterest())
		gifts += model.Value(b.GetGiftsByCash())
	}
	stateTax := c.stateTaxPaid()
	// Each line is rounded once, where it is worked out.
	floor := model.Dollars(medicalFloor.Of(max(0, c.r.agi)))
	deductibleMedical := max(0, medical-floor)
	salt := stateTax + realEstate
	limit, err := saltLimit(c.ctx.Year(), c.r.status, c.r.agi)
	if err != nil {
		return err
	}
	limit = model.Dollars(limit)
	afterCap := min(salt, limit)
	withheld, estimates, balance := c.saltParts()
	c.r.summary.Salt = &scenariosv1.SaltSummary{Withholding: model.Money(withheld), Estimates: model.Money(estimates),
		LastYearBalance: model.Money(balance), Cap: model.Money(limit)}
	charityFloor, charityFloorAmount := c.itemisedSummary(floor, deductibleMedical, gifts)
	sa.MedicalAndDentalExpenses, sa.DeductibleMedicalAndDental = model.Money(medical), model.Money(deductibleMedical)
	sa.StateLocalIncomeOrSalesTaxes, sa.StateLocalRealEstateTaxes = model.Money(stateTax), model.Money(realEstate)
	sa.TotalStateAndLocalTaxes, sa.StateAndLocalTaxesAfterCap = model.Money(salt), model.Money(afterCap)
	sa.TotalTaxesPaid = model.Money(afterCap)
	sa.MortgageInterestReportedOn_1098, sa.TotalMortgageInterestAndPoints, sa.TotalInterestPaid = model.Money(mortgage), model.Money(mortgage), model.Money(mortgage)
	sa.GiftsByCashOrCheck, sa.TotalGiftsToCharity = model.Money(gifts), model.Money(gifts)
	before := deductibleMedical + afterCap + mortgage + gifts
	w := c.r.working("federal.schedule_a.total_itemized_deductions")
	w.Rules = append(w.Rules,
		rule("26 U.S.C. § 213(a)", fmt.Sprintf("%v of medical expenses less 7.5%% of adjusted gross income, %v, leaves %v", medical, floor, deductibleMedical),
			"federal.schedule_a.medical_and_dental_expenses", "federal.schedule_a.deductible_medical_and_dental"),
		rule("26 U.S.C. § 164(b)(6) and (7), Pub. L. 119-21, sec. 70120", fmt.Sprintf("%v of state and local taxes, held to the %v limit, is %v", salt, limit, afterCap),
			"federal.schedule_a.total_state_and_local_taxes", "federal.schedule_a.state_and_local_taxes_after_cap"))
	limited, err := ustaxcalc.ItemisedLimits(c.ctx.Year(), c.r.status, before, min(gifts, before), c.r.agi)
	if err != nil {
		return err
	}
	c.add("federal.schedule_a.total_itemized_deductions", limited.Applied(), limited.Required())
	c.itemised = model.Dollars(limited.Value())
	sa.TotalItemizedDeductions = model.Money(c.itemised)
	// The total is Schedule A's lines less the charity floor and the 2/37
	// limit, each a figure of its own, so it adds up from the displayed
	// figures.
	bracket := before - charityFloorAmount - c.itemised
	c.r.summary.ItemisedLimitation, c.r.summary.ItemisedBracketLimit = model.Money(before-c.itemised), model.Money(bracket)
	if bracket != 0 {
		w := c.r.working("summary.itemised_bracket_limit")
		w.Rules = append(w.Rules, rule("26 U.S.C. § 68, as amended by Pub. L. 119-21, sec. 70111",
			fmt.Sprintf("In the 37%% bracket, 2/37 of the itemised deductions, or of the income over the bracket if less, doesn't count: %v", bracket),
			"summary.itemised_bracket_limit"))
	}
	c.r.term("federal.schedule_a.total_itemized_deductions", nil, "federal.schedule_a.deductible_medical_and_dental",
		"federal.schedule_a.total_taxes_paid", "federal.schedule_a.total_interest_paid", "federal.schedule_a.total_gifts_to_charity")
	if charityFloor != "" {
		c.r.minus("federal.schedule_a.total_itemized_deductions", charityFloor)
	}
	c.r.minus("federal.schedule_a.total_itemized_deductions", "summary.itemised_bracket_limit")
	taken, err := ustaxcalc.DeductionTaken(c.ctx.Year(), c.r.status, c.conditions(), c.itemised, c.election())
	if err != nil {
		return err
	}
	c.add("federal.form_1040.deduction", taken.Applied(), taken.Required())
	c.taken = taken.Value()
	c.taken.Amount = model.Dollars(c.taken.Amount)
	f := c.r.form.GetForm_1040()
	f.Deduction, f.Itemized = model.Money(c.taken.Amount), c.taken.Itemised
	f.TotalDeductions = model.Money(c.taken.Amount)
	return nil
}

// stateTaxPaid is the state income tax paid in the year, which Schedule A
// line 5a deducts: the state withholding on the W-2s, and the state tax
// payments dated in the year, whatever year they were for.
func (c *calc) stateTaxPaid() money.USCents {
	var paid money.USCents
	for _, v := range c.r.stateWithheld {
		paid += v
	}
	for _, t := range c.proj.GetTaxPaid() {
		if t.GetJurisdiction() == "US" {
			continue
		}
		for _, it := range t.GetItems() {
			d, _ := civil.FromProto(it.GetDate())
			if d.Year() == c.ctx.Year() && it.GetPayment().GetPurpose().GetNotTax() == nil && !it.GetPayment().GetFromLastYear() {
				paid += model.Value(it.GetPayment().GetAmount())
			}
		}
	}
	return paid
}

func (c *calc) election() ustaxcalc.Election {
	for _, e := range c.facts.GetElections() {
		switch e.GetDeductionMethod() {
		case factsv1.DeductionMethod_DEDUCTION_METHOD_STANDARD:
			return ustaxcalc.ElectStandard
		case factsv1.DeductionMethod_DEDUCTION_METHOD_ITEMISED:
			return ustaxcalc.ElectItemise
		}
	}
	return ustaxcalc.ElectLarger
}

// tax works out taxable income and the tax at the ordinary and gains rates,
// then Schedule 2's other taxes, total tax and the withholding.
func (c *calc) tax() error {
	f := c.r.form.GetForm_1040()
	taxable := max(0, c.r.agi-c.taken.Amount)
	f.TaxableIncome = model.Money(taxable)
	res, err := ustaxcalc.TaxOnIncome(c.ctx.Year(), c.r.status, taxable, c.qualified, c.gains.NetCapitalGain())
	if err != nil {
		return err
	}
	c.add("federal.form_1040.tax", res.Applied(), res.Required())
	income := model.Dollars(res.Value().Total())
	f.Tax, f.TaxPlusAdditional = model.Money(income), model.Money(income)
	other := c.seTax + c.early + c.excessIRA
	sch2 := c.r.form.GetSchedule_2()
	sch2.TotalAdditionalSocialSecurityMedicare = model.Money(0)
	sch2.TotalPartTwo = model.Money(other)
	f.OtherTaxes = model.Money(other)
	f.TaxAfterCredits = model.Money(income)
	c.r.total = income + other
	c.r.tax2210 = income + c.seTax + c.early
	f.TotalTax = model.Money(c.r.total)
	withheld := model.Value(f.GetWithheldFromW2()) + model.Value(f.GetWithheldFrom_1099())
	f.TotalWithheld = model.Money(withheld)
	c.r.term("federal.form_1040.total_tax", nil, "federal.form_1040.tax", "federal.form_1040.other_taxes")
	c.notIncluded()
	return nil
}

// notIncluded lists what the return leaves out because this build can't
// compute it, each only where it could apply.
func (c *calc) notIncluded() {
	n := &c.r.notes
	n.LeaveOut("Tax credits, such as the Child Tax Credit, education credits and the Earned Income Credit")
	if model.Value(c.r.form.GetSchedule_1().GetBusinessIncomeOrLoss()) > 0 {
		n.LeaveOut("The qualified business income deduction, which can take up to 20% off business income")
	}
	table, err := ustaxcalc.TableFor(c.ctx.Year(), c.r.status)
	var medicare money.USCents
	for _, p := range c.people {
		medicare += p.medicareWages
	}
	if err == nil && medicare > table.AdditionalMedicareThreshold {
		n.LeaveOut(fmt.Sprintf("The 0.9%% Additional Medicare Tax on wages and self-employment income over %v; what employers withheld for it is counted", table.AdditionalMedicareThreshold))
	}
	f := c.r.form.GetForm_1040()
	if model.Value(f.GetTaxableInterest()) > 0 || model.Value(f.GetOrdinaryDividends()) > 0 || c.gains.InIncome() > 0 {
		n.LeaveOut("The 3.8% Net Investment Income Tax, which applies to investment income at higher incomes")
	}
	if c.gains.LongTerm() > 0 {
		n.LeaveOut("The 25% and 28% rates on some gains, such as collectibles and depreciation on real estate")
	}
	n.LeaveOut("The Alternative Minimum Tax")
	if y := c.ctx.Year(); y >= 2025 && y <= 2028 {
		n.LeaveOut("The deductions for tips, overtime and car loan interest")
	}
}
