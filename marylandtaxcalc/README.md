# marylandtaxcalc

Maryland estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the interest on an
underpayment if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Maryland income tax calculator here yet.** The caller supplies the
year's Maryland and local tax, Form 502UP line 7, and the local part of it.

## Maryland is shaped differently from federal

| Rule | Maryland | Where |
|---|---|---|
| Who must pay | More than $500 of Maryland and county tax after Maryland withholding | [PVW (2026)](https://www.marylandcomptroller.gov/content/dam/mdcomp/tax/forms/worksheets/2025-PV-Worksheet.pdf); Tax-Gen. § 10-815 |
| The target | The lesser of 90% of this year's tax and 110% of last year's, at every income; a first-time filer uses 90% | [Form 502UP (2025)](https://www.marylandcomptroller.gov/content/dam/mdcomp/tax/forms/2025/502up.pdf) lines 8 to 10; § 13-602(a) |
| Due dates | A quarter by April 15, June 15, September 15 and January 15; a return filed and paid by January 31 (February 1 in 2027) replaces the last | PVW; §§ 1-201, 10-820, 10-902 |
| Withholding | On the dates withheld, not spread evenly | Form 502UP line 14 |
| Uneven income | The target in the shares of the year's income received by March 31, May 31, August 31 and December 31 | Form 502UP lines 11 to 13 |
| No interest | Tax after withholding of $500 or less; or 90% of income taxable by another state and the underpayment no more than the local tax | Form 502UP exceptions |
| The interest | Cumulative: each column's underpayment includes earlier ones still unpaid, charged to the next due date or until paid, to April 15, 2027 | Form 502UP lines 15 to 18 |
| The rate | 10.8133% for 2026, 9.995% for 2027, a twelfth a month, part of a month counting as one | § 13-604; [Md. Register 53:18](https://dsd.maryland.gov/MDRIssues/5318/Assembled.aspx) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## The 2026 interest factors are not published

Form 502UP prints one factor for each span between due dates, worked out from
the days in the span and the year's rate. The 2026 form is not out. The 2025
form's factors are for 2025's rates, so they are not used. Each span is charged
by the month as § 13-604(a) says, and every penalty reports this in `Required`
as an unpublished rate. For 2025's spans the printed factors differed from the
monthly figures by up to 0.0004 of the underpayment.

## The forms are followed, and where the statute differs is reported

Each result's `Divergences` lists six places: the $500 boundary, the
worksheet's condition on 110%, what counts as last year's tax, the printed
factors, the third-period December 15 text, and the 25% penalty the form does
not compute. The research behind each is in
[`research/estimated-payments/md.md`](../research/estimated-payments/md.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
md, err := marylandtaxcalc.NewEstimatedFacts().
    TaxableByAnotherState(false). // at least 90% of taxable income taxed elsewhere
    Build()
```

A fact left out is reported in `Required` with the default used.

## Not built

Farmers and fishermen get `estimated.ErrUnsupported`. The 60-day rule for $500
or more of unwithheld gambling winnings is not applied. The penalty of up to
25% under § 13-702 is not computed.
