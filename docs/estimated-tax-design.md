# Design: estimated tax in taxlisp

Status: proposed.

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
8. [Open questions](#open-questions)

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
- **Documented like the returns.** Each jurisdiction's estimated tax rules
  appear on the documentation site, with citations, as its return rules do.

Not goals here:
- Farmers and fishermen. The Go calculators refuse these today, as
  unsupported, and the taxlisp version starts the same way.
- Fiduciaries, corporations, and taxes other than personal income tax. The one
  exception is Washington's capital gains excise, which the Go rules already
  cover.

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
- **Arizona** — no estimates below $75,000 of Arizona gross income ($150,000
  joint) in both years.
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

Each jurisdiction implements one Go interface:

```go
type EstimatedTax interface {
	Requirement(f Facts) (working.Result[Requirement], error)
	Schedule(f Facts) (working.Result[[]Installment], error)
	Penalty(f Facts, asOf Date) (working.Result[Penalty], error)
	Explanation() Explanation
}
```

`estimated.Facts` is a hand-built description of the household for one
jurisdiction and year. It includes this year's tax and AGI as plain numbers,
because the calculators cannot work out a return.

The shared machinery is in `estimated`: dates and holidays, crediting, the
instalment ledger and interest. Each package supplies its own figures and
special cases, in about 44,000 lines of Go for the states.

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
// answer for, last year's figures, the payments, and how this year's tax
// is known.
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
  EstimatedPenaltyWaiver waiver = 7;

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

  // One state's figures from last year, for its last-year option and its
  // income tests.
  message PriorYear {
    // The state's postal code, such as "CA". Never "US".
    string jurisdiction = 1;

    // Whether a return was filed for last year, and the period it covered.
    PriorReturn return = 2;

    // Where the household lived last year, as the state counts residence.
    Residency residency = 3;

    // The tax on last year's return, after nonrefundable credits, as the
    // state's estimated tax rules define it. Read unless return is
    // PRIOR_RETURN_NOT_FILED. Negative is refused.
    taxprotos.common.v1.Money tax = 4;

    // Last year's adjusted gross income as the state defines it, for income
    // tests, such as the step from 100% to 110% of last year's tax above
    // $150,000. Read unless return is PRIOR_RETURN_NOT_FILED.
    taxprotos.common.v1.Money agi = 5;
  }

  // Whether last year's return was filed, and the period it covered.
  enum PriorReturn {
    // Not set. Refused.
    PRIOR_RETURN_UNSPECIFIED = 0;

    // No return was filed for last year. The last-year option is closed.
    PRIOR_RETURN_NOT_FILED = 1;

    // A return was filed, covering a full 12-month year.
    PRIOR_RETURN_FULL_YEAR = 2;

    // A return was filed, covering less than 12 months, such as a first
    // year. Most jurisdictions, like the federal rules, then close the
    // last-year option.
    PRIOR_RETURN_SHORT_YEAR = 3;
  }

  // Where the household lived during a year, as a state counts residence.
  enum Residency {
    // Not set. Refused.
    RESIDENCY_UNSPECIFIED = 0;

    // A resident of the state for the whole year.
    RESIDENCY_FULL_YEAR = 1;

    // A resident for part of the year. Some states then close the
    // last-year option.
    RESIDENCY_PART_YEAR = 2;

    // Not a resident at any time in the year.
    RESIDENCY_NONRESIDENT = 3;
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
  // jurisdiction's tax, AGI and withholding come from the return the
  // engine prepares. If the return can't be computed, every answer is
  // ESTIMATED_OUTCOME_REFUSED, with the return's diagnostics as the reason.
  message FromReturn {
    // The annualised income method: one entry per period, in date order.
    // Empty means the regular method. A jurisdiction without the annualised
    // method ignores it, and says so in its answer's questions.
    repeated Period periods = 1;
  }

  // This year's figures as the caller estimates them, per jurisdiction. The
  // return is not computed: of the rest of ReturnInputs, only the filing
  // status, the household and prior_year are read.
  message CallerEstimate {
    // One entry per jurisdiction answered for. A jurisdiction with no entry
    // is answered as under LastYearOnly.
    repeated Jurisdiction jurisdictions = 1;

    // One jurisdiction's figures for this year.
    message Jurisdiction {
      // The jurisdiction's postal code: "US" for federal, or a state's.
      string jurisdiction = 1;

      // This year's expected tax, after nonrefundable credits, as the
      // jurisdiction's estimated tax rules define it. Unset means not
      // estimated: the this-year option is then unavailable.
      optional taxprotos.common.v1.Money tax = 2;

      // This year's expected adjusted gross income as the jurisdiction
      // defines it, for income tests measured on this year, such as
      // California's $1,000,000 test. Unset means unknown: a test that
      // needs it becomes a question in the answer.
      optional taxprotos.common.v1.Money agi = 3;

      // This year's expected withholding for the jurisdiction, credited
      // under WITHHOLDING_CREDIT_EVENLY. Unset means none.
      optional taxprotos.common.v1.Money withheld = 4;

      // The annualised income method: one entry per period, in date order.
      // Empty means the regular method.
      repeated PeriodTax periods = 5;
    }

    // One annualisation period's figures, given by the caller.
    message PeriodTax {
      // The period's last day, as for Period.end.
      taxprotos.common.v1.Date end = 1;

      // Income from January 1 to end, as the jurisdiction defines it.
      taxprotos.common.v1.Money income = 2;

      // The tax on that income once annualised, before the period's
      // cumulative share is applied, as the jurisdiction's annualised
      // worksheet figures it.
      taxprotos.common.v1.Money annualised_tax = 3;
    }
  }

  // Use only last year's safe harbour. The this-year option is unavailable
  // in every answer, and a jurisdiction whose last-year option is closed
  // asks for this year's figures instead of answering. Empty today; a
  // message, not a flag, so assumptions can be added later, such as this
  // year's AGI staying under California's $1,000,000.
  message LastYearOnly {}
}

// The existing message, with the facts a waiver's conditions turn on.
message EstimatedPenaltyWaiver {
  // ... existing fields: requested_waiver_amount,
  // qualified_farmer_or_fisherman ...

  // Why the waiver is claimed. Unspecified is refused.
  WaiverCause cause = 3;

  // The day of the casualty, disaster, retirement or onset of disability.
  // Required.
  taxprotos.common.v1.Date occurred = 4;

  // For WAIVER_CAUSE_RETIRED_OR_DISABLED: the birth date of the filer who
  // retired or became disabled. A retirement counts only at 62 or older.
  taxprotos.common.v1.Date born = 5;

  // Why a penalty waiver is claimed.
  enum WaiverCause {
    // Not set. Refused.
    WAIVER_CAUSE_UNSPECIFIED = 0;

    // A casualty, disaster or other unusual circumstance made the penalty
    // inequitable (26 U.S.C. § 6654(e)(3)(A)).
    WAIVER_CAUSE_CASUALTY = 1;

    // A filer retired at 62 or older, or became disabled, in this tax year
    // or last, and the shortfall had reasonable cause (§ 6654(e)(3)(B)).
    WAIVER_CAUSE_RETIRED_OR_DISABLED = 2;
  }
}
```

**How this year's tax is known** is a oneof, because each choice needs
different data:

| | This year's figures | The rest of `ReturnInputs` | Annualised periods |
|---|---|---|---|
| `from_return` | the engine works them out from the return | must be complete enough for the return to be computed | income per period; the engine works out the tax |
| `caller_estimate` | the caller gives tax, AGI and withholding per jurisdiction | only the filing status, household and federal prior year are read | income and annualised tax per period |
| `last_year_only` | none | only the filing status, household and federal prior year are read | not possible |

Choices behind the shape:
- **Shared fields sit outside the oneof.** Payments, withholding, last
  year's figures and the as-of date are needed whichever way this year's tax
  is known.
- **Each mode carries only its own data.** Annualised periods live inside the
  modes that can use them. "Annualise with nothing to annualise" can't be
  expressed, and a non-empty `periods` list is the election.
- **`caller_estimate` keeps today's way of working.** A spreadsheet, a quick
  estimate, or a service that already knows the figure doesn't have to build
  a return.
- **`LastYearOnly` is an empty message, not a flag,** so a field can be added
  later without breaking callers.
- **Each figure in `CallerEstimate` is optional.** A caller can give
  California's AGI without its tax, which is what California's
  $1,000,000 test needs.
- **Enums where values are states.**
  - Last year's return is not filed, full year or short year, not two
    booleans that could say "not filed but full year".
  - Residence and how withholding is credited are named states too.
- **Made or planned follows from the date.** A payment is planned when it is
  dated after `as_of`, so there's no flag that could disagree with its date.

**Payments, waivers and last-year figures reuse what `ReturnInputs` has.**
`EstimatedPayment` and `EstimatedPenaltyWaiver` already exist for the annual
return. Only what's missing is added: the waiver's cause and dates, and the
states' last-year figures.

Inputs only one state reads go in that state's existing `StateInputs`
message, as the return's state inputs do. Examples are Pennsylvania's Tax
Forgiveness or a state-only waiver. `tools/stategen` keeps that list.

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

`EstimatedAnswer` and the messages under it exist today in
`us/v1/estimated.proto`, and `scenarios/v1` carries them. They move into
`returns/v1`, and the move is the moment to change them, with
`scenarios/v1` updated in the same PR:
- `EstimatedAnswer` gains a `jurisdiction` field;
- booleans that together describe one state become enums;
- every comment is rewritten to say exactly what the field holds and how the
  fields relate.

The ones a caller reads most:

```proto
// One jurisdiction's estimated tax answer for one tax year: whether
// payments are needed and how much, what is needed by each due date, and
// what the penalty is if payments go as given.
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

  // Whether payments are needed, every safe harbour considered, and the
  // total the chosen one requires.
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

  // The rules exist but don't cover these facts, such as a farmer's single
  // instalment. Only reason is set.
  ESTIMATED_OUTCOME_UNSUPPORTED = 3;

  // The facts as given are inconsistent or incomplete, such as a FromReturn
  // whose return can't be computed. Only reason is set.
  ESTIMATED_OUTCOME_REFUSED = 4;
}

