# northcarolinataxcalc

North Carolina estimated income tax for 2026: whether a household has to make
estimated payments, what is due by each date, and the interest on underpaid
estimated tax if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no North Carolina income tax calculator here yet.** The caller
supplies the year's tax after credits, Form D-422 line 3.

## North Carolina follows the federal rules, with these differences

| Rule | North Carolina | Where |
|---|---|---|
| Who owes interest | $1,000 or more of tax less withholding, the federal amount adopted by reference | N.C. Gen. Stat. § 105-163.15(f); [D-422](https://www.ncdor.gov/2025-form-d-422-underpayment-estimated-tax-individuals/open) line 6 |
| The two tests | 90% of this year's tax, or 100% of last year's from a filed twelve-month return; no 110% tier | § 105-163.15(d) |
| No-liability exception | No twelve-month or residence condition | § 105-163.15(f) |
| The rate | Set by the Secretary each half-year, 7% for 2026 | § 105-241.21; [Interest Rate](https://www.ncdor.gov/interest) |
| Filed by January 31 | The tax paid with the return counts in the January column | D-422 line 17 |
| Waivers | None for casualty, retirement or disability | § 105-237(a)(2) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go). No 2026 D-422 is published; the
2025 form's arithmetic is used with the 2026 dates and rate.

## The form is followed, and where the statute differs is reported

Each result's `Divergences` lists four places, among them the January
column rule, which counts the return's payment as made on January 15 where
§ 105-163.15(h) only excuses the fourth installment. The research is in
[`research/estimated-payments/nc.md`](../research/estimated-payments/nc.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
nc, err := northcarolinataxcalc.NewEstimatedFacts().
    FarmerLastYear(false).          // two-thirds from farming or fishing last year
    JanuaryThirtyFirstMoves(false). // whether a February 1, 2027 return counts
    Build()
```

A fact left out is reported in `Required` when it changes the figure, with the
default used.

## Not built

Farmers and fishermen get `estimated.ErrUnsupported`. Estates and trusts are
outside the section. North Carolina has no local income tax.
