# Where these files came from

Every North Dakota figure in this package is checked against a document the
Office of State Tax Commissioner or the Legislative Assembly published. These
files are text extracted from those documents. This records which document,
and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs were extracted with `pdftotext`
from poppler 24.02.0; the HTML page was reduced to text with the Python 3.12.3
script in `minnesotataxcalc/testdata/SOURCES.md`. No site refused a download.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `nd-1es-2026.txt` | [Form ND-1ES, Estimated Income Tax - Individuals, SFN 28709 (12-2025), 2026](https://www.tax.nd.gov/sites/www/files/documents/forms/individual/2025-iit/28709-form-nd-1es-2026.pdf), reading order, to the vouchers | `8ae79e875b28c19ee124cbfe59a700f880e231cea0f5d6807b79958bf90cb864` |
| `nd-1ut-2025.txt` | [Schedule ND-1UT, Underpayment or Late Payment of Estimated Income Tax for Individuals, SFN 28704 (12-2025), 2025](https://www.tax.nd.gov/sites/www/files/documents/forms/individual/2025-iit/28704-schedule-nd-1ut-2025.pdf), reading order | `3d6950869900148794caa94e612b701320e7efd83923ca24e0ecf12deaac5529` |
| `booklet-2025-estimated-tax.txt` | [2025 Individual Income Tax Booklet](https://www.tax.nd.gov/sites/www/files/documents/forms/individual/2025-iit/2025-individual-income-tax-booklet.pdf), reading order, the "Estimated tax requirement (for 2026)" section | `cea20eda0416339cdd2b049c67c772ca62665eafe2f0384795204c4197b4d75b` |
| `ndcc-57-38.txt` | [N.D.C.C. chapter 57-38](https://ndlegis.gov/cencode/t57c38.pdf), page layout, §§ 57-38-45, 57-38-62 and 57-38-63 | `2429bef99ad1fccf9bc14f4e0d4cbb11948c3798fbadc3326677fd34c5e42979` |
| `ndcc-1-02-15.txt` | [N.D.C.C. chapter 1-02](https://ndlegis.gov/cencode/t01c02.pdf), page layout, § 1-02-15 | `b9559efb8a020fbe836c75884c8e3634d0cfd5e0bc8d4a0172c28e501a452121` |
| `ndcc-1-03.txt` | [N.D.C.C. chapter 1-03](https://ndlegis.gov/cencode/t01c03.pdf), page layout, §§ 1-03-01 to 1-03-05 | `5ab482543f06d9e6e20255f4b464e923a31a25c982fdbd3ea6b01c564556e2cc` |
| `ndac-81-03-04-02.txt` | [N.D. Admin. Code chapter 81-03-04](https://www.ndlegis.gov/information/acdata/pdf/81-03-04.pdf), page layout, § 81-03-04-02 | `0727657e020682ee397e7f14ee5fc4e3256d89791018e4c92b1a7aa884cd944c` |
| `nd-disaster-extensions.txt` | [Office of State Tax Commissioner, Disaster Information and Tax Implications](https://www.tax.nd.gov/news/resources/disaster-information-and-tax-implications), the federal extensions passage | `798858d40e636cd6d0d1f6a21c67429230c1e42acabc04355cbf71f1d967ffb4` (the HTML) |
| `f2210-2025-schedule-ai.txt` | [IRS Form 2210 (2025)](https://www.irs.gov/pub/irs-pdf/f2210.pdf), page layout, the Schedule AI heading and line 20; the same file `ustaxcalc/testdata/f2210-2025.txt` is cut from | `6899ce672648b280bf00ab47200f1b0fbf40368cfbf137df507b945b8577159a` |

An HTML page's hash records what was fetched. A later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
F=https://www.tax.nd.gov/sites/www/files/documents/forms/individual/2025-iit
curl -o es.pdf      $F/28709-form-nd-1es-2026.pdf
curl -o ut.pdf      $F/28704-schedule-nd-1ut-2025.pdf
curl -o booklet.pdf $F/2025-individual-income-tax-booklet.pdf
curl -o t57c38.pdf  https://ndlegis.gov/cencode/t57c38.pdf
curl -o t01c02.pdf  https://ndlegis.gov/cencode/t01c02.pdf
curl -o t01c03.pdf  https://ndlegis.gov/cencode/t01c03.pdf
curl -o reg.pdf     https://www.ndlegis.gov/information/acdata/pdf/81-03-04.pdf
curl -o dis.html    https://www.tax.nd.gov/news/resources/disaster-information-and-tax-implications
curl -o f2210.pdf   https://www.irs.gov/pub/irs-pdf/f2210.pdf

pdftotext es.pdf - | awk '/^FORM ND-1ES INDIVIDUAL ESTIMATED TAX PAYMENT VOUCHER$/{exit} 1' > nd-1es-2026.txt
pdftotext ut.pdf nd-1ut-2025.txt
pdftotext -layout t57c38.pdf - | awk '/^ *57-38-45\. Interest and penalties\./{f=1} /^ *57-38-46\./{f=0}
  /^ *57-38-62\. Payment of estimated income tax\./{f=1} /^ *57-38-64\./{f=0} f' > ndcc-57-38.txt
pdftotext -layout t01c02.pdf - | awk '/^ *1-02-15\. Computation of time\./{f=1} /^ *1-02-16\./{exit} f' > ndcc-1-02-15.txt
pdftotext -layout t01c03.pdf - | awk '/^ *1-03-01\. Holidays\./{f=1} /^ *1-03-06\./{exit} f' > ndcc-1-03.txt
pdftotext -layout reg.pdf - | awk '/^ *81-03-04-02\. Payments of estimated/{f=1} /^ *81-03-04-03\./{exit} f' > ndac-81-03-04-02.txt
pdftotext booklet.pdf - | awk '/^Estimated tax requirement$/{f=1} f; /^Form ND-1ES payment voucher with the$/{if(f)exit}' \
  > booklet-2025-estimated-tax.txt
pdftotext -layout f2210.pdf - | sed -n '110,113p;146p' > f2210-2025-schedule-ai.txt
python3 html2text.py dis.html | awk '/^North Dakota Recognition of Federal Extensions Due to Disaster Situations$/{f=1} f;
  /^Eligible taxpayers have until/{if(f)exit}' > nd-disaster-extensions.txt
```

The booklet is in two columns, and its reading order puts a passage on
deceased taxpayers inside the estimated tax section. The tests read only the
estimated tax sentences.

## North Dakota has no worked example

ND-1UT prints its line arithmetic and no example with an answer. The tests
follow the lines: each quarter's underpayment, the days late ÷ 365 rounded to
three places, × 12% rounded to three places, × the underpayment, from its due
date until paid, offset or April 15.

## What is not here

- **The 2026 ND-1UT.** The department lists the 2025 schedule as the latest.
  Its arithmetic is used with the 2026 dates; Item C's federal date for 2026
  is February 1, 2027, the next business day after Sunday, January 31.
- **The federal rules that decide two facts.** Whether a filer is subject to
  26 U.S.C. § 6654, and whether the federal January installment was excused,
  are answers the caller gives in `EstimatedFacts`.
- **A North Dakota holiday calendar in the code.** No 2026 due date, and not
  April 15, 2027, falls on a § 1-03-01 holiday; the test computes the list and
  checks. The code applies weekends only.
- **N.D.C.C. chapter 40-05.1.** The research shows home rule cities may not
  levy an income tax; nothing here depends on it.
