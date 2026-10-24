# oregontaxcalc

Oregon personal income tax, checked against all 502 bands of Oregon's own tax
table, and Oregon estimated tax for 2026 (see [below](#estimated-tax-for-2026)).

It needs two federal figures rather than one, because Oregon is among the few
states that let you subtract part of the federal income tax you paid. Hand it
adjusted gross income and the federal tax, and it gives back Oregon taxable
income, the tax on it, and the working.

```go
subtraction, err := oregontaxcalc.FederalTaxSubtraction(2025, filing.Single,
    12_000*money.Dollar, 95_000*money.Dollar)

income, err := oregontaxcalc.TaxableIncome(2025, filing.Single, 95_000*money.Dollar,
    oregontaxcalc.Conditions{},
    oregontaxcalc.Adjustments{FederalTaxSubtraction: subtraction.Value()})

tax, err := oregontaxcalc.OrdinaryIncomeTax(2025, filing.Single, income.Value().Amount)
```

```
federal AGI     $95000.00
deduction       $2835.00
federal tax     $8500.00 subtracted, capped at $8500.00
taxable         $83665.00
tax             $7010.00
exemptions     -$256.00
owed            $6754.00

why, each bracket rounded to a whole dollar as Oregon does:
  4.75% on $4400.00 = $209.00
  6.75% on $6700.00 = $452.00
  8.75% on $72565.00 = $6349.00
```

That is [`example_test.go`](example_test.go), and its output is checked by the
suite.

Note which line is largest. The federal tax subtraction is $8,500 and the
standard deduction is $2,835 — on most Oregon returns the federal tax is the
biggest thing you subtract, and it is capped and then phased out entirely by
income.

## Oregon rounds each bracket, not the total

This is the thing most likely to be got wrong, and no document Oregon publishes
states it in words.

Oregon does not compute the tax exactly and round the answer. It rounds **each
bracket** to a whole dollar and sums those. For a joint filer at $50,000 the
exact bracket amounts are $418.00, $904.50 and $2,432.50, which sum to
$3,755.00. Oregon's published rate chart says **$3,756**.

Rounding the total gives $3,755 and disagrees with every figure Oregon prints.
Rounding each bracket gives $3,756 and agrees with all four published rate chart
bases and all 1,004 tax table cases. That is what
[`roundToDollar`](tax.go) implements, and it was derived from the published data
rather than from prose.

## The exemption credit is a cliff, not a taper

Above $100,000 of federal adjusted gross income — $200,000 on a joint return —
the exemption credit is not reduced. It is gone. A family of four one dollar
over the limit loses $1,024.

That is Oregon's rule rather than a simplification here, and it is worth
surfacing to anybody near the line.

## What it computes

| | Rule | Code |
|---|---|---|
| Income tax | ORS 316.037, Charts S and J | [`tax.go`](tax.go) |
| Taxable income | ORS 316.048, from federal AGI via Schedule OR-ASC | [`income.go`](income.go) |
| Standard deduction | ORS 316.695(9), with the age and blindness additions | [`income.go`](income.go) |
| Federal tax subtraction | ORS 316.680(1)(b), capped and phased out | [`tax.go`](tax.go) |
| Exemption credit | ORS 316.085, with the income cliff | [`tax.go`](tax.go) |

A separately filing spouse who itemises reduces the other spouse's standard
deduction to **zero** — not to the itemised total, to nothing. `Conditions.SpouseItemises`
is how you say so.

There is deliberately no `LongTermGainsTax`. Oregon taxes a long-term gain at
the same rates as wages, so fold it into taxable income.

## Where the numbers come from

The rate charts, the standard deduction and the federal tax subtraction phase-out
are the [2025 Form OR-40
instructions](https://www.oregon.gov/dor/forms/FormsPubs/form-or-40-inst_101-040-1_2025.pdf).
The bracket thresholds and the exemption credit are [Publication
OR-17](https://www.oregon.gov/dor/forms/FormsPubs/publication-or-17_101-431_2025.pdf),
whose four-year table has 2025 in its last column. Text extracted from both is
checked in beside the tests; see [`testdata/SOURCES.md`](testdata/SOURCES.md).

The strongest check is the tax table. Oregon prints its own answer for every
band of taxable income from $0 to $50,000 across two columns, and the suite
checks all of it: **1,004 cases over 502 bands**, compared against this
library's arithmetic.

The extract is scraped from a PDF, which is brittle, so the suite asserts it is
complete rather than assuming it. That check caught a real failure: the first
version of the filter kept rows that *began* with a band, which is how the
federal tax table is shaped and not how Oregon's is. It silently dropped the
whole $0 to $4,000 column — 115 bands — and looked healthy. Oregon's bands are
not a constant width either, $20 near zero and $100 higher up, so what the test
requires is continuity: every band's floor equal to the previous band's ceiling,
from $0 to exactly $50,000.

## Estimated tax for 2026

`EstimatedTax(2026)` implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface: whether a
household has to make Oregon estimated payments, what is due by each date, and
the underpayment interest if the payments go as planned. The caller supplies
the year's tax after all credits.

| Rule | Oregon | Where |
|---|---|---|
| Who must pay | $1,000 or more of tax after credits less withholding | [Publication OR-ESTIMATE](https://www.oregon.gov/dor/forms/FormsPubs/publication-or-estimate_101-026_2026.pdf) |
| The two tests | 90% of this year's tax after credits, or 100% of last year's at every income. No 110% tier | OR-ESTIMATE, Method 1 |
| Due dates | April 15, June 15, September 15, January 15, moved for Oregon legal holidays (ORS 187.010) | OR-ESTIMATE; `EstimatedCalendar` |
| A late start | 3, 2 or 1 equal installments | OR-ESTIMATE; ORS 316.579 |
| No interest | Under $1,000; no tax last year as a full-year resident; two-thirds of gross income from farming or fishing | [Form OR-10 instructions](https://www.oregon.gov/dor/forms/FormsPubs/form-or-10-instructions_101-031-1_2025.pdf) |
| The interest | 8% for 2026 at 0.000219 a day, on a running balance between events, each line rounded to a whole dollar, to April 15 | Form OR-10 instructions, line 4 |

Publication OR-ESTIMATE's Example 3 (Allan and Louise: $3,912 of tax, $721 of
withholding, $700 a quarter) and the Form OR-10 instructions' running-balance
example (Catelyn: $19, $3 and $11) are both tests. The code is in
[`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

Each result's `Divergences` lists six places where the department's documents
and the statutes or rules read differently, among them the late-start
installments, the gross income test the forms leave out, and the printed daily
factor. Oregon's own facts, whether last year's return was filed first and
whether the gross income test is met, go in `EstimatedFacts`. A fact left out
is reported in `Required` with the default used. A part-year resident's
required annual payment is divided over the installment periods of Oregon
residence or Oregon business income, as the Form OR-10 instructions' note to
Line 3 says. A period runs from the day after the previous due date, and one
with any day of residence counts; the instructions don't settle a partial
period, and `Required` says so.

## What comes next

- **More tax years.** 2025 only. `TableFor` returns `ErrNoTable` for anything
  else rather than reaching for an adjacent year's brackets.
- **Forms OR-40-P and OR-40-N.** Every figure here is a full-year resident's.
  `TaxableIncome` returns the residency question unanswered rather than assuming
  it.
- **Credits beyond the exemption credit.** The Oregon Kids Credit, the working
  family household and dependent care credit, and the retirement income credit
  are not computed.
- **The Oregon surplus refund.** The kicker is a credit in odd-numbered filing
  years and depends on a figure the state announces after the fact, so it is not
  modelled.
- **Local taxes.** Multnomah County's preschool tax and the Portland Metro
  supportive housing tax are separate levies on the same income and are not
  here.

## This is software, not tax advice

It computes what the published rules say for the figures you hand it. It does
not know your circumstances, and it is not a substitute for a professional.
