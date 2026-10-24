# Where these files came from

Every South Carolina figure in this package is checked against a document the
South Carolina Department of Revenue (SCDOR) published, or against statute text
from the South Carolina Legislature or the U.S. Government Publishing Office.
These files are text extracted from those documents. This records which
document, and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; HTML pages reduced to text with the Python 3.12.3 script
below. `F` is `https://dor.sc.gov/sites/dor/files`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `sc1040es-2026.txt` | [SC1040ES, Individual Declaration of Estimated Tax, 2026 (Rev. 10/13/25)](https://dor.sc.gov/sites/dor/files/forms/SC1040ES_2026.pdf) (`F/forms/SC1040ES_2026.pdf`), whole document, reading order | `2972c01622a340867e98a7c83dd215f72f330eaae33c22e55f531bb782f37bd9` |
| `sc2210-2025.txt` | [SC2210, Underpayment of Estimated Tax by Individuals, Estates, and Trusts, 2025 (Rev. 12/02/25)](https://dor.sc.gov/sites/dor/files/forms/SC2210_2025.pdf) (`F/forms/SC2210_2025.pdf`), form and instructions, page layout | `36b93d1729d19c594c9eac1dfecb293dbf721bdb560ca3c90d252664bd873d49` |
| `sc1040-instructions-2025-line33.txt` | [SC1040 Instructions, 2025](https://dor.sc.gov/sites/dor/files/forms/SC1040Instr_2025.pdf) (`F/forms/SC1040Instr_2025.pdf`), the line 33 section, reading order | `e75216d42f37f96c21cb8ca284b13a64b314958d2096c62970212cff94a2b931` |
| `il26-19.txt` | [SC Information Letter #26-19, Interest Rate (August 24, 2026)](https://dor.sc.gov/sites/dor/files/policies/IL26-19.pdf) (`F/policies/IL26-19.pdf`), page layout | `9ec0764df393cdddc0e73687218a399276aff4882604121e3ae682787e213b43` |
| `il26-20.txt` | [SC Information Letter #26-20, Individual Income Tax Reform (August 31, 2026)](https://dor.sc.gov/sites/dor/files/policies/IL26-20.pdf) (`F/policies/IL26-20.pdf`), reading order | `782ce8d4a7b9d7cec0be04834a27aa87d5b17e729e5ac617bdc5ad744837a5ac` |
| `rp23-1.txt` | [SC Revenue Procedure #23-1, Return Due Date Falling on Saturday, Sunday, or Legal Holiday](https://dor.sc.gov/resources-site/lawandpolicy/Advisory%20Opinions/RP23-1.pdf), reading order | `f9d2356946a97a899375455d6b110bd9d543473bdfc00e1dc2f54ed412fbc8e2` |
| `sc-code-12-54.txt` | [S.C. Code Title 12, Chapter 54](https://www.scstatehouse.gov/code/t12c054.php), §§ 12-54-25 and 12-54-55 | `00c3bcdd2f976892e0bbcd91d451d3dd60f203d184caf19abae45e7a0b793b7f` (the HTML) |
| `sc-code-12-6.txt` | [S.C. Code Title 12, Chapter 6](https://www.scstatehouse.gov/code/t12c006.php), § 12-6-50(16) and § 12-6-3910 | `d7930956ea06f1472cac54b7f02c020b880ace2e844a521f4343e4b30ef1e94b` (the HTML) |
| `act110-2026.txt` | [H.4216, Act No. 110 of 2026](https://www.scstatehouse.gov/sess126_2025-2026/bills/4216.htm), the ratified text from its title to its approval date | `9e3fbbd48551b138866bd5c54f1faadd6404d624ba350195459c0c65477e7493` (the HTML) |
| `usc-6654.txt` | [26 U.S.C. § 6654, United States Code, 2024 Edition](https://www.govinfo.gov/link/uscode/26/6654?link-type=html), the section text without its notes | `4d7fcd8ee19a8ba3f5502404a47a9f839378abf0d9a4c01d0adcc00483d480ff` (the HTML) |

An HTML page's hash records what was fetched. A page carries navigation and
scripts that change without its content changing, so a later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
F=https://dor.sc.gov/sites/dor/files
curl -L -o SC1040ES_2026.pdf "$F/forms/SC1040ES_2026.pdf"
curl -L -o SC2210_2025.pdf "$F/forms/SC2210_2025.pdf"
curl -L -o SC1040Instr_2025.pdf "$F/forms/SC1040Instr_2025.pdf"
curl -L -o IL26-19.pdf "$F/policies/IL26-19.pdf"
curl -L -o IL26-20.pdf "$F/policies/IL26-20.pdf"
curl -L -o RP23-1.pdf "https://dor.sc.gov/resources-site/lawandpolicy/Advisory%20Opinions/RP23-1.pdf"
curl -L -o t12c054.html https://www.scstatehouse.gov/code/t12c054.php
curl -L -o t12c006.html https://www.scstatehouse.gov/code/t12c006.php
curl -L -o h4216.html https://www.scstatehouse.gov/sess126_2025-2026/bills/4216.htm
curl -L -o usc6654.html "https://www.govinfo.gov/link/uscode/26/6654?link-type=html"

cat > html2text.py <<'PY'
# Reduces an HTML page to one line of text per block element.
import html, re, sys
raw = open(sys.argv[1], "rb").read()
enc = sys.argv[2] if len(sys.argv) > 2 else "utf-8"
t = raw.decode(enc)
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY

pdftotext SC1040ES_2026.pdf sc1040es-2026.txt
pdftotext -layout SC2210_2025.pdf sc2210-2025.txt
pdftotext SC1040Instr_2025.pdf - \
  | awk '/^Line 33: Underpayment of Estimated Tax penalty/{f=1} /^Line 34: Balance due/{exit} f' \
  > sc1040-instructions-2025-line33.txt
pdftotext -layout IL26-19.pdf il26-19.txt
pdftotext IL26-20.pdf il26-20.txt
pdftotext RP23-1.pdf rp23-1.txt

python3 html2text.py t12c054.html \
  | awk '/^SECTION /{f=0} /^SECTION 12-54-(25|55)\./{f=1} f' > sc-code-12-54.txt
python3 html2text.py t12c006.html \
  | awk '/^\(16\) Sections 2001 through 7655/{print} /^SECTION 12-6-3910\./{f=1} /^SECTION 12-6-3920\./{exit} f' \
  > sc-code-12-6.txt
python3 html2text.py h4216.html iso-8859-1 \
  | awk '/^AN ACT TO AMEND THE SOUTH CAROLINA CODE/{f=1} f; /^Approved the 30th day of March, 2026\./{if(f)exit}' \
  > act110-2026.txt
python3 html2text.py usc6654.html \
  | awk '/^§6654\. Failure by individual/{f=1} /^\(Aug\. 16, 1954/{exit} f' > usc-6654.txt
```

The SC1040ES, the SC1040 instructions, both information letters' prose and the
revenue procedure are extracted in reading order. The SC2210 and IL #26-19 keep
their layout, because their columns and rate periods sit in rows. Where `awk`
exits early, Python reports a broken pipe; the extract is complete. The bill
page is served as ISO-8859-1. `usc-6654.txt` is the same file
`alabamataxcalc/testdata/` holds, from the same fetch command.

## There is no worked penalty example

Neither the SC2210 nor its instructions works an example with an answer. The
only worked example in these documents is the SC1040ES's tax computation, $844
on $25,000, on the 0%, 3% and 6% schedule Act 110 replaced for 2026. This
package computes no tax, so that example is checked only as evidence that the
worksheet is out of date. The penalty tests are worked by hand in their
comments from the SC2210's line arithmetic and IL #26-19's rates.

## The holidays are rules, worked out here

Revenue Procedure #23-1 lists fifteen holidays as rules, such as "Third Monday
in January", and moves a holiday on a Saturday to the Friday before and one on a
Sunday to the Monday after. `estimated_y2026.go` holds the dates those rules
give for 2026 and January to April 15, 2027.
`TestEstimatedCalendarIsRevenueProcedureTwentyThreeOne` reads the list from
`rp23-1.txt`, works the dates out again, and compares.

## What is not here

- **The 2026 SC2210.** `F/forms/SC2210_2026.pdf` returned 404 on 2026-10-20.
  The 2025 form's arithmetic is used with the 2026 figures, and with IL
  #26-19's rate for each of its rate periods in place of its printed .07.
- **A revised 2026 SC1040ES.** None was found. The Rev. 10/13/25 form predates
  Act 110; its thresholds, percentages and dates are unchanged by the act and
  are used.
- **The rate from January 1, 2027.** Not published in IL #26-19. The penalty
  reports the gap in `Required`.
- **SC Information Letter #26-15.** IL #26-19 supersedes its rate table and
  repeats its statement that estimated tax interest is simple, so only #26-19
  is checked in.
- **S.C. Code § 12-60-50.** Not fetched from the Code. Its subsection (A) is
  quoted in full in Revenue Procedure #23-1, which is checked in.
- **A statute for the nonresident contractor exemption.** The SC1040ES states
  it; no section of Chapters 6 or 54 checked in here does. The package reports
  in `Required` that no document says whether it also excuses the penalty.
