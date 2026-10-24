# Where these files came from

Every Iowa figure in this package is checked against a document the Iowa
Department of Revenue or the Iowa Legislature published. These files are text
extracted from them. This records which document, and how, so that anybody can
redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; HTML pages reduced to text with the Python 3.12.3 script
below.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `ia-1040es-2026.txt` | [2026 IA 1040ES, Iowa Individual Estimated Income Tax Instructions](https://revenue.iowa.gov/media/2725/download?inline) (45-009a and 45-009b, 08/29/2025), whole document, reading order | `c2bd976c0c9e76a7539dffc629d6fa2e10fa383f1d7ce3aabb2420d5e315d825` |
| `ia-2210-2025.txt` | [2025 IA 2210, Iowa Underpayment of Estimated Tax by Individuals](https://revenue.iowa.gov/media/4399/download) (45-007a to 45-007d), form and instructions, reading order | `592bfa331e5aabd6b7aab7c98bea03884ca926f2199a6cd1465c5548fcc15a7f` |
| `iowa-code-2026-422.16-12.txt` | [Iowa Code 2026, § 422.16](https://www.legis.iowa.gov/docs/code/2026/422.16.pdf), subsection 12 and the section history | `bfa93a54e8cc7c302cb5bff6f1247666470b22f74db4255574289ca88919b8ad` |
| `iowa-code-2026-421.7.txt` | [Iowa Code § 421.7](https://www.legis.iowa.gov/docs/code/421.7.pdf), whole section (the PDF's footer says Iowa Code 2026) | `b225c611b75a2917aaf454313feb2a8736fe233605cdfaf3ac900fa6fefde5d1` |
| `iowa-code-2026-421.9A.txt` | [Iowa Code § 421.9A](https://www.legis.iowa.gov/docs/code/421.9A.pdf), whole section (Iowa Code 2026) | `33739991f31a875dbeeb9cd900fcad0400e9af9a10ddb2f8ef96446137c1ea63` |
| `iac-701-308.6.txt` | [Iowa Admin. Code r. 701—308.6](https://www.legis.iowa.gov/docs/iac/rule/701.308.6.pdf), whole rule | `e1bf6c28560c8ce491ede3b7f7d91a80d3d7178e876d2371038f2e80f7a57ee4` |
| `idr-2026-interest-rate.txt` | [IDR Announces 2026 Individual Income Tax and Interest Rates](https://revenue.iowa.gov/press-release/2025-10-21/idr-announces-2026-individual-income-tax-and-interest-rates), October 21, 2025, the release to the media contact | `f01ec37246c9d0d973b78fff5dc17849f8d8b168326ebc72422c98b9b2ab887e` (the HTML) |
| `idr-estimated-payments.txt` | [IDR, Estimated Income Tax Payments](https://revenue.iowa.gov/taxes/tax-guidance/individual-income-tax/estimated-income-tax-payments), undated, the page body | `54f3742f564d20b644db952f9b289f070d968fb1547106f691bb5070c2952c2f` (the HTML) |
| `idr-1040-line-35.txt` | [IDR, 1040 Expanded Instructions, Line 35: Penalty for Underpayment of Estimated Tax](https://revenue.iowa.gov/taxes/tax-guidance/individual-income-tax/1040-expanded-instructions/penalty-underpayment), instruction year 2025, the page body | `45de660148172289d0765080f174f028aee8b90921f21093545e4ffe991cffa2` (the HTML) |

An HTML page's hash records what was fetched. The pages carry navigation and
scripts that change without their content changing, so a later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
curl -L -o es26.pdf      "https://revenue.iowa.gov/media/2725/download?inline"
curl -L -o 2210-25.pdf   https://revenue.iowa.gov/media/4399/download
curl -L -o 422.16.pdf    https://www.legis.iowa.gov/docs/code/2026/422.16.pdf
curl -L -o 421.7.pdf     https://www.legis.iowa.gov/docs/code/421.7.pdf
curl -L -o 421.9A.pdf    https://www.legis.iowa.gov/docs/code/421.9A.pdf
curl -L -o 308.6.pdf     https://www.legis.iowa.gov/docs/iac/rule/701.308.6.pdf
curl -L -o rate26.html   https://revenue.iowa.gov/press-release/2025-10-21/idr-announces-2026-individual-income-tax-and-interest-rates
curl -L -o est.html      https://revenue.iowa.gov/taxes/tax-guidance/individual-income-tax/estimated-income-tax-payments
curl -L -o l35.html      https://revenue.iowa.gov/taxes/tax-guidance/individual-income-tax/1040-expanded-instructions/penalty-underpayment

pdftotext es26.pdf    ia-1040es-2026.txt
pdftotext 2210-25.pdf ia-2210-2025.txt
pdftotext 421.7.pdf   iowa-code-2026-421.7.txt
pdftotext 421.9A.pdf  iowa-code-2026-421.9A.txt
pdftotext 308.6.pdf   iac-701-308.6.txt
pdftotext 422.16.pdf  422.16.txt
awk '/^12\. a\. \(1\)/{f=1} /^13\. The director/{f=0} f' 422.16.txt > iowa-code-2026-422.16-12.txt
awk '/2025 Acts, ch 148, §58 – 60$/{f=1} f' 422.16.txt >> iowa-code-2026-422.16-12.txt

cat > html2text.py <<'PY'
# Reduces an IDR HTML page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
python3 html2text.py rate26.html \
  | awk 'NR>1 && /^IDR Announces 2026 Individual Income Tax and Interest Rates$/{f=1} /^Media Contact$/{if(f)exit} f' \
  > idr-2026-interest-rate.txt
python3 html2text.py est.html \
  | awk '/^Estimated income tax is just that/{f=1} /^Secondary Navigation Menu$/{if(f)exit} f' \
  > idr-estimated-payments.txt
python3 html2text.py l35.html \
  | awk '/^Line 35: Penalty for Underpayment of Estimated Tax$/{f=1} /^Go to Line 34/{if(f)exit} f' \
  > idr-1040-line-35.txt
```

The 1040ES and the IA 2210 are kept whole in reading order. The IA 2210's
late-start table loses its rows in reading order; the same table is in the
line 35 page as rows, and the test checks both. § 422.16 is cut to subsection
12, which ends where subsection 13 starts, and the history notes that date the
2025 amendment.

## The line 35 example is the strong check

The department's expanded instructions for IA 1040 line 35 work an example:
$1,000 due each quarter, paid $500 on June 15, $2,000 on June 30, $500 on
January 31 and $1,000 on February 25. They say how each payment is applied and
how many days each piece is charged: 46 and 61 days on April's two halves,
none on June, 92 and 31 days on September's $500, and 25 days on the fourth
$1,000. Inputs and day counts both come from the department.
`TestEstimatedPenaltyMatchesLineThirtyFiveExample` reproduces them with the
2026 dates; the fourth installment is due February 1, 2027 rather than January
31, so its piece is 24 days. The example gives no penalty in dollars.

## What is not here

- **The 2026 IA 2210.** Not published: the department's forms search lists the
  IA 2210 only through 2025 (research/estimated-payments/ia.md, Verification).
  The 2025 form's arithmetic is used with the 2026 dates. Its $200 test
  predates 2025 Iowa Acts ch. 148, § 59, so the act's $1,000 is used and the
  result reports the divergence.
- **The 2025 IA 2210F and IA 2210 Schedule AI.** Farmers, fishers and the
  annualised method are not built, so neither is checked in.
- **An Iowa holiday list.** § 421.9A names its holidays by category. No list of
  dates is checked in. The first three 2026 dates are weekdays, and the fourth
  moves for the weekend.
- **The rate from January 1, 2027.** Not published. The penalty reports the
  gap in `Required`.