// Whether a household has to make estimated payments to a jurisdiction,
// and the required annual payment: the total that withholding and estimated
// payments together must reach over the year to avoid the penalty.
//
// The required annual payment is the smallest of the safe harbours the
// household can use, each listed in options. For example, with this year's
// federal tax at $30,000 and last year's at $20,000 on AGI over $150,000,
// the options are 90% of this year's tax, $27,000, and 110% of last year's,
// $22,000. The target is $22,000.
message EstimatedRequirement {
  // Whether payments are required, and whether a target could be measured.
  RequirementStatus status = 1;

  // Why the status is what it is, in plain words and the household's
  // numbers.
  string why = 2;

  // The required annual payment, in cents: the amount of the chosen option.
  // Zero under REQUIREMENT_STATUS_EXEMPT and REQUIREMENT_STATUS_UNKNOWN.
  int64 target_cents = 3;

  // Every safe harbour and exception the jurisdiction has, in a fixed order
  // for the jurisdiction, including those this household can't use. Exactly
  // one is OPTION_STATUS_CHOSEN, except under REQUIREMENT_STATUS_UNKNOWN,
  // when none is.
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
  // California household with AGI of $1,000,000 or more. There is no
  // target, no instalments and no penalty, and the answer's questions ask
  // for this year's figures.
  REQUIREMENT_STATUS_UNKNOWN = 4;
}

