# Glossary

The terms these pages use, as taxcalcs uses them. A term in brackets, such as
[figure], is defined here; a protobuf type in backticks, such as
`ReturnInputs`, has a page of its own. The first paragraph of each entry is
its short definition, shown when a reader points at the term.

## anchor

One place in a [source]: a form line, a chart, a worksheet or a section of
law, with the page and the region of the page it is printed in. [Rules] and
[figures] point at anchors, never at a whole document.

An anchor says what became of it: a rule implements it, it is [not modeled]
(with the reason), or it is informational only.

See also: [source catalog], `Anchor`.

## calculation

A [rule] that computes one value, declared with `def`: the tax from a rate
schedule, a count of exemptions, a credit. A [form]'s line usually calls one.

See also: [test], [worksheet].

## citation

What a [rule] says it implements, written after `:cite`: a section of law,
`(law "Cal. Rev. & Tax. Code" "17054")`, which links to its page in the
[source catalog], or the name of a document not yet catalogued.

## derivation

The record of how a value was computed: each [calculation] called, with its
arguments and result, each [line] of each [form] and [worksheet], and each
condition of each [test], down to the [figures]. The evaluator records it
while the [rules] run, as a [trace], so it is the computation itself, not a
description of it.

See also: [working].

## diagnostic

A note on a [return], with a code, a severity, a message, the input fields it
concerns and the law it rests on. A credit or tax that is [not modeled] is a
warning, and leaves the return incomplete.

See also: `Diagnostic`, [return status].

## election

A choice the law leaves to the filer, such as itemizing deductions or filing
jointly. When the filer has not made it, taxcalcs works out each eligible
option, uses the one with the lower tax, and reports that it chose, with each
option's result.

See also: `Elections`, `ResolvedElection`.

## fact

Something true of a household that taxcalcs is told, such as a date of birth
or whether someone else can claim the filer as a dependent. Facts are the
only inputs: determinations, such as whether a child is a qualifying child,
are made by the [rules].

See also: [return inputs], `Household`.

## field path

A dotted path to a field of a [protocol buffer] message, such as
`federal.household.taxpayer.date_of_birth`. [Rules] read fields by path, with
`(. inputs 'federal.household)`, and `DescribeInputs` names the fields it
asks for by path.

## figure

A number a [source] publishes for one [tax year]: a rate, a threshold, a
standard deduction, a credit amount. Each figure names the [anchor] that
prints it.

See also: [figure set].

## figure set

One tax year's [figures] for a [jurisdiction], declared with `deffigures`.
[Rules] take the figure set as an argument and never contain a published
number, so a new year is a new figure set with the same rules.

## form

A [rule] that computes a whole tax form and returns it filled in, declared
with `defform`. It is written in the form's printed order, grouped into its
parts, as numbered [lines]; it attaches the schedules and [worksheets] it
uses.

See also: [line], `Form540`.

## information return

A document a household receives from an employer, bank or broker, such as a
W-2, 1099, 1098, K-1, 1095-A or 5498, given to taxcalcs as received, in the
shape of its [taxprotos] schema.

See also: `Documents`, `W2Document`.

## jurisdiction

A government that taxes income: the United States, a state, or the District
of Columbia. Each has its own [return], [rules] and [figure sets].

## line

One numbered, captioned line of a [form] or [worksheet], with the expression
that computes it. Later lines use earlier ones by name: `line13`.

## money

An exact amount. In [taxlisp] it is an exact rational number of dollars,
written `#usd"5706.00"`; in a [protocol buffer] it is a whole number of
cents, `minor`, with its currency. It is rounded only where a form or the law
says to round.

See also: `Money`.

## not modeled

Said of an [anchor] or a [line] that taxcalcs does not compute yet. Each says
why, and a [return] that needs one is incomplete, with a [diagnostic], rather
than wrong.

## protocol buffer

A typed message whose schema is a `.proto` file, compiled for each
programming language. Every input and output of taxcalcs is one, so Go, Rust
and an app in any language share one contract.

The [return inputs] and [return outputs] are defined in `returns/v1`, the tax
forms in [taxprotos]. [taxlisp] reads and builds messages directly, typed by
their schemas, and a taxlisp program is itself a protocol buffer.

See also: [field path], [schema].

## pure function

A function whose result depends only on its arguments, and which changes
nothing. taxcalcs is one: it keeps no state between calls, so the same
[return inputs] always give the same [return outputs].

## question

An input that taxcalcs still needs before a [return] can be complete.
`DescribeInputs` lists them, in priority order, as [field paths] with the
question to ask.

See also: `InputFieldDescriptor`.

## return

One [jurisdiction]'s tax return: its forms filled in, the [worksheets]
behind them, a summary of tax, payments and the refund or amount owed, its
[diagnostics], its [elections], and a [return status].

See also: `FederalReturn`, `CaliforniaReturn`.

## return inputs

Everything taxcalcs is told about a household for one [tax year]: [facts],
[information returns], payments and [elections], for the federal return and
each state return to prepare.

See also: `ReturnInputs`.

## return outputs

What taxcalcs gives back: the federal [return] and one for each state, and a
summary of the household across them.

See also: `ReturnOutputs`.

## return status

Whether a [return] is complete, incomplete (something it needs is
[not modeled] or missing), not required, or invalid.

See also: `ReturnStatus`.

## rule

A declaration in [taxlisp] that carries out part of the law: a [form], a
[worksheet], a [test] or a [calculation]. Each cites the law it implements.

## schema

The definition of a [protocol buffer] message: its fields, their types and
numbers, and the comments that document them. The type pages on this site
are generated from the schemas.

## source

One published document for one [tax year]: a form, its instructions, or a
code of law as it stood that year. The 2026 instructions are a different
source from the 2025 ones. A PDF copy of each is kept in the repository and
checked against its sha256.

See also: [anchor], [source catalog].

## source catalog

The list of every [source], with its [anchors], kept as text-format protocol
buffers under `sources/catalog`.

See also: `Source`.

## tax year

The calendar year whose income a [return] reports. Every [figure] and
[source] belongs to one, and a year whose figures taxcalcs does not have is
an error, never borrowed from a nearby year.

## taxlisp

The language the [rules] are written in: a small Lisp, statically typed and
purely functional, with exact [money]. [Forms] and [worksheets] are declared
line by line, and every program is also a [protocol buffer] syntax tree.

## taxprotos

The open [schemas] of US tax forms and [information returns], as
[protocol buffers]: Form 1040, the W-2, California's Form 540, and others.
taxcalcs fills them in.

## test

A [rule] that answers a yes-or-no question the law asks, such as whether
someone else can claim the filer as a dependent. Its name ends in a question
mark, and its conditions are written with `all-of` and `any-of` so a
[derivation] shows each one.

## trace

What the evaluator records while [rules] run: a tree of the calls, [lines]
and checks, each with its value. A [derivation] is a trace as these pages
show it.

## working

The reasoning behind a figure, as a [return] reports it: the [derivation] of
each line, and the [elections] made.

See also: `Working`.

## worksheet

A [rule] declared with `defworksheet`, written as numbered or lettered
[lines] like a [form], that returns one value. It is for the worksheets
printed in the instructions and kept with the filer's records.
