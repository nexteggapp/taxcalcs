# Where these files came from

Every Arkansas figure in this package is checked against a document the
Department of Finance and Administration (DFA) published. Those documents are
PDFs; these files are text extracted from them. This records which PDF, and
how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0, extracted with `pdftotext` from poppler
24.02.0. `U` is `https://www.dfa.arkansas.gov/wp-content/uploads`.

| File | Source | sha256 of the PDF |
|---|---|---|
| `ar1000es-2026.txt` | [AR1000ES, Estimated Tax Declaration Vouchers and Instructions for Tax Year 2026](https://www.dfa.arkansas.gov/wp-content/uploads/2026_Final_AR1000ES_2.pdf) (`U/2026_Final_AR1000ES_2.pdf`), instructions rev. 10/14/2025 and worksheet rev. 5/6/2026, from the start to the tax rate schedule, reading order | `724b350ba495c54a21299ee57aa68c1038a0c0809011940a04a38eb4896002db` |
| `ar1000es-2026-vouchers.txt` | the same PDF, the four vouchers, reading order | same |
| `ar2210-2025.txt` | [AR2210, Penalty for Underpayment of Estimated Tax, 2025](https://www.dfa.arkansas.gov/wp-content/uploads/2025_AR2210_Penalty_for_Underpayment_of_EstimatedTax_1.pdf) (rev. 6/6/2025), whole form, page layout | `0e1a6e610221e056461ad93845cac28799f9a9a99e2f862fd7aa438cee875938` |
| `ar2210i-2025.txt` | [Instructions for AR2210, 2025](https://www.dfa.arkansas.gov/wp-content/uploads/2025_AR2210_Instructions.pdf) (page 1 rev. 7/5/2024, page 2 rev. 6/27/2025), whole document, reading order | `aff181df2c3258d774f3ab6ba492c0b4e1feea0f30a44c555f2166b51907875d` |
| `ar2210i-2025-example.txt` | the same PDF, the Penalty Computation Example, page layout | same |
| `ar2210a-2025.txt` | [AR2210A, Annualized Penalty for Underpayment of Estimated Income Tax, 2025](https://www.dfa.arkansas.gov/wp-content/uploads/2025_AR2210A_AnnualizedPenalty_for_Underpayment_of_EstimatedTax.pdf) (rev. 4/28/2025), whole form, page layout | `964dddfe459f449f2a60006a614fcccd798b4f1cffb170bae80bb68184b56093` |
| `subject-603.txt` | [Subject 603, Penalty for Underpayment of Estimated Tax](https://dfa.arkansas.gov/wp-content/uploads/603-UEP.pdf) (rev. 12/22/2020), whole document, reading order | `c8a6d89aeb5b6ac04be1846ef239c5f2637543dbbd836198bb3d13a0a08c15e4` |

## Reproducing them

```bash
U=https://www.dfa.arkansas.gov/wp-content/uploads
curl -sS -L -o ar1000es-2026.pdf "$U/2026_Final_AR1000ES_2.pdf"
curl -sS -L -o ar2210-2025.pdf   "$U/2025_AR2210_Penalty_for_Underpayment_of_EstimatedTax_1.pdf"
curl -sS -L -o ar2210i-2025.pdf  "$U/2025_AR2210_Instructions.pdf"
curl -sS -L -o ar2210a-2025.pdf  "$U/2025_AR2210A_AnnualizedPenalty_for_Underpayment_of_EstimatedTax.pdf"
curl -sS -L -o 603-uep.pdf       https://dfa.arkansas.gov/wp-content/uploads/603-UEP.pdf

pdftotext ar1000es-2026.pdf ar1000es.R.txt
awk 'NR==1,/^TAX RATE SCHEDULE$/' ar1000es.R.txt > ar1000es-2026.txt
awk '/^Click Here to Print Vouchers$/{f=1} f' ar1000es.R.txt > ar1000es-2026-vouchers.txt

pdftotext -layout ar2210-2025.pdf ar2210-2025.txt

pdftotext ar2210i-2025.pdf ar2210i-2025.txt
pdftotext -layout ar2210i-2025.pdf ar2210i.L.txt
awk '/PENALTY COMPUTATION EXAMPLE/{f=1} f; /Total Penalty/{if(f)exit}' ar2210i.L.txt > ar2210i-2025-example.txt

pdftotext -layout ar2210a-2025.pdf ar2210a-2025.txt
pdftotext 603-uep.pdf subject-603.txt
```

The AR1000ES extracts leave out the tax rate schedule, which the estimated tax
rules do not use. The AR2210 instructions are set in two columns, so they are
extracted in reading order; the worked example loses its columns in reading
order, so it is extracted again with its layout, bounded by its own heading
and total.

## Page 1 of the AR2210 instructions is partly garbled

Some passages on page 1 use an embedded font whose characters extract 29 code
points too low: a space comes out as the control character `\x03`, `I` as `,`,
and the "fi" ligature as `¿`. The text is left as `pdftotext` writes it, so the
extract still matches its PDF. The tests find a garbled passage by writing the
plain phrase the same way (`garble` in `estimated_test.go`). Two digits are
lost outright: Exception 2's "January 31" extracts with no "31", and Subject
603 supplies it ("by January 31st").

## The AR2210 example is the strong check

Page 2 of the instructions works a penalty from start to finish: $5,000 of
tax, $2,000 of payments, a $3,000 prior-year tax, and $4, $13, $32 and $18 by
quarter, $67 in all. Inputs and answers both come from the department.
`TestEstimatedPenaltyMatchesTheAR2210Example` reproduces it on the 2026 dates,
which give the same day counts. It is also the only evidence of how the form
rounds: each charge to the whole dollar.

## What is not here

- **The Arkansas Code.** The estimated tax sections of Ark. Code Ann. title 26,
  chapter 51, and the penalty section § 26-18-208 that the instructions cite,
  are not checked in. The official Code is hosted by a commercial publisher as
  a JavaScript application, and arkleg.state.ar.us links out to it with no
  text of its own (`research/estimated-payments/ar.md`, Verification). The
  statute side of each divergence is page 1 of the AR2210 instructions, which
  restates the law in its own words, and each divergence says so.
- **The 2026 AR2210 and AR2210A.** Not published by 2026-10-20. The 2025 forms
  are used with the 2026 dates.
- **An Arkansas holiday list.** None is checked in. Every 2026 due date the
  AR1000ES prints is a weekday, so only weekends are applied, and the tests
  check the computed dates against the printed ones.
