# Where these files came from

Every Arizona figure in this package is checked against a document the
Arizona Department of Revenue (ADOR) or the Arizona Legislature published.
These files are text extracted from them. This records which document, and
how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; HTML pages reduced to text with the Python 3.12.3 script
below. `D` is `https://azdor.gov/sites/default/files/document`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `az-140es-2026-instructions.txt` | [Form 140ES instructions, 2026](https://azdor.gov/sites/default/files/document/FORMS_INDIVIDUAL_2026_140iES.pdf) (`D/FORMS_INDIVIDUAL_2026_140iES.pdf`), whole document, reading order | `2cd7fe66ce6c288f15c5fdd47c0c89024d2b0030e49cfd3a916479483fcb62c3` |
| `az-140es-2026.txt` | [Form 140ES voucher and worksheet, 2026](https://azdor.gov/sites/default/files/document/FORMS_INDIVIDUAL_2026_140ES_f.pdf) (`D/FORMS_INDIVIDUAL_2026_140ES_f.pdf`), whole form, page layout | `93240ea281207c2f8ef572c8b9c6fb1b38381205a05f7e777158bfc1ea2042c7` |
| `az-221-2025.txt` | [Form 221, Underpayment of Estimated Tax by Individuals, 2025](https://azdor.gov/sites/default/files/document/FORMS_INDIVIDUAL_2025_221_f.pdf) (`D/FORMS_INDIVIDUAL_2025_221_f.pdf`), whole form, page layout | `d09ada6d2ebf1d5c827ab4fbb593efd16b6dfb82e684680d960ee4181844ac41` |
| `az-221-2025-instructions.txt` | [Form 221 instructions, 2025](https://azdor.gov/sites/default/files/document/FORMS_INDIVIDUAL_2025_221i.pdf) (`D/FORMS_INDIVIDUAL_2025_221i.pdf`), whole document, reading order | `7cebb3a0df4e83a906977968a9d66423160bab6378795381a741d48ae2e97311` |
| `az-221-2025-days-chart.txt` | the same PDF, the Chart of Total Days per Period, page layout | same |
| `ars-43-581.txt` | [A.R.S. § 43-581](https://www.azleg.gov/ars/43/00581.htm), whole section | `a0f4cafb6333bca2cfd67e31f437a1c0ece754f18738b1c7822a9f7876ab92ea` (the HTML) |
| `ars-42-1125-q.txt` | [A.R.S. § 42-1125](https://www.azleg.gov/ars/42/01125.htm), heading and subsection Q | `645877cd9963e3f8a8451737056b7ff02cde2f0b824b85a078ba514daa27150b` (the HTML) |
| `ars-42-1123-ab.txt` | [A.R.S. § 42-1123](https://www.azleg.gov/ars/42/01123.htm), heading and subsections A and B | `53e0361efcd8ea76d8be7f9bd58aac8720d52421911b705bfe378c95887c42bf` (the HTML) |
| `ador-interest-rates.txt` | [ADOR, Interest Rates](https://azdor.gov/business/interest-rates), from the opening paragraph to the 2025 row | `42830eab0f7e7a98bfff1e43912e90edee2c4a4115cb2c5583564589fb590ce1` (the HTML) |
| `ador-individual-estimated-payments.txt` | [ADOR, Individual Estimated Tax Payments](https://azdor.gov/individuals/individual-estimated-tax-payments), When Am I Required to File to How Much Should My Estimated Payments Total | `a432754e351cd4dfc8691998e52197b555c8cf9eb243792587bad33dda7c40f0` (the HTML) |

An HTML page's hash records what was fetched. ADOR's pages carry navigation
and scripts that change without their content changing, so a later fetch can
hash differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
D=https://azdor.gov/sites/default/files/document
curl -o 140iES.pdf "$D/FORMS_INDIVIDUAL_2026_140iES.pdf"
curl -o 140ES.pdf  "$D/FORMS_INDIVIDUAL_2026_140ES_f.pdf"
curl -o 221.pdf    "$D/FORMS_INDIVIDUAL_2025_221_f.pdf"
curl -o 221i.pdf   "$D/FORMS_INDIVIDUAL_2025_221i.pdf"
curl -o ars-43-581.htm  https://www.azleg.gov/ars/43/00581.htm
curl -o ars-42-1125.htm https://www.azleg.gov/ars/42/01125.htm
curl -o ars-42-1123.htm https://www.azleg.gov/ars/42/01123.htm
curl -o interest-rates.html     https://azdor.gov/business/interest-rates
curl -o estimated-payments.html https://azdor.gov/individuals/individual-estimated-tax-payments

pdftotext 140iES.pdf az-140es-2026-instructions.txt
pdftotext -layout 140ES.pdf az-140es-2026.txt
pdftotext -layout 221.pdf az-221-2025.txt
pdftotext 221i.pdf az-221-2025-instructions.txt
pdftotext -layout 221i.pdf 221i.L.txt
awk '/Rate Period +\(a\) +\(b\) +\(c\) +\(d\)/{f=1} f; /4 \(line 26\)/{if(f)exit}' 221i.L.txt \
  > az-221-2025-days-chart.txt

cat > html2text.py <<'PY'
# Reduces an HTML page to one line of text per block element, and one line
# per table row with its cells separated by " | ".
import html, re, sys

def text(s):
    s = html.unescape(re.sub(r"<[^>]+>", " ", s))
    return re.sub(r"[ \t\r\n\xa0]+", " ", s).strip()

def row(m):
    cells = re.findall(r"(?is)<t[dh][^>]*>(.*?)</t[dh]>", m.group(0))
    return "\n" + " | ".join(text(c) for c in cells) + "\n"

t = open(sys.argv[1], encoding="utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?is)<tr[^>]*>.*?</tr>", row, t)
t = re.sub(r"(?i)<(br|/p|/li|/h\d|/div)[^>]*>", "\n", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\r\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
python3 html2text.py ars-43-581.htm > ars-43-581.txt
python3 html2text.py ars-42-1125.htm | awk 'NR==1 || /^Q\. /' > ars-42-1125-q.txt
python3 html2text.py ars-42-1123.htm | awk 'NR==1 || /^[AB]\. /' > ars-42-1123-ab.txt
python3 html2text.py interest-rates.html \
  | awk '/^On January 1 of each year/{f=1} f; /^1\/1\/2025 \|/{if(f)exit}' > ador-interest-rates.txt
python3 html2text.py estimated-payments.html \
  | awk '/^When Am I Required to File an Estimated Payment\?$/{f=1} /^How Do I Claim my Estimated Payment\?$/{if(f)exit} f' \
  > ador-individual-estimated-payments.txt
```

The instruction documents are set in two columns, so they are extracted in
reading order. The 140ES voucher and Form 221 keep their layout, because their
lines and columns sit in rows. The Chart of Total Days loses its columns in
reading order, so it is extracted again with its layout, bounded by its own
heading row and its last row; the left column's text shares those lines.

## Form 221's chart of days is the strong check

Arizona publishes no worked example with a stated penalty. Form 221's
instructions print the days an underpayment left unpaid spends in each rate
period: 76, 92, 92 and 105 for April; 15, 92, 92 and 105 for June; 15, 92
and 105 for September; 90 for January. `TestEstimatedDaysMatchTheForm221Chart`
accrues four shortfalls that are never paid and requires those days. The
chart's 15 days for June fit 2026's June 15 due date; the 2025 form's own
June 16 would give 14, so the chart appears to have been carried from a year
whose date did not move.

## What is not here

- **The 2026 Form 221.** Not published. The 2025 form's arithmetic, rate
  periods, compounding, cap and stop are used with the 2026 dates and rates.
- **The rate from January 1, 2027.** Not published. The 2025 form prints "*%"
  for the matching period, "to be determined". The penalty reports the gap in
  `Required`.
- **An Arizona holiday list.** No ADOR document listing the holidays that move
  a due date is checked in. Every 2026 due date the 140ES prints is a weekday,
  so only weekends are applied, and the tests check the computed dates against
  the printed ones.
- **Arizona Administrative Code rules under § 43-581(D).** Not checked; the
  research did not read them either. They might settle whether the prior-year
  option needs a required return or a 12-month year.
