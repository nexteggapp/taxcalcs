# Where these files came from

Every Oregon rate in this library is checked against a document the Oregon
Department of Revenue published. Those documents are PDFs; these files are text
extracted from them.

Retrieved 2026-08-26 with `pdftotext -layout` from poppler 24.02.0.

| File | Source | sha256 of the PDF |
|---|---|---|
| `or40-2025-tax-table.txt` | [2025 Form OR-40 Instructions](https://www.oregon.gov/dor/forms/FormsPubs/form-or-40-inst_101-040-1_2025.pdf) | `4ee45bad719499e2b69977b0db15de75ed063ac631b151fdefe3db081fa1eaeb` |
| `or40-2025-rate-charts.txt` | same instructions | same |
| `or40-2025-standard-deduction.txt` | same instructions | same |
| `or40-2025-federal-tax-subtraction.txt` | same instructions | same |
| `or17-2025-annual-figures.txt` | [Publication OR-17 (2025)](https://www.oregon.gov/dor/forms/FormsPubs/publication-or-17_101-431_2025.pdf) | `3ccfb6b46d9e221cefa0f3f867d0799bb53192005486b90446d9205f2b9cc7f6` |

## Reproducing them

```bash
curl -o or40-inst.pdf https://www.oregon.gov/dor/forms/FormsPubs/form-or-40-inst_101-040-1_2025.pdf
curl -o or17.pdf      https://www.oregon.gov/dor/forms/FormsPubs/publication-or-17_101-431_2025.pdf
pdftotext -layout or40-inst.pdf orinst.txt
pdftotext -layout or17.pdf      or17.txt

# The tax table is on pages 30 to 32.
pdftotext -layout -f 30 -l 32 or40-inst.pdf - \
  | grep -E "[0-9] *— *[0-9,]+ +[0-9,]+ +[0-9,]+" \
  > or40-2025-tax-table.txt

awk '/^  2025 Tax rate charts/{f=1} /150-101-040-1/{f=0} f' orinst.txt \
  > or40-2025-rate-charts.txt

awk '/^Table 5\. Standard deduction/{f=1} /^Add this amount to the standard deduction/{f=0} f' orinst.txt \
  > or40-2025-standard-deduction.txt

awk '/Table 4\. Federal tax liability subtraction AGI phaseout/{f=1} /Social Security and tier 1 Railroad/{f=0} f' orinst.txt \
  > or40-2025-federal-tax-subtraction.txt

awk '/Miscellaneous Oregon income tax information/{f=1} f{print} /regardless of filing status/{if(f)exit}' or17.txt \
  > or17-2025-annual-figures.txt
```

## Why the tax table is the strong check

Oregon prints its own answer for every band of taxable income from $0 to
$50,000, across two columns. `TestAgainstOregonTaxTable` checks all of it:
**1,004 cases over 502 bands**. Every other test here checks a case somebody
chose; this one checks cases nobody chose.

It also earned its keep immediately, in two separate ways.

**It caught an incomplete extract.** The first version of the filter kept lines
that *began* with a band, which is how the federal tax table is shaped. Oregon's
is not: the printed page sets four columns side by side, the lowest-income
column uses narrower bands than the rest, and its rows do not line up with the
others'. That filter parsed 387 bands, looked healthy, and had silently dropped
the whole $0 to $4,000 column. `assertTableIsComplete` fails on any
discontinuity, which is what turned a plausible extract into a failing build.

Oregon's bands are **not a constant width** — $20 near zero, then $30, $50 and
finally $100 — so neither a count nor a step size would work as a completeness
check. What holds throughout is continuity: every band's floor is the previous
band's ceiling, from $0 to exactly $50,000.

**It pinned the rounding rule, which no document states.** Oregon does not
compute the tax exactly and round the answer. It rounds *each bracket* to a
whole dollar and sums those. The difference is a dollar and it is real: for a
joint filer at $50,000, the exact bracket amounts are $418.00, $904.50 and
$2,432.50, summing to $3,755.00. Oregon's published rate chart says **$3,756**.
Rounding the total gives $3,755 and disagrees with every figure Oregon prints;
rounding each bracket gives $3,756 and agrees with all four published rate
chart bases and all 1,004 tax table cases. `roundToDollar` in `tax.go` is that
rule, and it was derived from the data rather than from prose, because neither
the instructions nor Publication OR-17 says it anywhere.

## Which column is 2025

`or17-2025-annual-figures.txt` carries four tax years side by side — 2022
through 2025 — and every figure this library uses is the last on its row. The
extract keeps the "for tax years 2022–2025" heading so that the column order is
checkable, and `TestBracketsMatchPublicationOR17` asserts that heading is still
there before trusting any figure beneath it.

The standard deduction is the cross-check that the right column was read: the
2025 column of Publication OR-17 and Table 5 of the separate Form OR-40
instructions state the same five figures, and the tests check both documents.

## What this does not prove

That the extract faithfully represents the PDF. The hashes let somebody redo the
extraction and diff, and nothing runs that check automatically.

That the concepts were understood. The tax table checks the rate ladder
exhaustively and proves nothing about the federal tax subtraction, the exemption
credit cliff, or what belongs in Oregon adjusted gross income. Those tests are
hand-written.

Nothing here covers Form OR-40-P or OR-40-N. Every figure is a full-year
resident's, and `TaxableIncome` returns the residency question unanswered.

# Estimated tax, 2026

The files below back the estimated tax rules in `estimated_y2026.go`. They
were retrieved on 2026-10-20 with curl 8.5.0. The PDFs were extracted with
`pdftotext` from poppler 24.02.0. The statute and rule pages are HTML, and
were reduced to text with the Python 3.12.3 standard library's `html.parser`,
using the script below. `D` is `https://www.oregon.gov/dor/forms/FormsPubs`.

| File | Source | sha256 of the download |
|---|---|---|
| `or-estimate-2026.txt` | [Publication OR-ESTIMATE, 2026 (150-101-026, Rev. 10-07-25)](https://www.oregon.gov/dor/forms/FormsPubs/publication-or-estimate_101-026_2026.pdf), whole document | `6abb5d87494ce16da8f32b93b812bd95c830acb0763294b8e0bcff420bee96b1` |
| `or10-instructions-2025.txt` | [Form OR-10 Instructions, 2025 (150-101-031-1, Rev. 9-16-25)](https://www.oregon.gov/dor/forms/FormsPubs/form-or-10-instructions_101-031-1_2025.pdf), whole document | `0c126fe76ac89ce24169f104ad4d4716e77ae993b6313a94d1f030b08f3db6d4` |
| `or40v-instructions.txt` | [Form OR-40-V Instructions (150-101-172-1, Rev. 04-23-25)](https://www.oregon.gov/dor/forms/FormsPubs/form-or-40-v-instructions_101-172-1.pdf), whole document | `40415fd83a7cfad6418ad844a3cb4099ce9eb3921bdf3942b67463f9167daa4b` |
| `ors316-estimated-tax.txt` | [ORS chapter 316 (2025 edition)](https://www.oregonlegislature.gov/bills_laws/ors/ors316.html), sections 316.557 to 316.589 | `eb506256ad70399518a475fbcc53e7993c62d42de1173ea5f12fcc02a00fc88c` |
| `ors305-220.txt` | [ORS chapter 305 (2025 edition)](https://www.oregonlegislature.gov/bills_laws/ors/ors305.html), section 305.220 | `9ba198368624ec3f6e77565c5f1c367f4b7f6d708dea55db41bd3376cffa05a7` |
| `ors187-legal-holidays.txt` | [ORS chapter 187 (2025 edition)](https://www.oregonlegislature.gov/bills_laws/ors/ors187.html), sections 187.010 and 187.020 | `251fa36f24c3902db7618ba1a7f2048ab62cc8452c265ac984ca78a47a604bea` |
| `oar150-316-estimated-tax.txt` | [OAR chapter 150, division 316](https://secure.sos.state.or.us/oard/displayDivisionRules.action?selectedDivision=448), rules 150-316-0465 to 150-316-0497 | `c1c76ab1e027be9629281183b2631ba84befa8906ed474f7d47320762903be10` |
| `oar150-305-interest.txt` | [OAR chapter 150, division 305](https://secure.sos.state.or.us/oard/displayDivisionRules.action?selectedDivision=419), rules 150-305-0140 to 150-305-0144 | `713b4cbed8e27d66d58c257103d94255ab53fdf861f6f2edc0ffd9fbe613af93` |
| `orlaws-2026-sections-amended.txt` | [Oregon Laws 2026, ORS sections amended, repealed or added to](https://www.oregonlegislature.gov/bills_laws/lawsstatutes/2026OrLawAR.pdf), whole document, page layout | `0db96df54637b8b1402dad9574ac7d56d6921f986d2b8b9e506da59e11fa205e` |

The Secretary of State's rule pages are generated on request, so a later
download can differ in markup, and so in its hash, with the same rule text.

## Reproducing them

```bash
D=https://www.oregon.gov/dor/forms/FormsPubs
curl -L -o or-estimate.pdf "$D/publication-or-estimate_101-026_2026.pdf"
curl -L -o or10-inst.pdf   "$D/form-or-10-instructions_101-031-1_2025.pdf"
curl -L -o or40v-inst.pdf  "$D/form-or-40-v-instructions_101-172-1.pdf"
curl -L -o orlaws-ar.pdf   https://www.oregonlegislature.gov/bills_laws/lawsstatutes/2026OrLawAR.pdf
for c in 316 305 187; do
  curl -L -o ors$c.html "https://www.oregonlegislature.gov/bills_laws/ors/ors$c.html"
done
curl -L -o oar448.html "https://secure.sos.state.or.us/oard/displayDivisionRules.action?selectedDivision=448"
curl -L -o oar419.html "https://secure.sos.state.or.us/oard/displayDivisionRules.action?selectedDivision=419"

# The DOR documents are set in two columns; -raw keeps each column and each
# table row in reading order.
pdftotext -raw or-estimate.pdf or-estimate-2026.txt
pdftotext -raw or10-inst.pdf   or10-instructions-2025.txt
pdftotext -raw or40v-inst.pdf  or40v-instructions.txt
pdftotext -layout orlaws-ar.pdf orlaws-2026-sections-amended.txt

cat > html2txt.py <<'PY'
import sys, html.parser

class P(html.parser.HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.out, self.skip = [], 0

    def handle_starttag(self, tag, attrs):
        if tag in ("script", "style"):
            self.skip += 1
        if tag in ("p", "br", "div", "tr", "li", "h1", "h2", "h3", "h4"):
            self.out.append("\n")

    def handle_endtag(self, tag):
        if tag in ("script", "style"):
            self.skip -= 1

    def handle_data(self, data):
        if not self.skip:
            self.out.append(data)

p = P()
p.feed(sys.stdin.read())
text = "".join(p.out).replace("\xa0", " ")
print("\n".join(l.rstrip() for l in text.splitlines() if l.strip()))
PY

# The ORS pages are windows-1252.
iconv -f windows-1252 -t utf-8 ors316.html | python3 html2txt.py \
  | awk '/^PAYMENT OF ESTIMATED$/{f=1} /^ *316\.590 \[/{f=0} f' > ors316-estimated-tax.txt
iconv -f windows-1252 -t utf-8 ors305.html | python3 html2txt.py \
  | awk '/^ *305\.220$/{f=1} /^ *305\.222$/{f=0} f' > ors305-220.txt
iconv -f windows-1252 -t utf-8 ors187.html | python3 html2txt.py \
  | awk '/^LEGAL HOLIDAYS$/{f=1} /^STANDARD OF TIME$/{f=0} f' > ors187-legal-holidays.txt
python3 html2txt.py < oar448.html \
  | awk '/^150-316-0465$/{f=1} /^150-316-0505$/{f=0} f' > oar150-316-estimated-tax.txt
python3 html2txt.py < oar419.html \
  | awk '/^150-305-0140$/{f=1} /^150-305-0150$/{f=0} f' > oar150-305-interest.txt
```

No site blocked a download.

## The two worked examples are the strong check

Publication OR-ESTIMATE's Example 3 gives Allan and Louise's required annual
payment ($3,521 on $3,912 of tax), their estimates after $721 of withholding
($2,800) and the quarterly amount ($700).
`TestEstimatedRequirementMatchesORESTIMATEExample` reproduces it in cents.

The Form OR-10 instructions work Catelyn's running balance line by line: $19,
$3 and $11, $33 by September 15, 2025. `TestEstimatedInterestMatchesOR10Example`
reproduces those lines to the dollar with 2025's dates and its 0.000247 daily
rate, through `export_test.go`, then runs the same household under the 2026
rules.

## What is not here

- **The 2026 Form OR-10.** Not published. The 2025 instructions' worksheets
  are used with the 2026 dates, and with the 2026 daily rate those instructions
  already print.
- **The rate from January 1, 2027.** OAR 150-305-0140 lists rates only through
  January 1, 2026. The interest stops at December 31, 2026 and the gap is
  reported in `Required`.
- **ORS 314.385.** The research file cites it for the return due date. The
  interest stop is taken from ORS 316.587(3)(a), the 15th day of the fourth
  month, and the Form OR-10 instructions instead.
