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
  // ... existing: household, documents, federal and state inputs ...
  PriorYear prior_year = ...;            // exists: federal prior AGI and total tax
  EstimatedTaxInputs estimated_tax = N;  // new; unset means no estimated tax
}

// What estimated tax needs beyond the year's return: last year's figures,
// payments made and planned, and how this year's tax is known.
message EstimatedTaxInputs {
  // The date the penalty and what is still due are worked out as of.
  Date as_of = 1;

  // Each jurisdiction's last-year figures. The federal ones are in
  // ReturnInputs.prior_year.
  repeated PriorYear prior_years = 2;

  // Payments made and planned. Existing message, shared with ReturnInputs,
  // gaining a planned flag.
  repeated EstimatedPayment payments = 3;

  // Withholding by date, for the election to count it as withheld.
  // Without the election it counts evenly through the year.
  repeated Withholding withholding = 4;
  bool withholding_as_withheld = 5;

  // A waiver claimed. Existing message, gaining its cause and dates.
  EstimatedPenaltyWaiver waiver = 6;

  // How this year's tax is known. Unset is refused.
  oneof this_year {
    FromReturn from_return = 10;
    CallerEstimate caller_estimate = 11;
    LastYearOnly last_year_only = 12;
  }

  // One jurisdiction's figures from last year's return.
  message PriorYear {
    string jurisdiction = 1;   // "CA"
    bool filed = 2;
    bool full_year = 3;        // a 12-month tax year
    bool resident = 4;
    Money tax = 5;
    Money agi = 6;             // for income tests, such as the 110% step
  }

  // Tax withheld on one date, for one jurisdiction.
  message Withholding {
    string jurisdiction = 1;
    Date date = 2;
    Money amount = 3;
  }

  // Cumulative income from the start of the year to a period's end, for
  // the annualised-income method.
  message Period {
    Date end = 1;
    Money income = 2;          // cumulative
  }

  // Compute this year's tax from the rest of ReturnInputs.
  message FromReturn {
    // For the annualised-income method. Empty means not annualised.
    repeated Period periods = 1;
  }

  // This year's figures as the caller estimates them, per jurisdiction.
  // The return is not computed.
  message CallerEstimate {
    repeated Jurisdiction jurisdictions = 1;

    message Jurisdiction {
      string jurisdiction = 1;
      optional Money tax = 2;  // this year's expected tax
      optional Money agi = 3;  // for income tests, such as California's $1M
      // For the annualised-income method: tax on cumulative income to each
      // period end. Empty means not annualised.
      repeated PeriodTax periods = 4;
    }

    message PeriodTax {
      Date end = 1;
      Money income = 2;        // cumulative
      Money tax = 3;           // on that income
    }
  }

  // Use only last year's safe harbour. No this-year option is offered.
  message LastYearOnly {}
}
```

**How this year's tax is known** is a oneof, because each choice needs
different data:

| | This year's figures | The rest of `ReturnInputs` | Annualised periods |
|---|---|---|---|
| `from_return` | the engine works them out from the return | must be complete enough for the return to be computed | income per period; the engine works out the tax |
| `caller_estimate` | the caller gives tax and AGI per jurisdiction | may be empty; no return is computed | income and tax per period |
| `last_year_only` | none | ignored for estimated tax | not possible |

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

**Payments, waivers and last-year figures reuse what `ReturnInputs` has.**
`EstimatedPayment` and `EstimatedPenaltyWaiver` already exist for the annual
return. Only what is missing is added: a payment's planned flag, the waiver's
cause and dates, and other jurisdictions' last-year figures.

Inputs only one state reads go in that state's existing `StateInputs`
message, as the return's state inputs do. Examples are Pennsylvania's Tax
Forgiveness or a state-only waiver. `tools/stategen` keeps that list.

### Output

```proto
message ReturnOutputs {
  // ... existing: the federal and state returns ...
  // One answer per jurisdiction asked for, federal first.
  repeated EstimatedAnswer estimated = N;
}
```

`EstimatedAnswer` is the message `us/v1/estimated.proto` already defines and
`scenarios/v1` already carries. It moves into `returns/v1` and gains a
`jurisdiction` field:

```proto
message EstimatedAnswer {
  string jurisdiction = 12;
  EstimatedOutcome outcome = 1;   // computed, no rules, unsupported, refused
  string reason = 2;
  EstimatedRequirement requirement = 3;
  repeated DuePayment to_meet = 4;             // what to pay, and when, to stay clear
  repeated EstimatedInstallment installments = 5;
  EstimatedPenalty penalty = 6;                // as of, with planned payments
  EstimatedPenalty penalty_without_planned = 7;
  EstimatedExplanation explanation = 8;        // plain words, with sources
  repeated EstimatedQuestion questions = 9;    // what the rules still need
  repeated EstimatedDivergence divergences = 10;
  bool this_year_tax_known = 11;
}

message EstimatedRequirement {
  bool required = 1;
  string why = 2;
  bool exempt = 3;
  bool target_known = 4;
  int64 target_cents = 5;                       // the lowest available option
  repeated EstimatedTargetOption options = 6;  // every option, not only the winner
}

message EstimatedTargetOption {
  TargetBasis basis = 1;           // this year's tax, last year's tax, last year's income, an exception
  int32 percent_basis_points = 2;  // 9000 = 90%, 11000 = 110%
  int64 amount_cents = 3;
  bool available = 4;
  string why_not = 5;
  bool chosen = 6;
  string label = 7;                // "110% of your 2025 tax"
}

message EstimatedInstallment {
  int32 number = 1;
  CalendarDate due = 2;        // moved off weekends and holidays
  int64 needed_by_cents = 3;   // cumulative
  int64 credited_by_cents = 4;
  int64 shortfall_cents = 5;
  InstallmentStatus status = 6;
}
```

`EstimatedPenalty` lists each instalment's shortfall and the charges on it:
principal, dates, days, rate, amount and the source of the rate.

**Every option is listed.** `requirement.options` holds each basis the
jurisdiction has, with its amount, whether it is available, and why not. The
target is the lowest available one, marked `chosen`. Two examples of an
option shown but not available:
- **`last_year_only`:** the this-year option comes back with "this year's tax
  wasn't estimated".
- **California at $1,000,000 of AGI:** the last-year option comes back with
  the rule that closed it.

**When an answer can't be given,** the jurisdiction's answer says so instead
of guessing:
- **No basis available:** if `last_year_only` is used and the last-year route
  is closed, `target_known` is false. A `questions` entry asks for this year's
  estimate, says which rule closed the last-year route, and what answering
  would change. Other jurisdictions answer normally.
- **A year with no rules:** the outcome is `NO_RULES`, never the nearest
  year's rules.
- **Facts the rules don't cover, such as farmers:** the outcome is
  `UNSUPPORTED`.

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
