# Where these files came from

Every New Jersey figure in this package is checked against a document the
Division of Taxation or the Legislature published. These files are text
extracted from those documents. This records which document, and how, so that
anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs were extracted with `pdftotext`
from poppler 24.02.0; the HTML notice was reduced to text with the Python
3.12.3 script in `minnesotataxcalc/testdata/SOURCES.md`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `nj-1040-es-2026.txt` | [2026 Form NJ-1040-ES](https://nj.gov/treasury/taxation/pdf/current/1040esi.pdf), instructions to Table B, reading order | `1244bf4fdc04530fe9eae6702012fa7f3d1fc0397d7493e2d719bece508ab7fc` |
| `nj-2210-2025.txt` | [Form NJ-2210 2025, with instructions](https://www.nj.gov/treasury/taxation/pdf/current/2210.pdf), page layout | `376d6913ab27304556a95d242f34c972c386ad16758ce35a0eee68fa172bbb14` |
| `nj-2210-2025-instructions.txt` | the same PDF, reading order, for the two-column instructions | same |
| `git-8-2026-passages.txt` | [GIT-8, Estimating Income Taxes (January 2026)](https://www.nj.gov/treasury/taxation/pdf/pubs/tgi-ee/git8.pdf), Due Dates, January Payment and the underpayment paragraph, reading order | `ff78673699f986365617260a1b1a7510de8d0115d9948f13936a308964fb89f5` |
| `git-8-2026-examples.txt` | the same PDF, the Part II example and the two Option 1 examples, page layout | same |
| `tb-21r.txt` | [TB-21(R), Interest Rate Assessed on Tax Balances for 2026 (issued December 1, 2025)](https://www.nj.gov/treasury/taxation/pdf/pubs/tb/tb21r.pdf), page layout | `d0e5c7f6275001fd2dd85fe8e36becff726e59d9fe33473f4c1a5443e44b28b9` |
| `nj-notice-over-150000.txt` | [Notice Concerning Estimated Tax for Taxpayers Earning Over $150,000](https://www.nj.gov/treasury/taxation/esti.shtml) (last updated 04/09/25) | `aa8f4ca3d0057788515c605cd091e4dfbbc30bcd854bd1eccbce6454a7270d8f` (the HTML) |
| `njsa-estimated-tax.txt` | [the Legislature's statutes text file](https://pub.njleg.state.nj.us/statutes/STATUTES-TEXT.zip) (`STATUTES.TXT` dated 2026-10-20), N.J.S.A. 54A:8-4, 54A:8-5, 54A:9-6, 54:49-3 and 54:49-11 | `b7b5502b34c140d477c8972dec61244125bfd075109a20b484b84a61a8fe2794` (the zip) |

## Reproducing them

```bash
curl -o es.pdf     https://nj.gov/treasury/taxation/pdf/current/1040esi.pdf
curl -o 2210.pdf   https://www.nj.gov/treasury/taxation/pdf/current/2210.pdf
curl -o git8.pdf   https://www.nj.gov/treasury/taxation/pdf/pubs/tgi-ee/git8.pdf
curl -o tb21r.pdf  https://www.nj.gov/treasury/taxation/pdf/pubs/tb/tb21r.pdf
curl -o esti.html  https://www.nj.gov/treasury/taxation/esti.shtml
curl -o statutes.zip https://pub.njleg.state.nj.us/statutes/STATUTES-TEXT.zip
unzip statutes.zip STATUTES.TXT

pdftotext es.pdf es.txt
awk '/^2026 Form NJ-1040-ES$/{n++} n>=1{f=1} f; /^Table B$/{if(f)exit}' es.txt > nj-1040-es-2026.txt
pdftotext -layout 2210.pdf nj-2210-2025.txt
pdftotext 2210.pdf nj-2210-2025-instructions.txt
pdftotext -layout tb21r.pdf tb-21r.txt

pdftotext git8.pdf git8.txt
awk '/^Due Dates$/{f=1} /^For calendar year taxpayers making estimated payments for the 2025 tax year/{f=0} /^January Payment\. You do not have to file your fourth-quarter/{f=1} /^Farmers\. If your estimated New Jersey farming income/{f=0} /^The amount of your underpayment is the difference between what you actually paid and the lesser of 80% of$/{f=1} /^Example$/{f=0} f' git8.txt > git-8-2026-passages.txt
pdftotext -layout git8.pdf git8.L.txt
awk '/^ +14\. Total amount paid and withheld from January 1 through/{f=1} f; /^After completing all four exceptions/{if(f)exit}' git8.L.txt > git-8-2026-examples.txt
awk '/^The taxpayer met exceptions for all but the third quarter underpayment\./{f=1} f; /\* This worksheet may not reflect current interest rates/{if(f)exit}' git8.L.txt >> git-8-2026-examples.txt

python3 html2text.py esti.html \
  | awk '/^Notice Concerning Estimated Tax for Taxpayers Earning Over \$150,000$/{n++} n==2{f=1} f; /^Therefore, the Division of Taxation will impose/{if(f)exit}' \
  > nj-notice-over-150000.txt

cat > njsections.py <<'PY'
# Prints the named sections of the New Jersey statutes text file, each from
# its heading line to the line before the next section heading.
# usage: njsections.py STATUTES.TXT HEADING...
import sys
lines = open(sys.argv[1], encoding="cp1252").read().replace("\r", "").split("\n")
for s in sys.argv[2:]:
    i = next(n for n, l in enumerate(lines) if l.rstrip() == s)
    j = next(n for n in range(i + 1, len(lines)) if lines[n][:3] in ("54:", "54A"))
    print("\n".join(lines[i:j]).rstrip() + "\n")
PY
python3 njsections.py STATUTES.TXT "54A:8-4   Declarations of estimated tax." "54A:8-5.  Payments of estimated tax" \
  "54A:9-6  Additions to tax and civil penalties." "54:49-3.     Interest, penalty on unpaid tax" \
  "54:49-11.    Remittance, waiving of penalty" > njsa-estimated-tax.txt
```

The statutes file is Windows-1252, which `njsections.py` decodes.

## GIT-8's examples are the strong check

GIT-8 works one taxpayer through Form NJ-2210: $150 due a quarter, $90, $90,
$90 and $240 paid, the four exceptions' amounts, and the Option 1 interest
both when only the third quarter misses an exception ($1.86) and when every
quarter does ($10.71). Its multipliers are marked as illustrations, not the
current rate. `TestEstimatedInterestFollowsTheGIT8Examples` takes the amounts,
the exceptions and the structure, which are the Division's, and applies the
2026 rate for the same months.

## What is not here

- **The 2026 Form NJ-2210 and the 2026 GIT-8.** Not published; the "current"
  URL serves the 2025 form. The 2025 form is used with the 2026 dates and
  rate.
- **The rate for 2027.** TB-21(R) is reissued each December from the prime
  rate on December 1. The interest reports the months it cannot charge.
- **N.J.A.C. 18:35.** Not reachable on an official site per
  `research/estimated-payments/nj.md`.
- **A New Jersey holiday list.** Every 2026 due date is a weekday, so only
  weekends are applied. Whether February 15, 2027, Presidents' Day, moves is
  left to the caller.
