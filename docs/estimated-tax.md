# Estimated tax

Tax is due as income is earned. Every jurisdiction that taxes income charges
an underpayment penalty when too little was paid during the year, through
withholding and estimated payments. The return preparer answers three
questions for a household, for each jurisdiction asked about:

- Does it have to make estimated payments, and what total keeps it clear of
  the penalty?
- How much is needed by each due date, and how much has been credited by
  then?
- If payments go as planned, what is the penalty?

The rules are taxlisp, beside the return rules they read, for tax year 2026:
the federal government, all fifty states and the District of Columbia. One
engine runs them in Go and in Rust. The API is protobuf, in
[`returns/v1/estimated.proto`](../returns/v1/estimated.proto), whose comments
are the reference for every field. [results.md](results.md#estimated-payments)
describes how `taxyear` uses the answers for each scenario, and
[`research/estimated-payments`](../research/estimated-payments) has the
research behind each jurisdiction, with citations.

## Contents

1. [Asking for estimated tax](#asking-for-estimated-tax)
2. [Shape: a common core, and each jurisdiction's own](#shape-a-common-core-and-each-jurisdictions-own)
3. [Input](#input)
4. [Output](#output)
5. [Rules by jurisdiction, 2026](#rules-by-jurisdiction-2026)
6. [How estimated tax works](#how-estimated-tax-works)
7. [Implementation in taxlisp](#implementation-in-taxlisp)
8. [Verification](#verification)
9. [Decisions](#decisions)
10. [Every state's own messages](#every-states-own-messages)

## Asking for estimated tax

Estimated tax is part of the return preparer
([`returns/v1`](returns.md)). There is no separate service: set
`ReturnInputs.estimated_tax`, an `EstimatedTaxInputs`, and the preparer
answers in `ReturnOutputs.estimated`, one `EstimatedAnswer` per jurisdiction,
in the order asked. With `estimated_tax` unset, `estimated` is empty.

In Go, with the engine the preparer service runs:

```go
eng, err := federal.DefaultEngine() // returns/rules/federal
out, err := eng.Prepare(&returnsv1.ReturnInputs{
	SchemaVersion: 1,
	TaxYear:       2026,
	Federal: &returnsv1.Federal1040Inputs{
		Elections: &returnsv1.Elections{FilingStatus: returnsv1.FilingStatusElection_FILING_STATUS_ELECTION_SINGLE},
		PriorYear: &returnsv1.PriorYear{PriorYearAgi: usd(130_000_00), PriorYearTotalTax: usd(9_800_00)},
	},
	States: []*returnsv1.StateInputs{{State: "CA"}},
	EstimatedTax: &returnsv1.EstimatedTaxInputs{
		AsOf:               &commonv1.Date{Year: 2026, Month: 9, Day: 1},
		Jurisdictions:      []string{"US", "CA"},
		FederalPriorReturn: returnsv1.EstimatedTaxInputs_PRIOR_RETURN_FULL_YEAR,
		PriorYears: []*returnsv1.EstimatedTaxInputs_PriorYear{{
			Jurisdiction: "CA", Return: returnsv1.EstimatedTaxInputs_PRIOR_RETURN_FULL_YEAR,
			Residency: returnsv1.EstimatedTaxInputs_RESIDENCY_FULL_YEAR, Tax: usd(4_100_00), Agi: usd(128_000_00),
		}},
		ThisYear: &returnsv1.EstimatedTaxInputs_CallerEstimate_{CallerEstimate: &returnsv1.EstimatedTaxInputs_CallerEstimate{
			Jurisdictions: []*returnsv1.EstimatedTaxInputs_CallerEstimate_Jurisdiction{
				{Jurisdiction: "US", Tax: usd(11_200_00), Withheld: usd(6_000_00)},
				{Jurisdiction: "CA", Tax: usd(4_600_00)},
			},
		}},
	},
})
for _, a := range out.GetEstimated() {
	fmt.Println(a.GetJurisdiction(), a.GetOutcome(), a.GetRequirement().GetTarget(), a.GetPenalty().GetTotal())
}
```

`usd` stands for a helper that makes a `commonv1.Money` in cents. Over gRPC,
the same `ReturnInputs` goes in a `PrepareRequest` to the `ReturnPreparer`
service ([`returns/preparer`](../returns/preparer)). In Rust,
`taxcalcs_returns::rules::federal::default_engine()?.prepare(&inputs)` gives
the same answer from the same rules.

`golden/testdata/estimated/inputs/` holds about 1,200 more households for
2026 as `ReturnInputs` in protojson, one file per jurisdiction, with the
answers the rules must give beside them.

## Shape: a common core, and each jurisdiction's own

The API has two parts, as the return protos do:
- **A common core every jurisdiction shares:** the due dates, what is
  needed, credited and short by each, the required annual payment and the
  options it was chosen from, what to pay next, and the penalty in total and
  per instalment. A caller showing what to pay when, for any jurisdiction,
  reads only these. Utah's April test and Washington's single payment are
  years of one instalment.
- **Each jurisdiction's own:** the facts its tests read, in
  `EstimatedStateInputs`, and its underpayment form line by line, as
  printed, in `EstimatedAnswer.worksheet`.

The common core holds only what every jurisdiction means the same way, so no
field means one thing in one state and another elsewhere. A test that reads
a figure only one state defines, such as California's $1,000,000 test on
California AGI, or Pennsylvania's test on income not subject to withholding,
reads a field of that state's own message, named as its form or statute
names it. A state that only changes the federal numbers has no message of
its own.

| | All common | All per jurisdiction | Common core and per jurisdiction |
|---|---|---|---|
| A caller showing what to pay when, for any state | one code path | one per jurisdiction, about 42 | one code path |
| A state adopts a new rule | a shared field or enum changes, for every caller | only that state's messages change | only that state's messages change |
| Inputs | generic fields, such as "AGI", that mean something different in each state | each state's own concepts | each state's own concepts, where it has them |
| Outputs | form lines flattened into generic charges | the state's form, line by line | the state's form, line by line, plus the core |

## Input

`EstimatedTaxInputs` holds what estimated tax needs beyond the year's
return. The federal prior-year AGI and total tax are the return's own
`ReturnInputs.federal.prior_year`, and the filing status is the return's.

| Field | Holds |
|---|---|
| `as_of` | The day the answer is worked out as of. A payment or withholding dated after it is planned. The penalty is figured to it, or to the day the jurisdiction's rules stop it, if earlier. Required. |
| `jurisdictions` | Postal codes to answer for: `"US"`, or a state's. Empty means federal, then every state in `ReturnInputs.states`. |
| `this_year` | How this year's tax is known: `from_return`, `caller_estimate` or `last_year_only` (below). Unset is refused. |
| `prior_years` | Each state's last-year return: whether it was filed and how long a year it covered (`PriorReturn`), residency, tax, AGI and filing status. A state with no entry is treated as not filed; an entry naming only the state means unknown. |
| `federal_prior_return` | Whether last year's federal return was filed, and whether it covered 12 months. |
| `prior_year_us_citizen_or_resident` | Every filer was a US citizen or resident all last year, for the federal exception for no tax last year and the states that copy it. Unset means unknown, and the answer asks. |
| `payments` | Every estimated payment for the year, each with its jurisdiction and date, last year's overpayment applied forward included. |
| `withholding_credit`, `withholding` | Whether withholding counts in equal shares on each due date (the default) or on the days withheld, and, for the latter, every withholding by day and jurisdiction. |
| `waiver` | A waiver claimed: its cause (casualty, disaster, retirement or disability, or none), the day it occurred, and the birth date a retirement waiver needs. Unset means not answered, and the answer asks. |
| `residence` | Dated residence periods covering the year, by which states prorate a part-year resident's payments. Empty means a full-year resident of every state in `ReturnInputs.states`. |
| `farming_and_fishing` | Gross income from farming and fishing against all gross income, this year and last, for the farmer and fisherman rules. |
| `return_filed` | The day this year's return was filed and paid in full: filing by the jurisdiction's day excuses the last instalment, and the penalty stops. |
| `requirement_first_met` | The first day the household could expect to owe estimated tax, which some states read. |
| `state_inputs` | Each state's own facts, below. |

**How this year's tax is known** is a oneof, because each choice needs
different data:

| | This year's figures | The rest of `ReturnInputs` | Annualised periods |
|---|---|---|---|
| `from_return` | worked out from the return, the states' own figures included | must be complete enough for the return to be computed | income per period |
| `caller_estimate` | the caller gives tax and withholding per jurisdiction, and each state's own figures in `state_inputs` | only the filing status, household and federal prior year are read | annualised tax (and income) per period |
| `last_year_only` | none | only the filing status, household and federal prior year are read | not possible |

`from_return` is not built yet: it is answered
`ESTIMATED_OUTCOME_UNSUPPORTED`. A `caller_estimate` jurisdiction with no
tax is answered without the this-year option; one with no entry is answered
as under `last_year_only`.

Choices behind the shape:
- **Shared fields sit outside the oneof.** Payments, withholding, last
  year's figures, each state's own facts and the as-of date are needed
  whichever way this year's tax is known.
- **Each mode carries only its own data.** Annualised periods live inside the
  modes that can use them, and a non-empty `periods` list is the election.
- **`LastYearOnly` is an empty message, not a flag,** so a field can be added
  later without breaking callers.
- **Common fields are defined by the form line they come from.** "This
  year's tax" and "last year's tax" are the lines each jurisdiction's
  underpayment form reads, and last year's AGI is the line of that
  jurisdiction's return. Any other figure is a state's own.
- **Enums where values are states.** Last year's return is not filed, not
  required, full year or short year, not booleans that could say "not filed
  but full year".
- **Made or planned follows from the date.** A payment is planned when it is
  dated after `as_of`, so there's no flag that could disagree with its date.

### Each state's own inputs

`EstimatedStateInputs` is a oneof with one message per state whose tests
read figures only it defines, such as `PennsylvaniaEstimatedInputs`. Each is
in a file of its own, `returns/v1/estimated_<st>.proto`, named and
documented as the state's form or statute names its facts, with any reading
of unsettled law the caller may choose (see [Decisions](#decisions)).
`tools/stategen` writes the oneof from those files. A state whose tests need
a fact it isn't given answers with a question asking for it, and uses the
default the question states. [Every state's own
messages](#every-states-own-messages) lists which states have one.

## Output

`EstimatedAnswer` is one jurisdiction's answer.

| Field | Holds |
|---|---|
| `jurisdiction` | The postal code answered for. |
| `outcome`, `reason` | `ESTIMATED_OUTCOME_COMPUTED`, or why not: `NO_RULES` (no rules for this jurisdiction and year, such as 2025; never the nearest year's rules), `UNSUPPORTED` (rules exist but don't cover these facts, such as a farmer or fisherman, or `from_return`), or `REFUSED` (the inputs are inconsistent or incomplete). Only `reason` is set when it isn't computed. |
| `requirement` | Whether payments are required (`REQUIRED`, `COVERED` by withholding, `EXEMPT` under an exception, or `UNKNOWN` when no option can be measured), why, the target, and every option. |
| `to_meet` | For each due date after `as_of`, the smallest payment that brings what is credited up to what is needed. |
| `installments` | Every instalment in date order: its due date (moved for weekends and holidays), what is needed, credited and short by it, and its status (met, short, nothing due or excused). Every amount is cumulative. |
| `penalty`, `penalty_without_planned` | The penalty figured to `as_of` with planned payments counted on their dates, and counting only payments made, so a caller can show what the plan saves. Each has its total, summary, per-instalment lines, `upper_bound` and `rated_through` for a rate not yet published, and the waiver's outcome and what would remain if granted. |
| `explanation` | How the jurisdiction's estimated tax works, in plain words, with sources. The same for every household. |
| `questions` | What the rules needed and didn't have, each with why it matters and what answering would change. The answer already uses the default each states. |
| `divergences` | Where the jurisdiction's form and its statute disagree on a rule the answer used, and which reading it follows. |
| `this_year_tax_known` | Whether this year's tax was given. |
| `worksheet` | The jurisdiction's underpayment form, line by line, from the same figures as the core (below). Unset when the requirement is `UNKNOWN`. |

**Every option is listed.** `requirement.options` holds each option the
jurisdiction's form lists, in its order, with its amount, its source, and
why any is unavailable. The target is the lowest available one,
`OPTION_STATUS_CHOSEN`, or the exception that applies. For example, with
this year's federal tax at $30,000 and last year's at $20,000 on AGI over
$150,000, the options are 90% of this year's tax, $27,000, and 110% of last
year's, $22,000, and the target is $22,000. An option's `id` is stable
across years, so a caller can match on it (see [Decisions](#decisions)).

**When an answer can't be given,** the answer says so instead of guessing:
- **No option available:** under `last_year_only` with the last-year route
  closed, `requirement.status` is `REQUIREMENT_STATUS_UNKNOWN`, with no
  instalments, penalty or worksheet. A question asks for this year's
  estimate and says which rule closed the last-year route.
- **A state fact not given:** the answer uses the default its question
  states, such as California AGI under $1,000,000, and asks for the fact.

**Waivers** are the tax authority's to grant, so none is applied. The
penalty is figured in full, `waiver_outcome` says whether the facts meet the
waiver's conditions, and `if_waived` what would remain.

### Each jurisdiction's own worksheet

The worksheet is the jurisdiction's underpayment form, line by line, as
taxlisp fills return forms. Each form is a taxprotos message:
`us/state/<st>/v1/form_<number>`, message `Form<Number>`. Federal Form 2210,
with Schedule AI, is `us`.

**Fifteen states' forms follow Form 2210:** the same lines and the same rule
for crediting payments, with only the figures changed. They share
`Form2210Pattern`, which is Form 2210's own message with the state's form
named. **The other 27 have forms of their own,** each a field of the
`worksheet` oneof, because each departs from Form 2210 in at least one way:
different shares, a different charge (by the month, a flat share, tiers, an
added penalty), more than one tax, a different order of crediting payments,
or a test Form 2210 has no line for. A state's second form, such as
Arkansas's AR2210A for the annualised method, is a second field.
[Every state's own messages](#every-states-own-messages) lists which each
state uses, and why. The eight states with no income tax, and Idaho, have no
form and no worksheet.

The rule is strict on purpose: a state whose form differs only slightly
still gets its own message, so `Form2210Pattern` never needs lines that mean
something in one state and nothing elsewhere.

## Rules by jurisdiction, 2026

"Standard" dates are Apr 15, Jun 15 and Sep 15, 2026 and Jan 15, 2027, in
equal shares. Most dates move for weekends, and in some states for state
holidays. "This year" and "Last year" are the safe-harbor shares: paying the
smaller of the two avoids the penalty. "Casualty" (a casualty, disaster or
unusual circumstance) and "Retired/disabled" (retirement after 62, or
disability) are the two common waivers. "File by" means filing and paying in full by that date
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
jurisdiction's rules file cites every figure's source. The research behind
each jurisdiction, with citations, is in
[`research/estimated-payments`](../research/estimated-payments).

## How estimated tax works

Income tax is due as income is earned, not when the return is filed. Wages
have tax withheld by the employer. Other income — self-employment,
investments, a large one-off gain — usually does not, so the taxpayer pays
estimated tax during the year. A taxpayer who pays too little by each due
date owes an underpayment penalty, which is in effect interest on the
shortfall.

### Federal

The federal rules are 26 U.S.C. § 6654, worked through on Form 2210.

**Who must pay.** Anyone who expects to owe $1,000 or more after withholding
and refundable credits, unless withholding already covers the required
annual payment.

**The required annual payment** is the smaller of two safe harbours:
- 90% of this year's tax; or
- 100% of last year's tax, or 110% if last year's AGI was over $150,000
  ($75,000 married filing separately). Last year must have been a 12-month
  tax year with a return filed.

Last year's figure is known when the year starts, so a taxpayer can always
avoid the penalty without predicting this year's income. This year's figure
is lower when income falls.

**No penalty at all** if the tax owed is under $1,000, or if the taxpayer was
a US citizen or resident with no tax liability for a full prior year.

**Instalments.** The required annual payment is due in four equal
instalments: April 15, June 15 and September 15 of the year, and January 15
of the next. A due date on a weekend or a legal holiday moves to the next
business day. Filing the return and paying in full by January 31 excuses the
January instalment.

**Crediting.** Each payment counts on the date it is made. Withholding counts
as paid in equal parts on each due date, however it was actually withheld,
unless the taxpayer elects to use the dates it was withheld. That makes a
large December withholding a remedy for an earlier shortfall. A payment is
applied to the earliest instalment still short.

**The annualised income method** (Schedule AI) suits uneven income. Each
instalment can be measured against the tax on income actually earned by then,
annualised. The periods end March 31, May 31, August 31 and December 31, and
the cumulative shares are 22.5%, 45%, 67.5% and 90%. Each instalment is the
smaller of the regular amount and the annualised one.

**The penalty** is interest on each instalment's shortfall. It runs from the
due date until the shortfall is paid, or until April 15 of the next year if
earlier, at the underpayment rate under § 6621. That rate is the federal
short-term rate plus three points, reset each quarter, and charged by the
day.

**Waivers.** The IRS may waive the penalty for a casualty, a disaster or
other unusual circumstance, or for a taxpayer who retired after 62 or became
disabled in this year or last, with reasonable cause. The IRS decides a
waiver, so the rules work out the penalty in full and say separately whether
the facts meet a waiver's conditions.

**Farmers and fishermen** (§ 6654(i)) have a simpler regime. A taxpayer
qualifies when at least two-thirds of gross income, this year or last, came
from farming or fishing. For them:
- **Required annual payment:** the smaller of 66⅔% of this year's tax or
  100% of last year's, with no 110% step.
- **Instalments:** one, due January 15 of the next year.
- **No penalty** if the return is filed and the tax paid in full by March 1.
- **Form:** Form 2210-F, not Form 2210.

The rules don't cover farmers and fishermen yet; see
[Decisions](#decisions).

### States

Most states with an income tax adopt § 6654 by reference and change a few
numbers: the threshold below which no estimates are needed, the percentages,
and sometimes the due dates. The rest of the machinery is the same: the
smaller of two safe harbours, four instalments, crediting, the annualised
method, and interest on each shortfall. A state's own interest rate and its
way of charging it make the biggest difference to the penalty.

Eight states have no individual income tax, so no estimated tax: Alaska,
Florida, Nevada, New Hampshire, South Dakota, Tennessee, Texas and Wyoming.
Washington taxes long-term capital gains only, as an excise, and Idaho does
not ask individuals for estimates.

[The table above](#rules-by-jurisdiction-2026)
gives every jurisdiction's 2026 rules. The ones that break the federal
pattern are:

**Different instalments**
- **California** — 30%, 40%, 0% and 30% of the required annual payment,
  not 25% each, so nothing is due in September.
- **Delaware** (April 30), **Hawaii** (the 20th of each due month),
  **Iowa** (the last day of April, June, September and January) and
  **Virginia** (May 1) — their own due dates.
- **Utah** — no quarterly estimates at all, only a test on April 15 of the
  next year: 90% of this year's tax or 100% of last year's paid by then.
- **Washington** — one payment for the capital gains excise, due with the
  return.
- **Mississippi** — all withholding counts as paid in April.

**Different safe harbours**
- **Colorado, Georgia, Louisiana and Oklahoma** — 70% of this year's tax.
- **Hawaii** — 60%.
- **Massachusetts, Mississippi, New Jersey and Rhode Island** — 80%.
- **Maryland** — 110% of last year's tax at any income, and no last-year
  option for a first-time filer.
- **District of Columbia** — 110% of last year's tax, and only with a
  full-year DC return.
- **California** — no last-year option at all once this year's California AGI
  reaches $1,000,000 ($500,000 married filing separately). This is the one
  income test measured on this year's income, so it needs this year's AGI
  even from a taxpayer who uses last year's tax.
- **Maine** — the last-year option is lost when income rises by more than
  $500,000.
- **Louisiana, Missouri and Virginia** — a third option: last year's income,
  taxed at this year's rates.
- **Pennsylvania** — a flat-rate state, where the last-year option is last
  year's income at this year's 3.07% rate, for full-year residents who filed.
  It also has an income test: estimates are needed only above $14,000 of
  income without withholding in 2026, a figure set by statute to rise each
  year.
- **North Dakota** — no last-year option for someone who moved in during the
  last year. **Ohio** requires last year's return to have been filed on time.
  **Kansas, Massachusetts, Vermont, West Virginia and Wisconsin** require it
  to have covered 12 months.

**Different thresholds and exceptions**
- The threshold for owing no estimates runs from $100 (DC, South Carolina) to
  $1,000 ($2,000 joint in Louisiana).
- **Arizona** — no estimates if Arizona gross income was $75,000 or less
  ($150,000 joint) in either this year or last.
- **New Jersey** — none at $20,000 of gross income or less ($10,000 single or
  married filing separately).
- **Georgia** — none when gross income is at most exemptions, the deduction
  and $1,000.
- **Indiana and Maryland** — the threshold covers state and county tax
  together.
- **New York** — counts each tax separately: state, New York City and
  Yonkers.
- **North Dakota and Maine** — exempt anyone whose last-year tax was under
  $1,000.
- The day by which filing and paying in full excuses the last instalment runs
  from January 15 (DC) to March 1 (Virginia).

**Different penalties**
- Most states charge interest by the day, like the federal penalty. Others
  charge by the month, compound it, or apply a factor their form prints.
- **Indiana** charges a flat 10% of each shortfall.
- **Illinois** charges 2% or 10%, depending on how late the payment is.
- **Michigan and Vermont** add a penalty on top of the interest.
- Some states publish the rate for a quarter only after it starts. The rules
  then work out the penalty as far as rates are published, and say so.

**Residence.** A part-year resident's required payment and instalments depend
on the months of residence. Many states also refuse the last-year option to
a taxpayer who did not live there all of last year.

## Implementation in taxlisp

### Layout

The rules sit beside the return rules they read:

```
returns/rules/federal/
  10_estimated_lib.tlisp    the shared machinery, for every jurisdiction
  10_estimated.tlisp        the dispatch: one answer per jurisdiction asked for
  10_estimated_us.tlisp     federal: figures, requirement, schedule, penalty, Form 2210
returns/rules/states/
  00_states.tlisp           state-estimated-answer: dispatches by state (generated by tools/stategen)
  <st>/03_estimated.tlisp   one state's figures, rules and worksheet
  notax/02_estimated.tlisp  the eight states with no income tax
returns/v1/
  estimated.proto           the common core
  estimated_<st>.proto      one state's own facts
```

Rule files load in name order, so the federal rules load after the library
they are built on. A state's rules need no registry entry: `tools/stategen`
finds `returns/rules/states/<st>/NN_estimated.tlisp`, which defines
`<name>-estimated-answer` (`alabama-estimated-answer`), and
`returns/v1/estimated_<st>.proto`, and writes the dispatch in
`00_states.tlisp`, the `EstimatedStateInputs` oneof (each field takes the
state's number in `StateInputs`), the `proto_library` and
`estimated.proto`'s imports. The eight states with no income tax have no
registry entry, so `00_states.tlisp` dispatches them by hand through
`no-income-tax-state?`, after the generated dispatch. They and Idaho still
answer when asked: `COMPUTED`, `REQUIREMENT_STATUS_EXEMPT` with "No income
tax" (or "Idaho doesn't require estimated payments") as the chosen option,
no instalments, a zero penalty, no worksheet, and an explanation of why.

`prepare-return`, after the federal and state returns, answers each
jurisdiction asked for: it builds that jurisdiction's facts (the common
ones, and the state's own `EstimatedStateInputs` message, if it has one),
calls its rules, and adds the answer to `ReturnOutputs.estimated`. The rules
fill the common core and the worksheet from the same computation, so the two
can't disagree.

### Shared machinery

`10_estimated_lib.tlisp` holds what every jurisdiction does the same way,
parameterised by the jurisdiction's figures:

- **Required annual payment.** It builds the options from the
  jurisdiction's bases: a percentage of this year's tax, a percentage of
  last year's tax with an optional income step, last year's income at this
  year's rates, or an exception. Each is marked available or not, with the
  reason, and the lowest available one is chosen.
- **Instalments.** It builds due dates and cumulative shares from the
  jurisdiction's schedule (25% each, 30/40/0/30, or one payment), and moves
  each date for weekends and that jurisdiction's holidays.
- **Annualised instalments.** Each cumulative share is applied to the
  annualised tax for its period, and each instalment is the smaller of the
  regular and annualised amounts, with the federal recapture of earlier
  savings.
- **Crediting.** Payments are applied to the earliest short instalment.
  Withholding is spread evenly over the due dates, or counted on the dates
  withheld when elected. Each instalment ends up met, short, excused or
  nothing due.
- **The penalty ledger.** It tracks each instalment's shortfall from its due
  date until paid, or until the jurisdiction's stopping date, splitting at
  every change of rate.
- **Interest by the day.** It splits a shortfall's days at each rate change
  and charges each period at its rate, as Form 2210 and the forms that
  follow it do, and fills `Form2210Pattern` with `est-form-2210`. A state
  that charges another way, by the month, a flat share, Illinois's tiers or
  Michigan's added penalty, figures it in its own worksheet; a form that
  prints a daily factor in place of a rate, as Oregon's OR-10 does, charges
  it with `est-daily-factor`.
- **Waivers.** It checks a waiver's conditions and works out what would
  remain if granted, leaving the penalty itself in full.

The engine builtins it rests on, in both engines: date arithmetic (days
between dates, adding days and months), business days (moving a date past
weekends and a given list of holidays, which are each jurisdiction's
figures), and dated rate series.

### A jurisdiction's rules

A jurisdiction's file is mostly figures, written with `deffigures`, each
anchored in its source, as the return figures are. California's, from
`returns/rules/states/ca/03_estimated.tlisp`:

```lisp
(deffigures ca-estimated-2026 CaliforniaEstimatedYear 2026
  (from ca-ftb-form-540es-instructions-2026
    (threshold #usd"500.00" :at who-must-pay)
    (threshold_separate #usd"250.00" :at who-must-pay)
    (this_year_rate 9000 :at who-must-pay)
    (last_year_rate 10000 :at who-must-pay)
    (high_income_rate 11000 :at prior-year-limit)
    (prior_year_step #usd"150000.00" :at prior-year-limit)
    (prior_year_step_separate #usd"75000.00" :at prior-year-limit)
    (high_income #usd"1000000.00" :at prior-year-limit)
    (high_income_separate #usd"500000.00" :at prior-year-limit)
    (shares [3000 4000 0 3000] :at installment-payments)
    (due_dates [#date"2026-04-15" #date"2026-06-15" #date"2026-09-15" #date"2027-01-15"] :at when-to-pay)
    (file_and_pay_by #date"2027-01-31" :at early-return))
  (from ca-ftb-estimate-penalty-rates-2026
    (rates [...] :at rates)))
```

Beyond its figures and its interest-rate series, a jurisdiction writes only
what is its own:
- **A state whose form follows Form 2210** and only changes numbers fills
  `Form2210Pattern` through the shared machinery.
- **A state whose form departs** writes its form with `defform`, line by
  line and cited, as the return forms are: California's FTB 5805, New York's
  IT-2105.9, Illinois's IL-2210. The form fills the state's own worksheet
  message.
- **A state with a test of its own,** such as Pennsylvania's income test or
  Utah's April test, writes that rule against the shared functions, reading
  the facts it needs from its own `EstimatedStateInputs` message.

The explanation, the questions and the divergences are text in the
jurisdiction's rules, and the documentation site renders the same text, with
each figure's source.

## Verification

- **Golden vectors.** Before the Go estimated tax calculators were removed,
  answered a fixed set of households for every jurisdiction, about 1,200 in
  all for each year, and their answers were written to `golden/testdata/estimated/<code>.json.gz`, with
  the households as `ReturnInputs` in `golden/testdata/estimated/inputs/`.
  Those files are now frozen. `golden/estimated_answer_test.go` prepares
  every household with the taxlisp rules and compares the answer with the
  vector, every text included; its doc comment lists the little the
  comparison leaves out, which `EstimatedAnswer` has no place for.
  [`golden/testdata/estimated/README.md`](../golden/testdata/estimated/README.md)
  says how each file was made.
- **The shared machinery on its own.** The reference jurisdiction "ZZ", in
  `golden/testdata/estimated/reference.tlisp`, is a small set of rules that
  is no jurisdiction's law. Only the golden test loads it, so the shared
  helpers are held to `ZZ.json.gz` without a test jurisdiction in the
  production rules.
- **Worksheets.** The vectors check only the common core. A test
  (`golden/estimated_worksheet_test.go`) checks that every answer's
  worksheet agrees with its core: the required annual payment, each
  instalment and its underpayment, and the penalty, reading each state's
  own form in `golden/estimated_worksheet_<st>_test.go`. A California
  household that annualises with Worksheet I, which no vector household
  does, is checked against the columns worked by hand from FTB 5805.
- **Rust.** The Rust engine runs the same rules, and
  `rust/returns/tests/estimated_golden.rs` holds it to every taxlisp answer
  the Go engine gives (`golden/testdata/estimated/taxlisp.json`).

## Decisions

Questions the design had to settle, and what it settled them as.

**Interest rates published late.** Rates are cited figures in each
jurisdiction's rules, like every other figure, so there's no input for
them. Until a rate is published, the penalty uses the last published rate
for the days after it, sets `upper_bound`, and gives the last day a
published rate covers as `rated_through`. A newly published rate ships as an
update to the rules.

**Part-year residents.** `EstimatedTaxInputs.residence` holds dated
residence periods, because states prorate the required payment and the
instalments by days or months of residence. The return's state inputs say
whether a household was a part-year resident, but not when.

**Local taxes.** The common core holds one requirement per state, as that
state's own form combines its taxes. Indiana's and Maryland's forms include
county tax with state tax. New York counts state, New York City, Yonkers and
MCTMT tax separately: the split is in its own inputs and worksheet, and the
core holds their total.

**Farmers and fishermen.** `EstimatedTaxInputs.farming_and_fishing` gives
the figures for the two-thirds test, this year and last, and the worksheet
oneof has `us_farmer` for Form 2210-F. The rules for them (federally,
66⅔% of this year's tax, one instalment on January 15, and no penalty when
filed by March 1) are not written yet, so a household that qualifies is
answered `ESTIMATED_OUTCOME_UNSUPPORTED`.

**`Form2210Pattern` is Form 2210.** A state whose form follows Form 2210
line for line uses the federal `Form2210` message with its own figures and
its form's name, rather than a separate set of lines.

**Option ids.** `EstimatedTargetOption.id` comes from a fixed scheme:
- `this-year-<percent>`, such as `this-year-90`;
- `last-year-<percent>`, such as `last-year-100` or `last-year-110`;
- `last-year-income`, for last year's income at this year's rates;
- `exception-<name>`, such as `exception-under-threshold` or
  `exception-no-tax-last-year`.

A new kind of option gets a new id under the same scheme, so callers can
match on them.

**A state reads the federal answer, not inputs about it.** Several states'
tests turn on federal estimated tax: whether federal estimates were required
(North Dakota), the federal required annual payment and instalments
(Vermont), whether the annualised method was used federally (Colorado). The
federal answer is worked out in the same request, so a state's rules read
it. Answering such a state works out federal too, even when the caller
didn't ask for it; the federal answer is then left out of
`ReturnOutputs.estimated`.

**Readings of unsettled law.** Where a state's form and its statute
disagree, or the law is unclear, the rules follow the form and report the
statute's reading as a divergence. Where a caller may choose the reading,
the choice is an enum field of that state's own message, named for the
reading, whose `UNSPECIFIED` value is the reading the rules take by default
(the form's, except where the state's message says otherwise). Two examples
are Rhode Island's January return and Mississippi's part months. The answer
asks when the other reading would change the figure.

**Planned payments.** Whether a payment is made or planned follows from its
date: after `as_of` it is planned. So there's no flag to disagree with it.

## Every state's own messages

Every jurisdiction with estimated tax, 42 in all: which underpayment form
it uses, whether its worksheet is `Form2210Pattern` or its own, how many
facts of its own it reads, and how its law treats farmers and fishermen,
whose rules are not written yet. Each state's own facts are documented field
by field in `returns/v1/estimated_<st>.proto`; facts the common core holds,
such as last year's return, this year's tax and farming income, are not
repeated there.

### Summary

| State | Underpayment form | Worksheet | Own inputs | Farmers and fishermen |
|---|---|---|---|---|
| AL | Form 2210AL | `Form2210Pattern` | 1 | follows federal, on Form 2210AL Part IV |
| AR | Form AR2210 (and AR2210A) | own: `FormAr2210` | none | farming only; one payment February 15 or file by March 15; no 66⅔% |
| AZ | Form 221 | own: `Form221` | 5; reads the federal answer | anyone a farmer federally; one payment January 15 or file by March 1; 90%/100% |
| CA | Form FTB 5805 | own: `FormFtb5805` | 4 | own form FTB 5805F; this-year only at $1,000,000 California AGI |
| CO | DR 0204 | `Form2210Pattern` | none; reads the federal answer | 50%, not 66⅔% |
| CT | Form CT-2210 | own: `FormCt2210` | 2 | follows federal |
| DC | D-2210 | own: `FormD2210` | none | no special rule |
| DE | Form PIT-UND | own: `FormPitUnd` | 1 | follows federal |
| GA | Form 500 UET | own: `Form500Uet` | 4 | 66⅔%; penalty on the last quarter only, at 9% |
| HI | Form N-210 | own: `FormN210` | 1 | 60%/100%, not 66⅔%; one payment January 20 |
| IA | IA 2210 | `Form2210Pattern` | 1 | follows federal; own form IA 2210F |
| IL | IL-2210 | own: `FormIl2210` | 5 (1 reading) | farming only, wholly exempt |
| IN | Schedule IT-2210 (and IT-2210A) | own: `FormIt2210` | 5 (1 reading) | follows federal |
| KS | Schedule K-210 | `Form2210Pattern` | 5 (3 readings) | follows federal |
| KY | Form 2210-K | `Form2210Pattern` | 5 | follows federal |
| LA | Form R-210R (R-210NR) | own: `FormR210r` | 3 (1 reading) | follows federal; oyster farming counts |
| MA | Form M-2210 | own: `FormM2210` | 3 (2 readings) | four 25% instalments at 66⅔% if March 1 is missed |
| MD | Form 502UP | own: `Form502up` | 3 | 100%, no 66⅔% |
| ME | Form 2210ME | own: `Form2210me` | 3 | follows federal |
| MI | MI-2210 | own: `FormMi2210` | 2 | follows federal; adds seafarers |
| MN | Schedule M15 | `Form2210Pattern` | 2 (1 reading) | follows federal |
| MO | Form MO-2210 | own: `FormMo2210` | 7 (2 readings) | farming only, 66⅔% |
| MS | Form 80-320 | own: `Form80320` | 2 (1 reading) | 80% by January 15, not 66⅔% |
| MT | Form EST-I | `Form2210Pattern` | 4 | farming or ranching only, wholly exempt |
| NC | Form D-422 (and D-422A) | `Form2210Pattern` | none | follows federal |
| ND | Schedule ND-1UT | own: `FormNd1ut` | 2; reads the federal answer | 66⅔%; the federal return by March 1 decides |
| NE | Form 2210N | `Form2210Pattern` | 2 (1 reading) | follows federal; ranching counts |
| NJ | Form NJ-2210 (NJ-2210NR) | own: `FormNj2210` | 8 (2 readings) | 66⅔% in full by January 15; no March 1 rule |
| NM | Form RPD-41272 | `Form2210Pattern` | 3 (1 reading) | farming or ranching only; penalty only if short by over one-third |
| NY | Form IT-2105.9 | own: `FormIt21059` | 6 | follows federal, on federal Form 2210-F |
| OH | Form IT/SD 2210 | own: `FormItSd2210` | 1 | exempt if filed by March 1, or paid by January 15 and filed on time; no share stated |
| OK | Form OW-8-P (and OW-8-P-SUP-I) | `Form2210Pattern` | none | farming only, wholly exempt |
| OR | Form OR-10 | `Form2210Pattern` | 3 | wholly exempt |
| PA | REV-1630 | own: `FormRev1630` | 6 | farming only; own form REV-1630A |
| RI | RI-2210 (and RI-2210A) | `Form2210Pattern` | 2 (2 readings) | follows federal; RI-2210 only, not RI-2210A |
| SC | SC2210 | `Form2210Pattern` | 2 | follows federal, on federal Form 2210-F |
| UT | none (TC-546 worksheet, Publication 58) | own: `FormTc546` | 1 | none |
| VA | Form 760C | own: `Form760c` | 4 | own form 760F; adds merchant seamen |
| VT | Worksheet IN-152 (and IN-152A) | own: `FormIn152` | none; reads the federal answer | wholly exempt |
| WA | none (capital gains tax) | own: `CapitalGainsPayment` | 2 | none |
| WI | Schedule U | own: `ScheduleU` | 2 | 90%/100%, all in the January column; no 66⅔% |
| WV | Form IT-210 | `Form2210Pattern` | none | farming only, 66⅔% |

Two states with no inputs of their own read the federal answer:
- **Colorado:** whether the annualised method was used federally. The DR 0204 Part 4 annualised method is open only then (DR 0204 instructions; DR 0104EP; C.R.S. § 39-22-605(6)(d)).
- **Vermont:** the federal required annual payment (Form 2210 line 9) and the federal annualised instalments (Schedule AI line 27). Worksheet IN-152A line 6 scales each federal instalment by the Vermont required annual payment over the federal one, and with no federal annualising Vermont allows none (IN-114 Instructions 2026).

Other states read the core or the federal answer for part of a test:
- **Arizona:** the federal facts in the same request, as Colorado does: this
  year's federal tax less federal withholding, from the caller's federal
  estimate, and no federal tax liability last year, from
  `federal_prior_return`, `ReturnInputs.federal.prior_year` and
  `prior_year_us_citizen_or_resident`. Either is a § 6654 exception, which
  § 43-581(E)(1) carries into Arizona (Form 221 instructions, line 1).
- **Georgia:** the gross income in its test is the core's
  `FarmingAndFishing.this_year_gross_income`; with `farming_and_fishing`
  unset it is unknown, and the answer asks.
- **Maryland:** Form 502UP lines 11 to 13 read the income received by each
  period's end, which scales the required payment by the share of the year's
  income received (code 301). It is the core's
  `CallerEstimate.PeriodTax.income`; no periods means the income-share
  method is not used.
- **North Dakota:** the federal answer, when the request gives federal
  figures: whether federal estimates were required, since North Dakota
  requires them of no one else (Form ND-1ES, 2026, condition 1); whether the
  federal January instalment was excused, which zeroes the fourth column
  (ND-1UT Item C); and whether the annualised method was used federally, the
  only case in which North Dakota allows it (ND-1UT Item B; N.D. Admin. Code
  81-03-04-02(3)(c)).
- **Nebraska:** Form 2210N's no-liability exception also needs U.S.
  citizenship or residence all last year, which the core's
  `prior_year_us_citizen_or_resident` holds.
- **New York:** one computation covers New York State, New York City,
  Yonkers and MCTMT tax (Tax Law § 685(c), (d)); the core's this year's and
  last year's tax are the four combined.

### Worksheets of their own

| State | taxprotos | Why not Form 2210 |
|---|---|---|
| AR | `us/state/ar/v1/form_ar2210`, `FormAr2210` (and `form_ar2210a`, `FormAr2210a`) | Payments count as paid in four equal parts on the due dates, whenever made (AR2210 line 8 and example), with no withholding election, a fixed 10% a year, and the annualised method on a separate form (AR2210A). |
| AZ | `us/state/az/v1/form_221`, `Form221` | The penalty is capped at 10% of the underpayment (A.R.S. § 42-1125(Q); Form 221 line 29) and compounds annually (§ 42-1123), and whether estimates are due is an income test in both years (§ 43-581(A)). |
| CA | `us/state/ca/v1/form_ftb_5805`, `FormFtb5805` | Instalments of 30%, 40%, 0% and 30%, with withholding deemed paid in the same split (FTB 5805 line 19; Worksheet II line 2), and annualised percentages of 27/63/63/90% (line 15). |
| CT | `us/state/ct/v1/form_ct_2210`, `FormCt2210` | Interest at 1% a month or fraction, from the 16th to the 15th, not by the day (§ 12-722(a); CT-2210 instructions), with the PE Tax Credit a line of its own. |
| DC | `us/state/dc/v1/form_d_2210`, `FormD2210` | Fixed factors per span on cumulative underpayments (D-2210 line 10), a 10%-of-liability exception, an income-proration annualised method (line 7), and always 110% of last year's. |
| DE | `us/state/de/v1/form_pit_und`, `FormPitUnd` | A short method of 12% less 0.0005 a day (Part 2, lines J and K), withholding in the period withheld, and a fixed 0.0005 a day to April 30 (lines 40 and 41). |
| GA | `us/state/ga/v1/form_500_uet`, `Form500Uet` | Each payment goes first to its own period's instalment (UET lines 9–13), with three per-instalment exceptions (lines 14–16) and a fixed 9% a year (line 20). |
| HI | `us/state/hi/v1/form_n_210`, `FormN210` | 2/3 of 1% per month or part, from the 21st to the 20th, not by the day (HRS § 235-97(f); N-210 lines 19–20). |
| IL | `us/state/il/v1/form_il_2210`, `FormIl2210` | A one-time 2% or 10% of each late amount by lateness, not interest by the day (§ 3-3(b-25)(1); Penalty Worksheet 1), with pass-through withholding in the quarter the entity's year ended (§ 804(g)(2)). |
| IN | `us/state/in/v1/form_it_2210`, `FormIt2210` (and `form_it_2210a`, `FormIt2210a`) | A flat 10% of each period's underpayment, whatever the days late (IC 6-3-4-4.1(b)), and a part-year resident divides by the periods of residence (line 14). |
| LA | `us/state/la/v1/form_r_210r`, `FormR210r` | Per-instalment exceptions, Exception 5 on actual income to date (R.S. 47:118(D); Act 307), and a statutory 12% daily factor to May 15 (line 18). |
| MA | `us/state/ma/v1/form_m_2210`, `FormM2210` | Part 3 figures the 4% surtax on its own lines (period income less the full-year threshold, times the annualisation amount, times 4%). |
| MD | `us/state/md/v1/form_502up`, `Form502up` | Cumulative columns charged again in every later column at a printed factor, by the month (502UP line 14; § 13-604(a)), and an income-share method in place of annualising (lines 11–13). |
| ME | `us/state/me/v1/form_2210me`, `Form2210me` | Monthly interest, a part month as whole, compounded monthly at the § 186 rate (Form 2210ME (2025) line 13; 36 M.R.S. § 5228(5)), and the unusual-event rule (§ 5228(1)(D)). |
| MI | `us/state/mi/v1/form_mi_2210`, `FormMi2210` | Interest by the day plus a flat 25% or 10% penalty, each on its own underpayment (MCL 205.23(2)–(4); MI-2210, TY2025). |
| MO | `us/state/mo/v1/form_mo_2210`, `FormMo2210` | Four exceptions that each excuse an instalment, a different one for each (Part III lines 20–24; RSMo 143.761.4(1) to (3)). |
| MS | `us/state/ms/v1/form_80_320`, `Form80320` | All withholding in column A (line 6), and ½% a month by whole months, not by the day (line 9; § 27-7-327(e)). |
| ND | `us/state/nd/v1/form_nd_1ut`, `FormNd1ut` | A payment goes first to its own quarter (ND-1UT Part 3 instructions), and the day fraction is rounded to three places (lines 16b–16e). |
| NJ | `us/state/nj/v1/form_nj_2210`, `FormNj2210` | By month or fraction at prime plus 3% (NJ-2210 Option 2; N.J.S.A. 54:49-3), with four per-quarter exceptions (§ 54A:9-6(d)(1)). |
| NY | `us/state/ny/v1/form_it_2105_9`, `FormIt21059` | State, City, Yonkers and MCTMT tax in one computation, a $300 threshold per tax, and an annualising worksheet per tax (IT-2105.9-I). |
| OH | `us/state/oh/v1/form_it_sd_2210`, `FormItSd2210` | Cumulative required amounts against cumulative payments, charged by one preset factor per span (IT/SD 2210 Part II; line 15). |
| PA | `us/state/pa/v1/form_rev_1630`, `FormRev1630` | Always 90% of this year's tax in four parts, with the last-year and income-to-date tests as per-instalment exceptions (REV-1630 Sections I and II; 72 P.S. § 7352(d)). |
| UT | `us/state/ut/v1/form_tc_546`, `FormTc546` | One test on April 15 for filers on extension, and a 2%-a-month penalty on the whole unpaid tax (§ 59-10-516(2)(b); § 59-1-401(5)(b)). |
| VA | `us/state/va/v1/form_760c`, `Form760c` | Four per-instalment exceptions (§ 58.1-492(C)), Exception 3's own periods and shares (760C line 18), and a small-amount test on the sum of underpayments (§ 58.1-492(D)(3)). |
| VT | `us/state/vt/v1/form_in_152`, `FormIn152` (and `form_in_152a`, `FormIn152a`) | Interest plus a 1% a month penalty capped at 25% (IN-152 lines 7–12; 32 V.S.A. § 3202(b)(2)), and annualising that scales the federal Schedule AI (IN-152A line 6). |
| WA | `us/state/wa/v1/capital_gains_payment`, `CapitalGainsPayment` | One payment of the whole tax on April 15 (RCW 82.87.110(3)), and a flat 9/19/29% late payment penalty, not interest (RCW 82.32.090(1)). |
| WI | `us/state/wi/v1/schedule_u`, `ScheduleU` | A payment goes first to its own period's instalment, then to the most recent shortfall, where Form 2210 pays the oldest first (Schedule U). |
