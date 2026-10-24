# washingtontaxcalc

**Washington has no personal income tax.** This package computes the one tax on
gain that it does levy: an excise tax on the sale or exchange of long-term
capital assets, under [chapter 82.87
RCW](https://app.leg.wa.gov/rcw/default.aspx?cite=82.87).

So there is no `OrdinaryIncomeTax` here, no rate table by filing status, and no
taxable income function. Not unimplemented — Washington does not levy the tax
they would compute. A stub returning zero would let a caller loop over three
states, get a number from each, and never learn that one of those numbers means
something different.

```go
tax, err := washingtontaxcalc.ExciseTax(2025, 2_278_000*money.Dollar,
    washingtontaxcalc.Deductions{},
    washingtontaxcalc.Facts{}) // nothing asserted, so both questions come back
```

```
Washington gain  $2278000.00
deduction       -$278000.00
taxable          $2000000.00
tax              $169000.00

why:
  RCW 82.87.060(1), as adjusted for 2025
  RCW 82.87.040(1)(a)
  RCW 82.87.040(1)(b), as enacted by ESSB 5813, ch. 421, Laws of 2025

unanswered:
  capital asset situs
  lot selection
```

That is [`example_test.go`](example_test.go), and its output is checked by the
suite.

## Two rates since 2025, and the second is on top of the first

The tax was a flat 7% from its creation in 2021. [ESSB
5813](https://dor.wa.gov/forms-publications/publications-subject/special-notices/new-tiered-rates-washingtons-capital-gains-tax),
chapter 421 of the Laws of 2025, added 2.9% on gains above one million dollars
from 1 January 2025.

**In addition to**, not instead of. The top of a large gain meets 9.9%. In the
example above, $2,000,000 of taxable gain is $140,000 at 7% plus $29,000 at
2.9% on the million above the threshold.

The threshold is measured against gain **after** the standard deduction, not
against the gross gain. A $1,278,000 gain is exactly $1,000,000 taxable and owes
no surcharge at all.

## Most people who have gains owe nothing

The standard deduction for 2025 is $278,000 — one deduction per individual,
married couple, or domestic partnership, so a couple get the same amount as one
person rather than twice it. Below it, no tax is due and no return is required.

Real estate is exempt outright, and so are retirement accounts, timber,
commercial fishing privileges, livestock and depreciable business assets.

## What this package cannot tell you

Whether a gain is Washington's at all.

[RCW 82.87.100](https://app.leg.wa.gov/rcw/default.aspx?cite=82.87.100)
allocates gain on intangible property by where the individual was **domiciled
when the sale settled** — not by where the asset was, where the broker was, or
where the company is. That is a fact about the seller, and no figure you pass in
carries it.

So `ExciseTax` takes Washington capital gains, already allocated and already
stripped of the exempt classes, and returns the allocation question in
`Result.Required()` rather than pretending it did the filtering. Hand it a federal
Schedule D total and you will get a confident, wrong answer.

Once you have done that work, say so and the question stops coming back:

```go
washingtontaxcalc.Facts{GainAllocated: true, LotsIdentified: true}
```

Answering changes nothing about the arithmetic — it is a statement about the
filer, not about the figures — and the suite checks that the tax does not move.

## What it computes

| | Rule | Code |
|---|---|---|
| Excise tax | RCW 82.87.040(1)(a), 7% | [`excise.go`](excise.go) |
| Surcharge | RCW 82.87.040(1)(b), a further 2.9% above $1,000,000 | [`excise.go`](excise.go) |
| Standard deduction | RCW 82.87.060(1), $278,000 for 2025 | [`y2025.go`](y2025.go) |
| Charitable deduction | Donations above $278,000, capped at $111,000 | [`excise.go`](excise.go) |
| Family business | RCW 82.87.070, deducted in full | [`excise.go`](excise.go) |

## Where the numbers come from

The surcharge is the Department of Revenue's [Summary of 2025
Legislation](https://dor.wa.gov/sites/default/files/2025-06/2025_Summary_of_Legislation.pdf).
The annual amounts are the Department's [capital gains tax
page](https://dor.wa.gov/taxes-rates/other-taxes/capital-gains-tax), which is
where they are published — the statute directs an inflation adjustment and
states no figure.

**One of those is a web page, and that is weaker than the other two states'
sources.** It has no revision, no date on its face, and it can change with
nothing to diff against. It was the best primary source available: the
Department's own overview PDF still prints the 2024 figure, and the return
instructions on the site are the 2022 edition. See
[`testdata/SOURCES.md`](testdata/SOURCES.md) for what was checked and rejected.

The tests are also weaker than California's and Oregon's, and for a reason worth
stating. Those states publish tables of their own answers, so a test can compare
arithmetic against arithmetic across thousands of cases. Washington publishes
none, because the tax is two multiplications and there is nothing to tabulate.
What is checked here is that the rates, the threshold and the deductions are the
ones the Department states — by requiring the *sentence*, since the page carries
2024's figures in the same paragraph — and that the edges behave.

## Paying it: one payment by April 15, no quarterly estimates

`EstimatedTax(2026)` implements the shared
[`estimated.EstimatedTax`](../estimated/estimated.go) interface. Washington's
version is short, because the tax is paid once:

| Rule | Washington, tax year 2026 | Where |
|---|---|---|
| When it is due | The federal return's due date, April 15, 2027. An extension does not move it | [RCW 82.87.110(3)](https://app.leg.wa.gov/RCW/default.aspx?cite=82.87.110), [special notice](https://dor.wa.gov/forms-publications/publications-subject/special-notices/prepayment-capital-gains-tax) |
| Paying early | From October 15, 2026 | [RCW 82.87.115(1)](https://app.leg.wa.gov/RCW/default.aspx?cite=82.87.115) |
| Paying late | 9% of the late tax, 19% after the end of May, 29% after the end of June; at least $5 | [RCW 82.32.090(1)](https://app.leg.wa.gov/RCW/default.aspx?cite=82.32.090) |
| With an extension and a timely estimate | No late payment penalty. 5% of the whole tax if the estimate was under 80% of it and $1,000 or more short | RCW 82.32.090(10) |
| Interest | From April 30, 2027, at 6% for 2027 | [RCW 82.32.050(1)(c)(ii)](https://app.leg.wa.gov/RCW/default.aspx?cite=82.32.050), [interest rate tables](https://dor.wa.gov/file-pay-taxes/late-filing/interest-rate-tables) |

The code is in [`estimated.go`](estimated.go), and the 2026 figures in
[`estimated_y2026.go`](estimated_y2026.go).

**Where the department's guidance predates the law, the law is followed.** The
5% penalty on a low estimate and the April 30 start for interest came in with
SSB 5314, in effect from July 27, 2025. The department's interim statement on
late payment penalties (April 7, 2023) and WAC 458-20-228 (certified February
29, 2024) describe the rules before it. Each result's `Divergences` reports
those two and one more, with both sources.

**Some readings are open, and each is reported in `Required` with the default
used:** whether "at least $1,000" is the shortfall or the payment; whether the
19% step moves off Memorial Day, May 31, 2027; how a part payment counts at the
later steps; and how interest days are counted. The one fact a caller can
supply, whether there was an extension, goes in `EstimatedFacts`:

```go
wa, err := washingtontaxcalc.NewEstimatedFacts().Extension(true).Build()
```

Not built: the late filing penalty, and last year's overpayment applied
forward, which gets `estimated.ErrUnsupported`.

## What comes next

- **More tax years.** 2025 only. 2024 in particular would be wrong twice: the
  deduction was $270,000 and there was no surcharge, so `TableFor` refuses it.
- **Allocation.** See above. It needs facts about the seller, not the sale.
- **The credits.** The business and occupation tax credit and the credit for
  income or excise tax paid to another jurisdiction are not computed.

## This is software, not tax advice

It computes what the published rules say for the figures you hand it. It does
not know your circumstances, and it is not a substitute for a professional.
