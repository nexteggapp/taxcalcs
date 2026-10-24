# utahtaxcalc

Utah has no estimated tax for individuals. This package answers the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) questions with Utah's one
prepayment rule, the extension test, so `estimated/all.For("UT", 2026)` can
find it.

**There is no Utah income tax calculator here yet.** The caller supplies the
year's tax.

## Utah tests one date, and only for an extended return

| Rule | Utah | Where |
|---|---|---|
| Who must pay during the year | Nobody. A return filed and paid by April 15, 2027 owes no prepayment penalty | [TC-40 instructions (2025)](https://files.tax.utah.gov/tax/forms/current/tc-40inst.pdf) |
| The extension test | By April 15, 2027, the lesser of 90% of 2026 tax or 100% of 2025 tax, to file by October 15, 2027 | Utah Code § 59-10-516(2)(b); [TC-546](https://files.tax.utah.gov/tax/forms/current/tc-546.pdf) |
| The penalty | 2% a month of all the tax unpaid on April 15, by the day, to at most 12% | Utah Code § 59-1-401(5)(b); [Publication 58](https://files.tax.utah.gov/tax/forms/pubs/pub-58.pdf) |
| Withholding | Counts in full, whenever withheld | TC-40 instructions (2025) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). Whether the filer will use the
extension is carried in `EstimatedFacts`.

## The Commission's documents are followed, and where the Code differs is reported

Each result's `Divergences` lists four places where the documents and the Utah
Code read differently: last year's tax of zero, no return last year, the
penalty by the day rather than by the month, and the penalty charged on the
whole unpaid tax. The 90% Publication 58 asks of a filer with no return last
year is also an open question in `Required`, because the research could not
find how the Commission assesses it.

## What is not built

Interest and the late-payment penalty on a balance paid after April 15, and the
penalties for a return filed after the extension ends, which gets
`estimated.ErrUnsupported`.
