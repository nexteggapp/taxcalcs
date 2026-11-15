# `returns/v1`: Foundation Tax Return Preparation Contract

`nexteggapp.taxcalcs.returns.v1` (`//returns/v1`) defines the language-neutral protobuf contract for preparing US federal and state individual tax returns from complete or partial taxpayer information.

## Architectural Role

`taxcalcs` separates tax calculation into two layers:

1. **Foundation Layer (`returns/v1`)**: Statelessly evaluates tax law over `ReturnInputs` (facts, received information returns, prior-year carryforwards, elections, and filing instructions) to produce `ReturnOutputs` (populated `taxprotos` return forms, IRS instruction worksheets, resolved election comparisons, diagnostics, and cell-level working traces).
2. **Scenario & Projection Layer (`scenarios/v1` / `taxyear`)**: Projects full-year figures from mid-year paystubs, run-rates, and what-if branches, and feeds the resulting `ReturnInputs` into the foundation layer.

## Core Design Principles

1. **Facts In, Computed Lines Out**:
   - `ReturnInputs` stores only factual records, received tax documents (wrapping `taxprotos` W-2, 1099, K-1, 1098, 1095-A, and 5498 messages), and explicit taxpayer elections.
   - Derived determinations (such as whether a child is a qualifying child under 26 U.S.C. § 152(c), whether Head of Household is allowed, or how much of a Social Security benefit is taxable) are computed by the engine and emitted in `ReturnOutputs`.
2. **Sensitive Identity Isolation (`identity.proto`)**:
   - Every sensitive identifier (`TaxpayerID`, `IPPIN`, `Address`, `BankAccount`, `SignaturePIN`) lives in `identity.proto` and wraps a `oneof` between the cleartext value (needed only at e-file transmission time) and a `Withheld` placeholder.
   - All tax calculations run identically when sensitive fields are `Withheld`.
3. **Specific Jurisdiction Return Envelopes**:
   - `ReturnOutputs` contains `FederalReturn federal` and `repeated StateReturn states` (`oneof return { CaliforniaReturn ca; OregonReturn or; WashingtonReturn wa; UnsupportedStateReturn unsupported; }`).
   - Every jurisdiction return message (`FederalReturn`, `CaliforniaReturn`, `OregonReturn`, `WashingtonReturn`, `UnsupportedStateReturn`) shares fields `1..5`:
     1. `ReturnStatus status = 1;`
     2. `Summary summary = 2;`
     3. `repeated Diagnostic diagnostics = 3;`
     4. `repeated ResolvedElection elections = 4;`
     5. `AmendmentResult amendment = 5;`
4. **First-Class Workpapers (`workpapers.proto`)**:
   - Publication and instruction worksheets that do not have their own MeF XML form (such as `QDCGTWorksheet`, `SSBenefitsWorksheet`, `CapitalLossCarryoverWorksheet`, `StandardDeductionWorksheet`, `IRADeductionWorksheet`, `StudentLoanInterestWorksheet`, `StateTaxRefundWorksheet`, and `SimplifiedMethodWorksheet`) are typed messages inside `FederalWorkpapers` on `FederalReturn`.
5. **Amended & Superseded Returns (`amendment.proto`)**:
   - `ReturnInputs.amendment` supplies the original/adjusted baseline return (`FederalReturn`, `Form540`, `FormOR40`, or `OriginalFigures`), prior IRS adjustments, and explanation of changes so the engine can produce Column A / Column B / Column C reconciliations in `AmendmentResult` alongside the corrected return.

## State Returns

State rules are taxlisp too, in [`returns/rules/states`](../returns/rules/states): the shared `00_states.tlisp`, a directory per state (`ca/`, `or/`, ...) with that state's published figures and its resident form, and `notax/` for the states with no income tax. They are loaded after the federal rules into one module, because a state return starts from the prepared federal return: federal AGI, the federal filing status, the federal Schedule A and the claimed dependents.

`prepare-return` prepares each return requested in `ReturnInputs.states`, in order, into `ReturnOutputs.states`, and adds the state figures to the household summary. A state with no rules yet comes back as `UnsupportedStateReturn` with a diagnostic.

- **California** (Form 540 and Schedule CA, full-year resident): Schedule CA subtractions (taxable social security) and additions, the standard or itemised deduction (with the Schedule CA Part II adjustments and the § 17077 limitation), the Schedule X/Y/Z tax, the exemption credits and their phase-out, the Behavioral Health Services Tax, withholding, payments, use tax, and the refund or amount owed.
- **Oregon** (Form OR-40, full-year resident): the federal tax subtraction, the social security subtraction, the standard deduction with its additions for age and blindness or the Schedule OR-A itemised deductions, the tax rate charts (each bracket rounded to a whole dollar), the exemption credit, withholding, payments, and the refund or tax due.

Credits and taxes not yet computed (California's renter's credit and shared responsibility penalty, Oregon's kicker, political contribution and 529 credits, a dependent filer's limited deduction) are reported as warning diagnostics and leave the return `RETURN_STATUS_INCOMPLETE`. A tax year with no figures is an error diagnostic, never the nearest year's figures.

`returns/rules/states:states_test` holds both states to `californiataxcalc` and `oregontaxcalc`, to the cent, for every filing status across the brackets.

Every state and the District of Columbia now has rules. The documentation site describes each one's form line by line, with what it computes and what it leaves out.

### Adding a state

A state is listed once, in [`tools/stategen/registry.go`](../tools/stategen/registry.go): its postal code, name, form, the number of its field in the `StateInputs` and `StateReturn` oneofs, the alias its taxprotos package goes by, and the form's directory in taxprotos. Every other list of states is written from that entry, between `BEGIN GENERATED` and `END GENERATED` markers:

- the `use-proto` lines and the dispatch in `00_states.tlisp`, and the summary case in `09_payments_and_1040.tlisp`;
- the `StateInputs` and `StateReturn` oneof fields;
- the Go `go_proto_library` targets and the deps that use them;
- the Rust proto deps, re-exports and rule-file list;
- the states the documentation site documents.

To add one:

1. Add the state's schema to taxprotos, and pin the new taxprotos commit in `MODULE.bazel`.
2. Write its rules in `returns/rules/states/<postal>/`, its test in `returns/rules/states/<postal>_test.go`, and its `<Name>Inputs` and `<Name>Return` messages in `returns/v1`.
3. Add its entry to the registry, and run `go run ./tools/stategen` from the repository root.
4. Regenerate the checked-in generated files (`bazel run //rust/proto:write_descriptors`, `bazel run //scenarios/v1:write_inputs_diff`, `bazel run //golden:golden_test -- -update`) and run `bazel test //...`.

The registry refuses two states with the same postal code, oneof field, alias or Go target, and a field that `NoIncomeTaxReturn` or `UnsupportedStateReturn` holds. A form whose directory name another state's form already has, as Idaho's and Alabama's `form_40` do, needs its own `GoTarget`. `go test ./tools/stategen` fails when a generated section is out of date.