// One safe harbour or exception: one way of measuring the required annual
// payment.
message EstimatedTargetOption {
  // What the option is measured against.
  TargetBasis basis = 1;

  // The share of the basis, in basis points: 9000 for 90% of this year's
  // tax, 11000 for 110% of last year's. Zero for an exception.
  int32 percent_basis_points = 2;

  // The amount the option requires over the year, in cents. Zero when
  // unavailable, and for an exception.
  int64 amount_cents = 3;

  // Whether the household can use the option, and whether it is the one
  // the target uses.
  OptionStatus status = 4;

  // Why the option can't be used, in plain words, such as "your 2026
  // California AGI is $1,000,000 or more". Set only under
  // OPTION_STATUS_UNAVAILABLE.
  string why_not = 5;

  // The option's name in plain words, as the jurisdiction's form gives it,
  // such as "90% of this year's tax" or "Tax less withholding under
  // $1,000".
  string label = 6;
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

// What a safe harbour is measured against.
enum TargetBasis {
  // Not set. Never returned.
  TARGET_BASIS_UNSPECIFIED = 0;

  // A share of this year's tax, such as 90% federally or 70% in Colorado.
  TARGET_BASIS_THIS_YEAR_TAX = 1;

  // A share of last year's tax, such as 100%, or 110% above an income step.
  TARGET_BASIS_LAST_YEAR_TAX = 2;

  // Last year's income taxed at this year's rates, as Louisiana, Missouri,
  // Virginia and Pennsylvania allow.
  TARGET_BASIS_LAST_YEAR_INCOME = 3;

  // An exception under which nothing need be paid, such as no tax last
  // year.
  TARGET_BASIS_EXCEPTION = 4;
}

// One instalment's running totals. Every amount is cumulative, from the
// start of the year to the due date.
message EstimatedInstallment {
  // The instalment's position in the year, 1 for the first.
  int32 number = 1;

  // The due date, after it is moved off a weekend or holiday.
  CalendarDate due = 2;

  // What withholding and estimated payments must have reached by the due
  // date, in cents. It is the target times the jurisdiction's cumulative
  // share for this instalment, or the annualised amount when that is lower.
  // The shares are 25%, 50%, 75% and 100% federally, and 30%, 70%, 70% and
  // 100% in California.
  int64 needed_by_cents = 3;

  // What withholding and estimated payments had reached by the due date, in
  // cents, planned payments included, with withholding credited as
  // EstimatedTaxInputs.withholding_credit says.
  int64 credited_by_cents = 4;

  // How far credited_by_cents falls short of needed_by_cents, in cents.
  // Zero when it doesn't.
  int64 shortfall_cents = 5;

  // What the totals show, given as_of.
  InstallmentStatus status = 6;
}
```

The rest move with their comments rewritten the same way:
- `DuePayment` and `InstallmentStatus`;
- `EstimatedPenalty`, `EstimatedPenaltyLine`, `EstimatedPenaltyCharge`, `PenaltyChargeKind` and `WaiverOutcome`;
- `EstimatedExplanation`, `EstimatedQuestion`, `EstimatedDivergence` and `DivergenceReading`.

The penalty breaks down into one line per short instalment, and each line
into charges, each with its principal, dates, days, rate and the source of
the rate. `EstimatedPenalty.upper_bound` stays a bool: it is a plain fact,
true when the penalty assumes rates not yet published.

**Every option is listed.** `requirement.options` holds each basis the
jurisdiction has, with its amount and status, and why any is unavailable.
The target is the lowest available one, `OPTION_STATUS_CHOSEN`. Two examples
of an option shown as `OPTION_STATUS_UNAVAILABLE`:
- **`last_year_only`:** the this-year option, because this year's tax wasn't
  estimated.
- **California at $1,000,000 of AGI:** the last-year option, with the rule
  that closed it.

**When an answer can't be given,** the jurisdiction's answer says so instead
of guessing:
- **No basis available:** if `last_year_only` is used and the last-year route
  is closed, `requirement.status` is `REQUIREMENT_STATUS_UNKNOWN`. A
  `questions` entry asks for this year's estimate, says which rule closed the
  last-year route, and what answering would change. Other jurisdictions
  answer normally.
- **A year with no rules:** the outcome is `ESTIMATED_OUTCOME_NO_RULES`,
  never the nearest year's rules.
- **Facts the rules don't cover, such as farmers:** the outcome is
  `ESTIMATED_OUTCOME_UNSUPPORTED`.

## Implementation in taxlisp

### Layout

The rules sit beside the return rules they read:

```
returns/rules/federal/
  10_estimated_lib.tlisp    shared machinery, for every jurisdiction
  10_estimated.tlisp        federal figures, the requirement, Form 2210 and Schedule AI
returns/rules/states/<st>/
  01_params.tlisp           gains the state's estimated tax figures
  03_estimated.tlisp        the state's requirement, schedule and penalty worksheet
returns/rules/states/00_states.tlisp
                            dispatches estimated tax by jurisdiction, as it
                            dispatches the returns (generated by tools/stategen)
```

`prepare-return` gains one step. After the federal and state returns, for
each jurisdiction asked for, it builds that jurisdiction's estimated facts
and calls its rules:
- with `from_return`, from the return just computed;
- with `caller_estimate` or `last_year_only`, from the input.

The answer is added to `ReturnOutputs.estimated`.

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
- **Charges.** Each charge is one of four kinds:
  - interest by the day, at a rate per period;
  - interest by the month;
  - a flat percentage of the shortfall, Indiana's kind;
  - a factor the form prints.

  Compounding and Illinois's lateness tiers are options of these.
- **Waivers.** It checks a waiver's conditions and works out what would
  remain if granted, leaving the penalty itself in full.

Each function returns its working, as the return rules do. Every figure
carries the rule and source it came from, so the answer's explanation and
the documentation site come from the same place.

### A jurisdiction's rules

A jurisdiction's file is mostly figures, written with `deffigures` and each
one anchored in its source, as the return figures are. Here is an
illustration for California; the struct and anchor names are not final.

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

Then it adds a short `defworksheet` for whatever is genuinely its own, such
as California's FTB 5805 or New York's IT-2105.9. It also sets the
interest-rate series, a table of rates by period with each rate's source.
A jurisdiction that only changes numbers needs no code of its own; one with
its own rule, such as Pennsylvania's income test or Utah's April test,
writes that rule against the shared functions.

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

The Rust engine runs the same rules, so the Rust golden tests check it with
no port of their own.

## Migration

Each step lands as its own PR, and nothing breaks between steps. Every
deletion happens only once its replacement passes the same checks.

1. **Protos.** `EstimatedTaxInputs`, `ReturnOutputs.estimated`, and
   `EstimatedAnswer` moved into `returns/v1`, with `scenarios/v1` pointing at
   it. Nothing is computed yet: a request for estimated tax returns
   `UNSUPPORTED`. The golden test also writes each test household out as
   `ReturnInputs`, so the households outlive the Go code that builds them.
2. **Engine builtins.** Date arithmetic, business days and rate series, in
   both engines.
3. **Federal and the shared machinery.** `10_estimated_lib.tlisp` and
   `10_estimated.tlisp`, with the annual Form 2210 rewritten on top of them.
   This is checked against `US.json.gz`.
4. **States, in batches of about ten.** Each batch's rules are checked
   against its vectors, and the same PR deletes those states' Go packages
   and their `estimated/all` entries.
   - First the states that only change numbers.
   - Then those with a different schedule or option: California, Delaware,
     Hawaii, Iowa, Virginia, Maryland, DC, Louisiana, Missouri and Maine.
   - Last the states with rules of their own: Pennsylvania, Utah,
     Mississippi, Washington, Indiana, Illinois, Michigan and Vermont.
5. **taxyear.** It builds `EstimatedTaxInputs` from its projection and reads
   `ReturnOutputs.estimated`, instead of calling `estimated/all`.
6. **Removal.** Once nothing calls them, `estimated`, `ustaxcalc`'s estimated
   rules and `ustaxcalcsvc` are deleted from Go, and their Rust ports with
   them.

Until a jurisdiction moves, its Go package is frozen. No new rules or tax
years are added there, and the next tax year's figures go straight into
taxlisp.

## Open questions

- **Interest rates published late.** The federal rate for a quarter is
  announced about a month before it starts; some states publish later. The
  Go rules work out the penalty only as far as rates are known and say so in
  `rated_through`. The taxlisp version keeps that, but needs a home for rates
  added between releases: figures in the rules, or an input.
- **Part-year residents.** Today's `Facts` carries residence periods. The
  return's state inputs carry residence too, but not by date. Estimated tax
  may need the dated form.
- **Local taxes.** Indiana's and Maryland's county taxes, and New York City's
  and Yonkers', sit inside the state rules today. They would stay there; the
  question is whether a caller ever needs them as separate answers.
- **Farmers and fishermen.** Not supported now. The shared machinery has a
  place for their single instalment and two-thirds share, to fill in once a
  caller needs it.
- **What `as_of` means for planned payments.** Today the penalty counts every
  payment on its own date, made or planned, and also gives the penalty
  without the planned ones. That stays, but the input should say whether a
  payment dated before `as_of` and still marked planned is an error.
