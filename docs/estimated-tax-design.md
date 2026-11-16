# Design: estimated tax in taxlisp

Status: proposed. This document is the plan for the move. Each step's PR
updates it where the step departs from it, and once the move is finished it
is folded into [estimated-tax.md](estimated-tax.md) and deleted.

Estimated tax is the one area of taxcalcs still written as hand-coded Go: the
[`estimated`](../estimated) package, the federal rules in
[`ustaxcalc`](../ustaxcalc), and one package per state. Every other tax rule
is taxlisp, which one engine runs in Go and in Rust. This document proposes
moving estimated tax into taxlisp as well, behind a protobuf API on the
existing return preparer, and then deleting the Go calculators. At the end,
the taxlisp rules are the single source of truth.

[estimated-tax.md](estimated-tax.md) describes the Go API as it is today, and
has the rules of every jurisdiction in one table.
[`research/estimated-payments`](../research/estimated-payments) has the
research behind each jurisdiction, with citations.

## Contents

1. [Goals](#goals)
2. [Background: how estimated tax works](#background-how-estimated-tax-works)
3. [Today's implementation](#todays-implementation)
4. [API](#api)
5. [Implementation in taxlisp](#implementation-in-taxlisp)
6. [Verification](#verification)
7. [Migration](#migration)
8. [Decisions](#decisions)
9. [Every state's own messages](#every-states-own-messages)

## Goals

- **One source of truth.** Estimated tax rules are written once, in taxlisp,
  beside the return rules they depend on, and run identically in Go and Rust.
- **The same answers.** Every jurisdiction's taxlisp rules reproduce what the
  Go calculators compute today, figure for figure and sentence for sentence,
  before the Go code is deleted.
- **One input model.** Estimated tax reads the same `ReturnInputs` a return is
  prepared from, so the figures it rests on cannot disagree with the return.
  A caller who has not built a return can still give the figures directly.
- **Every option shown.** The answer lists each safe harbour a household could
  use, with its amount and, where it is not available, why, instead of only
  the one chosen.
- **No field means different things in different places.** What every
  jurisdiction shares is common; a fact or a form line only one state has is
  in that state's own message, named as its form names it.
- **Documented like the returns.** Each jurisdiction's estimated tax rules
  appear on the documentation site, with citations, as its return rules do.

- **Farmers and fishermen too.** The Go calculators refuse them as
  unsupported. The taxlisp rules cover them, federally and in each state,
  because their rules are a simpler case of the same machinery.

Not a goal: fiduciaries, corporations, and taxes other than personal income
tax. The one exception is Washington's capital gains excise, which the Go
rules already cover.

## Background: how estimated tax works

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

[The table in estimated-tax.md](estimated-tax.md#rules-by-jurisdiction-2026)
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

## Today's implementation

[estimated-tax.md](estimated-tax.md) describes today's Go API. This design
depends on a few facts about it:
- **One Go package per jurisdiction**, about 44,000 lines of Go for the
  states, each implementing one `EstimatedTax` interface over the shared
  machinery in `estimated`.
- **This year's figures are inputs.** `estimated.Facts` takes this year's tax
  and AGI as plain numbers, because the calculators can't work out a return.
- **Farmers and fishermen aren't covered.** The calculators refuse them as
  unsupported.

Several things depend on them:
- `taxyear` calls them through `estimated/all` to show each scenario's
  payments, and copies the answer into `us/v1.EstimatedAnswer`.
- `ustaxcalcsvc` serves the federal calculators over gRPC.
- The Rust port of `ustaxcalc`, `estimated` and `working` serves callers that
  need them in Rust now.

A Go test in [`golden`](../golden) writes each jurisdiction's answers for a
fixed set of households to `golden/testdata/estimated/<code>.json.gz`.

## API

Estimated tax becomes part of the return preparer
([`returns/v1`](../returns/v1)). There's no new service: `ReturnInputs` gains
one message, and `ReturnOutputs` one repeated field.

### Shape: a common core, and each jurisdiction's own

The API has two parts, as the return protos do:
- **A common core every jurisdiction shares.** Return messages share an
  envelope (status, summary, diagnostics) around each jurisdiction's own form
  message.
- **Each jurisdiction's own messages.** State inputs are each state's own
  message, in a oneof that `tools/stategen` generates from the state
  registry.

For estimated tax:
- **Common:** the due dates, what is needed, credited and short by each, the
  required annual payment and the options it was chosen from, what to pay
  next, and the penalty in total and per instalment. These are what a caller
  shows for any jurisdiction, and every jurisdiction with estimated tax has
  them. Utah's April test and Washington's single payment are years of one
  instalment.
- **Each jurisdiction's own:** the facts its tests read, and its
  underpayment form line by line, as printed.

The common core is kept to what every jurisdiction means the same way, so no
field means one thing in one state and another elsewhere. A test that reads
a figure only one state defines, such as California's $1,000,000 test on
California AGI, or Pennsylvania's test on income not subject to withholding,
reads a field of that state's own message, named and documented as its form
or statute names it. A state that only changes the federal numbers needs no
message of its own.

#### Why not one or the other

| | All common | All per jurisdiction | Common core and per jurisdiction |
|---|---|---|---|
| A caller showing what to pay when, for any state | one code path | one per jurisdiction, about 42 | one code path |
| A state adopts a new rule | a shared field or enum changes, for every caller | only that state's messages change | only that state's messages change |
| Inputs | generic fields, such as "AGI", that mean something different in each state | each state's own concepts | each state's own concepts, where it has them |
| Outputs | form lines flattened into generic charges | the state's form, line by line | the state's form, line by line, plus the core |
| Schema size | small | about 84 messages | the core, plus a message for each state that departs from the federal pattern |

A fully common schema is brittle here. The [states that break the federal
pattern](#states) would each need a generic field that one state uses and
fifty ignore, or one whose meaning silently changes by state. Every new state
rule would then change a schema every caller depends on. A fully
per-jurisdiction schema fixes that, but pushes the same work onto every
caller that only wants to show four due dates.

### Input

```proto
message ReturnInputs {
  // ... existing fields: the household, documents, federal and state
  // inputs, and prior_year, which holds the federal prior-year AGI and
  // total tax that estimated tax also reads ...

  // Asks for estimated tax, and holds what it needs beyond the return.
  // Unset means estimated tax is not worked out, and
  // ReturnOutputs.estimated is empty.
  EstimatedTaxInputs estimated_tax = N;
}

// What estimated tax needs beyond the year's return: which jurisdictions to
// answer for, last year's figures, the payments, how this year's tax is
// known, and each state's own facts.
message EstimatedTaxInputs {
  // The day the answer is worked out as of. A payment or withholding dated
  // on or before it has been made; one dated after it is planned. The
  // penalty is figured to this day, or to the day the jurisdiction's rules
  // stop it, whichever is earlier. Required.
  taxprotos.common.v1.Date as_of = 1;

  // The jurisdictions to answer for, as postal codes: "US" for federal, or
  // a state's, such as "CA" or "DC". Empty means federal and then every
  // state in ReturnInputs.states. Answers come back in this order.
  repeated string jurisdictions = 2;

  // Last year's figures for each state answered for, one entry per state.
  // A state with no entry is treated as PRIOR_RETURN_NOT_FILED, which
  // closes its last-year option. The federal figures are
  // ReturnInputs.prior_year, not an entry here.
  repeated PriorYear prior_years = 3;

  // Every estimated payment for the tax year, for every jurisdiction,
  // including last year's overpayment applied forward. The existing
  // message, which the annual return also reads. A payment dated after
  // as_of is planned.
  repeated EstimatedPayment payments = 4;

  // How withholding is credited to the instalments.
  WithholdingCredit withholding_credit = 5;

  // Tax withheld, by day and jurisdiction. Read only under
  // WITHHOLDING_CREDIT_AS_WITHHELD, and then it must hold every
  // withholding of the year, planned ones included.
  repeated Withholding withholding = 6;

  // A waiver of the penalty claimed. The existing message, gaining the
  // cause and the dates a waiver's conditions turn on. Unset means none is
  // claimed.
  Waiver waiver = 7;

  // Each state's own facts, for the states whose tests read figures only
  // they define. One entry per such state answered for. A state whose
  // tests need a fact it isn't given answers with a question asking for
  // it, and uses the default the question states.
  repeated EstimatedStateInputs state_inputs = 8;

  // Where the household lived this year, as dated periods in date order,
  // covering the whole year with no gaps or overlaps. A state prorates a
  // part-year resident's required payment and instalments by these. Empty
  // means a full-year resident of every state in ReturnInputs.states. Under
  // from_return, they must agree with the return's state residency.
  repeated ResidencePeriod residence = 9;

  // Gross income from farming and fishing, for the farmer and fisherman
  // rules (26 U.S.C. § 6654(i), and each state's equivalent). Unset means
  // none, and the farmer and fisherman rules don't apply.
  FarmingAndFishing farming_and_fishing = 13;

  // The day this year's return was filed and the tax paid in full. Filing
  // and paying by the jurisdiction's day (January 31 federally) excuses
  // the last instalment, and the penalty stops running on this day. Unset
  // means not filed yet.
  taxprotos.common.v1.Date return_filed = 14;

  // Every filer was a US citizen or resident for the whole of last year. The
  // federal exception for no tax last year (26 U.S.C. § 6654(e)(2)) and the
  // states that copy it, such as Kentucky, Montana, Nebraska and West
  // Virginia, apply only then. Unset means unknown: an answer that needs it
  // asks, and leaves that exception unavailable meanwhile.
  optional bool prior_year_us_citizen_or_resident = 16;

  // Whether last year's federal return was filed, and the period it
  // covered. The federal last-year option needs a filed 12-month return
  // (26 U.S.C. § 6654(d)(1)(B)), and so do states that read last year's
  // federal tax, such as Rhode Island's exception for no federal tax last
  // year. The federal figures themselves are ReturnInputs.prior_year.
  // Unspecified is refused when federal is answered for.
  PriorReturn federal_prior_return = 17;

  // The first day the household could expect to owe estimated tax for the
  // year, such as the day a large gain was realised. Some states excuse
  // instalments due before it or move the year's first payment after it,
  // as Kentucky does for a household first required after September 1;
  // each state's rules say how they read it, and the federal rules don't.
  // Unset means January 1.
  taxprotos.common.v1.Date requirement_first_met = 15;

  // How this year's tax is known. Unset is refused, with an error naming
  // this field.
  oneof this_year {
    // Work out this year's figures from the rest of ReturnInputs.
    FromReturn from_return = 10;

    // Use the caller's own figures for this year. The return is not
    // computed.
    CallerEstimate caller_estimate = 11;

    // Use only last year's safe harbour. This year's tax is not estimated.
    LastYearOnly last_year_only = 12;
  }

  // How withholding is credited to the instalments.
  enum WithholdingCredit {
    // Not set: the same as EVENLY, the default the law gives.
    WITHHOLDING_CREDIT_UNSPECIFIED = 0;

    // The year's withholding counts as paid in equal shares on each due
    // date, whenever it was actually withheld (26 U.S.C. § 6654(g)(1)).
    // The withholding comes from the return, or from
    // CallerEstimate.Jurisdiction.withheld.
    WITHHOLDING_CREDIT_EVENLY = 1;

    // The election to count withholding on the days it was withheld
    // (§ 6654(g)(2)), from the withholding field. It applies to every
    // jurisdiction that allows it; one that doesn't credits evenly and says
    // so in its answer's questions.
    WITHHOLDING_CREDIT_AS_WITHHELD = 2;
  }

  // One state's figures from last year that every state's rules read: its
  // last-year option, and its step from 100% to 110% of last year's tax.
  message PriorYear {
    // The state's postal code, such as "CA". Never "US".
    string jurisdiction = 1;

    // Whether a return was filed for last year, and the period it covered.
    PriorReturn return = 2;

    // Where the household lived last year, as the state counts residence.
    Residency residency = 3;

    // The tax on last year's state return, after nonrefundable credits:
    // the line the state's underpayment form reads it from. Read unless
    // return is PRIOR_RETURN_NOT_FILED. Negative is refused.
    taxprotos.common.v1.Money tax = 4;

    // The adjusted gross income on last year's state return, as that return
    // reports it, for the step from 100% to 110% of last year's tax. Read
    // unless return is PRIOR_RETURN_NOT_FILED. A state whose step reads
    // last year's federal AGI instead reads ReturnInputs.prior_year.
    taxprotos.common.v1.Money agi = 5;

    // The filing status on last year's state return. Some states test last
    // year's status rather than this year's, such as Indiana's and
    // Michigan's lower income step for married filing separately. Read
    // unless return is PRIOR_RETURN_NOT_FILED or PRIOR_RETURN_NOT_REQUIRED.
    taxprotos.us.federal.v1.FilingStatus filing_status = 6;
  }

  // Whether last year's return was filed, and the period it covered.
  enum PriorReturn {
    // Not set. Refused.
    PRIOR_RETURN_UNSPECIFIED = 0;

    // No return was filed for last year though one was required. The
    // last-year option is closed.
    PRIOR_RETURN_NOT_FILED = 1;

    // A return was filed, covering a full 12-month year.
    PRIOR_RETURN_FULL_YEAR = 2;

    // A return was filed, covering less than 12 months, such as a first
    // year. Most jurisdictions, like the federal rules, then close the
    // last-year option.
    PRIOR_RETURN_SHORT_YEAR = 3;

    // No return was filed for last year because none was required, such as
    // income under the filing threshold. Several states, such as Illinois,
    // Kansas and Oklahoma, then excuse the year entirely, where
    // PRIOR_RETURN_NOT_FILED would not.
    PRIOR_RETURN_NOT_REQUIRED = 4;
  }

  // Where the household lived during a year, as a state counts residence.
  enum Residency {
    // Not set. Refused.
    RESIDENCY_UNSPECIFIED = 0;

    // A resident of the state for the whole year.
    RESIDENCY_FULL_YEAR = 1;

    // A resident for part of the year, such as a household that moved in.
    // Some states then close the last-year option.
    RESIDENCY_PART_YEAR = 2;

    // Not a resident at any time in the year.
    RESIDENCY_NONRESIDENT = 3;
  }

  // A span of days the household lived in one place.
  message ResidencePeriod {
    // The postal code of the state lived in, or "" for a place with no
    // state income tax, such as abroad.
    string state = 1;

    // The first day lived there.
    taxprotos.common.v1.Date from = 2;

    // The last day lived there.
    taxprotos.common.v1.Date to = 3;
  }

  // Gross income from farming and from fishing, this year and last, against
  // all gross income. Federally a household qualifies for the farmer and
  // fisherman rules when farming and fishing together make up at least
  // two-thirds of gross income in either year. A state may count only one
  // of them, as West Virginia counts only farming, so they are kept apart.
  // Each amount is gross income as 26 U.S.C. § 61 defines it, before
  // deductions, for both spouses on a joint return.
  message FarmingAndFishing {
    // This year's gross income from farming: Schedule F, farm rents on
    // Form 4835, and farm gains on Form 4797.
    taxprotos.common.v1.Money this_year_farming = 1;

    // This year's gross income from fishing.
    taxprotos.common.v1.Money this_year_fishing = 2;

    // This year's gross income from every source.
    taxprotos.common.v1.Money this_year_gross_income = 3;

    // Last year's gross income from farming, as for this_year_farming.
    taxprotos.common.v1.Money last_year_farming = 4;

    // Last year's gross income from fishing.
    taxprotos.common.v1.Money last_year_fishing = 5;

    // Last year's gross income from every source.
    taxprotos.common.v1.Money last_year_gross_income = 6;
  }

  // Tax withheld for one jurisdiction on one day.
  message Withholding {
    // The postal code of the jurisdiction the tax was withheld for: "US"
    // for federal income tax, or a state's.
    string jurisdiction = 1;

    // The day it was withheld, within the tax year. A day after as_of is
    // planned withholding.
    taxprotos.common.v1.Date date = 2;

    // The amount withheld that day. Negative is refused.
    taxprotos.common.v1.Money amount = 3;
  }

  // The end of one annualisation period, for FromReturn.
  message Period {
    // The period's last day. It must be an end date the jurisdiction's
    // annualised method sets: March 31, May 31, August 31 and December 31
    // federally.
    taxprotos.common.v1.Date end = 1;

    // Federal adjusted gross income from January 1 to end. The engine
    // derives each jurisdiction's figures for the period from it, as it
    // does for the whole year.
    taxprotos.common.v1.Money income = 2;
  }

  // Work out this year's figures from the rest of ReturnInputs: each
  // jurisdiction's tax, withholding and its own figures come from the
  // return the engine prepares. If the return can't be computed, every
  // answer is ESTIMATED_OUTCOME_REFUSED, with the return's diagnostics as
  // the reason.
  message FromReturn {
    // The annualised income method: one entry per period, in date order.
    // Empty means the regular method. A jurisdiction without the annualised
    // method ignores it, and says so in its answer's questions.
    repeated Period periods = 1;
  }

  // This year's figures as the caller estimates them, per jurisdiction. The
  // return is not computed: of the rest of ReturnInputs, only the filing
  // status, the household and prior_year are read. Figures a state's own
  // tests read go in state_inputs.
  message CallerEstimate {
    // One entry per jurisdiction answered for. A jurisdiction with no entry
    // is answered as under LastYearOnly.
    repeated Jurisdiction jurisdictions = 1;

    // One jurisdiction's figures for this year.
    message Jurisdiction {
      // The jurisdiction's postal code: "US" for federal, or a state's.
      string jurisdiction = 1;

      // This year's expected tax, after nonrefundable credits: the line the
      // jurisdiction's underpayment form reads as this year's tax. Unset
      // means not estimated, and the this-year option is then unavailable.
      optional taxprotos.common.v1.Money tax = 2;

      // This year's expected withholding for the jurisdiction, credited
      // under WITHHOLDING_CREDIT_EVENLY. Unset means none.
      optional taxprotos.common.v1.Money withheld = 3;

      // The annualised income method: one entry per period, in date order.
      // Empty means the regular method.
      repeated PeriodTax periods = 4;
    }

    // One annualisation period's figures, given by the caller.
    message PeriodTax {
      // The period's last day, as for Period.end.
      taxprotos.common.v1.Date end = 1;

      // The annualised tax on income from January 1 to end, before the
      // period's cumulative share is applied: the line of the
      // jurisdiction's annualised worksheet that the share multiplies.
      taxprotos.common.v1.Money annualised_tax = 2;
    }
  }

  // Use only last year's safe harbour. The this-year option is unavailable
  // in every answer, and a jurisdiction whose last-year option is closed
  // asks for this year's figures instead of answering. Empty today; a
  // message, not a flag, so assumptions can be added later.
  message LastYearOnly {}
}
```

The waiver is `EstimatedTaxInputs.Waiver`: its cause, a `WaiverCause` enum
of casualty or retired/disabled under § 6654(e)(3); the day it occurred; and
the birth date a retirement waiver needs. It is its own message rather than
an extension of the annual return's `EstimatedPenaltyWaiver`, which asks for
the amount to waive on Form 2210 after the year.

#### Each state's own inputs

`EstimatedStateInputs` is a oneof with one message per state whose tests
read figures only it defines. `tools/stategen` generates the oneof and its
field numbers from the state registry, as it does `StateInputs`. Each
message holds that state's facts, named as its form or statute names them,
and any reading of unsettled law the caller may choose (see
[Decisions](#decisions)).
Under `from_return`, the engine fills them from the return, and the caller
gives only what a return doesn't hold, such as whether last year's return
was filed on time.

```proto
// One state's own facts for its estimated tax rules: the figures its tests
// read that no other jurisdiction defines.
message EstimatedStateInputs {
  // The state's postal code, such as "CA". It must match the message set
  // below.
  string state = 1;

  // The state's own facts. Generated by tools/stategen from the state
  // registry; a state that only changes the federal numbers has no entry.
  oneof inputs {
    PennsylvaniaEstimatedInputs pa = 45;
    // ... one per state with facts or readings of its own, as "Every
    // state's own messages" lists them ...
  }
}

// Pennsylvania's own facts for estimated tax (72 P.S. § 7325; Form
// REV-1630).
message PennsylvaniaEstimatedInputs {
  // This year's expected PA-taxable income from which no employer withholds
  // Pennsylvania tax (REV-414 (I) line 1). Estimates are required only when
  // it exceeds the year's statutory amount, $14,000 for 2026
  // (72 P.S. § 7325(a)(1)). Under from_return, the engine reads it from the
  // return; otherwise unset means unknown, and the answer asks for it.
  optional taxprotos.common.v1.Money income_not_subject_to_withholding = 1;

  // This year's Tax Forgiveness, resident and other credits (PA-40 lines 21
  // to 23). REV-1630 Section I takes 90% of the tax before these credits
  // and subtracts them with withholding, so the rules add them back to the
  // common core's tax after credits. Under from_return, read from the
  // return.
  optional taxprotos.common.v1.Money credits = 2;

  // Last year's PA taxable income (PA-40 line 11). Exception 1 measures the
  // required payment as this income at this year's 3.07% rate, for a
  // full-year resident who filed (REV-1630 line 12). Unset closes that
  // option.
  optional taxprotos.common.v1.Money prior_year_taxable_income = 3;

  // Last year's Tax Forgiveness Credit (PA-40 line 21), which REV-1630
  // line 12 subtracts from Exception 1. Unset subtracts none, which can only
  // overstate that target, and the answer asks for it.
  optional taxprotos.common.v1.Money prior_year_tax_forgiveness_credit = 4;

  // The household qualified for 100% Tax Forgiveness on last year's PA-40
  // (Schedule SP). Such a filer is not subject to the penalty this year
  // (REV-1630, "Before You Begin"). Unset means unknown, and the answer
  // asks when it would change the figure.
  optional bool prior_year_full_tax_forgiveness = 5;
}
```

Pennsylvania's message shows the kind of facts a state's own message
holds. [Every state's own messages](#every-states-own-messages) lists
every state's, field by field.

**How this year's tax is known** is a oneof, because each choice needs
different data:

| | This year's figures | The rest of `ReturnInputs` | Annualised periods |
|---|---|---|---|
| `from_return` | the engine works them out from the return, the states' own figures included | must be complete enough for the return to be computed | income per period; the engine works out the tax |
| `caller_estimate` | the caller gives tax and withholding per jurisdiction, and each state's own figures in `state_inputs` | only the filing status, household and federal prior year are read | annualised tax per period |
| `last_year_only` | none | only the filing status, household and federal prior year are read | not possible |

Choices behind the shape:
- **Shared fields sit outside the oneof.** Payments, withholding, last
  year's figures, each state's own facts and the as-of date are needed
  whichever way this year's tax is known.
- **Each mode carries only its own data.** Annualised periods live inside the
  modes that can use them, and a non-empty `periods` list is the election.
- **`caller_estimate` keeps today's way of working.** A spreadsheet, a quick
  estimate, or a service that already knows the figure doesn't have to build
  a return.
- **`LastYearOnly` is an empty message, not a flag,** so a field can be added
  later without breaking callers.
- **Common fields are defined by the form line they come from.** "This
  year's tax" and "last year's tax" are the lines each jurisdiction's
  underpayment form reads, and last year's AGI is the line of that
  jurisdiction's return. Any other figure is a state's own.
- **Enums where values are states.** Last year's return is not filed, full
  year or short year, not two booleans that could say "not filed but full
  year".
- **Made or planned follows from the date.** A payment is planned when it is
  dated after `as_of`, so there's no flag that could disagree with its date.

### Output

```proto
message ReturnOutputs {
  // ... existing fields: the federal and state returns ...

  // One answer per jurisdiction answered for, in the order of
  // EstimatedTaxInputs.jurisdictions. Empty when
  // ReturnInputs.estimated_tax is unset.
  repeated EstimatedAnswer estimated = N;
}
```

`EstimatedAnswer` is today's message from `us/v1/estimated.proto`, which
`scenarios/v1` carries. It moves into `returns/v1`, and the move is the
moment to change it, with `scenarios/v1` updated in the same PR:
- it gains `jurisdiction` and the jurisdiction's own worksheet;
- the penalty's generic charge kinds go, since how a jurisdiction charges
  belongs in its worksheet;
- booleans that together describe one state become enums;
- every comment says exactly what the field holds.

```proto
// One jurisdiction's estimated tax answer for one tax year: whether
// payments are needed and how much, what is needed by each due date, what
// the penalty is if payments go as given, and the jurisdiction's own
// underpayment form, line by line.
message EstimatedAnswer {
  // The postal code of the jurisdiction answered for: "US" for federal, or
  // a state's, such as "CA".
  string jurisdiction = 1;

  // Whether the rules answered. Every field below reason is set only when
  // this is ESTIMATED_OUTCOME_COMPUTED.
  EstimatedOutcome outcome = 2;

  // Why the rules didn't answer, in plain words. Empty when outcome is
  // ESTIMATED_OUTCOME_COMPUTED.
  string reason = 3;

  // Whether payments are needed, every option considered, and the total the
  // chosen one requires.
  EstimatedRequirement requirement = 4;

  // What to pay from here on to stay clear of the penalty: for each due
  // date after as_of, the smallest payment on that date that brings what is
  // credited by it up to what is needed by it. Empty when nothing more is
  // needed, or when requirement.status is REQUIREMENT_STATUS_UNKNOWN.
  repeated DuePayment to_meet = 5;

  // Every instalment of the year, in date order, with what is needed,
  // credited and short by each due date, planned payments included. Empty
  // when requirement.status is REQUIREMENT_STATUS_UNKNOWN.
  repeated EstimatedInstallment installments = 6;

  // The penalty figured to as_of, with planned payments counted on their
  // dates as if made. Unset when requirement.status is
  // REQUIREMENT_STATUS_UNKNOWN.
  EstimatedPenalty penalty = 7;

  // The penalty figured to as_of counting only payments made by as_of, so
  // a caller can show what the plan saves. Unset when requirement.status is
  // REQUIREMENT_STATUS_UNKNOWN.
  EstimatedPenalty penalty_without_planned = 8;

  // How this jurisdiction's estimated tax works, in plain words, with its
  // sources. The same for every household.
  EstimatedExplanation explanation = 9;

  // What the rules needed from the household and didn't have, each with
  // why it matters and what answering would change. The answer above
  // already uses the default each question states.
  repeated EstimatedQuestion questions = 10;

  // Where the jurisdiction's form and its statute disagree on a rule the
  // answer used, with the reading the answer follows.
  repeated EstimatedDivergence divergences = 11;

  // This year's tax was known to the rules, worked out from the return or
  // given in a CallerEstimate. False under LastYearOnly, or when a
  // CallerEstimate left it unset; the this-year option is then
  // OPTION_STATUS_UNAVAILABLE.
  bool this_year_tax_known = 12;

  // The jurisdiction's own underpayment form, line by line as printed, with
  // the figures the core above came from. Generated by tools/stategen from
  // the state registry. Unset when requirement.status is
  // REQUIREMENT_STATUS_UNKNOWN.
  oneof worksheet {
    // Federal Form 2210, with Schedule AI for the annualised method.
    nexteggapp.taxprotos.us.federal.v1.Form2210 us = 20;

    // Federal Form 2210-F, for a farmer or fisherman.
    nexteggapp.taxprotos.us.federal.v1.Form2210F us_farmer = 21;

    // The form of one of the fifteen states whose form follows Form 2210
    // line for line.
    Form2210Pattern pattern = 22;

    // California's Form FTB 5805: 30%, 40%, 0% and 30% instalments.
    nexteggapp.taxprotos.us.state.ca.v1.FormFtb5805 ca = 23;

    // New York's Form IT-2105.9, with New York City and Yonkers tax.
    nexteggapp.taxprotos.us.state.ny.v1.FormIt21059 ny = 24;

    // ... one field per state with a form of its own, 27 in all, as
    // "Every state's own messages" lists them ...
  }
}

// Whether the rules gave an answer, and if not, why.
enum EstimatedOutcome {
  // Not set. Never returned.
  ESTIMATED_OUTCOME_UNSPECIFIED = 0;

  // The rules answered, and every field of the answer is set as its comment
  // says.
  ESTIMATED_OUTCOME_COMPUTED = 1;

  // No rules are built for this jurisdiction and tax year, such as a year
  // whose figures aren't published yet. Only reason is set.
  ESTIMATED_OUTCOME_NO_RULES = 2;

  // The rules exist but don't cover these facts, such as a fiduciary.
  // Only reason is set.
  ESTIMATED_OUTCOME_UNSUPPORTED = 3;

  // The facts as given are inconsistent or incomplete, such as a FromReturn
  // whose return can't be computed. Only reason is set.
  ESTIMATED_OUTCOME_REFUSED = 4;
}

// Whether a household has to make estimated payments to a jurisdiction,
// and the required annual payment: the total that withholding and estimated
// payments together must reach over the year to avoid the penalty.
//
// The required annual payment is the smallest of the options the household
// can use, each listed in options. For example, with this year's federal
// tax at $30,000 and last year's at $20,000 on AGI over $150,000, the
// options are 90% of this year's tax, $27,000, and 110% of last year's,
// $22,000. The target is $22,000.
message EstimatedRequirement {
  // Whether payments are required, and whether a target could be measured.
  RequirementStatus status = 1;

  // Why the status is what it is, in plain words and the household's
  // numbers.
  string why = 2;

  // The required annual payment: the amount of the chosen option.
  // Zero under REQUIREMENT_STATUS_EXEMPT and REQUIREMENT_STATUS_UNKNOWN.
  taxprotos.common.v1.Money target = 3;

  // Every option the jurisdiction has, in the order its form lists them,
  // including those this household can't use. Exactly one is
  // OPTION_STATUS_CHOSEN, except under REQUIREMENT_STATUS_UNKNOWN, when none
  // is.
  repeated EstimatedTargetOption options = 4;
}

// Whether estimated payments are required.
enum RequirementStatus {
  // Not set. Never returned.
  REQUIREMENT_STATUS_UNSPECIFIED = 0;

  // Payments are required: no exception applies, and the withholding
  // expected for the year falls short of the target.
  REQUIREMENT_STATUS_REQUIRED = 1;

  // No payments are required, because the withholding expected for the
  // year reaches the target. A shortfall on an early due date can still be
  // penalised under WITHHOLDING_CREDIT_AS_WITHHELD.
  REQUIREMENT_STATUS_COVERED = 2;

  // An exception means no penalty can arise for the year, whatever is paid,
  // such as tax owed after withholding under the jurisdiction's threshold
  // ($1,000 federally), or no tax last year. The exception is the chosen
  // option.
  REQUIREMENT_STATUS_EXEMPT = 3;

  // No option could be measured: this year's tax isn't known, and no
  // last-year option is available. An example is LastYearOnly for a
  // California household with California AGI of $1,000,000 or more. There
  // is no target, no instalments and no penalty, and the answer's questions
  // ask for this year's figures.
  REQUIREMENT_STATUS_UNKNOWN = 4;
}

// One option for the required annual payment: a safe harbour or an
// exception, as the jurisdiction's form lists it.
message EstimatedTargetOption {
  // The option's identity within the jurisdiction, stable across years,
  // such as "this-year-90", "last-year-110" or "pa-prior-income". A caller
  // that treats one option specially matches on this, not on basis.
  string id = 1;

  // The option's name in plain words, as the jurisdiction's form gives it,
  // such as "90% of this year's tax" or "Tax less withholding under
  // $1,000".
  string label = 2;

  // Where the option is on the jurisdiction's form or in its statute, such
  // as "Form 2210, line 9", so a reader can find it.
  string source = 3;

  // A coarse grouping of the option, for showing like options together
  // across jurisdictions. Not the option's identity: two options of the
  // same basis can follow different rules.
  TargetBasis basis = 4;

  // The amount the option requires over the year. Zero when
  // unavailable, and for an exception.
  taxprotos.common.v1.Money amount = 5;

  // Whether the household can use the option, and whether it is the one
  // the target uses.
  OptionStatus status = 6;

  // Why the option can't be used, in plain words, such as "your 2026
  // California AGI is $1,000,000 or more". Set only under
  // OPTION_STATUS_UNAVAILABLE.
  string why_not = 7;
}

// Whether an option can be used, and whether it was.
enum OptionStatus {
  // Not set. Never returned.
  OPTION_STATUS_UNSPECIFIED = 0;

  // The option the target uses: the lowest amount among the available
  // options, or an exception that applies.
  OPTION_STATUS_CHOSEN = 1;

  // The household can use the option, but another is lower.
  OPTION_STATUS_AVAILABLE = 2;

  // The household can't use the option. why_not says why.
  OPTION_STATUS_UNAVAILABLE = 3;
}

// A coarse grouping of options, for display. A jurisdiction's options are
// identified by EstimatedTargetOption.id; this only says what kind of
// figure an option is measured from.
enum TargetBasis {
  // Not set. Never returned.
  TARGET_BASIS_UNSPECIFIED = 0;

  // Measured from this year's figures, such as 90% of this year's tax.
  TARGET_BASIS_THIS_YEAR = 1;

  // Measured from last year's figures, such as 110% of last year's tax, or
  // last year's income at this year's rates.
  TARGET_BASIS_LAST_YEAR = 2;

  // An exception under which nothing need be paid.
  TARGET_BASIS_EXCEPTION = 3;
}

// One instalment's running totals. Every amount is cumulative, from the
// start of the year to the due date.
message EstimatedInstallment {
  // The instalment's position in the year, 1 for the first.
  int32 number = 1;

  // The due date, after it is moved off a weekend or holiday.
  taxprotos.common.v1.Date due = 2;

  // What withholding and estimated payments must have reached by the due
  // date. It is the target times the jurisdiction's cumulative
  // share for this instalment, or the annualised amount when that is lower.
  // The shares are 25%, 50%, 75% and 100% federally, and 30%, 70%, 70% and
  // 100% in California.
  taxprotos.common.v1.Money needed_by = 3;

  // What withholding and estimated payments had reached by the due date,
  // planned payments included, with withholding credited as
  // EstimatedTaxInputs.withholding_credit says.
  taxprotos.common.v1.Money credited_by = 4;

  // How far credited_by falls short of needed_by.
  // Zero when it doesn't.
  taxprotos.common.v1.Money shortfall = 5;

  // What the totals show, given as_of.
  InstallmentStatus status = 6;
}

// The underpayment penalty, in total and per instalment. How each
// instalment's penalty is charged (by the day, by the month, a flat share,
// tiers) is in the answer's worksheet, line by line.
message EstimatedPenalty {
  // The penalty.
  taxprotos.common.v1.Money total = 1;

  // The day it is figured to: as_of, or the day the jurisdiction's rules
  // stop it, whichever is earlier.
  taxprotos.common.v1.Date figured_to = 2;

  // The penalty assumes a rate not yet published for days after
  // rated_through, so it may change when the rate is.
  bool upper_bound = 3;

  // The last day a published rate covers. Equal to figured_to unless
  // upper_bound is true.
  taxprotos.common.v1.Date rated_through = 4;

  // One sentence on the penalty, in plain words and the household's
  // numbers.
  string summary = 5;

  // The penalty if the waiver claimed is granted. Equal to
  // total when no waiver is claimed.
  taxprotos.common.v1.Money if_waived = 6;

  // Whether the facts meet the conditions of the waiver claimed.
  WaiverOutcome waiver_outcome = 7;

  // Why, for waiver_outcome, in plain words. Empty when no waiver is
  // claimed.
  string waiver_outcome_reason = 8;

  // One entry per instalment with a shortfall, in date order.
  repeated InstalmentPenalty instalments = 9;

  // The penalty on one instalment.
  message InstalmentPenalty {
    // The instalment's number, as EstimatedInstallment.number.
    int32 number = 1;

    // The shortfall the penalty is charged on.
    taxprotos.common.v1.Money shortfall = 2;

    // The day the shortfall was paid, or figured_to if it hasn't been.
    taxprotos.common.v1.Date paid_on = 3;

    // The penalty on this instalment.
    taxprotos.common.v1.Money penalty = 4;
  }
}
```

`DuePayment`, `InstallmentStatus`, `WaiverOutcome`, `EstimatedExplanation`,
`EstimatedQuestion`, `EstimatedDivergence` and `DivergenceReading` move with
their comments rewritten the same way. `EstimatedPenaltyLine`,
`EstimatedPenaltyCharge` and `PenaltyChargeKind` go: the charges are in the
worksheet, as the jurisdiction's form figures them.

#### Each jurisdiction's own worksheet

The worksheet is the jurisdiction's underpayment form, line by line, as
taxlisp already fills return forms. Each form is a taxprotos message, under
the same layout as the return forms: `us/state/<st>/v1/form_<number>`, proto
package `nexteggapp.taxprotos.us.state.<st>.v1`, message `Form<Number>`.
Federal Form 2210, with Schedule AI, is already in taxprotos
(`us/federal/v1/form_1040/form_2210.proto`): the annual return fills it.
Form 2210-F is not, and is added there the same way, as
`us/federal/v1/form_1040/form_2210_f.proto`.

**Fifteen states' forms follow Form 2210:** the same lines and the same rule
for crediting payments, with only the figures changed. Rather than a copied
message each, they share `Form2210Pattern`, which is Form 2210's own message
with the state's form named:

```proto
// The underpayment form of a state whose form follows federal Form 2210
// line for line, and credits payments the same way: each payment to the
// earliest instalment still short. Only the figures differ: shares,
// thresholds, due dates and rates.
message Form2210Pattern {
  // The state's form, by its number and title as printed, such as
  // "Form 2210AL, Underpayment of Estimated Tax by Individuals".
  string form = 1;

  // The form's lines, as federal Form 2210 numbers them, holding the
  // state's figures.
  oneof lines {
    // The regular form, with Schedule AI for the annualised method.
    nexteggapp.taxprotos.us.federal.v1.Form2210 form_2210 = 2;

    // The farmer and fisherman form, for a state whose farmer rules follow
    // Form 2210-F line for line.
    nexteggapp.taxprotos.us.federal.v1.Form2210F form_2210_f = 3;
  }
}
```

**The other 27 have forms of their own,** each its own message in the
`worksheet` oneof. Each departs from Form 2210 in at least one way:
- different shares, such as California's 30/40/0/30;
- a different charge: by the month, a flat share, tiers, an added penalty;
- more than one tax, such as New York City and Yonkers;
- a different order of crediting payments, as in Wisconsin;
- a test Form 2210 has no line for.

[Every state's own messages](#every-states-own-messages) lists every state,
which worksheet it uses, and why.

**A farmer or fisherman's worksheet** is the jurisdiction's farmer form, where
it has one:
- **Federally:** Form 2210-F.
- **California, Iowa, Pennsylvania and Virginia:** each has its own farmer
  form (FTB 5805F, IA 2210F, REV-1630A and 760F). Each is a second field of
  the `worksheet` oneof, such as `ca_farmer`. Where the form follows Form
  2210-F line for line, it is instead `Form2210Pattern` with its
  `form_2210_f` lines.
- **Every other state:** a farmer's worksheet is the state's usual form, with
  whatever lines that form gives farmers.

The rule is strict on purpose. A state whose form differs only slightly still
gets its own message, so `Form2210Pattern` never needs optional lines that
mean something in one state and nothing elsewhere.

**Every option is listed.** `requirement.options` holds each option the
jurisdiction's form lists, with its amount, status and source, and why any
is unavailable. The target is the lowest available one,
`OPTION_STATUS_CHOSEN`. Two examples of an option shown as
`OPTION_STATUS_UNAVAILABLE`:
- **`last_year_only`:** the this-year option, because this year's tax wasn't
  estimated.
- **California at $1,000,000 of AGI:** the last-year option, with the rule
  that closed it.

**When an answer can't be given,** the jurisdiction's answer says so instead
of guessing:
- **No option available:** if `last_year_only` is used and the last-year
  route is closed, `requirement.status` is `REQUIREMENT_STATUS_UNKNOWN`. A
  `questions` entry asks for this year's estimate, says which rule closed the
  last-year route, and what answering would change. Other jurisdictions
  answer normally.
- **A state fact not given:** the answer uses the default its question
  states, such as California AGI under $1,000,000, and asks for the fact.
- **A year with no rules:** the outcome is `ESTIMATED_OUTCOME_NO_RULES`,
  never the nearest year's rules.
- **Facts the rules don't cover:** the outcome is
  `ESTIMATED_OUTCOME_UNSUPPORTED`, with the reason. Within personal income
  tax, nothing is left uncovered by design; this outcome is for a case the
  rules find they can't answer, such as a residence pattern a state's form
  has no line for.

## Implementation in taxlisp

### Layout

The rules sit beside the return rules they read:

```
returns/rules/federal/
  10_estimated_lib.tlisp    shared machinery, for every jurisdiction
  10_estimated.tlisp        federal figures, the requirement, Form 2210 and Schedule AI
returns/rules/states/<st>/
  01_params.tlisp           gains the state's estimated tax figures
  03_estimated.tlisp        the state's requirement, schedule and worksheet
returns/rules/states/00_states.tlisp
                            dispatches estimated tax by jurisdiction, as it
                            dispatches the returns (generated by tools/stategen)
```

As built in step 3, the federal rules are in `10_estimated_us.tlisp`, and
`10_estimated.tlisp` holds only the dispatch. Rule files load in name order,
and the federal figures are a constant built from the library's types, so
they load after `10_estimated_lib.tlisp`. The reference jurisdiction "ZZ"
that `golden/estimated_test.go` defines in Go is written on the same
library in `golden/testdata/estimated/reference.tlisp`, which only the golden
tests load, so the shared helpers are held to `ZZ.json.gz` without a test
jurisdiction in the production rules. `from_return` is answered
`ESTIMATED_OUTCOME_UNSUPPORTED` until this year's tax is read from the
prepared return.

As built in step 4, a state's estimated tax needs no registry entry, so
states landing in parallel touch no shared line by hand. `tools/stategen`
finds them from their files:
- a state's rules are `returns/rules/states/<st>/NN_estimated.tlisp`,
  defining `<name>-estimated-answer` (`alabama-estimated-answer`), which
  `state-estimated-answer` in `00_states.tlisp` dispatches to;
- a state's own facts are `<Name>EstimatedInputs` in a file of its own,
  `returns/v1/estimated_<st>.proto`, which joins the `EstimatedStateInputs`
  oneof with the state's `StateInputs` field number, the `proto_library`,
  and `estimated.proto`'s imports.

The federal `us` worksheet and a pattern state's `Form2210Pattern` are
filled by `est-form-2210` in `10_estimated_lib.tlisp` from the figures the
core came from, and `golden/estimated_worksheet_test.go` checks each
landed answer's worksheet against its core. The `worksheet` oneof's fields
for the states with forms of their own come with batches 2 to 4. A form
that prints a daily factor in place of a rate, as Oregon's Form OR-10 does,
charges it with `est-daily-factor`; Oregon's own worksheet, interest on a
running balance from one event to the next, is in its rules, and its
answer still fills `Form2210Pattern`, as the summary below lists it.

`prepare-return` gains one step. After the federal and state returns, it
does the following for each jurisdiction asked for:
1. It builds that jurisdiction's estimated facts: the common ones, and the
   state's own `EstimatedStateInputs` message, if it has one.
   - With `from_return`, the facts come from the return just computed.
   - With `caller_estimate` or `last_year_only`, they come from the input.
2. It calls the jurisdiction's rules on those facts. The rules fill in the
   common core of the answer and the jurisdiction's own worksheet from the
   same computation, so the two can't disagree.
3. It adds the answer to `ReturnOutputs.estimated`.

`tools/stategen` generates three things from the state registry:
- the `EstimatedStateInputs` oneof;
- the `worksheet` oneof;
- the dispatch.

So a state's estimated tax is one registry entry plus its own files, as its
return is.

### Shared machinery

`10_estimated_lib.tlisp` holds what every jurisdiction does the same way,
parameterised by the jurisdiction's figures:

- **Required annual payment.** It builds the list of options from the
  jurisdiction's bases. Each option is a percentage of this year's tax, a
  percentage of last year's tax with an optional income step, last year's
  income at this year's rates, or an exception. Each is marked available or
  not, with the reason, and the lowest available one is chosen.
- **Instalments.** It builds due dates and cumulative fractions from the
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
  and charges each period at its rate. This is what Form 2210 and the forms
  that follow it do, so it fills `Form2210Pattern`. A state that charges
  another way, such as by the month, a flat share, Illinois's tiers, or
  Michigan's added penalty, figures it in its own worksheet. The total per
  instalment then goes into the common core.
- **Waivers.** It checks a waiver's conditions and works out what would
  remain if granted, leaving the penalty itself in full.

Each function returns its working, as the return rules do. Every figure
carries the rule and source it came from, so the answer's explanation and
the documentation site come from the same place.

### A jurisdiction's rules

A jurisdiction's file is mostly figures, written with `deffigures` and each
one anchored in its source, as the return figures are. Here is an
illustration for California. It shows how a jurisdiction's rules are
written, not part of the API: the struct and anchor names are each state's
PR's to choose.

```lisp
; R&TC § 19136, § 19136.1 and § 19136.3, as the 2026 Form 540-ES
; instructions apply them.
(deffigures ca-estimated-2026 EstimatedYear 2026
  (from ca-ftb-540-es-instructions-2026
    (threshold                 #usd"500.00"      :at who-must-pay)
    (threshold_separate        #usd"250.00"      :at who-must-pay)
    (this_year_rate            0.90              :at required-annual-payment)
    (last_year_rate            1.00              :at required-annual-payment)
    (high_income_agi           #usd"150000.00"   :at required-annual-payment)
    (high_income_rate          1.10              :at required-annual-payment)
    (no_last_year_agi          #usd"1000000.00"  :at required-annual-payment)
    (no_last_year_agi_separate #usd"500000.00"   :at required-annual-payment)
    (instalment_shares         (0.30 0.40 0.00 0.30) :at due-dates)
    (due_dates                 (#date"2026-04-15" #date"2026-06-15"
                                #date"2026-09-15" #date"2027-01-15") :at due-dates)
    (file_by                   #date"2027-01-31" :at file-by)))
```

It also sets the interest-rate series: a table of rates by period, with
each rate's source. Beyond that, a jurisdiction writes only what is its own.
- **A state whose form follows Form 2210** and only changes numbers needs no
  code. Its figures fill `Form2210Pattern` through the shared machinery.
- **A state whose form departs** writes its form with `defform`, line by
  line and cited, as the return forms are: California's FTB 5805, New York's
  IT-2105.9, Illinois's IL-2210. The form fills the state's own worksheet
  message.
- **A state with a test of its own,** such as Pennsylvania's income test or
  Utah's April test, writes that rule against the shared functions. It reads
  the facts it needs from its own `EstimatedStateInputs` message.

The federal rules already work out the Form 2210 penalty on the annual
return. They become the federal jurisdiction's use of the shared machinery,
so the annual return and the during-the-year answer share one
implementation.

### What the engine needs

taxlisp has a `Date` type, but today it can only compare dates and read
their year, month and day. Estimated tax needs:
- **date arithmetic:** days between two dates, and adding days and months;
- **business days:** moving a date past weekends and a given list of holidays
  (each jurisdiction's holidays are figures, not code);
- **rate series:** looking up a rate in a dated table, and splitting a span of
  days where the rate changes.

These are small builtins, added to both engines with tests that hold them
together, in the first PR.

### Explanations and questions

Today each Go package writes its explanation, questions and divergences as
Go strings. They become text in the jurisdiction's rules:
- **explanation:** the summary, the paragraphs, how it differs from federal,
  and the sources;
- **questions:** what the rules still need, each with why it matters and what
  answering would change;
- **divergences:** where the form and the statute disagree, with the reading
  the rules follow.

The documentation site renders the same text.

## Verification

The Go calculators are the reference until they're deleted. Two checks hold
the taxlisp rules to them.

1. **Golden vectors.** `golden/testdata/estimated/<code>.json.gz` holds each
   jurisdiction's answers, today from Go: the requirement, schedule and
   penalty for the households `estimated/estimatedtest` builds, every text
   included. The households themselves are Go code today, so the first step
   writes them out as `ReturnInputs` data, each with a `caller_estimate`,
   beside the answers. A new test runs the engine on each and compares the
   answer with the vector exactly.
   A jurisdiction switches to taxlisp only when its file matches. The
   vectors stay checked in after the Go code is gone, as data.
2. **Returns.** For `from_return`, a test prepares the existing return
   fixtures with estimated tax added, and checks that the figures estimated
   tax read are the return's own: this year's tax, AGI and filing status.
3. **Worksheets.** The Go calculators have no form lines, so the vectors
   check only the common core. Each worksheet is checked another way.
   - Each worksheet is tested against the worked examples in its form's
     instructions, where they have any, and against households worked by
     hand from the form.
   - A test checks that each worksheet's totals equal the common core's: the
     required annual payment, each instalment, and the penalty.
   - Farmers and fishermen have no vectors either, because the Go
     calculators refuse them. Their answers are checked the same way, against
     Form 2210-F's and each state's worked examples and hand-worked
     households.

The Rust engine runs the same rules, so the Rust golden tests check it with
no port of their own.

## Migration

Each step lands as its own PR, and nothing breaks between steps. Every
deletion happens only once its replacement passes the same checks.

1. **Protos.** The common core, in `returns/v1/estimated.proto`:
   `EstimatedTaxInputs` on `ReturnInputs`, and `EstimatedAnswer` on
   `ReturnOutputs`. Nothing is computed yet: each jurisdiction asked for is
   answered `UNSUPPORTED`. Three parts come later, with the step that first
   needs them:
   - **The `worksheet` oneof and `Form2210Pattern`** come with federal (step
     3). `Form2210` is in taxprotos already; `Form2210F` is added there
     first.
   - **The `EstimatedStateInputs` oneof** comes with the first state that
     has inputs of its own (step 4). A oneof can't be empty, and
     `tools/stategen` generates it from those states.
   - **`scenarios/v1` moves** from `us/v1`'s `EstimatedAnswer` to this one
     with taxyear (step 5), which is what fills it.

   The golden test also writes each test household out as `ReturnInputs`,
   so the households outlive the Go code that builds them.
2. **Engine builtins.** Date arithmetic, business days and rate series, in
   both engines.
3. **Federal and the shared machinery.** `10_estimated_lib.tlisp` and
   `10_estimated.tlisp`, with the annual Form 2210 rewritten on top of them.
   This is checked against `US.json.gz`.
4. **States, in batches of about ten.** Each batch's rules are checked
   against its vectors. The same PR:
   - adds each state's own `EstimatedStateInputs` message and worksheet
     message, if it needs them, and its form to taxprotos;
   - leaves those states' Go estimated rules in place, frozen:
     `taxyear/paid` still answers through `estimated/all` until step 5, and
     the golden vectors are checked against them until then.
   - **Batch 1, the fifteen `Form2210Pattern` states:** Alabama, Colorado,
     Iowa, Kansas, Kentucky, Minnesota, Montana, North Carolina, Nebraska,
     New Mexico, Oklahoma, Oregon, Rhode Island, South Carolina and West
     Virginia.
   - **Batch 2:** Arkansas, Arizona, California, Connecticut, DC, Delaware,
     Georgia, Hawaii and Illinois.
   - **Batch 3:** Indiana, Louisiana, Massachusetts, Maryland, Maine,
     Michigan, Missouri, Mississippi and North Dakota.
   - **Batch 4:** New Jersey, New York, Ohio, Pennsylvania, Utah, Virginia,
     Vermont, Washington and Wisconsin.

   Batches 2–4 are the states with forms of their own. A batch can run in
   parallel with another, once federal and the shared machinery have
   landed.
5. **taxyear.** It builds `EstimatedTaxInputs` from its projection and reads
   `ReturnOutputs.estimated`, instead of calling `estimated/all`.
6. **Removal.** Once nothing calls them, `estimated`, `ustaxcalc`'s estimated
   rules and `ustaxcalcsvc` are deleted from Go, and their Rust ports with
   them.

Until step 6, a jurisdiction's Go estimated rules are frozen, moved or not. No new rules or tax
years are added there, and the next tax year's figures go straight into
taxlisp.

## Decisions

Questions the design had to settle, and what it settled them as.

**Interest rates published late.** Rates are cited figures in each
jurisdiction's rules, like every other figure, so there's no input for them.
- The federal rate for a quarter is announced about a month before it
  starts; some states publish later.
- Until a rate is published, the penalty uses the last published rate for
  the days after it. It sets `upper_bound`, with `rated_through` the last day
  a published rate covers.
- A newly published rate ships as an update to the rules.

**Part-year residents.** `EstimatedTaxInputs.residence` holds dated
residence periods, because states prorate the required payment and the
instalments by days or months of residence. The return's state inputs say
whether a household was a part-year resident, but not when. Under
`from_return`, the two must agree.

**Local taxes.** The common core holds one requirement per state, as that
state's own form combines its taxes:
- **Indiana and Maryland:** their forms include county tax with state tax.
- **New York:** counts state, New York City and Yonkers tax separately. That
  split is in its own inputs and its own worksheet, and the common core holds
  their total.

A caller that needs New York City on its own reads New York's worksheet.

**Farmers and fishermen** are in scope.
- **Inputs:** `EstimatedTaxInputs.farming_and_fishing` gives the figures for
  the two-thirds test, this year and last.
- **Federally:** Form 2210-F, with the 66⅔% option, one instalment on
  January 15, and the March 1 exception.
- **States:** each state's rules follow the federal farmer rules or apply its
  own, as [Every state's own messages](#every-states-own-messages) lists.
- **Engine:** a farmer's year is a one-instalment year, which the shared
  machinery handles already.

**`Form2210Pattern` is Form 2210.** A state whose form follows Form 2210 uses
the federal `Form2210` message with its own figures and its form's name,
rather than a separate set of lines. `Form2210` is in taxprotos already, and
the annual return fills it; `Form2210F` is added beside it. A state uses the
pattern only when its
form matches Form 2210 line for line. [Every state's own
messages](#every-states-own-messages) says which states do.

**Option ids.** `EstimatedTargetOption.id` comes from a fixed scheme:
- `this-year-<percent>`, such as `this-year-90` or `this-year-66` for
  farmers;
- `last-year-<percent>`, such as `last-year-100` or `last-year-110`;
- `last-year-income`, for last year's income at this year's rates;
- `exception-<name>`, such as `exception-under-threshold` or
  `exception-no-tax-last-year`.

A new kind of option gets a new id under the same scheme. A test fails when
a jurisdiction's ids change from one year's rules to the next, so callers
can match on them.

**A state reads the federal answer, not inputs about it.** Several states'
tests turn on federal estimated tax:
- **North Dakota:** whether federal estimates were required.
- **Vermont:** the federal required annual payment and instalments.
- **Colorado:** whether the annualised method was used federally.

Federal is worked out in the same request, so a state's rules read the
federal answer. Answering such a state answers federal too, even when the
caller didn't ask for it; the federal answer is then left out of
`ReturnOutputs.estimated`.

**Readings of unsettled law.** Where a state's form and its statute disagree,
or the law is unclear, the rules follow the form and report the statute's
reading as a divergence. Where the Go rules let a caller choose the reading,
the choice is an enum field of that state's own message, named for the
reading. It defaults to the reading the Go rules take today, which is the
form's in all but a few states, so the golden vectors still match. Two examples are Rhode Island's
January return and Mississippi's part months. The caller never chooses a
reading through a generic mechanism, so each choice is documented where it
applies.

**Planned payments.** Whether a payment is made or planned follows from its
date: after `as_of` it is planned. So a payment can't be "planned" with an
earlier date, and there's no flag to disagree with it.

## Every state's own messages

Every jurisdiction with estimated tax, 42 in all:
- **Summary:** each one's underpayment form, whether its worksheet is
  `Form2210Pattern` or its own, how many inputs of its own it reads, and how
  it treats farmers and fishermen.
- **States with inputs of their own:** each state's own message, field by
  field.
- **Worksheets of their own:** why each such state can't use Form 2210.

What isn't repeated here:
- facts the common core holds, such as last year's return, this year's tax,
  and farming and fishing income;
- facts about the federal answer, which a state's rules read from the same
  request;
- due-date moves, which are rules, not inputs.

A reading of unsettled law is an enum in the state's own message. Its
`UNSPECIFIED` value is the reading today's rules take, which is the form's
except where a row says otherwise, so the golden vectors still match. The
answer asks when the other reading would change the figure.

Each state's PR writes its fields as proto in its `<State>EstimatedInputs`
message, with full comments, exactly as listed.

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
| IN | Schedule IT-2210 (and IT-2210A) | own: `FormIt2210` | 4 (1 reading) | follows federal |
| KS | Schedule K-210 | `Form2210Pattern` | 5 (3 readings) | follows federal |
| KY | Form 2210-K | `Form2210Pattern` | 5 | follows federal |
| LA | Form R-210R (R-210NR) | own: `FormR210r` | 3 (1 reading) | follows federal; oyster farming counts |
| MA | Form M-2210 | own: `FormM2210` | 2 (2 readings) | four 25% instalments at 66⅔% if March 1 is missed |
| MD | Form 502UP | own: `Form502up` | 3 | 100%, no 66⅔% |
| ME | Form 2210ME | own: `Form2210me` | 3 | follows federal |
| MI | MI-2210 | own: `FormMi2210` | 2 | follows federal; adds seafarers |
| MN | Schedule M15 | `Form2210Pattern` | 2 (1 reading) | follows federal |
| MO | Form MO-2210 | own: `FormMo2210` | 6 (2 readings) | farming only, 66⅔% |
| MS | Form 80-320 | own: `Form80320` | 2 (1 reading) | 80% by January 15, not 66⅔% |
| MT | Form EST-I | `Form2210Pattern` | 4 | farming or ranching only, wholly exempt |
| NC | Form D-422 (and D-422A) | `Form2210Pattern` | none | follows federal |
| ND | Schedule ND-1UT | own: `FormNd1ut` | 1; reads the federal answer | 66⅔%; the federal return by March 1 decides |
| NE | Form 2210N | `Form2210Pattern` | 2 (1 reading) | follows federal; ranching counts |
| NJ | Form NJ-2210 (NJ-2210NR) | own: `FormNj2210` | 5 (1 reading) | 66⅔% in full by January 15; no March 1 rule |
| NM | Form RPD-41272 | `Form2210Pattern` | 3 (1 reading) | farming or ranching only; penalty only if short by over one-third |
| NY | Form IT-2105.9 | own: `FormIt21059` | 5 | follows federal, on federal Form 2210-F |
| OH | Form IT/SD 2210 | own: `FormItSd2210` | 1 | exempt if filed by March 1, or paid by January 15 and filed on time; no share stated |
| OK | Form OW-8-P (and OW-8-P-SUP-I) | `Form2210Pattern` | none | farming only, wholly exempt |
| OR | Form OR-10 | `Form2210Pattern` | 3 | wholly exempt |
| PA | REV-1630 | own: `FormRev1630` | 5 | farming only; own form REV-1630A |
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

### States with inputs of their own

#### AL — Alabama (`AlabamaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `alabama_agi` | `optional Money` | This year's expected Alabama AGI. The 2026 Form 40ES and § 6654(d)(1)(C), which Code of Ala. § 40-18-80(a) adopts, test last year's Alabama AGI against the $150,000 step, and the rules do too; the 2025 Form 2210AL instructions test the penalty year's own. It decides only whether that divergence affects the household; unset reports it whenever the other percentage would change the target. Added with Alabama's rules, which the golden vectors hold to this divergence. |

#### AZ — Arizona (`ArizonaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `arizona_gross_income` | `optional Money` | This year's expected Arizona gross income: federal AGI for a full-year resident, the part reported to Arizona for a part-year resident, the Arizona-source part for a nonresident (140ES instructions). Estimates are required only when it and last year's exceed $75,000 ($150,000 married filing jointly) (A.R.S. § 43-581(A)); unset falls back to federal AGI, and the answer says so. |
| `prior_year_arizona_gross_income` | `optional Money` | Last year's Arizona gross income, for the same two-year test. Below the threshold in either year, there is no penalty (Form 221 instructions). |
| `nonresident_alien` | `bool` | The filer is a nonresident alien, who makes three instalments (June 15, September 15, January 15) with 50% in the first (140ES instructions). The rules report it as unsupported. |
| `federal_agi` | `optional Money` | This year's expected federal AGI, which the rules take as Arizona gross income when `arizona_gross_income` is unset, and say so. Under `caller_estimate` no other field holds it. Added with Arizona's rules, which the golden vectors hold to this fallback. |
| `prior_year_return_required` | `optional bool` | Whether the filer was required to file last year's Arizona return. The 2026 Form 140ES offers 100% of last year's tax only to a filer who was required to file and filed (worksheet line 23); `PriorYear.return` can't say a return was filed though none was required. Unset keeps the option open, and the answer asks when it sets the target. Added with Arizona's rules. |

Reads the federal facts in the same request, as Colorado does: this year's federal tax less federal withholding, from the caller's federal estimate, and no federal tax liability last year, from `federal_prior_return`, `ReturnInputs.federal.prior_year` and `prior_year_us_citizen_or_resident`. Either is a § 6654 exception, which § 43-581(E)(1) carries into Arizona (Form 221 instructions, line 1).

#### CA — California (`CaliforniaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `california_agi` | `optional Money` | This year's expected California AGI (Form 540, line 17). At $1,000,000 or more ($500,000 married/RDP filing separately) the last-year option is closed, for farmers too (540-ES instructions; FTB 5805F line 10; R&TC § 19136.3); unset under `from_return`, where the engine reads it, and otherwise the answer asks. |
| `prior_year_withholding` | `optional Money` | California income tax withheld last year. No penalty arises when tax less credits, the withholding credit included, was under $500 ($250 married/RDP filing separately) for either year (FTB 5805, Part I; R&TC § 19136(c)(2)). |
| `group_return` | `bool` | The filer is a nonresident included in a group return under R&TC § 18537. From 2026, § 19136(j) takes them out of the penalty. |
| `worksheet_i` | `repeated WorksheetIColumn` | A Form 540NR filer's FTB 5805 Worksheet I, for the annualised method: one column per annualisation period, or none. Under `from_return` the engine fills it (Go: `worksheet_i.go`). |

`WorksheetIColumn`, one period's column:

| Field | Type | Holds |
|---|---|---|
| `tax_on_total` | `Money` | Line A: tax on the annualised total taxable income, Part III line 10. |
| `total_taxable` | `Money` | Line B: annualised total taxable income, Part III line 9. |
| `california_agi` | `Money` | Line D: California AGI for the period, Schedule CA (540NR) Part IV line 1, before annualising. |
| `annualised_total_agi` | `Money` | Line G: annualised total AGI from all sources, Part III line 3. |
| `deduction` | `Money` | Line I: deduction, Part III line 8. |
| `exemption_credits` | `Money` | Line M: exemption credits, Part III line 11. |
| `prorated_credits` | `Money` | Form 540NR lines 51 to 53, prorated by line N. |
| `other_credits` | `Money` | The rest of Part III line 13, as refigured. |
| `other_tax` | `Money` | Part III line 14b. |

#### CT — Connecticut (`ConnecticutEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `pe_tax_credit` | `optional Money` | This year's Pass-Through Entity Tax Credit, Schedule CT-PE line 1. CT-2210 counts it like withholding: against the $1,000 test (line 4) and a quarter of it by each due date (Part 3 lines 12–12b); unset means none. |
| `prior_year_connecticut_source_income` | `optional bool` | A filer who was not a full-year resident last year had Connecticut-source income then. Only then is the no-liability exception open (§ 12-722(j)(2); CT-2210 Box G); without it the filer must use 90% of this year's tax (CT-2210 Box E; CT-1040ES). |

#### DE — Delaware (`DelawareEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `s_corp_payments_and_refundable_business_credits` | `optional Money` | S corporation payments made on the filer's behalf and refundable business credits for the year. PIT-UND line 29 applies them "evenly to each quarter", apart from withholding, which the regular method counts in the period withheld (PIT-UND instructions, lines 29 and 30); unset means none. |

#### GA — Georgia (`GeorgiaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `estimated_deductions` | `optional Money` | This year's estimated deductions, standard or itemised, for the test of gross income against dependent exemptions, deductions and $1,000 of income not subject to withholding (Form 500-ES; IT-511). Unset means the standard deduction for the filing status: $15,000, or $30,000 married filing jointly, for 2026 (HB 463 § 2-3). |
| `dependents` | `optional uint32` | Number of dependents claimed on Form 500, at $5,000 each for 2026 (HB 463 § 2-2), for the same test. Unset means none, as the Go rules read it, and the answer asks. |
| `prior_year_taxable_income` | `optional Money` | Last year's Georgia taxable income. Form 500 UET Exception 1 excuses an instalment when payments to its date reach the tax on it at this year's rates and exemptions (line 14; Instruction D); unset means Exception 1 is not tested, and the answer asks. |
| `employer_withholding_agreement` | `bool` | The employer, by agreement, withholds additional tax to cover the income; estimated tax is then not required (Form 500-ES). |

The gross income in the same test is the core's this-year gross income, `FarmingAndFishing.this_year_gross_income`; with `farming_and_fishing` unset it is unknown, and the answer asks.

#### HI — Hawaii (`HawaiiEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `nonresident_alien_without_wages` | `optional bool` | The filer is a nonresident alien with no wages subject to Hawaii withholding, who pays one-half by June 20, one-fourth by September 20 and one-fourth by January 20 (Tax Facts 2019-3; N-210 instructions, Part III; 26 U.S.C. § 6654(j)). |

#### IA — Iowa (`IowaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `prior_year_iowa_net_modifications` | `optional Money` | Last year's Iowa net modifications, IA 1040 line 3, which can be negative. IA 2210 line 7 adds them to last year's federal AGI to decide whether the last-year share is 110% ($150,000, or $75,000 married filing separate federal returns); unset means unknown, and the answer asks. |

#### IL — Illinois (`IllinoisEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `nursing_home` | `optional bool` | The filer or spouse is 65 or older and lives permanently in a nursing home (Form IL-1040 line 34b; 35 ILCS 5/806; 86 Ill. Adm. Code 100.8000(e)(4) for either spouse on a joint return). When true, no estimated payments are required and no penalty is charged. |
| `pass_through_withholding` | `repeated PassThroughWithholding` | Illinois tax a partnership, S corporation or trust withheld on the filer's behalf (Schedule K-1-P or K-1-T; Form IL-1040 line 27), one entry per entity, each with `entity_year_end` (`Date`) and `amount` (`Money`). IL-2210 line 10b and 35 ILCS 5/804(g)(2) count each as paid on the last day of the entity's tax year, and it counts against the $1,000 test. |
| `pass_through_withholding_answered` | `bool` | True when `pass_through_withholding` is the full answer, an empty list meaning no entity withheld. |
| `late_installment_interest` | `LateInstallmentInterest` | Reading: whether interest runs on a late instalment on top of the penalty, which no Illinois document settles ([ADM-700.220]). `UNSPECIFIED`, the form's reading: none, as IL-2210 computes no interest, and the answer asks; `NONE`: the same reading, answered; `CHARGED`: interest is charged as well. |
| `prior_year_withheld` | `optional Money` | Last year's Illinois withholding, W-2, 1099 and pass-through together. The IL-2210 instructions excuse the penalty when last year's tax after credits less it is zero or less, for a twelve-month year; unset means unknown, and the answer asks when last year showed tax. |

#### IN — Indiana (`IndianaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `county_tax` | `optional Money` | This year's county income tax, the part of this year's tax that is county tax (Form ES-40 line E). It is already inside this year's tax, which Indiana measures as state and county tax together ([IB3]); it is held apart to show the split and to check it is not more than the whole. |
| `pass_through_entity_tax_credit` | `optional Money` | This year's credit for pass-through entity tax paid on the filer's behalf (Schedule 5 line 3). Schedule IT-2210 counts it with withholding, a quarter on each due date (lines 5 and 15), and it reduces the under-$1,000 test (line 6). |
| `prior_year_annualised_tax` | `optional Money` | Last year's tax worked out on a full-year basis, for a part-year resident last year ([2210-25] line 7 example). When set it replaces last year's tax in the last-year option. |
| `twelve_month_rule` | `TwelveMonthRule` | Reading: whether a short prior year closes the last-year option. `UNSPECIFIED`, the forms' reading: it doesn't, as Indiana's forms state no such condition; `APPLIES`: it does, as 26 U.S.C. § 6654, applied by IC 6-3-4-4.1(a), has one. |

#### KS — Kansas (`KansasEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `pass_through_entity_credit` | `optional Money` | This year's credit for tax paid on the K-120S by an electing pass-through entity (Form K-40 line 25). Schedule K-210 line 7 counts it as paid a quarter on each due date, as it does withholding; unset means none. |
| `refundable_credits` | `optional Money` | This year's refundable credits (Form K-40 lines 22 and 23). They count against the $500 threshold and not toward the instalments, as Schedule K-210 line 8 leaves them out; unset means none. Added with Kansas's rules, which read it as the Go rules do. |
| `short_prior_year` | `ShortPriorYear` | Reading: whether a prior-year return of fewer than 12 months opens the last-year option. `UNSPECIFIED`, the K-40ES reading: it doesn't ([ES-26]), and the answer asks; `EXCLUDED`: the same reading, taken, so not asked; `COUNTS`: it does, as no statute requires 12 months. |
| `late_start_installments` | `LateStartInstallments` | Reading: what a filer first required after April 1 owes. `UNSPECIFIED`, the form's reading: a quarter on each date, as Schedule K-210 prints, and the answer asks; `QUARTERS`: the same reading, taken, so not asked; `FEWER_LARGER`: the fewer, larger instalments of K.S.A. 79-32,103(a)(2) to (4). |
| `january_thirty_first` | `JanuaryThirtyFirst` | Reading: whether the January 31 file-and-pay day moves off a weekend, as January 31, 2027 is a Sunday. `UNSPECIFIED`: it doesn't, and the answer asks when the return was filed the next workday; `STAYS`: it doesn't, taken; `MOVES`: it moves to the next workday. Added with Kansas's rules, which read it as the Go rules do. |

#### KY — Kentucky (`KentuckyEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `non_wage_income` | `optional Money` | This year's gross income from sources other than wages on which Kentucky tax is withheld. KRS 141.305(1)(a) requires estimates when it can be expected to exceed $5,000 (and the tax exceeds $500). |
| `filing_threshold_met` | `optional bool` | This year's Kentucky AGI can be expected to reach the amount for which KRS 141.180 requires a return: the other income test of KRS 141.305(1)(b). |
| `combined_return` | `optional bool` | A married couple filing separately does so on one combined Form 740. KY-TAM-19-02 then adds the couple's incomes together for the $150,000 test of the 110% step ([TAM]). |
| `spouse_prior_year_agi` | `optional Money` | The spouse's Kentucky AGI for last year, for a couple on a combined return: added to last year's state AGI for the 110% test ([TAM]). |
| `prior_year_us_citizen_or_resident` | `optional bool` | Every filer was a U.S. citizen or resident for all of last year, which Form 2210-K's exception for no Kentucky tax last year needs. The Go rules read their own answer to this, apart from the full-year residence the core holds, and the golden vectors hold them to it: unset asks, and leaves the exception unapplied. Added with Kentucky's rules. |

#### LA — Louisiana (`LouisianaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `prior_year_income_at_this_year_rates` | `optional Money` | R-210R Exception 3: the tax on last year's return at this year's 3% rate and standard deduction ($12,875, or $25,750 joint or head of household; IT-540ESi (2026) line 3), less last year's credits (R.S. 47:118(D)(1)(b) as Act 307 words it). Payments that reach its share by a due date excuse that instalment; unset shows the option unavailable, and the answer asks. |
| `tax_on_income_to_date` | `repeated TaxToDate` | R-210R Exception 5: the tax on income actually earned from January 1 to each period end (March 31, May 31, August 31, December 31), not annualised, one entry per period with `end` (`Date`) and `tax` (`Money`) (R.S. 47:118(D)(2)). Payments that reach 70% of it by a due date excuse that instalment; under `from_return` the engine works it out, and empty means not claimed. |
| `joint_threshold` | `JointThreshold` | Reading: the under-threshold figure for a joint return. `UNSPECIFIED`, the department's reading: $2,000 (IT-540ESi; R-210R Exception 1); `STATUTE`: $1,000, the one figure R.S. 47:116(A) states. |

#### MA — Massachusetts (`MassachusettsEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `late_start` | `LateStart` | Reading: what a filer first required after March 31 owes. `UNSPECIFIED`, the department's reading: the required annual payment in equal parts over the remaining dates, nothing before (Form 1-ES (2026); AP 241 § 241.3; Form M-2210 line 8); `FOUR_QUARTERS`: four instalments of 25% whenever the requirement arose (M.G.L. c. 62B § 14(c)). |
| `part_year_prior_return` | `PartYearPriorReturn` | Reading: whether last year's part-year or nonresident return opens the 100% last-year option, which no Massachusetts source settles. `UNSPECIFIED`: it counts, as the Go code's figures take it; `EXCLUDED`: it doesn't. |

#### MD — Maryland (`MarylandEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `county_tax` | `optional Money` | This year's local (county or Baltimore City) income tax, inside this year's tax, which Maryland measures as "Maryland and local income tax" (PVW, TY2026). Form 502UP's other-state exception (code 302) compares the underpayment to it. |
| `taxable_by_another_state` | `optional bool` | At least 90% of this year's taxable income is also taxable by another state. Form 502UP then charges no interest when the underpayment is not greater than the local tax (code 302). |
| `maryland_agi` | `optional Money` | This year's Maryland AGI (Form 502 line 16). The 2026 PVW asks for 110% of last year's tax only for a filer whose income rises, where Form 502UP and § 13-602(a)(2) set no condition; with last year's Maryland AGI it decides only whether that divergence affects the household, and unset means it doesn't. Added with Maryland's rules, which read it. |

Form 502UP lines 11 to 13 read the income received by each period's end (March 31, May 31, August 31, December 31), which scales the required payment by the share of the year's income received (code 301). It is the core's `CallerEstimate.PeriodTax.income`, so Maryland's message has no field for it; no periods means the income-share method is not used.

#### ME — Maine (`MaineEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `periods` | `repeated UnusualEventPeriod` | The unusual-event test, one entry per instalment period in order, each with `income_not_withheld` and `prior_year_income_not_withheld` (`Money`, Maine taxable income not subject to Maine withholding in the period, this year and last) and `unusual_event_tax` (`optional Money`, the Maine tax on an excess of $500,000 or more). For such a period 90% of the tax on the excess is due with that instalment, last year's tax does not cover it, and the under-$1,000-last-year exception does not apply to it ([ES-26]; § 5228(1)(D), (2)(D)). |
| `no_unusual_event` | `bool` | The caller says no period had an unusual event. Empty `periods` with this false means not said, and the answer asks. |
| `pass_through_waiver` | `optional bool` | All four conditions of § 5228(11) hold for the unusual event: an owner in a pass-through entity, with no control over the distribution, who did not receive it, and pays the tax on it by the next instalment date after receipt. The assessor then waives the penalty on it or extends the time. |

#### MI — Michigan (`MichiganEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `seafaring_share` | `optional double` | The share of gross income from seafaring, as a fraction (0.25 for 25%). MCL 206.301(6) puts a seafarer under the farmers' and fishermen's rules, and the two-thirds test adds it to farming and fishing; unset means none. |
| `prior_year_estimates_required` | `optional bool` | The filer was required to make estimated payments for last year. When false, MCL 205.24(6) charges no penalty, though interest still runs; unset is worked out from last year's tax and withholding when both are known. |

#### MN — Minnesota (`MinnesotaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `prior_year_step_agi` | `PriorYearStepAgi` | Reading: which AGI the $150,000 test reads for a filer not resident all of one of the two years, since § 289A.25 subd. 5(2)(i) uses the Minnesota share without saying which year. `UNSPECIFIED`: the Minnesota share when not resident all last year, federal AGI otherwise; `FEDERAL_AGI` or `MINNESOTA_SHARE`: that one, whatever the residence. |
| `refundable_credits` | `optional Money` | This year's Minnesota refundable credits, Form M1 line 22. Schedule M15 line 2 counts them with withholding, and § 289A.25 subd. 8 treats them as paid evenly on the four due dates; unset counts only withholding, and the answer asks. Added with Minnesota's rules, which read it. |

#### MO — Missouri (`MissouriEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `prior_year_income_at_this_year_rates` | `optional Money` | MO-2210 Exception 2: the tax on last year's income at this year's rates and exemptions. Payments that reach it by a due date excuse that instalment, for a filer required to file last year (12 CSR 10-2.067(8)(D)); unset shows the option unavailable. |
| `tax_on_income_to_date` | `repeated Money` | MO-2210 Exception 4: the tax on taxable income from January 1 to March 31, May 31 and August 31, each figured as if it were the year's, in date order. Payments that reach 90% of it excuse the instalment, not the fourth; empty means not claimed. |
| `combat_zone_from` | `optional Date` | The first day of service in a combat zone under conditions that qualified for hostile fire pay (MO-2210 instruction I). Instalments due during it are exempt. |
| `combat_zone_to` | `optional Date` | The last day of that service; unset with `combat_zone_from` means not said, and an empty span means no service. |
| `threshold` | `Threshold` | Reading: when the penalty is figured. `UNSPECIFIED`, the form's reading: tax after credits less withholding of $500 or more; `STATUTORY`: $100 or more, the statute's declaration threshold. |
| `line_five` | `LineFive` | Reading: when last year's tax counts toward the required annual payment. `UNSPECIFIED`, as MO-2210 Part I line 5 is printed: whatever last year's return showed; `TWELVE_MONTH_LIABILITY`: only for a 12-month return showing a liability, as Exception 1 and the statute say. |

#### MS — Mississippi (`MississippiEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `prior_year_income_tax_at_this_years_rates` | `optional Money` | The tax on last year's income at this year's Mississippi rates and exemptions, for a household not required to file a resident return last year (35 Miss. Admin. Code Pt. III, Subpt. 11, Ch. 21, R. 100(3)), which then replaces last year's liability. Not given, Form 80-320 line 4 is followed: with line 3 zero and no estimates paid, 80% of this year's tax; negative is refused. |
| `part_month` | `PartMonth` | Reading: whether part of a month is charged as a whole month at ½% a month; § 27-7-327 and Form 80-320 line 9 say only "per month". `UNSPECIFIED`, today's rules' reading: a part month counts as whole; `WHOLE_MONTHS_ONLY`: only whole months are charged, as the Form 80-320 example counts them. |

#### MT — Montana (`MontanaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `composite_return` | `optional bool` | The household takes part in a partnership's or S corporation's composite return for this year. ARM 42.17.314(5) then requires no estimated tax; unset is read as no. |
| `approximately_equal_installments` | `optional bool` | The household's estimated payments were approximately equal quarterly instalments. With the required annual payment met by the year's payments, § 15-30-2512(5)(e) charges no interest; "approximately equal" is undefined, so this is the household's own judgement, and unset asks. |
| `refundable_credits` | `optional Money` | This year's Montana refundable credits, Form 2 lines 15 to 17. Form EST-I Part I line 3b counts them with withholding, a quarter on each due date; unset counts none, and the answer asks. Added with Montana's rules, which read it. |
| `retired_after_62_or_disabled` | `optional bool` | Someone on the return retired after reaching 62, or became disabled, this year or last. § 15-30-2512(1)(b)(iv) then requires no estimated tax; unset is read as no, and the answer asks when there is interest. A waiver claimed in `EstimatedTaxInputs.waiver` is separate. Added with Montana's rules, which read it. |

#### ND — North Dakota (`NorthDakotaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `first_north_dakota_income_on` | `Date` | The day a nonresident, or a household that moved in this year, first received income North Dakota taxes. ND-1UT Item D spreads the required payment over the remaining columns: one-third each from June, one-half each from September, all in January; unset means from January 1. |

Reads the federal answer: whether federal estimates were required, since North Dakota requires them of no one else (Form ND-1ES, 2026, condition 1); whether the federal January instalment was excused, which zeroes the fourth column (ND-1UT Item C); and whether the annualised method was used federally, the only case in which North Dakota allows it (ND-1UT Item B; N.D. Admin. Code 81-03-04-02(3)(c)).

#### NE — Nebraska (`NebraskaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `first_time_filer` | `optional bool` | This year's return is the household's first Nebraska income tax return. Form 2210N's Exceptions to the Penalty, item 2, and the 2026 Form 1040N-ES then charge no penalty, though neither § 77-2769 nor 316 Neb. Admin. Code ch. 20 has the exception; unset is read as no. Added with Nebraska's rules, which read it. |
| `january_deadline` | `JanuaryDeadline` | Reading: whether a return filed and paid on Monday, February 1, 2027 counts as filed by January 31, a Sunday, for the Form 2210N line 9 rule; the form doesn't say. `UNSPECIFIED`, today's rules' reading: the day doesn't move, and the answer asks; `NOT_MOVED`: the same, without asking; `MOVED`: the next business day counts. Added with Nebraska's rules, which read it. |

Form 2210N's no-liability exception also needs U.S. citizenship or residence all last year, which the core's `prior_year_us_citizen_or_resident` holds.

#### NJ — New Jersey (`NewJerseyEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `prior_year_income_tax_at_this_years_rates` | `optional Money` | The tax on last year's gross income at this year's exemptions and rates, less the credit for tax paid to other jurisdictions: the NJ-2210 Exception II worksheet, line 6. Unset means Exception 2 is not tried. |
| `tax_on_income_to_march_31` | `optional Money` | The tax on actual taxable income from January 1 to March 31, less the credit for tax paid to other jurisdictions: the NJ-2210 Exception IV worksheet, line 4, before the 90%. Unset means Exception 4 is not tried for that quarter. |
| `tax_on_income_to_may_31` | `optional Money` | The same, to May 31. |
| `tax_on_income_to_august_31` | `optional Money` | The same, to August 31. |
| `prior_year_overpayment` | `PriorYearOverpayment` | Reading: which instalment last year's overpayment applied forward counts toward. `UNSPECIFIED`, the penalty form's reading: June (NJ-2210 line 6, "column B"); `APRIL`: April (Form NJ-1040-ES). |

#### NM — New Mexico (`NewMexicoEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `first_year_resident` | `optional bool` | This is the household's first year as a New Mexico resident, which NMSA 1978 § 7-2-12.2(M) takes out of estimated tax; unset is read as no, and the answer asks when there is a penalty. Added with New Mexico's rules, which read it. |
| `january_deadline` | `JanuaryDeadline` | Reading: whether a return filed and paid on Monday, February 1, 2027 counts as filed by January 31, a Sunday, for § 7-2-12.2(I); no source says. `UNSPECIFIED`, today's rules' reading: the day doesn't move, and the answer asks; `NOT_MOVED`: the same, without asking; `MOVED`: the next business day counts. Added with New Mexico's rules, which read it. |
| `prior_year_withheld` | `optional Money` | The New Mexico income tax withheld on last year's PIT-1, which Form RPD-41272 line 4 compares with this year's tax where § 7-2-12.2(H)(1) reads this year's withholding. It decides only whether that divergence affects the household; unset reports it whenever tax less this year's withholding is under $1,000. Added with New Mexico's rules, which the golden vectors hold to this divergence. |

#### NY — New York (`NewYorkEstimatedInputs`)

One computation covers New York State, New York City, Yonkers and MCTMT tax (Tax Law § 685(c), (d)); the core's this year's and last year's tax are the four combined.

| Field | Type | Holds |
|---|---|---|
| `subject_to_new_york_city_tax` | `optional bool` | The household owes New York City resident tax this year. Each of the state, City and Yonkers taxes adds $300 to the threshold (IT-2105.9 line 15 worksheet); unset under `from_return`, otherwise the answer asks. |
| `subject_to_yonkers_tax` | `optional bool` | The household owes the Yonkers resident surcharge or nonresident earnings tax this year, adding $300 to the threshold. |
| `subject_to_mctmt` | `optional bool` | The household owes MCTMT on self-employment earnings this year. Any MCTMT owed requires estimates; it adds nothing to the threshold (IT-2105-I, 2026). |
| `prior_year_mctd_earnings` | `optional Money` | Last year's net earnings from self-employment allocated to the Metropolitan Commuter Transportation District. Above $150,000 the last-year option is 110% even if New York AGI is not (IT-2105-I, 2026); unset with MCTMT owed is read as above $150,000, and the answer asks. |
| `prior_year_new_york_source_income` | `optional bool` | A household not resident all last year had New York source income throughout it, which the no-liability exception needs (§ 685(d)(2); IT-2105.9-I). |

#### OH — Ohio (`OhioEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `prior_year_filed_on_time` | `optional bool` | Last year's IT 1040 was filed by its due date, extensions included. IT/SD 2210 line 6 offers last year's tax only then, where the statute asks only that a return was filed (ORC 5747.09(A)(4)), and the form is followed; unset asks. |

#### OR — Oregon (`OregonEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `prior_year_filed_first` | `optional bool` | Last year's return was or will be filed before this year's. OAR 150-316-0493(3)(a)(B) and Publication OR-ESTIMATE allow the last-year option only then; unset asks. |
| `gross_income_test_met` | `optional bool` | ORS 316.563(1)'s gross income test is met: more than $1,000 of gross income other than wages, or gross income over $20,000 ($10,000 joint with both spouses earning wages, $5,000 married not entitled to a joint declaration). Needed only when the core's this-year gross income is not over $20,000; not met, no declaration is required. |
| `oregon_business_income` | `optional OregonBusinessIncome` | For a part-year resident, the instalment periods (`repeated int32 periods`, 1 to 4) with income from an Oregon trade or business while living elsewhere. Form OR-10 divides the required annual payment over the periods of residence or Oregon business income. |

#### PA — Pennsylvania (`PennsylvaniaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `income_not_subject_to_withholding` | `optional Money` | This year's expected PA-taxable income from which no employer withholds Pennsylvania tax (REV-414 (I) line 1). Estimates are required only when it exceeds $14,000 for 2026 (72 P.S. § 7325(a)(1); the REV-413 (I) says "$14,000 or more"); at or under it the answer is exempt. |
| `credits` | `optional Money` | This year's Tax Forgiveness, resident and other credits, PA-40 lines 21 to 23. REV-1630 Section I takes 90% of the tax before them and subtracts them with withholding (lines 1a to 4), so the rules add them back to the core's tax after credits; read from the return under `from_return`. |
| `prior_year_taxable_income` | `optional Money` | Last year's PA taxable income, PA-40 line 11. Exception 1 (REV-1630 line 12; REV-413 (I) lines 10 to 14) is this income at this year's 3.07% rate, for a full-year resident who filed; unset closes that option, and the answer asks. |
| `prior_year_tax_forgiveness_credit` | `optional Money` | Last year's Tax Forgiveness Credit, PA-40 line 21, which REV-1630 line 12 subtracts from Exception 1. Unset subtracts none, which can only overstate that target, and the answer asks. |
| `prior_year_full_tax_forgiveness` | `optional bool` | The household qualified for 100% Tax Forgiveness on last year's PA-40 (Schedule SP; Act 85 of 2012). Such a filer is not subject to the penalty this year (REV-1630 (2025), Before You Begin). |

#### RI — Rhode Island (`RhodeIslandEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `january_return` | `JanuaryReturn` | Reading: by which day a return filed and paid stands in for the January instalment; the Division's two forms disagree. `UNSPECIFIED` takes the penalty form's reading and asks when a return filed after January 31 and by the coupon's date would count under the other; `PENALTY_FORM`, the same reading without asking: on or before January 31 (RI-2210 (2025) line 16 instructions); `COUPON`: by February 15, moved to February 16, 2027 (2026 RI-1040ES; § 44-30-55(e); § 44-30-91(b)). |
| `two_dollar_minimum` | `TwoDollarMinimum` | Reading: whether § 44-30-84(a)(1), which makes interest under $2.00 not payable, reaches estimated-tax interest under subsection (b). `UNSPECIFIED`: it doesn't, the charge is shown without it, and the answer asks when the charge is under $2.00; `DOES_NOT_APPLY`: the same without asking; `APPLIES`: interest under $2.00 is not payable. |

#### SC — South Carolina (`SouthCarolinaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `underpayment_from_other_state_income` | `optional bool` | The whole underpayment comes from personal service income earned in another state that withheld its own tax. No interest or penalty is due on it (§ 12-54-55(1)), and SC2210 lists it as a waiver reason; a share of the underpayment is not modelled. |
| `nonresident_contractor` | `optional bool` | The filer is a nonresident working in South Carolina under contracts over $10,000 with 2% withheld from each contract payment. The SC1040ES (2026) excuses such a filer from a declaration. |

#### UT — Utah (`UtahEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `files_on_extension` | `optional bool` | The return will be filed after the original due date under the automatic six-month extension. The prepayment test and its 2%-a-month penalty apply only then (Utah Code §§ 59-10-516, 59-1-401(5); Publication 58); unset shows the test and asks. |

#### VA — Virginia (`VirginiaEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `virginia_agi` | `optional Money` | This year's expected Virginia AGI. Under the Form 760ES (2026) Section I floor, $11,950 single or married filing separately and $23,900 married filing jointly, no return and no Form 760C is needed; read from the return under `from_return`. |
| `prior_year_income_tax` | `optional Money` | Form 760C Exception 2 Worksheet line 7: the tax on last year's income at this year's rates and personal exemptions, less last year's Spouse Tax Adjustment and credits. Payments reaching a quarter of it for each date so far excuse that instalment, with no need for a filed return or a liability last year (§ 58.1-492(C)); unset closes Exception 2. |
| `income_to_date_tax` | `repeated Money` | Form 760C line 19f: the Virginia tax on taxable income actually received from January 1 to April 30, May 31 and August 31, in that order. Exception 4 compares payments with 90% of each, not for the fourth instalment; derived from the periods under `from_return`, and empty closes Exception 4. |
| `merchant_seaman_gross_income` | `optional Money` | This year's gross income from working as a merchant seaman. It counts with the core's farming and fishing toward the two-thirds that puts a filer under Form 760F (§ 58.1-490(F); 760ES Section VIII); unset means none. |

#### WA — Washington (`WashingtonEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `extension` | `optional bool` | The filer had a federal filing extension and requested the Washington extension through My DOR by the due date (RCW 82.87.110(5)). With it, an estimate paid on time avoids the late payment penalty and is tested against 80% of the tax (RCW 82.32.090(10)); without it, the 9% / 19% / 29% late payment penalty applies (RCW 82.32.090(1)). |
| `five_years_on_time` | `optional bool` | The filer paid every capital gains tax return on time in the preceding five calendar years and was not contacted for enforcement for this period. The department must then waive the RCW 82.32.090(1) penalty on request (RCW 82.87.110(7)). |

#### WI — Wisconsin (`WisconsinEstimatedInputs`)

| Field | Type | Holds |
|---|---|---|
| `federal_disaster_extension` | `optional bool` | The filer qualifies for a federal extension of time under 26 U.S.C. § 7508A for a federally declared disaster or terroristic or military action. No interest (Wis. Stat. § 71.09(11)(e)); Schedule U exception code 10. |
| `active_duty_outside_us` | `optional bool` | The filer was on active duty with the U.S. armed forces while stationed outside the continental United States. The estimated tax section does not apply (§ 71.09(2)); Schedule U exception code 07. |

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
| IN | `us/state/in/v1/form_it_2210`, `FormIt2210` | A flat 10% of each period's underpayment, whatever the days late ([IC-2.1](b)), and a part-year resident divides by the periods of residence (line 14). |
| LA | `us/state/la/v1/form_r_210r`, `FormR210r` | Per-instalment exceptions, Exception 5 on actual income to date ([RS-118](D); [ACT-307]), and a statutory 12% daily factor to May 15 (line 18). |
| MA | `us/state/ma/v1/form_m_2210`, `FormM2210` | Part 3 figures the 4% surtax on its own lines (period income less the full-year threshold, times the annualisation amount, times 4%). |
| MD | `us/state/md/v1/form_502up`, `Form502up` | Cumulative columns charged again in every later column at a printed factor, by the month (502UP line 14; § 13-604(a)), and an income-share method in place of annualising (lines 11–13). |
| ME | `us/state/me/v1/form_2210me`, `Form2210me` | Monthly interest, a part month as whole, compounded monthly at the § 186 rate ([2210-25] line 13; [MRS-5228](5)), and the unusual-event rule (§ 5228(1)(D)). |
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
| VT | `us/state/vt/v1/form_in_152`, `FormIn152` | Interest plus a 1% a month penalty capped at 25% (IN-152 lines 7–12; 32 V.S.A. § 3202(b)(2)), and annualising that scales the federal Schedule AI (IN-152A line 6). |
| WA | `us/state/wa/v1/capital_gains_payment`, `CapitalGainsPayment` | One payment of the whole tax on April 15 (RCW 82.87.110(3)), and a flat 9/19/29% late payment penalty, not interest (RCW 82.32.090(1)). |
| WI | `us/state/wi/v1/schedule_u`, `ScheduleU` | A payment goes first to its own period's instalment, then to the most recent shortfall, where Form 2210 pays the oldest first (Schedule U). |
