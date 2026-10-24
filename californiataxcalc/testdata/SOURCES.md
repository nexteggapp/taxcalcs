# Where these files came from

Every California rate in this library is checked against a document the
Franchise Tax Board published. Those documents are PDFs; these files are text
extracted from them. This records which PDF, and how, so that anybody can redo
it and diff the result.

Retrieved 2026-08-26 with `pdftotext -layout` from poppler 24.02.0.

| File | Source | sha256 of the PDF |
|---|---|---|
| `ftb-540-2025-rate-schedules.txt` | [2025 Personal Income Tax Booklet](https://www.ftb.ca.gov/forms/2025/2025-540-booklet.pdf) | `b48ee24bd2f9b9e74fe9a5bbc548d15bae0bd1ce53306bef1ff33fc76f8078c1` |
| `ftb-540-2025-standard-deduction.txt` | same booklet | same |
| `ftb-540-2025-behavioral-health-tax.txt` | same booklet | same |
| `ftb-540-2025-exemption-credits.txt` | [2025 Form 540](https://www.ftb.ca.gov/forms/2025/2025-540.pdf) | `3be81dcc5d2be3d303596f6fa528e8fdb7cda6644afac6ccf869fc5ebeb7bf1c` |

## Reproducing them

```bash
curl -o 2025-540-booklet.pdf https://www.ftb.ca.gov/forms/2025/2025-540-booklet.pdf
curl -o 2025-540.pdf         https://www.ftb.ca.gov/forms/2025/2025-540.pdf
pdftotext -layout 2025-540-booklet.pdf booklet.txt
pdftotext -layout 2025-540.pdf         f540.txt

awk '/2025 California Tax Rate Schedules$/{f=1} /THIS PAGE INTENTIONALLY/{f=0} f' booklet.txt \
  > ftb-540-2025-rate-schedules.txt

awk '/California Standard Deduction Chart for Most People/{f=1} /The California standard deduction amounts are less/{f=0;print;exit} f' booklet.txt \
  > ftb-540-2025-standard-deduction.txt

awk '/Line 62 . Behavioral Health Services Tax/{f=1} /Line 63 . Other Taxes and Credit Recapture/{f=0} f' booklet.txt \
  > ftb-540-2025-behavioral-health-tax.txt

awk '/For line 7, line 8, line 9, and line 10/{f=1} /Exemption amount: Add line 7 through line 10/{f=0;print;exit} f' f540.txt \
  > ftb-540-2025-exemption-credits.txt
```

The bounds are the document's own headings rather than line numbers, for the
reason the federal `ustaxcalc/testdata/SOURCES.md` gives: a line range breaks
silently the next time the booklet is repaginated.

The rate schedule heading is anchored at the end of the line rather than the
start. The booklet's table of contents carries the same words followed by
leaders and a page number, and anchoring at the start matched that instead —
producing an extract that began at the contents page and contained no rate
schedule at all.

## Why the rate schedules are the strong check

The booklet prints three things for every bracket: where it starts, its rate,
and **the tax on all income below it**. That third column is the Franchise Tax
Board's own arithmetic over every band beneath, so
`TestLadderReproducesEveryPublishedBaseAmount` checks this library's whole
ladder against it — 45 cumulative sums across five filing statuses. One wrong
threshold anywhere in a schedule breaks every base amount above it, so the check
does not depend on anybody having chosen a good test case.

Beneath the schedules the booklet works a full example: a joint return with
$125,000 of taxable income, which it computes as $4,768.10. Inputs and answer
both come from the body that wrote the rule, and
`TestTheBookletsWorkedExample` reproduces it to the cent.

## What this does not prove

That the extract faithfully represents the PDF. The hashes let somebody redo the
extraction and diff, and nothing runs that check automatically.

That the **concepts** were understood. The extract confirms the figures; it
cannot confirm that the standard deduction was applied where California applies
it. The tests below the table level are hand-written and check what this
library's author believed the rules to be.

Nothing here covers Form 540NR. Every figure is a full-year resident's, and
`TaxableIncome` returns the residency question unanswered for that reason.

## Estimated tax for 2026

The figures in `estimated_y2026.go`, checked by `estimated_test.go`. Retrieved
2026-10-20 with curl 8.5.0; PDFs extracted with `pdftotext` from poppler
24.02.0, HTML pages reduced to text with the Python 3.12.3 script below.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `ftb-540es-2026-instructions.txt` | [2026 Instructions for Form 540-ES](https://www.ftb.ca.gov/forms/2026/2026-540-es-instructions.pdf), whole document, reading order | `3e67bc8f3dc6773cee6dab6e3d1b8d70ee03b8f91fbd581b5ad0e44ec49970bc` |
| `ftb-5805-2025.txt` | [2025 Form FTB 5805](https://www.ftb.ca.gov/forms/2025/2025-5805.pdf), whole form, page layout | `cf9a00556393ef16056d2f84dd2d579f845a8f8978580c75c5df4451fd675467` |
| `ftb-5805-2025-instructions.txt` | [2025 Instructions for Form FTB 5805](https://www.ftb.ca.gov/forms/2025/2025-5805-instructions.html), General Information to the end of Worksheet II | `ac1a77eb7c548151d2093aed5485ee86a79427e9bb6408d999760baa51dab33b` (the HTML) |
| `ftb-estimate-penalty-rates.txt` | [Interest and estimate penalty rates](https://www.ftb.ca.gov/pay/penalties-and-interest/interest-and-estimate-penalty-rates.html), last updated 06/22/2026 | `2059c3dccef8b8f72c826c9570a4f1c965f3ba59c5750435d3602597aadc7dd3` (the HTML) |
| `ftb-ab1518-analysis.txt` | [FTB bill analysis of AB 1518](https://www.ftb.ca.gov/tax-pros/law/legislation/2025-2026/AB1518-FINAL.pdf), pages 1 and 2 to the operative date | `9b9735cceb4446bc588e7cff99804ef779bb78c3d8441813666150ca54fc0347` |

An HTML page's hash records what was fetched. The page carries navigation and
scripts that change without its content changing, so a later fetch can hash
differently and extract the same; diff the extract rather than the hash.

### Reproducing them

```bash
curl -o 540es-i.pdf  https://www.ftb.ca.gov/forms/2026/2026-540-es-instructions.pdf
curl -o 5805.pdf     https://www.ftb.ca.gov/forms/2025/2025-5805.pdf
curl -o 5805-i.html  https://www.ftb.ca.gov/forms/2025/2025-5805-instructions.html
curl -o rates.html   https://www.ftb.ca.gov/pay/penalties-and-interest/interest-and-estimate-penalty-rates.html
curl -o ab1518.pdf   https://www.ftb.ca.gov/tax-pros/law/legislation/2025-2026/AB1518-FINAL.pdf

pdftotext 540es-i.pdf ftb-540es-2026-instructions.txt
pdftotext -layout 5805.pdf ftb-5805-2025.txt
pdftotext -layout ab1518.pdf ab1518.txt
awk 'NR==1,/January 1, 2026\.$/' ab1518.txt > ftb-ab1518-analysis.txt

cat > html2text.py <<'PY'
# Reduces an FTB HTML page to one line of text per block element.
import html, re, sys
t = open(sys.argv[1], encoding="utf-8").read()
t = re.sub(r"(?s)<script.*?</script>|<style.*?</style>", "", t)
t = re.sub(r"(?i)<(br|/p|/tr|/li|/h\d|/div)[^>]*>", "\n", t)
t = re.sub(r"(?i)</t[dh]>", " | ", t)
t = html.unescape(re.sub(r"<[^>]+>", "", t))
lines = [re.sub(r"[ \t\xa0]+", " ", l).strip() for l in t.split("\n")]
sys.stdout.write("\n".join(l for l in lines if l) + "\n")
PY
python3 html2text.py 5805-i.html \
  | awk '/^General Information$/{f=1} /^Franchise Tax Board Privacy Notice on Collection$/{if(f)exit} f' \
  > ftb-5805-2025-instructions.txt
python3 html2text.py rates.html \
  | awk '/^Current rates$/{f=1} f; /^Last updated/{if(f)exit}' \
  > ftb-estimate-penalty-rates.txt
```

The 540-ES instructions are set in two columns, so they are extracted in
reading order; FTB 5805 is a single-column form whose percentages sit in a
row, so it keeps its layout.

### The FTB 5805 instructions carry the strongest check

They print the most days each installment can spend in each 2025 rate period
(76 and 289, 15 and 289, 212, 90). `TestDaysPerRatePeriodMatchFTB5805`
accrues a shortfall that is never paid, at the 2025 rates the same
instructions print, and requires those days. It is the only published table
for California's penalty, and it is what showed that a span crossing into a
year of the same length must stay one span: FTB 5805 counts July 1, 2025 to
April 15, 2026 as 289 days on one line.

### What is not here

- **The Revenue and Taxation Code.** leginfo.legislature.ca.gov answers a
  script with a bot challenge, so §§ 19136, 19136.1, 19136.3, 19136.7 and
  19521 are not checked in. The statute side of each divergence, and the
  group-return exception's citation, rest on
  `research/estimated-payments/ca.md`, whose verification read them in a
  browser. The group-return rule is also in the FTB's own analysis of AB 1518,
  which is checked in; that analysis describes the bill and does not itself
  show it was chaptered.
- **The 2026 FTB 5805.** Not published. The 2025 form and instructions are
  used for the line arithmetic, the annualised percentages and the withholding
  shares, which § 19136.1 has fixed since 2009.
- **The rate from January 1, 2027.** Not published. The penalty reports the
  gap in `Required`.
- **A California holiday list.** No FTB document listing the holidays that
  move a due date is checked in. Every 2026 due date the 540-ES prints is a
  weekday, so only weekends are applied, and the tests check the computed
  dates against the printed ones.
