// Package missouritaxcalc computes Missouri's estimated income tax for 2026:
// whether estimated payments are needed, what is due by each date, and the
// addition to tax for underpayment.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the tax in as
// [estimated.Figures.Tax], Form MO-1040 line 36 less the credits on lines 42
// to 44, as MO-2210 Part I line 1 takes it.
//
// # How Missouri differs
//
// The prior-year test is always 100% and needs a twelve-month return showing
// a liability. Four exceptions excuse an installment whose payments reach
// them, including last year's income at this year's rates. A filer who first
// meets the requirement after April 1 pays fewer installments. The rate is
// set for each calendar year, 7% for 2026 and 2027, and there is no waiver
// for reasonable cause.
//
// # What is not built
//
// Farmers, who pay once at 66 2/3% or file and pay by March 1, get
// [estimated.ErrUnsupported]. Missouri's farm rule covers farming alone, so a
// fisher is under the general rules. The optional short method is not
// computed. Kansas City's and St. Louis's earnings taxes are not covered.
//
// Start at [EstimatedTax].
package missouritaxcalc
