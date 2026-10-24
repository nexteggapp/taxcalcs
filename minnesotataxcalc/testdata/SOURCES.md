# Where these files came from

Every Minnesota figure in this package is checked against a document the
Minnesota Department of Revenue or the Revisor of Statutes published. These
files are text extracted from those documents. This records which document,
and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. The PDF was extracted with `pdftotext`
from poppler 24.02.0; HTML pages were reduced to text with the Python 3.12.3
script below, which is the one `californiataxcalc/testdata/SOURCES.md` uses.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `m15-2026.txt` | [2026 Schedule M15, Underpayment of Estimated Income Tax, and instructions](https://www.revenue.state.mn.us/sites/default/files/2026-10/m15-26.pdf), marked "FINAL DRAFT — 10/1/26", whole document, page layout | `cdca5cf8e10c4e527793dcad1c3f561e713f96a95c68773b5ce8da1068e755a9` |
| `mndor-estimated-tax.txt` | [Estimated Tax Payments for Individuals](https://www.revenue.state.mn.us/mndor-pp/1946?type=html), last updated January 7, 2026, "Who should make estimated tax payments?" to the due dates | `59889cafdbf385696e367d56270c68de222d444a1bc39bd0ab0e1da99ec76728` (the HTML) |
| `mndor-interest-rates.txt` | [Penalties and Interest for Individuals](https://www.revenue.state.mn.us/mndor-pp/6751?type=html), the Interest Rates table | `79254d621ad81a9870330670ac74b4578a6211d1035132305392c9637e16cdde` (the HTML) |
| `minn-stat-289A.25.txt` | [Minn. Stat. § 289A.25](https://www.revisor.mn.gov/statutes/cite/289A.25), 2025 edition, subdivisions 1 to 10 | `8851f0ce0873c51848755f1425020fab620d7f05c17661ee0379d2f789a93443` (the HTML) |
| `minn-stat-270C.40.txt` | [Minn. Stat. § 270C.40](https://www.revisor.mn.gov/statutes/cite/270C.40), subdivisions 4 and 5 | `2d802ca6865c41b8e16b3fd6cdccba6ef2d67239a2147c022776261f6730dc21` (the HTML) |
| `minn-stat-270C.395.txt` | [Minn. Stat. § 270C.395](https://www.revisor.mn.gov/statutes/cite/270C.395), subdivision 1 | `48b3237983a65c5dbcc654cb9eb4cf93d16371553a1f680afa2a49179ff29bb3` (the HTML) |
| `minn-stat-645.151.txt` | [Minn. Stat. § 645.151](https://www.revisor.mn.gov/statutes/cite/645.151) | `7ba328a178f9a965991c6c8c2697ad063d32964a91a1f4f18126856c28137c0a` (the HTML) |

An HTML page's hash records what was fetched. The pages carry navigation that
changes without the content changing, so a later fetch can hash differently
and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
curl -o m15-26.pdf     https://www.revenue.state.mn.us/sites/default/files/2026-10/m15-26.pdf
curl -o est.html       "https://www.revenue.state.mn.us/mndor-pp/1946?type=html"
curl -o pi.html        "https://www.revenue.state.mn.us/mndor-pp/6751?type=html"
curl -o 289A.25.html   https://www.revisor.mn.gov/statutes/cite/289A.25
curl -o 270C.40.html   https://www.revisor.mn.gov/statutes/cite/270C.40
curl -o 270C.395.html  https://www.revisor.mn.gov/statutes/cite/270C.395
curl -o 645.151.html   https://www.revisor.mn.gov/statutes/cite/645.151

pdftotext -layout m15-26.pdf m15-2026.txt

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
for f in est pi 289A.25 270C.40 270C.395 645.151; do python3 html2text.py $f.html > $f.txt; done

awk '/^Who should make estimated tax payments\?/{f=1} /^Calculate Estimated Tax Payments/{if(f)exit} f' est.txt > mndor-estimated-tax.txt
awk '/^Interest Rates \[\+\]/{f=1} /^How Interest Applies for Individuals/{if(f)exit} f' pi.txt > mndor-interest-rates.txt
awk '/^289A.25 PAYMENT OF ESTIMATED TAX/{f=1} /^Subd. 11\.Fiscal year taxpayer\./{if(f)exit} f' 289A.25.txt > minn-stat-289A.25.txt
awk '/^Subd. 4\.Underpayment of estimated tax\./{f=1} /^Subd. 6\.Unpaid judgment\./{if(f)exit} f' 270C.40.txt > minn-stat-270C.40.txt
awk '/^270C.395 TIMELY MAILING/{f=1} /^Subd. 2\.Mailing requirements\./{if(f)exit} f' 270C.395.txt > minn-stat-270C.395.txt
awk '/^645.151 TIMELY DELIVERY/{f=1} /^History:/{if(f)exit} f' 645.151.txt > minn-stat-645.151.txt
```

The bounds are the documents' own headings, not line numbers.

## Minnesota has no worked example

Schedule M15 prints line arithmetic and no example with an answer. The tests
follow the lines: days ÷ 365 × 7% × the column's underpayment, from the due
date to the date the underpayment was paid in full or April 15, 2027.

## What is not here

- **A final 2026 Schedule M15.** The only 2026 edition published is marked
  "FINAL DRAFT — 10/1/26". Its figures could change before release.
- **The 2027 rate.** It is not needed. § 270C.40 subd. 4 charges a 2026
  underpayment at the rate for January 1, 2026 on every day, including the
  days in 2027.
- **A Minnesota holiday list.** Every 2026 due date the M15 prints is a
  weekday, so only weekends are applied, and the tests check the computed
  dates against the printed ones.
- **26 U.S.C. § 6654(i)(2).** The farmer divergence cites it as read in
  `research/estimated-payments/mn.md`; farmers are not built.
