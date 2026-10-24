// Package connecticuttaxcalc computes Connecticut's estimated income tax for
// 2026: whether estimated payments are needed, what is due by each date, and
// the interest Connecticut charges on an underpayment.
//
// # Only estimated tax so far
//
// There is no income tax calculator here yet. A caller passes the year's tax
// in as [estimated.Figures.Tax]: the income tax shown on Form CT-1040 line 14
// (Form CT-1040NR/PY line 16), which is after the credits on Schedule CT-IT
// Credit and before withholding, estimated payments and the Pass-Through
// Entity Tax Credit. That last credit comes in [EstimatedFacts].
//
// # How Connecticut differs
//
// The tests are the federal ones without the 110% step: 90% of this year's
// tax or 100% of last year's, four equal quarters, no charge under $1,000.
// The charge is interest at 1% for each month or part of a month, set by
// statute rather than indexed, and Connecticut calls it interest, not a
// penalty. The Pass-Through Entity Tax Credit (PE Tax Credit), the credit for
// tax a partnership or S corporation paid on a member's share, counts like
// withholding and is spread over the quarters. The no-liability exception
// needs residence last year or Connecticut-source income then. There is no
// waiver for casualty, disaster, retirement or disability.
//
// # What is not built
//
// Farmers and fishermen, who make one payment by January 15 under Conn. Gen.
// Stat. § 12-722(m), get [estimated.ErrUnsupported]. Trusts and estates,
// including the two-year exemption for a decedent's estate in § 12-722(n),
// are not covered: [estimated.Facts] carries an individual's filing status.
// No Connecticut municipality's income tax is covered; the research found
// none, without confirming that none exists.
//
// Start at [EstimatedTax].
package connecticuttaxcalc
