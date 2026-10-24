# alabamataxcalc

Alabama estimated individual income tax for 2026: whether a household has to
make estimated payments, what is due by each date, and the underpayment penalty
if the payments go as planned. It implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface.

**There is no Alabama income tax calculator here yet.** The caller supplies the
year's tax, Form 2210AL Part I line 3.

## Alabama adopts the federal rules, with a $500 threshold

[Code of Ala. § 40-18-80](testdata/al-code-40-18-80.txt) says individuals pay
estimated tax under 26 U.S.C. § 6654, with $500 in place of $1,000. This package
imports no other jurisdiction, so the § 6654 arithmetic is written here again.

| Rule | Alabama | Where |
|---|---|---|
| Who must pay | $500 or more of tax after withholding and credits | [Form 40ES instructions (2026)](https://www.revenue.alabama.gov/wp-content/uploads/2026/01/26f40esinstr.pdf) |
| The two tests | 90% of this year's tax, or 100% of last year's on a filed twelve-month return; 110% above $150,000 of last year's Alabama AGI ($75,000 separately) | Form 40ES instructions; [Form 2210AL (2025)](https://www.revenue.alabama.gov/wp-content/uploads/2026/01/25f2210al.pdf) Part I |
| Due dates | A quarter each on April 15, June 15, September 15 and January 15; a Saturday, Sunday or State holiday moves to the next business day | Form 40ES instructions |
| The penalty | Each shortfall × days ÷ 365 × the department's quarterly rate, to April 15 of the next year | Form 2210AL line 11; [Quarterly Interest Rates](https://www.revenue.alabama.gov/assessments/quarterly-interest-rates/) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

## A worked example

$40,000 of 2026 tax, and $20,000 of 2025 tax on $100,000 of Alabama AGI. The
target is the smaller of $36,000 and $20,000, so $5,000 a quarter. Nothing is
paid in April; $5,000 on June 15 pays April's quarter, and $5,000 on August 14
pays June's.

```go
r, _ := alabamataxcalc.EstimatedTax(2026)
p, _ := r.Penalty(facts, estimated.MustDate(2027, time.April, 15))
p.Value().Total() // $105.62
```

April's $5,000 is 61 days at 6%, $50.14. June's is 15 days at 6% and 45 days at
7%, $12.33 and $43.15. `TestEstimatedPenaltyAcrossTwoRatePeriods` checks it.

## The forms are followed, and where the statute differs is reported

Each result's `Divergences` lists six places where the department's forms and
§ 6654, as § 40-18-80 adopts it, read differently:

- **The rate.** The 2025 Form 2210AL prints 7% for the whole year. For 2026
  the department publishes 6% for April to June and 7% after, and those are
  used quarter by quarter.
- **The 110% test.** The 2025 Form 2210AL instructions test the penalty year's
  AGI. The 2026 Form 40ES and the statute test last year's, which is used.
- **January 31.** The statute excuses the January payment for a return filed
  and paid by January 31. The forms do not, and it is not applied.
- **No liability last year.** The statute excuses the penalty after a year with
  no liability. The form does not, and it is not applied.
- **A late start.** The 40ES lets a late starter pay a third or a half at once.
  Form 2210AL charges a quarter by every date, which is followed.
- **Nonresident aliens.** The statute gives them three installments. The forms
  do not, and it is not applied.

The research behind each is in
[`research/estimated-payments/al.md`](../research/estimated-payments/al.md).

## What it needs that no other state does

`EstimatedFacts`, carried in `estimated.Facts.State()`:

```go
al, err := alabamataxcalc.NewEstimatedFacts().
    PriorYearFarmerOrFisher(false). // two-thirds of 2025 gross income from farming or fishing
    Build()
```

Form 2210AL sends a filer to its farmers' part when either year's income
qualifies. Left out, it is reported in `Required` and taken as no.

## Not built

Farmers and fishermen (Form 2210AL Part IV) get `estimated.ErrUnsupported`.
Whether § 6654's "adjusted gross income" means federal or Alabama AGI is
unsettled; Alabama AGI is used, as the 40ES says, and the question is reported
when the answer could change the percentage. Local occupational taxes are not
covered.
