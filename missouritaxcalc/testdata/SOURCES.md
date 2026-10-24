# Where these files came from

Every Missouri figure in this package is checked against a document the
Missouri Department of Revenue or the Revisor of Statutes published. These
files are text extracted from those documents. This records which document,
and how, so that anybody can redo it and diff the result.

Retrieved 2026-10-20 with curl 8.5.0. PDFs were extracted with `pdftotext`
from poppler 24.02.0; HTML pages were reduced to text with the Python 3.12.3
script in `minnesotataxcalc/testdata/SOURCES.md`.

| File | Source | sha256 of what was fetched |
|---|---|---|
| `mo-1040es-2026.txt` | [Form MO-1040ES, 2026 Declaration of Estimated Tax for Individuals (Revised 12-2025)](https://dor.mo.gov/forms/MO-1040ES_2026.pdf), General Instructions items 1 to 8, reading order | `660f9c44610bb911f5652c42710cf86d49f693eb151dff35640932e7cae58793` |
| `mo-2210-2025.txt` | [Form MO-2210, 2025 Underpayment of Estimated Tax by Individuals](https://dor.mo.gov/forms/MO-2210_2025.pdf), form and instructions, page layout | `b17d567571d9507937a88406243f08d8c3ff84c318485e2fcfda30ba8bbca6fd` |
| `rsmo-143.521.txt` | [RSMo 143.521](https://revisor.mo.gov/main/OneSection.aspx?section=143.521) | `85fc49c0b51cfba34d5d4a9efedc9d5b154fe5fa1da983bffe8bda449a1e8236` (the HTML) |
| `rsmo-143.531.txt` | [RSMo 143.531](https://revisor.mo.gov/main/OneSection.aspx?section=143.531) | `1412b509f73fd992b51667abb5c647449634d05ff48c487c0fb7ccf4c702b0a5` (the HTML) |
| `rsmo-143.761.txt` | [RSMo 143.761](https://revisor.mo.gov/main/OneSection.aspx?section=143.761) | `c4418515ab2ed8fde4d5e0f5a2323af2e801dc7791741fcfc84a4815be9cdcf1` (the HTML) |
| `rsmo-32.065.txt` | [RSMo 32.065](https://revisor.mo.gov/main/OneSection.aspx?section=32.065) | `303afda6bc79744620bf763dbdcb123fa1814e7e253a7c712f77256ff018522d` (the HTML) |
| `12-csr-10-41.010-emergency-2026.txt` | [12 CSR 10-41.010, emergency amendment effective January 1, 2026](https://dor.mo.gov/resources/proposed-rules/documents/ER_-12_CSR_10-41_010_Emergency_Amendment.pdf), page layout | `c66e55a0eef83ae7a0008128a8e0d0a16ca908fbecbede6b3181e02ee7afb886` |
| `dor-statutory-interest-rates.txt` | [Missouri DOR, Statutory Interest Rates](https://dor.mo.gov/taxation/statutory-interest-rates.html), the Refunds and Deficiency table | `9ba72d2c90d1079cff3874b16fcee7f2c290d03fa984ff0610d4fe2308806637` (the HTML) |

An HTML page's hash records what was fetched. A later fetch can hash
differently and extract the same; diff the extract rather than the hash.

## Reproducing them

```bash
curl -o es.pdf     https://dor.mo.gov/forms/MO-1040ES_2026.pdf
curl -o 2210.pdf   https://dor.mo.gov/forms/MO-2210_2025.pdf
curl -o er.pdf     https://dor.mo.gov/resources/proposed-rules/documents/ER_-12_CSR_10-41_010_Emergency_Amendment.pdf
curl -o rates.html https://dor.mo.gov/taxation/statutory-interest-rates.html
for s in 143.521 143.531 143.761 32.065; do
  curl -o $s.html "https://revisor.mo.gov/main/OneSection.aspx?section=$s"
done

pdftotext es.pdf es.txt
awk '/2026 Form MO-1040ES Declaration Of Estimated Tax For Individuals - General Instructions$/{f=1} /Rounding on Missouri returns/{if(f)exit} f' es.txt \
  | tr -d '\f\a' > mo-1040es-2026.txt
pdftotext -layout 2210.pdf mo-2210-2025.txt
pdftotext -layout er.pdf 12-csr-10-41.010-emergency-2026.txt

for s in 143.521 143.531 143.761 32.065; do python3 html2text.py $s.html > $s.txt; done
for s in 143.521 143.531 143.761; do
  awk "/^$s\\./{f=1} /^\\(L\\. /{if(f)exit} f" $s.txt > rsmo-$s.txt
done
awk '/^32.065\./{f=1} /^\(L\. /{if(f)exit} f' 32.065.txt > rsmo-32.065.txt
python3 html2text.py rates.html \
  | awk '/^Interest Rates – Refunds and Deficiency/{f=1} /^\* Effective \(Q3\) July 1, 2019/{if(f)exit} f' \
  > dor-statutory-interest-rates.txt
```

The MO-1040ES text carries form-feed and BEL control characters, which `tr`
removes. The MO-2210 extract keeps them; the tests normalise whitespace and
do not read them.

## Missouri has no worked penalty example

MO-2210 prints its line arithmetic and two exception examples, neither with a
penalty to check. The tests follow the lines: each column's total
underpayment, by the day ÷ 365 at each calendar year's rate, from its due date
until paid or the next due date, and the last column to April 15.

## What is not here

- **The 2026 MO-2210.** Not published. The 2025 form is used with its dates
  a year on and the 2026 and 2027 rates.
- **12 CSR 10-2.067.** sos.mo.gov answered the script with a Cloudflare
  challenge page ("Just a moment..."), sha256
  `8c2efa33a5304596f06675c96f0a75c081359f676d25d4e4ef68ef1f3f6a572c`, in
  place of the PDF. The rule is cited as `research/estimated-payments/mo.md`
  quotes it in its verification: subsection (3), "imposed without regard to
  any reasonable cause or lack of willful neglect for the underpayment";
  (2)(B), fishing income "does not constitute gross income from farming";
  and (8)(D), Exception 2 "in case of a taxpayer required to file a return
  for the preceding taxable year."
- **A Missouri holiday list.** Every 2026 due date is a weekday, so only
  weekends are applied, and the tests check the computed dates against the
  printed ones.
