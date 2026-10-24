# Where these files came from

Every Nebraska figure in this package is checked against a document the
Nebraska Department of Revenue or the Legislature published. These files are
text extracted from those documents. This records which document, and how, so
that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs were extracted with `pdftotext`
from poppler 24.02.0, in reading order; HTML pages were reduced to text with
the Python 3.12.3 script in `minnesotataxcalc/testdata/SOURCES.md`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `form-2210n-2026.txt` | [Form 2210N, Individual Underpayment of Estimated Tax, 2026 (8-237-2026)](https://revenue.nebraska.gov/sites/default/files/doc/tax-forms/2026/f_2210n.pdf), form and instructions | `da7798bc0816d7ec536dc7e83f74a351361fb85f568f7474de71b40438c30dcf` |
| `form-1040n-es-2026.txt` | [2026 Form 1040N-ES (8-014-2025 Rev. 11-2025)](https://revenue.nebraska.gov/sites/default/files/doc/tax-forms/2025/f_1040N-ES.pdf), filed in the department's 2025 folder | `456e8d651addfb9f3056f996d195e0ae428b87a1e84727c8038a208fb6d880d5` |
| `revenue-ruling-99-26-2.txt` | [Revenue Ruling 99-26-2, Interest Rate Assessed on State Taxes (August 7, 2026)](https://revenue.nebraska.gov/sites/default/files/doc/legal/rulings/rr992401_interest_rate.pdf); the file name still says 99-24-1 | `cc2c3014c666ec9e000d905052ad623386fc7e5ab86eecaafd886e3751f251c4` |
| `nrs-77-2769.txt` | [Neb. Rev. Stat. § 77-2769](https://nebraskalegislature.gov/laws/statutes.php?statute=77-2769) | `8eddb15f657d90c666398e1154967a587011afae30688c4c300c321e6326da7a` (the HTML) |
| `nrs-45-104.02.txt` | [Neb. Rev. Stat. § 45-104.02](https://nebraskalegislature.gov/laws/statutes.php?statute=45-104.02) | `43a2645e4705fc20e1a5f4d51d4e223aa6a4ecdac1fd08d662613a3c7be0c8c2` (the HTML) |
| `316-nac-20-001-007.txt` | [316 Neb. Admin. Code ch. 20](https://revenue.nebraska.gov/about/legal-information/regulations/chapter-20-estimated-income-tax), REG-20-001 and REG-20-007 | `f85242936c8b0107700047b06219cceacba893fbc482e9c08ed1af0c402c5a9d` (the HTML) |

An HTML page's hash records what was fetched. A later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
curl -o 2210n.pdf  https://revenue.nebraska.gov/sites/default/files/doc/tax-forms/2026/f_2210n.pdf
curl -o es26.pdf   https://revenue.nebraska.gov/sites/default/files/doc/tax-forms/2025/f_1040N-ES.pdf
curl -o rr.pdf     https://revenue.nebraska.gov/sites/default/files/doc/legal/rulings/rr992401_interest_rate.pdf
curl -o 2769.html   "https://nebraskalegislature.gov/laws/statutes.php?statute=77-2769"
curl -o 104.02.html "https://nebraskalegislature.gov/laws/statutes.php?statute=45-104.02"
curl -o reg20.html  https://revenue.nebraska.gov/about/legal-information/regulations/chapter-20-estimated-income-tax

pdftotext 2210n.pdf form-2210n-2026.txt
pdftotext es26.pdf  form-1040n-es-2026.txt
pdftotext rr.pdf    revenue-ruling-99-26-2.txt

for f in 2769 104.02 reg20; do python3 html2text.py $f.html > $f.txt; done
awk '/^77-2769\.$/{f=1} /^Source$/{if(f)exit} f' 2769.txt > nrs-77-2769.txt
awk '/^45-104\.02\.$/{f=1} /^Source$/{if(f)exit} f' 104.02.txt > nrs-45-104.02.txt
awk '/^REG-20-001 Payment of Estimated Tax: Individuals$/{f=1} /^REG-20-002 Payment of Estimated Tax: Corporations and Certain Businesses; Overpayment of Estimated Tax$/{f=0} /^REG-20-007 Penalty Underpayment of Estimated Tax - Individuals$/{f=1} /^REG-20-008 Penalty Underpayment of Estimated Tax - Corporations$/{f=0} f' reg20.txt > 316-nac-20-001-007.txt
```

## Nebraska has no worked example

Form 2210N prints its line arithmetic and no example with an answer. The
tests follow the lines: each column's underpayment, this one's and any left
from earlier, by the day ÷ 365 at 8%, from its due date until paid or the
next due date, and the last column to April 15.

## What is not here

- **Neb. Rev. Stat. § 77-2790.** Fetched and read; it names no rate. The
  regulation, REG-20-007.03, ties the penalty to § 45-104.02, and both are
  checked in.
- **A Nebraska holiday list.** Every 2026 due date is a weekday, so only
  weekends are applied, and the tests check the computed dates against the
  printed ones. Whether January 31, 2027, a Sunday, moves is left to the
  caller, as Form 2210N does not say.
