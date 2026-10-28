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
