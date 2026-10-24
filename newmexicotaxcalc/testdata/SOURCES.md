# Where these files came from

Every New Mexico figure in this package is checked against a document the
Taxation and Revenue Department or the Legislature published. These files are
text extracted from those documents. This records which document, and how, so
that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs were extracted with `pdftotext`
from poppler 24.02.0; HTML pages were reduced to text with the Python 3.12.3
script in `minnesotataxcalc/testdata/SOURCES.md`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `rpd-41272-2025.txt` | [RPD-41272 (2025), Calculation of Estimated Personal Income Tax Underpayment Penalty (Rev. 12/22/2025 and 09/08/2025)](https://realfile.tax.newmexico.gov/2025rpd-41272.pdf), whole form, page layout | `c8e0d32d6070c69d734e2315fb5147df4464a2acb0f98a2f6d86f567789c2617` |
| `rpd-41272-2025-instructions.txt` | the same PDF, pages 5 to 7, reading order | same |
| `pit-es-2025-instructions.txt` | [Instructions for 2025 PIT-ES](https://realfile.tax.newmexico.gov/2025pit-es-ins.pdf), reading order | `a090aa35e3743ff1594b9ccb498ef090476f6328f64982ea58067ce1ccd8bacf` |
| `nmsa-7-2-12.2-hb470-2011.txt` | [Laws 2011, HB 470, enrolled](https://www.nmlegis.gov/Sessions/11%20Regular/final/HB0470.pdf), Section 1, § 7-2-12.2 as amended | `6a8d87b708bfbce03470b12b4d3e3fa9f0eae58de64964d5cb609682b70a4072` |
| `nm-table-of-changes-2026-chapter-7.txt` | [Table of Changes, 2026 Second Session](https://www.nmlegis.gov/Publications/Session/26/TOC.pdf), the heading and every Chapter 7 citation | `fb4aa59bb202780df272cb299691e4529d1c64f97485147414b2a92e59b18849` |
| `trd-penalty-interest-rates.txt` | [Penalty Interest Rates](https://www.tax.newmexico.gov/individuals/file-your-taxes-overview/penalty-interest-rates/), Table of Effective Interest Rates to 2025 | `098874c0d4f08e727a184fb5ceef401cedf110cffe48a8dcd01ab2110a596ff4` (the HTML) |
| `trd-estimated-payments.txt` | [Estimated Payments](https://www.tax.newmexico.gov/individuals/estimated-payments/) | `362f0fdc40a18deaf5cb3ab46b3ea58a77057ad4fbb5d80eb581abdc120d1b44` (the HTML) |

An HTML page's hash records what was fetched. A later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
curl -o 41272.pdf  https://realfile.tax.newmexico.gov/2025rpd-41272.pdf
curl -o es.pdf     https://realfile.tax.newmexico.gov/2025pit-es-ins.pdf
curl -o hb470.pdf  "https://www.nmlegis.gov/Sessions/11%20Regular/final/HB0470.pdf"
curl -o toc26.pdf  https://www.nmlegis.gov/Publications/Session/26/TOC.pdf
curl -o rates.html https://www.tax.newmexico.gov/individuals/file-your-taxes-overview/penalty-interest-rates/
curl -o web.html   https://www.tax.newmexico.gov/individuals/estimated-payments/

pdftotext -layout 41272.pdf rpd-41272-2025.txt
pdftotext -f 5 -l 7 41272.pdf rpd-41272-2025-instructions.txt
pdftotext es.pdf pit-es-2025-instructions.txt
pdftotext hb470.pdf hb470.txt
awk '/^"7-2-12.2\.$/{f=1} f; /^first-year residents\."$/{if(f)exit}' hb470.txt > nmsa-7-2-12.2-hb470-2011.txt
pdftotext toc26.pdf toc26.txt
{ sed -n 1,5p toc26.txt; grep "^7-" toc26.txt; } > nm-table-of-changes-2026-chapter-7.txt
python3 html2text.py rates.html \
  | awk '/^Table of Effective Interest Rates$/{f=1} /^January 1, 2024 – December 31, 2024 \|$/{if(f)exit} f' > trd-penalty-interest-rates.txt
python3 html2text.py web.html \
  | awk '/^Every individual who must file a New Mexico personal income tax return must pay estimated tax/{f=1} f; /^The PIT estimated payment vouchers and instructions are located below\./{if(f)exit}' \
  > trd-estimated-payments.txt
```

## New Mexico has no worked example

RPD-41272 prints its line arithmetic and no example with an answer. The tests
follow the lines: each part of each shortfall, by the day ÷ 365, at each
quarter's rate.

## What is not here

- **The 2026 RPD-41272 and PIT-ES instructions.** Not published:
  `2026rpd-41272.pdf` and `2026pit-es-ins.pdf` return a 404 page (sha256
  `96c622fac8fce43275db62082d21c99714804af089e93df0ad20a50dafb7fce1` and
  `d221c9a0880df9763c2bc6443f8bc51be5b47a9ad7e63e0668efb7e0a59330ae`). The
  department's page says the vouchers and instructions "are not tax year
  specific". The 2025 form is used with the 2026 dates and rates.
- **A current compilation of NMSA 1978 § 7-2-12.2 and § 7-1-67.** The
  statute is the 2011 enrolled bill. The 2026 Table of Changes lists Chapter
  7 sections and neither of these; `research/estimated-payments/nm.md` also
  checked the 2024 and 2025 tables.
- **The rate from January 1, 2027.** Not published. Until it is, the April
  15, 2027 stop changes no figure, and the penalty reports the gap.
- **A New Mexico holiday list.** Every 2026 due date is a weekday, so only
  weekends are applied. Whether January 31, 2027, a Sunday, moves is left to
  the caller.
