# Where these files came from

Every Pennsylvania figure in this package is checked against a document the
Department of Revenue published. Those documents are PDFs; these files are text
extracted from them. This records which PDF, and how, so that anybody can redo
it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0, extracted with `pdftotext` from poppler
24.02.0. `B` is
`https://www.pa.gov/content/dam/copapwp-pagov/en/revenue/documents/formsandpublications/formsforindividuals/pit/documents`,
where revenue.pa.gov's forms now live.

| File | Source | sha256 of the PDF |
|---|---|---|
| `rev-413i-2026.txt` | [REV-413 (I), Instructions for Estimating PA Personal Income Tax, 2026](https://www.pa.gov/content/dam/copapwp-pagov/en/revenue/documents/formsandpublications/formsforindividuals/pit/documents/2026/2026_rev-413i.pdf) (`B/2026/2026_rev-413i.pdf`), whole document, reading order | `910d2bb8e039455c6b63c3d176a1fba951f2c16ad735a2e2c604f939cc6b492d` |
| `rev-413i-2026-due-dates.txt` | the same PDF, the Payment Due Dates Table, page layout | same |
| `rev-1630-2025.txt` | [REV-1630, Underpayment of Estimated Tax by Individuals, 2025](https://www.pa.gov/content/dam/copapwp-pagov/en/revenue/documents/formsandpublications/formsforindividuals/pit/documents/2025/2025_rev-1630.pdf) (`B/2025/2025_rev-1630.pdf`), form and instructions, reading order | `49438a97fc8026ff763bd9d234d2493f0a988b9f9be4ca34391c9187ee710112` |
| `rev-1630-2025-example.txt` | the same PDF, the Example of Interest Calculation, page layout | same |
| `rev-1611.txt` | [REV-1611, Interest Rate and Calculation Method for Title 72 Taxes (11-25)](https://www.pa.gov/content/dam/copapwp-pagov/en/revenue/documents/formsandpublications/otherforms/documents/rev-1611.pdf), page layout | `a6e82821237347a573e5bea7c2a67be7f89f2a51d8d55ec58732bfc53cc81966` |

## Reproducing them

```bash
B=https://www.pa.gov/content/dam/copapwp-pagov/en/revenue/documents/formsandpublications/formsforindividuals/pit/documents
curl -o rev-413i.pdf "$B/2026/2026_rev-413i.pdf"
curl -o rev-1630.pdf "$B/2025/2025_rev-1630.pdf"
curl -o rev-1611.pdf https://www.pa.gov/content/dam/copapwp-pagov/en/revenue/documents/formsandpublications/otherforms/documents/rev-1611.pdf

pdftotext rev-413i.pdf rev-413i-2026.txt
pdftotext -layout rev-413i.pdf rev-413i.L.txt
awk '/PAYMENT DUE DATES TABLE/{f=1} f; /After Aug\. 31, 2026/{if(f)exit}' rev-413i.L.txt \
  > rev-413i-2026-due-dates.txt

pdftotext rev-1630.pdf rev-1630-2025.txt
pdftotext -layout rev-1630.pdf rev-1630.L.txt
awk '/EXAMPLE OF INTEREST CALCULATION$/{f=1} f; /Total Lines 15a, 15b, and 15c/{if(f)exit}' rev-1630.L.txt \
  > rev-1630-2025-example.txt

pdftotext -layout rev-1611.pdf rev-1611.txt
```

Both instruction documents are set in two columns, so they are extracted in
reading order. The two tables lose their rows in reading order, so they are
extracted again with their layout, bounded by the document's own headings.

## REV-1630's examples are the strong check

REV-1630 works two examples: when the penalty applies ($3,599 of tax, $1,344 of
withholding and credits, and the two tests at $3,239 and $3,502), and the
interest on four $474 underpayments. Inputs and answers both come from the
department. `TestEstimatedRequirementMatchesREV1630Example` and
`TestEstimatedInterestMatchesREV1630Example` reproduce them; the second uses
2026's June 15 due date, which gives 199 days rather than the 2025 example's
198, and the same $18.

## What is not here

- **The 2026 REV-1630.** Not published: `B/2026/2026_rev-1630.pdf` returns an
  HTML error page. The 2025 form's arithmetic is used with the 2026 figures
  from the REV-413 (I). The special exception's tax figure is the year's own
  threshold, $430, as the 2025 form uses 2025's $338; no published 2026 form
  confirms it.
- **The Tax Reform Code and the Pennsylvania Code.** palegis.us did not answer
  a connection on 2026-10-20, so 72 P.S. §§ 7325, 7326 and 7352 and 61 Pa.
  Code Chapter 115 are not checked in. The statute side of each divergence
  rests on `research/estimated-payments/pa.md`, whose verification read them.
- **The rate from January 1, 2027.** Not published. The penalty reports the
  gap in `Required`.
