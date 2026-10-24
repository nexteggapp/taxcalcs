# Where these files came from

Every North Carolina figure in this package is checked against a document the
Department of Revenue or the General Assembly published. These files are text
extracted from those documents. This records which document, and how, so that
anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs were extracted with `pdftotext`
from poppler 24.02.0; HTML pages were reduced to text with the Python 3.12.3
script in `minnesotataxcalc/testdata/SOURCES.md`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `d-422-2025.txt` | [D-422 2025, Underpayment of Estimated Tax by Individuals (Web 12-25)](https://www.ncdor.gov/2025-form-d-422-underpayment-estimated-tax-individuals/open), page layout | `dd00296ff0af12b287d90b9e185ee1d01eea0e2e27b4c9291d8c368caf8fa4ba` |
| `d-422-2025-instructions.txt` | the same PDF, reading order, for the two-column instructions | same |
| `nc-40-2026.txt` | [Form NC-40, Individual Estimated Income Tax (Web 7-25), for tax years beginning on or after January 1, 2026](https://www.ncdor.gov/individual-estimated-income-tax/open), reading order | `417e1f3fc061299939cdbe4222adf609c729f2cd806b8e7a3a9f1c947dd55b4c` |
| `gs-105-163.15.txt` | [N.C. Gen. Stat. § 105-163.15](https://www.ncleg.gov/EnactedLegislation/Statutes/PDF/BySection/Chapter_105/GS_105-163.15.pdf), page layout | `50a7cfe526032a9fa80938172222900ab6ff313c6bfc61ab1ebb955d21703561` |
| `gs-105-241.21.txt` | [N.C. Gen. Stat. § 105-241.21](https://www.ncleg.gov/EnactedLegislation/Statutes/PDF/BySection/Chapter_105/GS_105-241.21.pdf), page layout | `8e49e108d33f83ae14ccab47483744e37e883c76412de0811629bb93afe1b438` |
| `gs-105-237.txt` | [N.C. Gen. Stat. § 105-237](https://www.ncleg.gov/EnactedLegislation/Statutes/PDF/BySection/Chapter_105/GS_105-237.pdf), page layout | `e6eb536645318404676cf03c4e9680aebdf4359978c5559b685ae72699663827` |
| `ncdor-interest-rate.txt` | [NCDOR, Interest Rate](https://www.ncdor.gov/interest), to the 2024 row | `26a67e4f5fc49a535cb0d06a9bc5e3b5c971bff8a599153e5f2fae2a10bba388` (the HTML) |
| `ncdor-estimated-income-tax.txt` | [NCDOR, Estimated Income Tax](https://www.ncdor.gov/taxes/individual-income-tax/estimated-income-tax) | `1976cacea74bf560519ce5588ddf1cfc294ce96fa5ddae98a5e1c5371d647624` (the HTML) |

An HTML page's hash records what was fetched. A later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
curl -o d422.pdf https://www.ncdor.gov/2025-form-d-422-underpayment-estimated-tax-individuals/open
curl -o nc40.pdf https://www.ncdor.gov/individual-estimated-income-tax/open
B=https://www.ncleg.gov/EnactedLegislation/Statutes/PDF/BySection/Chapter_105
curl -o gs15.pdf  $B/GS_105-163.15.pdf
curl -o gs21.pdf  $B/GS_105-241.21.pdf
curl -o gs237.pdf $B/GS_105-237.pdf
curl -o int.html https://www.ncdor.gov/interest
curl -o web.html https://www.ncdor.gov/taxes/individual-income-tax/estimated-income-tax

pdftotext -layout d422.pdf d-422-2025.txt
pdftotext d422.pdf d-422-2025-instructions.txt
pdftotext nc40.pdf nc-40-2026.txt
pdftotext -layout gs15.pdf gs-105-163.15.txt
pdftotext -layout gs21.pdf gs-105-241.21.txt
pdftotext -layout gs237.pdf gs-105-237.txt
python3 html2text.py int.html \
  | awk '/^Interest Rate$/{f=1} f; /^\| January 1, 2024 through December 31, 2024 \|$/{if(f)exit}' > ncdor-interest-rate.txt
python3 html2text.py web.html \
  | awk '/^You are required to pay estimated income tax if the tax shown due on your return/{f=1} f; /^For most calendar year filers, estimated payments are due/{if(f)exit}' \
  > ncdor-estimated-income-tax.txt
```

## North Carolina has no worked example

D-422 prints its line arithmetic and no example with an answer. The tests
follow the lines: each part of each shortfall, by the day ÷ 365, at the rate,
from its due date until paid or April 15.

## What is not here

- **The 2026 D-422.** Not published: the 2026 page returns a 404 (sha256
  `320577517953dab8e3200d1a7cd3cddf4f27260c1b84c55d19c3580a661666af`). The
  2025 form is used with the 2026 dates and rate.
- **26 U.S.C. § 6654(e).** § 105-163.15(f) takes the threshold from it; the
  $1,000 is as Form D-422 and the department's page print it.
- **The rate from January 1, 2027.** The Secretary sets it by December 1.
  Until then, the April 15, 2027 stop and the January 15 rule change no
  figure for the 2027 days, and the interest reports the gap.
- **17 NCAC 06B.** reports.oah.state.nc.us refused the connection per
  `research/estimated-payments/nc.md`; the rules were not checked.
- **A North Carolina holiday list.** Every 2026 due date is a weekday, so only
  weekends are applied.
