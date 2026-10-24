# Where these files came from

Every Kentucky figure in this package is checked against one of these files.
Retrieved 2026-10-20 with curl 8.5.0; PDFs extracted with `pdftotext` from
poppler 24.02.0.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `krs-141.305.txt` | [KRS 141.305, Estimated income tax payments](https://apps.legislature.ky.gov/law/statutes/statute.aspx?id=49196), effective June 27, 2019, whole section, page layout | `63f92e4c4dde6484198c081bf4d5c5f18ea79b27c5635e8d5db89b44b3fefbc4` (a PDF) |
| `krs-131.183.txt` | [KRS 131.183, Tax interest rate](https://apps.legislature.ky.gov/law/statutes/statute.aspx?id=57909), effective July 15, 2026, whole section, page layout | `b6097a993966df89c7b05662d043c05b70ac5f2b0204469b20540d1dfb122194` (a PDF) |
| `103-kar-1-040-section-1.txt` | [103 KAR 1:040, Waiver of penalties](https://apps.legislature.ky.gov/law/kar/titles/103/001/040/), the engrossed PDF it links to, Section 1 only | `5e1b312d2141b637ff3ab58ddefb7afccad3ff6cf4aa1f89f218b91cbeb2389e` |
| `research-ky.md` | `research/estimated-payments/ky.md` at commit `31d590e18`, copied unchanged. **Not a primary source**: it stands in for the department's documents, which could not be fetched | `410d92b8ecd3fa8209434031e6292e6a53bc68ab18769eaf434bb6b09dd86871` |

## Reproducing them

```bash
curl -L -o krs141305.pdf "https://apps.legislature.ky.gov/law/statutes/statute.aspx?id=49196"
curl -L -o krs131183.pdf "https://apps.legislature.ky.gov/law/statutes/statute.aspx?id=57909"
curl -L -o kar1040.pdf "https://apps.legislature.ky.gov/law/kar/downloads/docs/779/document.engrossed.pdf"

pdftotext -layout krs141305.pdf krs-141.305.txt
pdftotext -layout krs131183.pdf krs-131.183.txt
pdftotext -layout kar1040.pdf kar.L.txt
awk '/^Section 1\. /{f=1} /^Section 2\. /{if(f)exit} f' kar.L.txt > 103-kar-1-040-section-1.txt

cp ../../research/estimated-payments/ky.md research-ky.md
```

The statute pages serve a PDF at the `statute.aspx` address. The regulation
is bounded by its own section headings.

## The department's documents were blocked

Every revenue.ky.gov address answered HTTP 403 with an HTML page headed "The
request is blocked." on 2026-10-20:

- `https://revenue.ky.gov/Forms/740-ES%20Instructions%20(2025).pdf`, the 2026
  voucher instructions, 42A740-S4 (9-25);
- `https://revenue.ky.gov/Forms/740es%202026.pdf`, the 2026 Form 740-ES,
  42A740ES (10/25);
- `https://revenue.ky.gov/Forms/2210-K%20(2025).pdf`, Form 2210-K 2025,
  42A740-S1 (10-25), with its instructions;
- `https://revenue.ky.gov/DOR%20Training%20Materials/Estimated%20Tax%20Payments%20TAM-19-02%20FINAL%204.3.19.pdf`,
  KY-TAM-19-02;
- `https://revenue.ky.gov/News/Pages/Tax-Interest-Rate-Update-for-01-01-26.aspx`,
  the 2026 tax interest rate.

Each figure from them (the 90%, 100% and 110% tests, the $150,000 and $75,000
steps, the due dates, the September 1 and February 1, 2027 dates, the 25%
installments and withholding, the annualised periods and percentages, the
April 15 stop, the $500 line 5 test, the 9% and 7% rates and the weekend
rule) is checked against the quote the research file's verification read in
the document itself, through `research-ky.md`. That checks this package
against the research, not against the department. Somebody with access
should fetch the five documents, check the quotes, and replace
`research-ky.md` with extracts.

## What is not here

- **A worked example.** The research names none in the 2025 Form 2210-K or the
  2026 voucher instructions, so the tests are hand-written on the form's
  arithmetic.
- **2026 Ky. Acts ch. 161.** It amended KRS 131.183 from July 15, 2026. It was
  not read, so what it changed is not known; the current section is checked
  in.
- **The rate from January 1, 2027.** Not published. The penalty reports the
  gap in `Required`.
- **A Kentucky holiday list.** None is checked in. The 2026 due dates are
  weekdays.
