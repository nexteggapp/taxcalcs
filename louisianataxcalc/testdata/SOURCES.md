# Where these files came from

Every Louisiana figure in this package is checked against a document the
Louisiana Department of Revenue (LDR) or the Legislature published. These
files are text extracted from them. This records which document, and how, so
that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; HTML pages reduced to text with the Python 3.12.3 script below.
No site blocked a download.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `act307-2026.txt` | [Act No. 307, 2026 Regular Session (HB 633, enrolled)](https://legis.la.gov/Legis/ViewDocument.aspx?d=1479573), whole act, page layout | `9db939a023b392e205a4f8981f515cee7acf6f9b548cb2521aa9b5570c2aa169` |
| `ldr-2026-legislative-summaries-act307.txt` | [LDR, 2026 Legislative Summaries](https://dam.ldr.la.gov/publications/6.26.26%202026%20Legislative%20Summaries%20Final_Posting_1.pdf), the Act 307 entry, page layout | `2a888960c24bb29dd569ce55e08fa359e901d3d68396155e43f3f4f672add43a` |
| `it540esi-2026.txt` | [IT-540ESi (2026)](https://dam.ldr.la.gov/taxforms/IT540ESi-2026.pdf), whole document, reading order | `5733a329213809222d304e87ab6ce17200790843490fc21240ce415ceb5947dd` |
| `r210ri-2025.txt` | [R-210R-i (1/26), for 2025](https://dam.ldr.la.gov/taxforms/210Ri-1-26.pdf), whole document, page layout | `f9ab7855196c6f9f2a8b7d23a9f48c9b58cbe52163cb5f8bc87c14a57f738b66` |
| `rs-47-116.txt` | [La. R.S. 47:116](https://legis.la.gov/legis/LawPrint.aspx?d=101106) | `57740e960ea800569456a8ad4d6ab8fd8a964c451e87d7fcd12c19cc1dc686be` (the HTML) |
| `rs-47-117.txt` | [La. R.S. 47:117](https://legis.la.gov/legis/LawPrint.aspx?d=101107) | `f1881580cf24bbdb7058ccc1f8dc5b190a8d0ab77154d23a6de08514a98d920e` (the HTML) |
| `rs-47-117.1.txt` | [La. R.S. 47:117.1](https://legis.la.gov/legis/LawPrint.aspx?d=101108) | `9b152657e4ffa384c5e2328f67864a5251afb6e2f5e4d2ec8ea0f3878230b427` (the HTML) |
| `rs-47-118.txt` | [La. R.S. 47:118](https://legis.la.gov/legis/LawPrint.aspx?d=101109) | `e7f15f78151deb0c6bf5312d972d3a0678cb15009c59c01d894277815e966b01` (the HTML) |
| `ldr-estimated-tax-page.txt` | [LDR, Declaration of Estimated Income Taxes](https://revenue.louisiana.gov/individuals/general-resources/declaration-of-estimated-income-taxes/), from its "Declaration of Estimated Tax" heading to "Updated February 27, 2025" | `4b977304f30594ef5406513ff6a43f900270288646f672f34de7eef69ca2b326` (the HTML) |

An HTML page's hash records what was fetched. A page can carry navigation that
changes without its content changing, so diff the extract rather than the hash.

## Reproducing them

```bash
curl -L -o act307.pdf "https://legis.la.gov/Legis/ViewDocument.aspx?d=1479573"
curl -L -o sum.pdf    "https://dam.ldr.la.gov/publications/6.26.26%202026%20Legislative%20Summaries%20Final_Posting_1.pdf"
curl -L -o esi.pdf    https://dam.ldr.la.gov/taxforms/IT540ESi-2026.pdf
curl -L -o 210ri.pdf  https://dam.ldr.la.gov/taxforms/210Ri-1-26.pdf
curl -L -o rs116.html "https://legis.la.gov/legis/LawPrint.aspx?d=101106"
curl -L -o rs117.html "https://legis.la.gov/legis/LawPrint.aspx?d=101107"
curl -L -o rs1171.html "https://legis.la.gov/legis/LawPrint.aspx?d=101108"
curl -L -o rs118.html "https://legis.la.gov/legis/LawPrint.aspx?d=101109"
curl -L -o est.html   https://revenue.louisiana.gov/individuals/general-resources/declaration-of-estimated-income-taxes/

pdftotext -layout act307.pdf act307-2026.txt
pdftotext -layout sum.pdf sum.txt
awk '/^Act 307 \(HB 633\) revises/{f=1} /^Act 624 \(HB 181\)/{if(f)exit} f' sum.txt \
  > ldr-2026-legislative-summaries-act307.txt
pdftotext esi.pdf it540esi-2026.txt
pdftotext -layout 210ri.pdf r210ri-2025.txt

cat > html2text.py <<'PY'
# Reduces an HTML page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
for p in "rs116 rs-47-116" "rs117 rs-47-117" "rs1171 rs-47-117.1" "rs118 rs-47-118"; do
  set -- $p
  python3 html2text.py $1.html | awk '/^RS 47:/{f=1} f' > $2.txt
done
python3 html2text.py est.html \
  | awk '/^Declaration of Estimated Tax$/{f=1} f; /^Updated /{if(f)exit}' \
  > ldr-estimated-tax-page.txt
```

IT-540ESi is set in two columns, so it is extracted in reading order. R-210R-i
and the Act keep their layout, so the form's tables and the Act's lines stay
on one line each. The Act's struck and added words both survive extraction:
"Ninety Seventy percent" is "Ninety" struck and "Seventy" added, as the Act's
footer explains. The legislative summary's PDF maps the "fi" ligature to a
replacement character; no phrase the tests check contains it.

## Which document is out of date

Act 307 applies to tax periods beginning on or after January 1, 2026, and took
effect on May 22, 2026, per LDR's summary. The documents it changes:

| Document | Revised | Says | Act 307 |
|---|---|---|---|
| R-210R-i | 1/26 (PDF created January 27, 2026) | 90%; April 15; Exception 4; Exception 3 on last year's taxable income | 70%; May 15; Exception 4 repealed; this year's standard deduction |
| LDR estimated tax page | February 27, 2025 | 90%; the fourth month; the annualized exception | as above |
| legis.la.gov R.S. 47:118 | history ends at Acts 2006, No. 33 | the text before the Act | as above |

IT-540ESi (2026), created December 3, 2025, also predates the Act, but it
states none of the changed rules, so nothing in it is out of date.

## The strong checks

R-210R-i works two examples. Under line 7, four periods of $400 against
payments of $300, $600, $350 and nothing, leaving $100 short in April and $350
in January; `TestEstimatedLedgerMatchesR210RExample` reproduces the
carryforward. Under line 18, 30 days late is a factor of 0.00987;
`TestEstimatedPenaltyUsesThePrintedDailyFactor` charges it on $1,000.

## What is not here

- **A 2026 R-210R.** Not published. The 2025 form's arithmetic is used, with
  Act 307's figures where the form predates it.
- **R-210NR-i (1/26)**, for nonresidents and part-year residents. Read; its
  arithmetic is the same as R-210R-i's, so no figure rests on it.
- **The IT-540ES voucher.** It carries no figure the rules use.
- **A Louisiana holiday list.** The four 2026 due dates are weekdays, so only
  weekends are applied, and the tests check the computed dates against the
  printed ones.
- **What moves January 31, 2027**, a Sunday. IT-540ESi prints January 31 and
  says nothing of moving it, so it is not moved.
