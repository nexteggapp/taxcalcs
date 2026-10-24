# Where these files came from

Every District of Columbia figure in this package is checked against a
document the Office of Tax and Revenue (OTR) or the Council of the District
of Columbia published. These files are text extracted from them. This records
which document, and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0, with no special headers. PDFs were
extracted with `pdftotext` from poppler 24.02.0, and HTML pages reduced to
text with the Python 3.12.3 script below. `B` is
`https://otr.cfo.dc.gov/sites/default/files/dc/sites/otr/publication/attachments`
and `C` is `https://code.dccouncil.gov/us/dc/council`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `d40es-2026.txt` | [2026 D-40ES, Estimated Payment for Individual Income Tax](https://otr.cfo.dc.gov/sites/default/files/dc/sites/otr/publication/attachments/2026_D40ES_Book_wLinks04012026.pdf) (`B/2026_D40ES_Book_wLinks04012026.pdf`), REV. 03/2026, the revision line and the General Instructions to the tax rate table, reading order | `fcce08263b771ebdb42910e9b5e181de0c5d151ef15790a7e48d69ad29c6373b` |
| `d2210-2025.txt` | [2025 D-2210, Underpayment of Estimated Income Tax By Individuals](https://otr.cfo.dc.gov/sites/default/files/dc/sites/otr/publication/attachments/2025_D-2210_021226.pdf) (`B/2025_D-2210_021226.pdf`), revised 05/2025, form and instructions, reading order | `26dbe65554e7244da2c614cc5f34ba0b8af5bfe81b6eb82a620b6cdef25b6072` |
| `d2210-2025-form.txt` | the same PDF, the form's front from the exceptions to its revision line, page layout | same |
| `d40-2025-excerpts.txt` | [2025 D-40 Booklet](https://otr.cfo.dc.gov/sites/default/files/dc/sites/otr/publication/attachments/2025_D40_Book_082026_v1.pdf) (`B/2025_D40_Book_082026_v1.pdf`): the estimated tax and electronic payment paragraphs, the rounding rule, and the instructions for lines 24 to 30 and 40, reading order | `6c1df426c2a6b4cdbe5182f7b83b3f9c0cf31011ce177f1ce404ed566d1754d0` |
| `dcc-47-4203.txt` | [D.C. Code Title 47, Chapter 42](https://code.dccouncil.gov/us/dc/council/code/titles/47/chapters/42/index.full.html) (`C/code/titles/47/chapters/42/index.full.html`), § 47-4203 and its notes | `07f84230f5817c8889074924bd20c8904b2f85928952b7480dc48b6a7dfae886` (the HTML) |
| `dcc-47-4201.txt` | [D.C. Code § 47-4201](https://code.dccouncil.gov/us/dc/council/code/sections/47-4201) (`C/code/sections/47-4201`), the section text | `6add9316838148c5d74ef676d8d19e74a2a5fa9a355c1656262fb8624d895ec3` (the HTML) |
| `dcc-47-1812.08-i.txt` | [D.C. Code § 47-1812.08](https://code.dccouncil.gov/us/dc/council/code/sections/47-1812.08) (`C/code/sections/47-1812.08`), subsection (i) | `2cead1bb58c12c78b04fc642be1ba40faea3d18bc601d95b1f1841240746e925` (the HTML) |
| `dcl-26-89.txt` | [D.C. Law 26-89](https://code.dccouncil.gov/us/dc/council/laws/26-89) (`C/laws/26-89`), its title, § 2(m) on § 47-1812.08, and §§ 4 to 6 with the effective date | `47d3234e74c1e60d651c4dd4747138934ef0b91ea83298c23242b69321ca687e` (the HTML) |
| `otr-underpayment-interest.txt` | [OTR, Underpayment of Estimated Tax Interest](https://otr.cfo.dc.gov/page/underpayment-estimated-tax-interest), the page's content | `279cab26aa3a428caa8afcc819655dd6513e49685a04002c53fe5d9b77f7dde8` (the HTML) |

An HTML page's hash records what was fetched. The OTR page carries scripts
that change between fetches: a second fetch the same day hashed
`7870c827…` and extracted to the same text. Diff the extract rather than the
hash. The Council's site asks visitors not to scrape it; each page here was
fetched once, by hand.

## Reproducing them

```bash
B=https://otr.cfo.dc.gov/sites/default/files/dc/sites/otr/publication/attachments
C=https://code.dccouncil.gov/us/dc/council
curl -sS -L -o d40es-2026.pdf "$B/2026_D40ES_Book_wLinks04012026.pdf"
curl -sS -L -o d2210-2025.pdf "$B/2025_D-2210_021226.pdf"
curl -sS -L -o d40-2025.pdf   "$B/2025_D40_Book_082026_v1.pdf"
curl -sS -L -o otr.html       https://otr.cfo.dc.gov/page/underpayment-estimated-tax-interest
curl -sS -L -o ch42.html      "$C/code/titles/47/chapters/42/index.full.html"
curl -sS -L -o 4201.html      "$C/code/sections/47-4201"
curl -sS -L -o 1812.08.html   "$C/code/sections/47-1812.08"
curl -sS -L -o law26-89.html  "$C/laws/26-89"

pdftotext d40es-2026.pdf es.txt
awk '/^D-40ES \(REV\. 03\/2026\)$/{print} /General Instructions$/{f=1} /Record of payments$/{if(f)exit} f' es.txt \
  > d40es-2026.txt

pdftotext d2210-2025.pdf d2210-2025.txt
pdftotext -layout d2210-2025.pdf d2210.L.txt
awk '/No underpayment Interest Is due/{f=1} f; /Revised 05\/2025/{if(f)exit}' d2210.L.txt > d2210-2025-form.txt

pdftotext d40-2025.pdf d40.txt
{ awk '/^You must file these vouchers if you are required to file a District of Columbia$/{f=1} f; /^through Modernized e-File \(MeF\)\.$/{if(f)exit}' d40.txt
  awk '/^Do not enter cents\. Round to the nearest dollar\.$/{f=1} f; /^\$10,500\.49 rounds to \$10,500$/{if(f)exit}' d40.txt
  awk '/^Line 24 Subtract Line 23 from Line 20$/{f=1} /^Line 31 DC income tax withheld$/{if(f)exit} f' d40.txt
} > d40-2025-excerpts.txt

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
for f in otr ch42 4201 1812.08 law26-89; do python3 html2text.py $f.html > $f.txt; done
awk '/^§ 47–4203\. Underpayment of estimated tax by individuals\.$/{f=1} /^§ 47–4204\. /{f=0} f' ch42.txt > dcc-47-4203.txt
awk '/^\(a\)\(1\) Unless otherwise provided/{f=1} /^Section References$/{f=0} f' 4201.txt > dcc-47-4201.txt
awk '/^\(i\) Declaration and payment of estimated tax/{f=1} /^\(j\) /{f=0} f' 1812.08.txt > dcc-47-1812.08-i.txt
{ awk 'NR==1' law26-89.txt
  awk '/^Note § 47-1812\.08$/{f=1} /^Sec\. 3\. /{f=0} f' law26-89.txt
  awk '/^Sec\. 4\. Applicability\.$/{f=1} /^Legislative History/{f=0} f' law26-89.txt
} > dcl-26-89.txt
awk '/^In 2005 the Office of Tax and Revenue/{f=1} f; /^If you have questions about estimated tax/{if(f)exit}' otr.txt \
  > otr-underpayment-interest.txt
```

The D-40 booklet is 13,000 lines of text, and the code chapter holds every
interest and penalty section, so only the parts the tests read are kept,
bounded by the documents' own headings. D-2210's front is a table whose
factors reading order separates from their line, so it is extracted again
with its layout.

## What is the strong check

The District publishes no worked example of the underpayment interest.
D-2210's instructions work lines 7 and 8 for a $2,000 installment and stop
before any interest. The tests check every figure against these extracts and
work the interest by hand from D-2210's line arithmetic.
`TestEstimatedInterestDivergesFromTheStatutesCompounding` computes the
statute's reading beside the form's: on $10,000 unpaid all year the form
charges $1,050 and 10% compounded daily gives $1,051.56; on $10,000 paid
May 1 the form charges $175 and the statute, to May 31, $126.81.

## What is not here

- **The 2026 D-2210.** Not published on 2026-10-20. The 2025 form's
  arithmetic and factors are used. Its factors match the fixed 10% rate over
  spans of 61, 92, 122 and 90 days, and 2026's spans are the same lengths.
- **The DC Municipal Regulations, Title 9, chapter 1.** The research found
  that dcregs.dc.gov does not render as text, and no regulation was read. No
  regulation on the annualised method, the holidays that move a due date, or
  the late-start installments is checked in.
- **A District holiday list.** None is checked in. D-40ES moves a date for a
  legal holiday without listing them. Every 2026 due date it prints is a
  weekday, and none is a federal holiday or DC Emancipation Day, so the tests
  check the computed dates against the printed ones.
- **D.C. Code § 47-4204.** The business section is cited by the research for
  the absence of a 10% exception in the statute; nothing here computes it.
