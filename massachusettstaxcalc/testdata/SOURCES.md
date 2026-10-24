# Where these files came from

Every Massachusetts figure in this package is checked against a document the
Department of Revenue or the General Court published. These files are text
extracted from them. This records which document, and how, so that anybody can
redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0; HTML pages reduced to text with the Python 3.12.3 script
below. The research file reports that mass.gov answered curl with HTTP 403;
on 2026-10-20 it answered 200 with the documents, so nothing here rests on a
quote copied from the research.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `form-1-es-2026.txt` | [2026 Form 1-ES](https://www.mass.gov/doc/2026-form-1-es-estimated-tax-payment-vouchers-instructions-and-worksheets/download), instructions and worksheets, up to the payment vouchers, reading order | `d5bdb884a9b70a2b5c04356e58a7edcdccd59ce6841bf1550409b6d9c99f0d19` |
| `form-m-2210-2025.txt` | [2025 Form M-2210](https://www.mass.gov/doc/2025-form-m-2210-underpayment-of-massachusetts-estimated-income-tax/download), form and instructions, reading order | `96fd1bd71c8926847f2c95dc74b7bad5fd50155e4e12bbb88cad1f0855fcbaeb` |
| `form-m-2210-2026-draft.txt` | [Draft 2026 Form M-2210](https://www.mass.gov/doc/draft-2026-form-m-2210-underpayment-of-massachusetts-estimated-income-tax/download), "DO NOT FILE", form and instructions, reading order | `5b043d848a5053847e34c80b623bca1798ecf14750642fd216fa1ef473995740` |
| `ap-241.txt` | [AP 241, Estimated Income Tax Payments](https://www.mass.gov/administrative-procedure/ap-241-requirement-to-make-estimated-income-tax-payments) (01/29/2025), from its date to its references | `ada941fa977b0423206defb46077c239c8246527419b33d9c01e1d274c31ae23` (the HTML) |
| `tir-26-5.txt` | [TIR 26-5, Interest Rate on Overpayments and Underpayments](https://www.mass.gov/technical-information-release/tir-26-5-interest-rate-on-overpayments-and-underpayments) (09/01/2026), from its date to the end of the rate table | `0cea84d41a79f4949df5dc8260bc5c970be018f927ad69a9ce355b6e28d767a4` (the HTML) |
| `mgl-62b-13.txt` | [M.G.L. c. 62B § 13](https://malegislature.gov/Laws/GeneralLaws/PartI/TitleIX/Chapter62B/Section13), whole section | `9ad27a4fa3ccbaa7afb730aa2f1b2b3d3c9a95e5b43c9113c1ac5ede26386c66` (the HTML) |
| `mgl-62b-14.txt` | [M.G.L. c. 62B § 14](https://malegislature.gov/Laws/GeneralLaws/PartI/TitleIX/Chapter62B/Section14), whole section | `efc96a29eac220230e156e2b85ef548046b2a8127575ea8af2bece2043796c03` (the HTML) |
| `mgl-62c-32a.txt` | [M.G.L. c. 62C § 32](https://malegislature.gov/Laws/GeneralLaws/PartI/TitleIX/Chapter62C/Section32), subsection (a) | `4abb8f8021ccf78caa25546e75518fcd81988eb7becfa1ac1200f6768089b896` (the HTML) |
| `mgl-4-7-eighteenth.txt` | [M.G.L. c. 4 § 7](https://malegislature.gov/Laws/GeneralLaws/PartI/TitleI/Chapter4/Section7), clause Eighteenth, legal holidays | `a6457d60bec047a9b28ca1d57f5efea36cbeb70c1ef26fe221c3d861de74b597` (the HTML) |

An HTML page's hash records what was fetched. The page carries navigation and
scripts that change without its content changing, so a later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
D=https://www.mass.gov/doc
L=https://malegislature.gov/Laws/GeneralLaws/PartI
curl -L -o 1es.pdf        "$D/2026-form-1-es-estimated-tax-payment-vouchers-instructions-and-worksheets/download"
curl -L -o m2210-2025.pdf "$D/2025-form-m-2210-underpayment-of-massachusetts-estimated-income-tax/download"
curl -L -o m2210-2026.pdf "$D/draft-2026-form-m-2210-underpayment-of-massachusetts-estimated-income-tax/download"
curl -L -o ap241.html     https://www.mass.gov/administrative-procedure/ap-241-requirement-to-make-estimated-income-tax-payments
curl -L -o tir26-5.html   https://www.mass.gov/technical-information-release/tir-26-5-interest-rate-on-overpayments-and-underpayments
curl -L -o c62b13.html    "$L/TitleIX/Chapter62B/Section13"
curl -L -o c62b14.html    "$L/TitleIX/Chapter62B/Section14"
curl -L -o c62c32.html    "$L/TitleIX/Chapter62C/Section32"
curl -L -o c4s7.html      "$L/TitleI/Chapter4/Section7"

pdftotext 1es.pdf 1es.txt
awk 'NR==1,/Practitioners: You must obtain prior approval/' 1es.txt > form-1-es-2026.txt
pdftotext m2210-2025.pdf form-m-2210-2025.txt
pdftotext m2210-2026.pdf form-m-2210-2026-draft.txt

cat > h2t.py <<'PY'
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
for f in ap241 tir26-5 c62b13 c62b14 c62c32 c4s7; do python3 h2t.py $f.html > $f.txt; done
awk '/^Date: \|$/{f=1} /^REFERENCES:/{f=0} f' ap241.txt \
  | grep -v -i "table of contents\|^You skipped\|^top$" > ap-241.txt
awk '/^Date: \|$/{f=1} f; /830 CMR 62C.33.1/{if(f)exit}' tir26-5.txt \
  | grep -v -i "table of contents\|^You skipped\|^top$" > tir-26-5.txt
awk '/^Section 13: /{f=1} /^Site Information & Links/{f=0} f' c62b13.txt > mgl-62b-13.txt
awk '/^Section 14: /{f=1} /^Site Information & Links/{f=0} f' c62b14.txt > mgl-62b-14.txt
awk '/^Section 32: /{f=1} /^\(b\) /{f=0} f' c62c32.txt > mgl-62c-32a.txt
awk '/^Eighteenth, /' c4s7.txt > mgl-4-7-eighteenth.txt
```

The forms are set in columns, so they are extracted in reading order. The
draft 2026 M-2210 carries a diagonal "DRAFT" watermark that `pdftotext` breaks
into fragments between some lines; the phrases the tests check fall clear of
them.

## There is no worked example

Neither Form 1-ES nor Form M-2210 works a case with a stated answer, and AP 241
has none. The tests check every figure against these extracts and work the
penalty cases by hand from M-2210 lines 13 to 24.

## Why both M-2210s

The 2025 form is the latest final one. The 2026 form is a draft marked "DO NOT
FILE". The tests check that both print the same arithmetic, exceptions and
annualised percentages; the 2026 dates, rate lines and surtax threshold come
from the draft and agree with Form 1-ES and TIR 26-5.

The 2025 form prints 8% for every day from April 16, 2025 to April 14, 2026,
including April 1 to 14, 2026, for which TIR 26-5 sets 7%. That is a 2025
penalty and is not computed here. The draft 2026 form's 7%, 8% and 8% match
TIR 26-5.

## What is not here

- **The rate from January 1, 2027.** Not published; the draft M-2210 prints
  "X%". The penalty reports the gap in `Required`.
- **DOR's estimated tax web page** (updated 06/18/2026). It gives the second
  installment as June 16, 2026, against June 15 in the statute and both forms,
  and says the rate is compounded daily. Neither is used, so it is not checked
  in.
- **A holiday list from the department.** None was found. The holidays come
  from c. 4 § 7. No 2026 due date is one, so the list changes no figure, and
  removing any of it fails no test.
