# Models

A **model** is one source of income, deduction or payment in a tax year, such
as a job, a bank account or a mortgage. It holds the source's own details and
a list of **parts**. The [`taxyear`](../taxyear) package expands every model
into **items**: the dated amounts the year is computed from, such as one
paycheck or one month's interest. Models sit in a scenario's inputs
([scenarios.md](scenarios.md)), and the items they expand into are in its
result ([results.md](results.md)).

## The models

A **slot** is a date a model fills, such as a pay date. A model's **default
part** fills every slot nothing else fills. A model with slots still to come
must have one.

`TaxYearModel` in [`scenarios/v1/model.proto`](../scenarios/v1/model.proto)
holds one list per kind. Each model has an `id`, and its parts' ids start with
it.

| List | One per | Slots | Default part |
|---|---|---|---|
| `employers` | Job | Pay dates, from `first_pay_date` and `pay_frequency` | `same_as_latest`, or `each_paycheck` before the first paystub |
| `businesses` | Self-employment business | Months | `pace_so_far` |
| `accounts` | Account's interest and dividends | Months | `pace_so_far` |
| `brokers` | Broker's sales | None | None |
| `retirement` | Person: IRA, HSA and workplace plan contributions | None | None |
| `itemised` | Mortgage, property tax, charity or medical | Months (mortgage), bill due dates (property tax), none (others) | `amortised`, `like_last_year` |
| `other_income` | Payer, state or property: retirement withdrawals, Social Security, unemployment, state refunds, rental, alimony, gambling, a home sale | None | None |
| `tax_paid` | Jurisdiction, `US` or a state code | Estimated-payment due dates | `spread_the_rest` |

## Parts

Every part is one case of a `oneof`. The case's comment in the proto gives its
origin and its shape.

**Origin.** A **known** part holds evidence inline: each form beside its
`DocumentRef`, or each bank entry beside its `LedgerRef`. An end that has
happened, such as `ended`, is known too. An **assumed** part states what the
household expects, such as a raise from October. Known parts live only in the
shared inputs.

| Shape | What it does | Examples |
|---|---|---|
| Evidence | Fills slots up to the latest evidence date | `from_paystubs`, `from_deposits` |
| Whole year | States the year's figures | `from_w2`, `year_total` |
| Period | Fills slots from its start until the next period part | `pay_rate` |
| Additive | Adds items on its own dates | `one_off` (a bonus or vest) |
| One slot | Fills only the slot it names | `planned_payment` (tax paid) |
| Adjustment | Changes other models' items after every model has expanded | `max_out_workplace_plan` (retirement) |
| End | Stops the model | `ended`, `ends` |

## How a model projects a year

The file's `as_of` date divides the year. An item read from evidence is
**made**. Any other item is **expected**. A model's **latest evidence date**
is the last day its evidence covers, reported as `so_far_through`.

Each slot is filled by the first source that applies:

1. **A whole-year part**, for the lines it states. A `year_total` shares the
   year less the evidence equally over the slots after the evidence.
2. **Evidence**, up to the latest evidence date. A W-2 states the year's
   boxes, and the paystubs stay as items. A pay date with no paystub or
   deposit gets an expected paycheck copying the nearest regular paystub.
3. **An assumed period part**, from its start until the next one's. An unset
   or earlier start means the day after the latest evidence.
4. **The default part.**

A month counts as evidence once it has ended by `as_of`. The month holding
`as_of` gets the next source's amount less what is already in it, never below
zero.

Then the adjustment parts run. Each person's `max_out_workplace_plan` or
`workplace_plan_total` sets the plan line on their expected paychecks across
their jobs. Then each person's HSA contributions through payroll are held to
their limit: the excess comes off the HSA lines of the paychecks the model
works out, and what is left is other income (`hsa_over_in_income`). The
paychecks a `year_total` fills keep its figures. For each year figure the
split would have changed on them, the figure's working gives what it would
have been as `automatic`. Then each job computes the withholding left unset,
in date order.

A model with no slots adds each part's items on their dates. A `year_total`
there is the year's total with the evidence included, and adds the total less
the evidence.

**Evidence is never refused.** An assumed part that evidence contradicts
**stands aside**: it counts for nothing, and the result asks a question
saying why. An `ends` dated before the latest paystub stands aside, for
example.

## Withholding on expected paychecks

A withholding line left unset on an expected paycheck is computed. The item's
`estimated` field lists it.

- **Federal**: Publication 15-T's percentage method, from the W-4. Without
  one, the latest paystub's filing status, else single.
- **A bonus or vest**: by `supplemental`, either a flat 22% (37% over
  $1,000,000 in the year) or aggregated with the regular paycheck.
- **Social Security** 6.2% up to the wage base, and **Medicare** 1.45% plus
  0.9% over $200,000, per job.
- **State**, where the work is done: a typed line, then the model's
  `state_withholding`, then the latest paystub's rate. Failing those, the
  result asks. Each paycheck's state lines are the states its work is
  done in on its date. A `year_total`'s state line, less the evidence, is
  shared over the paychecks worked in that state. Its California wages
  are not used: California's wages are box 1 and the payroll HSA.

## Matching evidence: Match and Actual

Each model matches evidence to slots by its own rules. For a job, a regular
paystub fills the pay date nearest it, and deposits within 3 days of a
paystub pair with it. Bonus, vest and final paystubs are additive. When the
rules can't tell, the result asks, and the answer is stored in the file, in
the messages in [`actual.proto`](../scenarios/v1/actual.proto).

**`Match`**, on an evidence entry, says what the entry is:

| Case | Meaning |
|---|---|
| `slot` | It fills the slot on this date |
| `item` | It belongs to the item with this id, such as a bonus |
| `not_this_source` | It isn't this source's, and leaves the model |

**`Actual`**, on a planned additive part, answers "Is this the one you
planned?". For a job, it is asked when a bonus or vest paystub falls within
31 days of a planned one. Until it is answered, both count.

| Field | Meaning |
|---|---|
| `is` | The planned part is this evidence entry, which counts in its place |
| `is_not` | These entries are other payments, and both count |

```jsonc
{ "id": "acme-k3m9q2wd", "oneOff": {
    "date": { "year": 2026, "month": 12, "day": 15 }, "kind": "PAYCHECK_KIND_BONUS",
    "amount": { "minor": "1000000", "currency": "USD" },
    "actual": { "is": "acme-p4h7n2rt" } } }
```

## Rounding

Items are whole dollars. A **run** is one part's made items, or its expected
items. Each run keeps a running total per line, rounded half away from zero,
and each item shows the change in that rounded total. Three $5,833.33
paychecks show $5,833, $5,833 and $5,834. A part's line then adds up to its
exact sum, rounded. [results.md](results.md#rounding) says how totals add up.
