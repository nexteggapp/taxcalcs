# rhodeislandtaxcalc

Rhode Island estimated personal income tax for 2026: whether a household has
to make estimated payments, what is due by each date, and the interest charged
on an underpayment if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Rhode Island income tax calculator here yet.** The caller
supplies the year's tax after credits, as RI-2210 line 1 takes it.

## A worked example

$6,000 of 2026 tax, $2,000 withheld, $4,400 of tax on the 2025 return, and
$500 paid on each due date. 100% of last year's tax is less than 80% of this
year's, so the target is $4,400, $1,100 a quarter. Withholding counts $500 on
each date, and each date's $1,000 pays the oldest shortfall first, so the
shortfalls are $100, $200, $300 and $400, the first three paid on the next
date. The charge to April 15, 2027 is **$18.61**, for the days of 2026. The
rate from January 1, 2027 is not published, and the result says so in
`Required`. `TestEstimatedReadmeExample` reproduces it.

## Rhode Island is federal in shape, not in numbers

| Rule | Rhode Island | Where |
|---|---|---|
| Who must pay | $250 or more of tax after credits | [RI-1040ES (2026)](https://tax.ri.gov/sites/g/files/xkgbur541/files/2026-01/2026%20RI-1040ES_w.pdf); R.I. Gen. Laws § 44-30-55(a) |
| The target | The smaller of 80% of this year's tax and 100% of last year's; no 110% tier | [RI-2210 (2025)](https://tax.ri.gov/sites/g/files/xkgbur541/files/2026-01/2025%20RI-2210_w.pdf) lines 2, 5 and 6 |
| Due dates | April 15, June 15, September 15, January 15; they move for weekends and Rhode Island holidays, and none does in 2026 | RI-1040ES (2026); §§ 44-30-91(b), 25-1-1 |
| No charge | Tax less withholding of $250 or less; no federal liability last year as a full-year resident | RI-2210 lines 4 and 5 |
| Annualised | 20%, 40%, 60% and 80% of the tax on annualised income | RI-2210 Part 5 |
| The charge | Simple interest by the day at 12% for 2026, to April 15 | [ADV 2025-23](https://tax.ri.gov/sites/g/files/xkgbur541/files/2025-11/ADV_2025_23_Interest_Rates.pdf); RI-2210 Part 4; § 44-1-7 |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## Where the forms and the statute differ, both are reported

Each result's `Divergences` lists six: the 100% prior-year option, which only
the forms create; the $250 floor; the no-liability test, federal on the form and
Rhode Island in the statute; the return filed in place of the January
installment; a requirement first met after April 15; and the $2 minimum in
§ 44-30-84(a). The research behind them is in
[`research/estimated-payments/ri.md`](../research/estimated-payments/ri.md).

## Two open questions change a number

The RI-2210 counts a return filed and paid by January 31 toward the January
installment. The RI-1040ES says February 15, which is February 16, 2027 after
Washington's Birthday. A return filed between the two is figured on the
RI-2210 date and the question is reported. Whether the $2 minimum reaches
estimate interest is reported when the charge is under $2. `EstimatedFacts`
answers both:

```go
ri, err := rhodeislandtaxcalc.NewEstimatedFacts().
    PriorYearNoFederalLiability(false).                     // RI-2210 line 5
    JanuaryReturn(rhodeislandtaxcalc.JanuaryReturnCoupon).  // or JanuaryReturnPenaltyForm
    TwoDollarMinimum(true).                                 // § 44-30-84(a)(1)
    Build()
```

## Not built

Farmers and fishermen get `estimated.ErrUnsupported`. Estates and trusts are
not covered. The RI-2210A shortcut is not offered; the full computation gives
the same answer, apart from the rounding of its printed factors.
