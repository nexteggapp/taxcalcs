# Where these files came from

Every Montana figure in this package is checked against a document the
Montana Department of Revenue or the Legislature published, and the 2027 rate
against the IRS ruling Montana's statute adopts. These files are text
extracted from those documents. This records which document, and how, so that
anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs were extracted with `pdftotext`
from poppler 24.02.0; HTML pages were reduced to text with the Python 3.12.3
script in `minnesotataxcalc/testdata/SOURCES.md`, and passages bounded with
the `passages.py` script in `mississippitaxcalc/testdata/SOURCES.md`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `est-i-2025.txt` | [Form EST-I, 2025 Interest on Underpayment of Estimated Tax by Individuals, Estates, and Trusts (V1 6/2025)](https://revenuefiles.mt.gov/files/Forms/EST-I/EST-I-2025.pdf), whole form, page layout | `395d9d608a5f700b0c6c6e4714e778b07ec5c793923cbea8a7f80053595c4f31` |
| `est-i-2025-instructions.txt` | the same PDF, pages 5 to 10, the instructions, reading order | same |
| `publication-1-2026-estimated.txt` | [2026 Montana Publication 1 (V1 September 2025)](https://revenuefiles.mt.gov/files/Forms/Publication-1/Publication-1-2026.pdf), Estimated Tax Payments to Estimating Taxable Income, and the interest paragraph, reading order | `e3c8ead48b90065910744bb2ffdfc23709760892bfaa34ee4b468286cdde620d` |
| `mca-15-30-2512.txt` | [Mont. Code Ann. § 15-30-2512 (2025)](https://archive.legmt.gov/bills/mca/title_0150/chapter_0300/part_0250/section_0120/0150-0300-0250-0120.html) | `a1bb91522bae5360a30faaa02f99420bacef19e8c8c5b7bdee18edb6d137d37e` (the HTML) |
| `mca-15-1-216.txt` | [Mont. Code Ann. § 15-1-216 (2025)](https://archive.legmt.gov/bills/mca/title_0150/chapter_0010/part_0020/section_0160/0150-0010-0020-0160.html), subsections (2) and (7) | `3f8a04811926a9c2004680c2f1816abd09856ca6476032223ee66de07c7a8bd8` (the HTML) |
| `mca-1-1-216.txt` | [Mont. Code Ann. § 1-1-216 (2025)](https://archive.legmt.gov/bills/mca/title_0010/chapter_0010/part_0020/section_0160/0010-0010-0020-0160.html) | `dc9f2a1d0fefda35d36b6fa99fc98f7394bcac4c55d7319560723b7584402c15` (the HTML) |
| `mca-1-1-307.txt` | [Mont. Code Ann. § 1-1-307 (2025)](https://archive.legmt.gov/bills/mca/title_0010/chapter_0010/part_0030/section_0070/0010-0010-0030-0070.html) | `44bc9ca3c9a316dc9142e728868e3cf8fc5b34096c5661bdb002ac2a79ac6aa0` (the HTML) |
| `dor-interest-and-penalties.txt` | [Montana DOR, Interest and Penalties](https://revenue.mt.gov/taxes/penalty-and-interest), Late Payment Penalty and 2026 Interest Rates | `c82f0224bc5c0192b60dac1dabe6303b273f983af9b1cd46008f8e7b9536ddfd` (the HTML) |
| `irs-rev-rul-2026-10.txt` | [Internal Revenue Bulletin 2026-22](https://www.irs.gov/pub/irs-irbs/irb26-22.pdf), Rev. Rul. 2026-10 to the § 6603 rate | `ee364d94f3b589274e36cdc7ce6bd9cb3c7a6c3d13599ccce297e2aef62c55ae` |

An HTML page's hash records what was fetched. A later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
curl -o esti.pdf https://revenuefiles.mt.gov/files/Forms/EST-I/EST-I-2025.pdf
curl -o pub1.pdf https://revenuefiles.mt.gov/files/Forms/Publication-1/Publication-1-2026.pdf
curl -o 2512.html    https://archive.legmt.gov/bills/mca/title_0150/chapter_0300/part_0250/section_0120/0150-0300-0250-0120.html
curl -o 216.html     https://archive.legmt.gov/bills/mca/title_0150/chapter_0010/part_0020/section_0160/0150-0010-0020-0160.html
curl -o 1-1-216.html https://archive.legmt.gov/bills/mca/title_0010/chapter_0010/part_0020/section_0160/0010-0010-0020-0160.html
curl -o 1-1-307.html https://archive.legmt.gov/bills/mca/title_0010/chapter_0010/part_0030/section_0070/0010-0010-0030-0070.html
curl -o pi.html      https://revenue.mt.gov/taxes/penalty-and-interest
curl -o irb26-22.pdf https://www.irs.gov/pub/irs-irbs/irb26-22.pdf

pdftotext -layout esti.pdf est-i-2025.txt
pdftotext -f 5 -l 10 esti.pdf est-i-2025-instructions.txt
pdftotext pub1.pdf pub1.txt
python3 passages.py pub1.txt \
  "Estimated Tax Payments" "Estimating Taxable Income for the" \
  "Interest is calculated daily based on the rate" "Note: You must indicate you are using the" \
  > publication-1-2026-estimated.txt

for f in 2512 216 1-1-216 1-1-307 pi; do python3 html2text.py $f.html > $f.txt; done
awk '/^15-30-2512\..*\(1\) \(a\) Each individual/{f=1} /^History:/{if(f)exit} f' 2512.txt > mca-15-30-2512.txt
awk '/^\(2\) \(a\) \(i\) Except as provided/{f=1} /^\(3\) \(a\) Subject to subsection/{f=0} /^\(7\) \(a\) Interest on taxes not paid when due/{f=1} /^\(8\) \(a\) Except as provided/{f=0} f' 216.txt > mca-15-1-216.txt
awk '/^1-1-216\..*\(1\) The following are legal holidays/{f=1} /^History:/{if(f)exit} f' 1-1-216.txt > mca-1-1-216.txt
awk '/^1-1-307\..*Whenever any act/{f=1} /^History:/{if(f)exit} f' 1-1-307.txt > mca-1-1-307.txt
python3 passages.py pi.txt "Late Payment Penalty" "Failure to File Penalty" "2026 Interest Rates" "Interest Rates for Taxes Owed From Prior Years" > dor-interest-and-penalties.txt
pdftotext -f 1 -l 50 irb26-22.pdf irb.txt
awk 'NR>=579 && /^Rev\. Rul\. 2026-10$/{f=1} f; /third calendar quarter in 2026\./{if(f)exit}' irb.txt > irs-rev-rul-2026-10.txt
```

The section headings on archive.legmt.gov put an em space after the section
number, which is why the patterns match it with `.*`. The IRB's table of
contents also names Rev. Rul. 2026-10, so its extract starts from the
ruling's own heading, the second occurrence.

## EST-I's Example 3 is the strong check, with one column that does not add up

The 2025 EST-I instructions work Example 3: $4,000 required each quarter,
five payments, and interest at 0.000192 a day. Columns B, C and D are
consistent with the form's lines. Column A is not: it charges the whole
$4,000 for both 15 days ($11.52) and 62 days ($47.62), though $2,000 was paid
on each date, and the $186.05 total leaves the $47.62 out. The instructions
say to make a separate computation for each payment, which charges $2,000
for each span. `TestEstimatedInterestFollowsTheFormExample` uses the 2026
dates and requires columns C and D to equal the example's $70.27 and $51.84,
which the same day counts give.

## What is not here

- **The 2026 EST-I.** Not published; `EST-I-2026.pdf` returns 404 per
  `research/estimated-payments/mt.md`. The 2025 form is used with the 2026
  dates.
- **The department's 2027 rate.** § 15-1-216(7)(a) has the department set
  each calendar year's rate by rule at the federal underpayment rate for the
  third quarter of the preceding year. Rev. Rul. 2026-10 sets that at 7%,
  and the department has not announced 2027's. The penalty reports this in
  `Required`.
- **The Administrative Rules of Montana.** rules.mt.gov serves a JavaScript
  application whose HTML (sha256
  `ad4e402b3ac31232c1b1f870f77389fc04fc37dcca7eba2c7755971af3f1d4c4` for
  ARM 42.17.314) holds no rule text. ARM 42.17.306(3), 42.17.314(1) and (5)
  and 42.2.505(5) are cited as `research/estimated-payments/mt.md` quotes
  them in its verification: 42.17.314(1), a nonresident or part-year
  resident "who was not required to file a Montana individual income tax
  return for the previous tax year"; 42.17.314(5), "A participant in a
  composite return for the current tax year is not required to pay
  estimated tax for the current tax year"; 42.2.505(5), "the rate in effect
  on the original due date of the tax return shall be used".
- **State general election day.** § 1-1-216(1)(l) makes it a holiday on a
  date another title sets. It is left out of the calendar; no date here is
  near November 3, 2026.
