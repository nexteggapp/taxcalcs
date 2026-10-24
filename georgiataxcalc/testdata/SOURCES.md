# Where these files came from

Every Georgia figure in this package is checked against a document the
Department of Revenue, the General Assembly or the Governor's office
published. These files are text extracted from them. This records which
document, and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs extracted with `pdftotext` from
poppler 24.02.0. The press release was converted to text with the Python 3
script below, which drops scripts and styles, replaces each tag with a line
break, unescapes entities, and keeps the non-blank lines stripped.

| File | Source | sha256 of the download |
|---|---|---|
| `form-500-uet.txt` | [Form 500 UET, Underpayment of Estimated Tax by Individuals/Fiduciary (Rev. 07/15/25), "For tax years 2025 and later"](https://dor.georgia.gov/document/document/beginning-or-after-january-1-2025-500-uet-underpayment-estimated-tax-individuals/download), form and instructions, reading order | `364ea3b0644fbd26a9753732b3b91c16f0e6b8b7a5772451c70d08677432b9b0` |
| `form-500-uet-tables.txt` | the same PDF, Instruction B's chart and Exception 3's example, page layout | same |
| `form-500-es.txt` | [Form 500-ES, Individual and Fiduciary Estimated Tax (Rev. 05/12/25)](https://dor.georgia.gov/document/document/2026-500-es-estimated-tax-individuals-and-fiduciaries/download), which the Department lists as the 2026 voucher: the instructions, and the revision line | `dbc270b3be6196d680af4a91fa1cf628813b1c9a37e6ad7b52a51ed8fe9e0b45` |
| `hb-463-2026.txt` | [HB 463 (2026), As Passed House and Senate](https://www.legis.ga.gov/api/legislation/document/20252026/249080), Sections 2-1 to 2-3 and 5-1 | `1c5fa7abcb2ab99964c09144384d774bceea9a1531ea235c7e637925459f6066` |
| `governor-2026-05-11.txt` | [Governor's press release, May 11, 2026, "Gov. Kemp Signs Legislation Lowering Taxes and Supporting Economic Growth"](https://gov.georgia.gov/press-releases/2026-05-11/gov-kemp-signs-legislation-lowering-taxes-and-supporting-economic-growth), the date, title and HB 463 paragraph | `4220fcc562b5ddf260bc942a2b4d347a295ec363729a48958e064579e8f1ce02` |

HB 463's PDF shows struck text and new text side by side, and `pdftotext`
cannot show the strike: "5.19 percent 4.99 percent" is the old rate struck and
the new one inserted. The press release confirms which is which.

A web page's sha256 is of the HTML as served that day; a later download may
hash differently and extract the same lines.

## Reproducing them

```bash
curl -L -o uet.pdf https://dor.georgia.gov/document/document/beginning-or-after-january-1-2025-500-uet-underpayment-estimated-tax-individuals/download
curl -L -o 500es.pdf https://dor.georgia.gov/document/document/2026-500-es-estimated-tax-individuals-and-fiduciaries/download
curl -L -o hb463.pdf https://www.legis.ga.gov/api/legislation/document/20252026/249080
curl -L -o gov.html https://gov.georgia.gov/press-releases/2026-05-11/gov-kemp-signs-legislation-lowering-taxes-and-supporting-economic-growth

pdftotext uet.pdf form-500-uet.txt
pdftotext -layout uet.pdf uet.L.txt
awk '/Period +Due Date/{f=1} f; /Jan\. 1 to Dec\. 31/{if(f)exit}' uet.L.txt > form-500-uet-tables.txt

pdftotext 500es.pdf 500es.txt
awk '/INSTRUCTIONS FOR INDIVIDUAL AND FIDUCIARIES/{f=1} f; /^If age 65 or older/{if(f)exit}' 500es.txt | tr -d '\f' > form-500-es.txt
grep -m1 "500 ES (Rev" 500es.txt >> form-500-es.txt

pdftotext hb463.pdf hb463.txt
sed -n '/^SECTION 2-1\.$/,/^SECTION 2-4\.$/p; /^SECTION 5-1\.$/,/^SECTION 5-2\.$/p' hb463.txt > hb-463-2026.txt

python3 - <<'PY'
import re, html
t = open('gov.html', encoding='utf-8').read()
t = re.sub(r'(?is)<(script|style).*?</\1>', '', t)
t = re.sub(r'(?s)<[^>]+>', '\n', t)
t = html.unescape(t)
open('gov.txt', 'w').write('\n'.join(l.strip() for l in t.splitlines() if l.strip()) + '\n')
PY
sed -n '/^May 11, 2026$/,/^Gov. Kemp Signs Legislation/p; /^HB 463$/,/promise to accelerate/p' gov.txt > governor-2026-05-11.txt
```

## Form 500 UET's example is the strong check

Instruction D works Exception 3 on a $779 tax with $109, $182, $291 and $436
paid by the four dates, and concludes that no installment is penalised: the
first three under Exception 3, the fourth because its own $145 exceeds its
installment. `TestEstimatedMatchesForm500UETExample` reproduces it. That
conclusion is also the evidence that a payment counts first toward its own
period's installment.

## What is not here

- **The statute.** O.C.G.A. §§ 48-7-114 to 48-7-120 are published officially
  only through LexisNexis, whose terms the research did not accept, and
  legis.ga.gov does not publish the Code. The rules rest on the forms.
- **A 2026 Form 500 UET.** The checked-in form is for "tax years 2025 and
  later", and its arithmetic is used with HB 463's 2026 figures.
- **A Georgia holiday list.** None is checked in. The four 2026 due dates fall
  on weekdays, and the test checks them against the 500-ES.
- **The 2025 IT-511 booklet.** It repeats the 9% rate Form 500 UET line 20
  prints; no figure here depends on it.
