# Estimated tax

Tax is due as income is earned. Every jurisdiction that taxes income charges
an underpayment penalty when too little was paid during the year, through
withholding and estimated payments. The [`estimated`](../estimated) package
and the per-jurisdiction calculators answer three questions for a household:

- Does it have to make estimated payments, and what total keeps it clear of
  the penalty?
- How much is needed by each due date, and how much has been credited by
  then?
- If payments go as planned, what is the penalty?

Rules are built for tax year 2026, for the federal government, all fifty
states and the District of Columbia. [results.md](results.md#estimated-payments)
describes how `taxyear` runs them for each scenario.

## Finding the rules

Each jurisdiction's rules are code, in that jurisdiction's own package: the
federal rules in [`ustaxcalc`](../ustaxcalc), California's in
[`californiataxcalc`](../californiataxcalc), and so on. Each package has an
`EstimatedTax(year)` constructor. [`estimated/all`](../estimated/all/all.go)
finds any of them:

```go
rules, err := all.For(estimated.California, 2026)
```

A year a package does not carry returns `estimated.ErrNoRules`, never the
nearest year's rules. Facts that need rules this build lacks, such as the
separate rules for farmers and fishermen, return `estimated.ErrUnsupported`.

Every jurisdiction implements one interface:

```go
type EstimatedTax interface {
	Requirement(f Facts) (working.Result[Requirement], error)
	Schedule(f Facts) (working.Result[[]Installment], error)
	Penalty(f Facts, asOf Date) (working.Result[Penalty], error)
	Explanation() Explanation
}
```

## Input

`estimated.Facts` holds what the rules read about one household, for one
jurisdiction and year. Every type has unexported fields and accessors, and is
made by a builder whose `Build` validates it.

```go
d := estimated.MustDate
ca, _ := estimated.NewResidencePeriod("CA").From(d(2026, time.January, 1)).To(d(2026, time.December, 31)).Build()
thisYear, _ := estimated.NewFigures().FederalAGI(money.Dollars(140_000)).Build()
lastFigures, _ := estimated.NewFigures().Tax(money.Dollars(9_800)).FederalAGI(money.Dollars(130_000)).Build()
lastYear, _ := estimated.NewPriorReturn().Filed(true).Figures(lastFigures).Build()
aug, _ := estimated.NewWithheld().On(d(2026, time.August, 14)).Amount(money.Dollars(812)).Build()

facts, err := estimated.NewFacts(estimated.California, 2026).
	FilingStatus(filing.Single).
	Residence(ca).
	ThisYear(thisYear).
	PriorYear(lastYear).
	Withheld(aug).
	Build()
```

| Builder method | Holds |
|---|---|
| `FilingStatus`, `Residence`, `PriorResidence` | The filing status, and dated residence periods this year and last |
| `ThisYear` | This year's `Figures`: tax after credits (optional), federal and state AGI, gross income, taxable income, local tax, withholding |
| `PriorYear` | Last year's return: filed or not, months covered, figures, full-year residence |
| `Withheld`, `Paid` | Dated withholding, and estimated payments made or planned. A payment can be last year's overpayment applied forward. |
| `ReturnFiled` | The day this year's return was filed and paid in full |
| `RequirementFirstMet` | The day the household could first expect to owe estimates |
| `People` | Each filer's age, retirement after 62, disability |
| `FarmShare`, `FishShare` | The share of gross income from farming and fishing |
| `Elections` | The annualised income method; crediting withholding on the dates withheld |
| `Positions` | The household's answers to judgement calls: the waiver asked for, its cause and date |
| `Periods` | Income to each period end and the tax on it annualised, for the annualised method |
| `State` | The jurisdiction's own extras, a type from its package, such as `pennsylvaniataxcalc.EstimatedFacts` |

**Waivers** are positions. `Positions.Waiver` is `WaiverNone`,
`WaiverCasualty` (a casualty, disaster or unusual circumstance) or
`WaiverRetiredOrDisabled` (retirement after 62, or disability, in this year or
last). The tax authority decides a waiver, so the penalty is always figured in
full. `Penalty.WaiverOutcome()` says whether the facts meet the conditions,
and `Penalty.IfWaived()` gives what would remain.

**This year's tax is optional.** Without it, the option measured on this
year's tax is unavailable, and the target is the best option left, usually
last year's tax. The penalty is then an upper bound. When no option can be
measured, `Schedule` and `Penalty` return `estimated.ErrNeedsThisYearTax`.

## Output

Each method returns a `working.Result`. `Value()` is the answer. `Applied()`
cites each rule used, in the household's numbers. `Required()` lists what the
caller has not said, each with a question, its effect, and the default used.
`Divergences()` lists rules on which the jurisdiction's form and its statute
disagree. The rules compute what the current form computes, and report the
statute's reading beside it.

| Type | Accessors |
|---|---|
| `Requirement` | `Required()`, `Why()`, `Target()`, `Options()`, `Chosen()`, `Exempt()`, `TargetKnown()` |
| `TargetOption` | `Basis()` (this year's tax, last year's tax, last year's income, or an exception), `Percent()`, `Amount()`, `Available()`, `WhyNot()`, `Label()` |
| `Installment` | `Number()`, `Due()` (moved for weekends and holidays), `NeededBy()`, `CreditedBy()`, `Shortfall()`, `Status()` |
| `Penalty` | `Total()`, `Reason()`, `Summary()`, `Lines()`, `UpperBound()`, `RatedThrough()`, `WaiverOutcome()`, `IfWaived()` |
| `Explanation` | `Summary()`, `Body()`, `VersusFederal()`, `Sources()` |

An exception that means no payments are needed is an option of basis
`BasisException`, at $0, and is the chosen target. An installment's status is
nothing due, met, excused or short. When a penalty's interest rate is not yet
published for the later days, `RatedThrough()` is the last day covered.

## Rules by jurisdiction, 2026

"Standard" dates are Apr 15, Jun 15 and Sep 15, 2026 and Jan 15, 2027, in
equal shares. Most dates move for weekends, and in some states for state
holidays. "This year" and "Last year" are the safe-harbor shares: paying the
smaller of the two avoids the penalty. "Casualty" and "Retired/disabled" are
the two waivers above. "File by" means filing and paying in full by that date
excuses the January installment. "No tax last year" is an exception for a
household that owed nothing last year.

| | Due dates | This year | Last year | No estimates needed if | Annualised | Waivers and exceptions |
|---|---|---|---|---|---|---|
| US | Standard | 90% | 100%; 110% if AGI over $150k ($75k MFS) | Under $1,000 owed; no tax last year | Yes | Casualty; retired/disabled; file by Jan 31 |
| AL | Standard | 90% | 100%; 110% over $150k ($75k MFS) | Under $500 owed | Yes | Casualty; retired/disabled |
| AK | No income tax | | | | | |
| AZ | Standard | 90% | 100%; needs a 2025 return | AZ gross income $75k or less ($150k joint) in either year; under $1,000 owed | Yes | Casualty; retired/disabled; file by Feb 1 |
| AR | Standard | 90% | 100% | $1,000 or less owed; no tax last year | Yes | Casualty; retired/disabled; file by Jan 31 |
| CA | Standard, 30/40/0/30% | 90% | 100%; 110% over $150k ($75k MFS); none at CA AGI of $1M or more ($500k MFS) | Under $500 ($250 MFS) owed; no tax last year | Yes | Casualty; retired/disabled; file by Jan 31 |
| CO | Standard | 70% | 100%; 110% over $150k ($75k MFS) | $1,000 or less owed | If used federally | Retired/disabled; good cause; file by Jan 31 |
| CT | Standard | 90% | 100% | Under $1,000 owed; no tax last year | Yes | File by Jan 31 |
| DE | Apr 30, Jun 15, Sep 15, Jan 15 | 90% | 100%; 110% over $150k ($75k separate) | $800 or less owed; no tax last year | Yes | Casualty; retired/disabled |
| DC | Standard | 90% | 110%; needs a full-year DC return | Under $100 owed or under 10% of tax; no tax last year | Yes | Casualty; retired/disabled; file by Jan 15 |
| FL | No income tax | | | | | |
| GA | Standard | 70% | 100% | Gross income at most exemptions, deduction and $1,000 | Yes | Payment by Jan 31 counts as Jan 15 |
| HI | Apr 20, Jun 20, Sep 20, Jan 20 | 60% | 100% | Under $500 owed; no tax last year | Yes | Casualty; retired/disabled; file by Jan 31 |
| ID | No estimates for individuals | | | | | |
| IL | Standard | 90% | 100%; needs a full-year return showing tax | $1,000 or less owed | Yes | Farmers; 65+ in a nursing home; no tax or return last year; casualty |
| IN | Standard | 90% | 100%; 110% over $150k ($75k MFS) | Under $1,000 owed, state and county | Yes | No tax last year; file by Feb 1 |
| IA | Apr 30, Jun 30, Sep 30, Jan 31 | 90% | 100%; 110% over $150k ($75k MFS) | Under $1,000 owed | No | Casualty; retired/disabled; no tax last year |
| KS | Standard | 90% | 100%; needs a 12-month return | Under $500 owed | Yes | No tax or return last year; file by Jan 31 |
| KY | Standard | 90% | 100%; 110% over $150k ($75k MFS) | $500 or less owed | Yes | Casualty; no tax last year; file by Feb 1 if first required after Sep 1 |
| LA | Standard | 70% | 100%; or last year's income at 2026 rates | $1,000 or less owed ($2,000 joint) | Income to date at 70% | Good faith; file by Jan 31 |
| ME | Standard | 90% | 100%; lost on a $500k income jump | Under $1,000 owed; last year's tax under $1,000 | Yes | For cause; file by Feb 1 |
| MD | Standard | 90% | 110% at any income; none for first-time filers | $500 or less owed, state and county | By share of income | Reasonable cause; file by Jan 31 |
| MA | Standard | 80% | 100%; needs a 12-month return | $400 or less owed | Yes | Casualty; retired/disabled; no tax last year |
| MI | Standard | 90% | 100%; 110% over $150k ($75k MFS) | $500 or less owed | Yes | Reasonable cause; declared disaster; file by Jan 31 |
| MN | Standard | 90% | 100%; 110% over $150k | Under $500 owed | Yes | Casualty; retired/disabled; no tax last year; filing by Jan 31 counts as Jan 15 |
| MS | Standard; all withholding counts in April | 80% | 100% | Tax of $200 or less | No | None published |
| MO | Standard | 90% | 100%; or last year's income at this year's rates | Under $500 owed | Yes | Combat zone; filing by Jan 31 counts as Jan 15 |
| MT | Standard | 90% | 100% | Under $500 owed | Yes | Retired/disabled; farm or ranch; casualty; file by Jan 31 |
| NE | Standard | 90% | 100%; 110% over $150k ($75k MFS) | Under $500 owed; first-time filer | Yes | Casualty; retired/disabled; file by Jan 31 |
| NV | No income tax | | | | | |
| NH | No income tax | | | | | |
| NJ | Standard | 80% | 100% | $400 or less owed; gross income $20k or less ($10k single or MFS) | Yes | File by Feb 15 |
| NM | Standard | 90% | 100% | Under $1,000 owed; first-year resident; no tax last year | No | File by Jan 31 |
| NY | Standard | 90% | 100%; 110% over $150k ($75k MFS) | Under $300 owed per tax | Yes | Casualty; retired/disabled; file by Jan 31 |
| NC | Standard | 90% | 100% | Under $1,000 owed; no tax last year | Yes | File by Jan 31 |
| ND | Standard | 90% | 100%; none if moved in last year | Under $1,000 owed; federal estimates not required; last year's tax under $1,000 | If used federally | File by Feb 1 |
| OH | Standard | 90% | 100%; needs last year's return filed on time | $500 or less owed | No | None in the rules |
| OK | Standard | 70% | 100% | Under $500 owed; not required to file last year | Yes | Farmers |
| OR | Standard | 90% | 100% | Under $1,000 owed; no tax last year | Yes | Farmers and fishers; casualty; retired/disabled |
| PA | Standard | 90% | Last year's income at 3.07%; full-year residents who filed | Under $14,000 of income without withholding, or under $430 owed | Income to date | Tax Forgiveness last year; late starters; file by Jan 31 |
| RI | Standard | 80% | 100% | $250 or less owed; no federal tax last year | Yes | File by Jan 31 |
| SC | Standard | 90% | 100%; 110% over $150k ($75k MFS) | Under $100 owed; no tax last year | Yes | Casualty; retired/disabled; file by Feb 1 |
| SD | No income tax | | | | | |
| TN | No income tax | | | | | |
| TX | No income tax | | | | | |
| UT | No quarterly estimates; an extension prepayment test on Apr 15, 2027 | 90% | 100% | Filing and paying by Apr 15 | No | Reasonable cause |
| VT | Standard | 90% | 100%; needs a 12-month return | Under $500 owed | Yes | Farmers and fishers |
| VA | May 1, Jun 15, Sep 15, Jan 15 | 90% | 100%; or last year's income at 2026 rates | Under $1,000 owed; low income | Yes, not for January | File by Mar 1 |
| WA | Capital gains excise only: one payment, Apr 15, 2027 | | | | No | Five on-time years; casualty |
| WV | Standard | 90% | 100%; needs a 12-month return | Under $600 owed, or under 10% of tax; no tax last year | Yes | Casualty; reasonable cause; file by Feb 1 |
| WI | Standard | 90% | 100%; needs a 12-month return | Under $500 owed; no tax last year | Yes | Casualty; retired/disabled; disaster; active duty abroad; file by Feb 1 |
| WY | No income tax | | | | | |

The charge differs as much as the rules. Most jurisdictions charge interest
by the day. Some charge by the month, compound it, or charge a factor their
form prints. Indiana charges a flat 10% of each shortfall, and Illinois 2% or
10% by lateness. Michigan and Vermont add a penalty to the interest. Each
package's `estimated_y2026.go` lists its figures and their sources. The
research behind each jurisdiction, with citations, is in
[`research/estimated-payments`](../research/estimated-payments).
