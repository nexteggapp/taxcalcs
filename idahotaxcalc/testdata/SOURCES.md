# Where these files came from

Every Idaho figure in this package is checked against a document the Idaho
State Tax Commission or the Idaho Legislature published. These files are text
extracted from them. This records which document, and how, so that anybody can
redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0 in reading order. Web pages converted to text with the Python
3 script below, which drops scripts and styles, replaces each tag with a line
break, unescapes entities, and keeps the non-blank lines stripped.

| File | Source | sha256 of the download |
|---|---|---|
| `form-40-instructions-2025.txt` | [Form 40 instructions, tax year 2025 (EIN00046, 03-02-2026)](https://tax.idaho.gov/wp-content/uploads/forms/EIN00046/EIN00046_03-02-2026.pdf), from "Idaho doesn't require estimated tax payments." to the interest rate | `952263e92f7c096b10aba50555cc7c166c78efc3b6ce6e5029d4837404553005` |
| `form-51.txt` | [Form 51, Estimated Payment of Individual Income Tax (EFO00092, 09-18-2025)](https://tax.idaho.gov/wp-content/uploads/forms/EFO00092/EFO00092_09-18-2025.pdf), whole document | `d89877750cc3368d1c99fa41ca1925ba0ba86da2ab894e7ea236ce4fbd27305d` |
| `idaho-code-63-3036a.txt` | [Idaho Code § 63-3036A, Payment of estimated tax](https://legislature.idaho.gov/statutesrules/idstat/title63/t63ch30/sect63-3036A/), the section and its history | `b1161f78ccefec298bef717ab99f569472b33f0cc488bcc6bca94c4780905650` |
| `idaho-code-63-3033.txt` | [Idaho Code § 63-3033, Extension of time](https://legislature.idaho.gov/statutesrules/idstat/title63/t63ch30/sect63-3033/), the section and its history | `f5d6518b394eaa69416455049d46ee406171cc10c0477591a7b52c58ea6de8ac` |
| `istc-interest-rates.txt` | [Idaho State Tax Commission, Interest Rates](https://tax.idaho.gov/online-services/compliance/penalty-and-interest/interest-rates/), the table's heading and its 2027 and 2026 rows | `ce8fe1a718d17519c883cad15ec465e14952ec8cf67d5ce8c10b72cf66187703` |

A web page's sha256 is of the HTML as served that day. The pages carry
navigation that can change without the text changing, so a later download may
hash differently and still extract the same lines.

## Reproducing them

```bash
curl -o f40.pdf https://tax.idaho.gov/wp-content/uploads/forms/EIN00046/EIN00046_03-02-2026.pdf
curl -o f51.pdf https://tax.idaho.gov/wp-content/uploads/forms/EFO00092/EFO00092_09-18-2025.pdf
curl -o ic3036a.html https://legislature.idaho.gov/statutesrules/idstat/title63/t63ch30/sect63-3036A/
curl -o ic3033.html https://legislature.idaho.gov/statutesrules/idstat/title63/t63ch30/sect63-3033/
curl -o istc-int.html https://tax.idaho.gov/online-services/compliance/penalty-and-interest/interest-rates/

pdftotext f40.pdf f40.txt
pdftotext f51.pdf form-51.txt
awk '/^Idaho doesn.t require estimated tax payments\./{f=1} f; /rate effective for 2026\)\./{if(f)exit}' f40.txt \
  > form-40-instructions-2025.txt

for f in ic3036a ic3033 istc-int; do python3 - "$f" <<'PY'
import sys, re, html
f = sys.argv[1]
t = open(f + '.html', encoding='utf-8').read()
t = re.sub(r'(?is)<(script|style).*?</\1>', '', t)
t = re.sub(r'(?s)<[^>]+>', '\n', t)
t = html.unescape(t)
open(f + '.txt', 'w').write('\n'.join(l.strip() for l in t.splitlines() if l.strip()) + '\n')
PY
done
sed -n '/^63-3036A\.$/,/^\[63-3036A, added/p' ic3036a.txt > idaho-code-63-3036a.txt
sed -n '/^63-3033\.$/,/^\[63-3033, added/p' ic3033.txt > idaho-code-63-3033.txt
sed -n '/^Related Pages$/,/^2025$/p' istc-int.txt > istc-interest-rates.txt
```

## What is not here

- **A Form 51 for the 2026 return's extension.** The Form 51 checked in serves
  the 2025 extension and 2026 estimated payments. Its 80%, 100% and $50 figures
  are the statute's, and § 63-3033 was last amended in 2014, so they stand for
  2026 on the statute.
- **Idaho Code §§ 63-3045, 63-3046 and 63-3046A.** Not checked in. No figure
  here comes from them: the 6% rate is the Tax Commission's published figure,
  and the late-filing and late-payment penalties are named in the Explanation
  without their rates. `research/estimated-payments/id.md` read them.
