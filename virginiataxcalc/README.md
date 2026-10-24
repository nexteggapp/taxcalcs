# virginiataxcalc

Virginia estimated individual income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the addition to tax if
the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Virginia income tax calculator here yet.** The caller supplies
the year's tax.

## Virginia measures each installment on its own date

| Rule | Virginia | Where |
|---|---|---|
| Who must pay | Tax after withholding and credits of more than $1,000; nobody below $11,950 of VAGI ($23,900 jointly) | [Form 760ES (2026)](https://www.tax.virginia.gov/sites/default/files/taxforms/individual-income-tax/2026/760es-2026.pdf), Section I |
| The installments | A quarter of the lesser of 90% of this year's tax and 100% of last year's, due May 1, June 15, September 15 and January 15. No 110% tier | Code § 58.1-492(A)(1); Form 760ES Section II |
| Payment order | A payment on an installment date pays that installment first, then the oldest shortfall | § 58.1-492(B)(2); 23VAC10-112-42, Example 2 |
| The exceptions | Last year's tax, last year's income at this year's rates, annualized income, income to date. Each cancels one installment's addition; none lowers a shortfall | [Form 760C (2025)](https://www.tax.virginia.gov/sites/default/files/taxforms/individual-income-tax/2025/760c-2025.pdf), Part II |
| Small amounts | No addition when the year's underpayments total $1,000 or less | § 58.1-492(D)(3), as amended by 2026 Acts c. 7 |
| The rate | 8% for April to June 2026 and 9% for July to December; 9% is charged at .00025 a day | Tax Bulletins 26-2 to 26-4; Form 760C line 23 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## An example

$10,000 of tax, $3,000 withheld, $8,000 of tax last year, and $1,000 paid on
May 1 and $2,000 on September 15. The installments are $2,000 each. The
shortfalls are $250 in May, $1,250 in June and $1,250 in January. September's
payment pays September first, then May and part of June. The addition to May
1, 2027 is $56.34, charged to December 31, 2026, because the rate for 2027 is
not published:

```
LINE 1 $250.00  137 days $8.10   ($3.29 at 8% for 60 days, $4.81 at .00025 for 77)
LINE 2 $1250.00 320 days $48.24
LINE 4 $1250.00 106 days $0.00   (all in 2027, reported as unrated)
```

## The forms are followed, and where the Code differs is reported

Each result's `Divergences` lists seven places where the Department's forms
and the Code or its regulations read differently. One of them is the 2025
Form 760C's $150 small-underpayment figure, which the 2026 law replaced with
$1,000. That is the law, computed in place of the form. The research behind
each is in
[`research/estimated-payments/va.md`](../research/estimated-payments/va.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
va, err := virginiataxcalc.NewEstimatedFacts().
    PriorYearIncomeTax(7_500 * money.Dollar).              // Form 760C Exception 2 Worksheet, line 7
    IncomeToDateTax(1_200*money.Dollar, 1_900*money.Dollar, // Form 760C line 19f,
        4_100*money.Dollar).                               // to April 30, May 31, August 31
    MerchantSeamanShare(0).
    Build()
```

A fact left out is reported in `Required` with the default used.

## Not built

Farmers, fishermen and merchant seamen (Form 760F) get
`estimated.ErrUnsupported`. Fiscal years, estates and trusts are not built.
