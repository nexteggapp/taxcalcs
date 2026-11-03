package taxyear

import (
	"fmt"
	"strings"

	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// formLine is a Form 1040 line's number and the label the builder page
// gives it.
type formLine struct{ number, label string }

// form1040Lines are Form 1040's lines by field name, with labels a person
// without tax knowledge can read. The numbers are the 2025 form's, from
// taxprotos' mapping_form1040.textproto, until the IRS publishes 2026's.
var form1040Lines = map[string]formLine{
	"w2_wages":                            {"1a", "Wages from jobs"},
	"household_employee_wages":            {"1b", "Wages as a household employee"},
	"unreported_tip_income":               {"1c", "Tips not on a W-2"},
	"medicaid_waiver_payments":            {"1d", "Medicaid waiver payments"},
	"taxable_dependent_care_benefits":     {"1e", "Dependent care benefits taxed"},
	"employer_provided_adoption_benefits": {"1f", "Adoption benefits"},
	"wages_from_form_8919":                {"1g", "Wages with no tax withheld"},
	"other_earned_income":                 {"1h", "Other earned income"},
	"nontaxable_combat_pay_election":      {"1i", "Combat pay"},
	"total_wages":                         {"1z", "All wages"},
	"tax_exempt_interest":                 {"2a", "Tax-free interest"},
	"taxable_interest":                    {"2b", "Interest"},
	"qualified_dividends":                 {"3a", "Dividends taxed at the lower rate"},
	"ordinary_dividends":                  {"3b", "Dividends"},
	"ira_distributions":                   {"4a", "IRA withdrawals"},
	"ira_distributions_taxable":           {"4b", "IRA withdrawals taxed"},
	"pensions_and_annuities":              {"5a", "Pensions"},
	"pensions_and_annuities_taxable":      {"5b", "Pensions taxed"},
	"social_security_benefits":            {"6a", "Social Security"},
	"social_security_benefits_taxable":    {"6b", "Social Security taxed"},
	"capital_gain_or_loss":                {"7a", "Gains from selling investments"},
	"additional_income":                   {"8", "Other income, such as self-employment"},
	"total_income":                        {"9", "Total income"},
	"adjustments_to_income":               {"10", "Taken off income before deductions"},
	"adjusted_gross_income":               {"11a", "Adjusted gross income"},
	"deduction":                           {"12e", "Standard or itemised deduction"},
	"qualified_business_income_deduction": {"13a", "Self-employment profit deduction"},
	"additional_deductions":               {"13b", "Other deductions"},
	"total_deductions":                    {"14", "Total deductions"},
	"taxable_income":                      {"15", "Income that is taxed"},
	"tax":                                 {"16", "Income tax"},
	"additional_tax_from_schedule_2":      {"17", "Extra tax, such as AMT"},
	"tax_plus_additional":                 {"18", "Tax before credits"},
	"child_tax_credit":                    {"19", "Child tax credit"},
	"nonrefundable_credits":               {"20", "Other credits"},
	"total_credits":                       {"21", "Total credits"},
	"tax_after_credits":                   {"22", "Tax after credits"},
	"other_taxes":                         {"23", "Other taxes, such as self-employment tax"},
	"total_tax":                           {"24", "Total tax"},
	"withheld_from_w2":                    {"25a", "Withheld from pay"},
	"withheld_from_1099":                  {"25b", "Withheld from 1099s"},
	"withheld_from_other_forms":           {"25c", "Withheld on other forms"},
	"total_withheld":                      {"25d", "Total withheld"},
	"estimated_tax_payments":              {"26", "Estimated payments"},
	"earned_income_credit":                {"27a", "Earned income credit"},
	"additional_child_tax_credit":         {"28", "Child tax credit paid out"},
	"american_opportunity_credit":         {"29", "College tuition credit"},
	"refundable_adoption_credit":          {"30", "Adoption credit"},
	"other_refundable_credits":            {"31", "Other credits paid out"},
	"total_other_payments":                {"32", "Other payments and credits"},
	"total_payments":                      {"33", "Total paid"},
	"overpaid":                            {"34", "Paid too much"},
	"refunded":                            {"35a", "Refunded"},
	"applied_to_next_year":                {"36", "Kept for next year"},
	"amount_owed":                         {"37", "Owed"},
	"estimated_tax_penalty":               {"38", "Penalty for paying late"},
}

// form1040Prefix starts every Form 1040 line's path in a result.
const form1040Prefix = "federal.form_1040."

// form1040Line is the line for a path in the result, if it is one of Form
// 1040's.
func form1040Line(path string) (formLine, bool) {
	if !strings.HasPrefix(path, form1040Prefix) {
		return formLine{}, false
	}
	l, ok := form1040Lines[strings.TrimPrefix(path, form1040Prefix)]
	return l, ok
}

// plainNames are labels for field names that humanise can't make plain.
var plainNames = map[string]string{
	"k401": "401(k)", "k403b": "403(b)", "pretax": "Taken out before tax", "gross": "Pay before tax",
	"federal_withheld": "Federal tax withheld", "social_security_withheld": "Social Security withheld",
	"medicare_withheld": "Medicare withheld", "ordinary_dividends": "Dividends",
	"qualified_dividends": "Dividends at the lower rate", "capital_gain_distributions": "Fund gains paid out",
	"section_199a_dividends": "REIT dividends", "short_term_gain": "Gain on shares held a year or less",
	"long_term_gain": "Gain on shares held over a year", "net_profit": "Profit", "receipts": "Money in",
	"expenses": "Costs", "real_estate_taxes": "Property tax", "gifts_by_cash": "Gifts",
	"state_income": "State wages", "tax_withheld": "Tax withheld", "lot_method": "Which shares are sold first",
	"filing_status": "Filing status", "deduction_method": "Standard or itemised",
	"withholding_dates": "When withholding counts", "waiver": "Late-payment penalty waiver",
	"pay_frequency": "How often you're paid", "first_pay_date": "First payday of the year",
	"retirement_plan": "Retirement plan", "roth_option": "Plan takes Roth contributions",
	"supplemental": "How bonuses are withheld", "employer_hsa": "Employer's HSA contributions",
	"covered_by_plan": "Covered by a workplace plan", "months_lived_with_you": "Months lived with you",
	"date_of_birth": "Date of birth", "hsa_coverage": "HSA health plan", "hsa_months": "Months with that plan",
	"full_time_student": "Full-time student", "permanently_disabled": "Permanently disabled",
	"holdings": "", "amounts": "", "states": "", "last_year_december": "Last December's fund payout",
	"carries_forward": "Carried to this year", "from_paystubs": "Paystubs", "from_deposits": "Bank deposits",
	"from_ledger": "Bank entries", "from_credits": "Credits from the bank", "from_statements": "Statements",
	"from_trades": "Trades", "from_forms": "Forms", "from_form_1098": "Form 1098", "from_bill": "Bills",
	"gift_from_bank": "Gifts from the bank", "gift_with_receipt": "Gifts with a receipt",
	"paid_from_bank": "Payments from the bank", "paid_with_receipt": "Payments with a receipt",
	"one_off": "One-time amount", "rest_of_year": "Rest of the year", "year_end_distribution": "Year-end fund payout",
	"planned_sale": "A sale I plan", "planned_gain": "A gain I expect", "lots": "Lots", "clients": "Client",
}

// enumNames are labels for enum values, by name without the enum's prefix,
// that enumLabel can't make plain.
var enumNames = map[string]string{
	"FIRST_IN_FIRST_OUT": "Oldest shares first", "SPECIFIC_LOTS": "Shares you pick",
	"SEMI_MONTHLY": "Twice a month", "BIWEEKLY": "Every other week", "FLAT": "Flat 22%",
	"AGGREGATE": "With the regular paycheck", "SHORT_TERM_CAPITAL_LOSS": "Short-term investment loss",
	"LONG_TERM_CAPITAL_LOSS": "Long-term investment loss", "CHARITABLE_CONTRIBUTION": "Gifts to charity",
	"1099_NEC": "1099-NEC", "1099_K": "1099-K", "1099_MISC": "1099-MISC",
}

// stateNames are the states' names by postal code.
var stateNames = map[string]string{
	"AL": "Alabama", "AK": "Alaska", "AZ": "Arizona", "AR": "Arkansas", "CA": "California", "CO": "Colorado",
	"CT": "Connecticut", "DE": "Delaware", "DC": "District of Columbia", "FL": "Florida", "GA": "Georgia",
	"HI": "Hawaii", "ID": "Idaho", "IL": "Illinois", "IN": "Indiana", "IA": "Iowa", "KS": "Kansas",
	"KY": "Kentucky", "LA": "Louisiana", "ME": "Maine", "MD": "Maryland", "MA": "Massachusetts",
	"MI": "Michigan", "MN": "Minnesota", "MS": "Mississippi", "MO": "Missouri", "MT": "Montana",
	"NE": "Nebraska", "NV": "Nevada", "NH": "New Hampshire", "NJ": "New Jersey", "NM": "New Mexico",
	"NY": "New York", "NC": "North Carolina", "ND": "North Dakota", "OH": "Ohio", "OK": "Oklahoma",
	"OR": "Oregon", "PA": "Pennsylvania", "RI": "Rhode Island", "SC": "South Carolina", "SD": "South Dakota",
	"TN": "Tennessee", "TX": "Texas", "UT": "Utah", "VT": "Vermont", "VA": "Virginia", "WA": "Washington",
	"WV": "West Virginia", "WI": "Wisconsin", "WY": "Wyoming",
}

// stateName is a state's name, or its code when it has none here.
func stateName(code string) string {
	if n, ok := stateNames[code]; ok {
		return n
	}
	return code
}

// moneyText writes an amount as the page does: "$1,234", with cents only
// when there are any, and "−$80" for a negative amount.
func moneyText(m *commonv1.Money) string {
	minor := m.GetMinor()
	sign := ""
	if minor < 0 {
		sign, minor = "−", -minor
	}
	whole := fmt.Sprint(minor / 100)
	for i := len(whole) - 3; i > 0; i -= 3 {
		whole = whole[:i] + "," + whole[i:]
	}
	if c := minor % 100; c != 0 {
		return fmt.Sprintf("%s$%s.%02d", sign, whole, c)
	}
	return sign + "$" + whole
}

// shortDate writes a day as "Aug 31".
func shortDate(d *commonv1.Date) string {
	months := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	if d.GetMonth() < 1 || d.GetMonth() > 12 {
		return ""
	}
	return fmt.Sprintf("%s %d", months[d.GetMonth()-1], d.GetDay())
}

// joinAnd joins words as a list: "a, b and c".
func joinAnd(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	}
	return strings.Join(words[:len(words)-1], ", ") + " and " + words[len(words)-1]
}

// plural is n and a noun, made plural when n isn't 1.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// addNames are the labels of the buttons that add a part, by its kind.
var addNames = map[string]string{
	"hsa_contribution": "+ HSA contribution", "ira_contribution": "+ IRA contribution",
	"max_out_workplace_plan": "+ Max out the workplace plan", "workplace_plan_total": "+ Workplace plan total for the year",
	"year_amount": "+ The year's amount", "one_off": "+ One-time amount", "planned_payment": "+ A payment",
}

// addLabel is the label of the button that adds a part of a kind.
func addLabel(kind string) string {
	if l, ok := addNames[kind]; ok {
		return l
	}
	return "+ " + humanise(kind)
}

// properWords keep their capital inside a sentence.
var properWords = []string{"Roth", "Social", "Medicare", "Treasury", "US", "IRA", "HSA", "W-2", "California"}
