# Where these files came from

Every New York figure in this package is checked against a document the
Department of Taxation and Finance or the Senate's laws site published. These
files are text extracted from those documents. This records which document,
and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs were extracted with `pdftotext`
from poppler 24.02.0, in reading order; HTML pages were reduced to text with
the Python 3.12.3 script in `minnesotataxcalc/testdata/SOURCES.md`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `it-2105-i-2026.txt` | [IT-2105-I (2026), Instructions for Form IT-2105](https://www.tax.ny.gov/pdf/2026/inc/it2105i_2026.pdf) | `c5476fba20126277bb3556def5655293b3dfa42f7d8ee04c92732b51bbf1823f` |
| `it-2105.9-i-2025.txt` | [IT-2105.9-I (2025), Instructions for Form IT-2105.9](https://www.tax.ny.gov/pdf/2025/inc/it2105_9i_2025.pdf) | `47f4092f86b2262dbc967dbf1891bb3e3f4e5603c04bd7ecadedade3f8e853a8` |
| `tax-law-685-c-d.txt` | [Tax Law § 685](https://www.nysenate.gov/legislation/laws/TAX/685), subsections (c) and (d) | `3edbfefb2f3ee2751c7d9f17bcc9e03b767c9316f66723b29b16681c0a09dc4f` (the HTML) |
| `tax-law-697-j.txt` | [Tax Law § 697](https://www.nysenate.gov/legislation/laws/TAX/697), subsection (j) | `be79dc30cf2d01661922dfc67994a448a0b0e18195af2e166e65b2758d2d5410` (the HTML) |
| `general-construction-law-25-a.txt` | [General Construction Law § 25-a](https://www.nysenate.gov/legislation/laws/GCN/25-A) | `66a74792fda93cde96d28bc1ce4908c1ccca4adb3fde4f3a61b3c9fa109012bc` (the HTML) |
| `dtf-interest-rates-2026.txt` | DTF interest rate pages for [Q1](https://www.tax.ny.gov/pay/interest/2026/p1.htm), [Q2](https://www.tax.ny.gov/pay/interest/2026/p2.htm), [Q3](https://www.tax.ny.gov/pay/interest/2026/p3.htm) and [Q4](https://www.tax.ny.gov/pay/interest/2026/p4.htm) 2026: each page's title and its § 697(j) sentence | `f6e27326…29d1`, `465c85ff…9836`, `00b8be17…8ecd`, `0308cd55…68bf5` (the HTML; in full below) |

The four rate pages' hashes in full: p1
`f6e27326300865d1335637b0da7e5ac16020a2a706faecea77cc03f1973929d1`, p2
`465c85ff59ad741b19facea470f02eb8acded2bce009ed01cc9c87a641a19836`, p3
`00b8be1753884ad382b316e8214099f22ced405df4c06f873500f8ddf4678ecd`, p4
`0308cd5577016283c251915d7455f6827ec96738ec4018eccbad113f02568bf5`.

An HTML page's hash records what was fetched. A later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
curl -o it2105i.pdf   https://www.tax.ny.gov/pdf/2026/inc/it2105i_2026.pdf
curl -o it2105_9i.pdf https://www.tax.ny.gov/pdf/2025/inc/it2105_9i_2025.pdf
curl -o 685.html https://www.nysenate.gov/legislation/laws/TAX/685
curl -o 697.html https://www.nysenate.gov/legislation/laws/TAX/697
curl -o gcl.html https://www.nysenate.gov/legislation/laws/GCN/25-A
for p in p1 p2 p3 p4; do curl -o $p.html https://www.tax.ny.gov/pay/interest/2026/$p.htm; done

pdftotext it2105i.pdf it-2105-i-2026.txt
pdftotext it2105_9i.pdf it-2105.9-i-2025.txt
for f in 685 697 gcl p1 p2 p3 p4; do python3 html2text.py $f.html > $f.txt; done
awk '/^\(c\) Failure by individual to pay estimated income tax\.--$/{f=1} /^\(e\) Deficiency due to fraud\./{if(f)exit} f' 685.txt > tax-law-685-c-d.txt
awk '/^\(j\) Authority to set interest rates\./{f=1} /^\(k\) /{if(f)exit} f' 697.txt > tax-law-697-j.txt
awk '/^§ 25-a\. Public holiday, Saturday or Sunday in statutes;/{f=1} /^NYSenate\.gov$/{if(f)exit} f' gcl.txt > general-construction-law-25-a.txt
for p in p1 p2 p3 p4; do sed -n 1p $p.txt; grep "For purposes of" $p.txt | grep "697(j)"; done > dtf-interest-rates-2026.txt
```

`research/estimated-payments/ny.md` reports that the Senate's laws site blocked
direct download. On 2026-10-20 it served the full text to curl.

## New York has no worked example for this method

The IT-2105.9 instructions work only the short method, at the 2025 rate. The
tests follow the regular method's arithmetic: each part of each shortfall, by
the day ÷ 365, at each period's rate, a period ending at the payment, the end
of the rate quarter or the next due date.

## What is not here

- **The 2026 IT-2105.9 and its instructions.** Not published:
  `it2105_9i_2026.pdf` returns a 404 page (sha256
  `68719802e886369b1dfa0d96493f5324d6168a26035c5bd541b049ef050de2b8`). The
  2025 instructions are used with the 2026 dates and rates.
- **The rate from January 1, 2027.** Not published. The penalty reports the
  gap in `Required`.
- **The New York City and Yonkers laws, and the MCTMT article.** Not read.
  The $300 for each tax and the MCTD 110% step are as the forms state them.
- **A New York holiday list.** Every 2026 due date is a weekday, so only
  weekends are applied.
